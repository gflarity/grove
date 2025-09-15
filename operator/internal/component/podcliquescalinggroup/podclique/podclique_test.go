/*
Copyright 2025 The Grove Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podclique

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestNew validates that the New function correctly creates a PodClique component operator
// with all required dependencies properly initialized.
func TestNew(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// client is the Kubernetes client to be used by the operator
		client client.Client
		// scheme is the runtime scheme for object serialization/deserialization
		scheme *runtime.Scheme
		// eventRecorder is used for recording Kubernetes events
		eventRecorder record.EventRecorder
		// expectNil indicates whether the returned operator should be nil
		expectNil bool
	}{
		{
			// Tests successful creation of operator with valid dependencies
			name:          "successful_creation_with_valid_dependencies",
			client:        fake.NewClientBuilder().Build(),
			scheme:        runtime.NewScheme(),
			eventRecorder: &record.FakeRecorder{},
			expectNil:     false,
		},
		{
			// Tests creation with nil client - should still create operator but may fail at runtime
			name:          "creation_with_nil_client",
			client:        nil,
			scheme:        runtime.NewScheme(),
			eventRecorder: &record.FakeRecorder{},
			expectNil:     false,
		},
		{
			// Tests creation with nil scheme - should still create operator but may fail at runtime
			name:          "creation_with_nil_scheme",
			client:        fake.NewClientBuilder().Build(),
			scheme:        nil,
			eventRecorder: &record.FakeRecorder{},
			expectNil:     false,
		},
		{
			// Tests creation with nil event recorder - should still create operator
			name:          "creation_with_nil_event_recorder",
			client:        fake.NewClientBuilder().Build(),
			scheme:        runtime.NewScheme(),
			eventRecorder: nil,
			expectNil:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operator := New(tt.client, tt.scheme, tt.eventRecorder)

			if tt.expectNil {
				assert.Nil(t, operator)
			} else {
				assert.NotNil(t, operator)

				// Verify the operator implements the expected interface
				assert.Implements(t, (*component.Operator[grovecorev1alpha1.PodCliqueScalingGroup])(nil), operator, "operator should implement component.Operator interface")
			}
		})
	}
}

// TestGetExistingResourceNames validates the GetExistingResourceNames method which retrieves
// names of all PodCliques currently managed by a PodCliqueScalingGroup.
func TestGetExistingResourceNames(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// existingObjects are the Kubernetes objects that exist before the test
		existingObjects []client.Object
		// pcsgObjectMeta is the metadata of the PodCliqueScalingGroup to query for
		pcsgObjectMeta metav1.ObjectMeta
		// expectedNames are the PodClique names expected to be returned
		expectedNames []string
		// expectError indicates whether an error is expected
		expectError bool
		// errorCode is the expected Grove error code if an error occurs
		errorCode grovecorev1alpha1.ErrorCode
	}{
		{
			// Tests retrieval when no PodCliques exist for the PCSG
			name:            "no_existing_podcliques",
			existingObjects: []client.Object{},
			pcsgObjectMeta: metav1.ObjectMeta{
				Name:      "test-pcsg",
				Namespace: "default",
				Labels: map[string]string{
					grovecorev1alpha1.LabelPartOfKey:              "test-pgs",
					grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0",
				},
			},
			expectedNames: []string{},
			expectError:   false,
		},
		{
			// Tests retrieval when multiple PodCliques exist for the PCSG
			name: "multiple_existing_podcliques",
			existingObjects: []client.Object{
				createTestPodCliqueWithOwner("test-pcsg-0-worker", "default", "test-pgs", "test-pcsg", types.UID("test-pcsg-uid")),
				createTestPodCliqueWithOwner("test-pcsg-0-master", "default", "test-pgs", "test-pcsg", types.UID("test-pcsg-uid")),
				createTestPodClique("other-pcsg-0-worker", "default", "test-pgs", "other-pcsg"), // Should not be included
			},
			pcsgObjectMeta: metav1.ObjectMeta{
				Name:      "test-pcsg",
				Namespace: "default",
				UID:       types.UID("test-pcsg-uid"),
				Labels: map[string]string{
					grovecorev1alpha1.LabelPartOfKey:              "test-pgs",
					grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0",
				},
			},
			expectedNames: []string{"test-pcsg-0-worker", "test-pcsg-0-master"},
			expectError:   false,
		},
		{
			// Tests retrieval when PodCliques exist in different namespaces
			name: "podcliques_in_different_namespace",
			existingObjects: []client.Object{
				createTestPodClique("test-pcsg-0-worker", "other-namespace", "test-pgs", "test-pcsg"),
			},
			pcsgObjectMeta: metav1.ObjectMeta{
				Name:      "test-pcsg",
				Namespace: "default",
				Labels: map[string]string{
					grovecorev1alpha1.LabelPartOfKey:              "test-pgs",
					grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0",
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

			operator := New(client, scheme, &record.FakeRecorder{})
			ctx := context.Background()
			logger := logr.Discard()

			names, err := operator.GetExistingResourceNames(ctx, logger, tt.pcsgObjectMeta)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorCode != "" {
					groveErr, ok := err.(*groveerr.GroveError)
					require.True(t, ok, "expected GroveError")
					assert.Equal(t, tt.errorCode, groveErr.Code)
				}
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.expectedNames, names)
			}
		})
	}
}

// TestDelete validates the Delete method which removes all PodCliques managed by
// a PodCliqueScalingGroup during cleanup operations.
func TestDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// existingObjects are the Kubernetes objects that exist before the test
		existingObjects []client.Object
		// pcsgObjectMeta is the metadata of the PodCliqueScalingGroup being deleted
		pcsgObjectMeta metav1.ObjectMeta
		// expectError indicates whether an error is expected
		expectError bool
		// errorCode is the expected Grove error code if an error occurs
		errorCode grovecorev1alpha1.ErrorCode
		// expectedRemainingPodCliques are PodCliques that should remain after deletion
		expectedRemainingPodCliques []string
	}{
		{
			// Tests deletion when no PodCliques exist
			name:            "no_existing_podcliques",
			existingObjects: []client.Object{},
			pcsgObjectMeta: metav1.ObjectMeta{
				Name:      "test-pcsg",
				Namespace: "default",
				Labels: map[string]string{
					grovecorev1alpha1.LabelPartOfKey:              "test-pgs",
					grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0",
				},
			},
			expectError:                 false,
			expectedRemainingPodCliques: []string{},
		},
		{
			// Tests deletion of multiple PodCliques belonging to the PCSG
			name: "delete_multiple_podcliques",
			existingObjects: []client.Object{
				createTestPodCliqueWithOwner("test-pcsg-0-worker", "default", "test-pgs", "test-pcsg", types.UID("test-pcsg-uid")),
				createTestPodCliqueWithOwner("test-pcsg-0-master", "default", "test-pgs", "test-pcsg", types.UID("test-pcsg-uid")),
				createTestPodClique("other-pcsg-0-worker", "default", "test-pgs", "other-pcsg"), // Should remain
			},
			pcsgObjectMeta: metav1.ObjectMeta{
				Name:      "test-pcsg",
				Namespace: "default",
				UID:       types.UID("test-pcsg-uid"),
				Labels: map[string]string{
					grovecorev1alpha1.LabelPartOfKey:              "test-pgs",
					grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0",
				},
			},
			expectError:                 false,
			expectedRemainingPodCliques: []string{"other-pcsg-0-worker"},
		},
		{
			// Tests deletion when PodCliques are in different namespace
			name: "podcliques_in_different_namespace",
			existingObjects: []client.Object{
				createTestPodClique("test-pcsg-0-worker", "other-namespace", "test-pgs", "test-pcsg"),
			},
			pcsgObjectMeta: metav1.ObjectMeta{
				Name:      "test-pcsg",
				Namespace: "default",
				Labels: map[string]string{
					grovecorev1alpha1.LabelPartOfKey:              "test-pgs",
					grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0",
				},
			},
			expectError:                 false,
			expectedRemainingPodCliques: []string{"test-pcsg-0-worker"}, // Should remain as it's in different namespace
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjects...).
				Build()

			operator := New(client, scheme, &record.FakeRecorder{})
			ctx := context.Background()
			logger := logr.Discard()

			err := operator.Delete(ctx, logger, tt.pcsgObjectMeta)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorCode != "" {
					groveErr, ok := err.(*groveerr.GroveError)
					require.True(t, ok, "expected GroveError")
					assert.Equal(t, tt.errorCode, groveErr.Code)
				}
			} else {
				assert.NoError(t, err)

				// Verify expected PodCliques remain
				pclqList := &grovecorev1alpha1.PodCliqueList{}
				err := client.List(ctx, pclqList)
				assert.NoError(t, err)

				actualNames := make([]string, 0, len(pclqList.Items))
				for _, pclq := range pclqList.Items {
					actualNames = append(actualNames, pclq.Name)
				}
				assert.ElementsMatch(t, tt.expectedRemainingPodCliques, actualNames)
			}
		})
	}
}

// TestGetExpectedPodCliqueFQNs validates the helper function that generates expected
// PodClique fully qualified names based on PCSG specification.
func TestGetExpectedPodCliqueFQNs(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pcsg is the PodCliqueScalingGroup to generate FQNs for
		pcsg *grovecorev1alpha1.PodCliqueScalingGroup
		// expectedFQNs are the expected fully qualified names
		expectedFQNs []string
	}{
		{
			// Tests FQN generation for single replica with multiple cliques
			name: "single_replica_multiple_cliques",
			pcsg: createTestPodCliqueScalingGroup("test-pcsg", "default", 1, []string{"worker", "master"}),
			expectedFQNs: []string{
				"test-pcsg-0-worker",
				"test-pcsg-0-master",
			},
		},
		{
			// Tests FQN generation for multiple replicas with single clique
			name: "multiple_replicas_single_clique",
			pcsg: createTestPodCliqueScalingGroup("test-pcsg", "default", 3, []string{"worker"}),
			expectedFQNs: []string{
				"test-pcsg-0-worker",
				"test-pcsg-1-worker",
				"test-pcsg-2-worker",
			},
		},
		{
			// Tests FQN generation for multiple replicas with multiple cliques
			name: "multiple_replicas_multiple_cliques",
			pcsg: createTestPodCliqueScalingGroup("test-pcsg", "default", 2, []string{"worker", "master"}),
			expectedFQNs: []string{
				"test-pcsg-0-worker", "test-pcsg-0-master",
				"test-pcsg-1-worker", "test-pcsg-1-master",
			},
		},
		{
			// Tests FQN generation when no cliques are specified
			name:         "no_cliques",
			pcsg:         createTestPodCliqueScalingGroup("test-pcsg", "default", 2, []string{}),
			expectedFQNs: []string{},
		},
		{
			// Tests FQN generation when replicas is zero
			name:         "zero_replicas",
			pcsg:         createTestPodCliqueScalingGroup("test-pcsg", "default", 0, []string{"worker"}),
			expectedFQNs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualFQNs := getExpectedPodCliqueFQNs(tt.pcsg)
			assert.ElementsMatch(t, tt.expectedFQNs, actualFQNs)
		})
	}
}

// TestGetPodCliqueSelectorLabels validates the helper function that generates
// label selectors for finding PodCliques managed by a PCSG.
func TestGetPodCliqueSelectorLabels(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pcsgObjectMeta is the metadata of the PCSG to generate selectors for
		pcsgObjectMeta metav1.ObjectMeta
		// expectedLabels are the expected selector labels
		expectedLabels map[string]string
	}{
		{
			// Tests selector generation for basic PCSG metadata
			name: "basic_pcsg_metadata",
			pcsgObjectMeta: metav1.ObjectMeta{
				Name:      "test-pcsg",
				Namespace: "default",
				Labels: map[string]string{
					grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0",
				},
			},
			expectedLabels: map[string]string{
				grovecorev1alpha1.LabelComponentKey:          grovecorev1alpha1.LabelComponentPCSGPodCliqueValue,
				grovecorev1alpha1.LabelPodCliqueScalingGroup: "test-pcsg",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualLabels := getPodCliqueSelectorLabels(tt.pcsgObjectMeta)

			// Verify expected labels are present
			for key, expectedValue := range tt.expectedLabels {
				actualValue, exists := actualLabels[key]
				assert.True(t, exists, "Expected label key %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "Label value for key %s should match", key)
			}
		})
	}
}

// TestGetLabelsToDeletePCSGReplicaIndexPCLQs validates the helper function that generates
// labels for deleting PodCliques belonging to a specific PCSG replica index.
func TestGetLabelsToDeletePCSGReplicaIndexPCLQs(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsName is the name of the parent PodGangSet
		pgsName string
		// pcsgName is the name of the PodCliqueScalingGroup
		pcsgName string
		// pcsgReplicaIndex is the replica index to delete
		pcsgReplicaIndex string
		// expectedLabels are the expected deletion labels
		expectedLabels map[string]string
	}{
		{
			// Tests deletion label generation for specific replica index
			name:             "basic_deletion_labels",
			pgsName:          "test-pgs",
			pcsgName:         "test-pcsg",
			pcsgReplicaIndex: "1",
			expectedLabels: map[string]string{
				grovecorev1alpha1.LabelComponentKey:                      grovecorev1alpha1.LabelComponentPCSGPodCliqueValue,
				grovecorev1alpha1.LabelPodCliqueScalingGroup:             "test-pcsg",
				grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: "1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualLabels := getLabelsToDeletePCSGReplicaIndexPCLQs(tt.pgsName, tt.pcsgName, tt.pcsgReplicaIndex)

			// Verify expected labels are present
			for key, expectedValue := range tt.expectedLabels {
				actualValue, exists := actualLabels[key]
				assert.True(t, exists, "Expected label key %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "Label value for key %s should match", key)
			}
		})
	}
}

// TestGetExcessPCSGReplicaIndexesToDelete validates the helper function that identifies
// which PCSG replica indexes have PodCliques that should be deleted.
func TestGetExcessPCSGReplicaIndexesToDelete(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pcsg is the PodCliqueScalingGroup with current desired state
		pcsg *grovecorev1alpha1.PodCliqueScalingGroup
		// existingPCLQs are the PodCliques that currently exist
		existingPCLQs []grovecorev1alpha1.PodClique
		// expectedIndexes are the replica indexes expected to be marked for deletion
		expectedIndexes []string
	}{
		{
			// Tests when no excess PodCliques exist
			name: "no_excess_podcliques",
			pcsg: createTestPodCliqueScalingGroup("test-pcsg", "default", 2, []string{"worker"}),
			existingPCLQs: []grovecorev1alpha1.PodClique{
				*createTestPodClique("test-pcsg-0-worker", "default", "test-pgs", "test-pcsg"),
				*createTestPodCliqueWithReplicaIndex("test-pcsg-1-worker", "default", "test-pgs", "test-pcsg", "1"),
			},
			expectedIndexes: []string{},
		},
		{
			// Tests when excess PodCliques exist due to scale-down
			name: "excess_podcliques_from_scale_down",
			pcsg: createTestPodCliqueScalingGroup("test-pcsg", "default", 1, []string{"worker"}),
			existingPCLQs: []grovecorev1alpha1.PodClique{
				*createTestPodClique("test-pcsg-0-worker", "default", "test-pgs", "test-pcsg"),
				*createTestPodCliqueWithReplicaIndex("test-pcsg-1-worker", "default", "test-pgs", "test-pcsg", "1"),
				*createTestPodCliqueWithReplicaIndex("test-pcsg-2-worker", "default", "test-pgs", "test-pcsg", "2"),
			},
			expectedIndexes: []string{"1", "2"},
		},
		{
			// Tests when PodCliques exist but don't have replica index labels
			name: "podcliques_without_replica_index_labels",
			pcsg: createTestPodCliqueScalingGroup("test-pcsg", "default", 1, []string{"worker"}),
			existingPCLQs: []grovecorev1alpha1.PodClique{
				*createTestPodCliqueWithoutReplicaIndex("test-pcsg-0-worker", "default", "test-pgs", "test-pcsg"),
			},
			expectedIndexes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualIndexes := getExcessPCSGReplicaIndexesToDelete(tt.pcsg, tt.existingPCLQs)
			assert.ElementsMatch(t, tt.expectedIndexes, actualIndexes)
		})
	}
}

// TestEmptyPodClique validates the helper function that creates empty PodClique objects.
func TestEmptyPodClique(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// objKey is the object key to create the empty PodClique with
		objKey client.ObjectKey
		// expectedName is the expected name of the created PodClique
		expectedName string
		// expectedNamespace is the expected namespace of the created PodClique
		expectedNamespace string
	}{
		{
			// Tests creation of empty PodClique with basic object key
			name: "basic_empty_podclique",
			objKey: client.ObjectKey{
				Name:      "test-podclique",
				Namespace: "default",
			},
			expectedName:      "test-podclique",
			expectedNamespace: "default",
		},
		{
			// Tests creation of empty PodClique with different namespace
			name: "empty_podclique_different_namespace",
			objKey: client.ObjectKey{
				Name:      "another-podclique",
				Namespace: "kube-system",
			},
			expectedName:      "another-podclique",
			expectedNamespace: "kube-system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pclq := emptyPodClique(tt.objKey)

			assert.NotNil(t, pclq)
			assert.Equal(t, tt.expectedName, pclq.Name)
			assert.Equal(t, tt.expectedNamespace, pclq.Namespace)

			// Verify it's truly empty (no spec or status)
			assert.Empty(t, pclq.Spec.RoleName)
			assert.Equal(t, int32(0), pclq.Spec.Replicas)
			assert.Empty(t, pclq.Status.Conditions)
		})
	}
}

// Helper functions for creating test objects

// createTestPodCliqueScalingGroup creates a PodCliqueScalingGroup for testing
func createTestPodCliqueScalingGroup(name, namespace string, replicas int32, cliqueNames []string) *grovecorev1alpha1.PodCliqueScalingGroup {
	minAvailable := int32(1)
	return &grovecorev1alpha1.PodCliqueScalingGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				grovecorev1alpha1.LabelPartOfKey:              "test-pgs",
				grovecorev1alpha1.LabelPodGangSetReplicaIndex: "0",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         grovecorev1alpha1.SchemeGroupVersion.String(),
					Kind:               "PodGangSet",
					Name:               "test-pgs",
					UID:                types.UID("test-pgs-uid"),
					Controller:         &[]bool{true}[0],
					BlockOwnerDeletion: &[]bool{true}[0],
				},
			},
		},
		Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
			Replicas:     replicas,
			MinAvailable: &minAvailable,
			CliqueNames:  cliqueNames,
		},
	}
}

// createTestPodClique creates a PodClique for testing with proper ownership
func createTestPodClique(name, namespace, pgsName, pcsgName string) *grovecorev1alpha1.PodClique {
	return createTestPodCliqueWithOwner(name, namespace, pgsName, pcsgName, types.UID(pcsgName+"-uid"))
}

// createTestPodCliqueWithOwner creates a PodClique with specific owner UID
func createTestPodCliqueWithOwner(name, namespace, pgsName, pcsgName string, ownerUID types.UID) *grovecorev1alpha1.PodClique {
	return &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				grovecorev1alpha1.LabelManagedByKey:                      grovecorev1alpha1.LabelManagedByValue,
				grovecorev1alpha1.LabelPartOfKey:                         pgsName,
				grovecorev1alpha1.LabelComponentKey:                      grovecorev1alpha1.LabelComponentPCSGPodCliqueValue,
				grovecorev1alpha1.LabelPodCliqueScalingGroup:             pcsgName,
				grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: "0",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         grovecorev1alpha1.SchemeGroupVersion.String(),
					Kind:               "PodCliqueScalingGroup",
					Name:               pcsgName,
					UID:                ownerUID,
					Controller:         &[]bool{true}[0],
					BlockOwnerDeletion: &[]bool{true}[0],
				},
			},
		},
		Spec: grovecorev1alpha1.PodCliqueSpec{
			RoleName: "worker",
			Replicas: 1,
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "worker",
						Image: "nginx:latest",
					},
				},
			},
		},
	}
}

// createTestPodCliqueWithReplicaIndex creates a PodClique with specific replica index
func createTestPodCliqueWithReplicaIndex(name, namespace, pgsName, pcsgName, replicaIndex string) *grovecorev1alpha1.PodClique {
	pclq := createTestPodClique(name, namespace, pgsName, pcsgName)
	pclq.Labels[grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex] = replicaIndex
	return pclq
}

// createTestPodCliqueWithoutReplicaIndex creates a PodClique without replica index label
func createTestPodCliqueWithoutReplicaIndex(name, namespace, pgsName, pcsgName string) *grovecorev1alpha1.PodClique {
	pclq := createTestPodClique(name, namespace, pgsName, pcsgName)
	delete(pclq.Labels, grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex)
	return pclq
}
