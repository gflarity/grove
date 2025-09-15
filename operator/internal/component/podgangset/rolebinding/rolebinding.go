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

// Package rolebinding provides a component operator for managing Kubernetes RoleBinding resources
// associated with PodGangSet instances. It handles the creation, synchronization, and deletion
// of RoleBindings that bind ServiceAccounts to Roles for pod-level RBAC permissions.
package rolebinding

import (
	"context"
	"fmt"
	"strings"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Error codes for RoleBinding operations
const (
	errGetRoleBinding    grovecorev1alpha1.ErrorCode = "ERR_GET_ROLEBINDING"
	errSyncRoleBinding   grovecorev1alpha1.ErrorCode = "ERR_SYNC_ROLEBINDING"
	errDeleteRoleBinding grovecorev1alpha1.ErrorCode = "ERR_DELETE_ROLEBINDING"
)

// _resource implements the component.Operator interface for RoleBinding resources.
// It manages the lifecycle of RoleBindings associated with PodGangSet instances.
type _resource struct {
	client client.Client
	scheme *runtime.Scheme
}

// New creates an instance of RoleBinding component operator.
// The returned operator manages RoleBinding resources for PodGangSet instances.
func New(client client.Client, scheme *runtime.Scheme) component.Operator[grovecorev1alpha1.PodGangSet] {
	return &_resource{
		client: client,
		scheme: scheme,
	}
}

// GetExistingResourceNames returns the names of all the existing resources that the RoleBinding Operator manages.
// It checks for a single RoleBinding resource controlled by the given PodGangSet.
func (r _resource) GetExistingResourceNames(ctx context.Context, _ logr.Logger, pgsObjMeta metav1.ObjectMeta) ([]string, error) {
	// Initialize slice with capacity of 1 since we expect at most one RoleBinding per PodGangSet
	roleBindingNames := make([]string, 0, 1)
	objectKey := getObjectKey(pgsObjMeta)

	// Create metadata object to query for existing RoleBinding
	objMeta := &metav1.PartialObjectMetadata{}
	objMeta.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("RoleBinding"))
	// Attempt to get the RoleBinding resource
	if err := r.client.Get(ctx, objectKey, objMeta); err != nil {
		if errors.IsNotFound(err) {
			// RoleBinding doesn't exist, return empty list
			return roleBindingNames, nil
		}
		return roleBindingNames, groveerr.WrapError(err,
			errGetRoleBinding,
			component.OperationGetExistingResourceNames,
			fmt.Sprintf("Error getting RoleBinding: %v for PodGangSet: %v", objectKey, k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}
	// Only include RoleBinding if it's controlled by this PodGangSet
	if metav1.IsControlledBy(objMeta, &pgsObjMeta) {
		roleBindingNames = append(roleBindingNames, objMeta.Name)
	}
	return roleBindingNames, nil
}

// Sync synchronizes all resources that the RoleBinding Operator manages.
// It creates a RoleBinding if one doesn't already exist for the PodGangSet.
func (r _resource) Sync(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) error {
	// Check for existing RoleBindings managed by this operator
	existingRoleBindingNames, err := r.GetExistingResourceNames(ctx, logger, pgs.ObjectMeta)
	if err != nil {
		return groveerr.WrapError(err,
			errSyncRoleBinding,
			component.OperationSync,
			fmt.Sprintf("Error getting existing RoleBinding names for PodGangSet: %v", client.ObjectKeyFromObject(pgs)),
		)
	}
	// Skip creation if RoleBinding already exists
	if len(existingRoleBindingNames) > 0 {
		logger.Info("RoleBinding already exists, skipping creation", "existingRoleBinding", existingRoleBindingNames[0])
		return nil
	}
	// Create new RoleBinding resource
	objectKey := getObjectKey(pgs.ObjectMeta)
	roleBinding := emptyRoleBinding(objectKey)
	logger.Info("Running CreateOrUpdate RoleBinding", "objectKey", objectKey)

	// Build the RoleBinding resource with proper configuration
	if err := r.buildResource(pgs, roleBinding); err != nil {
		return groveerr.WrapError(err,
			errSyncRoleBinding,
			component.OperationSync,
			fmt.Sprintf("Error building RoleBinding: %v for PodGangSet: %v", objectKey, client.ObjectKeyFromObject(pgs)),
		)
	}
	// Create the RoleBinding, ignoring if it already exists
	if err := client.IgnoreAlreadyExists(r.client.Create(ctx, roleBinding)); err != nil {
		return groveerr.WrapError(err,
			errSyncRoleBinding,
			component.OperationSync,
			fmt.Sprintf("Error syncing RoleBinding: %v for PodGangSet: %v", objectKey, client.ObjectKeyFromObject(pgs)),
		)
	}
	logger.Info("Created RoleBinding", "objectKey", objectKey)
	return nil
}

// Delete removes the RoleBinding resource associated with the given PodGangSet.
// It gracefully handles cases where the RoleBinding doesn't exist.
func (r _resource) Delete(ctx context.Context, logger logr.Logger, pgsObjMeta metav1.ObjectMeta) error {
	objectKey := getObjectKey(pgsObjMeta)
	logger.Info("Triggering delete of RoleBinding", "objectKey", objectKey)

	// Attempt to delete the RoleBinding
	if err := r.client.Delete(ctx, emptyRoleBinding(objectKey)); err != nil {
		if errors.IsNotFound(err) {
			// RoleBinding doesn't exist, nothing to delete
			logger.Info("RoleBinding not found, deletion is a no-op", "objectKey", objectKey)
			return nil
		}
		return groveerr.WrapError(err,
			errDeleteRoleBinding,
			component.OperationDelete,
			fmt.Sprintf("Error deleting RoleBinding: %v for PodGangSet: %v", objectKey, k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}
	logger.Info("deleted RoleBinding", "objectKey", objectKey)
	return nil
}

// buildResource configures the RoleBinding with proper labels, controller reference,
// role reference, and subjects based on the PodGangSet specification.
func (r _resource) buildResource(pgs *grovecorev1alpha1.PodGangSet, roleBinding *rbacv1.RoleBinding) error {
	// Set labels for the RoleBinding
	roleBinding.Labels = getLabels(pgs.ObjectMeta)

	// Set controller reference to ensure proper ownership and cleanup
	if err := controllerutil.SetControllerReference(pgs, roleBinding, r.scheme); err != nil {
		return groveerr.WrapError(err,
			errSyncRoleBinding,
			component.OperationSync,
			fmt.Sprintf("Error setting controller reference for RoleBinding: %v", client.ObjectKeyFromObject(roleBinding)),
		)
	}

	// Configure the Role reference that this RoleBinding binds to
	roleBinding.RoleRef = rbacv1.RoleRef{
		APIGroup: rbacv1.SchemeGroupVersion.Group,
		Kind:     "Role",
		Name:     apicommon.GeneratePodRoleName(pgs.Name),
	}

	// Configure the ServiceAccount subject that gets bound to the Role
	roleBinding.Subjects = []rbacv1.Subject{
		{
			APIGroup:  corev1.SchemeGroupVersion.Group,
			Kind:      "ServiceAccount",
			Name:      apicommon.GeneratePodServiceAccountName(pgs.Name),
			Namespace: pgs.Namespace,
		},
	}
	return nil
}

// getLabels generates the appropriate labels for the RoleBinding resource.
// It combines default PodGangSet labels with component-specific labels.
func getLabels(pgsObjMeta metav1.ObjectMeta) map[string]string {
	// Create component-specific labels
	roleLabels := map[string]string{
		apicommon.LabelComponentKey: apicommon.LabelComponentNamePodRoleBinding,
		apicommon.LabelAppNameKey:   strings.ReplaceAll(apicommon.GeneratePodRoleBindingName(pgsObjMeta.Name), ":", "-"),
	}
	// Merge with default PodGangSet labels
	return lo.Assign(
		apicommon.GetDefaultLabelsForPodGangSetManagedResources(pgsObjMeta.Name),
		roleLabels,
	)
}

// getObjectKey generates the Kubernetes object key for the RoleBinding resource
// based on the PodGangSet metadata.
func getObjectKey(pgsObjMeta metav1.ObjectMeta) client.ObjectKey {
	return client.ObjectKey{
		Name:      apicommon.GeneratePodRoleBindingName(pgsObjMeta.Name),
		Namespace: pgsObjMeta.Namespace,
	}
}

// emptyRoleBinding creates a minimal RoleBinding resource with only the name and namespace set.
// This is used as a template for operations like deletion or as a base for building the full resource.
func emptyRoleBinding(objKey client.ObjectKey) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objKey.Name,
			Namespace: objKey.Namespace,
		},
	}
}
