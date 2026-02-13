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
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// GlobalCache provides a single, informer-backed cache of all cluster state
// needed by the TUI. It replaces both DataProvider (direct API calls) and
// TopologyCache (informer-based topology data) with a unified interface.
//
// All read operations are served from local informer caches — zero on-demand
// API calls except for GetPodYAML which is a single GET for a specific pod.
type GlobalCache interface {
	// Lifecycle
	Start(ctx context.Context) error
	WaitForSync(ctx context.Context) bool
	Stop()

	// Notifications — signals when any watched resource changes (debounced).
	Updates() <-chan struct{}

	// Snapshot returns a point-in-time snapshot of all cached data.
	// Thread-safe, never hits the API server.
	Snapshot() *CacheSnapshot

	// GetPodYAML is the one exception — a direct API GET for a specific pod's YAML.
	// Pod YAML is large, rarely accessed, and not worth caching globally.
	GetPodYAML(ctx context.Context, podName, namespace string) (string, error)

	// GetResourceYAML fetches any resource's YAML by type and name.
	// Supported types: "PodCliqueSet", "PodCliqueScalingGroup", "PodClique", "Pod".
	// For virtual types like "(PodCliqueSet replica)", callers should resolve to the parent.
	GetResourceYAML(ctx context.Context, resourceType, name, namespace string) (string, error)
}

// WarningConfigurable is an optional interface that GlobalCache implementations
// can satisfy to receive a warning callback before Start(). The TUI uses this
// to capture non-fatal startup warnings (e.g. missing CRDs) and route them to
// the error log box.
type WarningConfigurable interface {
	SetOnWarning(fn func(string))
}

// CacheSnapshot holds a point-in-time snapshot of all cluster state needed by the TUI.
// Built from informer caches on every debounced change. All fields are read-only
// after construction.
type CacheSnapshot struct {
	// Forest view data — top-level PodCliqueSets
	PodCliqueSets []Resource

	// PodCliqueSet specs — needed for topology info resolution
	PodCliqueSetSpecs map[string]*corev1alpha1.PodCliqueSet // pcsName -> PCS

	// Hierarchy — pre-built from labels on cached resources
	ReplicaIndexesByPCS     map[string][]string   // pcsName -> sorted replica indexes
	ScalingGroupsByReplica  map[string][]Resource  // "pcsName/replicaIndex" -> []PCSG resources
	PodCliquesByReplica     map[string][]Resource  // "pcsName/replicaIndex" -> standalone PodCliques
	ReplicaIndexesByPCSG    map[string][]string    // pcsgName -> sorted PCSG replica indexes
	PodCliquesByPCSG        map[string][]Resource  // pcsgName -> []PodClique resources
	PodCliquesByPCSGReplica map[string][]Resource  // "pcsgName/replicaIndex" -> []PodClique resources
	PodsByPodClique         map[string][]Resource  // podCliqueName -> []Pod resources

	// Events — cached and pre-indexed by involved object
	EventsByObject map[string][]Event // "Kind/name" -> events (sorted newest first)

	// Topology view data (same as today)
	TopologyViewData *TopologyViewData

	// GPU data (same as today)
	GPUSummary      *GPUSummary
	NodeGPUProducts map[string]string

	// Raw caches for topology resolution
	NodeLabels      map[string]map[string]string // nodeName -> filtered topology labels
	PodInfos        map[string]CachedPodInfo     // all PCS-managed pods: podName -> info
	NodeGPUCapacity map[string]int64             // nodeName -> total GPU count from status.allocatable
}

// PodCliqueSetInfo holds display-ready info about a PodCliqueSet.
type PodCliqueSetInfo struct {
	Name      string
	Namespace string
	Resource  Resource
}

// GetEventsForPCS returns all events related to a PodCliqueSet and its descendants.
func (s *CacheSnapshot) GetEventsForPCS(pcsName string) []Event {
	if s == nil {
		return nil
	}

	seen := make(map[string]bool)
	var result []Event

	// PCS itself
	addUniqueEvents(&result, s.EventsByObject["PodCliqueSet/"+pcsName], seen)

	// Collect PCSG names that belong to this PCS (used to scope PCSG child lookups)
	pcsgNamesForPCS := make(map[string]bool)

	// All PCSGs for this PCS (across all replicas)
	for key, pcsgResources := range s.ScalingGroupsByReplica {
		if strings.HasPrefix(key, pcsName+"/") {
			for _, pcsg := range pcsgResources {
				pcsgNamesForPCS[pcsg.Name] = true
				addUniqueEvents(&result, s.EventsByObject["PodCliqueScalingGroup/"+pcsg.Name], seen)
			}
		}
	}

	// All PodCliques for this PCS — standalone cliques (across all replicas)
	for key, pcResources := range s.PodCliquesByReplica {
		if strings.HasPrefix(key, pcsName+"/") {
			for _, pc := range pcResources {
				addUniqueEvents(&result, s.EventsByObject["PodClique/"+pc.Name], seen)
			}
		}
	}

	// PodCliques within PCSGs that belong to this PCS
	for pcsgName, pcResources := range s.PodCliquesByPCSG {
		if !pcsgNamesForPCS[pcsgName] {
			continue
		}
		for _, pc := range pcResources {
			addUniqueEvents(&result, s.EventsByObject["PodClique/"+pc.Name], seen)
		}
	}
	for pcsgReplicaKey, pcResources := range s.PodCliquesByPCSGReplica {
		// pcsgReplicaKey is "pcsgName/replicaIndex" — extract pcsgName
		pcsgName := pcsgReplicaKey
		if idx := strings.Index(pcsgReplicaKey, "/"); idx >= 0 {
			pcsgName = pcsgReplicaKey[:idx]
		}
		if !pcsgNamesForPCS[pcsgName] {
			continue
		}
		for _, pc := range pcResources {
			addUniqueEvents(&result, s.EventsByObject["PodClique/"+pc.Name], seen)
		}
	}

	// All Pods for this PCS
	for podName, podInfo := range s.PodInfos {
		if podInfo.Labels["app.kubernetes.io/part-of"] == pcsName {
			addUniqueEvents(&result, s.EventsByObject["Pod/"+podName], seen)
		}
	}

	sortEventsByTimestamp(result)
	return result
}

// GetEventsForReplica returns events for resources in a specific PCS replica.
func (s *CacheSnapshot) GetEventsForReplica(pcsName, replicaIndex string) []Event {
	if s == nil {
		return nil
	}

	seen := make(map[string]bool)
	var result []Event

	replicaKey := pcsName + "/" + replicaIndex

	// PCSGs in this replica
	for _, pcsg := range s.ScalingGroupsByReplica[replicaKey] {
		addUniqueEvents(&result, s.EventsByObject["PodCliqueScalingGroup/"+pcsg.Name], seen)
		// PodCliques within this PCSG
		for _, pc := range s.PodCliquesByPCSG[pcsg.Name] {
			addUniqueEvents(&result, s.EventsByObject["PodClique/"+pc.Name], seen)
			// Pods within this PodClique
			for _, pod := range s.PodsByPodClique[pc.Name] {
				addUniqueEvents(&result, s.EventsByObject["Pod/"+pod.Name], seen)
			}
		}
	}

	// Standalone PodCliques in this replica
	for _, pc := range s.PodCliquesByReplica[replicaKey] {
		addUniqueEvents(&result, s.EventsByObject["PodClique/"+pc.Name], seen)
		for _, pod := range s.PodsByPodClique[pc.Name] {
			addUniqueEvents(&result, s.EventsByObject["Pod/"+pod.Name], seen)
		}
	}

	sortEventsByTimestamp(result)
	return result
}

// GetEventsForPCSG returns events for a PCSG and its descendants.
func (s *CacheSnapshot) GetEventsForPCSG(pcsgName string) []Event {
	if s == nil {
		return nil
	}

	seen := make(map[string]bool)
	var result []Event

	// PCSG itself
	addUniqueEvents(&result, s.EventsByObject["PodCliqueScalingGroup/"+pcsgName], seen)

	// PodCliques within this PCSG
	for _, pc := range s.PodCliquesByPCSG[pcsgName] {
		addUniqueEvents(&result, s.EventsByObject["PodClique/"+pc.Name], seen)
		for _, pod := range s.PodsByPodClique[pc.Name] {
			addUniqueEvents(&result, s.EventsByObject["Pod/"+pod.Name], seen)
		}
	}

	// Also check PCSG replica PodCliques
	for key, pcResources := range s.PodCliquesByPCSGReplica {
		if strings.HasPrefix(key, pcsgName+"/") {
			for _, pc := range pcResources {
				addUniqueEvents(&result, s.EventsByObject["PodClique/"+pc.Name], seen)
				for _, pod := range s.PodsByPodClique[pc.Name] {
					addUniqueEvents(&result, s.EventsByObject["Pod/"+pod.Name], seen)
				}
			}
		}
	}

	sortEventsByTimestamp(result)
	return result
}

// GetEventsForPCSGReplica returns events for a specific PCSG replica and its descendants.
func (s *CacheSnapshot) GetEventsForPCSGReplica(pcsgName, replicaIndex string) []Event {
	if s == nil {
		return nil
	}

	seen := make(map[string]bool)
	var result []Event

	replicaKey := pcsgName + "/" + replicaIndex

	// PodCliques in this PCSG replica
	for _, pc := range s.PodCliquesByPCSGReplica[replicaKey] {
		addUniqueEvents(&result, s.EventsByObject["PodClique/"+pc.Name], seen)
		for _, pod := range s.PodsByPodClique[pc.Name] {
			addUniqueEvents(&result, s.EventsByObject["Pod/"+pod.Name], seen)
		}
	}

	sortEventsByTimestamp(result)
	return result
}

// GetEventsForPodClique returns events for a PodClique and its child pods.
func (s *CacheSnapshot) GetEventsForPodClique(pcName string) []Event {
	if s == nil {
		return nil
	}

	seen := make(map[string]bool)
	var result []Event

	// PodClique itself
	addUniqueEvents(&result, s.EventsByObject["PodClique/"+pcName], seen)

	// Pods within this PodClique
	for _, pod := range s.PodsByPodClique[pcName] {
		addUniqueEvents(&result, s.EventsByObject["Pod/"+pod.Name], seen)
	}

	sortEventsByTimestamp(result)
	return result
}

// FormatAge formats a time duration into a human-readable age string.
func FormatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	duration := time.Since(t)

	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd", int(duration.Hours()/24))
}

// Helper functions

func addUniqueEvents(result *[]Event, events []Event, seen map[string]bool) {
	for _, e := range events {
		key := e.Kind + "/" + e.Parent + "/" + e.Reason + "/" + e.Message
		if !seen[key] {
			seen[key] = true
			*result = append(*result, e)
		}
	}
}

func sortEventsByTimestamp(events []Event) {
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})
}
