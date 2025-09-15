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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestReconcileStepResult_Result verifies that the Result method correctly returns
// the controller result and joined errors from a ReconcileStepResult.
func TestReconcileStepResult_Result(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// stepResult is the input ReconcileStepResult to test
		stepResult ReconcileStepResult
		// expectedResult is the expected controller result
		expectedResult ctrl.Result
		// expectedError is the expected joined error (nil if no errors)
		expectedError error
	}{
		{
			// Tests result with no errors and no requeue
			name: "no errors no requeue",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{Requeue: false},
				errs:   []error{},
			},
			expectedResult: ctrl.Result{Requeue: false},
			expectedError:  nil,
		},
		{
			// Tests result with single error
			name: "single error",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{Requeue: true},
				errs:   []error{errors.New("test error")},
			},
			expectedResult: ctrl.Result{Requeue: true},
			expectedError:  errors.New("test error"),
		},
		{
			// Tests result with multiple errors that should be joined
			name: "multiple errors",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{Requeue: true},
				errs:   []error{errors.New("error 1"), errors.New("error 2")},
			},
			expectedResult: ctrl.Result{Requeue: true},
			expectedError:  errors.Join(errors.New("error 1"), errors.New("error 2")),
		},
		{
			// Tests result with requeue after duration
			name: "requeue after duration",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{RequeueAfter: 5 * time.Second},
				errs:   []error{},
			},
			expectedResult: ctrl.Result{RequeueAfter: 5 * time.Second},
			expectedError:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.stepResult.Result()
			assert.Equal(t, tt.expectedResult, result)
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestReconcileStepResult_NeedsRequeue verifies that the NeedsRequeue method correctly
// identifies when a reconcile step needs to be requeued based on errors or requeue flags.
func TestReconcileStepResult_NeedsRequeue(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// stepResult is the input ReconcileStepResult to test
		stepResult ReconcileStepResult
		// expectedNeedsRequeue indicates whether requeue should be needed
		expectedNeedsRequeue bool
	}{
		{
			// Tests case where no requeue is needed
			name: "no requeue needed",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{Requeue: false},
				errs:   []error{},
			},
			expectedNeedsRequeue: false,
		},
		{
			// Tests case where requeue is needed due to explicit requeue flag
			name: "requeue flag set",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{Requeue: true},
				errs:   []error{},
			},
			expectedNeedsRequeue: true,
		},
		{
			// Tests case where requeue is needed due to RequeueAfter duration
			name: "requeue after duration",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{RequeueAfter: 10 * time.Second},
				errs:   []error{},
			},
			expectedNeedsRequeue: true,
		},
		{
			// Tests case where requeue is needed due to errors
			name: "has errors",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{Requeue: false},
				errs:   []error{errors.New("test error")},
			},
			expectedNeedsRequeue: true,
		},
		{
			// Tests case where requeue is needed due to both errors and requeue flag
			name: "has errors and requeue flag",
			stepResult: ReconcileStepResult{
				result: ctrl.Result{Requeue: true},
				errs:   []error{errors.New("test error")},
			},
			expectedNeedsRequeue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needsRequeue := tt.stepResult.NeedsRequeue()
			assert.Equal(t, tt.expectedNeedsRequeue, needsRequeue)
		})
	}
}

// TestReconcileStepResult_HasErrors verifies that the HasErrors method correctly
// identifies when a ReconcileStepResult contains errors.
func TestReconcileStepResult_HasErrors(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// stepResult is the input ReconcileStepResult to test
		stepResult ReconcileStepResult
		// expectedHasErrors indicates whether errors should be present
		expectedHasErrors bool
	}{
		{
			// Tests case with no errors
			name: "no errors",
			stepResult: ReconcileStepResult{
				errs: []error{},
			},
			expectedHasErrors: false,
		},
		{
			// Tests case with nil errors slice
			name: "nil errors slice",
			stepResult: ReconcileStepResult{
				errs: nil,
			},
			expectedHasErrors: false,
		},
		{
			// Tests case with single error
			name: "single error",
			stepResult: ReconcileStepResult{
				errs: []error{errors.New("test error")},
			},
			expectedHasErrors: true,
		},
		{
			// Tests case with multiple errors
			name: "multiple errors",
			stepResult: ReconcileStepResult{
				errs: []error{errors.New("error 1"), errors.New("error 2")},
			},
			expectedHasErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasErrors := tt.stepResult.HasErrors()
			assert.Equal(t, tt.expectedHasErrors, hasErrors)
		})
	}
}

// TestReconcileStepResult_GetErrors verifies that the GetErrors method correctly
// returns the errors slice from a ReconcileStepResult.
func TestReconcileStepResult_GetErrors(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// stepResult is the input ReconcileStepResult to test
		stepResult ReconcileStepResult
		// expectedErrors are the expected errors that should be returned
		expectedErrors []error
	}{
		{
			// Tests case with no errors
			name: "no errors",
			stepResult: ReconcileStepResult{
				errs: []error{},
			},
			expectedErrors: []error{},
		},
		{
			// Tests case with nil errors slice
			name: "nil errors slice",
			stepResult: ReconcileStepResult{
				errs: nil,
			},
			expectedErrors: nil,
		},
		{
			// Tests case with single error
			name: "single error",
			stepResult: ReconcileStepResult{
				errs: []error{errors.New("test error")},
			},
			expectedErrors: []error{errors.New("test error")},
		},
		{
			// Tests case with multiple errors
			name: "multiple errors",
			stepResult: ReconcileStepResult{
				errs: []error{errors.New("error 1"), errors.New("error 2")},
			},
			expectedErrors: []error{errors.New("error 1"), errors.New("error 2")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := tt.stepResult.GetErrors()
			if tt.expectedErrors == nil {
				assert.Nil(t, errors)
			} else {
				assert.Equal(t, len(tt.expectedErrors), len(errors))
				for i, expectedErr := range tt.expectedErrors {
					assert.Equal(t, expectedErr.Error(), errors[i].Error())
				}
			}
		})
	}
}

// TestReconcileStepResult_GetDescription verifies that the GetDescription method
// correctly returns the description from a ReconcileStepResult.
func TestReconcileStepResult_GetDescription(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// stepResult is the input ReconcileStepResult to test
		stepResult ReconcileStepResult
		// expectedDescription is the expected description string
		expectedDescription string
	}{
		{
			// Tests case with empty description
			name: "empty description",
			stepResult: ReconcileStepResult{
				description: "",
			},
			expectedDescription: "",
		},
		{
			// Tests case with non-empty description
			name: "non-empty description",
			stepResult: ReconcileStepResult{
				description: "test description",
			},
			expectedDescription: "test description",
		},
		{
			// Tests case with detailed description
			name: "detailed description",
			stepResult: ReconcileStepResult{
				description: "Failed to create resource due to validation error",
			},
			expectedDescription: "Failed to create resource due to validation error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			description := tt.stepResult.GetDescription()
			assert.Equal(t, tt.expectedDescription, description)
		})
	}
}

// TestDoNotRequeue verifies that the DoNotRequeue helper function creates
// a ReconcileStepResult that does not requeue the reconciliation.
func TestDoNotRequeue(t *testing.T) {
	result := DoNotRequeue()

	// Should not continue reconcile
	assert.False(t, result.continueReconcile)
	// Should not requeue
	assert.False(t, result.result.Requeue)
	assert.Equal(t, time.Duration(0), result.result.RequeueAfter)
	// Should have no errors
	assert.False(t, result.HasErrors())
	assert.Empty(t, result.GetErrors())
	// Should not need requeue
	assert.False(t, result.NeedsRequeue())
	// Should have empty description
	assert.Empty(t, result.GetDescription())
}

// TestRecordErrorAndDoNotRequeue verifies that the RecordErrorAndDoNotRequeue helper
// function creates a ReconcileStepResult that records errors but does not requeue.
func TestRecordErrorAndDoNotRequeue(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// description is the error description to record
		description string
		// errors are the errors to record
		errors []error
		// expectedErrorCount is the expected number of errors
		expectedErrorCount int
	}{
		{
			// Tests recording a single error with description
			name:               "single error with description",
			description:        "failed to create resource",
			errors:             []error{errors.New("resource already exists")},
			expectedErrorCount: 1,
		},
		{
			// Tests recording multiple errors
			name:               "multiple errors",
			description:        "validation failed",
			errors:             []error{errors.New("invalid name"), errors.New("invalid namespace")},
			expectedErrorCount: 2,
		},
		{
			// Tests recording no errors (edge case)
			name:               "no errors",
			description:        "completed with warnings",
			errors:             []error{},
			expectedErrorCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RecordErrorAndDoNotRequeue(tt.description, tt.errors...)

			// Should not continue reconcile
			assert.False(t, result.continueReconcile)
			// Should not requeue
			assert.False(t, result.result.Requeue)
			assert.Equal(t, time.Duration(0), result.result.RequeueAfter)
			// Should have expected description
			assert.Equal(t, tt.description, result.GetDescription())
			// Should have expected errors
			assert.Equal(t, tt.expectedErrorCount, len(result.GetErrors()))
			assert.Equal(t, tt.expectedErrorCount > 0, result.HasErrors())
			// Should need requeue only if there are errors
			assert.Equal(t, tt.expectedErrorCount > 0, result.NeedsRequeue())
		})
	}
}

// TestContinueReconcile verifies that the ContinueReconcile helper function
// creates a ReconcileStepResult that continues the reconciliation to the next step.
func TestContinueReconcile(t *testing.T) {
	result := ContinueReconcile()

	// Should continue reconcile
	assert.True(t, result.continueReconcile)
	// Should not requeue (default zero value)
	assert.False(t, result.result.Requeue)
	assert.Equal(t, time.Duration(0), result.result.RequeueAfter)
	// Should have no errors
	assert.False(t, result.HasErrors())
	assert.Empty(t, result.GetErrors())
	// Should not need requeue
	assert.False(t, result.NeedsRequeue())
	// Should have empty description
	assert.Empty(t, result.GetDescription())
}

// TestReconcileWithErrors verifies that the ReconcileWithErrors helper function
// creates a ReconcileStepResult that re-queues the reconciliation with errors.
func TestReconcileWithErrors(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// description is the error description to record
		description string
		// errors are the errors to record
		errors []error
		// expectedErrorCount is the expected number of errors
		expectedErrorCount int
	}{
		{
			// Tests reconciling with a single error
			name:               "single error",
			description:        "temporary failure",
			errors:             []error{errors.New("connection timeout")},
			expectedErrorCount: 1,
		},
		{
			// Tests reconciling with multiple errors
			name:               "multiple errors",
			description:        "multiple failures occurred",
			errors:             []error{errors.New("network error"), errors.New("auth error")},
			expectedErrorCount: 2,
		},
		{
			// Tests reconciling with no errors (edge case)
			name:               "no errors but requeue",
			description:        "requeue requested",
			errors:             []error{},
			expectedErrorCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReconcileWithErrors(tt.description, tt.errors...)

			// Should not continue reconcile
			assert.False(t, result.continueReconcile)
			// Should requeue
			assert.True(t, result.result.Requeue)
			assert.Equal(t, time.Duration(0), result.result.RequeueAfter)
			// Should have expected description
			assert.Equal(t, tt.description, result.GetDescription())
			// Should have expected errors
			assert.Equal(t, tt.expectedErrorCount, len(result.GetErrors()))
			assert.Equal(t, tt.expectedErrorCount > 0, result.HasErrors())
			// Should always need requeue (due to Requeue flag)
			assert.True(t, result.NeedsRequeue())
		})
	}
}

// TestReconcileAfter verifies that the ReconcileAfter helper function creates
// a ReconcileStepResult that re-queues the reconciliation after a specified duration.
func TestReconcileAfter(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// duration is the time to wait before requeuing
		duration time.Duration
		// description is the reason for the delayed requeue
		description string
	}{
		{
			// Tests requeuing after a short duration
			name:        "short duration",
			duration:    5 * time.Second,
			description: "waiting for resource to be ready",
		},
		{
			// Tests requeuing after a longer duration
			name:        "long duration",
			duration:    5 * time.Minute,
			description: "periodic health check",
		},
		{
			// Tests requeuing with zero duration (edge case)
			name:        "zero duration",
			duration:    0,
			description: "immediate requeue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReconcileAfter(tt.duration, tt.description)

			// Should not continue reconcile
			assert.False(t, result.continueReconcile)
			// Should not have immediate requeue flag
			assert.False(t, result.result.Requeue)
			// Should have expected requeue duration
			assert.Equal(t, tt.duration, result.result.RequeueAfter)
			// Should have expected description
			assert.Equal(t, tt.description, result.GetDescription())
			// Should have no errors
			assert.False(t, result.HasErrors())
			assert.Empty(t, result.GetErrors())
			// Should need requeue if duration > 0
			assert.Equal(t, tt.duration > 0, result.NeedsRequeue())
		})
	}
}

// TestShortCircuitReconcileFlow verifies that the ShortCircuitReconcileFlow function
// correctly determines when the reconcile flow should be short-circuited.
func TestShortCircuitReconcileFlow(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// stepResult is the input ReconcileStepResult to evaluate
		stepResult ReconcileStepResult
		// expectedShortCircuit indicates whether the flow should be short-circuited
		expectedShortCircuit bool
	}{
		{
			// Tests case where reconcile should continue (no short-circuit)
			name: "continue reconcile",
			stepResult: ReconcileStepResult{
				continueReconcile: true,
			},
			expectedShortCircuit: false,
		},
		{
			// Tests case where reconcile should be short-circuited
			name: "short circuit reconcile",
			stepResult: ReconcileStepResult{
				continueReconcile: false,
			},
			expectedShortCircuit: true,
		},
		{
			// Tests case using DoNotRequeue helper
			name:                 "do not requeue result",
			stepResult:           DoNotRequeue(),
			expectedShortCircuit: true,
		},
		{
			// Tests case using ContinueReconcile helper
			name:                 "continue reconcile result",
			stepResult:           ContinueReconcile(),
			expectedShortCircuit: false,
		},
		{
			// Tests case using ReconcileWithErrors helper
			name:                 "reconcile with errors result",
			stepResult:           ReconcileWithErrors("test error", errors.New("test")),
			expectedShortCircuit: true,
		},
		{
			// Tests case using ReconcileAfter helper
			name:                 "reconcile after result",
			stepResult:           ReconcileAfter(5*time.Second, "wait for resource"),
			expectedShortCircuit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldShortCircuit := ShortCircuitReconcileFlow(tt.stepResult)
			assert.Equal(t, tt.expectedShortCircuit, shouldShortCircuit)
		})
	}
}

// TestReconcileStepFn verifies that the ReconcileStepFn type can be used correctly
// with different Grove custom resource types.
func TestReconcileStepFn(t *testing.T) {
	// Test with PodGangSet type
	var podGangSetStepFn ReconcileStepFn[grovecorev1alpha1.PodGangSet] = func(ctx context.Context, log logr.Logger, obj *grovecorev1alpha1.PodGangSet) ReconcileStepResult {
		return ContinueReconcile()
	}

	// Test with PodClique type
	var podCliqueStepFn ReconcileStepFn[grovecorev1alpha1.PodClique] = func(ctx context.Context, log logr.Logger, obj *grovecorev1alpha1.PodClique) ReconcileStepResult {
		return DoNotRequeue()
	}

	// Test with PodCliqueScalingGroup type
	var podCliqueScalingGroupStepFn ReconcileStepFn[grovecorev1alpha1.PodCliqueScalingGroup] = func(ctx context.Context, log logr.Logger, obj *grovecorev1alpha1.PodCliqueScalingGroup) ReconcileStepResult {
		return ReconcileWithErrors("test error", errors.New("test"))
	}

	// Verify the functions can be called and return expected results
	ctx := context.Background()
	log := logr.Discard()

	// Test PodGangSet step function
	pgsResult := podGangSetStepFn(ctx, log, &grovecorev1alpha1.PodGangSet{})
	assert.True(t, pgsResult.continueReconcile)
	assert.False(t, pgsResult.NeedsRequeue())

	// Test PodClique step function
	pcResult := podCliqueStepFn(ctx, log, &grovecorev1alpha1.PodClique{})
	assert.False(t, pcResult.continueReconcile)
	assert.False(t, pcResult.NeedsRequeue())

	// Test PodCliqueScalingGroup step function
	pcsgResult := podCliqueScalingGroupStepFn(ctx, log, &grovecorev1alpha1.PodCliqueScalingGroup{})
	assert.False(t, pcsgResult.continueReconcile)
	assert.True(t, pcsgResult.NeedsRequeue())
	assert.True(t, pcsgResult.HasErrors())
}
