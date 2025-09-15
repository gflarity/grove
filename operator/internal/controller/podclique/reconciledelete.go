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
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	ctrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"
	"github.com/NVIDIA/grove/operator/internal/utils"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// triggerDeletionFlow orchestrates the complete deletion process for a PodClique resource.
// It executes deletion steps sequentially and short-circuits on any failure.
func (r *Reconciler) triggerDeletionFlow(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	// Define the ordered sequence of deletion steps
	deleteStepFns := []ctrlcommon.ReconcileStepFn[grovecorev1alpha1.PodClique]{
		r.deletePodCliqueResources,
		r.verifyNoResourcesAwaitsCleanup,
		r.removeFinalizer,
	}
	// Execute each deletion step, stopping on first failure
	for _, fn := range deleteStepFns {
		if stepResult := fn(ctx, logger, pclq); ctrlcommon.ShortCircuitReconcileFlow(stepResult) {
			return stepResult
		}
	}
	logger.Info("PodClique deleted successfully")
	return ctrlcommon.DoNotRequeue()
}

// deletePodCliqueResources deletes all managed resources associated with the PodClique.
// It runs deletion tasks concurrently for all registered operators.
func (r *Reconciler) deletePodCliqueResources(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	operators := r.operatorRegistry.GetAllOperators()
	// Create deletion tasks for all registered operators
	deleteTasks := make([]utils.Task, 0, len(operators))
	for kind, operator := range operators {
		deleteTasks = append(deleteTasks, utils.Task{
			Name: fmt.Sprintf("delete-%s", kind),
			Fn: func(ctx context.Context) error {
				return operator.Delete(ctx, logger, pclq.ObjectMeta)
			},
		})
	}
	logger.Info("Triggering delete of PodClique resources")
	// Execute all deletion tasks concurrently
	if runResult := utils.RunConcurrently(ctx, logger, deleteTasks); runResult.HasErrors() {
		deletionErr := runResult.GetAggregatedError()
		logger.Error(deletionErr, "Error deleting managed resources", "summary", runResult.GetSummary())
		return ctrlcommon.ReconcileWithErrors("error deleting managed resources", deletionErr)
	}
	return ctrlcommon.ContinueReconcile()
}

// verifyNoResourcesAwaitsCleanup ensures all managed resources have been successfully deleted.
// It delegates to the controller utilities to perform the verification.
func (r *Reconciler) verifyNoResourcesAwaitsCleanup(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	return ctrlutils.VerifyNoResourceAwaitsCleanup(ctx, logger, r.operatorRegistry, pclq.ObjectMeta)
}

// removeFinalizer removes the PodClique finalizer from the resource to allow garbage collection.
// It checks for finalizer presence before attempting removal and handles errors appropriately.
func (r *Reconciler) removeFinalizer(ctx context.Context, logger logr.Logger, pclq *grovecorev1alpha1.PodClique) ctrlcommon.ReconcileStepResult {
	// Check if finalizer is present before attempting removal
	if !controllerutil.ContainsFinalizer(pclq, grovecorev1alpha1.FinalizerPodClique) {
		logger.Info("Finalizer not found", "PodClique", pclq)
		return ctrlcommon.DoNotRequeue()
	}
	logger.Info("Removing finalizer", "PodClique", pclq, "finalizerName", grovecorev1alpha1.FinalizerPodClique)
	// Remove finalizer and patch the resource
	if err := ctrlutils.RemoveAndPatchFinalizer(ctx, r.client, pclq, grovecorev1alpha1.FinalizerPodClique); err != nil {
		return ctrlcommon.ReconcileWithErrors("error removing finalizer", fmt.Errorf("failed to remove finalizer: %s from PodClique: %v: %w", grovecorev1alpha1.FinalizerPodClique, client.ObjectKeyFromObject(pclq), err))
	}
	return ctrlcommon.ContinueReconcile()
}
