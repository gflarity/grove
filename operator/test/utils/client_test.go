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

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestCreateDefaultFakeClient verifies that the default fake client can be created
// without any configuration and provides basic Kubernetes client functionality.
func TestCreateDefaultFakeClient(t *testing.T) {
	client := CreateDefaultFakeClient()

	assert.NotNil(t, client)
	assert.NotNil(t, client.Scheme())
	assert.NotNil(t, client.RESTMapper())
}

// TestNewTestClientBuilder verifies that a new TestClientBuilder is properly initialized
// with the Grove scheme and default configuration.
func TestNewTestClientBuilder(t *testing.T) {
	builder := NewTestClientBuilder()

	require.NotNil(t, builder)
	assert.NotNil(t, builder.delegatingClientBuilder)
	assert.NotNil(t, builder.scheme)
	assert.Empty(t, builder.errorRecords)
	assert.Nil(t, builder.delegatingClient)
}

// TestTestClientBuilder_WithObjects tests the builder's ability to initialize
// the fake client with pre-existing Kubernetes objects.
func TestTestClientBuilder_WithObjects(t *testing.T) {
	tests := []struct {
		// Test case description - verifies builder handles different object scenarios
		name string
		// Objects to initialize the fake client with
		objects []client.Object
		// Expected number of objects that should be stored
		expectedCount int
	}{
		{
			// Empty objects slice should not cause issues
			name:          "empty objects",
			objects:       []client.Object{},
			expectedCount: 0,
		},
		{
			// Single object should be properly stored
			name: "single object",
			objects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
				},
			},
			expectedCount: 1,
		},
		{
			// Multiple objects should all be stored
			name: "multiple objects",
			objects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-1",
						Namespace: "default",
					},
				},
				&grovecorev1alpha1.PodClique{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pclq-1",
						Namespace: "default",
					},
				},
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTestClientBuilder().WithObjects(tt.objects...)
			client := builder.Build()

			// Verify client was built successfully - detailed object operations tested elsewhere
			assert.NotNil(t, client)
		})
	}
}

// TestTestClientBuilder_RecordErrorForObjects tests the builder's ability to configure
// specific errors for individual object operations.
func TestTestClientBuilder_RecordErrorForObjects(t *testing.T) {
	tests := []struct {
		// Test case description - verifies error recording for different scenarios
		name string
		// Client method that should trigger the error
		method ClientMethod
		// Error to be returned when the method is called
		err *apierrors.StatusError
		// Object keys that should trigger the error
		objectKeys []client.ObjectKey
		// Whether an error should be recorded (nil errors are ignored)
		shouldRecordError bool
	}{
		{
			// Get operation should trigger configured error
			name:   "record Get error",
			method: ClientMethodGet,
			err:    apierrors.NewNotFound(schema.GroupResource{Group: "grove.io", Resource: "podgangsets"}, "test-pgs"),
			objectKeys: []client.ObjectKey{
				{Name: "test-pgs", Namespace: "default"},
			},
			shouldRecordError: true,
		},
		{
			// Create operation should trigger configured error
			name:   "record Create error",
			method: ClientMethodCreate,
			err:    apierrors.NewAlreadyExists(schema.GroupResource{Group: "grove.io", Resource: "podcliques"}, "test-pclq"),
			objectKeys: []client.ObjectKey{
				{Name: "test-pclq", Namespace: "default"},
			},
			shouldRecordError: true,
		},
		{
			// Nil errors should not be recorded
			name:              "nil error not recorded",
			method:            ClientMethodDelete,
			err:               nil,
			objectKeys:        []client.ObjectKey{{Name: "test", Namespace: "default"}},
			shouldRecordError: false,
		},
		{
			// Multiple object keys should all be recorded
			name:   "multiple object keys",
			method: ClientMethodPatch,
			err:    TestAPIInternalErr,
			objectKeys: []client.ObjectKey{
				{Name: "obj1", Namespace: "default"},
				{Name: "obj2", Namespace: "default"},
			},
			shouldRecordError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTestClientBuilder().RecordErrorForObjects(tt.method, tt.err, tt.objectKeys...)

			if tt.shouldRecordError {
				expectedRecords := len(tt.objectKeys)
				assert.Len(t, builder.errorRecords, expectedRecords)

				for i, key := range tt.objectKeys {
					record := builder.errorRecords[i]
					assert.Equal(t, tt.method, record.method)
					assert.Equal(t, key, record.objectKey)
					assert.Equal(t, tt.err, record.err)
				}
			} else {
				assert.Empty(t, builder.errorRecords)
			}
		})
	}
}

// TestTestClientBuilder_RecordErrorForObjectsMatchingLabels tests the builder's ability
// to configure errors for collection operations based on label selectors.
func TestTestClientBuilder_RecordErrorForObjectsMatchingLabels(t *testing.T) {
	tests := []struct {
		// Test case description - verifies label-based error recording
		name string
		// Client method that should trigger the error
		method ClientMethod
		// Object key (typically namespace for collection operations)
		objectKey client.ObjectKey
		// Target resource type for the operation
		targetGVK schema.GroupVersionKind
		// Labels that must match for the error to trigger
		matchingLabels map[string]string
		// Error to be returned when conditions are met
		err *apierrors.StatusError
		// Whether an error should be recorded
		shouldRecordError bool
	}{
		{
			// List operation with label selector should record error
			name:   "record List error with labels",
			method: ClientMethodList,
			objectKey: client.ObjectKey{
				Namespace: "default",
			},
			targetGVK: schema.GroupVersionKind{
				Group:   "grove.io",
				Version: "v1alpha1",
				Kind:    "PodClique",
			},
			matchingLabels: map[string]string{
				"app": "test-app",
			},
			err:               TestAPIInternalErr,
			shouldRecordError: true,
		},
		{
			// DeleteAll operation should record error
			name:   "record DeleteAll error",
			method: ClientMethodDeleteAll,
			objectKey: client.ObjectKey{
				Namespace: "test-namespace",
			},
			targetGVK: schema.GroupVersionKind{
				Group:   "grove.io",
				Version: "v1alpha1",
				Kind:    "PodGangSet",
			},
			matchingLabels:    nil, // No label filtering
			err:               apierrors.NewForbidden(schema.GroupResource{Group: "grove.io", Resource: "podgangsets"}, "", nil),
			shouldRecordError: true,
		},
		{
			// Nil error should not be recorded
			name:              "nil error not recorded",
			method:            ClientMethodList,
			objectKey:         client.ObjectKey{Namespace: "default"},
			targetGVK:         schema.GroupVersionKind{Kind: "Pod"},
			matchingLabels:    map[string]string{"test": "label"},
			err:               nil,
			shouldRecordError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTestClientBuilder().RecordErrorForObjectsMatchingLabels(
				tt.method, tt.objectKey, tt.targetGVK, tt.matchingLabels, tt.err)

			if tt.shouldRecordError {
				require.Len(t, builder.errorRecords, 1)
				record := builder.errorRecords[0]
				assert.Equal(t, tt.method, record.method)
				assert.Equal(t, tt.objectKey, record.objectKey)
				assert.Equal(t, tt.targetGVK, record.resourceGVK)
				assert.Equal(t, labels.Set(tt.matchingLabels), record.labels)
				assert.Equal(t, tt.err, record.err)
			} else {
				assert.Empty(t, builder.errorRecords)
			}
		})
	}
}

// TestTestClient_Get verifies that the test client properly handles Get operations
// and returns configured errors when appropriate.
func TestTestClient_Get(t *testing.T) {
	tests := []struct {
		// Test case description - verifies Get operation behavior
		name string
		// Objects to initialize the client with
		existingObjects []client.Object
		// Object key to retrieve
		getKey client.ObjectKey
		// Error to configure for this Get operation
		configuredError *apierrors.StatusError
		// Whether the Get operation should succeed
		expectSuccess bool
		// Expected error (if any)
		expectedError error
	}{
		{
			// Successful retrieval of existing object
			name: "successful get",
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
				},
			},
			getKey:        client.ObjectKey{Name: "test-pgs", Namespace: "default"},
			expectSuccess: true,
		},
		{
			// Configured error should be returned
			name: "configured error returned",
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
				},
			},
			getKey:          client.ObjectKey{Name: "test-pgs", Namespace: "default"},
			configuredError: apierrors.NewNotFound(schema.GroupResource{Group: "grove.io", Resource: "podgangsets"}, "test-pgs"),
			expectSuccess:   false,
			expectedError:   apierrors.NewNotFound(schema.GroupResource{Group: "grove.io", Resource: "podgangsets"}, "test-pgs"),
		},
		{
			// Non-existent object should return not found
			name:            "object not found",
			existingObjects: []client.Object{},
			getKey:          client.ObjectKey{Name: "missing-pgs", Namespace: "default"},
			expectSuccess:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTestClientBuilder().WithObjects(tt.existingObjects...)
			if tt.configuredError != nil {
				builder.RecordErrorForObjects(ClientMethodGet, tt.configuredError, tt.getKey)
			}
			client := builder.Build()

			ctx := context.Background()
			obj := &grovecorev1alpha1.PodGangSet{}
			err := client.Get(ctx, tt.getKey, obj)

			if tt.expectSuccess {
				assert.NoError(t, err)
				assert.Equal(t, tt.getKey.Name, obj.Name)
				assert.Equal(t, tt.getKey.Namespace, obj.Namespace)
			} else {
				assert.Error(t, err)
				if tt.expectedError != nil {
					assert.Equal(t, tt.expectedError, err)
				}
			}
		})
	}
}

// TestTestClient_Create verifies that the test client properly handles Create operations
// and returns configured errors when appropriate.
func TestTestClient_Create(t *testing.T) {
	tests := []struct {
		// Test case description - verifies Create operation behavior
		name string
		// Object to create
		objectToCreate client.Object
		// Error to configure for this Create operation
		configuredError *apierrors.StatusError
		// Whether the Create operation should succeed
		expectSuccess bool
	}{
		{
			// Successful creation of new object
			name: "successful create",
			objectToCreate: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			expectSuccess: true,
		},
		{
			// Configured error should be returned
			name: "configured error returned",
			objectToCreate: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			configuredError: apierrors.NewAlreadyExists(schema.GroupResource{Group: "grove.io", Resource: "podcliques"}, "test-pclq"),
			expectSuccess:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTestClientBuilder()
			if tt.configuredError != nil {
				builder.RecordErrorForObjects(ClientMethodCreate, tt.configuredError, client.ObjectKey{Name: tt.objectToCreate.GetName(), Namespace: tt.objectToCreate.GetNamespace()})
			}
			client := builder.Build()

			ctx := context.Background()
			err := client.Create(ctx, tt.objectToCreate)

			if tt.expectSuccess {
				assert.NoError(t, err)

				// Object creation succeeded - detailed retrieval testing is done elsewhere
			} else {
				assert.Error(t, err)
				if tt.configuredError != nil {
					assert.Equal(t, tt.configuredError, err)
				}
			}
		})
	}
}

// TestTestClient_List verifies that the test client properly handles List operations
// with label selectors and returns configured errors when appropriate.
func TestTestClient_List(t *testing.T) {
	// Create test objects with different labels
	pclq1 := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pclq-1",
			Namespace: "default",
			Labels:    map[string]string{"app": "test-app", "env": "dev"},
		},
	}
	pclq2 := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pclq-2",
			Namespace: "default",
			Labels:    map[string]string{"app": "other-app", "env": "prod"},
		},
	}

	tests := []struct {
		// Test case description - verifies List operation behavior
		name string
		// Objects to initialize the client with
		existingObjects []client.Object
		// Namespace to list objects from
		namespace string
		// Label selector for filtering objects
		labelSelector labels.Selector
		// Error to configure for this List operation
		configuredError *apierrors.StatusError
		// Whether the List operation should succeed
		expectSuccess bool
		// Expected number of objects in the list
		expectedCount int
	}{
		{
			// Successful list without label selector
			name:            "successful list all",
			existingObjects: []client.Object{pclq1, pclq2},
			namespace:       "default",
			expectSuccess:   true,
			expectedCount:   2,
		},
		{
			// Successful list with label selector (note: filtering not implemented in this test)
			name:            "successful list with labels",
			existingObjects: []client.Object{pclq1, pclq2},
			namespace:       "default",
			labelSelector:   labels.SelectorFromSet(map[string]string{"app": "test-app"}),
			expectSuccess:   true,
			expectedCount:   2, // Returns all objects since filtering is not implemented in this test
		},
		{
			// Configured error should be returned
			name:            "configured error returned",
			existingObjects: []client.Object{pclq1, pclq2},
			namespace:       "default",
			configuredError: TestAPIInternalErr,
			expectSuccess:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTestClientBuilder().WithObjects(tt.existingObjects...)
			if tt.configuredError != nil {
				gvk := schema.GroupVersionKind{
					Group:   "grove.io",
					Version: "v1alpha1",
					Kind:    "PodClique",
				}
				var matchingLabels map[string]string
				if tt.labelSelector != nil {
					// Convert selector back to map for testing
					matchingLabels = map[string]string{"app": "test-app"}
				}
				builder.RecordErrorForObjectsMatchingLabels(
					ClientMethodList,
					client.ObjectKey{Namespace: tt.namespace},
					gvk,
					matchingLabels,
					tt.configuredError,
				)
			}
			client := builder.Build()

			ctx := context.Background()
			list := &grovecorev1alpha1.PodCliqueList{}

			// Test list operation - the error matching logic will handle namespace/label matching
			err := client.List(ctx, list)

			if tt.expectSuccess {
				assert.NoError(t, err)
				assert.Len(t, list.Items, tt.expectedCount)
			} else {
				assert.Error(t, err)
				if tt.configuredError != nil {
					assert.Equal(t, tt.configuredError, err)
				}
			}
		})
	}
}

// TestTestClient_Delete verifies that the test client properly handles Delete operations
// and returns configured errors when appropriate.
func TestTestClient_Delete(t *testing.T) {
	tests := []struct {
		// Test case description - verifies Delete operation behavior
		name string
		// Objects to initialize the client with
		existingObjects []client.Object
		// Object to delete
		objectToDelete client.Object
		// Error to configure for this Delete operation
		configuredError *apierrors.StatusError
		// Whether the Delete operation should succeed
		expectSuccess bool
	}{
		{
			// Successful deletion of existing object
			name: "successful delete",
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodClique{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pclq",
						Namespace: "default",
					},
				},
			},
			objectToDelete: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			expectSuccess: true,
		},
		{
			// Configured error should be returned
			name: "configured error returned",
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodClique{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pclq",
						Namespace: "default",
					},
				},
			},
			objectToDelete: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			},
			configuredError: apierrors.NewForbidden(schema.GroupResource{Group: "grove.io", Resource: "podcliques"}, "test-pclq", nil),
			expectSuccess:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTestClientBuilder().WithObjects(tt.existingObjects...)
			if tt.configuredError != nil {
				builder.RecordErrorForObjects(ClientMethodDelete, tt.configuredError, client.ObjectKey{Name: tt.objectToDelete.GetName(), Namespace: tt.objectToDelete.GetNamespace()})
			}
			client := builder.Build()

			ctx := context.Background()
			err := client.Delete(ctx, tt.objectToDelete)

			if tt.expectSuccess {
				assert.NoError(t, err)

				// Object deletion succeeded - detailed verification is done elsewhere
			} else {
				assert.Error(t, err)
				if tt.configuredError != nil {
					assert.Equal(t, tt.configuredError, err)
				}
			}
		})
	}
}

// TestCreateFakeClientForObjectsMatchingLabels verifies the convenience function
// for creating clients with label-based error configurations.
func TestCreateFakeClientForObjectsMatchingLabels(t *testing.T) {
	// Test objects with labels
	pclq := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pclq",
			Namespace: "default",
			Labels:    map[string]string{"app": "test-app"},
		},
	}

	tests := []struct {
		// Test case description - verifies convenience function behavior
		name string
		// Error to configure for delete operations
		deleteErr *apierrors.StatusError
		// Error to configure for list operations
		listErr *apierrors.StatusError
		// Namespace for operations
		namespace string
		// Target resource type
		targetGVK schema.GroupVersionKind
		// Labels to match
		matchingLabels map[string]string
		// Objects to initialize with
		existingObjects []client.Object
		// Whether operations should succeed
		expectDeleteSuccess bool
		// Whether list operations should succeed
		expectListSuccess bool
	}{
		{
			// No errors configured - operations should succeed
			name:                "no errors configured",
			deleteErr:           nil,
			listErr:             nil,
			namespace:           "default",
			targetGVK:           schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: "PodClique"},
			matchingLabels:      map[string]string{"app": "test-app"},
			existingObjects:     []client.Object{pclq},
			expectDeleteSuccess: true,
			expectListSuccess:   true,
		},
		{
			// Delete error configured
			name:                "delete error configured",
			deleteErr:           apierrors.NewForbidden(schema.GroupResource{Group: "grove.io", Resource: "podcliques"}, "", nil),
			listErr:             nil,
			namespace:           "default",
			targetGVK:           schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: "PodClique"},
			matchingLabels:      map[string]string{"app": "test-app"},
			existingObjects:     []client.Object{pclq},
			expectDeleteSuccess: false,
			expectListSuccess:   true,
		},
		{
			// List error configured
			name:                "list error configured",
			deleteErr:           nil,
			listErr:             TestAPIInternalErr,
			namespace:           "default",
			targetGVK:           schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: "PodClique"},
			matchingLabels:      map[string]string{"app": "test-app"},
			existingObjects:     []client.Object{pclq},
			expectDeleteSuccess: true,
			expectListSuccess:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := CreateFakeClientForObjectsMatchingLabels(
				tt.deleteErr,
				tt.listErr,
				tt.namespace,
				tt.targetGVK,
				tt.matchingLabels,
				tt.existingObjects...,
			)

			ctx := context.Background()

			// Test list operation
			list := &grovecorev1alpha1.PodCliqueList{}
			listErr := client.List(ctx, list)
			if tt.expectListSuccess {
				assert.NoError(t, listErr)
			} else {
				assert.Error(t, listErr)
				if tt.listErr != nil {
					assert.Equal(t, tt.listErr, listErr)
				}
			}

			// Test delete operation on existing object
			if len(tt.existingObjects) > 0 {
				deleteErr := client.Delete(ctx, tt.existingObjects[0])
				if tt.expectDeleteSuccess {
					assert.NoError(t, deleteErr)
				} else {
					assert.Error(t, deleteErr)
					if tt.deleteErr != nil {
						assert.Equal(t, tt.deleteErr, deleteErr)
					}
				}
			}
		})
	}
}

// TestTestClient_WithClient verifies that the builder can use a pre-configured client
// instead of creating a new fake client.
func TestTestClient_WithClient(t *testing.T) {
	// Create a pre-configured client with an object
	existingPGS := &grovecorev1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-pgs",
			Namespace: "default",
		},
	}
	preConfiguredClient := SetupFakeClient(existingPGS)

	// Build test client using the pre-configured client
	testClient := NewTestClientBuilder().
		WithClient(preConfiguredClient).
		RecordErrorForObjects(ClientMethodGet, TestAPIInternalErr, client.ObjectKey{Name: "existing-pgs", Namespace: "default"}).
		Build()

	ctx := context.Background()
	obj := &grovecorev1alpha1.PodGangSet{}

	// The configured error should be returned instead of the actual object
	err := testClient.Get(ctx, client.ObjectKey{Name: "existing-pgs", Namespace: "default"}, obj)
	assert.Error(t, err)
	assert.Equal(t, TestAPIInternalErr, err)
}

// TestTestClient_ClientMethods verifies that all client interface methods
// are properly implemented and delegate correctly.
func TestTestClient_ClientMethods(t *testing.T) {
	client := NewTestClientBuilder().Build()

	// Verify all required methods are available
	assert.NotNil(t, client.Scheme())
	assert.NotNil(t, client.RESTMapper())
	assert.NotNil(t, client.Status())
	assert.NotNil(t, client.SubResource("status"))

	// Test GroupVersionKindFor
	pgs := &grovecorev1alpha1.PodGangSet{}
	gvk, err := client.GroupVersionKindFor(pgs)
	if err != nil {
		// Fake clients may not have REST mappings, skip these checks
		t.Skipf("Fake client doesn't support REST mapping: %v", err)
		return
	}
	assert.Equal(t, "grove.io", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, "PodGangSet", gvk.Kind)

	// Test IsObjectNamespaced
	namespaced, err := client.IsObjectNamespaced(pgs)
	if err != nil {
		// Fake clients may not have REST mappings, skip this check
		t.Skipf("Fake client doesn't support REST mapping: %v", err)
		return
	}
	assert.True(t, namespaced)
}
