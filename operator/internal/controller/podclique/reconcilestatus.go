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

// Package podclique implements the PodClique controller which manages the lifecycle of pod groups.
package podclique

import (
	"context"
	"fmt"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	componentutils "github.com/NVIDIA/grove/operator/internal/controller/common/component/utils"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *Reconciler) reconcileStatus(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	pcsName := componentutils.GetPodCliqueSetName(pclq.ObjectMeta)
	pclqObjectKey := client.ObjectKeyFromObject(pclq)
	patch := client.MergeFrom(pclq.DeepCopy())

	pcs, err := componentutils.GetPodCliqueSet(ctx, r.client, pclq.ObjectMeta)
	if err != nil {
		logger.Error(err, "could not get PodCliqueSet for PodClique", "pclqObjectKey", pclqObjectKey)
		return ctrlcommon.ReconcileWithErrors("could not get PodCliqueSet for PodClique", err)
	}

	existingPods, err := componentutils.GetPCLQPods(ctx, r.client, pcsName, pclq)
	if err != nil {
		logger.Error(err, "failed to list pods for PodClique")
		return ctrlcommon.ReconcileWithErrors(fmt.Sprintf("failed to list pods for PodClique: %q", pclqObjectKey), err)
	}

	podCategories := k8sutils.CategorizePodsByConditionType(logger, existingPods)

	// mutate PodClique.Status.CurrentPodTemplateHash and PodClique.Status.CurrentPodCliqueSetGenerationHash
	if err = mutateCurrentHashes(logger, pcs, pclq); err != nil {
		logger.Error(err, "failed to compute PodClique current hashes")
		return ctrlcommon.ReconcileWithErrors("failed to compute PodClique current hashes", err)
	}
	// mutate PodClique Status Replicas, ReadyReplicas, ScheduleGatedReplicas and UpdatedReplicas.
	mutateReplicas(pclq, podCategories, len(existingPods))
	mutateUpdatedReplica(pclq, existingPods)

	// mutate the conditions only if the PodClique has been successfully reconciled at least once.
	// This prevents prematurely setting incorrect conditions.
	if pclq.Status.ObservedGeneration != nil {
		mutatePodCliqueScheduledCondition(pclq)
		mutateWasOnceHealthyCondition(pclq)
		mutateMinAvailableBreachedCondition(pclq,
			len(podCategories[k8sutils.PodHasAtleastOneContainerWithNonZeroExitCode]),
			len(podCategories[k8sutils.PodStartedButNotReady]))
	}

	// mutate the selector that will be used by an autoscaler.
	if err = mutateSelector(pcsName, pclq); err != nil {
		logger.Error(err, "failed to update selector for PodClique")
		return ctrlcommon.ReconcileWithErrors("failed to set selector for PodClique", err)
	}

	// update the PodClique status.
	if err := r.client.Status().Patch(ctx, pclq, patch); err != nil {
		logger.Error(err, "failed to update PodClique status")
		return ctrlcommon.ReconcileWithErrors("failed to update PodClique status", err)
	}
	return ctrlcommon.ContinueReconcile()
}

func mutateCurrentHashes(logger logr.Logger, pcs *grovecorev1alpha1.PodCliqueSet, pclq *grovecorev1alpha1.PodClique) error {
	if componentutils.IsPCLQUpdateInProgress(pclq) || pclq.Status.UpdatedReplicas != pclq.Status.Replicas {
		logger.Info("PodClique is currently updating, cannot set PodCliqueSet CurrentGenerationHash yet")
		return nil
	}
	if pclq.Status.RollingUpdateProgress == nil {
		expectedPodTemplateHash, err := componentutils.GetExpectedPCLQPodTemplateHash(pcs, pclq.ObjectMeta)
		if err != nil {
			return err
		}
		if pclq.Status.CurrentPodTemplateHash == nil || *pclq.Status.CurrentPodTemplateHash == expectedPodTemplateHash {
			pclq.Status.CurrentPodTemplateHash = ptr.To(expectedPodTemplateHash)
			pclq.Status.CurrentPodCliqueSetGenerationHash = pcs.Status.CurrentGenerationHash
		}
	} else if componentutils.IsLastPCLQUpdateCompleted(pclq) {
		logger.Info("PodClique update has completed, setting CurrentPodCliqueSetGenerationHash")
		pclq.Status.CurrentPodTemplateHash = ptr.To(pclq.Status.RollingUpdateProgress.PodTemplateHash)
		pclq.Status.CurrentPodCliqueSetGenerationHash = ptr.To(pclq.Status.RollingUpdateProgress.PodCliqueSetGenerationHash)
	}
	return nil
}

func mutateReplicas(pclq *grovecorev1alpha1.PodClique, podCategories map[corev1.PodConditionType][]*corev1.Pod, numExistingPods int) {
	// mutate the PCLQ status with current number of schedule gated, ready pods and updated pods.
	numNonTerminatingPods := int32(numExistingPods - len(podCategories[k8sutils.TerminatingPod]))
	pclq.Status.Replicas = numNonTerminatingPods
	pclq.Status.ReadyReplicas = int32(len(podCategories[corev1.PodReady]))
	pclq.Status.ScheduleGatedReplicas = int32(len(podCategories[k8sutils.ScheduleGatedPod]))
	pclq.Status.ScheduledReplicas = int32(len(podCategories[corev1.PodScheduled]))
}

func mutateUpdatedReplica(pclq *grovecorev1alpha1.PodClique, existingPods []*corev1.Pod) {
	var expectedPodTemplateHash string
	// If the PCLQ update is in progress then take the expected PodTemplateHash from the PodClique.Status.RollingUpdateProgress.PodCliqueSetGenerationHash field
	// else take it from the PodClique.Status.CurrentPodTemplateHash field
	if componentutils.IsPCLQUpdateInProgress(pclq) {
		expectedPodTemplateHash = pclq.Status.RollingUpdateProgress.PodTemplateHash
	} else if pclq.Status.CurrentPodTemplateHash != nil {
		// CurrentPodTemplateHash will be set if RollingUpdateProgress is nil or if the last update has completed.
		expectedPodTemplateHash = *pclq.Status.CurrentPodTemplateHash
	}
	// If expectedPodTemplateHash is empty, it means that the PCLQ has never been successfully reconciled and therefore no pods should be considered as updated.
	// This prevents incorrectly marking all existing pods as updated when the PCLQ is first created.
	// Once the PCLQ is successfully reconciled, the expectedPodTemplateHash will be set and the updated replicas can be calculated correctly.
	if expectedPodTemplateHash != "" {
		updatedReplicas := lo.Reduce(existingPods, func(agg int, pod *corev1.Pod, _ int) int {
			if pod.Labels[apicommon.LabelPodTemplateHash] == expectedPodTemplateHash {
				return agg + 1
			}
			return agg
		}, 0)
		pclq.Status.UpdatedReplicas = int32(updatedReplicas)
	}
}

func mutateSelector(pcsName string, pclq *grovecorev1alpha1.PodClique) error {
	if pclq.Spec.ScaleConfig == nil {
		return nil
	}
	labels := lo.Assign(
		apicommon.GetDefaultLabelsForPodCliqueSetManagedResources(pcsName),
		map[string]string{
			apicommon.LabelPodClique: pclq.Name,
		},
	)
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: labels})
	if err != nil {
		return fmt.Errorf("%w: failed to create label selector for PodClique %v", err, client.ObjectKeyFromObject(pclq))
	}
	pclq.Status.Selector = ptr.To(selector.String())
	return nil
}

// mutateWasOnceHealthyCondition updates the WasOnceHealthy condition on the PodClique.
//
// The WasOnceHealthy condition enables gang termination to distinguish between:
// - Initial State: Pods haven't been created/scheduled yet → Don't trigger termination
// - Degraded State: Pods were running but got deleted/failed → DO trigger termination
//
// This prevents false positives during pod creation while ensuring gang termination
// activates when the workload degrades (e.g., pods deleted for whatever reason).
//
// For more information on gang termination, see the following documentation
// see docs/gang_termination_overview.md
func mutateWasOnceHealthyCondition(pclq *grovecorev1alpha1.PodClique) {
	newCondition := computeWasOnceHealthyCondition(pclq)
	if k8sutils.HasConditionChanged(pclq.Status.Conditions, newCondition) {

		meta.SetStatusCondition(&pclq.Status.Conditions, newCondition)
	}
}

func mutateMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, numNotReadyPodsWithContainersInError, numPodsStartedButNotReady int) {
	newCondition := computeMinAvailableBreachedCondition(pclq, numNotReadyPodsWithContainersInError, numPodsStartedButNotReady)
	if k8sutils.HasConditionChanged(pclq.Status.Conditions, newCondition) {
		meta.SetStatusCondition(&pclq.Status.Conditions, newCondition)
	}
}

// computeWasOnceHealthyCondition calculates the WasOnceHealthy condition for the PodClique.
// This is a one-way condition with a ratchet behavior:
// 1. Starts as False when PodClique is created
// 2. Transitions to True when readyReplicas >= minAvailable
// 3. Never transitions back to False, even if pods are deleted/failed
//
// This one-way behavior is critical for distinguishing two scenarios that both have readyReplicas=0:
//   - Initial Creation: PodClique just created, pods haven't started yet → DON'T trigger gang termination
//   - Complete Degradation: PodClique was healthy, then ALL pods were lost → DO trigger gang termination
//
// The condition "remembers" that this PodClique was once healthy, so readyReplicas=0
// after being healthy is treated as a true failure, not just initial pod creation delays
func computeWasOnceHealthyCondition(pclq *grovecorev1alpha1.PodClique) metav1.Condition {
	minAvailable := int(*pclq.Spec.MinAvailable)
	readyReplicas := int(pclq.Status.ReadyReplicas)
	now := metav1.Now()

	// Check if WasOnceHealthy condition already exists and is True
	wasOnceHealthyCond := meta.FindStatusCondition(pclq.Status.Conditions, constants.ConditionTypeWasOnceHealthy)
	if wasOnceHealthyCond != nil && wasOnceHealthyCond.Status == metav1.ConditionTrue {
		// Once True, always True - this is a one-way transition
		return *wasOnceHealthyCond
	}

	// Check if we've achieved healthy state
	if readyReplicas >= minAvailable {
		return metav1.Condition{
			Type:               constants.ConditionTypeWasOnceHealthy,
			Status:             metav1.ConditionTrue,
			Reason:             constants.ConditionReasonHealthyStateAchieved,
			Message:            fmt.Sprintf("PodClique achieved healthy state with %d ready pods (minAvailable: %d)", readyReplicas, minAvailable),
			LastTransitionTime: now,
		}
	}

	// Not yet healthy
	return metav1.Condition{
		Type:               constants.ConditionTypeWasOnceHealthy,
		Status:             metav1.ConditionFalse,
		Reason:             constants.ConditionReasonNeverHealthy,
		Message:            fmt.Sprintf("PodClique has not yet achieved healthy state. ready: %d, minAvailable: %d", readyReplicas, minAvailable),
		LastTransitionTime: now,
	}
}

func computeMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, numPodsHavingAtleastOneContainerWithNonZeroExitCode, numPodsStartedButNotReady int) metav1.Condition {
	if componentutils.IsPCLQUpdateInProgress(pclq) {
		return metav1.Condition{
			Type:    constants.ConditionTypeMinAvailableBreached,
			Status:  metav1.ConditionUnknown,
			Reason:  constants.ConditionReasonUpdateInProgress,
			Message: "Update is in progress",
		}
	}
	// dereferencing is considered safe as MinAvailable will always be set by the defaulting webhook. If this changes in the future,
	// make sure that you check for nil explicitly.
	minAvailable := int(*pclq.Spec.MinAvailable)
	scheduledReplicas := int(pclq.Status.ScheduledReplicas)
	readyReplicas := int(pclq.Status.ReadyReplicas)
	now := metav1.Now()

	// Check if PodClique has ever been healthy
	wasOnceHealthyCond := meta.FindStatusCondition(pclq.Status.Conditions, constants.ConditionTypeWasOnceHealthy)
	wasOnceHealthy := wasOnceHealthyCond != nil && wasOnceHealthyCond.Status == metav1.ConditionTrue

	// Special case: Complete pod loss (scheduledReplicas < minAvailable && readyReplicas == 0)
	// The readyReplicas == 0 check ensures we only apply this logic when ALL pods are gone,
	// not partial degradation. Partial cases are handled by readyOrStartingPods logic below.
	//
	// Use WasOnceHealthy to distinguish:
	//   - Initial creation (WasOnceHealthy=false) → Don't trigger gang termination
	//   - Complete degradation (WasOnceHealthy=true) → Trigger gang termination
	if scheduledReplicas < minAvailable && readyReplicas == 0 {
		if !wasOnceHealthy {
			return metav1.Condition{
				Type:               constants.ConditionTypeMinAvailableBreached,
				Status:             metav1.ConditionFalse,
				Reason:             constants.ConditionReasonInsufficientScheduledPods,
				Message:            fmt.Sprintf("Initial state: insufficient scheduled pods. expected at least: %d, found: %d", minAvailable, scheduledReplicas),
				LastTransitionTime: now,
			}
		}
		return metav1.Condition{
			Type:               constants.ConditionTypeMinAvailableBreached,
			Status:             metav1.ConditionTrue,
			Reason:             constants.ConditionReasonInsufficientScheduledPods,
			Message:            fmt.Sprintf("Degraded state: insufficient scheduled pods. expected at least: %d, found: %d", minAvailable, scheduledReplicas),
			LastTransitionTime: now,
		}
	}

	// Normal Case: Check if we have sufficient ready or starting pods
	// Calculate readyOrStartingPods by excluding:
	// - Pods with containers that have exited with non-zero exit codes (failed containers)
	// - Pods that have started but are not yet ready (still initializing)
	//
	// This gives pods with long-running init containers or slow startup time a grace period
	// before they are considered unavailable and trigger gang termination.
	readyOrStartingPods := scheduledReplicas - numPodsHavingAtleastOneContainerWithNonZeroExitCode - numPodsStartedButNotReady

	if readyOrStartingPods < minAvailable {
		return metav1.Condition{
			Type:               constants.ConditionTypeMinAvailableBreached,
			Status:             metav1.ConditionTrue,
			Reason:             constants.ConditionReasonInsufficientReadyPods,
			Message:            fmt.Sprintf("Insufficient ready or starting pods. expected at least: %d, found: %d", minAvailable, readyOrStartingPods),
			LastTransitionTime: now,
		}
	}
	return metav1.Condition{
		Type:               constants.ConditionTypeMinAvailableBreached,
		Status:             metav1.ConditionFalse,
		Reason:             constants.ConditionReasonSufficientReadyPods,
		Message:            fmt.Sprintf("Either sufficient ready or starting pods found. expected at least: %d, found: %d", minAvailable, readyOrStartingPods),
		LastTransitionTime: now,
	}
}

func mutatePodCliqueScheduledCondition(pclq *grovecorev1alpha1.PodClique) {
	newCondition := computePodCliqueScheduledCondition(pclq)
	if k8sutils.HasConditionChanged(pclq.Status.Conditions, newCondition) {
		meta.SetStatusCondition(&pclq.Status.Conditions, newCondition)
	}
}

func computePodCliqueScheduledCondition(pclq *grovecorev1alpha1.PodClique) metav1.Condition {
	now := metav1.Now()
	if pclq.Status.ScheduledReplicas < *pclq.Spec.MinAvailable {
		return metav1.Condition{
			Type:               constants.ConditionTypePodCliqueScheduled,
			Status:             metav1.ConditionFalse,
			Reason:             constants.ConditionReasonInsufficientScheduledPods,
			Message:            fmt.Sprintf("Insufficient scheduled pods. expected at least: %d, found: %d", *pclq.Spec.MinAvailable, pclq.Status.ScheduledReplicas),
			LastTransitionTime: now,
		}
	}
	return metav1.Condition{
		Type:               constants.ConditionTypePodCliqueScheduled,
		Status:             metav1.ConditionTrue,
		Reason:             constants.ConditionReasonSufficientScheduledPods,
		Message:            fmt.Sprintf("Sufficient scheduled pods found. expected at least: %d, found: %d", *pclq.Spec.MinAvailable, pclq.Status.ScheduledReplicas),
		LastTransitionTime: now,
	}
}
