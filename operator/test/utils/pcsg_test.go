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
	"strconv"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestNewPodCliqueScalingGroupBuilder verifies that a new PodCliqueScalingGroupBuilder
// is properly initialized with correct default values and labels.
func TestNewPodCliqueScalingGroupBuilder(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different builder initialization scenarios
		name string
		// Name of the PodCliqueScalingGroup
		pcsgName string
		// Namespace for the PodCliqueScalingGroup
		namespace string
		// Name of the parent PodGangSet
		pgsName string
		// Replica index within the PodGangSet
		replicaIndex int
	}{
		{
			// Standard PCSG creation scenario
			name:         "standard pcsg",
			pcsgName:     "workers",
			namespace:    "default",
			pgsName:      "test-pgs",
			replicaIndex: 0,
		},
		{
			// PCSG with different replica index
			name:         "different replica index",
			pcsgName:     "servers",
			namespace:    "production",
			pgsName:      "prod-pgs",
			replicaIndex: 2,
		},
		{
			// PCSG with long names
			name:         "long names",
			pcsgName:     "very-long-scaling-group-name",
			namespace:    "very-long-namespace-name",
			pgsName:      "very-long-podgangset-name",
			replicaIndex: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueScalingGroupBuilder(tt.pcsgName, tt.namespace, tt.pgsName, tt.replicaIndex)

			require.NotNil(t, builder)
			require.NotNil(t, builder.pcsg)

			// Verify basic metadata
			assert.Equal(t, tt.pcsgName, builder.pcsg.Name)
			assert.Equal(t, tt.namespace, builder.pcsg.Namespace)

			// Verify default labels
			expectedLabels := map[string]string{
				grovecorev1alpha1.LabelManagedByKey:           grovecorev1alpha1.LabelManagedByValue,
				grovecorev1alpha1.LabelPartOfKey:              tt.pgsName,
				grovecorev1alpha1.LabelPodGangSetReplicaIndex: strconv.Itoa(tt.replicaIndex),
			}
			for key, expectedValue := range expectedLabels {
				assert.Equal(t, expectedValue, builder.pcsg.Labels[key], "Label %s should match", key)
			}

			// Verify default spec values
			assert.Equal(t, int32(1), builder.pcsg.Spec.Replicas)
			require.NotNil(t, builder.pcsg.Spec.MinAvailable)
			assert.Equal(t, int32(1), *builder.pcsg.Spec.MinAvailable)

			// Verify empty status
			assert.Equal(t, grovecorev1alpha1.PodCliqueScalingGroupStatus{}, builder.pcsg.Status)
		})
	}
}

// TestPodCliqueScalingGroupBuilder_WithReplicas verifies that the replica count
// can be customized.
func TestPodCliqueScalingGroupBuilder_WithReplicas(t *testing.T) {
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
			builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)

			// Apply replica count
			result := builder.WithReplicas(tt.replicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify replica count was set
			assert.Equal(t, tt.replicas, builder.pcsg.Spec.Replicas)
		})
	}
}

// TestPodCliqueScalingGroupBuilder_WithMinAvailable verifies that the MinAvailable
// field can be customized.
func TestPodCliqueScalingGroupBuilder_WithMinAvailable(t *testing.T) {
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
			builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)

			// Apply MinAvailable
			result := builder.WithMinAvailable(tt.minAvailable)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify MinAvailable was set
			require.NotNil(t, builder.pcsg.Spec.MinAvailable)
			assert.Equal(t, tt.minAvailable, *builder.pcsg.Spec.MinAvailable)
		})
	}
}

// TestPodCliqueScalingGroupBuilder_WithCliqueNames verifies that the CliqueNames
// field can be set with different configurations.
func TestPodCliqueScalingGroupBuilder_WithCliqueNames(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different clique name scenarios
		name string
		// Names of cliques to be managed by this scaling group
		cliqueNames []string
		// Expected number of clique names
		expectedCount int
	}{
		{
			// No clique names
			name:          "no clique names",
			cliqueNames:   []string{},
			expectedCount: 0,
		},
		{
			// Single clique name
			name:          "single clique name",
			cliqueNames:   []string{"worker"},
			expectedCount: 1,
		},
		{
			// Multiple clique names
			name:          "multiple clique names",
			cliqueNames:   []string{"worker", "server", "cache"},
			expectedCount: 3,
		},
		{
			// Nil clique names
			name:          "nil clique names",
			cliqueNames:   nil,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)

			// Apply clique names
			result := builder.WithCliqueNames(tt.cliqueNames)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify clique names were set
			assert.Len(t, builder.pcsg.Spec.CliqueNames, tt.expectedCount)
			for i, expectedName := range tt.cliqueNames {
				assert.Equal(t, expectedName, builder.pcsg.Spec.CliqueNames[i])
			}
		})
	}
}

// TestPodCliqueScalingGroupBuilder_WithLabels verifies that custom labels
// can be added and merged with default labels.
func TestPodCliqueScalingGroupBuilder_WithLabels(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different label scenarios
		name string
		// Custom labels to add
		customLabels map[string]string
		// Whether custom labels should override defaults
		shouldOverrideDefaults bool
	}{
		{
			// Additional labels that don't conflict with defaults
			name: "additional labels",
			customLabels: map[string]string{
				"custom-label": "custom-value",
				"environment":  "test",
				"team":         "platform",
			},
			shouldOverrideDefaults: false,
		},
		{
			// Labels that override default values
			name: "override default labels",
			customLabels: map[string]string{
				grovecorev1alpha1.LabelPartOfKey: "custom-pgs",
				"custom-override":                "value",
			},
			shouldOverrideDefaults: true,
		},
		{
			// Empty labels map
			name:                   "empty labels",
			customLabels:           map[string]string{},
			shouldOverrideDefaults: false,
		},
		{
			// Nil labels map
			name:                   "nil labels",
			customLabels:           nil,
			shouldOverrideDefaults: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)
			originalLabels := make(map[string]string)
			for k, v := range builder.pcsg.Labels {
				originalLabels[k] = v
			}

			// Apply custom labels
			result := builder.WithLabels(tt.customLabels)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify custom labels were added
			for key, expectedValue := range tt.customLabels {
				assert.Equal(t, expectedValue, builder.pcsg.Labels[key], "Custom label %s should be set", key)
			}

			// Verify original labels are preserved (unless overridden)
			for key, originalValue := range originalLabels {
				if _, overridden := tt.customLabels[key]; !overridden {
					assert.Equal(t, originalValue, builder.pcsg.Labels[key], "Original label %s should be preserved", key)
				}
			}
		})
	}
}

// TestPodCliqueScalingGroupBuilder_WithOwnerReference verifies that custom
// owner references can be added to the PodCliqueScalingGroup.
func TestPodCliqueScalingGroupBuilder_WithOwnerReference(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different owner reference scenarios
		name string
		// Kind of the owner resource
		ownerKind string
		// Name of the owner resource
		ownerName string
		// UID of the owner resource (empty string uses default test UID)
		ownerUID string
		// Expected UID in the owner reference
		expectedUID types.UID
	}{
		{
			// Custom owner with explicit UID
			name:        "custom owner with uid",
			ownerKind:   "PodGangSet",
			ownerName:   "test-pgs",
			ownerUID:    "custom-uid-123",
			expectedUID: "custom-uid-123",
		},
		{
			// Custom owner with default UID
			name:        "custom owner with default uid",
			ownerKind:   "Deployment",
			ownerName:   "test-deployment",
			ownerUID:    "",
			expectedUID: "test-uid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)
			originalOwnerCount := len(builder.pcsg.OwnerReferences)

			// Apply custom owner reference
			result := builder.WithOwnerReference(tt.ownerKind, tt.ownerName, tt.ownerUID)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify owner reference was added
			assert.Len(t, builder.pcsg.OwnerReferences, originalOwnerCount+1)

			// Find the new owner reference
			var newOwnerRef *metav1.OwnerReference
			for i := range builder.pcsg.OwnerReferences {
				if builder.pcsg.OwnerReferences[i].Kind == tt.ownerKind {
					newOwnerRef = &builder.pcsg.OwnerReferences[i]
					break
				}
			}

			require.NotNil(t, newOwnerRef, "New owner reference should be found")
			assert.Equal(t, tt.ownerKind, newOwnerRef.Kind)
			assert.Equal(t, tt.ownerName, newOwnerRef.Name)
			assert.Equal(t, tt.expectedUID, newOwnerRef.UID)
		})
	}
}

// TestPodCliqueScalingGroupBuilder_WithOptions verifies that option functions
// can be applied to customize the PodCliqueScalingGroup during building.
func TestPodCliqueScalingGroupBuilder_WithOptions(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different option scenarios
		name string
		// Option functions to apply
		options []PCSGOption
		// Function to verify the expected state after applying options
		verifyFunc func(*testing.T, *grovecorev1alpha1.PodCliqueScalingGroup)
	}{
		{
			// No options applied
			name:    "no options",
			options: []PCSGOption{},
			verifyFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				// Should have default state
				assert.Empty(t, pcsg.Status.Conditions)
				assert.Equal(t, int32(0), pcsg.Status.AvailableReplicas)
				assert.Nil(t, pcsg.Status.ObservedGeneration)
			},
		},
		{
			// Single option applied
			name:    "single option",
			options: []PCSGOption{WithPCSGMinAvailableBreached()},
			verifyFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				// Should have condition set by WithPCSGMinAvailableBreached
				assert.Len(t, pcsg.Status.Conditions, 1)
				condition := pcsg.Status.Conditions[0]
				assert.Equal(t, grovecorev1alpha1.ConditionTypeMinAvailableBreached, condition.Type)
				assert.Equal(t, metav1.ConditionTrue, condition.Status)
				assert.Equal(t, int32(0), pcsg.Status.AvailableReplicas) // 1 - 1 = 0
			},
		},
		{
			// Multiple options applied
			name: "multiple options",
			options: []PCSGOption{
				WithPCSGMinAvailableBreached(),
				WithPCSGObservedGeneration(5),
				WithPCSGAvailableReplicas(2),
			},
			verifyFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				// Should have condition from first option
				assert.Len(t, pcsg.Status.Conditions, 1)
				condition := pcsg.Status.Conditions[0]
				assert.Equal(t, grovecorev1alpha1.ConditionTypeMinAvailableBreached, condition.Type)
				assert.Equal(t, metav1.ConditionTrue, condition.Status)

				// Should have ObservedGeneration from second option
				require.NotNil(t, pcsg.Status.ObservedGeneration)
				assert.Equal(t, int64(5), *pcsg.Status.ObservedGeneration)

				// Should have AvailableReplicas overridden by third option
				assert.Equal(t, int32(2), pcsg.Status.AvailableReplicas)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)

			// Apply options
			result := builder.WithOptions(tt.options...)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Build and verify the result
			pcsg := builder.Build()
			tt.verifyFunc(t, pcsg)
		})
	}
}

// TestPodCliqueScalingGroupBuilder_Build verifies that the Build method returns
// a properly configured PodCliqueScalingGroup.
func TestPodCliqueScalingGroupBuilder_Build(t *testing.T) {
	builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)

	// Customize the builder
	pcsg := builder.
		WithReplicas(5).
		WithMinAvailable(3).
		WithCliqueNames([]string{"worker", "server"}).
		WithLabels(map[string]string{"custom": "label"}).
		WithOwnerReference("PodGangSet", "test-pgs", "pgs-uid").
		Build()

	// Verify the built PodCliqueScalingGroup
	require.NotNil(t, pcsg)

	// Verify basic metadata
	assert.Equal(t, "test-pcsg", pcsg.Name)
	assert.Equal(t, "default", pcsg.Namespace)

	// Verify spec configuration
	assert.Equal(t, int32(5), pcsg.Spec.Replicas)
	require.NotNil(t, pcsg.Spec.MinAvailable)
	assert.Equal(t, int32(3), *pcsg.Spec.MinAvailable)
	assert.Equal(t, []string{"worker", "server"}, pcsg.Spec.CliqueNames)

	// Verify custom labels were applied
	assert.Equal(t, "label", pcsg.Labels["custom"])

	// Verify owner reference was added
	assert.Len(t, pcsg.OwnerReferences, 1)
	ownerRef := pcsg.OwnerReferences[0]
	assert.Equal(t, "PodGangSet", ownerRef.Kind)
	assert.Equal(t, "test-pgs", ownerRef.Name)
	assert.Equal(t, types.UID("pgs-uid"), ownerRef.UID)
}

// TestPodCliqueScalingGroupBuilder_Chaining verifies that all builder methods
// can be chained together fluently.
func TestPodCliqueScalingGroupBuilder_Chaining(t *testing.T) {
	// This test verifies the fluent interface works correctly
	pcsg := NewPodCliqueScalingGroupBuilder("workers", "production", "prod-pgs", 1).
		WithReplicas(10).
		WithMinAvailable(7).
		WithCliqueNames([]string{"worker-a", "worker-b", "worker-c"}).
		WithLabels(map[string]string{
			"environment": "production",
			"team":        "platform",
		}).
		WithOwnerReference("PodGangSet", "prod-pgs", "prod-pgs-uid").
		WithOptions(
			WithPCSGObservedGeneration(3),
			WithPCSGAvailableReplicas(8),
		).
		Build()

	// Verify all configurations were applied
	assert.Equal(t, "workers", pcsg.Name)
	assert.Equal(t, "production", pcsg.Namespace)
	assert.Equal(t, int32(10), pcsg.Spec.Replicas)
	assert.Equal(t, int32(7), *pcsg.Spec.MinAvailable)
	assert.Equal(t, []string{"worker-a", "worker-b", "worker-c"}, pcsg.Spec.CliqueNames)
	assert.Equal(t, "production", pcsg.Labels["environment"])
	assert.Equal(t, "platform", pcsg.Labels["team"])
	assert.Len(t, pcsg.OwnerReferences, 1)
	assert.Equal(t, int64(3), *pcsg.Status.ObservedGeneration)
	assert.Equal(t, int32(8), pcsg.Status.AvailableReplicas)
}

// TestPodCliqueScalingGroupBuilder_DefaultLabels verifies that default labels
// are properly set during initialization.
func TestPodCliqueScalingGroupBuilder_DefaultLabels(t *testing.T) {
	pgsName := "test-pgs"
	replicaIndex := 2
	builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", pgsName, replicaIndex)

	// Verify all expected default labels are present
	expectedLabels := map[string]string{
		grovecorev1alpha1.LabelManagedByKey:           grovecorev1alpha1.LabelManagedByValue,
		grovecorev1alpha1.LabelPartOfKey:              pgsName,
		grovecorev1alpha1.LabelPodGangSetReplicaIndex: strconv.Itoa(replicaIndex),
	}

	for key, expectedValue := range expectedLabels {
		actualValue, exists := builder.pcsg.Labels[key]
		assert.True(t, exists, "Label %s should exist", key)
		assert.Equal(t, expectedValue, actualValue, "Label %s should have correct value", key)
	}
}

// TestPodCliqueScalingGroupBuilder_EmptyLabelsHandling verifies that the builder
// properly handles cases where labels map is nil or empty.
func TestPodCliqueScalingGroupBuilder_EmptyLabelsHandling(t *testing.T) {
	builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)

	// Initially labels should not be nil
	require.NotNil(t, builder.pcsg.Labels)

	// Test adding to nil labels (simulate edge case)
	builder.pcsg.Labels = nil
	result := builder.WithLabels(map[string]string{"test": "value"})

	// Verify builder is returned and labels are properly initialized
	assert.Equal(t, builder, result)
	require.NotNil(t, builder.pcsg.Labels)
	assert.Equal(t, "value", builder.pcsg.Labels["test"])
}

// TestPodCliqueScalingGroupBuilder_MultipleOwnerReferences verifies that
// multiple owner references can be added.
func TestPodCliqueScalingGroupBuilder_MultipleOwnerReferences(t *testing.T) {
	builder := NewPodCliqueScalingGroupBuilder("test-pcsg", "default", "test-pgs", 0)

	// Add multiple owner references
	pcsg := builder.
		WithOwnerReference("PodGangSet", "pgs-1", "uid-1").
		WithOwnerReference("Deployment", "deploy-1", "uid-2").
		WithOwnerReference("StatefulSet", "sts-1", "uid-3").
		Build()

	// Verify all owner references were added
	assert.Len(t, pcsg.OwnerReferences, 3)

	// Verify each owner reference
	ownerKinds := make(map[string]string)
	for _, ownerRef := range pcsg.OwnerReferences {
		ownerKinds[ownerRef.Kind] = ownerRef.Name
	}

	assert.Equal(t, "pgs-1", ownerKinds["PodGangSet"])
	assert.Equal(t, "deploy-1", ownerKinds["Deployment"])
	assert.Equal(t, "sts-1", ownerKinds["StatefulSet"])
}
