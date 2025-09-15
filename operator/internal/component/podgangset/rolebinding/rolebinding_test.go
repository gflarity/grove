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

package rolebinding

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestNew validates that the New function creates a proper RoleBinding operator instance.
// It ensures the operator is initialized with the correct client and scheme dependencies.
func TestNew(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// client is the Kubernetes client to be used by the operator
		client client.Client
		// scheme is the runtime scheme for Kubernetes objects
		scheme *runtime.Scheme
		// expectNil indicates whether the returned operator should be nil
		expectNil bool
	}{
		{
			// Tests successful creation of operator with valid dependencies.
			// Should return a non-nil operator instance with proper client and scheme.
			name:      "successful operator creation",
			client:    fake.NewClientBuilder().Build(),
			scheme:    runtime.NewScheme(),
			expectNil: false,
		},
		{
			// Tests operator creation with nil client.
			// Should still create operator instance as validation is not enforced in constructor.
			name:      "operator creation with nil client",
			client:    nil,
			scheme:    runtime.NewScheme(),
			expectNil: false,
		},
		{
			// Tests operator creation with nil scheme.
			// Should still create operator instance as validation is not enforced in constructor.
			name:      "operator creation with nil scheme",
			client:    fake.NewClientBuilder().Build(),
			scheme:    nil,
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operator := New(tt.client, tt.scheme)

			if tt.expectNil {
				assert.Nil(t, operator)
			} else {
				assert.NotNil(t, operator)

				// Verify the operator implements the expected interface
				assert.Implements(t, (*component.Operator[grovecorev1alpha1.PodGangSet])(nil), operator)
			}
		})
	}
}

// TestGetExistingResourceNames validates the GetExistingResourceNames method.
// It tests various scenarios including existing RoleBindings, missing resources, and error conditions.
func TestGetExistingResourceNames(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsObjMeta is the PodGangSet metadata used to query for RoleBindings
		pgsObjMeta metav1.ObjectMeta
		// existingObjects are the Kubernetes objects that exist in the fake client
		existingObjects []client.Object
		// expectedNames are the RoleBinding names expected to be returned
		expectedNames []string
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code if an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests scenario where no RoleBinding exists for the PodGangSet.
			// Should return empty list without error.
			name: "no existing rolebinding",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			existingObjects: []client.Object{},
			expectedNames:   []string{},
			expectError:     false,
		},
		{
			// Tests scenario where a RoleBinding exists and is controlled by the PodGangSet.
			// Should return the RoleBinding name in the result list.
			name: "existing rolebinding controlled by pgs",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			existingObjects: []client.Object{
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodRoleBindingName("test-pgs"),
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:               "PodGangSet",
								Name:               "test-pgs",
								UID:                "test-uid",
								Controller:         &[]bool{true}[0],
								BlockOwnerDeletion: &[]bool{true}[0],
							},
						},
					},
				},
			},
			expectedNames: []string{apicommon.GeneratePodRoleBindingName("test-pgs")},
			expectError:   false,
		},
		{
			// Tests scenario where a RoleBinding exists but is not controlled by the PodGangSet.
			// Should return empty list as the RoleBinding is not owned by this PodGangSet.
			name: "existing rolebinding not controlled by pgs",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			existingObjects: []client.Object{
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodRoleBindingName("test-pgs"),
						Namespace: "default",
						// No owner reference, so not controlled by PodGangSet
					},
				},
			},
			expectedNames: []string{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjects...).
				Build()

			operator := New(client, scheme)
			logger := logr.Discard()

			names, err := operator.GetExistingResourceNames(context.Background(), logger, tt.pgsObjMeta)

			if tt.expectError {
				assert.Error(t, err)
				var groveErr *groveerr.GroveError
				if assert.ErrorAs(t, err, &groveErr) {
					assert.Equal(t, tt.expectedErrorCode, groveErr.Code)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedNames, names)
			}
		})
	}
}

// TestSync validates the Sync method for creating and managing RoleBinding resources.
// It tests creation of new RoleBindings and skipping when they already exist.
func TestSync(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgs is the PodGangSet resource being synchronized
		pgs *grovecorev1alpha1.PodGangSet
		// existingObjects are the Kubernetes objects that exist in the fake client
		existingObjects []client.Object
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code if an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
		// validateFunc performs additional assertions on the result
		validateFunc func(t *testing.T, client client.Client, pgs *grovecorev1alpha1.PodGangSet)
	}{
		{
			// Tests creation of a new RoleBinding when none exists.
			// Should successfully create RoleBinding with proper configuration.
			name: "create new rolebinding",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid").
				Build(),
			existingObjects: []client.Object{},
			expectError:     false,
			validateFunc: func(t *testing.T, client client.Client, pgs *grovecorev1alpha1.PodGangSet) {
				// Verify RoleBinding was created
				roleBinding := &rbacv1.RoleBinding{}
				objectKey := getObjectKey(pgs.ObjectMeta)
				err := client.Get(context.Background(), objectKey, roleBinding)
				require.NoError(t, err)

				// Verify RoleBinding configuration
				assert.Equal(t, objectKey.Name, roleBinding.Name)
				assert.Equal(t, objectKey.Namespace, roleBinding.Namespace)
				assert.True(t, metav1.IsControlledBy(roleBinding, pgs))

				// Verify RoleRef
				expectedRoleRef := rbacv1.RoleRef{
					APIGroup: rbacv1.SchemeGroupVersion.Group,
					Kind:     "Role",
					Name:     apicommon.GeneratePodRoleName(pgs.Name),
				}
				assert.Equal(t, expectedRoleRef, roleBinding.RoleRef)

				// Verify Subjects
				expectedSubjects := []rbacv1.Subject{
					{
						APIGroup:  corev1.SchemeGroupVersion.Group,
						Kind:      "ServiceAccount",
						Name:      apicommon.GeneratePodServiceAccountName(pgs.Name),
						Namespace: pgs.Namespace,
					},
				}
				assert.Equal(t, expectedSubjects, roleBinding.Subjects)

				// Verify labels
				expectedLabels := getLabels(pgs.ObjectMeta)
				for key, expectedValue := range expectedLabels {
					assert.Equal(t, expectedValue, roleBinding.Labels[key])
				}
			},
		},
		{
			// Tests skipping creation when RoleBinding already exists.
			// Should not modify existing RoleBinding and return without error.
			name: "skip creation when rolebinding exists",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid").
				Build(),
			existingObjects: []client.Object{
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodRoleBindingName("test-pgs"),
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:               "PodGangSet",
								Name:               "test-pgs",
								UID:                "test-uid",
								Controller:         &[]bool{true}[0],
								BlockOwnerDeletion: &[]bool{true}[0],
							},
						},
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, client client.Client, pgs *grovecorev1alpha1.PodGangSet) {
				// Verify RoleBinding still exists
				roleBinding := &rbacv1.RoleBinding{}
				objectKey := getObjectKey(pgs.ObjectMeta)
				err := client.Get(context.Background(), objectKey, roleBinding)
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set UID for the PodGangSet to enable owner reference validation
			tt.pgs.UID = "test-uid"

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjects...).
				Build()

			operator := New(client, scheme)
			logger := logr.Discard()

			err := operator.Sync(context.Background(), logger, tt.pgs)

			if tt.expectError {
				assert.Error(t, err)
				var groveErr *groveerr.GroveError
				if assert.ErrorAs(t, err, &groveErr) {
					assert.Equal(t, tt.expectedErrorCode, groveErr.Code)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateFunc != nil {
					tt.validateFunc(t, client, tt.pgs)
				}
			}
		})
	}
}

// TestDelete validates the Delete method for removing RoleBinding resources.
// It tests successful deletion and graceful handling of non-existent resources.
func TestDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsObjMeta is the PodGangSet metadata used to identify the RoleBinding to delete
		pgsObjMeta metav1.ObjectMeta
		// existingObjects are the Kubernetes objects that exist in the fake client
		existingObjects []client.Object
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code if an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
		// validateFunc performs additional assertions on the result
		validateFunc func(t *testing.T, client client.Client, pgsObjMeta metav1.ObjectMeta)
	}{
		{
			// Tests successful deletion of an existing RoleBinding.
			// Should remove the RoleBinding from the cluster.
			name: "delete existing rolebinding",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			existingObjects: []client.Object{
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodRoleBindingName("test-pgs"),
						Namespace: "default",
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, client client.Client, pgsObjMeta metav1.ObjectMeta) {
				// Verify RoleBinding was deleted
				roleBinding := &rbacv1.RoleBinding{}
				objectKey := getObjectKey(pgsObjMeta)
				err := client.Get(context.Background(), objectKey, roleBinding)
				assert.True(t, errors.IsNotFound(err), "RoleBinding should be deleted")
			},
		},
		{
			// Tests graceful handling when RoleBinding doesn't exist.
			// Should return without error as deletion is idempotent.
			name: "delete non-existent rolebinding",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			existingObjects: []client.Object{},
			expectError:     false,
			validateFunc: func(t *testing.T, client client.Client, pgsObjMeta metav1.ObjectMeta) {
				// Verify RoleBinding still doesn't exist
				roleBinding := &rbacv1.RoleBinding{}
				objectKey := getObjectKey(pgsObjMeta)
				err := client.Get(context.Background(), objectKey, roleBinding)
				assert.True(t, errors.IsNotFound(err), "RoleBinding should not exist")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjects...).
				Build()

			operator := New(client, scheme)
			logger := logr.Discard()

			err := operator.Delete(context.Background(), logger, tt.pgsObjMeta)

			if tt.expectError {
				assert.Error(t, err)
				var groveErr *groveerr.GroveError
				if assert.ErrorAs(t, err, &groveErr) {
					assert.Equal(t, tt.expectedErrorCode, groveErr.Code)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateFunc != nil {
					tt.validateFunc(t, client, tt.pgsObjMeta)
				}
			}
		})
	}
}

// TestBuildResource validates the buildResource method for configuring RoleBinding resources.
// It tests proper setting of labels, owner references, role references, and subjects.
func TestBuildResource(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgs is the PodGangSet resource used to build the RoleBinding
		pgs *grovecorev1alpha1.PodGangSet
		// roleBinding is the RoleBinding resource to be configured
		roleBinding *rbacv1.RoleBinding
		// expectError indicates whether an error should be returned
		expectError bool
		// validateFunc performs assertions on the configured RoleBinding
		validateFunc func(t *testing.T, pgs *grovecorev1alpha1.PodGangSet, roleBinding *rbacv1.RoleBinding)
	}{
		{
			// Tests successful configuration of RoleBinding with all required fields.
			// Should set labels, owner reference, role reference, and subjects correctly.
			name: "successful rolebinding configuration",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid").
				Build(),
			roleBinding: &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      apicommon.GeneratePodRoleBindingName("test-pgs"),
					Namespace: "default",
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, pgs *grovecorev1alpha1.PodGangSet, roleBinding *rbacv1.RoleBinding) {
				// Verify labels are set correctly
				expectedLabels := getLabels(pgs.ObjectMeta)
				for key, expectedValue := range expectedLabels {
					assert.Equal(t, expectedValue, roleBinding.Labels[key])
				}

				// Verify owner reference is set
				assert.True(t, metav1.IsControlledBy(roleBinding, pgs))

				// Verify RoleRef is configured correctly
				expectedRoleRef := rbacv1.RoleRef{
					APIGroup: rbacv1.SchemeGroupVersion.Group,
					Kind:     "Role",
					Name:     apicommon.GeneratePodRoleName(pgs.Name),
				}
				assert.Equal(t, expectedRoleRef, roleBinding.RoleRef)

				// Verify Subjects are configured correctly
				expectedSubjects := []rbacv1.Subject{
					{
						APIGroup:  corev1.SchemeGroupVersion.Group,
						Kind:      "ServiceAccount",
						Name:      apicommon.GeneratePodServiceAccountName(pgs.Name),
						Namespace: pgs.Namespace,
					},
				}
				assert.Equal(t, expectedSubjects, roleBinding.Subjects)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set UID for the PodGangSet to enable owner reference validation
			tt.pgs.UID = "test-uid"

			operator := &_resource{
				client: fake.NewClientBuilder().WithScheme(scheme).Build(),
				scheme: scheme,
			}

			err := operator.buildResource(tt.pgs, tt.roleBinding)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.validateFunc != nil {
					tt.validateFunc(t, tt.pgs, tt.roleBinding)
				}
			}
		})
	}
}

// TestGetLabels validates the getLabels function for generating appropriate labels.
// It tests that component-specific and default PodGangSet labels are properly merged.
func TestGetLabels(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsObjMeta is the PodGangSet metadata used to generate labels
		pgsObjMeta metav1.ObjectMeta
		// expectedLabels are the labels expected to be returned
		expectedLabels map[string]string
	}{
		{
			// Tests label generation for a standard PodGangSet.
			// Should include component-specific labels and default PodGangSet labels.
			name: "standard podgangset labels",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			expectedLabels: map[string]string{
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodRoleBinding,
				apicommon.LabelAppNameKey:   "grove.io-pgs-test-pgs",
				// Note: Additional default labels would be included by k8sutils.GetDefaultLabelsForPodGangSetManagedResources
			},
		},
		{
			// Tests label generation with PodGangSet name containing colons.
			// Should replace colons with hyphens in the app name label.
			name: "podgangset name with colons",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test:pgs:with:colons",
				Namespace: "default",
			},
			expectedLabels: map[string]string{
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodRoleBinding,
				apicommon.LabelAppNameKey:   "grove.io-pgs-test-pgs-with-colons",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := getLabels(tt.pgsObjMeta)

			// Verify that expected labels are present
			for key, expectedValue := range tt.expectedLabels {
				actualValue, exists := labels[key]
				assert.True(t, exists, "Label %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "Label %s should have correct value", key)
			}

			// Verify component and app name labels are always present
			assert.Contains(t, labels, apicommon.LabelComponentKey)
			assert.Contains(t, labels, apicommon.LabelAppNameKey)
			assert.Equal(t, apicommon.LabelComponentNamePodRoleBinding, labels[apicommon.LabelComponentKey])
		})
	}
}

// TestGetObjectKey validates the getObjectKey function for generating Kubernetes object keys.
// It tests that the correct name and namespace are used based on PodGangSet metadata.
func TestGetObjectKey(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsObjMeta is the PodGangSet metadata used to generate the object key
		pgsObjMeta metav1.ObjectMeta
		// expectedKey is the object key expected to be returned
		expectedKey client.ObjectKey
	}{
		{
			// Tests object key generation for a standard PodGangSet.
			// Should use generated RoleBinding name and PodGangSet namespace.
			name: "standard podgangset object key",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			expectedKey: client.ObjectKey{
				Name:      apicommon.GeneratePodRoleBindingName("test-pgs"),
				Namespace: "default",
			},
		},
		{
			// Tests object key generation with different namespace.
			// Should use the correct namespace from PodGangSet metadata.
			name: "different namespace",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "kube-system",
			},
			expectedKey: client.ObjectKey{
				Name:      apicommon.GeneratePodRoleBindingName("test-pgs"),
				Namespace: "kube-system",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectKey := getObjectKey(tt.pgsObjMeta)
			assert.Equal(t, tt.expectedKey, objectKey)
		})
	}
}

// TestEmptyRoleBinding validates the emptyRoleBinding function for creating minimal RoleBinding resources.
// It tests that only name and namespace are set in the returned RoleBinding.
func TestEmptyRoleBinding(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// objKey is the object key used to create the empty RoleBinding
		objKey client.ObjectKey
		// validateFunc performs assertions on the returned RoleBinding
		validateFunc func(t *testing.T, roleBinding *rbacv1.RoleBinding, objKey client.ObjectKey)
	}{
		{
			// Tests creation of empty RoleBinding with standard object key.
			// Should create RoleBinding with only name and namespace set.
			name: "standard empty rolebinding",
			objKey: client.ObjectKey{
				Name:      "test-rolebinding",
				Namespace: "default",
			},
			validateFunc: func(t *testing.T, roleBinding *rbacv1.RoleBinding, objKey client.ObjectKey) {
				assert.Equal(t, objKey.Name, roleBinding.Name)
				assert.Equal(t, objKey.Namespace, roleBinding.Namespace)

				// Verify that other fields are empty/default
				assert.Empty(t, roleBinding.Labels)
				assert.Empty(t, roleBinding.Annotations)
				assert.Empty(t, roleBinding.OwnerReferences)
				assert.Empty(t, roleBinding.RoleRef.Name)
				assert.Empty(t, roleBinding.Subjects)
			},
		},
		{
			// Tests creation with different namespace.
			// Should correctly set the namespace from the object key.
			name: "different namespace",
			objKey: client.ObjectKey{
				Name:      "test-rolebinding",
				Namespace: "kube-system",
			},
			validateFunc: func(t *testing.T, roleBinding *rbacv1.RoleBinding, objKey client.ObjectKey) {
				assert.Equal(t, objKey.Name, roleBinding.Name)
				assert.Equal(t, objKey.Namespace, roleBinding.Namespace)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roleBinding := emptyRoleBinding(tt.objKey)

			assert.NotNil(t, roleBinding)
			assert.IsType(t, &rbacv1.RoleBinding{}, roleBinding)

			if tt.validateFunc != nil {
				tt.validateFunc(t, roleBinding, tt.objKey)
			}
		})
	}
}

// TestSyncErrorHandling validates error handling scenarios in the Sync method.
// It tests various error conditions and ensures proper Grove error wrapping.
func TestSyncErrorHandling(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgs is the PodGangSet resource being synchronized
		pgs *grovecorev1alpha1.PodGangSet
		// clientBuilder configures the fake client to simulate error conditions
		clientBuilder func() client.Client
		// expectedErrorCode is the expected Grove error code
		expectedErrorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests error handling when GetExistingResourceNames fails.
			// Should wrap the error with appropriate Grove error code.
			name: "get existing resource names error",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid").
				Build(),
			clientBuilder: func() client.Client {
				// Create a client that will fail on Get operations
				return &failingClient{
					Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
					failOn: "Get",
				}
			},
			expectedErrorCode: errSyncRoleBinding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.pgs.UID = "test-uid"

			client := tt.clientBuilder()
			operator := New(client, scheme)
			logger := logr.Discard()

			err := operator.Sync(context.Background(), logger, tt.pgs)

			assert.Error(t, err)
			var groveErr *groveerr.GroveError
			if assert.ErrorAs(t, err, &groveErr) {
				assert.Equal(t, tt.expectedErrorCode, groveErr.Code)
			}
		})
	}
}

// failingClient is a test helper that wraps a client and fails on specific operations
type failingClient struct {
	client.Client
	failOn string
}

func (f *failingClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	if f.failOn == "Get" {
		return errors.NewInternalError(assert.AnError)
	}
	return f.Client.Get(ctx, key, obj, opts...)
}

func (f *failingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if f.failOn == "Create" {
		return errors.NewInternalError(assert.AnError)
	}
	return f.Client.Create(ctx, obj, opts...)
}

func (f *failingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if f.failOn == "Delete" {
		return errors.NewInternalError(assert.AnError)
	}
	return f.Client.Delete(ctx, obj, opts...)
}
