package k8s

import (
	"fmt"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

// convertK8sEventToEvent converts a Kubernetes Event to our data.Event type.
func convertK8sEventToEvent(k8sEvent corev1.Event) data.Event {
	// Calculate age
	age := formatAge(k8sEvent.LastTimestamp.Time)
	if k8sEvent.LastTimestamp.IsZero() {
		age = formatAge(k8sEvent.EventTime.Time)
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

// formatAge formats a time duration into a human-readable age string.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	duration := time.Since(t)

	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	} else {
		return fmt.Sprintf("%dd", int(duration.Hours()/24))
	}
}

// ResolveCurrentContext returns the current kubeconfig context name and cluster name.
// Returns "(unknown)" for either value if it cannot be determined (e.g. in-cluster config,
// missing kubeconfig, etc.).
func ResolveCurrentContext() (contextName, clusterName string) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{},
	).RawConfig()
	if err != nil {
		return "(unknown)", "(unknown)"
	}

	contextName = config.CurrentContext
	if contextName == "" {
		contextName = "(unknown)"
	}

	clusterName = "(unknown)"
	if ctx, ok := config.Contexts[contextName]; ok && ctx.Cluster != "" {
		clusterName = ctx.Cluster
	}

	return contextName, clusterName
}

// ResolveCurrentUser returns the kubeconfig user (AuthInfo) name for the current context.
// Returns "(unknown)" if unavailable.
func ResolveCurrentUser() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{},
	).RawConfig()
	if err != nil {
		return "(unknown)"
	}

	ctx, ok := config.Contexts[config.CurrentContext]
	if !ok || ctx.AuthInfo == "" {
		return "(unknown)"
	}
	return ctx.AuthInfo
}

// ResolveCurrentNamespace returns the namespace from the current kubeconfig context,
// falling back to "default" if none is set.
func ResolveCurrentNamespace() (string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	ns, _, err := kubeConfig.Namespace()
	if err != nil {
		return "default", nil //nolint:nilerr
	}
	if ns == "" {
		return "default", nil
	}
	return ns, nil
}
