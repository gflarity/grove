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

// Package internal implements initialization container functionality for waiting on PodClique dependencies.
// This package provides mechanisms for init containers to wait for parent PodCliques to become ready
// before allowing their own pods to start, enabling controlled startup ordering.
package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/common"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Error codes for various failure scenarios during PodClique dependency waiting.
const (
	// errCodeLabelSelectorCreationForPods indicates failure to create label selector for pod filtering.
	errCodeLabelSelectorCreationForPods grovecorev1alpha1.ErrorCode = "ERR_LABEL_SELECTOR_CREATION_FOR_PODS"
	// errCodeRegisterEventHandler indicates failure to register Kubernetes event handlers.
	errCodeRegisterEventHandler grovecorev1alpha1.ErrorCode = "ERR_REGISTER_EVENT_HANDLER"
)

const (
	// operationWaitForParentPodClique identifies the operation context for error reporting.
	operationWaitForParentPodClique = "WaitForParentPodClique"
)

// ParentPodCliqueDependencies tracks the readiness state of parent PodCliques that must be ready
// before the current pod can start. It monitors pod events and signals when all dependencies are satisfied.
type ParentPodCliqueDependencies struct {
	// mutex protects concurrent access to the readiness state maps.
	mutex sync.Mutex
	// namespace is the Kubernetes namespace where pods are monitored.
	namespace string
	// podGang is the name of the PodGang this dependency tracker belongs to.
	podGang string
	// pclqFQNToMinAvailable maps parent PodClique fully qualified names to minimum required ready pods.
	pclqFQNToMinAvailable map[string]int
	// currentPCLQReadyPods tracks currently ready pods for each parent PodClique.
	currentPCLQReadyPods map[string]sets.Set[string]
	// allReadyCh signals when all parent PodCliques have met their minimum ready pod requirements.
	allReadyCh chan struct{}
}

// NewPodCliqueStateWithPaths creates and initializes a ParentPodCliqueDependencies tracker.
// It reads the pod namespace and PodGang name from the specified file paths and initializes
// all parent PodCliques in an unready state.
//
// podCliqueDependencies maps parent PodClique names to minimum required ready pods.
// namespaceFilePath is the path to the file containing the pod namespace.
// podGangFilePath is the path to the file containing the PodGang name.
// Returns an error if pod metadata files cannot be read.
func NewPodCliqueStateWithPaths(podCliqueDependencies map[string]int, namespaceFilePath, podGangFilePath string, log logr.Logger) (*ParentPodCliqueDependencies, error) {
	// Read pod namespace from specified file path
	podNamespace, err := os.ReadFile(namespaceFilePath)
	if err != nil {
		log.Error(err, "Failed to read the pod namespace from the file", "filepath", namespaceFilePath)
		return nil, err
	}

	// Read PodGang name from specified file path
	podGangName, err := os.ReadFile(podGangFilePath)
	if err != nil {
		log.Error(err, "Failed to read the PodGang name from the file", "filepath", podGangFilePath)
		return nil, err
	}

	// Initialize empty sets for tracking ready pods for each parent PodClique
	currentlyReadyPods := make(map[string]sets.Set[string])
	// Initialize the keys to indicate these are the parent PodCliques.
	for parentPodCliqueName := range podCliqueDependencies {
		currentlyReadyPods[parentPodCliqueName] = sets.New[string]()
	}

	// Construct the dependency tracker with all required state
	state := &ParentPodCliqueDependencies{
		namespace:             string(podNamespace),
		podGang:               string(podGangName),
		pclqFQNToMinAvailable: podCliqueDependencies,
		currentPCLQReadyPods:  currentlyReadyPods,
		allReadyCh:            make(chan struct{}, len(podCliqueDependencies)),
	}

	return state, nil
}

// NewPodCliqueState creates and initializes a ParentPodCliqueDependencies tracker.
// It reads the pod namespace and PodGang name from the default mounted file locations
// and initializes all parent PodCliques in an unready state.
func NewPodCliqueState(podCliqueDependencies map[string]int, log logr.Logger) (*ParentPodCliqueDependencies, error) {
	// Use default downward API mounted file paths
	podNamespaceFilePath := filepath.Join(common.VolumeMountPathPodInfo, common.PodNamespaceFileName)
	podGangNameFilePath := filepath.Join(common.VolumeMountPathPodInfo, common.PodGangNameFileName)

	return NewPodCliqueStateWithPaths(podCliqueDependencies, podNamespaceFilePath, podGangNameFilePath, log)
}

// WaitForReady blocks until all parent PodClique dependencies are ready or the context is cancelled.
// It establishes a Kubernetes informer to watch pod events and tracks readiness state changes using the provided client.
// Returns nil when all dependencies are satisfied, or an error if the wait fails or times out.
func (c *ParentPodCliqueDependencies) WaitForReady(ctx context.Context, client kubernetes.Interface, log logr.Logger) error {
	// Ensure channel cleanup when function exits to prevent goroutine leaks
	defer close(c.allReadyCh) // Close the channel the informers write to *after* the context they use is cancelled.

	log.Info("Parent PodClique(s) being waited on", "pclqFQNToMinAvailable", c.pclqFQNToMinAvailable)

	// Build label selector to filter pods by PodGang
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		// Get labels that identify pods in this PodGang
		MatchLabels: getLabelSelectorForPods(c.podGang),
	})
	if err != nil {
		// Wrap error with context for debugging
		return groveerr.WrapError(
			err,
			errCodeLabelSelectorCreationForPods,
			operationWaitForParentPodClique,
			"failed to convert labels required for the PodGang to selector",
		)
	}

	// Create separate context for event handlers to enable cleanup control
	eventHandlerContext, cancel := context.WithCancel(ctx)
	defer cancel() // Cancel the context used by the informers if the wait is successful, or an err occurs.

	// Set up shared informer factory with namespace and label filtering
	factory := informers.NewSharedInformerFactoryWithOptions(
		client,
		time.Second, // 1 second resync period for cache refresh
		// Limit to specific namespace
		informers.WithNamespace(c.namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			// Apply label selector to watch only relevant pods
			opts.LabelSelector = selector.String()
		},
		),
	)
	// Register handlers for pod lifecycle events
	if err := c.registerEventHandler(factory, log); err != nil {
		return groveerr.WrapError(
			err,
			errCodeRegisterEventHandler,
			operationWaitForParentPodClique,
			"failed to register the Pod event handler",
		)
	}

	// Wait for cache sync to ensure up-to-date view of existing pods
	factory.WaitForCacheSync(eventHandlerContext.Done())
	// Start informer to begin watching for pod events
	factory.Start(eventHandlerContext.Done())

	// Block until dependencies are ready or context is cancelled
	select {
	case <-c.allReadyCh:
		// All parent PodCliques are ready - success case
		return nil
	case <-ctx.Done():
		// Context cancelled (timeout or external cancellation)
		return ctx.Err()
	}
}

// registerEventHandler registers pod event handlers with the shared informer factory.
// The handlers track pod additions, updates, and deletions to maintain readiness state
// and signal when all parent dependencies are satisfied.
func (c *ParentPodCliqueDependencies) registerEventHandler(factory informers.SharedInformerFactory, log logr.Logger) error {
	// Get the pod informer from the factory and register event handlers for pod lifecycle events
	typedInformer := factory.Core().V1().Pods().Informer()
	_, err := typedInformer.AddEventHandlerWithOptions(cache.ResourceEventHandlerFuncs{
		// AddFunc handle new pod creation events
		AddFunc: func(obj any) {
			// Protect concurrent access to readiness state
			c.mutex.Lock()
			defer c.mutex.Unlock()
			// Type assert to ensure valid pod object
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				// Currently we silently ignore invalid objects
				return
			}

			// Update readiness tracking (not a deletion)
			c.refreshReadyPodsOfPodClique(pod, false)
			// Check if all dependencies are now satisfied
			if c.checkAllParentsReady() {
				// Signal success - non-blocking due to buffered channel
				c.allReadyCh <- struct{}{}
			}
		},
		// Handle pod update events (status changes, etc.)
		UpdateFunc: func(_, newObj any) {
			// Protect concurrent access to readiness state
			c.mutex.Lock()
			defer c.mutex.Unlock()
			// Type assert - only care about new object
			pod, ok := newObj.(*corev1.Pod)
			if !ok {
				// Silently ignore invalid objects
				return
			}

			// Update readiness tracking (not a deletion)
			c.refreshReadyPodsOfPodClique(pod, false)
			// Check if all dependencies are now satisfied
			if c.checkAllParentsReady() {
				// Signal success - non-blocking due to buffered channel
				c.allReadyCh <- struct{}{}
			}
		},
		// Handle pod deletion events
		DeleteFunc: func(obj any) {
			// Protect concurrent access to readiness state
			c.mutex.Lock()
			defer c.mutex.Unlock()
			// Handle tombstone objects for deleted resources
			if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				// Extract original object from tombstone
				obj = tombstone.Obj
			}
			// Type assert to ensure valid pod object
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				// Silently ignore invalid objects
				return
			}

			// Remove pod from readiness tracking (deletion event)
			c.refreshReadyPodsOfPodClique(pod, true)
			// Check if dependencies still satisfied after deletion
			if c.checkAllParentsReady() {
				// Signal success - non-blocking due to buffered channel
				c.allReadyCh <- struct{}{}
			}
		},
	}, cache.HandlerOptions{Logger: &log})
	return err
}

// refreshReadyPodsOfPodClique updates the readiness tracking for a pod belonging to a parent PodClique.
// It determines which PodClique the pod belongs to by name prefix matching and updates the ready pod set.
// If deletionEvent is true, the pod is removed from tracking; otherwise readiness is evaluated.
func (c *ParentPodCliqueDependencies) refreshReadyPodsOfPodClique(pod *corev1.Pod, deletionEvent bool) {
	// Find which parent PodClique this pod belongs to by matching name prefixes
	// PodClique names are used as prefixes for their member pod names
	podCliqueName, ok := lo.Find(lo.Keys(c.pclqFQNToMinAvailable), func(podCliqueFQN string) bool {
		return strings.HasPrefix(pod.Name, podCliqueFQN)
	})
	if !ok {
		return // If not found, the Pod is not related to any parent PodClique.
	}

	// Handle pod deletion by removing from ready tracking
	if deletionEvent {
		c.currentPCLQReadyPods[podCliqueName].Delete(pod.Name)
		return
	}

	// Find the Ready condition in the pod's status
	readyCondition, ok := lo.Find(pod.Status.Conditions, func(podCondition corev1.PodCondition) bool {
		return podCondition.Type == corev1.PodReady
	})
	// Determine if pod is actually ready (condition exists and is True)
	// Both condition existence AND True status are required
	podReady := ok && readyCondition.Status == corev1.ConditionTrue

	// Update the ready pod tracking based on current readiness state
	if podReady {
		c.currentPCLQReadyPods[podCliqueName].Insert(pod.Name)
	} else {
		c.currentPCLQReadyPods[podCliqueName].Delete(pod.Name)
	}
}

// checkAllParentsReady returns true if all parent PodCliques have met their minimum ready pod requirements.
// It compares the current count of ready pods against the required minimum for each parent PodClique.
func (c *ParentPodCliqueDependencies) checkAllParentsReady() bool {
	// Iterate through all parent PodCliques to verify readiness requirements
	// All parents must be ready for this function to return true
	for cliqueName, readyPods := range c.currentPCLQReadyPods {
		if len(readyPods) < c.pclqFQNToMinAvailable[cliqueName] {
			return false // If any single parent is not ready, wait.
		}
	}
	return true
}

// getLabelSelectorForPods returns a label selector map for filtering pods by PodGang name.
// This selector is used to watch only pods that belong to the specified PodGang.
func getLabelSelectorForPods(podGangName string) map[string]string {
	return map[string]string{
		// Use Grove's standard PodGang label
		grovecorev1alpha1.LabelPodGang: podGangName,
	}
}
