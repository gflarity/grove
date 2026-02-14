package k8s

import (
	"github.com/ai-dynamo/grove/arborist/internal/data"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

// convertK8sEventToEvent converts a Kubernetes Event to our data.Event type.
func convertK8sEventToEvent(k8sEvent corev1.Event) data.Event {
	// Calculate age
	age := data.FormatAge(k8sEvent.LastTimestamp.Time)
	if k8sEvent.LastTimestamp.IsZero() {
		age = data.FormatAge(k8sEvent.EventTime.Time)
	}

	return data.Event{
		Type:      k8sEvent.Type,
		Kind:      k8sEvent.InvolvedObject.Kind,
		Reason:    k8sEvent.Reason,
		Age:       age,
		From:      k8sEvent.Source.Component,
		Message:   k8sEvent.Message,
		Parent:    k8sEvent.InvolvedObject.Name,
		Timestamp: k8sEvent.LastTimestamp.Time,
	}
}

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
