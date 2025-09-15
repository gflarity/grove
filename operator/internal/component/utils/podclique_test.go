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
	"time"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Test constants
const (
	testNamespacePodClique = "test-namespace"
	testPGSNamePodClique   = "test-pgs"
	testPCSGNamePodClique  = "test-pcsg"
)

// TestGetPCLQsByOwner tests the retrieval of PodCliques by owner reference.
// This function validates filtering PodCliques that belong to a specific owner
// and match the provided selector labels.
func TestGetPCLQsByOwner(t *testing.T) {
	type testCase struct {
		// name describes the test scenario being executed
		name string
		// existingPCLQs are the PodCliques that exist in the fake client
		existingPCLQs []grovecorev1alpha1.PodClique
		// ownerKind is the Kind field of the owner reference to filter by
		ownerKind string
		// ownerObjectKey contains the name and namespace of the owner
		ownerObjectKey client.ObjectKey
		// selectorLabels are the labels used to filter PodCliques before owner filtering
		selectorLabels map[string]string
		// expectedPCLQNames are the names of PodCliques expected in the result
		expectedPCLQNames []string
		// expectError indicates whether an error should be returned
		expectError bool
	}

	tests := []testCase{
		{
			// Test filtering by owner when multiple PodCliques exist with matching labels
			name: "filter by owner with matching labels",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "test"},
					createOwnerRef("PodGangSet", "owner-1")),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{"app": "test"},
					createOwnerRef("PodGangSet", "owner-2")),
				createTestPodClique("pclq-3", testNamespacePodClique,
					map[string]string{"app": "test"},
					createOwnerRef("PodGangSet", "owner-1")),
			},
			ownerKind:         "PodGangSet",
			ownerObjectKey:    client.ObjectKey{Name: "owner-1", Namespace: testNamespacePodClique},
			selectorLabels:    map[string]string{"app": "test"},
			expectedPCLQNames: []string{"pclq-1", "pclq-3"},
			expectError:       false,
		},
		{
			// Test when no PodCliques match the selector labels
			name: "no matching labels",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "other"},
					createOwnerRef("PodGangSet", "owner-1")),
			},
			ownerKind:         "PodGangSet",
			ownerObjectKey:    client.ObjectKey{Name: "owner-1", Namespace: testNamespacePodClique},
			selectorLabels:    map[string]string{"app": "test"},
			expectedPCLQNames: []string{},
			expectError:       false,
		},
		{
			// Test when PodCliques match labels but have different owner kind
			name: "matching labels but different owner kind",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "test"},
					createOwnerRef("PodCliqueScalingGroup", "owner-1")),
			},
			ownerKind:         "PodGangSet",
			ownerObjectKey:    client.ObjectKey{Name: "owner-1", Namespace: testNamespacePodClique},
			selectorLabels:    map[string]string{"app": "test"},
			expectedPCLQNames: []string{},
			expectError:       false,
		},
		{
			// Test when PodCliques have no owner references
			name: "no owner references",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "test"}, nil),
			},
			ownerKind:         "PodGangSet",
			ownerObjectKey:    client.ObjectKey{Name: "owner-1", Namespace: testNamespacePodClique},
			selectorLabels:    map[string]string{"app": "test"},
			expectedPCLQNames: []string{},
			expectError:       false,
		},
		{
			// Test empty selector returns all PodCliques matching owner
			name: "empty selector with owner match",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "test"},
					createOwnerRef("PodGangSet", "owner-1")),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{"env": "prod"},
					createOwnerRef("PodGangSet", "owner-1")),
			},
			ownerKind:         "PodGangSet",
			ownerObjectKey:    client.ObjectKey{Name: "owner-1", Namespace: testNamespacePodClique},
			selectorLabels:    map[string]string{},
			expectedPCLQNames: []string{"pclq-1", "pclq-2"},
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client with test PodCliques
			fakeClient := testutils.SetupFakeClient(convertToClientObjects(tt.existingPCLQs)...)
			ctx := testutils.SetupTestContext()

			// Execute the function under test
			result, err := GetPCLQsByOwner(ctx, fakeClient, tt.ownerKind, tt.ownerObjectKey, tt.selectorLabels)

			// Validate results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				actualNames := extractPodCliqueNames(result)
				assert.ElementsMatch(t, tt.expectedPCLQNames, actualNames)
			}
		})
	}
}

// TestGetPCLQsMatchingLabels tests the retrieval of PodCliques by label selector.
// This function validates that only PodCliques with matching labels are returned
// from the specified namespace.
func TestGetPCLQsMatchingLabels(t *testing.T) {
	type testCase struct {
		// name describes the test scenario being executed
		name string
		// existingPCLQs are the PodCliques that exist in the fake client
		existingPCLQs []grovecorev1alpha1.PodClique
		// namespace is the namespace to search in
		namespace string
		// selectorLabels are the labels used to filter PodCliques
		selectorLabels map[string]string
		// expectedPCLQNames are the names of PodCliques expected in the result
		expectedPCLQNames []string
		// expectError indicates whether an error should be returned
		expectError bool
	}

	tests := []testCase{
		{
			// Test retrieving PodCliques with specific label values
			name: "matching single label",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "test", "env": "prod"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{"app": "test", "env": "dev"}, nil),
				createTestPodClique("pclq-3", testNamespacePodClique,
					map[string]string{"app": "other"}, nil),
			},
			namespace:         testNamespacePodClique,
			selectorLabels:    map[string]string{"app": "test"},
			expectedPCLQNames: []string{"pclq-1", "pclq-2"},
			expectError:       false,
		},
		{
			// Test retrieving PodCliques with multiple label requirements
			name: "matching multiple labels",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "test", "env": "prod"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{"app": "test", "env": "dev"}, nil),
			},
			namespace:         testNamespacePodClique,
			selectorLabels:    map[string]string{"app": "test", "env": "prod"},
			expectedPCLQNames: []string{"pclq-1"},
			expectError:       false,
		},
		{
			// Test when no PodCliques match the label selector
			name: "no matching labels",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "other"}, nil),
			},
			namespace:         testNamespacePodClique,
			selectorLabels:    map[string]string{"app": "test"},
			expectedPCLQNames: []string{},
			expectError:       false,
		},
		{
			// Test with empty label selector returns all PodCliques in namespace
			name: "empty selector",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "test"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{"env": "prod"}, nil),
			},
			namespace:         testNamespacePodClique,
			selectorLabels:    map[string]string{},
			expectedPCLQNames: []string{"pclq-1", "pclq-2"},
			expectError:       false,
		},
		{
			// Test that PodCliques from different namespace are not returned
			name: "different namespace filtering",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{"app": "test"}, nil),
				createTestPodClique("pclq-2", "other-namespace",
					map[string]string{"app": "test"}, nil),
			},
			namespace:         testNamespacePodClique,
			selectorLabels:    map[string]string{"app": "test"},
			expectedPCLQNames: []string{"pclq-1"},
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client with test PodCliques
			fakeClient := testutils.SetupFakeClient(convertToClientObjects(tt.existingPCLQs)...)
			ctx := testutils.SetupTestContext()

			// Execute the function under test
			result, err := GetPCLQsMatchingLabels(ctx, fakeClient, tt.namespace, tt.selectorLabels)

			// Validate results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				actualNames := extractPodCliqueNames(result)
				assert.ElementsMatch(t, tt.expectedPCLQNames, actualNames)
			}
		})
	}
}

// TestGetPCLQsByNames tests the retrieval of PodCliques by their fully qualified names.
// This function validates proper handling of existing and non-existing PodCliques,
// returning found PodCliques and tracking not found ones separately.
func TestGetPCLQsByNames(t *testing.T) {
	type testCase struct {
		// name describes the test scenario being executed
		name string
		// existingPCLQs are the PodCliques that exist in the fake client
		existingPCLQs []grovecorev1alpha1.PodClique
		// namespace is the namespace to search in
		namespace string
		// pclqFQNs are the fully qualified names to search for
		pclqFQNs []string
		// expectedFoundNames are the names of PodCliques expected to be found
		expectedFoundNames []string
		// expectedNotFoundNames are the names expected to be not found
		expectedNotFoundNames []string
		// expectError indicates whether an error should be returned
		expectError bool
	}

	tests := []testCase{
		{
			// Test finding all requested PodCliques
			name: "all PodCliques found",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique, nil, nil),
				createTestPodClique("pclq-2", testNamespacePodClique, nil, nil),
				createTestPodClique("pclq-3", testNamespacePodClique, nil, nil),
			},
			namespace:             testNamespacePodClique,
			pclqFQNs:              []string{"pclq-1", "pclq-2"},
			expectedFoundNames:    []string{"pclq-1", "pclq-2"},
			expectedNotFoundNames: []string{},
			expectError:           false,
		},
		{
			// Test when some PodCliques are not found
			name: "some PodCliques not found",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique, nil, nil),
			},
			namespace:             testNamespacePodClique,
			pclqFQNs:              []string{"pclq-1", "pclq-nonexistent"},
			expectedFoundNames:    []string{"pclq-1"},
			expectedNotFoundNames: []string{"pclq-nonexistent"},
			expectError:           false,
		},
		{
			// Test when no PodCliques are found
			name: "no PodCliques found",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique, nil, nil),
			},
			namespace:             testNamespacePodClique,
			pclqFQNs:              []string{"pclq-nonexistent-1", "pclq-nonexistent-2"},
			expectedFoundNames:    []string{},
			expectedNotFoundNames: []string{"pclq-nonexistent-1", "pclq-nonexistent-2"},
			expectError:           false,
		},
		{
			// Test with empty search list
			name: "empty search list",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique, nil, nil),
			},
			namespace:             testNamespacePodClique,
			pclqFQNs:              []string{},
			expectedFoundNames:    []string{},
			expectedNotFoundNames: []string{},
			expectError:           false,
		},
		{
			// Test searching in different namespace doesn't find PodCliques
			name: "wrong namespace",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", "other-namespace", nil, nil),
			},
			namespace:             testNamespacePodClique,
			pclqFQNs:              []string{"pclq-1"},
			expectedFoundNames:    []string{},
			expectedNotFoundNames: []string{"pclq-1"},
			expectError:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client with test PodCliques
			fakeClient := testutils.SetupFakeClient(convertToClientObjects(tt.existingPCLQs)...)
			ctx := testutils.SetupTestContext()

			// Execute the function under test
			foundPCLQs, notFoundNames, err := GetPCLQsByNames(ctx, fakeClient, tt.namespace, tt.pclqFQNs)

			// Validate results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				actualFoundNames := extractPodCliqueNames(foundPCLQs)
				assert.ElementsMatch(t, tt.expectedFoundNames, actualFoundNames)
				assert.ElementsMatch(t, tt.expectedNotFoundNames, notFoundNames)
			}
		})
	}
}

// TestGetPodCliquesWithParentPGS tests the retrieval of PodCliques directly managed by a PodGangSet.
// This function validates that only PodCliques with the correct parent PGS labels are returned,
// excluding those managed by PodCliqueScalingGroups.
func TestGetPodCliquesWithParentPGS(t *testing.T) {
	type testCase struct {
		// name describes the test scenario being executed
		name string
		// existingPCLQs are the PodCliques that exist in the fake client
		existingPCLQs []grovecorev1alpha1.PodClique
		// pgsObjKey contains the name and namespace of the PodGangSet
		pgsObjKey client.ObjectKey
		// expectedPCLQNames are the names of PodCliques expected in the result
		expectedPCLQNames []string
		// expectError indicates whether an error should be returned
		expectError bool
	}

	// Expected labels for PGS-managed PodCliques
	pgsLabels := map[string]string{
		grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
		grovecorev1alpha1.LabelPartOfKey:    testPGSNamePodClique,
		grovecorev1alpha1.LabelComponentKey: grovecorev1alpha1.LabelComponentPGSPodCliqueValue,
	}

	// Labels for PCSG-managed PodCliques (should be excluded)
	pcsgLabels := map[string]string{
		grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
		grovecorev1alpha1.LabelPartOfKey:    testPGSNamePodClique,
		grovecorev1alpha1.LabelComponentKey: grovecorev1alpha1.LabelComponentPCSGPodCliqueValue,
	}

	tests := []testCase{
		{
			// Test retrieving PodCliques directly managed by PGS
			name: "PGS-managed PodCliques found",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pgs-pclq-1", testNamespacePodClique, pgsLabels, nil),
				createTestPodClique("pgs-pclq-2", testNamespacePodClique, pgsLabels, nil),
				createTestPodClique("pcsg-pclq-1", testNamespacePodClique, pcsgLabels, nil),
			},
			pgsObjKey:         client.ObjectKey{Name: testPGSNamePodClique, Namespace: testNamespacePodClique},
			expectedPCLQNames: []string{"pgs-pclq-1", "pgs-pclq-2"},
			expectError:       false,
		},
		{
			// Test when no PGS-managed PodCliques exist
			name: "no PGS-managed PodCliques",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pcsg-pclq-1", testNamespacePodClique, pcsgLabels, nil),
				createTestPodClique("other-pclq", testNamespacePodClique,
					map[string]string{"app": "other"}, nil),
			},
			pgsObjKey:         client.ObjectKey{Name: testPGSNamePodClique, Namespace: testNamespacePodClique},
			expectedPCLQNames: []string{},
			expectError:       false,
		},
		{
			// Test with different PGS name doesn't match
			name: "different PGS name",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pgs-pclq-1", testNamespacePodClique,
					map[string]string{
						grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
						grovecorev1alpha1.LabelPartOfKey:    "other-pgs",
						grovecorev1alpha1.LabelComponentKey: grovecorev1alpha1.LabelComponentPGSPodCliqueValue,
					}, nil),
			},
			pgsObjKey:         client.ObjectKey{Name: testPGSNamePodClique, Namespace: testNamespacePodClique},
			expectedPCLQNames: []string{},
			expectError:       false,
		},
		{
			// Test with different namespace doesn't match
			name: "different namespace",
			existingPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pgs-pclq-1", "other-namespace", pgsLabels, nil),
			},
			pgsObjKey:         client.ObjectKey{Name: testPGSNamePodClique, Namespace: testNamespacePodClique},
			expectedPCLQNames: []string{},
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client with test PodCliques
			fakeClient := testutils.SetupFakeClient(convertToClientObjects(tt.existingPCLQs)...)
			ctx := testutils.SetupTestContext()

			// Execute the function under test
			result, err := GetPodCliquesWithParentPGS(ctx, fakeClient, tt.pgsObjKey)

			// Validate results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				actualNames := extractPodCliqueNames(result)
				assert.ElementsMatch(t, tt.expectedPCLQNames, actualNames)
			}
		})
	}
}

// TestGroupPCLQsByPodGangName tests the grouping of PodCliques by PodGang name label.
// This function validates that PodCliques are correctly grouped by their PodGang label,
// and those without the label are excluded from the result.
func TestGroupPCLQsByPodGangName(t *testing.T) {
	type testCase struct {
		// name describes the test scenario being executed
		name string
		// inputPCLQs are the PodCliques to group
		inputPCLQs []grovecorev1alpha1.PodClique
		// expectedGroups maps PodGang names to expected PodClique names
		expectedGroups map[string][]string
	}

	tests := []testCase{
		{
			// Test grouping PodCliques with different PodGang labels
			name: "group by PodGang name",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGang: "gang-a"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGang: "gang-a"}, nil),
				createTestPodClique("pclq-3", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGang: "gang-b"}, nil),
			},
			expectedGroups: map[string][]string{
				"gang-a": {"pclq-1", "pclq-2"},
				"gang-b": {"pclq-3"},
			},
		},
		{
			// Test that PodCliques without PodGang label are excluded
			name: "exclude PodCliques without PodGang label",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGang: "gang-a"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{"other": "label"}, nil),
			},
			expectedGroups: map[string][]string{
				"gang-a": {"pclq-1"},
			},
		},
		{
			// Test with empty input
			name:           "empty input",
			inputPCLQs:     []grovecorev1alpha1.PodClique{},
			expectedGroups: map[string][]string{},
		},
		{
			// Test with single PodGang
			name: "single PodGang",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGang: "gang-single"}, nil),
			},
			expectedGroups: map[string][]string{
				"gang-single": {"pclq-1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			result := GroupPCLQsByPodGangName(tt.inputPCLQs)

			// Validate results
			assert.Equal(t, len(tt.expectedGroups), len(result))
			for expectedKey, expectedNames := range tt.expectedGroups {
				actualPCLQs, exists := result[expectedKey]
				assert.True(t, exists, "Expected group %s not found", expectedKey)
				actualNames := extractPodCliqueNames(actualPCLQs)
				assert.ElementsMatch(t, expectedNames, actualNames)
			}
		})
	}
}

// TestGroupPCLQsByPCSGReplicaIndex tests the grouping of PodCliques by PCSG replica index.
// This function validates that PodCliques are correctly grouped by their PCSG replica index label,
// and those without the label are excluded from the result.
func TestGroupPCLQsByPCSGReplicaIndex(t *testing.T) {
	type testCase struct {
		// name describes the test scenario being executed
		name string
		// inputPCLQs are the PodCliques to group
		inputPCLQs []grovecorev1alpha1.PodClique
		// expectedGroups maps PCSG replica indices to expected PodClique names
		expectedGroups map[string][]string
	}

	tests := []testCase{
		{
			// Test grouping PodCliques with different PCSG replica indices
			name: "group by PCSG replica index",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: "0"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: "0"}, nil),
				createTestPodClique("pclq-3", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: "1"}, nil),
			},
			expectedGroups: map[string][]string{
				"0": {"pclq-1", "pclq-2"},
				"1": {"pclq-3"},
			},
		},
		{
			// Test that PodCliques without PCSG replica index label are excluded
			name: "exclude PodCliques without PCSG replica index label",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: "0"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{"other": "label"}, nil),
			},
			expectedGroups: map[string][]string{
				"0": {"pclq-1"},
			},
		},
		{
			// Test with empty input
			name:           "empty input",
			inputPCLQs:     []grovecorev1alpha1.PodClique{},
			expectedGroups: map[string][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			result := GroupPCLQsByPCSGReplicaIndex(tt.inputPCLQs)

			// Validate results
			assert.Equal(t, len(tt.expectedGroups), len(result))
			for expectedKey, expectedNames := range tt.expectedGroups {
				actualPCLQs, exists := result[expectedKey]
				assert.True(t, exists, "Expected group %s not found", expectedKey)
				actualNames := extractPodCliqueNames(actualPCLQs)
				assert.ElementsMatch(t, expectedNames, actualNames)
			}
		})
	}
}

// TestGroupPCLQsByPGSReplicaIndex tests the grouping of PodCliques by PGS replica index.
// This function validates that PodCliques are correctly grouped by their PGS replica index label,
// and those without the label are excluded from the result.
func TestGroupPCLQsByPGSReplicaIndex(t *testing.T) {
	type testCase struct {
		// name describes the test scenario being executed
		name string
		// inputPCLQs are the PodCliques to group
		inputPCLQs []grovecorev1alpha1.PodClique
		// expectedGroups maps PGS replica indices to expected PodClique names
		expectedGroups map[string][]string
	}

	tests := []testCase{
		{
			// Test grouping PodCliques with different PGS replica indices
			name: "group by PGS replica index",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0"}, nil),
				createTestPodClique("pclq-3", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGangSetReplicaIndex: "1"}, nil),
			},
			expectedGroups: map[string][]string{
				"0": {"pclq-1", "pclq-2"},
				"1": {"pclq-3"},
			},
		},
		{
			// Test that PodCliques without PGS replica index label are excluded
			name: "exclude PodCliques without PGS replica index label",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique,
					map[string]string{grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0"}, nil),
				createTestPodClique("pclq-2", testNamespacePodClique,
					map[string]string{"other": "label"}, nil),
			},
			expectedGroups: map[string][]string{
				"0": {"pclq-1"},
			},
		},
		{
			// Test with empty input
			name:           "empty input",
			inputPCLQs:     []grovecorev1alpha1.PodClique{},
			expectedGroups: map[string][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			result := GroupPCLQsByPGSReplicaIndex(tt.inputPCLQs)

			// Validate results
			assert.Equal(t, len(tt.expectedGroups), len(result))
			for expectedKey, expectedNames := range tt.expectedGroups {
				actualPCLQs, exists := result[expectedKey]
				assert.True(t, exists, "Expected group %s not found", expectedKey)
				actualNames := extractPodCliqueNames(actualPCLQs)
				assert.ElementsMatch(t, expectedNames, actualNames)
			}
		})
	}
}

// TestGetMinAvailableBreachedPCLQInfo tests the retrieval of PodCliques with breached MinAvailable
// and calculation of termination delay wait times. This function validates proper handling of
// condition timestamps and duration calculations.
func TestGetMinAvailableBreachedPCLQInfo(t *testing.T) {
	baseTime := time.Now()
	terminationDelay := 5 * time.Minute

	type testCase struct {
		// name describes the test scenario being executed
		name string
		// inputPCLQs are the PodCliques to analyze
		inputPCLQs []grovecorev1alpha1.PodClique
		// terminationDelay is the duration before breach leads to termination
		terminationDelay time.Duration
		// since is the reference time for duration calculations
		since time.Time
		// expectedCandidateNames are the names of PodCliques expected as candidates
		expectedCandidateNames []string
		// expectedWaitDuration is the expected shortest wait duration
		expectedWaitDuration time.Duration
	}

	tests := []testCase{
		{
			// Test PodCliques with MinAvailableBreached condition set to true
			name: "breached PodCliques found",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createPodCliqueWithCondition("pclq-1", testNamespacePodClique,
					grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					metav1.ConditionTrue,
					baseTime.Add(-2*time.Minute)), // Breached 2 minutes ago
				createPodCliqueWithCondition("pclq-2", testNamespacePodClique,
					grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					metav1.ConditionTrue,
					baseTime.Add(-1*time.Minute)), // Breached 1 minute ago
			},
			terminationDelay:       terminationDelay,
			since:                  baseTime,
			expectedCandidateNames: []string{"pclq-1", "pclq-2"},
			expectedWaitDuration:   3 * time.Minute, // Shortest: 5min - 2min = 3min
		},
		{
			// Test PodCliques with MinAvailableBreached condition set to false
			name: "no breached PodCliques",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createPodCliqueWithCondition("pclq-1", testNamespacePodClique,
					grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					metav1.ConditionFalse,
					baseTime.Add(-2*time.Minute)),
			},
			terminationDelay:       terminationDelay,
			since:                  baseTime,
			expectedCandidateNames: []string{},
			expectedWaitDuration:   0,
		},
		{
			// Test PodCliques without MinAvailableBreached condition
			name: "no breach condition",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createTestPodClique("pclq-1", testNamespacePodClique, nil, nil),
			},
			terminationDelay:       terminationDelay,
			since:                  baseTime,
			expectedCandidateNames: []string{},
			expectedWaitDuration:   0,
		},
		{
			// Test single PodClique with recent breach
			name: "single recent breach",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createPodCliqueWithCondition("pclq-1", testNamespacePodClique,
					grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					metav1.ConditionTrue,
					baseTime.Add(-30*time.Second)), // Breached 30 seconds ago
			},
			terminationDelay:       terminationDelay,
			since:                  baseTime,
			expectedCandidateNames: []string{"pclq-1"},
			expectedWaitDuration:   4*time.Minute + 30*time.Second, // 5min - 30sec
		},
		{
			// Test breach that occurred after termination delay (negative wait time)
			name: "breach beyond termination delay",
			inputPCLQs: []grovecorev1alpha1.PodClique{
				createPodCliqueWithCondition("pclq-1", testNamespacePodClique,
					grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					metav1.ConditionTrue,
					baseTime.Add(-10*time.Minute)), // Breached 10 minutes ago
			},
			terminationDelay:       terminationDelay,
			since:                  baseTime,
			expectedCandidateNames: []string{"pclq-1"},
			expectedWaitDuration:   -5 * time.Minute, // 5min - 10min = -5min
		},
		{
			// Test empty input
			name:                   "empty input",
			inputPCLQs:             []grovecorev1alpha1.PodClique{},
			terminationDelay:       terminationDelay,
			since:                  baseTime,
			expectedCandidateNames: []string{},
			expectedWaitDuration:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			candidateNames, waitDuration := GetMinAvailableBreachedPCLQInfo(
				tt.inputPCLQs, tt.terminationDelay, tt.since)

			// Validate results
			assert.ElementsMatch(t, tt.expectedCandidateNames, candidateNames)
			assert.Equal(t, tt.expectedWaitDuration, waitDuration)
		})
	}
}

// Helper functions for test setup

// createTestPodClique creates a test PodClique with the specified parameters.
func createTestPodClique(name, namespace string, labels map[string]string, ownerRef *metav1.OwnerReference) grovecorev1alpha1.PodClique {
	pclq := grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: grovecorev1alpha1.PodCliqueSpec{
			RoleName: "test-role",
			Replicas: 3,
		},
	}

	if ownerRef != nil {
		pclq.OwnerReferences = []metav1.OwnerReference{*ownerRef}
	}

	return pclq
}

// createOwnerRef creates a test owner reference with the specified kind and name.
func createOwnerRef(kind, name string) *metav1.OwnerReference {
	return &metav1.OwnerReference{
		APIVersion: "grove.io/v1alpha1",
		Kind:       kind,
		Name:       name,
		UID:        types.UID("test-uid-" + name),
	}
}

// createPodCliqueWithCondition creates a test PodClique with a specific condition.
func createPodCliqueWithCondition(name, namespace, conditionType string, status metav1.ConditionStatus, lastTransitionTime time.Time) grovecorev1alpha1.PodClique {
	pclq := createTestPodClique(name, namespace, nil, nil)
	pclq.Status.Conditions = []metav1.Condition{
		{
			Type:               conditionType,
			Status:             status,
			LastTransitionTime: metav1.NewTime(lastTransitionTime),
			Reason:             "TestReason",
			Message:            "Test condition",
		},
	}
	return pclq
}

// convertToClientObjects converts PodClique slice to client.Object slice for fake client setup.
func convertToClientObjects(pclqs []grovecorev1alpha1.PodClique) []client.Object {
	objects := make([]client.Object, len(pclqs))
	for i, pclq := range pclqs {
		pclq := pclq // Create copy to avoid loop variable issues
		objects[i] = &pclq
	}
	return objects
}

// extractPodCliqueNames extracts names from a slice of PodCliques.
func extractPodCliqueNames(pclqs []grovecorev1alpha1.PodClique) []string {
	names := make([]string, len(pclqs))
	for i, pclq := range pclqs {
		names[i] = pclq.Name
	}
	return names
}
