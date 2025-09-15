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

// Package podgangset provides PodGangSet reconciliation logic for managing
// the desired state of PodGangSet resources and their associated components.
package podgangset

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

// reconcileSpec performs the main reconciliation of PodGangSet desired state.
// It executes a series of reconciliation steps in order and handles any errors
// by recording incomplete reconciliation status.
func (r *Reconciler) reconcileSpec(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	rLog := logger.WithValues("operation", "spec-reconcile")

	// Define the ordered steps for reconciling PodGangSet spec
	reconcileStepFns := []ctrlcommon.ReconcileStepFn[grovecorev1alpha1.PodGangSet]{
		r.ensureFinalizer,
		r.recordReconcileStart,
		r.syncPodGangSetResources,
		r.recordReconcileSuccess,
		r.updateObservedGeneration,
	}

	// Execute each reconciliation step in sequence
	for _, fn := range reconcileStepFns {
		if stepResult := fn(ctx, rLog, pgs); ctrlcommon.ShortCircuitReconcileFlow(stepResult) {
			return r.recordIncompleteReconcile(ctx, logger, pgs, &stepResult)
		}
	}
	logger.Info("Finished spec reconciliation flow", "PodGangSet", client.ObjectKeyFromObject(pgs))
	return ctrlcommon.ContinueReconcile()
}

// ensureFinalizer adds the PodGangSet finalizer if it's not already present.
// The finalizer ensures proper cleanup when the PodGangSet is being deleted.
func (r *Reconciler) ensureFinalizer(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	if !controllerutil.ContainsFinalizer(pgs, grovecorev1alpha1.FinalizerPodGangSet) {
		logger.Info("Adding finalizer", "finalizerName", grovecorev1alpha1.FinalizerPodGangSet)
		if err := ctrlutils.AddAndPatchFinalizer(ctx, r.client, pgs, grovecorev1alpha1.FinalizerPodGangSet); err != nil {
			return ctrlcommon.ReconcileWithErrors("error adding finalizer", fmt.Errorf("failed to add finalizer: %s to PodGangSet: %v: %w", grovecorev1alpha1.FinalizerPodGangSet, client.ObjectKeyFromObject(pgs), err))
		}
	}
	return ctrlcommon.ContinueReconcile()
}

// recordReconcileStart records the start of a reconcile operation in the PodGangSet status.
// This provides visibility into the current state of the reconciliation process.
func (r *Reconciler) recordReconcileStart(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordStart(ctx, pgs, grovecorev1alpha1.LastOperationTypeReconcile); err != nil {
		logger.Error(err, "failed to record reconcile start operation")
		return ctrlcommon.ReconcileWithErrors("error recoding reconcile start", err)
	}
	return ctrlcommon.ContinueReconcile()
}

// syncPodGangSetResources synchronizes all managed resources for the PodGangSet.
// It processes each component type in a specific order to handle dependencies correctly
// and manages various error conditions with appropriate retry strategies.
func (r *Reconciler) syncPodGangSetResources(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	continueReconcileAndRequeueKinds := make([]component.Kind, 0)

	// Sync each component kind in dependency order
	for _, kind := range getOrderedKindsForSync() {
		operator, err := r.operatorRegistry.GetOperator(kind)
		if err != nil {
			return ctrlcommon.ReconcileWithErrors(fmt.Sprintf("error getting operator for kind: %s", kind), err)
		}
		logger.Info("Syncing PodGangSet resource", "kind", kind)
		if err = operator.Sync(ctx, logger, pgs); err != nil {
			// Handle different error types with appropriate retry strategies
			if ctrlutils.ShouldContinueReconcileAndRequeue(err) {
				logger.Info("continuing sync due to component", "kind", kind)
				continueReconcileAndRequeueKinds = append(continueReconcileAndRequeueKinds, kind)
				continue
			}
			if ctrlutils.ShouldRequeueAfter(err) {
				logger.Info("retrying sync due to component", "kind", kind, "syncRetryInterval", ctrlcommon.ComponentSyncRetryInterval)
				return ctrlcommon.ReconcileAfter(ctrlcommon.ComponentSyncRetryInterval, fmt.Sprintf("requeueing sync due to component %s after %s", kind, ctrlcommon.ComponentSyncRetryInterval))
			}
			logger.Error(err, "failed to sync PodGangSet resources", "kind", kind)
			return ctrlcommon.ReconcileWithErrors("error syncing managed resources", fmt.Errorf("failed to sync %s: %w", kind, err))
		}
	}

	// Schedule requeue if any components need continuation
	if len(continueReconcileAndRequeueKinds) > 0 {
		return ctrlcommon.ReconcileAfter(ctrlcommon.ComponentSyncRetryInterval, fmt.Sprintf("requeueing sync due to component(s) %v after %s", continueReconcileAndRequeueKinds, ctrlcommon.ComponentSyncRetryInterval))
	}
	return ctrlcommon.ContinueReconcile()
}

// recordReconcileSuccess records the successful completion of a reconcile operation
// in the PodGangSet status, indicating that all resources have been synchronized.
func (r *Reconciler) recordReconcileSuccess(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordCompletion(ctx, pgs, grovecorev1alpha1.LastOperationTypeReconcile, nil); err != nil {
		logger.Error(err, "failed to record reconcile success operation")
		return ctrlcommon.ReconcileWithErrors("error recording reconcile success", err)
	}
	return ctrlcommon.ContinueReconcile()
}

// updateObservedGeneration updates the status.ObservedGeneration field to match
// the current generation, indicating that the controller has processed this version.
func (r *Reconciler) updateObservedGeneration(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	original := pgs.DeepCopy()
	pgs.Status.ObservedGeneration = &pgs.Generation
	if err := r.client.Status().Patch(ctx, pgs, client.MergeFrom(original)); err != nil {
		logger.Error(err, "failed to patch status.ObservedGeneration")
		return ctrlcommon.ReconcileWithErrors("error updating observed generation", err)
	}
	logger.Info("patched status.ObservedGeneration", "ObservedGeneration", pgs.Generation)
	return ctrlcommon.ContinueReconcile()
}

// recordIncompleteReconcile records when a reconcile operation fails or is incomplete,
// capturing the error details in the PodGangSet status for observability.
func (r *Reconciler) recordIncompleteReconcile(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet, errResult *ctrlcommon.ReconcileStepResult) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordCompletion(ctx, pgs, grovecorev1alpha1.LastOperationTypeReconcile, errResult); err != nil {
		logger.Error(err, "failed to record incomplete reconcile operation")
		// Combine all errors to provide complete error context
		allErrs := append(errResult.GetErrors(), err)
		return ctrlcommon.ReconcileWithErrors("error recording incomplete reconciliation", allErrs...)
	}
	return *errResult
}

// getOrderedKindsForSync returns the component kinds in dependency order for synchronization.
// The ordering ensures that prerequisite resources (RBAC, secrets, services) are created
// before dependent resources (HPAs, PodCliques, PodGangs).
func getOrderedKindsForSync() []component.Kind {
	return []component.Kind{
		// RBAC components must be created first
		component.KindServiceAccount,
		component.KindRole,
		component.KindRoleBinding,
		component.KindServiceAccountTokenSecret,
		// Networking components
		component.KindHeadlessService,
		// Scaling and workload components
		component.KindHorizontalPodAutoscaler,
		component.KindPodClique,
		component.KindPodCliqueScalingGroup,
		component.KindPodGang,
	}
}
