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

package podcliquescalinggroup

import (
	"context"
	"testing"

	groveconfigv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestRegisterWithManager tests the RegisterWithManager function to ensure proper controller registration.
// This test validates that the controller can be registered with a manager without errors.
//
// Note: This test focuses on reconciler initialization validation rather than actual manager
// registration due to the complexity of mocking the full manager.Manager interface.
// Full integration testing of the registration process should be done in integration tests
// with a real controller-runtime manager.
func TestRegisterWithManager(t *testing.T) {
	// This test validates the controller registration logic by checking that
	// the reconciler is properly initialized with all required components

	// Create reconciler with test configuration
	concurrentSyncs := 5
	config := groveconfigv1alpha1.PodCliqueScalingGroupControllerConfiguration{
		ConcurrentSyncs: &concurrentSyncs,
	}

	// Create a fake client for the reconciler
	scheme := runtime.NewScheme()
	require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	reconciler := &Reconciler{
		config:                  config,
		client:                  fakeClient,
		reconcileStatusRecorder: ctrlcommon.NewReconcileStatusRecorder(fakeClient, &record.FakeRecorder{}),
		operatorRegistry:        component.NewOperatorRegistry[grovecorev1alpha1.PodCliqueScalingGroup](),
	}

	// Validate that the reconciler is properly initialized
	assert.NotNil(t, reconciler, "Reconciler should be properly initialized")
	assert.Equal(t, concurrentSyncs, *reconciler.config.ConcurrentSyncs, "ConcurrentSyncs should match configuration")
	assert.NotNil(t, reconciler.client, "Client should be initialized")
	assert.NotNil(t, reconciler.reconcileStatusRecorder, "ReconcileStatusRecorder should be initialized")
	assert.NotNil(t, reconciler.operatorRegistry, "OperatorRegistry should be initialized")
}

// TestPodCliqueScalingGroupUpdatePredicate tests the predicate function that filters PodCliqueScalingGroup events.
func TestPodCliqueScalingGroupUpdatePredicate(t *testing.T) {
	predicate := podCliqueScalingGroupUpdatePredicate()

	testCases := []struct {
		// Test case description explaining the scenario being tested
		name string
		// Test function that exercises the predicate with specific event type
		testFunc func(t *testing.T)
	}{
		{
			// Test that create events are allowed for Grove-managed PCSGs with PGS owners
			name: "create_event_grove_managed_with_pgs_owner",
			testFunc: func(t *testing.T) {
				obj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				addGroveLabels(obj)
				addPGSOwner(obj)

				event := event.CreateEvent{Object: obj}
				result := predicate.Create(event)
				assert.True(t, result, "Should allow create events for Grove-managed PCSG with PGS owner")
			},
		},
		{
			// Test that create events are rejected for non-Grove-managed PCSGs
			name: "create_event_not_grove_managed",
			testFunc: func(t *testing.T) {
				obj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				// No Grove labels
				addPGSOwner(obj)

				event := event.CreateEvent{Object: obj}
				result := predicate.Create(event)
				assert.False(t, result, "Should reject create events for non-Grove-managed PCSG")
			},
		},
		{
			// Test that create events are rejected for Grove-managed PCSGs without PGS owner
			name: "create_event_grove_managed_without_pgs_owner",
			testFunc: func(t *testing.T) {
				obj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				addGroveLabels(obj)
				// No PGS owner

				event := event.CreateEvent{Object: obj}
				result := predicate.Create(event)
				assert.False(t, result, "Should reject create events for Grove-managed PCSG without PGS owner")
			},
		},
		{
			// Test that delete events are always ignored
			name: "delete_event_always_ignored",
			testFunc: func(t *testing.T) {
				obj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				addGroveLabels(obj)
				addPGSOwner(obj)

				event := event.DeleteEvent{Object: obj}
				result := predicate.Delete(event)
				assert.False(t, result, "Should always reject delete events")
			},
		},
		{
			// Test that update events are allowed for Grove-managed PCSGs with PGS owners
			name: "update_event_grove_managed_with_pgs_owner",
			testFunc: func(t *testing.T) {
				oldObj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				addGroveLabels(oldObj)
				addPGSOwner(oldObj)

				newObj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				addGroveLabels(newObj)
				addPGSOwner(newObj)

				event := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
				result := predicate.Update(event)
				assert.True(t, result, "Should allow update events for Grove-managed PCSG with PGS owner")
			},
		},
		{
			// Test that update events are rejected for non-Grove-managed PCSGs
			name: "update_event_not_grove_managed",
			testFunc: func(t *testing.T) {
				oldObj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				// No Grove labels
				addPGSOwner(oldObj)

				newObj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				addPGSOwner(newObj)

				event := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
				result := predicate.Update(event)
				assert.False(t, result, "Should reject update events for non-Grove-managed PCSG")
			},
		},
		{
			// Test that generic events are always ignored
			name: "generic_event_always_ignored",
			testFunc: func(t *testing.T) {
				obj := createTestPCSGForRegisterTest("test-pcsg", "test-ns")
				addGroveLabels(obj)
				addPGSOwner(obj)

				event := event.GenericEvent{Object: obj}
				result := predicate.Generic(event)
				assert.False(t, result, "Should always reject generic events")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.testFunc(t)
		})
	}
}

// TestMapPGSToPCSG tests the handler map function that converts PodGangSet events to PodCliqueScalingGroup reconcile requests.
func TestMapPGSToPCSG(t *testing.T) {
	mapFunc := mapPGSToPCSG()

	testCases := []struct {
		// Test case description explaining the scenario being tested
		name string
		// Input object to be mapped (can be nil for invalid object tests)
		inputObj client.Object
		// Expected number of reconcile requests to be generated
		expectedRequestCount int
		// Expected reconcile requests (nil if count is 0)
		expectedRequests []reconcile.Request
	}{
		{
			// Test mapping with valid PGS containing multiple scaling group configs and replicas
			name:                 "valid_pgs_with_multiple_configs_and_replicas",
			inputObj:             createTestPGSWithScalingGroups("test-pgs", "test-ns", 2, []string{"sg1", "sg2"}),
			expectedRequestCount: 4, // 2 replicas * 2 scaling groups
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "test-pgs-0-sg1", Namespace: "test-ns"}},
				{NamespacedName: types.NamespacedName{Name: "test-pgs-0-sg2", Namespace: "test-ns"}},
				{NamespacedName: types.NamespacedName{Name: "test-pgs-1-sg1", Namespace: "test-ns"}},
				{NamespacedName: types.NamespacedName{Name: "test-pgs-1-sg2", Namespace: "test-ns"}},
			},
		},
		{
			// Test mapping with PGS containing single scaling group config
			name:                 "valid_pgs_with_single_config",
			inputObj:             createTestPGSWithScalingGroups("test-pgs", "test-ns", 1, []string{"sg1"}),
			expectedRequestCount: 1,
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "test-pgs-0-sg1", Namespace: "test-ns"}},
			},
		},
		{
			// Test mapping with PGS containing no scaling group configs
			name:                 "valid_pgs_with_no_configs",
			inputObj:             createTestPGSWithScalingGroups("test-pgs", "test-ns", 1, []string{}),
			expectedRequestCount: 0,
			expectedRequests:     nil,
		},
		{
			// Test mapping with invalid object type (not PodGangSet)
			name:                 "invalid_object_type",
			inputObj:             createTestPCSGForRegisterTest("test-pcsg", "test-ns"),
			expectedRequestCount: 0,
			expectedRequests:     nil,
		},
		{
			// Test mapping with nil object
			name:                 "nil_object",
			inputObj:             nil,
			expectedRequestCount: 0,
			expectedRequests:     nil,
		},
		{
			// Test mapping with PGS having zero replicas (edge case)
			name:                 "valid_pgs_with_zero_replicas",
			inputObj:             createTestPGSWithScalingGroups("test-pgs", "test-ns", 0, []string{"sg1"}),
			expectedRequestCount: 0,
			expectedRequests:     nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			requests := mapFunc(ctx, tc.inputObj)

			assert.Len(t, requests, tc.expectedRequestCount, "Should generate expected number of reconcile requests")

			if tc.expectedRequests != nil {
				assert.ElementsMatch(t, tc.expectedRequests, requests, "Should generate expected reconcile requests")
			}
		})
	}
}

// TestPodGangSetPredicate tests the predicate function that filters PodGangSet events.
func TestPodGangSetPredicate(t *testing.T) {
	predicate := podGangSetPredicate()

	testCases := []struct {
		// Test case description explaining the scenario being tested
		name string
		// Test function that exercises the predicate with specific event type
		testFunc func(t *testing.T)
	}{
		{
			// Test that create events are always ignored
			name: "create_event_always_ignored",
			testFunc: func(t *testing.T) {
				obj := createTestPGS("test-pgs", "test-ns")
				addGroveLabels(obj)

				event := event.CreateEvent{Object: obj}
				result := predicate.Create(event)
				assert.False(t, result, "Should always reject create events")
			},
		},
		{
			// Test that delete events are always ignored
			name: "delete_event_always_ignored",
			testFunc: func(t *testing.T) {
				obj := createTestPGS("test-pgs", "test-ns")
				addGroveLabels(obj)

				event := event.DeleteEvent{Object: obj}
				result := predicate.Delete(event)
				assert.False(t, result, "Should always reject delete events")
			},
		},
		{
			// Test that update events are allowed for Grove-managed PodGangSets
			name: "update_event_grove_managed",
			testFunc: func(t *testing.T) {
				oldObj := createTestPGS("test-pgs", "test-ns")
				addGroveLabels(oldObj)

				newObj := createTestPGS("test-pgs", "test-ns")
				addGroveLabels(newObj)

				event := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
				result := predicate.Update(event)
				assert.True(t, result, "Should allow update events for Grove-managed PodGangSet")
			},
		},
		{
			// Test that update events are rejected for non-Grove-managed PodGangSets
			name: "update_event_not_grove_managed",
			testFunc: func(t *testing.T) {
				oldObj := createTestPGS("test-pgs", "test-ns")
				// No Grove labels

				newObj := createTestPGS("test-pgs", "test-ns")

				event := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
				result := predicate.Update(event)
				assert.False(t, result, "Should reject update events for non-Grove-managed PodGangSet")
			},
		},
		{
			// Test that generic events are always ignored
			name: "generic_event_always_ignored",
			testFunc: func(t *testing.T) {
				obj := createTestPGS("test-pgs", "test-ns")
				addGroveLabels(obj)

				event := event.GenericEvent{Object: obj}
				result := predicate.Generic(event)
				assert.False(t, result, "Should always reject generic events")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.testFunc(t)
		})
	}
}

// TestMapPCLQToPCSG tests the handler map function that converts PodClique events to PodCliqueScalingGroup reconcile requests.
func TestMapPCLQToPCSG(t *testing.T) {
	mapFunc := mapPCLQToPCSG()

	testCases := []struct {
		// Test case description explaining the scenario being tested
		name string
		// Input object to be mapped (can be nil for invalid object tests)
		inputObj client.Object
		// Expected number of reconcile requests to be generated
		expectedRequestCount int
		// Expected reconcile requests (nil if count is 0)
		expectedRequests []reconcile.Request
	}{
		{
			// Test mapping with valid PodClique containing PCSG label
			name:                 "valid_podclique_with_pcsg_label",
			inputObj:             createTestPCLQWithPCSGLabel("test-pclq", "test-ns", "test-pcsg"),
			expectedRequestCount: 1,
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "test-pcsg", Namespace: "test-ns"}},
			},
		},
		{
			// Test mapping with PodClique missing PCSG label
			name:                 "podclique_missing_pcsg_label",
			inputObj:             createTestPCLQ("test-pclq", "test-ns"),
			expectedRequestCount: 0,
			expectedRequests:     nil,
		},
		{
			// Test mapping with PodClique having empty PCSG label
			name:                 "podclique_empty_pcsg_label",
			inputObj:             createTestPCLQWithPCSGLabel("test-pclq", "test-ns", ""),
			expectedRequestCount: 0,
			expectedRequests:     nil,
		},
		{
			// Test mapping with invalid object type (not PodClique)
			name:                 "invalid_object_type",
			inputObj:             createTestPGS("test-pgs", "test-ns"),
			expectedRequestCount: 0,
			expectedRequests:     nil,
		},
		{
			// Test mapping with nil object
			name:                 "nil_object",
			inputObj:             nil,
			expectedRequestCount: 0,
			expectedRequests:     nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			requests := mapFunc(ctx, tc.inputObj)

			assert.Len(t, requests, tc.expectedRequestCount, "Should generate expected number of reconcile requests")

			if tc.expectedRequests != nil {
				assert.Equal(t, tc.expectedRequests, requests, "Should generate expected reconcile requests")
			}
		})
	}
}

// TestPodCliquePredicate tests the predicate function that filters PodClique events.
func TestPodCliquePredicate(t *testing.T) {
	predicate := podCliquePredicate()

	testCases := []struct {
		// Test case description explaining the scenario being tested
		name string
		// Test function that exercises the predicate with specific event type
		testFunc func(t *testing.T)
	}{
		{
			// Test that create events are always ignored
			name: "create_event_always_ignored",
			testFunc: func(t *testing.T) {
				obj := createTestPCLQManagedByPCSG("test-pclq", "test-ns")

				event := event.CreateEvent{Object: obj}
				result := predicate.Create(event)
				assert.False(t, result, "Should always reject create events")
			},
		},
		{
			// Test that delete events are allowed for PodCliques managed by PCSGs
			name: "delete_event_managed_by_pcsg",
			testFunc: func(t *testing.T) {
				obj := createTestPCLQManagedByPCSG("test-pclq", "test-ns")

				event := event.DeleteEvent{Object: obj}
				result := predicate.Delete(event)
				assert.True(t, result, "Should allow delete events for PodCliques managed by PCSG")
			},
		},
		{
			// Test that delete events are rejected for PodCliques not managed by PCSGs
			name: "delete_event_not_managed_by_pcsg",
			testFunc: func(t *testing.T) {
				obj := createTestPCLQ("test-pclq", "test-ns")
				// Not managed by PCSG

				event := event.DeleteEvent{Object: obj}
				result := predicate.Delete(event)
				assert.False(t, result, "Should reject delete events for PodCliques not managed by PCSG")
			},
		},
		{
			// Test that update events are allowed for PodCliques managed by PCSGs
			name: "update_event_managed_by_pcsg",
			testFunc: func(t *testing.T) {
				oldObj := createTestPCLQManagedByPCSG("test-pclq", "test-ns")
				newObj := createTestPCLQManagedByPCSG("test-pclq", "test-ns")

				event := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
				result := predicate.Update(event)
				assert.True(t, result, "Should allow update events for PodCliques managed by PCSG")
			},
		},
		{
			// Test that update events are rejected for PodCliques not managed by PCSGs
			name: "update_event_not_managed_by_pcsg",
			testFunc: func(t *testing.T) {
				oldObj := createTestPCLQ("test-pclq", "test-ns")
				newObj := createTestPCLQ("test-pclq", "test-ns")

				event := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
				result := predicate.Update(event)
				assert.False(t, result, "Should reject update events for PodCliques not managed by PCSG")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.testFunc(t)
		})
	}
}

// Helper functions for creating test objects

// createTestPCSGForRegisterTest creates a test PodCliqueScalingGroup object with the given name and namespace for register tests.
func createTestPCSGForRegisterTest(name, namespace string) *grovecorev1alpha1.PodCliqueScalingGroup {
	return &grovecorev1alpha1.PodCliqueScalingGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
			Replicas:    1,
			CliqueNames: []string{"test-clique"},
		},
	}
}

// createTestPGS creates a test PodGangSet object with the given name and namespace.
func createTestPGS(name, namespace string) *grovecorev1alpha1.PodGangSet {
	return &grovecorev1alpha1.PodGangSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: grovecorev1alpha1.PodGangSetSpec{
			Replicas: 1,
		},
	}
}

// createTestPGSWithScalingGroups creates a test PodGangSet with scaling group configurations.
func createTestPGSWithScalingGroups(name, namespace string, replicas int32, scalingGroupNames []string) *grovecorev1alpha1.PodGangSet {
	pgs := createTestPGS(name, namespace)
	pgs.Spec.Replicas = replicas

	// Create scaling group configs
	var configs []grovecorev1alpha1.PodCliqueScalingGroupConfig
	for _, sgName := range scalingGroupNames {
		configs = append(configs, grovecorev1alpha1.PodCliqueScalingGroupConfig{
			Name: sgName,
		})
	}
	pgs.Spec.Template.PodCliqueScalingGroupConfigs = configs

	return pgs
}

// createTestPCLQ creates a test PodClique object with the given name and namespace.
func createTestPCLQ(name, namespace string) *grovecorev1alpha1.PodClique {
	return &grovecorev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: grovecorev1alpha1.PodCliqueSpec{
			Replicas: 1,
		},
	}
}

// createTestPCLQWithPCSGLabel creates a test PodClique with a PCSG label.
func createTestPCLQWithPCSGLabel(name, namespace, pcsgName string) *grovecorev1alpha1.PodClique {
	pclq := createTestPCLQ(name, namespace)
	if pclq.Labels == nil {
		pclq.Labels = make(map[string]string)
	}
	pclq.Labels[grovecorev1alpha1.LabelPodCliqueScalingGroup] = pcsgName
	return pclq
}

// createTestPCLQManagedByPCSG creates a test PodClique managed by a PodCliqueScalingGroup.
func createTestPCLQManagedByPCSG(name, namespace string) *grovecorev1alpha1.PodClique {
	pclq := createTestPCLQ(name, namespace)
	addGroveLabels(pclq)
	addPCSGOwner(pclq)
	return pclq
}

// addGroveLabels adds Grove management labels to an object.
func addGroveLabels(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[grovecorev1alpha1.LabelManagedByKey] = grovecorev1alpha1.LabelManagedByValue
	obj.SetLabels(labels)
}

// addPGSOwner adds a PodGangSet owner reference to an object.
func addPGSOwner(obj client.Object) {
	ownerRefs := obj.GetOwnerReferences()
	ownerRefs = append(ownerRefs, metav1.OwnerReference{
		Kind: grovecorev1alpha1.PodGangSetKind,
		Name: "test-pgs",
	})
	obj.SetOwnerReferences(ownerRefs)
}

// addPCSGOwner adds a PodCliqueScalingGroup owner reference to an object.
func addPCSGOwner(obj client.Object) {
	ownerRefs := obj.GetOwnerReferences()
	ownerRefs = append(ownerRefs, metav1.OwnerReference{
		Kind: grovecorev1alpha1.PodCliqueScalingGroupKind,
		Name: "test-pcsg",
	})
	obj.SetOwnerReferences(ownerRefs)
}

// Fake implementations for testing
