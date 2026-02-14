package tui

import (
	"fmt"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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

		m.openLogsOverlay(m.viewState.SelectedPod, containerName, namespace)

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

// openLogsOverlay initializes the logs overlay state.
func (m *Model) openLogsOverlay(podName, containerName, namespace string) {
	loadingMsg := "# Loading logs for " + podName + "/" + containerName + "..."
	m.logsOverlay.Open(loadingMsg, m.width, m.height)
	m.logsPodName = podName
	m.logsContainerName = containerName
	m.logsWrapEnabled = false
	m.logsNamespace = namespace
	m.logsAutoScroll = false
	m.logsHorizontalOffset = 0
	m.logsOverlay.Viewport.GotoBottom()
}

// handleLogsContent handles LogsContentMsg — stores log content and updates viewport.
func (m Model) handleLogsContent(msg LogsContentMsg) (tea.Model, tea.Cmd) {
	if !m.logsOverlay.Active {
		return m, nil
	}

	if msg.Err != nil {
		debugLogWithContext("ERROR loading logs: %v", msg.Err)
		m.logsOverlay.Content = fmt.Sprintf("# Error loading logs for %s/%s: %v", msg.PodName, msg.Container, msg.Err)
		m.addError(fmt.Sprintf("Failed to load logs for %s/%s: %v", msg.PodName, msg.Container, msg.Err))
	} else {
		debugLogWithContext("loaded %d bytes of logs for %s/%s", len(msg.Content), msg.PodName, msg.Container)
		m.logsOverlay.Content = msg.Content
		if m.logsOverlay.Content == "" {
			m.logsOverlay.Content = "# No logs available"
		}
	}
	m.updateLogsViewportContent()
	m.logsOverlay.Viewport.GotoBottom()
	return m, nil
}

// handleLogsAutoScrollTick handles logsAutoScrollTickMsg — re-fetches logs and schedules the next tick.
// If autoscroll is off or the overlay is closed, returns nil to break the tick chain.
func (m Model) handleLogsAutoScrollTick() (tea.Model, tea.Cmd) {
	if !m.logsOverlay.Active || !m.logsAutoScroll {
		return m, nil
	}
	return m, tea.Batch(
		loadPodLogsCmd(m.cache, m.ctx, m.logsPodName, m.logsNamespace, m.logsContainerName, 1000),
		logsAutoScrollTickCmd(),
	)
}

// handleLogsRequest handles LogsRequestMsg — opens overlay and loads logs.
func (m Model) handleLogsRequest(msg LogsRequestMsg) (tea.Model, tea.Cmd) {
	m.openLogsOverlay(msg.PodName, msg.Container, msg.Namespace)

	debugLogWithContext("opening logs overlay via request: pod=%s container=%s", msg.PodName, msg.Container)
	return m, loadPodLogsCmd(m.cache, m.ctx, msg.PodName, msg.Namespace, msg.Container, 1000)
}

// handleLogsOverlayKey handles keys when the logs overlay is active.
func (m Model) handleLogsOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If search is active, delegate to the overlay's search handler
	if m.logsOverlay.SearchActive {
		needsUpdate, cmd := m.logsOverlay.HandleSearchKey(msg)
		if needsUpdate {
			m.updateLogsViewportContent()
			if m.logsOverlay.SearchText != "" {
				m.applyLogsSearch()
			}
			debugLogWithContext("logs search %s: %q",
				map[bool]string{true: "applied", false: "cancelled"}[m.logsOverlay.SearchText != ""],
				m.logsOverlay.SearchText)
		}
		return m, cmd
	}

	// Handle logs-specific keys before delegating to common overlay handler
	switch msg.Type {
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
	}

	// Delegate common keys (Esc/q, viewport nav, search, n/N) to the overlay model
	handled, cmd := m.logsOverlay.HandleKey(msg)
	if handled {
		if !m.logsOverlay.Active {
			m.logsAutoScroll = false
			debugLogWithContext("logs overlay closed")
		}
		return m, cmd
	}

	return m, nil
}

// updateLogsViewportContent sets the viewport content from logsContent,
// applying carriage-return resolution, wrap/truncation, and search highlighting.
func (m *Model) updateLogsViewportContent() {
	m.logsOverlay.UpdateViewportContentWithTransform(m.logsContentTransform)
}

// logsContentTransform applies CR resolution + wrap/horizontal-slice to log content.
func (m *Model) logsContentTransform(content string) string {
	// Resolve carriage returns first — progress bars (tqdm, etc.) use \r to
	// overwrite the current line. Without this, the terminal interprets \r
	// literally, moving the cursor to column 0 and corrupting the frame border.
	content = resolveCarriageReturns(content)

	if m.logsWrapEnabled && m.logsOverlay.Viewport.Width > 0 {
		content = wrapText(content, m.logsOverlay.Viewport.Width)
	} else if m.logsOverlay.Viewport.Width > 0 {
		// Apply horizontal sliding window so the user can pan left/right
		// through long lines (like k9s). Falls back to simple truncation
		// when offset is 0.
		content = horizontalSlice(content, m.logsHorizontalOffset, m.logsOverlay.Viewport.Width)
	}
	return content
}

// logsSearchTransform applies CR + wrap (not horizontal slice) for search matching.
func (m *Model) logsSearchTransform(content string) string {
	content = resolveCarriageReturns(content)
	if m.logsWrapEnabled && m.logsOverlay.Viewport.Width > 0 {
		content = wrapText(content, m.logsOverlay.Viewport.Width)
	}
	return content
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
	m.logsOverlay.ApplySearchWithTransform(m.logsSearchTransform)
}

// logsSearchNext jumps to the next (or previous) search match.
func (m *Model) logsSearchNext(reverse bool) {
	m.logsOverlay.SearchNextWithTransform(reverse, m.logsSearchTransform)
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
