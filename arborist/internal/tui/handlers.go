package tui

import (
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// handleCacheSynced handles CacheSyncedMsg.
// Reads the initial snapshot, populates all tables, and starts listening for updates.
func (m Model) handleCacheSynced(_ CacheSyncedMsg) (tea.Model, tea.Cmd) {
	debugLogWithContext("global cache synced")
	m.cacheSynced = true

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
	snapshot := m.cache.Snapshot()
	if snapshot == nil {
		return
	}

	m.cachedSnapshot = snapshot

	// Extract topology view data
	m.topologyViewData = snapshot.TopologyViewData
	m.gpuSummary = snapshot.GPUSummary

	// Rebuild forest data based on the active resource type
	m.populateForestResources(snapshot)

	// Rebuild topology info for the currently selected PCS
	if m.viewState.SelectedPodCliqueSet != "" {
		pcsName := m.viewState.SelectedPodCliqueSet
		if pcs, ok := snapshot.PodCliqueSetSpecs[pcsName]; ok {
			topoInfo := data.BuildTopologyInfo(pcs)
			// Populate DomainToKey from topology view data
			if snapshot.TopologyViewData != nil {
				topoInfo.DomainToKey = snapshot.TopologyViewData.DomainToKey
			}
			m.cachedTopologyInfo = topoInfo
		}
	}

	// Rebuild hierarchy data from snapshot for the current view
	m.rebuildHierarchyFromSnapshot(snapshot)

	// Rebuild events for current view
	m.rebuildEventsFromSnapshot(snapshot)

	// Validate drill stack against new data
	m.validateTopologyDrillStack()

	// Rebuild all tables
	m.rebuildResourcesTable()
	m.rebuildEventsTable()
	if m.viewState.ViewType == data.TopologyView {
		m.rebuildTopologyDomainsTable()
		m.rebuildTopologyPodsTable()
	}
}

// rebuildHierarchyFromSnapshot populates allResources from the cache snapshot
// for the currently selected view and any parent views needed for navigation.
func (m *Model) rebuildHierarchyFromSnapshot(snapshot *data.CacheSnapshot) {
	if snapshot == nil {
		return
	}

	pcsName := m.viewState.SelectedPodCliqueSet
	replicaIndex := m.viewState.SelectedReplicaIndex
	pcsgName := m.viewState.SelectedScalingGroup
	pcsgReplicaIndex := m.viewState.SelectedPCSGReplicaIndex
	pcName := m.viewState.SelectedPodClique

	// Always rebuild forest based on active resource type
	m.populateForestResources(snapshot)

	if pcsName == "" {
		// No PCS context — either at the forest top level, or drilled in from a flat list.
		// Build only the resources needed for the current flat-list drill-in view.
		m.rebuildFlatDrillInResources(snapshot)
		return
	}

	// Determine namespace from the PCS
	namespace := ""
	if pcs, ok := snapshot.PodCliqueSetSpecs[pcsName]; ok {
		namespace = pcs.Namespace
	}

	// Build PCS replicas
	replicaIndexes := snapshot.ReplicaIndexesByPCS[pcsName]
	pcsKey := "PodCliqueSet/" + pcsName

	if len(replicaIndexes) > 0 {
		replicaResources := make([]data.Resource, 0, len(replicaIndexes))
		for _, ri := range replicaIndexes {
			replicaKey := pcsName + "/" + ri

			// Calculate aggregate ready/scheduled counts
			var totalReady, totalScheduled, totalReplicas int
			for _, sg := range snapshot.ScalingGroupsByReplica[replicaKey] {
				var ready, replicas int
				fmt.Sscanf(sg.Ready, "%d/%d", &ready, &replicas)
				totalReady += ready
				totalReplicas += replicas
				var scheduled, scheduledMax int
				fmt.Sscanf(sg.Scheduled, "%d/%d", &scheduled, &scheduledMax)
				totalScheduled += scheduled
			}
			for _, pc := range snapshot.PodCliquesByReplica[replicaKey] {
				var ready, replicas int
				fmt.Sscanf(pc.Ready, "%d/%d", &ready, &replicas)
				totalReady += ready
				totalReplicas += replicas
				var scheduled, scheduledMax int
				fmt.Sscanf(pc.Scheduled, "%d/%d", &scheduled, &scheduledMax)
				totalScheduled += scheduled
			}

			// Resolve topology for replica
			replicaTopology := "N/A"
			if m.cachedTopologyInfo != nil && m.cachedTopologyInfo.PCSPackDomain != "" {
				replicaTopology = data.ResolveTopologyDisplay("", m.cachedTopologyInfo.PCSPackDomain)
				domain := data.ExtractDomain(replicaTopology)
				value := data.ResolveTopologyValueByReplicaIndex(domain, pcsName, ri, m.cachedTopologyInfo, snapshot.PodInfos, snapshot.NodeLabels)
				replicaTopology = data.EnhanceTopologyDisplay(replicaTopology, value)
			}

			replicaResources = append(replicaResources, data.Resource{
				Name:       fmt.Sprintf("%s-replica-%s", pcsName, ri),
				Type:       "PodCliqueSetReplica",
				Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
				Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
				Namespace:  namespace,
				ParentType: "PodCliqueSet",
				ParentName: pcsName,
				Topology:   replicaTopology,
			})
		}
		m.allResources[pcsKey] = replicaResources
	}

	if replicaIndex == "" {
		return
	}

	// Build replica children (PCSGs + standalone PodCliques)
	replicaChildKey := "PodCliqueSetReplica/" + pcsName + "/" + replicaIndex
	replicaKey := pcsName + "/" + replicaIndex

	var replicaChildren []data.Resource

	// PCSGs with topology
	for _, sg := range snapshot.ScalingGroupsByReplica[replicaKey] {
		sg := sg // copy
		pcsgConfigName := data.ExtractConfigName(sg.Name, pcsName, replicaIndex)
		if m.cachedTopologyInfo != nil {
			sg.Topology = m.cachedTopologyInfo.ResolvePCSGTopology(pcsgConfigName)
			domain := data.ExtractDomain(sg.Topology)
			value := data.ResolveTopologyValue(domain, "grove.io/podcliquescalinggroup", sg.Name, m.cachedTopologyInfo, snapshot.PodInfos, snapshot.NodeLabels)
			sg.Topology = data.EnhanceTopologyDisplay(sg.Topology, value)
		} else {
			sg.Topology = "N/A"
		}
		replicaChildren = append(replicaChildren, sg)
	}

	// Standalone PodCliques with topology
	for _, pc := range snapshot.PodCliquesByReplica[replicaKey] {
		pc := pc // copy
		cliqueTemplateName := data.ExtractConfigName(pc.Name, pcsName, replicaIndex)
		if m.cachedTopologyInfo != nil {
			pc.Topology = m.cachedTopologyInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
			domain := data.ExtractDomain(pc.Topology)
			value := data.ResolveTopologyValue(domain, "grove.io/podclique", pc.Name, m.cachedTopologyInfo, snapshot.PodInfos, snapshot.NodeLabels)
			pc.Topology = data.EnhanceTopologyDisplay(pc.Topology, value)
		} else {
			pc.Topology = "N/A"
		}
		replicaChildren = append(replicaChildren, pc)
	}
	// Sort for stable table ordering (defense-in-depth; the cache also sorts).
	data.SortResourcesByName(replicaChildren)
	m.allResources[replicaChildKey] = replicaChildren

	if pcsgName == "" && pcName == "" {
		return
	}

	// PCSG replicas
	if pcsgName != "" {
		pcsgKey := "PodCliqueScalingGroup/" + pcsgName
		pcsgReplicaIndexes := snapshot.ReplicaIndexesByPCSG[pcsgName]
		if len(pcsgReplicaIndexes) > 0 {
			pcsgReplicaResources := make([]data.Resource, 0, len(pcsgReplicaIndexes))
			for _, ri := range pcsgReplicaIndexes {
				var totalReady, totalScheduled, totalReplicas int
				pcsgReplicaKey := pcsgName + "/" + ri
				for _, pc := range snapshot.PodCliquesByPCSGReplica[pcsgReplicaKey] {
					var ready, replicas int
					fmt.Sscanf(pc.Ready, "%d/%d", &ready, &replicas)
					totalReady += ready
					totalReplicas += replicas
					var scheduled, scheduledMax int
					fmt.Sscanf(pc.Scheduled, "%d/%d", &scheduled, &scheduledMax)
					totalScheduled += scheduled
				}
				pcsgReplicaResources = append(pcsgReplicaResources, data.Resource{
					Name:       fmt.Sprintf("%s-replica-%s", pcsgName, ri),
					Type:       "PodCliqueScalingGroupReplica",
					Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
					Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
					Namespace:  namespace,
					ParentType: "PodCliqueScalingGroup",
					ParentName: pcsgName,
				})
			}
			m.allResources[pcsgKey] = pcsgReplicaResources
		}

		// PCSG replica children (PodCliques in a specific PCSG replica)
		if pcsgReplicaIndex != "" {
			pcsgReplicaChildKey := "PodCliqueScalingGroupReplica/" + pcsgName + "/" + pcsgReplicaIndex
			pcsgReplicaKey := pcsgName + "/" + pcsgReplicaIndex
			pcsgConfigName := data.ExtractConfigName(pcsgName, pcsName, replicaIndex)

			var pcsgChildren []data.Resource
			for _, pc := range snapshot.PodCliquesByPCSGReplica[pcsgReplicaKey] {
				pc := pc // copy
				cliqueTemplateName := data.ExtractCliqueTemplateNameFromPCSGChild(pc.Name, pcsgName)
				if m.cachedTopologyInfo != nil {
					pc.Topology = m.cachedTopologyInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
					domain := data.ExtractDomain(pc.Topology)
					value := data.ResolveTopologyValue(domain, "grove.io/podclique", pc.Name, m.cachedTopologyInfo, snapshot.PodInfos, snapshot.NodeLabels)
					pc.Topology = data.EnhanceTopologyDisplay(pc.Topology, value)
				} else {
					pc.Topology = "N/A"
				}
				pcsgChildren = append(pcsgChildren, pc)
			}
		// Sort for stable table ordering (defense-in-depth; the cache also sorts).
		data.SortResourcesByName(pcsgChildren)
		m.allResources[pcsgReplicaChildKey] = pcsgChildren
		}
	}

	// PodClique children (Pods)
	if pcName != "" {
		podCliqueKey := "PodClique/" + pcName
		pods := snapshot.PodsByPodClique[pcName]

		// Set topology on Pods (inherited from parent PodClique)
		basePodTopology := "N/A"
		if m.cachedTopologyInfo != nil {
			if pcsgName != "" {
				pcsgConfigName := data.ExtractConfigName(pcsgName, pcsName, replicaIndex)
				cliqueTemplateName := data.ExtractCliqueTemplateNameFromPCSGChild(pcName, pcsgName)
				effectiveClique := m.cachedTopologyInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
				basePodTopology = data.WrapInherited(effectiveClique)
			} else {
				cliqueTemplateName := data.ExtractConfigName(pcName, pcsName, replicaIndex)
				effectiveClique := m.cachedTopologyInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
				basePodTopology = data.WrapInherited(effectiveClique)
			}
		}

		domain := data.ExtractDomain(basePodTopology)
		podResources := make([]data.Resource, len(pods))
		for i, pod := range pods {
			podResources[i] = pod
			if domain != "" {
				nodeName := ""
				if cached, ok := snapshot.PodInfos[pod.Name]; ok {
					nodeName = cached.NodeName
				}
				value := data.ResolveTopologyValueForNode(domain, nodeName, m.cachedTopologyInfo, snapshot.NodeLabels)
				podResources[i].Topology = data.EnhanceTopologyDisplay(basePodTopology, value)
			} else {
				podResources[i].Topology = basePodTopology
			}
		}
	// Sort for stable table ordering (defense-in-depth; the cache also sorts).
	data.SortResourcesByName(podResources)
	m.allResources[podCliqueKey] = podResources
	}
}

// rebuildEventsFromSnapshot populates allEvents from the cache snapshot based on current view.
func (m *Model) rebuildEventsFromSnapshot(snapshot *data.CacheSnapshot) {
	if snapshot == nil {
		m.allEvents = nil
		return
	}

	switch m.viewState.ViewType {
	case data.ForestView:
		// Dispatch events based on the forest resource type
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 3 {
			switch selectedRow[1] {
			case "PodCliqueSet":
				m.allEvents = snapshot.GetEventsForPCS(selectedRow[2])
			case "PodCliqueScalingGroup":
				m.allEvents = snapshot.GetEventsForPCSG(selectedRow[2])
			case "PodClique":
				m.allEvents = snapshot.GetEventsForPodClique(selectedRow[2])
			case "Pod":
				m.allEvents = snapshot.EventsByObject["Pod/"+selectedRow[2]]
			default:
				m.allEvents = nil
			}
		} else if m.forestResourceType == "pcs" && len(snapshot.PodCliqueSets) > 0 {
			m.allEvents = snapshot.GetEventsForPCS(snapshot.PodCliqueSets[0].Name)
		} else {
			m.allEvents = nil
		}

	case data.PodCliqueSetView:
		// Show events for the selected replica
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 3 && selectedRow[1] == "PodCliqueSetReplica" {
			replicaIndex := extractReplicaIndex(selectedRow[2])
			m.allEvents = snapshot.GetEventsForReplica(m.viewState.SelectedPodCliqueSet, replicaIndex)
		} else {
			m.allEvents = snapshot.GetEventsForPCS(m.viewState.SelectedPodCliqueSet)
		}

	case data.PodCliqueSetReplicaView:
		// Show events for the selected resource in the replica
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 3 {
			switch selectedRow[1] {
			case "PodCliqueScalingGroup":
				m.allEvents = snapshot.GetEventsForPCSG(selectedRow[2])
			case "PodClique":
				m.allEvents = snapshot.GetEventsForPodClique(selectedRow[2])
			default:
				m.allEvents = snapshot.GetEventsForReplica(m.viewState.SelectedPodCliqueSet, m.viewState.SelectedReplicaIndex)
			}
		} else {
			m.allEvents = snapshot.GetEventsForReplica(m.viewState.SelectedPodCliqueSet, m.viewState.SelectedReplicaIndex)
		}

	case data.PodCliqueScalingGroupView:
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 3 && selectedRow[1] == "PodCliqueScalingGroupReplica" {
			replicaIndex := extractReplicaIndex(selectedRow[2])
			m.allEvents = snapshot.GetEventsForPCSGReplica(m.viewState.SelectedScalingGroup, replicaIndex)
		} else {
			m.allEvents = snapshot.GetEventsForPCSG(m.viewState.SelectedScalingGroup)
		}

	case data.PodCliqueScalingGroupReplicaView:
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 3 && selectedRow[1] == "PodClique" {
			m.allEvents = snapshot.GetEventsForPodClique(selectedRow[2])
		} else {
			m.allEvents = snapshot.GetEventsForPCSGReplica(m.viewState.SelectedScalingGroup, m.viewState.SelectedPCSGReplicaIndex)
		}

	case data.PodCliqueView:
		m.allEvents = snapshot.GetEventsForPodClique(m.viewState.SelectedPodClique)

	case data.PodView:
		m.allEvents = snapshot.GetEventsForPodClique(m.viewState.SelectedPodClique)

	default:
		m.allEvents = nil
	}
}

// handlePodYAML handles PodYAMLMsg.
func (m Model) handlePodYAML(msg PodYAMLMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading Pod YAML: %v", msg.Err)
		m.podYAMLData[msg.PodName] = fmt.Sprintf("# Error loading Pod YAML: %v", msg.Err)
	} else {
		debugLogWithContext("loaded %d bytes of YAML for Pod %s", len(msg.YAML), msg.PodName)
		m.podYAMLData[msg.PodName] = msg.YAML
	}
	m.updatePodViewport()
	return m, nil
}

// rebuildFlatDrillInResources populates allResources for views reached by drilling
// from a flat forest list (where SelectedPodCliqueSet is empty). Without PCS context,
// the normal hierarchy builder returns early — this fills the gap.
func (m *Model) rebuildFlatDrillInResources(snapshot *data.CacheSnapshot) {
	if snapshot == nil {
		return
	}

	pcsgName := m.viewState.SelectedScalingGroup
	pcsgReplicaIndex := m.viewState.SelectedPCSGReplicaIndex
	pcName := m.viewState.SelectedPodClique

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
		pcsgReplicaIndexes := snapshot.ReplicaIndexesByPCSG[pcsgName]
		if len(pcsgReplicaIndexes) > 0 {
			pcsgReplicaResources := make([]data.Resource, 0, len(pcsgReplicaIndexes))
			for _, ri := range pcsgReplicaIndexes {
				var totalReady, totalScheduled, totalReplicas int
				pcsgReplicaKey := pcsgName + "/" + ri
				for _, pc := range snapshot.PodCliquesByPCSGReplica[pcsgReplicaKey] {
					var ready, replicas int
					fmt.Sscanf(pc.Ready, "%d/%d", &ready, &replicas)
					totalReady += ready
					totalReplicas += replicas
					var scheduled, scheduledMax int
					fmt.Sscanf(pc.Scheduled, "%d/%d", &scheduled, &scheduledMax)
					totalScheduled += scheduled
				}
				pcsgReplicaResources = append(pcsgReplicaResources, data.Resource{
					Name:       fmt.Sprintf("%s-replica-%s", pcsgName, ri),
					Type:       "PodCliqueScalingGroupReplica",
					Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
					Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
					Namespace:  namespace,
					ParentType: "PodCliqueScalingGroup",
					ParentName: pcsgName,
					Topology:   "N/A",
				})
			}
			m.allResources[pcsgKey] = pcsgReplicaResources
		}

		// PCSG replica children — PodCliques within a specific replica
		if pcsgReplicaIndex != "" {
			pcsgReplicaChildKey := "PodCliqueScalingGroupReplica/" + pcsgName + "/" + pcsgReplicaIndex
			pcsgReplicaKey := pcsgName + "/" + pcsgReplicaIndex
			var pcsgChildren []data.Resource
			for _, pc := range snapshot.PodCliquesByPCSGReplica[pcsgReplicaKey] {
				pc := pc // copy
				pc.Topology = "N/A"
				pcsgChildren = append(pcsgChildren, pc)
			}
			data.SortResourcesByName(pcsgChildren)
			m.allResources[pcsgReplicaChildKey] = pcsgChildren
		}
	}

	// --- PodClique drill-in: build pod list ---
	if pcName != "" {
		podCliqueKey := "PodClique/" + pcName
		pods := snapshot.PodsByPodClique[pcName]
		podResources := make([]data.Resource, len(pods))
		for i, pod := range pods {
			podResources[i] = pod
			podResources[i].Topology = "N/A"
		}
		data.SortResourcesByName(podResources)
		m.allResources[podCliqueKey] = podResources
	}
}

// populateForestResources sets m.allResources["forest"] based on m.forestResourceType.
func (m *Model) populateForestResources(snapshot *data.CacheSnapshot) {
	if snapshot == nil {
		return
	}
	switch m.forestResourceType {
	case "pc":
		m.allResources["forest"] = flatPodCliques(snapshot)
	case "pcsg":
		m.allResources["forest"] = flatScalingGroups(snapshot)
	case "pod":
		m.allResources["forest"] = flatPods(snapshot)
	default: // "pcs" or empty
		m.allResources["forest"] = snapshot.PodCliqueSets
	}
}

// flatPodCliques returns a deduplicated flat list of all PodCliques from the snapshot.
func flatPodCliques(snapshot *data.CacheSnapshot) []data.Resource {
	if snapshot == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []data.Resource

	addPC := func(pc data.Resource) {
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

	data.SortResourcesByName(result)
	return result
}

// flatScalingGroups returns a deduplicated flat list of all PodCliqueScalingGroups from the snapshot.
func flatScalingGroups(snapshot *data.CacheSnapshot) []data.Resource {
	if snapshot == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []data.Resource

	for _, sgs := range snapshot.ScalingGroupsByReplica {
		for _, sg := range sgs {
			if !seen[sg.Name] {
				seen[sg.Name] = true
				result = append(result, sg)
			}
		}
	}

	data.SortResourcesByName(result)
	return result
}

// flatPods returns a deduplicated flat list of all Pods from the snapshot.
func flatPods(snapshot *data.CacheSnapshot) []data.Resource {
	if snapshot == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []data.Resource

	for _, pods := range snapshot.PodsByPodClique {
		for _, pod := range pods {
			if !seen[pod.Name] {
				seen[pod.Name] = true
				result = append(result, pod)
			}
		}
	}

	data.SortResourcesByName(result)
	return result
}

// validateTopologyDrillStack checks that the current drill stack is still valid.
func (m *Model) validateTopologyDrillStack() {
	if m.topologyViewData == nil || len(m.topologyDrillStack) == 0 {
		return
	}

	domainSet := make(map[string]bool)
	for _, d := range m.topologyViewData.Domains {
		domainSet[d.Domain] = true
	}

	for _, entry := range m.topologyDrillStack {
		if !domainSet[entry.Domain] {
			debugLogWithContext("drill stack domain %q no longer exists, resetting", entry.Domain)
			m.topologyDrillStack = nil
			return
		}
	}
}
