package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// startGlobalCacheCmd starts the global cache and waits for initial sync.
// Any non-fatal warnings from startup (e.g. missing CRDs) are collected
// and delivered via CacheSyncedMsg.Warnings.
func startGlobalCacheCmd(cache data.GlobalCache, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("startGlobalCache")

		// Collect warnings from the cache startup via the OnWarning callback.
		// The callback fires synchronously during cache.Start(), so a simple
		// slice is safe (no concurrent access).
		var warnings []string
		if wc, ok := cache.(data.WarningConfigurable); ok {
			wc.SetOnWarning(func(msg string) {
				warnings = append(warnings, msg)
			})
		}

		if err := cache.Start(ctx); err != nil {
			return ErrorMsg{Operation: "startGlobalCache", Err: err}
		}
		syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cache.WaitForSync(syncCtx)
		return CacheSyncedMsg{Warnings: warnings}
	}
}

// waitForCacheUpdateCmd blocks until the cache has a new snapshot.
func waitForCacheUpdateCmd(cache data.GlobalCache) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("waitForCacheUpdate")
		_, ok := <-cache.Updates()
		if !ok {
			return nil // channel closed, cache stopped
		}
		return CacheUpdateMsg{}
	}
}

// loadResourceYAMLCmd creates a command to load any resource's YAML for the YAML overlay.
func loadResourceYAMLCmd(cache data.GlobalCache, ctx context.Context, resourceType, name, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadResourceYAML", "type", resourceType, "name", name, "ns", namespace)
		if cache == nil {
			return ResourceYAMLMsg{ResourceType: resourceType, ResourceName: name, YAML: "# No cache available"}
		}

		yaml, err := cache.GetResourceYAML(ctx, resourceType, name, namespace)
		return ResourceYAMLMsg{ResourceType: resourceType, ResourceName: name, YAML: yaml, Err: err}
	}
}

// loadPodYAMLCmd creates a command to load a Pod's YAML via direct API GET.
// This is the one exception — Pod YAML is large and rarely accessed.
func loadPodYAMLCmd(cache data.GlobalCache, ctx context.Context, podName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPodYAML", "pod", podName, "ns", namespace)
		if cache == nil {
			return PodYAMLMsg{PodName: podName, YAML: "# No cache available"}
		}

		yaml, err := cache.GetPodYAML(ctx, podName, namespace)
		return PodYAMLMsg{PodName: podName, YAML: yaml, Err: err}
	}
}

// loadPodContainersCmd creates a command to load a Pod's container info.
func loadPodContainersCmd(cache data.GlobalCache, ctx context.Context, podName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPodContainers", "pod", podName, "ns", namespace)
		if cache == nil {
			return PodContainersMsg{PodName: podName, Err: fmt.Errorf("no cache available")}
		}

		containers, err := cache.GetPodContainers(ctx, podName, namespace)
		return PodContainersMsg{PodName: podName, Namespace: namespace, Containers: containers, Err: err}
	}
}

// loadPodLogsCmd creates a command to load a pod container's logs.
func loadPodLogsCmd(cache data.GlobalCache, ctx context.Context, podName, namespace, container string, tailLines int64) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPodLogs", "pod", podName, "ns", namespace, "container", container)
		if cache == nil {
			return LogsContentMsg{PodName: podName, Container: container, Err: fmt.Errorf("no cache available")}
		}

		content, err := cache.GetPodLogs(ctx, podName, namespace, container, tailLines)
		return LogsContentMsg{PodName: podName, Container: container, Content: content, Err: err}
	}
}

// logsAutoScrollInterval is how often logs are re-fetched when autoscroll is active.
const logsAutoScrollInterval = 2 * time.Second

// logsAutoScrollTickCmd returns a tea.Tick that fires a logsAutoScrollTickMsg after the interval.
func logsAutoScrollTickCmd() tea.Cmd {
	return tea.Tick(logsAutoScrollInterval, func(time.Time) tea.Msg {
		return logsAutoScrollTickMsg{}
	})
}

// fetchFirstContainerForLogsCmd fetches containers for a pod and returns a LogsRequestMsg
// for the first running container (or first container if none running), or an ErrorMsg.
func fetchFirstContainerForLogsCmd(cache data.GlobalCache, ctx context.Context, podName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("fetchFirstContainerForLogs", "pod", podName, "ns", namespace)
		if cache == nil {
			return ErrorMsg{Operation: "logs", Err: fmt.Errorf("no cache available")}
		}

		containers, err := cache.GetPodContainers(ctx, podName, namespace)
		if err != nil {
			return ErrorMsg{Operation: "logs", Err: fmt.Errorf("failed to get containers: %w", err)}
		}
		if len(containers) == 0 {
			return ErrorMsg{Operation: "logs", Err: fmt.Errorf("no containers in pod %s", podName)}
		}

		// Prefer first running container, fall back to first container
		for _, c := range containers {
			if c.State == "Running" {
				return LogsRequestMsg{PodName: podName, Namespace: namespace, Container: c.Name}
			}
		}
		return LogsRequestMsg{PodName: podName, Namespace: namespace, Container: containers[0].Name}
	}
}

// fetchFirstRunningContainerCmd fetches containers for a pod and returns a ShellRequestMsg
// for the first running container, or an ErrorMsg if none are found.
func fetchFirstRunningContainerCmd(cache data.GlobalCache, ctx context.Context, podName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("fetchFirstRunningContainer", "pod", podName, "ns", namespace)
		if cache == nil {
			return ErrorMsg{Operation: "shell", Err: fmt.Errorf("no cache available")}
		}

		containers, err := cache.GetPodContainers(ctx, podName, namespace)
		if err != nil {
			return ErrorMsg{Operation: "shell", Err: fmt.Errorf("failed to get containers: %w", err)}
		}

		for _, c := range containers {
			if c.State == "Running" {
				return ShellRequestMsg{PodName: podName, Namespace: namespace, Container: c.Name}
			}
		}

		return ErrorMsg{Operation: "shell", Err: fmt.Errorf("no running containers in pod %s", podName)}
	}
}
