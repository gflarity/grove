package tui

import (
	"testing"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
)

// ---------------------------------------------------------------------------
// computeWeightedColumns
// ---------------------------------------------------------------------------

func TestComputeWeightedColumns(t *testing.T) {
	t.Run("proportional distribution", func(t *testing.T) {
		specs := []ColumnSpec{
			{Title: "A", Weight: 1},
			{Title: "B", Weight: 2},
			{Title: "C", Weight: 1},
		}
		cols := computeWeightedColumns(specs, 100)
		if len(cols) != 3 {
			t.Fatalf("expected 3 columns, got %d", len(cols))
		}
		// unit = 100/4 = 25
		if cols[0].Width != 25 {
			t.Errorf("col[0].Width = %d, want 25", cols[0].Width)
		}
		if cols[1].Width != 50 {
			t.Errorf("col[1].Width = %d, want 50", cols[1].Width)
		}
		// Last column absorbs remainder: 25 + (100 - 100) = 25
		if cols[2].Width != 25 {
			t.Errorf("col[2].Width = %d, want 25", cols[2].Width)
		}
	})

	t.Run("rounding remainder goes to last column", func(t *testing.T) {
		specs := []ColumnSpec{
			{Title: "A", Weight: 1},
			{Title: "B", Weight: 1},
			{Title: "C", Weight: 1},
		}
		// 101 / 3 = 33 per unit, used = 99, remainder = 2
		cols := computeWeightedColumns(specs, 101)
		if len(cols) != 3 {
			t.Fatalf("expected 3 columns, got %d", len(cols))
		}
		totalWidth := 0
		for _, c := range cols {
			totalWidth += c.Width
		}
		if totalWidth != 101 {
			t.Errorf("total width = %d, want 101", totalWidth)
		}
		if cols[2].Width != 35 { // 33 + 2
			t.Errorf("last col width = %d, want 35", cols[2].Width)
		}
	})

	t.Run("empty specs returns nil", func(t *testing.T) {
		cols := computeWeightedColumns(nil, 100)
		if cols != nil {
			t.Errorf("expected nil for empty specs, got %v", cols)
		}
	})

	t.Run("zero weights uses equal distribution", func(t *testing.T) {
		specs := []ColumnSpec{
			{Title: "A", Weight: 0},
			{Title: "B", Weight: 0},
		}
		cols := computeWeightedColumns(specs, 100)
		if len(cols) != 2 {
			t.Fatalf("expected 2 columns, got %d", len(cols))
		}
		totalWidth := cols[0].Width + cols[1].Width
		if totalWidth != 100 {
			t.Errorf("total width = %d, want 100", totalWidth)
		}
	})

	t.Run("preserves titles", func(t *testing.T) {
		specs := []ColumnSpec{
			{Title: "NAMESPACE", Weight: 2},
			{Title: "NAME", Weight: 5},
		}
		cols := computeWeightedColumns(specs, 70)
		if cols[0].Title != "NAMESPACE" {
			t.Errorf("col[0].Title = %q, want %q", cols[0].Title, "NAMESPACE")
		}
		if cols[1].Title != "NAME" {
			t.Errorf("col[1].Title = %q, want %q", cols[1].Title, "NAME")
		}
	})

	t.Run("single column gets full width", func(t *testing.T) {
		specs := []ColumnSpec{{Title: "ONLY", Weight: 1}}
		cols := computeWeightedColumns(specs, 200)
		if len(cols) != 1 || cols[0].Width != 200 {
			t.Errorf("single col width = %d, want 200", cols[0].Width)
		}
	})
}

// ---------------------------------------------------------------------------
// tableContentWidth
// ---------------------------------------------------------------------------

func TestTableContentWidth(t *testing.T) {
	t.Run("normal calculation", func(t *testing.T) {
		// termWidth=120, 6 columns -> 120 - 2 (borders) - 12 (6*2 padding) = 106
		w := tableContentWidth(120, 6)
		if w != 106 {
			t.Errorf("tableContentWidth(120, 6) = %d, want 106", w)
		}
	})

	t.Run("minimum 80", func(t *testing.T) {
		// Very narrow terminal: 50 - 2 - 12 = 36 -> clamped to 80
		w := tableContentWidth(50, 6)
		if w != 80 {
			t.Errorf("tableContentWidth(50, 6) = %d, want 80", w)
		}
	})

	t.Run("zero columns", func(t *testing.T) {
		// 120 - 2 - 0 = 118
		w := tableContentWidth(120, 0)
		if w != 118 {
			t.Errorf("tableContentWidth(120, 0) = %d, want 118", w)
		}
	})

	t.Run("many columns narrows content", func(t *testing.T) {
		// 120 - 2 - 40 = 78 -> clamped to 80
		w := tableContentWidth(120, 20)
		if w != 80 {
			t.Errorf("tableContentWidth(120, 20) = %d, want 80", w)
		}
	})
}

// ---------------------------------------------------------------------------
// extractReplicaIndex
// ---------------------------------------------------------------------------

func TestExtractReplicaIndex(t *testing.T) {
	tests := []struct {
		name string
		input string
		want string
	}{
		{name: "normal replica name", input: "my-pcs-replica-0", want: "0"},
		{name: "multi-digit index", input: "pcs-replica-12", want: "12"},
		{name: "no replica suffix", input: "my-pcs-name", want: ""},
		{name: "empty string", input: "", want: ""},
		{name: "just prefix", input: "-replica-", want: ""},
		{name: "replica keyword in name", input: "replica-pcs-replica-3", want: "3"},
		{name: "nested hyphen pcs name", input: "my-cool-pcs-replica-7", want: "7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractReplicaIndex(tt.input)
			if got != tt.want {
				t.Errorf("extractReplicaIndex(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// gpuCountsForResource
// ---------------------------------------------------------------------------

func TestGpuCountsForResource(t *testing.T) {
	summary := &clusterstate.GPUSummary{
		GPUTypes: []string{"H100", "A100"},
		ByPCS: map[string]clusterstate.GPUCounts{
			"pcs-a": {"H100": 16, "A100": 8},
		},
		ByReplica: map[string]clusterstate.GPUCounts{
			"pcs-a/0": {"H100": 8},
		},
		ByPCSG: map[string]clusterstate.GPUCounts{
			"pcsg-x": {"A100": 4},
		},
		ByPodClique: map[string]clusterstate.GPUCounts{
			"pc-1": {"H100": 2},
		},
		ByPod: map[string]clusterstate.GPUCounts{
			"pod-1": {"H100": 1},
		},
	}

	m := Model{
		DataState: DataState{gpuSummary: summary},
		viewState: clusterstate.ViewState{
			SelectedPodCliqueSet: "pcs-a",
		},
	}

	t.Run("PodCliqueSet", func(t *testing.T) {
		counts := m.gpuCountsForResource(clusterstate.Resource{Name: "pcs-a", Type: "PodCliqueSet"})
		if counts["H100"] != 16 || counts["A100"] != 8 {
			t.Errorf("PCS counts = %v, want H100:16 A100:8", counts)
		}
	})

	t.Run("(PodCliqueSet replica)", func(t *testing.T) {
		counts := m.gpuCountsForResource(clusterstate.Resource{Name: "pcs-a-replica-0", Type: "(PodCliqueSet replica)"})
		if counts["H100"] != 8 {
			t.Errorf("replica counts = %v, want H100:8", counts)
		}
	})

	t.Run("PodCliqueScalingGroup", func(t *testing.T) {
		counts := m.gpuCountsForResource(clusterstate.Resource{Name: "pcsg-x", Type: "PodCliqueScalingGroup"})
		if counts["A100"] != 4 {
			t.Errorf("PCSG counts = %v, want A100:4", counts)
		}
	})

	t.Run("PodClique", func(t *testing.T) {
		counts := m.gpuCountsForResource(clusterstate.Resource{Name: "pc-1", Type: "PodClique"})
		if counts["H100"] != 2 {
			t.Errorf("PodClique counts = %v, want H100:2", counts)
		}
	})

	t.Run("Pod", func(t *testing.T) {
		counts := m.gpuCountsForResource(clusterstate.Resource{Name: "pod-1", Type: "Pod"})
		if counts["H100"] != 1 {
			t.Errorf("Pod counts = %v, want H100:1", counts)
		}
	})

	t.Run("unknown resource", func(t *testing.T) {
		counts := m.gpuCountsForResource(clusterstate.Resource{Name: "unknown", Type: "Unknown"})
		if counts != nil {
			t.Errorf("unknown resource counts = %v, want nil", counts)
		}
	})

	t.Run("nil gpuSummary", func(t *testing.T) {
		m2 := Model{DataState: DataState{gpuSummary: nil}}
		counts := m2.gpuCountsForResource(clusterstate.Resource{Name: "pcs-a", Type: "PodCliqueSet"})
		if counts != nil {
			t.Errorf("nil summary counts = %v, want nil", counts)
		}
	})
}

// ---------------------------------------------------------------------------
// isResourcePending
// ---------------------------------------------------------------------------

func TestIsResourcePending(t *testing.T) {
	summary := &clusterstate.GPUSummary{
		PendingGPUPods: map[string]int64{
			"pending-pod": 8,
		},
	}

	m := Model{DataState: DataState{gpuSummary: summary}}

	t.Run("pending Pod", func(t *testing.T) {
		if !m.isResourcePending(clusterstate.Resource{Name: "pending-pod", Type: "Pod"}) {
			t.Error("expected pending Pod to return true")
		}
	})

	t.Run("non-pending Pod", func(t *testing.T) {
		if m.isResourcePending(clusterstate.Resource{Name: "running-pod", Type: "Pod"}) {
			t.Error("expected non-pending Pod to return false")
		}
	})

	t.Run("non-Pod resource is never pending", func(t *testing.T) {
		if m.isResourcePending(clusterstate.Resource{Name: "pcs-a", Type: "PodCliqueSet"}) {
			t.Error("expected non-Pod to return false")
		}
	})

	t.Run("nil gpuSummary", func(t *testing.T) {
		m2 := Model{DataState: DataState{gpuSummary: nil}}
		if m2.isResourcePending(clusterstate.Resource{Name: "pending-pod", Type: "Pod"}) {
			t.Error("nil summary should return false")
		}
	})

	t.Run("empty PendingGPUPods", func(t *testing.T) {
		m2 := Model{DataState: DataState{gpuSummary: &clusterstate.GPUSummary{PendingGPUPods: map[string]int64{}}}}
		if m2.isResourcePending(clusterstate.Resource{Name: "pending-pod", Type: "Pod"}) {
			t.Error("empty pending map should return false")
		}
	})
}

// ---------------------------------------------------------------------------
// getFilteredEvents
// ---------------------------------------------------------------------------

func TestGetFilteredEvents(t *testing.T) {
	now := time.Now()

	allEvents := []clusterstate.Event{
		{Type: "Normal", Parent: "pod-a", Reason: "Scheduled", Message: "pod-a scheduled", Timestamp: now},
		{Type: "Warning", Parent: "pod-b", Reason: "Failed", Message: "pod-b failed", Timestamp: now},
		{Type: "Normal", Parent: "pc-a", Reason: "Created", Message: "pc-a created", Timestamp: now},
	}

	t.Run("PodView filters to selected pod", func(t *testing.T) {
		m := Model{
			viewState: clusterstate.ViewState{
				ViewType:    clusterstate.PodView,
				SelectedPod: "pod-a",
			},
			DataState: DataState{allEvents: allEvents},
		}

		events := m.getFilteredEvents()
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].Parent != "pod-a" {
			t.Errorf("event parent = %q, want %q", events[0].Parent, "pod-a")
		}
	})

	t.Run("PodView with no matching events", func(t *testing.T) {
		m := Model{
			viewState: clusterstate.ViewState{
				ViewType:    clusterstate.PodView,
				SelectedPod: "pod-nonexistent",
			},
			DataState: DataState{allEvents: allEvents},
		}

		events := m.getFilteredEvents()
		if len(events) != 0 {
			t.Errorf("expected 0 events for nonexistent pod, got %d", len(events))
		}
	})

	t.Run("ForestView returns all events", func(t *testing.T) {
		m := Model{
			viewState: clusterstate.ViewState{
				ViewType: clusterstate.ForestView,
			},
			DataState: DataState{allEvents: allEvents},
		}

		events := m.getFilteredEvents()
		if len(events) != 3 {
			t.Errorf("expected 3 events in forest view, got %d", len(events))
		}
	})

	t.Run("empty allEvents returns empty", func(t *testing.T) {
		m := Model{
			viewState: clusterstate.ViewState{
				ViewType: clusterstate.ForestView,
			},
			DataState: DataState{allEvents: nil},
		}

		events := m.getFilteredEvents()
		if len(events) != 0 {
			t.Errorf("expected 0 events for nil allEvents, got %d", len(events))
		}
	})
}
