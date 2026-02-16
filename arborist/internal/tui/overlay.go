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
	Active           bool
	Viewport         viewport.Model
	Content          string                 // raw content (before any transforms)
	ContentTransform func(string) string    // optional transform applied before display/search (nil = identity)
	SearchTransform  func(string) string    // optional separate transform for search matching (nil = use ContentTransform)
	SearchActive     bool
	SearchInput      textinput.Model
	SearchText       string
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

// UpdateViewportContent sets the viewport content from Content, applying
// ContentTransform (if set) before search highlighting.
func (o *OverlayModel) UpdateViewportContent() {
	content := o.Content
	if o.ContentTransform != nil {
		content = o.ContentTransform(content)
	}
	if o.SearchText != "" && content != "" {
		content = highlightSearchMatches(content, o.SearchText)
	}
	o.Viewport.SetContent(content)
}

// ApplySearch scrolls to the first search match, applying SearchTransform
// (falling back to ContentTransform) to the content before matching.
func (o *OverlayModel) ApplySearch() {
	content := o.Content
	if t := o.searchTransform(); t != nil {
		content = t(content)
	}
	viewportSearchFirst(content, o.SearchText, &o.Viewport)
}

// SearchNext jumps to the next (or previous) search match, applying
// SearchTransform (falling back to ContentTransform) before matching.
func (o *OverlayModel) SearchNext(reverse bool) {
	content := o.Content
	if t := o.searchTransform(); t != nil {
		content = t(content)
	}
	viewportSearchNext(content, o.SearchText, &o.Viewport, reverse)
}

// searchTransform returns the effective transform for search operations:
// SearchTransform if set, otherwise ContentTransform.
func (o *OverlayModel) searchTransform() func(string) string {
	if o.SearchTransform != nil {
		return o.SearchTransform
	}
	return o.ContentTransform
}

// HandleKeyMsg is a unified key handler for overlays. It handles:
//  1. Search-active input delegation (via HandleSearchKey)
//  2. Viewport content refresh + search apply after search commit/cancel
//  3. Common keys (close, viewport nav, search triggers, n/N)
//
// Returns (handled, cmd). If handled is false, the caller should handle the
// key with overlay-specific logic (e.g. logs wrap toggle, horizontal scroll).
func (o *OverlayModel) HandleKeyMsg(msg tea.KeyMsg) (handled bool, cmd tea.Cmd) {
	if o.SearchActive {
		needsUpdate, cmd := o.HandleSearchKey(msg)
		if needsUpdate {
			o.UpdateViewportContent()
			if o.SearchText != "" {
				o.ApplySearch()
			}
		}
		return true, cmd
	}

	return o.HandleKey(msg)
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
