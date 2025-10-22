//go:build e2e

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

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	grovev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/e2e_testing/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// Test_RU7_RollingUpdatePCSPodClique tests rolling update when PCS-owned Podclique spec is updated
// Scenario RU-7:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Change the specification of pc-a
// 4. Verify that only one pod is deleted at a time
// 5. Verify that a single PCS replica is updated first before moving to another
func Test_RU7_RollingUpdatePCSPodClique(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	logger.Info("2. Deploy workload WL1, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != expectedPods {
		t.Fatalf("Expected %d pods, but found %d", expectedPods, len(pods.Items))
	}

	// Set up watcher before triggering update
	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	// Wait to ensure the pod watcher is fully established before triggering the rolling update.
	// This prevents a race condition where early pod events (deletions/additions) could be
	// missed if the watcher hasn't fully subscribed to the API server's watch stream.
	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a")
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", "pc-a")
	if err != nil {
		t.Fatalf("Failed to update PodClique spec: %v", err)
	}

	// Wait for rolling update to complete
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 1, 1*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	// Stop tracking now that the update is complete
	tracker.Stop()

	logger.Info("4. Verify that only one pod is deleted at a time")
	events := tracker.getEvents()
	verifyOnePodDeletedAtATime(t, events)

	logger.Info("5. Verify that a single PCS replica is updated first before moving to another")
	verifySinglePCSReplicaUpdatedFirst(t, events)

	logger.Info("🎉 Rolling Update on PCS-owned Podclique test (RU-7) completed successfully!")
}

// Test_RU8_RollingUpdatePCSGPodClique tests rolling update when PCSG-owned Podclique spec is updated
// Scenario RU-8:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Change the specification of pc-b
// 4. Verify that only one PCSG replica is deleted at a time
// 5. Verify that a single PCS replica is updated first before moving to another
func Test_RU8_RollingUpdatePCSGPodClique(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	logger.Info("2. Deploy workload WL1, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != expectedPods {
		t.Fatalf("Expected %d pods, but found %d", expectedPods, len(pods.Items))
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	// Wait to ensure the pod watcher is fully established before triggering the rolling update.
	// This prevents a race condition where early pod events (deletions/additions) could be
	// missed if the watcher hasn't fully subscribed to the API server's watch stream.
	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-b")
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", "pc-b")
	if err != nil {
		t.Fatalf("Failed to update PodClique spec: %v", err)
	}

	// Wait for rolling update to complete
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 1, 1*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	// Stop tracking now that the update is complete
	tracker.Stop()

	logger.Info("4. Verify that only one PCSG replica is deleted at a time")
	events := tracker.getEvents()
	verifyOnePCSGReplicaDeletedAtATime(t, events)

	logger.Info("5. Verify that a single PCS replica is updated first before moving to another")
	verifySinglePCSReplicaUpdatedFirst(t, events)

	logger.Info("🎉 Rolling Update on PCSG-owned Podclique test (RU-8) completed successfully!")
}

// Test_RU9_RollingUpdateAllPodCliques tests rolling update when all Podclique specs are updated
// Scenario RU-9:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Verify that only one pod in each Podclique and one replica in each PCSG is deleted at a time
// 5. Verify that a single PCS replica is updated first before moving to another
func Test_RU9_RollingUpdateAllPodCliques(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	logger.Info("2. Deploy workload WL1, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != expectedPods {
		t.Fatalf("Expected %d pods, but found %d", expectedPods, len(pods.Items))
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	// Wait to ensure the pod watcher is fully established before triggering the rolling update.
	// This prevents a race condition where early pod events (deletions/additions) could be
	// missed if the watcher hasn't fully subscribed to the API server's watch stream.
	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	// Wait for rolling update to complete
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 1, 1*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	// Stop tracking now that the update is complete
	tracker.Stop()

	logger.Info("4. Verify that only one pod in each Podclique and one replica in each PCSG is deleted at a time")
	events := tracker.getEvents()
	verifySinglePCSReplicaUpdatedFirst(t, events)
	verifyOnePodDeletedAtATimePerPodclique(t, events)
	verifyOnePCSGReplicaDeletedAtATimePerPCSG(t, events)

	logger.Info("5. Verify that a single PCS replica is updated first before moving to another")
	verifySinglePCSReplicaUpdatedFirst(t, events)

	logger.Info("🎉 Rolling Update on all Podcliques test (RU-9) completed successfully!")
}

// Test_RU10_RollingUpdateInsufficientResources tests rolling update with insufficient resources
// Scenario RU-10:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Cordon all worker nodes
// 4. Change the specification of pc-a
// 5. Verify the rolling update does not progress due to insufficient resources
// 6. Uncordon the nodes, and verify the rolling update continues
func Test_RU10_RollingUpdateInsufficientResources(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	logger.Info("2. Deploy workload WL1, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("3. Cordon all worker nodes")
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	for _, nodeName := range agentNodes {
		if err := utils.CordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// Capture the existing pods before starting the tracker
	existingPods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list existing pods: %v", err)
	}

	// Capture the existing pods names for verification later
	existingPodNames := make(map[string]bool)
	for _, pod := range existingPods.Items {
		existingPodNames[pod.Name] = true
	}
	logger.Infof("Captured %d existing pods before rolling update", len(existingPodNames))

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	// Wait to ensure the pod watcher is fully established before triggering the rolling update.
	// This prevents a race condition where early pod events (deletions/additions) could be
	// missed if the watcher hasn't fully subscribed to the API server's watch stream.
	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("4. Change the specification of pc-a")
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", "pc-a")
	if err != nil {
		t.Fatalf("Failed to update PodClique spec: %v", err)
	}

	logger.Info("5. Verify the rolling update does not progress due to insufficient resources")
	time.Sleep(1 * time.Minute)

	// Verify that none of the existing pods were deleted during the insufficient resources period
	events := tracker.getEvents()
	var deletedExistingPods []string
	for _, event := range events {
		switch event.Type {
		case watch.Deleted:
			if existingPodNames[event.Pod.Name] {
				deletedExistingPods = append(deletedExistingPods, event.Pod.Name)
				logger.Debugf("Existing pod deleted during insufficient resources: %s", event.Pod.Name)
			}
		}
	}

	if len(deletedExistingPods) > 0 {
		t.Fatalf("Rolling update progressed despite insufficient resources: %d existing pods deleted: %v",
			len(deletedExistingPods), deletedExistingPods)
	}

	logger.Info("6. Uncordon the nodes, and verify the rolling update continues")
	for _, nodeName := range agentNodes {
		if err := utils.CordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for rolling update to complete after uncordoning
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 1, 1*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	logger.Info("🎉 Rolling Update with insufficient resources test (RU-10) completed successfully!")
}

// Test_RU11_RollingUpdateWithPCSScaleOut tests rolling update with scale-out on PCS
// Scenario RU-11:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a
// 4. Scale out the PCS during the rolling update
// 5. Verify the scaled out replica is created with the correct specifications
func Test_RU11_RollingUpdateWithPCSScaleOut(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// Scale PCS to 2 replicas first
	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a")
	err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", "pc-a")
	if err != nil {
		t.Fatalf("Failed to update PodClique spec: %v", err)
	}

	// Wait a bit for rolling update to start
	time.Sleep(5 * time.Second)

	logger.Info("4. Scale out the PCS during the rolling update")
	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 3, 30, 0)

	// Wait for rolling update to complete
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 3, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	logger.Info("5. Verify the scaled out replica is created with the correct specifications")
	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 30 {
		t.Fatalf("Expected 30 pods after scale-out, got %d", len(pods.Items))
	}

	// Verify all pods have the updated spec
	events := tracker.getEvents()
	logger.Debugf("Captured %d pod events during rolling update", len(events))

	logger.Info("🎉 Rolling Update with PCS scale-out test (RU-11) completed successfully!")
}

// Test_RU12_RollingUpdateWithPCSScaleInDuringUpdate tests rolling update with scale-in on PCS while final ordinal is being updated
// Scenario RU-12:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale in the PCS while the final ordinal is being updated
// 5. Verify the update goes through successfully
func Test_RU12_RollingUpdateWithPCSScaleInDuringUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	// Wait for rolling update to progress to final ordinal
	time.Sleep(10 * time.Second)

	logger.Info("4. Scale in the PCS while the final ordinal is being updated")
	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 1, 10, 0)

	logger.Info("5. Verify the update goes through successfully")
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 1, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 10 {
		t.Fatalf("Expected 10 pods after scale-in, got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PCS scale-in during update test (RU-12) completed successfully!")
}

// Test_RU13_RollingUpdateWithPCSScaleInAfterFinalOrdinal tests rolling update with scale-in on PCS after final ordinal finishes
// Scenario RU-13:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Wait for rolling update to complete on replica 1
// 5. Scale in the PCS after final ordinal has been updated
// 6. Verify the update goes through successfully
func Test_RU13_RollingUpdateWithPCSScaleInAfterFinalOrdinal(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	logger.Info("4. Wait for rolling update to complete on both replicas")
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	logger.Info("5. Scale in the PCS after final ordinal has been updated")
	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 1, 10, 0)

	logger.Info("6. Verify the update goes through successfully")
	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 10 {
		t.Fatalf("Expected 10 pods after scale-in, got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PCS scale-in after final ordinal test (RU-13) completed successfully!")
}

// Test_RU14_RollingUpdateWithPCSGScaleOutDuringUpdate tests rolling update with scale-out on PCSG being updated
// Scenario RU-14:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale out the PCSG during its rolling update
// 5. Verify the scaled out replica is created with the correct specifications
// 6. Verify it should not be updated again before the rolling update ends
func Test_RU14_RollingUpdateWithPCSGScaleOutDuringUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	// Wait for rolling update to start
	time.Sleep(5 * time.Second)

	logger.Info("4. Scale out the PCSG during its rolling update")
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "sg-x", 3, 26, 0)

	logger.Info("5. Verify the scaled out replica is created with the correct specifications")
	logger.Info("6. Verify it should not be updated again before the rolling update ends")

	// Wait for rolling update to complete
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 26 {
		t.Fatalf("Expected 26 pods after PCSG scale-out (2 pc-a + 12 pc-b + 12 pc-c), got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PCSG scale-out during update test (RU-14) completed successfully!")
}

// Test_RU15_RollingUpdateWithPCSGScaleOutBeforeUpdate tests rolling update with scale-out on PCSG before it is updated
// Scenario RU-15:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale out the PCSG before its rolling update starts
// 5. Verify the scaled out replica is created with the correct specifications
// 6. Verify it should not be updated again before the rolling update ends
func Test_RU15_RollingUpdateWithPCSGScaleOutBeforeUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("4. Scale out the PCSG before its rolling update starts")
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "sg-x", 3, 26, 0)

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	logger.Info("5. Verify the scaled out replica is created with the correct specifications")
	logger.Info("6. Verify it should not be updated again before the rolling update ends")

	// Wait for rolling update to complete
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 26 {
		t.Fatalf("Expected 26 pods after PCSG scale-out, got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PCSG scale-out before update test (RU-15) completed successfully!")
}

// Test_RU16_RollingUpdateWithPCSGScaleInDuringUpdate tests rolling update with scale-in on PCSG being updated
// Scenario RU-16:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale in the PCSG during its rolling update
// 5. Verify the update goes through successfully
func Test_RU16_RollingUpdateWithPCSGScaleInDuringUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	// Wait for rolling update to start
	time.Sleep(5 * time.Second)

	logger.Info("4. Scale in the PCSG during its rolling update")
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "sg-x", 1, 14, 0)

	logger.Info("5. Verify the update goes through successfully")
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 14 {
		t.Fatalf("Expected 14 pods after PCSG scale-in (2 pc-a + 4 pc-b + 4 pc-c), got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PCSG scale-in during update test (RU-16) completed successfully!")
}

// Test_RU17_RollingUpdateWithPCSGScaleInBeforeUpdate tests rolling update with scale-in on PCSG before it is updated
// Scenario RU-17:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale in the PCSG before its rolling update starts
// 5. Verify the update goes through successfully
func Test_RU17_RollingUpdateWithPCSGScaleInBeforeUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("4. Scale in the PCSG before its rolling update starts")
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "sg-x", 1, 14, 0)

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	logger.Info("5. Verify the update goes through successfully")
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 14 {
		t.Fatalf("Expected 14 pods after PCSG scale-in, got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PCSG scale-in before update test (RU-17) completed successfully!")
}

// Test_RU18_RollingUpdateWithPodCliqueScaleOutDuringUpdate tests rolling update with scale-out on standalone PCLQ being updated
// Scenario RU-18:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale out the standalone PCLQ (pc-a) during its rolling update
// 5. Verify the scaled pods are created with the correct specifications
// 6. Verify they should not be updated again before the rolling update ends
func Test_RU18_RollingUpdateWithPodCliqueScaleOutDuringUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	// Wait for rolling update to start
	time.Sleep(5 * time.Second)

	logger.Info("4. Scale out the standalone PCLQ (pc-a) during its rolling update")
	if err := scalePodCliqueInPCS(ctx, dynamicClient, workloadNamespace, "workload1", "pc-a", 4); err != nil {
		t.Fatalf("Failed to scale PodClique pc-a: %v", err)
	}

	// Wait for new pods to be created
	err = pollForCondition(ctx, defaultPollTimeout, defaultPollInterval, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) >= 24, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for scaled pods: %v", err)
	}

	logger.Info("5. Verify the scaled pods are created with the correct specifications")
	logger.Info("6. Verify they should not be updated again before the rolling update ends")

	// Wait for rolling update to complete
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 24 {
		t.Fatalf("Expected 24 pods after PodClique scale-out (4 pc-a + 4 pc-b + 12 pc-c), got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PodClique scale-out during update test (RU-18) completed successfully!")
}

// Test_RU19_RollingUpdateWithPodCliqueScaleOutBeforeUpdate tests rolling update with scale-out on standalone PCLQ before it is updated
// Scenario RU-19:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Scale out the standalone PCLQ (pc-a) before its rolling update
// 4. Change the specification of pc-a, pc-b and pc-c
// 5. Verify the scaled pods are created with the correct specifications
// 6. Verify they should not be updated again before the rolling update ends
func Test_RU19_RollingUpdateWithPodCliqueScaleOutBeforeUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("3. Scale out the standalone PCLQ (pc-a) before its rolling update")
	if err := scalePodCliqueInPCS(ctx, dynamicClient, workloadNamespace, "workload1", "pc-a", 4); err != nil {
		t.Fatalf("Failed to scale PodClique pc-a: %v", err)
	}

	// Wait for new pods to be created
	err = pollForCondition(ctx, defaultPollTimeout, defaultPollInterval, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) >= 24, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for scaled pods: %v", err)
	}

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for scaled pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("4. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	logger.Info("5. Verify the scaled pods are created with the correct specifications")
	logger.Info("6. Verify they should not be updated again before the rolling update ends")

	// Wait for rolling update to complete
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 24 {
		t.Fatalf("Expected 24 pods after PodClique scale-out, got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PodClique scale-out before update test (RU-19) completed successfully!")
}

// Test_RU20_RollingUpdateWithPodCliqueScaleInDuringUpdate tests rolling update with scale-in on standalone PCLQ being updated
// Scenario RU-20:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale in the standalone PCLQ (pc-a) during its rolling update
// 5. Verify the update goes through successfully
func Test_RU20_RollingUpdateWithPodCliqueScaleInDuringUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	// Wait for rolling update to start
	time.Sleep(5 * time.Second)

	logger.Info("4. Scale in the standalone PCLQ (pc-a) during its rolling update")
	if err := scalePodCliqueInPCS(ctx, dynamicClient, workloadNamespace, "workload1", "pc-a", 1); err != nil {
		t.Fatalf("Failed to scale PodClique pc-a: %v", err)
	}

	logger.Info("5. Verify the update goes through successfully")
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 18 {
		t.Fatalf("Expected 18 pods after PodClique scale-in (1 pc-a per replica), got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PodClique scale-in during update test (RU-20) completed successfully!")
}

// Test_RU21_RollingUpdateWithPodCliqueScaleInBeforeUpdate tests rolling update with scale-in on standalone PCLQ before it is updated
// Scenario RU-21:
// 1. Initialize a 2-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Scale in the standalone PCLQ (pc-a) before its rolling update
// 4. Change the specification of pc-a, pc-b and pc-c
// 5. Verify the update goes through successfully
func Test_RU21_RollingUpdateWithPodCliqueScaleInBeforeUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 2-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 2)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload1.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 20, 0)

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("3. Scale in the standalone PCLQ (pc-a) before its rolling update")
	if err := scalePodCliqueInPCS(ctx, dynamicClient, workloadNamespace, "workload1", "pc-a", 1); err != nil {
		t.Fatalf("Failed to scale PodClique pc-a: %v", err)
	}

	// Wait for pods to be deleted
	err = pollForCondition(ctx, defaultPollTimeout, defaultPollInterval, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) <= 18, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be scaled in: %v", err)
	}

	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for remaining pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ctx, clientset, workloadNamespace, workloadLabelSelector); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop()

	if err := tracker.WaitForReady(); err != nil {
		t.Fatalf("Failed to wait for tracker to be ready: %v", err)
	}

	logger.Info("4. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		err = triggerPodCliqueRollingUpdate(ctx, dynamicClient, workloadNamespace, "workload1", cliqueName)
		if err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	logger.Info("5. Verify the update goes through successfully")
	if err := waitForRollingUpdateComplete(ctx, dynamicClient, workloadNamespace, "workload1", 2, 2*time.Minute); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tracker.Stop()

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 18 {
		t.Fatalf("Expected 18 pods after PodClique scale-in, got %d", len(pods.Items))
	}

	logger.Info("🎉 Rolling Update with PodClique scale-in before update test (RU-21) completed successfully!")
}

// podEvent represents a pod lifecycle event during rolling update
type podEvent struct {
	Type      watch.EventType
	Pod       *corev1.Pod
	Timestamp time.Time
}

// rollingUpdateTracker tracks pod events during rolling update
type rollingUpdateTracker struct {
	events  []podEvent
	mu      sync.Mutex
	watcher watch.Interface
	cancel  context.CancelFunc
	ready   chan struct{}
}

// newRollingUpdateTracker creates a new rolling update tracker
func newRollingUpdateTracker() *rollingUpdateTracker {
	return &rollingUpdateTracker{
		ready: make(chan struct{}),
	}
}

// Start begins watching pod events
func (t *rollingUpdateTracker) Start(ctx context.Context, clientset kubernetes.Interface, namespace, labelSelector string) error {
	watcherCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	watcher, err := clientset.CoreV1().Pods(namespace).Watch(watcherCtx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		cancel()
		return err
	}
	t.watcher = watcher

	go func() {
		// Signal that the watcher goroutine is running and ready to receive events
		close(t.ready)

		for {
			select {
			case <-watcherCtx.Done():
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					return
				}
				if pod, ok := event.Object.(*corev1.Pod); ok {
					t.recordEvent(event.Type, pod)
				}
			}
		}
	}()

	return nil
}

// Stop stops the watcher and cleans up resources
func (t *rollingUpdateTracker) Stop() {
	if t.watcher != nil {
		t.watcher.Stop()
	}
	if t.cancel != nil {
		t.cancel()
	}
}

// WaitForReady waits for the watcher goroutine to be running and ready to receive events
// This prevents a race condition where early pod events could be missed
func (t *rollingUpdateTracker) WaitForReady() error {
	select {
	case <-t.ready:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for watcher to be ready")
	}
}

func (t *rollingUpdateTracker) recordEvent(eventType watch.EventType, pod *corev1.Pod) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, podEvent{
		Type:      eventType,
		Pod:       pod.DeepCopy(),
		Timestamp: time.Now(),
	})
}

func (t *rollingUpdateTracker) getEvents() []podEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]podEvent{}, t.events...)
}

// triggerPodCliqueRollingUpdate triggers a rolling update by adding/updating an environment variable in a PodClique
func triggerPodCliqueRollingUpdate(ctx context.Context, dynamicClient dynamic.Interface, namespace, pcsName string, cliqueName string) error {
	pcsGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquesets"}

	// Use current timestamp to ensure the value changes
	updateValue := fmt.Sprintf("%d", time.Now().Unix())

	// Retry on conflict errors (optimistic concurrency control)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the unstructured PodCliqueSet
		unstructuredPCS, err := dynamicClient.Resource(pcsGVR).Namespace(namespace).Get(ctx, pcsName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get PodCliqueSet: %w", err)
		}

		// Convert unstructured to typed PodCliqueSet
		var pcs grovev1alpha1.PodCliqueSet
		err = convertUnstructuredToTyped(unstructuredPCS.Object, &pcs)
		if err != nil {
			return fmt.Errorf("failed to convert to PodCliqueSet: %w", err)
		}

		// Find and update the appropriate clique
		found := false
		for i, clique := range pcs.Spec.Template.Cliques {
			if clique.Name == cliqueName {
				// Update the first container's environment variable
				if len(clique.Spec.PodSpec.Containers) > 0 {
					container := &pcs.Spec.Template.Cliques[i].Spec.PodSpec.Containers[0]

					// Check if ROLLING_UPDATE_TRIGGER env var exists
					envVarFound := false
					for j := range container.Env {
						if container.Env[j].Name == "ROLLING_UPDATE_TRIGGER" {
							container.Env[j].Value = updateValue
							envVarFound = true
							break
						}
					}

					// If not found, add new env var
					if !envVarFound {
						container.Env = append(container.Env, corev1.EnvVar{
							Name:  "ROLLING_UPDATE_TRIGGER",
							Value: updateValue,
						})
					}
					found = true
				}
				break
			}
		}

		if !found {
			return fmt.Errorf("clique %s not found in PodCliqueSet %s", cliqueName, pcsName)
		}

		// Convert back to unstructured
		updatedUnstructured, err := convertTypedToUnstructured(&pcs)
		if err != nil {
			return fmt.Errorf("failed to convert to unstructured: %w", err)
		}

		// Update the resource - will return conflict error if resource was modified
		_, err = dynamicClient.Resource(pcsGVR).Namespace(namespace).Update(ctx, updatedUnstructured, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		return nil
	})
}

// convertUnstructuredToTyped converts an unstructured map to a typed object
func convertUnstructuredToTyped(unstructured map[string]interface{}, typed interface{}) error {
	data, err := json.Marshal(unstructured)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, typed)
}

// convertTypedToUnstructured converts a typed object to an unstructured object
func convertTypedToUnstructured(typed interface{}) (*unstructured.Unstructured, error) {
	data, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}
	var unstructuredMap map[string]interface{}
	err = json.Unmarshal(data, &unstructuredMap)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: unstructuredMap}, nil
}

// waitForRollingUpdateComplete waits for rolling update to complete by checking UpdatedReplicas
func waitForRollingUpdateComplete(ctx context.Context, dynamicClient dynamic.Interface, namespace, pcsName string, expectedReplicas int32, timeout time.Duration) error {
	pcsGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquesets"}

	return pollForCondition(ctx, timeout, 5*time.Second, func() (bool, error) {
		unstructuredPCS, err := dynamicClient.Resource(pcsGVR).Namespace(namespace).Get(ctx, pcsName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		var pcs grovev1alpha1.PodCliqueSet
		err = convertUnstructuredToTyped(unstructuredPCS.Object, &pcs)
		if err != nil {
			return false, err
		}

		// Check if rolling update is complete:
		// - UpdatedReplicas should match expected
		// - RollingUpdateProgress should exist with UpdateEndedAt set (not nil)
		if pcs.Status.UpdatedReplicas == expectedReplicas &&
			pcs.Status.RollingUpdateProgress != nil &&
			pcs.Status.RollingUpdateProgress.UpdateEndedAt != nil {
			return true, nil
		}

		return false, nil
	})
}

// getPodIdentifier returns a stable identifier for a pod based on its logical position in the workload.
// This identifier remains the same when a pod is replaced during rolling updates.
//
// Stability is guaranteed because the hostname is set by the operator based on the pod's logical
// position in the PodClique (pclqName-podIndex), not the pod's generated name. During a rolling
// update:
//  1. The old pod is deleted (e.g., hostname: "workload1-pc-a-0-0")
//  2. The PodClique remains unchanged (same name and replica count)
//  3. The new pod is created to fill the same logical position
//  4. The new pod receives the same hostname (e.g., "workload1-pc-a-0-0")
//
// Only the pod name changes (due to GenerateName), while the hostname represents the pod's
// logical position in the workload hierarchy and remains constant.
func getPodIdentifier(t *testing.T, pod *corev1.Pod) string {
	t.Helper()

	// Use hostname as the stable identifier (set by configurePodHostname in pod.go)
	// Format: {pclqName}-{podIndex} (e.g., "workload1-pc-a-0-0")
	if pod.Spec.Hostname == "" {
		t.Fatalf("Pod %s does not have hostname set, cannot determine stable identifier", pod.Name)
	}

	return pod.Spec.Hostname
}

// verifyOnePodDeletedAtATime verifies only one pod globally is being deleted at a time
// during rolling updates.
func verifyOnePodDeletedAtATime(t *testing.T, events []podEvent) {
	t.Helper()

	// Track which pods are currently in the process of being deleted/replaced.
	// The deletingPods map uses pod identifiers as keys and deletion timestamps as values.
	deletingPods := make(map[string]time.Time)
	maxConcurrentDeletions := 0

	for _, event := range events {
		podID := getPodIdentifier(t, event.Pod)

		switch event.Type {
		case watch.Deleted:
			// When a pod is deleted, we add it to the deletingPods map using its unique identifier.
			// This marks the start of that pod's deletion/replacement cycle.
			deletingPods[podID] = event.Timestamp
		case watch.Added:
			// When a pod with the same identifier is added, we remove it from the deletingPods map.
			// This marks the completion of the replacement - the new pod is ready and the old one
			// is no longer being replaced.
			delete(deletingPods, podID)
		}

		// After processing each event, we track the maximum number of concurrent deletions observed.
		if len(deletingPods) > maxConcurrentDeletions {
			maxConcurrentDeletions = len(deletingPods)
		}
	}

	// Assert that at most 1 pod was being deleted/replaced at any point in time,
	// which ensures the rolling update respects the MaxUnavailable=1 constraint at the global level.
	if maxConcurrentDeletions > 1 {
		t.Fatalf("Expected at most 1 pod being deleted at a time, but found %d concurrent deletions", maxConcurrentDeletions)
	}
}

// verifyOnePodDeletedAtATimePerPodclique verifies only one pod per Podclique is being deleted at a time
// during rolling updates.
//
// This function processes a sequence of pod events and tracks the number of individual pods
// that are in a "deleting" state within each Podclique at any given time.
func verifyOnePodDeletedAtATimePerPodclique(t *testing.T, events []podEvent) {
	t.Helper()

	// Track which pods are currently in the process of being deleted/replaced, grouped by Podclique.
	// The deletingPods map is keyed by Podclique name, with values being maps of pod identifiers to deletion timestamps.
	// Map structure: podcliqueName -> podID -> deletion timestamp
	deletingPods := make(map[string]map[string]time.Time)
	maxConcurrentDeletionsPerPodclique := make(map[string]int)

	for _, event := range events {
		// All pods should have labels - if nil, that's a bug
		if event.Pod.Labels == nil {
			t.Fatalf("Pod %s has no labels, which indicates a bug in pod creation", event.Pod.Name)
		}

		podcliqueName, ok := event.Pod.Labels["grove.io/podclique"]
		if !ok {
			t.Fatalf("Pod %s does not have grove.io/podclique label", event.Pod.Name)
		}

		podID := getPodIdentifier(t, event.Pod)

		switch event.Type {
		case watch.Deleted:
			// When a pod is deleted, we add it to the deletingPods map (grouped by Podclique) using its unique identifier.
			// This marks the start of that pod's deletion/replacement cycle.
			if deletingPods[podcliqueName] == nil {
				deletingPods[podcliqueName] = make(map[string]time.Time)
			}
			deletingPods[podcliqueName][podID] = event.Timestamp
		case watch.Added:
			// When a pod with the same identifier is added, we remove it from the deletingPods map.
			// This marks the completion of the replacement - the new pod is ready and the old one
			// is no longer being replaced.
			if deletingPods[podcliqueName] != nil {
				delete(deletingPods[podcliqueName], podID)
				if len(deletingPods[podcliqueName]) == 0 {
					delete(deletingPods, podcliqueName)
				}
			}
		}

		// After processing each event, we track the maximum number of concurrent deletions observed per Podclique.
		for podclique, pods := range deletingPods {
			if len(pods) > maxConcurrentDeletionsPerPodclique[podclique] {
				maxConcurrentDeletionsPerPodclique[podclique] = len(pods)
			}
		}
	}

	// Assert that at most 1 pod per Podclique was in the deletion-to-creation window at any point in time,
	// which ensures the rolling update deletes pods sequentially within each Podclique.
	for podclique, maxDeletions := range maxConcurrentDeletionsPerPodclique {
		if maxDeletions > 1 {
			t.Fatalf("Expected at most 1 pod being deleted at a time in Podclique %s, but found %d concurrent deletions", podclique, maxDeletions)
		}
	}
}

// verifySinglePCSReplicaUpdatedFirst verifies that a single PCS replica is fully updated before another replica begins.
//
// This function ensures strict replica-level ordering during PodCliqueSet rolling updates:
// - Once a replica starts updating (any pod is deleted), NO other replica can start updating
// - The updating replica must complete ALL pod updates across ALL its PodCliques before another replica begins
// - A replica is considered "complete" only when all pods across all PodCliques have been deleted and re-added
//
// Background:
// A PodCliqueSet (PCS) can have multiple replicas, where each replica contains multiple PodCliques.
// For example, with replicas=2, you might have:
//   - Replica 0: pc-a (2 pods), pc-b (1 pod), pc-c (3 pods) = 6 total pods
//   - Replica 1: pc-a (2 pods), pc-b (1 pod), pc-c (3 pods) = 6 total pods
//
// During a rolling update, the system should update Replica 0 completely (all 6 pods across all PodCliques)
// before starting any updates to Replica 1.
//
// Implementation Details:
// - Tracks pods being updated per PodClique within each replica
// - Uses stable pod identifiers (hostname) to correlate pod deletions with additions
// - Only considers Deleted/Added events; Modified events are ignored as they don't affect ordering
//
// Note: This verification is most meaningful when PCS has replicas > 1. For replicas=1, it provides
// minimal validation since there's only one replica to update.
func verifySinglePCSReplicaUpdatedFirst(t *testing.T, events []podEvent) {
	t.Helper()

	// Track the state of pods being updated within each replica's PodCliques
	// Structure: replicaIndex -> podcliqueName -> set of pod identifiers being updated
	// Example: map[0]map["pc-a"]map["workload1-0-pc-a-0"] = true
	replicaPodCliqueState := make(map[int]map[string]map[string]bool)

	// Track which replica is currently being updated (-1 means no replica is actively updating)
	currentlyUpdatingReplica := -1

	for _, event := range events {
		// Extract replica index from pod labels (defaults to 0 if not present)
		replicaIdx := 0
		if val, ok := event.Pod.Labels["grove.io/podcliqueset-replica-index"]; ok {
			replicaIdx, _ = strconv.Atoi(val)
		}

		// Extract PodClique name - skip pods without this label
		podcliqueName, ok := event.Pod.Labels["grove.io/podclique"]
		if !ok {
			continue
		}

		// Get stable pod identifier (hostname) that persists across pod replacements
		podID := getPodIdentifier(t, event.Pod)

		// Process only Deleted/Added events to verify replica ordering
		// Modified events (status changes) don't affect the ordering of replica updates
		switch event.Type {
		case watch.Deleted:
			// Initialize nested maps if this is the first pod deletion for this replica/PodClique
			if replicaPodCliqueState[replicaIdx] == nil {
				replicaPodCliqueState[replicaIdx] = make(map[string]map[string]bool)
			}
			if replicaPodCliqueState[replicaIdx][podcliqueName] == nil {
				replicaPodCliqueState[replicaIdx][podcliqueName] = make(map[string]bool)
			}

			// Determine if we're starting a new replica update or continuing the current one
			switch {
			case currentlyUpdatingReplica == -1:
				// No replica is currently updating - this is the first deletion, start tracking this replica
				currentlyUpdatingReplica = replicaIdx
				logger.Debugf("Started updating replica %d (first pod deleted: %s from PodClique: %s)",
					replicaIdx, podID, podcliqueName)

			case currentlyUpdatingReplica == replicaIdx:
				// Continuing update of the same replica - this is expected
				logger.Debugf("Continuing update of replica %d (pod deleted: %s from PodClique: %s)",
					replicaIdx, podID, podcliqueName)

			default:
				// ERROR: A different replica started updating before the current one finished
				currentPodCount := getTotalPodCount(replicaPodCliqueState[currentlyUpdatingReplica])
				podCliqueNames := getPodCliqueNames(replicaPodCliqueState[currentlyUpdatingReplica])
				t.Fatalf("Expected replica %d to finish updating before replica %d starts. "+
					"Replica %d still has %d pod(s) being updated across PodCliques: %v, "+
					"but replica %d pod %s (PodClique: %s) was deleted.",
					currentlyUpdatingReplica, replicaIdx,
					currentlyUpdatingReplica, currentPodCount, podCliqueNames,
					replicaIdx, podID, podcliqueName)
			}

			// Track this pod as being updated in this replica's PodClique
			replicaPodCliqueState[replicaIdx][podcliqueName][podID] = true
			logger.Debugf("Pod deleted in replica %d, PodClique %s: %s (total pods updating in this PodClique: %d)",
				replicaIdx, podcliqueName, podID, len(replicaPodCliqueState[replicaIdx][podcliqueName]))

		case watch.Added:
			// Pod has been re-created and added back - remove it from tracking
			switch {
			case currentlyUpdatingReplica == -1:
				// No replica is currently updating - this could be initial pod creation or pods added
				// after the rolling update has fully completed
				logger.Debugf("Pod added to replica %d (no active rolling update): %s", replicaIdx, podID)

			case currentlyUpdatingReplica == replicaIdx:
				// Pod added back to the replica we're currently tracking - this is the expected path
				if replicaPodCliqueState[replicaIdx] != nil &&
					replicaPodCliqueState[replicaIdx][podcliqueName] != nil {
					delete(replicaPodCliqueState[replicaIdx][podcliqueName], podID)
					logger.Debugf("Pod added back in replica %d, PodClique %s: %s (remaining pods in this PodClique: %d)",
						replicaIdx, podcliqueName, podID, len(replicaPodCliqueState[replicaIdx][podcliqueName]))

					// Clean up empty PodClique map
					if len(replicaPodCliqueState[replicaIdx][podcliqueName]) == 0 {
						delete(replicaPodCliqueState[replicaIdx], podcliqueName)
						logger.Debugf("PodClique %s in replica %d completed all pod updates", podcliqueName, replicaIdx)
					}

					// Check if this replica is now fully done (all PodCliques have finished updating)
					if len(replicaPodCliqueState[replicaIdx]) == 0 {
						logger.Debugf("Completed updating replica %d - all PodCliques finished", replicaIdx)
						delete(replicaPodCliqueState, replicaIdx)
						currentlyUpdatingReplica = -1
					}
				}

			default:
				// ERROR: Pod added to a different replica while another replica is still updating
				currentPodCount := getTotalPodCount(replicaPodCliqueState[currentlyUpdatingReplica])
				podCliqueNames := getPodCliqueNames(replicaPodCliqueState[currentlyUpdatingReplica])
				t.Fatalf("Replica ordering violation: replica %d is still being updated (%d pod(s) pending across PodCliques: %v), "+
					"but a pod was added to replica %d (PodClique: %s, pod: %s). "+
					"Expected replica %d to complete before replica %d starts.",
					currentlyUpdatingReplica, currentPodCount, podCliqueNames,
					replicaIdx, podcliqueName, podID,
					currentlyUpdatingReplica, replicaIdx)
			}
		}
	}

	// If we still have a replica being updated at the end, it may indicate incomplete tracking
	// or that the test captured events while the rolling update was still in progress.
	// This is logged for debugging but doesn't fail the test.
	if currentlyUpdatingReplica != -1 {
		remainingPodCount := getTotalPodCount(replicaPodCliqueState[currentlyUpdatingReplica])
		if remainingPodCount > 0 {
			logger.Debugf("Note: Replica %d has %d pod(s) still in updating state at end of event stream. "+
				"This may be expected if the rolling update was still in progress when tracking stopped.",
				currentlyUpdatingReplica, remainingPodCount)
		}
	}
}

// getTotalPodCount returns the total number of pods being updated across all PodCliques in a replica.
//
// Parameters:
//   - podCliqueMap: map of PodClique name to set of pod identifiers
//
// Returns:
//   - Total count of pods across all PodCliques
//
// Example: If pc-a has 2 pods updating and pc-b has 1 pod updating, returns 3
func getTotalPodCount(podCliqueMap map[string]map[string]bool) int {
	total := 0
	for _, pods := range podCliqueMap {
		total += len(pods)
	}
	return total
}

// getPodCliqueNames returns a sorted list of PodClique names from the given map.
//
// This is used for generating readable error messages that show which PodCliques
// still have pending updates.
//
// Parameters:
//   - podCliqueMap: map of PodClique name to set of pod identifiers
//
// Returns:
//   - Sorted slice of PodClique names
//
// Example: Returns ["pc-a", "pc-b", "pc-c"] (sorted alphabetically)
func getPodCliqueNames(podCliqueMap map[string]map[string]bool) []string {
	names := make([]string, 0, len(podCliqueMap))
	for name := range podCliqueMap {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// verifyOnePCSGReplicaDeletedAtATime verifies only one PCSG replica globally is deleted at a time
// during rolling updates.
//
// SCOPE: GLOBAL across all PCSGs
// This is the strictest constraint - only ONE PCSG replica in the entire system can be rolling at any time.
// If you have multiple PCSGs (e.g., sg-x and sg-y), only one replica from any of them can be rolling.
//
// Compare with verifyOnePCSGReplicaDeletedAtATimePerPCSG which allows concurrent rolling across different PCSGs.
func verifyOnePCSGReplicaDeletedAtATime(t *testing.T, events []podEvent) {
	t.Helper()

	// Track how many pods per replica are currently being rolled.
	// Key format: "<pcsg-name>-<replica-index>" -> count of pods being rolled
	// A replica is considered "rolling" if it has any pods in the rolling state (count > 0).
	// The map size represents how many replicas are currently rolling globally.
	deletingReplicaPodCount := make(map[string]int)
	maxConcurrentDeletions := 0

	for _, event := range events {
		// All pods should have labels - if nil, that's a bug
		if event.Pod.Labels == nil {
			t.Fatalf("Pod %s has no labels, which indicates a bug in pod creation", event.Pod.Name)
		}

		// Only process pods that belong to a PCSG (not all pods have this label)
		replicaID, hasReplicaLabel := event.Pod.Labels["grove.io/podcliquescalinggroup-replica-index"]
		if !hasReplicaLabel {
			continue
		}

		pcsgName, ok := event.Pod.Labels["grove.io/podcliquescalinggroup"]
		if !ok {
			t.Fatalf("Pod %s has PCSG replica index but no PCSG name label", event.Pod.Name)
		}

		// Create a composite key to uniquely identify this PCSG replica globally
		replicaKey := fmt.Sprintf("%s-%s", pcsgName, replicaID)

		switch event.Type {
		case watch.Deleted:
			// When a pod is deleted, increment the count for this replica.
			// This marks a pod in that replica's deletion/replacement cycle.
			deletingReplicaPodCount[replicaKey]++
			logger.Debugf("Pod %s deleted, replica %s now has %d pods rolling", event.Pod.Name, replicaKey, deletingReplicaPodCount[replicaKey])
		case watch.Added:
			// When a pod is added back, decrement the count for this replica.
			// When count reaches 0, the replacement is complete - all pods in that replica are ready.
			if count, exists := deletingReplicaPodCount[replicaKey]; exists && count > 0 {
				deletingReplicaPodCount[replicaKey]--
				logger.Debugf("Pod %s added, replica %s now has %d pods rolling", event.Pod.Name, replicaKey, deletingReplicaPodCount[replicaKey])
				if deletingReplicaPodCount[replicaKey] == 0 {
					delete(deletingReplicaPodCount, replicaKey)
				}
			}
		}

		// Track the maximum number of replicas being replaced concurrently.
		if len(deletingReplicaPodCount) > maxConcurrentDeletions {
			maxConcurrentDeletions = len(deletingReplicaPodCount)
		}
	}

	// Assert that at most 1 replica was being deleted/replaced at any point in time.
	// This ensures the rolling update respects the MaxUnavailable=1 constraint at the global level.
	if maxConcurrentDeletions > 1 {
		t.Fatalf("Expected at most 1 PCSG replica being deleted at a time, but found %d concurrent deletions", maxConcurrentDeletions)
	}
}

// verifyOnePCSGReplicaDeletedAtATimePerPCSG verifies only one replica per PCSG is deleted at a time
// during rolling updates.
//
// SCOPE: PER-PCSG (allows concurrent rolling across different PCSGs)
// This constraint allows multiple PCSGs to roll simultaneously, but within each PCSG, only one replica
// can be rolling at a time. For example, if you have sg-x and sg-y:
//   - ✅ ALLOWED: sg-x replica 0 AND sg-y replica 0 rolling simultaneously
//   - ❌ NOT ALLOWED: sg-x replica 0 AND sg-x replica 1 rolling simultaneously
//
// Compare with verifyOnePCSGReplicaDeletedAtATime which enforces a global constraint (stricter).
func verifyOnePCSGReplicaDeletedAtATimePerPCSG(t *testing.T, events []podEvent) {
	t.Helper()

	// Track how many pods per replica are currently being rolled, grouped by PCSG.
	// Map structure: pcsgName -> replicaIndex -> count of pods being rolled
	// A replica is considered "rolling" if it has any pods in the rolling state (count > 0).
	// The inner map size represents the number of replicas currently rolling within each PCSG.
	deletingReplicaPodCount := make(map[string]map[string]int)
	maxConcurrentDeletionsPerPCSG := make(map[string]int)

	for _, event := range events {
		// All pods should have labels - if nil, that's a bug
		if event.Pod.Labels == nil {
			t.Fatalf("Pod %s has no labels, which indicates a bug in pod creation", event.Pod.Name)
		}

		// Only process pods that belong to a PCSG (not all pods have this label)
		replicaID, hasReplicaLabel := event.Pod.Labels["grove.io/podcliquescalinggroup-replica-index"]
		if !hasReplicaLabel {
			continue
		}

		pcsgName, ok := event.Pod.Labels["grove.io/podcliquescalinggroup"]
		if !ok {
			t.Fatalf("Pod %s has PCSG replica index but no PCSG name label", event.Pod.Name)
		}

		switch event.Type {
		case watch.Deleted:
			// When a pod is deleted, increment the count for this replica in this PCSG.
			// This marks a pod in that replica's deletion/replacement cycle.
			if deletingReplicaPodCount[pcsgName] == nil {
				deletingReplicaPodCount[pcsgName] = make(map[string]int)
			}
			deletingReplicaPodCount[pcsgName][replicaID]++
			logger.Debugf("Pod %s deleted, replica %s in PCSG %s now has %d pods rolling", event.Pod.Name, replicaID, pcsgName, deletingReplicaPodCount[pcsgName][replicaID])
		case watch.Added:
			// When a pod is added back, decrement the count for this replica.
			// When count reaches 0, the replacement is complete - all pods in that replica are ready.
			if deletingReplicaPodCount[pcsgName] != nil {
				if count, exists := deletingReplicaPodCount[pcsgName][replicaID]; exists && count > 0 {
					deletingReplicaPodCount[pcsgName][replicaID]--
					logger.Debugf("Pod %s added, replica %s in PCSG %s now has %d pods rolling", event.Pod.Name, replicaID, pcsgName, deletingReplicaPodCount[pcsgName][replicaID])
					if deletingReplicaPodCount[pcsgName][replicaID] == 0 {
						delete(deletingReplicaPodCount[pcsgName], replicaID)
						if len(deletingReplicaPodCount[pcsgName]) == 0 {
							delete(deletingReplicaPodCount, pcsgName)
						}
					}
				}
			}
		}

		// Track the maximum number of replicas being replaced concurrently per PCSG.
		for pcsg, replicas := range deletingReplicaPodCount {
			if len(replicas) > maxConcurrentDeletionsPerPCSG[pcsg] {
				maxConcurrentDeletionsPerPCSG[pcsg] = len(replicas)
			}
		}
	}

	// Assert that at most 1 replica per PCSG was being deleted/replaced at any point in time.
	// This ensures the rolling update respects the MaxUnavailable=1 constraint at the PCSG level.
	for pcsg, maxDeletions := range maxConcurrentDeletionsPerPCSG {
		if maxDeletions > 1 {
			t.Fatalf("Expected at most 1 replica being deleted at a time in PCSG %s, but found %d concurrent deletions", pcsg, maxDeletions)
		}
	}
}

// scalePodCliqueInPCS scales a specific PodClique within a PodCliqueSet by updating its replicas field
func scalePodCliqueInPCS(ctx context.Context, dynamicClient dynamic.Interface, namespace, pcsName, cliqueName string, replicas int32) error {
	pcsGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquesets"}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		unstructuredPCS, err := dynamicClient.Resource(pcsGVR).Namespace(namespace).Get(ctx, pcsName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get PodCliqueSet: %w", err)
		}

		var pcs grovev1alpha1.PodCliqueSet
		err = convertUnstructuredToTyped(unstructuredPCS.Object, &pcs)
		if err != nil {
			return fmt.Errorf("failed to convert to PodCliqueSet: %w", err)
		}

		found := false
		for i, clique := range pcs.Spec.Template.Cliques {
			if clique.Name == cliqueName {
				pcs.Spec.Template.Cliques[i].Spec.Replicas = replicas
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("clique %s not found in PodCliqueSet %s", cliqueName, pcsName)
		}

		updatedUnstructured, err := convertTypedToUnstructured(&pcs)
		if err != nil {
			return fmt.Errorf("failed to convert to unstructured: %w", err)
		}

		_, err = dynamicClient.Resource(pcsGVR).Namespace(namespace).Update(ctx, updatedUnstructured, metav1.UpdateOptions{})
		return err
	})
}

// verifyPodSpecHasEnvVar verifies that pods with a specific label have the expected environment variable value
func verifyPodSpecHasEnvVar(t *testing.T, pods []corev1.Pod, envVarName, expectedValue string) {
	t.Helper()

	for _, pod := range pods {
		if len(pod.Spec.Containers) == 0 {
			t.Fatalf("Pod %s has no containers", pod.Name)
		}

		found := false
		for _, env := range pod.Spec.Containers[0].Env {
			if env.Name == envVarName {
				if env.Value != expectedValue {
					t.Fatalf("Pod %s has incorrect env var %s value: expected %s, got %s",
						pod.Name, envVarName, expectedValue, env.Value)
				}
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("Pod %s does not have env var %s", pod.Name, envVarName)
		}
	}
}
