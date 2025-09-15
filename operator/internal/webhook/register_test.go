// /*
// Copyright 2024 The Grove Authors.
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

package webhook

import (
	"context"
	"net/http"
	"testing"

	"github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// TestRegisterWebhooks validates the webhook registration process.
// It ensures that both defaulting and validating webhooks are properly
// registered with the controller manager without errors.
func TestRegisterWebhooks(t *testing.T) {
	testCases := []struct {
		// Test case name describing the scenario being tested
		name string
		// setupManager function to create and configure the manager for testing
		setupManager func() (manager.Manager, error)
		// expectError indicates whether the registration should fail
		expectError bool
		// expectedErrMsg is the expected error message substring when expectError is true
		expectedErrMsg string
	}{
		{
			// Successful registration with a properly configured manager
			name: "successful webhook registration",
			setupManager: func() (manager.Manager, error) {
				return createTestManager()
			},
			expectError: false,
		},
		{
			// Test with manager setup failure
			name: "handles manager setup failure",
			setupManager: func() (manager.Manager, error) {
				return nil, assert.AnError
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := tc.setupManager()
			if err != nil {
				// If manager setup fails, we expect the test case to handle this
				assert.Error(t, err, "Manager setup should fail for this test case")
				return
			}
			require.NoError(t, err, "Failed to setup manager for test")

			err = RegisterWebhooks(mgr)

			if tc.expectError {
				assert.Error(t, err, "Expected webhook registration to fail")
				if tc.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected webhook registration to succeed")
			}
		})
	}
}

// TestRegisterWebhooksIntegration performs integration testing of webhook registration.
// It verifies that webhooks are actually registered and accessible through the webhook server.
func TestRegisterWebhooksIntegration(t *testing.T) {
	mgr, err := createTestManager()
	require.NoError(t, err, "Failed to create test manager")

	err = RegisterWebhooks(mgr)
	require.NoError(t, err, "Failed to register webhooks")

	// Verify that the webhook server has the expected webhooks registered
	webhookServer := mgr.GetWebhookServer()
	require.NotNil(t, webhookServer, "Webhook server should not be nil")

	// Note: In a real integration test, we would verify the webhook paths are registered
	// However, the controller-runtime webhook server doesn't expose registered paths
	// This test mainly ensures no panics occur during registration
}

// createTestManager creates a test manager with the necessary scheme and configuration.
// Returns a manager suitable for webhook registration testing.
func createTestManager() (manager.Manager, error) {
	scheme := runtime.NewScheme()
	err := v1alpha1.AddToScheme(scheme)
	if err != nil {
		return nil, err
	}

	// Create a minimal manager configuration for testing
	mgr := &mockManager{
		scheme:        scheme,
		webhookServer: webhook.NewServer(webhook.Options{}),
	}

	return mgr, nil
}

// mockManager implements the manager.Manager interface for testing purposes.
// It provides minimal functionality needed for webhook registration tests.
type mockManager struct {
	scheme        *runtime.Scheme
	webhookServer webhook.Server
}

func (m *mockManager) GetScheme() *runtime.Scheme {
	if m.scheme == nil {
		scheme := runtime.NewScheme()
		_ = v1alpha1.AddToScheme(scheme)
		return scheme
	}
	return m.scheme
}

func (m *mockManager) GetWebhookServer() webhook.Server {
	return m.webhookServer
}

func (m *mockManager) GetLogger() logr.Logger {
	return logr.Discard()
}

// Implement other required manager.Manager methods with minimal functionality
func (m *mockManager) Add(manager.Runnable) error                              { return nil }
func (m *mockManager) Elected() <-chan struct{}                                { return make(chan struct{}) }
func (m *mockManager) AddMetricsExtraHandler(string, http.Handler) error       { return nil }
func (m *mockManager) AddMetricsServerExtraHandler(string, http.Handler) error { return nil }
func (m *mockManager) AddHealthzCheck(string, healthz.Checker) error           { return nil }
func (m *mockManager) AddReadyzCheck(string, healthz.Checker) error            { return nil }
func (m *mockManager) Start(context.Context) error                             { return nil }
func (m *mockManager) GetConfig() *rest.Config                                 { return nil }
func (m *mockManager) GetClient() client.Client                                { return nil }
func (m *mockManager) GetFieldIndexer() client.FieldIndexer                    { return nil }
func (m *mockManager) GetCache() cache.Cache                                   { return nil }
func (m *mockManager) GetEventRecorderFor(string) record.EventRecorder         { return nil }
func (m *mockManager) GetRESTMapper() meta.RESTMapper                          { return nil }
func (m *mockManager) GetAPIReader() client.Reader                             { return nil }
func (m *mockManager) GetControllerOptions() config.Controller                 { return config.Controller{} }
func (m *mockManager) GetHTTPClient() *http.Client                             { return nil }
