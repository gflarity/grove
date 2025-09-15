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
	"testing"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	groveschedulerv1alpha1 "github.com/NVIDIA/grove/scheduler/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestGetPodGangSelectorLabels validates the label selector creation for PodGang resources.
// It ensures that the correct labels are generated for listing PodGangs managed by a PodGangSet.
func TestGetPodGangSelectorLabels(t *testing.T) {
	tests := []struct {
		// name describes the test case scenario
		name string
		// objMeta is the ObjectMeta of the PodGangSet that manages the PodGangs
		objMeta metav1.ObjectMeta
		// expectedLabels contains the complete set of labels that should be returned
		expectedLabels map[string]string
	}{
		{
			// Test with basic PodGangSet metadata containing only name
			name: "basic podgangset metadata",
			objMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			expectedLabels: map[string]string{
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:    "test-pgs",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGang,
			},
		},
		{
			// Test with PodGangSet metadata that includes existing labels (should not affect result)
			name: "podgangset with existing labels",
			objMeta: metav1.ObjectMeta{
				Name:      "labeled-pgs",
				Namespace: "test-namespace",
				Labels: map[string]string{
					"custom-label": "custom-value",
				},
			},
			expectedLabels: map[string]string{
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:    "labeled-pgs",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGang,
			},
		},
		{
			// Test with empty PodGangSet name to validate edge case behavior
			name: "empty podgangset name",
			objMeta: metav1.ObjectMeta{
				Name:      "",
				Namespace: "default",
			},
			expectedLabels: map[string]string{
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:    "",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGang,
			},
		},
		{
			// Test with special characters in PodGangSet name
			name: "podgangset with special characters",
			objMeta: metav1.ObjectMeta{
				Name:      "test-pgs-123_special",
				Namespace: "special-ns",
			},
			expectedLabels: map[string]string{
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:    "test-pgs-123_special",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGang,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := GetPodGangSelectorLabels(tt.objMeta)

			// Verify that all expected labels are present with correct values
			assert.Equal(t, tt.expectedLabels, labels, "returned labels should match expected labels")

			// Verify specific label keys are present
			assert.Contains(t, labels, apicommon.LabelManagedByKey, "should contain managed-by label")
			assert.Contains(t, labels, apicommon.LabelPartOfKey, "should contain part-of label")
			assert.Contains(t, labels, apicommon.LabelComponentKey, "should contain component label")

			// Verify specific label values
			assert.Equal(t, apicommon.LabelManagedByValue, labels[apicommon.LabelManagedByKey], "managed-by label should have correct value")
			assert.Equal(t, tt.objMeta.Name, labels[apicommon.LabelPartOfKey], "part-of label should match PodGangSet name")
			assert.Equal(t, apicommon.LabelComponentNamePodGang, labels[apicommon.LabelComponentKey], "component label should be 'podgang'")
		})
	}
}

// TestGetPodGang validates the PodGang retrieval functionality.
// It tests both successful retrieval and error handling scenarios using a fake Kubernetes client.
func TestGetPodGang(t *testing.T) {
	// Create a test PodGang resource for successful retrieval tests
	testPodGang := &groveschedulerv1alpha1.PodGang{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-podgang",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"test-label": "test-value",
			},
		},
		Spec: groveschedulerv1alpha1.PodGangSpec{
			// PodGangSpec structure is from external scheduler API
			// Using empty spec for testing purposes
		},
	}

	// Setup runtime scheme for fake client
	scheme := runtime.NewScheme()
	err := groveschedulerv1alpha1.AddToScheme(scheme)
	require.NoError(t, err, "failed to add scheduler scheme")

	tests := []struct {
		// name describes the test case scenario
		name string
		// existingObjects are the objects that exist in the fake client before the test
		existingObjects []client.Object
		// podGangName is the name of the PodGang to retrieve
		podGangName string
		// namespace is the namespace where the PodGang should be located
		namespace string
		// expectError indicates whether the operation should return an error
		expectError bool
		// expectedErrorType specifies the type of error expected (if any)
		expectedErrorType string
		// expectedPodGang is the PodGang object that should be returned (if successful)
		expectedPodGang *groveschedulerv1alpha1.PodGang
	}{
		{
			// Test successful retrieval of an existing PodGang
			name:            "successful podgang retrieval",
			existingObjects: []client.Object{testPodGang},
			podGangName:     "test-podgang",
			namespace:       "test-namespace",
			expectError:     false,
			expectedPodGang: testPodGang,
		},
		{
			// Test error when PodGang does not exist
			name:              "podgang not found",
			existingObjects:   []client.Object{},
			podGangName:       "nonexistent-podgang",
			namespace:         "test-namespace",
			expectError:       true,
			expectedErrorType: "NotFound",
		},
		{
			// Test error when PodGang exists in different namespace
			name:              "podgang in wrong namespace",
			existingObjects:   []client.Object{testPodGang},
			podGangName:       "test-podgang",
			namespace:         "wrong-namespace",
			expectError:       true,
			expectedErrorType: "NotFound",
		},
		{
			// Test with empty PodGang name
			name:              "empty podgang name",
			existingObjects:   []client.Object{testPodGang},
			podGangName:       "",
			namespace:         "test-namespace",
			expectError:       true,
			expectedErrorType: "NotFound",
		},
		{
			// Test with empty namespace
			name:              "empty namespace",
			existingObjects:   []client.Object{testPodGang},
			podGangName:       "test-podgang",
			namespace:         "",
			expectError:       true,
			expectedErrorType: "NotFound",
		},
		{
			// Test retrieval with multiple PodGangs in the cluster (should return specific one)
			name: "multiple podgangs present",
			existingObjects: []client.Object{
				testPodGang,
				&groveschedulerv1alpha1.PodGang{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "another-podgang",
						Namespace: "test-namespace",
					},
				},
				&groveschedulerv1alpha1.PodGang{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-podgang",
						Namespace: "another-namespace",
					},
				},
			},
			podGangName:     "test-podgang",
			namespace:       "test-namespace",
			expectError:     false,
			expectedPodGang: testPodGang,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with existing objects
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjects...).
				Build()

			ctx := context.Background()

			// Execute the function under test
			result, err := GetPodGang(ctx, fakeClient, tt.podGangName, tt.namespace)

			if tt.expectError {
				// Verify that an error occurred
				assert.Error(t, err, "expected an error but got none")
				assert.Nil(t, result, "result should be nil when error occurs")

				// Verify the error type if specified
				if tt.expectedErrorType != "" {
					switch tt.expectedErrorType {
					case "NotFound":
						assert.True(t, apierrors.IsNotFound(err), "error should be NotFound type")
					default:
						t.Errorf("unknown expected error type: %s", tt.expectedErrorType)
					}
				}
			} else {
				// Verify successful operation
				assert.NoError(t, err, "expected no error but got: %v", err)
				require.NotNil(t, result, "result should not be nil")

				// Verify the returned PodGang matches expectations
				assert.Equal(t, tt.expectedPodGang.Name, result.Name, "PodGang name should match")
				assert.Equal(t, tt.expectedPodGang.Namespace, result.Namespace, "PodGang namespace should match")
				assert.Equal(t, tt.expectedPodGang.Labels, result.Labels, "PodGang labels should match")
				assert.Equal(t, tt.expectedPodGang.Spec, result.Spec, "PodGang spec should match")
			}
		})
	}
}

// TestGetPodGangWithClientError validates error handling when the client itself fails.
// This tests scenarios where the Kubernetes client encounters unexpected errors.
func TestGetPodGangWithClientError(t *testing.T) {
	// Create a client that will return an error
	errorClient := &errorClient{
		err: assert.AnError,
	}

	ctx := context.Background()
	result, err := GetPodGang(ctx, errorClient, "test-podgang", "test-namespace")

	// Verify that the client error is properly propagated
	assert.Error(t, err, "expected an error from the error client")
	assert.Nil(t, result, "result should be nil when client error occurs")
	assert.Equal(t, assert.AnError, err, "should return the client error")
}

// errorClient is a mock client that always returns an error for testing error handling scenarios.
type errorClient struct {
	// err is the error that will be returned by all client operations
	err error
}

// Get implements the client.Client interface and always returns the configured error.
func (c *errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return c.err
}

// List implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return c.err
}

// Create implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return c.err
}

// Delete implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return c.err
}

// Update implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return c.err
}

// Patch implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return c.err
}

// DeleteAllOf implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return c.err
}

// Status implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) Status() client.SubResourceWriter {
	return &errorStatusWriter{err: c.err}
}

// Scheme implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) Scheme() *runtime.Scheme {
	return runtime.NewScheme()
}

// RESTMapper implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) RESTMapper() meta.RESTMapper {
	return nil
}

// GroupVersionKindFor implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, c.err
}

// IsObjectNamespaced implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return false, c.err
}

// SubResource implements the client.Client interface (not used in tests but required for interface compliance).
func (c *errorClient) SubResource(subResource string) client.SubResourceClient {
	return &errorSubResourceClient{err: c.err}
}

// errorSubResourceClient is a mock sub-resource client that always returns an error.
type errorSubResourceClient struct {
	// err is the error that will be returned by all sub-resource operations
	err error
}

// Get implements the client.SubResourceClient interface and always returns the configured error.
func (c *errorSubResourceClient) Get(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceGetOption) error {
	return c.err
}

// Create implements the client.SubResourceClient interface and always returns the configured error.
func (c *errorSubResourceClient) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return c.err
}

// Update implements the client.SubResourceClient interface and always returns the configured error.
func (c *errorSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return c.err
}

// Patch implements the client.SubResourceClient interface and always returns the configured error.
func (c *errorSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return c.err
}

// errorStatusWriter is a mock status writer that always returns an error.
type errorStatusWriter struct {
	// err is the error that will be returned by all status operations
	err error
}

// Create implements the client.SubResourceWriter interface and always returns the configured error.
func (w *errorStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return w.err
}

// Update implements the client.SubResourceWriter interface and always returns the configured error.
func (w *errorStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return w.err
}

// Patch implements the client.SubResourceWriter interface and always returns the configured error.
func (w *errorStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return w.err
}
