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

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	componentutils "github.com/NVIDIA/grove/operator/internal/component/utils"
	grovectrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"
	"github.com/NVIDIA/grove/operator/internal/utils"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Controller configuration constants.
const (
	// controllerName is the unique identifier for the PodGangSet controller.
	controllerName = "podgangset-controller"
)

// RegisterWithManager registers the PodGangSet Reconciler with the manager.
func (r *Reconciler) RegisterWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: ptr.Deref(r.config.ConcurrentSyncs, 1),
		}).
		For(&grovecorev1alpha1.PodGangSet{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&grovecorev1alpha1.PodClique{},
			handler.EnqueueRequestsFromMapFunc(mapPodCliqueToPodGangSet()),
			builder.WithPredicates(podCliquePredicate()),
		).
		Watches(
			&grovecorev1alpha1.PodCliqueScalingGroup{},
			handler.EnqueueRequestsFromMapFunc(mapPodCliqueScaleGroupToPodGangSet()),
			builder.WithPredicates(podCliqueScalingGroupPredicate()),
		).
		Complete(r)
}

// mapPodCliqueToPodGangSet returns a mapper function that maps PodClique events
// to reconcile requests for their owning PodGangSet.
func mapPodCliqueToPodGangSet() handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		pclq, ok := obj.(*grovecorev1alpha1.PodClique)
		if !ok {
			return nil
		}
		pgsName := componentutils.GetPodGangSetName(pclq.ObjectMeta)
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: pgsName, Namespace: pclq.Namespace}}}
	}
}

// mapPodCliqueScaleGroupToPodGangSet returns a mapper function that maps PodCliqueScalingGroup events
// to reconcile requests for their owning PodGangSet.
func mapPodCliqueScaleGroupToPodGangSet() handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		pcsg, ok := obj.(*grovecorev1alpha1.PodCliqueScalingGroup)
		if !ok {
			return nil
		}
		pgsName := componentutils.GetPodGangSetName(pcsg.ObjectMeta)
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: pgsName, Namespace: pcsg.Namespace}}}
	}
}

// podCliquePredicate returns a predicate that filters PodClique events to only process
// those managed by Grove and with relevant spec or status changes.
func podCliquePredicate() predicate.Predicate {
	return predicate.Funcs{
		// Ignore create events as PodCliques are created by the controller
		CreateFunc: func(_ event.CreateEvent) bool { return false },
		// Process delete events for Grove-managed PodCliques
		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			return grovectrlutils.IsManagedPodClique(deleteEvent.Object, grovecorev1alpha1.PodGangSetKind)
		},
		// Process update events for Grove-managed PodCliques with spec or status changes
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return grovectrlutils.IsManagedPodClique(updateEvent.ObjectOld, grovecorev1alpha1.PodGangSetKind, grovecorev1alpha1.PodCliqueScalingGroupKind) &&
				(hasSpecChanged(updateEvent) || hasStatusChanged(updateEvent))
		},
		// Ignore generic events
		GenericFunc: func(_ event.GenericEvent) bool { return false },
	}
}

// podCliqueScalingGroupPredicate returns a predicate that filters PodCliqueScalingGroup events
// to only process updates where the MinAvailableBreached condition has changed.
func podCliqueScalingGroupPredicate() predicate.Predicate {
	return predicate.Funcs{
		// Ignore create events
		CreateFunc: func(_ event.CreateEvent) bool { return false },
		// Ignore delete events
		DeleteFunc: func(_ event.DeleteEvent) bool { return false },
		// Process update events only when MinAvailableBreached condition changes
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			oldPCSG, okOld := updateEvent.ObjectOld.(*grovecorev1alpha1.PodCliqueScalingGroup)
			newPCSG, okNew := updateEvent.ObjectNew.(*grovecorev1alpha1.PodCliqueScalingGroup)
			if !okOld || !okNew {
				return false
			}
			return hasMinAvailableBreachedConditionChanged(oldPCSG.Status.Conditions, newPCSG.Status.Conditions)
		},
		// Ignore generic events
		GenericFunc: func(_ event.TypedGenericEvent[client.Object]) bool { return false },
	}
}

// hasSpecChanged returns true if the object's spec has changed by comparing generations.
func hasSpecChanged(updateEvent event.UpdateEvent) bool {
	return updateEvent.ObjectOld.GetGeneration() != updateEvent.ObjectNew.GetGeneration()
}

// hasStatusChanged returns true if the PodClique's status has changed in ways that
// require PodGangSet reconciliation (replica counts or MinAvailableBreached condition).
func hasStatusChanged(updateEvent event.UpdateEvent) bool {
	oldPCLQ, okOld := updateEvent.ObjectOld.(*grovecorev1alpha1.PodClique)
	newPCLQ, okNew := updateEvent.ObjectNew.(*grovecorev1alpha1.PodClique)
	if !okOld || !okNew {
		return false
	}
	return hasAnyStatusReplicasChanged(oldPCLQ.Status, newPCLQ.Status) ||
		hasMinAvailableBreachedConditionChanged(oldPCLQ.Status.Conditions, newPCLQ.Status.Conditions)
}

// hasAnyStatusReplicasChanged returns true if any of the replica count fields have changed.
func hasAnyStatusReplicasChanged(oldPCLQStatus, newPCLQStatus grovecorev1alpha1.PodCliqueStatus) bool {
	return oldPCLQStatus.Replicas != newPCLQStatus.Replicas ||
		oldPCLQStatus.ReadyReplicas != newPCLQStatus.ReadyReplicas ||
		oldPCLQStatus.ScheduleGatedReplicas != newPCLQStatus.ScheduleGatedReplicas
}

// hasMinAvailableBreachedConditionChanged returns true if the MinAvailableBreached condition
// has been added, removed, or its status has changed.
func hasMinAvailableBreachedConditionChanged(oldConditions, newConditions []metav1.Condition) bool {
	oldMinAvailableBreachedCond := meta.FindStatusCondition(oldConditions, grovecorev1alpha1.ConditionTypeMinAvailableBreached)
	newMinAvailableBreachedCond := meta.FindStatusCondition(newConditions, grovecorev1alpha1.ConditionTypeMinAvailableBreached)
	// Return true if one condition exists but the other doesn't
	if utils.OnlyOneIsNil(oldMinAvailableBreachedCond, newMinAvailableBreachedCond) {
		return true
	}
	// Return true if both exist but have different status
	if oldMinAvailableBreachedCond != nil && newMinAvailableBreachedCond != nil {
		return oldMinAvailableBreachedCond.Status != newMinAvailableBreachedCond.Status
	}
	return false
}
