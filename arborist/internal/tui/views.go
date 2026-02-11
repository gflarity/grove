package tui

import (
	"fmt"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/charmbracelet/lipgloss"
)

// View renders the entire TUI. This is the main entry point for Bubble Tea rendering.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	if !m.cacheSynced && m.cache != nil {
		return "Syncing..."
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
	// Must match handleWindowSize — see that function for the full breakdown.
	// Fixed: 6(header) + 2*(2 border + 1 table header) = 12
	// (section headers are now embedded in the top border, not separate lines)
	fixedLines := 12
	if m.filterActive {
		fixedLines += 3 // filter frame: top border + content + bottom border
	}
	if m.commandActive {
		fixedLines += 3 // command frame: top border + content + bottom border
	}

	// Account for footnote line in topology view with GPU columns
	topologyFootnoteVisible := m.viewState.ViewType == data.TopologyView && m.topologyHasGPUColumns()
	if topologyFootnoteVisible {
		fixedLines++ // 1 line for the footnote
	}

	availableHeight := m.height - fixedLines
	resourcesHeight := availableHeight / 2
	eventsHeight := availableHeight - resourcesHeight
	if resourcesHeight < 3 {
		resourcesHeight = 3
	}
	if eventsHeight < 3 {
		eventsHeight = 3
	}

	// Render view-specific panes
	switch m.viewState.ViewType {
	case data.TopologyView:
		sections = append(sections, m.renderTopologyDomainsFrame(resourcesHeight))
		sections = append(sections, m.renderTopologyPodsFrame(eventsHeight))
		if topologyFootnoteVisible {
			sections = append(sections, m.renderTopologyFootnote())
		}
	default:
		sections = append(sections, m.renderResourcesFrame(resourcesHeight))
		sections = append(sections, m.renderEventsFrame(eventsHeight))
	}

	// Note: bottom menu bar removed — shortcuts are now displayed in the header
	// (see renderHeaderFrame middle column). renderMenuBar() and buildShortcutsString()
	// are kept as internal helpers for test compatibility.

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// viewDisplayName returns a short display name for the current view type.
func (m Model) viewDisplayName() string {
	switch m.viewState.ViewType {
	case data.ForestView:
		return "Forest"
	case data.PodCliqueSetView:
		return "PodCliqueSet"
	case data.PodCliqueSetReplicaView:
		return "PodCliqueSetReplica"
	case data.PodCliqueScalingGroupView:
		return "PodCliqueScalingGroup"
	case data.PodCliqueScalingGroupReplicaView:
		return "PodCliqueScalingGroupReplica"
	case data.PodCliqueView:
		return "PodClique"
	case data.PodView:
		return "Pod"
	case data.TopologyView:
		return "Topology"
	default:
		return "Unknown"
	}
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
	viewLine := HeaderLabelStyle.Render("Lens:        ") + " " + HeaderValueStyle.Render(m.viewDisplayName())

	leftCol := lipgloss.JoinVertical(lipgloss.Left, contextLine, clusterLine, userLine, arboristLine, k8sLine, viewLine)

	// --- Middle column: key helpers in 2 rows ---
	type menuItem struct {
		key    string
		action string
	}

	items := []menuItem{
		{":", "Cmd"},
		{"/", "Filter"},
		{"tab", "Switch"},
	}

	if m.viewState.ViewType == data.PodView {
		items = append(items, menuItem{"↑↓", "Scroll"})
	} else {
		items = append(items, menuItem{"↑↓", "Nav"})
		items = append(items, menuItem{"enter", "Drill"})
	}

	items = append(items, menuItem{"t", "Toggle"})

	if m.viewState.ViewType != data.ForestView {
		items = append(items, menuItem{"esc", "Back"})
	}

	items = append(items, menuItem{"q", "Quit"})

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

// renderHeader renders the top header bar with logo and optional context info.
func (m Model) renderHeader() string {
	logo := LogoStyle.Render("Arborist")
	return logo
}

// renderFilterBar renders the filter input bar (plain text, no frame).
func (m Model) renderFilterBar() string {
	return FilterBarStyle.Render("/") + " " + m.filterInput.View()
}

// renderFilterFrame renders the filter input as a standalone framed box between
// the header and the resources table, k9s-style. Uses a normal (non-rounded)
// border and a tree emoji prefix like k9s uses a poodle.
//
//	┌──────────────────────────────────────────┐
//	│ 🌲/                                      │
//	└──────────────────────────────────────────┘
func (m Model) renderFilterFrame() string {
	border := lipgloss.NormalBorder()
	bc := lipgloss.NewStyle().Foreground(ColorBorderFocused)
	contentWidth := m.width - 2 // inside left + right border chars

	// Content: tree emoji + filter input.
	// Clamp to contentWidth so the right border is never pushed off-screen.
	content := "🌲" + m.filterInput.View()
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

// renderCommandFrame renders the command input as a standalone framed box,
// vim-style. Uses a normal border and a ":" prefix.
//
//	┌──────────────────────────────────────────┐
//	│ :topology                                │
//	└──────────────────────────────────────────┘
func (m Model) renderCommandFrame() string {
	border := lipgloss.NormalBorder()
	bc := lipgloss.NewStyle().Foreground(ColorBorderFocused)
	contentWidth := m.width - 2 // inside left + right border chars

	// Content: command input with ":" prompt (handled by textinput itself).
	content := m.commandInput.View()
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
	var content string
	if m.viewState.ViewType == data.PodView {
		content = m.renderPodViewport()
	} else {
		content = m.renderResourcesTable()
	}

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
	if m.topologyViewData == nil || len(m.topologyDrillStack) == 0 {
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
	return FootnoteStyle.Render("¹ GPU: Grove/Other/Total")
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

	if len(m.topologyDrillStack) == 0 {
		return renderSectionHeader("Topology Domains", count, m.activePane == data.TopologyDomainsPane, "")
	}

	// Drilled in — show the current domain name and breadcrumb
	currentDomain, _ := m.currentTopologyDomain()
	breadcrumb := m.topologyBreadcrumbString()
	label := currentDomain
	if label == "" {
		label = "Topology Domains"
	}

	return renderSectionHeader(label, count, m.activePane == data.TopologyDomainsPane, breadcrumb)
}

// renderTopologyPodsSectionHeader renders the section header for the topology pods pane.
func (m Model) renderTopologyPodsSectionHeader() string {
	count := len(m.topologyPodsTable.Rows())
	breadcrumb := m.topologyBreadcrumbString()
	if breadcrumb == "" {
		if len(m.topologyDrillStack) == 0 {
			// Top-level domain list — pods aren't scoped yet
			breadcrumb = "N/A"
		} else if selectedRow := m.topologyDomainsTable.SelectedRow(); len(selectedRow) >= 1 {
			// Drilled into a domain showing values — show the highlighted value
			breadcrumb = selectedRow[0]
		}
	}
	return renderSectionHeader("Pods", count, m.activePane == data.TopologyPodsPane, breadcrumb)
}

// renderResourcesSection renders the resources section: header + table (no border).
// Kept for compatibility - now delegates to frame version internals.
func (m Model) renderResourcesSection(height int) string {
	// Section header
	header := m.renderResourcesSectionHeader()

	// Content
	var content string
	if m.viewState.ViewType == data.PodView {
		content = m.renderPodViewport()
	} else {
		content = m.renderResourcesTable()
	}

	return header + "\n" + content
}

// renderEventsSection renders the events section: header + table (no border).
// Kept for compatibility - now delegates to frame version internals.
func (m Model) renderEventsSection(height int) string {
	header := m.renderEventsSectionHeader()
	content := m.renderEventsTable()
	return header + "\n" + content
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
	viewKey := m.getCurrentViewKey()
	resources := m.allResources[viewKey]

	suffix := m.renderBreadcrumb()

	// Add filter indicator
	if m.filterText != "" {
		suffix += " " + SectionCountStyle.Render("|") + " " +
			FilterBarStyle.Render("filter:") + " " + m.filterText
	}

	return renderSectionHeader("Resources", len(resources), m.activePane == data.ResourcesPane, suffix)
}

// renderEventsSectionHeader renders the events section header in k9s style.
// Example: "Events [5]"
func (m Model) renderEventsSectionHeader() string {
	events := m.getFilteredEvents()
	return renderSectionHeader("Events", len(events), m.activePane == data.EventsPane, "")
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

// renderBreadcrumb returns the breadcrumb title for the current view with lipgloss styling.
func (m Model) renderBreadcrumb() string {
	forestStyle := BreadcrumbStyles["Forest"]
	pcsStyle := BreadcrumbStyles["PodCliqueSet"]
	pcsgStyle := BreadcrumbStyles["PodCliqueScalingGroup"]
	pcStyle := BreadcrumbStyles["PodClique"]
	podStyle := BreadcrumbStyles["Pod"]
	sep := BreadcrumbSeparator.String()

	switch m.viewState.ViewType {
	case data.ForestView:
		return forestStyle.Render("Forest")
	case data.PodCliqueSetView:
		return forestStyle.Render("Forest") + sep + pcsStyle.Render(m.viewState.SelectedPodCliqueSet)
	case data.PodCliqueSetReplicaView:
		return forestStyle.Render("Forest") + sep +
			pcsStyle.Render(m.viewState.SelectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.SelectedReplicaIndex)
	case data.PodCliqueScalingGroupView:
		return forestStyle.Render("Forest") + sep +
			pcsStyle.Render(m.viewState.SelectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.SelectedReplicaIndex) + sep +
			pcsgStyle.Render(m.viewState.SelectedScalingGroup)
	case data.PodCliqueScalingGroupReplicaView:
		return forestStyle.Render("Forest") + sep +
			pcsStyle.Render(m.viewState.SelectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.SelectedReplicaIndex) + sep +
			pcsgStyle.Render(m.viewState.SelectedScalingGroup) + sep +
			pcsgStyle.Render("replica-"+m.viewState.SelectedPCSGReplicaIndex)
	case data.PodCliqueView:
		parent := pcsStyle.Render(m.viewState.SelectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.SelectedReplicaIndex)
		if m.viewState.SelectedScalingGroup != "" {
			parent += sep + pcsgStyle.Render(m.viewState.SelectedScalingGroup)
			if m.viewState.SelectedPCSGReplicaIndex != "" {
				parent += sep + pcsgStyle.Render("replica-"+m.viewState.SelectedPCSGReplicaIndex)
			}
		}
		return forestStyle.Render("Forest") + sep + parent + sep + pcStyle.Render(m.viewState.SelectedPodClique)
	case data.PodView:
		parent := pcsStyle.Render(m.viewState.SelectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.SelectedReplicaIndex)
		if m.viewState.SelectedScalingGroup != "" {
			parent += sep + pcsgStyle.Render(m.viewState.SelectedScalingGroup)
			if m.viewState.SelectedPCSGReplicaIndex != "" {
				parent += sep + pcsgStyle.Render("replica-"+m.viewState.SelectedPCSGReplicaIndex)
			}
		}
		return forestStyle.Render("Forest") + sep + parent + sep +
			pcStyle.Render(m.viewState.SelectedPodClique) + sep +
			podStyle.Render(m.viewState.SelectedPod)
	}
	return forestStyle.Render("Forest")
}

// renderMenuBar renders the bottom menu bar in k9s style: <key>Action  <key>Action ...
func (m Model) renderMenuBar() string {
	type menuItem struct {
		key    string
		action string
	}

	items := []menuItem{
		{":", "Cmd"},
		{"/", "Filter"},
		{"tab", "Switch"},
	}

	if m.viewState.ViewType == data.PodView {
		items = append(items, menuItem{"↑↓", "Scroll"})
	} else {
		items = append(items, menuItem{"↑↓", "Nav"})
		items = append(items, menuItem{"enter", "Drill"})
	}

	items = append(items, menuItem{"t", "Toggle"})

	if m.viewState.ViewType != data.ForestView {
		items = append(items, menuItem{"esc", "Back"})
	}

	items = append(items, menuItem{"q", "Quit"})

	var parts []string
	for _, item := range items {
		part := MenuKeyStyle.Render("<"+item.key+">") + MenuActionStyle.Render(item.action)
		parts = append(parts, part)
	}

	return strings.Join(parts, "  ")
}

// renderStatusBar is the legacy name kept for compatibility. Delegates to renderMenuBar.
func (m Model) renderStatusBar() string {
	return m.renderMenuBar()
}

// buildShortcutsString builds the keyboard shortcuts string for testing.
// Returns plain text in k9s format: <key>Action
func (m Model) buildShortcutsString() string {
	var parts []string

	parts = append(parts, "<:>Cmd")
	parts = append(parts, "</>Filter")
	parts = append(parts, "<tab>Switch")

	if m.viewState.ViewType == data.PodView {
		parts = append(parts, "<↑↓>Scroll")
	} else {
		parts = append(parts, "<↑↓>Nav")
		parts = append(parts, "<enter>Drill")
	}

	parts = append(parts, "<t>Toggle")

	if m.viewState.ViewType != data.ForestView {
		parts = append(parts, "<esc>Back")
	}

	parts = append(parts, "<q>Quit")

	return strings.Join(parts, "  ")
}

// colorizeResourceRow returns plain text for each column value.
// Note: bubbles/table doesn't handle per-cell ANSI styling well,
// so we return plain text and rely on row-level styling (Selected style).
func colorizeResourceRow(r data.Resource) []string {
	return []string{r.Namespace, r.Type, r.Name, r.Topology, r.Ready, r.Scheduled}
}

// colorizeEventRow returns plain text for each column value.
// Note: bubbles/table doesn't handle per-cell ANSI styling well,
// so we return plain text and rely on row-level styling (Selected style).
func colorizeEventRow(e data.Event) []string {
	return []string{e.Type, e.Kind, e.Reason, e.Age, e.From, e.Message}
}
