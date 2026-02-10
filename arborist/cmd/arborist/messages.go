package main

// messages.go defines all tea.Msg types used by the Bubble Tea TUI.
// The Update loop handles these messages to update model state.

// ForestDataMsg is sent when PodCliqueSet data has been loaded for the forest view.
type ForestDataMsg struct {
	Resources []Resource
	Err       error
}

// ReplicaDataMsg is sent when replica index data has been loaded for a PodCliqueSet.
type ReplicaDataMsg struct {
	PCSName        string
	Namespace      string
	ReplicaIndexes []string
	// ScalingGroupsByReplica maps replicaIndex -> scaling group resources.
	ScalingGroupsByReplica map[string][]Resource
	// PodCliquesByReplica maps replicaIndex -> standalone PodClique resources.
	PodCliquesByReplica map[string][]Resource
	Err                 error
}

// ReplicaChildrenMsg is sent when children resources for a specific replica have been loaded.
type ReplicaChildrenMsg struct {
	PCSName       string
	Namespace     string
	ReplicaIndex  string
	ScalingGroups []Resource
	PodCliques    []Resource
	Err           error
}

// PCSGChildrenMsg is sent when PodClique children of a PodCliqueScalingGroup have been loaded.
type PCSGChildrenMsg struct {
	PCSGName   string
	Namespace  string
	PodCliques []Resource
	Err        error
}

// PodCliqueChildrenMsg is sent when Pod children of a PodClique have been loaded.
type PodCliqueChildrenMsg struct {
	PodCliqueName string
	Namespace     string
	Pods          []Resource
	Err           error
}

// EventsMsg is sent when events have been loaded for a resource.
type EventsMsg struct {
	Events []Event
	Err    error
}

// PodYAMLMsg is sent when a Pod's YAML has been loaded.
type PodYAMLMsg struct {
	PodName string
	YAML    string
	Err     error
}

// TopologyInfoMsg is sent when topology info has been loaded for a PodCliqueSet.
type TopologyInfoMsg struct {
	PCSName      string
	Namespace    string
	TopologyInfo *TopologyInfo
	Err          error
}

// PodInfoMsg is sent when cached pod info has been loaded for a PodCliqueSet.
type PodInfoMsg struct {
	PCSName   string
	Namespace string
	PodInfos  map[string]CachedPodInfo
	Err       error
}

// NodeLabelsMsg is sent when node topology labels have been loaded.
type NodeLabelsMsg struct {
	NodeLabels map[string]map[string]string
	Err        error
}

// ErrorMsg is a generic error message for operations that don't have a specific message type.
type ErrorMsg struct {
	Operation string
	Err       error
}
