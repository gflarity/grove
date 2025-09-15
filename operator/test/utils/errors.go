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
	"testing"

	groveerr "github.com/NVIDIA/grove/operator/internal/errors"

	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var (
	// TestAPIInternalErr is an API internal server error meant to be used to mimic HTTP response code 500 for tests.
	TestAPIInternalErr = apierrors.NewInternalError(errors.New("fake internal error"))
)

// CheckGroveError checks that an actual error is a Grove error and further checks its underline cause, error code and operation.
func CheckGroveError(t *testing.T, expectedError *groveerr.GroveError, actualErr error) {
	if expectedError == nil {
		panic("expectedError cannot be nil")
	}
	assert.Error(t, actualErr)
	var groveErr *groveerr.GroveError
	assert.True(t, errors.As(actualErr, &groveErr))
	assert.Equal(t, expectedError.Code, groveErr.Code)
	// Compare error messages instead of using errors.Is for exact matching
	if expectedError.Cause != nil && groveErr.Cause != nil {
		assert.Equal(t, expectedError.Cause.Error(), groveErr.Cause.Error())
	} else {
		assert.Equal(t, expectedError.Cause, groveErr.Cause)
	}
	assert.Equal(t, expectedError.Operation, groveErr.Operation)
}
