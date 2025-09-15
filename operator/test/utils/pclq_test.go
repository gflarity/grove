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

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestNewPodCliqueBuilder verifies that a new PodCliqueBuilder is properly initialized
// with correct default values and owner references for PodGangSet-managed PodCliques.
func TestNewPodCliqueBuilder(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different builder initialization scenarios
		name string
		// Name of the parent PodGangSet
		pgsName string
		// UID of the parent PodGangSet
		pgsUID types.UID
		// Name of the PodClique template
		pclqTemplateName string
		// Namespace for the PodClique
		namespace string
		// Replica index within the PodGangSet
		pgsReplicaIndex int32
	}{
		{
			// Standard PodClique creation scenario
			name:             "standard podclique",
			pgsName:          "test-pgs",
			pgsUID:           "test-uid-123",
			pclqTemplateName: "worker",
			namespace:        "default",
			pgsReplicaIndex:  0,
		},
		{
			// PodClique with different replica index
			name:             "different replica index",
			pgsName:          "multi-replica-pgs",
			pgsUID:           "uid-456",
			pclqTemplateName: "server",
			namespace:        "production",
			pgsReplicaIndex:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueBuilder(tt.pgsName, tt.pgsUID, tt.pclqTemplateName, tt.namespace, tt.pgsReplicaIndex)

			require.NotNil(t, builder)
			assert.Equal(t, tt.pgsName, builder.pgsName)
			assert.Equal(t, tt.pgsReplicaIndex, builder.pgsReplicaIndex)
			require.NotNil(t, builder.pclq)

			// Verify generated name
			expectedName := apicommon.GeneratePodCliqueName(
				apicommon.ResourceNameReplica{Name: tt.pgsName, Replica: int(tt.pgsReplicaIndex)},
				tt.pclqTemplateName,
			)
			assert.Equal(t, expectedName, builder.pclq.Name)
			assert.Equal(t, tt.namespace, builder.pclq.Namespace)

			// Verify owner reference
			require.Len(t, builder.pclq.OwnerReferences, 1)
			ownerRef := builder.pclq.OwnerReferences[0]
			assert.Equal(t, grovecorev1alpha1.SchemeGroupVersion.String(), ownerRef.APIVersion)
			assert.Equal(t, constants.KindPodGangSet, ownerRef.Kind)
			assert.Equal(t, tt.pgsName, ownerRef.Name)
			assert.Equal(t, tt.pgsUID, ownerRef.UID)
			assert.True(t, *ownerRef.Controller)
			assert.True(t, *ownerRef.BlockOwnerDeletion)

			// Verify default spec values
			assert.Equal(t, int32(1), builder.pclq.Spec.Replicas)
			require.NotNil(t, builder.pclq.Spec.MinAvailable)
			assert.Equal(t, int32(1), *builder.pclq.Spec.MinAvailable)

			// Verify default labels
			expectedLabels := map[string]string{
				apicommon.LabelManagedByKey:           apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:              tt.pgsName,
				apicommon.LabelAppNameKey:             expectedName,
				apicommon.LabelComponentKey:           apicommon.LabelComponentNamePodGangSetPodClique,
				apicommon.LabelPodGangSetReplicaIndex: strconv.Itoa(int(tt.pgsReplicaIndex)),
			}
			for key, expectedValue := range expectedLabels {
				assert.Equal(t, expectedValue, builder.pclq.Labels[key], "Label %s should match", key)
			}
		})
	}
}

// TestNewPCSGPodCliqueBuilder verifies that a PodClique builder for PodCliqueScalingGroup
// is properly initialized with correct labels and configuration.
func TestNewPCSGPodCliqueBuilder(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different PCSG PodClique scenarios
		name string
		// Name of the PodClique
		pclqName string
		// Namespace for the PodClique
		namespace string
		// Name of the parent PodGangSet
		pgsName string
		// Name of the PodCliqueScalingGroup
		pcsgName string
		// Replica index within the PodGangSet
		pgsReplicaIndex int
		// Replica index within the PodCliqueScalingGroup
		pcsgReplicaIndex int
	}{
		{
			// Standard PCSG PodClique creation
			name:             "standard pcsg podclique",
			pclqName:         "worker-0-0",
			namespace:        "default",
			pgsName:          "test-pgs",
			pcsgName:         "workers",
			pgsReplicaIndex:  0,
			pcsgReplicaIndex: 0,
		},
		{
			// PCSG PodClique with different indices
			name:             "different indices",
			pclqName:         "server-1-2",
			namespace:        "production",
			pgsName:          "prod-pgs",
			pcsgName:         "servers",
			pgsReplicaIndex:  1,
			pcsgReplicaIndex: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPCSGPodCliqueBuilder(tt.pclqName, tt.namespace, tt.pgsName, tt.pcsgName, tt.pgsReplicaIndex, tt.pcsgReplicaIndex)

			require.NotNil(t, builder)
			assert.Equal(t, tt.pgsName, builder.pgsName)
			assert.Equal(t, int32(tt.pgsReplicaIndex), builder.pgsReplicaIndex)
			require.NotNil(t, builder.pclq)

			// Verify basic metadata
			assert.Equal(t, tt.pclqName, builder.pclq.Name)
			assert.Equal(t, tt.namespace, builder.pclq.Namespace)

			// Verify PCSG-specific labels
			expectedLabels := map[string]string{
				apicommon.LabelManagedByKey:                      apicommon.LabelManagedByValue,
				apicommon.LabelPartOfKey:                         tt.pgsName,
				apicommon.LabelPodCliqueScalingGroup:             tt.pcsgName,
				apicommon.LabelComponentKey:                      apicommon.LabelComponentNamePodCliqueScalingGroupPodClique,
				apicommon.LabelPodGangSetReplicaIndex:            strconv.Itoa(tt.pgsReplicaIndex),
				apicommon.LabelPodCliqueScalingGroupReplicaIndex: strconv.Itoa(tt.pcsgReplicaIndex),
			}
			for key, expectedValue := range expectedLabels {
				assert.Equal(t, expectedValue, builder.pclq.Labels[key], "Label %s should match", key)
			}

			// Verify default spec values
			assert.Equal(t, int32(1), builder.pclq.Spec.Replicas)
			require.NotNil(t, builder.pclq.Spec.MinAvailable)
			assert.Equal(t, int32(1), *builder.pclq.Spec.MinAvailable)
		})
	}
}

// TestPodCliqueBuilder_WithLabels verifies that custom labels can be added
// and merged with default labels.
func TestPodCliqueBuilder_WithLabels(t *testing.T) {
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
				apicommon.LabelComponentKey: "custom-component",
				"custom-override":           "value",
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
			builder := NewPodCliqueBuilder("test-pgs", "test-uid", "worker", "default", 0)
			originalLabels := make(map[string]string)
			for k, v := range builder.pclq.Labels {
				originalLabels[k] = v
			}

			// Apply custom labels
			result := builder.WithLabels(tt.customLabels)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify custom labels were added
			for key, expectedValue := range tt.customLabels {
				assert.Equal(t, expectedValue, builder.pclq.Labels[key], "Custom label %s should be set", key)
			}

			// Verify original labels are preserved (unless overridden)
			for key, originalValue := range originalLabels {
				if _, overridden := tt.customLabels[key]; !overridden {
					assert.Equal(t, originalValue, builder.pclq.Labels[key], "Original label %s should be preserved", key)
				}
			}
		})
	}
}

// TestPodCliqueBuilder_WithReplicas verifies that the replica count can be customized.
func TestPodCliqueBuilder_WithReplicas(t *testing.T) {
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
			builder := NewPodCliqueBuilder("test-pgs", "test-uid", "worker", "default", 0)

			// Apply replica count
			result := builder.WithReplicas(tt.replicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify replica count was set
			assert.Equal(t, tt.replicas, builder.pclq.Spec.Replicas)
		})
	}
}

// TestPodCliqueBuilder_WithStartsAfter verifies that dependencies can be configured
// and template names are properly converted to PodClique names.
func TestPodCliqueBuilder_WithStartsAfter(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different dependency scenarios
		name string
		// Template names that this PodClique should start after
		pclqTemplateNames []string
		// Expected number of dependencies
		expectedDependencyCount int
	}{
		{
			// No dependencies
			name:                    "no dependencies",
			pclqTemplateNames:       []string{},
			expectedDependencyCount: 0,
		},
		{
			// Single dependency
			name:                    "single dependency",
			pclqTemplateNames:       []string{"database"},
			expectedDependencyCount: 1,
		},
		{
			// Multiple dependencies
			name:                    "multiple dependencies",
			pclqTemplateNames:       []string{"database", "cache", "message-queue"},
			expectedDependencyCount: 3,
		},
		{
			// Nil dependencies
			name:                    "nil dependencies",
			pclqTemplateNames:       nil,
			expectedDependencyCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgsName := "test-pgs"
			replicaIndex := int32(0)
			builder := NewPodCliqueBuilder(pgsName, "test-uid", "worker", "default", replicaIndex)

			// Apply dependencies
			result := builder.WithStartsAfter(tt.pclqTemplateNames)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify dependency count
			assert.Len(t, builder.pclq.Spec.StartsAfter, tt.expectedDependencyCount)

			// Verify dependency names are properly generated
			for i, templateName := range tt.pclqTemplateNames {
				expectedName := apicommon.GeneratePodCliqueName(
					apicommon.ResourceNameReplica{Name: pgsName, Replica: int(replicaIndex)},
					templateName,
				)
				assert.Equal(t, expectedName, builder.pclq.Spec.StartsAfter[i])
			}
		})
	}
}

// TestPodCliqueBuilder_WithAutoScaleMaxReplicas verifies that autoscaling
// configuration can be set with maximum replica limits.
func TestPodCliqueBuilder_WithAutoScaleMaxReplicas(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different autoscaling scenarios
		name string
		// Maximum number of replicas for autoscaling
		maxReplicas int32
	}{
		{
			// Small maximum
			name:        "small maximum",
			maxReplicas: 3,
		},
		{
			// Large maximum
			name:        "large maximum",
			maxReplicas: 50,
		},
		{
			// Minimum valid maximum (should be >= 1)
			name:        "minimum maximum",
			maxReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueBuilder("test-pgs", "test-uid", "worker", "default", 0)

			// Apply autoscaling configuration
			result := builder.WithAutoScaleMaxReplicas(tt.maxReplicas)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify autoscaling configuration was set
			require.NotNil(t, builder.pclq.Spec.ScaleConfig)
			assert.Equal(t, tt.maxReplicas, builder.pclq.Spec.ScaleConfig.MaxReplicas)
			assert.Nil(t, builder.pclq.Spec.ScaleConfig.MinReplicas) // Should not be set by this method
		})
	}
}

// TestPodCliqueBuilder_WithOwnerReference verifies that custom owner references
// can be added to the PodClique.
func TestPodCliqueBuilder_WithOwnerReference(t *testing.T) {
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
			ownerKind:   "Deployment",
			ownerName:   "test-deployment",
			ownerUID:    "custom-uid-123",
			expectedUID: "custom-uid-123",
		},
		{
			// Custom owner with default UID
			name:        "custom owner with default uid",
			ownerKind:   "StatefulSet",
			ownerName:   "test-statefulset",
			ownerUID:    "",
			expectedUID: "test-uid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueBuilder("test-pgs", "original-uid", "worker", "default", 0)
			originalOwnerCount := len(builder.pclq.OwnerReferences)

			// Apply custom owner reference
			result := builder.WithOwnerReference(tt.ownerKind, tt.ownerName, types.UID(tt.ownerUID))

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Verify owner reference was added
			assert.Len(t, builder.pclq.OwnerReferences, originalOwnerCount+1)

			// Find the new owner reference
			var newOwnerRef *metav1.OwnerReference
			for i := range builder.pclq.OwnerReferences {
				if builder.pclq.OwnerReferences[i].Kind == tt.ownerKind {
					newOwnerRef = &builder.pclq.OwnerReferences[i]
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

// TestPodCliqueBuilder_WithOptions verifies that option functions can be applied
// to customize the PodClique during building.
func TestPodCliqueBuilder_WithOptions(t *testing.T) {
	tests := []struct {
		// Test case description - verifies different option scenarios
		name string
		// Option functions to apply
		options []PCLQOption
		// Function to verify the expected state after applying options
		verifyFunc func(*testing.T, *grovecorev1alpha1.PodClique)
	}{
		{
			// No options applied
			name:    "no options",
			options: []PCLQOption{},
			verifyFunc: func(t *testing.T, pclq *grovecorev1alpha1.PodClique) {
				// Should have default state
				assert.Empty(t, pclq.Status.Conditions)
				assert.Equal(t, int32(0), pclq.Status.ReadyReplicas)
			},
		},
		{
			// Single option applied
			name:    "single option",
			options: []PCLQOption{WithPCLQAvailable()},
			verifyFunc: func(t *testing.T, pclq *grovecorev1alpha1.PodClique) {
				// Should have conditions set by WithPCLQAvailable
				assert.Len(t, pclq.Status.Conditions, 2)
				assert.Equal(t, int32(1), pclq.Status.ReadyReplicas) // Matches spec.replicas
			},
		},
		{
			// Multiple options applied
			name: "multiple options",
			options: []PCLQOption{
				WithPCLQAvailable(),
				WithPCLQReplicaReadyStatus(3),
			},
			verifyFunc: func(t *testing.T, pclq *grovecorev1alpha1.PodClique) {
				// Should have conditions from first option and ReadyReplicas from second
				assert.Len(t, pclq.Status.Conditions, 2)
				assert.Equal(t, int32(3), pclq.Status.ReadyReplicas) // Overridden by second option
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPodCliqueBuilder("test-pgs", "test-uid", "worker", "default", 0)

			// Apply options
			result := builder.WithOptions(tt.options...)

			// Verify builder is returned for chaining
			assert.Equal(t, builder, result)

			// Build and verify the result
			pclq := builder.Build()
			tt.verifyFunc(t, pclq)
		})
	}
}

// TestPodCliqueBuilder_Build verifies that the Build method returns a properly
// configured PodClique with default PodSpec.
func TestPodCliqueBuilder_Build(t *testing.T) {
	builder := NewPodCliqueBuilder("test-pgs", "test-uid", "worker", "default", 0)

	// Customize the builder
	pclq := builder.
		WithReplicas(3).
		WithLabels(map[string]string{"custom": "label"}).
		WithStartsAfter([]string{"database"}).
		WithAutoScaleMaxReplicas(10).
		Build()

	// Verify the built PodClique
	require.NotNil(t, pclq)

	// Verify basic metadata
	assert.Contains(t, pclq.Name, "worker")
	assert.Equal(t, "default", pclq.Namespace)

	// Verify spec configuration
	assert.Equal(t, int32(3), pclq.Spec.Replicas)
	assert.Len(t, pclq.Spec.StartsAfter, 1)
	require.NotNil(t, pclq.Spec.ScaleConfig)
	assert.Equal(t, int32(10), pclq.Spec.ScaleConfig.MaxReplicas)

	// Verify custom labels were applied
	assert.Equal(t, "label", pclq.Labels["custom"])

	// Verify default PodSpec was set
	require.NotNil(t, pclq.Spec.PodSpec.Containers)
	assert.Len(t, pclq.Spec.PodSpec.Containers, 1)
	assert.Equal(t, "test-container", pclq.Spec.PodSpec.Containers[0].Name)
}

// TestPodCliqueBuilder_Chaining verifies that all builder methods can be chained
// together fluently.
func TestPodCliqueBuilder_Chaining(t *testing.T) {
	// This test verifies the fluent interface works correctly
	pclq := NewPodCliqueBuilder("test-pgs", "test-uid", "worker", "default", 0).
		WithReplicas(5).
		WithLabels(map[string]string{"env": "test"}).
		WithStartsAfter([]string{"db", "cache"}).
		WithAutoScaleMaxReplicas(20).
		WithOwnerReference("Deployment", "test-deploy", "deploy-uid").
		WithOptions(WithPCLQAvailable()).
		Build()

	// Verify all configurations were applied
	assert.Equal(t, int32(5), pclq.Spec.Replicas)
	assert.Equal(t, "test", pclq.Labels["env"])
	assert.Len(t, pclq.Spec.StartsAfter, 2)
	assert.Equal(t, int32(20), pclq.Spec.ScaleConfig.MaxReplicas)
	assert.Len(t, pclq.OwnerReferences, 2)               // Original PGS owner + custom owner
	assert.Len(t, pclq.Status.Conditions, 2)             // From WithPCLQAvailable option
	assert.Equal(t, int32(5), pclq.Status.ReadyReplicas) // From WithPCLQAvailable option
}

// TestCreateDefaultPodCliqueWithoutPodSpec verifies the internal helper function
// creates a properly configured PodClique without PodSpec.
func TestCreateDefaultPodCliqueWithoutPodSpec(t *testing.T) {
	pgsName := "test-pgs"
	pgsUID := types.UID("test-uid-123")
	pclqTemplateName := "worker"
	namespace := "test-namespace"
	pgsReplicaIndex := int32(1)

	pclq := createDefaultPodCliqueWithoutPodSpec(pgsName, pgsUID, pclqTemplateName, namespace, pgsReplicaIndex)

	// Verify basic metadata
	expectedName := apicommon.GeneratePodCliqueName(
		apicommon.ResourceNameReplica{Name: pgsName, Replica: int(pgsReplicaIndex)},
		pclqTemplateName,
	)
	assert.Equal(t, expectedName, pclq.Name)
	assert.Equal(t, namespace, pclq.Namespace)

	// Verify owner reference
	require.Len(t, pclq.OwnerReferences, 1)
	ownerRef := pclq.OwnerReferences[0]
	assert.Equal(t, grovecorev1alpha1.SchemeGroupVersion.String(), ownerRef.APIVersion)
	assert.Equal(t, constants.KindPodGangSet, ownerRef.Kind)
	assert.Equal(t, pgsName, ownerRef.Name)
	assert.Equal(t, pgsUID, ownerRef.UID)

	// Verify default spec
	assert.Equal(t, int32(1), pclq.Spec.Replicas)
	require.NotNil(t, pclq.Spec.MinAvailable)
	assert.Equal(t, int32(1), *pclq.Spec.MinAvailable)

	// Verify PodSpec is empty (as expected)
	assert.Empty(t, pclq.Spec.PodSpec.Containers)

	// Verify labels contain expected values
	assert.Equal(t, apicommon.LabelManagedByValue, pclq.Labels[apicommon.LabelManagedByKey])
	assert.Equal(t, pgsName, pclq.Labels[apicommon.LabelPartOfKey])
	assert.Equal(t, expectedName, pclq.Labels[apicommon.LabelAppNameKey])
	assert.Equal(t, strconv.Itoa(int(pgsReplicaIndex)), pclq.Labels[apicommon.LabelPodGangSetReplicaIndex])
}
