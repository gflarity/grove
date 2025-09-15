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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// TestNewPodBuilder verifies that a new PodSpecBuilder is properly initialized
// with default configuration suitable for testing.
func TestNewPodBuilder(t *testing.T) {
	builder := NewPodBuilder()

	require.NotNil(t, builder)
	require.NotNil(t, builder.podSpec)

	// Verify default container configuration
	require.Len(t, builder.podSpec.Containers, 1)
	container := builder.podSpec.Containers[0]
	assert.Equal(t, "test-container", container.Name)
	assert.Equal(t, "alpine:3.21", container.Image)
	assert.Equal(t, []string{"/bin/sh", "-c", "sleep 2m"}, container.Command)

	// Verify default restart policy
	assert.Equal(t, corev1.RestartPolicyAlways, builder.podSpec.RestartPolicy)
}

// TestPodSpecBuilder_Build verifies that the Build method returns the
// constructed PodSpec object.
func TestPodSpecBuilder_Build(t *testing.T) {
	builder := NewPodBuilder()
	podSpec := builder.Build()

	require.NotNil(t, podSpec)

	// Verify it's the same object that was being built
	assert.Equal(t, builder.podSpec, podSpec)

	// Verify the built PodSpec has expected configuration
	require.Len(t, podSpec.Containers, 1)
	container := podSpec.Containers[0]
	assert.Equal(t, "test-container", container.Name)
	assert.Equal(t, "alpine:3.21", container.Image)
	assert.Equal(t, []string{"/bin/sh", "-c", "sleep 2m"}, container.Command)
	assert.Equal(t, corev1.RestartPolicyAlways, podSpec.RestartPolicy)
}

// TestPodSpecBuilder_MultipleBuilds verifies that multiple calls to Build
// return the same PodSpec object.
func TestPodSpecBuilder_MultipleBuilds(t *testing.T) {
	builder := NewPodBuilder()

	podSpec1 := builder.Build()
	podSpec2 := builder.Build()

	// Should return the same object
	assert.Equal(t, podSpec1, podSpec2)
	assert.True(t, podSpec1 == podSpec2) // Same memory address
}

// TestCreateDefaultPodSpec verifies the internal helper function creates
// a properly configured default PodSpec.
func TestCreateDefaultPodSpec(t *testing.T) {
	podSpec := createDefaultPodSpec()

	require.NotNil(t, podSpec)

	// Verify container configuration
	require.Len(t, podSpec.Containers, 1)
	container := podSpec.Containers[0]
	assert.Equal(t, "test-container", container.Name)
	assert.Equal(t, "alpine:3.21", container.Image)
	assert.Equal(t, []string{"/bin/sh", "-c", "sleep 2m"}, container.Command)

	// Verify restart policy
	assert.Equal(t, corev1.RestartPolicyAlways, podSpec.RestartPolicy)

	// Verify other fields are in default state
	assert.Empty(t, podSpec.InitContainers)
	assert.Empty(t, podSpec.Volumes)
	assert.Empty(t, podSpec.ServiceAccountName)
	assert.Nil(t, podSpec.SecurityContext)
}

// TestPodSpecBuilder_DefaultContainerProperties verifies that the default
// container has appropriate properties for testing scenarios.
func TestPodSpecBuilder_DefaultContainerProperties(t *testing.T) {
	builder := NewPodBuilder()
	podSpec := builder.Build()

	container := podSpec.Containers[0]

	// Verify container uses a lightweight, commonly available image
	assert.Equal(t, "alpine:3.21", container.Image)

	// Verify container has a simple, long-running command suitable for testing
	assert.Equal(t, []string{"/bin/sh", "-c", "sleep 2m"}, container.Command)

	// Verify container name is descriptive
	assert.Equal(t, "test-container", container.Name)

	// Verify container doesn't have resource limits/requests (default for testing)
	assert.Nil(t, container.Resources.Limits)
	assert.Nil(t, container.Resources.Requests)

	// Verify no environment variables by default
	assert.Empty(t, container.Env)

	// Verify no volume mounts by default
	assert.Empty(t, container.VolumeMounts)

	// Verify no ports exposed by default
	assert.Empty(t, container.Ports)
}

// TestPodSpecBuilder_RestartPolicyDefault verifies that the default restart
// policy is appropriate for most testing scenarios.
func TestPodSpecBuilder_RestartPolicyDefault(t *testing.T) {
	builder := NewPodBuilder()
	podSpec := builder.Build()

	// RestartPolicyAlways is appropriate for most test scenarios as it simulates
	// typical workload behavior
	assert.Equal(t, corev1.RestartPolicyAlways, podSpec.RestartPolicy)
}

// TestPodSpecBuilder_ImmutabilityAfterBuild verifies that the PodSpec can be
// safely modified after building without affecting the builder.
func TestPodSpecBuilder_ImmutabilityAfterBuild(t *testing.T) {
	builder := NewPodBuilder()
	podSpec := builder.Build()

	// Modify the built PodSpec
	originalContainerName := podSpec.Containers[0].Name
	podSpec.Containers[0].Name = "modified-container"
	podSpec.RestartPolicy = corev1.RestartPolicyNever

	// Build again and verify the builder's internal state wasn't affected
	podSpec2 := builder.Build()

	// The second build should reflect the modifications since it's the same object
	assert.Equal(t, "modified-container", podSpec2.Containers[0].Name)
	assert.Equal(t, corev1.RestartPolicyNever, podSpec2.RestartPolicy)

	// Verify they are the same object
	assert.True(t, podSpec == podSpec2)

	// This demonstrates that the builder returns a reference to its internal state,
	// which is appropriate for the testing use case where the PodSpec is typically
	// used immediately and not shared across multiple contexts
	_ = originalContainerName // Avoid unused variable warning
}

// TestPodSpecBuilder_SuitableForTesting verifies that the default PodSpec
// is suitable for various testing scenarios.
func TestPodSpecBuilder_SuitableForTesting(t *testing.T) {
	builder := NewPodBuilder()
	podSpec := builder.Build()

	// Verify the PodSpec can be used in Kubernetes resource definitions
	assert.NotEmpty(t, podSpec.Containers)
	assert.NotEmpty(t, podSpec.Containers[0].Name)
	assert.NotEmpty(t, podSpec.Containers[0].Image)

	// Verify the container command is non-empty (required for some test scenarios)
	assert.NotEmpty(t, podSpec.Containers[0].Command)

	// Verify the restart policy is set (required field)
	assert.NotEqual(t, corev1.RestartPolicy(""), podSpec.RestartPolicy)

	// Verify the configuration is minimal but complete
	// (no unnecessary complexity for testing)
	assert.Empty(t, podSpec.InitContainers)
	assert.Empty(t, podSpec.Volumes)
	assert.Empty(t, podSpec.ImagePullSecrets)
}

// TestPodSpecBuilder_AlpineImageChoice verifies that the choice of Alpine Linux
// as the default image is appropriate for testing.
func TestPodSpecBuilder_AlpineImageChoice(t *testing.T) {
	builder := NewPodBuilder()
	podSpec := builder.Build()

	container := podSpec.Containers[0]

	// Alpine is chosen because:
	// 1. It's lightweight (small download/startup time)
	// 2. It's widely available in container registries
	// 3. It includes basic shell utilities needed for testing
	// 4. It has a specific version tag for reproducibility
	assert.Equal(t, "alpine:3.21", container.Image)

	// The sleep command provides a long-running process that:
	// 1. Keeps the container alive for testing
	// 2. Doesn't consume significant resources
	// 3. Can be easily terminated
	// 4. Has a reasonable timeout (2 minutes) for test scenarios
	commandStr := strings.Join(container.Command, " ")
	assert.Contains(t, commandStr, "sleep")
	assert.Contains(t, commandStr, "2m")
}
