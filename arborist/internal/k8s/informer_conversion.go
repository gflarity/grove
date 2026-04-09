package k8s

import (
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// toTyped converts an informer store item to a typed pointer. It handles both
// typed informers (direct *T assertion) and dynamic informers (*unstructured.Unstructured
// → FromUnstructured conversion). This eliminates the repeated type-assert +
// FromUnstructured boilerplate that appears across all informer read functions.
func toTyped[T any](item interface{}) (*T, error) {
	if typed, ok := item.(*T); ok {
		return typed, nil
	}
	uns, ok := item.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("expected *%T or *unstructured.Unstructured, got %T", *new(T), item)
	}
	var obj T
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uns.Object, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// GVR definition for ClusterTopology (cluster-scoped CRD).
var clusterTopologyGVR = schema.GroupVersionResource{
	Group:    "grove.io",
	Version:  "v1alpha1",
	Resource: "clustertopologies",
}

// gpuProductLabelKey is the node label that identifies the GPU product type.
const gpuProductLabelKey = "nvidia.com/gpu.product"

// nodeReadResult holds the combined output of readNodeLabelsFromInformer.
type nodeReadResult struct {
	nodeLabels      map[string]map[string]string // nodeName -> filtered topology labels
	nodeGPUProducts map[string]string            // nodeName -> short GPU type (e.g. "H200")
	nodeGPUCapacity map[string]int64             // nodeName -> total GPU count from status.allocatable
}

// readClusterTopologyLevelsFromInformer reads the ClusterTopology CR from the given informer.
// Used by InformerGlobalCache.
func readClusterTopologyLevelsFromInformer(informer cache.SharedIndexInformer) []corev1alpha1.TopologyLevel {
	items := informer.GetStore().List()
	for _, item := range items {
		ct, err := toTyped[corev1alpha1.ClusterTopology](item)
		if err != nil {
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
// Used by InformerGlobalCache.
func readNodeLabelsFromInformer(informer cache.SharedIndexInformer, topologyKeys map[string]bool) nodeReadResult {
	items := informer.GetStore().List()
	result := nodeReadResult{
		nodeLabels:      make(map[string]map[string]string, len(items)),
		nodeGPUProducts: make(map[string]string, len(items)),
		nodeGPUCapacity: make(map[string]int64, len(items)),
	}

	for _, item := range items {
		node, err := toTyped[corev1.Node](item)
		if err != nil {
			continue
		}
		if node.Name == "" {
			continue
		}

		filtered := make(map[string]string)
		for k, v := range node.Labels {
			if topologyKeys[k] {
				filtered[k] = v
			}
		}
		result.nodeLabels[node.Name] = filtered

		gpuProduct := GPUProductFromNode(node)
		if gpuProduct != "" {
			result.nodeGPUProducts[node.Name] = gpuProduct
		}

		gpuCap := GPUCapacityFromNode(node)
		if gpuCap > 0 {
			result.nodeGPUCapacity[node.Name] = gpuCap
		}
	}

	return result
}

// readPCSSpecsFromInformer reads all PodCliqueSet specs from the given informer.
// Used by InformerGlobalCache.
func readPCSSpecsFromInformer(informer cache.SharedIndexInformer) map[string]*corev1alpha1.PodCliqueSet {
	items := informer.GetStore().List()
	result := make(map[string]*corev1alpha1.PodCliqueSet, len(items))

	for _, item := range items {
		pcs, err := toTyped[corev1alpha1.PodCliqueSet](item)
		if err != nil {
			continue
		}
		result[pcs.Name] = pcs
	}

	return result
}

// readPodsFromInformer reads all pods from the given informer and converts them to TopologyPodInput.
// When claims is non-nil, DRA GPU allocations are included in GPURequests.
// Used by InformerGlobalCache.
func readPodsFromInformer(informer cache.SharedIndexInformer, claims map[string]*resourcev1.ResourceClaim) []clusterstate.TopologyPodInput {
	items := informer.GetStore().List()
	result := make([]clusterstate.TopologyPodInput, 0, len(items))

	for _, item := range items {
		pod, err := toTyped[corev1.Pod](item)
		if err != nil {
			continue
		}

		devicePluginGPUs := GPURequestsFromPod(pod)
		draGPUs := DRAGPUCountForPod(pod, claims)
		gpuRequests := devicePluginGPUs
		if draGPUs > gpuRequests {
			gpuRequests = draGPUs
		}

		result = append(result, clusterstate.TopologyPodInput{
			Namespace:   pod.Namespace,
			Name:        pod.Name,
			NodeName:    pod.Spec.NodeName,
			Phase:       string(pod.Status.Phase),
			Labels:      pod.Labels,
			GPURequests: gpuRequests,
		})
	}

	return result
}

// GPURequestsFromPod sums nvidia.com/gpu resource requests across all containers in a pod.
func GPURequestsFromPod(pod *corev1.Pod) int64 {
	var total int64
	for i := range pod.Spec.Containers {
		if qty, ok := pod.Spec.Containers[i].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]; ok {
			total += qty.Value()
		}
	}
	return total
}

// NVIDIADRADriver is the DRA driver name for NVIDIA GPUs.
const NVIDIADRADriver = "gpu.nvidia.com"

// DRAGPUCountForPod counts the number of NVIDIA GPUs allocated via DRA for a pod.
// It resolves pod.Spec.ResourceClaims to actual ResourceClaim objects (via the claims
// map keyed by "namespace/name"), then counts DeviceRequestAllocationResult entries
// with the NVIDIA GPU driver.
//
// For template-based claims, it uses pod.Status.ResourceClaimStatuses to find the
// generated ResourceClaim name. Returns 0 if claims is nil (DRA unavailable).
func DRAGPUCountForPod(pod *corev1.Pod, claims map[string]*resourcev1.ResourceClaim) int64 {
	if len(claims) == 0 || len(pod.Spec.ResourceClaims) == 0 {
		return 0
	}

	// Build lookup for template-based claim name resolution.
	statusByName := make(map[string]string, len(pod.Status.ResourceClaimStatuses))
	for i := range pod.Status.ResourceClaimStatuses {
		s := &pod.Status.ResourceClaimStatuses[i]
		if s.ResourceClaimName != nil {
			statusByName[s.Name] = *s.ResourceClaimName
		}
	}

	var total int64
	for i := range pod.Spec.ResourceClaims {
		prc := &pod.Spec.ResourceClaims[i]

		var claimName string
		if prc.ResourceClaimName != nil {
			claimName = *prc.ResourceClaimName
		} else if prc.ResourceClaimTemplateName != nil {
			claimName = statusByName[prc.Name]
		}
		if claimName == "" {
			continue
		}

		key := pod.Namespace + "/" + claimName
		claim, ok := claims[key]
		if !ok || claim.Status.Allocation == nil {
			continue
		}

		for j := range claim.Status.Allocation.Devices.Results {
			if claim.Status.Allocation.Devices.Results[j].Driver == NVIDIADRADriver {
				total++
			}
		}
	}
	return total
}

// GPUCapacityFromNode reads the nvidia.com/gpu allocatable count from a node.
func GPUCapacityFromNode(node *corev1.Node) int64 {
	if qty, ok := node.Status.Allocatable[corev1.ResourceName("nvidia.com/gpu")]; ok {
		return qty.Value()
	}
	return 0
}

// GPUProductFromNode extracts the short GPU product name from a node's labels.
func GPUProductFromNode(node *corev1.Node) string {
	gpuLabel := node.Labels[gpuProductLabelKey]
	if gpuLabel == "" {
		return ""
	}
	return clusterstate.ParseGPUProductShortName(gpuLabel)
}
