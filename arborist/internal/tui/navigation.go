package tui

import (
	"fmt"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	tea "github.com/charmbracelet/bubbletea"
)

// panesForCurrentView returns the ordered list of panes available in the
// current view. This is the single source of truth for pane cycling.
func (m *Model) panesForCurrentView() []clusterstate.Pane {
	return m.behavior().PaneList()
}

// switchPane cycles to the next pane in the current view's pane list.
func (m *Model) switchPane() {
	panes := m.panesForCurrentView()
	currentIdx := 0
	for i, p := range panes {
		if p == m.activePane {
			currentIdx = i
			break
		}
	}
	nextIdx := (currentIdx + 1) % len(panes)
	m.activePane = panes[nextIdx]
	m.updateTableFocus()
	debugLogWithContext("switched pane to %s", clusterstate.PaneName(m.activePane))
}

// updateTableFocus blurs all tables and focuses the one corresponding to the
// active pane.
func (m *Model) updateTableFocus() {
	m.resourcesTable.Blur()
	m.eventsTable.Blur()
	m.topologyDomainsTable.Blur()
	m.topologyPodsTable.Blur()

	switch m.activePane {
	case clusterstate.ResourcesPane:
		m.resourcesTable.Focus()
	case clusterstate.EventsPane:
		m.eventsTable.Focus()
	case clusterstate.TopologyDomainsPane:
		m.topologyDomainsTable.Focus()
	case clusterstate.TopologyPodsPane:
		m.topologyPodsTable.Focus()
	}
}

// resolveNamespace returns the namespace for the current PCS context.
func (m Model) resolveNamespace() string {
	if m.cachedSnapshot != nil && m.viewState.SelectedPodCliqueSet != "" {
		if pcs, ok := m.cachedSnapshot.PodCliqueSetSpecs[m.viewState.SelectedPodCliqueSet]; ok {
			return pcs.Namespace
		}
	}
	// Try current view resources
	viewKey := m.getCurrentViewKey()
	if resources, ok := m.allResources[viewKey]; ok && len(resources) > 0 {
		return resources[0].Namespace
	}
	// Try forest
	if resources, ok := m.allResources["forest"]; ok {
		for _, r := range resources {
			if r.Name == m.viewState.SelectedPodCliqueSet {
				return r.Namespace
			}
		}
	}
	return "default"
}

// getCurrentViewKey returns the key for looking up resources in allResources map.
func (m Model) getCurrentViewKey() string {
	return m.behavior().ViewKey(m.viewState)
}

// navigateActions maps resource types to their drill-down action.
var navigateActions = map[string]func(m *Model, name, namespace string) tea.Cmd{
	clusterstate.ResourceTypePodCliqueSet: func(m *Model, name, ns string) tea.Cmd { m.navigateIntoPCS(name, ns); return nil },
	clusterstate.ResourceTypePCSReplica:   func(m *Model, name, _ string) tea.Cmd { m.navigateIntoPCSReplica(name); return nil },
	clusterstate.ResourceTypePCSG:         func(m *Model, name, ns string) tea.Cmd { m.navigateIntoPCSG(name, ns); return nil },
	clusterstate.ResourceTypePCSGReplica:  func(m *Model, name, _ string) tea.Cmd { m.navigateIntoPCSGReplica(name); return nil },
	clusterstate.ResourceTypePodClique:    func(m *Model, name, _ string) tea.Cmd { m.navigateIntoPodClique(name); return nil },
	clusterstate.ResourceTypePod:          func(m *Model, name, ns string) tea.Cmd { return m.navigateIntoPod(name, ns) },
}

// navigateInto drills down into the selected resource.
// Navigation is now synchronous — data comes from the cached snapshot.
func (m Model) navigateInto() (tea.Model, tea.Cmd) {
	// Clear any active filter when navigating
	m.filterText = ""
	m.filterInput.SetValue("")

	if m.viewState.ViewType == clusterstate.PodView || m.viewState.ViewType == clusterstate.ContainersView {
		debugLogWithContext("navigateInto: already in PodView/ContainersView, ignoring")
		return m, nil
	}

	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		debugLogWithContext("navigateInto: no valid row selected")
		return m, nil
	}

	selectedNamespace := selectedRow[0]
	selectedType := selectedRow[1]
	selectedName := selectedRow[2]

	debugLogWithContext("navigateInto: type=%s name=%s namespace=%s", selectedType, selectedName, selectedNamespace)

	if action, ok := navigateActions[selectedType]; ok {
		cmd := action(&m, selectedName, selectedNamespace)
		return m, cmd
	}

	return m, nil
}

// navigateIntoPCS drills into a PodCliqueSet, auto-skipping to the replica view
// when only one replica exists.
func (m *Model) navigateIntoPCS(selectedName, selectedNamespace string) {
	oldViewType := m.viewState.ViewType
	m.viewState.SelectedPodCliqueSet = selectedName
	m.viewState.SelectedReplicaIndex = ""
	m.viewState.SelectedScalingGroup = ""
	m.viewState.SelectedPodClique = ""
	m.viewState.SelectedPod = ""

	// Build topology info from cached PCS spec
	m.cachedTopologyInfo = nil
	if m.cachedSnapshot != nil {
		if pcs, ok := m.cachedSnapshot.PodCliqueSetSpecs[selectedName]; ok {
			topoInfo := clusterstate.BuildTopologyInfo(pcs)
			if m.cachedSnapshot.TopologyViewData != nil {
				topoInfo.DomainToKey = m.cachedSnapshot.TopologyViewData.DomainToKey
			}
			m.cachedTopologyInfo = topoInfo
		}
	}

	// Check replica count for auto-skip
	replicaIndexes := m.cachedSnapshot.ReplicaIndexesByPCS[selectedName]
	if len(replicaIndexes) == 1 {
		// Single replica — skip PodCliqueSetView, go directly to replica view
		m.viewState.ViewType = clusterstate.PodCliqueSetReplicaView
		m.viewState.SelectedReplicaIndex = replicaIndexes[0]
		debugLogStateTransition(oldViewType, clusterstate.PodCliqueSetReplicaView, fmt.Sprintf("single replica skip, pcs=%q replica=%q", selectedName, replicaIndexes[0]))

		// Build virtual replica resources for tracking (needed for back navigation)
		pcsKey := "PodCliqueSet/" + selectedName
		m.allResources[pcsKey] = []clusterstate.Resource{{
			Name:      replicaDisplayName(selectedName, replicaIndexes[0]),
			Type:      clusterstate.ResourceTypePCSReplica,
			Namespace: selectedNamespace,
		}}
	} else {
		m.viewState.ViewType = clusterstate.PodCliqueSetView
		debugLogStateTransition(oldViewType, clusterstate.PodCliqueSetView, fmt.Sprintf("pcs=%q", selectedName))
	}

	m.rebuildAllFromSnapshot()
}

// navigateIntoPCSReplica drills into a PCS replica.
func (m *Model) navigateIntoPCSReplica(selectedName string) {
	parts := strings.Split(selectedName, replicaSeparator)
	if len(parts) != 2 {
		return
	}
	oldViewType := m.viewState.ViewType
	m.viewState.ViewType = clusterstate.PodCliqueSetReplicaView
	m.viewState.SelectedReplicaIndex = parts[1]
	m.viewState.SelectedScalingGroup = ""
	m.viewState.SelectedPodClique = ""
	m.viewState.SelectedPod = ""

	debugLogStateTransition(oldViewType, clusterstate.PodCliqueSetReplicaView, fmt.Sprintf("replica=%q", parts[1]))

	m.rebuildAllFromSnapshot()
}

// navigateIntoPCSG drills into a PodCliqueScalingGroup, auto-skipping to the
// replica view when only one replica exists.
func (m *Model) navigateIntoPCSG(selectedName, selectedNamespace string) {
	oldViewType := m.viewState.ViewType
	m.viewState.SelectedScalingGroup = selectedName
	m.viewState.SelectedPCSGReplicaIndex = ""
	m.viewState.SelectedPodClique = ""
	m.viewState.SelectedPod = ""

	// Check PCSG replica count for auto-skip
	pcsgReplicaIndexes := m.cachedSnapshot.ReplicaIndexesByPCSG[selectedName]
	if len(pcsgReplicaIndexes) == 1 {
		m.viewState.ViewType = clusterstate.PodCliqueScalingGroupReplicaView
		m.viewState.SelectedPCSGReplicaIndex = pcsgReplicaIndexes[0]
		debugLogStateTransition(oldViewType, clusterstate.PodCliqueScalingGroupReplicaView, fmt.Sprintf("single PCSG replica skip, pcsg=%q replica=%q", selectedName, pcsgReplicaIndexes[0]))

		// Build virtual replica resources for tracking (needed for back navigation)
		pcsgKey := "PodCliqueScalingGroup/" + selectedName
		m.allResources[pcsgKey] = []clusterstate.Resource{{
			Name:      replicaDisplayName(selectedName, pcsgReplicaIndexes[0]),
			Type:      clusterstate.ResourceTypePCSGReplica,
			Namespace: selectedNamespace,
		}}
	} else {
		m.viewState.ViewType = clusterstate.PodCliqueScalingGroupView
		debugLogStateTransition(oldViewType, clusterstate.PodCliqueScalingGroupView, fmt.Sprintf("pcsg=%q", selectedName))
	}

	m.rebuildAllFromSnapshot()
}

// navigateIntoPCSGReplica drills into a PCSG replica.
func (m *Model) navigateIntoPCSGReplica(selectedName string) {
	parts := strings.Split(selectedName, replicaSeparator)
	if len(parts) != 2 {
		return
	}
	oldViewType := m.viewState.ViewType
	m.viewState.ViewType = clusterstate.PodCliqueScalingGroupReplicaView
	m.viewState.SelectedPCSGReplicaIndex = parts[1]
	m.viewState.SelectedPodClique = ""
	m.viewState.SelectedPod = ""

	debugLogStateTransition(oldViewType, clusterstate.PodCliqueScalingGroupReplicaView, fmt.Sprintf("pcsg-replica=%q", parts[1]))

	m.rebuildAllFromSnapshot()
}

// navigateIntoPodClique drills into a PodClique.
func (m *Model) navigateIntoPodClique(selectedName string) {
	oldViewType := m.viewState.ViewType
	m.viewState.ViewType = clusterstate.PodCliqueView
	m.viewState.SelectedPodClique = selectedName
	m.viewState.SelectedPod = ""

	debugLogStateTransition(oldViewType, clusterstate.PodCliqueView, fmt.Sprintf("podClique=%q", selectedName))

	m.rebuildAllFromSnapshot()
}

// navigateIntoPod drills into a Pod, switching to ContainersView and
// asynchronously loading container info.
func (m *Model) navigateIntoPod(selectedName, selectedNamespace string) tea.Cmd {
	oldViewType := m.viewState.ViewType
	m.viewState.ViewType = clusterstate.ContainersView
	m.viewState.SelectedPod = selectedName

	debugLogStateTransition(oldViewType, clusterstate.ContainersView, fmt.Sprintf("pod=%q", selectedName))

	m.rebuildResourcesTable()
	m.rebuildEventsTable()

	return loadPodContainersCmd(m.cache, m.ctx, selectedName, selectedNamespace)
}

