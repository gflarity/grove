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

package podgangset

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestMapPodCliqueToPodGangSet validates PodClique to PodGangSet mapping for various scenarios.
func TestMapPodCliqueToPodGangSet(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodClique setup function that creates the test resource
		setupPodClique func() *grovecorev1alpha1.PodClique
		// Expected reconcile requests
		expectedRequests []reconcile.Request
	}{
		{
			// Valid PodClique with PodGangSet owner reference
			name: "valid PodClique maps to PodGangSet",
			setupPodClique: func() *grovecorev1alpha1.PodClique {
				pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pgs-0-worker", "test-namespace", 0).Build()
				if pclq.Labels == nil {
					pclq.Labels = make(map[string]string)
				}
				pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
				return pclq
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "test-pgs", Namespace: "test-namespace"}},
			},
		},
		{
			// PodClique without PodGangSet owner reference
			name: "PodClique without PGS owner returns empty",
			setupPodClique: func() *grovecorev1alpha1.PodClique {
				// Create a PodClique without PodGangSet owner reference
				pclq := &grovecorev1alpha1.PodClique{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "standalone-pclq",
						Namespace: "test-namespace",
						// No owner references - this is a standalone PodClique
					},
					Spec: grovecorev1alpha1.PodCliqueSpec{
						Replicas:     1,
						MinAvailable: ptr.To(int32(1)),
					},
				}
				return pclq
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "", Namespace: "test-namespace"}},
			},
		},
		{
			// Nil object returns empty requests
			name: "nil object returns empty",
			setupPodClique: func() *grovecorev1alpha1.PodClique {
				return nil
			},
			expectedRequests: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mapper := mapPodCliqueToPodGangSet()

			var requests []reconcile.Request
			if tt.setupPodClique() != nil {
				requests = mapper(context.Background(), tt.setupPodClique())
			} else {
				// Test with wrong object type
				requests = mapper(context.Background(), &grovecorev1alpha1.PodGangSet{})
			}

			assert.Equal(t, tt.expectedRequests, requests)
		})
	}
}

// TestMapPodCliqueScaleGroupToPodGangSet validates PodCliqueScalingGroup to PodGangSet mapping for various scenarios.
func TestMapPodCliqueScaleGroupToPodGangSet(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodCliqueScalingGroup setup function that creates the test resource
		setupPCSG func() *grovecorev1alpha1.PodCliqueScalingGroup
		// Expected reconcile requests
		expectedRequests []reconcile.Request
	}{
		{
			// Valid PCSG with PodGangSet owner reference
			name: "valid PCSG maps to PodGangSet",
			setupPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pgs-0-compute", "test-namespace", "test-pgs", 0).Build()
				if pcsg.Labels == nil {
					pcsg.Labels = make(map[string]string)
				}
				pcsg.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
				return pcsg
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "test-pgs", Namespace: "test-namespace"}},
			},
		},
		{
			// PCSG without PodGangSet owner reference
			name: "PCSG without PGS owner returns empty",
			setupPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
				return testutils.NewPodCliqueScalingGroupBuilder("standalone-pcsg", "test-namespace", "", 0).Build()
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "", Namespace: "test-namespace"}},
			},
		},
		{
			// Nil object returns empty requests
			name: "nil object returns empty",
			setupPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
				return nil
			},
			expectedRequests: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mapper := mapPodCliqueScaleGroupToPodGangSet()

			var requests []reconcile.Request
			if tt.setupPCSG() != nil {
				requests = mapper(context.Background(), tt.setupPCSG())
			} else {
				// Test with wrong object type
				requests = mapper(context.Background(), &grovecorev1alpha1.PodGangSet{})
			}

			assert.Equal(t, tt.expectedRequests, requests)
		})
	}
}

// TestPodCliquePredicate validates PodClique event filtering for various scenarios.
func TestPodCliquePredicate(t *testing.T) {
	predicate := podCliquePredicate()

	t.Run("CreateFunc", func(t *testing.T) {
		tests := []struct {
			// Test case name describing the scenario being tested
			name string
			// Expected result from predicate
			expected bool
		}{
			{
				// Create events should always be ignored
				name:     "create events always ignored",
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
				createEvent := event.CreateEvent{Object: pclq}

				result := predicate.Create(createEvent)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("DeleteFunc", func(t *testing.T) {
		tests := []struct {
			// Test case name describing the scenario being tested
			name string
			// PodClique setup function that creates the test resource
			setupPodClique func() *grovecorev1alpha1.PodClique
			// Expected result from predicate
			expected bool
		}{
			{
				// Grove-managed PodClique deletion should be processed
				name: "grove managed PodClique deletion processed",
				setupPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					return pclq
				},
				expected: true,
			},
			{
				// Non-Grove-managed PodClique deletion should be ignored
				name: "non-grove managed PodClique deletion ignored",
				setupPodClique: func() *grovecorev1alpha1.PodClique {
					// Create a PodClique that is NOT managed by Grove (no Grove labels or owner references)
					return &grovecorev1alpha1.PodClique{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "external-pclq",
							Namespace: "test-namespace",
							Labels: map[string]string{
								"app": "external-app", // Non-Grove labels
							},
							// No Grove owner references
						},
						Spec: grovecorev1alpha1.PodCliqueSpec{
							Replicas:     1,
							MinAvailable: ptr.To(int32(1)),
						},
					}
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				pclq := tt.setupPodClique()
				deleteEvent := event.DeleteEvent{Object: pclq}

				result := predicate.Delete(deleteEvent)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("UpdateFunc", func(t *testing.T) {
		tests := []struct {
			// Test case name describing the scenario being tested
			name string
			// Old PodClique setup function
			setupOldPodClique func() *grovecorev1alpha1.PodClique
			// New PodClique setup function
			setupNewPodClique func() *grovecorev1alpha1.PodClique
			// Expected result from predicate
			expected bool
		}{
			{
				// Spec change on Grove-managed PodClique should be processed
				name: "spec change on grove managed PodClique processed",
				setupOldPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					pclq.Generation = 1
					return pclq
				},
				setupNewPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					pclq.Generation = 2
					return pclq
				},
				expected: true,
			},
			{
				// Status change on Grove-managed PodClique should be processed
				name: "status change on grove managed PodClique processed",
				setupOldPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					pclq.Status.ReadyReplicas = 0
					return pclq
				},
				setupNewPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					pclq.Status.ReadyReplicas = 1
					return pclq
				},
				expected: true,
			},
			{
				// MinAvailableBreached condition change should be processed
				name: "MinAvailableBreached condition change processed",
				setupOldPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					return pclq
				},
				setupNewPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					meta.SetStatusCondition(&pclq.Status.Conditions, metav1.Condition{
						Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
						Status: metav1.ConditionTrue,
						Reason: "TestReason",
					})
					return pclq
				},
				expected: true,
			},
			{
				// No relevant changes should be ignored
				name: "no relevant changes ignored",
				setupOldPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					return pclq
				},
				setupNewPodClique: func() *grovecorev1alpha1.PodClique {
					pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
					if pclq.Labels == nil {
						pclq.Labels = make(map[string]string)
					}
					pclq.Labels[grovecorev1alpha1.LabelPartOfKey] = "test-pgs"
					return pclq
				},
				expected: false,
			},
			{
				// Non-Grove-managed PodClique should be ignored
				name: "non-grove managed PodClique ignored",
				setupOldPodClique: func() *grovecorev1alpha1.PodClique {
					// Create a PodClique that is NOT managed by Grove (no Grove labels or owner references)
					return &grovecorev1alpha1.PodClique{
						ObjectMeta: metav1.ObjectMeta{
							Name:       "external-pclq",
							Namespace:  "test-namespace",
							Generation: 1,
							Labels: map[string]string{
								"app": "external-app", // Non-Grove labels
							},
							// No Grove owner references
						},
						Spec: grovecorev1alpha1.PodCliqueSpec{
							Replicas:     1,
							MinAvailable: ptr.To(int32(1)),
						},
					}
				},
				setupNewPodClique: func() *grovecorev1alpha1.PodClique {
					// Create a PodClique that is NOT managed by Grove (no Grove labels or owner references)
					return &grovecorev1alpha1.PodClique{
						ObjectMeta: metav1.ObjectMeta{
							Name:       "external-pclq",
							Namespace:  "test-namespace",
							Generation: 2, // Generation changed to trigger spec change
							Labels: map[string]string{
								"app": "external-app", // Non-Grove labels
							},
							// No Grove owner references
						},
						Spec: grovecorev1alpha1.PodCliqueSpec{
							Replicas:     1,
							MinAvailable: ptr.To(int32(1)),
						},
					}
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				oldPclq := tt.setupOldPodClique()
				newPclq := tt.setupNewPodClique()
				updateEvent := event.UpdateEvent{ObjectOld: oldPclq, ObjectNew: newPclq}

				result := predicate.Update(updateEvent)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("GenericFunc", func(t *testing.T) {
		tests := []struct {
			// Test case name describing the scenario being tested
			name string
			// Expected result from predicate
			expected bool
		}{
			{
				// Generic events should always be ignored
				name:     "generic events always ignored",
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
				genericEvent := event.GenericEvent{Object: pclq}

				result := predicate.Generic(genericEvent)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

// TestPodCliqueScalingGroupPredicate validates PodCliqueScalingGroup event filtering for various scenarios.
func TestPodCliqueScalingGroupPredicate(t *testing.T) {
	predicate := podCliqueScalingGroupPredicate()

	t.Run("CreateFunc", func(t *testing.T) {
		tests := []struct {
			// Test case name describing the scenario being tested
			name string
			// Expected result from predicate
			expected bool
		}{
			{
				// Create events should always be ignored
				name:     "create events always ignored",
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
				createEvent := event.CreateEvent{Object: pcsg}

				result := predicate.Create(createEvent)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("DeleteFunc", func(t *testing.T) {
		tests := []struct {
			// Test case name describing the scenario being tested
			name string
			// Expected result from predicate
			expected bool
		}{
			{
				// Delete events should always be ignored
				name:     "delete events always ignored",
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
				deleteEvent := event.DeleteEvent{Object: pcsg}

				result := predicate.Delete(deleteEvent)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("UpdateFunc", func(t *testing.T) {
		tests := []struct {
			// Test case name describing the scenario being tested
			name string
			// Old PCSG setup function
			setupOldPCSG func() *grovecorev1alpha1.PodCliqueScalingGroup
			// New PCSG setup function
			setupNewPCSG func() *grovecorev1alpha1.PodCliqueScalingGroup
			// Expected result from predicate
			expected bool
		}{
			{
				// MinAvailableBreached condition added should be processed
				name: "MinAvailableBreached condition added processed",
				setupOldPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					return testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
				},
				setupNewPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
					meta.SetStatusCondition(&pcsg.Status.Conditions, metav1.Condition{
						Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
						Status: metav1.ConditionTrue,
						Reason: "TestReason",
					})
					return pcsg
				},
				expected: true,
			},
			{
				// MinAvailableBreached condition status changed should be processed
				name: "MinAvailableBreached condition status changed processed",
				setupOldPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
					meta.SetStatusCondition(&pcsg.Status.Conditions, metav1.Condition{
						Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
						Status: metav1.ConditionTrue,
						Reason: "TestReason",
					})
					return pcsg
				},
				setupNewPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
					meta.SetStatusCondition(&pcsg.Status.Conditions, metav1.Condition{
						Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
						Status: metav1.ConditionFalse,
						Reason: "TestReason",
					})
					return pcsg
				},
				expected: true,
			},
			{
				// MinAvailableBreached condition removed should be processed
				name: "MinAvailableBreached condition removed processed",
				setupOldPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
					meta.SetStatusCondition(&pcsg.Status.Conditions, metav1.Condition{
						Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
						Status: metav1.ConditionTrue,
						Reason: "TestReason",
					})
					return pcsg
				},
				setupNewPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					return testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
				},
				expected: true,
			},
			{
				// No MinAvailableBreached condition changes should be ignored
				name: "no MinAvailableBreached condition changes ignored",
				setupOldPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
					meta.SetStatusCondition(&pcsg.Status.Conditions, metav1.Condition{
						Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
						Status: metav1.ConditionTrue,
						Reason: "TestReason",
					})
					return pcsg
				},
				setupNewPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
					meta.SetStatusCondition(&pcsg.Status.Conditions, metav1.Condition{
						Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
						Status: metav1.ConditionTrue,
						Reason: "TestReason",
					})
					return pcsg
				},
				expected: false,
			},
			{
				// Other condition changes should be ignored
				name: "other condition changes ignored",
				setupOldPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					return testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
				},
				setupNewPCSG: func() *grovecorev1alpha1.PodCliqueScalingGroup {
					pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
					meta.SetStatusCondition(&pcsg.Status.Conditions, metav1.Condition{
						Type:   "SomeOtherCondition",
						Status: metav1.ConditionTrue,
						Reason: "TestReason",
					})
					return pcsg
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				oldPCSG := tt.setupOldPCSG()
				newPCSG := tt.setupNewPCSG()
				updateEvent := event.UpdateEvent{ObjectOld: oldPCSG, ObjectNew: newPCSG}

				result := predicate.Update(updateEvent)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("GenericFunc", func(t *testing.T) {
		tests := []struct {
			// Test case name describing the scenario being tested
			name string
			// Expected result from predicate
			expected bool
		}{
			{
				// Generic events should always be ignored
				name:     "generic events always ignored",
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-namespace", "test-pgs", 0).Build()
				genericEvent := event.GenericEvent{Object: pcsg}

				result := predicate.Generic(genericEvent)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

// TestHasSpecChanged validates spec change detection for various scenarios.
func TestHasSpecChanged(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Old object generation
		oldGeneration int64
		// New object generation
		newGeneration int64
		// Expected result
		expected bool
	}{
		{
			// Generation change indicates spec change
			name:          "generation change indicates spec change",
			oldGeneration: 1,
			newGeneration: 2,
			expected:      true,
		},
		{
			// Same generation indicates no spec change
			name:          "same generation indicates no spec change",
			oldGeneration: 1,
			newGeneration: 1,
			expected:      false,
		},
		{
			// Generation decrease (unusual but possible) indicates change
			name:          "generation decrease indicates change",
			oldGeneration: 2,
			newGeneration: 1,
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			oldPclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
			newPclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()

			oldPclq.Generation = tt.oldGeneration
			newPclq.Generation = tt.newGeneration

			updateEvent := event.UpdateEvent{ObjectOld: oldPclq, ObjectNew: newPclq}
			result := hasSpecChanged(updateEvent)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestHasStatusChanged validates status change detection for various scenarios.
func TestHasStatusChanged(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Old PodClique setup function
		setupOldPodClique func() *grovecorev1alpha1.PodClique
		// New PodClique setup function
		setupNewPodClique func() *grovecorev1alpha1.PodClique
		// Expected result
		expected bool
	}{
		{
			// Replica count change should be detected
			name: "replica count change detected",
			setupOldPodClique: func() *grovecorev1alpha1.PodClique {
				pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
				pclq.Status.ReadyReplicas = 0
				return pclq
			},
			setupNewPodClique: func() *grovecorev1alpha1.PodClique {
				pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
				pclq.Status.ReadyReplicas = 1
				return pclq
			},
			expected: true,
		},
		{
			// MinAvailableBreached condition change should be detected
			name: "MinAvailableBreached condition change detected",
			setupOldPodClique: func() *grovecorev1alpha1.PodClique {
				return testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
			},
			setupNewPodClique: func() *grovecorev1alpha1.PodClique {
				pclq := testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
				meta.SetStatusCondition(&pclq.Status.Conditions, metav1.Condition{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				})
				return pclq
			},
			expected: true,
		},
		{
			// No status changes should not be detected
			name: "no status changes not detected",
			setupOldPodClique: func() *grovecorev1alpha1.PodClique {
				return testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
			},
			setupNewPodClique: func() *grovecorev1alpha1.PodClique {
				return testutils.NewPodCliqueBuilder("test-pgs", types.UID("test-uid"), "test-pclq", "test-namespace", 0).Build()
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			oldPclq := tt.setupOldPodClique()
			newPclq := tt.setupNewPodClique()
			updateEvent := event.UpdateEvent{ObjectOld: oldPclq, ObjectNew: newPclq}

			result := hasStatusChanged(updateEvent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestHasAnyStatusReplicasChanged validates replica status change detection for various scenarios.
func TestHasAnyStatusReplicasChanged(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Old status setup
		oldStatus grovecorev1alpha1.PodCliqueStatus
		// New status setup
		newStatus grovecorev1alpha1.PodCliqueStatus
		// Expected result
		expected bool
	}{
		{
			// Replicas field change should be detected
			name: "replicas field change detected",
			oldStatus: grovecorev1alpha1.PodCliqueStatus{
				Replicas: 1,
			},
			newStatus: grovecorev1alpha1.PodCliqueStatus{
				Replicas: 2,
			},
			expected: true,
		},
		{
			// ReadyReplicas field change should be detected
			name: "ready replicas field change detected",
			oldStatus: grovecorev1alpha1.PodCliqueStatus{
				ReadyReplicas: 0,
			},
			newStatus: grovecorev1alpha1.PodCliqueStatus{
				ReadyReplicas: 1,
			},
			expected: true,
		},
		{
			// ScheduleGatedReplicas field change should be detected
			name: "schedule gated replicas field change detected",
			oldStatus: grovecorev1alpha1.PodCliqueStatus{
				ScheduleGatedReplicas: 0,
			},
			newStatus: grovecorev1alpha1.PodCliqueStatus{
				ScheduleGatedReplicas: 1,
			},
			expected: true,
		},
		{
			// No replica field changes should not be detected
			name: "no replica field changes not detected",
			oldStatus: grovecorev1alpha1.PodCliqueStatus{
				Replicas:              1,
				ReadyReplicas:         1,
				ScheduleGatedReplicas: 0,
			},
			newStatus: grovecorev1alpha1.PodCliqueStatus{
				Replicas:              1,
				ReadyReplicas:         1,
				ScheduleGatedReplicas: 0,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := hasAnyStatusReplicasChanged(tt.oldStatus, tt.newStatus)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestHasMinAvailableBreachedConditionChanged validates MinAvailableBreached condition change detection for various scenarios.
func TestHasMinAvailableBreachedConditionChanged(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Old conditions setup
		oldConditions []metav1.Condition
		// New conditions setup
		newConditions []metav1.Condition
		// Expected result
		expected bool
	}{
		{
			// Condition added should be detected
			name:          "condition added detected",
			oldConditions: []metav1.Condition{},
			newConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
			},
			expected: true,
		},
		{
			// Condition removed should be detected
			name: "condition removed detected",
			oldConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
			},
			newConditions: []metav1.Condition{},
			expected:      true,
		},
		{
			// Condition status changed should be detected
			name: "condition status changed detected",
			oldConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
			},
			newConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionFalse,
					Reason: "TestReason",
				},
			},
			expected: true,
		},
		{
			// No condition changes should not be detected
			name: "no condition changes not detected",
			oldConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
			},
			newConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
			},
			expected: false,
		},
		{
			// Both conditions nil should not be detected
			name:          "both conditions nil not detected",
			oldConditions: []metav1.Condition{},
			newConditions: []metav1.Condition{},
			expected:      false,
		},
		{
			// Other condition changes should not be detected
			name: "other condition changes not detected",
			oldConditions: []metav1.Condition{
				{
					Type:   "SomeOtherCondition",
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
			},
			newConditions: []metav1.Condition{
				{
					Type:   "SomeOtherCondition",
					Status: metav1.ConditionFalse,
					Reason: "TestReason",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := hasMinAvailableBreachedConditionChanged(tt.oldConditions, tt.newConditions)
			assert.Equal(t, tt.expected, result)
		})
	}
}
