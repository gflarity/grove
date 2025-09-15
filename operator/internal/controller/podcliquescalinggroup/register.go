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

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	ctrlutils "github.com/NVIDIA/grove/operator/internal/controller/utils"

	"github.com/samber/lo"
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

// controllerName defines the name used for this controller when registering with the manager.
const (
	controllerName = "podcliquescalingroup-controller"
)

// RegisterWithManager registers the PodCliqueScalingGroup Reconciler with the manager.
// This reconciler will only be called when the PodCliqueScalingGroup resource is updated. The resource can either be
// updated by an HPA or an equivalent external component.
func (r *Reconciler) RegisterWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: ptr.Deref(r.config.ConcurrentSyncs, 1),
		}).
		For(&grovecorev1alpha1.PodCliqueScalingGroup{}, builder.WithPredicates(
			predicate.And(
				predicate.GenerationChangedPredicate{},
				podCliqueScalingGroupUpdatePredicate(),
			)),
		).
		Watches(&grovecorev1alpha1.PodGangSet{},
			handler.EnqueueRequestsFromMapFunc(mapPGSToPCSG()),
			builder.WithPredicates(podGangSetPredicate()),
		).
		Watches(&grovecorev1alpha1.PodClique{},
			handler.EnqueueRequestsFromMapFunc(mapPCLQToPCSG()),
			builder.WithPredicates(podCliquePredicate()),
		).
		Complete(r)
}

// podCliqueScalingGroupUpdatePredicate returns a predicate that filters PodCliqueScalingGroup events.
// It only allows create and update events for resources managed by Grove with PodGangSet owners.
func podCliqueScalingGroupUpdatePredicate() predicate.Predicate {
	return predicate.Funcs{
		// Allow create events for Grove-managed PodCliqueScalingGroups owned by PodGangSets
		CreateFunc: func(createEvent event.CreateEvent) bool {
			return ctrlutils.IsManagedByGrove(createEvent.Object.GetLabels()) &&
				ctrlutils.HasExpectedOwner(grovecorev1alpha1.PodGangSetKind, createEvent.Object.GetOwnerReferences())
		},
		// Ignore delete events
		DeleteFunc: func(_ event.DeleteEvent) bool { return false },
		// Allow update events for Grove-managed PodCliqueScalingGroups owned by PodGangSets
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return ctrlutils.IsManagedByGrove(updateEvent.ObjectOld.GetLabels()) &&
				ctrlutils.HasExpectedOwner(grovecorev1alpha1.PodGangSetKind, updateEvent.ObjectOld.GetOwnerReferences())
		},
		// Ignore generic events
		GenericFunc: func(_ event.GenericEvent) bool { return false },
	}
}

// mapPGSToPCSG returns a handler map function that converts PodGangSet events to
// PodCliqueScalingGroup reconcile requests for all associated scaling groups.
func mapPGSToPCSG() handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		// Cast the object to PodGangSet
		pgs, ok := obj.(*grovecorev1alpha1.PodGangSet)
		if !ok {
			return nil
		}
		// Extract scaling group configurations from the PodGangSet spec
		pcsgConfigs := pgs.Spec.Template.PodCliqueScalingGroupConfigs
		if len(pcsgConfigs) == 0 {
			return nil
		}
		// Generate reconcile requests for all PodCliqueScalingGroups across all replicas
		requests := make([]reconcile.Request, 0, int(pgs.Spec.Replicas)*len(pcsgConfigs))
		for pgsReplica := range pgs.Spec.Replicas {
			for _, pcsgConfig := range pcsgConfigs {
				pcsgName := grovecorev1alpha1.GeneratePodCliqueScalingGroupName(grovecorev1alpha1.ResourceNameReplica{Name: pgs.Name, Replica: int(pgsReplica)}, pcsgConfig.Name)
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKey{
						Name:      pcsgName,
						Namespace: pgs.Namespace,
					},
				})
			}
		}
		return requests
	}
}

// podGangSetPredicate returns a predicate that filters PodGangSet events.
// It only allows update events for Grove-managed resources.
func podGangSetPredicate() predicate.Predicate {
	return predicate.Funcs{
		// Ignore create events
		CreateFunc: func(_ event.CreateEvent) bool { return false },
		// Ignore delete events
		DeleteFunc: func(_ event.DeleteEvent) bool { return false },
		// Allow update events for Grove-managed PodGangSets
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return ctrlutils.IsManagedByGrove(updateEvent.ObjectOld.GetLabels())
		},
		// Ignore generic events
		GenericFunc: func(_ event.GenericEvent) bool { return false },
	}
}

// mapPCLQToPCSG returns a handler map function that converts PodClique events to
// PodCliqueScalingGroup reconcile requests based on the scaling group label.
func mapPCLQToPCSG() handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		// Cast the object to PodClique
		pclq, ok := obj.(*grovecorev1alpha1.PodClique)
		if !ok {
			return nil
		}
		// Extract the PodCliqueScalingGroup name from the label
		pcsgName, ok := pclq.GetLabels()[grovecorev1alpha1.LabelPodCliqueScalingGroup]
		if !ok || lo.IsEmpty(pcsgName) {
			return nil
		}
		// Return reconcile request for the associated PodCliqueScalingGroup
		return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: pcsgName, Namespace: pclq.Namespace}}}
	}
}

// podCliquePredicate returns a predicate that filters PodClique events.
// It allows delete and update events for PodCliques managed by PodCliqueScalingGroups.
func podCliquePredicate() predicate.Predicate {
	return predicate.Funcs{
		// Ignore create events
		CreateFunc: func(_ event.CreateEvent) bool { return false },
		// Allow delete events for PodCliques managed by PodCliqueScalingGroups
		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			return ctrlutils.IsManagedPodClique(deleteEvent.Object, grovecorev1alpha1.PodCliqueScalingGroupKind)
		},
		// Allow update events for PodCliques managed by PodCliqueScalingGroups
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return ctrlutils.IsManagedPodClique(updateEvent.ObjectOld, grovecorev1alpha1.PodCliqueScalingGroupKind)
		},
	}
}
