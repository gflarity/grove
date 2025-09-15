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
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Package utils provides test utilities for Grove operator testing.

// PodGangSetBuilder implements a fluent builder pattern for constructing PodGangSet objects.
type PodGangSetBuilder struct {
	pgs *grovecorev1alpha1.PodGangSet
}

// NewPodGangSetBuilder creates a new PodGangSetBuilder with the given name and namespace.
func NewPodGangSetBuilder(name, namespace string) *PodGangSetBuilder {
	return &PodGangSetBuilder{
		pgs: createEmptyPodGangSet(name, namespace),
	}
}

// WithCliqueStartupType configures the startup behavior for all cliques in the PodGangSet.
// The startupType parameter determines the order and conditions for clique initialization.
func (b *PodGangSetBuilder) WithCliqueStartupType(startupType *grovecorev1alpha1.CliqueStartupType) *PodGangSetBuilder {
	b.pgs.Spec.Template.StartupType = startupType
	return b
}

// WithReplicas sets the desired number of replicas for the entire PodGangSet.
// This value acts as a global scaling factor for all contained cliques.
func (b *PodGangSetBuilder) WithReplicas(replicas int32) *PodGangSetBuilder {
	b.pgs.Spec.Replicas = replicas
	return b
}

// WithPodCliqueParameters adds a new PodClique to the PodGangSet with the specified configuration.
// It creates a PodCliqueTemplateSpec with the given name, replicas, and startup dependencies.
func (b *PodGangSetBuilder) WithPodCliqueParameters(name string, replicas int32, startsAfter []string) *PodGangSetBuilder {
	pclqTemplateSpec := NewPodCliqueTemplateSpecBuilder(name).
		WithReplicas(replicas).
		WithStartsAfter(startsAfter).
		Build()
	return b.WithPodCliqueTemplateSpec(pclqTemplateSpec)
}

// WithPodCliqueTemplateSpec adds a pre-configured PodCliqueTemplateSpec to the PodGangSet.
// This method allows direct addition of a PodCliqueTemplateSpec created using the PodCliqueBuilder.
func (b *PodGangSetBuilder) WithPodCliqueTemplateSpec(pclq *grovecorev1alpha1.PodCliqueTemplateSpec) *PodGangSetBuilder {
	b.pgs.Spec.Template.Cliques = append(b.pgs.Spec.Template.Cliques, pclq)
	return b
}

// WithPodCliqueScalingGroupConfig adds a scaling group configuration to the PodGangSet.
// Scaling groups define how multiple cliques should scale together.
func (b *PodGangSetBuilder) WithPodCliqueScalingGroupConfig(config grovecorev1alpha1.PodCliqueScalingGroupConfig) *PodGangSetBuilder {
	b.pgs.Spec.Template.PodCliqueScalingGroupConfigs = append(b.pgs.Spec.Template.PodCliqueScalingGroupConfigs, config)
	return b
}

// WithStandaloneClique adds a single-replica clique that operates independently of any scaling group.
func (b *PodGangSetBuilder) WithStandaloneClique(name string) *PodGangSetBuilder {
	cliqueSpec := &grovecorev1alpha1.PodCliqueTemplateSpec{
		Name: name,
		Spec: grovecorev1alpha1.PodCliqueSpec{
			Replicas: 1,
		},
	}
	b.pgs.Spec.Template.Cliques = append(b.pgs.Spec.Template.Cliques, cliqueSpec)
	return b
}

// WithStandaloneCliqueReplicas adds a standalone clique with a custom number of replicas.
// The clique operates independently of any scaling group with the specified replica count.
func (b *PodGangSetBuilder) WithStandaloneCliqueReplicas(name string, replicas int32) *PodGangSetBuilder {
	cliqueSpec := &grovecorev1alpha1.PodCliqueTemplateSpec{
		Name: name,
		Spec: grovecorev1alpha1.PodCliqueSpec{
			Replicas: replicas,
		},
	}
	b.pgs.Spec.Template.Cliques = append(b.pgs.Spec.Template.Cliques, cliqueSpec)
	return b
}

// WithScalingGroup adds a scaling group with default configuration (replicas=1, minAvailable=1).
// The scaling group will manage the specified cliques as a single unit.
func (b *PodGangSetBuilder) WithScalingGroup(name string, cliqueNames []string) *PodGangSetBuilder {
	return b.WithScalingGroupConfig(name, cliqueNames, 1, 1)
}

// WithScalingGroupConfig adds a scaling group with custom configuration.
// The scaling group manages the specified cliques with custom replica count and minimum availability requirements.
func (b *PodGangSetBuilder) WithScalingGroupConfig(name string, cliqueNames []string, replicas, minAvailable int32) *PodGangSetBuilder {
	// Create and add single-replica cliques that will be managed by this scaling group
	for _, cliqueName := range cliqueNames {
		cliqueSpec := &grovecorev1alpha1.PodCliqueTemplateSpec{
			Name: cliqueName,
			Spec: grovecorev1alpha1.PodCliqueSpec{
				Replicas: 1,
			},
		}
		b.pgs.Spec.Template.Cliques = append(b.pgs.Spec.Template.Cliques, cliqueSpec)
	}

	// Add scaling group config
	pcsgConfig := grovecorev1alpha1.PodCliqueScalingGroupConfig{
		Name:         name,
		CliqueNames:  cliqueNames,
		Replicas:     &replicas,
		MinAvailable: &minAvailable,
	}
	b.pgs.Spec.Template.PodCliqueScalingGroupConfigs = append(b.pgs.Spec.Template.PodCliqueScalingGroupConfigs, pcsgConfig)
	return b
}

// Build finalizes and returns the constructed PodGangSet object.
func (b *PodGangSetBuilder) Build() *grovecorev1alpha1.PodGangSet {
	return b.pgs
}

// createEmptyPodGangSet initializes a new PodGangSet with basic metadata and default replica count.
func createEmptyPodGangSet(name, namespace string) *grovecorev1alpha1.PodGangSet {
	return &grovecorev1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(uuid.NewString()),
		},
		Spec: grovecorev1alpha1.PodGangSetSpec{
			Replicas: 1,
		},
	}
}
