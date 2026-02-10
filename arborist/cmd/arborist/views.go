package main

import (
	"fmt"
	"strings"

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

	// Calculate available height for the main viewport
	// Top space(1) + Header frame(3) + menu bar(1) = 5 fixed lines
	fixedLines := 5
	availableHeight := m.height - fixedLines
	resourcesHeight := availableHeight / 2
	eventsHeight := availableHeight - resourcesHeight
	if resourcesHeight < 5 {
		resourcesHeight = 5
	}
	if eventsHeight < 5 {
		eventsHeight = 5
	}

	// Resources section (framed)
	sections = append(sections, m.renderResourcesFrame(resourcesHeight))

	// Events section (framed)
	sections = append(sections, m.renderEventsFrame(eventsHeight))

	// Menu bar (k9s-style bottom shortcut line)
	sections = append(sections, m.renderMenuBar())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderHeaderFrame renders the top header bar in a framed box.
func (m Model) renderHeaderFrame() string {
	logo := LogoStyle.Render("Arborist")

	// Add context info on the right side
	context := HeaderInfoStyle.Render("Kubernetes Resource Explorer")

	// Calculate spacing to fill the header
	contentWidth := m.width - 6 // Account for border + padding
	if contentWidth < 40 {
		contentWidth = 40
	}

	logoLen := lipgloss.Width(logo)
	contextLen := lipgloss.Width(context)
	padding := contentWidth - logoLen - contextLen
	if padding < 1 {
		padding = 1
	}

	headerContent := logo + strings.Repeat(" ", padding) + context

	// Create the frame with k9s focused border color
	frameStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderFocused)

	return frameStyle.Render(headerContent)
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

// renderResourcesFrame renders the resources section in a framed box.
func (m Model) renderResourcesFrame(height int) string {
	// Build the content
	var contentLines []string

	// Section header with breadcrumb - pad to full width
	header := m.renderResourcesSectionHeader()
	headerWidth := lipgloss.Width(header)
	tableWidth := m.width - 4 // Account for frame borders
	if headerWidth < tableWidth {
		header = header + strings.Repeat(" ", tableWidth-headerWidth)
	}
	contentLines = append(contentLines, header)

	// Filter bar (if active)
	if m.filterActive {
		contentLines = append(contentLines, m.renderFilterBar())
	}

	// Table content
	if m.viewState.viewType == PodView {
		contentLines = append(contentLines, m.renderPodViewport())
	} else {
		contentLines = append(contentLines, m.renderResourcesTable())
	}

	content := strings.Join(contentLines, "\n")

	// k9s style: focused border color
	frameStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderFocused)

	return frameStyle.Render(content)
}

// renderEventsFrame renders the events section in a framed box.
func (m Model) renderEventsFrame(height int) string {
	// Build the content
	var contentLines []string

	// Section header - pad to full width
	header := m.renderEventsSectionHeader()
	headerWidth := lipgloss.Width(header)
	tableWidth := m.width - 4 // Account for frame borders
	if headerWidth < tableWidth {
		header = header + strings.Repeat(" ", tableWidth-headerWidth)
	}
	contentLines = append(contentLines, header)

	// Table content
	contentLines = append(contentLines, m.renderEventsTable())

	content := strings.Join(contentLines, "\n")

	// k9s style: focused border color
	frameStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderFocused)

	return frameStyle.Render(content)
}

// renderResourcesSection renders the resources section: header + table (no border).
// Kept for compatibility - now delegates to frame version internals.
func (m Model) renderResourcesSection(height int) string {
	// Section header
	header := m.renderResourcesSectionHeader()

	// Content
	var content string
	if m.viewState.viewType == PodView {
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
	if m.activePane == ResourcesPane {
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
	if m.activePane == EventsPane {
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

	switch m.viewState.viewType {
	case ForestView:
		return forestStyle.Render("Forest")
	case PodCliqueSetView:
		return forestStyle.Render("Forest") + sep + pcsStyle.Render(m.viewState.selectedPodCliqueSet)
	case PodCliqueSetReplicaView:
		return forestStyle.Render("Forest") + sep +
			pcsStyle.Render(m.viewState.selectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.selectedReplicaIndex)
	case PodCliqueScalingGroupView:
		return forestStyle.Render("Forest") + sep +
			pcsStyle.Render(m.viewState.selectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.selectedReplicaIndex) + sep +
			pcsgStyle.Render(m.viewState.selectedScalingGroup)
	case PodCliqueView:
		parent := pcsStyle.Render(m.viewState.selectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.selectedReplicaIndex)
		if m.viewState.selectedScalingGroup != "" {
			parent += sep + pcsgStyle.Render(m.viewState.selectedScalingGroup)
		}
		return forestStyle.Render("Forest") + sep + parent + sep + pcStyle.Render(m.viewState.selectedPodClique)
	case PodView:
		parent := pcsStyle.Render(m.viewState.selectedPodCliqueSet) + sep +
			pcsStyle.Render("replica-"+m.viewState.selectedReplicaIndex)
		if m.viewState.selectedScalingGroup != "" {
			parent += sep + pcsgStyle.Render(m.viewState.selectedScalingGroup)
		}
		return forestStyle.Render("Forest") + sep + parent + sep +
			pcStyle.Render(m.viewState.selectedPodClique) + sep +
			podStyle.Render(m.viewState.selectedPod)
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

	if m.viewState.viewType == PodView {
		items = append(items, menuItem{"↑↓", "Scroll"})
	} else {
		items = append(items, menuItem{"↑↓", "Nav"})
		items = append(items, menuItem{"enter", "Drill"})
	}

	if m.viewState.viewType != ForestView {
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

	if m.viewState.viewType == PodView {
		parts = append(parts, "<↑↓>Scroll")
	} else {
		parts = append(parts, "<↑↓>Nav")
		parts = append(parts, "<enter>Drill")
	}

	if m.viewState.viewType != ForestView {
		parts = append(parts, "<esc>Back")
	}

	parts = append(parts, "<q>Quit")

	return strings.Join(parts, "  ")
}

// colorizeResourceRow returns plain text for each column value.
// Note: bubbles/table doesn't handle per-cell ANSI styling well,
// so we return plain text and rely on row-level styling (Selected style).
func colorizeResourceRow(r Resource) []string {
	return []string{r.Namespace, r.Type, r.Name, r.Topology, r.Ready, r.Scheduled}
}

// colorizeEventRow returns plain text for each column value.
// Note: bubbles/table doesn't handle per-cell ANSI styling well,
// so we return plain text and rely on row-level styling (Selected style).
func colorizeEventRow(e Event) []string {
	return []string{e.Type, e.Reason, e.Age, e.From, e.Message}
}
