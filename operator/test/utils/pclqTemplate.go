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
)

// Package utils provides test utilities for Grove operator components.

// PodCliqueTemplateSpecBuilder implements a fluent builder pattern for constructing
// PodCliqueTemplateSpec objects with a chainable API.
type PodCliqueTemplateSpecBuilder struct {
	pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec
}

// NewPodCliqueTemplateSpecBuilder creates a new builder instance with the given name
// and default configuration (1 replica).
func NewPodCliqueTemplateSpecBuilder(name string) *PodCliqueTemplateSpecBuilder {
	return &PodCliqueTemplateSpecBuilder{
		pclqTemplateSpec: createDefaultPodCliqueTemplateSpec(name),
	}
}

// WithReplicas sets the desired number of pod replicas in the PodClique.
// A replica count of 0 means the PodClique is effectively disabled.
func (b *PodCliqueTemplateSpecBuilder) WithReplicas(replicas int32) *PodCliqueTemplateSpecBuilder {
	b.pclqTemplateSpec.Spec.Replicas = replicas
	return b
}

// WithLabels sets Kubernetes labels that will be applied to the PodClique and its pods.
func (b *PodCliqueTemplateSpecBuilder) WithLabels(labels map[string]string) *PodCliqueTemplateSpecBuilder {
	b.pclqTemplateSpec.Labels = labels
	return b
}

// WithStartsAfter defines dependencies by specifying names of PodCliques that must be ready
// before this PodClique can start.
func (b *PodCliqueTemplateSpecBuilder) WithStartsAfter(startsAfter []string) *PodCliqueTemplateSpecBuilder {
	b.pclqTemplateSpec.Spec.StartsAfter = startsAfter
	return b
}

// WithMinAvailable sets the minimum number of pods that must be available for the
// PodClique to be considered ready.
func (b *PodCliqueTemplateSpecBuilder) WithMinAvailable(minAvailable int32) *PodCliqueTemplateSpecBuilder {
	b.pclqTemplateSpec.Spec.MinAvailable = &minAvailable
	return b
}

// WithScaleConfig configures autoscaling bounds for the PodClique by setting both
// minimum and maximum replica limits in a single call.
func (b *PodCliqueTemplateSpecBuilder) WithScaleConfig(minReplicas *int32, maxReplicas int32) *PodCliqueTemplateSpecBuilder {
	b.pclqTemplateSpec.Spec.ScaleConfig = &grovecorev1alpha1.AutoScalingConfig{
		MinReplicas: minReplicas,
		MaxReplicas: maxReplicas,
	}
	return b
}

// WithAutoScaleMinReplicas sets the lower bound for autoscaling. If ScaleConfig
// doesn't exist, it will be created.
func (b *PodCliqueTemplateSpecBuilder) WithAutoScaleMinReplicas(minimum int32) *PodCliqueTemplateSpecBuilder {
	if b.pclqTemplateSpec.Spec.ScaleConfig == nil {
		b.pclqTemplateSpec.Spec.ScaleConfig = &grovecorev1alpha1.AutoScalingConfig{}
	}
	b.pclqTemplateSpec.Spec.ScaleConfig.MinReplicas = &minimum
	return b
}

// WithAutoScaleMaxReplicas sets the upper bound for autoscaling. If ScaleConfig
// doesn't exist, it will be created.
func (b *PodCliqueTemplateSpecBuilder) WithAutoScaleMaxReplicas(maximum int32) *PodCliqueTemplateSpecBuilder {
	if b.pclqTemplateSpec.Spec.ScaleConfig == nil {
		b.pclqTemplateSpec.Spec.ScaleConfig = &grovecorev1alpha1.AutoScalingConfig{}
	}
	b.pclqTemplateSpec.Spec.ScaleConfig.MaxReplicas = maximum
	return b
}

// Build finalizes and returns the constructed PodCliqueTemplateSpec with default
// PodSpec configuration.
func (b *PodCliqueTemplateSpecBuilder) Build() *grovecorev1alpha1.PodCliqueTemplateSpec {
	b.withDefaultPodSpec()
	return b.pclqTemplateSpec
}

// withDefaultPodSpec ensures the PodCliqueTemplateSpec has a basic PodSpec configuration.
func (b *PodCliqueTemplateSpecBuilder) withDefaultPodSpec() *PodCliqueTemplateSpecBuilder {
	b.pclqTemplateSpec.Spec.PodSpec = *NewPodBuilder().Build()
	return b
}

// createDefaultPodCliqueTemplateSpec initializes a new PodCliqueTemplateSpec with the given
// name and default settings (1 replica).
func createDefaultPodCliqueTemplateSpec(name string) *grovecorev1alpha1.PodCliqueTemplateSpec {
	return &grovecorev1alpha1.PodCliqueTemplateSpec{
		Name: name,
		Spec: grovecorev1alpha1.PodCliqueSpec{
			Replicas: 1,
		},
	}
}
