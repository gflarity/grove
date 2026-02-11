package k8s

import (
	"context"
	"testing"
	"time"

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// newGlobalFakeScheme creates a runtime.Scheme that includes all dynamic types
// used by InformerGlobalCache (PCS, PCSG, PC, ClusterTopology).
func newGlobalFakeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()

	for _, kind := range []string{"PodCliqueSet", "PodCliqueScalingGroup", "PodClique", "ClusterTopology"} {
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: kind},
			&unstructured.Unstructured{},
		)
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{Group: "grove.io", Version: "v1alpha1", Kind: kind + "List"},
			&unstructured.UnstructuredList{},
		)
	}
	return scheme
}

// toUnstructuredObj converts a typed object to Unstructured with the given GVK.
func toUnstructuredObj(obj interface{}, group, version, kind string) *unstructured.Unstructured {
	raw, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	u := &unstructured.Unstructured{Object: raw}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	return u
}

// ---------------------------------------------------------------------------
// Lifecycle tests
// ---------------------------------------------------------------------------

func TestInformerGlobalCache_StartAndSync(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer gc.Stop()

	if !gc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false — informers did not sync")
	}
}

func TestInformerGlobalCache_SnapshotNilBeforeStart(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)

	if snap := gc.Snapshot(); snap != nil {
		t.Errorf("Snapshot() before Start should be nil, got %+v", snap)
	}
}

func TestInformerGlobalCache_StopClosesChannel(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	gc.Stop()

	// The updates channel should be closed
	select {
	case _, ok := <-gc.Updates():
		if ok {
			_, ok = <-gc.Updates()
			if ok {
				t.Error("expected updates channel to be closed after Stop()")
			}
		}
	case <-time.After(1 * time.Second):
		t.Error("timed out reading from closed channel")
	}
}

func TestInformerGlobalCache_DoubleStopSafe(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)
	ctx := context.Background()
	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	// Calling Stop twice should not panic
	gc.Stop()
	gc.Stop()
}

// ---------------------------------------------------------------------------
// Empty cluster
// ---------------------------------------------------------------------------

func TestInformerGlobalCache_EmptyCluster(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer gc.Stop()

	if !gc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false")
	}

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil after sync")
	}

	if len(snap.PodCliqueSets) != 0 {
		t.Errorf("expected 0 PCS resources, got %d", len(snap.PodCliqueSets))
	}
	if len(snap.EventsByObject) != 0 {
		t.Errorf("expected 0 events, got %d", len(snap.EventsByObject))
	}
}

// ---------------------------------------------------------------------------
// Full snapshot rebuild
// ---------------------------------------------------------------------------

func TestInformerGlobalCache_RebuildSnapshot(t *testing.T) {
	// Create test data: nodes, ClusterTopology, PCS, PCSG, PC, Pods, Events.
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"nvidia.com/gpu.product":      "NVIDIA-H100-80GB-HBM3",
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				"nvidia.com/gpu": *mustParseQuantity("8"),
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
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	event1 := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs.event1",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "PodCliqueSet",
			Name: "my-pcs",
		},
		Type:         "Normal",
		Reason:       "Created",
		Message:      "PCS created successfully",
		LastTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Second)),
	}

	clientset := kubefake.NewSimpleClientset(node1, pod1, event1)

	ct := &corev1alpha1.ClusterTopology{
		ObjectMeta: metav1.ObjectMeta{Name: corev1alpha1.DefaultClusterTopologyName},
		Spec: corev1alpha1.ClusterTopologySpec{
			Levels: []corev1alpha1.TopologyLevel{
				{Domain: corev1alpha1.TopologyDomainZone, Key: "topology.kubernetes.io/zone"},
			},
		},
	}

	pcs := &corev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs",
			Namespace: "default",
		},
		Spec: corev1alpha1.PodCliqueSetSpec{
			Replicas: 1,
			Template: corev1alpha1.PodCliqueSetTemplateSpec{
				Cliques: []*corev1alpha1.PodCliqueTemplateSpec{
					{Name: "worker"},
				},
			},
		},
		Status: corev1alpha1.PodCliqueSetStatus{
			Replicas:          1,
			AvailableReplicas: 1,
		},
	}

	pcsg := &corev1alpha1.PodCliqueScalingGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcsg-0",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
			},
		},
		Status: corev1alpha1.PodCliqueScalingGroupStatus{
			Replicas:          1,
			AvailableReplicas: 1,
		},
	}

	pc := &corev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs-0-worker",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "my-pcs",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podcliquescalinggroup":      "my-pcsg-0",
			},
		},
		Spec: corev1alpha1.PodCliqueSpec{
			Replicas: 1,
		},
		Status: corev1alpha1.PodCliqueStatus{
			ReadyReplicas: 1,
		},
	}

	dynClient := dynamicfake.NewSimpleDynamicClient(
		newGlobalFakeScheme(),
		toUnstructuredObj(ct, "grove.io", "v1alpha1", "ClusterTopology"),
		toUnstructuredObj(pcs, "grove.io", "v1alpha1", "PodCliqueSet"),
		toUnstructuredObj(pcsg, "grove.io", "v1alpha1", "PodCliqueScalingGroup"),
		toUnstructuredObj(pc, "grove.io", "v1alpha1", "PodClique"),
	)

	gc := NewInformerGlobalCache(clientset, dynClient)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer gc.Stop()

	if !gc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false")
	}

	// Wait for debounced rebuild
	waitForUpdate(t, gc, 5*time.Second)

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil after sync + update")
	}

	// Verify PodCliqueSets
	if len(snap.PodCliqueSets) != 1 {
		t.Fatalf("expected 1 PCS, got %d", len(snap.PodCliqueSets))
	}
	if snap.PodCliqueSets[0].Name != "my-pcs" {
		t.Errorf("PCS name = %q, want %q", snap.PodCliqueSets[0].Name, "my-pcs")
	}
	if snap.PodCliqueSets[0].Ready != "1/1" {
		t.Errorf("PCS ready = %q, want %q", snap.PodCliqueSets[0].Ready, "1/1")
	}

	// Verify PCS specs
	if _, ok := snap.PodCliqueSetSpecs["my-pcs"]; !ok {
		t.Error("PodCliqueSetSpecs missing 'my-pcs'")
	}

	// Verify replica indexes
	indexes, ok := snap.ReplicaIndexesByPCS["my-pcs"]
	if !ok || len(indexes) == 0 {
		t.Errorf("ReplicaIndexesByPCS missing 'my-pcs', got %v", indexes)
	}

	// Verify ScalingGroupsByReplica
	sgs := snap.ScalingGroupsByReplica["my-pcs/0"]
	if len(sgs) != 1 {
		t.Fatalf("expected 1 PCSG under replica my-pcs/0, got %d", len(sgs))
	}
	if sgs[0].Name != "my-pcsg-0" {
		t.Errorf("PCSG name = %q, want %q", sgs[0].Name, "my-pcsg-0")
	}

	// Verify PodCliques under PCSG
	pcs_under_pcsg := snap.PodCliquesByPCSG["my-pcsg-0"]
	if len(pcs_under_pcsg) != 1 {
		t.Fatalf("expected 1 PodClique under PCSG my-pcsg-0, got %d", len(pcs_under_pcsg))
	}
	if pcs_under_pcsg[0].Name != "my-pcs-0-worker" {
		t.Errorf("PC name = %q, want %q", pcs_under_pcsg[0].Name, "my-pcs-0-worker")
	}

	// Verify Pods
	pods := snap.PodsByPodClique["my-pcs-0-worker"]
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod under PodClique my-pcs-0-worker, got %d", len(pods))
	}
	if pods[0].Name != "my-pcs-0-worker-abc" {
		t.Errorf("pod name = %q, want %q", pods[0].Name, "my-pcs-0-worker-abc")
	}
	if pods[0].Ready != "1/1" {
		t.Errorf("pod ready = %q, want %q", pods[0].Ready, "1/1")
	}

	// Verify PodInfos
	podInfo, ok := snap.PodInfos["my-pcs-0-worker-abc"]
	if !ok {
		t.Fatal("PodInfos missing 'my-pcs-0-worker-abc'")
	}
	if podInfo.NodeName != "node-1" {
		t.Errorf("pod node = %q, want %q", podInfo.NodeName, "node-1")
	}

	// Verify Events
	evts, ok := snap.EventsByObject["PodCliqueSet/my-pcs"]
	if !ok || len(evts) == 0 {
		t.Error("EventsByObject missing 'PodCliqueSet/my-pcs'")
	}

	// Verify node GPU products
	if snap.NodeGPUProducts["node-1"] != "H100" {
		t.Errorf("node GPU product = %q, want %q", snap.NodeGPUProducts["node-1"], "H100")
	}

	// Verify node GPU capacity
	if snap.NodeGPUCapacity["node-1"] != 8 {
		t.Errorf("node GPU capacity = %d, want 8", snap.NodeGPUCapacity["node-1"])
	}

	// Verify node labels (filtered to topology keys)
	if _, ok := snap.NodeLabels["node-1"]; !ok {
		t.Error("NodeLabels missing 'node-1'")
	}

	// Verify GPU summary is built
	if snap.GPUSummary == nil {
		t.Error("GPUSummary should not be nil")
	}

	// Verify TopologyViewData
	if snap.TopologyViewData == nil {
		t.Error("TopologyViewData should not be nil")
	}
}

// ---------------------------------------------------------------------------
// GetPodYAML
// ---------------------------------------------------------------------------

func TestInformerGlobalCache_GetPodYAML(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	clientset := kubefake.NewSimpleClientset(pod)
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer gc.Stop()

	t.Run("existing pod returns YAML", func(t *testing.T) {
		yaml, err := gc.GetPodYAML(ctx, "test-pod", "default")
		if err != nil {
			t.Fatalf("GetPodYAML() error: %v", err)
		}
		if yaml == "" {
			t.Error("GetPodYAML() returned empty string")
		}
	})

	t.Run("nonexistent pod returns error", func(t *testing.T) {
		_, err := gc.GetPodYAML(ctx, "nonexistent", "default")
		if err == nil {
			t.Error("GetPodYAML() expected error for nonexistent pod")
		}
	})
}

// ---------------------------------------------------------------------------
// Debounce
// ---------------------------------------------------------------------------

func TestInformerGlobalCache_DebounceCoalesces(t *testing.T) {
	clientset := kubefake.NewSimpleClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer gc.Stop()

	if !gc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false")
	}

	// Drain the initial update from WaitForSync's rebuildSnapshot call
	// (WaitForSync calls rebuildSnapshot directly, which sends to updatesCh)
	select {
	case <-gc.Updates():
	case <-time.After(2 * time.Second):
		// It's possible WaitForSync already consumed it - that's ok
	}

	// Trigger multiple rapid onChange calls
	for i := 0; i < 10; i++ {
		gc.onChange()
		time.Sleep(10 * time.Millisecond) // well within the 500ms debounce window
	}

	// Wait for the single debounced rebuild
	select {
	case <-gc.Updates():
		// Good — got exactly one notification
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for debounced update")
	}

	// There should not be another notification queued
	select {
	case <-gc.Updates():
		t.Error("received unexpected second notification — debounce should have coalesced")
	case <-time.After(700 * time.Millisecond):
		// Good — no extra notification
	}
}

// ---------------------------------------------------------------------------
// Pending pod Ready status
// ---------------------------------------------------------------------------

func TestInformerGlobalCache_PendingPodStatus(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
				"grove.io/podclique":        "my-pc",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	pcs := &corev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pcs", Namespace: "default"},
		Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 1},
	}

	clientset := kubefake.NewSimpleClientset(pod)
	dynClient := dynamicfake.NewSimpleDynamicClient(
		newGlobalFakeScheme(),
		toUnstructuredObj(pcs, "grove.io", "v1alpha1", "PodCliqueSet"),
	)

	gc := NewInformerGlobalCache(clientset, dynClient)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer gc.Stop()

	if !gc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false")
	}

	waitForUpdate(t, gc, 5*time.Second)

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	pods := snap.PodsByPodClique["my-pc"]
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods))
	}

	// Pending pods should have Ready = "0/1"
	if pods[0].Ready != "0/1" {
		t.Errorf("pending pod ready = %q, want %q", pods[0].Ready, "0/1")
	}
	if pods[0].Status != "Pending" {
		t.Errorf("pending pod status = %q, want %q", pods[0].Status, "Pending")
	}
}

// ---------------------------------------------------------------------------
// Event filtering by age
// ---------------------------------------------------------------------------

func TestInformerGlobalCache_OldEventsFiltered(t *testing.T) {
	// Event older than 1h should be filtered out
	oldEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "old-event",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "pod-old",
		},
		Type:         "Normal",
		Reason:       "Scheduled",
		Message:      "old event",
		LastTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
	}

	recentEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "recent-event",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "pod-recent",
		},
		Type:         "Warning",
		Reason:       "Failed",
		Message:      "recent event",
		LastTimestamp: metav1.NewTime(time.Now().Add(-5 * time.Minute)),
	}

	clientset := kubefake.NewSimpleClientset(oldEvent, recentEvent)
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer gc.Stop()

	if !gc.WaitForSync(ctx) {
		t.Fatal("WaitForSync() returned false")
	}

	waitForUpdate(t, gc, 5*time.Second)

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// Old event should be filtered out
	if _, ok := snap.EventsByObject["Pod/pod-old"]; ok {
		t.Error("old event (>1h) should have been filtered out")
	}

	// Recent event should be present
	evts, ok := snap.EventsByObject["Pod/pod-recent"]
	if !ok || len(evts) == 0 {
		t.Error("recent event should be present")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// waitForUpdate drains the updates channel, waiting up to the given timeout.
func waitForUpdate(t *testing.T, gc *InformerGlobalCache, timeout time.Duration) {
	t.Helper()
	select {
	case <-gc.Updates():
	case <-time.After(timeout):
		t.Fatal("timed out waiting for cache update notification")
	}
}

// mustParseQuantity parses a resource.Quantity or panics.
func mustParseQuantity(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}
