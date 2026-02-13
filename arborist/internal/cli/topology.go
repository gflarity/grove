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

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/ai-dynamo/grove/arborist/internal/k8s"
)

// TopologyCmd shows pods grouped by topology domain for all (or a specific) PodCliqueSet(s).
type TopologyCmd struct {
	Domain        string `arg:"" help:"Topology domain (e.g. rack, zone, block, host)."`
	PCS           string `arg:"" optional:"" help:"PodCliqueSet name (default: all PCS)."`
	Namespace     string `short:"n" help:"Kubernetes namespace (defaults to current kubeconfig context namespace)."`
	AllNamespaces bool   `short:"A" help:"Show pods across all namespaces."`
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
	// Resolve namespace from kubeconfig if not specified
	ns := namespace
	if allNamespaces {
		ns = ""
	} else if ns == "" {
		resolved, err := k8s.ResolveCurrentNamespace()
		if err != nil {
			return fmt.Errorf("failed to resolve namespace: %w", err)
		}
		ns = resolved
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	k8sClient, err := k8s.NewK8sClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
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

	// Build display filter predicate and extract display pods
	displayPods, pcsDisplay, isDisplayPod := filterDisplayPods(cliData.AllPods, pcsFilter)

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
		cliData.AllPods, isDisplayPod,
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

// filterDisplayPods partitions allPods into display pods and builds the isDisplayPod predicate.
// Returns: displayPods, pcsDisplayName, isDisplayPod predicate.
func filterDisplayPods(
	allPods []k8s.TopologyCLIPod,
	pcsFilter string,
) ([]k8s.TopologyCLIPod, string, func(*k8s.TopologyCLIPod) bool) {
	var isDisplayPod func(*k8s.TopologyCLIPod) bool
	pcsDisplay := "all"

	if pcsFilter != "" {
		pcsDisplay = pcsFilter
		isDisplayPod = func(pod *k8s.TopologyCLIPod) bool {
			return pod.Labels["app.kubernetes.io/part-of"] == pcsFilter
		}
	} else {
		isDisplayPod = func(pod *k8s.TopologyCLIPod) bool {
			return pod.Labels["app.kubernetes.io/part-of"] != ""
		}
	}

	var displayPods []k8s.TopologyCLIPod
	for i := range allPods {
		if isDisplayPod(&allPods[i]) {
			displayPods = append(displayPods, allPods[i])
		}
	}

	return displayPods, pcsDisplay, isDisplayPod
}

// groupPodsByTopology groups display pods by their node's topology label value.
// Returns scheduled groups and unscheduled GPU pods separately.
// Pods that are unscheduled or on nodes missing the topology label are separated out.
func groupPodsByTopology(
	displayPods []k8s.TopologyCLIPod,
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
func computeThreeWayGPUUsage(
	labelKey string,
	nodeLabels map[string]map[string]string,
	nodeGPUProducts map[string]string,
	nodeGPUCapacity map[string]int64,
	allPods []k8s.TopologyCLIPod,
	isDisplayPod func(*k8s.TopologyCLIPod) bool,
) map[string][]gpuUsageEntry {
	// key: (domainValue, gpuType)
	type dvgt struct{ domainValue, gpuType string }

	capacity := make(map[dvgt]int64)
	thisPCS := make(map[dvgt]int64)
	other := make(map[dvgt]int64)

	// 1. Capacity — from nodes
	for nodeName, labels := range nodeLabels {
		domainValue := labels[labelKey]
		if domainValue == "" {
			continue
		}
		gpuType := nodeGPUProducts[nodeName]
		if gpuType == "" {
			continue
		}
		cap := nodeGPUCapacity[nodeName]
		if cap <= 0 {
			continue
		}
		capacity[dvgt{domainValue, gpuType}] += cap
	}

	// 2. This + Other — from pods
	for i := range allPods {
		pod := &allPods[i]
		if pod.GPURequests <= 0 || pod.NodeName == "" {
			continue
		}
		labels, ok := nodeLabels[pod.NodeName]
		if !ok {
			continue
		}
		domainValue := labels[labelKey]
		if domainValue == "" {
			continue
		}
		gpuType := nodeGPUProducts[pod.NodeName]
		if gpuType == "" {
			continue
		}

		key := dvgt{domainValue, gpuType}
		if isDisplayPod(pod) {
			thisPCS[key] += pod.GPURequests
		} else {
			other[key] += pod.GPURequests
		}
	}

	// 3. Combine into gpuUsageEntry per domain value
	// Collect all (domainValue, gpuType) pairs from capacity
	dvKeys := make(map[string]map[string]bool)
	for key := range capacity {
		if dvKeys[key.domainValue] == nil {
			dvKeys[key.domainValue] = make(map[string]bool)
		}
		dvKeys[key.domainValue][key.gpuType] = true
	}

	result := make(map[string][]gpuUsageEntry)
	for domainValue, gpuTypes := range dvKeys {
		// Sort GPU types
		sortedTypes := make([]string, 0, len(gpuTypes))
		for t := range gpuTypes {
			sortedTypes = append(sortedTypes, t)
		}
		sort.Strings(sortedTypes)

		var entries []gpuUsageEntry
		for _, gpuType := range sortedTypes {
			key := dvgt{domainValue, gpuType}
			total := capacity[key]
			used := thisPCS[key]
			otherUsed := other[key]
			free := total - used - otherUsed
			if free < 0 {
				free = 0
			}
			entries = append(entries, gpuUsageEntry{
				Type:    gpuType,
				ThisPCS: used,
				Total:   total,
				Other:   otherUsed,
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
		bar := data.FormatGPUBar(e.ThisPCS, e.Other, e.Total, 20)
		// Add superscript annotations: ¹ after bar graph, ² after numbers
		bar = strings.Replace(bar, "] (", "]¹ (", 1)
		bar += "²"
		fmt.Fprintf(w, "%s%s %s\n", prefix, e.Type, bar)
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
