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

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	ctrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// reconcileSpec orchestrates the complete reconciliation of a PodClique's spec.
// It executes a series of reconciliation steps in order and handles any failures
// by recording incomplete reconciliation status.
func (r *Reconciler) reconcileSpec(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	rLog := logger.WithValues("operation", "spec-reconcile")

	// Define the ordered sequence of reconciliation steps
	reconcileStepFns := []ctrlcommon.ReconcileStepFn[grovecorev1alpha1.PodClique]{
		r.ensureFinalizer,
		r.recordReconcileStart,
		r.syncPCLQResources,
		r.recordReconcileSuccess,
		r.updateObservedGeneration,
	}

	// Execute each reconciliation step in sequence
	for _, fn := range reconcileStepFns {
		if stepResult := fn(ctx, rLog, pclq); ctrlcommon.ShortCircuitReconcileFlow(stepResult) {
			return r.recordIncompleteReconcile(ctx, logger, pclq, &stepResult)
		}
	}
	logger.Info("Finished spec reconciliation flow", "PodClique", client.ObjectKeyFromObject(pclq))
	return ctrlcommon.ContinueReconcile()
}

// ensureFinalizer adds the PodClique finalizer if it's not already present.
// The finalizer ensures proper cleanup during deletion. Returns requeue when finalizer is added.
func (r *Reconciler) ensureFinalizer(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	if !controllerutil.ContainsFinalizer(pclq, grovecorev1alpha1.FinalizerPodClique) {
		logger.Info("Adding finalizer", "PodClique", client.ObjectKeyFromObject(pclq), "finalizerName", grovecorev1alpha1.FinalizerPodClique)
		if err := ctrlutils.AddAndPatchFinalizer(ctx, r.client, pclq, grovecorev1alpha1.FinalizerPodClique); err != nil {
			return ctrlcommon.ReconcileWithErrors("error adding finalizer", err)
		}
		// Requeue to work with the updated object that now has the finalizer
		return ctrlcommon.Requeue("finalizer added, requeuing to continue reconciliation")
	}
	return ctrlcommon.ContinueReconcile()
}

// recordReconcileStart records the beginning of a reconciliation operation
// in the PodClique's status for observability and debugging.
func (r *Reconciler) recordReconcileStart(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordStart(ctx, pclq, grovecorev1alpha1.LastOperationTypeReconcile); err != nil {
		logger.Error(err, "failed to record reconcile start operation")
		return ctrlcommon.ReconcileWithErrors("error recoding reconcile start", err)
	}
	return ctrlcommon.ContinueReconcile()
}

// syncPCLQResources synchronizes all managed resources for the PodClique.
// It iterates through each resource kind in the defined order and applies
// the desired state using the appropriate component operators.
func (r *Reconciler) syncPCLQResources(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	for _, kind := range getOrderedKindsForSync() {
		// Get the component operator for this resource kind
		operator, err := r.operatorRegistry.GetOperator(kind)
		if err != nil {
			return ctrlcommon.ReconcileWithErrors(fmt.Sprintf("error getting operator for kind: %s", kind), err)
		}

		logger.Info("Syncing PodClique resources", "kind", kind)
		if err = operator.Sync(ctx, logger, pclq); err != nil {
			// Handle retryable errors with exponential backoff
			if ctrlutils.ShouldRequeueAfter(err) {
				logger.Info("retrying sync due to component", "kind", kind, "syncRetryInterval", ctrlcommon.ComponentSyncRetryInterval)
				return ctrlcommon.ReconcileAfter(ctrlcommon.ComponentSyncRetryInterval, fmt.Sprintf("requeueing sync due to component %s after %s", kind, ctrlcommon.ComponentSyncRetryInterval))
			}
			logger.Error(err, "failed to sync PodClique resources", "kind", kind)
			return ctrlcommon.ReconcileWithErrors("error syncing managed resources", fmt.Errorf("failed to sync %s: %w", kind, err))
		}
	}
	return ctrlcommon.ContinueReconcile()
}

// recordReconcileSuccess records the successful completion of a reconciliation
// operation in the PodClique's status.
func (r *Reconciler) recordReconcileSuccess(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordCompletion(ctx, pclq, grovecorev1alpha1.LastOperationTypeReconcile, nil); err != nil {
		logger.Error(err, "failed to record reconcile success operation")
		return ctrlcommon.ReconcileWithErrors("error recording reconcile success", err)
	}
	return ctrlcommon.ContinueReconcile()
}

// updateObservedGeneration updates the PodClique's status to reflect that
// the current generation has been successfully reconciled.
func (r *Reconciler) updateObservedGeneration(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	// Create a copy for the patch operation
	original := pclq.DeepCopy()
	pclq.Status.ObservedGeneration = &pclq.Generation
	if err := r.client.Status().Patch(ctx, pclq, client.MergeFrom(original)); err != nil {
		logger.Error(err, "failed to patch status.ObservedGeneration")
		return ctrlcommon.ReconcileWithErrors("error updating observed generation", err)
	}
	logger.Info("patched status.ObservedGeneration", "ObservedGeneration", pclq.Generation)
	return ctrlcommon.ContinueReconcile()
}

// recordIncompleteReconcile records a failed or incomplete reconciliation
// operation in the PodClique's status and combines any additional errors.
func (r *Reconciler) recordIncompleteReconcile(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique, errResult *ctrlcommon.ReconcileStepResult) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordCompletion(ctx, pclq, grovecorev1alpha1.LastOperationTypeReconcile, errResult); err != nil {
		logger.Error(err, "failed to record incomplete reconcile operation")
		// Combine all errors from the original failure and the recording failure
		allErrs := append(errResult.GetErrors(), err)
		return ctrlcommon.ReconcileWithErrors("error recording incomplete reconciliation", allErrs...)
	}
	return *errResult
}

// getOrderedKindsForSync returns the ordered list of component kinds that
// need to be synchronized during PodClique reconciliation.
func getOrderedKindsForSync() []component.Kind {
	return []component.Kind{
		component.KindPod,
	}
}
