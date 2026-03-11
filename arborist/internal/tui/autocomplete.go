package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// Autocompleter manages a list of completion candidates and integrates with
// the charmbracelet/bubbles textinput inline suggestion feature. It is not
// tied to any specific domain (lens commands, namespaces, etc.) — callers
// provide their own candidate lists.
type Autocompleter struct {
	candidates []string
}

// NewAutocompleter creates an Autocompleter with the given initial candidates.
func NewAutocompleter(candidates []string) *Autocompleter {
	return &Autocompleter{candidates: candidates}
}

// SetCandidates replaces the candidate list.
func (a *Autocompleter) SetCandidates(candidates []string) {
	a.candidates = candidates
}

// Candidates returns the current candidate list.
func (a *Autocompleter) Candidates() []string {
	return a.candidates
}

// ConfigureInput enables the built-in inline suggestion feature on a
// textinput.Model: sets ShowSuggestions, CompletionStyle, and loads the
// candidate list as suggestions.
func (a *Autocompleter) ConfigureInput(input *textinput.Model, style lipgloss.Style) {
	input.ShowSuggestions = true
	input.CompletionStyle = style
	input.SetSuggestions(a.candidates)
}

// UniqueMatch returns the single candidate that starts with prefix (case-
// insensitive). If exactly one candidate matches, it is returned with true.
// If zero or more than one candidates match, ("", false) is returned.
func (a *Autocompleter) UniqueMatch(prefix string) (string, bool) {
	if prefix == "" {
		return "", false
	}
	lower := strings.ToLower(prefix)
	var matches []string
	for _, c := range a.candidates {
		if strings.HasPrefix(strings.ToLower(c), lower) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}
