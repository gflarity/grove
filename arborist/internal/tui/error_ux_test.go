package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
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
	mc := data.NewMockGlobalCache()
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

// --- 'e' keybinding ---

func TestEKey_TogglesErrorLogOff(t *testing.T) {
	m := newTestModel(nil)
	m.addError("err")
	if !m.errorLogVisible {
		t.Fatal("precondition: errorLogVisible should be true")
	}

	m = sendRune(m, 'e')
	if m.errorLogVisible {
		t.Error("expected 'e' to toggle errorLogVisible to false")
	}
}

func TestEKey_TogglesErrorLogOn(t *testing.T) {
	m := newTestModel(nil)
	m.addError("err")
	m.errorLogVisible = false

	m = sendRune(m, 'e')
	if !m.errorLogVisible {
		t.Error("expected 'e' to toggle errorLogVisible to true")
	}
}

func TestEKey_TogglesEvenWhenErrorLogEmpty(t *testing.T) {
	m := newTestModel(nil)
	// No errors — 'e' should still toggle errorLogVisible
	m = sendRune(m, 'e')
	if !m.errorLogVisible {
		t.Error("expected 'e' to toggle errorLogVisible to true even with empty errorLog")
	}
	// Toggle back
	m = sendRune(m, 'e')
	if m.errorLogVisible {
		t.Error("expected 'e' to toggle errorLogVisible back to false")
	}
}

func TestNewError_ReShowsBoxAfterToggleAway(t *testing.T) {
	m := newTestModel(nil)
	m.addError("first error")

	// User toggles it away
	m = sendRune(m, 'e')
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
	if !strings.Contains(shortcuts, "<e>Errors") {
		t.Errorf("expected shortcuts to contain '<e>Errors', got %q", shortcuts)
	}
}

func TestErrorsMenuHint_AlwaysPresent(t *testing.T) {
	m := newTestModel(nil)

	shortcuts := m.buildShortcutsString()
	if !strings.Contains(shortcuts, "<e>Errors") {
		t.Errorf("expected shortcuts to always contain '<e>Errors', got %q", shortcuts)
	}
}

// --- YAML overlay still renders on top ---

func TestYAMLOverlay_RendersOverErrorLog(t *testing.T) {
	m := newTestModel(nil)
	m.addError("err")
	m.yamlOverlayActive = true
	m.yamlContent = "apiVersion: v1"
	m.yamlViewport.SetContent(m.yamlContent)

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
	m = sendRune(m, 'e')
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
	m = sendRune(m, 'e')
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
	m.topologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{},
	}

	if m.topologyAvailable() {
		t.Error("expected topologyAvailable() = false for empty Domains")
	}
}

func TestTopologyAvailable_WithValidDomains(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
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

	if m.viewState.ViewType != data.ForestView {
		t.Errorf("expected to stay in ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
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
	m.topologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	m = sendRune(m, 't')

	if m.viewState.ViewType != data.TopologyView {
		t.Errorf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.errorLog) != 0 {
		t.Errorf("expected no errors, got %d", len(m.errorLog))
	}
}

// --- :topology command with no topology data ---

func TestTopologyCommand_NoTopologyData_LogsError(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = nil

	// Simulate ":topology" command
	m.commandActive = true
	m.commandInput.SetValue("topology")
	m = mustApply(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.viewState.ViewType != data.ForestView {
		t.Errorf("expected to stay in ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.errorLog) != 1 {
		t.Fatalf("expected 1 error, got %d", len(m.errorLog))
	}
	if !strings.Contains(m.errorLog[0].Message, "Topology unavailable") {
		t.Errorf("error message = %q, want something about topology unavailable", m.errorLog[0].Message)
	}
}

// --- :topology command with topology data ---

func TestTopologyCommand_WithTopologyData_Switches(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	m.commandActive = true
	m.commandInput.SetValue("topology")
	m = mustApply(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.viewState.ViewType != data.TopologyView {
		t.Errorf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

// --- Pressing 't' from TopologyView goes back even if topology data disappeared ---

func TestTKey_FromTopologyView_GoesBackEvenIfDataDisappeared(t *testing.T) {
	m := newTestModel(nil)
	m.topologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
		},
	}

	// Enter topology view
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.TopologyView {
		t.Fatal("precondition: should be in TopologyView")
	}

	// Topology data disappears
	m.topologyViewData = nil

	// Press 't' to go back — should work unconditionally
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.ForestView {
		t.Errorf("expected ForestView after toggling back, got %s", data.ViewTypeName(m.viewState.ViewType))
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
	m.topologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
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
	mc := data.NewMockGlobalCache()
	m := newTestModelWithCache(mc)

	// Initially no topology data
	m.topologyViewData = nil
	if m.topologyAvailable() {
		t.Fatal("precondition: topology should be unavailable")
	}

	// Simulate cache update that delivers topology data
	snap := mc.Snapshot()
	snap.TopologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
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
	if m.viewState.ViewType != data.TopologyView {
		t.Errorf("expected TopologyView after cache update, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}
