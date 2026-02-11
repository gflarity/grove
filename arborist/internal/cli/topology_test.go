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

	"github.com/ai-dynamo/grove/arborist/internal/k8s"
)

func TestGroupPodsByTopology(t *testing.T) {
	tests := []struct {
		name           string
		displayPods    []k8s.TopologyCLIPod
		nodeLabels     map[string]map[string]string
		labelKey       string
		nodeGPUProducts map[string]string
		wantGroupCount int
		wantGroups     map[string][]string // value -> pod names
	}{
		{
			name: "pods across two racks",
			displayPods: []k8s.TopologyCLIPod{
				{Name: "foo-0-worker-0", NodeName: "node-1", Labels: map[string]string{}},
				{Name: "foo-0-worker-1", NodeName: "node-2", Labels: map[string]string{}},
				{Name: "foo-0-worker-2", NodeName: "node-3", Labels: map[string]string{}},
			},
			nodeLabels: map[string]map[string]string{
				"node-1": {"topology.io/rack": "rack-0"},
				"node-2": {"topology.io/rack": "rack-0"},
				"node-3": {"topology.io/rack": "rack-1"},
			},
			labelKey:        "topology.io/rack",
			nodeGPUProducts: map[string]string{},
			wantGroupCount:  2,
			wantGroups: map[string][]string{
				"rack-0": {"foo-0-worker-0", "foo-0-worker-1"},
				"rack-1": {"foo-0-worker-2"},
			},
		},
		{
			name: "unscheduled pods without GPU are omitted",
			displayPods: []k8s.TopologyCLIPod{
				{Name: "foo-0-worker-0", NodeName: "node-1", Labels: map[string]string{}},
				{Name: "foo-0-worker-1", NodeName: "", Labels: map[string]string{}},
			},
			nodeLabels: map[string]map[string]string{
				"node-1": {"topology.io/rack": "rack-0"},
			},
			labelKey:        "topology.io/rack",
			nodeGPUProducts: map[string]string{},
			wantGroupCount:  1,
			wantGroups: map[string][]string{
				"rack-0": {"foo-0-worker-0"},
			},
		},
		{
			name: "all pods unscheduled yields no groups",
			displayPods: []k8s.TopologyCLIPod{
				{Name: "foo-0-worker-0", NodeName: "", Labels: map[string]string{}},
				{Name: "foo-0-worker-1", NodeName: "", Labels: map[string]string{}},
			},
			nodeLabels:      map[string]map[string]string{},
			labelKey:        "topology.io/rack",
			nodeGPUProducts: map[string]string{},
			wantGroupCount:  0,
			wantGroups:      map[string][]string{},
		},
		{
			name: "node missing topology label omits pod",
			displayPods: []k8s.TopologyCLIPod{
				{Name: "foo-0-worker-0", NodeName: "node-1", Labels: map[string]string{}},
			},
			nodeLabels: map[string]map[string]string{
				"node-1": {}, // no rack label
			},
			labelKey:        "topology.io/rack",
			nodeGPUProducts: map[string]string{},
			wantGroupCount:  0,
			wantGroups:      map[string][]string{},
		},
		{
			name:            "no pods",
			displayPods:     []k8s.TopologyCLIPod{},
			nodeLabels:      map[string]map[string]string{},
			labelKey:        "topology.io/rack",
			nodeGPUProducts: map[string]string{},
			wantGroupCount:  0,
			wantGroups:      map[string][]string{},
		},
		{
			name: "empty domain values shown from node labels",
			displayPods: []k8s.TopologyCLIPod{
				{Name: "foo-0-worker-0", NodeName: "node-1", Labels: map[string]string{}},
			},
			nodeLabels: map[string]map[string]string{
				"node-1": {"topology.io/rack": "rack-0"},
				"node-2": {"topology.io/rack": "rack-1"}, // no pods here
			},
			labelKey:        "topology.io/rack",
			nodeGPUProducts: map[string]string{},
			wantGroupCount:  2,
			wantGroups: map[string][]string{
				"rack-0": {"foo-0-worker-0"},
				"rack-1": nil, // empty group
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups, _ := groupPodsByTopology(tt.displayPods, tt.nodeLabels, tt.labelKey, tt.nodeGPUProducts)

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
					if pod.Name != wantPods[i] {
						t.Errorf("group %q pod[%d]: got %q, want %q", group.Value, i, pod.Name, wantPods[i])
					}
				}
			}

			// Verify all expected groups are present
			for value := range tt.wantGroups {
				found := false
				for _, group := range groups {
					if group.Value == value {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected group %q not found", value)
				}
			}
		})
	}
}

func TestGroupPodsByTopology_SortOrder(t *testing.T) {
	displayPods := []k8s.TopologyCLIPod{
		{Name: "pod-c", NodeName: "node-2", Labels: map[string]string{}},
		{Name: "pod-a", NodeName: "node-1", Labels: map[string]string{}},
		{Name: "pod-b", NodeName: "node-1", Labels: map[string]string{}},
	}
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-1"},
		"node-2": {"topology.io/rack": "rack-0"},
	}

	groups, _ := groupPodsByTopology(displayPods, nodeLabels, "topology.io/rack", map[string]string{})

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
	if groups[1].Pods[0].Name != "pod-a" || groups[1].Pods[1].Name != "pod-b" {
		t.Errorf("pods in rack-1 should be [pod-a, pod-b], got %v", groups[1].Pods)
	}
}

func TestPrintTopologyTree(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		labelKey    string
		pcsDisplay  string
		namespace   string
		groups      []topologyGroup
		unscheduled []topologyPod
		wantLines   []string
	}{
		{
			name:       "single PCS with two groups",
			domain:     "rack",
			labelKey:   "topology.io/rack",
			pcsDisplay: "foo",
			namespace:  "default",
			groups: []topologyGroup{
				{Value: "rack-0", Pods: []topologyPod{{Name: "foo-0-worker-0"}, {Name: "foo-0-worker-1"}}},
				{Value: "rack-1", Pods: []topologyPod{{Name: "foo-0-worker-2"}}},
			},
			wantLines: []string{
				"Topology: rack (topology.io/rack)",
				"Namespace: default",
				"PodCliqueSets: foo",
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
			name:       "all PCS with mixed pods",
			domain:     "rack",
			labelKey:   "topology.io/rack",
			pcsDisplay: "all",
			namespace:  "default",
			groups: []topologyGroup{
				{Value: "rack-0", Pods: []topologyPod{{Name: "bar-0-worker-0"}, {Name: "foo-0-worker-0"}}},
				{Value: "rack-1", Pods: []topologyPod{{Name: "foo-0-worker-1"}}},
			},
			wantLines: []string{
				"Topology: rack (topology.io/rack)",
				"Namespace: default",
				"PodCliqueSets: all",
				"",
				"┌ rack: rack-0",
				"├─ bar-0-worker-0",
				"└─ foo-0-worker-0",
				"",
				"┌ rack: rack-1",
				"└─ foo-0-worker-1",
			},
		},
		{
			name:       "no groups yields header only",
			domain:     "zone",
			labelKey:   "topology.kubernetes.io/zone",
			pcsDisplay: "baz",
			namespace:  "default",
			groups:     []topologyGroup{},
			wantLines: []string{
				"Topology: zone (topology.kubernetes.io/zone)",
				"Namespace: default",
				"PodCliqueSets: baz",
			},
		},
		{
			name:       "single pod per group",
			domain:     "rack",
			labelKey:   "topology.io/rack",
			pcsDisplay: "solo",
			namespace:  "default",
			groups: []topologyGroup{
				{Value: "rack-0", Pods: []topologyPod{{Name: "solo-0-worker-0"}}},
			},
			wantLines: []string{
				"Topology: rack (topology.io/rack)",
				"Namespace: default",
				"PodCliqueSets: solo",
				"",
				"┌ rack: rack-0",
				"└─ solo-0-worker-0",
			},
		},
		{
			name:       "different namespace",
			domain:     "rack",
			labelKey:   "topology.io/rack",
			pcsDisplay: "bar",
			namespace:  "prod",
			groups: []topologyGroup{
				{Value: "rack-0", Pods: []topologyPod{{Name: "bar-0-worker-0"}}},
			},
			wantLines: []string{
				"Topology: rack (topology.io/rack)",
				"Namespace: prod",
				"PodCliqueSets: bar",
				"",
				"┌ rack: rack-0",
				"└─ bar-0-worker-0",
			},
		},
		{
			name:       "empty domain value shown",
			domain:     "block",
			labelKey:   "kubernetes.io/block",
			pcsDisplay: "all",
			namespace:  "default",
			groups: []topologyGroup{
				{Value: "block-0", Pods: []topologyPod{{Name: "foo-0-worker-0"}}},
				{Value: "block-1", Pods: nil},
			},
			wantLines: []string{
				"Topology: block (kubernetes.io/block)",
				"Namespace: default",
				"PodCliqueSets: all",
				"",
				"┌ block: block-0",
				"└─ foo-0-worker-0",
				"",
				"─ block: block-1",
			},
		},
		{
			name:       "all namespaces",
			domain:     "rack",
			labelKey:   "topology.io/rack",
			pcsDisplay: "all",
			namespace:  "all",
			groups: []topologyGroup{
				{Value: "rack-0", Pods: []topologyPod{{Name: "bar-0-worker-0"}, {Name: "foo-0-worker-0"}}},
			},
			wantLines: []string{
				"Topology: rack (topology.io/rack)",
				"Namespace: all",
				"PodCliqueSets: all",
				"",
				"┌ rack: rack-0",
				"├─ bar-0-worker-0",
				"└─ foo-0-worker-0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printTopologyTree(&buf, tt.domain, tt.labelKey, tt.pcsDisplay, tt.namespace, tt.groups, tt.unscheduled)

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

// =====================
// GPU-specific tests
// =====================

func TestGroupPodsByTopology_GPUInfo(t *testing.T) {
	displayPods := []k8s.TopologyCLIPod{
		{Name: "gpu-pod-1", NodeName: "node-1", Labels: map[string]string{}, GPURequests: 2},
		{Name: "gpu-pod-pending", NodeName: "", Labels: map[string]string{}, GPURequests: 4},
		{Name: "non-gpu-pod", NodeName: "node-1", Labels: map[string]string{}, GPURequests: 0},
	}
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-0"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
	}

	groups, unscheduled := groupPodsByTopology(displayPods, nodeLabels, "topology.io/rack", nodeGPUProducts)

	// Scheduled pods in rack-0
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Pods) != 2 {
		t.Fatalf("expected 2 pods in group, got %d", len(groups[0].Pods))
	}

	// gpu-pod-1: scheduled on node-1 with H200
	gpuPod := groups[0].Pods[0]
	if gpuPod.Name != "gpu-pod-1" {
		t.Errorf("expected gpu-pod-1, got %q", gpuPod.Name)
	}
	if gpuPod.GPUType != "H200" {
		t.Errorf("expected GPUType=H200, got %q", gpuPod.GPUType)
	}
	if gpuPod.GPUCount != 2 {
		t.Errorf("expected GPUCount=2, got %d", gpuPod.GPUCount)
	}

	// non-gpu-pod: no GPU annotation
	nonGPUPod := groups[0].Pods[1]
	if nonGPUPod.Name != "non-gpu-pod" {
		t.Errorf("expected non-gpu-pod, got %q", nonGPUPod.Name)
	}
	if nonGPUPod.GPUType != "" {
		t.Errorf("expected GPUType empty, got %q", nonGPUPod.GPUType)
	}
	if nonGPUPod.GPUCount != 0 {
		t.Errorf("expected GPUCount=0, got %d", nonGPUPod.GPUCount)
	}

	// Unscheduled GPU pod
	if len(unscheduled) != 1 {
		t.Fatalf("expected 1 unscheduled pod, got %d", len(unscheduled))
	}
	if unscheduled[0].Name != "gpu-pod-pending" {
		t.Errorf("expected gpu-pod-pending, got %q", unscheduled[0].Name)
	}
	if unscheduled[0].GPUType != "?" {
		t.Errorf("expected GPUType=?, got %q", unscheduled[0].GPUType)
	}
	if unscheduled[0].GPUCount != 4 {
		t.Errorf("expected GPUCount=4, got %d", unscheduled[0].GPUCount)
	}
}

func TestComputeThreeWayGPUUsage(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-0"},
		"node-2": {"topology.io/rack": "rack-0"},
		"node-3": {"topology.io/rack": "rack-1"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
		"node-3": "B200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
		"node-3": 4,
	}

	allPods := []k8s.TopologyCLIPod{
		// "this" pods (PCS=my-pcs)
		{Name: "my-pcs-0", NodeName: "node-1", Labels: map[string]string{"app.kubernetes.io/part-of": "my-pcs"}, GPURequests: 2},
		{Name: "my-pcs-1", NodeName: "node-2", Labels: map[string]string{"app.kubernetes.io/part-of": "my-pcs"}, GPURequests: 4},
		// "other" pods (different PCS)
		{Name: "other-pcs-0", NodeName: "node-1", Labels: map[string]string{"app.kubernetes.io/part-of": "other-pcs"}, GPURequests: 3},
		// "other" pod in rack-1
		{Name: "other-pcs-1", NodeName: "node-3", Labels: map[string]string{"app.kubernetes.io/part-of": "other-pcs"}, GPURequests: 1},
		// pending pod (should be excluded from both this and other)
		{Name: "pending-pod", NodeName: "", Labels: map[string]string{"app.kubernetes.io/part-of": "my-pcs"}, GPURequests: 2},
		// non-GPU pod
		{Name: "no-gpu", NodeName: "node-1", Labels: map[string]string{"app.kubernetes.io/part-of": "my-pcs"}, GPURequests: 0},
	}

	isDisplayPod := func(pod *k8s.TopologyCLIPod) bool {
		return pod.Labels["app.kubernetes.io/part-of"] == "my-pcs"
	}

	result := computeThreeWayGPUUsage("topology.io/rack", nodeLabels, nodeGPUProducts, nodeGPUCapacity, allPods, isDisplayPod)

	// rack-0: H200 nodes with 16 total
	rack0 := result["rack-0"]
	if len(rack0) != 1 {
		t.Fatalf("rack-0: expected 1 GPU type, got %d", len(rack0))
	}
	if rack0[0].Type != "H200" {
		t.Errorf("rack-0: expected type H200, got %q", rack0[0].Type)
	}
	if rack0[0].Total != 16 {
		t.Errorf("rack-0: expected total=16, got %d", rack0[0].Total)
	}
	if rack0[0].ThisPCS != 6 { // 2 + 4
		t.Errorf("rack-0: expected this=6, got %d", rack0[0].ThisPCS)
	}
	if rack0[0].Other != 3 {
		t.Errorf("rack-0: expected other=3, got %d", rack0[0].Other)
	}
	if rack0[0].Free != 7 { // 16 - 6 - 3
		t.Errorf("rack-0: expected free=7, got %d", rack0[0].Free)
	}

	// rack-1: B200 nodes with 4 total
	rack1 := result["rack-1"]
	if len(rack1) != 1 {
		t.Fatalf("rack-1: expected 1 GPU type, got %d", len(rack1))
	}
	if rack1[0].Type != "B200" {
		t.Errorf("rack-1: expected type B200, got %q", rack1[0].Type)
	}
	if rack1[0].Total != 4 {
		t.Errorf("rack-1: expected total=4, got %d", rack1[0].Total)
	}
	if rack1[0].ThisPCS != 0 {
		t.Errorf("rack-1: expected this=0, got %d", rack1[0].ThisPCS)
	}
	if rack1[0].Other != 1 {
		t.Errorf("rack-1: expected other=1, got %d", rack1[0].Other)
	}
	if rack1[0].Free != 3 { // 4 - 0 - 1
		t.Errorf("rack-1: expected free=3, got %d", rack1[0].Free)
	}
}

func TestComputeThreeWayGPUUsage_MixedTypes(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/block": "block-0"},
		"node-2": {"topology.io/block": "block-0"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "B200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 4,
		"node-2": 4,
	}

	allPods := []k8s.TopologyCLIPod{
		{Name: "pod-1", NodeName: "node-1", Labels: map[string]string{"app.kubernetes.io/part-of": "pcs"}, GPURequests: 3},
		{Name: "pod-2", NodeName: "node-2", Labels: map[string]string{"app.kubernetes.io/part-of": "pcs"}, GPURequests: 2},
	}

	isDisplayPod := func(pod *k8s.TopologyCLIPod) bool {
		return pod.Labels["app.kubernetes.io/part-of"] == "pcs"
	}

	result := computeThreeWayGPUUsage("topology.io/block", nodeLabels, nodeGPUProducts, nodeGPUCapacity, allPods, isDisplayPod)

	block0 := result["block-0"]
	if len(block0) != 2 {
		t.Fatalf("block-0: expected 2 GPU types, got %d", len(block0))
	}

	// Types should be sorted: B200, H200
	if block0[0].Type != "B200" {
		t.Errorf("expected first type B200, got %q", block0[0].Type)
	}
	if block0[0].ThisPCS != 2 || block0[0].Total != 4 || block0[0].Free != 2 {
		t.Errorf("B200: this=%d, total=%d, free=%d", block0[0].ThisPCS, block0[0].Total, block0[0].Free)
	}

	if block0[1].Type != "H200" {
		t.Errorf("expected second type H200, got %q", block0[1].Type)
	}
	if block0[1].ThisPCS != 3 || block0[1].Total != 4 || block0[1].Free != 1 {
		t.Errorf("H200: this=%d, total=%d, free=%d", block0[1].ThisPCS, block0[1].Total, block0[1].Free)
	}
}

func TestComputeThreeWayGPUUsage_NoGPUNodes(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-0"},
	}
	// No GPU products or capacity
	nodeGPUProducts := map[string]string{}
	nodeGPUCapacity := map[string]int64{}

	allPods := []k8s.TopologyCLIPod{
		{Name: "pod-1", NodeName: "node-1", Labels: map[string]string{"app.kubernetes.io/part-of": "pcs"}, GPURequests: 0},
	}

	isDisplayPod := func(pod *k8s.TopologyCLIPod) bool { return true }

	result := computeThreeWayGPUUsage("topology.io/rack", nodeLabels, nodeGPUProducts, nodeGPUCapacity, allPods, isDisplayPod)

	if len(result) != 0 {
		t.Errorf("expected no GPU entries for non-GPU nodes, got %d", len(result))
	}
}

func TestPrintGPUMiniTable(t *testing.T) {
	tests := []struct {
		name    string
		entries []gpuUsageEntry
		want    string
	}{
		{
			name:    "empty entries produces no output",
			entries: nil,
			want:    "",
		},
		{
			name: "single type",
			entries: []gpuUsageEntry{
				{Type: "H200", ThisPCS: 2, Total: 8, Other: 1, Free: 5},
			},
		want: "│        total  grove  other  free\n" +
			"│  H200      8      2      1     5\n",
	},
	{
		name: "multiple types",
		entries: []gpuUsageEntry{
			{Type: "B200", ThisPCS: 7, Total: 56, Other: 3, Free: 46},
			{Type: "H200", ThisPCS: 7, Total: 56, Other: 3, Free: 46},
		},
		want: "│        total  grove  other  free\n" +
			"│  B200     56      7      3    46\n" +
			"│  H200     56      7      3    46\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printGPUMiniTable(&buf, tt.entries)
			got := buf.String()
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestPrintTopologyTree_WithGPU(t *testing.T) {
	groups := []topologyGroup{
		{
			Value: "block-0",
			Pods: []topologyPod{
				{Name: "pcs-0-router-6whz7", GPUType: "H200", GPUCount: 1},
				{Name: "pcs-0-workers-0-worker-8szft", GPUType: "H200", GPUCount: 1},
				{Name: "pcs-1-coordinator-abc12", GPUType: "", GPUCount: 0}, // non-GPU pod
			},
			GPUUsage: []gpuUsageEntry{
				{Type: "H200", ThisPCS: 2, Total: 8, Other: 1, Free: 5},
			},
		},
	}

	var buf bytes.Buffer
	printTopologyTree(&buf, "block", "kubernetes.io/block", "my-pcs", "default", groups, nil)

	got := buf.String()
	gotLines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	wantLines := []string{
		"Topology: block (kubernetes.io/block)",
		"Namespace: default",
		"PodCliqueSets: my-pcs",
		"",
		"┌ block: block-0",
		"│        total  grove  other  free",
		"│  H200      8      2      1     5",
		"├─ pcs-0-router-6whz7              [H200: 1]",
		"├─ pcs-0-workers-0-worker-8szft    [H200: 1]",
		"└─ pcs-1-coordinator-abc12",
	}

	if len(gotLines) != len(wantLines) {
		t.Errorf("got %d lines, want %d lines\ngot:\n%s", len(gotLines), len(wantLines), got)
		return
	}

	for i, wantLine := range wantLines {
		if gotLines[i] != wantLine {
			t.Errorf("line %d:\n  got:  %q\n  want: %q", i, gotLines[i], wantLine)
		}
	}
}

func TestPrintTopologyTree_UnscheduledGPUPods(t *testing.T) {
	unscheduled := []topologyPod{
		{Name: "pcs-5-workers-0-worker-pending", GPUType: "?", GPUCount: 2},
	}

	var buf bytes.Buffer
	printTopologyTree(&buf, "block", "kubernetes.io/block", "my-pcs", "default", nil, unscheduled)

	got := buf.String()

	if !strings.Contains(got, "┌ <unscheduled>") {
		t.Errorf("expected <unscheduled> header, got:\n%s", got)
	}
	if !strings.Contains(got, "[?: 2]") {
		t.Errorf("expected [?: 2] annotation, got:\n%s", got)
	}
}

func TestPrintTopologyTree_NoUnscheduledWhenEmpty(t *testing.T) {
	groups := []topologyGroup{
		{Value: "rack-0", Pods: []topologyPod{{Name: "pod-1"}}},
	}

	var buf bytes.Buffer
	printTopologyTree(&buf, "rack", "topology.io/rack", "my-pcs", "default", groups, nil)

	got := buf.String()
	if strings.Contains(got, "unscheduled") {
		t.Errorf("should not show unscheduled section when empty, got:\n%s", got)
	}
}

func TestFilterDisplayPods(t *testing.T) {
	allPods := []k8s.TopologyCLIPod{
		{Name: "my-pcs-pod-1", Labels: map[string]string{"app.kubernetes.io/part-of": "my-pcs"}},
		{Name: "my-pcs-pod-2", Labels: map[string]string{"app.kubernetes.io/part-of": "my-pcs"}},
		{Name: "other-pcs-pod", Labels: map[string]string{"app.kubernetes.io/part-of": "other-pcs"}},
		{Name: "no-pcs-pod", Labels: map[string]string{}},
	}

	t.Run("specific PCS filter", func(t *testing.T) {
		displayPods, pcsDisplay, isDisplayPod := filterDisplayPods(allPods, "my-pcs")

		if pcsDisplay != "my-pcs" {
			t.Errorf("expected pcsDisplay=my-pcs, got %q", pcsDisplay)
		}
		if len(displayPods) != 2 {
			t.Errorf("expected 2 display pods, got %d", len(displayPods))
		}

		// isDisplayPod should return true for my-pcs pods
		myPod := &k8s.TopologyCLIPod{Labels: map[string]string{"app.kubernetes.io/part-of": "my-pcs"}}
		otherPod := &k8s.TopologyCLIPod{Labels: map[string]string{"app.kubernetes.io/part-of": "other-pcs"}}
		noPod := &k8s.TopologyCLIPod{Labels: map[string]string{}}

		if !isDisplayPod(myPod) {
			t.Error("isDisplayPod should return true for my-pcs pod")
		}
		if isDisplayPod(otherPod) {
			t.Error("isDisplayPod should return false for other-pcs pod")
		}
		if isDisplayPod(noPod) {
			t.Error("isDisplayPod should return false for non-PCS pod")
		}
	})

	t.Run("no PCS filter (all PCS)", func(t *testing.T) {
		displayPods, pcsDisplay, isDisplayPod := filterDisplayPods(allPods, "")

		if pcsDisplay != "all" {
			t.Errorf("expected pcsDisplay=all, got %q", pcsDisplay)
		}
		if len(displayPods) != 3 { // my-pcs-pod-1, my-pcs-pod-2, other-pcs-pod
			t.Errorf("expected 3 display pods, got %d", len(displayPods))
		}

		// isDisplayPod should return true for any PCS-managed pod
		noPod := &k8s.TopologyCLIPod{Labels: map[string]string{}}
		if isDisplayPod(noPod) {
			t.Error("isDisplayPod should return false for non-PCS pod")
		}

		pcsPod := &k8s.TopologyCLIPod{Labels: map[string]string{"app.kubernetes.io/part-of": "any-pcs"}}
		if !isDisplayPod(pcsPod) {
			t.Error("isDisplayPod should return true for any PCS-managed pod")
		}
	})
}

func TestComputeThreeWayGPUUsage_FreeClamped(t *testing.T) {
	// Test overcommit scenario: more GPU requests than capacity
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-0"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 2, // only 2 GPUs
	}

	allPods := []k8s.TopologyCLIPod{
		{Name: "pod-1", NodeName: "node-1", Labels: map[string]string{"app.kubernetes.io/part-of": "pcs"}, GPURequests: 3}, // overcommit
	}

	isDisplayPod := func(pod *k8s.TopologyCLIPod) bool { return true }

	result := computeThreeWayGPUUsage("topology.io/rack", nodeLabels, nodeGPUProducts, nodeGPUCapacity, allPods, isDisplayPod)

	rack0 := result["rack-0"]
	if len(rack0) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rack0))
	}
	if rack0[0].Free != 0 {
		t.Errorf("free should be clamped to 0, got %d", rack0[0].Free)
	}
}
