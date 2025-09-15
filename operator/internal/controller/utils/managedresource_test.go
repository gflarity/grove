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
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// newOwnerReference creates a test OwnerReference with the specified kind, name, and controller status.
// Used as a helper function to create consistent owner references for testing.
func newOwnerReference(kind, name string, isController bool) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: "grove.io/v1alpha1",
		Kind:       kind,
		Name:       name,
		UID:        uuid.NewUUID(),
		Controller: ptr.To(isController),
	}
}

// TestHasExpectedOwner tests the HasExpectedOwner function which validates that a resource
// has exactly one owner reference matching the expected owner kind.
func TestHasExpectedOwner(t *testing.T) {
	testCases := []struct {
		// The expected owner kind to match against
		expectedOwnerKind string
		// The owner references to check
		ownerRefs []metav1.OwnerReference
		// Whether the function should return true
		expected bool
	}{
		{
			// Single owner reference matches the expected kind
			expectedOwnerKind: "PodGangSet",
			ownerRefs:         []metav1.OwnerReference{newOwnerReference("PodGangSet", "test-pgs", true)},
			expected:          true,
		},
		{
			// Single owner reference does not match the expected kind
			expectedOwnerKind: "PodGangSet",
			ownerRefs:         []metav1.OwnerReference{newOwnerReference("PodCliqueScalingGroup", "test-pcsg", true)},
			expected:          false,
		},
		{
			// Empty owner references slice should return false
			expectedOwnerKind: "PodGangSet",
			ownerRefs:         []metav1.OwnerReference{},
			expected:          false,
		},
		{
			// Nil owner references should return false
			expectedOwnerKind: "PodGangSet",
			ownerRefs:         nil,
			expected:          false,
		},
		{
			// Multiple owner references should return false (expects exactly one)
			expectedOwnerKind: "PodGangSet",
			ownerRefs: []metav1.OwnerReference{
				newOwnerReference("PodGangSet", "test-pgs", true),
				newOwnerReference("PodCliqueScalingGroup", "test-pcsg", false),
			},
			expected: false,
		},
	}

	for i, tc := range testCases {
		testName := "matching_owner"
		if len(tc.ownerRefs) == 0 {
			testName = "no_owners"
		} else if len(tc.ownerRefs) > 1 {
			testName = "multiple_owners"
		} else if tc.ownerRefs[0].Kind != tc.expectedOwnerKind {
			testName = "wrong_owner_kind"
		}
		t.Run(testName, func(t *testing.T) {
			result := HasExpectedOwner(tc.expectedOwnerKind, tc.ownerRefs)
			assert.Equal(t, tc.expected, result, "test case %d failed", i)
		})
	}
}

// TestIsManagedByGrove tests the IsManagedByGrove function which checks if a resource
// is managed by Grove by inspecting its labels for the standard managed-by label.
func TestIsManagedByGrove(t *testing.T) {
	testCases := []struct {
		// The labels map to check for Grove management
		labels map[string]string
		// Whether the function should return true
		expected bool
	}{
		{
			// Correct managed-by label value should return true
			labels: map[string]string{
				grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
			},
			expected: true,
		},
		{
			// Incorrect managed-by label value should return false
			labels: map[string]string{
				grovecorev1alpha1.LabelManagedByKey: "other-operator",
			},
			expected: false,
		},
		{
			// Missing managed-by label should return false
			labels: map[string]string{
				"app":     "test-app",
				"version": "v1.0",
			},
			expected: false,
		},
		{
			// Nil labels map should return false
			labels:   nil,
			expected: false,
		},
	}

	for i, tc := range testCases {
		testName := "managed_by_grove"
		if tc.labels == nil {
			testName = "nil_labels"
		} else if val, ok := tc.labels[grovecorev1alpha1.LabelManagedByKey]; !ok {
			testName = "missing_managed_by_label"
		} else if val != grovecorev1alpha1.LabelManagedByValue {
			testName = "incorrect_managed_by_value"
		}
		t.Run(testName, func(t *testing.T) {
			result := IsManagedByGrove(tc.labels)
			assert.Equal(t, tc.expected, result, "test case %d failed", i)
		})
	}
}

// TestIsManagedPodClique tests the IsManagedPodClique function which validates that a PodClique
// is both managed by Grove and has the expected owner kind.
func TestIsManagedPodClique(t *testing.T) {
	testCases := []struct {
		// The client object to check (should be a PodClique for positive cases)
		obj client.Object
		// The expected owner kinds to validate against
		expectedOwnerKinds []string
		// Whether the function should return true
		expected bool
	}{
		{
			// PodClique managed by Grove with correct owner should return true
			obj: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managed-pclq",
					Namespace: "test-ns",
					Labels: map[string]string{
						grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						newOwnerReference("PodGangSet", "test-pgs", true),
					},
				},
			},
			expectedOwnerKinds: []string{"PodGangSet"},
			expected:           true,
		},
		{
			// PodClique not managed by Grove should return false
			obj: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unmanaged-pclq",
					Namespace: "test-ns",
					Labels: map[string]string{
						grovecorev1alpha1.LabelManagedByKey: "other-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						newOwnerReference("PodGangSet", "test-pgs", true),
					},
				},
			},
			expectedOwnerKinds: []string{"PodGangSet"},
			expected:           false,
		},
		{
			// PodClique with wrong owner kind should return false
			obj: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrong-owner-pclq",
					Namespace: "test-ns",
					Labels: map[string]string{
						grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						newOwnerReference("WrongKind", "test-wrong", true),
					},
				},
			},
			expectedOwnerKinds: []string{"PodGangSet"},
			expected:           false,
		},
		{
			// Non-PodClique object should return false
			obj: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "different-ns",
					Labels: map[string]string{
						grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						newOwnerReference("PodGangSet", "test-pgs", true),
					},
				},
			},
			expectedOwnerKinds: []string{"PodGangSet"},
			expected:           false,
		},
		{
			// PodClique with no labels should return false
			obj: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-labels-pclq",
					Namespace: "test-ns",
					Labels:    nil,
					OwnerReferences: []metav1.OwnerReference{
						newOwnerReference("PodGangSet", "test-pgs", true),
					},
				},
			},
			expectedOwnerKinds: []string{"PodGangSet"},
			expected:           false,
		},
	}

	for i, tc := range testCases {
		testName := "managed_podclique_correct_owner"
		if _, ok := tc.obj.(*grovecorev1alpha1.PodClique); !ok {
			testName = "non_podclique_object"
		} else if tc.obj.GetLabels() == nil || tc.obj.GetLabels()[grovecorev1alpha1.LabelManagedByKey] != grovecorev1alpha1.LabelManagedByValue {
			testName = "not_managed_by_grove"
		} else if len(tc.obj.GetOwnerReferences()) == 0 {
			testName = "no_owner_references"
		} else if len(tc.obj.GetOwnerReferences()) > 1 {
			testName = "multiple_owner_references"
		} else {
			// Check if owner kind matches expected
			ownerKind := tc.obj.GetOwnerReferences()[0].Kind
			found := false
			for _, expectedKind := range tc.expectedOwnerKinds {
				if ownerKind == expectedKind {
					found = true
					break
				}
			}
			if !found {
				testName = "wrong_owner_kind"
			}
		}
		t.Run(testName, func(t *testing.T) {
			result := IsManagedPodClique(tc.obj, tc.expectedOwnerKinds...)
			assert.Equal(t, tc.expected, result, "test case %d failed", i)
		})
	}
}
