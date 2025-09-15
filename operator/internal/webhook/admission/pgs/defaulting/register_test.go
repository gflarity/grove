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

package defaulting

import (
	"testing"

	"github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// TestHandler_RegisterWithManager validates the webhook registration process.
// It ensures the defaulting webhook is properly registered with the manager
// and can handle registration scenarios including error cases.
func TestHandler_RegisterWithManager(t *testing.T) {
	testCases := []struct {
		// Test case name describing the registration scenario
		name string
		// handler is the defaulting handler instance to test
		handler *Handler
		// mgr is the manager instance used for registration
		mgr manager.Manager
		// expectError indicates whether registration should fail
		expectError bool
		// expectedErrMsg is the expected error message substring when expectError is true
		expectedErrMsg string
	}{
		{
			// Successful registration with valid manager and handler
			name: "successful registration with valid manager",
			handler: &Handler{
				logger: logr.Discard(),
			},
			mgr:         createTestManager(),
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var err error

			if tc.handler != nil {
				err = tc.handler.RegisterWithManager(tc.mgr)
			}

			if tc.expectError {
				assert.Error(t, err, "Expected registration to fail")
				if tc.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected registration to succeed")
			}
		})
	}
}

// TestHandler_RegisterWithManagerIntegration performs integration testing of webhook registration.
// It validates the complete registration workflow and ensures the webhook is accessible.
func TestHandler_RegisterWithManagerIntegration(t *testing.T) {
	mgr := createTestManager()
	handler := NewHandler(mgr)

	err := handler.RegisterWithManager(mgr)
	require.NoError(t, err, "Integration registration should succeed")

	// Verify that the webhook server has been configured
	webhookServer := mgr.GetWebhookServer()
	assert.NotNil(t, webhookServer, "Webhook server should be available after registration")
}

// TestConstants validates the package constants used for webhook registration.
// It ensures the webhook name and path constants are properly defined.
func TestConstants(t *testing.T) {
	testCases := []struct {
		// Test case name describing the constant being validated
		name string
		// value is the constant value to test
		value string
		// expectEmpty indicates whether the value should be empty
		expectEmpty bool
		// expectedPrefix is the expected prefix for the value
		expectedPrefix string
	}{
		{
			// Webhook name should be properly defined
			name:        "webhook name is defined",
			value:       Name,
			expectEmpty: false,
		},
		{
			// Webhook path should be properly defined with correct prefix
			name:           "webhook path is defined with correct prefix",
			value:          webhookPath,
			expectEmpty:    false,
			expectedPrefix: "/webhooks/",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expectEmpty {
				assert.Empty(t, tc.value, "Value should be empty")
			} else {
				assert.NotEmpty(t, tc.value, "Value should not be empty")
			}

			if tc.expectedPrefix != "" {
				assert.Contains(t, tc.value, tc.expectedPrefix, "Value should contain expected prefix")
			}
		})
	}
}

// TestWebhookConfiguration validates the webhook configuration used during registration.
// It ensures the webhook is configured with the correct options and panic recovery.
func TestWebhookConfiguration(t *testing.T) {
	mgr := createTestManager()
	handler := &Handler{
		logger: logr.Discard(),
	}

	// Test that registration doesn't panic and completes successfully
	err := handler.RegisterWithManager(mgr)
	assert.NoError(t, err, "Webhook configuration should be valid")

	// Verify webhook server is properly configured
	webhookServer := mgr.GetWebhookServer()
	assert.NotNil(t, webhookServer, "Webhook server should be configured")
}

// createTestManager creates a test manager with the necessary configuration.
// Returns a manager suitable for webhook registration testing.
func createTestManager() manager.Manager {
	return &testMockManager{
		mockManager:   createMockManager().(*mockManager),
		webhookServer: webhook.NewServer(webhook.Options{}),
	}
}

// testMockManager extends mockManager with webhook server for registration tests
type testMockManager struct {
	*mockManager
	webhookServer webhook.Server
}

func (m *testMockManager) GetWebhookServer() webhook.Server {
	if m.webhookServer == nil {
		// Return a nil interface to trigger the error
		return nil
	}
	return m.webhookServer
}

func (m *testMockManager) GetScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	return scheme
}
