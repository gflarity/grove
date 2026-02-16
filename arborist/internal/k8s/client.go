package k8s

import (
	"context"
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

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
func (k *K8sClient) NewGlobalCache(opts ...GlobalCacheOption) clusterstate.GlobalCache {
	return NewInformerGlobalCache(k.clientset, k.dynamicClient, opts...)
}

// CheckGroveCRDs verifies that the core Grove CRDs (PodCliqueSet, PodCliqueScalingGroup,
// PodClique) are registered on the API server. Returns a list of missing resource names,
// or nil if all are present.
func (k *K8sClient) CheckGroveCRDs(ctx context.Context) []string {
	required := []string{"podcliquesets", "podcliquescalinggroups", "podcliques"}
	if ctx.Err() != nil {
		return required
	}
	resourceList, err := k.clientset.Discovery().ServerResourcesForGroupVersion("grove.io/v1alpha1")
	if err != nil {
		// API group not found at all — all CRDs missing
		return required
	}
	found := make(map[string]bool)
	for _, r := range resourceList.APIResources {
		found[r.Name] = true
	}
	var missing []string
	for _, name := range required {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// GetServerVersion returns the Kubernetes server version string (e.g. "v1.33.5+k3s1").
// Returns "(unknown)" if the version cannot be determined.
func (k *K8sClient) GetServerVersion(ctx context.Context) string {
	if ctx.Err() != nil {
		return "(unknown)"
	}
	info, err := k.clientset.Discovery().ServerVersion()
	if err != nil {
		return "(unknown)"
	}
	return info.GitVersion
}
