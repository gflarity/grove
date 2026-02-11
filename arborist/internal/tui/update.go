package tui

import (
	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Update handles messages and updates the model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Update debug context and log the message
	debugSetContext(m.viewState.ViewType, m.activePane, m.filterActive)
	debugLogMsg(msg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	// Data messages
	case ForestDataMsg:
		return m.handleForestData(msg)

	case ReplicaDataMsg:
		return m.handleReplicaData(msg)

	case ReplicaChildrenMsg:
		return m.handleReplicaChildren(msg)

	case PCSGReplicaDataMsg:
		return m.handlePCSGReplicaData(msg)

	case PCSGChildrenMsg:
		return m.handlePCSGChildren(msg)

	case PodCliqueChildrenMsg:
		return m.handlePodCliqueChildren(msg)

	case EventsMsg:
		return m.handleEvents(msg)

	case PodYAMLMsg:
		return m.handlePodYAML(msg)

	case TopologyInfoMsg:
		return m.handleTopologyInfo(msg)

	case PodInfoMsg:
		return m.handlePodInfo(msg)

	case NodeLabelsMsg:
		return m.handleNodeLabels(msg)

	case TopologyCacheSyncedMsg:
		return m.handleTopologyCacheSynced(msg)

	case TopologyViewDataMsg:
		return m.handleTopologyViewData(msg)

	case ErrorMsg:
		debugLog("ERROR: %s: %v", msg.Operation, msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	return m, nil
}

// handleWindowSize handles terminal resize events.
func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	// Calculate table heights.
	// Layout (lines):
	//   6  header (left column: Context, Cluster, User, Arborist Rev, K8s Rev, View)
	//   2  resources frame border (top + bottom)
	//       (section header is embedded in the top border — no extra line)
	//   1  resources table column header (NAMESPACE, TYPE, ...)
	//   2  events frame border (top + bottom)
	//       (section header is embedded in the top border — no extra line)
	//   1  events table column header (TYPE, REASON, ...)
	//  --
	//  12  total fixed lines
	//
	// The remaining height is split equally between the two table data areas.
	fixedLines := 12
	if m.filterActive {
		fixedLines += 3 // filter frame: top border + content + bottom border
	}
	if m.commandActive {
		fixedLines += 3 // command frame: top border + content + bottom border
	}
	availableHeight := m.height - fixedLines
	paneHeight := availableHeight / 2
	if paneHeight < 3 {
		paneHeight = 3
	}

	// Table dimensions — frame uses .Width(m.width - 2), so the content area
	// inside the border is m.width - 2. Tables and highlight should match.
	frameContentWidth := m.width - 2
	resizeTable(&m.resourcesTable, frameContentWidth, paneHeight)
	resizeTable(&m.eventsTable, frameContentWidth, paneHeight)
	resizeTable(&m.topologyDomainsTable, frameContentWidth, paneHeight)
	resizeTable(&m.topologyPodsTable, frameContentWidth, paneHeight)

	// Update viewport for pod view
	m.podViewport.Width = frameContentWidth
	m.podViewport.Height = paneHeight

	// Update filter input width (frame content width minus tree emoji)
	m.filterInput.Width = m.width - 6
	// Update command input width (frame content width minus prompt)
	m.commandInput.Width = m.width - 6

	// Rebuild tables
	m.rebuildResourcesTable()
	m.rebuildEventsTable()
	if m.viewState.ViewType == data.TopologyView {
		m.rebuildTopologyDomainsTable()
		m.rebuildTopologyPodsTable()
	}

	if !m.ready {
		m.ready = true
		debugLogWithContext("window ready: %dx%d", m.width, m.height)
	}

	return m, nil
}

// handleKeyMsg handles keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C always quits
	if msg.Type == tea.KeyCtrlC {
		debugLogWithContext("quitting (ctrl+c)")
		return m, tea.Quit
	}

	// Command mode has different key handling
	if m.commandActive {
		return m.handleCommandModeKey(msg)
	}

	// Filter mode has different key handling
	if m.filterActive {
		return m.handleFilterModeKey(msg)
	}

	// Normal mode key handling
	return m.handleNormalModeKey(msg)
}

// handleFilterModeKey handles keys when filter mode is active.
func (m Model) handleFilterModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Exit filter mode and clear filter
		m.filterActive = false
		m.filterText = ""
		m.filterInput.SetValue("")
		m.rebuildResourcesTable()
		debugLogWithContext("filter mode deactivated (cleared)")
		return m, nil

	case tea.KeyEnter:
		// Exit filter mode but keep filter applied
		m.filterActive = false
		m.filterText = m.filterInput.Value()
		m.rebuildResourcesTable()
		debugLogWithContext("filter mode deactivated (applied: %q)", m.filterText)
		return m, nil

	case tea.KeyTab:
		// Allow pane switching while filtering
		m.switchPane()
		return m, nil

	default:
		// Pass other keys to the filter input
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.filterText = m.filterInput.Value()
		m.rebuildResourcesTable()
		return m, cmd
	}
}

// handleNormalModeKey handles keys in normal (non-filter) mode.
func (m Model) handleNormalModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab:
		m.switchPane()
		return m, nil

	case tea.KeyEsc:
		// In Topology view with empty drill stack, switch back to Forest
		if m.viewState.ViewType == data.TopologyView && len(m.topologyDrillStack) == 0 {
			m.viewState.ViewType = data.ForestView
			m.activePane = data.ResourcesPane
			m.updateTableFocus()
			debugLogWithContext("switched from TopologyView to ForestView via Esc")
			return m, nil
		}
		// In Topology view with drill stack, pop back
		if m.viewState.ViewType == data.TopologyView {
			m.topologyDrillBack()
			return m, nil
		}
		return m.navigateBack()

	case tea.KeyEnter:
		// Topology view: drill into domains
		if m.viewState.ViewType == data.TopologyView && m.activePane == data.TopologyDomainsPane {
			m.topologyDrillInto()
			return m, nil
		}
		if m.activePane == data.ResourcesPane {
			return m.navigateInto()
		}
		return m, nil

	case tea.KeyUp, tea.KeyDown:
		// Topology view key handling
		if m.viewState.ViewType == data.TopologyView {
			if m.activePane == data.TopologyDomainsPane {
				var cmd tea.Cmd
				m.topologyDomainsTable, cmd = m.topologyDomainsTable.Update(msg)
				// Rebuild pods table based on new domain selection
				m.rebuildTopologyPodsTable()
				return m, cmd
			}
			var cmd tea.Cmd
			m.topologyPodsTable, cmd = m.topologyPodsTable.Update(msg)
			return m, cmd
		}

		// Forest view key handling
		if m.activePane == data.ResourcesPane {
			if m.viewState.ViewType == data.PodView {
				var cmd tea.Cmd
				m.podViewport, cmd = m.podViewport.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.resourcesTable, cmd = m.resourcesTable.Update(msg)
			// Load/filter events for newly selected resource
			if eventsCmd := m.eventsCommandForSelection(); eventsCmd != nil {
				return m, tea.Batch(cmd, eventsCmd)
			}
			return m, cmd
		}
		var cmd tea.Cmd
		m.eventsTable, cmd = m.eventsTable.Update(msg)
		return m, cmd

	case tea.KeyRunes:
		switch msg.String() {
		case "q", "Q":
			debugLogWithContext("quitting (q)")
			return m, tea.Quit
		case "t", "T":
			return m.toggleTopologyView()
		case "/":
			m.filterActive = true
			m.filterInput.SetValue(m.filterText)
			m.filterInput.Focus()
			debugLogWithContext("filter mode activated")
			return m, textinput.Blink
		case ":":
			m.commandActive = true
			m.commandInput.SetValue("")
			m.commandInput.Focus()
			debugLogWithContext("command mode activated")
			return m, textinput.Blink
		}
	}

	return m, nil
}

// handleCommandModeKey handles keys when command mode is active.
func (m Model) handleCommandModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Exit command mode without executing
		m.commandActive = false
		m.commandInput.SetValue("")
		debugLogWithContext("command mode deactivated (cancelled)")
		return m, nil

	case tea.KeyEnter:
		// Execute the command and exit command mode
		input := m.commandInput.Value()
		m.commandActive = false
		m.commandInput.SetValue("")
		debugLogWithContext("command mode executing: %q", input)
		return m.executeCommand(input)

	case tea.KeyTab:
		// Tab-complete the current input
		current := m.commandInput.Value()
		completed := completeLensCommand(current)
		if completed != current {
			m.commandInput.SetValue(completed)
			m.commandInput.CursorEnd()
			debugLogWithContext("command mode tab-complete: %q -> %q", current, completed)
		}
		return m, nil

	default:
		// Pass other keys to the command input
		var cmd tea.Cmd
		m.commandInput, cmd = m.commandInput.Update(msg)
		return m, cmd
	}
}

// toggleTopologyView switches between Forest and Topology views.
func (m Model) toggleTopologyView() (tea.Model, tea.Cmd) {
	if m.viewState.ViewType == data.TopologyView {
		// Switch back to Forest
		m.viewState.ViewType = data.ForestView
		m.activePane = data.ResourcesPane
		m.updateTableFocus()
		debugLogWithContext("toggled from TopologyView to ForestView")
		return m, nil
	}

	// Switch to Topology view (only from ForestView or any forest sub-view)
	m.viewState.ViewType = data.TopologyView
	m.activePane = data.TopologyDomainsPane
	m.topologyDrillStack = nil // reset drill state
	m.updateTableFocus()
	debugLogWithContext("toggled to TopologyView")

	// Start cache if not already started
	if !m.topologyCacheStarted && m.topologyCache != nil {
		m.topologyCacheStarted = true
		debugLogWithContext("starting topology cache")
		return m, startTopologyCacheCmd(m.topologyCache, m.ctx)
	}

	// Cache already running — rebuild from existing snapshot
	if m.topologyCache != nil {
		m.topologyViewData = m.topologyCache.Snapshot()
		m.rebuildTopologyDomainsTable()
		m.rebuildTopologyPodsTable()
	}
	return m, nil
}
