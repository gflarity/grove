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

// Package tests contains end-to-end tests for the Grove operator.
//
// These tests are disabled by default due to the 'e2e' build tag above.
// To run these tests, use:
//
//	go test -tags=e2e ./e2e_testing/tests/...
//
// Without the -tags=e2e flag, these tests will be skipped entirely.
package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NVIDIA/grove/operator/e2e_testing/utils"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// TerminationDelay is a mirror of the value in the workload YAMLs.
	TerminationDelay = 10 * time.Second
)

// Test_GT1_GangTerminationFullReplicasPCSOwned tests gang-termination behavior when a PCS-owned PodClique is breached
// Scenario GT-1:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Wait for pods to get scheduled and become ready
// 4. Cordon node and then delete 1 ready pod from PCS-owned podclique pcs-0-pc-a
// 5. Wait for TerminationDelay seconds
// 6. Verify that all pods in the workload get terminated
func Test_GT1_GangTerminationFullReplicasPCSOwned(t *testing.T) {
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
		t.Errorf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10
	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Errorf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("3. Wait for pods to get scheduled and become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, 30*time.Second, logger); err != nil {
		t.Errorf("Failed to wait for pods to be ready: %v", err)
	}

	// Re-fetch pods after they're ready to get updated NodeName assignments
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list pods after ready: %v", err)
	}

	// Verify pods are distributed across distinct nodes as per the workload YAML and Node resource constraints
	assertPodsOnDistinctNodes(t, pods.Items)

	logger.Info("4. Cordon node and then delete 1 ready pod from PCS-owned podclique pcs-0-pc-a")
	// Find a pod from pcs-0-pc-a podclique (PCS-owned)
	var targetPod *v1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		// PCS-owned PodClique pods have labels like: grove.io/podclique=workload1-0-pc-a
		if podCliqueLabel, exists := pod.Labels["grove.io/podclique"]; exists && podCliqueLabel == "workload1-0-pc-a" {
			if pod.Status.Phase == v1.PodRunning && isPodReady(pod) {
				targetPod = pod
				break
			}
		}
	}

	if targetPod == nil {
		t.Fatalf("Failed to find a ready pod from PCS-owned podclique pcs-0-pc-a")
	}

	// Cordon the node where the target pod is running
	if err := utils.CordonNode(ctx, clientset, targetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", targetPod.Spec.NodeName, err)
	}

	// Capture pod UIDs before gang termination
	originalPodUIDs := capturePodUIDs(pods)

	// Delete the target pod
	logger.Debugf("Deleting pod %s from node %s", targetPod.Name, targetPod.Spec.NodeName)
	if err := clientset.CoreV1().Pods(workloadNamespace).Delete(ctx, targetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", targetPod.Name, err)
	}

	// DEBUGGING: Check PodClique status immediately after deletion
	logger.Debug("Checking PodClique status after pod deletion...")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-pc-a", logger)

	logger.Infof("5. Wait for TerminationDelay (%v) seconds", TerminationDelay)

	// DEBUGGING: Poll PodClique status during the delay
	go func() {
		for i := 0; i < 3; i++ {
			time.Sleep(3 * time.Second)
			logger.Debugf("Checking status at T+%ds...", (i+1)*3)
			debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-pc-a", logger)
			debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload1", logger)
		}
	}()

	time.Sleep(TerminationDelay)

	// DEBUGGING: Final status check before verification
	logger.Debug("Final status check after TerminationDelay...")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-pc-a", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload1", logger)

	// DEBUGGING: Dump operator logs for troubleshooting
	logger.Debug("Fetching operator controller logs...")
	dumpOperatorLogs(ctx, clientset, logger, "grove-operator", 50)

	logger.Info("6. Verify that all pods in the workload get gang-terminated and recreated")
	// After gang-termination, pods should be recreated with new UIDs and be in Pending state
	// Extended timeout to account for pod graceful termination and recreation
	pollCount := 0
	err = pollForCondition(ctx, 10*time.Second, 1*time.Second, func() (bool, error) {
		pollCount++
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}

		// Verify none of the original pod UIDs exist (all were deleted and recreated)
		currentUIDs := make(map[string]bool)
		pendingCount := 0
		runningCount := 0
		terminatingCount := 0
		oldPodsRemaining := 0
		for _, pod := range pods.Items {
			currentUIDs[string(pod.UID)] = true
			switch pod.Status.Phase {
			case v1.PodPending:
				pendingCount++
			case v1.PodRunning:
				runningCount++
			}
			if pod.DeletionTimestamp != nil {
				terminatingCount++
			}
		}

		// Check that no original UIDs exist in current pods (all recreated)
		for _, originalUID := range originalPodUIDs {
			if currentUIDs[originalUID] {
				oldPodsRemaining++
			}
		}

		success := len(pods.Items) == expectedPods && oldPodsRemaining == 0 && pendingCount == expectedPods
		status := "✅"
		if !success {
			status = "❌"
		}
		logger.Debugf("%s [Poll %d] total=%d/%d, pending=%d/%d, running=%d, terminating=%d, old=%d",
			status, pollCount, len(pods.Items), expectedPods, pendingCount, expectedPods, runningCount, terminatingCount, oldPodsRemaining)

		return success, nil
	})
	if err != nil {
		// Add detailed diagnostics on failure
		pods, listErr := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if listErr == nil {
			logger.Errorf("Gang-termination verification failed. Current state: total_pods=%d, expected=%d", len(pods.Items), expectedPods)
			for _, pod := range pods.Items {
				logger.Debugf("Pod %s: phase=%s, uid=%s", pod.Name, pod.Status.Phase, pod.UID)
			}
		}
		t.Errorf("Failed to verify gang-termination and recreation: %v", err)
	} else {
		logger.Info("🎉 Gang-termination with full-replicas PCS-owned test (GT-1) completed successfully!")
	}
}

// Test_GT2_GangTerminationFullReplicasPCSGOwned tests gang-termination behavior when a PCSG-owned PodClique is breached
// Scenario GT-2:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Wait for pods to get scheduled and become ready
// 4. Cordon node and then delete 1 ready pod from PCSG-owned podclique pcs-0-sg-x-0-pc-c
// 5. Wait for TerminationDelay seconds
// 6. Verify that all pods in the workload get terminated
func Test_GT2_GangTerminationFullReplicasPCSGOwned(t *testing.T) {
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
		t.Errorf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10
	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Errorf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("3. Wait for pods to get scheduled and become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, 10*time.Minute, logger); err != nil {
		t.Errorf("Failed to wait for pods to be ready: %v", err)
	}

	// Re-fetch pods after they're ready to get updated NodeName assignments
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list pods after ready: %v", err)
	}

	// Verify pods are distributed across distinct nodes as per the workload YAML and Node resource constraints
	assertPodsOnDistinctNodes(t, pods.Items)

	logger.Info("4. Cordon node and then delete 1 ready pod from PCSG-owned podclique pcs-0-sg-x-0-pc-c")
	// Find a pod from pcs-0-sg-x-0-pc-c podclique (PCSG-owned)
	var targetPod *v1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		// PCSG-owned PodClique pods have labels like: grove.io/podclique=workload1-0-sg-x-0-pc-c
		if podCliqueLabel, exists := pod.Labels["grove.io/podclique"]; exists && podCliqueLabel == "workload1-0-sg-x-0-pc-c" {
			if pod.Status.Phase == v1.PodRunning && isPodReady(pod) {
				targetPod = pod
				break
			}
		}
	}

	if targetPod == nil {
		t.Fatalf("Failed to find a ready pod from PCSG-owned podclique pcs-0-sg-x-0-pc-c")
	}

	// Cordon the node where the target pod is running
	if err := utils.CordonNode(ctx, clientset, targetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", targetPod.Spec.NodeName, err)
	}

	// Capture pod UIDs before gang termination
	originalPodUIDs := capturePodUIDs(pods)

	// Delete the target pod
	logger.Debugf("Deleting pod %s from node %s", targetPod.Name, targetPod.Spec.NodeName)
	if err := clientset.CoreV1().Pods(workloadNamespace).Delete(ctx, targetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", targetPod.Name, err)
	}

	// DEBUGGING: Check PodClique and PCSG status after deletion
	logger.Debug("Checking PodClique and PCSG status after pod deletion...")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-sg-x-0-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-sg-x", logger)

	// DEBUGGING: Check actual pod counts
	logger.Debug("Checking pod states after deletion...")
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	logger.Infof("5. Wait for TerminationDelay (%v) seconds", TerminationDelay)

	// DEBUGGING: Poll status during the delay with more granular checks
	startTime := time.Now()
	tickerDone := make(chan bool)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(startTime)
				logger.Debugf("Status check at T+%.1fs (%.0f%% of TerminationDelay)...", elapsed.Seconds(), (elapsed.Seconds()/TerminationDelay.Seconds())*100)
				debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-sg-x-0-pc-c", logger)
				debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-sg-x", logger)
				debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload1", logger)
				debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
			case <-tickerDone:
				return
			}
		}
	}()

	time.Sleep(TerminationDelay)
	close(tickerDone)
	time.Sleep(100 * time.Millisecond) // Give goroutine time to finish

	// DEBUGGING: Final status check
	logger.Debug("Final status check after TerminationDelay...")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-sg-x-0-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload1-0-sg-x", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload1", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	// DEBUGGING: Check all related PodCliques
	logger.Debug("Checking ALL PodCliques in the workload...")
	debugAllPodCliques(ctx, t, restConfig, workloadNamespace, "workload1", logger)

	// DEBUGGING: Check K8s events for gang termination indicators
	logger.Debug("Checking Kubernetes events...")
	debugEvents(ctx, t, clientset, workloadNamespace, logger)

	// DEBUGGING: Dump operator logs
	logger.Debug("Fetching operator controller logs...")
	dumpOperatorLogs(ctx, clientset, logger, "grove-operator", 100)

	logger.Info("6. Verify that all pods in the workload get gang-terminated and recreated")

	// Print expected behavior for debugging
	logger.Debug("📋 EXPECTED BEHAVIOR:")
	logger.Debug("  1. PodClique workload1-0-sg-x-0-pc-c should have MinAvailableBreached=True")
	logger.Debug("     (because 1 pod deleted: 2 remaining < minAvailable:3)")
	logger.Debug("  2. PCSG workload1-0-sg-x should have MinAvailableBreached=True")
	logger.Debug("     (because replica 0 is breached: 1 available < minAvailable:2)")
	logger.Debug("  3. PCS controller should detect breach and wait for TerminationDelay (10s)")
	logger.Debug("  4. After TerminationDelay, ALL 10 pods should be deleted and recreated")
	logger.Debug("  5. New pods should have different UIDs and be in Pending state")

	// After gang-termination, pods should be recreated with new UIDs and be in Pending state
	pollCount := 0
	err = pollForCondition(ctx, 10*time.Second, 1*time.Second, func() (bool, error) {
		pollCount++
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}

		// Verify none of the original pod UIDs exist (all were deleted and recreated)
		currentUIDs := make(map[string]bool)
		pendingCount := 0
		runningCount := 0
		terminatingCount := 0
		oldPodsRemaining := 0
		for _, pod := range pods.Items {
			currentUIDs[string(pod.UID)] = true
			switch pod.Status.Phase {
			case v1.PodPending:
				pendingCount++
			case v1.PodRunning:
				runningCount++
			}
			if pod.DeletionTimestamp != nil {
				terminatingCount++
			}
		}

		// Check that no original UIDs exist in current pods (all recreated)
		for _, originalUID := range originalPodUIDs {
			if currentUIDs[originalUID] {
				oldPodsRemaining++
			}
		}

		success := len(pods.Items) == expectedPods && oldPodsRemaining == 0 && pendingCount == expectedPods
		status := "✅"
		if !success {
			status = "❌"
		}
		logger.Debugf("%s [Poll %d] total=%d/%d, pending=%d/%d, running=%d, terminating=%d, old=%d",
			status, pollCount, len(pods.Items), expectedPods, pendingCount, expectedPods, runningCount, terminatingCount, oldPodsRemaining)

		return success, nil
	})
	if err != nil {
		// Add detailed diagnostics on failure
		pods, listErr := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if listErr == nil {
			logger.Errorf("Gang-termination verification failed. Current state: total_pods=%d, expected=%d", len(pods.Items), expectedPods)
			for _, pod := range pods.Items {
				logger.Debugf("Pod %s: phase=%s, uid=%s", pod.Name, pod.Status.Phase, pod.UID)
			}
		}
		t.Errorf("Failed to verify gang-termination and recreation: %v", err)
	} else {
		logger.Info("🎉 Gang-termination with full-replicas PCSG-owned test (GT-2) completed successfully!")
	}
}

// Test_GT3_GangTerminationMinReplicasPCSOwned tests gang-termination behavior with min-replicas when a PCS-owned PodClique is breached
// Scenario GT-3:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Wait for pods to get scheduled and become ready
// 4. Cordon node and then delete 1 pod from PCS-owned podclique pcs-0-pc-a
// 5. Wait for TerminationDelay seconds
// 6. Verify that workload pods do not get gang-terminated
// 7. Cordon node and then delete 1 ready pod from PCS-owned podclique pcs-0-pc-a
// 8. Wait for TerminationDelay seconds
// 9. Verify that all pods in the workload get terminated
func Test_GT3_GangTerminationMinReplicasPCSOwned(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	logger.Info("2. Deploy workload WL2, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload2.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload2"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Errorf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10
	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Errorf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("3. Wait for pods to get scheduled and become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, 10*time.Minute, logger); err != nil {
		t.Errorf("Failed to wait for pods to be ready: %v", err)
	}

	// Re-fetch pods after they're ready to get updated NodeName assignments
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list pods after ready: %v", err)
	}

	// Verify pods are distributed across distinct nodes as per the workload YAML and Node resource constraints
	assertPodsOnDistinctNodes(t, pods.Items)

	logger.Info("4. Cordon node and then delete 1 pod from PCS-owned podclique pcs-0-pc-a")
	// Find the first pod from workload2-0-pc-a podclique (PCS-owned)
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// DEBUGGING: Check initial pod distribution
	logger.Debug("Initial pod distribution before first deletion:")
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-pc-a", logger)

	var firstTargetPod *v1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		// PCS-owned PodClique pods have labels like: grove.io/podclique=workload2-0-pc-a
		if podCliqueLabel, exists := pod.Labels["grove.io/podclique"]; exists && podCliqueLabel == "workload2-0-pc-a" {
			if pod.Status.Phase == v1.PodRunning && isPodReady(pod) {
				firstTargetPod = pod
				break
			}
		}
	}

	if firstTargetPod == nil {
		t.Fatalf("Failed to find a ready pod from PCS-owned podclique workload2-0-pc-a")
	}

	// Cordon the node where the first target pod is running
	if err := utils.CordonNode(ctx, clientset, firstTargetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", firstTargetPod.Spec.NodeName, err)
	}

	// Delete the first target pod
	logger.Infof("🗑️  Deleting FIRST pod %s from node %s (podclique: workload2-0-pc-a)", firstTargetPod.Name, firstTargetPod.Spec.NodeName)
	logger.Debugf("📋 Expected after first deletion: minAvailable should NOT be breached (1 pod remains >= minAvailable:1)")
	if err := clientset.CoreV1().Pods(workloadNamespace).Delete(ctx, firstTargetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", firstTargetPod.Name, err)
	}

	// DEBUGGING: Check status immediately after first deletion
	logger.Debug("Checking status immediately after FIRST pod deletion...")
	time.Sleep(2 * time.Second) // Give operator time to reconcile
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-pc-a", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	logger.Infof("5. Wait for 2x TerminationDelay (%v) to ensure no gang-termination occurs", 2*TerminationDelay)

	// DEBUGGING: Monitor status during the wait period
	startTime := time.Now()
	tickerDone := make(chan bool)
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(startTime)
				logger.Debugf("Status check at T+%.1fs (%.0f%% of 2xTerminationDelay)...", elapsed.Seconds(), (elapsed.Seconds()/(2*TerminationDelay).Seconds())*100)
				debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-pc-a", logger)
				debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
				debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
			case <-tickerDone:
				return
			}
		}
	}()

	time.Sleep(2 * TerminationDelay)
	close(tickerDone)
	time.Sleep(100 * time.Millisecond) // Give goroutine time to finish

	// DEBUGGING: Final check after 2x TerminationDelay
	logger.Debug("Final status check after 2x TerminationDelay (should be no gang termination)...")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-pc-a", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	logger.Info("6. Verify that workload pods do not get gang-terminated")
	verifyNoGangTermination(ctx, t, clientset, workloadNamespace, workloadLabelSelector, 3)

	logger.Info("7. Cordon node and then delete 1 ready pod from PCS-owned podclique pcs-0-pc-a")

	// DEBUGGING: Check state before second deletion
	logger.Debug("State before SECOND pod deletion:")
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-pc-a", logger)

	// Find another ready pod from workload2-0-pc-a
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	var secondTargetPod *v1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if podCliqueLabel, exists := pod.Labels["grove.io/podclique"]; exists && podCliqueLabel == "workload2-0-pc-a" {
			if pod.Status.Phase == v1.PodRunning && isPodReady(pod) && pod.Name != firstTargetPod.Name {
				secondTargetPod = pod
				break
			}
		}
	}

	if secondTargetPod == nil {
		t.Fatalf("Failed to find a second ready pod from PCS-owned podclique workload2-0-pc-a")
	}

	// Cordon the node where the second target pod is running
	if err := utils.CordonNode(ctx, clientset, secondTargetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", secondTargetPod.Spec.NodeName, err)
	}

	// Capture pod UIDs before gang termination (after first deletion)
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}
	originalPodUIDs := capturePodUIDs(pods)

	// Delete the second target pod
	logger.Infof("🗑️  Deleting SECOND pod %s from node %s (podclique: workload2-0-pc-a)", secondTargetPod.Name, secondTargetPod.Spec.NodeName)
	logger.Debugf("📋 Expected after second deletion: minAvailable SHOULD be breached (0 pods remaining < minAvailable:1)")
	if err := clientset.CoreV1().Pods(workloadNamespace).Delete(ctx, secondTargetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", secondTargetPod.Name, err)
	}

	// DEBUGGING: Check status immediately after second deletion
	logger.Debug("Checking status immediately after SECOND pod deletion...")
	time.Sleep(2 * time.Second) // Give operator time to reconcile
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-pc-a", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	logger.Infof("8. Wait for TerminationDelay (%v) seconds", TerminationDelay)

	// DEBUGGING: Monitor status during TerminationDelay with detailed checks
	startTime2 := time.Now()
	tickerDone2 := make(chan bool)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(startTime2)
				logger.Debugf("Status check at T+%.1fs (%.0f%% of TerminationDelay)...", elapsed.Seconds(), (elapsed.Seconds()/TerminationDelay.Seconds())*100)
				debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-pc-a", logger)
				debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
				debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
			case <-tickerDone2:
				return
			}
		}
	}()

	time.Sleep(TerminationDelay)
	close(tickerDone2)
	time.Sleep(100 * time.Millisecond) // Give goroutine time to finish

	// DEBUGGING: Final comprehensive status check
	logger.Debug("Final status check after TerminationDelay (gang termination should have occurred)...")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-pc-a", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugAllPodCliques(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	// DEBUGGING: Check events
	logger.Debug("Checking Kubernetes events...")
	debugEvents(ctx, t, clientset, workloadNamespace, logger)

	// DEBUGGING: Dump operator logs
	logger.Debug("Fetching operator controller logs...")
	dumpOperatorLogs(ctx, clientset, logger, "grove-operator", 100)

	logger.Info("9. Verify that all pods in the workload get gang-terminated and recreated")

	// DEBUGGING: Print expected behavior
	logger.Debug("📋 EXPECTED BEHAVIOR:")
	logger.Debug("  1. PodClique workload2-0-pc-a should have MinAvailableBreached=True")
	logger.Debug("     (because 2 pods deleted: 0 remaining < minAvailable:1)")
	logger.Debug("  2. PCS controller should detect breach and wait for TerminationDelay (10s)")
	logger.Debug("  3. After TerminationDelay, ALL 10 pods should be deleted and recreated")
	logger.Debug("  4. New pods should have different UIDs and be in Pending state")

	// After breaching min-replicas, gang-termination should occur
	pollCount := 0
	err = pollForCondition(ctx, 10*time.Second, 1*time.Second, func() (bool, error) {
		pollCount++
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}

		// Verify none of the original pod UIDs exist (all were deleted and recreated)
		currentUIDs := make(map[string]bool)
		pendingCount := 0
		runningCount := 0
		terminatingCount := 0
		oldPodsRemaining := 0
		for _, pod := range pods.Items {
			currentUIDs[string(pod.UID)] = true
			switch pod.Status.Phase {
			case v1.PodPending:
				pendingCount++
			case v1.PodRunning:
				runningCount++
			}
			if pod.DeletionTimestamp != nil {
				terminatingCount++
			}
		}

		// Check that no original UIDs exist in current pods (all recreated)
		for _, originalUID := range originalPodUIDs {
			if currentUIDs[originalUID] {
				oldPodsRemaining++
			}
		}

		success := len(pods.Items) == expectedPods && oldPodsRemaining == 0 && pendingCount == expectedPods
		status := "✅"
		if !success {
			status = "❌"
		}
		logger.Debugf("%s [Poll %d] total=%d/%d, pending=%d/%d, running=%d, terminating=%d, old=%d",
			status, pollCount, len(pods.Items), expectedPods, pendingCount, expectedPods, runningCount, terminatingCount, oldPodsRemaining)

		return success, nil
	})
	if err != nil {
		// Add detailed diagnostics on failure
		pods, listErr := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if listErr == nil {
			logger.Errorf("Gang-termination verification failed. Current state: total_pods=%d, expected=%d", len(pods.Items), expectedPods)
			for _, pod := range pods.Items {
				logger.Debugf("Pod %s: phase=%s, uid=%s", pod.Name, pod.Status.Phase, pod.UID)
			}
		}
		t.Errorf("Failed to verify gang-termination and recreation: %v", err)
	} else {
		logger.Info("🎉 Gang-termination with min-replicas PCS-owned test (GT-3) completed successfully!")
	}
}

// Test_GT4_GangTerminationMinReplicasPCSGOwned tests gang-termination behavior with min-replicas when a PCSG-owned PodClique is breached
// Scenario GT-4:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Wait for pods to get scheduled and become ready
// 4. Cordon node and then delete 1 ready pod from PCSG-owned podclique pcs-0-sg-x-0-pc-c
// 5. Wait for TerminationDelay seconds
// 6. Verify that workload pods do not get gang-terminated
// 7. Cordon nodes and then delete 2 remaining ready pods from PCSG-owned podclique pcs-0-sg-x-0-pc-c
// 8. Wait for TerminationDelay seconds
// 9. Verify that both podcliques on PCSG pcs-0-sg-x-0 are recreated but workload is not gang-terminated
// 10. Cordon node and then delete 1 ready pod from PCSG-owned podclique pcs-0-sg-x-1-pc-c
// 11. Wait for TerminationDelay seconds
// 12. Verify that workload pods do not get gang-terminated
// 13. Cordon nodes and then delete 2 remaining ready pods from PCSG-owned podclique pcs-0-sg-x-1-pc-c
// 14. Wait for TerminationDelay seconds
// 15. Verify that all pods in the workload get terminated
func Test_GT4_GangTerminationMinReplicasPCSGOwned(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	// Track nodes cordoned by this test so we can uncordon them later
	cordonedNodes := make(map[string]bool)

	logger.Info("2. Deploy workload WL2, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload2.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload2"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Errorf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10
	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Errorf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("3. Wait for pods to get scheduled and become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, 10*time.Minute, logger); err != nil {
		t.Errorf("Failed to wait for pods to be ready: %v", err)
	}

	// Re-fetch pods after they're ready to get updated NodeName assignments
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list pods after ready: %v", err)
	}

	// Verify pods are distributed across distinct nodes (not required for GT-4 but good to verify)
	// Note: GT-4 doesn't have the assertion but we can add it for consistency
	// assertPodsOnDistinctNodes(t, pods.Items)

	logger.Info("4. Cordon node and then delete 1 ready pod from PCSG-owned podclique pcs-0-sg-x-0-pc-c")

	// DEBUGGING: Check initial state before first deletion
	logger.Debug("State before first pod deletion from sg-x-0-pc-c:")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// Find and delete first pod from workload2-0-sg-x-0-pc-c
	logger.Info("🗑️  Deleting FIRST pod from workload2-0-sg-x-0-pc-c")
	logger.Debug("📋 EXPECTED: After 1st deletion, 2 pods remain >= minAvailable:1 → NO breach")
	firstPodToDelete := findAndDeletePodFromPodClique(ctx, t, clientset, pods, "workload2-0-sg-x-0-pc-c", workloadNamespace, "first", cordonedNodes)

	// DEBUGGING: Check status immediately after deletion
	logger.Debug("State immediately after first pod deletion:")
	time.Sleep(2 * time.Second) // Give operator time to reconcile
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	logger.Infof("5. Wait for 2x TerminationDelay (%v) to ensure no gang-termination occurs", 2*TerminationDelay)

	// DEBUGGING: Monitor status during wait period
	startTime := time.Now()
	tickerDone := make(chan bool)
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(startTime)
				logger.Debugf("Status at T+%.1fs (%.0f%% of 2xTerminationDelay)...", elapsed.Seconds(), (elapsed.Seconds()/(2*TerminationDelay).Seconds())*100)
				debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
				debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
				debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
			case <-tickerDone:
				return
			}
		}
	}()

	time.Sleep(2 * TerminationDelay)
	close(tickerDone)
	time.Sleep(100 * time.Millisecond)

	logger.Debug("Final state after 2x TerminationDelay (should be NO gang termination):")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	logger.Info("6. Verify that workload pods do not get gang-terminated")
	verifyNoGangTermination(ctx, t, clientset, workloadNamespace, workloadLabelSelector, 3)

	logger.Info("7. Cordon nodes and then delete 2 remaining ready pods from PCSG-owned podclique pcs-0-sg-x-0-pc-c")
	logger.Info("📋 EXPECTED: After all 3 pods deleted, 0 remaining < minAvailable:1 → PCSG replica 0 breach")
	logger.Info("🔍 DEBUG: State before deleting remaining pods:")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// Find and delete remaining pods from workload2-0-sg-x-0-pc-c
	deletedPods := []string{firstPodToDelete}
	for i := 0; i < 2; i++ {
		logger.Infof("🗑️  Deleting pod %d/2 from sg-x-0-pc-c...", i+2)
		podName := findAndDeletePodFromPodCliqueExcluding(ctx, t, clientset, pods, "workload2-0-sg-x-0-pc-c", workloadNamespace, deletedPods, fmt.Sprintf("pod %d", i+2), cordonedNodes)
		deletedPods = append(deletedPods, podName)

		// DEBUGGING: Check status after each deletion
		time.Sleep(2 * time.Second)
		logger.Debugf("Status after deleting pod %d:", i+2)
		debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
		debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)

		// Refresh pod list
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			t.Errorf("Failed to list workload pods: %v", err)
		}
	}

	logger.Infof("8. Wait for 2x TerminationDelay (%v) seconds", 2*TerminationDelay)
	logger.Info("📋 EXPECTED BEHAVIOR:")
	logger.Info("  1. PodClique workload2-0-sg-x-0-pc-c: MinAvailableBreached = True (0 pods < minAvailable:1)")
	logger.Info("  2. PCSG replica 0: breached")
	logger.Info("  3. PCSG workload2-0-sg-x: MinAvailableBreached = False (1 available replica >= minAvailable:1)")
	logger.Info("  4. Local PCSG replica termination (both pc-b and pc-c deleted)")
	logger.Info("  5. NO full PCS gang termination")

	// DEBUGGING: Monitor during wait period
	startTime2 := time.Now()
	tickerDone2 := make(chan bool)
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(startTime2)
				logger.Infof("🔍 DEBUG: Status at T+%.1fs (%.0f%% of 2xTerminationDelay)...", elapsed.Seconds(), (elapsed.Seconds()/(2*TerminationDelay).Seconds())*100)
				debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
				debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-b", logger)
				debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
				debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
			case <-tickerDone2:
				return
			}
		}
	}()

	time.Sleep(2 * TerminationDelay)
	close(tickerDone2)
	time.Sleep(100 * time.Millisecond)

	logger.Info("🔍 DEBUG: Final state after 2x TerminationDelay:")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-b", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugAllPodCliques(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
	debugEvents(ctx, t, clientset, workloadNamespace, logger)
	dumpOperatorLogs(ctx, clientset, logger, "grove-operator", 100)

	logger.Info("9. Verify that both podcliques on PCSG pcs-0-sg-x-0 (pcs-0-sg-x-0-pc-b and pcs-0-sg-x-0-pc-c) are recreated but workload is not gang-terminated")
	// After deleting all pods from pc-c, the PCSG should recreate both pc-b and pc-c podcliques
	// But gang-termination should not occur because we haven't breached min-replicas at PCSG level
	verifyNoGangTermination(ctx, t, clientset, workloadNamespace, workloadLabelSelector, 3)

	// CRITICAL: DO NOT uncordon nodes yet!
	// We need to keep replica 0 unschedulable so that when we delete replica 1 in step 13,
	// BOTH replicas will be breached simultaneously, triggering PCSG-level gang termination.
	// If we uncordon now, replica 0 will recover before replica 1 is breached, and we'll only
	// get local replica termination instead of full gang termination.
	logger.Infof("📌 Keeping %d nodes cordoned to prevent replica 0 from recovering", len(cordonedNodes))
	logger.Info("   This ensures both PCSG replicas will be breached simultaneously in step 13")

	logger.Info("10. Cordon node and then delete 1 ready pod from PCSG-owned podclique pcs-0-sg-x-1-pc-c")
	logger.Info("🔍 DEBUG: State before first pod deletion from sg-x-1-pc-c:")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-1-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// Find and delete first pod from workload2-0-sg-x-1-pc-c
	logger.Info("🗑️  Deleting FIRST pod from workload2-0-sg-x-1-pc-c")
	logger.Info("📋 EXPECTED: After 1st deletion, 2 pods remain >= minAvailable:1 → NO breach")
	firstPodSgx1 := findAndDeletePodFromPodClique(ctx, t, clientset, pods, "workload2-0-sg-x-1-pc-c", workloadNamespace, "first from sg-x-1", cordonedNodes)

	logger.Info("🔍 DEBUG: State immediately after first pod deletion from sg-x-1:")
	time.Sleep(2 * time.Second)
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-1-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	logger.Infof("11. Wait for 2x TerminationDelay (%v) to ensure no gang-termination occurs", 2*TerminationDelay)
	time.Sleep(2 * TerminationDelay)

	logger.Info("🔍 DEBUG: State after 2x TerminationDelay:")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-1-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	logger.Info("12. Verify that workload pods do not get gang-terminated")
	verifyNoGangTermination(ctx, t, clientset, workloadNamespace, workloadLabelSelector, 3)

	logger.Info("13. Cordon nodes and then delete 2 remaining ready pods from PCSG-owned podclique pcs-0-sg-x-1-pc-c")
	logger.Info("📋 CRITICAL: After all 3 pods deleted from replica 1, BOTH PCSG replicas will be breached:")
	logger.Info("   - Replica 0: Still unschedulable (nodes cordoned in step 7, never uncordoned)")
	logger.Info("   - Replica 1: Just deleted (0 ready pods)")
	logger.Info("   → 0 available replicas < minAvailable:1 → FULL gang termination expected")
	logger.Info("🔍 DEBUG: State before deleting remaining pods from sg-x-1:")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-1-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// Find and delete remaining pods from workload2-0-sg-x-1-pc-c
	deletedPodsSgx1 := []string{firstPodSgx1}
	for i := 0; i < 2; i++ {
		logger.Infof("🗑️  Deleting pod %d/2 from sg-x-1-pc-c...", i+2)
		podName := findAndDeletePodFromPodCliqueExcluding(ctx, t, clientset, pods, "workload2-0-sg-x-1-pc-c", workloadNamespace, deletedPodsSgx1, fmt.Sprintf("pod %d from sg-x-1", i+2), cordonedNodes)
		deletedPodsSgx1 = append(deletedPodsSgx1, podName)

		// DEBUGGING: Check status after each deletion
		time.Sleep(2 * time.Second)
		logger.Debugf("Status after deleting pod %d from sg-x-1:", i+2)
		debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-1-pc-c", logger)
		debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)

		// Refresh pod list
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			t.Errorf("Failed to list workload pods: %v", err)
		}
	}

	// Capture pod UIDs before final gang termination
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}
	originalPodUIDs := capturePodUIDs(pods)
	logger.Infof("📸 Captured %d pod UIDs before gang termination", len(originalPodUIDs))

	logger.Infof("14. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	logger.Info("📋 EXPECTED BEHAVIOR:")
	logger.Info("  1. PodClique workload2-0-sg-x-1-pc-c: MinAvailableBreached = True (0 pods < minAvailable:1)")
	logger.Info("  2. PCSG replica 1: breached")
	logger.Info("  3. PodClique workload2-0-sg-x-0-pc-c: Still unschedulable (nodes cordoned in step 7)")
	logger.Info("  4. PCSG replica 0: NOT available (pods unschedulable)")
	logger.Info("  5. PCSG workload2-0-sg-x: MinAvailableBreached = True (0 available replicas < minAvailable:1)")
	logger.Info("  6. PCS controller detects PCSG breach → FULL gang termination")
	logger.Info("  7. ALL 10 pods deleted and recreated")

	// DEBUGGING: Monitor during termination delay
	startTime3 := time.Now()
	tickerDone3 := make(chan bool)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(startTime3)
				logger.Debugf("Status at T+%.1fs (%.0f%% of TerminationDelay)...", elapsed.Seconds(), (elapsed.Seconds()/TerminationDelay.Seconds())*100)
				debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
				debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-1-pc-c", logger)
				debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
				debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
				debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
			case <-tickerDone3:
				return
			}
		}
	}()

	time.Sleep(TerminationDelay)
	close(tickerDone3)
	time.Sleep(100 * time.Millisecond)

	logger.Info("🔍 DEBUG: FINAL comprehensive state after TerminationDelay:")
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-0-pc-c", logger)
	debugPodCliqueStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x-1-pc-c", logger)
	debugPodCliqueScalingGroupStatus(ctx, t, restConfig, workloadNamespace, "workload2-0-sg-x", logger)
	debugPodCliqueSetStatus(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugAllPodCliques(ctx, t, restConfig, workloadNamespace, "workload2", logger)
	debugPodStates(ctx, t, clientset, workloadNamespace, workloadLabelSelector, logger)
	debugEvents(ctx, t, clientset, workloadNamespace, logger)
	dumpOperatorLogs(ctx, clientset, logger, "grove-operator", 200)

	logger.Info("15. Uncordon nodes to allow gang-terminated pods to reschedule")
	// Now that gang termination has occurred, uncordon all nodes so the recreated pods can schedule
	logger.Infof("🔓 Uncordoning %d nodes to allow gang-terminated pods to schedule...", len(cordonedNodes))
	if err := uncordonNodes(ctx, clientset, cordonedNodes); err != nil {
		t.Errorf("Failed to uncordon nodes: %v", err)
	}
	logger.Info("⏳ Waiting 5s for pods to start scheduling...")
	time.Sleep(5 * time.Second)

	logger.Info("16. Verify that all pods in the workload get gang-terminated and recreated")
	// After breaching min-replicas at PCSG level, gang-termination should occur
	pollCount := 0
	err = pollForCondition(ctx, 30*time.Second, 1*time.Second, func() (bool, error) {
		pollCount++
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}

		// Verify none of the original pod UIDs exist (all were deleted and recreated)
		currentUIDs := make(map[string]bool)
		pendingCount := 0
		runningCount := 0
		terminatingCount := 0
		oldPodsRemaining := 0
		for _, pod := range pods.Items {
			currentUIDs[string(pod.UID)] = true
			switch pod.Status.Phase {
			case v1.PodPending:
				pendingCount++
			case v1.PodRunning:
				runningCount++
			}
			if pod.DeletionTimestamp != nil {
				terminatingCount++
			}
		}

		// Check that no original UIDs exist in current pods (all recreated)
		for _, originalUID := range originalPodUIDs {
			if currentUIDs[originalUID] {
				oldPodsRemaining++
			}
		}

		// Success: All pods recreated (new UIDs), total count correct
		// We don't require all pending because after uncordoning in step 15, pods will start Running
		success := len(pods.Items) == expectedPods && oldPodsRemaining == 0
		status := "✅"
		if !success {
			status = "❌"
		}
		logger.Debugf("%s [Poll %d] total=%d/%d, pending=%d, running=%d, terminating=%d, old=%d",
			status, pollCount, len(pods.Items), expectedPods, pendingCount, runningCount, terminatingCount, oldPodsRemaining)

		return success, nil
	})
	if err != nil {
		// Add detailed diagnostics on failure
		pods, listErr := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if listErr == nil {
			logger.Errorf("Gang-termination verification failed. Current state: total_pods=%d, expected=%d", len(pods.Items), expectedPods)
			for _, pod := range pods.Items {
				logger.Debugf("Pod %s: phase=%s, uid=%s", pod.Name, pod.Status.Phase, pod.UID)
			}
		}
		t.Errorf("Failed to verify gang-termination and recreation: %v", err)
		return // Don't try to uncordon again on failure
	}

	logger.Info("🎉 Gang-termination with min-replicas PCSG-owned test (GT-4) completed successfully!")

}

// capturePodUIDs captures the UIDs of all pods in the list
func capturePodUIDs(pods *v1.PodList) map[string]string {
	uidMap := make(map[string]string)
	for _, pod := range pods.Items {
		uidMap[pod.Name] = string(pod.UID)
	}
	return uidMap
}

// isPodReady checks if a pod is ready
func isPodReady(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

// findAndDeletePodFromPodClique finds a ready pod from the specified podclique and deletes it
func findAndDeletePodFromPodClique(ctx context.Context, t *testing.T, clientset kubernetes.Interface, pods *v1.PodList, podCliqueName, namespace, description string, cordonedNodes map[string]bool) string {
	t.Helper()

	var targetPod *v1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if podCliqueLabel, exists := pod.Labels["grove.io/podclique"]; exists && podCliqueLabel == podCliqueName {
			if pod.Status.Phase == v1.PodRunning && isPodReady(pod) {
				targetPod = pod
				break
			}
		}
	}

	if targetPod == nil {
		t.Fatalf("Failed to find %s ready pod from podclique %s", description, podCliqueName)
		return ""
	}

	// Cordon the node and track it
	if err := utils.CordonNode(ctx, clientset, targetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", targetPod.Spec.NodeName, err)
	}
	if cordonedNodes != nil {
		cordonedNodes[targetPod.Spec.NodeName] = true
	}

	// Delete the pod
	logger.Debugf("Deleting %s pod %s from node %s (podclique: %s)", description, targetPod.Name, targetPod.Spec.NodeName, podCliqueName)
	if err := clientset.CoreV1().Pods(namespace).Delete(ctx, targetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", targetPod.Name, err)
	}

	return targetPod.Name
}

// findAndDeletePodFromPodCliqueExcluding finds a ready pod from the specified podclique (excluding certain pods) and deletes it
func findAndDeletePodFromPodCliqueExcluding(ctx context.Context, t *testing.T, clientset kubernetes.Interface, pods *v1.PodList, podCliqueName, namespace string, excludePods []string, description string, cordonedNodes map[string]bool) string {
	t.Helper()

	var targetPod *v1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if podCliqueLabel, exists := pod.Labels["grove.io/podclique"]; exists && podCliqueLabel == podCliqueName {
			if pod.Status.Phase == v1.PodRunning && isPodReady(pod) {
				// Check if this pod should be excluded
				shouldExclude := false
				for _, excludeName := range excludePods {
					if pod.Name == excludeName {
						shouldExclude = true
						break
					}
				}
				if !shouldExclude {
					targetPod = pod
					break
				}
			}
		}
	}

	if targetPod == nil {
		t.Fatalf("Failed to find %s ready pod from podclique %s", description, podCliqueName)
		return ""
	}

	// Cordon the node and track it
	if err := utils.CordonNode(ctx, clientset, targetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", targetPod.Spec.NodeName, err)
	}
	if cordonedNodes != nil {
		cordonedNodes[targetPod.Spec.NodeName] = true
	}

	// Delete the pod
	logger.Debugf("Deleting %s pod %s from node %s (podclique: %s)", description, targetPod.Name, targetPod.Spec.NodeName, podCliqueName)
	if err := clientset.CoreV1().Pods(namespace).Delete(ctx, targetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", targetPod.Name, err)
	}

	return targetPod.Name
}

// verifyNoGangTermination verifies that gang-termination has not occurred
func verifyNoGangTermination(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace, labelSelector string, minExpectedRunning int) {
	t.Helper()

	pollCount := 0
	err := pollForCondition(ctx, 10*time.Second, 1*time.Second, func() (bool, error) {
		pollCount++
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return false, err
		}

		runningCount := 0
		pendingCount := 0
		terminatingCount := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningCount++
			case v1.PodPending:
				pendingCount++
			}
			if pod.DeletionTimestamp != nil {
				terminatingCount++
			}
		}

		runningOrPendingCount := runningCount + pendingCount
		success := runningOrPendingCount >= minExpectedRunning
		status := "✅"
		if !success {
			status = "❌"
		}
		logger.Debugf("%s [Poll %d] running=%d, pending=%d, terminating=%d, total=%d (min_expected=%d)",
			status, pollCount, runningCount, pendingCount, terminatingCount, len(pods.Items), minExpectedRunning)

		return success, nil
	})

	if err != nil {
		t.Errorf("Failed to verify no gang-termination: %v", err)
	}
}

// debugPodCliqueStatus dumps detailed PodClique status for debugging
func debugPodCliqueStatus(ctx context.Context, t *testing.T, restConfig *rest.Config, namespace, pclqName string, logger *logrus.Logger) {
	t.Helper()

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		logger.Errorf("Failed to create dynamic client: %v", err)
		return
	}

	pclqGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}

	pclq, err := dynamicClient.Resource(pclqGVR).Namespace(namespace).Get(ctx, pclqName, metav1.GetOptions{})
	if err != nil {
		logger.Errorf("Failed to get PodClique %s: %v", pclqName, err)
		return
	}

	spec, specFound, _ := unstructured.NestedMap(pclq.Object, "spec")
	status, statusFound, _ := unstructured.NestedMap(pclq.Object, "status")
	if !statusFound {
		logger.Warnf("PodClique %s has no status", pclqName)
		return
	}

	// Extract spec values
	var minAvailable interface{}
	if specFound {
		minAvailable = spec["minAvailable"]
	}

	// Extract status values
	replicas := status["replicas"]
	readyReplicas := status["readyReplicas"]
	scheduledReplicas := status["scheduledReplicas"]

	conditions, _, _ := unstructured.NestedSlice(status, "conditions")
	logger.Debugf("📊 PodClique %s Status:", pclqName)
	logger.Debugf("  Spec.MinAvailable: %v", minAvailable)
	logger.Debugf("  Status.Replicas: %v (non-terminating pods)", replicas)
	logger.Debugf("  Status.ReadyReplicas: %v", readyReplicas)
	logger.Debugf("  Status.ScheduledReplicas: %v", scheduledReplicas)

	// Show the breach logic calculation
	if minAvailable != nil && scheduledReplicas != nil && readyReplicas != nil {
		minAvailInt64, _ := minAvailable.(int64)
		scheduledInt64, _ := scheduledReplicas.(int64)
		readyInt64, _ := readyReplicas.(int64)

		logger.Debugf("  📐 Breach Calculation:")
		logger.Debugf("    scheduledReplicas (%d) < minAvailable (%d)? %v", scheduledInt64, minAvailInt64, scheduledInt64 < minAvailInt64)
		logger.Debugf("    readyReplicas (%d) == 0? %v", readyInt64, readyInt64 == 0)
		logger.Debugf("    → Should MinAvailableBreached be True? %v", scheduledInt64 >= minAvailInt64 && scheduledInt64 < minAvailInt64)
	}

	logger.Debugf("  Conditions (%d):", len(conditions))
	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType := condMap["type"]
		condStatus := condMap["status"]
		condReason := condMap["reason"]
		condMessage := condMap["message"]
		lastTransition := condMap["lastTransitionTime"]

		emoji := "ℹ️"
		if condType == "MinAvailableBreached" {
			if condStatus == "True" {
				emoji = "🔴"
			} else {
				emoji = "🟢"
			}
		}
		logger.Debugf("    %s %s: %s (reason: %s, lastTransition: %s)", emoji, condType, condStatus, condReason, lastTransition)
		if condMessage != nil && condMessage != "" {
			logger.Debugf("       Message: %s", condMessage)
		}
	}
}

// debugPodCliqueSetStatus dumps detailed PodCliqueSet status for debugging
func debugPodCliqueSetStatus(ctx context.Context, t *testing.T, restConfig *rest.Config, namespace, pcsName string, logger *logrus.Logger) {
	t.Helper()

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		logger.Errorf("Failed to create dynamic client: %v", err)
		return
	}

	pcsGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquesets",
	}

	pcs, err := dynamicClient.Resource(pcsGVR).Namespace(namespace).Get(ctx, pcsName, metav1.GetOptions{})
	if err != nil {
		logger.Errorf("Failed to get PodCliqueSet %s: %v", pcsName, err)
		return
	}

	status, found, _ := unstructured.NestedMap(pcs.Object, "status")
	if !found {
		logger.Warnf("PodCliqueSet %s has no status", pcsName)
		return
	}

	logger.Debugf("📊 PodCliqueSet %s Status:", pcsName)
	logger.Debugf("  Replicas: %v", status["replicas"])
	logger.Debugf("  AvailableReplicas: %v", status["availableReplicas"])
	logger.Debugf("  UpdatedReplicas: %v", status["updatedReplicas"])
	logger.Debugf("  ObservedGeneration: %v", status["observedGeneration"])

	// Check for last errors
	lastErrors, _, _ := unstructured.NestedSlice(status, "lastErrors")
	if len(lastErrors) > 0 {
		logger.Warnf("  LastErrors (%d):", len(lastErrors))
		for i, err := range lastErrors {
			errMap, ok := err.(map[string]interface{})
			if !ok {
				continue
			}
			logger.Warnf("    [%d] Code: %v, Description: %v", i, errMap["code"], errMap["description"])
		}
	}
}

// debugPodCliqueScalingGroupStatus dumps detailed PCSG status for debugging
func debugPodCliqueScalingGroupStatus(ctx context.Context, t *testing.T, restConfig *rest.Config, namespace, pcsgName string, logger *logrus.Logger) {
	t.Helper()

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		logger.Errorf("Failed to create dynamic client: %v", err)
		return
	}

	pcsgGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquescalinggroups",
	}

	pcsg, err := dynamicClient.Resource(pcsgGVR).Namespace(namespace).Get(ctx, pcsgName, metav1.GetOptions{})
	if err != nil {
		logger.Errorf("Failed to get PodCliqueScalingGroup %s: %v", pcsgName, err)
		return
	}

	status, found, _ := unstructured.NestedMap(pcsg.Object, "status")
	if !found {
		logger.Warnf("PodCliqueScalingGroup %s has no status", pcsgName)
		return
	}

	conditions, _, _ := unstructured.NestedSlice(status, "conditions")
	logger.Debugf("📊 PodCliqueScalingGroup %s Status:", pcsgName)
	logger.Debugf("  Replicas: %v", status["replicas"])
	logger.Debugf("  AvailableReplicas: %v", status["availableReplicas"])
	logger.Debugf("  ScheduledReplicas: %v", status["scheduledReplicas"])

	logger.Debugf("  Conditions (%d):", len(conditions))
	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType := condMap["type"]
		condStatus := condMap["status"]
		condReason := condMap["reason"]
		lastTransition := condMap["lastTransitionTime"]

		emoji := "ℹ️"
		if condType == "MinAvailableBreached" {
			if condStatus == "True" {
				emoji = "🔴"
			} else {
				emoji = "🟢"
			}
		}
		logger.Debugf("    %s %s: %s (reason: %s, lastTransition: %s)", emoji, condType, condStatus, condReason, lastTransition)
	}
}

// dumpOperatorLogs fetches and logs recent operator controller logs
func dumpOperatorLogs(ctx context.Context, clientset kubernetes.Interface, logger *logrus.Logger, operatorNamespace string, tailLines int64) {
	// Find operator pods
	pods, err := clientset.CoreV1().Pods(operatorNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=grove-operator",
	})
	if err != nil {
		logger.Errorf("Failed to list operator pods: %v", err)
		return
	}

	if len(pods.Items) == 0 {
		logger.Warn("No operator pods found")
		return
	}

	// Get logs from the first operator pod
	pod := pods.Items[0]
	logger.Infof("📋 Recent operator logs from pod %s (last %d lines):", pod.Name, tailLines)

	logOptions := &v1.PodLogOptions{
		TailLines: &tailLines,
	}

	logs, err := clientset.CoreV1().Pods(operatorNamespace).GetLogs(pod.Name, logOptions).DoRaw(ctx)
	if err != nil {
		logger.Errorf("Failed to get operator logs: %v", err)
		return
	}

	// Print logs with indentation
	logLines := string(logs)
	logger.Infof("--- BEGIN OPERATOR LOGS ---")
	logger.Info(logLines)
	logger.Infof("--- END OPERATOR LOGS ---")
}

// debugPodStates provides detailed debugging info about pod states
func debugPodStates(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace, labelSelector string, logger *logrus.Logger) {
	t.Helper()

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		logger.Errorf("Failed to list pods: %v", err)
		return
	}

	logger.Debugf("📊 Pod States Summary: Total=%d", len(pods.Items))

	// Group pods by podclique
	podsByClique := make(map[string][]*v1.Pod)
	for i := range pods.Items {
		pod := &pods.Items[i]
		cliqueName := pod.Labels["grove.io/podclique"]
		if cliqueName == "" {
			cliqueName = "unknown"
		}
		podsByClique[cliqueName] = append(podsByClique[cliqueName], pod)
	}

	// Print per-clique stats
	for cliqueName, cliquePods := range podsByClique {
		ready := 0
		pending := 0
		running := 0
		failed := 0
		terminating := 0
		scheduled := 0

		logger.Debugf("  🔷 PodClique: %s (pods=%d)", cliqueName, len(cliquePods))
		for _, pod := range cliquePods {
			if pod.DeletionTimestamp != nil {
				terminating++
			}

			switch pod.Status.Phase {
			case v1.PodPending:
				pending++
			case v1.PodRunning:
				running++
			case v1.PodFailed:
				failed++
			}

			if pod.Spec.NodeName != "" {
				scheduled++
			}

			if isPodReady(pod) {
				ready++
			}

			// Detailed per-pod info for the affected clique
			if cliqueName == "workload1-0-sg-x-0-pc-c" || cliqueName == "workload2-0-pc-a" {
				logger.Debugf("    - Pod: %s, Phase=%s, Ready=%v, Scheduled=%v, Node=%s, Terminating=%v, UID=%s",
					pod.Name, pod.Status.Phase, isPodReady(pod), pod.Spec.NodeName != "", pod.Spec.NodeName,
					pod.DeletionTimestamp != nil, pod.UID)
			}
		}

		logger.Debugf("    Stats: Ready=%d, Scheduled=%d, Running=%d, Pending=%d, Failed=%d, Terminating=%d",
			ready, scheduled, running, pending, failed, terminating)
	}
}

// debugAllPodCliques checks status of all PodCliques in a workload
func debugAllPodCliques(ctx context.Context, t *testing.T, restConfig *rest.Config, namespace, pcsName string, logger *logrus.Logger) {
	t.Helper()

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		logger.Errorf("Failed to create dynamic client: %v", err)
		return
	}

	pclqGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}

	// List all podcliques in the namespace
	pclqList, err := dynamicClient.Resource(pclqGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("grove.io/podcliqueset=%s", pcsName),
	})
	if err != nil {
		logger.Errorf("Failed to list PodCliques: %v", err)
		return
	}

	logger.Debugf("📊 All PodCliques in %s (count=%d):", pcsName, len(pclqList.Items))

	for _, pclqItem := range pclqList.Items {
		pclqName := pclqItem.GetName()

		status, found, _ := unstructured.NestedMap(pclqItem.Object, "status")
		if !found {
			logger.Warnf("  ⚠️  PodClique %s has no status", pclqName)
			continue
		}

		spec, _, _ := unstructured.NestedMap(pclqItem.Object, "spec")
		minAvailable := spec["minAvailable"]

		replicas := status["replicas"]
		readyReplicas := status["readyReplicas"]
		scheduledReplicas := status["scheduledReplicas"]

		// Find MinAvailableBreached condition
		conditions, _, _ := unstructured.NestedSlice(status, "conditions")
		var breachStatus, breachReason, breachMessage, breachTime string
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			if condMap["type"] == "MinAvailableBreached" {
				breachStatus = fmt.Sprintf("%v", condMap["status"])
				breachReason = fmt.Sprintf("%v", condMap["reason"])
				breachMessage = fmt.Sprintf("%v", condMap["message"])
				breachTime = fmt.Sprintf("%v", condMap["lastTransitionTime"])
				break
			}
		}

		emoji := "✅"
		if breachStatus == "True" {
			emoji = "🔴"
		} else if breachStatus == "Unknown" {
			emoji = "⚠️"
		}

		logger.Debugf("  %s %s:", emoji, pclqName)
		logger.Debugf("    Spec: minAvailable=%v", minAvailable)
		logger.Debugf("    Status: replicas=%v, ready=%v, scheduled=%v", replicas, readyReplicas, scheduledReplicas)
		logger.Debugf("    MinAvailableBreached: status=%s, reason=%s", breachStatus, breachReason)
		logger.Debugf("    Message: %s", breachMessage)
		logger.Debugf("    LastTransitionTime: %s", breachTime)

		// Calculate time since transition
		if breachTime != "" && breachStatus == "True" {
			// Parse the time and calculate duration
			logger.Debugf("    ⏱️  Time since breach detected: %s", breachTime)
		}
	}
}

// debugEvents lists recent Kubernetes events in the namespace
func debugEvents(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace string, logger *logrus.Logger) {
	t.Helper()

	events, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.Errorf("Failed to list events: %v", err)
		return
	}

	logger.Debugf("📋 Recent Kubernetes Events (last 20):")

	// Sort events by timestamp (newest first) and limit to recent ones
	recentEvents := events.Items
	if len(recentEvents) > 20 {
		recentEvents = recentEvents[len(recentEvents)-20:]
	}

	for _, event := range recentEvents {
		emoji := "ℹ️"
		switch event.Type {
		case "Warning":
			emoji = "⚠️"
		case "Error":
			emoji = "❌"
		case "Normal":
			emoji = "✅"
		}

		logger.Debugf("  %s [%s] %s/%s: %s - %s (count=%d, last=%s)",
			emoji,
			event.Type,
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.Reason,
			event.Message,
			event.Count,
			event.LastTimestamp.Format("15:04:05"),
		)
	}
}

// uncordonNodes uncordons only the nodes that were cordoned by this test
func uncordonNodes(ctx context.Context, clientset kubernetes.Interface, cordonedNodes map[string]bool) error {
	for nodeName := range cordonedNodes {
		if err := utils.CordonNode(ctx, clientset, nodeName, false); err != nil {
			return fmt.Errorf("failed to uncordon node %s: %w", nodeName, err)
		}
		logger.Debugf("Uncordoned node %s", nodeName)
	}

	return nil
}
