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

// The file contains E2E tests for startup ordering functionality.
//
// Startup Ordering Mechanism:
// The Grove operator enforces startup ordering using init containers (grove-initc).
// The init container watches for parent PodCliques to reach their minAvailable count
// in the Ready state, blocking the pod from becoming ready until dependencies are satisfied.
//
// Test Verification Approach:
// These tests verify startup ordering by checking the LastTransitionTime of each pod's
// Ready condition (not CreationTimestamp). This is the correct approach because:
//   - CreationTimestamp: When the pod object was created (doesn't reflect dependencies)
//   - Ready LastTransitionTime: When the pod actually became ready (after init containers complete)
//
// The init container enforces ordering by blocking the Ready state, so we must check
// when pods became ready, not when they were created.

package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/grove/operator/e2e_testing/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test_SO1_InorderStartupOrderWithFullReplicas tests inorder startup with full replicas
// Scenario SO-1:
//  1. Initialize a 10-node Grove cluster
//  2. Deploy workload WL3, and verify 10 newly created pods
//  3. Wait for pods to get scheduled and become ready
//  4. Verify each print clique prints in the following order:
//     pcs-0-pc-a
//     pcs-0-sg-x-0-pc-b, pcs-0-sg-x-1-pc-b
//     pcs-0-sg-x-0-pc-c, pcs-0-sg-x-1-pc-c
func Test_SO1_InorderStartupOrderWithFullReplicas(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	logger.Info("2. Deploy workload WL3, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload3.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload3"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10 // pc-a: 2 replicas, pc-b: 1*2 (scaling group), pc-c: 3*2 (scaling group) = 2+2+6=10

	var pods *v1.PodList
	err = pollForCondition(ctx, defaultPollTimeout, defaultPollInterval, func() (bool, error) {
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
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("3. Wait for pods to get scheduled and become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	// Re-fetch pods to ensure we have the latest state with Ready conditions
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to re-fetch pods after waiting: %v", err)
	}

	logger.Info("4. Verify each print clique prints in the following order:")
	logger.Info("   pcs-0-pc-a")
	logger.Info("   pcs-0-sg-x-0-pc-b, pcs-0-sg-x-1-pc-b")
	logger.Info("   pcs-0-sg-x-0-pc-c, pcs-0-sg-x-1-pc-c")

	// Get pods by clique pattern
	pcAPods := getPodsByCliquePattern(pods.Items, "-pc-a-")
	pcBPodsFiltered := getPodsByCliquePattern(pods.Items, "-pc-b-")
	pcCPodsFiltered := getPodsByCliquePattern(pods.Items, "-pc-c-")

	// Verify we have the expected number of pods
	if len(pcAPods) != 2 {
		t.Fatalf("Expected 2 pc-a pods, got %d", len(pcAPods))
	}
	if len(pcBPodsFiltered) != 2 {
		t.Fatalf("Expected 2 pc-b pods, got %d", len(pcBPodsFiltered))
	}
	if len(pcCPodsFiltered) != 6 {
		t.Fatalf("Expected 6 pc-c pods, got %d", len(pcCPodsFiltered))
	}

	// Verify startup order: all pc-a pods should start before any pc-b pod,
	// and all pc-b pods should start before any pc-c pod
	verifyGroupStartupOrder(t, pcAPods, pcBPodsFiltered, "pc-a", "pc-b")
	verifyGroupStartupOrder(t, pcBPodsFiltered, pcCPodsFiltered, "pc-b", "pc-c")

	logger.Info("🎉 Inorder startup order with full replicas test completed successfully!")
}

// Test_SO2_InorderStartupOrderWithMinReplicas tests inorder startup with min replicas
// Scenario SO-2:
//  1. Initialize a 6-node Grove cluster
//  2. Deploy workload WL4, and verify 10 newly created pods
//  3. Wait for 6 pods to get scheduled and become ready (4 will be unschedulable since there are only 6 nodes available):
//     pcs-0-{pc-a = 2}
//     pcs-0-{sg-x-0-pc-b = 1, sg-x-0-pc-c = 3}
//  4. Verify each print clique prints in the following order:
//     pcs-0-pc-a
//     pcs-0-sg-x-0-pc-b
//     pcs-0-sg-x-0-pc-c
func Test_SO2_InorderStartupOrderWithMinReplicas(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 6-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 6)
	defer cleanup()

	logger.Info("2. Deploy workload WL4, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload4.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload4"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10 // pc-a: 2 replicas, pc-b: 1*2 (scaling group), pc-c: 3*2 (scaling group) = 2+2+6=10

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
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("3. Wait for 6 pods to get scheduled and become ready (4 will be unschedulable):")
	logger.Info("   pcs-0-{pc-a = 2}")
	logger.Info("   pcs-0-{sg-x-0-pc-b = 1, sg-x-0-pc-c = 3}")

	// Wait for 6 pods to become running (4 will remain unschedulable due to insufficient nodes)
	err = pollForCondition(ctx, defaultPollTimeout, defaultPollInterval, func() (bool, error) {
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			}
		}
		return runningPods == 6, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 6 pods to be running: %v", err)
	}

	logger.Info("4. Verify each print clique prints in the following order:")
	logger.Info("   pcs-0-pc-a")
	logger.Info("   pcs-0-sg-x-0-pc-b")
	logger.Info("   pcs-0-sg-x-0-pc-c")

	// Get running pods by clique pattern
	var runningPodsList []v1.Pod
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningPodsList = append(runningPodsList, pod)
		}
	}

	pcAPods := getPodsByCliquePattern(runningPodsList, "-pc-a-")
	sgX0PCBPods := getPodsByCliquePattern(runningPodsList, "-sg-x-0-pc-b-")
	sgX0PCCPods := getPodsByCliquePattern(runningPodsList, "-sg-x-0-pc-c-")

	// Verify we have the expected number of running pods
	if len(pcAPods) != 2 {
		t.Fatalf("Expected 2 running pc-a pods, got %d", len(pcAPods))
	}
	if len(sgX0PCBPods) != 1 {
		t.Fatalf("Expected 1 running sg-x-0-pc-b pod, got %d", len(sgX0PCBPods))
	}
	if len(sgX0PCCPods) != 3 {
		t.Fatalf("Expected 3 running sg-x-0-pc-c pods, got %d", len(sgX0PCCPods))
	}

	// Verify startup ordering: pc-a → sg-x-0-pc-b → sg-x-0-pc-c
	// First verify pc-a starts before sg-x-0-pc-b
	verifyGroupStartupOrder(t, pcAPods, sgX0PCBPods, "pcs-0-pc-a", "pcs-0-sg-x-0-pc-b")

	// Then verify sg-x-0-pc-b starts before sg-x-0-pc-c
	verifyGroupStartupOrder(t, sgX0PCBPods, sgX0PCCPods, "pcs-0-sg-x-0-pc-b", "pcs-0-sg-x-0-pc-c")

	logger.Info("🎉 Inorder startup order with min replicas test completed successfully!")
}

// Test_SO3_ExplicitStartupOrderWithFullReplicas tests explicit startup order with full replicas
// Scenario SO-3:
//  1. Initialize a 10-node Grove cluster
//  2. Deploy workload WL5, and verify 10 newly created pods
//  3. Wait for pods to get scheduled and become ready
//  4. Verify each print clique prints in the following order:
//     pcs-0-pc-a
//     pcs-0-sg-x-0-pc-c, pcs-0-sg-x-1-pc-c
//     pcs-0-sg-x-0-pc-b, pcs-0-sg-x-1-pc-b
func Test_SO3_ExplicitStartupOrderWithFullReplicas(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	logger.Info("2. Deploy workload WL5, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload5.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload5"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10 // pc-a: 2 replicas, pc-b: 1*2 (scaling group), pc-c: 3*2 (scaling group) = 2+2+6=10

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
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("3. Wait for pods to get scheduled and become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, workloadLabelSelector, defaultPollTimeout, defaultPollInterval, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	// Re-fetch pods to ensure we have the latest state with Ready conditions
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to re-fetch pods after waiting: %v", err)
	}

	logger.Info("4. Verify each print clique prints in the following order:")
	logger.Info("   pcs-0-pc-a")
	logger.Info("   pcs-0-sg-x-0-pc-c, pcs-0-sg-x-1-pc-c")
	logger.Info("   pcs-0-sg-x-0-pc-b, pcs-0-sg-x-1-pc-b")

	// Get pods by clique pattern
	pcAPods := getPodsByCliquePattern(pods.Items, "-pc-a-")
	pcBPodsFiltered := getPodsByCliquePattern(pods.Items, "-pc-b-")
	pcCPodsFiltered := getPodsByCliquePattern(pods.Items, "-pc-c-")

	// Verify we have the expected number of pods
	if len(pcAPods) != 2 {
		t.Fatalf("Expected 2 pc-a pods, got %d", len(pcAPods))
	}
	if len(pcBPodsFiltered) != 2 {
		t.Fatalf("Expected 2 pc-b pods, got %d", len(pcBPodsFiltered))
	}
	if len(pcCPodsFiltered) != 6 {
		t.Fatalf("Expected 6 pc-c pods, got %d", len(pcCPodsFiltered))
	}

	// Verify startup order: all pc-a pods should start first, then all pc-c pods,
	// then all pc-b pods (explicit dependency: pc-b starts after pc-c)
	verifyGroupStartupOrder(t, pcAPods, pcCPodsFiltered, "pc-a", "pc-c")
	verifyGroupStartupOrder(t, pcAPods, pcBPodsFiltered, "pc-a", "pc-b")
	verifyGroupStartupOrder(t, pcCPodsFiltered, pcBPodsFiltered, "pc-c", "pc-b")

	logger.Info("🎉 Explicit startup order with full replicas test completed successfully!")
}

// Test_SO4_ExplicitStartupOrderWithMinReplicas tests explicit startup order with min replicas
// Scenario SO-4:
//  1. Initialize a 6-node Grove cluster
//  2. Deploy workload WL6, and verify 10 newly created pods
//  3. Wait for 6 pods to get scheduled and become ready (4 will be unschedulable since there are only 6 nodes available):
//     pcs-0-{pc-a = 2}
//     pcs-0-{sg-x-0-pc-b = 1, sg-x-0-pc-c = 3}
//  4. Verify each print clique prints in the following order:
//     pcs-0-pc-a
//     pcs-0-sg-x-0-pc-b
//     pcs-0-sg-x-0-pc-c
func Test_SO4_ExplicitStartupOrderWithMinReplicas(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 6-node Grove cluster")
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 6)
	defer cleanup()

	logger.Info("2. Deploy workload WL6, and verify 10 newly created pods")
	workloadNamespace := "default"
	workloadYAMLPath := "../yaml/workload6.yaml"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload6"

	_, err := utils.ApplyYAMLFile(ctx, workloadYAMLPath, workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	expectedPods := 10 // pc-a: 2 replicas, pc-b: 1*2 (scaling group), pc-c: 3*2 (scaling group) = 2+2+6=10

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
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("3. Wait for 6 pods to get scheduled and become ready (4 will be unschedulable):")
	logger.Info("   pcs-0-{pc-a = 2}")
	logger.Info("   pcs-0-{sg-x-0-pc-b = 1, sg-x-0-pc-c = 3}")

	// Wait for 6 pods to become running (4 will remain unschedulable due to insufficient nodes)
	err = pollForCondition(ctx, 10*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadLabelSelector,
		})
		if err != nil {
			return false, err
		}
		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			}
		}
		return runningPods == 6, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 6 pods to be running: %v", err)
	}

	logger.Info("4. Verify each print clique prints in the following order:")
	logger.Info("   pcs-0-pc-a")
	logger.Info("   pcs-0-sg-x-0-pc-b")
	logger.Info("   pcs-0-sg-x-0-pc-c")

	// Get running pods by clique pattern
	var runningPodsList []v1.Pod
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningPodsList = append(runningPodsList, pod)
		}
	}

	pcAPods := getPodsByCliquePattern(runningPodsList, "-pc-a-")
	sgX0PCBPods := getPodsByCliquePattern(runningPodsList, "-sg-x-0-pc-b-")
	sgX0PCCPods := getPodsByCliquePattern(runningPodsList, "-sg-x-0-pc-c-")

	// Verify we have the expected number of running pods
	if len(pcAPods) != 2 {
		t.Fatalf("Expected 2 running pc-a pods, got %d", len(pcAPods))
	}
	if len(sgX0PCBPods) != 1 {
		t.Fatalf("Expected 1 running sg-x-0-pc-b pod, got %d", len(sgX0PCBPods))
	}
	if len(sgX0PCCPods) != 3 {
		t.Fatalf("Expected 3 running sg-x-0-pc-c pods, got %d", len(sgX0PCCPods))
	}

	// Verify startup ordering: pc-a → sg-x-0-pc-b → sg-x-0-pc-c
	// First verify pc-a starts before sg-x-0-pc-b
	verifyGroupStartupOrder(t, pcAPods, sgX0PCBPods, "pcs-0-pc-a", "pcs-0-sg-x-0-pc-b")

	// Then verify sg-x-0-pc-b starts before sg-x-0-pc-c
	verifyGroupStartupOrder(t, sgX0PCBPods, sgX0PCCPods, "pcs-0-sg-x-0-pc-b", "pcs-0-sg-x-0-pc-c")

	logger.Info("🎉 Explicit startup order with min replicas test completed successfully!")
}

// Helper function to get the Ready condition's LastTransitionTime from a pod
// According to the sample files, this is the correct timestamp to check for startup ordering
func getReadyConditionTransitionTime(pod v1.Pod) time.Time {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return condition.LastTransitionTime.Time
		}
	}
	// Debug: log why we couldn't find a Ready timestamp
	logger.Debugf("Pod %s has no Ready=True condition. Phase: %s, Conditions: %+v",
		pod.Name, pod.Status.Phase, pod.Status.Conditions)
	return time.Time{}
}

// Helper function to get the earliest Ready transition time from a list of pods
func getEarliestPodTime(pods []v1.Pod) time.Time {
	if len(pods) == 0 {
		return time.Time{}
	}

	var earliest time.Time
	for _, pod := range pods {
		readyTime := getReadyConditionTransitionTime(pod)
		if readyTime.IsZero() {
			continue // Skip pods without a valid Ready timestamp
		}
		if earliest.IsZero() || readyTime.Before(earliest) {
			earliest = readyTime
		}
	}
	return earliest
}

// Helper function to get the latest Ready transition time from a list of pods
func getLatestPodTime(pods []v1.Pod) time.Time {
	if len(pods) == 0 {
		return time.Time{}
	}

	var latest time.Time
	for _, pod := range pods {
		readyTime := getReadyConditionTransitionTime(pod)
		if readyTime.IsZero() {
			continue // Skip pods without a valid Ready timestamp
		}
		if readyTime.After(latest) {
			latest = readyTime
		}
	}
	return latest
}

// Helper function to verify that all pods in groupBefore started before all pods in groupAfter
func verifyGroupStartupOrder(t *testing.T, groupBefore, groupAfter []v1.Pod, beforeName, afterName string) {
	if len(groupBefore) == 0 {
		t.Fatalf("Group %s has no pods", beforeName)
	}
	if len(groupAfter) == 0 {
		t.Fatalf("Group %s has no pods", afterName)
	}

	// Get the latest time from the "before" group
	latestBefore := getLatestPodTime(groupBefore)
	// Get the earliest time from the "after" group
	earliestAfter := getEarliestPodTime(groupAfter)

	// Check for pods without Ready timestamps
	if latestBefore.IsZero() {
		// Debug: Show which pods don't have Ready timestamps
		logger.Errorf("Group %s has no pods with valid Ready timestamps. Debugging pod states:", beforeName)
		for i, pod := range groupBefore {
			readyTime := getReadyConditionTransitionTime(pod)
			logger.Errorf("  Pod[%d] %s: Phase=%s, ReadyTime=%v", i, pod.Name, pod.Status.Phase, readyTime)
		}
		t.Fatalf("Group %s has no pods with valid Ready condition timestamps (pods may not be ready yet)", beforeName)
	}
	if earliestAfter.IsZero() {
		// Debug: Show which pods don't have Ready timestamps
		logger.Errorf("Group %s has no pods with valid Ready timestamps. Debugging pod states:", afterName)
		for i, pod := range groupAfter {
			readyTime := getReadyConditionTransitionTime(pod)
			logger.Errorf("  Pod[%d] %s: Phase=%s, ReadyTime=%v", i, pod.Name, pod.Status.Phase, readyTime)
		}
		t.Fatalf("Group %s has no pods with valid Ready condition timestamps (pods may not be ready yet)", afterName)
	}

	// Verify the ordering: all pods in groupBefore should start before any pod in groupAfter
	if earliestAfter.Before(latestBefore) {
		t.Fatalf("Startup order violation: group %s (earliest at %v) started before group %s (latest at %v)",
			afterName, earliestAfter, beforeName, latestBefore)
	}

	logger.Debugf("✓ Verified startup order: %s (latest: %v) → %s (earliest: %v)",
		beforeName, latestBefore, afterName, earliestAfter)
}

// Helper function to get pods by clique name pattern
func getPodsByCliquePattern(pods []v1.Pod, pattern string) []v1.Pod {
	var result []v1.Pod
	for _, pod := range pods {
		if strings.Contains(pod.Name, pattern) {
			result = append(result, pod)
		}
	}
	return result
}
