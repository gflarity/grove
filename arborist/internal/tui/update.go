package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Update handles messages and updates the model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Update debug context and log the message
	debugSetContext(m.viewState.ViewType, m.activePane, m.filterActive)
	debugLogMsg(msg)

	var model tea.Model
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		model, cmd = m.handleWindowSize(msg)

	case tea.KeyMsg:
		model, cmd = m.handleKeyMsg(msg)

	// Cache messages
	case CacheSyncedMsg:
		model, cmd = m.handleCacheSynced(msg)

	case CacheUpdateMsg:
		model, cmd = m.handleCacheUpdate(msg)

	case PodYAMLMsg:
		model, cmd = m.handlePodYAML(msg)

	case ResourceYAMLMsg:
		model, cmd = m.handleResourceYAML(msg)

	case PodContainersMsg:
		model, cmd = m.handlePodContainers(msg)

	case LogsContentMsg:
		model, cmd = m.handleLogsContent(msg)

	case LogsRequestMsg:
		model, cmd = m.handleLogsRequest(msg)

	case ShellRequestMsg:
		model, cmd = m.handleShellRequest(msg)

	case ShellExitMsg:
		model, cmd = m.handleShellExit(msg)

	case logsAutoScrollTickMsg:
		model, cmd = m.handleLogsAutoScrollTick()

	case ErrorMsg:
		debugLog("ERROR: %s: %v", msg.Operation, msg.Err)
		m.lastError = msg.Err
		m.addError(fmt.Sprintf("%s: %v", msg.Operation, msg.Err))
		// If startGlobalCache failed, also set cacheSynced = true so the TUI
		// doesn't hang on "Syncing..." forever.
		if msg.Operation == "startGlobalCache" {
			m.cacheSynced = true
		}
		model, cmd = m, nil

	default:
		model, cmd = m, nil
	}

	// Log the outcome: what view we're in now and whether a command was returned.
	if updated, ok := model.(Model); ok {
		hasCmd := cmd != nil
		debugLogWithContext("Update result: view=%s pane=%s hasCmd=%v",
			data.ViewTypeName(updated.viewState.ViewType),
			data.PaneName(updated.activePane),
			hasCmd)
	}

	return model, cmd
}

// resizeLayout recalculates pane heights and resizes all tables/viewports to
// fit the current terminal dimensions. Must be called whenever the terminal
// size changes OR any element that affects layout height changes (e.g. error
// log appearing/disappearing, filter bar toggling).
func (m *Model) resizeLayout() {
	// Calculate table heights.
	// 7(header: context+cluster+user+arborist+k8s+namespace+lens) + 2*(2 border + 1 table header) = 13
	fixedLines := 13
	if m.filterActive {
		fixedLines += 3
	}
	if m.commandActive {
		fixedLines += 3
	}
	fixedLines += m.errorLogFrameHeight()
	// Account for footnote line in topology view with GPU columns
	if m.viewState.ViewType == data.TopologyView && m.topologyHasGPUColumns() {
		fixedLines++
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

	// Resize logs overlay viewport
	m.logsViewport.Width = frameContentWidth
	m.logsViewport.Height = m.height - 6 // room for header/footer
	m.logsSearchInput.Width = m.width - 6
}

// handleWindowSize handles terminal resize events.
func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	m.resizeLayout()

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
	debugLogWithContext("handleKeyMsg: type=%d(%s) runes=%q alt=%v modes=[yaml=%v lens=%v cmd=%v filter=%v]",
		msg.Type, msg.Type.String(), string(msg.Runes), msg.Alt,
		m.yamlOverlayActive, m.lensEditActive, m.commandActive, m.filterActive)

	// Ctrl+C always quits
	if msg.Type == tea.KeyCtrlC {
		debugLogWithContext("quitting (ctrl+c)")
		return m, tea.Quit
	}

	// Logs overlay mode has its own key handling
	if m.logsOverlayActive {
		return m.handleLogsOverlayKey(msg)
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
	debugLogWithContext("handleNormalModeKey: keyType=%d(%s) str=%q view=%s pane=%s drillDepth=%d",
		msg.Type, msg.Type.String(), msg.String(),
		data.ViewTypeName(m.viewState.ViewType), data.PaneName(m.activePane),
		len(m.topologyDrillStack))

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
		case "!":
			m.errorLogVisible = !m.errorLogVisible
			m.resizeLayout()
			debugLogWithContext("error log toggled: visible=%v", m.errorLogVisible)
			return m, nil
		case "y", "Y":
			return m.openYAMLOverlay()
		case "s", "S":
			return m.handleShellExec()
		case "l", "L":
			return m.handleLogsExec()
		case "v", "V":
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
	debugLogWithContext("handleCommandModeKey: keyType=%d(%s) str=%q inputValue=%q",
		msg.Type, msg.Type.String(), msg.String(), m.commandInput.Value())

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
		debugLogWithContext("command mode executing: %q (commandActive now=%v)", input, m.commandActive)
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
	debugLogWithContext("handleLensEditKey: keyType=%d(%s) str=%q inputValue=%q",
		msg.Type, msg.Type.String(), msg.String(), m.lensInput.Value())

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
		debugLogWithContext("lens edit mode executing: %q (lensEditActive now=%v)", input, m.lensEditActive)
		return m.executeCommand(input)

	default:
		var cmd tea.Cmd
		m.lensInput, cmd = m.lensInput.Update(msg)
		return m, cmd
	}
}

// toggleTopologyView switches between Forest and Topology views.
func (m Model) toggleTopologyView() (tea.Model, tea.Cmd) {
	debugLogWithContext("toggleTopologyView: current view=%s", data.ViewTypeName(m.viewState.ViewType))
	if m.viewState.ViewType == data.TopologyView {
		// Switching FROM topology back to forest — always allowed
		m.viewState.ViewType = data.ForestView
		m.activePane = data.ResourcesPane
		m.updateTableFocus()
		debugLogWithContext("toggled from TopologyView to ForestView")
		return m, nil
	}

	// Switching TO topology — guard: must have topology data
	if !m.topologyAvailable() {
		m.addError("Topology unavailable — no ClusterTopology resource found")
		debugLogWithContext("toggleTopologyView: topology unavailable, staying in current view")
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
	case "(PodCliqueSet replica)":
		actualType = "PodCliqueSet"
		actualName = m.viewState.SelectedPodCliqueSet
	case "(PodCliqueScalingGroup replica)":
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

	// For PodView or ContainersView, the selected resource is the pod itself
	if m.viewState.ViewType == data.PodView || m.viewState.ViewType == data.ContainersView {
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

// handleShellExec handles the 's' key press to shell into a container.
func (m Model) handleShellExec() (tea.Model, tea.Cmd) {
	switch m.viewState.ViewType {
	case data.ContainersView:
		// Get selected container row
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) < 3 {
			return m, nil
		}
		containerName := selectedRow[0]
		containerState := selectedRow[2]
		if containerState != "Running" {
			m.addError(fmt.Sprintf("Cannot shell into container %q — state is %s", containerName, containerState))
			return m, nil
		}
		namespace := m.resolveNamespace()
		c := exec.Command("kubectl", "exec", "-it", m.viewState.SelectedPod, "-n", namespace, "-c", containerName, "--", "/bin/sh") //nolint:gosec // user-initiated shell exec
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return ShellExitMsg{Err: err}
		})

	case data.PodCliqueView:
		// Check if a Pod is selected
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) < 3 || selectedRow[1] != "Pod" {
			return m, nil
		}
		podName := selectedRow[2]
		namespace := selectedRow[0]
		return m, fetchFirstRunningContainerCmd(m.cache, m.ctx, podName, namespace)

	case data.ForestView:
		// Check if a Pod is selected in a pod lens
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) < 3 || selectedRow[1] != "Pod" {
			return m, nil
		}
		podName := selectedRow[2]
		namespace := selectedRow[0]
		return m, fetchFirstRunningContainerCmd(m.cache, m.ctx, podName, namespace)

	default:
		return m, nil
	}
}

// handlePodContainers handles PodContainersMsg — stores containers and rebuilds table.
func (m Model) handlePodContainers(msg PodContainersMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading pod containers: %v", msg.Err)
		m.addError(fmt.Sprintf("Failed to load containers for %s: %v", msg.PodName, msg.Err))
		return m, nil
	}

	debugLogWithContext("loaded %d containers for pod %s", len(msg.Containers), msg.PodName)
	m.containerInfos = msg.Containers
	m.rebuildContainersTable()
	return m, nil
}

// handleShellRequest handles ShellRequestMsg — launches kubectl exec.
func (m Model) handleShellRequest(msg ShellRequestMsg) (tea.Model, tea.Cmd) {
	debugLogWithContext("launching shell: pod=%s ns=%s container=%s", msg.PodName, msg.Namespace, msg.Container)
	c := exec.Command("kubectl", "exec", "-it", msg.PodName, "-n", msg.Namespace, "-c", msg.Container, "--", "/bin/sh") //nolint:gosec // user-initiated shell exec
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return ShellExitMsg{Err: err}
	})
}

// handleShellExit handles ShellExitMsg — shell process exited.
func (m Model) handleShellExit(msg ShellExitMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("shell exited with error: %v", msg.Err)
		m.addError(fmt.Sprintf("Shell exited: %v", msg.Err))
	} else {
		debugLogWithContext("shell exited cleanly")
	}
	return m, nil
}

// handleLogsExec handles the 'l' key press to open logs for a pod/container.
func (m Model) handleLogsExec() (tea.Model, tea.Cmd) {
	switch m.viewState.ViewType {
	case data.ContainersView:
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) < 3 {
			return m, nil
		}
		containerName := selectedRow[0]
		namespace := m.resolveNamespace()

		m.logsOverlayActive = true
		m.logsPodName = m.viewState.SelectedPod
		m.logsContainerName = containerName
		m.logsContent = "# Loading logs for " + m.viewState.SelectedPod + "/" + containerName + "..."
		m.logsSearchActive = false
		m.logsSearchText = ""
		m.logsSearchInput.SetValue("")
		m.logsWrapEnabled = false
		m.logsNamespace = namespace
		m.logsAutoScroll = false
		m.logsHorizontalOffset = 0

		m.logsViewport.Width = m.width - 4
		m.logsViewport.Height = m.height - 6
		m.logsViewport.SetContent(m.logsContent)
		m.logsViewport.GotoBottom()

		debugLogWithContext("opening logs overlay: pod=%s container=%s", m.viewState.SelectedPod, containerName)
		return m, loadPodLogsCmd(m.cache, m.ctx, m.viewState.SelectedPod, namespace, containerName, 1000)

	case data.PodCliqueView:
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) < 3 || selectedRow[1] != "Pod" {
			return m, nil
		}
		podName := selectedRow[2]
		namespace := selectedRow[0]
		return m, fetchFirstContainerForLogsCmd(m.cache, m.ctx, podName, namespace)

	case data.ForestView:
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) < 3 || selectedRow[1] != "Pod" {
			return m, nil
		}
		podName := selectedRow[2]
		namespace := selectedRow[0]
		return m, fetchFirstContainerForLogsCmd(m.cache, m.ctx, podName, namespace)

	default:
		return m, nil
	}
}

// handleLogsContent handles LogsContentMsg — stores log content and updates viewport.
func (m Model) handleLogsContent(msg LogsContentMsg) (tea.Model, tea.Cmd) {
	if !m.logsOverlayActive {
		return m, nil
	}

	if msg.Err != nil {
		debugLogWithContext("ERROR loading logs: %v", msg.Err)
		m.logsContent = fmt.Sprintf("# Error loading logs for %s/%s: %v", msg.PodName, msg.Container, msg.Err)
	} else {
		debugLogWithContext("loaded %d bytes of logs for %s/%s", len(msg.Content), msg.PodName, msg.Container)
		m.logsContent = msg.Content
		if m.logsContent == "" {
			m.logsContent = "# No logs available"
		}
	}
	m.updateLogsViewportContent()
	m.logsViewport.GotoBottom()
	return m, nil
}

// handleLogsAutoScrollTick handles logsAutoScrollTickMsg — re-fetches logs and schedules the next tick.
// If autoscroll is off or the overlay is closed, returns nil to break the tick chain.
func (m Model) handleLogsAutoScrollTick() (tea.Model, tea.Cmd) {
	if !m.logsOverlayActive || !m.logsAutoScroll {
		return m, nil
	}
	return m, tea.Batch(
		loadPodLogsCmd(m.cache, m.ctx, m.logsPodName, m.logsNamespace, m.logsContainerName, 1000),
		logsAutoScrollTickCmd(),
	)
}

// handleLogsRequest handles LogsRequestMsg — opens overlay and loads logs.
func (m Model) handleLogsRequest(msg LogsRequestMsg) (tea.Model, tea.Cmd) {
	m.logsOverlayActive = true
	m.logsPodName = msg.PodName
	m.logsContainerName = msg.Container
	m.logsContent = "# Loading logs for " + msg.PodName + "/" + msg.Container + "..."
	m.logsSearchActive = false
	m.logsSearchText = ""
	m.logsSearchInput.SetValue("")
	m.logsWrapEnabled = false
	m.logsNamespace = msg.Namespace
	m.logsAutoScroll = false
	m.logsHorizontalOffset = 0

	m.logsViewport.Width = m.width - 4
	m.logsViewport.Height = m.height - 6
	m.logsViewport.SetContent(m.logsContent)
	m.logsViewport.GotoBottom()

	debugLogWithContext("opening logs overlay via request: pod=%s container=%s", msg.PodName, msg.Container)
	return m, loadPodLogsCmd(m.cache, m.ctx, msg.PodName, msg.Namespace, msg.Container, 1000)
}

// handleLogsOverlayKey handles keys when the logs overlay is active.
func (m Model) handleLogsOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.logsSearchActive {
		return m.handleLogsSearchKey(msg)
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.logsOverlayActive = false
		m.logsContent = ""
		m.logsAutoScroll = false
		debugLogWithContext("logs overlay closed")
		return m, nil

	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		var cmd tea.Cmd
		m.logsViewport, cmd = m.logsViewport.Update(msg)
		return m, cmd

	case tea.KeyLeft:
		if !m.logsWrapEnabled && m.logsHorizontalOffset > 0 {
			m.logsHorizontalOffset -= logsHorizontalScrollStep
			if m.logsHorizontalOffset < 0 {
				m.logsHorizontalOffset = 0
			}
			m.updateLogsViewportContent()
		}
		return m, nil

	case tea.KeyRight:
		if !m.logsWrapEnabled {
			m.logsHorizontalOffset += logsHorizontalScrollStep
			m.updateLogsViewportContent()
		}
		return m, nil

	case tea.KeyRunes:
		switch msg.String() {
		case "q", "Q":
			m.logsOverlayActive = false
			m.logsContent = ""
			m.logsAutoScroll = false
			debugLogWithContext("logs overlay closed (q)")
			return m, nil
		case "/":
			m.logsSearchActive = true
			m.logsSearchInput.SetValue(m.logsSearchText)
			m.logsSearchInput.Focus()
			debugLogWithContext("logs search mode activated")
			return m, textinput.Blink
		case "n":
			if m.logsSearchText != "" {
				m.logsSearchNext(false)
			}
			return m, nil
		case "N":
			if m.logsSearchText != "" {
				m.logsSearchNext(true)
			}
			return m, nil
		case "w", "W":
			m.logsWrapEnabled = !m.logsWrapEnabled
			m.logsHorizontalOffset = 0
			m.updateLogsViewportContent()
			debugLogWithContext("logs wrap toggled: %v", m.logsWrapEnabled)
			return m, nil
		case "s", "S":
			m.logsAutoScroll = !m.logsAutoScroll
			debugLogWithContext("logs autoscroll toggled: %v", m.logsAutoScroll)
			if m.logsAutoScroll {
				return m, tea.Batch(
					logsAutoScrollTickCmd(),
					loadPodLogsCmd(m.cache, m.ctx, m.logsPodName, m.logsNamespace, m.logsContainerName, 1000),
				)
			}
			return m, nil
		}

	case tea.KeyCtrlD:
		m.logsViewport.HalfViewDown()
		return m, nil

	case tea.KeyCtrlU:
		m.logsViewport.HalfViewUp()
		return m, nil
	}

	return m, nil
}

// handleLogsSearchKey handles keys when the logs search input is active.
func (m Model) handleLogsSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.logsSearchActive = false
		m.logsSearchText = ""
		m.logsSearchInput.SetValue("")
		m.updateLogsViewportContent()
		debugLogWithContext("logs search cancelled")
		return m, nil

	case tea.KeyEnter:
		m.logsSearchActive = false
		m.logsSearchText = m.logsSearchInput.Value()
		m.updateLogsViewportContent()
		if m.logsSearchText != "" {
			m.applyLogsSearch()
		}
		debugLogWithContext("logs search applied: %q", m.logsSearchText)
		return m, nil

	default:
		var cmd tea.Cmd
		m.logsSearchInput, cmd = m.logsSearchInput.Update(msg)
		return m, cmd
	}
}

// updateLogsViewportContent sets the viewport content from logsContent,
// applying carriage-return resolution, wrap/truncation, and search highlighting.
func (m *Model) updateLogsViewportContent() {
	content := m.logsContent

	// Resolve carriage returns first — progress bars (tqdm, etc.) use \r to
	// overwrite the current line. Without this, the terminal interprets \r
	// literally, moving the cursor to column 0 and corrupting the frame border.
	content = resolveCarriageReturns(content)

	if m.logsWrapEnabled && m.logsViewport.Width > 0 {
		content = wrapText(content, m.logsViewport.Width)
	} else if m.logsViewport.Width > 0 {
		// Apply horizontal sliding window so the user can pan left/right
		// through long lines (like k9s). Falls back to simple truncation
		// when offset is 0.
		content = horizontalSlice(content, m.logsHorizontalOffset, m.logsViewport.Width)
	}
	if m.logsSearchText != "" {
		content = highlightSearch(content, m.logsSearchText)
	}
	m.logsViewport.SetContent(content)
}

// resolveCarriageReturns processes \r characters in log output the way a
// terminal would: for each line, the last \r-separated segment is what the
// user would actually see. This prevents progress bar output from corrupting
// the TUI frame borders.
func resolveCarriageReturns(text string) string {
	if !strings.Contains(text, "\r") {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if idx := strings.LastIndex(line, "\r"); idx >= 0 {
			lines[i] = line[idx+1:]
		}
	}
	return strings.Join(lines, "\n")
}

// logsHorizontalScrollStep is how many runes left/right arrow scrolls.
const logsHorizontalScrollStep = 8

// horizontalSlice applies a horizontal window to text: for each line, skip
// `offset` runes from the left, then truncate to `maxWidth` visual columns
// using lipgloss (which correctly handles ANSI escape sequences and wide
// characters). This gives the user a panning view of long lines (like k9s).
func horizontalSlice(text string, offset, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		// Apply horizontal offset: skip `offset` runes from the left
		if offset > 0 {
			runes := []rune(line)
			if offset >= len(runes) {
				lines[i] = ""
				continue
			}
			line = string(runes[offset:])
		}
		// Truncate to maxWidth visual columns (lipgloss-aware, handles ANSI)
		if lipgloss.Width(line) > maxWidth {
			lines[i] = lipgloss.NewStyle().MaxWidth(maxWidth).Render(line)
		} else {
			lines[i] = line
		}
	}
	return strings.Join(lines, "\n")
}

// truncateLines clips each line to maxWidth visual columns using lipgloss,
// which correctly handles multi-byte characters and ANSI escape sequences.
func truncateLines(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > maxWidth {
			lines[i] = lipgloss.NewStyle().MaxWidth(maxWidth).Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// applyLogsSearch scrolls to the first search match.
func (m *Model) applyLogsSearch() {
	if m.logsSearchText == "" || m.logsContent == "" {
		return
	}

	content := m.logsContent
	if m.logsWrapEnabled && m.logsViewport.Width > 0 {
		content = wrapText(content, m.logsViewport.Width)
	}

	searchLower := strings.ToLower(m.logsSearchText)
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), searchLower) {
			targetLine := i - m.logsViewport.Height/2
			if targetLine < 0 {
				targetLine = 0
			}
			m.logsViewport.SetYOffset(targetLine)
			return
		}
	}
}

// logsSearchNext jumps to the next (or previous) search match.
func (m *Model) logsSearchNext(reverse bool) {
	if m.logsSearchText == "" || m.logsContent == "" {
		return
	}

	content := m.logsContent
	if m.logsWrapEnabled && m.logsViewport.Width > 0 {
		content = wrapText(content, m.logsViewport.Width)
	}

	searchLower := strings.ToLower(m.logsSearchText)
	lines := strings.Split(content, "\n")
	currentLine := m.logsViewport.YOffset + m.logsViewport.Height/2

	if reverse {
		for i := currentLine - 1; i >= 0; i-- {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - m.logsViewport.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				m.logsViewport.SetYOffset(targetLine)
				return
			}
		}
		for i := len(lines) - 1; i >= currentLine; i-- {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - m.logsViewport.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				m.logsViewport.SetYOffset(targetLine)
				return
			}
		}
	} else {
		for i := currentLine + 1; i < len(lines); i++ {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - m.logsViewport.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				m.logsViewport.SetYOffset(targetLine)
				return
			}
		}
		for i := 0; i <= currentLine; i++ {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - m.logsViewport.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				m.logsViewport.SetYOffset(targetLine)
				return
			}
		}
	}
}

// wrapText hard-wraps lines that exceed maxWidth.
func wrapText(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		for len(line) > maxWidth {
			result = append(result, line[:maxWidth])
			line = line[maxWidth:]
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// highlightSearch returns content with all occurrences of searchText
// highlighted using YAMLSearchHighlightStyle. Matching is case-insensitive.
func highlightSearch(content, searchText string) string {
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

		var result strings.Builder
		pos := 0
		for {
			idx := strings.Index(strings.ToLower(line[pos:]), searchLower)
			if idx == -1 {
				result.WriteString(line[pos:])
				break
			}
			result.WriteString(line[pos : pos+idx])
			matchEnd := pos + idx + len(searchText)
			result.WriteString(YAMLSearchHighlightStyle.Render(line[pos+idx : matchEnd]))
			pos = matchEnd
		}
		highlighted[i] = result.String()
	}

	return strings.Join(highlighted, "\n")
}

// logsAvailable returns true when the 'l' key should be shown in the menu.
func (m Model) logsAvailable() bool {
	if m.viewState.ViewType == data.ContainersView {
		return true
	}
	if m.viewState.ViewType == data.PodCliqueView || m.viewState.ViewType == data.ForestView {
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 2 && selectedRow[1] == "Pod" {
			return true
		}
	}
	return false
}

// updateEventsForSelection updates allEvents based on the currently highlighted
// resource row, reading from the cache snapshot synchronously.
func (m *Model) updateEventsForSelection() {
	if m.cachedSnapshot == nil {
		return
	}
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
}
