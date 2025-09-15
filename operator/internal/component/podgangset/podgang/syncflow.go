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

// Package podgang implements the sync flow logic for managing PodGang resources.
// It handles the creation, update, and deletion of PodGang resources based on
// PodGangSet specifications and the current state of PodCliques and their pods.
package podgang

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveevents "github.com/NVIDIA/grove/operator/internal/component/events"
	componentutils "github.com/NVIDIA/grove/operator/internal/component/utils"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	groveschedulerv1alpha1 "github.com/NVIDIA/grove/scheduler/api/core/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// prepareSyncFlow computes the required state for the PodGang sync flow.
// It gathers all necessary information including expected PodGangs, existing resources,
// and pod assignments to build a complete sync context.
func (r _resource) prepareSyncFlow(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) (*syncContext, error) {
	pgsObjectKey := client.ObjectKeyFromObject(pgs)
	// Initialize sync context with empty collections
	sc := &syncContext{
		ctx:                  ctx,
		pgs:                  pgs,
		logger:               logger,
		expectedPodGangs:     make([]podGangInfo, 0),
		existingPodGangNames: make([]string, 0),
		pclqs:                make([]grovecorev1alpha1.PodClique, 0),
		unassignedPodsByPCLQ: make(map[string][]corev1.Pod),
		pclqPods:             make(map[string][]corev1.Pod),
	}

	// Gather all PodCliques managed by this PodGangSet
	pclqs, err := r.getPCLQsForPGS(ctx, pgsObjectKey)
	if err != nil {
		return nil, groveerr.WrapError(err,
			errCodeListPodCliques,
			component.OperationSync,
			fmt.Sprintf("failed to list PodCliques for PodGangSet %v", pgsObjectKey),
		)
	}
	sc.pclqs = pclqs

	// Compute what PodGangs should exist based on PGS spec and scaling groups
	if err := r.computeExpectedPodGangs(sc); err != nil {
		return nil, groveerr.WrapError(err,
			errCodeComputeExistingPodGangs,
			component.OperationSync,
			fmt.Sprintf("failed to compute existing PodGangs for PodGangSet %v", pgsObjectKey),
		)
	}

	// Get names of currently existing PodGang resources
	existingPodGangNames, err := r.GetExistingResourceNames(ctx, logger, pgs.ObjectMeta)
	if err != nil {
		return nil, groveerr.WrapError(err,
			errCodeListPodGangs,
			component.OperationSync,
			fmt.Sprintf("Failed to get existing PodGang names for PodGangSet: %v", client.ObjectKeyFromObject(sc.pgs)),
		)
	}
	sc.existingPodGangNames = existingPodGangNames

	// Gather all pods managed by this PodGangSet, grouped by PodClique
	podsByPCLQ, err := r.getPodsByPCLQForPGS(ctx, pgsObjectKey)
	if err != nil {
		return nil, groveerr.WrapError(err,
			errCodeListPods,
			component.OperationSync,
			fmt.Sprintf("failed to list Pods for PodGangSet %v", pgsObjectKey),
		)
	}

	sc.pclqPods = podsByPCLQ
	// Categorize pods as assigned to PodGangs or unassigned
	sc.initializeAssignedAndUnassignedPodsForPGS(podsByPCLQ)

	return sc, nil
}

// getPCLQsForPGS retrieves all PodClique resources managed by the specified PodGangSet.
func (r _resource) getPCLQsForPGS(ctx context.Context, pgsObjectKey client.ObjectKey) ([]grovecorev1alpha1.PodClique, error) {
	pclqList := &grovecorev1alpha1.PodCliqueList{}
	if err := r.client.List(ctx, pclqList,
		client.InNamespace(pgsObjectKey.Namespace),
		client.MatchingLabels(k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsObjectKey.Name))); err != nil {
		return nil, err
	}
	return pclqList.Items, nil
}

// computeExpectedPodGangs computes the expected PodGangs for the PodGangSet.
// It inspects the defined PodCliqueScalingGroup's and PodGangSet replicas to identify PodGangs.
func (r _resource) computeExpectedPodGangs(sc *syncContext) error {
	expectedPodGangs := make([]podGangInfo, 0, 50) // preallocate to avoid multiple allocations

	// Create base PodGangs for each PGS replica containing:
	// - Standalone PodCliques (not in scaling groups)
	// - Base replicas (0 to minAvailable-1) of scaling group PodCliques
	expectedPodGangs = append(expectedPodGangs, getExpectedPodGangForPGSReplicas(sc)...)

	// Create scaled PodGangs for scaling group replicas beyond minAvailable
	// These represent additional capacity that can be scaled independently
	if len(sc.pgs.Spec.Template.PodCliqueScalingGroupConfigs) > 0 {
		for pgsReplica := range sc.pgs.Spec.Replicas {
			expectedPodGangsForPCSG, err := r.getExpectedPodGangsForPCSG(sc.ctx, sc.pgs, int(pgsReplica))
			if err != nil {
				return err
			}
			expectedPodGangs = append(expectedPodGangs, expectedPodGangsForPCSG...)
		}
	}
	sc.expectedPodGangs = expectedPodGangs
	return nil
}

// getExpectedPodGangForPGSReplicas creates the BASE PodGangs for each PodGangSet replica.
//
// These are the foundational PodGangs that contain:
// 1. Standalone PodCliques (not part of any scaling group)
// 2. Base scaling group PodCliques (replicas 0 through minAvailable-1 of each scaling group)
//
// Scaled PodGangs (for scaling group replicas >= minAvailable) are handled
// separately by getExpectedPodGangsForPCSG() and managed by the PodCliqueScalingGroup controller.
func getExpectedPodGangForPGSReplicas(sc *syncContext) []podGangInfo {
	expectedPodGangs := make([]podGangInfo, 0, int(sc.pgs.Spec.Replicas))
	for pgsReplica := range sc.pgs.Spec.Replicas {
		podGangName := grovecorev1alpha1.GenerateBasePodGangName(grovecorev1alpha1.ResourceNameReplica{Name: sc.pgs.Name, Replica: int(pgsReplica)})
		expectedPodGangs = append(expectedPodGangs, podGangInfo{
			fqn:   podGangName,
			pclqs: identifyConstituentPCLQsForPGSBasePodGang(sc, pgsReplica),
		})
	}
	return expectedPodGangs
}

// getExpectedPodGangsForPCSG computes scaled PodGangs for PodCliqueScalingGroup replicas
// beyond minAvailable. These PodGangs represent additional scaling capacity.
func (r _resource) getExpectedPodGangsForPCSG(ctx context.Context, pgs *grovecorev1alpha1.PodGangSet, pgsReplica int) ([]podGangInfo, error) {
	existingPCSGs, err := r.getExistingPodCliqueScalingGroups(ctx, pgs, pgsReplica)
	if err != nil {
		return nil, err
	}

	expectedPodGangs := make([]podGangInfo, 0, 50) // preallocate to avoid multiple allocations

	// Process each scaling group configuration to determine scaled PodGangs
	for _, pcsgConfig := range pgs.Spec.Template.PodCliqueScalingGroupConfigs {
		// Generate PCSG resource name from template name
		pcsgFQN := grovecorev1alpha1.GeneratePodCliqueScalingGroupName(grovecorev1alpha1.ResourceNameReplica{Name: pgs.Name, Replica: pgsReplica}, pcsgConfig.Name)

		// MinAvailable should always be non-nil due to kubebuilder default and defaulting webhook
		minAvailable := int(*pcsgConfig.MinAvailable)

		// Use actual PCSG replicas if resource exists, otherwise fall back to template
		replicas := int(*pcsgConfig.Replicas)
		pcsg, ok := lo.Find(existingPCSGs, func(sg grovecorev1alpha1.PodCliqueScalingGroup) bool {
			return sg.Name == pcsgFQN
		})
		if ok {
			replicas = int(pcsg.Spec.Replicas)
		}

		// Create scaled PodGangs for replicas starting from minAvailable
		// The first 0..(minAvailable-1) replicas are handled by the PGS replica PodGang
		// Scaled PodGangs use 0-based indexing regardless of minAvailable value
		scaledReplicas := replicas - minAvailable
		for podGangIndex, pcsgReplica := 0, minAvailable; podGangIndex < scaledReplicas; podGangIndex, pcsgReplica = podGangIndex+1, pcsgReplica+1 {
			podGangName := grovecorev1alpha1.CreatePodGangNameFromPCSGFQN(pcsgFQN, podGangIndex)
			pclqs, err := identifyConstituentPCLQsForPCSGPodGang(pgs, pcsgFQN, pcsgReplica, pcsgConfig.CliqueNames)
			if err != nil {
				return nil, err
			}
			expectedPodGangs = append(expectedPodGangs, podGangInfo{
				fqn:   podGangName,
				pclqs: pclqs,
			})
		}
	}
	return expectedPodGangs, nil
}

// identifyConstituentPCLQsForPGSBasePodGang determines which PodCliques belong to a base PodGang.
// Base PodGangs contain standalone PodCliques and base replicas of scaling group PodCliques.
func identifyConstituentPCLQsForPGSBasePodGang(sc *syncContext, pgsReplica int32) []pclqInfo {
	constituentPCLQs := make([]pclqInfo, 0, len(sc.pgs.Spec.Template.Cliques))
	// Process each PodClique template to determine base PodGang membership
	for _, pclqTemplateSpec := range sc.pgs.Spec.Template.Cliques {
		// Check if this PodClique belongs to a scaling group
		pcsgConfig := componentutils.FindScalingGroupConfigForClique(sc.pgs.Spec.Template.PodCliqueScalingGroupConfigs, pclqTemplateSpec.Name)
		if pcsgConfig != nil {
			// Add scaling group PodClique instances for replicas 0 through (minAvailable-1)
			scalingGroupPclqs := buildPCSGPodCliqueInfosForBasePodGang(sc, pclqTemplateSpec, pcsgConfig, pgsReplica)
			constituentPCLQs = append(constituentPCLQs, scalingGroupPclqs...)
		} else {
			// Add standalone PodClique (not part of a scaling group)
			standalonePclq := buildNonPCSGPodCliqueInfosForBasePodGang(sc, pclqTemplateSpec, pgsReplica)
			constituentPCLQs = append(constituentPCLQs, standalonePclq)
		}
	}
	return constituentPCLQs
}

// buildPCSGPodCliqueInfosForBasePodGang generates PodClique info for the BASE PODGANG portion of a scaling group.
//
// IMPORTANT: This function only generates PodClique info for replicas 0 through (minAvailable-1).
// These PodCliques will be grouped into the BASE PodGang, which represents the minimum viable
// cluster that must be scheduled together as a gang.
//
// Scaled PodGangs (for replicas >= minAvailable) are generated separately by the
// PodCliqueScalingGroup controller and get their own scaled PodGang resources.
//
// EXAMPLE with minAvailable=3:
//   - This function creates PodCliques for replicas 0, 1, 2 → go into base PodGang "simple1-0"
//   - PCSG controller creates PodCliques for replicas 3, 4 → get scaled PodGangs "simple1-0-sga-0", etc.
func buildPCSGPodCliqueInfosForBasePodGang(sc *syncContext, pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec, pcsgConfig *grovecorev1alpha1.PodCliqueScalingGroupConfig, pgsReplica int32) []pclqInfo {
	// MinAvailable should always be non-nil due to kubebuilder default and defaulting webhook
	minAvailable := int(*pcsgConfig.MinAvailable)

	pclqs := make([]pclqInfo, 0, minAvailable)
	for replicaIndex := range minAvailable {
		pcsgFQN := grovecorev1alpha1.GeneratePodCliqueScalingGroupName(
			grovecorev1alpha1.ResourceNameReplica{Name: sc.pgs.Name, Replica: int(pgsReplica)},
			pcsgConfig.Name,
		)
		pclqFQN := grovecorev1alpha1.GeneratePodCliqueName(
			grovecorev1alpha1.ResourceNameReplica{Name: pcsgFQN, Replica: replicaIndex},
			pclqTemplateSpec.Name,
		)

		pclqInfo := buildPodCliqueInfo(sc, pclqTemplateSpec, pclqFQN)
		pclqs = append(pclqs, pclqInfo)
	}
	return pclqs
}

// buildNonPCSGPodCliqueInfosForBasePodGang generates pclqInfo for a PodClique that doesn't belong to a scaling group
func buildNonPCSGPodCliqueInfosForBasePodGang(sc *syncContext, pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec, pgsReplica int32) pclqInfo {
	pclqFQN := grovecorev1alpha1.GeneratePodCliqueName(
		grovecorev1alpha1.ResourceNameReplica{Name: sc.pgs.Name, Replica: int(pgsReplica)},
		pclqTemplateSpec.Name,
	)
	return buildPodCliqueInfo(sc, pclqTemplateSpec, pclqFQN)
}

// buildPodCliqueInfo creates a pclqInfo object with the correct replicas and minAvailable values
func buildPodCliqueInfo(sc *syncContext, pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec, pclqFQN string) pclqInfo {
	replicas := determinePodCliqueReplicas(sc, pclqTemplateSpec, pclqFQN)
	return pclqInfo{
		fqn:          pclqFQN,
		replicas:     replicas,
		minAvailable: *pclqTemplateSpec.Spec.MinAvailable,
	}
}

// determinePodCliqueReplicas determines the correct replicas count for a PodClique
func determinePodCliqueReplicas(sc *syncContext, pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec, pclqFQN string) int32 {
	if pclqTemplateSpec.Spec.ScaleConfig == nil {
		return pclqTemplateSpec.Spec.Replicas
	}
	matchingPCLQ, found := lo.Find(sc.pclqs, func(pclq grovecorev1alpha1.PodClique) bool {
		return pclqFQN == pclq.Name
	})
	if !found {
		// PodClique resource not found - might be during initial creation
		// Fall back to template replicas but log warning for visibility
		sc.logger.Info("[WARN]: PodClique resource not found, using template replicas",
			"podCliqueFQN", pclqFQN,
			"templateReplicas", pclqTemplateSpec.Spec.Replicas)
		return pclqTemplateSpec.Spec.Replicas
	}
	return matchingPCLQ.Spec.Replicas
}

// identifyConstituentPCLQsForPCSGPodGang determines PodCliques for a scaled PodGang.
// Scaled PodGangs contain specific replicas of scaling group PodCliques.
func identifyConstituentPCLQsForPCSGPodGang(pgs *grovecorev1alpha1.PodGangSet, pcsgFQN string, pcsgReplica int, cliqueNames []string) ([]pclqInfo, error) {
	constituentPCLQs := make([]pclqInfo, 0, len(cliqueNames))
	for _, pclqName := range cliqueNames {
		pclqTemplate, ok := lo.Find(pgs.Spec.Template.Cliques, func(pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec) bool {
			return pclqName == pclqTemplateSpec.Name
		})
		if !ok {
			return nil, fmt.Errorf("PodCliqueScalingGroup references a PodClique that does not exist in the PodGangSet: %s", pclqName)
		}

		pclqFQN := grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{Name: pcsgFQN, Replica: pcsgReplica}, pclqName)

		// Create pclqInfo using consistent logic (note: we use template replicas here since this is for scaling group instances)
		constituentPCLQs = append(constituentPCLQs, pclqInfo{
			fqn:          pclqFQN,
			replicas:     pclqTemplate.Spec.Replicas, // For scaling group instances, always use template replicas
			minAvailable: *pclqTemplate.Spec.MinAvailable,
		})
	}
	return constituentPCLQs, nil
}

// getExistingPodCliqueScalingGroups retrieves all PodCliqueScalingGroup resources
// for a specific PodGangSet replica.
func (r _resource) getExistingPodCliqueScalingGroups(ctx context.Context, pgs *grovecorev1alpha1.PodGangSet, pgsReplica int) ([]grovecorev1alpha1.PodCliqueScalingGroup, error) {
	pcsgList := &grovecorev1alpha1.PodCliqueScalingGroupList{}
	if err := r.client.List(ctx,
		pcsgList,
		client.InNamespace(pgs.Namespace),
		client.MatchingLabels(
			lo.Assign(
				k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgs.Name),
				map[string]string{
					grovecorev1alpha1.LabelPodGangSetReplicaIndex: strconv.Itoa(pgsReplica),
				},
			),
		),
	); err != nil {
		return nil, err
	}
	return lo.Filter(pcsgList.Items, func(pcsg grovecorev1alpha1.PodCliqueScalingGroup, _ int) bool {
		return metav1.IsControlledBy(&pcsg, pgs)
	}), nil
}

// getPodsByPCLQForPGS retrieves all pods managed by a PodGangSet,
// grouped by their owning PodClique name.
func (r _resource) getPodsByPCLQForPGS(ctx context.Context, pgsObjectKey client.ObjectKey) (map[string][]corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := r.client.List(ctx,
		podList,
		client.InNamespace(pgsObjectKey.Namespace),
		client.MatchingLabels(k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsObjectKey.Name)),
	); err != nil {
		return nil, err
	}

	// Group pods by their PodClique owner, excluding pods being deleted
	podsByPCLQ := make(map[string][]corev1.Pod)
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		pclqFQN := k8sutils.GetFirstOwnerName(pod.ObjectMeta)
		podsByPCLQ[pclqFQN] = append(podsByPCLQ[pclqFQN], pod)
	}

	return podsByPCLQ, nil
}

// runSyncFlow executes the main sync logic: delete excess PodGangs and create/update expected ones.
func (r _resource) runSyncFlow(sc *syncContext) syncFlowResult {
	result := syncFlowResult{}
	if err := r.deleteExcessPodGangs(sc); err != nil {
		result.errs = append(result.errs, err)
		return result
	}
	return r.createOrUpdatePodGangs(sc)
}

// deleteExcessPodGangs removes PodGang resources that are no longer needed
// based on the current PodGangSet specification.
func (r _resource) deleteExcessPodGangs(sc *syncContext) error {
	// Identify PodGangs that exist but are no longer expected
	expectedPodGangNames := lo.Map(sc.expectedPodGangs, func(pg podGangInfo, _ int) string {
		return pg.fqn
	})
	excessPodGangs, _ := lo.Difference(sc.existingPodGangNames, expectedPodGangNames)
	namespace := sc.pgs.Namespace
	for _, podGangToDelete := range excessPodGangs {
		pgObjectKey := client.ObjectKey{Namespace: namespace, Name: podGangToDelete}
		pg := emptyPodGang(pgObjectKey)
		sc.logger.Info("Delete excess PodGang", "objectKey", client.ObjectKeyFromObject(pg))
		if err := client.IgnoreNotFound(r.client.Delete(sc.ctx, pg)); err != nil {
			r.eventRecorder.Eventf(sc.pgs, corev1.EventTypeWarning, groveevents.ReasonPodGangDeletionFailed, "Error deleting PodGang %v: %v", pgObjectKey, err)
			return groveerr.WrapError(err,
				errCodeDeleteExcessPodGang,
				component.OperationSync,
				fmt.Sprintf("failed to delete PodGang %v", pgObjectKey),
			)
		}
		r.eventRecorder.Eventf(sc.pgs, corev1.EventTypeNormal, groveevents.ReasonPodGangDeletionSuccessful, "Deleted PodGang %v", pgObjectKey)
		sc.deletedPodGangNames = append(sc.deletedPodGangNames, podGangToDelete)
		sc.logger.Info("Triggered delete of excess PodGang", "objectKey", client.ObjectKeyFromObject(pg))
	}
	return nil
}

// createOrUpdatePodGangs processes all expected PodGangs, creating or updating them
// if all their constituent pods are ready.
func (r _resource) createOrUpdatePodGangs(sc *syncContext) syncFlowResult {
	result := syncFlowResult{}
	// Identify PodGangs that don't exist yet
	pendingPodGangNames := sc.getPodGangNamesPendingCreation()
	for _, podGang := range sc.expectedPodGangs {
		sc.logger.Info("[createOrUpdatePodGangs] processing PodGang", "fqn", podGang.fqn)
		isPodGangPendingCreation := slices.Contains(pendingPodGangNames, podGang.fqn)
		// Check if all required pods are created and properly labeled
		numPendingPods := r.getPodsPendingCreationOrAssociation(sc, podGang)
		if isPodGangPendingCreation && numPendingPods > 0 {
			sc.logger.Info("skipping creation of PodGang as all desired replicas have not yet been created or assigned", "fqn", podGang.fqn, "numPendingPodsToCreateOrAssociate", numPendingPods)
			result.recordPodGangPendingCreation(podGang.fqn)
			continue
		}
		if err := r.createOrUpdatePodGang(sc, podGang); err != nil {
			sc.logger.Error(err, "failed to create PodGang", "PodGangName", podGang.fqn)
			result.recordError(err)
			return result
		}
		result.recordPodGangCreation(podGang.fqn)
	}
	return result
}

// getPodsForPodCliquesPendingCreation returns the number of expected pods from PodCliques that are pending creation
func (r _resource) getPodsForPodCliquesPendingCreation(sc *syncContext, podGang podGangInfo) int {
	existingPCLQNames := lo.Map(sc.pclqs, func(pclq grovecorev1alpha1.PodClique, _ int) string {
		return pclq.Name
	})

	return lo.Reduce(podGang.pclqs, func(agg int, pclq pclqInfo, _ int) int {
		if !slices.Contains(existingPCLQNames, pclq.fqn) {
			return agg + int(pclq.replicas)
		}
		return agg
	}, 0)
}

// getPodsPendingCreationOrAssociation counts pods that are not yet ready for PodGang assignment.
// This includes pods from non-existent PodCliques and pods missing proper PodGang labels.
func (r _resource) getPodsPendingCreationOrAssociation(sc *syncContext, podGang podGangInfo) int {
	// Count pods from PodCliques that don't exist yet
	numPodsPendingPCLQCreate := r.getPodsForPodCliquesPendingCreation(sc, podGang)

	// Count pods from existing PodCliques that aren't ready
	var numPodsPendingCreateOrAssociate int
	pclqs := sc.getPodCliques(podGang)
	for _, pclq := range pclqs {
		existingPCLQPods := sc.pclqPods[pclq.Name]
		// Count missing pods (positive difference means pods need to be created)
		// Negative difference means excess pods exist, which we ignore for creation logic
		numPodsPendingCreateOrAssociate += max(0, int(pclq.Spec.Replicas)-len(existingPCLQPods))

		// Check existing pods for proper PodGang label assignment
		for _, existingPod := range existingPCLQPods {
			podGangLabelValue, ok := existingPod.GetLabels()[grovecorev1alpha1.LabelPodGang]
			if !ok {
				sc.logger.Info("Pod does not have a PodGang label yet", "podObjectKey", client.ObjectKeyFromObject(&existingPod), "expectedPodGangName", podGang.fqn)
				numPodsPendingCreateOrAssociate += 1
				continue
			}
			if podGangLabelValue != podGang.fqn {
				sc.logger.Error(nil, "PodGang label does not match expected PodGang name. This should ideally never happen and indicates a coding error", "podObjectKey", client.ObjectKeyFromObject(&existingPod), "expectedPodGangName", podGang.fqn, "podGangLabelValue", podGangLabelValue)
				numPodsPendingCreateOrAssociate += 1
			}
		}
	}
	return numPodsPendingPCLQCreate + numPodsPendingCreateOrAssociate
}

// createOrUpdatePodGang creates or updates a single PodGang resource using controller-runtime's CreateOrPatch.
func (r _resource) createOrUpdatePodGang(sc *syncContext, pgInfo podGangInfo) error {
	pgObjectKey := client.ObjectKey{
		Namespace: sc.pgs.Namespace,
		Name:      pgInfo.fqn,
	}
	pg := emptyPodGang(pgObjectKey)
	sc.logger.Info("CreateOrPatch PodGang", "objectKey", pgObjectKey)
	_, err := controllerutil.CreateOrPatch(sc.ctx, r.client, pg, func() error {
		return r.buildResource(sc.pgs, pgInfo, pg)
	})
	if err != nil {
		r.eventRecorder.Eventf(sc.pgs, corev1.EventTypeWarning, groveevents.ReasonPodGangCreationOrUpdationFailed, "Error Creating/Updating PodGang %v: %v", pgObjectKey, err)
		return groveerr.WrapError(err,
			errCodeCreateOrPatchPodGang,
			component.OperationSync,
			fmt.Sprintf("Failed to CreateOrPatch PodGang %v", pgObjectKey),
		)
	}
	r.eventRecorder.Eventf(sc.pgs, corev1.EventTypeNormal, groveevents.ReasonPodGangCreationOrUpdationSuccessful, "Created/Updated PodGang %v", pgObjectKey)
	sc.logger.Info("Triggered CreateOrPatch of PodGang", "objectKey", pgObjectKey)
	return nil
}

// createPodGroupsForPodGang converts PodClique information into PodGroup specifications
// for the scheduler. Each PodClique becomes a PodGroup with its associated pod references.
func createPodGroupsForPodGang(namespace string, pgInfo podGangInfo) []groveschedulerv1alpha1.PodGroup {
	podGroups := lo.Map(pgInfo.pclqs, func(pclq pclqInfo, _ int) groveschedulerv1alpha1.PodGroup {
		namespacedNames := lo.Map(pclq.associatedPodNames, func(associatedPodName string, _ int) groveschedulerv1alpha1.NamespacedName {
			return groveschedulerv1alpha1.NamespacedName{
				Namespace: namespace,
				Name:      associatedPodName,
			}
		})
		// Sort pod references to ensure deterministic ordering and prevent unnecessary updates
		sort.Slice(namespacedNames, func(i, j int) bool {
			return namespacedNames[i].Name < namespacedNames[j].Name
		})
		return groveschedulerv1alpha1.PodGroup{
			Name:          pclq.fqn,
			PodReferences: namespacedNames,
			MinReplicas:   pclq.minAvailable,
		}
	})
	return podGroups
}

// Convenience types and methods on these types that are used during sync flow run.
// ------------------------------------------------------------------------------------------------

// syncContext holds the complete state required during a PodGang sync flow execution.
// It contains all the information needed to determine what PodGangs should exist,
// what currently exists, and how pods are assigned to PodGangs.
type syncContext struct {
	ctx                  context.Context
	pgs                  *grovecorev1alpha1.PodGangSet
	logger               logr.Logger
	expectedPodGangs     []podGangInfo
	existingPodGangNames []string
	deletedPodGangNames  []string
	pclqs                []grovecorev1alpha1.PodClique
	unassignedPodsByPCLQ map[string][]corev1.Pod
	pclqPods             map[string][]corev1.Pod
}

// getPodGangNamesPendingCreation returns names of expected PodGangs that don't exist yet.
func (sc *syncContext) getPodGangNamesPendingCreation() []string {
	return lo.FilterMap(sc.expectedPodGangs, func(podGang podGangInfo, _ int) (string, bool) {
		return podGang.fqn, !slices.Contains(sc.existingPodGangNames, podGang.fqn)
	})
}

// initializeAssignedAndUnassignedPodsForPGS categorizes pods based on their PodGang label status.
// Pods with PodGang labels are associated with their respective PodGangs,
// while unlabeled pods are marked as unassigned.
func (sc *syncContext) initializeAssignedAndUnassignedPodsForPGS(podsByPLCQ map[string][]corev1.Pod) {
	// Process each pod to determine its assignment status
	for pclqName, pods := range podsByPLCQ {
		for _, pod := range pods {
			if metav1.HasLabel(pod.ObjectMeta, grovecorev1alpha1.LabelPodGang) {
				podGangName := pod.GetLabels()[grovecorev1alpha1.LabelPodGang]
				// Find the index to work with the original slice element, not a copy
				pgiIndex := slices.IndexFunc(sc.expectedPodGangs, func(pgi podGangInfo) bool {
					return podGangName == pgi.fqn
				})
				if pgiIndex == -1 {
					continue
				}
				// Associate pod with its PodGang
				sc.expectedPodGangs[pgiIndex].refreshAssociatedPCLQPods(pclqName, pod.Name)
			} else {
				// Pod lacks PodGang label, mark as unassigned
				sc.unassignedPodsByPCLQ[pclqName] = append(sc.unassignedPodsByPCLQ[pclqName], pod)
			}
		}
	}
}

// getPodCliques retrieves the actual PodClique resources that constitute a PodGang.
func (sc *syncContext) getPodCliques(podGang podGangInfo) []grovecorev1alpha1.PodClique {
	constituentPCLQs := make([]grovecorev1alpha1.PodClique, 0, len(podGang.pclqs))
	for _, podGangConstituentPCLQInfo := range podGang.pclqs {
		for _, pclq := range sc.pclqs {
			if pclq.Name == podGangConstituentPCLQInfo.fqn {
				constituentPCLQs = append(constituentPCLQs, pclq)
			}
		}
	}
	return constituentPCLQs
}

// syncFlowResult captures the outcome of a PodGang sync flow execution.
// It tracks which PodGangs were successfully processed, which are still pending,
// and any errors that occurred during the sync operation.
type syncFlowResult struct {
	// podsGangsPendingCreation are the names of PodGangs that could not be created in this sync run.
	// It could be due to all PCLQs not present, or it could be due to presence of at least one PCLQ that is not ready.
	podsGangsPendingCreation []string
	// createdPodGangNames are the names of the PodGangs that got created during the sync flow run.
	createdPodGangNames []string
	// errs are the list of errors during the sync flow run.
	errs []error
}

// hasErrors returns true if any errors occurred during the sync flow.
func (sfr *syncFlowResult) hasErrors() bool {
	return len(sfr.errs) > 0
}

// recordError adds an error to the sync flow result.
func (sfr *syncFlowResult) recordError(err error) {
	sfr.errs = append(sfr.errs, err)
}

// hasPodGangsPendingCreation returns true if any PodGangs are waiting to be created.
func (sfr *syncFlowResult) hasPodGangsPendingCreation() bool {
	return len(sfr.podsGangsPendingCreation) > 0
}

// recordPodGangCreation marks a PodGang as successfully created or updated.
func (sfr *syncFlowResult) recordPodGangCreation(podGangName string) {
	sfr.createdPodGangNames = append(sfr.createdPodGangNames, podGangName)
}

// recordPodGangPendingCreation marks a PodGang as pending creation due to unready dependencies.
func (sfr *syncFlowResult) recordPodGangPendingCreation(podGangName string) {
	sfr.podsGangsPendingCreation = append(sfr.podsGangsPendingCreation, podGangName)
}

// getAggregatedError combines all errors from the sync flow into a single error.
func (sfr *syncFlowResult) getAggregatedError() error {
	return errors.Join(sfr.errs...)
}

// podGangInfo represents a PodGang specification with its constituent PodCliques.
// It contains all the information needed to create or update a PodGang resource,
// including which PodCliques belong to it and their replica requirements.
// Each PodClique constituent maps directly to a scheduler PodGroup.
type podGangInfo struct {
	// fqn is a fully qualified name of a PodGang.
	fqn string
	// pclqs holds the relevant information for all constituent PodCliques for this PodGang.
	pclqs []pclqInfo
}

// refreshAssociatedPCLQPods updates the list of pods associated with a specific PodClique in this PodGang.
func (pgi *podGangInfo) refreshAssociatedPCLQPods(pclqName string, newlyAssociatedPods ...string) {
	for i := range pgi.pclqs {
		if pgi.pclqs[i].fqn == pclqName {
			pgi.pclqs[i].associatedPodNames = append(pgi.pclqs[i].associatedPodNames, newlyAssociatedPods...)
		}
	}
}

// pclqInfo represents a PodClique's contribution to a PodGang.
// It captures the PodClique's replica requirements and current pod assignments
// within the context of a specific PodGang. This maps directly to a scheduler PodGroup.
type pclqInfo struct {
	// fqn is a fully qualified name for the PodClique
	fqn string
	// replicas is the number of Pods that are assigned to the PodGang for which this PodClique is a constituent.
	replicas int32
	// minAvailable is the minimum number of pods that are required for gang scheduling from this PodClique
	minAvailable int32
	// associatedPodNames are Pod names (having this PodClique as an owner) that have already been associated to this PodGang.
	// This will be updated as and when pods are either deleted or new pods are associated.
	associatedPodNames []string
}
