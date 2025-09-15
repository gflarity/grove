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
	"errors"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TestReconcileSpec validates the main spec reconciliation flow for various scenarios.
func TestReconcileSpec(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodGangSet setup function that creates the test resource
		setupPGS func() *grovecorev1alpha1.PodGangSet
		// Mock setup function to configure operator registry behavior
		setupMocks func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet)
	}{
		{
			// Successful spec reconciliation with all steps completing
			name: "successful spec reconciliation",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
					WithReplicas(1).
					WithStandaloneClique("worker").
					Build()
			},
			setupMocks: func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder) {
				// Mock successful status recording
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(nil)
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, (*ctrlcommon.ReconcileStepResult)(nil)).Return(nil)

				// Mock successful operator sync for all component kinds
				mockOp := &mockOperator{}
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				registry.On("GetOperator", mock.Anything).Return(mockOp, nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
				// Verify finalizer was added
				assert.True(t, controllerutil.ContainsFinalizer(pgs, grovecorev1alpha1.FinalizerPodGangSet))
				// Verify observed generation was updated
				assert.NotNil(t, pgs.Status.ObservedGeneration)
				assert.Equal(t, pgs.Generation, *pgs.Status.ObservedGeneration)
			},
		},
		{
			// Spec reconciliation fails during sync step
			name: "sync failure causes incomplete reconciliation",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
					WithReplicas(1).
					Build()
			},
			setupMocks: func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(nil)
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.Anything).Return(nil)

				// Mock operator sync failure
				mockOp := &mockOperator{}
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("sync failed"))
				registry.On("GetOperator", mock.Anything).Return(mockOp, nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.HasErrors())
			},
		},
		{
			// Spec reconciliation with requeue-after error
			name: "sync with requeue after error",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
					WithReplicas(1).
					Build()
			},
			setupMocks: func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(nil)
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.Anything).Return(nil)

				// Mock operator sync with requeue-after error
				mockOp := &mockOperator{}
				requeueErr := errors.New("requeue after error")
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(requeueErr)
				registry.On("GetOperator", mock.Anything).Return(mockOp, nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.NeedsRequeue())
			},
		},
		{
			// Spec reconciliation with continue-and-requeue error
			name: "sync with continue and requeue error",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
					WithReplicas(1).
					Build()
			},
			setupMocks: func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(nil)
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.Anything).Return(nil)

				// Mock operator sync with continue-and-requeue error
				mockOp := &mockOperator{}
				continueErr := errors.New("continue and requeue error")
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(continueErr)
				registry.On("GetOperator", mock.Anything).Return(mockOp, nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.NeedsRequeue())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := tt.setupPGS()
			fakeClient := testutils.SetupFakeClient(pgs)

			mockRegistry := &mockOperatorRegistry{}
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMocks(mockRegistry, mockRecorder)

			reconciler := &Reconciler{
				client:                  fakeClient,
				reconcileStatusRecorder: mockRecorder,
				operatorRegistry:        mockRegistry,
			}

			result := reconciler.reconcileSpec(
				testutils.SetupTestContext(),
				testutils.SetupTestLogger(),
				pgs,
			)

			tt.validate(t, result, pgs)
		})
	}
}

// TestEnsureFinalizer validates finalizer addition logic for various scenarios.
func TestEnsureFinalizer(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodGangSet setup function that creates the test resource
		setupPGS func() *grovecorev1alpha1.PodGangSet
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet)
	}{
		{
			// Finalizer is added when not present
			name: "adds finalizer when not present",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
					WithReplicas(1).
					Build()
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
				assert.True(t, controllerutil.ContainsFinalizer(pgs, grovecorev1alpha1.FinalizerPodGangSet))
			},
		},
		{
			// No action when finalizer already present
			name: "no action when finalizer already present",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
					WithReplicas(1).
					Build()
				controllerutil.AddFinalizer(pgs, grovecorev1alpha1.FinalizerPodGangSet)
				return pgs
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
				assert.True(t, controllerutil.ContainsFinalizer(pgs, grovecorev1alpha1.FinalizerPodGangSet))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := tt.setupPGS()
			fakeClient := testutils.SetupFakeClient(pgs)

			reconciler := &Reconciler{
				client: fakeClient,
			}

			result := reconciler.ensureFinalizer(
				testutils.SetupTestContext(),
				testutils.SetupTestLogger(),
				pgs,
			)

			tt.validate(t, result, pgs)
		})
	}
}

// TestRecordReconcileStart validates reconcile start recording for various scenarios.
func TestRecordReconcileStart(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Mock setup function to configure recorder behavior
		setupMock func(recorder *mockReconcileStatusRecorder)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult)
	}{
		{
			// Successful recording of reconcile start
			name: "successful reconcile start recording",
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
			},
		},
		{
			// Recording failure causes error result
			name: "recording failure causes error",
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(errors.New("recording failed"))
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.HasErrors())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").Build()
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMock(mockRecorder)

			reconciler := &Reconciler{
				reconcileStatusRecorder: mockRecorder,
			}

			result := reconciler.recordReconcileStart(
				testutils.SetupTestContext(),
				testutils.SetupTestLogger(),
				pgs,
			)

			tt.validate(t, result)
		})
	}
}

// TestSyncPodGangSetResources validates resource synchronization for various component scenarios.
func TestSyncPodGangSetResources(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Mock setup function to configure operator registry behavior
		setupMock func(registry *mockOperatorRegistry)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult)
	}{
		{
			// All components sync successfully
			name: "all components sync successfully",
			setupMock: func(registry *mockOperatorRegistry) {
				mockOp := &mockOperator{}
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				registry.On("GetOperator", mock.Anything).Return(mockOp, nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
			},
		},
		{
			// Component sync fails with regular error
			name: "component sync fails with error",
			setupMock: func(registry *mockOperatorRegistry) {
				mockOp := &mockOperator{}
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("sync failed"))
				registry.On("GetOperator", mock.Anything).Return(mockOp, nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.HasErrors())
			},
		},
		{
			// Component sync with continue-and-requeue error
			name: "component sync with continue and requeue",
			setupMock: func(registry *mockOperatorRegistry) {
				mockOp := &mockOperator{}
				continueErr := errors.New("continue and requeue error")
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(continueErr)
				registry.On("GetOperator", mock.Anything).Return(mockOp, nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.NeedsRequeue())
			},
		},
		{
			// Component sync with requeue-after error
			name: "component sync with requeue after",
			setupMock: func(registry *mockOperatorRegistry) {
				mockOp := &mockOperator{}
				requeueErr := errors.New("requeue after error")
				mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(requeueErr)
				registry.On("GetOperator", mock.Anything).Return(mockOp, nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.NeedsRequeue())
			},
		},
		{
			// Operator registry fails to get operator
			name: "operator registry get failure",
			setupMock: func(registry *mockOperatorRegistry) {
				registry.On("GetOperator", mock.Anything).Return(nil, errors.New("operator not found"))
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.HasErrors())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").Build()
			mockRegistry := &mockOperatorRegistry{}
			tt.setupMock(mockRegistry)

			reconciler := &Reconciler{
				operatorRegistry: mockRegistry,
			}

			result := reconciler.syncPodGangSetResources(
				testutils.SetupTestContext(),
				testutils.SetupTestLogger(),
				pgs,
			)

			tt.validate(t, result)
		})
	}
}

// TestRecordReconcileSuccess validates successful reconcile recording for various scenarios.
func TestRecordReconcileSuccess(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Mock setup function to configure recorder behavior
		setupMock func(recorder *mockReconcileStatusRecorder)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult)
	}{
		{
			// Successful recording of reconcile completion
			name: "successful reconcile completion recording",
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, (*ctrlcommon.ReconcileStepResult)(nil)).Return(nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
			},
		},
		{
			// Recording failure causes error result
			name: "recording failure causes error",
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, (*ctrlcommon.ReconcileStepResult)(nil)).Return(errors.New("recording failed"))
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.HasErrors())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").Build()
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMock(mockRecorder)

			reconciler := &Reconciler{
				reconcileStatusRecorder: mockRecorder,
			}

			result := reconciler.recordReconcileSuccess(
				testutils.SetupTestContext(),
				testutils.SetupTestLogger(),
				pgs,
			)

			tt.validate(t, result)
		})
	}
}

// TestUpdateObservedGeneration validates observed generation update for various scenarios.
func TestUpdateObservedGeneration(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodGangSet setup function that creates the test resource
		setupPGS func() *grovecorev1alpha1.PodGangSet
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet)
	}{
		{
			// Observed generation is updated successfully
			name: "observed generation updated successfully",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").Build()
				pgs.Generation = 5
				return pgs
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
				require.NotNil(t, pgs.Status.ObservedGeneration)
				assert.Equal(t, int64(5), *pgs.Status.ObservedGeneration)
			},
		},
		{
			// Observed generation update from zero
			name: "observed generation updated from zero",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").Build()
				pgs.Generation = 1
				return pgs
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
				require.NotNil(t, pgs.Status.ObservedGeneration)
				assert.Equal(t, int64(1), *pgs.Status.ObservedGeneration)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := tt.setupPGS()
			fakeClient := testutils.SetupFakeClient(pgs)

			reconciler := &Reconciler{
				client: fakeClient,
			}

			result := reconciler.updateObservedGeneration(
				testutils.SetupTestContext(),
				testutils.SetupTestLogger(),
				pgs,
			)

			tt.validate(t, result, pgs)
		})
	}
}

// TestRecordIncompleteReconcile validates incomplete reconcile recording for various error scenarios.
func TestRecordIncompleteReconcile(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Error result to record as incomplete
		errorResult *ctrlcommon.ReconcileStepResult
		// Mock setup function to configure recorder behavior
		setupMock func(recorder *mockReconcileStatusRecorder)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult)
	}{
		{
			// Successful recording of incomplete reconcile
			name: "successful incomplete reconcile recording",
			errorResult: func() *ctrlcommon.ReconcileStepResult {
				r := ctrlcommon.ReconcileWithErrors("test error", errors.New("original error"))
				return &r
			}(),
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.Anything).Return(nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, result.HasErrors())
				assert.Len(t, result.GetErrors(), 1)
			},
		},
		{
			// Recording failure adds additional error
			name: "recording failure adds additional error",
			errorResult: func() *ctrlcommon.ReconcileStepResult {
				r := ctrlcommon.ReconcileWithErrors("test error", errors.New("original error"))
				return &r
			}(),
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.Anything).Return(errors.New("recording failed"))
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, result.HasErrors())
				assert.Len(t, result.GetErrors(), 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").Build()
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMock(mockRecorder)

			reconciler := &Reconciler{
				reconcileStatusRecorder: mockRecorder,
			}

			result := reconciler.recordIncompleteReconcile(
				testutils.SetupTestContext(),
				testutils.SetupTestLogger(),
				pgs,
				tt.errorResult,
			)

			tt.validate(t, result)
		})
	}
}

// TestGetOrderedKindsForSync validates the component synchronization order.
func TestGetOrderedKindsForSync(t *testing.T) {
	// Component kinds should be returned in dependency order
	expectedOrder := []component.Kind{
		component.KindServiceAccount,
		component.KindRole,
		component.KindRoleBinding,
		component.KindServiceAccountTokenSecret,
		component.KindHeadlessService,
		component.KindHorizontalPodAutoscaler,
		component.KindPodClique,
		component.KindPodCliqueScalingGroup,
		component.KindPodGang,
	}

	actualOrder := getOrderedKindsForSync()

	assert.Equal(t, expectedOrder, actualOrder, "Component kinds should be in dependency order")
	assert.Len(t, actualOrder, len(expectedOrder), "All expected component kinds should be present")
}
