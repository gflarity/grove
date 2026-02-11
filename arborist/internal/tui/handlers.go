package tui

import (
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// handleForestData handles ForestDataMsg.
func (m Model) handleForestData(msg ForestDataMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading forest data: %v", msg.Err)
		m.lastError = msg.Err
		m.allResources["forest"] = []data.Resource{}
	} else {
		debugLogWithContext("loaded %d PodCliqueSets", len(msg.Resources))
		m.allResources["forest"] = msg.Resources
	}
	m.rebuildResourcesTable()

	// If we have PCSes, load events for the first one
	if len(msg.Resources) > 0 {
		firstPCS := msg.Resources[0]
		return m, loadEventsForPCSCmd(m.provider, m.ctx, firstPCS.Name, firstPCS.Namespace)
	}
	return m, nil
}

// handleReplicaData handles ReplicaDataMsg.
func (m Model) handleReplicaData(msg ReplicaDataMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading replica data: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	debugLogWithContext("loaded %d replica indexes for %s/%s", len(msg.ReplicaIndexes), msg.Namespace, msg.PCSName)

	// If there's only 1 replica, skip directly to PodCliqueSetReplicaView
	if len(msg.ReplicaIndexes) == 1 {
		replicaIndex := msg.ReplicaIndexes[0]
		oldViewType := m.viewState.ViewType
		m.viewState.ViewType = data.PodCliqueSetReplicaView
		m.viewState.SelectedReplicaIndex = replicaIndex

		debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, fmt.Sprintf("single replica skip, replica=%q", replicaIndex))

		// Build virtual replica resources for tracking (needed for back navigation)
		key := "PodCliqueSet/" + msg.PCSName
		m.allResources[key] = []data.Resource{{
			Name:      msg.PCSName + "-replica-" + replicaIndex,
			Type:      "PodCliqueSetReplica",
			Namespace: msg.Namespace,
		}}

		// Store children data and load events
		return m, tea.Batch(
			loadReplicaChildrenCmd(m.provider, m.ctx, msg.PCSName, msg.Namespace, replicaIndex),
			loadEventsForReplicaCmd(m.provider, m.ctx, msg.PCSName, msg.Namespace, replicaIndex),
		)
	}

	// Multiple replicas, show PodCliqueSetView with replica list
	m.viewState.ViewType = data.PodCliqueSetView

	// Build virtual PodCliqueSetReplica resources
	key := "PodCliqueSet/" + msg.PCSName
	resources := make([]data.Resource, 0, len(msg.ReplicaIndexes))

	for _, replicaIndex := range msg.ReplicaIndexes {
		// Calculate aggregate ready/scheduled counts from ScalingGroups and PodCliques
		var totalReady, totalScheduled, totalReplicas int

		for _, sg := range msg.ScalingGroupsByReplica[replicaIndex] {
			var ready, replicas int
			fmt.Sscanf(sg.Ready, "%d/%d", &ready, &replicas)
			totalReady += ready
			totalReplicas += replicas

			var scheduled, scheduledMax int
			fmt.Sscanf(sg.Scheduled, "%d/%d", &scheduled, &scheduledMax)
			totalScheduled += scheduled
		}

		for _, pc := range msg.PodCliquesByReplica[replicaIndex] {
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
			value := data.ResolveTopologyValueByReplicaIndex(domain, msg.PCSName, replicaIndex, m.cachedTopologyInfo, m.cachedPods, m.cachedNodeLabels)
			replicaTopology = data.EnhanceTopologyDisplay(replicaTopology, value)
		}

		resources = append(resources, data.Resource{
			Name:       fmt.Sprintf("%s-replica-%s", msg.PCSName, replicaIndex),
			Type:       "PodCliqueSetReplica",
			Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
			Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
			Namespace:  msg.Namespace,
			ParentType: "PodCliqueSet",
			ParentName: msg.PCSName,
			Topology:   replicaTopology,
		})
	}

	m.allResources[key] = resources
	m.rebuildResourcesTable()

	// Load events scoped to the first replica (where the cursor starts).
	// When navigateBack also loads events for a specific replica, both commands
	// will race; the last EventsMsg to arrive wins, which is acceptable since
	// the user can always press up/down to re-scope.
	if len(msg.ReplicaIndexes) > 0 {
		return m, loadEventsForReplicaCmd(m.provider, m.ctx, msg.PCSName, msg.Namespace, msg.ReplicaIndexes[0])
	}
	return m, nil
}

// handleReplicaChildren handles ReplicaChildrenMsg.
func (m Model) handleReplicaChildren(msg ReplicaChildrenMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading replica children: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	key := "PodCliqueSetReplica/" + msg.PCSName + "/" + msg.ReplicaIndex
	debugLogWithContext("loaded %d scaling groups and %d pod cliques for replica %s",
		len(msg.ScalingGroups), len(msg.PodCliques), msg.ReplicaIndex)

	// Set topology on PCSGs
	for i := range msg.ScalingGroups {
		pcsgConfigName := data.ExtractConfigName(msg.ScalingGroups[i].Name, msg.PCSName, msg.ReplicaIndex)
		if m.cachedTopologyInfo != nil {
			msg.ScalingGroups[i].Topology = m.cachedTopologyInfo.ResolvePCSGTopology(pcsgConfigName)
			domain := data.ExtractDomain(msg.ScalingGroups[i].Topology)
			value := data.ResolveTopologyValue(domain, "grove.io/podcliquescalinggroup", msg.ScalingGroups[i].Name, m.cachedTopologyInfo, m.cachedPods, m.cachedNodeLabels)
			msg.ScalingGroups[i].Topology = data.EnhanceTopologyDisplay(msg.ScalingGroups[i].Topology, value)
		} else {
			msg.ScalingGroups[i].Topology = "N/A"
		}
	}

	// Set topology on standalone PodCliques
	for i := range msg.PodCliques {
		cliqueTemplateName := data.ExtractConfigName(msg.PodCliques[i].Name, msg.PCSName, msg.ReplicaIndex)
		if m.cachedTopologyInfo != nil {
			msg.PodCliques[i].Topology = m.cachedTopologyInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
			domain := data.ExtractDomain(msg.PodCliques[i].Topology)
			value := data.ResolveTopologyValue(domain, "grove.io/podclique", msg.PodCliques[i].Name, m.cachedTopologyInfo, m.cachedPods, m.cachedNodeLabels)
			msg.PodCliques[i].Topology = data.EnhanceTopologyDisplay(msg.PodCliques[i].Topology, value)
		} else {
			msg.PodCliques[i].Topology = "N/A"
		}
	}

	// Combine scaling groups and pod cliques
	resources := append(msg.ScalingGroups, msg.PodCliques...)
	m.allResources[key] = resources
	m.rebuildResourcesTable()

	return m, nil
}

// handlePCSGReplicaData handles PCSGReplicaDataMsg (mirrors handleReplicaData for PCS replicas).
func (m Model) handlePCSGReplicaData(msg PCSGReplicaDataMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading PCSG replica data: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	debugLogWithContext("loaded %d PCSG replica indexes for %s/%s", len(msg.ReplicaIndexes), msg.Namespace, msg.PCSGName)

	// If there's only 1 replica, skip directly to PodCliqueScalingGroupReplicaView
	if len(msg.ReplicaIndexes) == 1 {
		replicaIndex := msg.ReplicaIndexes[0]
		oldViewType := m.viewState.ViewType
		m.viewState.ViewType = data.PodCliqueScalingGroupReplicaView
		m.viewState.SelectedPCSGReplicaIndex = replicaIndex

		debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupReplicaView, fmt.Sprintf("single PCSG replica skip, replica=%q", replicaIndex))

		// Build virtual replica resources for tracking (needed for back navigation)
		key := "PodCliqueScalingGroup/" + msg.PCSGName
		m.allResources[key] = []data.Resource{{
			Name:      msg.PCSGName + "-replica-" + replicaIndex,
			Type:      "PodCliqueScalingGroupReplica",
			Namespace: msg.Namespace,
		}}

		// Load children for this single replica and events scoped to it
		return m, tea.Batch(
			loadPCSGReplicaChildrenCmd(m.provider, m.ctx, msg.PCSGName, msg.Namespace, replicaIndex),
			loadEventsForPCSGReplicaCmd(m.provider, m.ctx, msg.PCSGName, msg.Namespace, replicaIndex),
		)
	}

	// Multiple replicas, show PodCliqueScalingGroupView with replica list
	m.viewState.ViewType = data.PodCliqueScalingGroupView

	// Build virtual PodCliqueScalingGroupReplica resources
	key := "PodCliqueScalingGroup/" + msg.PCSGName
	resources := make([]data.Resource, 0, len(msg.ReplicaIndexes))

	for _, replicaIndex := range msg.ReplicaIndexes {
		// Calculate aggregate ready/scheduled counts from PodCliques in this replica
		var totalReady, totalScheduled, totalReplicas int

		for _, pc := range msg.PodCliquesByReplica[replicaIndex] {
			var ready, replicas int
			fmt.Sscanf(pc.Ready, "%d/%d", &ready, &replicas)
			totalReady += ready
			totalReplicas += replicas

			var scheduled, scheduledMax int
			fmt.Sscanf(pc.Scheduled, "%d/%d", &scheduled, &scheduledMax)
			totalScheduled += scheduled
		}

		resources = append(resources, data.Resource{
			Name:       fmt.Sprintf("%s-replica-%s", msg.PCSGName, replicaIndex),
			Type:       "PodCliqueScalingGroupReplica",
			Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
			Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
			Namespace:  msg.Namespace,
			ParentType: "PodCliqueScalingGroup",
			ParentName: msg.PCSGName,
		})
	}

	m.allResources[key] = resources
	m.rebuildResourcesTable()

	// Load events scoped to the first PCSG replica (where the cursor starts).
	if len(msg.ReplicaIndexes) > 0 {
		return m, loadEventsForPCSGReplicaCmd(m.provider, m.ctx, msg.PCSGName, msg.Namespace, msg.ReplicaIndexes[0])
	}
	return m, nil
}

// handlePCSGChildren handles PCSGChildrenMsg.
// This is used both for the legacy flat PCSG children view and for PCSG replica children.
func (m Model) handlePCSGChildren(msg PCSGChildrenMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading PCSG children: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	// Determine the storage key based on current view
	var key string
	if m.viewState.ViewType == data.PodCliqueScalingGroupReplicaView && m.viewState.SelectedPCSGReplicaIndex != "" {
		key = "PodCliqueScalingGroupReplica/" + msg.PCSGName + "/" + m.viewState.SelectedPCSGReplicaIndex
	} else {
		key = "PodCliqueScalingGroup/" + msg.PCSGName
	}
	debugLogWithContext("loaded %d pod cliques for PCSG %s (key=%s)", len(msg.PodCliques), msg.PCSGName, key)

	// Set topology on PodCliques within this PCSG
	pcsgConfigName := data.ExtractConfigName(msg.PCSGName, m.viewState.SelectedPodCliqueSet, m.viewState.SelectedReplicaIndex)
	for i := range msg.PodCliques {
		cliqueTemplateName := data.ExtractCliqueTemplateNameFromPCSGChild(msg.PodCliques[i].Name, msg.PCSGName)
		if m.cachedTopologyInfo != nil {
			msg.PodCliques[i].Topology = m.cachedTopologyInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
			domain := data.ExtractDomain(msg.PodCliques[i].Topology)
			value := data.ResolveTopologyValue(domain, "grove.io/podclique", msg.PodCliques[i].Name, m.cachedTopologyInfo, m.cachedPods, m.cachedNodeLabels)
			msg.PodCliques[i].Topology = data.EnhanceTopologyDisplay(msg.PodCliques[i].Topology, value)
		} else {
			msg.PodCliques[i].Topology = "N/A"
		}
	}

	m.allResources[key] = msg.PodCliques
	m.rebuildResourcesTable()

	return m, nil
}

// handlePodCliqueChildren handles PodCliqueChildrenMsg.
func (m Model) handlePodCliqueChildren(msg PodCliqueChildrenMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading PodClique children: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	key := "PodClique/" + msg.PodCliqueName
	debugLogWithContext("loaded %d pods for PodClique %s", len(msg.Pods), msg.PodCliqueName)

	// Set topology on Pods (inherited from parent PodClique)
	basePodTopology := "N/A"
	if m.cachedTopologyInfo != nil {
		if m.viewState.SelectedScalingGroup != "" {
			pcsgConfigName := data.ExtractConfigName(m.viewState.SelectedScalingGroup, m.viewState.SelectedPodCliqueSet, m.viewState.SelectedReplicaIndex)
			cliqueTemplateName := data.ExtractCliqueTemplateNameFromPCSGChild(msg.PodCliqueName, m.viewState.SelectedScalingGroup)
			effectiveClique := m.cachedTopologyInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
			basePodTopology = data.WrapInherited(effectiveClique)
		} else {
			cliqueTemplateName := data.ExtractConfigName(msg.PodCliqueName, m.viewState.SelectedPodCliqueSet, m.viewState.SelectedReplicaIndex)
			effectiveClique := m.cachedTopologyInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
			basePodTopology = data.WrapInherited(effectiveClique)
		}
	}

	domain := data.ExtractDomain(basePodTopology)
	for i := range msg.Pods {
		if domain != "" {
			nodeName := ""
			if cached, ok := m.cachedPods[msg.Pods[i].Name]; ok {
				nodeName = cached.NodeName
			}
			value := data.ResolveTopologyValueForNode(domain, nodeName, m.cachedTopologyInfo, m.cachedNodeLabels)
			msg.Pods[i].Topology = data.EnhanceTopologyDisplay(basePodTopology, value)
		} else {
			msg.Pods[i].Topology = basePodTopology
		}
	}

	m.allResources[key] = msg.Pods
	m.rebuildResourcesTable()

	return m, nil
}

// handleEvents handles EventsMsg.
func (m Model) handleEvents(msg EventsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading events: %v", msg.Err)
		m.lastError = msg.Err
		m.allEvents = []data.Event{}
	} else {
		debugLogWithContext("loaded %d events", len(msg.Events))
		m.allEvents = msg.Events
	}
	m.rebuildEventsTable()
	return m, nil
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

// handleTopologyInfo handles TopologyInfoMsg.
func (m Model) handleTopologyInfo(msg TopologyInfoMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading topology info: %v", msg.Err)
		m.cachedTopologyInfo = nil
	} else if msg.TopologyInfo != nil {
		debugLogWithContext("built topology info: PCSPackDomain=%q, %d PCSGs, %d cliques",
			msg.TopologyInfo.PCSPackDomain, len(msg.TopologyInfo.PCSGPackDomains), len(msg.TopologyInfo.CliquePackDomains))
		m.cachedTopologyInfo = msg.TopologyInfo
	}
	return m, loadNodeLabelsCmd(m.provider, m.ctx, m.cachedTopologyInfo)
}

// handlePodInfo handles PodInfoMsg.
func (m Model) handlePodInfo(msg PodInfoMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading pod info: %v", msg.Err)
		m.cachedPods = nil
	} else {
		debugLogWithContext("cached %d pods for PCS %s/%s", len(msg.PodInfos), msg.Namespace, msg.PCSName)
		m.cachedPods = msg.PodInfos
	}
	return m, nil
}

// handleTopologyCacheSynced handles TopologyCacheSyncedMsg.
// Reads the initial snapshot, populates tables, and starts listening for updates.
func (m Model) handleTopologyCacheSynced(_ TopologyCacheSyncedMsg) (tea.Model, tea.Cmd) {
	debugLogWithContext("topology cache synced")

	if m.topologyCache == nil {
		return m, nil
	}

	snapshot := m.topologyCache.Snapshot()
	m.topologyViewData = snapshot

	// Validate drill stack against new data
	m.validateTopologyDrillStack()

	m.rebuildTopologyDomainsTable()
	m.rebuildTopologyPodsTable()

	// Start listening for updates
	return m, waitForTopologyCacheUpdateCmd(m.topologyCache)
}

// handleTopologyViewData handles TopologyViewDataMsg.
// Stores the new snapshot, rebuilds tables preserving drill-down state, and re-subscribes.
func (m Model) handleTopologyViewData(msg TopologyViewDataMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR from topology cache: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	debugLogWithContext("received topology view data update")
	m.topologyViewData = msg.Data

	// Validate drill stack against new data (domain may have been removed)
	m.validateTopologyDrillStack()

	m.rebuildTopologyDomainsTable()
	m.rebuildTopologyPodsTable()

	// Re-subscribe for next update
	if m.topologyCache != nil {
		return m, waitForTopologyCacheUpdateCmd(m.topologyCache)
	}
	return m, nil
}

// validateTopologyDrillStack checks that the current drill stack is still valid
// against the latest topology data. If a domain in the stack no longer exists
// in the ClusterTopology, the stack is reset to the top level.
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

// handleNodeLabels handles NodeLabelsMsg.
func (m Model) handleNodeLabels(msg NodeLabelsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading node labels: %v", msg.Err)
		m.cachedNodeLabels = nil
	} else {
		debugLogWithContext("cached labels for %d nodes", len(msg.NodeLabels))
		m.cachedNodeLabels = msg.NodeLabels
	}
	// Rebuild tables to apply topology values
	m.rebuildResourcesTable()
	return m, nil
}
