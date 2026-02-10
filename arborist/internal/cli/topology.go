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
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// TopologyCmd shows pods for a PodCliqueSet grouped by topology domain.
type TopologyCmd struct {
	PodCliqueSet string `arg:"" help:"Name of the PodCliqueSet."`
	Domain       string `arg:"" help:"Topology domain (e.g. rack, zone, block, host)."`
	Namespace    string `short:"n" help:"Kubernetes namespace (defaults to current kubeconfig context namespace)."`
}

// Run executes the topology command.
func (c *TopologyCmd) Run(globals *CLI) error {
	return runTopology(c.PodCliqueSet, c.Domain, c.Namespace)
}

// topologyGroup holds the pods grouped under a single topology value.
type topologyGroup struct {
	Value string   // The topology label value (e.g. "rack-0")
	Pods  []string // Pod names sorted alphabetically
}

// runTopology implements the "arborist topology <podcliqueset> <domain>" CLI command.
func runTopology(pcsName, domain, namespace string) error {
	// Resolve namespace from kubeconfig if not specified
	ns := namespace
	if ns == "" {
		resolved, err := k8s.ResolveCurrentNamespace()
		if err != nil {
			return fmt.Errorf("failed to resolve namespace: %w", err)
		}
		ns = resolved
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize K8s client
	k8sClient, err := k8s.NewK8sClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Fetch ClusterTopology to resolve domain -> label key
	ct, err := k8sClient.GetClusterTopology(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch ClusterTopology: %w", err)
	}
	if ct == nil {
		return fmt.Errorf("no ClusterTopology resource found in cluster (TAS not configured?)")
	}

	labelKey := ""
	resolvedDomain := ""
	for _, level := range ct.Spec.Levels {
		if string(level.Domain) == domain {
			labelKey = level.Key
			resolvedDomain = string(level.Domain)
			break
		}
	}
	if labelKey == "" {
		return fmt.Errorf("topology domain %q not found in ClusterTopology; available domains: %s",
			domain, availableDomains(ct))
	}

	// Fetch all pods for this PCS
	podInfo, err := k8sClient.GetPodInfoForPCS(ctx, pcsName, ns)
	if err != nil {
		return fmt.Errorf("failed to fetch pods for PodCliqueSet %s/%s: %w", ns, pcsName, err)
	}
	if len(podInfo) == 0 {
		return fmt.Errorf("no pods found for PodCliqueSet %q in namespace %q", pcsName, ns)
	}

	// Fetch node labels for the topology key
	nodeLabels, err := k8sClient.GetAllNodeLabels(ctx, []string{labelKey})
	if err != nil {
		return fmt.Errorf("failed to fetch node labels: %w", err)
	}

	// Group pods by topology value
	groups, unscheduled := groupPodsByTopology(podInfo, nodeLabels, labelKey)

	// Print the tree
	printTopologyTree(os.Stdout, resolvedDomain, labelKey, pcsName, ns, groups, unscheduled)

	return nil
}

// groupPodsByTopology groups pods by their node's topology label value.
// Pods without a node assignment are returned in the unscheduled slice.
func groupPodsByTopology(
	podInfo map[string]data.CachedPodInfo,
	nodeLabels map[string]map[string]string,
	labelKey string,
) ([]topologyGroup, []string) {
	groupMap := make(map[string][]string)
	var unscheduled []string

	for podName, info := range podInfo {
		if info.NodeName == "" {
			unscheduled = append(unscheduled, podName)
			continue
		}

		value := ""
		if labels, ok := nodeLabels[info.NodeName]; ok {
			value = labels[labelKey]
		}

		if value == "" {
			// Node exists but doesn't have the topology label
			unscheduled = append(unscheduled, podName)
			continue
		}

		groupMap[value] = append(groupMap[value], podName)
	}

	// Sort pods within each group
	for _, pods := range groupMap {
		sort.Strings(pods)
	}
	sort.Strings(unscheduled)

	// Convert to sorted slice of groups
	groups := make([]topologyGroup, 0, len(groupMap))
	for value, pods := range groupMap {
		groups = append(groups, topologyGroup{Value: value, Pods: pods})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Value < groups[j].Value
	})

	return groups, unscheduled
}

// printTopologyTree renders the topology tree to the given writer.
func printTopologyTree(
	w io.Writer,
	domain, labelKey, pcsName, namespace string,
	groups []topologyGroup,
	unscheduled []string,
) {
	fmt.Fprintf(w, "Topology: %s (%s)\n", domain, labelKey)
	fmt.Fprintf(w, "PodCliqueSet: %s (namespace: %s)\n", pcsName, namespace)

	for _, group := range groups {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "┌ %s: %s\n", domain, group.Value)
		printPodList(w, group.Pods)
	}

	if len(unscheduled) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "┌ <unscheduled>\n")
		printPodList(w, unscheduled)
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

// availableDomains returns a comma-separated list of domains from the ClusterTopology.
func availableDomains(ct *corev1alpha1.ClusterTopology) string {
	domains := make([]string, 0, len(ct.Spec.Levels))
	for _, level := range ct.Spec.Levels {
		domains = append(domains, string(level.Domain))
	}
	return strings.Join(domains, ", ")
}
