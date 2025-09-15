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

// Package utils provides utility functions for working with PodClique resources.
package utils

import (
	"context"
	"slices"
	"time"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPCLQsByOwner retrieves PodClique objects that are owned by the specified owner kind and object key, and match the provided selector labels.
func GetPCLQsByOwner(ctx context.Context, cl client.Client, ownerKind string, ownerObjectKey client.ObjectKey, selectorLabels map[string]string) ([]grovecorev1alpha1.PodClique, error) {
	// Get all PodCliques matching the selector labels in the namespace
	pclqs, err := GetPCLQsMatchingLabels(ctx, cl, ownerObjectKey.Namespace, selectorLabels)
	if err != nil {
		return pclqs, err
	}

	// Filter PodCliques by owner reference
	filteredPCLQs := lo.Filter(pclqs, func(pclq grovecorev1alpha1.PodClique, _ int) bool {
		if len(pclq.OwnerReferences) == 0 {
			return false
		}
		return pclq.OwnerReferences[0].Kind == ownerKind && pclq.OwnerReferences[0].Name == ownerObjectKey.Name
	})
	return filteredPCLQs, nil
}

// GetPCLQsMatchingLabels gets all the PodCliques in a given namespace matching selectorLabels.
func GetPCLQsMatchingLabels(ctx context.Context, cl client.Client, namespace string, selectorLabels map[string]string) ([]grovecorev1alpha1.PodClique, error) {
	podCliqueList := &grovecorev1alpha1.PodCliqueList{}

	// List PodCliques in namespace with matching labels
	if err := cl.List(ctx,
		podCliqueList,
		client.InNamespace(namespace),
		client.MatchingLabels(selectorLabels)); err != nil {
		return nil, err
	}
	return podCliqueList.Items, nil
}

// GetPCLQsByNames fetches PodClique objects by name. It returns the found PodClique objects
// and a slice of names for which no PodClique exists. If there is an error, it returns the error.
func GetPCLQsByNames(ctx context.Context, cl client.Client, namespace string, pclqFQNs []string) (pclqs []grovecorev1alpha1.PodClique, notFoundPCLQs []string, err error) {
	// Iterate through each PodClique name and attempt to fetch it
	for _, pclqFQN := range pclqFQNs {
		pclq := grovecorev1alpha1.PodClique{}
		if err := cl.Get(ctx, client.ObjectKey{Name: pclqFQN, Namespace: namespace}, &pclq); err != nil {
			// Track not found PodCliques separately
			if apierrors.IsNotFound(err) {
				notFoundPCLQs = append(notFoundPCLQs, pclqFQN)
				continue
			}
			return nil, nil, err
		}
		pclqs = append(pclqs, pclq)
	}
	return pclqs, notFoundPCLQs, nil
}

// GroupPCLQsByPodGangName filters PCLQs that have a PodGang label and groups them by the PodGang name.
func GroupPCLQsByPodGangName(pclqs []grovecorev1alpha1.PodClique) map[string][]grovecorev1alpha1.PodClique {
	return groupPCLQsByLabel(pclqs, grovecorev1alpha1.LabelPodGang)
}

// GroupPCLQsByPCSGReplicaIndex filters PCLQs that have a PodCliqueScalingGroupReplicaIndex label and groups them by the PCSG replica.
func GroupPCLQsByPCSGReplicaIndex(pclqs []grovecorev1alpha1.PodClique) map[string][]grovecorev1alpha1.PodClique {
	return groupPCLQsByLabel(pclqs, grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex)
}

// GroupPCLQsByPGSReplicaIndex filters PCLQs that have a PodGangSetReplicaIndex label and groups them by the PGS replica.
func GroupPCLQsByPGSReplicaIndex(pclqs []grovecorev1alpha1.PodClique) map[string][]grovecorev1alpha1.PodClique {
	return groupPCLQsByLabel(pclqs, grovecorev1alpha1.LabelPodGangSetReplicaIndex)
}

// GetMinAvailableBreachedPCLQInfo filters PodCliques that have ConditionTypeMinAvailableBreached set to true.
// For each such PodClique, it returns the name and calculates the duration to wait before terminationDelay is breached.
// Returns the candidate names and the shortest wait duration.
func GetMinAvailableBreachedPCLQInfo(pclqs []grovecorev1alpha1.PodClique, terminationDelay time.Duration, since time.Time) ([]string, time.Duration) {
	pclqCandidateNames := make([]string, 0, len(pclqs))
	waitForDurations := make([]time.Duration, 0, len(pclqs))

	// Check each PodClique for MinAvailableBreached condition
	for _, pclq := range pclqs {
		cond := meta.FindStatusCondition(pclq.Status.Conditions, grovecorev1alpha1.ConditionTypeMinAvailableBreached)
		if cond == nil {
			continue
		}

		// Process PodCliques with breached condition
		if cond.Status == metav1.ConditionTrue {
			pclqCandidateNames = append(pclqCandidateNames, pclq.Name)
			// Calculate remaining wait time before termination delay expires
			waitFor := terminationDelay - since.Sub(cond.LastTransitionTime.Time)
			waitForDurations = append(waitForDurations, waitFor)
		}
	}

	// Return shortest wait duration if any candidates exist
	if len(waitForDurations) == 0 {
		return pclqCandidateNames, 0
	}
	slices.Sort(waitForDurations)
	return pclqCandidateNames, waitForDurations[0]
}

// GetPodCliquesWithParentPGS retrieves PodClique objects that are directly managed by the given PodGangSet
// (not part of any PodCliqueScalingGroup).
func GetPodCliquesWithParentPGS(ctx context.Context, cl client.Client, pgsObjKey client.ObjectKey) ([]grovecorev1alpha1.PodClique, error) {
	pclqList := &grovecorev1alpha1.PodCliqueList{}

	// Build label selector for PodCliques directly managed by PodGangSet
	labels := lo.Assign(
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsObjKey.Name),
		map[string]string{
			grovecorev1alpha1.LabelComponentKey: grovecorev1alpha1.LabelComponentPGSPodCliqueValue,
		},
	)

	// List PodCliques with the constructed labels
	err := cl.List(ctx,
		pclqList,
		client.InNamespace(pgsObjKey.Namespace),
		client.MatchingLabels(labels),
	)
	if err != nil {
		return nil, err
	}
	return pclqList.Items, nil
}

// groupPCLQsByLabel groups PodCliques by the value of the specified label key.
// PodCliques without the label are excluded from the result.
func groupPCLQsByLabel(pclqs []grovecorev1alpha1.PodClique, labelKey string) map[string][]grovecorev1alpha1.PodClique {
	grouped := make(map[string][]grovecorev1alpha1.PodClique)

	// Group PodCliques by label value
	for _, pclq := range pclqs {
		labelValue, exists := pclq.Labels[labelKey]
		if !exists {
			continue
		}
		grouped[labelValue] = append(grouped[labelValue], pclq)
	}
	return grouped
}
