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

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// TopologyInfo captures the topology constraints extracted from a PodCliqueSet spec.
// It provides a flat lookup structure so that child views can resolve explicit vs inherited
// topology without re-fetching the PCS or threading the full spec through function signatures.
type TopologyInfo struct {
	// PCSPackDomain is the PCS-level topology packDomain (e.g. "zone"), or "" if none.
	PCSPackDomain string
	// PCSGPackDomains maps PCSG config name -> packDomain (e.g. "prefill" -> "block").
	PCSGPackDomains map[string]string
	// CliquePackDomains maps clique template name -> packDomain (e.g. "p-worker" -> "rack").
	CliquePackDomains map[string]string
	// CliqueToScalingGroup maps clique template name -> PCSG config name.
	// Only populated for cliques that belong to a scaling group.
	CliqueToScalingGroup map[string]string
	// DomainToKey maps topology domain name -> node label key (e.g. "rack" -> "topology.io/rack").
	// Populated from the ClusterTopology CR. Empty if ClusterTopology is not available.
	DomainToKey map[string]string
}

// BuildTopologyInfo extracts topology constraint information from a PodCliqueSet
// into a flat lookup structure for use by the arborist UI.
func BuildTopologyInfo(pcs *corev1alpha1.PodCliqueSet) *TopologyInfo {
	info := &TopologyInfo{
		PCSGPackDomains:      make(map[string]string),
		CliquePackDomains:    make(map[string]string),
		CliqueToScalingGroup: make(map[string]string),
		DomainToKey:          make(map[string]string),
	}

	// PCS-level topology
	if pcs.Spec.Template.TopologyConstraint != nil {
		info.PCSPackDomain = string(pcs.Spec.Template.TopologyConstraint.PackDomain)
	}

	// Clique-level topology
	for _, clique := range pcs.Spec.Template.Cliques {
		if clique == nil {
			continue
		}
		if clique.TopologyConstraint != nil {
			info.CliquePackDomains[clique.Name] = string(clique.TopologyConstraint.PackDomain)
		}
	}

	// PCSG-level topology and clique-to-PCSG mapping
	for _, pcsg := range pcs.Spec.Template.PodCliqueScalingGroupConfigs {
		if pcsg.TopologyConstraint != nil {
			info.PCSGPackDomains[pcsg.Name] = string(pcsg.TopologyConstraint.PackDomain)
		}
		for _, cliqueName := range pcsg.CliqueNames {
			info.CliqueToScalingGroup[cliqueName] = pcsg.Name
		}
	}

	return info
}

// ResolveTopologyDisplay returns the display string for a resource's topology.
//   - If explicit is non-empty, return it directly (e.g. "rack").
//   - If inherited is non-empty, return it in parentheses (e.g. "(rack)").
//   - Otherwise, return "N/A".
func ResolveTopologyDisplay(explicit, inherited string) string {
	if explicit != "" {
		return explicit
	}
	if inherited != "" {
		return fmt.Sprintf("(%s)", inherited)
	}
	return "N/A"
}

// ResolveCliqueTopology resolves the topology display for a PodClique given its template name.
// It checks explicit clique topology, then falls back to PCSG (if the clique belongs to one),
// then to PCS, returning the appropriate explicit or inherited display string.
func (t *TopologyInfo) ResolveCliqueTopology(cliqueName string) string {
	if t == nil {
		return "N/A"
	}

	explicit := t.CliquePackDomains[cliqueName]
	if explicit != "" {
		return explicit
	}

	// Check if this clique belongs to a PCSG
	if pcsgName, ok := t.CliqueToScalingGroup[cliqueName]; ok {
		if pcsgDomain := t.PCSGPackDomains[pcsgName]; pcsgDomain != "" {
			return fmt.Sprintf("(%s)", pcsgDomain)
		}
	}

	// Fall back to PCS-level
	if t.PCSPackDomain != "" {
		return fmt.Sprintf("(%s)", t.PCSPackDomain)
	}

	return "N/A"
}

// ResolvePCSGTopology resolves the topology display for a PodCliqueScalingGroup given its config name.
// It checks explicit PCSG topology, then falls back to PCS-level as inherited.
func (t *TopologyInfo) ResolvePCSGTopology(pcsgName string) string {
	if t == nil {
		return "N/A"
	}

	explicit := t.PCSGPackDomains[pcsgName]
	return ResolveTopologyDisplay(explicit, t.PCSPackDomain)
}

// ResolveStandaloneCliqueTopology resolves topology for a standalone PodClique (not in a PCSG).
// Checks explicit clique topology, then falls back to PCS-level as inherited.
func (t *TopologyInfo) ResolveStandaloneCliqueTopology(cliqueName string) string {
	if t == nil {
		return "N/A"
	}

	explicit := t.CliquePackDomains[cliqueName]
	return ResolveTopologyDisplay(explicit, t.PCSPackDomain)
}

// ResolveCliqueInPCSGTopology resolves topology for a PodClique that belongs to a PCSG.
// Checks explicit clique topology, then PCSG, then PCS as inherited.
func (t *TopologyInfo) ResolveCliqueInPCSGTopology(cliqueName, pcsgName string) string {
	if t == nil {
		return "N/A"
	}

	explicit := t.CliquePackDomains[cliqueName]
	if explicit != "" {
		return explicit
	}

	// Inherited: try PCSG first, then PCS
	if pcsgDomain := t.PCSGPackDomains[pcsgName]; pcsgDomain != "" {
		return fmt.Sprintf("(%s)", pcsgDomain)
	}
	if t.PCSPackDomain != "" {
		return fmt.Sprintf("(%s)", t.PCSPackDomain)
	}

	return "N/A"
}

// WrapInherited takes an effective topology display value and ensures it's shown as inherited.
// If the value is already inherited (parenthesized) or "N/A", it's returned as-is.
// If the value is explicit (e.g. "rack"), it's wrapped in parentheses since the child
// resource inherits it rather than declaring it directly.
func WrapInherited(effective string) string {
	if effective == "N/A" || effective == "" {
		return "N/A"
	}
	// Already inherited (parenthesized)
	if len(effective) > 0 && effective[0] == '(' {
		return effective
	}
	// Explicit at the parent level becomes inherited at the child level
	return fmt.Sprintf("(%s)", effective)
}

// ResolveTopologyValue looks up the actual topology node label value for a given domain
// by filtering cached pods with the given label key/value and reading node labels.
// Returns a comma-separated string of unique values (e.g. "rack-0" or "rack-0,rack-1"),
// or "" if no values can be resolved.
func ResolveTopologyValue(
	domain string,
	labelKey, labelValue string,
	topoInfo *TopologyInfo,
	cachedPods map[string]CachedPodInfo,
	cachedNodeLabels map[string]map[string]string,
) string {
	if topoInfo == nil || cachedPods == nil || cachedNodeLabels == nil {
		return ""
	}
	topologyKey := topoInfo.DomainToKey[domain]
	if topologyKey == "" {
		return ""
	}

	values := make(map[string]bool)
	for _, pod := range cachedPods {
		if pod.Labels[labelKey] == labelValue && pod.NodeName != "" {
			if nodeLabels, ok := cachedNodeLabels[pod.NodeName]; ok {
				if val := nodeLabels[topologyKey]; val != "" {
					values[val] = true
				}
			}
		}
	}

	return JoinValues(values)
}

// ResolveTopologyValueForNode looks up the topology value for a single node.
func ResolveTopologyValueForNode(
	domain, nodeName string,
	topoInfo *TopologyInfo,
	cachedNodeLabels map[string]map[string]string,
) string {
	if topoInfo == nil || cachedNodeLabels == nil || nodeName == "" {
		return ""
	}
	topologyKey := topoInfo.DomainToKey[domain]
	if topologyKey == "" {
		return ""
	}
	if nodeLabels, ok := cachedNodeLabels[nodeName]; ok {
		return nodeLabels[topologyKey]
	}
	return ""
}

// ResolveTopologyValueByReplicaIndex resolves topology value for all pods in a given PCS replica.
func ResolveTopologyValueByReplicaIndex(
	domain, pcsName, replicaIndex string,
	topoInfo *TopologyInfo,
	cachedPods map[string]CachedPodInfo,
	cachedNodeLabels map[string]map[string]string,
) string {
	if topoInfo == nil || cachedPods == nil || cachedNodeLabels == nil {
		return ""
	}
	topologyKey := topoInfo.DomainToKey[domain]
	if topologyKey == "" {
		return ""
	}

	values := make(map[string]bool)
	for _, pod := range cachedPods {
		if pod.Labels["app.kubernetes.io/part-of"] == pcsName &&
			pod.Labels["grove.io/podcliqueset-replica-index"] == replicaIndex &&
			pod.NodeName != "" {
			if nodeLabels, ok := cachedNodeLabels[pod.NodeName]; ok {
				if val := nodeLabels[topologyKey]; val != "" {
					values[val] = true
				}
			}
		}
	}

	return JoinValues(values)
}

// EnhanceTopologyDisplay takes a topology display string (e.g. "rack", "(rack)", "N/A")
// and appends the actual topology value if available.
// "rack" + "rack-0" -> "rack: rack-0"
// "(rack)" + "rack-0" -> "(rack: rack-0)"
// "N/A" + anything -> "N/A"
func EnhanceTopologyDisplay(display, value string) string {
	if display == "N/A" || value == "" {
		return display
	}

	inherited := len(display) > 0 && display[0] == '('
	if inherited {
		domain := display[1 : len(display)-1]
		return fmt.Sprintf("(%s: %s)", domain, value)
	}
	return fmt.Sprintf("%s: %s", display, value)
}

// ExtractDomain extracts the topology domain name from a display string.
// "rack" -> "rack", "(rack)" -> "rack", "rack: rack-0" -> "rack", "(rack: rack-0)" -> "rack"
func ExtractDomain(display string) string {
	if display == "N/A" || display == "" {
		return ""
	}
	s := display
	// Strip parens
	if len(s) > 0 && s[0] == '(' {
		s = s[1 : len(s)-1]
	}
	// Strip value after ": "
	if idx := strings.Index(s, ": "); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// JoinValues joins a set of unique values into a sorted, comma-separated string.
func JoinValues(values map[string]bool) string {
	if len(values) == 0 {
		return ""
	}
	sorted := make([]string, 0, len(values))
	for v := range values {
		sorted = append(sorted, v)
	}
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// ExtractConfigName extracts the config/template name suffix from a generated K8s resource name.
// Generated names follow the pattern: {ownerName}-{replicaIndex}-{configName}
// Given the ownerName and replicaIndex, this strips the "{ownerName}-{replicaIndex}-" prefix.
func ExtractConfigName(resourceName, ownerName, replicaIndex string) string {
	prefix := fmt.Sprintf("%s-%s-", ownerName, replicaIndex)
	return strings.TrimPrefix(resourceName, prefix)
}

// ExtractCliqueTemplateNameFromPCSGChild extracts the clique template name from a PodClique
// resource name within a PCSG. Names follow: {pcsgResourceName}-{pcsgReplicaIndex}-{cliqueTemplateName}.
// Since pcsgReplicaIndex is numeric, we strip the PCSG name prefix and split at the first "-".
func ExtractCliqueTemplateNameFromPCSGChild(podCliqueResourceName, pcsgResourceName string) string {
	prefix := pcsgResourceName + "-"
	rest := strings.TrimPrefix(podCliqueResourceName, prefix)
	// rest is "{pcsgReplicaIndex}-{cliqueTemplateName}"
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	// Fallback: return the rest as-is
	return rest
}

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
		pcsName := pod.Labels["app.kubernetes.io/part-of"]
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
	podCliqueName := labels["grove.io/podclique"]
	if podCliqueName == "" {
		return ""
	}

	// If the pod is in a PCSG, extract template name from the PCSG child pattern
	pcsgName := labels["grove.io/podcliquescalinggroup"]
	if pcsgName != "" {
		return ExtractCliqueTemplateNameFromPCSGChild(podCliqueName, pcsgName)
	}

	// Standalone clique: extract from PCS name + replica index
	pcsName := labels["app.kubernetes.io/part-of"]
	replicaIndex := labels["grove.io/podcliqueset-replica-index"]
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
