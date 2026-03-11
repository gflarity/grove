package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Autocompleter.UniqueMatch
// ---------------------------------------------------------------------------

func TestAutocompleter_UniqueMatch(t *testing.T) {
	ac := NewAutocompleter([]string{"forest", "topology"})

	tests := []struct {
		prefix    string
		wantName  string
		wantMatch bool
	}{
		// Exact match
		{"forest", "forest", true},
		{"topology", "topology", true},
		// Unique prefix
		{"for", "forest", true},
		{"top", "topology", true},
		{"f", "forest", true},
		{"t", "topology", true},
		// Case insensitive
		{"For", "forest", true},
		{"TOP", "topology", true},
		{"FOREST", "forest", true},
		// Ambiguous prefix (empty matches both? No — "f" and "t" are unique.)
		// Empty string
		{"", "", false},
		// No match
		{"xyz", "", false},
		{"z", "", false},
	}

	for _, tt := range tests {
		name, ok := ac.UniqueMatch(tt.prefix)
		if ok != tt.wantMatch {
			t.Errorf("UniqueMatch(%q): got ok=%v, want %v", tt.prefix, ok, tt.wantMatch)
		}
		if name != tt.wantName {
			t.Errorf("UniqueMatch(%q): got name=%q, want %q", tt.prefix, name, tt.wantName)
		}
	}
}

// ---------------------------------------------------------------------------
// Autocompleter.SetCandidates
// ---------------------------------------------------------------------------

func TestAutocompleter_SetCandidates(t *testing.T) {
	ac := NewAutocompleter([]string{"forest", "topology"})

	// Initial candidates work
	if name, ok := ac.UniqueMatch("for"); !ok || name != "forest" {
		t.Fatalf("expected forest, got %q ok=%v", name, ok)
	}

	// Update candidates
	ac.SetCandidates([]string{"alpha", "bravo", "charlie"})

	if _, ok := ac.UniqueMatch("for"); ok {
		t.Fatal("expected no match for 'for' after updating candidates")
	}
	if name, ok := ac.UniqueMatch("a"); !ok || name != "alpha" {
		t.Fatalf("expected alpha, got %q ok=%v", name, ok)
	}
	if name, ok := ac.UniqueMatch("br"); !ok || name != "bravo" {
		t.Fatalf("expected bravo, got %q ok=%v", name, ok)
	}

	// Candidates() returns the updated list
	cands := ac.Candidates()
	if len(cands) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(cands))
	}
}

// ---------------------------------------------------------------------------
// Autocompleter.ConfigureInput
// ---------------------------------------------------------------------------

func TestAutocompleter_ConfigureInput(t *testing.T) {
	ac := NewAutocompleter([]string{"forest", "topology"})
	input := textinput.New()

	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#778899"))
	ac.ConfigureInput(&input, style)

	if !input.ShowSuggestions {
		t.Fatal("expected ShowSuggestions to be true after ConfigureInput")
	}

	// AvailableSuggestions returns the full candidate list
	avail := input.AvailableSuggestions()
	if len(avail) != 2 {
		t.Fatalf("expected 2 available suggestions, got %d", len(avail))
	}
}

// ---------------------------------------------------------------------------
// Integration: typing into a configured textinput produces suggestions
// ---------------------------------------------------------------------------

func TestAutocompleter_SuggestionsIntegration(t *testing.T) {
	ac := NewAutocompleter([]string{"forest", "topology"})
	input := textinput.New()
	ac.ConfigureInput(&input, AutocompleteSuggestionStyle)
	input.Focus()

	// Type "f" — should produce "forest" as a matched suggestion
	input, _ = input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})

	matched := input.MatchedSuggestions()
	if len(matched) == 0 {
		t.Fatal("expected at least one matched suggestion after typing 'f'")
	}
	found := false
	for _, s := range matched {
		if s == "forest" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'forest' in matched suggestions, got %v", matched)
	}
}

// ---------------------------------------------------------------------------
// Integration: Tab accepts the suggestion (AcceptSuggestion)
// ---------------------------------------------------------------------------

func TestAutocompleter_AcceptSuggestion(t *testing.T) {
	ac := NewAutocompleter([]string{"forest", "topology"})
	input := textinput.New()
	ac.ConfigureInput(&input, AutocompleteSuggestionStyle)
	input.Focus()

	// Type "top"
	for _, r := range "top" {
		input, _ = input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if input.Value() != "top" {
		t.Fatalf("expected input value 'top', got %q", input.Value())
	}

	// Press Tab to accept suggestion
	input, _ = input.Update(tea.KeyMsg{Type: tea.KeyTab})
	if input.Value() != "topology" {
		t.Fatalf("expected input value 'topology' after Tab, got %q", input.Value())
	}
}

// ---------------------------------------------------------------------------
// Edge case: empty candidates
// ---------------------------------------------------------------------------

func TestAutocompleter_EmptyCandidates(t *testing.T) {
	ac := NewAutocompleter([]string{})

	if _, ok := ac.UniqueMatch("anything"); ok {
		t.Fatal("expected no match with empty candidates")
	}

	input := textinput.New()
	ac.ConfigureInput(&input, AutocompleteSuggestionStyle)
	input.Focus()

	// Type something — no suggestions
	input, _ = input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if len(input.MatchedSuggestions()) != 0 {
		t.Fatal("expected no matched suggestions with empty candidates")
	}

	// Tab does nothing
	input, _ = input.Update(tea.KeyMsg{Type: tea.KeyTab})
	if input.Value() != "x" {
		t.Fatalf("expected input value 'x' (unchanged), got %q", input.Value())
	}
}
