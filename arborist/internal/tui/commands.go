package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	tea "github.com/charmbracelet/bubbletea"
)

// =============================================================================
// Command Mode (vim-style ":" view switching with autocomplete)
// =============================================================================

type viewCommand struct {
	Name string
}

// viewCommands is the full list for 'v' (View) mode — includes forest, topology,
// and all resource type shortcuts.
var viewCommands = []viewCommand{
	{Name: "forest"},
	{Name: "topology"},
	{Name: "pcs"},
	{Name: "podcliqueset"},
	{Name: "pc"},
	{Name: "podclique"},
	{Name: "pcsg"},
	{Name: "podcliquescalinggroup"},
	{Name: "pod"},
}

// commandModeCommands is the restricted list for ':' (Command) mode — resource
// types only, no "forest" or "topology" (those have dedicated keys: esc/v and t).
var commandModeCommands = []viewCommand{
	{Name: "pcs"},
	{Name: "podcliqueset"},
	{Name: "pc"},
	{Name: "podclique"},
	{Name: "pcsg"},
	{Name: "podcliquescalinggroup"},
	{Name: "pod"},
}

// ViewCommandNames returns the list of available view command names (full set).
func ViewCommandNames() []string {
	names := make([]string, len(viewCommands))
	for i, c := range viewCommands {
		names[i] = c.Name
	}
	return names
}

// CommandModeNames returns the list of command names for ':' command mode (resource types only).
func CommandModeNames() []string {
	names := make([]string, len(commandModeCommands))
	for i, c := range commandModeCommands {
		names[i] = c.Name
	}
	return names
}

func (m Model) executeCommand(input string, commands []viewCommand, ac *Autocompleter) (tea.Model, tea.Cmd) {
	debugLogWithContext("executeCommand: input=%q currentView=%s commandActive=%v viewEditActive=%v",
		input, clusterstate.ViewTypeName(m.viewState.ViewType), m.commandActive, m.viewEditActive)

	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return m, nil
	}

	matched := ""
	for _, c := range commands {
		if c.Name == input {
			matched = c.Name
			break
		}
	}
	if matched == "" {
		if ac != nil {
			if name, ok := ac.UniqueMatch(input); ok {
				matched = name
			}
		}
	}
	if matched == "" {
		debugLogWithContext("executeCommand: no match for %q", input)
		return m, nil
	}

	debugLogWithContext("executeCommand: executing %q (matched %q)", input, matched)

	// Normalize long forms to short forms for resource type commands
	normalized := normalizeResourceType(matched)

	switch matched {
	case "forest":
		// "forest" is equivalent to ":pcs"
		normalized = "pcs"
		m.switchToForestResourceType(normalized)
		return m, nil

	case "topology":
		// Guard: if not currently in topology view and topology is unavailable, log error
		if m.viewState.ViewType != clusterstate.TopologyView && !m.topologyAvailable() {
			m.addError("Topology unavailable — no ClusterTopology resource found")
			debugLogWithContext("executeCommand: topology unavailable, staying in current view")
			return m, nil
		}
		return m.toggleTopologyView()

	case "pcs", "podcliqueset", "pc", "podclique", "pcsg", "podcliquescalinggroup", "pod":
		m.switchToForestResourceType(normalized)
		return m, nil
	}

	return m, nil
}

// switchToForestResourceType switches to ForestView with the given resource type,
// clearing drill state but preserving the filter.
func (m *Model) switchToForestResourceType(rt string) {
	m.forestResourceType = rt
	m.viewState.ViewType = clusterstate.ForestView
	m.viewState.SelectedPodCliqueSet = ""
	m.viewState.SelectedReplicaIndex = ""
	m.viewState.SelectedScalingGroup = ""
	m.viewState.SelectedPodClique = ""
	m.viewState.SelectedPod = ""
	m.cachedTopologyInfo = nil
	m.activePane = clusterstate.ResourcesPane
	m.updateTableFocus()
	m.rebuildAllFromSnapshot()
}

// =============================================================================
// Bubble Tea Commands (tea.Cmd factories)
// =============================================================================

// startGlobalCacheCmd starts the global cache and waits for initial sync.
// Any non-fatal warnings from startup (e.g. missing CRDs) are collected
// and delivered via CacheSyncedMsg.Warnings.
func startGlobalCacheCmd(ctx context.Context, cache clusterstate.GlobalCache) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("startGlobalCache")

		// Collect warnings from the cache startup via the OnWarning callback.
		// The callback fires synchronously during cache.Start(), so a simple
		// slice is safe (no concurrent access).
		var warnings []string
		if wc, ok := cache.(clusterstate.WarningConfigurable); ok {
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
func waitForCacheUpdateCmd(cache clusterstate.GlobalCache) tea.Cmd {
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
func loadResourceYAMLCmd(ctx context.Context, cache clusterstate.GlobalCache, resourceType, name, namespace string) tea.Cmd {
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
func loadPodYAMLCmd(ctx context.Context, cache clusterstate.GlobalCache, podName, namespace string) tea.Cmd {
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
func loadPodContainersCmd(ctx context.Context, cache clusterstate.GlobalCache, podName, namespace string) tea.Cmd {
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
func loadPodLogsCmd(ctx context.Context, cache clusterstate.GlobalCache, podName, namespace, container string, tailLines int64) tea.Cmd {
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
func fetchFirstContainerForLogsCmd(ctx context.Context, cache clusterstate.GlobalCache, podName, namespace string) tea.Cmd {
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
func fetchFirstRunningContainerCmd(ctx context.Context, cache clusterstate.GlobalCache, podName, namespace string) tea.Cmd {
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
