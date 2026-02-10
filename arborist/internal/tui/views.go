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

	var sections []string

	// Empty line at top for spacing (ensures top border is visible)
	sections = append(sections, "")

	// Header bar (in its own frame)
	sections = append(sections, m.renderHeaderFrame())

	// Filter bar (if active) - rendered inside the resources frame
	// so we don't add it separately here

	// Calculate available height for the main viewport.
	// Must match handleWindowSize — see that function for the full breakdown.
	// Fixed: 1(blank) + 6(header) + 2*(2 border + 1 table header) = 13
	// (section headers are now embedded in the top border, not separate lines)
	fixedLines := 13
	if m.filterActive {
		fixedLines++
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

	// Resources section (framed)
	sections = append(sections, m.renderResourcesFrame(resourcesHeight))

	// Events section (framed)
	sections = append(sections, m.renderEventsFrame(eventsHeight))

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
	case data.PodCliqueView:
		return "PodClique"
	case data.PodView:
		return "Pod"
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
	viewLine := HeaderLabelStyle.Render("View:        ") + " " + HeaderValueStyle.Render(m.viewDisplayName())

	leftCol := lipgloss.JoinVertical(lipgloss.Left, contextLine, clusterLine, userLine, arboristLine, k8sLine, viewLine)

	// --- Middle column: key helpers in 2 rows ---
	type menuItem struct {
		key    string
		action string
	}

	items := []menuItem{
		{"/", "Filter"},
		{"tab", "Switch"},
	}

	if m.viewState.ViewType == data.PodView {
		items = append(items, menuItem{"↑↓", "Scroll"})
	} else {
		items = append(items, menuItem{"↑↓", "Nav"})
		items = append(items, menuItem{"enter", "Drill"})
	}

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

// renderFilterBar renders the filter input bar.
func (m Model) renderFilterBar() string {
	return FilterBarStyle.Render("/") + " " + m.filterInput.View()
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
func renderFrameWithTitle(title, content string, totalWidth int, borderColor lipgloss.Color) string {
	border := lipgloss.RoundedBorder()
	bc := lipgloss.NewStyle().Foreground(borderColor)
	contentWidth := totalWidth - 2 // subtract left + right border chars

	// --- Top border with embedded title ---
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
func (m Model) renderResourcesFrame(height int) string {
	// Section header becomes the border title
	title := m.renderResourcesSectionHeader()

	// Build the content (no section header line — it's in the border now)
	var contentLines []string

	// Filter bar (if active)
	if m.filterActive {
		contentLines = append(contentLines, m.renderFilterBar())
	}

	// Table content
	if m.viewState.ViewType == data.PodView {
		contentLines = append(contentLines, m.renderPodViewport())
	} else {
		contentLines = append(contentLines, m.renderResourcesTable())
	}

	content := strings.Join(contentLines, "\n")

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

// renderResourcesSectionHeader renders the resources section header in k9s style.
// Example: "Resources [3] Forest > my-pcs > replica-0"
func (m Model) renderResourcesSectionHeader() string {
	breadcrumb := m.renderBreadcrumb()

	// Count resources
	viewKey := m.getCurrentViewKey()
	resources := m.allResources[viewKey]
	count := len(resources)
	countStr := SectionCountStyle.Render(fmt.Sprintf("[%d]", count))

	// Active/inactive styling
	var label string
	if m.activePane == data.ResourcesPane {
		label = SectionHeaderActiveStyle.Render("Resources")
	} else {
		label = SectionHeaderInactiveStyle.Render("Resources")
	}

	parts := label + " " + countStr + " " + breadcrumb

	// Add filter indicator
	if m.filterText != "" {
		parts += " " + SectionCountStyle.Render("|") + " " +
			FilterBarStyle.Render("filter:") + " " + m.filterText
	}

	return parts
}

// renderEventsSectionHeader renders the events section header in k9s style.
// Example: "Events [5]"
func (m Model) renderEventsSectionHeader() string {
	events := m.getFilteredEvents()
	count := len(events)
	countStr := SectionCountStyle.Render(fmt.Sprintf("[%d]", count))

	var label string
	if m.activePane == data.EventsPane {
		label = SectionHeaderActiveStyle.Render("Events")
	} else {
		label = SectionHeaderInactiveStyle.Render("Events")
	}

	return label + " " + countStr
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
	case data.PodCliqueView:
		parent := pcsStyle.Render(m.viewState.SelectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.SelectedReplicaIndex)
		if m.viewState.SelectedScalingGroup != "" {
			parent += sep + pcsgStyle.Render(m.viewState.SelectedScalingGroup)
		}
		return forestStyle.Render("Forest") + sep + parent + sep + pcStyle.Render(m.viewState.SelectedPodClique)
	case data.PodView:
		parent := pcsStyle.Render(m.viewState.SelectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.SelectedReplicaIndex)
		if m.viewState.SelectedScalingGroup != "" {
			parent += sep + pcsgStyle.Render(m.viewState.SelectedScalingGroup)
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
		{"/", "Filter"},
		{"tab", "Switch"},
	}

	if m.viewState.ViewType == data.PodView {
		items = append(items, menuItem{"↑↓", "Scroll"})
	} else {
		items = append(items, menuItem{"↑↓", "Nav"})
		items = append(items, menuItem{"enter", "Drill"})
	}

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

	parts = append(parts, "</>Filter")
	parts = append(parts, "<tab>Switch")

	if m.viewState.ViewType == data.PodView {
		parts = append(parts, "<↑↓>Scroll")
	} else {
		parts = append(parts, "<↑↓>Nav")
		parts = append(parts, "<enter>Drill")
	}

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
	return []string{e.Type, e.Reason, e.Age, e.From, e.Message}
}
