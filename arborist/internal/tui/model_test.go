package tui

import (
	"strings"
	"testing"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestModel creates a Model backed by a MockGlobalCache pre-loaded with the
// given PodCliqueSet resources. It also sends a WindowSizeMsg so the model is
// considered "ready" and will render a full view, then delivers CacheSyncedMsg.
func newTestModel(pcsResources []data.Resource) Model {
	mc := data.NewMockGlobalCache()
	if pcsResources != nil {
		snap := mc.Snapshot()
		snap.PodCliqueSets = pcsResources
		mc.SetSnapshot(snap)
	}
	m := NewModel(mc)
	// Simulate initial window size so the model is ready
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	// Deliver cache synced so data is populated
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	return m
}

// newTestModelWithCache creates a Model backed by a fully configured
// MockGlobalCache. Sends a WindowSizeMsg to make the model ready and
// delivers CacheSyncedMsg.
func newTestModelWithCache(mc *data.MockGlobalCache) Model {
	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	return m
}

// mustApply sends a single message through the model's Update function and
// returns the updated model, discarding the command.
func mustApply(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

// applyMsg sends a single message through the model's Update function and
// returns the updated model plus the command.
func applyMsg(m Model, msg tea.Msg) (Model, tea.Cmd) {
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

// sendKey sends a tea.KeyMsg with the given key type through the model.
func sendKey(m Model, keyType tea.KeyType) Model {
	return mustApply(m, tea.KeyMsg{Type: keyType})
}

// sendRune sends a tea.KeyMsg containing a single rune (e.g. 'q', '/').
func sendRune(m Model, r rune) Model {
	return mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// assertView asserts that the rendered view contains every string in `contains`.
func assertView(t *testing.T, m Model, contains []string) {
	t.Helper()
	view := m.View()
	for _, s := range contains {
		if !strings.Contains(view, s) {
			t.Errorf("expected view to contain %q but it did not.\nview:\n%s", s, view)
		}
	}
}

// assertNotInView asserts that the rendered view does NOT contain any string in `absent`.
func assertNotInView(t *testing.T, m Model, absent []string) {
	t.Helper()
	view := m.View()
	for _, s := range absent {
		if strings.Contains(view, s) {
			t.Errorf("expected view NOT to contain %q but it did.\nview:\n%s", s, view)
		}
	}
}

// executeCmd executes a tea.Cmd synchronously and returns the resulting message.
// Returns nil if cmd is nil.
func executeCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// executeCmdAndApply executes a tea.Cmd and applies the resulting message(s)
// to the model. Handles tea.BatchMsg by executing each sub-command and
// applying the results sequentially.
func executeCmdAndApply(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}

	// tea.Batch returns a BatchMsg which is []Cmd
	if batchMsg, ok := msg.(tea.BatchMsg); ok {
		for _, subCmd := range batchMsg {
			if subCmd != nil {
				subMsg := subCmd()
				if subMsg != nil {
					if _, isBatch := subMsg.(tea.BatchMsg); isBatch {
						m = executeCmdAndApply(m, subCmd)
					} else {
						var nextCmd tea.Cmd
						m, nextCmd = applyMsg(m, subMsg)
						if nextCmd != nil {
							m = executeCmdAndApply(m, nextCmd)
						}
					}
				}
			}
		}
		return m
	}

	var nextCmd tea.Cmd
	m, nextCmd = applyMsg(m, msg)
	if nextCmd != nil {
		m = executeCmdAndApply(m, nextCmd)
	}
	return m
}

// ---------------------------------------------------------------------------
// Sample data factories
// ---------------------------------------------------------------------------

func samplePCSResources() []data.Resource {
	return []data.Resource{
		{Name: "alpha-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "3/3", Scheduled: "3/3", Topology: "rack"},
		{Name: "beta-pcs", Type: "PodCliqueSet", Namespace: "staging", Ready: "2/3", Scheduled: "3/3", Topology: "zone"},
		{Name: "gamma-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "0/5", Scheduled: "5/5", Topology: "N/A"},
	}
}

func sampleEvents() []data.Event {
	return []data.Event{
		{Type: "Normal", Reason: "Scaled", Age: "5m", From: "controller", Message: "Scaled up to 3", Parent: "alpha-pcs"},
		{Type: "Warning", Reason: "Unschedulable", Age: "2m", From: "scheduler", Message: "No nodes available", Parent: "beta-pcs"},
		{Type: "Normal", Reason: "Created", Age: "10m", From: "controller", Message: "Created pod", Parent: "alpha-pcs"},
	}
}

func sampleReplicaChildren() ([]data.Resource, []data.Resource) {
	scalingGroups := []data.Resource{
		{Name: "alpha-pcs-0-sg-prefill", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "2/2", Scheduled: "2/2", Topology: "block"},
	}
	podCliques := []data.Resource{
		{Name: "alpha-pcs-0-standalone-pc", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1", Topology: "rack"},
	}
	return scalingGroups, podCliques
}

func samplePods() []data.Resource {
	return []data.Resource{
		{Name: "alpha-pcs-0-pc-worker-0", Type: "Pod", Namespace: "default", Ready: "1/1", Scheduled: "Running", Topology: "(rack)"},
		{Name: "alpha-pcs-0-pc-worker-1", Type: "Pod", Namespace: "default", Ready: "1/1", Scheduled: "Running", Topology: "(rack)"},
		{Name: "alpha-pcs-0-pc-worker-2", Type: "Pod", Namespace: "default", Ready: "0/1", Scheduled: "Pending", Topology: "(rack)"},
	}
}

// buildFullMockCache creates a MockGlobalCache with a complete hierarchy for
// "alpha-pcs": Forest -> PCS (1 replica) -> Replica children -> PodClique -> Pods.
func buildFullMockCache() *data.MockGlobalCache {
	mc := data.NewMockGlobalCache()
	scalingGroups, podCliques := sampleReplicaChildren()

	snapshot := &data.CacheSnapshot{
		PodCliqueSets: samplePCSResources(),
		PodCliqueSetSpecs: map[string]*corev1alpha1.PodCliqueSet{
			"alpha-pcs": {ObjectMeta: metav1.ObjectMeta{Name: "alpha-pcs", Namespace: "default"}, Spec: corev1alpha1.PodCliqueSetSpec{Replicas: 3}},
			"beta-pcs":  {ObjectMeta: metav1.ObjectMeta{Name: "beta-pcs", Namespace: "staging"}, Spec: corev1alpha1.PodCliqueSetSpec{Replicas: 3}},
			"gamma-pcs": {ObjectMeta: metav1.ObjectMeta{Name: "gamma-pcs", Namespace: "default"}, Spec: corev1alpha1.PodCliqueSetSpec{Replicas: 5}},
		},
		ReplicaIndexesByPCS: map[string][]string{
			"alpha-pcs": {"0"},
			"beta-pcs":  {"0", "1"},
		},
		ScalingGroupsByReplica: map[string][]data.Resource{
			"alpha-pcs/0": scalingGroups,
			"beta-pcs/0": {
				{Name: "beta-pcs-0-sg-main", Type: "PodCliqueScalingGroup", Namespace: "staging", Ready: "1/1", Scheduled: "1/1"},
			},
			"beta-pcs/1": {
				{Name: "beta-pcs-1-sg-main", Type: "PodCliqueScalingGroup", Namespace: "staging", Ready: "1/1", Scheduled: "1/1"},
			},
		},
		PodCliquesByReplica: map[string][]data.Resource{
			"alpha-pcs/0": podCliques,
		},
		ReplicaIndexesByPCSG: map[string][]string{
			"alpha-pcs-0-sg-prefill": {"0", "1"},
		},
		PodCliquesByPCSG: map[string][]data.Resource{
			"alpha-pcs-0-sg-prefill": {
				{Name: "alpha-pcs-0-sg-prefill-0-worker", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
				{Name: "alpha-pcs-0-sg-prefill-1-worker", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
			},
		},
		PodCliquesByPCSGReplica: map[string][]data.Resource{
			"alpha-pcs-0-sg-prefill/0": {
				{Name: "alpha-pcs-0-sg-prefill-0-worker", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
			},
			"alpha-pcs-0-sg-prefill/1": {
				{Name: "alpha-pcs-0-sg-prefill-1-worker", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
			},
		},
		PodsByPodClique: map[string][]data.Resource{
			"alpha-pcs-0-standalone-pc":       samplePods(),
			"alpha-pcs-0-sg-prefill-0-worker": samplePods(),
		},
		EventsByObject:  make(map[string][]data.Event),
		NodeLabels:      make(map[string]map[string]string),
		PodInfos:        make(map[string]data.CachedPodInfo),
		NodeGPUProducts: make(map[string]string),
		NodeGPUCapacity: make(map[string]int64),
	}

	mc.SetSnapshot(snapshot)
	mc.PodYAMLs["default/alpha-pcs-0-pc-worker-0"] = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: alpha-pcs-0-pc-worker-0\n"

	return mc
}

// ---------------------------------------------------------------------------
// 6.1 Test: Test infrastructure itself
// ---------------------------------------------------------------------------

func TestNewTestModel_IsReady(t *testing.T) {
	m := newTestModel(samplePCSResources())
	if !m.ready {
		t.Fatal("expected model to be ready after WindowSizeMsg")
	}
	view := m.View()
	if view == "Loading..." {
		t.Fatal("expected rendered view, got Loading...")
	}
}

// ---------------------------------------------------------------------------
// 6.2 Test: Initial render with forest data
// ---------------------------------------------------------------------------

func TestInitialRenderWithForestData(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// The view should contain all PCS names
	assertView(t, m, []string{"alpha-pcs", "beta-pcs", "gamma-pcs"})
	// Should contain resource types
	assertView(t, m, []string{"PodCliqueSet"})
	// Should contain namespaces
	assertView(t, m, []string{"default", "staging"})
	// Should show menu bar shortcuts (k9s-style)
	assertView(t, m, []string{"Filter", "Switch", "Quit"})
	// Should show the breadcrumb indicating ForestView
	assertView(t, m, []string{"Forest"})
}

func TestInitialRenderEmptyForest(t *testing.T) {
	m := newTestModel([]data.Resource{})

	// Should still render without panic
	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}
	// Forest breadcrumb should still show
	assertView(t, m, []string{"Forest"})
}

// ---------------------------------------------------------------------------
// 6.3 Test: Navigation drill-down scenario
// ---------------------------------------------------------------------------

func TestNavigationDrillDown(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Verify we're at ForestView
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Press Enter to drill into first PCS (alpha-pcs).
	// Navigation is synchronous — data comes from snapshot.
	m = sendKey(m, tea.KeyEnter)

	// Verify the PCS was selected
	if m.viewState.SelectedPodCliqueSet != "alpha-pcs" {
		t.Fatalf("expected SelectedPodCliqueSet=alpha-pcs, got %s", m.viewState.SelectedPodCliqueSet)
	}

	// alpha-pcs has 1 replica, so it should skip to PodCliqueSetReplicaView
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView after single-replica skip, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Breadcrumb should show the path
	assertView(t, m, []string{"Forest", "alpha-pcs", "replica-0"})

	// Now navigate back
	m = sendKey(m, tea.KeyEsc)

	// Since alpha-pcs has 1 replica, back should go to ForestView directly
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after back from single-replica PCS, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

// ---------------------------------------------------------------------------
// 6.4 Test: Single-replica skip behavior
// ---------------------------------------------------------------------------

func TestSingleReplicaSkip(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Press Enter to drill into alpha-pcs (has 1 replica)
	m = sendKey(m, tea.KeyEnter)

	// Should skip directly to PodCliqueSetReplicaView
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView (single-replica skip), got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.viewState.SelectedReplicaIndex != "0" {
		t.Fatalf("expected SelectedReplicaIndex=0, got %s", m.viewState.SelectedReplicaIndex)
	}

	// Press Esc - should return to ForestView (not PodCliqueSetView)
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc from single-replica skip, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

// TestMultiReplicaPCS_FullNavigationFlow verifies pressing Enter on a PCS with
// multiple replicas transitions to PodCliqueSetView and shows the replica list.
func TestMultiReplicaPCS_FullNavigationFlow(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Move down to beta-pcs (second row, has 2 replicas)
	m = sendKey(m, tea.KeyDown)

	// Verify beta-pcs is selected
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 || selectedRow[2] != "beta-pcs" {
		t.Fatalf("expected beta-pcs selected, got %v", selectedRow)
	}

	// Press Enter to drill into beta-pcs
	m = sendKey(m, tea.KeyEnter)

	// beta-pcs has 2 replicas, should stay at PodCliqueSetView with replica list
	if m.viewState.ViewType != data.PodCliqueSetView {
		t.Fatalf("expected PodCliqueSetView for multi-replica PCS, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.viewState.SelectedPodCliqueSet != "beta-pcs" {
		t.Fatalf("expected SelectedPodCliqueSet=beta-pcs, got %s", m.viewState.SelectedPodCliqueSet)
	}

	// Check that 2 replicas are stored and visible
	pcsKey := "PodCliqueSet/beta-pcs"
	if len(m.allResources[pcsKey]) != 2 {
		t.Fatalf("expected 2 replica resources, got %d", len(m.allResources[pcsKey]))
	}

	// Back should go to ForestView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

// ---------------------------------------------------------------------------
// 6.5 Test: Filter functionality
// ---------------------------------------------------------------------------

func TestFilterFunctionality(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Verify all PCSes visible
	assertView(t, m, []string{"alpha-pcs", "beta-pcs", "gamma-pcs"})

	// Press '/' to activate filter
	m = sendRune(m, '/')
	if !m.filterActive {
		t.Fatal("expected filter to be active after pressing /")
	}

	// Type "alpha" - send each rune through the filter input
	for _, r := range "alpha" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Only alpha-pcs should be visible
	assertView(t, m, []string{"alpha-pcs"})
	assertNotInView(t, m, []string{"beta-pcs", "gamma-pcs"})

	// Press Enter to apply the filter
	m = sendKey(m, tea.KeyEnter)
	if m.filterActive {
		t.Fatal("expected filter mode to be deactivated after Enter")
	}
	if m.filterText != "alpha" {
		t.Fatalf("expected filterText='alpha', got %q", m.filterText)
	}
	// Filter should still be applied
	assertView(t, m, []string{"alpha-pcs"})
	assertNotInView(t, m, []string{"beta-pcs", "gamma-pcs"})

	// Press '/' then Esc to clear the filter
	m = sendRune(m, '/')
	m = sendKey(m, tea.KeyEsc)
	if m.filterActive {
		t.Fatal("expected filter to be inactive after Esc")
	}
	if m.filterText != "" {
		t.Fatalf("expected filterText to be empty after Esc, got %q", m.filterText)
	}
	// All PCSes should be visible again
	assertView(t, m, []string{"alpha-pcs", "beta-pcs", "gamma-pcs"})
}

func TestFilterStateTransitions(t *testing.T) {
	m := newTestModel(samplePCSResources())

	// Initially filter is off
	if m.filterActive {
		t.Fatal("expected filter inactive initially")
	}

	// Activate filter
	m = sendRune(m, '/')
	if !m.filterActive {
		t.Fatal("expected filter active after /")
	}

	// Type a character
	m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.filterInput.Value() != "a" {
		t.Fatalf("expected filter input 'a', got %q", m.filterInput.Value())
	}

	// Esc clears
	m = sendKey(m, tea.KeyEsc)
	if m.filterActive {
		t.Fatal("expected filter inactive after Esc")
	}
	if m.filterText != "" {
		t.Fatalf("expected empty filterText, got %q", m.filterText)
	}
}

// ---------------------------------------------------------------------------
// 6.6 Test: Pane switching
// ---------------------------------------------------------------------------

func TestPaneSwitching(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Initial active pane should be Resources
	if m.activePane != data.ResourcesPane {
		t.Fatalf("expected initial pane to be ResourcesPane, got %d", m.activePane)
	}

	// Press Tab to switch to Events
	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.EventsPane {
		t.Fatalf("expected EventsPane after Tab, got %d", m.activePane)
	}

	// Press Tab again to switch back to Resources
	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.ResourcesPane {
		t.Fatalf("expected ResourcesPane after second Tab, got %d", m.activePane)
	}
}

func TestPaneSwitchingDuringFilter(t *testing.T) {
	m := newTestModel(samplePCSResources())

	// Activate filter
	m = sendRune(m, '/')
	if !m.filterActive {
		t.Fatal("expected filter active")
	}

	// Tab should still switch panes
	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.EventsPane {
		t.Fatalf("expected pane switch during filter mode, got %d", m.activePane)
	}
}

func TestPaneSwitchingUpdatesTableFocus(t *testing.T) {
	m := newTestModel(samplePCSResources())

	// Initially resources table should be focused
	if !m.resourcesTable.Focused() {
		t.Fatal("expected resources table focused initially")
	}
	if m.eventsTable.Focused() {
		t.Fatal("expected events table not focused initially")
	}

	// Switch to events
	m = sendKey(m, tea.KeyTab)
	if m.resourcesTable.Focused() {
		t.Fatal("expected resources table unfocused after Tab")
	}
	if !m.eventsTable.Focused() {
		t.Fatal("expected events table focused after Tab")
	}

	// Switch back
	m = sendKey(m, tea.KeyTab)
	if !m.resourcesTable.Focused() {
		t.Fatal("expected resources table focused after second Tab")
	}
}

// ---------------------------------------------------------------------------
// 6.7 Test: Events filtering by selection
// ---------------------------------------------------------------------------

func TestEventsFilterByPodView(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Put model into PodView state manually
	m.viewState.ViewType = data.PodView
	m.viewState.SelectedPod = "alpha-pcs-0-pc-worker-0"
	m.allEvents = []data.Event{
		{Type: "Normal", Reason: "Scheduled", Age: "1m", From: "scheduler", Message: "Assigned", Parent: "alpha-pcs-0-pc-worker-0"},
		{Type: "Normal", Reason: "Pulled", Age: "30s", From: "kubelet", Message: "Pulled image", Parent: "alpha-pcs-0-pc-worker-0"},
		{Type: "Warning", Reason: "Failed", Age: "10s", From: "kubelet", Message: "Failed", Parent: "other-pod"},
	}

	// getFilteredEvents should only return events for the selected pod
	filtered := m.getFilteredEvents()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 events for selected pod, got %d", len(filtered))
	}
	for _, e := range filtered {
		if e.Parent != "alpha-pcs-0-pc-worker-0" {
			t.Errorf("expected event parent to be alpha-pcs-0-pc-worker-0, got %s", e.Parent)
		}
	}
}

func TestEventsShowAllInForestView(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m.viewState.ViewType = data.ForestView
	m.allEvents = sampleEvents()

	filtered := m.getFilteredEvents()
	if len(filtered) != len(sampleEvents()) {
		t.Fatalf("expected all %d events in ForestView, got %d", len(sampleEvents()), len(filtered))
	}
}

// ---------------------------------------------------------------------------
// 6.8 Test: Pod YAML view
// ---------------------------------------------------------------------------

func TestPodYAMLView_ViaMessages(t *testing.T) {
	m := newTestModel(nil)

	// Set up at PodView
	m.viewState.ViewType = data.PodView
	m.viewState.SelectedPod = "alpha-pcs-0-pc-worker-0"

	// Deliver PodYAMLMsg
	yamlContent := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: alpha-pcs-0-pc-worker-0\n"
	m = mustApply(m, PodYAMLMsg{PodName: "alpha-pcs-0-pc-worker-0", YAML: yamlContent})

	// The YAML should be stored
	yaml, exists := m.podYAMLData["alpha-pcs-0-pc-worker-0"]
	if !exists {
		t.Fatal("expected podYAMLData to be populated")
	}
	if !strings.Contains(yaml, "kind: Pod") {
		t.Errorf("expected YAML to contain 'kind: Pod', got: %s", yaml)
	}

	// Press Esc to go back to PodClique view
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView after Esc from PodView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestPodYAMLView_FullDrillDown(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Navigate into alpha-pcs (single replica skip -> PodCliqueSetReplicaView)
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Navigate into the first child resource
	m = sendKey(m, tea.KeyEnter)

	// Continue drilling until we reach PodView or can't go further
	for m.viewState.ViewType != data.PodView && m.viewState.ViewType != data.ForestView {
		prevViewType := m.viewState.ViewType
		m = sendKey(m, tea.KeyEnter)
		if m.viewState.ViewType == prevViewType {
			break
		}
	}

	if m.viewState.ViewType == data.PodView {
		if m.viewState.SelectedPod == "" {
			t.Fatal("expected SelectedPod to be set in PodView")
		}

		// Go back
		m = sendKey(m, tea.KeyEsc)
		if m.viewState.ViewType != data.PodCliqueView {
			t.Fatalf("expected PodCliqueView after Esc, got %s", data.ViewTypeName(m.viewState.ViewType))
		}
	}
}

func TestPodViewportContent(t *testing.T) {
	m := newTestModel(nil)

	// Manually put model in PodView
	m.viewState.ViewType = data.PodView
	m.viewState.SelectedPod = "my-pod"

	// Deliver PodYAMLMsg
	yamlContent := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: my-pod\nspec:\n  containers: []\n"
	m = mustApply(m, PodYAMLMsg{PodName: "my-pod", YAML: yamlContent})

	if m.podYAMLData["my-pod"] != yamlContent {
		t.Errorf("expected podYAMLData to contain the YAML, got %q", m.podYAMLData["my-pod"])
	}
}

// ---------------------------------------------------------------------------
// 6.9 Test: Error handling
// ---------------------------------------------------------------------------

func TestErrorHandling_GenericErrorMsg(t *testing.T) {
	m := newTestModel(nil)

	m = mustApply(m, ErrorMsg{Operation: "test-op", Err: errForTest("something broke")})

	if m.lastError == nil {
		t.Fatal("expected lastError to be set")
	}
}

func TestErrorHandling_PodYAMLError(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.ViewType = data.PodView
	m.viewState.SelectedPod = "broken-pod"

	m = mustApply(m, PodYAMLMsg{PodName: "broken-pod", Err: errForTest("pod not found")})

	// Should store an error message in podYAMLData instead of crashing
	yaml, exists := m.podYAMLData["broken-pod"]
	if !exists {
		t.Fatal("expected podYAMLData entry even on error")
	}
	if !strings.Contains(yaml, "Error") {
		t.Errorf("expected error message in YAML data, got %q", yaml)
	}
}

// errForTest creates a simple error for test assertions.
type testError string

func (e testError) Error() string { return string(e) }

func errForTest(msg string) error {
	return testError(msg)
}

// ---------------------------------------------------------------------------
// 6.10 Test: Window resize
// ---------------------------------------------------------------------------

func TestWindowResize(t *testing.T) {
	m := newTestModel(samplePCSResources())

	// Normal size
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", m.width, m.height)
	}
	view := m.View()
	if view == "" || view == "Loading..." {
		t.Fatal("expected rendered view at 120x40")
	}

	// Resize to smaller
	m = mustApply(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.width != 80 || m.height != 24 {
		t.Fatalf("expected 80x24, got %dx%d", m.width, m.height)
	}
	view = m.View()
	if view == "" {
		t.Fatal("expected non-empty view at 80x24")
	}

	// Resize to very small (should not panic)
	m = mustApply(m, tea.WindowSizeMsg{Width: 20, Height: 10})
	if m.width != 20 || m.height != 10 {
		t.Fatalf("expected 20x10, got %dx%d", m.width, m.height)
	}
	view = m.View()
	if view == "" {
		t.Fatal("expected non-empty view at 20x10")
	}

	// Resize to very large
	m = mustApply(m, tea.WindowSizeMsg{Width: 300, Height: 100})
	view = m.View()
	if view == "" {
		t.Fatal("expected non-empty view at 300x100")
	}
}

func TestWindowResize_BeforeReady(t *testing.T) {
	mc := data.NewMockGlobalCache()
	m := NewModel(mc)

	// Before WindowSizeMsg, view should show "Loading..."
	if m.View() != "Loading..." {
		t.Fatalf("expected 'Loading...' before WindowSizeMsg, got %q", m.View())
	}

	// First WindowSizeMsg makes it ready
	m = mustApply(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if !m.ready {
		t.Fatal("expected model to be ready after first WindowSizeMsg")
	}
	if m.View() == "Loading..." {
		t.Fatal("expected rendered view after WindowSizeMsg")
	}
}

// ---------------------------------------------------------------------------
// Additional scenario tests
// ---------------------------------------------------------------------------

func TestQuitWithQ(t *testing.T) {
	m := newTestModel(samplePCSResources())

	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	// The command should be tea.Quit
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestQuitWithCtrlC(t *testing.T) {
	m := newTestModel(samplePCSResources())

	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyCtrlC})

	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsEvenInFilterMode(t *testing.T) {
	m := newTestModel(samplePCSResources())
	m = sendRune(m, '/') // activate filter
	if !m.filterActive {
		t.Fatal("expected filter active")
	}

	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command even in filter mode")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestEscAtForestViewDoesNothing(t *testing.T) {
	m := newTestModel(samplePCSResources())

	// Esc at forest view should not change anything
	before := m.viewState.ViewType
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != before {
		t.Fatalf("expected viewType to remain %s, got %s", data.ViewTypeName(before), data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBackClearsFilter(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set up at PodCliqueSetReplicaView manually
	m.viewState.ViewType = data.PodCliqueSetReplicaView
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"
	m.viewState.SelectedReplicaIndex = "0"
	m.filterText = "something"

	// Navigate back
	m = sendKey(m, tea.KeyEsc)

	// Filter should be cleared
	if m.filterText != "" {
		t.Fatalf("expected filter to be cleared after navigateBack, got %q", m.filterText)
	}
}

func TestNavigateIntoClearsFilter(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set a filter
	m.filterText = "alpha"
	m.rebuildResourcesTable()

	// Navigate into (Enter)
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Filter should be cleared
	if m.filterText != "" {
		t.Fatalf("expected filter to be cleared after navigateInto, got %q", m.filterText)
	}
}

func TestViewStateNavigationFullCycle(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set up deep navigation state manually
	m.viewState = data.ViewState{
		ViewType:             data.PodView,
		SelectedPodCliqueSet: "alpha-pcs",
		SelectedReplicaIndex: "0",
		SelectedPodClique:    "alpha-pcs-0-standalone-pc",
		SelectedPod:          "alpha-pcs-0-pc-worker-0",
	}

	// PodView -> Esc -> PodCliqueView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// PodCliqueView -> Esc -> PodCliqueSetReplicaView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// PodCliqueSetReplicaView -> Esc -> ForestView (single replica)
	// Need allResources with single replica entry for the skip logic
	m.allResources["PodCliqueSet/alpha-pcs"] = []data.Resource{{
		Name: "alpha-pcs-replica-0", Type: "PodCliqueSetReplica", Namespace: "default",
	}}
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBackFromPCSGView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = data.ViewState{
		ViewType:             data.PodCliqueScalingGroupView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "0",
		SelectedScalingGroup: "my-sg",
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.viewState.SelectedScalingGroup != "" {
		t.Fatalf("expected SelectedScalingGroup cleared, got %q", m.viewState.SelectedScalingGroup)
	}
	if m.viewState.SelectedPCSGReplicaIndex != "" {
		t.Fatalf("expected SelectedPCSGReplicaIndex cleared, got %q", m.viewState.SelectedPCSGReplicaIndex)
	}
}

func TestNavigateBackFromPodCliqueView_WithScalingGroup(t *testing.T) {
	m := newTestModel(nil)
	// With PCSG replica index set, back should go to PodCliqueScalingGroupReplicaView
	m.viewState = data.ViewState{
		ViewType:                 data.PodCliqueView,
		SelectedPodCliqueSet:     "my-pcs",
		SelectedReplicaIndex:     "0",
		SelectedScalingGroup:     "my-sg",
		SelectedPCSGReplicaIndex: "0",
		SelectedPodClique:        "my-pc",
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueScalingGroupReplicaView {
		t.Fatalf("expected PodCliqueScalingGroupReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBackFromPodCliqueView_WithScalingGroupNoPCSGReplicaIndex(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = data.ViewState{
		ViewType:             data.PodCliqueView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "0",
		SelectedScalingGroup: "my-sg",
		SelectedPodClique:    "my-pc",
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueScalingGroupView {
		t.Fatalf("expected PodCliqueScalingGroupView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBackFromPodCliqueView_WithoutScalingGroup(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = data.ViewState{
		ViewType:             data.PodCliqueView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "0",
		SelectedPodClique:    "my-pc",
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBackFromPCSGReplicaView_MultiReplica(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = data.ViewState{
		ViewType:                 data.PodCliqueScalingGroupReplicaView,
		SelectedPodCliqueSet:     "my-pcs",
		SelectedReplicaIndex:     "0",
		SelectedScalingGroup:     "my-pcsg",
		SelectedPCSGReplicaIndex: "1",
	}
	// Store 2 PCSG replicas (so it won't skip back)
	m.allResources["PodCliqueScalingGroup/my-pcsg"] = []data.Resource{
		{Name: "my-pcsg-replica-0", Type: "PodCliqueScalingGroupReplica", Namespace: "default"},
		{Name: "my-pcsg-replica-1", Type: "PodCliqueScalingGroupReplica", Namespace: "default"},
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueScalingGroupView {
		t.Fatalf("expected PodCliqueScalingGroupView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.viewState.SelectedPCSGReplicaIndex != "" {
		t.Fatalf("expected SelectedPCSGReplicaIndex cleared, got %q", m.viewState.SelectedPCSGReplicaIndex)
	}
}

func TestPCSGReplica_FullNavigationFlow(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Navigate into alpha-pcs (1 PCS replica → single-replica skip)
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// First row should be the PCSG: alpha-pcs-0-sg-prefill
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 || selectedRow[2] != "alpha-pcs-0-sg-prefill" {
		t.Fatalf("expected alpha-pcs-0-sg-prefill selected, got %v", selectedRow)
	}

	// Navigate into the PCSG (has 2 PCSG replicas → should show PCSG replica list)
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.ViewType != data.PodCliqueScalingGroupView {
		t.Fatalf("expected PodCliqueScalingGroupView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	pcsgKey := "PodCliqueScalingGroup/alpha-pcs-0-sg-prefill"
	if len(m.allResources[pcsgKey]) != 2 {
		t.Fatalf("expected 2 PCSG replica resources, got %d", len(m.allResources[pcsgKey]))
	}

	// Navigate into first PCSG replica
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.ViewType != data.PodCliqueScalingGroupReplicaView {
		t.Fatalf("expected PodCliqueScalingGroupReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Should show PodCliques for this PCSG replica
	replicaKey := "PodCliqueScalingGroupReplica/alpha-pcs-0-sg-prefill/0"
	if len(m.allResources[replicaKey]) != 1 {
		t.Fatalf("expected 1 PodClique in PCSG replica, got %d", len(m.allResources[replicaKey]))
	}

	// Back should go to PodCliqueScalingGroupView (2 replicas, no skip)
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueScalingGroupView {
		t.Fatalf("expected PodCliqueScalingGroupView after Esc, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Back again should go to PodCliqueSetReplicaView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView after Esc, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestPCSGReplica_BreadcrumbRendering(t *testing.T) {
	m := newTestModel(nil)

	m.viewState = data.ViewState{
		ViewType:                 data.PodCliqueScalingGroupReplicaView,
		SelectedPodCliqueSet:     "my-pcs",
		SelectedReplicaIndex:     "0",
		SelectedScalingGroup:     "my-pcsg",
		SelectedPCSGReplicaIndex: "1",
	}
	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, "my-pcsg") {
		t.Errorf("expected breadcrumb to contain 'my-pcsg', got %q", bc)
	}
	if !strings.Contains(bc, "replica-1") {
		t.Errorf("expected breadcrumb to contain 'replica-1' (PCSG replica), got %q", bc)
	}
}

func TestGetCurrentViewKey(t *testing.T) {
	m := newTestModel(nil)

	tests := []struct {
		viewState data.ViewState
		expected  string
	}{
		{data.ViewState{ViewType: data.ForestView}, "forest"},
		{data.ViewState{ViewType: data.PodCliqueSetView, SelectedPodCliqueSet: "my-pcs"}, "PodCliqueSet/my-pcs"},
		{data.ViewState{ViewType: data.PodCliqueSetReplicaView, SelectedPodCliqueSet: "my-pcs", SelectedReplicaIndex: "1"}, "PodCliqueSetReplica/my-pcs/1"},
		{data.ViewState{ViewType: data.PodCliqueScalingGroupView, SelectedScalingGroup: "my-sg"}, "PodCliqueScalingGroup/my-sg"},
		{data.ViewState{ViewType: data.PodCliqueScalingGroupReplicaView, SelectedScalingGroup: "my-sg", SelectedPCSGReplicaIndex: "0"}, "PodCliqueScalingGroupReplica/my-sg/0"},
		{data.ViewState{ViewType: data.PodCliqueView, SelectedPodClique: "my-pc"}, "PodClique/my-pc"},
		{data.ViewState{ViewType: data.PodView}, ""},
	}

	for _, tt := range tests {
		m.viewState = tt.viewState
		got := m.getCurrentViewKey()
		if got != tt.expected {
			t.Errorf("getCurrentViewKey() for viewType=%s: expected %q, got %q", data.ViewTypeName(tt.viewState.ViewType), tt.expected, got)
		}
	}
}

func TestEnterOnEventsPane_DoesNothing(t *testing.T) {
	m := newTestModel(samplePCSResources())

	// Switch to events pane
	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.EventsPane {
		t.Fatal("expected events pane")
	}

	// Enter on events pane should not navigate
	before := m.viewState.ViewType
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.ViewType != before {
		t.Fatalf("expected no navigation on Enter in events pane, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestBreadcrumbRendering(t *testing.T) {
	m := newTestModel(nil)

	// ForestView breadcrumb
	m.viewState = data.ViewState{ViewType: data.ForestView}
	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, "Forest") {
		t.Errorf("expected ForestView breadcrumb to contain 'Forest', got %q", bc)
	}

	// PodCliqueSetView breadcrumb
	m.viewState = data.ViewState{ViewType: data.PodCliqueSetView, SelectedPodCliqueSet: "my-pcs"}
	bc = m.renderBreadcrumb()
	if !strings.Contains(bc, "Forest") || !strings.Contains(bc, "my-pcs") {
		t.Errorf("expected PodCliqueSetView breadcrumb to contain 'Forest' and 'my-pcs', got %q", bc)
	}

	// PodCliqueSetReplicaView breadcrumb
	m.viewState = data.ViewState{
		ViewType:             data.PodCliqueSetReplicaView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "2",
	}
	bc = m.renderBreadcrumb()
	if !strings.Contains(bc, "replica-2") {
		t.Errorf("expected breadcrumb to contain 'replica-2', got %q", bc)
	}

	// PodView breadcrumb
	m.viewState = data.ViewState{
		ViewType:             data.PodView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "0",
		SelectedPodClique:    "my-pc",
		SelectedPod:          "my-pod-0",
	}
	bc = m.renderBreadcrumb()
	if !strings.Contains(bc, "my-pod-0") {
		t.Errorf("expected PodView breadcrumb to contain 'my-pod-0', got %q", bc)
	}
}

func TestHeaderContainsShortcuts(t *testing.T) {
	m := newTestModel(samplePCSResources())

	// Shortcuts should appear in the header
	header := m.renderHeaderFrame()
	expectedShortcuts := []string{"Filter", "Switch", "Quit"}
	for _, s := range expectedShortcuts {
		if !strings.Contains(header, s) {
			t.Errorf("expected header to contain %q, got:\n%s", s, header)
		}
	}
}

func TestHeaderShowsBackWhenNotAtForest(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.ViewType = data.PodCliqueSetView

	header := m.renderHeaderFrame()
	if !strings.Contains(header, "Back") {
		t.Errorf("expected header to contain 'Back' when not at ForestView, got:\n%s", header)
	}
}

func TestHeaderHidesBackAtForestView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.ViewType = data.ForestView

	header := m.renderHeaderFrame()
	if strings.Contains(header, "Back") {
		t.Errorf("expected header NOT to contain 'Back' at ForestView, got:\n%s", header)
	}
}

func TestHeaderShowsScrollInPodView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.ViewType = data.PodView

	header := m.renderHeaderFrame()
	if !strings.Contains(header, "Scroll") {
		t.Errorf("expected header to contain 'Scroll' at PodView, got:\n%s", header)
	}
	if strings.Contains(header, "Drill") {
		t.Errorf("expected header NOT to contain 'Drill' at PodView, got:\n%s", header)
	}
}

func TestHeaderShowsClusterInfo(t *testing.T) {
	mc := data.NewMockGlobalCache()
	m := NewModel(mc,
		WithClusterInfo("my-test-context", "my-test-cluster"),
		WithUserName("admin@my-test-cluster"),
		WithK8sVersion("v1.33.5+k3s1"),
		WithArboristVersion("v0.1.0"),
	)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	header := m.renderHeaderFrame()

	if !strings.Contains(header, "my-test-context") {
		t.Errorf("expected header to contain context name 'my-test-context', got:\n%s", header)
	}
	if !strings.Contains(header, "my-test-cluster") {
		t.Errorf("expected header to contain cluster name 'my-test-cluster', got:\n%s", header)
	}
	if !strings.Contains(header, "admin@my-test-cluster") {
		t.Errorf("expected header to contain user name 'admin@my-test-cluster', got:\n%s", header)
	}
	if !strings.Contains(header, "v1.33.5+k3s1") {
		t.Errorf("expected header to contain K8s version 'v1.33.5+k3s1', got:\n%s", header)
	}
	if !strings.Contains(header, "v0.1.0") {
		t.Errorf("expected header to contain Arborist version 'v0.1.0', got:\n%s", header)
	}
	if !strings.Contains(header, "Forest") {
		t.Errorf("expected header to contain view name 'Forest', got:\n%s", header)
	}
	for _, label := range []string{"Context:", "Cluster:", "User:", "Arborist Rev:", "K8s Rev:", "View:"} {
		if !strings.Contains(header, label) {
			t.Errorf("expected header to contain '%s' label, got:\n%s", label, header)
		}
	}
}

func TestHeaderShowsUnknownWhenNoClusterInfo(t *testing.T) {
	m := newTestModel(nil)

	header := m.renderHeaderFrame()

	count := strings.Count(header, "(unknown)")
	if count < 4 {
		t.Errorf("expected at least 4 '(unknown)' entries when no info set, got %d in:\n%s", count, header)
	}
}

func TestHeaderShowsCurrentViewName(t *testing.T) {
	m := newTestModel(nil)

	m.viewState.ViewType = data.PodCliqueSetView
	header := m.renderHeaderFrame()
	if !strings.Contains(header, "PodCliqueSet") {
		t.Errorf("expected header to show 'PodCliqueSet' view name, got:\n%s", header)
	}

	m.viewState.ViewType = data.PodView
	header = m.renderHeaderFrame()
	if !strings.Contains(header, "Pod") {
		t.Errorf("expected header to show 'Pod' view name, got:\n%s", header)
	}
}

// ---------------------------------------------------------------------------
// Topology View Tests
// ---------------------------------------------------------------------------

// sampleTopologyViewData creates a TopologyViewData snapshot for testing.
func sampleTopologyViewData() *data.TopologyViewData {
	return &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
			{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
			{Domain: "rack", Key: "topology.kubernetes.io/rack", ValuesCount: 6},
		},
		NodeLabels: map[string]map[string]string{
			"node-01": {
				"topology.kubernetes.io/region": "us-east-1",
				"topology.kubernetes.io/zone":   "us-east-1a",
				"topology.kubernetes.io/rack":   "rack-01",
			},
			"node-02": {
				"topology.kubernetes.io/region": "us-east-1",
				"topology.kubernetes.io/zone":   "us-east-1a",
				"topology.kubernetes.io/rack":   "rack-02",
			},
			"node-03": {
				"topology.kubernetes.io/region": "us-east-1",
				"topology.kubernetes.io/zone":   "us-east-1b",
				"topology.kubernetes.io/rack":   "rack-03",
			},
			"node-04": {
				"topology.kubernetes.io/region": "us-west-2",
				"topology.kubernetes.io/zone":   "us-west-2a",
				"topology.kubernetes.io/rack":   "rack-04",
			},
			"node-05": {
				"topology.kubernetes.io/region": "us-west-2",
				"topology.kubernetes.io/zone":   "us-west-2a",
				"topology.kubernetes.io/rack":   "rack-05",
			},
			"node-06": {
				"topology.kubernetes.io/region": "us-west-2",
				"topology.kubernetes.io/zone":   "us-west-2b",
				"topology.kubernetes.io/rack":   "rack-06",
			},
		},
		Pods: []data.TopologyViewPod{
			{Namespace: "default", Node: "node-01", Name: "pod-a", Topology: "rack: rack-01", Phase: "Running"},
			{Namespace: "default", Node: "node-02", Name: "pod-b", Topology: "rack: rack-02", Phase: "Running"},
			{Namespace: "default", Node: "node-03", Name: "pod-c", Topology: "rack: rack-03", Phase: "Running"},
			{Namespace: "default", Node: "node-04", Name: "pod-d", Topology: "rack: rack-04", Phase: "Running"},
			{Namespace: "default", Node: "node-05", Name: "pod-e", Topology: "rack: rack-05", Phase: "Pending"},
			{Namespace: "default", Node: "node-06", Name: "pod-f", Topology: "rack: rack-06", Phase: "Running"},
		},
		DomainToKey: map[string]string{
			"region": "topology.kubernetes.io/region",
			"zone":   "topology.kubernetes.io/zone",
			"rack":   "topology.kubernetes.io/rack",
		},
	}
}

// newTopologyTestModel creates a Model with a MockGlobalCache pre-loaded
// with topology data. It simulates toggling to Topology view.
func newTopologyTestModel() Model {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})

	// Toggle to Topology view
	m = sendRune(m, 't')

	return m
}

func TestTopologyView_ToggleForestToTopology(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})

	// Initially in Forest view
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Press 't' to toggle to Topology view
	m = sendRune(m, 't')

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView after pressing t, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.activePane != data.TopologyDomainsPane {
		t.Fatalf("expected TopologyDomainsPane, got %s", data.PaneName(m.activePane))
	}

	// After toggle, view data should be populated from snapshot
	if m.topologyViewData == nil {
		t.Fatal("expected topologyViewData to be set from snapshot")
	}

	// View should show Topology
	assertView(t, m, []string{"Topology"})
}

func TestTopologyView_ToggleTopologyToForest(t *testing.T) {
	m := newTopologyTestModel()

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Press 't' to toggle back to Forest
	m = sendRune(m, 't')

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after toggling back, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.activePane != data.ResourcesPane {
		t.Fatalf("expected ResourcesPane after toggling to Forest, got %s", data.PaneName(m.activePane))
	}
}

func TestTopologyView_DomainTableShowsCorrectDomains(t *testing.T) {
	m := newTopologyTestModel()

	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 domain rows (region, zone, rack), got %d", len(rows))
	}

	expectedDomains := []string{"region", "zone", "rack"}
	for i, expected := range expectedDomains {
		if rows[i][0] != expected {
			t.Errorf("row %d: expected domain %q, got %q", i, expected, rows[i][0])
		}
	}
}

func TestTopologyView_DrillIntoDomain(t *testing.T) {
	m := newTopologyTestModel()

	// Cursor is on first row (region). Press Enter to drill in.
	m = sendKey(m, tea.KeyEnter)

	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected drill stack depth 1, got %d", len(m.topologyDrillStack))
	}
	if m.topologyDrillStack[0].Domain != "region" {
		t.Fatalf("expected drill into 'region', got %q", m.topologyDrillStack[0].Domain)
	}
	if m.topologyDrillStack[0].Value != "" {
		t.Fatalf("expected empty value (showing values list), got %q", m.topologyDrillStack[0].Value)
	}

	// Domains table should now show distinct region values
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 region values (us-east-1, us-west-2), got %d", len(rows))
	}

	if rows[0][0] != "us-east-1" {
		t.Errorf("expected first value 'us-east-1', got %q", rows[0][0])
	}
	if rows[1][0] != "us-west-2" {
		t.Errorf("expected second value 'us-west-2', got %q", rows[1][0])
	}
}

func TestTopologyView_DrillIntoValueAdvancesToNextDomain(t *testing.T) {
	m := newTopologyTestModel()

	// Drill into region
	m = sendKey(m, tea.KeyEnter)

	// Select first value (us-east-1) and drill in
	m = sendKey(m, tea.KeyEnter)

	if len(m.topologyDrillStack) != 2 {
		t.Fatalf("expected drill stack depth 2, got %d", len(m.topologyDrillStack))
	}
	if m.topologyDrillStack[0].Value != "us-east-1" {
		t.Fatalf("expected first stack entry value='us-east-1', got %q", m.topologyDrillStack[0].Value)
	}
	if m.topologyDrillStack[1].Domain != "zone" {
		t.Fatalf("expected second stack entry domain='zone', got %q", m.topologyDrillStack[1].Domain)
	}

	// Should show zone values scoped to us-east-1
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 zone values for us-east-1, got %d", len(rows))
	}
	if rows[0][0] != "us-east-1a" {
		t.Errorf("expected first zone value 'us-east-1a', got %q", rows[0][0])
	}
	if rows[1][0] != "us-east-1b" {
		t.Errorf("expected second zone value 'us-east-1b', got %q", rows[1][0])
	}
}

func TestTopologyView_EscGoesBackOneDrillLevel(t *testing.T) {
	m := newTopologyTestModel()

	m = sendKey(m, tea.KeyEnter) // drill into region
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected drill stack depth 1, got %d", len(m.topologyDrillStack))
	}

	m = sendKey(m, tea.KeyEsc) // go back
	if len(m.topologyDrillStack) != 0 {
		t.Fatalf("expected drill stack to be empty after Esc, got depth %d", len(m.topologyDrillStack))
	}

	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 domain rows after Esc, got %d", len(rows))
	}
}

func TestTopologyView_EscFromEmptyDrillStackSwitchesToForest(t *testing.T) {
	m := newTopologyTestModel()

	if len(m.topologyDrillStack) != 0 {
		t.Fatalf("expected empty drill stack, got depth %d", len(m.topologyDrillStack))
	}

	m = sendKey(m, tea.KeyEsc)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc at top-level Topology, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestTopologyView_PodsTableFiltersCorrectly(t *testing.T) {
	m := newTopologyTestModel()

	// At top-level, pods table should be empty
	podRows := m.topologyPodsTable.Rows()
	if len(podRows) != 0 {
		t.Fatalf("expected 0 pods at top level (before drilling), got %d", len(podRows))
	}

	// Drill into region
	m = sendKey(m, tea.KeyEnter)

	// Cursor defaults to us-east-1 => 3 pods
	podRows = m.topologyPodsTable.Rows()
	if len(podRows) != 3 {
		t.Fatalf("expected 3 pods when highlighting us-east-1 value, got %d", len(podRows))
	}

	// Move cursor down to us-west-2 => 3 pods
	m = sendKey(m, tea.KeyDown)
	podRows = m.topologyPodsTable.Rows()
	if len(podRows) != 3 {
		t.Fatalf("expected 3 pods when highlighting us-west-2 value, got %d", len(podRows))
	}

	// Move cursor back up to us-east-1 and select it → advance to zone values
	m = sendKey(m, tea.KeyUp)
	m = sendKey(m, tea.KeyEnter)

	// Highlighting us-east-1a filters to 2 pods
	podRows = m.topologyPodsTable.Rows()
	if len(podRows) != 2 {
		t.Fatalf("expected 2 pods when highlighting zone us-east-1a, got %d", len(podRows))
	}

	// Move to us-east-1b => 1 pod
	m = sendKey(m, tea.KeyDown)
	podRows = m.topologyPodsTable.Rows()
	if len(podRows) != 1 {
		t.Fatalf("expected 1 pod when highlighting zone us-east-1b, got %d", len(podRows))
	}
	if len(podRows[0]) >= 3 && podRows[0][2] != "pod-c" {
		t.Errorf("expected pod-c for zone us-east-1b, got %q", podRows[0][2])
	}
}

func TestTopologyView_TabSwitchesBetweenPanes(t *testing.T) {
	m := newTopologyTestModel()

	if m.activePane != data.TopologyDomainsPane {
		t.Fatalf("expected TopologyDomainsPane, got %s", data.PaneName(m.activePane))
	}
	if !m.topologyDomainsTable.Focused() {
		t.Fatal("expected topology domains table to be focused")
	}

	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.TopologyPodsPane {
		t.Fatalf("expected TopologyPodsPane after Tab, got %s", data.PaneName(m.activePane))
	}
	if !m.topologyPodsTable.Focused() {
		t.Fatal("expected topology pods table to be focused after Tab")
	}

	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.TopologyDomainsPane {
		t.Fatalf("expected TopologyDomainsPane after second Tab, got %s", data.PaneName(m.activePane))
	}
}

func TestTopologyView_NoNADomainRow(t *testing.T) {
	m := newTopologyTestModel()

	rows := m.topologyDomainsTable.Rows()
	for i, row := range rows {
		if len(row) >= 1 && row[0] == "N/A" {
			t.Errorf("row %d: unexpected N/A domain row in topology domains table", i)
		}
	}
}

func TestTopologyView_DeepDrillDown(t *testing.T) {
	m := newTopologyTestModel()

	m = sendKey(m, tea.KeyEnter) // into region
	m = sendKey(m, tea.KeyEnter) // select us-east-1, advance to zone
	m = sendKey(m, tea.KeyEnter) // select us-east-1a, advance to rack

	// Stack: [{region, "us-east-1"}, {zone, "us-east-1a"}, {rack, ""}]
	if len(m.topologyDrillStack) != 3 {
		t.Fatalf("expected drill stack depth 3, got %d", len(m.topologyDrillStack))
	}

	// Should show rack values for us-east-1/us-east-1a: rack-01, rack-02
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rack values, got %d", len(rows))
	}

	// Esc 1: pop rack (empty value), clear zone value
	// Stack → [{region, "us-east-1"}, {zone, ""}]
	// Shows zone values for us-east-1
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 2 {
		t.Fatalf("expected depth 2 after Esc 1, got %d", len(m.topologyDrillStack))
	}
	if m.topologyDrillStack[1].Value != "" {
		t.Fatalf("expected zone value cleared after Esc 1, got %q", m.topologyDrillStack[1].Value)
	}
	// Top table should now show zone values (not rack values)
	rows = m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 zone values for us-east-1 after Esc 1, got %d", len(rows))
	}
	if rows[0][0] != "us-east-1a" {
		t.Errorf("expected first zone value 'us-east-1a', got %q", rows[0][0])
	}

	// Esc 2: pop zone (empty value), clear region value
	// Stack → [{region, ""}]
	// Shows region values
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected depth 1 after Esc 2, got %d", len(m.topologyDrillStack))
	}
	if m.topologyDrillStack[0].Value != "" {
		t.Fatalf("expected region value cleared after Esc 2, got %q", m.topologyDrillStack[0].Value)
	}

	// Esc 3: pop region (empty value)
	// Stack → []
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 0 {
		t.Fatalf("expected empty drill stack after Esc 3, got depth %d", len(m.topologyDrillStack))
	}

	// Esc 4: switches to Forest
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestTopologyView_NarrowestDomainEnterIsNoop(t *testing.T) {
	m := newTopologyTestModel()

	m = sendKey(m, tea.KeyEnter) // into region
	m = sendKey(m, tea.KeyEnter) // us-east-1 -> zone
	m = sendKey(m, tea.KeyEnter) // us-east-1a -> rack

	stackDepthAtRack := len(m.topologyDrillStack)

	m = sendKey(m, tea.KeyEnter) // noop at narrowest domain

	if len(m.topologyDrillStack) != stackDepthAtRack {
		t.Fatalf("expected drill stack depth unchanged at narrowest domain, was %d, got %d",
			stackDepthAtRack, len(m.topologyDrillStack))
	}
}

func TestTopologyView_TogglePreservesDrillStackOnReturn(t *testing.T) {
	m := newTopologyTestModel()

	m = sendKey(m, tea.KeyEnter) // drill into region
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected drill stack depth 1, got %d", len(m.topologyDrillStack))
	}

	// Toggle to Forest
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Toggle back to Topology — drill stack should be reset
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.topologyDrillStack) != 0 {
		t.Fatalf("expected drill stack reset on toggle back, got depth %d", len(m.topologyDrillStack))
	}
}

func TestTopologyView_ViewShowsTopologyHeader(t *testing.T) {
	m := newTopologyTestModel()

	view := m.View()
	if !strings.Contains(view, "Topology") {
		t.Errorf("expected view to contain 'Topology', got:\n%s", view)
	}
	if !strings.Contains(view, "Topology Domains") {
		t.Errorf("expected view to contain 'Topology Domains', got:\n%s", view)
	}
	if !strings.Contains(view, "Pods") {
		t.Errorf("expected view to contain 'Pods', got:\n%s", view)
	}
}

func TestTopologyView_WindowResizeUpdatesTopologyTables(t *testing.T) {
	m := newTopologyTestModel()

	m = mustApply(m, tea.WindowSizeMsg{Width: 200, Height: 60})

	view := m.View()
	if view == "" || view == "Loading..." {
		t.Fatal("expected rendered view after resize")
	}
	if !strings.Contains(view, "Topology") {
		t.Error("expected 'Topology' in resized view")
	}
}

func TestTopologyView_ArrowKeysNavigateDomains(t *testing.T) {
	m := newTopologyTestModel()

	selectedRow := m.topologyDomainsTable.SelectedRow()
	if len(selectedRow) < 1 || selectedRow[0] != "region" {
		t.Fatalf("expected initial selection on 'region', got %v", selectedRow)
	}

	m = sendKey(m, tea.KeyDown)
	selectedRow = m.topologyDomainsTable.SelectedRow()
	if len(selectedRow) < 1 || selectedRow[0] != "zone" {
		t.Fatalf("expected selection on 'zone' after down arrow, got %v", selectedRow)
	}

	m = sendKey(m, tea.KeyDown)
	selectedRow = m.topologyDomainsTable.SelectedRow()
	if len(selectedRow) < 1 || selectedRow[0] != "rack" {
		t.Fatalf("expected selection on 'rack' after down arrow, got %v", selectedRow)
	}
}

func TestTopologyView_EnterOnPodsPane_IsNoop(t *testing.T) {
	m := newTopologyTestModel()

	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.TopologyPodsPane {
		t.Fatalf("expected TopologyPodsPane, got %s", data.PaneName(m.activePane))
	}

	beforeView := m.viewState.ViewType
	beforeDrillLen := len(m.topologyDrillStack)
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != beforeView {
		t.Fatalf("expected view unchanged, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.topologyDrillStack) != beforeDrillLen {
		t.Fatalf("expected drill stack unchanged, got depth %d", len(m.topologyDrillStack))
	}
}

func TestTopologyView_NilSnapshotToggleDoesNotPanic(t *testing.T) {
	mc := data.NewMockGlobalCache()
	// No topology view data in snapshot
	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})

	// Press 't' — should toggle view
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView even without topology data, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Toggle back should work
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestTopologyView_EmptySnapshot(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = &data.TopologyViewData{
		Domains:     []data.TopologyDomainRow{},
		NodeLabels:  map[string]map[string]string{},
		Pods:        []data.TopologyViewPod{},
		DomainToKey: map[string]string{},
	}
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})

	// Toggle to topology
	m = sendRune(m, 't')

	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 0 {
		t.Fatalf("expected 0 domain rows, got %d", len(rows))
	}
	podRows := m.topologyPodsTable.Rows()
	if len(podRows) != 0 {
		t.Fatalf("expected 0 pod rows, got %d", len(podRows))
	}
}

func TestTopologyView_BreadcrumbString(t *testing.T) {
	m := newTopologyTestModel()

	bc := m.topologyBreadcrumbString()
	if bc != "" {
		t.Errorf("expected empty breadcrumb at top level, got %q", bc)
	}

	m = sendKey(m, tea.KeyEnter) // drill into region
	bc = m.topologyBreadcrumbString()
	if bc != "" {
		t.Errorf("expected empty breadcrumb when no value selected, got %q", bc)
	}

	m = sendKey(m, tea.KeyEnter) // select us-east-1
	bc = m.topologyBreadcrumbString()
	if !strings.Contains(bc, "region=us-east-1") {
		t.Errorf("expected breadcrumb to contain 'region=us-east-1', got %q", bc)
	}

	m = sendKey(m, tea.KeyEnter) // select us-east-1a, advance to rack
	bc = m.topologyBreadcrumbString()
	if !strings.Contains(bc, "region=us-east-1") {
		t.Errorf("expected breadcrumb to contain 'region=us-east-1', got %q", bc)
	}
	if !strings.Contains(bc, "zone=us-east-1a") {
		t.Errorf("expected breadcrumb to contain 'zone=us-east-1a', got %q", bc)
	}
}

func TestTopologyView_MovingDomainCursorUpdatesPods(t *testing.T) {
	m := newTopologyTestModel()

	podRows := m.topologyPodsTable.Rows()
	if len(podRows) != 0 {
		t.Fatalf("expected 0 pods at top level, got %d", len(podRows))
	}

	m = sendKey(m, tea.KeyDown)
	podRows = m.topologyPodsTable.Rows()
	if len(podRows) != 0 {
		t.Fatalf("expected 0 pods at top level on 'zone', got %d", len(podRows))
	}
}

func TestTopologyView_SecondToggleUsesWarmCache(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})

	// First toggle: to topology
	m = sendRune(m, 't')
	if m.topologyViewData == nil {
		t.Fatal("expected topologyViewData set after first toggle")
	}

	// Toggle back to Forest
	m = sendRune(m, 't')

	// Toggle to Topology again — data should already be populated
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.topologyViewData == nil {
		t.Fatal("expected topologyViewData to be set from warm cache")
	}
}

// ---------------------------------------------------------------------------
// Command Mode Tests (vim-style ":" lens switching)
// ---------------------------------------------------------------------------

func TestCommandMode_ActivateWithColon(t *testing.T) {
	m := newTestModel(samplePCSResources())

	if m.commandActive {
		t.Fatal("expected command mode inactive initially")
	}

	m = sendRune(m, ':')
	if !m.commandActive {
		t.Fatal("expected command mode active after pressing :")
	}
}

func TestCommandMode_EscCancels(t *testing.T) {
	m := newTestModel(samplePCSResources())

	m = sendRune(m, ':')
	if !m.commandActive {
		t.Fatal("expected command mode active")
	}

	m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})

	m = sendKey(m, tea.KeyEsc)
	if m.commandActive {
		t.Fatal("expected command mode deactivated after Esc")
	}
	if m.commandInput.Value() != "" {
		t.Fatalf("expected command input cleared after Esc, got %q", m.commandInput.Value())
	}
}

func TestCommandMode_EnterExecutesTopology(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	m = sendRune(m, ':')
	for _, r := range "topology" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyEnter)

	if m.commandActive {
		t.Fatal("expected command mode deactivated after Enter")
	}
	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView after :topology, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestCommandMode_PrefixMatchTopology(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "top" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView after :top (prefix match), got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestCommandMode_PrefixMatchForest(t *testing.T) {
	m := newTopologyTestModel()

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	m = sendRune(m, ':')
	for _, r := range "for" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after :for (prefix match), got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestCommandMode_TabCompletion(t *testing.T) {
	m := newTestModel(samplePCSResources())

	m = sendRune(m, ':')
	for _, r := range "top" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.commandInput.Value() != "top" {
		t.Fatalf("expected command input 'top', got %q", m.commandInput.Value())
	}

	m = sendKey(m, tea.KeyTab)
	if m.commandInput.Value() != "topology" {
		t.Fatalf("expected command input 'topology' after Tab, got %q", m.commandInput.Value())
	}
}

func TestCommandMode_TabCompletionForest(t *testing.T) {
	m := newTestModel(samplePCSResources())

	m = sendRune(m, ':')
	for _, r := range "for" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyTab)
	if m.commandInput.Value() != "forest" {
		t.Fatalf("expected command input 'forest' after Tab, got %q", m.commandInput.Value())
	}
}

func TestCommandMode_NoMatchDoesNothing(t *testing.T) {
	m := newTestModel(samplePCSResources())

	viewBefore := m.viewState.ViewType

	m = sendRune(m, ':')
	for _, r := range "xyz" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != viewBefore {
		t.Fatalf("expected view unchanged after unmatched command, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.commandActive {
		t.Fatal("expected command mode deactivated after Enter (even with no match)")
	}
}

func TestCommandMode_EmptyInputDoesNothing(t *testing.T) {
	m := newTestModel(samplePCSResources())

	viewBefore := m.viewState.ViewType

	m = sendRune(m, ':')
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != viewBefore {
		t.Fatalf("expected view unchanged after empty command, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestCommandMode_CtrlCQuitsFromCommandMode(t *testing.T) {
	m := newTestModel(samplePCSResources())
	m = sendRune(m, ':')
	if !m.commandActive {
		t.Fatal("expected command active")
	}

	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command even in command mode")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestCommandMode_ViewShowsCommandBar(t *testing.T) {
	m := newTestModel(samplePCSResources())

	m = sendRune(m, ':')

	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view with command bar")
	}
}

func TestCommandMode_HeaderShowsCmdShortcut(t *testing.T) {
	m := newTestModel(samplePCSResources())

	header := m.renderHeaderFrame()
	if !strings.Contains(header, "Cmd") {
		t.Errorf("expected header to contain 'Cmd' shortcut, got:\n%s", header)
	}
}

func TestLensCommandNames(t *testing.T) {
	names := LensCommandNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 lens commands, got %d", len(names))
	}
	expected := map[string]bool{"forest": true, "topology": true}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected lens command name: %q", n)
		}
	}
}

func TestMatchLensCommand(t *testing.T) {
	tests := []struct {
		prefix    string
		wantName  string
		wantMatch bool
	}{
		{"topology", "topology", true},
		{"top", "topology", true},
		{"t", "topology", true},
		{"forest", "forest", true},
		{"for", "forest", true},
		{"f", "forest", true},
		{"xyz", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		name, ok := matchLensCommand(tt.prefix)
		if ok != tt.wantMatch {
			t.Errorf("matchLensCommand(%q): got ok=%v, want %v", tt.prefix, ok, tt.wantMatch)
		}
		if name != tt.wantName {
			t.Errorf("matchLensCommand(%q): got name=%q, want %q", tt.prefix, name, tt.wantName)
		}
	}
}

func TestCompleteLensCommand(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{"top", "topology"},
		{"topology", "topology"},
		{"for", "forest"},
		{"forest", "forest"},
		{"f", "forest"},
		{"t", "topology"},
		{"xyz", "xyz"},
		{"", ""},
	}

	for _, tt := range tests {
		got := completeLensCommand(tt.prefix)
		if got != tt.want {
			t.Errorf("completeLensCommand(%q): got %q, want %q", tt.prefix, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Topology GPU Column Tests
// ---------------------------------------------------------------------------

// sampleTopologyViewDataWithGPU creates topology data with GPU capacity/products.
func sampleTopologyViewDataWithGPU() *data.TopologyViewData {
	return &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "block", Key: "topology.io/block", ValuesCount: 2},
			{Domain: "rack", Key: "topology.io/rack", ValuesCount: 4},
		},
		NodeLabels: map[string]map[string]string{
			"node-1": {"topology.io/block": "block-01", "topology.io/rack": "rack-01"},
			"node-2": {"topology.io/block": "block-01", "topology.io/rack": "rack-02"},
			"node-3": {"topology.io/block": "block-02", "topology.io/rack": "rack-03"},
			"node-4": {"topology.io/block": "block-02", "topology.io/rack": "rack-04"},
		},
		Pods: []data.TopologyViewPod{
			{Namespace: "default", Node: "node-1", Name: "pod-a", Topology: "rack: rack-01", Phase: "Running"},
			{Namespace: "default", Node: "node-2", Name: "pod-b", Topology: "rack: rack-02", Phase: "Running"},
			{Namespace: "default", Node: "node-3", Name: "pod-c", Topology: "rack: rack-03", Phase: "Running"},
		},
		DomainToKey: map[string]string{
			"block": "topology.io/block",
			"rack":  "topology.io/rack",
		},
		NodeGPUProducts: map[string]string{
			"node-1": "H200",
			"node-2": "H200",
			"node-3": "B200",
			"node-4": "B200",
		},
		NodeGPUCapacity: map[string]int64{
			"node-1": 8,
			"node-2": 8,
			"node-3": 8,
			"node-4": 8,
		},
		RawPods: []data.TopologyPodInput{
			{Name: "pod-a", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{}},
			{Name: "pod-b", NodeName: "node-2", GPURequests: 2, Labels: map[string]string{}},
			{Name: "pod-c", NodeName: "node-3", GPURequests: 8, Labels: map[string]string{}},
		},
	}
}

func newTopologyGPUTestModel() Model {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewDataWithGPU()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})

	// Toggle to Topology view
	m = sendRune(m, 't')
	return m
}

func TestTopologyView_GPUColumnsAppearWhenDrilledIn(t *testing.T) {
	m := newTopologyGPUTestModel()

	// At root level, domains table should have 3 columns (DOMAIN, KEY, VALUES)
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 domain rows, got %d", len(rows))
	}
	if len(rows[0]) != 3 {
		t.Fatalf("expected 3 columns at root level (DOMAIN/KEY/VALUES), got %d", len(rows[0]))
	}

	// Drill into "block"
	m = sendKey(m, tea.KeyEnter)

	// Should now show block values with GPU¹ + GPU PODS + PODS
	rows = m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 block values (block-01, block-02), got %d", len(rows))
	}

	// Column order: VALUE + B200¹ + H200¹ + GPU PODS + PODS = 5 columns
	if len(rows[0]) != 5 {
		t.Fatalf("expected 5 columns (VALUE + B200¹ + H200¹ + GPU PODS + PODS), got %d cols: %v", len(rows[0]), rows[0])
	}

	// block-01 has H200 nodes: node-1 (pod-a GPU=4), node-2 (pod-b GPU=2)
	// All pods have empty Labels → Other (no part-of label)
	if rows[0][0] != "block-01" {
		t.Errorf("expected first value 'block-01', got %q", rows[0][0])
	}
	if rows[0][1] != "0/0/0" {
		t.Errorf("block-01 B200¹ = %q, want '0/0/0'", rows[0][1])
	}
	if rows[0][2] != "0/6/16" {
		t.Errorf("block-01 H200¹ = %q, want '0/6/16'", rows[0][2])
	}
	if rows[0][3] != "2" {
		t.Errorf("block-01 GPU PODS = %q, want '2'", rows[0][3])
	}
	if rows[0][4] != "2" {
		t.Errorf("block-01 PODS = %q, want '2'", rows[0][4])
	}

	// block-02 has B200 nodes: node-3 (pod-c GPU=8), node-4 (no pods)
	// pod-c has empty Labels → Other
	if rows[1][0] != "block-02" {
		t.Errorf("expected second value 'block-02', got %q", rows[1][0])
	}
	if rows[1][1] != "0/8/16" {
		t.Errorf("block-02 B200¹ = %q, want '0/8/16'", rows[1][1])
	}
	if rows[1][2] != "0/0/0" {
		t.Errorf("block-02 H200¹ = %q, want '0/0/0'", rows[1][2])
	}
	if rows[1][3] != "1" {
		t.Errorf("block-02 GPU PODS = %q, want '1'", rows[1][3])
	}
	if rows[1][4] != "1" {
		t.Errorf("block-02 PODS = %q, want '1'", rows[1][4])
	}
}

func TestTopologyView_GPUColumnsNotShownAtRootLevel(t *testing.T) {
	m := newTopologyGPUTestModel()

	// At root level, no GPU columns
	rows := m.topologyDomainsTable.Rows()
	for _, row := range rows {
		if len(row) != 3 {
			t.Errorf("expected 3 columns at root level, got %d: %v", len(row), row)
		}
	}
}

func TestTopologyView_NoGPUNodes_HasPodColumnsOnly(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	// Topology data without GPU info
	tvd := sampleTopologyViewData()
	tvd.NodeGPUProducts = map[string]string{}
	tvd.NodeGPUCapacity = map[string]int64{}
	tvd.RawPods = []data.TopologyPodInput{}
	snap.TopologyViewData = tvd
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	m = sendRune(m, 't')

	// Drill into region
	m = sendKey(m, tea.KeyEnter)

	// Should have VALUE + GPU PODS + PODS = 3 columns (no GPU USAGE columns)
	rows := m.topologyDomainsTable.Rows()
	if len(rows) > 0 && len(rows[0]) != 3 {
		t.Errorf("expected 3 columns (VALUE + GPU PODS + PODS) when no GPU nodes, got %d: %v", len(rows[0]), rows[0])
	}
}

func TestTopologyView_PodCountColumnsAppearWhenDrilledIn(t *testing.T) {
	// Build topology data with a mix of GPU and regular pods
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "block", Key: "topology.io/block", ValuesCount: 2},
		},
		NodeLabels: map[string]map[string]string{
			"node-1": {"topology.io/block": "block-01"},
			"node-2": {"topology.io/block": "block-01"},
			"node-3": {"topology.io/block": "block-02"},
		},
		Pods: []data.TopologyViewPod{
			{Namespace: "default", Node: "node-1", Name: "gpu-pod-1", Phase: "Running"},
			{Namespace: "default", Node: "node-1", Name: "reg-pod-1", Phase: "Running"},
			{Namespace: "default", Node: "node-2", Name: "gpu-pod-2", Phase: "Running"},
			{Namespace: "default", Node: "node-3", Name: "reg-pod-2", Phase: "Running"},
			{Namespace: "default", Node: "node-3", Name: "reg-pod-3", Phase: "Running"},
		},
		DomainToKey: map[string]string{"block": "topology.io/block"},
		RawPods: []data.TopologyPodInput{
			{Name: "gpu-pod-1", NodeName: "node-1", GPURequests: 4},
			{Name: "reg-pod-1", NodeName: "node-1", GPURequests: 0},
			{Name: "gpu-pod-2", NodeName: "node-2", GPURequests: 2},
			{Name: "reg-pod-2", NodeName: "node-3", GPURequests: 0},
			{Name: "reg-pod-3", NodeName: "node-3", GPURequests: 0},
		},
		NodeGPUProducts: map[string]string{},
		NodeGPUCapacity: map[string]int64{},
	}
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	m = sendRune(m, 't')

	// At root level, no pod count columns
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 domain row, got %d", len(rows))
	}
	if len(rows[0]) != 3 {
		t.Fatalf("expected 3 columns at root (DOMAIN/KEY/VALUES), got %d", len(rows[0]))
	}

	// Drill into "block"
	m = sendKey(m, tea.KeyEnter)

	rows = m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 block values, got %d", len(rows))
	}

	// Column order: VALUE + GPU PODS + PODS = 3 columns (no GPU USAGE since no GPU products)
	if len(rows[0]) != 3 {
		t.Fatalf("expected 3 columns, got %d: %v", len(rows[0]), rows[0])
	}

	// block-01: gpu-pod-1, reg-pod-1, gpu-pod-2 → GPU=2, Total=3
	if rows[0][0] != "block-01" {
		t.Errorf("first value = %q, want 'block-01'", rows[0][0])
	}
	if rows[0][1] != "2" {
		t.Errorf("block-01 GPU PODS = %q, want '2'", rows[0][1])
	}
	if rows[0][2] != "3" {
		t.Errorf("block-01 PODS = %q, want '3'", rows[0][2])
	}

	// block-02: reg-pod-2, reg-pod-3 → GPU=0, Total=2
	if rows[1][0] != "block-02" {
		t.Errorf("second value = %q, want 'block-02'", rows[1][0])
	}
	if rows[1][1] != "0" {
		t.Errorf("block-02 GPU PODS = %q, want '0'", rows[1][1])
	}
	if rows[1][2] != "2" {
		t.Errorf("block-02 PODS = %q, want '2'", rows[1][2])
	}
}

func TestTopologyView_PodCountColumnsNotAtRootLevel(t *testing.T) {
	m := newTopologyGPUTestModel()

	// At root level, columns are DOMAIN, KEY, VALUES — no PODS columns
	rows := m.topologyDomainsTable.Rows()
	for _, row := range rows {
		if len(row) != 3 {
			t.Errorf("expected 3 columns at root level, got %d: %v", len(row), row)
		}
	}
}

func TestTopologyView_FootnoteAppearsWithGPUColumns(t *testing.T) {
	m := newTopologyGPUTestModel()

	// Drill into "block" so GPU columns are visible
	m = sendKey(m, tea.KeyEnter)

	view := m.View()
	if !strings.Contains(view, "¹ GPU: Grove/Other/Total") {
		t.Errorf("expected footnote '¹ GPU: Grove/Other/Total' in view when GPU columns are present.\nview:\n%s", view)
	}
}

func TestTopologyView_FootnoteHiddenWithoutGPU(t *testing.T) {
	// Topology data without GPU info
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	tvd := sampleTopologyViewData()
	tvd.NodeGPUProducts = map[string]string{}
	tvd.NodeGPUCapacity = map[string]int64{}
	tvd.RawPods = []data.TopologyPodInput{}
	snap.TopologyViewData = tvd
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	m = sendRune(m, 't')

	// At top level, no footnote
	view := m.View()
	if strings.Contains(view, "Grove/Other/Total") {
		t.Errorf("expected no footnote at top level without GPU data.\nview:\n%s", view)
	}

	// Drill into region — still no GPU columns
	m = sendKey(m, tea.KeyEnter)
	view = m.View()
	if strings.Contains(view, "Grove/Other/Total") {
		t.Errorf("expected no footnote when drilled in without GPU data.\nview:\n%s", view)
	}
}

func TestTopologyView_ThreeWaySplit_MixedWorkloads(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "block", Key: "topology.io/block", ValuesCount: 1},
		},
		NodeLabels: map[string]map[string]string{
			"node-1": {"topology.io/block": "block-01"},
			"node-2": {"topology.io/block": "block-01"},
		},
		Pods: []data.TopologyViewPod{
			{Namespace: "default", Node: "node-1", Name: "grove-pod", Phase: "Running"},
			{Namespace: "default", Node: "node-1", Name: "other-pod", Phase: "Running"},
			{Namespace: "default", Node: "node-2", Name: "grove-pod-2", Phase: "Running"},
		},
		DomainToKey: map[string]string{"block": "topology.io/block"},
		NodeGPUProducts: map[string]string{
			"node-1": "H200",
			"node-2": "H200",
		},
		NodeGPUCapacity: map[string]int64{
			"node-1": 8,
			"node-2": 8,
		},
		RawPods: []data.TopologyPodInput{
			// PCS-managed pod → Grove
			{Name: "grove-pod", NodeName: "node-1", GPURequests: 4, Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
			}},
			// Non-PCS pod → Other
			{Name: "other-pod", NodeName: "node-1", GPURequests: 2, Labels: map[string]string{}},
			// PCS-managed pod → Grove
			{Name: "grove-pod-2", NodeName: "node-2", GPURequests: 3, Labels: map[string]string{
				"app.kubernetes.io/part-of": "other-pcs",
			}},
		},
	}
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	m = sendRune(m, 't')

	// Drill into "block"
	m = sendKey(m, tea.KeyEnter)

	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 block value, got %d", len(rows))
	}

	// block-01: Grove=4+3=7, Other=2, Total=8+8=16
	// Column order: VALUE + H200¹ + GPU PODS + PODS = 4 columns
	if len(rows[0]) != 4 {
		t.Fatalf("expected 4 columns, got %d: %v", len(rows[0]), rows[0])
	}
	if rows[0][0] != "block-01" {
		t.Errorf("value = %q, want 'block-01'", rows[0][0])
	}
	if rows[0][1] != "7/2/16" {
		t.Errorf("block-01 H200¹ = %q, want '7/2/16'", rows[0][1])
	}
}

func TestCommandMode_SwitchToForestFromDeepView(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set up deep navigation state
	m.viewState = data.ViewState{
		ViewType:             data.PodCliqueView,
		SelectedPodCliqueSet: "alpha-pcs",
		SelectedReplicaIndex: "0",
		SelectedPodClique:    "alpha-pcs-0-standalone-pc",
	}

	// Use command mode to go to forest
	m = sendRune(m, ':')
	for _, r := range "forest" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after :forest from deep view, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.viewState.SelectedPodCliqueSet != "" {
		t.Fatalf("expected SelectedPodCliqueSet cleared, got %q", m.viewState.SelectedPodCliqueSet)
	}
}
