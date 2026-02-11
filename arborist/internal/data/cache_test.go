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

// ---------------------------------------------------------------------------
// GetEventsForReplica
// ---------------------------------------------------------------------------

func TestGetEventsForReplica(t *testing.T) {
	now := time.Now()

	snapshot := &CacheSnapshot{
		ScalingGroupsByReplica: map[string][]Resource{
			"pcs-a/0": {
				{Name: "pcsg-a-0", Type: "PodCliqueScalingGroup"},
			},
			"pcs-a/1": {
				{Name: "pcsg-a-1", Type: "PodCliqueScalingGroup"},
			},
		},
		PodCliquesByReplica: map[string][]Resource{
			"pcs-a/0": {
				{Name: "pc-standalone-0", Type: "PodClique"},
			},
		},
		PodCliquesByPCSG: map[string][]Resource{
			"pcsg-a-0": {
				{Name: "pc-in-pcsg-0", Type: "PodClique"},
			},
			"pcsg-a-1": {
				{Name: "pc-in-pcsg-1", Type: "PodClique"},
			},
		},
		PodsByPodClique: map[string][]Resource{
			"pc-in-pcsg-0":    {{Name: "pod-0a", Type: "Pod"}},
			"pc-standalone-0": {{Name: "pod-0b", Type: "Pod"}},
			"pc-in-pcsg-1":    {{Name: "pod-1a", Type: "Pod"}},
		},
		EventsByObject: map[string][]Event{
			"PodCliqueScalingGroup/pcsg-a-0": {{Type: "Normal", Kind: "PodCliqueScalingGroup", Reason: "Scaled", Message: "pcsg-a-0 scaled", Parent: "pcsg-a-0", Timestamp: now}},
			"PodCliqueScalingGroup/pcsg-a-1": {{Type: "Normal", Kind: "PodCliqueScalingGroup", Reason: "Scaled", Message: "pcsg-a-1 scaled", Parent: "pcsg-a-1", Timestamp: now}},
			"PodClique/pc-in-pcsg-0":         {{Type: "Normal", Kind: "PodClique", Reason: "Created", Message: "pc-in-pcsg-0 created", Parent: "pc-in-pcsg-0", Timestamp: now}},
			"PodClique/pc-in-pcsg-1":         {{Type: "Normal", Kind: "PodClique", Reason: "Created", Message: "pc-in-pcsg-1 created", Parent: "pc-in-pcsg-1", Timestamp: now}},
			"PodClique/pc-standalone-0":       {{Type: "Normal", Kind: "PodClique", Reason: "Created", Message: "pc-standalone-0 created", Parent: "pc-standalone-0", Timestamp: now}},
			"Pod/pod-0a":                      {{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Message: "pod-0a scheduled", Parent: "pod-0a", Timestamp: now}},
			"Pod/pod-0b":                      {{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Message: "pod-0b scheduled", Parent: "pod-0b", Timestamp: now}},
			"Pod/pod-1a":                      {{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Message: "pod-1a scheduled", Parent: "pod-1a", Timestamp: now}},
		},
	}

	t.Run("replica 0 returns only its events", func(t *testing.T) {
		events := snapshot.GetEventsForReplica("pcs-a", "0")

		expected := map[string]bool{
			"pcsg-a-0 scaled":       false,
			"pc-in-pcsg-0 created":  false,
			"pc-standalone-0 created": false,
			"pod-0a scheduled":      false,
			"pod-0b scheduled":      false,
		}
		excluded := []string{"pcsg-a-1 scaled", "pc-in-pcsg-1 created", "pod-1a scheduled"}

		for _, e := range events {
			if _, ok := expected[e.Message]; ok {
				expected[e.Message] = true
			}
			for _, exc := range excluded {
				if e.Message == exc {
					t.Errorf("replica 0 got event from replica 1: %s", e.Message)
				}
			}
		}
		for msg, found := range expected {
			if !found {
				t.Errorf("replica 0 missing expected event: %s", msg)
			}
		}
	})

	t.Run("replica 1 returns only its events", func(t *testing.T) {
		events := snapshot.GetEventsForReplica("pcs-a", "1")

		expected := map[string]bool{
			"pcsg-a-1 scaled":      false,
			"pc-in-pcsg-1 created": false,
			"pod-1a scheduled":     false,
		}
		excluded := []string{"pcsg-a-0 scaled", "pc-in-pcsg-0 created", "pc-standalone-0 created", "pod-0a scheduled", "pod-0b scheduled"}

		for _, e := range events {
			if _, ok := expected[e.Message]; ok {
				expected[e.Message] = true
			}
			for _, exc := range excluded {
				if e.Message == exc {
					t.Errorf("replica 1 got event from replica 0: %s", e.Message)
				}
			}
		}
		for msg, found := range expected {
			if !found {
				t.Errorf("replica 1 missing expected event: %s", msg)
			}
		}
	})

	t.Run("nonexistent replica returns no events", func(t *testing.T) {
		events := snapshot.GetEventsForReplica("pcs-a", "99")
		if len(events) != 0 {
			t.Errorf("nonexistent replica returned %d events, want 0", len(events))
		}
	})

	t.Run("nil snapshot returns nil", func(t *testing.T) {
		var nilSnapshot *CacheSnapshot
		events := nilSnapshot.GetEventsForReplica("pcs-a", "0")
		if events != nil {
			t.Errorf("nil snapshot GetEventsForReplica returned non-nil: %v", events)
		}
	})
}

// ---------------------------------------------------------------------------
// GetEventsForPCSG
// ---------------------------------------------------------------------------

func TestGetEventsForPCSG(t *testing.T) {
	now := time.Now()

	snapshot := &CacheSnapshot{
		PodCliquesByPCSG: map[string][]Resource{
			"pcsg-a": {
				{Name: "pc-a-1", Type: "PodClique"},
				{Name: "pc-a-2", Type: "PodClique"},
			},
			"pcsg-b": {
				{Name: "pc-b-1", Type: "PodClique"},
			},
		},
		PodCliquesByPCSGReplica: map[string][]Resource{
			"pcsg-a/0": {
				{Name: "pc-a-r0", Type: "PodClique"},
			},
			"pcsg-b/0": {
				{Name: "pc-b-r0", Type: "PodClique"},
			},
		},
		PodsByPodClique: map[string][]Resource{
			"pc-a-1":  {{Name: "pod-a-1", Type: "Pod"}},
			"pc-a-2":  {{Name: "pod-a-2", Type: "Pod"}},
			"pc-a-r0": {{Name: "pod-a-r0", Type: "Pod"}},
			"pc-b-1":  {{Name: "pod-b-1", Type: "Pod"}},
		},
		EventsByObject: map[string][]Event{
			"PodCliqueScalingGroup/pcsg-a": {{Type: "Normal", Reason: "Scaled", Message: "pcsg-a scaled", Parent: "pcsg-a", Timestamp: now}},
			"PodCliqueScalingGroup/pcsg-b": {{Type: "Normal", Reason: "Scaled", Message: "pcsg-b scaled", Parent: "pcsg-b", Timestamp: now}},
			"PodClique/pc-a-1":             {{Type: "Normal", Reason: "Created", Message: "pc-a-1 created", Parent: "pc-a-1", Timestamp: now}},
			"PodClique/pc-a-2":             {{Type: "Normal", Reason: "Created", Message: "pc-a-2 created", Parent: "pc-a-2", Timestamp: now}},
			"PodClique/pc-a-r0":            {{Type: "Normal", Reason: "Created", Message: "pc-a-r0 created", Parent: "pc-a-r0", Timestamp: now}},
			"PodClique/pc-b-1":             {{Type: "Normal", Reason: "Created", Message: "pc-b-1 created", Parent: "pc-b-1", Timestamp: now}},
			"Pod/pod-a-1":                  {{Type: "Normal", Reason: "Scheduled", Message: "pod-a-1 scheduled", Parent: "pod-a-1", Timestamp: now}},
			"Pod/pod-a-2":                  {{Type: "Normal", Reason: "Scheduled", Message: "pod-a-2 scheduled", Parent: "pod-a-2", Timestamp: now}},
			"Pod/pod-a-r0":                 {{Type: "Normal", Reason: "Scheduled", Message: "pod-a-r0 scheduled", Parent: "pod-a-r0", Timestamp: now}},
			"Pod/pod-b-1":                  {{Type: "Normal", Reason: "Scheduled", Message: "pod-b-1 scheduled", Parent: "pod-b-1", Timestamp: now}},
		},
	}

	t.Run("pcsg-a includes own + child events", func(t *testing.T) {
		events := snapshot.GetEventsForPCSG("pcsg-a")

		expected := map[string]bool{
			"pcsg-a scaled":    false,
			"pc-a-1 created":   false,
			"pc-a-2 created":   false,
			"pc-a-r0 created":  false,
			"pod-a-1 scheduled": false,
			"pod-a-2 scheduled": false,
			"pod-a-r0 scheduled": false,
		}
		excluded := []string{"pcsg-b scaled", "pc-b-1 created", "pod-b-1 scheduled"}

		for _, e := range events {
			if _, ok := expected[e.Message]; ok {
				expected[e.Message] = true
			}
			for _, exc := range excluded {
				if e.Message == exc {
					t.Errorf("pcsg-a got event from pcsg-b: %s", e.Message)
				}
			}
		}
		for msg, found := range expected {
			if !found {
				t.Errorf("pcsg-a missing expected event: %s", msg)
			}
		}
	})

	t.Run("nonexistent PCSG returns no events", func(t *testing.T) {
		events := snapshot.GetEventsForPCSG("pcsg-nope")
		if len(events) != 0 {
			t.Errorf("nonexistent PCSG returned %d events, want 0", len(events))
		}
	})

	t.Run("nil snapshot returns nil", func(t *testing.T) {
		var nilSnapshot *CacheSnapshot
		events := nilSnapshot.GetEventsForPCSG("pcsg-a")
		if events != nil {
			t.Errorf("nil snapshot GetEventsForPCSG returned non-nil: %v", events)
		}
	})
}

// ---------------------------------------------------------------------------
// GetEventsForPCSGReplica
// ---------------------------------------------------------------------------

func TestGetEventsForPCSGReplica(t *testing.T) {
	now := time.Now()

	snapshot := &CacheSnapshot{
		PodCliquesByPCSGReplica: map[string][]Resource{
			"pcsg-a/0": {
				{Name: "pc-r0-a", Type: "PodClique"},
				{Name: "pc-r0-b", Type: "PodClique"},
			},
			"pcsg-a/1": {
				{Name: "pc-r1-a", Type: "PodClique"},
			},
		},
		PodsByPodClique: map[string][]Resource{
			"pc-r0-a": {{Name: "pod-r0a", Type: "Pod"}},
			"pc-r0-b": {{Name: "pod-r0b", Type: "Pod"}},
			"pc-r1-a": {{Name: "pod-r1a", Type: "Pod"}},
		},
		EventsByObject: map[string][]Event{
			"PodClique/pc-r0-a": {{Type: "Normal", Reason: "Created", Message: "pc-r0-a created", Parent: "pc-r0-a", Timestamp: now}},
			"PodClique/pc-r0-b": {{Type: "Normal", Reason: "Created", Message: "pc-r0-b created", Parent: "pc-r0-b", Timestamp: now}},
			"PodClique/pc-r1-a": {{Type: "Normal", Reason: "Created", Message: "pc-r1-a created", Parent: "pc-r1-a", Timestamp: now}},
			"Pod/pod-r0a":       {{Type: "Normal", Reason: "Scheduled", Message: "pod-r0a scheduled", Parent: "pod-r0a", Timestamp: now}},
			"Pod/pod-r0b":       {{Type: "Normal", Reason: "Scheduled", Message: "pod-r0b scheduled", Parent: "pod-r0b", Timestamp: now}},
			"Pod/pod-r1a":       {{Type: "Normal", Reason: "Scheduled", Message: "pod-r1a scheduled", Parent: "pod-r1a", Timestamp: now}},
		},
	}

	t.Run("replica 0 returns only its events", func(t *testing.T) {
		events := snapshot.GetEventsForPCSGReplica("pcsg-a", "0")

		expected := map[string]bool{
			"pc-r0-a created":   false,
			"pc-r0-b created":   false,
			"pod-r0a scheduled": false,
			"pod-r0b scheduled": false,
		}
		excluded := []string{"pc-r1-a created", "pod-r1a scheduled"}

		for _, e := range events {
			if _, ok := expected[e.Message]; ok {
				expected[e.Message] = true
			}
			for _, exc := range excluded {
				if e.Message == exc {
					t.Errorf("replica 0 got event from replica 1: %s", e.Message)
				}
			}
		}
		for msg, found := range expected {
			if !found {
				t.Errorf("replica 0 missing expected event: %s", msg)
			}
		}
	})

	t.Run("nonexistent replica returns no events", func(t *testing.T) {
		events := snapshot.GetEventsForPCSGReplica("pcsg-a", "99")
		if len(events) != 0 {
			t.Errorf("nonexistent PCSG replica returned %d events, want 0", len(events))
		}
	})

	t.Run("nil snapshot returns nil", func(t *testing.T) {
		var nilSnapshot *CacheSnapshot
		events := nilSnapshot.GetEventsForPCSGReplica("pcsg-a", "0")
		if events != nil {
			t.Errorf("nil snapshot GetEventsForPCSGReplica returned non-nil: %v", events)
		}
	})
}

// ---------------------------------------------------------------------------
// GetEventsForPodClique
// ---------------------------------------------------------------------------

func TestGetEventsForPodClique(t *testing.T) {
	now := time.Now()

	snapshot := &CacheSnapshot{
		PodsByPodClique: map[string][]Resource{
			"pc-a": {
				{Name: "pod-a-1", Type: "Pod"},
				{Name: "pod-a-2", Type: "Pod"},
			},
			"pc-b": {
				{Name: "pod-b-1", Type: "Pod"},
			},
		},
		EventsByObject: map[string][]Event{
			"PodClique/pc-a": {{Type: "Normal", Reason: "Created", Message: "pc-a created", Parent: "pc-a", Timestamp: now}},
			"PodClique/pc-b": {{Type: "Normal", Reason: "Created", Message: "pc-b created", Parent: "pc-b", Timestamp: now}},
			"Pod/pod-a-1":    {{Type: "Normal", Reason: "Scheduled", Message: "pod-a-1 scheduled", Parent: "pod-a-1", Timestamp: now}},
			"Pod/pod-a-2":    {{Type: "Warning", Reason: "Unschedulable", Message: "pod-a-2 unschedulable", Parent: "pod-a-2", Timestamp: now.Add(-time.Minute)}},
			"Pod/pod-b-1":    {{Type: "Normal", Reason: "Scheduled", Message: "pod-b-1 scheduled", Parent: "pod-b-1", Timestamp: now}},
		},
	}

	t.Run("pc-a includes own + child pod events", func(t *testing.T) {
		events := snapshot.GetEventsForPodClique("pc-a")

		expected := map[string]bool{
			"pc-a created":          false,
			"pod-a-1 scheduled":     false,
			"pod-a-2 unschedulable": false,
		}
		excluded := []string{"pc-b created", "pod-b-1 scheduled"}

		for _, e := range events {
			if _, ok := expected[e.Message]; ok {
				expected[e.Message] = true
			}
			for _, exc := range excluded {
				if e.Message == exc {
					t.Errorf("pc-a got event from pc-b: %s", e.Message)
				}
			}
		}
		for msg, found := range expected {
			if !found {
				t.Errorf("pc-a missing expected event: %s", msg)
			}
		}
	})

	t.Run("events are sorted newest first", func(t *testing.T) {
		events := snapshot.GetEventsForPodClique("pc-a")
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events, got %d", len(events))
		}
		for i := 1; i < len(events); i++ {
			if events[i].Timestamp.After(events[i-1].Timestamp) {
				t.Errorf("events not sorted newest first: [%d]=%v after [%d]=%v",
					i, events[i].Timestamp, i-1, events[i-1].Timestamp)
			}
		}
	})

	t.Run("nonexistent PodClique returns no events", func(t *testing.T) {
		events := snapshot.GetEventsForPodClique("pc-nope")
		if len(events) != 0 {
			t.Errorf("nonexistent PodClique returned %d events, want 0", len(events))
		}
	})

	t.Run("nil snapshot returns nil", func(t *testing.T) {
		var nilSnapshot *CacheSnapshot
		events := nilSnapshot.GetEventsForPodClique("pc-a")
		if events != nil {
			t.Errorf("nil snapshot GetEventsForPodClique returned non-nil: %v", events)
		}
	})
}

// ---------------------------------------------------------------------------
// FormatAge (data package version)
// ---------------------------------------------------------------------------

func TestFormatAge(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{name: "zero time", t: time.Time{}, want: "unknown"},
		{name: "5 seconds ago", t: now.Add(-5 * time.Second), want: "5s"},
		{name: "59 seconds ago", t: now.Add(-59 * time.Second), want: "59s"},
		{name: "1 minute ago", t: now.Add(-1 * time.Minute), want: "1m"},
		{name: "59 minutes ago", t: now.Add(-59 * time.Minute), want: "59m"},
		{name: "1 hour ago", t: now.Add(-1 * time.Hour), want: "1h"},
		{name: "23 hours ago", t: now.Add(-23 * time.Hour), want: "23h"},
		{name: "1 day ago", t: now.Add(-24 * time.Hour), want: "1d"},
		{name: "7 days ago", t: now.Add(-7 * 24 * time.Hour), want: "7d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAge(tt.t)
			if got != tt.want {
				t.Errorf("FormatAge() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// addUniqueEvents
// ---------------------------------------------------------------------------

func TestAddUniqueEvents(t *testing.T) {
	now := time.Now()

	t.Run("deduplicates by key", func(t *testing.T) {
		seen := make(map[string]bool)
		var result []Event

		events := []Event{
			{Kind: "Pod", Parent: "pod-a", Reason: "Scheduled", Message: "assigned to node", Timestamp: now},
			{Kind: "Pod", Parent: "pod-a", Reason: "Scheduled", Message: "assigned to node", Timestamp: now.Add(-time.Second)},
		}

		addUniqueEvents(&result, events, seen)
		if len(result) != 1 {
			t.Errorf("expected 1 unique event, got %d", len(result))
		}
	})

	t.Run("different messages are not deduplicated", func(t *testing.T) {
		seen := make(map[string]bool)
		var result []Event

		events := []Event{
			{Kind: "Pod", Parent: "pod-a", Reason: "Scheduled", Message: "msg 1", Timestamp: now},
			{Kind: "Pod", Parent: "pod-a", Reason: "Scheduled", Message: "msg 2", Timestamp: now},
		}

		addUniqueEvents(&result, events, seen)
		if len(result) != 2 {
			t.Errorf("expected 2 events, got %d", len(result))
		}
	})

	t.Run("nil events is safe", func(t *testing.T) {
		seen := make(map[string]bool)
		var result []Event

		addUniqueEvents(&result, nil, seen)
		if len(result) != 0 {
			t.Errorf("expected 0 events, got %d", len(result))
		}
	})

	t.Run("accumulates across multiple calls", func(t *testing.T) {
		seen := make(map[string]bool)
		var result []Event

		batch1 := []Event{
			{Kind: "Pod", Parent: "pod-a", Reason: "Scheduled", Message: "msg 1", Timestamp: now},
		}
		batch2 := []Event{
			{Kind: "Pod", Parent: "pod-a", Reason: "Scheduled", Message: "msg 1", Timestamp: now}, // duplicate
			{Kind: "Pod", Parent: "pod-b", Reason: "Pulled", Message: "msg 2", Timestamp: now},
		}

		addUniqueEvents(&result, batch1, seen)
		addUniqueEvents(&result, batch2, seen)
		if len(result) != 2 {
			t.Errorf("expected 2 unique events across calls, got %d", len(result))
		}
	})
}
