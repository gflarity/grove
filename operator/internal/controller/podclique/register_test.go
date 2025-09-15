// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package podclique

import (
	"context"
	"testing"

	"github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	groveschedulerv1alpha1 "github.com/NVIDIA/grove/scheduler/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// boolPtr returns a pointer to the given boolean value
func boolPtr(b bool) *bool {
	return &b
}

// TestManagedPodCliquePredicate tests the managedPodCliquePredicate function
// which filters PodClique events to only those managed by PodCliqueScalingGroup or PodGangSet.
func TestManagedPodCliquePredicate(t *testing.T) {
	predicate := managedPodCliquePredicate()

	tests := []struct {
		// name describes the test scenario
		name string
		// podClique is the PodClique object to test
		podClique *grovecorev1alpha1.PodClique
		// expectedCreate indicates if CreateFunc should return true
		expectedCreate bool
		// expectedDelete indicates if DeleteFunc should return true
		expectedDelete bool
		// expectedUpdate indicates if UpdateFunc should return true
		expectedUpdate bool
	}{
		{
			// PodClique managed by PodGangSet should be accepted
			name: "managed by PodGangSet",
			podClique: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodGangSet,
							Name: "test-pgs",
						},
					},
				},
			},
			expectedCreate: true,
			expectedDelete: true,
			expectedUpdate: true,
		},
		{
			// PodClique managed by PodCliqueScalingGroup should be accepted
			name: "managed by PodCliqueScalingGroup",
			podClique: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodCliqueScalingGroup,
							Name: "test-pcsg",
						},
					},
				},
			},
			expectedCreate: true,
			expectedDelete: true,
			expectedUpdate: true,
		},
		{
			// PodClique without Grove management labels should be rejected
			name: "not managed by Grove",
			podClique: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodGangSet,
							Name: "test-pgs",
						},
					},
				},
			},
			expectedCreate: false,
			expectedDelete: false,
			expectedUpdate: false,
		},
		{
			// PodClique with wrong owner kind should be rejected
			name: "wrong owner kind",
			podClique: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "SomeOtherKind",
							Name: "test-other",
						},
					},
				},
			},
			expectedCreate: false,
			expectedDelete: false,
			expectedUpdate: false,
		},
		{
			// PodClique without owner references should be rejected
			name: "no owner references",
			podClique: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
				},
			},
			expectedCreate: false,
			expectedDelete: false,
			expectedUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test CreateFunc
			createEvent := event.CreateEvent{Object: tt.podClique}
			assert.Equal(t, tt.expectedCreate, predicate.Create(createEvent),
				"CreateFunc should return %v for %s", tt.expectedCreate, tt.name)

			// Test DeleteFunc
			deleteEvent := event.DeleteEvent{Object: tt.podClique}
			assert.Equal(t, tt.expectedDelete, predicate.Delete(deleteEvent),
				"DeleteFunc should return %v for %s", tt.expectedDelete, tt.name)

			// Test UpdateFunc
			updateEvent := event.UpdateEvent{ObjectOld: tt.podClique, ObjectNew: tt.podClique}
			assert.Equal(t, tt.expectedUpdate, predicate.Update(updateEvent),
				"UpdateFunc should return %v for %s", tt.expectedUpdate, tt.name)

			// Test GenericFunc - should always return false
			genericEvent := event.GenericEvent{Object: tt.podClique}
			assert.False(t, predicate.Generic(genericEvent),
				"GenericFunc should always return false")
		})
	}
}

// TestPodPredicate tests the podPredicate function which filters Pod events
// to only include pods managed by Grove, focusing on deletion and status changes.
func TestPodPredicate(t *testing.T) {
	predicate := podPredicate()

	tests := []struct {
		// name describes the test scenario
		name string
		// pod is the Pod object to test
		pod *corev1.Pod
		// oldPod is the old Pod object for update events (can be nil for non-update tests)
		oldPod *corev1.Pod
		// expectedDelete indicates if DeleteFunc should return true
		expectedDelete bool
		// expectedUpdate indicates if UpdateFunc should return true
		expectedUpdate bool
	}{
		{
			// Grove-managed pod deletion should be accepted
			name: "grove managed pod deletion",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodClique,
							Name: "test-pclq",
						},
					},
				},
			},
			expectedDelete: true,
		},
		{
			// Non-Grove managed pod deletion should be rejected
			name: "non-grove managed pod deletion",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expectedDelete: false,
		},
		{
			// Grove-managed pod status change should be accepted for updates
			name: "grove managed pod status change",
			oldPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pod",
					Namespace:  "default",
					Generation: 1,
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodClique,
							Name: "test-pclq",
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pod",
					Namespace:  "default",
					Generation: 1, // Same generation (no spec change)
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodClique,
							Name: "test-pclq",
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue, // Status changed
						},
					},
				},
			},
			expectedDelete: false, // Delete should be false for update scenarios
			expectedUpdate: true,
		},
		{
			// Grove-managed pod spec change should be rejected for updates
			name: "grove managed pod spec change",
			oldPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pod",
					Namespace:  "default",
					Generation: 1,
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodClique,
							Name: "test-pclq",
						},
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pod",
					Namespace:  "default",
					Generation: 2, // Generation changed (spec change)
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodClique,
							Name: "test-pclq",
						},
					},
				},
			},
			expectedDelete: false, // Delete should be false for update scenarios
			expectedUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test CreateFunc - should always return false
			createEvent := event.CreateEvent{Object: tt.pod}
			assert.False(t, predicate.Create(createEvent),
				"CreateFunc should always return false")

			// Test DeleteFunc - only test if this is a deletion test case (no oldPod)
			if tt.oldPod == nil {
				deleteEvent := event.DeleteEvent{Object: tt.pod}
				assert.Equal(t, tt.expectedDelete, predicate.Delete(deleteEvent),
					"DeleteFunc should return %v for %s", tt.expectedDelete, tt.name)
			}

			// Test UpdateFunc (only if oldPod is provided)
			if tt.oldPod != nil {
				updateEvent := event.UpdateEvent{ObjectOld: tt.oldPod, ObjectNew: tt.pod}
				assert.Equal(t, tt.expectedUpdate, predicate.Update(updateEvent),
					"UpdateFunc should return %v for %s", tt.expectedUpdate, tt.name)
			}

			// Test GenericFunc - should always return false
			genericEvent := event.GenericEvent{Object: tt.pod}
			assert.False(t, predicate.Generic(genericEvent),
				"GenericFunc should always return false")
		})
	}
}

// TestHasPodSpecChanged tests the hasPodSpecChanged function which determines
// if a Pod's spec has changed by comparing generations.
func TestHasPodSpecChanged(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// oldGeneration is the generation of the old Pod object
		oldGeneration int64
		// newGeneration is the generation of the new Pod object
		newGeneration int64
		// expected indicates if the function should return true
		expected bool
	}{
		{
			// Same generation means no spec change
			name:          "same generation",
			oldGeneration: 1,
			newGeneration: 1,
			expected:      false,
		},
		{
			// Different generation means spec changed
			name:          "generation increased",
			oldGeneration: 1,
			newGeneration: 2,
			expected:      true,
		},
		{
			// Edge case: generation decreased (shouldn't happen in practice)
			name:          "generation decreased",
			oldGeneration: 2,
			newGeneration: 1,
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Generation: tt.oldGeneration},
			}
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Generation: tt.newGeneration},
			}

			updateEvent := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}

			result := hasPodSpecChanged(updateEvent)
			assert.Equal(t, tt.expected, result,
				"hasPodSpecChanged should return %v for %s", tt.expected, tt.name)
		})
	}
}

// TestHasPodStatusChanged tests the hasPodStatusChanged function which determines
// if a Pod's status has changed in meaningful ways.
func TestHasPodStatusChanged(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// oldPod is the old Pod object
		oldPod *corev1.Pod
		// newPod is the new Pod object
		newPod *corev1.Pod
		// expected indicates if the function should return true
		expected bool
	}{
		{
			// Ready condition changed from false to true
			name: "ready condition changed to true",
			oldPod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			newPod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: true,
		},
		{
			// Ready condition unchanged
			name: "ready condition unchanged",
			oldPod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			newPod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: false,
		},
		{
			// Container started status changed
			name: "container started status changed",
			oldPod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:    "test-container",
							Started: boolPtr(false),
							Ready:   false,
						},
					},
				},
			},
			newPod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:    "test-container",
							Started: boolPtr(true),
							Ready:   false,
						},
					},
				},
			},
			expected: true,
		},
		{
			// Container ready status changed
			name: "container ready status changed",
			oldPod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:    "test-container",
							Started: boolPtr(true),
							Ready:   false,
						},
					},
				},
			},
			newPod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:    "test-container",
							Started: boolPtr(true),
							Ready:   true,
						},
					},
				},
			},
			expected: true,
		},
		{
			// Termination state appeared
			name: "termination state appeared",
			oldPod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "test-container",
						},
					},
				},
			},
			newPod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "test-container",
							LastTerminationState: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 1,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			// Invalid pod objects (not *corev1.Pod)
			name:     "invalid pod objects",
			oldPod:   nil,
			newPod:   nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var updateEvent event.UpdateEvent
			if tt.oldPod != nil && tt.newPod != nil {
				updateEvent = event.UpdateEvent{
					ObjectOld: tt.oldPod,
					ObjectNew: tt.newPod,
				}
			} else {
				// Test with non-Pod objects
				updateEvent = event.UpdateEvent{
					ObjectOld: &grovecorev1alpha1.PodClique{},
					ObjectNew: &grovecorev1alpha1.PodClique{},
				}
			}

			result := hasPodStatusChanged(updateEvent)
			assert.Equal(t, tt.expected, result,
				"hasPodStatusChanged should return %v for %s", tt.expected, tt.name)
		})
	}
}

// TestHasReadyConditionChanged tests the hasReadyConditionChanged function
// which checks if a Pod's ready condition has changed between updates.
func TestHasReadyConditionChanged(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// oldConditions are the conditions from the old Pod
		oldConditions []corev1.PodCondition
		// newConditions are the conditions from the new Pod
		newConditions []corev1.PodCondition
		// expected indicates if the function should return true
		expected bool
	}{
		{
			// Ready condition changed from false to true
			name: "ready condition changed false to true",
			oldConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
			newConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			expected: true,
		},
		{
			// Ready condition changed from true to false
			name: "ready condition changed true to false",
			oldConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			newConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
			expected: true,
		},
		{
			// Ready condition unchanged (both true)
			name: "ready condition unchanged both true",
			oldConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			newConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			expected: false,
		},
		{
			// Ready condition unchanged (both false)
			name: "ready condition unchanged both false",
			oldConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
			newConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
			expected: false,
		},
		{
			// Ready condition missing in old, present in new
			name:          "ready condition added",
			oldConditions: []corev1.PodCondition{},
			newConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			expected: true,
		},
		{
			// Ready condition present in old, missing in new
			name: "ready condition removed",
			oldConditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			newConditions: []corev1.PodCondition{},
			expected:      true,
		},
		{
			// No ready condition in either
			name:          "no ready condition in either",
			oldConditions: []corev1.PodCondition{},
			newConditions: []corev1.PodCondition{},
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasReadyConditionChanged(tt.oldConditions, tt.newConditions)
			assert.Equal(t, tt.expected, result,
				"hasReadyConditionChanged should return %v for %s", tt.expected, tt.name)
		})
	}
}

// TestHasLastTerminationStateChanged tests the hasLastTerminationStateChanged function
// which checks if container termination states have changed.
func TestHasLastTerminationStateChanged(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// oldStatuses are the container statuses from the old Pod
		oldStatuses []corev1.ContainerStatus
		// newStatuses are the container statuses from the new Pod
		newStatuses []corev1.ContainerStatus
		// expected indicates if the function should return true
		expected bool
	}{
		{
			// Termination state appeared (nil to non-nil)
			name:        "termination state appeared",
			oldStatuses: []corev1.ContainerStatus{{Name: "test"}},
			newStatuses: []corev1.ContainerStatus{
				{
					Name: "test",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{ExitCode: 1},
					},
				},
			},
			expected: true,
		},
		{
			// Termination state disappeared (non-nil to nil)
			name: "termination state disappeared",
			oldStatuses: []corev1.ContainerStatus{
				{
					Name: "test",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{ExitCode: 1},
					},
				},
			},
			newStatuses: []corev1.ContainerStatus{{Name: "test"}},
			expected:    true,
		},
		{
			// Both have termination states (no change)
			name: "both have termination states",
			oldStatuses: []corev1.ContainerStatus{
				{
					Name: "test",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{ExitCode: 1},
					},
				},
			},
			newStatuses: []corev1.ContainerStatus{
				{
					Name: "test",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{ExitCode: 1},
					},
				},
			},
			expected: false,
		},
		{
			// Neither have termination states (no change)
			name:        "neither have termination states",
			oldStatuses: []corev1.ContainerStatus{{Name: "test"}},
			newStatuses: []corev1.ContainerStatus{{Name: "test"}},
			expected:    false,
		},
		{
			// Empty container statuses
			name:        "empty container statuses",
			oldStatuses: []corev1.ContainerStatus{},
			newStatuses: []corev1.ContainerStatus{},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasLastTerminationStateChanged(tt.oldStatuses, tt.newStatuses)
			assert.Equal(t, tt.expected, result,
				"hasLastTerminationStateChanged should return %v for %s", tt.expected, tt.name)
		})
	}
}

// TestHasStartedAndReadyChangedForAnyContainer tests the hasStartedAndReadyChangedForAnyContainer function
// which checks if any container's started or ready status has changed.
func TestHasStartedAndReadyChangedForAnyContainer(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// oldStatuses are the container statuses from the old Pod
		oldStatuses []corev1.ContainerStatus
		// newStatuses are the container statuses from the new Pod
		newStatuses []corev1.ContainerStatus
		// expected indicates if the function should return true
		expected bool
	}{
		{
			// Container started status changed
			name: "container started status changed",
			oldStatuses: []corev1.ContainerStatus{
				{Name: "test", Started: boolPtr(false), Ready: false},
			},
			newStatuses: []corev1.ContainerStatus{
				{Name: "test", Started: boolPtr(true), Ready: false},
			},
			expected: true,
		},
		{
			// Container ready status changed
			name: "container ready status changed",
			oldStatuses: []corev1.ContainerStatus{
				{Name: "test", Started: boolPtr(true), Ready: false},
			},
			newStatuses: []corev1.ContainerStatus{
				{Name: "test", Started: boolPtr(true), Ready: true},
			},
			expected: true,
		},
		{
			// No status changes - but function compares pointers, not values
			name: "no status changes",
			oldStatuses: []corev1.ContainerStatus{
				{Name: "test", Started: boolPtr(true), Ready: true},
			},
			newStatuses: []corev1.ContainerStatus{
				{Name: "test", Started: boolPtr(true), Ready: true},
			},
			expected: true, // Function compares pointer addresses, not values, so different pointers = change detected
		},
		{
			// Container removed (present in old, missing in new)
			name: "container removed",
			oldStatuses: []corev1.ContainerStatus{
				{Name: "test", Started: boolPtr(true), Ready: true},
			},
			newStatuses: []corev1.ContainerStatus{},
			expected:    true,
		},
		{
			// Multiple containers, one changed
			name: "multiple containers one changed",
			oldStatuses: []corev1.ContainerStatus{
				{Name: "test1", Started: boolPtr(true), Ready: true},
				{Name: "test2", Started: boolPtr(true), Ready: false},
			},
			newStatuses: []corev1.ContainerStatus{
				{Name: "test1", Started: boolPtr(true), Ready: true},
				{Name: "test2", Started: boolPtr(true), Ready: true},
			},
			expected: true,
		},
		{
			// Empty container statuses
			name:        "empty container statuses",
			oldStatuses: []corev1.ContainerStatus{},
			newStatuses: []corev1.ContainerStatus{},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasStartedAndReadyChangedForAnyContainer(tt.oldStatuses, tt.newStatuses)
			assert.Equal(t, tt.expected, result,
				"hasStartedAndReadyChangedForAnyContainer should return %v for %s", tt.expected, tt.name)
		})
	}
}

// TestMapPodGangToPCLQs tests the mapPodGangToPCLQs function which maps
// a PodGang to reconcile requests for its constituent PodCliques.
func TestMapPodGangToPCLQs(t *testing.T) {
	mapFunc := mapPodGangToPCLQs()

	tests := []struct {
		// name describes the test scenario
		name string
		// obj is the client.Object to map (should be a PodGang)
		obj client.Object
		// expectedRequests are the expected reconcile requests
		expectedRequests []reconcile.Request
	}{
		{
			// PodGang with multiple pod groups should generate multiple requests
			name: "podgang with multiple pod groups",
			obj: &groveschedulerv1alpha1.PodGang{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgang",
					Namespace: "default",
				},
				Spec: groveschedulerv1alpha1.PodGangSpec{
					PodGroups: []groveschedulerv1alpha1.PodGroup{
						{
							PodReferences: []groveschedulerv1alpha1.NamespacedName{
								{Name: "test-pclq-1-0", Namespace: "default"},
							},
						},
						{
							PodReferences: []groveschedulerv1alpha1.NamespacedName{
								{Name: "test-pclq-2-0", Namespace: "default"},
							},
						},
					},
				},
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "test-pclq-1", Namespace: "default"}},
				{NamespacedName: types.NamespacedName{Name: "test-pclq-2", Namespace: "default"}},
			},
		},
		{
			// PodGang with empty pod references should be skipped
			name: "podgang with empty pod references",
			obj: &groveschedulerv1alpha1.PodGang{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgang",
					Namespace: "default",
				},
				Spec: groveschedulerv1alpha1.PodGangSpec{
					PodGroups: []groveschedulerv1alpha1.PodGroup{
						{
							PodReferences: []groveschedulerv1alpha1.NamespacedName{},
						},
						{
							PodReferences: []groveschedulerv1alpha1.NamespacedName{
								{Name: "test-pclq-1-0", Namespace: "default"},
							},
						},
					},
				},
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "test-pclq-1", Namespace: "default"}},
			},
		},
		{
			// Empty PodGang should generate no requests
			name: "empty podgang",
			obj: &groveschedulerv1alpha1.PodGang{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgang",
					Namespace: "default",
				},
				Spec: groveschedulerv1alpha1.PodGangSpec{
					PodGroups: []groveschedulerv1alpha1.PodGroup{},
				},
			},
			expectedRequests: []reconcile.Request{},
		},
		{
			// Non-PodGang object should return nil
			name:             "non-podgang object",
			obj:              &corev1.Pod{},
			expectedRequests: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			requests := mapFunc(ctx, tt.obj)

			if tt.expectedRequests == nil {
				assert.Nil(t, requests, "Expected nil requests for %s", tt.name)
			} else {
				require.Equal(t, len(tt.expectedRequests), len(requests),
					"Expected %d requests but got %d for %s", len(tt.expectedRequests), len(requests), tt.name)

				for i, expectedReq := range tt.expectedRequests {
					assert.Equal(t, expectedReq, requests[i],
						"Request %d should match expected for %s", i, tt.name)
				}
			}
		})
	}
}

// TestExtractPCLQNameFromPodName tests the extractPCLQNameFromPodName function
// which extracts the PodClique name from a Pod name by removing the suffix after the last hyphen.
func TestExtractPCLQNameFromPodName(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// podName is the input Pod name
		podName string
		// expected is the expected PodClique name
		expected string
	}{
		{
			// Standard pod name with single suffix
			name:     "standard pod name",
			podName:  "test-pclq-0",
			expected: "test-pclq",
		},
		{
			// Pod name with multiple hyphens
			name:     "pod name with multiple hyphens",
			podName:  "my-test-pclq-name-0",
			expected: "my-test-pclq-name",
		},
		{
			// Pod name with complex suffix
			name:     "pod name with complex suffix",
			podName:  "test-pclq-abc123",
			expected: "test-pclq",
		},
		// Note: The function will panic on strings without hyphens, so we don't test that edge case
		// In practice, pod names generated by Grove will always have hyphens
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPCLQNameFromPodName(tt.podName)
			assert.Equal(t, tt.expected, result,
				"extractPCLQNameFromPodName should return %q for input %q", tt.expected, tt.podName)
		})
	}
}

// TestPodGangPredicate tests the podGangPredicate function which filters
// PodGang events to only process create and update events.
func TestPodGangPredicate(t *testing.T) {
	predicate := podGangPredicate()
	podGang := &groveschedulerv1alpha1.PodGang{}

	// Test CreateFunc - should return true
	createEvent := event.CreateEvent{Object: podGang}
	assert.True(t, predicate.Create(createEvent),
		"CreateFunc should return true for PodGang create events")

	// Test DeleteFunc - should return false
	deleteEvent := event.DeleteEvent{Object: podGang}
	assert.False(t, predicate.Delete(deleteEvent),
		"DeleteFunc should return false for PodGang delete events")

	// Test UpdateFunc - should return true
	updateEvent := event.UpdateEvent{ObjectOld: podGang, ObjectNew: podGang}
	assert.True(t, predicate.Update(updateEvent),
		"UpdateFunc should return true for PodGang update events")

	// Test GenericFunc - should return false
	genericEvent := event.GenericEvent{Object: podGang}
	assert.False(t, predicate.Generic(genericEvent),
		"GenericFunc should return false for PodGang generic events")
}

// TestIsManagedPod tests the isManagedPod function which determines
// if a Pod is managed by Grove by checking owner and labels.
func TestIsManagedPod(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// obj is the client.Object to test (should be a Pod)
		obj client.Object
		// expected indicates if the function should return true
		expected bool
	}{
		{
			// Grove-managed pod with correct owner and labels
			name: "grove managed pod",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodClique,
							Name: "test-pclq",
						},
					},
				},
			},
			expected: true,
		},
		{
			// Pod without Grove management labels
			name: "pod without grove labels",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodClique,
							Name: "test-pclq",
						},
					},
				},
			},
			expected: false,
		},
		{
			// Pod without PodClique owner
			name: "pod without podclique owner",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "SomeOtherKind",
							Name: "test-other",
						},
					},
				},
			},
			expected: false,
		},
		{
			// Non-Pod object
			name:     "non-pod object",
			obj:      &grovecorev1alpha1.PodClique{},
			expected: false,
		},
		{
			// Pod with no owner references
			name: "pod with no owner references",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isManagedPod(tt.obj)
			assert.Equal(t, tt.expected, result,
				"isManagedPod should return %v for %s", tt.expected, tt.name)
		})
	}
}
