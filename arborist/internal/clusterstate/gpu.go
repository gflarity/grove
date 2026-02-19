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
	// ByPCSGReplica maps "pcsgName/replicaIndex" -> GPUCounts
	ByPCSGReplica map[string]GPUCounts
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

// discoverGPUTypes returns sorted unique GPU type names from node GPU products.
// If nodeFilter is non-nil, only includes nodes in that set.
func discoverGPUTypes(nodeGPUProducts map[string]string, nodeFilter map[string]bool) []string {
	gpuTypeSet := make(map[string]bool)
	for nodeName, gpuType := range nodeGPUProducts {
		if gpuType != "" && (nodeFilter == nil || nodeFilter[nodeName]) {
			gpuTypeSet[gpuType] = true
		}
	}
	gpuTypes := make([]string, 0, len(gpuTypeSet))
	for t := range gpuTypeSet {
		gpuTypes = append(gpuTypes, t)
	}
	sort.Strings(gpuTypes)
	return gpuTypes
}

// podGPUType returns the GPU type for a pod based on its node, or "" if unknown/pending.
func podGPUType(nodeName string, nodeGPUProducts map[string]string) string {
	if nodeName == "" {
		return ""
	}
	return nodeGPUProducts[nodeName]
}

// BuildGPUSummary constructs a GPUSummary from raw pod data and node GPU product mappings.
// It scans nodeGPUProduct to discover GPU types, then iterates pods to aggregate GPU counts
// at every hierarchy level using grove labels.
func BuildGPUSummary(pods []TopologyPodInput, nodeGPUProduct map[string]string) *GPUSummary {
	summary := &GPUSummary{
		ByPod:          make(map[string]GPUCounts),
		ByPodClique:    make(map[string]GPUCounts),
		ByPCSG:         make(map[string]GPUCounts),
		ByPCSGReplica:  make(map[string]GPUCounts),
		ByReplica:      make(map[string]GPUCounts),
		ByPCS:          make(map[string]GPUCounts),
		PendingGPUPods: make(map[string]int64),
	}

	// 1. Collect sorted set of distinct GPU types from all nodes.
	summary.GPUTypes = discoverGPUTypes(nodeGPUProduct, nil)

	// 2. Iterate pods: for each pod with GPURequests > 0, attribute to the node's GPU type.
	for _, pod := range pods {
		if pod.GPURequests <= 0 {
			continue
		}

		// If the pod has no node, it's pending — track separately.
		if pod.NodeName == "" {
			summary.PendingGPUPods[pod.Name] = pod.GPURequests
			continue
		}

		// Look up the node's GPU type
		gpuType := podGPUType(pod.NodeName, nodeGPUProduct)
		if gpuType == "" {
			continue
		}

		count := pod.GPURequests

		// Attribute to pod level
		addGPUCount(summary.ByPod, pod.Name, gpuType, count)

		// Extract grove hierarchy labels
		pcsName := pod.Labels[LabelPartOf]
		replicaIndex := pod.Labels[LabelPCSReplicaIndex]
		pcsgName := pod.Labels[LabelPCSG]
		podCliqueName := pod.Labels[LabelPodClique]

		// Attribute to PodClique level
		if podCliqueName != "" {
			addGPUCount(summary.ByPodClique, podCliqueName, gpuType, count)
		}

		// Attribute to PCSG level
		if pcsgName != "" {
			addGPUCount(summary.ByPCSG, pcsgName, gpuType, count)
		}

		// Attribute to PCSG Replica level (pcsgName/pcsgReplicaIndex)
		pcsgReplicaIndex := pod.Labels[LabelPCSGReplicaIndex]
		if pcsgName != "" && pcsgReplicaIndex != "" {
			pcsgReplicaKey := CompositeKey(pcsgName, pcsgReplicaIndex)
			addGPUCount(summary.ByPCSGReplica, pcsgReplicaKey, gpuType, count)
		}

		// Attribute to Replica level (pcsName/replicaIndex)
		if pcsName != "" && replicaIndex != "" {
			replicaKey := CompositeKey(pcsName, replicaIndex)
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

// DomainGPUCounts holds Grove/Other/Total GPU counts for a single GPU type.
//   - Grove = GPUs used by PCS-managed pods (pods with app.kubernetes.io/part-of label)
//   - Other = GPUs used by non-PCS pods (no app.kubernetes.io/part-of label)
//   - Total = total GPU capacity from node nvidia.com/gpu resource
type DomainGPUCounts struct {
	Grove int64
	Other int64
	Total int64
}

// DomainGPUSummary maps domainValue -> gpuType -> DomainGPUCounts.
type DomainGPUSummary struct {
	GPUTypes []string                              // sorted GPU types (column headers)
	ByValue  map[string]map[string]DomainGPUCounts // value -> gpuType -> counts
}

// PodClassifier determines whether a pod should be classified as "Grove" (true)
// or "Other" (false) for GPU accounting purposes. The default classifier used by
// the TUI checks for the app.kubernetes.io/part-of label. The CLI can supply a
// custom classifier (e.g. filter by specific PCS name).
type PodClassifier func(pod TopologyPodInput) bool

// DefaultPodClassifier classifies pods as "Grove" when they have an
// app.kubernetes.io/part-of label (i.e. PCS-managed pods).
func DefaultPodClassifier(pod TopologyPodInput) bool {
	return pod.Labels[LabelPartOf] != ""
}

// DomainGPUInput holds the parameters for ComputeDomainGPUSummary.
type DomainGPUInput struct {
	DomainKey       string
	MatchingNodes   []string
	NodeLabels      map[string]map[string]string
	NodeGPUProducts map[string]string
	NodeGPUCapacity map[string]int64
	Pods            []TopologyPodInput
	Classifier      PodClassifier // nil means DefaultPodClassifier
}

// ComputeDomainGPUSummary computes GPU Grove/Other/Total for each distinct value
// of a topology domain key, scoped to the given set of matching nodes.
// The Classifier determines how pods are split into Grove vs Other categories.
// If Classifier is nil, DefaultPodClassifier is used.
//   - Grove = GPUs used by pods where classifier returns true
//   - Other = GPUs used by pods where classifier returns false
//   - Total = total GPU capacity from node nvidia.com/gpu resource
func ComputeDomainGPUSummary(input DomainGPUInput) *DomainGPUSummary {
	classify := DefaultPodClassifier
	if input.Classifier != nil {
		classify = input.Classifier
	}

	domainKey := input.DomainKey
	matchingNodes := input.MatchingNodes
	nodeLabels := input.NodeLabels
	nodeGPUProducts := input.NodeGPUProducts
	nodeGPUCapacity := input.NodeGPUCapacity
	pods := input.Pods

	summary := &DomainGPUSummary{
		ByValue: make(map[string]map[string]DomainGPUCounts),
	}

	// Build a set of matching nodes for fast lookup
	nodeSet := make(map[string]bool, len(matchingNodes))
	for _, n := range matchingNodes {
		nodeSet[n] = true
	}

	// 1. Aggregate "Total" (capacity) from matching nodes: for each node, get its domain value,
	// GPU type, and GPU capacity.
	for _, nodeName := range matchingNodes {
		labels := nodeLabels[nodeName]
		if labels == nil {
			continue
		}
		domainValue := labels[domainKey]
		if domainValue == "" {
			continue
		}
		gpuType := podGPUType(nodeName, nodeGPUProducts)
		if gpuType == "" {
			continue
		}
		capacity := nodeGPUCapacity[nodeName]

		if summary.ByValue[domainValue] == nil {
			summary.ByValue[domainValue] = make(map[string]DomainGPUCounts)
		}
		counts := summary.ByValue[domainValue][gpuType]
		counts.Total += capacity
		summary.ByValue[domainValue][gpuType] = counts
	}

	// 2. Aggregate Grove/Other from pods on matching nodes.
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
		gpuType := podGPUType(pod.NodeName, nodeGPUProducts)
		if gpuType == "" {
			continue
		}

		if summary.ByValue[domainValue] == nil {
			summary.ByValue[domainValue] = make(map[string]DomainGPUCounts)
		}
		counts := summary.ByValue[domainValue][gpuType]
		if classify(pod) {
			counts.Grove += pod.GPURequests
		} else {
			counts.Other += pod.GPURequests
		}
		summary.ByValue[domainValue][gpuType] = counts
	}

	// 3. Build sorted GPU types from matching nodes.
	summary.GPUTypes = discoverGPUTypes(nodeGPUProducts, nodeSet)

	return summary
}

// FormatGPUGroveOtherTotal formats GPU counts as "grove/other/total".
func FormatGPUGroveOtherTotal(grove, other, total int64) string {
	return fmt.Sprintf("%d/%d/%d", grove, other, total)
}

// barSegments holds the character counts for each section of a GPU bar.
type barSegments struct {
	grove int
	other int
	free  int
}

// computeBarSegments calculates how many characters each segment (grove, other, free)
// should occupy within the given barWidth based on GPU counts.
func computeBarSegments(grove, other, total int64, barWidth int) barSegments {
	if total <= 0 {
		return barSegments{free: barWidth}
	}

	used := grove + other
	if used > total {
		// Overcommit: scale grove and other proportionally to fill the bar
		groveChars := int(float64(grove) / float64(used) * float64(barWidth))
		otherChars := int(float64(other) / float64(used) * float64(barWidth))
		// Distribute truncation remainder to grove (largest contributor)
		groveChars += barWidth - groveChars - otherChars
		return barSegments{grove: groveChars, other: otherChars}
	}

	groveChars := int(float64(grove) / float64(total) * float64(barWidth))
	otherChars := int(float64(other) / float64(total) * float64(barWidth))
	return barSegments{
		grove: groveChars,
		other: otherChars,
		free:  barWidth - groveChars - otherChars,
	}
}

// renderBarOnly writes a bracketed bar string without the numeric suffix.
func renderBarOnly(seg barSegments) string {
	var b strings.Builder
	b.WriteRune('[')
	for i := 0; i < seg.grove; i++ {
		b.WriteRune('▓')
	}
	for i := 0; i < seg.other; i++ {
		b.WriteRune('░')
	}
	for i := 0; i < seg.free; i++ {
		b.WriteRune(' ')
	}
	b.WriteRune(']')
	return b.String()
}

// renderBar writes a bracketed bar string using the given segment counts.
func renderBar(seg barSegments, grove, other, total int64) string {
	var b strings.Builder
	b.WriteRune('[')
	for i := 0; i < seg.grove; i++ {
		b.WriteRune('▓')
	}
	for i := 0; i < seg.other; i++ {
		b.WriteRune('░')
	}
	for i := 0; i < seg.free; i++ {
		b.WriteRune(' ')
	}
	b.WriteRune(']')
	b.WriteRune(' ')
	b.WriteString(fmt.Sprintf("(%d/%d/%d)", grove, other, total))
	return b.String()
}

// FormatGPUBarOnly renders just the bracketed bar portion (e.g. "[▓▓░   ]")
// without the numeric suffix. Use this when you need to compose your own
// annotated output around the bar.
func FormatGPUBarOnly(grove, other, total int64, barWidth int) string {
	if barWidth <= 0 {
		return "[]"
	}
	seg := computeBarSegments(grove, other, total, barWidth)
	return renderBarOnly(seg)
}

// ClusterGPUTypeSummary holds per-GPU-type cluster-wide totals.
type ClusterGPUTypeSummary struct {
	GPUType string
	Grove   int64
	Other   int64
	Total   int64
}

// ComputeScopedGPUSummary computes GPU totals per type, optionally scoped to a set of nodes.
// If nodeFilter is nil, all nodes are included (cluster-wide).
// It aggregates capacity from nodeGPUCapacity and usage from pods, classifying
// pods as Grove (has app.kubernetes.io/part-of label) or Other using DefaultPodClassifier.
// Pods without a node assignment (pending) are excluded.
// Returns a sorted slice (by GPU type name) for deterministic display.
func ComputeScopedGPUSummary(
	nodeGPUProducts map[string]string,
	nodeGPUCapacity map[string]int64,
	pods []TopologyPodInput,
	nodeFilter map[string]bool,
) []ClusterGPUTypeSummary {
	// Aggregate total capacity per GPU type from nodes.
	totalByType := make(map[string]int64)
	for nodeName, capacity := range nodeGPUCapacity {
		if nodeFilter != nil && !nodeFilter[nodeName] {
			continue
		}
		gpuType := nodeGPUProducts[nodeName]
		if gpuType == "" {
			continue
		}
		totalByType[gpuType] += capacity
	}

	if len(totalByType) == 0 {
		return nil
	}

	// Aggregate grove/other usage per GPU type from pods.
	groveByType := make(map[string]int64)
	otherByType := make(map[string]int64)
	for _, pod := range pods {
		if pod.GPURequests <= 0 || pod.NodeName == "" {
			continue
		}
		if nodeFilter != nil && !nodeFilter[pod.NodeName] {
			continue
		}
		gpuType := nodeGPUProducts[pod.NodeName]
		if gpuType == "" {
			continue
		}
		if DefaultPodClassifier(pod) {
			groveByType[gpuType] += pod.GPURequests
		} else {
			otherByType[gpuType] += pod.GPURequests
		}
	}

	// Collect and sort GPU types.
	gpuTypes := make([]string, 0, len(totalByType))
	for t := range totalByType {
		gpuTypes = append(gpuTypes, t)
	}
	sort.Strings(gpuTypes)

	result := make([]ClusterGPUTypeSummary, len(gpuTypes))
	for i, t := range gpuTypes {
		result[i] = ClusterGPUTypeSummary{
			GPUType: t,
			Grove:   groveByType[t],
			Other:   otherByType[t],
			Total:   totalByType[t],
		}
	}
	return result
}

// ComputeClusterGPUSummary computes cluster-wide GPU totals per GPU type.
// It delegates to ComputeScopedGPUSummary with a nil filter (all nodes).
func ComputeClusterGPUSummary(
	nodeGPUProducts map[string]string,
	nodeGPUCapacity map[string]int64,
	pods []TopologyPodInput,
) []ClusterGPUTypeSummary {
	return ComputeScopedGPUSummary(nodeGPUProducts, nodeGPUCapacity, pods, nil)
}

// formatGPUHeaderSuffix formats a slice of ClusterGPUTypeSummary into a compact
// header suffix string like "·  H200: 29/112 (26%)  ·  B200: 29/112 (26%)".
// Returns "" if summaries is empty.
func formatGPUHeaderSuffix(summaries []ClusterGPUTypeSummary) string {
	if len(summaries) == 0 {
		return ""
	}
	var parts []string
	for _, s := range summaries {
		used := s.Grove + s.Other
		pct := int64(0)
		if s.Total > 0 {
			pct = used * 100 / s.Total
		}
		parts = append(parts, fmt.Sprintf("%s: %d/%d (%d%%)", s.GPUType, used, s.Total, pct))
	}
	return "·  " + strings.Join(parts, "  ·  ")
}

// FormatClusterGPUHeaderSuffix returns a compact string like
// "·  H200: 29/112 (26%)  ·  B200: 29/112 (26%)" for embedding in a section header.
// The "used" count is grove + other. Returns "" if there are no GPU types.
func FormatClusterGPUHeaderSuffix(
	nodeGPUProducts map[string]string,
	nodeGPUCapacity map[string]int64,
	pods []TopologyPodInput,
) string {
	return formatGPUHeaderSuffix(ComputeClusterGPUSummary(nodeGPUProducts, nodeGPUCapacity, pods))
}

// FormatScopedGPUHeaderSuffix returns a GPU summary string scoped to a set of matching nodes.
// Pass nil matchingNodes for cluster-wide (delegates to FormatClusterGPUHeaderSuffix).
func FormatScopedGPUHeaderSuffix(
	nodeGPUProducts map[string]string,
	nodeGPUCapacity map[string]int64,
	pods []TopologyPodInput,
	matchingNodes []string,
) string {
	if matchingNodes == nil {
		return FormatClusterGPUHeaderSuffix(nodeGPUProducts, nodeGPUCapacity, pods)
	}
	nodeFilter := make(map[string]bool, len(matchingNodes))
	for _, n := range matchingNodes {
		nodeFilter[n] = true
	}
	return formatGPUHeaderSuffix(ComputeScopedGPUSummary(nodeGPUProducts, nodeGPUCapacity, pods, nodeFilter))
}

// FormatGPUBar renders a unicode bar graph showing grove/other/free proportions
// within barWidth characters, wrapped in brackets, with a numeric suffix.
//
// Character legend:
//   - ▓ (dark shade U+2593)  = Grove (PCS-managed)
//   - ░ (light shade U+2591) = Other (non-PCS)
//   - ' ' (space)            = Free
//
// Example: "[▓▓                    ] 7/0/56"
func FormatGPUBar(grove, other, total int64, barWidth int) string {
	if barWidth <= 0 {
		return fmt.Sprintf("[] (%d/%d/%d)", grove, other, total)
	}
	seg := computeBarSegments(grove, other, total, barWidth)
	return renderBar(seg, grove, other, total)
}
