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

	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// TestWithPCSGMinAvailableBreached verifies that the option correctly sets
// the MinAvailableBreached condition and adjusts AvailableReplicas.
func TestWithPCSGMinAvailableBreached(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different PCSG breach scenarios
		name string
		// Initial PCSG configuration before applying the option
		initialPCSG *grovecorev1alpha1.PodCliqueScalingGroup
		// Expected AvailableReplicas after applying the option
		expectedAvailableReplicas int32
		// Expected condition status
		expectedConditionStatus metav1.ConditionStatus
		// Expected condition reason
		expectedConditionReason string
	}{
		{
			// PCSG with default MinAvailable should set AvailableReplicas to replicas-1
			name: "default min available",
			initialPCSG: &grovecorev1alpha1.PodCliqueScalingGroup{
				Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
					Replicas: 3,
					// MinAvailable defaults to Replicas when nil
				},
			},
			expectedAvailableReplicas: 2, // 3 - 1
			expectedConditionStatus:   metav1.ConditionTrue,
			expectedConditionReason:   constants.ConditionReasonInsufficientAvailablePCSGReplicas,
		},
		{
			// PCSG with explicit MinAvailable should set AvailableReplicas to MinAvailable-1
			name: "explicit min available",
			initialPCSG: &grovecorev1alpha1.PodCliqueScalingGroup{
				Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
					Replicas:     5,
					MinAvailable: ptr.To(int32(2)),
				},
			},
			expectedAvailableReplicas: 1, // 2 - 1
			expectedConditionStatus:   metav1.ConditionTrue,
			expectedConditionReason:   constants.ConditionReasonInsufficientAvailablePCSGReplicas,
		},
		{
			// PCSG with MinAvailable=0 should set AvailableReplicas to 0
			name: "zero min available",
			initialPCSG: &grovecorev1alpha1.PodCliqueScalingGroup{
				Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
					Replicas:     3,
					MinAvailable: ptr.To(int32(0)),
				},
			},
			expectedAvailableReplicas: 0,
			expectedConditionStatus:   metav1.ConditionTrue,
			expectedConditionReason:   constants.ConditionReasonInsufficientAvailablePCSGReplicas,
		},
		{
			// PCSG with Replicas=0 should handle edge case gracefully
			name: "zero replicas",
			initialPCSG: &grovecorev1alpha1.PodCliqueScalingGroup{
				Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
					Replicas: 0,
				},
			},
			expectedAvailableReplicas: 0,
			expectedConditionStatus:   metav1.ConditionTrue,
			expectedConditionReason:   constants.ConditionReasonInsufficientAvailablePCSGReplicas,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply the option
			option := WithPCSGMinAvailableBreached()
			option(tt.initialPCSG)

			// Verify the condition was set correctly
			require.Len(t, tt.initialPCSG.Status.Conditions, 1)
			condition := tt.initialPCSG.Status.Conditions[0]
			assert.Equal(t, constants.ConditionTypeMinAvailableBreached, condition.Type)
			assert.Equal(t, tt.expectedConditionStatus, condition.Status)
			assert.Equal(t, tt.expectedConditionReason, condition.Reason)

			// Verify AvailableReplicas was set correctly
			assert.Equal(t, tt.expectedAvailableReplicas, tt.initialPCSG.Status.AvailableReplicas)
		})
	}
}

// TestWithPCSGUnknownCondition verifies that the option sets the condition
// to Unknown status and resets AvailableReplicas to 0.
func TestWithPCSGUnknownCondition(t *testing.T) {
	pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{
		Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
			Replicas:     3,
			MinAvailable: ptr.To(int32(2)),
		},
		Status: grovecorev1alpha1.PodCliqueScalingGroupStatus{
			AvailableReplicas: 2, // This should be reset to 0
		},
	}

	// Apply the option
	option := WithPCSGUnknownCondition()
	option(pcsg)

	// Verify the condition was set correctly
	require.Len(t, pcsg.Status.Conditions, 1)
	condition := pcsg.Status.Conditions[0]
	assert.Equal(t, constants.ConditionTypeMinAvailableBreached, condition.Type)
	assert.Equal(t, metav1.ConditionUnknown, condition.Status)
	assert.Equal(t, "UnknownState", condition.Reason)

	// Verify AvailableReplicas was reset to 0
	assert.Equal(t, int32(0), pcsg.Status.AvailableReplicas)
}

// TestWithPCSGObservedGeneration verifies that the option correctly sets
// the ObservedGeneration field.
func TestWithPCSGObservedGeneration(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different generation values
		name string
		// Generation value to set
		generation int64
	}{
		{
			// Positive generation value
			name:       "positive generation",
			generation: 5,
		},
		{
			// Zero generation value
			name:       "zero generation",
			generation: 0,
		},
		{
			// Large generation value
			name:       "large generation",
			generation: 9223372036854775807, // max int64
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{}

			// Apply the option
			option := WithPCSGObservedGeneration(tt.generation)
			option(pcsg)

			// Verify ObservedGeneration was set correctly
			require.NotNil(t, pcsg.Status.ObservedGeneration)
			assert.Equal(t, tt.generation, *pcsg.Status.ObservedGeneration)
		})
	}
}

// TestWithPCSGAvailableReplicas verifies that the option sets the AvailableReplicas
// field without modifying conditions.
func TestWithPCSGAvailableReplicas(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different replica counts
		name string
		// Number of available replicas to set
		availableReplicas int32
		// Initial conditions that should remain unchanged
		initialConditions []metav1.Condition
	}{
		{
			// Zero available replicas
			name:              "zero available replicas",
			availableReplicas: 0,
			initialConditions: []metav1.Condition{
				{
					Type:   constants.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
			},
		},
		{
			// Positive available replicas
			name:              "positive available replicas",
			availableReplicas: 3,
			initialConditions: []metav1.Condition{
				{
					Type:   constants.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionFalse,
					Reason: "SufficientReplicas",
				},
			},
		},
		{
			// No initial conditions
			name:              "no initial conditions",
			availableReplicas: 2,
			initialConditions: []metav1.Condition{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{
				Status: grovecorev1alpha1.PodCliqueScalingGroupStatus{
					Conditions: tt.initialConditions,
				},
			}

			// Apply the option
			option := WithPCSGAvailableReplicas(tt.availableReplicas)
			option(pcsg)

			// Verify AvailableReplicas was set correctly
			assert.Equal(t, tt.availableReplicas, pcsg.Status.AvailableReplicas)

			// Verify conditions were not modified
			assert.Equal(t, tt.initialConditions, pcsg.Status.Conditions)
		})
	}
}

// TestWithPCLQAvailable verifies that the option sets both conditions to their
// positive states and sets ReadyReplicas to match Spec.Replicas.
func TestWithPCLQAvailable(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different replica configurations
		name string
		// Number of replicas in the spec
		specReplicas int32
	}{
		{
			// Single replica PodClique
			name:         "single replica",
			specReplicas: 1,
		},
		{
			// Multiple replica PodClique
			name:         "multiple replicas",
			specReplicas: 5,
		},
		{
			// Zero replica PodClique (edge case)
			name:         "zero replicas",
			specReplicas: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pclq := &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas: tt.specReplicas,
				},
			}

			// Apply the option
			option := WithPCLQAvailable()
			option(pclq)

			// Verify conditions were set correctly
			require.Len(t, pclq.Status.Conditions, 2)

			// Check MinAvailableBreached condition
			minAvailableCondition := findCondition(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
			require.NotNil(t, minAvailableCondition)
			assert.Equal(t, metav1.ConditionFalse, minAvailableCondition.Status)
			assert.Equal(t, "SufficientReadyReplicas", minAvailableCondition.Reason)

			// Check PodCliqueScheduled condition
			scheduledCondition := findCondition(pclq.Status.Conditions, constants.ConditionTypePodCliqueScheduled)
			require.NotNil(t, scheduledCondition)
			assert.Equal(t, metav1.ConditionTrue, scheduledCondition.Status)
			assert.Equal(t, "ScheduledSuccessfully", scheduledCondition.Reason)

			// Verify ReadyReplicas matches spec
			assert.Equal(t, tt.specReplicas, pclq.Status.ReadyReplicas)
		})
	}
}

// TestWithPCLQTerminating verifies that the option sets DeletionTimestamp
// and adds a finalizer to simulate termination.
func TestWithPCLQTerminating(t *testing.T) {
	pclq := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pclq",
			Namespace: "default",
		},
	}

	// Apply the option
	option := WithPCLQTerminating()
	option(pclq)

	// Verify DeletionTimestamp was set
	require.NotNil(t, pclq.DeletionTimestamp)
	assert.WithinDuration(t, time.Now(), pclq.DeletionTimestamp.Time, 5*time.Second)

	// Verify finalizer was added
	require.Len(t, pclq.Finalizers, 1)
	assert.Equal(t, "test-finalizer", pclq.Finalizers[0])
}

// TestWithPCLQMinAvailableBreached verifies that the option sets MinAvailableBreached
// to True while keeping PodCliqueScheduled as True.
func TestWithPCLQMinAvailableBreached(t *testing.T) {
	pclq := &grovecorev1alpha1.PodClique{}

	// Apply the option
	option := WithPCLQMinAvailableBreached()
	option(pclq)

	// Verify conditions were set correctly
	require.Len(t, pclq.Status.Conditions, 2)

	// Check MinAvailableBreached condition
	minAvailableCondition := findCondition(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
	require.NotNil(t, minAvailableCondition)
	assert.Equal(t, metav1.ConditionTrue, minAvailableCondition.Status)
	assert.Equal(t, "InsufficientReadyReplicas", minAvailableCondition.Reason)

	// Check PodCliqueScheduled condition
	scheduledCondition := findCondition(pclq.Status.Conditions, constants.ConditionTypePodCliqueScheduled)
	require.NotNil(t, scheduledCondition)
	assert.Equal(t, metav1.ConditionTrue, scheduledCondition.Status)
	assert.Equal(t, "ScheduledSuccessfully", scheduledCondition.Reason)
}

// TestWithPCLQNotScheduled verifies that the option sets PodCliqueScheduled
// to False to simulate scheduling failure.
func TestWithPCLQNotScheduled(t *testing.T) {
	pclq := &grovecorev1alpha1.PodClique{}

	// Apply the option
	option := WithPCLQNotScheduled()
	option(pclq)

	// Verify condition was set correctly
	require.Len(t, pclq.Status.Conditions, 1)

	scheduledCondition := findCondition(pclq.Status.Conditions, constants.ConditionTypePodCliqueScheduled)
	require.NotNil(t, scheduledCondition)
	assert.Equal(t, metav1.ConditionFalse, scheduledCondition.Status)
	assert.Equal(t, "SchedulingFailed", scheduledCondition.Reason)
}

// TestWithPCLQScheduledAndAvailable verifies that this option is an alias
// for WithPCLQAvailable and produces the same results.
func TestWithPCLQScheduledAndAvailable(t *testing.T) {
	pclq1 := &grovecorev1alpha1.PodClique{
		Spec: grovecorev1alpha1.PodCliqueSpec{Replicas: 3},
	}
	pclq2 := &grovecorev1alpha1.PodClique{
		Spec: grovecorev1alpha1.PodCliqueSpec{Replicas: 3},
	}

	// Apply both options
	WithPCLQScheduledAndAvailable()(pclq1)
	WithPCLQAvailable()(pclq2)

	// Results should be identical
	assert.Equal(t, pclq2.Status.Conditions, pclq1.Status.Conditions)
	assert.Equal(t, pclq2.Status.ReadyReplicas, pclq1.Status.ReadyReplicas)
}

// TestWithPCLQScheduledButBreached verifies that this option is an alias
// for WithPCLQMinAvailableBreached and produces the same results.
func TestWithPCLQScheduledButBreached(t *testing.T) {
	pclq1 := &grovecorev1alpha1.PodClique{}
	pclq2 := &grovecorev1alpha1.PodClique{}

	// Apply both options
	WithPCLQScheduledButBreached()(pclq1)
	WithPCLQMinAvailableBreached()(pclq2)

	// Results should be identical
	assert.Equal(t, pclq2.Status.Conditions, pclq1.Status.Conditions)
}

// TestWithPCLQNoConditions verifies that the option removes all conditions
// from the PodClique status.
func TestWithPCLQNoConditions(t *testing.T) {
	pclq := &grovecorev1alpha1.PodClique{
		Status: grovecorev1alpha1.PodCliqueStatus{
			Conditions: []metav1.Condition{
				{
					Type:   constants.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
				{
					Type:   constants.ConditionTypePodCliqueScheduled,
					Status: metav1.ConditionFalse,
					Reason: "AnotherReason",
				},
			},
		},
	}

	// Apply the option
	option := WithPCLQNoConditions()
	option(pclq)

	// Verify all conditions were removed
	assert.Empty(t, pclq.Status.Conditions)
}

// TestWithPCLQReplicaReadyStatus verifies that the option sets ReadyReplicas
// without modifying conditions.
func TestWithPCLQReplicaReadyStatus(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different ready replica counts
		name string
		// Number of ready replicas to set
		readyReplicas int32
		// Initial conditions that should remain unchanged
		initialConditions []metav1.Condition
	}{
		{
			// Zero ready replicas
			name:          "zero ready replicas",
			readyReplicas: 0,
			initialConditions: []metav1.Condition{
				{
					Type:   constants.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: "TestReason",
				},
			},
		},
		{
			// Positive ready replicas
			name:          "positive ready replicas",
			readyReplicas: 4,
			initialConditions: []metav1.Condition{
				{
					Type:   constants.ConditionTypePodCliqueScheduled,
					Status: metav1.ConditionTrue,
					Reason: "Scheduled",
				},
			},
		},
		{
			// No initial conditions
			name:              "no initial conditions",
			readyReplicas:     2,
			initialConditions: []metav1.Condition{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pclq := &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					Conditions: tt.initialConditions,
				},
			}

			// Apply the option
			option := WithPCLQReplicaReadyStatus(tt.readyReplicas)
			option(pclq)

			// Verify ReadyReplicas was set correctly
			assert.Equal(t, tt.readyReplicas, pclq.Status.ReadyReplicas)

			// Verify conditions were not modified
			assert.Equal(t, tt.initialConditions, pclq.Status.Conditions)
		})
	}
}

// TestPCSGOptionChaining verifies that multiple PCSG options can be chained
// together and applied in sequence.
func TestPCSGOptionChaining(t *testing.T) {
	pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{
		Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
			Replicas:     5,
			MinAvailable: ptr.To(int32(3)),
		},
	}

	// Apply multiple options in sequence
	WithPCSGMinAvailableBreached()(pcsg)
	WithPCSGObservedGeneration(10)(pcsg)
	WithPCSGAvailableReplicas(1)(pcsg)

	// Verify all options were applied
	require.Len(t, pcsg.Status.Conditions, 1)
	condition := pcsg.Status.Conditions[0]
	assert.Equal(t, constants.ConditionTypeMinAvailableBreached, condition.Type)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)

	require.NotNil(t, pcsg.Status.ObservedGeneration)
	assert.Equal(t, int64(10), *pcsg.Status.ObservedGeneration)

	assert.Equal(t, int32(1), pcsg.Status.AvailableReplicas)
}

// TestPCLQOptionChaining verifies that multiple PCLQ options can be chained
// together and applied in sequence.
func TestPCLQOptionChaining(t *testing.T) {
	pclq := &grovecorev1alpha1.PodClique{
		Spec: grovecorev1alpha1.PodCliqueSpec{
			Replicas: 3,
		},
	}

	// Apply multiple options in sequence
	WithPCLQAvailable()(pclq)
	WithPCLQReplicaReadyStatus(2)(pclq)

	// Verify conditions from first option
	require.Len(t, pclq.Status.Conditions, 2)
	minAvailableCondition := findCondition(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
	require.NotNil(t, minAvailableCondition)
	assert.Equal(t, metav1.ConditionFalse, minAvailableCondition.Status)

	scheduledCondition := findCondition(pclq.Status.Conditions, constants.ConditionTypePodCliqueScheduled)
	require.NotNil(t, scheduledCondition)
	assert.Equal(t, metav1.ConditionTrue, scheduledCondition.Status)

	// Verify ReadyReplicas was overridden by second option
	assert.Equal(t, int32(2), pclq.Status.ReadyReplicas)
}

// findCondition is a helper function that finds a condition by type in a slice of conditions.
// Returns nil if the condition is not found.
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
