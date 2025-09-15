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

package pod

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/NVIDIA/grove/operator/api/common"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/expect"

	groveschedulerv1alpha1 "github.com/NVIDIA/grove/scheduler/api/core/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestPrepareSyncFlow tests the prepareSyncFlow function which gathers information
// in preparation for the sync flow to run.
func TestPrepareSyncFlow(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pclq is the PodClique resource to prepare sync flow for
		pclq *grovecorev1alpha1.PodClique
		// setupObjects are the Kubernetes objects to create in the fake client before testing
		setupObjects []client.Object
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedAssociatedPodGangName is the expected associated PodGang name in the sync context
		expectedAssociatedPodGangName string
		// expectedPodCount is the expected number of existing pods in the sync context
		expectedPodCount int
	}{
		// Successfully prepares sync context when all dependencies exist
		{
			name: "successful preparation with PodGangSet and PodGang",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs-0-test-pclq",
					Namespace: "default",
					UID:       "pclq-uid-123",
					Labels: map[string]string{
						common.LabelPodGang:                "test-podgang",
						common.LabelPartOfKey:              "test-pgs",
						common.LabelPodGangSetReplicaIndex: "0",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "grove.io/v1alpha1",
							Kind:       "PodGangSet",
							Name:       "test-pgs",
							UID:        "pgs-uid-123",
						},
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas: 2,
				},
			},
			setupObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
						UID:       "pgs-uid-123",
					},
					Spec: grovecorev1alpha1.PodGangSetSpec{
						Template: grovecorev1alpha1.PodGangSetTemplateSpec{
							Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
								{
									Name: "test-pclq",
									Spec: grovecorev1alpha1.PodCliqueSpec{
										Replicas: 2,
									},
								},
							},
						},
					},
				},
				&groveschedulerv1alpha1.PodGang{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-podgang",
						Namespace: "default",
					},
					Spec: groveschedulerv1alpha1.PodGangSpec{
						PodGroups: []groveschedulerv1alpha1.PodGroup{
							{
								Name:        "test-pgs-0-test-pclq",
								MinReplicas: 2,
								PodReferences: []groveschedulerv1alpha1.NamespacedName{
									{Name: "test-pod-0"},
									{Name: "test-pod-1"},
								},
							},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-0",
						Namespace: "default",
						Labels: map[string]string{
							common.LabelManagedByKey: common.LabelManagedByValue,
							common.LabelPartOfKey:    "test-pgs",
							common.LabelPodClique:    "test-pgs-0-test-pclq",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "grove.io/v1alpha1",
								Kind:       "PodClique",
								Name:       "test-pgs-0-test-pclq",
								UID:        "pclq-uid-123",
								Controller: ptr.To(true),
							},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-1",
						Namespace: "default",
						Labels: map[string]string{
							common.LabelManagedByKey: common.LabelManagedByValue,
							common.LabelPartOfKey:    "test-pgs",
							common.LabelPodClique:    "test-pgs-0-test-pclq",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "grove.io/v1alpha1",
								Kind:       "PodClique",
								Name:       "test-pgs-0-test-pclq",
								UID:        "pclq-uid-123",
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectError:                   false,
			expectedAssociatedPodGangName: "test-podgang",
			expectedPodCount:              2,
		},
		// Returns error when PodClique is missing required PodGang label
		{
			name: "missing PodGang label on PodClique",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Labels: map[string]string{
						common.LabelPartOfKey: "test-pgs",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "grove.io/v1alpha1",
							Kind:       "PodGangSet",
							Name:       "test-pgs",
							UID:        "pgs-uid-123",
						},
					},
				},
			},
			setupObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
						UID:       "pgs-uid-123",
					},
				},
			},
			expectError: true,
		},
		// Returns error when owner PodGangSet does not exist
		{
			name: "missing PodGangSet owner",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
					Labels: map[string]string{
						common.LabelPodGang:   "test-podgang",
						common.LabelPartOfKey: "missing-pgs",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "grove.io/v1alpha1",
							Kind:       "PodGangSet",
							Name:       "missing-pgs",
							UID:        "missing-pgs-uid",
						},
					},
				},
			},
			setupObjects: []client.Object{},
			expectError:  true,
		},
		// Successfully prepares sync context even when PodGang doesn't exist yet
		{
			name: "missing PodGang resource (not an error)",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs-0-test-pclq",
					Namespace: "default",
					Labels: map[string]string{
						common.LabelPodGang:                "missing-podgang",
						common.LabelPartOfKey:              "test-pgs",
						common.LabelPodGangSetReplicaIndex: "0",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "grove.io/v1alpha1",
							Kind:       "PodGangSet",
							Name:       "test-pgs",
							UID:        "pgs-uid-123",
						},
					},
				},
			},
			setupObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
						UID:       "pgs-uid-123",
					},
					Spec: grovecorev1alpha1.PodGangSetSpec{
						Template: grovecorev1alpha1.PodGangSetTemplateSpec{
							Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
								{
									Name: "test-pclq",
									Spec: grovecorev1alpha1.PodCliqueSpec{
										Replicas: 2,
									},
								},
							},
						},
					},
				},
			},
			expectError:                   false,
			expectedAssociatedPodGangName: "missing-podgang",
			expectedPodCount:              0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with test objects
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, groveschedulerv1alpha1.AddToScheme(scheme))
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.setupObjects...).Build()

			// Create resource with fake client and expectations store
			r := &_resource{
				client:            fakeClient,
				scheme:            scheme,
				eventRecorder:     &record.FakeRecorder{},
				expectationsStore: expect.NewExpectationsStore(),
			}

			// Execute the function under test
			sc, err := r.prepareSyncFlow(context.Background(), logr.Discard(), tt.pclq)

			// Verify error expectations
			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				return
			}
			require.NoError(t, err, "Unexpected error for test case: %s", tt.name)

			// Verify sync context contents
			assert.NotNil(t, sc, "Sync context should not be nil")
			assert.Equal(t, tt.pclq, sc.pclq, "PodClique reference should match")
			assert.NotNil(t, sc.pgs, "PodGangSet should be present in sync context")
			assert.Equal(t, tt.expectedAssociatedPodGangName, sc.associatedPodGangName)
			assert.Len(t, sc.existingPCLQPods, tt.expectedPodCount, "Existing pod count should match expected")
			assert.NotEmpty(t, sc.pclqExpectationsStoreKey, "Expectations store key should be set")
		})
	}
}

// TestSyncExpectationsAndComputeDifference tests the expectation synchronization
// and pod count difference calculation logic.
func TestSyncExpectationsAndComputeDifference(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// existingPods are the pods that currently exist in the cluster
		existingPods []*corev1.Pod
		// desiredReplicas is the target number of replicas for the PodClique
		desiredReplicas int32
		// createExpectations are UIDs expected to be created (simulating pending creates)
		createExpectations []string
		// deleteExpectations are UIDs expected to be deleted (simulating pending deletes)
		deleteExpectations []string
		// expectedDiff is the expected difference (negative means need to create, positive means need to delete)
		expectedDiff int
	}{
		// When no pods exist and 3 are desired, difference should be -3 (need to create 3)
		{
			name:            "need to create pods - no existing pods",
			existingPods:    []*corev1.Pod{},
			desiredReplicas: 3,
			expectedDiff:    -3,
		},
		// When 1 pod exists and 3 are desired, difference should be -2 (need to create 2)
		{
			name: "need to create pods - some existing pods",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
			},
			desiredReplicas: 3,
			expectedDiff:    -2,
		},
		// When 4 pods exist and 2 are desired, difference should be 2 (need to delete 2)
		{
			name: "need to delete pods - excess existing pods",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
				createTestPodWithUID("pod-3", "uid-3"),
				createTestPodWithUID("pod-4", "uid-4"),
			},
			desiredReplicas: 2,
			expectedDiff:    2,
		},
		// When 2 pods exist and 2 are desired, difference should be 0 (no action needed)
		{
			name: "exact match - no action needed",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
			},
			desiredReplicas: 2,
			expectedDiff:    0,
		},
		// When 1 pod exists, 3 desired, and 2 pending creates, difference should be 0
		{
			name: "expectations affect calculation - pending creates",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
			},
			desiredReplicas:    3,
			createExpectations: []string{"pending-uid-1", "pending-uid-2"},
			expectedDiff:       0,
		},
		// When 4 pods exist, 2 desired, and 2 pending deletes, difference should be 0
		{
			name: "expectations affect calculation - pending deletes",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
				createTestPodWithUID("pod-3", "uid-3"),
				createTestPodWithUID("pod-4", "uid-4"),
			},
			desiredReplicas:    2,
			deleteExpectations: []string{"uid-3", "uid-4"},
			expectedDiff:       0,
		},
		// Algorithm counts all pods initially, but SyncExpectations adjusts for terminating ones
		{
			name: "terminating pods excluded from count",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
				createTerminatingPodWithUID("terminating-pod", "terminating-uid"),
			},
			desiredReplicas: 3,
			expectedDiff:    0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create expectations store and populate with test expectations
			expectationsStore := expect.NewExpectationsStore()
			pclqKey := "test-namespace/test-pclq"

			// Add create expectations if any
			if len(tt.createExpectations) > 0 {
				createUIDs := make([]types.UID, len(tt.createExpectations))
				for i, uid := range tt.createExpectations {
					createUIDs[i] = types.UID(uid)
				}
				err := expectationsStore.ExpectCreations(logr.Discard(), pclqKey, createUIDs...)
				require.NoError(t, err)
			}

			// Add delete expectations if any
			if len(tt.deleteExpectations) > 0 {
				deleteUIDs := make([]types.UID, len(tt.deleteExpectations))
				for i, uid := range tt.deleteExpectations {
					deleteUIDs[i] = types.UID(uid)
				}
				err := expectationsStore.ExpectDeletions(logr.Discard(), pclqKey, deleteUIDs...)
				require.NoError(t, err)
			}

			// Create test PodClique
			pclq := &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-namespace",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas: tt.desiredReplicas,
				},
			}

			// Create sync context
			sc := &syncContext{
				pclq:                     pclq,
				existingPCLQPods:         tt.existingPods,
				pclqExpectationsStoreKey: pclqKey,
			}

			// Create resource with expectations store
			r := &_resource{
				expectationsStore: expectationsStore,
			}

			// Execute the function under test
			actualDiff := r.syncExpectationsAndComputeDifference(logr.Discard(), sc)

			// Verify the result
			assert.Equal(t, tt.expectedDiff, actualDiff)
		})
	}
}

// TestGetTerminatingAndNonTerminatingPodUIDs tests the utility function that separates
// terminating and non-terminating pods.
func TestGetTerminatingAndNonTerminatingPodUIDs(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pods are the input pods to categorize
		pods []*corev1.Pod
		// expectedTerminating are the expected UIDs of terminating pods
		expectedTerminating []types.UID
		// expectedNonTerminating are the expected UIDs of non-terminating pods
		expectedNonTerminating []types.UID
	}{
		// Empty pod list should return empty slices
		{
			name:                   "empty pod list",
			pods:                   []*corev1.Pod{},
			expectedTerminating:    []types.UID{},
			expectedNonTerminating: []types.UID{},
		},
		// All non-terminating pods should be in non-terminating list
		{
			name: "all non-terminating pods",
			pods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
			},
			expectedTerminating:    []types.UID{},
			expectedNonTerminating: []types.UID{"uid-1", "uid-2"},
		},
		// All terminating pods should be in terminating list
		{
			name: "all terminating pods",
			pods: []*corev1.Pod{
				createTerminatingPodWithUID("pod-1", "uid-1"),
				createTerminatingPodWithUID("pod-2", "uid-2"),
			},
			expectedTerminating:    []types.UID{"uid-1", "uid-2"},
			expectedNonTerminating: []types.UID{},
		},
		// Pods should be correctly categorized by termination status
		{
			name: "mixed terminating and non-terminating pods",
			pods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTerminatingPodWithUID("pod-2", "uid-2"),
				createTestPodWithUID("pod-3", "uid-3"),
				createTerminatingPodWithUID("pod-4", "uid-4"),
			},
			expectedTerminating:    []types.UID{"uid-2", "uid-4"},
			expectedNonTerminating: []types.UID{"uid-1", "uid-3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			terminating, nonTerminating := getTerminatingAndNonTerminatingPodUIDs(tt.pods)

			// Verify the results (order doesn't matter for the test)
			assert.ElementsMatch(t, tt.expectedTerminating, terminating, "Terminating UIDs should match: %s")
			assert.ElementsMatch(t, tt.expectedNonTerminating, nonTerminating, "Non-terminating UIDs should match: %s")
		})
	}
}

// TestSelectExcessPodsToDelete tests the pod selection logic for deletion.
func TestSelectExcessPodsToDelete(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// existingPods are the pods currently in the cluster
		existingPods []*corev1.Pod
		// desiredReplicas is the target number of replicas
		desiredReplicas int32
		// expectedSelectedCount is the expected number of pods selected for deletion
		expectedSelectedCount int
	}{
		// When current replicas equal desired, no pods should be selected for deletion
		{
			name: "no excess pods",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
			},
			desiredReplicas:       2,
			expectedSelectedCount: 0,
		},
		// When 4 pods exist and 2 are desired, 2 pods should be selected for deletion
		{
			name: "some excess pods",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
				createTestPodWithUID("pod-3", "uid-3"),
				createTestPodWithUID("pod-4", "uid-4"),
			},
			desiredReplicas:       2,
			expectedSelectedCount: 2,
		},
		// When fewer pods exist than desired, no pods should be selected for deletion
		{
			name: "fewer pods than desired",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
			},
			desiredReplicas:       3,
			expectedSelectedCount: 0,
		},
		// When no pods exist, no pods can be selected for deletion
		{
			name:                  "no existing pods",
			existingPods:          []*corev1.Pod{},
			desiredReplicas:       2,
			expectedSelectedCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test PodClique
			pclq := &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-namespace",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas: tt.desiredReplicas,
				},
			}

			// Create sync context
			sc := &syncContext{
				pclq:             pclq,
				existingPCLQPods: tt.existingPods,
			}

			// Calculate the diff as the function used to do internally
			diff := len(tt.existingPods) - int(tt.desiredReplicas)

			// Execute the function under test
			selectedPods := selectExcessPodsToDelete(sc, logr.Discard(), diff)

			// Verify the result
			assert.Len(t, selectedPods, tt.expectedSelectedCount)

			// Verify that selected pods are from the original list
			for _, selectedPod := range selectedPods {
				assert.Contains(t, tt.existingPods, selectedPod, "Selected pod should be from existing pods list")
			}
		})
	}
}

// TestDeleteExcessPods tests the pod deletion orchestration function.
func TestDeleteExcessPods(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// existingPods are the pods currently in the cluster
		existingPods []*corev1.Pod
		// desiredReplicas is the target number of replicas
		desiredReplicas int32
		// diff is the calculated difference (positive means excess pods to delete)
		diff int
		// expectError indicates whether an error should be returned
		expectError bool
	}{
		// Should successfully delete excess pods when scaling down
		{
			name: "successful deletion of excess pods",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
				createTestPodWithUID("pod-3", "uid-3"),
				createTestPodWithUID("pod-4", "uid-4"),
			},
			desiredReplicas: 2,
			diff:            2,
			expectError:     false,
		},
		// Should not attempt deletion when diff is zero (steady state)
		{
			name: "no deletion needed when diff is zero",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
				createTestPodWithUID("pod-2", "uid-2"),
			},
			desiredReplicas: 2,
			diff:            0,
			expectError:     false,
		},
		// Should delete only available pods when diff exceeds existing pod count
		{
			name: "deletion limited by available pods",
			existingPods: []*corev1.Pod{
				createTestPodWithUID("pod-1", "uid-1"),
			},
			desiredReplicas: 2,
			diff:            5, // Want to delete 5 but only 1 pod exists
			expectError:     false,
		},
		// Should handle gracefully when trying to delete from empty pod list
		{
			name:            "no pods to delete from empty list",
			existingPods:    []*corev1.Pod{},
			desiredReplicas: 0,
			diff:            1,
			expectError:     false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with existing pods
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			objects := make([]client.Object, len(tt.existingPods))
			for i, pod := range tt.existingPods {
				objects[i] = pod
			}
			// Counter for generating unique names and UIDs
			var createCounter int
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						createCounter++
						// Handle GenerateName by setting a name if it's empty
						if obj.GetName() == "" && obj.GetGenerateName() != "" {
							obj.SetName(fmt.Sprintf("%s%d", obj.GetGenerateName(), createCounter))
						}
						// Assign a UID to created objects (mimicking real API server behavior)
						if obj.GetUID() == "" {
							obj.SetUID(types.UID(fmt.Sprintf("fake-uid-%s-%d", obj.GetName(), createCounter)))
						}
						return nil
					},
					Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						// The fake client handles the actual deletion
						return nil
					},
				}).
				Build()

			// Create test PodClique with proper naming convention
			pclq := &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs-0-pclq", // Follow expected naming convention
					Namespace: "test-namespace",
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
						common.LabelPartOfKey:    "test-pgs",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "test-pgs",
							UID:  "pgs-uid-123",
						},
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas: tt.desiredReplicas,
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image:latest",
							},
						},
					},
				},
			}

			// Create sync context
			sc := &syncContext{
				ctx:                      context.Background(),
				pclq:                     pclq,
				existingPCLQPods:         tt.existingPods,
				pclqExpectationsStoreKey: "test-namespace/test-pgs-0-pclq",
			}

			// Create resource with fake client and expectations store
			r := &_resource{
				client:            fakeClient,
				scheme:            scheme,
				eventRecorder:     &record.FakeRecorder{},
				expectationsStore: expect.NewExpectationsStore(),
			}

			// Execute the function under test
			err := r.deleteExcessPods(sc, logr.Discard(), tt.diff)

			// Verify error expectations
			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
			} else {
				require.NoError(t, err, "Unexpected error for test case: %s", tt.name)
			}

			// For successful cases, verify the behavior through expectations
			if !tt.expectError && tt.diff > 0 && len(tt.existingPods) > 0 {
				// Verify that delete expectations were created
				deleteExpectations := r.expectationsStore.GetDeleteExpectations(sc.pclqExpectationsStoreKey)
				expectedDeletions := min(tt.diff, len(tt.existingPods))
				assert.Len(t, deleteExpectations, expectedDeletions, "Should have correct number of delete expectations: %s")
			}
		})
	}
}

// TestGetAssociatedPodGangName tests the function that extracts PodGang name from PodClique labels.
func TestGetAssociatedPodGangName(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pclqObjectMeta is the PodClique metadata to test
		pclqObjectMeta metav1.ObjectMeta
		// expectedPodGangName is the expected PodGang name to be extracted
		expectedPodGangName string
		// expectError indicates whether an error should be returned
		expectError bool
	}{
		// Should successfully extract PodGang name when label is present
		{
			name: "valid PodGang label present",
			pclqObjectMeta: metav1.ObjectMeta{
				Name:      "test-pclq",
				Namespace: "default",
				Labels: map[string]string{
					common.LabelPodGang: "test-podgang",
					"other-label":       "other-value",
				},
			},
			expectedPodGangName: "test-podgang",
			expectError:         false,
		},
		// Should return error when PodGang label is missing
		{
			name: "missing PodGang label",
			pclqObjectMeta: metav1.ObjectMeta{
				Name:      "test-pclq",
				Namespace: "default",
				Labels: map[string]string{
					"other-label": "other-value",
				},
			},
			expectError: true,
		},
		// Should return error when labels map is empty
		{
			name: "empty labels map",
			pclqObjectMeta: metav1.ObjectMeta{
				Name:      "test-pclq",
				Namespace: "default",
				Labels:    map[string]string{},
			},
			expectError: true,
		},
		// Should return error when labels map is nil
		{
			name: "nil labels map",
			pclqObjectMeta: metav1.ObjectMeta{
				Name:      "test-pclq",
				Namespace: "default",
				Labels:    nil,
			},
			expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create resource (doesn't need client for this test)
			r := &_resource{}

			// Execute the function under test
			podGangName, err := r.getAssociatedPodGangName(tt.pclqObjectMeta)

			// Verify error expectations
			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
			} else {
				require.NoError(t, err, "Unexpected error for test case: %s", tt.name)
				assert.Equal(t, tt.expectedPodGangName, podGangName)
			}
		})
	}
}

// TestGetPodNamesUpdatedInAssociatedPodGang tests the function that extracts pod names
// from PodGang PodGroup specifications.
func TestGetPodNamesUpdatedInAssociatedPodGang(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// existingPodGang is the PodGang resource to extract pod names from
		existingPodGang *groveschedulerv1alpha1.PodGang
		// pclqFQN is the fully qualified name of the PodClique to look for
		pclqFQN string
		// expectedPodNames are the expected pod names to be extracted
		expectedPodNames []string
	}{
		// Should return nil when PodGang is nil
		{
			name:             "nil PodGang",
			existingPodGang:  nil,
			pclqFQN:          "test-pclq",
			expectedPodNames: nil,
		},
		// Should extract pod names when PodGroup matches the PodClique FQN
		{
			name: "PodGang with matching PodGroup",
			existingPodGang: &groveschedulerv1alpha1.PodGang{
				Spec: groveschedulerv1alpha1.PodGangSpec{
					PodGroups: []groveschedulerv1alpha1.PodGroup{
						{
							Name:        "test-pclq",
							MinReplicas: 2,
							PodReferences: []groveschedulerv1alpha1.NamespacedName{
								{Name: "pod-1"},
								{Name: "pod-2"},
								{Name: "pod-3"},
							},
						},
						{
							Name:        "other-pclq",
							MinReplicas: 1,
							PodReferences: []groveschedulerv1alpha1.NamespacedName{
								{Name: "other-pod-1"},
							},
						},
					},
				},
			},
			pclqFQN:          "test-pclq",
			expectedPodNames: []string{"pod-1", "pod-2", "pod-3"},
		},
		// Should return empty list when no PodGroup matches the PodClique FQN
		{
			name: "PodGang without matching PodGroup",
			existingPodGang: &groveschedulerv1alpha1.PodGang{
				Spec: groveschedulerv1alpha1.PodGangSpec{
					PodGroups: []groveschedulerv1alpha1.PodGroup{
						{
							Name:        "other-pclq",
							MinReplicas: 1,
							PodReferences: []groveschedulerv1alpha1.NamespacedName{
								{Name: "other-pod-1"},
							},
						},
					},
				},
			},
			pclqFQN:          "test-pclq",
			expectedPodNames: nil,
		},
		// Should return empty list when PodGroups list is empty
		{
			name: "empty PodGroups list",
			existingPodGang: &groveschedulerv1alpha1.PodGang{
				Spec: groveschedulerv1alpha1.PodGangSpec{
					PodGroups: []groveschedulerv1alpha1.PodGroup{},
				},
			},
			pclqFQN:          "test-pclq",
			expectedPodNames: nil,
		},
		// Should return empty list when matching PodGroup has no PodReferences
		{
			name: "PodGroup with empty PodReferences",
			existingPodGang: &groveschedulerv1alpha1.PodGang{
				Spec: groveschedulerv1alpha1.PodGangSpec{
					PodGroups: []groveschedulerv1alpha1.PodGroup{
						{
							Name:          "test-pclq",
							MinReplicas:   2,
							PodReferences: []groveschedulerv1alpha1.NamespacedName{},
						},
					},
				},
			},
			pclqFQN:          "test-pclq",
			expectedPodNames: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create resource (doesn't need client for this test)
			r := &_resource{}

			// Execute the function under test
			podNames := r.getPodNamesUpdatedInAssociatedPodGang(tt.existingPodGang, tt.pclqFQN)

			// Verify the result
			if tt.expectedPodNames == nil {
				assert.Nil(t, podNames)
			} else {
				assert.Equal(t, tt.expectedPodNames, podNames)
			}
		})
	}
}

// TestHasPodGangSchedulingGate tests the utility function that checks for PodGang scheduling gates.
func TestHasPodGangSchedulingGate(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pod is the pod to check for scheduling gates
		pod *corev1.Pod
		// expectedResult is whether the pod should have a PodGang scheduling gate
		expectedResult bool
	}{
		// Should return true when pod has the PodGang scheduling gate
		{
			name: "pod with PodGang scheduling gate",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{
						{Name: podGangSchedulingGate},
					},
				},
			},
			expectedResult: true,
		},
		// Should return true when pod has PodGang gate among other gates
		{
			name: "pod with PodGang and other scheduling gates",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{
						{Name: "other-gate"},
						{Name: podGangSchedulingGate},
						{Name: "another-gate"},
					},
				},
			},
			expectedResult: true,
		},
		// Should return false when pod has other gates but no PodGang gate
		{
			name: "pod with other scheduling gates but no PodGang gate",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{
						{Name: "other-gate"},
						{Name: "another-gate"},
					},
				},
			},
			expectedResult: false,
		},
		// Should return false when pod has no scheduling gates
		{
			name: "pod with no scheduling gates",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{},
				},
			},
			expectedResult: false,
		},
		// Should return false when pod has nil scheduling gates
		{
			name: "pod with nil scheduling gates",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					SchedulingGates: nil,
				},
			},
			expectedResult: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			result := hasPodGangSchedulingGate(tt.pod)

			// Verify the result
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

// TestCheckAndRemovePodSchedulingGates_MinAvailableAware tests the comprehensive scheduling gate
// removal logic with MinAvailable awareness, covering base and scaled PodGang scenarios.
func TestCheckAndRemovePodSchedulingGates_MinAvailableAware(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// podGangName is the name of the PodGang that the test pod belongs to
		podGangName string
		// basePodGangExists indicates whether the base PodGang resource should exist in the cluster
		basePodGangExists bool
		// basePodGangReady indicates whether the base PodGang should be in a ready state (all PodCliques meet MinAvailable)
		basePodGangReady bool
		// podHasGate indicates whether the test pod should have a PodGang scheduling gate
		podHasGate bool
		// podInPodGang indicates whether the pod should be marked as updated in the PodGang resource
		podInPodGang bool
		// expectedGateRemoved indicates whether the scheduling gate should be removed after the operation
		expectedGateRemoved bool
		// expectedSkippedPods is the expected number of pods that should be skipped (not have gates removed)
		expectedSkippedPods int
		// expectError indicates whether an error should be returned from the operation
		expectError bool
	}{
		// Should remove gates immediately for base PodGang pods
		{
			name:                "Base PodGang pod - gates removed immediately",
			podGangName:         "simple1-0",
			basePodGangExists:   true,
			basePodGangReady:    false, // Irrelevant for base PodGang
			podHasGate:          true,
			podInPodGang:        true,
			expectedGateRemoved: true,
			expectedSkippedPods: 0,
			expectError:         false,
		},
		// Should skip gate removal for scaled PodGang when base is not ready
		{
			name:                "Scaled PodGang pod - base not ready",
			podGangName:         "simple1-0-sga-2",
			basePodGangExists:   true,
			basePodGangReady:    false,
			podHasGate:          true,
			podInPodGang:        true,
			expectedGateRemoved: false,
			expectedSkippedPods: 1,
			expectError:         false,
		},
		// Should remove gates for scaled PodGang when base is ready
		{
			name:                "Scaled PodGang pod - base ready",
			podGangName:         "simple1-0-sga-2",
			basePodGangExists:   true,
			basePodGangReady:    true,
			podHasGate:          true,
			podInPodGang:        true,
			expectedGateRemoved: true,
			expectedSkippedPods: 0,
			expectError:         false,
		},
		// Should skip gate removal when base PodGang doesn't exist
		{
			name:                "Scaled PodGang pod - base missing",
			podGangName:         "simple1-0-sga-3",
			basePodGangExists:   false,
			basePodGangReady:    false,
			podHasGate:          true,
			podInPodGang:        true,
			expectedGateRemoved: false,
			expectedSkippedPods: 0, // No skips when error occurs
			expectError:         true,
		},
		// Should skip gate removal when pod is not yet in PodGang
		{
			name:                "Pod not in PodGang yet",
			podGangName:         "simple1-0-sga-2",
			basePodGangExists:   true,
			basePodGangReady:    true,
			podHasGate:          true,
			podInPodGang:        false,
			expectedGateRemoved: false,
			expectedSkippedPods: 1,
			expectError:         false,
		},
		// Should skip processing when pod doesn't have PodGang scheduling gate
		{
			name:                "Pod without gate",
			podGangName:         "simple1-0-sga-2",
			basePodGangExists:   true,
			basePodGangReady:    true,
			podHasGate:          false,
			podInPodGang:        true,
			expectedGateRemoved: false,
			expectedSkippedPods: 0,
			expectError:         false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test objects
			pod := createTestPod(tt.podGangName, tt.podHasGate, tt.podInPodGang)
			var objects []client.Object
			objects = append(objects, pod)

			// Check if this is testing a base PodGang or scaled PodGang based on name pattern
			isScaledPodGangTest := strings.Contains(tt.podGangName, "-sga-")

			if isScaledPodGangTest {
				// Always create the scaled PodGang for scaled PodGang tests
				scaledPodGang := &groveschedulerv1alpha1.PodGang{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tt.podGangName,
						Namespace: "default",
					},
					Spec: groveschedulerv1alpha1.PodGangSpec{
						PodGroups: []groveschedulerv1alpha1.PodGroup{
							{
								Name:        "simple1-0-sga-pcb",
								MinReplicas: 1,
							},
						},
					},
				}
				objects = append(objects, scaledPodGang)

				// Only create base PodGang if it's supposed to exist
				if tt.basePodGangExists {
					basePodGang := createTestBasePodGang(tt.podGangName, tt.basePodGangReady)
					objects = append(objects, basePodGang)
				}
			} else if tt.basePodGangExists {
				// Just create the base PodGang for base PodGang tests
				basePodGang := createTestBasePodGang(tt.podGangName, tt.basePodGangReady)
				objects = append(objects, basePodGang)
			}

			if tt.basePodGangExists {
				if tt.basePodGangReady {
					// Add ready PodClique for base PodGang
					basePclq := createTestPodClique("simple1-0-pcb", 2, 2) // ReadyReplicas >= MinAvailable
					objects = append(objects, basePclq)
				} else {
					// Add not-ready PodClique for base PodGang
					basePclq := createTestPodClique("simple1-0-pcb", 2, 1) // ReadyReplicas < MinAvailable
					objects = append(objects, basePclq)
				}
			}

			// Create fake client
			scheme := runtime.NewScheme()
			err := corev1.AddToScheme(scheme)
			require.NoError(t, err)
			err = grovecorev1alpha1.AddToScheme(scheme)
			require.NoError(t, err)
			err = groveschedulerv1alpha1.AddToScheme(scheme)
			require.NoError(t, err)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			// Create a test PodClique for the sync context
			testPclq := &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "default",
				},
			}

			// For scaled PodGang tests, add the base PodGang label to the PodClique
			// This is what the production code expects to read in checkBasePodGangReadinessForPodClique
			if isScaledPodGangTest {
				testPclq.Labels = map[string]string{
					common.LabelBasePodGang: "simple1-0",
				}
			}

			// Create resource and sync context
			r := &_resource{client: fakeClient}
			sc := &syncContext{
				ctx:                           context.Background(),
				pclq:                          testPclq,
				existingPCLQPods:              []*corev1.Pod{pod},
				podNamesUpdatedInPCLQPodGangs: []string{},
			}

			if tt.podInPodGang {
				sc.podNamesUpdatedInPCLQPodGangs = []string{pod.Name}
			}

			// Test the gate removal logic
			skippedPods, err := r.checkAndRemovePodSchedulingGates(sc, logr.Discard())

			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				// When error occurs, we don't check skipped pods as function returns early
				return
			} else {
				require.NoError(t, err, "Unexpected error for test case: %s", tt.name)
			}

			// Verify results
			assert.Len(t, skippedPods, tt.expectedSkippedPods)

			// Check if gate was actually removed
			updatedPod := &corev1.Pod{}
			err = fakeClient.Get(context.Background(), client.ObjectKeyFromObject(pod), updatedPod)
			require.NoError(t, err)

			hasGateAfter := hasPodGangSchedulingGate(updatedPod)
			if tt.expectedGateRemoved {
				assert.False(t, hasGateAfter, "Pod should not have scheduling gate after removal")
			} else if tt.podHasGate {
				assert.True(t, hasGateAfter, "Pod should still have scheduling gate")
			}
		})
	}
}

// TestCheckAndRemovePodSchedulingGates_ConcurrentExecution tests that multiple pods
// can have their scheduling gates removed concurrently without race conditions.
func TestCheckAndRemovePodSchedulingGates_ConcurrentExecution(t *testing.T) {
	// Test that multiple pods can have their gates removed concurrently without race conditions

	// Create multiple pods with gates for base PodGang (should all be processed)
	var pods []*corev1.Pod
	var objects []client.Object

	for i := 0; i < 5; i++ {
		pod := createTestPod("simple1-0", true, true) // Base PodGang, has gate, in PodGang
		pod.Name = fmt.Sprintf("test-pod-%d", i)
		pods = append(pods, pod)
		objects = append(objects, pod)
	}

	// Create a base PodGang resource (needed for the logic to work correctly)
	basePodGang := &groveschedulerv1alpha1.PodGang{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "simple1-0",
			Namespace: "default",
			// No base-podgang label - this indicates it's a base PodGang
		},
		Spec: groveschedulerv1alpha1.PodGangSpec{
			PodGroups: []groveschedulerv1alpha1.PodGroup{
				{
					Name:        "simple1-0-pcb",
					MinReplicas: 2,
				},
			},
		},
	}
	objects = append(objects, basePodGang)

	// Create fake client
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = grovecorev1alpha1.AddToScheme(scheme)
	_ = groveschedulerv1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

	// Create test PodClique for the sync context
	testPclq := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pclq",
			Namespace: "default",
		},
	}

	// Create resource and sync context
	r := &_resource{client: fakeClient}
	sc := &syncContext{
		ctx:                           context.Background(),
		pclq:                          testPclq,
		existingPCLQPods:              pods,
		podNamesUpdatedInPCLQPodGangs: []string{},
	}

	// All pods are in PodGang
	for _, pod := range pods {
		sc.podNamesUpdatedInPCLQPodGangs = append(sc.podNamesUpdatedInPCLQPodGangs, pod.Name)
	}

	// Test the gate removal logic
	skippedPods, err := r.checkAndRemovePodSchedulingGates(sc, logr.Discard())
	require.NoError(t, err)
	assert.Empty(t, skippedPods, "No pods should be skipped for base PodGang")

	// Verify all pods had their gates removed
	for i, originalPod := range pods {
		updatedPod := &corev1.Pod{}
		err = fakeClient.Get(context.Background(), client.ObjectKeyFromObject(originalPod), updatedPod)
		require.NoError(t, err)

		hasGateAfter := hasPodGangSchedulingGate(updatedPod)
		assert.False(t, hasGateAfter, "Pod %d should not have scheduling gate after removal", i)
	}
}

// TestIsBasePodGangReady tests the base PodGang readiness evaluation logic.
// A base PodGang is considered ready when all its constituent PodCliques have
// achieved their minimum required number of ready pods.
func TestIsBasePodGangReady(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// basePodGangExists indicates whether the base PodGang resource should exist
		basePodGangExists bool
		// podCliques are the test PodClique configurations to create for the base PodGang
		podCliques []testPodClique
		// expectedReady is whether the base PodGang should be considered ready
		expectedReady bool
		// expectError indicates whether an error should be returned
		expectError bool
	}{
		// Should return true when all PodCliques in base PodGang meet MinAvailable
		{
			name:              "Base PodGang ready - all PodCliques meet MinAvailable",
			basePodGangExists: true,
			podCliques: []testPodClique{
				{name: "simple1-0-pcb", minAvailable: 2, readyReplicas: 2},
				{name: "simple1-0-pcc", minAvailable: 1, readyReplicas: 3},
			},
			expectedReady: true,
			expectError:   false,
		},
		// Should return false when any PodClique is below MinAvailable
		{
			name:              "Base PodGang not ready - one PodClique below MinAvailable",
			basePodGangExists: true,
			podCliques: []testPodClique{
				{name: "simple1-0-pcb", minAvailable: 2, readyReplicas: 2},
				{name: "simple1-0-pcc", minAvailable: 3, readyReplicas: 2}, // Below MinAvailable
			},
			expectedReady: false,
			expectError:   false,
		},
		// Should return false when base PodGang doesn't exist
		{
			name:              "Base PodGang missing",
			basePodGangExists: false,
			podCliques:        []testPodClique{},
			expectedReady:     false,
			expectError:       true,
		},
		// Should return true for single PodClique meeting MinAvailable
		{
			name:              "Base PodGang ready - single PodClique",
			basePodGangExists: true,
			podCliques: []testPodClique{
				{name: "simple1-0-pcb", minAvailable: 1, readyReplicas: 1},
			},
			expectedReady: true,
			expectError:   false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object

			if tt.basePodGangExists {
				// Create the scaled PodGang with the base-podgang label
				scaledPodGang := &groveschedulerv1alpha1.PodGang{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "simple1-0-sga-2",
						Namespace: "default",
						Labels: map[string]string{
							common.LabelBasePodGang: "simple1-0",
						},
					},
					Spec: groveschedulerv1alpha1.PodGangSpec{
						PodGroups: []groveschedulerv1alpha1.PodGroup{
							{
								Name:        "simple1-0-sga-pcb",
								MinReplicas: 1,
							},
						},
					},
				}
				objects = append(objects, scaledPodGang)

				// Create the base PodGang
				basePodGang := createTestBasePodGangWithPodCliques(tt.podCliques)
				objects = append(objects, basePodGang)

				// Add corresponding PodCliques
				for _, pclq := range tt.podCliques {
					podClique := createTestPodClique(pclq.name, pclq.minAvailable, pclq.readyReplicas)
					objects = append(objects, podClique)
				}
			}

			// Create fake client
			scheme := runtime.NewScheme()
			_ = grovecorev1alpha1.AddToScheme(scheme)
			_ = groveschedulerv1alpha1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			// Test the readiness check
			r := &_resource{client: fakeClient}
			result, err := r.isBasePodGangReady(context.Background(), logr.Discard(), "default", "simple1-0")

			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				// When error occurs, result should be false and we don't check expectedReady
				assert.False(t, result, "Result should be false when error occurs")
			} else {
				require.NoError(t, err, "Unexpected error for test case: %s", tt.name)
				assert.Equal(t, tt.expectedReady, result)
			}
		})
	}
}

// TestCreatePods tests the pod creation orchestration function.
// Note: This test focuses on the main coordination logic rather than the actual pod creation tasks
// since those involve complex concurrent operations and external dependencies.
func TestCreatePods(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// existingPods are the pods that currently exist (for index calculation)
		existingPods []*corev1.Pod
		// numPodsToCreate is the number of new pods to create
		numPodsToCreate int
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedCreatedCount is the expected number of pods to be created (successful tasks)
		expectedCreatedCount int
	}{
		// Should create pods when none exist in the cluster
		{
			name:                 "create pods in empty cluster",
			existingPods:         []*corev1.Pod{},
			numPodsToCreate:      3,
			expectError:          false,
			expectedCreatedCount: 3,
		},
		// Should create pods to fill gaps in existing pod indices
		{
			name: "create pods with existing pods (fill holes)",
			existingPods: []*corev1.Pod{
				createTestPodWithHostname("pod-0", "test-pgs-0-pclq-0"),
				createTestPodWithHostname("pod-2", "test-pgs-0-pclq-2"),
			},
			numPodsToCreate:      2,
			expectError:          false,
			expectedCreatedCount: 2,
		},
		// Should handle gracefully when asked to create zero pods
		{
			name:                 "create zero pods",
			existingPods:         []*corev1.Pod{},
			numPodsToCreate:      0,
			expectError:          false,
			expectedCreatedCount: 0,
		},
		// Should create pods with sequential indices when no gaps exist
		{
			name: "create pods with sequential indices",
			existingPods: []*corev1.Pod{
				createTestPodWithHostname("pod-0", "test-pgs-0-pclq-0"),
				createTestPodWithHostname("pod-1", "test-pgs-0-pclq-1"),
			},
			numPodsToCreate:      2,
			expectError:          false,
			expectedCreatedCount: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			objects := make([]client.Object, len(tt.existingPods))
			for i, pod := range tt.existingPods {
				objects[i] = pod
			}
			// Counter for generating unique names and UIDs
			var createCounter int
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						createCounter++
						// Handle GenerateName by setting a name if it's empty
						if obj.GetName() == "" && obj.GetGenerateName() != "" {
							obj.SetName(fmt.Sprintf("%s%d", obj.GetGenerateName(), createCounter))
						}
						// Assign a UID to created objects (mimicking real API server behavior)
						if obj.GetUID() == "" {
							obj.SetUID(types.UID(fmt.Sprintf("fake-uid-%s-%d", obj.GetName(), createCounter)))
						}
						return nil
					},
				}).
				Build()

			// Create test PodGangSet
			pgs := &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "test-namespace",
				},
			}

			// Create test PodClique with proper naming convention
			pclq := &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs-0-pclq", // Follow expected naming convention
					Namespace: "test-namespace",
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
						common.LabelPartOfKey:    "test-pgs",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "test-pgs",
							UID:  "pgs-uid-123",
						},
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas: 5,
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image:latest",
							},
						},
					},
				},
			}

			// Create sync context
			sc := &syncContext{
				ctx:                      context.Background(),
				pgs:                      pgs,
				pclq:                     pclq,
				associatedPodGangName:    "test-podgang",
				existingPCLQPods:         tt.existingPods,
				pclqExpectationsStoreKey: "test-namespace/test-pgs-0-pclq",
			}

			// Create resource with fake client and expectations store
			r := &_resource{
				client:            fakeClient,
				scheme:            scheme,
				eventRecorder:     &record.FakeRecorder{},
				expectationsStore: expect.NewExpectationsStore(),
			}

			// Execute the function under test
			createdCount, err := r.createPods(context.Background(), logr.Discard(), sc, tt.numPodsToCreate)

			// Verify error expectations
			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
			} else {
				require.NoError(t, err, "Unexpected error for test case: %s", tt.name)
				assert.Equal(t, tt.expectedCreatedCount, createdCount)
			}

			// For successful cases, verify that create expectations were set
			if !tt.expectError && tt.numPodsToCreate > 0 {
				createExpectations := r.expectationsStore.GetCreateExpectations(sc.pclqExpectationsStoreKey)
				assert.Len(t, createExpectations, tt.expectedCreatedCount, "Should have correct number of create expectations")
			}
		})
	}
}

// TestRunSyncFlow tests the main sync flow orchestration function.
// This test focuses on the high-level coordination and decision making
// rather than the detailed implementation of individual operations.
func TestRunSyncFlow(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// existingPods are the pods that currently exist in the cluster
		existingPods []*corev1.Pod
		// desiredReplicas is the target number of replicas for the PodClique
		desiredReplicas int32
		// createExpectations are UIDs expected to be created (simulating pending creates)
		createExpectations []string
		// deleteExpectations are UIDs expected to be deleted (simulating pending deletes)
		deleteExpectations []string
		// expectCreatePods indicates whether pod creation should be triggered
		expectCreatePods bool
		// expectDeletePods indicates whether pod deletion should be triggered
		expectDeletePods bool
		// expectError indicates whether an error should be returned
		expectError bool
	}{
		// Should create pods when scaling up from fewer to more replicas
		{
			name:             "scale up scenario - create pods",
			existingPods:     []*corev1.Pod{},
			desiredReplicas:  3,
			expectCreatePods: true,
			expectDeletePods: false,
			expectError:      false,
		},
		// Should delete pods when scaling down from more to fewer replicas
		{
			name: "scale down scenario - delete pods",
			existingPods: []*corev1.Pod{
				createTestPodWithHostname("pod-0", "test-pgs-0-pclq-0"),
				createTestPodWithHostname("pod-1", "test-pgs-0-pclq-1"),
				createTestPodWithHostname("pod-2", "test-pgs-0-pclq-2"),
				createTestPodWithHostname("pod-3", "test-pgs-0-pclq-3"),
			},
			desiredReplicas:  2,
			expectCreatePods: false,
			expectDeletePods: true,
			expectError:      false,
		},
		// Should take no action when current replicas match desired replicas
		{
			name: "steady state - no action needed",
			existingPods: []*corev1.Pod{
				createTestPodWithHostname("pod-0", "test-pgs-0-pclq-0"),
				createTestPodWithHostname("pod-1", "test-pgs-0-pclq-1"),
			},
			desiredReplicas:  2,
			expectCreatePods: false,
			expectDeletePods: false,
			expectError:      false,
		},
		// Should skip action when pending create expectations exist
		{
			name: "expectations prevent action - pending creates",
			existingPods: []*corev1.Pod{
				createTestPodWithHostname("pod-0", "test-pgs-0-pclq-0"),
			},
			desiredReplicas:    3,
			createExpectations: []string{"pending-uid-1", "pending-uid-2"},
			expectCreatePods:   false,
			expectDeletePods:   false,
			expectError:        false,
		},
		// Should skip action when pending delete expectations exist
		{
			name: "expectations prevent action - pending deletes",
			existingPods: []*corev1.Pod{
				createTestPodWithHostname("pod-0", "test-pgs-0-pclq-0"),
				createTestPodWithHostname("pod-1", "test-pgs-0-pclq-1"),
				createTestPodWithHostname("pod-2", "test-pgs-0-pclq-2"),
				createTestPodWithHostname("pod-3", "test-pgs-0-pclq-3"),
			},
			desiredReplicas:    2,
			deleteExpectations: []string{"pod-2-uid", "pod-3-uid"},
			expectCreatePods:   false,
			expectDeletePods:   false,
			expectError:        false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create expectations store and populate with test expectations
			expectationsStore := expect.NewExpectationsStore()
			pclqKey := "test-namespace/test-pgs-0-pclq"

			// Add create expectations if any
			if len(tt.createExpectations) > 0 {
				createUIDs := make([]types.UID, len(tt.createExpectations))
				for i, uid := range tt.createExpectations {
					createUIDs[i] = types.UID(uid)
				}
				err := expectationsStore.ExpectCreations(logr.Discard(), pclqKey, createUIDs...)
				require.NoError(t, err)
			}

			// Add delete expectations if any
			if len(tt.deleteExpectations) > 0 {
				deleteUIDs := make([]types.UID, len(tt.deleteExpectations))
				for i, uid := range tt.deleteExpectations {
					deleteUIDs[i] = types.UID(uid)
				}
				err := expectationsStore.ExpectDeletions(logr.Discard(), pclqKey, deleteUIDs...)
				require.NoError(t, err)
			}

			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			objects := make([]client.Object, len(tt.existingPods))
			for i, pod := range tt.existingPods {
				objects[i] = pod
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			// Create test PodGangSet
			pgs := &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "test-namespace",
				},
			}

			// Create test PodClique
			pclq := &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs-0-pclq",
					Namespace: "test-namespace",
					Labels: map[string]string{
						common.LabelManagedByKey: common.LabelManagedByValue,
						common.LabelPartOfKey:    "test-pgs",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "test-pgs",
							UID:  "pgs-uid-123",
						},
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas: tt.desiredReplicas,
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image:latest",
							},
						},
					},
				},
			}

			// Create sync context
			sc := &syncContext{
				ctx:                           context.Background(),
				pgs:                           pgs,
				pclq:                          pclq,
				associatedPodGangName:         "test-podgang",
				existingPCLQPods:              tt.existingPods,
				podNamesUpdatedInPCLQPodGangs: []string{}, // Empty for simplicity
				pclqExpectationsStoreKey:      "test-namespace/test-pgs-0-pclq",
			}

			// Create resource with fake client and expectations store
			r := &_resource{
				client:            fakeClient,
				scheme:            scheme,
				eventRecorder:     &record.FakeRecorder{},
				expectationsStore: expectationsStore,
			}

			// Execute the function under test
			result := r.runSyncFlow(logr.Discard(), sc)

			// Verify error expectations
			if tt.expectError {
				assert.True(t, result.hasErrors(), "Expected error for test case: %s", tt.name)
			} else {
				assert.False(t, result.hasErrors(), "Unexpected error for test case: %s - errors: %v", tt.name, result.getAggregatedError())
			}

			// Verify action expectations by checking the expectations store state
			finalCreateExpectations := r.expectationsStore.GetCreateExpectations(pclqKey)
			finalDeleteExpectations := r.expectationsStore.GetDeleteExpectations(pclqKey)

			if tt.expectCreatePods {
				assert.NotEmpty(t, finalCreateExpectations, "Should have create expectations when creating pods: %s")
			}

			if tt.expectDeletePods {
				assert.NotEmpty(t, finalDeleteExpectations, "Should have delete expectations when deleting pods: %s")
			}

			// If no actions expected, verify no new expectations were added beyond initial test setup
			if !tt.expectCreatePods && !tt.expectDeletePods {
				// For this simplified test, we're mainly checking that the flow completed without errors
				// and that the logic correctly determined no action was needed
				assert.False(t, result.hasErrors(), "Should complete without errors when no action needed: %s")
			}
		})
	}
}

// Test helper types and functions

// testPodClique represents a test configuration for a PodClique used in base PodGang readiness tests.
type testPodClique struct {
	// name is the fully qualified name of the PodClique
	name string
	// minAvailable is the minimum number of pods required to be ready for this PodClique
	minAvailable int32
	// readyReplicas is the current number of ready replicas for this PodClique
	readyReplicas int32
}

func createTestPod(podGangName string, hasGate bool, inPodGang bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    map[string]string{},
		},
		Spec: corev1.PodSpec{},
	}

	if hasGate {
		pod.Spec.SchedulingGates = []corev1.PodSchedulingGate{
			{Name: podGangSchedulingGate},
		}
	}

	if inPodGang {
		pod.Labels[common.LabelPodGang] = podGangName

		// Add base-podgang label for scaled PodGang pods
		if strings.Contains(podGangName, "-sga-") {
			// Extract base PodGang name: "simple1-0-sga-2" -> "simple1-0"
			if sgaIndex := strings.Index(podGangName, "-sga-"); sgaIndex != -1 {
				basePodGangName := podGangName[:sgaIndex]
				pod.Labels[common.LabelBasePodGang] = basePodGangName
			}
		}
	}

	return pod
}

// createTestPodWithName creates a test pod with specific name and owner labels for testing
func createTestPodWithName(name, pgsName, pclqName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				common.LabelManagedByKey: common.LabelManagedByValue,
				common.LabelPartOfKey:    pgsName,
				common.LabelPodClique:    pclqName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "grove.io/v1alpha1",
					Kind:       "PodClique",
					Name:       pclqName,
					UID:        "pclq-uid-123",
					Controller: ptr.To(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}
}

// createTestPodWithUID creates a test pod with a specific UID for expectation testing
func createTestPodWithUID(name, uid string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-namespace",
			UID:       types.UID(uid),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}
}

// createTerminatingPodWithUID creates a test pod with DeletionTimestamp set (terminating)
func createTerminatingPodWithUID(name, uid string) *corev1.Pod {
	now := metav1.Now()
	pod := createTestPodWithUID(name, uid)
	pod.DeletionTimestamp = &now
	return pod
}

// createTestPodWithHostname creates a test pod with a specific hostname for index testing
func createTestPodWithHostname(name, hostname string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-namespace",
			UID:       types.UID(name + "-uid"),
		},
		Spec: corev1.PodSpec{
			Hostname: hostname,
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}
}

// createTestPodGangs creates both scaled and base PodGangs for testing
func createTestPodGangs(scaledPodGangName string, ready bool) []client.Object {
	basePodGangName := "simple1-0"
	var objects []client.Object

	// Create the scaled PodGang (no longer needs base-podgang label since production code doesn't add it)
	scaledPodGang := &groveschedulerv1alpha1.PodGang{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scaledPodGangName,
			Namespace: "default",
		},
		Spec: groveschedulerv1alpha1.PodGangSpec{
			PodGroups: []groveschedulerv1alpha1.PodGroup{
				{
					Name:        "simple1-0-sga-pcb",
					MinReplicas: 1,
				},
			},
		},
	}
	objects = append(objects, scaledPodGang)

	// Create the base PodGang
	podGroups := []groveschedulerv1alpha1.PodGroup{
		{
			Name:        "simple1-0-pcb",
			MinReplicas: 2,
		},
	}

	if !ready {
		// Add another PodGroup to make it more complex
		podGroups = append(podGroups, groveschedulerv1alpha1.PodGroup{
			Name:        "simple1-0-pcc",
			MinReplicas: 3,
		})
	}

	basePodGang := &groveschedulerv1alpha1.PodGang{
		ObjectMeta: metav1.ObjectMeta{
			Name:      basePodGangName,
			Namespace: "default",
		},
		Spec: groveschedulerv1alpha1.PodGangSpec{
			PodGroups: podGroups,
		},
	}
	objects = append(objects, basePodGang)

	return objects
}

// Legacy function for backwards compatibility - now returns just the base PodGang
func createTestBasePodGang(scaledPodGangName string, ready bool) *groveschedulerv1alpha1.PodGang {
	objects := createTestPodGangs(scaledPodGangName, ready)
	// Return the base PodGang (last object in the list)
	return objects[len(objects)-1].(*groveschedulerv1alpha1.PodGang)
}

func createTestBasePodGangWithPodCliques(podCliques []testPodClique) *groveschedulerv1alpha1.PodGang {
	podGroups := make([]groveschedulerv1alpha1.PodGroup, len(podCliques))
	for i, pclq := range podCliques {
		podGroups[i] = groveschedulerv1alpha1.PodGroup{
			Name:        pclq.name,
			MinReplicas: pclq.minAvailable,
		}
	}

	return &groveschedulerv1alpha1.PodGang{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "simple1-0",
			Namespace: "default",
		},
		Spec: groveschedulerv1alpha1.PodGangSpec{
			PodGroups: podGroups,
		},
	}
}

func createTestPodClique(name string, minAvailable, readyReplicas int32) *grovecorev1alpha1.PodClique {
	return &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: grovecorev1alpha1.PodCliqueSpec{
			MinAvailable: ptr.To(minAvailable),
		},
		Status: grovecorev1alpha1.PodCliqueStatus{
			ReadyReplicas: readyReplicas,
		},
	}
}
