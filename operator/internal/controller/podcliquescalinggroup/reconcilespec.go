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

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	ctrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// reconcileSpec performs the main reconciliation logic for PodCliqueScalingGroup specifications.
// It executes a series of reconciliation steps in order and returns early if any step fails.
func (r *Reconciler) reconcileSpec(ctx context.Context, logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) ctrlcommon.ReconcileStepResult {
	// Define the ordered sequence of reconciliation steps
	reconcileStepFns := []ctrlcommon.ReconcileStepFn[grovecorev1alpha1.PodCliqueScalingGroup]{
		r.ensureFinalizer,
		r.recordReconcileStart,
		r.syncPodCliqueScalingGroupResources,
		r.recordReconcileSuccess,
		r.updateObservedGeneration,
	}

	// Execute each reconciliation step in sequence, stopping early on failure
	for _, fn := range reconcileStepFns {
		if stepResult := fn(ctx, logger, pcsg); ctrlcommon.ShortCircuitReconcileFlow(stepResult) {
			return r.recordIncompleteReconcile(ctx, logger, pcsg, &stepResult)
		}
	}
	logger.Info("Finished spec reconciliation flow")
	return ctrlcommon.ContinueReconcile()
}

// ensureFinalizer adds the PodCliqueScalingGroup finalizer if it's not already present.
// This finalizer ensures proper cleanup during deletion.
func (r *Reconciler) ensureFinalizer(ctx context.Context, logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) ctrlcommon.ReconcileStepResult {
	if !controllerutil.ContainsFinalizer(pcsg, grovecorev1alpha1.FinalizerPodCliqueScalingGroup) {
		logger.Info("Adding finalizer", "finalizerName", grovecorev1alpha1.FinalizerPodCliqueScalingGroup)
		if err := ctrlutils.AddAndPatchFinalizer(ctx, r.client, pcsg, grovecorev1alpha1.FinalizerPodCliqueScalingGroup); err != nil {
			return ctrlcommon.ReconcileWithErrors("error adding finalizer", fmt.Errorf("failed to add finalizer: %s to PodCliqueScalingGroup: %v: %w", grovecorev1alpha1.FinalizerPodCliqueScalingGroup, client.ObjectKeyFromObject(pcsg), err))
		}
	}
	return ctrlcommon.ContinueReconcile()
}

// recordReconcileStart records the beginning of a reconcile operation in the resource status.
func (r *Reconciler) recordReconcileStart(ctx context.Context, logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordStart(ctx, pcsg, grovecorev1alpha1.LastOperationTypeReconcile); err != nil {
		logger.Error(err, "failed to record reconcile start operation")
		return ctrlcommon.ReconcileWithErrors("error recoding reconcile start", err)
	}
	return ctrlcommon.ContinueReconcile()
}

// syncPodCliqueScalingGroupResources synchronizes all managed resources for the PodCliqueScalingGroup.
// It iterates through all resource kinds in the correct order and applies their desired state.
func (r *Reconciler) syncPodCliqueScalingGroupResources(ctx context.Context, logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) ctrlcommon.ReconcileStepResult {
	// Process each resource kind in dependency order
	for _, kind := range getOrderedKindsForSync() {
		operator, err := r.operatorRegistry.GetOperator(kind)
		if err != nil {
			return ctrlcommon.ReconcileWithErrors(fmt.Sprintf("error getting operator for kind: %s", kind), err)
		}
		logger.Info("Syncing PodCliqueScalingGroup resource", "kind", kind)
		if err = operator.Sync(ctx, logger, pcsg); err != nil {
			// Check if this is a transient error that should trigger a retry
			if ctrlutils.ShouldRequeueAfter(err) {
				logger.Info("retrying sync due to component", "kind", kind, "syncRetryInterval", ctrlcommon.ComponentSyncRetryInterval)
				return ctrlcommon.ReconcileAfter(ctrlcommon.ComponentSyncRetryInterval, fmt.Sprintf("requeueing sync due to component %s after %s", kind, ctrlcommon.ComponentSyncRetryInterval))
			}
			logger.Error(err, "failed to sync PodGangSet resources", "kind", kind)
			return ctrlcommon.ReconcileWithErrors("error syncing managed resources", fmt.Errorf("failed to sync %s: %w", kind, err))
		}
	}
	return ctrlcommon.ContinueReconcile()
}

// recordReconcileSuccess records the successful completion of a reconcile operation in the resource status.
func (r *Reconciler) recordReconcileSuccess(ctx context.Context, logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordCompletion(ctx, pcsg, grovecorev1alpha1.LastOperationTypeReconcile, nil); err != nil {
		logger.Error(err, "failed to record reconcile success operation")
		return ctrlcommon.ReconcileWithErrors("error recording reconcile success", err)
	}
	return ctrlcommon.ContinueReconcile()
}

// recordIncompleteReconcile records the failure or incomplete status of a reconcile operation.
// It combines any existing errors with recording errors and returns the consolidated result.
func (r *Reconciler) recordIncompleteReconcile(ctx context.Context, logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, errStepResult *ctrlcommon.ReconcileStepResult) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordCompletion(ctx, pcsg, grovecorev1alpha1.LastOperationTypeReconcile, errStepResult); err != nil {
		logger.Error(err, "failed to record incomplete reconcile operation")
		// Combine the original reconcile errors with the recording error
		allErrs := append(errStepResult.GetErrors(), err)
		return ctrlcommon.ReconcileWithErrors("error recording incomplete reconciliation", allErrs...)
	}
	return *errStepResult
}

// updateObservedGeneration updates the status.ObservedGeneration field to match the current resource generation.
// This indicates that the controller has successfully processed the current resource specification.
func (r *Reconciler) updateObservedGeneration(ctx context.Context, logger logr.Logger, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) ctrlcommon.ReconcileStepResult {
	// Create a copy for the patch operation and update the observed generation
	original := pcsg.DeepCopy()
	pcsg.Status.ObservedGeneration = &pcsg.Generation
	if err := r.client.Status().Patch(ctx, pcsg, client.MergeFrom(original)); err != nil {
		logger.Error(err, "failed to patch status.ObservedGeneration")
		return ctrlcommon.ReconcileWithErrors("error updating observed generation", err)
	}
	logger.Info("patched status.ObservedGeneration", "ObservedGeneration", pcsg.Generation)
	return ctrlcommon.ContinueReconcile()
}

// getOrderedKindsForSync returns the resource kinds that need to be synchronized in dependency order.
// The order ensures that dependencies are created before dependents.
func getOrderedKindsForSync() []component.Kind {
	return []component.Kind{
		component.KindPodClique,
	}
}
