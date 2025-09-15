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

package podgangset

import (
	"context"
	"fmt"

	"github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	ctrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"
	"github.com/NVIDIA/grove/operator/internal/utils"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// triggerDeletionFlow orchestrates the complete deletion process for a PodGangSet.
// It executes deletion steps sequentially and handles any errors by recording incomplete deletion status.
func (r *Reconciler) triggerDeletionFlow(ctx context.Context, logger logr.Logger, pgs *v1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	// Define ordered deletion steps to execute sequentially
	deleteStepFns := []ctrlcommon.ReconcileStepFn[v1alpha1.PodGangSet]{
		r.recordDeletionStart,
		r.deletePodGangSetResources,
		r.verifyNoResourcesAwaitsCleanup,
		r.removeFinalizer,
	}
	// Execute each deletion step, short-circuiting on first error
	for _, fn := range deleteStepFns {
		if stepResult := fn(ctx, logger, pgs); ctrlcommon.ShortCircuitReconcileFlow(stepResult) {
			return r.recordIncompleteDeletion(ctx, logger, pgs, &stepResult)
		}
	}
	logger.Info("PodGangSet deleted successfully")
	return ctrlcommon.DoNotRequeue()
}

// recordDeletionStart marks the beginning of a deletion operation in the PodGangSet status.
func (r *Reconciler) recordDeletionStart(ctx context.Context, logger logr.Logger, pgs *v1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordStart(ctx, pgs, v1alpha1.LastOperationTypeDelete); err != nil {
		errMsg := "failed to record deletion start operation"
		logger.Error(err, errMsg, "PodGangSet", pgs)
		return ctrlcommon.ReconcileWithErrors(errMsg, err)
	}
	return ctrlcommon.ContinueReconcile()
}

// deletePodGangSetResources concurrently deletes all managed resources owned by the PodGangSet.
func (r *Reconciler) deletePodGangSetResources(ctx context.Context, logger logr.Logger, pgs *v1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	// Get all registered operators and prepare concurrent deletion tasks
	operators := r.operatorRegistry.GetAllOperators()
	deleteTasks := make([]utils.Task, 0, len(operators))
	// Create deletion task for each resource type
	for kind, operator := range operators {
		deleteTasks = append(deleteTasks, utils.Task{
			Name: fmt.Sprintf("delete-%s", kind),
			Fn: func(ctx context.Context) error {
				return operator.Delete(ctx, logger, pgs.ObjectMeta)
			},
		})
	}
	logger.Info("Triggering delete of PodGangSet resources")
	if runResult := utils.RunConcurrently(ctx, logger, deleteTasks); runResult.HasErrors() {
		deletionErr := runResult.GetAggregatedError()
		logger.Error(deletionErr, "Error deleting managed resources", "summary", runResult.GetSummary())
		return ctrlcommon.ReconcileWithErrors("error deleting managed resources", deletionErr)
	}
	return ctrlcommon.ContinueReconcile()
}

// verifyNoResourcesAwaitsCleanup ensures all managed resources have been successfully cleaned up.
func (r *Reconciler) verifyNoResourcesAwaitsCleanup(ctx context.Context, logger logr.Logger, pgs *v1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	return ctrlutils.VerifyNoResourceAwaitsCleanup(ctx, logger, r.operatorRegistry, pgs.ObjectMeta)
}

// removeFinalizer removes the PodGangSet finalizer to allow Kubernetes to complete object deletion.
func (r *Reconciler) removeFinalizer(ctx context.Context, logger logr.Logger, pgs *v1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	// Skip if finalizer is already removed
	if !controllerutil.ContainsFinalizer(pgs, v1alpha1.FinalizerPodGangSet) {
		logger.Info("Finalizer not found", "PodGangSet", pgs)
		return ctrlcommon.ContinueReconcile()
	}
	logger.Info("Removing finalizer", "PodGangSet", pgs, "finalizerName", v1alpha1.FinalizerPodGangSet)
	if err := ctrlutils.RemoveAndPatchFinalizer(ctx, r.client, pgs, v1alpha1.FinalizerPodGangSet); err != nil {
		return ctrlcommon.ReconcileWithErrors("error removing finalizer", fmt.Errorf("failed to remove finalizer: %s from PodGangSet: %v: %w", v1alpha1.FinalizerPodGangSet, client.ObjectKeyFromObject(pgs), err))
	}
	return ctrlcommon.ContinueReconcile()
}

// recordIncompleteDeletion records a failed deletion operation in the PodGangSet status and preserves original errors.
func (r *Reconciler) recordIncompleteDeletion(ctx context.Context, logger logr.Logger, pgs *v1alpha1.PodGangSet, errResult *ctrlcommon.ReconcileStepResult) ctrlcommon.ReconcileStepResult {
	if err := r.reconcileStatusRecorder.RecordCompletion(ctx, pgs, v1alpha1.LastOperationTypeDelete, errResult); err != nil {
		logger.Error(err, "failed to record deletion completion operation", "PodGangSet", pgs)
		// Combine original deletion errors with status recording error
		allErrs := append(errResult.GetErrors(), err)
		return ctrlcommon.ReconcileWithErrors("error recording incomplete reconciliation", allErrs...)
	}
	return *errResult
}
