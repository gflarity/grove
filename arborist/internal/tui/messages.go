package tui

import "github.com/ai-dynamo/grove/arborist/internal/clusterstate"

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

// PodContainersMsg is sent when a Pod's container info has been loaded.
type PodContainersMsg struct {
	PodName    string
	Namespace  string
	Containers []clusterstate.ContainerInfo
	Err        error
}

// ShellRequestMsg signals that a shell should be launched into a container.
type ShellRequestMsg struct {
	PodName   string
	Namespace string
	Container string
}

// ShellExitMsg is sent when a shell process exits.
type ShellExitMsg struct {
	Err error
}

// LogsContentMsg is sent when pod logs have been fetched.
type LogsContentMsg struct {
	PodName   string
	Container string
	Content   string
	Err       error
}

// LogsRequestMsg signals that a specific container's logs should be loaded.
// Used when the container is resolved asynchronously (e.g. from PodCliqueView).
type LogsRequestMsg struct {
	PodName   string
	Namespace string
	Container string
}

// logsAutoScrollTickMsg is a periodic tick that triggers log re-fetch when autoscroll is active.
type logsAutoScrollTickMsg struct{}
