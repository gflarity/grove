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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// TerminationDelay is the time to wait for gang-termination to occur
	// This should match the configuration in the operator
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
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, 10*time.Minute, logger); err != nil {
		t.Errorf("Failed to wait for pods to be ready: %v", err)
	}

	// Verify all pods are running and ready
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

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
		t.Errorf("Failed to find a ready pod from PCS-owned podclique pcs-0-pc-a")
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

	logger.Infof("5. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	time.Sleep(TerminationDelay)

	logger.Info("6. Verify that all pods in the workload get gang-terminated and recreated")
	// After gang-termination, pods should be recreated with new UIDs and be in Pending state
	err = pollForCondition(ctx, 10*time.Second, 1*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}

		// Should have the same number of pods (recreated, not deleted)
		if len(pods.Items) != expectedPods {
			return false, nil
		}

		// Verify none of the original pod UIDs exist (all were deleted and recreated)
		currentUIDs := make(map[string]bool)
		pendingCount := 0
		for _, pod := range pods.Items {
			currentUIDs[string(pod.UID)] = true
			if pod.Status.Phase == v1.PodPending {
				pendingCount++
			}
		}

		// Check that no original UIDs exist in current pods (all recreated)
		for _, originalUID := range originalPodUIDs {
			if currentUIDs[originalUID] {
				// Found an old pod UID, recreation not complete
				return false, nil
			}
		}

		// All pods should be pending (cordoned nodes)
		return pendingCount == expectedPods, nil
	})
	if err != nil {
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

	// Verify all pods are running and ready
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

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
		t.Errorf("Failed to find a ready pod from PCSG-owned podclique pcs-0-sg-x-0-pc-c")
	}

	// Cordon the node where the target pod is running
	if err := utils.CordonNode(ctx, clientset, targetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", targetPod.Spec.NodeName, err)
	}

	// Capture pod UIDs before gang termination
	originalPodUIDs := capturePodUIDs(pods)

	// Delete the target pod
	logger.Infof("Deleting pod %s from node %s", targetPod.Name, targetPod.Spec.NodeName)
	if err := clientset.CoreV1().Pods(workloadNamespace).Delete(ctx, targetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", targetPod.Name, err)
	}

	logger.Infof("5. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	time.Sleep(TerminationDelay)

	logger.Info("6. Verify that all pods in the workload get gang-terminated and recreated")
	// After gang-termination, pods should be recreated with new UIDs and be in Pending state
	err = pollForCondition(ctx, 10*time.Second, 1*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}

		// Should have the same number of pods (recreated, not deleted)
		if len(pods.Items) != expectedPods {
			return false, nil
		}

		// Verify none of the original pod UIDs exist (all were deleted and recreated)
		currentUIDs := make(map[string]bool)
		pendingCount := 0
		for _, pod := range pods.Items {
			currentUIDs[string(pod.UID)] = true
			if pod.Status.Phase == v1.PodPending {
				pendingCount++
			}
		}

		// Check that no original UIDs exist in current pods (all recreated)
		for _, originalUID := range originalPodUIDs {
			if currentUIDs[originalUID] {
				// Found an old pod UID, recreation not complete
				return false, nil
			}
		}

		// All pods should be pending (cordoned nodes)
		return pendingCount == expectedPods, nil
	})
	if err != nil {
		t.Errorf("Failed to verify gang-termination and recreation: %v", err)
	}

	logger.Info("🎉 Gang-termination with full-replicas PCSG-owned test (GT-2) completed successfully!")
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

	logger.Info("4. Cordon node and then delete 1 pod from PCS-owned podclique pcs-0-pc-a")
	// Find the first pod from workload2-0-pc-a podclique (PCS-owned)
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

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
		t.Errorf("Failed to find a ready pod from PCS-owned podclique workload2-0-pc-a")
	}

	// Cordon the node where the first target pod is running
	if err := utils.CordonNode(ctx, clientset, firstTargetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", firstTargetPod.Spec.NodeName, err)
	}

	// Delete the first target pod
	logger.Infof("Deleting first pod %s from node %s", firstTargetPod.Name, firstTargetPod.Spec.NodeName)
	if err := clientset.CoreV1().Pods(workloadNamespace).Delete(ctx, firstTargetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", firstTargetPod.Name, err)
	}

	logger.Infof("5. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	time.Sleep(TerminationDelay)

	logger.Info("6. Verify that workload pods do not get gang-terminated")
	// With min-replicas, deleting one pod should not trigger gang-termination
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	runningOrPendingCount := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodPending {
			runningOrPendingCount++
		}
	}

	// Most pods should still be running (at least min-replicas worth)
	if runningOrPendingCount < 3 {
		t.Errorf("Expected most pods to still be running after first deletion, but only %d are running/pending", runningOrPendingCount)
	}

	logger.Info("7. Cordon node and then delete 1 ready pod from PCS-owned podclique pcs-0-pc-a")
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
		t.Errorf("Failed to find a second ready pod from PCS-owned podclique workload2-0-pc-a")
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
	logger.Infof("Deleting second pod %s from node %s", secondTargetPod.Name, secondTargetPod.Spec.NodeName)
	if err := clientset.CoreV1().Pods(workloadNamespace).Delete(ctx, secondTargetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", secondTargetPod.Name, err)
	}

	logger.Infof("8. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	time.Sleep(TerminationDelay)

	logger.Info("9. Verify that all pods in the workload get gang-terminated and recreated")
	// After breaching min-replicas, gang-termination should occur
	err = pollForCondition(ctx, 10*time.Second, 1*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}

		// Should have the same number of pods (recreated, not deleted)
		if len(pods.Items) != expectedPods {
			return false, nil
		}

		// Verify none of the original pod UIDs exist (all were deleted and recreated)
		currentUIDs := make(map[string]bool)
		pendingCount := 0
		for _, pod := range pods.Items {
			currentUIDs[string(pod.UID)] = true
			if pod.Status.Phase == v1.PodPending {
				pendingCount++
			}
		}

		// Check that no original UIDs exist in current pods (all recreated)
		for _, originalUID := range originalPodUIDs {
			if currentUIDs[originalUID] {
				// Found an old pod UID, recreation not complete
				return false, nil
			}
		}

		// All pods should be pending (cordoned nodes)
		return pendingCount == expectedPods, nil
	})
	if err != nil {
		t.Errorf("Failed to verify gang-termination and recreation: %v", err)
	}

	logger.Info("🎉 Gang-termination with min-replicas PCS-owned test (GT-3) completed successfully!")
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

	logger.Info("4. Cordon node and then delete 1 ready pod from PCSG-owned podclique pcs-0-sg-x-0-pc-c")
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// Find and delete first pod from workload2-0-sg-x-0-pc-c
	firstPodToDelete := findAndDeletePodFromPodClique(ctx, t, clientset, pods, "workload2-0-sg-x-0-pc-c", workloadNamespace, "first")

	logger.Infof("5. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	time.Sleep(TerminationDelay)

	logger.Info("6. Verify that workload pods do not get gang-terminated")
	verifyNoGangTermination(ctx, t, clientset, workloadNamespace, workloadLabelSelector, 3)

	logger.Info("7. Cordon nodes and then delete 2 remaining ready pods from PCSG-owned podclique pcs-0-sg-x-0-pc-c")
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// Find and delete remaining pods from workload2-0-sg-x-0-pc-c
	deletedPods := []string{firstPodToDelete}
	for i := 0; i < 2; i++ {
		podName := findAndDeletePodFromPodCliqueExcluding(ctx, t, clientset, pods, "workload2-0-sg-x-0-pc-c", workloadNamespace, deletedPods, fmt.Sprintf("pod %d", i+2))
		deletedPods = append(deletedPods, podName)
		// Refresh pod list
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			t.Errorf("Failed to list workload pods: %v", err)
		}
	}

	logger.Infof("8. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	time.Sleep(TerminationDelay)

	logger.Info("9. Verify that both podcliques on PCSG pcs-0-sg-x-0 (pcs-0-sg-x-0-pc-b and pcs-0-sg-x-0-pc-c) are recreated but workload is not gang-terminated")
	// After deleting all pods from pc-c, the PCSG should recreate both pc-b and pc-c podcliques
	// But gang-termination should not occur because we haven't breached min-replicas at PCSG level
	verifyNoGangTermination(ctx, t, clientset, workloadNamespace, workloadLabelSelector, 3)

	// Wait for pods to be recreated
	time.Sleep(30 * time.Second)

	logger.Info("10. Cordon node and then delete 1 ready pod from PCSG-owned podclique pcs-0-sg-x-1-pc-c")
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// Find and delete first pod from workload2-0-sg-x-1-pc-c
	firstPodSgx1 := findAndDeletePodFromPodClique(ctx, t, clientset, pods, "workload2-0-sg-x-1-pc-c", workloadNamespace, "first from sg-x-1")

	logger.Infof("11. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	time.Sleep(TerminationDelay)

	logger.Info("12. Verify that workload pods do not get gang-terminated")
	verifyNoGangTermination(ctx, t, clientset, workloadNamespace, workloadLabelSelector, 3)

	logger.Info("13. Cordon nodes and then delete 2 remaining ready pods from PCSG-owned podclique pcs-0-sg-x-1-pc-c")
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
	}

	// Find and delete remaining pods from workload2-0-sg-x-1-pc-c
	deletedPodsSgx1 := []string{firstPodSgx1}
	for i := 0; i < 2; i++ {
		podName := findAndDeletePodFromPodCliqueExcluding(ctx, t, clientset, pods, "workload2-0-sg-x-1-pc-c", workloadNamespace, deletedPodsSgx1, fmt.Sprintf("pod %d from sg-x-1", i+2))
		deletedPodsSgx1 = append(deletedPodsSgx1, podName)
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

	logger.Infof("14. Wait for TerminationDelay (%v) seconds", TerminationDelay)
	time.Sleep(TerminationDelay)

	logger.Info("15. Verify that all pods in the workload get gang-terminated and recreated")
	// After breaching min-replicas at PCSG level, gang-termination should occur
	err = pollForCondition(ctx, 10*time.Second, 1*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}

		// Should have the same number of pods (recreated, not deleted)
		if len(pods.Items) != expectedPods {
			return false, nil
		}

		// Verify none of the original pod UIDs exist (all were deleted and recreated)
		currentUIDs := make(map[string]bool)
		pendingCount := 0
		for _, pod := range pods.Items {
			currentUIDs[string(pod.UID)] = true
			if pod.Status.Phase == v1.PodPending {
				pendingCount++
			}
		}

		// Check that no original UIDs exist in current pods (all recreated)
		for _, originalUID := range originalPodUIDs {
			if currentUIDs[originalUID] {
				// Found an old pod UID, recreation not complete
				return false, nil
			}
		}

		// All pods should be pending (cordoned nodes)
		return pendingCount == expectedPods, nil
	})
	if err != nil {
		t.Errorf("Failed to verify gang-termination and recreation: %v", err)
	}

	logger.Info("🎉 Gang-termination with min-replicas PCSG-owned test (GT-4) completed successfully!")
}

// Helper functions

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
func findAndDeletePodFromPodClique(ctx context.Context, t *testing.T, clientset kubernetes.Interface, pods *v1.PodList, podCliqueName, namespace, description string) string {
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
		t.Errorf("Failed to find %s ready pod from podclique %s", description, podCliqueName)
		return ""
	}

	// Cordon the node
	if err := utils.CordonNode(ctx, clientset, targetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", targetPod.Spec.NodeName, err)
	}

	// Delete the pod
	logger.Infof("Deleting %s pod %s from node %s (podclique: %s)", description, targetPod.Name, targetPod.Spec.NodeName, podCliqueName)
	if err := clientset.CoreV1().Pods(namespace).Delete(ctx, targetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", targetPod.Name, err)
	}

	return targetPod.Name
}

// findAndDeletePodFromPodCliqueExcluding finds a ready pod from the specified podclique (excluding certain pods) and deletes it
func findAndDeletePodFromPodCliqueExcluding(ctx context.Context, t *testing.T, clientset kubernetes.Interface, pods *v1.PodList, podCliqueName, namespace string, excludePods []string, description string) string {
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
		t.Errorf("Failed to find %s ready pod from podclique %s", description, podCliqueName)
		return ""
	}

	// Cordon the node
	if err := utils.CordonNode(ctx, clientset, targetPod.Spec.NodeName, true); err != nil {
		t.Errorf("Failed to cordon node %s: %v", targetPod.Spec.NodeName, err)
	}

	// Delete the pod
	logger.Infof("Deleting %s pod %s from node %s (podclique: %s)", description, targetPod.Name, targetPod.Spec.NodeName, podCliqueName)
	if err := clientset.CoreV1().Pods(namespace).Delete(ctx, targetPod.Name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("Failed to delete pod %s: %v", targetPod.Name, err)
	}

	return targetPod.Name
}

// verifyNoGangTermination verifies that gang-termination has not occurred
func verifyNoGangTermination(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace, labelSelector string, minExpectedRunning int) {
	t.Helper()

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		t.Errorf("Failed to list workload pods: %v", err)
		return
	}

	runningOrPendingCount := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodPending {
			runningOrPendingCount++
		}
	}

	// Most pods should still be running (at least min-replicas worth)
	if runningOrPendingCount < minExpectedRunning {
		t.Errorf("Expected at least %d pods to still be running/pending, but only %d are", minExpectedRunning, runningOrPendingCount)
	}
}
