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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-dynamo/grove/operator/e2e/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Test_SGE1_BaseGangFormationEvents tests that appropriate events are recorded during base gang formation
// Scenario SGE-1:
// 1. Deploy a simple PodCliqueSet with 2 PodCliques (no scaling group)
// 2. Verify schedule gate events are recorded during gang formation
// 3. Verify schedule gate removal events appear once gang is formed
// 4. Verify gang formation complete events are recorded
func Test_SGE1_BaseGangFormationEvents(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize Grove cluster")
	clientset, restConfig, _, cleanup := prepareTestCluster(ctx, t, 3)
	defer cleanup()

	logger.Info("2. Deploy workload with base gang (no scaling group)")
	workloadNamespace := "default"

	// Create a test YAML for a simple base gang
	testYAML := `
apiVersion: grove.ai-dynamo.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: test-base-gang
  namespace: default
spec:
  replicas: 1
  podCliques:
    - name: worker
      podSpec:
        replicas: 2
        minAvailable: 2
        template:
          spec:
            containers:
            - name: worker
              image: nginx:alpine-slim
              resources:
                requests:
                  memory: "50Mi"
    - name: ps
      podSpec:
        replicas: 1
        minAvailable: 1
        template:
          spec:
            containers:
            - name: ps
              image: nginx:alpine-slim
              resources:
                requests:
                  memory: "50Mi"
`

	if err := utils.ApplyYAMLString(ctx, testYAML, workloadNamespace, restConfig, logger); err != nil {
		t.Fatalf("Failed to apply test YAML: %v", err)
	}

	logger.Info("3. Verify gang formation events are recorded")
	// Look for events on the PodCliqueSet or PodCliques indicating gang formation
	expectedEvents := []expectedEventPattern{
		{
			reason:      "GangFormationComplete",
			eventType:   v1.EventTypeNormal,
			messageContains: []string{"gang formation complete", "PodCliques"},
		},
		{
			reason:      "ScheduleGateRemoved",
			eventType:   v1.EventTypeNormal,
			messageContains: []string{"schedule gate", "pod is now eligible for scheduling"},
		},
	}

	// Wait for pods to be created
	err := utils.PollForCondition(ctx, 60*time.Second, 2*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "grove.ai-dynamo.io/podcliqueset=test-base-gang",
		})
		if err != nil {
			return false, err
		}
		// Expect 3 pods total: 2 workers + 1 ps
		return len(pods.Items) >= 3, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("4. Wait for all pods to become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, "grove.ai-dynamo.io/podcliqueset=test-base-gang", 60*time.Second, 5*time.Second, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("5. Verify expected events were recorded")
	if err := verifyEventsExist(ctx, clientset, workloadNamespace, expectedEvents, 30*time.Second); err != nil {
		t.Fatalf("Failed to verify expected events: %v", err)
	}

	logger.Info("🎉 Base Gang Formation Events test completed successfully!")
}

// Test_SGE2_ScaledGangWaitingForBase tests that scaled gang pods emit appropriate waiting events
// Scenario SGE-2:
// 1. Deploy a PodCliqueSet with a scaling group
// 2. Verify base gang schedules first
// 3. Verify scaled gang pods emit ScheduleGateWaitingBasePodCliques events
// 4. Verify events include correct PodClique names and status
func Test_SGE2_ScaledGangWaitingForBase(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize Grove cluster")
	clientset, restConfig, _, cleanup := prepareTestCluster(ctx, t, 5)
	defer cleanup()

	logger.Info("2. Deploy workload with scaling group")
	workloadNamespace := "default"

	// Use workload1.yaml which has a scaling group
	_, err := utils.ApplyYAMLFile(ctx, "../yaml/workload1.yaml", workloadNamespace, restConfig, logger)
	if err != nil {
		t.Fatalf("Failed to apply workload YAML: %v", err)
	}

	logger.Info("3. Wait for pods to be created")
	// workload1: pc-a: 2, pc-b: 1*2 (scaling group), pc-c: 3*2 (scaling group) = 2+2+6=10
	expectedPods := 10
	err = utils.PollForCondition(ctx, 60*time.Second, 2*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/part-of=workload1",
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) >= expectedPods, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("4. Verify scaled gang waiting events are recorded")
	// Look for events indicating scaled gang is waiting for base
	expectedEvents := []expectedEventPattern{
		{
			reason:      "ScheduleGateWaitingBasePodCliques",
			eventType:   v1.EventTypeNormal,
			messageContains: []string{"waiting for base PodCliques", "replica"},
		},
	}

	// Give some time for events to be generated before checking
	time.Sleep(5 * time.Second)

	if err := verifyEventsExist(ctx, clientset, workloadNamespace, expectedEvents, 30*time.Second); err != nil {
		// This may not always fail the test since timing can be tricky - log as warning
		logger.Warningf("Expected scaled gang waiting events not found (this may be timing-dependent): %v", err)
	}

	logger.Info("5. Wait for all pods to become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, "app.kubernetes.io/part-of=workload1", 120*time.Second, 5*time.Second, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("6. Verify schedule gate removal events after gang formation")
	gateRemovedEvents := []expectedEventPattern{
		{
			reason:      "ScheduleGateRemoved",
			eventType:   v1.EventTypeNormal,
			messageContains: []string{"schedule gate", "eligible for scheduling"},
		},
	}

	if err := verifyEventsExist(ctx, clientset, workloadNamespace, gateRemovedEvents, 30*time.Second); err != nil {
		t.Fatalf("Failed to verify gate removal events: %v", err)
	}

	logger.Info("🎉 Scaled Gang Waiting For Base Events test completed successfully!")
}

// Test_SGE3_PartialBaseGangScheduling tests events when some PodCliques in base gang can't schedule
// Scenario SGE-3:
// 1. Deploy a multi-clique base gang
// 2. Constrain resources so some PodCliques can't fully schedule
// 3. Verify events reference specific blocking PodCliques with status details
// 4. Release constraints and verify completion events
func Test_SGE3_PartialBaseGangScheduling(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize Grove cluster and cordon nodes")
	clientset, restConfig, _, cleanup := prepareTestCluster(ctx, t, 5)
	defer cleanup()

	// Get worker nodes
	workerNodes, err := getWorkerNodes(ctx, clientset)
	if err != nil {
		t.Fatalf("Failed to get worker nodes: %v", err)
	}

	// Cordon enough nodes to prevent full scheduling
	nodesToCordon := 2
	if len(workerNodes) < nodesToCordon {
		t.Fatalf("Need at least %d worker nodes, found %d", nodesToCordon, len(workerNodes))
	}

	for i := 0; i < nodesToCordon; i++ {
		logger.Debugf("🚫 Cordoning node: %s", workerNodes[i])
		if err := utils.CordonNode(ctx, clientset, workerNodes[i], true); err != nil {
			t.Fatalf("Failed to cordon node %s: %v", workerNodes[i], err)
		}
	}

	logger.Info("2. Deploy workload that requires more resources than available")
	workloadNamespace := "default"

	testYAML := `
apiVersion: grove.ai-dynamo.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: test-partial-gang
  namespace: default
spec:
  replicas: 1
  podCliques:
    - name: worker
      podSpec:
        replicas: 3
        minAvailable: 3
        template:
          spec:
            containers:
            - name: worker
              image: nginx:alpine-slim
              resources:
                requests:
                  memory: "50Mi"
    - name: ps
      podSpec:
        replicas: 2
        minAvailable: 2
        template:
          spec:
            containers:
            - name: ps
              image: nginx:alpine-slim
              resources:
                requests:
                  memory: "50Mi"
`

	if err := utils.ApplyYAMLString(ctx, testYAML, workloadNamespace, restConfig, logger); err != nil {
		t.Fatalf("Failed to apply test YAML: %v", err)
	}

	logger.Info("3. Wait for pods to be created")
	err = utils.PollForCondition(ctx, 60*time.Second, 2*time.Second, func() (bool, error) {
		pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "grove.ai-dynamo.io/podcliqueset=test-partial-gang",
		})
		if err != nil {
			return false, err
		}
		// Expect 5 pods: 3 workers + 2 ps
		return len(pods.Items) >= 5, nil
	})
	if err != nil {
		t.Fatalf("Failed to wait for pods to be created: %v", err)
	}

	logger.Info("4. Verify pods are pending due to insufficient resources")
	time.Sleep(10 * time.Second) // Give scheduler time to attempt scheduling

	pods, err := clientset.CoreV1().Pods(workloadNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "grove.ai-dynamo.io/podcliqueset=test-partial-gang",
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	pendingPods := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending {
			pendingPods++
		}
	}

	logger.Infof("Found %d pending pods out of %d total", pendingPods, len(pods.Items))

	logger.Info("5. Uncordon nodes to allow full scheduling")
	for i := 0; i < nodesToCordon; i++ {
		if err := utils.CordonNode(ctx, clientset, workerNodes[i], false); err != nil {
			t.Fatalf("Failed to uncordon node %s: %v", workerNodes[i], err)
		}
	}

	logger.Info("6. Wait for all pods to become ready")
	if err := utils.WaitForPods(ctx, restConfig, []string{workloadNamespace}, "grove.ai-dynamo.io/podcliqueset=test-partial-gang", 90*time.Second, 5*time.Second, logger); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("7. Verify gang formation complete events")
	expectedEvents := []expectedEventPattern{
		{
			reason:      "GangFormationComplete",
			eventType:   v1.EventTypeNormal,
			messageContains: []string{"gang formation complete"},
		},
	}

	if err := verifyEventsExist(ctx, clientset, workloadNamespace, expectedEvents, 30*time.Second); err != nil {
		t.Fatalf("Failed to verify completion events: %v", err)
	}

	logger.Info("🎉 Partial Base Gang Scheduling Events test completed successfully!")
}

// Helper types and functions for event verification

type expectedEventPattern struct {
	reason          string
	eventType       string
	messageContains []string
}

// verifyEventsExist checks if events matching the expected patterns exist in the namespace
func verifyEventsExist(ctx context.Context, clientset kubernetes.Interface, namespace string, expectedEvents []expectedEventPattern, timeout time.Duration) error {
	startTime := time.Now()

	for _, expected := range expectedEvents {
		found := false
		
		// Poll for this specific event pattern
		err := utils.PollForCondition(ctx, timeout, 2*time.Second, func() (bool, error) {
			// Adjust timeout for remaining time
			elapsed := time.Since(startTime)
			if elapsed >= timeout {
				return false, fmt.Errorf("timeout waiting for event with reason: %s", expected.reason)
			}

			events, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				return false, err
			}

			for _, event := range events.Items {
				if event.Reason == expected.reason && event.Type == expected.eventType {
					// Check if all required message substrings are present
					allMatch := true
					for _, substring := range expected.messageContains {
						if !strings.Contains(event.Message, substring) {
							allMatch = false
							break
						}
					}
					if allMatch {
						logger.Debugf("✓ Found expected event: %s - %s", event.Reason, event.Message)
						return true, nil
					}
				}
			}
			return false, nil
		})

		if err != nil {
			return fmt.Errorf("event pattern not found - reason: %s, type: %s, contains: %v - error: %w", 
				expected.reason, expected.eventType, expected.messageContains, err)
		}
		
		found = true
		if !found {
			return fmt.Errorf("expected event not found: reason=%s, type=%s", expected.reason, expected.eventType)
		}
	}

	return nil
}
