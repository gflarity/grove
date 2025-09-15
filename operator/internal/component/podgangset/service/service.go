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

package service

import (
	"context"
	"fmt"
	"strconv"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	"github.com/NVIDIA/grove/operator/internal/utils"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// errSyncPodGangSetService is the error code for PodGangSet service sync failures.
	errSyncPodGangSetService grovecorev1alpha1.ErrorCode = "ERR_SYNC_PODGANGSET_SERVICE"
	// errDeletePodGangSetService is the error code for PodGangSet service deletion failures.
	errDeletePodGangSetService grovecorev1alpha1.ErrorCode = "ERR_DELETE_PODGANGSET_SERVICE"
)

// _resource implements the component.Operator interface for managing PodGangSet headless services.
type _resource struct {
	client client.Client
	scheme *runtime.Scheme
}

// New creates an instance of Service component operator.
func New(client client.Client, scheme *runtime.Scheme) component.Operator[grovecorev1alpha1.PodGangSet] {
	return &_resource{
		client: client,
		scheme: scheme,
	}
}

// GetExistingResourceNames returns the names of all the existing resources that the Service Operator manages.
func (r _resource) GetExistingResourceNames(ctx context.Context, logger logr.Logger, pgsObjMeta metav1.ObjectMeta) ([]string, error) {
	logger.Info("Looking for existing PodGangSet Headless Services", "objectKey", k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta))
	// Query for existing services with matching labels
	objMetaList := &metav1.PartialObjectMetadataList{}
	objMetaList.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	if err := r.client.List(ctx,
		objMetaList,
		client.InNamespace(pgsObjMeta.Namespace),
		client.MatchingLabels(getSelectorLabelsForAllHeadlessServices(pgsObjMeta.Name)),
	); err != nil {
		return nil, groveerr.WrapError(err,
			errSyncPodGangSetService,
			component.OperationGetExistingResourceNames,
			fmt.Sprintf("Error listing Headless Services for PodGangSet: %v", k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}
	return k8sutils.FilterMapOwnedResourceNames(pgsObjMeta, objMetaList.Items), nil
}

// Sync synchronizes all resources that the Service Operator manages.
func (r _resource) Sync(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) error {
	// Generate object keys for all required services
	replicaIndexToObjectKeys := getObjectKeys(pgs)
	// Create concurrent tasks for each service
	tasks := make([]utils.Task, 0, len(replicaIndexToObjectKeys))
	for replicaIndex, objectKey := range replicaIndexToObjectKeys {
		createOrUpdateTask := utils.Task{
			Name: fmt.Sprintf("CreateOrUpdatePodGangSetService-%s", objectKey),
			Fn: func(ctx context.Context) error {
				return r.doCreateOrUpdate(ctx, logger, pgs, replicaIndex, objectKey)
			},
		}
		tasks = append(tasks, createOrUpdateTask)
	}
	// Execute all service creation/update tasks concurrently
	if runResult := utils.RunConcurrently(ctx, logger, tasks); runResult.HasErrors() {
		return groveerr.WrapError(runResult.GetAggregatedError(),
			errSyncPodGangSetService,
			component.OperationSync,
			fmt.Sprintf("Error creating or updating PodGangSet Headless Services for PodGangSet: %v, run summary: %s", client.ObjectKeyFromObject(pgs), runResult.GetSummary()),
		)
	}
	logger.Info("Successfully synced Headless Services")
	return nil
}

// Delete removes all headless services associated with the PodGangSet.
func (r _resource) Delete(ctx context.Context, logger logr.Logger, pgObjMeta metav1.ObjectMeta) error {
	logger.Info("Deleting Headless Services")
	// Delete all services matching the PodGangSet selector labels
	if err := r.client.DeleteAllOf(ctx,
		&corev1.Service{},
		client.InNamespace(pgObjMeta.Namespace),
		client.MatchingLabels(getSelectorLabelsForAllHeadlessServices(pgObjMeta.Name))); err != nil {
		return groveerr.WrapError(err,
			errDeletePodGangSetService,
			component.OperationDelete,
			fmt.Sprintf("Failed to delete Headless Services for PodGangSet: %v", k8sutils.GetObjectKeyFromObjectMeta(pgObjMeta)),
		)
	}
	logger.Info("Deleted Headless Services")
	return nil
}

// doCreateOrUpdate creates or updates a single headless service for a PodGangSet replica.
func (r _resource) doCreateOrUpdate(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int, pgServiceObjectKey client.ObjectKey) error {
	logger.Info("Running CreateOrUpdate PodGangSet Headless Service", "pgsReplicaIndex", pgsReplicaIndex, "objectKey", pgServiceObjectKey)
	// Create empty service object for patching
	pgService := emptyPGService(pgServiceObjectKey)
	opResult, err := controllerutil.CreateOrPatch(ctx, r.client, pgService, func() error {
		return r.buildResource(pgService, pgs, pgsReplicaIndex)
	})
	if err != nil {
		return groveerr.WrapError(err,
			errSyncPodGangSetService,
			component.OperationSync,
			fmt.Sprintf("Error syncing Headless Service: %v for PodGangSet: %v", pgServiceObjectKey, client.ObjectKeyFromObject(pgs)),
		)
	}
	logger.Info("Triggered create or update of PodGang Headless Service", "pgServiceObjectKey", pgServiceObjectKey, "result", opResult)
	return nil
}

// buildResource configures the service specification and sets controller ownership.
func (r _resource) buildResource(svc *corev1.Service, pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int) error {
	// Set service labels
	svc.Labels = getLabels(pgs.Name, client.ObjectKeyFromObject(svc), pgsReplicaIndex)
	// Configure publishNotReadyAddresses from PodGangSet spec
	var publishNotReadyAddresses bool
	if pgs.Spec.Template.HeadlessServiceConfig != nil {
		publishNotReadyAddresses = pgs.Spec.Template.HeadlessServiceConfig.PublishNotReadyAddresses
	}
	// Configure headless service specification
	svc.Spec = corev1.ServiceSpec{
		Selector:                 getLabelSelectorForPodsInAPodGangSetReplica(pgs.Name, pgsReplicaIndex),
		ClusterIP:                "None",
		PublishNotReadyAddresses: publishNotReadyAddresses,
	}

	// Set controller reference for garbage collection
	if err := controllerutil.SetControllerReference(pgs, svc, r.scheme); err != nil {
		return err
	}

	return nil
}

// getLabels returns the complete set of labels for a headless service.
func getLabels(pgsName string, svcObjectKey client.ObjectKey, pgsReplicaIndex int) map[string]string {
	// Service-specific labels
	svcLabels := map[string]string{
		grovecorev1alpha1.LabelAppNameKey:             svcObjectKey.Name,
		grovecorev1alpha1.LabelComponentKey:           component.NamePodGangHeadlessService,
		grovecorev1alpha1.LabelPodGangSetReplicaIndex: strconv.Itoa(pgsReplicaIndex),
	}
	return lo.Assign(
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		svcLabels,
	)
}

// getLabelSelectorForPodsInAPodGangSetReplica returns labels to select pods in a specific replica.
func getLabelSelectorForPodsInAPodGangSetReplica(pgsName string, pgsReplicaIndex int) map[string]string {
	return lo.Assign(
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		map[string]string{
			grovecorev1alpha1.LabelPodGangSetReplicaIndex: strconv.Itoa(pgsReplicaIndex),
		},
	)
}

// getSelectorLabelsForAllHeadlessServices returns labels to select all headless services for a PodGangSet.
func getSelectorLabelsForAllHeadlessServices(pgsName string) map[string]string {
	svcMatchingLabels := map[string]string{
		grovecorev1alpha1.LabelComponentKey: component.NamePodGangHeadlessService,
	}
	return lo.Assign(
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		svcMatchingLabels,
	)
}

// getObjectKeys generates object keys for all headless services based on PodGangSet replicas.
func getObjectKeys(pgs *grovecorev1alpha1.PodGangSet) []client.ObjectKey {
	// Pre-allocate slice for all replica services
	objectKeys := make([]client.ObjectKey, 0, pgs.Spec.Replicas)
	// Generate service name and object key for each replica
	for replicaIndex := range pgs.Spec.Replicas {
		serviceName := grovecorev1alpha1.GenerateHeadlessServiceName(grovecorev1alpha1.ResourceNameReplica{Name: pgs.Name, Replica: int(replicaIndex)})
		objectKeys = append(objectKeys, client.ObjectKey{
			Name:      serviceName,
			Namespace: pgs.Namespace,
		})
	}
	return objectKeys
}

// emptyPGService creates an empty service with the specified object key.
func emptyPGService(objKey client.ObjectKey) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objKey.Name,
			Namespace: objKey.Namespace,
		},
	}
}
