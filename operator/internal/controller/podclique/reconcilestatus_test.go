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

package podclique

import (
	"testing"

	"github.com/ai-dynamo/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestComputeMinAvailableBreachedCondition(t *testing.T) {
	tests := []struct {
		name               string
		pclq               *grovecorev1alpha1.PodClique
		oldReadyReplicas   int32
		expectedStatus     metav1.ConditionStatus
		expectedReason     string
		preserveTransition bool // if true, we should preserve the existing condition
	}{
		{
			name: "Transition from healthy to unhealthy - sets True",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ReadyReplicas: 2, // current ready
				},
			},
			oldReadyReplicas: 3, // was healthy
			expectedStatus:   metav1.ConditionTrue,
			expectedReason:   constants.ConditionReasonInsufficientReadyPods,
		},
		{
			name: "Already unhealthy, no transition - preserves existing True condition",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ReadyReplicas: 1, // current ready
					Conditions: []metav1.Condition{
						{
							Type:   constants.ConditionTypeMinAvailableBreached,
							Status: metav1.ConditionTrue,
							Reason: constants.ConditionReasonInsufficientReadyPods,
						},
					},
				},
			},
			oldReadyReplicas:   2, // was already unhealthy
			expectedStatus:     metav1.ConditionTrue,
			expectedReason:     constants.ConditionReasonInsufficientReadyPods,
			preserveTransition: true,
		},
		{
			name: "Recovered from unhealthy to healthy - sets False",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ReadyReplicas: 3, // current ready
				},
			},
			oldReadyReplicas: 2, // was unhealthy
			expectedStatus:   metav1.ConditionFalse,
			expectedReason:   constants.ConditionReasonSufficientReadyPods,
		},
		{
			name: "Stable healthy state - sets False",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ReadyReplicas: 3, // current ready
				},
			},
			oldReadyReplicas: 3, // was healthy
			expectedStatus:   metav1.ConditionFalse,
			expectedReason:   constants.ConditionReasonSufficientReadyPods,
		},
		{
			name: "Update in progress - sets Unknown",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ReadyReplicas: 2,
					RollingUpdateProgress: &grovecorev1alpha1.PodCliqueRollingUpdateProgress{
						UpdateStartedAt:            metav1.Now(),
						UpdateEndedAt:              nil, // still in progress
						PodCliqueSetGenerationHash: "abc123",
						PodTemplateHash:            "xyz789",
					},
				},
			},
			oldReadyReplicas: 3,
			expectedStatus:   metav1.ConditionUnknown,
			expectedReason:   constants.ConditionReasonUpdateInProgress,
		},
		{
			name: "Already unhealthy with no existing condition - sets False (no transition)",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ReadyReplicas: 1, // current ready
					Conditions:    []metav1.Condition{},
				},
			},
			oldReadyReplicas: 2, // was already unhealthy
			expectedStatus:   metav1.ConditionFalse,
			expectedReason:   constants.ConditionReasonNoTransitionDetected,
		},
		{
			name: "Above MinAvailable after being below - sets False",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ReadyReplicas: 5, // well above minAvailable
				},
			},
			oldReadyReplicas: 1, // was well below
			expectedStatus:   metav1.ConditionFalse,
			expectedReason:   constants.ConditionReasonSufficientReadyPods,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := computeMinAvailableBreachedCondition(tc.pclq, tc.oldReadyReplicas)

			assert.Equal(t, constants.ConditionTypeMinAvailableBreached, result.Type)
			assert.Equal(t, tc.expectedStatus, result.Status)
			assert.Equal(t, tc.expectedReason, result.Reason)
		})
	}
}

func TestComputeMinAvailableBreachedCondition_UnscheduledPodsIncluded(t *testing.T) {
	// This test verifies that unscheduled pods are NOT excluded from the breach calculation
	// (the old guard based on scheduledReplicas has been removed)
	pclq := &grovecorev1alpha1.PodClique{
		Spec: grovecorev1alpha1.PodCliqueSpec{
			MinAvailable: ptr.To(int32(3)),
		},
		Status: grovecorev1alpha1.PodCliqueStatus{
			ReadyReplicas:     0, // no ready pods
			ScheduledReplicas: 0, // no scheduled pods
		},
	}

	// With the old behavior, this would return False because scheduledReplicas < minAvailable
	// With the new behavior, since oldReadyReplicas (3) >= minAvailable and currentReadyReplicas (0) < minAvailable,
	// it should detect a transition and return True
	result := computeMinAvailableBreachedCondition(pclq, 3)

	assert.Equal(t, metav1.ConditionTrue, result.Status,
		"Breach should be detected even when pods are unscheduled (scheduledReplicas guard removed)")
	assert.Equal(t, constants.ConditionReasonInsufficientReadyPods, result.Reason)
}

func TestComputeMinAvailableBreachedCondition_PreservesLastTransitionTime(t *testing.T) {
	// Test that LastTransitionTime is preserved when condition status doesn't change
	originalTime := metav1.NewTime(metav1.Now().Add(-1 * 60 * 1000000000)) // 1 minute ago

	tests := []struct {
		name                    string
		existingConditionStatus metav1.ConditionStatus
		newStatus               metav1.ConditionStatus
		oldReadyReplicas        int32
		currentReadyReplicas    int32
		minAvailable            int32
	}{
		{
			name:                    "Status stays False (healthy) - preserves time",
			existingConditionStatus: metav1.ConditionFalse,
			newStatus:               metav1.ConditionFalse,
			oldReadyReplicas:        3,
			currentReadyReplicas:    3,
			minAvailable:            3,
		},
		{
			name:                    "Status stays True (unhealthy) - preserves time",
			existingConditionStatus: metav1.ConditionTrue,
			newStatus:               metav1.ConditionTrue,
			oldReadyReplicas:        1, // was already unhealthy
			currentReadyReplicas:    1,
			minAvailable:            3,
		},
		{
			name:                    "Status changes False to True - new time",
			existingConditionStatus: metav1.ConditionFalse,
			newStatus:               metav1.ConditionTrue,
			oldReadyReplicas:        3, // was healthy
			currentReadyReplicas:    1,
			minAvailable:            3,
		},
		{
			name:                    "Status changes True to False - new time",
			existingConditionStatus: metav1.ConditionTrue,
			newStatus:               metav1.ConditionFalse,
			oldReadyReplicas:        1,
			currentReadyReplicas:    3, // now healthy
			minAvailable:            3,
		},
		// Note: Update in progress always gets a new timestamp (state machine reset)
		// This is tested separately in TestComputeMinAvailableBreachedCondition
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pclq := &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(tc.minAvailable),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ReadyReplicas: tc.currentReadyReplicas,
					Conditions: []metav1.Condition{
						{
							Type:               constants.ConditionTypeMinAvailableBreached,
							Status:             tc.existingConditionStatus,
							Reason:             "TestReason",
							LastTransitionTime: originalTime,
						},
					},
				},
			}

			result := computeMinAvailableBreachedCondition(pclq, tc.oldReadyReplicas)

			assert.Equal(t, tc.newStatus, result.Status, "Unexpected condition status")

			// When status doesn't change, LastTransitionTime should be preserved
			if tc.existingConditionStatus == tc.newStatus {
				assert.Equal(t, originalTime, result.LastTransitionTime,
					"LastTransitionTime should be preserved when status doesn't change")
			} else {
				assert.NotEqual(t, originalTime, result.LastTransitionTime,
					"LastTransitionTime should be updated when status changes")
			}

		})
	}
}
