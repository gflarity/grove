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
	"time"

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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestDeletePodCliqueResources tests the deletion of PodClique managed resources
func TestDeletePodCliqueResources(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object being deleted
		pclq *grovecorev1alpha1.PodClique
		// setupMocks configures the mock operator registry for the test
		setupMocks func(*mockOperatorRegistry)
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Test successful deletion of all managed resources
			name: "successful_deletion_all_resources",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// Test deletion with one operator failing
			name: "deletion_with_operator_failure",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("failed to delete pod"))

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: false,
			expectedError:    true,
		},
		{
			// Test deletion with multiple operators, some failing
			name: "deletion_with_mixed_results",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockServiceOperator := &mockOperator{}
				mockServiceOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("service deletion failed"))

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod:            mockPodOperator,
					component.KindServiceAccount: mockServiceOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: false,
			expectedError:    true,
		},
		{
			// Test deletion with no registered operators
			name: "deletion_with_no_operators",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// Test deletion with all available component kinds
			name: "deletion_with_all_component_kinds",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				// Create mock operators for all component kinds
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockServiceAccountOperator := &mockOperator{}
				mockServiceAccountOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockRoleOperator := &mockOperator{}
				mockRoleOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockRoleBindingOperator := &mockOperator{}
				mockRoleBindingOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockTokenSecretOperator := &mockOperator{}
				mockTokenSecretOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockHeadlessServiceOperator := &mockOperator{}
				mockHeadlessServiceOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockHPAOperator := &mockOperator{}
				mockHPAOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockPodCliqueScalingGroupOperator := &mockOperator{}
				mockPodCliqueScalingGroupOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockPodGangOperator := &mockOperator{}
				mockPodGangOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod:                       mockPodOperator,
					component.KindServiceAccount:            mockServiceAccountOperator,
					component.KindRole:                      mockRoleOperator,
					component.KindRoleBinding:               mockRoleBindingOperator,
					component.KindServiceAccountTokenSecret: mockTokenSecretOperator,
					component.KindHeadlessService:           mockHeadlessServiceOperator,
					component.KindHorizontalPodAutoscaler:   mockHPAOperator,
					component.KindPodCliqueScalingGroup:     mockPodCliqueScalingGroupOperator,
					component.KindPodGang:                   mockPodGangOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// Test deletion with all component kinds where some fail
			name: "deletion_with_all_kinds_mixed_results",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				// Create mock operators - some succeed, some fail
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockServiceAccountOperator := &mockOperator{}
				mockServiceAccountOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("service account deletion failed"))

				mockRoleOperator := &mockOperator{}
				mockRoleOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockRoleBindingOperator := &mockOperator{}
				mockRoleBindingOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("role binding deletion failed"))

				mockTokenSecretOperator := &mockOperator{}
				mockTokenSecretOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod:                       mockPodOperator,
					component.KindServiceAccount:            mockServiceAccountOperator,
					component.KindRole:                      mockRoleOperator,
					component.KindRoleBinding:               mockRoleBindingOperator,
					component.KindServiceAccountTokenSecret: mockTokenSecretOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
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
			result := reconciler.deletePodCliqueResources(ctx, logger, tt.pclq)

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

// TestVerifyNoResourcesAwaitsCleanup tests the verification of resource cleanup
func TestVerifyNoResourcesAwaitsCleanup(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object being verified
		pclq *grovecorev1alpha1.PodClique
		// setupMocks configures the mock operator registry for the test
		setupMocks func(*mockOperatorRegistry)
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
		// expectedRequeue indicates whether the operation should requeue
		expectedRequeue bool
	}{
		{
			// Test successful verification with no resources awaiting cleanup
			name: "no_resources_awaiting_cleanup",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: true,
			expectedError:    false,
			expectedRequeue:  false,
		},
		{
			// Test when resources are still awaiting cleanup (should requeue)
			name: "resources_still_awaiting_cleanup",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{"test-pod-1", "test-pod-2"}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: false,
			expectedError:    false,
			expectedRequeue:  true,
		},
		{
			// Test operator error during resource name retrieval
			name: "operator_error_getting_resource_names",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, errors.New("failed to get resource names"))

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: false,
			expectedError:    true,
			expectedRequeue:  false,
		},
		{
			// Test with multiple operators having mixed results
			name: "mixed_operator_results",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				mockServiceOperator := &mockOperator{}
				mockServiceOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{"test-service"}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod:            mockPodOperator,
					component.KindServiceAccount: mockServiceOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: false,
			expectedError:    false,
			expectedRequeue:  true,
		},
		{
			// Test with empty operator registry
			name: "empty_operator_registry",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedContinue: true,
			expectedError:    false,
			expectedRequeue:  false,
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

			// Create mocks
			mockRegistry := &mockOperatorRegistry{}
			tt.setupMocks(mockRegistry)

			// Create reconciler
			reconciler := &Reconciler{
				client:           fakeClient,
				operatorRegistry: mockRegistry,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test
			result := reconciler.verifyNoResourcesAwaitsCleanup(ctx, logger, tt.pclq)

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

			if tt.expectedRequeue {
				assert.True(t, result.NeedsRequeue(), "Expected requeue but got none")
			}

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
		})
	}
}

// TestRemoveFinalizer tests the finalizer removal functionality
func TestRemoveFinalizer(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object with or without finalizer
		pclq *grovecorev1alpha1.PodClique
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Test successful finalizer removal
			name: "successful_finalizer_removal",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{grovecorev1alpha1.FinalizerPodClique},
				},
			},
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// Test when finalizer is not present
			name: "finalizer_not_present",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					// No finalizers
				},
			},
			expectedContinue: false,
			expectedError:    false,
		},
		{
			// Test with multiple finalizers including the PodClique finalizer
			name: "multiple_finalizers_with_podclique",
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
			expectedContinue: true,
			expectedError:    false,
		},
		{
			// Test with multiple finalizers but no PodClique finalizer
			name: "multiple_finalizers_without_podclique",
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
			expectedContinue: false,
			expectedError:    false,
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
			result := reconciler.removeFinalizer(ctx, logger, tt.pclq)

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

// TestTriggerDeletionFlowIntegration tests the complete deletion flow integration
func TestTriggerDeletionFlowIntegration(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object being deleted
		pclq *grovecorev1alpha1.PodClique
		// setupMocks configures the mock operator registry for the test
		setupMocks func(*mockOperatorRegistry)
		// expectedFinalResult indicates the expected final result type
		expectedFinalResult string // "success", "error", "no_requeue"
	}{
		{
			// Test complete successful deletion flow
			name: "complete_successful_deletion",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{grovecorev1alpha1.FinalizerPodClique},
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedFinalResult: "success",
		},
		{
			// Test deletion flow with resource deletion failure
			name: "deletion_with_resource_failure",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{grovecorev1alpha1.FinalizerPodClique},
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("resource deletion failed"))
				// Note: GetExistingResourceNames won't be called because deletion fails first

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedFinalResult: "error",
		},
		{
			// Test deletion flow with verification failure (resources still awaiting cleanup)
			name: "deletion_with_verification_requeue",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{grovecorev1alpha1.FinalizerPodClique},
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				// Deletion succeeds
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				// But verification shows resources still exist (should requeue)
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{"remaining-pod"}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedFinalResult: "no_requeue", // Should requeue, not error
		},
		{
			// Test deletion flow with no finalizer present (should short-circuit early)
			name: "deletion_with_no_finalizer",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					// No finalizers
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodOperator := &mockOperator{}
				// Deletion should succeed
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				// Verification should succeed
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod: mockPodOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedFinalResult: "no_requeue", // Should short-circuit when no finalizer
		},
		{
			// Test complete successful deletion flow with all component kinds
			name: "complete_successful_deletion_all_kinds",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pclq",
					Namespace:  "default",
					Finalizers: []string{grovecorev1alpha1.FinalizerPodClique},
				},
			},
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				// Create mock operators for multiple component kinds
				mockPodOperator := &mockOperator{}
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				mockServiceAccountOperator := &mockOperator{}
				mockServiceAccountOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockServiceAccountOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				mockRoleOperator := &mockOperator{}
				mockRoleOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockRoleOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

				operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
					component.KindPod:            mockPodOperator,
					component.KindServiceAccount: mockServiceAccountOperator,
					component.KindRole:           mockRoleOperator,
				}
				mockRegistry.On("GetAllOperators").Return(operators)
			},
			expectedFinalResult: "success",
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

			// Create mocks
			mockRegistry := &mockOperatorRegistry{}
			tt.setupMocks(mockRegistry)

			// Create reconciler
			reconciler := &Reconciler{
				client:           fakeClient,
				operatorRegistry: mockRegistry,
			}

			// Set up context and logger
			ctx := context.Background()
			logger := logr.Discard()

			// Execute test
			result := reconciler.triggerDeletionFlow(ctx, logger, tt.pclq)

			// Verify results based on expected outcome
			switch tt.expectedFinalResult {
			case "success":
				assert.False(t, result.HasErrors(), "Expected successful deletion")
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected flow to short-circuit on success")
			case "error":
				assert.True(t, result.HasErrors(), "Expected error during deletion")
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected flow to short-circuit on error")
			case "no_requeue":
				assert.False(t, result.HasErrors(), "Expected no error")
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result), "Expected flow to short-circuit")
			}

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
		})
	}
}

// TestDeletePodCliqueResourcesWithContextCancellation tests deletion behavior when context is cancelled
func TestDeletePodCliqueResourcesWithContextCancellation(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object being deleted
		pclq *grovecorev1alpha1.PodClique
		// setupMocks configures the mock operator registry for the test
		setupMocks func(*mockOperator)
		// cancelAfter specifies when to cancel the context
		cancelAfter time.Duration
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Test context cancellation during resource deletion
			name: "context_cancelled_during_deletion",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockPodOperator *mockOperator) {
				// Mock operator that blocks until context is cancelled
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(func(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) error {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(100 * time.Millisecond):
						return nil
					}
				})
			},
			cancelAfter:   50 * time.Millisecond,
			expectedError: true,
		},
		{
			// Test successful deletion when context is not cancelled
			name: "context_not_cancelled_successful_deletion",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockPodOperator *mockOperator) {
				mockPodOperator.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			cancelAfter:   200 * time.Millisecond, // Cancel after operation completes
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mocks
			mockPodOperator := &mockOperator{}
			tt.setupMocks(mockPodOperator)

			mockRegistry := &mockOperatorRegistry{}
			operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
				component.KindPod: mockPodOperator,
			}
			mockRegistry.On("GetAllOperators").Return(operators)

			// Create reconciler
			reconciler := &Reconciler{
				operatorRegistry: mockRegistry,
			}

			// Set up context with cancellation
			ctx, cancel := context.WithCancel(context.Background())
			logger := logr.Discard()

			// Cancel context after specified duration
			go func() {
				time.Sleep(tt.cancelAfter)
				cancel()
			}()

			// Execute test
			result := reconciler.deletePodCliqueResources(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedError {
				assert.True(t, result.HasErrors(), "Expected error but got none")
			} else {
				assert.False(t, result.HasErrors(), "Expected no error but got one")
			}

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
			mockPodOperator.AssertExpectations(t)
		})
	}
}

// TestVerifyNoResourcesAwaitsCleanupWithContextCancellation tests verification behavior when context is cancelled
func TestVerifyNoResourcesAwaitsCleanupWithContextCancellation(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// pclq is the PodClique object being verified
		pclq *grovecorev1alpha1.PodClique
		// setupMocks configures the mock operator registry for the test
		setupMocks func(*mockOperator)
		// cancelAfter specifies when to cancel the context
		cancelAfter time.Duration
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Test context cancellation during resource verification
			name: "context_cancelled_during_verification",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			setupMocks: func(mockPodOperator *mockOperator) {
				// Mock operator that returns context cancelled error
				mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, context.Canceled)
			},
			cancelAfter:   50 * time.Millisecond,
			expectedError: true,
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

			// Create mocks
			mockPodOperator := &mockOperator{}
			tt.setupMocks(mockPodOperator)

			mockRegistry := &mockOperatorRegistry{}
			operators := map[component.Kind]component.Operator[grovecorev1alpha1.PodClique]{
				component.KindPod: mockPodOperator,
			}
			mockRegistry.On("GetAllOperators").Return(operators)

			// Create reconciler
			reconciler := &Reconciler{
				client:           fakeClient,
				operatorRegistry: mockRegistry,
			}

			// Set up context with cancellation
			ctx, cancel := context.WithCancel(context.Background())
			logger := logr.Discard()

			// Cancel context after specified duration
			go func() {
				time.Sleep(tt.cancelAfter)
				cancel()
			}()

			// Execute test
			result := reconciler.verifyNoResourcesAwaitsCleanup(ctx, logger, tt.pclq)

			// Verify results
			if tt.expectedError {
				assert.True(t, result.HasErrors(), "Expected error but got none")
			} else {
				assert.False(t, result.HasErrors(), "Expected no error but got one")
			}

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
			mockPodOperator.AssertExpectations(t)
		})
	}
}
