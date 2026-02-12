package tui

import (
	"fmt"
	"strings"

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

	case ResourceYAMLMsg:
		return m.handleResourceYAML(msg)

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
	m.lensInput.Width = m.width / 3 // lens input sits inline in the header, so keep it compact
	m.yamlSearchInput.Width = m.width - 6

	// Resize YAML overlay viewport
	m.yamlViewport.Width = frameContentWidth
	m.yamlViewport.Height = m.height - 6 // room for header/footer

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

	// YAML overlay mode has its own key handling
	if m.yamlOverlayActive {
		return m.handleYAMLOverlayKey(msg)
	}

	// Lens edit mode has its own key handling (inline in header)
	if m.lensEditActive {
		return m.handleLensEditKey(msg)
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
		case "y", "Y":
			return m.openYAMLOverlay()
		case "l", "L":
			m.lensEditActive = true
			m.lensInput.SetValue("")
			m.lensInput.Focus()
			debugLogWithContext("lens edit mode activated")
			return m, textinput.Blink
		}
	}

	return m, nil
}

// handleCommandModeKey handles keys when command mode is active.
// Tab is no longer intercepted — it flows through to textinput.Update which
// handles AcceptSuggestion natively (fills ghost text, moves cursor to end).
// Only Enter (execute) and Esc (cancel) are intercepted.
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

	default:
		var cmd tea.Cmd
		m.commandInput, cmd = m.commandInput.Update(msg)
		return m, cmd
	}
}

// handleLensEditKey handles keys when lens edit mode is active (inline in header).
// Tab is no longer intercepted — it flows through to textinput.Update which
// handles AcceptSuggestion natively (fills ghost text, moves cursor to end).
// Only Enter (execute) and Esc (cancel) are intercepted.
func (m Model) handleLensEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.lensEditActive = false
		m.lensInput.SetValue("")
		debugLogWithContext("lens edit mode deactivated (cancelled)")
		return m, nil

	case tea.KeyEnter:
		input := m.lensInput.Value()
		m.lensEditActive = false
		m.lensInput.SetValue("")
		debugLogWithContext("lens edit mode executing: %q", input)
		return m.executeCommand(input)

	default:
		var cmd tea.Cmd
		m.lensInput, cmd = m.lensInput.Update(msg)
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

// openYAMLOverlay starts loading YAML for the currently selected resource.
func (m Model) openYAMLOverlay() (tea.Model, tea.Cmd) {
	// Determine what resource is selected
	resourceType, resourceName, namespace := m.selectedResourceInfo()
	if resourceType == "" || resourceName == "" {
		debugLogWithContext("openYAMLOverlay: no resource selected")
		return m, nil
	}

	// For virtual replica types, resolve to the actual parent resource
	actualType := resourceType
	actualName := resourceName
	switch resourceType {
	case "PodCliqueSetReplica":
		actualType = "PodCliqueSet"
		actualName = m.viewState.SelectedPodCliqueSet
	case "PodCliqueScalingGroupReplica":
		actualType = "PodCliqueScalingGroup"
		actualName = m.viewState.SelectedScalingGroup
	}

	// Set overlay state — show "Loading..." while fetching
	m.yamlOverlayActive = true
	m.yamlResourceType = resourceType
	m.yamlResourceName = resourceName
	m.yamlContent = "# Loading YAML for " + actualType + "/" + actualName + "..."
	m.yamlSearchActive = false
	m.yamlSearchText = ""
	m.yamlSearchInput.SetValue("")

	// Size the viewport
	m.yamlViewport.Width = m.width - 4 // room for border + padding
	m.yamlViewport.Height = m.height - 6 // room for header/footer
	m.yamlViewport.SetContent(m.yamlContent)
	m.yamlViewport.GotoTop()

	debugLogWithContext("openYAMLOverlay: loading %s/%s (actual: %s/%s)", resourceType, resourceName, actualType, actualName)

	return m, loadResourceYAMLCmd(m.cache, m.ctx, actualType, actualName, namespace)
}

// selectedResourceInfo returns the type, name, and namespace of the currently selected resource.
func (m Model) selectedResourceInfo() (string, string, string) {
	if m.viewState.ViewType == data.TopologyView {
		// In topology view, use the pods table if focused on pods pane
		if m.activePane == data.TopologyPodsPane {
			row := m.topologyPodsTable.SelectedRow()
			if len(row) >= 3 {
				return "Pod", row[2], row[0] // NAME at index 2, NAMESPACE at index 0
			}
		}
		return "", "", ""
	}

	// For PodView, the selected resource is the pod itself
	if m.viewState.ViewType == data.PodView {
		return "Pod", m.viewState.SelectedPod, m.resolveNamespace()
	}

	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		return "", "", ""
	}
	return selectedRow[1], selectedRow[2], selectedRow[0] // TYPE, NAME, NAMESPACE
}

// handleResourceYAML handles ResourceYAMLMsg.
func (m Model) handleResourceYAML(msg ResourceYAMLMsg) (tea.Model, tea.Cmd) {
	if !m.yamlOverlayActive {
		return m, nil
	}

	if msg.Err != nil {
		debugLogWithContext("ERROR loading resource YAML: %v", msg.Err)
		m.yamlContent = fmt.Sprintf("# Error loading YAML for %s/%s: %v", msg.ResourceType, msg.ResourceName, msg.Err)
	} else {
		debugLogWithContext("loaded %d bytes of YAML for %s/%s", len(msg.YAML), msg.ResourceType, msg.ResourceName)
		m.yamlContent = msg.YAML
	}
	m.updateYAMLViewportContent()
	m.yamlViewport.GotoTop()
	return m, nil
}

// handleYAMLOverlayKey handles keys when the YAML overlay is active.
func (m Model) handleYAMLOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If search is active in the YAML overlay, handle search input
	if m.yamlSearchActive {
		return m.handleYAMLSearchKey(msg)
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.yamlOverlayActive = false
		m.yamlContent = ""
		debugLogWithContext("YAML overlay closed")
		return m, nil

	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		var cmd tea.Cmd
		m.yamlViewport, cmd = m.yamlViewport.Update(msg)
		return m, cmd

	case tea.KeyRunes:
		switch msg.String() {
		case "q", "Q":
			m.yamlOverlayActive = false
			m.yamlContent = ""
			debugLogWithContext("YAML overlay closed (q)")
			return m, nil
		case "/":
			m.yamlSearchActive = true
			m.yamlSearchInput.SetValue(m.yamlSearchText)
			m.yamlSearchInput.Focus()
			debugLogWithContext("YAML search mode activated")
			return m, textinput.Blink
		case "n":
			// Jump to next search match
			if m.yamlSearchText != "" {
				m.yamlSearchNext(false)
			}
			return m, nil
		case "N":
			// Jump to previous search match
			if m.yamlSearchText != "" {
				m.yamlSearchNext(true)
			}
			return m, nil
		}

	case tea.KeyCtrlD:
		// Half-page down (like vim)
		var cmd tea.Cmd
		m.yamlViewport.HalfViewDown()
		return m, cmd

	case tea.KeyCtrlU:
		// Half-page up (like vim)
		var cmd tea.Cmd
		m.yamlViewport.HalfViewUp()
		return m, cmd
	}

	return m, nil
}

// handleYAMLSearchKey handles keys when the YAML search input is active.
func (m Model) handleYAMLSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.yamlSearchActive = false
		// Clear search and remove highlights
		m.yamlSearchText = ""
		m.yamlSearchInput.SetValue("")
		m.updateYAMLViewportContent()
		debugLogWithContext("YAML search cancelled")
		return m, nil

	case tea.KeyEnter:
		m.yamlSearchActive = false
		m.yamlSearchText = m.yamlSearchInput.Value()
		m.updateYAMLViewportContent()
		if m.yamlSearchText != "" {
			m.applyYAMLSearch()
		}
		debugLogWithContext("YAML search applied: %q", m.yamlSearchText)
		return m, nil

	default:
		var cmd tea.Cmd
		m.yamlSearchInput, cmd = m.yamlSearchInput.Update(msg)
		return m, cmd
	}
}

// updateYAMLViewportContent sets the viewport content from yamlContent,
// applying search highlighting if yamlSearchText is set.
func (m *Model) updateYAMLViewportContent() {
	if m.yamlSearchText == "" || m.yamlContent == "" {
		m.yamlViewport.SetContent(m.yamlContent)
		return
	}

	m.yamlViewport.SetContent(highlightYAMLSearch(m.yamlContent, m.yamlSearchText))
}

// highlightYAMLSearch returns content with all occurrences of searchText
// highlighted using YAMLSearchHighlightStyle. Matching is case-insensitive.
func highlightYAMLSearch(content, searchText string) string {
	if searchText == "" {
		return content
	}

	searchLower := strings.ToLower(searchText)
	lines := strings.Split(content, "\n")
	highlighted := make([]string, len(lines))

	for i, line := range lines {
		lineLower := strings.ToLower(line)
		if !strings.Contains(lineLower, searchLower) {
			highlighted[i] = line
			continue
		}

		// Build the line with highlighted matches
		var result strings.Builder
		pos := 0
		for {
			idx := strings.Index(strings.ToLower(line[pos:]), searchLower)
			if idx == -1 {
				result.WriteString(line[pos:])
				break
			}
			// Write text before the match
			result.WriteString(line[pos : pos+idx])
			// Write the matched text with highlight style (preserve original case)
			matchEnd := pos + idx + len(searchText)
			result.WriteString(YAMLSearchHighlightStyle.Render(line[pos+idx : matchEnd]))
			pos = matchEnd
		}
		highlighted[i] = result.String()
	}

	return strings.Join(highlighted, "\n")
}

// applyYAMLSearch scrolls to the first search match.
func (m *Model) applyYAMLSearch() {
	if m.yamlSearchText == "" || m.yamlContent == "" {
		return
	}

	searchLower := strings.ToLower(m.yamlSearchText)
	lines := strings.Split(m.yamlContent, "\n")

	// Find the first line containing the search text
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), searchLower) {
			// Scroll viewport to show this line (center it if possible)
			targetLine := i - m.yamlViewport.Height/2
			if targetLine < 0 {
				targetLine = 0
			}
			m.yamlViewport.SetYOffset(targetLine)
			return
		}
	}
}

// yamlSearchNext jumps to the next (or previous) search match.
func (m *Model) yamlSearchNext(reverse bool) {
	if m.yamlSearchText == "" || m.yamlContent == "" {
		return
	}

	searchLower := strings.ToLower(m.yamlSearchText)
	lines := strings.Split(m.yamlContent, "\n")
	currentLine := m.yamlViewport.YOffset + m.yamlViewport.Height/2

	if reverse {
		// Search backwards from current position
		for i := currentLine - 1; i >= 0; i-- {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - m.yamlViewport.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				m.yamlViewport.SetYOffset(targetLine)
				return
			}
		}
		// Wrap around
		for i := len(lines) - 1; i >= currentLine; i-- {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - m.yamlViewport.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				m.yamlViewport.SetYOffset(targetLine)
				return
			}
		}
	} else {
		// Search forwards from current position
		for i := currentLine + 1; i < len(lines); i++ {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - m.yamlViewport.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				m.yamlViewport.SetYOffset(targetLine)
				return
			}
		}
		// Wrap around
		for i := 0; i <= currentLine; i++ {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - m.yamlViewport.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				m.yamlViewport.SetYOffset(targetLine)
				return
			}
		}
	}
}

// updateEventsForSelection updates allEvents based on the currently highlighted
// resource row, reading from the cache snapshot synchronously.
func (m *Model) updateEventsForSelection() {
	if m.cachedSnapshot == nil {
		return
	}
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
}
