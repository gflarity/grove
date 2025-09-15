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

package role

import (
	"context"
	"strings"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestNew verifies that the New constructor properly initializes a role operator
// with the provided client and scheme.
func TestNew(t *testing.T) {
	// Create a fake client and scheme for testing
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Test that New returns a properly initialized operator
	operator := New(fakeClient, scheme)

	assert.NotNil(t, operator, "New should return a non-nil operator")
}

// TestGetExistingResourceNames tests the retrieval of existing Role resource names
// managed by the operator for a given PodGangSet.
func TestGetExistingResourceNames(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata used to search for roles
		pgsObjMeta metav1.ObjectMeta
		// existingRoles are the Role resources that already exist in the cluster
		existingRoles []rbacv1.Role
		// expectedNames are the role names that should be returned
		expectedNames []string
		// expectError indicates whether an error should be returned
		expectError bool
		// errorCode is the expected Grove error code when expectError is true
		errorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests successful retrieval when a matching role exists and is owned by the PodGangSet
			name: "role exists and is owned by PodGangSet",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-ns",
				UID:       types.UID("test-uid"),
			},
			existingRoles: []rbacv1.Role{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodRoleName("test-pgs"),
						Namespace: "test-ns",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodGangSet",
								Name:       "test-pgs",
								UID:        types.UID("test-uid"),
								Controller: lo.ToPtr(true),
							},
						},
					},
				},
			},
			expectedNames: []string{apicommon.GeneratePodRoleName("test-pgs")},
			expectError:   false,
		},
		{
			// Tests that no names are returned when no role exists
			name: "no role exists",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-ns",
				UID:       types.UID("test-uid"),
			},
			existingRoles: []rbacv1.Role{},
			expectedNames: []string{},
			expectError:   false,
		},
		{
			// Tests that no names are returned when role exists but is not owned by the PodGangSet
			name: "role exists but not owned by PodGangSet",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-ns",
				UID:       types.UID("test-uid"),
			},
			existingRoles: []rbacv1.Role{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodRoleName("test-pgs"),
						Namespace: "test-ns",
						// No owner references - not owned by PodGangSet
					},
				},
			},
			expectedNames: []string{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with existing roles
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(convertRolesToRuntimeObjects(tt.existingRoles)...).
				Build()

			operator := &_resource{
				client: fakeClient,
				scheme: scheme,
			}

			ctx := context.Background()
			logger := logr.Discard()

			// Execute the method under test
			names, err := operator.GetExistingResourceNames(ctx, logger, tt.pgsObjMeta)

			// Verify results
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
				if err != nil {
					var groveErr *groveerr.GroveError
					require.ErrorAs(t, err, &groveErr, "Error should be a GroveError")
					assert.Equal(t, tt.errorCode, groveErr.Code, "Error code should match expected")
				}
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
				assert.Equal(t, tt.expectedNames, names, "Returned names should match expected")
			}
		})
	}
}

// TestSync tests the synchronization of Role resources, including creation
// and handling of existing resources.
func TestSync(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the PodGangSet resource that owns the role
		pgs *grovecorev1alpha1.PodGangSet
		// existingRoles are the Role resources that already exist in the cluster
		existingRoles []rbacv1.Role
		// expectError indicates whether an error should be returned
		expectError bool
		// errorCode is the expected Grove error code when expectError is true
		errorCode grovecorev1alpha1.ErrorCode
		// expectRoleCreated indicates whether a new role should be created
		expectRoleCreated bool
	}{
		{
			// Tests successful role creation when no role exists
			name: "creates role when none exists",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-ns", types.UID("test-uid")).
				WithReplicas(1).
				Build(),
			existingRoles:     []rbacv1.Role{},
			expectError:       false,
			expectRoleCreated: true,
		},
		{
			// Tests that sync skips creation when role already exists
			name: "skips creation when role already exists",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-ns", types.UID("test-uid")).
				WithReplicas(1).
				Build(),
			existingRoles: []rbacv1.Role{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodRoleName("test-pgs"),
						Namespace: "test-ns",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodGangSet",
								Name:       "test-pgs",
								UID:        "test-uid",
								Controller: lo.ToPtr(true),
							},
						},
					},
				},
			},
			expectError:       false,
			expectRoleCreated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set UID on PodGangSet to match owner references
			if len(tt.existingRoles) > 0 {
				tt.pgs.UID = types.UID("test-uid")
			}

			// Create fake client with existing roles
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(convertRolesToRuntimeObjects(tt.existingRoles)...).
				Build()

			operator := &_resource{
				client: fakeClient,
				scheme: scheme,
			}

			ctx := context.Background()
			logger := logr.Discard()

			// Execute the method under test
			err := operator.Sync(ctx, logger, tt.pgs)

			// Verify results
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
				if err != nil {
					var groveErr *groveerr.GroveError
					require.ErrorAs(t, err, &groveErr, "Error should be a GroveError")
					assert.Equal(t, tt.errorCode, groveErr.Code, "Error code should match expected")
				}
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)

				// Verify role creation/existence
				objectKey := getObjectKey(tt.pgs.ObjectMeta)
				role := &rbacv1.Role{}
				err := fakeClient.Get(ctx, objectKey, role)

				if tt.expectRoleCreated {
					assert.NoError(t, err, "Role should exist after sync")
					if err == nil {
						// Verify role is properly configured
						assert.Equal(t, objectKey.Name, role.Name, "Role name should match expected")
						assert.Equal(t, objectKey.Namespace, role.Namespace, "Role namespace should match expected")
						assert.NotEmpty(t, role.Rules, "Role should have RBAC rules configured")

						// Verify RBAC rules
						expectedRules := []rbacv1.PolicyRule{
							{
								APIGroups: []string{""},
								Resources: []string{"pods", "pods/status"},
								Verbs:     []string{"get", "list", "watch"},
							},
						}
						assert.Equal(t, expectedRules, role.Rules, "Role rules should match expected")
					}
				} else {
					// Role should still exist but not be newly created
					assert.NoError(t, err, "Role should exist")
				}
			}
		})
	}
}

// TestDelete tests the deletion of Role resources associated with a PodGangSet.
func TestDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata used to identify the role to delete
		pgsObjMeta metav1.ObjectMeta
		// existingRoles are the Role resources that already exist in the cluster
		existingRoles []rbacv1.Role
		// expectError indicates whether an error should be returned
		expectError bool
		// errorCode is the expected Grove error code when expectError is true
		errorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests successful deletion when role exists
			name: "successfully deletes existing role",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-ns",
			},
			existingRoles: []rbacv1.Role{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodRoleName("test-pgs"),
						Namespace: "test-ns",
					},
				},
			},
			expectError: false,
		},
		{
			// Tests that deletion is a no-op when role doesn't exist
			name: "no-op when role doesn't exist",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-ns",
			},
			existingRoles: []rbacv1.Role{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with existing roles
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(convertRolesToRuntimeObjects(tt.existingRoles)...).
				Build()

			operator := &_resource{
				client: fakeClient,
				scheme: scheme,
			}

			ctx := context.Background()
			logger := logr.Discard()

			// Execute the method under test
			err := operator.Delete(ctx, logger, tt.pgsObjMeta)

			// Verify results
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
				if err != nil {
					var groveErr *groveerr.GroveError
					require.ErrorAs(t, err, &groveErr, "Error should be a GroveError")
					assert.Equal(t, tt.errorCode, groveErr.Code, "Error code should match expected")
				}
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)

				// Verify role is deleted
				objectKey := getObjectKey(tt.pgsObjMeta)
				role := &rbacv1.Role{}
				err := fakeClient.Get(ctx, objectKey, role)
				assert.True(t, errors.IsNotFound(err), "Role should be deleted")
			}
		})
	}
}

// TestBuildResource tests the buildResource method that configures Role resources
// with proper labels, owner references, and RBAC rules.
func TestBuildResource(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the PodGangSet resource that will own the role
		pgs *grovecorev1alpha1.PodGangSet
		// expectError indicates whether an error should be returned
		expectError bool
	}{
		{
			// Tests successful role configuration with proper labels and RBAC rules
			name: "successfully configures role",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-ns", types.UID("test-uid")).
				WithReplicas(1).
				Build(),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set UID for owner reference
			tt.pgs.UID = types.UID("test-uid")

			operator := &_resource{
				client: fake.NewClientBuilder().WithScheme(scheme).Build(),
				scheme: scheme,
			}

			// Create empty role to configure
			objectKey := getObjectKey(tt.pgs.ObjectMeta)
			role := emptyRole(objectKey)

			// Execute the method under test
			err := operator.buildResource(tt.pgs, role)

			// Verify results
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)

				// Verify labels are set correctly
				expectedLabels := getLabels(tt.pgs.ObjectMeta)
				assert.Equal(t, expectedLabels, role.Labels, "Role labels should match expected")

				// Verify owner reference is set
				assert.Len(t, role.OwnerReferences, 1, "Role should have one owner reference")
				ownerRef := role.OwnerReferences[0]
				assert.Equal(t, tt.pgs.Name, ownerRef.Name, "Owner reference name should match PodGangSet")
				assert.Equal(t, tt.pgs.UID, ownerRef.UID, "Owner reference UID should match PodGangSet")
				assert.True(t, *ownerRef.Controller, "Owner reference should be a controller")

				// Verify RBAC rules are configured correctly
				expectedRules := []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"pods", "pods/status"},
						Verbs:     []string{"get", "list", "watch"},
					},
				}
				assert.Equal(t, expectedRules, role.Rules, "Role rules should match expected")
			}
		})
	}
}

// TestGetLabels tests the getLabels helper function that generates appropriate
// labels for Role resources based on PodGangSet metadata.
func TestGetLabels(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata used to generate labels
		pgsObjMeta metav1.ObjectMeta
		// expectedLabels are the labels that should be generated
		expectedLabels map[string]string
	}{
		{
			// Tests label generation for a standard PodGangSet
			name: "generates correct labels for PodGangSet",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-ns",
			},
			expectedLabels: func() map[string]string {
				// Get default labels and merge with role-specific labels
				defaultLabels := apicommon.GetDefaultLabelsForPodGangSetManagedResources("test-pgs")
				roleLabels := map[string]string{
					apicommon.LabelComponentKey: apicommon.LabelComponentNamePodRole,
					apicommon.LabelAppNameKey:   strings.ReplaceAll(apicommon.GeneratePodRoleName("test-pgs"), ":", "-"),
				}
				return lo.Assign(defaultLabels, roleLabels)
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			labels := getLabels(tt.pgsObjMeta)

			// Verify results
			assert.Equal(t, tt.expectedLabels, labels, "Generated labels should match expected")

			// Verify required labels are present
			assert.Contains(t, labels, apicommon.LabelComponentKey, "Should contain component label")
			assert.Equal(t, apicommon.LabelComponentNamePodRole, labels[apicommon.LabelComponentKey], "Component label should be pod-role")
			assert.Contains(t, labels, apicommon.LabelAppNameKey, "Should contain app name label")
		})
	}
}

// TestGetObjectKey tests the getObjectKey helper function that constructs
// Kubernetes object keys for Role resources.
func TestGetObjectKey(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata used to generate the object key
		pgsObjMeta metav1.ObjectMeta
		// expectedKey is the object key that should be generated
		expectedKey client.ObjectKey
	}{
		{
			// Tests object key generation for a standard PodGangSet
			name: "generates correct object key",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-ns",
			},
			expectedKey: client.ObjectKey{
				Name:      apicommon.GeneratePodRoleName("test-pgs"),
				Namespace: "test-ns",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			objectKey := getObjectKey(tt.pgsObjMeta)

			// Verify results
			assert.Equal(t, tt.expectedKey, objectKey, "Generated object key should match expected")
			assert.NotEmpty(t, objectKey.Name, "Object key name should not be empty")
			assert.NotEmpty(t, objectKey.Namespace, "Object key namespace should not be empty")
		})
	}
}

// TestEmptyRole tests the emptyRole helper function that creates minimal
// Role resources for creation and deletion operations.
func TestEmptyRole(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// objKey is the object key used to create the empty role
		objKey client.ObjectKey
		// expectedName is the name that should be set on the role
		expectedName string
		// expectedNamespace is the namespace that should be set on the role
		expectedNamespace string
	}{
		{
			// Tests empty role creation with proper name and namespace
			name: "creates empty role with correct metadata",
			objKey: client.ObjectKey{
				Name:      "test-role",
				Namespace: "test-ns",
			},
			expectedName:      "test-role",
			expectedNamespace: "test-ns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			role := emptyRole(tt.objKey)

			// Verify results
			assert.NotNil(t, role, "Empty role should not be nil")
			assert.Equal(t, tt.expectedName, role.Name, "Role name should match expected")
			assert.Equal(t, tt.expectedNamespace, role.Namespace, "Role namespace should match expected")
			assert.Empty(t, role.Rules, "Empty role should have no RBAC rules")
			assert.Empty(t, role.Labels, "Empty role should have no labels")
			assert.Empty(t, role.OwnerReferences, "Empty role should have no owner references")
		})
	}
}

// convertRolesToRuntimeObjects is a helper function that converts Role objects
// to runtime.Object for use with the fake client builder.
func convertRolesToRuntimeObjects(roles []rbacv1.Role) []runtime.Object {
	objects := make([]runtime.Object, len(roles))
	for i, role := range roles {
		objects[i] = &role
	}
	return objects
}
