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
	"errors"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespaceForPod = "test-namespace"
	testPGSNameForPod   = "test-pgs"
	testPCLQNameForPod  = "test-podclique"
	testPodBaseName     = "test-pod"
)

// mockErrorClient is a mock client that simulates errors for testing error scenarios
type mockErrorClient struct {
	client.Client
	listError error
}

// List implements the client.Client interface and returns the configured error
func (e *mockErrorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if e.listError != nil {
		return e.listError
	}
	return e.Client.List(ctx, list, opts...)
}

// TestGetPCLQPods verifies that GetPCLQPods correctly retrieves pods belonging to a specific PodClique
// by filtering based on labels and owner references. It tests various scenarios including pods
// with correct labels but wrong ownership, correct ownership but wrong labels, and proper matches.
func TestGetPCLQPods(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	// Create a test PodClique that will own some pods
	pclqUID := uuid.NewUUID()
	pclq := &grovecorev1alpha1.PodClique{
		TypeMeta: metav1.TypeMeta{
			APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
			Kind:       "PodClique",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPCLQNameForPod,
			Namespace: testNamespaceForPod,
			UID:       pclqUID,
		},
	}

	// Helper function to create expected labels for PodGangSet managed resources
	expectedLabels := func() map[string]string {
		baseLabels := k8sutils.GetDefaultLabelsForPodGangSetManagedResources(testPGSNameForPod)
		baseLabels[grovecorev1alpha1.LabelPodClique] = testPCLQNameForPod
		return baseLabels
	}

	testCases := []struct {
		// name describes the test scenario for better test output readability
		name string
		// pgsName specifies the PodGangSet name used in label filtering
		pgsName string
		// pclq is the PodClique resource for which we're finding pods
		pclq *grovecorev1alpha1.PodClique
		// existingPods are the pods that exist in the cluster before the function call
		existingPods []*corev1.Pod
		// expectedPodNames contains the names of pods that should be returned by GetPCLQPods
		expectedPodNames []string
		// expectedError indicates whether an error should be returned
		expectedError bool
	}{
		{
			// Should return no pods when no pods exist in the cluster
			name:             "empty_cluster",
			pgsName:          testPGSNameForPod,
			pclq:             pclq,
			existingPods:     []*corev1.Pod{},
			expectedPodNames: []string{},
			expectedError:    false,
		},
		{
			// Should return only pods that have correct labels AND are owned by the PodClique
			name:    "correct_labels_and_ownership",
			pgsName: testPGSNameForPod,
			pclq:    pclq,
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-pod-with-labels",
						Namespace: testNamespaceForPod,
						Labels:    expectedLabels(),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodClique",
								Name:       testPCLQNameForPod,
								UID:        pclqUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedPodNames: []string{"owned-pod-with-labels"},
			expectedError:    false,
		},
		{
			// Should exclude pods with correct labels but wrong ownership
			name:    "wrong_ownership_excluded",
			pgsName: testPGSNameForPod,
			pclq:    pclq,
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "labeled-but-unowned",
						Namespace: testNamespaceForPod,
						Labels:    expectedLabels(),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodClique",
								Name:       "other-podclique",
								UID:        uuid.NewUUID(),
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedPodNames: []string{},
			expectedError:    false,
		},
		{
			// Should exclude pods with correct ownership but missing required labels
			name:    "missing_labels_excluded",
			pgsName: testPGSNameForPod,
			pclq:    pclq,
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-but-unlabeled",
						Namespace: testNamespaceForPod,
						Labels: map[string]string{
							"some-other-label": "value",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodClique",
								Name:       testPCLQNameForPod,
								UID:        pclqUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedPodNames: []string{},
			expectedError:    false,
		},
		{
			// Should handle mixed scenarios correctly - some matching, some not
			name:    "mixed_scenarios_with_multiple_pods",
			pgsName: testPGSNameForPod,
			pclq:    pclq,
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "matching-pod-1",
						Namespace: testNamespaceForPod,
						Labels:    expectedLabels(),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodClique",
								Name:       testPCLQNameForPod,
								UID:        pclqUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "matching-pod-2",
						Namespace: testNamespaceForPod,
						Labels:    expectedLabels(),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodClique",
								Name:       testPCLQNameForPod,
								UID:        pclqUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "wrong-namespace-pod",
						Namespace: "wrong-namespace",
						Labels:    expectedLabels(),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodClique",
								Name:       testPCLQNameForPod,
								UID:        pclqUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "wrong-owner-pod",
						Namespace: testNamespaceForPod,
						Labels:    expectedLabels(),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodClique",
								Name:       "other-podclique",
								UID:        uuid.NewUUID(),
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedPodNames: []string{"matching-pod-1", "matching-pod-2"},
			expectedError:    false,
		},
		{
			// Should handle pods in different namespace correctly (should be excluded)
			name:    "different_namespace_excluded",
			pgsName: testPGSNameForPod,
			pclq:    pclq,
			existingPods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "different-namespace-pod",
						Namespace: "different-namespace",
						Labels:    expectedLabels(),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
								Kind:       "PodClique",
								Name:       testPCLQNameForPod,
								UID:        pclqUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedPodNames: []string{},
			expectedError:    false,
		},
		{
			// Should return error when client.List() fails
			name:             "client_list_error",
			pgsName:          testPGSNameForPod,
			pclq:             pclq,
			existingPods:     []*corev1.Pod{},
			expectedPodNames: []string{},
			expectedError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Convert pod pointers to objects for the fake client
			objects := make([]client.Object, 0, len(tc.existingPods)+1)
			objects = append(objects, tc.pclq)
			for _, pod := range tc.existingPods {
				objects = append(objects, pod)
			}

			var testClient client.Client

			// Create appropriate client based on test case
			if tc.name == "client_list_error" {
				// Create fake client and wrap it with error client for error simulation
				baseFakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(objects...).
					Build()
				testClient = &mockErrorClient{
					Client:    baseFakeClient,
					listError: errors.New("simulated client.List() error"),
				}
			} else {
				// Create normal fake client for other test cases
				testClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(objects...).
					Build()
			}

			// Call the function under test
			result, err := GetPCLQPods(context.Background(), testClient, tc.pgsName, tc.pclq)

			// Verify error expectation
			if tc.expectedError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify the number of returned pods
			assert.Len(t, result, len(tc.expectedPodNames))

			// Verify the returned pod names match expectations
			actualPodNames := make([]string, len(result))
			for i, pod := range result {
				actualPodNames[i] = pod.Name
			}
			assert.ElementsMatch(t, tc.expectedPodNames, actualPodNames)

			// Verify that all returned pods are indeed owned by the PodClique
			for _, pod := range result {
				assert.True(t, metav1.IsControlledBy(pod, tc.pclq), "Pod %s should be controlled by PodClique %s", pod.Name, tc.pclq.Name)
			}
		})
	}
}

// TestGetPCLQPods_ErrorScenarios specifically tests error handling in GetPCLQPods
// to ensure proper error propagation when the Kubernetes client encounters failures.
func TestGetPCLQPods_ErrorScenarios(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

	// Create a test PodClique
	pclq := &grovecorev1alpha1.PodClique{
		TypeMeta: metav1.TypeMeta{
			APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
			Kind:       "PodClique",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPCLQNameForPod,
			Namespace: testNamespaceForPod,
			UID:       uuid.NewUUID(),
		},
	}

	testCases := []struct {
		// name describes the error scenario being tested
		name string
		// clientError is the error that the mock client will return
		clientError error
		// expectedErrorContains is a substring that should be present in the returned error
		expectedErrorContains string
	}{
		{
			// Should propagate client.List() errors correctly
			name:                  "api_server_unavailable",
			clientError:           errors.New("connection refused: api server unavailable"),
			expectedErrorContains: "connection refused",
		},
		{
			// Should propagate permission errors correctly
			name:                  "permission_denied",
			clientError:           errors.New("pods is forbidden: User cannot list resource pods in API group"),
			expectedErrorContains: "forbidden",
		},
		{
			// Should propagate timeout errors correctly
			name:                  "request_timeout",
			clientError:           errors.New("context deadline exceeded"),
			expectedErrorContains: "deadline exceeded",
		},
		{
			// Should propagate network errors correctly
			name:                  "network_error",
			clientError:           errors.New("network is unreachable"),
			expectedErrorContains: "network is unreachable",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create error client that simulates the specific error
			baseFakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pclq).
				Build()

			testErrorClient := &mockErrorClient{
				Client:    baseFakeClient,
				listError: tc.clientError,
			}

			// Call the function under test
			result, err := GetPCLQPods(context.Background(), testErrorClient, testPGSNameForPod, pclq)

			// Verify error is returned and propagated correctly
			assert.Error(t, err, "Expected error for scenario: %s", tc.name)
			assert.Nil(t, result, "Result should be nil when error occurs")
			assert.Contains(t, err.Error(), tc.expectedErrorContains,
				"Error should contain expected substring for scenario: %s", tc.name)
		})
	}
}

// TestAddEnvVarsToContainers verifies that AddEnvVarsToContainers correctly appends
// environment variables to all containers in the provided slice. It tests scenarios
// with empty containers, single container, multiple containers, and edge cases.
func TestAddEnvVarsToContainers(t *testing.T) {
	testCases := []struct {
		// name describes the test scenario for better test output readability
		name string
		// containers is the initial slice of containers to which env vars will be added
		containers []corev1.Container
		// envVars are the environment variables to be added to each container
		envVars []corev1.EnvVar
		// expectedEnvCounts maps container indices to their expected total environment variable count after addition
		expectedEnvCounts map[int]int
		// expectedEnvVars maps container indices to their expected environment variables after addition
		expectedEnvVars map[int][]corev1.EnvVar
	}{
		{
			// Should handle empty containers slice gracefully
			name:              "empty_containers_slice",
			containers:        []corev1.Container{},
			envVars:           []corev1.EnvVar{{Name: "TEST_VAR", Value: "test"}},
			expectedEnvCounts: map[int]int{},
			expectedEnvVars:   map[int][]corev1.EnvVar{},
		},
		{
			// Should handle empty env vars slice gracefully
			name: "empty_env_vars_slice",
			containers: []corev1.Container{
				{Name: "test-container", Env: []corev1.EnvVar{{Name: "EXISTING", Value: "value"}}},
			},
			envVars:           []corev1.EnvVar{},
			expectedEnvCounts: map[int]int{0: 1},
			expectedEnvVars: map[int][]corev1.EnvVar{
				0: {{Name: "EXISTING", Value: "value"}},
			},
		},
		{
			// Should add env vars to single container with no existing env vars
			name: "single_container_no_existing_env_vars",
			containers: []corev1.Container{
				{Name: "test-container"},
			},
			envVars: []corev1.EnvVar{
				{Name: "NEW_VAR1", Value: "value1"},
				{Name: "NEW_VAR2", Value: "value2"},
			},
			expectedEnvCounts: map[int]int{0: 2},
			expectedEnvVars: map[int][]corev1.EnvVar{
				0: {
					{Name: "NEW_VAR1", Value: "value1"},
					{Name: "NEW_VAR2", Value: "value2"},
				},
			},
		},
		{
			// Should append env vars to single container with existing env vars
			name: "single_container_with_existing_env_vars",
			containers: []corev1.Container{
				{
					Name: "test-container",
					Env: []corev1.EnvVar{
						{Name: "EXISTING1", Value: "existing1"},
						{Name: "EXISTING2", Value: "existing2"},
					},
				},
			},
			envVars: []corev1.EnvVar{
				{Name: "NEW_VAR1", Value: "new1"},
				{Name: "NEW_VAR2", Value: "new2"},
			},
			expectedEnvCounts: map[int]int{0: 4},
			expectedEnvVars: map[int][]corev1.EnvVar{
				0: {
					{Name: "EXISTING1", Value: "existing1"},
					{Name: "EXISTING2", Value: "existing2"},
					{Name: "NEW_VAR1", Value: "new1"},
					{Name: "NEW_VAR2", Value: "new2"},
				},
			},
		},
		{
			// Should add env vars to multiple containers consistently
			name: "multiple_containers",
			containers: []corev1.Container{
				{
					Name: "container1",
					Env: []corev1.EnvVar{
						{Name: "EXISTING_C1", Value: "value_c1"},
					},
				},
				{
					Name: "container2",
					Env: []corev1.EnvVar{
						{Name: "EXISTING_C2_1", Value: "value_c2_1"},
						{Name: "EXISTING_C2_2", Value: "value_c2_2"},
					},
				},
				{
					Name: "container3",
					// No existing env vars
				},
			},
			envVars: []corev1.EnvVar{
				{Name: "SHARED_VAR1", Value: "shared1"},
				{Name: "SHARED_VAR2", Value: "shared2"},
			},
			expectedEnvCounts: map[int]int{0: 3, 1: 4, 2: 2},
			expectedEnvVars: map[int][]corev1.EnvVar{
				0: {
					{Name: "EXISTING_C1", Value: "value_c1"},
					{Name: "SHARED_VAR1", Value: "shared1"},
					{Name: "SHARED_VAR2", Value: "shared2"},
				},
				1: {
					{Name: "EXISTING_C2_1", Value: "value_c2_1"},
					{Name: "EXISTING_C2_2", Value: "value_c2_2"},
					{Name: "SHARED_VAR1", Value: "shared1"},
					{Name: "SHARED_VAR2", Value: "shared2"},
				},
				2: {
					{Name: "SHARED_VAR1", Value: "shared1"},
					{Name: "SHARED_VAR2", Value: "shared2"},
				},
			},
		},
		{
			// Should handle env vars with complex configurations (ValueFrom, etc.)
			name: "complex_env_var_configurations",
			containers: []corev1.Container{
				{Name: "test-container"},
			},
			envVars: []corev1.EnvVar{
				{Name: "SIMPLE_VAR", Value: "simple"},
				{
					Name: "SECRET_VAR",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
							Key:                  "secret-key",
						},
					},
				},
				{
					Name: "CONFIGMAP_VAR",
					ValueFrom: &corev1.EnvVarSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "my-configmap"},
							Key:                  "config-key",
						},
					},
				},
			},
			expectedEnvCounts: map[int]int{0: 3},
			expectedEnvVars: map[int][]corev1.EnvVar{
				0: {
					{Name: "SIMPLE_VAR", Value: "simple"},
					{
						Name: "SECRET_VAR",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
								Key:                  "secret-key",
							},
						},
					},
					{
						Name: "CONFIGMAP_VAR",
						ValueFrom: &corev1.EnvVarSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "my-configmap"},
								Key:                  "config-key",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Make a deep copy of containers to avoid test interference
			containersCopy := make([]corev1.Container, len(tc.containers))
			for j, container := range tc.containers {
				containersCopy[j] = corev1.Container{
					Name: container.Name,
					Env:  make([]corev1.EnvVar, len(container.Env)),
				}
				copy(containersCopy[j].Env, container.Env)
			}

			// Call the function under test
			AddEnvVarsToContainers(containersCopy, tc.envVars)

			// Verify the number of containers hasn't changed
			assert.Len(t, containersCopy, len(tc.containers))

			// Verify each container has the expected number of environment variables
			for containerIndex, expectedCount := range tc.expectedEnvCounts {
				assert.Len(t, containersCopy[containerIndex].Env, expectedCount,
					"Container %d should have %d env vars", containerIndex, expectedCount)
			}

			// Verify each container has the expected environment variables
			for containerIndex, expectedEnvVars := range tc.expectedEnvVars {
				assert.Equal(t, expectedEnvVars, containersCopy[containerIndex].Env,
					"Container %d should have expected env vars", containerIndex)
			}

			// Verify that the original env vars are preserved and new ones are appended
			for containerIndex, container := range containersCopy {
				if containerIndex < len(tc.containers) {
					originalEnvCount := len(tc.containers[containerIndex].Env)
					newEnvCount := len(tc.envVars)

					// Check that original env vars are still at the beginning
					for j := 0; j < originalEnvCount; j++ {
						assert.Equal(t, tc.containers[containerIndex].Env[j], container.Env[j],
							"Original env var %d in container %d should be preserved", j, containerIndex)
					}

					// Check that new env vars are appended at the end
					for j := 0; j < newEnvCount; j++ {
						envIndex := originalEnvCount + j
						if envIndex < len(container.Env) {
							assert.Equal(t, tc.envVars[j], container.Env[envIndex],
								"New env var %d in container %d should be appended correctly", j, containerIndex)
						}
					}
				}
			}
		})
	}
}

// TestAddEnvVarsToContainers_Mutation verifies that AddEnvVarsToContainers modifies
// the containers slice in-place and that the modifications persist after the function call.
func TestAddEnvVarsToContainers_Mutation(t *testing.T) {
	// Setup initial containers
	containers := []corev1.Container{
		{
			Name: "test-container",
			Env: []corev1.EnvVar{
				{Name: "ORIGINAL", Value: "original"},
			},
		},
	}

	// Setup env vars to add
	envVars := []corev1.EnvVar{
		{Name: "ADDED", Value: "added"},
	}

	// Record original state
	originalEnvCount := len(containers[0].Env)

	// Call function
	AddEnvVarsToContainers(containers, envVars)

	// Verify mutation occurred
	assert.Len(t, containers[0].Env, originalEnvCount+len(envVars),
		"Container should have additional env vars after function call")

	assert.Equal(t, "ORIGINAL", containers[0].Env[0].Name,
		"Original env var should be preserved")
	assert.Equal(t, "original", containers[0].Env[0].Value,
		"Original env var value should be preserved")

	assert.Equal(t, "ADDED", containers[0].Env[1].Name,
		"New env var should be added")
	assert.Equal(t, "added", containers[0].Env[1].Value,
		"New env var value should be set correctly")
}

// TestAddEnvVarsToContainers_NilSlices verifies that AddEnvVarsToContainers handles
// nil slices gracefully without panicking.
func TestAddEnvVarsToContainers_NilSlices(t *testing.T) {
	testCases := []struct {
		// name describes the test scenario for better test output readability
		name string
		// containers slice to test with
		containers []corev1.Container
		// envVars slice to test with
		envVars []corev1.EnvVar
	}{
		{
			// Both slices are nil
			name:       "both_slices_nil",
			containers: nil,
			envVars:    nil,
		},
		{
			// Containers slice is nil, envVars is not
			name:       "containers_nil_envvars_not_nil",
			containers: nil,
			envVars:    []corev1.EnvVar{{Name: "TEST", Value: "test"}},
		},
		{
			// Containers slice is not nil, envVars is nil
			name:       "containers_not_nil_envvars_nil",
			containers: []corev1.Container{{Name: "test"}},
			envVars:    nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				AddEnvVarsToContainers(tc.containers, tc.envVars)
			})
		})
	}
}
