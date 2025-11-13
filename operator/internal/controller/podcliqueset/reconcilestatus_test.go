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

package podcliqueset

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	testutils "github.com/ai-dynamo/grove/operator/test/utils"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Test constants
const (
	testNamespace = "test-namespace"
	testPCSName   = "test-pcs"
)

func TestComputePCSAvailableReplicas(t *testing.T) {
	pcsGenerationHash := string(uuid.NewUUID())
	pcsUID := uuid.NewUUID()
	testCases := []struct {
		name                         string
		setupPCS                     func() *grovecorev1alpha1.PodCliqueSet
		childResources               func() []client.Object
		expectedAvailable            int32
		expectedUnavailableIndices   []int32
	}{
		{
			name: "all healthy - 2 replicas with standalone and scaling groups",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(2).
					WithStandaloneClique("worker").
					WithScalingGroup("compute", []string{"frontend", "backend"}).
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					// Healthy PCSGs
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-0-compute", testNamespace, testPCSName, 0).
						WithOptions(testutils.WithPCSGAvailableReplicas(1)).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-1-compute", testNamespace, testPCSName, 1).
						WithOptions(testutils.WithPCSGAvailableReplicas(1)).Build(),
					// Healthy standalone PodCliques
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 1).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
				}
			},
			expectedAvailable:          2,
			expectedUnavailableIndices: []int32{},
		},
		{
			name: "mixed health - 1 healthy, 1 unhealthy",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(2).
					WithStandaloneClique("worker").
					WithScalingGroup("compute", []string{"frontend"}).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-0-compute", testNamespace, testPCSName, 0).
						WithOptions(testutils.WithPCSGAvailableReplicas(1)).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-1-compute", testNamespace, testPCSName, 1).
						WithOptions(testutils.WithPCSGMinAvailableBreached()).Build(),
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 1).
						WithOptions(testutils.WithPCLQTerminating(), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
				}
			},
			expectedAvailable:          1,
			expectedUnavailableIndices: []int32{1},
		},
		{
			name: "count mismatch - missing resources",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(1).
					WithStandaloneClique("worker").
					WithScalingGroup("compute", []string{"frontend"}).
					Build()
			},
			childResources: func() []client.Object {
				// Missing PCSG, extra standalone PodClique
				return []client.Object{
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "test-pcs-0-worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQAvailable(), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "test-pcs-0-extra", testNamespace, 0).
						WithOptions(testutils.WithPCLQAvailable(), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
				}
			},
			expectedAvailable:          0,
			expectedUnavailableIndices: []int32{0},
		},
		{
			name: "empty configuration",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(1).
					Build()
			},
			childResources:             func() []client.Object { return []client.Object{} },
			expectedAvailable:          1,
			expectedUnavailableIndices: []int32{},
		},
		{
			name: "only standalone cliques - all healthy",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(2).
					WithStandaloneClique("worker").
					WithStandaloneClique("monitor").
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "monitor", testNamespace, 0).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 1).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "monitor", testNamespace, 1).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
				}
			},
			expectedAvailable:          2,
			expectedUnavailableIndices: []int32{},
		},
		{
			name: "only scaling groups - all healthy",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(2).
					WithScalingGroup("compute", []string{"frontend", "backend"}).
					WithScalingGroup("storage", []string{"database"}).
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-0-compute", testNamespace, testPCSName, 0).
						WithOptions(testutils.WithPCSGAvailableReplicas(1)).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-0-storage", testNamespace, testPCSName, 0).
						WithOptions(testutils.WithPCSGAvailableReplicas(1)).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-1-compute", testNamespace, testPCSName, 1).
						WithOptions(testutils.WithPCSGAvailableReplicas(1)).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-1-storage", testNamespace, testPCSName, 1).
						WithOptions(testutils.WithPCSGAvailableReplicas(1)).Build(),
				}
			},
			expectedAvailable:          2,
			expectedUnavailableIndices: []int32{},
		},
		{
			name: "terminating resources",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(1).
					WithStandaloneClique("worker").
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "test-pcs-0-worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQTerminating(), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
				}
			},
			expectedAvailable:          0,
			expectedUnavailableIndices: []int32{0},
		},
		{
			name: "unknown condition status",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(1).
					WithScalingGroup("compute", []string{"frontend"}).
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-0-compute", testNamespace, testPCSName, 0).
						WithOptions(testutils.WithPCSGUnknownCondition()).Build(),
				}
			},
			expectedAvailable:          0,
			expectedUnavailableIndices: []int32{0},
		},
		{
			name: "no conditions set",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(1).
					WithStandaloneClique("worker").
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "test-pcs-0-worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQNoConditions()).Build(),
				}
			},
			expectedAvailable:          0,
			expectedUnavailableIndices: []int32{0},
		},
		// Edge cases
		{
			name: "no child resources when expected",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(1).
					WithScalingGroup("compute", []string{"frontend"}).
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources:             func() []client.Object { return []client.Object{} },
			expectedAvailable:          0,
			expectedUnavailableIndices: []int32{0},
		},
		{
			name: "extra unexpected resources",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(1).
					WithStandaloneClique("worker").
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-0-unexpected", testNamespace, testPCSName, 0).
						WithOptions(testutils.WithPCSGAvailableReplicas(1)).Build(),
				}
			},
			expectedAvailable:          1,
			expectedUnavailableIndices: []int32{},
		},
		{
			name: "multiple non-contiguous unavailable replicas",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(5).
					WithStandaloneClique("worker").
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					// Replica 0 - unavailable (terminating)
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQTerminating(), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					// Replica 1 - available
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 1).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					// Replica 2 - unavailable (no conditions)
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 2).
						WithOptions(testutils.WithPCLQNoConditions()).Build(),
					// Replica 3 - available
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 3).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(1), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					// Replica 4 - unavailable (missing - no resources created)
				}
			},
			expectedAvailable:          2,
			expectedUnavailableIndices: []int32{0, 2, 4},
		},
		{
			name: "all replicas unavailable",
			setupPCS: func() *grovecorev1alpha1.PodCliqueSet {
				return testutils.NewPodCliqueSetBuilder(testPCSName, testNamespace, pcsUID).
					WithReplicas(3).
					WithStandaloneClique("worker").
					WithScalingGroup("compute", []string{"frontend"}).
					WithPodCliqueSetGenerationHash(&pcsGenerationHash).
					Build()
			},
			childResources: func() []client.Object {
				return []client.Object{
					// Replica 0 - unavailable (insufficient ready replicas)
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 0).
						WithOptions(testutils.WithPCLQReplicaReadyStatus(0), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-0-compute", testNamespace, testPCSName, 0).
						WithOptions(testutils.WithPCSGMinAvailableBreached()).Build(),
					// Replica 1 - unavailable (terminating)
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 1).
						WithOptions(testutils.WithPCLQTerminating(), testutils.WithPCLQCurrentPCSGenerationHash(pcsGenerationHash)).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-1-compute", testNamespace, testPCSName, 1).
						WithOptions(testutils.WithPCSGMinAvailableBreached()).Build(),
					// Replica 2 - unavailable (no conditions)
					testutils.NewPodCliqueBuilder(testPCSName, uuid.NewUUID(), "worker", testNamespace, 2).
						WithOptions(testutils.WithPCLQNoConditions()).Build(),
					testutils.NewPodCliqueScalingGroupBuilder("test-pcs-2-compute", testNamespace, testPCSName, 2).
						WithOptions(testutils.WithPCSGUnknownCondition()).Build(),
				}
			},
			expectedAvailable:          0,
			expectedUnavailableIndices: []int32{0, 1, 2},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			//t.Parallel()
			pcs := tt.setupPCS()
			existingObjects := []client.Object{pcs}
			existingObjects = append(existingObjects, tt.childResources()...)
			cl := testutils.CreateDefaultFakeClient(existingObjects)
			reconciler := &Reconciler{client: cl}
			// Compute available replicas
			available, _, unavailableIndices, err := reconciler.computeReplicaMetrics(context.Background(), logr.Discard(), pcs)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedAvailable, available, "Available replicas mismatch")
			assert.Equal(t, tt.expectedUnavailableIndices, unavailableIndices, "Unavailable replica indices mismatch")
		})
	}
}
