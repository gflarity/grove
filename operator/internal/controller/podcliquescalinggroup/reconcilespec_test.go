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
	"errors"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ============================================================================
// Test helper functions
// ============================================================================

// createTestPCSG creates a test PodCliqueScalingGroup with common defaults
func createTestPCSG(name, namespace string, withFinalizer bool) *grovecorev1alpha1.PodCliqueScalingGroup {
	pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
		},
		Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
			Replicas: 2,
		},
	}

	if withFinalizer {
		controllerutil.AddFinalizer(pcsg, grovecorev1alpha1.FinalizerPodCliqueScalingGroup)
	}

	return pcsg
}

// ============================================================================
// Unit Tests for individual reconcile step functions
// ============================================================================

// TestEnsureFinalizer tests the ensureFinalizer function which adds the
// PodCliqueScalingGroup finalizer if it's not already present.
func TestEnsureFinalizer(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// pcsg is the PodCliqueScalingGroup resource, with or without finalizer
		pcsg *grovecorev1alpha1.PodCliqueScalingGroup
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
		// expectedFinalizerAdded indicates whether the finalizer should be added
		expectedFinalizerAdded bool
	}{
		{
			// Test adding finalizer to resource without one
			name:                   "add_finalizer_to_resource_without_finalizer",
			pcsg:                   createTestPCSG("test-pcsg", "default", false),
			expectedContinue:       true,
			expectedError:          false,
			expectedFinalizerAdded: true,
		},
		{
			// Test resource that already has the finalizer
			name:                   "resource_already_has_finalizer",
			pcsg:                   createTestPCSG("test-pcsg", "default", true),
			expectedContinue:       true,
			expectedError:          false,
			expectedFinalizerAdded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := testutils.SetupTestLogger()

			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.pcsg).
				Build()

			// Create reconciler
			reconciler := &Reconciler{client: fakeClient}

			// Track initial finalizer state
			initialHasFinalizer := controllerutil.ContainsFinalizer(tt.pcsg, grovecorev1alpha1.FinalizerPodCliqueScalingGroup)

			// Execute the test
			result := reconciler.ensureFinalizer(ctx, logger, tt.pcsg)

			// Verify results
			assert.Equal(t, tt.expectedContinue, !ctrlcommon.ShortCircuitReconcileFlow(result), "unexpected continue behavior")
			assert.Equal(t, tt.expectedError, result.HasErrors(), "unexpected error behavior")

			// Verify finalizer state
			currentHasFinalizer := controllerutil.ContainsFinalizer(tt.pcsg, grovecorev1alpha1.FinalizerPodCliqueScalingGroup)
			if tt.expectedFinalizerAdded {
				assert.False(t, initialHasFinalizer, "resource should not have had finalizer initially")
				assert.True(t, currentHasFinalizer, "finalizer should have been added")
			} else {
				assert.True(t, initialHasFinalizer, "resource should have had finalizer initially")
				assert.True(t, currentHasFinalizer, "finalizer should still be present")
			}
		})
	}
}

// TestSyncPodCliqueScalingGroupResources tests the syncPodCliqueScalingGroupResources function
// which synchronizes all managed resources for the PodCliqueScalingGroup.
func TestSyncPodCliqueScalingGroupResources(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// pcsg is the PodCliqueScalingGroup resource being reconciled
		pcsg *grovecorev1alpha1.PodCliqueScalingGroup
		// setupMocks configures the mock OperatorRegistry and operators
		setupMocks func(*mockOperatorRegistry)
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
		// expectedRequeue indicates whether the operation should be requeued
		expectedRequeue bool
	}{
		{
			// Test successful synchronization of all resources
			name: "successful_sync_all_resources",
			pcsg: createTestPCSG("test-pcsg", "default", true),
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodCliqueOperator := &mockOperator{}
				mockPodCliqueOperator.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockRegistry.On("GetOperator", component.KindPodClique).Return(mockPodCliqueOperator, nil)
			},
			expectedContinue: true,
			expectedError:    false,
			expectedRequeue:  false,
		},
		{
			// Test failure to get operator from registry
			name: "failure_getting_operator",
			pcsg: createTestPCSG("test-pcsg", "default", true),
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				// Use a typed nil to avoid interface conversion panic
				var nilOperator component.Operator[grovecorev1alpha1.PodCliqueScalingGroup]
				mockRegistry.On("GetOperator", component.KindPodClique).Return(nilOperator, errors.New("operator not found"))
			},
			expectedContinue: false,
			expectedError:    true,
			expectedRequeue:  true,
		},
		{
			// Test operator sync failure with permanent error
			name: "operator_sync_permanent_failure",
			pcsg: createTestPCSG("test-pcsg", "default", true),
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodCliqueOperator := &mockOperator{}
				mockPodCliqueOperator.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("permanent sync failure"))
				mockRegistry.On("GetOperator", component.KindPodClique).Return(mockPodCliqueOperator, nil)
			},
			expectedContinue: false,
			expectedError:    true,
			expectedRequeue:  true,
		},
		{
			// Test operator sync failure with transient error (should requeue after interval)
			name: "operator_sync_transient_failure",
			pcsg: createTestPCSG("test-pcsg", "default", true),
			setupMocks: func(mockRegistry *mockOperatorRegistry) {
				mockPodCliqueOperator := &mockOperator{}
				transientErr := groveerr.WrapError(errors.New("temporary failure"), groveerr.ErrCodeRequeueAfter, "sync", "transient error")
				mockPodCliqueOperator.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(transientErr)
				mockRegistry.On("GetOperator", component.KindPodClique).Return(mockPodCliqueOperator, nil)
			},
			expectedContinue: false,
			expectedError:    false,
			expectedRequeue:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := testutils.SetupTestLogger()

			// Create mocks
			mockRegistry := &mockOperatorRegistry{}
			tt.setupMocks(mockRegistry)

			// Create reconciler
			reconciler := &Reconciler{operatorRegistry: mockRegistry}

			// Execute the test
			result := reconciler.syncPodCliqueScalingGroupResources(ctx, logger, tt.pcsg)

			// Verify results
			assert.Equal(t, tt.expectedContinue, !ctrlcommon.ShortCircuitReconcileFlow(result), "unexpected continue behavior")
			assert.Equal(t, tt.expectedError, result.HasErrors(), "unexpected error behavior")
			assert.Equal(t, tt.expectedRequeue, result.NeedsRequeue(), "unexpected requeue behavior")

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
		})
	}
}

// TestUpdateObservedGeneration tests the updateObservedGeneration function which
// updates the status.ObservedGeneration field to match the current resource generation.
func TestUpdateObservedGeneration(t *testing.T) {
	tests := []struct {
		// name describes the test scenario
		name string
		// pcsg is the PodCliqueScalingGroup resource with specific generation
		pcsg *grovecorev1alpha1.PodCliqueScalingGroup
		// expectedContinue indicates whether the reconcile flow should continue
		expectedContinue bool
		// expectedError indicates whether an error should be returned
		expectedError bool
		// expectedObservedGeneration is the expected value of status.ObservedGeneration
		expectedObservedGeneration int64
	}{
		{
			// Test successful update of observed generation
			name: "successful_update_observed_generation",
			pcsg: func() *grovecorev1alpha1.PodCliqueScalingGroup {
				pcsg := createTestPCSG("test-pcsg", "default", true)
				pcsg.Generation = 5
				return pcsg
			}(),
			expectedContinue:           true,
			expectedError:              false,
			expectedObservedGeneration: 5,
		},
		{
			// Test update with generation zero
			name: "update_with_zero_generation",
			pcsg: func() *grovecorev1alpha1.PodCliqueScalingGroup {
				pcsg := createTestPCSG("test-pcsg", "default", true)
				pcsg.Generation = 0
				return pcsg
			}(),
			expectedContinue:           true,
			expectedError:              false,
			expectedObservedGeneration: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := testutils.SetupTestLogger()

			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.pcsg).
				WithStatusSubresource(tt.pcsg).
				Build()

			// Create reconciler
			reconciler := &Reconciler{client: fakeClient}

			// Execute the test
			result := reconciler.updateObservedGeneration(ctx, logger, tt.pcsg)

			// Verify results
			assert.Equal(t, tt.expectedContinue, !ctrlcommon.ShortCircuitReconcileFlow(result), "unexpected continue behavior")
			assert.Equal(t, tt.expectedError, result.HasErrors(), "unexpected error behavior")

			// Verify observed generation was updated
			if !tt.expectedError {
				require.NotNil(t, tt.pcsg.Status.ObservedGeneration, "ObservedGeneration should be set")
				assert.Equal(t, tt.expectedObservedGeneration, *tt.pcsg.Status.ObservedGeneration, "unexpected ObservedGeneration value")
			}
		})
	}
}

// ============================================================================
// Unit Tests for helper functions
// ============================================================================

// TestGetOrderedKindsForSync tests the getOrderedKindsForSync function which
// returns the resource kinds that need to be synchronized in dependency order.
func TestGetOrderedKindsForSync(t *testing.T) {
	// Test that the function returns the expected kinds in the correct order
	kinds := getOrderedKindsForSync()

	// Verify the expected kinds are present
	expectedKinds := []component.Kind{
		component.KindPodClique,
	}

	assert.Equal(t, expectedKinds, kinds, "unexpected kinds or order returned")
	assert.NotEmpty(t, kinds, "should return at least one kind")

	// Verify all returned kinds are valid
	for _, kind := range kinds {
		assert.NotEmpty(t, string(kind), "kind should not be empty")
	}
}
