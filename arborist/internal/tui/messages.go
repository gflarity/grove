package tui

// messages.go defines all tea.Msg types used by the Bubble Tea TUI.
// The Update loop handles these messages to update model state.

// CacheSyncedMsg signals the global cache has completed initial sync.
type CacheSyncedMsg struct{}

// CacheUpdateMsg signals the global cache has a new snapshot available.
type CacheUpdateMsg struct{}

// PodYAMLMsg is sent when a Pod's YAML has been loaded.
type PodYAMLMsg struct {
	PodName string
	YAML    string
	Err     error
}

// ErrorMsg is a generic error message for operations that don't have a specific message type.
type ErrorMsg struct {
	Operation string
	Err       error
}
