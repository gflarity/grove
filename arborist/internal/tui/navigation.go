package tui

import (
	"fmt"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// switchPane toggles between Resources and Events panes.
func (m *Model) switchPane() {
	if m.activePane == data.ResourcesPane {
		m.activePane = data.EventsPane
		m.resourcesTable.Blur()
		m.eventsTable.Focus()
	} else {
		m.activePane = data.ResourcesPane
		m.eventsTable.Blur()
		m.resourcesTable.Focus()
	}
	debugLogWithContext("switched pane to %s", data.PaneName(m.activePane))
}

// loadEventsForSelection loads events based on the current selection.
func (m *Model) loadEventsForSelection() {
	// This is called when selection changes - we'll trigger event reload via commands
	// For now, events are already loaded when navigating
}

// getCurrentViewKey returns the key for looking up resources in allResources map.
func (m Model) getCurrentViewKey() string {
	switch m.viewState.ViewType {
	case data.ForestView:
		return "forest"
	case data.PodCliqueSetView:
		return "PodCliqueSet/" + m.viewState.SelectedPodCliqueSet
	case data.PodCliqueSetReplicaView:
		return "PodCliqueSetReplica/" + m.viewState.SelectedPodCliqueSet + "/" + m.viewState.SelectedReplicaIndex
	case data.PodCliqueScalingGroupView:
		return "PodCliqueScalingGroup/" + m.viewState.SelectedScalingGroup
	case data.PodCliqueView:
		return "PodClique/" + m.viewState.SelectedPodClique
	case data.PodView:
		return "" // Pod view doesn't list resources
	}
	return "forest"
}

// navigateInto drills down into the selected resource.
func (m Model) navigateInto() (tea.Model, tea.Cmd) {
	// Clear any active filter when navigating
	m.filterText = ""
	m.filterInput.SetValue("")

	// Can't navigate if in Pod view
	if m.viewState.ViewType == data.PodView {
		debugLogWithContext("navigateInto: already in PodView, ignoring")
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

	var cmds []tea.Cmd

	switch selectedType {
	case "PodCliqueSet":
		m.viewState.SelectedPodCliqueSet = selectedName
		m.viewState.SelectedReplicaIndex = ""
		m.viewState.SelectedScalingGroup = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		// Clear cached topology data
		m.cachedTopologyInfo = nil
		m.cachedPods = nil
		m.cachedNodeLabels = nil

		// Load topology info, pod info, node labels, replicas, and events
		cmds = append(cmds,
			loadTopologyInfoCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadPodInfoCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadReplicasCmd(m.provider, m.ctx, selectedName, selectedNamespace),
		)

	case "PodCliqueSetReplica":
		// Extract replica index from name (format: "pcsname-replica-0")
		parts := strings.Split(selectedName, "-replica-")
		if len(parts) == 2 {
			oldViewType := m.viewState.ViewType
			m.viewState.ViewType = data.PodCliqueSetReplicaView
			m.viewState.SelectedReplicaIndex = parts[1]
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, fmt.Sprintf("replica=%q", parts[1]))

			// Load children resources and events for this replica
			cmds = append(cmds,
				loadReplicaChildrenCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, selectedNamespace, parts[1]),
				loadEventsForReplicaCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, selectedNamespace, parts[1]),
			)
		}

	case "PodCliqueScalingGroup":
		oldViewType := m.viewState.ViewType
		m.viewState.ViewType = data.PodCliqueScalingGroupView
		m.viewState.SelectedScalingGroup = selectedName
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupView, fmt.Sprintf("pcsg=%q", selectedName))

		// Load children resources and events for this PCSG
		cmds = append(cmds,
			loadPCSGChildrenCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadEventsForPCSGCmd(m.provider, m.ctx, selectedName, selectedNamespace),
		)

	case "PodClique":
		oldViewType := m.viewState.ViewType
		m.viewState.ViewType = data.PodCliqueView
		m.viewState.SelectedPodClique = selectedName
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueView, fmt.Sprintf("podClique=%q", selectedName))

		// Load children resources (Pods) and events for this PodClique
		cmds = append(cmds,
			loadPodCliqueChildrenCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadEventsForPodCliqueCmd(m.provider, m.ctx, selectedName, selectedNamespace),
		)

	case "Pod":
		oldViewType := m.viewState.ViewType
		m.viewState.ViewType = data.PodView
		m.viewState.SelectedPod = selectedName

		debugLogStateTransition(oldViewType, data.PodView, fmt.Sprintf("pod=%q", selectedName))

		// Load Pod YAML
		cmds = append(cmds, loadPodYAMLCmd(m.provider, m.ctx, selectedName, selectedNamespace))
	}

	m.rebuildResourcesTable()
	m.rebuildEventsTable()

	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// navigateBack goes up one level in the hierarchy.
func (m Model) navigateBack() (tea.Model, tea.Cmd) {
	// Clear any active filter when navigating
	m.filterText = ""
	m.filterInput.SetValue("")

	oldViewType := m.viewState.ViewType
	debugLogWithContext("navigateBack: current viewType=%s", data.ViewTypeName(oldViewType))

	var cmds []tea.Cmd

	switch m.viewState.ViewType {
	case data.ForestView:
		// Already at root, nothing to do
		debugLogWithContext("navigateBack: already at ForestView, ignoring")
		return m, nil

	case data.PodCliqueSetView:
		// Go back to Forest
		m.viewState.ViewType = data.ForestView
		m.viewState.SelectedPodCliqueSet = ""
		m.viewState.SelectedReplicaIndex = ""
		m.viewState.SelectedScalingGroup = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""
		m.cachedTopologyInfo = nil
		m.cachedPods = nil
		m.cachedNodeLabels = nil

		debugLogStateTransition(oldViewType, data.ForestView, "")

		// Reload forest data
		cmds = append(cmds, loadForestDataCmd(m.provider, m.ctx))
		m.allEvents = []data.Event{}

	case data.PodCliqueSetReplicaView:
		// Check if we should go back to PodCliqueSet view or directly to Forest
		// (if there's only 1 replica, we skip PodCliqueSetView)
		viewKey := m.getCurrentViewKey()
		resources, exists := m.allResources[viewKey]
		namespace := "default"
		if exists && len(resources) > 0 {
			namespace = resources[0].Namespace
		}

		// Check how many replicas (by looking at PodCliqueSet key)
		pcsKey := "PodCliqueSet/" + m.viewState.SelectedPodCliqueSet
		pcsResources := m.allResources[pcsKey]

		if len(pcsResources) == 1 {
			// Only 1 replica, so we came directly from Forest view
			m.viewState.ViewType = data.ForestView
			m.viewState.SelectedPodCliqueSet = ""
			m.viewState.SelectedReplicaIndex = ""
			m.cachedTopologyInfo = nil
			m.cachedPods = nil
			m.cachedNodeLabels = nil

			debugLogStateTransition(oldViewType, data.ForestView, "single replica skip")

			cmds = append(cmds, loadForestDataCmd(m.provider, m.ctx))
			m.allEvents = []data.Event{}
		} else {
			// Multiple replicas, go back to PodCliqueSet view
			m.viewState.ViewType = data.PodCliqueSetView
			m.viewState.SelectedReplicaIndex = ""
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueSetView, "")

			cmds = append(cmds,
				loadReplicasCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, namespace),
				loadEventsForPCSCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, namespace),
			)
		}

	case data.PodCliqueScalingGroupView:
		// Go back to PodCliqueSetReplica
		m.viewState.ViewType = data.PodCliqueSetReplicaView
		m.viewState.SelectedScalingGroup = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, "")

	case data.PodCliqueView:
		// Go back to parent (either PodCliqueSetReplica or PodCliqueScalingGroup)
		var newViewType data.ViewType
		if m.viewState.SelectedScalingGroup != "" {
			newViewType = data.PodCliqueScalingGroupView
		} else {
			newViewType = data.PodCliqueSetReplicaView
		}
		m.viewState.ViewType = newViewType
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, newViewType, "")

	case data.PodView:
		// Go back to PodClique view
		m.viewState.ViewType = data.PodCliqueView
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueView, "")
	}

	m.rebuildResourcesTable()
	m.rebuildEventsTable()

	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	return m, nil
}
