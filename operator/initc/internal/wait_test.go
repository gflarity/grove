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

package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	apicommon "github.com/NVIDIA/grove/operator/api/common"

	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

// TestNewPodCliqueState tests the NewPodCliqueStateWithPaths function which initializes
// ParentPodCliqueDependencies by reading pod metadata from specified file paths.
// It verifies proper state initialization, file reading, and error handling.
func TestNewPodCliqueState(t *testing.T) {
	tests := []struct {
		// Test case identifier for clear test output and debugging
		name string
		// Map of parent PodClique names to minimum required ready pods for this test
		podCliqueDependencies map[string]int
		// Function to create necessary files in temp directory simulating downward API mounts
		setupFiles func(t *testing.T, tempDir string)
		// Whether this test case should result in an error during state creation
		expectError bool
		// Expected namespace value to be read from the namespace file
		expectedNamespace string
		// Expected PodGang name to be read from the podgang file
		expectedPodGang string
		// Expected internal map of parent PodClique names to minimum ready pod requirements
		expectedPclqFQNToMinAvail map[string]int
		// Expected internal map of ready pod tracking sets (initially empty for all parents)
		expectedCurrentPCLQReady map[string]sets.Set[string]
	}{
		{
			// Test Case: Successful initialization with multiple parent PodClique dependencies
			// Expectation: State should be properly initialized with all required fields populated
			// and empty ready pod sets created for each parent PodClique
			name: "successful initialization with valid files",
			podCliqueDependencies: map[string]int{
				"parent-pclq-1": 2,
				"parent-pclq-2": 1,
			},
			setupFiles: func(t *testing.T, tempDir string) {
				// Create namespace file simulating downward API volume mount
				namespaceFile := filepath.Join(tempDir, "namespace")
				err := os.WriteFile(namespaceFile, []byte("test-namespace"), 0644)
				if err != nil {
					t.Fatalf("failed to create namespace file: %v", err)
				}

				// Create podgang name file simulating downward API volume mount
				podGangFile := filepath.Join(tempDir, "podgangname")
				err = os.WriteFile(podGangFile, []byte("test-podgang"), 0644)
				if err != nil {
					t.Fatalf("failed to create podgang file: %v", err)
				}
			},
			expectError:       false,
			expectedNamespace: "test-namespace",
			expectedPodGang:   "test-podgang",
			expectedPclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 2, // Requires 2 ready pods
				"parent-pclq-2": 1, // Requires 1 ready pod
			},
			expectedCurrentPCLQReady: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](), // Initially empty
				"parent-pclq-2": sets.New[string](), // Initially empty
			},
		},
		{
			// Test Case: Missing namespace file error handling
			// Expectation: Function should fail when the required namespace file is not present
			// This simulates a scenario where the downward API volume mount is incomplete
			name: "missing namespace file",
			podCliqueDependencies: map[string]int{
				"parent-pclq-1": 1,
			},
			setupFiles: func(t *testing.T, tempDir string) {
				// Only create podgang file, intentionally omit namespace file
				podGangFile := filepath.Join(tempDir, "podgangname")
				err := os.WriteFile(podGangFile, []byte("test-podgang"), 0644)
				if err != nil {
					t.Fatalf("failed to create podgang file: %v", err)
				}
			},
			expectError: true, // Should fail due to missing namespace file
		},
		{
			// Test Case: Missing podgang file error handling
			// Expectation: Function should fail when the required podgang name file is not present
			// This simulates a scenario where the downward API volume mount is incomplete
			name: "missing podgang file",
			podCliqueDependencies: map[string]int{
				"parent-pclq-1": 1,
			},
			setupFiles: func(t *testing.T, tempDir string) {
				// Only create namespace file, intentionally omit podgang file
				namespaceFile := filepath.Join(tempDir, "namespace")
				err := os.WriteFile(namespaceFile, []byte("test-namespace"), 0644)
				if err != nil {
					t.Fatalf("failed to create namespace file: %v", err)
				}
			},
			expectError: true, // Should fail due to missing podgang file
		},
		{
			// Test Case: Initialization with no parent dependencies
			// Expectation: Should successfully initialize with empty dependency maps
			// This represents a pod that doesn't need to wait for any parent PodCliques
			name:                  "empty dependencies map",
			podCliqueDependencies: map[string]int{}, // No parent dependencies
			setupFiles: func(t *testing.T, tempDir string) {
				// Create both required files
				namespaceFile := filepath.Join(tempDir, "namespace")
				err := os.WriteFile(namespaceFile, []byte("test-namespace"), 0644)
				if err != nil {
					t.Fatalf("failed to create namespace file: %v", err)
				}

				podGangFile := filepath.Join(tempDir, "podgangname")
				err = os.WriteFile(podGangFile, []byte("test-podgang"), 0644)
				if err != nil {
					t.Fatalf("failed to create podgang file: %v", err)
				}
			},
			expectError:               false,
			expectedNamespace:         "test-namespace",
			expectedPodGang:           "test-podgang",
			expectedPclqFQNToMinAvail: map[string]int{},              // Empty map
			expectedCurrentPCLQReady:  map[string]sets.Set[string]{}, // Empty map
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test files
			tempDir := t.TempDir()

			// Setup test files
			tt.setupFiles(t, tempDir)

			// Create logger
			logger := testr.New(t)

			// Create file paths for this test
			namespaceFilePath := filepath.Join(tempDir, "namespace")
			podGangFilePath := filepath.Join(tempDir, "podgangname")

			// Call the actual function under test
			state, err := NewPodCliqueStateWithPaths(tt.podCliqueDependencies, namespaceFilePath, podGangFilePath, logger)

			// Verify error expectation
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify state
			if state.namespace != tt.expectedNamespace {
				t.Errorf("expected namespace %q, got %q", tt.expectedNamespace, state.namespace)
			}

			if state.podGang != tt.expectedPodGang {
				t.Errorf("expected podGang %q, got %q", tt.expectedPodGang, state.podGang)
			}

			if !reflect.DeepEqual(state.pclqFQNToMinAvailable, tt.expectedPclqFQNToMinAvail) {
				t.Errorf("expected pclqFQNToMinAvailable %v, got %v", tt.expectedPclqFQNToMinAvail, state.pclqFQNToMinAvailable)
			}

			if !reflect.DeepEqual(state.currentPCLQReadyPods, tt.expectedCurrentPCLQReady) {
				t.Errorf("expected currentPCLQReadyPods %v, got %v", tt.expectedCurrentPCLQReady, state.currentPCLQReadyPods)
			}

			// Verify channel is initialized
			if state.allReadyCh == nil {
				t.Error("allReadyCh should be initialized")
			}
		})
	}
}

// TestGetLabelSelectorForPods tests the getLabelSelectorForPods helper function
// which creates Kubernetes label selectors for filtering pods by PodGang name.
// It verifies correct label map generation for various input scenarios.
func TestGetLabelSelectorForPods(t *testing.T) {
	tests := []struct {
		// Test case identifier for clear test output and debugging
		name string
		// Input PodGang name to generate label selector for
		podGangName string
		// Expected label map that should be returned by getLabelSelectorForPods()
		expected map[string]string
	}{
		{
			// Test Case: Basic podgang name selector creation
			// Expectation: Should return a label map with the correct Grove PodGang label key-value pair
			name:        "basic podgang name",
			podGangName: "test-podgang",
			expected: map[string]string{
				apicommon.LabelPodGang: "test-podgang",
			},
		},
		{
			// Test Case: Empty podgang name handling
			// Expectation: Should handle empty strings gracefully and return empty value for the label
			name:        "empty podgang name",
			podGangName: "",
			expected: map[string]string{
				apicommon.LabelPodGang: "",
			},
		},
		{
			// Test Case: PodGang name with special characters
			// Expectation: Should preserve special characters and numbers in the label value
			name:        "podgang with special characters",
			podGangName: "test-podgang-123_special",
			expected: map[string]string{
				apicommon.LabelPodGang: "test-podgang-123_special",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getLabelSelectorForPods(tt.podGangName)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestCheckAllParentsReady tests the checkAllParentsReady method which determines
// if all parent PodCliques have met their minimum ready pod requirements.
// It verifies the readiness logic across various dependency scenarios.
func TestCheckAllParentsReady(t *testing.T) {
	tests := []struct {
		// Test case identifier for clear test output and debugging
		name string
		// Map of parent PodClique names to their minimum required ready pod counts
		pclqFQNToMinAvail map[string]int
		// Current state of ready pods for each parent PodClique (sets of pod names)
		currentPCLQReadyPods map[string]sets.Set[string]
		// Expected boolean result from checkAllParentsReady() function
		expected bool
	}{
		{
			// Test Case: All parent PodCliques meet their minimum ready pod requirements
			// Expectation: Should return true when all parents have sufficient ready pods
			name: "all parents ready",
			pclqFQNToMinAvail: map[string]int{
				"parent-1": 2,
				"parent-2": 1,
			},
			currentPCLQReadyPods: map[string]sets.Set[string]{
				"parent-1": sets.New("pod-1", "pod-2"),
				"parent-2": sets.New("pod-3"),
			},
			expected: true,
		},
		{
			// Test Case: One parent PodClique does not meet minimum ready pod requirements
			// Expectation: Should return false when any parent lacks sufficient ready pods
			name: "one parent not ready",
			pclqFQNToMinAvail: map[string]int{
				"parent-1": 2,
				"parent-2": 2,
			},
			currentPCLQReadyPods: map[string]sets.Set[string]{
				"parent-1": sets.New("pod-1", "pod-2"),
				"parent-2": sets.New("pod-3"), // Only 1 ready, needs 2
			},
			expected: false,
		},
		{
			// Test Case: Parent PodClique exceeds minimum requirements
			// Expectation: Should return true when ready pods exceed minimum (not just meet)
			name: "exceeds minimum requirements",
			pclqFQNToMinAvail: map[string]int{
				"parent-1": 1,
			},
			currentPCLQReadyPods: map[string]sets.Set[string]{
				"parent-1": sets.New("pod-1", "pod-2", "pod-3"), // 3 ready, needs only 1
			},
			expected: true,
		},
		{
			// Test Case: No parent dependencies defined
			// Expectation: Should return true when there are no dependencies to wait for
			name:                 "empty dependencies",
			pclqFQNToMinAvail:    map[string]int{},
			currentPCLQReadyPods: map[string]sets.Set[string]{},
			expected:             true,
		},
		{
			// Test Case: Parent PodClique with zero minimum requirement
			// Expectation: Should return true when minimum requirement is 0 (edge case)
			name: "zero minimum requirement",
			pclqFQNToMinAvail: map[string]int{
				"parent-1": 0,
			},
			currentPCLQReadyPods: map[string]sets.Set[string]{
				"parent-1": sets.New[string](), // Empty set, but 0 required
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &ParentPodCliqueDependencies{
				pclqFQNToMinAvailable: tt.pclqFQNToMinAvail,
				currentPCLQReadyPods:  tt.currentPCLQReadyPods,
			}

			result := state.checkAllParentsReady()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestRefreshReadyPodsOfPodClique tests the refreshReadyPodsOfPodClique method
// which updates the internal tracking of ready pods based on pod events.
// It verifies pod addition, removal, and readiness state changes.
func TestRefreshReadyPodsOfPodClique(t *testing.T) {
	tests := []struct {
		// Test case identifier for clear test output and debugging
		name string
		// Initial state of ready pod sets before the operation
		initialReadyPods map[string]sets.Set[string]
		// Map of parent PodClique names to minimum required ready pods (for state setup)
		pclqFQNToMinAvail map[string]int
		// Pod object to process (either add/update or delete based on deletionEvent flag)
		pod *corev1.Pod
		// Whether this is a pod deletion event (true) or add/update event (false)
		deletionEvent bool
		// Expected state of ready pod sets after the operation completes
		expectedReadyPods map[string]sets.Set[string]
	}{
		{
			// Test Case: Adding a ready pod to the tracking system
			// Expectation: Pod should be added to the ready set when it has Ready=True condition
			name: "add ready pod",
			initialReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](),
			},
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 1,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "parent-pclq-1-pod-0",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			deletionEvent: false,
			expectedReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New("parent-pclq-1-pod-0"),
			},
		},
		{
			// Test Case: Adding a non-ready pod to the tracking system
			// Expectation: Pod should NOT be added to ready set when Ready=False or missing
			name: "add non-ready pod",
			initialReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](),
			},
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 1,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "parent-pclq-1-pod-0",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			deletionEvent: false,
			expectedReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](),
			},
		},
		{
			// Test Case: Removing a pod from tracking due to deletion event
			// Expectation: Pod should be removed from ready set regardless of its status
			name: "delete pod",
			initialReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New("parent-pclq-1-pod-0"),
			},
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 1,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "parent-pclq-1-pod-0",
				},
			},
			deletionEvent: true,
			expectedReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](),
			},
		},
		{
			// Test Case: Pod from a PodClique that is not being tracked
			// Expectation: Should ignore pods that don't match any tracked parent PodClique names
			name: "pod not from tracked parent",
			initialReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](),
			},
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 1,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-pclq-pod-0", // Different prefix - not tracked
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			deletionEvent: false,
			expectedReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](), // Should remain unchanged
			},
		},
		{
			// Test Case: Pod without Ready condition in its status
			// Expectation: Should not be added to ready set when Ready condition is absent
			name: "pod with no ready condition",
			initialReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](),
			},
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 1,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "parent-pclq-1-pod-0",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						// No ready condition present
					},
				},
			},
			deletionEvent: false,
			expectedReadyPods: map[string]sets.Set[string]{
				"parent-pclq-1": sets.New[string](), // Should remain empty
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &ParentPodCliqueDependencies{
				pclqFQNToMinAvailable: tt.pclqFQNToMinAvail,
				currentPCLQReadyPods:  tt.initialReadyPods,
			}

			state.refreshReadyPodsOfPodClique(tt.pod, tt.deletionEvent)

			if !reflect.DeepEqual(state.currentPCLQReadyPods, tt.expectedReadyPods) {
				t.Errorf("expected ready pods %v, got %v", tt.expectedReadyPods, state.currentPCLQReadyPods)
			}
		})
	}
}

// TestWaitForReady tests the WaitForReady method using a fake Kubernetes client.
// It verifies that the system correctly waits for parent PodClique dependencies to be satisfied
// through pod creation, updates, and handles timeout scenarios appropriately.
func TestWaitForReady(t *testing.T) {
	tests := []struct {
		// Test case identifier for clear test output and debugging
		name string
		// Map of parent PodClique names to minimum required ready pods for dependencies
		pclqFQNToMinAvail map[string]int
		// Pods that exist in the fake Kubernetes cluster at the start of the test
		initialPods []*corev1.Pod
		// Pods to be added to the cluster during the test (simulates pod creation events)
		podsToAdd []*corev1.Pod
		// Pods to be updated in the cluster during the test (simulates pod status changes)
		podsToUpdate []*corev1.Pod
		// Pods to be deleted from the cluster during the test (simulates pod deletion events)
		podsToDelete []*corev1.Pod
		// Maximum time to wait before considering the test a timeout
		contextTimeout time.Duration
		// Whether this test case should result in an error
		expectError bool
		// Whether the expected error should be a context cancellation/timeout
		expectContextCanceled bool
	}{
		{
			// Test Case: All dependencies are already satisfied at start
			// Expectation: Should return immediately without waiting
			name: "dependencies already satisfied",
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 1,
			},
			initialPods: []*corev1.Pod{
				createTestPod("parent-pclq-1-pod-0", "test-namespace", "test-podgang", true),
			},
			contextTimeout: 5 * time.Second,
			expectError:    false,
		},
		{
			// Test Case: Dependencies become satisfied after adding a new pod
			// Expectation: Should wait until new pod is added and becomes ready
			name: "dependencies satisfied after pod added",
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 2,
			},
			initialPods: []*corev1.Pod{
				createTestPod("parent-pclq-1-pod-0", "test-namespace", "test-podgang", true),
			},
			podsToAdd: []*corev1.Pod{
				createTestPod("parent-pclq-1-pod-1", "test-namespace", "test-podgang", true),
			},
			contextTimeout: 5 * time.Second,
			expectError:    false,
		},
		{
			// Test Case: Dependencies become satisfied after updating a pod to ready state
			// Expectation: Should wait until existing non-ready pod becomes ready
			name: "dependencies satisfied after pod updated",
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 1,
			},
			initialPods: []*corev1.Pod{
				createTestPod("parent-pclq-1-pod-0", "test-namespace", "test-podgang", false), // Not ready initially
			},
			podsToUpdate: []*corev1.Pod{
				createTestPod("parent-pclq-1-pod-0", "test-namespace", "test-podgang", true), // Updated to ready state
			},
			contextTimeout: 5 * time.Second,
			expectError:    false,
		},
		{
			// Test Case: Context timeout before dependencies are satisfied
			// Expectation: Should return context deadline exceeded error when timeout occurs
			name: "context timeout",
			pclqFQNToMinAvail: map[string]int{
				"parent-pclq-1": 2,
			},
			initialPods: []*corev1.Pod{
				createTestPod("parent-pclq-1-pod-0", "test-namespace", "test-podgang", true),
			},
			// Intentionally don't add the second required pod to trigger timeout
			contextTimeout:        100 * time.Millisecond,
			expectError:           true,
			expectContextCanceled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client with initial pods
			fakeClient := fake.NewSimpleClientset()
			for _, pod := range tt.initialPods {
				_, err := fakeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("failed to create initial pod: %v", err)
				}
			}

			// Create state with mocked file reads
			state := &ParentPodCliqueDependencies{
				namespace:             "test-namespace",
				podGang:               "test-podgang",
				pclqFQNToMinAvailable: tt.pclqFQNToMinAvail,
				currentPCLQReadyPods:  make(map[string]sets.Set[string]),
				allReadyCh:            make(chan struct{}, len(tt.pclqFQNToMinAvail)),
			}

			// Initialize empty sets for tracking
			for pclqName := range tt.pclqFQNToMinAvail {
				state.currentPCLQReadyPods[pclqName] = sets.New[string]()
			}

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), tt.contextTimeout)
			defer cancel()

			logger := testr.New(t)

			// Start WaitForReady in a goroutine using the fake client
			errCh := make(chan error, 1)
			go func() {
				// Now we can directly test WaitForReady with our fake client
				err := state.WaitForReady(ctx, fakeClient, logger)
				errCh <- err
			}()

			// Give some time for the informers to start
			time.Sleep(50 * time.Millisecond)

			// Simulate pod events
			for _, pod := range tt.podsToAdd {
				_, err := fakeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("failed to add pod: %v", err)
				}
			}

			for _, pod := range tt.podsToUpdate {
				_, err := fakeClient.CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("failed to update pod: %v", err)
				}
			}

			for _, pod := range tt.podsToDelete {
				err := fakeClient.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
				if err != nil {
					t.Fatalf("failed to delete pod: %v", err)
				}
			}

			// Wait for result
			select {
			case err := <-errCh:
				if tt.expectError {
					if err == nil {
						t.Error("expected error but got none")
					} else if tt.expectContextCanceled && err != context.DeadlineExceeded {
						t.Errorf("expected context.DeadlineExceeded, got %v", err)
					}
				} else if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			case <-time.After(tt.contextTimeout + 500*time.Millisecond):
				t.Error("test timed out waiting for WaitForReady to complete")
			}
		})
	}
}

// TestRegisterEventHandler tests the registerEventHandler method which sets up
// Kubernetes informer event handlers for pod lifecycle events (Add, Update, Delete).
// It verifies successful registration without errors.
func TestRegisterEventHandler(t *testing.T) {
	// Test Case: Successful registration of pod event handlers
	// Expectation: Should register Add, Update, and Delete handlers without error

	// Create a fake client and factory
	fakeClient := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(fakeClient, time.Second)

	// Create test state
	state := &ParentPodCliqueDependencies{
		pclqFQNToMinAvailable: map[string]int{
			"parent-pclq-1": 1,
		},
		currentPCLQReadyPods: map[string]sets.Set[string]{
			"parent-pclq-1": sets.New[string](),
		},
		allReadyCh: make(chan struct{}, 1),
	}

	logger := testr.New(t)

	// Test successful registration
	err := state.registerEventHandler(factory, logger)
	if err != nil {
		t.Errorf("unexpected error registering event handler: %v", err)
	}
}

// Helper function to create test pods with specified readiness state
// Creates pods with the appropriate Grove labels and Ready condition
func createTestPod(name, namespace, podGang string, ready bool) *corev1.Pod {
	conditions := []corev1.PodCondition{}
	if ready {
		conditions = append(conditions, corev1.PodCondition{
			Type:   corev1.PodReady,
			Status: corev1.ConditionTrue,
		})
	} else {
		conditions = append(conditions, corev1.PodCondition{
			Type:   corev1.PodReady,
			Status: corev1.ConditionFalse,
		})
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				apicommon.LabelPodGang: podGang,
			},
		},
		Status: corev1.PodStatus{
			Conditions: conditions,
		},
	}
}

// BenchmarkCheckAllParentsReady benchmarks the performance of the checkAllParentsReady method
// with a large number of parent PodCliques to ensure it scales efficiently.
// It measures the time taken to check readiness across 100+ parent dependencies.
func BenchmarkCheckAllParentsReady(b *testing.B) {
	// Setup a state with many parent PodCliques
	pclqFQNToMinAvail := make(map[string]int)
	currentPCLQReadyPods := make(map[string]sets.Set[string])

	for i := 0; i < 100; i++ {
		pclqName := fmt.Sprintf("parent-pclq-%d", i)
		pclqFQNToMinAvail[pclqName] = 5
		readyPods := sets.New[string]()
		for j := 0; j < 5; j++ {
			readyPods.Insert(fmt.Sprintf("%s-pod-%d", pclqName, j))
		}
		currentPCLQReadyPods[pclqName] = readyPods
	}

	state := &ParentPodCliqueDependencies{
		pclqFQNToMinAvailable: pclqFQNToMinAvail,
		currentPCLQReadyPods:  currentPCLQReadyPods,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.checkAllParentsReady()
	}
}

// TestConcurrentAccess tests thread safety of the ParentPodCliqueDependencies state
// when accessed concurrently by multiple goroutines performing pod updates and readiness checks.
// It verifies that no race conditions occur during concurrent operations.
func TestConcurrentAccess(t *testing.T) {
	state := &ParentPodCliqueDependencies{
		pclqFQNToMinAvailable: map[string]int{
			"parent-pclq-1": 2,
		},
		currentPCLQReadyPods: map[string]sets.Set[string]{
			"parent-pclq-1": sets.New[string](),
		},
		allReadyCh: make(chan struct{}, 1),
	}
	done := make(chan struct{})

	// Concurrent readiness checks
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				state.checkAllParentsReady()
			}
		}
	}()

	// Simulate concurrent pod updates
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			pod := createTestPod(fmt.Sprintf("parent-pclq-1-pod-%d", i%3), "test-ns", "test-gang", i%2 == 0)
			state.refreshReadyPodsOfPodClique(pod, false)
		}
	}()

	// Wait for completion
	<-done
}
