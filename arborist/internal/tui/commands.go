package tui

import (
	"context"
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
