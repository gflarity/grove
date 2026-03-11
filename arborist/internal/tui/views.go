package tui

import (
	"fmt"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	"github.com/charmbracelet/lipgloss"
)

// errorLogFrameHeight returns the height consumed by the error log frame when visible.
// Frame border (top + bottom) = 2, plus one line per error entry (minimum 1 for the empty state).
func (m Model) errorLogFrameHeight() int {
	if !m.errorLogVisible {
		return 0
	}
	entries := len(m.errorLog)
	if entries == 0 {
		entries = 1 // empty-state placeholder line
	}
	return 2 + entries // border top + border bottom + content lines
}

// fixedLayoutLines returns the number of terminal lines consumed by fixed-height
// UI elements (header, frame borders, table headers, filter/command bars, error
// log, topology footnote). Both View() and resizeLayout() use this to calculate
// how much vertical space remains for the two main panes.
func (m Model) fixedLayoutLines() int {
	// 7(header: context+cluster+user+arborist+k8s+namespace+view) + 2*(2 border + 1 table header) = 13
	fixed := 13
	if m.filterActive {
		fixed += 3 // filter frame: top border + content + bottom border
	}
	if m.commandActive {
		fixed += 3 // command frame: top border + content + bottom border
	}
	fixed += m.errorLogFrameHeight()
	if m.viewState.ViewType == clusterstate.TopologyView && m.topologyHasGPUColumns() {
		fixed++ // 1 line for the footnote
	}
	return fixed
}

// View renders the entire TUI. This is the main entry point for Bubble Tea rendering.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	if !m.cacheSynced && m.cache != nil {
		return "Syncing..."
	}

	// Logs overlay takes over the full screen
	if m.logsOverlay.Active {
		return m.renderLogsOverlay()
	}

	// YAML overlay takes over the full screen
	if m.yamlOverlay.Active {
		return m.renderYAMLOverlay()
	}

	var sections []string

	// Header bar
	sections = append(sections, m.renderHeaderFrame())

	// Filter bar (if active) - standalone framed box between header and resources
	if m.filterActive {
		sections = append(sections, m.renderFilterFrame())
	}

	// Command bar (if active) - standalone framed box between header and resources
	if m.commandActive {
		sections = append(sections, m.renderCommandFrame())
	}

	// Calculate available height for the main viewport.
	availableHeight := m.height - m.fixedLayoutLines()
	resourcesHeight := availableHeight / 2
	eventsHeight := availableHeight - resourcesHeight
	if resourcesHeight < 3 {
		resourcesHeight = 3
	}
	if eventsHeight < 3 {
		eventsHeight = 3
	}

	// Render view-specific panes
	sections = append(sections, m.behavior().RenderPanes(&m, resourcesHeight, eventsHeight)...)

	// Error log frame (appears at the bottom when visible)
	if m.errorLogVisible {
		sections = append(sections, m.renderErrorLogFrame())
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// viewDisplayName returns the view name. There are only two views:
// "forest" (all hierarchy views) and "topology".
func (m Model) viewDisplayName() string {
	if m.viewState.ViewType == clusterstate.TopologyView {
		return "topology"
	}
	return "forest"
}

// menuItem represents a single key/action pair in the menu/shortcuts bar.
type menuItem struct {
	key    string
	action string
}

// buildMenuItems returns the context-sensitive list of menu items for the
// current view state. This is the single source of truth — renderHeaderFrame
// and buildShortcutsString both derive their output from it.
func (m Model) buildMenuItems() []menuItem {
	items := []menuItem{
		{":", "Cmd"},
		{"v", "View"},
		{"/", "Filter"},
		{"tab", "Switch"},
	}

	if m.viewState.ViewType == clusterstate.PodView {
		items = append(items, menuItem{"↑↓", "Scroll"})
	} else {
		items = append(items, menuItem{"↑↓", "Nav"})
		items = append(items, menuItem{"enter", "Drill"})
	}

	items = append(items, menuItem{"y", "YAML"})
	if m.podActionAvailable() {
		items = append(items, menuItem{"l", "Logs"})
	}
	if m.shellAvailable() {
		items = append(items, menuItem{"s", "Shell"})
	}
	if m.topologyAvailable() {
		items = append(items, menuItem{"t", "Toggle View"})
	}
	items = append(items, menuItem{"!", "Errors"})

	if m.viewState.ViewType != clusterstate.ForestView {
		items = append(items, menuItem{"esc", "Back"})
	}

	items = append(items, menuItem{"q", "Quit"})
	return items
}

// renderHeaderFrame renders the k9s-style header with context info on the left,
// key helpers in the middle, and ASCII art logo on the right. No border frame.
func (m Model) renderHeaderFrame() string {
	// --- Left column: Context, Cluster, User, Arborist Rev, K8s Rev, View ---
	orUnknown := func(s string) string {
		if s == "" {
			return "(unknown)"
		}
		return s
	}

	// Use fixed-width labels so the values line up (longest label is "Arborist Rev:" = 13 chars)
	contextLine := HeaderLabelStyle.Render("Context:     ") + " " + HeaderValueStyle.Render(orUnknown(m.contextName))
	clusterLine := HeaderLabelStyle.Render("Cluster:     ") + " " + HeaderValueStyle.Render(orUnknown(m.clusterName))
	userLine := HeaderLabelStyle.Render("User:        ") + " " + HeaderValueStyle.Render(orUnknown(m.userName))
	arboristLine := HeaderLabelStyle.Render("Arborist Rev:") + " " + HeaderValueStyle.Render(orUnknown(m.arboristVersion))
	k8sLine := HeaderLabelStyle.Render("K8s Rev:     ") + " " + HeaderValueStyle.Render(orUnknown(m.k8sVersion))
	nsDisplay := "all"
	if !m.allNamespaces && m.namespace != "" {
		nsDisplay = m.namespace
	}
	namespaceLine := HeaderLabelStyle.Render("Namespace:   ") + " " + HeaderValueStyle.Render(nsDisplay)
	var viewLine string
	if m.viewEditActive {
		viewLine = HeaderLabelStyle.Render("View:        ") + " " + m.viewInput.View()
	} else {
		viewLine = HeaderLabelStyle.Render("View:        ") + " " + HeaderValueStyle.Render(m.viewDisplayName())
	}

	leftCol := lipgloss.JoinVertical(lipgloss.Left, contextLine, clusterLine, userLine, arboristLine, k8sLine, namespaceLine, viewLine)

	// --- Middle column: key helpers in 2 rows ---
	items := m.buildMenuItems()

	// Split items into 2 rows
	half := (len(items) + 1) / 2
	var row1Parts, row2Parts []string
	for i, item := range items {
		part := MenuKeyStyle.Render("<"+item.key+">") + MenuActionStyle.Render(item.action)
		if i < half {
			row1Parts = append(row1Parts, part)
		} else {
			row2Parts = append(row2Parts, part)
		}
	}
	row1 := strings.Join(row1Parts, "  ")
	row2 := strings.Join(row2Parts, "  ")

	middleCol := lipgloss.JoinVertical(lipgloss.Left, row1, row2)

	// --- Right column: ASCII art logo ---
	rightCol := LogoStyle.Render(ArboristASCII)

	// --- Compose columns based on terminal width ---
	if m.width < 60 {
		// Very narrow: only show left column
		return leftCol
	}

	asciiWidth := lipgloss.Width(rightCol)

	if m.width < 90 {
		// Narrow: show left + middle, skip ASCII art
		leftWidth := m.width / 2
		middleWidth := m.width - leftWidth
		leftStyled := lipgloss.NewStyle().Width(leftWidth).Render(leftCol)
		middleStyled := lipgloss.NewStyle().Width(middleWidth).Render(middleCol)
		return lipgloss.JoinHorizontal(lipgloss.Top, leftStyled, middleStyled)
	}

	// Full width: show all three columns
	leftWidth := m.width * 30 / 100
	if leftWidth < 40 {
		leftWidth = 40
	}
	middleWidth := m.width - leftWidth - asciiWidth
	if middleWidth < 20 {
		middleWidth = 20
	}

	leftStyled := lipgloss.NewStyle().Width(leftWidth).Render(leftCol)
	middleStyled := lipgloss.NewStyle().Width(middleWidth).Render(middleCol)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyled, middleStyled, rightCol)
}

// renderFilterBar renders the filter input bar (plain text, no frame).
func (m Model) renderFilterBar() string {
	return FilterBarStyle.Render("/") + " " + m.filterInput.View()
}

// renderInputFrame renders a single-line input inside a normal-bordered frame.
// Used by both the filter and command frames.
func (m Model) renderInputFrame(content string) string {
	border := lipgloss.NormalBorder()
	bc := lipgloss.NewStyle().Foreground(ColorBorderFocused)
	contentWidth := m.width - 2 // inside left + right border chars

	content = lipgloss.NewStyle().MaxWidth(contentWidth).Render(content)

	lineWidth := lipgloss.Width(content)
	pad := contentWidth - lineWidth
	if pad < 0 {
		pad = 0
	}

	topLine := bc.Render(border.TopLeft + strings.Repeat(border.Top, contentWidth) + border.TopRight)
	contentLine := bc.Render(border.Left) + content + strings.Repeat(" ", pad) + bc.Render(border.Right)
	bottomLine := bc.Render(border.BottomLeft + strings.Repeat(border.Bottom, contentWidth) + border.BottomRight)

	return topLine + "\n" + contentLine + "\n" + bottomLine
}

// renderFilterFrame renders the filter input as a standalone framed box between
// the header and the resources table, k9s-style.
func (m Model) renderFilterFrame() string {
	return m.renderInputFrame("🌲" + m.filterInput.View())
}

// renderCommandFrame renders the command input as a standalone framed box,
// vim-style.
func (m Model) renderCommandFrame() string {
	return m.renderInputFrame(m.commandInput.View())
}

// truncateStyled truncates a styled string (may contain ANSI escape sequences)
// from the right to fit within maxWidth visual columns. If truncation is needed,
// an ellipsis character is appended. Returns the original string unchanged if it
// already fits.
func truncateStyled(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	// Truncate to maxWidth-1 to leave room for the ellipsis
	truncated := lipgloss.NewStyle().MaxWidth(maxWidth - 1).Render(s)
	return truncated + "…"
}

// truncateStyledLeft truncates a styled string (may contain ANSI escape sequences)
// from the left to fit within maxWidth visual columns. If truncation is needed,
// an ellipsis character is prepended. Useful for breadcrumbs where the deepest
// (rightmost) path segment is most relevant.
//
// ANSI escape sequences are skipped as atomic units so that cut points never
// fall inside an escape sequence.
func truncateStyledLeft(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}

	runes := []rune(s)
	n := len(runes)
	i := 0

	for i < n {
		// Skip ANSI escape sequences as atomic units (\x1b[...letter)
		if runes[i] == '\x1b' && i+1 < n && runes[i+1] == '[' {
			i += 2
			for i < n {
				ch := runes[i]
				i++
				if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
					break
				}
			}
			continue
		}

		// Advance past one visible character
		i++

		// Check whether "…" + remainder fits
		remainder := string(runes[i:])
		if lipgloss.Width("…"+remainder) <= maxWidth {
			return "…" + remainder
		}
	}

	return "…"
}

// renderFrameWithTitle renders a bordered frame with the title embedded in the
// top border line, k9s-style. Example:
//
//	╭── Resources [3] Forest ───────────────────────╮
//	│ content line 1                                 │
//	│ content line 2                                 │
//	╰────────────────────────────────────────────────╯
//
// totalWidth is the desired total outer width of the rendered frame.
// If the title is too wide for the frame, it is truncated with an ellipsis.
func renderFrameWithTitle(title, content string, totalWidth int, borderColor lipgloss.Color) string {
	border := lipgloss.RoundedBorder()
	bc := lipgloss.NewStyle().Foreground(borderColor)
	contentWidth := totalWidth - 2 // subtract left + right border chars

	// --- Top border with embedded title ---
	// Max title width = contentWidth minus 2 spaces around it minus at least 1 dash on each side
	maxTitleWidth := contentWidth - 4
	if maxTitleWidth < 1 {
		maxTitleWidth = 1
	}
	title = truncateStyledLeft(title, maxTitleWidth)

	titleWidth := lipgloss.Width(title)
	// Available space for ─ characters = contentWidth minus title and 2 spaces around it
	dashSpace := contentWidth - titleWidth - 2
	if dashSpace < 2 {
		dashSpace = 2
	}
	leftDashes := dashSpace / 2
	rightDashes := dashSpace - leftDashes

	topLine := bc.Render(border.TopLeft+strings.Repeat(border.Top, leftDashes)) +
		" " + title + " " +
		bc.Render(strings.Repeat(border.Top, rightDashes)+border.TopRight)

	// --- Content lines with side borders ---
	lines := strings.Split(content, "\n")
	framedLines := make([]string, 0, len(lines))
	for _, line := range lines {
		lineWidth := lipgloss.Width(line)
		pad := contentWidth - lineWidth
		if pad < 0 {
			pad = 0
		}
		framedLine := bc.Render(border.Left) + line + strings.Repeat(" ", pad) + bc.Render(border.Right)
		framedLines = append(framedLines, framedLine)
	}

	// --- Bottom border ---
	bottomLine := bc.Render(border.BottomLeft + strings.Repeat(border.Bottom, contentWidth) + border.BottomRight)

	return topLine + "\n" + strings.Join(framedLines, "\n") + "\n" + bottomLine
}

// renderResourcesFrame renders the resources section in a framed box.
// The section header is embedded in the top border (k9s-style).
// The filter bar is now a separate framed section (see renderFilterFrame).
func (m Model) renderResourcesFrame(height int) string {
	// Section header becomes the border title
	title := m.renderResourcesSectionHeader()

	// Build the content
	content := m.renderResourcesTable()

	return renderFrameWithTitle(title, content, m.width, ColorBorderFocused)
}

// renderEventsFrame renders the events section in a framed box.
// The section header is embedded in the top border (k9s-style).
func (m Model) renderEventsFrame(height int) string {
	// Section header becomes the border title
	title := m.renderEventsSectionHeader()

	// Build the content (no section header line — it's in the border now)
	content := m.renderEventsTable()

	return renderFrameWithTitle(title, content, m.width, ColorBorderFocused)
}

// renderTopologyDomainsFrame renders the topology domains section in a framed box.
func (m Model) renderTopologyDomainsFrame(height int) string {
	title := m.renderTopologyDomainsSectionHeader()
	content := m.topologyDomainsTable.View()
	return renderFrameWithTitle(title, content, m.width, ColorBorderFocused)
}

// topologyHasGPUColumns returns true when the topology view is drilled into
// a domain level that has GPU columns (i.e., there are GPU types discovered).
func (m Model) topologyHasGPUColumns() bool {
	if m.topologyViewData == nil || m.topologyDrill.IsEmpty() {
		return false
	}
	// Check if node GPU products are present
	if len(m.topologyViewData.NodeGPUProducts) == 0 {
		return false
	}
	// Check if the current drilled-in domain actually produced GPU columns
	// by looking at the domains table columns — if more than 3 (VALUE + GPU PODS + PODS),
	// then there are GPU columns present.
	rows := m.topologyDomainsTable.Rows()
	if len(rows) == 0 {
		return false
	}
	// With GPU columns: VALUE + <gpuType>¹ ... + GPU PODS + PODS (>3)
	// Without: VALUE + GPU PODS + PODS (3)
	return len(rows[0]) > 3
}

// renderTopologyFootnote returns the footnote line explaining GPU column format.
// Only displayed when GPU columns are visible in the topology view.
func (m Model) renderTopologyFootnote() string {
	return FootnoteStyle.Render("¹ GPU: ▓▓ Grove  ░░ Other  (grove/other/total)")
}

// renderErrorLogFrame renders a framed box showing the last N errors (newest first).
// Uses DarkOrange border color to draw the eye. Shows a placeholder when empty.
func (m Model) renderErrorLogFrame() string {
	title := renderSectionHeader("Errors", len(m.errorLog), false, "")

	var content string
	if len(m.errorLog) == 0 {
		content = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("No errors")
	} else {
		var lines []string
		for _, entry := range m.errorLog {
			ts := ErrorLogTimestampStyle.Render(entry.Time.Format("15:04:05"))
			msg := ErrorLogMessageStyle.Render(entry.Message)
			lines = append(lines, ts+"  "+msg)
		}
		content = strings.Join(lines, "\n")
	}

	return renderFrameWithTitle(title, content, m.width, ColorDarkOrange)
}

// renderTopologyPodsFrame renders the topology pods section in a framed box.
func (m Model) renderTopologyPodsFrame(height int) string {
	title := m.renderTopologyPodsSectionHeader()
	content := m.topologyPodsTable.View()
	return renderFrameWithTitle(title, content, m.width, ColorBorderFocused)
}

// renderTopologyDomainsSectionHeader renders the section header for the topology domains pane.
func (m Model) renderTopologyDomainsSectionHeader() string {
	count := len(m.topologyDomainsTable.Rows())

	if m.topologyDrill.IsEmpty() {
		header := renderSectionHeader("Topology Domains", count, m.activePane == clusterstate.TopologyDomainsPane, "")
		if m.topologyViewData != nil {
			gpuSuffix := clusterstate.FormatClusterGPUHeaderSuffix(
				m.topologyViewData.NodeGPUProducts,
				m.topologyViewData.NodeGPUCapacity,
				m.topologyViewData.RawPods,
			)
			if gpuSuffix != "" {
				header += "  " + gpuSuffix
			}
		}
		return header
	}

	// Drilled in — show the current domain name and breadcrumb
	currentDomain, _ := m.currentTopologyDomain()
	breadcrumb := m.topologyBreadcrumbString()
	label := currentDomain
	if label == "" {
		label = "Topology Domains"
	}

	header := renderSectionHeader(label, count, m.activePane == clusterstate.TopologyDomainsPane, breadcrumb)
	if m.topologyViewData != nil {
		matchingNodes := m.topologyDrill.MatchingNodes(m.topologyViewData.NodeLabels)
		gpuSuffix := clusterstate.FormatScopedGPUHeaderSuffix(
			m.topologyViewData.NodeGPUProducts,
			m.topologyViewData.NodeGPUCapacity,
			m.topologyViewData.RawPods,
			matchingNodes,
		)
		if gpuSuffix != "" {
			header += "  " + gpuSuffix
		}
	}
	return header
}

// renderTopologyPodsSectionHeader renders the section header for the topology pods pane.
func (m Model) renderTopologyPodsSectionHeader() string {
	count := len(m.topologyPodsTable.Rows())
	breadcrumb := m.topologyBreadcrumbString()
	if breadcrumb == "" {
		if m.topologyDrill.IsEmpty() {
			// Top-level domain list — pods aren't scoped yet
			breadcrumb = "N/A"
		} else if selectedRow := m.topologyDomainsTable.SelectedRow(); len(selectedRow) >= 1 {
			// Drilled into a domain showing values — show the highlighted value
			breadcrumb = selectedRow[0]
		}
	}
	return renderSectionHeader("Pods", count, m.activePane == clusterstate.TopologyPodsPane, breadcrumb)
}

// renderSectionHeader renders a generic section header in k9s style.
// Example: "Resources [3] Forest > my-pcs" or "Events [5]".
// The label is styled as active or inactive. The optional suffix is appended after
// the count (e.g. breadcrumb text, filter indicator).
func renderSectionHeader(label string, count int, isActive bool, suffix string) string {
	countStr := SectionCountStyle.Render(fmt.Sprintf("[%d]", count))

	var styledLabel string
	if isActive {
		styledLabel = SectionHeaderActiveStyle.Render(label)
	} else {
		styledLabel = SectionHeaderInactiveStyle.Render(label)
	}

	result := styledLabel + " " + countStr
	if suffix != "" {
		result += " " + suffix
	}
	return result
}

// renderResourcesSectionHeader renders the resources section header in k9s style.
// Example: "Resources [3] Forest > my-pcs > replica-0"
func (m Model) renderResourcesSectionHeader() string {
	var count int
	if m.viewState.ViewType == clusterstate.ContainersView {
		count = len(m.containerInfos)
	} else {
		viewKey := m.getCurrentViewKey()
		count = len(m.allResources[viewKey])
	}
	label := "Resources"
	if m.viewState.ViewType == clusterstate.ContainersView {
		label = "Containers"
	}

	suffix := m.renderBreadcrumb()

	// Add filter indicator
	if m.filterText != "" {
		suffix += " " + SectionCountStyle.Render("|") + " " +
			FilterBarStyle.Render("filter:") + " " + m.filterText
	}

	return renderSectionHeader(label, count, m.activePane == clusterstate.ResourcesPane, suffix)
}

// renderEventsSectionHeader renders the events section header in k9s style.
// Example: "Events [5]"
func (m Model) renderEventsSectionHeader() string {
	events := m.getFilteredEvents()
	return renderSectionHeader("Events", len(events), m.activePane == clusterstate.EventsPane, "")
}

// renderResourcesTable renders the resources table.
func (m Model) renderResourcesTable() string {
	return m.resourcesTable.View()
}

// renderEventsTable renders the events table.
func (m Model) renderEventsTable() string {
	return m.eventsTable.View()
}

// renderPodViewport renders the Pod YAML viewport.
func (m Model) renderPodViewport() string {
	return m.podViewport.View()
}

// renderFullScreenOverlay renders a full-screen overlay (YAML, logs, etc.)
// with a framed viewport, search bar, and key hints. Callers build their own
// title and hints strings to allow overlay-specific customizations.
func (m Model) renderFullScreenOverlay(overlay OverlayModel, title, hints string) string {
	var sections []string

	// Add search bar if active, or persistent search indicator
	if overlay.SearchActive {
		sections = append(sections, overlay.RenderSearchFrame(m.width))
	} else if overlay.SearchText != "" {
		searchIndicator := FilterBarStyle.Render("search: " + overlay.SearchText)
		sections = append(sections, searchIndicator)
	}

	// Calculate content height: total height minus header, footer, frame borders, search
	fixedLines := 4 // frame top + bottom + title line + hints line
	if overlay.SearchActive {
		fixedLines += 3 // search frame
	} else if overlay.SearchText != "" {
		fixedLines += 1 // search indicator
	}

	contentHeight := m.height - fixedLines
	if contentHeight < 3 {
		contentHeight = 3
	}

	// Ensure viewport height matches (operates on the local copy)
	overlay.Viewport.Height = contentHeight

	// Render viewport content inside a frame
	content := overlay.Viewport.View()
	frame := renderFrameWithTitle(title, content, m.width, ColorBorderFocused)

	// Build output: search (if any) + frame + hints
	result := ""
	if len(sections) > 0 {
		result = lipgloss.JoinVertical(lipgloss.Left, sections...) + "\n"
	}
	result += frame + "\n" + hints

	return result
}

// renderYAMLOverlay renders the full-screen YAML overlay with framed viewport.
func (m Model) renderYAMLOverlay() string {
	title := SectionHeaderActiveStyle.Render("YAML") + " " +
		SectionCountStyle.Render(m.yamlOverlay.ResourceType+"/"+m.yamlOverlay.ResourceName)

	scrollPct := ""
	if m.yamlOverlay.Viewport.TotalLineCount() > 0 {
		pct := int(m.yamlOverlay.Viewport.ScrollPercent() * 100)
		scrollPct = fmt.Sprintf(" %d%%", pct)
	}

	hints := MenuKeyStyle.Render("<esc>") + MenuActionStyle.Render("Close") + "  " +
		MenuKeyStyle.Render("<↑↓>") + MenuActionStyle.Render("Scroll") + "  " +
		MenuKeyStyle.Render("</>") + MenuActionStyle.Render("Search")
	if m.yamlOverlay.SearchText != "" {
		hints += "  " + MenuKeyStyle.Render("<n/N>") + MenuActionStyle.Render("Next/Prev")
	}
	hints += "  " + SectionCountStyle.Render(scrollPct)

	return m.renderFullScreenOverlay(m.yamlOverlay.OverlayModel, title, hints)
}

// renderLogsOverlay renders the full-screen logs overlay with framed viewport.
func (m Model) renderLogsOverlay() string {
	title := SectionHeaderActiveStyle.Render("Logs") + " " +
		SectionCountStyle.Render(m.logsOverlay.PodName+"/"+m.logsOverlay.ContainerName)

	wrapStatus := "OFF"
	if m.logsOverlay.WrapEnabled {
		wrapStatus = "ON"
	}
	autoScrollStatus := "OFF"
	if m.logsOverlay.AutoScroll {
		autoScrollStatus = "ON"
	}

	scrollPct := ""
	if m.logsOverlay.Viewport.TotalLineCount() > 0 {
		pct := int(m.logsOverlay.Viewport.ScrollPercent() * 100)
		scrollPct = fmt.Sprintf(" %d%%", pct)
	}

	colIndicator := ""
	if m.logsOverlay.HorizontalOffset > 0 {
		colIndicator = fmt.Sprintf(" Col:%d", m.logsOverlay.HorizontalOffset)
	}

	hints := MenuKeyStyle.Render("<esc>") + MenuActionStyle.Render("Close") + "  " +
		MenuKeyStyle.Render("<↑↓←→>") + MenuActionStyle.Render("Scroll") + "  " +
		MenuKeyStyle.Render("<w>") + MenuActionStyle.Render("Wrap:"+wrapStatus) + "  " +
		MenuKeyStyle.Render("<s>") + MenuActionStyle.Render("AutoScroll:"+autoScrollStatus) + "  " +
		MenuKeyStyle.Render("</>") + MenuActionStyle.Render("Search")
	if m.logsOverlay.SearchText != "" {
		hints += "  " + MenuKeyStyle.Render("<n/N>") + MenuActionStyle.Render("Next/Prev")
	}
	hints += "  " + SectionCountStyle.Render(scrollPct+colIndicator)

	return m.renderFullScreenOverlay(m.logsOverlay.OverlayModel, title, hints)
}

// breadcrumbPart is a single segment of the breadcrumb trail with its style.
type breadcrumbPart struct {
	label string
	style lipgloss.Style
}

// appendBreadcrumb appends a breadcrumb part only if value is non-empty.
func appendBreadcrumb(parts *[]breadcrumbPart, value string, style lipgloss.Style) {
	if value != "" {
		*parts = append(*parts, breadcrumbPart{value, style})
	}
}

// buildBreadcrumbParts returns the breadcrumb segments for the current view state.
// Parts are derived from which ViewState fields are non-empty, avoiding the need
// for a per-ViewType switch.
func (m Model) buildBreadcrumbParts() []breadcrumbPart {
	forestStyle := BreadcrumbStyles["Forest"]
	pcsStyle := BreadcrumbStyles["PodCliqueSet"]
	pcsgStyle := BreadcrumbStyles["PodCliqueScalingGroup"]
	pcStyle := BreadcrumbStyles["PodClique"]
	podStyle := BreadcrumbStyles["Pod"]

	var parts []breadcrumbPart

	// Root segment
	if m.viewState.SelectedPodCliqueSet != "" {
		parts = append(parts, breadcrumbPart{"Forest", forestStyle})
		parts = append(parts, breadcrumbPart{m.viewState.SelectedPodCliqueSet, pcsStyle})
		if m.viewState.SelectedReplicaIndex != "" {
			parts = append(parts, breadcrumbPart{"replica-" + m.viewState.SelectedReplicaIndex, pcsStyle})
		}
	} else {
		parts = append(parts, breadcrumbPart{forestResourceTypeLabel(m.forestResourceType), forestStyle})
	}

	// PCSG segment (name + optional replica)
	appendBreadcrumb(&parts, m.viewState.SelectedScalingGroup, pcsgStyle)
	if m.viewState.SelectedPCSGReplicaIndex != "" {
		parts = append(parts, breadcrumbPart{"replica-" + m.viewState.SelectedPCSGReplicaIndex, pcsgStyle})
	}

	appendBreadcrumb(&parts, m.viewState.SelectedPodClique, pcStyle)
	appendBreadcrumb(&parts, m.viewState.SelectedPod, podStyle)

	return parts
}

// renderBreadcrumb returns the breadcrumb title for the current view with lipgloss styling.
func (m Model) renderBreadcrumb() string {
	parts := m.buildBreadcrumbParts()
	sep := BreadcrumbSeparator.String()

	rendered := make([]string, len(parts))
	for i, p := range parts {
		rendered[i] = p.style.Render(p.label)
	}
	return strings.Join(rendered, sep)
}

// buildShortcutsString builds the keyboard shortcuts string for testing.
// Returns plain text in k9s format: <key>Action
func (m Model) buildShortcutsString() string {
	items := m.buildMenuItems()

	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = "<" + item.key + ">" + item.action
	}

	return strings.Join(parts, "  ")
}

// forestResourceTypeLabel returns a human-readable label for a forest resource type.
func forestResourceTypeLabel(rt string) string {
	switch rt {
	case "pc":
		return "PodCliques"
	case "pcsg":
		return "PodCliqueScalingGroups"
	case "pod":
		return "Pods"
	case "pcs", "":
		return "Forest"
	default:
		return "Forest"
	}
}

// shellAvailable returns true when the 's' key should be shown in the menu.
func (m Model) shellAvailable() bool {
	return m.podActionAvailable()
}

// colorizeEventRow returns plain text for each column value.
// Note: bubbles/table doesn't handle per-cell ANSI styling well,
// so we return plain text and rely on row-level styling (Selected style).
func colorizeEventRow(e clusterstate.Event) []string {
	return []string{e.Type, e.Kind, e.Reason, e.Age, e.From, e.Message}
}
