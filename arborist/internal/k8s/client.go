package k8s

import (
	"context"
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// TopologyCLIData holds the minimal cluster data needed by the
// `arborist topology` CLI command. Fetched via 3 targeted API calls.
type TopologyCLIData struct {
	DomainToKey     map[string]string            // domain → node label key (from ClusterTopology)
	NodeLabels      map[string]map[string]string  // nodeName → topology label key → value
	NodeGPUProducts map[string]string             // nodeName → short GPU type (e.g. "H200")
	NodeGPUCapacity map[string]int64              // nodeName → total GPU count (allocatable)
	AllPods         []TopologyCLIPod              // All pods in scope (namespace-scoped or cluster-wide)
}

// TopologyCLIPod holds the minimal per-pod info needed by the CLI.
type TopologyCLIPod struct {
	Name        string
	Namespace   string
	NodeName    string
	Labels      map[string]string
	GPURequests int64 // total nvidia.com/gpu requests (0 if none)
}

// K8sClient wraps the Kubernetes clients
type K8sClient struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
	restConfig    *rest.Config
}

// NewK8sClient creates a new Kubernetes client using the default kubeconfig
func NewK8sClient() (*K8sClient, error) {
	// Try to use in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &K8sClient{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		restConfig:    config,
	}, nil
}

// Clientset returns the underlying kubernetes.Interface clientset.
func (k *K8sClient) Clientset() kubernetes.Interface {
	return k.clientset
}

// DynamicClient returns the underlying dynamic.Interface client.
func (k *K8sClient) DynamicClient() dynamic.Interface {
	return k.dynamicClient
}

// NewGlobalCache creates a new InformerGlobalCache using this client's
// clientset and dynamic client. The caller is responsible for calling Start()
// and Stop() on the returned cache.
func (k *K8sClient) NewGlobalCache(opts ...GlobalCacheOption) data.GlobalCache {
	return NewInformerGlobalCache(k.clientset, k.dynamicClient, opts...)
}

// GetServerVersion returns the Kubernetes server version string (e.g. "v1.33.5+k3s1").
// Returns "(unknown)" if the version cannot be determined.
func (k *K8sClient) GetServerVersion() string {
	info, err := k.clientset.Discovery().ServerVersion()
	if err != nil {
		return "(unknown)"
	}
	return info.GitVersion
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
		gpuProduct := gpuProductFromNode(node)
		if gpuProduct != "" {
			result.NodeGPUProducts[name] = gpuProduct
		}

		// GPU capacity
		gpuCap := gpuCapacityFromNode(node)
		if gpuCap > 0 {
			result.NodeGPUCapacity[name] = gpuCap
		}
	}

	// 3. Fetch Pods
	podList, err := k.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	result.AllPods = make([]TopologyCLIPod, 0, len(podList.Items))
	for i := range podList.Items {
		pod := &podList.Items[i]
		result.AllPods = append(result.AllPods, TopologyCLIPod{
			Name:        pod.Name,
			Namespace:   pod.Namespace,
			NodeName:    pod.Spec.NodeName,
			Labels:      pod.Labels,
			GPURequests: gpuRequestsFromPod(pod),
		})
	}

	return result, nil
}

// gpuRequestsFromPod sums nvidia.com/gpu resource requests across all containers in a pod.
func gpuRequestsFromPod(pod *corev1.Pod) int64 {
	var total int64
	for i := range pod.Spec.Containers {
		if qty, ok := pod.Spec.Containers[i].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]; ok {
			total += qty.Value()
		}
	}
	return total
}

// gpuCapacityFromNode reads the nvidia.com/gpu allocatable count from a node.
func gpuCapacityFromNode(node *corev1.Node) int64 {
	if qty, ok := node.Status.Allocatable[corev1.ResourceName("nvidia.com/gpu")]; ok {
		return qty.Value()
	}
	return 0
}

// gpuProductFromNode extracts the short GPU product name from a node's labels.
func gpuProductFromNode(node *corev1.Node) string {
	gpuLabel := node.Labels[gpuProductLabelKey]
	if gpuLabel == "" {
		return ""
	}
	return data.ParseGPUProductShortName(gpuLabel)
}
