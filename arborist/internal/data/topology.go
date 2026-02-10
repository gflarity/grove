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
