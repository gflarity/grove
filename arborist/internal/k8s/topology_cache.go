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
	"sync"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Compile-time check that InformerTopologyCache satisfies data.TopologyCache.
var _ data.TopologyCache = (*InformerTopologyCache)(nil)

const (
	// debounceInterval is how long to wait after the last change event before
	// rebuilding the snapshot. This coalesces rapid changes (e.g. during a rollout)
	// into a single rebuild.
	debounceInterval = 500 * time.Millisecond

	// pcsLabelSelector limits the Pod informer to PCS-managed pods only.
	pcsLabelSelector = "app.kubernetes.io/part-of"
)

// GVR definitions for Grove CRDs.
var (
	pcsGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquesets",
	}
	clusterTopologyGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "clustertopologies",
	}
)

// InformerTopologyCache implements data.TopologyCache using client-go informers.
// It watches Nodes, Pods, PodCliqueSets, and ClusterTopology resources,
// debounces changes, and provides atomic snapshots for the TUI.
type InformerTopologyCache struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface

	// Informer factories
	coreFactory    informers.SharedInformerFactory
	dynamicFactory dynamicinformer.DynamicSharedInformerFactory

	// Informers
	nodeInformer cache.SharedIndexInformer
	podInformer  cache.SharedIndexInformer
	pcsInformer  cache.SharedIndexInformer
	ctInformer   cache.SharedIndexInformer

	// Snapshot state
	snapshot  *data.TopologyViewData
	mu        sync.RWMutex
	updatesCh chan struct{}

	// Debounce
	debounceTimer *time.Timer
	debounceMu    sync.Mutex

	// Lifecycle
	cancel  context.CancelFunc
	stopped bool
}

// NewInformerTopologyCache creates a new InformerTopologyCache.
// The cache is not started until Start() is called.
func NewInformerTopologyCache(clientset kubernetes.Interface, dynamicClient dynamic.Interface) *InformerTopologyCache {
	return &InformerTopologyCache{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		updatesCh:     make(chan struct{}, 1),
	}
}

// Start begins the informers and starts watching. Non-blocking.
func (c *InformerTopologyCache) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Create core informer factory for Nodes and Pods.
	// For Pods, we use a label selector to only watch PCS-managed pods.
	c.coreFactory = informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		0, // no resync period — we rely on watch events
	)

	// Create dynamic informer factory for CRDs (PCS, ClusterTopology).
	c.dynamicFactory = dynamicinformer.NewDynamicSharedInformerFactory(
		c.dynamicClient,
		0, // no resync period
	)

	// Set up informers
	c.nodeInformer = c.coreFactory.Core().V1().Nodes().Informer()
	// For pods, use a filtered informer with label selector
	podInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		0,
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = pcsLabelSelector
		}),
	)
	c.podInformer = podInformerFactory.Core().V1().Pods().Informer()

	c.pcsInformer = c.dynamicFactory.ForResource(pcsGVR).Informer()
	c.ctInformer = c.dynamicFactory.ForResource(clusterTopologyGVR).Informer()

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
	c.pcsInformer.AddEventHandler(handler)
	//nolint:errcheck
	c.ctInformer.AddEventHandler(handler)

	// Start informers
	c.coreFactory.Start(ctx.Done())
	podInformerFactory.Start(ctx.Done())
	c.dynamicFactory.Start(ctx.Done())

	return nil
}

// WaitForSync blocks until all informers have completed their initial list,
// or the context is cancelled. After sync completes, it triggers an immediate
// snapshot rebuild to ensure the snapshot is available right away (without
// waiting for the debounce timer, which may never fire if the cluster is empty).
func (c *InformerTopologyCache) WaitForSync(ctx context.Context) bool {
	synced := cache.WaitForCacheSync(ctx.Done(),
		c.nodeInformer.HasSynced,
		c.podInformer.HasSynced,
		c.pcsInformer.HasSynced,
		c.ctInformer.HasSynced,
	)
	if synced {
		// Build the initial snapshot immediately so callers can read it
		// without waiting for the debounce timer (which requires at least
		// one change event to fire).
		c.rebuildSnapshot()
	}
	return synced
}

// Snapshot returns the latest computed TopologyViewData. Thread-safe.
func (c *InformerTopologyCache) Snapshot() *data.TopologyViewData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

// Updates returns the notification channel.
func (c *InformerTopologyCache) Updates() <-chan struct{} {
	return c.updatesCh
}

// Stop shuts down informers and closes the updates channel.
func (c *InformerTopologyCache) Stop() {
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

// onChange is called by informer event handlers whenever any watched resource changes.
// It resets the debounce timer to coalesce rapid changes into a single snapshot rebuild.
func (c *InformerTopologyCache) onChange() {
	c.debounceMu.Lock()
	defer c.debounceMu.Unlock()

	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
	}
	c.debounceTimer = time.AfterFunc(debounceInterval, func() {
		c.rebuildSnapshot()
	})
}

// rebuildSnapshot reads from all informer caches and builds a new TopologyViewData snapshot.
func (c *InformerTopologyCache) rebuildSnapshot() {
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

	// Read Pods
	pods := c.readPods()

	// Build the topology view snapshot
	snapshot := data.BuildTopologyViewData(levels, pcsSpecs, pods, nodeResult.nodeLabels)

	// Build and attach GPU summary
	gpuSummary := data.BuildGPUSummary(pods, nodeResult.nodeGPUProducts)
	snapshot.GPUSummary = gpuSummary
	snapshot.NodeGPUProducts = nodeResult.nodeGPUProducts

	// Store and notify
	c.mu.Lock()
	c.snapshot = snapshot
	stopped := c.stopped
	c.mu.Unlock()

	if !stopped {
		// Non-blocking send: if the channel already has a pending notification,
		// the TUI will pick it up and see the latest snapshot.
		select {
		case c.updatesCh <- struct{}{}:
		default:
		}
	}
}

// readClusterTopologyLevels reads the ClusterTopology CR from the informer cache.
func (c *InformerTopologyCache) readClusterTopologyLevels() []corev1alpha1.TopologyLevel {
	items := c.ctInformer.GetStore().List()
	for _, item := range items {
		uns, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		var ct corev1alpha1.ClusterTopology
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uns.Object, &ct); err != nil {
			continue
		}
		if ct.Name == corev1alpha1.DefaultClusterTopologyName {
			return ct.Spec.Levels
		}
	}
	return nil
}

// nodeReadResult holds the combined output of readNodeLabels: topology labels and GPU product map.
type nodeReadResult struct {
	nodeLabels      map[string]map[string]string // nodeName -> filtered topology labels
	nodeGPUProducts map[string]string            // nodeName -> short GPU type (e.g. "H200")
}

// gpuProductLabelKey is the node label that identifies the GPU product type.
const gpuProductLabelKey = "nvidia.com/gpu.product"

// readNodeLabels reads all node labels from the informer cache,
// filtering to only topology-relevant keys. Also captures the GPU product
// label from each node and parses it into a short GPU type name.
func (c *InformerTopologyCache) readNodeLabels(topologyKeys map[string]bool) nodeReadResult {
	items := c.nodeInformer.GetStore().List()
	result := nodeReadResult{
		nodeLabels:      make(map[string]map[string]string, len(items)),
		nodeGPUProducts: make(map[string]string, len(items)),
	}

	for _, item := range items {
		obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(item)
		if err != nil {
			continue
		}
		name, _, _ := unstructured.NestedString(obj, "metadata", "name")
		if name == "" {
			continue
		}
		labels, _, _ := unstructured.NestedStringMap(obj, "metadata", "labels")

		filtered := make(map[string]string)
		for k, v := range labels {
			if topologyKeys[k] {
				filtered[k] = v
			}
		}
		result.nodeLabels[name] = filtered

		// Capture GPU product label
		if gpuProduct, ok := labels[gpuProductLabelKey]; ok && gpuProduct != "" {
			shortName := data.ParseGPUProductShortName(gpuProduct)
			if shortName != "" {
				result.nodeGPUProducts[name] = shortName
			}
		}
	}

	return result
}

// readPCSSpecs reads all PodCliqueSet specs from the informer cache.
func (c *InformerTopologyCache) readPCSSpecs() map[string]*corev1alpha1.PodCliqueSet {
	items := c.pcsInformer.GetStore().List()
	result := make(map[string]*corev1alpha1.PodCliqueSet, len(items))

	for _, item := range items {
		uns, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		var pcs corev1alpha1.PodCliqueSet
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uns.Object, &pcs); err != nil {
			continue
		}
		result[pcs.Name] = &pcs
	}

	return result
}

// readPods reads all pods from the informer cache and converts them to TopologyPodInput.
func (c *InformerTopologyCache) readPods() []data.TopologyPodInput {
	items := c.podInformer.GetStore().List()
	result := make([]data.TopologyPodInput, 0, len(items))

	for _, item := range items {
		obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(item)
		if err != nil {
			continue
		}

		name, _, _ := unstructured.NestedString(obj, "metadata", "name")
		namespace, _, _ := unstructured.NestedString(obj, "metadata", "namespace")
		labels, _, _ := unstructured.NestedStringMap(obj, "metadata", "labels")
		nodeName, _, _ := unstructured.NestedString(obj, "spec", "nodeName")
		phase, _, _ := unstructured.NestedString(obj, "status", "phase")

		// Parse GPU requests from all containers
		gpuRequests := parseGPURequests(obj)

		result = append(result, data.TopologyPodInput{
			Namespace:   namespace,
			Name:        name,
			NodeName:    nodeName,
			Phase:       phase,
			Labels:      labels,
			GPURequests: gpuRequests,
		})
	}

	return result
}

// parseGPURequests sums nvidia.com/gpu resource requests across all containers in a pod.
func parseGPURequests(obj map[string]interface{}) int64 {
	containers, found, err := unstructured.NestedSlice(obj, "spec", "containers")
	if err != nil || !found {
		return 0
	}

	var total int64
	for _, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		// Navigate to resources.requests["nvidia.com/gpu"]
		gpuVal, found, err := unstructured.NestedFieldNoCopy(container, "resources", "requests", "nvidia.com/gpu")
		if err != nil || !found || gpuVal == nil {
			continue
		}
		// The value could be a string (quantity) or a number
		switch v := gpuVal.(type) {
		case string:
			// Parse simple integer quantities (e.g. "2", "4")
			var n int64
			for _, ch := range v {
				if ch >= '0' && ch <= '9' {
					n = n*10 + int64(ch-'0')
				} else {
					break
				}
			}
			total += n
		case int64:
			total += v
		case float64:
			total += int64(v)
		}
	}

	return total
}
