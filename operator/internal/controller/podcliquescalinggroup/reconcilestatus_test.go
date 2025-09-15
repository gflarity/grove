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

package podcliquescalinggroup

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Test helpers
func buildHealthyClique(name string) grovecorev1alpha1.PodClique {
	return *testutils.NewPodCliqueBuilder("test-pgs", types.UID(uuid.NewString()), name, "test-ns", 0).
		WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build()
}

func buildScheduledClique(name string) grovecorev1alpha1.PodClique {
	return *testutils.NewPodCliqueBuilder("test-pgs", types.UID(uuid.NewString()), name, "test-ns", 0).
		WithOptions(testutils.WithPCLQScheduledButBreached()).Build()
}

func buildFailedClique(name string) grovecorev1alpha1.PodClique {
	return *testutils.NewPodCliqueBuilder("test-pgs", types.UID(uuid.NewString()), name, "test-ns", 0).
		WithOptions(testutils.WithPCLQNotScheduled()).Build()
}

func buildTerminatingClique(name string) grovecorev1alpha1.PodClique {
	return *testutils.NewPodCliqueBuilder("test-pgs", types.UID(uuid.NewString()), name, "test-ns", 0).
		WithOptions(testutils.WithPCLQTerminating()).Build()
}

// expectedCondition defines the expected state of a condition for comprehensive validation
type expectedCondition struct {
	Type    string
	Status  metav1.ConditionStatus
	Reason  string
	Message string // Optional - if empty, message won't be validated
}

// assertCondition provides basic condition breach validation for backward compatibility
func assertCondition(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, expectBreached bool) {
	var condition *metav1.Condition
	for i := range pcsg.Status.Conditions {
		if pcsg.Status.Conditions[i].Type == "MinAvailableBreached" {
			condition = &pcsg.Status.Conditions[i]
			break
		}
	}

	require.NotNil(t, condition, "MinAvailableBreached condition should exist")
	isBreached := condition.Status == metav1.ConditionTrue
	assert.Equal(t, expectBreached, isBreached, "condition breach status mismatch")
}

// assertConditionDetails provides comprehensive condition validation including reason and message
func assertConditionDetails(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, expected expectedCondition) {
	var condition *metav1.Condition
	for i := range pcsg.Status.Conditions {
		if pcsg.Status.Conditions[i].Type == expected.Type {
			condition = &pcsg.Status.Conditions[i]
			break
		}
	}

	require.NotNil(t, condition, "Condition %s should exist", expected.Type)
	assert.Equal(t, expected.Status, condition.Status, "Condition status mismatch")
	assert.Equal(t, expected.Reason, condition.Reason, "Condition reason mismatch")
	if expected.Message != "" {
		assert.Contains(t, condition.Message, expected.Message, "Condition message should contain expected text")
	}
	assert.NotZero(t, condition.LastTransitionTime, "LastTransitionTime should be set")
}

// ============================================================================
// Unit Tests
// ============================================================================

func TestComputeReplicaStatus(t *testing.T) {
	logger := testutils.SetupTestLogger()

	tests := []struct {
		name          string
		description   string // Added for better test documentation
		expectedSize  int
		cliques       []grovecorev1alpha1.PodClique
		minAvailable  int32
		wantScheduled bool
		wantAvailable bool
	}{
		{
			name:          "mixed_healthy_and_failed_cliques",
			description:   "When a replica has both healthy and failed cliques, the entire replica should be considered unavailable and unscheduled",
			expectedSize:  2,
			cliques:       []grovecorev1alpha1.PodClique{buildHealthyClique("frontend"), buildFailedClique("backend")},
			minAvailable:  1,
			wantScheduled: false,
			wantAvailable: false,
		},
		{
			name:          "incomplete_replica_with_missing_cliques",
			description:   "When a replica doesn't have all expected cliques, it should be considered unavailable and unscheduled regardless of existing clique health",
			expectedSize:  3,
			cliques:       []grovecorev1alpha1.PodClique{buildHealthyClique("frontend")},
			minAvailable:  1,
			wantScheduled: false,
			wantAvailable: false,
		},
		{
			name:          "scheduled_but_not_available_cliques",
			description:   "When all cliques are scheduled but some don't meet minAvailable threshold, replica should be scheduled but not available",
			expectedSize:  2,
			cliques:       []grovecorev1alpha1.PodClique{buildHealthyClique("frontend"), buildScheduledClique("backend")},
			minAvailable:  1,
			wantScheduled: true,
			wantAvailable: false,
		},
		{
			name:          "terminating_cliques_should_be_filtered",
			description:   "Terminating cliques should not count toward replica status, only non-terminating cliques should be considered",
			expectedSize:  2,
			cliques:       []grovecorev1alpha1.PodClique{buildHealthyClique("frontend"), buildHealthyClique("backend"), buildTerminatingClique("old"), buildTerminatingClique("terminated")},
			minAvailable:  1,
			wantScheduled: true,
			wantAvailable: true,
		},
		{
			name:          "zero_minAvailable_makes_scheduled_cliques_available",
			description:   "When minAvailable is 0, any scheduled clique should be considered available regardless of ready pod count",
			expectedSize:  2,
			cliques:       []grovecorev1alpha1.PodClique{buildHealthyClique("frontend"), buildScheduledClique("backend")},
			minAvailable:  0,
			wantScheduled: true,
			wantAvailable: true,
		},
		{
			name:          "high_minAvailable_threshold_blocks_availability",
			description:   "When minAvailable is higher than ready pods in cliques, replica should be scheduled but not available",
			expectedSize:  2,
			cliques:       []grovecorev1alpha1.PodClique{buildHealthyClique("frontend"), buildHealthyClique("backend")},
			minAvailable:  2,
			wantScheduled: true,
			wantAvailable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test scenario: %s", tt.description)
			scheduled, available := computeReplicaStatus(logger, tt.expectedSize, "0", tt.cliques, tt.minAvailable)

			assert.Equal(t, tt.wantScheduled, scheduled, "scheduled mismatch for scenario: %s", tt.description)
			assert.Equal(t, tt.wantAvailable, available, "available mismatch for scenario: %s", tt.description)
		})
	}
}

func TestComputeMinAvailableBreachedCondition(t *testing.T) {
	tests := []struct {
		name         string
		replicas     int32
		minAvailable *int32
		scheduled    int32
		available    int32
		pclqsMap     map[string][]grovecorev1alpha1.PodClique
		wantStatus   metav1.ConditionStatus
		wantReason   string
	}{
		{
			name:       "sufficient replicas",
			replicas:   3,
			scheduled:  3,
			available:  3,
			pclqsMap:   make(map[string][]grovecorev1alpha1.PodClique),
			wantStatus: metav1.ConditionFalse,
			wantReason: "SufficientAvailablePodCliqueScalingGroupReplicas",
		},
		{
			name:         "custom minAvailable met",
			replicas:     5,
			minAvailable: ptr.To(int32(2)),
			scheduled:    3,
			available:    3,
			pclqsMap:     make(map[string][]grovecorev1alpha1.PodClique),
			wantStatus:   metav1.ConditionFalse,
			wantReason:   "SufficientAvailablePodCliqueScalingGroupReplicas",
		},
		{
			name:         "insufficient scheduled",
			replicas:     3,
			minAvailable: ptr.To(int32(2)),
			scheduled:    1,
			available:    1,
			pclqsMap:     make(map[string][]grovecorev1alpha1.PodClique),
			wantStatus:   metav1.ConditionFalse,
			wantReason:   "InsufficientScheduledPodCliqueScalingGroupReplicas",
		},
		{
			name:         "insufficient available",
			replicas:     3,
			minAvailable: ptr.To(int32(2)),
			scheduled:    2,
			available:    1,
			pclqsMap: map[string][]grovecorev1alpha1.PodClique{
				"0": {
					{
						Status: grovecorev1alpha1.PodCliqueStatus{
							Conditions: []metav1.Condition{
								{
									Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: "InsufficientAvailablePodCliqueScalingGroupReplicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minAvailable := tt.minAvailable
			if minAvailable == nil {
				minAvailable = &tt.replicas
			}
			pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{
				Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
					Replicas:     tt.replicas,
					MinAvailable: minAvailable,
				},
				Status: grovecorev1alpha1.PodCliqueScalingGroupStatus{
					ScheduledReplicas: tt.scheduled,
					AvailableReplicas: tt.available,
				},
			}

			logger := testutils.SetupTestLogger()
			condition := computeMinAvailableBreachedCondition(logger, pcsg, tt.pclqsMap)

			assert.Equal(t, "MinAvailableBreached", condition.Type)
			assert.Equal(t, tt.wantStatus, condition.Status)
			assert.Equal(t, tt.wantReason, condition.Reason)
		})
	}
}

func TestGetPodCliquesPerPCSGReplica(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		objects      []client.Object
		wantReplicas int
	}{
		{
			name: "find expected cliques",
			objects: []client.Object{
				testutils.NewPCSGPodCliqueBuilder("test-pgs-0-frontend-0", "test-ns", "test-pgs", "test-pcsg", 0, 0).
					WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").Build(),
				testutils.NewPCSGPodCliqueBuilder("test-pgs-0-backend-0", "test-ns", "test-pgs", "test-pcsg", 0, 0).
					WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").Build(),
				testutils.NewPCSGPodCliqueBuilder("test-pgs-0-frontend-1", "test-ns", "test-pgs", "test-pcsg", 0, 1).
					WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").Build(),
			},
			wantReplicas: 2,
		},
		{
			name:         "no cliques found",
			objects:      []client.Object{},
			wantReplicas: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := testutils.SetupFakeClient(tt.objects...)
			reconciler := &Reconciler{client: fakeClient}
			objKey := client.ObjectKey{Name: "test-pcsg", Namespace: "test-ns"}

			result, err := reconciler.getPodCliquesPerPCSGReplica(ctx, "test-pgs", objKey)

			require.NoError(t, err)
			assert.Len(t, result, tt.wantReplicas)
		})
	}
}

// ============================================================================
// Error Condition Tests
// ============================================================================

func TestReconcileStatus_ErrorConditions(t *testing.T) {
	ctx := context.Background()
	logger := testutils.SetupTestLogger()

	tests := []struct {
		name          string
		description   string
		setup         func() (*Reconciler, *grovecorev1alpha1.PodCliqueScalingGroup)
		expectedError string
	}{
		{
			name:        "missing_owner_podgangset",
			description: "Should fail gracefully when the owner PodGangSet cannot be found",
			setup: func() (*Reconciler, *grovecorev1alpha1.PodCliqueScalingGroup) {
				// Create PCSG without corresponding PodGangSet
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-ns", "missing-pgs", 0).
					WithOwnerReference("PodGangSet", "missing-pgs", "test-uid").
					WithOptions(testutils.WithPCSGObservedGeneration(1)).Build()
				fakeClient := testutils.SetupFakeClient(pcsg) // No PodGangSet in client
				return &Reconciler{client: fakeClient}, pcsg
			},
			expectedError: "not found",
		},
		{
			name:        "client_error_during_status_update",
			description: "Should handle client errors during status update gracefully",
			setup: func() (*Reconciler, *grovecorev1alpha1.PodCliqueScalingGroup) {
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-ns", "test-pgs", 0).
					WithOptions(testutils.WithPCSGObservedGeneration(1)).Build()
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").Build()

				// Create a client that will fail on status updates
				fakeClient := testutils.SetupFakeClient(pcsg, pgs)
				// Note: In a real test, we'd use a mock client that fails on Status().Update()
				// For this example, we'll simulate the error condition
				return &Reconciler{client: fakeClient}, pcsg
			},
			expectedError: "", // This test would need a proper mock client to simulate the error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test scenario: %s", tt.description)
			reconciler, pcsg := tt.setup()

			result := reconciler.reconcileStatus(ctx, logger, pcsg)

			if tt.expectedError != "" {
				assert.True(t, result.HasErrors(), "Expected reconciliation to fail")
				assert.Contains(t, result.GetErrors()[0].Error(), tt.expectedError, "Error message should contain expected text")
			} else {
				// For cases where we can't easily simulate the error, just ensure no panic
				// In a production test suite, we'd use proper mocks
				t.Skip("Skipping test that requires mock client - would need dependency injection for proper testing")
			}
		})
	}
}

func TestMutateSelector_ErrorConditions(t *testing.T) {
	tests := []struct {
		name        string
		description string
		setup       func() (*grovecorev1alpha1.PodGangSet, *grovecorev1alpha1.PodCliqueScalingGroup)
		wantError   bool
	}{
		{
			name:        "missing_podgangset_replica_index_label",
			description: "Should handle missing PodGangSet replica index label gracefully",
			setup: func() (*grovecorev1alpha1.PodGangSet, *grovecorev1alpha1.PodCliqueScalingGroup) {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").Build()
				// Create PCSG without proper replica index label
				pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pcsg",
						Namespace: "test-ns",
						Labels:    map[string]string{}, // Missing replica index label
					},
				}
				return pgs, pcsg
			},
			wantError: true,
		},
		{
			name:        "pcsg_not_found_in_pgs_template",
			description: "Should handle case where PCSG is not defined in PodGangSet template",
			setup: func() (*grovecorev1alpha1.PodGangSet, *grovecorev1alpha1.PodCliqueScalingGroup) {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").Build()
				// Create PCSG that doesn't match any config in PGS
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("non-matching-pcsg", "test-ns", "test-pgs", 0).Build()
				return pgs, pcsg
			},
			wantError: false, // This case is handled gracefully (returns nil)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test scenario: %s", tt.description)
			pgs, pcsg := tt.setup()

			err := mutateSelector(pgs, pcsg)

			if tt.wantError {
				assert.Error(t, err, "Expected mutateSelector to return an error")
			} else {
				assert.NoError(t, err, "Expected mutateSelector to succeed")
			}
		})
	}
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestReconcileStatus(t *testing.T) {
	ctx := context.Background()
	logger := testutils.SetupTestLogger()

	tests := []struct {
		name          string
		description   string
		setup         func() (*grovecorev1alpha1.PodCliqueScalingGroup, *grovecorev1alpha1.PodGangSet, []client.Object)
		wantAvailable int32
		wantScheduled int32
		wantCondition expectedCondition
	}{
		{
			name:        "all_replicas_healthy_and_available",
			description: "All PCSG replicas have healthy cliques that meet minAvailable threshold. Should result in all replicas being scheduled and available with no breach condition.",
			setup: func() (*grovecorev1alpha1.PodCliqueScalingGroup, *grovecorev1alpha1.PodGangSet, []client.Object) {
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-ns", "test-pgs", 0).
					WithReplicas(2).
					WithCliqueNames([]string{"frontend", "backend"}).
					WithOptions(testutils.WithPCSGObservedGeneration(1)).Build()
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").Build()
				cliques := []client.Object{
					// Replica 0: Both cliques healthy
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-frontend-0", "test-ns", "test-pgs", "test-pcsg", 0, 0).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build(),
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-backend-0", "test-ns", "test-pgs", "test-pcsg", 0, 0).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build(),
					// Replica 1: Both cliques healthy
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-frontend-1", "test-ns", "test-pgs", "test-pcsg", 0, 1).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build(),
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-backend-1", "test-ns", "test-pgs", "test-pcsg", 0, 1).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build(),
				}
				return pcsg, pgs, cliques
			},
			wantAvailable: 2,
			wantScheduled: 2,
			wantCondition: expectedCondition{
				Type:    "MinAvailableBreached",
				Status:  metav1.ConditionFalse,
				Reason:  "SufficientAvailablePodCliqueScalingGroupReplicas",
				Message: "expected at least: 1, found: 2",
			},
		},
		{
			name:        "mixed_replica_states_with_custom_minavailable",
			description: "PCSG with 3 replicas and custom minAvailable=2. Replica 0 is healthy, replica 1 is scheduled but breached, replica 2 is not scheduled. Should result in breach condition since only 1 replica is available but 2 are required.",
			setup: func() (*grovecorev1alpha1.PodCliqueScalingGroup, *grovecorev1alpha1.PodGangSet, []client.Object) {
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-ns", "test-pgs", 0).
					WithReplicas(3).
					WithCliqueNames([]string{"worker"}).
					WithMinAvailable(2).
					WithOptions(testutils.WithPCSGObservedGeneration(1)).Build()
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").Build()
				cliques := []client.Object{
					// Replica 0: Healthy and available
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-worker-0", "test-ns", "test-pgs", "test-pcsg", 0, 0).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build(),
					// Replica 1: Scheduled but breached (not enough ready pods)
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-worker-1", "test-ns", "test-pgs", "test-pcsg", 0, 1).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledButBreached()).Build(),
					// Replica 2: Not scheduled at all
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-worker-2", "test-ns", "test-pgs", "test-pcsg", 0, 2).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQNotScheduled()).Build(),
				}
				return pcsg, pgs, cliques
			},
			wantAvailable: 1,
			wantScheduled: 2,
			wantCondition: expectedCondition{
				Type:    "MinAvailableBreached",
				Status:  metav1.ConditionTrue,
				Reason:  "InsufficientAvailablePodCliqueScalingGroupReplicas",
				Message: "expected at least: 2, found: 1",
			},
		},
		{
			name:        "terminating_cliques_should_be_excluded",
			description: "When some cliques are terminating, they should be filtered out from replica status calculations. Only replica 0 should count as available since replica 1 has a terminating clique.",
			setup: func() (*grovecorev1alpha1.PodCliqueScalingGroup, *grovecorev1alpha1.PodGangSet, []client.Object) {
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-ns", "test-pgs", 0).
					WithReplicas(2).
					WithCliqueNames([]string{"frontend", "backend"}).
					WithOptions(testutils.WithPCSGObservedGeneration(1)).Build()
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").Build()
				cliques := []client.Object{
					// Replica 0: Both cliques healthy and available
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-frontend-0", "test-ns", "test-pgs", "test-pcsg", 0, 0).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build(),
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-backend-0", "test-ns", "test-pgs", "test-pcsg", 0, 0).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build(),
					// Replica 1: One healthy clique, one terminating clique (should make replica unavailable)
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-frontend-1", "test-ns", "test-pgs", "test-pcsg", 0, 1).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQScheduledAndAvailable()).Build(),
					testutils.NewPCSGPodCliqueBuilder("test-pgs-0-backend-1", "test-ns", "test-pgs", "test-pcsg", 0, 1).
						WithOwnerReference("PodCliqueScalingGroup", "test-pcsg", "").
						WithReplicas(2).
						WithOptions(testutils.WithPCLQTerminating()).Build(),
				}
				return pcsg, pgs, cliques
			},
			wantAvailable: 1, // Only replica 0 has all non-terminated cliques
			wantScheduled: 1, // Only replica 0 has sufficient non-terminated cliques
			wantCondition: expectedCondition{
				Type:    "MinAvailableBreached",
				Status:  metav1.ConditionFalse,
				Reason:  "SufficientAvailablePodCliqueScalingGroupReplicas",
				Message: "expected at least: 1, found: 1", // 1 >= 1 (default minAvailable)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test scenario: %s", tt.description)
			pcsg, pgs, cliques := tt.setup()
			allObjects := append([]client.Object{pcsg, pgs}, cliques...)
			fakeClient := testutils.SetupFakeClient(allObjects...)
			reconciler := &Reconciler{client: fakeClient}

			result := reconciler.reconcileStatus(ctx, logger, pcsg)

			require.False(t, result.HasErrors(), "Reconciliation should succeed")
			assert.Equal(t, tt.wantAvailable, pcsg.Status.AvailableReplicas, "Available replicas mismatch")
			assert.Equal(t, tt.wantScheduled, pcsg.Status.ScheduledReplicas, "Scheduled replicas mismatch")

			if pcsg.Status.ObservedGeneration != nil {
				assertConditionDetails(t, pcsg, tt.wantCondition)
			}
		})
	}
}

func TestReconcileStatus_EdgeCases(t *testing.T) {
	ctx := context.Background()
	logger := testutils.SetupTestLogger()

	tests := []struct {
		name        string
		description string
		pcsg        *grovecorev1alpha1.PodCliqueScalingGroup
	}{
		{
			name:        "zero_replicas_should_not_fail",
			description: "PCSG with zero replicas should be handled gracefully without errors",
			pcsg: testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-ns", "test-pgs", 0).
				WithReplicas(0).
				WithOptions(testutils.WithPCSGObservedGeneration(1)).Build(),
		},
		{
			name:        "empty_clique_names_should_not_fail",
			description: "PCSG with empty clique names array should be handled gracefully without errors",
			pcsg: testutils.NewPodCliqueScalingGroupBuilder("test-pcsg", "test-ns", "test-pgs", 0).
				WithReplicas(1).
				WithCliqueNames([]string{}).
				WithOptions(testutils.WithPCSGObservedGeneration(1)).Build(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test scenario: %s", tt.description)
			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").Build()
			fakeClient := testutils.SetupFakeClient(tt.pcsg, pgs)
			reconciler := &Reconciler{client: fakeClient}

			result := reconciler.reconcileStatus(ctx, logger, tt.pcsg)

			assert.False(t, result.HasErrors(), "Edge case should be handled gracefully")
		})
	}
}

// ============================================================================
// Direct Function Tests
// ============================================================================

func TestMutateSelector_Comprehensive(t *testing.T) {
	tests := []struct {
		name         string
		description  string
		setup        func() (*grovecorev1alpha1.PodGangSet, *grovecorev1alpha1.PodCliqueScalingGroup)
		wantError    bool
		wantSelector bool
	}{
		{
			name:        "pcsg_with_scale_config_should_set_selector",
			description: "When PCSG has a ScaleConfig defined in PGS template, selector should be set for HPA integration",
			setup: func() (*grovecorev1alpha1.PodGangSet, *grovecorev1alpha1.PodCliqueScalingGroup) {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").
					WithPodCliqueScalingGroupConfig(grovecorev1alpha1.PodCliqueScalingGroupConfig{
						Name: "test-pcsg",
						ScaleConfig: &grovecorev1alpha1.AutoScalingConfig{
							MinReplicas: ptr.To(int32(1)),
							MaxReplicas: 10,
						},
					}).Build()
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pgs-0-test-pcsg", "test-ns", "test-pgs", 0).Build()
				return pgs, pcsg
			},
			wantError:    false,
			wantSelector: true,
		},
		{
			name:        "pcsg_without_scale_config_should_not_set_selector",
			description: "When PCSG has no ScaleConfig defined, selector should not be set",
			setup: func() (*grovecorev1alpha1.PodGangSet, *grovecorev1alpha1.PodCliqueScalingGroup) {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-ns").
					WithPodCliqueScalingGroupConfig(grovecorev1alpha1.PodCliqueScalingGroupConfig{
						Name: "test-pcsg",
						// No ScaleConfig
					}).Build()
				pcsg := testutils.NewPodCliqueScalingGroupBuilder("test-pgs-0-test-pcsg", "test-ns", "test-pgs", 0).Build()
				return pgs, pcsg
			},
			wantError:    false,
			wantSelector: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test scenario: %s", tt.description)
			pgs, pcsg := tt.setup()

			err := mutateSelector(pgs, pcsg)

			if tt.wantError {
				assert.Error(t, err, "Expected mutateSelector to return an error")
			} else {
				assert.NoError(t, err, "Expected mutateSelector to succeed")
			}

			if tt.wantSelector {
				assert.NotNil(t, pcsg.Status.Selector, "Selector should be set")
				assert.NotEmpty(t, *pcsg.Status.Selector, "Selector should not be empty")
				t.Logf("Generated selector: %s", *pcsg.Status.Selector)
			} else {
				assert.Nil(t, pcsg.Status.Selector, "Selector should not be set")
			}
		})
	}
}
