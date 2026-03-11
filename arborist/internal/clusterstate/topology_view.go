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

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// TopologyPodInput represents raw pod data input for BuildTopologyViewData.
// This decouples the topology view data construction from Kubernetes API types.
type TopologyPodInput struct {
	Namespace   string
	Name        string
	NodeName    string
	Phase       string
	Labels      map[string]string
	GPURequests int64 // Sum of nvidia.com/gpu resource requests across all containers
}

// BuildTopologyViewData constructs a TopologyViewData snapshot from raw cluster state.
// It takes:
//   - levels: ClusterTopology levels ordered broadest to narrowest
//   - pcsSpecs: map of PCS name → PodCliqueSet spec (for resolving pod topology)
//   - pods: raw pod data
//   - nodeLabels: map of nodeName → labelKey → labelValue
//
// Returns a fully-populated TopologyViewData ready for use by the TUI.
func BuildTopologyViewData(
	levels []corev1alpha1.TopologyLevel,
	pcsSpecs map[string]*corev1alpha1.PodCliqueSet,
	pods []TopologyPodInput,
	nodeLabels map[string]map[string]string,
) *TopologyViewData {
	// Build domainToKey from levels
	domainToKey := make(map[string]string, len(levels))
	for _, level := range levels {
		domainToKey[string(level.Domain)] = level.Key
	}

	// Build domain rows with distinct value counts
	domains := make([]TopologyDomainRow, 0, len(levels)+1)
	for _, level := range levels {
		values := make(map[string]bool)
		for _, labels := range nodeLabels {
			if v, ok := labels[level.Key]; ok && v != "" {
				values[v] = true
			}
		}
		domains = append(domains, TopologyDomainRow{
			Domain:      string(level.Domain),
			Key:         level.Key,
			ValuesCount: len(values),
		})
	}
	// N/A row removed — it adds no value to the topology view

	// Pre-build TopologyInfo cache (pcsName -> *TopologyInfo)
	topoInfoCache := make(map[string]*TopologyInfo, len(pcsSpecs))
	for name, pcs := range pcsSpecs {
		info := BuildTopologyInfo(pcs)
		info.DomainToKey = domainToKey
		topoInfoCache[name] = info
	}

	// Build pod list with topology display
	viewPods := make([]TopologyViewPod, 0, len(pods))
	for _, pod := range pods {
		viewPod := TopologyViewPod{
			Namespace: pod.Namespace,
			Name:      pod.Name,
			Phase:     pod.Phase,
		}

		if pod.NodeName != "" {
			viewPod.Node = pod.NodeName
		} else {
			viewPod.Node = "<pending>"
		}

		// Determine topology display
		pcsName := pod.Labels[LabelPartOf]
		info := topoInfoCache[pcsName]
		if pcsName == "" || info == nil {
			viewPod.Topology = "N/A"
		} else {
			cliqueName := resolveCliqueTemplateName(pod.Labels)
			topoDisplay := info.ResolveCliqueTopology(cliqueName)

			// Resolve the actual node label value for the effective domain
			domain := ExtractDomain(topoDisplay)
			value := ""
			if pod.NodeName != "" && domain != "" {
				value = ResolveTopologyValueForNode(domain, pod.NodeName, info, nodeLabels)
			}

			viewPod.Topology = FormatTopologyViewPodDisplay(topoDisplay, value)
		}

		viewPods = append(viewPods, viewPod)
	}

	return &TopologyViewData{
		Domains:     domains,
		NodeLabels:  nodeLabels,
		Pods:        viewPods,
		DomainToKey: domainToKey,
		RawPods:     pods,
	}
}

// resolveCliqueTemplateName extracts the clique template name from pod labels.
// It handles both PCSG-owned cliques and standalone cliques.
func resolveCliqueTemplateName(labels map[string]string) string {
	podCliqueName := labels[LabelPodClique]
	if podCliqueName == "" {
		return ""
	}

	// If the pod is in a PCSG, extract template name from the PCSG child pattern
	pcsgName := labels[LabelPCSG]
	if pcsgName != "" {
		return ExtractCliqueTemplateNameFromPCSGChild(podCliqueName, pcsgName)
	}

	// Standalone clique: extract from PCS name + replica index
	pcsName := labels[LabelPartOf]
	replicaIndex := labels[LabelPCSReplicaIndex]
	if pcsName != "" && replicaIndex != "" {
		return ExtractConfigName(podCliqueName, pcsName, replicaIndex)
	}

	return ""
}

// FormatTopologyViewPodDisplay formats the TOPOLOGY column for the Topology view's pod table.
// The format differs from EnhanceTopologyDisplay:
//   - Explicit: "domain: value" (e.g., "rack: rack-0")
//   - Inherited: "domain: (value)" (e.g., "rack: (rack-0)")
//   - Unscheduled (no value): "domain: (?)"
//   - No topology: "N/A"
func FormatTopologyViewPodDisplay(topoDisplay, value string) string {
	if topoDisplay == "N/A" {
		return "N/A"
	}

	domain := ExtractDomain(topoDisplay)
	inherited := len(topoDisplay) > 0 && topoDisplay[0] == '('

	if value == "" {
		return fmt.Sprintf("%s: (?)", domain)
	}
	if inherited {
		return fmt.Sprintf("%s: (%s)", domain, value)
	}
	return fmt.Sprintf("%s: %s", domain, value)
}

// FilterNodesByBreadcrumb returns node names matching all breadcrumb constraints.
// Only drill stack entries with a non-empty Value are applied as label constraints.
// Entries with empty Value (domain selected but no value chosen yet) are skipped.
// Returns a sorted slice of matching node names.
func FilterNodesByBreadcrumb(nodeLabels map[string]map[string]string, drillStack []TopologyDrillSelection) []string {
	var matchingNodes []string
	for nodeName, labels := range nodeLabels {
		match := true
		for _, sel := range drillStack {
			if sel.Value == "" {
				continue // domain selected but no value yet — no constraint
			}
			if labels[sel.Key] != sel.Value {
				match = false
				break
			}
		}
		if match {
			matchingNodes = append(matchingNodes, nodeName)
		}
	}
	sort.Strings(matchingNodes)
	return matchingNodes
}

// DistinctValuesForDomain returns sorted distinct values for a label key
// across the given set of matching nodes.
func DistinctValuesForDomain(nodeLabels map[string]map[string]string, domainKey string, matchingNodes []string) []string {
	nodeSet := make(map[string]bool, len(matchingNodes))
	for _, n := range matchingNodes {
		nodeSet[n] = true
	}

	values := make(map[string]bool)
	for nodeName, labels := range nodeLabels {
		if !nodeSet[nodeName] {
			continue
		}
		if v, ok := labels[domainKey]; ok && v != "" {
			values[v] = true
		}
	}

	result := make([]string, 0, len(values))
	for v := range values {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}

// DomainPodCounts holds pod counts for a single topology domain value.
type DomainPodCounts struct {
	Total   int
	GPU     int
	Regular int
}

// ComputeDomainPodCounts counts pods per distinct domain value, scoped to matching nodes.
// A pod is a "GPU pod" if GPURequests > 0, and a "regular pod" if GPURequests == 0.
// Pods without a NodeName (pending) are not counted.
func ComputeDomainPodCounts(
	domainKey string,
	matchingNodes []string,
	nodeLabels map[string]map[string]string,
	pods []TopologyPodInput,
) map[string]DomainPodCounts {
	// Build a set of matching nodes for fast lookup
	nodeSet := make(map[string]bool, len(matchingNodes))
	for _, n := range matchingNodes {
		nodeSet[n] = true
	}

	// Build a map from node -> domain value for matching nodes
	nodeToDomainValue := make(map[string]string, len(matchingNodes))
	for _, nodeName := range matchingNodes {
		labels := nodeLabels[nodeName]
		if labels == nil {
			continue
		}
		if v := labels[domainKey]; v != "" {
			nodeToDomainValue[nodeName] = v
		}
	}

	result := make(map[string]DomainPodCounts)
	for _, pod := range pods {
		if pod.NodeName == "" {
			continue // pending pods are not counted
		}
		if !nodeSet[pod.NodeName] {
			continue
		}
		domainValue, ok := nodeToDomainValue[pod.NodeName]
		if !ok {
			continue
		}
		counts := result[domainValue]
		counts.Total++
		if pod.GPURequests > 0 {
			counts.GPU++
		} else {
			counts.Regular++
		}
		result[domainValue] = counts
	}
	return result
}

// FilterPodsByNodes returns pods scheduled on nodes in the given set.
// Pods with Node == "<pending>" will not be matched unless "<pending>" is in nodeNames.
func FilterPodsByNodes(pods []TopologyViewPod, nodeNames []string) []TopologyViewPod {
	nodeSet := make(map[string]bool, len(nodeNames))
	for _, n := range nodeNames {
		nodeSet[n] = true
	}

	var result []TopologyViewPod
	for _, pod := range pods {
		if nodeSet[pod.Node] {
			result = append(result, pod)
		}
	}
	return result
}
