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

package pod

import (
	"context"
	"testing"

	"github.com/ai-dynamo/grove/operator/api/common"
	"github.com/ai-dynamo/grove/operator/internal/expect"
	testutils "github.com/ai-dynamo/grove/operator/test/utils"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testOldTemplateHash      = "old-hash-abc123"
	testExpectedTemplateHash = "new-hash-xyz789"
	testNamespace            = "default"
)

// TestComputeUpdateWork_PodCategorizationGap tests that computeUpdateWork correctly
// categorizes all pods with old template hash into appropriate buckets.
// This is a regression test for the bug where pods with failing startup probes
// were not categorized, causing rolling updates to complete prematurely.
func TestComputeUpdateWork_PodCategorizationGap(t *testing.T) {
	tests := []struct {
		name                      string
		pod                       *corev1.Pod
		expectedInPendingBucket   bool
		expectedInUnhealthyBucket bool
		expectedInReadyBucket     bool
		description               string
	}{
		{
			name:                      "pending pod is categorized as pending",
			pod:                       createRollingUpdateTestPod("pending-pod", corev1.PodPending, nil, false, false),
			expectedInPendingBucket:   true,
			expectedInUnhealthyBucket: false,
			expectedInReadyBucket:     false,
			description:               "Pending pods should be in pending bucket",
		},
		{
			name:                      "started but not ready pod is categorized as unhealthy",
			pod:                       createRollingUpdateTestPod("started-not-ready-pod", corev1.PodRunning, ptr.To(true), false, false),
			expectedInPendingBucket:   false,
			expectedInUnhealthyBucket: true,
			expectedInReadyBucket:     false,
			description:               "Pods with Started=true but Ready=false should be unhealthy",
		},
		{
			name:                      "ready pod is categorized as ready",
			pod:                       createRollingUpdateTestPod("ready-pod", corev1.PodRunning, ptr.To(true), true, true),
			expectedInPendingBucket:   false,
			expectedInUnhealthyBucket: false,
			expectedInReadyBucket:     true,
			description:               "Ready pods should be in ready bucket",
		},
		{
			// BUG CASE: Pod with failing startup probe (Started=false, container running)
			name:                      "startup probe failing pod should be categorized as unhealthy",
			pod:                       createRollingUpdateTestPod("startup-failing-pod", corev1.PodRunning, ptr.To(false), false, false),
			expectedInPendingBucket:   false,
			expectedInUnhealthyBucket: true,
			expectedInReadyBucket:     false,
			description:               "Pods with failing startup probe should be unhealthy",
		},
		{
			// BUG CASE: Pod with Started=nil (startup probe not yet evaluated)
			name:                      "startup probe pending pod should be categorized as unhealthy",
			pod:                       createRollingUpdateTestPod("startup-nil-pod", corev1.PodRunning, nil, false, false),
			expectedInPendingBucket:   false,
			expectedInUnhealthyBucket: true,
			expectedInReadyBucket:     false,
			description:               "Pods with Started=nil should be unhealthy",
		},
	}

	// Create fake client and resource
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &_resource{
		client:            fakeClient,
		expectationsStore: expect.NewExpectationsStore(),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &syncContext{
				ctx:                      context.Background(),
				existingPCLQPods:         []*corev1.Pod{tt.pod},
				expectedPodTemplateHash:  testExpectedTemplateHash,
				pclqExpectationsStoreKey: "default/test-pclq",
			}

			work := r.computeUpdateWork(logr.Discard(), sc)

			inPending := containsPodByName(work.oldTemplateHashPendingPods, tt.pod.Name)
			inUnhealthy := containsPodByName(work.oldTemplateHashUnhealthyPods, tt.pod.Name)
			inReady := containsPodByName(work.oldTemplateHashReadyPods, tt.pod.Name)

			// Check if pod is categorized at all
			podCategorized := inPending || inUnhealthy || inReady
			if !podCategorized {
				t.Errorf("Pod %q was not categorized into any bucket - this causes rolling update to complete prematurely", tt.pod.Name)
			}

			assert.Equal(t, tt.expectedInPendingBucket, inPending, "pending bucket: %s", tt.description)
			assert.Equal(t, tt.expectedInUnhealthyBucket, inUnhealthy, "unhealthy bucket: %s", tt.description)
			assert.Equal(t, tt.expectedInReadyBucket, inReady, "ready bucket: %s", tt.description)
		})
	}
}

// TestComputeUpdateWork_AllOldHashPodsCategorized verifies that all pods with old
// template hash are categorized into exactly one bucket.
func TestComputeUpdateWork_AllOldHashPodsCategorized(t *testing.T) {
	pods := []*corev1.Pod{
		createRollingUpdateTestPod("pending", corev1.PodPending, nil, false, false),
		createRollingUpdateTestPod("started-not-ready", corev1.PodRunning, ptr.To(true), false, false),
		createRollingUpdateTestPod("ready", corev1.PodRunning, ptr.To(true), true, true),
		createRollingUpdateTestPod("startup-failing", corev1.PodRunning, ptr.To(false), false, false),
		createRollingUpdateTestPod("startup-nil", corev1.PodRunning, nil, false, false),
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &_resource{
		client:            fakeClient,
		expectationsStore: expect.NewExpectationsStore(),
	}

	sc := &syncContext{
		ctx:                      context.Background(),
		existingPCLQPods:         pods,
		expectedPodTemplateHash:  testExpectedTemplateHash,
		pclqExpectationsStoreKey: "default/test-pclq",
	}

	work := r.computeUpdateWork(logr.Discard(), sc)

	// Build map of categorized pods
	categorized := make(map[string]string)
	for _, p := range work.oldTemplateHashPendingPods {
		categorized[p.Name] = "pending"
	}
	for _, p := range work.oldTemplateHashUnhealthyPods {
		categorized[p.Name] = "unhealthy"
	}
	for _, p := range work.oldTemplateHashReadyPods {
		categorized[p.Name] = "ready"
	}

	// Verify all pods are categorized
	for _, pod := range pods {
		_, found := categorized[pod.Name]
		assert.True(t, found, "Pod %q should be categorized into a bucket", pod.Name)
	}

	assert.Equal(t, len(pods), len(categorized),
		"All %d pods should be categorized, got %d", len(pods), len(categorized))
}

// TestComputeUpdateWork_NewHashPodsNotInOldBuckets verifies pods with new template
// hash are not placed in old hash buckets.
func TestComputeUpdateWork_NewHashPodsNotInOldBuckets(t *testing.T) {
	// Create a pod with new template hash
	newHashPod := testutils.NewPodBuilder("new-hash-pod", testNamespace).
		WithLabels(map[string]string{
			common.LabelPodTemplateHash: testExpectedTemplateHash,
		}).
		Build()
	newHashPod.Status.Phase = corev1.PodRunning
	newHashPod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &_resource{
		client:            fakeClient,
		expectationsStore: expect.NewExpectationsStore(),
	}

	sc := &syncContext{
		ctx:                      context.Background(),
		existingPCLQPods:         []*corev1.Pod{newHashPod},
		expectedPodTemplateHash:  testExpectedTemplateHash,
		pclqExpectationsStoreKey: "default/test-pclq",
	}

	work := r.computeUpdateWork(logr.Discard(), sc)

	assert.Empty(t, work.oldTemplateHashPendingPods, "new hash pod should not be in old pending bucket")
	assert.Empty(t, work.oldTemplateHashUnhealthyPods, "new hash pod should not be in old unhealthy bucket")
	assert.Empty(t, work.oldTemplateHashReadyPods, "new hash pod should not be in old ready bucket")
	assert.Len(t, work.newTemplateHashReadyPods, 1, "new hash pod should be in new ready bucket")
}

// Helper functions for rolling update tests
// -------------------------------------------------------------------------------------------

// createRollingUpdateTestPod creates a pod for rolling update tests with the specified state.
func createRollingUpdateTestPod(name string, phase corev1.PodPhase, started *bool, containerReady bool, podReady bool) *corev1.Pod {
	pod := testutils.NewPodBuilder(name, testNamespace).
		WithLabels(map[string]string{
			common.LabelPodTemplateHash: testOldTemplateHash,
		}).
		Build()

	pod.Status.Phase = phase

	// Add container status for running pods
	if phase == corev1.PodRunning {
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{
				Name:    "main",
				Started: started,
				Ready:   containerReady,
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{
						StartedAt: metav1.Now(),
					},
				},
			},
		}
	}

	// Add pod ready condition if needed
	if podReady {
		pod.Status.Conditions = []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		}
	}

	return pod
}

// containsPodByName checks if a pod with the given name exists in the slice.
func containsPodByName(pods []*corev1.Pod, name string) bool {
	for _, p := range pods {
		if p.Name == name {
			return true
		}
	}
	return false
}
