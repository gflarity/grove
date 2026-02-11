package tui

import (
	"testing"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/charmbracelet/bubbles/table"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ===========================================================================
// Phase 3, Item 1: currentTopologyDomain
// ===========================================================================

func TestCurrentTopologyDomain_LastEntryHasValue_FindsNextDomain(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{
				{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
				{Domain: "rack", Key: "topology.kubernetes.io/rack", ValuesCount: 6},
			},
		},
		topologyDrillStack: []data.TopologyDrillSelection{
			{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
		},
	}

	domain, key := m.currentTopologyDomain()
	if domain != "zone" {
		t.Errorf("expected domain 'zone', got %q", domain)
	}
	if key != "topology.kubernetes.io/zone" {
		t.Errorf("expected key 'topology.kubernetes.io/zone', got %q", key)
	}
}

func TestCurrentTopologyDomain_AtNarrowestDomain_ReturnsEmpty(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{
				{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
			},
		},
		topologyDrillStack: []data.TopologyDrillSelection{
			{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
			// zone has a value selected — try to find next domain, but there is none
			{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: "us-east-1a"},
		},
	}

	domain, key := m.currentTopologyDomain()
	if domain != "" {
		t.Errorf("expected empty domain at narrowest, got %q", domain)
	}
	if key != "" {
		t.Errorf("expected empty key at narrowest, got %q", key)
	}
}

func TestCurrentTopologyDomain_NilTopologyViewData(t *testing.T) {
	m := Model{
		topologyViewData:   nil,
		topologyDrillStack: []data.TopologyDrillSelection{{Domain: "region", Key: "k", Value: ""}},
	}

	domain, key := m.currentTopologyDomain()
	if domain != "" || key != "" {
		t.Errorf("expected ('', '') for nil topologyViewData, got (%q, %q)", domain, key)
	}
}

func TestCurrentTopologyDomain_EmptyDomains(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{},
		},
		topologyDrillStack: []data.TopologyDrillSelection{{Domain: "region", Key: "k", Value: ""}},
	}

	domain, key := m.currentTopologyDomain()
	if domain != "" || key != "" {
		t.Errorf("expected ('', '') for empty Domains slice, got (%q, %q)", domain, key)
	}
}

func TestCurrentTopologyDomain_EmptyDrillStack(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{
				{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
			},
		},
		topologyDrillStack: nil,
	}

	domain, key := m.currentTopologyDomain()
	if domain != "" || key != "" {
		t.Errorf("expected ('', '') for empty drill stack, got (%q, %q)", domain, key)
	}
}

func TestCurrentTopologyDomain_LastEntryNoValue_ReturnsThatDomain(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{
				{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
			},
		},
		topologyDrillStack: []data.TopologyDrillSelection{
			{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
			{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: ""},
		},
	}

	domain, key := m.currentTopologyDomain()
	if domain != "zone" {
		t.Errorf("expected domain 'zone', got %q", domain)
	}
	if key != "topology.kubernetes.io/zone" {
		t.Errorf("expected key 'topology.kubernetes.io/zone', got %q", key)
	}
}

// ===========================================================================
// Phase 3, Item 2: rebuildEventsFromSnapshot
// ===========================================================================

func TestRebuildEventsFromSnapshot_PodCliqueSetReplicaView_PCSGRow(t *testing.T) {
	now := time.Now()
	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		EventsByObject: map[string][]data.Event{
			"PodCliqueScalingGroup/my-pcsg": {
				{Type: "Normal", Reason: "Created", Message: "PCSG created", Parent: "my-pcsg", Timestamp: now},
			},
		},
		ReplicaIndexesByPCS:     map[string][]string{"alpha-pcs": {"0"}},
		ScalingGroupsByReplica:  map[string][]data.Resource{},
		PodCliquesByReplica:     map[string][]data.Resource{},
		ReplicaIndexesByPCSG:    map[string][]string{},
		PodCliquesByPCSG:        map[string][]data.Resource{},
		PodCliquesByPCSGReplica: map[string][]data.Resource{},
		PodsByPodClique:         map[string][]data.Resource{},
		NodeLabels:              map[string]map[string]string{},
		PodInfos:                map[string]data.CachedPodInfo{},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:             data.PodCliqueSetReplicaView,
			SelectedPodCliqueSet: "alpha-pcs",
			SelectedReplicaIndex: "0",
		},
		allResources: make(map[string][]data.Resource),
	}
	// Set up resources table with a PCSG row selected
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.resourcesTable.SetRows([]table.Row{
		{"default", "PodCliqueScalingGroup", "my-pcsg", "N/A", "1/1", "1/1"},
	})
	m.resourcesTable.SetCursor(0)

	m.rebuildEventsFromSnapshot(snapshot)

	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 event for PCSG, got %d", len(m.allEvents))
	}
	if m.allEvents[0].Message != "PCSG created" {
		t.Errorf("expected event message 'PCSG created', got %q", m.allEvents[0].Message)
	}
}

func TestRebuildEventsFromSnapshot_PodCliqueSetReplicaView_PodCliqueRow(t *testing.T) {
	now := time.Now()
	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		EventsByObject: map[string][]data.Event{
			"PodClique/my-pc": {
				{Type: "Normal", Reason: "Scaled", Message: "PC scaled", Parent: "my-pc", Timestamp: now},
			},
		},
		ReplicaIndexesByPCS:     map[string][]string{"alpha-pcs": {"0"}},
		ScalingGroupsByReplica:  map[string][]data.Resource{},
		PodCliquesByReplica:     map[string][]data.Resource{},
		ReplicaIndexesByPCSG:    map[string][]string{},
		PodCliquesByPCSG:        map[string][]data.Resource{},
		PodCliquesByPCSGReplica: map[string][]data.Resource{},
		PodsByPodClique:         map[string][]data.Resource{},
		NodeLabels:              map[string]map[string]string{},
		PodInfos:                map[string]data.CachedPodInfo{},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:             data.PodCliqueSetReplicaView,
			SelectedPodCliqueSet: "alpha-pcs",
			SelectedReplicaIndex: "0",
		},
		allResources: make(map[string][]data.Resource),
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.resourcesTable.SetRows([]table.Row{
		{"default", "PodClique", "my-pc", "N/A", "1/1", "1/1"},
	})
	m.resourcesTable.SetCursor(0)

	m.rebuildEventsFromSnapshot(snapshot)

	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 event for PodClique, got %d", len(m.allEvents))
	}
	if m.allEvents[0].Message != "PC scaled" {
		t.Errorf("expected event message 'PC scaled', got %q", m.allEvents[0].Message)
	}
}

func TestRebuildEventsFromSnapshot_PodCliqueSetReplicaView_Fallback(t *testing.T) {
	now := time.Now()
	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		EventsByObject: map[string][]data.Event{
			"PodCliqueScalingGroup/alpha-pcs-0-sg-prefill": {
				{Type: "Normal", Reason: "Ready", Message: "replica events", Parent: "alpha-pcs-0-sg-prefill", Timestamp: now},
			},
		},
		ReplicaIndexesByPCS: map[string][]string{"alpha-pcs": {"0"}},
		ScalingGroupsByReplica: map[string][]data.Resource{
			"alpha-pcs/0": {{Name: "alpha-pcs-0-sg-prefill", Type: "PodCliqueScalingGroup"}},
		},
		PodCliquesByReplica:     map[string][]data.Resource{},
		ReplicaIndexesByPCSG:    map[string][]string{},
		PodCliquesByPCSG:        map[string][]data.Resource{},
		PodCliquesByPCSGReplica: map[string][]data.Resource{},
		PodsByPodClique:         map[string][]data.Resource{},
		NodeLabels:              map[string]map[string]string{},
		PodInfos:                map[string]data.CachedPodInfo{},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:             data.PodCliqueSetReplicaView,
			SelectedPodCliqueSet: "alpha-pcs",
			SelectedReplicaIndex: "0",
		},
		allResources: make(map[string][]data.Resource),
	}
	// No rows selected (empty table) → fallback to replica events
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.resourcesTable.SetRows([]table.Row{})

	m.rebuildEventsFromSnapshot(snapshot)

	// Fallback: GetEventsForReplica("alpha-pcs", "0")
	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 event (fallback to replica events), got %d", len(m.allEvents))
	}
}

func TestRebuildEventsFromSnapshot_PodCliqueScalingGroupView_PCSGReplicaRow(t *testing.T) {
	now := time.Now()
	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		EventsByObject: map[string][]data.Event{
			"PodClique/my-pcsg-0-worker": {
				{Type: "Normal", Reason: "Started", Message: "replica event", Parent: "my-pcsg-0-worker", Timestamp: now},
			},
		},
		ReplicaIndexesByPCS:    map[string][]string{},
		ScalingGroupsByReplica: map[string][]data.Resource{},
		PodCliquesByReplica:    map[string][]data.Resource{},
		ReplicaIndexesByPCSG:   map[string][]string{"my-pcsg": {"0"}},
		PodCliquesByPCSG:       map[string][]data.Resource{},
		PodCliquesByPCSGReplica: map[string][]data.Resource{
			"my-pcsg/0": {{Name: "my-pcsg-0-worker", Type: "PodClique"}},
		},
		PodsByPodClique: map[string][]data.Resource{},
		NodeLabels:      map[string]map[string]string{},
		PodInfos:        map[string]data.CachedPodInfo{},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:             data.PodCliqueScalingGroupView,
			SelectedScalingGroup: "my-pcsg",
		},
		allResources: make(map[string][]data.Resource),
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.resourcesTable.SetRows([]table.Row{
		{"default", "PodCliqueScalingGroupReplica", "my-pcsg-replica-0", "N/A", "1/1", "1/1"},
	})
	m.resourcesTable.SetCursor(0)

	m.rebuildEventsFromSnapshot(snapshot)

	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 event for PCSG replica, got %d", len(m.allEvents))
	}
}

func TestRebuildEventsFromSnapshot_PodCliqueScalingGroupReplicaView_PodCliqueRow(t *testing.T) {
	now := time.Now()
	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		EventsByObject: map[string][]data.Event{
			"PodClique/my-pcsg-0-worker": {
				{Type: "Normal", Reason: "Synced", Message: "PC event in PCSG replica", Parent: "my-pcsg-0-worker", Timestamp: now},
			},
		},
		ReplicaIndexesByPCS:     map[string][]string{},
		ScalingGroupsByReplica:  map[string][]data.Resource{},
		PodCliquesByReplica:     map[string][]data.Resource{},
		ReplicaIndexesByPCSG:    map[string][]string{},
		PodCliquesByPCSG:        map[string][]data.Resource{},
		PodCliquesByPCSGReplica: map[string][]data.Resource{},
		PodsByPodClique:         map[string][]data.Resource{},
		NodeLabels:              map[string]map[string]string{},
		PodInfos:                map[string]data.CachedPodInfo{},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:                 data.PodCliqueScalingGroupReplicaView,
			SelectedScalingGroup:     "my-pcsg",
			SelectedPCSGReplicaIndex: "0",
		},
		allResources: make(map[string][]data.Resource),
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.resourcesTable.SetRows([]table.Row{
		{"default", "PodClique", "my-pcsg-0-worker", "N/A", "1/1", "1/1"},
	})
	m.resourcesTable.SetCursor(0)

	m.rebuildEventsFromSnapshot(snapshot)

	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 event for PodClique, got %d", len(m.allEvents))
	}
	if m.allEvents[0].Message != "PC event in PCSG replica" {
		t.Errorf("expected 'PC event in PCSG replica', got %q", m.allEvents[0].Message)
	}
}

func TestRebuildEventsFromSnapshot_PodCliqueScalingGroupReplicaView_Fallback(t *testing.T) {
	now := time.Now()
	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		EventsByObject: map[string][]data.Event{
			"PodClique/my-pcsg-0-worker": {
				{Type: "Normal", Reason: "Synced", Message: "fallback event", Parent: "my-pcsg-0-worker", Timestamp: now},
			},
		},
		ReplicaIndexesByPCS:    map[string][]string{},
		ScalingGroupsByReplica: map[string][]data.Resource{},
		PodCliquesByReplica:    map[string][]data.Resource{},
		ReplicaIndexesByPCSG:   map[string][]string{},
		PodCliquesByPCSG:       map[string][]data.Resource{},
		PodCliquesByPCSGReplica: map[string][]data.Resource{
			"my-pcsg/0": {{Name: "my-pcsg-0-worker", Type: "PodClique"}},
		},
		PodsByPodClique: map[string][]data.Resource{},
		NodeLabels:      map[string]map[string]string{},
		PodInfos:        map[string]data.CachedPodInfo{},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:                 data.PodCliqueScalingGroupReplicaView,
			SelectedScalingGroup:     "my-pcsg",
			SelectedPCSGReplicaIndex: "0",
		},
		allResources: make(map[string][]data.Resource),
	}
	// Empty table → fallback
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.resourcesTable.SetRows([]table.Row{})

	m.rebuildEventsFromSnapshot(snapshot)

	// Falls through to GetEventsForPCSGReplica("my-pcsg", "0")
	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 fallback event, got %d", len(m.allEvents))
	}
}

func TestRebuildEventsFromSnapshot_PodCliqueView(t *testing.T) {
	now := time.Now()
	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		EventsByObject: map[string][]data.Event{
			"PodClique/my-pc": {
				{Type: "Normal", Reason: "Ready", Message: "pc event", Parent: "my-pc", Timestamp: now},
			},
		},
		ReplicaIndexesByPCS:     map[string][]string{},
		ScalingGroupsByReplica:  map[string][]data.Resource{},
		PodCliquesByReplica:     map[string][]data.Resource{},
		ReplicaIndexesByPCSG:    map[string][]string{},
		PodCliquesByPCSG:        map[string][]data.Resource{},
		PodCliquesByPCSGReplica: map[string][]data.Resource{},
		PodsByPodClique:         map[string][]data.Resource{},
		NodeLabels:              map[string]map[string]string{},
		PodInfos:                map[string]data.CachedPodInfo{},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:          data.PodCliqueView,
			SelectedPodClique: "my-pc",
		},
		allResources: make(map[string][]data.Resource),
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)

	m.rebuildEventsFromSnapshot(snapshot)

	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 event for PodClique, got %d", len(m.allEvents))
	}
}

func TestRebuildEventsFromSnapshot_PodView(t *testing.T) {
	now := time.Now()
	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		EventsByObject: map[string][]data.Event{
			"PodClique/my-pc": {
				{Type: "Normal", Reason: "Ready", Message: "pod view event", Parent: "my-pc", Timestamp: now},
			},
			"Pod/my-pod": {
				{Type: "Normal", Reason: "Scheduled", Message: "pod scheduled", Parent: "my-pod", Timestamp: now},
			},
		},
		ReplicaIndexesByPCS:     map[string][]string{},
		ScalingGroupsByReplica:  map[string][]data.Resource{},
		PodCliquesByReplica:     map[string][]data.Resource{},
		ReplicaIndexesByPCSG:    map[string][]string{},
		PodCliquesByPCSG:        map[string][]data.Resource{},
		PodCliquesByPCSGReplica: map[string][]data.Resource{},
		PodsByPodClique: map[string][]data.Resource{
			"my-pc": {{Name: "my-pod", Type: "Pod"}},
		},
		NodeLabels: map[string]map[string]string{},
		PodInfos:   map[string]data.CachedPodInfo{},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:          data.PodView,
			SelectedPodClique: "my-pc",
			SelectedPod:       "my-pod",
		},
		allResources: make(map[string][]data.Resource),
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)

	m.rebuildEventsFromSnapshot(snapshot)

	// PodView uses GetEventsForPodClique, which includes the PodClique events + Pod events
	if len(m.allEvents) != 2 {
		t.Fatalf("expected 2 events for PodView (pc + pod), got %d", len(m.allEvents))
	}
}

func TestRebuildEventsFromSnapshot_NilSnapshot(t *testing.T) {
	m := Model{
		viewState: data.ViewState{ViewType: data.ForestView},
		allEvents: []data.Event{{Type: "old"}},
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)

	m.rebuildEventsFromSnapshot(nil)

	if m.allEvents != nil {
		t.Errorf("expected nil allEvents after nil snapshot, got %v", m.allEvents)
	}
}

// ===========================================================================
// Phase 3, Item 3: gpuCountsForResource
// ===========================================================================

func TestGpuCountsForResource_PodCliqueScalingGroupReplica_Valid(t *testing.T) {
	summary := &data.GPUSummary{
		GPUTypes: []string{"H100"},
		ByPCSG: map[string]data.GPUCounts{
			"my-pcsg-replica-0": {"H100": 4},
		},
	}

	m := Model{
		gpuSummary: summary,
		viewState: data.ViewState{
			SelectedScalingGroup: "my-pcsg",
		},
	}

	counts := m.gpuCountsForResource(data.Resource{
		Name: "my-pcsg-replica-0",
		Type: "PodCliqueScalingGroupReplica",
	})

	if counts == nil {
		t.Fatal("expected non-nil counts for PodCliqueScalingGroupReplica")
	}
	if counts["H100"] != 4 {
		t.Errorf("expected H100=4, got %d", counts["H100"])
	}
}

func TestGpuCountsForResource_PodCliqueScalingGroupReplica_EmptyPCSGName(t *testing.T) {
	summary := &data.GPUSummary{
		GPUTypes: []string{"H100"},
		ByPCSG: map[string]data.GPUCounts{
			"my-pcsg-replica-0": {"H100": 4},
		},
	}

	m := Model{
		gpuSummary: summary,
		viewState: data.ViewState{
			SelectedScalingGroup: "", // empty pcsg name
		},
	}

	counts := m.gpuCountsForResource(data.Resource{
		Name: "my-pcsg-replica-0",
		Type: "PodCliqueScalingGroupReplica",
	})

	// Even with empty pcsgName, the code falls back to ByPCSG[r.Name]
	if counts == nil {
		t.Fatal("expected non-nil counts (fallback to ByPCSG[r.Name])")
	}
	if counts["H100"] != 4 {
		t.Errorf("expected fallback H100=4, got %d", counts["H100"])
	}
}

func TestGpuCountsForResource_PodCliqueScalingGroupReplica_BadReplicaIndex(t *testing.T) {
	summary := &data.GPUSummary{
		GPUTypes: []string{"H100"},
		ByPCSG: map[string]data.GPUCounts{
			"my-pcsg-no-replica-suffix": {"H100": 2},
		},
	}

	m := Model{
		gpuSummary: summary,
		viewState: data.ViewState{
			SelectedScalingGroup: "my-pcsg",
		},
	}

	// Name without "-replica-" suffix — extractReplicaIndex returns ""
	counts := m.gpuCountsForResource(data.Resource{
		Name: "my-pcsg-no-replica-suffix",
		Type: "PodCliqueScalingGroupReplica",
	})

	// Falls back to ByPCSG[r.Name]
	if counts == nil {
		t.Fatal("expected non-nil counts (fallback)")
	}
	if counts["H100"] != 2 {
		t.Errorf("expected fallback H100=2, got %d", counts["H100"])
	}
}

func TestGpuCountsForResource_PodCliqueSetReplica_EmptyPCSName(t *testing.T) {
	summary := &data.GPUSummary{
		GPUTypes:  []string{"H100"},
		ByReplica: map[string]data.GPUCounts{"pcs-a/0": {"H100": 8}},
	}

	m := Model{
		gpuSummary: summary,
		viewState: data.ViewState{
			SelectedPodCliqueSet: "", // empty pcsName
		},
	}

	counts := m.gpuCountsForResource(data.Resource{
		Name: "pcs-a-replica-0",
		Type: "PodCliqueSetReplica",
	})

	// pcsName is "" so the lookup key is "/0" → no match → nil
	if counts != nil {
		t.Errorf("expected nil counts for empty pcsName, got %v", counts)
	}
}

func TestGpuCountsForResource_PodCliqueSetReplica_BadReplicaIndex(t *testing.T) {
	summary := &data.GPUSummary{
		GPUTypes:  []string{"H100"},
		ByReplica: map[string]data.GPUCounts{"pcs-a/0": {"H100": 8}},
	}

	m := Model{
		gpuSummary: summary,
		viewState: data.ViewState{
			SelectedPodCliqueSet: "pcs-a",
		},
	}

	// Name without "-replica-" => replicaIndex = "" => short-circuit, return nil
	counts := m.gpuCountsForResource(data.Resource{
		Name: "pcs-a-no-replica",
		Type: "PodCliqueSetReplica",
	})

	if counts != nil {
		t.Errorf("expected nil counts for bad replica index, got %v", counts)
	}
}

// ===========================================================================
// Phase 3, Item 4: getFilteredEvents
// ===========================================================================

func TestGetFilteredEvents_PodCliqueView_PodRowSelected(t *testing.T) {
	allEvents := []data.Event{
		{Type: "Normal", Parent: "pod-a", Reason: "Scheduled", Message: "pod-a scheduled"},
		{Type: "Normal", Parent: "pod-b", Reason: "Scheduled", Message: "pod-b scheduled"},
		{Type: "Warning", Parent: "pc-a", Reason: "Failed", Message: "pc-a failed"},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:          data.PodCliqueView,
			SelectedPodClique: "pc-a",
		},
		allEvents: allEvents,
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.resourcesTable.SetRows([]table.Row{
		{"default", "Pod", "pod-a", "N/A", "1/1", "Running"},
	})
	m.resourcesTable.SetCursor(0)

	events := m.getFilteredEvents()

	if len(events) != 1 {
		t.Fatalf("expected 1 event for selected pod-a, got %d", len(events))
	}
	if events[0].Parent != "pod-a" {
		t.Errorf("expected event parent 'pod-a', got %q", events[0].Parent)
	}
}

func TestGetFilteredEvents_PodCliqueView_NonPodRowSelected(t *testing.T) {
	allEvents := []data.Event{
		{Type: "Normal", Parent: "pod-a", Reason: "Scheduled", Message: "event 1"},
		{Type: "Warning", Parent: "pod-b", Reason: "Failed", Message: "event 2"},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:          data.PodCliqueView,
			SelectedPodClique: "pc-a",
		},
		allEvents: allEvents,
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	// Row type is PodClique, not Pod
	m.resourcesTable.SetRows([]table.Row{
		{"default", "PodClique", "pc-child", "N/A", "1/1", "1/1"},
	})
	m.resourcesTable.SetCursor(0)

	events := m.getFilteredEvents()

	// Non-Pod row → returns all events
	if len(events) != 2 {
		t.Fatalf("expected all 2 events for non-Pod row, got %d", len(events))
	}
}

func TestGetFilteredEvents_PodCliqueView_NoRowSelected(t *testing.T) {
	allEvents := []data.Event{
		{Type: "Normal", Parent: "pod-a", Reason: "Scheduled", Message: "event 1"},
	}

	m := Model{
		viewState: data.ViewState{
			ViewType:          data.PodCliqueView,
			SelectedPodClique: "pc-a",
		},
		allEvents: allEvents,
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	// Empty table, no row selected
	m.resourcesTable.SetRows([]table.Row{})

	events := m.getFilteredEvents()

	// No valid selection → returns all events
	if len(events) != 1 {
		t.Fatalf("expected 1 event (all events), got %d", len(events))
	}
}

// ===========================================================================
// Phase 3, Item 5: validateTopologyDrillStack
// ===========================================================================

func TestValidateTopologyDrillStack_DomainRemoved_StackResets(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{
				{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
				// "rack" domain was removed
			},
		},
		topologyDrillStack: []data.TopologyDrillSelection{
			{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
			{Domain: "rack", Key: "topology.kubernetes.io/rack", Value: ""},
		},
	}

	m.validateTopologyDrillStack()

	if m.topologyDrillStack != nil {
		t.Errorf("expected drill stack to be nil after domain removal, got %v", m.topologyDrillStack)
	}
}

func TestValidateTopologyDrillStack_AllDomainsValid_StackPreserved(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{
				{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
			},
		},
		topologyDrillStack: []data.TopologyDrillSelection{
			{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
			{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: ""},
		},
	}

	m.validateTopologyDrillStack()

	if len(m.topologyDrillStack) != 2 {
		t.Errorf("expected drill stack preserved with 2 entries, got %d", len(m.topologyDrillStack))
	}
}

func TestValidateTopologyDrillStack_PartiallyStale(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{
				{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
				// "zone" was removed, "rack" still exists
				{Domain: "rack", Key: "topology.kubernetes.io/rack", ValuesCount: 6},
			},
		},
		topologyDrillStack: []data.TopologyDrillSelection{
			{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
			{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: "us-east-1a"},
			{Domain: "rack", Key: "topology.kubernetes.io/rack", Value: ""},
		},
	}

	m.validateTopologyDrillStack()

	// "zone" no longer exists → stack should be reset
	if m.topologyDrillStack != nil {
		t.Errorf("expected drill stack to be nil after partial stale, got %v", m.topologyDrillStack)
	}
}

func TestValidateTopologyDrillStack_NilTopologyViewData(t *testing.T) {
	m := Model{
		topologyViewData: nil,
		topologyDrillStack: []data.TopologyDrillSelection{
			{Domain: "region", Key: "k", Value: ""},
		},
	}

	m.validateTopologyDrillStack()

	// Stack should remain unchanged (nil topologyViewData triggers early return)
	if len(m.topologyDrillStack) != 1 {
		t.Errorf("expected drill stack unchanged for nil topologyViewData, got %d", len(m.topologyDrillStack))
	}
}

func TestValidateTopologyDrillStack_EmptyStack(t *testing.T) {
	m := Model{
		topologyViewData: &data.TopologyViewData{
			Domains: []data.TopologyDomainRow{
				{Domain: "region", Key: "k", ValuesCount: 1},
			},
		},
		topologyDrillStack: nil,
	}

	// Should not panic
	m.validateTopologyDrillStack()

	if m.topologyDrillStack != nil {
		t.Errorf("expected nil drill stack to remain nil, got %v", m.topologyDrillStack)
	}
}

// ===========================================================================
// Phase 3, Item 6: applySnapshot
// ===========================================================================

func TestApplySnapshot_TopologyInfoRebuiltForSelectedPCS(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = samplePCSResources()
	snap.PodCliqueSetSpecs = map[string]*corev1alpha1.PodCliqueSet{
		"alpha-pcs": {
			ObjectMeta: metav1.ObjectMeta{Name: "alpha-pcs", Namespace: "default"},
			Spec: corev1alpha1.PodCliqueSetSpec{
				Replicas: 3,
			},
		},
	}
	snap.TopologyViewData = &data.TopologyViewData{
		DomainToKey: map[string]string{
			"rack": "topology.io/rack",
			"zone": "topology.kubernetes.io/zone",
		},
	}
	snap.ReplicaIndexesByPCS = map[string][]string{"alpha-pcs": {"0"}}
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"

	m.applySnapshot()

	if m.cachedTopologyInfo == nil {
		t.Fatal("expected cachedTopologyInfo to be set after applySnapshot with SelectedPodCliqueSet")
	}
}

func TestApplySnapshot_DomainToKeyPopulated(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = samplePCSResources()
	snap.PodCliqueSetSpecs = map[string]*corev1alpha1.PodCliqueSet{
		"alpha-pcs": {
			ObjectMeta: metav1.ObjectMeta{Name: "alpha-pcs", Namespace: "default"},
			Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 1},
		},
	}
	snap.TopologyViewData = &data.TopologyViewData{
		DomainToKey: map[string]string{
			"rack": "topology.io/rack",
			"zone": "topology.kubernetes.io/zone",
		},
	}
	snap.ReplicaIndexesByPCS = map[string][]string{"alpha-pcs": {"0"}}
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"

	m.applySnapshot()

	if m.cachedTopologyInfo == nil {
		t.Fatal("expected cachedTopologyInfo to be set")
	}
	if m.cachedTopologyInfo.DomainToKey == nil {
		t.Fatal("expected DomainToKey to be populated on TopologyInfo")
	}
	if m.cachedTopologyInfo.DomainToKey["rack"] != "topology.io/rack" {
		t.Errorf("expected DomainToKey['rack'] = 'topology.io/rack', got %q", m.cachedTopologyInfo.DomainToKey["rack"])
	}
	if m.cachedTopologyInfo.DomainToKey["zone"] != "topology.kubernetes.io/zone" {
		t.Errorf("expected DomainToKey['zone'] = 'topology.kubernetes.io/zone', got %q", m.cachedTopologyInfo.DomainToKey["zone"])
	}
}

func TestApplySnapshot_TopologyViewTriggersTableRebuild(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = samplePCSResources()
	snap.TopologyViewData = sampleTopologyViewData()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})

	// Toggle to Topology view
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Verify domains table is populated
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 domain rows, got %d", len(rows))
	}

	// Now update the snapshot with different topology data
	newSnap := mc.Snapshot()
	newSnap.TopologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "block", Key: "topology.io/block", ValuesCount: 2},
		},
		NodeLabels:  map[string]map[string]string{},
		Pods:        []data.TopologyViewPod{},
		DomainToKey: map[string]string{"block": "topology.io/block"},
	}
	mc.SetSnapshot(newSnap)

	// Deliver cache update
	m = mustApply(m, CacheUpdateMsg{})

	// Verify domains table was rebuilt with new data
	rows = m.topologyDomainsTable.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 domain row after topology update, got %d", len(rows))
	}
	if rows[0][0] != "block" {
		t.Errorf("expected domain 'block', got %q", rows[0][0])
	}
}

func TestApplySnapshot_NilSnapshot(t *testing.T) {
	mc := data.NewMockGlobalCache()
	// Set snapshot to nil to test early return
	mc.SetSnapshot(nil)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true

	// Should not panic
	m.applySnapshot()

	if m.cachedSnapshot != nil {
		t.Errorf("expected nil cachedSnapshot, got %v", m.cachedSnapshot)
	}
}

func TestApplySnapshot_NoSelectedPCS_SkipsTopologyInfo(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = samplePCSResources()
	snap.PodCliqueSetSpecs = map[string]*corev1alpha1.PodCliqueSet{
		"alpha-pcs": {
			ObjectMeta: metav1.ObjectMeta{Name: "alpha-pcs", Namespace: "default"},
			Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 1},
		},
	}
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m.viewState.SelectedPodCliqueSet = "" // no PCS selected

	m.applySnapshot()

	if m.cachedTopologyInfo != nil {
		t.Errorf("expected nil cachedTopologyInfo when no PCS selected, got %v", m.cachedTopologyInfo)
	}
}
