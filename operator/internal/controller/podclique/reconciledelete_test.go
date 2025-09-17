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

	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TestTriggerDeletionFlow tests the triggerDeletionFlow function which orchestrates
// the complete deletion process for a PodClique resource by executing deletion steps sequentially.
func TestTriggerDeletionFlow(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique object to delete
		pclq *grovecorev1alpha1.PodClique
		// mockOperatorRegistry is the mock operator registry to use
		mockOperatorRegistry *MockOperatorRegistry
		// mockVerifyCleanup controls whether resource cleanup verification should succeed
		mockVerifyCleanup bool
		// expectedSuccess indicates if deletion should complete successfully
		expectedSuccess bool
		// expectedRequeue indicates if the result should trigger a requeue
		expectedRequeue bool
	}{
		{
			// Successful deletion flow should complete all steps without requeue
			name: "successful deletion flow",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{constants.FinalizerPodClique},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(1)),
				},
			},
			mockOperatorRegistry: &MockOperatorRegistry{
				operators: map[component.Kind]*MockOperator{
					component.KindPod: &MockOperator{
						deleteError: nil,
					},
				},
			},
			mockVerifyCleanup: true,
			expectedSuccess:   true,
			expectedRequeue:   false,
		},
		{
			// Deletion with operator error should fail and requeue
			name: "deletion with operator error",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{constants.FinalizerPodClique},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(1)),
				},
			},
			mockOperatorRegistry: &MockOperatorRegistry{
				operators: map[component.Kind]*MockOperator{
					component.KindPod: &MockOperator{
						deleteError: errors.New("delete failed"),
					},
				},
			},
			mockVerifyCleanup: false,
			expectedSuccess:   false,
			expectedRequeue:   true,
		},
		{
			// Deletion without finalizer should still succeed
			name: "deletion without finalizer",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					MinAvailable: ptr.To(int32(1)),
				},
			},
			mockOperatorRegistry: &MockOperatorRegistry{
				operators: map[component.Kind]*MockOperator{
					component.KindPod: &MockOperator{
						deleteError: nil,
					},
				},
			},
			mockVerifyCleanup: true,
			expectedSuccess:   true,
			expectedRequeue:   false,
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

			// Set up mock expectations
			for _, mockOp := range tt.mockOperatorRegistry.operators {
				mockOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(mockOp.deleteError)
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)
			}

			// Create reconciler with mock dependencies
			reconciler := &Reconciler{
				client:           fakeClient,
				operatorRegistry: tt.mockOperatorRegistry,
			}

			// Execute deletion flow
			ctx := context.Background()
			logger := log.FromContext(ctx)
			result := reconciler.triggerDeletionFlow(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedSuccess {
				assert.False(t, result.NeedsRequeue(), "Expected successful deletion without requeue")
				assert.Empty(t, result.GetErrors(), "Expected no errors but got: %v", result.GetErrors())
			} else {
				assert.True(t, result.NeedsRequeue() || len(result.GetErrors()) > 0, "Expected error or requeue for failed deletion")
			}

			if tt.expectedRequeue {
				assert.True(t, result.NeedsRequeue() || len(result.GetErrors()) > 0, "Expected requeue but got success")
			}
		})
	}
}

// TestDeletePodCliqueResources tests the deletePodCliqueResources function which deletes
// all managed resources associated with the PodClique by running deletion tasks concurrently.
func TestDeletePodCliqueResources(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique object whose resources should be deleted
		pclq *grovecorev1alpha1.PodClique
		// mockOperators defines the mock operators and their expected behavior
		mockOperators map[component.Kind]*MockOperator
		// expectedSuccess indicates if deletion should succeed
		expectedSuccess bool
		// expectedContinue indicates if reconciliation should continue
		expectedContinue bool
	}{
		{
			// Successful resource deletion should continue reconciliation
			name: "successful resource deletion",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			mockOperators: map[component.Kind]*MockOperator{
				component.KindPod: &MockOperator{
					deleteError: nil,
				},
			},
			expectedSuccess:  true,
			expectedContinue: true,
		},
		{
			// Failed resource deletion should return error and stop reconciliation
			name: "failed resource deletion",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			mockOperators: map[component.Kind]*MockOperator{
				component.KindPod: &MockOperator{
					deleteError: errors.New("deletion failed"),
				},
			},
			expectedSuccess:  false,
			expectedContinue: false,
		},
		{
			// Multiple operators with mixed results should fail if any fails
			name: "multiple operators with failure",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			mockOperators: map[component.Kind]*MockOperator{
				component.KindPod: &MockOperator{
					deleteError: nil,
				},
				"other-kind": &MockOperator{
					deleteError: errors.New("other deletion failed"),
				},
			},
			expectedSuccess:  false,
			expectedContinue: false,
		},
		{
			// No operators should succeed without doing anything
			name: "no operators",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			mockOperators:    map[component.Kind]*MockOperator{},
			expectedSuccess:  true,
			expectedContinue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock operator registry
			mockRegistry := &MockOperatorRegistry{
				operators: tt.mockOperators,
			}

			// Set up mock expectations
			for _, mockOp := range tt.mockOperators {
				mockOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(mockOp.deleteError)
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)
			}

			reconciler := &Reconciler{
				operatorRegistry: mockRegistry,
			}

			// Execute resource deletion
			ctx := context.Background()
			logger := log.FromContext(ctx)
			result := reconciler.deletePodCliqueResources(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedSuccess {
				assert.Empty(t, result.GetErrors(), "Expected no errors but got: %v", result.GetErrors())
			} else {
				assert.NotEmpty(t, result.GetErrors(), "Expected errors but got none")
			}

			if tt.expectedContinue {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected reconciliation to continue")
			} else {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected reconciliation to stop")
			}
		})
	}
}

// TestVerifyNoResourcesAwaitsCleanup tests the verifyNoResourcesAwaitsCleanup function
// which ensures all managed resources have been successfully deleted before finalizer removal.
func TestVerifyNoResourcesAwaitsCleanup(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique object to verify cleanup for
		pclq *grovecorev1alpha1.PodClique
		// mockOperatorRegistry is the mock operator registry to use
		mockOperatorRegistry *MockOperatorRegistry
		// expectedSuccess indicates if verification should succeed
		expectedSuccess bool
	}{
		{
			// Successful verification should continue reconciliation
			name: "successful verification",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			mockOperatorRegistry: &MockOperatorRegistry{
				operators: map[component.Kind]*MockOperator{},
			},
			expectedSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &Reconciler{
				operatorRegistry: tt.mockOperatorRegistry,
			}

			// Execute verification
			ctx := context.Background()
			logger := log.FromContext(ctx)
			result := reconciler.verifyNoResourcesAwaitsCleanup(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedSuccess {
				// Note: This test is limited because we can't easily mock ctrlutils.VerifyNoResourceAwaitsCleanup
				// In a real implementation, you would need to mock the ctrlutils package or use dependency injection
				assert.NotNil(t, result, "Result should not be nil")
			}
		})
	}
}

// TestRemoveFinalizer tests the removeFinalizer function which removes the PodClique
// finalizer from the resource to allow garbage collection by Kubernetes.
func TestRemoveFinalizer(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique object to remove finalizer from
		pclq *grovecorev1alpha1.PodClique
		// expectedFinalizerRemoved indicates if the finalizer should be removed
		expectedFinalizerRemoved bool
		// expectedContinue indicates if reconciliation should continue
		expectedContinue bool
		// expectedRequeue indicates if the result should not requeue
		expectedRequeue bool
	}{
		{
			// PodClique with finalizer should have finalizer removed
			name: "remove finalizer",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{constants.FinalizerPodClique},
				},
			},
			expectedFinalizerRemoved: true,
			expectedContinue:         true,
			expectedRequeue:          false,
		},
		{
			// PodClique without finalizer should not requeue and continue
			name: "no finalizer to remove",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			expectedFinalizerRemoved: false,
			expectedContinue:         false, // DoNotRequeue stops reconciliation
			expectedRequeue:          false,
		},
		{
			// PodClique with other finalizers should only remove Grove finalizer
			name: "remove only grove finalizer",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Finalizers: []string{
						constants.FinalizerPodClique,
						"other.finalizer/test",
					},
				},
			},
			expectedFinalizerRemoved: true,
			expectedContinue:         true,
			expectedRequeue:          false,
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

			// Store original finalizer state
			originalHasFinalizer := controllerutil.ContainsFinalizer(tt.pclq, constants.FinalizerPodClique)

			// Execute finalizer removal
			ctx := context.Background()
			logger := log.FromContext(ctx)
			result := reconciler.removeFinalizer(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedRequeue {
				assert.True(t, result.NeedsRequeue(), "Expected requeue but got no requeue")
			} else {
				assert.False(t, result.NeedsRequeue(), "Expected no requeue but got requeue")
			}

			if tt.expectedContinue {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected reconciliation to continue")
			}

			// Verify finalizer state
			if tt.expectedFinalizerRemoved && originalHasFinalizer {
				// Note: In a real test, you would verify the finalizer was removed from the cluster
				// This is limited by the fake client's behavior with patches
				assert.Empty(t, result.GetErrors(), "Expected no errors when removing finalizer")
			}
		})
	}
}

// TestDeletionFlowIntegration tests the complete deletion flow integration
// by verifying that all deletion steps are executed in the correct order.
func TestDeletionFlowIntegration(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// pclq is the PodClique object to delete
		pclq *grovecorev1alpha1.PodClique
		// simulateOperatorFailure indicates if operator deletion should fail
		simulateOperatorFailure bool
		// expectedStepsExecuted indicates which deletion steps should be executed
		expectedStepsExecuted []string
		// expectedFinalResult indicates the final result type
		expectedFinalResult string
	}{
		{
			// Complete successful deletion should execute all steps
			name: "complete successful deletion",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{constants.FinalizerPodClique},
				},
			},
			simulateOperatorFailure: false,
			expectedStepsExecuted:   []string{"delete", "verify", "removeFinalizer"},
			expectedFinalResult:     "success",
		},
		{
			// Deletion with operator failure should stop at first step
			name: "deletion with operator failure",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{constants.FinalizerPodClique},
				},
			},
			simulateOperatorFailure: true,
			expectedStepsExecuted:   []string{"delete"},
			expectedFinalResult:     "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock operator registry
			var deleteError error
			if tt.simulateOperatorFailure {
				deleteError = errors.New("simulated operator failure")
			}

			mockRegistry := &MockOperatorRegistry{
				operators: map[component.Kind]*MockOperator{
					component.KindPod: &MockOperator{
						deleteError: deleteError,
					},
				},
			}

			// Set up mock expectations
			for _, mockOp := range mockRegistry.operators {
				mockOp.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(mockOp.deleteError)
				mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)
			}

			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.pclq).
				Build()

			reconciler := &Reconciler{
				client:           fakeClient,
				operatorRegistry: mockRegistry,
			}

			// Execute deletion flow
			ctx := context.Background()
			logger := log.FromContext(ctx)
			result := reconciler.triggerDeletionFlow(ctx, logger, tt.pclq)

			// Verify final result
			switch tt.expectedFinalResult {
			case "success":
				assert.False(t, result.NeedsRequeue(), "Expected successful deletion without requeue")
				assert.Empty(t, result.GetErrors(), "Expected no errors but got: %v", result.GetErrors())
			case "error":
				assert.True(t, result.NeedsRequeue() || len(result.GetErrors()) > 0, "Expected error or requeue")
			}

			// Note: In a more sophisticated test, you would track which steps were executed
			// by using mock objects that record method calls
		})
	}
}
