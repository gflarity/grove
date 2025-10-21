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

package podcliquescalinggroup

import (
	"context"
	"fmt"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	componentutils "github.com/NVIDIA/grove/operator/internal/controller/common/component/utils"
	ctrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *Reconciler) reconcileStatus(ctx context.Context, logger logr.Logger, pcsgObjectKey client.ObjectKey) ctrlcommon.ReconcileStepResult {
	// It is important that we re-fetch the PodCliqueScalingGroup. In case rolling update has been started during the spec reconciliation,
	// then UpdateInProgress condition will be set. It is essential that this is checked when computing status.
	// It is a possibility that the informer cache does not reflect the changes that are made to status conditions are not immediately reflected.
	// However, it is currently assumed that eventually this condition will be visible eventually. We will think of alleviating this delay
	// in the future.
	pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{}
	if result := ctrlutils.GetPodCliqueScalingGroup(ctx, r.client, logger, pcsgObjectKey, pcsg); ctrlcommon.ShortCircuitReconcileFlow(result) {
		return result
	}

	patchObj := client.MergeFrom(pcsg.DeepCopy())

	pcs, err := componentutils.GetPodCliqueSet(ctx, r.client, pcsg.ObjectMeta)
	if err != nil {
		logger.Error(err, "failed to get owner PodCliqueSet")
		return ctrlcommon.ReconcileWithErrors("failed to get owner PodCliqueSet", err)
	}

	pclqsPerPCSGReplica, err := r.getPodCliquesPerPCSGReplica(ctx, pcs.Name, client.ObjectKeyFromObject(pcsg))
	if err != nil {
		logger.Error(err, "failed to list PodCliques for PodCliqueScalingGroup")
		return ctrlcommon.ReconcileWithErrors(fmt.Sprintf("failed to list PodCliques for PodCliqueScalingGroup: %q", client.ObjectKeyFromObject(pcsg)), err)
	}
	mutateReplicas(logger, pcs.Status.CurrentGenerationHash, pcsg, pclqsPerPCSGReplica)
	mutateMinAvailableBreachedCondition(logger, pcsg, pclqsPerPCSGReplica)

	if err = mutateSelector(pcs, pcsg); err != nil {
		logger.Error(err, "failed to update selector for PodCliqueScalingGroup")
		return ctrlcommon.ReconcileWithErrors("failed to update selector for PodCliqueScalingGroup", err)
	}

	mutateCurrentPodCliqueSetGenerationHash(logger, pcs, pcsg, lo.Flatten(lo.Values(pclqsPerPCSGReplica)))

	if err = r.client.Status().Patch(ctx, pcsg, patchObj); err != nil {
		logger.Error(err, "failed to update PodCliqueScalingGroup status")
		return ctrlcommon.ReconcileWithErrors("failed to update the status with label selector and replicas", err)
	}

	return ctrlcommon.ContinueReconcile()
}

func mutateReplicas(logger logr.Logger, currentPCSGenerationHash *string, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) {
	pcsg.Status.Replicas = pcsg.Spec.Replicas
	var scheduledReplicas, availableReplicas, updatedReplicas int32
	for pcsgReplicaIndex, pclqs := range pclqsPerPCSGReplica {
		isScheduled, isAvailable, isUpdated := computeReplicaStatus(logger, currentPCSGenerationHash, pcsgReplicaIndex, len(pcsg.Spec.CliqueNames), pclqs)
		if isScheduled {
			scheduledReplicas++
		}
		if isAvailable {
			availableReplicas++
		}
		if isUpdated {
			updatedReplicas++
		}
	}
	logger.Info("Mutating PodCliqueScalingGroup replicas",
		"pcsg", client.ObjectKeyFromObject(pcsg),
		"scheduledReplicas", scheduledReplicas, "availableReplicas", availableReplicas, "updatedReplicas", updatedReplicas)
	pcsg.Status.ScheduledReplicas = scheduledReplicas
	pcsg.Status.AvailableReplicas = availableReplicas
	pcsg.Status.UpdatedReplicas = updatedReplicas
}

// computeReplicaStatus processes a single PodCliqueScalingGroup replica and returns whether it is scheduled and available.
func computeReplicaStatus(logger logr.Logger, currentPCSGenerationHash *string, pcsgReplicaIndex string, numPCSGCliqueNames int, pclqs []grovecorev1alpha1.PodClique) (isScheduled, isAvailable, isUpdated bool) {
	nonTerminatedPCSGPodCliques := lo.Filter(pclqs, func(pclq grovecorev1alpha1.PodClique, _ int) bool {
		return !k8sutils.IsResourceTerminating(pclq.ObjectMeta)
	})
	if len(nonTerminatedPCSGPodCliques) != numPCSGCliqueNames {
		logger.V(1).Info("PCSG replica does not have the expected number of PodCliques",
			"pcsgReplicaIndex", pcsgReplicaIndex,
			"expectedPCSGReplicaPCLQSize", numPCSGCliqueNames,
			"actualPCSGReplicaPCLQSize", len(nonTerminatedPCSGPodCliques))
		return
	}
	isScheduled = lo.EveryBy(nonTerminatedPCSGPodCliques, func(pclq grovecorev1alpha1.PodClique) bool {
		return k8sutils.IsConditionTrue(pclq.Status.Conditions, constants.ConditionTypePodCliqueScheduled)
	})
	// A PodClique is considered available if it schedules at least MinAvailable pods.
	if isScheduled {
		isAvailable = lo.EveryBy(nonTerminatedPCSGPodCliques, func(pclq grovecorev1alpha1.PodClique) bool {
			return pclq.Status.ReadyReplicas >= *pclq.Spec.MinAvailable
		})
		isAvailable = isAvailable && len(nonTerminatedPCSGPodCliques) == numPCSGCliqueNames
		isUpdated = isAvailable &&
			currentPCSGenerationHash != nil &&
			lo.EveryBy(nonTerminatedPCSGPodCliques, func(pclq grovecorev1alpha1.PodClique) bool {
				return pclq.Status.CurrentPodCliqueSetGenerationHash != nil && *pclq.Status.CurrentPodCliqueSetGenerationHash == *currentPCSGenerationHash
			})
	}
	return
}

func mutateMinAvailableBreachedCondition(logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) {
	newCondition := computeMinAvailableBreachedCondition(pcsg, pclqsPerPCSGReplica)
	if k8sutils.HasConditionChanged(pcsg.Status.Conditions, newCondition) {
		logger.Info("Updating MinAvailableBreached condition for PodCliqueScalingGroup",
			"pcsg", client.ObjectKeyFromObject(pcsg),
			"conditionType", newCondition.Type,
			"conditionStatus", newCondition.Status,
			"reason", newCondition.Reason)
		meta.SetStatusCondition(&pcsg.Status.Conditions, newCondition)
	}
}

// computeMinAvailableBreachedCondition computes the MinAvailableBreached condition for the PodCliqueScalingGroup.
// During rolling updates, returns `Unknown` to prevent influencing gang termination until the update completes.
//
// Distinguishes between INITIAL STATE (never scheduled, availableReplicas == 0) which returns False to avoid
// gang termination during creation, and DEGRADED STATE (previously available but lost) which evaluates
// availableReplicas to determine if gang termination should trigger.
func computeMinAvailableBreachedCondition(pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) metav1.Condition {
	if componentutils.IsPCSGUpdateInProgress(pcsg) {
		return metav1.Condition{
			Type:    constants.ConditionTypeMinAvailableBreached,
			Status:  metav1.ConditionUnknown,
			Reason:  constants.ConditionReasonUpdateInProgress,
			Message: "Update is in progress",
		}
	}

	minAvailable := int(*pcsg.Spec.MinAvailable)
	scheduledReplicas := int(pcsg.Status.ScheduledReplicas)

	// Compute CURRENT available replicas by directly checking PodCliques
	// We must do this BEFORE the initial state check because Status.AvailableReplicas
	// is stale and doesn't reflect the current state
	availableReplicas := computeAvailablePCSGReplicas(pclqsPerPCSGReplica)

	// Special case: Initial PCSG replica creation (availableReplicasComputed == 0)
	// Check if ANY child PodClique has WasOnceHealthy=True to distinguish:
	//   - Initial creation (all PodCliques WasOnceHealthy=false) → Don't trigger gang termination
	//   - Complete degradation (any PodClique WasOnceHealthy=true) → Trigger gang termination
	allPodCliquesNeverHealthy := true
	for _, pclqs := range pclqsPerPCSGReplica {
		for _, pclq := range pclqs {
			wasOnceHealthy := k8sutils.IsConditionTrue(pclq.Status.Conditions, constants.ConditionTypeWasOnceHealthy)
			if wasOnceHealthy {
				allPodCliquesNeverHealthy = false
				break
			}
		}
		if !allPodCliquesNeverHealthy {
			break
		}
	}

	if scheduledReplicas < minAvailable && availableReplicas == 0 && allPodCliquesNeverHealthy {
		return metav1.Condition{
			Type:    constants.ConditionTypeMinAvailableBreached,
			Status:  metav1.ConditionFalse,
			Reason:  constants.ConditionReasonInsufficientScheduledPCSGReplicas,
			Message: fmt.Sprintf("Insufficient scheduled replicas. expected at least: %d, found: %d", minAvailable, scheduledReplicas),
		}
	}

	// Normal case: Check if we have sufficient available PCSG replicas
	if availableReplicas < minAvailable {
		return metav1.Condition{
			Type:    constants.ConditionTypeMinAvailableBreached,
			Status:  metav1.ConditionTrue,
			Reason:  constants.ConditionReasonInsufficientAvailablePCSGReplicas,
			Message: fmt.Sprintf("Insufficient PodCliqueScalingGroup ready replicas, expected at least: %d, found: %d", minAvailable, availableReplicas),
		}
	}
	return metav1.Condition{
		Type:    constants.ConditionTypeMinAvailableBreached,
		Status:  metav1.ConditionFalse,
		Reason:  constants.ConditionReasonSufficientAvailablePCSGReplicas,
		Message: fmt.Sprintf("Sufficient PodCliqueScalingGroup ready replicas, expected at least: %d, found: %d", minAvailable, availableReplicas),
	}
}

// computeAvailablePCSGReplicas directly counts PCSG replicas where all constituent PodCliques are available.
// A PodClique is considered available if:
// 1. readyReplicas >= minAvailable AND
// 2. MinAvailableBreached condition is False (or not set)
// This is more accurate than scheduledReplicas - breachedReplicas because it avoids race conditions during deletion.
func computeAvailablePCSGReplicas(pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) int {
	var availableReplicas int
	for _, pclqs := range pclqsPerPCSGReplica {
		allAvailable := true
		for _, pclq := range pclqs {
			// Check if this PodClique has MinAvailableBreached=True
			isBreached := k8sutils.IsConditionTrue(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)

			// Check if this PodClique has enough ready pods
			hasEnoughReady := pclq.Status.ReadyReplicas >= *pclq.Spec.MinAvailable

			// A PodClique is only available if it has enough ready pods AND is not breached
			if !hasEnoughReady || isBreached {
				allAvailable = false
				break
			}
		}
		if allAvailable && len(pclqs) > 0 {
			availableReplicas++
		}
	}
	return availableReplicas
}

func (r *Reconciler) getPodCliquesPerPCSGReplica(ctx context.Context, pcsName string, pcsgObjKey client.ObjectKey) (map[string][]grovecorev1alpha1.PodClique, error) {
	selectorLabels := lo.Assign(
		apicommon.GetDefaultLabelsForPodCliqueSetManagedResources(pcsName),
		map[string]string{
			apicommon.LabelPodCliqueScalingGroup: pcsgObjKey.Name,
			apicommon.LabelComponentKey:          apicommon.LabelComponentNamePodCliqueScalingGroupPodClique,
		},
	)
	pclqs, err := componentutils.GetPCLQsByOwner(ctx,
		r.client,
		constants.KindPodCliqueScalingGroup,
		pcsgObjKey,
		selectorLabels,
	)
	if err != nil {
		return nil, err
	}
	pclqsPerPCSGReplica := componentutils.GroupPCLQsByPCSGReplicaIndex(pclqs)
	return pclqsPerPCSGReplica, nil
}

func mutateSelector(pcs *grovecorev1alpha1.PodCliqueSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) error {
	pcsReplicaIndex, err := k8sutils.GetPodCliqueSetReplicaIndex(pcsg.ObjectMeta)
	if err != nil {
		return err
	}
	matchingPCSGConfig, ok := lo.Find(pcs.Spec.Template.PodCliqueScalingGroupConfigs, func(pcsgConfig grovecorev1alpha1.PodCliqueScalingGroupConfig) bool {
		pcsgFQN := apicommon.GeneratePodCliqueScalingGroupName(apicommon.ResourceNameReplica{Name: pcs.Name, Replica: pcsReplicaIndex}, pcsgConfig.Name)
		return pcsgFQN == pcsg.Name
	})
	if !ok {
		// This should ideally never happen but if you find a PCSG that is not defined in PCS then just ignore it.
		return nil
	}
	// No ScaleConfig has been defined of this PCSG, therefore there is no need to add a selector in the status.
	if matchingPCSGConfig.ScaleConfig == nil {
		return nil
	}
	labels := lo.Assign(
		apicommon.GetDefaultLabelsForPodCliqueSetManagedResources(pcs.Name),
		map[string]string{
			apicommon.LabelPodCliqueScalingGroup: pcsg.Name,
		},
	)
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: labels})
	if err != nil {
		return fmt.Errorf("%w: failed to create label selector for PodCliqueScalingGroup %v", err, client.ObjectKeyFromObject(pcsg))
	}
	pcsg.Status.Selector = ptr.To(selector.String())
	return nil
}

func mutateCurrentPodCliqueSetGenerationHash(logger logr.Logger, pcs *grovecorev1alpha1.PodCliqueSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, existingPCLQs []grovecorev1alpha1.PodClique) {
	pclqFQNsPendingUpdate := componentutils.GetPCLQsInPCSGPendingUpdate(pcs, pcsg, existingPCLQs)
	if len(pclqFQNsPendingUpdate) > 0 {
		logger.Info("Found PodCliques associated to PodCliqueScalingGroup pending update", "pclqFQNsPendingUpdate", pclqFQNsPendingUpdate)
		return
	}
	if componentutils.IsPCSGUpdateInProgress(pcsg) {
		logger.Info("PodCliqueScalingGroup is currently updating, cannot set PodCliqueSet CurrentGenerationHash yet")
		return
	}
	pcsg.Status.CurrentPodCliqueSetGenerationHash = pcs.Status.CurrentGenerationHash
}
