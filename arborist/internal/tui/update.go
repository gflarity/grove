package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/data"
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
		m.yamlOverlay.Active, m.lensEditActive, m.commandActive, m.filterActive)

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
