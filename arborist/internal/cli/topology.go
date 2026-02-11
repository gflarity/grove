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

// topologyGroup holds the pods grouped under a single topology value.
type topologyGroup struct {
	Value string   // The topology label value (e.g. "rack-0")
	Pods  []string // Pod names sorted alphabetically
}

// runTopology implements the "arborist topology <domain> [--pcs <name>]" CLI command.
// When pcsFilter is empty, pods from all PCS in the namespace are shown.
// When pcsFilter is set, only pods from that specific PCS are shown.
// When allNamespaces is true, pods from all namespaces are included.
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

	// Initialize K8s client and global cache
	k8sClient, err := k8s.NewK8sClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	cache := k8sClient.NewGlobalCache()
	if err := cache.Start(ctx); err != nil {
		return fmt.Errorf("failed to start cache: %w", err)
	}
	defer cache.Stop()

	syncCtx, syncCancel := context.WithTimeout(ctx, 15*time.Second)
	defer syncCancel()
	if !cache.WaitForSync(syncCtx) {
		return fmt.Errorf("timed out waiting for cache to sync")
	}

	snapshot := cache.Snapshot()
	if snapshot == nil {
		return fmt.Errorf("no data available from cache")
	}

	// Find ClusterTopology levels from topology view data
	if snapshot.TopologyViewData == nil {
		return fmt.Errorf("no ClusterTopology resource found in cluster (TAS not configured?)")
	}

	labelKey := ""
	resolvedDomain := ""
	domainToKey := snapshot.TopologyViewData.DomainToKey
	for d, k := range domainToKey {
		if d == domain {
			labelKey = k
			resolvedDomain = d
			break
		}
	}
	if labelKey == "" {
		// Build available domains string
		var availDomains []string
		for d := range domainToKey {
			availDomains = append(availDomains, d)
		}
		sort.Strings(availDomains)
		return fmt.Errorf("topology domain %q not found in ClusterTopology; available domains: %s",
			domain, strings.Join(availDomains, ", "))
	}

	// Determine which PCS names to include
	pcsDisplay := "all"
	nsDisplay := ns
	if allNamespaces {
		nsDisplay = "all"
	}
	var pcsNames map[string]bool

	if pcsFilter != "" {
		// Validate the specific PCS exists
		if _, ok := snapshot.PodCliqueSetSpecs[pcsFilter]; !ok {
			return fmt.Errorf("PodCliqueSet %q not found in cluster", pcsFilter)
		}
		pcsDisplay = pcsFilter
		pcsNames = map[string]bool{pcsFilter: true}
	} else {
		// Collect PCS names, optionally filtered by namespace
		pcsNames = make(map[string]bool)
		for _, pcs := range snapshot.PodCliqueSets {
			if allNamespaces || pcs.Namespace == ns {
				pcsNames[pcs.Name] = true
			}
		}
		if len(pcsNames) == 0 {
			if allNamespaces {
				return fmt.Errorf("no PodCliqueSets found in cluster")
			}
			return fmt.Errorf("no PodCliqueSets found in namespace %q", ns)
		}
	}

	// Filter pod info to matching PCS names
	podInfo := make(map[string]data.CachedPodInfo)
	for podName, info := range snapshot.PodInfos {
		if pcsNames[info.Labels["app.kubernetes.io/part-of"]] {
			podInfo[podName] = info
		}
	}
	if len(podInfo) == 0 {
		if pcsFilter != "" {
			return fmt.Errorf("no pods found for PodCliqueSet %q", pcsFilter)
		}
		if allNamespaces {
			return fmt.Errorf("no pods found for any PodCliqueSet in cluster")
		}
		return fmt.Errorf("no pods found for any PodCliqueSet in namespace %q", ns)
	}

	// Group pods by topology value (unscheduled pods are silently omitted)
	groups := groupPodsByTopology(podInfo, snapshot.NodeLabels, labelKey)

	// Print the tree
	printTopologyTree(os.Stdout, resolvedDomain, labelKey, pcsDisplay, nsDisplay, groups)

	return nil
}

// groupPodsByTopology groups pods by their node's topology label value.
// All distinct values for the label key across all nodes are included, even
// if no matching pods are scheduled there (empty groups).
// Pods that are unscheduled or on nodes missing the topology label are omitted.
func groupPodsByTopology(
	podInfo map[string]data.CachedPodInfo,
	nodeLabels map[string]map[string]string,
	labelKey string,
) []topologyGroup {
	// Seed with all known domain values from node labels so empty values appear
	groupMap := make(map[string][]string)
	for _, labels := range nodeLabels {
		if v, ok := labels[labelKey]; ok && v != "" {
			if _, exists := groupMap[v]; !exists {
				groupMap[v] = nil
			}
		}
	}

	for podName, info := range podInfo {
		if info.NodeName == "" {
			continue
		}

		value := ""
		if labels, ok := nodeLabels[info.NodeName]; ok {
			value = labels[labelKey]
		}

		if value == "" {
			continue
		}

		groupMap[value] = append(groupMap[value], podName)
	}

	for _, pods := range groupMap {
		sort.Strings(pods)
	}

	groups := make([]topologyGroup, 0, len(groupMap))
	for value, pods := range groupMap {
		groups = append(groups, topologyGroup{Value: value, Pods: pods})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Value < groups[j].Value
	})

	return groups
}

// printTopologyTree renders the topology tree to the given writer.
func printTopologyTree(
	w io.Writer,
	domain, labelKey, pcsDisplay, namespace string,
	groups []topologyGroup,
) {
	fmt.Fprintf(w, "Topology: %s (%s)\n", domain, labelKey)
	fmt.Fprintf(w, "Namespace: %s\n", namespace)
	fmt.Fprintf(w, "PodCliqueSets: %s\n", pcsDisplay)

	for _, group := range groups {
		fmt.Fprintln(w)
		if len(group.Pods) == 0 {
			fmt.Fprintf(w, "─ %s: %s\n", domain, group.Value)
		} else {
			fmt.Fprintf(w, "┌ %s: %s\n", domain, group.Value)
			printPodList(w, group.Pods)
		}
	}
}

// printPodList prints a list of pods with tree connectors.
func printPodList(w io.Writer, pods []string) {
	for i, pod := range pods {
		if i == len(pods)-1 {
			fmt.Fprintf(w, "└─ %s\n", pod)
		} else {
			fmt.Fprintf(w, "├─ %s\n", pod)
		}
	}
}
