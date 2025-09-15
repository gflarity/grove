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
	"errors"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestEnsureFinalizer tests the finalizer addition functionality
func TestEnsureFinalizer(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object to test finalizer addition on
		pclq *grovecorev1alpha1.PodClique
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
		// expectedFinalizerAdded indicates whether the finalizer should be added
		expectedFinalizerAdded bool
	}{
		{
			// Test adding finalizer when not present
			name: "add_finalizer_when_not_present",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					// No finalizers initially
				},
			},
			expectedContinue:       false,
			expectedError:          false,
			expectedFinalizerAdded: true,
		},
		{
			// Test when finalizer is already present
			name: "finalizer_already_present",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{grovecorev1alpha1.FinalizerPodClique},
				},
			},
			expectedContinue:       true,
			expectedError:          false,
			expectedFinalizerAdded: false,
		},
		{
			// Test with multiple finalizers, PodClique finalizer not present
			name: "add_finalizer_with_other_finalizers",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Finalizers: []string{
						"other-finalizer",
						"another-finalizer",
					},
				},
			},
			expectedContinue:       false,
			expectedError:          false,
			expectedFinalizerAdded: true,
		},
		{
			// Test with multiple finalizers including PodClique finalizer
			name: "finalizer_present_with_others",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Finalizers: []string{
						"other-finalizer",
						grovecorev1alpha1.FinalizerPodClique,
						"another-finalizer",
					},
				},
			},
			expectedContinue:       true,
			expectedError:          false,
			expectedFinalizerAdded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.pclq).
				Build()

			// Create reconciler
			reconciler := &Reconciler{
				client: fakeClient,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test
			result := reconciler.ensureFinalizer(ctx, logger, tt.pclq)

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
		})
	}
}

// TestRecordReconcileStart tests the reconcile start recording functionality
func TestRecordReconcileStart(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object for which to record reconcile start
		pclq *grovecorev1alpha1.PodClique
		// setupMocks configures the mock reconcile status recorder
		setupMocks func(*mockReconcileStatusRecorder)
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Test successful reconcile start recording
			name: "successful_reconcile_start_recording",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRecorder *mockReconcileStatusRecorder) {
				mockRecorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(nil)
			},
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// Test reconcile start recording with error
			name: "reconcile_start_recording_error",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRecorder *mockReconcileStatusRecorder) {
				mockRecorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(errors.New("recording failed"))
			},
			expectedContinue: false,
			expectedError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mocks
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMocks(mockRecorder)

			// Create reconciler
			reconciler := &Reconciler{
				reconcileStatusRecorder: mockRecorder,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test
			result := reconciler.recordReconcileStart(ctx, logger, tt.pclq)

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

			// Verify mock expectations
			mockRecorder.AssertExpectations(t)
		})
	}
}

// TestSyncPCLQResources tests the resource synchronization functionality
func TestSyncPCLQResources(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object for which to sync resources
		pclq *grovecorev1alpha1.PodClique
		// setupMocks configures the mock operator registry
		setupMocks func(*mockOperatorRegistry)
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Test successful resource synchronization
			name: "successful_resource_sync",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					RoleName: "worker",
					Replicas: 3,
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "test", Image: "test:latest"}},
					},
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockRegistry.On("GetOperator", component.KindPod).Return(mockPodOperator, nil)
			},
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// Test resource sync with operator error
			name: "resource_sync_with_operator_error",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					RoleName: "worker",
					Replicas: 3,
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "test", Image: "test:latest"}},
					},
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("sync failed"))
				mockRegistry.On("GetOperator", component.KindPod).Return(mockPodOperator, nil)
			},
			expectedContinue: false,
			expectedError:    true,
		},
		{
			// Test resource sync with operator not found
			name: "resource_sync_operator_not_found",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					RoleName: "worker",
					Replicas: 3,
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "test", Image: "test:latest"}},
					},
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockRegistry.On("GetOperator", component.KindPod).Return(nil, errors.New("operator not found"))
			},
			expectedContinue: false,
			expectedError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mocks
			mockRegistry := &mockOperatorRegistry{}
			tt.setupMocks(mockRegistry)

			// Create reconciler
			reconciler := &Reconciler{
				operatorRegistry: mockRegistry,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test
			result := reconciler.syncPCLQResources(ctx, logger, tt.pclq)

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

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
		})
	}
}

// TestRecordReconcileSuccess tests the reconcile success recording functionality
func TestRecordReconcileSuccess(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object for which to record reconcile success
		pclq *grovecorev1alpha1.PodClique
		// setupMocks configures the mock reconcile status recorder
		setupMocks func(*mockReconcileStatusRecorder)
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Test successful reconcile success recording
			name: "successful_reconcile_success_recording",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRecorder *mockReconcileStatusRecorder) {
				mockRecorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, (*ctrlcommon.ReconcileStepResult)(nil)).Return(nil)
			},
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// Test reconcile success recording with error
			name: "reconcile_success_recording_error",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRecorder *mockReconcileStatusRecorder) {
				mockRecorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, (*ctrlcommon.ReconcileStepResult)(nil)).Return(errors.New("recording failed"))
			},
			expectedContinue: false,
			expectedError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mocks
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMocks(mockRecorder)

			// Create reconciler
			reconciler := &Reconciler{
				reconcileStatusRecorder: mockRecorder,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test
			result := reconciler.recordReconcileSuccess(ctx, logger, tt.pclq)

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

			// Verify mock expectations
			mockRecorder.AssertExpectations(t)
		})
	}
}

// TestUpdateObservedGeneration tests the observed generation update functionality
func TestUpdateObservedGeneration(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object for which to update observed generation
		pclq *grovecorev1alpha1.PodClique
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Test successful observed generation update
			name: "successful_observed_generation_update",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Generation: 5,
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ObservedGeneration: ptr.To[int64](3), // Different from current generation
				},
			},
			expectedContinue: false,
			expectedError:    true,
		},
		{
			// Test observed generation update when already up to date
			name: "observed_generation_already_current",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Generation: 5,
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ObservedGeneration: ptr.To[int64](5), // Same as current generation
				},
			},
			expectedContinue: false,
			expectedError:    true,
		},
		{
			// Test observed generation update when nil
			name: "observed_generation_nil",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Generation: 1,
				},
				Status: grovecorev1alpha1.PodCliqueStatus{
					ObservedGeneration: nil,
				},
			},
			expectedContinue: false,
			expectedError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.pclq).
				Build()

			// Create reconciler
			reconciler := &Reconciler{
				client: fakeClient,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test
			result := reconciler.updateObservedGeneration(ctx, logger, tt.pclq)

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

			// Verify that observed generation was updated
			if !result.HasErrors() {
				assert.Equal(t, tt.pclq.Generation, *tt.pclq.Status.ObservedGeneration, "ObservedGeneration should match current generation")
			}
		})
	}
}

// TestRecordIncompleteReconcile tests the incomplete reconcile recording functionality
func TestRecordIncompleteReconcile(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object for which to record incomplete reconcile
		pclq *grovecorev1alpha1.PodClique
		// errResult is the error result to record
		errResult *ctrlcommon.ReconcileStepResult
		// setupMocks configures the mock reconcile status recorder
		setupMocks func(*mockReconcileStatusRecorder)
		// expectedError indicates whether an error should be returned
		expectedError bool
		// expectedErrorCount is the expected number of errors in the result
		expectedErrorCount int
	}{
		{
			// Test successful incomplete reconcile recording
			name: "successful_incomplete_reconcile_recording",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			errResult: &ctrlcommon.ReconcileStepResult{},
			setupMocks: func(mockRecorder *mockReconcileStatusRecorder) {
				mockRecorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.AnythingOfType("*common.ReconcileStepResult")).Return(nil)
			},
			expectedError:      false,
			expectedErrorCount: 0,
		},
		{
			// Test incomplete reconcile recording with recording error
			name: "incomplete_reconcile_recording_error",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			errResult: &ctrlcommon.ReconcileStepResult{},
			setupMocks: func(mockRecorder *mockReconcileStatusRecorder) {
				mockRecorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.AnythingOfType("*common.ReconcileStepResult")).Return(errors.New("recording failed"))
			},
			expectedError:      true,
			expectedErrorCount: 1,
		},
		{
			// Test incomplete reconcile recording with original error and recording error
			name: "incomplete_reconcile_with_original_and_recording_errors",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			errResult: func() *ctrlcommon.ReconcileStepResult {
				result := ctrlcommon.ReconcileWithErrors("original error", errors.New("original failure"))
				return &result
			}(),
			setupMocks: func(mockRecorder *mockReconcileStatusRecorder) {
				mockRecorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.AnythingOfType("*common.ReconcileStepResult")).Return(errors.New("recording failed"))
			},
			expectedError:      true,
			expectedErrorCount: 2, // Original error + recording error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mocks
			mockRecorder := &mockReconcileStatusRecorder{}
			tt.setupMocks(mockRecorder)

			// Create reconciler
			reconciler := &Reconciler{
				reconcileStatusRecorder: mockRecorder,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test
			result := reconciler.recordIncompleteReconcile(ctx, logger, tt.pclq, tt.errResult)

			// Verify results
			if tt.expectedError {
				assert.True(t, result.HasErrors(), "Expected error but got none")
				assert.Len(t, result.GetErrors(), tt.expectedErrorCount, "Expected %d errors but got %d", tt.expectedErrorCount, len(result.GetErrors()))
			} else {
				assert.False(t, result.HasErrors(), "Expected no error but got one")
			}

			// Verify mock expectations
			mockRecorder.AssertExpectations(t)
		})
	}
}
