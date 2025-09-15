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

// Package utils provides utility functions for controller operations.
package utils

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// AddAndPatchFinalizer adds a finalizer to the object using merge-patch strategy
// with optimistic locking to prevent conflicts during concurrent updates.
func AddAndPatchFinalizer(ctx context.Context, writer client.Writer, obj client.Object, finalizer string) error {
	return patchFinalizer(ctx,
		writer,
		obj,
		mergeFromWithOptimisticLock,
		controllerutil.AddFinalizer,
		finalizer,
	)
}

// RemoveAndPatchFinalizer removes a finalizer from the object using merge-patch strategy
// with optimistic locking. Returns nil if the object is not found.
func RemoveAndPatchFinalizer(ctx context.Context, writer client.Writer, obj client.Object, finalizer string) error {
	return client.IgnoreNotFound(
		patchFinalizer(ctx,
			writer,
			obj,
			mergeFromWithOptimisticLock,
			controllerutil.RemoveFinalizer,
			finalizer,
		),
	)
}

// mergeFromWithOptimisticLock creates a merge patch with optimistic locking enabled
// to prevent conflicts during concurrent updates.
func mergeFromWithOptimisticLock(obj client.Object, opts ...client.MergeFromOption) client.Patch {
	return client.MergeFromWithOptions(obj, append(opts, client.MergeFromWithOptimisticLock{})...)
}

// patchFn creates a client.Patch using the given object as the base.
type patchFn func(client.Object, ...client.MergeFromOption) client.Patch

// mutateFn modifies an object by adding or removing the specified finalizer.
// Returns true if the object was modified.
type mutateFn func(client.Object, string) bool

// patchFinalizer applies a finalizer mutation to an object and patches it to the API server.
// It creates a deep copy before mutation to generate the proper patch.
func patchFinalizer(ctx context.Context, writer client.Writer, obj client.Object, patchFunc patchFn, mutateFunc mutateFn, finalizer string) error {
	// Create a copy of the object before mutation for patch generation
	beforePatch := obj.DeepCopyObject().(client.Object)

	// Apply the finalizer mutation (add or remove)
	mutateFunc(obj, finalizer)

	// Patch the object with the changes
	return writer.Patch(ctx, obj, patchFunc(beforePatch))
}
