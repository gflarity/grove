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
	availableHeight := m.height - fixedLines
	paneHeight := availableHeight / 2
	if paneHeight < 3 {
		paneHeight = 3
	}

	// Table dimensions — frame uses .Width(m.width - 2), so the content area
	// inside the border is m.width - 2. Tables and highlight should match.
	frameContentWidth := m.width - 2
	m.resourcesTable.SetWidth(frameContentWidth)
	m.resourcesTable.SetHeight(paneHeight)
	m.eventsTable.SetWidth(frameContentWidth)
	m.eventsTable.SetHeight(paneHeight)

	// Update table styles so the selected-row highlight spans the full frame width
	styledWidth := ArboristTableStylesWithWidth(frameContentWidth)
	m.resourcesTable.SetStyles(styledWidth)
	m.eventsTable.SetStyles(styledWidth)

	// Update viewport for pod view
	m.podViewport.Width = frameContentWidth
	m.podViewport.Height = paneHeight

	// Update filter input width (frame content width minus tree emoji)
	m.filterInput.Width = m.width - 6

	// Rebuild tables
	m.rebuildResourcesTable()
	m.rebuildEventsTable()

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
		return m.navigateBack()

	case tea.KeyEnter:
		if m.activePane == data.ResourcesPane {
			return m.navigateInto()
		}
		return m, nil

	case tea.KeyUp, tea.KeyDown:
		// Pass to active table
		if m.activePane == data.ResourcesPane {
			if m.viewState.ViewType == data.PodView {
				var cmd tea.Cmd
				m.podViewport, cmd = m.podViewport.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.resourcesTable, cmd = m.resourcesTable.Update(msg)
			// Load events for newly selected resource
			m.loadEventsForSelection()
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
		case "/":
			m.filterActive = true
			m.filterInput.SetValue(m.filterText)
			m.filterInput.Focus()
			debugLogWithContext("filter mode activated")
			return m, textinput.Blink
		}
	}

	return m, nil
}
