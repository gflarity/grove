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

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetupCompleteK3DClusterK3DCluster(t *testing.T) {
	ctx := context.Background()

	// Create a logger for this test
	logger := NewCILogger(logrus.InfoLevel)

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

	// Setup cluster with custom config including installations
	clientset, _, _, cleanup, err := SetupCompleteK3DCluster(ctx, customCfg, logger)
	defer cleanup() // always call cleanup
	if err != nil {
		t.Fatalf("Failed to setup k3d cluster: %v", err)
	}

	fmt.Printf("✅ Complete cluster setup finished, testing node listing...\n")

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

	fmt.Printf("✅ Namespace %s ensured by helper\n", "grove-system")

	fmt.Printf("🎉 Test completed successfully! Grove, Kai, and NVIDIA GPU Operator are all ready.\n")
}
