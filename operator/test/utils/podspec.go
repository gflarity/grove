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
	corev1 "k8s.io/api/core/v1"
)

// PodSpecBuilder provides a fluent interface for constructing Kubernetes PodSpec objects.
// It encapsulates the creation of PodSpecs with sensible defaults for testing purposes.
type PodSpecBuilder struct {
	podSpec *corev1.PodSpec
}

// NewPodBuilder initializes a new PodSpecBuilder with default configuration.
// The default configuration includes a simple alpine container that sleeps.
func NewPodBuilder() *PodSpecBuilder {
	return &PodSpecBuilder{
		podSpec: createDefaultPodSpec(),
	}
}

// Build returns the final constructed PodSpec object.
// This method should be called after all desired modifications are made.
func (b *PodSpecBuilder) Build() *corev1.PodSpec {
	return b.podSpec
}

// createDefaultPodSpec creates a basic PodSpec with a single alpine container
// configured to sleep for 2 minutes. The pod uses RestartPolicyAlways.
func createDefaultPodSpec() *corev1.PodSpec {
	return &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:    "test-container",
				Image:   "alpine:3.21",
				Command: []string{"/bin/sh", "-c", "sleep 2m"},
			},
		},
		RestartPolicy: corev1.RestartPolicyAlways,
	}
}
