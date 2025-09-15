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

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	groveclientscheme "github.com/NVIDIA/grove/operator/internal/client"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ============================================================================
// Common Test Setup Functions
// ============================================================================

// SetupFakeClient creates a fake Kubernetes client with Grove CRDs and status subresources.
func SetupFakeClient(objects ...client.Object) client.WithWatch {
	return fake.NewClientBuilder().
		WithScheme(groveclientscheme.Scheme).
		WithStatusSubresource(&grovecorev1alpha1.PodGangSet{}).
		WithStatusSubresource(&grovecorev1alpha1.PodCliqueScalingGroup{}).
		WithStatusSubresource(&grovecorev1alpha1.PodClique{}).
		WithObjects(objects...).
		Build()
}

// SetupTestLogger returns a discarded logger for tests to avoid log noise.
func SetupTestLogger() logr.Logger {
	return logr.Discard()
}

// SetupTestContext returns a background context for tests.
func SetupTestContext() context.Context {
	return context.Background()
}
