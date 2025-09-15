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
	"context"
	"net/http"
	"testing"

	"github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// TestNewHandler validates the creation of a new defaulting handler.
// It ensures the handler is properly initialized with the correct logger configuration.
func TestNewHandler(t *testing.T) {
	testCases := []struct {
		// Test case name describing the handler creation scenario
		name string
		// mgr is the manager instance used to create the handler
		mgr manager.Manager
		// expectNil indicates whether the handler should be nil
		expectNil bool
	}{
		{
			// Successful handler creation with valid manager
			name:      "creates handler with valid manager",
			mgr:       createMockManager(),
			expectNil: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewHandler(tc.mgr)

			if tc.expectNil {
				assert.Nil(t, handler, "Handler should be nil")
			} else {
				assert.NotNil(t, handler, "Handler should not be nil")
				assert.NotNil(t, handler.logger, "Handler logger should not be nil")
			}
		})
	}
}

// TestHandler_Default validates the defaulting webhook functionality.
// It tests the Default method with various PodGangSet configurations
// and ensures proper error handling for invalid objects.
func TestHandler_Default(t *testing.T) {
	testCases := []struct {
		// Test case name describing the defaulting scenario
		name string
		// handler is the defaulting handler instance to test
		handler *Handler
		// ctx is the context with admission request information
		ctx context.Context
		// obj is the runtime object to apply defaults to
		obj runtime.Object
		// expectError indicates whether the operation should fail
		expectError bool
		// expectedErrMsg is the expected error message substring when expectError is true
		expectedErrMsg string
		// validateDefaults is a function to validate that defaults were applied correctly
		validateDefaults func(t *testing.T, obj runtime.Object)
	}{
		{
			// Successful defaulting of a minimal PodGangSet
			name: "applies defaults to minimal PodGangSet",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx: createContextWithAdmissionRequest(),
			obj: &v1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: v1alpha1.PodGangSetSpec{
					Template: v1alpha1.PodGangSetTemplateSpec{
						Cliques: []*v1alpha1.PodCliqueTemplateSpec{
							{
								Name: "test-clique",
								Spec: v1alpha1.PodCliqueSpec{
									Replicas: 1,
								},
							},
						},
					},
				},
			},
			expectError: false,
			validateDefaults: func(t *testing.T, obj runtime.Object) {
				pgs, ok := obj.(*v1alpha1.PodGangSet)
				require.True(t, ok, "Object should be PodGangSet")

				// Verify namespace default
				assert.Equal(t, "default", pgs.Namespace, "Namespace should be defaulted to 'default'")

				// Verify termination delay default
				assert.NotNil(t, pgs.Spec.Template.TerminationDelay, "TerminationDelay should be set")

				// Verify headless service config default
				assert.NotNil(t, pgs.Spec.Template.HeadlessServiceConfig, "HeadlessServiceConfig should be set")
				assert.True(t, pgs.Spec.Template.HeadlessServiceConfig.PublishNotReadyAddresses, "PublishNotReadyAddresses should be true")
			},
		},
		{
			// Error case: invalid object type
			name: "returns error for non-PodGangSet object",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:            createContextWithAdmissionRequest(),
			obj:            &corev1.Pod{},
			expectError:    true,
			expectedErrMsg: "expected an PodGangSet object but got",
		},
		{
			// Error case: context without admission request
			name: "returns error when context lacks admission request",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx: context.Background(),
			obj: &v1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
			},
			expectError:    true,
			expectedErrMsg: "admission.Request not found in context",
		},
		{
			// Successful defaulting with existing namespace
			name: "preserves existing namespace",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx: createContextWithAdmissionRequest(),
			obj: &v1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "custom-namespace",
				},
				Spec: v1alpha1.PodGangSetSpec{
					Template: v1alpha1.PodGangSetTemplateSpec{
						Cliques: []*v1alpha1.PodCliqueTemplateSpec{
							{
								Name: "test-clique",
								Spec: v1alpha1.PodCliqueSpec{
									Replicas: 1,
								},
							},
						},
					},
				},
			},
			expectError: false,
			validateDefaults: func(t *testing.T, obj runtime.Object) {
				pgs, ok := obj.(*v1alpha1.PodGangSet)
				require.True(t, ok, "Object should be PodGangSet")

				// Verify existing namespace is preserved
				assert.Equal(t, "custom-namespace", pgs.Namespace, "Existing namespace should be preserved")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.handler.Default(tc.ctx, tc.obj)

			if tc.expectError {
				assert.Error(t, err, "Expected defaulting to fail")
				if tc.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected defaulting to succeed")
				if tc.validateDefaults != nil {
					tc.validateDefaults(t, tc.obj)
				}
			}
		})
	}
}

// TestHandler_DefaultIntegration performs integration testing of the defaulting handler.
// It validates the complete defaulting workflow with a realistic PodGangSet configuration.
func TestHandler_DefaultIntegration(t *testing.T) {
	handler := NewHandler(createMockManager())

	pgs := &v1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "integration-test-pgs",
		},
		Spec: v1alpha1.PodGangSetSpec{
			Template: v1alpha1.PodGangSetTemplateSpec{
				Cliques: []*v1alpha1.PodCliqueTemplateSpec{
					{
						Name: "worker",
						Spec: v1alpha1.PodCliqueSpec{
							Replicas: 3,
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "app",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	ctx := createContextWithAdmissionRequest()
	err := handler.Default(ctx, pgs)

	require.NoError(t, err, "Integration defaulting should succeed")

	// Verify comprehensive defaults were applied
	assert.Equal(t, "default", pgs.Namespace, "Namespace should be defaulted")
	assert.NotNil(t, pgs.Spec.Template.TerminationDelay, "TerminationDelay should be set")
	assert.NotNil(t, pgs.Spec.Template.HeadlessServiceConfig, "HeadlessServiceConfig should be set")
	assert.True(t, pgs.Spec.Template.HeadlessServiceConfig.PublishNotReadyAddresses, "PublishNotReadyAddresses should be true")

	// Verify clique defaults
	require.Len(t, pgs.Spec.Template.Cliques, 1, "Should have one clique")
	clique := pgs.Spec.Template.Cliques[0]
	assert.NotNil(t, clique.Spec.MinAvailable, "MinAvailable should be set")
	assert.Equal(t, corev1.RestartPolicyAlways, clique.Spec.PodSpec.RestartPolicy, "RestartPolicy should be defaulted")
	assert.NotNil(t, clique.Spec.PodSpec.TerminationGracePeriodSeconds, "TerminationGracePeriodSeconds should be set")
}

// createMockManager creates a mock manager for testing purposes.
// Returns a manager with minimal functionality needed for handler creation.
func createMockManager() manager.Manager {
	return &mockManager{
		logger: logr.Discard(),
	}
}

// createContextWithAdmissionRequest creates a context with an admission request.
// This simulates the context that would be provided by the webhook framework.
func createContextWithAdmissionRequest() context.Context {
	req := admission.Request{}
	return admission.NewContextWithRequest(context.Background(), req)
}

// mockManager implements the manager.Manager interface for testing purposes.
// It provides minimal functionality needed for defaulting handler tests.
type mockManager struct {
	logger logr.Logger
}

func (m *mockManager) GetLogger() logr.Logger {
	return m.logger.WithName("webhook").WithName(Name)
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
func (m *mockManager) GetScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	return scheme
}
func (m *mockManager) GetWebhookServer() webhook.Server { return nil }
