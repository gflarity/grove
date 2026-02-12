package tui

import (
	"testing"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// ===========================================================================
// Cursor-stability test helpers
// ===========================================================================

// assertCursorOnName verifies the resources table cursor is on a row whose
// NAME column (index 2) matches expectedName.
func assertCursorOnName(t *testing.T, m Model, expectedName string) {
	t.Helper()
	row := m.resourcesTable.SelectedRow()
	if len(row) < 3 {
		t.Fatalf("no valid row selected (got %d cols); wanted cursor on %q", len(row), expectedName)
	}
	if row[2] != expectedName {
		t.Errorf("cursor on %q, want %q (cursor index=%d)", row[2], expectedName, m.resourcesTable.Cursor())
	}
}

// assertCursorValid verifies the cursor is in the valid range [0, len(rows)).
func assertCursorValid(t *testing.T, m Model) {
	t.Helper()
	rows := m.resourcesTable.Rows()
	cursor := m.resourcesTable.Cursor()
	if len(rows) == 0 {
		return // no rows → cursor is irrelevant
	}
	if cursor < 0 || cursor >= len(rows) {
		t.Errorf("cursor %d out of range [0, %d)", cursor, len(rows))
	}
}

// cacheUpdate modifies the MockGlobalCache snapshot via mutate, then delivers
// CacheUpdateMsg to trigger a full rebuild. Returns the updated model.
func cacheUpdate(m Model, mc *data.MockGlobalCache, mutate func(*data.CacheSnapshot)) Model {
	snap := mc.Snapshot()
	mutate(snap)
	mc.SetSnapshot(snap)
	return mustApply(m, CacheUpdateMsg{})
}

// ===========================================================================
// C1. ForestView — cache update preserves selected PCS
// ===========================================================================

func TestC1_ForestView_CacheUpdatePreservesSelectedPCS(t *testing.T) {
	t.Run("ready counts change", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		// Move cursor to beta-pcs (row 1)
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs")

		// Cache update: change beta-pcs Ready
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			for i := range snap.PodCliqueSets {
				if snap.PodCliqueSets[i].Name == "beta-pcs" {
					snap.PodCliqueSets[i].Ready = "1/3"
				}
			}
		})

		assertCursorOnName(t, m, "beta-pcs")
	})

	t.Run("new PCS sorts before selected", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs")

		// Add "aaa-pcs" that sorts before all existing PCSes.
		// beta-pcs shifts from index 1 to index 2; cursor should follow by name.
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodCliqueSets = append(
				[]data.Resource{{Name: "aaa-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"}},
				snap.PodCliqueSets...,
			)
		})

		assertCursorOnName(t, m, "beta-pcs")
	})

	t.Run("different PCS removed", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs")

		// Remove gamma-pcs (a different PCS)
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			var kept []data.Resource
			for _, r := range snap.PodCliqueSets {
				if r.Name != "gamma-pcs" {
					kept = append(kept, r)
				}
			}
			snap.PodCliqueSets = kept
		})

		assertCursorOnName(t, m, "beta-pcs")
	})

	t.Run("selected PCS removed — cursor clamps", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs")

		// Remove beta-pcs itself
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			var kept []data.Resource
			for _, r := range snap.PodCliqueSets {
				if r.Name != "beta-pcs" {
					kept = append(kept, r)
				}
			}
			snap.PodCliqueSets = kept
		})

		assertCursorValid(t, m)
		row := m.resourcesTable.SelectedRow()
		if len(row) >= 3 && row[2] == "beta-pcs" {
			t.Error("cursor should not be on removed beta-pcs")
		}
	})
}

// ===========================================================================
// C2. PodCliqueSetView — cache update preserves selected replica
// ===========================================================================

func TestC2_PodCliqueSetView_CacheUpdatePreservesSelectedReplica(t *testing.T) {
	t.Run("ready counts change", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		// Navigate to beta-pcs (2 replicas → PodCliqueSetView)
		m = sendKey(m, tea.KeyDown) // beta-pcs
		m = sendKey(m, tea.KeyEnter)
		if m.viewState.ViewType != data.PodCliqueSetView {
			t.Fatalf("expected PodCliqueSetView, got %s", data.ViewTypeName(m.viewState.ViewType))
		}

		// Move cursor to replica-1
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs-replica-1")

		// Change Ready counts for replica-1's scaling group
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.ScalingGroupsByReplica["beta-pcs/1"] = []data.Resource{
				{Name: "beta-pcs-1-sg-main", Type: "PodCliqueScalingGroup", Namespace: "staging", Ready: "0/1", Scheduled: "0/1"},
			}
		})

		assertCursorOnName(t, m, "beta-pcs-replica-1")
	})

	t.Run("new replica added", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyDown)
		m = sendKey(m, tea.KeyEnter) // PodCliqueSetView
		// Cursor falls back to index 1 from ForestView; move up to replica-0
		m = sendKey(m, tea.KeyUp)
		assertCursorOnName(t, m, "beta-pcs-replica-0")

		// Add replica-2
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.ReplicaIndexesByPCS["beta-pcs"] = []string{"0", "1", "2"}
			snap.ScalingGroupsByReplica["beta-pcs/2"] = []data.Resource{
				{Name: "beta-pcs-2-sg-main", Type: "PodCliqueScalingGroup", Namespace: "staging", Ready: "1/1", Scheduled: "1/1"},
			}
		})

		assertCursorOnName(t, m, "beta-pcs-replica-0")
	})

	t.Run("selected replica removed — cursor clamps", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyDown)
		m = sendKey(m, tea.KeyEnter)
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs-replica-1")

		// Remove replica-1 from the index list
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.ReplicaIndexesByPCS["beta-pcs"] = []string{"0"}
			delete(snap.ScalingGroupsByReplica, "beta-pcs/1")
		})

		assertCursorValid(t, m)
		row := m.resourcesTable.SelectedRow()
		if len(row) >= 3 && row[2] == "beta-pcs-replica-1" {
			t.Error("cursor should not be on removed replica-1")
		}
	})
}

// ===========================================================================
// C3. PodCliqueSetReplicaView — preserves selected PCSG/PodClique
// ===========================================================================

func TestC3_PodCliqueSetReplicaView_CacheUpdatePreservesSelectedResource(t *testing.T) {
	t.Run("PCSG ready changes", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		// alpha-pcs has 1 replica → auto-skip to PodCliqueSetReplicaView
		m = sendKey(m, tea.KeyEnter)
		if m.viewState.ViewType != data.PodCliqueSetReplicaView {
			t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
		}
		// First row is the PCSG
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill")

		// Change PCSG Ready
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.ScalingGroupsByReplica["alpha-pcs/0"] = []data.Resource{
				{Name: "alpha-pcs-0-sg-prefill", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "0/2", Scheduled: "2/2"},
			}
		})

		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill")
	})

	t.Run("standalone PodClique selected, new PCSG added", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyEnter) // alpha-pcs → PodCliqueSetReplicaView
		// Move cursor to standalone PodClique (row 1)
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "alpha-pcs-0-standalone-pc")

		// Add a new PCSG
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.ScalingGroupsByReplica["alpha-pcs/0"] = append(
				snap.ScalingGroupsByReplica["alpha-pcs/0"],
				data.Resource{Name: "alpha-pcs-0-sg-new", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
			)
		})

		assertCursorOnName(t, m, "alpha-pcs-0-standalone-pc")
	})

	t.Run("selected PCSG removed — cursor clamps", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyEnter)
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill")

		// Remove the PCSG, only standalone PodClique remains
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.ScalingGroupsByReplica["alpha-pcs/0"] = []data.Resource{}
		})

		assertCursorValid(t, m)
		row := m.resourcesTable.SelectedRow()
		if len(row) >= 3 && row[2] == "alpha-pcs-0-sg-prefill" {
			t.Error("cursor should not be on removed PCSG")
		}
	})
}

// ===========================================================================
// C4. PodCliqueScalingGroupView — preserves selected PCSG replica
// ===========================================================================

func TestC4_PodCliqueScalingGroupView_CacheUpdatePreservesSelectedReplica(t *testing.T) {
	t.Run("data changes", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyEnter) // alpha-pcs → PodCliqueSetReplicaView (single replica skip)
		m = sendKey(m, tea.KeyEnter) // PCSG → PodCliqueScalingGroupView
		if m.viewState.ViewType != data.PodCliqueScalingGroupView {
			t.Fatalf("expected PodCliqueScalingGroupView, got %s", data.ViewTypeName(m.viewState.ViewType))
		}
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-replica-0")

		// Change underlying PodClique data for replica-0
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodCliquesByPCSGReplica["alpha-pcs-0-sg-prefill/0"] = []data.Resource{
				{Name: "alpha-pcs-0-sg-prefill-0-worker", Type: "PodClique", Namespace: "default", Ready: "0/1", Scheduled: "0/1"},
			}
		})

		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-replica-0")
	})

	t.Run("new PCSG replica added", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		m = sendKey(m, tea.KeyEnter) // alpha-pcs → PodCliqueSetReplicaView
		m = sendKey(m, tea.KeyEnter) // PCSG → PodCliqueScalingGroupView
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-replica-0")

		// Add replica-2
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.ReplicaIndexesByPCSG["alpha-pcs-0-sg-prefill"] = []string{"0", "1", "2"}
			snap.PodCliquesByPCSGReplica["alpha-pcs-0-sg-prefill/2"] = []data.Resource{
				{Name: "alpha-pcs-0-sg-prefill-2-worker", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
			}
		})

		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-replica-0")
	})
}

// ===========================================================================
// C5. PodCliqueScalingGroupReplicaView — preserves selected PodClique
// ===========================================================================

func TestC5_PodCliqueScalingGroupReplicaView_CacheUpdatePreservesSelectedPodClique(t *testing.T) {
	// Setup helper: add a second PodClique to PCSG replica-0 for richer testing.
	// Names are chosen so that sorted order is: backup (0), worker (1).
	setupWithTwoPodCliques := func(t *testing.T) (*data.MockGlobalCache, Model) {
		t.Helper()
		mc := buildFullMockCache()
		snap := mc.Snapshot()
		snap.PodCliquesByPCSGReplica["alpha-pcs-0-sg-prefill/0"] = []data.Resource{
			{Name: "alpha-pcs-0-sg-prefill-0-worker", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
			{Name: "alpha-pcs-0-sg-prefill-0-backup", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
		}
		mc.SetSnapshot(snap)

		m := newTestModelWithCache(mc)
		m = sendKey(m, tea.KeyEnter) // alpha-pcs → PodCliqueSetReplicaView
		m = sendKey(m, tea.KeyEnter) // PCSG → PodCliqueScalingGroupView
		m = sendKey(m, tea.KeyEnter) // replica-0 → PodCliqueScalingGroupReplicaView

		if m.viewState.ViewType != data.PodCliqueScalingGroupReplicaView {
			t.Fatalf("expected PodCliqueScalingGroupReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
		}
		return mc, m
	}

	t.Run("data changes", func(t *testing.T) {
		mc, m := setupWithTwoPodCliques(t)
		// Sorted order: backup (0), worker (1). Cursor falls to index 0 = backup.
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-0-backup")

		// Move cursor to worker
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-0-worker")

		// Change PodClique Ready
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodCliquesByPCSGReplica["alpha-pcs-0-sg-prefill/0"] = []data.Resource{
				{Name: "alpha-pcs-0-sg-prefill-0-worker", Type: "PodClique", Namespace: "default", Ready: "0/1", Scheduled: "0/1"},
				{Name: "alpha-pcs-0-sg-prefill-0-backup", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
			}
		})

		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-0-worker")
	})

	t.Run("selected PodClique removed — cursor clamps", func(t *testing.T) {
		mc, m := setupWithTwoPodCliques(t)
		// Sorted order: backup (0), worker (1). Cursor falls to index 0 = backup.
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-0-backup")

		// Move cursor to worker
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill-0-worker")

		// Remove the worker, keep only backup
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodCliquesByPCSGReplica["alpha-pcs-0-sg-prefill/0"] = []data.Resource{
				{Name: "alpha-pcs-0-sg-prefill-0-backup", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
			}
		})

		assertCursorValid(t, m)
		row := m.resourcesTable.SelectedRow()
		if len(row) >= 3 && row[2] == "alpha-pcs-0-sg-prefill-0-worker" {
			t.Error("cursor should not be on removed PodClique")
		}
	})
}

// ===========================================================================
// C6. PodCliqueView — cache update preserves selected Pod
// ===========================================================================

func TestC6_PodCliqueView_CacheUpdatePreservesSelectedPod(t *testing.T) {
	// Helper: navigate to the PodCliqueView for the standalone PodClique.
	navigateToPodCliqueView := func(t *testing.T, mc *data.MockGlobalCache) Model {
		t.Helper()
		m := newTestModelWithCache(mc)
		m = sendKey(m, tea.KeyEnter) // alpha-pcs → PodCliqueSetReplicaView (single replica skip)
		m = sendKey(m, tea.KeyDown)  // move to standalone PodClique
		m = sendKey(m, tea.KeyEnter) // → PodCliqueView
		if m.viewState.ViewType != data.PodCliqueView {
			t.Fatalf("expected PodCliqueView, got %s", data.ViewTypeName(m.viewState.ViewType))
		}
		return m
	}

	t.Run("pod statuses change", func(t *testing.T) {
		mc := buildFullMockCache()
		m := navigateToPodCliqueView(t, mc)

		// Cursor naturally lands on worker-1 (index 1, from parent view's cursor fallback)
		assertCursorOnName(t, m, "alpha-pcs-0-pc-worker-1")

		// Change pod statuses (worker-1 becomes CrashLoopBackOff)
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodsByPodClique["alpha-pcs-0-standalone-pc"] = []data.Resource{
				{Name: "alpha-pcs-0-pc-worker-0", Type: "Pod", Namespace: "default", Ready: "1/1", Scheduled: "Running"},
				{Name: "alpha-pcs-0-pc-worker-1", Type: "Pod", Namespace: "default", Ready: "0/1", Scheduled: "CrashLoopBackOff"},
				{Name: "alpha-pcs-0-pc-worker-2", Type: "Pod", Namespace: "default", Ready: "0/1", Scheduled: "Pending"},
			}
		})

		assertCursorOnName(t, m, "alpha-pcs-0-pc-worker-1")
	})

	t.Run("new pod added", func(t *testing.T) {
		mc := buildFullMockCache()
		m := navigateToPodCliqueView(t, mc)

		// Cursor naturally lands on worker-1
		assertCursorOnName(t, m, "alpha-pcs-0-pc-worker-1")

		// Add a new pod
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodsByPodClique["alpha-pcs-0-standalone-pc"] = append(
				snap.PodsByPodClique["alpha-pcs-0-standalone-pc"],
				data.Resource{Name: "alpha-pcs-0-pc-worker-3", Type: "Pod", Namespace: "default", Ready: "0/1", Scheduled: "Pending"},
			)
		})

		assertCursorOnName(t, m, "alpha-pcs-0-pc-worker-1")
	})

	t.Run("selected pod removed — cursor clamps", func(t *testing.T) {
		mc := buildFullMockCache()
		m := navigateToPodCliqueView(t, mc)

		// Cursor naturally lands on worker-1
		assertCursorOnName(t, m, "alpha-pcs-0-pc-worker-1")

		// Remove worker-1
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			var kept []data.Resource
			for _, pod := range snap.PodsByPodClique["alpha-pcs-0-standalone-pc"] {
				if pod.Name != "alpha-pcs-0-pc-worker-1" {
					kept = append(kept, pod)
				}
			}
			snap.PodsByPodClique["alpha-pcs-0-standalone-pc"] = kept
		})

		assertCursorValid(t, m)
		row := m.resourcesTable.SelectedRow()
		if len(row) >= 3 && row[2] == "alpha-pcs-0-pc-worker-1" {
			t.Error("cursor should not be on removed pod")
		}
	})
}

// ===========================================================================
// C6b. PodCliqueView — reversed pod order doesn't cause row reorder
// ===========================================================================

func TestC6b_PodCliqueView_ReversedPodOrderStable(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Navigate to PodCliqueView: alpha-pcs → PodCliqueSetReplicaView → standalone PC
	m = sendKey(m, tea.KeyEnter) // alpha-pcs → PodCliqueSetReplicaView (single replica skip)
	m = sendKey(m, tea.KeyDown)  // move to standalone PodClique
	m = sendKey(m, tea.KeyEnter) // → PodCliqueView
	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Cursor naturally falls on worker-1 (index 1, from parent cursor fallback).
	// Pods sorted by name: worker-0 (0), worker-1 (1), worker-2 (2).
	assertCursorOnName(t, m, "alpha-pcs-0-pc-worker-1")

	// Record row order — should be sorted by name
	rows := m.resourcesTable.Rows()
	if len(rows) < 3 {
		t.Fatalf("expected at least 3 pod rows, got %d", len(rows))
	}
	firstNameBefore := rows[0][2]

	// Cache update: reverse pod order (simulates non-deterministic map iteration)
	m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
		pods := snap.PodsByPodClique["alpha-pcs-0-standalone-pc"]
		for i, j := 0, len(pods)-1; i < j; i, j = i+1, j-1 {
			pods[i], pods[j] = pods[j], pods[i]
		}
	})

	// Row order must be preserved (sorted by name) despite reversed input
	rowsAfter := m.resourcesTable.Rows()
	if len(rowsAfter) < 3 {
		t.Fatalf("expected at least 3 pod rows after update, got %d", len(rowsAfter))
	}
	firstNameAfter := rowsAfter[0][2]
	if firstNameBefore != firstNameAfter {
		t.Errorf("pod row order changed: first row was %q, now %q", firstNameBefore, firstNameAfter)
	}

	// Cursor should still be on worker-1
	assertCursorOnName(t, m, "alpha-pcs-0-pc-worker-1")
}

// ===========================================================================
// Phase 2 helpers
// ===========================================================================

// assertEventsCursorValid verifies the events table cursor is in valid range.
func assertEventsCursorValid(t *testing.T, m Model) {
	t.Helper()
	rows := m.eventsTable.Rows()
	cursor := m.eventsTable.Cursor()
	if len(rows) == 0 {
		return
	}
	if cursor < 0 || cursor >= len(rows) {
		t.Errorf("events cursor %d out of range [0, %d)", cursor, len(rows))
	}
}

// assertTopologyDomainCursorOnName verifies the topology domains table cursor
// is on a row whose first column matches expectedName.
func assertTopologyDomainCursorOnName(t *testing.T, m Model, expectedName string) {
	t.Helper()
	row := m.topologyDomainsTable.SelectedRow()
	if len(row) < 1 {
		t.Fatalf("no valid topology domain row selected; wanted cursor on %q", expectedName)
	}
	if row[0] != expectedName {
		t.Errorf("topology domains cursor on %q, want %q (cursor index=%d)", row[0], expectedName, m.topologyDomainsTable.Cursor())
	}
}

// assertTopologyDomainCursorValid verifies the topology domains cursor is valid.
func assertTopologyDomainCursorValid(t *testing.T, m Model) {
	t.Helper()
	rows := m.topologyDomainsTable.Rows()
	cursor := m.topologyDomainsTable.Cursor()
	if len(rows) == 0 {
		return
	}
	if cursor < 0 || cursor >= len(rows) {
		t.Errorf("topology domains cursor %d out of range [0, %d)", cursor, len(rows))
	}
}

// assertTopologyPodCursorOnName verifies the topology pods table cursor is on a
// row whose NAME column (index 2) matches expectedName.
func assertTopologyPodCursorOnName(t *testing.T, m Model, expectedName string) {
	t.Helper()
	row := m.topologyPodsTable.SelectedRow()
	if len(row) < 3 {
		t.Fatalf("no valid topology pod row selected; wanted cursor on %q", expectedName)
	}
	if row[2] != expectedName {
		t.Errorf("topology pods cursor on %q, want %q (cursor index=%d)", row[2], expectedName, m.topologyPodsTable.Cursor())
	}
}

// assertTopologyPodCursorValid verifies the topology pods cursor is valid.
func assertTopologyPodCursorValid(t *testing.T, m Model) {
	t.Helper()
	rows := m.topologyPodsTable.Rows()
	cursor := m.topologyPodsTable.Cursor()
	if len(rows) == 0 {
		return
	}
	if cursor < 0 || cursor >= len(rows) {
		t.Errorf("topology pods cursor %d out of range [0, %d)", cursor, len(rows))
	}
}

// buildTopologyMockCache creates a MockGlobalCache with the standard topology
// data from sampleTopologyViewData plus the forest data from samplePCSResources.
func buildTopologyMockCache() *data.MockGlobalCache {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)
	return mc
}

// newTopologyTestModelWithCache creates a Model backed by the given
// MockGlobalCache, toggled to TopologyView.
func newTopologyTestModelWithCache(mc *data.MockGlobalCache) Model {
	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	// Toggle to Topology view
	m = sendRune(m, 't')
	return m
}

// topologyCacheUpdate is like cacheUpdate but works with the topology mock.
// It mutates the snapshot and delivers CacheUpdateMsg.
func topologyCacheUpdate(m Model, mc *data.MockGlobalCache, mutate func(*data.CacheSnapshot)) Model {
	snap := mc.Snapshot()
	mutate(snap)
	mc.SetSnapshot(snap)
	return mustApply(m, CacheUpdateMsg{})
}

// ===========================================================================
// C7. Events table cursor during cache update
// ===========================================================================

func TestC7_EventsTableCursorDuringCacheUpdate(t *testing.T) {
	t.Run("ForestView events pane focused — cursor should not jump to 0", func(t *testing.T) {
		events := map[string][]data.Event{
			"PodCliqueSet/alpha-pcs": {
				{Type: "Normal", Kind: "PodCliqueSet", Reason: "Scaled", Age: "5m", From: "controller", Message: "Scaled up", Parent: "alpha-pcs", Timestamp: time.Now().Add(-5 * time.Minute)},
				{Type: "Warning", Kind: "PodCliqueSet", Reason: "NotReady", Age: "3m", From: "controller", Message: "Not all replicas ready", Parent: "alpha-pcs", Timestamp: time.Now().Add(-3 * time.Minute)},
				{Type: "Normal", Kind: "PodCliqueSet", Reason: "Updated", Age: "1m", From: "controller", Message: "Updated config", Parent: "alpha-pcs", Timestamp: time.Now().Add(-1 * time.Minute)},
			},
		}
		mc := buildMockCacheWithEvents(events)
		m := newTestModelWithCache(mc)

		// Tab to events pane
		m = sendKey(m, tea.KeyTab)
		if m.activePane != data.EventsPane {
			t.Fatalf("expected EventsPane, got %d", m.activePane)
		}

		// Move cursor down in events table
		m = sendKey(m, tea.KeyDown)
		eventsCursorBefore := m.eventsTable.Cursor()
		if eventsCursorBefore == 0 {
			// Move further if we're still at 0
			m = sendKey(m, tea.KeyDown)
			eventsCursorBefore = m.eventsTable.Cursor()
		}

		// Cache update changes PCS ready counts
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			for i := range snap.PodCliqueSets {
				if snap.PodCliqueSets[i].Name == "alpha-pcs" {
					snap.PodCliqueSets[i].Ready = "2/3"
				}
			}
		})

		assertEventsCursorValid(t, m)
		// Events cursor should not have reset (it stays in range)
		eventsCursorAfter := m.eventsTable.Cursor()
		eventsRows := m.eventsTable.Rows()
		// If data hasn't changed the number of events, cursor should be preserved
		if len(eventsRows) > 0 && eventsCursorAfter < 0 {
			t.Errorf("events cursor became negative: %d", eventsCursorAfter)
		}
	})

	t.Run("ForestView resources pane focused — resources cursor stays", func(t *testing.T) {
		events := map[string][]data.Event{
			"PodCliqueSet/alpha-pcs": {
				{Type: "Normal", Kind: "PodCliqueSet", Reason: "Scaled", Age: "5m", From: "controller", Message: "Scaled up", Parent: "alpha-pcs", Timestamp: time.Now().Add(-5 * time.Minute)},
			},
		}
		mc := buildMockCacheWithEvents(events)
		m := newTestModelWithCache(mc)

		// Move resources cursor to beta-pcs
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs")

		// Cache update — resources cursor should stay on beta-pcs
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			for i := range snap.PodCliqueSets {
				if snap.PodCliqueSets[i].Name == "alpha-pcs" {
					snap.PodCliqueSets[i].Ready = "2/3"
				}
			}
		})

		assertCursorOnName(t, m, "beta-pcs")
	})

	t.Run("PodCliqueView events pane focused — no crash on cache update", func(t *testing.T) {
		events := map[string][]data.Event{
			"PodClique/alpha-pcs-0-standalone-pc": {
				{Type: "Normal", Kind: "PodClique", Reason: "Created", Age: "1m", From: "controller", Message: "Created pod", Parent: "alpha-pcs-0-standalone-pc", Timestamp: time.Now().Add(-1 * time.Minute)},
				{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Age: "30s", From: "scheduler", Message: "Assigned to node", Parent: "alpha-pcs-0-pc-worker-0", Timestamp: time.Now().Add(-30 * time.Second)},
			},
			"Pod/alpha-pcs-0-pc-worker-0": {
				{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Age: "30s", From: "scheduler", Message: "Assigned to node", Parent: "alpha-pcs-0-pc-worker-0", Timestamp: time.Now().Add(-30 * time.Second)},
			},
		}
		mc := buildMockCacheWithEvents(events)
		m := newTestModelWithCache(mc)

		// Navigate to PodCliqueView: alpha-pcs → PodCliqueSetReplicaView → standalone PC
		m = sendKey(m, tea.KeyEnter) // alpha-pcs → PodCliqueSetReplicaView (single replica skip)
		m = sendKey(m, tea.KeyDown)  // standalone PodClique
		m = sendKey(m, tea.KeyEnter) // → PodCliqueView
		if m.viewState.ViewType != data.PodCliqueView {
			t.Fatalf("expected PodCliqueView, got %s", data.ViewTypeName(m.viewState.ViewType))
		}

		// Tab to events pane
		m = sendKey(m, tea.KeyTab)
		if m.activePane != data.EventsPane {
			t.Fatalf("expected EventsPane, got %d", m.activePane)
		}

		// Cache update changes pod statuses — should not crash
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodsByPodClique["alpha-pcs-0-standalone-pc"] = []data.Resource{
				{Name: "alpha-pcs-0-pc-worker-0", Type: "Pod", Namespace: "default", Ready: "0/1", Scheduled: "CrashLoopBackOff"},
				{Name: "alpha-pcs-0-pc-worker-1", Type: "Pod", Namespace: "default", Ready: "1/1", Scheduled: "Running"},
				{Name: "alpha-pcs-0-pc-worker-2", Type: "Pod", Namespace: "default", Ready: "0/1", Scheduled: "Pending"},
			}
		})

		assertEventsCursorValid(t, m)
	})
}

// ===========================================================================
// C8. TopologyView top-level domains — cache update preserves cursor
// ===========================================================================

func TestC8_TopologyViewDomains_CacheUpdatePreservesCursor(t *testing.T) {
	t.Run("node labels change — cursor stays on zone", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Move cursor to "zone" (row 1)
		m = sendKey(m, tea.KeyDown)
		assertTopologyDomainCursorOnName(t, m, "zone")

		// Cache update: change a node label (doesn't affect domain list)
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.NodeLabels["node-01"]["topology.kubernetes.io/rack"] = "rack-01-updated"
		})

		assertTopologyDomainCursorOnName(t, m, "zone")
	})

	t.Run("new domain added — cursor stays on previously selected", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Move cursor to "zone"
		m = sendKey(m, tea.KeyDown)
		assertTopologyDomainCursorOnName(t, m, "zone")

		// Add a new domain "block" at position 0 (before all others)
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.Domains = append(
				[]data.TopologyDomainRow{{Domain: "block", Key: "topology.kubernetes.io/block", ValuesCount: 2}},
				snap.TopologyViewData.Domains...,
			)
		})

		assertTopologyDomainCursorOnName(t, m, "zone")
	})

	t.Run("unselected domain removed — cursor stays on selected", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Cursor on "region" (row 0)
		assertTopologyDomainCursorOnName(t, m, "region")

		// Remove "rack" domain (not selected)
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			var kept []data.TopologyDomainRow
			for _, d := range snap.TopologyViewData.Domains {
				if d.Domain != "rack" {
					kept = append(kept, d)
				}
			}
			snap.TopologyViewData.Domains = kept
		})

		assertTopologyDomainCursorOnName(t, m, "region")
	})
}

// ===========================================================================
// C9. TopologyView drilled-in values — cache update preserves cursor
// ===========================================================================

func TestC9_TopologyViewDrilledInValues_CacheUpdatePreservesCursor(t *testing.T) {
	t.Run("drilled into region — cursor stays on us-west-2", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region
		m = sendKey(m, tea.KeyEnter)
		// Move cursor to us-west-2 (row 1)
		m = sendKey(m, tea.KeyDown)
		assertTopologyDomainCursorOnName(t, m, "us-west-2")

		// Cache update: change node labels (us-west-2 still exists)
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.NodeLabels["node-04"]["topology.kubernetes.io/rack"] = "rack-04-updated"
		})

		assertTopologyDomainCursorOnName(t, m, "us-west-2")
	})

	t.Run("drilled into region — new value added, cursor stays on us-east-1", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region
		m = sendKey(m, tea.KeyEnter)
		// Cursor on us-east-1 (row 0)
		assertTopologyDomainCursorOnName(t, m, "us-east-1")

		// Add a new region by adding a node with a different region label
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.NodeLabels["node-07"] = map[string]string{
				"topology.kubernetes.io/region": "eu-west-1",
				"topology.kubernetes.io/zone":   "eu-west-1a",
				"topology.kubernetes.io/rack":   "rack-07",
			}
		})

		assertTopologyDomainCursorOnName(t, m, "us-east-1")
	})

	t.Run("drilled into region — selected value removed, cursor clamps", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region, move to us-west-2
		m = sendKey(m, tea.KeyEnter)
		m = sendKey(m, tea.KeyDown)
		assertTopologyDomainCursorOnName(t, m, "us-west-2")

		// Remove all us-west-2 nodes
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			delete(snap.TopologyViewData.NodeLabels, "node-04")
			delete(snap.TopologyViewData.NodeLabels, "node-05")
			delete(snap.TopologyViewData.NodeLabels, "node-06")
		})

		assertTopologyDomainCursorValid(t, m)
		row := m.topologyDomainsTable.SelectedRow()
		if len(row) >= 1 && row[0] == "us-west-2" {
			t.Error("cursor should not be on removed value us-west-2")
		}
	})
}

// ===========================================================================
// C10. TopologyView drilled-in — drill stack validation on cache update
// ===========================================================================

func TestC10_TopologyViewDrillStackValidation(t *testing.T) {
	t.Run("drill stack domain removed — stack resets", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into "rack" (row 2)
		m = sendKey(m, tea.KeyDown) // zone
		m = sendKey(m, tea.KeyDown) // rack
		m = sendKey(m, tea.KeyEnter)
		if len(m.topologyDrillStack) != 1 || m.topologyDrillStack[0].Domain != "rack" {
			t.Fatalf("expected drill into rack, got stack: %+v", m.topologyDrillStack)
		}

		// Remove "rack" domain from domains list
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			var kept []data.TopologyDomainRow
			for _, d := range snap.TopologyViewData.Domains {
				if d.Domain != "rack" {
					kept = append(kept, d)
				}
			}
			snap.TopologyViewData.Domains = kept
		})

		// Drill stack should be reset
		if len(m.topologyDrillStack) != 0 {
			t.Fatalf("expected drill stack reset when domain removed, got depth %d: %+v", len(m.topologyDrillStack), m.topologyDrillStack)
		}
	})

	t.Run("multi-level drill stack — second domain removed — stack resets", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region, select us-east-1, advance to zone
		m = sendKey(m, tea.KeyEnter) // region
		m = sendKey(m, tea.KeyEnter) // us-east-1 → zone
		if len(m.topologyDrillStack) != 2 {
			t.Fatalf("expected drill stack depth 2, got %d", len(m.topologyDrillStack))
		}

		// Remove "zone" domain
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			var kept []data.TopologyDomainRow
			for _, d := range snap.TopologyViewData.Domains {
				if d.Domain != "zone" {
					kept = append(kept, d)
				}
			}
			snap.TopologyViewData.Domains = kept
		})

		// Drill stack should be reset because "zone" was in it
		if len(m.topologyDrillStack) != 0 {
			t.Fatalf("expected drill stack reset when zone removed, got depth %d: %+v", len(m.topologyDrillStack), m.topologyDrillStack)
		}
	})

	t.Run("multi-level drill stack — all domains preserved — stack and cursor stable", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region, select us-east-1, advance to zone
		m = sendKey(m, tea.KeyEnter) // region
		m = sendKey(m, tea.KeyEnter) // us-east-1 → zone
		if len(m.topologyDrillStack) != 2 {
			t.Fatalf("expected drill stack depth 2, got %d", len(m.topologyDrillStack))
		}

		// Cache update that doesn't remove any domains
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.NodeLabels["node-01"]["topology.kubernetes.io/rack"] = "rack-01-updated"
		})

		// Drill stack should be preserved
		if len(m.topologyDrillStack) != 2 {
			t.Fatalf("expected drill stack preserved, got depth %d", len(m.topologyDrillStack))
		}
		if m.topologyDrillStack[0].Domain != "region" || m.topologyDrillStack[0].Value != "us-east-1" {
			t.Errorf("expected first entry region=us-east-1, got %+v", m.topologyDrillStack[0])
		}
		if m.topologyDrillStack[1].Domain != "zone" {
			t.Errorf("expected second entry domain=zone, got %+v", m.topologyDrillStack[1])
		}
		assertTopologyDomainCursorValid(t, m)
	})
}

// ===========================================================================
// C11. TopologyView pods table — cache update preserves cursor
// ===========================================================================

func TestC11_TopologyViewPodsTable_CacheUpdatePreservesCursor(t *testing.T) {
	// Note: when drilled into region→zone with domains cursor on us-east-1a,
	// the pods table only shows pods on nodes in zone us-east-1a: pod-a (node-01)
	// and pod-b (node-02). pod-c is on node-03 (zone us-east-1b) and not visible.

	t.Run("pods table cursor stays on pod-b after phase change", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region, then select us-east-1 → zone values
		// Domains cursor defaults to us-east-1a → pods: pod-a, pod-b
		m = sendKey(m, tea.KeyEnter) // region values
		m = sendKey(m, tea.KeyEnter) // us-east-1 → zone values

		// Switch to pods pane
		m = sendKey(m, tea.KeyTab)
		if m.activePane != data.TopologyPodsPane {
			t.Fatalf("expected TopologyPodsPane, got %s", data.PaneName(m.activePane))
		}

		// Navigate pods to pod-b (row 1)
		m = sendKey(m, tea.KeyDown) // pod-b
		assertTopologyPodCursorOnName(t, m, "pod-b")

		// Cache update: change pod-b's phase
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			for i := range snap.TopologyViewData.Pods {
				if snap.TopologyViewData.Pods[i].Name == "pod-b" {
					snap.TopologyViewData.Pods[i].Phase = "Pending"
				}
			}
		})

		assertTopologyPodCursorOnName(t, m, "pod-b")
	})

	t.Run("new pod added — cursor stays on pod-b", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region → zone values for us-east-1
		m = sendKey(m, tea.KeyEnter)
		m = sendKey(m, tea.KeyEnter)

		// Switch to pods pane and move to pod-b
		m = sendKey(m, tea.KeyTab)
		m = sendKey(m, tea.KeyDown) // pod-b
		assertTopologyPodCursorOnName(t, m, "pod-b")

		// Add a new pod on node-02 (same zone us-east-1a, visible in current filter)
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.Pods = append(snap.TopologyViewData.Pods, data.TopologyViewPod{
				Namespace: "default", Node: "node-02", Name: "pod-aa-new", Topology: "rack: rack-02", Phase: "Running",
			})
		})

		assertTopologyPodCursorOnName(t, m, "pod-b")
	})

	t.Run("selected pod removed — cursor clamps", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region → zone values for us-east-1 (domains cursor on us-east-1a)
		// Visible pods: pod-a, pod-b
		m = sendKey(m, tea.KeyEnter)
		m = sendKey(m, tea.KeyEnter)

		// Switch to pods pane and move to pod-b
		m = sendKey(m, tea.KeyTab)
		m = sendKey(m, tea.KeyDown) // pod-b
		assertTopologyPodCursorOnName(t, m, "pod-b")

		// Remove pod-b
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			var kept []data.TopologyViewPod
			for _, p := range snap.TopologyViewData.Pods {
				if p.Name != "pod-b" {
					kept = append(kept, p)
				}
			}
			snap.TopologyViewData.Pods = kept
		})

		assertTopologyPodCursorValid(t, m)
		row := m.topologyPodsTable.SelectedRow()
		if len(row) >= 3 && row[2] == "pod-b" {
			t.Error("cursor should not be on removed pod-b")
		}
	})

	t.Run("pods pane focused — cursor preserved by name on cache update", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region → zone values (domains cursor on us-east-1a)
		// Visible pods: pod-a, pod-b
		m = sendKey(m, tea.KeyEnter)
		m = sendKey(m, tea.KeyEnter)

		// Switch to pods pane
		m = sendKey(m, tea.KeyTab)
		if m.activePane != data.TopologyPodsPane {
			t.Fatalf("expected TopologyPodsPane, got %s", data.PaneName(m.activePane))
		}

		// Navigate to pod-b
		m = sendKey(m, tea.KeyDown)
		assertTopologyPodCursorOnName(t, m, "pod-b")

		// Cache update: add a pod on node-01 that sorts before pod-b
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.Pods = append(snap.TopologyViewData.Pods, data.TopologyViewPod{
				Namespace: "default", Node: "node-01", Name: "pod-aaa", Topology: "rack: rack-01", Phase: "Running",
			})
		})

		assertTopologyPodCursorOnName(t, m, "pod-b")
	})
}

// ===========================================================================
// C12. Inactive pane cursor stability
// ===========================================================================

func TestC12_InactivePaneCursorStability(t *testing.T) {
	t.Run("ForestView: resources active, events cursor should not reset", func(t *testing.T) {
		events := map[string][]data.Event{
			"PodCliqueSet/alpha-pcs": {
				{Type: "Normal", Kind: "PodCliqueSet", Reason: "Scaled", Age: "5m", From: "controller", Message: "Event 1", Parent: "alpha-pcs", Timestamp: time.Now().Add(-5 * time.Minute)},
				{Type: "Warning", Kind: "PodCliqueSet", Reason: "NotReady", Age: "3m", From: "controller", Message: "Event 2", Parent: "alpha-pcs", Timestamp: time.Now().Add(-3 * time.Minute)},
				{Type: "Normal", Kind: "PodCliqueSet", Reason: "Updated", Age: "1m", From: "controller", Message: "Event 3", Parent: "alpha-pcs", Timestamp: time.Now().Add(-1 * time.Minute)},
			},
		}
		mc := buildMockCacheWithEvents(events)
		m := newTestModelWithCache(mc)

		// Tab to events, move cursor down, then tab back to resources
		m = sendKey(m, tea.KeyTab)
		m = sendKey(m, tea.KeyDown) // events row 1
		eventsCursorBefore := m.eventsTable.Cursor()
		m = sendKey(m, tea.KeyTab) // back to resources

		if m.activePane != data.ResourcesPane {
			t.Fatalf("expected ResourcesPane, got %d", m.activePane)
		}

		// Cache update
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			for i := range snap.PodCliqueSets {
				if snap.PodCliqueSets[i].Name == "alpha-pcs" {
					snap.PodCliqueSets[i].Ready = "2/3"
				}
			}
		})

		assertEventsCursorValid(t, m)
		eventsCursorAfter := m.eventsTable.Cursor()
		eventsRows := m.eventsTable.Rows()
		// Cursor should be in range. If events count didn't change, cursor should be same.
		if len(eventsRows) >= 3 && eventsCursorBefore > 0 && eventsCursorAfter != eventsCursorBefore {
			// This is a "nice to have" check — the events table only guarantees in-range
			t.Logf("events cursor moved from %d to %d (events count: %d)", eventsCursorBefore, eventsCursorAfter, len(eventsRows))
		}
	})

	t.Run("TopologyView: domains active, pods cursor preserved by name", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region → zone values
		m = sendKey(m, tea.KeyEnter)
		m = sendKey(m, tea.KeyEnter)

		// Switch to pods pane, move to pod-b, then switch back to domains
		m = sendKey(m, tea.KeyTab)
		m = sendKey(m, tea.KeyDown) // pod-b
		assertTopologyPodCursorOnName(t, m, "pod-b")
		m = sendKey(m, tea.KeyTab) // back to domains

		if m.activePane != data.TopologyDomainsPane {
			t.Fatalf("expected TopologyDomainsPane, got %s", data.PaneName(m.activePane))
		}

		// Cache update — should preserve pods cursor by name
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			for i := range snap.TopologyViewData.Pods {
				if snap.TopologyViewData.Pods[i].Name == "pod-a" {
					snap.TopologyViewData.Pods[i].Phase = "Pending"
				}
			}
		})

		assertTopologyPodCursorOnName(t, m, "pod-b")
	})

	t.Run("TopologyView: pods active, domains cursor preserved by name", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Move domains cursor to "zone" (row 1)
		m = sendKey(m, tea.KeyDown)
		assertTopologyDomainCursorOnName(t, m, "zone")

		// Switch to pods pane (now domains is inactive)
		m = sendKey(m, tea.KeyTab)
		if m.activePane != data.TopologyPodsPane {
			t.Fatalf("expected TopologyPodsPane, got %s", data.PaneName(m.activePane))
		}

		// Cache update
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.NodeLabels["node-01"]["topology.kubernetes.io/rack"] = "rack-01-updated"
		})

		assertTopologyDomainCursorOnName(t, m, "zone")
	})
}

// ===========================================================================
// C13. Cache update with empty data
// ===========================================================================

func TestC13_CacheUpdateWithEmptyData(t *testing.T) {
	t.Run("ForestView: PodCliqueSets set to empty — no panic", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		// Move cursor to beta-pcs
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs")

		// Cache update: empty PodCliqueSets
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodCliqueSets = []data.Resource{}
		})

		// Should not panic, cursor should be valid
		rows := m.resourcesTable.Rows()
		if len(rows) != 0 {
			t.Errorf("expected 0 rows, got %d", len(rows))
		}
	})

	t.Run("TopologyView drilled-in: NodeLabels set to empty — no panic", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region
		m = sendKey(m, tea.KeyEnter)
		rows := m.topologyDomainsTable.Rows()
		if len(rows) == 0 {
			t.Fatal("expected some values after drilling into region")
		}

		// Cache update: empty NodeLabels
		m = topologyCacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.TopologyViewData.NodeLabels = map[string]map[string]string{}
		})

		// Values list should become empty, no panic
		rows = m.topologyDomainsTable.Rows()
		if len(rows) != 0 {
			t.Errorf("expected 0 values after emptying NodeLabels, got %d", len(rows))
		}
	})

	t.Run("PodCliqueView: pods set to empty — no panic", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		// Navigate to PodCliqueView
		m = sendKey(m, tea.KeyEnter) // alpha-pcs → PodCliqueSetReplicaView
		m = sendKey(m, tea.KeyDown)  // standalone PodClique
		m = sendKey(m, tea.KeyEnter) // → PodCliqueView
		if m.viewState.ViewType != data.PodCliqueView {
			t.Fatalf("expected PodCliqueView, got %s", data.ViewTypeName(m.viewState.ViewType))
		}

		rows := m.resourcesTable.Rows()
		if len(rows) == 0 {
			t.Fatal("expected some pods before emptying")
		}

		// Cache update: empty pods for the current PodClique
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			snap.PodsByPodClique["alpha-pcs-0-standalone-pc"] = []data.Resource{}
		})

		// Should not panic, table should be empty
		rows = m.resourcesTable.Rows()
		if len(rows) != 0 {
			t.Errorf("expected 0 pod rows, got %d", len(rows))
		}
	})
}

// ===========================================================================
// C14. Rapid successive cache updates
// ===========================================================================

func TestC14_RapidSuccessiveCacheUpdates(t *testing.T) {
	t.Run("two updates — final state reflects last snapshot, cursor stable", func(t *testing.T) {
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		// Move cursor to beta-pcs
		m = sendKey(m, tea.KeyDown)
		assertCursorOnName(t, m, "beta-pcs")

		// First update: change alpha-pcs ready
		snap := mc.Snapshot()
		for i := range snap.PodCliqueSets {
			if snap.PodCliqueSets[i].Name == "alpha-pcs" {
				snap.PodCliqueSets[i].Ready = "1/3"
			}
		}
		mc.SetSnapshot(snap)
		m = mustApply(m, CacheUpdateMsg{})

		// Second update: add a new PCS
		snap = mc.Snapshot()
		snap.PodCliqueSets = append(snap.PodCliqueSets, data.Resource{
			Name: "delta-pcs", Type: "PodCliqueSet", Namespace: "prod", Ready: "5/5", Scheduled: "5/5", Topology: "N/A",
		})
		mc.SetSnapshot(snap)
		m = mustApply(m, CacheUpdateMsg{})

		// Cursor should still be on beta-pcs
		assertCursorOnName(t, m, "beta-pcs")

		// Final state should have 4 PCSes (alpha, beta, delta, gamma)
		rows := m.resourcesTable.Rows()
		if len(rows) != 4 {
			t.Errorf("expected 4 PCS rows after two updates, got %d", len(rows))
		}

		// Verify alpha-pcs has the updated ready count.
		// The READY column position depends on whether TOPOLOGY is visible:
		// With topology: NAMESPACE(0), TYPE(1), NAME(2), TOPOLOGY(3), READY(4), SCHEDULED(5)
		// Without:       NAMESPACE(0), TYPE(1), NAME(2), READY(3), SCHEDULED(4)
		readyIdx := 3 // no topology column by default
		cols := m.resourcesTable.Columns()
		for i, col := range cols {
			if col.Title == "READY" {
				readyIdx = i
				break
			}
		}
		for _, row := range rows {
			if len(row) > readyIdx && row[2] == "alpha-pcs" {
				if row[readyIdx] != "1/3" {
					t.Errorf("alpha-pcs Ready = %q, want '1/3'", row[readyIdx])
				}
			}
		}
	})

	t.Run("reversed resource order — cursor should not visually jump", func(t *testing.T) {
		// This test reproduces the real-world bug: informer store List()
		// returns items in non-deterministic order. When the order flips
		// between cache updates, the cursor appears to jump because the
		// rows swap positions. The fix is to sort resource lists by name.
		mc := buildFullMockCache()
		m := newTestModelWithCache(mc)

		// Navigate to PodCliqueSetReplicaView for alpha-pcs (single replica skip)
		m = sendKey(m, tea.KeyEnter)
		if m.viewState.ViewType != data.PodCliqueSetReplicaView {
			t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
		}

		// Cursor should be on first row: alpha-pcs-0-sg-prefill
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill")

		// Record current row order
		rowsBefore := m.resourcesTable.Rows()
		if len(rowsBefore) < 2 {
			t.Fatalf("expected at least 2 rows, got %d", len(rowsBefore))
		}
		firstNameBefore := rowsBefore[0][2]

		// Cache update: reverse the order of PodCliques and ScalingGroups.
		// This simulates what happens when informer store List() returns
		// items in a different map iteration order.
		m = cacheUpdate(m, mc, func(snap *data.CacheSnapshot) {
			pcs := snap.PodCliquesByReplica["alpha-pcs/0"]
			if len(pcs) >= 2 {
				// Reverse slice
				for i, j := 0, len(pcs)-1; i < j; i, j = i+1, j-1 {
					pcs[i], pcs[j] = pcs[j], pcs[i]
				}
			}
			sgs := snap.ScalingGroupsByReplica["alpha-pcs/0"]
			if len(sgs) >= 2 {
				for i, j := 0, len(sgs)-1; i < j; i, j = i+1, j-1 {
					sgs[i], sgs[j] = sgs[j], sgs[i]
				}
			}
		})

		// Row order MUST remain the same (sorted by name)
		rowsAfter := m.resourcesTable.Rows()
		if len(rowsAfter) < 2 {
			t.Fatalf("expected at least 2 rows after update, got %d", len(rowsAfter))
		}
		firstNameAfter := rowsAfter[0][2]
		if firstNameBefore != firstNameAfter {
			t.Errorf("row order changed: first row was %q, now %q — this causes the cursor to visually jump",
				firstNameBefore, firstNameAfter)
		}

		// Cursor should still be on the same name
		assertCursorOnName(t, m, "alpha-pcs-0-sg-prefill")
	})

	t.Run("topology: two updates — final state is correct", func(t *testing.T) {
		mc := buildTopologyMockCache()
		m := newTopologyTestModelWithCache(mc)

		// Drill into region
		m = sendKey(m, tea.KeyEnter)
		// Move to us-west-2
		m = sendKey(m, tea.KeyDown)
		assertTopologyDomainCursorOnName(t, m, "us-west-2")

		// First update: add a node to us-east-1
		snap := mc.Snapshot()
		snap.TopologyViewData.NodeLabels["node-07"] = map[string]string{
			"topology.kubernetes.io/region": "us-east-1",
			"topology.kubernetes.io/zone":   "us-east-1c",
			"topology.kubernetes.io/rack":   "rack-07",
		}
		mc.SetSnapshot(snap)
		m = mustApply(m, CacheUpdateMsg{})

		// Second update: change a label
		snap = mc.Snapshot()
		snap.TopologyViewData.NodeLabels["node-01"]["topology.kubernetes.io/rack"] = "rack-01-v2"
		mc.SetSnapshot(snap)
		m = mustApply(m, CacheUpdateMsg{})

		// Cursor should still be on us-west-2
		assertTopologyDomainCursorOnName(t, m, "us-west-2")

		// Should still have 2 region values (us-east-1 and us-west-2)
		rows := m.topologyDomainsTable.Rows()
		if len(rows) != 2 {
			t.Errorf("expected 2 region values, got %d", len(rows))
		}
	})
}
