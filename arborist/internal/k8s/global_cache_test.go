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

// newFakeClientset creates a kubefake.Clientset with the Grove CRD resources
// registered in the fake discovery so that checkGroveCRDsAvailable() finds them.
func newFakeClientset(objects ...runtime.Object) *kubefake.Clientset {
	cs := kubefake.NewSimpleClientset(objects...)
	cs.Resources = append(cs.Resources, &metav1.APIResourceList{
		GroupVersion: "grove.io/v1alpha1",
		APIResources: []metav1.APIResource{
			{Name: "podcliquesets", Kind: "PodCliqueSet", Namespaced: true},
			{Name: "podcliquescalinggroups", Kind: "PodCliqueScalingGroup", Namespaced: true},
			{Name: "podcliques", Kind: "PodClique", Namespaced: true},
			{Name: "clustertopologies", Kind: "ClusterTopology"},
		},
	})
	return cs
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
	clientset := newFakeClientset()
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
	clientset := newFakeClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient)

	if snap := gc.Snapshot(); snap != nil {
		t.Errorf("Snapshot() before Start should be nil, got %+v", snap)
	}
}

func TestInformerGlobalCache_StopClosesChannel(t *testing.T) {
	clientset := newFakeClientset()
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
	clientset := newFakeClientset()
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
	clientset := newFakeClientset()
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

	clientset := newFakeClientset(node1, pod1, event1)

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
		Spec: corev1alpha1.PodCliqueScalingGroupSpec{
			Replicas:    1,
			CliqueNames: []string{"worker"},
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
				"app.kubernetes.io/part-of":                       "my-pcs",
				"grove.io/podcliqueset-replica-index":             "0",
				"grove.io/podcliquescalinggroup":                  "my-pcsg-0",
				"grove.io/podcliquescalinggroup-replica-index":    "0",
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
	// Scheduled is computed from pod NodeName: 1 pod on node-1 -> PCS "1/1"
	if snap.PodCliqueSets[0].Scheduled != "1/1" {
		t.Errorf("PCS scheduled = %q, want %q", snap.PodCliqueSets[0].Scheduled, "1/1")
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
	// PCSG Scheduled: 1 replica, its PodClique has 1/1 pods scheduled -> "1/1"
	if sgs[0].Scheduled != "1/1" {
		t.Errorf("PCSG scheduled = %q, want %q", sgs[0].Scheduled, "1/1")
	}

	// Verify PodCliques under PCSG
	pcs_under_pcsg := snap.PodCliquesByPCSG["my-pcsg-0"]
	if len(pcs_under_pcsg) != 1 {
		t.Fatalf("expected 1 PodClique under PCSG my-pcsg-0, got %d", len(pcs_under_pcsg))
	}
	if pcs_under_pcsg[0].Name != "my-pcs-0-worker" {
		t.Errorf("PC name = %q, want %q", pcs_under_pcsg[0].Name, "my-pcs-0-worker")
	}
	// PodClique Scheduled: 1 pod with NodeName -> "1/1"
	if pcs_under_pcsg[0].Scheduled != "1/1" {
		t.Errorf("PC scheduled = %q, want %q", pcs_under_pcsg[0].Scheduled, "1/1")
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

	clientset := newFakeClientset(pod)
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
	clientset := newFakeClientset()
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

	clientset := newFakeClientset(pod)
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

	clientset := newFakeClientset(oldEvent, recentEvent)
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
// Scheduled computation unit tests
// ---------------------------------------------------------------------------

func TestIsPCSGReplicaScheduled(t *testing.T) {
	tests := []struct {
		name     string
		pcs      []*corev1alpha1.PodClique
		sched    map[string]int32
		expected bool
	}{
		{
			name:     "no PodCliques -> not scheduled",
			pcs:      nil,
			sched:    map[string]int32{},
			expected: false,
		},
		{
			name: "all PodCliques meet minAvailable (defaults to replicas)",
			pcs: []*corev1alpha1.PodClique{
				{ObjectMeta: metav1.ObjectMeta{Name: "pc-a"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pc-b"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}},
			},
			sched:    map[string]int32{"pc-a": 2, "pc-b": 1},
			expected: true,
		},
		{
			name: "one PodClique below minAvailable -> not scheduled",
			pcs: []*corev1alpha1.PodClique{
				{ObjectMeta: metav1.ObjectMeta{Name: "pc-a"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pc-b"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 3}},
			},
			sched:    map[string]int32{"pc-a": 2, "pc-b": 2},
			expected: false,
		},
		{
			name: "MinAvailable explicitly set, meets threshold",
			pcs: []*corev1alpha1.PodClique{
				{ObjectMeta: metav1.ObjectMeta{Name: "pc-a"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 4, MinAvailable: int32Ptr(2)}},
			},
			sched:    map[string]int32{"pc-a": 2},
			expected: true,
		},
		{
			name: "MinAvailable nil defaults to Replicas, not enough scheduled",
			pcs: []*corev1alpha1.PodClique{
				{ObjectMeta: metav1.ObjectMeta{Name: "pc-a"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 3}},
			},
			sched:    map[string]int32{"pc-a": 2},
			expected: false,
		},
		{
			name: "zero pods scheduled",
			pcs: []*corev1alpha1.PodClique{
				{ObjectMeta: metav1.ObjectMeta{Name: "pc-a"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}},
			},
			sched:    map[string]int32{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPCSGReplicaScheduled(tt.pcs, tt.sched)
			if got != tt.expected {
				t.Errorf("isPCSGReplicaScheduled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestComputePCSGScheduledReplicas(t *testing.T) {
	tests := []struct {
		name     string
		pcsg     *corev1alpha1.PodCliqueScalingGroup
		sched    map[string]int32
		pcObjs   map[string][]*corev1alpha1.PodClique
		expected int32
	}{
		{
			name: "2 PCSG replicas, both scheduled",
			pcsg: &corev1alpha1.PodCliqueScalingGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1"},
				Spec:       corev1alpha1.PodCliqueScalingGroupSpec{Replicas: 2, CliqueNames: []string{"worker"}},
			},
			sched: map[string]int32{"pcsg-1-0-worker": 2, "pcsg-1-1-worker": 2},
			pcObjs: map[string][]*corev1alpha1.PodClique{
				"pcsg-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1-0-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}}},
				"pcsg-1/1": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1-1-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}}},
			},
			expected: 2,
		},
		{
			name: "2 PCSG replicas, one not scheduled (partial)",
			pcsg: &corev1alpha1.PodCliqueScalingGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1"},
				Spec:       corev1alpha1.PodCliqueScalingGroupSpec{Replicas: 2, CliqueNames: []string{"worker"}},
			},
			sched: map[string]int32{"pcsg-1-0-worker": 2, "pcsg-1-1-worker": 0},
			pcObjs: map[string][]*corev1alpha1.PodClique{
				"pcsg-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1-0-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}}},
				"pcsg-1/1": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1-1-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}}},
			},
			expected: 1,
		},
		{
			name: "MinAvailable edge case: Replicas=4, MinAvailable=2, only 2 scheduled -> still counts",
			pcsg: &corev1alpha1.PodCliqueScalingGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1"},
				Spec:       corev1alpha1.PodCliqueScalingGroupSpec{Replicas: 1, CliqueNames: []string{"worker"}},
			},
			sched: map[string]int32{"pcsg-1-0-worker": 2},
			pcObjs: map[string][]*corev1alpha1.PodClique{
				"pcsg-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1-0-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 4, MinAvailable: int32Ptr(2)}}},
			},
			expected: 1,
		},
		{
			name: "zero pods scheduled everywhere",
			pcsg: &corev1alpha1.PodCliqueScalingGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1"},
				Spec:       corev1alpha1.PodCliqueScalingGroupSpec{Replicas: 2, CliqueNames: []string{"worker"}},
			},
			sched: map[string]int32{},
			pcObjs: map[string][]*corev1alpha1.PodClique{
				"pcsg-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1-0-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}}},
				"pcsg-1/1": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-1-1-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}}},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computePCSGScheduledReplicas(tt.pcsg, tt.sched, tt.pcObjs)
			if got != tt.expected {
				t.Errorf("computePCSGScheduledReplicas() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestComputePCSScheduledReplicas(t *testing.T) {
	tests := []struct {
		name           string
		pcs            *corev1alpha1.PodCliqueSet
		sched          map[string]int32
		pcObjsByPCSG   map[string][]*corev1alpha1.PodClique
		standalonePCs  map[string][]*corev1alpha1.PodClique
		pcsgsByReplica map[string][]*corev1alpha1.PodCliqueScalingGroup
		expected       int32
	}{
		{
			name: "1 PCS replica, all standalone PCs scheduled -> 1/1",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "pcs-1"},
				Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 1},
			},
			sched: map[string]int32{"pcs-1-0-worker": 2},
			standalonePCs: map[string][]*corev1alpha1.PodClique{
				"pcs-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcs-1-0-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}}},
			},
			pcsgsByReplica: map[string][]*corev1alpha1.PodCliqueScalingGroup{},
			pcObjsByPCSG:   map[string][]*corev1alpha1.PodClique{},
			expected:       1,
		},
		{
			name: "2 PCS replicas, one has unscheduled standalone PC -> 1/2",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "pcs-1"},
				Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 2},
			},
			sched: map[string]int32{"pcs-1-0-worker": 2, "pcs-1-1-worker": 0},
			standalonePCs: map[string][]*corev1alpha1.PodClique{
				"pcs-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcs-1-0-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}}},
				"pcs-1/1": {{ObjectMeta: metav1.ObjectMeta{Name: "pcs-1-1-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 2}}},
			},
			pcsgsByReplica: map[string][]*corev1alpha1.PodCliqueScalingGroup{},
			pcObjsByPCSG:   map[string][]*corev1alpha1.PodClique{},
			expected:       1,
		},
		{
			name: "PCS replica with scheduled standalone PC but unscheduled PCSG -> not counted",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "pcs-1"},
				Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 1},
			},
			sched: map[string]int32{"pcs-1-0-worker": 1, "pcsg-0-0-trainer": 0},
			standalonePCs: map[string][]*corev1alpha1.PodClique{
				"pcs-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcs-1-0-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}}},
			},
			pcsgsByReplica: map[string][]*corev1alpha1.PodCliqueScalingGroup{
				"pcs-1/0": {{
					ObjectMeta: metav1.ObjectMeta{Name: "pcsg-0"},
					Spec:       corev1alpha1.PodCliqueScalingGroupSpec{Replicas: 1, CliqueNames: []string{"trainer"}},
				}},
			},
			pcObjsByPCSG: map[string][]*corev1alpha1.PodClique{
				"pcsg-0/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-0-0-trainer"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}}},
			},
			expected: 0,
		},
		{
			name: "all scheduled: standalone PC + PCSG both meet requirements",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "pcs-1"},
				Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 1},
			},
			sched: map[string]int32{"pcs-1-0-worker": 1, "pcsg-0-0-trainer": 1},
			standalonePCs: map[string][]*corev1alpha1.PodClique{
				"pcs-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcs-1-0-worker"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}}},
			},
			pcsgsByReplica: map[string][]*corev1alpha1.PodCliqueScalingGroup{
				"pcs-1/0": {{
					ObjectMeta: metav1.ObjectMeta{Name: "pcsg-0"},
					Spec:       corev1alpha1.PodCliqueScalingGroupSpec{Replicas: 1, CliqueNames: []string{"trainer"}},
				}},
			},
			pcObjsByPCSG: map[string][]*corev1alpha1.PodClique{
				"pcsg-0/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcsg-0-0-trainer"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}}},
			},
			expected: 1,
		},
		{
			name: "zero pods scheduled at all levels",
			pcs: &corev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{Name: "pcs-1"},
				Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 2},
			},
			sched: map[string]int32{},
			standalonePCs: map[string][]*corev1alpha1.PodClique{
				"pcs-1/0": {{ObjectMeta: metav1.ObjectMeta{Name: "pcs-1-0-w"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}}},
				"pcs-1/1": {{ObjectMeta: metav1.ObjectMeta{Name: "pcs-1-1-w"}, Spec: corev1alpha1.PodCliqueSpec{Replicas: 1}}},
			},
			pcsgsByReplica: map[string][]*corev1alpha1.PodCliqueScalingGroup{},
			pcObjsByPCSG:   map[string][]*corev1alpha1.PodClique{},
			expected:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computePCSScheduledReplicas(tt.pcs, tt.sched, tt.pcObjsByPCSG, tt.standalonePCs, tt.pcsgsByReplica)
			if got != tt.expected {
				t.Errorf("computePCSScheduledReplicas() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Option plumbing tests (Steps 4.3–4.4)
// ---------------------------------------------------------------------------

func TestWithCacheNamespace_SetsField(t *testing.T) {
	clientset := newFakeClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	gc := NewInformerGlobalCache(clientset, dynClient, WithCacheNamespace("test-ns"))
	if gc.namespace != "test-ns" {
		t.Errorf("namespace = %q, want %q", gc.namespace, "test-ns")
	}
}

func TestWithCacheNamespace_EmptyIsClusterWide(t *testing.T) {
	clientset := newFakeClientset()
	dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())

	// No options
	gc1 := NewInformerGlobalCache(clientset, dynClient)
	if gc1.namespace != "" {
		t.Errorf("no option: namespace = %q, want %q", gc1.namespace, "")
	}

	// Explicit empty string
	gc2 := NewInformerGlobalCache(clientset, dynClient, WithCacheNamespace(""))
	if gc2.namespace != "" {
		t.Errorf("empty option: namespace = %q, want %q", gc2.namespace, "")
	}
}

// ---------------------------------------------------------------------------
// Namespace-scoped snapshot tests (Steps 4.5–4.11)
// ---------------------------------------------------------------------------

// newNamespaceScopedTestResources creates a full set of resources in two
// namespaces ("test-ns" and "other-ns") for namespace-scoping tests.
type namespacedTestData struct {
	clientset *kubefake.Clientset
	dynClient *dynamicfake.FakeDynamicClient
}

func newNamespacedTestData() namespacedTestData {
	// Nodes (cluster-scoped)
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

	// --- test-ns resources ---
	podTestNS := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pcs-a-0-worker-abc",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "pcs-a",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podclique":                  "pcs-a-0-worker",
			},
		},
		Spec:   corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	eventTestNS := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pcs-a.event1",
			Namespace: "test-ns",
		},
		InvolvedObject: corev1.ObjectReference{Kind: "PodCliqueSet", Name: "pcs-a"},
		Type:           "Normal",
		Reason:         "Created",
		Message:        "test-ns event",
		LastTimestamp:   metav1.NewTime(time.Now().Add(-10 * time.Second)),
	}

	// --- other-ns resources ---
	podOtherNS := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pcs-b-0-worker-xyz",
			Namespace: "other-ns",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "pcs-b",
				"grove.io/podcliqueset-replica-index": "0",
				"grove.io/podclique":                  "pcs-b-0-worker",
			},
		},
		Spec:   corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	eventOtherNS := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pcs-b.event1",
			Namespace: "other-ns",
		},
		InvolvedObject: corev1.ObjectReference{Kind: "PodCliqueSet", Name: "pcs-b"},
		Type:           "Warning",
		Reason:         "Failed",
		Message:        "other-ns event",
		LastTimestamp:   metav1.NewTime(time.Now().Add(-5 * time.Second)),
	}

	clientset := newFakeClientset(node1, podTestNS, podOtherNS, eventTestNS, eventOtherNS)

	// ClusterTopology (cluster-scoped)
	ct := &corev1alpha1.ClusterTopology{
		ObjectMeta: metav1.ObjectMeta{Name: corev1alpha1.DefaultClusterTopologyName},
		Spec: corev1alpha1.ClusterTopologySpec{
			Levels: []corev1alpha1.TopologyLevel{
				{Domain: corev1alpha1.TopologyDomainZone, Key: "topology.kubernetes.io/zone"},
			},
		},
	}

	// PCS in test-ns
	pcsA := &corev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{Name: "pcs-a", Namespace: "test-ns"},
		Spec: corev1alpha1.PodCliqueSetSpec{
			Replicas: 1,
			Template: corev1alpha1.PodCliqueSetTemplateSpec{
				Cliques: []*corev1alpha1.PodCliqueTemplateSpec{{Name: "worker"}},
			},
		},
		Status: corev1alpha1.PodCliqueSetStatus{Replicas: 1, AvailableReplicas: 1},
	}

	// PCSG in test-ns
	pcsgA := &corev1alpha1.PodCliqueScalingGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pcsg-a-0", Namespace: "test-ns",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "pcs-a",
				"grove.io/podcliqueset-replica-index": "0",
			},
		},
		Spec: corev1alpha1.PodCliqueScalingGroupSpec{Replicas: 1, CliqueNames: []string{"worker"}},
		Status: corev1alpha1.PodCliqueScalingGroupStatus{Replicas: 1, AvailableReplicas: 1},
	}

	// PC in test-ns
	pcA := &corev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pcs-a-0-worker", Namespace: "test-ns",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":                    "pcs-a",
				"grove.io/podcliqueset-replica-index":          "0",
				"grove.io/podcliquescalinggroup":               "pcsg-a-0",
				"grove.io/podcliquescalinggroup-replica-index": "0",
			},
		},
		Spec:   corev1alpha1.PodCliqueSpec{Replicas: 1},
		Status: corev1alpha1.PodCliqueStatus{ReadyReplicas: 1},
	}

	// PCS in other-ns
	pcsB := &corev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{Name: "pcs-b", Namespace: "other-ns"},
		Spec: corev1alpha1.PodCliqueSetSpec{
			Replicas: 1,
			Template: corev1alpha1.PodCliqueSetTemplateSpec{
				Cliques: []*corev1alpha1.PodCliqueTemplateSpec{{Name: "worker"}},
			},
		},
		Status: corev1alpha1.PodCliqueSetStatus{Replicas: 1, AvailableReplicas: 1},
	}

	// PCSG in other-ns
	pcsgB := &corev1alpha1.PodCliqueScalingGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pcsg-b-0", Namespace: "other-ns",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":           "pcs-b",
				"grove.io/podcliqueset-replica-index": "0",
			},
		},
		Spec: corev1alpha1.PodCliqueScalingGroupSpec{Replicas: 1, CliqueNames: []string{"worker"}},
		Status: corev1alpha1.PodCliqueScalingGroupStatus{Replicas: 1, AvailableReplicas: 1},
	}

	// PC in other-ns
	pcB := &corev1alpha1.PodClique{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pcs-b-0-worker", Namespace: "other-ns",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":                    "pcs-b",
				"grove.io/podcliqueset-replica-index":          "0",
				"grove.io/podcliquescalinggroup":               "pcsg-b-0",
				"grove.io/podcliquescalinggroup-replica-index": "0",
			},
		},
		Spec:   corev1alpha1.PodCliqueSpec{Replicas: 1},
		Status: corev1alpha1.PodCliqueStatus{ReadyReplicas: 1},
	}

	dynClient := dynamicfake.NewSimpleDynamicClient(
		newGlobalFakeScheme(),
		toUnstructuredObj(ct, "grove.io", "v1alpha1", "ClusterTopology"),
		toUnstructuredObj(pcsA, "grove.io", "v1alpha1", "PodCliqueSet"),
		toUnstructuredObj(pcsgA, "grove.io", "v1alpha1", "PodCliqueScalingGroup"),
		toUnstructuredObj(pcA, "grove.io", "v1alpha1", "PodClique"),
		toUnstructuredObj(pcsB, "grove.io", "v1alpha1", "PodCliqueSet"),
		toUnstructuredObj(pcsgB, "grove.io", "v1alpha1", "PodCliqueScalingGroup"),
		toUnstructuredObj(pcB, "grove.io", "v1alpha1", "PodClique"),
	)

	return namespacedTestData{clientset: clientset, dynClient: dynClient}
}

// startAndSync is a test helper that starts a cache and waits for sync + first update.
func startAndSync(t *testing.T, gc *InformerGlobalCache) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := gc.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start() returned error: %v", err)
	}
	if !gc.WaitForSync(ctx) {
		cancel()
		t.Fatal("WaitForSync() returned false")
	}
	waitForUpdate(t, gc, 5*time.Second)
	return ctx, cancel
}

func TestInformerGlobalCache_NamespaceScoped_PCSFiltered(t *testing.T) {
	td := newNamespacedTestData()
	gc := NewInformerGlobalCache(td.clientset, td.dynClient, WithCacheNamespace("test-ns"))
	ctx, cancel := startAndSync(t, gc)
	defer cancel()
	defer gc.Stop()
	_ = ctx

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// Only PCS from test-ns should be present
	if len(snap.PodCliqueSets) != 1 {
		t.Fatalf("expected 1 PCS, got %d", len(snap.PodCliqueSets))
	}
	if snap.PodCliqueSets[0].Name != "pcs-a" {
		t.Errorf("PCS name = %q, want %q", snap.PodCliqueSets[0].Name, "pcs-a")
	}
	if snap.PodCliqueSets[0].Namespace != "test-ns" {
		t.Errorf("PCS namespace = %q, want %q", snap.PodCliqueSets[0].Namespace, "test-ns")
	}
}

func TestInformerGlobalCache_NamespaceScoped_PodsFiltered(t *testing.T) {
	td := newNamespacedTestData()
	gc := NewInformerGlobalCache(td.clientset, td.dynClient, WithCacheNamespace("test-ns"))
	_, cancel := startAndSync(t, gc)
	defer cancel()
	defer gc.Stop()

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// Only pods from test-ns should be present
	pods := snap.PodsByPodClique["pcs-a-0-worker"]
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod under pcs-a-0-worker, got %d", len(pods))
	}
	if pods[0].Name != "pcs-a-0-worker-abc" {
		t.Errorf("pod name = %q, want %q", pods[0].Name, "pcs-a-0-worker-abc")
	}

	// other-ns pods should NOT be present
	otherPods := snap.PodsByPodClique["pcs-b-0-worker"]
	if len(otherPods) != 0 {
		t.Errorf("expected 0 pods from other-ns, got %d", len(otherPods))
	}

	// PodInfos should only contain test-ns pods
	if _, ok := snap.PodInfos["pcs-b-0-worker-xyz"]; ok {
		t.Error("PodInfos should not contain pod from other-ns")
	}
	if _, ok := snap.PodInfos["pcs-a-0-worker-abc"]; !ok {
		t.Error("PodInfos should contain pod from test-ns")
	}
}

func TestInformerGlobalCache_NamespaceScoped_EventsFiltered(t *testing.T) {
	td := newNamespacedTestData()
	gc := NewInformerGlobalCache(td.clientset, td.dynClient, WithCacheNamespace("test-ns"))
	_, cancel := startAndSync(t, gc)
	defer cancel()
	defer gc.Stop()

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// test-ns event should be present
	evts, ok := snap.EventsByObject["PodCliqueSet/pcs-a"]
	if !ok || len(evts) == 0 {
		t.Error("expected event for pcs-a from test-ns")
	}

	// other-ns event should NOT be present
	if _, ok := snap.EventsByObject["PodCliqueSet/pcs-b"]; ok {
		t.Error("event from other-ns should not be present")
	}
}

func TestInformerGlobalCache_NamespaceScoped_PCSGAndPCFiltered(t *testing.T) {
	td := newNamespacedTestData()
	gc := NewInformerGlobalCache(td.clientset, td.dynClient, WithCacheNamespace("test-ns"))
	_, cancel := startAndSync(t, gc)
	defer cancel()
	defer gc.Stop()

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// PCSG from test-ns should be present
	sgs := snap.ScalingGroupsByReplica["pcs-a/0"]
	if len(sgs) != 1 {
		t.Fatalf("expected 1 PCSG under pcs-a/0, got %d", len(sgs))
	}
	if sgs[0].Name != "pcsg-a-0" {
		t.Errorf("PCSG name = %q, want %q", sgs[0].Name, "pcsg-a-0")
	}

	// PCSG from other-ns should NOT be present
	otherSgs := snap.ScalingGroupsByReplica["pcs-b/0"]
	if len(otherSgs) != 0 {
		t.Errorf("expected 0 PCSGs from other-ns, got %d", len(otherSgs))
	}

	// PodCliques from test-ns under PCSG
	pcs := snap.PodCliquesByPCSG["pcsg-a-0"]
	if len(pcs) != 1 {
		t.Fatalf("expected 1 PC under pcsg-a-0, got %d", len(pcs))
	}
	if pcs[0].Name != "pcs-a-0-worker" {
		t.Errorf("PC name = %q, want %q", pcs[0].Name, "pcs-a-0-worker")
	}

	// PodCliques from other-ns should NOT be present
	otherPcs := snap.PodCliquesByPCSG["pcsg-b-0"]
	if len(otherPcs) != 0 {
		t.Errorf("expected 0 PCs from other-ns, got %d", len(otherPcs))
	}
}

func TestInformerGlobalCache_NamespaceScoped_NodesStillVisible(t *testing.T) {
	td := newNamespacedTestData()
	gc := NewInformerGlobalCache(td.clientset, td.dynClient, WithCacheNamespace("test-ns"))
	_, cancel := startAndSync(t, gc)
	defer cancel()
	defer gc.Stop()

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// Nodes are cluster-scoped — should still be visible
	if _, ok := snap.NodeLabels["node-1"]; !ok {
		t.Error("NodeLabels should still contain node-1 under namespace scoping")
	}
	if snap.NodeGPUProducts["node-1"] != "H100" {
		t.Errorf("NodeGPUProducts[node-1] = %q, want %q", snap.NodeGPUProducts["node-1"], "H100")
	}
	if snap.NodeGPUCapacity["node-1"] != 8 {
		t.Errorf("NodeGPUCapacity[node-1] = %d, want 8", snap.NodeGPUCapacity["node-1"])
	}
}

func TestInformerGlobalCache_NamespaceScoped_ClusterTopologyStillVisible(t *testing.T) {
	td := newNamespacedTestData()
	gc := NewInformerGlobalCache(td.clientset, td.dynClient, WithCacheNamespace("test-ns"))
	_, cancel := startAndSync(t, gc)
	defer cancel()
	defer gc.Stop()

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// ClusterTopology is cluster-scoped — should still populate TopologyViewData
	if snap.TopologyViewData == nil {
		t.Error("TopologyViewData should not be nil under namespace scoping")
	}
}

func TestInformerGlobalCache_NamespaceScoped_FullHierarchy(t *testing.T) {
	td := newNamespacedTestData()
	gc := NewInformerGlobalCache(td.clientset, td.dynClient, WithCacheNamespace("test-ns"))
	_, cancel := startAndSync(t, gc)
	defer cancel()
	defer gc.Stop()

	snap := gc.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// --- Verify test-ns hierarchy is correct ---

	// PCS
	if len(snap.PodCliqueSets) != 1 {
		t.Fatalf("expected 1 PCS, got %d", len(snap.PodCliqueSets))
	}
	if snap.PodCliqueSets[0].Name != "pcs-a" {
		t.Errorf("PCS name = %q, want %q", snap.PodCliqueSets[0].Name, "pcs-a")
	}

	// Replica indexes
	indexes := snap.ReplicaIndexesByPCS["pcs-a"]
	if len(indexes) == 0 {
		t.Error("ReplicaIndexesByPCS should have entries for pcs-a")
	}

	// PCSG under PCS replica
	sgs := snap.ScalingGroupsByReplica["pcs-a/0"]
	if len(sgs) != 1 || sgs[0].Name != "pcsg-a-0" {
		t.Errorf("PCSG under pcs-a/0: got %v", sgs)
	}

	// PC under PCSG
	pcs := snap.PodCliquesByPCSG["pcsg-a-0"]
	if len(pcs) != 1 || pcs[0].Name != "pcs-a-0-worker" {
		t.Errorf("PC under pcsg-a-0: got %v", pcs)
	}

	// Pods under PC
	pods := snap.PodsByPodClique["pcs-a-0-worker"]
	if len(pods) != 1 || pods[0].Name != "pcs-a-0-worker-abc" {
		t.Errorf("Pod under pcs-a-0-worker: got %v", pods)
	}

	// Events for test-ns PCS
	if _, ok := snap.EventsByObject["PodCliqueSet/pcs-a"]; !ok {
		t.Error("missing events for pcs-a")
	}

	// Nodes (cluster-scoped, always visible)
	if _, ok := snap.NodeLabels["node-1"]; !ok {
		t.Error("NodeLabels missing node-1")
	}

	// TopologyViewData (cluster-scoped, always visible)
	if snap.TopologyViewData == nil {
		t.Error("TopologyViewData should not be nil")
	}

	// GPU summary built from scoped pods only
	if snap.GPUSummary == nil {
		t.Error("GPUSummary should not be nil")
	}

	// --- Verify zero resources from other-ns leaked ---

	// No other-ns PCS
	for _, pcsRes := range snap.PodCliqueSets {
		if pcsRes.Namespace == "other-ns" {
			t.Errorf("PCS from other-ns leaked: %s", pcsRes.Name)
		}
	}

	// No other-ns PCSG
	for key, sgs := range snap.ScalingGroupsByReplica {
		for _, sg := range sgs {
			if sg.Namespace == "other-ns" {
				t.Errorf("PCSG from other-ns leaked at key %s: %s", key, sg.Name)
			}
		}
	}

	// No other-ns pods
	for key, pods := range snap.PodsByPodClique {
		for _, pod := range pods {
			if pod.Namespace == "other-ns" {
				t.Errorf("Pod from other-ns leaked at key %s: %s", key, pod.Name)
			}
		}
	}

	// No other-ns events
	if _, ok := snap.EventsByObject["PodCliqueSet/pcs-b"]; ok {
		t.Error("events from other-ns leaked for pcs-b")
	}

	// No other-ns PodInfos
	if _, ok := snap.PodInfos["pcs-b-0-worker-xyz"]; ok {
		t.Error("PodInfos from other-ns leaked")
	}
}

// ---------------------------------------------------------------------------
// CLI wiring test (Step 4.12)
// ---------------------------------------------------------------------------

func TestBuildCacheOptions_NamespacePassedToGlobalCache(t *testing.T) {
	// Verify the option-building logic: when namespace is non-empty,
	// WithCacheNamespace should produce a GlobalCacheOption that sets the field.
	tests := []struct {
		name      string
		namespace string
		wantNS    string
	}{
		{"empty namespace means cluster-wide", "", ""},
		{"non-empty namespace is scoped", "gpu-stack", "gpu-stack"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []GlobalCacheOption
			if tt.namespace != "" {
				opts = append(opts, WithCacheNamespace(tt.namespace))
			}

			clientset := newFakeClientset()
			dynClient := dynamicfake.NewSimpleDynamicClient(newGlobalFakeScheme())
			gc := NewInformerGlobalCache(clientset, dynClient, opts...)

			if gc.namespace != tt.wantNS {
				t.Errorf("namespace = %q, want %q", gc.namespace, tt.wantNS)
			}
		})
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

// int32Ptr returns a pointer to the given int32 value.
func int32Ptr(i int32) *int32 {
	return &i
}
