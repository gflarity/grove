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

package satokensecret

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestNew verifies that the New function correctly creates a ServiceAccount token secret component operator
// with the expected client and scheme configuration.
func TestNew(t *testing.T) {
	// Test setup: create a fake client and scheme
	fakeClient := fake.NewClientBuilder().Build()
	testScheme := runtime.NewScheme()

	// Execute the function under test
	operator := New(fakeClient, testScheme)

	// Verify the operator is created with correct type and configuration
	require.NotNil(t, operator, "New should return a non-nil operator")

	// Type assertion to access internal fields for validation
	resource, ok := operator.(*_resource)
	require.True(t, ok, "New should return a *_resource type")
	assert.Equal(t, fakeClient, resource.client, "Client should be set correctly")
	assert.Equal(t, testScheme, resource.scheme, "Scheme should be set correctly")
}

// TestGetExistingResourceNames verifies the retrieval of existing ServiceAccount token secret names
// that are controlled by a PodGangSet, handling various scenarios including missing secrets,
// existing controlled secrets, and existing uncontrolled secrets.
func TestGetExistingResourceNames(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata used to search for controlled secrets
		pgsObjMeta metav1.ObjectMeta
		// existingSecrets are the secrets that already exist in the cluster
		existingSecrets []client.Object
		// expectedNames are the secret names that should be returned as controlled by the PodGangSet
		expectedNames []string
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code when an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests the case where no secret exists for the PodGangSet
			name: "no existing secret",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingSecrets: []client.Object{},
			expectedNames:   []string{},
			expectError:     false,
		},
		{
			// Tests the case where a secret exists and is controlled by the PodGangSet
			name: "existing controlled secret",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingSecrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GenerateInitContainerSATokenSecretName("test-pgs"),
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodGangSet",
								Name:       "test-pgs",
								UID:        "test-uid-123",
								Controller: &[]bool{true}[0],
							},
						},
					},
				},
			},
			expectedNames: []string{apicommon.GenerateInitContainerSATokenSecretName("test-pgs")},
			expectError:   false,
		},
		{
			// Tests the case where a secret exists but is not controlled by the PodGangSet
			name: "existing uncontrolled secret",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingSecrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GenerateInitContainerSATokenSecretName("test-pgs"),
						Namespace: "default",
						// No owner reference or different owner
					},
				},
			},
			expectedNames: []string{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: create fake client with existing secrets
			fakeClient := fake.NewClientBuilder().
				WithObjects(tt.existingSecrets...).
				Build()

			r := &_resource{
				client: fakeClient,
				scheme: scheme.Scheme,
			}

			// Execute the function under test
			ctx := context.Background()
			logger := logr.Discard()
			names, err := r.GetExistingResourceNames(ctx, logger, tt.pgsObjMeta)

			// Verify the results
			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				var groveErr *groveerr.GroveError
				require.ErrorAs(t, err, &groveErr, "Error should be a GroveError")
				assert.Equal(t, tt.expectedErrorCode, groveErr.Code, "Error code should match expected")
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)
				assert.Equal(t, tt.expectedNames, names, "Returned names should match expected")
			}
		})
	}
}

// TestSync verifies the synchronization of ServiceAccount token secrets, including creation of new secrets
// when they don't exist and skipping creation when they already exist.
func TestSync(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the PodGangSet resource for which the secret should be created
		pgs *grovecorev1alpha1.PodGangSet
		// existingSecrets are the secrets that already exist in the cluster
		existingSecrets []client.Object
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code when an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
		// expectSecretCreated indicates whether a new secret should be created
		expectSecretCreated bool
	}{
		{
			// Tests creation of a new secret when none exists
			name: "create new secret",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid-123").
				Build(),
			existingSecrets:     []client.Object{},
			expectError:         false,
			expectSecretCreated: true,
		},
		{
			// Tests skipping creation when a controlled secret already exists
			name: "skip creation when secret exists",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid-456").
				Build(),
			existingSecrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GenerateInitContainerSATokenSecretName("test-pgs"),
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodGangSet",
								Name:       "test-pgs",
								UID:        "test-uid-123",
								Controller: &[]bool{true}[0],
							},
						},
					},
				},
			},
			expectError:         false,
			expectSecretCreated: false,
		},
		{
			// Tests error handling when buildResource fails due to missing scheme
			name: "buildResource fails with invalid scheme",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid-789").
				Build(),
			existingSecrets:     []client.Object{},
			expectError:         true,
			expectedErrorCode:   errCodeSetControllerReference,
			expectSecretCreated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: create fake client with existing secrets and proper scheme
			testScheme := runtime.NewScheme()

			// For error test cases, use an incomplete scheme to trigger SetControllerReference failure
			if !tt.expectError {
				require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
				require.NoError(t, corev1.AddToScheme(testScheme))
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(tt.existingSecrets...).
				Build()

			r := &_resource{
				client: fakeClient,
				scheme: testScheme,
			}

			// Set UID for the PodGangSet to enable owner reference
			if tt.pgs.UID == "" {
				tt.pgs.UID = "test-uid-123"
			}

			// Execute the function under test
			ctx := context.Background()
			logger := logr.Discard()
			err := r.Sync(ctx, logger, tt.pgs)

			// Verify the results
			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				var groveErr *groveerr.GroveError
				require.ErrorAs(t, err, &groveErr, "Error should be a GroveError")
				assert.Equal(t, tt.expectedErrorCode, groveErr.Code, "Error code should match expected")
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)

				// Verify secret creation if expected
				if tt.expectSecretCreated {
					secretKey := client.ObjectKey{
						Name:      apicommon.GenerateInitContainerSATokenSecretName(tt.pgs.Name),
						Namespace: tt.pgs.Namespace,
					}
					secret := &corev1.Secret{}
					err := fakeClient.Get(ctx, secretKey, secret)
					require.NoError(t, err, "Created secret should be retrievable")

					// Verify secret configuration
					assert.Equal(t, corev1.SecretTypeServiceAccountToken, secret.Type, "Secret type should be ServiceAccountToken")
					assert.Equal(t, apicommon.GeneratePodServiceAccountName(tt.pgs.Name),
						secret.Annotations[corev1.ServiceAccountNameKey], "ServiceAccount annotation should be set correctly")
					assert.True(t, metav1.IsControlledBy(secret, tt.pgs), "Secret should be controlled by PodGangSet")
				}
			}
		})
	}
}

// TestDelete verifies the deletion of ServiceAccount token secrets, handling both successful deletions
// and graceful handling of non-existent secrets.
func TestDelete(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata used to identify the secret to delete
		pgsObjMeta metav1.ObjectMeta
		// existingSecrets are the secrets that already exist in the cluster
		existingSecrets []client.Object
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code when an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests successful deletion of an existing secret
			name: "delete existing secret",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			existingSecrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GenerateInitContainerSATokenSecretName("test-pgs"),
						Namespace: "default",
					},
				},
			},
			expectError: false,
		},
		{
			// Tests graceful handling when the secret doesn't exist
			name: "delete non-existent secret",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			existingSecrets: []client.Object{},
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: create fake client with existing secrets
			fakeClient := fake.NewClientBuilder().
				WithObjects(tt.existingSecrets...).
				Build()

			r := &_resource{
				client: fakeClient,
				scheme: scheme.Scheme,
			}

			// Execute the function under test
			ctx := context.Background()
			logger := logr.Discard()
			err := r.Delete(ctx, logger, tt.pgsObjMeta)

			// Verify the results
			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				var groveErr *groveerr.GroveError
				require.ErrorAs(t, err, &groveErr, "Error should be a GroveError")
				assert.Equal(t, tt.expectedErrorCode, groveErr.Code, "Error code should match expected")
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)

				// Verify the secret was deleted (or didn't exist)
				secretKey := client.ObjectKey{
					Name:      apicommon.GenerateInitContainerSATokenSecretName(tt.pgsObjMeta.Name),
					Namespace: tt.pgsObjMeta.Namespace,
				}
				secret := &corev1.Secret{}
				err := fakeClient.Get(ctx, secretKey, secret)
				assert.True(t, errors.IsNotFound(err), "Secret should not exist after deletion")
			}
		})
	}
}

// TestBuildResource verifies the configuration of ServiceAccount token secrets with proper metadata,
// labels, annotations, and controller ownership relationships.
func TestBuildResource(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the PodGangSet resource that should own the secret
		pgs *grovecorev1alpha1.PodGangSet
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code when an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests successful configuration of a secret with proper PodGangSet ownership
			name: "successful resource building",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid-abc").
				Build(),
			expectError: false,
		},
		{
			// Tests error handling when SetControllerReference fails due to incomplete scheme
			name: "SetControllerReference fails with incomplete scheme",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", "test-uid-def").
				Build(),
			expectError:       true,
			expectedErrorCode: errCodeSetControllerReference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: create proper scheme and resource
			testScheme := runtime.NewScheme()

			// For error test cases, use an incomplete scheme to trigger SetControllerReference failure
			if !tt.expectError {
				require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
				require.NoError(t, corev1.AddToScheme(testScheme))
			}

			r := &_resource{
				client: fake.NewClientBuilder().Build(),
				scheme: testScheme,
			}

			// Set UID for the PodGangSet to enable owner reference
			if tt.pgs.UID == "" {
				tt.pgs.UID = "test-uid-123"
			}

			// Create empty secret to configure
			objKey := getObjectKey(tt.pgs.ObjectMeta)
			secret := emptySecret(objKey)

			// Execute the function under test
			err := r.buildResource(tt.pgs, secret)

			// Verify the results
			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				var groveErr *groveerr.GroveError
				require.ErrorAs(t, err, &groveErr, "Error should be a GroveError")
				assert.Equal(t, tt.expectedErrorCode, groveErr.Code, "Error code should match expected")
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)

				// Verify secret configuration
				assert.Equal(t, corev1.SecretTypeServiceAccountToken, secret.Type, "Secret type should be ServiceAccountToken")

				// Verify annotations
				expectedSAName := apicommon.GeneratePodServiceAccountName(tt.pgs.Name)
				assert.Equal(t, expectedSAName, secret.Annotations[corev1.ServiceAccountNameKey],
					"ServiceAccount annotation should be set correctly")

				// Verify labels
				expectedLabels := getLabels(tt.pgs.Name, secret.Name)
				assert.Equal(t, expectedLabels, secret.Labels, "Labels should be set correctly")

				// Verify controller ownership
				assert.True(t, metav1.IsControlledBy(secret, tt.pgs), "Secret should be controlled by PodGangSet")
			}
		})
	}
}

// TestGetLabels verifies the generation of standard labels for ServiceAccount token secrets,
// ensuring they include both default PodGangSet labels and component-specific labels.
func TestGetLabels(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsName is the name of the PodGangSet
		pgsName string
		// secretName is the name of the secret
		secretName string
		// expectedLabels are the labels that should be generated
		expectedLabels map[string]string
	}{
		{
			// Tests label generation for a typical PodGangSet and secret
			name:       "standard label generation",
			pgsName:    "test-pgs",
			secretName: "test-secret",
			expectedLabels: map[string]string{
				apicommon.LabelComponentKey: apicommon.LabelComponentNameServiceAccountTokenSecret,
				apicommon.LabelAppNameKey:   "test-secret",
				// Note: Additional default labels would be included by GetDefaultLabelsForPodGangSetManagedResources
				// but we can't easily test those without mocking that function
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			labels := getLabels(tt.pgsName, tt.secretName)

			// Verify component-specific labels are present
			assert.Equal(t, apicommon.LabelComponentNameServiceAccountTokenSecret, labels[apicommon.LabelComponentKey],
				"Component label should be set correctly")
			assert.Equal(t, tt.secretName, labels[apicommon.LabelAppNameKey],
				"App name label should be set correctly")

			// Verify that labels map is not empty and contains expected keys
			assert.NotEmpty(t, labels, "Labels should not be empty")
			assert.Contains(t, labels, apicommon.LabelComponentKey, "Should contain component label")
			assert.Contains(t, labels, apicommon.LabelAppNameKey, "Should contain app name label")
		})
	}
}

// TestGetObjectKey verifies the generation of Kubernetes object keys for ServiceAccount token secrets
// based on PodGangSet metadata.
func TestGetObjectKey(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata
		pgsObjMeta metav1.ObjectMeta
		// expectedKey is the object key that should be generated
		expectedKey client.ObjectKey
	}{
		{
			// Tests object key generation for a typical PodGangSet
			name: "standard object key generation",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			expectedKey: client.ObjectKey{
				Name:      apicommon.GenerateInitContainerSATokenSecretName("test-pgs"),
				Namespace: "default",
			},
		},
		{
			// Tests object key generation with different namespace
			name: "different namespace",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "my-pgs",
				Namespace: "custom-ns",
			},
			expectedKey: client.ObjectKey{
				Name:      apicommon.GenerateInitContainerSATokenSecretName("my-pgs"),
				Namespace: "custom-ns",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			objKey := getObjectKey(tt.pgsObjMeta)

			// Verify the results
			assert.Equal(t, tt.expectedKey, objKey, "Object key should match expected")
		})
	}
}

// TestEmptySecret verifies the creation of empty Secret objects with basic metadata populated
// for use as templates in creation and deletion operations.
func TestEmptySecret(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// objKey is the object key used to create the empty secret
		objKey client.ObjectKey
		// expectedName is the expected name of the created secret
		expectedName string
		// expectedNamespace is the expected namespace of the created secret
		expectedNamespace string
	}{
		{
			// Tests creation of empty secret with standard object key
			name: "standard empty secret creation",
			objKey: client.ObjectKey{
				Name:      "test-secret",
				Namespace: "default",
			},
			expectedName:      "test-secret",
			expectedNamespace: "default",
		},
		{
			// Tests creation of empty secret with different namespace
			name: "different namespace",
			objKey: client.ObjectKey{
				Name:      "another-secret",
				Namespace: "custom-ns",
			},
			expectedName:      "another-secret",
			expectedNamespace: "custom-ns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			secret := emptySecret(tt.objKey)

			// Verify the results
			require.NotNil(t, secret, "Empty secret should not be nil")
			assert.Equal(t, tt.expectedName, secret.Name, "Secret name should match expected")
			assert.Equal(t, tt.expectedNamespace, secret.Namespace, "Secret namespace should match expected")

			// Verify it's a proper Secret object
			assert.IsType(t, &corev1.Secret{}, secret, "Should return a Secret object")

			// Verify only basic metadata is set (no other fields should be populated)
			assert.Empty(t, secret.Data, "Data should be empty")
			assert.Empty(t, secret.StringData, "StringData should be empty")
			assert.Empty(t, secret.Labels, "Labels should be empty")
			assert.Empty(t, secret.Annotations, "Annotations should be empty")
		})
	}
}
