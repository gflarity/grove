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
	"time"
)

// Pane represents which pane is active.
type Pane int

const (
	ResourcesPane Pane = iota
	EventsPane
)

// ViewType represents the current view in the hierarchy.
type ViewType int

const (
	ForestView ViewType = iota
	PodCliqueSetView
	PodCliqueSetReplicaView
	PodCliqueScalingGroupView
	PodCliqueView
	PodView
)

// ViewState tracks the current navigation state.
type ViewState struct {
	ViewType             ViewType
	SelectedPodCliqueSet string
	SelectedReplicaIndex string // The replica index (e.g., "0", "1", "2")
	SelectedScalingGroup string
	SelectedPodClique    string
	SelectedPod          string
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
	case PodCliqueView:
		return "PodCliqueView"
	case PodView:
		return "PodView"
	default:
		return fmt.Sprintf("ViewType(%d)", vt)
	}
}

// PaneName returns a human-readable name for a Pane.
func PaneName(p Pane) string {
	switch p {
	case ResourcesPane:
		return "Resources"
	case EventsPane:
		return "Events"
	default:
		return fmt.Sprintf("Pane(%d)", p)
	}
}
