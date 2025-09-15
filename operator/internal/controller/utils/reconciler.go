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

package utils

import (
	"context"
	"errors"
	"time"

	"github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	grovectrl "github.com/NVIDIA/grove/operator/internal/controller/common"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPodGangSet retrieves the latest PodGangSet object from the cluster.
// It typically hits the informer cache for performance. If the object is not found,
// it logs an info message and returns DoNotRequeue to avoid unnecessary reconciliation.
func GetPodGangSet(ctx context.Context, cl client.Client, logger logr.Logger, objectKey client.ObjectKey, pgs *v1alpha1.PodGangSet) grovectrl.ReconcileStepResult {
	// Attempt to retrieve the PodGangSet from the cluster
	if err := cl.Get(ctx, objectKey, pgs); err != nil {
		// Handle not found errors gracefully
		if apierrors.IsNotFound(err) {
			logger.Info("PodGangSet not found", "objectKey", objectKey)
			return grovectrl.DoNotRequeue()
		}
		return grovectrl.ReconcileWithErrors("error getting PodGangSet", err)
	}
	return grovectrl.ContinueReconcile()
}

// GetPodClique retrieves the latest PodClique object from the cluster.
// It typically hits the informer cache for performance. The ignoreNotFound parameter
// controls whether not found errors should be treated as non-errors.
func GetPodClique(ctx context.Context, cl client.Client, logger logr.Logger, objectKey client.ObjectKey, pclq *v1alpha1.PodClique, ignoreNotFound bool) grovectrl.ReconcileStepResult {
	// Attempt to retrieve the PodClique from the cluster
	if err := cl.Get(ctx, objectKey, pclq); err != nil {
		// Handle not found errors based on ignoreNotFound flag
		if ignoreNotFound && apierrors.IsNotFound(err) {
			logger.Info("PodClique not found", "objectKey", objectKey)
			return grovectrl.DoNotRequeue()
		}
		return grovectrl.ReconcileWithErrors("error getting PodClique", err)
	}
	return grovectrl.ContinueReconcile()
}

// GetPodCliqueScalingGroup retrieves the latest PodCliqueScalingGroup object from the cluster.
// It typically hits the informer cache for performance. If the object is not found,
// it logs an info message and returns DoNotRequeue to avoid unnecessary reconciliation.
func GetPodCliqueScalingGroup(ctx context.Context, cl client.Client, logger logr.Logger, objectKey client.ObjectKey, pcsg *v1alpha1.PodCliqueScalingGroup) grovectrl.ReconcileStepResult {
	// Attempt to retrieve the PodCliqueScalingGroup from the cluster
	if err := cl.Get(ctx, objectKey, pcsg); err != nil {
		// Handle not found errors gracefully
		if apierrors.IsNotFound(err) {
			logger.Info("PodCliqueScalingGroup not found")
			return grovectrl.DoNotRequeue()
		}
		logger.Error(err, "error getting PodCliqueScalingGroup")
		return grovectrl.ReconcileWithErrors("error getting PodCliqueScalingGroup", err)
	}
	return grovectrl.ContinueReconcile()
}

// VerifyNoResourceAwaitsCleanup ensures no managed resources are still present in the cluster
// before allowing finalizer removal. It checks all operators in the registry for existing
// resources and requeues with a delay if any are found.
func VerifyNoResourceAwaitsCleanup[T component.GroveCustomResourceType](ctx context.Context, logger logr.Logger, operatorRegistry component.OperatorRegistry[T], objMeta metav1.ObjectMeta) grovectrl.ReconcileStepResult {
	// Get all operators from the registry
	operators := operatorRegistry.GetAllOperators()
	resourceNamesAwaitingCleanup := make([]string, 0, len(operators))

	// Check each operator for existing resources
	for _, operator := range operators {
		existingResourceNames, err := operator.GetExistingResourceNames(ctx, logger, objMeta)
		if err != nil {
			return grovectrl.ReconcileWithErrors("error getting existing resource names", err)
		}
		if len(existingResourceNames) > 0 {
			resourceNamesAwaitingCleanup = append(resourceNamesAwaitingCleanup, existingResourceNames...)
		}
	}

	// If resources still exist, requeue after delay
	if len(resourceNamesAwaitingCleanup) > 0 {
		logger.Info("Resources are still awaiting cleanup", "reconciledObjectKey", k8sutils.GetObjectKeyFromObjectMeta(objMeta), "resources", resourceNamesAwaitingCleanup)
		return grovectrl.ReconcileAfter(5*time.Second, "Resources are still awaiting cleanup. Skipping removal of finalizer")
	}

	logger.Info("No resources are awaiting cleanup")
	return grovectrl.ContinueReconcile()
}

// ShouldRequeueAfter checks if an error is a GroveError and if yes then returns true
// when the error code is groveerr.ErrCodeRequeueAfter along with the GroveError.Message, else it returns false and an empty message.
func ShouldRequeueAfter(err error) bool {
	// Check if error is a GroveError with requeue-after code
	groveErr := &groveerr.GroveError{}
	if errors.As(err, &groveErr) {
		return groveErr.Code == groveerr.ErrCodeRequeueAfter
	}
	return false
}

// ShouldContinueReconcileAndRequeue determines if an error indicates the reconciliation
// should continue processing but also requeue for future reconciliation. It returns true
// if the error is a GroveError with ErrCodeContinueReconcileAndRequeue, false otherwise.
func ShouldContinueReconcileAndRequeue(err error) bool {
	// Check if error is a GroveError with continue-and-requeue code
	groveErr := &groveerr.GroveError{}
	if errors.As(err, &groveErr) {
		return groveErr.Code == groveerr.ErrCodeContinueReconcileAndRequeue
	}
	return false
}
