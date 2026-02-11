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

	// Cache messages
	case CacheSyncedMsg:
		return m.handleCacheSynced(msg)

	case CacheUpdateMsg:
		return m.handleCacheUpdate(msg)

	case PodYAMLMsg:
		return m.handlePodYAML(msg)

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
	fixedLines := 12
	if m.filterActive {
		fixedLines += 3
	}
	if m.commandActive {
		fixedLines += 3
	}
	availableHeight := m.height - fixedLines
	paneHeight := availableHeight / 2
	if paneHeight < 3 {
		paneHeight = 3
	}

	frameContentWidth := m.width - 2
	resizeTable(&m.resourcesTable, frameContentWidth, paneHeight)
	resizeTable(&m.eventsTable, frameContentWidth, paneHeight)
	resizeTable(&m.topologyDomainsTable, frameContentWidth, paneHeight)
	resizeTable(&m.topologyPodsTable, frameContentWidth, paneHeight)

	m.podViewport.Width = frameContentWidth
	m.podViewport.Height = paneHeight

	m.filterInput.Width = m.width - 6
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

		// Start global cache eagerly after first window size.
		if !m.cacheStarted && m.cache != nil {
			m.cacheStarted = true
			debugLogWithContext("starting global cache")
			return m, startGlobalCacheCmd(m.cache, m.ctx)
		}
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
		m.filterActive = false
		m.filterText = ""
		m.filterInput.SetValue("")
		m.rebuildResourcesTable()
		debugLogWithContext("filter mode deactivated (cleared)")
		return m, nil

	case tea.KeyEnter:
		m.filterActive = false
		m.filterText = m.filterInput.Value()
		m.rebuildResourcesTable()
		debugLogWithContext("filter mode deactivated (applied: %q)", m.filterText)
		return m, nil

	case tea.KeyTab:
		m.switchPane()
		return m, nil

	default:
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
		if m.viewState.ViewType == data.TopologyView && len(m.topologyDrillStack) == 0 {
			m.viewState.ViewType = data.ForestView
			m.activePane = data.ResourcesPane
			m.updateTableFocus()
			debugLogWithContext("switched from TopologyView to ForestView via Esc")
			return m, nil
		}
		if m.viewState.ViewType == data.TopologyView {
			m.topologyDrillBack()
			return m, nil
		}
		return m.navigateBack()

	case tea.KeyEnter:
		if m.viewState.ViewType == data.TopologyView && m.activePane == data.TopologyDomainsPane {
			m.topologyDrillInto()
			return m, nil
		}
		if m.activePane == data.ResourcesPane {
			return m.navigateInto()
		}
		return m, nil

	case tea.KeyUp, tea.KeyDown:
		if m.viewState.ViewType == data.TopologyView {
			if m.activePane == data.TopologyDomainsPane {
				var cmd tea.Cmd
				m.topologyDomainsTable, cmd = m.topologyDomainsTable.Update(msg)
				m.rebuildTopologyPodsTable()
				return m, cmd
			}
			var cmd tea.Cmd
			m.topologyPodsTable, cmd = m.topologyPodsTable.Update(msg)
			return m, cmd
		}

		if m.activePane == data.ResourcesPane {
			if m.viewState.ViewType == data.PodView {
				var cmd tea.Cmd
				m.podViewport, cmd = m.podViewport.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.resourcesTable, cmd = m.resourcesTable.Update(msg)
			// Update events based on new selection (from cache, synchronous)
			m.updateEventsForSelection()
			m.rebuildEventsTable()
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
		m.commandActive = false
		m.commandInput.SetValue("")
		debugLogWithContext("command mode deactivated (cancelled)")
		return m, nil

	case tea.KeyEnter:
		input := m.commandInput.Value()
		m.commandActive = false
		m.commandInput.SetValue("")
		debugLogWithContext("command mode executing: %q", input)
		return m.executeCommand(input)

	case tea.KeyTab:
		current := m.commandInput.Value()
		completed := completeLensCommand(current)
		if completed != current {
			m.commandInput.SetValue(completed)
			m.commandInput.CursorEnd()
			debugLogWithContext("command mode tab-complete: %q -> %q", current, completed)
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.commandInput, cmd = m.commandInput.Update(msg)
		return m, cmd
	}
}

// toggleTopologyView switches between Forest and Topology views.
func (m Model) toggleTopologyView() (tea.Model, tea.Cmd) {
	if m.viewState.ViewType == data.TopologyView {
		m.viewState.ViewType = data.ForestView
		m.activePane = data.ResourcesPane
		m.updateTableFocus()
		debugLogWithContext("toggled from TopologyView to ForestView")
		return m, nil
	}

	m.viewState.ViewType = data.TopologyView
	m.activePane = data.TopologyDomainsPane
	m.topologyDrillStack = nil
	m.updateTableFocus()
	debugLogWithContext("toggled to TopologyView")

	// Rebuild from existing snapshot
	if m.cachedSnapshot != nil {
		m.topologyViewData = m.cachedSnapshot.TopologyViewData
		m.gpuSummary = m.cachedSnapshot.GPUSummary
		m.rebuildTopologyDomainsTable()
		m.rebuildTopologyPodsTable()
	}
	return m, nil
}

// updateEventsForSelection updates allEvents based on the currently highlighted
// resource row, reading from the cache snapshot synchronously.
func (m *Model) updateEventsForSelection() {
	if m.cachedSnapshot == nil {
		return
	}
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
}
