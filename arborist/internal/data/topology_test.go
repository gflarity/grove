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
