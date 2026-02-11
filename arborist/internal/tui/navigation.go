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
// active pane.
func (m *Model) updateTableFocus() {
	m.resourcesTable.Blur()
	m.eventsTable.Blur()
	m.topologyDomainsTable.Blur()
	m.topologyPodsTable.Blur()

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
	switch m.viewState.ViewType {
	case data.ForestView:
		return "forest"
	case data.PodCliqueSetView:
		return "PodCliqueSet/" + m.viewState.SelectedPodCliqueSet
	case data.PodCliqueSetReplicaView:
		return "PodCliqueSetReplica/" + m.viewState.SelectedPodCliqueSet + "/" + m.viewState.SelectedReplicaIndex
	case data.PodCliqueScalingGroupView:
		return "PodCliqueScalingGroup/" + m.viewState.SelectedScalingGroup
	case data.PodCliqueScalingGroupReplicaView:
		return "PodCliqueScalingGroupReplica/" + m.viewState.SelectedScalingGroup + "/" + m.viewState.SelectedPCSGReplicaIndex
	case data.PodCliqueView:
		return "PodClique/" + m.viewState.SelectedPodClique
	case data.PodView:
		return "" // Pod view doesn't list resources
	}
	return "forest"
}

// navigateInto drills down into the selected resource.
// Navigation is now synchronous — data comes from the cached snapshot.
func (m Model) navigateInto() (tea.Model, tea.Cmd) {
	// Clear any active filter when navigating
	m.filterText = ""
	m.filterInput.SetValue("")

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

	switch selectedType {
	case "PodCliqueSet":
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
				topoInfo := data.BuildTopologyInfo(pcs)
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
			m.viewState.ViewType = data.PodCliqueSetReplicaView
			m.viewState.SelectedReplicaIndex = replicaIndexes[0]
			debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, fmt.Sprintf("single replica skip, pcs=%q replica=%q", selectedName, replicaIndexes[0]))

			// Build virtual replica resources for tracking (needed for back navigation)
			pcsKey := "PodCliqueSet/" + selectedName
			m.allResources[pcsKey] = []data.Resource{{
				Name:      selectedName + "-replica-" + replicaIndexes[0],
				Type:      "PodCliqueSetReplica",
				Namespace: selectedNamespace,
			}}
		} else {
			m.viewState.ViewType = data.PodCliqueSetView
			debugLogStateTransition(oldViewType, data.PodCliqueSetView, fmt.Sprintf("pcs=%q", selectedName))
		}

		// Rebuild from snapshot
		m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
		m.rebuildEventsFromSnapshot(m.cachedSnapshot)
		m.rebuildResourcesTable()
		m.rebuildEventsTable()
		return m, nil

	case "PodCliqueSetReplica":
		parts := strings.Split(selectedName, "-replica-")
		if len(parts) == 2 {
			oldViewType := m.viewState.ViewType
			m.viewState.ViewType = data.PodCliqueSetReplicaView
			m.viewState.SelectedReplicaIndex = parts[1]
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, fmt.Sprintf("replica=%q", parts[1]))

			m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
			m.rebuildEventsFromSnapshot(m.cachedSnapshot)
			m.rebuildResourcesTable()
			m.rebuildEventsTable()
		}
		return m, nil

	case "PodCliqueScalingGroup":
		oldViewType := m.viewState.ViewType
		m.viewState.SelectedScalingGroup = selectedName
		m.viewState.SelectedPCSGReplicaIndex = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		// Check PCSG replica count for auto-skip
		pcsgReplicaIndexes := m.cachedSnapshot.ReplicaIndexesByPCSG[selectedName]
		if len(pcsgReplicaIndexes) == 1 {
			m.viewState.ViewType = data.PodCliqueScalingGroupReplicaView
			m.viewState.SelectedPCSGReplicaIndex = pcsgReplicaIndexes[0]
			debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupReplicaView, fmt.Sprintf("single PCSG replica skip, pcsg=%q replica=%q", selectedName, pcsgReplicaIndexes[0]))

			// Build virtual replica resources for tracking (needed for back navigation)
			pcsgKey := "PodCliqueScalingGroup/" + selectedName
			m.allResources[pcsgKey] = []data.Resource{{
				Name:      selectedName + "-replica-" + pcsgReplicaIndexes[0],
				Type:      "PodCliqueScalingGroupReplica",
				Namespace: selectedNamespace,
			}}
		} else {
			m.viewState.ViewType = data.PodCliqueScalingGroupView
			debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupView, fmt.Sprintf("pcsg=%q", selectedName))
		}

		m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
		m.rebuildEventsFromSnapshot(m.cachedSnapshot)
		m.rebuildResourcesTable()
		m.rebuildEventsTable()
		return m, nil

	case "PodCliqueScalingGroupReplica":
		parts := strings.Split(selectedName, "-replica-")
		if len(parts) == 2 {
			oldViewType := m.viewState.ViewType
			m.viewState.ViewType = data.PodCliqueScalingGroupReplicaView
			m.viewState.SelectedPCSGReplicaIndex = parts[1]
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupReplicaView, fmt.Sprintf("pcsg-replica=%q", parts[1]))

			m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
			m.rebuildEventsFromSnapshot(m.cachedSnapshot)
			m.rebuildResourcesTable()
			m.rebuildEventsTable()
		}
		return m, nil

	case "PodClique":
		oldViewType := m.viewState.ViewType
		m.viewState.ViewType = data.PodCliqueView
		m.viewState.SelectedPodClique = selectedName
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueView, fmt.Sprintf("podClique=%q", selectedName))

		m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
		m.rebuildEventsFromSnapshot(m.cachedSnapshot)
		m.rebuildResourcesTable()
		m.rebuildEventsTable()
		return m, nil

	case "Pod":
		oldViewType := m.viewState.ViewType
		m.viewState.ViewType = data.PodView
		m.viewState.SelectedPod = selectedName

		debugLogStateTransition(oldViewType, data.PodView, fmt.Sprintf("pod=%q", selectedName))

		m.rebuildResourcesTable()
		m.rebuildEventsTable()

		// Pod YAML is the one async API call that remains
		return m, loadPodYAMLCmd(m.cache, m.ctx, selectedName, selectedNamespace)
	}

	return m, nil
}

// topologyDrillInto pushes to the drill stack based on the current selection.
func (m *Model) topologyDrillInto() {
	if m.topologyViewData == nil {
		return
	}

	if len(m.topologyDrillStack) == 0 {
		selectedRow := m.topologyDomainsTable.SelectedRow()
		if len(selectedRow) < 2 {
			return
		}

		domain := selectedRow[0]
		key := selectedRow[1]

		m.topologyDrillStack = append(m.topologyDrillStack, data.TopologyDrillSelection{
			Domain: domain,
			Key:    key,
			Value:  "",
		})

		debugLogWithContext("topology drill into domain: %s (key: %s)", domain, key)
	} else {
		selectedRow := m.topologyDomainsTable.SelectedRow()
		if len(selectedRow) < 1 {
			return
		}

		value := selectedRow[0]
		m.topologyDrillStack[len(m.topologyDrillStack)-1].Value = value

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
			m.topologyDrillStack = append(m.topologyDrillStack, data.TopologyDrillSelection{
				Domain: nextDomain,
				Key:    nextKey,
				Value:  "",
			})
			debugLogWithContext("topology drill into value %q, advancing to domain: %s", value, nextDomain)
		} else {
			debugLogWithContext("topology drill: at narrowest domain, no-op")
			m.topologyDrillStack[len(m.topologyDrillStack)-1].Value = ""
			return
		}
	}

	m.rebuildTopologyDomainsTable()
	m.rebuildTopologyPodsTable()
}

// topologyDrillBack pops the last entry from the drill stack.
// When the last entry has no value (showing values for a domain), popping it
// also clears the previous entry's value so the user sees the parent domain's
// values list in a single Esc press. Without this, the intermediate state
// (previous entry still has a value) causes currentTopologyDomain to return
// the same domain again, making Esc appear to do nothing.
func (m *Model) topologyDrillBack() {
	if len(m.topologyDrillStack) == 0 {
		return
	}

	lastEntry := m.topologyDrillStack[len(m.topologyDrillStack)-1]

	if lastEntry.Value == "" {
		// Pop the empty-value entry (we're leaving this domain level)
		m.topologyDrillStack = m.topologyDrillStack[:len(m.topologyDrillStack)-1]
		// Also clear the previous entry's value so we go back to its values list.
		// Without this, currentTopologyDomain would still point to the same domain
		// we just popped (because the previous entry has a selected value, so the
		// "next domain" is the one we just left).
		if len(m.topologyDrillStack) > 0 {
			m.topologyDrillStack[len(m.topologyDrillStack)-1].Value = ""
		}
	} else {
		m.topologyDrillStack[len(m.topologyDrillStack)-1].Value = ""
	}

	debugLogWithContext("topology drill back, stack depth now: %d", len(m.topologyDrillStack))
	m.rebuildTopologyDomainsTable()
	m.rebuildTopologyPodsTable()
}

// =============================================================================
// Command Mode (vim-style ":" lens switching with autocomplete)
// =============================================================================

type lensCommand struct {
	Name string
}

var lensCommands = []lensCommand{
	{Name: "forest"},
	{Name: "topology"},
}

// LensCommandNames returns the list of available command names.
func LensCommandNames() []string {
	names := make([]string, len(lensCommands))
	for i, c := range lensCommands {
		names[i] = c.Name
	}
	return names
}

func matchLensCommand(prefix string) (string, bool) {
	if prefix == "" {
		return "", false
	}
	prefix = strings.ToLower(prefix)
	var matches []string
	for _, c := range lensCommands {
		if strings.HasPrefix(c.Name, prefix) {
			matches = append(matches, c.Name)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func completeLensCommand(prefix string) string {
	if prefix == "" {
		return ""
	}
	lower := strings.ToLower(prefix)
	var matches []string
	for _, c := range lensCommands {
		if strings.HasPrefix(c.Name, lower) {
			matches = append(matches, c.Name)
		}
	}
	if len(matches) == 0 {
		return prefix
	}
	if len(matches) == 1 {
		return matches[0]
	}
	lcp := matches[0]
	for _, m := range matches[1:] {
		for i := 0; i < len(lcp); i++ {
			if i >= len(m) || lcp[i] != m[i] {
				lcp = lcp[:i]
				break
			}
		}
	}
	return lcp
}

func (m Model) executeCommand(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return m, nil
	}

	matched := ""
	for _, c := range lensCommands {
		if c.Name == input {
			matched = c.Name
			break
		}
	}
	if matched == "" {
		if name, ok := matchLensCommand(input); ok {
			matched = name
		}
	}
	if matched == "" {
		debugLogWithContext("command mode: no match for %q", input)
		return m, nil
	}

	debugLogWithContext("command mode: executing %q (matched %q)", input, matched)

	switch matched {
	case "forest":
		if m.viewState.ViewType != data.ForestView {
			m.viewState.ViewType = data.ForestView
			m.viewState.SelectedPodCliqueSet = ""
			m.viewState.SelectedReplicaIndex = ""
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""
			m.cachedTopologyInfo = nil
			m.activePane = data.ResourcesPane
			m.updateTableFocus()
			m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
			m.rebuildEventsFromSnapshot(m.cachedSnapshot)
			m.rebuildResourcesTable()
			m.rebuildEventsTable()
		}
		return m, nil

	case "topology":
		return m.toggleTopologyView()
	}

	return m, nil
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
// Navigation is now synchronous — data comes from the cached snapshot.
func (m Model) navigateBack() (tea.Model, tea.Cmd) {
	// Clear any active filter when navigating
	m.filterText = ""
	m.filterInput.SetValue("")

	oldViewType := m.viewState.ViewType
	debugLogWithContext("navigateBack: current viewType=%s", data.ViewTypeName(oldViewType))

	switch m.viewState.ViewType {
	case data.ForestView:
		debugLogWithContext("navigateBack: already at ForestView, ignoring")
		return m, nil

	case data.PodCliqueSetView:
		m.viewState.ViewType = data.ForestView
		m.viewState.SelectedPodCliqueSet = ""
		m.viewState.SelectedReplicaIndex = ""
		m.viewState.SelectedScalingGroup = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""
		m.cachedTopologyInfo = nil

		debugLogStateTransition(oldViewType, data.ForestView, "")

	case data.PodCliqueSetReplicaView:
		// Check if we should go back to PodCliqueSetView or directly to Forest
		pcsKey := "PodCliqueSet/" + m.viewState.SelectedPodCliqueSet
		pcsResources := m.allResources[pcsKey]

		if len(pcsResources) == 1 {
			// Only 1 replica — came directly from Forest view
			m.viewState.ViewType = data.ForestView
			m.viewState.SelectedPodCliqueSet = ""
			m.viewState.SelectedReplicaIndex = ""
			m.cachedTopologyInfo = nil

			debugLogStateTransition(oldViewType, data.ForestView, "single replica skip")
		} else {
			m.viewState.ViewType = data.PodCliqueSetView
			m.viewState.SelectedReplicaIndex = ""
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueSetView, "")
		}

	case data.PodCliqueScalingGroupView:
		m.viewState.ViewType = data.PodCliqueSetReplicaView
		m.viewState.SelectedScalingGroup = ""
		m.viewState.SelectedPCSGReplicaIndex = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, "")

	case data.PodCliqueScalingGroupReplicaView:
		pcsgKey := "PodCliqueScalingGroup/" + m.viewState.SelectedScalingGroup
		pcsgResources := m.allResources[pcsgKey]

		if len(pcsgResources) == 1 {
			m.viewState.ViewType = data.PodCliqueSetReplicaView
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPCSGReplicaIndex = ""

			debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, "single PCSG replica skip")
		} else {
			m.viewState.ViewType = data.PodCliqueScalingGroupView
			m.viewState.SelectedPCSGReplicaIndex = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupView, "")
		}

	case data.PodCliqueView:
		var newViewType data.ViewType
		if m.viewState.SelectedScalingGroup != "" && m.viewState.SelectedPCSGReplicaIndex != "" {
			newViewType = data.PodCliqueScalingGroupReplicaView
		} else if m.viewState.SelectedScalingGroup != "" {
			newViewType = data.PodCliqueScalingGroupView
		} else {
			newViewType = data.PodCliqueSetReplicaView
		}
		m.viewState.ViewType = newViewType
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, newViewType, "")

	case data.PodView:
		m.viewState.ViewType = data.PodCliqueView
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueView, "")
	}

	// Rebuild from snapshot (synchronous)
	m.rebuildHierarchyFromSnapshot(m.cachedSnapshot)
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
	m.rebuildResourcesTable()
	m.rebuildEventsTable()

	return m, nil
}
