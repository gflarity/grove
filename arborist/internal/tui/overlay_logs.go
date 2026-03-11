package tui

import (
	"fmt"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handleLogsExec handles the 'l' key press to open logs for a pod/container.
func (m Model) handleLogsExec() (tea.Model, tea.Cmd) {
	exec, ok := m.behavior().(LogsExecutor)
	if !ok {
		return m, nil
	}
	return exec.LogsExec(&m)
}

// openLogsOverlay initializes the logs overlay state.
func (m *Model) openLogsOverlay(podName, containerName, namespace string) {
	loadingMsg := "# Loading logs for " + podName + "/" + containerName + "..."
	m.logsOverlay.Open(loadingMsg, m.width, m.height)
	m.logsOverlay.ContentTransform = m.logsContentTransform
	m.logsOverlay.SearchTransform = m.logsSearchTransform
	m.logsOverlay.PodName = podName
	m.logsOverlay.ContainerName = containerName
	m.logsOverlay.WrapEnabled = false
	m.logsOverlay.Namespace = namespace
	m.logsOverlay.AutoScroll = false
	m.logsOverlay.HorizontalOffset = 0
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
	if !m.logsOverlay.Active || !m.logsOverlay.AutoScroll {
		return m, nil
	}
	return m, tea.Batch(
		loadPodLogsCmd(m.ctx, m.cache, m.logsOverlay.PodName, m.logsOverlay.Namespace, m.logsOverlay.ContainerName, 1000),
		logsAutoScrollTickCmd(),
	)
}

// handleLogsRequest handles LogsRequestMsg — opens overlay and loads logs.
func (m Model) handleLogsRequest(msg LogsRequestMsg) (tea.Model, tea.Cmd) {
	m.openLogsOverlay(msg.PodName, msg.Container, msg.Namespace)

	debugLogWithContext("opening logs overlay via request: pod=%s container=%s", msg.PodName, msg.Container)
	return m, loadPodLogsCmd(m.ctx, m.cache, msg.PodName, msg.Namespace, msg.Container, 1000)
}

// handleLogsOverlayKey handles keys when the logs overlay is active.
func (m Model) handleLogsOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle logs-specific keys before delegating to common overlay handler.
	// Search-active state is handled by HandleKeyMsg below, so only intercept
	// logs-specific keys when search is NOT active.
	if !m.logsOverlay.SearchActive {
		switch msg.Type {
		case tea.KeyLeft:
			if !m.logsOverlay.WrapEnabled && m.logsOverlay.HorizontalOffset > 0 {
				m.logsOverlay.HorizontalOffset -= logsHorizontalScrollStep
				if m.logsOverlay.HorizontalOffset < 0 {
					m.logsOverlay.HorizontalOffset = 0
				}
				m.updateLogsViewportContent()
			}
			return m, nil

		case tea.KeyRight:
			if !m.logsOverlay.WrapEnabled {
				m.logsOverlay.HorizontalOffset += logsHorizontalScrollStep
				m.updateLogsViewportContent()
			}
			return m, nil

		case tea.KeyRunes:
			switch msg.String() {
			case "w", "W":
				m.logsOverlay.WrapEnabled = !m.logsOverlay.WrapEnabled
				m.logsOverlay.HorizontalOffset = 0
				m.updateLogsViewportContent()
				debugLogWithContext("logs wrap toggled: %v", m.logsOverlay.WrapEnabled)
				return m, nil
			case "s", "S":
				m.logsOverlay.AutoScroll = !m.logsOverlay.AutoScroll
				debugLogWithContext("logs autoscroll toggled: %v", m.logsOverlay.AutoScroll)
				if m.logsOverlay.AutoScroll {
					return m, tea.Batch(
						logsAutoScrollTickCmd(),
						loadPodLogsCmd(m.ctx, m.cache, m.logsOverlay.PodName, m.logsOverlay.Namespace, m.logsOverlay.ContainerName, 1000),
					)
				}
				return m, nil
			}
		}
	}

	// Delegate search-active handling + common keys to the unified handler
	handled, cmd := m.logsOverlay.HandleKeyMsg(msg)
	if handled {
		if !m.logsOverlay.Active {
			m.logsOverlay.AutoScroll = false
			debugLogWithContext("logs overlay closed")
		}
		return m, cmd
	}

	return m, nil
}

// updateLogsViewportContent sets the viewport content from logsContent,
// applying carriage-return resolution, wrap/truncation, and search highlighting.
func (m *Model) updateLogsViewportContent() {
	m.logsOverlay.UpdateViewportContent()
}

// logsContentTransform applies CR resolution + wrap/horizontal-slice to log content.
func (m *Model) logsContentTransform(content string) string {
	// Resolve carriage returns first — progress bars (tqdm, etc.) use \r to
	// overwrite the current line. Without this, the terminal interprets \r
	// literally, moving the cursor to column 0 and corrupting the frame border.
	content = resolveCarriageReturns(content)

	if m.logsOverlay.WrapEnabled && m.logsOverlay.Viewport.Width > 0 {
		content = wrapText(content, m.logsOverlay.Viewport.Width)
	} else if m.logsOverlay.Viewport.Width > 0 {
		// Apply horizontal sliding window so the user can pan left/right
		// through long lines (like k9s). Falls back to simple truncation
		// when offset is 0.
		content = horizontalSlice(content, m.logsOverlay.HorizontalOffset, m.logsOverlay.Viewport.Width)
	}
	return content
}

// logsSearchTransform applies CR + wrap (not horizontal slice) for search matching.
func (m *Model) logsSearchTransform(content string) string {
	content = resolveCarriageReturns(content)
	if m.logsOverlay.WrapEnabled && m.logsOverlay.Viewport.Width > 0 {
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
	m.logsOverlay.ApplySearch()
}

// logsSearchNext jumps to the next (or previous) search match.
func (m *Model) logsSearchNext(reverse bool) {
	m.logsOverlay.SearchNext(reverse)
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

// podActionAvailable returns true when pod actions (logs, shell) should be shown.
// This is true in ContainersView, or when a Pod is selected in the resources table
// for views that support pod actions.
func (m Model) podActionAvailable() bool {
	if _, ok := m.behavior().(LogsExecutor); !ok {
		return false
	}
	if m.viewState.ViewType == clusterstate.ContainersView {
		return true
	}
	selectedRow := m.resourcesTable.SelectedRow()
	return len(selectedRow) >= 2 && selectedRow[1] == clusterstate.ResourceTypePod
}
