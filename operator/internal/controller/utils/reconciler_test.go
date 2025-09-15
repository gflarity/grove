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

package utils

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	grovectrl "github.com/NVIDIA/grove/operator/internal/controller/common"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// TestShouldRequeueAfter tests the ShouldRequeueAfter function which determines if an error
// indicates that reconciliation should be requeued after a delay.
func TestShouldRequeueAfter(t *testing.T) {
	testCases := []struct {
		// The error to check for requeue-after indication
		err error
		// Whether the function should return true
		expected bool
	}{
		{
			// GroveError with RequeueAfter code should return true
			err: &groveerr.GroveError{
				Code:    groveerr.ErrCodeRequeueAfter,
				Message: "test requeue after error",
			},
			expected: true,
		},
		{
			// GroveError with ContinueReconcileAndRequeue code should return false
			err: &groveerr.GroveError{
				Code:    groveerr.ErrCodeContinueReconcileAndRequeue,
				Message: "test continue and requeue error",
			},
			expected: false,
		},
		{
			// GroveError with other code should return false
			err: &groveerr.GroveError{
				Code:    "ERR_OTHER_CODE",
				Message: "test other error",
			},
			expected: false,
		},
		{
			// Non-GroveError should return false
			err:      errors.New("standard error"),
			expected: false,
		},
		{
			// Nil error should return false
			err:      nil,
			expected: false,
		},
		{
			// Wrapped standard error should return false
			err:      errors.New("wrapped: standard error"),
			expected: false,
		},
	}

	for _, tc := range testCases {
		testName := "nil_error"
		if tc.err != nil {
			if groveErr, ok := tc.err.(*groveerr.GroveError); ok {
				switch groveErr.Code {
				case groveerr.ErrCodeRequeueAfter:
					testName = "grove_error_requeue_after"
				case groveerr.ErrCodeContinueReconcileAndRequeue:
					testName = "grove_error_continue_and_requeue"
				default:
					testName = "grove_error_other_code"
				}
			} else {
				testName = "non_grove_error"
			}
		}
		t.Run(testName, func(t *testing.T) {
			result := ShouldRequeueAfter(tc.err)
			assert.Equal(t, tc.expected, result, "unexpected result for error type")
		})
	}
}

// TestShouldContinueReconcileAndRequeue tests the ShouldContinueReconcileAndRequeue function which
// determines if an error indicates that reconciliation should continue processing but also requeue.
func TestShouldContinueReconcileAndRequeue(t *testing.T) {
	testCases := []struct {
		// The error to check for continue-and-requeue indication
		err error
		// Whether the function should return true
		expected bool
	}{
		{
			// GroveError with ContinueReconcileAndRequeue code should return true
			err: &groveerr.GroveError{
				Code:    groveerr.ErrCodeContinueReconcileAndRequeue,
				Message: "test continue and requeue error",
			},
			expected: true,
		},
		{
			// GroveError with RequeueAfter code should return false
			err: &groveerr.GroveError{
				Code:    groveerr.ErrCodeRequeueAfter,
				Message: "test requeue after error",
			},
			expected: false,
		},
		{
			// GroveError with other code should return false
			err: &groveerr.GroveError{
				Code:    "ERR_OTHER_CODE",
				Message: "test other error",
			},
			expected: false,
		},
		{
			// Non-GroveError should return false
			err:      errors.New("standard error"),
			expected: false,
		},
		{
			// Nil error should return false
			err:      nil,
			expected: false,
		},
		{
			// Wrapped standard error should return false
			err:      errors.New("wrapped: standard error"),
			expected: false,
		},
		{
			// GroveError with unknown code should return false
			err: &groveerr.GroveError{
				Code:    "unknown-code",
				Message: "test unknown code error",
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		testName := "nil_error"
		if tc.err != nil {
			if groveErr, ok := tc.err.(*groveerr.GroveError); ok {
				switch groveErr.Code {
				case groveerr.ErrCodeContinueReconcileAndRequeue:
					testName = "grove_error_continue_and_requeue"
				case groveerr.ErrCodeRequeueAfter:
					testName = "grove_error_requeue_after"
				default:
					testName = "grove_error_other_code"
				}
			} else {
				testName = "non_grove_error"
			}
		}
		t.Run(testName, func(t *testing.T) {
			result := ShouldContinueReconcileAndRequeue(tc.err)
			assert.Equal(t, tc.expected, result, "unexpected result for error type")
		})
	}
}

// mockClient is a test implementation of client.Client for testing Get operations
type mockClient struct {
	mock.Mock
}

func (m *mockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj, opts)
	return args.Error(0)
}

func (m *mockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

func (m *mockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *mockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *mockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *mockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := m.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

func (m *mockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *mockClient) Status() client.StatusWriter {
	args := m.Called()
	return args.Get(0).(client.StatusWriter)
}

func (m *mockClient) Scheme() *runtime.Scheme {
	args := m.Called()
	return args.Get(0).(*runtime.Scheme)
}

func (m *mockClient) RESTMapper() meta.RESTMapper {
	args := m.Called()
	return args.Get(0).(meta.RESTMapper)
}

func (m *mockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	args := m.Called(obj)
	return args.Get(0).(schema.GroupVersionKind), args.Error(1)
}

func (m *mockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	args := m.Called(obj)
	return args.Bool(0), args.Error(1)
}

func (m *mockClient) SubResource(subResource string) client.SubResourceClient {
	args := m.Called(subResource)
	return args.Get(0).(client.SubResourceClient)
}

// mockOperator is a test implementation of component.Operator for testing cleanup verification
type mockOperator struct {
	mock.Mock
}

func (m *mockOperator) GetExistingResourceNames(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) ([]string, error) {
	args := m.Called(ctx, logger, objMeta)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockOperator) Sync(ctx context.Context, logger logr.Logger, obj *v1alpha1.PodGangSet) error {
	args := m.Called(ctx, logger, obj)
	return args.Error(0)
}

func (m *mockOperator) Delete(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) error {
	args := m.Called(ctx, logger, objMeta)
	return args.Error(0)
}

// mockOperatorRegistry is a test implementation of component.OperatorRegistry
type mockOperatorRegistry struct {
	mock.Mock
}

func (m *mockOperatorRegistry) Register(kind component.Kind, operator component.Operator[v1alpha1.PodGangSet]) {
	m.Called(kind, operator)
}

func (m *mockOperatorRegistry) GetOperator(kind component.Kind) (component.Operator[v1alpha1.PodGangSet], error) {
	args := m.Called(kind)
	return args.Get(0).(component.Operator[v1alpha1.PodGangSet]), args.Error(1)
}

func (m *mockOperatorRegistry) GetAllOperators() map[component.Kind]component.Operator[v1alpha1.PodGangSet] {
	args := m.Called()
	return args.Get(0).(map[component.Kind]component.Operator[v1alpha1.PodGangSet])
}

// TestVerifyNoResourceAwaitsCleanup tests the VerifyNoResourceAwaitsCleanup function which ensures
// no managed resources are still present before allowing finalizer removal.
func TestVerifyNoResourceAwaitsCleanup(t *testing.T) {
	testCases := []struct {
		// Test case name describing the scenario being tested
		name string
		// Map of operators to return from GetAllOperators
		operators map[component.Kind]component.Operator[v1alpha1.PodGangSet]
		// Function to configure mock operator expectations
		setupMocks func(operators map[component.Kind]component.Operator[v1alpha1.PodGangSet], objMeta metav1.ObjectMeta)
		// Expected reconcile step result
		expectedResult grovectrl.ReconcileStepResult
		// Whether the result should indicate a requeue
		expectRequeue bool
		// Whether the result should indicate an error
		expectError bool
	}{
		{
			// No operators registered should return ContinueReconcile
			name:      "no_operators_registered",
			operators: map[component.Kind]component.Operator[v1alpha1.PodGangSet]{},
			setupMocks: func(operators map[component.Kind]component.Operator[v1alpha1.PodGangSet], objMeta metav1.ObjectMeta) {
				// No setup needed for empty operators map
			},
			expectedResult: grovectrl.ContinueReconcile(),
			expectRequeue:  false,
			expectError:    false,
		},
		{
			// All operators return no resources should return ContinueReconcile
			name: "no_resources_awaiting_cleanup",
			operators: map[component.Kind]component.Operator[v1alpha1.PodGangSet]{
				component.KindPodClique: &mockOperator{},
				component.KindRole:      &mockOperator{},
			},
			setupMocks: func(operators map[component.Kind]component.Operator[v1alpha1.PodGangSet], objMeta metav1.ObjectMeta) {
				for _, op := range operators {
					mockOp := op.(*mockOperator)
					mockOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, objMeta).Return([]string{}, nil)
				}
			},
			expectedResult: grovectrl.ContinueReconcile(),
			expectRequeue:  false,
			expectError:    false,
		},
		{
			// Some operators return resources should return ReconcileAfter
			name: "resources_still_awaiting_cleanup",
			operators: map[component.Kind]component.Operator[v1alpha1.PodGangSet]{
				component.KindPodClique: &mockOperator{},
				component.KindRole:      &mockOperator{},
			},
			setupMocks: func(operators map[component.Kind]component.Operator[v1alpha1.PodGangSet], objMeta metav1.ObjectMeta) {
				podCliqueOp := operators[component.KindPodClique].(*mockOperator)
				podCliqueOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, objMeta).Return([]string{"pclq-1", "pclq-2"}, nil)

				roleOp := operators[component.KindRole].(*mockOperator)
				roleOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, objMeta).Return([]string{}, nil)
			},
			expectedResult: grovectrl.ReconcileAfter(5*time.Second, "Resources are still awaiting cleanup. Skipping removal of finalizer"),
			expectRequeue:  true,
			expectError:    false,
		},
		{
			// Multiple operators return resources should collect all and return ReconcileAfter
			name: "multiple_operators_with_resources",
			operators: map[component.Kind]component.Operator[v1alpha1.PodGangSet]{
				component.KindPodClique:      &mockOperator{},
				component.KindServiceAccount: &mockOperator{},
				component.KindRole:           &mockOperator{},
			},
			setupMocks: func(operators map[component.Kind]component.Operator[v1alpha1.PodGangSet], objMeta metav1.ObjectMeta) {
				podCliqueOp := operators[component.KindPodClique].(*mockOperator)
				podCliqueOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, objMeta).Return([]string{"pclq-1"}, nil)

				saOp := operators[component.KindServiceAccount].(*mockOperator)
				saOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, objMeta).Return([]string{"sa-1", "sa-2"}, nil)

				roleOp := operators[component.KindRole].(*mockOperator)
				roleOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, objMeta).Return([]string{}, nil)
			},
			expectedResult: grovectrl.ReconcileAfter(5*time.Second, "Resources are still awaiting cleanup. Skipping removal of finalizer"),
			expectRequeue:  true,
			expectError:    false,
		},
		{
			// Operator returns error should return ReconcileWithErrors
			name: "operator_returns_error",
			operators: map[component.Kind]component.Operator[v1alpha1.PodGangSet]{
				component.KindPodClique: &mockOperator{},
			},
			setupMocks: func(operators map[component.Kind]component.Operator[v1alpha1.PodGangSet], objMeta metav1.ObjectMeta) {
				podCliqueOp := operators[component.KindPodClique].(*mockOperator)
				podCliqueOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, objMeta).Return([]string{}, errors.New("failed to list resources"))
			},
			expectedResult: grovectrl.ReconcileWithErrors("error getting existing resource names", errors.New("failed to list resources")),
			expectRequeue:  true,
			expectError:    true,
		},
		{
			// Network timeout error should return ReconcileWithErrors
			name: "network_timeout_error",
			operators: map[component.Kind]component.Operator[v1alpha1.PodGangSet]{
				component.KindRole: &mockOperator{},
			},
			setupMocks: func(operators map[component.Kind]component.Operator[v1alpha1.PodGangSet], objMeta metav1.ObjectMeta) {
				roleOp := operators[component.KindRole].(*mockOperator)
				roleOp.On("GetExistingResourceNames", mock.Anything, mock.Anything, objMeta).Return([]string{}, errors.New("api server unavailable"))
			},
			expectedResult: grovectrl.ReconcileWithErrors("error getting existing resource names", errors.New("api server unavailable")),
			expectRequeue:  true,
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup test objects and mocks
			ctx := context.Background()
			logger := zap.New()
			objMeta := metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-ns",
			}
			registry := &mockOperatorRegistry{}

			// Configure registry mock
			registry.On("GetAllOperators").Return(tc.operators)

			// Setup operator mocks
			tc.setupMocks(tc.operators, objMeta)

			// Execute the function under test
			result := VerifyNoResourceAwaitsCleanup(ctx, logger, registry, objMeta)

			// Verify results
			if tc.expectError {
				assert.True(t, result.HasErrors(), "test case %s: expected result to have errors", tc.name)
				assert.True(t, result.NeedsRequeue(), "test case %s: expected error result to indicate requeue", tc.name)
			} else if tc.expectRequeue {
				assert.True(t, result.NeedsRequeue(), "test case %s: expected result to indicate requeue", tc.name)
				assert.False(t, result.HasErrors(), "test case %s: expected no errors for requeue case", tc.name)
			} else {
				assert.False(t, result.NeedsRequeue(), "test case %s: expected result to not indicate requeue", tc.name)
				assert.False(t, result.HasErrors(), "test case %s: expected no errors for success case", tc.name)
			}

			// Verify mock expectations
			registry.AssertExpectations(t)
			for _, op := range tc.operators {
				if mockOp, ok := op.(*mockOperator); ok {
					mockOp.AssertExpectations(t)
				}
			}
		})
	}
}

// TestGetPodGangSet tests the GetPodGangSet function which retrieves a PodGangSet object
// from the cluster and handles various error conditions.
func TestGetPodGangSet(t *testing.T) {
	testCases := []struct {
		// The error that should be returned by the mock client's Get method
		getError error
		// The expected reconcile step result
		expectedResult grovectrl.ReconcileStepResult
		// Whether the result should indicate an error
		expectError bool
	}{
		{
			// Successful retrieval should return ContinueReconcile
			getError:       nil,
			expectedResult: grovectrl.ContinueReconcile(),
			expectError:    false,
		},
		{
			// Not found error should return DoNotRequeue
			getError:       apierrors.NewNotFound(schema.GroupResource{Group: "grove.io", Resource: "podgangsets"}, "test-pgs"),
			expectedResult: grovectrl.DoNotRequeue(),
			expectError:    false,
		},
		{
			// Other errors should return ReconcileWithErrors
			getError:       errors.New("api server error"),
			expectedResult: grovectrl.ReconcileWithErrors("error getting PodGangSet", errors.New("api server error")),
			expectError:    true,
		},
		{
			// Forbidden error should return ReconcileWithErrors
			getError:       apierrors.NewForbidden(schema.GroupResource{Group: "grove.io", Resource: "podgangsets"}, "test-pgs", errors.New("access denied")),
			expectedResult: grovectrl.ReconcileWithErrors("error getting PodGangSet", apierrors.NewForbidden(schema.GroupResource{Group: "grove.io", Resource: "podgangsets"}, "test-pgs", errors.New("access denied"))),
			expectError:    true,
		},
		{
			// Timeout error should return ReconcileWithErrors
			getError:       apierrors.NewTimeoutError("request timeout", 30),
			expectedResult: grovectrl.ReconcileWithErrors("error getting PodGangSet", apierrors.NewTimeoutError("request timeout", 30)),
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		testName := "successful_retrieval"
		if tc.getError != nil {
			if apierrors.IsNotFound(tc.getError) {
				testName = "not_found_error"
			} else if apierrors.IsForbidden(tc.getError) {
				testName = "forbidden_error"
			} else if apierrors.IsTimeout(tc.getError) {
				testName = "timeout_error"
			} else {
				testName = "api_server_error"
			}
		}
		t.Run(testName, func(t *testing.T) {
			// Setup test objects and mocks
			ctx := context.Background()
			client := &mockClient{}
			logger := zap.New()
			objectKey := types.NamespacedName{Name: "test-pgs", Namespace: "test-ns"}
			pgs := &v1alpha1.PodGangSet{}

			// Configure mock expectations
			client.On("Get", ctx, objectKey, pgs, mock.Anything).Return(tc.getError)

			// Execute the function under test
			result := GetPodGangSet(ctx, client, logger, objectKey, pgs)

			// Verify results
			if tc.expectError {
				assert.True(t, result.NeedsRequeue(), "expected error result to indicate requeue")
			} else {
				assert.Equal(t, tc.expectedResult.NeedsRequeue(), result.NeedsRequeue(), "requeue mismatch")
			}

			// Verify mock expectations
			client.AssertExpectations(t)
		})
	}
}

// TestGetPodClique tests the GetPodClique function which retrieves a PodClique object
// from the cluster with configurable handling of not found errors.
func TestGetPodClique(t *testing.T) {
	testCases := []struct {
		// The error that should be returned by the mock client's Get method
		getError error
		// Whether not found errors should be ignored
		ignoreNotFound bool
		// The expected reconcile step result
		expectedResult grovectrl.ReconcileStepResult
		// Whether the result should indicate an error
		expectError bool
	}{
		{
			// Successful retrieval should return ContinueReconcile
			getError:       nil,
			ignoreNotFound: false,
			expectedResult: grovectrl.ContinueReconcile(),
			expectError:    false,
		},
		{
			// Not found error with ignoreNotFound=true should return DoNotRequeue
			getError:       apierrors.NewNotFound(schema.GroupResource{Group: "grove.io", Resource: "podcliques"}, "test-pclq"),
			ignoreNotFound: true,
			expectedResult: grovectrl.DoNotRequeue(),
			expectError:    false,
		},
		{
			// Not found error with ignoreNotFound=false should return ReconcileWithErrors
			getError:       apierrors.NewNotFound(schema.GroupResource{Group: "grove.io", Resource: "podcliques"}, "test-pclq"),
			ignoreNotFound: false,
			expectedResult: grovectrl.ReconcileWithErrors("error getting PodClique", apierrors.NewNotFound(schema.GroupResource{Group: "grove.io", Resource: "podcliques"}, "test-pclq")),
			expectError:    true,
		},
		{
			// Other errors should return ReconcileWithErrors regardless of ignoreNotFound
			getError:       errors.New("api server error"),
			ignoreNotFound: true,
			expectedResult: grovectrl.ReconcileWithErrors("error getting PodClique", errors.New("api server error")),
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		testName := "successful_retrieval"
		if tc.getError != nil {
			if apierrors.IsNotFound(tc.getError) {
				if tc.ignoreNotFound {
					testName = "not_found_error_ignored"
				} else {
					testName = "not_found_error_not_ignored"
				}
			} else {
				testName = "api_server_error"
			}
		}
		t.Run(testName, func(t *testing.T) {
			// Setup test objects and mocks
			ctx := context.Background()
			client := &mockClient{}
			logger := zap.New()
			objectKey := types.NamespacedName{Name: "test-pclq", Namespace: "test-ns"}
			pclq := &v1alpha1.PodClique{}

			// Configure mock expectations
			client.On("Get", ctx, objectKey, pclq, mock.Anything).Return(tc.getError)

			// Execute the function under test
			result := GetPodClique(ctx, client, logger, objectKey, pclq, tc.ignoreNotFound)

			// Verify results
			if tc.expectError {
				assert.True(t, result.NeedsRequeue(), "expected error result to indicate requeue")
			} else {
				assert.Equal(t, tc.expectedResult.NeedsRequeue(), result.NeedsRequeue(), "requeue mismatch")
			}

			// Verify mock expectations
			client.AssertExpectations(t)
		})
	}
}

// TestGetPodCliqueScalingGroup tests the GetPodCliqueScalingGroup function which retrieves
// a PodCliqueScalingGroup object from the cluster and handles various error conditions.
func TestGetPodCliqueScalingGroup(t *testing.T) {
	testCases := []struct {
		// The error that should be returned by the mock client's Get method
		getError error
		// The expected reconcile step result
		expectedResult grovectrl.ReconcileStepResult
		// Whether the result should indicate an error
		expectError bool
	}{
		{
			// Successful retrieval should return ContinueReconcile
			getError:       nil,
			expectedResult: grovectrl.ContinueReconcile(),
			expectError:    false,
		},
		{
			// Not found error should return DoNotRequeue
			getError:       apierrors.NewNotFound(schema.GroupResource{Group: "grove.io", Resource: "podcliquescalinggroups"}, "test-pcsg"),
			expectedResult: grovectrl.DoNotRequeue(),
			expectError:    false,
		},
		{
			// Other errors should return ReconcileWithErrors
			getError:       errors.New("api server error"),
			expectedResult: grovectrl.ReconcileWithErrors("error getting PodCliqueScalingGroup", errors.New("api server error")),
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		testName := "successful_retrieval"
		if tc.getError != nil {
			if apierrors.IsNotFound(tc.getError) {
				testName = "not_found_error"
			} else if apierrors.IsForbidden(tc.getError) {
				testName = "forbidden_error"
			} else if apierrors.IsTimeout(tc.getError) {
				testName = "timeout_error"
			} else {
				testName = "api_server_error"
			}
		}
		t.Run(testName, func(t *testing.T) {
			// Setup test objects and mocks
			ctx := context.Background()
			client := &mockClient{}
			logger := zap.New()
			objectKey := types.NamespacedName{Name: "test-pcsg", Namespace: "test-ns"}
			pcsg := &v1alpha1.PodCliqueScalingGroup{}

			// Configure mock expectations
			client.On("Get", ctx, objectKey, pcsg, mock.Anything).Return(tc.getError)

			// Execute the function under test
			result := GetPodCliqueScalingGroup(ctx, client, logger, objectKey, pcsg)

			// Verify results
			if tc.expectError {
				assert.True(t, result.NeedsRequeue(), "expected error result to indicate requeue")
			} else {
				assert.Equal(t, tc.expectedResult.NeedsRequeue(), result.NeedsRequeue(), "requeue mismatch")
			}

			// Verify mock expectations
			client.AssertExpectations(t)
		})
	}
}
