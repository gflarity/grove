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

package k8s

import (
	"context"
	"testing"
	"time"

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// newFakeScheme creates a runtime.Scheme that includes the types needed for the dynamic fake client.
func newFakeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	// Register the Unstructured and UnstructuredList types for dynamic client.
	// The dynamic fake client needs these to handle List operations.
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: "PodCliqueSet"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: "PodCliqueSetList"},
		&unstructured.UnstructuredList{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: "ClusterTopology"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: "ClusterTopologyList"},
		&unstructured.UnstructuredList{},
	)
	return scheme
}

// toUnstructuredClusterTopology converts a ClusterTopology to an Unstructured object.
func toUnstructuredClusterTopology(ct *corev1alpha1.ClusterTopology) *unstructured.Unstructured {
	obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(ct)
	u := &unstructured.Unstructured{Object: obj}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "grove.io",
		Version: "v1alpha1",
		Kind:    "ClusterTopology",
	})
	return u
}

// toUnstructuredPCS converts a PodCliqueSet to an Unstructured object.
func toUnstructuredPCS(pcs *corev1alpha1.PodCliqueSet) *unstructured.Unstructured {
	obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(pcs)
	u := &unstructured.Unstructured{Object: obj}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "grove.io",
		Version: "v1alpha1",
		Kind:    "PodCliqueSet",
	})
	return u
}

func TestInformerTopologyCache_StartAndSync(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newFakeScheme())

	tc := NewInformerTopologyCache(clientset, dynClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer tc.Stop()

	if !tc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false — informers did not sync")
	}
}

func TestInformerTopologyCache_SnapshotNilBeforeRebuild(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newFakeScheme())

	tc := NewInformerTopologyCache(clientset, dynClient)

	// Before Start, snapshot should be nil
	if snap := tc.Snapshot(); snap != nil {
		t.Errorf("Snapshot() before Start should be nil, got %+v", snap)
	}
}

func TestInformerTopologyCache_RebuildSnapshot(t *testing.T) {
	// Create test data: nodes, a ClusterTopology, a PCS, and pods
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"topology.io/rack":            "rack-0",
			},
		},
	}
	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-2",
			Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1b",
				"topology.io/rack":            "rack-1",
			},
		},
	}

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs-0-worker-abc",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podclique":                  "my-pcs-0-worker",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	clientset := kubefake.NewSimpleClientset(node1, node2, pod1)

	ct := &corev1alpha1.ClusterTopology{
		ObjectMeta: metav1.ObjectMeta{
			Name: corev1alpha1.DefaultClusterTopologyName,
		},
		Spec: corev1alpha1.ClusterTopologySpec{
			Levels: []corev1alpha1.TopologyLevel{
				{Domain: corev1alpha1.TopologyDomainZone, Key: "topology.kubernetes.io/zone"},
				{Domain: corev1alpha1.TopologyDomainRack, Key: "topology.io/rack"},
			},
		},
	}

	pcs := &corev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs",
			Namespace: "default",
		},
		Spec: corev1alpha1.PodCliqueSetSpec{
			Template: corev1alpha1.PodCliqueSetTemplateSpec{
				Cliques: []*corev1alpha1.PodCliqueTemplateSpec{
					{
						Name: "worker",
						TopologyConstraint: &corev1alpha1.TopologyConstraint{
							PackDomain: corev1alpha1.TopologyDomainRack,
						},
					},
				},
			},
		},
	}

	dynClient := dynamicfake.NewSimpleDynamicClient(
		newFakeScheme(),
		toUnstructuredClusterTopology(ct),
		toUnstructuredPCS(pcs),
	)

	tc := NewInformerTopologyCache(clientset, dynClient)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer tc.Stop()

	if !tc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false")
	}

	// Wait for the debounced snapshot rebuild.
	// The informer sync triggers onChange which sets a 500ms debounce timer.
	select {
	case <-tc.Updates():
		// Got the update notification
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cache update notification")
	}

	snap := tc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil after sync + update")
	}

	// Verify domains: zone, rack, N/A
	if len(snap.Domains) != 3 {
		t.Fatalf("expected 3 domains (zone, rack, N/A), got %d", len(snap.Domains))
	}
	if snap.Domains[0].Domain != "zone" {
		t.Errorf("domain[0] = %q, want %q", snap.Domains[0].Domain, "zone")
	}
	if snap.Domains[1].Domain != "rack" {
		t.Errorf("domain[1] = %q, want %q", snap.Domains[1].Domain, "rack")
	}
	if snap.Domains[2].Domain != "N/A" {
		t.Errorf("domain[2] = %q, want %q", snap.Domains[2].Domain, "N/A")
	}

	// Verify zone has 2 distinct values
	if snap.Domains[0].ValuesCount != 2 {
		t.Errorf("zone values count = %d, want 2", snap.Domains[0].ValuesCount)
	}

	// Verify node labels are present
	if len(snap.NodeLabels) != 2 {
		t.Errorf("expected 2 node entries, got %d", len(snap.NodeLabels))
	}

	// Verify pods
	if len(snap.Pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(snap.Pods))
	}
	if snap.Pods[0].Name != "my-pcs-0-worker-abc" {
		t.Errorf("pod name = %q, want %q", snap.Pods[0].Name, "my-pcs-0-worker-abc")
	}
	if snap.Pods[0].Node != "node-1" {
		t.Errorf("pod node = %q, want %q", snap.Pods[0].Node, "node-1")
	}
}

func TestInformerTopologyCache_StopClosesChannel(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newFakeScheme())

	tc := NewInformerTopologyCache(clientset, dynClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	tc.Stop()

	// The updates channel should be closed
	select {
	case _, ok := <-tc.Updates():
		if ok {
			// Might have received a pending update before close, drain one more
			_, ok = <-tc.Updates()
			if ok {
				t.Error("expected updates channel to be closed after Stop()")
			}
		}
	case <-time.After(1 * time.Second):
		t.Error("timed out reading from closed channel")
	}
}

func TestInformerTopologyCache_DoubleStopSafe(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newFakeScheme())

	tc := NewInformerTopologyCache(clientset, dynClient)

	ctx := context.Background()
	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	// Calling Stop twice should not panic
	tc.Stop()
	tc.Stop()
}

func TestInformerTopologyCache_EmptyCluster(t *testing.T) {
	// No nodes, no pods, no CRDs
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newFakeScheme())

	tc := NewInformerTopologyCache(clientset, dynClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer tc.Stop()

	if !tc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false")
	}

	// Wait for debounced rebuild
	select {
	case <-tc.Updates():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cache update notification")
	}

	snap := tc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil after sync")
	}

	// Only N/A domain when there's no ClusterTopology
	if len(snap.Domains) != 1 {
		t.Fatalf("expected 1 domain (N/A), got %d", len(snap.Domains))
	}
	if snap.Domains[0].Domain != "N/A" {
		t.Errorf("domain[0] = %q, want %q", snap.Domains[0].Domain, "N/A")
	}

	if len(snap.Pods) != 0 {
		t.Errorf("expected 0 pods, got %d", len(snap.Pods))
	}
}

func TestInformerTopologyCache_DebounceCoalesces(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newFakeScheme())

	tc := NewInformerTopologyCache(clientset, dynClient)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer tc.Stop()

	if !tc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false")
	}

	// Drain the initial update
	select {
	case <-tc.Updates():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial update")
	}

	// Trigger multiple rapid onChange calls
	for i := 0; i < 10; i++ {
		tc.onChange()
		time.Sleep(10 * time.Millisecond) // 10ms apart, well within the 500ms debounce window
	}

	// Wait for the single debounced rebuild
	select {
	case <-tc.Updates():
		// Good — got exactly one notification
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for debounced update")
	}

	// There should not be another notification queued
	select {
	case <-tc.Updates():
		t.Error("received unexpected second notification — debounce should have coalesced")
	case <-time.After(700 * time.Millisecond):
		// Good — no extra notification
	}
}
