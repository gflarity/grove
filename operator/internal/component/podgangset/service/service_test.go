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

package service

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestNew verifies that the New constructor creates a service operator with the correct dependencies.
func TestNew(t *testing.T) {
	// Test setup with fake client and scheme
	fakeClient := fake.NewClientBuilder().Build()
	testScheme := runtime.NewScheme()

	// Execute the constructor
	operator := New(fakeClient, testScheme)

	// Verify the operator is properly initialized
	assert.NotNil(t, operator, "New should return a non-nil operator")

	// Verify the operator implements the expected interface (operator is already properly typed)
	assert.Implements(t, (*component.Operator[grovecorev1alpha1.PodGangSet])(nil), operator, "New should return an operator that implements component.Operator[PodGangSet]")
}

// TestGetExistingResourceNames tests the retrieval of existing headless service names.
func TestGetExistingResourceNames(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// PodGangSet metadata used for querying existing services
		pgsObjMeta metav1.ObjectMeta
		// Pre-existing services in the cluster to simulate various scenarios
		existingServices []corev1.Service
		// Expected service names that should be returned by the method
		expectedNames []string
		// Whether an error is expected during execution
		expectError bool
	}{
		{
			// Basic scenario with no existing services
			name: "no existing services",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       types.UID("test-uid"),
			},
			existingServices: []corev1.Service{},
			expectedNames:    []string{},
			expectError:      false,
		},
		{
			// Scenario with services that match the PodGangSet selector
			name: "services with matching labels",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       types.UID("test-uid"),
			},
			existingServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-0",
						Namespace: "default",
						Labels: map[string]string{
							apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
							apicommon.LabelPartOfKey:    "test-pgs",
							apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
						},
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-1",
						Namespace: "default",
						Labels: map[string]string{
							apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
							apicommon.LabelPartOfKey:    "test-pgs",
							apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
						},
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
			expectedNames: []string{"test-pgs-0", "test-pgs-1"},
			expectError:   false,
		},
		{
			// Scenario with services that don't match the selector (should be filtered out)
			name: "services with non-matching labels",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       types.UID("test-uid"),
			},
			existingServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-service",
						Namespace: "default",
						Labels: map[string]string{
							apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
							apicommon.LabelPartOfKey:    "other-pgs",
							apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
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
			// Setup scheme with Grove types
			testScheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, corev1.AddToScheme(testScheme))

			// Setup fake client with existing services
			clientBuilder := fake.NewClientBuilder().WithScheme(testScheme)
			for _, svc := range tt.existingServices {
				clientBuilder = clientBuilder.WithObjects(&svc)
			}
			fakeClient := clientBuilder.Build()

			// Create the service operator
			operator := New(fakeClient, testScheme)
			logger := logr.Discard()

			// Execute the method under test
			names, err := operator.GetExistingResourceNames(context.Background(), logger, tt.pgsObjMeta)

			// Verify results
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
				assert.ElementsMatch(t, tt.expectedNames, names, "Expected names don't match actual names")
			}
		})
	}
}

// TestSync tests the synchronization of headless services for a PodGangSet.
func TestSync(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// PodGangSet configuration that defines the desired state
		pgs *grovecorev1alpha1.PodGangSet
		// Pre-existing services in the cluster before sync
		existingServices []corev1.Service
		// Expected number of services after sync operation
		expectedServiceCount int
		// Whether an error is expected during sync
		expectError bool
	}{
		{
			// Test creating services for a new PodGangSet with multiple replicas
			name: "create services for new PodGangSet",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", types.UID("test-uid")).
				WithReplicas(2).
				WithPodCliqueTemplateSpec(
					testutils.NewPodCliqueTemplateSpecBuilder("worker").
						WithReplicas(1).
						Build(),
				).
				Build(),
			existingServices:     []corev1.Service{},
			expectedServiceCount: 2, // One service per replica
			expectError:          false,
		},
		{
			// Test sync with headless service configuration enabled
			name: "sync with headless service config",
			pgs: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "default", types.UID("test-uid")).
					WithReplicas(1).
					WithPodCliqueTemplateSpec(
						testutils.NewPodCliqueTemplateSpecBuilder("worker").
							WithReplicas(1).
							Build(),
					).
					Build()
				pgs.Spec.Template.HeadlessServiceConfig = &grovecorev1alpha1.HeadlessServiceConfig{
					PublishNotReadyAddresses: true,
				}
				return pgs
			}(),
			existingServices:     []corev1.Service{},
			expectedServiceCount: 1,
			expectError:          false,
		},
		{
			// Test sync with zero replicas (should create no services)
			name: "sync with zero replicas",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", types.UID("test-uid")).
				WithReplicas(0).
				WithPodCliqueTemplateSpec(
					testutils.NewPodCliqueTemplateSpecBuilder("worker").
						WithReplicas(1).
						Build(),
				).
				Build(),
			existingServices:     []corev1.Service{},
			expectedServiceCount: 0,
			expectError:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup scheme with Grove types
			testScheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, corev1.AddToScheme(testScheme))

			// Setup fake client with existing services
			clientBuilder := fake.NewClientBuilder().WithScheme(testScheme)
			for _, svc := range tt.existingServices {
				clientBuilder = clientBuilder.WithObjects(&svc)
			}
			fakeClient := clientBuilder.Build()

			// Create the service operator
			operator := New(fakeClient, testScheme)
			logger := logr.Discard()

			// Execute sync operation
			err := operator.Sync(context.Background(), logger, tt.pgs)

			// Verify results
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)

				// Verify the correct number of services were created
				serviceList := &corev1.ServiceList{}
				err = fakeClient.List(context.Background(), serviceList, client.InNamespace(tt.pgs.Namespace))
				require.NoError(t, err, "Failed to list services")
				assert.Len(t, serviceList.Items, tt.expectedServiceCount, "Unexpected number of services created")

				// Verify service properties for each created service
				for i, service := range serviceList.Items {
					assert.Equal(t, "None", service.Spec.ClusterIP, "Service should be headless")
					assert.Contains(t, service.Name, tt.pgs.Name, "Service name should contain PodGangSet name")

					// Verify labels are correctly set
					expectedLabels := getLabels(tt.pgs.Name, client.ObjectKeyFromObject(&service), i)
					for key, expectedValue := range expectedLabels {
						assert.Equal(t, expectedValue, service.Labels[key], "Label %s should match expected value", key)
					}

					// Verify owner reference is set
					assert.Len(t, service.OwnerReferences, 1, "Service should have one owner reference")
					assert.Equal(t, tt.pgs.Name, service.OwnerReferences[0].Name, "Owner reference should point to PodGangSet")
				}
			}
		})
	}
}

// TestDelete tests the deletion of all headless services associated with a PodGangSet.
func TestDelete(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// PodGangSet metadata for identifying services to delete
		pgsObjMeta metav1.ObjectMeta
		// Pre-existing services in the cluster before deletion
		existingServices []corev1.Service
		// Expected number of services remaining after deletion
		expectedRemainingServices int
		// Whether an error is expected during deletion
		expectError bool
	}{
		{
			// Test deleting services when matching services exist
			name: "delete existing services",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			existingServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-0",
						Namespace: "default",
						Labels: map[string]string{
							apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
							apicommon.LabelPartOfKey:    "test-pgs",
							apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-1",
						Namespace: "default",
						Labels: map[string]string{
							apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
							apicommon.LabelPartOfKey:    "test-pgs",
							apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
						},
					},
				},
			},
			expectedRemainingServices: 0,
			expectError:               false,
		},
		{
			// Test deletion when no matching services exist
			name: "delete with no existing services",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			existingServices:          []corev1.Service{},
			expectedRemainingServices: 0,
			expectError:               false,
		},
		{
			// Test that services from other PodGangSets are not deleted
			name: "preserve services from other PodGangSets",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
			},
			existingServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-pgs-0",
						Namespace: "default",
						Labels: map[string]string{
							apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
							apicommon.LabelPartOfKey:    "other-pgs",
							apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
						},
					},
				},
			},
			expectedRemainingServices: 1, // Service from other PodGangSet should remain
			expectError:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup scheme with Grove types
			testScheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, corev1.AddToScheme(testScheme))

			// Setup fake client with existing services
			clientBuilder := fake.NewClientBuilder().WithScheme(testScheme)
			for _, svc := range tt.existingServices {
				clientBuilder = clientBuilder.WithObjects(&svc)
			}
			fakeClient := clientBuilder.Build()

			// Create the service operator
			operator := New(fakeClient, testScheme)
			logger := logr.Discard()

			// Execute delete operation
			err := operator.Delete(context.Background(), logger, tt.pgsObjMeta)

			// Verify results
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)

				// Verify the correct number of services remain
				serviceList := &corev1.ServiceList{}
				err = fakeClient.List(context.Background(), serviceList, client.InNamespace(tt.pgsObjMeta.Namespace))
				require.NoError(t, err, "Failed to list services")
				assert.Len(t, serviceList.Items, tt.expectedRemainingServices, "Unexpected number of services remaining")
			}
		})
	}
}

// TestGetLabels tests the label generation for headless services.
func TestGetLabels(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// PodGangSet name used in label generation
		pgsName string
		// Service object key containing name and namespace
		svcObjectKey client.ObjectKey
		// Replica index for the PodGangSet replica
		pgsReplicaIndex int
		// Expected labels that should be generated
		expectedLabels map[string]string
	}{
		{
			// Test label generation for first replica
			name:    "labels for first replica",
			pgsName: "test-pgs",
			svcObjectKey: client.ObjectKey{
				Name:      "test-pgs-0",
				Namespace: "default",
			},
			pgsReplicaIndex: 0,
			expectedLabels: map[string]string{
				apicommon.LabelManagedByKey:           apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:              "test-pgs",
				apicommon.LabelAppNameKey:             "test-pgs-0",
				apicommon.LabelComponentKey:           apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
				apicommon.LabelPodGangSetReplicaIndex: "0",
			},
		},
		{
			// Test label generation for higher replica index
			name:    "labels for higher replica index",
			pgsName: "my-workload",
			svcObjectKey: client.ObjectKey{
				Name:      "my-workload-5",
				Namespace: "production",
			},
			pgsReplicaIndex: 5,
			expectedLabels: map[string]string{
				apicommon.LabelManagedByKey:           apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:              "my-workload",
				apicommon.LabelAppNameKey:             "my-workload-5",
				apicommon.LabelComponentKey:           apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
				apicommon.LabelPodGangSetReplicaIndex: "5",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute label generation
			actualLabels := getLabels(tt.pgsName, tt.svcObjectKey, tt.pgsReplicaIndex)

			// Verify all expected labels are present with correct values
			for key, expectedValue := range tt.expectedLabels {
				actualValue, exists := actualLabels[key]
				assert.True(t, exists, "Expected label %s to exist", key)
				assert.Equal(t, expectedValue, actualValue, "Label %s should have value %s", key, expectedValue)
			}

			// Verify no unexpected labels are present
			assert.Len(t, actualLabels, len(tt.expectedLabels), "Should have exactly the expected number of labels")
		})
	}
}

// TestGetLabelSelectorForPodsInAPodGangSetReplica tests pod selector label generation.
func TestGetLabelSelectorForPodsInAPodGangSetReplica(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// PodGangSet name used in selector generation
		pgsName string
		// Replica index for the PodGangSet replica
		pgsReplicaIndex int
		// Expected selector labels for pod selection
		expectedSelector map[string]string
	}{
		{
			// Test selector for first replica
			name:            "selector for first replica",
			pgsName:         "test-pgs",
			pgsReplicaIndex: 0,
			expectedSelector: map[string]string{
				apicommon.LabelManagedByKey:           apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:              "test-pgs",
				apicommon.LabelPodGangSetReplicaIndex: "0",
			},
		},
		{
			// Test selector for higher replica index
			name:            "selector for higher replica index",
			pgsName:         "my-workload",
			pgsReplicaIndex: 3,
			expectedSelector: map[string]string{
				apicommon.LabelManagedByKey:           apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:              "my-workload",
				apicommon.LabelPodGangSetReplicaIndex: "3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute selector generation
			actualSelector := getLabelSelectorForPodsInAPodGangSetReplica(tt.pgsName, tt.pgsReplicaIndex)

			// Verify all expected selector labels are present
			for key, expectedValue := range tt.expectedSelector {
				actualValue, exists := actualSelector[key]
				assert.True(t, exists, "Expected selector label %s to exist", key)
				assert.Equal(t, expectedValue, actualValue, "Selector label %s should have value %s", key, expectedValue)
			}

			// Verify no unexpected labels are present
			assert.Len(t, actualSelector, len(tt.expectedSelector), "Should have exactly the expected number of selector labels")
		})
	}
}

// TestGetSelectorLabelsForAllHeadlessServices tests service selector label generation.
func TestGetSelectorLabelsForAllHeadlessServices(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// PodGangSet name used in selector generation
		pgsName string
		// Expected selector labels for service selection
		expectedSelector map[string]string
	}{
		{
			// Test selector for basic PodGangSet
			name:    "selector for basic PodGangSet",
			pgsName: "test-pgs",
			expectedSelector: map[string]string{
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:    "test-pgs",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
			},
		},
		{
			// Test selector for PodGangSet with complex name
			name:    "selector for complex PodGangSet name",
			pgsName: "my-complex-workload-v2",
			expectedSelector: map[string]string{
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:    "my-complex-workload-v2",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGangSetReplicaHeadlessService,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute selector generation
			actualSelector := getSelectorLabelsForAllHeadlessServices(tt.pgsName)

			// Verify all expected selector labels are present
			for key, expectedValue := range tt.expectedSelector {
				actualValue, exists := actualSelector[key]
				assert.True(t, exists, "Expected selector label %s to exist", key)
				assert.Equal(t, expectedValue, actualValue, "Selector label %s should have value %s", key, expectedValue)
			}

			// Verify no unexpected labels are present
			assert.Len(t, actualSelector, len(tt.expectedSelector), "Should have exactly the expected number of selector labels")
		})
	}
}

// TestGetObjectKeys tests the generation of object keys for headless services.
func TestGetObjectKeys(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// PodGangSet configuration used for object key generation
		pgs *grovecorev1alpha1.PodGangSet
		// Expected object keys that should be generated
		expectedKeys []client.ObjectKey
	}{
		{
			// Test object key generation for single replica
			name: "single replica PodGangSet",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", types.UID("test-uid")).
				WithReplicas(1).
				Build(),
			expectedKeys: []client.ObjectKey{
				{Name: "test-pgs-0", Namespace: "default"},
			},
		},
		{
			// Test object key generation for multiple replicas
			name: "multiple replica PodGangSet",
			pgs: testutils.NewPodGangSetBuilder("my-workload", "production", types.UID("test-uid")).
				WithReplicas(3).
				Build(),
			expectedKeys: []client.ObjectKey{
				{Name: "my-workload-0", Namespace: "production"},
				{Name: "my-workload-1", Namespace: "production"},
				{Name: "my-workload-2", Namespace: "production"},
			},
		},
		{
			// Test object key generation for zero replicas
			name: "zero replica PodGangSet",
			pgs: testutils.NewPodGangSetBuilder("empty-pgs", "default", types.UID("test-uid")).
				WithReplicas(0).
				Build(),
			expectedKeys: []client.ObjectKey{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute object key generation
			actualKeys := getObjectKeys(tt.pgs)

			// Verify the correct number of keys are generated
			assert.Len(t, actualKeys, len(tt.expectedKeys), "Should generate the expected number of object keys")

			// Verify each expected key is present
			for i, expectedKey := range tt.expectedKeys {
				assert.Equal(t, expectedKey, actualKeys[i], "Object key at index %d should match expected", i)
			}
		})
	}
}

// TestEmptyPGService tests the creation of empty service objects.
func TestEmptyPGService(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// Object key used for service creation
		objKey client.ObjectKey
		// Expected service name after creation
		expectedName string
		// Expected service namespace after creation
		expectedNamespace string
	}{
		{
			// Test empty service creation with basic object key
			name: "create empty service with basic key",
			objKey: client.ObjectKey{
				Name:      "test-service",
				Namespace: "default",
			},
			expectedName:      "test-service",
			expectedNamespace: "default",
		},
		{
			// Test empty service creation with complex object key
			name: "create empty service with complex key",
			objKey: client.ObjectKey{
				Name:      "my-complex-service-name",
				Namespace: "production",
			},
			expectedName:      "my-complex-service-name",
			expectedNamespace: "production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute empty service creation
			service := emptyPGService(tt.objKey)

			// Verify service properties
			assert.NotNil(t, service, "Should return a non-nil service")
			assert.Equal(t, tt.expectedName, service.Name, "Service name should match expected")
			assert.Equal(t, tt.expectedNamespace, service.Namespace, "Service namespace should match expected")

			// Verify service is properly initialized but empty
			assert.Empty(t, service.Labels, "Service should have no labels initially")
			assert.Empty(t, service.Spec.Selector, "Service should have no selector initially")
			assert.Equal(t, "", service.Spec.ClusterIP, "Service should have no ClusterIP initially")
		})
	}
}

// TestBuildResource tests the service specification building logic.
func TestBuildResource(t *testing.T) {
	tests := []struct {
		// Test case name for identification
		name string
		// PodGangSet configuration used for building the service
		pgs *grovecorev1alpha1.PodGangSet
		// Replica index for the service being built
		pgsReplicaIndex int
		// Whether PublishNotReadyAddresses should be enabled
		expectPublishNotReady bool
		// Whether an error is expected during build
		expectError bool
	}{
		{
			// Test building service without headless service config
			name: "build service without headless config",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "default", types.UID("test-uid")).
				WithReplicas(1).
				WithPodCliqueTemplateSpec(
					testutils.NewPodCliqueTemplateSpecBuilder("worker").
						WithReplicas(1).
						Build(),
				).
				Build(),
			pgsReplicaIndex:       0,
			expectPublishNotReady: false, // Default value when config is nil
			expectError:           false,
		},
		{
			// Test building service with headless service config enabled
			name: "build service with headless config enabled",
			pgs: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "default", types.UID("test-uid")).
					WithReplicas(1).
					WithPodCliqueTemplateSpec(
						testutils.NewPodCliqueTemplateSpecBuilder("worker").
							WithReplicas(1).
							Build(),
					).
					Build()
				pgs.Spec.Template.HeadlessServiceConfig = &grovecorev1alpha1.HeadlessServiceConfig{
					PublishNotReadyAddresses: true,
				}
				return pgs
			}(),
			pgsReplicaIndex:       0,
			expectPublishNotReady: true,
			expectError:           false,
		},
		{
			// Test building service with headless service config disabled
			name: "build service with headless config disabled",
			pgs: func() *grovecorev1alpha1.PodGangSet {
				pgs := testutils.NewPodGangSetBuilder("test-pgs", "default", types.UID("test-uid")).
					WithReplicas(1).
					WithPodCliqueTemplateSpec(
						testutils.NewPodCliqueTemplateSpecBuilder("worker").
							WithReplicas(1).
							Build(),
					).
					Build()
				pgs.Spec.Template.HeadlessServiceConfig = &grovecorev1alpha1.HeadlessServiceConfig{
					PublishNotReadyAddresses: false,
				}
				return pgs
			}(),
			pgsReplicaIndex:       0,
			expectPublishNotReady: false,
			expectError:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup service operator with scheme
			testScheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, corev1.AddToScheme(testScheme))

			operator := &_resource{
				client: fake.NewClientBuilder().Build(),
				scheme: testScheme,
			}

			// Create empty service for building
			serviceName := apicommon.GenerateHeadlessServiceName(apicommon.ResourceNameReplica{
				Name:    tt.pgs.Name,
				Replica: tt.pgsReplicaIndex,
			})
			service := emptyPGService(client.ObjectKey{
				Name:      serviceName,
				Namespace: tt.pgs.Namespace,
			})

			// Execute build operation
			err := operator.buildResource(service, tt.pgs, tt.pgsReplicaIndex)

			// Verify results
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)

				// Verify service specification
				assert.Equal(t, "None", service.Spec.ClusterIP, "Service should be headless")
				assert.Equal(t, tt.expectPublishNotReady, service.Spec.PublishNotReadyAddresses, "PublishNotReadyAddresses should match expected")

				// Verify labels are set correctly
				expectedLabels := getLabels(tt.pgs.Name, client.ObjectKeyFromObject(service), tt.pgsReplicaIndex)
				for key, expectedValue := range expectedLabels {
					assert.Equal(t, expectedValue, service.Labels[key], "Label %s should match expected value", key)
				}

				// Verify selector is set correctly
				expectedSelector := getLabelSelectorForPodsInAPodGangSetReplica(tt.pgs.Name, tt.pgsReplicaIndex)
				for key, expectedValue := range expectedSelector {
					assert.Equal(t, expectedValue, service.Spec.Selector[key], "Selector %s should match expected value", key)
				}

				// Verify owner reference is set
				assert.Len(t, service.OwnerReferences, 1, "Service should have one owner reference")
				assert.Equal(t, tt.pgs.Name, service.OwnerReferences[0].Name, "Owner reference should point to PodGangSet")
				assert.True(t, *service.OwnerReferences[0].Controller, "Owner reference should be a controller reference")
			}
		})
	}
}
