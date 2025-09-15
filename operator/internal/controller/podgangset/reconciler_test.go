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
	"testing"

	"github.com/NVIDIA/grove/operator/api/common/constants"
	configv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TestNewReconciler validates that NewReconciler creates a properly configured reconciler instance.
func TestNewReconciler(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// Controller configuration to use for the test
		config configv1alpha1.PodGangSetControllerConfiguration
		// Expected behavior validation function
		validate func(t *testing.T, reconciler *Reconciler)
	}{
		{
			// Standard configuration with default concurrent syncs
			name: "creates reconciler with default config",
			config: configv1alpha1.PodGangSetControllerConfiguration{
				ConcurrentSyncs: func() *int { i := 1; return &i }(),
			},
			validate: func(t *testing.T, reconciler *Reconciler) {
				assert.NotNil(t, reconciler.client)
				assert.NotNil(t, reconciler.reconcileStatusRecorder)
				assert.NotNil(t, reconciler.operatorRegistry)
				assert.Equal(t, 1, *reconciler.config.ConcurrentSyncs)
			},
		},
		{
			// Configuration with custom concurrent syncs value
			name: "creates reconciler with custom concurrent syncs",
			config: configv1alpha1.PodGangSetControllerConfiguration{
				ConcurrentSyncs: func() *int { i := 5; return &i }(),
			},
			validate: func(t *testing.T, reconciler *Reconciler) {
				assert.Equal(t, 5, *reconciler.config.ConcurrentSyncs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a fake manager for testing
			fakeClient := testutils.SetupFakeClient()
			mgr := &mockManager{
				client:        fakeClient,
				eventRecorder: &record.FakeRecorder{},
			}

			reconciler := NewReconciler(mgr, tt.config)

			require.NotNil(t, reconciler)
			tt.validate(t, reconciler)
		})
	}
}

// TestReconcile validates the main reconciliation loop behavior for various PodGangSet states.
func TestReconcile(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodGangSet setup function that creates the test resource
		setupPGS func() *grovecorev1alpha1.PodGangSet
		// Additional objects to include in the fake client
		existingObjects []client.Object
		// Expected reconcile result
		expectedResult ctrl.Result
		// Whether an error is expected
		expectError bool
		// Validation function to check the final state
		validate func(t *testing.T, pgs *grovecorev1alpha1.PodGangSet, client client.Client)
	}{
		{
			// PodGangSet not found should return no requeue
			name: "resource not found returns no requeue",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return nil // No PGS created
			},
			expectedResult: ctrl.Result{},
			expectError:    false,
		},
		{
			// Normal reconciliation of healthy PodGangSet
			name: "successful reconcile of healthy PodGangSet",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
			},
			expectedResult: ctrl.Result{},
			expectError:    false,
			validate: func(t *testing.T, pgs *grovecorev1alpha1.PodGangSet, client client.Client) {
				// Verify finalizer was added
				assert.True(t, controllerutil.ContainsFinalizer(pgs, constants.FinalizerPodGangSet))
				// Verify status was updated
				assert.NotNil(t, pgs.Status.ObservedGeneration)
				assert.Equal(t, pgs.Generation, *pgs.Status.ObservedGeneration)
			},
		},
		{
			// PodGangSet marked for deletion should trigger deletion flow
			name: "deletion triggers deletion flow",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
				// Add finalizer and deletion timestamp
				controllerutil.AddFinalizer(pgs, constants.FinalizerPodGangSet)
				now := metav1.Now()
				pgs.DeletionTimestamp = &now
				return pgs
			},
			expectedResult: ctrl.Result{},
			expectError:    false,
			validate: func(t *testing.T, pgs *grovecorev1alpha1.PodGangSet, client client.Client) {
				// Verify finalizer was removed (deletion completed)
				assert.False(t, controllerutil.ContainsFinalizer(pgs, constants.FinalizerPodGangSet))
			},
		},
		{
			// PodGangSet already deleted (not found) should not requeue
			name: "already deleted PodGangSet does not requeue",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				// Return nil - the PodGangSet doesn't exist (already deleted)
				return nil
			},
			expectedResult: ctrl.Result{},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup fake client with test objects
			var objects []client.Object
			if tt.setupPGS() != nil {
				objects = append(objects, tt.setupPGS())
			}
			objects = append(objects, tt.existingObjects...)
			fakeClient := testutils.SetupFakeClient(objects...)

			// Create reconciler with mock components
			mockStatusRecorder := &mockReconcileStatusRecorder{}
			mockOpRegistry := &mockOperatorRegistry{}

			// Set up mock expectations based on test scenario
			pgs := tt.setupPGS()
			if pgs != nil {
				if pgs.DeletionTimestamp != nil {
					// Deletion flow expectations
					mockStatusRecorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete).Return(nil)
					mockStatusRecorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete, mock.Anything).Return(nil)
					mockOpRegistry.On("GetAllOperators").Return(map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{})
				} else {
					// Normal reconcile flow expectations
					mockStatusRecorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile).Return(nil)
					mockStatusRecorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeReconcile, mock.Anything).Return(nil)

					// Create mock operator and set up its expectations
					mockOp := &mockOperator{}
					mockOp.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(nil)
					mockOpRegistry.On("GetOperator", mock.Anything).Return(mockOp, nil)
				}
			}

			reconciler := &Reconciler{
				client:                  fakeClient,
				reconcileStatusRecorder: mockStatusRecorder,
				operatorRegistry:        mockOpRegistry,
			}

			// Execute reconcile
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-pgs",
					Namespace: "test-namespace",
				},
			}

			result, err := reconciler.Reconcile(context.Background(), req)

			// Validate results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedResult, result)

			// Run additional validations if provided
			if tt.validate != nil && tt.setupPGS() != nil {
				pgs := &grovecorev1alpha1.PodGangSet{}
				err := fakeClient.Get(context.Background(), req.NamespacedName, pgs)
				if err == nil {
					tt.validate(t, pgs, fakeClient)
				}
			}
		})
	}
}

// TestReconcileDelete validates the deletion reconciliation logic for various deletion scenarios.
func TestReconcileDelete(t *testing.T) {
	tests := []struct {
		// Test case name describing the scenario being tested
		name string
		// PodGangSet setup function that creates the test resource
		setupPGS func() *grovecorev1alpha1.PodGangSet
		// Expected reconcile step result
		expectedResult ctrlcommon.ReconcileStepResult
		// Validation function to check behavior
		validate func(t *testing.T, result ctrlcommon.ReconcileStepResult)
	}{
		{
			// PodGangSet not marked for deletion should continue reconcile
			name: "not marked for deletion continues reconcile",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				return testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.False(t, ctrlcommon.ShortCircuitReconcileFlow(result))
			},
		},
		{
			// PodGangSet marked for deletion without finalizer should not requeue
			name: "marked for deletion without finalizer does not requeue",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
				// Add finalizer first to make it valid for fake client
				controllerutil.AddFinalizer(pgs, constants.FinalizerPodGangSet)
				now := metav1.Now()
				pgs.DeletionTimestamp = &now
				return pgs
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.NeedsRequeue())
			},
		},
		{
			// PodGangSet marked for deletion with finalizer should trigger deletion
			name: "marked for deletion with finalizer triggers deletion",
			setupPGS: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "test-namespace", types.UID("test-uid")).
					WithReplicas(1).
					Build()
				controllerutil.AddFinalizer(pgs, constants.FinalizerPodGangSet)
				now := metav1.Now()
				pgs.DeletionTimestamp = &now
				return pgs
			},
			validate: func(t *testing.T, result ctrlcommon.ReconcileStepResult) {
				assert.True(t, ctrlcommon.ShortCircuitReconcileFlow(result))
				assert.False(t, result.NeedsRequeue())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pgs := tt.setupPGS()
			fakeClient := testutils.SetupFakeClient(pgs)

			// Special handling for the "without finalizer" test case
			if tt.name == "marked for deletion without finalizer does not requeue" && pgs != nil {
				// Remove the finalizer after creating the fake client to simulate the scenario
				controllerutil.RemoveFinalizer(pgs, constants.FinalizerPodGangSet)
			}

			// Set up mock expectations
			mockStatusRecorder := &mockReconcileStatusRecorder{}
			mockOpRegistry := &mockOperatorRegistry{}

			// Set up expectations for deletion flow if the test will trigger it
			if pgs != nil && !pgs.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(pgs, constants.FinalizerPodGangSet) {
				mockStatusRecorder.On("RecordStart", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete).Return(nil)
				mockStatusRecorder.On("RecordCompletion", mock.Anything, mock.Anything, grovecorev1alpha1.LastOperationTypeDelete, mock.Anything).Return(nil)
				mockOpRegistry.On("GetAllOperators").Return(map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet]{})
			}

			reconciler := &Reconciler{
				client:                  fakeClient,
				reconcileStatusRecorder: mockStatusRecorder,
				operatorRegistry:        mockOpRegistry,
			}

			result := reconciler.reconcileDelete(
				context.Background(),
				logr.Discard(),
				pgs,
			)

			tt.validate(t, result)
		})
	}
}
