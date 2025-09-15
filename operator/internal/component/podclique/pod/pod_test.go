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

package pod

import (
	"context"
	"testing"

	"github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/expect"
	testutils "github.com/NVIDIA/grove/operator/test/utils"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

// TestAddEnvironmentVariables verifies that Grove-specific environment variables
// are correctly added to all containers in a Pod based on the PodClique configuration.
func TestAddEnvironmentVariables(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pclq is the PodClique resource containing the Pod template
		pclq *grovecorev1alpha1.PodClique
		// expectedEnvVars are the environment variable names that should be present
		expectedEnvVars []string
		// unexpectedEnvVars are the environment variable names that should NOT be present
		unexpectedEnvVars []string
	}{
		{
			// Tests that a standalone PodClique (not part of a PCSG) gets all required Grove environment variables
			name: "standalone PodClique",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image",
							},
						},
					},
				},
			},
			expectedEnvVars: []string{
				constants.EnvVarPGSName,
				constants.EnvVarPGSIndex,
				constants.EnvVarPCLQName,
				constants.EnvVarHeadlessService,
				constants.EnvVarPodIndex,
			},
		},
		{
			// Tests that a PodClique that is part of a PCSG gets the same Grove environment variables as standalone
			name: "PCSG member PodClique",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
					Labels: map[string]string{
						common.LabelPodCliqueScalingGroup: "test-pcsg",
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image",
							},
						},
					},
				},
			},
			expectedEnvVars: []string{
				constants.EnvVarPGSName,
				constants.EnvVarPGSIndex,
				constants.EnvVarPCLQName,
				constants.EnvVarHeadlessService,
				constants.EnvVarPodIndex,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				Spec: tt.pclq.Spec.PodSpec,
			}

			addEnvironmentVariables(pod, tt.pclq, "test-pgs", 0, 0)

			// Check that all containers have the expected environment variables
			for _, container := range pod.Spec.Containers {
				assertExpectedEnvVars(t, container, tt.expectedEnvVars)

				// Check unexpected environment variables are not present
				envVarNames := make(map[string]bool)
				for _, env := range container.Env {
					envVarNames[env.Name] = true
				}
				for _, unexpectedEnv := range tt.unexpectedEnvVars {
					if envVarNames[unexpectedEnv] {
						t.Errorf("unexpected environment variable %s found in container %s", unexpectedEnv, container.Name)
					}
				}

				// Verify Grove environment variables use direct values
				assertGroveEnvVarsDirectValues(t, container, tt.expectedEnvVars)
			}
		})
	}
}

// TestAddGroveEnvironmentVariables_NoDuplicates verifies that the environment variable
// injection handles existing environment variables correctly by replacing Grove variables
// and preserving user-defined variables without creating duplicates.
func TestAddGroveEnvironmentVariables_NoDuplicates(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pclq is the PodClique resource containing the Pod template
		pclq *grovecorev1alpha1.PodClique
		// existingEnvVars are environment variables already present in the container
		existingEnvVars []corev1.EnvVar
		// expectedEnvVars are the Grove environment variable names that should be present
		expectedEnvVars []string
		// shouldReplace maps Grove env var names to their expected values after replacement
		shouldReplace map[string]string
		// shouldPreserve lists user env var names that should remain unchanged
		shouldPreserve []string
	}{
		{
			// Tests that existing Grove environment variables are replaced with correct values
			name: "Container with existing Grove env vars",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image",
								Env: []corev1.EnvVar{
									{Name: "GROVE_PGS_NAME", Value: "old-pgs-name"},
									{Name: "GROVE_PGS_INDEX", Value: "old-index"},
								},
							},
						},
					},
				},
			},
			expectedEnvVars: []string{
				constants.EnvVarPGSName,
				constants.EnvVarPGSIndex,
				constants.EnvVarPCLQName,
				constants.EnvVarHeadlessService,
			},
			shouldReplace: map[string]string{
				"GROVE_PGS_NAME":  "test-pgs",
				"GROVE_PGS_INDEX": "0",
			},
		},
		{
			// Tests that user-defined environment variables are preserved while Grove variables are added
			name: "Container with user env vars",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image",
								Env: []corev1.EnvVar{
									{Name: "USER_VAR", Value: "user-value"},
									{Name: "CUSTOM_CONFIG", Value: "custom-value"},
								},
							},
						},
					},
				},
			},
			expectedEnvVars: []string{
				constants.EnvVarPGSName,
				constants.EnvVarPGSIndex,
				constants.EnvVarPCLQName,
				constants.EnvVarHeadlessService,
			},
			shouldPreserve: []string{"USER_VAR", "CUSTOM_CONFIG"},
		},
		{
			// Tests that Grove variables are replaced and user variables are preserved in mixed scenarios
			name: "Container with mixed env vars",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image",
								Env: []corev1.EnvVar{
									{Name: "USER_VAR", Value: "user-value"},
									{Name: "GROVE_PGS_NAME", Value: "old-pgs-name"},
									{Name: "CUSTOM_CONFIG", Value: "custom-value"},
								},
							},
						},
					},
				},
			},
			expectedEnvVars: []string{
				constants.EnvVarPGSName,
				constants.EnvVarPGSIndex,
				constants.EnvVarPCLQName,
				constants.EnvVarHeadlessService,
			},
			shouldReplace: map[string]string{
				"GROVE_PGS_NAME": "test-pgs",
			},
			shouldPreserve: []string{"USER_VAR", "CUSTOM_CONFIG"},
		},
		{
			// Tests that PCSG PodCliques preserve user variables and don't add PCSG-specific variables
			name: "PCSG PodClique with existing env vars",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
					Labels: map[string]string{
						common.LabelPodCliqueScalingGroup: "test-pcsg",
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image",
								Env: []corev1.EnvVar{
									{Name: "GROVE_PCSG_NAME", Value: "old-pcsg-name"},
									{Name: "USER_VAR", Value: "user-value"},
								},
							},
						},
					},
				},
			},
			expectedEnvVars: []string{
				constants.EnvVarPGSName,
				constants.EnvVarPGSIndex,
				constants.EnvVarPCLQName,
				constants.EnvVarHeadlessService,
			},
			shouldReplace:  map[string]string{},
			shouldPreserve: []string{"USER_VAR"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				Spec: tt.pclq.Spec.PodSpec,
			}

			addEnvironmentVariables(pod, tt.pclq, "test-pgs", 0, 0)

			// Check that all containers have the expected environment variables
			for _, container := range pod.Spec.Containers {
				assertExpectedEnvVars(t, container, tt.expectedEnvVars)
				assertReplacedEnvVars(t, container, tt.shouldReplace)
				assertPreservedEnvVars(t, container, tt.shouldPreserve)
			}
		})
	}
}

// TestAddGroveEnvironmentVariables_EmptyContainers verifies that the environment variable
// injection function handles edge cases gracefully, such as Pods with no containers.
func TestAddGroveEnvironmentVariables_EmptyContainers(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{},
		},
	}
	pclq := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pclq",
			Namespace: "test-ns",
		},
	}

	// Should not panic with empty containers
	addEnvironmentVariables(pod, pclq, "test-pgs", 0, 0)
	assert.Empty(t, pod.Spec.Containers)
}

// TestAddGroveEnvironmentVariables_MultipleContainers verifies that Grove environment
// variables are correctly added to all containers in a Pod, including preserving existing
// environment variables in individual containers.
func TestAddGroveEnvironmentVariables_MultipleContainers(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "container1",
					Image: "image1",
				},
				{
					Name:  "container2",
					Image: "image2",
					Env: []corev1.EnvVar{
						{Name: "EXISTING_VAR", Value: "existing-value"},
					},
				},
			},
		},
	}
	pclq := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pclq",
			Namespace: "test-ns",
		},
	}

	addEnvironmentVariables(pod, pclq, "test-pgs", 0, 0)

	// Both containers should have Grove environment variables
	expectedEnvVars := []string{
		constants.EnvVarPGSName,
		constants.EnvVarPGSIndex,
		constants.EnvVarPCLQName,
		constants.EnvVarHeadlessService,
	}

	for _, container := range pod.Spec.Containers {
		assertExpectedEnvVars(t, container, expectedEnvVars)
		assertNoDuplicateEnvVars(t, container)
	}

	// Second container should preserve existing environment variable
	envVarNames := make(map[string]bool)
	for _, env := range pod.Spec.Containers[1].Env {
		envVarNames[env.Name] = true
	}
	assert.True(t, envVarNames["EXISTING_VAR"], "existing environment variable should be preserved")
}

// Helper functions
// -------------------------------------------------------------------------------------------

// assertExpectedEnvVars asserts that the expected environment variables are present.
func assertExpectedEnvVars(t *testing.T, container corev1.Container, expectedEnvVars []string) {
	envVarNames := make(map[string]bool)
	for _, env := range container.Env {
		envVarNames[env.Name] = true
	}
	for _, expectedEnv := range expectedEnvVars {
		assert.True(t, envVarNames[expectedEnv], "expected environment variable %s not found in container %s", expectedEnv, container.Name)
	}
}

// assertGroveEnvVarsDirectValues asserts Grove environment variables have direct values.
func assertGroveEnvVarsDirectValues(t *testing.T, container corev1.Container, groveEnvVars []string) {
	// Create a set of Grove env vars for quick lookup
	groveEnvVarSet := make(map[string]bool)
	for _, envVar := range groveEnvVars {
		groveEnvVarSet[envVar] = true
	}

	for _, env := range container.Env {
		// Only validate Grove environment variables
		if groveEnvVarSet[env.Name] {
			assert.NotEmpty(t, env.Value, "Grove environment variable %s should have a direct value", env.Name)
			assert.Nil(t, env.ValueFrom, "Grove environment variable %s should not use ValueFrom (Downward API)", env.Name)
		}
	}
}

// Helper function to assert replaced environment variables use correct Downward API
func assertReplacedEnvVars(t *testing.T, container corev1.Container, shouldReplace map[string]string) {
	if shouldReplace == nil {
		return
	}

	envVarNames := make(map[string]corev1.EnvVar)
	for _, env := range container.Env {
		envVarNames[env.Name] = env
	}

	for envName, expectedValue := range shouldReplace {
		envVar, found := envVarNames[envName]
		assert.True(t, found, "environment variable %s should exist", envName)
		if found {
			assert.Equal(t, expectedValue, envVar.Value,
				"environment variable %s has wrong value", envName)
		}
	}
}

// Helper function to assert preserved environment variables maintain their values
func assertPreservedEnvVars(t *testing.T, container corev1.Container, shouldPreserve []string) {
	if shouldPreserve == nil {
		return
	}

	envVarNames := make(map[string]corev1.EnvVar)
	for _, env := range container.Env {
		envVarNames[env.Name] = env
	}

	for _, preserveEnv := range shouldPreserve {
		envVar, found := envVarNames[preserveEnv]
		assert.True(t, found, "expected preserved environment variable %s not found in container %s", preserveEnv, container.Name)
		if found {
			assert.NotEmpty(t, envVar.Value, "preserved environment variable %s should have its original value", preserveEnv)
		}
	}
}

// Helper function to assert no duplicate environment variables
func assertNoDuplicateEnvVars(t *testing.T, container corev1.Container) {
	envVarCounts := make(map[string]int)
	for _, env := range container.Env {
		envVarCounts[env.Name]++
	}
	for envName, count := range envVarCounts {
		assert.Equal(t, 1, count, "environment variable %s appears %d times (should be 1)", envName, count)
	}
}

// TestSync verifies that the Sync method correctly handles basic validation
// and delegates to the sync flow preparation and execution.
func TestSync(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pclq is the PodClique resource to sync
		pclq *grovecorev1alpha1.PodClique
		// expectError indicates whether an error should be returned
		expectError bool
	}{
		{
			// Tests that Sync returns an error when PodClique is missing the required PodGang label
			name: "PodClique missing PodGang label",
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "inference-0-prefill",
					Namespace: "test-ns",
					Labels:    map[string]string{}, // Missing PodGang label
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "inference",
							UID:  "pgs-uid-123",
						},
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					Replicas: 1,
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image",
							},
						},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client
			fakeClient := testutils.SetupFakeClient()
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			operator := New(fakeClient, scheme, &record.FakeRecorder{}, expect.NewExpectationsStore())
			resource := operator.(*_resource)

			// Execute the method under test
			err := resource.Sync(context.Background(), testr.New(t), tt.pclq)

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				// Sync may return various errors including requeue which are expected
				// For this basic test, we just verify the method can be called
				// More detailed sync testing is done in syncflow_test.go
			}
		})
	}
}

// TestGetSelectorLabelsForPods verifies that the function generates correct label selectors
// for identifying pods belonging to a specific PodClique.
func TestGetSelectorLabelsForPods(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pclqObjectMeta contains metadata for the PodClique
		pclqObjectMeta metav1.ObjectMeta
		// expectedLabels are the labels expected to be returned
		expectedLabels map[string]string
	}{
		{
			// Tests that selector labels are generated correctly for a PodClique with owner reference
			name: "PodClique with owner reference",
			pclqObjectMeta: metav1.ObjectMeta{
				Name:      "test-pclq",
				Namespace: "test-ns",
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "test-pgs",
						UID:  "pgs-uid-456",
					},
				},
			},
			expectedLabels: map[string]string{
				common.LabelPartOfKey:    "test-pgs",
				common.LabelPodClique:    "test-pclq",
				common.LabelManagedByKey: common.LabelManagedByValue,
			},
		},
		{
			// Tests that PodCliques without owner references use empty part-of label
			name: "PodClique with no owner references",
			pclqObjectMeta: metav1.ObjectMeta{
				Name:            "orphan-pclq",
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{},
			},
			expectedLabels: map[string]string{
				common.LabelPartOfKey:    "",
				common.LabelPodClique:    "orphan-pclq",
				common.LabelManagedByKey: common.LabelManagedByValue,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := getSelectorLabelsForPods(tt.pclqObjectMeta)
			assert.Equal(t, tt.expectedLabels, labels)
		})
	}
}

// TestGetLabels verifies that the function constructs complete label sets for pods,
// combining default labels, user labels, and Grove-specific labels.
func TestGetLabels(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pclqObjectMeta contains metadata for the PodClique
		pclqObjectMeta metav1.ObjectMeta
		// pgsName is the name of the PodGangSet
		pgsName string
		// podGangName is the name of the PodGang
		podGangName string
		// pgsReplicaIndex is the replica index within the PodGangSet
		pgsReplicaIndex int
		// expectedLabelsContain are labels that must be present in the result
		expectedLabelsContain map[string]string
	}{
		{
			// Tests that all label sources (default, user, Grove-specific) are combined correctly
			name: "combine all label sources",
			pclqObjectMeta: metav1.ObjectMeta{
				Name:      "test-pclq",
				Namespace: "test-ns",
				Labels: map[string]string{
					"user-label": "user-value",
					"app":        "my-app",
				},
			},
			pgsName:         "test-pgs",
			podGangName:     "test-podgang",
			pgsReplicaIndex: 2,
			expectedLabelsContain: map[string]string{
				common.LabelPodClique:              "test-pclq",
				common.LabelPodGangSetReplicaIndex: "2",
				common.LabelPodGang:                "test-podgang",
				common.LabelPartOfKey:              "test-pgs",
				"user-label":                       "user-value",
				"app":                              "my-app",
			},
		},
		{
			// Tests that PodCliques with no user labels still get Grove-specific labels
			name: "PodClique with no user labels",
			pclqObjectMeta: metav1.ObjectMeta{
				Name:      "minimal-pclq",
				Namespace: "test-ns",
				Labels:    nil,
			},
			pgsName:         "minimal-pgs",
			podGangName:     "minimal-podgang",
			pgsReplicaIndex: 0,
			expectedLabelsContain: map[string]string{
				common.LabelPodClique:              "minimal-pclq",
				common.LabelPodGangSetReplicaIndex: "0",
				common.LabelPodGang:                "minimal-podgang",
				common.LabelPartOfKey:              "minimal-pgs",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := getLabels(tt.pclqObjectMeta, tt.pgsName, tt.podGangName, tt.pgsReplicaIndex)

			// Verify all expected labels are present
			for key, expectedValue := range tt.expectedLabelsContain {
				actualValue, exists := labels[key]
				assert.True(t, exists, "expected label %s to be present", key)
				assert.Equal(t, expectedValue, actualValue, "label %s has incorrect value", key)
			}
		})
	}
}

// TestConfigurePodHostname verifies that the function correctly sets pod hostname
// and subdomain for service discovery within a PodClique.
func TestConfigurePodHostname(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgsName is the name of the PodGangSet
		pgsName string
		// pgsReplicaIndex is the replica index within the PodGangSet
		pgsReplicaIndex int
		// pclqName is the name of the PodClique
		pclqName string
		// podIndex is the index of the pod within the PodClique
		podIndex int
		// expectedHostname is the expected hostname to be set
		expectedHostname string
		// expectedSubdomain is the expected subdomain to be set
		expectedSubdomain string
	}{
		{
			// Tests that hostname and subdomain are set correctly for service discovery
			name:              "correct hostname and subdomain",
			pgsName:           "test-pgs",
			pgsReplicaIndex:   1,
			pclqName:          "test-pclq",
			podIndex:          3,
			expectedHostname:  "test-pclq-3",
			expectedSubdomain: "test-pgs-1",
		},
		{
			// Tests that zero indices are handled correctly in hostname generation
			name:              "zero indices",
			pgsName:           "zero-pgs",
			pgsReplicaIndex:   0,
			pclqName:          "zero-pclq",
			podIndex:          0,
			expectedHostname:  "zero-pclq-0",
			expectedSubdomain: "zero-pgs-0",
		},
		{
			// Tests that large indices are handled correctly in hostname generation
			name:              "large indices",
			pgsName:           "large-pgs",
			pgsReplicaIndex:   99,
			pclqName:          "large-pclq",
			podIndex:          42,
			expectedHostname:  "large-pclq-42",
			expectedSubdomain: "large-pgs-99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{}
			configurePodHostname(tt.pgsName, tt.pgsReplicaIndex, tt.pclqName, pod, tt.podIndex)

			assert.Equal(t, tt.expectedHostname, pod.Spec.Hostname)
			assert.Equal(t, tt.expectedSubdomain, pod.Spec.Subdomain)
		})
	}
}

// TestBuildResource verifies that the buildResource method correctly constructs
// a Pod resource from a PodClique template with proper configuration.
func TestBuildResource(t *testing.T) {
	tests := []struct {
		// name describes the test scenario being validated
		name string
		// pgs is the PodGangSet that owns the PodClique
		pgs *grovecorev1alpha1.PodGangSet
		// pclq is the PodClique template to build from
		pclq *grovecorev1alpha1.PodClique
		// podGangName is the name of the PodGang
		podGangName string
		// podIndex is the index of the pod within the PodClique
		podIndex int
		// expectError indicates whether an error should be returned
		expectError bool
		// validatePod is a function to validate the built pod
		validatePod func(t *testing.T, pod *corev1.Pod)
	}{
		{
			// Tests that a Pod is built correctly from a PodClique template with all metadata and spec
			name: "build pod with correct metadata and spec",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "inference",
					Namespace: "test-ns",
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "inference-0-prefill",
					Namespace: "test-ns",
					Labels: map[string]string{
						"user-label":             "user-value",
						common.LabelPartOfKey:    "inference",
						common.LabelManagedByKey: common.LabelManagedByValue,
					},
					Annotations: map[string]string{
						"user-annotation": "user-annotation-value",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "inference",
							UID:  "pgs-uid-123",
						},
					},
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image:latest",
							},
						},
					},
				},
			},
			podGangName: "test-podgang",
			podIndex:    2,
			expectError: false,
			validatePod: func(t *testing.T, pod *corev1.Pod) {
				// Verify metadata
				assert.Equal(t, "inference-0-prefill-", pod.GenerateName)
				assert.Equal(t, "test-ns", pod.Namespace)
				assert.Equal(t, "user-annotation-value", pod.Annotations["user-annotation"])

				// Verify labels contain Grove-specific labels
				assert.Equal(t, "inference-0-prefill", pod.Labels[common.LabelPodClique])
				assert.Equal(t, "0", pod.Labels[common.LabelPodGangSetReplicaIndex])
				assert.Equal(t, "test-podgang", pod.Labels[common.LabelPodGang])
				assert.Equal(t, "user-value", pod.Labels["user-label"])

				// Verify PodSpec was copied
				assert.Len(t, pod.Spec.Containers, 1)
				assert.Equal(t, "test-container", pod.Spec.Containers[0].Name)
				assert.Equal(t, "test-image:latest", pod.Spec.Containers[0].Image)

				// Verify scheduling gate was added
				assert.Len(t, pod.Spec.SchedulingGates, 1)
				assert.Equal(t, podGangSchedulingGate, pod.Spec.SchedulingGates[0].Name)

				// Verify hostname and subdomain
				assert.Equal(t, "inference-0-prefill-2", pod.Spec.Hostname)
				assert.Equal(t, "inference-0", pod.Spec.Subdomain)

				// Verify Grove environment variables were added
				container := pod.Spec.Containers[0]
				envVarMap := make(map[string]string)
				for _, env := range container.Env {
					envVarMap[env.Name] = env.Value
				}
				assert.Equal(t, "inference", envVarMap[constants.EnvVarPGSName])
				assert.Equal(t, "0", envVarMap[constants.EnvVarPGSIndex])
				assert.Equal(t, "inference-0-prefill", envVarMap[constants.EnvVarPCLQName])
				assert.Equal(t, "2", envVarMap[constants.EnvVarPodIndex])
			},
		},
		{
			// Tests that buildResource returns an error when PodClique name format is invalid
			name: "PodClique with invalid name format",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "test-ns",
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-name-format",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image:latest",
							},
						},
					},
				},
			},
			podGangName: "test-podgang",
			podIndex:    0,
			expectError: true,
			validatePod: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			fakeClient := testutils.SetupFakeClient()
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			operator := New(fakeClient, scheme, &record.FakeRecorder{}, expect.NewExpectationsStore())
			resource := operator.(*_resource)

			pod := &corev1.Pod{}

			// Execute
			err := resource.buildResource(tt.pgs, tt.pclq, tt.podGangName, pod, tt.podIndex)

			// Verify
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.validatePod != nil {
					tt.validatePod(t, pod)
				}
			}
		})
	}
}
