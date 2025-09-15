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

// Package errors_test provides comprehensive tests for the Grove error handling system.
// This test suite validates error creation, wrapping, formatting, and conversion functionality
// to ensure robust error handling throughout the Grove operator.
package errors

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test helper functions

// createTestGroveError creates a GroveError for testing purposes with the given parameters.
func createTestGroveError(code grovecorev1alpha1.ErrorCode, operation, message string, cause error) *GroveError {
	return &GroveError{
		Code:       code,
		Cause:      cause,
		Operation:  operation,
		Message:    message,
		ObservedAt: time.Now().UTC(),
	}
}

// assertGroveErrorFields validates that a GroveError has the expected field values.
func assertGroveErrorFields(t *testing.T, err error, expectedCode grovecorev1alpha1.ErrorCode, expectedOperation, expectedMessage string, expectedCause error) {
	t.Helper()
	groveErr := &GroveError{}
	require.True(t, errors.As(err, &groveErr), "Error should be a GroveError")
	assert.Equal(t, expectedCode, groveErr.Code)
	assert.Equal(t, expectedOperation, groveErr.Operation)
	assert.Equal(t, expectedMessage, groveErr.Message)
	assert.Equal(t, expectedCause, groveErr.Cause)
	assert.False(t, groveErr.ObservedAt.IsZero(), "ObservedAt should be set")
}

// TestNew validates the New function creates GroveError instances correctly
// with proper field initialization and time handling.
func TestNew(t *testing.T) {
	testCases := []struct {
		name      string
		code      grovecorev1alpha1.ErrorCode
		operation string
		message   string
	}{
		{
			name:      "Standard error creation",
			code:      "ERR_TEST",
			operation: "test-operation",
			message:   "test message",
		},
		{
			name:      "Empty message",
			code:      "ERR_EMPTY_MSG",
			operation: "test-operation",
			message:   "",
		},
		{
			name:      "Empty operation",
			code:      "ERR_EMPTY_OP",
			operation: "",
			message:   "test message",
		},
		{
			name:      "Special error code - requeue after",
			code:      ErrCodeRequeueAfter,
			operation: "reconcile-operation",
			message:   "requeue needed",
		},
		{
			name:      "Special error code - continue reconcile and requeue",
			code:      ErrCodeContinueReconcileAndRequeue,
			operation: "sync-operation",
			message:   "continue and requeue",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			beforeTime := time.Now().UTC()
			err := New(tc.code, tc.operation, tc.message)
			afterTime := time.Now().UTC()

			// Validate it's a GroveError with correct fields
			groveErr := &GroveError{}
			require.True(t, errors.As(err, &groveErr))
			assert.Equal(t, tc.code, groveErr.Code)
			assert.Equal(t, tc.operation, groveErr.Operation)
			assert.Equal(t, tc.message, groveErr.Message)
			assert.Nil(t, groveErr.Cause, "New should not set a cause")

			// Validate time is set correctly and in UTC
			assert.True(t, groveErr.ObservedAt.After(beforeTime) || groveErr.ObservedAt.Equal(beforeTime))
			assert.True(t, groveErr.ObservedAt.Before(afterTime) || groveErr.ObservedAt.Equal(afterTime))
			assert.Equal(t, time.UTC, groveErr.ObservedAt.Location())
		})
	}
}

// TestWrapError validates that WrapError properly wraps errors with Grove metadata
// and handles edge cases including nil errors correctly.
func TestWrapError(t *testing.T) {
	testCases := []struct {
		name          string
		cause         error
		code          grovecorev1alpha1.ErrorCode
		operation     string
		message       string
		expectedError error
	}{
		{
			name:          "Standard error wrapping",
			cause:         fmt.Errorf("original error"),
			code:          "ERR_TEST",
			operation:     "test-operation",
			message:       "test message",
			expectedError: nil, // Will be validated as GroveError
		},
		{
			name:          "Nil error returns nil",
			cause:         nil,
			code:          "ERR_TEST",
			operation:     "test-operation",
			message:       "test message",
			expectedError: nil,
		},
		{
			name:          "Empty strings are preserved",
			cause:         fmt.Errorf("test error"),
			code:          "ERR_EMPTY",
			operation:     "",
			message:       "",
			expectedError: nil, // Will be validated as GroveError
		},
		{
			name:          "Wrapping another GroveError",
			cause:         createTestGroveError("ERR_INNER", "inner-op", "inner message", nil),
			code:          "ERR_OUTER",
			operation:     "outer-operation",
			message:       "outer message",
			expectedError: nil, // Will be validated as GroveError
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			beforeTime := time.Now().UTC()
			err := WrapError(tc.cause, tc.code, tc.operation, tc.message)
			afterTime := time.Now().UTC()

			if tc.cause == nil {
				assert.Nil(t, err, "WrapError should return nil when cause is nil")
				return
			}

			// Validate it's a GroveError with correct fields
			assertGroveErrorFields(t, err, tc.code, tc.operation, tc.message, tc.cause)

			// Validate time is set correctly
			groveErr := err.(*GroveError)
			assert.True(t, groveErr.ObservedAt.After(beforeTime) || groveErr.ObservedAt.Equal(beforeTime))
			assert.True(t, groveErr.ObservedAt.Before(afterTime) || groveErr.ObservedAt.Equal(afterTime))
			assert.Equal(t, time.UTC, groveErr.ObservedAt.Location())
		})
	}
}

// TestGroveError_Error validates the Error method formatting and handles edge cases
// including nil causes and empty fields correctly.
func TestGroveError_Error(t *testing.T) {
	testCases := []struct {
		name             string
		groveError       *GroveError
		expectedContains []string
		expectedFormat   string
	}{
		{
			name: "Error with cause",
			groveError: &GroveError{
				Code:      "ERR_TEST",
				Operation: "test-operation",
				Message:   "test message",
				Cause:     fmt.Errorf("original error"),
			},
			expectedContains: []string{"Operation: test-operation", "Code: ERR_TEST", "message: test message", "cause: original error"},
			expectedFormat:   "[Operation: test-operation, Code: ERR_TEST] message: test message, cause: original error",
		},
		{
			name: "Error without cause",
			groveError: &GroveError{
				Code:      "ERR_NO_CAUSE",
				Operation: "test-operation",
				Message:   "test message",
				Cause:     nil,
			},
			expectedContains: []string{"Operation: test-operation", "Code: ERR_NO_CAUSE", "message: test message"},
			expectedFormat:   "[Operation: test-operation, Code: ERR_NO_CAUSE] message: test message",
		},
		{
			name: "Error with empty fields",
			groveError: &GroveError{
				Code:      "",
				Operation: "",
				Message:   "",
				Cause:     nil,
			},
			expectedContains: []string{"Operation: ", "Code: ", "message: "},
			expectedFormat:   "[Operation: , Code: ] message: ",
		},
		{
			name: "Error with nested GroveError cause",
			groveError: &GroveError{
				Code:      "ERR_OUTER",
				Operation: "outer-op",
				Message:   "outer message",
				Cause: &GroveError{
					Code:      "ERR_INNER",
					Operation: "inner-op",
					Message:   "inner message",
				},
			},
			expectedContains: []string{"Operation: outer-op", "Code: ERR_OUTER", "outer message", "inner-op", "ERR_INNER"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errorMsg := tc.groveError.Error()

			// Check that all expected strings are present
			for _, expected := range tc.expectedContains {
				assert.Contains(t, errorMsg, expected, "Error message should contain: %s", expected)
			}

			// Check exact format if specified
			if tc.expectedFormat != "" {
				assert.Equal(t, tc.expectedFormat, errorMsg)
			}

			// Ensure error message is not empty
			assert.NotEmpty(t, errorMsg, "Error message should not be empty")
		})
	}
}

// TestSpecialErrorCodes validates that the predefined special error codes
// work correctly in error creation and wrapping scenarios.
func TestSpecialErrorCodes(t *testing.T) {
	testCases := []struct {
		name      string
		errorCode grovecorev1alpha1.ErrorCode
		operation string
		message   string
	}{
		{
			name:      "ErrCodeRequeueAfter constant",
			errorCode: ErrCodeRequeueAfter,
			operation: "reconcile-step",
			message:   "need to requeue after delay",
		},
		{
			name:      "ErrCodeContinueReconcileAndRequeue constant",
			errorCode: ErrCodeContinueReconcileAndRequeue,
			operation: "component-sync",
			message:   "continue with next component and requeue",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test with New()
			err := New(tc.errorCode, tc.operation, tc.message)
			assertGroveErrorFields(t, err, tc.errorCode, tc.operation, tc.message, nil)

			// Test with WrapError()
			cause := fmt.Errorf("underlying cause")
			wrappedErr := WrapError(cause, tc.errorCode, tc.operation, tc.message)
			assertGroveErrorFields(t, wrappedErr, tc.errorCode, tc.operation, tc.message, cause)

			// Validate error code string values
			groveErr := err.(*GroveError)
			assert.NotEmpty(t, string(groveErr.Code), "Error code should not be empty")
		})
	}

	// Test the actual constant values
	t.Run("Constant values", func(t *testing.T) {
		assert.Equal(t, grovecorev1alpha1.ErrorCode("ERR_REQUEUE_AFTER"), ErrCodeRequeueAfter)
		assert.Equal(t, grovecorev1alpha1.ErrorCode("ERR_CONTINUE_RECONCILE_AND_REQUEUE"), ErrCodeContinueReconcileAndRequeue)
	})
}

// TestTimeHandling validates that time fields are properly set and handled
// in UTC timezone with appropriate precision.
func TestTimeHandling(t *testing.T) {
	t.Run("New sets ObservedAt in UTC", func(t *testing.T) {
		beforeTime := time.Now().UTC()
		err := New("ERR_TIME_TEST", "time-operation", "time test")
		afterTime := time.Now().UTC()

		groveErr := err.(*GroveError)
		assert.Equal(t, time.UTC, groveErr.ObservedAt.Location())
		assert.True(t, groveErr.ObservedAt.After(beforeTime) || groveErr.ObservedAt.Equal(beforeTime))
		assert.True(t, groveErr.ObservedAt.Before(afterTime) || groveErr.ObservedAt.Equal(afterTime))
	})

	t.Run("WrapError sets ObservedAt in UTC", func(t *testing.T) {
		cause := fmt.Errorf("test cause")
		beforeTime := time.Now().UTC()
		err := WrapError(cause, "ERR_WRAP_TIME_TEST", "wrap-time-operation", "wrap time test")
		afterTime := time.Now().UTC()

		groveErr := err.(*GroveError)
		assert.Equal(t, time.UTC, groveErr.ObservedAt.Location())
		assert.True(t, groveErr.ObservedAt.After(beforeTime) || groveErr.ObservedAt.Equal(beforeTime))
		assert.True(t, groveErr.ObservedAt.Before(afterTime) || groveErr.ObservedAt.Equal(afterTime))
	})

	t.Run("Time precision is maintained", func(t *testing.T) {
		err1 := New("ERR_TIME1", "op1", "msg1")
		time.Sleep(1 * time.Millisecond) // Ensure different times
		err2 := New("ERR_TIME2", "op2", "msg2")

		groveErr1 := err1.(*GroveError)
		groveErr2 := err2.(*GroveError)
		assert.True(t, groveErr2.ObservedAt.After(groveErr1.ObservedAt), "Second error should have later timestamp")
	})
}

// TestMapToLastErrors validates the conversion of Grove errors to LastError slice
// and handles mixed error types correctly.
func TestMapToLastErrors(t *testing.T) {
	// Create test errors with known times for predictable testing
	fixedTime1 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	fixedTime2 := time.Date(2025, 1, 1, 12, 1, 0, 0, time.UTC)

	err1 := &GroveError{
		Code:       grovecorev1alpha1.ErrorCode("ERR_TEST1"),
		Cause:      fmt.Errorf("test-error1"),
		Operation:  "test-op",
		Message:    "test-message1",
		ObservedAt: fixedTime1,
	}
	err2 := &GroveError{
		Code:       grovecorev1alpha1.ErrorCode("ERR_TEST2"),
		Cause:      fmt.Errorf("test-error2"),
		Operation:  "test-op",
		Message:    "test-message2",
		ObservedAt: fixedTime2,
	}

	testCases := []struct {
		name             string
		errs             []error
		expectedLastErrs []grovecorev1alpha1.LastError
	}{
		{
			name:             "No errors",
			errs:             []error{},
			expectedLastErrs: []grovecorev1alpha1.LastError{},
		},
		{
			name: "Single Grove error",
			errs: []error{err1},
			expectedLastErrs: []grovecorev1alpha1.LastError{
				{Code: err1.Code, Description: err1.Error(), ObservedAt: metav1.NewTime(err1.ObservedAt)},
			},
		},
		{
			name: "Multiple Grove errors",
			errs: []error{err1, err2},
			expectedLastErrs: []grovecorev1alpha1.LastError{
				{Code: err1.Code, Description: err1.Error(), ObservedAt: metav1.NewTime(err1.ObservedAt)},
				{Code: err2.Code, Description: err2.Error(), ObservedAt: metav1.NewTime(err2.ObservedAt)},
			},
		},
		{
			name: "Mixed Grove errors and standard errors",
			errs: []error{err1, fmt.Errorf("standard-error"), err2, errors.New("another-standard-error")},
			expectedLastErrs: []grovecorev1alpha1.LastError{
				{Code: err1.Code, Description: err1.Error(), ObservedAt: metav1.NewTime(err1.ObservedAt)},
				{Code: err2.Code, Description: err2.Error(), ObservedAt: metav1.NewTime(err2.ObservedAt)},
			},
		},
		{
			name:             "Only standard errors",
			errs:             []error{fmt.Errorf("error1"), errors.New("error2")},
			expectedLastErrs: []grovecorev1alpha1.LastError{},
		},
		{
			name: "Nil errors in slice",
			errs: []error{err1, nil, err2},
			expectedLastErrs: []grovecorev1alpha1.LastError{
				{Code: err1.Code, Description: err1.Error(), ObservedAt: metav1.NewTime(err1.ObservedAt)},
				{Code: err2.Code, Description: err2.Error(), ObservedAt: metav1.NewTime(err2.ObservedAt)},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lastErrs := MapToLastErrors(tc.errs)
			assert.ElementsMatch(t, tc.expectedLastErrs, lastErrs)
			assert.Len(t, lastErrs, len(tc.expectedLastErrs))

			// Validate each LastError has proper fields
			for _, lastErr := range lastErrs {
				assert.NotEmpty(t, lastErr.Code, "LastError should have a code")
				assert.NotEmpty(t, lastErr.Description, "LastError should have a description")
				assert.False(t, lastErr.ObservedAt.IsZero(), "LastError should have ObservedAt time")
			}
		})
	}
}

// TestIntegrationScenarios tests realistic error handling scenarios
// that might occur in the Grove operator.
func TestIntegrationScenarios(t *testing.T) {
	t.Run("Controller reconciliation error chain", func(t *testing.T) {
		// Simulate a chain of errors that might occur during reconciliation
		k8sErr := fmt.Errorf("failed to get resource: not found")
		componentErr := WrapError(k8sErr, "ERR_COMPONENT_SYNC", "sync-podclique", "failed to sync podclique component")
		reconcileErr := WrapError(componentErr, ErrCodeRequeueAfter, "reconcile-podgangset", "reconciliation failed, will retry")

		// Validate the error chain
		groveErr := reconcileErr.(*GroveError)
		assert.Equal(t, ErrCodeRequeueAfter, groveErr.Code)
		assert.Equal(t, "reconcile-podgangset", groveErr.Operation)
		assert.Contains(t, groveErr.Error(), "ERR_COMPONENT_SYNC")
		assert.Contains(t, groveErr.Error(), "not found")

		// Test conversion to LastError
		lastErrs := MapToLastErrors([]error{reconcileErr})
		require.Len(t, lastErrs, 1)
		assert.Equal(t, ErrCodeRequeueAfter, lastErrs[0].Code)
		assert.Contains(t, lastErrs[0].Description, "reconciliation failed")
	})

	t.Run("Multiple component errors", func(t *testing.T) {
		// Simulate multiple components failing during reconciliation
		podErr := New("ERR_POD_CREATE", "create-pod", "failed to create pod")
		serviceErr := New("ERR_SERVICE_CREATE", "create-service", "failed to create service")
		configErr := WrapError(fmt.Errorf("validation failed"), "ERR_CONFIG_INVALID", "validate-config", "configuration is invalid")

		errors := []error{podErr, serviceErr, configErr}
		lastErrs := MapToLastErrors(errors)

		assert.Len(t, lastErrs, 3)
		codes := make([]grovecorev1alpha1.ErrorCode, len(lastErrs))
		for i, err := range lastErrs {
			codes[i] = err.Code
		}
		assert.Contains(t, codes, grovecorev1alpha1.ErrorCode("ERR_POD_CREATE"))
		assert.Contains(t, codes, grovecorev1alpha1.ErrorCode("ERR_SERVICE_CREATE"))
		assert.Contains(t, codes, grovecorev1alpha1.ErrorCode("ERR_CONFIG_INVALID"))
	})

	t.Run("Error formatting consistency", func(t *testing.T) {
		// Test that error formatting is consistent across different scenarios
		baseErr := fmt.Errorf("network timeout")
		wrappedErr := WrapError(baseErr, "ERR_NETWORK", "api-call", "failed to call Kubernetes API")

		errorMsg := wrappedErr.Error()

		// Validate consistent format
		assert.True(t, strings.HasPrefix(errorMsg, "[Operation: api-call, Code: ERR_NETWORK]"))
		assert.Contains(t, errorMsg, "message: failed to call Kubernetes API")
		assert.Contains(t, errorMsg, "cause: network timeout")

		// Ensure it implements error interface properly
		var err error = wrappedErr
		assert.Equal(t, errorMsg, err.Error())
	})
}
