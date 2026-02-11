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

// InformerGlobalCache implements data.GlobalCache using client-go informers.
// It watches all resource types needed by the TUI: Nodes, Pods, Events,
// PodCliqueSets, PodCliqueScalingGroups, PodCliques, and ClusterTopology.
type InformerGlobalCache struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface

	// Informer factories
	coreFactory    informers.SharedInformerFactory
	dynamicFactory dynamicinformer.DynamicSharedInformerFactory

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
}

// NewInformerGlobalCache creates a new InformerGlobalCache.
// The cache is not started until Start() is called.
func NewInformerGlobalCache(clientset kubernetes.Interface, dynamicClient dynamic.Interface) *InformerGlobalCache {
	return &InformerGlobalCache{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		updatesCh:     make(chan struct{}, 1),
	}
}

// Start begins the informers and starts watching. Non-blocking.
func (c *InformerGlobalCache) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Create core informer factory for Nodes.
	c.coreFactory = informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		0, // no resync period — we rely on watch events
	)

	// Create dynamic informer factory for CRDs (PCS, PCSG, PC, ClusterTopology).
	c.dynamicFactory = dynamicinformer.NewDynamicSharedInformerFactory(
		c.dynamicClient,
		0, // no resync period
	)

	// Set up core informers
	c.nodeInformer = c.coreFactory.Core().V1().Nodes().Informer()

	// Pod informer with label selector for PCS-managed pods
	c.podInformerFactory = informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		0,
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = globalPcsLabelSelector
		}),
	)
	c.podInformer = c.podInformerFactory.Core().V1().Pods().Informer()

	// Event informer — watches all events (namespace-scoped, filtered in-memory)
	c.eventInformerFactory = informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		0,
	)
	c.eventInformer = c.eventInformerFactory.Core().V1().Events().Informer()

	// Dynamic informers for CRDs
	c.pcsInformer = c.dynamicFactory.ForResource(globalPcsGVR).Informer()
	c.pcsgInformer = c.dynamicFactory.ForResource(globalPcsgGVR).Informer()
	c.pcInformer = c.dynamicFactory.ForResource(globalPcGVR).Informer()
	c.ctInformer = c.dynamicFactory.ForResource(globalClusterTopologyGVR).Informer()

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
	//nolint:errcheck
	c.ctInformer.AddEventHandler(handler)

	// Start informers
	c.coreFactory.Start(ctx.Done())
	c.podInformerFactory.Start(ctx.Done())
	c.eventInformerFactory.Start(ctx.Done())
	c.dynamicFactory.Start(ctx.Done())

	return nil
}

// WaitForSync blocks until all informers have completed their initial list,
// or the context is cancelled. After sync completes, it triggers an immediate
// snapshot rebuild.
func (c *InformerGlobalCache) WaitForSync(ctx context.Context) bool {
	synced := cache.WaitForCacheSync(ctx.Done(),
		c.nodeInformer.HasSynced,
		c.podInformer.HasSynced,
		c.eventInformer.HasSynced,
		c.pcsInformer.HasSynced,
		c.pcsgInformer.HasSynced,
		c.pcInformer.HasSynced,
		c.ctInformer.HasSynced,
	)
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
	pcsgsByReplica, pcsgObjects := c.readPCSGs()

	// Read PodClique resources
	pcsByReplica, pcsByPCSG, pcsByPCSGReplica := c.readPodCliques()

	// Read Pods
	pods := c.readPods()

	// Build pod lookup (podName -> CachedPodInfo) and PodsByPodClique
	podInfos := make(map[string]data.CachedPodInfo, len(pods))
	podsByPodClique := make(map[string][]data.Resource)
	for _, pod := range pods {
		podInfos[pod.Name] = data.CachedPodInfo{
			NodeName: pod.NodeName,
			Labels:   pod.Labels,
		}

		podCliqueName := pod.Labels["grove.io/podclique"]
		if podCliqueName != "" {
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

	// Read Events
	eventsByObject := c.readEvents()

	// Build forest view (PodCliqueSet resources)
	pcsResources := c.buildPCSResources(pcsSpecs)

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

	snapshot := &data.CacheSnapshot{
		PodCliqueSets:       pcsResources,
		PodCliqueSetSpecs:   pcsSpecs,
		ReplicaIndexesByPCS: replicaIndexesByPCS,
		ScalingGroupsByReplica: func() map[string][]data.Resource {
			result := make(map[string][]data.Resource)
			for k, v := range pcsgsByReplica {
				result[k] = c.convertPCSGsToResources(v)
			}
			return result
		}(),
		PodCliquesByReplica:  pcsByReplica,
		ReplicaIndexesByPCSG: replicaIndexesByPCSG,
		PodCliquesByPCSG:     pcsByPCSG,
		PodCliquesByPCSGReplica: pcsByPCSGReplica,
		PodsByPodClique:      podsByPodClique,
		EventsByObject:       eventsByObject,
		TopologyViewData:     topologyViewData,
		GPUSummary:           gpuSummary,
		NodeGPUProducts:      nodeResult.nodeGPUProducts,
		NodeLabels:           nodeResult.nodeLabels,
		PodInfos:             podInfos,
		NodeGPUCapacity:      nodeResult.nodeGPUCapacity,
	}

	_ = pcsgObjects // used in convertPCSGsToResources

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
func (c *InformerGlobalCache) readClusterTopologyLevels() []corev1alpha1.TopologyLevel {
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
func (c *InformerGlobalCache) convertPCSGsToResources(pcsgs []*corev1alpha1.PodCliqueScalingGroup) []data.Resource {
	resources := make([]data.Resource, 0, len(pcsgs))
	for _, pcsg := range pcsgs {
		replicas := pcsg.Status.Replicas
		availableReplicas := pcsg.Status.AvailableReplicas
		scheduledReplicas := pcsg.Status.ScheduledReplicas

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

// readPodCliques reads all PodCliques from the informer cache and groups them.
func (c *InformerGlobalCache) readPodCliques() (
	byReplica map[string][]data.Resource, // "pcsName/replicaIndex" -> standalone PodCliques
	byPCSG map[string][]data.Resource, // pcsgName -> PodCliques
	byPCSGReplica map[string][]data.Resource, // "pcsgName/replicaIndex" -> PodCliques
) {
	items := c.pcInformer.GetStore().List()
	byReplica = make(map[string][]data.Resource)
	byPCSG = make(map[string][]data.Resource)
	byPCSGReplica = make(map[string][]data.Resource)

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
		scheduledReplicas := pc.Status.ScheduledReplicas

		resource := data.Resource{
			Name:      pc.Name,
			Type:      "PodClique",
			Ready:     fmt.Sprintf("%d/%d", readyReplicas, replicas),
			Scheduled: fmt.Sprintf("%d/%d", scheduledReplicas, replicas),
			Status:    "",
			Namespace: pc.Namespace,
		}

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
			}
		} else if pcsName != "" && replicaIndex != "" {
			// Standalone PodClique directly under PCS replica
			resource.ParentType = "PodCliqueSetReplica"
			resource.ParentName = fmt.Sprintf("%s-replica-%s", pcsName, replicaIndex)
			key := pcsName + "/" + replicaIndex
			byReplica[key] = append(byReplica[key], resource)
		}
	}

	return byReplica, byPCSG, byPCSGReplica
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
func (c *InformerGlobalCache) buildPCSResources(pcsSpecs map[string]*corev1alpha1.PodCliqueSet) []data.Resource {
	resources := make([]data.Resource, 0, len(pcsSpecs))

	for _, pcs := range pcsSpecs {
		replicas := pcs.Spec.Replicas
		availableReplicas := pcs.Status.AvailableReplicas
		scheduledReplicas := pcs.Status.ScheduledReplicas

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
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Name < resources[j].Name
	})

	return resources
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
