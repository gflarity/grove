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

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ai-dynamo/grove/arborist/internal/data"
)

func TestGroupPodsByTopology(t *testing.T) {
	tests := []struct {
		name            string
		podInfo         map[string]data.CachedPodInfo
		nodeLabels      map[string]map[string]string
		labelKey        string
		wantGroupCount  int
		wantGroups      map[string][]string // value -> pod names
		wantUnscheduled []string
	}{
		{
			name: "pods across two racks",
			podInfo: map[string]data.CachedPodInfo{
				"foo-0-worker-0": {NodeName: "node-1", Labels: map[string]string{}},
				"foo-0-worker-1": {NodeName: "node-2", Labels: map[string]string{}},
				"foo-0-worker-2": {NodeName: "node-3", Labels: map[string]string{}},
			},
			nodeLabels: map[string]map[string]string{
				"node-1": {"topology.io/rack": "rack-0"},
				"node-2": {"topology.io/rack": "rack-0"},
				"node-3": {"topology.io/rack": "rack-1"},
			},
			labelKey:       "topology.io/rack",
			wantGroupCount: 2,
			wantGroups: map[string][]string{
				"rack-0": {"foo-0-worker-0", "foo-0-worker-1"},
				"rack-1": {"foo-0-worker-2"},
			},
			wantUnscheduled: nil,
		},
		{
			name: "some pods unscheduled",
			podInfo: map[string]data.CachedPodInfo{
				"foo-0-worker-0": {NodeName: "node-1", Labels: map[string]string{}},
				"foo-0-worker-1": {NodeName: "", Labels: map[string]string{}},
			},
			nodeLabels: map[string]map[string]string{
				"node-1": {"topology.io/rack": "rack-0"},
			},
			labelKey:       "topology.io/rack",
			wantGroupCount: 1,
			wantGroups: map[string][]string{
				"rack-0": {"foo-0-worker-0"},
			},
			wantUnscheduled: []string{"foo-0-worker-1"},
		},
		{
			name: "all pods unscheduled",
			podInfo: map[string]data.CachedPodInfo{
				"foo-0-worker-0": {NodeName: "", Labels: map[string]string{}},
				"foo-0-worker-1": {NodeName: "", Labels: map[string]string{}},
			},
			nodeLabels:      map[string]map[string]string{},
			labelKey:        "topology.io/rack",
			wantGroupCount:  0,
			wantGroups:      map[string][]string{},
			wantUnscheduled: []string{"foo-0-worker-0", "foo-0-worker-1"},
		},
		{
			name: "node missing topology label treated as unscheduled",
			podInfo: map[string]data.CachedPodInfo{
				"foo-0-worker-0": {NodeName: "node-1", Labels: map[string]string{}},
			},
			nodeLabels: map[string]map[string]string{
				"node-1": {}, // no rack label
			},
			labelKey:        "topology.io/rack",
			wantGroupCount:  0,
			wantGroups:      map[string][]string{},
			wantUnscheduled: []string{"foo-0-worker-0"},
		},
		{
			name:            "no pods",
			podInfo:         map[string]data.CachedPodInfo{},
			nodeLabels:      map[string]map[string]string{},
			labelKey:        "topology.io/rack",
			wantGroupCount:  0,
			wantGroups:      map[string][]string{},
			wantUnscheduled: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups, unscheduled := groupPodsByTopology(tt.podInfo, tt.nodeLabels, tt.labelKey)

			if len(groups) != tt.wantGroupCount {
				t.Errorf("got %d groups, want %d", len(groups), tt.wantGroupCount)
			}

			for _, group := range groups {
				wantPods, ok := tt.wantGroups[group.Value]
				if !ok {
					t.Errorf("unexpected group %q", group.Value)
					continue
				}
				if len(group.Pods) != len(wantPods) {
					t.Errorf("group %q: got %d pods, want %d", group.Value, len(group.Pods), len(wantPods))
					continue
				}
				for i, pod := range group.Pods {
					if pod != wantPods[i] {
						t.Errorf("group %q pod[%d]: got %q, want %q", group.Value, i, pod, wantPods[i])
					}
				}
			}

			if len(unscheduled) != len(tt.wantUnscheduled) {
				t.Errorf("got %d unscheduled, want %d", len(unscheduled), len(tt.wantUnscheduled))
			} else {
				for i, pod := range unscheduled {
					if pod != tt.wantUnscheduled[i] {
						t.Errorf("unscheduled[%d]: got %q, want %q", i, pod, tt.wantUnscheduled[i])
					}
				}
			}
		})
	}
}

func TestGroupPodsByTopology_SortOrder(t *testing.T) {
	podInfo := map[string]data.CachedPodInfo{
		"pod-c": {NodeName: "node-2", Labels: map[string]string{}},
		"pod-a": {NodeName: "node-1", Labels: map[string]string{}},
		"pod-b": {NodeName: "node-1", Labels: map[string]string{}},
	}
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-1"},
		"node-2": {"topology.io/rack": "rack-0"},
	}

	groups, _ := groupPodsByTopology(podInfo, nodeLabels, "topology.io/rack")

	// Groups should be sorted alphabetically by value
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Value != "rack-0" {
		t.Errorf("first group should be rack-0, got %q", groups[0].Value)
	}
	if groups[1].Value != "rack-1" {
		t.Errorf("second group should be rack-1, got %q", groups[1].Value)
	}

	// Pods within rack-1 should be sorted
	if len(groups[1].Pods) != 2 {
		t.Fatalf("rack-1 should have 2 pods, got %d", len(groups[1].Pods))
	}
	if groups[1].Pods[0] != "pod-a" || groups[1].Pods[1] != "pod-b" {
		t.Errorf("pods in rack-1 should be [pod-a, pod-b], got %v", groups[1].Pods)
	}
}

func TestPrintTopologyTree(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		labelKey    string
		pcsName     string
		namespace   string
		groups      []topologyGroup
		unscheduled []string
		wantLines   []string
	}{
		{
			name:      "basic tree with two groups",
			domain:    "rack",
			labelKey:  "topology.io/rack",
			pcsName:   "foo",
			namespace: "default",
			groups: []topologyGroup{
				{Value: "rack-0", Pods: []string{"foo-0-worker-0", "foo-0-worker-1"}},
				{Value: "rack-1", Pods: []string{"foo-0-worker-2"}},
			},
			unscheduled: nil,
			wantLines: []string{
				"Topology: rack (topology.io/rack)",
				"PodCliqueSet: foo (namespace: default)",
				"",
				"┌ rack: rack-0",
				"├─ foo-0-worker-0",
				"└─ foo-0-worker-1",
				"",
				"┌ rack: rack-1",
				"└─ foo-0-worker-2",
			},
		},
		{
			name:      "tree with unscheduled pods",
			domain:    "rack",
			labelKey:  "topology.io/rack",
			pcsName:   "bar",
			namespace: "prod",
			groups: []topologyGroup{
				{Value: "rack-0", Pods: []string{"bar-0-worker-0"}},
			},
			unscheduled: []string{"bar-0-worker-1"},
			wantLines: []string{
				"Topology: rack (topology.io/rack)",
				"PodCliqueSet: bar (namespace: prod)",
				"",
				"┌ rack: rack-0",
				"└─ bar-0-worker-0",
				"",
				"┌ <unscheduled>",
				"└─ bar-0-worker-1",
			},
		},
		{
			name:        "only unscheduled pods",
			domain:      "zone",
			labelKey:    "topology.kubernetes.io/zone",
			pcsName:     "baz",
			namespace:   "default",
			groups:      []topologyGroup{},
			unscheduled: []string{"baz-0-worker-0", "baz-0-worker-1"},
			wantLines: []string{
				"Topology: zone (topology.kubernetes.io/zone)",
				"PodCliqueSet: baz (namespace: default)",
				"",
				"┌ <unscheduled>",
				"├─ baz-0-worker-0",
				"└─ baz-0-worker-1",
			},
		},
		{
			name:      "single pod per group",
			domain:    "rack",
			labelKey:  "topology.io/rack",
			pcsName:   "solo",
			namespace: "default",
			groups: []topologyGroup{
				{Value: "rack-0", Pods: []string{"solo-0-worker-0"}},
			},
			unscheduled: nil,
			wantLines: []string{
				"Topology: rack (topology.io/rack)",
				"PodCliqueSet: solo (namespace: default)",
				"",
				"┌ rack: rack-0",
				"└─ solo-0-worker-0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printTopologyTree(&buf, tt.domain, tt.labelKey, tt.pcsName, tt.namespace, tt.groups, tt.unscheduled)

			got := buf.String()
			gotLines := strings.Split(strings.TrimRight(got, "\n"), "\n")

			if len(gotLines) != len(tt.wantLines) {
				t.Errorf("got %d lines, want %d lines\ngot:\n%s", len(gotLines), len(tt.wantLines), got)
				return
			}

			for i, wantLine := range tt.wantLines {
				if gotLines[i] != wantLine {
					t.Errorf("line %d:\n  got:  %q\n  want: %q", i, gotLines[i], wantLine)
				}
			}
		})
	}
}
