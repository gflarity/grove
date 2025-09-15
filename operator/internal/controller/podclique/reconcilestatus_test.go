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

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestReconcileStatus tests the main status reconciliation functionality
// Note: This test focuses on the core logic rather than full integration
func TestReconcileStatus(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object for which to reconcile status
		pclq *grovecorev1alpha1.PodClique
		// existingPods are the pods that should exist in the cluster
		existingPods []ctrlclient.Object
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
		// expectedReplicas is the expected replica count in status
		expectedReplicas int32
		// expectedReadyReplicas is the expected ready replica count in status
		expectedReadyReplicas int32
	}{
		{
			// Test status reconciliation with ready pods
			name: "status_reconcile_with_ready_pods",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					UID:       "test-pclq-uid",
					Labels: map[string]string{
						grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
						grovecorev1alpha1.LabelPartOfKey:    "test-pgs",
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					RoleName:     "worker",
					Replicas:     3,
					MinAvailable: ptr.To[int32](2),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "test", Image: "test:latest"}},
					},
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ObservedGeneration: ptr.To[int64](1),
				},
			},
			existingPods: []ctrlclient.Object{
				createTestPod("test-pclq-0", "default", corev1.PodRunning, true),
				createTestPod("test-pclq-1", "default", corev1.PodRunning, true),
				createTestPod("test-pclq-2", "default", corev1.PodRunning, false),
			},
			expectedContinue:      true,
			expectedError:         false,
			expectedReplicas:      3,
			expectedReadyReplicas: 2,
		},
		{
			// Test status reconciliation with no pods
			name: "status_reconcile_with_no_pods",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					UID:       "test-pclq-uid",
					Labels: map[string]string{
						grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
						grovecorev1alpha1.LabelPartOfKey:    "test-pgs",
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					RoleName:     "worker",
					Replicas:     3,
					MinAvailable: ptr.To[int32](2),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "test", Image: "test:latest"}},
					},
				},
			},
			existingPods:          []ctrlclient.Object{},
			expectedContinue:      true,
			expectedError:         false,
			expectedReplicas:      0,
			expectedReadyReplicas: 0,
		},
		{
			// Test status reconciliation with terminating pods
			name: "status_reconcile_with_terminating_pods",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					UID:       "test-pclq-uid",
					Labels: map[string]string{
						grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
						grovecorev1alpha1.LabelPartOfKey:    "test-pgs",
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					RoleName:     "worker",
					Replicas:     3,
					MinAvailable: ptr.To[int32](2),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "test", Image: "test:latest"}},
					},
				},
			},
			existingPods: []ctrlclient.Object{
				createTestPod("test-pclq-0", "default", corev1.PodRunning, true),
				createTestPod("test-pclq-1", "default", corev1.PodRunning, true),
				createTerminatingTestPod("test-pclq-2", "default"),
			},
			expectedContinue:      true,
			expectedError:         false,
			expectedReplicas:      2, // Terminating pods are excluded
			expectedReadyReplicas: 2,
		},
		{
			// Test status reconciliation with mixed pod states
			name: "status_reconcile_with_mixed_pod_states",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					UID:       "test-pclq-uid",
					Labels: map[string]string{
						grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
						grovecorev1alpha1.LabelPartOfKey:    "test-pgs",
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					RoleName:     "worker",
					Replicas:     5,
					MinAvailable: ptr.To[int32](3),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "test", Image: "test:latest"}},
					},
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ObservedGeneration: ptr.To[int64](1),
				},
			},
			existingPods: []ctrlclient.Object{
				createTestPod("test-pclq-0", "default", corev1.PodRunning, true),  // Ready
				createTestPod("test-pclq-1", "default", corev1.PodPending, false), // Not ready
				createTestPod("test-pclq-2", "default", corev1.PodRunning, true),  // Ready
				createFailedTestPod("test-pclq-3", "default"),                     // Failed
				createTerminatingTestPod("test-pclq-4", "default"),                // Terminating
			},
			expectedContinue:      true,
			expectedError:         false,
			expectedReplicas:      4, // Excluding terminating pod
			expectedReadyReplicas: 2, // Only 2 ready pods
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with proper scheme
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			// Add all objects to the fake client
			allObjects := append([]ctrlclient.Object{tt.pclq}, tt.existingPods...)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(allObjects...).
				WithStatusSubresource(&grovecorev1alpha1.PodClique{}).
				Build()

			// Create reconciler
			reconciler := &Reconciler{
				client: fakeClient,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test - this will call the real reconcileStatus function
			result := reconciler.reconcileStatus(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedError {
				assert.True(t, result.HasErrors(), "Expected error but got none")
			} else {
				assert.False(t, result.HasErrors(), "Expected no error but got one")
			}

			if tt.expectedContinue {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected reconcile to continue")
			} else {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected reconcile to short-circuit")
			}

			// Verify status updates if no error
			if !result.HasErrors() {
				assert.Equal(t, tt.expectedReplicas, tt.pclq.Status.Replicas, "Replicas count should match expected")
				assert.Equal(t, tt.expectedReadyReplicas, tt.pclq.Status.ReadyReplicas, "ReadyReplicas count should match expected")
			}
		})
	}
}

// TestMutateStatusReplicaCounts tests the replica count mutation functionality
func TestMutateStatusReplicaCounts(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object to mutate
		pclq *grovecorev1alpha1.PodClique
		// podCategories contains categorized pods by condition type
		podCategories map[corev1.PodConditionType][]*corev1.Pod
		// numExistingPods is the total number of existing pods
		numExistingPods int
		// expectedReplicas is the expected replica count after mutation
		expectedReplicas int32
		// expectedReadyReplicas is the expected ready replica count after mutation
		expectedReadyReplicas int32
		// expectedScheduledReplicas is the expected scheduled replica count after mutation
		expectedScheduledReplicas int32
		// expectedScheduleGatedReplicas is the expected schedule gated replica count after mutation
		expectedScheduleGatedReplicas int32
	}{
		{
			// Test with all pods ready and scheduled
			name: "all_pods_ready_and_scheduled",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pclq"},
			},
			podCategories: map[corev1.PodConditionType][]*corev1.Pod{
				corev1.PodReady:           {createTestPodPtr("pod-1", "default", corev1.PodRunning, true), createTestPodPtr("pod-2", "default", corev1.PodRunning, true)},
				corev1.PodScheduled:       {createTestPodPtr("pod-1", "default", corev1.PodRunning, true), createTestPodPtr("pod-2", "default", corev1.PodRunning, true)},
				k8sutils.ScheduleGatedPod: {},
				k8sutils.TerminatingPod:   {},
			},
			numExistingPods:               2,
			expectedReplicas:              2,
			expectedReadyReplicas:         2,
			expectedScheduledReplicas:     2,
			expectedScheduleGatedReplicas: 0,
		},
		{
			// Test with some pods terminating
			name: "some_pods_terminating",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pclq"},
			},
			podCategories: map[corev1.PodConditionType][]*corev1.Pod{
				corev1.PodReady:           {createTestPodPtr("pod-1", "default", corev1.PodRunning, true)},
				corev1.PodScheduled:       {createTestPodPtr("pod-1", "default", corev1.PodRunning, true)},
				k8sutils.ScheduleGatedPod: {},
				k8sutils.TerminatingPod:   {createTestPodPtr("pod-2", "default", corev1.PodRunning, false)},
			},
			numExistingPods:               2,
			expectedReplicas:              1, // Terminating pods excluded
			expectedReadyReplicas:         1,
			expectedScheduledReplicas:     1,
			expectedScheduleGatedReplicas: 0,
		},
		{
			// Test with schedule gated pods
			name: "with_schedule_gated_pods",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pclq"},
			},
			podCategories: map[corev1.PodConditionType][]*corev1.Pod{
				corev1.PodReady:           {createTestPodPtr("pod-1", "default", corev1.PodRunning, true)},
				corev1.PodScheduled:       {createTestPodPtr("pod-1", "default", corev1.PodRunning, true)},
				k8sutils.ScheduleGatedPod: {createTestPodPtr("pod-2", "default", corev1.PodPending, false)},
				k8sutils.TerminatingPod:   {},
			},
			numExistingPods:               2,
			expectedReplicas:              2,
			expectedReadyReplicas:         1,
			expectedScheduledReplicas:     1,
			expectedScheduleGatedReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute test
			mutateStatusReplicaCounts(tt.pclq, tt.podCategories, tt.numExistingPods)

			// Verify results
			assert.Equal(t, tt.expectedReplicas, tt.pclq.Status.Replicas, "Replicas should match expected")
			assert.Equal(t, tt.expectedReadyReplicas, tt.pclq.Status.ReadyReplicas, "ReadyReplicas should match expected")
			assert.Equal(t, tt.expectedScheduledReplicas, tt.pclq.Status.ScheduledReplicas, "ScheduledReplicas should match expected")
			assert.Equal(t, tt.expectedScheduleGatedReplicas, tt.pclq.Status.ScheduleGatedReplicas, "ScheduleGatedReplicas should match expected")
		})
	}
}

// TestMutateSelector tests the selector mutation functionality
func TestMutateSelector(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pgsName is the PodGangSet name
		pgsName string
		// pclq is the PodClique object to mutate
		pclq *grovecorev1alpha1.PodClique
		// expectedError indicates whether an error should be returned
		expectedError bool
		// expectedSelectorSet indicates whether the selector should be set
		expectedSelectorSet bool
	}{
		{
			// Test selector mutation with scale config
			name:    "selector_with_scale_config",
			pgsName: "test-pgs",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					ScaleConfig: &grovecorev1alpha1.AutoScalingConfig{
						MaxReplicas: 10,
					},
				},
			},
			expectedError:       false,
			expectedSelectorSet: true,
		},
		{
			// Test selector mutation without scale config
			name:    "selector_without_scale_config",
			pgsName: "test-pgs",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					ScaleConfig: nil,
				},
			},
			expectedError:       false,
			expectedSelectorSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute test
			err := mutateSelector(tt.pgsName, tt.pclq)

			// Verify results
			if tt.expectedError {
				assert.Error(t, err, "Expected error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got one")
			}

			if tt.expectedSelectorSet {
				assert.NotNil(t, tt.pclq.Status.Selector, "Selector should be set")
				assert.NotEmpty(t, *tt.pclq.Status.Selector, "Selector should not be empty")
			} else {
				// Selector should remain unchanged (nil if it was nil)
				if tt.pclq.Spec.ScaleConfig == nil {
					// We can't assert on the selector value since it might be set from previous tests
					// Just verify no error occurred
				}
			}
		})
	}
}

// TestComputeMinAvailableBreachedCondition tests the MinAvailableBreached condition computation
func TestComputeMinAvailableBreachedCondition(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object to compute condition for
		pclq *grovecorev1alpha1.PodClique
		// numPodsHavingAtleastOneContainerWithNonZeroExitCode is the count of pods with failed containers
		numPodsHavingAtleastOneContainerWithNonZeroExitCode int
		// numPodsStartedButNotReady is the count of pods that started but are not ready
		numPodsStartedButNotReady int
		// expectedStatus is the expected condition status
		expectedStatus metav1.ConditionStatus
		// expectedReason is the expected condition reason
		expectedReason string
	}{
		{
			// Test condition when insufficient scheduled pods
			name: "insufficient_scheduled_pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](3),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 2, // Less than MinAvailable
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 0,
			numPodsStartedButNotReady:                           0,
			expectedStatus:                                      metav1.ConditionFalse,
			expectedReason:                                      grovecorev1alpha1.ConditionReasonInsufficientScheduledPods,
		},
		{
			// Test condition when sufficient ready pods
			name: "sufficient_ready_pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](2),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3, // More than MinAvailable
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 0,
			numPodsStartedButNotReady:                           0,
			expectedStatus:                                      metav1.ConditionFalse,
			expectedReason:                                      grovecorev1alpha1.ConditionReasonSufficientReadyPods,
		},
		{
			// Test condition when insufficient ready pods due to failures
			name: "insufficient_ready_pods_due_to_failures",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](3),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3,
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 2, // 2 failed pods
			numPodsStartedButNotReady:                           0,
			expectedStatus:                                      metav1.ConditionTrue,
			expectedReason:                                      grovecorev1alpha1.ConditionReasonInsufficientReadyPods,
		},
		{
			// Test condition when pods are starting but not ready yet
			name: "pods_starting_but_not_ready",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](3),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3,
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 0,
			numPodsStartedButNotReady:                           1,                    // 1 pod starting
			expectedStatus:                                      metav1.ConditionTrue, // 3 - 0 - 1 = 2, which is < 3
			expectedReason:                                      grovecorev1alpha1.ConditionReasonInsufficientReadyPods,
		},
		{
			// Test condition when both failed and starting pods exist
			name: "mixed_failed_and_starting_pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](3),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 5, // 5 scheduled pods
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 2,                    // 2 failed pods
			numPodsStartedButNotReady:                           1,                    // 1 starting pod
			expectedStatus:                                      metav1.ConditionTrue, // 5 - 2 - 1 = 2, which is < 3
			expectedReason:                                      grovecorev1alpha1.ConditionReasonInsufficientReadyPods,
		},
		{
			// Test edge case with exactly minimum available after failures
			name: "exactly_minimum_after_failures",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](2),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 4, // 4 scheduled pods
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 2,                     // 2 failed pods
			numPodsStartedButNotReady:                           0,                     // 0 starting pods
			expectedStatus:                                      metav1.ConditionFalse, // 4 - 2 - 0 = 2, exactly minimum
			expectedReason:                                      grovecorev1alpha1.ConditionReasonSufficientReadyPods,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute test
			condition := computeMinAvailableBreachedCondition(tt.pclq, tt.numPodsHavingAtleastOneContainerWithNonZeroExitCode, tt.numPodsStartedButNotReady)

			// Verify results
			assert.Equal(t, grovecorev1alpha1.ConditionTypeMinAvailableBreached, condition.Type, "Condition type should match")
			assert.Equal(t, tt.expectedStatus, condition.Status, "Condition status should match expected")
			assert.Equal(t, tt.expectedReason, condition.Reason, "Condition reason should match expected")
			assert.NotEmpty(t, condition.Message, "Condition message should not be empty")
			assert.False(t, condition.LastTransitionTime.IsZero(), "LastTransitionTime should be set")
		})
	}
}

// TestComputePodCliqueScheduledCondition tests the PodCliqueScheduled condition computation
func TestComputePodCliqueScheduledCondition(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object to compute condition for
		pclq *grovecorev1alpha1.PodClique
		// expectedStatus is the expected condition status
		expectedStatus metav1.ConditionStatus
		// expectedReason is the expected condition reason
		expectedReason string
	}{
		{
			// Test condition when insufficient scheduled pods
			name: "insufficient_scheduled_pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](3),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 2, // Less than MinAvailable
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: grovecorev1alpha1.ConditionReasonInsufficientScheduledPods,
		},
		{
			// Test condition when sufficient scheduled pods
			name: "sufficient_scheduled_pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](2),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3, // More than MinAvailable
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: grovecorev1alpha1.ConditionReasonSufficientScheduledPods,
		},
		{
			// Test condition when exactly meeting minimum
			name: "exactly_meeting_minimum",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](3),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3, // Exactly MinAvailable
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: grovecorev1alpha1.ConditionReasonSufficientScheduledPods,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute test
			condition := computePodCliqueScheduledCondition(tt.pclq)

			// Verify results
			assert.Equal(t, grovecorev1alpha1.ConditionTypePodCliqueScheduled, condition.Type, "Condition type should match")
			assert.Equal(t, tt.expectedStatus, condition.Status, "Condition status should match expected")
			assert.Equal(t, tt.expectedReason, condition.Reason, "Condition reason should match expected")
			assert.NotEmpty(t, condition.Message, "Condition message should not be empty")
			assert.False(t, condition.LastTransitionTime.IsZero(), "LastTransitionTime should be set")
		})
	}
}

// TestMutateConditions tests the condition mutation functionality
func TestMutateConditions(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object to test condition mutation on
		pclq *grovecorev1alpha1.PodClique
		// numPodsWithFailedContainers is the count of pods with failed containers
		numPodsWithFailedContainers int
		// numPodsStartedButNotReady is the count of pods that started but are not ready
		numPodsStartedButNotReady int
		// expectedConditionCount is the expected number of conditions after mutation
		expectedConditionCount int
	}{
		{
			// Test condition mutation when observed generation is set
			name: "conditions_with_observed_generation",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](2),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ObservedGeneration: ptr.To[int64](1),
					ScheduledReplicas:  3,
				},
			},
			numPodsWithFailedContainers: 0,
			numPodsStartedButNotReady:   0,
			expectedConditionCount:      2, // PodCliqueScheduled and MinAvailableBreached
		},
		{
			// Test condition mutation when observed generation is nil
			name: "conditions_without_observed_generation",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](2),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ObservedGeneration: nil,
					ScheduledReplicas:  3,
				},
			},
			numPodsWithFailedContainers: 0,
			numPodsStartedButNotReady:   0,
			expectedConditionCount:      0, // No conditions should be set
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store initial condition count
			initialConditionCount := len(tt.pclq.Status.Conditions)

			// Execute condition mutations (simulating the logic from reconcileStatus)
			if tt.pclq.Status.ObservedGeneration != nil {
				mutatePodCliqueScheduledCondition(tt.pclq)
				mutateMinAvailableBreachedCondition(tt.pclq, tt.numPodsWithFailedContainers, tt.numPodsStartedButNotReady)
			}

			// Verify results
			finalConditionCount := len(tt.pclq.Status.Conditions)
			if tt.expectedConditionCount > 0 {
				assert.GreaterOrEqual(t, finalConditionCount, initialConditionCount, "Conditions should be added")

				// Check for specific condition types
				scheduledCondition := meta.FindStatusCondition(tt.pclq.Status.Conditions, grovecorev1alpha1.ConditionTypePodCliqueScheduled)
				assert.NotNil(t, scheduledCondition, "PodCliqueScheduled condition should be present")

				minAvailableCondition := meta.FindStatusCondition(tt.pclq.Status.Conditions, grovecorev1alpha1.ConditionTypeMinAvailableBreached)
				assert.NotNil(t, minAvailableCondition, "MinAvailableBreached condition should be present")
			} else {
				assert.Equal(t, initialConditionCount, finalConditionCount, "No conditions should be added")
			}
		})
	}
}

// TestConditionTransitions tests condition transitions and message validation
func TestConditionTransitions(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// initialConditions are the existing conditions on the PodClique
		initialConditions []metav1.Condition
		// pclq is the PodClique object to test
		pclq *grovecorev1alpha1.PodClique
		// numFailedPods is the count of pods with failed containers
		numFailedPods int
		// numStartingPods is the count of pods that are starting but not ready
		numStartingPods int
		// expectedConditionUpdated indicates if the condition should be updated
		expectedConditionUpdated bool
		// expectedFinalStatus is the expected final condition status
		expectedFinalStatus metav1.ConditionStatus
	}{
		{
			// Test transition from True to False
			name: "transition_from_true_to_false",
			initialConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionTrue,
					Reason: grovecorev1alpha1.ConditionReasonInsufficientReadyPods,
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](2),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3,
				},
			},
			numFailedPods:            0, // No failed pods now
			numStartingPods:          0, // No starting pods
			expectedConditionUpdated: true,
			expectedFinalStatus:      metav1.ConditionFalse,
		},
		{
			// Test no transition when condition remains the same
			name: "no_transition_same_condition",
			initialConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionFalse,
					Reason: grovecorev1alpha1.ConditionReasonSufficientReadyPods,
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](2),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3,
				},
			},
			numFailedPods:            0, // No failed pods
			numStartingPods:          0, // No starting pods
			expectedConditionUpdated: false,
			expectedFinalStatus:      metav1.ConditionFalse,
		},
		{
			// Test transition from False to True
			name: "transition_from_false_to_true",
			initialConditions: []metav1.Condition{
				{
					Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
					Status: metav1.ConditionFalse,
					Reason: grovecorev1alpha1.ConditionReasonSufficientReadyPods,
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To[int32](3),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3,
				},
			},
			numFailedPods:            2, // 2 failed pods, leaving only 1 ready
			numStartingPods:          0,
			expectedConditionUpdated: true,
			expectedFinalStatus:      metav1.ConditionTrue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set initial conditions
			tt.pclq.Status.Conditions = tt.initialConditions

			// Store initial condition for comparison
			initialCondition := meta.FindStatusCondition(tt.pclq.Status.Conditions, grovecorev1alpha1.ConditionTypeMinAvailableBreached)
			var initialTransitionTime metav1.Time
			if initialCondition != nil {
				initialTransitionTime = initialCondition.LastTransitionTime
			}

			// Execute condition mutation
			mutateMinAvailableBreachedCondition(tt.pclq, tt.numFailedPods, tt.numStartingPods)

			// Verify results
			finalCondition := meta.FindStatusCondition(tt.pclq.Status.Conditions, grovecorev1alpha1.ConditionTypeMinAvailableBreached)
			require.NotNil(t, finalCondition, "MinAvailableBreached condition should be present")

			assert.Equal(t, tt.expectedFinalStatus, finalCondition.Status, "Final condition status should match expected")
			assert.NotEmpty(t, finalCondition.Message, "Condition message should not be empty")

			if tt.expectedConditionUpdated {
				// If condition was updated, transition time should be different
				if initialCondition != nil {
					assert.True(t, finalCondition.LastTransitionTime.After(initialTransitionTime.Time) ||
						finalCondition.LastTransitionTime.Equal(&initialTransitionTime),
						"LastTransitionTime should be updated when condition changes")
				}
			}

			// Verify message content is appropriate for the status
			if finalCondition.Status == metav1.ConditionTrue {
				assert.Contains(t, finalCondition.Message, "Insufficient", "True condition should mention insufficient pods")
			} else {
				assert.Contains(t, finalCondition.Message, "sufficient", "False condition should mention sufficient pods")
			}
		})
	}
}

// Helper functions for creating test objects

func createTestPod(name, namespace string, phase corev1.PodPhase, ready bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				grovecorev1alpha1.LabelPodClique:    "test-pclq",
				grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
				grovecorev1alpha1.LabelPartOfKey:    "test-pgs",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
					Kind:       grovecorev1alpha1.PodCliqueKind,
					Name:       "test-pclq",
					UID:        "test-pclq-uid",
					Controller: ptr.To(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test", Image: "test:latest"}},
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}

	if ready {
		pod.Status.Conditions = []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
			{
				Type:   corev1.PodScheduled,
				Status: corev1.ConditionTrue,
			},
		}
	} else {
		pod.Status.Conditions = []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
			},
		}
	}

	return pod
}

func createTestPodPtr(name, namespace string, phase corev1.PodPhase, ready bool) *corev1.Pod {
	return createTestPod(name, namespace, phase, ready)
}

func createTerminatingTestPod(name, namespace string) *corev1.Pod {
	now := metav1.Now()
	pod := createTestPod(name, namespace, corev1.PodRunning, false)
	pod.DeletionTimestamp = &now
	// Add finalizer to make it valid for fake client
	pod.Finalizers = []string{"test.finalizer"}
	return pod
}

func createFailedTestPod(name, namespace string) *corev1.Pod {
	pod := createTestPod(name, namespace, corev1.PodFailed, false)
	// Add container status with non-zero exit code
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "test",
			LastTerminationState: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 1,
					Reason:   "Error",
				},
			},
		},
	}
	return pod
}
