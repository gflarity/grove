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

// Package satokensecret manages ServiceAccount token secrets for PodGangSet resources.
// It creates and manages Kubernetes secrets that contain ServiceAccount tokens
// required by init containers in PodGangSet pods.
package satokensecret

import (
	"context"
	"fmt"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	k8sutils "github.com/NVIDIA/grove/operator/internal/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Error codes for ServiceAccount token secret operations.
const (
	errCodeGetSecret              grovecorev1alpha1.ErrorCode = "ERR_GET_SECRET"
	errCodeSetControllerReference grovecorev1alpha1.ErrorCode = "ERR_SET_CONTROLLER_REFERENCE"
	errCodeCreateSecret           grovecorev1alpha1.ErrorCode = "ERR_CREATE_SECRET"
	errCodeDeleteSecret           grovecorev1alpha1.ErrorCode = "ERR_DELETE_SECRET"
)

// _resource implements the component.Operator interface for managing
// ServiceAccount token secrets associated with PodGangSet resources.
type _resource struct {
	client client.Client
	scheme *runtime.Scheme
}

// New creates a new ServiceAccount token secret component operator.
// It returns a component.Operator that manages secrets containing ServiceAccount tokens
// for PodGangSet resources.
func New(client client.Client, scheme *runtime.Scheme) component.Operator[grovecorev1alpha1.PodGangSet] {
	return &_resource{
		client: client,
		scheme: scheme,
	}
}

// GetExistingResourceNames returns the names of existing ServiceAccount token secrets
// that are controlled by the given PodGangSet. It returns an empty slice if no
// controlled secrets are found.
func (r _resource) GetExistingResourceNames(ctx context.Context, _ logr.Logger, pgsObjMeta metav1.ObjectMeta) ([]string, error) {
	secretNames := make([]string, 0, 1)
	objKey := getObjectKey(pgsObjMeta)

	// Check if the expected secret already exists
	partialObjMeta, err := k8sutils.GetExistingPartialObjectMetadata(ctx, r.client, corev1.SchemeGroupVersion.WithKind("Secret"), objKey)
	if err != nil {
		if errors.IsNotFound(err) {
			return secretNames, nil
		}
		return nil, groveerr.WrapError(err,
			errCodeGetSecret,
			component.OperationGetExistingResourceNames,
			fmt.Sprintf("Error getting Secret: %v for PodGangSet: %v", objKey, k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}

	// Only include secrets that are controlled by this PodGangSet
	if metav1.IsControlledBy(partialObjMeta, &pgsObjMeta) {
		secretNames = append(secretNames, partialObjMeta.Name)
	}
	return secretNames, nil
}

// Sync creates a ServiceAccount token secret for the given PodGangSet if one doesn't already exist.
// The secret is configured with the appropriate ServiceAccount reference and controller ownership.
func (r _resource) Sync(ctx context.Context, logger logr.Logger, pgs *grovecorev1alpha1.PodGangSet) error {
	pgsObjKey := client.ObjectKeyFromObject(pgs)

	// Check if a secret already exists for this PodGangSet
	existingSecretNames, err := r.GetExistingResourceNames(ctx, logger, pgs.ObjectMeta)
	if err != nil {
		return groveerr.WrapError(err,
			errCodeGetSecret,
			component.OperationSync,
			fmt.Sprintf("Error getting existing satokensecret names for PodGangSet: %v", pgsObjKey),
		)
	}
	if len(existingSecretNames) > 0 {
		logger.Info("Secret already exists, skipping creation", "existingSecret", existingSecretNames[0])
		return nil
	}

	// Create and configure the new secret
	objKey := getObjectKey(pgs.ObjectMeta)
	secret := emptySecret(objKey)
	if err = r.buildResource(pgs, secret); err != nil {
		return err
	}

	// Create the secret in Kubernetes
	if err = client.IgnoreAlreadyExists(r.client.Create(ctx, secret)); err != nil {
		return groveerr.WrapError(err,
			errCodeCreateSecret,
			component.OperationSync,
			fmt.Sprintf("Error creating satokensecret: %v for PodGangSet: %v", objKey, pgsObjKey),
		)
	}
	logger.Info("Created Secret", "objectKey", objKey)
	return nil
}

// Delete removes the ServiceAccount token secret associated with the given PodGangSet.
// It gracefully handles the case where the secret doesn't exist.
func (r _resource) Delete(ctx context.Context, logger logr.Logger, pgsObjMeta metav1.ObjectMeta) error {
	objectKey := getObjectKey(pgsObjMeta)
	logger.Info("Triggering delete of Secret", "objectKey", objectKey)
	if err := r.client.Delete(ctx, emptySecret(objectKey)); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Secret not found", "objectKey", objectKey)
			return nil
		}
		return groveerr.WrapError(err,
			errCodeDeleteSecret,
			component.OperationDelete,
			fmt.Sprintf("Error deleting satokensecret: %v for PodGangSet: %v", objectKey, k8sutils.GetObjectKeyFromObjectMeta(pgsObjMeta)),
		)
	}
	logger.Info("Deleted Secret", "objectKey", objectKey)
	return nil
}

// buildResource configures the given secret with the necessary metadata, labels, and annotations
// to function as a ServiceAccount token secret for the PodGangSet.
func (r _resource) buildResource(pgs *grovecorev1alpha1.PodGangSet, secret *corev1.Secret) error {
	// Set standard labels for the secret
	secret.Labels = getLabels(pgs.Name, secret.Name)

	// Establish controller ownership relationship
	if err := controllerutil.SetControllerReference(pgs, secret, r.scheme); err != nil {
		return groveerr.WrapError(err,
			errCodeSetControllerReference,
			component.OperationSync,
			fmt.Sprintf("Error setting controller reference for satokensecret: %v", client.ObjectKeyFromObject(secret)),
		)
	}

	// Configure as ServiceAccount token secret with appropriate annotation
	secret.Type = corev1.SecretTypeServiceAccountToken
	secret.Annotations = map[string]string{
		corev1.ServiceAccountNameKey: apicommon.GeneratePodServiceAccountName(pgs.Name),
	}
	return nil
}

// getLabels returns the standard labels for a ServiceAccount token secret,
// combining default PodGangSet labels with component-specific labels.
func getLabels(pgsName, secretName string) map[string]string {
	secretLabels := map[string]string{
		apicommon.LabelComponentKey: apicommon.LabelComponentNameServiceAccountTokenSecret,
		apicommon.LabelAppNameKey:   secretName,
	}
	return lo.Assign(
		apicommon.GetDefaultLabelsForPodGangSetManagedResources(pgsName),
		secretLabels,
	)
}

// getObjectKey generates the Kubernetes object key for the ServiceAccount token secret
// associated with the given PodGangSet metadata.
func getObjectKey(pgsObjMeta metav1.ObjectMeta) client.ObjectKey {
	return client.ObjectKey{
		Name:      apicommon.GenerateInitContainerSATokenSecretName(pgsObjMeta.Name),
		Namespace: pgsObjMeta.Namespace,
	}
}

// emptySecret creates a new Secret object with only the basic metadata populated.
// This is used as a template for both creation and deletion operations.
func emptySecret(objKey client.ObjectKey) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objKey.Name,
			Namespace: objKey.Namespace,
		},
	}
}
