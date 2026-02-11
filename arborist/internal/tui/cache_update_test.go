package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---------------------------------------------------------------------------
// CacheUpdateMsg — refreshes resources after data change
// ---------------------------------------------------------------------------

func TestCacheUpdateMsg_RefreshesResourcesList(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []data.Resource{
		{Name: "pcs-a", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
	}
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)
	assertView(t, m, []string{"pcs-a"})

	// Now update the cache with a new PCS
	newSnap := mc.Snapshot()
	newSnap.PodCliqueSets = append(newSnap.PodCliqueSets, data.Resource{
		Name: "pcs-b", Type: "PodCliqueSet", Namespace: "default", Ready: "2/2", Scheduled: "2/2",
	})
	mc.SetSnapshot(newSnap)

	// Deliver CacheUpdateMsg
	m = mustApply(m, CacheUpdateMsg{})

	assertView(t, m, []string{"pcs-a", "pcs-b"})
}

func TestCacheUpdateMsg_RemovesDeletedResources(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []data.Resource{
		{Name: "pcs-a", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
		{Name: "pcs-b", Type: "PodCliqueSet", Namespace: "default", Ready: "2/2", Scheduled: "2/2"},
	}
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)
	assertView(t, m, []string{"pcs-a", "pcs-b"})

	// Remove pcs-b
	newSnap := mc.Snapshot()
	newSnap.PodCliqueSets = []data.Resource{
		{Name: "pcs-a", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
	}
	mc.SetSnapshot(newSnap)

	m = mustApply(m, CacheUpdateMsg{})

	assertView(t, m, []string{"pcs-a"})
	assertNotInView(t, m, []string{"pcs-b"})
}

// ---------------------------------------------------------------------------
// CacheUpdateMsg — cursor preservation
// ---------------------------------------------------------------------------

func TestCacheUpdateMsg_PreservesCursorPosition(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []data.Resource{
		{Name: "alpha-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
		{Name: "beta-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "2/2", Scheduled: "2/2"},
		{Name: "gamma-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "3/3", Scheduled: "3/3"},
	}
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	// Move cursor down to beta-pcs
	m = sendKey(m, tea.KeyDown)
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 || selectedRow[2] != "beta-pcs" {
		t.Fatalf("cursor should be on beta-pcs, got %v", selectedRow)
	}

	// Update cache (data changes but beta-pcs still exists)
	newSnap := mc.Snapshot()
	newSnap.PodCliqueSets = []data.Resource{
		{Name: "alpha-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
		{Name: "beta-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "1/2", Scheduled: "1/2"}, // changed
		{Name: "gamma-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "3/3", Scheduled: "3/3"},
	}
	mc.SetSnapshot(newSnap)

	m = mustApply(m, CacheUpdateMsg{})

	// Cursor should still be on beta-pcs
	selectedRow = m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 || selectedRow[2] != "beta-pcs" {
		t.Errorf("after update cursor should be on beta-pcs, got %v", selectedRow)
	}
}

// ---------------------------------------------------------------------------
// CacheUpdateMsg — GPU summary update
// ---------------------------------------------------------------------------

func TestCacheUpdateMsg_UpdatesGPUSummary(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []data.Resource{
		{Name: "pcs-a", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
	}
	snap.GPUSummary = &data.GPUSummary{
		GPUTypes: []string{"H100"},
		ByPCS:    map[string]data.GPUCounts{"pcs-a": {"H100": 8}},
	}
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	if m.gpuSummary == nil {
		t.Fatal("gpuSummary should not be nil after initial sync")
	}
	if m.gpuSummary.ByPCS["pcs-a"]["H100"] != 8 {
		t.Errorf("initial GPU count = %d, want 8", m.gpuSummary.ByPCS["pcs-a"]["H100"])
	}

	// Update GPU count
	newSnap := mc.Snapshot()
	newSnap.GPUSummary = &data.GPUSummary{
		GPUTypes: []string{"H100"},
		ByPCS:    map[string]data.GPUCounts{"pcs-a": {"H100": 16}},
	}
	mc.SetSnapshot(newSnap)

	m = mustApply(m, CacheUpdateMsg{})

	if m.gpuSummary == nil {
		t.Fatal("gpuSummary should not be nil after update")
	}
	if m.gpuSummary.ByPCS["pcs-a"]["H100"] != 16 {
		t.Errorf("updated GPU count = %d, want 16", m.gpuSummary.ByPCS["pcs-a"]["H100"])
	}
}

// ---------------------------------------------------------------------------
// CacheUpdateMsg — topology update
// ---------------------------------------------------------------------------

func TestCacheUpdateMsg_UpdatesTopologyViewData(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []data.Resource{
		{Name: "pcs-a", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
	}
	snap.TopologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 2},
		},
	}
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	if m.topologyViewData == nil {
		t.Fatal("topologyViewData should not be nil after initial sync")
	}
	if len(m.topologyViewData.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(m.topologyViewData.Domains))
	}

	// Update with additional domain
	newSnap := mc.Snapshot()
	newSnap.TopologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 2},
			{Domain: "rack", Key: "topology.io/rack", ValuesCount: 4},
		},
	}
	mc.SetSnapshot(newSnap)

	m = mustApply(m, CacheUpdateMsg{})

	if m.topologyViewData == nil {
		t.Fatal("topologyViewData should not be nil after update")
	}
	if len(m.topologyViewData.Domains) != 2 {
		t.Errorf("expected 2 domains after update, got %d", len(m.topologyViewData.Domains))
	}
}

// ---------------------------------------------------------------------------
// CacheUpdateMsg — events update
// ---------------------------------------------------------------------------

func TestCacheUpdateMsg_UpdatesEvents(t *testing.T) {
	now := time.Now()

	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []data.Resource{
		{Name: "pcs-a", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
	}
	snap.EventsByObject = map[string][]data.Event{
		"PodCliqueSet/pcs-a": {
			{Type: "Normal", Kind: "PodCliqueSet", Reason: "Created", Message: "initial event", Parent: "pcs-a", Timestamp: now},
		},
	}
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	view := m.View()
	if !strings.Contains(view, "initial event") {
		t.Error("view should contain initial event")
	}

	// Update events
	newSnap := mc.Snapshot()
	newSnap.EventsByObject = map[string][]data.Event{
		"PodCliqueSet/pcs-a": {
			{Type: "Warning", Kind: "PodCliqueSet", Reason: "Degraded", Message: "new warning event", Parent: "pcs-a", Timestamp: now},
		},
	}
	mc.SetSnapshot(newSnap)

	m = mustApply(m, CacheUpdateMsg{})

	view = m.View()
	if !strings.Contains(view, "new warning event") {
		t.Error("view should contain new warning event after update")
	}
}

// ---------------------------------------------------------------------------
// CacheUpdateMsg — nil cache is safe
// ---------------------------------------------------------------------------

func TestCacheUpdateMsg_NilCacheIsSafe(t *testing.T) {
	m := NewModel(nil)
	m = mustApply(m, CacheUpdateMsg{})
	// Should not panic — nil cache is handled gracefully
}

// ---------------------------------------------------------------------------
// buildMockCacheWithEvents factory test
// ---------------------------------------------------------------------------

func TestBuildMockCacheWithEvents_ShowsEvents(t *testing.T) {
	now := time.Now()
	mc := buildMockCacheWithEvents(map[string][]data.Event{
		"PodCliqueSet/alpha-pcs": {
			{Type: "Warning", Kind: "PodCliqueSet", Reason: "Degraded", Message: "alpha degraded", Parent: "alpha-pcs", Timestamp: now},
		},
	})

	m := newTestModelWithCache(mc)
	assertView(t, m, []string{"alpha degraded"})
}

// ---------------------------------------------------------------------------
// applySnapshot — rebuilds hierarchy from snapshot
// ---------------------------------------------------------------------------

func TestApplySnapshot_PopulatesHierarchy(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []data.Resource{
		{Name: "my-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
	}
	snap.PodCliqueSetSpecs = map[string]*corev1alpha1.PodCliqueSet{
		"my-pcs": {
			ObjectMeta: metav1.ObjectMeta{Name: "my-pcs", Namespace: "default"},
			Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 2},
		},
	}
	snap.ReplicaIndexesByPCS = map[string][]string{
		"my-pcs": {"0", "1"},
	}
	snap.ScalingGroupsByReplica = map[string][]data.Resource{
		"my-pcs/0": {
			{Name: "my-pcsg-0", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
		},
		"my-pcs/1": {
			{Name: "my-pcsg-1", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
		},
	}
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	// Navigate into PCS (with 2 replicas, won't skip)
	m = sendKey(m, tea.KeyEnter)

	// Should show replicas
	assertView(t, m, []string{"my-pcs-replica-0", "my-pcs-replica-1"})
}
