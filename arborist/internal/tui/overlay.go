package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// OverlayModel encapsulates the shared viewport + search state for full-screen
// overlays (YAML viewer, logs viewer, etc.). Specific overlays embed this and
// add their own extra fields.
type OverlayModel struct {
	Active       bool
	Viewport     viewport.Model
	Content      string // raw content (before any transforms)
	SearchActive bool
	SearchInput  textinput.Model
	SearchText   string
}

// Open activates the overlay with initial content and dimensions.
func (o *OverlayModel) Open(content string, width, height int) {
	o.Active = true
	o.Content = content
	o.SearchActive = false
	o.SearchText = ""
	o.SearchInput.SetValue("")

	o.Viewport.Width = width - 4  // room for border + padding
	o.Viewport.Height = height - 6 // room for header/footer
	o.Viewport.SetContent(content)
}

// Close deactivates the overlay and clears content.
func (o *OverlayModel) Close() {
	o.Active = false
	o.Content = ""
}

// HandleSearchKey handles keys when the search input is active.
// Returns (needsViewportUpdate, cmd). The caller must call UpdateViewportContent
// after cancel/apply to refresh the viewport.
func (o *OverlayModel) HandleSearchKey(msg tea.KeyMsg) (needsViewportUpdate bool, cmd tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		o.SearchActive = false
		o.SearchText = ""
		o.SearchInput.SetValue("")
		return true, nil

	case tea.KeyEnter:
		o.SearchActive = false
		o.SearchText = o.SearchInput.Value()
		return true, nil

	default:
		o.SearchInput, cmd = o.SearchInput.Update(msg)
		return false, cmd
	}
}

// HandleKey handles common overlay keys (close, viewport nav, search triggers).
// Returns (handled, cmd). If handled is false, the caller should handle the key
// with overlay-specific logic.
func (o *OverlayModel) HandleKey(msg tea.KeyMsg) (handled bool, cmd tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		o.Close()
		return true, nil

	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		o.Viewport, cmd = o.Viewport.Update(msg)
		return true, cmd

	case tea.KeyCtrlD:
		o.Viewport.HalfViewDown()
		return true, nil

	case tea.KeyCtrlU:
		o.Viewport.HalfViewUp()
		return true, nil

	case tea.KeyRunes:
		switch msg.String() {
		case "q", "Q":
			o.Close()
			return true, nil
		case "/":
			o.SearchActive = true
			o.SearchInput.SetValue(o.SearchText)
			o.SearchInput.Focus()
			return true, textinput.Blink
		case "n":
			if o.SearchText != "" {
				o.SearchNext(false)
			}
			return true, nil
		case "N":
			if o.SearchText != "" {
				o.SearchNext(true)
			}
			return true, nil
		}
	}

	return false, nil
}

// UpdateViewportContent sets the viewport content from Content with search
// highlighting applied. No content transformation is done — callers that need
// transforms (e.g. wrap, horizontal slice) should use UpdateViewportContentWithTransform.
func (o *OverlayModel) UpdateViewportContent() {
	content := o.Content
	if o.SearchText != "" && content != "" {
		content = highlightSearchMatches(content, o.SearchText)
	}
	o.Viewport.SetContent(content)
}

// UpdateViewportContentWithTransform sets the viewport content from Content,
// applying the given transform before search highlighting. The transform is
// applied to the raw content; highlighting is applied to the result.
func (o *OverlayModel) UpdateViewportContentWithTransform(transform func(string) string) {
	content := o.Content
	if transform != nil {
		content = transform(content)
	}
	if o.SearchText != "" && content != "" {
		content = highlightSearchMatches(content, o.SearchText)
	}
	o.Viewport.SetContent(content)
}

// ApplySearch scrolls to the first search match in the raw content.
func (o *OverlayModel) ApplySearch() {
	viewportSearchFirst(o.Content, o.SearchText, &o.Viewport)
}

// ApplySearchWithTransform scrolls to the first search match in transformed content.
func (o *OverlayModel) ApplySearchWithTransform(transform func(string) string) {
	content := o.Content
	if transform != nil {
		content = transform(content)
	}
	viewportSearchFirst(content, o.SearchText, &o.Viewport)
}

// SearchNext jumps to the next (or previous) search match in the raw content.
func (o *OverlayModel) SearchNext(reverse bool) {
	viewportSearchNext(o.Content, o.SearchText, &o.Viewport, reverse)
}

// SearchNextWithTransform jumps to the next/previous match in transformed content.
func (o *OverlayModel) SearchNextWithTransform(reverse bool, transform func(string) string) {
	content := o.Content
	if transform != nil {
		content = transform(content)
	}
	viewportSearchNext(content, o.SearchText, &o.Viewport, reverse)
}

// RenderSearchFrame renders the search input as a framed box.
func (o *OverlayModel) RenderSearchFrame(width int) string {
	border := lipgloss.NormalBorder()
	bc := lipgloss.NewStyle().Foreground(ColorBorderFocused)
	contentWidth := width - 2

	content := "🔍" + o.SearchInput.View()
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
