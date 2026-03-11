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

package clusterstate

import (
	"sort"
	"strings"
)

// eventKey is a struct-based composite key for event deduplication.
// Using a struct instead of string concatenation avoids key collisions
// when field values contain the delimiter character.
type eventKey struct {
	Kind    string
	Parent  string
	Reason  string
	Message string
}

// GetEventsForPCS returns all events related to a PodCliqueSet and its descendants.
func (s *CacheSnapshot) GetEventsForPCS(pcsName string) []Event {
	return s.gatherEvents(func(result *[]Event, seen map[eventKey]bool) {
		// PCS itself
		addUniqueEvents(result, s.EventsByObject[ResourceTypePodCliqueSet+"/"+pcsName], seen)

		// All replicas: gather PCSG descendants and standalone PodCliques per replica
		for _, replicaIndex := range s.ReplicaIndexesByPCS[pcsName] {
			replicaKey := CompositeKey(pcsName, replicaIndex)
			for _, pcsg := range s.ScalingGroupsByReplica[replicaKey] {
				s.gatherPCSGDescendantEvents(result, pcsg.Name, seen)
			}
			s.gatherPodCliqueEvents(result, s.PodCliquesByReplica[replicaKey], seen)
		}
	})
}

// GetEventsForReplica returns events for resources in a specific PCS replica.
func (s *CacheSnapshot) GetEventsForReplica(pcsName, replicaIndex string) []Event {
	return s.gatherEvents(func(result *[]Event, seen map[eventKey]bool) {
		replicaKey := pcsName + "/" + replicaIndex
		for _, pcsg := range s.ScalingGroupsByReplica[replicaKey] {
			s.gatherPCSGDescendantEvents(result, pcsg.Name, seen)
		}
		s.gatherPodCliqueEvents(result, s.PodCliquesByReplica[replicaKey], seen)
	})
}

// GetEventsForPCSG returns events for a PCSG and its descendants.
func (s *CacheSnapshot) GetEventsForPCSG(pcsgName string) []Event {
	return s.gatherEvents(func(result *[]Event, seen map[eventKey]bool) {
		s.gatherPCSGDescendantEvents(result, pcsgName, seen)
	})
}

// GetEventsForPCSGReplica returns events for a specific PCSG replica and its descendants.
func (s *CacheSnapshot) GetEventsForPCSGReplica(pcsgName, replicaIndex string) []Event {
	return s.gatherEvents(func(result *[]Event, seen map[eventKey]bool) {
		replicaKey := pcsgName + "/" + replicaIndex
		s.gatherPodCliqueEvents(result, s.PodCliquesByPCSGReplica[replicaKey], seen)
	})
}

// GetEventsForPodClique returns events for a PodClique and its child pods.
func (s *CacheSnapshot) GetEventsForPodClique(pcName string) []Event {
	return s.gatherEvents(func(result *[]Event, seen map[eventKey]bool) {
		s.gatherPodCliqueEvents(result, []Resource{{Name: pcName}}, seen)
	})
}

// gatherEvents handles the common nil-check, seen-map init, gather, and sort
// boilerplate shared by the GetEventsFor* methods.
func (s *CacheSnapshot) gatherEvents(gather func(result *[]Event, seen map[eventKey]bool)) []Event {
	if s == nil {
		return nil
	}
	seen := make(map[eventKey]bool)
	var result []Event
	gather(&result, seen)
	sortEventsByTimestamp(result)
	return result
}

// gatherPCSGDescendantEvents collects events for a PCSG and all its descendant
// PodCliques and Pods. It checks both PodCliquesByPCSG and PodCliquesByPCSGReplica
// to ensure completeness, relying on seen for deduplication.
func (s *CacheSnapshot) gatherPCSGDescendantEvents(result *[]Event, pcsgName string, seen map[eventKey]bool) {
	addUniqueEvents(result, s.EventsByObject[ResourceTypePCSG+"/"+pcsgName], seen)
	s.gatherPodCliqueEvents(result, s.PodCliquesByPCSG[pcsgName], seen)
	for key, pcResources := range s.PodCliquesByPCSGReplica {
		if strings.HasPrefix(key, pcsgName+"/") {
			s.gatherPodCliqueEvents(result, pcResources, seen)
		}
	}
}

// gatherPodCliqueEvents collects events for a list of PodCliques and their child pods.
// This is the common pattern shared by GetEventsForReplica, GetEventsForPCSG,
// GetEventsForPCSGReplica, and GetEventsForPodClique.
func (s *CacheSnapshot) gatherPodCliqueEvents(result *[]Event, pcs []Resource, seen map[eventKey]bool) {
	for _, pc := range pcs {
		addUniqueEvents(result, s.EventsByObject[ResourceTypePodClique+"/"+pc.Name], seen)
		for _, pod := range s.PodsByPodClique[pc.Name] {
			addUniqueEvents(result, s.EventsByObject[ResourceTypePod+"/"+pod.Name], seen)
		}
	}
}

func addUniqueEvents(result *[]Event, events []Event, seen map[eventKey]bool) {
	for _, e := range events {
		key := eventKey{Kind: e.Kind, Parent: e.Parent, Reason: e.Reason, Message: e.Message}
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
