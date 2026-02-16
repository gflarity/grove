package tui

import (
	"testing"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
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
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{
					{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
					{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
					{Domain: "rack", Key: "topology.kubernetes.io/rack", ValuesCount: 6},
				},
			},
			topologyDrill: clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
			}),
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
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{
					{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
					{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
				},
			},
			topologyDrill: clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
				// zone has a value selected — try to find next domain, but there is none
				{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: "us-east-1a"},
			}),
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
		TopologyState: TopologyState{
			topologyViewData: nil,
			topologyDrill:    clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{{Domain: "region", Key: "k", Value: ""}}),
		},
	}

	domain, key := m.currentTopologyDomain()
	if domain != "" || key != "" {
		t.Errorf("expected ('', '') for nil topologyViewData, got (%q, %q)", domain, key)
	}
}

func TestCurrentTopologyDomain_EmptyDomains(t *testing.T) {
	m := Model{
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{},
			},
			topologyDrill: clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{{Domain: "region", Key: "k", Value: ""}}),
		},
	}

	domain, key := m.currentTopologyDomain()
	if domain != "" || key != "" {
		t.Errorf("expected ('', '') for empty Domains slice, got (%q, %q)", domain, key)
	}
}

func TestCurrentTopologyDomain_EmptyDrillStack(t *testing.T) {
	m := Model{
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{
					{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
				},
			},
		},
	}

	domain, key := m.currentTopologyDomain()
	if domain != "" || key != "" {
		t.Errorf("expected ('', '') for empty drill stack, got (%q, %q)", domain, key)
	}
}

func TestCurrentTopologyDomain_LastEntryNoValue_ReturnsThatDomain(t *testing.T) {
	m := Model{
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{
					{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
					{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
				},
			},
			topologyDrill: clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: ""},
			}),
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
	snapshot := &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:           samplePCSResources(),
			ReplicaIndexesByPCS:     map[string][]string{"alpha-pcs": {"0"}},
			ScalingGroupsByReplica:  map[string][]clusterstate.Resource{},
			PodCliquesByReplica:     map[string][]clusterstate.Resource{},
			ReplicaIndexesByPCSG:    map[string][]string{},
			PodCliquesByPCSG:        map[string][]clusterstate.Resource{},
			PodCliquesByPCSGReplica: map[string][]clusterstate.Resource{},
			PodsByPodClique:         map[string][]clusterstate.Resource{},
		},
		EventsByObject: map[string][]clusterstate.Event{
			"PodCliqueScalingGroup/my-pcsg": {
				{Type: "Normal", Reason: "Created", Message: "PCSG created", Parent: "my-pcsg", Timestamp: now},
			},
		},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:             clusterstate.PodCliqueSetReplicaView,
			SelectedPodCliqueSet: "alpha-pcs",
			SelectedReplicaIndex: "0",
		},
		DataState: DataState{allResources: make(map[string][]clusterstate.Resource)},
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
	snapshot := &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:           samplePCSResources(),
			ReplicaIndexesByPCS:     map[string][]string{"alpha-pcs": {"0"}},
			ScalingGroupsByReplica:  map[string][]clusterstate.Resource{},
			PodCliquesByReplica:     map[string][]clusterstate.Resource{},
			ReplicaIndexesByPCSG:    map[string][]string{},
			PodCliquesByPCSG:        map[string][]clusterstate.Resource{},
			PodCliquesByPCSGReplica: map[string][]clusterstate.Resource{},
			PodsByPodClique:         map[string][]clusterstate.Resource{},
		},
		EventsByObject: map[string][]clusterstate.Event{
			"PodClique/my-pc": {
				{Type: "Normal", Reason: "Scaled", Message: "PC scaled", Parent: "my-pc", Timestamp: now},
			},
		},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:             clusterstate.PodCliqueSetReplicaView,
			SelectedPodCliqueSet: "alpha-pcs",
			SelectedReplicaIndex: "0",
		},
		DataState: DataState{allResources: make(map[string][]clusterstate.Resource)},
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
	snapshot := &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:       samplePCSResources(),
			ReplicaIndexesByPCS: map[string][]string{"alpha-pcs": {"0"}},
			ScalingGroupsByReplica: map[string][]clusterstate.Resource{
				"alpha-pcs/0": {{Name: "alpha-pcs-0-sg-prefill", Type: "PodCliqueScalingGroup"}},
			},
			PodCliquesByReplica:     map[string][]clusterstate.Resource{},
			ReplicaIndexesByPCSG:    map[string][]string{},
			PodCliquesByPCSG:        map[string][]clusterstate.Resource{},
			PodCliquesByPCSGReplica: map[string][]clusterstate.Resource{},
			PodsByPodClique:         map[string][]clusterstate.Resource{},
		},
		EventsByObject: map[string][]clusterstate.Event{
			"PodCliqueScalingGroup/alpha-pcs-0-sg-prefill": {
				{Type: "Normal", Reason: "Ready", Message: "replica events", Parent: "alpha-pcs-0-sg-prefill", Timestamp: now},
			},
		},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:             clusterstate.PodCliqueSetReplicaView,
			SelectedPodCliqueSet: "alpha-pcs",
			SelectedReplicaIndex: "0",
		},
		DataState: DataState{allResources: make(map[string][]clusterstate.Resource)},
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
	snapshot := &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:          samplePCSResources(),
			ReplicaIndexesByPCS:    map[string][]string{},
			ScalingGroupsByReplica: map[string][]clusterstate.Resource{},
			PodCliquesByReplica:    map[string][]clusterstate.Resource{},
			ReplicaIndexesByPCSG:   map[string][]string{"my-pcsg": {"0"}},
			PodCliquesByPCSG:       map[string][]clusterstate.Resource{},
			PodCliquesByPCSGReplica: map[string][]clusterstate.Resource{
				"my-pcsg/0": {{Name: "my-pcsg-0-worker", Type: "PodClique"}},
			},
			PodsByPodClique: map[string][]clusterstate.Resource{},
		},
		EventsByObject: map[string][]clusterstate.Event{
			"PodClique/my-pcsg-0-worker": {
				{Type: "Normal", Reason: "Started", Message: "replica event", Parent: "my-pcsg-0-worker", Timestamp: now},
			},
		},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:             clusterstate.PodCliqueScalingGroupView,
			SelectedScalingGroup: "my-pcsg",
		},
		DataState: DataState{allResources: make(map[string][]clusterstate.Resource)},
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.resourcesTable.SetRows([]table.Row{
		{"default", "(PodCliqueScalingGroup replica)", "my-pcsg-replica-0", "N/A", "1/1", "1/1"},
	})
	m.resourcesTable.SetCursor(0)

	m.rebuildEventsFromSnapshot(snapshot)

	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 event for PCSG replica, got %d", len(m.allEvents))
	}
}

func TestRebuildEventsFromSnapshot_PodCliqueScalingGroupReplicaView_PodCliqueRow(t *testing.T) {
	now := time.Now()
	snapshot := &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:           samplePCSResources(),
			ReplicaIndexesByPCS:     map[string][]string{},
			ScalingGroupsByReplica:  map[string][]clusterstate.Resource{},
			PodCliquesByReplica:     map[string][]clusterstate.Resource{},
			ReplicaIndexesByPCSG:    map[string][]string{},
			PodCliquesByPCSG:        map[string][]clusterstate.Resource{},
			PodCliquesByPCSGReplica: map[string][]clusterstate.Resource{},
			PodsByPodClique:         map[string][]clusterstate.Resource{},
		},
		EventsByObject: map[string][]clusterstate.Event{
			"PodClique/my-pcsg-0-worker": {
				{Type: "Normal", Reason: "Synced", Message: "PC event in PCSG replica", Parent: "my-pcsg-0-worker", Timestamp: now},
			},
		},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:                 clusterstate.PodCliqueScalingGroupReplicaView,
			SelectedScalingGroup:     "my-pcsg",
			SelectedPCSGReplicaIndex: "0",
		},
		DataState: DataState{allResources: make(map[string][]clusterstate.Resource)},
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
	snapshot := &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:          samplePCSResources(),
			ReplicaIndexesByPCS:    map[string][]string{},
			ScalingGroupsByReplica: map[string][]clusterstate.Resource{},
			PodCliquesByReplica:    map[string][]clusterstate.Resource{},
			ReplicaIndexesByPCSG:   map[string][]string{},
			PodCliquesByPCSG:       map[string][]clusterstate.Resource{},
			PodCliquesByPCSGReplica: map[string][]clusterstate.Resource{
				"my-pcsg/0": {{Name: "my-pcsg-0-worker", Type: "PodClique"}},
			},
			PodsByPodClique: map[string][]clusterstate.Resource{},
		},
		EventsByObject: map[string][]clusterstate.Event{
			"PodClique/my-pcsg-0-worker": {
				{Type: "Normal", Reason: "Synced", Message: "fallback event", Parent: "my-pcsg-0-worker", Timestamp: now},
			},
		},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:                 clusterstate.PodCliqueScalingGroupReplicaView,
			SelectedScalingGroup:     "my-pcsg",
			SelectedPCSGReplicaIndex: "0",
		},
		DataState: DataState{allResources: make(map[string][]clusterstate.Resource)},
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
	snapshot := &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:           samplePCSResources(),
			ReplicaIndexesByPCS:     map[string][]string{},
			ScalingGroupsByReplica:  map[string][]clusterstate.Resource{},
			PodCliquesByReplica:     map[string][]clusterstate.Resource{},
			ReplicaIndexesByPCSG:    map[string][]string{},
			PodCliquesByPCSG:        map[string][]clusterstate.Resource{},
			PodCliquesByPCSGReplica: map[string][]clusterstate.Resource{},
			PodsByPodClique:         map[string][]clusterstate.Resource{},
		},
		EventsByObject: map[string][]clusterstate.Event{
			"PodClique/my-pc": {
				{Type: "Normal", Reason: "Ready", Message: "pc event", Parent: "my-pc", Timestamp: now},
			},
		},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:          clusterstate.PodCliqueView,
			SelectedPodClique: "my-pc",
		},
		DataState: DataState{allResources: make(map[string][]clusterstate.Resource)},
	}
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)

	m.rebuildEventsFromSnapshot(snapshot)

	if len(m.allEvents) != 1 {
		t.Fatalf("expected 1 event for PodClique, got %d", len(m.allEvents))
	}
}

func TestRebuildEventsFromSnapshot_PodView(t *testing.T) {
	now := time.Now()
	snapshot := &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:           samplePCSResources(),
			ReplicaIndexesByPCS:     map[string][]string{},
			ScalingGroupsByReplica:  map[string][]clusterstate.Resource{},
			PodCliquesByReplica:     map[string][]clusterstate.Resource{},
			ReplicaIndexesByPCSG:    map[string][]string{},
			PodCliquesByPCSG:        map[string][]clusterstate.Resource{},
			PodCliquesByPCSGReplica: map[string][]clusterstate.Resource{},
			PodsByPodClique: map[string][]clusterstate.Resource{
				"my-pc": {{Name: "my-pod", Type: "Pod"}},
			},
		},
		EventsByObject: map[string][]clusterstate.Event{
			"PodClique/my-pc": {
				{Type: "Normal", Reason: "Ready", Message: "pod view event", Parent: "my-pc", Timestamp: now},
			},
			"Pod/my-pod": {
				{Type: "Normal", Reason: "Scheduled", Message: "pod scheduled", Parent: "my-pod", Timestamp: now},
			},
		},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:          clusterstate.PodView,
			SelectedPodClique: "my-pc",
			SelectedPod:       "my-pod",
		},
		DataState: DataState{allResources: make(map[string][]clusterstate.Resource)},
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
		viewState: clusterstate.ViewState{ViewType: clusterstate.ForestView},
		DataState: DataState{allEvents: []clusterstate.Event{{Type: "old"}}},
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
	summary := &clusterstate.GPUSummary{
		GPUTypes: []string{"H100"},
		ByPCSGReplica: map[string]clusterstate.GPUCounts{
			"my-pcsg/0": {"H100": 4},
		},
	}

	m := Model{
		DataState: DataState{gpuSummary: summary},
		viewState: clusterstate.ViewState{
			SelectedScalingGroup: "my-pcsg",
		},
	}

	counts := m.gpuCountsForResource(clusterstate.Resource{
		Name: "my-pcsg-replica-0",
		Type: "(PodCliqueScalingGroup replica)",
	})

	if counts == nil {
		t.Fatal("expected non-nil counts for PodCliqueScalingGroupReplica")
	}
	if counts["H100"] != 4 {
		t.Errorf("expected H100=4, got %d", counts["H100"])
	}
}

func TestGpuCountsForResource_PodCliqueScalingGroupReplica_EmptyPCSGName(t *testing.T) {
	summary := &clusterstate.GPUSummary{
		GPUTypes: []string{"H100"},
		ByPCSGReplica: map[string]clusterstate.GPUCounts{
			"my-pcsg/0": {"H100": 4},
		},
	}

	m := Model{
		DataState: DataState{gpuSummary: summary},
		viewState: clusterstate.ViewState{
			SelectedScalingGroup: "", // empty pcsg name
		},
	}

	counts := m.gpuCountsForResource(clusterstate.Resource{
		Name: "my-pcsg-replica-0",
		Type: "(PodCliqueScalingGroup replica)",
	})

	// With empty pcsgName, key construction fails — returns nil
	if counts != nil {
		t.Fatal("expected nil counts when SelectedScalingGroup is empty")
	}
}

func TestGpuCountsForResource_PodCliqueScalingGroupReplica_BadReplicaIndex(t *testing.T) {
	summary := &clusterstate.GPUSummary{
		GPUTypes: []string{"H100"},
		ByPCSGReplica: map[string]clusterstate.GPUCounts{
			"my-pcsg/0": {"H100": 2},
		},
	}

	m := Model{
		DataState: DataState{gpuSummary: summary},
		viewState: clusterstate.ViewState{
			SelectedScalingGroup: "my-pcsg",
		},
	}

	// Name without "-replica-" suffix — extractReplicaIndex returns ""
	counts := m.gpuCountsForResource(clusterstate.Resource{
		Name: "my-pcsg-no-replica-suffix",
		Type: "(PodCliqueScalingGroup replica)",
	})

	// With bad replica index, key construction fails — returns nil
	if counts != nil {
		t.Fatal("expected nil counts when replica index cannot be extracted")
	}
}

func TestGpuCountsForResource_PodCliqueSetReplica_EmptyPCSName(t *testing.T) {
	summary := &clusterstate.GPUSummary{
		GPUTypes:  []string{"H100"},
		ByReplica: map[string]clusterstate.GPUCounts{"pcs-a/0": {"H100": 8}},
	}

	m := Model{
		DataState: DataState{gpuSummary: summary},
		viewState: clusterstate.ViewState{
			SelectedPodCliqueSet: "", // empty pcsName
		},
	}

	counts := m.gpuCountsForResource(clusterstate.Resource{
		Name: "pcs-a-replica-0",
		Type: "(PodCliqueSet replica)",
	})

	// pcsName is "" so the lookup key is "/0" → no match → nil
	if counts != nil {
		t.Errorf("expected nil counts for empty pcsName, got %v", counts)
	}
}

func TestGpuCountsForResource_PodCliqueSetReplica_BadReplicaIndex(t *testing.T) {
	summary := &clusterstate.GPUSummary{
		GPUTypes:  []string{"H100"},
		ByReplica: map[string]clusterstate.GPUCounts{"pcs-a/0": {"H100": 8}},
	}

	m := Model{
		DataState: DataState{gpuSummary: summary},
		viewState: clusterstate.ViewState{
			SelectedPodCliqueSet: "pcs-a",
		},
	}

	// Name without "-replica-" => replicaIndex = "" => short-circuit, return nil
	counts := m.gpuCountsForResource(clusterstate.Resource{
		Name: "pcs-a-no-replica",
		Type: "(PodCliqueSet replica)",
	})

	if counts != nil {
		t.Errorf("expected nil counts for bad replica index, got %v", counts)
	}
}

// ===========================================================================
// Phase 3, Item 4: getFilteredEvents
// ===========================================================================

func TestGetFilteredEvents_PodCliqueView_PodRowSelected(t *testing.T) {
	allEvents := []clusterstate.Event{
		{Type: "Normal", Parent: "pod-a", Reason: "Scheduled", Message: "pod-a scheduled"},
		{Type: "Normal", Parent: "pod-b", Reason: "Scheduled", Message: "pod-b scheduled"},
		{Type: "Warning", Parent: "pc-a", Reason: "Failed", Message: "pc-a failed"},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:          clusterstate.PodCliqueView,
			SelectedPodClique: "pc-a",
		},
		DataState: DataState{allEvents: allEvents},
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
	allEvents := []clusterstate.Event{
		{Type: "Normal", Parent: "pod-a", Reason: "Scheduled", Message: "event 1"},
		{Type: "Warning", Parent: "pod-b", Reason: "Failed", Message: "event 2"},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:          clusterstate.PodCliqueView,
			SelectedPodClique: "pc-a",
		},
		DataState: DataState{allEvents: allEvents},
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
	allEvents := []clusterstate.Event{
		{Type: "Normal", Parent: "pod-a", Reason: "Scheduled", Message: "event 1"},
	}

	m := Model{
		viewState: clusterstate.ViewState{
			ViewType:          clusterstate.PodCliqueView,
			SelectedPodClique: "pc-a",
		},
		DataState: DataState{allEvents: allEvents},
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
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{
					{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
					// "rack" domain was removed
				},
			},
			topologyDrill: clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
				{Domain: "rack", Key: "topology.kubernetes.io/rack", Value: ""},
			}),
		},
	}

	m.validateTopologyDrillStack()

	// Stack should be truncated to depth 1 (region preserved, rack removed)
	if m.topologyDrill.Depth() != 1 || m.topologyDrill.Entries()[0].Domain != "region" {
		t.Errorf("expected drill stack truncated to [region], got %v", m.topologyDrill.Entries())
	}
}

func TestValidateTopologyDrillStack_AllDomainsValid_StackPreserved(t *testing.T) {
	m := Model{
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{
					{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
					{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
				},
			},
			topologyDrill: clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: ""},
			}),
		},
	}

	m.validateTopologyDrillStack()

	if m.topologyDrill.Depth() != 2 {
		t.Errorf("expected drill stack preserved with 2 entries, got %d", m.topologyDrill.Depth())
	}
}

func TestValidateTopologyDrillStack_PartiallyStale(t *testing.T) {
	m := Model{
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{
					{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
					// "zone" was removed, "rack" still exists
					{Domain: "rack", Key: "topology.kubernetes.io/rack", ValuesCount: 6},
				},
			},
			topologyDrill: clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: "us-east-1a"},
				{Domain: "rack", Key: "topology.kubernetes.io/rack", Value: ""},
			}),
		},
	}

	m.validateTopologyDrillStack()

	// "zone" no longer exists → stack should be truncated to [region]
	if m.topologyDrill.Depth() != 1 || m.topologyDrill.Entries()[0].Domain != "region" {
		t.Errorf("expected drill stack truncated to [region], got %v", m.topologyDrill.Entries())
	}
}

func TestValidateTopologyDrillStack_NilTopologyViewData(t *testing.T) {
	m := Model{
		TopologyState: TopologyState{
			topologyViewData: nil,
			topologyDrill: clusterstate.NewTopologyDrillStack([]clusterstate.TopologyDrillSelection{
				{Domain: "region", Key: "k", Value: ""},
			}),
		},
	}

	m.validateTopologyDrillStack()

	// Stack should remain unchanged (nil topologyViewData triggers early return)
	if m.topologyDrill.Depth() != 1 {
		t.Errorf("expected drill stack unchanged for nil topologyViewData, got %d", m.topologyDrill.Depth())
	}
}

func TestValidateTopologyDrillStack_EmptyStack(t *testing.T) {
	m := Model{
		TopologyState: TopologyState{
			topologyViewData: &clusterstate.TopologyViewData{
				Domains: []clusterstate.TopologyDomainRow{
					{Domain: "region", Key: "k", ValuesCount: 1},
				},
			},
		},
	}

	// Should not panic
	m.validateTopologyDrillStack()

	if !m.topologyDrill.IsEmpty() {
		t.Errorf("expected empty drill stack to remain empty, got %v", m.topologyDrill.Entries())
	}
}

// ===========================================================================
// Phase 3, Item 6: applySnapshot
// ===========================================================================

func TestApplySnapshot_TopologyInfoRebuiltForSelectedPCS(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
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
	snap.TopologyViewData = &clusterstate.TopologyViewData{
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
	mc := clusterstate.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = samplePCSResources()
	snap.PodCliqueSetSpecs = map[string]*corev1alpha1.PodCliqueSet{
		"alpha-pcs": {
			ObjectMeta: metav1.ObjectMeta{Name: "alpha-pcs", Namespace: "default"},
			Spec:       corev1alpha1.PodCliqueSetSpec{Replicas: 1},
		},
	}
	snap.TopologyViewData = &clusterstate.TopologyViewData{
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
	mc := clusterstate.NewMockGlobalCache()
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
	if m.viewState.ViewType != clusterstate.TopologyView {
		t.Fatalf("expected TopologyView, got %s", clusterstate.ViewTypeName(m.viewState.ViewType))
	}

	// Verify domains table is populated
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 domain rows, got %d", len(rows))
	}

	// Now update the snapshot with different topology data
	newSnap := mc.Snapshot()
	newSnap.TopologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "block", Key: "topology.io/block", ValuesCount: 2},
		},
		NodeLabels:  map[string]map[string]string{},
		Pods:        []clusterstate.TopologyViewPod{},
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
	mc := clusterstate.NewMockGlobalCache()
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
	mc := clusterstate.NewMockGlobalCache()
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
