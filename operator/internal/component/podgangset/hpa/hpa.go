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

// Package hpa provides HorizontalPodAutoscaler management for PodGangSet resources.
// It handles creation, updates, and deletion of HPA resources for PodCliques and
// PodCliqueScalingGroups within a PodGangSet.
package hpa

import (
	"context"
	"fmt"
	"slices"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	"github.com/NVIDIA/grove/operator/internal/utils"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Error codes for HPA operations.
const (
	errListHPA   grovecorev1alpha1.ErrorCode = "ERR_LIST_HPA"
	errSyncHPA   grovecorev1alpha1.ErrorCode = "ERR_SYNC_HPA"
	errDeleteHPA grovecorev1alpha1.ErrorCode = "ERR_DELETE_HPA"
)

// _resource implements the component.Operator interface for HPA management.
type _resource struct {
	client client.Client
	scheme *runtime.Scheme
}

// New creates an instance of HPA component operator.
func New(client client.Client, scheme *runtime.Scheme) component.Operator[grovecorev1alpha1.PodGangSet] {
	return &_resource{
		client: client,
		scheme: scheme,
	}
}

// GetExistingResourceNames returns the names of all the existing resources that the HPA Operator manages.
func (r _resource) GetExistingResourceNames(ctx context.Context, logger logr.Logger, pgsObjMeta metav1.ObjectMeta) ([]string, error) {
	logger.Info("Looking for existing HPA resources")
	objMetaList := &metav1.PartialObjectMetadataList{}
	objMetaList.SetGroupVersionKind(autoscalingv2.SchemeGroupVersion.WithKind("HorizontalPodAutoscaler"))
	if err := r.client.List(ctx,
		objMetaList,
		client.InNamespace(pgsObjMeta.Namespace),
		client.MatchingLabels(getPodCliqueHPASelectorLabels(pgsObjMeta)),
	); err != nil {
		return nil, groveerr.WrapError(err,
			errListHPA,
			component.OperationGetExistingResourceNames,
			fmt.Sprintf("Error listing HorizontalPodAutoscaler for PodCliques belonging to PodGangSet: %s", k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}
	return k8sutils.FilterMapOwnedResourceNames(pgsObjMeta, objMetaList.Items), nil
}

// Sync synchronizes all resources that the HPA Operator manages.
func (r _resource) Sync(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) error {
	// Get current HPA resources managed by this PodGangSet
	existingHPANames, err := r.GetExistingResourceNames(ctx, logger, pgs.ObjectMeta)
	if err != nil {
		return err
	}

	// Compute the expected HPA resources based on PodGangSet spec
	expectedHPAInfos := r.computeExpectedHPAs(pgs)

	// Create tasks for deletion and creation/update operations
	tasks := make([]utils.Task, 0, (len(pgs.Spec.Template.Cliques)+len(pgs.Spec.Template.PodCliqueScalingGroupConfigs))*int(pgs.Spec.Replicas))
	tasks = append(tasks, r.deleteExcessHPATasks(logger, pgs, existingHPANames, expectedHPAInfos)...)
	tasks = append(tasks, r.createOrUpdateHPATasks(logger, pgs, expectedHPAInfos)...)

	// Execute all HPA sync tasks concurrently
	if runResult := utils.RunConcurrentlyWithSlowStart(ctx, logger, 1, tasks); runResult.HasErrors() {
		return groveerr.WrapError(runResult.GetAggregatedError(),
			errSyncHPA,
			component.OperationSync,
			fmt.Sprintf("Error CreateOrUpdate HorizontalPodAutoscalers for PodGangSet: %v, run summary: %s", client.ObjectKeyFromObject(pgs), runResult.GetSummary()),
		)
	}
	logger.Info("Successfully synced HorizontalPodAutoscalers for PodGangSet")
	return nil
}

// Delete deletes all resources that the HPA Operator manages.
func (r _resource) Delete(ctx context.Context, logger logr.Logger, pgsObjMeta metav1.ObjectMeta) error {
	logger.Info("Triggering delete of HPA(s)")
	if err := r.client.DeleteAllOf(ctx,
		&autoscalingv2.HorizontalPodAutoscaler{},
		client.InNamespace(pgsObjMeta.Namespace),
		client.MatchingLabels(getPodCliqueHPASelectorLabels(pgsObjMeta)),
	); err != nil {
		return groveerr.WrapError(err,
			errDeleteHPA,
			component.OperationDelete,
			fmt.Sprintf("Error deleting HPA for PodGangSet %v", k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}
	logger.Info("Deleted HPA(s)")
	return nil
}

// hpaInfo holds the state for a HPA resource. This will be used during sync run for HPA resources.
type hpaInfo struct {
	objectKey               client.ObjectKey
	targetScaleResourceKind string
	targetScaleResourceName string
	scaleConfig             grovecorev1alpha1.AutoScalingConfig
}

// computeExpectedHPAs calculates all HPA resources that should exist based on the PodGangSet specification.
// It creates HPAs for both individual PodCliques and PodCliqueScalingGroups that have scaling configurations.

func (r _resource) computeExpectedHPAs(pgs *grovecorev1alpha1.PodGangSet) []hpaInfo {
	expectedHPAInfos := make([]hpaInfo, 0, (len(pgs.Spec.Template.Cliques)+len(pgs.Spec.Template.PodCliqueScalingGroupConfigs))*int(pgs.Spec.Replicas))

	// Iterate through each replica of the PodGangSet
	for replicaIndex := range pgs.Spec.Replicas {
		// Process PodCliques with individual scaling configurations
		for _, pclqTemplateSpec := range pgs.Spec.Template.Cliques {
			// Skip PodCliques without scaling configuration
			if pclqTemplateSpec.Spec.ScaleConfig == nil {
				continue
			}

			// Generate fully qualified name for the PodClique
			pclqFQN := apicommon.GeneratePodCliqueName(apicommon.ResourceNameReplica{Name: pgs.Name, Replica: int(replicaIndex)}, pclqTemplateSpec.Name)
			hpaObjectKey := client.ObjectKey{
				Namespace: pgs.Namespace,
				Name:      pclqFQN,
			}
			expectedHPAInfos = append(expectedHPAInfos, hpaInfo{
				objectKey:               hpaObjectKey,
				targetScaleResourceKind: constants.KindPodClique,
				targetScaleResourceName: pclqFQN,
				scaleConfig:             *pclqTemplateSpec.Spec.ScaleConfig,
			})
		}

		// Process PodCliqueScalingGroups with scaling configurations
		for _, pcsgConfig := range pgs.Spec.Template.PodCliqueScalingGroupConfigs {
			// Skip PodCliqueScalingGroups without scaling configuration
			if pcsgConfig.ScaleConfig == nil {
				continue
			}

			// Generate fully qualified name for the PodCliqueScalingGroup
			pcsgFQN := apicommon.GeneratePodCliqueScalingGroupName(apicommon.ResourceNameReplica{Name: pgs.Name, Replica: int(replicaIndex)}, pcsgConfig.Name)
			hpaObjectKey := client.ObjectKey{
				Namespace: pgs.Namespace,
				Name:      pcsgFQN,
			}
			expectedHPAInfos = append(expectedHPAInfos, hpaInfo{
				objectKey:               hpaObjectKey,
				targetScaleResourceKind: constants.KindPodCliqueScalingGroup,
				targetScaleResourceName: pcsgFQN,
				scaleConfig:             *pcsgConfig.ScaleConfig,
			})
		}
	}
	return expectedHPAInfos
}

// createOrUpdateHPATasks generates tasks for creating or updating HPA resources.
func (r _resource) createOrUpdateHPATasks(logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, expectedHPAInfos []hpaInfo) []utils.Task {
	createOrUpdateTasks := make([]utils.Task, 0, len(expectedHPAInfos))
	for _, expectedHPAInfo := range expectedHPAInfos {
		task := utils.Task{
			Name: fmt.Sprintf("CreateOrUpdateHPA-%s", expectedHPAInfo.objectKey.Name),
			Fn: func(ctx context.Context) error {
				return r.doCreateOrUpdateHPA(ctx, logger, pgs, expectedHPAInfo)
			},
		}
		logger.V(4).Info("Adding task to create or update HPA", "taskName", task.Name, "hpaObjectKey", expectedHPAInfo.objectKey, "targetResourceKind", expectedHPAInfo.targetScaleResourceKind, "targetResourceName", expectedHPAInfo.targetScaleResourceName)
		createOrUpdateTasks = append(createOrUpdateTasks, task)
	}
	return createOrUpdateTasks
}

// deleteExcessHPATasks generates tasks for deleting HPA resources that are no longer needed.
func (r _resource) deleteExcessHPATasks(logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, existingHPANames []string, expectedHPAInfos []hpaInfo) []utils.Task {
	deleteTasks := make([]utils.Task, 0)

	// Extract expected HPA names from the computed info
	expectedHPANames := lo.Map(expectedHPAInfos, func(h hpaInfo, _ int) string {
		return h.objectKey.Name
	})

	// Find HPAs that exist but are no longer expected
	excessHPANames := lo.Filter(existingHPANames, func(existingHPAName string, _ int) bool {
		return !slices.Contains(expectedHPANames, existingHPAName)
	})
	// Create deletion tasks for excess HPAs
	for _, excessHPA := range excessHPANames {
		objectKey := client.ObjectKey{
			Namespace: pgs.Namespace,
			Name:      excessHPA,
		}
		task := utils.Task{
			Name: fmt.Sprintf("DeleteHPA-%s", excessHPA),
			Fn: func(ctx context.Context) error {
				return r.doDeleteHPA(ctx, logger, pgs.ObjectMeta, objectKey)
			},
		}
		logger.V(4).Info("Adding task to delete HPA", "taskName", task.Name, "hpaObjectKey", objectKey)
		deleteTasks = append(deleteTasks, task)
	}
	return deleteTasks
}

// doCreateOrUpdateHPA performs the actual creation or update of a single HPA resource.
func (r _resource) doCreateOrUpdateHPA(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, expectedHPAInfo hpaInfo) error {
	logger.Info("Running CreateOrUpdate HPA", "targetScaleResourceKind", expectedHPAInfo.targetScaleResourceKind, "targetScaleResourceName", expectedHPAInfo.targetScaleResourceName, "hpaObjectKey", expectedHPAInfo.objectKey)

	// Create empty HPA object with correct namespace and name
	hpa := emptyHPA(expectedHPAInfo.objectKey)

	// Use CreateOrPatch to handle both creation and updates
	opResult, err := controllerutil.CreateOrPatch(ctx, r.client, hpa, func() error {
		return r.buildResource(pgs, hpa, expectedHPAInfo)
	})
	if err != nil {
		return groveerr.WrapError(err,
			errSyncHPA,
			component.OperationSync,
			fmt.Sprintf("Error creating or updating HPA: %v for [Kind: %s, Name: %s]", expectedHPAInfo.objectKey, expectedHPAInfo.targetScaleResourceKind, expectedHPAInfo.targetScaleResourceName),
		)
	}
	logger.Info("Triggered create or update of HPA", "hpaObjectKey", expectedHPAInfo.objectKey, "result", opResult)
	return nil
}

// doDeleteHPA performs the actual deletion of a single HPA resource.
func (r _resource) doDeleteHPA(ctx context.Context, logger logr.Logger, pgsObjectMeta metav1.ObjectMeta, objectKey client.ObjectKey) error {
	logger.Info("Running Delete HPA", "hpaObjectKey", objectKey)

	// Attempt to delete the HPA resource
	if err := r.client.Delete(ctx, emptyHPA(objectKey)); err != nil {
		// Handle case where HPA was already deleted
		if errors.IsNotFound(err) {
			logger.Info("HPA not found, deletion is a no-op", "objectKey", objectKey)
			return nil
		}
		return groveerr.WrapError(err,
			errDeleteHPA,
			component.OperationSync,
			fmt.Sprintf("Error deleting excess HPA for PodGangSet %v", k8sutils.GetObjectKeyFromObjectMeta(pgsObjectMeta)),
		)
	}
	logger.Info("Triggered Delete of HPA", "hpaObjectKey", objectKey)
	return nil
}

// buildResource configures the HPA resource with the desired specification.
func (r _resource) buildResource(pgs *grovecorev1alpha1.PodGangSet, hpa *autoscalingv2.HorizontalPodAutoscaler, expectedHPAInfo hpaInfo) error {
	// Configure scaling parameters from the scale config
	hpa.Spec.MinReplicas = expectedHPAInfo.scaleConfig.MinReplicas
	hpa.Spec.MaxReplicas = expectedHPAInfo.scaleConfig.MaxReplicas

	// Set the target resource reference
	hpa.Spec.ScaleTargetRef = autoscalingv2.CrossVersionObjectReference{
		Kind:       expectedHPAInfo.targetScaleResourceKind,
		Name:       expectedHPAInfo.targetScaleResourceName,
		APIVersion: grovecorev1alpha1.SchemeGroupVersion.String(),
	}

	// Configure metrics and labels
	hpa.Spec.Metrics = expectedHPAInfo.scaleConfig.Metrics
	hpa.Labels = getLabels(pgs.Name, hpa.Name)

	// Set the PodGangSet as the controller owner
	if err := controllerutil.SetControllerReference(pgs, hpa, r.scheme); err != nil {
		return groveerr.WrapError(err,
			errSyncHPA,
			component.OperationSync,
			fmt.Sprintf("Error setting owner reference of HPA %s to PodGang %s", hpa.Name, pgs.Name),
		)
	}
	return nil
}

// getLabels generates the standard labels for HPA resources.
func getLabels(pgsName, hpaName string) map[string]string {
	hpaComponentLabels := map[string]string{
		apicommon.LabelAppNameKey:   hpaName,
		apicommon.LabelComponentKey: apicommon.LabelComponentNameHorizontalPodAutoscaler,
	}
	return lo.Assign(
		apicommon.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		hpaComponentLabels,
	)
}

// getPodCliqueHPASelectorLabels returns labels used to select HPA resources managed by a PodGangSet.
func getPodCliqueHPASelectorLabels(pgsObjectMeta metav1.ObjectMeta) map[string]string {
	return lo.Assign(
		apicommon.GetDefaultLabelsForPodGangSetManagedResources(pgsObjectMeta.Name),
		map[string]string{
			apicommon.LabelComponentKey: apicommon.LabelComponentNameHorizontalPodAutoscaler,
		},
	)
}

// emptyHPA creates an empty HPA object with the specified namespace and name.
func emptyHPA(objKey client.ObjectKey) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objKey.Name,
			Namespace: objKey.Namespace,
		},
	}
}
