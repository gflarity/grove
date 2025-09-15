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

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// PodCliqueBuilder is a builder for creating PodClique objects.
// This should primarily be used for tests.
type PodCliqueBuilder struct {
	pgsName         string                       // Name of the parent PodGangSet
	pgsReplicaIndex int32                        // Replica index within the PodGangSet
	pclq            *grovecorev1alpha1.PodClique // The PodClique being built
}

// NewPodCliqueBuilder creates a new PodCliqueBuilder for a PodGangSet-managed PodClique.
// It initializes a builder with default values and standard owner references.
func NewPodCliqueBuilder(pgsName string, pgsUID types.UID, pclqTemplateName, namespace string, pgsReplicaIndex int32) *PodCliqueBuilder {
	return &PodCliqueBuilder{
		pgsName:         pgsName,
		pgsReplicaIndex: pgsReplicaIndex,
		pclq:            createDefaultPodCliqueWithoutPodSpec(pgsName, pgsUID, pclqTemplateName, namespace, pgsReplicaIndex),
	}
}

// NewPCSGPodCliqueBuilder creates a PodClique builder for a PodCliqueScalingGroup-managed PodClique.
// It sets up appropriate labels and defaults for scaling group membership.
func NewPCSGPodCliqueBuilder(name, namespace, pgsName, pcsgName string, pgsReplicaIndex, pcsgReplicaIndex int) *PodCliqueBuilder {
	pclq := &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				grovecorev1alpha1.LabelManagedByKey:                      grovecorev1alpha1.LabelManagedByValue,
				grovecorev1alpha1.LabelPartOfKey:                         pgsName,
				grovecorev1alpha1.LabelPodCliqueScalingGroup:             pcsgName,
				grovecorev1alpha1.LabelComponentKey:                      grovecorev1alpha1.LabelComponentPCSGPodCliqueValue,
				grovecorev1alpha1.LabelPodGangSetReplicaIndex:            strconv.Itoa(pgsReplicaIndex),
				grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: strconv.Itoa(pcsgReplicaIndex),
			},
		},
		Spec: grovecorev1alpha1.PodCliqueSpec{
			Replicas:     1,
			MinAvailable: ptr.To(int32(1)),
		},
		Status: grovecorev1alpha1.PodCliqueStatus{},
	}

	return &PodCliqueBuilder{
		pgsName:         pgsName,
		pgsReplicaIndex: int32(pgsReplicaIndex),
		pclq:            pclq,
	}
}

// WithLabels merges the provided labels with the default labels.
// If there are conflicts, the provided labels take precedence over default labels.
func (b *PodCliqueBuilder) WithLabels(labels map[string]string) *PodCliqueBuilder {
	b.pclq.Labels = lo.Assign(b.pclq.Labels, labels)
	return b
}

// WithReplicas sets the desired number of replicas for the PodClique.
// The default value is 1 if not specified.
func (b *PodCliqueBuilder) WithReplicas(replicas int32) *PodCliqueBuilder {
	b.pclq.Spec.Replicas = replicas
	return b
}

// WithStartsAfter specifies the PodClique dependencies that must be ready before this one starts.
// It takes template names and generates the corresponding PodClique names based on the PodGangSet context.
func (b *PodCliqueBuilder) WithStartsAfter(pclqTemplateNames []string) *PodCliqueBuilder {
	pclqDependencies := lo.Map(pclqTemplateNames, func(pclqTemplateName string, _ int) string {
		return grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{Name: b.pgsName, Replica: int(b.pgsReplicaIndex)}, pclqTemplateName)
	})
	b.pclq.Spec.StartsAfter = pclqDependencies
	return b
}

// WithAutoScaleMaxReplicas configures the maximum number of replicas for auto-scaling.
// This sets up the ScaleConfig with the specified upper bound.
func (b *PodCliqueBuilder) WithAutoScaleMaxReplicas(maximum int32) *PodCliqueBuilder {
	b.pclq.Spec.ScaleConfig = &grovecorev1alpha1.AutoScalingConfig{
		MaxReplicas: maximum,
	}
	return b
}

// WithOwnerReference sets a custom owner reference for the PodClique.
// If uid is empty, a test-specific default UID will be used.
func (b *PodCliqueBuilder) WithOwnerReference(kind, name, uid string) *PodCliqueBuilder {
	ownerRef := metav1.OwnerReference{
		Kind: kind,
		Name: name,
		UID:  types.UID("test-uid"),
	}
	if uid != "" {
		ownerRef.UID = types.UID(uid)
	}
	b.pclq.OwnerReferences = append(b.pclq.OwnerReferences, ownerRef)
	return b
}

// WithOptions applies a series of option functions to customize the PodClique.
// This allows for flexible configuration using functional options pattern.
func (b *PodCliqueBuilder) WithOptions(opts ...PCLQOption) *PodCliqueBuilder {
	for _, opt := range opts {
		opt(b.pclq)
	}
	return b
}

// Build finalizes and returns the constructed PodClique object.
// It ensures a default PodSpec is set before returning.
func (b *PodCliqueBuilder) Build() *grovecorev1alpha1.PodClique {
	_ = b.withDefaultPodSpec()
	return b.pclq
}

// withDefaultPodSpec sets a default pod specification for the PodClique.
// It uses the default PodBuilder to create a basic pod spec.
func (b *PodCliqueBuilder) withDefaultPodSpec() *PodCliqueBuilder {
	b.pclq.Spec.PodSpec = *NewPodBuilder().Build()
	return b
}

// createDefaultPodCliqueWithoutPodSpec creates a new PodClique with standard metadata and owner references.
// The PodSpec is intentionally left empty and should be set separately.
func createDefaultPodCliqueWithoutPodSpec(pgsName string, pgsUID types.UID, pclqTemplateName, namespace string, pgsReplicaIndex int32) *grovecorev1alpha1.PodClique {
	pclqName := grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{Name: pgsName, Replica: int(pgsReplicaIndex)}, pclqTemplateName)
	return &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pclqName,
			Namespace: namespace,
			Labels:    getDefaultLabels(pgsName, pclqName, pgsReplicaIndex),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         grovecorev1alpha1.SchemeGroupVersion.String(),
					Kind:               grovecorev1alpha1.PodGangSetKind,
					Name:               pgsName,
					UID:                pgsUID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: grovecorev1alpha1.PodCliqueSpec{
			Replicas:     1,
			MinAvailable: ptr.To(int32(1)),
		},
	}
}

// getDefaultLabels returns the standard set of labels for a PodClique resource.
// It combines PodGangSet managed resource labels with PodClique-specific component labels.
func getDefaultLabels(pgsName, pclqName string, pgsReplicaIndex int32) map[string]string {
	pclqComponentLabels := map[string]string{
		grovecorev1alpha1.LabelAppNameKey:             pclqName,
		grovecorev1alpha1.LabelComponentKey:           grovecorev1alpha1.LabelComponentPGSPodCliqueValue,
		grovecorev1alpha1.LabelPodGangSetReplicaIndex: strconv.Itoa(int(pgsReplicaIndex)),
	}
	return lo.Assign(
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		pclqComponentLabels,
	)
}
