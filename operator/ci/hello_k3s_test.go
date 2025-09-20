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

package ci

import (
	"context"
	"fmt"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/release"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/grove/operator/ci/utils"
)

func TestWithK3DCluster(t *testing.T) {
	ctx := context.Background()

	// Create a CILogger for this test
	logger, closeLogger := utils.NewCILoggerWithFile()
	defer closeLogger()

	// Custom configuration
	customCfg := utils.ClusterConfig{
		Name:             "custom-test-cluster",
		Servers:          2,
		Agents:           20,
		Image:            "rancher/k3s:v1.28.8-k3s1",
		HostPort:         "6551",
		LoadBalancerPort: "8081:80",
	}

	fmt.Printf("🚀 Starting k3d cluster test with config: %+v\n", customCfg)

	// Setup cluster with custom config
	clientset, restConfig, _, cleanup, err := utils.SetupK3DCluster(ctx, customCfg, logger)
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

	// Install Grove with timing
	groveConfig := utils.GroveInstallConfigV0_1_0_Alpha1()
	groveConfig.ReleaseName = "grove-test"
	groveConfig.Namespace = namespace
	// Use the same REST config as the cluster
	groveConfig.RestConfig = restConfig

	groveResult, err := InstallGroveWithTiming(t, groveConfig, logger)
	if err != nil {
		t.Fatalf("Grove installation failed: %v", err)
	}

	fmt.Printf("⏱️  Grove installation took %v (release: %s, namespace: %s)\n",
		groveResult.Duration, groveResult.Release.Name, groveResult.Release.Namespace)

	// Apply workload1.yaml and wait for all pods to be ready
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath: "/Users/gflarity/git/grove/operator/ci/workloads/workload1.yaml",
		Namespace:    namespace,
		RestConfig:   restConfig,
		Timeout:      10 * time.Minute, // Allow more time for workload pods
	}

	fmt.Printf("🚀 Applying workload1.yaml and waiting for pods to be ready...\n")
	if err := utils.ApplyYAMLAndWaitForPods(ctx, workloadConfig, logger); err != nil {
		t.Fatalf("Failed to apply workload and wait for pods: %v", err)
	}

	fmt.Printf("🎉 Test completed successfully! All workload pods are ready.\n")
}

// Example of how to use kind with custom configuration
func TestWithKindCluster(t *testing.T) {
	ctx := context.Background()

	// Create a CILogger for this test
	logger, closeLogger := utils.NewCILoggerWithFile()
	defer closeLogger()

	// Custom configuration
	customCfg := utils.KindClusterConfig{
		Name:          "custom-kind-cluster",
		ControlPlanes: 2,
		Workers:       3,
		Image:         "kindest/node:v1.28.0", // Optional: specify custom image
	}

	fmt.Printf("🚀 Starting kind cluster test with config: %+v\n", customCfg)

	// Setup cluster with custom config
	clientset, restConfig, cleanup, err := utils.SetupKindCluster(ctx, customCfg, logger)
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

	// Install Grove with timing on Kind cluster
	groveConfig := utils.GroveInstallConfigV0_1_0_Alpha1()
	groveConfig.ReleaseName = "grove-kind-test"
	groveConfig.Namespace = namespace
	// Use the same REST config as the cluster
	groveConfig.RestConfig = restConfig

	groveResult, err := InstallOrUpgradeGroveWithTiming(t, groveConfig, logger)
	if err != nil {
		t.Fatalf("Grove installation failed: %v", err)
	}

	fmt.Printf("⏱️  Grove installation took %v (release: %s, namespace: %s)\n",
		groveResult.Duration, groveResult.Release.Name, groveResult.Release.Namespace)

	// Apply workload1.yaml and wait for all pods to be ready
	workloadConfig := &utils.WorkloadConfig{
		YAMLFilePath: "/Users/gflarity/git/grove/operator/ci/workloads/workload1.yaml",
		Namespace:    namespace,
		RestConfig:   restConfig,
		Timeout:      10 * time.Minute, // Allow more time for workload pods
	}

	fmt.Printf("🚀 Applying workload1.yaml and waiting for pods to be ready...\n")
	if err := utils.ApplyYAMLAndWaitForPods(ctx, workloadConfig, logger); err != nil {
		t.Fatalf("Failed to apply workload and wait for pods: %v", err)
	}

	fmt.Printf("🎉 Kind test completed successfully! All workload pods are ready.\n")
}

// GroveInstallResult holds the result of a timed Grove installation
type GroveInstallResult struct {
	Release  *release.Release
	Duration time.Duration
}

// InstallGroveWithTiming installs Grove and measures the installation time
// It prints the timing information and returns both the release and duration
func InstallGroveWithTiming(t *testing.T, config *utils.GroveInstallConfig, logger *utils.CILogger) (*GroveInstallResult, error) {
	t.Helper()

	start := time.Now()
	logger.Info("🚀 Starting Grove installation...")

	rel, err := utils.InstallGrove(config, logger)
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
func InstallOrUpgradeGroveWithTiming(t *testing.T, config *utils.GroveInstallConfig, logger *utils.CILogger) (*GroveInstallResult, error) {
	t.Helper()

	start := time.Now()
	logger.Info("🚀 Starting Grove install/upgrade...")

	rel, err := utils.InstallOrUpgradeGrove(config, logger)
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
