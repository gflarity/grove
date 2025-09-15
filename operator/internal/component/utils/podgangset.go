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
	"context"
	"slices"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetAllPodCliqueScalingGroupFQNsForPGSReplica computes the FQNs for all PodCliqueScalingGroups defined in PGS for all its replicas.
func GetAllPodCliqueScalingGroupFQNsForPGSReplica(pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int) []string {
	// Pre-allocate slice with known capacity for efficiency
	pcsgNames := make([]string, 0, len(pgs.Spec.Template.PodCliqueScalingGroupConfigs))

	// Generate FQN for each PodCliqueScalingGroup configuration
	for _, pcsgConfig := range pgs.Spec.Template.PodCliqueScalingGroupConfigs {
		pcsgName := grovecorev1alpha1.GeneratePodCliqueScalingGroupName(grovecorev1alpha1.ResourceNameReplica{Name: pgs.Name, Replica: pgsReplicaIndex}, pcsgConfig.Name)
		pcsgNames = append(pcsgNames, pcsgName)
	}
	return pcsgNames
}

// GetPodCliqueFQNsForPGSReplicaNotInPCSG computes the FQNs for all PodCliques for a PGS replica which are not part of any PCSG.
func GetPodCliqueFQNsForPGSReplicaNotInPCSG(pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int) []string {
	// Pre-allocate slice with maximum possible capacity
	pclqNames := make([]string, 0, len(pgs.Spec.Template.Cliques))

	// Filter PodCliques that are not managed by any PodCliqueScalingGroup
	for _, pclqTemplateSpec := range pgs.Spec.Template.Cliques {
		if !isPCLQInPCSG(pclqTemplateSpec.Name, pgs.Spec.Template.PodCliqueScalingGroupConfigs) {
			pclqNames = append(pclqNames, grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{Name: pgs.Name, Replica: pgsReplicaIndex}, pclqTemplateSpec.Name))
		}
	}
	return pclqNames
}

// isPCLQInPCSG checks if a PodClique name is managed by any PodCliqueScalingGroup configuration.
func isPCLQInPCSG(pclqName string, pcsgConfigs []grovecorev1alpha1.PodCliqueScalingGroupConfig) bool {
	// Use reduce to check if any PCSG configuration contains the given PodClique name
	return lo.Reduce(pcsgConfigs, func(agg bool, pcsgConfig grovecorev1alpha1.PodCliqueScalingGroupConfig, _ int) bool {
		return agg || slices.Contains(pcsgConfig.CliqueNames, pclqName)
	}, false)
}

// GetOwnerPodGangSet gets the owner PodGangSet object.
func GetOwnerPodGangSet(ctx context.Context, cl client.Client, objectMeta metav1.ObjectMeta) (*grovecorev1alpha1.PodGangSet, error) {
	// Extract PodGangSet name from object metadata labels
	pgsName := GetPodGangSetName(objectMeta)

	// Create PodGangSet object with name and namespace
	pgs := &grovecorev1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pgsName,
			Namespace: objectMeta.Namespace,
		},
	}

	// Fetch the actual PodGangSet from the cluster
	err := cl.Get(ctx, client.ObjectKeyFromObject(pgs), pgs)
	return pgs, err
}

// GetPodGangSetName retrieves the PodGangSet name from the labels of the given ObjectMeta.
// NOTE: It is assumed that all managed objects like PCSG, PCLQ and Pods will always have PGS name as value for grovecorev1alpha1.LabelPartOfKey label.
// It should be ensured that labels that are set by the operator are never removed.
func GetPodGangSetName(objectMeta metav1.ObjectMeta) string {
	// Extract PodGangSet name from the standard label
	pgsName := objectMeta.GetLabels()[grovecorev1alpha1.LabelPartOfKey]
	return pgsName
}

// GetExpectedPCLQNamesGroupByOwner returns the expected unqualified PodClique names which are either owned by PodGangSet or PodCliqueScalingGroup.
func GetExpectedPCLQNamesGroupByOwner(pgs *grovecorev1alpha1.PodGangSet) (expectedPCLQNamesForPGS []string, expectedPCLQNamesForPCSG []string) {
	// Collect all PodClique names managed by PodCliqueScalingGroups
	pcsgConfigs := pgs.Spec.Template.PodCliqueScalingGroupConfigs
	for _, pcsgConfig := range pcsgConfigs {
		expectedPCLQNamesForPCSG = append(expectedPCLQNamesForPCSG, pcsgConfig.CliqueNames...)
	}

	// Extract all PodClique names from the PodGangSet template
	pgsCliqueNames := lo.Map(pgs.Spec.Template.Cliques, func(pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec, _ int) string {
		return pclqTemplateSpec.Name
	})

	// Calculate PodCliques owned directly by PGS (not managed by any PCSG)
	expectedPCLQNamesForPGS, _ = lo.Difference(pgsCliqueNames, expectedPCLQNamesForPCSG)
	return
}
