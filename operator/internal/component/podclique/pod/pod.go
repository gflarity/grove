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

// Package pod provides the Pod component operator for managing Kubernetes Pods
// within a PodClique. It handles Pod creation, deletion, and lifecycle management
// including startup ordering, service discovery configuration, and Grove-specific
// environment variables.
package pod

import (
	"context"
	"fmt"
	"strconv"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	componentutils "github.com/NVIDIA/grove/operator/internal/component/utils"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	"github.com/NVIDIA/grove/operator/internal/expect"
	"github.com/NVIDIA/grove/operator/internal/utils"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Error codes for Pod component operations.
const (
	errCodeGetPod                              grovecorev1alpha1.ErrorCode = "ERR_GET_POD"
	errCodeDeletePod                           grovecorev1alpha1.ErrorCode = "ERR_DELETE_POD"
	errCodeGetAvailablePodHostNameIndices      grovecorev1alpha1.ErrorCode = "ERR_GET_AVAILABLE_POD_HOSTNAME_INDICES"
	errCodeGetPodGang                          grovecorev1alpha1.ErrorCode = "ERR_GET_PODGANG"
	errCodeGetPodGangSet                       grovecorev1alpha1.ErrorCode = "ERR_GET_PODGANGSET"
	errCodeGetPodClique                        grovecorev1alpha1.ErrorCode = "ERR_GET_PODCLIQUE"
	errCodeListPod                             grovecorev1alpha1.ErrorCode = "ERR_LIST_POD"
	errCodeRemovePodSchedulingGate             grovecorev1alpha1.ErrorCode = "ERR_REMOVE_POD_SCHEDULING_GATE"
	errCodeCreatePod                           grovecorev1alpha1.ErrorCode = "ERR_CREATE_POD"
	errCodeMissingPodGangLabelOnPCLQ           grovecorev1alpha1.ErrorCode = "ERR_MISSING_PODGANG_LABEL_ON_PODCLIQUE"
	errCodeInitContainerImageEnvVarMissing     grovecorev1alpha1.ErrorCode = "ERR_INITCONTAINER_ENVIRONMENT_VARIABLE_MISSING"
	errCodeCreatePodCliqueExpectationsStoreKey grovecorev1alpha1.ErrorCode = "ERR_CREATE_PODCLIQUE_EXPECTATIONS_STORE_KEY"
	errCodeDeletePodCliqueExpectations         grovecorev1alpha1.ErrorCode = "ERR_DELETE_PODCLIQUE_EXPECTATIONS_STORE_KEY"
	errCodeGetPodGangSetReplicaIndex           grovecorev1alpha1.ErrorCode = "ERR_GET_PODGANGSET_REPLICA_INDEX"
	errCodeSetControllerReference              grovecorev1alpha1.ErrorCode = "ERR_SET_CONTROLLER_REFERENCE"
	errCodeBuildPodResource                    grovecorev1alpha1.ErrorCode = "ERR_BUILD_POD_RESOURCE"
	errCodeMissingPodCliqueTemplate            grovecorev1alpha1.ErrorCode = "ERR_MISSING_PODCLIQUE_TEMPLATE"
	errCodeGetPodCliqueTemplate                grovecorev1alpha1.ErrorCode = "ERR_GET_PODCLIQUE_TEMPLATE"
	errCodeUpdatePodCliqueStatus               grovecorev1alpha1.ErrorCode = "ERR_UPDATE_PODCLIQUE_STATUS"
)

// Scheduling gate constants.
const (
	// podGangSchedulingGate is applied to pods to prevent scheduling until the PodGang is ready.
	podGangSchedulingGate = "grove.io/podgang-pending-creation"
)

// _resource implements the Pod component operator with necessary Kubernetes clients and utilities.
type _resource struct {
	// client provides access to the Kubernetes API
	client client.Client
	// scheme contains the runtime type information for Kubernetes objects
	scheme *runtime.Scheme
	// eventRecorder publishes events to the Kubernetes event stream
	eventRecorder record.EventRecorder
	// expectationsStore tracks expected resource operations to avoid race conditions
	expectationsStore *expect.ExpectationsStore
}

// New creates an instance of Pod component operator.
func New(client client.Client, scheme *runtime.Scheme, eventRecorder record.EventRecorder, expectationsStore *expect.ExpectationsStore) component.Operator[grovecorev1alpha1.PodClique] {
	return &_resource{
		client:            client,
		scheme:            scheme,
		eventRecorder:     eventRecorder,
		expectationsStore: expectationsStore,
	}
}

// GetExistingResourceNames returns the names of all the existing pods for the given PodClique.
// NOTE: Since we do not currently support Jobs, therefore we do not have to filter the pods that are reached their final state.
// Pods created for Jobs can reach corev1.PodSucceeded state or corev1.PodFailed state but these are not relevant for us at the moment.
// In future when these states become relevant then we have to list the pods and filter on their status.Phase.
func (r _resource) GetExistingResourceNames(ctx context.Context, _ logr.Logger, pclqObjMeta metav1.ObjectMeta) ([]string, error) {
	var podNames []string
	// List all Pod metadata in the namespace matching PodClique labels
	objMetaList := &metav1.PartialObjectMetadataList{}
	objMetaList.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
	if err := r.client.List(ctx,
		objMetaList,
		client.InNamespace(pclqObjMeta.Namespace),
		client.MatchingLabels(getSelectorLabelsForPods(pclqObjMeta)),
	); err != nil {
		return podNames, groveerr.WrapError(err,
			errCodeGetPod,
			component.OperationGetExistingResourceNames,
			"failed to list pods",
		)
	}
	// Filter pods to only include those controlled by this PodClique
	for _, pod := range objMetaList.Items {
		if metav1.IsControlledBy(&pod, &pclqObjMeta) {
			podNames = append(podNames, pod.Name)
		}
	}
	return podNames, nil
}

// Sync reconciles the desired state of Pods for a PodClique by creating missing Pods
// and removing scheduling gates from ready Pods.
func (r _resource) Sync(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) error {
	// Prepare sync context with current state and desired changes
	sc, err := r.prepareSyncFlow(ctx, logger, pclq)
	if err != nil {
		return err
	}
	// Execute the sync operations (create pods, remove scheduling gates)
	result := r.runSyncFlow(logger, sc)
	if result.hasErrors() {
		return result.getAggregatedError()
	}
	if result.hasPendingScheduleGatedPods() {
		return groveerr.New(groveerr.ErrCodeRequeueAfter,
			component.OperationSync,
			"some pods are still schedule gated. requeuing request to retry removal of scheduling gates",
		)
	}
	return nil
}

// buildResource constructs a Pod resource from a PodClique template, configuring
// metadata, labels, environment variables, hostname, and init containers as needed.
func (r _resource) buildResource(pgs *grovecorev1alpha1.PodGangSet, pclq *grovecorev1alpha1.PodClique, podGangName string, pod *corev1.Pod, podIndex int) error {
	// Extract PGS replica index from PodClique name for now (will be replaced with direct parameter)
	pgsName := componentutils.GetPodGangSetName(pclq.ObjectMeta)
	pgsReplicaIndex, err := utils.GetPodGangSetReplicaIndexFromPodCliqueFQN(pgsName, pclq.Name)
	if err != nil {
		return groveerr.WrapError(err,
			errCodeGetPodGangSetReplicaIndex,
			component.OperationSync,
			fmt.Sprintf("error extracting PGS replica index for PodClique %v", client.ObjectKeyFromObject(pclq)),
		)
	}

	// Configure Pod metadata with generated name and labels
	labels := getLabels(pclq.ObjectMeta, pgsName, podGangName, pgsReplicaIndex)
	pod.ObjectMeta = metav1.ObjectMeta{
		GenerateName: fmt.Sprintf("%s-", pclq.Name),
		Namespace:    pclq.Namespace,
		Labels:       labels,
		Annotations:  pclq.Annotations,
	}
	if err = controllerutil.SetControllerReference(pclq, pod, r.scheme); err != nil {
		return groveerr.WrapError(err,
			errCodeSetControllerReference,
			component.OperationSync,
			fmt.Sprintf("error setting controller reference of PodClique: %v on Pod", client.ObjectKeyFromObject(pclq)),
		)
	}
	// Copy PodSpec from template and add scheduling gate to prevent premature scheduling
	pod.Spec = *pclq.Spec.PodSpec.DeepCopy()
	pod.Spec.SchedulingGates = []corev1.PodSchedulingGate{{Name: podGangSchedulingGate}}
	// Add GROVE specific Pod environment variables
	addEnvironmentVariables(pod, pclq, pgsName, pgsReplicaIndex, podIndex)
	// Configure hostname and subdomain for service discovery
	configurePodHostname(pgsName, pgsReplicaIndex, pclq.Name, pod, podIndex)
	// Configure init container for startup ordering if dependencies are specified
	if len(pclq.Spec.StartsAfter) != 0 {
		return configurePodInitContainer(pgs, pclq, pod)
	}
	return nil
}

// Delete removes all Pods associated with a PodClique and cleans up expectations store entries.
func (r _resource) Delete(ctx context.Context, logger logr.Logger, pclqObjectMeta metav1.ObjectMeta) error {
	logger.Info("Triggering delete of all pods for the PodClique")
	if err := r.client.DeleteAllOf(ctx,
		&corev1.Pod{},
		client.InNamespace(pclqObjectMeta.Namespace),
		client.MatchingLabels(getSelectorLabelsForPods(pclqObjectMeta))); err != nil {
		return groveerr.WrapError(err,
			errCodeDeletePod,
			component.OperationDelete,
			fmt.Sprintf("failed to delete all pods for PodClique %v", k8sutils.GetObjectKeyFromObjectMeta(pclqObjectMeta)),
		)
	}
	// Clean up expectations store to prevent stale entries
	pclqExpStoreKey, err := getPodCliqueExpectationsStoreKey(logger, component.OperationDelete, pclqObjectMeta)
	if err != nil {
		return err
	}
	if err = r.expectationsStore.DeleteExpectations(logger, pclqExpStoreKey); err != nil {
		return groveerr.WrapError(err,
			errCodeDeletePodCliqueExpectations,
			component.OperationDelete,
			fmt.Sprintf("failed to delete expectations store for PodClique %v", pclqObjectMeta.Name))
	}
	logger.Info("Successfully deleted all pods for the PodClique")
	return nil
}

// getSelectorLabelsForPods returns the label selector used to identify Pods belonging to a PodClique.
func getSelectorLabelsForPods(pclqObjectMeta metav1.ObjectMeta) map[string]string {
	// Combine default PodGangSet labels with PodClique-specific label
	pgsName := k8sutils.GetFirstOwnerName(pclqObjectMeta)
	return lo.Assign(
		apicommon.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		map[string]string{
			apicommon.LabelPodClique: pclqObjectMeta.Name,
		},
	)
}

// getLabels constructs the complete set of labels for a Pod, combining default labels,
// PodClique labels, and Grove-specific labels.
func getLabels(pclqObjectMeta metav1.ObjectMeta, pgsName, podGangName string, pgsReplicaIndex int) map[string]string {
	// Build Grove-specific labels for Pod identification and grouping
	labels := map[string]string{
		apicommon.LabelPodClique:              pclqObjectMeta.Name,
		apicommon.LabelPodGangSetReplicaIndex: strconv.Itoa(pgsReplicaIndex),
		apicommon.LabelPodGang:                podGangName,
	}
	// Merge default labels, user labels, and Grove labels
	return lo.Assign(
		apicommon.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		pclqObjectMeta.Labels,
		labels,
	)
}

// addEnvironmentVariables adds Grove-specific environment variables to all containers and init-containers.
func addEnvironmentVariables(pod *corev1.Pod, pclq *grovecorev1alpha1.PodClique, pgsName string, pgsReplicaIndex, podIndex int) {
	// Define Grove-specific environment variables for service discovery and identification
	groveEnvVars := []corev1.EnvVar{
		{
			Name:  constants.EnvVarPGSName,
			Value: pgsName,
		},
		{
			Name:  constants.EnvVarPGSIndex,
			Value: strconv.Itoa(pgsReplicaIndex),
		},
		{
			Name:  constants.EnvVarPCLQName,
			Value: pclq.Name,
		},
		{
			Name: constants.EnvVarHeadlessService,
			Value: apicommon.GenerateHeadlessServiceAddress(
				apicommon.ResourceNameReplica{Name: pgsName, Replica: pgsReplicaIndex},
				pod.Namespace),
		},
		{
			Name:  constants.EnvVarPodIndex,
			Value: strconv.Itoa(podIndex),
		},
	}
	// Inject environment variables into all containers and init containers
	componentutils.AddEnvVarsToContainers(pod.Spec.Containers, groveEnvVars)
	componentutils.AddEnvVarsToContainers(pod.Spec.InitContainers, groveEnvVars)
}

// configurePodHostname sets the pod hostname and subdomain for service discovery
func configurePodHostname(pgsName string, pgsReplicaIndex int, pclqName string, pod *corev1.Pod, podIndex int) {
	// Set hostname for service discovery (e.g., "my-pclq-0")
	pod.Spec.Hostname = fmt.Sprintf("%s-%d", pclqName, podIndex)

	// Set subdomain to headless service name (reusing existing logic)
	pod.Spec.Subdomain = apicommon.GenerateHeadlessServiceName(
		apicommon.ResourceNameReplica{Name: pgsName, Replica: pgsReplicaIndex})
}
