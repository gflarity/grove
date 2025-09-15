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

package podcliquescalinggroup

import (
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"

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
// correctly initializes and configures an OperatorRegistry for PodCliqueScalingGroup resources.
// It ensures that the registry is properly created and contains the expected PodClique operator.
func TestCreateOperatorRegistry(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// setupFunc prepares the test environment and returns necessary dependencies.
		// It creates a manager and event recorder for the test case.
		setupFunc func(t *testing.T) (manager.Manager, record.EventRecorder)
		// validateFunc performs assertions on the returned registry to ensure
		// it meets the expected behavior and contains the correct operators.
		validateFunc func(t *testing.T, registry component.OperatorRegistry[grovecorev1alpha1.PodCliqueScalingGroup])
	}{
		{
			// Tests that CreateOperatorRegistry successfully creates a registry with the required PodClique operator.
			// The registry should be non-nil and contain a PodClique operator registered under KindPodClique.
			name: "successful_registry_creation",
			setupFunc: func(t *testing.T) (manager.Manager, record.EventRecorder) {
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

				eventRecorder := record.NewFakeRecorder(100)
				return mgr, eventRecorder
			},
			validateFunc: func(t *testing.T, registry component.OperatorRegistry[grovecorev1alpha1.PodCliqueScalingGroup]) {
				// Registry should not be nil
				require.NotNil(t, registry)

				// Should be able to retrieve the PodClique operator
				podCliqueOperator, err := registry.GetOperator(component.KindPodClique)
				assert.NoError(t, err)
				assert.NotNil(t, podCliqueOperator)

				// Should return an error for non-registered operators
				_, err = registry.GetOperator(component.KindPod)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "operator for kind Pod not found")

				// GetAllOperators should return exactly one operator
				allOperators := registry.GetAllOperators()
				assert.Len(t, allOperators, 1)
				assert.Contains(t, allOperators, component.KindPodClique)
			},
		},
		{
			// Tests that the registry correctly handles multiple calls and maintains state.
			// Verifies that the same operator instance is returned on subsequent calls.
			name: "registry_consistency",
			setupFunc: func(t *testing.T) (manager.Manager, record.EventRecorder) {
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
				return mgr, eventRecorder
			},
			validateFunc: func(t *testing.T, registry component.OperatorRegistry[grovecorev1alpha1.PodCliqueScalingGroup]) {
				// Get the operator twice and verify it's the same instance
				operator1, err1 := registry.GetOperator(component.KindPodClique)
				operator2, err2 := registry.GetOperator(component.KindPodClique)

				assert.NoError(t, err1)
				assert.NoError(t, err2)
				assert.Same(t, operator1, operator2, "Registry should return the same operator instance on multiple calls")

				// Verify the operator is properly configured
				assert.NotNil(t, operator1)
			},
		},
		{
			// Tests that CreateOperatorRegistry works with different manager configurations.
			// Ensures the function is robust to various manager setups.
			name: "different_manager_configuration",
			setupFunc: func(t *testing.T) (manager.Manager, record.EventRecorder) {
				// Create a scheme with additional types to test robustness
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

				// Use a different event recorder configuration
				eventRecorder := record.NewFakeRecorder(50)
				return mgr, eventRecorder
			},
			validateFunc: func(t *testing.T, registry component.OperatorRegistry[grovecorev1alpha1.PodCliqueScalingGroup]) {
				// Basic validation that registry is functional
				require.NotNil(t, registry)

				// Verify the PodClique operator is registered and accessible
				operator, err := registry.GetOperator(component.KindPodClique)
				assert.NoError(t, err)
				assert.NotNil(t, operator)

				// Verify registry structure
				allOperators := registry.GetAllOperators()
				assert.Len(t, allOperators, 1)
				assert.Equal(t, operator, allOperators[component.KindPodClique])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test dependencies
			mgr, eventRecorder := tt.setupFunc(t)

			// Execute the function under test
			registry := CreateOperatorRegistry(mgr, eventRecorder)

			// Validate the results
			tt.validateFunc(t, registry)
		})
	}
}

// TestCreateOperatorRegistryNilInputs tests the behavior of CreateOperatorRegistry
// when provided with nil inputs. This ensures the function handles edge cases gracefully.
func TestCreateOperatorRegistryNilInputs(t *testing.T) {
	tests := []struct {
		// name describes the specific nil input scenario being tested
		name string
		// mgr is the manager instance to pass to CreateOperatorRegistry (may be nil)
		mgr manager.Manager
		// eventRecorder is the event recorder to pass to CreateOperatorRegistry (may be nil)
		eventRecorder record.EventRecorder
		// expectPanic indicates whether the test expects a panic to occur
		expectPanic bool
		// panicMessage is the expected panic message if expectPanic is true
		panicMessage string
	}{
		{
			// Tests behavior when manager is nil.
			// Should panic as the function tries to access mgr.GetClient() and mgr.GetScheme().
			name:          "nil_manager",
			mgr:           nil,
			eventRecorder: record.NewFakeRecorder(10),
			expectPanic:   true,
			panicMessage:  "runtime error",
		},
		{
			// Tests behavior when event recorder is nil.
			// Should not panic as the event recorder is passed directly to the operator constructor.
			name: "nil_event_recorder",
			mgr: func() manager.Manager {
				scheme := runtime.NewScheme()
				_ = grovecorev1alpha1.AddToScheme(scheme)
				mgr, _ := manager.New(&rest.Config{}, manager.Options{
					Scheme: scheme,
					Metrics: metricsserver.Options{
						BindAddress: "0",
					},
					WebhookServer: webhook.NewServer(webhook.Options{
						Port: -1,
					}),
				})
				return mgr
			}(),
			eventRecorder: nil,
			expectPanic:   false,
		},
		{
			// Tests behavior when both manager and event recorder are nil.
			// Should panic due to nil manager access.
			name:          "both_nil",
			mgr:           nil,
			eventRecorder: nil,
			expectPanic:   true,
			panicMessage:  "runtime error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				// Test that the function panics as expected
				assert.Panics(t, func() {
					CreateOperatorRegistry(tt.mgr, tt.eventRecorder)
				}, "CreateOperatorRegistry should panic with nil manager")
			} else {
				// Test that the function doesn't panic and returns a valid registry
				assert.NotPanics(t, func() {
					registry := CreateOperatorRegistry(tt.mgr, tt.eventRecorder)
					assert.NotNil(t, registry)
				}, "CreateOperatorRegistry should not panic with nil event recorder")
			}
		})
	}
}
