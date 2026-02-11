package k8s

import (
	"github.com/ai-dynamo/grove/arborist/internal/data"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// readClusterTopologyLevelsFromInformer reads the ClusterTopology CR from the given informer.
// Shared by InformerGlobalCache and InformerTopologyCache.
func readClusterTopologyLevelsFromInformer(informer cache.SharedIndexInformer) []corev1alpha1.TopologyLevel {
	items := informer.GetStore().List()
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

// readNodeLabelsFromInformer reads all node labels from the given informer,
// filtering to only topology-relevant keys. Also captures GPU product labels
// and GPU capacity per node.
// Shared by InformerGlobalCache and InformerTopologyCache.
func readNodeLabelsFromInformer(informer cache.SharedIndexInformer, topologyKeys map[string]bool) nodeReadResult {
	items := informer.GetStore().List()
	result := nodeReadResult{
		nodeLabels:      make(map[string]map[string]string, len(items)),
		nodeGPUProducts: make(map[string]string, len(items)),
		nodeGPUCapacity: make(map[string]int64, len(items)),
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

		if gpuProduct, ok := labels[gpuProductLabelKey]; ok && gpuProduct != "" {
			shortName := data.ParseGPUProductShortName(gpuProduct)
			if shortName != "" {
				result.nodeGPUProducts[name] = shortName
			}
		}

		gpuCap := parseNodeGPUCapacity(obj)
		if gpuCap > 0 {
			result.nodeGPUCapacity[name] = gpuCap
		}
	}

	return result
}

// readPCSSpecsFromInformer reads all PodCliqueSet specs from the given informer.
// Shared by InformerGlobalCache and InformerTopologyCache.
func readPCSSpecsFromInformer(informer cache.SharedIndexInformer) map[string]*corev1alpha1.PodCliqueSet {
	items := informer.GetStore().List()
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

// readPodsFromInformer reads all pods from the given informer and converts them to TopologyPodInput.
// Shared by InformerGlobalCache and InformerTopologyCache.
func readPodsFromInformer(informer cache.SharedIndexInformer) []data.TopologyPodInput {
	items := informer.GetStore().List()
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
