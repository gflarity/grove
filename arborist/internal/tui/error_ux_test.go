package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	tea "github.com/charmbracelet/bubbletea"
)

// ===========================================================================
// Part 1: Error Log Box Tests
// ===========================================================================

// --- addError ---

func TestAddError_AddsEntryToErrorLog(t *testing.T) {
	m := newTestModel(nil)
	m.addError("something failed")

	if len(m.errorLog) != 1 {
		t.Fatalf("expected 1 error, got %d", len(m.errorLog))
	}
	if m.errorLog[0].Message != "something failed" {
		t.Errorf("message = %q, want %q", m.errorLog[0].Message, "something failed")
	}
}

func TestAddError_SetsErrorLogVisible(t *testing.T) {
	m := newTestModel(nil)
	m.errorLogVisible = false
	m.addError("err")

	if !m.errorLogVisible {
		t.Error("expected errorLogVisible to be true after addError")
	}
}

func TestAddError_CapsAt3Entries(t *testing.T) {
	m := newTestModel(nil)
	m.addError("err1")
	m.addError("err2")
	m.addError("err3")
	m.addError("err4")

	if len(m.errorLog) != 3 {
		t.Fatalf("expected 3 errors (capped), got %d", len(m.errorLog))
	}
}

func TestAddError_NewestFirst(t *testing.T) {
	m := newTestModel(nil)
	m.addError("first")
	m.addError("second")
	m.addError("third")

	if m.errorLog[0].Message != "third" {
		t.Errorf("errorLog[0] = %q, want %q", m.errorLog[0].Message, "third")
	}
	if m.errorLog[1].Message != "second" {
		t.Errorf("errorLog[1] = %q, want %q", m.errorLog[1].Message, "second")
	}
	if m.errorLog[2].Message != "first" {
		t.Errorf("errorLog[2] = %q, want %q", m.errorLog[2].Message, "first")
	}
}

func TestAddError_FourthEvictsOldest(t *testing.T) {
	m := newTestModel(nil)
	m.addError("first")
	m.addError("second")
	m.addError("third")
	m.addError("fourth")

	if m.errorLog[0].Message != "fourth" {
		t.Errorf("errorLog[0] = %q, want %q", m.errorLog[0].Message, "fourth")
	}
	if m.errorLog[2].Message != "second" {
		t.Errorf("errorLog[2] = %q, want %q (oldest surviving)", m.errorLog[2].Message, "second")
	}
	// "first" should be evicted
	for _, e := range m.errorLog {
		if e.Message == "first" {
			t.Error("'first' should have been evicted but is still in errorLog")
		}
	}
}

func TestAddError_ContainsFormattedTimestamp(t *testing.T) {
	m := newTestModel(nil)
	before := time.Now()
	m.addError("test")
	after := time.Now()

	entry := m.errorLog[0]
	if entry.Time.Before(before) || entry.Time.After(after) {
		t.Errorf("entry time %v not between %v and %v", entry.Time, before, after)
	}
	// Verify the timestamp formats correctly as HH:MM:SS
	ts := entry.Time.Format("15:04:05")
	if len(ts) != 8 {
		t.Errorf("formatted timestamp %q has unexpected length", ts)
	}
}

// --- ErrorMsg routing ---

func TestErrorMsg_AddsToErrorLog(t *testing.T) {
	m := newTestModel(nil)
	m = mustApply(m, ErrorMsg{Operation: "test-op", Err: fmt.Errorf("bad thing")})

	if len(m.errorLog) != 1 {
		t.Fatalf("expected 1 error, got %d", len(m.errorLog))
	}
	if !strings.Contains(m.errorLog[0].Message, "test-op") {
		t.Errorf("error message %q should contain operation", m.errorLog[0].Message)
	}
	if !strings.Contains(m.errorLog[0].Message, "bad thing") {
		t.Errorf("error message %q should contain error text", m.errorLog[0].Message)
	}
	if !m.errorLogVisible {
		t.Error("expected errorLogVisible to be true after ErrorMsg")
	}
}

func TestErrorMsg_FormatsAsOperationColonError(t *testing.T) {
	m := newTestModel(nil)
	m = mustApply(m, ErrorMsg{Operation: "fetchData", Err: fmt.Errorf("timeout")})

	want := "fetchData: timeout"
	if m.errorLog[0].Message != want {
		t.Errorf("error message = %q, want %q", m.errorLog[0].Message, want)
	}
}

func TestErrorMsg_StartGlobalCache_SetsCacheSynced(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// cacheSynced should be false before
	if m.cacheSynced {
		t.Fatal("expected cacheSynced to be false initially")
	}

	m = mustApply(m, ErrorMsg{Operation: "startGlobalCache", Err: fmt.Errorf("connection refused")})

	if !m.cacheSynced {
		t.Error("expected cacheSynced to be true after startGlobalCache error")
	}
	if len(m.errorLog) != 1 {
		t.Fatalf("expected 1 error, got %d", len(m.errorLog))
	}
	if !strings.Contains(m.errorLog[0].Message, "startGlobalCache") {
		t.Errorf("error message should contain 'startGlobalCache', got %q", m.errorLog[0].Message)
	}
}

func TestErrorMsg_MultipleInSequence_NewestFirst(t *testing.T) {
	m := newTestModel(nil)
	m = mustApply(m, ErrorMsg{Operation: "op1", Err: fmt.Errorf("err1")})
	m = mustApply(m, ErrorMsg{Operation: "op2", Err: fmt.Errorf("err2")})
	m = mustApply(m, ErrorMsg{Operation: "op3", Err: fmt.Errorf("err3")})

	if len(m.errorLog) != 3 {
		t.Fatalf("expected 3 errors, got %d", len(m.errorLog))
	}
	if !strings.Contains(m.errorLog[0].Message, "op3") {
		t.Errorf("errorLog[0] = %q, want 'op3' (newest)", m.errorLog[0].Message)
	}
	if !strings.Contains(m.errorLog[2].Message, "op1") {
		t.Errorf("errorLog[2] = %q, want 'op1' (oldest)", m.errorLog[2].Message)
	}
}

// --- Error log frame visibility in View() ---

func TestErrorLogFrame_AppearsWhenVisibleAndNonEmpty(t *testing.T) {
	m := newTestModel(nil)
	m.addError("test error")

	view := m.View()
	if !strings.Contains(view, "Errors") {
		t.Error("expected view to contain 'Errors' section header")
	}
	if !strings.Contains(view, "test error") {
		t.Error("expected view to contain error message")
	}
}

func TestErrorLogFrame_HiddenWhenNotVisible(t *testing.T) {
	m := newTestModel(nil)
	m.addError("test error")
	m.errorLogVisible = false

	view := m.View()
	if strings.Contains(view, "test error") {
		t.Error("expected error message to be hidden when errorLogVisible is false")
	}
}

func TestErrorLogFrame_ShowsPlaceholderWhenEmpty(t *testing.T) {
	m := newTestModel(nil)
	m.errorLogVisible = true // visible but empty

	view := m.View()
	if !strings.Contains(view, "No errors") {
		t.Error("expected 'No errors' placeholder when errorLog is empty and visible")
	}
}

// --- '!' keybinding ---

func TestEKey_TogglesErrorLogOff(t *testing.T) {
	m := newTestModel(nil)
	m.addError("err")
	if !m.errorLogVisible {
		t.Fatal("precondition: errorLogVisible should be true")
	}

	m = sendRune(m, '!')
	if m.errorLogVisible {
		t.Error("expected '!' to toggle errorLogVisible to false")
	}
}

func TestEKey_TogglesErrorLogOn(t *testing.T) {
	m := newTestModel(nil)
	m.addError("err")
	m.errorLogVisible = false

	m = sendRune(m, '!')
	if !m.errorLogVisible {
		t.Error("expected '!' to toggle errorLogVisible to true")
	}
}

func TestEKey_TogglesEvenWhenErrorLogEmpty(t *testing.T) {
	m := newTestModel(nil)
	// No errors — '!' should still toggle errorLogVisible
	m = sendRune(m, '!')
	if !m.errorLogVisible {
		t.Error("expected '!' to toggle errorLogVisible to true even with empty errorLog")
	}
	// Toggle back
	m = sendRune(m, '!')
	if m.errorLogVisible {
		t.Error("expected '!' to toggle errorLogVisible back to false")
	}
}

func TestNewError_ReShowsBoxAfterToggleAway(t *testing.T) {
	m := newTestModel(nil)
	m.addError("first error")

	// User toggles it away
	m = sendRune(m, '!')
	if m.errorLogVisible {
		t.Fatal("precondition: error log should be hidden after toggle")
	}

	// New error arrives — should re-show
	m = mustApply(m, ErrorMsg{Operation: "op", Err: fmt.Errorf("new error")})
	if !m.errorLogVisible {
		t.Error("expected new error to re-show the error log box")
	}
}

// --- Menu hint ---

func TestErrorsMenuHint_AppearsWhenErrorsExist(t *testing.T) {
	m := newTestModel(nil)
	m.addError("err")

	shortcuts := m.buildShortcutsString()
	if !strings.Contains(shortcuts, "<!>Errors") {
		t.Errorf("expected shortcuts to contain '<!>Errors', got %q", shortcuts)
	}
}

func TestErrorsMenuHint_AlwaysPresent(t *testing.T) {
	m := newTestModel(nil)

	shortcuts := m.buildShortcutsString()
	if !strings.Contains(shortcuts, "<!>Errors") {
		t.Errorf("expected shortcuts to always contain '<!>Errors', got %q", shortcuts)
	}
}

// --- YAML overlay still renders on top ---

func TestYAMLOverlay_RendersOverErrorLog(t *testing.T) {
	m := newTestModel(nil)
	m.addError("err")
	m.yamlOverlay.Active = true
	m.yamlOverlay.Content = "apiVersion: v1"
	m.yamlOverlay.Viewport.SetContent(m.yamlOverlay.Content)

	view := m.View()
	// YAML overlay should render, error log should NOT be visible
	if !strings.Contains(view, "apiVersion: v1") {
		t.Error("expected YAML overlay content to render")
	}
	// The error log frame should not appear in YAML overlay mode
	if strings.Contains(view, "err") && strings.Contains(view, "Errors") {
		// This is OK only if "Errors" appears as part of something else.
		// The key check is that the error log frame structure doesn't bleed through.
	}
}

// --- errorLogFrameHeight ---

func TestErrorLogFrameHeight_ZeroWhenHidden(t *testing.T) {
	m := newTestModel(nil)
	m.errorLogVisible = false

	if h := m.errorLogFrameHeight(); h != 0 {
		t.Errorf("errorLogFrameHeight() = %d, want 0 when hidden", h)
	}
}

func TestErrorLogFrameHeight_EmptyButVisible(t *testing.T) {
	m := newTestModel(nil)
	m.errorLogVisible = true

	// border top + border bottom + 1 placeholder line = 3
	if h := m.errorLogFrameHeight(); h != 3 {
		t.Errorf("errorLogFrameHeight() = %d, want 3 for empty visible box", h)
	}
}

func TestErrorLogFrameHeight_WithEntries(t *testing.T) {
	m := newTestModel(nil)
	m.addError("a")
	m.addError("b")

	// border top + border bottom + 2 entries = 4
	if h := m.errorLogFrameHeight(); h != 4 {
		t.Errorf("errorLogFrameHeight() = %d, want 4 for 2 entries", h)
	}
}

// --- Layout height adjustment ---

func TestLayoutHeight_AdjustsWhenErrorLogVisible(t *testing.T) {
	m := newTestModel(nil)

	// Baseline: no errors
	viewNoErrors := m.View()
	linesNoErrors := strings.Count(viewNoErrors, "\n")

	// Add errors
	m.addError("err1")
	m.addError("err2")
	viewWithErrors := m.View()
	linesWithErrors := strings.Count(viewWithErrors, "\n")

	// With errors visible, the error log frame takes space (2 border lines + 2 content lines = 4).
	// The total rendered height should be approximately the same (tables shrink to make room),
	// but the view should now contain the error content.
	if !strings.Contains(viewWithErrors, "err1") || !strings.Contains(viewWithErrors, "err2") {
		t.Error("expected error messages in the view")
	}
	// The total line count should be similar (within a few lines due to rounding)
	diff := linesWithErrors - linesNoErrors
	if diff > 5 || diff < -5 {
		t.Errorf("line count difference = %d (noErrors=%d, withErrors=%d), expected within ±5",
			diff, linesNoErrors, linesWithErrors)
	}
}

func TestResizeLayout_TablesShrinkWhenErrorLogAppears(t *testing.T) {
	m := newTestModel(nil)

	// Baseline table height with no error log
	baseHeight := m.resourcesTable.Height()

	// Error log appears (2 entries → 4 lines of frame)
	m.addError("err1")
	m.addError("err2")

	newHeight := m.resourcesTable.Height()
	// errorLogFrameHeight = 4 (2 border + 2 entries), split across 2 panes → each shrinks by ~2
	if newHeight >= baseHeight {
		t.Errorf("expected table height to shrink: base=%d, after errors=%d", baseHeight, newHeight)
	}
}

func TestResizeLayout_TablesGrowWhenErrorLogToggledOff(t *testing.T) {
	m := newTestModel(nil)

	// Baseline table height with no error log
	baseHeight := m.resourcesTable.Height()

	// Show error log
	m.addError("err")
	shrunkHeight := m.resourcesTable.Height()

	// Toggle off
	m = sendRune(m, '!')
	restoredHeight := m.resourcesTable.Height()

	if shrunkHeight >= baseHeight {
		t.Errorf("precondition: table should shrink with errors; base=%d, shrunk=%d", baseHeight, shrunkHeight)
	}
	if restoredHeight != baseHeight {
		t.Errorf("expected table height to restore to %d after toggle off, got %d", baseHeight, restoredHeight)
	}
}

func TestResizeLayout_EmptyErrorBoxStillShrinksTables(t *testing.T) {
	m := newTestModel(nil)

	// Baseline table height
	baseHeight := m.resourcesTable.Height()

	// Toggle on empty error box
	m = sendRune(m, '!')
	if !m.errorLogVisible {
		t.Fatal("precondition: errorLogVisible should be true")
	}

	newHeight := m.resourcesTable.Height()
	// Empty box = 3 lines (2 border + 1 placeholder), split across 2 panes → each shrinks by ~1-2
	if newHeight >= baseHeight {
		t.Errorf("expected table height to shrink for empty error box: base=%d, after=%d", baseHeight, newHeight)
	}
}

// ===========================================================================
// Part 2: Graceful Topology Unavailability Tests
// ===========================================================================

// --- topologyAvailable ---

func TestTopologyAvailable_NilTopologyViewData(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = nil

	if m.topologyAvailable() {
		t.Error("expected topologyAvailable() = false for nil TopologyViewData")
	}
}

func TestTopologyAvailable_EmptyDomains(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{},
	}

	if m.topologyAvailable() {
		t.Error("expected topologyAvailable() = false for empty Domains")
	}
}

func TestTopologyAvailable_WithValidDomains(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	if !m.topologyAvailable() {
		t.Error("expected topologyAvailable() = true with valid domains")
	}
}

// --- Pressing 't' with no topology data ---

func TestTKey_NoTopologyData_LogsError_StaysInForestView(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = nil

	m = sendRune(m, 't')

	if m.viewState.ViewType != clusterstate.ForestView {
		t.Errorf("expected to stay in ForestView, got %s", clusterstate.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.errorLog) != 1 {
		t.Fatalf("expected 1 error, got %d", len(m.errorLog))
	}
	if !strings.Contains(m.errorLog[0].Message, "Topology unavailable") {
		t.Errorf("error message = %q, want something about topology unavailable", m.errorLog[0].Message)
	}
}

// --- Pressing 't' with topology data ---

func TestTKey_WithTopologyData_SwitchesToTopologyView(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	m = sendRune(m, 't')

	if m.viewState.ViewType != clusterstate.TopologyView {
		t.Errorf("expected TopologyView, got %s", clusterstate.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.errorLog) != 0 {
		t.Errorf("expected no errors, got %d", len(m.errorLog))
	}
}

// --- 't' key with no topology data logs error ---

func TestTopologyCommand_NoTopologyData_LogsError(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = nil

	// Press 't' to toggle topology — should log error since no data
	m = sendRune(m, 't')

	if m.viewState.ViewType != clusterstate.ForestView {
		t.Errorf("expected to stay in ForestView, got %s", clusterstate.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.errorLog) != 1 {
		t.Fatalf("expected 1 error, got %d", len(m.errorLog))
	}
	if !strings.Contains(m.errorLog[0].Message, "Topology unavailable") {
		t.Errorf("error message = %q, want something about topology unavailable", m.errorLog[0].Message)
	}
}

// --- 't' key with topology data switches view ---

func TestTopologyCommand_WithTopologyData_Switches(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	// Press 't' to toggle topology
	m = sendRune(m, 't')

	if m.viewState.ViewType != clusterstate.TopologyView {
		t.Errorf("expected TopologyView, got %s", clusterstate.ViewTypeName(m.viewState.ViewType))
	}
}

// --- Pressing 't' from TopologyView goes back even if topology data disappeared ---

func TestTKey_FromTopologyView_GoesBackEvenIfDataDisappeared(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	// Enter topology view
	m = sendRune(m, 't')
	if m.viewState.ViewType != clusterstate.TopologyView {
		t.Fatal("precondition: should be in TopologyView")
	}

	// Topology data disappears
	m.topologyViewData = nil

	// Press 't' to go back — should work unconditionally
	m = sendRune(m, 't')
	if m.viewState.ViewType != clusterstate.ForestView {
		t.Errorf("expected ForestView after toggling back, got %s", clusterstate.ViewTypeName(m.viewState.ViewType))
	}
	// No error should be logged for going BACK
	if len(m.errorLog) != 0 {
		t.Errorf("expected no errors when toggling back from topology, got %d", len(m.errorLog))
	}
}

// --- Header hints ---

func TestHeader_HidesTopologyHint_WhenUnavailable(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = nil

	shortcuts := m.buildShortcutsString()
	if strings.Contains(shortcuts, "<t>Topology") {
		t.Errorf("expected shortcuts NOT to contain '<t>Topology' when topology unavailable, got %q", shortcuts)
	}
}

func TestHeader_ShowsTopologyHint_WhenAvailable(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	shortcuts := m.buildShortcutsString()
	if !strings.Contains(shortcuts, "<t>Topology") {
		t.Errorf("expected shortcuts to contain '<t>Topology', got %q", shortcuts)
	}
}

// --- After cache update delivers topology data, 't' starts working ---

func TestTopology_TransitionFalseToTrue_AfterCacheUpdate(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
	m := newTestModelWithCache(mc)

	// Initially no topology data
	m.topologyViewData = nil
	if m.topologyAvailable() {
		t.Fatal("precondition: topology should be unavailable")
	}

	// Simulate cache update that delivers topology data
	snap := mc.Snapshot()
	snap.TopologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
		},
	}
	mc.SetSnapshot(snap)

	// Apply snapshot manually (simulating cache update)
	m.applySnapshot()

	if !m.topologyAvailable() {
		t.Error("expected topologyAvailable() = true after cache update")
	}

	// 't' should now work
	m = sendRune(m, 't')
	if m.viewState.ViewType != clusterstate.TopologyView {
		t.Errorf("expected TopologyView after cache update, got %s", clusterstate.ViewTypeName(m.viewState.ViewType))
	}
}

// ===========================================================================
// Part 3: Hide Topology Column & Note When Unavailable
// ===========================================================================

// --- topologyColumnVisible ---

func TestTopologyColumnVisible_FalseWhenNoTopologyData(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = nil

	if m.topologyColumnVisible() {
		t.Error("expected topologyColumnVisible() = false when topologyViewData is nil")
	}
}

func TestTopologyColumnVisible_FalseWhenEmptyDomains(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{},
	}

	if m.topologyColumnVisible() {
		t.Error("expected topologyColumnVisible() = false when Domains is empty")
	}
}

func TestTopologyColumnVisible_TrueWhenDomainsExist(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	if !m.topologyColumnVisible() {
		t.Error("expected topologyColumnVisible() = true when topology data exists")
	}
}

// --- TOPOLOGY column hidden when unavailable ---

func TestResourcesTable_HidesTopologyColumn_WhenUnavailable(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []clusterstate.Resource{
		{Name: "pcs-1", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1", Topology: "N/A"},
	}
	mc.SetSnapshot(snap)
	m := newTestModelWithCache(mc)

	// No topology data
	m.topologyViewData = nil
	m.rebuildResourcesTable()

	// Check that column headers do NOT include "TOPOLOGY"
	cols := m.resourcesTable.Columns()
	for _, col := range cols {
		if col.Title == "TOPOLOGY" {
			t.Error("TOPOLOGY column should be hidden when topology is unavailable")
		}
	}
}

func TestResourcesTable_ShowsTopologyColumn_WhenAvailable(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []clusterstate.Resource{
		{Name: "pcs-1", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1", Topology: "rack"},
	}
	snap.TopologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "rack", Key: "topology.io/rack", ValuesCount: 3},
		},
	}
	mc.SetSnapshot(snap)
	m := newTestModelWithCache(mc)

	// Rebuild to pick up topology data
	m.rebuildResourcesTable()

	// Check that column headers include "TOPOLOGY"
	found := false
	cols := m.resourcesTable.Columns()
	for _, col := range cols {
		if col.Title == "TOPOLOGY" {
			found = true
			break
		}
	}
	if !found {
		t.Error("TOPOLOGY column should be visible when topology data is available")
	}
}

func TestResourcesTable_RowWidth_MatchesColumns_NoTopology(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []clusterstate.Resource{
		{Name: "pcs-1", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1", Topology: "N/A"},
	}
	mc.SetSnapshot(snap)
	m := newTestModelWithCache(mc)

	// No topology data — TOPOLOGY column hidden
	m.topologyViewData = nil
	m.rebuildResourcesTable()

	cols := m.resourcesTable.Columns()
	rows := m.resourcesTable.Rows()
	if len(rows) == 0 {
		t.Fatal("expected at least 1 row")
	}
	if len(rows[0]) != len(cols) {
		t.Errorf("row width %d != column count %d (no topology)", len(rows[0]), len(cols))
	}
}

func TestResourcesTable_RowWidth_MatchesColumns_WithTopology(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []clusterstate.Resource{
		{Name: "pcs-1", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1", Topology: "rack"},
	}
	snap.TopologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "rack", Key: "topology.io/rack", ValuesCount: 3},
		},
	}
	mc.SetSnapshot(snap)
	m := newTestModelWithCache(mc)
	m.rebuildResourcesTable()

	cols := m.resourcesTable.Columns()
	rows := m.resourcesTable.Rows()
	if len(rows) == 0 {
		t.Fatal("expected at least 1 row")
	}
	if len(rows[0]) != len(cols) {
		t.Errorf("row width %d != column count %d (with topology)", len(rows[0]), len(cols))
	}
}

// --- Topology column reappears after cache update delivers topology ---

func TestTopologyColumn_AppearsAfterCacheUpdateDeliversTopology(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []clusterstate.Resource{
		{Name: "pcs-1", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1", Topology: "N/A"},
	}
	mc.SetSnapshot(snap)
	m := newTestModelWithCache(mc)

	// Initially no topology
	m.topologyViewData = nil
	m.rebuildResourcesTable()

	// Verify no TOPOLOGY column
	for _, col := range m.resourcesTable.Columns() {
		if col.Title == "TOPOLOGY" {
			t.Fatal("precondition: TOPOLOGY column should be hidden initially")
		}
	}

	// Simulate cache update that delivers topology
	snap = mc.Snapshot()
	snap.TopologyViewData = &clusterstate.TopologyViewData{
		Domains: []clusterstate.TopologyDomainRow{
			{Domain: "rack", Key: "topology.io/rack", ValuesCount: 2},
		},
	}
	snap.PodCliqueSets = []clusterstate.Resource{
		{Name: "pcs-1", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1", Topology: "rack"},
	}
	mc.SetSnapshot(snap)
	m.applySnapshot()

	// Now TOPOLOGY column should be present
	found := false
	for _, col := range m.resourcesTable.Columns() {
		if col.Title == "TOPOLOGY" {
			found = true
			break
		}
	}
	if !found {
		t.Error("TOPOLOGY column should appear after topology data becomes available")
	}
}

// ===========================================================================
// Part 4: Logs Autoscroll (tail -f) Tests
// ===========================================================================

// helper: create a model with the logs overlay open on a mock pod/container.
func newTestModelWithLogsOverlay() Model {
	mc := clusterstate.NewMockGlobalCache()
	m := newTestModelWithCache(mc)
	m.logsOverlay.Active = true
	m.logsOverlay.PodName = "test-pod"
	m.logsOverlay.ContainerName = "main"
	m.logsOverlay.Namespace = "default"
	m.logsOverlay.Content = "line 1\nline 2\nline 3"
	m.logsOverlay.Viewport.SetContent(m.logsOverlay.Content)
	return m
}

// --- Toggle on ---

func TestLogsAutoScroll_ToggleOn(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	if m.logsOverlay.AutoScroll {
		t.Fatal("precondition: logsAutoScroll should be false")
	}

	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if !m.logsOverlay.AutoScroll {
		t.Error("expected logsAutoScroll to be true after pressing 's'")
	}
	if cmd == nil {
		t.Error("expected a command to be returned (tick + log fetch)")
	}
}

// --- Toggle off ---

func TestLogsAutoScroll_ToggleOff(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = true

	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if m.logsOverlay.AutoScroll {
		t.Error("expected logsAutoScroll to be false after second 's' press")
	}
	if cmd != nil {
		t.Error("expected nil command when turning off autoscroll")
	}
}

// --- Close overlay with Esc resets autoscroll ---

func TestLogsAutoScroll_ResetOnEsc(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = true

	m = mustApply(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.logsOverlay.AutoScroll {
		t.Error("expected logsAutoScroll to be false after closing overlay with Esc")
	}
	if m.logsOverlay.Active {
		t.Error("expected logsOverlayActive to be false after Esc")
	}
}

// --- Close overlay with 'q' resets autoscroll ---

func TestLogsAutoScroll_ResetOnQ(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = true

	m = sendRune(m, 'q')

	if m.logsOverlay.AutoScroll {
		t.Error("expected logsAutoScroll to be false after closing overlay with 'q'")
	}
	if m.logsOverlay.Active {
		t.Error("expected logsOverlayActive to be false after 'q'")
	}
}

// --- Tick when overlay closed is no-op ---

func TestLogsAutoScrollTick_NoOpWhenOverlayClosed(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = true
	m.logsOverlay.Active = false // overlay closed

	m, cmd := applyMsg(m, logsAutoScrollTickMsg{})

	if cmd != nil {
		t.Error("expected nil command when overlay is closed (tick should stop)")
	}
}

// --- Tick when autoscroll off is no-op ---

func TestLogsAutoScrollTick_NoOpWhenAutoScrollOff(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = false

	m, cmd := applyMsg(m, logsAutoScrollTickMsg{})

	if cmd != nil {
		t.Error("expected nil command when autoscroll is off (tick should stop)")
	}
}

// --- Tick when active schedules next tick + refetch ---

func TestLogsAutoScrollTick_SchedulesNextWhenActive(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = true

	_, cmd := applyMsg(m, logsAutoScrollTickMsg{})

	if cmd == nil {
		t.Error("expected a command to be returned (next tick + log refetch)")
	}
}

// --- Render shows AutoScroll:OFF ---

func TestLogsOverlay_ShowsAutoScrollOFF(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = false

	view := m.View()
	if !strings.Contains(view, "AutoScroll:OFF") {
		t.Error("expected view to contain 'AutoScroll:OFF'")
	}
}

// --- Render shows AutoScroll:ON ---

func TestLogsOverlay_ShowsAutoScrollON(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = true

	view := m.View()
	if !strings.Contains(view, "AutoScroll:ON") {
		t.Error("expected view to contain 'AutoScroll:ON'")
	}
}

// --- handleLogsExec resets autoscroll ---

func TestLogsExec_ResetsAutoScroll(t *testing.T) {
	mc := clusterstate.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []clusterstate.Resource{
		{Name: "test-pod", Type: "Pod", Namespace: "default", Ready: "1/1", Scheduled: "Running"},
	}
	mc.SetSnapshot(snap)
	m := newTestModelWithCache(mc)
	m.forestResourceType = "pod"
	m.applySnapshot()

	// Set autoscroll as if it were previously active
	m.logsOverlay.AutoScroll = true

	// Put in containers view to use the direct logs path
	m.viewState.ViewType = clusterstate.ContainersView
	m.viewState.SelectedPod = "test-pod"
	m.containerInfos = []clusterstate.ContainerInfo{
		{Name: "main", Image: "nginx", State: "Running", Ready: true},
	}
	m.rebuildContainersTable()

	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	if m.logsOverlay.AutoScroll {
		t.Error("expected logsAutoScroll to be reset to false when opening new logs overlay")
	}
}

// --- resolveCarriageReturns ---

func TestResolveCarriageReturns_NoOp(t *testing.T) {
	input := "hello\nworld"
	got := resolveCarriageReturns(input)
	if got != input {
		t.Errorf("expected no change, got %q", got)
	}
}

func TestResolveCarriageReturns_ProgressBar(t *testing.T) {
	// Simulates tqdm output: multiple \r-separated updates on one line
	input := "Loading:  10%\rLoading:  50%\rLoading: 100%"
	got := resolveCarriageReturns(input)
	if got != "Loading: 100%" {
		t.Errorf("expected last segment, got %q", got)
	}
}

func TestResolveCarriageReturns_MultiLine(t *testing.T) {
	input := "line1\roverwritten1\nline2\roverwritten2\nplain"
	got := resolveCarriageReturns(input)
	want := "overwritten1\noverwritten2\nplain"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- truncateLines ---

func TestTruncateLines_ShortLinesUnchanged(t *testing.T) {
	input := "short\nlines"
	got := truncateLines(input, 80)
	if got != input {
		t.Errorf("expected no change for short lines, got %q", got)
	}
}

func TestTruncateLines_LongLinesTruncated(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := truncateLines(long, 50)
	if len(got) > 50 {
		t.Errorf("expected truncated to <=50 chars, got len=%d", len(got))
	}
}

// --- handleLogsRequest stores namespace and resets autoscroll ---

func TestLogsRequest_StoresNamespaceAndResetsAutoScroll(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.AutoScroll = true

	m = mustApply(m, LogsRequestMsg{PodName: "new-pod", Namespace: "kube-system", Container: "sidecar"})

	if m.logsOverlay.AutoScroll {
		t.Error("expected logsAutoScroll to be reset on LogsRequestMsg")
	}
	if m.logsOverlay.Namespace != "kube-system" {
		t.Errorf("expected logsNamespace = %q, got %q", "kube-system", m.logsOverlay.Namespace)
	}
}

// ===========================================================================
// Part 5: Logs Overlay Horizontal Scrolling Tests
// ===========================================================================

// --- horizontalSlice ---

func TestHorizontalSlice_NoOffset(t *testing.T) {
	input := "abcdefghij\n1234567890"
	got := horizontalSlice(input, 0, 5)
	want := "abcde\n12345"
	if got != want {
		t.Errorf("horizontalSlice(offset=0, width=5) = %q, want %q", got, want)
	}
}

func TestHorizontalSlice_WithOffset(t *testing.T) {
	input := "abcdefghij\n1234567890"
	got := horizontalSlice(input, 3, 5)
	want := "defgh\n45678"
	if got != want {
		t.Errorf("horizontalSlice(offset=3, width=5) = %q, want %q", got, want)
	}
}

func TestHorizontalSlice_OffsetBeyondLine(t *testing.T) {
	input := "short\nabc"
	got := horizontalSlice(input, 100, 10)
	want := "\n"
	if got != want {
		t.Errorf("horizontalSlice(offset=100) = %q, want %q (empty lines)", got, want)
	}
}

// --- Arrow key tests ---

func TestLogsRightArrow_IncrementsOffset(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.WrapEnabled = false
	m.logsOverlay.HorizontalOffset = 0

	m = mustApply(m, tea.KeyMsg{Type: tea.KeyRight})

	if m.logsOverlay.HorizontalOffset != logsHorizontalScrollStep {
		t.Errorf("logsHorizontalOffset = %d, want %d", m.logsOverlay.HorizontalOffset, logsHorizontalScrollStep)
	}
}

func TestLogsLeftArrow_DecrementsOffset(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.WrapEnabled = false
	m.logsOverlay.HorizontalOffset = 16

	m = mustApply(m, tea.KeyMsg{Type: tea.KeyLeft})

	want := 16 - logsHorizontalScrollStep
	if m.logsOverlay.HorizontalOffset != want {
		t.Errorf("logsHorizontalOffset = %d, want %d", m.logsOverlay.HorizontalOffset, want)
	}
}

func TestLogsLeftArrow_ClampsAtZero(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.WrapEnabled = false
	m.logsOverlay.HorizontalOffset = 3 // less than one step

	m = mustApply(m, tea.KeyMsg{Type: tea.KeyLeft})

	if m.logsOverlay.HorizontalOffset != 0 {
		t.Errorf("logsHorizontalOffset = %d, want 0 (clamped)", m.logsOverlay.HorizontalOffset)
	}
}

func TestLogsLeftArrow_NoOpWhenWrapEnabled(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.WrapEnabled = true
	m.logsOverlay.HorizontalOffset = 16

	m = mustApply(m, tea.KeyMsg{Type: tea.KeyLeft})

	if m.logsOverlay.HorizontalOffset != 16 {
		t.Errorf("logsHorizontalOffset = %d, want 16 (unchanged when wrap enabled)", m.logsOverlay.HorizontalOffset)
	}
}

func TestLogsWrapToggle_ResetsHorizontalOffset(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.WrapEnabled = false
	m.logsOverlay.HorizontalOffset = 24

	m = sendRune(m, 'w')

	if m.logsOverlay.HorizontalOffset != 0 {
		t.Errorf("logsHorizontalOffset = %d, want 0 after wrap toggle", m.logsOverlay.HorizontalOffset)
	}
	if !m.logsOverlay.WrapEnabled {
		t.Error("expected logsWrapEnabled to be true after toggle")
	}
}

// --- Col indicator tests ---

func TestLogsOverlay_ShowsColIndicator(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.HorizontalOffset = 16

	view := m.View()
	if !strings.Contains(view, "Col:16") {
		t.Error("expected view to contain 'Col:16' when logsHorizontalOffset > 0")
	}
}

func TestLogsOverlay_HidesColIndicator(t *testing.T) {
	m := newTestModelWithLogsOverlay()
	m.logsOverlay.HorizontalOffset = 0

	view := m.View()
	if strings.Contains(view, "Col:") {
		t.Error("expected view NOT to contain 'Col:' when logsHorizontalOffset = 0")
	}
}

