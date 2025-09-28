package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/NVIDIA/grove/operator/ci/utils"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	// isRunningFullSuite tracks whether we're running the full test suite via TestMain
	isRunningFullSuite bool
)

// TestMain manages the lifecycle of the shared cluster for all tests
func TestMain(m *testing.M) {
	ctx := context.Background()

	// Mark that we're running the full test suite
	isRunningFullSuite = true

	// Setup shared cluster once for all tests
	sharedCluster := GetSharedCluster()
	if err := sharedCluster.Setup(ctx); err != nil {
		fmt.Printf("❌ Failed to setup shared cluster: %v\n", err)
		os.Exit(1)
	}

	// Run all tests
	code := m.Run()

	// Teardown shared cluster
	sharedCluster.Teardown()

	os.Exit(code)
}

// setupTestCluster sets up the shared cluster for a test
func setupTestCluster(ctx context.Context, t *testing.T, requiredAgents int) (*kubernetes.Clientset, *rest.Config, dynamic.Interface, func(), string) {
	// Always use shared cluster approach
	sharedCluster := GetSharedCluster()

	// Setup shared cluster if not already done
	if !sharedCluster.IsSetup() {
		if err := sharedCluster.Setup(ctx); err != nil {
			t.Fatalf("Failed to setup shared cluster: %v", err)
		}
	}

	if err := sharedCluster.PrepareForTest(ctx, t, requiredAgents); err != nil {
		t.Fatalf("Failed to prepare shared cluster for test: %v", err)
	}

	clientset, restConfig, dynamicClient := sharedCluster.GetClients()

	// Cleanup function cleans workloads and handles teardown for individual tests
	cleanup := func() {
		if err := sharedCluster.CleanupWorkloads(ctx, t); err != nil {
			t.Logf("Warning: failed to cleanup workloads: %v", err)
		}

		// If running individual test (not full suite), teardown the cluster completely
		if !isRunningFullSuite {
			sharedCluster.Teardown()
		}
	}

	return clientset, restConfig, dynamicClient, cleanup, sharedCluster.GetRegistryPort()
}

// Test_GS1_GangSchedulingWithFullReplicas tests gang-scheduling behavior with insufficient resources
// Scenario GS-1:
// 1. Initialize a 10-node Grove cluster, then cordon 1 node
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon the node and verify all pods get scheduled
func Test_GS1_GangSchedulingWithFullReplicas(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with full replicas")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 1 {
		t.Fatalf("Need at least 1 agent node to cordon, but found %d", len(agentNodes))
	}

	agentNodeToCordon := agentNodes[0]
	t.Logf("🚫 Cordoning agent node: %s", agentNodeToCordon)
	if err := cordonNode(ctx, clientset, agentNodeToCordon, true); err != nil {
		t.Fatalf("Failed to cordon node %s: %v", agentNodeToCordon, err)
	}

	// 2. Deploy workload WL1, and verify 10 newly created pods
	// Deploy workload1.yaml
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath: "../yaml/workload1.yaml",
		Namespace:    workloadNamespace,
		RestConfig:   restConfig,
		Timeout:      1 * time.Minute, // Short timeout since we expect pods to be pending
	}

	t.Log("🚀 Applying workload1.yaml...")
	_, err = utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	// Poll for pod creation and verify they are pending
	t.Log("🔍 Polling for pods to be created and verifying they remain pending...")
	expectedPods := 10 // pc-a: 2 replicas, pc-b: 1*2 (scaling group), pc-c: 3*2 (scaling group) = 2+2+6=10

	// Poll until we have the expected number of pods created
	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/part-of=workload1",
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	t.Logf("✅ Found %d workload pods as expected", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	// Verify all pods are pending due to unschedulable nodes
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
	}

	t.Logf("✅ Verified %d pods are pending (expected all %d to be pending)", pendingPods, len(pods.Items))

	// Poll to verify pods remain pending for a reasonable time (gang scheduling should prevent partial scheduling)
	t.Log("🔍 Verifying pods remain pending due to gang scheduling...")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/part-of=workload1",
		})
		if err != nil {
			return false, err
		}

		stillPendingPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPendingPods++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPendingPods, len(pods.Items))
		// We're checking that they remain pending, so we want this condition to be true consistently
		return stillPendingPods == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	t.Log("✅ Verified pods remain pending (gang scheduling working correctly)")

	// 4. Uncordon the node and verify all pods get scheduled
	// Uncordon the node to provide sufficient resources
	t.Logf("✅ Uncordoning agent node: %s", agentNodeToCordon)
	if err := cordonNode(ctx, clientset, agentNodeToCordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", agentNodeToCordon, err)
	}

	// Wait for all pods to be scheduled and ready
	t.Log("⏳ Waiting for all pods to be scheduled and ready...")
	workloadConfig.Timeout = 10 * time.Minute // Allow more time for workload pods
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	// Verify all pods are now running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/part-of=workload1",
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	runningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningPods++
		}
	}

	t.Logf("✅ Verified %d pods are now running (expected all %d to be running)", runningPods, len(pods.Items))

	if runningPods != len(pods.Items) {
		t.Errorf("Expected all %d pods to be running, but only %d are running", len(pods.Items), runningPods)
	}
	assertPodsOnDistinctNodes(t, pods.Items)

	t.Log("🎉 Gang-scheduling test completed successfully! Grove, Kai, and NVIDIA GPU Operator installed, all workload pods transitioned from pending to running after uncordoning.")

	// Note: Cleanup is handled by the shared cluster cleanup function
}

// Test_GS2_GangSchedulingWithScalingFullReplicas verifies gang-scheduling behavior when scaling a PodCliqueScalingGroup
// Scenario GS-2:
// 1. Initialize a 14-node Grove cluster, then cordon 5 nodes
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node to allow scheduling and verify pods get scheduled
// 5. Wait for pods to become ready
// 6. Scale PCSG replicas to 3 and verify 4 new pending pods
// 7. Uncordon remaining nodes and verify all pods get scheduled
func Test_GS2_GangSchedulingWithScalingFullReplicas(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 14)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCSG scaling (14 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 5 {
		t.Fatalf("expected at least 5 agent nodes to cordon, but found %d", len(agentNodes))
	}

	nodesToCordon := agentNodes[:5]
	fmt.Printf("🚫 Cordoning %d agent nodes: %v\n", len(nodesToCordon), nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL1, and verify 10 newly created pods
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload1.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute,
		PodLabelSelector: "app.kubernetes.io/part-of=workload1",
	}

	fmt.Printf("🚀 Applying workload1.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// 4. Uncordon 1 node to allow scheduling and verify pods get scheduled
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// 5. Wait for pods to become ready
	fmt.Printf("⏳ Waiting for all pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	runningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", runningPods, len(pods.Items))

	if runningPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be running, but only %d are running", len(pods.Items), runningPods)
	}
	assertPodsOnDistinctNodes(t, pods.Items)

	// 6. Scale PCSG replicas to 3 and verify 4 new pending pods
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	pcsgGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquescalinggroups"}
	pcsgName := "workload1-0-sg-x"

	fmt.Printf("🔍 Waiting for PodCliqueScalingGroup %s to become available...\n", pcsgName)
	err = pollForCondition(ctx, 3*time.Minute, 5*time.Second, func() (bool, error) {
		_, err := dynamicClient.Resource(pcsgGVR).Namespace(workloadNamespace).Get(ctx, pcsgName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("Failed to find PodCliqueScalingGroup %s: %v", pcsgName, err)
	}

	scalePatch := map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": 3,
		},
	}
	patchBytes, err := json.Marshal(scalePatch)
	if err != nil {
		t.Fatalf("Failed to marshal scale patch: %v", err)
	}

	fmt.Printf("📈 Scaling PodCliqueScalingGroup %s to replicas=3...\n", pcsgName)
	if _, err := dynamicClient.Resource(pcsgGVR).Namespace(workloadNamespace).Patch(ctx, pcsgName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
		t.Fatalf("Failed to scale PodCliqueScalingGroup %s: %v", pcsgName, err)
	}

	fmt.Printf("🔄 Waiting for scaled pods to be created...\n")
	expectedScaledPods := 14
	err = pollForCondition(ctx, 3*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods after scaling (waiting for %d)\n", len(pods.Items), expectedScaledPods)
		return len(pods.Items) == expectedScaledPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for scaled pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods after scaling as expected\n", len(pods.Items))

	runningPods = 0
	pendingPods = 0
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodRunning:
			runningPods++
		case v1.PodPending:
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("📊 Post-scaling pod states: %d running, %d pending\n", runningPods, pendingPods)
	if pendingPods != 4 {
		t.Fatalf("Expected 4 pending pods after scaling, but found %d", pendingPods)
	}
	if runningPods != expectedPods {
		t.Fatalf("Expected %d running pods after scaling, but found %d", expectedPods, runningPods)
	}

	// 7. Uncordon remaining nodes and verify all pods get scheduled
	remainingNodesToUncordon := nodesToCordon[1:]
	fmt.Printf("✅ Uncordoning remaining agent nodes: %v\n", remainingNodesToUncordon)
	for _, nodeName := range remainingNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all scaled pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 15 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for scaled pods to be ready: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods after final uncordon: %v", err)
	}

	runningPods = 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running after scaling (expected all %d to be running)\n", runningPods, len(pods.Items))
	if runningPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be running after scaling, but only %d are running", len(pods.Items), runningPods)
	}
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCSG scaling test completed successfully!\n")

	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
	}
}

// TestGangSchedulingWithPCSScalingFullReplicas verifies gang-scheduling behavior when scaling a PodCliqueSet
// Scenario GS-3:
// 1. Initialize a 20-node Grove cluster, then cordon 11 nodes
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node to allow scheduling and verify pods get scheduled
// 5. Wait for pods to become ready
// 6. Scale PCS replicas to 2 and verify 10 new pending pods
// 7. Uncordon remaining nodes and verify all pods get scheduled
func Test_GS3_GangSchedulingWithPCSScalingFullReplicas(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 20)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCS scaling (20 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 11 {
		t.Fatalf("expected at least 11 agent nodes to cordon, but found %d", len(agentNodes))
	}

	// Step 1 (continued): Cordon 11 nodes
	nodesToCordon := agentNodes[:11]
	fmt.Printf("🚫 Cordoning %d agent nodes: %v\n", len(nodesToCordon), nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL1, and verify 10 newly created pods
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload1.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute,
		PodLabelSelector: "app.kubernetes.io/part-of=workload1",
	}

	fmt.Printf("🚀 Applying workload1.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// 4. Uncordon 1 node to allow scheduling and verify pods get scheduled
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// 5. Wait for pods to become ready
	fmt.Printf("⏳ Waiting for all pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	runningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", runningPods, len(pods.Items))

	if runningPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be running, but only %d are running", len(pods.Items), runningPods)
	}
	assertPodsOnDistinctNodes(t, pods.Items)

	// 6. Scale PCS replicas to 2 and verify 10 new pending pods
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	replicas := int32(2)
	pcsPatch := map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": replicas,
		},
	}
	pcsPatchBytes, err := json.Marshal(pcsPatch)
	if err != nil {
		t.Fatalf("Failed to marshal PodCliqueSet patch: %v", err)
	}

	pcsGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquesets"}
	pcsName := "workload1"

	fmt.Printf("📈 Scaling PodCliqueSet %s to replicas=%d...\n", pcsName, replicas)
	if _, err := dynamicClient.Resource(pcsGVR).Namespace(workloadNamespace).Patch(ctx, pcsName, types.MergePatchType, pcsPatchBytes, metav1.PatchOptions{}); err != nil {
		t.Fatalf("Failed to scale PodCliqueSet %s: %v", pcsName, err)
	}

	expectedScaledPods := int(replicas) * expectedPods

	fmt.Printf("🔄 Waiting for workload pods to be created after scaling (expect %d)...\n", expectedScaledPods)
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods after scaling (waiting for %d)\n", len(pods.Items), expectedScaledPods)
		return len(pods.Items) == expectedScaledPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for scaled pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods after scaling as expected\n", len(pods.Items))

	runningPods = 0
	pendingPods = 0
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodRunning:
			runningPods++
		case v1.PodPending:
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	expectedNewPending := expectedScaledPods - expectedPods
	fmt.Printf("📊 Post-scaling pod states: %d running, %d pending\n", runningPods, pendingPods)
	if pendingPods != expectedNewPending {
		t.Fatalf("Expected %d pending pods after scaling, but found %d", expectedNewPending, pendingPods)
	}
	if runningPods != expectedPods {
		t.Fatalf("Expected %d running pods after scaling, but found %d", expectedPods, runningPods)
	}

	// 7. Uncordon remaining nodes and verify all pods get scheduled
	remainingNodesToUncordon := nodesToCordon[1:]
	fmt.Printf("✅ Uncordoning remaining agent nodes: %v\n", remainingNodesToUncordon)
	for _, nodeName := range remainingNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all pods to be scheduled and ready after final uncordon...\n")
	workloadConfig.Timeout = 15 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for scaled pods to be ready: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods after final uncordon: %v", err)
	}

	runningPods = 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running after scaling (expected all %d to be running)\n", runningPods, len(pods.Items))
	if runningPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be running after scaling, but only %d are running", len(pods.Items), runningPods)
	}
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCS scaling test completed successfully!\n")

	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
	}
}

// TestGangSchedulingWithPCSAndPCSGScalingFullReplicas verifies gang scheduling while scaling both PodCliqueSet and PodCliqueScalingGroup replicas
// Scenario GS-4:
// 1. Initialize a 28-node Grove cluster, then cordon 19 nodes
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node to allow scheduling and verify pods get scheduled
// 5. Wait for pods to become ready
// 6. Scale PCSG replicas to 3 and verify 4 new pending pods
// 7. Uncordon 4 nodes and verify scaled pods get scheduled
// 8. Scale PCS replicas to 2 and verify 10 new pending pods
// 9. Scale PCSG replicas to 3 and verify 4 new pending pods
// 10. Uncordon remaining nodes and verify all pods get scheduled
func Test_GS4_GangSchedulingWithPCSAndPCSGScalingFullReplicas(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 28)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCS+PCSG scaling (28 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 19 {
		t.Fatalf("expected at least 19 agent nodes to cordon, but found %d", len(agentNodes))
	}

	// cordon 19 nodes
	nodesToCordon := agentNodes[:19]
	fmt.Printf("🚫 Cordoning %d agent nodes: %v\n", len(nodesToCordon), nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL1, and verify 10 newly created pods
	workloadNamespace := "default"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload1"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload1.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute,
		PodLabelSelector: workloadLabelSelector,
	}

	fmt.Printf("🚀 Applying workload1.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	defer func() {
		fmt.Printf("🧹 Cleaning up applied resources...\n")
		for _, resource := range appliedResources {
			fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		}
	}()

	// 3. Verify all workload pods are pending due to insufficient resources
	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// 4. Uncordon 1 node to allow scheduling and verify pods get scheduled
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// 5. Wait for pods to become ready
	fmt.Printf("⏳ Waiting for all pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	runningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", runningPods, len(pods.Items))

	assertPodsOnDistinctNodes(t, pods.Items)

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// 6. Scale PCSG replicas to 3 and verify 4 new pending pods
	pcsgName := "workload1-0-sg-x"
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, pcsgName, 3, 14, 4)

	fmt.Printf("✅ PCSG %s scaled to 3 replicas with expected pending pods\n", pcsgName)

	// 7. Uncordon 4 nodes and verify scaled pods get scheduled
	remainingNodesAfterFirstUncordon := nodesToCordon[1:5]
	fmt.Printf("✅ Uncordoning nodes for PCSG scale readiness: %v\n", remainingNodesAfterFirstUncordon)
	for _, nodeName := range remainingNodesAfterFirstUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all pods to be scheduled and ready after PCSG scale...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for pods to be ready after PCSG scale: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
	if err != nil {
		t.Fatalf("Failed to list workload pods after PCSG scale: %v", err)
	}

	fmt.Printf("✅ Verified %d pods are running after PCSG scale\n", len(pods.Items))
	assertPodsOnDistinctNodes(t, pods.Items)

	// 8. Scale PCS replicas to 2 and verify 10 new pending pods
	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload1", 2, 24, 10)

	fmt.Printf("✅ PCS scaled to 2 replicas with expected pending pods\n")

	remainingNodesAfterPCSScale := nodesToCordon[5:15]
	fmt.Printf("✅ Uncordoning nodes for PCS scale readiness: %v\n", remainingNodesAfterPCSScale)
	for _, nodeName := range remainingNodesAfterPCSScale {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all pods to be scheduled and ready after PCS scale...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for pods to be ready after PCS scale: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
	if err != nil {
		t.Fatalf("Failed to list workload pods after PCS scale: %v", err)
	}

	fmt.Printf("✅ Verified %d pods are running after PCS scale\n", len(pods.Items))
	assertPodsOnDistinctNodes(t, pods.Items)

	// 9. Scale PCSG replicas to 3 and verify 4 new pending pods
	secondReplicaPCSGName := "workload1-1-sg-x"
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, secondReplicaPCSGName, 3, 28, 4)

	fmt.Printf("✅ PCSG %s scaled to 3 replicas with expected pending pods\n", secondReplicaPCSGName)

	// 10. Uncordon remaining nodes and verify all pods get scheduled
	finalNodes := nodesToCordon[15:19]
	fmt.Printf("✅ Uncordoning final nodes for PCSG scale readiness: %v\n", finalNodes)
	for _, nodeName := range finalNodes {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all pods to be scheduled and ready after final PCSG scale...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for pods to be ready after final PCSG scale: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
	if err != nil {
		t.Fatalf("Failed to list workload pods after final PCSG scale: %v", err)
	}

	fmt.Printf("✅ Verified %d pods are running after final PCSG scale\n", len(pods.Items))
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCS+PCSG scaling test completed successfully!\n")
}

func assertPodsOnDistinctNodes(t *testing.T, pods []v1.Pod) {
	t.Helper()

	assignedNodes := make(map[string]string, len(pods))
	podPlacements := make([]string, 0, len(pods))
	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			t.Fatalf("Pod %s is running but has no assigned node", pod.Name)
		}
		if existingPod, exists := assignedNodes[nodeName]; exists {
			t.Fatalf("Pods %s and %s are scheduled on the same node %s; expected unique nodes", existingPod, pod.Name, nodeName)
		}
		assignedNodes[nodeName] = pod.Name
		podPlacements = append(podPlacements, fmt.Sprintf("%s->%s", pod.Name, nodeName))
	}

	fmt.Println("✅ All pods scheduled on distinct nodes")
}

// pollForCondition polls a condition function until it returns true or times out
func pollForCondition(ctx context.Context, timeout, interval time.Duration, condition func() (bool, error)) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Check immediately first
	if satisfied, err := condition(); err != nil {
		return err
	} else if satisfied {
		return nil
	}

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("condition not met within timeout of %v", timeout)
		case <-ticker.C:
			if satisfied, err := condition(); err != nil {
				return err
			} else if satisfied {
				return nil
			}
		}
	}
}

func scalePCSGAndWait(t *testing.T, ctx context.Context, clientset kubernetes.Interface, dynamicClient dynamic.Interface, namespace, labelSelector, pcsgName string, replicas int32, expectedTotalPods, expectedPending int) {
	t.Helper()

	pcsgGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquescalinggroups"}
	patchBytes, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": replicas,
		},
	})
	if err != nil {
		t.Fatalf("Failed to marshal PCSG patch: %v", err)
	}

	fmt.Printf("📈 Scaling PodCliqueScalingGroup %s to replicas=%d...\n", pcsgName, replicas)
	if _, err := dynamicClient.Resource(pcsgGVR).Namespace(namespace).Patch(ctx, pcsgName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
		t.Fatalf("Failed to scale PodCliqueScalingGroup %s: %v", pcsgName, err)
	}

	fmt.Printf("🔄 Waiting for workload pods to be created after scaling PCSG %s (expect %d)...\n", pcsgName, expectedTotalPods)
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		fmt.Printf("Found %d workload pods after PCSG scaling (waiting for %d)\n", len(pods.Items), expectedTotalPods)
		return len(pods.Items) == expectedTotalPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods after PCSG scaling: %v", err)
	}

	evaluatePodStates(t, ctx, clientset, namespace, labelSelector, expectedTotalPods, expectedPending)
}

func scalePCSAndWait(t *testing.T, ctx context.Context, clientset kubernetes.Interface, dynamicClient dynamic.Interface, namespace, labelSelector, pcsName string, replicas int32, expectedTotalPods, expectedPending int) {
	t.Helper()

	pcsGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquesets"}
	patchBytes, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": replicas,
		},
	})
	if err != nil {
		t.Fatalf("Failed to marshal PCS patch: %v", err)
	}

	fmt.Printf("📈 Scaling PodCliqueSet %s to replicas=%d...\n", pcsName, replicas)
	if _, err := dynamicClient.Resource(pcsGVR).Namespace(namespace).Patch(ctx, pcsName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
		t.Fatalf("Failed to scale PodCliqueSet %s: %v", pcsName, err)
	}

	fmt.Printf("🔄 Waiting for workload pods to be created after scaling PCS %s (expect %d)...\n", pcsName, expectedTotalPods)
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		fmt.Printf("Found %d workload pods after PCS scaling (waiting for %d)\n", len(pods.Items), expectedTotalPods)
		return len(pods.Items) == expectedTotalPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods after PCS scaling: %v", err)
	}

	evaluatePodStates(t, ctx, clientset, namespace, labelSelector, expectedTotalPods, expectedPending)
}

func evaluatePodStates(t *testing.T, ctx context.Context, clientset kubernetes.Interface, namespace, labelSelector string, expectedTotalPods, expectedPending int) {
	t.Helper()

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	runningPods := 0
	pendingPods := 0
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodRunning:
			runningPods++
		case v1.PodPending:
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("📊 Pod states: %d running, %d pending (expected %d pending)\n", runningPods, pendingPods, expectedPending)

	if len(pods.Items) != expectedTotalPods {
		t.Fatalf("Expected %d total pods, but found %d", expectedTotalPods, len(pods.Items))
	}

	if pendingPods != expectedPending {
		t.Fatalf("Expected %d pending pods, but found %d", expectedPending, pendingPods)
	}

	if runningPods != expectedTotalPods-expectedPending {
		t.Fatalf("Expected %d running pods, but found %d", expectedTotalPods-expectedPending, runningPods)
	}
}

// Test_GS5_GangSchedulingWithMinReplicas tests gang-scheduling behavior with min-replicas
// Scenario GS-5:
// 1. Initialize a 10-node Grove cluster, then cordon 8 nodes
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 5. Wait for scheduled pods to become ready
// 6. Uncordon 7 nodes and verify all remaining workload pods get scheduled
func Test_GS5_GangSchedulingWithMinReplicas(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 10)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with min replicas (10 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 8 {
		t.Fatalf("expected at least 8 agent nodes to cordon, but found %d", len(agentNodes))
	}

	// Cordon 8 agent nodes
	nodesToCordon := agentNodes[:8]
	fmt.Printf("🚫 Cordoning 8 agent nodes: %v\n", nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL2, and verify 10 newly created pods
	// 2. Deploy workload WL2, and verify 10 newly created pods
	// Deploy workload2.yaml
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload2.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute, // Short timeout since we expect pods to be pending
		PodLabelSelector: "app.kubernetes.io/part-of=workload2",
	}

	fmt.Printf("🚀 Applying workload2.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	// workload2 creates: 1 PCS replica * (pc-a: 2 + pc-b: 1 + pc-c: 3) + sg-x: 2 replicas * (pc-b: 1 + pc-c: 3) = 6 + 8 = 14 pods
	// But the test description says 10 pods, so let me check the actual workload2 structure more carefully
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	if pendingPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be pending, but only %d are pending", len(pods.Items), pendingPods)
	}

	// Verify pods remain pending due to gang scheduling constraints
	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
	// Based on workload2 min-replicas: pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1}
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning 1 agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// Wait for exactly 3 pods to be scheduled (min-replicas)
	fmt.Printf("⏳ Waiting for exactly 3 pods to be scheduled (min-replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 3 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-3)

		// We expect exactly 3 pods to be running (min-replicas) and the rest pending
		return runningPods == 3 && pendingPods == len(pods.Items)-3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 3 pods to be scheduled: %v", err)
	}

	// Verify the scheduled pods and their distribution
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	runningPods := 0
	pendingPods = 0
	runningPodNames := make([]string, 0)
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodRunning:
			runningPods++
			runningPodNames = append(runningPodNames, pod.Name)
		case v1.PodPending:
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified exactly 3 pods are running: %v\n", runningPodNames)
	fmt.Printf("✅ Verified %d pods remain pending\n", pendingPods)

	if runningPods != 3 {
		t.Fatalf("Expected exactly 3 pods to be running (min-replicas), but found %d", runningPods)
	}

	if pendingPods != len(pods.Items)-3 {
		t.Fatalf("Expected %d pods to remain pending, but found %d", len(pods.Items)-3, pendingPods)
	}

	// 5. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 3 scheduled pods to become ready...\n")
	workloadConfig.Timeout = 5 * time.Minute
	// Note: WaitForPods waits for ALL pods, but we only want the running ones to be ready
	// We'll verify readiness manually
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/3\n", readyRunningPods)
		return readyRunningPods == 3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 3 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 3 scheduled pods are now ready\n")

	// 6. Uncordon 7 nodes and verify all remaining workload pods get scheduled
	remainingNodesToUncordon := nodesToCordon[1:]
	fmt.Printf("✅ Uncordoning remaining 7 agent nodes: %v\n", remainingNodesToUncordon)
	for _, nodeName := range remainingNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Final verification - all pods should be running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	finalRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			finalRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", finalRunningPods, len(pods.Items))

	if finalRunningPods != len(pods.Items) {
		t.Errorf("Expected all %d pods to be running, but only %d are running", len(pods.Items), finalRunningPods)
	}

	// Verify pods are distributed across distinct nodes
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling min-replicas test (GS-5) completed successfully! All workload pods transitioned correctly through min-replicas scheduling.\n")

	// Cleanup applied resources
	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		// Note: Cleanup is handled by the cluster cleanup function
	}
}

// Test_GS6_GangSchedulingWithPCSGScalingMinReplicas tests gang-scheduling behavior with PCSG scaling and min-replicas
// Scenario GS-6:
// 1. Initialize a 14-node Grove cluster, then cordon 12 nodes
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 5. Wait for scheduled pods to become ready
// 6. Uncordon 7 nodes and verify the remaining workload pods get scheduled
// 7. Wait for scheduled pods to become ready
// 8. Set pcs-0-sg-x resource replicas equal to 3, then verify 4 newly created pods
// 9. Verify all newly created pods are pending due to insufficient resources
// 10. Uncordon 2 nodes and verify 2 more pods get scheduled (pcs-0-{sg-x-2-pc-b=1, sg-x-2-pc-c=1})
// 11. Wait for scheduled pods to become ready
// 12. Uncordon 2 nodes and verify remaining workload pods get scheduled
func Test_GS6_GangSchedulingWithPCSGScalingMinReplicas(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 14)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCSG scaling and min replicas (14 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 12 {
		t.Fatalf("expected at least 12 agent nodes to cordon, but found %d", len(agentNodes))
	}

	// Cordon 12 agent nodes
	nodesToCordon := agentNodes[:12]
	fmt.Printf("🚫 Cordoning 12 agent nodes: %v\n", nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// Deploy workload2.yaml
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload2.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute, // Short timeout since we expect pods to be pending
		PodLabelSelector: "app.kubernetes.io/part-of=workload2",
	}

	fmt.Printf("🚀 Applying workload2.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	// workload2 initially creates 10 pods
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	if pendingPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be pending, but only %d are pending", len(pods.Items), pendingPods)
	}

	// Verify pods remain pending due to gang scheduling constraints
	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
	// Based on workload2 min-replicas: pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1}
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning 1 agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// Wait for exactly 3 pods to be scheduled (min-replicas)
	fmt.Printf("⏳ Waiting for exactly 3 pods to be scheduled (min-replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 3 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-3)

		// We expect exactly 3 pods to be running (min-replicas) and the rest pending
		return runningPods == 3 && pendingPods == len(pods.Items)-3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 3 pods to be scheduled: %v", err)
	}

	// Verify the scheduled pods and their distribution
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	runningPods := 0
	pendingPods = 0
	runningPodNames := make([]string, 0)
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodRunning:
			runningPods++
			runningPodNames = append(runningPodNames, pod.Name)
		case v1.PodPending:
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified exactly 3 pods are running: %v\n", runningPodNames)
	fmt.Printf("✅ Verified %d pods remain pending\n", pendingPods)

	if runningPods != 3 {
		t.Fatalf("Expected exactly 3 pods to be running (min-replicas), but found %d", runningPods)
	}

	if pendingPods != len(pods.Items)-3 {
		t.Fatalf("Expected %d pods to remain pending, but found %d", len(pods.Items)-3, pendingPods)
	}

	// 5. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 3 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/3\n", readyRunningPods)
		return readyRunningPods == 3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 3 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 3 scheduled pods are now ready\n")

	// 6. Uncordon 7 nodes and verify the remaining workload pods get scheduled
	sevenNodesToUncordon := nodesToCordon[1:8]
	fmt.Printf("✅ Uncordoning 7 agent nodes: %v\n", sevenNodesToUncordon)
	for _, nodeName := range sevenNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Verify all 10 initial pods are running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	allRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			allRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", allRunningPods, len(pods.Items))

	if allRunningPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be running, but only %d are running", len(pods.Items), allRunningPods)
	}

	fmt.Printf("✅ All initial workload pods are now ready\n")

	// 7. Wait for scheduled pods to become ready
	// Create dynamic client for PCSG scaling operations
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// 8. Set pcs-0-sg-x resource replicas equal to 3, then verify 4 newly created pods
	// Scale PCSG sg-x to 3 replicas and verify 4 newly created pods
	pcsgName := "workload2-0-sg-x"
	fmt.Printf("📈 Scaling PodCliqueScalingGroup %s to 3 replicas...\n", pcsgName)

	// Expected total pods after scaling: 10 (initial) + 4 (new from scaling sg-x from 2 to 3) = 14
	expectedPodsAfterScaling := 14
	expectedNewPendingPods := 4

	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadConfig.PodLabelSelector, pcsgName, 3, expectedPodsAfterScaling, expectedNewPendingPods)

	// 9. Verify all newly created pods are pending due to insufficient resources
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list pods after PCSG scaling: %v", err)
	}

	runningAfter := 0
	pendingAfter := 0
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodRunning:
			runningAfter++
		case v1.PodPending:
			pendingAfter++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("📊 Post-scaling pod states: %d running, %d pending (expected %d pending)\n", runningAfter, pendingAfter, expectedNewPendingPods)
	if len(pods.Items) != expectedPodsAfterScaling {
		t.Fatalf("Expected %d total pods after scaling, but found %d", expectedPodsAfterScaling, len(pods.Items))
	}
	if pendingAfter != expectedNewPendingPods {
		t.Fatalf("Expected %d pending pods after scaling, but found %d", expectedNewPendingPods, pendingAfter)
	}
	if runningAfter != expectedPodsAfterScaling-expectedNewPendingPods {
		t.Fatalf("Expected %d running pods after scaling, but found %d", expectedPodsAfterScaling-expectedNewPendingPods, runningAfter)
	}

	fmt.Printf("✅ PCSG %s scaled to 3 replicas with %d new pending pods\n", pcsgName, expectedNewPendingPods)

	// 10. Uncordon 2 nodes and verify 2 more pods get scheduled (pcs-0-{sg-x-2-pc-b=1, sg-x-2-pc-c=1})
	// Uncordon 2 nodes and verify exactly 2 more pods get scheduled
	// pcs-0-{sg-x-2-pc-b = 1, sg-x-2-pc-c = 1} (min-replicas for the new PCSG replica)
	twoNodesToUncordon := nodesToCordon[8:10]
	fmt.Printf("✅ Uncordoning 2 agent nodes: %v\n", twoNodesToUncordon)
	for _, nodeName := range twoNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for exactly 2 more pods to be scheduled (min-replicas for new PCSG replica)
	fmt.Printf("⏳ Waiting for exactly 2 more pods to be scheduled (min-replicas for new PCSG replica)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states after PCSG scaling: %d running, %d pending (expecting 12 running, 2 pending)\n",
			runningPods, pendingPods)

		// We expect 12 pods running (10 initial + 2 from min-replicas) and 2 pending
		return runningPods == 12 && pendingPods == 2, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 2 more pods to be scheduled after PCSG scaling: %v", err)
	}

	fmt.Printf("✅ Verified exactly 2 more pods are running after PCSG scaling\n")

	// 11. Wait for scheduled pods to become ready
	// Wait for the 2 newly scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 2 newly scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/12\n", readyRunningPods)
		return readyRunningPods == 12, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 12 pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 12 scheduled pods are now ready\n")

	// 12. Uncordon 2 nodes and verify remaining workload pods get scheduled
	// Uncordon remaining 2 nodes and verify all remaining workload pods get scheduled
	remainingNodesToUncordon := nodesToCordon[10:12]
	fmt.Printf("✅ Uncordoning remaining 2 agent nodes: %v\n", remainingNodesToUncordon)
	for _, nodeName := range remainingNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Final verification - all 14 pods should be running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	finalRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			finalRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", finalRunningPods, len(pods.Items))

	if finalRunningPods != expectedPodsAfterScaling {
		t.Errorf("Expected all %d pods to be running, but only %d are running", expectedPodsAfterScaling, finalRunningPods)
	}

	// Verify pods are distributed across distinct nodes
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCSG scaling min-replicas test (GS-6) completed successfully! All workload pods transitioned correctly through PCSG scaling with min-replicas.\n")

	// Cleanup applied resources
	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		// Note: Cleanup is handled by the cluster cleanup function
	}
}

// Test_GS7_GangSchedulingWithPCSGScalingMinReplicasAdvanced1 tests advanced gang-scheduling behavior with PCSG scaling and min-replicas
// Scenario GS-7:
// 1. Initialize a 14-node Grove cluster, then cordon 12 nodes
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 5. Wait for scheduled pods to become ready
// 6. Uncordon 2 nodes and verify 2 more pods get scheduled (pcs-0-{sg-x-1-pc-b=1, sg-x-1-pc-c=1})
// 7. Wait for scheduled pods to become ready
// 8. Uncordon 5 nodes and verify the remaining workload pods get scheduled
// 9. Wait for scheduled pods to become ready
// 10. Set pcs-0-sg-x resource replicas equal to 3, then verify 4 newly created pods
// 11. Verify all newly created pods are pending due to insufficient resources
// 12. Uncordon 2 nodes and verify 2 more pods get scheduled (pcs-0-{sg-x-2-pc-b=1, sg-x-2-pc-c=1})
// 13. Wait for scheduled pods to become ready
// 14. Uncordon 2 nodes and verify remaining workload pods get scheduled
func Test_GS7_GangSchedulingWithPCSGScalingMinReplicasAdvanced1(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 14)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCSG scaling min replicas advanced1 (14 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 12 {
		t.Fatalf("expected at least 12 agent nodes to cordon, but found %d", len(agentNodes))
	}

	// Cordon 12 agent nodes
	nodesToCordon := agentNodes[:12]
	fmt.Printf("🚫 Cordoning 12 agent nodes: %v\n", nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL2, and verify 10 newly created pods
	// Deploy workload2.yaml
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload2.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute, // Short timeout since we expect pods to be pending
		PodLabelSelector: "app.kubernetes.io/part-of=workload2",
	}

	fmt.Printf("🚀 Applying workload2.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	// workload2 initially creates 10 pods
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	if pendingPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be pending, but only %d are pending", len(pods.Items), pendingPods)
	}

	// Verify pods remain pending due to gang scheduling constraints
	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
	// Based on workload2 min-replicas: pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1}
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning 1 agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// Wait for exactly 3 pods to be scheduled (min-replicas)
	fmt.Printf("⏳ Waiting for exactly 3 pods to be scheduled (min-replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 3 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-3)

		// We expect exactly 3 pods to be running (min-replicas) and the rest pending
		return runningPods == 3 && pendingPods == len(pods.Items)-3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 3 pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified exactly 3 pods are running (min-replicas)\n")

	// 5. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 3 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/3\n", readyRunningPods)
		return readyRunningPods == 3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 3 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 3 scheduled pods are now ready\n")

	// 6. Uncordon 2 nodes and verify 2 more pods get scheduled (pcs-0-{sg-x-1-pc-b=1, sg-x-1-pc-c=1})
	twoNodesToUncordon := nodesToCordon[1:3]
	fmt.Printf("✅ Uncordoning 2 agent nodes: %v\n", twoNodesToUncordon)
	for _, nodeName := range twoNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for exactly 2 more pods to be scheduled (sg-x-1 min-replicas)
	fmt.Printf("⏳ Waiting for exactly 2 more pods to be scheduled (sg-x-1 min-replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 5 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-5)

		// We expect 5 pods running (3 + 2 new) and the rest pending
		return runningPods == 5 && pendingPods == len(pods.Items)-5, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 2 more pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified exactly 2 more pods are running (sg-x-1 min-replicas)\n")

	// 7. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 5 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/5\n", readyRunningPods)
		return readyRunningPods == 5, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 5 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 5 scheduled pods are now ready\n")

	// 8. Uncordon 5 nodes and verify the remaining workload pods get scheduled
	fiveNodesToUncordon := nodesToCordon[3:8]
	fmt.Printf("✅ Uncordoning 5 agent nodes: %v\n", fiveNodesToUncordon)
	for _, nodeName := range fiveNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Verify all 10 initial pods are running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	allRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			allRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", allRunningPods, len(pods.Items))

	if allRunningPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be running, but only %d are running", len(pods.Items), allRunningPods)
	}

	fmt.Printf("✅ All initial workload pods are now ready\n")

	// 9. Wait for scheduled pods to become ready (already verified above)

	// Create dynamic client for PCSG scaling operations
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// 10. Set pcs-0-sg-x resource replicas equal to 3, then verify 4 newly created pods
	// Scale PCSG sg-x to 3 replicas and verify 4 newly created pods
	pcsgName := "workload2-0-sg-x"
	fmt.Printf("📈 Scaling PodCliqueScalingGroup %s to 3 replicas...\n", pcsgName)

	// Expected total pods after scaling: 10 (initial) + 4 (new from scaling sg-x from 2 to 3) = 14
	expectedPodsAfterScaling := 14
	expectedNewPendingPods := 4

	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadConfig.PodLabelSelector, pcsgName, 3, expectedPodsAfterScaling, expectedNewPendingPods)

	fmt.Printf("✅ PCSG %s scaled to 3 replicas with %d new pending pods\n", pcsgName, expectedNewPendingPods)

	// 11. Verify all newly created pods are pending due to insufficient resources (verified in scalePCSGAndWait)

	// 12. Uncordon 2 nodes and verify 2 more pods get scheduled (pcs-0-{sg-x-2-pc-b=1, sg-x-2-pc-c=1})
	// Uncordon 2 nodes and verify exactly 2 more pods get scheduled
	// pcs-0-{sg-x-2-pc-b = 1, sg-x-2-pc-c = 1} (min-replicas for the new PCSG replica)
	twoMoreNodesToUncordon := nodesToCordon[8:10]
	fmt.Printf("✅ Uncordoning 2 agent nodes: %v\n", twoMoreNodesToUncordon)
	for _, nodeName := range twoMoreNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for exactly 2 more pods to be scheduled (min-replicas for new PCSG replica)
	fmt.Printf("⏳ Waiting for exactly 2 more pods to be scheduled (min-replicas for new PCSG replica)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states after PCSG scaling: %d running, %d pending (expecting 12 running, 2 pending)\n",
			runningPods, pendingPods)

		// We expect 12 pods running (10 initial + 2 from min-replicas) and 2 pending
		return runningPods == 12 && pendingPods == 2, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 2 more pods to be scheduled after PCSG scaling: %v", err)
	}

	fmt.Printf("✅ Verified exactly 2 more pods are running after PCSG scaling\n")

	// 13. Wait for scheduled pods to become ready
	// Wait for the 2 newly scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 2 newly scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/12\n", readyRunningPods)
		return readyRunningPods == 12, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 12 pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 12 scheduled pods are now ready\n")

	// 14. Uncordon 2 nodes and verify remaining workload pods get scheduled
	// Uncordon remaining 2 nodes and verify all remaining workload pods get scheduled
	remainingNodesToUncordon := nodesToCordon[10:12]
	fmt.Printf("✅ Uncordoning remaining 2 agent nodes: %v\n", remainingNodesToUncordon)
	for _, nodeName := range remainingNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Final verification - all 14 pods should be running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	finalRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			finalRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", finalRunningPods, len(pods.Items))

	if finalRunningPods != expectedPodsAfterScaling {
		t.Errorf("Expected all %d pods to be running, but only %d are running", expectedPodsAfterScaling, finalRunningPods)
	}

	// Verify pods are distributed across distinct nodes
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCSG scaling min-replicas advanced1 test (GS-7) completed successfully! All workload pods transitioned correctly through advanced PCSG scaling with min-replicas.\n")

	// Cleanup applied resources
	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		// Note: Cleanup is handled by the cluster cleanup function
	}
}

// TestGangSchedulingWithPCSGScalingMinReplicasAdvanced2 tests advanced gang-scheduling behavior with early PCSG scaling and min-replicas
// Scenario GS-8:
// 1. Initialize a 14-node Grove cluster, then cordon 12 nodes
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Set pcs-0-sg-x resource replicas equal to 3, verify 4 more newly created pods
// 5. Verify all 14 newly created pods are pending due to insufficient resources
// 6. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 7. Wait for scheduled pods to become ready
// 8. Uncordon 4 nodes and verify 4 more pods get scheduled (pcs-0-{sg-x-1-pc-b=1, sg-x-1-pc-c=1}, pcs-0-{sg-x-2-pc-b=1, sg-x-2-pc-c=1})
// 9. Wait for scheduled pods to become ready
// 10. Uncordon 7 nodes and verify the remaining workload pods get scheduled
func Test_GS8_GangSchedulingWithPCSGScalingMinReplicasAdvanced2(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 14)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCSG scaling min replicas advanced2 (14 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 12 {
		t.Fatalf("expected at least 12 agent nodes to cordon, but found %d", len(agentNodes))
	}

	// Cordon 12 agent nodes
	nodesToCordon := agentNodes[:12]
	fmt.Printf("🚫 Cordoning 12 agent nodes: %v\n", nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL2, and verify 10 newly created pods
	// Deploy workload2.yaml
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload2.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute, // Short timeout since we expect pods to be pending
		PodLabelSelector: "app.kubernetes.io/part-of=workload2",
	}

	fmt.Printf("🚀 Applying workload2.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	// workload2 initially creates 10 pods
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	if pendingPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be pending, but only %d are pending", len(pods.Items), pendingPods)
	}

	// Verify pods remain pending due to gang scheduling constraints
	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// Create dynamic client for PCSG scaling operations
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// 4. Set pcs-0-sg-x resource replicas equal to 3, verify 4 more newly created pods
	pcsgName := "workload2-0-sg-x"
	fmt.Printf("📈 Scaling PodCliqueScalingGroup %s to 3 replicas before uncordoning...\n", pcsgName)

	// Expected total pods after scaling: 10 (initial) + 4 (new from scaling sg-x from 2 to 3) = 14
	expectedPodsAfterScaling := 14

	scalePatch := map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": 3,
		},
	}
	patchBytes, err := json.Marshal(scalePatch)
	if err != nil {
		t.Fatalf("Failed to marshal scale patch: %v", err)
	}

	pcsgGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquescalinggroups"}
	if _, err := dynamicClient.Resource(pcsgGVR).Namespace(workloadNamespace).Patch(ctx, pcsgName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
		t.Fatalf("Failed to scale PodCliqueScalingGroup %s: %v", pcsgName, err)
	}

	fmt.Printf("🔄 Waiting for workload pods to be created after scaling (expect %d)...\n", expectedPodsAfterScaling)
	err = pollForCondition(ctx, 3*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods after scaling (waiting for %d)\n", len(pods.Items), expectedPodsAfterScaling)
		return len(pods.Items) == expectedPodsAfterScaling, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for scaled pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods after scaling as expected\n", len(pods.Items))

	// 5. Verify all 14 newly created pods are pending due to insufficient resources
	pendingPods = 0
	runningPods := 0
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodPending:
			pendingPods++
		case v1.PodRunning:
			runningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("📊 Post-scaling pod states: %d running, %d pending (expected 0 running, %d pending)\n", runningPods, pendingPods, expectedPodsAfterScaling)

	if len(pods.Items) != expectedPodsAfterScaling {
		t.Fatalf("Expected %d total pods after scaling, but found %d", expectedPodsAfterScaling, len(pods.Items))
	}
	if pendingPods != expectedPodsAfterScaling {
		t.Fatalf("Expected all %d pods to be pending after scaling, but found %d pending", expectedPodsAfterScaling, pendingPods)
	}
	if runningPods != 0 {
		t.Fatalf("Expected 0 running pods after scaling with all nodes cordoned, but found %d running", runningPods)
	}

	fmt.Printf("✅ Verified all %d pods are pending after PCSG scaling (gang scheduling working correctly)\n", len(pods.Items))

	// 6. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning 1 agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// Wait for exactly 3 pods to be scheduled (min-replicas)
	fmt.Printf("⏳ Waiting for exactly 3 pods to be scheduled (min-replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 3 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-3)

		// We expect exactly 3 pods to be running (min-replicas) and the rest pending
		return runningPods == 3 && pendingPods == len(pods.Items)-3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 3 pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified exactly 3 pods are running (min-replicas)\n")

	// 7. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 3 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/3\n", readyRunningPods)
		return readyRunningPods == 3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 3 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 3 scheduled pods are now ready\n")

	// 8. Uncordon 4 nodes and verify 4 more pods get scheduled
	fourNodesToUncordon := nodesToCordon[1:5]
	fmt.Printf("✅ Uncordoning 4 agent nodes: %v\n", fourNodesToUncordon)
	for _, nodeName := range fourNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for exactly 4 more pods to be scheduled (sg-x-1 and sg-x-2 min-replicas)
	fmt.Printf("⏳ Waiting for exactly 4 more pods to be scheduled (sg-x-1 and sg-x-2 min-replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 7 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-7)

		// We expect 7 pods running (3 + 4 new) and the rest pending
		return runningPods == 7 && pendingPods == len(pods.Items)-7, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 4 more pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified exactly 4 more pods are running (sg-x-1 and sg-x-2 min-replicas)\n")

	// 9. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 7 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/7\n", readyRunningPods)
		return readyRunningPods == 7, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 7 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 7 scheduled pods are now ready\n")

	// 10. Uncordon 7 nodes and verify the remaining workload pods get scheduled
	remainingNodesToUncordon := nodesToCordon[5:]
	fmt.Printf("✅ Uncordoning remaining 7 agent nodes: %v\n", remainingNodesToUncordon)
	for _, nodeName := range remainingNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Final verification - all 14 pods should be running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	finalRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			finalRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", finalRunningPods, len(pods.Items))

	if finalRunningPods != expectedPodsAfterScaling {
		t.Errorf("Expected all %d pods to be running, but only %d are running", expectedPodsAfterScaling, finalRunningPods)
	}

	// Verify pods are distributed across distinct nodes
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCSG scaling min-replicas advanced2 test (GS-8) completed successfully! All workload pods transitioned correctly through early PCSG scaling with min-replicas.\n")

	// Cleanup applied resources
	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		// Note: Cleanup is handled by the cluster cleanup function
	}
}

// TestGangSchedulingWithPCSScalingMinReplicas tests gang-scheduling behavior with PodCliqueSet scaling and min-replicas
// Scenario GS-9:
// 1. Initialize a 20-node Grove cluster, then cordon 18 nodes
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 5. Wait for scheduled pods to become ready
// 6. Uncordon 7 nodes and verify the remaining workload pods get scheduled
// 7. Wait for scheduled pods to become ready
// 8. Set PCS resource replicas equal to 2, then verify 10 more newly created pods
// 9. Uncordon 3 nodes and verify another 3 pods get scheduled (pcs-1-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 10. Wait for scheduled pods to become ready
// 11. Uncordon 7 nodes and verify the remaining workload pods get scheduled
func Test_GS9_GangSchedulingWithPCSScalingMinReplicas(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 20)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCS scaling min replicas (20 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 18 {
		t.Fatalf("expected at least 18 agent nodes to cordon, but found %d", len(agentNodes))
	}

	// Cordon 18 agent nodes
	nodesToCordon := agentNodes[:18]
	fmt.Printf("🚫 Cordoning 18 agent nodes: %v\n", nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL2, and verify 10 newly created pods
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload2.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute, // Short timeout since we expect pods to be pending
		PodLabelSelector: "app.kubernetes.io/part-of=workload2",
	}

	fmt.Printf("🚀 Applying workload2.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	// workload2 initially creates 10 pods
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	if pendingPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be pending, but only %d are pending", len(pods.Items), pendingPods)
	}

	// Verify pods remain pending due to gang scheduling constraints
	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning 1 agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// Wait for exactly 3 pods to be scheduled (min-replicas)
	fmt.Printf("⏳ Waiting for exactly 3 pods to be scheduled (min-replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 3 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-3)

		// We expect exactly 3 pods to be running (min-replicas) and the rest pending
		return runningPods == 3 && pendingPods == len(pods.Items)-3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 3 pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified exactly 3 pods are running (min-replicas)\n")

	// 5. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 3 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/3\n", readyRunningPods)
		return readyRunningPods == 3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 3 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 3 scheduled pods are now ready\n")

	// 6. Uncordon 7 nodes and verify the remaining workload pods get scheduled
	sevenNodesToUncordon := nodesToCordon[1:8]
	fmt.Printf("✅ Uncordoning 7 agent nodes: %v\n", sevenNodesToUncordon)
	for _, nodeName := range sevenNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Verify all 10 initial pods are running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	allRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			allRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", allRunningPods, len(pods.Items))

	if allRunningPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be running, but only %d are running", len(pods.Items), allRunningPods)
	}

	fmt.Printf("✅ All initial workload pods are now ready\n")

	// 7. Wait for scheduled pods to become ready (already verified above)
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// 8. Set PCS resource replicas equal to 2, then verify 10 more newly created pods
	// Scale PodCliqueSet to 2 replicas and verify 10 more newly created pods
	pcsName := "workload2"
	fmt.Printf("📈 Scaling PodCliqueSet %s to 2 replicas...\n", pcsName)

	// Expected total pods after scaling: 10 (initial) + 10 (new from scaling PCS from 1 to 2) = 20
	expectedPodsAfterScaling := 20
	expectedNewPendingPods := 10

	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadConfig.PodLabelSelector, pcsName, 2, expectedPodsAfterScaling, expectedNewPendingPods)

	fmt.Printf("✅ PCS %s scaled to 2 replicas with %d new pending pods\n", pcsName, expectedNewPendingPods)

	// 9. Uncordon 3 nodes and verify another 3 pods get scheduled (pcs-1-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
	threeNodesToUncordon := nodesToCordon[8:11]
	fmt.Printf("✅ Uncordoning 3 agent nodes: %v\n", threeNodesToUncordon)
	for _, nodeName := range threeNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for exactly 3 more pods to be scheduled (min-replicas for new PCS replica)
	fmt.Printf("⏳ Waiting for exactly 3 more pods to be scheduled (min-replicas for new PCS replica)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states after PCS scaling: %d running, %d pending (expecting 13 running, 7 pending)\n",
			runningPods, pendingPods)

		// We expect 13 pods running (10 initial + 3 from min-replicas) and 7 pending
		return runningPods == 13 && pendingPods == 7, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 3 more pods to be scheduled after PCS scaling: %v", err)
	}

	fmt.Printf("✅ Verified exactly 3 more pods are running after PCS scaling\n")

	// 10. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 3 newly scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/13\n", readyRunningPods)
		return readyRunningPods == 13, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 13 pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 13 scheduled pods are now ready\n")

	// 11. Uncordon 7 nodes and verify the remaining workload pods get scheduled
	remainingNodesToUncordon := nodesToCordon[11:18]
	fmt.Printf("✅ Uncordoning remaining 7 agent nodes: %v\n", remainingNodesToUncordon)
	for _, nodeName := range remainingNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Final verification - all 20 pods should be running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	finalRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			finalRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", finalRunningPods, len(pods.Items))

	if finalRunningPods != expectedPodsAfterScaling {
		t.Errorf("Expected all %d pods to be running, but only %d are running", expectedPodsAfterScaling, finalRunningPods)
	}

	// Verify pods are distributed across distinct nodes
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCS scaling min-replicas test (GS-9) completed successfully! All workload pods transitioned correctly through PCS scaling with min-replicas.\n")

	// Cleanup applied resources
	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		// Note: Cleanup is handled by the cluster cleanup function
	}
}

// Test_GS10_GangSchedulingWithPCSScalingMinReplicasAdvanced tests advanced gang-scheduling behavior with early PCS scaling and min-replicas
// Scenario GS-10:
// 1. Initialize a 20-node Grove cluster, then cordon 18 nodes
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Set PCS resource replicas equal to 2, then verify 10 more newly created pods
// 5. Verify all 20 newly created pods are pending due to insufficient resources
// 6. Uncordon 4 nodes and verify a total of 6 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1}, pcs-1-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 7. Wait for scheduled pods to become ready
// 8. Uncordon 4 nodes and verify 4 more pods get scheduled (pcs-0-{sg-x-1-pc-b=1, sg-x-1-pc-c=1}, pcs-1-{sg-x-1-pc-b=1, sg-x-1-pc-c=1})
// 9. Wait for scheduled pods to become ready
// 10. Uncordon 10 nodes and verify the remaining workload pods get scheduled
func Test_GS10_GangSchedulingWithPCSScalingMinReplicasAdvanced(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 20)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCS scaling min replicas advanced (20 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 18 {
		t.Fatalf("expected at least 18 agent nodes to cordon, but found %d", len(agentNodes))
	}

	// Cordon 18 agent nodes
	nodesToCordon := agentNodes[:18]
	fmt.Printf("🚫 Cordoning 18 agent nodes: %v\n", nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL2, and verify 10 newly created pods
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload2.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute, // Short timeout since we expect pods to be pending
		PodLabelSelector: "app.kubernetes.io/part-of=workload2",
	}

	fmt.Printf("🚀 Applying workload2.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
	// workload2 initially creates 10 pods
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	if pendingPods != len(pods.Items) {
		t.Fatalf("Expected all %d pods to be pending, but only %d are pending", len(pods.Items), pendingPods)
	}

	// Verify pods remain pending due to gang scheduling constraints
	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Still pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// Create dynamic client for PCS scaling operations
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// 4. Set PCS resource replicas equal to 2, then verify 10 more newly created pods
	pcsName := "workload2"
	fmt.Printf("📈 Scaling PodCliqueSet %s to 2 replicas before uncordoning...\n", pcsName)

	// Expected total pods after scaling: 10 (initial) + 10 (new from scaling PCS from 1 to 2) = 20
	expectedPodsAfterScaling := 20

	scalePatch := map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": 2,
		},
	}
	patchBytes, err := json.Marshal(scalePatch)
	if err != nil {
		t.Fatalf("Failed to marshal scale patch: %v", err)
	}

	pcsGVR := schema.GroupVersionResource{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquesets"}
	if _, err := dynamicClient.Resource(pcsGVR).Namespace(workloadNamespace).Patch(ctx, pcsName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
		t.Fatalf("Failed to scale PodCliqueSet %s: %v", pcsName, err)
	}

	fmt.Printf("🔄 Waiting for workload pods to be created after scaling (expect %d)...\n", expectedPodsAfterScaling)
	err = pollForCondition(ctx, 3*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods after scaling (waiting for %d)\n", len(pods.Items), expectedPodsAfterScaling)
		return len(pods.Items) == expectedPodsAfterScaling, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for scaled pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods after scaling as expected\n", len(pods.Items))

	// 5. Verify all 20 newly created pods are pending due to insufficient resources
	pendingPods = 0
	runningPods := 0
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodPending:
			pendingPods++
		case v1.PodRunning:
			runningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("📊 Post-scaling pod states: %d running, %d pending (expected 0 running, %d pending)\n", runningPods, pendingPods, expectedPodsAfterScaling)

	if len(pods.Items) != expectedPodsAfterScaling {
		t.Fatalf("Expected %d total pods after scaling, but found %d", expectedPodsAfterScaling, len(pods.Items))
	}
	if pendingPods != expectedPodsAfterScaling {
		t.Fatalf("Expected all %d pods to be pending after scaling, but found %d pending", expectedPodsAfterScaling, pendingPods)
	}
	if runningPods != 0 {
		t.Fatalf("Expected 0 running pods after scaling with all nodes cordoned, but found %d running", runningPods)
	}

	fmt.Printf("✅ Verified all %d pods are pending after PCS scaling (gang scheduling working correctly)\n", len(pods.Items))

	// 6. Uncordon 4 nodes and verify a total of 6 pods get scheduled
	fourNodesToUncordon := nodesToCordon[0:4]
	fmt.Printf("✅ Uncordoning 4 agent nodes: %v\n", fourNodesToUncordon)
	for _, nodeName := range fourNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for exactly 6 pods to be scheduled (min-replicas for both PCS replicas)
	fmt.Printf("⏳ Waiting for exactly 6 pods to be scheduled (min-replicas for both PCS replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 6 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-6)

		// We expect exactly 6 pods to be running (min-replicas for both PCS replicas) and the rest pending
		return runningPods == 6 && pendingPods == len(pods.Items)-6, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 6 pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified exactly 6 pods are running (min-replicas for both PCS replicas)\n")

	// 7. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 6 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/6\n", readyRunningPods)
		return readyRunningPods == 6, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 6 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 6 scheduled pods are now ready\n")

	// 8. Uncordon 4 nodes and verify 4 more pods get scheduled
	fourMoreNodesToUncordon := nodesToCordon[4:8]
	fmt.Printf("✅ Uncordoning 4 more agent nodes: %v\n", fourMoreNodesToUncordon)
	for _, nodeName := range fourMoreNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for exactly 4 more pods to be scheduled (sg-x-1 for both PCS replicas)
	fmt.Printf("⏳ Waiting for exactly 4 more pods to be scheduled (sg-x-1 for both PCS replicas)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		fmt.Printf("Pod states: %d running, %d pending (expecting 10 running, %d pending)\n",
			runningPods, pendingPods, len(pods.Items)-10)

		// We expect 10 pods running (6 + 4 new) and the rest pending
		return runningPods == 10 && pendingPods == len(pods.Items)-10, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for exactly 4 more pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified exactly 4 more pods are running (sg-x-1 for both PCS replicas)\n")

	// 9. Wait for scheduled pods to become ready
	fmt.Printf("⏳ Waiting for the 10 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: workloadConfig.PodLabelSelector,
		})
		if err != nil {
			return false, err
		}

		readyRunningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				// Check if pod is ready
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyRunningPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready running pods: %d/10\n", readyRunningPods)
		return readyRunningPods == 10, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 10 scheduled pods to become ready: %v", err)
	}

	fmt.Printf("✅ All 10 scheduled pods are now ready\n")

	// 10. Uncordon 10 nodes and verify the remaining workload pods get scheduled
	remainingNodesToUncordon := nodesToCordon[8:18]
	fmt.Printf("✅ Uncordoning remaining 10 agent nodes: %v\n", remainingNodesToUncordon)
	for _, nodeName := range remainingNodesToUncordon {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// Wait for all remaining pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all remaining workload pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all pods to be ready: %v", err)
	}

	// Final verification - all 20 pods should be running
	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: workloadConfig.PodLabelSelector,
	})
	if err != nil {
		t.Fatalf("Failed to list workload pods: %v", err)
	}

	finalRunningPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			finalRunningPods++
		}
		t.Logf("Pod %s: Phase=%s, Node=%s", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", finalRunningPods, len(pods.Items))

	if finalRunningPods != expectedPodsAfterScaling {
		t.Errorf("Expected all %d pods to be running, but only %d are running", expectedPodsAfterScaling, finalRunningPods)
	}

	// Verify pods are distributed across distinct nodes
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCS scaling min-replicas advanced test (GS-10) completed successfully! All workload pods transitioned correctly through early PCS scaling with min-replicas.\n")

	// Cleanup applied resources
	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		// Note: Cleanup is handled by the cluster cleanup function
	}
}

// Test_GS11_GangSchedulingWithPCSAndPCSGScalingMinReplicas tests gang-scheduling behavior with both PCS and PCSG scaling using min-replicas
// Scenario GS-11:
// 1. Initialize a 28-node Grove cluster, then cordon 26 nodes
// 2. Deploy workload WL6, and verify 6 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node and verify a total of 3 pods get scheduled (pcs-0-{pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 5. Scale pcs-0-sg-x replicas to 3, verify 4 newly created pods
// 6. Verify all newly created pods are pending due to insufficient resources
// 7. Scale pcs-0 replicas to 3, verify 6 newly created pods
// 8. Verify all newly created pods are pending due to insufficient resources
// 9. Uncordon 2 nodes
// 10. Verify a total of 9 pods get scheduled (pcs-0, pcs-1, pcs-2 each with {pc-a=1, sg-x-0-pc-b=1, sg-x-0-pc-c=1})
// 11. Scale pcs-0-sg-x replicas to 5, verify 8 newly created pods
// 12. Verify all newly created pods are pending due to insufficient resources
// 13. Scale pcs-1-sg-x replicas to 5, verify 8 newly created pods
// 14. Verify all newly created pods are pending due to insufficient resources
// 15. Scale pcs-2-sg-x replicas to 5, verify 8 newly created pods
// 16. Verify all newly created pods are pending due to insufficient resources
// 17. Uncordon 6 nodes
// 18. Verify all pods get scheduled

// Test_GS11_GangSchedulingWithPCSAndPCSGScalingMinReplicas tests gang-scheduling behavior with both PCS and PCSG scaling using min-replicas
// Scenario GS-11:
// 1. Initialize a 28-node Grove cluster, then cordon 26 nodes
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Uncordon 1 node
// 5. Wait for min-replicas pods to be scheduled and ready (should be 3 pods for min-available)
// 6. Uncordon 7 nodes and verify the remaining workload pods get scheduled
// 7. Set pcs-0-sg-x resource replicas equal to 3, then verify 4 newly created pods
// 8. Verify all newly created pods are pending due to insufficient resources
// 9. Uncordon 2 nodes
// 10. Wait for 2 more pods to be scheduled and ready (min-available for sg-x-2)
// 11. Uncordon 2 nodes and verify remaining workload pods get scheduled
// 12. Set pcs resource replicas equal to 2, then verify 10 more newly created pods
// 13. Uncordon 3 nodes
// 14. Wait for 3 more pods to be scheduled (min-available for pcs-1)
// 15. Uncordon 7 nodes and verify the remaining workload pods get scheduled
// 16. Set pcs-1-sg-x resource replicas equal to 3, then verify 4 newly created pods
// 17. Verify all newly created pods are pending due to insufficient resources
// 18. Uncordon 2 nodes
// 19. Wait for 2 more pods to be scheduled (min-available for pcs-1-sg-x-2)
// 20. Uncordon 2 nodes and verify remaining workload pods get scheduled
func Test_GS11_GangSchedulingWithPCSAndPCSGScalingMinReplicas(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 28)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with PCS+PCSG scaling min replicas (28 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 26 {
		t.Fatalf("expected at least 26 agent nodes to cordon, but found %d", len(agentNodes))
	}

	nodesToCordon := agentNodes[:26]
	fmt.Printf("🚫 Cordoning %d agent nodes: %v\n", len(nodesToCordon), nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL2, and verify 10 newly created pods
	workloadNamespace := "default"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload2"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload2.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute,
		PodLabelSelector: workloadLabelSelector,
	}

	fmt.Printf("🚀 Applying workload2.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	defer func() {
		fmt.Printf("🧹 Cleaning up applied resources...\n")
		for _, resource := range appliedResources {
			fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		}
	}()

	fmt.Printf("🔍 Polling for pods to be created...\n")
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	fmt.Printf("🔍 Verifying all workload pods are pending due to insufficient resources...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified all workload pods are pending (gang scheduling working correctly)\n")

	// 4. Uncordon 1 node
	firstNodeToUncordon := nodesToCordon[0]
	fmt.Printf("✅ Uncordoning agent node: %s\n", firstNodeToUncordon)
	if err := cordonNode(ctx, clientset, firstNodeToUncordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", firstNodeToUncordon, err)
	}

	// 5. Wait for min-replicas pods to be scheduled and ready (should be 3 pods for min-available)
	fmt.Printf("⏳ Waiting for min-replicas pods to be scheduled and ready (expecting 3 pods)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			}
		}

		fmt.Printf("Running pods: %d (waiting for 3)\n", runningPods)
		return runningPods == 3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for min-replicas pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified 3 pods are now running (min-available scheduling)\n")

	// 6. Uncordon 7 nodes and verify the remaining workload pods get scheduled
	remainingNodesFirstWave := nodesToCordon[1:8]
	fmt.Printf("✅ Uncordoning nodes for first wave completion: %v\n", remainingNodesFirstWave)
	for _, nodeName := range remainingNodesFirstWave {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all first wave pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for first wave pods to be ready: %v", err)
	}

	fmt.Printf("✅ Verified all first wave pods are running\n")

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// 7. Set pcs-0-sg-x resource replicas equal to 3, then verify 4 newly created pods
	pcsgName := "workload2-0-sg-x"
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, pcsgName, 3, 14, 4)

	fmt.Printf("✅ PCSG %s scaled to 3 replicas with expected pending pods\n", pcsgName)

	// 8. Verify all newly created pods are pending due to insufficient resources
	fmt.Printf("🔍 Verifying all newly created pods are pending due to insufficient resources...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		totalPods := len(pods.Items)
		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		expectedRunning := 10 // Initial 10 pods from first wave
		expectedPending := 4  // 4 new pods from PCSG scaling
		fmt.Printf("Pod states after PCSG scaling: %d running, %d pending (expected %d running, %d pending)\n",
			runningPods, pendingPods, expectedRunning, expectedPending)

		return totalPods == 14 && runningPods == expectedRunning && pendingPods == expectedPending, nil
	})
	if err != nil {
		t.Fatalf("Failed to verify newly created pods are pending: %v", err)
	}

	fmt.Printf("✅ Verified all newly created pods are pending due to insufficient resources\n")

	// 9. Uncordon 2 nodes
	remainingNodesSecondWave := nodesToCordon[8:10]
	fmt.Printf("✅ Uncordoning nodes for PCSG partial scheduling: %v\n", remainingNodesSecondWave)
	for _, nodeName := range remainingNodesSecondWave {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// 10. Wait for 2 more pods to be scheduled and ready (min-available for sg-x-2)
	fmt.Printf("⏳ Waiting for 2 more pods to be scheduled and ready...\n")
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			}
		}

		fmt.Printf("Running pods: %d (waiting for 12)\n", runningPods)
		return runningPods == 12, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for PCSG partial scheduling: %v", err)
	}

	fmt.Printf("✅ Verified 2 more pods are running (min-available for sg-x-2)\n")

	// 11. Uncordon 2 nodes and verify remaining workload pods get scheduled
	remainingNodesThirdWave := nodesToCordon[10:12]
	fmt.Printf("✅ Uncordoning nodes for PCSG completion: %v\n", remainingNodesThirdWave)
	for _, nodeName := range remainingNodesThirdWave {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all PCSG pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for PCSG completion pods to be ready: %v", err)
	}

	fmt.Printf("✅ Verified all PCSG scaling pods are running\n")

	// 12. Set pcs resource replicas equal to 2, then verify 10 more newly created pods
	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload2", 2, 24, 10)

	fmt.Printf("✅ PCS scaled to 2 replicas with expected pending pods\n")

	// 13. Uncordon 3 nodes
	remainingNodesFourthWave := nodesToCordon[12:15]
	fmt.Printf("✅ Uncordoning nodes for PCS partial scheduling: %v\n", remainingNodesFourthWave)
	for _, nodeName := range remainingNodesFourthWave {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// 14. Wait for 3 more pods to be scheduled (min-available for pcs-1)
	fmt.Printf("⏳ Waiting for 3 more pods to be scheduled for PCS partial...\n")
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			}
		}

		fmt.Printf("Running pods: %d (waiting for 17)\n", runningPods)
		return runningPods == 17, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for PCS partial scheduling: %v", err)
	}

	fmt.Printf("✅ Verified 3 more pods are running (min-available for pcs-1)\n")

	// 15. Uncordon 7 nodes and verify the remaining workload pods get scheduled
	remainingNodesFifthWave := nodesToCordon[15:22]
	fmt.Printf("✅ Uncordoning nodes for PCS completion: %v\n", remainingNodesFifthWave)
	for _, nodeName := range remainingNodesFifthWave {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all PCS pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for PCS completion pods to be ready: %v", err)
	}

	fmt.Printf("✅ Verified all PCS scaling pods are running\n")

	// 16. Set pcs-1-sg-x resource replicas equal to 3, then verify 4 newly created pods
	secondReplicaPCSGName := "workload2-1-sg-x"
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, secondReplicaPCSGName, 3, 28, 4)

	fmt.Printf("✅ PCSG %s scaled to 3 replicas with expected pending pods\n", secondReplicaPCSGName)

	// 17. Verify all newly created pods are pending due to insufficient resources
	fmt.Printf("🔍 Verifying all newly created pods are pending due to insufficient resources...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		totalPods := len(pods.Items)
		runningPods := 0
		pendingPods := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case v1.PodRunning:
				runningPods++
			case v1.PodPending:
				pendingPods++
			}
		}

		expectedRunning := 24 // All previous pods should be running
		expectedPending := 4  // 4 new pods from second PCSG scaling
		fmt.Printf("Pod states after second PCSG scaling: %d running, %d pending (expected %d running, %d pending)\n",
			runningPods, pendingPods, expectedRunning, expectedPending)

		return totalPods == 28 && runningPods == expectedRunning && pendingPods == expectedPending, nil
	})
	if err != nil {
		t.Fatalf("Failed to verify newly created pods are pending after second PCSG scaling: %v", err)
	}

	fmt.Printf("✅ Verified all newly created pods are pending due to insufficient resources\n")

	// 18. Uncordon 2 nodes
	remainingNodesSixthWave := nodesToCordon[22:24]
	fmt.Printf("✅ Uncordoning nodes for final PCSG partial scheduling: %v\n", remainingNodesSixthWave)
	for _, nodeName := range remainingNodesSixthWave {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	// 19. Wait for 2 more pods to be scheduled (min-available for pcs-1-sg-x-2)
	fmt.Printf("⏳ Waiting for 2 more pods to be scheduled for final PCSG partial...\n")
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			}
		}

		fmt.Printf("Running pods: %d (waiting for 26)\n", runningPods)
		return runningPods == 26, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for final PCSG partial scheduling: %v", err)
	}

	fmt.Printf("✅ Verified 2 more pods are running (min-available for pcs-1-sg-x-2)\n")

	// 20. Uncordon 2 nodes and verify remaining workload pods get scheduled
	finalNodes := nodesToCordon[24:26]
	fmt.Printf("✅ Uncordoning final nodes for complete scheduling: %v\n", finalNodes)
	for _, nodeName := range finalNodes {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all final pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all final pods to be ready: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
	if err != nil {
		t.Fatalf("Failed to list all final workload pods: %v", err)
	}

	fmt.Printf("✅ Verified %d pods are running after final scheduling completion\n", len(pods.Items))
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling PCS+PCSG scaling with min-replicas test (GS-11) completed successfully!\n")
}

// Test_GS12_GangSchedulingWithComplexPCSGScaling tests gang-scheduling behavior with complex PCSG scaling operations
// Scenario GS-12:
// 1. Initialize a 28-node Grove cluster, then cordon 26 nodes
// 2. Deploy workload WL2, and verify 10 newly created pods
// 3. Verify all workload pods are pending due to insufficient resources
// 4. Set pcs resource replicas equal to 2, then verify 10 more newly created pods
// 5. Verify all 20 newly created pods are pending due to insufficient resources
// 6. Set both pcs-0-sg-x and pcs-1-sg-x resource replicas equal to 3, verify 8 newly created pods
// 7. Verify all 28 created pods are pending due to insufficient resources
// 8. Uncordon 4 nodes and verify a total of 6 pods get scheduled (pcs-0 and pcs-1 min-available)
// 9. Wait for scheduled pods to become ready
// 10. Uncordon 8 nodes and verify 8 more pods get scheduled (remaining PCSG pods)
// 11. Wait for scheduled pods to become ready
// 12. Uncordon 14 nodes and verify the remaining workload pods get scheduled
func Test_GS12_GangSchedulingWithComplexPCSGScaling(t *testing.T) {
	ctx := context.Background()

	// Setup cluster (shared or individual based on test run mode)
	clientset, restConfig, _, cleanup, _ := setupTestCluster(ctx, t, 28)
	defer cleanup()

	t.Log("🚀 Starting gang-scheduling test with complex PCSG scaling (28 nodes)")

	// Get agent nodes for cordoning
	agentNodes, err := getAgentNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	if len(agentNodes) < 26 {
		t.Fatalf("expected at least 26 agent nodes to cordon, but found %d", len(agentNodes))
	}

	nodesToCordon := agentNodes[:26]
	fmt.Printf("🚫 Cordoning %d agent nodes: %v\n", len(nodesToCordon), nodesToCordon)
	for _, nodeName := range nodesToCordon {
		if err := cordonNode(ctx, clientset, nodeName, true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}

	// 2. Deploy workload WL2, and verify 10 newly created pods
	workloadNamespace := "default"
	workloadLabelSelector := "app.kubernetes.io/part-of=workload2"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath:     "../yaml/workload2.yaml",
		Namespace:        workloadNamespace,
		RestConfig:       restConfig,
		Timeout:          1 * time.Minute,
		PodLabelSelector: workloadLabelSelector,
	}

	fmt.Printf("🚀 Applying workload2.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, utils.NewCILogger(nil))
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	defer func() {
		fmt.Printf("🧹 Cleaning up applied resources...\n")
		for _, resource := range appliedResources {
			fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		}
	}()

	fmt.Printf("🔍 Polling for pods to be created...\n")
	expectedPods := 10

	var pods *v1.PodList
	err = pollForCondition(ctx, 2*time.Minute, 5*time.Second, func() (bool, error) {
		var err error
		pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		fmt.Printf("Found %d workload pods (waiting for %d)\n", len(pods.Items), expectedPods)
		return len(pods.Items) == expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// 3. Verify all workload pods are pending due to insufficient resources
	fmt.Printf("🔍 Verifying all workload pods are pending due to insufficient resources...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		stillPending := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				stillPending++
			}
		}

		fmt.Printf("Pending pods: %d/%d\n", stillPending, len(pods.Items))
		return stillPending == len(pods.Items), nil
	})
	if err != nil {
		t.Fatalf("Failed to verify pods remain pending: %v", err)
	}

	fmt.Printf("✅ Verified all workload pods are pending (gang scheduling working correctly)\n")

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	// 4. Set pcs resource replicas equal to 2, then verify 10 more newly created pods
	scalePCSAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, "workload2", 2, 20, 20)

	fmt.Printf("✅ PCS scaled to 2 replicas with expected pending pods\n")

	// 5. Verify all 20 newly created pods are pending due to insufficient resources
	fmt.Printf("🔍 Verifying all 20 pods are pending due to insufficient resources...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		totalPods := len(pods.Items)
		pendingPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				pendingPods++
			}
		}

		fmt.Printf("Total pods: %d, Pending pods: %d (expected all 20 to be pending)\n", totalPods, pendingPods)
		return totalPods == 20 && pendingPods == 20, nil
	})
	if err != nil {
		t.Fatalf("Failed to verify all 20 pods are pending: %v", err)
	}

	fmt.Printf("✅ Verified all 20 pods are pending due to insufficient resources\n")

	// 6. Set both pcs-0-sg-x and pcs-1-sg-x resource replicas equal to 3, verify 8 newly created pods
	fmt.Printf("📈 Scaling both PCSGs to 3 replicas...\n")

	pcsg1Name := "workload2-0-sg-x"
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, pcsg1Name, 3, 24, 24)

	pcsg2Name := "workload2-1-sg-x"
	scalePCSGAndWait(t, ctx, clientset, dynamicClient, workloadNamespace, workloadLabelSelector, pcsg2Name, 3, 28, 28)

	fmt.Printf("✅ Both PCSGs scaled to 3 replicas with 8 total new pending pods\n")

	// 7. Verify all 28 created pods are pending due to insufficient resources
	fmt.Printf("🔍 Verifying all 28 pods are pending due to insufficient resources...\n")
	err = pollForCondition(ctx, 2*time.Minute, 10*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		totalPods := len(pods.Items)
		pendingPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				pendingPods++
			}
		}

		fmt.Printf("Total pods: %d, Pending pods: %d (expected all 28 to be pending)\n", totalPods, pendingPods)
		return totalPods == 28 && pendingPods == 28, nil
	})
	if err != nil {
		t.Fatalf("Failed to verify all 28 pods are pending: %v", err)
	}

	fmt.Printf("✅ Verified all 28 pods are pending due to insufficient resources\n")

	// 8. Uncordon 4 nodes and verify a total of 6 pods get scheduled (pcs-0 and pcs-1 min-available)
	firstWaveNodes := nodesToCordon[:4]
	fmt.Printf("✅ Uncordoning 4 nodes for min-available scheduling: %v\n", firstWaveNodes)
	for _, nodeName := range firstWaveNodes {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for 6 pods to be scheduled (min-available from both PCS instances)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			}
		}

		fmt.Printf("Running pods: %d (waiting for 6)\n", runningPods)
		return runningPods == 6, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 6 pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified 6 pods are running (min-available from both PCS instances)\n")

	// 9. Wait for scheduled pods to become ready (only the 6 that are scheduled)
	fmt.Printf("⏳ Waiting for the 6 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 10*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		readyPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready pods: %d (waiting for 6)\n", readyPods)
		return readyPods == 6, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 6 pods to be ready: %v", err)
	}

	// 10. Uncordon 8 nodes and verify 8 more pods get scheduled (remaining PCSG pods)
	secondWaveNodes := nodesToCordon[4:12]
	fmt.Printf("✅ Uncordoning 8 nodes for PCSG pods: %v\n", secondWaveNodes)
	for _, nodeName := range secondWaveNodes {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for 8 more pods to be scheduled (remaining PCSG pods)...\n")
	err = pollForCondition(ctx, 5*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			}
		}

		fmt.Printf("Running pods: %d (waiting for 14)\n", runningPods)
		return runningPods == 14, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 8 more pods to be scheduled: %v", err)
	}

	fmt.Printf("✅ Verified 8 more pods are running (remaining PCSG pods)\n")

	// 11. Wait for scheduled pods to become ready (only the 14 that are scheduled)
	fmt.Printf("⏳ Waiting for the 14 scheduled pods to become ready...\n")
	err = pollForCondition(ctx, 10*time.Minute, 5*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
		if err != nil {
			return false, err
		}

		readyPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
						readyPods++
						break
					}
				}
			}
		}

		fmt.Printf("Ready pods: %d (waiting for 14)\n", readyPods)
		return readyPods == 14, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for 14 pods to be ready: %v", err)
	}

	// 12. Uncordon 14 nodes and verify the remaining workload pods get scheduled
	finalWaveNodes := nodesToCordon[12:26]
	fmt.Printf("✅ Uncordoning remaining 14 nodes for final scheduling: %v\n", finalWaveNodes)
	for _, nodeName := range finalWaveNodes {
		if err := cordonNode(ctx, clientset, nodeName, false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}

	fmt.Printf("⏳ Waiting for all remaining pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, utils.NewCILogger(nil)); err != nil {
		t.Fatalf("Failed to wait for all final pods to be ready: %v", err)
	}

	pods, err = clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: workloadLabelSelector})
	if err != nil {
		t.Fatalf("Failed to list all final workload pods: %v", err)
	}

	fmt.Printf("✅ Verified %d pods are running after final scheduling completion\n", len(pods.Items))
	assertPodsOnDistinctNodes(t, pods.Items)

	fmt.Printf("🎉 Gang-scheduling complex PCSG scaling test (GS-12) completed successfully!\n")
}

// getAgentNodes returns a list of agent node names from the cluster
func getAgentNodes(ctx context.Context, clientset kubernetes.Interface) ([]string, error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	agentNodes := make([]string, 0)
	for _, node := range nodes.Items {
		if _, isServer := node.Labels["node-role.kubernetes.io/control-plane"]; !isServer {
			agentNodes = append(agentNodes, node.Name)
		}
	}

	return agentNodes, nil
}
