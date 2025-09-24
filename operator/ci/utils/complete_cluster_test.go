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
	"fmt"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/release"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWithK3DCluster(t *testing.T) {
	ctx := context.Background()

	// Create a CILogger for this test
	logger := NewCILogger(nil)

	// Custom configuration
	customCfg := ClusterConfig{
		Name:             "custom-test-cluster",
		Servers:          2,
		Agents:           20,
		Image:            "rancher/k3s:v1.28.8-k3s1",
		HostPort:         "6551",
		LoadBalancerPort: "8081:80",
		AgentNodeLabels: map[string]string{
			"node_role.e2e.grove.nvidia.com": "agent", // Required for Grove workloads
		},
		AgentNodeTaints: []NodeTaint{
			{
				Key:    "node_role.e2e.grove.nvidia.com",
				Value:  "agent",
				Effect: "NoSchedule",
			},
		},
	}

	fmt.Printf("🚀 Starting k3d cluster test with config: %+v\n", customCfg)

	// Setup cluster with custom config
	clientset, restConfig, _, cleanup, err := SetupK3DCluster(ctx, customCfg, logger)
	defer cleanup() // always call cleanup
	if err != nil {
		t.Fatalf("Failed to setup k3d cluster: %v", err)
	}

	fmt.Printf("✅ Cluster setup complete, testing node listing...\n")

	// Test with custom cluster
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("could not list nodes: %s", err)
	}

	expectedNodes := customCfg.Servers + customCfg.Agents
	fmt.Printf("✅ Found %d nodes in the custom cluster (expected %d)\n", len(nodes.Items), expectedNodes)
	t.Logf("✅ Found %d nodes in the custom cluster", len(nodes.Items))

	if len(nodes.Items) != expectedNodes {
		t.Errorf("expected %d nodes, but found %d", expectedNodes, len(nodes.Items))
	}

	// Verify configured node labels are applied to agent nodes
	agentNodes := 0
	for _, node := range nodes.Items {
		// Check if this is an agent node (not a server/control-plane)
		if _, isServer := node.Labels["node-role.kubernetes.io/control-plane"]; !isServer {
			agentNodes++
			// Verify all configured labels are present
			for k, expectedV := range customCfg.AgentNodeLabels {
				if actualV, exists := node.Labels[k]; !exists || actualV != expectedV {
					t.Errorf("Expected label %s=%s on agent node %s, but got %s", k, expectedV, node.Name, actualV)
				}
			}
		}
	}
	fmt.Printf("✅ Verified labels on %d agent nodes\n", agentNodes)

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
	groveConfig := GroveInstallConfigV0_1_0_Alpha1()
	groveConfig.ReleaseName = "grove-test"
	groveConfig.Namespace = namespace
	groveConfig.RestConfig = restConfig

	// Add tolerations for control-plane and Grove e2e taints so Grove can schedule on all nodes
	groveConfig.Values["tolerations"] = []map[string]interface{}{
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

	// Configure Kai Scheduler
	kaiConfig := KaiInstallConfigLatest("v0.9.3")
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
	nvidiaConfig := NvidiaOperatorInstallConfigLatest("v25.3.4")
	nvidiaConfig.ReleaseName = "nvidia-gpu-operator-test" // Use specific name instead of auto-generated
	nvidiaConfig.GenerateName = false                     // Disable auto-generation
	nvidiaConfig.RestConfig = restConfig

	// Add tolerations for control-plane and Grove e2e taints so NVIDIA operator can schedule on all nodes
	nvidiaConfig.Values["node-feature-discovery"] = map[string]interface{}{
		"gc": map[string]interface{}{
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
		_, err := InstallGrove(groveConfig, logger)
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
		_, err := InstallKai(kaiConfig, logger)
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
		_, err := InstallNvidiaOperator(nvidiaConfig, logger)
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
	if err := WaitForGrovePodsReady(ctx, namespace, restConfig, logger); err != nil {
		t.Fatalf("Grove pods not ready: %v", err)
	}

	// Wait for Kai Scheduler pods to be ready
	if err := WaitForKaiPodsReady(ctx, restConfig, logger); err != nil {
		t.Fatalf("Kai Scheduler pods not ready: %v", err)
	}

	// Wait for Kai CRDs to be ready
	if err := WaitForKaiCRDs(ctx, restConfig, logger); err != nil {
		t.Fatalf("Failed to wait for Kai CRDs: %v", err)
	}

	// Create default queue for Kai scheduler
	if err := CreateDefaultKaiQueues(ctx, restConfig, logger); err != nil {
		t.Fatalf("Failed to create default queue for Kai scheduler: %v", err)
	}

	// Wait for NVIDIA GPU Operator to be ready
	if err := WaitForNvidiaOperatorReady(ctx, restConfig, logger); err != nil {
		t.Fatalf("NVIDIA GPU Operator not ready: %v", err)
	}

	fmt.Printf("🎉 Test completed successfully! Grove, Kai, and NVIDIA GPU Operator are all ready.\n")
}

// Example of how to use kind with custom configuration
func TestWithKindCluster(t *testing.T) {
	ctx := context.Background()

	// Create a CILogger for this test
	logger := NewCILogger(nil)

	// Custom configuration
	customCfg := KindClusterConfig{
		Name:          "custom-kind-cluster",
		ControlPlanes: 2,
		Workers:       3,
		Image:         "kindest/node:v1.28.0", // Optional: specify custom image
		WorkerNodeLabels: map[string]string{
			"node_role.e2e.grove.nvidia.com": "agent", // Required for Grove workloads
		},
		WorkerNodeTaints: []NodeTaint{
			{
				Key:    "node_role.e2e.grove.nvidia.com",
				Value:  "agent",
				Effect: "NoSchedule",
			},
		},
	}

	fmt.Printf("🚀 Starting kind cluster test with config: %+v\n", customCfg)

	// Setup cluster with custom config
	clientset, restConfig, cleanup, err := SetupKindCluster(ctx, customCfg, logger)
	defer cleanup() // always call cleanup
	if err != nil {
		t.Fatalf("Failed to setup kind cluster: %v", err)

	}

	fmt.Printf("✅ Kind cluster setup complete, testing node listing...\n")

	// Test with custom cluster
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("could not list nodes: %s", err)
	}

	expectedNodes := customCfg.ControlPlanes + customCfg.Workers
	fmt.Printf("✅ Found %d nodes in the custom kind cluster (expected %d)\n", len(nodes.Items), expectedNodes)
	t.Logf("✅ Found %d nodes in the custom kind cluster", len(nodes.Items))

	if len(nodes.Items) != expectedNodes {
		t.Errorf("expected %d nodes, but found %d", expectedNodes, len(nodes.Items))
	}

	// Verify configured node labels are applied to worker nodes
	workerNodes := 0
	for _, node := range nodes.Items {
		// Check if this is a worker node (not a control-plane)
		if _, isControlPlane := node.Labels["node-role.kubernetes.io/control-plane"]; !isControlPlane {
			workerNodes++
			// Verify all configured labels are present
			for k, expectedV := range customCfg.WorkerNodeLabels {
				if actualV, exists := node.Labels[k]; !exists || actualV != expectedV {
					t.Errorf("Expected label %s=%s on worker node %s, but got %s", k, expectedV, node.Name, actualV)
				}
			}
		}
	}
	fmt.Printf("✅ Verified labels on %d worker nodes\n", workerNodes)

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
	groveConfig := GroveInstallConfigV0_1_0_Alpha1()
	groveConfig.ReleaseName = "grove-kind-test"
	groveConfig.Namespace = namespace
	groveConfig.RestConfig = restConfig

	// Add tolerations for control-plane and Grove e2e taints so Grove can schedule on all nodes
	groveConfig.Values["tolerations"] = []map[string]interface{}{
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

	// Configure Kai Scheduler
	kaiConfig := KaiInstallConfigLatest("v0.9.3")
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
	nvidiaConfig := NvidiaOperatorInstallConfigLatest("v25.3.4")
	nvidiaConfig.ReleaseName = "nvidia-gpu-operator-kind-test" // Use specific name instead of auto-generated
	nvidiaConfig.GenerateName = false                          // Disable auto-generation
	nvidiaConfig.RestConfig = restConfig

	// Add tolerations for control-plane and Grove e2e taints so NVIDIA operator can schedule on all nodes
	nvidiaConfig.Values["node-feature-discovery"] = map[string]interface{}{
		"gc": map[string]interface{}{
			"tolerations": []map[string]interface{}{
				{
					"key":      "node-role.kubernetes.io/control-plane",
					"operator": "Exists",
					"effect":   "NoSchedule",
				},
			},
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
		_, err := InstallOrUpgradeGrove(groveConfig, logger)
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
		_, err := InstallOrUpgradeKai(kaiConfig, logger)
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
		_, err := InstallNvidiaOperator(nvidiaConfig, logger)
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
	if err := WaitForGrovePodsReady(ctx, namespace, restConfig, logger); err != nil {
		t.Fatalf("Grove pods not ready: %v", err)
	}

	// Wait for Kai Scheduler pods to be ready
	if err := WaitForKaiPodsReady(ctx, restConfig, logger); err != nil {
		t.Fatalf("Kai Scheduler pods not ready: %v", err)
	}

	// Wait for Kai CRDs to be ready
	if err := WaitForKaiCRDs(ctx, restConfig, logger); err != nil {
		t.Fatalf("Failed to wait for Kai CRDs: %v", err)
	}

	// Create default queue for Kai scheduler
	if err := CreateDefaultKaiQueues(ctx, restConfig, logger); err != nil {
		t.Fatalf("Failed to create default queue for Kai scheduler: %v", err)
	}

	// Wait for NVIDIA GPU Operator to be ready
	if err := WaitForNvidiaOperatorReady(ctx, restConfig, logger); err != nil {
		t.Fatalf("NVIDIA GPU Operator not ready: %v", err)
	}

	fmt.Printf("🎉 Kind test completed successfully! Grove, Kai, and NVIDIA GPU Operator are all ready.\n")
}

// GroveInstallResult holds the result of a timed Grove installation
type GroveInstallResult struct {
	Release  *release.Release
	Duration time.Duration
}

// KaiInstallResult holds the result of a timed Kai installation
type KaiInstallResult struct {
	Release  *release.Release
	Duration time.Duration
}

// InstallGroveWithTiming installs Grove and measures the installation time
// It prints the timing information and returns both the release and duration
func InstallGroveWithTiming(t *testing.T, config *GroveInstallConfig, logger *CILogger) (*GroveInstallResult, error) {
	t.Helper()

	start := time.Now()
	logger.Info("🚀 Starting Grove installation...")

	rel, err := InstallGrove(config, logger)
	duration := time.Since(start)

	result := &GroveInstallResult{
		Release:  rel,
		Duration: duration,
	}

	if err != nil {
		logger.Errorf("❌ Grove installation failed after %v: %v", duration, err)
		return result, err
	} else {
		logger.Infof("✅ Grove installation completed successfully in %v (release: %s, namespace: %s)",
			duration, rel.Name, rel.Namespace)
	}

	return result, nil
}

// InstallOrUpgradeGroveWithTiming installs or upgrades Grove and measures the time
// It prints the timing information and returns both the release and duration
func InstallOrUpgradeGroveWithTiming(t *testing.T, config *GroveInstallConfig, logger *CILogger) (*GroveInstallResult, error) {
	t.Helper()

	start := time.Now()
	logger.Info("🚀 Starting Grove install/upgrade...")

	rel, err := InstallOrUpgradeGrove(config, logger)
	duration := time.Since(start)

	result := &GroveInstallResult{
		Release:  rel,
		Duration: duration,
	}

	if err != nil {
		logger.Errorf("❌ Grove install/upgrade failed after %v: %v", duration, err)
		return result, err
	} else {
		logger.Infof("✅ Grove install/upgrade completed successfully in %v (release: %s, namespace: %s)",
			duration, rel.Name, rel.Namespace)
	}

	return result, nil
}

// InstallKaiWithTiming installs Kai Scheduler and measures the installation time
// It prints the timing information and returns both the release and duration
func InstallKaiWithTiming(t *testing.T, config *KaiInstallConfig, logger *CILogger) (*KaiInstallResult, error) {
	t.Helper()

	start := time.Now()
	logger.Info("🚀 Starting Kai Scheduler installation...")

	rel, err := InstallKai(config, logger)
	duration := time.Since(start)

	result := &KaiInstallResult{
		Release:  rel,
		Duration: duration,
	}

	if err != nil {
		logger.Errorf("❌ Kai Scheduler installation failed after %v: %v", duration, err)
		return result, err
	} else {
		logger.Infof("✅ Kai Scheduler installation completed successfully in %v (release: %s, namespace: %s)",
			duration, rel.Name, rel.Namespace)
	}

	return result, nil
}

// InstallOrUpgradeKaiWithTiming installs or upgrades Kai Scheduler and measures the time
// It prints the timing information and returns both the release and duration
func InstallOrUpgradeKaiWithTiming(t *testing.T, config *KaiInstallConfig, logger *CILogger) (*KaiInstallResult, error) {
	t.Helper()

	start := time.Now()
	logger.Info("🚀 Starting Kai Scheduler install/upgrade...")

	rel, err := InstallOrUpgradeKai(config, logger)
	duration := time.Since(start)

	result := &KaiInstallResult{
		Release:  rel,
		Duration: duration,
	}

	if err != nil {
		logger.Errorf("❌ Kai Scheduler install/upgrade failed after %v: %v", duration, err)
		return result, err
	} else {
		logger.Infof("✅ Kai Scheduler install/upgrade completed successfully in %v (release: %s, namespace: %s)",
			duration, rel.Name, rel.Namespace)
	}

	return result, nil
}
