package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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
			clusterstate.ViewTypeName(updated.viewState.ViewType),
			clusterstate.PaneName(updated.activePane),
			hasCmd)
	}

	return model, cmd
}

// resizeLayout recalculates pane heights and resizes all tables/viewports to
// fit the current terminal dimensions. Must be called whenever the terminal
// size changes OR any element that affects layout height changes (e.g. error
// log appearing/disappearing, filter bar toggling).
func (m *Model) resizeLayout() {
	availableHeight := m.height - m.fixedLayoutLines()
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
	m.viewInput.Width = m.width / 3 // view input sits inline in the header, so keep it compact
	m.yamlOverlay.SearchInput.Width = m.width - 6

	// Resize YAML overlay viewport
	m.yamlOverlay.Viewport.Width = frameContentWidth
	m.yamlOverlay.Viewport.Height = m.height - 6 // room for header/footer

	// Resize logs overlay viewport
	m.logsOverlay.Viewport.Width = frameContentWidth
	m.logsOverlay.Viewport.Height = m.height - 6 // room for header/footer
	m.logsOverlay.SearchInput.Width = m.width - 6
}

// handleWindowSize handles terminal resize events.
func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	m.resizeLayout()

	// Rebuild tables
	m.rebuildResourcesTable()
	m.rebuildEventsTable()
	if m.viewState.ViewType == clusterstate.TopologyView {
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
			return m, startGlobalCacheCmd(m.ctx, m.cache)
		}
	}

	return m, nil
}

// handleKeyMsg handles keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	debugLogWithContext("handleKeyMsg: type=%d(%s) runes=%q alt=%v modes=[yaml=%v view=%v cmd=%v filter=%v]",
		msg.Type, msg.Type.String(), string(msg.Runes), msg.Alt,
		m.yamlOverlay.Active, m.viewEditActive, m.commandActive, m.filterActive)

	// Ctrl+C always quits
	if msg.Type == tea.KeyCtrlC {
		debugLogWithContext("quitting (ctrl+c)")
		return m, tea.Quit
	}

	// Logs overlay mode has its own key handling
	if m.logsOverlay.Active {
		return m.handleLogsOverlayKey(msg)
	}

	// YAML overlay mode has its own key handling
	if m.yamlOverlay.Active {
		return m.handleYAMLOverlayKey(msg)
	}

	// View edit mode has its own key handling (inline in header)
	if m.viewEditActive {
		return m.handleViewEditKey(msg)
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
//
// 3-phase dispatch:
//  1. Tab — global pane switch (always handled here)
//  2. behavior().HandleKey — view-specific Esc/Enter/Up/Down
//  3. Global rune keys — q, t, /, :, !, y, s, l, v (unchanged across views)
//
// The cascading mode checks in handleKeyMsg (overlay → lens → command →
// filter → normal) look like they should be refactored into a state machine,
// but the linear priority chain is actually simpler and more readable — each
// mode completely owns input when active.
func (m Model) handleNormalModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	debugLogWithContext("handleNormalModeKey: keyType=%d(%s) str=%q view=%s pane=%s drillDepth=%d",
		msg.Type, msg.Type.String(), msg.String(),
		clusterstate.ViewTypeName(m.viewState.ViewType), clusterstate.PaneName(m.activePane),
		m.topologyDrill.Depth())

	// Phase 1: Tab — global pane switch
	if msg.Type == tea.KeyTab {
		m.switchPane()
		return m, nil
	}

	// Phase 2: View-specific key handling (Esc, Enter, Up/Down)
	model, cmd, handled := m.behavior().HandleKey(&m, msg)
	if handled {
		return model, cmd
	}

	// Phase 3: Global rune keys
	if msg.Type == tea.KeyRunes {
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
			m.viewEditActive = true
			m.viewInput.SetValue("")
			m.viewInput.Focus()
			debugLogWithContext("view edit mode activated")
			return m, textinput.Blink
		}
	}

	return m, nil
}

// handleCommandModeKey handles keys when command mode is active.
func (m Model) handleCommandModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return handleTextInputKey(&m, msg, &m.commandInput, &m.commandActive, "command mode", commandModeCommands, m.commandAutocomplete)
}

// handleViewEditKey handles keys when view edit mode is active (inline in header).
func (m Model) handleViewEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return handleTextInputKey(&m, msg, &m.viewInput, &m.viewEditActive, "view edit mode", viewCommands, m.viewAutocomplete)
}

// handleTextInputKey is the shared handler for text input modes (command, view edit).
// Esc cancels, Enter executes the typed command, other keys update the input.
func handleTextInputKey(m *Model, msg tea.KeyMsg, input *textinput.Model, active *bool, modeName string, commands []viewCommand, ac *Autocompleter) (tea.Model, tea.Cmd) {
	debugLogWithContext("handle %s key: keyType=%d(%s) str=%q inputValue=%q",
		modeName, msg.Type, msg.Type.String(), msg.String(), input.Value())

	switch msg.Type {
	case tea.KeyEsc:
		*active = false
		input.SetValue("")
		debugLogWithContext("%s deactivated (cancelled)", modeName)
		return *m, nil

	case tea.KeyEnter:
		value := input.Value()
		*active = false
		input.SetValue("")
		debugLogWithContext("%s executing: %q", modeName, value)
		return m.executeCommand(value, commands, ac)

	default:
		var cmd tea.Cmd
		*input, cmd = input.Update(msg)
		return *m, cmd
	}
}

// toggleTopologyView switches between Forest and Topology views.
func (m Model) toggleTopologyView() (tea.Model, tea.Cmd) {
	debugLogWithContext("toggleTopologyView: current view=%s", clusterstate.ViewTypeName(m.viewState.ViewType))
	if m.viewState.ViewType == clusterstate.TopologyView {
		// Switching FROM topology back to forest — always allowed
		m.viewState.ViewType = clusterstate.ForestView
		m.activePane = clusterstate.ResourcesPane
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

	m.viewState.ViewType = clusterstate.TopologyView
	m.activePane = clusterstate.TopologyDomainsPane
	m.topologyDrill.Reset()
	m.updateTableFocus()
	debugLogWithContext("toggled to TopologyView")

	// Rebuild from existing snapshot
	if m.cachedSnapshot != nil {
		m.topologyViewData = m.cachedSnapshot.TopologyViewData
		if m.cachedSnapshot.TopologyViewData != nil {
			m.gpuSummary = m.cachedSnapshot.TopologyViewData.GPUSummary
		}
		m.rebuildTopologyDomainsTable()
		m.rebuildTopologyPodsTable()
	}
	return m, nil
}

// handleShellExec handles the 's' key press to shell into a container.
func (m Model) handleShellExec() (tea.Model, tea.Cmd) {
	exec, ok := m.behavior().(ShellExecutor)
	if !ok {
		return m, nil
	}
	return exec.ShellExec(&m)
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

// viewportSearchFirst scrolls a viewport to the first line containing searchText.
func viewportSearchFirst(content, searchText string, vp *viewport.Model) {
	if searchText == "" || content == "" {
		return
	}

	searchLower := strings.ToLower(searchText)
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), searchLower) {
			targetLine := i - vp.Height/2
			if targetLine < 0 {
				targetLine = 0
			}
			vp.SetYOffset(targetLine)
			return
		}
	}
}

// viewportSearchNext scrolls a viewport to the next (or previous, if reverse)
// line containing searchText, wrapping around at the end/beginning.
func viewportSearchNext(content, searchText string, vp *viewport.Model, reverse bool) {
	if searchText == "" || content == "" {
		return
	}

	searchLower := strings.ToLower(searchText)
	lines := strings.Split(content, "\n")
	currentLine := vp.YOffset + vp.Height/2

	if reverse {
		for i := currentLine - 1; i >= 0; i-- {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - vp.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				vp.SetYOffset(targetLine)
				return
			}
		}
		// Wrap around
		for i := len(lines) - 1; i >= currentLine; i-- {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - vp.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				vp.SetYOffset(targetLine)
				return
			}
		}
	} else {
		for i := currentLine + 1; i < len(lines); i++ {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - vp.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				vp.SetYOffset(targetLine)
				return
			}
		}
		// Wrap around
		for i := 0; i <= currentLine; i++ {
			if strings.Contains(strings.ToLower(lines[i]), searchLower) {
				targetLine := i - vp.Height/2
				if targetLine < 0 {
					targetLine = 0
				}
				vp.SetYOffset(targetLine)
				return
			}
		}
	}
}

// highlightSearchMatches returns content with all occurrences of searchText
// highlighted. Matching is case-insensitive. Shared by YAML and logs overlays.
func highlightSearchMatches(content, searchText string) string {
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

// updateEventsForSelection updates allEvents based on the currently highlighted
// resource row, reading from the cache snapshot synchronously.
func (m *Model) updateEventsForSelection() {
	if m.cachedSnapshot == nil {
		return
	}
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
}
