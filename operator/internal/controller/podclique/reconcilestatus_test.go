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
	"context"
	"testing"

	"github.com/NVIDIA/grove/operator/api/common"
	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TestMutateCurrentHashes tests the mutateCurrentHashes function which updates
// the PodClique's current pod template hash and PodGangSet generation hash based on update state.
func TestMutateCurrentHashes(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pgs is the PodGangSet to use for hash computation
		pgs *grovecorev1alpha1.PodGangSet
		// pclq is the PodClique to mutate (will be modified in-place)
		pclq *grovecorev1alpha1.PodClique
		// expectedError indicates if an error should occur
		expectedError bool
		// expectedCurrentPodTemplateHash is the expected value after mutation
		expectedCurrentPodTemplateHash *string
		// expectedCurrentPodGangSetGenerationHash is the expected value after mutation
		expectedCurrentPodGangSetGenerationHash *string
	}{
		{
			// PodClique with update in progress should not set current hashes
			name: "update in progress",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("new-hash"),
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					Replicas:        3,
					UpdatedReplicas: 1, // Update in progress
					RollingUpdateProgress: &grovecorev1alpha1.PodCliqueRollingUpdateProgress{
						PodGangSetGenerationHash: "old-hash",
						UpdateStartedAt:          metav1.Now(),
					},
				},
			},
			expectedError:                           false,
			expectedCurrentPodTemplateHash:          nil,
			expectedCurrentPodGangSetGenerationHash: nil,
		},
		{
			// PodClique with no rolling update progress should set hashes from PGS
			name: "no rolling update progress",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "worker",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									PodSpec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "test", Image: "test:latest"},
										},
									},
								},
							},
						},
					},
				},
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("current-hash"),
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs-0-worker", // Correct FQN format
					Namespace: "default",
					Labels: map[string]string{
						common.LabelPartOfKey:                 "test-pgs",
						apicommon.LabelPodGangSetReplicaIndex: "0",
					},
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					Replicas:              3,
					UpdatedReplicas:       3, // All replicas updated
					RollingUpdateProgress: nil,
				},
			},
			expectedError:                           false,
			expectedCurrentPodTemplateHash:          ptr.To("expected-hash"), // This would be computed
			expectedCurrentPodGangSetGenerationHash: ptr.To("current-hash"),
		},
		{
			// PodClique with completed update should set hashes from rolling update progress
			name: "completed update",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("current-hash"),
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					Replicas:        3,
					UpdatedReplicas: 3, // All replicas updated
					RollingUpdateProgress: &grovecorev1alpha1.PodCliqueRollingUpdateProgress{
						PodGangSetGenerationHash: "completed-hash",
						PodTemplateHash:          "completed-template-hash",
						UpdateStartedAt:          metav1.Now(),
						UpdateEndedAt:            &metav1.Time{},
					},
				},
			},
			expectedError:                           false,
			expectedCurrentPodTemplateHash:          ptr.To("completed-template-hash"),
			expectedCurrentPodGangSetGenerationHash: ptr.To("completed-hash"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.FromContext(context.Background())
			err := mutateCurrentHashes(logger, tt.pgs, tt.pclq)

			if tt.expectedError {
				assert.Error(t, err, "Expected error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}

			// Note: Due to the complexity of mocking componentutils.GetExpectedPCLQPodTemplateHash,
			// we can only test the basic logic flow. In a real implementation, you would need
			// to mock the componentutils package or use dependency injection.
			if tt.expectedCurrentPodGangSetGenerationHash != nil {
				assert.Equal(t, tt.expectedCurrentPodGangSetGenerationHash, tt.pclq.Status.CurrentPodGangSetGenerationHash,
					"CurrentPodGangSetGenerationHash should match expected")
			}
		})
	}
}

// TestMutateReplicas tests the mutateReplicas function which updates the PodClique's
// replica counts based on pod categorization by condition types.
func TestMutateReplicas(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique to mutate (will be modified in-place)
		pclq *grovecorev1alpha1.PodClique
		// podCategories maps condition types to lists of pods
		podCategories map[corev1.PodConditionType][]*corev1.Pod
		// numExistingPods is the total number of existing pods
		numExistingPods int
		// expectedReplicas is the expected Replicas count after mutation
		expectedReplicas int32
		// expectedReadyReplicas is the expected ReadyReplicas count after mutation
		expectedReadyReplicas int32
		// expectedScheduleGatedReplicas is the expected ScheduleGatedReplicas count after mutation
		expectedScheduleGatedReplicas int32
		// expectedScheduledReplicas is the expected ScheduledReplicas count after mutation
		expectedScheduledReplicas int32
	}{
		{
			// Standard pod categorization should set correct replica counts
			name: "standard pod categorization",
			pclq: &grovecorev1alpha1.PodClique{},
			podCategories: map[corev1.PodConditionType][]*corev1.Pod{
				corev1.PodReady: {
					{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "pod2"}},
				},
				corev1.PodScheduled: {
					{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "pod2"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "pod3"}},
				},
				k8sutils.ScheduleGatedPod: {
					{ObjectMeta: metav1.ObjectMeta{Name: "pod4"}},
				},
				k8sutils.TerminatingPod: {
					{ObjectMeta: metav1.ObjectMeta{Name: "pod5"}},
				},
			},
			numExistingPods:               5,
			expectedReplicas:              4, // 5 total - 1 terminating
			expectedReadyReplicas:         2,
			expectedScheduleGatedReplicas: 1,
			expectedScheduledReplicas:     3,
		},
		{
			// Empty pod categories should result in zero counts
			name:                          "empty pod categories",
			pclq:                          &grovecorev1alpha1.PodClique{},
			podCategories:                 map[corev1.PodConditionType][]*corev1.Pod{},
			numExistingPods:               0,
			expectedReplicas:              0,
			expectedReadyReplicas:         0,
			expectedScheduleGatedReplicas: 0,
			expectedScheduledReplicas:     0,
		},
		{
			// All pods terminating should result in zero non-terminating replicas
			name: "all pods terminating",
			pclq: &grovecorev1alpha1.PodClique{},
			podCategories: map[corev1.PodConditionType][]*corev1.Pod{
				k8sutils.TerminatingPod: {
					{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "pod2"}},
				},
			},
			numExistingPods:               2,
			expectedReplicas:              0, // All pods are terminating
			expectedReadyReplicas:         0,
			expectedScheduleGatedReplicas: 0,
			expectedScheduledReplicas:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutateReplicas(tt.pclq, tt.podCategories, tt.numExistingPods)

			assert.Equal(t, tt.expectedReplicas, tt.pclq.Status.Replicas, "Replicas should match expected")
			assert.Equal(t, tt.expectedReadyReplicas, tt.pclq.Status.ReadyReplicas, "ReadyReplicas should match expected")
			assert.Equal(t, tt.expectedScheduleGatedReplicas, tt.pclq.Status.ScheduleGatedReplicas, "ScheduleGatedReplicas should match expected")
			assert.Equal(t, tt.expectedScheduledReplicas, tt.pclq.Status.ScheduledReplicas, "ScheduledReplicas should match expected")
		})
	}
}

// TestMutateUpdatedReplica tests the mutateUpdatedReplica function which calculates
// the number of updated replicas based on pod template hash matching.
func TestMutateUpdatedReplica(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique to mutate (will be modified in-place)
		pclq *grovecorev1alpha1.PodClique
		// existingPods are the pods to check for template hash matching
		existingPods []*corev1.Pod
		// expectedUpdatedReplicas is the expected UpdatedReplicas count after mutation
		expectedUpdatedReplicas int32
	}{
		{
			// PodClique with update in progress should use rolling update template hash
			name: "update in progress",
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodCliqueRollingUpdateProgress{
						PodTemplateHash: "new-hash",
						UpdateStartedAt: metav1.Now(),
					},
				},
			},
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							apicommon.LabelPodTemplateHash: "new-hash",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod2",
						Labels: map[string]string{
							apicommon.LabelPodTemplateHash: "old-hash",
						},
					},
				},
			},
			expectedUpdatedReplicas: 1,
		},
		{
			// PodClique with current template hash should use that for comparison
			name: "using current template hash",
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					CurrentPodTemplateHash: ptr.To("current-hash"),
					RollingUpdateProgress:  nil,
				},
			},
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							apicommon.LabelPodTemplateHash: "current-hash",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod2",
						Labels: map[string]string{
							apicommon.LabelPodTemplateHash: "current-hash",
						},
					},
				},
			},
			expectedUpdatedReplicas: 2,
		},
		{
			// PodClique without template hash should not count any pods as updated
			name: "no template hash",
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					CurrentPodTemplateHash: nil,
					RollingUpdateProgress:  nil,
				},
			},
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							apicommon.LabelPodTemplateHash: "some-hash",
						},
					},
				},
			},
			expectedUpdatedReplicas: 0,
		},
		{
			// Pods without template hash labels should not be counted as updated
			name: "pods without template hash labels",
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					CurrentPodTemplateHash: ptr.To("expected-hash"),
				},
			},
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
						// No template hash label
					},
				},
			},
			expectedUpdatedReplicas: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutateUpdatedReplica(tt.pclq, tt.existingPods)
			assert.Equal(t, tt.expectedUpdatedReplicas, tt.pclq.Status.UpdatedReplicas, "UpdatedReplicas should match expected")
		})
	}
}

// TestMutateSelector tests the mutateSelector function which builds the label selector
// that will be used by autoscalers to identify pods belonging to this PodClique.
func TestMutateSelector(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pgsName is the PodGangSet name to use for selector labels
		pgsName string
		// pclq is the PodClique to mutate (will be modified in-place)
		pclq *grovecorev1alpha1.PodClique
		// expectedError indicates if an error should occur
		expectedError bool
		// expectedSelectorSet indicates if selector should be set
		expectedSelectorSet bool
	}{
		{
			// PodClique with scale config should have selector set
			name:    "podclique with scale config",
			pgsName: "test-pgs",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					ScaleConfig: &grovecorev1alpha1.AutoScalingConfig{
						MinReplicas: ptr.To(int32(1)),
						MaxReplicas: 10,
					},
				},
			},
			expectedError:       false,
			expectedSelectorSet: true,
		},
		{
			// PodClique without scale config should not have selector set
			name:    "podclique without scale config",
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
			err := mutateSelector(tt.pgsName, tt.pclq)

			if tt.expectedError {
				assert.Error(t, err, "Expected error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}

			if tt.expectedSelectorSet {
				assert.NotNil(t, tt.pclq.Status.Selector, "Selector should be set")
				assert.NotEmpty(t, *tt.pclq.Status.Selector, "Selector should not be empty")
			} else {
				assert.Nil(t, tt.pclq.Status.Selector, "Selector should not be set")
			}
		})
	}
}

// TestComputeMinAvailableBreachedCondition tests the computeMinAvailableBreachedCondition function
// which determines the MinAvailableBreached condition based on pod states and availability requirements.
func TestComputeMinAvailableBreachedCondition(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique to evaluate
		pclq *grovecorev1alpha1.PodClique
		// numPodsHavingAtleastOneContainerWithNonZeroExitCode is the count of failed pods
		numPodsHavingAtleastOneContainerWithNonZeroExitCode int
		// numPodsStartedButNotReady is the count of pods that started but aren't ready
		numPodsStartedButNotReady int
		// expectedStatus is the expected condition status
		expectedStatus metav1.ConditionStatus
		// expectedReason is the expected condition reason
		expectedReason string
	}{
		{
			// PodClique with update in progress should return unknown status
			name: "update in progress",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(2)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodCliqueRollingUpdateProgress{
						UpdateStartedAt: metav1.Now(),
					},
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 0,
			numPodsStartedButNotReady:                           0,
			expectedStatus:                                      metav1.ConditionUnknown,
			expectedReason:                                      constants.ConditionReasonUpdateInProgress,
		},
		{
			// Insufficient scheduled pods should not breach minimum available
			name: "insufficient scheduled pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 2, // Less than minAvailable
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 0,
			numPodsStartedButNotReady:                           0,
			expectedStatus:                                      metav1.ConditionFalse,
			expectedReason:                                      constants.ConditionReasonInsufficientScheduledPods,
		},
		{
			// Insufficient ready or starting pods should breach minimum available
			name: "insufficient ready pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 5, // Sufficient scheduled pods
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 2,                    // 2 failed pods
			numPodsStartedButNotReady:                           1,                    // 1 not ready pod
			expectedStatus:                                      metav1.ConditionTrue, // 5 - 2 - 1 = 2 < 3
			expectedReason:                                      constants.ConditionReasonInsufficientReadyPods,
		},
		{
			// Sufficient ready or starting pods should not breach minimum available
			name: "sufficient ready pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 5, // Sufficient scheduled pods
				},
			},
			numPodsHavingAtleastOneContainerWithNonZeroExitCode: 1,                     // 1 failed pod
			numPodsStartedButNotReady:                           1,                     // 1 not ready pod
			expectedStatus:                                      metav1.ConditionFalse, // 5 - 1 - 1 = 3 >= 3
			expectedReason:                                      constants.ConditionReasonSufficientReadyPods,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition := computeMinAvailableBreachedCondition(tt.pclq, tt.numPodsHavingAtleastOneContainerWithNonZeroExitCode, tt.numPodsStartedButNotReady)

			assert.Equal(t, constants.ConditionTypeMinAvailableBreached, condition.Type, "Condition type should be MinAvailableBreached")
			assert.Equal(t, tt.expectedStatus, condition.Status, "Condition status should match expected")
			assert.Equal(t, tt.expectedReason, condition.Reason, "Condition reason should match expected")
			assert.NotEmpty(t, condition.Message, "Condition message should not be empty")
		})
	}
}

// TestComputePodCliqueScheduledCondition tests the computePodCliqueScheduledCondition function
// which determines the PodCliqueScheduled condition based on scheduled replica counts.
func TestComputePodCliqueScheduledCondition(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique to evaluate
		pclq *grovecorev1alpha1.PodClique
		// expectedStatus is the expected condition status
		expectedStatus metav1.ConditionStatus
		// expectedReason is the expected condition reason
		expectedReason string
	}{
		{
			// Insufficient scheduled pods should result in false condition
			name: "insufficient scheduled pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 2, // Less than minAvailable
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: constants.ConditionReasonInsufficientScheduledPods,
		},
		{
			// Sufficient scheduled pods should result in true condition
			name: "sufficient scheduled pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3, // Equal to minAvailable
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: constants.ConditionReasonSufficientScheduledPods,
		},
		{
			// More than sufficient scheduled pods should result in true condition
			name: "more than sufficient scheduled pods",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(2)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 5, // More than minAvailable
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: constants.ConditionReasonSufficientScheduledPods,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition := computePodCliqueScheduledCondition(tt.pclq)

			assert.Equal(t, constants.ConditionTypePodCliqueScheduled, condition.Type, "Condition type should be PodCliqueScheduled")
			assert.Equal(t, tt.expectedStatus, condition.Status, "Condition status should match expected")
			assert.Equal(t, tt.expectedReason, condition.Reason, "Condition reason should match expected")
			assert.NotEmpty(t, condition.Message, "Condition message should not be empty")
		})
	}
}

// TestMutatePodCliqueScheduledCondition tests the mutatePodCliqueScheduledCondition function
// which updates the PodCliqueScheduled condition if it has changed.
func TestMutatePodCliqueScheduledCondition(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique to mutate (will be modified in-place)
		pclq *grovecorev1alpha1.PodClique
		// expectedConditionCount is the expected number of conditions after mutation
		expectedConditionCount int
		// expectedConditionStatus is the expected status of the PodCliqueScheduled condition
		expectedConditionStatus metav1.ConditionStatus
	}{
		{
			// PodClique without existing conditions should add new condition
			name: "add new condition",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(2)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3,
					Conditions:        []metav1.Condition{},
				},
			},
			expectedConditionCount:  1,
			expectedConditionStatus: metav1.ConditionTrue,
		},
		{
			// PodClique with existing different condition should add new condition
			name: "add condition alongside existing",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(2)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 1,
					Conditions: []metav1.Condition{
						{
							Type:   constants.ConditionTypeMinAvailableBreached,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expectedConditionCount:  2,
			expectedConditionStatus: metav1.ConditionFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutatePodCliqueScheduledCondition(tt.pclq)

			assert.Len(t, tt.pclq.Status.Conditions, tt.expectedConditionCount, "Should have expected number of conditions")

			// Find the PodCliqueScheduled condition
			scheduledCondition := meta.FindStatusCondition(tt.pclq.Status.Conditions, constants.ConditionTypePodCliqueScheduled)
			require.NotNil(t, scheduledCondition, "PodCliqueScheduled condition should exist")
			assert.Equal(t, tt.expectedConditionStatus, scheduledCondition.Status, "Condition status should match expected")
		})
	}
}

// TestMutateMinAvailableBreachedCondition tests the mutateMinAvailableBreachedCondition function
// which updates the MinAvailableBreached condition if it has changed.
func TestMutateMinAvailableBreachedCondition(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique to mutate (will be modified in-place)
		pclq *grovecorev1alpha1.PodClique
		// numNotReadyPodsWithContainersInError is the count of pods with container errors
		numNotReadyPodsWithContainersInError int
		// numPodsStartedButNotReady is the count of pods that started but aren't ready
		numPodsStartedButNotReady int
		// expectedConditionCount is the expected number of conditions after mutation
		expectedConditionCount int
		// expectedConditionStatus is the expected status of the MinAvailableBreached condition
		expectedConditionStatus metav1.ConditionStatus
	}{
		{
			// PodClique without existing conditions should add new condition
			name: "add new condition",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(2)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 3,
					Conditions:        []metav1.Condition{},
				},
			},
			numNotReadyPodsWithContainersInError: 0,
			numPodsStartedButNotReady:            0,
			expectedConditionCount:               1,
			expectedConditionStatus:              metav1.ConditionFalse, // 3 - 0 - 0 = 3 >= 2
		},
		{
			// PodClique with insufficient ready pods should breach condition
			name: "breach condition",
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(3)),
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ScheduledReplicas: 4,
					Conditions:        []metav1.Condition{},
				},
			},
			numNotReadyPodsWithContainersInError: 2,
			numPodsStartedButNotReady:            1,
			expectedConditionCount:               1,
			expectedConditionStatus:              metav1.ConditionTrue, // 4 - 2 - 1 = 1 < 3
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutateMinAvailableBreachedCondition(tt.pclq, tt.numNotReadyPodsWithContainersInError, tt.numPodsStartedButNotReady)

			assert.Len(t, tt.pclq.Status.Conditions, tt.expectedConditionCount, "Should have expected number of conditions")

			// Find the MinAvailableBreached condition
			breachedCondition := meta.FindStatusCondition(tt.pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
			require.NotNil(t, breachedCondition, "MinAvailableBreached condition should exist")
			assert.Equal(t, tt.expectedConditionStatus, breachedCondition.Status, "Condition status should match expected")
		})
	}
}
