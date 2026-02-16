package k8s

import (
	"context"
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TopologyCLIData holds the minimal cluster data needed by the
// `arborist topology` CLI command. Fetched via 3 targeted API calls.
type TopologyCLIData struct {
	DomainToKey     map[string]string            // domain → node label key (from ClusterTopology)
	NodeLabels      map[string]map[string]string  // nodeName → topology label key → value
	NodeGPUProducts map[string]string             // nodeName → short GPU type (e.g. "H200")
	NodeGPUCapacity map[string]int64              // nodeName → total GPU count (allocatable)
	AllPods         []clusterstate.TopologyPodInput       // All pods in scope (namespace-scoped or cluster-wide)
}

// FetchTopologyCLIData makes exactly 3 targeted API calls to fetch the minimal
// data needed by the `arborist topology` CLI command:
//  1. ClusterTopology CR → domain-to-key mapping
//  2. Nodes → topology labels, GPU products, GPU capacity
//  3. Pods → all pods in scope (for display + three-way GPU breakdown)
//
// namespace is "" for all-namespaces mode, otherwise scoped to that namespace.
func (k *K8sClient) FetchTopologyCLIData(ctx context.Context, namespace string) (*TopologyCLIData, error) {
	result := &TopologyCLIData{
		DomainToKey:     make(map[string]string),
		NodeLabels:      make(map[string]map[string]string),
		NodeGPUProducts: make(map[string]string),
		NodeGPUCapacity: make(map[string]int64),
	}

	// 1. Fetch ClusterTopology CR
	ctObj, err := k.dynamicClient.Resource(clusterTopologyGVR).Get(ctx, corev1alpha1.DefaultClusterTopologyName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ClusterTopology %q: %w", corev1alpha1.DefaultClusterTopologyName, err)
	}

	var ct corev1alpha1.ClusterTopology
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(ctObj.Object, &ct); err != nil {
		return nil, fmt.Errorf("failed to parse ClusterTopology: %w", err)
	}

	topologyKeys := make(map[string]bool, len(ct.Spec.Levels))
	for _, level := range ct.Spec.Levels {
		result.DomainToKey[string(level.Domain)] = level.Key
		topologyKeys[level.Key] = true
	}

	// 2. Fetch Nodes
	nodeList, err := k.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		name := node.Name

		// Filter labels to topology keys only
		filtered := make(map[string]string)
		for key, val := range node.Labels {
			if topologyKeys[key] {
				filtered[key] = val
			}
		}
		result.NodeLabels[name] = filtered

		// GPU product
		gpuProduct := GPUProductFromNode(node)
		if gpuProduct != "" {
			result.NodeGPUProducts[name] = gpuProduct
		}

		// GPU capacity
		gpuCap := GPUCapacityFromNode(node)
		if gpuCap > 0 {
			result.NodeGPUCapacity[name] = gpuCap
		}
	}

	// 3. Fetch Pods
	podList, err := k.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	result.AllPods = make([]clusterstate.TopologyPodInput, 0, len(podList.Items))
	for i := range podList.Items {
		pod := &podList.Items[i]
		result.AllPods = append(result.AllPods, clusterstate.TopologyPodInput{
			Name:        pod.Name,
			Namespace:   pod.Namespace,
			NodeName:    pod.Spec.NodeName,
			Phase:       string(pod.Status.Phase),
			Labels:      pod.Labels,
			GPURequests: GPURequestsFromPod(pod),
		})
	}

	return result, nil
}
