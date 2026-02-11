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
	"testing"
	"time"
)

// TestGetEventsForPCS_ScopedToPCS verifies that GetEventsForPCS only returns
// events belonging to the specified PCS, not events from other PCSs.
func TestGetEventsForPCS_ScopedToPCS(t *testing.T) {
	now := time.Now()

	snapshot := &CacheSnapshot{
		// Two PCSs exist: "pcs-a" and "pcs-b"
		PodCliqueSets: []Resource{
			{Name: "pcs-a", Type: "PodCliqueSet"},
			{Name: "pcs-b", Type: "PodCliqueSet"},
		},

		// pcs-a has a PCSG "pcsg-a" under replica 0
		ScalingGroupsByReplica: map[string][]Resource{
			"pcs-a/0": {
				{Name: "pcsg-a", Type: "PodCliqueScalingGroup"},
			},
		},

		// Standalone PodCliques per replica
		PodCliquesByReplica: map[string][]Resource{
			"pcs-b/0": {
				{Name: "pc-b-standalone", Type: "PodClique"},
			},
		},

		// PodCliques within PCSGs
		PodCliquesByPCSG: map[string][]Resource{
			"pcsg-a": {
				{Name: "pc-a-in-pcsg", Type: "PodClique"},
			},
		},

		// PodCliques within PCSG replicas
		PodCliquesByPCSGReplica: map[string][]Resource{
			"pcsg-a/0": {
				{Name: "pc-a-in-pcsg-replica", Type: "PodClique"},
			},
		},

		// Pods
		PodsByPodClique: map[string][]Resource{
			"pc-a-in-pcsg":         {{Name: "pod-a-1", Type: "Pod"}},
			"pc-a-in-pcsg-replica": {{Name: "pod-a-2", Type: "Pod"}},
			"pc-b-standalone":      {{Name: "pod-b-1", Type: "Pod"}},
		},

		// Pod infos with part-of labels
		PodInfos: map[string]CachedPodInfo{
			"pod-a-1": {Labels: map[string]string{"app.kubernetes.io/part-of": "pcs-a"}},
			"pod-a-2": {Labels: map[string]string{"app.kubernetes.io/part-of": "pcs-a"}},
			"pod-b-1": {Labels: map[string]string{"app.kubernetes.io/part-of": "pcs-b"}},
		},

		// Events indexed by object
		EventsByObject: map[string][]Event{
			"PodCliqueSet/pcs-a":              {{Type: "Normal", Kind: "PodCliqueSet", Reason: "Created", Message: "pcs-a created", Parent: "pcs-a", Timestamp: now}},
			"PodCliqueSet/pcs-b":              {{Type: "Normal", Kind: "PodCliqueSet", Reason: "Created", Message: "pcs-b created", Parent: "pcs-b", Timestamp: now}},
			"PodCliqueScalingGroup/pcsg-a":    {{Type: "Normal", Kind: "PodCliqueScalingGroup", Reason: "Scaled", Message: "pcsg-a scaled", Parent: "pcsg-a", Timestamp: now}},
			"PodClique/pc-a-in-pcsg":          {{Type: "Normal", Kind: "PodClique", Reason: "PodCreateSuccessful", Message: "Created pod-a-1", Parent: "pc-a-in-pcsg", Timestamp: now}},
			"PodClique/pc-a-in-pcsg-replica":  {{Type: "Normal", Kind: "PodClique", Reason: "PodCreateSuccessful", Message: "Created pod-a-2", Parent: "pc-a-in-pcsg-replica", Timestamp: now}},
			"PodClique/pc-b-standalone":        {{Type: "Normal", Kind: "PodClique", Reason: "PodCreateSuccessful", Message: "Created pod-b-1", Parent: "pc-b-standalone", Timestamp: now}},
			"Pod/pod-a-1":                     {{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Message: "pod-a-1 scheduled", Parent: "pod-a-1", Timestamp: now}},
			"Pod/pod-a-2":                     {{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Message: "pod-a-2 scheduled", Parent: "pod-a-2", Timestamp: now}},
			"Pod/pod-b-1":                     {{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Message: "pod-b-1 scheduled", Parent: "pod-b-1", Timestamp: now}},
		},
	}

	t.Run("pcs-a gets only its own events", func(t *testing.T) {
		events := snapshot.GetEventsForPCS("pcs-a")

		// Should include events for: pcs-a, pcsg-a, pc-a-in-pcsg, pc-a-in-pcsg-replica, pod-a-1, pod-a-2
		// Should NOT include events for: pcs-b, pc-b-standalone, pod-b-1
		for _, e := range events {
			if e.Message == "pcs-b created" || e.Message == "Created pod-b-1" || e.Message == "pod-b-1 scheduled" {
				t.Errorf("GetEventsForPCS(\"pcs-a\") returned event from pcs-b: %+v", e)
			}
		}

		// Verify expected events are present
		expectedMessages := map[string]bool{
			"pcs-a created":    false,
			"pcsg-a scaled":    false,
			"Created pod-a-1":  false,
			"Created pod-a-2":  false,
			"pod-a-1 scheduled": false,
			"pod-a-2 scheduled": false,
		}
		for _, e := range events {
			if _, ok := expectedMessages[e.Message]; ok {
				expectedMessages[e.Message] = true
			}
		}
		for msg, found := range expectedMessages {
			if !found {
				t.Errorf("GetEventsForPCS(\"pcs-a\") missing expected event: %s", msg)
			}
		}
	})

	t.Run("pcs-b gets only its own events", func(t *testing.T) {
		events := snapshot.GetEventsForPCS("pcs-b")

		// Should include events for: pcs-b, pc-b-standalone, pod-b-1
		// Should NOT include events for: pcs-a, pcsg-a, pc-a-in-pcsg, pod-a-1, pod-a-2
		for _, e := range events {
			if e.Message == "pcs-a created" || e.Message == "pcsg-a scaled" ||
				e.Message == "Created pod-a-1" || e.Message == "Created pod-a-2" ||
				e.Message == "pod-a-1 scheduled" || e.Message == "pod-a-2 scheduled" {
				t.Errorf("GetEventsForPCS(\"pcs-b\") returned event from pcs-a: %+v", e)
			}
		}

		// Verify expected events are present
		expectedMessages := map[string]bool{
			"pcs-b created":    false,
			"Created pod-b-1":  false,
			"pod-b-1 scheduled": false,
		}
		for _, e := range events {
			if _, ok := expectedMessages[e.Message]; ok {
				expectedMessages[e.Message] = true
			}
		}
		for msg, found := range expectedMessages {
			if !found {
				t.Errorf("GetEventsForPCS(\"pcs-b\") missing expected event: %s", msg)
			}
		}
	})

	t.Run("nonexistent PCS returns no events", func(t *testing.T) {
		events := snapshot.GetEventsForPCS("pcs-nonexistent")
		if len(events) != 0 {
			t.Errorf("GetEventsForPCS(\"pcs-nonexistent\") returned %d events, want 0", len(events))
		}
	})

	t.Run("nil snapshot returns nil", func(t *testing.T) {
		var nilSnapshot *CacheSnapshot
		events := nilSnapshot.GetEventsForPCS("pcs-a")
		if events != nil {
			t.Errorf("nil snapshot GetEventsForPCS returned non-nil: %v", events)
		}
	})
}
