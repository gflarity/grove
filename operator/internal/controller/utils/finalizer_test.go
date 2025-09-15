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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mockObject is a test implementation of client.Object for testing finalizer operations
type mockObject struct {
	metav1.ObjectMeta
	finalizers []string
}

func (m *mockObject) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (m *mockObject) DeepCopyObject() runtime.Object {
	return &mockObject{
		ObjectMeta: m.ObjectMeta,
		finalizers: append([]string(nil), m.finalizers...),
	}
}

func (m *mockObject) GetFinalizers() []string {
	return m.finalizers
}

func (m *mockObject) SetFinalizers(finalizers []string) {
	m.finalizers = finalizers
}

// mockWriter is a test implementation of client.Writer for testing patch operations
type mockWriter struct {
	mock.Mock
}

func (m *mockWriter) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *mockWriter) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *mockWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *mockWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := m.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

func (m *mockWriter) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

// TestAddAndPatchFinalizer tests the AddAndPatchFinalizer function which adds a finalizer
// to a Kubernetes object using merge-patch strategy with optimistic locking.
func TestAddAndPatchFinalizer(t *testing.T) {
	testCases := []struct {
		// Test case name describing the scenario being tested
		name string
		// The finalizer string to be added to the object
		finalizer string
		// Initial finalizers present on the object before the operation
		initialFinalizers []string
		// Expected finalizers on the object after the operation
		expectedFinalizers []string
		// Error that should be returned by the mock writer's Patch method
		patchError error
		// Expected error from AddAndPatchFinalizer function
		expectedError error
	}{
		{
			// Successfully adds a new finalizer to an object with no existing finalizers
			name:               "should add finalizer to object with no existing finalizers",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{},
			expectedFinalizers: []string{"test.finalizer/cleanup"},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Successfully adds a new finalizer to an object that already has other finalizers
			name:               "should add finalizer to object with existing finalizers",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{"existing.finalizer/other"},
			expectedFinalizers: []string{"existing.finalizer/other", "test.finalizer/cleanup"},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Does not duplicate a finalizer that already exists on the object
			name:               "should not duplicate existing finalizer",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{"test.finalizer/cleanup"},
			expectedFinalizers: []string{"test.finalizer/cleanup"},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Properly propagates patch errors from the underlying client
			name:               "should return error when patch fails",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{},
			expectedFinalizers: []string{"test.finalizer/cleanup"},
			patchError:         errors.New("patch failed"),
			expectedError:      errors.New("patch failed"),
		},
		{
			// Handles empty finalizer string gracefully
			name:               "should handle empty finalizer string",
			finalizer:          "",
			initialFinalizers:  []string{},
			expectedFinalizers: []string{""},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Handles very long finalizer names
			name:               "should handle long finalizer names",
			finalizer:          "very.long.finalizer.name.that.exceeds.normal.length/cleanup-operation-with-detailed-description",
			initialFinalizers:  []string{},
			expectedFinalizers: []string{"very.long.finalizer.name.that.exceeds.normal.length/cleanup-operation-with-detailed-description"},
			patchError:         nil,
			expectedError:      nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup test objects and mocks
			ctx := context.Background()
			obj := &mockObject{
				finalizers: tc.initialFinalizers,
			}
			writer := &mockWriter{}

			// Configure mock expectations
			writer.On("Patch", ctx, obj, mock.AnythingOfType("*client.mergeFromPatch"), mock.Anything).Return(tc.patchError)

			// Execute the function under test
			err := AddAndPatchFinalizer(ctx, writer, obj, tc.finalizer)

			// Verify results
			if tc.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			// Verify finalizers were modified correctly
			assert.Equal(t, tc.expectedFinalizers, obj.GetFinalizers())

			// Verify mock expectations
			writer.AssertExpectations(t)
		})
	}
}

// TestRemoveAndPatchFinalizer tests the RemoveAndPatchFinalizer function which removes a finalizer
// from a Kubernetes object using merge-patch strategy with optimistic locking.
func TestRemoveAndPatchFinalizer(t *testing.T) {
	testCases := []struct {
		// Test case name describing the scenario being tested
		name string
		// The finalizer string to be removed from the object
		finalizer string
		// Initial finalizers present on the object before the operation
		initialFinalizers []string
		// Expected finalizers on the object after the operation
		expectedFinalizers []string
		// Error that should be returned by the mock writer's Patch method
		patchError error
		// Expected error from RemoveAndPatchFinalizer function
		expectedError error
	}{
		{
			// Successfully removes an existing finalizer from the object
			name:               "should remove existing finalizer",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{"test.finalizer/cleanup"},
			expectedFinalizers: []string{},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Successfully removes one finalizer while leaving others intact
			name:               "should remove finalizer and keep others",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{"existing.finalizer/other", "test.finalizer/cleanup"},
			expectedFinalizers: []string{"existing.finalizer/other"},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Handles removal of non-existent finalizer gracefully without error
			name:               "should handle removal of non-existent finalizer",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{"existing.finalizer/other"},
			expectedFinalizers: []string{"existing.finalizer/other"},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Handles removal from object with no finalizers gracefully
			name:               "should handle removal from object with no finalizers",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{},
			expectedFinalizers: []string{},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Properly propagates patch errors from the underlying client
			name:               "should return error when patch fails",
			finalizer:          "test.finalizer/cleanup",
			initialFinalizers:  []string{"test.finalizer/cleanup"},
			expectedFinalizers: []string{},
			patchError:         errors.New("patch failed"),
			expectedError:      errors.New("patch failed"),
		},
		{
			// Handles empty finalizer string removal gracefully
			name:               "should handle empty finalizer string removal",
			finalizer:          "",
			initialFinalizers:  []string{""},
			expectedFinalizers: []string{},
			patchError:         nil,
			expectedError:      nil,
		},
		{
			// Handles removal from large finalizer list
			name:               "should handle removal from large finalizer list",
			finalizer:          "target.finalizer/cleanup",
			initialFinalizers:  []string{"a/1", "b/2", "c/3", "target.finalizer/cleanup", "d/4", "e/5"},
			expectedFinalizers: []string{"a/1", "b/2", "c/3", "d/4", "e/5"},
			patchError:         nil,
			expectedError:      nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup test objects and mocks
			ctx := context.Background()
			obj := &mockObject{
				finalizers: tc.initialFinalizers,
			}
			writer := &mockWriter{}

			// Configure mock expectations
			writer.On("Patch", ctx, obj, mock.AnythingOfType("*client.mergeFromPatch"), mock.Anything).Return(tc.patchError)

			// Execute the function under test
			err := RemoveAndPatchFinalizer(ctx, writer, obj, tc.finalizer)

			// Verify results
			if tc.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			// Verify finalizers were modified correctly
			assert.Equal(t, tc.expectedFinalizers, obj.GetFinalizers())

			// Verify mock expectations
			writer.AssertExpectations(t)
		})
	}
}
