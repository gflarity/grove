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

package podgang

import (
	"context"
	"fmt"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	componentutils "github.com/NVIDIA/grove/operator/internal/component/utils"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	groveschedulerv1alpha1 "github.com/NVIDIA/grove/scheduler/api/core/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Error codes for PodGang component operations.
const (
	errCodeListPodGangs            grovecorev1alpha1.ErrorCode = "ERR_LIST_PODGANGS"
	errCodeDeletePodGangs          grovecorev1alpha1.ErrorCode = "ERR_DELETE_PODGANGS"
	errCodeDeleteExcessPodGang     grovecorev1alpha1.ErrorCode = "ERR_DELETE_EXCESS_PODGANG"
	errCodeListPods                grovecorev1alpha1.ErrorCode = "ERR_LIST_PODS_FOR_PODGANGSET"
	errCodeListPodCliques          grovecorev1alpha1.ErrorCode = "ERR_LIST_PODCLIQUES_FOR_PODGANGSET"
	errCodeComputeExistingPodGangs grovecorev1alpha1.ErrorCode = "ERR_COMPUTE_EXISTING_PODGANG"
	errCodeSetControllerReference  grovecorev1alpha1.ErrorCode = "ERR_SET_CONTROLLER_REFERENCE"
	errCodeCreateOrPatchPodGang    grovecorev1alpha1.ErrorCode = "ERR_CREATE_OR_PATCH_PODGANG"
)

// _resource implements the component.Operator interface for managing PodGang resources.
// It provides CRUD operations for PodGang resources associated with a PodGangSet.
type _resource struct {
	client        client.Client
	scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
}

// New creates a new instance of PodGang component operator.
func New(client client.Client, scheme *runtime.Scheme, eventRecorder record.EventRecorder) component.Operator[grovecorev1alpha1.PodGangSet] {
	return &_resource{
		client:        client,
		scheme:        scheme,
		eventRecorder: eventRecorder,
	}
}

// GetExistingResourceNames retrieves the names of all existing PodGang resources
// that are owned by the specified PodGangSet.
func (r _resource) GetExistingResourceNames(ctx context.Context, logger logr.Logger, pgsObjMeta metav1.ObjectMeta) ([]string, error) {
	logger.Info("Looking for existing PodGang resources created per replica of PodGangSet")
	objMetaList := &metav1.PartialObjectMetadataList{}
	objMetaList.SetGroupVersionKind(groveschedulerv1alpha1.SchemeGroupVersion.WithKind("PodGang"))
	if err := r.client.List(ctx,
		objMetaList,
		client.InNamespace(pgsObjMeta.Namespace),
		client.MatchingLabels(componentutils.GetPodGangSelectorLabels(pgsObjMeta)),
	); err != nil {
		return nil, groveerr.WrapError(err,
			errCodeListPodGangs,
			component.OperationGetExistingResourceNames,
			fmt.Sprintf("Error listing PodGang for PodGangSet: %v", k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}
	return k8sutils.FilterMapOwnedResourceNames(pgsObjMeta, objMetaList.Items), nil
}

// Sync ensures that the desired PodGang resources exist and are up-to-date
// based on the PodGangSet specification. It creates, updates, or deletes
// PodGang resources as needed to match the desired state.
func (r _resource) Sync(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) error {
	logger.Info("Syncing PodGang resources")
	// Prepare sync context with current and desired state
	sc, err := r.prepareSyncFlow(ctx, logger, pgs)
	if err != nil {
		return err
	}
	// Execute the sync flow to reconcile state
	result := r.runSyncFlow(sc)
	if result.hasErrors() {
		return result.getAggregatedError()
	}
	if result.hasPodGangsPendingCreation() {
		return groveerr.New(groveerr.ErrCodeRequeueAfter,
			component.OperationSync,
			fmt.Sprintf("PodGangs pending creation: %v", result.podsGangsPendingCreation),
		)
	}
	return nil
}

// Delete removes all PodGang resources owned by the specified PodGangSet.
// This is typically called during PodGangSet deletion or cleanup operations.
func (r _resource) Delete(ctx context.Context, logger logr.Logger, pgsObjectMeta metav1.ObjectMeta) error {
	logger.Info("Triggering deletion of PodGangs")
	if err := r.client.DeleteAllOf(ctx,
		&groveschedulerv1alpha1.PodGang{},
		client.InNamespace(pgsObjectMeta.Namespace),
		client.MatchingLabels(getPodGangSelectorLabels(pgsObjectMeta))); err != nil {
		return groveerr.WrapError(err,
			errCodeDeletePodGangs,
			component.OperationDelete,
			fmt.Sprintf("Failed to delete PodGangs for PodGangSet: %v", k8sutils.GetObjectKeyFromObjectMeta(pgsObjectMeta)),
		)
	}
	logger.Info("Deleted PodGangs")
	return nil
}

// buildResource configures a PodGang resource with the appropriate labels,
// controller reference, and specification based on the PodGangSet and podGangInfo.
func (r _resource) buildResource(pgs *grovecorev1alpha1.PodGangSet, pgInfo podGangInfo, pg *groveschedulerv1alpha1.PodGang) error {
	// Set standard labels for PodGang resource
	pg.Labels = getLabels(pgs.Name)
	// Establish owner reference to PodGangSet for garbage collection
	if err := controllerutil.SetControllerReference(pgs, pg, r.scheme); err != nil {
		return groveerr.WrapError(
			err,
			errCodeSetControllerReference,
			component.OperationSync,
			fmt.Sprintf("failed to set the controller reference on PodGang %s to PodGangSet %v", pgInfo.fqn, client.ObjectKeyFromObject(pgs)),
		)
	}
	// Configure PodGang specification from PodGangSet template
	pg.Spec.PodGroups = createPodGroupsForPodGang(pg.Namespace, pgInfo)
	pg.Spec.PriorityClassName = pgs.Spec.Template.PriorityClassName
	return nil
}

// getPodGangSelectorLabels returns the label selector used to identify
// PodGang resources owned by a specific PodGangSet.
func getPodGangSelectorLabels(pgsObjMeta metav1.ObjectMeta) map[string]string {
	return lo.Assign(
		apicommon.GetDefaultLabelsForPodGangSetManagedResources(pgsObjMeta.Name),
		map[string]string{
			apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGang,
		})
}

// emptyPodGang creates a new PodGang resource with only the namespace and name set.
// This is used as a template for create-or-update operations.
func emptyPodGang(objKey client.ObjectKey) *groveschedulerv1alpha1.PodGang {
	return &groveschedulerv1alpha1.PodGang{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: objKey.Namespace,
			Name:      objKey.Name,
		},
	}
}

// getLabels returns the standard labels applied to PodGang resources
// managed by a PodGangSet with the given name.
func getLabels(pgsName string) map[string]string {
	return lo.Assign(
		apicommon.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		map[string]string{
			apicommon.LabelComponentKey: apicommon.LabelComponentNamePodGang,
		})
}
