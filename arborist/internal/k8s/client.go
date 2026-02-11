package k8s

import (
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sClient wraps the Kubernetes clients
type K8sClient struct {
	clientset     *kubernetes.Clientset
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

// NewGlobalCache creates a new InformerGlobalCache using this client's
// clientset and dynamic client. The caller is responsible for calling Start()
// and Stop() on the returned cache.
func (k *K8sClient) NewGlobalCache() data.GlobalCache {
	return NewInformerGlobalCache(k.clientset, k.dynamicClient)
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
