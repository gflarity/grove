// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package k8s

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"
)

// Compile-time check that InformerGlobalCache satisfies data.GlobalCache.
var _ data.GlobalCache = (*InformerGlobalCache)(nil)

// GVR definitions for Grove CRDs.
var (
	globalPcsGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquesets",
	}
	globalPcsgGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquescalinggroups",
	}
	globalPcGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}
	globalClusterTopologyGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "clustertopologies",
	}
)

const (
	// globalDebounceInterval is how long to wait after the last change event
	// before rebuilding the snapshot.
	globalDebounceInterval = 500 * time.Millisecond

	// globalPcsLabelSelector limits the Pod informer to PCS-managed pods only.
	globalPcsLabelSelector = "app.kubernetes.io/part-of"

	// eventMaxAge bounds in-memory event storage to recent events only.
	eventMaxAge = 1 * time.Hour
)

// GlobalCacheOption configures an InformerGlobalCache.
type GlobalCacheOption func(*InformerGlobalCache)

// WithCacheNamespace scopes the cache's informers to a single namespace.
// Namespace-scoped resources (Pods, Events, PCS, PCSG, PC) will only watch
// the given namespace. Cluster-scoped resources (Nodes, ClusterTopology) are
// always watched cluster-wide regardless of this option.
// When ns is "" or this option is not provided, the cache watches all namespaces.
func WithCacheNamespace(ns string) GlobalCacheOption {
	return func(c *InformerGlobalCache) {
		c.namespace = ns
	}
}

// WithOnWarning sets a callback that is invoked for non-fatal warnings during
// cache startup (e.g. a missing CRD). The TUI uses this to surface warnings in
// the error log box instead of losing them to stderr.
func WithOnWarning(fn func(string)) GlobalCacheOption {
	return func(c *InformerGlobalCache) {
		c.onWarning = fn
	}
}

// InformerGlobalCache implements data.GlobalCache using client-go informers.
// It watches all resource types needed by the TUI: Nodes, Pods, Events,
// PodCliqueSets, PodCliqueScalingGroups, PodCliques, and ClusterTopology.
type InformerGlobalCache struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface

	// namespace limits namespace-scoped informers to a single namespace.
	// Empty string means all namespaces (cluster-wide).
	namespace string

	// Informer factories
	coreFactory           informers.SharedInformerFactory
	dynamicFactory        dynamicinformer.DynamicSharedInformerFactory
	clusterDynamicFactory dynamicinformer.DynamicSharedInformerFactory

	// Additional factories for filtered informers
	podInformerFactory   informers.SharedInformerFactory
	eventInformerFactory informers.SharedInformerFactory

	// Informers
	nodeInformer  cache.SharedIndexInformer
	podInformer   cache.SharedIndexInformer
	eventInformer cache.SharedIndexInformer
	pcsInformer   cache.SharedIndexInformer
	pcsgInformer  cache.SharedIndexInformer
	pcInformer    cache.SharedIndexInformer
	ctInformer    cache.SharedIndexInformer

	// Snapshot state
	snapshot  *data.CacheSnapshot
	mu        sync.RWMutex
	updatesCh chan struct{}

	// Debounce
	debounceTimer *time.Timer
	debounceMu    sync.Mutex

	// Lifecycle
	cancel  context.CancelFunc
	stopped bool

	// Optional warning callback for non-fatal startup issues.
	onWarning func(string)

	// topologyCRDAvailable tracks whether the ClusterTopology CRD was found
	// during Start(). When false, the ctInformer is nil and topology features
	// are gracefully unavailable.
	topologyCRDAvailable bool
}

// NewInformerGlobalCache creates a new InformerGlobalCache.
// The cache is not started until Start() is called.
func NewInformerGlobalCache(clientset kubernetes.Interface, dynamicClient dynamic.Interface, opts ...GlobalCacheOption) *InformerGlobalCache {
	c := &InformerGlobalCache{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		updatesCh:     make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetOnWarning sets the warning callback. Implements data.WarningConfigurable.
func (c *InformerGlobalCache) SetOnWarning(fn func(string)) {
	c.onWarning = fn
}

// Start begins the informers and starts watching. Non-blocking.
func (c *InformerGlobalCache) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Create core informer factory for Nodes — always cluster-wide.
	c.coreFactory = informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		0, // no resync period — we rely on watch events
	)

	// Pod informer with label selector for PCS-managed pods.
	// When a namespace is set, scope to that namespace.
	podFactoryOpts := []informers.SharedInformerOption{
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = globalPcsLabelSelector
		}),
	}
	if c.namespace != "" {
		podFactoryOpts = append(podFactoryOpts, informers.WithNamespace(c.namespace))
	}
	c.podInformerFactory = informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		0,
		podFactoryOpts...,
	)

	// Event informer — when a namespace is set, scope to that namespace.
	var eventFactoryOpts []informers.SharedInformerOption
	if c.namespace != "" {
		eventFactoryOpts = append(eventFactoryOpts, informers.WithNamespace(c.namespace))
	}
	c.eventInformerFactory = informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		0,
		eventFactoryOpts...,
	)

	// Dynamic informer factory for namespace-scoped CRDs (PCS, PCSG, PC).
	// When a namespace is set, use NewFilteredDynamicSharedInformerFactory to scope.
	if c.namespace != "" {
		c.dynamicFactory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(
			c.dynamicClient,
			0,
			c.namespace,
			nil,
		)
	} else {
		c.dynamicFactory = dynamicinformer.NewDynamicSharedInformerFactory(
			c.dynamicClient,
			0,
		)
	}

	// Set up core informers
	c.nodeInformer = c.coreFactory.Core().V1().Nodes().Informer()
	c.podInformer = c.podInformerFactory.Core().V1().Pods().Informer()
	c.eventInformer = c.eventInformerFactory.Core().V1().Events().Informer()

	// Dynamic informers for namespace-scoped CRDs
	c.pcsInformer = c.dynamicFactory.ForResource(globalPcsGVR).Informer()
	c.pcsgInformer = c.dynamicFactory.ForResource(globalPcsgGVR).Informer()
	c.pcInformer = c.dynamicFactory.ForResource(globalPcGVR).Informer()

	// ClusterTopology informer — check if the CRD exists first to avoid
	// noisy reflector errors when it doesn't.
	c.topologyCRDAvailable = c.checkCRDExists(globalClusterTopologyGVR)
	if c.topologyCRDAvailable {
		c.clusterDynamicFactory = dynamicinformer.NewDynamicSharedInformerFactory(
			c.dynamicClient,
			0,
		)
		c.ctInformer = c.clusterDynamicFactory.ForResource(globalClusterTopologyGVR).Informer()
	} else {
		if c.onWarning != nil {
			c.onWarning("ClusterTopology CRD not found — topology view unavailable")
		}
	}

	// Register event handlers on all informers
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.onChange() },
		UpdateFunc: func(_, _ interface{}) { c.onChange() },
		DeleteFunc: func(_ interface{}) { c.onChange() },
	}

	//nolint:errcheck // registration errors only occur if informer is stopped
	c.nodeInformer.AddEventHandler(handler)
	//nolint:errcheck
	c.podInformer.AddEventHandler(handler)
	//nolint:errcheck
	c.eventInformer.AddEventHandler(handler)
	//nolint:errcheck
	c.pcsInformer.AddEventHandler(handler)
	//nolint:errcheck
	c.pcsgInformer.AddEventHandler(handler)
	//nolint:errcheck
	c.pcInformer.AddEventHandler(handler)
	if c.ctInformer != nil {
		//nolint:errcheck
		c.ctInformer.AddEventHandler(handler)
	}

	// Start informers
	c.coreFactory.Start(ctx.Done())
	c.podInformerFactory.Start(ctx.Done())
	c.eventInformerFactory.Start(ctx.Done())
	c.dynamicFactory.Start(ctx.Done())
	if c.clusterDynamicFactory != nil {
		c.clusterDynamicFactory.Start(ctx.Done())
	}

	return nil
}

// WaitForSync blocks until all informers have completed their initial list,
// or the context is cancelled. After sync completes, it triggers an immediate
// snapshot rebuild.
func (c *InformerGlobalCache) WaitForSync(ctx context.Context) bool {
	syncFuncs := []cache.InformerSynced{
		c.nodeInformer.HasSynced,
		c.podInformer.HasSynced,
		c.eventInformer.HasSynced,
		c.pcsInformer.HasSynced,
		c.pcsgInformer.HasSynced,
		c.pcInformer.HasSynced,
	}
	if c.ctInformer != nil {
		syncFuncs = append(syncFuncs, c.ctInformer.HasSynced)
	}
	synced := cache.WaitForCacheSync(ctx.Done(), syncFuncs...)
	if synced {
		c.rebuildSnapshot()
	}
	return synced
}

// Snapshot returns the latest CacheSnapshot. Thread-safe.
func (c *InformerGlobalCache) Snapshot() *data.CacheSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

// Updates returns the notification channel.
func (c *InformerGlobalCache) Updates() <-chan struct{} {
	return c.updatesCh
}

// Stop shuts down informers and closes the updates channel.
func (c *InformerGlobalCache) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	c.stopped = true

	if c.cancel != nil {
		c.cancel()
	}

	c.debounceMu.Lock()
	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
	}
	c.debounceMu.Unlock()

	close(c.updatesCh)
}

// GetPodYAML fetches a Pod and returns it as YAML string.
// This is the one API call that remains — it's a single GET for a specific pod.
func (c *InformerGlobalCache) GetPodYAML(ctx context.Context, podName, namespace string) (string, error) {
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get Pod: %w", err)
	}

	yamlData, err := yaml.Marshal(pod)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Pod to YAML: %w", err)
	}

	return string(yamlData), nil
}

// GetResourceYAML fetches any resource's YAML by type and name.
// For CRDs it uses the dynamic client; for Pods it delegates to GetPodYAML.
func (c *InformerGlobalCache) GetResourceYAML(ctx context.Context, resourceType, name, namespace string) (string, error) {
	switch resourceType {
	case "Pod":
		return c.GetPodYAML(ctx, name, namespace)
	case "PodCliqueSet":
		return c.getDynamicResourceYAML(ctx, globalPcsGVR, name, namespace)
	case "PodCliqueScalingGroup":
		return c.getDynamicResourceYAML(ctx, globalPcsgGVR, name, namespace)
	case "PodClique":
		return c.getDynamicResourceYAML(ctx, globalPcGVR, name, namespace)
	default:
		return "", fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// getDynamicResourceYAML fetches a CRD resource via the dynamic client and returns YAML.
func (c *InformerGlobalCache) getDynamicResourceYAML(ctx context.Context, gvr schema.GroupVersionResource, name, namespace string) (string, error) {
	var obj *unstructured.Unstructured
	var err error

	if namespace != "" {
		obj, err = c.dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = c.dynamicClient.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return "", fmt.Errorf("failed to get %s/%s: %w", gvr.Resource, name, err)
	}

	yamlData, err := yaml.Marshal(obj.Object)
	if err != nil {
		return "", fmt.Errorf("failed to marshal %s/%s to YAML: %w", gvr.Resource, name, err)
	}

	return string(yamlData), nil
}

// onChange is called by informer event handlers whenever any watched resource changes.
func (c *InformerGlobalCache) onChange() {
	c.debounceMu.Lock()
	defer c.debounceMu.Unlock()

	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
	}
	c.debounceTimer = time.AfterFunc(globalDebounceInterval, func() {
		c.rebuildSnapshot()
	})
}

// rebuildSnapshot reads from all informer caches and builds a new CacheSnapshot.
func (c *InformerGlobalCache) rebuildSnapshot() {
	// Read ClusterTopology
	levels := c.readClusterTopologyLevels()

	// Read all topology keys from levels for node label filtering
	topologyKeys := make(map[string]bool, len(levels))
	for _, level := range levels {
		topologyKeys[level.Key] = true
	}

	// Read Nodes (topology labels + GPU product labels)
	nodeResult := c.readNodeLabels(topologyKeys)

	// Read PCS specs
	pcsSpecs := c.readPCSSpecs()

	// Read PCSG resources
	pcsgsByReplica, _ := c.readPCSGs()

	// Read Pods first — needed to build scheduledByPodClique before reading PodCliques.
	pods := c.readPods()

	// Build pod lookup (podName -> CachedPodInfo), PodsByPodClique, and scheduledByPodClique.
	// scheduledByPodClique counts pods with NodeName != "" per PodClique — used to compute
	// the Scheduled column bottom-up instead of reading status.scheduledReplicas from CRDs.
	podInfos := make(map[string]data.CachedPodInfo, len(pods))
	podsByPodClique := make(map[string][]data.Resource)
	scheduledByPodClique := make(map[string]int32)
	for _, pod := range pods {
		podInfos[pod.Name] = data.CachedPodInfo{
			NodeName: pod.NodeName,
			Labels:   pod.Labels,
		}

		podCliqueName := pod.Labels["grove.io/podclique"]
		if podCliqueName != "" {
			// Count scheduled pods (those assigned to a node)
			if pod.NodeName != "" {
				scheduledByPodClique[podCliqueName]++
			}

			// Determine ready status
			ready := "0/1"
			phase := pod.Phase
			if phase == "Running" {
				ready = "1/1"
			}

			podsByPodClique[podCliqueName] = append(podsByPodClique[podCliqueName], data.Resource{
				Name:       pod.Name,
				Type:       "Pod",
				Ready:      ready,
				Scheduled:  phase,
				Status:     phase,
				Namespace:  pod.Namespace,
				ParentType: "PodClique",
				ParentName: podCliqueName,
			})
		}
	}

	// Read PodClique resources (uses scheduledByPodClique computed from pods above)
	pcsByReplica, pcsByPCSG, pcsByPCSGReplica, pcObjectsByPCSGReplica, standalonePCObjects := c.readPodCliques(scheduledByPodClique)

	// Read Events
	eventsByObject := c.readEvents()

	// Build forest view (PodCliqueSet resources) — uses computed scheduled counts
	pcsResources := c.buildPCSResources(pcsSpecs, scheduledByPodClique, pcObjectsByPCSGReplica, standalonePCObjects, pcsgsByReplica)

	// Build replica indexes by PCS
	replicaIndexesByPCS := make(map[string][]string)
	for key := range pcsgsByReplica {
		parts := splitReplicaKey(key)
		if len(parts) == 2 {
			pcsName, replicaIndex := parts[0], parts[1]
			if !containsString(replicaIndexesByPCS[pcsName], replicaIndex) {
				replicaIndexesByPCS[pcsName] = append(replicaIndexesByPCS[pcsName], replicaIndex)
			}
		}
	}
	for key := range pcsByReplica {
		parts := splitReplicaKey(key)
		if len(parts) == 2 {
			pcsName, replicaIndex := parts[0], parts[1]
			if !containsString(replicaIndexesByPCS[pcsName], replicaIndex) {
				replicaIndexesByPCS[pcsName] = append(replicaIndexesByPCS[pcsName], replicaIndex)
			}
		}
	}
	for k, v := range replicaIndexesByPCS {
		sort.Strings(v)
		replicaIndexesByPCS[k] = v
	}

	// Build PCSG replica indexes
	replicaIndexesByPCSG := make(map[string][]string)
	for key := range pcsByPCSGReplica {
		parts := splitReplicaKey(key)
		if len(parts) == 2 {
			pcsgName, replicaIndex := parts[0], parts[1]
			if !containsString(replicaIndexesByPCSG[pcsgName], replicaIndex) {
				replicaIndexesByPCSG[pcsgName] = append(replicaIndexesByPCSG[pcsgName], replicaIndex)
			}
		}
	}
	for k, v := range replicaIndexesByPCSG {
		sort.Strings(v)
		replicaIndexesByPCSG[k] = v
	}

	// Build the topology view snapshot
	topologyViewData := data.BuildTopologyViewData(levels, pcsSpecs, pods, nodeResult.nodeLabels)

	// Build and attach GPU summary
	gpuSummary := data.BuildGPUSummary(pods, nodeResult.nodeGPUProducts)
	topologyViewData.GPUSummary = gpuSummary
	topologyViewData.NodeGPUProducts = nodeResult.nodeGPUProducts
	topologyViewData.NodeGPUCapacity = nodeResult.nodeGPUCapacity

	// Sort all resource lists by name for stable table ordering.
	// Without this, informer store List() returns items in non-deterministic
	// map iteration order, causing table rows to shuffle on each snapshot
	// rebuild and making the cursor appear to jump randomly.
	scalingGroupsByReplica := func() map[string][]data.Resource {
		result := make(map[string][]data.Resource)
		for k, v := range pcsgsByReplica {
			result[k] = c.convertPCSGsToResources(v, scheduledByPodClique, pcObjectsByPCSGReplica)
		}
		return result
	}()
	data.SortResourceMapsByName(scalingGroupsByReplica)
	data.SortResourceMapsByName(pcsByReplica)
	data.SortResourceMapsByName(pcsByPCSG)
	data.SortResourceMapsByName(pcsByPCSGReplica)
	data.SortResourceMapsByName(podsByPodClique)

	snapshot := &data.CacheSnapshot{
		PodCliqueSets:          pcsResources,
		PodCliqueSetSpecs:      pcsSpecs,
		ReplicaIndexesByPCS:    replicaIndexesByPCS,
		ScalingGroupsByReplica: scalingGroupsByReplica,
		PodCliquesByReplica:    pcsByReplica,
		ReplicaIndexesByPCSG:   replicaIndexesByPCSG,
		PodCliquesByPCSG:       pcsByPCSG,
		PodCliquesByPCSGReplica: pcsByPCSGReplica,
		PodsByPodClique:        podsByPodClique,
		EventsByObject:       eventsByObject,
		TopologyViewData:     topologyViewData,
		GPUSummary:           gpuSummary,
		NodeGPUProducts:      nodeResult.nodeGPUProducts,
		NodeLabels:           nodeResult.nodeLabels,
		PodInfos:             podInfos,
		NodeGPUCapacity:      nodeResult.nodeGPUCapacity,
	}

	// Store and notify
	c.mu.Lock()
	c.snapshot = snapshot
	stopped := c.stopped
	c.mu.Unlock()

	if !stopped {
		select {
		case c.updatesCh <- struct{}{}:
		default:
		}
	}
}

// readClusterTopologyLevels reads the ClusterTopology CR from the informer cache.
// Returns nil when the ClusterTopology CRD is not available.
func (c *InformerGlobalCache) readClusterTopologyLevels() []corev1alpha1.TopologyLevel {
	if c.ctInformer == nil {
		return nil
	}
	return readClusterTopologyLevelsFromInformer(c.ctInformer)
}

// readNodeLabels reads all node labels from the informer cache.
func (c *InformerGlobalCache) readNodeLabels(topologyKeys map[string]bool) nodeReadResult {
	return readNodeLabelsFromInformer(c.nodeInformer, topologyKeys)
}

// readPCSSpecs reads all PodCliqueSet specs from the informer cache.
func (c *InformerGlobalCache) readPCSSpecs() map[string]*corev1alpha1.PodCliqueSet {
	return readPCSSpecsFromInformer(c.pcsInformer)
}

// readPCSGs reads all PodCliqueScalingGroups from the informer cache and groups them by PCS replica.
func (c *InformerGlobalCache) readPCSGs() (map[string][]*corev1alpha1.PodCliqueScalingGroup, []*corev1alpha1.PodCliqueScalingGroup) {
	items := c.pcsgInformer.GetStore().List()
	byReplica := make(map[string][]*corev1alpha1.PodCliqueScalingGroup)
	var allPCSGs []*corev1alpha1.PodCliqueScalingGroup

	for _, item := range items {
		uns, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		var pcsg corev1alpha1.PodCliqueScalingGroup
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uns.Object, &pcsg); err != nil {
			continue
		}

		allPCSGs = append(allPCSGs, &pcsg)

		pcsName := pcsg.Labels["app.kubernetes.io/part-of"]
		replicaIndex := pcsg.Labels["grove.io/podcliqueset-replica-index"]
		if pcsName != "" && replicaIndex != "" {
			key := pcsName + "/" + replicaIndex
			byReplica[key] = append(byReplica[key], &pcsg)
		}
	}

	return byReplica, allPCSGs
}

// convertPCSGsToResources converts PCSG objects to Resource display objects.
// It computes the Scheduled count bottom-up: a PCSG replica is "scheduled" when
// all its constituent PodCliques have scheduledPods >= minAvailable.
func (c *InformerGlobalCache) convertPCSGsToResources(
	pcsgs []*corev1alpha1.PodCliqueScalingGroup,
	scheduledByPodClique map[string]int32,
	pcObjectsByPCSGReplica map[string][]*corev1alpha1.PodClique,
) []data.Resource {
	resources := make([]data.Resource, 0, len(pcsgs))
	for _, pcsg := range pcsgs {
		replicas := pcsg.Status.Replicas
		availableReplicas := pcsg.Status.AvailableReplicas

		// Compute scheduled replicas bottom-up from PodClique data
		scheduledReplicas := computePCSGScheduledReplicas(pcsg, scheduledByPodClique, pcObjectsByPCSGReplica)

		pcsName := pcsg.Labels["app.kubernetes.io/part-of"]
		replicaIndex := pcsg.Labels["grove.io/podcliqueset-replica-index"]

		resources = append(resources, data.Resource{
			Name:       pcsg.Name,
			Type:       "PodCliqueScalingGroup",
			Ready:      fmt.Sprintf("%d/%d", availableReplicas, replicas),
			Scheduled:  fmt.Sprintf("%d/%d", scheduledReplicas, replicas),
			Status:     "",
			Namespace:  pcsg.Namespace,
			ParentType: "PodCliqueSetReplica",
			ParentName: fmt.Sprintf("%s-replica-%s", pcsName, replicaIndex),
		})
	}
	return resources
}

// computePCSGScheduledReplicas computes how many PCSG replicas are "scheduled".
// A PCSG replica is scheduled when ALL its constituent PodCliques have
// scheduledPods >= minAvailable (where minAvailable defaults to replicas if nil).
func computePCSGScheduledReplicas(
	pcsg *corev1alpha1.PodCliqueScalingGroup,
	scheduledByPodClique map[string]int32,
	pcObjectsByPCSGReplica map[string][]*corev1alpha1.PodClique,
) int32 {
	totalReplicas := pcsg.Spec.Replicas
	var scheduled int32
	for i := int32(0); i < totalReplicas; i++ {
		replicaKey := fmt.Sprintf("%s/%d", pcsg.Name, i)
		pcs := pcObjectsByPCSGReplica[replicaKey]
		if isPCSGReplicaScheduled(pcs, scheduledByPodClique) {
			scheduled++
		}
	}
	return scheduled
}

// isPCSGReplicaScheduled returns true if all PodCliques in a PCSG replica
// have scheduledPods >= minAvailable.
func isPCSGReplicaScheduled(pcs []*corev1alpha1.PodClique, scheduledByPodClique map[string]int32) bool {
	if len(pcs) == 0 {
		return false
	}
	for _, pc := range pcs {
		minAvail := pc.Spec.Replicas
		if pc.Spec.MinAvailable != nil {
			minAvail = *pc.Spec.MinAvailable
		}
		if scheduledByPodClique[pc.Name] < minAvail {
			return false
		}
	}
	return true
}

// readPodCliques reads all PodCliques from the informer cache and groups them.
// It uses scheduledByPodClique (computed from pod NodeName) instead of status.ScheduledReplicas.
// It also returns pcObjectsByPCSGReplica for PCSG scheduled-count roll-up.
func (c *InformerGlobalCache) readPodCliques(scheduledByPodClique map[string]int32) (
	byReplica map[string][]data.Resource, // "pcsName/replicaIndex" -> standalone PodCliques
	byPCSG map[string][]data.Resource, // pcsgName -> PodCliques
	byPCSGReplica map[string][]data.Resource, // "pcsgName/replicaIndex" -> PodCliques
	pcObjectsByPCSGReplica map[string][]*corev1alpha1.PodClique, // "pcsgName/replicaIndex" -> PodClique objects
	standalonePCObjects map[string][]*corev1alpha1.PodClique, // "pcsName/replicaIndex" -> standalone PodClique objects
) {
	items := c.pcInformer.GetStore().List()
	byReplica = make(map[string][]data.Resource)
	byPCSG = make(map[string][]data.Resource)
	byPCSGReplica = make(map[string][]data.Resource)
	pcObjectsByPCSGReplica = make(map[string][]*corev1alpha1.PodClique)
	standalonePCObjects = make(map[string][]*corev1alpha1.PodClique)

	for _, item := range items {
		uns, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		var pc corev1alpha1.PodClique
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uns.Object, &pc); err != nil {
			continue
		}

		labels := pc.Labels
		pcsName := labels["app.kubernetes.io/part-of"]
		replicaIndex := labels["grove.io/podcliqueset-replica-index"]
		pcsgName := labels["grove.io/podcliquescalinggroup"]
		pcsgReplicaIndex := labels["grove.io/podcliquescalinggroup-replica-index"]

		replicas := pc.Spec.Replicas
		readyReplicas := pc.Status.ReadyReplicas
		scheduledReplicas := scheduledByPodClique[pc.Name]

		resource := data.Resource{
			Name:      pc.Name,
			Type:      "PodClique",
			Ready:     fmt.Sprintf("%d/%d", readyReplicas, replicas),
			Scheduled: fmt.Sprintf("%d/%d", scheduledReplicas, replicas),
			Status:    "",
			Namespace: pc.Namespace,
		}

		pcCopy := pc // copy for storing in object maps

		if pcsgName != "" {
			// PodClique belongs to a PCSG
			resource.ParentType = "PodCliqueScalingGroup"
			resource.ParentName = pcsgName
			byPCSG[pcsgName] = append(byPCSG[pcsgName], resource)

			if pcsgReplicaIndex != "" {
				pcsgReplicaKey := pcsgName + "/" + pcsgReplicaIndex
				replicaResource := resource
				replicaResource.ParentType = "PodCliqueScalingGroupReplica"
				replicaResource.ParentName = fmt.Sprintf("%s-replica-%s", pcsgName, pcsgReplicaIndex)
				byPCSGReplica[pcsgReplicaKey] = append(byPCSGReplica[pcsgReplicaKey], replicaResource)

				pcObjectsByPCSGReplica[pcsgReplicaKey] = append(pcObjectsByPCSGReplica[pcsgReplicaKey], &pcCopy)
			}
		} else if pcsName != "" && replicaIndex != "" {
			// Standalone PodClique directly under PCS replica
			resource.ParentType = "PodCliqueSetReplica"
			resource.ParentName = fmt.Sprintf("%s-replica-%s", pcsName, replicaIndex)
			key := pcsName + "/" + replicaIndex
			byReplica[key] = append(byReplica[key], resource)

			standalonePCObjects[key] = append(standalonePCObjects[key], &pcCopy)
		}
	}

	return byReplica, byPCSG, byPCSGReplica, pcObjectsByPCSGReplica, standalonePCObjects
}

// readPods reads all pods from the informer cache and converts them to TopologyPodInput.
func (c *InformerGlobalCache) readPods() []data.TopologyPodInput {
	return readPodsFromInformer(c.podInformer)
}

// readEvents reads all events from the informer cache and indexes by involved object.
func (c *InformerGlobalCache) readEvents() map[string][]data.Event {
	items := c.eventInformer.GetStore().List()
	result := make(map[string][]data.Event)
	cutoff := time.Now().Add(-eventMaxAge)

	for _, item := range items {
		obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(item)
		if err != nil {
			continue
		}

		var event corev1.Event
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj, &event); err != nil {
			continue
		}

		// Skip old events
		eventTime := event.LastTimestamp.Time
		if eventTime.IsZero() {
			eventTime = event.EventTime.Time
		}
		if !eventTime.IsZero() && eventTime.Before(cutoff) {
			continue
		}

		age := data.FormatAge(eventTime)

		evt := data.Event{
			Type:      event.Type,
			Kind:      event.InvolvedObject.Kind,
			Reason:    event.Reason,
			Age:       age,
			From:      event.Source.Component,
			Message:   event.Message,
			Parent:    event.InvolvedObject.Name,
			Timestamp: eventTime,
		}

		key := event.InvolvedObject.Kind + "/" + event.InvolvedObject.Name
		result[key] = append(result[key], evt)
	}

	// Sort each event list newest-first
	for k := range result {
		sort.Slice(result[k], func(i, j int) bool {
			return result[k][i].Timestamp.After(result[k][j].Timestamp)
		})
	}

	return result
}

// buildPCSResources constructs Resource display objects for PodCliqueSets.
// It computes the Scheduled count bottom-up: a PCS replica is "scheduled" when
// all its standalone PodCliques have scheduledPods >= minAvailable AND all its
// PCSGs are fully scheduled (all PCSG replicas meet minAvailable).
func (c *InformerGlobalCache) buildPCSResources(
	pcsSpecs map[string]*corev1alpha1.PodCliqueSet,
	scheduledByPodClique map[string]int32,
	pcObjectsByPCSGReplica map[string][]*corev1alpha1.PodClique,
	standalonePCObjects map[string][]*corev1alpha1.PodClique,
	pcsgsByReplica map[string][]*corev1alpha1.PodCliqueScalingGroup,
) []data.Resource {
	resources := make([]data.Resource, 0, len(pcsSpecs))

	for _, pcs := range pcsSpecs {
		replicas := pcs.Spec.Replicas
		availableReplicas := pcs.Status.AvailableReplicas

		// Compute scheduled replicas bottom-up
		scheduledReplicas := computePCSScheduledReplicas(pcs, scheduledByPodClique, pcObjectsByPCSGReplica, standalonePCObjects, pcsgsByReplica)

		topology := "N/A"
		if pcs.Spec.Template.TopologyConstraint != nil {
			topology = string(pcs.Spec.Template.TopologyConstraint.PackDomain)
		}

		resources = append(resources, data.Resource{
			Name:      pcs.Name,
			Type:      "PodCliqueSet",
			Ready:     fmt.Sprintf("%d/%d", availableReplicas, replicas),
			Scheduled: fmt.Sprintf("%d/%d", scheduledReplicas, replicas),
			Status:    "",
			Namespace: pcs.Namespace,
			Topology:  topology,
		})
	}

	// Sort by name for stable ordering
	data.SortResourcesByName(resources)

	return resources
}

// computePCSScheduledReplicas computes how many PCS replicas are "scheduled".
// A PCS replica is scheduled when:
//  1. All standalone PodCliques in that replica have scheduledPods >= minAvailable
//  2. All PCSGs in that replica are fully scheduled (every PCSG replica meets minAvailable)
func computePCSScheduledReplicas(
	pcs *corev1alpha1.PodCliqueSet,
	scheduledByPodClique map[string]int32,
	pcObjectsByPCSGReplica map[string][]*corev1alpha1.PodClique,
	standalonePCObjects map[string][]*corev1alpha1.PodClique,
	pcsgsByReplica map[string][]*corev1alpha1.PodCliqueScalingGroup,
) int32 {
	var scheduled int32
	for i := int32(0); i < pcs.Spec.Replicas; i++ {
		replicaKey := fmt.Sprintf("%s/%d", pcs.Name, i)
		if isPCSReplicaScheduled(replicaKey, scheduledByPodClique, pcObjectsByPCSGReplica, standalonePCObjects, pcsgsByReplica) {
			scheduled++
		}
	}
	return scheduled
}

// isPCSReplicaScheduled checks whether a single PCS replica is fully scheduled.
func isPCSReplicaScheduled(
	replicaKey string,
	scheduledByPodClique map[string]int32,
	pcObjectsByPCSGReplica map[string][]*corev1alpha1.PodClique,
	standalonePCObjects map[string][]*corev1alpha1.PodClique,
	pcsgsByReplica map[string][]*corev1alpha1.PodCliqueScalingGroup,
) bool {
	// Check standalone PodCliques
	for _, pc := range standalonePCObjects[replicaKey] {
		minAvail := pc.Spec.Replicas
		if pc.Spec.MinAvailable != nil {
			minAvail = *pc.Spec.MinAvailable
		}
		if scheduledByPodClique[pc.Name] < minAvail {
			return false
		}
	}

	// Check PCSGs in this replica
	for _, pcsg := range pcsgsByReplica[replicaKey] {
		pcsgScheduled := computePCSGScheduledReplicas(pcsg, scheduledByPodClique, pcObjectsByPCSGReplica)
		pcsgMinAvail := pcsg.Spec.Replicas
		if pcsg.Spec.MinAvailable != nil {
			pcsgMinAvail = *pcsg.Spec.MinAvailable
		}
		if pcsgScheduled < pcsgMinAvail {
			return false
		}
	}

	// A replica with no standalone PCs and no PCSGs has nothing to schedule,
	// so we consider it scheduled only if at least one component exists.
	standalones := standalonePCObjects[replicaKey]
	pcsgs := pcsgsByReplica[replicaKey]
	if len(standalones) == 0 && len(pcsgs) == 0 {
		return false
	}

	return true
}

// Helper functions

func splitReplicaKey(key string) []string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return []string{key[:i], key[i+1:]}
		}
	}
	return []string{key}
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// checkCRDExists uses the discovery API to check whether a given GVR's resource
// type is registered on the API server. Returns false if the API group or
// resource is not found (e.g. the CRD is not installed).
func (c *InformerGlobalCache) checkCRDExists(gvr schema.GroupVersionResource) bool {
	resourceList, err := c.clientset.Discovery().ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	if err != nil {
		// API group not found — CRD not installed
		return false
	}
	for _, r := range resourceList.APIResources {
		if r.Name == gvr.Resource {
			return true
		}
	}
	return false
}

