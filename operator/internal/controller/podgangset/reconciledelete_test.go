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
	"errors"
	"testing"

	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TestTriggerDeletionFlow validates the complete deletion flow for various scenarios.
func TestTriggerDeletionFlow(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodGangSet setup function that creates the test resource
		setupPGS func() *grovecorev1alpha1.PodGangSet
		// Mock setup function to configure dependencies
		setupMocks func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet)
	}{
		{
			// Successful deletion flow with all steps completing
			name: "successful deletion flow",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
				controllerutil.AddFinalizer(pgs, constants.FinalizerPodGangSet)
				return pgs
			},
			setupMocks: func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder) {
				// Mock successful status recording
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete).Return(nil)

				// Mock successful resource deletion
				mockOp := &mockOperator{}
				mockOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{
					component.KindPodClique: mockOp,
				}
				registry.On("GetAllOperators").Return(operators)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
				assert.False(t, result.NeedsRequeue())
				// Verify finalizer was removed
				assert.False(t, controllerutil.ContainsFinalizer(pgs, constants.FinalizerPodGangSet))
			},
		},
		{
			// Deletion flow fails during resource deletion
			name: "deletion flow fails during resource deletion",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
				controllerutil.AddFinalizer(pgs, constants.FinalizerPodGangSet)
				return pgs
			},
			setupMocks: func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete).Return(nil)
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete, mock.Anything).Return(nil)

				// Mock resource deletion failure
				mockOp := &mockOperator{}
				mockOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("deletion failed"))

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{
					component.KindPodClique: mockOp,
				}
				registry.On("GetAllOperators").Return(operators)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.HasErrors())
				// Finalizer should still be present due to failure
				assert.True(t, controllerutil.ContainsFinalizer(pgs, constants.FinalizerPodGangSet))
			},
		},
		{
			// Deletion flow fails during status recording
			name: "deletion flow fails during status recording",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
				controllerutil.AddFinalizer(pgs, constants.FinalizerPodGangSet)
				return pgs
			},
			setupMocks: func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder) {
				// Mock status recording failure
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete).Return(errors.New("recording failed"))
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete, mock.Anything).Return(nil)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.HasErrors())
			},
		},
		{
			// Deletion flow requeues during cleanup verification when resources remain
			name: "deletion flow fails during cleanup verification",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
				controllerutil.AddFinalizer(pgs, constants.FinalizerPodGangSet)
				return pgs
			},
			setupMocks: func(registry *mockOperatorRegistry, recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete).Return(nil)
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete, mock.Anything).Return(nil)

				// Mock successful deletion but failed cleanup verification
				mockOp := &mockOperator{}
				mockOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{"remaining-resource"}, nil) // Resources still exist
				// Resources still exist

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{
					component.KindPodClique: mockOp,
				}
				registry.On("GetAllOperators").Return(operators)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.NeedsRequeue())
				assert.False(t, result.HasErrors()) // Cleanup verification returns requeue, not error
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

			result := reconciler.triggerDeletionFlow(
				context.Background(),
				logr.Discard(),
				pgs,
			)

			tt.validate(t, result, pgs)
		})
	}
}

// TestRecordDeletionStart validates deletion start recording for various scenarios.
func TestRecordDeletionStart(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Mock setup function to configure recorder behavior
		setupMock func(recorder *mockReconcileStatusRecorder)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult)
	}{
		{
			// Successful recording of deletion start
			name: "successful deletion start recording",
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete).Return(nil)
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
				recorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete).Return(errors.New("recording failed"))
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

			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).Build()
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMock(mockRecorder)

			reconciler := &Reconciler{
				reconcileStatusRecorder: mockRecorder,
			}

			result := reconciler.recordDeletionStart(
				context.Background(),
				logr.Discard(),
				pgs,
			)

			tt.validate(t, result)
		})
	}
}

// TestDeletePodGangSetResources validates resource deletion for various scenarios.
func TestDeletePodGangSetResources(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Mock setup function to configure operator registry behavior
		setupMock func(registry *mockOperatorRegistry)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult)
	}{
		{
			// All resources deleted successfully
			name: "all resources deleted successfully",
			setupMock: func(registry *mockOperatorRegistry) {
				mockOp := &mockOperator{}
				mockOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{
					component.KindPodClique:             mockOp,
					component.KindPodCliqueScalingGroup: mockOp,
					component.KindServiceAccount:        mockOp,
				}
				registry.On("GetAllOperators").Return(operators)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
			},
		},
		{
			// Some resource deletion fails
			name: "resource deletion fails",
			setupMock: func(registry *mockOperatorRegistry) {
				successOp := &mockOperator{}
				successOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				failOp := &mockOperator{}
				failOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("deletion failed"))

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{
					component.KindPodClique:             successOp,
					component.KindPodCliqueScalingGroup: failOp,
				}
				registry.On("GetAllOperators").Return(operators)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.HasErrors())
			},
		},
		{
			// No operators to delete
			name: "no operators to delete",
			setupMock: func(registry *mockOperatorRegistry) {
				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{}
				registry.On("GetAllOperators").Return(operators)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).Build()
			mockRegistry := &mockOperatorRegistry{}
			tt.setupMock(mockRegistry)

			reconciler := &Reconciler{
				operatorRegistry: mockRegistry,
			}

			result := reconciler.deletePodGangSetResources(
				context.Background(),
				logr.Discard(),
				pgs,
			)

			tt.validate(t, result)
		})
	}
}

// TestVerifyNoResourcesAwaitsCleanup validates cleanup verification for various scenarios.
func TestVerifyNoResourcesAwaitsCleanup(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Mock setup function to configure operator registry behavior
		setupMock func(registry *mockOperatorRegistry)
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult)
	}{
		{
			// All resources cleaned up successfully
			name: "all resources cleaned up",
			setupMock: func(registry *mockOperatorRegistry) {
				mockOp := &mockOperator{}
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{
					component.KindPodClique: mockOp,
				}
				registry.On("GetAllOperators").Return(operators)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
			},
		},
		{
			// Some resources still exist
			name: "resources still exist",
			setupMock: func(registry *mockOperatorRegistry) {
				mockOp := &mockOperator{}
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{"remaining-resource"}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{
					component.KindPodClique: mockOp,
				}
				registry.On("GetAllOperators").Return(operators)
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.True(t, result.NeedsRequeue())
				assert.False(t, result.HasErrors()) // Should requeue, not error
			},
		},
		{
			// Error checking resource existence
			name: "error checking resource existence",
			setupMock: func(registry *mockOperatorRegistry) {
				mockOp := &mockOperator{}
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, errors.New("check failed"))

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{
					component.KindPodClique: mockOp,
				}
				registry.On("GetAllOperators").Return(operators)
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

			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).Build()
			mockRegistry := &mockOperatorRegistry{}
			tt.setupMock(mockRegistry)

			reconciler := &Reconciler{
				operatorRegistry: mockRegistry,
			}

			result := reconciler.verifyNoResourcesAwaitsCleanup(
				context.Background(),
				logr.Discard(),
				pgs,
			)

			tt.validate(t, result)
		})
	}
}

// TestRemoveFinalizer validates finalizer removal for various scenarios.
func TestRemoveFinalizer(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodGangSet setup function that creates the test resource
		setupPGS func() *grovecorev1alpha1.PodGangSet
		// Expected reconcile step result validation
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet)
	}{
		{
			// Finalizer is removed when present
			name: "removes finalizer when present",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
				controllerutil.AddFinalizer(pgs, constants.FinalizerPodGangSet)
				return pgs
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
				assert.False(t, controllerutil.ContainsFinalizer(pgs, constants.FinalizerPodGangSet))
			},
		},
		{
			// No action when finalizer not present
			name: "no action when finalizer not present",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult, pgs *grovecorev1alpha1.PodGangSet) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.HasErrors())
				assert.False(t, controllerutil.ContainsFinalizer(pgs, constants.FinalizerPodGangSet))
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

			result := reconciler.removeFinalizer(
				context.Background(),
				logr.Discard(),
				pgs,
			)

			tt.validate(t, result, pgs)
		})
	}
}

// TestRecordIncompleteDeletion validates incomplete deletion recording for various error scenarios.
func TestRecordIncompleteDeletion(t *testing.T) {
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
			// Successful recording of incomplete deletion
			name: "successful incomplete deletion recording",
			errorResult: func() *ctrlcommon.ReconcileStepResult {
				r := ctrlcommon.ReconcileWithErrors("test error", errors.New("original error"))
				return &r
			}(),
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete, mock.Anything).Return(nil)
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
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete, mock.Anything).Return(errors.New("recording failed"))
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, result.HasErrors())
				assert.Len(t, result.GetErrors(), 2)
			},
		},
		{
			// Multiple original errors preserved
			name: "multiple original errors preserved",
			errorResult: func() *ctrlcommon.ReconcileStepResult {
				r := ctrlcommon.ReconcileWithErrors("test error",
					errors.New("error 1"),
					errors.New("error 2"))
				return &r
			}(),
			setupMock: func(recorder *mockReconcileStatusRecorder) {
				recorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete, mock.Anything).Return(nil)
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

			pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).Build()
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMock(mockRecorder)

			reconciler := &Reconciler{
				reconcileStatusRecorder: mockRecorder,
			}

			result := reconciler.recordIncompleteDeletion(
				context.Background(),
				logr.Discard(),
				pgs,
				tt.errorResult,
			)

			tt.validate(t, result)
		})
	}
}
