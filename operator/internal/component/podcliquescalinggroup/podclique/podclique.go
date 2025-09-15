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
	"slices"
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

// Error codes used throughout the PodClique component for consistent error handling.
const (
	errCodeListPodClique              grovecorev1alpha1.ErrorCode = "ERR_LIST_PODCLIQUE"
	errCodeMissingStartupType         grovecorev1alpha1.ErrorCode = "ERR_UNDEFINED_STARTUP_TYPE"
	errCodeSetPodCliqueOwnerReference grovecorev1alpha1.ErrorCode = "ERR_SET_PODCLIQUE_OWNER_REFERENCE"
	errCodeBuildPodClique             grovecorev1alpha1.ErrorCode = "ERR_BUILD_PODCLIQUE"
	errCodeCreatePodCliques           grovecorev1alpha1.ErrorCode = "ERR_CREATE_PODCLIQUES"
	errCodeDeletePodClique            grovecorev1alpha1.ErrorCode = "ERR_DELETE_PODCLIQUE"
	errCodeGetPodGangSet              grovecorev1alpha1.ErrorCode = "ERR_GET_PODGANGSET"
	errCodeMissingPGSReplicaIndex     grovecorev1alpha1.ErrorCode = "ERR_MISSING_PODGANGSET_REPLICA_INDEX"
	errCodeReplicaIndexIntConversion  grovecorev1alpha1.ErrorCode = "ERR_PODGANGSET_REPLICA_INDEX_CONVERSION"
	errCodeListPodCliquesForPCSG      grovecorev1alpha1.ErrorCode = "ERR_LIST_PODCLIQUE_FOR_PCSG"
	errCodeCreatePodClique            grovecorev1alpha1.ErrorCode = "ERR_CREATE_PODCLIQUE"
)

// _resource implements the component.Operator interface for managing PodClique resources
// within a PodCliqueScalingGroup. It handles the lifecycle operations including creation,
// deletion, and synchronization of PodCliques based on scaling group specifications.
type _resource struct {
	client        client.Client
	scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
}

// New creates an instance of PodClique component operator.
func New(client client.Client, scheme *runtime.Scheme, eventRecorder record.EventRecorder) component.Operator[grovecorev1alpha1.PodCliqueScalingGroup] {
	return &_resource{
		client:        client,
		scheme:        scheme,
		eventRecorder: eventRecorder,
	}
}

// GetExistingResourceNames returns the names of all the existing resources that the PodClique Operator manages.
func (r _resource) GetExistingResourceNames(ctx context.Context, logger logr.Logger, pcsgObjMeta metav1.ObjectMeta) ([]string, error) {
	logger.Info("Looking for existing PodCliques managed by PodCliqueScalingGroup")
	pclqPartialObjMetaList, err := k8sutils.ListExistingPartialObjectMetadata(ctx,
		r.client,
		grovecorev1alpha1.SchemeGroupVersion.WithKind("PodClique"),
		pcsgObjMeta,
		getPodCliqueSelectorLabels(pcsgObjMeta))
	if err != nil {
		return nil, groveerr.WrapError(err,
			errCodeListPodClique,
			component.OperationGetExistingResourceNames,
			fmt.Sprintf("Error listing PodCliques for PodCliqueScalingGroup: %v", k8sutils.GetObjectKeyFromObjectMeta(pcsgObjMeta)),
		)
	}
	return k8sutils.FilterMapOwnedResourceNames(pcsgObjMeta, pclqPartialObjMetaList), nil
}

// Sync synchronizes all resources that the PodClique Operator manages.
func (r _resource) Sync(ctx context.Context, logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) error {
	// Calculate total expected PodCliques: replicas × clique names
	expectedPCLQs := int(pcsg.Spec.Replicas) * len(pcsg.Spec.CliqueNames)
	// Get the parent PodGangSet to access template specifications
	pgs, err := componentutils.GetOwnerPodGangSet(ctx, r.client, pcsg.ObjectMeta)
	if err != nil {
		return groveerr.WrapError(err,
			errCodeGetPodGangSet,
			component.OperationSync,
			fmt.Sprintf("failed to get owner PodGangSet for PodCliqueScalingGroup %s", client.ObjectKeyFromObject(pcsg)),
		)
	}

	existingPCLQs, err := r.getExistingPCLQs(ctx, pcsg)
	if err != nil {
		return err
	}
	// If there are excess PodCliques than expected, delete the ones that are no longer expected but existing.
	// This can happen when PCSG replicas have been scaled-in.
	// Clean up excess PodCliques when scaling down
	if err = r.triggerDeletionOfExcessPCLQs(ctx, logger, pgs, pcsg, existingPCLQs, expectedPCLQs); err != nil {
		return err
	}
	// Handle MinAvailable violations by terminating affected replica groups
	shouldRequeue, err := r.triggerDeletionOfMinAvailableBreachedPCSGReplicas(ctx, logger, pgs, pcsg)
	if err != nil {
		return err
	}
	// Create missing PodCliques to match desired state
	existingPCLQFQNs := lo.Map(existingPCLQs, func(pclq grovecorev1alpha1.PodClique, _ int) string { return pclq.Name })
	if err = r.createExpectedPCLQs(ctx, logger, pgs, pcsg, existingPCLQFQNs, expectedPCLQs); err != nil {
		return err
	}

	if shouldRequeue {
		return groveerr.New(groveerr.ErrCodeRequeueAfter,
			component.OperationSync,
			"Requeuing to re-process PCLQs that have breached MinAvailable but not crossed TerminationDelay",
		)
	}

	return nil
}

// getExistingPCLQs retrieves all PodCliques currently owned by the given PodCliqueScalingGroup.
func (r _resource) getExistingPCLQs(ctx context.Context, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) ([]grovecorev1alpha1.PodClique, error) {
	existingPCLQs, err := componentutils.GetPCLQsByOwner(ctx, r.client, grovecorev1alpha1.PodCliqueScalingGroupKind, client.ObjectKeyFromObject(pcsg), getPodCliqueSelectorLabels(pcsg.ObjectMeta))
	if err != nil {
		return nil, groveerr.WrapError(err,
			errCodeListPodCliquesForPCSG,
			component.OperationSync,
			fmt.Sprintf("Unable to fetch existing PodCliques for PodCliqueScalingGroup: %v", client.ObjectKeyFromObject(pcsg)),
		)
	}
	return existingPCLQs, nil
}

// triggerDeletionOfExcessPCLQs removes PodCliques that exceed the expected count,
// typically occurring during scale-down operations.
func (r _resource) triggerDeletionOfExcessPCLQs(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, existingPCLQs []grovecorev1alpha1.PodClique, numExpectedPCLQs int) error {
	// Calculate excess PodCliques that need deletion
	diff := len(existingPCLQs) - numExpectedPCLQs
	if diff > 0 {
		logger.Info("Found more PodCliques than expected, triggering deletion of excess PodCliques", "expected", numExpectedPCLQs, "existing", len(existingPCLQs), "diff", diff)
		// Identify replica indexes of PodCliques to delete
		deletionCandidatePodGangNames := getExcessPCSGReplicaIndexesToDelete(pcsg, existingPCLQs)
		reason := fmt.Sprintf("Delete excess PodCliques associated to PodGangs %v for PodCliqueScalingGroup %v", deletionCandidatePodGangNames, client.ObjectKeyFromObject(pcsg))
		deletionTasks := r.createDeleteTasks(logger, pgs, pcsg.Name, deletionCandidatePodGangNames, reason)
		return r.triggerDeletionOfPodCliques(ctx, logger, client.ObjectKeyFromObject(pcsg), deletionTasks)
	}
	return nil
}

// triggerDeletionOfMinAvailableBreachedPCSGReplicas handles deletion of PCSG replicas
// where MinAvailable constraints have been violated beyond the termination delay.
func (r _resource) triggerDeletionOfMinAvailableBreachedPCSGReplicas(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) (shouldRequeue bool, err error) {
	terminationDelay := pgs.Spec.Template.TerminationDelay.Duration
	pcsgReplicaIndexesToDelete, pcsgReplicaIndexesRequiringRequeue, err := r.findPCSGReplicasToDelete(ctx, pcsg, terminationDelay, logger)
	if err != nil {
		return
	}
	numMinAvailableBreachedPCSGReplicas := len(pcsgReplicaIndexesToDelete) + len(pcsgReplicaIndexesRequiringRequeue)
	// Delegate to PGS reconciler when all replicas are breached
	if numMinAvailableBreachedPCSGReplicas == int(pcsg.Spec.Replicas) {
		logger.Info("For each PCSG replica, MinAvailableBreached is true, delegating the responsibility to PGS reconciler to trigger the deletion of PGS replica.")
		return
	}
	if len(pcsgReplicaIndexesToDelete) > 0 {
		reason := fmt.Sprintf("Delete PodCliques %v for PodCliqueScalingGroup %v which have breached MinAvailable longer than TerminationDelay: %s", pcsgReplicaIndexesToDelete, client.ObjectKeyFromObject(pcsg), terminationDelay)
		pclqGangTerminationTasks := r.createDeleteTasks(logger, pgs, pcsg.Name, pcsgReplicaIndexesToDelete, reason)
		if err = r.triggerDeletionOfPodCliques(ctx, logger, client.ObjectKeyFromObject(pcsg), pclqGangTerminationTasks); err != nil {
			return
		}
	}
	if len(pcsgReplicaIndexesRequiringRequeue) > 0 {
		logger.Info("Found PCLQs with MinAvailable breached but which have not crossed TerminationDelay", "pclqs", pcsgReplicaIndexesRequiringRequeue, "terminationDelay", terminationDelay)
		shouldRequeue = true
		// return true to trigger a requeue
		return
	}
	return
}

// createExpectedPCLQs creates any missing PodCliques to match the desired replica count
// and clique configuration specified in the PodCliqueScalingGroup.
func (r _resource) createExpectedPCLQs(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, existingPCLQNames []string, numExpectedPCLQs int) error {
	// Build creation tasks for missing PodCliques
	tasks := make([]utils.Task, 0, numExpectedPCLQs)
	for pcsgReplicaIndex := range pcsg.Spec.Replicas {
		// Generate FQNs for PodCliques in this replica that match configured clique names
		pclqFQNs := lo.FilterMap(pgs.Spec.Template.Cliques, func(pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec, _ int) (string, bool) {
			pclqFQN := grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{
				Name:    pcsg.Name,
				Replica: int(pcsgReplicaIndex),
			}, pclqTemplateSpec.Name)
			return pclqFQN, slices.Contains(pcsg.Spec.CliqueNames, pclqTemplateSpec.Name)
		})

		// Create tasks for PodCliques that don't already exist
		for _, pclqFQN := range pclqFQNs {
			if slices.Contains(existingPCLQNames, pclqFQN) {
				continue
			}
			pclqObjectKey := client.ObjectKey{
				Name:      pclqFQN,
				Namespace: pcsg.Namespace,
			}
			createTask := utils.Task{
				Name: fmt.Sprintf("CreatePodClique-%s", pclqObjectKey),
				Fn: func(ctx context.Context) error {
					return r.doCreate(ctx, logger, pgs, pcsg, int(pcsgReplicaIndex), pclqObjectKey)
				},
			}
			tasks = append(tasks, createTask)
		}
	}
	if runResult := utils.RunConcurrently(ctx, logger, tasks); runResult.HasErrors() {
		return groveerr.WrapError(runResult.GetAggregatedError(),
			errCodeCreatePodCliques,
			component.OperationSync,
			fmt.Sprintf("Error Create of PodCliques for PodCliqueScalingGroup: %v, run summary: %s", client.ObjectKeyFromObject(pcsg), runResult.GetSummary()),
		)
	}
	return nil
}

// findPCSGReplicasToDelete identifies PCSG replicas that have violated MinAvailable constraints
// and determines which should be terminated immediately vs. requeued for later processing.
func (r _resource) findPCSGReplicasToDelete(ctx context.Context, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, terminationDelay time.Duration, logger logr.Logger) (pcsgReplicaIndexesToTerminate []string, pcsgReplicaIndexToRequeue []string, err error) {
	existingPCLQs, err := componentutils.GetPCLQsByOwner(ctx, r.client, grovecorev1alpha1.PodCliqueScalingGroupKind, client.ObjectKeyFromObject(pcsg), getPodCliqueSelectorLabels(pcsg.ObjectMeta))
	if err != nil {
		err = groveerr.WrapError(err,
			errCodeListPodCliquesForPCSG,
			component.OperationSync,
			fmt.Sprintf("Unable to fetch existing PodCliques for PodCliqueScalingGroup: %v", client.ObjectKeyFromObject(pcsg)),
		)
		return
	}
	now := time.Now()
	// Group PodCliques by their PCSG replica index for batch processing
	pcsgReplicaIndexPCLQs := componentutils.GroupPCLQsByPCSGReplicaIndex(existingPCLQs)
	// Check each replica group for MinAvailable violations. Those PCSG replicas should be marked for termination.
	for pcsgReplicaIndex, pclqs := range pcsgReplicaIndexPCLQs {
		pclqNames, minWaitFor := componentutils.GetMinAvailableBreachedPCLQInfo(pclqs, terminationDelay, now)
		if len(pclqNames) > 0 {
			logger.Info("minAvailable breached for PCLQs", "pcsgReplicaIndex", pcsgReplicaIndex, "pclqNames", pclqNames, "minWaitFor", minWaitFor)
			if minWaitFor <= 0 {
				pcsgReplicaIndexesToTerminate = append(pcsgReplicaIndexesToTerminate, pcsgReplicaIndex)
			} else {
				pcsgReplicaIndexToRequeue = append(pcsgReplicaIndexToRequeue, pcsgReplicaIndex)
			}
		}
	}
	return
}

// triggerDeletionOfPodCliques executes deletion tasks concurrently and handles any errors.
func (r _resource) triggerDeletionOfPodCliques(ctx context.Context, logger logr.Logger, pcsgObjectKey client.ObjectKey, deletionTasks []utils.Task) error {
	if runResult := utils.RunConcurrently(ctx, logger, deletionTasks); runResult.HasErrors() {
		return groveerr.WrapError(runResult.GetAggregatedError(),
			errCodeDeletePodClique,
			component.OperationSync,
			fmt.Sprintf("Error deleting PodCliques for PodCliqueScalingGroup: %v", pcsgObjectKey),
		)
	}
	logger.Info("Deleted PodCliques of PodCliqueScalingGroup", "pcsgObjectKey", pcsgObjectKey)
	return nil
}

// createDeleteTasks builds a list of deletion tasks for the specified PCSG replica indexes.
func (r _resource) createDeleteTasks(logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pcsgName string, pcsgReplicaIndexesToDelete []string, reason string) []utils.Task {
	deletionTasks := make([]utils.Task, 0, len(pcsgReplicaIndexesToDelete))
	for _, pcsgReplicaIndex := range pcsgReplicaIndexesToDelete {
		task := utils.Task{
			Name: "DeletePCSGReplicaPodCliques-" + pcsgReplicaIndex,
			Fn: func(ctx context.Context) error {
				if err := r.client.DeleteAllOf(ctx,
					&grovecorev1alpha1.PodClique{},
					client.InNamespace(pgs.Namespace),
					client.MatchingLabels(getLabelsToDeletePCSGReplicaIndexPCLQs(pgs.Name, pcsgName, pcsgReplicaIndex))); err != nil {
					r.eventRecorder.Eventf(pgs, corev1.EventTypeWarning, groveevents.ReasonPodCliqueScalingGroupReplicaDeletionFailed, "Error deleting PodCliqueScalingGroup %s Replica %s : %v", pcsgName, pcsgReplicaIndex, err)
					logger.Error(err, "failed to delete PodCliques for PCSG replica index", "pcsgReplicaIndex", pcsgReplicaIndex, "reason", reason)
					return err
				}
				r.eventRecorder.Eventf(pgs, corev1.EventTypeNormal, groveevents.ReasonPodCliqueScalingGroupReplicaDeletionSuccessful, "Deleted PodCliqueScalingGroup %s replica: %s", pcsgName, pcsgReplicaIndex)
				return nil
			},
		}
		deletionTasks = append(deletionTasks, task)
	}
	return deletionTasks
}

// getLabelsToDeletePCSGReplicaIndexPCLQs returns the label selector for deleting
// all PodCliques belonging to a specific PCSG replica index.
func getLabelsToDeletePCSGReplicaIndexPCLQs(pgsName, pcsgName, pcsgReplicaIndex string) map[string]string {
	return lo.Assign(
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		map[string]string{
			grovecorev1alpha1.LabelComponentKey:                      grovecorev1alpha1.LabelComponentPCSGPodCliqueValue,
			grovecorev1alpha1.LabelPodCliqueScalingGroup:             pcsgName,
			grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: pcsgReplicaIndex,
		},
	)
}

// getExcessPCSGReplicaIndexesToDelete identifies which PCSG replica indexes have
// PodCliques that are no longer expected and should be deleted.
func getExcessPCSGReplicaIndexesToDelete(pcsg *grovecorev1alpha1.PodCliqueScalingGroup, existingPCLQs []grovecorev1alpha1.PodClique) []string {
	// Get list of expected PodClique names based on current PCSG spec
	expectedPCLQNames := getExpectedPodCliqueFQNs(pcsg)
	// Find existing PodCliques that are no longer expected
	return lo.FilterMap(existingPCLQs, func(existingPCLQ grovecorev1alpha1.PodClique, _ int) (string, bool) {
		if slices.Contains(expectedPCLQNames, existingPCLQ.Name) {
			return "", false
		}
		pcsgReplicaIndex, ok := existingPCLQ.GetLabels()[grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex]
		if !ok {
			return "", false
		}
		return pcsgReplicaIndex, true
	})
}

// Delete deletes all resources that the PodClique Operator manages.
func (r _resource) Delete(ctx context.Context, logger logr.Logger, pcsgObjectMeta metav1.ObjectMeta) error {
	logger.Info("Triggering deletion of PodCliques managed by PodCliqueScalingGroup")
	// Get all PodClique names currently managed by this PCSG
	existingPCLQNames, err := r.GetExistingResourceNames(ctx, logger, pcsgObjectMeta)
	if err != nil {
		return groveerr.WrapError(err,
			errCodeListPodClique,
			component.OperationDelete,
			fmt.Sprintf("Unable to fetch existing PodClique names for PodCliqueScalingGroup: %v", k8sutils.GetObjectKeyFromObjectMeta(pcsgObjectMeta)),
		)
	}
	// Build deletion tasks for each existing PodClique
	deleteTasks := make([]utils.Task, 0, len(existingPCLQNames))
	for _, pclqName := range existingPCLQNames {
		pclqObjectKey := client.ObjectKey{Name: pclqName, Namespace: pcsgObjectMeta.Namespace}
		task := utils.Task{
			Name: "DeletePodClique-" + pclqName,
			Fn: func(ctx context.Context) error {
				if err := client.IgnoreNotFound(r.client.Delete(ctx, emptyPodClique(pclqObjectKey))); err != nil {
					return groveerr.WrapError(err,
						errCodeDeletePodClique,
						component.OperationDelete,
						fmt.Sprintf("Failed to delete PodClique: %v for PodCliqueScalingGroup: %v", pclqObjectKey, k8sutils.GetObjectKeyFromObjectMeta(pcsgObjectMeta)),
					)
				}
				return nil
			},
		}
		deleteTasks = append(deleteTasks, task)
	}
	if runResult := utils.RunConcurrently(ctx, logger, deleteTasks); runResult.HasErrors() {
		logger.Error(runResult.GetAggregatedError(), "Error deleting PodCliques", "run summary", runResult.GetSummary())
		return groveerr.WrapError(runResult.GetAggregatedError(),
			errCodeDeletePodClique,
			component.OperationDelete,
			fmt.Sprintf("Error deleting PodCliques for PodCliqueScalingGroup: %v", k8sutils.GetObjectKeyFromObjectMeta(pcsgObjectMeta)),
		)
	}

	logger.Info("Deleted PodCliques belonging to PodCliqueScalingGroup")
	return nil
}

// getPCSGTemplateNumPods calculates the total number of pods across all cliques
// in the PCSG template by summing replicas from matching PodClique templates.
func (r _resource) getPCSGTemplateNumPods(pgs *grovecorev1alpha1.PodGangSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) int {
	var pcsgTemplateNumPods int
	// Create lookup map for PodClique templates by name
	pcMap := make(map[string]*grovecorev1alpha1.PodCliqueTemplateSpec, len(pgs.Spec.Template.Cliques))
	for _, pclqTemplateSpec := range pgs.Spec.Template.Cliques {
		pcMap[pclqTemplateSpec.Name] = pclqTemplateSpec
	}
	for _, pclqTemplateName := range pcsg.Spec.CliqueNames {
		pclqTemplateSpec, ok := pcMap[pclqTemplateName]
		if !ok {
			continue
		}
		pcsgTemplateNumPods += int(pclqTemplateSpec.Spec.Replicas)
	}
	return pcsgTemplateNumPods
}

// doCreate handles the creation of a single PodClique resource, including building
// its specification and establishing ownership relationships.
func (r _resource) doCreate(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pcsgReplicaIndex int, pclqObjectKey client.ObjectKey) error {
	logger.Info("Running CreateOrUpdate PodClique", "pclqObjectKey", pclqObjectKey)
	// Initialize empty PodClique with basic metadata
	pclq := emptyPodClique(pclqObjectKey)
	pcsgObjKey := client.ObjectKeyFromObject(pclq)
	if err := r.buildResource(logger, pgs, pcsg, pcsgReplicaIndex, pclq); err != nil {
		return err
	}
	if err := r.client.Create(ctx, pclq); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("PodClique creation failed as it already exists", "pclq", pclqObjectKey)
			return nil
		}
		r.eventRecorder.Eventf(pcsg, corev1.EventTypeWarning, groveevents.ReasonPodCliqueCreationFailed, "PodClique %v creation failed: %v", pclqObjectKey, err)
		return groveerr.WrapError(err,
			errCodeCreatePodClique,
			component.OperationSync,
			fmt.Sprintf("Error creating PodClique: %v for PodCliqueScalingGroup: %v", pclqObjectKey, pcsgObjKey),
		)
	}
	r.eventRecorder.Eventf(pcsg, corev1.EventTypeNormal, groveevents.ReasonPodCliqueCreationSuccessful, "PodClique %v created successfully", pclqObjectKey)
	logger.Info("Successfully created PodClique", "pclqObjectKey", pclqObjectKey)
	return nil
}

// buildResource populates a PodClique resource with the appropriate specification,
// labels, annotations, and dependency relationships based on the template.
func (r _resource) buildResource(logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pcsgReplicaIndex int, pclq *grovecorev1alpha1.PodClique) error {
	var err error
	pclqObjectKey, pgsObjectKey := client.ObjectKeyFromObject(pclq), client.ObjectKeyFromObject(pgs)
	// Find the matching PodClique template specification
	pclqTemplateSpec, foundAtIndex, ok := lo.FindIndexOf(pgs.Spec.Template.Cliques, func(pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec) bool {
		return strings.HasSuffix(pclq.Name, pclqTemplateSpec.Name)
	})
	if !ok {
		logger.Info("Error building PodClique resource, PodClique template spec not found in PodGangSet", "podCliqueObjectKey", pclqObjectKey, "podGangSetObjectKey", pgsObjectKey)
		return groveerr.New(errCodeBuildPodClique,
			component.OperationSync,
			fmt.Sprintf("Error building PodClique resource, PodCliqueTemplateSpec for PodClique: %v not found in PodGangSet: %v", pclqObjectKey, pgsObjectKey),
		)
	}
	// Establish ownership relationship with PCSG
	if err = controllerutil.SetControllerReference(pcsg, pclq, r.scheme); err != nil {
		return groveerr.WrapError(err,
			errCodeSetPodCliqueOwnerReference,
			component.OperationSync,
			fmt.Sprintf("Error setting controller reference for PodClique: %v", client.ObjectKeyFromObject(pclq)),
		)
	}

	pgsReplicaIndex, err := getPGSReplicaFromPCSG(pcsg)
	if err != nil {
		return err
	}

	// Generate PodGang name for this PodClique
	podGangName := grovecorev1alpha1.GeneratePodGangNameForPodCliqueOwnedByPCSG(pgs, pgsReplicaIndex, pcsg, pcsgReplicaIndex)

	// Set metadata from template and generated values
	pclq.Labels = getLabels(pgs, pgsReplicaIndex, pcsg, pcsgReplicaIndex, pclqObjectKey, pclqTemplateSpec, podGangName)
	pclq.Annotations = pclqTemplateSpec.Annotations
	// Copy base specification from template
	pclq.Spec = pclqTemplateSpec.Spec
	// Add PCSG-specific environment variables to containers
	pcsgTemplateNumPods := r.getPCSGTemplateNumPods(pgs, pcsg)
	r.addEnvironmentVariablesToPodContainerSpecs(pclq, pcsgTemplateNumPods)
	// Resolve startup dependencies based on template ordering
	dependentPCLQNames, err := identifyFullyQualifiedStartupDependencyNames(pgs, pgsReplicaIndex, pcsg, pcsgReplicaIndex, pclq, foundAtIndex)
	if err != nil {
		return err
	}
	pclq.Spec.StartsAfter = dependentPCLQNames
	return nil
}

// addEnvironmentVariablesToPodContainerSpecs injects PCSG-specific environment variables
// into all containers and init containers in the PodClique specification.
func (r _resource) addEnvironmentVariablesToPodContainerSpecs(pclq *grovecorev1alpha1.PodClique, pcsgTemplateNumPods int) {
	// Define PCSG-specific environment variables
	pcsgEnvVars := []corev1.EnvVar{
		{
			Name: grovecorev1alpha1.EnvVarPCSGName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.labels['%s']", grovecorev1alpha1.LabelPodCliqueScalingGroup),
				},
			},
		},
		{
			Name:  grovecorev1alpha1.EnvVarPCSGTemplateNumPods,
			Value: strconv.Itoa(pcsgTemplateNumPods),
		},
		{
			Name: grovecorev1alpha1.EnvVarPCSGIndex,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.labels['%s']", grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex),
				},
			},
		},
	}
	pclqObjPodSpec := &pclq.Spec.PodSpec
	componentutils.AddEnvVarsToContainers(pclqObjPodSpec.Containers, pcsgEnvVars)
	componentutils.AddEnvVarsToContainers(pclqObjPodSpec.InitContainers, pcsgEnvVars)
}

// getExpectedPodCliqueFQNs generates the fully qualified names for all PodCliques
// that should exist based on the current PCSG specification.
func getExpectedPodCliqueFQNs(pcsg *grovecorev1alpha1.PodCliqueScalingGroup) []string {
	expectedPCLQFQNs := make([]string, 0, int(pcsg.Spec.Replicas)*len(pcsg.Spec.CliqueNames))
	for pcsgReplicaIndex := range pcsg.Spec.Replicas {
		pclqFQNs := lo.Map(pcsg.Spec.CliqueNames, func(cliqueName string, _ int) string {
			return grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{
				Name:    pcsg.Name,
				Replica: int(pcsgReplicaIndex),
			}, cliqueName)
		})
		expectedPCLQFQNs = append(expectedPCLQFQNs, pclqFQNs...)
	}
	return expectedPCLQFQNs
}

// getPGSReplicaFromPCSG extracts the PodGangSet replica index from the PCSG's labels.
func getPGSReplicaFromPCSG(pcsg *grovecorev1alpha1.PodCliqueScalingGroup) (int, error) {
	pgsReplicaIndex, ok := pcsg.GetLabels()[grovecorev1alpha1.LabelPodGangSetReplicaIndex]
	if !ok {
		return 0, groveerr.New(errCodeMissingPGSReplicaIndex, component.OperationSync, fmt.Sprintf("failed to get the PodGangSet replica ind value from the labels for PodCliqueScalingGroup %s", client.ObjectKeyFromObject(pcsg)))
	}
	pgsReplica, err := strconv.Atoi(pgsReplicaIndex)
	if err != nil {
		return 0, groveerr.WrapError(err,
			errCodeReplicaIndexIntConversion,
			component.OperationSync,
			"failed to convert replica index value from string to integer",
		)
	}
	return pgsReplica, nil
}

// identifyFullyQualifiedStartupDependencyNames resolves the startup dependencies
// for a PodClique based on the configured startup type (in-order or explicit).
func identifyFullyQualifiedStartupDependencyNames(pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pcsgReplicaIndex int, pclq *grovecorev1alpha1.PodClique, foundAtIndex int) ([]string, error) {
	cliqueStartupType := pgs.Spec.Template.StartupType
	// Validate startup type is configured (should be set by defaulting webhook)
	if cliqueStartupType == nil {
		return nil, groveerr.New(errCodeMissingStartupType, component.OperationSync, fmt.Sprintf("PodClique: %v has nil StartupType", client.ObjectKeyFromObject(pclq)))
	}
	switch *cliqueStartupType {
	case grovecorev1alpha1.CliqueStartupTypeInOrder:
		return getInOrderStartupDependencies(pgs, pgsReplicaIndex, pcsg, pcsgReplicaIndex, foundAtIndex), nil
	case grovecorev1alpha1.CliqueStartupTypeExplicit:
		return getExplicitStartupDependencies(pgs, pgsReplicaIndex, pcsg, pcsgReplicaIndex, pclq), nil
	default:
		return nil, nil
	}
}

// getInOrderStartupDependencies calculates startup dependencies for in-order startup type,
// where PodCliques start in the order they are defined in the template.
func getInOrderStartupDependencies(pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pcsgReplicaIndex, foundAtIndex int) []string {
	// First clique has no dependencies
	if foundAtIndex == 0 {
		return nil
	}
	// Get the name of the previous clique in the template order
	parentCliqueName := pgs.Spec.Template.Cliques[foundAtIndex-1].Name

	// Handle base PodGang dependencies differently
	if pcsgReplicaIndex < int(*pcsg.Spec.MinAvailable) {
		return componentutils.GenerateDependencyNamesForBasePodGang(pgs, pgsReplicaIndex, parentCliqueName)
	}

	// Skip dependencies on cliques not in this PCSG (scaled PodGangs only depend on their own cliques)
	if !slices.Contains(pcsg.Spec.CliqueNames, parentCliqueName) {
		return nil
	}

	return []string{
		grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{Name: pcsg.Name, Replica: pcsgReplicaIndex}, parentCliqueName),
	}
}

// getExplicitStartupDependencies resolves startup dependencies for explicit startup type,
// where dependencies are explicitly defined in the PodClique template.
func getExplicitStartupDependencies(pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pcsgReplicaIndex int, pclq *grovecorev1alpha1.PodClique) []string {
	parentCliqueNames := make([]string, 0, len(pclq.Spec.StartsAfter))
	// Handle base PodGang explicit dependencies
	if pcsgReplicaIndex < int(*pcsg.Spec.MinAvailable) {
		for _, dependency := range pclq.Spec.StartsAfter {
			parentCliqueNames = append(parentCliqueNames, componentutils.GenerateDependencyNamesForBasePodGang(pgs, pgsReplicaIndex, dependency)...)
		}
		return parentCliqueNames
	}

	// Process explicit dependencies for scaled PodGangs
	for _, dependency := range pclq.Spec.StartsAfter {
		// Only include dependencies within this PCSG
		if !slices.Contains(pcsg.Spec.CliqueNames, dependency) {
			continue
		}
		parentCliqueNames = append(parentCliqueNames, grovecorev1alpha1.GeneratePodCliqueName(grovecorev1alpha1.ResourceNameReplica{Name: pcsg.Name, Replica: pcsgReplicaIndex}, dependency))
	}
	return parentCliqueNames
}

// getPodCliqueSelectorLabels returns the label selector for finding PodCliques
// managed by a specific PodCliqueScalingGroup.
func getPodCliqueSelectorLabels(pcsgObjectMeta metav1.ObjectMeta) map[string]string {
	pgsName := componentutils.GetPodGangSetName(pcsgObjectMeta)
	return lo.Assign(
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		map[string]string{
			grovecorev1alpha1.LabelComponentKey:          grovecorev1alpha1.LabelComponentPCSGPodCliqueValue,
			grovecorev1alpha1.LabelPodCliqueScalingGroup: pcsgObjectMeta.Name,
		},
	)
}

// getLabels constructs the complete set of labels for a PodClique, combining
// template labels with component-specific labels for tracking and selection.
func getLabels(pgs *grovecorev1alpha1.PodGangSet, pgsReplicaIndex int, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pcsgReplicaIndex int, pclqObjectKey client.ObjectKey, pclqTemplateSpec *grovecorev1alpha1.PodCliqueTemplateSpec, podGangName string) map[string]string {
	pclqComponentLabels := map[string]string{
		grovecorev1alpha1.LabelAppNameKey:                        pclqObjectKey.Name,
		grovecorev1alpha1.LabelComponentKey:                      grovecorev1alpha1.LabelComponentPCSGPodCliqueValue,
		grovecorev1alpha1.LabelPodCliqueScalingGroup:             pcsg.Name,
		grovecorev1alpha1.LabelPodGang:                           podGangName,
		grovecorev1alpha1.LabelPodGangSetReplicaIndex:            strconv.Itoa(pgsReplicaIndex),
		grovecorev1alpha1.LabelPodCliqueScalingGroupReplicaIndex: strconv.Itoa(pcsgReplicaIndex),
	}

	// Add base PodGang label for scaled PodGangs to maintain relationships
	basePodGangName := grovecorev1alpha1.GenerateBasePodGangName(
		grovecorev1alpha1.ResourceNameReplica{Name: pgs.Name, Replica: pgsReplicaIndex},
	)
	if podGangName != basePodGangName {
		// This pod belongs to a scaled PodGang - add the base PodGang label
		pclqComponentLabels[grovecorev1alpha1.LabelBasePodGang] = basePodGangName
	}

	return lo.Assign(
		pclqTemplateSpec.Labels,
		k8sutils.GetDefaultLabelsForPodGangSetManagedResources(pgs.Name),
		pclqComponentLabels,
	)
}

// emptyPodClique creates a new PodClique resource with only basic metadata populated.
func emptyPodClique(objKey client.ObjectKey) *grovecorev1alpha1.PodClique {
	return &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objKey.Name,
			Namespace: objKey.Namespace,
		},
	}
}
