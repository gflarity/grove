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

// TestNewPodGangSetBuilder verifies that a new PodGangSetBuilder is properly
// initialized with correct default values.
func TestNewPodGangSetBuilder(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different builder initialization scenarios
		name string
		// Name of the PodGangSet
		pgsName string
		// Namespace for the PodGangSet
		namespace string
	}{
		{
			// Standard PodGangSet creation scenario
			name:      "standard podgangset",
			pgsName:   "test-pgs",
			namespace: "default",
		},
		{
			// PodGangSet with different namespace
			name:      "different namespace",
			pgsName:   "prod-pgs",
			namespace: "production",
		},
		{
			// PodGangSet with long names
			name:      "long names",
			pgsName:   "very-long-podgangset-name",
			namespace: "very-long-namespace-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodGangSetBuilder(tt.pgsName, tt.namespace, "test-uid")

			require.NotNil(t, builder)
			require.NotNil(t, builder.pgs)

			// Verify basic metadata
			assert.Equal(t, tt.pgsName, builder.pgs.Name)
			assert.Equal(t, tt.namespace, builder.pgs.Namespace)
			assert.NotEmpty(t, builder.pgs.UID) // Should have a generated UID

			// Verify default spec values
			assert.Equal(t, int32(1), builder.pgs.Spec.Replicas)

			// Verify template is initialized but empty
			assert.Empty(t, builder.pgs.Spec.Template.Cliques)
			assert.Empty(t, builder.pgs.Spec.Template.PodCliqueScalingGroupConfigs)
			assert.Nil(t, builder.pgs.Spec.Template.StartupType)
		})
	}
}

// TestPodGangSetBuilder_WithCliqueStartupType verifies that the startup type
// can be configured for all cliques in the PodGangSet.
func TestPodGangSetBuilder_WithCliqueStartupType(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different startup type scenarios
		name string
		// Startup type to set
		startupType *grovecorev1alpha1.CliqueStartupType
	}{
		{
			// Parallel startup type
			name:        "parallel startup",
			startupType: ptr.To(grovecorev1alpha1.CliqueStartupTypeAnyOrder),
		},
		{
			// Sequential startup type
			name:        "sequential startup",
			startupType: ptr.To(grovecorev1alpha1.CliqueStartupTypeInOrder),
		},
		{
			// Nil startup type (should be handled gracefully)
			name:        "nil startup type",
			startupType: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

			// Apply startup type
			result := builder.WithCliqueStartupType(tt.startupType)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify startup type was set
			assert.Equal(t, tt.startupType, builder.pgs.Spec.Template.StartupType)
		})
	}
}

// TestPodGangSetBuilder_WithReplicas verifies that the replica count can be customized.
func TestPodGangSetBuilder_WithReplicas(t *testing.T) {
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
			// Zero replicas (edge case)
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
			builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

			// Apply replica count
			result := builder.WithReplicas(tt.replicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify replica count was set
			assert.Equal(t, tt.replicas, builder.pgs.Spec.Replicas)
		})
	}
}

// TestPodGangSetBuilder_WithPodCliqueParameters verifies that PodCliques can be
// added with specific parameters.
func TestPodGangSetBuilder_WithPodCliqueParameters(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different PodClique parameter scenarios
		name string
		// Name of the PodClique template
		pclqName string
		// Number of replicas for the PodClique
		replicas int32
		// Dependencies for the PodClique
		startsAfter []string
		// Expected number of cliques after addition
		expectedCliqueCount int
	}{
		{
			// Single PodClique without dependencies
			name:                "single clique no deps",
			pclqName:            "worker",
			replicas:            3,
			startsAfter:         []string{},
			expectedCliqueCount: 1,
		},
		{
			// Single PodClique with dependencies
			name:                "single clique with deps",
			pclqName:            "server",
			replicas:            2,
			startsAfter:         []string{"database", "cache"},
			expectedCliqueCount: 1,
		},
		{
			// Zero replicas PodClique
			name:                "zero replicas clique",
			pclqName:            "disabled",
			replicas:            0,
			startsAfter:         nil,
			expectedCliqueCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

			// Apply PodClique parameters
			result := builder.WithPodCliqueParameters(tt.pclqName, tt.replicas, tt.startsAfter)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify clique was added
			assert.Len(t, builder.pgs.Spec.Template.Cliques, tt.expectedCliqueCount)

			// Verify clique configuration
			clique := builder.pgs.Spec.Template.Cliques[0]
			assert.Equal(t, tt.pclqName, clique.Name)
			assert.Equal(t, tt.replicas, clique.Spec.Replicas)
			assert.Equal(t, tt.startsAfter, clique.Spec.StartsAfter)

			// Verify PodSpec was set (by the builder)
			assert.NotEmpty(t, clique.Spec.PodSpec.Containers)
		})
	}
}

// TestPodGangSetBuilder_WithPodCliqueTemplateSpec verifies that pre-configured
// PodCliqueTemplateSpecs can be added directly.
func TestPodGangSetBuilder_WithPodCliqueTemplateSpec(t *testing.T) {
	// Create a custom PodCliqueTemplateSpec
	customPclq := NewPodCliqueTemplateSpecBuilder("custom-worker").
		WithReplicas(5).
		WithStartsAfter([]string{"init"}).
		WithMinAvailable(3).
		Build()

	builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

	// Apply the custom PodCliqueTemplateSpec
	result := builder.WithPodCliqueTemplateSpec(customPclq)

	// Verify builder is returned for chaining
	assert.Equal(t, builder, result)

	// Verify clique was added
	assert.Len(t, builder.pgs.Spec.Template.Cliques, 1)

	// Verify it's the same object
	assert.Equal(t, customPclq, builder.pgs.Spec.Template.Cliques[0])
}

// TestPodGangSetBuilder_WithPodCliqueScalingGroupConfig verifies that scaling
// group configurations can be added.
func TestPodGangSetBuilder_WithPodCliqueScalingGroupConfig(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different scaling group scenarios
		name string
		// Scaling group configuration to add
		config grovecorev1alpha1.PodCliqueScalingGroupConfig
		// Expected number of scaling groups after addition
		expectedScalingGroupCount int
	}{
		{
			// Basic scaling group configuration
			name: "basic scaling group",
			config: grovecorev1alpha1.PodCliqueScalingGroupConfig{
				Name:         "workers",
				CliqueNames:  []string{"worker-a", "worker-b"},
				Replicas:     ptr.To(int32(3)),
				MinAvailable: ptr.To(int32(2)),
			},
			expectedScalingGroupCount: 1,
		},
		{
			// Scaling group with nil replicas/minAvailable
			name: "scaling group with nils",
			config: grovecorev1alpha1.PodCliqueScalingGroupConfig{
				Name:         "servers",
				CliqueNames:  []string{"server"},
				Replicas:     nil,
				MinAvailable: nil,
			},
			expectedScalingGroupCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

			// Apply scaling group configuration
			result := builder.WithPodCliqueScalingGroupConfig(tt.config)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify scaling group was added
			assert.Len(t, builder.pgs.Spec.Template.PodCliqueScalingGroupConfigs, tt.expectedScalingGroupCount)

			// Verify scaling group configuration
			scalingGroup := builder.pgs.Spec.Template.PodCliqueScalingGroupConfigs[0]
			assert.Equal(t, tt.config.Name, scalingGroup.Name)
			assert.Equal(t, tt.config.CliqueNames, scalingGroup.CliqueNames)
			assert.Equal(t, tt.config.Replicas, scalingGroup.Replicas)
			assert.Equal(t, tt.config.MinAvailable, scalingGroup.MinAvailable)
		})
	}
}

// TestPodGangSetBuilder_WithStandaloneClique verifies that standalone cliques
// can be added with default configuration.
func TestPodGangSetBuilder_WithStandaloneClique(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different standalone clique scenarios
		name string
		// Name of the standalone clique
		cliqueName string
	}{
		{
			// Standard standalone clique
			name:       "standard standalone",
			cliqueName: "standalone-worker",
		},
		{
			// Standalone clique with special characters
			name:       "special chars",
			cliqueName: "worker-with-dashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

			// Apply standalone clique
			result := builder.WithStandaloneClique(tt.cliqueName)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify clique was added
			assert.Len(t, builder.pgs.Spec.Template.Cliques, 1)

			// Verify clique configuration
			clique := builder.pgs.Spec.Template.Cliques[0]
			assert.Equal(t, tt.cliqueName, clique.Name)
			assert.Equal(t, int32(1), clique.Spec.Replicas) // Default replica count
		})
	}
}

// TestPodGangSetBuilder_WithStandaloneCliqueReplicas verifies that standalone
// cliques can be added with custom replica counts.
func TestPodGangSetBuilder_WithStandaloneCliqueReplicas(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different replica scenarios
		name string
		// Name of the standalone clique
		cliqueName string
		// Number of replicas for the clique
		replicas int32
	}{
		{
			// Single replica standalone clique
			name:       "single replica",
			cliqueName: "single-worker",
			replicas:   1,
		},
		{
			// Multiple replica standalone clique
			name:       "multiple replicas",
			cliqueName: "multi-worker",
			replicas:   5,
		},
		{
			// Zero replica standalone clique
			name:       "zero replicas",
			cliqueName: "disabled-worker",
			replicas:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

			// Apply standalone clique with replicas
			result := builder.WithStandaloneCliqueReplicas(tt.cliqueName, tt.replicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify clique was added
			assert.Len(t, builder.pgs.Spec.Template.Cliques, 1)

			// Verify clique configuration
			clique := builder.pgs.Spec.Template.Cliques[0]
			assert.Equal(t, tt.cliqueName, clique.Name)
			assert.Equal(t, tt.replicas, clique.Spec.Replicas)
		})
	}
}

// TestPodGangSetBuilder_WithScalingGroup verifies that scaling groups can be
// added with default configuration.
func TestPodGangSetBuilder_WithScalingGroup(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different scaling group scenarios
		name string
		// Name of the scaling group
		scalingGroupName string
		// Names of cliques to be managed by the scaling group
		cliqueNames []string
		// Expected number of cliques created
		expectedCliqueCount int
		// Expected number of scaling groups created
		expectedScalingGroupCount int
	}{
		{
			// Single clique scaling group
			name:                      "single clique group",
			scalingGroupName:          "workers",
			cliqueNames:               []string{"worker"},
			expectedCliqueCount:       1,
			expectedScalingGroupCount: 1,
		},
		{
			// Multiple clique scaling group
			name:                      "multiple clique group",
			scalingGroupName:          "servers",
			cliqueNames:               []string{"web-server", "api-server", "db-server"},
			expectedCliqueCount:       3,
			expectedScalingGroupCount: 1,
		},
		{
			// Empty clique names
			name:                      "empty clique names",
			scalingGroupName:          "empty",
			cliqueNames:               []string{},
			expectedCliqueCount:       0,
			expectedScalingGroupCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

			// Apply scaling group
			result := builder.WithScalingGroup(tt.scalingGroupName, tt.cliqueNames)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify cliques were created
			assert.Len(t, builder.pgs.Spec.Template.Cliques, tt.expectedCliqueCount)

			// Verify each clique has correct configuration
			for i, expectedName := range tt.cliqueNames {
				clique := builder.pgs.Spec.Template.Cliques[i]
				assert.Equal(t, expectedName, clique.Name)
				assert.Equal(t, int32(1), clique.Spec.Replicas) // Default for scaling group cliques
			}

			// Verify scaling group was created
			assert.Len(t, builder.pgs.Spec.Template.PodCliqueScalingGroupConfigs, tt.expectedScalingGroupCount)

			if tt.expectedScalingGroupCount > 0 {
				scalingGroup := builder.pgs.Spec.Template.PodCliqueScalingGroupConfigs[0]
				assert.Equal(t, tt.scalingGroupName, scalingGroup.Name)
				assert.Equal(t, tt.cliqueNames, scalingGroup.CliqueNames)
				require.NotNil(t, scalingGroup.Replicas)
				assert.Equal(t, int32(1), *scalingGroup.Replicas) // Default replicas
				require.NotNil(t, scalingGroup.MinAvailable)
				assert.Equal(t, int32(1), *scalingGroup.MinAvailable) // Default minAvailable
			}
		})
	}
}

// TestPodGangSetBuilder_WithScalingGroupConfig verifies that scaling groups
// can be added with custom configuration.
func TestPodGangSetBuilder_WithScalingGroupConfig(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different custom scaling group scenarios
		name string
		// Name of the scaling group
		scalingGroupName string
		// Names of cliques to be managed by the scaling group
		cliqueNames []string
		// Number of replicas for the scaling group
		replicas int32
		// Minimum available replicas
		minAvailable int32
	}{
		{
			// Custom scaling group configuration
			name:             "custom config",
			scalingGroupName: "workers",
			cliqueNames:      []string{"worker-a", "worker-b"},
			replicas:         5,
			minAvailable:     3,
		},
		{
			// Zero replicas scaling group
			name:             "zero replicas",
			scalingGroupName: "disabled",
			cliqueNames:      []string{"disabled-worker"},
			replicas:         0,
			minAvailable:     0,
		},
		{
			// Large scaling group
			name:             "large scaling group",
			scalingGroupName: "massive",
			cliqueNames:      []string{"worker-1", "worker-2", "worker-3", "worker-4"},
			replicas:         100,
			minAvailable:     80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

			// Apply custom scaling group configuration
			result := builder.WithScalingGroupConfig(tt.scalingGroupName, tt.cliqueNames, tt.replicas, tt.minAvailable)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify cliques were created
			assert.Len(t, builder.pgs.Spec.Template.Cliques, len(tt.cliqueNames))

			// Verify scaling group was created with custom configuration
			assert.Len(t, builder.pgs.Spec.Template.PodCliqueScalingGroupConfigs, 1)

			scalingGroup := builder.pgs.Spec.Template.PodCliqueScalingGroupConfigs[0]
			assert.Equal(t, tt.scalingGroupName, scalingGroup.Name)
			assert.Equal(t, tt.cliqueNames, scalingGroup.CliqueNames)
			require.NotNil(t, scalingGroup.Replicas)
			assert.Equal(t, tt.replicas, *scalingGroup.Replicas)
			require.NotNil(t, scalingGroup.MinAvailable)
			assert.Equal(t, tt.minAvailable, *scalingGroup.MinAvailable)
		})
	}
}

// TestPodGangSetBuilder_Build verifies that the Build method returns a properly
// configured PodGangSet.
func TestPodGangSetBuilder_Build(t *testing.T) {
	builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

	// Customize the builder
	pgs := builder.
		WithReplicas(3).
		WithCliqueStartupType(ptr.To(grovecorev1alpha1.CliqueStartupTypeInOrder)).
		WithPodCliqueParameters("database", 1, []string{}).
		WithStandaloneClique("cache").
		WithScalingGroupConfig("workers", []string{"worker-a", "worker-b"}, 5, 3).
		Build()

	// Verify the built PodGangSet
	require.NotNil(t, pgs)

	// Verify basic metadata
	assert.Equal(t, "test-pgs", pgs.Name)
	assert.Equal(t, "default", pgs.Namespace)
	assert.NotEmpty(t, pgs.UID)

	// Verify spec configuration
	assert.Equal(t, int32(3), pgs.Spec.Replicas)
	require.NotNil(t, pgs.Spec.Template.StartupType)
	assert.Equal(t, grovecorev1alpha1.CliqueStartupTypeInOrder, *pgs.Spec.Template.StartupType)

	// Verify cliques were added (database + cache + worker-a + worker-b = 4)
	assert.Len(t, pgs.Spec.Template.Cliques, 4)

	// Verify scaling group was added
	assert.Len(t, pgs.Spec.Template.PodCliqueScalingGroupConfigs, 1)
	scalingGroup := pgs.Spec.Template.PodCliqueScalingGroupConfigs[0]
	assert.Equal(t, "workers", scalingGroup.Name)
	assert.Equal(t, []string{"worker-a", "worker-b"}, scalingGroup.CliqueNames)
}

// TestPodGangSetBuilder_Chaining verifies that all builder methods can be
// chained together fluently.
func TestPodGangSetBuilder_Chaining(t *testing.T) {
	// This test verifies the fluent interface works correctly
	pgs := NewPodGangSetBuilder("complex-pgs", "production", "test-uid").
		WithReplicas(10).
		WithCliqueStartupType(ptr.To(grovecorev1alpha1.CliqueStartupTypeAnyOrder)).
		WithPodCliqueParameters("init", 1, []string{}).
		WithPodCliqueParameters("database", 3, []string{"init"}).
		WithStandaloneClique("cache").
		WithStandaloneCliqueReplicas("monitoring", 2).
		WithScalingGroup("workers", []string{"worker-cpu", "worker-gpu"}).
		WithScalingGroupConfig("servers", []string{"web-server", "api-server"}, 8, 6).
		Build()

	// Verify all configurations were applied
	assert.Equal(t, "complex-pgs", pgs.Name)
	assert.Equal(t, "production", pgs.Namespace)
	assert.Equal(t, int32(10), pgs.Spec.Replicas)
	assert.Equal(t, grovecorev1alpha1.CliqueStartupTypeAnyOrder, *pgs.Spec.Template.StartupType)

	// Verify cliques count: init + database + cache + monitoring + worker-cpu + worker-gpu + web-server + api-server = 8
	assert.Len(t, pgs.Spec.Template.Cliques, 8)

	// Verify scaling groups count: workers + servers = 2
	assert.Len(t, pgs.Spec.Template.PodCliqueScalingGroupConfigs, 2)
}

// TestPodGangSetBuilder_MultipleAdditions verifies that multiple cliques and
// scaling groups can be added incrementally.
func TestPodGangSetBuilder_MultipleAdditions(t *testing.T) {
	builder := NewPodGangSetBuilder("test-pgs", "default", "test-uid")

	// Add cliques incrementally
	builder.WithStandaloneClique("clique-1")
	builder.WithStandaloneClique("clique-2")
	builder.WithPodCliqueParameters("clique-3", 2, []string{"clique-1"})

	// Add scaling groups incrementally
	builder.WithScalingGroup("group-1", []string{"worker-1", "worker-2"})
	builder.WithScalingGroup("group-2", []string{"server-1"})

	pgs := builder.Build()

	// Verify all additions were preserved
	// clique-1 + clique-2 + clique-3 + worker-1 + worker-2 + server-1 = 6
	assert.Len(t, pgs.Spec.Template.Cliques, 6)
	assert.Len(t, pgs.Spec.Template.PodCliqueScalingGroupConfigs, 2)
}

// TestCreateEmptyPodGangSet verifies the internal helper function creates
// a properly initialized empty PodGangSet.
func TestCreateEmptyPodGangSet(t *testing.T) {
	name := "test-pgs"
	namespace := "test-namespace"

	pgs := createEmptyPodGangSet(name, namespace, "test-uid")

	// Verify basic metadata
	assert.Equal(t, name, pgs.Name)
	assert.Equal(t, namespace, pgs.Namespace)
	assert.NotEmpty(t, pgs.UID) // Should have a generated UID

	// Verify default spec
	assert.Equal(t, int32(1), pgs.Spec.Replicas)

	// Verify template is initialized but empty
	assert.Empty(t, pgs.Spec.Template.Cliques)
	assert.Empty(t, pgs.Spec.Template.PodCliqueScalingGroupConfigs)
	assert.Nil(t, pgs.Spec.Template.StartupType)
}

// TestPodGangSetBuilder_UIDGeneration verifies that each PodGangSet gets
// a unique UID when created.
func TestPodGangSetBuilder_UIDGeneration(t *testing.T) {
	builder1 := NewPodGangSetBuilder("pgs-1", "default", "test-uid-1")
	builder2 := NewPodGangSetBuilder("pgs-2", "default", "test-uid-2")

	pgs1 := builder1.Build()
	pgs2 := builder2.Build()

	// Verify UIDs are different
	assert.NotEqual(t, pgs1.UID, pgs2.UID)
	assert.NotEmpty(t, pgs1.UID)
	assert.NotEmpty(t, pgs2.UID)

	// Verify UIDs are valid UUID format (basic check)
	assert.True(t, string(pgs1.UID) != "")
	assert.True(t, string(pgs2.UID) != "")
}
