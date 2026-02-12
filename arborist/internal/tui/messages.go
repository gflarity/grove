package tui

// messages.go defines all tea.Msg types used by the Bubble Tea TUI.
// The Update loop handles these messages to update model state.

// CacheSyncedMsg signals the global cache has completed initial sync.
// Warnings contains any non-fatal messages from cache startup (e.g. missing CRDs).
type CacheSyncedMsg struct {
	Warnings []string
}

// CacheUpdateMsg signals the global cache has a new snapshot available.
type CacheUpdateMsg struct{}

// PodYAMLMsg is sent when a Pod's YAML has been loaded.
type PodYAMLMsg struct {
	PodName string
	YAML    string
	Err     error
}

// ResourceYAMLMsg is sent when a resource's YAML has been loaded (for the YAML overlay).
type ResourceYAMLMsg struct {
	ResourceType string
	ResourceName string
	YAML         string
	Err          error
}

// ErrorMsg is a generic error message for operations that don't have a specific message type.
type ErrorMsg struct {
	Operation string
	Err       error
}

// WarningMsg is a non-fatal warning message (e.g. missing CRD) that should be
// surfaced in the error log box.
type WarningMsg struct {
	Message string
}
