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

package serviceaccount

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestNew verifies that the New function correctly creates a ServiceAccount component operator
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

// TestGetExistingResourceNames verifies the retrieval of existing ServiceAccount names
// that are controlled by a PodGangSet, handling various scenarios including missing ServiceAccounts,
// existing controlled ServiceAccounts, and existing uncontrolled ServiceAccounts.
func TestGetExistingResourceNames(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata used to search for controlled ServiceAccounts
		pgsObjMeta metav1.ObjectMeta
		// existingServiceAccounts are the ServiceAccounts that already exist in the cluster
		existingServiceAccounts []client.Object
		// expectedNames are the ServiceAccount names that should be returned as controlled by the PodGangSet
		expectedNames []string
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code when an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests the case where no ServiceAccount exists for the PodGangSet
			name: "no existing service account",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingServiceAccounts: []client.Object{},
			expectedNames:           []string{},
			expectError:             false,
		},
		{
			// Tests the case where a ServiceAccount exists and is controlled by the PodGangSet
			name: "existing controlled service account",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingServiceAccounts: []client.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodServiceAccountName("test-pgs"),
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:               "PodGangSet",
								Name:               "test-pgs",
								UID:                "test-uid-123",
								Controller:         ptr.To(true),
								BlockOwnerDeletion: ptr.To(true),
							},
						},
					},
					AutomountServiceAccountToken: ptr.To(true),
				},
			},
			expectedNames: []string{apicommon.GeneratePodServiceAccountName("test-pgs")},
			expectError:   false,
		},
		{
			// Tests the case where a ServiceAccount exists but is not controlled by the PodGangSet
			name: "existing uncontrolled service account",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingServiceAccounts: []client.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodServiceAccountName("test-pgs"),
						Namespace: "default",
						// No owner reference or different owner
					},
				},
			},
			expectedNames: []string{},
			expectError:   false,
		},
		{
			// Tests the case where a ServiceAccount exists and is controlled by a different PodGangSet
			name: "existing service account controlled by different pgs",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingServiceAccounts: []client.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apicommon.GeneratePodServiceAccountName("test-pgs"),
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:               "PodGangSet",
								Name:               "different-pgs",
								UID:                "different-uid-456",
								Controller:         ptr.To(true),
								BlockOwnerDeletion: ptr.To(true),
							},
						},
					},
				},
			},
			expectedNames: []string{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the fake client with existing ServiceAccounts
			clientBuilder := fake.NewClientBuilder()
			if len(tt.existingServiceAccounts) > 0 {
				clientBuilder = clientBuilder.WithObjects(tt.existingServiceAccounts...)
			}
			fakeClient := clientBuilder.Build()

			// Create the operator instance
			operator := &_resource{
				client: fakeClient,
				scheme: scheme.Scheme,
			}

			// Execute the function under test
			names, err := operator.GetExistingResourceNames(context.Background(), logr.Discard(), tt.pgsObjMeta)

			// Verify the results
			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				groveErr, ok := err.(*groveerr.GroveError)
				require.True(t, ok, "Expected a Grove error")
				assert.Equal(t, tt.expectedErrorCode, groveErr.Code, "Error code should match expected")
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)
				assert.ElementsMatch(t, tt.expectedNames, names, "Returned names should match expected")
			}
		})
	}
}

// TestSync verifies the Sync method which creates or updates ServiceAccount resources
// for a PodGangSet, ensuring proper labels, ownership, and configuration.
func TestSync(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the PodGangSet object being synchronized
		pgs *grovecorev1alpha1.PodGangSet
		// existingServiceAccount is the ServiceAccount that may already exist
		existingServiceAccount *corev1.ServiceAccount
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code when an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
		// validateServiceAccount is a function to validate the resulting ServiceAccount
		validateServiceAccount func(t *testing.T, sa *corev1.ServiceAccount, pgs *grovecorev1alpha1.PodGangSet)
	}{
		{
			// Tests creating a new ServiceAccount when none exists
			name: "create new service account",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid-123",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
				},
			},
			existingServiceAccount: nil,
			expectError:            false,
			validateServiceAccount: func(t *testing.T, sa *corev1.ServiceAccount, pgs *grovecorev1alpha1.PodGangSet) {
				// Verify basic metadata
				assert.Equal(t, apicommon.GeneratePodServiceAccountName(pgs.Name), sa.Name)
				assert.Equal(t, pgs.Namespace, sa.Namespace)

				// Verify labels
				expectedLabels := map[string]string{
					apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
					apicommon.LabelPartOfKey:    pgs.Name,
					apicommon.LabelComponentKey: apicommon.LabelComponentNamePodServiceAccount,
					apicommon.LabelAppNameKey:   apicommon.GeneratePodServiceAccountName(pgs.Name),
				}
				for key, expectedValue := range expectedLabels {
					actualValue, exists := sa.Labels[key]
					assert.True(t, exists, "Label %s should exist", key)
					assert.Equal(t, expectedValue, actualValue, "Label %s should have correct value", key)
				}

				// Verify owner reference
				require.Len(t, sa.OwnerReferences, 1, "Should have exactly one owner reference")
				ownerRef := sa.OwnerReferences[0]
				assert.Equal(t, grovecorev1alpha1.SchemeGroupVersion.String(), ownerRef.APIVersion)
				assert.Equal(t, "PodGangSet", ownerRef.Kind)
				assert.Equal(t, pgs.Name, ownerRef.Name)
				assert.Equal(t, pgs.UID, ownerRef.UID)
				assert.True(t, *ownerRef.Controller, "Should be marked as controller")
				assert.True(t, *ownerRef.BlockOwnerDeletion, "Should block owner deletion")

				// Verify ServiceAccount configuration
				assert.NotNil(t, sa.AutomountServiceAccountToken, "AutomountServiceAccountToken should be set")
				assert.True(t, *sa.AutomountServiceAccountToken, "AutomountServiceAccountToken should be true")
			},
		},
		{
			// Tests updating an existing ServiceAccount
			name: "update existing service account",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid-123",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
				},
			},
			existingServiceAccount: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      apicommon.GeneratePodServiceAccountName("test-pgs"),
					Namespace: "default",
					Labels: map[string]string{
						"old-label": "old-value",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         grovecorev1alpha1.SchemeGroupVersion.String(),
							Kind:               "PodGangSet",
							Name:               "test-pgs",
							UID:                "test-uid-123",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				AutomountServiceAccountToken: ptr.To(false), // This should be updated to true
			},
			expectError: false,
			validateServiceAccount: func(t *testing.T, sa *corev1.ServiceAccount, pgs *grovecorev1alpha1.PodGangSet) {
				// Verify that labels are updated correctly
				expectedLabels := map[string]string{
					apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
					apicommon.LabelPartOfKey:    pgs.Name,
					apicommon.LabelComponentKey: apicommon.LabelComponentNamePodServiceAccount,
					apicommon.LabelAppNameKey:   apicommon.GeneratePodServiceAccountName(pgs.Name),
				}
				for key, expectedValue := range expectedLabels {
					actualValue, exists := sa.Labels[key]
					assert.True(t, exists, "Label %s should exist", key)
					assert.Equal(t, expectedValue, actualValue, "Label %s should have correct value", key)
				}

				// Verify old labels are not present
				_, exists := sa.Labels["old-label"]
				assert.False(t, exists, "Old labels should be removed")

				// Verify AutomountServiceAccountToken is updated
				assert.NotNil(t, sa.AutomountServiceAccountToken, "AutomountServiceAccountToken should be set")
				assert.True(t, *sa.AutomountServiceAccountToken, "AutomountServiceAccountToken should be updated to true")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the fake client with existing ServiceAccount if provided
			clientBuilder := fake.NewClientBuilder()
			if tt.existingServiceAccount != nil {
				clientBuilder = clientBuilder.WithObjects(tt.existingServiceAccount)
			}
			fakeClient := clientBuilder.Build()

			// Setup scheme with required types
			testScheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))

			// Create the operator instance
			operator := &_resource{
				client: fakeClient,
				scheme: testScheme,
			}

			// Execute the function under test
			err := operator.Sync(context.Background(), logr.Discard(), tt.pgs)

			// Verify the results
			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				groveErr, ok := err.(*groveerr.GroveError)
				require.True(t, ok, "Expected a Grove error")
				assert.Equal(t, tt.expectedErrorCode, groveErr.Code, "Error code should match expected")
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)

				// Retrieve the ServiceAccount from the fake client
				sa := &corev1.ServiceAccount{}
				objectKey := client.ObjectKey{
					Name:      apicommon.GeneratePodServiceAccountName(tt.pgs.Name),
					Namespace: tt.pgs.Namespace,
				}
				err = fakeClient.Get(context.Background(), objectKey, sa)
				require.NoError(t, err, "ServiceAccount should exist after sync")

				// Run the validation function if provided
				if tt.validateServiceAccount != nil {
					tt.validateServiceAccount(t, sa, tt.pgs)
				}
			}
		})
	}
}

// TestDelete verifies the Delete method which removes ServiceAccount resources
// associated with a PodGangSet, handling various scenarios including missing ServiceAccounts.
func TestDelete(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata for the ServiceAccount to delete
		pgsObjMeta metav1.ObjectMeta
		// existingServiceAccount is the ServiceAccount that may already exist
		existingServiceAccount *corev1.ServiceAccount
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorCode is the expected Grove error code when an error occurs
		expectedErrorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests deleting a ServiceAccount that exists
			name: "delete existing service account",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingServiceAccount: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      apicommon.GeneratePodServiceAccountName("test-pgs"),
					Namespace: "default",
				},
			},
			expectError: false,
		},
		{
			// Tests deleting a ServiceAccount that doesn't exist (should be a no-op)
			name: "delete non-existent service account",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid-123",
			},
			existingServiceAccount: nil,
			expectError:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the fake client with existing ServiceAccount if provided
			clientBuilder := fake.NewClientBuilder()
			if tt.existingServiceAccount != nil {
				clientBuilder = clientBuilder.WithObjects(tt.existingServiceAccount)
			}
			fakeClient := clientBuilder.Build()

			// Create the operator instance
			operator := &_resource{
				client: fakeClient,
				scheme: scheme.Scheme,
			}

			// Execute the function under test
			err := operator.Delete(context.Background(), logr.Discard(), tt.pgsObjMeta)

			// Verify the results
			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				groveErr, ok := err.(*groveerr.GroveError)
				require.True(t, ok, "Expected a Grove error")
				assert.Equal(t, tt.expectedErrorCode, groveErr.Code, "Error code should match expected")
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)

				// Verify the ServiceAccount no longer exists
				sa := &corev1.ServiceAccount{}
				objectKey := client.ObjectKey{
					Name:      apicommon.GeneratePodServiceAccountName(tt.pgsObjMeta.Name),
					Namespace: tt.pgsObjMeta.Namespace,
				}
				err = fakeClient.Get(context.Background(), objectKey, sa)
				assert.True(t, errors.IsNotFound(err), "ServiceAccount should be deleted")
			}
		})
	}
}

// TestGetLabels verifies the getLabels function generates the correct labels
// for ServiceAccount resources based on PodGangSet metadata.
func TestGetLabels(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata to generate labels from
		pgsObjMeta metav1.ObjectMeta
		// expectedLabels are the labels that should be generated
		expectedLabels map[string]string
	}{
		{
			// Tests label generation for a basic PodGangSet
			name: "basic label generation",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			expectedLabels: map[string]string{
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:    "test-pgs",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodServiceAccount,
				apicommon.LabelAppNameKey:   apicommon.GeneratePodServiceAccountName("test-pgs"),
			},
		},
		{
			// Tests label generation with different PodGangSet name
			name: "different pgs name",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "my-application",
				Namespace: "production",
			},
			expectedLabels: map[string]string{
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:    "my-application",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodServiceAccount,
				apicommon.LabelAppNameKey:   apicommon.GeneratePodServiceAccountName("my-application"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			labels := getLabels(tt.pgsObjMeta)

			// Verify all expected labels are present and correct
			for key, expectedValue := range tt.expectedLabels {
				actualValue, exists := labels[key]
				assert.True(t, exists, "Label %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "Label %s should have correct value", key)
			}

			// Verify no unexpected labels are present
			assert.Len(t, labels, len(tt.expectedLabels), "Should have exactly the expected number of labels")
		})
	}
}

// TestGetObjectKey verifies the getObjectKey function generates the correct
// Kubernetes object key for ServiceAccount resources.
func TestGetObjectKey(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsObjMeta contains the PodGangSet metadata to generate object key from
		pgsObjMeta metav1.ObjectMeta
		// expectedObjectKey is the object key that should be generated
		expectedObjectKey client.ObjectKey
	}{
		{
			// Tests object key generation for a basic PodGangSet
			name: "basic object key generation",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			expectedObjectKey: client.ObjectKey{
				Name:      apicommon.GeneratePodServiceAccountName("test-pgs"),
				Namespace: "default",
			},
		},
		{
			// Tests object key generation with different namespace
			name: "different namespace",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "production-app",
				Namespace: "production",
			},
			expectedObjectKey: client.ObjectKey{
				Name:      apicommon.GeneratePodServiceAccountName("production-app"),
				Namespace: "production",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			objectKey := getObjectKey(tt.pgsObjMeta)

			// Verify the object key is correct
			assert.Equal(t, tt.expectedObjectKey.Name, objectKey.Name, "Object key name should match")
			assert.Equal(t, tt.expectedObjectKey.Namespace, objectKey.Namespace, "Object key namespace should match")
		})
	}
}

// TestEmptyServiceAccount verifies the emptyServiceAccount function creates
// a ServiceAccount with the correct basic metadata.
func TestEmptyServiceAccount(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// objKey is the object key to create the ServiceAccount with
		objKey client.ObjectKey
	}{
		{
			// Tests creating an empty ServiceAccount with basic metadata
			name: "basic empty service account",
			objKey: client.ObjectKey{
				Name:      "test-service-account",
				Namespace: "default",
			},
		},
		{
			// Tests creating an empty ServiceAccount with different metadata
			name: "different namespace and name",
			objKey: client.ObjectKey{
				Name:      "my-app-service-account",
				Namespace: "production",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			sa := emptyServiceAccount(tt.objKey)

			// Verify the ServiceAccount has correct basic metadata
			require.NotNil(t, sa, "ServiceAccount should not be nil")
			assert.Equal(t, tt.objKey.Name, sa.Name, "ServiceAccount name should match")
			assert.Equal(t, tt.objKey.Namespace, sa.Namespace, "ServiceAccount namespace should match")

			// Verify it's a clean ServiceAccount with minimal fields set
			assert.Nil(t, sa.Labels, "ServiceAccount should have no labels initially")
			assert.Nil(t, sa.Annotations, "ServiceAccount should have no annotations initially")
			assert.Empty(t, sa.OwnerReferences, "ServiceAccount should have no owner references initially")
			assert.Nil(t, sa.AutomountServiceAccountToken, "AutomountServiceAccountToken should not be set initially")
		})
	}
}

// TestBuildResource verifies the buildResource function correctly configures
// a ServiceAccount with proper labels, ownership, and settings.
func TestBuildResource(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the PodGangSet object to build the ServiceAccount for
		pgs *grovecorev1alpha1.PodGangSet
		// expectError indicates whether an error should be returned
		expectError bool
	}{
		{
			// Tests building a ServiceAccount for a basic PodGangSet
			name: "basic service account build",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid-123",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup scheme with required types
			testScheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))

			// Create the operator instance
			operator := &_resource{
				client: fake.NewClientBuilder().Build(),
				scheme: testScheme,
			}

			// Create an empty ServiceAccount to build
			objectKey := getObjectKey(tt.pgs.ObjectMeta)
			sa := emptyServiceAccount(objectKey)

			// Execute the function under test
			err := operator.buildResource(tt.pgs, sa)

			// Verify the results
			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)

				// Verify labels are set correctly
				expectedLabels := getLabels(tt.pgs.ObjectMeta)
				for key, expectedValue := range expectedLabels {
					actualValue, exists := sa.Labels[key]
					assert.True(t, exists, "Label %s should exist", key)
					assert.Equal(t, expectedValue, actualValue, "Label %s should have correct value", key)
				}

				// Verify owner reference is set
				require.Len(t, sa.OwnerReferences, 1, "Should have exactly one owner reference")
				ownerRef := sa.OwnerReferences[0]
				assert.Equal(t, grovecorev1alpha1.SchemeGroupVersion.String(), ownerRef.APIVersion)
				assert.Equal(t, "PodGangSet", ownerRef.Kind)
				assert.Equal(t, tt.pgs.Name, ownerRef.Name)
				assert.Equal(t, tt.pgs.UID, ownerRef.UID)
				assert.True(t, *ownerRef.Controller, "Should be marked as controller")
				assert.True(t, *ownerRef.BlockOwnerDeletion, "Should block owner deletion")

				// Verify AutomountServiceAccountToken is set
				assert.NotNil(t, sa.AutomountServiceAccountToken, "AutomountServiceAccountToken should be set")
				assert.True(t, *sa.AutomountServiceAccountToken, "AutomountServiceAccountToken should be true")
			}
		})
	}
}
