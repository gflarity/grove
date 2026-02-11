package tui

import (
	"context"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// loadForestDataCmd creates a command to load all PodCliqueSets.
func loadForestDataCmd(provider data.DataProvider, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadForestData")
		if provider == nil {
			debugLogWithContext("loadForestData: no provider")
			return ForestDataMsg{Resources: []data.Resource{}, Err: nil}
		}

		resources, err := provider.GetAllPodCliqueSets(ctx)
		return ForestDataMsg{Resources: resources, Err: err}
	}
}

// loadReplicasCmd creates a command to load replica data for a PodCliqueSet.
func loadReplicasCmd(provider data.DataProvider, ctx context.Context, pcsName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadReplicas", "pcs", pcsName, "ns", namespace)
		if provider == nil {
			return ReplicaDataMsg{PCSName: pcsName, Namespace: namespace, Err: nil}
		}

		// Get all replica indexes
		replicaIndexes, err := provider.GetReplicaIndexesForPodCliqueSet(ctx, pcsName, namespace)
		if err != nil {
			return ReplicaDataMsg{PCSName: pcsName, Namespace: namespace, Err: err}
		}

		// For each replica, get scaling groups and pod cliques
		scalingGroupsByReplica := make(map[string][]data.Resource)
		podCliquesByReplica := make(map[string][]data.Resource)

		for _, replicaIndex := range replicaIndexes {
			scalingGroups, _ := provider.GetPodCliqueScalingGroupsForPodCliqueSetReplica(ctx, pcsName, namespace, replicaIndex)
			podCliques, _ := provider.GetPodCliquesForPodCliqueSetReplica(ctx, pcsName, namespace, replicaIndex)
			scalingGroupsByReplica[replicaIndex] = scalingGroups
			podCliquesByReplica[replicaIndex] = podCliques
		}

		return ReplicaDataMsg{
			PCSName:                pcsName,
			Namespace:              namespace,
			ReplicaIndexes:         replicaIndexes,
			ScalingGroupsByReplica: scalingGroupsByReplica,
			PodCliquesByReplica:    podCliquesByReplica,
			Err:                    nil,
		}
	}
}

// loadReplicaChildrenCmd creates a command to load children for a specific replica.
func loadReplicaChildrenCmd(provider data.DataProvider, ctx context.Context, pcsName, namespace, replicaIndex string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadReplicaChildren", "pcs", pcsName, "ns", namespace, "replica", replicaIndex)
		if provider == nil {
			return ReplicaChildrenMsg{PCSName: pcsName, Namespace: namespace, ReplicaIndex: replicaIndex}
		}

		scalingGroups, err := provider.GetPodCliqueScalingGroupsForPodCliqueSetReplica(ctx, pcsName, namespace, replicaIndex)
		if err != nil {
			return ReplicaChildrenMsg{PCSName: pcsName, Namespace: namespace, ReplicaIndex: replicaIndex, Err: err}
		}

		podCliques, err := provider.GetPodCliquesForPodCliqueSetReplica(ctx, pcsName, namespace, replicaIndex)
		if err != nil {
			return ReplicaChildrenMsg{PCSName: pcsName, Namespace: namespace, ReplicaIndex: replicaIndex, Err: err}
		}

		return ReplicaChildrenMsg{
			PCSName:       pcsName,
			Namespace:     namespace,
			ReplicaIndex:  replicaIndex,
			ScalingGroups: scalingGroups,
			PodCliques:    podCliques,
			Err:           nil,
		}
	}
}

// loadPCSGChildrenCmd creates a command to load children for a PodCliqueScalingGroup.
func loadPCSGChildrenCmd(provider data.DataProvider, ctx context.Context, pcsgName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPCSGChildren", "pcsg", pcsgName, "ns", namespace)
		if provider == nil {
			return PCSGChildrenMsg{PCSGName: pcsgName, Namespace: namespace}
		}

		podCliques, err := provider.GetPodCliquesForPodCliqueScalingGroup(ctx, pcsgName, namespace)
		return PCSGChildrenMsg{
			PCSGName:   pcsgName,
			Namespace:  namespace,
			PodCliques: podCliques,
			Err:        err,
		}
	}
}

// loadPCSGReplicasCmd creates a command to load PCSG replica data (mirrors loadReplicasCmd for PCS).
func loadPCSGReplicasCmd(provider data.DataProvider, ctx context.Context, pcsgName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPCSGReplicas", "pcsg", pcsgName, "ns", namespace)
		if provider == nil {
			return PCSGReplicaDataMsg{PCSGName: pcsgName, Namespace: namespace, Err: nil}
		}

		replicaIndexes, err := provider.GetReplicaIndexesForPodCliqueScalingGroup(ctx, pcsgName, namespace)
		if err != nil {
			return PCSGReplicaDataMsg{PCSGName: pcsgName, Namespace: namespace, Err: err}
		}

		// For each replica, get PodCliques for aggregation
		podCliquesByReplica := make(map[string][]data.Resource)
		for _, replicaIndex := range replicaIndexes {
			podCliques, _ := provider.GetPodCliquesForPodCliqueScalingGroupReplica(ctx, pcsgName, namespace, replicaIndex)
			podCliquesByReplica[replicaIndex] = podCliques
		}

		return PCSGReplicaDataMsg{
			PCSGName:            pcsgName,
			Namespace:           namespace,
			ReplicaIndexes:      replicaIndexes,
			PodCliquesByReplica: podCliquesByReplica,
			Err:                 nil,
		}
	}
}

// loadPCSGReplicaChildrenCmd creates a command to load children for a specific PCSG replica.
func loadPCSGReplicaChildrenCmd(provider data.DataProvider, ctx context.Context, pcsgName, namespace, replicaIndex string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPCSGReplicaChildren", "pcsg", pcsgName, "ns", namespace, "replica", replicaIndex)
		if provider == nil {
			return PCSGChildrenMsg{PCSGName: pcsgName, Namespace: namespace}
		}

		podCliques, err := provider.GetPodCliquesForPodCliqueScalingGroupReplica(ctx, pcsgName, namespace, replicaIndex)
		return PCSGChildrenMsg{
			PCSGName:   pcsgName,
			Namespace:  namespace,
			PodCliques: podCliques,
			Err:        err,
		}
	}
}

// loadPodCliqueChildrenCmd creates a command to load children (Pods) for a PodClique.
func loadPodCliqueChildrenCmd(provider data.DataProvider, ctx context.Context, pcName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPodCliqueChildren", "pc", pcName, "ns", namespace)
		if provider == nil {
			return PodCliqueChildrenMsg{PodCliqueName: pcName, Namespace: namespace}
		}

		pods, err := provider.GetPodsForPodClique(ctx, pcName, namespace)
		return PodCliqueChildrenMsg{
			PodCliqueName: pcName,
			Namespace:     namespace,
			Pods:          pods,
			Err:           err,
		}
	}
}

// loadEventsForPCSCmd creates a command to load events for a PodCliqueSet.
func loadEventsForPCSCmd(provider data.DataProvider, ctx context.Context, pcsName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadEventsForPCS", "pcs", pcsName, "ns", namespace)
		if provider == nil {
			return EventsMsg{Events: []data.Event{}}
		}

		events, err := provider.GetEventsForPodCliqueSet(ctx, pcsName, namespace)
		return EventsMsg{Events: events, Err: err}
	}
}

// loadEventsForReplicaCmd creates a command to load events for a PodCliqueSet replica.
func loadEventsForReplicaCmd(provider data.DataProvider, ctx context.Context, pcsName, namespace, replicaIndex string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadEventsForReplica", "pcs", pcsName, "ns", namespace, "replica", replicaIndex)
		if provider == nil {
			return EventsMsg{Events: []data.Event{}}
		}

		events, err := provider.GetEventsForPodCliqueSetReplica(ctx, pcsName, namespace, replicaIndex)
		return EventsMsg{Events: events, Err: err}
	}
}

// loadEventsForPCSGCmd creates a command to load events for a PodCliqueScalingGroup.
func loadEventsForPCSGCmd(provider data.DataProvider, ctx context.Context, pcsgName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadEventsForPCSG", "pcsg", pcsgName, "ns", namespace)
		if provider == nil {
			return EventsMsg{Events: []data.Event{}}
		}

		events, err := provider.GetEventsForPodCliqueScalingGroup(ctx, pcsgName, namespace)
		return EventsMsg{Events: events, Err: err}
	}
}

// loadEventsForPCSGReplicaCmd creates a command to load events for a PodCliqueScalingGroup replica.
func loadEventsForPCSGReplicaCmd(provider data.DataProvider, ctx context.Context, pcsgName, namespace, replicaIndex string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadEventsForPCSGReplica", "pcsg", pcsgName, "ns", namespace, "replica", replicaIndex)
		if provider == nil {
			return EventsMsg{Events: []data.Event{}}
		}

		events, err := provider.GetEventsForPodCliqueScalingGroupReplica(ctx, pcsgName, namespace, replicaIndex)
		return EventsMsg{Events: events, Err: err}
	}
}

// loadEventsForPodCliqueCmd creates a command to load events for a PodClique.
func loadEventsForPodCliqueCmd(provider data.DataProvider, ctx context.Context, pcName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadEventsForPodClique", "pc", pcName, "ns", namespace)
		if provider == nil {
			return EventsMsg{Events: []data.Event{}}
		}

		events, err := provider.GetEventsForPodClique(ctx, pcName, namespace)
		return EventsMsg{Events: events, Err: err}
	}
}

// loadPodYAMLCmd creates a command to load a Pod's YAML.
func loadPodYAMLCmd(provider data.DataProvider, ctx context.Context, podName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPodYAML", "pod", podName, "ns", namespace)
		if provider == nil {
			return PodYAMLMsg{PodName: podName, YAML: "# No provider available"}
		}

		yaml, err := provider.GetPodYAML(ctx, podName, namespace)
		return PodYAMLMsg{PodName: podName, YAML: yaml, Err: err}
	}
}

// loadTopologyInfoCmd creates a command to load topology info for a PodCliqueSet.
func loadTopologyInfoCmd(provider data.DataProvider, ctx context.Context, pcsName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadTopologyInfo", "pcs", pcsName, "ns", namespace)
		if provider == nil {
			return TopologyInfoMsg{PCSName: pcsName, Namespace: namespace}
		}

		// Use a short timeout to avoid blocking the UI
		topoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// Get the PodCliqueSet spec
		pcs, err := provider.GetPodCliqueSet(topoCtx, pcsName, namespace)
		if err != nil {
			return TopologyInfoMsg{PCSName: pcsName, Namespace: namespace, Err: err}
		}

		// Build topology info from the PCS spec
		topologyInfo := data.BuildTopologyInfo(pcs)

		// Fetch ClusterTopology for domain->key mapping
		ct, err := provider.GetClusterTopology(topoCtx)
		if err == nil && ct != nil && topologyInfo != nil {
			for _, level := range ct.Spec.Levels {
				topologyInfo.DomainToKey[string(level.Domain)] = level.Key
			}
			debugLogWithContext("loaded ClusterTopology domain->key mappings: %v", topologyInfo.DomainToKey)
		}

		return TopologyInfoMsg{
			PCSName:      pcsName,
			Namespace:    namespace,
			TopologyInfo: topologyInfo,
			Err:          nil,
		}
	}
}

// loadPodInfoCmd creates a command to load cached pod info for a PodCliqueSet.
func loadPodInfoCmd(provider data.DataProvider, ctx context.Context, pcsName, namespace string) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadPodInfo", "pcs", pcsName, "ns", namespace)
		if provider == nil {
			return PodInfoMsg{PCSName: pcsName, Namespace: namespace}
		}

		topoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		podInfos, err := provider.GetPodInfoForPCS(topoCtx, pcsName, namespace)
		return PodInfoMsg{
			PCSName:   pcsName,
			Namespace: namespace,
			PodInfos:  podInfos,
			Err:       err,
		}
	}
}

// startTopologyCacheCmd starts the topology cache and waits for initial sync.
func startTopologyCacheCmd(cache data.TopologyCache, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("startTopologyCache")
		if err := cache.Start(ctx); err != nil {
			return ErrorMsg{Operation: "startTopologyCache", Err: err}
		}
		syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cache.WaitForSync(syncCtx)
		return TopologyCacheSyncedMsg{}
	}
}

// waitForTopologyCacheUpdateCmd blocks until the cache has a new snapshot.
func waitForTopologyCacheUpdateCmd(cache data.TopologyCache) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("waitForTopologyCacheUpdate")
		_, ok := <-cache.Updates()
		if !ok {
			return nil // channel closed, cache stopped
		}
		return TopologyViewDataMsg{Data: cache.Snapshot()}
	}
}

// loadNodeLabelsCmd creates a command to load node topology labels.
func loadNodeLabelsCmd(provider data.DataProvider, ctx context.Context, topologyInfo *data.TopologyInfo) tea.Cmd {
	return func() tea.Msg {
		debugLogCmd("loadNodeLabels")
		if provider == nil || topologyInfo == nil || len(topologyInfo.DomainToKey) == 0 {
			return NodeLabelsMsg{}
		}

		topoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		topologyKeys := make([]string, 0, len(topologyInfo.DomainToKey))
		for _, key := range topologyInfo.DomainToKey {
			topologyKeys = append(topologyKeys, key)
		}

		nodeLabels, err := provider.GetAllNodeLabels(topoCtx, topologyKeys)
		return NodeLabelsMsg{
			NodeLabels: nodeLabels,
			Err:        err,
		}
	}
}
