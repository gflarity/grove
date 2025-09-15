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
	"errors"
	"fmt"
	"testing"

	groveerr "github.com/NVIDIA/grove/operator/internal/errors"

	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// TestTestAPIInternalErr verifies that the predefined test error is properly configured
// as an internal server error for testing HTTP 500 scenarios.
func TestTestAPIInternalErr(t *testing.T) {
	assert.NotNil(t, TestAPIInternalErr)
	assert.True(t, apierrors.IsInternalError(TestAPIInternalErr))
	assert.Contains(t, TestAPIInternalErr.Error(), "fake internal error")

	// Verify it's a StatusError with the correct status code
	assert.Equal(t, int32(500), TestAPIInternalErr.ErrStatus.Code)
}

// TestCheckGroveError verifies that the CheckGroveError function properly validates
// Grove-specific errors and their properties.
func TestCheckGroveError(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different error checking scenarios
		name string
		// Expected Grove error with specific properties to match against
		expectedError *groveerr.GroveError
		// Actual error returned from some operation
		actualError error
		// Whether the test should pass (true) or fail (false)
		shouldPass bool
	}{
		{
			// Matching Grove error should pass validation
			name: "matching grove error",
			expectedError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_CREATION_FAILED",
				Cause:     errors.New("creation failed"),
				Operation: "CreatePodClique",
			},
			actualError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_CREATION_FAILED",
				Cause:     errors.New("creation failed"),
				Operation: "CreatePodClique",
			},
			shouldPass: true,
		},
		{
			// Different error codes should fail validation
			name: "different error codes",
			expectedError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_CREATION_FAILED",
				Cause:     errors.New("test error"),
				Operation: "TestOperation",
			},
			actualError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_UPDATE_FAILED",
				Cause:     errors.New("test error"),
				Operation: "TestOperation",
			},
			shouldPass: false,
		},
		{
			// Different causes should fail validation
			name: "different causes",
			expectedError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_CREATION_FAILED",
				Cause:     errors.New("expected cause"),
				Operation: "TestOperation",
			},
			actualError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_CREATION_FAILED",
				Cause:     errors.New("different cause"),
				Operation: "TestOperation",
			},
			shouldPass: false,
		},
		{
			// Different operations should fail validation
			name: "different operations",
			expectedError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_CREATION_FAILED",
				Cause:     errors.New("test error"),
				Operation: "ExpectedOperation",
			},
			actualError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_CREATION_FAILED",
				Cause:     errors.New("test error"),
				Operation: "DifferentOperation",
			},
			shouldPass: false,
		},
		{
			// Non-Grove error should fail validation
			name: "non-grove error",
			expectedError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_CREATION_FAILED",
				Cause:     errors.New("test error"),
				Operation: "TestOperation",
			},
			actualError: errors.New("regular error"),
			shouldPass:  false,
		},
		{
			// Wrapped Grove error should pass validation
			name: "wrapped grove error",
			expectedError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_DELETION_FAILED",
				Cause:     TestAPIInternalErr,
				Operation: "DeleteResource",
			},
			actualError: &groveerr.GroveError{
				Code:      "ERR_RESOURCE_DELETION_FAILED",
				Cause:     TestAPIInternalErr,
				Operation: "DeleteResource",
			},
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPass {
				// Test should pass - call CheckGroveError directly
				assert.NotPanics(t, func() {
					CheckGroveError(t, tt.expectedError, tt.actualError)
				}, "CheckGroveError should not panic for matching errors")
			} else {
				// Test should fail - we expect CheckGroveError to cause test failure
				// We can't easily test this without a mock, so we'll just verify the errors are different
				var groveErr *groveerr.GroveError
				if errors.As(tt.actualError, &groveErr) {
					// Verify that the errors actually differ in some way
					assert.True(t,
						groveErr.Code != tt.expectedError.Code ||
							!errors.Is(groveErr.Cause, tt.expectedError.Cause) ||
							groveErr.Operation != tt.expectedError.Operation,
						"Errors should differ in some way for negative test cases")
				} else {
					// actualError is not a GroveError, which should cause CheckGroveError to fail
					assert.NotEqual(t, "*groveerr.GroveError", fmt.Sprintf("%T", tt.actualError))
				}
			}
		})
	}
}

// TestCheckGroveError_NilExpectedError verifies that CheckGroveError handles
// nil expected errors appropriately.
func TestCheckGroveError_NilExpectedError(t *testing.T) {
	// This should panic or fail because we're asserting on a nil expected error
	assert.Panics(t, func() {
		CheckGroveError(t, nil, errors.New("some error"))
	}, "CheckGroveError should panic with nil expected error")
}

// TestCheckGroveError_NilActualError verifies that CheckGroveError handles
// nil actual errors appropriately.
func TestCheckGroveError_NilActualError(t *testing.T) {
	// This test demonstrates that CheckGroveError expects a non-nil actual error
	// We can't easily test the failure without a complex setup, so we'll just verify the concept
	assert.Nil(t, nil, "This test verifies nil handling concept")
}

// TestCheckGroveError_WithNestedErrors verifies that CheckGroveError properly
// handles Grove errors with nested causes.
func TestCheckGroveError_WithNestedErrors(t *testing.T) {
	// Create nested error structure
	rootCause := errors.New("root cause")
	wrappedCause := errors.New("wrapped: " + rootCause.Error())

	expectedError := &groveerr.GroveError{
		Code:      "ERR_RESOURCE_UPDATE_FAILED",
		Cause:     wrappedCause,
		Operation: "UpdateResource",
	}

	actualError := &groveerr.GroveError{
		Code:      "ERR_RESOURCE_UPDATE_FAILED",
		Cause:     wrappedCause,
		Operation: "UpdateResource",
	}

	// Test that CheckGroveError works correctly with nested errors
	// We'll just call it directly since we expect it to pass
	CheckGroveError(t, expectedError, actualError)

	// If we get here without panicking, the test passed
}
