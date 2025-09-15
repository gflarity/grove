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
	"strings"

	groveclientscheme "github.com/NVIDIA/grove/operator/internal/client"

	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ClientMethod is a name of the method on client.Client for which an error is recorded.
type ClientMethod string

const (
	// ClientMethodGet is the name of the Get method on client.Client.
	ClientMethodGet ClientMethod = "Get"
	// ClientMethodList is the name of the List method on client.Client.
	ClientMethodList ClientMethod = "List"
	// ClientMethodCreate is the name of the Create method on client.Client.
	ClientMethodCreate ClientMethod = "Create"
	// ClientMethodDelete is the name of the Delete method on client.Client.
	ClientMethodDelete ClientMethod = "Delete"
	// ClientMethodDeleteAll is the name of the DeleteAllOf method on client.Client.
	ClientMethodDeleteAll ClientMethod = "DeleteAll"
	// ClientMethodPatch is the name of the Patch method on client.Client.
	ClientMethodPatch ClientMethod = "Patch"
	// ClientMethodUpdate is the name of the Update method on client.Client.
	ClientMethodUpdate ClientMethod = "Update"
	// ClientMethodStatus is the name of the Status method on client.Client.StatusClient.
	ClientMethodStatus ClientMethod = "Status"
)

// TestClientBuilder is a builder for creating a test client.Client which is capable of recording and replaying errors.
type TestClientBuilder struct {
	delegatingClientBuilder *fake.ClientBuilder // underlying fake client builder
	delegatingClient        client.Client       // optional pre-configured client
	scheme                  *runtime.Scheme     // runtime scheme for type registration
	errorRecords            []errorRecord       // recorded error conditions
}

// errorRecord holds the configuration for a specific error condition to be replayed.
type errorRecord struct {
	method      ClientMethod            // client method to trigger error
	objectKey   client.ObjectKey        // target resource identifier
	labels      labels.Set              // label selector for collection operations
	resourceGVK schema.GroupVersionKind // resource type information
	err         error                   // error to be returned
}

// CreateDefaultFakeClient returns a basic fake client without error configurations.
func CreateDefaultFakeClient() client.Client {
	return fake.NewClientBuilder().Build()
}

// CreateFakeClientForObjects creates a fake client.Client with initial set of existing objects
// along with any expected errors for the Get, Create, Patch, and Delete client methods.
func CreateFakeClientForObjects(getErr, createErr, patchErr, deleteErr *apierrors.StatusError, existingObjects []client.Object) client.Client {
	clientBuilder := NewTestClientBuilder()
	if len(existingObjects) > 0 {
		clientBuilder.WithObjects(existingObjects...)
	}

	for _, obj := range existingObjects {
		objKey := client.ObjectKeyFromObject(obj)
		clientBuilder.RecordErrorForObjects(ClientMethodGet, getErr, objKey)
		clientBuilder.RecordErrorForObjects(ClientMethodCreate, createErr, objKey)
		clientBuilder.RecordErrorForObjects(ClientMethodPatch, patchErr, objKey)
		clientBuilder.RecordErrorForObjects(ClientMethodDelete, deleteErr, objKey)
	}
	return clientBuilder.Build()
}

// CreateFakeClientForObjectsMatchingLabels creates a fake client.Client with initial set of existing objects
// along with any expected list and/or deleteAll errors for objects matching the given matching labels for the given GVK.
// If the matchingLabels is nil or empty then the deleteAll/list error will be recorded for all objects matching the GVK in the given namespace.
func CreateFakeClientForObjectsMatchingLabels(deleteErr, listErr *apierrors.StatusError, namespace string, targetObjectsGVK schema.GroupVersionKind, matchingLabels map[string]string, existingObjects ...client.Object) client.Client {
	clientBuilder := NewTestClientBuilder()
	for _, existingObject := range existingObjects {
		// Errors recorded for individual object delete
		clientBuilder.WithObjects(existingObject).
			RecordErrorForObjectsMatchingLabels(ClientMethodDelete, client.ObjectKeyFromObject(existingObject), targetObjectsGVK, matchingLabels, deleteErr)
	}
	return clientBuilder.
		RecordErrorForObjectsMatchingLabels(ClientMethodList, client.ObjectKey{Namespace: namespace}, targetObjectsGVK, matchingLabels, listErr).
		RecordErrorForObjectsMatchingLabels(ClientMethodDelete, client.ObjectKey{Namespace: namespace}, targetObjectsGVK, matchingLabels, deleteErr).
		Build()
}

// ------------------- Functions to explicitly create and configure a test client builder -------------------

// NewTestClientBuilder creates a new TestClientBuilder initialized with the Grove scheme.
func NewTestClientBuilder() *TestClientBuilder {
	return &TestClientBuilder{
		delegatingClientBuilder: fake.NewClientBuilder(),
		scheme:                  groveclientscheme.Scheme,
	}
}

// WithClient configures the builder to use a pre-configured client for delegation.
func (b *TestClientBuilder) WithClient(cl client.Client) *TestClientBuilder {
	b.delegatingClient = cl
	return b
}

// WithObjects adds the provided objects to the fake client's initial state.
func (b *TestClientBuilder) WithObjects(objects ...client.Object) *TestClientBuilder {
	if len(objects) > 0 {
		b.delegatingClientBuilder.WithObjects(objects...)
	}
	return b
}

// RecordErrorForObjects configures errors to be returned for specific operations on given objects.
// If err is nil, no error record is created.
func (b *TestClientBuilder) RecordErrorForObjects(method ClientMethod, err *apierrors.StatusError, objectKeys ...client.ObjectKey) *TestClientBuilder {
	if err == nil {
		return b
	}
	for _, objectKey := range objectKeys {
		b.errorRecords = append(b.errorRecords, errorRecord{
			method:    method,
			objectKey: objectKey,
			err:       err,
		})
	}
	return b
}

// RecordErrorForObjectsMatchingLabels configures errors for operations on objects matching specific labels.
// Used primarily for List and DeleteAll operations that target multiple resources.
func (b *TestClientBuilder) RecordErrorForObjectsMatchingLabels(method ClientMethod, objectKey client.ObjectKey, targetObjectsGVK schema.GroupVersionKind, matchingLabels map[string]string, err *apierrors.StatusError) *TestClientBuilder {
	if err == nil {
		return b
	}
	b.errorRecords = append(b.errorRecords, errorRecord{
		method:      method,
		objectKey:   objectKey,
		resourceGVK: targetObjectsGVK,
		labels:      matchingLabels,
		err:         err,
	})
	return b
}

// Build creates and returns a new test client with the configured error behaviors.
func (b *TestClientBuilder) Build() client.Client {
	return &testClient{
		delegate:     b.getClient(),
		errorRecords: b.errorRecords,
	}
}

// getClient returns either the pre-configured client or builds a new one with the scheme.
func (b *TestClientBuilder) getClient() client.Client {
	if b.delegatingClient != nil {
		return b.delegatingClient
	}
	return b.delegatingClientBuilder.WithScheme(b.scheme).Build()
}

// ---------------------------------- Implementation of client.Client ----------------------------------

// testClient implements client.Client with support for replaying pre-configured errors.
type testClient struct {
	delegate     client.Client // underlying client implementation
	errorRecords []errorRecord // configured error conditions
}

// Get retrieves a single resource, returning any pre-configured error or delegating to the underlying client.
func (c *testClient) Get(ctx context.Context, objKey client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := c.getRecordedObjectError(ClientMethodGet, objKey); err != nil {
		return err
	}
	return c.delegate.Get(ctx, objKey, obj, opts...)
}

// List retrieves multiple resources, applying any pre-configured errors based on namespace and labels.
func (c *testClient) List(ctx context.Context, objList client.ObjectList, opts ...client.ListOption) error {
	listOpts := client.ListOptions{}
	listOpts.ApplyOptions(opts)
	gvk, err := apiutil.GVKForObject(objList, c.delegate.Scheme())
	if err != nil {
		return err
	}
	// Convert list GVK to item GVK (remove "List" suffix from Kind)
	// Special case: PartialObjectMetadataList already has the item GVK set
	itemGVK := gvk
	if strings.HasSuffix(gvk.Kind, "List") {
		itemGVK = schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind[:len(gvk.Kind)-4], // Remove "List" suffix
		}
	}
	if err = c.getRecordedObjectCollectionError(ClientMethodList, listOpts.Namespace, listOpts.LabelSelector, itemGVK); err != nil {
		return err
	}
	return c.delegate.List(ctx, objList, opts...)
}

// Create attempts to create a resource, returning any pre-configured error or delegating the operation.
func (c *testClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if err := c.getRecordedObjectError(ClientMethodCreate, client.ObjectKeyFromObject(obj)); err != nil {
		return err
	}
	return c.delegate.Create(ctx, obj, opts...)
}

// Delete attempts to delete a resource, returning any pre-configured error or delegating the operation.
func (c *testClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if err := c.getRecordedObjectError(ClientMethodDelete, client.ObjectKeyFromObject(obj)); err != nil {
		return err
	}
	return c.delegate.Delete(ctx, obj, opts...)
}

// DeleteAllOf deletes all matching resources, applying any pre-configured errors based on namespace and labels.
func (c *testClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	deleteOpts := client.DeleteAllOfOptions{}
	deleteOpts.ApplyOptions(opts)
	gvk, err := apiutil.GVKForObject(obj, c.delegate.Scheme())
	if err != nil {
		return err
	}
	if err = c.getRecordedObjectCollectionError(ClientMethodDeleteAll, deleteOpts.Namespace, deleteOpts.LabelSelector, gvk); err != nil {
		return err
	}
	return c.delegate.DeleteAllOf(ctx, obj, opts...)
}

// Patch applies a patch to a resource, returning any pre-configured error or delegating the operation.
func (c *testClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if err := c.getRecordedObjectError(ClientMethodPatch, client.ObjectKeyFromObject(obj)); err != nil {
		return err
	}
	return c.delegate.Patch(ctx, obj, patch, opts...)
}

// Update modifies an existing resource, returning any pre-configured error or delegating the operation.
func (c *testClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if err := c.getRecordedObjectError(ClientMethodUpdate, client.ObjectKeyFromObject(obj)); err != nil {
		return err
	}
	return c.delegate.Update(ctx, obj, opts...)
}

// Status returns a StatusWriter for updating status subresource.
func (c *testClient) Status() client.StatusWriter {
	return c.delegate.Status()
}

// SubResource returns a SubResourceClient for the specified subresource.
func (c *testClient) SubResource(subResource string) client.SubResourceClient {
	return c.delegate.SubResource(subResource)
}

// Scheme returns the scheme used by this client.
func (c *testClient) Scheme() *runtime.Scheme {
	return c.delegate.Scheme()
}

// RESTMapper returns the RESTMapper used by this client.
func (c *testClient) RESTMapper() meta.RESTMapper {
	return c.delegate.RESTMapper()
}

// GroupVersionKindFor returns the GVK for the given object.
func (c *testClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return c.delegate.GroupVersionKindFor(obj)
}

// IsObjectNamespaced reports if the given object is namespaced.
func (c *testClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return c.delegate.IsObjectNamespaced(obj)
}

// ---------------------------------- Helper methods ----------------------------------

// getRecordedObjectError returns any pre-configured error for a specific operation and object key.
func (c *testClient) getRecordedObjectError(method ClientMethod, objKey client.ObjectKey) error {
	foundErrorRecord, ok := lo.Find(c.errorRecords, func(errRecord errorRecord) bool {
		return errRecord.method == method && errRecord.objectKey == objKey
	})
	return lo.Ternary(ok, foundErrorRecord.err, nil)
}

// getRecordedObjectCollectionError returns any pre-configured error for collection operations
// based on namespace, labels, and resource type.
func (c *testClient) getRecordedObjectCollectionError(method ClientMethod, namespace string, labelSelector labels.Selector, objGVK schema.GroupVersionKind) error {
	foundErrRecord, ok := lo.Find(c.errorRecords, func(errRecord errorRecord) bool {
		// Match method and GVK
		if errRecord.method != method || errRecord.resourceGVK != objGVK {
			return false
		}

		// For testing purposes, match namespace more loosely:
		// - If both are empty, match
		// - If error record has namespace and request has same namespace, match
		// - If error record has namespace and request is empty, match (for testing)
		// - If error record is empty and request has namespace, no match
		if errRecord.objectKey.Namespace != "" && namespace != "" && errRecord.objectKey.Namespace != namespace {
			return false
		}

		// If the error record has labels, check if the selector matches
		if len(errRecord.labels) > 0 {
			if labelSelector != nil {
				return labelSelector.Matches(errRecord.labels)
			}
			// For testing purposes, if record has labels but no selector provided, still match
			// This allows tests to configure errors for specific label scenarios
			return true
		}
		// If record has no labels, it matches any selector (or no selector)
		return true
	})
	return lo.Ternary(ok, foundErrRecord.err, nil)
}
