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

package common

import (
	"context"
	"errors"
	"testing"
	"time"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// mockReconciledObject is a mock implementation of ReconciledObject for testing
type mockReconciledObject struct {
	client.Object
	lastOperation *grovecorev1alpha1.LastOperation
	lastErrors    []grovecorev1alpha1.LastError
	gvk           schema.GroupVersionKind
}

func (m *mockReconciledObject) SetLastOperation(operation *grovecorev1alpha1.LastOperation) {
	m.lastOperation = operation
}

func (m *mockReconciledObject) SetLastErrors(lastErrors ...grovecorev1alpha1.LastError) {
	m.lastErrors = lastErrors
}

func (m *mockReconciledObject) GetObjectKind() schema.ObjectKind {
	return &mockObjectKind{gvk: m.gvk}
}

func (m *mockReconciledObject) DeepCopyObject() runtime.Object {
	copy := &mockReconciledObject{
		Object:        m.Object,
		lastOperation: m.lastOperation,
		lastErrors:    make([]grovecorev1alpha1.LastError, len(m.lastErrors)),
		gvk:           m.gvk,
	}
	if m.lastOperation != nil {
		copy.lastOperation = &grovecorev1alpha1.LastOperation{
			Type:           m.lastOperation.Type,
			State:          m.lastOperation.State,
			LastUpdateTime: m.lastOperation.LastUpdateTime,
			Description:    m.lastOperation.Description,
		}
	}
	for i, err := range m.lastErrors {
		copy.lastErrors[i] = err
	}
	return copy
}

type mockObjectKind struct {
	gvk schema.GroupVersionKind
}

func (m *mockObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return m.gvk
}

func (m *mockObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	m.gvk = gvk
}

// mockStatusWriter is a mock implementation of client.StatusWriter for testing
type mockStatusWriter struct {
	mock.Mock
	shouldFail bool
	failError  error
}

func (m *mockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if m.shouldFail {
		return m.failError
	}
	return nil
}

func (m *mockStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if m.shouldFail {
		return m.failError
	}
	return nil
}

func (m *mockStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	if m.shouldFail {
		return m.failError
	}
	return nil
}

// mockEventRecorder is a mock implementation of record.EventRecorder for testing
type mockEventRecorder struct {
	mock.Mock
}

func (m *mockEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	m.Called(object, eventtype, reason, message)
}

func (m *mockEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	m.Called(object, eventtype, reason, messageFmt, args)
}

func (m *mockEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	m.Called(object, annotations, eventtype, reason, messageFmt, args)
}

// fakeClientWithStatusWriter wraps a fake client with a custom status writer
type fakeClientWithStatusWriter struct {
	client.Client
	statusWriter *mockStatusWriter
}

func (f *fakeClientWithStatusWriter) Status() client.StatusWriter {
	return f.statusWriter
}

// TestNewReconcileStatusRecorder tests the constructor function
func TestNewReconcileStatusRecorder(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	mockEventRecorder := &mockEventRecorder{}

	recorder := NewReconcileStatusRecorder(fakeClient, mockEventRecorder)

	require.NotNil(t, recorder)
	// Verify the recorder implements the interface
	assert.Implements(t, (*ReconcileStatusRecorder)(nil), recorder)
}

// TestRecordStart tests the RecordStart method with various scenarios
func TestRecordStart(t *testing.T) {
	tests := []struct {
		// Test case description explaining what scenario is being tested
		name string
		// The operation type being tested (reconcile or delete)
		operationType grovecorev1alpha1.LastOperationType
		// The resource kind to use in the mock object
		resourceKind string
		// Whether the status patch should succeed or fail
		patchShouldFail bool
		// The error to return from the patch operation if it should fail
		patchError error
		// Expected event reason that should be emitted
		expectedEventReason string
		// Expected event message that should be emitted
		expectedEventMessage string
		// Expected description in the LastOperation status
		expectedDescription string
	}{
		{
			// Test successful reconcile operation start recording
			name:                 "successful reconcile operation start",
			operationType:        grovecorev1alpha1.LastOperationTypeReconcile,
			resourceKind:         "PodGangSet",
			patchShouldFail:      false,
			expectedEventReason:  grovecorev1alpha1.EventReconciling,
			expectedEventMessage: "Reconciling PodGangSet",
			expectedDescription:  "PodGangSet reconciliation is in progress",
		},
		{
			// Test successful delete operation start recording
			name:                 "successful delete operation start",
			operationType:        grovecorev1alpha1.LastOperationTypeDelete,
			resourceKind:         "PodClique",
			patchShouldFail:      false,
			expectedEventReason:  grovecorev1alpha1.EventDeleting,
			expectedEventMessage: "Reconciling PodClique",
			expectedDescription:  "PodClique deletion is in progress",
		},
		{
			// Test handling of status patch failure during reconcile start
			name:                 "patch failure during reconcile start",
			operationType:        grovecorev1alpha1.LastOperationTypeReconcile,
			resourceKind:         "PodGangSet",
			patchShouldFail:      true,
			patchError:           errors.New("patch failed"),
			expectedEventReason:  grovecorev1alpha1.EventReconciling,
			expectedEventMessage: "Reconciling PodGangSet",
			expectedDescription:  "PodGangSet reconciliation is in progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			scheme := runtime.NewScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			mockStatusWriter := &mockStatusWriter{
				shouldFail: tt.patchShouldFail,
				failError:  tt.patchError,
			}
			clientWithStatus := &fakeClientWithStatusWriter{
				Client:       fakeClient,
				statusWriter: mockStatusWriter,
			}
			mockEventRecorder := &mockEventRecorder{}

			// Create test object with specified resource kind
			obj := &mockReconciledObject{
				gvk: schema.GroupVersionKind{Kind: tt.resourceKind},
			}

			// Setup event recorder expectations
			mockEventRecorder.On("Event", obj, v1.EventTypeNormal, tt.expectedEventReason, tt.expectedEventMessage)

			// Create recorder and execute test
			recorder := NewReconcileStatusRecorder(clientWithStatus, mockEventRecorder)
			err := recorder.RecordStart(context.Background(), obj, tt.operationType)

			// Verify results
			if tt.patchShouldFail {
				assert.Error(t, err)
				assert.Equal(t, tt.patchError, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify LastOperation was set correctly
			require.NotNil(t, obj.lastOperation)
			assert.Equal(t, tt.operationType, obj.lastOperation.Type)
			assert.Equal(t, grovecorev1alpha1.LastOperationStateProcessing, obj.lastOperation.State)
			assert.Equal(t, tt.expectedDescription, obj.lastOperation.Description)
			assert.WithinDuration(t, time.Now().UTC(), obj.lastOperation.LastUpdateTime.Time, time.Second)

			// Verify mocks were called as expected
			mockEventRecorder.AssertExpectations(t)
		})
	}
}

// TestRecordCompletion tests the RecordCompletion method with various scenarios
func TestRecordCompletion(t *testing.T) {
	tests := []struct {
		// Test case description explaining what scenario is being tested
		name string
		// The operation type being tested (reconcile or delete)
		operationType grovecorev1alpha1.LastOperationType
		// The resource kind to use in the mock object
		resourceKind string
		// The reconcile step result to pass (nil for success, non-nil for errors)
		stepResult *ReconcileStepResult
		// Whether the status patch should succeed or fail
		patchShouldFail bool
		// The error to return from the patch operation if it should fail
		patchError error
		// Expected event type that should be emitted
		expectedEventType string
		// Expected event reason that should be emitted
		expectedEventReason string
		// Expected event message that should be emitted
		expectedEventMessage string
		// Expected LastOperation state
		expectedOperationState grovecorev1alpha1.LastOperationState
		// Expected description in the LastOperation status
		expectedDescription string
		// Expected number of LastErrors
		expectedErrorCount int
	}{
		{
			// Test successful reconcile completion without errors
			name:                   "successful reconcile completion",
			operationType:          grovecorev1alpha1.LastOperationTypeReconcile,
			resourceKind:           "PodGangSet",
			stepResult:             nil,
			patchShouldFail:        false,
			expectedEventType:      v1.EventTypeNormal,
			expectedEventReason:    grovecorev1alpha1.EventReconciled,
			expectedEventMessage:   "Reconciled PodGangSet",
			expectedOperationState: grovecorev1alpha1.LastOperationStateSucceeded,
			expectedDescription:    "PodGangSet has been successfully reconciled",
			expectedErrorCount:     0,
		},
		{
			// Test successful delete completion without errors
			name:                   "successful delete completion",
			operationType:          grovecorev1alpha1.LastOperationTypeDelete,
			resourceKind:           "PodClique",
			stepResult:             nil,
			patchShouldFail:        false,
			expectedEventType:      v1.EventTypeNormal,
			expectedEventReason:    grovecorev1alpha1.EventDeleted,
			expectedEventMessage:   "Deleted PodClique",
			expectedOperationState: grovecorev1alpha1.LastOperationStateSucceeded,
			expectedDescription:    "PodClique has been successfully deleted",
			expectedErrorCount:     0,
		},
		{
			// Test reconcile completion with errors
			name:          "reconcile completion with errors",
			operationType: grovecorev1alpha1.LastOperationTypeReconcile,
			resourceKind:  "PodGangSet",
			stepResult: &ReconcileStepResult{
				errs:        []error{groveerr.New("ERR_TEST", "test-operation", "test error")},
				description: "Test error occurred",
			},
			patchShouldFail:        false,
			expectedEventType:      v1.EventTypeWarning,
			expectedEventReason:    grovecorev1alpha1.EventReconcileError,
			expectedEventMessage:   "Test error occurred",
			expectedOperationState: grovecorev1alpha1.LastOperationStateError,
			expectedDescription:    "Test error occurred. Operation will be retried.",
			expectedErrorCount:     1,
		},
		{
			// Test delete completion with errors
			name:          "delete completion with errors",
			operationType: grovecorev1alpha1.LastOperationTypeDelete,
			resourceKind:  "PodClique",
			stepResult: &ReconcileStepResult{
				errs:        []error{groveerr.New("ERR_DELETE", "delete-operation", "delete failed")},
				description: "Delete operation failed",
			},
			patchShouldFail:        false,
			expectedEventType:      v1.EventTypeWarning,
			expectedEventReason:    grovecorev1alpha1.EventDeleteError,
			expectedEventMessage:   "Delete operation failed",
			expectedOperationState: grovecorev1alpha1.LastOperationStateError,
			expectedDescription:    "Delete operation failed. Operation will be retried.",
			expectedErrorCount:     1,
		},
		{
			// Test handling of status patch failure during completion
			name:                   "patch failure during completion",
			operationType:          grovecorev1alpha1.LastOperationTypeReconcile,
			resourceKind:           "PodGangSet",
			stepResult:             nil,
			patchShouldFail:        true,
			patchError:             errors.New("status patch failed"),
			expectedEventType:      v1.EventTypeNormal,
			expectedEventReason:    grovecorev1alpha1.EventReconciled,
			expectedEventMessage:   "Reconciled PodGangSet",
			expectedOperationState: grovecorev1alpha1.LastOperationStateSucceeded,
			expectedDescription:    "PodGangSet has been successfully reconciled",
			expectedErrorCount:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			scheme := runtime.NewScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			mockStatusWriter := &mockStatusWriter{
				shouldFail: tt.patchShouldFail,
				failError:  tt.patchError,
			}
			clientWithStatus := &fakeClientWithStatusWriter{
				Client:       fakeClient,
				statusWriter: mockStatusWriter,
			}
			mockEventRecorder := &mockEventRecorder{}

			// Create test object with specified resource kind
			obj := &mockReconciledObject{
				gvk: schema.GroupVersionKind{Kind: tt.resourceKind},
			}

			// Setup event recorder expectations
			mockEventRecorder.On("Event", obj, tt.expectedEventType, tt.expectedEventReason, tt.expectedEventMessage)

			// Create recorder and execute test
			recorder := NewReconcileStatusRecorder(clientWithStatus, mockEventRecorder)
			err := recorder.RecordCompletion(context.Background(), obj, tt.operationType, tt.stepResult)

			// Verify results
			if tt.patchShouldFail {
				assert.Error(t, err)
				assert.Equal(t, tt.patchError, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify LastOperation was set correctly
			require.NotNil(t, obj.lastOperation)
			assert.Equal(t, tt.operationType, obj.lastOperation.Type)
			assert.Equal(t, tt.expectedOperationState, obj.lastOperation.State)
			assert.Equal(t, tt.expectedDescription, obj.lastOperation.Description)
			assert.WithinDuration(t, time.Now().UTC(), obj.lastOperation.LastUpdateTime.Time, time.Second)

			// Verify LastErrors were set correctly
			assert.Len(t, obj.lastErrors, tt.expectedErrorCount)

			// Verify mocks were called as expected
			mockEventRecorder.AssertExpectations(t)
		})
	}
}

// TestGetCompletionEventReason tests the helper function for determining event reasons
func TestGetCompletionEventReason(t *testing.T) {
	tests := []struct {
		// Test case description explaining what scenario is being tested
		name string
		// The operation type being tested
		operationType grovecorev1alpha1.LastOperationType
		// The operation result (nil for success, non-nil with errors for failure)
		operationResult *ReconcileStepResult
		// Expected event reason string
		expectedReason string
	}{
		{
			// Test reconcile success event reason
			name:            "reconcile success",
			operationType:   grovecorev1alpha1.LastOperationTypeReconcile,
			operationResult: nil,
			expectedReason:  grovecorev1alpha1.EventReconciled,
		},
		{
			// Test delete success event reason
			name:            "delete success",
			operationType:   grovecorev1alpha1.LastOperationTypeDelete,
			operationResult: nil,
			expectedReason:  grovecorev1alpha1.EventDeleted,
		},
		{
			// Test reconcile error event reason
			name:          "reconcile error",
			operationType: grovecorev1alpha1.LastOperationTypeReconcile,
			operationResult: &ReconcileStepResult{
				errs: []error{errors.New("test error")},
			},
			expectedReason: grovecorev1alpha1.EventReconcileError,
		},
		{
			// Test delete error event reason
			name:          "delete error",
			operationType: grovecorev1alpha1.LastOperationTypeDelete,
			operationResult: &ReconcileStepResult{
				errs: []error{errors.New("test error")},
			},
			expectedReason: grovecorev1alpha1.EventDeleteError,
		},
		{
			// Test reconcile with no errors but non-nil result
			name:          "reconcile with empty result",
			operationType: grovecorev1alpha1.LastOperationTypeReconcile,
			operationResult: &ReconcileStepResult{
				errs: []error{},
			},
			expectedReason: grovecorev1alpha1.EventReconciled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := getCompletionEventReason(tt.operationType, tt.operationResult)
			assert.Equal(t, tt.expectedReason, reason)
		})
	}
}

// TestGetCompletionEventMessage tests the helper function for generating event messages
func TestGetCompletionEventMessage(t *testing.T) {
	tests := []struct {
		// Test case description explaining what scenario is being tested
		name string
		// The operation type being tested
		operationType grovecorev1alpha1.LastOperationType
		// The operation result (nil for success, non-nil with description for failure)
		operationResult *ReconcileStepResult
		// The resource kind for the message
		resourceKind string
		// Expected event message string
		expectedMessage string
	}{
		{
			// Test reconcile success message
			name:            "reconcile success message",
			operationType:   grovecorev1alpha1.LastOperationTypeReconcile,
			operationResult: nil,
			resourceKind:    "PodGangSet",
			expectedMessage: "Reconciled PodGangSet",
		},
		{
			// Test delete success message
			name:            "delete success message",
			operationType:   grovecorev1alpha1.LastOperationTypeDelete,
			operationResult: nil,
			resourceKind:    "PodClique",
			expectedMessage: "Deleted PodClique",
		},
		{
			// Test error message uses operation result description
			name:          "error message from result",
			operationType: grovecorev1alpha1.LastOperationTypeReconcile,
			operationResult: &ReconcileStepResult{
				errs:        []error{errors.New("test error")},
				description: "Custom error description",
			},
			resourceKind:    "PodGangSet",
			expectedMessage: "Custom error description",
		},
		{
			// Test empty result with no errors uses success message
			name:          "empty result success message",
			operationType: grovecorev1alpha1.LastOperationTypeDelete,
			operationResult: &ReconcileStepResult{
				errs: []error{},
			},
			resourceKind:    "PodClique",
			expectedMessage: "Deleted PodClique",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := getCompletionEventMessage(tt.operationType, tt.operationResult, tt.resourceKind)
			assert.Equal(t, tt.expectedMessage, message)
		})
	}
}

// TestGetLastOperationCompletionDescription tests the helper function for generating status descriptions
func TestGetLastOperationCompletionDescription(t *testing.T) {
	tests := []struct {
		// Test case description explaining what scenario is being tested
		name string
		// The operation type being tested
		operationType grovecorev1alpha1.LastOperationType
		// The resource kind for the description
		resourceKind string
		// The operation result (nil for success, non-nil with description for failure)
		operationResult *ReconcileStepResult
		// Expected description string
		expectedDescription string
	}{
		{
			// Test reconcile success description
			name:                "reconcile success description",
			operationType:       grovecorev1alpha1.LastOperationTypeReconcile,
			resourceKind:        "PodGangSet",
			operationResult:     nil,
			expectedDescription: "PodGangSet has been successfully reconciled",
		},
		{
			// Test delete success description
			name:                "delete success description",
			operationType:       grovecorev1alpha1.LastOperationTypeDelete,
			resourceKind:        "PodClique",
			operationResult:     nil,
			expectedDescription: "PodClique has been successfully deleted",
		},
		{
			// Test error description includes retry notice
			name:          "error description with retry notice",
			operationType: grovecorev1alpha1.LastOperationTypeReconcile,
			resourceKind:  "PodGangSet",
			operationResult: &ReconcileStepResult{
				errs:        []error{errors.New("test error")},
				description: "Operation failed due to test error",
			},
			expectedDescription: "Operation failed due to test error. Operation will be retried.",
		},
		{
			// Test empty result with no errors uses success description
			name:          "empty result success description",
			operationType: grovecorev1alpha1.LastOperationTypeDelete,
			resourceKind:  "PodClique",
			operationResult: &ReconcileStepResult{
				errs: []error{},
			},
			expectedDescription: "PodClique has been successfully deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			description := getLastOperationCompletionDescription(tt.operationType, tt.resourceKind, tt.operationResult)
			assert.Equal(t, tt.expectedDescription, description)
		})
	}
}

// TestRecordLastOperationAndLastErrors tests the internal helper method
func TestRecordLastOperationAndLastErrors(t *testing.T) {
	tests := []struct {
		// Test case description explaining what scenario is being tested
		name string
		// The operation type to set
		operationType grovecorev1alpha1.LastOperationType
		// The operation state to set
		operationState grovecorev1alpha1.LastOperationState
		// The description to set
		description string
		// The last errors to set
		lastErrors []grovecorev1alpha1.LastError
		// Whether the patch should fail
		patchShouldFail bool
		// The error to return from patch if it should fail
		patchError error
	}{
		{
			// Test successful status update with no errors
			name:            "successful update with no errors",
			operationType:   grovecorev1alpha1.LastOperationTypeReconcile,
			operationState:  grovecorev1alpha1.LastOperationStateSucceeded,
			description:     "Operation completed successfully",
			lastErrors:      []grovecorev1alpha1.LastError{},
			patchShouldFail: false,
		},
		{
			// Test successful status update with errors
			name:           "successful update with errors",
			operationType:  grovecorev1alpha1.LastOperationTypeReconcile,
			operationState: grovecorev1alpha1.LastOperationStateError,
			description:    "Operation failed",
			lastErrors: []grovecorev1alpha1.LastError{
				{
					Code:        "ERR_TEST",
					Description: "Test error",
					ObservedAt:  metav1.NewTime(time.Now().UTC()),
				},
			},
			patchShouldFail: false,
		},
		{
			// Test patch failure handling
			name:            "patch failure",
			operationType:   grovecorev1alpha1.LastOperationTypeReconcile,
			operationState:  grovecorev1alpha1.LastOperationStateSucceeded,
			description:     "Operation completed",
			lastErrors:      []grovecorev1alpha1.LastError{},
			patchShouldFail: true,
			patchError:      errors.New("patch operation failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			scheme := runtime.NewScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			mockStatusWriter := &mockStatusWriter{
				shouldFail: tt.patchShouldFail,
				failError:  tt.patchError,
			}
			clientWithStatus := &fakeClientWithStatusWriter{
				Client:       fakeClient,
				statusWriter: mockStatusWriter,
			}
			mockEventRecorder := &mockEventRecorder{}

			// Create test object
			obj := &mockReconciledObject{
				gvk: schema.GroupVersionKind{Kind: "TestResource"},
			}

			// Create recorder and call the internal method
			recorderImpl := &recorder{
				client:        clientWithStatus,
				eventRecorder: mockEventRecorder,
			}

			err := recorderImpl.recordLastOperationAndLastErrors(
				context.Background(),
				obj,
				tt.operationType,
				tt.operationState,
				tt.description,
				tt.lastErrors...,
			)

			// Verify results
			if tt.patchShouldFail {
				assert.Error(t, err)
				assert.Equal(t, tt.patchError, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify LastOperation was set correctly
			require.NotNil(t, obj.lastOperation)
			assert.Equal(t, tt.operationType, obj.lastOperation.Type)
			assert.Equal(t, tt.operationState, obj.lastOperation.State)
			assert.Equal(t, tt.description, obj.lastOperation.Description)
			assert.WithinDuration(t, time.Now().UTC(), obj.lastOperation.LastUpdateTime.Time, time.Second)

			// Verify LastErrors were set correctly
			assert.Equal(t, tt.lastErrors, obj.lastErrors)
		})
	}
}
