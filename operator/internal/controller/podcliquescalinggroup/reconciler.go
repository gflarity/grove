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

	"github.com/NVIDIA/grove/operator/api/common/constants"
	groveconfigv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	pcsgcomponent "github.com/NVIDIA/grove/operator/internal/component/podcliquescalinggroup"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	ctrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllogger "sigs.k8s.io/controller-runtime/pkg/log"
)

// Reconciler reconciles PodCliqueScalingGroup objects.
// It manages the lifecycle of PodCliqueScalingGroup resources by coordinating
// the creation, update, and deletion of dependent resources through an operator registry.
type Reconciler struct {
	// config holds the controller-specific configuration settings
	config groveconfigv1alpha1.PodCliqueScalingGroupControllerConfiguration
	// client provides access to the Kubernetes API server
	client client.Client
	// reconcileStatusRecorder handles status updates and event recording
	reconcileStatusRecorder ctrlcommon.ReconcileStatusRecorder
	// operatorRegistry manages the lifecycle of dependent resources
	operatorRegistry component.OperatorRegistry[grovecorev1alpha1.PodCliqueScalingGroup]
}

// NewReconciler creates a new instance of the PodCliqueScalingGroup Reconciler.
// It initializes the reconciler with the provided manager and controller configuration,
// setting up the necessary clients, event recorders, and operator registry.
func NewReconciler(mgr ctrl.Manager, controllerCfg groveconfigv1alpha1.PodCliqueScalingGroupControllerConfiguration) *Reconciler {
	eventRecorder := mgr.GetEventRecorderFor(controllerName)
	return &Reconciler{
		config:                  controllerCfg,
		client:                  mgr.GetClient(),
		reconcileStatusRecorder: ctrlcommon.NewReconcileStatusRecorder(mgr.GetClient(), eventRecorder),
		operatorRegistry:        pcsgcomponent.CreateOperatorRegistry(mgr, eventRecorder),
	}
}

// Reconcile reconciles a PodCliqueScalingGroup resource.
// It implements the main reconciliation logic, handling both creation/update and deletion flows.
// The method processes the resource through three main phases:
// 1. Resource retrieval and validation
// 2. Deletion or specification reconciliation based on deletion timestamp
// 3. Status reconciliation and result evaluation
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Create a logger instance for this reconciliation cycle
	logger := ctrllogger.FromContext(ctx).WithName(controllerName)

	// Retrieve the PodCliqueScalingGroup resource from the cluster
	pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{}
	if result := ctrlutils.GetPodCliqueScalingGroup(ctx, r.client, logger, req.NamespacedName, pcsg); ctrlcommon.ShortCircuitReconcileFlow(result) {
		return result.Result()
	}

	// Determine reconciliation flow based on deletion timestamp
	var deletionOrSpecReconcileFlowResult ctrlcommon.ReconcileStepResult
	if !pcsg.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(pcsg, constants.FinalizerPodCliqueScalingGroup) {
			return ctrlcommon.DoNotRequeue().Result()
		}
		dLog := logger.WithValues("operation", "delete")
		deletionOrSpecReconcileFlowResult = r.triggerDeletionFlow(ctx, dLog, pcsg)
	} else {
		// Resource is active - reconcile the specification
		specLog := logger.WithValues("operation", "specReconcile")
		deletionOrSpecReconcileFlowResult = r.reconcileSpec(ctx, specLog, pcsg)
	}

	if statusReconcileResult := r.reconcileStatus(ctx, logger, client.ObjectKeyFromObject(pcsg)); ctrlcommon.ShortCircuitReconcileFlow(statusReconcileResult) {
		return statusReconcileResult.Result()
	}

	// Check if deletion or spec reconciliation requires early return
	if ctrlcommon.ShortCircuitReconcileFlow(deletionOrSpecReconcileFlowResult) {
		return deletionOrSpecReconcileFlowResult.Result()
	}

	// All reconciliation steps completed successfully - no requeue needed
	return ctrlcommon.DoNotRequeue().Result()
}
