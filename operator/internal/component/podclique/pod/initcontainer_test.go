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
	"fmt"
	"os"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/common"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	"github.com/NVIDIA/grove/operator/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// TestConfigurePodInitContainer tests the main function that configures a pod with init container setup.
// It verifies that all required volumes and the init container are properly added to the pod.
func TestConfigurePodInitContainer(t *testing.T) {
	tests := []struct {
		name                   string                                         // Test case name
		pgs                    *grovecorev1alpha1.PodGangSet                  // Input PodGangSet
		pclq                   *grovecorev1alpha1.PodClique                   // Input PodClique
		pod                    *corev1.Pod                                    // Input Pod to configure
		envVarValue            string                                         // Value to set for GROVE_INIT_CONTAINER_IMAGE env var
		setEnvVar              bool                                           // Whether to set the environment variable
		expectedError          bool                                           // Whether an error is expected
		expectedVolumes        int                                            // Expected number of volumes after configuration
		expectedInitContainers int                                            // Expected number of init containers after configuration
		validateVolumes        func(t *testing.T, volumes []corev1.Volume)    // Custom volume validation function
		validateInitContainer  func(t *testing.T, container corev1.Container) // Custom init container validation function
	}{
		{
			name: "successful configuration with dependencies",
			// Test: Configure a pod with init container when PodClique has dependencies
			// Expect: Pod should get SA token volume, pod info volume, and init container with dependency args
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "parent-clique",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{"test-pgs-0-parent-clique"},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "test-image",
						},
					},
				},
			},
			envVarValue:            "registry.example.com/grove/initc",
			setEnvVar:              true,
			expectedError:          false,
			expectedVolumes:        2, // SA token secret + pod info volumes
			expectedInitContainers: 1,
			validateVolumes: func(t *testing.T, volumes []corev1.Volume) {
				// Verify service account token secret volume
				saTokenVol := findVolumeByName(volumes, serviceAccountTokenSecretVolumeName)
				require.NotNil(t, saTokenVol, "Service account token volume should be present")
				assert.NotNil(t, saTokenVol.Secret, "Volume should have secret source")
				assert.Equal(t, "test-pgs-initc-sa-token-secret", saTokenVol.Secret.SecretName)
				assert.Equal(t, ptr.To[int32](420), saTokenVol.Secret.DefaultMode)

				// Verify pod info volume
				podInfoVol := findVolumeByName(volumes, podInfoVolumeName)
				require.NotNil(t, podInfoVol, "Pod info volume should be present")
				assert.NotNil(t, podInfoVol.DownwardAPI, "Volume should have downward API source")
				assert.Len(t, podInfoVol.DownwardAPI.Items, 2, "Should have 2 downward API items")
			},
			validateInitContainer: func(t *testing.T, container corev1.Container) {
				assert.Equal(t, initContainerName, container.Name)
				assert.Equal(t, "registry.example.com/grove/initc:v0.0.0-master+$Format:%H$", container.Image)
				assert.Contains(t, container.Args, "--podcliques=test-pgs-0-parent-clique:2")
				assert.Len(t, container.VolumeMounts, 2, "Should have 2 volume mounts")
			},
		},
		{
			name: "configuration without dependencies",
			// Test: Configure a pod with init container when PodClique has no dependencies
			// Expect: Pod should get volumes and init container but with empty args
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
				},
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{}, // No dependencies
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "test-image",
						},
					},
				},
			},
			envVarValue:            "registry.example.com/grove/initc",
			setEnvVar:              true,
			expectedError:          false,
			expectedVolumes:        2,
			expectedInitContainers: 1,
			validateInitContainer: func(t *testing.T, container corev1.Container) {
				assert.Equal(t, initContainerName, container.Name)
				assert.Empty(t, container.Args, "Should have no args when no dependencies")
			},
		},
		{
			name: "error when environment variable not set",
			// Test: Try to configure pod when GROVE_INIT_CONTAINER_IMAGE env var is missing
			// Expect: Should add volumes but fail to add init container, returning Grove error
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "test-ns",
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pclq",
					Namespace: "test-ns",
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
			},
			setEnvVar:              false,
			expectedError:          true,
			expectedVolumes:        2, // Volumes are added before init container fails
			expectedInitContainers: 0, // Init container should not be added due to error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variable
			if tt.setEnvVar {
				t.Setenv(envVarInitContainerImage, tt.envVarValue)
			} else {
				os.Unsetenv(envVarInitContainerImage)
			}

			// Execute the function under test
			err := configurePodInitContainer(tt.pgs, tt.pclq, tt.pod)

			// Validate error expectation
			if tt.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Validate volumes
			assert.Len(t, tt.pod.Spec.Volumes, tt.expectedVolumes, "Unexpected number of volumes")
			if tt.validateVolumes != nil {
				tt.validateVolumes(t, tt.pod.Spec.Volumes)
			}

			// Validate init containers
			assert.Len(t, tt.pod.Spec.InitContainers, tt.expectedInitContainers, "Unexpected number of init containers")
			if tt.expectedInitContainers > 0 && tt.validateInitContainer != nil {
				tt.validateInitContainer(t, tt.pod.Spec.InitContainers[0])
			}
		})
	}
}

// TestAddServiceAccountTokenSecretVolume tests the function that adds service account token secret volume.
// It verifies that the volume is correctly configured with the expected secret name and permissions.
func TestAddServiceAccountTokenSecretVolume(t *testing.T) {
	tests := []struct {
		name               string          // Test case name
		pgsName            string          // PodGangSet name to use for secret name generation
		initialVolumes     []corev1.Volume // Initial volumes in the pod
		expectedSecretName string          // Expected secret name in the volume
		expectedMode       *int32          // Expected default mode for the secret
	}{
		{
			name: "add service account token volume to empty pod",
			// Test: Add SA token volume to a pod that has no existing volumes
			// Expect: Pod should have exactly one volume with correct secret name and permissions
			pgsName:            "test-pgs",
			initialVolumes:     []corev1.Volume{},
			expectedSecretName: "test-pgs-initc-sa-token-secret",
			expectedMode:       ptr.To[int32](420),
		},
		{
			name: "add service account token volume to pod with existing volumes",
			// Test: Add SA token volume to a pod that already has other volumes
			// Expect: Pod should have original volume plus new SA token volume
			pgsName: "another-pgs",
			initialVolumes: []corev1.Volume{
				{
					Name: "existing-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			expectedSecretName: "another-pgs-initc-sa-token-secret",
			expectedMode:       ptr.To[int32](420),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup pod with initial volumes
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Volumes: tt.initialVolumes,
				},
			}
			initialVolumeCount := len(pod.Spec.Volumes)

			// Execute the function under test
			addServiceAccountTokenSecretVolume(tt.pgsName, pod)

			// Validate that exactly one volume was added
			assert.Len(t, pod.Spec.Volumes, initialVolumeCount+1, "Should add exactly one volume")

			// Find and validate the service account token volume
			saTokenVol := findVolumeByName(pod.Spec.Volumes, serviceAccountTokenSecretVolumeName)
			require.NotNil(t, saTokenVol, "Service account token volume should be present")

			assert.Equal(t, serviceAccountTokenSecretVolumeName, saTokenVol.Name)
			require.NotNil(t, saTokenVol.Secret, "Volume should have secret source")
			assert.Equal(t, tt.expectedSecretName, saTokenVol.Secret.SecretName)
			assert.Equal(t, tt.expectedMode, saTokenVol.Secret.DefaultMode)
		})
	}
}

// TestAddPodInfoVolume tests the function that adds downward API volume for pod metadata.
// It verifies that the volume exposes the correct pod metadata fields.
func TestAddPodInfoVolume(t *testing.T) {
	tests := []struct {
		name           string          // Test case name
		initialVolumes []corev1.Volume // Initial volumes in the pod
		expectedItems  int             // Expected number of downward API items
	}{
		{
			name: "add pod info volume to empty pod",
			// Test: Add downward API volume to a pod with no existing volumes
			// Expect: Pod should have one volume with namespace and gang name fields
			initialVolumes: []corev1.Volume{},
			expectedItems:  2, // namespace and gang name
		},
		{
			name: "add pod info volume to pod with existing volumes",
			// Test: Add downward API volume to a pod that already has other volumes
			// Expect: Pod should have original volume plus new downward API volume
			initialVolumes: []corev1.Volume{
				{
					Name: "existing-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			expectedItems: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup pod with initial volumes
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Volumes: tt.initialVolumes,
				},
			}
			initialVolumeCount := len(pod.Spec.Volumes)

			// Execute the function under test
			addPodInfoVolume(pod)

			// Validate that exactly one volume was added
			assert.Len(t, pod.Spec.Volumes, initialVolumeCount+1, "Should add exactly one volume")

			// Find and validate the pod info volume
			podInfoVol := findVolumeByName(pod.Spec.Volumes, podInfoVolumeName)
			require.NotNil(t, podInfoVol, "Pod info volume should be present")

			assert.Equal(t, podInfoVolumeName, podInfoVol.Name)
			require.NotNil(t, podInfoVol.DownwardAPI, "Volume should have downward API source")
			assert.Len(t, podInfoVol.DownwardAPI.Items, tt.expectedItems, "Should have expected number of downward API items")

			// Validate downward API items
			items := podInfoVol.DownwardAPI.Items

			// Find namespace item
			namespaceItem := findDownwardAPIItemByPath(items, common.PodNamespaceFileName)
			require.NotNil(t, namespaceItem, "Namespace item should be present")
			assert.Equal(t, "metadata.namespace", namespaceItem.FieldRef.FieldPath)

			// Find gang name item
			gangNameItem := findDownwardAPIItemByPath(items, common.PodGangNameFileName)
			require.NotNil(t, gangNameItem, "Gang name item should be present")
			expectedFieldPath := fmt.Sprintf("metadata.labels['%s']", grovecorev1alpha1.LabelPodGang)
			assert.Equal(t, expectedFieldPath, gangNameItem.FieldRef.FieldPath)
		})
	}
}

// TestAddInitContainer tests the function that adds the Grove init container to the pod.
// It verifies container configuration, volume mounts, and argument generation.
func TestAddInitContainer(t *testing.T) {
	tests := []struct {
		name                 string                                         // Test case name
		pgs                  *grovecorev1alpha1.PodGangSet                  // Input PodGangSet
		pclq                 *grovecorev1alpha1.PodClique                   // Input PodClique
		pod                  *corev1.Pod                                    // Input Pod
		envVarValue          string                                         // Value for GROVE_INIT_CONTAINER_IMAGE env var
		setEnvVar            bool                                           // Whether to set the environment variable
		expectedError        bool                                           // Whether an error is expected
		expectedArgs         []string                                       // Expected container arguments
		expectedVolumeMounts int                                            // Expected number of volume mounts
		validateContainer    func(t *testing.T, container corev1.Container) // Custom container validation function
	}{
		{
			name: "successful init container addition with dependencies",
			// Test: Add init container when PodClique has dependency on another clique
			// Expect: Container added with correct image, args, and volume mounts
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "parent-clique",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](3),
								},
							},
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{"test-pgs-0-parent-clique"},
				},
			},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{},
				},
			},
			envVarValue:          "registry.example.com/grove/initc",
			setEnvVar:            true,
			expectedError:        false,
			expectedArgs:         []string{"--podcliques=test-pgs-0-parent-clique:3"},
			expectedVolumeMounts: 2,
			validateContainer: func(t *testing.T, container corev1.Container) {
				assert.Equal(t, initContainerName, container.Name)
				assert.Equal(t, fmt.Sprintf("registry.example.com/grove/initc:%s", version.Get().GitVersion), container.Image)

				// Validate volume mounts
				podInfoMount := findVolumeMountByName(container.VolumeMounts, podInfoVolumeName)
				require.NotNil(t, podInfoMount, "Pod info volume mount should be present")
				assert.Equal(t, common.VolumeMountPathPodInfo, podInfoMount.MountPath)
				assert.True(t, podInfoMount.ReadOnly)

				saTokenMount := findVolumeMountByName(container.VolumeMounts, serviceAccountTokenSecretVolumeName)
				require.NotNil(t, saTokenMount, "SA token volume mount should be present")
				assert.Equal(t, volumeMountPathServiceAccount, saTokenMount.MountPath)
				assert.True(t, saTokenMount.ReadOnly)
			},
		},
		{
			name: "init container without dependencies",
			// Test: Add init container when PodClique has no dependencies
			// Expect: Container added with empty args but proper volume mounts
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{},
				},
			},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{},
				},
			},
			envVarValue:          "registry.example.com/grove/initc",
			setEnvVar:            true,
			expectedError:        false,
			expectedArgs:         []string{},
			expectedVolumeMounts: 2,
		},
		{
			name: "error when environment variable not set",
			// Test: Try to add init container when GROVE_INIT_CONTAINER_IMAGE env var is missing
			// Expect: Should return Grove error and not add any init container
			pgs:  &grovecorev1alpha1.PodGangSet{},
			pclq: &grovecorev1alpha1.PodClique{},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{},
				},
			},
			setEnvVar:     false,
			expectedError: true,
		},
		{
			name: "error when parent clique template not found",
			// Test: Try to add init container when dependency references non-existent clique
			// Expect: Should return Grove error about missing template
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pgs",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "different-clique",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](1),
								},
							},
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{"test-pgs-0-missing-clique"},
				},
			},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{},
				},
			},
			envVarValue:   "registry.example.com/grove/initc",
			setEnvVar:     true,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variable
			if tt.setEnvVar {
				t.Setenv(envVarInitContainerImage, tt.envVarValue)
			} else {
				os.Unsetenv(envVarInitContainerImage)
			}

			initialInitContainerCount := len(tt.pod.Spec.InitContainers)

			// Execute the function under test
			err := addInitContainer(tt.pgs, tt.pclq, tt.pod)

			// Validate error expectation
			if tt.expectedError {
				assert.Error(t, err)
				assert.Len(t, tt.pod.Spec.InitContainers, initialInitContainerCount, "Should not add init container on error")
				return
			}
			assert.NoError(t, err)

			// Validate that exactly one init container was added
			assert.Len(t, tt.pod.Spec.InitContainers, initialInitContainerCount+1, "Should add exactly one init container")

			// Validate the added init container
			initContainer := tt.pod.Spec.InitContainers[initialInitContainerCount]
			assert.Equal(t, tt.expectedArgs, initContainer.Args)
			assert.Len(t, initContainer.VolumeMounts, tt.expectedVolumeMounts)

			if tt.validateContainer != nil {
				tt.validateContainer(t, initContainer)
			}
		})
	}
}

// TestGetInitContainerImage tests the function that retrieves the init container image from environment.
// It verifies both successful retrieval and error handling when the environment variable is missing.
func TestGetInitContainerImage(t *testing.T) {
	tests := []struct {
		name          string                        // Test case name
		envVarValue   string                        // Value to set for the environment variable
		setEnvVar     bool                          // Whether to set the environment variable
		expectedImage string                        // Expected returned image value
		expectedError bool                          // Whether an error is expected
		validateError func(t *testing.T, err error) // Custom error validation function
	}{
		{
			name: "successful image retrieval",
			// Test: Retrieve init container image when environment variable is set
			// Expect: Should return the exact image value from environment
			envVarValue:   "registry.example.com/grove/initc",
			setEnvVar:     true,
			expectedImage: "registry.example.com/grove/initc",
			expectedError: false,
		},
		{
			name: "successful image retrieval with different registry",
			// Test: Retrieve init container image from different registry
			// Expect: Should return the image value regardless of registry format
			envVarValue:   "gcr.io/my-project/grove-init",
			setEnvVar:     true,
			expectedImage: "gcr.io/my-project/grove-init",
			expectedError: false,
		},
		{
			name: "error when environment variable not set",
			// Test: Try to retrieve image when GROVE_INIT_CONTAINER_IMAGE is not set
			// Expect: Should return Grove error with specific error code and message
			setEnvVar:     false,
			expectedError: true,
			validateError: func(t *testing.T, err error) {
				var groveErr *groveerr.GroveError
				require.ErrorAs(t, err, &groveErr, "Error should be a Grove error")
				assert.Equal(t, errCodeInitContainerImageEnvVarMissing, groveErr.Code)
				assert.Equal(t, component.OperationSync, groveErr.Operation)
				assert.Contains(t, groveErr.Message, envVarInitContainerImage)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variable
			if tt.setEnvVar {
				t.Setenv(envVarInitContainerImage, tt.envVarValue)
			} else {
				os.Unsetenv(envVarInitContainerImage)
			}

			// Execute the function under test
			image, err := getInitContainerImage()

			// Validate error expectation
			if tt.expectedError {
				assert.Error(t, err)
				assert.Empty(t, image, "Image should be empty on error")
				if tt.validateError != nil {
					tt.validateError(t, err)
				}
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedImage, image)
		})
	}
}

// TestGenerateArgsForInitContainer tests the function that generates command-line arguments for the init container.
// It verifies argument generation for various dependency scenarios and error handling.
func TestGenerateArgsForInitContainer(t *testing.T) {
	tests := []struct {
		name          string                        // Test case name
		pgs           *grovecorev1alpha1.PodGangSet // Input PodGangSet with template cliques
		pclq          *grovecorev1alpha1.PodClique  // Input PodClique with dependencies
		expectedArgs  []string                      // Expected generated arguments
		expectedError bool                          // Whether an error is expected
		validateError func(t *testing.T, err error) // Custom error validation function
	}{
		{
			name: "no dependencies",
			// Test: Generate args when PodClique has no StartsAfter dependencies
			// Expect: Should return empty args slice with no error
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{},
				},
			},
			expectedArgs:  []string{},
			expectedError: false,
		},
		{
			name: "single dependency",
			// Test: Generate args when PodClique depends on one parent clique
			// Expect: Should return single --podcliques arg with clique name and MinAvailable count
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "parent-clique",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{"test-pgs-0-parent-clique"},
				},
			},
			expectedArgs:  []string{"--podcliques=test-pgs-0-parent-clique:2"},
			expectedError: false,
		},
		{
			name: "multiple dependencies",
			// Test: Generate args when PodClique depends on multiple parent cliques
			// Expect: Should return multiple --podcliques args, one for each dependency
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "clique-a",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](1),
								},
							},
							{
								Name: "clique-b",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](3),
								},
							},
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{"test-pgs-0-clique-a", "test-pgs-0-clique-b"},
				},
			},
			expectedArgs: []string{
				"--podcliques=test-pgs-0-clique-a:1",
				"--podcliques=test-pgs-0-clique-b:3",
			},
			expectedError: false,
		},
		{
			name: "dependency with high min available count",
			// Test: Generate args when parent clique has high MinAvailable value
			// Expect: Should correctly include the high count in the argument
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "large-clique",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](10),
								},
							},
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{"test-pgs-0-large-clique"},
				},
			},
			expectedArgs:  []string{"--podcliques=test-pgs-0-large-clique:10"},
			expectedError: false,
		},
		{
			name: "error when parent clique template not found",
			// Test: Try to generate args when dependency references non-existent clique template
			// Expect: Should return Grove error about missing PodClique template
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "existing-clique",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](1),
								},
							},
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{"test-pgs-0-missing-clique"},
				},
			},
			expectedError: true,
			validateError: func(t *testing.T, err error) {
				var groveErr *groveerr.GroveError
				require.ErrorAs(t, err, &groveErr, "Error should be a Grove error")
				assert.Equal(t, errCodeMissingPodCliqueTemplate, groveErr.Code)
				assert.Equal(t, component.OperationSync, groveErr.Operation)
				assert.Contains(t, groveErr.Message, "test-pgs-0-missing-clique")
			},
		},
		{
			name: "error when multiple parent cliques not found",
			// Test: Try to generate args when one of multiple dependencies is missing
			// Expect: Should return Grove error for the first missing template encountered
			pgs: &grovecorev1alpha1.PodGangSet{
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
							{
								Name: "existing-clique",
								Spec: grovecorev1alpha1.PodCliqueSpec{
									MinAvailable: ptr.To[int32](1),
								},
							},
						},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					StartsAfter: []string{"test-pgs-0-existing-clique", "test-pgs-0-missing-clique"},
				},
			},
			expectedError: true,
			validateError: func(t *testing.T, err error) {
				var groveErr *groveerr.GroveError
				require.ErrorAs(t, err, &groveErr, "Error should be a Grove error")
				assert.Equal(t, errCodeMissingPodCliqueTemplate, groveErr.Code)
				assert.Contains(t, groveErr.Message, "test-pgs-0-missing-clique")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the function under test
			args, err := generateArgsForInitContainer(tt.pgs, tt.pclq)

			// Validate error expectation
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, args, "Args should be nil on error")
				if tt.validateError != nil {
					tt.validateError(t, err)
				}
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

// Helper functions for test validation

// findVolumeByName finds a volume in the slice by name.
func findVolumeByName(volumes []corev1.Volume, name string) *corev1.Volume {
	for i := range volumes {
		if volumes[i].Name == name {
			return &volumes[i]
		}
	}
	return nil
}

// findVolumeMountByName finds a volume mount in the slice by name.
func findVolumeMountByName(mounts []corev1.VolumeMount, name string) *corev1.VolumeMount {
	for i := range mounts {
		if mounts[i].Name == name {
			return &mounts[i]
		}
	}
	return nil
}

// findDownwardAPIItemByPath finds a downward API item in the slice by path.
func findDownwardAPIItemByPath(items []corev1.DownwardAPIVolumeFile, path string) *corev1.DownwardAPIVolumeFile {
	for i := range items {
		if items[i].Path == path {
			return &items[i]
		}
	}
	return nil
}
