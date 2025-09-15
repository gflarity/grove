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
	"fmt"
	"os"
	"strings"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/common"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	"github.com/NVIDIA/grove/operator/internal/version"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const (
	// envVarInitContainerImage is the environment variable name for the init container image.
	// The value should contain only the registry and repository, excluding the tag.
	envVarInitContainerImage string = "GROVE_INIT_CONTAINER_IMAGE"

	// initContainerName is the standard name assigned to Grove init containers.
	initContainerName = "grove-initc"

	// serviceAccountTokenSecretVolumeName is the volume name for mounting service account token secrets.
	serviceAccountTokenSecretVolumeName = "sa-token-secret-vol"

	// podInfoVolumeName is the volume name for the downward API that provides pod metadata.
	podInfoVolumeName = "pod-info-vol"

	// volumeMountPathServiceAccount is the standard Kubernetes path for service account credentials.
	volumeMountPathServiceAccount = "/var/run/secrets/kubernetes.io/serviceaccount"
)

// configurePodInitContainer configures a pod with the necessary init container setup.
// It adds required volumes and the Grove init container to coordinate pod startup dependencies.
func configurePodInitContainer(pgs *grovecorev1alpha1.PodGangSet, pclq *grovecorev1alpha1.PodClique, pod *corev1.Pod) error {
	addServiceAccountTokenSecretVolume(pgs.Name, pod)
	addPodInfoVolume(pod)
	return addInitContainer(pgs, pclq, pod)
}

// addServiceAccountTokenSecretVolume adds a volume for the service account token secret.
// The volume provides authentication credentials for the init container to access the Kubernetes API.
func addServiceAccountTokenSecretVolume(pgsName string, pod *corev1.Pod) {
	// Create volume spec for service account token secret
	saTokenSecretVol := corev1.Volume{
		Name: serviceAccountTokenSecretVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  grovecorev1alpha1.GenerateInitContainerSATokenSecretName(pgsName),
				DefaultMode: ptr.To[int32](420),
			},
		},
	}
	// Append the volume to the pod's volume list
	pod.Spec.Volumes = append(pod.Spec.Volumes, saTokenSecretVol)
}

// addPodInfoVolume adds a downward API volume that exposes pod metadata to the init container.
// The volume provides the pod's namespace and gang name for coordination logic.
func addPodInfoVolume(pod *corev1.Pod) {
	// Create downward API volume to expose pod metadata
	podInfoVol := corev1.Volume{
		Name: podInfoVolumeName,
		VolumeSource: corev1.VolumeSource{
			DownwardAPI: &corev1.DownwardAPIVolumeSource{
				// Expose pod namespace and gang name as files
				Items: []corev1.DownwardAPIVolumeFile{
					{
						Path: common.PodNamespaceFileName,
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
					{
						Path: common.PodGangNameFileName,
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: fmt.Sprintf("metadata.labels['%s']", grovecorev1alpha1.LabelPodGang),
						},
					},
				},
			},
		},
	}
	// Append the downward API volume to the pod's volume list
	pod.Spec.Volumes = append(pod.Spec.Volumes, podInfoVol)
}

// addInitContainer adds the Grove init container to the pod's init container list.
// The init container coordinates pod startup dependencies by waiting for prerequisite
// PodCliques to reach their minimum availability before allowing the pod to start.
func addInitContainer(pgs *grovecorev1alpha1.PodGangSet, pclq *grovecorev1alpha1.PodClique, pod *corev1.Pod) error {
	// Get the init container image from environment
	image, err := getInitContainerImage()
	if err != nil {
		return err
	}
	// Generate command arguments for dependency coordination
	args, err := generateArgsForInitContainer(pgs, pclq)
	if err != nil {
		return err
	}

	// Add the Grove init container with required volumes mounted
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:  initContainerName,
		Image: fmt.Sprintf("%s:%s", image, version.Get().GitVersion),
		Args:  args,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      podInfoVolumeName,
				ReadOnly:  true,
				MountPath: common.VolumeMountPathPodInfo,
			},
			{
				Name:      serviceAccountTokenSecretVolumeName,
				ReadOnly:  true,
				MountPath: volumeMountPathServiceAccount,
			},
		},
	})
	return nil
}

// getInitContainerImage retrieves the init container image from the environment variable.
// Returns an error if the GROVE_INIT_CONTAINER_IMAGE environment variable is not set.
func getInitContainerImage() (string, error) {
	// Look up the init container image from environment variable
	initContainerImage, ok := os.LookupEnv(envVarInitContainerImage)
	// Return error if environment variable is not set
	if !ok {
		return "", groveerr.New(
			errCodeInitContainerImageEnvVarMissing,
			component.OperationSync,
			fmt.Sprintf("environment variable %s specifying the init-container image is missing", envVarInitContainerImage),
		)
	}
	return initContainerImage, nil
}

// generateArgsForInitContainer builds command-line arguments for the init container.
// It creates --podcliques arguments for each PodClique dependency specified in the
// StartsAfter field, including the required minimum availability count.
func generateArgsForInitContainer(pgs *grovecorev1alpha1.PodGangSet, pclq *grovecorev1alpha1.PodClique) ([]string, error) {
	// Initialize arguments slice for init container
	args := make([]string, 0)
	// Process each PodClique dependency in StartsAfter
	for _, parentCliqueFQN := range pclq.Spec.StartsAfter {
		// Find the template spec for the parent clique
		parentCliqueTemplateSpec, ok := lo.Find(pgs.Spec.Template.Cliques, func(templateSpec *grovecorev1alpha1.PodCliqueTemplateSpec) bool {
			return strings.HasSuffix(parentCliqueFQN, templateSpec.Name)
		})
		// Return error if parent clique template is not found
		if !ok {
			return nil, groveerr.New(
				errCodeMissingPodCliqueTemplate,
				component.OperationSync,
				fmt.Sprintf("PodClique %s specified in startsAfter is not present in the templates", parentCliqueFQN),
			)
		}
		// Add argument with clique name and minimum availability count
		args = append(args, fmt.Sprintf("--podcliques=%s:%d", parentCliqueFQN, *parentCliqueTemplateSpec.Spec.MinAvailable))
	}
	return args, nil
}
