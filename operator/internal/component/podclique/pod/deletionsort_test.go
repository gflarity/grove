/*
Copyright 2025 The Grove Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pod

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestDeletionSorter_Len tests the Len method of DeletionSorter.
// It verifies that the length is correctly returned for various slice sizes.
func TestDeletionSorter_Len(t *testing.T) {
	tests := []struct {
		name     string        // Test case name for identification
		pods     []*corev1.Pod // Input slice of pods to test
		expected int           // Expected length to be returned
	}{
		{
			name:     "empty slice",
			pods:     []*corev1.Pod{},
			expected: 0,
		},
		{
			name:     "single pod",
			pods:     []*corev1.Pod{createDeletionTestPod("pod1", "")},
			expected: 1,
		},
		{
			name: "multiple pods",
			pods: []*corev1.Pod{
				createDeletionTestPod("pod1", ""),
				createDeletionTestPod("pod2", ""),
				createDeletionTestPod("pod3", ""),
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorter := DeletionSorter(tt.pods)
			assert.Equal(t, tt.expected, sorter.Len())
		})
	}
}

// TestDeletionSorter_Swap tests the Swap method of DeletionSorter.
// It verifies that two elements are correctly swapped at given indices.
func TestDeletionSorter_Swap(t *testing.T) {
	tests := []struct {
		name        string        // Test case name for identification
		pods        []*corev1.Pod // Initial slice of pods
		swapI       int           // First index to swap
		swapJ       int           // Second index to swap
		expectedI   string        // Expected pod name at index i after swap
		expectedJ   string        // Expected pod name at index j after swap
		description string        // Description of what this test verifies
	}{
		{
			name: "swap first and last",
			pods: []*corev1.Pod{
				createDeletionTestPod("pod1", ""),
				createDeletionTestPod("pod2", ""),
				createDeletionTestPod("pod3", ""),
			},
			swapI:       0,
			swapJ:       2,
			expectedI:   "pod3",
			expectedJ:   "pod1",
			description: "Verifies swapping elements at the beginning and end of the slice",
		},
		{
			name: "swap adjacent elements",
			pods: []*corev1.Pod{
				createDeletionTestPod("pod1", ""),
				createDeletionTestPod("pod2", ""),
			},
			swapI:       0,
			swapJ:       1,
			expectedI:   "pod2",
			expectedJ:   "pod1",
			description: "Verifies swapping adjacent elements",
		},
		{
			name: "swap same element",
			pods: []*corev1.Pod{
				createDeletionTestPod("pod1", ""),
			},
			swapI:       0,
			swapJ:       0,
			expectedI:   "pod1",
			expectedJ:   "pod1",
			description: "Verifies swapping an element with itself (no-op)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorter := DeletionSorter(tt.pods)
			sorter.Swap(tt.swapI, tt.swapJ)

			assert.Equal(t, tt.expectedI, sorter[tt.swapI].Name, "Pod at index %d should be %s", tt.swapI, tt.expectedI)
			assert.Equal(t, tt.expectedJ, sorter[tt.swapJ].Name, "Pod at index %d should be %s", tt.swapJ, tt.expectedJ)
		})
	}
}

// TestDeletionSorter_Less tests the Less method of DeletionSorter.
// It verifies all sorting criteria: node assignment, pod phase, readiness, and creation time.
func TestDeletionSorter_Less(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	tests := []struct {
		name        string      // Test case name for identification
		podI        *corev1.Pod // First pod to compare
		podJ        *corev1.Pod // Second pod to compare
		expected    bool        // Expected result of Less(i, j)
		description string      // Description of what sorting criterion this tests
	}{
		// Test criterion 1: Unassigned < assigned
		{
			name:        "unassigned pod preferred over assigned",
			podI:        createDeletionTestPod("unassigned", ""),
			podJ:        createDeletionTestPod("assigned", "node1"),
			expected:    true,
			description: "Unassigned pods should be preferred for deletion over assigned pods",
		},
		{
			name:        "assigned pod not preferred over unassigned",
			podI:        createDeletionTestPod("assigned", "node1"),
			podJ:        createDeletionTestPod("unassigned", ""),
			expected:    false,
			description: "Assigned pods should not be preferred over unassigned pods",
		},
		{
			name:        "both assigned pods fall through to next criteria",
			podI:        createDeletionTestPod("assigned1", "node1"),
			podJ:        createDeletionTestPod("assigned2", "node2"),
			expected:    true,
			description: "When both pods are assigned, falls through to creation time (newer preferred)",
		},

		// Test criterion 2: PodPending < PodUnknown < PodRunning
		{
			name:        "pending preferred over unknown",
			podI:        createDeletionTestPodWithPhase("pending", "", corev1.PodPending),
			podJ:        createDeletionTestPodWithPhase("unknown", "", corev1.PodUnknown),
			expected:    true,
			description: "Pending pods should be preferred for deletion over unknown pods",
		},
		{
			name:        "pending preferred over running",
			podI:        createDeletionTestPodWithPhase("pending", "", corev1.PodPending),
			podJ:        createDeletionTestPodWithPhase("running", "", corev1.PodRunning),
			expected:    true,
			description: "Pending pods should be preferred for deletion over running pods",
		},
		{
			name:        "unknown preferred over running",
			podI:        createDeletionTestPodWithPhase("unknown", "", corev1.PodUnknown),
			podJ:        createDeletionTestPodWithPhase("running", "", corev1.PodRunning),
			expected:    true,
			description: "Unknown pods should be preferred for deletion over running pods",
		},
		{
			name:        "running not preferred over pending",
			podI:        createDeletionTestPodWithPhase("running", "", corev1.PodRunning),
			podJ:        createDeletionTestPodWithPhase("pending", "", corev1.PodPending),
			expected:    false,
			description: "Running pods should not be preferred over pending pods",
		},

		// Test criterion 3: Not ready < ready
		{
			name:        "not ready preferred over ready",
			podI:        createDeletionTestPodWithReadiness("notready", "", false),
			podJ:        createDeletionTestPodWithReadiness("ready", "", true),
			expected:    true,
			description: "Not ready pods should be preferred for deletion over ready pods",
		},
		{
			name:        "ready not preferred over not ready",
			podI:        createDeletionTestPodWithReadiness("ready", "", true),
			podJ:        createDeletionTestPodWithReadiness("notready", "", false),
			expected:    false,
			description: "Ready pods should not be preferred over not ready pods",
		},
		{
			name:        "both ready pods fall through to next criteria",
			podI:        createDeletionTestPodWithReadiness("ready1", "", true),
			podJ:        createDeletionTestPodWithReadiness("ready2", "", true),
			expected:    true,
			description: "When both pods have same readiness, falls through to creation time (newer preferred)",
		},

		// Test criterion 4: Empty creation time pods < newer pods < older pods
		{
			name:        "zero creation time preferred",
			podI:        createDeletionTestPodWithCreationTime("zero", "", time.Time{}),
			podJ:        createDeletionTestPodWithCreationTime("normal", "", now),
			expected:    true,
			description: "Pods with zero creation time should be preferred for deletion",
		},
		{
			name:        "newer pod preferred over older",
			podI:        createDeletionTestPodWithCreationTime("newer", "", now),
			podJ:        createDeletionTestPodWithCreationTime("older", "", earlier),
			expected:    true,
			description: "Newer pods should be preferred for deletion over older pods",
		},
		{
			name:        "older pod not preferred over newer",
			podI:        createDeletionTestPodWithCreationTime("older", "", earlier),
			podJ:        createDeletionTestPodWithCreationTime("newer", "", now),
			expected:    false,
			description: "Older pods should not be preferred over newer pods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorter := DeletionSorter([]*corev1.Pod{tt.podI, tt.podJ})
			result := sorter.Less(0, 1)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestIsPodReady tests the isPodReady helper function.
// It verifies correct detection of pod readiness based on PodReady condition.
func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name        string                // Test case name for identification
		conditions  []corev1.PodCondition // Pod conditions to test
		expected    bool                  // Expected readiness result
		description string                // Description of what this test verifies
	}{
		{
			name: "pod with ready condition true",
			conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
			expected:    true,
			description: "Pod with PodReady condition set to True should be considered ready",
		},
		{
			name: "pod with ready condition false",
			conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
			expected:    false,
			description: "Pod with PodReady condition set to False should not be considered ready",
		},
		{
			name: "pod with ready condition unknown",
			conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionUnknown,
				},
			},
			expected:    false,
			description: "Pod with PodReady condition set to Unknown should not be considered ready",
		},
		{
			name: "pod without ready condition",
			conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
			},
			expected:    false,
			description: "Pod without PodReady condition should not be considered ready",
		},
		{
			name:        "pod with no conditions",
			conditions:  []corev1.PodCondition{},
			expected:    false,
			description: "Pod with no conditions should not be considered ready",
		},
		{
			name: "pod with multiple conditions including ready true",
			conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
			},
			expected:    true,
			description: "Pod with multiple conditions where PodReady is True should be considered ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: tt.conditions,
				},
			}
			result := isPodReady(pod)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestDeletionSorter_FullSort tests the complete sorting functionality.
// It verifies that pods are sorted correctly according to all deletion criteria.
func TestDeletionSorter_FullSort(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	tests := []struct {
		name          string        // Test case name for identification
		inputPods     []*corev1.Pod // Input pods to sort
		expectedOrder []string      // Expected order of pod names after sorting
		description   string        // Description of what this test verifies
	}{
		{
			name: "sort by node assignment priority",
			inputPods: []*corev1.Pod{
				createDeletionTestPod("assigned", "node1"),
				createDeletionTestPod("unassigned", ""),
			},
			expectedOrder: []string{"unassigned", "assigned"},
			description:   "Unassigned pods should come first in deletion order",
		},
		{
			name: "sort by pod phase priority",
			inputPods: []*corev1.Pod{
				createDeletionTestPodWithPhase("running", "node1", corev1.PodRunning),
				createDeletionTestPodWithPhase("pending", "node2", corev1.PodPending),
				createDeletionTestPodWithPhase("unknown", "node3", corev1.PodUnknown),
			},
			expectedOrder: []string{"pending", "unknown", "running"},
			description:   "Pods should be sorted by phase: Pending < Unknown < Running",
		},
		{
			name: "sort by readiness priority",
			inputPods: []*corev1.Pod{
				createDeletionTestPodWithReadiness("ready", "node1", true),
				createDeletionTestPodWithReadiness("notready", "node2", false),
			},
			expectedOrder: []string{"notready", "ready"},
			description:   "Not ready pods should come first in deletion order",
		},
		{
			name: "sort by creation time priority",
			inputPods: []*corev1.Pod{
				createDeletionTestPodWithCreationTime("older", "node1", earlier),
				createDeletionTestPodWithCreationTime("newer", "node2", now),
				createDeletionTestPodWithCreationTime("zero", "node3", time.Time{}),
			},
			expectedOrder: []string{"zero", "newer", "older"},
			description:   "Pods should be sorted by creation time: zero time < newer < older",
		},
		{
			name: "complex multi-criteria sort",
			inputPods: []*corev1.Pod{
				// This pod: assigned, running, ready, older - should be last
				func() *corev1.Pod {
					pod := createDeletionTestPodWithCreationTime("last", "node1", earlier)
					pod.Status.Phase = corev1.PodRunning
					pod.Status.Conditions = []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					}
					return pod
				}(),
				// This pod: unassigned, pending, not ready, newer - should be first
				func() *corev1.Pod {
					pod := createDeletionTestPodWithCreationTime("first", "", now)
					pod.Status.Phase = corev1.PodPending
					pod.Status.Conditions = []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					}
					return pod
				}(),
				// This pod: assigned, running, not ready, newer - should be middle
				func() *corev1.Pod {
					pod := createDeletionTestPodWithCreationTime("middle", "node2", now)
					pod.Status.Phase = corev1.PodRunning
					pod.Status.Conditions = []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					}
					return pod
				}(),
			},
			expectedOrder: []string{"first", "middle", "last"},
			description:   "Complex sorting should prioritize all criteria in correct order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorter := DeletionSorter(tt.inputPods)
			sort.Sort(sorter)

			require.Len(t, sorter, len(tt.expectedOrder), "Sorted slice should have same length as expected order")

			actualOrder := make([]string, len(sorter))
			for i, pod := range sorter {
				actualOrder[i] = pod.Name
			}

			assert.Equal(t, tt.expectedOrder, actualOrder, tt.description)
		})
	}
}

// Helper functions for creating test pods with specific characteristics

// createDeletionTestPod creates a basic test pod with the given name and node assignment.
func createDeletionTestPod(name, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}

// createDeletionTestPodWithPhase creates a test pod with the specified phase.
func createDeletionTestPodWithPhase(name, nodeName string, phase corev1.PodPhase) *corev1.Pod {
	pod := createDeletionTestPod(name, nodeName)
	pod.Status.Phase = phase
	return pod
}

// createDeletionTestPodWithReadiness creates a test pod with the specified readiness condition.
func createDeletionTestPodWithReadiness(name, nodeName string, ready bool) *corev1.Pod {
	pod := createDeletionTestPod(name, nodeName)
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	pod.Status.Conditions = []corev1.PodCondition{
		{
			Type:   corev1.PodReady,
			Status: status,
		},
	}
	return pod
}

// createDeletionTestPodWithCreationTime creates a test pod with the specified creation timestamp.
func createDeletionTestPodWithCreationTime(name, nodeName string, creationTime time.Time) *corev1.Pod {
	pod := createDeletionTestPod(name, nodeName)
	if !creationTime.IsZero() {
		pod.CreationTimestamp = metav1.NewTime(creationTime)
	}
	return pod
}
