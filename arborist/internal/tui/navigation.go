package tui

import (
	"fmt"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// panesForCurrentView returns the ordered list of panes available in the
// current view. This is the single source of truth for pane cycling.
func (m *Model) panesForCurrentView() []data.Pane {
	if m.viewState.ViewType == data.TopologyView {
		return []data.Pane{data.TopologyDomainsPane, data.TopologyPodsPane}
	}
	return []data.Pane{data.ResourcesPane, data.EventsPane}
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
	debugLogWithContext("switched pane to %s", data.PaneName(m.activePane))
}

// updateTableFocus blurs all tables and focuses the one corresponding to the
// active pane. Called after any pane change to keep focus state consistent.
func (m *Model) updateTableFocus() {
	// Blur all tables
	m.resourcesTable.Blur()
	m.eventsTable.Blur()
	m.topologyDomainsTable.Blur()
	m.topologyPodsTable.Blur()

	// Focus the active pane's table
	switch m.activePane {
	case data.ResourcesPane:
		m.resourcesTable.Focus()
	case data.EventsPane:
		m.eventsTable.Focus()
	case data.TopologyDomainsPane:
		m.topologyDomainsTable.Focus()
	case data.TopologyPodsPane:
		m.topologyPodsTable.Focus()
	}
}

// eventsCommandForSelection returns a tea.Cmd that updates the events table
// to reflect the currently highlighted resource row. In ForestView this means
// loading events for the highlighted PodCliqueSet from the backend; in
// PodCliqueView the events are already loaded so we just rebuild the table
// with the client-side filter (no server call needed).
func (m *Model) eventsCommandForSelection() tea.Cmd {
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		return nil
	}

	selectedNamespace := selectedRow[0]
	selectedType := selectedRow[1]
	selectedName := selectedRow[2]

	switch m.viewState.ViewType {
	case data.ForestView:
		if selectedType == "PodCliqueSet" {
			return loadEventsForPCSCmd(m.provider, m.ctx, selectedName, selectedNamespace)
		}
	case data.PodCliqueView:
		// Client-side filter: getFilteredEvents reads the current selected row,
		// so just rebuilding the events table is sufficient.
		m.rebuildEventsTable()
	}

	return nil
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

// topologyDrillInto pushes to the drill stack based on the current selection.
func (m *Model) topologyDrillInto() {
	if m.topologyViewData == nil {
		return
	}

	if len(m.topologyDrillStack) == 0 {
		// At top-level domains — drill into the selected domain
		selectedRow := m.topologyDomainsTable.SelectedRow()
		if len(selectedRow) < 2 {
			return
		}

		domain := selectedRow[0]
		key := selectedRow[1]

		m.topologyDrillStack = append(m.topologyDrillStack, data.TopologyDrillSelection{
			Domain: domain,
			Key:    key,
			Value:  "", // no value selected yet — will show values list
		})

		debugLogWithContext("topology drill into domain: %s (key: %s)", domain, key)
	} else {
		// At a values list — select the value and advance to next domain
		selectedRow := m.topologyDomainsTable.SelectedRow()
		if len(selectedRow) < 1 {
			return
		}

		value := selectedRow[0]

		// Set the value on the current drill entry
		m.topologyDrillStack[len(m.topologyDrillStack)-1].Value = value

		// Check if there's a next domain to drill into
		currentDomain := m.topologyDrillStack[len(m.topologyDrillStack)-1].Domain
		domains := m.topologyViewData.Domains

		nextDomain := ""
		nextKey := ""
		for i, d := range domains {
			if d.Domain == currentDomain && i+1 < len(domains) {
				nextDomain = domains[i+1].Domain
				nextKey = domains[i+1].Key
				break
			}
		}

		if nextDomain != "" {
			// Push next domain onto the stack
			m.topologyDrillStack = append(m.topologyDrillStack, data.TopologyDrillSelection{
				Domain: nextDomain,
				Key:    nextKey,
				Value:  "", // will show values list for this domain
			})
			debugLogWithContext("topology drill into value %q, advancing to domain: %s", value, nextDomain)
		} else {
			debugLogWithContext("topology drill: at narrowest domain, no-op")
			// At the narrowest domain, enter on a value is a no-op.
			// But we already set the value, so pop it back to no-value state
			// Actually: the plan says enter at narrowest is no-op. So undo the value set.
			m.topologyDrillStack[len(m.topologyDrillStack)-1].Value = ""
			return
		}
	}

	m.rebuildTopologyDomainsTable()
	m.rebuildTopologyPodsTable()
}

// topologyDrillBack pops the last entry from the drill stack.
func (m *Model) topologyDrillBack() {
	if len(m.topologyDrillStack) == 0 {
		return
	}

	lastEntry := m.topologyDrillStack[len(m.topologyDrillStack)-1]

	if lastEntry.Value == "" {
		// At a domain with no value selected yet — pop this entry entirely
		m.topologyDrillStack = m.topologyDrillStack[:len(m.topologyDrillStack)-1]
	} else {
		// Has a value selected — clear the value to go back to value selection
		m.topologyDrillStack[len(m.topologyDrillStack)-1].Value = ""
	}

	debugLogWithContext("topology drill back, stack depth now: %d", len(m.topologyDrillStack))
	m.rebuildTopologyDomainsTable()
	m.rebuildTopologyPodsTable()
}

// topologyBreadcrumbString generates a breadcrumb like "region=us-east-1 > zone=us-east-1a".
func (m *Model) topologyBreadcrumbString() string {
	if len(m.topologyDrillStack) == 0 {
		return ""
	}

	var parts []string
	for _, entry := range m.topologyDrillStack {
		if entry.Value != "" {
			parts = append(parts, entry.Domain+"="+entry.Value)
		}
	}

	return strings.Join(parts, " > ")
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
