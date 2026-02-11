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

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResolveTopologyDisplay(t *testing.T) {
	tests := []struct {
		name      string
		explicit  string
		inherited string
		want      string
	}{
		{
			name:      "explicit topology",
			explicit:  "rack",
			inherited: "zone",
			want:      "rack",
		},
		{
			name:      "inherited topology",
			explicit:  "",
			inherited: "rack",
			want:      "(rack)",
		},
		{
			name:      "no topology",
			explicit:  "",
			inherited: "",
			want:      "N/A",
		},
		{
			name:      "explicit takes precedence over inherited",
			explicit:  "host",
			inherited: "rack",
			want:      "host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTopologyDisplay(tt.explicit, tt.inherited)
			if got != tt.want {
				t.Errorf("ResolveTopologyDisplay(%q, %q) = %q, want %q", tt.explicit, tt.inherited, got, tt.want)
			}
		})
	}
}

func TestBuildTopologyInfo(t *testing.T) {
	tests := []struct {
		name string
		pcs  *corev1alpha1.PodCliqueSet
		want *TopologyInfo
	}{
		{
			name: "PCS with topology at all levels",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-pcs"},
				Spec: corev1alpha1.PodCliqueSetSpec{
					Template: corev1alpha1.PodCliqueSetTemplateSpec{
						TopologyConstraint: &corev1alpha1.TopologyConstraint{
							PackDomain: corev1alpha1.TopologyDomainZone,
						},
						Cliques: []*corev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "router",
								TopologyConstraint: &corev1alpha1.TopologyConstraint{
									PackDomain: corev1alpha1.TopologyDomainHost,
								},
							},
							{
								Name: "worker",
								TopologyConstraint: &corev1alpha1.TopologyConstraint{
									PackDomain: corev1alpha1.TopologyDomainRack,
								},
							},
						},
						PodCliqueScalingGroupConfigs: []corev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "prefill",
								CliqueNames: []string{"worker"},
								TopologyConstraint: &corev1alpha1.TopologyConstraint{
									PackDomain: corev1alpha1.TopologyDomainBlock,
								},
							},
						},
					},
				},
			},
			want: &TopologyInfo{
				PCSPackDomain:        "zone",
				PCSGPackDomains:      map[string]string{"prefill": "block"},
				CliquePackDomains:    map[string]string{"router": "host", "worker": "rack"},
				CliqueToScalingGroup: map[string]string{"worker": "prefill"},
				DomainToKey:          map[string]string{},
			},
		},
		{
			name: "PCS with topology only at PCS level",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "simple-pcs"},
				Spec: corev1alpha1.PodCliqueSetSpec{
					Template: corev1alpha1.PodCliqueSetTemplateSpec{
						TopologyConstraint: &corev1alpha1.TopologyConstraint{
							PackDomain: corev1alpha1.TopologyDomainRack,
						},
						Cliques: []*corev1alpha1.PodCliqueTemplateSpec{
							{Name: "worker"},
						},
					},
				},
			},
			want: &TopologyInfo{
				PCSPackDomain:        "rack",
				PCSGPackDomains:      map[string]string{},
				CliquePackDomains:    map[string]string{},
				CliqueToScalingGroup: map[string]string{},
				DomainToKey:          map[string]string{},
			},
		},
		{
			name: "PCS with no topology",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "no-topo-pcs"},
				Spec: corev1alpha1.PodCliqueSetSpec{
					Template: corev1alpha1.PodCliqueSetTemplateSpec{
						Cliques: []*corev1alpha1.PodCliqueTemplateSpec{
							{Name: "worker"},
						},
					},
				},
			},
			want: &TopologyInfo{
				PCSPackDomain:        "",
				PCSGPackDomains:      map[string]string{},
				CliquePackDomains:    map[string]string{},
				CliqueToScalingGroup: map[string]string{},
				DomainToKey:          map[string]string{},
			},
		},
		{
			name: "PCS with PCSG topology but no PCS topology",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "pcsg-topo-pcs"},
				Spec: corev1alpha1.PodCliqueSetSpec{
					Template: corev1alpha1.PodCliqueSetTemplateSpec{
						Cliques: []*corev1alpha1.PodCliqueTemplateSpec{
							{Name: "leader"},
							{Name: "worker"},
						},
						PodCliqueScalingGroupConfigs: []corev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "decode",
								CliqueNames: []string{"leader", "worker"},
								TopologyConstraint: &corev1alpha1.TopologyConstraint{
									PackDomain: corev1alpha1.TopologyDomainRack,
								},
							},
						},
					},
				},
			},
			want: &TopologyInfo{
				PCSPackDomain:        "",
				PCSGPackDomains:      map[string]string{"decode": "rack"},
				CliquePackDomains:    map[string]string{},
				CliqueToScalingGroup: map[string]string{"leader": "decode", "worker": "decode"},
				DomainToKey:          map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildTopologyInfo(tt.pcs)

			if got.PCSPackDomain != tt.want.PCSPackDomain {
				t.Errorf("PCSPackDomain = %q, want %q", got.PCSPackDomain, tt.want.PCSPackDomain)
			}

			if len(got.PCSGPackDomains) != len(tt.want.PCSGPackDomains) {
				t.Errorf("PCSGPackDomains length = %d, want %d", len(got.PCSGPackDomains), len(tt.want.PCSGPackDomains))
			}
			for k, v := range tt.want.PCSGPackDomains {
				if got.PCSGPackDomains[k] != v {
					t.Errorf("PCSGPackDomains[%q] = %q, want %q", k, got.PCSGPackDomains[k], v)
				}
			}

			if len(got.CliquePackDomains) != len(tt.want.CliquePackDomains) {
				t.Errorf("CliquePackDomains length = %d, want %d", len(got.CliquePackDomains), len(tt.want.CliquePackDomains))
			}
			for k, v := range tt.want.CliquePackDomains {
				if got.CliquePackDomains[k] != v {
					t.Errorf("CliquePackDomains[%q] = %q, want %q", k, got.CliquePackDomains[k], v)
				}
			}

			if len(got.CliqueToScalingGroup) != len(tt.want.CliqueToScalingGroup) {
				t.Errorf("CliqueToScalingGroup length = %d, want %d", len(got.CliqueToScalingGroup), len(tt.want.CliqueToScalingGroup))
			}
			for k, v := range tt.want.CliqueToScalingGroup {
				if got.CliqueToScalingGroup[k] != v {
					t.Errorf("CliqueToScalingGroup[%q] = %q, want %q", k, got.CliqueToScalingGroup[k], v)
				}
			}

			if len(got.DomainToKey) != len(tt.want.DomainToKey) {
				t.Errorf("DomainToKey length = %d, want %d", len(got.DomainToKey), len(tt.want.DomainToKey))
			}
			for k, v := range tt.want.DomainToKey {
				if got.DomainToKey[k] != v {
					t.Errorf("DomainToKey[%q] = %q, want %q", k, got.DomainToKey[k], v)
				}
			}
		})
	}
}

func TestTopologyInfo_ResolvePCSGTopology(t *testing.T) {
	tests := []struct {
		name     string
		info     *TopologyInfo
		pcsgName string
		want     string
	}{
		{
			name: "explicit PCSG topology",
			info: &TopologyInfo{
				PCSPackDomain:   "zone",
				PCSGPackDomains: map[string]string{"prefill": "block"},
			},
			pcsgName: "prefill",
			want:     "block",
		},
		{
			name: "inherited from PCS",
			info: &TopologyInfo{
				PCSPackDomain:   "zone",
				PCSGPackDomains: map[string]string{},
			},
			pcsgName: "decode",
			want:     "(zone)",
		},
		{
			name: "no topology anywhere",
			info: &TopologyInfo{
				PCSPackDomain:   "",
				PCSGPackDomains: map[string]string{},
			},
			pcsgName: "decode",
			want:     "N/A",
		},
		{
			name:     "nil TopologyInfo",
			info:     nil,
			pcsgName: "prefill",
			want:     "N/A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.ResolvePCSGTopology(tt.pcsgName)
			if got != tt.want {
				t.Errorf("ResolvePCSGTopology(%q) = %q, want %q", tt.pcsgName, got, tt.want)
			}
		})
	}
}

func TestTopologyInfo_ResolveStandaloneCliqueTopology(t *testing.T) {
	tests := []struct {
		name       string
		info       *TopologyInfo
		cliqueName string
		want       string
	}{
		{
			name: "explicit clique topology",
			info: &TopologyInfo{
				PCSPackDomain:     "zone",
				CliquePackDomains: map[string]string{"router": "host"},
			},
			cliqueName: "router",
			want:       "host",
		},
		{
			name: "inherited from PCS",
			info: &TopologyInfo{
				PCSPackDomain:     "zone",
				CliquePackDomains: map[string]string{},
			},
			cliqueName: "router",
			want:       "(zone)",
		},
		{
			name: "no topology anywhere",
			info: &TopologyInfo{
				PCSPackDomain:     "",
				CliquePackDomains: map[string]string{},
			},
			cliqueName: "router",
			want:       "N/A",
		},
		{
			name:       "nil TopologyInfo",
			info:       nil,
			cliqueName: "router",
			want:       "N/A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.ResolveStandaloneCliqueTopology(tt.cliqueName)
			if got != tt.want {
				t.Errorf("ResolveStandaloneCliqueTopology(%q) = %q, want %q", tt.cliqueName, got, tt.want)
			}
		})
	}
}

func TestTopologyInfo_ResolveCliqueInPCSGTopology(t *testing.T) {
	tests := []struct {
		name       string
		info       *TopologyInfo
		cliqueName string
		pcsgName   string
		want       string
	}{
		{
			name: "explicit clique topology",
			info: &TopologyInfo{
				PCSPackDomain:     "zone",
				PCSGPackDomains:   map[string]string{"prefill": "block"},
				CliquePackDomains: map[string]string{"worker": "rack"},
			},
			cliqueName: "worker",
			pcsgName:   "prefill",
			want:       "rack",
		},
		{
			name: "inherited from PCSG",
			info: &TopologyInfo{
				PCSPackDomain:     "zone",
				PCSGPackDomains:   map[string]string{"prefill": "block"},
				CliquePackDomains: map[string]string{},
			},
			cliqueName: "worker",
			pcsgName:   "prefill",
			want:       "(block)",
		},
		{
			name: "inherited from PCS when no PCSG topology",
			info: &TopologyInfo{
				PCSPackDomain:     "zone",
				PCSGPackDomains:   map[string]string{},
				CliquePackDomains: map[string]string{},
			},
			cliqueName: "worker",
			pcsgName:   "prefill",
			want:       "(zone)",
		},
		{
			name: "no topology anywhere",
			info: &TopologyInfo{
				PCSPackDomain:     "",
				PCSGPackDomains:   map[string]string{},
				CliquePackDomains: map[string]string{},
			},
			cliqueName: "worker",
			pcsgName:   "prefill",
			want:       "N/A",
		},
		{
			name:       "nil TopologyInfo",
			info:       nil,
			cliqueName: "worker",
			pcsgName:   "prefill",
			want:       "N/A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.ResolveCliqueInPCSGTopology(tt.cliqueName, tt.pcsgName)
			if got != tt.want {
				t.Errorf("ResolveCliqueInPCSGTopology(%q, %q) = %q, want %q", tt.cliqueName, tt.pcsgName, got, tt.want)
			}
		})
	}
}

func TestEnhanceTopologyDisplay(t *testing.T) {
	tests := []struct {
		name    string
		display string
		value   string
		want    string
	}{
		{name: "explicit with value", display: "rack", value: "rack-0", want: "rack: rack-0"},
		{name: "inherited with value", display: "(rack)", value: "rack-0", want: "(rack: rack-0)"},
		{name: "N/A unchanged", display: "N/A", value: "rack-0", want: "N/A"},
		{name: "explicit no value", display: "rack", value: "", want: "rack"},
		{name: "inherited no value", display: "(rack)", value: "", want: "(rack)"},
		{name: "multi values", display: "rack", value: "rack-0,rack-1", want: "rack: rack-0,rack-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnhanceTopologyDisplay(tt.display, tt.value)
			if got != tt.want {
				t.Errorf("EnhanceTopologyDisplay(%q, %q) = %q, want %q", tt.display, tt.value, got, tt.want)
			}
		})
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name    string
		display string
		want    string
	}{
		{name: "plain domain", display: "rack", want: "rack"},
		{name: "inherited domain", display: "(rack)", want: "rack"},
		{name: "domain with value", display: "rack: rack-0", want: "rack"},
		{name: "inherited with value", display: "(rack: rack-0)", want: "rack"},
		{name: "N/A", display: "N/A", want: ""},
		{name: "empty", display: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractDomain(tt.display)
			if got != tt.want {
				t.Errorf("ExtractDomain(%q) = %q, want %q", tt.display, got, tt.want)
			}
		})
	}
}

func TestResolveTopologyValue(t *testing.T) {
	topoInfo := &TopologyInfo{
		DomainToKey: map[string]string{"rack": "topology.io/rack"},
	}
	cachedPods := map[string]CachedPodInfo{
		"pod-1": {NodeName: "node-1", Labels: map[string]string{"grove.io/podclique": "my-pclq"}},
		"pod-2": {NodeName: "node-2", Labels: map[string]string{"grove.io/podclique": "my-pclq"}},
		"pod-3": {NodeName: "node-3", Labels: map[string]string{"grove.io/podclique": "other-pclq"}},
	}
	cachedNodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-0"},
		"node-2": {"topology.io/rack": "rack-0"},
		"node-3": {"topology.io/rack": "rack-1"},
	}

	tests := []struct {
		name       string
		domain     string
		labelKey   string
		labelValue string
		want       string
	}{
		{name: "same rack for all matching pods", domain: "rack", labelKey: "grove.io/podclique", labelValue: "my-pclq", want: "rack-0"},
		{name: "different pod clique", domain: "rack", labelKey: "grove.io/podclique", labelValue: "other-pclq", want: "rack-1"},
		{name: "no matching pods", domain: "rack", labelKey: "grove.io/podclique", labelValue: "nonexistent", want: ""},
		{name: "unknown domain", domain: "block", labelKey: "grove.io/podclique", labelValue: "my-pclq", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTopologyValue(tt.domain, tt.labelKey, tt.labelValue, topoInfo, cachedPods, cachedNodeLabels)
			if got != tt.want {
				t.Errorf("ResolveTopologyValue(%q, %q, %q) = %q, want %q", tt.domain, tt.labelKey, tt.labelValue, got, tt.want)
			}
		})
	}
}

func TestResolveTopologyValueForNode(t *testing.T) {
	topoInfo := &TopologyInfo{
		DomainToKey: map[string]string{"rack": "topology.io/rack"},
	}
	cachedNodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-0"},
	}

	tests := []struct {
		name     string
		domain   string
		nodeName string
		want     string
	}{
		{name: "existing node", domain: "rack", nodeName: "node-1", want: "rack-0"},
		{name: "unknown node", domain: "rack", nodeName: "node-99", want: ""},
		{name: "empty node name", domain: "rack", nodeName: "", want: ""},
		{name: "unknown domain", domain: "block", nodeName: "node-1", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTopologyValueForNode(tt.domain, tt.nodeName, topoInfo, cachedNodeLabels)
			if got != tt.want {
				t.Errorf("ResolveTopologyValueForNode(%q, %q) = %q, want %q", tt.domain, tt.nodeName, got, tt.want)
			}
		})
	}
}

func TestWrapInherited(t *testing.T) {
	tests := []struct {
		name      string
		effective string
		want      string
	}{
		{name: "explicit becomes inherited", effective: "rack", want: "(rack)"},
		{name: "already inherited stays inherited", effective: "(rack)", want: "(rack)"},
		{name: "N/A stays N/A", effective: "N/A", want: "N/A"},
		{name: "empty becomes N/A", effective: "", want: "N/A"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapInherited(tt.effective)
			if got != tt.want {
				t.Errorf("WrapInherited(%q) = %q, want %q", tt.effective, got, tt.want)
			}
		})
	}
}

func TestExtractConfigName(t *testing.T) {
	tests := []struct {
		name         string
		resourceName string
		ownerName    string
		replicaIndex string
		want         string
	}{
		{
			name:         "simple PCSG name",
			resourceName: "my-pcs-0-prefill",
			ownerName:    "my-pcs",
			replicaIndex: "0",
			want:         "prefill",
		},
		{
			name:         "hyphenated PCS name",
			resourceName: "my-cool-pcs-0-prefill",
			ownerName:    "my-cool-pcs",
			replicaIndex: "0",
			want:         "prefill",
		},
		{
			name:         "multi-digit replica index",
			resourceName: "my-pcs-12-decode",
			ownerName:    "my-pcs",
			replicaIndex: "12",
			want:         "decode",
		},
		{
			name:         "hyphenated config name",
			resourceName: "my-pcs-0-my-scaling-group",
			ownerName:    "my-pcs",
			replicaIndex: "0",
			want:         "my-scaling-group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractConfigName(tt.resourceName, tt.ownerName, tt.replicaIndex)
			if got != tt.want {
				t.Errorf("ExtractConfigName(%q, %q, %q) = %q, want %q", tt.resourceName, tt.ownerName, tt.replicaIndex, got, tt.want)
			}
		})
	}
}

func TestExtractCliqueTemplateNameFromPCSGChild(t *testing.T) {
	tests := []struct {
		name                  string
		podCliqueResourceName string
		pcsgResourceName      string
		want                  string
	}{
		{
			name:                  "simple clique name",
			podCliqueResourceName: "my-pcs-0-prefill-0-worker",
			pcsgResourceName:      "my-pcs-0-prefill",
			want:                  "worker",
		},
		{
			name:                  "hyphenated clique name",
			podCliqueResourceName: "my-pcs-0-prefill-0-p-worker",
			pcsgResourceName:      "my-pcs-0-prefill",
			want:                  "p-worker",
		},
		{
			name:                  "multi-digit PCSG replica index",
			podCliqueResourceName: "my-pcs-0-prefill-12-worker",
			pcsgResourceName:      "my-pcs-0-prefill",
			want:                  "worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCliqueTemplateNameFromPCSGChild(tt.podCliqueResourceName, tt.pcsgResourceName)
			if got != tt.want {
				t.Errorf("ExtractCliqueTemplateNameFromPCSGChild(%q, %q) = %q, want %q", tt.podCliqueResourceName, tt.pcsgResourceName, got, tt.want)
			}
		})
	}
}

func TestFormatTopologyViewPodDisplay(t *testing.T) {
	tests := []struct {
		name        string
		topoDisplay string
		value       string
		want        string
	}{
		{name: "explicit with value", topoDisplay: "rack", value: "rack-0", want: "rack: rack-0"},
		{name: "inherited with value", topoDisplay: "(rack)", value: "rack-0", want: "rack: (rack-0)"},
		{name: "explicit unscheduled", topoDisplay: "rack", value: "", want: "rack: (?)"},
		{name: "inherited unscheduled", topoDisplay: "(zone)", value: "", want: "zone: (?)"},
		{name: "N/A unchanged", topoDisplay: "N/A", value: "", want: "N/A"},
		{name: "N/A with value ignored", topoDisplay: "N/A", value: "rack-0", want: "N/A"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTopologyViewPodDisplay(tt.topoDisplay, tt.value)
			if got != tt.want {
				t.Errorf("FormatTopologyViewPodDisplay(%q, %q) = %q, want %q", tt.topoDisplay, tt.value, got, tt.want)
			}
		})
	}
}

func TestBuildTopologyViewData(t *testing.T) {
	levels := []corev1alpha1.TopologyLevel{
		{Domain: corev1alpha1.TopologyDomainRegion, Key: "topology.kubernetes.io/region"},
		{Domain: corev1alpha1.TopologyDomainZone, Key: "topology.kubernetes.io/zone"},
		{Domain: corev1alpha1.TopologyDomainRack, Key: "topology.io/rack"},
	}

	pcsSpecs := map[string]*corev1alpha1.PodCliqueSet{
		"my-pcs": {
			ObjectMeta: metav1.ObjectMeta{Name: "my-pcs"},
			Spec: corev1alpha1.PodCliqueSetSpec{
				Template: corev1alpha1.PodCliqueSetTemplateSpec{
					TopologyConstraint: &corev1alpha1.TopologyConstraint{
						PackDomain: corev1alpha1.TopologyDomainZone,
					},
					Cliques: []*corev1alpha1.PodCliqueTemplateSpec{
						{
							Name: "worker",
							TopologyConstraint: &corev1alpha1.TopologyConstraint{
								PackDomain: corev1alpha1.TopologyDomainRack,
							},
						},
						{
							Name: "leader",
							// No explicit topology — inherits from PCS
						},
					},
				},
			},
		},
	}

	nodeLabels := map[string]map[string]string{
		"node-1": {
			"topology.kubernetes.io/region": "us-east-1",
			"topology.kubernetes.io/zone":   "us-east-1a",
			"topology.io/rack":              "rack-0",
		},
		"node-2": {
			"topology.kubernetes.io/region": "us-east-1",
			"topology.kubernetes.io/zone":   "us-east-1b",
			"topology.io/rack":              "rack-1",
		},
		"node-3": {
			"topology.kubernetes.io/region": "us-west-2",
			"topology.kubernetes.io/zone":   "us-west-2a",
			"topology.io/rack":              "rack-2",
		},
	}

	pods := []TopologyPodInput{
		{
			Namespace: "default",
			Name:      "my-pcs-0-worker-abc",
			NodeName:  "node-1",
			Phase:     "Running",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":            "my-pcs",
				"grove.io/podcliqueset-replica-index":  "0",
				"grove.io/podclique":                   "my-pcs-0-worker",
			},
		},
		{
			Namespace: "default",
			Name:      "my-pcs-0-leader-xyz",
			NodeName:  "node-2",
			Phase:     "Running",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":            "my-pcs",
				"grove.io/podcliqueset-replica-index":  "0",
				"grove.io/podclique":                   "my-pcs-0-leader",
			},
		},
		{
			Namespace: "default",
			Name:      "standalone-pod",
			NodeName:  "node-3",
			Phase:     "Running",
			Labels:    map[string]string{},
		},
		{
			Namespace: "default",
			Name:      "pending-worker",
			NodeName:  "",
			Phase:     "Pending",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":            "my-pcs",
				"grove.io/podcliqueset-replica-index":  "0",
				"grove.io/podclique":                   "my-pcs-0-worker",
			},
		},
	}

	result := BuildTopologyViewData(levels, pcsSpecs, pods, nodeLabels)

	// Verify domains
	if len(result.Domains) != 3 { // region, zone, rack
		t.Fatalf("expected 3 domains, got %d", len(result.Domains))
	}

	// Check domain ordering and values
	expectedDomains := []struct {
		domain      string
		key         string
		valuesCount int
	}{
		{"region", "topology.kubernetes.io/region", 2},   // us-east-1, us-west-2
		{"zone", "topology.kubernetes.io/zone", 3},       // us-east-1a, us-east-1b, us-west-2a
		{"rack", "topology.io/rack", 3},                  // rack-0, rack-1, rack-2
	}
	for i, exp := range expectedDomains {
		if result.Domains[i].Domain != exp.domain {
			t.Errorf("domain[%d].Domain = %q, want %q", i, result.Domains[i].Domain, exp.domain)
		}
		if result.Domains[i].Key != exp.key {
			t.Errorf("domain[%d].Key = %q, want %q", i, result.Domains[i].Key, exp.key)
		}
		if result.Domains[i].ValuesCount != exp.valuesCount {
			t.Errorf("domain[%d].ValuesCount = %d, want %d", i, result.Domains[i].ValuesCount, exp.valuesCount)
		}
	}

	// Verify DomainToKey
	if result.DomainToKey["region"] != "topology.kubernetes.io/region" {
		t.Errorf("DomainToKey[region] = %q, want %q", result.DomainToKey["region"], "topology.kubernetes.io/region")
	}
	if result.DomainToKey["rack"] != "topology.io/rack" {
		t.Errorf("DomainToKey[rack] = %q, want %q", result.DomainToKey["rack"], "topology.io/rack")
	}

	// Verify pods
	if len(result.Pods) != 4 {
		t.Fatalf("expected 4 pods, got %d", len(result.Pods))
	}

	// Worker pod: explicit rack constraint, on node-1 rack-0
	workerPod := result.Pods[0]
	if workerPod.Name != "my-pcs-0-worker-abc" {
		t.Errorf("pod[0].Name = %q, want %q", workerPod.Name, "my-pcs-0-worker-abc")
	}
	if workerPod.Node != "node-1" {
		t.Errorf("pod[0].Node = %q, want %q", workerPod.Node, "node-1")
	}
	if workerPod.Topology != "rack: rack-0" {
		t.Errorf("pod[0].Topology = %q, want %q", workerPod.Topology, "rack: rack-0")
	}

	// Leader pod: inherited zone constraint, on node-2 us-east-1b
	leaderPod := result.Pods[1]
	if leaderPod.Name != "my-pcs-0-leader-xyz" {
		t.Errorf("pod[1].Name = %q, want %q", leaderPod.Name, "my-pcs-0-leader-xyz")
	}
	if leaderPod.Topology != "zone: (us-east-1b)" {
		t.Errorf("pod[1].Topology = %q, want %q", leaderPod.Topology, "zone: (us-east-1b)")
	}

	// Standalone pod: no PCS → N/A
	standalonePod := result.Pods[2]
	if standalonePod.Topology != "N/A" {
		t.Errorf("pod[2].Topology = %q, want %q", standalonePod.Topology, "N/A")
	}

	// Pending worker pod: unscheduled → rack: (?)
	pendingPod := result.Pods[3]
	if pendingPod.Node != "<pending>" {
		t.Errorf("pod[3].Node = %q, want %q", pendingPod.Node, "<pending>")
	}
	if pendingPod.Topology != "rack: (?)" {
		t.Errorf("pod[3].Topology = %q, want %q", pendingPod.Topology, "rack: (?)")
	}
}

func TestBuildTopologyViewData_NoLevels(t *testing.T) {
	result := BuildTopologyViewData(nil, nil, nil, nil)

	if len(result.Domains) != 0 {
		t.Fatalf("expected 0 domains, got %d", len(result.Domains))
	}
	if len(result.Pods) != 0 {
		t.Errorf("expected 0 pods, got %d", len(result.Pods))
	}
}

func TestBuildTopologyViewData_NoPCSSpecs(t *testing.T) {
	levels := []corev1alpha1.TopologyLevel{
		{Domain: corev1alpha1.TopologyDomainRack, Key: "topology.io/rack"},
	}
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-0"},
	}
	pods := []TopologyPodInput{
		{
			Namespace: "default",
			Name:      "my-pod",
			NodeName:  "node-1",
			Phase:     "Running",
			Labels:    map[string]string{"app.kubernetes.io/part-of": "unknown-pcs"},
		},
	}

	result := BuildTopologyViewData(levels, nil, pods, nodeLabels)

	if len(result.Pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(result.Pods))
	}
	if result.Pods[0].Topology != "N/A" {
		t.Errorf("pod topology = %q, want %q", result.Pods[0].Topology, "N/A")
	}
}

func TestBuildTopologyViewData_PCSGPod(t *testing.T) {
	levels := []corev1alpha1.TopologyLevel{
		{Domain: corev1alpha1.TopologyDomainZone, Key: "topology.kubernetes.io/zone"},
		{Domain: corev1alpha1.TopologyDomainRack, Key: "topology.io/rack"},
	}

	pcsSpecs := map[string]*corev1alpha1.PodCliqueSet{
		"my-pcs": {
			ObjectMeta: metav1.ObjectMeta{Name: "my-pcs"},
			Spec: corev1alpha1.PodCliqueSetSpec{
				Template: corev1alpha1.PodCliqueSetTemplateSpec{
					TopologyConstraint: &corev1alpha1.TopologyConstraint{
						PackDomain: corev1alpha1.TopologyDomainZone,
					},
					Cliques: []*corev1alpha1.PodCliqueTemplateSpec{
						{
							Name: "worker",
							TopologyConstraint: &corev1alpha1.TopologyConstraint{
								PackDomain: corev1alpha1.TopologyDomainRack,
							},
						},
					},
					PodCliqueScalingGroupConfigs: []corev1alpha1.PodCliqueScalingGroupConfig{
						{
							Name:        "prefill",
							CliqueNames: []string{"worker"},
							TopologyConstraint: &corev1alpha1.TopologyConstraint{
								PackDomain: corev1alpha1.TopologyDomainRack,
							},
						},
					},
				},
			},
		},
	}

	nodeLabels := map[string]map[string]string{
		"node-1": {
			"topology.kubernetes.io/zone": "us-east-1a",
			"topology.io/rack":            "rack-0",
		},
	}

	pods := []TopologyPodInput{
		{
			Namespace: "default",
			Name:      "my-pcs-0-prefill-0-worker-abc",
			NodeName:  "node-1",
			Phase:     "Running",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":            "my-pcs",
				"grove.io/podcliqueset-replica-index":  "0",
				"grove.io/podclique":                   "my-pcs-0-prefill-0-worker",
				"grove.io/podcliquescalinggroup":       "my-pcs-0-prefill",
			},
		},
	}

	result := BuildTopologyViewData(levels, pcsSpecs, pods, nodeLabels)

	if len(result.Pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(result.Pods))
	}
	// Worker in PCSG "prefill": explicit rack at clique level → "rack: rack-0"
	if result.Pods[0].Topology != "rack: rack-0" {
		t.Errorf("pod topology = %q, want %q", result.Pods[0].Topology, "rack: rack-0")
	}
}

func TestFilterNodesByBreadcrumb(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {
			"topology.kubernetes.io/region": "us-east-1",
			"topology.kubernetes.io/zone":   "us-east-1a",
			"topology.io/rack":              "rack-0",
		},
		"node-2": {
			"topology.kubernetes.io/region": "us-east-1",
			"topology.kubernetes.io/zone":   "us-east-1b",
			"topology.io/rack":              "rack-1",
		},
		"node-3": {
			"topology.kubernetes.io/region": "us-west-2",
			"topology.kubernetes.io/zone":   "us-west-2a",
			"topology.io/rack":              "rack-2",
		},
	}

	tests := []struct {
		name       string
		drillStack []TopologyDrillSelection
		wantNodes  []string
	}{
		{
			name:       "empty stack returns all nodes",
			drillStack: nil,
			wantNodes:  []string{"node-1", "node-2", "node-3"},
		},
		{
			name: "single constraint filters nodes",
			drillStack: []TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
			},
			wantNodes: []string{"node-1", "node-2"},
		},
		{
			name: "multiple constraints narrow down",
			drillStack: []TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: "us-east-1a"},
			},
			wantNodes: []string{"node-1"},
		},
		{
			name: "no matching nodes",
			drillStack: []TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "eu-west-1"},
			},
			wantNodes: nil,
		},
		{
			name: "entries with empty value are skipped",
			drillStack: []TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: ""}, // no value yet
			},
			wantNodes: []string{"node-1", "node-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterNodesByBreadcrumb(nodeLabels, tt.drillStack)
			if len(got) != len(tt.wantNodes) {
				t.Fatalf("FilterNodesByBreadcrumb() returned %d nodes, want %d: got %v", len(got), len(tt.wantNodes), got)
			}
			for i, nodeName := range tt.wantNodes {
				if got[i] != nodeName {
					t.Errorf("node[%d] = %q, want %q", i, got[i], nodeName)
				}
			}
		})
	}
}

func TestDistinctValuesForDomain(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {
			"topology.kubernetes.io/zone": "us-east-1a",
			"topology.io/rack":            "rack-0",
		},
		"node-2": {
			"topology.kubernetes.io/zone": "us-east-1b",
			"topology.io/rack":            "rack-0",
		},
		"node-3": {
			"topology.kubernetes.io/zone": "us-east-1a",
			"topology.io/rack":            "rack-1",
		},
		"node-4": {
			"topology.kubernetes.io/zone": "us-west-2a",
			// No rack label
		},
	}

	tests := []struct {
		name          string
		domainKey     string
		matchingNodes []string
		wantValues    []string
	}{
		{
			name:          "distinct zone values across all nodes",
			domainKey:     "topology.kubernetes.io/zone",
			matchingNodes: []string{"node-1", "node-2", "node-3", "node-4"},
			wantValues:    []string{"us-east-1a", "us-east-1b", "us-west-2a"},
		},
		{
			name:          "rack values with duplicates",
			domainKey:     "topology.io/rack",
			matchingNodes: []string{"node-1", "node-2", "node-3"},
			wantValues:    []string{"rack-0", "rack-1"},
		},
		{
			name:          "subset of nodes",
			domainKey:     "topology.kubernetes.io/zone",
			matchingNodes: []string{"node-1", "node-3"},
			wantValues:    []string{"us-east-1a"},
		},
		{
			name:          "no matching nodes",
			domainKey:     "topology.io/rack",
			matchingNodes: nil,
			wantValues:    nil,
		},
		{
			name:          "nodes without the label key",
			domainKey:     "topology.io/rack",
			matchingNodes: []string{"node-4"},
			wantValues:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DistinctValuesForDomain(nodeLabels, tt.domainKey, tt.matchingNodes)
			if len(got) != len(tt.wantValues) {
				t.Fatalf("DistinctValuesForDomain() returned %d values, want %d: got %v", len(got), len(tt.wantValues), got)
			}
			for i, val := range tt.wantValues {
				if got[i] != val {
					t.Errorf("value[%d] = %q, want %q", i, got[i], val)
				}
			}
		})
	}
}

func TestFilterPodsByNodes(t *testing.T) {
	pods := []TopologyViewPod{
		{Namespace: "default", Node: "node-1", Name: "pod-a", Topology: "rack: rack-0", Phase: "Running"},
		{Namespace: "default", Node: "node-2", Name: "pod-b", Topology: "rack: rack-1", Phase: "Running"},
		{Namespace: "default", Node: "node-3", Name: "pod-c", Topology: "rack: rack-2", Phase: "Running"},
		{Namespace: "default", Node: "<pending>", Name: "pod-d", Topology: "rack: (?)", Phase: "Pending"},
	}

	tests := []struct {
		name      string
		nodeNames []string
		wantPods  []string
	}{
		{
			name:      "filter to specific nodes",
			nodeNames: []string{"node-1", "node-2"},
			wantPods:  []string{"pod-a", "pod-b"},
		},
		{
			name:      "single node",
			nodeNames: []string{"node-3"},
			wantPods:  []string{"pod-c"},
		},
		{
			name:      "no matching nodes",
			nodeNames: []string{"node-99"},
			wantPods:  nil,
		},
		{
			name:      "pending pods not matched by real nodes",
			nodeNames: []string{"node-1", "node-2", "node-3"},
			wantPods:  []string{"pod-a", "pod-b", "pod-c"},
		},
		{
			name:      "empty node list",
			nodeNames: nil,
			wantPods:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterPodsByNodes(pods, tt.nodeNames)
			if len(got) != len(tt.wantPods) {
				t.Fatalf("FilterPodsByNodes() returned %d pods, want %d", len(got), len(tt.wantPods))
			}
			for i, name := range tt.wantPods {
				if got[i].Name != name {
					t.Errorf("pod[%d].Name = %q, want %q", i, got[i].Name, name)
				}
			}
		})
	}
}

func TestComputeDomainPodCounts(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/block": "block-01"},
		"node-2": {"topology.io/block": "block-01"},
		"node-3": {"topology.io/block": "block-02"},
		"node-4": {"topology.io/block": "block-02"},
	}
	allNodes := []string{"node-1", "node-2", "node-3", "node-4"}

	t.Run("basic multi-value scenario with mix of GPU and non-GPU pods", func(t *testing.T) {
		pods := []TopologyPodInput{
			{Name: "gpu-pod-1", NodeName: "node-1", GPURequests: 4},
			{Name: "gpu-pod-2", NodeName: "node-2", GPURequests: 2},
			{Name: "reg-pod-1", NodeName: "node-1", GPURequests: 0},
			{Name: "gpu-pod-3", NodeName: "node-3", GPURequests: 8},
			{Name: "reg-pod-2", NodeName: "node-3", GPURequests: 0},
			{Name: "reg-pod-3", NodeName: "node-4", GPURequests: 0},
		}

		result := ComputeDomainPodCounts("topology.io/block", allNodes, nodeLabels, pods)

		// block-01: node-1 (gpu-pod-1 + reg-pod-1), node-2 (gpu-pod-2) = 3 total, 2 GPU, 1 reg
		b1 := result["block-01"]
		if b1.Total != 3 {
			t.Errorf("block-01 Total = %d, want 3", b1.Total)
		}
		if b1.GPU != 2 {
			t.Errorf("block-01 GPU = %d, want 2", b1.GPU)
		}
		if b1.Regular != 1 {
			t.Errorf("block-01 Regular = %d, want 1", b1.Regular)
		}

		// block-02: node-3 (gpu-pod-3 + reg-pod-2), node-4 (reg-pod-3) = 3 total, 1 GPU, 2 reg
		b2 := result["block-02"]
		if b2.Total != 3 {
			t.Errorf("block-02 Total = %d, want 3", b2.Total)
		}
		if b2.GPU != 1 {
			t.Errorf("block-02 GPU = %d, want 1", b2.GPU)
		}
		if b2.Regular != 2 {
			t.Errorf("block-02 Regular = %d, want 2", b2.Regular)
		}
	})

	t.Run("no pods returns empty map", func(t *testing.T) {
		result := ComputeDomainPodCounts("topology.io/block", allNodes, nodeLabels, nil)
		if len(result) != 0 {
			t.Errorf("expected empty result, got %v", result)
		}
	})

	t.Run("all GPU pods means Regular is 0", func(t *testing.T) {
		pods := []TopologyPodInput{
			{Name: "gpu-1", NodeName: "node-1", GPURequests: 1},
			{Name: "gpu-2", NodeName: "node-1", GPURequests: 4},
		}
		result := ComputeDomainPodCounts("topology.io/block", allNodes, nodeLabels, pods)
		b1 := result["block-01"]
		if b1.Total != 2 || b1.GPU != 2 || b1.Regular != 0 {
			t.Errorf("block-01 = %+v, want {Total:2 GPU:2 Regular:0}", b1)
		}
	})

	t.Run("all regular pods means GPU is 0", func(t *testing.T) {
		pods := []TopologyPodInput{
			{Name: "reg-1", NodeName: "node-1", GPURequests: 0},
			{Name: "reg-2", NodeName: "node-2", GPURequests: 0},
		}
		result := ComputeDomainPodCounts("topology.io/block", allNodes, nodeLabels, pods)
		b1 := result["block-01"]
		if b1.Total != 2 || b1.GPU != 0 || b1.Regular != 2 {
			t.Errorf("block-01 = %+v, want {Total:2 GPU:0 Regular:2}", b1)
		}
	})

	t.Run("scoped to matching nodes only", func(t *testing.T) {
		pods := []TopologyPodInput{
			{Name: "pod-on-1", NodeName: "node-1", GPURequests: 1},
			{Name: "pod-on-3", NodeName: "node-3", GPURequests: 2},
		}
		// Only include node-1 and node-2 (block-01)
		result := ComputeDomainPodCounts("topology.io/block", []string{"node-1", "node-2"}, nodeLabels, pods)
		if len(result) != 1 {
			t.Fatalf("expected 1 domain value, got %d: %v", len(result), result)
		}
		b1 := result["block-01"]
		if b1.Total != 1 || b1.GPU != 1 {
			t.Errorf("block-01 = %+v, want {Total:1 GPU:1 Regular:0}", b1)
		}
		if _, ok := result["block-02"]; ok {
			t.Error("expected block-02 to be absent (node-3 not in matching nodes)")
		}
	})

	t.Run("pending pods (no NodeName) are not counted", func(t *testing.T) {
		pods := []TopologyPodInput{
			{Name: "scheduled", NodeName: "node-1", GPURequests: 1},
			{Name: "pending", NodeName: "", GPURequests: 4},
		}
		result := ComputeDomainPodCounts("topology.io/block", allNodes, nodeLabels, pods)
		b1 := result["block-01"]
		if b1.Total != 1 {
			t.Errorf("block-01 Total = %d, want 1 (pending pod should not be counted)", b1.Total)
		}
	})
}

func TestResolveCliqueTemplateName(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name: "standalone clique",
			labels: map[string]string{
				"app.kubernetes.io/part-of":            "my-pcs",
				"grove.io/podcliqueset-replica-index":  "0",
				"grove.io/podclique":                   "my-pcs-0-worker",
			},
			want: "worker",
		},
		{
			name: "PCSG clique",
			labels: map[string]string{
				"app.kubernetes.io/part-of":            "my-pcs",
				"grove.io/podcliqueset-replica-index":  "0",
				"grove.io/podclique":                   "my-pcs-0-prefill-0-worker",
				"grove.io/podcliquescalinggroup":       "my-pcs-0-prefill",
			},
			want: "worker",
		},
		{
			name: "hyphenated clique in PCSG",
			labels: map[string]string{
				"app.kubernetes.io/part-of":            "my-pcs",
				"grove.io/podcliqueset-replica-index":  "0",
				"grove.io/podclique":                   "my-pcs-0-prefill-0-p-worker",
				"grove.io/podcliquescalinggroup":       "my-pcs-0-prefill",
			},
			want: "p-worker",
		},
		{
			name:   "no podclique label",
			labels: map[string]string{},
			want:   "",
		},
		{
			name: "podclique label only (no PCS info)",
			labels: map[string]string{
				"grove.io/podclique": "some-clique",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCliqueTemplateName(tt.labels)
			if got != tt.want {
				t.Errorf("resolveCliqueTemplateName() = %q, want %q", got, tt.want)
			}
		})
	}
}
