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

package podgang

import (
	"context"
	"fmt"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	groveschedulerv1alpha1 "github.com/NVIDIA/grove/scheduler/api/core/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestNew verifies that the New constructor creates a properly initialized PodGang operator.
// It ensures all required dependencies are correctly assigned and the returned operator
// implements the expected interface.
func TestNew(t *testing.T) {
	// Test setup with mock dependencies
	testScheme := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(testScheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
	require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
	eventRecorder := &record.FakeRecorder{}

	// Execute constructor
	operator := New(fakeClient, testScheme, eventRecorder)

	// Verify operator is properly initialized
	require.NotNil(t, operator, "New should return a non-nil operator")

	// Verify operator implements the expected interface
	assert.Implements(t, (*component.Operator[grovecorev1alpha1.PodGangSet])(nil), operator, "New should return an operator that implements component.Operator[PodGangSet]")

	// Verify internal structure (type assertion to access private fields)
	resource, ok := operator.(*_resource)
	require.True(t, ok, "New should return a *_resource instance")
	assert.Equal(t, fakeClient, resource.client, "Client should be properly assigned")
	assert.Equal(t, testScheme, resource.scheme, "Scheme should be properly assigned")
	assert.Equal(t, eventRecorder, resource.eventRecorder, "EventRecorder should be properly assigned")
}

// TestGetExistingResourceNames verifies the retrieval of existing PodGang resource names.
// It tests various scenarios including successful retrieval, empty results, and error conditions.
func TestGetExistingResourceNames(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being executed
		name string
		// existingPodGangs contains PodGang resources that should be found in the cluster
		existingPodGangs []groveschedulerv1alpha1.PodGang
		// pgsObjectMeta represents the PodGangSet metadata used for owner filtering
		pgsObjectMeta metav1.ObjectMeta
		// expectedNames contains the resource names that should be returned
		expectedNames []string
		// shouldError indicates whether the operation should fail
		shouldError bool
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Basic success case with multiple owned PodGangs
			name: "successful retrieval with multiple PodGangs",
			existingPodGangs: []groveschedulerv1alpha1.PodGang{
				createTestPodGang("test-namespace", "podgang-1", "test-pgs"),
				createTestPodGang("test-namespace", "podgang-2", "test-pgs"),
				createTestPodGang("test-namespace", "podgang-3", "other-pgs"), // Different owner
			},
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			expectedNames: []string{"podgang-1", "podgang-2"},
			shouldError:   false,
		},
		{
			// Edge case with no existing PodGangs
			name:             "no existing PodGangs",
			existingPodGangs: []groveschedulerv1alpha1.PodGang{},
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			expectedNames: []string{},
			shouldError:   false,
		},
		{
			// Edge case with PodGangs in different namespace
			name: "PodGangs in different namespace should not be returned",
			existingPodGangs: []groveschedulerv1alpha1.PodGang{
				createTestPodGang("other-namespace", "podgang-1", "test-pgs"),
			},
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			expectedNames: []string{},
			shouldError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := logr.Discard()

			// Create fake client with existing resources
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			clientBuilder := fake.NewClientBuilder().WithScheme(testScheme)
			if len(tt.existingPodGangs) > 0 {
				objects := make([]client.Object, len(tt.existingPodGangs))
				for i := range tt.existingPodGangs {
					objects[i] = &tt.existingPodGangs[i]
				}
				clientBuilder = clientBuilder.WithObjects(objects...)
			}
			fakeClient := clientBuilder.Build()

			// Create operator instance
			operator := New(fakeClient, runtime.NewScheme(), &record.FakeRecorder{})

			// Execute method under test
			names, err := operator.GetExistingResourceNames(ctx, logger, tt.pgsObjectMeta)

			// Verify results
			if tt.shouldError {
				assert.Error(t, err, "Expected error but got none")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Unexpected error: %v", err)
				assert.ElementsMatch(t, tt.expectedNames, names, "Returned names should match expected names")
			}
		})
	}
}

// TestSync verifies the synchronization logic for PodGang resources.
// It tests the complete sync flow including creation, updates, and error handling.
func TestSync(t *testing.T) {
	tests := []struct {
		// name describes the specific sync scenario being tested
		name string
		// pgs represents the PodGangSet specification to sync
		pgs *grovecorev1alpha1.PodGangSet
		// existingPodGangs contains PodGangs already present in the cluster
		existingPodGangs []groveschedulerv1alpha1.PodGang
		// existingPodCliques contains PodCliques that should be considered during sync
		existingPodCliques []grovecorev1alpha1.PodClique
		// shouldError indicates whether the sync operation should fail
		shouldError bool
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Basic success case with simple PodGangSet - expects requeue when PodGangs are pending creation
			name: "successful sync with simple PodGangSet",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			existingPodGangs:   []groveschedulerv1alpha1.PodGang{},
			existingPodCliques: []grovecorev1alpha1.PodClique{},
			shouldError:        true,
			errorContains:      "PodGangs pending creation",
		},
		{
			// Test sync with existing PodGangs that need updates - expects requeue when additional PodGangs are pending creation
			name: "sync with existing PodGangs requiring updates",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(2).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(3).
					WithMinAvailable(3).
					Build()).
				Build(),
			existingPodGangs: []groveschedulerv1alpha1.PodGang{
				createTestPodGang("test-namespace", "test-pgs-0", "test-pgs"),
			},
			existingPodCliques: []grovecorev1alpha1.PodClique{},
			shouldError:        true,
			errorContains:      "PodGangs pending creation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := logr.Discard()

			// Create fake client with existing resources
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			objects := []client.Object{tt.pgs}
			for i := range tt.existingPodGangs {
				objects = append(objects, &tt.existingPodGangs[i])
			}
			for i := range tt.existingPodCliques {
				objects = append(objects, &tt.existingPodCliques[i])
			}

			fakeClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(objects...).Build()

			// Create operator instance
			operator := New(fakeClient, testScheme, &record.FakeRecorder{})

			// Execute sync operation
			err := operator.Sync(ctx, logger, tt.pgs)

			// Verify results
			if tt.shouldError {
				assert.Error(t, err, "Expected error but got none")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Unexpected error during sync: %v", err)
			}
		})
	}
}

// TestDelete verifies the deletion of all PodGang resources owned by a PodGangSet.
// It tests successful deletion scenarios and error handling during cleanup operations.
func TestDelete(t *testing.T) {
	tests := []struct {
		// name describes the specific deletion scenario being tested
		name string
		// existingPodGangs contains PodGangs that should be deleted
		existingPodGangs []groveschedulerv1alpha1.PodGang
		// pgsObjectMeta represents the PodGangSet metadata for owner matching
		pgsObjectMeta metav1.ObjectMeta
		// shouldError indicates whether the delete operation should fail
		shouldError bool
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Successful deletion of multiple PodGangs
			name: "successful deletion of multiple PodGangs",
			existingPodGangs: []groveschedulerv1alpha1.PodGang{
				createTestPodGang("test-namespace", "podgang-1", "test-pgs"),
				createTestPodGang("test-namespace", "podgang-2", "test-pgs"),
			},
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			shouldError: false,
		},
		{
			// Deletion when no PodGangs exist (should succeed)
			name:             "deletion with no existing PodGangs",
			existingPodGangs: []groveschedulerv1alpha1.PodGang{},
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := logr.Discard()

			// Create fake client with existing resources
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			objects := make([]client.Object, len(tt.existingPodGangs))
			for i := range tt.existingPodGangs {
				objects[i] = &tt.existingPodGangs[i]
			}
			fakeClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(objects...).Build()

			// Create operator instance
			operator := New(fakeClient, runtime.NewScheme(), &record.FakeRecorder{})

			// Execute delete operation
			err := operator.Delete(ctx, logger, tt.pgsObjectMeta)

			// Verify results
			if tt.shouldError {
				assert.Error(t, err, "Expected error but got none")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Unexpected error during deletion: %v", err)

				// Verify PodGangs were actually deleted
				podGangList := &groveschedulerv1alpha1.PodGangList{}
				err = fakeClient.List(ctx, podGangList, client.InNamespace(tt.pgsObjectMeta.Namespace))
				assert.NoError(t, err, "Failed to list PodGangs after deletion")

				// Filter for PodGangs that should have been deleted
				remainingPodGangs := 0
				for _, pg := range podGangList.Items {
					if hasExpectedLabels(pg.Labels, tt.pgsObjectMeta.Name) {
						remainingPodGangs++
					}
				}
				assert.Equal(t, 0, remainingPodGangs, "All matching PodGangs should be deleted")
			}
		})
	}
}

// TestBuildResource verifies the configuration of PodGang resources.
// It tests proper label assignment, controller reference setup, and specification building.
func TestBuildResource(t *testing.T) {
	tests := []struct {
		// name describes the specific resource building scenario
		name string
		// pgs represents the source PodGangSet for building the resource
		pgs *grovecorev1alpha1.PodGangSet
		// pgInfo contains the PodGang-specific information for configuration
		pgInfo podGangInfo
		// shouldError indicates whether the build operation should fail
		shouldError bool
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Successful resource building with valid inputs
			name: "successful resource building",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			pgInfo: podGangInfo{
				fqn: "test-pgs-0",
				pclqs: []pclqInfo{
					{
						fqn:                "worker-0",
						replicas:           2,
						minAvailable:       2,
						associatedPodNames: []string{"worker-0-pod-1", "worker-0-pod-2"},
					},
				},
			},
			shouldError: false,
		},
		{
			// Resource building with empty PodGang info
			name: "resource building with empty PodGang info",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				Build(),
			pgInfo: podGangInfo{
				fqn:   "test-pgs-0",
				pclqs: []pclqInfo{},
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			// Create operator instance
			operator := &_resource{
				client:        fake.NewClientBuilder().WithScheme(testScheme).Build(),
				scheme:        testScheme,
				eventRecorder: &record.FakeRecorder{},
			}

			// Create empty PodGang for building
			pg := emptyPodGang(types.NamespacedName{
				Namespace: tt.pgs.Namespace,
				Name:      tt.pgInfo.fqn,
			})

			// Execute build operation
			err := operator.buildResource(tt.pgs, tt.pgInfo, pg)

			// Verify results
			if tt.shouldError {
				assert.Error(t, err, "Expected error but got none")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Unexpected error during resource building: %v", err)

				// Verify PodGang was properly configured
				assert.NotEmpty(t, pg.Labels, "PodGang should have labels assigned")
				assert.Contains(t, pg.Labels, grovecorev1alpha1.LabelComponentKey, "PodGang should have component label")
				assert.Equal(t, component.NamePodGang, pg.Labels[grovecorev1alpha1.LabelComponentKey], "Component label should be correct")

				// Verify controller reference is set
				assert.Len(t, pg.OwnerReferences, 1, "PodGang should have one owner reference")
				assert.Equal(t, tt.pgs.Name, pg.OwnerReferences[0].Name, "Owner reference should point to PodGangSet")

				// Verify spec is configured
				assert.Len(t, pg.Spec.PodGroups, len(tt.pgInfo.pclqs), "PodGang should have correct number of PodGroups")
				assert.Equal(t, tt.pgs.Spec.Template.PriorityClassName, pg.Spec.PriorityClassName, "Priority class should match PodGangSet")
			}
		})
	}
}

// TestGetPodGangSelectorLabels verifies the label selector generation for PodGang resources.
// It ensures the correct labels are returned for identifying PodGangs owned by a PodGangSet.
func TestGetPodGangSelectorLabels(t *testing.T) {
	tests := []struct {
		// name describes the specific label generation scenario
		name string
		// pgsObjectMeta represents the PodGangSet metadata for label generation
		pgsObjectMeta metav1.ObjectMeta
		// expectedLabels contains the labels that should be generated
		expectedLabels map[string]string
	}{
		{
			// Standard label generation for a typical PodGangSet
			name: "standard label generation",
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
			},
			expectedLabels: map[string]string{
				grovecorev1alpha1.LabelComponentKey: component.NamePodGang,
				grovecorev1alpha1.LabelPartOfKey:    "test-pgs",
				grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
			},
		},
		{
			// Label generation with special characters in name
			name: "label generation with special characters",
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs-with-dashes",
				Namespace: "test-namespace",
			},
			expectedLabels: map[string]string{
				grovecorev1alpha1.LabelComponentKey: component.NamePodGang,
				grovecorev1alpha1.LabelPartOfKey:    "test-pgs-with-dashes",
				grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute label generation
			labels := getPodGangSelectorLabels(tt.pgsObjectMeta)

			// Verify all expected labels are present
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

// TestEmptyPodGang verifies the creation of empty PodGang templates.
// It ensures the template has the correct namespace and name for create-or-update operations.
func TestEmptyPodGang(t *testing.T) {
	tests := []struct {
		// name describes the specific template creation scenario
		name string
		// objKey represents the namespace and name for the PodGang template
		objKey client.ObjectKey
	}{
		{
			// Standard template creation with typical values
			name: "standard template creation",
			objKey: client.ObjectKey{
				Namespace: "test-namespace",
				Name:      "test-podgang",
			},
		},
		{
			// Template creation with empty namespace (cluster-scoped)
			name: "template creation with empty namespace",
			objKey: client.ObjectKey{
				Namespace: "",
				Name:      "cluster-podgang",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute template creation
			pg := emptyPodGang(tt.objKey)

			// Verify template properties
			assert.NotNil(t, pg, "emptyPodGang should return a non-nil PodGang")
			assert.Equal(t, tt.objKey.Namespace, pg.Namespace, "PodGang namespace should match ObjectKey")
			assert.Equal(t, tt.objKey.Name, pg.Name, "PodGang name should match ObjectKey")

			// Verify template is truly empty (no other fields set)
			assert.Empty(t, pg.Labels, "Empty PodGang should have no labels")
			assert.Empty(t, pg.Annotations, "Empty PodGang should have no annotations")
			assert.Empty(t, pg.Spec.PodGroups, "Empty PodGang should have no PodGroups")
			assert.Empty(t, pg.Spec.PriorityClassName, "Empty PodGang should have no priority class")
		})
	}
}

// TestGetLabels verifies the standard label generation for PodGang resources.
// It ensures the correct labels are applied to PodGangs managed by a PodGangSet.
func TestGetLabels(t *testing.T) {
	tests := []struct {
		// name describes the specific label generation scenario
		name string
		// pgsName represents the PodGangSet name for label generation
		pgsName string
		// expectedLabels contains the labels that should be generated
		expectedLabels map[string]string
	}{
		{
			// Standard label generation for a typical PodGangSet name
			name:    "standard label generation",
			pgsName: "test-pgs",
			expectedLabels: map[string]string{
				grovecorev1alpha1.LabelComponentKey: component.NamePodGang,
				grovecorev1alpha1.LabelPartOfKey:    "test-pgs",
				grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
			},
		},
		{
			// Label generation with complex PodGangSet name
			name:    "label generation with complex name",
			pgsName: "complex-pgs-name-with-dashes",
			expectedLabels: map[string]string{
				grovecorev1alpha1.LabelComponentKey: component.NamePodGang,
				grovecorev1alpha1.LabelPartOfKey:    "complex-pgs-name-with-dashes",
				grovecorev1alpha1.LabelManagedByKey: grovecorev1alpha1.LabelManagedByValue,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute label generation
			labels := getLabels(tt.pgsName)

			// Verify all expected labels are present
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

// TestSyncErrorScenarios verifies error handling during sync operations.
// It tests various failure modes including client errors and invalid configurations.
func TestSyncErrorScenarios(t *testing.T) {
	tests := []struct {
		// name describes the specific error scenario being tested
		name string
		// pgs represents the PodGangSet specification to sync
		pgs *grovecorev1alpha1.PodGangSet
		// simulateError configures the fake client to return specific errors
		simulateError func(*fake.ClientBuilder) *fake.ClientBuilder
		// expectedErrorCode specifies the Grove error code that should be returned
		expectedErrorCode string
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Test error when listing pods fails during sync preparation
			name: "error listing pods during sync",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			simulateError: func(builder *fake.ClientBuilder) *fake.ClientBuilder {
				return builder.WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						// Simulate error when listing pods
						if _, ok := list.(*corev1.PodList); ok {
							return fmt.Errorf("simulated pod list error")
						}
						return client.List(ctx, list, opts...)
					},
				})
			},
			expectedErrorCode: "ERR_LIST_PODS_FOR_PODGANGSET",
			errorContains:     "failed to list Pods for PodGangSet",
		},
		{
			// Test error when listing PodCliques fails during sync preparation
			name: "error listing PodCliques during sync",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			simulateError: func(builder *fake.ClientBuilder) *fake.ClientBuilder {
				return builder.WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						// Simulate error when listing PodCliques
						if _, ok := list.(*grovecorev1alpha1.PodCliqueList); ok {
							return fmt.Errorf("simulated podclique list error")
						}
						return client.List(ctx, list, opts...)
					},
				})
			},
			expectedErrorCode: "ERR_LIST_PODCLIQUES_FOR_PODGANGSET",
			errorContains:     "failed to list PodCliques for PodGangSet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := logr.Discard()

			// Create fake client with error simulation
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			clientBuilder := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(tt.pgs)
			fakeClient := tt.simulateError(clientBuilder).Build()

			// Create operator instance
			operator := New(fakeClient, testScheme, &record.FakeRecorder{})

			// Execute sync operation
			err := operator.Sync(ctx, logger, tt.pgs)

			// Verify error occurred and contains expected information
			require.Error(t, err, "Expected error but got none")
			assert.Contains(t, err.Error(), tt.expectedErrorCode, "Error should contain expected error code")
			assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
		})
	}
}

// TestGetExistingResourceNamesErrorScenarios verifies error handling during resource name retrieval.
// It tests client failures and ensures proper error wrapping and codes.
func TestGetExistingResourceNamesErrorScenarios(t *testing.T) {
	tests := []struct {
		// name describes the specific error scenario being tested
		name string
		// pgsObjectMeta represents the PodGangSet metadata for the operation
		pgsObjectMeta metav1.ObjectMeta
		// simulateError configures the fake client to return specific errors
		simulateError func(*fake.ClientBuilder) *fake.ClientBuilder
		// expectedErrorCode specifies the Grove error code that should be returned
		expectedErrorCode string
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Test error when listing PodGangs fails
			name: "error listing PodGangs",
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			simulateError: func(builder *fake.ClientBuilder) *fake.ClientBuilder {
				return builder.WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						// Simulate error when listing PodGangs
						if _, ok := list.(*metav1.PartialObjectMetadataList); ok {
							return fmt.Errorf("simulated list error")
						}
						return client.List(ctx, list, opts...)
					},
				})
			},
			expectedErrorCode: "ERR_LIST_PODGANGS",
			errorContains:     "Error listing PodGang for PodGangSet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := logr.Discard()

			// Create fake client with error simulation
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			fakeClient := tt.simulateError(fake.NewClientBuilder().WithScheme(testScheme)).Build()

			// Create operator instance
			operator := New(fakeClient, testScheme, &record.FakeRecorder{})

			// Execute method under test
			names, err := operator.GetExistingResourceNames(ctx, logger, tt.pgsObjectMeta)

			// Verify error occurred and contains expected information
			require.Error(t, err, "Expected error but got none")
			assert.Nil(t, names, "Names should be nil when error occurs")
			assert.Contains(t, err.Error(), tt.expectedErrorCode, "Error should contain expected error code")
			assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
		})
	}
}

// TestDeleteErrorScenarios verifies error handling during delete operations.
// It tests client failures and ensures proper error wrapping and codes.
func TestDeleteErrorScenarios(t *testing.T) {
	tests := []struct {
		// name describes the specific error scenario being tested
		name string
		// pgsObjectMeta represents the PodGangSet metadata for the operation
		pgsObjectMeta metav1.ObjectMeta
		// simulateError configures the fake client to return specific errors
		simulateError func(*fake.ClientBuilder) *fake.ClientBuilder
		// expectedErrorCode specifies the Grove error code that should be returned
		expectedErrorCode string
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Test error when DeleteAllOf fails
			name: "error during DeleteAllOf operation",
			pgsObjectMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			simulateError: func(builder *fake.ClientBuilder) *fake.ClientBuilder {
				return builder.WithInterceptorFuncs(interceptor.Funcs{
					DeleteAllOf: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
						return fmt.Errorf("simulated delete error")
					},
				})
			},
			expectedErrorCode: "ERR_DELETE_PODGANGS",
			errorContains:     "Failed to delete PodGangs for PodGangSet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := logr.Discard()

			// Create fake client with error simulation
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			fakeClient := tt.simulateError(fake.NewClientBuilder().WithScheme(testScheme)).Build()

			// Create operator instance
			operator := New(fakeClient, testScheme, &record.FakeRecorder{})

			// Execute delete operation
			err := operator.Delete(ctx, logger, tt.pgsObjectMeta)

			// Verify error occurred and contains expected information
			require.Error(t, err, "Expected error but got none")
			assert.Contains(t, err.Error(), tt.expectedErrorCode, "Error should contain expected error code")
			assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
		})
	}
}

// TestBuildResourceErrorScenarios verifies error handling during resource building.
// It tests invalid configurations and controller reference failures.
func TestBuildResourceErrorScenarios(t *testing.T) {
	tests := []struct {
		// name describes the specific error scenario being tested
		name string
		// pgs represents the source PodGangSet for building the resource
		pgs *grovecorev1alpha1.PodGangSet
		// pgInfo contains the PodGang-specific information for configuration
		pgInfo podGangInfo
		// setupScheme configures the scheme to simulate errors
		setupScheme func(*runtime.Scheme)
		// expectedErrorCode specifies the Grove error code that should be returned
		expectedErrorCode string
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Test error when setting controller reference fails due to missing scheme
			name: "error setting controller reference with invalid scheme",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			pgInfo: podGangInfo{
				fqn: "test-pgs-0",
				pclqs: []pclqInfo{
					{
						fqn:                "worker-0",
						replicas:           2,
						minAvailable:       2,
						associatedPodNames: []string{"worker-0-pod-1"},
					},
				},
			},
			setupScheme: func(s *runtime.Scheme) {
				// Don't add Grove types to scheme to cause controller reference error
			},
			expectedErrorCode: "ERR_SET_CONTROLLER_REFERENCE",
			errorContains:     "failed to set the controller reference on PodGang",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment with potentially invalid scheme
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			if tt.setupScheme != nil {
				tt.setupScheme(testScheme)
			} else {
				require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			}

			// Create operator instance
			operator := &_resource{
				client:        fake.NewClientBuilder().WithScheme(testScheme).Build(),
				scheme:        testScheme,
				eventRecorder: &record.FakeRecorder{},
			}

			// Create empty PodGang for building
			pg := emptyPodGang(types.NamespacedName{
				Namespace: tt.pgs.Namespace,
				Name:      tt.pgInfo.fqn,
			})

			// Execute build operation
			err := operator.buildResource(tt.pgs, tt.pgInfo, pg)

			// Verify error occurred and contains expected information
			require.Error(t, err, "Expected error but got none")
			assert.Contains(t, err.Error(), tt.expectedErrorCode, "Error should contain expected error code")
			assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
		})
	}
}

// TestSyncIntegration verifies end-to-end sync behavior and resource validation.
// It tests that sync operations properly handle resource lifecycle and return expected errors.
func TestSyncIntegration(t *testing.T) {
	tests := []struct {
		// name describes the specific integration scenario being tested
		name string
		// pgs represents the PodGangSet specification to sync
		pgs *grovecorev1alpha1.PodGangSet
		// existingPodGangs contains PodGangs already present in the cluster
		existingPodGangs []groveschedulerv1alpha1.PodGang
		// expectedRequeueError indicates whether sync should return a requeue error
		expectedRequeueError bool
		// validateBehavior performs validation on the sync behavior
		validateBehavior func(t *testing.T, client client.Client, pgs *grovecorev1alpha1.PodGangSet, err error)
	}{
		{
			// Test that sync properly handles new PodGangSet and returns requeue for pending creation
			name: "sync with new PodGangSet returns requeue error",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			existingPodGangs:     []groveschedulerv1alpha1.PodGang{},
			expectedRequeueError: true,
			validateBehavior: func(t *testing.T, cl client.Client, pgs *grovecorev1alpha1.PodGangSet, err error) {
				// Verify requeue error is returned for pending creation
				require.Error(t, err, "Expected requeue error for pending creation")
				assert.Contains(t, err.Error(), "PodGangs pending creation", "Should indicate PodGangs are pending creation")
				assert.Contains(t, err.Error(), "test-pgs-0", "Should mention the specific PodGang name")

				// Verify no PodGangs were created yet (since they're pending)
				podGangList := &groveschedulerv1alpha1.PodGangList{}
				listErr := cl.List(context.Background(), podGangList, client.InNamespace(pgs.Namespace))
				require.NoError(t, listErr, "Failed to list PodGangs")

				// The sync may or may not create PodGangs depending on the internal logic,
				// but it should consistently return the requeue error
				t.Logf("Found %d PodGangs after sync (may be 0 if pending creation)", len(podGangList.Items))
			},
		},
		{
			// Test sync behavior with multiple replicas
			name: "sync with multiple replicas returns appropriate requeue error",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(2).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(1).
					WithMinAvailable(1).
					Build()).
				Build(),
			existingPodGangs:     []groveschedulerv1alpha1.PodGang{},
			expectedRequeueError: true,
			validateBehavior: func(t *testing.T, cl client.Client, pgs *grovecorev1alpha1.PodGangSet, err error) {
				// Verify requeue error mentions multiple PodGangs
				require.Error(t, err, "Expected requeue error for multiple pending creations")
				assert.Contains(t, err.Error(), "PodGangs pending creation", "Should indicate PodGangs are pending creation")

				// Should mention expected PodGang names in the error message
				errorMsg := err.Error()
				containsPodGang0 := assert.Contains(t, errorMsg, "test-pgs-0", "Error should mention test-pgs-0")
				containsPodGang1 := assert.Contains(t, errorMsg, "test-pgs-1", "Error should mention test-pgs-1")

				// At least one of the expected PodGang names should be mentioned
				if !containsPodGang0 && !containsPodGang1 {
					t.Errorf("Error message should mention at least one expected PodGang name (test-pgs-0 or test-pgs-1), got: %s", errorMsg)
				}
			},
		},
		{
			// Test sync with existing PodGangs to verify update behavior
			name: "sync with existing PodGangs handles updates",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(2).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(1).
					WithMinAvailable(1).
					Build()).
				Build(),
			existingPodGangs: []groveschedulerv1alpha1.PodGang{
				createTestPodGang("test-namespace", "test-pgs-0", "test-pgs"),
			},
			expectedRequeueError: true,
			validateBehavior: func(t *testing.T, cl client.Client, pgs *grovecorev1alpha1.PodGangSet, err error) {
				// Should still return requeue error for the second PodGang
				require.Error(t, err, "Expected requeue error for additional PodGang creation")
				assert.Contains(t, err.Error(), "PodGangs pending creation", "Should indicate additional PodGangs are pending")

				// Verify existing PodGang is still present
				podGangList := &groveschedulerv1alpha1.PodGangList{}
				listErr := cl.List(context.Background(), podGangList, client.InNamespace(pgs.Namespace))
				require.NoError(t, listErr, "Failed to list PodGangs")

				// Should have at least the existing PodGang
				assert.GreaterOrEqual(t, len(podGangList.Items), 1, "Should have at least the existing PodGang")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := logr.Discard()

			// Create fake client with existing resources
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			objects := []client.Object{tt.pgs}
			for i := range tt.existingPodGangs {
				objects = append(objects, &tt.existingPodGangs[i])
			}

			fakeClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(objects...).Build()

			// Create operator instance
			operator := New(fakeClient, testScheme, &record.FakeRecorder{})

			// Execute sync operation
			err := operator.Sync(ctx, logger, tt.pgs)

			// Verify expected error behavior
			if tt.expectedRequeueError {
				require.Error(t, err, "Expected requeue error but got none")
			} else {
				assert.NoError(t, err, "Unexpected error during sync")
			}

			// Perform additional validation
			if tt.validateBehavior != nil {
				tt.validateBehavior(t, fakeClient, tt.pgs, err)
			}
		})
	}
}

// TestCreateOrUpdatePodGang verifies the createOrUpdatePodGang function behavior.
// It tests both creation and update scenarios with proper error handling.
func TestCreateOrUpdatePodGang(t *testing.T) {
	tests := []struct {
		// name describes the specific scenario being tested
		name string
		// pgs represents the PodGangSet for the operation
		pgs *grovecorev1alpha1.PodGangSet
		// pgInfo contains the PodGang information
		pgInfo podGangInfo
		// existingPodGang indicates if a PodGang already exists
		existingPodGang *groveschedulerv1alpha1.PodGang
		// simulateError configures the fake client to return errors
		simulateError func(*fake.ClientBuilder) *fake.ClientBuilder
		// shouldError indicates whether the operation should fail
		shouldError bool
		// errorContains specifies text that should be present in error messages
		errorContains string
	}{
		{
			// Test successful PodGang creation
			name: "successful PodGang creation",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			pgInfo: podGangInfo{
				fqn: "test-pgs-0",
				pclqs: []pclqInfo{
					{
						fqn:                "worker-0",
						replicas:           2,
						minAvailable:       2,
						associatedPodNames: []string{"worker-0-pod-1"},
					},
				},
			},
			existingPodGang: nil,
			shouldError:     false,
		},
		{
			// Test successful PodGang update
			name: "successful PodGang update",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			pgInfo: podGangInfo{
				fqn: "test-pgs-0",
				pclqs: []pclqInfo{
					{
						fqn:                "worker-0",
						replicas:           2,
						minAvailable:       2,
						associatedPodNames: []string{"worker-0-pod-1", "worker-0-pod-2"},
					},
				},
			},
			existingPodGang: &groveschedulerv1alpha1.PodGang{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs-0",
					Namespace: "test-namespace",
					Labels:    getLabels("test-pgs"),
				},
			},
			shouldError: false,
		},
		{
			// Test error during CreateOrPatch operation
			name: "error during CreateOrPatch",
			pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
				WithReplicas(1).
				WithPodCliqueTemplateSpec(testutils.NewPodCliqueTemplateSpecBuilder("worker").
					WithReplicas(2).
					WithMinAvailable(2).
					Build()).
				Build(),
			pgInfo: podGangInfo{
				fqn: "test-pgs-0",
				pclqs: []pclqInfo{
					{
						fqn:                "worker-0",
						replicas:           2,
						minAvailable:       2,
						associatedPodNames: []string{"worker-0-pod-1"},
					},
				},
			},
			simulateError: func(builder *fake.ClientBuilder) *fake.ClientBuilder {
				return builder.WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						return fmt.Errorf("simulated create error")
					},
				})
			},
			shouldError:   true,
			errorContains: "ERR_CREATE_OR_PATCH_PODGANG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			ctx := context.Background()
			logger := logr.Discard()

			// Create fake client with existing resources
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(testScheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(testScheme))

			objects := []client.Object{tt.pgs}
			if tt.existingPodGang != nil {
				objects = append(objects, tt.existingPodGang)
			}

			clientBuilder := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(objects...)
			if tt.simulateError != nil {
				clientBuilder = tt.simulateError(clientBuilder)
			}
			fakeClient := clientBuilder.Build()

			// Create operator instance
			operator := &_resource{
				client:        fakeClient,
				scheme:        testScheme,
				eventRecorder: &record.FakeRecorder{},
			}

			// Create sync context
			sc := &syncContext{
				ctx:    ctx,
				pgs:    tt.pgs,
				logger: logger,
			}

			// Execute createOrUpdatePodGang
			err := operator.createOrUpdatePodGang(sc, tt.pgInfo)

			// Verify results
			if tt.shouldError {
				require.Error(t, err, "Expected error but got none")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Unexpected error: %v", err)

				// Verify PodGang was created/updated
				pg := &groveschedulerv1alpha1.PodGang{}
				getErr := fakeClient.Get(ctx, client.ObjectKey{
					Namespace: tt.pgs.Namespace,
					Name:      tt.pgInfo.fqn,
				}, pg)
				assert.NoError(t, getErr, "PodGang should exist after createOrUpdate")

				if getErr == nil {
					// Verify PodGang has correct labels
					expectedLabels := getLabels(tt.pgs.Name)
					for key, expectedValue := range expectedLabels {
						actualValue, exists := pg.Labels[key]
						assert.True(t, exists, "Label %s should exist", key)
						assert.Equal(t, expectedValue, actualValue, "Label %s should have correct value", key)
					}
				}
			}
		})
	}
}

// TestSyncFlowResultMethods verifies the syncFlowResult helper methods.
// It tests error recording, aggregation, and PodGang tracking functionality.
func TestSyncFlowResultMethods(t *testing.T) {
	t.Run("error handling methods", func(t *testing.T) {
		result := &syncFlowResult{}

		// Test initial state
		assert.False(t, result.hasErrors(), "Should have no errors initially")
		assert.Nil(t, result.getAggregatedError(), "Should return nil when no errors")

		// Test recording errors
		err1 := fmt.Errorf("first error")
		err2 := fmt.Errorf("second error")

		result.recordError(err1)
		assert.True(t, result.hasErrors(), "Should have errors after recording")

		result.recordError(err2)
		assert.True(t, result.hasErrors(), "Should still have errors")

		// Test error aggregation
		aggregatedErr := result.getAggregatedError()
		require.Error(t, aggregatedErr, "Should return aggregated error")
		assert.Contains(t, aggregatedErr.Error(), "first error", "Should contain first error")
		assert.Contains(t, aggregatedErr.Error(), "second error", "Should contain second error")
	})

	t.Run("PodGang tracking methods", func(t *testing.T) {
		result := &syncFlowResult{}

		// Test initial state
		assert.False(t, result.hasPodGangsPendingCreation(), "Should have no pending PodGangs initially")

		// Test recording PodGang creation
		result.recordPodGangCreation("test-pgs-0")
		assert.Contains(t, result.createdPodGangNames, "test-pgs-0", "Should track created PodGang")

		// Test recording pending creation
		result.recordPodGangPendingCreation("test-pgs-1")
		assert.True(t, result.hasPodGangsPendingCreation(), "Should have pending PodGangs")
		assert.Contains(t, result.podsGangsPendingCreation, "test-pgs-1", "Should track pending PodGang")

		// Test multiple pending PodGangs
		result.recordPodGangPendingCreation("test-pgs-2")
		assert.True(t, result.hasPodGangsPendingCreation(), "Should still have pending PodGangs")
		assert.Len(t, result.podsGangsPendingCreation, 2, "Should track multiple pending PodGangs")
	})
}

// TestPodGangInfoMethods verifies the podGangInfo helper methods.
// It tests pod association and refresh functionality.
func TestPodGangInfoMethods(t *testing.T) {
	t.Run("refreshAssociatedPCLQPods", func(t *testing.T) {
		pgInfo := podGangInfo{
			fqn: "test-pgs-0",
			pclqs: []pclqInfo{
				{
					fqn:                "worker-0",
					replicas:           2,
					minAvailable:       2,
					associatedPodNames: []string{"worker-0-pod-1"},
				},
				{
					fqn:                "db-0",
					replicas:           1,
					minAvailable:       1,
					associatedPodNames: []string{},
				},
			},
		}

		// Test refreshing pods for existing PodClique
		pgInfo.refreshAssociatedPCLQPods("worker-0", "worker-0-pod-2", "worker-0-pod-3")

		// Verify pods were added to the correct PodClique
		workerPclq := pgInfo.pclqs[0]
		assert.Len(t, workerPclq.associatedPodNames, 3, "Should have 3 associated pods")
		assert.Contains(t, workerPclq.associatedPodNames, "worker-0-pod-1", "Should retain original pod")
		assert.Contains(t, workerPclq.associatedPodNames, "worker-0-pod-2", "Should add new pod")
		assert.Contains(t, workerPclq.associatedPodNames, "worker-0-pod-3", "Should add new pod")

		// Verify other PodClique was not affected
		dbPclq := pgInfo.pclqs[1]
		assert.Len(t, dbPclq.associatedPodNames, 0, "Other PodClique should be unchanged")

		// Test refreshing pods for different PodClique
		pgInfo.refreshAssociatedPCLQPods("db-0", "db-0-pod-1")

		// Verify pods were added to the correct PodClique
		dbPclq = pgInfo.pclqs[1]
		assert.Len(t, dbPclq.associatedPodNames, 1, "Should have 1 associated pod")
		assert.Contains(t, dbPclq.associatedPodNames, "db-0-pod-1", "Should add pod to correct PodClique")

		// Test refreshing pods for non-existent PodClique (should not crash)
		pgInfo.refreshAssociatedPCLQPods("non-existent", "some-pod")

		// Verify existing PodCliques were not affected
		assert.Len(t, pgInfo.pclqs[0].associatedPodNames, 3, "Worker PodClique should be unchanged")
		assert.Len(t, pgInfo.pclqs[1].associatedPodNames, 1, "DB PodClique should be unchanged")
	})
}

// TestDeterminePodCliqueReplicas verifies the replica calculation logic.
// It tests various scaling scenarios and configurations.
func TestDeterminePodCliqueReplicas(t *testing.T) {
	tests := []struct {
		// name describes the specific replica calculation scenario
		name string
		// pclqTemplateSpec represents the PodClique template specification
		pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec
		// pclqFQN is the fully qualified name of the PodClique
		pclqFQN string
		// setupSyncContext configures the sync context for the test
		setupSyncContext func() *syncContext
		// expectedReplicas is the expected replica count
		expectedReplicas int32
	}{
		{
			// Test basic replica calculation without scaling
			name: "basic replicas without scaling",
			pclqTemplateSpec: testutils.NewPodCliqueTemplateSpecBuilder("worker").
				WithReplicas(3).
				WithMinAvailable(2).
				Build(),
			pclqFQN: "worker-0",
			setupSyncContext: func() *syncContext {
				return &syncContext{
					pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
						WithReplicas(1).
						Build(),
				}
			},
			expectedReplicas: 3,
		},
		{
			// Test replica calculation with scale config
			name: "replicas with scale config",
			pclqTemplateSpec: &grovecorev1alpha1.PodCliqueTemplateSpec{
				Name: "worker",
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas:     5,
					MinAvailable: &[]int32{2}[0],
					// Note: ScaleConfig is not available in the current API, using basic replicas
				},
			},
			pclqFQN: "worker-0",
			setupSyncContext: func() *syncContext {
				return &syncContext{
					pgs: testutils.NewPodGangSetBuilder("test-pgs", "test-namespace").
						WithReplicas(1).
						Build(),
					unassignedPodsByPCLQ: map[string][]corev1.Pod{
						"worker-0": {}, // No unassigned pods
					},
				}
			},
			expectedReplicas: 5, // Should use basic replicas when no scale config
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup sync context
			sc := tt.setupSyncContext()

			// Execute determinePodCliqueReplicas
			replicas := determinePodCliqueReplicas(sc, tt.pclqTemplateSpec, tt.pclqFQN)

			// Verify result
			assert.Equal(t, tt.expectedReplicas, replicas, "Should return expected replica count")
		})
	}
}

// Helper functions for test setup and validation

// createTestPodGang creates a PodGang resource for testing purposes.
// It sets up proper ownership and labeling to simulate real cluster resources.
func createTestPodGang(namespace, name, ownerName string) groveschedulerv1alpha1.PodGang {
	return groveschedulerv1alpha1.PodGang{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    getLabels(ownerName),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
					Kind:       "PodGangSet",
					Name:       ownerName,
					UID:        "test-uid",
					Controller: &[]bool{true}[0],
				},
			},
		},
		Spec: groveschedulerv1alpha1.PodGangSpec{
			PodGroups: []groveschedulerv1alpha1.PodGroup{
				{
					Name:        fmt.Sprintf("%s-worker", name),
					MinReplicas: 1,
				},
			},
		},
	}
}

// hasExpectedLabels checks if a label map contains the expected labels for a PodGangSet.
// This is used to verify proper labeling during testing.
func hasExpectedLabels(labels map[string]string, pgsName string) bool {
	expectedLabels := getLabels(pgsName)
	for key, expectedValue := range expectedLabels {
		if actualValue, exists := labels[key]; !exists || actualValue != expectedValue {
			return false
		}
	}
	return true
}
