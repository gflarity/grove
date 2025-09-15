/*
Copyright 2025 The Grove Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podclique

import (
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	"github.com/NVIDIA/grove/operator/internal/expect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// TestCreateOperatorRegistry validates that the CreateOperatorRegistry function
// correctly initializes and configures an OperatorRegistry for PodClique resources.
// It ensures that all required components are properly registered and accessible.
func TestCreateOperatorRegistry(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// setupFunc prepares the test environment and returns necessary dependencies
		setupFunc func(t *testing.T) (manager.Manager, record.EventRecorder, *expect.ExpectationsStore)
		// validateFunc performs assertions on the returned registry
		validateFunc func(t *testing.T, registry component.OperatorRegistry[grovecorev1alpha1.PodClique])
	}{
		{
			// Tests that CreateOperatorRegistry successfully creates a registry with all required components.
			// Registry should be created and contain a Pod operator registered under KindPod.
			name: "successful registry creation",
			setupFunc: func(t *testing.T) (manager.Manager, record.EventRecorder, *expect.ExpectationsStore) {
				// Create a test manager with minimal configuration
				scheme := runtime.NewScheme()
				require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

				mgr, err := manager.New(&rest.Config{}, manager.Options{
					Scheme: scheme,
					Metrics: metricsserver.Options{
						BindAddress: "0", // Disable metrics server for tests
					},
					WebhookServer: webhook.NewServer(webhook.Options{
						Port: -1, // Disable webhook server for tests
					}),
				})
				require.NoError(t, err)

				// Create a fake event recorder
				eventRecorder := record.NewFakeRecorder(100)

				// Create an expectations store
				expectationsStore := expect.NewExpectationsStore()

				return mgr, eventRecorder, expectationsStore
			},
			validateFunc: func(t *testing.T, registry component.OperatorRegistry[grovecorev1alpha1.PodClique]) {
				// Verify the registry is not nil
				require.NotNil(t, registry)

				// Verify that Pod operator is registered
				podOperator, err := registry.GetOperator(component.KindPod)
				assert.NoError(t, err)
				assert.NotNil(t, podOperator)

				// Verify that we get exactly one operator registered
				allOperators := registry.GetAllOperators()
				assert.Len(t, allOperators, 1)
				assert.Contains(t, allOperators, component.KindPod)
			},
		},
		{
			// Tests that registered operators can be retrieved correctly from the registry.
			// Pod operator should be retrievable and non-Pod operators should return errors.
			name: "registry operator retrieval",
			setupFunc: func(t *testing.T) (manager.Manager, record.EventRecorder, *expect.ExpectationsStore) {
				scheme := runtime.NewScheme()
				require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

				mgr, err := manager.New(&rest.Config{}, manager.Options{
					Scheme: scheme,
					Metrics: metricsserver.Options{
						BindAddress: "0",
					},
					WebhookServer: webhook.NewServer(webhook.Options{
						Port: -1,
					}),
				})
				require.NoError(t, err)

				eventRecorder := record.NewFakeRecorder(100)
				expectationsStore := expect.NewExpectationsStore()

				return mgr, eventRecorder, expectationsStore
			},
			validateFunc: func(t *testing.T, registry component.OperatorRegistry[grovecorev1alpha1.PodClique]) {
				// Test successful retrieval of registered Pod operator
				podOperator, err := registry.GetOperator(component.KindPod)
				assert.NoError(t, err)
				assert.NotNil(t, podOperator)

				// Test that unregistered operators return errors
				unregisteredKinds := []component.Kind{
					component.KindServiceAccount,
					component.KindRole,
					component.KindRoleBinding,
					component.KindHeadlessService,
					component.KindHorizontalPodAutoscaler,
					component.KindPodCliqueScalingGroup,
					component.KindPodGang,
				}

				for _, kind := range unregisteredKinds {
					operator, err := registry.GetOperator(kind)
					assert.Error(t, err)
					assert.Nil(t, operator)
					assert.Contains(t, err.Error(), "not found")
				}
			},
		},
		{
			// Tests that the registry maintains consistent state across multiple operations.
			// Registry should consistently return the same operator instance and maintain its registration.
			name: "registry state consistency",
			setupFunc: func(t *testing.T) (manager.Manager, record.EventRecorder, *expect.ExpectationsStore) {
				scheme := runtime.NewScheme()
				require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

				mgr, err := manager.New(&rest.Config{}, manager.Options{
					Scheme: scheme,
					Metrics: metricsserver.Options{
						BindAddress: "0",
					},
					WebhookServer: webhook.NewServer(webhook.Options{
						Port: -1,
					}),
				})
				require.NoError(t, err)

				eventRecorder := record.NewFakeRecorder(100)
				expectationsStore := expect.NewExpectationsStore()

				return mgr, eventRecorder, expectationsStore
			},
			validateFunc: func(t *testing.T, registry component.OperatorRegistry[grovecorev1alpha1.PodClique]) {
				// Get the Pod operator multiple times
				operator1, err1 := registry.GetOperator(component.KindPod)
				operator2, err2 := registry.GetOperator(component.KindPod)

				// Both calls should succeed
				assert.NoError(t, err1)
				assert.NoError(t, err2)
				assert.NotNil(t, operator1)
				assert.NotNil(t, operator2)

				// Should return the same operator instance
				assert.Equal(t, operator1, operator2)

				// GetAllOperators should consistently return the same result
				allOperators1 := registry.GetAllOperators()
				allOperators2 := registry.GetAllOperators()
				assert.Equal(t, allOperators1, allOperators2)
				assert.Len(t, allOperators1, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test dependencies
			mgr, eventRecorder, expectationsStore := tt.setupFunc(t)

			// Execute the function under test
			registry := CreateOperatorRegistry(mgr, eventRecorder, expectationsStore)

			// Validate the results
			tt.validateFunc(t, registry)
		})
	}
}

// TestCreateOperatorRegistryWithNilInputs validates that CreateOperatorRegistry
// handles nil inputs gracefully and fails appropriately when required dependencies are missing.
func TestCreateOperatorRegistryWithNilInputs(t *testing.T) {
	tests := []struct {
		// name describes the specific error scenario being tested
		name string
		// mgr is the manager instance to pass (may be nil for error testing)
		mgr manager.Manager
		// eventRecorder is the event recorder to pass (may be nil for error testing)
		eventRecorder record.EventRecorder
		// expectationsStore is the expectations store to pass (may be nil for error testing)
		expectationsStore *expect.ExpectationsStore
		// shouldPanic indicates whether this test case should cause a panic
		shouldPanic bool
	}{
		{
			// Tests behavior when a nil manager is passed to CreateOperatorRegistry.
			// Should panic due to nil manager access when trying to get client and scheme.
			name:              "nil manager",
			mgr:               nil,
			eventRecorder:     record.NewFakeRecorder(100),
			expectationsStore: expect.NewExpectationsStore(),
			shouldPanic:       true,
		},
		{
			// Tests behavior when a nil event recorder is passed.
			// Should successfully create registry but Pod operator will have nil event recorder.
			name:              "nil event recorder",
			mgr:               createTestManager(t),
			eventRecorder:     nil,
			expectationsStore: expect.NewExpectationsStore(),
			shouldPanic:       false,
		},
		{
			// Tests behavior when a nil expectations store is passed.
			// Should successfully create registry but Pod operator will have nil expectations store.
			name:              "nil expectations store",
			mgr:               createTestManager(t),
			eventRecorder:     record.NewFakeRecorder(100),
			expectationsStore: nil,
			shouldPanic:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				// Test that the function panics (don't check specific message as it may vary)
				assert.Panics(t, func() {
					CreateOperatorRegistry(tt.mgr, tt.eventRecorder, tt.expectationsStore)
				})
			} else {
				// Test that the function doesn't panic and creates a registry
				assert.NotPanics(t, func() {
					registry := CreateOperatorRegistry(tt.mgr, tt.eventRecorder, tt.expectationsStore)
					assert.NotNil(t, registry)

					// Verify that Pod operator is still registered even with nil dependencies
					podOperator, err := registry.GetOperator(component.KindPod)
					assert.NoError(t, err)
					assert.NotNil(t, podOperator)
				})
			}
		})
	}
}

// createTestManager creates a minimal test manager for use in tests.
// Returns a controller manager configured with the Grove scheme and disabled servers.
func createTestManager(t *testing.T) manager.Manager {
	scheme := runtime.NewScheme()
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	mgr, err := manager.New(&rest.Config{}, manager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: -1, // Disable webhook server
		}),
	})
	require.NoError(t, err)
	return mgr
}
