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
// to reflect the currently highlighted resource row. Called on every cursor
// up/down in the resources table.
//
// Design principle: events should always reflect the subtree of the currently
// highlighted row. At every level, navigating between rows reloads events
// scoped to that row's subtree.
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
		// Each PCS is an independent resource tree → load that PCS's events
		if selectedType == "PodCliqueSet" {
			return loadEventsForPCSCmd(m.provider, m.ctx, selectedName, selectedNamespace)
		}

	case data.PodCliqueSetView:
		// Highlighting a PodCliqueSetReplica → load events for that replica's subtree
		if selectedType == "PodCliqueSetReplica" {
			// Extract replica index from name (format: "pcsname-replica-INDEX")
			parts := strings.Split(selectedName, "-replica-")
			if len(parts) == 2 {
				return loadEventsForReplicaCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, selectedNamespace, parts[1])
			}
		}

	case data.PodCliqueSetReplicaView:
		// Highlighting a PCSG → load events for that PCSG's subtree
		if selectedType == "PodCliqueScalingGroup" {
			return loadEventsForPCSGCmd(m.provider, m.ctx, selectedName, selectedNamespace)
		}
		// Highlighting a standalone PodClique → load events for that PodClique
		if selectedType == "PodClique" {
			return loadEventsForPodCliqueCmd(m.provider, m.ctx, selectedName, selectedNamespace)
		}

	case data.PodCliqueScalingGroupView:
		// Highlighting a PodCliqueScalingGroupReplica → load events for that PCSG replica's subtree
		if selectedType == "PodCliqueScalingGroupReplica" {
			// Extract replica index from name (format: "pcsgName-replica-INDEX")
			parts := strings.Split(selectedName, "-replica-")
			if len(parts) == 2 {
				return loadEventsForPCSGReplicaCmd(m.provider, m.ctx, m.viewState.SelectedScalingGroup, selectedNamespace, parts[1])
			}
		}

	case data.PodCliqueScalingGroupReplicaView:
		// Highlighting a PodClique → load events for that PodClique
		if selectedType == "PodClique" {
			return loadEventsForPodCliqueCmd(m.provider, m.ctx, selectedName, selectedNamespace)
		}

	case data.PodCliqueView:
		// Client-side filter: getFilteredEvents reads the current selected row,
		// so just rebuilding the events table is sufficient (no server call).
		m.rebuildEventsTable()
	}

	return nil
}

// resolveNamespace returns the namespace for the current PCS context by
// checking stored resources. All resources under a PCS share the same namespace.
func (m Model) resolveNamespace() string {
	// Try current view resources
	viewKey := m.getCurrentViewKey()
	if resources, ok := m.allResources[viewKey]; ok && len(resources) > 0 {
		return resources[0].Namespace
	}
	// Try PCS replica key
	replicaKey := "PodCliqueSetReplica/" + m.viewState.SelectedPodCliqueSet + "/" + m.viewState.SelectedReplicaIndex
	if resources, ok := m.allResources[replicaKey]; ok && len(resources) > 0 {
		return resources[0].Namespace
	}
	// Try PCS key
	pcsKey := "PodCliqueSet/" + m.viewState.SelectedPodCliqueSet
	if resources, ok := m.allResources[pcsKey]; ok && len(resources) > 0 {
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
		oldViewType := m.viewState.ViewType
		m.viewState.ViewType = data.PodCliqueSetView
		m.viewState.SelectedPodCliqueSet = selectedName
		m.viewState.SelectedReplicaIndex = ""
		m.viewState.SelectedScalingGroup = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueSetView, fmt.Sprintf("pcs=%q", selectedName))

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
		m.viewState.SelectedPCSGReplicaIndex = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupView, fmt.Sprintf("pcsg=%q", selectedName))

		// Load PCSG replica indexes (mirrors PCS replica flow)
		cmds = append(cmds,
			loadPCSGReplicasCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadEventsForPCSGCmd(m.provider, m.ctx, selectedName, selectedNamespace),
		)

	case "PodCliqueScalingGroupReplica":
		// Extract replica index from name (format: "pcsgName-replica-INDEX")
		parts := strings.Split(selectedName, "-replica-")
		if len(parts) == 2 {
			oldViewType := m.viewState.ViewType
			m.viewState.ViewType = data.PodCliqueScalingGroupReplicaView
			m.viewState.SelectedPCSGReplicaIndex = parts[1]
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupReplicaView, fmt.Sprintf("pcsg-replica=%q", parts[1]))

			// Load PodCliques for this PCSG replica and events scoped to it
			cmds = append(cmds,
				loadPCSGReplicaChildrenCmd(m.provider, m.ctx, m.viewState.SelectedScalingGroup, selectedNamespace, parts[1]),
				loadEventsForPCSGReplicaCmd(m.provider, m.ctx, m.viewState.SelectedScalingGroup, selectedNamespace, parts[1]),
			)
		}

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

// =============================================================================
// Command Mode (vim-style ":" lens switching with autocomplete)
// =============================================================================

// lensCommand represents a named lens (view) that can be switched to via command mode.
type lensCommand struct {
	Name string // canonical name shown in UI, e.g. "topology"
}

// lensCommands is the registry of all available lens commands, sorted alphabetically.
// This is the single source of truth for command-mode autocomplete and matching.
var lensCommands = []lensCommand{
	{Name: "forest"},
	{Name: "topology"},
}

// LensCommandNames returns the list of available command names.
// Exported for testing.
func LensCommandNames() []string {
	names := make([]string, len(lensCommands))
	for i, c := range lensCommands {
		names[i] = c.Name
	}
	return names
}

// matchLensCommand finds the best lens command matching the given prefix.
// Returns the matching command name and true if exactly one command matches.
// Returns ("", false) if zero or multiple commands match.
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

// completeLensCommand returns the longest common prefix among all lens commands
// that match the given input prefix. Used for tab-completion.
// If no commands match, returns the original prefix unchanged.
// If exactly one command matches, returns its full name.
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
	// Find longest common prefix among matches
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

// executeCommand attempts to execute a command string by matching it to a lens
// and switching the view. Returns the updated model and any tea.Cmd.
func (m Model) executeCommand(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return m, nil
	}

	// Try exact match first, then prefix match
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
		if m.viewState.ViewType == data.TopologyView {
			m.viewState.ViewType = data.ForestView
			m.activePane = data.ResourcesPane
			m.updateTableFocus()
		}
		// If already in forest hierarchy, go back to root
		if m.viewState.ViewType != data.ForestView {
			m.viewState.ViewType = data.ForestView
			m.viewState.SelectedPodCliqueSet = ""
			m.viewState.SelectedReplicaIndex = ""
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""
			m.cachedTopologyInfo = nil
			m.cachedPods = nil
			m.cachedNodeLabels = nil
			m.activePane = data.ResourcesPane
			m.updateTableFocus()
			m.rebuildResourcesTable()
			m.rebuildEventsTable()
			return m, loadForestDataCmd(m.provider, m.ctx)
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
			// Capture the replica index we came from — cursor will land on this row
			returningToReplicaIndex := m.viewState.SelectedReplicaIndex
			m.viewState.ViewType = data.PodCliqueSetView
			m.viewState.SelectedReplicaIndex = ""
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueSetView, "")

			// Load replicas data; events scoped to the replica cursor will land on
			cmds = append(cmds,
				loadReplicasCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, namespace),
			)
			if returningToReplicaIndex != "" {
				cmds = append(cmds,
					loadEventsForReplicaCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, namespace, returningToReplicaIndex),
				)
			}
		}

	case data.PodCliqueScalingGroupView:
		// Go back to PodCliqueSetReplica
		namespace := m.resolveNamespace()
		// Capture the PCSG we came from — the cursor will land on this row
		returningToScalingGroup := m.viewState.SelectedScalingGroup
		m.viewState.ViewType = data.PodCliqueSetReplicaView
		m.viewState.SelectedScalingGroup = ""
		m.viewState.SelectedPCSGReplicaIndex = ""
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, "")

		// Load events scoped to the PCSG row the cursor will land on
		if returningToScalingGroup != "" {
			cmds = append(cmds,
				loadEventsForPCSGCmd(m.provider, m.ctx, returningToScalingGroup, namespace),
			)
		} else {
			cmds = append(cmds,
				loadEventsForReplicaCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, namespace, m.viewState.SelectedReplicaIndex),
			)
		}

	case data.PodCliqueScalingGroupReplicaView:
		// Check if we should go back to PodCliqueScalingGroupView or PodCliqueSetReplicaView
		// (if there's only 1 PCSG replica, we skip PodCliqueScalingGroupView)
		pcsgKey := "PodCliqueScalingGroup/" + m.viewState.SelectedScalingGroup
		pcsgResources := m.allResources[pcsgKey]
		namespace := m.resolveNamespace()

		if len(pcsgResources) == 1 {
			// Only 1 PCSG replica, so we came directly from PodCliqueSetReplicaView
			// Capture the PCSG we came from — the cursor will land on this row
			returningToScalingGroup := m.viewState.SelectedScalingGroup
			m.viewState.ViewType = data.PodCliqueSetReplicaView
			m.viewState.SelectedScalingGroup = ""
			m.viewState.SelectedPCSGReplicaIndex = ""

			debugLogStateTransition(oldViewType, data.PodCliqueSetReplicaView, "single PCSG replica skip")

			// Load events scoped to the PCSG row the cursor will land on
			if returningToScalingGroup != "" {
				cmds = append(cmds,
					loadEventsForPCSGCmd(m.provider, m.ctx, returningToScalingGroup, namespace),
				)
			} else {
				cmds = append(cmds,
					loadEventsForReplicaCmd(m.provider, m.ctx, m.viewState.SelectedPodCliqueSet, namespace, m.viewState.SelectedReplicaIndex),
				)
			}
		} else {
			// Multiple PCSG replicas, go back to PodCliqueScalingGroupView
			// Capture the PCSG replica index — cursor will land on this row
			returningToPCSGReplicaIndex := m.viewState.SelectedPCSGReplicaIndex
			m.viewState.ViewType = data.PodCliqueScalingGroupView
			m.viewState.SelectedPCSGReplicaIndex = ""
			m.viewState.SelectedPodClique = ""
			m.viewState.SelectedPod = ""

			debugLogStateTransition(oldViewType, data.PodCliqueScalingGroupView, "")

			// Load events scoped to the PCSG replica the cursor will land on
			if returningToPCSGReplicaIndex != "" {
				cmds = append(cmds,
					loadPCSGReplicasCmd(m.provider, m.ctx, m.viewState.SelectedScalingGroup, namespace),
					loadEventsForPCSGReplicaCmd(m.provider, m.ctx, m.viewState.SelectedScalingGroup, namespace, returningToPCSGReplicaIndex),
				)
			} else {
				cmds = append(cmds,
					loadPCSGReplicasCmd(m.provider, m.ctx, m.viewState.SelectedScalingGroup, namespace),
					loadEventsForPCSGCmd(m.provider, m.ctx, m.viewState.SelectedScalingGroup, namespace),
				)
			}
		}

	case data.PodCliqueView:
		// Go back to parent — determine which level based on view state
		namespace := m.resolveNamespace()
		// Capture the PodClique we came from — the cursor will land on this row
		returningToPodClique := m.viewState.SelectedPodClique
		var newViewType data.ViewType
		if m.viewState.SelectedScalingGroup != "" && m.viewState.SelectedPCSGReplicaIndex != "" {
			newViewType = data.PodCliqueScalingGroupReplicaView
		} else if m.viewState.SelectedScalingGroup != "" {
			// Shouldn't happen in normal flow but handle gracefully
			newViewType = data.PodCliqueScalingGroupView
		} else {
			newViewType = data.PodCliqueSetReplicaView
		}
		m.viewState.ViewType = newViewType
		m.viewState.SelectedPodClique = ""
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, newViewType, "")

		// Load events scoped to the PodClique row the cursor will land on
		if returningToPodClique != "" {
			cmds = append(cmds,
				loadEventsForPodCliqueCmd(m.provider, m.ctx, returningToPodClique, namespace),
			)
		}

	case data.PodView:
		// Go back to PodClique view
		namespace := m.resolveNamespace()
		m.viewState.ViewType = data.PodCliqueView
		m.viewState.SelectedPod = ""

		debugLogStateTransition(oldViewType, data.PodCliqueView, "")

		// Reload events for the PodClique
		cmds = append(cmds,
			loadEventsForPodCliqueCmd(m.provider, m.ctx, m.viewState.SelectedPodClique, namespace),
		)
	}

	m.rebuildResourcesTable()
	m.rebuildEventsTable()

	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	return m, nil
}
