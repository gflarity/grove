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
	"strings"
)

// GPUCounts maps GPU type short name (e.g. "H200") to count.
type GPUCounts map[string]int64

// GPUSummary provides pre-aggregated GPU counts keyed by resource hierarchy.
type GPUSummary struct {
	// GPUTypes is the sorted list of distinct GPU types in the cluster (column headers).
	GPUTypes []string
	// ByPod maps podName -> GPUCounts
	ByPod map[string]GPUCounts
	// ByPodClique maps podCliqueName -> GPUCounts
	ByPodClique map[string]GPUCounts
	// ByPCSG maps pcsgName -> GPUCounts
	ByPCSG map[string]GPUCounts
	// ByReplica maps "pcsName/replicaIndex" -> GPUCounts
	ByReplica map[string]GPUCounts
	// ByPCS maps pcsName -> GPUCounts
	ByPCS map[string]GPUCounts
	// PendingGPUPods maps podName -> requested GPU count for pods with GPURequests > 0 but no NodeName.
	PendingGPUPods map[string]int64
}

// ParseGPUProductShortName extracts a short GPU type name from an nvidia.com/gpu.product label value.
//
// Examples:
//
//	"NVIDIA-H200-141GB-HBM3e" → "H200"
//	"NVIDIA-B200-192GB-HBM3e" → "B200"
//	"NVIDIA-H100-80GB-HBM3"  → "H100"
//
// Pattern: split on "-", take the second segment.
// Unknown/empty → "" (skip this node for GPU type classification).
func ParseGPUProductShortName(label string) string {
	if label == "" {
		return ""
	}
	parts := strings.Split(label, "-")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// BuildGPUSummary constructs a GPUSummary from raw pod data and node GPU product mappings.
// It scans nodeGPUProduct to discover GPU types, then iterates pods to aggregate GPU counts
// at every hierarchy level using grove labels.
func BuildGPUSummary(pods []TopologyPodInput, nodeGPUProduct map[string]string) *GPUSummary {
	summary := &GPUSummary{
		ByPod:          make(map[string]GPUCounts),
		ByPodClique:    make(map[string]GPUCounts),
		ByPCSG:         make(map[string]GPUCounts),
		ByReplica:      make(map[string]GPUCounts),
		ByPCS:          make(map[string]GPUCounts),
		PendingGPUPods: make(map[string]int64),
	}

	// 1. Collect sorted set of distinct GPU types from nodes.
	gpuTypeSet := make(map[string]bool)
	for _, gpuType := range nodeGPUProduct {
		if gpuType != "" {
			gpuTypeSet[gpuType] = true
		}
	}
	gpuTypes := make([]string, 0, len(gpuTypeSet))
	for t := range gpuTypeSet {
		gpuTypes = append(gpuTypes, t)
	}
	sort.Strings(gpuTypes)
	summary.GPUTypes = gpuTypes

	// 2. Iterate pods: for each pod with GPURequests > 0, attribute to the node's GPU type.
	for _, pod := range pods {
		if pod.GPURequests <= 0 {
			continue
		}

		// If the pod has no node, it's pending — track separately.
		if pod.NodeName == "" {
			summary.PendingGPUPods[pod.Name] = pod.GPURequests
			// Still aggregate upward so parent rows know there are pending GPU pods,
			// but we don't attribute to any GPU type.
			continue
		}

		// Look up the node's GPU type
		gpuType := nodeGPUProduct[pod.NodeName]
		if gpuType == "" {
			// Node has no recognized GPU type — don't attribute to any column.
			continue
		}

		count := pod.GPURequests

		// Attribute to pod level
		addGPUCount(summary.ByPod, pod.Name, gpuType, count)

		// Extract grove hierarchy labels
		pcsName := pod.Labels["app.kubernetes.io/part-of"]
		replicaIndex := pod.Labels["grove.io/podcliqueset-replica-index"]
		pcsgName := pod.Labels["grove.io/podcliquescalinggroup"]
		podCliqueName := pod.Labels["grove.io/podclique"]

		// Attribute to PodClique level
		if podCliqueName != "" {
			addGPUCount(summary.ByPodClique, podCliqueName, gpuType, count)
		}

		// Attribute to PCSG level
		if pcsgName != "" {
			addGPUCount(summary.ByPCSG, pcsgName, gpuType, count)
		}

		// Attribute to Replica level (pcsName/replicaIndex)
		if pcsName != "" && replicaIndex != "" {
			replicaKey := pcsName + "/" + replicaIndex
			addGPUCount(summary.ByReplica, replicaKey, gpuType, count)
		}

		// Attribute to PCS level
		if pcsName != "" {
			addGPUCount(summary.ByPCS, pcsName, gpuType, count)
		}
	}

	return summary
}

// addGPUCount adds a GPU count to a specific key in a map of GPUCounts.
func addGPUCount(m map[string]GPUCounts, key, gpuType string, count int64) {
	if _, ok := m[key]; !ok {
		m[key] = make(GPUCounts)
	}
	m[key][gpuType] += count
}

// DomainGPUCounts holds used/available GPU counts for a single GPU type.
type DomainGPUCounts struct {
	Used      int64
	Available int64
}

// DomainGPUSummary maps domainValue -> gpuType -> DomainGPUCounts.
type DomainGPUSummary struct {
	GPUTypes []string                              // sorted GPU types (column headers)
	ByValue  map[string]map[string]DomainGPUCounts // value -> gpuType -> counts
}

// ComputeDomainGPUSummary computes GPU used/available for each distinct value
// of a topology domain key, scoped to the given set of matching nodes.
func ComputeDomainGPUSummary(
	domainKey string,
	matchingNodes []string,
	nodeLabels map[string]map[string]string,
	nodeGPUProducts map[string]string,
	nodeGPUCapacity map[string]int64,
	pods []TopologyPodInput,
) *DomainGPUSummary {
	summary := &DomainGPUSummary{
		ByValue: make(map[string]map[string]DomainGPUCounts),
	}

	// Build a set of matching nodes for fast lookup
	nodeSet := make(map[string]bool, len(matchingNodes))
	for _, n := range matchingNodes {
		nodeSet[n] = true
	}

	// 1. Aggregate "available" from matching nodes: for each node, get its domain value,
	// GPU type, and GPU capacity.
	gpuTypeSet := make(map[string]bool)
	for _, nodeName := range matchingNodes {
		labels := nodeLabels[nodeName]
		if labels == nil {
			continue
		}
		domainValue := labels[domainKey]
		if domainValue == "" {
			continue
		}
		gpuType := nodeGPUProducts[nodeName]
		if gpuType == "" {
			continue
		}
		gpuTypeSet[gpuType] = true
		capacity := nodeGPUCapacity[nodeName]

		if summary.ByValue[domainValue] == nil {
			summary.ByValue[domainValue] = make(map[string]DomainGPUCounts)
		}
		counts := summary.ByValue[domainValue][gpuType]
		counts.Available += capacity
		summary.ByValue[domainValue][gpuType] = counts
	}

	// 2. Aggregate "used" from pods on matching nodes.
	for _, pod := range pods {
		if pod.GPURequests <= 0 || pod.NodeName == "" {
			continue
		}
		if !nodeSet[pod.NodeName] {
			continue
		}
		labels := nodeLabels[pod.NodeName]
		if labels == nil {
			continue
		}
		domainValue := labels[domainKey]
		if domainValue == "" {
			continue
		}
		gpuType := nodeGPUProducts[pod.NodeName]
		if gpuType == "" {
			continue
		}

		if summary.ByValue[domainValue] == nil {
			summary.ByValue[domainValue] = make(map[string]DomainGPUCounts)
		}
		counts := summary.ByValue[domainValue][gpuType]
		counts.Used += pod.GPURequests
		summary.ByValue[domainValue][gpuType] = counts
	}

	// 3. Build sorted GPU types from matching nodes.
	gpuTypes := make([]string, 0, len(gpuTypeSet))
	for t := range gpuTypeSet {
		gpuTypes = append(gpuTypes, t)
	}
	sort.Strings(gpuTypes)
	summary.GPUTypes = gpuTypes

	return summary
}

// FormatGPUUsedAvailable formats a used/available GPU count as "used/available".
func FormatGPUUsedAvailable(used, available int64) string {
	return fmt.Sprintf("%d/%d", used, available)
}
