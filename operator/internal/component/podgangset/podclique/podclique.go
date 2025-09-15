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

package podclique

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveevents "github.com/NVIDIA/grove/operator/internal/component/events"
	componentutils "github.com/NVIDIA/grove/operator/internal/component/utils"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	"github.com/NVIDIA/grove/operator/internal/utils"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Error codes for PodClique operations.
const (
	// errListPodClique indicates failure to list PodClique resources.
	errListPodClique grovecorev1alpha1.ErrorCode = "ERR_LIST_PODCLIQUE"
	// errSyncPodClique indicates failure during PodClique synchronization.
	errSyncPodClique grovecorev1alpha1.ErrorCode = "ERR_SYNC_PODCLIQUE"
	// errDeletePodClique indicates failure to delete PodClique resources.
	errDeletePodClique grovecorev1alpha1.ErrorCode = "ERR_DELETE_PODCLIQUE"
	// errCodeListPodCliqueScalingGroup indicates failure to list PodCliqueScalingGroup resources.
	errCodeListPodCliqueScalingGroup grovecorev1alpha1.ErrorCode = "ERR_LIST_PODCLIQUESCALINGGROUP"
	// errCodeListPodCliques indicates failure to list multiple PodClique resources.
	errCodeListPodCliques grovecorev1alpha1.ErrorCode = "ERR_LIST_PODCLIQUES"
	// errCodeCreatePodClique indicates failure to create a PodClique resource.
	errCodeCreatePodClique grovecorev1alpha1.ErrorCode = "ERR_CREATE_PODCLIQUE"
)

// _resource implements the component.Operator interface for managing PodClique resources
// within a PodGangSet. It handles creation, synchronization, and deletion of PodCliques.
type _resource struct {
	client        client.Client
	scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
}

// New creates an instance of PodClique component operator.
func New(client client.Client, scheme *runtime.Scheme, eventRecorder record.EventRecorder) component.Operator[grovecorev1alpha1.PodGangSet] {
	return &_resource{
		client:        client,
		scheme:        scheme,
		eventRecorder: eventRecorder,
	}
}

// GetExistingResourceNames returns the names of all the existing resources that the PodClique Operator manages.
func (r _resource) GetExistingResourceNames(ctx context.Context, logger logr.Logger, pgsObjMeta metav1.ObjectMeta) ([]string, error) {
	logger.Info("Looking for existing PodCliques")
	pclqPartialObjMetaList, err := k8sutils.ListExistingPartialObjectMetadata(ctx,
		r.client,
		grovecorev1alpha1.SchemeGroupVersion.WithKind("PodClique"),
		pgsObjMeta,
		getPodCliqueSelectorLabels(pgsObjMeta))
	if err != nil {
		return nil, groveerr.WrapError(err,
			errCodeListPodCliques,
			component.OperationGetExistingResourceNames,
			fmt.Sprintf("Error listing PodCliques for PodGangSet: %v", k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}
	return k8sutils.FilterMapOwnedResourceNames(pgsObjMeta, pclqPartialObjMetaList), nil
}

// Sync synchronizes all resources that the PodClique Operator manages.
func (r _resource) Sync(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) error {
	expectedPCLQNames, _ := componentutils.GetExpectedPCLQNamesGroupByOwner(pgs)

	if err := r.triggerDeletionOfExcessPCLQs(ctx, logger, pgs, expectedPCLQNames); err != nil {
		return err
	}

	terminationDelay := pgs.Spec.Template.TerminationDelay.Duration
	podGangsRequiringRequeue, err := r.checkMinAvailableBreachAndDeletePGSReplicaPodGang(ctx, logger, pgs, terminationDelay)
	if err != nil {
		return err
	}
	if err = r.createExpectedPCLQs(ctx, logger, pgs, expectedPCLQNames); err != nil {
		return err
	}

	if len(podGangsRequiringRequeue) > 0 {
		logger.Info("Found PCLQs with MinAvailable breached but which have not crossed TerminationDelay", "podGangs", podGangsRequiringRequeue, "terminationDelay", terminationDelay)
		return groveerr.New(groveerr.ErrCodeContinueReconcileAndRequeue,
			component.OperationSync,
			"Requeuing to re-process PCLQs that have breached MinAvailable but not crossed TerminationDelay",
		)
	}

	return nil
}

// triggerDeletionOfExcessPCLQs removes PodCliques that exceed the expected replica count.
// It compares existing PodCliques with expected ones and deletes the excess.
func (r _resource) triggerDeletionOfExcessPCLQs(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, expectedPCLQNames []string) error {
	existingPCLQNames, err := r.GetExistingResourceNames(ctx, logger, pgs.ObjectMeta)
	if err != nil {
		return groveerr.WrapError(err,
			errSyncPodClique,
			component.OperationSync,
			fmt.Sprintf("Unable to fetch existing PodClique names for PodGangSet: %v", client.ObjectKeyFromObject(pgs)),
		)
	}
	// Calculate how many PodCliques need to be deleted
	diff := len(existingPCLQNames) - len(expectedPCLQNames)
	if diff > 0 {
		logger.Info("Found more PodCliques than expected", "expected", expectedPCLQNames, "existing", existingPCLQNames)
		logger.Info("Triggering deletion of extra PodCliques", "count", diff)
		// Identify which PodCliques should be deleted based on replica index
		deletionCandidateNames, err := getPodCliqueNamesToDelete(pgs.Name, int(pgs.Spec.Replicas), existingPCLQNames)
		if err != nil {
			return err
		}
		deletePCLQTasks := r.createDeleteTasks(logger, pgs, deletionCandidateNames)
		return r.triggerDeletionOfPodCliques(ctx, logger, client.ObjectKeyFromObject(pgs), deletePCLQTasks)
	}
	return nil
}

// createExpectedPCLQs creates all expected PodCliques for the PodGangSet.
// It generates creation tasks for each replica and PodClique combination.
func (r _resource) createExpectedPCLQs(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, expectedPCLQNames []string) error {
	tasks := make([]utils.Task, 0, len(expectedPCLQNames))

	// Create tasks for each PodGangSet replica and expected PodClique
	for pgsReplica := range pgs.Spec.Replicas {
		for _, expectedPCLQName := range expectedPCLQNames {
			pclqObjectKey := client.ObjectKey{
				Name:      grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{Name: pgs.Name, Replica: int(pgsReplica)}, expectedPCLQName),
				Namespace: pgs.Namespace,
			}
			createTask := utils.Task{
				Name: fmt.Sprintf("CreatePodClique-%s", pclqObjectKey),
				Fn: func(ctx context.Context) error {
					return r.doCreate(ctx, logger, pgs, pgsReplica, pclqObjectKey)
				},
			}
			tasks = append(tasks, createTask)
		}
	}
	if runResult := utils.RunConcurrently(ctx, logger, tasks); runResult.HasErrors() {
		return groveerr.WrapError(runResult.GetAggregatedError(),
			errSyncPodClique,
			component.OperationSync,
			fmt.Sprintf("Error Create of PodCliques for PodGangSet: %v, run summary: %s", client.ObjectKeyFromObject(pgs), runResult.GetSummary()),
		)
	}
	return nil
}

// getMinAvailableBreachedPCSGsForPGSReplica checks for PodCliqueScalingGroups that have breached MinAvailable.
// Returns the names of breached PCSGs and the minimum wait time before termination.
func (r _resource) getMinAvailableBreachedPCSGsForPGSReplica(ctx context.Context, pgsObjKey client.ObjectKey, pgsReplicaIndex int, terminationDelay time.Duration, since time.Time) ([]string, time.Duration, error) {
	pcsgs, err := componentutils.GetPCSGsForPGSReplicaIndex(ctx, r.client, pgsObjKey, pgsReplicaIndex)
	if err != nil {
		return nil, 0, groveerr.WrapError(err,
			errCodeListPodCliqueScalingGroup,
			component.OperationSync,
			fmt.Sprintf("failed to list PodCliqueScalingGroups for PodGangSet: %v,  replica Index: %d", pgsObjKey, pgsReplicaIndex))
	}

	breachedPCSGNames, minWaitFor := componentutils.GetMinAvailableBreachedPCSGInfo(pcsgs, terminationDelay, since)
	return breachedPCSGNames, minWaitFor, nil
}

// getMinAvailableBreachedPCLQsNotInPCSGForPGSReplica checks for PodCliques not managed by PCSGs that have breached MinAvailable.
// Returns breached PodClique names, minimum wait time, and whether to skip this replica.
func (r _resource) getMinAvailableBreachedPCLQsNotInPCSGForPGSReplica(ctx context.Context, pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int, since time.Time) (breachedPCLQNames []string, minWaitFor time.Duration, skipPGSReplica bool, err error) {
	pclqFQNsNotInPCSG := componentutils.GetPodCliqueFQNsForPGSReplicaNotInPCSG(pgs, pgsReplicaIndex)
	pclqs, notFoundPCLQFQNs, err := componentutils.GetPCLQsByNames(ctx, r.client, pgs.Namespace, pclqFQNsNotInPCSG)
	if err != nil {
		err = groveerr.WrapError(err,
			errCodeListPodCliques,
			component.OperationSync,
			fmt.Sprintf("failed to list PodCliques: %v for PodGangSet: %v, replica Index: %d", pclqFQNsNotInPCSG, client.ObjectKeyFromObject(pgs), pgsReplicaIndex),
		)
		return
	}
	// Skip this replica if some expected PodCliques don't exist yet
	if len(notFoundPCLQFQNs) > 0 {
		skipPGSReplica = true
		return
	}
	breachedPCLQNames, minWaitFor = componentutils.GetMinAvailableBreachedPCLQInfo(pclqs, pgs.Spec.Template.TerminationDelay.Duration, since)
	return
}

// checkMinAvailableBreachAndDeletePGSReplicaPodGang evaluates MinAvailable breaches and triggers deletions.
// Returns replica indices requiring requeue and any errors encountered.
func (r _resource) checkMinAvailableBreachAndDeletePGSReplicaPodGang(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, terminationDelay time.Duration) ([]string, error) {
	now := time.Now()
	pgsObjectKey := client.ObjectKeyFromObject(pgs)
	pgsReplicaIndexRequiringRequeue := make([]string, 0, pgs.Spec.Replicas)
	deletionTasks := make([]utils.Task, 0, pgs.Spec.Replicas)
	// Check each PodGangSet replica for MinAvailable breaches
	for pgsReplicaIndex := range int(pgs.Spec.Replicas) {
		breachedPCSGNames, minPCSGWaitFor, err := r.getMinAvailableBreachedPCSGsForPGSReplica(ctx, pgsObjectKey, pgsReplicaIndex, terminationDelay, now)
		if err != nil {
			return nil, err
		}
		breachedPCLQNames, minPCLQWaitFor, skipPGSReplicaIndex, err := r.getMinAvailableBreachedPCLQsNotInPCSGForPGSReplica(ctx, pgs, pgsReplicaIndex, now)
		if err != nil {
			return nil, err
		}
		if skipPGSReplicaIndex {
			continue
		}

		// Delete replica if termination delay has passed
		if (len(breachedPCSGNames) > 0 && minPCSGWaitFor <= 0) ||
			(len(breachedPCLQNames) > 0 && minPCLQWaitFor <= 0) {
			// Terminate all PodCliques for this PGS replica index
			reason := fmt.Sprintf("Delete all PodCliques for PodGangSet %v with replicaIndex :%d due to MinAvailable breached longer than TerminationDelay: %s", pgsObjectKey, pgsReplicaIndex, terminationDelay)
			pclqGangTerminationTask := r.createPGSReplicaDeleteTask(logger, pgs, pgsReplicaIndex, reason)
			deletionTasks = append(deletionTasks, pclqGangTerminationTask)
		} else if len(breachedPCSGNames) > 0 || len(breachedPCLQNames) > 0 {
			// Mark replica for requeue if breached but termination delay not yet passed
			pgsReplicaIndexRequiringRequeue = append(pgsReplicaIndexRequiringRequeue, strconv.Itoa(pgsReplicaIndex))
		}
	}

	return pgsReplicaIndexRequiringRequeue, r.triggerDeletionOfPodCliques(ctx, logger, pgsObjectKey, deletionTasks)
}

// createPGSReplicaDeleteTask creates a task to delete all PodCliques for a specific PodGangSet replica.
func (r _resource) createPGSReplicaDeleteTask(logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int, reason string) utils.Task {
	return utils.Task{
		Name: fmt.Sprintf("DeletePGSReplicaPodCliques-%d", pgsReplicaIndex),
		Fn: func(ctx context.Context) error {
			if err := r.client.DeleteAllOf(ctx,
				&grovecorev1alpha1.PodClique{},
				client.InNamespace(pgs.Namespace),
				client.MatchingLabels(
					lo.Assign(
						k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgs.Name),
						map[string]string{
							grovecorev1alpha1.LabelPodGangSetReplicaIndex: strconv.Itoa(pgsReplicaIndex),
						},
					))); err != nil {
				logger.Error(err, "failed to delete PodCliques for PGS Replica index", "pgsReplicaIndex", pgsReplicaIndex, "reason", reason)
				r.eventRecorder.Eventf(pgs, corev1.EventTypeWarning, groveevents.ReasonPodGangSetReplicaDeletionFailed, "Error deleting PodGangSet replica %d: %v", pgsReplicaIndex, err)
				return err
			}
			logger.Info("Deleted PGS replica PodCliques", "pgsReplicaIndex", pgsReplicaIndex, "reason", reason)
			r.eventRecorder.Eventf(pgs, corev1.EventTypeNormal, groveevents.ReasonPodGangSetReplicaDeletionSuccessful, "PodGangSet replica %d deleted", pgsReplicaIndex)
			return nil
		},
	}
}

// triggerDeletionOfPodCliques executes deletion tasks concurrently.
func (r _resource) triggerDeletionOfPodCliques(ctx context.Context, logger logr.Logger, pgsObjKey client.ObjectKey, deletionTasks []utils.Task) error {
	if len(deletionTasks) == 0 {
		return nil
	}
	if runResult := utils.RunConcurrently(ctx, logger, deletionTasks); runResult.HasErrors() {
		return groveerr.WrapError(runResult.GetAggregatedError(),
			errDeletePodClique,
			component.OperationSync,
			fmt.Sprintf("Error deleting PodCliques for PodGangSet: %v", pgsObjKey.Name),
		)
	}
	logger.Info("Deleted PodCliques of PodGangSet", "pgsObjectKey", pgsObjKey)
	return nil
}

// createDeleteTasks creates deletion tasks for the specified PodClique names.
func (r _resource) createDeleteTasks(logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, targetPCLQNames []string) []utils.Task {
	deletionTasks := make([]utils.Task, 0, len(targetPCLQNames))
	for _, pclqName := range targetPCLQNames {
		pclqObjectKey := client.ObjectKey{
			Name:      pclqName,
			Namespace: pgs.Namespace,
		}
		pclq := emptyPodClique(pclqObjectKey)
		task := utils.Task{
			Name: "DeleteExcessPodClique-" + pclqName,
			Fn: func(ctx context.Context) error {
				if err := client.IgnoreNotFound(r.client.Delete(ctx, pclq)); err != nil {
					logger.Error(err, "failed to delete excess PodClique", "objectKey", pclqObjectKey)
					r.eventRecorder.Eventf(pgs, corev1.EventTypeWarning, groveevents.ReasonPodCliqueDeletionFailed, "Error deleting PodClique %v: %v", pclqObjectKey, err)
					return err
				}
				logger.Info("Deleted PodClique", "pclqObjectKey", pclqObjectKey)
				r.eventRecorder.Eventf(pgs, corev1.EventTypeNormal, groveevents.ReasonPodCliqueDeletionSuccessful, "Deleted PodClique: %s", pclqName)
				return nil
			},
		}
		deletionTasks = append(deletionTasks, task)
	}
	return deletionTasks
}

// getPodCliqueNamesToDelete identifies PodCliques that should be deleted based on replica index.
// PodCliques with replica indices >= pgsReplicas are marked for deletion.
func getPodCliqueNamesToDelete(pgsName string, pgsReplicas int, existingPCLQNames []string) ([]string, error) {
	pclqsToDelete := make([]string, 0, len(existingPCLQNames))
	for _, pclqName := range existingPCLQNames {
		extractedPGSReplica, err := utils.GetPodGangSetReplicaIndexFromPodCliqueFQN(pgsName, pclqName)
		if err != nil {
			return nil, groveerr.WrapError(err,
				errSyncPodClique,
				component.OperationSync,
				fmt.Sprintf("Failed to extract PodGangSet replica index from PodClique name: %s", pclqName),
			)
		}
		// Mark PodCliques with replica index >= current replica count for deletion
		if extractedPGSReplica >= pgsReplicas {
			pclqsToDelete = append(pclqsToDelete, pclqName)
		}
	}
	return pclqsToDelete, nil
}

// Delete deletes all resources that the PodClique Operator manages.
func (r _resource) Delete(ctx context.Context, logger logr.Logger, pgsObjectMeta metav1.ObjectMeta) error {
	logger.Info("Triggering deletion of PodCliques")
	existingPCLQNames, err := r.GetExistingResourceNames(ctx, logger, pgsObjectMeta)
	if err != nil {
		return groveerr.WrapError(err,
			errListPodClique,
			component.OperationDelete,
			fmt.Sprintf("Unable to fetch existing PodClique names for PodGangSet: %v", k8sutils.GetObjectKeyFromObjectMeta(pgsObjectMeta)),
		)
	}
	deleteTasks := make([]utils.Task, 0, len(existingPCLQNames))
	for _, pclqName := range existingPCLQNames {
		pclqObjectKey := client.ObjectKey{Name: pclqName, Namespace: pgsObjectMeta.Namespace}
		task := utils.Task{
			Name: "DeletePodClique-" + pclqName,
			Fn: func(ctx context.Context) error {
				if err := client.IgnoreNotFound(r.client.Delete(ctx, emptyPodClique(pclqObjectKey))); err != nil {
					return fmt.Errorf("failed to delete PodClique: %v for PodGangSet: %v with error: %w", pclqObjectKey, k8sutils.GetObjectKeyFromObjectMeta(pgsObjectMeta), err)
				}
				return nil
			},
		}
		deleteTasks = append(deleteTasks, task)
	}
	if runResult := utils.RunConcurrently(ctx, logger, deleteTasks); runResult.HasErrors() {
		logger.Error(runResult.GetAggregatedError(), "Error deleting PodCliques", "run summary", runResult.GetSummary())
		return groveerr.WrapError(runResult.GetAggregatedError(),
			errDeletePodClique,
			component.OperationDelete,
			fmt.Sprintf("Error deleting PodCliques for PodGangSet: %v", k8sutils.GetObjectKeyFromObjectMeta(pgsObjectMeta)),
		)
	}

	logger.Info("Deleted PodCliques")
	return nil
}

// doCreate creates a single PodClique resource with proper configuration and ownership.
func (r _resource) doCreate(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pgsReplica int32, pclqObjectKey client.ObjectKey) error {
	logger.Info("Running CreateOrUpdate PodClique", "pclqObjectKey", pclqObjectKey)
	pclq := emptyPodClique(pclqObjectKey)
	pgsObjKey := client.ObjectKeyFromObject(pgs)
	if err := r.buildResource(logger, pclq, pgs, int(pgsReplica)); err != nil {
		return err
	}
	if err := r.client.Create(ctx, pclq); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("PodClique creation failed for PodGangSet as it already exists", "pgs", pgsObjKey, "pclq", pclqObjectKey)
			return nil
		}
		r.eventRecorder.Eventf(pgs, corev1.EventTypeWarning, groveevents.ReasonPodCliqueCreationFailed, "PodClique %v creation failed: %v", pclqObjectKey, err)
		return groveerr.WrapError(err,
			errCodeCreatePodClique,
			component.OperationSync,
			fmt.Sprintf("Error creating PodClique: %v for PodGangSet: %v", pclqObjectKey, pgsObjKey),
		)
	}
	r.eventRecorder.Eventf(pgs, corev1.EventTypeNormal, groveevents.ReasonPodCliqueCreationSuccessful, "PodClique %v created successfully", pclqObjectKey)
	logger.Info("triggered create of PodClique for PodGangSet", "pgs", pgsObjKey, "pclqObjectKey", pclqObjectKey)
	return nil
}

// buildResource configures a PodClique resource based on the PodGangSet template.
// It sets metadata, labels, annotations, and startup dependencies.
func (r _resource) buildResource(logger logr.Logger, pclq *grovecorev1alpha1.PodClique, pgs *grovecorev1alpha1.PodGangSet, pgsReplica int) error {
	var err error
	pclqObjectKey, pgsObjectKey := client.ObjectKeyFromObject(pclq), client.ObjectKeyFromObject(pgs)
	// Find the matching template spec for this PodClique
	pclqTemplateSpec, foundAtIndex, ok := lo.FindIndexOf(pgs.Spec.Template.Cliques, func(pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec) bool {
		return strings.HasSuffix(pclq.Name, pclqTemplateSpec.Name)
	})
	if !ok {
		logger.Info("PodClique template spec not found in PodGangSet", "podCliqueObjectKey", pclqObjectKey, "podGangSetObjectKey", pgsObjectKey)
		return groveerr.New(errSyncPodClique,
			component.OperationSync,
			fmt.Sprintf("PodCliqueTemplateSpec for PodClique: %v not found in PodGangSet: %v", pclqObjectKey, pgsObjectKey),
		)
	}
	// Configure PodClique metadata and ownership
	if err = controllerutil.SetControllerReference(pgs, pclq, r.scheme); err != nil {
		return groveerr.WrapError(err,
			errSyncPodClique,
			component.OperationSync,
			fmt.Sprintf("Error setting controller reference for PodClique: %v", client.ObjectKeyFromObject(pclq)),
		)
	}
	pclq.Labels = getLabels(pgs, pgsReplica, pclqObjectKey, pclqTemplateSpec, grovecorev1alpha1.GeneratePodGangNameForPodCliqueOwnedByPodGangSet(pgs, pgsReplica))
	pclq.Annotations = pclqTemplateSpec.Annotations
	// Configure PodClique specification from template
	pclq.Spec = pclqTemplateSpec.Spec
	// Determine startup dependencies based on PodGangSet configuration
	var dependentPclqNames []string
	if dependentPclqNames, err = identifyFullyQualifiedStartupDependencyNames(pgs, pclq, pgsReplica, foundAtIndex); err != nil {
		return err
	}
	pclq.Spec.StartsAfter = dependentPclqNames
	return nil
}

// identifyFullyQualifiedStartupDependencyNames determines startup dependencies for a PodClique
// based on the PodGangSet's startup type (InOrder or Explicit).
func identifyFullyQualifiedStartupDependencyNames(pgs *grovecorev1alpha1.PodGangSet, pclq *grovecorev1alpha1.PodClique, pgsReplicaIndex, foundAtIndex int) ([]string, error) {
	cliqueStartupType := pgs.Spec.Template.StartupType
	// StartupType should never be nil due to defaulting webhook
	if cliqueStartupType == nil {
		return nil, groveerr.New(errSyncPodClique, component.OperationSync, fmt.Sprintf("PodClique: %v has nil StartupType", client.ObjectKeyFromObject(pclq)))
	}
	switch *cliqueStartupType {
	case grovecorev1alpha1.CliqueStartupTypeInOrder:
		return getInOrderStartupDependencies(pgs, pgsReplicaIndex, foundAtIndex), nil
	case grovecorev1alpha1.CliqueStartupTypeExplicit:
		return getExplicitStartupDependencies(pgs, pgsReplicaIndex, pclq), nil
	default:
		return nil, nil
	}
}

// getInOrderStartupDependencies returns dependencies for in-order startup.
// Each PodClique depends on the previous one in the template list.
func getInOrderStartupDependencies(pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex, foundAtIndex int) []string {
	if foundAtIndex == 0 {
		return nil
	}
	previousCliqueName := pgs.Spec.Template.Cliques[foundAtIndex-1].Name
	return componentutils.GenerateDependencyNamesForBasePodGang(pgs, pgsReplicaIndex, previousCliqueName)
}

// getExplicitStartupDependencies returns dependencies for explicit startup.
// Dependencies are explicitly defined in the PodClique's StartsAfter field.
func getExplicitStartupDependencies(pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int, pclq *grovecorev1alpha1.PodClique) []string {
	dependencies := make([]string, 0, len(pclq.Spec.StartsAfter))
	for _, dependency := range pclq.Spec.StartsAfter {
		dependencies = append(dependencies, componentutils.GenerateDependencyNamesForBasePodGang(pgs, pgsReplicaIndex, dependency)...)
	}
	return dependencies
}

// getPodCliqueSelectorLabels returns the label selector for PodCliques managed by a PodGangSet.
func getPodCliqueSelectorLabels(pgsObjectMeta metav1.ObjectMeta) map[string]string {
	return lo.Assign(
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsObjectMeta.Name),
		map[string]string{
			grovecorev1alpha1.LabelComponentKey: grovecorev1alpha1.LabelComponentPGSPodCliqueValue,
		},
	)
}

// getLabels constructs the complete set of labels for a PodClique resource.
// It combines template labels, default PodGangSet labels, and component-specific labels.
func getLabels(pgs *grovecorev1alpha1.PodGangSet, pgsReplica int, pclqObjectKey client.ObjectKey, pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec, podGangName string) map[string]string {
	// Component-specific labels for this PodClique
	pclqComponentLabels := map[string]string{
		grovecorev1alpha1.LabelAppNameKey:             pclqObjectKey.Name,
		grovecorev1alpha1.LabelComponentKey:           grovecorev1alpha1.LabelComponentPGSPodCliqueValue,
		grovecorev1alpha1.LabelPodGangSetReplicaIndex: strconv.Itoa(pgsReplica),
		grovecorev1alpha1.LabelPodGang:                podGangName,
	}
	return lo.Assign(
		pclqTemplateSpec.Labels,
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgs.Name),
		pclqComponentLabels,
	)
}

// emptyPodClique creates a minimal PodClique with only ObjectMeta set.
func emptyPodClique(objKey client.ObjectKey) *grovecorev1alpha1.PodClique {
	return &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objKey.Name,
			Namespace: objKey.Namespace,
		},
	}
}
