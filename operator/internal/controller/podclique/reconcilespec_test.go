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
	"errors"
	"testing"

	"github.com/NVIDIA/grove/operator/api/common"
	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TestReconcileSpec tests the reconcileSpec function which orchestrates the complete
// reconciliation of a PodClique's spec by executing a series of reconciliation steps.
func TestReconcileSpec(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique object to reconcile
		pclq *grovecorev1alpha1.PodClique
		// existingObjects are objects that should exist in the fake client
		existingObjects []client.Object
		// mockOperatorRegistry is the mock operator registry to use
		mockOperatorRegistry *MockOperatorRegistry
		// expectedContinue indicates if reconciliation should continue
		expectedContinue bool
		// expectedError indicates if an error should occur
		expectedError bool
	}{
		{
			// Successful spec reconciliation should complete all steps
			name: "successful spec reconciliation",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Generation: 1,
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
						common.LabelPartOfKey:    "test-pgs",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: constants.KindPodGangSet,
							Name: "test-pgs",
						},
					},
					Finalizers: []string{constants.FinalizerPodClique},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(1)),
				},
			},
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
					Status: grovecorev1alpha1.PodGangSetStatus{
						CurrentGenerationHash: ptr.To("test-hash"),
					},
				},
			},
			mockOperatorRegistry: &MockOperatorRegistry{
				operators: map[component.Kind]*MockOperator{
					component.KindPod: &MockOperator{
						syncError: nil,
					},
				},
			},
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// PodClique without finalizer should add finalizer and requeue
			name: "podclique without finalizer",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(1)),
				},
			},
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
					Status: grovecorev1alpha1.PodGangSetStatus{
						CurrentGenerationHash: ptr.To("test-hash"),
					},
				},
			},
			mockOperatorRegistry: &MockOperatorRegistry{
				operators: make(map[component.Kind]*MockOperator),
			},
			expectedContinue: false,
			expectedError:    true, // Requeue is treated as error in this context
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with existing objects
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			// Add the PodClique to existing objects for the test
			allObjects := append(tt.existingObjects, tt.pclq)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(allObjects...).
				WithStatusSubresource(&grovecorev1alpha1.PodClique{}).
				Build()

			// Set up mock expectations
			for _, mockOp := range tt.mockOperatorRegistry.operators {
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(mockOp.syncError)
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)
			}

			mockStatusRecorder := &MockReconcileStatusRecorder{}
			mockStatusRecorder.On("RecordStart", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			mockStatusRecorder.On("RecordCompletion", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			// Create reconciler with mock dependencies
			reconciler := &Reconciler{
				client:                  fakeClient,
				eventRecorder:           record.NewFakeRecorder(100),
				reconcileStatusRecorder: mockStatusRecorder,
				operatorRegistry:        tt.mockOperatorRegistry,
			}

			// Execute spec reconciliation
			ctx := context.Background()
			logger := log.FromContext(ctx)
			result := reconciler.reconcileSpec(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedError {
				assert.True(t, result.NeedsRequeue() || len(result.GetErrors()) > 0, "Expected error or requeue but got success")
			} else {
				assert.False(t, result.NeedsRequeue(), "Expected no requeue but got requeue")
				assert.Empty(t, result.GetErrors(), "Expected no errors but got: %v", result.GetErrors())
			}

			if tt.expectedContinue {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected reconciliation to continue")
			}
		})
	}
}

// TestEnsureFinalizer tests the ensureFinalizer function which adds the PodClique
// finalizer if it's not already present to ensure proper cleanup during deletion.
func TestEnsureFinalizer(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique object to test
		pclq *grovecorev1alpha1.PodClique
		// expectedRequeue indicates if the function should return a requeue result
		expectedRequeue bool
		// expectedError indicates if an error should occur
		expectedError bool
		// expectedFinalizerAdded indicates if the finalizer should be added
		expectedFinalizerAdded bool
	}{
		{
			// PodClique without finalizer should add finalizer and requeue
			name: "add finalizer and requeue",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			expectedRequeue:        true,
			expectedError:          false,
			expectedFinalizerAdded: true,
		},
		{
			// PodClique with finalizer should continue without changes
			name: "finalizer already present",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{constants.FinalizerPodClique},
				},
			},
			expectedRequeue:        false,
			expectedError:          false,
			expectedFinalizerAdded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.pclq).
				Build()

			reconciler := &Reconciler{
				client: fakeClient,
			}

			// Execute finalizer check
			ctx := context.Background()
			logger := log.FromContext(ctx)
			result := reconciler.ensureFinalizer(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedRequeue {
				assert.True(t, result.NeedsRequeue() || len(result.GetErrors()) > 0, "Expected requeue but got success")
			} else {
				assert.False(t, result.NeedsRequeue(), "Expected no requeue but got requeue")
				assert.Empty(t, result.GetErrors(), "Expected no errors but got: %v", result.GetErrors())
			}

			// Verify finalizer state
			if tt.expectedFinalizerAdded {
				assert.True(t, controllerutil.ContainsFinalizer(tt.pclq, constants.FinalizerPodClique), "Finalizer should be added")
			}
		})
	}
}

// TestPgsHasNoActiveRollingUpdate tests the pgsHasNoActiveRollingUpdate function
// which determines if a PodGangSet has an active rolling update in progress.
func TestPgsHasNoActiveRollingUpdate(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pgs is the PodGangSet to check
		pgs *grovecorev1alpha1.PodGangSet
		// expected indicates if the function should return true (no active update)
		expected bool
	}{
		{
			// PodGangSet with nil CurrentGenerationHash has no active update
			name: "nil current generation hash",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: nil,
				},
			},
			expected: true,
		},
		{
			// PodGangSet with nil RollingUpdateProgress has no active update
			name: "nil rolling update progress",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("test-hash"),
					RollingUpdateProgress: nil,
				},
			},
			expected: true,
		},
		{
			// PodGangSet with nil CurrentlyUpdating has no active update
			name: "nil currently updating",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("test-hash"),
					RollingUpdateProgress: &grovecorev1alpha1.PodGangSetRollingUpdateProgress{
						CurrentlyUpdating: nil,
					},
				},
			},
			expected: true,
		},
		{
			// PodGangSet with all fields set has active update
			name: "active rolling update",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("test-hash"),
					RollingUpdateProgress: &grovecorev1alpha1.PodGangSetRollingUpdateProgress{
						CurrentlyUpdating: &grovecorev1alpha1.PodGangSetReplicaRollingUpdateProgress{
							ReplicaIndex: 0,
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pgsHasNoActiveRollingUpdate(tt.pgs)
			assert.Equal(t, tt.expected, result, "pgsHasNoActiveRollingUpdate should return %v", tt.expected)
		})
	}
}

// TestShouldCheckPendingUpdatesForPCLQ tests the shouldCheckPendingUpdatesForPCLQ function
// which determines if a PodClique should be evaluated for pending updates based on its
// relationship to PodGangSet and PodCliqueScalingGroup.
func TestShouldCheckPendingUpdatesForPCLQ(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pgs is the PodGangSet to check against
		pgs *grovecorev1alpha1.PodGangSet
		// pclq is the PodClique to evaluate
		pclq *grovecorev1alpha1.PodClique
		// expected indicates if the function should return true
		expected bool
		// expectedError indicates if an error should occur
		expectedError bool
	}{
		{
			// PodClique not in PGS standalone list should not be evaluated
			name: "pclq not in pgs standalone list",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "other-pclq"},
						},
					},
				},
				Status: grovecorev1alpha1.PodGangSetStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodGangSetRollingUpdateProgress{
						CurrentlyUpdating: &grovecorev1alpha1.PodGangSetReplicaRollingUpdateProgress{
							ReplicaIndex: 0,
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs-0-test-pclq", // This doesn't match the expected FQN for "other-pclq"
					Labels: map[string]string{
						apicommon.LabelPodGangSetReplicaIndex: "0",
					},
				},
			},
			expected:      false,
			expectedError: false,
		},
		{
			// PodClique missing replica index label should return error
			name: "pclq missing replica index label",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "test-pclq"},
						},
					},
				},
				Status: grovecorev1alpha1.PodGangSetStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodGangSetRollingUpdateProgress{
						CurrentlyUpdating: &grovecorev1alpha1.PodGangSetReplicaRollingUpdateProgress{
							ReplicaIndex: 0,
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs-0-test-pclq", // Correct FQN format
				},
			},
			expected:      false,
			expectedError: true,
		},
		{
			// PodClique with different replica index should not be evaluated
			name: "pclq different replica index",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 2,
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "test-pclq"},
						},
					},
				},
				Status: grovecorev1alpha1.PodGangSetStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodGangSetRollingUpdateProgress{
						CurrentlyUpdating: &grovecorev1alpha1.PodGangSetReplicaRollingUpdateProgress{
							ReplicaIndex: 1,
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs-0-test-pclq", // Replica 0, but currently updating replica 1
					Labels: map[string]string{
						apicommon.LabelPodGangSetReplicaIndex: "0",
					},
				},
			},
			expected:      false,
			expectedError: false,
		},
		{
			// PodClique with matching replica index should be evaluated
			name: "pclq matching replica index",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{Name: "test-pclq"},
						},
					},
				},
				Status: grovecorev1alpha1.PodGangSetStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodGangSetRollingUpdateProgress{
						CurrentlyUpdating: &grovecorev1alpha1.PodGangSetReplicaRollingUpdateProgress{
							ReplicaIndex: 0,
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs-0-test-pclq", // Correct FQN format
					Labels: map[string]string{
						apicommon.LabelPodGangSetReplicaIndex: "0",
					},
				},
			},
			expected:      true,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.FromContext(context.Background())
			result, err := shouldCheckPendingUpdatesForPCLQ(logger, tt.pgs, tt.pclq)

			if tt.expectedError {
				assert.Error(t, err, "Expected error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}

			assert.Equal(t, tt.expected, result, "shouldCheckPendingUpdatesForPCLQ should return %v", tt.expected)
		})
	}
}

// TestShouldResetOrTriggerRollingUpdate tests the shouldResetOrTriggerRollingUpdate function
// which determines if a rolling update should be reset or triggered based on generation hashes.
func TestShouldResetOrTriggerRollingUpdate(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pgs is the PodGangSet to check
		pgs *grovecorev1alpha1.PodGangSet
		// pclq is the PodClique to evaluate
		pclq *grovecorev1alpha1.PodClique
		// expected indicates if the function should return true
		expected bool
	}{
		{
			// First ever update required when PCLQ has never been updated
			name: "first ever update required",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("new-hash"),
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					RollingUpdateProgress:           nil,
					CurrentPodGangSetGenerationHash: ptr.To("old-hash"),
				},
			},
			expected: true,
		},
		{
			// In-progress update with matching hash should not reset
			name: "in-progress update with matching hash",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("current-hash"),
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodCliqueRollingUpdateProgress{
						PodGangSetGenerationHash: "current-hash",
						UpdateStartedAt:          metav1.Now(),
					},
				},
			},
			expected: false,
		},
		{
			// Completed update with matching hash should not reset
			name: "completed update with matching hash",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("current-hash"),
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodCliqueRollingUpdateProgress{
						PodGangSetGenerationHash: "current-hash",
						UpdateStartedAt:          metav1.Now(),
						UpdateEndedAt:            &metav1.Time{},
					},
				},
			},
			expected: false,
		},
		{
			// In-progress update with different hash should reset
			name: "in-progress update with different hash",
			pgs: &grovecorev1alpha1.PodGangSet{
				Status: grovecorev1alpha1.PodGangSetStatus{
					CurrentGenerationHash: ptr.To("new-hash"),
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Status: grovecorev1alpha1.PodCliqueStatus{
					RollingUpdateProgress: &grovecorev1alpha1.PodCliqueRollingUpdateProgress{
						PodGangSetGenerationHash: "old-hash",
						UpdateStartedAt:          metav1.Now(),
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldResetOrTriggerRollingUpdate(tt.pgs, tt.pclq)
			assert.Equal(t, tt.expected, result, "shouldResetOrTriggerRollingUpdate should return %v", tt.expected)
		})
	}
}

// TestGetOrderedKindsForSync tests the getOrderedKindsForSync function which returns
// the ordered list of component kinds that need to be synchronized during reconciliation.
func TestGetOrderedKindsForSync(t *testing.T) {
	// Function should return consistent ordered list of kinds
	kinds := getOrderedKindsForSync()

	// Verify expected kinds are present
	expectedKinds := []component.Kind{
		component.KindPod,
	}

	assert.Equal(t, expectedKinds, kinds, "getOrderedKindsForSync should return expected kinds in order")
	assert.NotEmpty(t, kinds, "getOrderedKindsForSync should return non-empty list")
}

// MockOperatorRegistry implements component.OperatorRegistry for testing
type MockOperatorRegistry struct {
	operators map[component.Kind]*MockOperator
}

func (m *MockOperatorRegistry) Register(kind component.Kind, operator component.Operator[grovecorev1alpha1.PodClique]) {
	m.operators[kind] = operator.(*MockOperator)
}

func (m *MockOperatorRegistry) GetOperator(kind component.Kind) (component.Operator[grovecorev1alpha1.PodClique], error) {
	if op, exists := m.operators[kind]; exists {
		return op, nil
	}
	return nil, errors.New("operator not found")
}

func (m *MockOperatorRegistry) GetAllOperators() map[component.Kind]component.Operator[grovecorev1alpha1.PodClique] {
	result := make(map[component.Kind]component.Operator[grovecorev1alpha1.PodClique])
	for kind, op := range m.operators {
		result[kind] = op
	}
	return result
}

// MockOperator implements component.Operator for testing
type MockOperator struct {
	mock.Mock
	syncError   error
	deleteError error
}

func (m *MockOperator) GetExistingResourceNames(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) ([]string, error) {
	args := m.Called(ctx, logger, objMeta)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockOperator) Sync(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) error {
	args := m.Called(ctx, logger, pclq)
	if m.syncError != nil {
		return m.syncError
	}
	return args.Error(0)
}

func (m *MockOperator) Delete(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) error {
	args := m.Called(ctx, logger, objMeta)
	if m.deleteError != nil {
		return m.deleteError
	}
	return args.Error(0)
}

// MockReconcileStatusRecorder implements ctrlcommon.ReconcileStatusRecorder for testing
type MockReconcileStatusRecorder struct {
	mock.Mock
}

func (m *MockReconcileStatusRecorder) RecordStart(ctx context.Context, obj ctrlcommon.ReconciledObject, operationType grovecorev1alpha1.LastOperationType) error {
	args := m.Called(ctx, obj, operationType)
	return args.Error(0)
}

func (m *MockReconcileStatusRecorder) RecordCompletion(ctx context.Context, obj ctrlcommon.ReconciledObject, operationType grovecorev1alpha1.LastOperationType, errResult *ctrlcommon.ReconcileStepResult) error {
	args := m.Called(ctx, obj, operationType, errResult)
	return args.Error(0)
}
