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

package validation

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// TestNewHandler validates the creation of a new validation handler.
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

// TestHandler_ValidateCreate validates the creation validation webhook functionality.
// It tests the ValidateCreate method with various PodGangSet configurations
// and ensures proper error handling and warning generation.
func TestHandler_ValidateCreate(t *testing.T) {
	testCases := []struct {
		// Test case name describing the validation scenario
		name string
		// handler is the validation handler instance to test
		handler *Handler
		// ctx is the context with admission request information
		ctx context.Context
		// obj is the runtime object to validate for creation
		obj runtime.Object
		// expectError indicates whether validation should fail
		expectError bool
		// expectedErrMsg is the expected error message substring when expectError is true
		expectedErrMsg string
		// expectWarnings indicates whether warnings should be generated
		expectWarnings bool
	}{
		{
			// Successful validation of a valid PodGangSet
			name: "validates valid PodGangSet successfully",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:         createContextWithAdmissionRequest(),
			obj:         createValidPodGangSet("test-pgs"),
			expectError: false,
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
			// Error case: PodGangSet with validation errors
			name: "returns error for invalid PodGangSet",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:            createContextWithAdmissionRequest(),
			obj:            createInvalidPodGangSet("invalid-pgs"),
			expectError:    true,
			expectedErrMsg: "field is required",
		},
		{
			// Warning case: PodGangSet with warnings but no errors
			name: "returns warnings for PodGangSet with warnings",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:            createContextWithAdmissionRequest(),
			obj:            createPodGangSetWithWarnings("warning-pgs"),
			expectError:    false,
			expectWarnings: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			warnings, err := tc.handler.ValidateCreate(tc.ctx, tc.obj)

			if tc.expectError {
				assert.Error(t, err, "Expected validation to fail")
				if tc.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected validation to succeed")
			}

			if tc.expectWarnings {
				assert.NotEmpty(t, warnings, "Expected warnings to be generated")
			} else {
				assert.Empty(t, warnings, "Expected no warnings")
			}
		})
	}
}

// TestHandler_ValidateUpdate validates the update validation webhook functionality.
// It tests the ValidateUpdate method with various PodGangSet update scenarios
// and ensures proper validation of immutable fields and allowed changes.
func TestHandler_ValidateUpdate(t *testing.T) {
	testCases := []struct {
		// Test case name describing the update validation scenario
		name string
		// handler is the validation handler instance to test
		handler *Handler
		// ctx is the context with admission request information
		ctx context.Context
		// newObj is the new version of the object being updated
		newObj runtime.Object
		// oldObj is the existing version of the object being updated
		oldObj runtime.Object
		// expectError indicates whether validation should fail
		expectError bool
		// expectedErrMsg is the expected error message substring when expectError is true
		expectedErrMsg string
		// expectWarnings indicates whether warnings should be generated
		expectWarnings bool
	}{
		{
			// Successful validation of allowed update
			name: "validates allowed update successfully",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:         createContextWithAdmissionRequest(),
			newObj:      createValidPodGangSet("test-pgs"),
			oldObj:      createValidPodGangSet("test-pgs"),
			expectError: false,
		},
		{
			// Error case: invalid new object type
			name: "returns error for invalid new object type",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:            createContextWithAdmissionRequest(),
			newObj:         &corev1.Pod{},
			oldObj:         createValidPodGangSet("test-pgs"),
			expectError:    true,
			expectedErrMsg: "expected an PodGangSet object but got",
		},
		{
			// Error case: invalid old object type
			name: "returns error for invalid old object type",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:            createContextWithAdmissionRequest(),
			newObj:         createValidPodGangSet("test-pgs"),
			oldObj:         &corev1.Pod{},
			expectError:    true,
			expectedErrMsg: "expected an PodGangSet object but got",
		},
		{
			// Error case: update with validation errors in new object
			name: "returns error for invalid new PodGangSet",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:            createContextWithAdmissionRequest(),
			newObj:         createInvalidPodGangSet("invalid-pgs"),
			oldObj:         createValidPodGangSet("test-pgs"),
			expectError:    true,
			expectedErrMsg: "field is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			warnings, err := tc.handler.ValidateUpdate(tc.ctx, tc.newObj, tc.oldObj)

			if tc.expectError {
				assert.Error(t, err, "Expected validation to fail")
				if tc.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected validation to succeed")
			}

			if tc.expectWarnings {
				assert.NotEmpty(t, warnings, "Expected warnings to be generated")
			} else {
				assert.Empty(t, warnings, "Expected no warnings")
			}
		})
	}
}

// TestHandler_ValidateDelete validates the deletion validation webhook functionality.
// It tests the ValidateDelete method and ensures it allows all deletions as expected.
func TestHandler_ValidateDelete(t *testing.T) {
	testCases := []struct {
		// Test case name describing the deletion validation scenario
		name string
		// handler is the validation handler instance to test
		handler *Handler
		// ctx is the context with admission request information
		ctx context.Context
		// obj is the runtime object being deleted
		obj runtime.Object
		// expectError indicates whether validation should fail
		expectError bool
		// expectWarnings indicates whether warnings should be generated
		expectWarnings bool
	}{
		{
			// Deletion should always be allowed
			name: "allows deletion of PodGangSet",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:         createContextWithAdmissionRequest(),
			obj:         createValidPodGangSet("test-pgs"),
			expectError: false,
		},
		{
			// Deletion should be allowed even for invalid objects
			name: "allows deletion of any object",
			handler: &Handler{
				logger: logr.Discard(),
			},
			ctx:         createContextWithAdmissionRequest(),
			obj:         &corev1.Pod{},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			warnings, err := tc.handler.ValidateDelete(tc.ctx, tc.obj)

			if tc.expectError {
				assert.Error(t, err, "Expected validation to fail")
			} else {
				assert.NoError(t, err, "Expected validation to succeed")
			}

			if tc.expectWarnings {
				assert.NotEmpty(t, warnings, "Expected warnings to be generated")
			} else {
				assert.Empty(t, warnings, "Expected no warnings")
			}
		})
	}
}

// TestCastToPodGangSet validates the object type casting functionality.
// It ensures proper error handling when casting invalid objects to PodGangSet.
func TestCastToPodGangSet(t *testing.T) {
	testCases := []struct {
		// Test case name describing the casting scenario
		name string
		// obj is the runtime object to cast
		obj runtime.Object
		// expectError indicates whether casting should fail
		expectError bool
		// expectedErrMsg is the expected error message substring when expectError is true
		expectedErrMsg string
	}{
		{
			// Successful casting of valid PodGangSet
			name:        "successfully casts PodGangSet",
			obj:         createValidPodGangSet("test-pgs"),
			expectError: false,
		},
		{
			// Error case: invalid object type
			name:           "returns error for non-PodGangSet object",
			obj:            &corev1.Pod{},
			expectError:    true,
			expectedErrMsg: "expected an PodGangSet object but got",
		},
		{
			// Error case: nil object
			name:           "returns error for nil object",
			obj:            nil,
			expectError:    true,
			expectedErrMsg: "expected an PodGangSet object but got",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pgs, err := castToPodGangSet(tc.obj)

			if tc.expectError {
				assert.Error(t, err, "Expected casting to fail")
				assert.Nil(t, pgs, "PodGangSet should be nil on error")
				if tc.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected casting to succeed")
				assert.NotNil(t, pgs, "PodGangSet should not be nil on success")
			}
		})
	}
}

// createValidPodGangSet creates a valid PodGangSet for testing purposes.
// Returns a PodGangSet with minimal valid configuration.
func createValidPodGangSet(name string) *v1alpha1.PodGangSet {
	return &v1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1alpha1.PodGangSetSpec{
			Replicas: 1,
			Template: v1alpha1.PodGangSetTemplateSpec{
				TerminationDelay: &metav1.Duration{Duration: 30 * time.Second},
				StartupType:      ptr.To(v1alpha1.CliqueStartupTypeAnyOrder),
				Cliques: []*v1alpha1.PodCliqueTemplateSpec{
					{
						Name: "test-clique",
						Spec: v1alpha1.PodCliqueSpec{
							Replicas:     1,
							RoleName:     "test-role",
							MinAvailable: ptr.To[int32](1),
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "test:latest",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// createInvalidPodGangSet creates an invalid PodGangSet for testing error cases.
// Returns a PodGangSet with validation errors (missing required fields).
func createInvalidPodGangSet(name string) *v1alpha1.PodGangSet {
	return &v1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1alpha1.PodGangSetSpec{
			// Missing required fields to trigger validation errors
			Template: v1alpha1.PodGangSetTemplateSpec{
				// Missing required cliques
			},
		},
	}
}

// createPodGangSetWithWarnings creates a PodGangSet that generates warnings but no errors.
// Returns a PodGangSet with configuration that triggers validation warnings.
func createPodGangSetWithWarnings(name string) *v1alpha1.PodGangSet {
	pgs := createValidPodGangSet(name)
	// Set restart policy to trigger warning
	pgs.Spec.Template.Cliques[0].Spec.PodSpec.RestartPolicy = corev1.RestartPolicyNever
	return pgs
}

// createContextWithAdmissionRequest creates a context with an admission request.
// This simulates the context that would be provided by the webhook framework.
func createContextWithAdmissionRequest() context.Context {
	req := admission.Request{}
	return admission.NewContextWithRequest(context.Background(), req)
}

// createMockManager creates a mock manager for testing purposes.
// Returns a manager with minimal functionality needed for handler creation.
func createMockManager() manager.Manager {
	return &mockManager{
		logger: logr.Discard(),
	}
}

// mockManager implements the manager.Manager interface for testing purposes.
// It provides minimal functionality needed for validation handler tests.
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
