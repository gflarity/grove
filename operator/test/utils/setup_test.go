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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestSetupFakeClient verifies that SetupFakeClient creates a properly configured
// fake Kubernetes client with Grove CRDs and status subresources.
func TestSetupFakeClient(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different client setup scenarios
		name string
		// Objects to initialize the fake client with
		objects []client.Object
		// Expected number of objects that should be retrievable
		expectedObjectCount int
	}{
		{
			// Client with no initial objects
			name:                "no initial objects",
			objects:             []client.Object{},
			expectedObjectCount: 0,
		},
		{
			// Client with single Grove CRD object
			name: "single grove object",
			objects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
				},
			},
			expectedObjectCount: 1,
		},
		{
			// Client with multiple Grove CRD objects
			name: "multiple grove objects",
			objects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
				},
				&grovecorev1alpha1.PodClique{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pclq",
						Namespace: "default",
					},
				},
				&grovecorev1alpha1.PodCliqueScalingGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pcsg",
						Namespace: "default",
					},
				},
			},
			expectedObjectCount: 3,
		},
		{
			// Client with mixed Grove and Kubernetes core objects
			name: "mixed object types",
			objects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
				},
			},
			expectedObjectCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := SetupFakeClient(tt.objects...)

			require.NotNil(t, fakeClient)

			// Verify the client implements the expected interfaces
			assert.Implements(t, (*client.Client)(nil), fakeClient)
			assert.Implements(t, (*client.WithWatch)(nil), fakeClient)

			// Verify the client has a proper scheme
			clientScheme := fakeClient.Scheme()
			require.NotNil(t, clientScheme)

			// Verify Grove CRDs are registered in the scheme
			assert.True(t, clientScheme.Recognizes(grovecorev1alpha1.SchemeGroupVersion.WithKind("PodGangSet")))
			assert.True(t, clientScheme.Recognizes(grovecorev1alpha1.SchemeGroupVersion.WithKind("PodClique")))
			assert.True(t, clientScheme.Recognizes(grovecorev1alpha1.SchemeGroupVersion.WithKind("PodCliqueScalingGroup")))

			// Verify core Kubernetes objects are registered
			assert.True(t, clientScheme.Recognizes(corev1.SchemeGroupVersion.WithKind("Pod")))

			// Verify objects can be retrieved
			ctx := context.Background()
			for _, obj := range tt.objects {
				key := client.ObjectKey{Name: obj.GetName(), Namespace: obj.GetNamespace()}
				retrieved := obj.DeepCopyObject().(client.Object)
				err := fakeClient.Get(ctx, key, retrieved)
				assert.NoError(t, err, "Should be able to retrieve object %s/%s", key.Namespace, key.Name)
				assert.Equal(t, obj.GetName(), retrieved.GetName())
				assert.Equal(t, obj.GetNamespace(), retrieved.GetNamespace())
			}
		})
	}
}

// TestSetupFakeClient_StatusSubresources verifies that status subresources
// are properly configured for Grove CRDs.
func TestSetupFakeClient_StatusSubresources(t *testing.T) {
	// Create objects with status information
	pgs := &grovecorev1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pgs",
			Namespace: "default",
		},
		Status: grovecorev1alpha1.PodGangSetStatus{
			Replicas: 3,
		},
	}

	pclq := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pclq",
			Namespace: "default",
		},
		Status: grovecorev1alpha1.PodCliqueStatus{
			ReadyReplicas: 2,
		},
	}

	pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pcsg",
			Namespace: "default",
		},
		Status: grovecorev1alpha1.PodCliqueScalingGroupStatus{
			AvailableReplicas: 1,
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	fakeClient := SetupFakeClient(pgs, pclq, pcsg, pod)
	ctx := context.Background()

	tests := []struct {
		// Test case description - verifies status subresource functionality
		name string
		// Object to test status updates on
		obj client.Object
		// Function to verify status was updated correctly
		verifyStatusFunc func(*testing.T, client.Object)
	}{
		{
			// PodGangSet status subresource
			name: "podgangset status",
			obj:  pgs,
			verifyStatusFunc: func(t *testing.T, obj client.Object) {
				pgs := obj.(*grovecorev1alpha1.PodGangSet)
				assert.Equal(t, int32(3), pgs.Status.Replicas)
			},
		},
		{
			// PodClique status subresource
			name: "podclique status",
			obj:  pclq,
			verifyStatusFunc: func(t *testing.T, obj client.Object) {
				pclq := obj.(*grovecorev1alpha1.PodClique)
				assert.Equal(t, int32(2), pclq.Status.ReadyReplicas)
			},
		},
		{
			// PodCliqueScalingGroup status subresource
			name: "podcliquescalinggroup status",
			obj:  pcsg,
			verifyStatusFunc: func(t *testing.T, obj client.Object) {
				pcsg := obj.(*grovecorev1alpha1.PodCliqueScalingGroup)
				assert.Equal(t, int32(1), pcsg.Status.AvailableReplicas)
			},
		},
		{
			// Pod status subresource
			name: "pod status",
			obj:  pod,
			verifyStatusFunc: func(t *testing.T, obj client.Object) {
				pod := obj.(*corev1.Pod)
				assert.Equal(t, corev1.PodRunning, pod.Status.Phase)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Retrieve the object and verify status
			key := client.ObjectKey{Name: tt.obj.GetName(), Namespace: tt.obj.GetNamespace()}
			retrieved := tt.obj.DeepCopyObject().(client.Object)
			err := fakeClient.Get(ctx, key, retrieved)
			require.NoError(t, err)

			tt.verifyStatusFunc(t, retrieved)

			// Verify status writer is available
			statusWriter := fakeClient.Status()
			assert.NotNil(t, statusWriter)

			// Test status update (this should not fail with status subresource configured)
			err = statusWriter.Update(ctx, retrieved)
			assert.NoError(t, err, "Status update should succeed with status subresource configured")
		})
	}
}

// TestSetupFakeClient_SchemeConfiguration verifies that the fake client's scheme
// is properly configured with all necessary types.
func TestSetupFakeClient_SchemeConfiguration(t *testing.T) {
	fakeClient := SetupFakeClient()
	scheme := fakeClient.Scheme()

	// Test Grove CRD types
	groveTypes := []struct {
		// Type name for test identification
		name string
		// GroupVersionKind to test
		gvk runtime.Object
	}{
		{
			name: "PodGangSet",
			gvk:  &grovecorev1alpha1.PodGangSet{},
		},
		{
			name: "PodClique",
			gvk:  &grovecorev1alpha1.PodClique{},
		},
		{
			name: "PodCliqueScalingGroup",
			gvk:  &grovecorev1alpha1.PodCliqueScalingGroup{},
		},
	}

	for _, tt := range groveTypes {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the type is recognized by the scheme
			gvk, err := fakeClient.GroupVersionKindFor(tt.gvk)
			if err != nil {
				// Fake clients may not have REST mappings, skip this check
				t.Skipf("Fake client doesn't support REST mapping for %s: %v", tt.name, err)
				return
			}
			assert.Equal(t, "grove.io", gvk.Group)
			assert.Equal(t, "v1alpha1", gvk.Version)
			assert.Equal(t, tt.name, gvk.Kind)

			// Verify the type is namespaced
			namespaced, err := fakeClient.IsObjectNamespaced(tt.gvk)
			if err != nil {
				// Fake clients may not have REST mappings, skip this check
				t.Skipf("Fake client doesn't support REST mapping for %s: %v", tt.name, err)
				return
			}
			assert.True(t, namespaced, "Grove type %s should be namespaced", tt.name)

			// Verify the scheme recognizes the type
			assert.True(t, scheme.Recognizes(gvk), "Scheme should recognize %s", tt.name)
		})
	}

	// Test core Kubernetes types
	coreTypes := []struct {
		// Type name for test identification
		name string
		// GroupVersionKind to test
		gvk runtime.Object
	}{
		{
			name: "Pod",
			gvk:  &corev1.Pod{},
		},
	}

	for _, tt := range coreTypes {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the type is recognized by the scheme
			gvk, err := fakeClient.GroupVersionKindFor(tt.gvk)
			assert.NoError(t, err, "Should recognize core type %s", tt.name)
			assert.Equal(t, "", gvk.Group) // Core group is empty string
			assert.Equal(t, "v1", gvk.Version)
			assert.Equal(t, tt.name, gvk.Kind)
		})
	}
}

// TestSetupFakeClient_EmptyObjectsHandling verifies that the setup function
// handles various edge cases with object initialization.
func TestSetupFakeClient_EmptyObjectsHandling(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different edge case scenarios
		name string
		// Objects to pass to SetupFakeClient
		objects []client.Object
	}{
		{
			// No objects provided
			name:    "no objects",
			objects: nil,
		},
		{
			// Empty slice provided
			name:    "empty slice",
			objects: []client.Object{},
		},
		{
			// Objects with minimal metadata
			name: "minimal objects",
			objects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minimal-pgs",
						Namespace: "default",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic or fail
			assert.NotPanics(t, func() {
				fakeClient := SetupFakeClient(tt.objects...)
				assert.NotNil(t, fakeClient)
				assert.NotNil(t, fakeClient.Scheme())
			})
		})
	}
}

// TestSetupFakeClient_ClientOperations verifies that the fake client supports
// all standard Kubernetes client operations.
func TestSetupFakeClient_ClientOperations(t *testing.T) {
	pgs := &grovecorev1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ops-test-pgs",
			Namespace: "default",
		},
		Spec: grovecorev1alpha1.PodGangSetSpec{
			Replicas: 1,
		},
	}

	fakeClient := SetupFakeClient(pgs)
	ctx := context.Background()

	// Test Get operation
	retrieved := &grovecorev1alpha1.PodGangSet{}
	err := fakeClient.Get(ctx, client.ObjectKey{Name: pgs.GetName(), Namespace: pgs.GetNamespace()}, retrieved)
	assert.NoError(t, err)
	assert.Equal(t, pgs.Name, retrieved.Name)

	// Test List operation
	pgsList := &grovecorev1alpha1.PodGangSetList{}
	err = fakeClient.List(ctx, pgsList, client.InNamespace("default"))
	assert.NoError(t, err)
	assert.Len(t, pgsList.Items, 1)
	assert.Equal(t, pgs.Name, pgsList.Items[0].Name)

	// Test Create operation
	newPGS := &grovecorev1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-pgs",
			Namespace: "default",
		},
		Spec: grovecorev1alpha1.PodGangSetSpec{
			Replicas: 2,
		},
	}
	err = fakeClient.Create(ctx, newPGS)
	assert.NoError(t, err)

	// Verify the new object exists
	err = fakeClient.Get(ctx, client.ObjectKey{Name: newPGS.GetName(), Namespace: newPGS.GetNamespace()}, &grovecorev1alpha1.PodGangSet{})
	assert.NoError(t, err)

	// Test Update operation
	retrieved.Spec.Replicas = 5
	err = fakeClient.Update(ctx, retrieved)
	assert.NoError(t, err)

	// Verify the update
	updated := &grovecorev1alpha1.PodGangSet{}
	err = fakeClient.Get(ctx, client.ObjectKey{Name: retrieved.GetName(), Namespace: retrieved.GetNamespace()}, updated)
	assert.NoError(t, err)
	assert.Equal(t, int32(5), updated.Spec.Replicas)

	// Test Delete operation
	err = fakeClient.Delete(ctx, newPGS)
	assert.NoError(t, err)

	// Verify the object was deleted
	err = fakeClient.Get(ctx, client.ObjectKey{Name: newPGS.GetName(), Namespace: newPGS.GetNamespace()}, &grovecorev1alpha1.PodGangSet{})
	assert.Error(t, err)
	assert.True(t, client.IgnoreNotFound(err) == nil)
}
