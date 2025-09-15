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
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

// TestNewPodCliqueTemplateSpecBuilder verifies that a new PodCliqueTemplateSpecBuilder
// is properly initialized with correct default values.
func TestNewPodCliqueTemplateSpecBuilder(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different builder initialization scenarios
		name string
		// Name of the PodCliqueTemplateSpec
		templateName string
	}{
		{
			// Standard template creation scenario
			name:         "standard template",
			templateName: "worker",
		},
		{
			// Template with special characters
			name:         "special chars",
			templateName: "worker-with-dashes",
		},
		{
			// Template with long name
			name:         "long name",
			templateName: "very-long-podclique-template-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder(tt.templateName)

			require.NotNil(t, builder)
			require.NotNil(t, builder.pclqTemplateSpec)

			// Verify basic configuration
			assert.Equal(t, tt.templateName, builder.pclqTemplateSpec.Name)

			// Verify default spec values
			assert.Equal(t, int32(1), builder.pclqTemplateSpec.Spec.Replicas)

			// Verify other fields are in default state
			assert.Nil(t, builder.pclqTemplateSpec.Labels)
			assert.Empty(t, builder.pclqTemplateSpec.Spec.StartsAfter)
			assert.Nil(t, builder.pclqTemplateSpec.Spec.MinAvailable)
			assert.Nil(t, builder.pclqTemplateSpec.Spec.ScaleConfig)
		})
	}
}

// TestPodCliqueTemplateSpecBuilder_WithReplicas verifies that the replica count
// can be customized.
func TestPodCliqueTemplateSpecBuilder_WithReplicas(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different replica scenarios
		name string
		// Number of replicas to set
		replicas int32
	}{
		{
			// Single replica (default case)
			name:     "single replica",
			replicas: 1,
		},
		{
			// Multiple replicas
			name:     "multiple replicas",
			replicas: 5,
		},
		{
			// Zero replicas (disabled PodClique)
			name:     "zero replicas",
			replicas: 0,
		},
		{
			// Large number of replicas
			name:     "large replica count",
			replicas: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder("test-template")

			// Apply replica count
			result := builder.WithReplicas(tt.replicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify replica count was set
			assert.Equal(t, tt.replicas, builder.pclqTemplateSpec.Spec.Replicas)
		})
	}
}

// TestPodCliqueTemplateSpecBuilder_WithLabels verifies that labels can be set
// on the PodCliqueTemplateSpec.
func TestPodCliqueTemplateSpecBuilder_WithLabels(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different label scenarios
		name string
		// Labels to set on the template
		labels map[string]string
	}{
		{
			// Standard labels
			name: "standard labels",
			labels: map[string]string{
				"app":         "my-app",
				"environment": "test",
				"team":        "platform",
			},
		},
		{
			// Single label
			name: "single label",
			labels: map[string]string{
				"component": "worker",
			},
		},
		{
			// Empty labels map
			name:   "empty labels",
			labels: map[string]string{},
		},
		{
			// Nil labels map
			name:   "nil labels",
			labels: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder("test-template")

			// Apply labels
			result := builder.WithLabels(tt.labels)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify labels were set
			assert.Equal(t, tt.labels, builder.pclqTemplateSpec.Labels)
		})
	}
}

// TestPodCliqueTemplateSpecBuilder_WithStartsAfter verifies that dependencies
// can be configured.
func TestPodCliqueTemplateSpecBuilder_WithStartsAfter(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different dependency scenarios
		name string
		// Dependencies for the PodClique
		startsAfter []string
		// Expected number of dependencies
		expectedCount int
	}{
		{
			// No dependencies
			name:          "no dependencies",
			startsAfter:   []string{},
			expectedCount: 0,
		},
		{
			// Single dependency
			name:          "single dependency",
			startsAfter:   []string{"database"},
			expectedCount: 1,
		},
		{
			// Multiple dependencies
			name:          "multiple dependencies",
			startsAfter:   []string{"database", "cache", "message-queue"},
			expectedCount: 3,
		},
		{
			// Nil dependencies
			name:          "nil dependencies",
			startsAfter:   nil,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder("test-template")

			// Apply dependencies
			result := builder.WithStartsAfter(tt.startsAfter)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify dependencies were set
			assert.Len(t, builder.pclqTemplateSpec.Spec.StartsAfter, tt.expectedCount)
			assert.Equal(t, tt.startsAfter, builder.pclqTemplateSpec.Spec.StartsAfter)
		})
	}
}

// TestPodCliqueTemplateSpecBuilder_WithMinAvailable verifies that the minimum
// available replicas can be configured.
func TestPodCliqueTemplateSpecBuilder_WithMinAvailable(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different MinAvailable scenarios
		name string
		// Minimum available replicas to set
		minAvailable int32
	}{
		{
			// Single minimum available
			name:         "single min available",
			minAvailable: 1,
		},
		{
			// Multiple minimum available
			name:         "multiple min available",
			minAvailable: 3,
		},
		{
			// Zero minimum available
			name:         "zero min available",
			minAvailable: 0,
		},
		{
			// Large minimum available
			name:         "large min available",
			minAvailable: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder("test-template")

			// Apply MinAvailable
			result := builder.WithMinAvailable(tt.minAvailable)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify MinAvailable was set
			require.NotNil(t, builder.pclqTemplateSpec.Spec.MinAvailable)
			assert.Equal(t, tt.minAvailable, *builder.pclqTemplateSpec.Spec.MinAvailable)
		})
	}
}

// TestPodCliqueTemplateSpecBuilder_WithScaleConfig verifies that autoscaling
// configuration can be set with both min and max replicas.
func TestPodCliqueTemplateSpecBuilder_WithScaleConfig(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different scale config scenarios
		name string
		// Minimum replicas for autoscaling (can be nil)
		minReplicas *int32
		// Maximum replicas for autoscaling
		maxReplicas int32
	}{
		{
			// Both min and max specified
			name:        "both min and max",
			minReplicas: ptr.To(int32(2)),
			maxReplicas: 10,
		},
		{
			// Only max specified (min is nil)
			name:        "only max specified",
			minReplicas: nil,
			maxReplicas: 5,
		},
		{
			// Min equals max (fixed scaling)
			name:        "min equals max",
			minReplicas: ptr.To(int32(3)),
			maxReplicas: 3,
		},
		{
			// Large scale config
			name:        "large scale config",
			minReplicas: ptr.To(int32(10)),
			maxReplicas: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder("test-template")

			// Apply scale config
			result := builder.WithScaleConfig(tt.minReplicas, tt.maxReplicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify scale config was set
			require.NotNil(t, builder.pclqTemplateSpec.Spec.ScaleConfig)
			assert.Equal(t, tt.minReplicas, builder.pclqTemplateSpec.Spec.ScaleConfig.MinReplicas)
			assert.Equal(t, tt.maxReplicas, builder.pclqTemplateSpec.Spec.ScaleConfig.MaxReplicas)
		})
	}
}

// TestPodCliqueTemplateSpecBuilder_WithAutoScaleMinReplicas verifies that
// minimum replicas for autoscaling can be set independently.
func TestPodCliqueTemplateSpecBuilder_WithAutoScaleMinReplicas(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different min replica scenarios
		name string
		// Minimum replicas for autoscaling
		minReplicas int32
		// Whether ScaleConfig should be created if it doesn't exist
		shouldCreateConfig bool
	}{
		{
			// Setting min replicas when no config exists
			name:               "create new config",
			minReplicas:        2,
			shouldCreateConfig: true,
		},
		{
			// Setting min replicas with different values
			name:               "different min values",
			minReplicas:        5,
			shouldCreateConfig: true,
		},
		{
			// Zero min replicas
			name:               "zero min replicas",
			minReplicas:        0,
			shouldCreateConfig: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder("test-template")

			// Apply min replicas
			result := builder.WithAutoScaleMinReplicas(tt.minReplicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify scale config was created and min replicas set
			require.NotNil(t, builder.pclqTemplateSpec.Spec.ScaleConfig)
			require.NotNil(t, builder.pclqTemplateSpec.Spec.ScaleConfig.MinReplicas)
			assert.Equal(t, tt.minReplicas, *builder.pclqTemplateSpec.Spec.ScaleConfig.MinReplicas)

			// MaxReplicas should not be set by this method
			assert.Equal(t, int32(0), builder.pclqTemplateSpec.Spec.ScaleConfig.MaxReplicas)
		})
	}
}

// TestPodCliqueTemplateSpecBuilder_WithAutoScaleMaxReplicas verifies that
// maximum replicas for autoscaling can be set independently.
func TestPodCliqueTemplateSpecBuilder_WithAutoScaleMaxReplicas(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different max replica scenarios
		name string
		// Maximum replicas for autoscaling
		maxReplicas int32
	}{
		{
			// Setting max replicas when no config exists
			name:        "create new config",
			maxReplicas: 10,
		},
		{
			// Different max values
			name:        "different max values",
			maxReplicas: 50,
		},
		{
			// Small max replicas
			name:        "small max replicas",
			maxReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder("test-template")

			// Apply max replicas
			result := builder.WithAutoScaleMaxReplicas(tt.maxReplicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify scale config was created and max replicas set
			require.NotNil(t, builder.pclqTemplateSpec.Spec.ScaleConfig)
			assert.Equal(t, tt.maxReplicas, builder.pclqTemplateSpec.Spec.ScaleConfig.MaxReplicas)

			// MinReplicas should not be set by this method
			assert.Nil(t, builder.pclqTemplateSpec.Spec.ScaleConfig.MinReplicas)
		})
	}
}

// TestPodCliqueTemplateSpecBuilder_Build verifies that the Build method returns
// a properly configured PodCliqueTemplateSpec with default PodSpec.
func TestPodCliqueTemplateSpecBuilder_Build(t *testing.T) {
	builder := NewPodCliqueTemplateSpecBuilder("test-template")

	// Customize the builder
	template := builder.
		WithReplicas(3).
		WithLabels(map[string]string{"app": "test"}).
		WithStartsAfter([]string{"database"}).
		WithMinAvailable(2).
		WithScaleConfig(ptr.To(int32(1)), 10).
		Build()

	// Verify the built PodCliqueTemplateSpec
	require.NotNil(t, template)

	// Verify basic configuration
	assert.Equal(t, "test-template", template.Name)

	// Verify spec configuration
	assert.Equal(t, int32(3), template.Spec.Replicas)
	assert.Equal(t, []string{"database"}, template.Spec.StartsAfter)
	require.NotNil(t, template.Spec.MinAvailable)
	assert.Equal(t, int32(2), *template.Spec.MinAvailable)

	// Verify scale config
	require.NotNil(t, template.Spec.ScaleConfig)
	require.NotNil(t, template.Spec.ScaleConfig.MinReplicas)
	assert.Equal(t, int32(1), *template.Spec.ScaleConfig.MinReplicas)
	assert.Equal(t, int32(10), template.Spec.ScaleConfig.MaxReplicas)

	// Verify labels
	assert.Equal(t, map[string]string{"app": "test"}, template.Labels)

	// Verify default PodSpec was set
	require.NotNil(t, template.Spec.PodSpec.Containers)
	assert.Len(t, template.Spec.PodSpec.Containers, 1)
	assert.Equal(t, "test-container", template.Spec.PodSpec.Containers[0].Name)
}

// TestPodCliqueTemplateSpecBuilder_Chaining verifies that all builder methods
// can be chained together fluently.
func TestPodCliqueTemplateSpecBuilder_Chaining(t *testing.T) {
	// This test verifies the fluent interface works correctly
	template := NewPodCliqueTemplateSpecBuilder("chained-template").
		WithReplicas(5).
		WithLabels(map[string]string{
			"environment": "test",
			"component":   "worker",
		}).
		WithStartsAfter([]string{"init", "database", "cache"}).
		WithMinAvailable(3).
		WithAutoScaleMinReplicas(2).
		WithAutoScaleMaxReplicas(20).
		Build()

	// Verify all configurations were applied
	assert.Equal(t, "chained-template", template.Name)
	assert.Equal(t, int32(5), template.Spec.Replicas)
	assert.Equal(t, "test", template.Labels["environment"])
	assert.Equal(t, "worker", template.Labels["component"])
	assert.Equal(t, []string{"init", "database", "cache"}, template.Spec.StartsAfter)
	assert.Equal(t, int32(3), *template.Spec.MinAvailable)
	assert.Equal(t, int32(2), *template.Spec.ScaleConfig.MinReplicas)
	assert.Equal(t, int32(20), template.Spec.ScaleConfig.MaxReplicas)
}

// TestPodCliqueTemplateSpecBuilder_ScaleConfigOverride verifies that scale
// config methods can override each other properly.
func TestPodCliqueTemplateSpecBuilder_ScaleConfigOverride(t *testing.T) {
	builder := NewPodCliqueTemplateSpecBuilder("test-template")

	// First set complete scale config
	builder.WithScaleConfig(ptr.To(int32(1)), 5)

	// Then override with individual methods
	template := builder.
		WithAutoScaleMinReplicas(3).
		WithAutoScaleMaxReplicas(15).
		Build()

	// Verify the final values reflect the last settings
	require.NotNil(t, template.Spec.ScaleConfig)
	require.NotNil(t, template.Spec.ScaleConfig.MinReplicas)
	assert.Equal(t, int32(3), *template.Spec.ScaleConfig.MinReplicas) // Overridden
	assert.Equal(t, int32(15), template.Spec.ScaleConfig.MaxReplicas) // Overridden
}

// TestPodCliqueTemplateSpecBuilder_IndependentScaleConfigMethods verifies that
// individual scale config methods work independently.
func TestPodCliqueTemplateSpecBuilder_IndependentScaleConfigMethods(t *testing.T) {
	tests := []struct {
		// Test case description - verifies independent method behavior
		name string
		// Function to apply to the builder
		applyFunc func(*PodCliqueTemplateSpecBuilder) *PodCliqueTemplateSpecBuilder
		// Function to verify the expected result
		verifyFunc func(*testing.T, *grovecorev1alpha1.PodCliqueTemplateSpec)
	}{
		{
			// Only min replicas set
			name: "only min replicas",
			applyFunc: func(b *PodCliqueTemplateSpecBuilder) *PodCliqueTemplateSpecBuilder {
				return b.WithAutoScaleMinReplicas(4)
			},
			verifyFunc: func(t *testing.T, template *grovecorev1alpha1.PodCliqueTemplateSpec) {
				require.NotNil(t, template.Spec.ScaleConfig)
				require.NotNil(t, template.Spec.ScaleConfig.MinReplicas)
				assert.Equal(t, int32(4), *template.Spec.ScaleConfig.MinReplicas)
				assert.Equal(t, int32(0), template.Spec.ScaleConfig.MaxReplicas) // Default value
			},
		},
		{
			// Only max replicas set
			name: "only max replicas",
			applyFunc: func(b *PodCliqueTemplateSpecBuilder) *PodCliqueTemplateSpecBuilder {
				return b.WithAutoScaleMaxReplicas(12)
			},
			verifyFunc: func(t *testing.T, template *grovecorev1alpha1.PodCliqueTemplateSpec) {
				require.NotNil(t, template.Spec.ScaleConfig)
				assert.Nil(t, template.Spec.ScaleConfig.MinReplicas) // Not set
				assert.Equal(t, int32(12), template.Spec.ScaleConfig.MaxReplicas)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueTemplateSpecBuilder("test-template")
			template := tt.applyFunc(builder).Build()
			tt.verifyFunc(t, template)
		})
	}
}

// TestCreateDefaultPodCliqueTemplateSpec verifies the internal helper function
// creates a properly initialized PodCliqueTemplateSpec.
func TestCreateDefaultPodCliqueTemplateSpec(t *testing.T) {
	name := "test-template"
	template := createDefaultPodCliqueTemplateSpec(name)

	// Verify basic configuration
	assert.Equal(t, name, template.Name)
	assert.Equal(t, int32(1), template.Spec.Replicas)

	// Verify other fields are in default state
	assert.Nil(t, template.Labels)
	assert.Empty(t, template.Spec.StartsAfter)
	assert.Nil(t, template.Spec.MinAvailable)
	assert.Nil(t, template.Spec.ScaleConfig)

	// Verify PodSpec is empty (will be set by Build method)
	assert.Empty(t, template.Spec.PodSpec.Containers)
}
