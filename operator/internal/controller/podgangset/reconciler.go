// /*
// Copyright 2024 The Grove Authors.
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

	configv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	pgscomponent "github.com/NVIDIA/grove/operator/internal/component/podgangset"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	ctrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllogger "sigs.k8s.io/controller-runtime/pkg/log"
)

// Reconciler reconciles PodGangSet resources, managing their lifecycle through
// creation, updates, and deletion operations.
type Reconciler struct {
	// config holds the controller-specific configuration for PodGangSet reconciliation
	config configv1alpha1.PodGangSetControllerConfiguration
	// client provides access to the Kubernetes API server for resource operations
	client ctrlclient.Client
	// reconcileStatusRecorder tracks and records the status of reconciliation operations
	reconcileStatusRecorder ctrlcommon.ReconcileStatusRecorder
	// operatorRegistry manages component operations for PodGangSet resources
	operatorRegistry component.OperatorRegistry[grovecorev1alpha1.PodGangSet]
}

// NewReconciler creates a new reconciler for PodGangSet resources with the
// provided manager and controller configuration.
func NewReconciler(mgr ctrl.Manager, controllerCfg configv1alpha1.PodGangSetControllerConfiguration) *Reconciler {
	// Create event recorder for publishing Kubernetes events
	eventRecorder := mgr.GetEventRecorderFor(controllerName)
	return &Reconciler{
		config:                  controllerCfg,
		client:                  mgr.GetClient(),
		reconcileStatusRecorder: ctrlcommon.NewReconcileStatusRecorder(mgr.GetClient(), eventRecorder),
		operatorRegistry:        pgscomponent.CreateOperatorRegistry(mgr, eventRecorder),
	}
}

// Reconcile implements the main reconciliation loop for PodGangSet resources.
// It handles the complete lifecycle including creation, updates, and deletion.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Create logger with controller name for consistent logging
	logger := ctrllogger.FromContext(ctx).WithName(controllerName)

	// Fetch the PodGangSet resource from the cluster
	pgs := &grovecorev1alpha1.PodGangSet{}
	if result := ctrlutils.GetPodGangSet(ctx, r.client, logger, req.NamespacedName, pgs); ctrlcommon.ShortCircuitReconcileFlow(result) {
		return result.Result()
	}

	// Handle deletion if the resource is marked for deletion
	if result := r.reconcileDelete(ctx, logger, pgs); ctrlcommon.ShortCircuitReconcileFlow(result) {
		return result.Result()
	}

	// Reconcile the desired state specification
	if result := r.reconcileSpec(ctx, logger, pgs); result.HasErrors() {
		logger.Info("Reconciliation spec step failed",
			"PodGangSet", ctrlclient.ObjectKeyFromObject(pgs), "errors", result.GetErrors(), "description", result.GetDescription())
	}

	// Update the resource status and return the final reconciliation result
	return r.reconcileStatus(ctx, logger, pgs).Result()
}

// reconcileDelete handles the deletion workflow for PodGangSet resources.
// It processes finalizers and triggers the deletion flow when appropriate.
func (r *Reconciler) reconcileDelete(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) ctrlcommon.ReconcileStepResult {
	// Check if the resource is marked for deletion
	if !pgs.DeletionTimestamp.IsZero() {
		// Skip deletion if finalizer is not present (already cleaned up)
		if !controllerutil.ContainsFinalizer(pgs, grovecorev1alpha1.FinalizerPodGangSet) {
			return ctrlcommon.DoNotRequeue()
		}
		// Create deletion-specific logger and trigger deletion workflow
		dLog := logger.WithValues("operation", "delete")
		return r.triggerDeletionFlow(ctx, dLog, pgs)
	}
	// Resource is not being deleted, continue with normal reconciliation
	return ctrlcommon.ContinueReconcile()
}
