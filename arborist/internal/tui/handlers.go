package tui

import (
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	tea "github.com/charmbracelet/bubbletea"
)

// handleCacheSynced handles CacheSyncedMsg.
// Reads the initial snapshot, populates all tables, and starts listening for updates.
// Any non-fatal warnings from cache startup are surfaced in the error log box.
func (m Model) handleCacheSynced(msg CacheSyncedMsg) (tea.Model, tea.Cmd) {
	debugLogWithContext("global cache synced")
	m.cacheSynced = true

	// Surface any startup warnings in the error log box.
	for _, w := range msg.Warnings {
		debugLogWithContext("cache warning: %s", w)
		m.addError(w)
	}

	if m.cache == nil {
		return m, nil
	}

	m.applySnapshot()

	// Start listening for updates
	return m, waitForCacheUpdateCmd(m.cache)
}

// handleCacheUpdate handles CacheUpdateMsg.
// Reads the new snapshot, rebuilds all visible tables, and re-subscribes.
func (m Model) handleCacheUpdate(_ CacheUpdateMsg) (tea.Model, tea.Cmd) {
	debugLogWithContext("received cache update")

	if m.cache == nil {
		return m, nil
	}

	m.applySnapshot()

	// Re-subscribe for next update
	return m, waitForCacheUpdateCmd(m.cache)
}

// applySnapshot reads the latest snapshot from the cache and rebuilds all data structures.
func (m *Model) applySnapshot() {
	if !m.loadSnapshotData() {
		return
	}
	m.rebuildUIFromData()
}

// loadSnapshotData reads the latest snapshot from the cache and extracts
// topology data into model fields. Returns false when there is no snapshot
// (i.e. the cache hasn't synced yet).
func (m *Model) loadSnapshotData() bool {
	snapshot := m.cache.Snapshot()
	if snapshot == nil {
		return false
	}

	m.cachedSnapshot = snapshot

	// Extract topology view data
	m.topologyViewData = snapshot.TopologyViewData
	if snapshot.TopologyViewData != nil {
		m.gpuSummary = snapshot.TopologyViewData.GPUSummary
	}

	// Rebuild topology info for the currently selected PCS
	if m.viewState.SelectedPodCliqueSet != "" {
		pcsName := m.viewState.SelectedPodCliqueSet
		if pcs, ok := snapshot.PodCliqueSetSpecs[pcsName]; ok {
			topoInfo := clusterstate.BuildTopologyInfo(pcs)
			if snapshot.TopologyViewData != nil {
				topoInfo.DomainToKey = snapshot.TopologyViewData.DomainToKey
			}
			m.cachedTopologyInfo = topoInfo
		}
	}

	return true
}

// rebuildUIFromData rebuilds all UI tables and view state from the cached
// snapshot clusterstate. Call this after loadSnapshotData when the underlying data
// has changed. For navigation-only changes (where the snapshot hasn't changed),
// use rebuildAllFromSnapshot instead.
func (m *Model) rebuildUIFromData() {
	m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
	m.validateTopologyDrillStack()
	m.rebuildResourcesTable()
	m.rebuildEventsTable()
	if m.viewState.ViewType == clusterstate.TopologyView {
		m.rebuildTopologyDomainsTable()
		m.rebuildTopologyPodsTable()
	}
}

// aggregateReadyScheduled accumulates Ready and Scheduled counts from a slice
// of resources by parsing their "ready/total" and "scheduled/total" strings.
func aggregateReadyScheduled(resources []clusterstate.Resource) (ready, replicas, scheduled int) {
	for _, r := range resources {
		var rd, rp int
		fmt.Sscanf(r.Ready, "%d/%d", &rd, &rp)
		ready += rd
		replicas += rp
		var sc, sm int
		fmt.Sscanf(r.Scheduled, "%d/%d", &sc, &sm)
		scheduled += sc
	}
	return
}

// buildPCSGReplicaResources builds the Resource slice for PCSG replicas by
// aggregating Ready/Scheduled counts from PodCliques in each replica.
// defaultTopology is set on each resource (e.g. "N/A" for flat drill-ins, "" for hierarchy).
func buildPCSGReplicaResources(snapshot *clusterstate.CacheSnapshot, pcsgName, namespace, defaultTopology string) []clusterstate.Resource {
	pcsgReplicaIndexes := snapshot.ReplicaIndexesByPCSG[pcsgName]
	if len(pcsgReplicaIndexes) == 0 {
		return nil
	}
	resources := make([]clusterstate.Resource, 0, len(pcsgReplicaIndexes))
	for _, ri := range pcsgReplicaIndexes {
		pcsgReplicaKey := clusterstate.CompositeKey(pcsgName, ri)
		totalReady, totalReplicas, totalScheduled := aggregateReadyScheduled(snapshot.PodCliquesByPCSGReplica[pcsgReplicaKey])
		resources = append(resources, clusterstate.Resource{
			Name:       replicaDisplayName(pcsgName, ri),
			Type:       clusterstate.ResourceTypePCSGReplica,
			Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
			Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
			Namespace:  namespace,
			ParentType: clusterstate.ResourceTypePCSG,
			ParentName: pcsgName,
			Topology:   defaultTopology,
		})
	}
	return resources
}

// snapshotNodeLabels safely returns NodeLabels from the snapshot's TopologyViewData.
// Returns nil if TopologyViewData is not populated.
func snapshotNodeLabels(snapshot *clusterstate.CacheSnapshot) map[string]map[string]string {
	if snapshot == nil || snapshot.TopologyViewData == nil {
		return nil
	}
	return snapshotNodeLabels(snapshot)
}

// resolveResourceTopology resolves topology display for a resource using the
// standard pattern: resolve base topology -> extract domain -> resolve value -> enhance display.
// resolveFn should call the appropriate TopologyInfo method to get the base topology string.
func resolveResourceTopology(
	topoInfo *clusterstate.TopologyInfo,
	resolveFn func() string,
	labelKey string,
	resourceName string,
	podInfos map[string]clusterstate.CachedPodInfo,
	nodeLabels map[string]map[string]string,
) string {
	if topoInfo == nil {
		return "N/A"
	}
	topology := resolveFn()
	domain := clusterstate.ExtractDomain(topology)
	value := clusterstate.ResolveTopologyValue(domain, labelKey, resourceName, topoInfo, podInfos, nodeLabels)
	return clusterstate.EnhanceTopologyDisplay(topology, value)
}

// rebuildAllFromSnapshot rebuilds the full data model from the cached snapshot.
// This is the standard 4-step sequence used after any view/navigation change.
func (m *Model) rebuildAllFromSnapshot() {
	m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
	m.rebuildResourcesTable()
	m.rebuildEventsTable()
}

// buildPCSReplicaResources builds the Resource slice for PCS replicas,
// aggregating Ready/Scheduled from child scaling groups and PodCliques,
// with PCS-level topology resolution.
func buildPCSReplicaResources(
	snapshot *clusterstate.CacheSnapshot,
	topoInfo *clusterstate.TopologyInfo,
	pcsName string,
	namespace string,
) []clusterstate.Resource {
	replicaIndexes := snapshot.ReplicaIndexesByPCS[pcsName]
	if len(replicaIndexes) == 0 {
		return nil
	}

	replicaResources := make([]clusterstate.Resource, 0, len(replicaIndexes))
	for _, ri := range replicaIndexes {
		replicaKey := clusterstate.CompositeKey(pcsName, ri)

		// Calculate aggregate ready/scheduled counts
		sgReady, sgReplicas, sgScheduled := aggregateReadyScheduled(snapshot.ScalingGroupsByReplica[replicaKey])
		pcReady, pcReplicas, pcScheduled := aggregateReadyScheduled(snapshot.PodCliquesByReplica[replicaKey])
		totalReady := sgReady + pcReady
		totalReplicas := sgReplicas + pcReplicas
		totalScheduled := sgScheduled + pcScheduled

		// Resolve topology for replica
		replicaTopology := "N/A"
		if topoInfo != nil && topoInfo.PCSPackDomain != "" {
			replicaTopology = clusterstate.ResolveTopologyDisplay("", topoInfo.PCSPackDomain)
			domain := clusterstate.ExtractDomain(replicaTopology)
			value := clusterstate.ResolveTopologyValueByReplicaIndex(domain, pcsName, ri, topoInfo, snapshot.PodInfos, snapshotNodeLabels(snapshot))
			replicaTopology = clusterstate.EnhanceTopologyDisplay(replicaTopology, value)
		}

		replicaResources = append(replicaResources, clusterstate.Resource{
			Name:       replicaDisplayName(pcsName, ri),
			Type:       clusterstate.ResourceTypePCSReplica,
			Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
			Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
			Namespace:  namespace,
			ParentType: clusterstate.ResourceTypePodCliqueSet,
			ParentName: pcsName,
			Topology:   replicaTopology,
		})
	}
	return replicaResources
}

// buildReplicaChildResources builds PCSGs and standalone PodCliques under a
// specific PCS replica, with per-resource topology resolution.
func buildReplicaChildResources(
	snapshot *clusterstate.CacheSnapshot,
	topoInfo *clusterstate.TopologyInfo,
	pcsName string,
	replicaIndex string,
) []clusterstate.Resource {
	replicaKey := clusterstate.CompositeKey(pcsName, replicaIndex)
	var replicaChildren []clusterstate.Resource

	// PCSGs with topology
	for _, sg := range snapshot.ScalingGroupsByReplica[replicaKey] {
		sg := sg // copy
		pcsgConfigName := clusterstate.ExtractConfigName(sg.Name, pcsName, replicaIndex)
		sg.Topology = resolveResourceTopology(
			topoInfo,
			func() string { return topoInfo.ResolvePCSGTopology(pcsgConfigName) },
			clusterstate.LabelPCSG, sg.Name, snapshot.PodInfos, snapshotNodeLabels(snapshot),
		)
		replicaChildren = append(replicaChildren, sg)
	}

	// Standalone PodCliques with topology
	for _, pc := range snapshot.PodCliquesByReplica[replicaKey] {
		pc := pc // copy
		cliqueTemplateName := clusterstate.ExtractConfigName(pc.Name, pcsName, replicaIndex)
		pc.Topology = resolveResourceTopology(
			topoInfo,
			func() string { return topoInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName) },
			clusterstate.LabelPodClique, pc.Name, snapshot.PodInfos, snapshotNodeLabels(snapshot),
		)
		replicaChildren = append(replicaChildren, pc)
	}

	// Sort for stable table ordering (defense-in-depth; the cache also sorts).
	clusterstate.SortResourcesByName(replicaChildren)
	return replicaChildren
}

// buildPCSGReplicaChildResources builds PodCliques under a specific PCSG replica,
// with topology resolved using both PCSG and clique template context.
func buildPCSGReplicaChildResources(
	snapshot *clusterstate.CacheSnapshot,
	topoInfo *clusterstate.TopologyInfo,
	pcsgName string,
	pcsgReplicaIndex string,
	pcsName string,
	replicaIndex string,
) []clusterstate.Resource {
	pcsgReplicaKey := clusterstate.CompositeKey(pcsgName, pcsgReplicaIndex)
	pcsgConfigName := clusterstate.ExtractConfigName(pcsgName, pcsName, replicaIndex)

	var pcsgChildren []clusterstate.Resource
	for _, pc := range snapshot.PodCliquesByPCSGReplica[pcsgReplicaKey] {
		pc := pc // copy
		cliqueTemplateName := clusterstate.ExtractCliqueTemplateNameFromPCSGChild(pc.Name, pcsgName)
		pc.Topology = resolveResourceTopology(
			topoInfo,
			func() string { return topoInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName) },
			clusterstate.LabelPodClique, pc.Name, snapshot.PodInfos, snapshotNodeLabels(snapshot),
		)
		pcsgChildren = append(pcsgChildren, pc)
	}

	// Sort for stable table ordering (defense-in-depth; the cache also sorts).
	clusterstate.SortResourcesByName(pcsgChildren)
	return pcsgChildren
}

// buildPodChildResources builds Pods under a PodClique with inherited topology
// and per-pod node resolution.
func buildPodChildResources(
	snapshot *clusterstate.CacheSnapshot,
	topoInfo *clusterstate.TopologyInfo,
	pcName string,
	pcsgName string,
	pcsName string,
	replicaIndex string,
) []clusterstate.Resource {
	pods := snapshot.PodsByPodClique[pcName]

	// Set topology on Pods (inherited from parent PodClique)
	basePodTopology := "N/A"
	if topoInfo != nil {
		if pcsgName != "" {
			pcsgConfigName := clusterstate.ExtractConfigName(pcsgName, pcsName, replicaIndex)
			cliqueTemplateName := clusterstate.ExtractCliqueTemplateNameFromPCSGChild(pcName, pcsgName)
			effectiveClique := topoInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
			basePodTopology = clusterstate.WrapInherited(effectiveClique)
		} else {
			cliqueTemplateName := clusterstate.ExtractConfigName(pcName, pcsName, replicaIndex)
			effectiveClique := topoInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
			basePodTopology = clusterstate.WrapInherited(effectiveClique)
		}
	}

	domain := clusterstate.ExtractDomain(basePodTopology)
	podResources := make([]clusterstate.Resource, len(pods))
	for i, pod := range pods {
		podResources[i] = pod
		if domain != "" {
			nodeName := ""
			if cached, ok := snapshot.PodInfos[pod.Name]; ok {
				nodeName = cached.NodeName
			}
			value := clusterstate.ResolveTopologyValueForNode(domain, nodeName, topoInfo, snapshotNodeLabels(snapshot))
			podResources[i].Topology = clusterstate.EnhanceTopologyDisplay(basePodTopology, value)
		} else {
			podResources[i].Topology = basePodTopology
		}
	}

	// Sort for stable table ordering (defense-in-depth; the cache also sorts).
	clusterstate.SortResourcesByName(podResources)
	return podResources
}

// HierarchyBuildParams contains all inputs needed to build the allResources map.
// Extracted as a struct so buildAllResources can be a pure function.
type HierarchyBuildParams struct {
	Snapshot           *clusterstate.CacheSnapshot
	ViewState          clusterstate.ViewState
	TopoInfo           *clusterstate.TopologyInfo
	ForestResourceType string
	AllNamespaces      bool
	Namespace          string
}

// buildAllResources is a pure function that builds the complete allResources map
// from a cache snapshot and view state. Data-building logic is extracted here for
// testability, while the Model method rebuildHierarchyFromSnapshot remains as a
// thin wrapper that assigns the result.
func buildAllResources(p HierarchyBuildParams) map[string][]clusterstate.Resource {
	if p.Snapshot == nil {
		return nil
	}

	result := make(map[string][]clusterstate.Resource)

	// Always build forest based on active resource type
	result["forest"] = buildForestResources(p.Snapshot, p.ForestResourceType, p.AllNamespaces, p.Namespace)

	pcsName := p.ViewState.SelectedPodCliqueSet
	replicaIndex := p.ViewState.SelectedReplicaIndex
	pcsgName := p.ViewState.SelectedScalingGroup
	pcsgReplicaIndex := p.ViewState.SelectedPCSGReplicaIndex
	pcName := p.ViewState.SelectedPodClique

	if pcsName == "" {
		// No PCS context — either at the forest top level, or drilled in from a flat list.
		for k, v := range buildFlatDrillInResources(p.Snapshot, p.ViewState) {
			result[k] = v
		}
		return result
	}

	// Determine namespace from the PCS
	namespace := ""
	if pcs, ok := p.Snapshot.PodCliqueSetSpecs[pcsName]; ok {
		namespace = pcs.Namespace
	}

	// Build PCS replicas
	if replicas := buildPCSReplicaResources(p.Snapshot, p.TopoInfo, pcsName, namespace); len(replicas) > 0 {
		result["PodCliqueSet/"+pcsName] = replicas
	}

	if replicaIndex == "" {
		return result
	}

	// Build replica children (PCSGs + standalone PodCliques)
	result["PodCliqueSetReplica/"+pcsName+"/"+replicaIndex] = buildReplicaChildResources(p.Snapshot, p.TopoInfo, pcsName, replicaIndex)

	if pcsgName == "" && pcName == "" {
		return result
	}

	// PCSG replicas and their children
	if pcsgName != "" {
		if replicas := buildPCSGReplicaResources(p.Snapshot, pcsgName, namespace, ""); len(replicas) > 0 {
			result["PodCliqueScalingGroup/"+pcsgName] = replicas
		}

		if pcsgReplicaIndex != "" {
			result["PodCliqueScalingGroupReplica/"+pcsgName+"/"+pcsgReplicaIndex] = buildPCSGReplicaChildResources(
				p.Snapshot, p.TopoInfo, pcsgName, pcsgReplicaIndex, pcsName, replicaIndex,
			)
		}
	}

	// PodClique children (Pods)
	if pcName != "" {
		result["PodClique/"+pcName] = buildPodChildResources(p.Snapshot, p.TopoInfo, pcName, pcsgName, pcsName, replicaIndex)
	}

	return result
}

// rebuildHierarchyFromSnapshot populates allResources from the cache snapshot
// for the currently selected view and any parent views needed for navigation.
func (m *Model) rebuildHierarchyFromSnapshot(snapshot *clusterstate.CacheSnapshot) {
	result := buildAllResources(HierarchyBuildParams{
		Snapshot:           snapshot,
		ViewState:          m.viewState,
		TopoInfo:           m.cachedTopologyInfo,
		ForestResourceType: m.forestResourceType,
		AllNamespaces:      m.allNamespaces,
		Namespace:          m.namespace,
	})
	if result != nil {
		m.allResources = result
	}
}

// rebuildEventsFromSnapshot populates allEvents from the cache snapshot based on current view.
func (m *Model) rebuildEventsFromSnapshot(snapshot *clusterstate.CacheSnapshot) {
	if snapshot == nil {
		m.allEvents = nil
		return
	}

	m.behavior().RebuildEvents(m, snapshot)
}

// handlePodYAML handles PodYAMLMsg.
func (m Model) handlePodYAML(msg PodYAMLMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading Pod YAML: %v", msg.Err)
		m.podYAMLData[msg.PodName] = fmt.Sprintf("# Error loading Pod YAML: %v", msg.Err)
		m.addError(fmt.Sprintf("Failed to load YAML for Pod %s: %v", msg.PodName, msg.Err))
	} else {
		debugLogWithContext("loaded %d bytes of YAML for Pod %s", len(msg.YAML), msg.PodName)
		m.podYAMLData[msg.PodName] = msg.YAML
	}
	m.updatePodViewport()
	return m, nil
}

// buildFlatDrillInResources returns resources for views reached by drilling from
// a flat forest list (where SelectedPodCliqueSet is empty). Without PCS context,
// the normal hierarchy builder returns early — this fills the gap.
func buildFlatDrillInResources(snapshot *clusterstate.CacheSnapshot, vs clusterstate.ViewState) map[string][]clusterstate.Resource {
	if snapshot == nil {
		return nil
	}

	result := make(map[string][]clusterstate.Resource)
	pcsgName := vs.SelectedScalingGroup
	pcsgReplicaIndex := vs.SelectedPCSGReplicaIndex
	pcName := vs.SelectedPodClique

	// --- PCSG drill-in: build replica list and replica children ---
	if pcsgName != "" {
		// Resolve namespace from any matching PCSG resource in the snapshot
		namespace := ""
		for _, sgs := range snapshot.ScalingGroupsByReplica {
			for _, sg := range sgs {
				if sg.Name == pcsgName {
					namespace = sg.Namespace
					break
				}
			}
			if namespace != "" {
				break
			}
		}

		// PCSG replicas (for PodCliqueScalingGroupView)
		pcsgKey := "PodCliqueScalingGroup/" + pcsgName
		if replicas := buildPCSGReplicaResources(snapshot, pcsgName, namespace, "N/A"); len(replicas) > 0 {
			result[pcsgKey] = replicas
		}

		// PCSG replica children — PodCliques within a specific replica
		if pcsgReplicaIndex != "" {
			pcsgReplicaChildKey := "PodCliqueScalingGroupReplica/" + pcsgName + "/" + pcsgReplicaIndex
			pcsgReplicaKey := clusterstate.CompositeKey(pcsgName, pcsgReplicaIndex)
			var pcsgChildren []clusterstate.Resource
			for _, pc := range snapshot.PodCliquesByPCSGReplica[pcsgReplicaKey] {
				pc := pc // copy
				pc.Topology = "N/A"
				pcsgChildren = append(pcsgChildren, pc)
			}
			clusterstate.SortResourcesByName(pcsgChildren)
			result[pcsgReplicaChildKey] = pcsgChildren
		}
	}

	// --- PodClique drill-in: build pod list ---
	if pcName != "" {
		podCliqueKey := "PodClique/" + pcName
		pods := snapshot.PodsByPodClique[pcName]
		podResources := make([]clusterstate.Resource, len(pods))
		for i, pod := range pods {
			podResources[i] = pod
			podResources[i].Topology = "N/A"
		}
		clusterstate.SortResourcesByName(podResources)
		result[podCliqueKey] = podResources
	}

	return result
}

// rebuildFlatDrillInResources populates allResources from flat drill-in clusterstate.
func (m *Model) rebuildFlatDrillInResources(snapshot *clusterstate.CacheSnapshot) {
	for k, v := range buildFlatDrillInResources(snapshot, m.viewState) {
		m.allResources[k] = v
	}
}

// buildForestResources returns the forest resource list based on the given resource type,
// filtered by namespace if scoping is active.
func buildForestResources(snapshot *clusterstate.CacheSnapshot, forestResourceType string, allNamespaces bool, namespace string) []clusterstate.Resource {
	if snapshot == nil {
		return nil
	}
	var resources []clusterstate.Resource
	switch forestResourceType {
	case "pc":
		resources = flatPodCliques(snapshot)
	case "pcsg":
		resources = flatScalingGroups(snapshot)
	case "pod":
		resources = flatPods(snapshot)
	default: // "pcs" or empty
		resources = snapshot.PodCliqueSets
	}
	return filterByNamespace(resources, allNamespaces, namespace)
}

// populateForestResources sets m.allResources["forest"] from the snapshot.
func (m *Model) populateForestResources(snapshot *clusterstate.CacheSnapshot) {
	m.allResources["forest"] = buildForestResources(snapshot, m.forestResourceType, m.allNamespaces, m.namespace)
}

// filterByNamespace returns the subset of resources matching the given namespace.
// If allNamespaces is true (or namespace is empty), the input is returned unchanged.
func filterByNamespace(resources []clusterstate.Resource, allNamespaces bool, namespace string) []clusterstate.Resource {
	if allNamespaces || namespace == "" {
		return resources
	}
	var filtered []clusterstate.Resource
	for _, r := range resources {
		if r.Namespace == namespace {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// flatPodCliques returns a deduplicated flat list of all PodCliques from the snapshot.
func flatPodCliques(snapshot *clusterstate.CacheSnapshot) []clusterstate.Resource {
	if snapshot == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []clusterstate.Resource

	addPC := func(pc clusterstate.Resource) {
		if !seen[pc.Name] {
			seen[pc.Name] = true
			result = append(result, pc)
		}
	}

	// Standalone PodCliques (directly under PCS replicas)
	for _, pcs := range snapshot.PodCliquesByReplica {
		for _, pc := range pcs {
			addPC(pc)
		}
	}

	// PodCliques under PCSGs
	for _, pcs := range snapshot.PodCliquesByPCSGReplica {
		for _, pc := range pcs {
			addPC(pc)
		}
	}

	clusterstate.SortResourcesByName(result)
	return result
}

// flatScalingGroups returns a deduplicated flat list of all PodCliqueScalingGroups from the snapshot.
func flatScalingGroups(snapshot *clusterstate.CacheSnapshot) []clusterstate.Resource {
	if snapshot == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []clusterstate.Resource

	for _, sgs := range snapshot.ScalingGroupsByReplica {
		for _, sg := range sgs {
			if !seen[sg.Name] {
				seen[sg.Name] = true
				result = append(result, sg)
			}
		}
	}

	clusterstate.SortResourcesByName(result)
	return result
}

// flatPods returns a deduplicated flat list of all Pods from the snapshot.
func flatPods(snapshot *clusterstate.CacheSnapshot) []clusterstate.Resource {
	if snapshot == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []clusterstate.Resource

	for _, pods := range snapshot.PodsByPodClique {
		for _, pod := range pods {
			if !seen[pod.Name] {
				seen[pod.Name] = true
				result = append(result, pod)
			}
		}
	}

	clusterstate.SortResourcesByName(result)
	return result
}

