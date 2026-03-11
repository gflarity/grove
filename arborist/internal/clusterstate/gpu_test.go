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
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseGPUProductShortName(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  string
	}{
		{name: "H200 full label", label: "NVIDIA-H200-141GB-HBM3e", want: "H200"},
		{name: "B200 full label", label: "NVIDIA-B200-192GB-HBM3e", want: "B200"},
		{name: "H100 full label", label: "NVIDIA-H100-80GB-HBM3", want: "H100"},
		{name: "A100 full label", label: "NVIDIA-A100-40GB-HBM2e", want: "A100"},
		{name: "two segments only", label: "NVIDIA-H200", want: "H200"},
		{name: "empty label", label: "", want: ""},
		{name: "single segment", label: "NVIDIA", want: ""},
		{name: "non-NVIDIA format", label: "AMD-MI300X-192GB", want: "MI300X"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseGPUProductShortName(tt.label)
			if got != tt.want {
				t.Errorf("ParseGPUProductShortName(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

func TestBuildGPUSummary_AllSameGPUType(t *testing.T) {
	nodeGPUProduct := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
	}
	pods := []TopologyPodInput{
		{
			Name: "pod-a", NodeName: "node-1", GPURequests: 2,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podclique":                  "my-pcs-0-worker",
			},
		},
		{
			Name: "pod-b", NodeName: "node-2", GPURequests: 2,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podclique":                  "my-pcs-0-worker",
			},
		},
	}

	summary := BuildGPUSummary(pods, nodeGPUProduct)

	if len(summary.GPUTypes) != 1 || summary.GPUTypes[0] != "H200" {
		t.Fatalf("GPUTypes = %v, want [H200]", summary.GPUTypes)
	}
	if summary.ByPCS["my-pcs"]["H200"] != 4 {
		t.Errorf("ByPCS[my-pcs][H200] = %d, want 4", summary.ByPCS["my-pcs"]["H200"])
	}
	if summary.ByReplica["my-pcs/0"]["H200"] != 4 {
		t.Errorf("ByReplica[my-pcs/0][H200] = %d, want 4", summary.ByReplica["my-pcs/0"]["H200"])
	}
	if summary.ByPodClique["my-pcs-0-worker"]["H200"] != 4 {
		t.Errorf("ByPodClique[my-pcs-0-worker][H200] = %d, want 4", summary.ByPodClique["my-pcs-0-worker"]["H200"])
	}
	if summary.ByPod["pod-a"]["H200"] != 2 {
		t.Errorf("ByPod[pod-a][H200] = %d, want 2", summary.ByPod["pod-a"]["H200"])
	}
}

func TestBuildGPUSummary_MixedGPUTypes(t *testing.T) {
	nodeGPUProduct := map[string]string{
		"node-1": "H200",
		"node-2": "B200",
	}
	pods := []TopologyPodInput{
		{
			Name: "pod-a", NodeName: "node-1", GPURequests: 4,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podclique":                  "my-pcs-0-compute",
			},
		},
		{
			Name: "pod-b", NodeName: "node-2", GPURequests: 4,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podclique":                  "my-pcs-0-accel",
			},
		},
	}

	summary := BuildGPUSummary(pods, nodeGPUProduct)

	if len(summary.GPUTypes) != 2 {
		t.Fatalf("GPUTypes = %v, want [B200 H200]", summary.GPUTypes)
	}
	if summary.GPUTypes[0] != "B200" || summary.GPUTypes[1] != "H200" {
		t.Errorf("GPUTypes = %v, want [B200 H200]", summary.GPUTypes)
	}
	if summary.ByPCS["my-pcs"]["H200"] != 4 {
		t.Errorf("ByPCS[my-pcs][H200] = %d, want 4", summary.ByPCS["my-pcs"]["H200"])
	}
	if summary.ByPCS["my-pcs"]["B200"] != 4 {
		t.Errorf("ByPCS[my-pcs][B200] = %d, want 4", summary.ByPCS["my-pcs"]["B200"])
	}
}

func TestBuildGPUSummary_NoGPURequests(t *testing.T) {
	nodeGPUProduct := map[string]string{
		"node-1": "H200",
	}
	pods := []TopologyPodInput{
		{
			Name: "pod-a", NodeName: "node-1", GPURequests: 0,
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
			},
		},
	}

	summary := BuildGPUSummary(pods, nodeGPUProduct)

	// GPU types still discovered from nodes
	if len(summary.GPUTypes) != 1 || summary.GPUTypes[0] != "H200" {
		t.Fatalf("GPUTypes = %v, want [H200]", summary.GPUTypes)
	}
	// But no counts aggregated
	if len(summary.ByPCS) != 0 {
		t.Errorf("ByPCS should be empty, got %v", summary.ByPCS)
	}
}

func TestBuildGPUSummary_PendingPods(t *testing.T) {
	nodeGPUProduct := map[string]string{
		"node-1": "H200",
	}
	pods := []TopologyPodInput{
		{
			Name: "pending-pod", NodeName: "", GPURequests: 4,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podclique":                  "my-pcs-0-worker",
			},
		},
	}

	summary := BuildGPUSummary(pods, nodeGPUProduct)

	// Pending pod should be tracked
	if summary.PendingGPUPods["pending-pod"] != 4 {
		t.Errorf("PendingGPUPods[pending-pod] = %d, want 4", summary.PendingGPUPods["pending-pod"])
	}
	// Should not be attributed to any GPU type
	if len(summary.ByPod) != 0 {
		t.Errorf("ByPod should be empty for pending pods, got %v", summary.ByPod)
	}
}

func TestBuildGPUSummary_NoGPUNodes(t *testing.T) {
	nodeGPUProduct := map[string]string{} // no GPU nodes
	pods := []TopologyPodInput{
		{
			Name: "pod-a", NodeName: "node-1", GPURequests: 2,
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
			},
		},
	}

	summary := BuildGPUSummary(pods, nodeGPUProduct)

	// No GPU types discovered
	if len(summary.GPUTypes) != 0 {
		t.Errorf("GPUTypes should be empty, got %v", summary.GPUTypes)
	}
}

func TestFormatGPUGroveOtherTotal(t *testing.T) {
	tests := []struct {
		grove int64
		other int64
		total int64
		want  string
	}{
		{0, 0, 0, "0/0/0"},
		{16, 0, 32, "16/0/32"},
		{16, 16, 32, "16/16/32"},
		{0, 0, 8, "0/0/8"},
		{4, 2, 16, "4/2/16"},
	}
	for _, tt := range tests {
		got := FormatGPUGroveOtherTotal(tt.grove, tt.other, tt.total)
		if got != tt.want {
			t.Errorf("FormatGPUGroveOtherTotal(%d, %d, %d) = %q, want %q", tt.grove, tt.other, tt.total, got, tt.want)
		}
	}
}

func TestComputeDomainGPUSummary_SingleGPUType(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/block": "block-01"},
		"node-2": {"topology.io/block": "block-01"},
		"node-3": {"topology.io/block": "block-02"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
		"node-3": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
		"node-3": 8,
	}
	pods := []TopologyPodInput{
		{Name: "pod-a", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{}},
		{Name: "pod-b", NodeName: "node-2", GPURequests: 2, Labels: map[string]string{}},
		{Name: "pod-c", NodeName: "node-3", GPURequests: 8, Labels: map[string]string{}},
	}
	matchingNodes := []string{"node-1", "node-2", "node-3"}

	summary := ComputeDomainGPUSummary(DomainGPUInput{
		DomainKey:       "topology.io/block",
		MatchingNodes:   matchingNodes,
		NodeLabels:      nodeLabels,
		NodeGPUProducts: nodeGPUProducts,
		NodeGPUCapacity: nodeGPUCapacity,
		Pods:            pods,
	})

	if len(summary.GPUTypes) != 1 || summary.GPUTypes[0] != "H200" {
		t.Fatalf("GPUTypes = %v, want [H200]", summary.GPUTypes)
	}

	// block-01: 2 nodes with 8 GPUs each = 16 total, pods use 4+2=6 (all Other, no part-of label)
	b01 := summary.ByValue["block-01"]["H200"]
	if b01.Total != 16 {
		t.Errorf("block-01 H200 Total = %d, want 16", b01.Total)
	}
	if b01.Other != 6 {
		t.Errorf("block-01 H200 Other = %d, want 6", b01.Other)
	}
	if b01.Grove != 0 {
		t.Errorf("block-01 H200 Grove = %d, want 0", b01.Grove)
	}

	// block-02: 1 node with 8 GPUs, pod uses 8 (Other)
	b02 := summary.ByValue["block-02"]["H200"]
	if b02.Total != 8 {
		t.Errorf("block-02 H200 Total = %d, want 8", b02.Total)
	}
	if b02.Other != 8 {
		t.Errorf("block-02 H200 Other = %d, want 8", b02.Other)
	}
	if b02.Grove != 0 {
		t.Errorf("block-02 H200 Grove = %d, want 0", b02.Grove)
	}
}

func TestComputeDomainGPUSummary_MixedGPUTypes(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/block": "block-01"},
		"node-2": {"topology.io/block": "block-02"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "B200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
	}
	pods := []TopologyPodInput{
		{Name: "pod-a", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{}},
		{Name: "pod-b", NodeName: "node-2", GPURequests: 2, Labels: map[string]string{}},
	}
	matchingNodes := []string{"node-1", "node-2"}

	summary := ComputeDomainGPUSummary(DomainGPUInput{
		DomainKey:       "topology.io/block",
		MatchingNodes:   matchingNodes,
		NodeLabels:      nodeLabels,
		NodeGPUProducts: nodeGPUProducts,
		NodeGPUCapacity: nodeGPUCapacity,
		Pods:            pods,
	})

	if len(summary.GPUTypes) != 2 || summary.GPUTypes[0] != "B200" || summary.GPUTypes[1] != "H200" {
		t.Fatalf("GPUTypes = %v, want [B200 H200]", summary.GPUTypes)
	}

	// block-01 has H200 only (pod-a has no part-of label → Other)
	b01H200 := summary.ByValue["block-01"]["H200"]
	if b01H200.Total != 8 || b01H200.Other != 4 || b01H200.Grove != 0 {
		t.Errorf("block-01 H200 = Grove=%d/Other=%d/Total=%d, want 0/4/8", b01H200.Grove, b01H200.Other, b01H200.Total)
	}
	// block-01 should have no B200
	b01B200 := summary.ByValue["block-01"]["B200"]
	if b01B200.Total != 0 || b01B200.Other != 0 || b01B200.Grove != 0 {
		t.Errorf("block-01 B200 = Grove=%d/Other=%d/Total=%d, want 0/0/0", b01B200.Grove, b01B200.Other, b01B200.Total)
	}

	// block-02 has B200 only (pod-b has no part-of label → Other)
	b02B200 := summary.ByValue["block-02"]["B200"]
	if b02B200.Total != 8 || b02B200.Other != 2 || b02B200.Grove != 0 {
		t.Errorf("block-02 B200 = Grove=%d/Other=%d/Total=%d, want 0/2/8", b02B200.Grove, b02B200.Other, b02B200.Total)
	}
}

func TestComputeDomainGPUSummary_NoGPUNodes(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/block": "block-01"},
	}
	nodeGPUProducts := map[string]string{}
	nodeGPUCapacity := map[string]int64{}
	pods := []TopologyPodInput{
		{Name: "pod-a", NodeName: "node-1", GPURequests: 2, Labels: map[string]string{}},
	}
	matchingNodes := []string{"node-1"}

	summary := ComputeDomainGPUSummary(DomainGPUInput{
		DomainKey:       "topology.io/block",
		MatchingNodes:   matchingNodes,
		NodeLabels:      nodeLabels,
		NodeGPUProducts: nodeGPUProducts,
		NodeGPUCapacity: nodeGPUCapacity,
		Pods:            pods,
	})

	if len(summary.GPUTypes) != 0 {
		t.Errorf("expected no GPU types, got %v", summary.GPUTypes)
	}
	if len(summary.ByValue) != 0 {
		t.Errorf("expected empty ByValue, got %v", summary.ByValue)
	}
}

func TestComputeDomainGPUSummary_PendingPods(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/block": "block-01"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
	}
	pods := []TopologyPodInput{
		{Name: "pending-pod", NodeName: "", GPURequests: 4, Labels: map[string]string{}}, // pending
		{Name: "running-pod", NodeName: "node-1", GPURequests: 2, Labels: map[string]string{}},
	}
	matchingNodes := []string{"node-1"}

	summary := ComputeDomainGPUSummary(DomainGPUInput{
		DomainKey:       "topology.io/block",
		MatchingNodes:   matchingNodes,
		NodeLabels:      nodeLabels,
		NodeGPUProducts: nodeGPUProducts,
		NodeGPUCapacity: nodeGPUCapacity,
		Pods:            pods,
	})

	b01 := summary.ByValue["block-01"]["H200"]
	// Only running-pod should be counted (pending has no node); no part-of label → Other
	if b01.Other != 2 {
		t.Errorf("block-01 H200 Other = %d, want 2 (pending pod excluded)", b01.Other)
	}
	if b01.Grove != 0 {
		t.Errorf("block-01 H200 Grove = %d, want 0", b01.Grove)
	}
	if b01.Total != 8 {
		t.Errorf("block-01 H200 Total = %d, want 8", b01.Total)
	}
}

func TestComputeDomainGPUSummary_EmptyNodes(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/block": "block-01"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
	}
	pods := []TopologyPodInput{} // no pods
	matchingNodes := []string{"node-1"}

	summary := ComputeDomainGPUSummary(DomainGPUInput{
		DomainKey:       "topology.io/block",
		MatchingNodes:   matchingNodes,
		NodeLabels:      nodeLabels,
		NodeGPUProducts: nodeGPUProducts,
		NodeGPUCapacity: nodeGPUCapacity,
		Pods:            pods,
	})

	b01 := summary.ByValue["block-01"]["H200"]
	if b01.Grove != 0 {
		t.Errorf("block-01 H200 Grove = %d, want 0", b01.Grove)
	}
	if b01.Other != 0 {
		t.Errorf("block-01 H200 Other = %d, want 0", b01.Other)
	}
	if b01.Total != 8 {
		t.Errorf("block-01 H200 Total = %d, want 8", b01.Total)
	}
}

func TestComputeDomainGPUSummary_ThreeWaySplit(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/block": "block-01"},
		"node-2": {"topology.io/block": "block-01"},
	}
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
	}
	pods := []TopologyPodInput{
		// PCS-managed pod (has app.kubernetes.io/part-of label) → Grove
		{Name: "grove-pod-1", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
		// PCS-managed pod → Grove
		{Name: "grove-pod-2", NodeName: "node-2", GPURequests: 3, Labels: map[string]string{
			"app.kubernetes.io/part-of": "other-pcs",
		}},
		// Non-PCS pod (no part-of label) → Other
		{Name: "other-pod-1", NodeName: "node-1", GPURequests: 2, Labels: map[string]string{}},
		// Non-PCS pod → Other
		{Name: "other-pod-2", NodeName: "node-2", GPURequests: 1, Labels: map[string]string{
			"some-other-label": "value",
		}},
	}
	matchingNodes := []string{"node-1", "node-2"}

	summary := ComputeDomainGPUSummary(DomainGPUInput{
		DomainKey:       "topology.io/block",
		MatchingNodes:   matchingNodes,
		NodeLabels:      nodeLabels,
		NodeGPUProducts: nodeGPUProducts,
		NodeGPUCapacity: nodeGPUCapacity,
		Pods:            pods,
	})

	b01 := summary.ByValue["block-01"]["H200"]
	// Grove: 4 + 3 = 7
	if b01.Grove != 7 {
		t.Errorf("block-01 H200 Grove = %d, want 7", b01.Grove)
	}
	// Other: 2 + 1 = 3
	if b01.Other != 3 {
		t.Errorf("block-01 H200 Other = %d, want 3", b01.Other)
	}
	// Total: 8 + 8 = 16
	if b01.Total != 16 {
		t.Errorf("block-01 H200 Total = %d, want 16", b01.Total)
	}
}

func TestFormatGPUBarOnly(t *testing.T) {
	tests := []struct {
		name     string
		grove    int64
		other    int64
		total    int64
		barWidth int
		want     string
	}{
		{
			name:     "normal grove only",
			grove:    7,
			other:    0,
			total:    56,
			barWidth: 20,
		},
		{
			name:     "zero bar width",
			grove:    7,
			other:    3,
			total:    56,
			barWidth: 0,
			want:     "[]",
		},
		{
			name:     "fully used by grove",
			grove:    56,
			other:    0,
			total:    56,
			barWidth: 10,
		},
		{
			name:     "all free",
			grove:    0,
			other:    0,
			total:    56,
			barWidth: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatGPUBarOnly(tt.grove, tt.other, tt.total, tt.barWidth)

			// Must start with [ and end with ]
			if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
				t.Errorf("FormatGPUBarOnly() = %q, want bracketed", got)
			}

			// Must NOT contain numeric suffix
			if strings.Contains(got, "(") {
				t.Errorf("FormatGPUBarOnly() = %q, should not contain numeric suffix", got)
			}

			if tt.want != "" {
				if got != tt.want {
					t.Errorf("FormatGPUBarOnly() = %q, want %q", got, tt.want)
				}
				return
			}

			// Verify bar width
			barContent := got[1 : len(got)-1]
			barRunes := utf8.RuneCountInString(barContent)
			if barRunes != tt.barWidth {
				t.Errorf("bar width = %d runes, want %d; bar=%q", barRunes, tt.barWidth, barContent)
			}
		})
	}
}

func TestFormatGPUBar(t *testing.T) {
	tests := []struct {
		name     string
		grove    int64
		other    int64
		total    int64
		barWidth int
		wantBar  string // the bar portion between [ and ]
		wantSuf  string // the numeric suffix after "] "
	}{
		{
			name:     "normal grove only",
			grove:    7,
			other:    0,
			total:    56,
			barWidth: 20,
			wantSuf:  "(7/0/56)",
		},
		{
			name:     "mixed grove and other",
			grove:    7,
			other:    3,
			total:    56,
			barWidth: 20,
			wantSuf:  "(7/3/56)",
		},
		{
			name:     "all free",
			grove:    0,
			other:    0,
			total:    56,
			barWidth: 20,
			wantSuf:  "(0/0/56)",
		},
		{
			name:     "fully used by grove",
			grove:    56,
			other:    0,
			total:    56,
			barWidth: 20,
			wantSuf:  "(56/0/56)",
		},
		{
			name:     "overcommit",
			grove:    60,
			other:    0,
			total:    56,
			barWidth: 20,
			wantSuf:  "(60/0/56)",
		},
		{
			name:     "total zero",
			grove:    0,
			other:    0,
			total:    0,
			barWidth: 20,
			wantSuf:  "(0/0/0)",
		},
		{
			name:     "small bar width 1",
			grove:    7,
			other:    3,
			total:    56,
			barWidth: 1,
			wantSuf:  "(7/3/56)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatGPUBar(tt.grove, tt.other, tt.total, tt.barWidth)

			// Verify suffix
			if !strings.HasSuffix(got, tt.wantSuf) {
				t.Errorf("FormatGPUBar(%d, %d, %d, %d) = %q, want suffix %q",
					tt.grove, tt.other, tt.total, tt.barWidth, got, tt.wantSuf)
			}

			// Verify bar is wrapped in brackets
			if !strings.HasPrefix(got, "[") {
				t.Errorf("bar should start with '[', got %q", got)
			}
			closeBracket := strings.Index(got, "]")
			if closeBracket < 0 {
				t.Fatalf("bar should contain ']', got %q", got)
			}

			// Extract bar content between brackets
			barContent := got[len("["):closeBracket]
			barRunes := utf8.RuneCountInString(barContent)
			if barRunes != tt.barWidth {
				t.Errorf("bar width = %d runes, want %d; bar=%q", barRunes, tt.barWidth, barContent)
			}

			// Count character types
			groveCount := strings.Count(barContent, "▓")
			otherCount := strings.Count(barContent, "░")
			freeCount := strings.Count(barContent, " ")
			if groveCount+otherCount+freeCount != tt.barWidth {
				t.Errorf("bar segments sum = %d, want %d; bar=%q",
					groveCount+otherCount+freeCount, tt.barWidth, barContent)
			}
		})
	}
}

func TestFormatGPUBar_SegmentWidths(t *testing.T) {
	// Verify rounding: segments must always sum to exactly barWidth
	testCases := [][3]int64{
		{1, 1, 3},
		{1, 0, 3},
		{0, 1, 3},
		{33, 33, 100},
		{1, 1, 100},
		{99, 0, 100},
		{0, 0, 100},
	}
	for _, tc := range testCases {
		grove, other, total := tc[0], tc[1], tc[2]
		for _, barWidth := range []int{1, 5, 10, 20, 30, 50} {
			got := FormatGPUBar(grove, other, total, barWidth)
			closeBracket := strings.Index(got, "]")
			barContent := got[len("["):closeBracket]
			barRunes := utf8.RuneCountInString(barContent)
			if barRunes != barWidth {
				t.Errorf("FormatGPUBar(%d, %d, %d, %d): bar width = %d, want %d; bar=%q",
					grove, other, total, barWidth, barRunes, barWidth, barContent)
			}
		}
	}
}

func TestFormatGPUBar_AllFreeWhenTotalZero(t *testing.T) {
	got := FormatGPUBar(0, 0, 0, 10)
	closeBracket := strings.Index(got, "]")
	barContent := got[len("["):closeBracket]
	// All should be free characters (space)
	freeCount := strings.Count(barContent, " ")
	if freeCount != 10 {
		t.Errorf("expected 10 free chars for total=0, got %d; bar=%q", freeCount, barContent)
	}
}

func TestFormatGPUBar_OvercommitNoFree(t *testing.T) {
	got := FormatGPUBar(60, 10, 56, 20)
	closeBracket := strings.Index(got, "]")
	barContent := got[len("["):closeBracket]
	freeCount := strings.Count(barContent, " ")
	if freeCount != 0 {
		t.Errorf("expected 0 free chars for overcommit, got %d; bar=%q", freeCount, barContent)
	}
}

func TestBuildGPUSummary_PCSGAggregation(t *testing.T) {
	nodeGPUProduct := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
	}
	pods := []TopologyPodInput{
		{
			Name: "pod-a", NodeName: "node-1", GPURequests: 2,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podcliquescalinggroup":      "my-pcs-0-workers",
				"grove.io/podclique":                  "my-pcs-0-workers-0-w",
			},
		},
		{
			Name: "pod-b", NodeName: "node-2", GPURequests: 2,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podcliquescalinggroup":      "my-pcs-0-workers",
				"grove.io/podclique":                  "my-pcs-0-workers-0-w",
			},
		},
	}

	summary := BuildGPUSummary(pods, nodeGPUProduct)

	if summary.ByPCSG["my-pcs-0-workers"]["H200"] != 4 {
		t.Errorf("ByPCSG[my-pcs-0-workers][H200] = %d, want 4", summary.ByPCSG["my-pcs-0-workers"]["H200"])
	}
}

func TestComputeClusterGPUSummary_EmptyCluster(t *testing.T) {
	result := ComputeClusterGPUSummary(
		map[string]string{},
		map[string]int64{},
		nil,
	)
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %v", result)
	}
}

func TestComputeClusterGPUSummary_SingleTypeNoPods(t *testing.T) {
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
	}

	result := ComputeClusterGPUSummary(nodeGPUProducts, nodeGPUCapacity, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 GPU type, got %d", len(result))
	}
	if result[0].GPUType != "H200" {
		t.Errorf("GPUType = %q, want H200", result[0].GPUType)
	}
	if result[0].Total != 16 {
		t.Errorf("Total = %d, want 16", result[0].Total)
	}
	if result[0].Grove != 0 {
		t.Errorf("Grove = %d, want 0", result[0].Grove)
	}
	if result[0].Other != 0 {
		t.Errorf("Other = %d, want 0", result[0].Other)
	}
}

func TestComputeClusterGPUSummary_MixedTypesWithGroveAndOther(t *testing.T) {
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
		"node-3": "B200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
		"node-3": 8,
	}
	pods := []TopologyPodInput{
		// Grove pod on H200 node
		{Name: "grove-pod", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
		// Other pod on H200 node
		{Name: "other-pod", NodeName: "node-2", GPURequests: 2, Labels: map[string]string{}},
		// Grove pod on B200 node
		{Name: "grove-b200", NodeName: "node-3", GPURequests: 6, Labels: map[string]string{
			"app.kubernetes.io/part-of": "other-pcs",
		}},
	}

	result := ComputeClusterGPUSummary(nodeGPUProducts, nodeGPUCapacity, pods)

	if len(result) != 2 {
		t.Fatalf("expected 2 GPU types, got %d", len(result))
	}
	// Sorted: B200 first, then H200
	if result[0].GPUType != "B200" {
		t.Errorf("result[0].GPUType = %q, want B200", result[0].GPUType)
	}
	if result[0].Grove != 6 || result[0].Other != 0 || result[0].Total != 8 {
		t.Errorf("B200: Grove=%d Other=%d Total=%d, want 6/0/8", result[0].Grove, result[0].Other, result[0].Total)
	}
	if result[1].GPUType != "H200" {
		t.Errorf("result[1].GPUType = %q, want H200", result[1].GPUType)
	}
	if result[1].Grove != 4 || result[1].Other != 2 || result[1].Total != 16 {
		t.Errorf("H200: Grove=%d Other=%d Total=%d, want 4/2/16", result[1].Grove, result[1].Other, result[1].Total)
	}
}

func TestComputeClusterGPUSummary_PendingPodsExcluded(t *testing.T) {
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
	}
	pods := []TopologyPodInput{
		// Pending pod (no node) should be excluded
		{Name: "pending-pod", NodeName: "", GPURequests: 4, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
		// Running pod should be counted
		{Name: "running-pod", NodeName: "node-1", GPURequests: 2, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
	}

	result := ComputeClusterGPUSummary(nodeGPUProducts, nodeGPUCapacity, pods)

	if len(result) != 1 {
		t.Fatalf("expected 1 GPU type, got %d", len(result))
	}
	if result[0].Grove != 2 {
		t.Errorf("Grove = %d, want 2 (pending pod excluded)", result[0].Grove)
	}
	if result[0].Total != 8 {
		t.Errorf("Total = %d, want 8", result[0].Total)
	}
}

func TestComputeScopedGPUSummary_WithFilter(t *testing.T) {
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
		"node-3": "B200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
		"node-3": 8,
	}
	pods := []TopologyPodInput{
		{Name: "pod-a", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
		{Name: "pod-b", NodeName: "node-2", GPURequests: 2, Labels: map[string]string{}},
		{Name: "pod-c", NodeName: "node-3", GPURequests: 6, Labels: map[string]string{
			"app.kubernetes.io/part-of": "other-pcs",
		}},
	}

	// Filter to only node-1 and node-2 (H200 nodes)
	nodeFilter := map[string]bool{
		"node-1": true,
		"node-2": true,
	}

	result := ComputeScopedGPUSummary(nodeGPUProducts, nodeGPUCapacity, pods, nodeFilter)

	if len(result) != 1 {
		t.Fatalf("expected 1 GPU type, got %d: %v", len(result), result)
	}
	if result[0].GPUType != "H200" {
		t.Errorf("GPUType = %q, want H200", result[0].GPUType)
	}
	if result[0].Total != 16 {
		t.Errorf("Total = %d, want 16 (8+8 from node-1 and node-2)", result[0].Total)
	}
	if result[0].Grove != 4 {
		t.Errorf("Grove = %d, want 4 (pod-a only)", result[0].Grove)
	}
	if result[0].Other != 2 {
		t.Errorf("Other = %d, want 2 (pod-b only)", result[0].Other)
	}
}

func TestComputeScopedGPUSummary_NilFilter(t *testing.T) {
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "B200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
	}
	pods := []TopologyPodInput{
		{Name: "pod-a", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
	}

	// nil filter = cluster-wide, same as ComputeClusterGPUSummary
	scoped := ComputeScopedGPUSummary(nodeGPUProducts, nodeGPUCapacity, pods, nil)
	cluster := ComputeClusterGPUSummary(nodeGPUProducts, nodeGPUCapacity, pods)

	if len(scoped) != len(cluster) {
		t.Fatalf("scoped len=%d, cluster len=%d", len(scoped), len(cluster))
	}
	for i := range scoped {
		if scoped[i] != cluster[i] {
			t.Errorf("index %d: scoped=%+v, cluster=%+v", i, scoped[i], cluster[i])
		}
	}
}

func TestFormatScopedGPUHeaderSuffix(t *testing.T) {
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
		"node-3": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
		"node-3": 8,
	}
	pods := []TopologyPodInput{
		{Name: "pod-a", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
		{Name: "pod-b", NodeName: "node-2", GPURequests: 2, Labels: map[string]string{}},
		{Name: "pod-c", NodeName: "node-3", GPURequests: 8, Labels: map[string]string{
			"app.kubernetes.io/part-of": "other-pcs",
		}},
	}

	// Scoped to node-1 and node-2 only: used = 4+2 = 6, total = 16, pct = 37%
	result := FormatScopedGPUHeaderSuffix(nodeGPUProducts, nodeGPUCapacity, pods, []string{"node-1", "node-2"})
	want := "·  H200: 6/16 (37%)"
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}

	// nil matchingNodes = cluster-wide: used = 4+2+8 = 14, total = 24, pct = 58%
	resultAll := FormatScopedGPUHeaderSuffix(nodeGPUProducts, nodeGPUCapacity, pods, nil)
	wantAll := "·  H200: 14/24 (58%)"
	if resultAll != wantAll {
		t.Errorf("nil matchingNodes: got %q, want %q", resultAll, wantAll)
	}

	// Empty matchingNodes slice = no nodes match, empty result
	resultEmpty := FormatScopedGPUHeaderSuffix(nodeGPUProducts, nodeGPUCapacity, pods, []string{})
	if resultEmpty != "" {
		t.Errorf("empty matchingNodes: got %q, want empty", resultEmpty)
	}
}

func TestFormatClusterGPUHeaderSuffix_Empty(t *testing.T) {
	result := FormatClusterGPUHeaderSuffix(
		map[string]string{},
		map[string]int64{},
		nil,
	)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestFormatClusterGPUHeaderSuffix_SingleType(t *testing.T) {
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "H200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 56,
		"node-2": 56,
	}
	pods := []TopologyPodInput{
		{Name: "pod-a", NodeName: "node-1", GPURequests: 14, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
		{Name: "pod-b", NodeName: "node-2", GPURequests: 15, Labels: map[string]string{}},
	}

	result := FormatClusterGPUHeaderSuffix(nodeGPUProducts, nodeGPUCapacity, pods)

	// used = 14 + 15 = 29, total = 112, pct = 29*100/112 = 25 (integer division)
	want := "·  H200: 29/112 (25%)"
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}

func TestFormatClusterGPUHeaderSuffix_MultipleTypes(t *testing.T) {
	nodeGPUProducts := map[string]string{
		"node-1": "H200",
		"node-2": "B200",
	}
	nodeGPUCapacity := map[string]int64{
		"node-1": 8,
		"node-2": 8,
	}
	pods := []TopologyPodInput{
		{Name: "pod-a", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{
			"app.kubernetes.io/part-of": "my-pcs",
		}},
		{Name: "pod-b", NodeName: "node-2", GPURequests: 2, Labels: map[string]string{}},
	}

	result := FormatClusterGPUHeaderSuffix(nodeGPUProducts, nodeGPUCapacity, pods)

	// B200: 2/8 (25%), H200: 4/8 (50%) — sorted alphabetically
	want := "·  B200: 2/8 (25%)  ·  H200: 4/8 (50%)"
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}
