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

package utils

import (
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGetAllPodCliqueScalingGroupFQNsForPGSReplica verifies that the function correctly generates
// fully qualified names for all PodCliqueScalingGroups defined in a PodGangSet for a specific replica.
func TestGetAllPodCliqueScalingGroupFQNsForPGSReplica(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the input PodGangSet containing PodCliqueScalingGroup configurations
		pgs *grovecorev1alpha1.PodGangSet
		// pgsReplicaIndex is the replica index for which to generate FQNs
		pgsReplicaIndex int
		// expectedFQNs are the expected fully qualified names that should be generated
		expectedFQNs []string
	}{
		{
			// Tests the case where no PodCliqueScalingGroups are defined
			name: "no scaling groups",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{},
					},
				},
			},
			pgsReplicaIndex: 0,
			expectedFQNs:    []string{},
		},
		{
			// Tests generation of FQNs for a single scaling group with replica index 0
			name: "single scaling group with replica 0",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "worker-group",
								CliqueNames: []string{"worker"},
							},
						},
					},
				},
			},
			pgsReplicaIndex: 0,
			expectedFQNs:    []string{"my-pgs-0-worker-group"},
		},
		{
			// Tests generation of FQNs for multiple scaling groups with replica index 1
			name: "multiple scaling groups with replica 1",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "complex-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "compute-group",
								CliqueNames: []string{"compute-a", "compute-b"},
							},
							{
								Name:        "storage-group",
								CliqueNames: []string{"storage"},
							},
							{
								Name:        "network-group",
								CliqueNames: []string{"ingress", "egress"},
							},
						},
					},
				},
			},
			pgsReplicaIndex: 1,
			expectedFQNs: []string{
				"complex-pgs-1-compute-group",
				"complex-pgs-1-storage-group",
				"complex-pgs-1-network-group",
			},
		},
		{
			// Tests generation of FQNs with higher replica index to validate proper formatting
			name: "scaling groups with high replica index",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "scaled-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "batch-processors",
								CliqueNames: []string{"processor-1", "processor-2"},
							},
						},
					},
				},
			},
			pgsReplicaIndex: 42,
			expectedFQNs:    []string{"scaled-pgs-42-batch-processors"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute the function under test
			actualFQNs := GetAllPodCliqueScalingGroupFQNsForPGSReplica(tc.pgs, tc.pgsReplicaIndex)

			// Verify the returned FQNs match expectations
			assert.Equal(t, tc.expectedFQNs, actualFQNs, "Generated FQNs should match expected values")
			assert.Len(t, actualFQNs, len(tc.expectedFQNs), "Number of generated FQNs should match expected count")
		})
	}
}

// TestGetPodCliqueFQNsForPGSReplicaNotInPCSG verifies that the function correctly identifies
// and generates FQNs for PodCliques that are not managed by any PodCliqueScalingGroup.
func TestGetPodCliqueFQNsForPGSReplicaNotInPCSG(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the input PodGangSet containing both PodCliques and PodCliqueScalingGroup configurations
		pgs *grovecorev1alpha1.PodGangSet
		// pgsReplicaIndex is the replica index for which to generate FQNs
		pgsReplicaIndex int
		// expectedFQNs are the expected FQNs for PodCliques not managed by any scaling group
		expectedFQNs []string
	}{
		{
			// Tests the case where no PodCliques are defined
			name: "no cliques defined",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "empty-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques:                      []*grovecorev1alpha1.PodCliqueTemplateSpec{},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{},
					},
				},
			},
			pgsReplicaIndex: 0,
			expectedFQNs:    []string{},
		},
		{
			// Tests the case where all PodCliques are managed by scaling groups
			name: "all cliques in scaling groups",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "worker-a"},
							{Name: "worker-b"},
							{Name: "storage"},
						},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "worker-group",
								CliqueNames: []string{"worker-a", "worker-b"},
							},
							{
								Name:        "storage-group",
								CliqueNames: []string{"storage"},
							},
						},
					},
				},
			},
			pgsReplicaIndex: 0,
			expectedFQNs:    []string{},
		},
		{
			// Tests the case where some PodCliques are standalone (not in any scaling group)
			name: "mixed cliques - some standalone, some in scaling groups",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mixed-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "coordinator"}, // standalone
							{Name: "worker-1"},    // in scaling group
							{Name: "worker-2"},    // in scaling group
							{Name: "monitor"},     // standalone
							{Name: "cache"},       // standalone
						},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "worker-group",
								CliqueNames: []string{"worker-1", "worker-2"},
							},
						},
					},
				},
			},
			pgsReplicaIndex: 1,
			expectedFQNs: []string{
				"mixed-pgs-1-coordinator",
				"mixed-pgs-1-monitor",
				"mixed-pgs-1-cache",
			},
		},
		{
			// Tests the case where all PodCliques are standalone (none in scaling groups)
			name: "all cliques standalone",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standalone-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "frontend"},
							{Name: "backend"},
							{Name: "database"},
						},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{},
					},
				},
			},
			pgsReplicaIndex: 2,
			expectedFQNs: []string{
				"standalone-pgs-2-frontend",
				"standalone-pgs-2-backend",
				"standalone-pgs-2-database",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute the function under test
			actualFQNs := GetPodCliqueFQNsForPGSReplicaNotInPCSG(tc.pgs, tc.pgsReplicaIndex)

			// Verify the returned FQNs match expectations
			assert.ElementsMatch(t, tc.expectedFQNs, actualFQNs, "Generated FQNs should match expected values")
			assert.Len(t, actualFQNs, len(tc.expectedFQNs), "Number of generated FQNs should match expected count")
		})
	}
}

// TestIsPCLQInPCSG verifies that the helper function correctly determines whether
// a PodClique name is managed by any PodCliqueScalingGroup configuration.
func TestIsPCLQInPCSG(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pclqName is the PodClique name to search for in the scaling group configurations
		pclqName string
		// pcsgConfigs are the PodCliqueScalingGroup configurations to search within
		pcsgConfigs []grovecorev1alpha1.PodCliqueScalingGroupConfig
		// expectedFound indicates whether the PodClique should be found in any scaling group
		expectedFound bool
	}{
		{
			// Tests the case where no scaling group configurations exist
			name:          "no scaling group configs",
			pclqName:      "any-clique",
			pcsgConfigs:   []grovecorev1alpha1.PodCliqueScalingGroupConfig{},
			expectedFound: false,
		},
		{
			// Tests successful lookup when PodClique exists in the first scaling group
			name:     "clique found in first scaling group",
			pclqName: "worker-a",
			pcsgConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
				{
					Name:        "worker-group",
					CliqueNames: []string{"worker-a", "worker-b"},
				},
				{
					Name:        "storage-group",
					CliqueNames: []string{"storage-a", "storage-b"},
				},
			},
			expectedFound: true,
		},
		{
			// Tests successful lookup when PodClique exists in a later scaling group
			name:     "clique found in second scaling group",
			pclqName: "storage-b",
			pcsgConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
				{
					Name:        "worker-group",
					CliqueNames: []string{"worker-a", "worker-b"},
				},
				{
					Name:        "storage-group",
					CliqueNames: []string{"storage-a", "storage-b"},
				},
			},
			expectedFound: true,
		},
		{
			// Tests the case where PodClique is not found in any scaling group
			name:     "clique not found in any scaling group",
			pclqName: "standalone-clique",
			pcsgConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
				{
					Name:        "worker-group",
					CliqueNames: []string{"worker-a", "worker-b"},
				},
				{
					Name:        "storage-group",
					CliqueNames: []string{"storage-a", "storage-b"},
				},
			},
			expectedFound: false,
		},
		{
			// Tests edge case with empty clique names in scaling group configurations
			name:     "empty clique names in scaling groups",
			pclqName: "any-clique",
			pcsgConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
				{
					Name:        "empty-group-1",
					CliqueNames: []string{},
				},
				{
					Name:        "empty-group-2",
					CliqueNames: []string{},
				},
			},
			expectedFound: false,
		},
		{
			// Tests lookup with a single clique in a scaling group
			name:     "single clique in scaling group",
			pclqName: "singleton",
			pcsgConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
				{
					Name:        "singleton-group",
					CliqueNames: []string{"singleton"},
				},
			},
			expectedFound: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute the function under test
			actualFound := isPCLQInPCSG(tc.pclqName, tc.pcsgConfigs)

			// Verify the result matches expectations
			assert.Equal(t, tc.expectedFound, actualFound, "Function should correctly identify if PodClique is in any scaling group")
		})
	}
}

// TestGetPodGangSetName verifies that the function correctly extracts the PodGangSet name
// from the labels of the provided ObjectMeta.
func TestGetPodGangSetName(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// objectMeta is the input metadata containing labels
		objectMeta metav1.ObjectMeta
		// expectedPGSName is the expected PodGangSet name that should be extracted
		expectedPGSName string
	}{
		{
			// Tests successful extraction when the part-of label is present
			name: "successful extraction with part-of label",
			objectMeta: metav1.ObjectMeta{
				Name:      "test-object",
				Namespace: "test-namespace",
				Labels: map[string]string{
					grovecorev1alpha1.LabelPartOfKey: "my-podgangset",
					"other-label":                    "other-value",
				},
			},
			expectedPGSName: "my-podgangset",
		},
		{
			// Tests the case where no labels exist on the object
			name: "no labels on object",
			objectMeta: metav1.ObjectMeta{
				Name:      "unlabeled-object",
				Namespace: "test-namespace",
			},
			expectedPGSName: "",
		},
		{
			// Tests the case where labels exist but part-of label is missing
			name: "labels exist but part-of label missing",
			objectMeta: metav1.ObjectMeta{
				Name:      "partially-labeled-object",
				Namespace: "test-namespace",
				Labels: map[string]string{
					"app":         "my-app",
					"version":     "v1.0",
					"environment": "production",
				},
			},
			expectedPGSName: "",
		},
		{
			// Tests extraction when part-of label has empty value
			name: "part-of label with empty value",
			objectMeta: metav1.ObjectMeta{
				Name:      "empty-labeled-object",
				Namespace: "test-namespace",
				Labels: map[string]string{
					grovecorev1alpha1.LabelPartOfKey: "",
					"other-label":                    "some-value",
				},
			},
			expectedPGSName: "",
		},
		{
			// Tests extraction when multiple labels are present including part-of
			name: "multiple labels with part-of present",
			objectMeta: metav1.ObjectMeta{
				Name:      "multi-labeled-object",
				Namespace: "test-namespace",
				Labels: map[string]string{
					grovecorev1alpha1.LabelAppNameKey:   "my-app",
					grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
					grovecorev1alpha1.LabelPartOfKey:    "complex-podgangset-name",
					grovecorev1alpha1.LabelComponentKey: "component-type",
				},
			},
			expectedPGSName: "complex-podgangset-name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute the function under test
			actualPGSName := GetPodGangSetName(tc.objectMeta)

			// Verify the extracted name matches expectations
			assert.Equal(t, tc.expectedPGSName, actualPGSName, "Extracted PodGangSet name should match expected value")
		})
	}
}

// TestGetExpectedPCLQNamesGroupByOwner verifies that the function correctly categorizes
// PodClique names by their ownership (PodGangSet vs PodCliqueScalingGroup).
func TestGetExpectedPCLQNamesGroupByOwner(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the input PodGangSet containing PodClique and PodCliqueScalingGroup configurations
		pgs *grovecorev1alpha1.PodGangSet
		// expectedPGSCliques are the PodClique names expected to be owned directly by the PodGangSet
		expectedPGSCliques []string
		// expectedPCSGCliques are the PodClique names expected to be owned by PodCliqueScalingGroups
		expectedPCSGCliques []string
	}{
		{
			// Tests the case where no PodCliques are defined
			name: "no cliques defined",
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques:                      []*grovecorev1alpha1.PodCliqueTemplateSpec{},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{},
					},
				},
			},
			expectedPGSCliques:  []string{},
			expectedPCSGCliques: []string{},
		},
		{
			// Tests the case where all PodCliques are owned directly by PodGangSet (no scaling groups)
			name: "all cliques owned by PodGangSet",
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "frontend"},
							{Name: "backend"},
							{Name: "database"},
						},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{},
					},
				},
			},
			expectedPGSCliques:  []string{"frontend", "backend", "database"},
			expectedPCSGCliques: []string{},
		},
		{
			// Tests the case where all PodCliques are owned by scaling groups
			name: "all cliques owned by scaling groups",
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "worker-a"},
							{Name: "worker-b"},
							{Name: "storage"},
						},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "worker-group",
								CliqueNames: []string{"worker-a", "worker-b"},
							},
							{
								Name:        "storage-group",
								CliqueNames: []string{"storage"},
							},
						},
					},
				},
			},
			expectedPGSCliques:  []string{},
			expectedPCSGCliques: []string{"worker-a", "worker-b", "storage"},
		},
		{
			// Tests mixed ownership scenario with some PodCliques owned by PGS and others by scaling groups
			name: "mixed ownership - some PGS, some PCSG",
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "coordinator"},    // PGS-owned
							{Name: "worker-1"},       // PCSG-owned
							{Name: "worker-2"},       // PCSG-owned
							{Name: "monitor"},        // PGS-owned
							{Name: "cache"},          // PGS-owned
							{Name: "storage-main"},   // PCSG-owned
							{Name: "storage-backup"}, // PCSG-owned
						},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "worker-group",
								CliqueNames: []string{"worker-1", "worker-2"},
							},
							{
								Name:        "storage-group",
								CliqueNames: []string{"storage-main", "storage-backup"},
							},
						},
					},
				},
			},
			expectedPGSCliques:  []string{"coordinator", "monitor", "cache"},
			expectedPCSGCliques: []string{"worker-1", "worker-2", "storage-main", "storage-backup"},
		},
		{
			// Tests edge case where a scaling group has empty clique names
			name: "scaling group with empty clique names",
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "standalone-a"},
							{Name: "standalone-b"},
						},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "empty-group",
								CliqueNames: []string{},
							},
						},
					},
				},
			},
			expectedPGSCliques:  []string{"standalone-a", "standalone-b"},
			expectedPCSGCliques: []string{},
		},
		{
			// Tests scenario with overlapping clique names across multiple scaling groups
			name: "clique appears in multiple scaling groups",
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "shared-clique"},
							{Name: "unique-clique"},
						},
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:        "group-a",
								CliqueNames: []string{"shared-clique"},
							},
							{
								Name:        "group-b",
								CliqueNames: []string{"shared-clique"}, // Same clique in multiple groups
							},
						},
					},
				},
			},
			expectedPGSCliques:  []string{"unique-clique"},
			expectedPCSGCliques: []string{"shared-clique", "shared-clique"}, // Will appear twice as function doesn't deduplicate
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute the function under test
			actualPGSCliques, actualPCSGCliques := GetExpectedPCLQNamesGroupByOwner(tc.pgs)

			// Verify the PGS-owned cliques match expectations
			assert.ElementsMatch(t, tc.expectedPGSCliques, actualPGSCliques, "PGS-owned cliques should match expected values")
			assert.Len(t, actualPGSCliques, len(tc.expectedPGSCliques), "Number of PGS-owned cliques should match expected count")

			// Verify the PCSG-owned cliques match expectations
			assert.ElementsMatch(t, tc.expectedPCSGCliques, actualPCSGCliques, "PCSG-owned cliques should match expected values")
			assert.Len(t, actualPCSGCliques, len(tc.expectedPCSGCliques), "Number of PCSG-owned cliques should match expected count")
		})
	}
}
