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

func TestFormatGPUUsedAvailable(t *testing.T) {
	tests := []struct {
		used      int64
		available int64
		want      string
	}{
		{0, 0, "0/0"},
		{16, 32, "16/32"},
		{32, 32, "32/32"},
		{0, 8, "0/8"},
	}
	for _, tt := range tests {
		got := FormatGPUUsedAvailable(tt.used, tt.available)
		if got != tt.want {
			t.Errorf("FormatGPUUsedAvailable(%d, %d) = %q, want %q", tt.used, tt.available, got, tt.want)
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

	summary := ComputeDomainGPUSummary("topology.io/block", matchingNodes, nodeLabels, nodeGPUProducts, nodeGPUCapacity, pods)

	if len(summary.GPUTypes) != 1 || summary.GPUTypes[0] != "H200" {
		t.Fatalf("GPUTypes = %v, want [H200]", summary.GPUTypes)
	}

	// block-01: 2 nodes with 8 GPUs each = 16 available, pods use 4+2=6
	b01 := summary.ByValue["block-01"]["H200"]
	if b01.Available != 16 {
		t.Errorf("block-01 H200 Available = %d, want 16", b01.Available)
	}
	if b01.Used != 6 {
		t.Errorf("block-01 H200 Used = %d, want 6", b01.Used)
	}

	// block-02: 1 node with 8 GPUs, pod uses 8
	b02 := summary.ByValue["block-02"]["H200"]
	if b02.Available != 8 {
		t.Errorf("block-02 H200 Available = %d, want 8", b02.Available)
	}
	if b02.Used != 8 {
		t.Errorf("block-02 H200 Used = %d, want 8", b02.Used)
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

	summary := ComputeDomainGPUSummary("topology.io/block", matchingNodes, nodeLabels, nodeGPUProducts, nodeGPUCapacity, pods)

	if len(summary.GPUTypes) != 2 || summary.GPUTypes[0] != "B200" || summary.GPUTypes[1] != "H200" {
		t.Fatalf("GPUTypes = %v, want [B200 H200]", summary.GPUTypes)
	}

	// block-01 has H200 only
	b01H200 := summary.ByValue["block-01"]["H200"]
	if b01H200.Available != 8 || b01H200.Used != 4 {
		t.Errorf("block-01 H200 = %d/%d, want 4/8", b01H200.Used, b01H200.Available)
	}
	// block-01 should have no B200
	b01B200 := summary.ByValue["block-01"]["B200"]
	if b01B200.Available != 0 || b01B200.Used != 0 {
		t.Errorf("block-01 B200 = %d/%d, want 0/0", b01B200.Used, b01B200.Available)
	}

	// block-02 has B200 only
	b02B200 := summary.ByValue["block-02"]["B200"]
	if b02B200.Available != 8 || b02B200.Used != 2 {
		t.Errorf("block-02 B200 = %d/%d, want 2/8", b02B200.Used, b02B200.Available)
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

	summary := ComputeDomainGPUSummary("topology.io/block", matchingNodes, nodeLabels, nodeGPUProducts, nodeGPUCapacity, pods)

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

	summary := ComputeDomainGPUSummary("topology.io/block", matchingNodes, nodeLabels, nodeGPUProducts, nodeGPUCapacity, pods)

	b01 := summary.ByValue["block-01"]["H200"]
	// Only running-pod should be counted (pending has no node)
	if b01.Used != 2 {
		t.Errorf("block-01 H200 Used = %d, want 2 (pending pod excluded)", b01.Used)
	}
	if b01.Available != 8 {
		t.Errorf("block-01 H200 Available = %d, want 8", b01.Available)
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

	summary := ComputeDomainGPUSummary("topology.io/block", matchingNodes, nodeLabels, nodeGPUProducts, nodeGPUCapacity, pods)

	b01 := summary.ByValue["block-01"]["H200"]
	if b01.Used != 0 {
		t.Errorf("block-01 H200 Used = %d, want 0", b01.Used)
	}
	if b01.Available != 8 {
		t.Errorf("block-01 H200 Available = %d, want 8", b01.Available)
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
