// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package data

import (
	"fmt"
	"sort"
	"time"
)

// Pane represents which pane is active.
type Pane int

const (
	ResourcesPane Pane = iota
	EventsPane
	TopologyDomainsPane
	TopologyPodsPane
)

// ViewType represents the current view in the hierarchy.
type ViewType int

const (
	ForestView ViewType = iota
	PodCliqueSetView
	PodCliqueSetReplicaView
	PodCliqueScalingGroupView
	PodCliqueScalingGroupReplicaView
	PodCliqueView
	PodView
	ContainersView
	TopologyView
)

// ViewState tracks the current navigation state.
type ViewState struct {
	ViewType                 ViewType
	SelectedPodCliqueSet     string
	SelectedReplicaIndex     string // The PCS replica index (e.g., "0", "1", "2")
	SelectedScalingGroup     string
	SelectedPCSGReplicaIndex string // The PCSG replica index (e.g., "0", "1", "2")
	SelectedPodClique        string
	SelectedPod              string
}

// Resource represents a generic resource item.
type Resource struct {
	Name       string
	Type       string
	Ready      string
	Scheduled  string // Scheduled replicas (for PodClique/PodCliqueScalingGroup) or Phase (for Pods)
	Status     string
	Namespace  string
	ParentType string
	ParentName string
	YAML       string // For Pod detail view
	Topology   string // Topology display: "rack" (explicit), "(rack)" (inherited), or "N/A"
}

// Event represents a Kubernetes event.
type Event struct {
	Type      string    // Normal, Warning, Error
	Kind      string    // InvolvedObject kind: Pod, PodClique, PodCliqueScalingGroup, etc.
	Reason    string    // The reason for the event
	Age       string    // How long ago
	From      string    // Component that generated the event
	Message   string    // Detailed message
	Parent    string    // Parent resource name for filtering
	Timestamp time.Time // Event timestamp for sorting
}

// CachedPodInfo holds cached pod information for topology value resolution.
type CachedPodInfo struct {
	NodeName string
	Labels   map[string]string
}

// TopologyDomainRow represents a row in the Topology Domains table.
type TopologyDomainRow struct {
	Domain      string // "region", "rack", "N/A"
	Key         string // node label key, or "—" for N/A
	ValuesCount int    // count of distinct values across nodes, or -1 for N/A
}

// TopologyDrillSelection tracks one level of hierarchical drill-down.
type TopologyDrillSelection struct {
	Domain string // e.g. "region"
	Key    string // e.g. "topology.kubernetes.io/region"
	Value  string // e.g. "us-east-1" (empty if just selected the domain row)
}

// TopologyViewPod represents a pod in the Topology Pods table.
type TopologyViewPod struct {
	Namespace string
	Node      string // node name or "<pending>"
	Name      string
	Topology  string // "rack: rack-0", "(rack: rack-0)", "N/A"
	Phase     string // Running, Pending, etc.
}

// TopologyViewData holds a point-in-time snapshot of all topology-relevant cluster state.
type TopologyViewData struct {
	Domains         []TopologyDomainRow         // sorted broadest to narrowest, N/A last
	NodeLabels      map[string]map[string]string // nodeName -> labelKey -> labelValue
	Pods            []TopologyViewPod           // all pods with topology info
	DomainToKey     map[string]string           // domain -> node label key (from ClusterTopology)
	GPUSummary      *GPUSummary                 // pre-aggregated GPU counts by resource hierarchy
	NodeGPUProducts map[string]string           // nodeName -> short GPU type (e.g. "H200")
	NodeGPUCapacity map[string]int64            // nodeName -> total GPU count from status.allocatable
	RawPods         []TopologyPodInput          // raw pod data for GPU aggregation in topology drill-down
}

// ViewTypeName returns a human-readable name for a ViewType.
func ViewTypeName(vt ViewType) string {
	switch vt {
	case ForestView:
		return "ForestView"
	case PodCliqueSetView:
		return "PodCliqueSetView"
	case PodCliqueSetReplicaView:
		return "PodCliqueSetReplicaView"
	case PodCliqueScalingGroupView:
		return "PodCliqueScalingGroupView"
	case PodCliqueScalingGroupReplicaView:
		return "PodCliqueScalingGroupReplicaView"
	case PodCliqueView:
		return "PodCliqueView"
	case PodView:
		return "PodView"
	case ContainersView:
		return "ContainersView"
	case TopologyView:
		return "TopologyView"
	default:
		return fmt.Sprintf("ViewType(%d)", vt)
	}
}

// ContainerInfo holds display-ready info about a container in a Pod.
type ContainerInfo struct {
	Name         string
	Image        string
	Ready        bool
	State        string // "Running", "Waiting", "Terminated"
	RestartCount int32
}

// SortResourcesByName sorts a slice of Resource in place by Name.
// This ensures stable, deterministic row ordering in the TUI regardless
// of Go map iteration order or informer store List() order.
func SortResourcesByName(resources []Resource) {
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Name < resources[j].Name
	})
}

// SortResourceMapsByName sorts every []Resource value in the map by Name.
func SortResourceMapsByName(m map[string][]Resource) {
	for _, resources := range m {
		SortResourcesByName(resources)
	}
}

// PaneName returns a human-readable name for a Pane.
func PaneName(p Pane) string {
	switch p {
	case ResourcesPane:
		return "Resources"
	case EventsPane:
		return "Events"
	case TopologyDomainsPane:
		return "TopologyDomains"
	case TopologyPodsPane:
		return "TopologyPods"
	default:
		return fmt.Sprintf("Pane(%d)", p)
	}
}
