package k8s

import (
	"k8s.io/client-go/tools/clientcmd"
)

// KubeConfigInfo holds display-ready metadata extracted from a single kubeconfig load.
type KubeConfigInfo struct {
	ContextName string
	ClusterName string
	UserName    string
	Namespace   string // from context, falls back to "default"
}

// ResolveKubeConfigInfo loads the kubeconfig once and extracts context, cluster,
// user, and namespace. All fields fall back to sensible defaults on error.
func ResolveKubeConfigInfo() KubeConfigInfo {
	info := KubeConfigInfo{
		ContextName: "(unknown)",
		ClusterName: "(unknown)",
		UserName:    "(unknown)",
		Namespace:   "default",
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return info
	}

	if rawConfig.CurrentContext != "" {
		info.ContextName = rawConfig.CurrentContext
	}

	if ctx, ok := rawConfig.Contexts[rawConfig.CurrentContext]; ok {
		if ctx.Cluster != "" {
			info.ClusterName = ctx.Cluster
		}
		if ctx.AuthInfo != "" {
			info.UserName = ctx.AuthInfo
		}
	}

	// Namespace() handles overrides and defaults correctly
	if ns, _, err := clientConfig.Namespace(); err == nil && ns != "" {
		info.Namespace = ns
	}

	return info
}

// ResolveCurrentContext returns the current kubeconfig context name and cluster name.
// Returns "(unknown)" for either value if it cannot be determined (e.g. in-cluster config,
// missing kubeconfig, etc.).
func ResolveCurrentContext() (contextName, clusterName string) {
	info := ResolveKubeConfigInfo()
	return info.ContextName, info.ClusterName
}

// ResolveCurrentUser returns the kubeconfig user (AuthInfo) name for the current context.
// Returns "(unknown)" if unavailable.
func ResolveCurrentUser() string {
	return ResolveKubeConfigInfo().UserName
}

// ResolveCurrentNamespace returns the namespace from the current kubeconfig context,
// falling back to "default" if none is set.
func ResolveCurrentNamespace() (string, error) {
	return ResolveKubeConfigInfo().Namespace, nil
}
