package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the arborist TUI.
type KeyMap struct {
	Quit     key.Binding
	CtrlC    key.Binding
	Tab      key.Binding
	Enter    key.Binding
	Back     key.Binding
	Filter   key.Binding
	Up       key.Binding
	Down     key.Binding
	Topology key.Binding
}

// DefaultKeyMap returns the default key bindings matching the existing TUI behavior.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "Q"),
			key.WithHelp("q", "quit"),
		),
		CtrlC: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("Tab", "switch pane"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("Enter", "drill down"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("Esc", "back"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
		Topology: key.NewBinding(
			key.WithKeys("t", "T"),
			key.WithHelp("t", "toggle topology"),
		),
	}
}

// ShortHelp returns the key bindings shown in the short help view (status bar).
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Filter, k.Tab, k.Up, k.Down, k.Enter, k.Topology, k.Back, k.Quit,
	}
}

// FullHelp returns the full set of key bindings for the help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Filter, k.Tab, k.Up, k.Down},
		{k.Enter, k.Topology, k.Back, k.Quit, k.CtrlC},
	}
}
