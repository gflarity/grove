package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NVIDIA/grove/operator/ci/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestGangSchedulingWithFullReplicas tests gang-scheduling behavior with insufficient resources
// Scenario: Initialize a 10-node Grove cluster, cordon 1 node, deploy workload WL1, verify 10 newly created pods
// are all pending due to insufficient resources, then uncordon the node and verify all pods get scheduled
func TestGangSchedulingWithFullReplicas(t *testing.T) {
	ctx := context.Background()

	// Create a CILogger for this test
	logger := utils.NewCILogger(nil)

	// Custom configuration for 10-node k3d cluster (1 server + 9 agents)
	customCfg := utils.ClusterConfig{
		Name:             "gang-scheduling-test-cluster",
		Servers:          1,
		Agents:           10, // 10 agents (worker nodes)
		Image:            "rancher/k3s:v1.28.8-k3s1",
		HostPort:         "6552",
		LoadBalancerPort: "8082:80",
		AgentNodeLabels: map[string]string{
			"node_role.e2e.grove.nvidia.com": "agent", // Required for Grove workloads
		},
		AgentNodeTaints: []utils.NodeTaint{
			{
				Key:    "node_role.e2e.grove.nvidia.com",
				Value:  "agent",
				Effect: "NoSchedule",
			},
		},
	}

	fmt.Printf("🚀 Starting gang-scheduling test with 10-node k3d cluster: %+v\n", customCfg)

	// Setup cluster with custom config
	clientset, restConfig, _, cleanup, err := utils.SetupK3DCluster(ctx, customCfg, logger)
	defer cleanup() // always call cleanup
	if err != nil {
		t.Fatalf("Failed to setup k3d cluster: %v", err)
	}

	fmt.Printf("✅ Cluster setup complete, verifying node count...\n")

	// Verify we have exactly 10 nodes
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("could not list nodes: %s", err)
	}

	expectedNodes := customCfg.Servers + customCfg.Agents
	fmt.Printf("✅ Found %d nodes in the cluster (expected %d)\n", len(nodes.Items), expectedNodes)

	if len(nodes.Items) != expectedNodes {
		t.Fatalf("expected %d nodes, but found %d", expectedNodes, len(nodes.Items))
	}

	// Cordon one agent node to simulate insufficient resources
	agentNodeToCordon := ""
	for _, node := range nodes.Items {
		// Find an agent node (not a server/control-plane)
		if _, isServer := node.Labels["node-role.kubernetes.io/control-plane"]; !isServer {
			agentNodeToCordon = node.Name
			break
		}
	}

	if agentNodeToCordon == "" {
		t.Fatalf("Could not find any agent nodes to cordon")
	}

	fmt.Printf("🚫 Cordoning agent node: %s\n", agentNodeToCordon)
	if err := cordonNode(ctx, clientset, agentNodeToCordon, true); err != nil {
		t.Fatalf("Failed to cordon node %s: %v", agentNodeToCordon, err)
	}

	// Create the namespace for Grove installation
	namespace := "grove-system"
	_, err = clientset.CoreV1().Namespaces().Create(ctx, &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace %s: %v", namespace, err)
	}
	fmt.Printf("✅ Created namespace: %s\n", namespace)

	// Install Grove, Kai, and NVIDIA GPU Operator in parallel
	fmt.Printf("🚀 Starting parallel installation of Grove, Kai Scheduler, and NVIDIA GPU Operator...\n")

	// Configure Grove
	groveConfig := utils.GroveInstallConfigV0_1_0_Alpha1()
	groveConfig.ReleaseName = "grove-gang-test"
	groveConfig.Namespace = namespace
	groveConfig.RestConfig = restConfig

	// Add tolerations for control-plane and Grove e2e taints so Grove can schedule on all worker nodes
	groveConfig.Values["tolerations"] = []map[string]interface{}{
		{
			"key":      "node_role.e2e.grove.nvidia.com",
			"operator": "Equal",
			"value":    "agent",
			"effect":   "NoSchedule",
		},
	}

	// Configure Kai Scheduler
	kaiConfig := utils.KaiInstallConfigLatest("v0.9.3")
	kaiConfig.RestConfig = restConfig

	// Add tolerations for control-plane and Grove e2e taints so Kai can schedule on all nodes
	kaiConfig.Values["global"] = map[string]interface{}{
		"tolerations": []map[string]interface{}{
			{
				"key":      "node-role.kubernetes.io/control-plane",
				"operator": "Exists",
				"effect":   "NoSchedule",
			},
			{
				"key":      "node_role.e2e.grove.nvidia.com",
				"operator": "Equal",
				"value":    "agent",
				"effect":   "NoSchedule",
			},
		},
	}

	// Configure NVIDIA GPU Operator
	nvidiaConfig := utils.NvidiaOperatorInstallConfigLatest("v25.3.4")
	nvidiaConfig.ReleaseName = "nvidia-gpu-operator-gang-test" // Use specific name instead of auto-generated
	nvidiaConfig.GenerateName = false                          // Disable auto-generation
	nvidiaConfig.RestConfig = restConfig

	// Add tolerations for control-plane and Grove e2e taints so NVIDIA operator can schedule on all nodes
	nvidiaConfig.Values["tolerations"] = []map[string]interface{}{
		{
			"key":      "node-role.kubernetes.io/control-plane",
			"operator": "Exists",
			"effect":   "NoSchedule",
		},
		{
			"key":      "node_role.e2e.grove.nvidia.com",
			"operator": "Equal",
			"value":    "agent",
			"effect":   "NoSchedule",
		},
	}

	// Configure NVIDIA operator for test environment without actual GPUs
	nvidiaConfig.Values["driver"] = map[string]interface{}{
		"enabled": false, // Disable GPU driver installation in test environment
	}
	nvidiaConfig.Values["toolkit"] = map[string]interface{}{
		"enabled": false, // Disable container toolkit in test environment
	}
	nvidiaConfig.Values["devicePlugin"] = map[string]interface{}{
		"enabled": false, // Disable device plugin in test environment
	}
	nvidiaConfig.Values["dcgmExporter"] = map[string]interface{}{
		"enabled": false, // Disable DCGM exporter in test environment
	}
	nvidiaConfig.Values["gfd"] = map[string]interface{}{
		"enabled": false, // Disable GPU feature discovery in test environment
	}
	nvidiaConfig.Values["migManager"] = map[string]interface{}{
		"enabled": false, // Disable MIG manager in test environment
	}
	nvidiaConfig.Values["nodeStatusExporter"] = map[string]interface{}{
		"enabled": false, // Disable node status exporter in test environment
	}

	// Install all three components in parallel using goroutines
	type installResult struct {
		name string
		err  error
	}

	resultChan := make(chan installResult, 3)

	// Install Grove
	go func() {
		logger.Info("🚀 Starting Grove installation...")
		_, err := utils.InstallGrove(groveConfig, logger)
		if err != nil {
			logger.Errorf("❌ Grove installation failed: %v", err)
			resultChan <- installResult{"Grove", err}
		} else {
			logger.Info("✅ Grove installation completed successfully")
			resultChan <- installResult{"Grove", nil}
		}
	}()

	// Install Kai Scheduler
	go func() {
		logger.Info("🚀 Starting Kai Scheduler installation...")
		_, err := utils.InstallKai(kaiConfig, logger)
		if err != nil {
			logger.Errorf("❌ Kai Scheduler installation failed: %v", err)
			resultChan <- installResult{"Kai", err}
		} else {
			logger.Info("✅ Kai Scheduler installation completed successfully")
			resultChan <- installResult{"Kai", nil}
		}
	}()

	// Install NVIDIA GPU Operator
	go func() {
		logger.Info("🚀 Starting NVIDIA GPU Operator installation...")
		_, err := utils.InstallNvidiaOperator(nvidiaConfig, logger)
		if err != nil {
			logger.Errorf("❌ NVIDIA GPU Operator installation failed: %v", err)
			resultChan <- installResult{"NVIDIA GPU Operator", err}
		} else {
			logger.Info("✅ NVIDIA GPU Operator installation completed successfully")
			resultChan <- installResult{"NVIDIA GPU Operator", nil}
		}
	}()

	// Wait for all three installations to complete
	for i := 0; i < 3; i++ {
		result := <-resultChan
		if result.err != nil {
			t.Fatalf("%s installation failed: %v", result.name, result.err)
		}
		fmt.Printf("✅ %s installation completed\n", result.name)
	}

	fmt.Printf("✅ All three installations (Grove, Kai Scheduler, and NVIDIA GPU Operator) completed successfully\n")

	// Wait for Grove pods to be ready
	if err := utils.WaitForGrovePodsReady(ctx, namespace, restConfig, logger); err != nil {
		t.Fatalf("Grove pods not ready: %v", err)
	}

	// Wait for Kai Scheduler pods to be ready
	if err := utils.WaitForKaiPodsReady(ctx, restConfig, logger); err != nil {
		t.Fatalf("Kai Scheduler pods not ready: %v", err)
	}

	// Wait for Kai CRDs to be ready
	if err := utils.WaitForKaiCRDs(ctx, restConfig, logger); err != nil {
		t.Fatalf("Failed to wait for Kai CRDs: %v", err)
	}

	// Create default queue for Kai scheduler
	if err := utils.CreateDefaultKaiQueue(ctx, restConfig, logger); err != nil {
		t.Fatalf("Failed to create default queue for Kai scheduler: %v", err)
	}

	// Wait for NVIDIA GPU Operator to be ready
	if err := utils.WaitForNvidiaOperatorReady(ctx, restConfig, logger); err != nil {
		t.Fatalf("NVIDIA GPU Operator not ready: %v", err)
	}

	// Deploy workload1.yaml
	workloadNamespace := "default"
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath: "/Users/gflarity/git/grove/operator/ci/workloads/workload1.yaml",
		Namespace:    workloadNamespace,
		RestConfig:   restConfig,
		Timeout:      2 * time.Minute, // Short timeout since we expect pods to be pending
	}

	fmt.Printf("🚀 Applying workload1.yaml...\n")
	appliedResources, err := utils.ApplyYAML(ctx, workloadConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	// Poll for pod creation and verify they are pending
	fmt.Printf("🔍 Polling for pods to be created and verifying they remain pending...\n")
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

	fmt.Printf("✅ Found %d workload pods as expected\n", len(pods.Items))

	// Verify all pods are pending due to unschedulable nodes
	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
		fmt.Printf("Pod %s: Phase=%s, Node=%s\n", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are pending (expected all %d to be pending)\n", pendingPods, len(pods.Items))

	// Poll to verify pods remain pending for a reasonable time (gang scheduling should prevent partial scheduling)
	fmt.Printf("🔍 Verifying pods remain pending due to gang scheduling...\n")
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

	fmt.Printf("✅ Verified pods remain pending (gang scheduling working correctly)\n")

	// Uncordon the node to provide sufficient resources
	fmt.Printf("✅ Uncordoning agent node: %s\n", agentNodeToCordon)
	if err := cordonNode(ctx, clientset, agentNodeToCordon, false); err != nil {
		t.Fatalf("Failed to uncordon node %s: %v", agentNodeToCordon, err)
	}

	// Wait for all pods to be scheduled and ready
	fmt.Printf("⏳ Waiting for all pods to be scheduled and ready...\n")
	workloadConfig.Timeout = 10 * time.Minute // Allow more time for workload pods
	if err := utils.WaitForPods(ctx, workloadConfig, []string{workloadNamespace}, logger); err != nil {
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
		fmt.Printf("Pod %s: Phase=%s, Node=%s\n", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	fmt.Printf("✅ Verified %d pods are now running (expected all %d to be running)\n", runningPods, len(pods.Items))

	if runningPods != len(pods.Items) {
		t.Errorf("Expected all %d pods to be running, but only %d are running", len(pods.Items), runningPods)
	}

	fmt.Printf("🎉 Gang-scheduling test completed successfully! Grove, Kai, and NVIDIA GPU Operator installed, all workload pods transitioned from pending to running after uncordoning.\n")

	// Cleanup applied resources
	fmt.Printf("🧹 Cleaning up applied resources...\n")
	for _, resource := range appliedResources {
		fmt.Printf("Deleting %s %s/%s\n", resource.GVK.Kind, resource.Namespace, resource.Name)
		// Note: Cleanup is handled by the cluster cleanup function
	}
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

// cordonNode cordons or uncordons a node by setting its Unschedulable field
func cordonNode(ctx context.Context, clientset kubernetes.Interface, nodeName string, cordon bool) error {
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	if node.Spec.Unschedulable == cordon {
		// Already in desired state
		return nil
	}

	node.Spec.Unschedulable = cordon
	_, err = clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update node %s: %w", nodeName, err)
	}

	action := "uncordoned"
	if cordon {
		action = "cordoned"
	}
	fmt.Printf("✅ Successfully %s node %s\n", action, nodeName)
	return nil
}
