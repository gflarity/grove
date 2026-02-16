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
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	"github.com/ai-dynamo/grove/arborist/internal/k8s"
)

// TopologyCmd shows pods grouped by topology domain for all (or a specific) PodCliqueSet(s).
type TopologyCmd struct {
	Domain string `arg:"" help:"Topology domain (e.g. rack, zone, block, host)."`
	PCS    string `arg:"" optional:"" help:"PodCliqueSet name (default: all PCS)."`
	NamespaceFlags
}

// Run executes the topology command.
func (c *TopologyCmd) Run(globals *CLI) error {
	return runTopology(c.Domain, c.Namespace, c.AllNamespaces, c.PCS)
}

// topologyPod holds a pod name and its GPU annotation info for CLI rendering.
type topologyPod struct {
	Name     string
	GPUType  string // short GPU type (e.g. "H200"), "?" for pending, "" for no GPU
	GPUCount int64  // number of GPUs requested (0 means no GPU annotation)
}

// gpuUsageEntry holds the three-way GPU breakdown for one GPU type in a domain value.
type gpuUsageEntry struct {
	Type    string // "H200", "B200", etc.
	ThisPCS int64  // GPUs used by pods matching the current filter
	Total   int64  // total GPU capacity in this domain value
	Other   int64  // GPUs used by pods NOT matching the current filter
	Free    int64  // Total - ThisPCS - Other
}

// topologyGroup holds the pods grouped under a single topology value.
type topologyGroup struct {
	Value    string          // The topology label value (e.g. "rack-0")
	Pods     []topologyPod   // Pods sorted alphabetically by name
	GPUUsage []gpuUsageEntry // Per-GPU-type three-way breakdown for the header
}

// runTopology implements the "arborist topology <domain> [pcs]" CLI command.
// Uses 3 targeted API calls instead of the full GlobalCache informer machinery.
func runTopology(domain, namespace string, allNamespaces bool, pcsFilter string) error {
	ns, err := resolveNamespace(namespace, allNamespaces)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	k8sClient, err := k8s.NewK8sClient()
	if err != nil {
		return err
	}

	// 3 targeted API calls instead of 7 informers + full snapshot
	cliData, err := k8sClient.FetchTopologyCLIData(ctx, ns)
	if err != nil {
		return fmt.Errorf("failed to fetch topology data: %w", err)
	}

	// Resolve domain → label key
	labelKey := cliData.DomainToKey[domain]
	if labelKey == "" {
		var availDomains []string
		for d := range cliData.DomainToKey {
			availDomains = append(availDomains, d)
		}
		sort.Strings(availDomains)
		return fmt.Errorf("topology domain %q not found in ClusterTopology; available domains: %s",
			domain, strings.Join(availDomains, ", "))
	}

	// Filter pods to those matching the PCS filter
	displayPods, pcsDisplay := filterDisplayPods(cliData.AllPods, pcsFilter)

	if len(displayPods) == 0 {
		if pcsFilter != "" {
			return fmt.Errorf("no pods found for PodCliqueSet %q", pcsFilter)
		}
		nsDisplay := ns
		if allNamespaces {
			nsDisplay = "cluster"
		}
		return fmt.Errorf("no PCS-managed pods found in %s", nsDisplay)
	}

	// Group displayed pods by topology + compute per-pod GPU annotation
	groups, unscheduled := groupPodsByTopology(
		displayPods, cliData.NodeLabels, labelKey, cliData.NodeGPUProducts,
	)

	// Compute three-way GPU breakdown per domain value
	gpuByDomain := computeThreeWayGPUUsage(
		labelKey, cliData.NodeLabels,
		cliData.NodeGPUProducts, cliData.NodeGPUCapacity,
		cliData.AllPods, pcsFilter,
	)
	for i := range groups {
		groups[i].GPUUsage = gpuByDomain[groups[i].Value]
	}

	nsDisplay := ns
	if allNamespaces {
		nsDisplay = "all"
	}
	printTopologyTree(os.Stdout, domain, labelKey, pcsDisplay, nsDisplay, groups, unscheduled)
	return nil
}

// isPCSMatch returns true if the pod's "app.kubernetes.io/part-of" label
// matches the given filter. When pcsFilter is empty, any pod with a non-empty
// "part-of" label is considered a match (i.e. any PCS-managed pod).
func isPCSMatch(labels map[string]string, pcsFilter string) bool {
	partOf := labels["app.kubernetes.io/part-of"]
	if pcsFilter != "" {
		return partOf == pcsFilter
	}
	return partOf != ""
}

// filterDisplayPods partitions allPods into pods matching the PCS filter.
// If pcsFilter is empty, all PCS-managed pods (those with a "part-of" label) are included.
// Returns: displayPods and the PCS display name for rendering.
func filterDisplayPods(
	allPods []clusterstate.TopologyPodInput,
	pcsFilter string,
) ([]clusterstate.TopologyPodInput, string) {
	pcsDisplay := "all"
	if pcsFilter != "" {
		pcsDisplay = pcsFilter
	}

	var displayPods []clusterstate.TopologyPodInput
	for i := range allPods {
		if isPCSMatch(allPods[i].Labels, pcsFilter) {
			displayPods = append(displayPods, allPods[i])
		}
	}

	return displayPods, pcsDisplay
}

// groupPodsByTopology groups display pods by their node's topology label value.
// Returns scheduled groups and unscheduled GPU pods separately.
// Pods that are unscheduled or on nodes missing the topology label are separated out.
func groupPodsByTopology(
	displayPods []clusterstate.TopologyPodInput,
	nodeLabels map[string]map[string]string,
	labelKey string,
	nodeGPUProducts map[string]string,
) ([]topologyGroup, []topologyPod) {
	// Seed with all known domain values from node labels so empty values appear
	groupMap := make(map[string][]topologyPod)
	for _, labels := range nodeLabels {
		if v, ok := labels[labelKey]; ok && v != "" {
			if _, exists := groupMap[v]; !exists {
				groupMap[v] = nil
			}
		}
	}

	var unscheduled []topologyPod

	for _, pod := range displayPods {
		tp := topologyPod{Name: pod.Name}

		// Compute GPU annotation
		if pod.GPURequests > 0 {
			tp.GPUCount = pod.GPURequests
			if pod.NodeName == "" {
				tp.GPUType = "?"
			} else {
				gpuType := nodeGPUProducts[pod.NodeName]
				if gpuType != "" {
					tp.GPUType = gpuType
				} else {
					tp.GPUType = "?"
				}
			}
		}

		if pod.NodeName == "" {
			// Unscheduled pod — only include if it has GPU requests
			if pod.GPURequests > 0 {
				unscheduled = append(unscheduled, tp)
			}
			continue
		}

		value := ""
		if labels, ok := nodeLabels[pod.NodeName]; ok {
			value = labels[labelKey]
		}

		if value == "" {
			continue
		}

		groupMap[value] = append(groupMap[value], tp)
	}

	// Sort pods within each group
	for _, pods := range groupMap {
		sort.Slice(pods, func(i, j int) bool {
			return pods[i].Name < pods[j].Name
		})
	}

	// Sort unscheduled
	sort.Slice(unscheduled, func(i, j int) bool {
		return unscheduled[i].Name < unscheduled[j].Name
	})

	// Build sorted groups
	groups := make([]topologyGroup, 0, len(groupMap))
	for value, pods := range groupMap {
		groups = append(groups, topologyGroup{Value: value, Pods: pods})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Value < groups[j].Value
	})

	return groups, unscheduled
}

// computeThreeWayGPUUsage computes the this/other/free GPU split per domain value.
// pcsFilter selects which pods count as "grove" (this PCS); empty means all PCS-managed pods.
// It delegates to clusterstate.ComputeDomainGPUSummary and converts the result to gpuUsageEntry slices.
func computeThreeWayGPUUsage(
	labelKey string,
	nodeLabels map[string]map[string]string,
	nodeGPUProducts map[string]string,
	nodeGPUCapacity map[string]int64,
	allPods []clusterstate.TopologyPodInput,
	pcsFilter string,
) map[string][]gpuUsageEntry {
	// Build a set of display pod names directly from the PCS filter
	displayPodNames := make(map[string]bool, len(allPods))
	for i := range allPods {
		if isPCSMatch(allPods[i].Labels, pcsFilter) {
			displayPodNames[allPods[i].Name] = true
		}
	}
	classifier := func(pod clusterstate.TopologyPodInput) bool {
		return displayPodNames[pod.Name]
	}

	// Get all node names as matching nodes (all nodes are in scope)
	matchingNodes := make([]string, 0, len(nodeLabels))
	for nodeName := range nodeLabels {
		matchingNodes = append(matchingNodes, nodeName)
	}

	summary := clusterstate.ComputeDomainGPUSummary(clusterstate.DomainGPUInput{
		DomainKey:       labelKey,
		MatchingNodes:   matchingNodes,
		NodeLabels:      nodeLabels,
		NodeGPUProducts: nodeGPUProducts,
		NodeGPUCapacity: nodeGPUCapacity,
		Pods:            allPods,
		Classifier:      classifier,
	})

	// Convert DomainGPUSummary to map[string][]gpuUsageEntry
	result := make(map[string][]gpuUsageEntry)
	for domainValue, gpuCounts := range summary.ByValue {
		var entries []gpuUsageEntry
		for _, gpuType := range summary.GPUTypes {
			counts := gpuCounts[gpuType]
			// Skip GPU types with no presence in this domain value
			if counts.Total == 0 && counts.Grove == 0 && counts.Other == 0 {
				continue
			}
			free := counts.Total - counts.Grove - counts.Other
			if free < 0 {
				free = 0
			}
			entries = append(entries, gpuUsageEntry{
				Type:    gpuType,
				ThisPCS: counts.Grove,
				Total:   counts.Total,
				Other:   counts.Other,
				Free:    free,
			})
		}
		if len(entries) > 0 {
			result[domainValue] = entries
		}
	}

	return result
}

// printGPUMiniTable renders a bar-graph mini table of GPU usage under a block header.
// Lines are prefixed with "│  " (tree continuation).
// Does nothing when entries is empty.
//
// Example output:
//
//	│  H200 [██████░░░░░░░░░░░░░░] 7/0/56
func printGPUMiniTable(w io.Writer, entries []gpuUsageEntry) {
	if len(entries) == 0 {
		return
	}

	prefix := "│  "

	for _, e := range entries {
		bar := clusterstate.FormatGPUBarOnly(e.ThisPCS, e.Other, e.Total, 20)
		fmt.Fprintf(w, "%s%s %s¹ (%d/%d/%d)²\n", prefix, e.Type, bar, e.ThisPCS, e.Other, e.Total)
	}
}

// printTopologyTree renders the topology tree to the given writer.
func printTopologyTree(
	w io.Writer,
	domain, labelKey, pcsDisplay, namespace string,
	groups []topologyGroup,
	unscheduled []topologyPod,
) {
	fmt.Fprintf(w, "Topology: %s (%s)\n", domain, labelKey)
	fmt.Fprintf(w, "Namespace: %s\n", namespace)
	fmt.Fprintf(w, "PodCliqueSets: %s\n", pcsDisplay)

	for _, group := range groups {
		fmt.Fprintln(w)
		hasPods := len(group.Pods) > 0
		hasGPU := len(group.GPUUsage) > 0

		if hasPods || hasGPU {
			fmt.Fprintf(w, "┌ %s: %s\n", domain, group.Value)
			printGPUMiniTable(w, group.GPUUsage)
			if hasPods {
				printPodListWithGPU(w, group.Pods)
			} else {
				fmt.Fprintln(w, "└─")
			}
		} else {
			fmt.Fprintf(w, "─ %s: %s\n", domain, group.Value)
		}
	}

	// Unscheduled GPU pods
	if len(unscheduled) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "┌ <unscheduled>\n")
		printPodListWithGPU(w, unscheduled)
	}

	// Legend for GPU bar graph annotations
	hasGPU := false
	for _, g := range groups {
		if len(g.GPUUsage) > 0 {
			hasGPU = true
			break
		}
	}
	if hasGPU {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  ¹ ▓ = grove  ░ = other  space = free")
		fmt.Fprintln(w, "  ² (grove/other/total)")
	}
}

// printPodListWithGPU prints a list of pods with tree connectors and GPU annotations.
func printPodListWithGPU(w io.Writer, pods []topologyPod) {
	// Compute max pod name length for alignment (only if any pod has GPU info)
	hasGPU := false
	maxNameLen := 0
	for _, pod := range pods {
		if pod.GPUCount > 0 {
			hasGPU = true
		}
		if len(pod.Name) > maxNameLen {
			maxNameLen = len(pod.Name)
		}
	}

	for i, pod := range pods {
		connector := "├─"
		if i == len(pods)-1 {
			connector = "└─"
		}

		if pod.GPUCount > 0 && hasGPU {
			// Right-align GPU annotation
			padding := maxNameLen - len(pod.Name) + 4
			if padding < 2 {
				padding = 2
			}
			fmt.Fprintf(w, "%s %s%s[%s: %d]\n", connector, pod.Name, strings.Repeat(" ", padding), pod.GPUType, pod.GPUCount)
		} else {
			fmt.Fprintf(w, "%s %s\n", connector, pod.Name)
		}
	}
}
