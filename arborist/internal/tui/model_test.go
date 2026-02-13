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

// buildMockCacheWithEvents creates a MockGlobalCache identical to buildFullMockCache
// but with the provided events map merged into EventsByObject. This makes it easy to
// test event-related behavior without duplicating all the hierarchy setup.
func buildMockCacheWithEvents(events map[string][]data.Event) *data.MockGlobalCache {
	mc := buildFullMockCache()
	snap := mc.Snapshot()
	if snap.EventsByObject == nil {
		snap.EventsByObject = make(map[string][]data.Event)
	}
	for k, v := range events {
		snap.EventsByObject[k] = v
	}
	mc.SetSnapshot(snap)
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

	// Set up at PodView with parent PodClique context
	m.viewState.ViewType = data.PodView
	m.viewState.SelectedPod = "alpha-pcs-0-pc-worker-0"
	m.viewState.SelectedPodClique = "alpha-pcs-0-standalone-pc"

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

func TestNavigateBackPreservesFilter(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set up at PodCliqueSetReplicaView manually
	m.viewState.ViewType = data.PodCliqueSetReplicaView
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"
	m.viewState.SelectedReplicaIndex = "0"
	m.filterText = "something"
	m.filterInput.SetValue("something")

	// Navigate back
	m = sendKey(m, tea.KeyEsc)

	// Filter should be preserved across back navigation
	if m.filterText != "something" {
		t.Fatalf("expected filter to be preserved after navigateBack, got %q", m.filterText)
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
		Name: "alpha-pcs-replica-0", Type: "(PodCliqueSet replica)", Namespace: "default",
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
		{Name: "my-pcsg-replica-0", Type: "(PodCliqueScalingGroup replica)", Namespace: "default"},
		{Name: "my-pcsg-replica-1", Type: "(PodCliqueScalingGroup replica)", Namespace: "default"},
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
	if !strings.Contains(header, "forest") {
		t.Errorf("expected header to contain view name 'forest', got:\n%s", header)
	}
	for _, label := range []string{"Context:", "Cluster:", "User:", "Arborist Rev:", "K8s Rev:", "Namespace:", "Lens:"} {
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

	// All hierarchy views should show "forest" as the lens
	m.viewState.ViewType = data.PodCliqueSetView
	header := m.renderHeaderFrame()
	if !strings.Contains(header, "forest") {
		t.Errorf("expected header to show 'forest' lens for PodCliqueSetView, got:\n%s", header)
	}

	m.viewState.ViewType = data.PodView
	header = m.renderHeaderFrame()
	if !strings.Contains(header, "forest") {
		t.Errorf("expected header to show 'forest' lens for PodView, got:\n%s", header)
	}

	// Topology should show "topology"
	m.viewState.ViewType = data.TopologyView
	header = m.renderHeaderFrame()
	if !strings.Contains(header, "topology") {
		t.Errorf("expected header to show 'topology' lens for TopologyView, got:\n%s", header)
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

	// Press 't' — with no topology data, should stay in ForestView and log an error
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView when topology unavailable, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.errorLog) == 0 {
		t.Fatal("expected an error to be logged when topology is unavailable")
	}
	if !strings.Contains(m.errorLog[0].Message, "Topology unavailable") {
		t.Errorf("expected error about topology unavailable, got %q", m.errorLog[0].Message)
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

	// Toggle to topology — empty domains means topologyAvailable() = false,
	// so pressing 't' should stay in ForestView and log an error.
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView when topology has empty domains, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if len(m.errorLog) == 0 {
		t.Fatal("expected error logged when toggling to topology with empty domains")
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

func TestTopologyView_QuitWithQ(t *testing.T) {
	m := newTopologyTestModel()

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Press 'q' — should quit immediately, NOT navigate back to forest
	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit command from topology view")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from topology view, got %T", msg)
	}
}

func TestTopologyView_QuitWithCtrlC(t *testing.T) {
	m := newTopologyTestModel()

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Press Ctrl+C — should quit immediately
	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command from topology view with Ctrl+C")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from topology view, got %T", msg)
	}
}

func TestTopologyView_QuitAfterCommandMode(t *testing.T) {
	// Reproduce the exact user scenario: :topology then q
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	// Enter command mode with ':'
	m = sendRune(m, ':')
	if !m.commandActive {
		t.Fatal("expected command mode active")
	}

	// Type "topology"
	for _, r := range "topology" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Press Enter to execute
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView after :topology, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.commandActive {
		t.Fatal("expected command mode deactivated")
	}

	// Now press 'q' — should quit immediately
	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit command after :topology + q")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg after :topology + q, got %T", msg)
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

// ===========================================================================
// Lens Edit Mode Tests (inline header editing via 'l' key)
// ===========================================================================

func TestLensEdit_ActivateWithL(t *testing.T) {
	m := newTestModel(samplePCSResources())

	if m.lensEditActive {
		t.Fatal("expected lens edit mode inactive initially")
	}

	m = sendRune(m, 'l')
	if !m.lensEditActive {
		t.Fatal("expected lens edit mode active after pressing l")
	}
}

func TestLensEdit_EscCancels(t *testing.T) {
	m := newTestModel(samplePCSResources())

	m = sendRune(m, 'l')
	if !m.lensEditActive {
		t.Fatal("expected lens edit mode active")
	}

	// Type something first
	m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})

	m = sendKey(m, tea.KeyEsc)
	if m.lensEditActive {
		t.Fatal("expected lens edit mode deactivated after Esc")
	}
	if m.lensInput.Value() != "" {
		t.Fatalf("expected lens input cleared after Esc, got %q", m.lensInput.Value())
	}
}

func TestLensEdit_EnterExecutesTopology(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	m = sendRune(m, 'l')
	for _, r := range "topology" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyEnter)

	if m.lensEditActive {
		t.Fatal("expected lens edit mode deactivated after Enter")
	}
	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView after typing 'topology', got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestLensEdit_PrefixMatchTopology(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	snap.PodCliqueSets = samplePCSResources()
	mc.SetSnapshot(snap)

	m := newTestModelWithCache(mc)

	m = sendRune(m, 'l')
	for _, r := range "top" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView after prefix 'top', got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestLensEdit_PrefixMatchForest(t *testing.T) {
	m := newTopologyTestModel()

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	m = sendRune(m, 'l')
	for _, r := range "for" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after prefix 'for', got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestLensEdit_TabCompletion(t *testing.T) {
	m := newTestModel(samplePCSResources())

	m = sendRune(m, 'l')
	for _, r := range "top" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.lensInput.Value() != "top" {
		t.Fatalf("expected lens input 'top', got %q", m.lensInput.Value())
	}

	m = sendKey(m, tea.KeyTab)
	if m.lensInput.Value() != "topology" {
		t.Fatalf("expected lens input 'topology' after Tab, got %q", m.lensInput.Value())
	}
}

func TestLensEdit_TabCompletionForest(t *testing.T) {
	m := newTestModel(samplePCSResources())

	m = sendRune(m, 'l')
	for _, r := range "for" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyTab)
	if m.lensInput.Value() != "forest" {
		t.Fatalf("expected lens input 'forest' after Tab, got %q", m.lensInput.Value())
	}
}

func TestLensEdit_NoMatchDoesNothing(t *testing.T) {
	m := newTestModel(samplePCSResources())

	viewBefore := m.viewState.ViewType

	m = sendRune(m, 'l')
	for _, r := range "xyz" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != viewBefore {
		t.Fatalf("expected view unchanged after unmatched input, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.lensEditActive {
		t.Fatal("expected lens edit mode deactivated after Enter (even with no match)")
	}
}

func TestLensEdit_EmptyInputDoesNothing(t *testing.T) {
	m := newTestModel(samplePCSResources())

	viewBefore := m.viewState.ViewType

	m = sendRune(m, 'l')
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != viewBefore {
		t.Fatalf("expected view unchanged after empty input, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestLensEdit_CtrlCQuitsFromLensEdit(t *testing.T) {
	m := newTestModel(samplePCSResources())
	m = sendRune(m, 'l')
	if !m.lensEditActive {
		t.Fatal("expected lens edit active")
	}

	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command even in lens edit mode")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestLensEdit_HeaderShowsInlineInput(t *testing.T) {
	m := newTestModel(samplePCSResources())

	// Before activating, header should show the lens value
	header := m.renderHeaderFrame()
	if !strings.Contains(header, "forest") {
		t.Errorf("expected header to contain 'forest' lens value, got:\n%s", header)
	}

	// Activate lens edit and type something
	m = sendRune(m, 'l')
	for _, r := range "top" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	header = m.renderHeaderFrame()
	// The header should contain the typed text inline
	if !strings.Contains(header, "top") {
		t.Errorf("expected header to contain typed 'top' in lens input, got:\n%s", header)
	}
}

func TestLensEdit_HeaderShowsLensShortcut(t *testing.T) {
	m := newTestModel(samplePCSResources())

	header := m.renderHeaderFrame()
	if !strings.Contains(header, "Lens") {
		t.Errorf("expected header to contain 'Lens' shortcut, got:\n%s", header)
	}
}

func TestLensCommandNames(t *testing.T) {
	names := LensCommandNames()
	if len(names) != 9 {
		t.Fatalf("expected 9 lens commands, got %d", len(names))
	}
	expected := map[string]bool{
		"forest": true, "topology": true,
		"pcs": true, "podcliqueset": true,
		"pc": true, "podclique": true,
		"pcsg": true, "podcliquescalinggroup": true,
		"pod": true,
	}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected lens command name: %q", n)
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
	if rows[0][1] != "[                    ] (0/0/0)" {
		t.Errorf("block-01 B200¹ = %q, want bar format with '(0/0/0)'", rows[0][1])
	}
	if rows[0][2] != "[░░░░░░░             ] (0/6/16)" {
		t.Errorf("block-01 H200¹ = %q, want bar format with '(0/6/16)'", rows[0][2])
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
	if rows[1][1] != "[░░░░░░░░░░          ] (0/8/16)" {
		t.Errorf("block-02 B200¹ = %q, want bar format with '(0/8/16)'", rows[1][1])
	}
	if rows[1][2] != "[                    ] (0/0/0)" {
		t.Errorf("block-02 H200¹ = %q, want bar format with '(0/0/0)'", rows[1][2])
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
	if !strings.Contains(view, "¹ GPU: ▓▓ Grove  ░░ Other  (grove/other/total)") {
		t.Errorf("expected footnote with bar legend in view when GPU columns are present.\nview:\n%s", view)
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
	if strings.Contains(view, "grove/other/total)") {
		t.Errorf("expected no footnote at top level without GPU data.\nview:\n%s", view)
	}

	// Drill into region — still no GPU columns
	m = sendKey(m, tea.KeyEnter)
	view = m.View()
	if strings.Contains(view, "grove/other/total)") {
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
	if rows[0][1] != "[▓▓▓▓▓▓▓▓░░          ] (7/2/16)" {
		t.Errorf("block-01 H200¹ = %q, want bar format with '(7/2/16)'", rows[0][1])
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

// ---------------------------------------------------------------------------
// YAML Overlay Tests
// ---------------------------------------------------------------------------

func TestYAMLOverlay_YKeyOpensOverlay(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// In ForestView with alpha-pcs selected
	if m.yamlOverlayActive {
		t.Fatal("expected YAML overlay inactive initially")
	}

	// Press 'y' to open YAML overlay
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !m.yamlOverlayActive {
		t.Fatal("expected YAML overlay active after pressing y")
	}
	if m.yamlResourceType != "PodCliqueSet" {
		t.Fatalf("expected yamlResourceType='PodCliqueSet', got %q", m.yamlResourceType)
	}
	if m.yamlResourceName != "alpha-pcs" {
		t.Fatalf("expected yamlResourceName='alpha-pcs', got %q", m.yamlResourceName)
	}
	if cmd == nil {
		t.Fatal("expected a command to load YAML")
	}
}

func TestYAMLOverlay_EscClosesOverlay(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !m.yamlOverlayActive {
		t.Fatal("expected YAML overlay active")
	}

	// Press Esc to close
	m = sendKey(m, tea.KeyEsc)
	if m.yamlOverlayActive {
		t.Fatal("expected YAML overlay closed after Esc")
	}
}

func TestYAMLOverlay_QClosesOverlay(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !m.yamlOverlayActive {
		t.Fatal("expected YAML overlay active")
	}

	// Press q to close (should NOT quit the app)
	m = sendRune(m, 'q')
	if m.yamlOverlayActive {
		t.Fatal("expected YAML overlay closed after q")
	}
	// Verify we're still in the model (not quitting)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected still in ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestYAMLOverlay_CtrlCStillQuits(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Ctrl+C should still quit
	_, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command from Ctrl+C in YAML overlay")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestYAMLOverlay_ResourceYAMLMsgPopulatesViewport(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Deliver ResourceYAMLMsg
	yamlContent := "apiVersion: grove.io/v1alpha1\nkind: PodCliqueSet\nmetadata:\n  name: alpha-pcs\n"
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         yamlContent,
	})

	if m.yamlContent != yamlContent {
		t.Fatalf("expected yamlContent to be set, got %q", m.yamlContent)
	}
}

func TestYAMLOverlay_ResourceYAMLMsgError(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Deliver error message
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		Err:          errForTest("api error"),
	})

	if !strings.Contains(m.yamlContent, "Error") {
		t.Fatalf("expected yamlContent to contain error message, got %q", m.yamlContent)
	}
	// Overlay should still be active (showing the error)
	if !m.yamlOverlayActive {
		t.Fatal("expected YAML overlay still active after error")
	}
}

func TestYAMLOverlay_ViewRendersOverlay(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay and deliver YAML content
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "apiVersion: grove.io/v1alpha1\nkind: PodCliqueSet\n",
	})

	view := m.View()

	// Should show YAML content
	assertView(t, m, []string{"YAML", "PodCliqueSet"})

	// Should show overlay key hints
	if !strings.Contains(view, "Close") {
		t.Errorf("expected view to contain 'Close' hint")
	}
	if !strings.Contains(view, "Scroll") {
		t.Errorf("expected view to contain 'Scroll' hint")
	}
	if !strings.Contains(view, "Search") {
		t.Errorf("expected view to contain 'Search' hint")
	}

	// Should NOT show the normal resources table headers
	// (the overlay takes over the full screen)
	if strings.Contains(view, "NAMESPACE") && strings.Contains(view, "SCHEDULED") {
		t.Errorf("expected overlay to replace normal view, but table headers are visible")
	}
}

func TestYAMLOverlay_UpDownScrolls(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay with multi-line content
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Build long content that exceeds viewport height
	var lines string
	for i := 0; i < 100; i++ {
		lines += "line: " + string(rune('0'+i%10)) + "\n"
	}
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         lines,
	})

	initialOffset := m.yamlViewport.YOffset

	// Press Down multiple times
	m = sendKey(m, tea.KeyDown)
	m = sendKey(m, tea.KeyDown)
	m = sendKey(m, tea.KeyDown)

	if m.yamlViewport.YOffset <= initialOffset {
		t.Errorf("expected viewport to scroll down, offset was %d now %d", initialOffset, m.yamlViewport.YOffset)
	}

	// Press Up
	scrolledOffset := m.yamlViewport.YOffset
	m = sendKey(m, tea.KeyUp)

	if m.yamlViewport.YOffset >= scrolledOffset {
		t.Errorf("expected viewport to scroll up, offset was %d now %d", scrolledOffset, m.yamlViewport.YOffset)
	}
}

func TestYAMLOverlay_PgUpPgDownScrolls(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay with multi-line content
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	var lines string
	for i := 0; i < 200; i++ {
		lines += "line: content here\n"
	}
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         lines,
	})

	// PgDown should scroll more than a single Down
	m = sendKey(m, tea.KeyPgDown)
	afterPgDown := m.yamlViewport.YOffset

	if afterPgDown == 0 {
		t.Error("expected PgDown to scroll viewport")
	}
}

func TestYAMLOverlay_SearchActivatesAndApplies(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay with content
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "line1: first\nline2: second\nline3: third\nsearchTarget: found-it\nline5: fifth\n",
	})

	// Press '/' to activate search
	m = sendRune(m, '/')
	if !m.yamlSearchActive {
		t.Fatal("expected YAML search to be active after /")
	}

	// Type search text
	for _, r := range "searchTarget" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Press Enter to apply search
	m = sendKey(m, tea.KeyEnter)
	if m.yamlSearchActive {
		t.Fatal("expected YAML search deactivated after Enter")
	}
	if m.yamlSearchText != "searchTarget" {
		t.Fatalf("expected yamlSearchText='searchTarget', got %q", m.yamlSearchText)
	}
}

func TestYAMLOverlay_SearchEscCancels(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay and start search
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "some: yaml\n",
	})

	m = sendRune(m, '/')
	if !m.yamlSearchActive {
		t.Fatal("expected search active")
	}

	// Press Esc to cancel search (NOT close overlay)
	m = sendKey(m, tea.KeyEsc)
	if m.yamlSearchActive {
		t.Fatal("expected search deactivated after Esc")
	}
	// Overlay should still be active
	if !m.yamlOverlayActive {
		t.Fatal("expected YAML overlay still active after search Esc")
	}
}

func TestYAMLOverlay_SearchViewShowsIndicator(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay, search, and apply
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "some: yaml\ntarget: value\n",
	})

	m = sendRune(m, '/')
	for _, r := range "target" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	// View should show the search text and Next/Prev hints
	view := m.View()
	if !strings.Contains(view, "target") {
		t.Errorf("expected view to show search text 'target'")
	}
	if !strings.Contains(view, "Next/Prev") {
		t.Errorf("expected view to show 'Next/Prev' hint when search is active")
	}
}

func TestYAMLOverlay_NoResourceSelectedDoesNothing(t *testing.T) {
	// Empty model with no resources
	m := newTestModel([]data.Resource{})

	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if m.yamlOverlayActive {
		t.Fatal("expected YAML overlay NOT to open when no resource is selected")
	}
	if cmd != nil {
		t.Fatal("expected no command when no resource is selected")
	}
}

func TestYAMLOverlay_VirtualTypeResolvesToParent(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Navigate into alpha-pcs (single replica skip → PodCliqueSetReplicaView)
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Go back and navigate into beta-pcs (2 replicas → PodCliqueSetView)
	m = sendKey(m, tea.KeyEsc)
	m = sendKey(m, tea.KeyDown) // move to beta-pcs
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.ViewType != data.PodCliqueSetView {
		t.Fatalf("expected PodCliqueSetView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// First row should be a PodCliqueSetReplica
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 2 || selectedRow[1] != "(PodCliqueSet replica)" {
		t.Fatalf("expected PodCliqueSetReplica selected, got %v", selectedRow)
	}

	// Press 'y' — should resolve to the PodCliqueSet parent
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !m.yamlOverlayActive {
		t.Fatal("expected YAML overlay active")
	}
	// yamlResourceType records the table's type (for display), but the actual
	// fetch resolves to the parent PodCliqueSet
	if m.yamlResourceType != "(PodCliqueSet replica)" {
		t.Fatalf("expected yamlResourceType='PodCliqueSetReplica', got %q", m.yamlResourceType)
	}
	if cmd == nil {
		t.Fatal("expected a command to load YAML")
	}
}

func TestYAMLOverlay_OverlayDoesNotInterfereWithNormalView(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open and close YAML overlay
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "some: yaml\n",
	})
	m = sendKey(m, tea.KeyEsc)

	// Should be back to normal view
	if m.yamlOverlayActive {
		t.Fatal("expected overlay closed")
	}

	// View should show normal forest data
	assertView(t, m, []string{"alpha-pcs", "beta-pcs", "gamma-pcs"})
	// Should NOT show YAML overlay content
	assertNotInView(t, m, []string{"some: yaml"})
}

func TestYAMLOverlay_ResourceYAMLMsgIgnoredWhenOverlayClosed(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Deliver ResourceYAMLMsg without overlay being active (race condition defense)
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "apiVersion: grove.io/v1alpha1\n",
	})

	// Should not crash and overlay should remain inactive
	if m.yamlOverlayActive {
		t.Fatal("expected YAML overlay to remain inactive")
	}
}

func TestYAMLOverlay_HeaderShowsYAMLShortcut(t *testing.T) {
	m := newTestModel(samplePCSResources())

	header := m.renderHeaderFrame()
	if !strings.Contains(header, "YAML") {
		t.Errorf("expected header to contain 'YAML' shortcut, got:\n%s", header)
	}
}

func TestYAMLOverlay_YKeyFromPodView(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set up PodView state manually
	m.viewState = data.ViewState{
		ViewType:             data.PodView,
		SelectedPodCliqueSet: "alpha-pcs",
		SelectedReplicaIndex: "0",
		SelectedPodClique:    "alpha-pcs-0-standalone-pc",
		SelectedPod:          "alpha-pcs-0-pc-worker-0",
	}

	// Press 'y'
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !m.yamlOverlayActive {
		t.Fatal("expected YAML overlay active from PodView")
	}
	if m.yamlResourceType != "Pod" {
		t.Fatalf("expected yamlResourceType='Pod', got %q", m.yamlResourceType)
	}
	if m.yamlResourceName != "alpha-pcs-0-pc-worker-0" {
		t.Fatalf("expected yamlResourceName='alpha-pcs-0-pc-worker-0', got %q", m.yamlResourceName)
	}
	if cmd == nil {
		t.Fatal("expected a command to load YAML")
	}
}

func TestYAMLOverlay_WindowResizeUpdatesViewport(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "some: yaml\ncontent: here\n",
	})

	// Resize window
	m = mustApply(m, tea.WindowSizeMsg{Width: 200, Height: 60})

	// Overlay should still be active and renderable
	if !m.yamlOverlayActive {
		t.Fatal("expected overlay still active after resize")
	}
	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view after resize")
	}
}

func TestYAMLOverlay_SelectedResourceInfo_ForestView(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	resType, resName, ns := m.selectedResourceInfo()
	if resType != "PodCliqueSet" {
		t.Fatalf("expected type 'PodCliqueSet', got %q", resType)
	}
	if resName != "alpha-pcs" {
		t.Fatalf("expected name 'alpha-pcs', got %q", resName)
	}
	if ns != "default" {
		t.Fatalf("expected namespace 'default', got %q", ns)
	}
}

func TestYAMLOverlay_SelectedResourceInfo_PodView(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m.viewState = data.ViewState{
		ViewType:             data.PodView,
		SelectedPodCliqueSet: "alpha-pcs",
		SelectedReplicaIndex: "0",
		SelectedPodClique:    "alpha-pcs-0-standalone-pc",
		SelectedPod:          "alpha-pcs-0-pc-worker-0",
	}

	resType, resName, _ := m.selectedResourceInfo()
	if resType != "Pod" {
		t.Fatalf("expected type 'Pod', got %q", resType)
	}
	if resName != "alpha-pcs-0-pc-worker-0" {
		t.Fatalf("expected name 'alpha-pcs-0-pc-worker-0', got %q", resName)
	}
}

func TestYAMLOverlay_NSearchJumpsToNextMatch(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay with content containing repeated matches
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	var lines string
	for i := 0; i < 100; i++ {
		if i == 30 || i == 60 || i == 90 {
			lines += "match: found\n"
		} else {
			lines += "other: content\n"
		}
	}
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         lines,
	})

	// Search for "match"
	m = sendRune(m, '/')
	for _, r := range "match" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	firstOffset := m.yamlViewport.YOffset

	// Press 'n' to go to next match
	m = sendRune(m, 'n')
	secondOffset := m.yamlViewport.YOffset

	// Should have moved to a different position
	if secondOffset == firstOffset {
		// This might happen if both matches are visible in the same viewport page,
		// but with matches at lines 30, 60, 90 and a typical viewport height of ~34,
		// at least one 'n' should move the viewport
		m = sendRune(m, 'n')
		thirdOffset := m.yamlViewport.YOffset
		if thirdOffset == firstOffset && thirdOffset == secondOffset {
			t.Error("expected 'n' to navigate between search matches")
		}
	}
}

func TestYAMLOverlay_KeysPassedToViewportNotToNormalMode(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "some: yaml\n",
	})

	// 't' in overlay should NOT toggle topology view
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected 't' in YAML overlay to NOT toggle topology, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	// Overlay should still be active (unknown key is a no-op)
	if !m.yamlOverlayActive {
		t.Fatal("expected overlay still active after pressing t")
	}

	// '/' should activate search, not the normal filter
	m = sendRune(m, '/')
	if m.yamlSearchActive != true {
		t.Fatal("expected YAML search active after / in overlay")
	}
	if m.filterActive {
		t.Fatal("expected normal filter NOT active when in YAML overlay")
	}
}

func TestYAMLOverlay_SearchHighlightsMatches(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Open YAML overlay with content
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         "apiVersion: grove.io/v1alpha1\nkind: PodCliqueSet\nmetadata:\n  name: alpha-pcs\n",
	})

	// Search for "PodCliqueSet"
	m = sendRune(m, '/')
	for _, r := range "PodCliqueSet" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	// The view should contain ANSI-styled highlight (the rendered view will have
	// the search text rendered through YAMLSearchHighlightStyle)
	view := m.View()
	// The highlighted text should still appear in the view
	if !strings.Contains(view, "PodCliqueSet") {
		t.Errorf("expected view to still contain 'PodCliqueSet' (highlighted)")
	}
}

func TestHighlightYAMLSearch_BasicHighlighting(t *testing.T) {
	content := "line1: hello\nline2: world\nline3: hello world\n"

	result := highlightYAMLSearch(content, "hello")
	lines := strings.Split(result, "\n")

	// All lines should be present
	if len(lines) != 4 { // 3 lines + trailing empty from final \n
		t.Fatalf("expected 4 lines (incl trailing), got %d", len(lines))
	}

	// Line 1 should contain "hello" (possibly styled)
	if !strings.Contains(lines[0], "hello") {
		t.Errorf("expected line1 to contain 'hello', got %q", lines[0])
	}
	// Line 1 should also contain the prefix
	if !strings.Contains(lines[0], "line1: ") {
		t.Errorf("expected line1 to preserve 'line1: ' prefix, got %q", lines[0])
	}
	// Line 2 should be completely unmodified (no match)
	if lines[1] != "line2: world" {
		t.Errorf("expected line2 unmodified, got %q", lines[1])
	}
	// Line 3 should contain both "hello" and "world"
	if !strings.Contains(lines[2], "hello") || !strings.Contains(lines[2], "world") {
		t.Errorf("expected line3 to contain 'hello' and 'world', got %q", lines[2])
	}
}

func TestHighlightYAMLSearch_CaseInsensitive(t *testing.T) {
	content := "Kind: PodCliqueSet\nkind: podcliqueset\nother: line\n"

	result := highlightYAMLSearch(content, "kind")
	lines := strings.Split(result, "\n")

	// Both matching lines should contain the original text
	if !strings.Contains(lines[0], "Kind") {
		t.Errorf("expected first line to contain 'Kind', got %q", lines[0])
	}
	if !strings.Contains(lines[1], "kind") {
		t.Errorf("expected second line to contain 'kind', got %q", lines[1])
	}
	// Non-matching line should be unmodified
	if lines[2] != "other: line" {
		t.Errorf("expected non-matching line unmodified, got %q", lines[2])
	}
}

func TestHighlightYAMLSearch_EmptySearchReturnsOriginal(t *testing.T) {
	content := "some: yaml\n"
	result := highlightYAMLSearch(content, "")
	if result != content {
		t.Errorf("expected original content for empty search, got %q", result)
	}
}

func TestHighlightYAMLSearch_MultipleMatchesPerLine(t *testing.T) {
	content := "aa bb aa cc aa\n"

	result := highlightYAMLSearch(content, "aa")

	// Verify the non-matching parts are preserved verbatim
	if !strings.Contains(result, " bb ") {
		t.Error("expected ' bb ' to be preserved between highlights")
	}
	if !strings.Contains(result, " cc ") {
		t.Error("expected ' cc ' to be preserved between highlights")
	}
}

func TestHighlightYAMLSearch_PreservesOriginalCase(t *testing.T) {
	// Search is case-insensitive but highlighted text should keep original case
	content := "Name: MyResource\nname: other\n"
	result := highlightYAMLSearch(content, "name")

	// Both "Name" and "name" should appear (with original casing)
	if !strings.Contains(result, "Name") {
		t.Error("expected 'Name' (original case) to appear in highlighted output")
	}
	if !strings.Contains(result, "name") {
		t.Error("expected 'name' (original case) to appear in highlighted output")
	}
}

func TestHighlightYAMLSearch_NoMatchReturnsOriginal(t *testing.T) {
	content := "line1: hello\nline2: world\n"
	result := highlightYAMLSearch(content, "zzzzz")
	if result != content {
		t.Errorf("expected unmodified content when no match, got %q", result)
	}
}

func TestHighlightYAMLSearch_WithANSI(t *testing.T) {
	// Verify the function calls the highlight style by checking that
	// the Render method is invoked (the output should contain the match text
	// wrapped by whatever the style produces — even if no ANSI in test env,
	// the function still runs the code path)
	content := "target: value\nother: line\n"
	result := highlightYAMLSearch(content, "target")

	// The result should contain "target" somewhere
	if !strings.Contains(result, "target") {
		t.Error("expected 'target' to appear in result")
	}
	// And ": value" should be preserved
	if !strings.Contains(result, ": value") {
		t.Error("expected ': value' to be preserved after match")
	}
	// The non-matching line should be untouched
	lines := strings.Split(result, "\n")
	if lines[1] != "other: line" {
		t.Errorf("expected non-matching line unchanged, got %q", lines[1])
	}
}

func TestYAMLOverlay_SearchEscClearsHighlights(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	yamlContent := "apiVersion: grove.io/v1alpha1\nkind: PodCliqueSet\n"

	// Open YAML overlay and search
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mustApply(m, ResourceYAMLMsg{
		ResourceType: "PodCliqueSet",
		ResourceName: "alpha-pcs",
		YAML:         yamlContent,
	})

	// Apply a search
	m = sendRune(m, '/')
	for _, r := range "kind" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.yamlSearchText != "kind" {
		t.Fatalf("expected search text 'kind', got %q", m.yamlSearchText)
	}

	// Open search again and press Esc to cancel/clear
	m = sendRune(m, '/')
	m = sendKey(m, tea.KeyEsc)

	// Search text should be cleared
	if m.yamlSearchText != "" {
		t.Fatalf("expected search text cleared after Esc, got %q", m.yamlSearchText)
	}
}

// ---------------------------------------------------------------------------
// Resource Switching via `:` Command Tests
// ---------------------------------------------------------------------------

// --- 11b. Flat resource builder tests ---

func TestFlatPodCliques_Basic(t *testing.T) {
	mc := buildFullMockCache()
	snap := mc.Snapshot()
	result := flatPodCliques(snap)
	if len(result) == 0 {
		t.Fatal("expected non-empty flat PodCliques list")
	}
	// Verify all are PodClique type
	for _, r := range result {
		if r.Type != "PodClique" {
			t.Errorf("expected Type=PodClique, got %q for %q", r.Type, r.Name)
		}
	}
	// Verify sorted by name
	for i := 1; i < len(result); i++ {
		if result[i].Name < result[i-1].Name {
			t.Errorf("expected sorted, but %q comes after %q", result[i].Name, result[i-1].Name)
		}
	}
}

func TestFlatPodCliques_Deduplication(t *testing.T) {
	mc := buildFullMockCache()
	snap := mc.Snapshot()
	result := flatPodCliques(snap)
	seen := make(map[string]bool)
	for _, r := range result {
		if seen[r.Name] {
			t.Errorf("duplicate PodClique: %q", r.Name)
		}
		seen[r.Name] = true
	}
}

func TestFlatPodCliques_EmptySnapshot(t *testing.T) {
	result := flatPodCliques(&data.CacheSnapshot{})
	if len(result) != 0 {
		t.Errorf("expected empty list for empty snapshot, got %d", len(result))
	}
	// nil snapshot
	result = flatPodCliques(nil)
	if result != nil {
		t.Errorf("expected nil for nil snapshot, got %v", result)
	}
}

func TestFlatScalingGroups_Basic(t *testing.T) {
	mc := buildFullMockCache()
	snap := mc.Snapshot()
	result := flatScalingGroups(snap)
	if len(result) == 0 {
		t.Fatal("expected non-empty flat ScalingGroups list")
	}
	for _, r := range result {
		if r.Type != "PodCliqueScalingGroup" {
			t.Errorf("expected Type=PodCliqueScalingGroup, got %q for %q", r.Type, r.Name)
		}
	}
}

func TestFlatScalingGroups_EmptySnapshot(t *testing.T) {
	result := flatScalingGroups(&data.CacheSnapshot{})
	if len(result) != 0 {
		t.Errorf("expected empty list for empty snapshot, got %d", len(result))
	}
}

func TestFlatPods_Basic(t *testing.T) {
	mc := buildFullMockCache()
	snap := mc.Snapshot()
	result := flatPods(snap)
	if len(result) == 0 {
		t.Fatal("expected non-empty flat Pods list")
	}
	for _, r := range result {
		if r.Type != "Pod" {
			t.Errorf("expected Type=Pod, got %q for %q", r.Type, r.Name)
		}
	}
}

func TestFlatPods_Deduplication(t *testing.T) {
	mc := buildFullMockCache()
	snap := mc.Snapshot()
	result := flatPods(snap)
	seen := make(map[string]bool)
	for _, r := range result {
		if seen[r.Name] {
			t.Errorf("duplicate Pod: %q", r.Name)
		}
		seen[r.Name] = true
	}
}

func TestFlatPods_EmptySnapshot(t *testing.T) {
	result := flatPods(&data.CacheSnapshot{})
	if len(result) != 0 {
		t.Errorf("expected empty list for empty snapshot, got %d", len(result))
	}
}

// --- 11c. Command execution for resource types ---

func TestExecuteCommand_PCS_ExactMatch(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "pcs" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.forestResourceType != "pcs" {
		t.Fatalf("expected forestResourceType=pcs, got %q", m.forestResourceType)
	}
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestExecuteCommand_PC_ExactMatch(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "pc" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	// "pc" is not a unique prefix (matches pc, pcs, pcsg, podclique, podcliqueset, podcliquescalinggroup)
	// but it IS an exact match for the "pc" command, so it should match
	if m.forestResourceType != "pc" {
		t.Fatalf("expected forestResourceType=pc, got %q", m.forestResourceType)
	}
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestExecuteCommand_PCSG_ExactMatch(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "pcsg" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.forestResourceType != "pcsg" {
		t.Fatalf("expected forestResourceType=pcsg, got %q", m.forestResourceType)
	}
}

func TestExecuteCommand_Pod_ExactMatch(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "pod" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.forestResourceType != "pod" {
		t.Fatalf("expected forestResourceType=pod, got %q", m.forestResourceType)
	}
}

func TestExecuteCommand_LongForm_PodCliqueSet(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "podcliqueset" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.forestResourceType != "pcs" {
		t.Fatalf("expected forestResourceType=pcs (normalized), got %q", m.forestResourceType)
	}
}

func TestExecuteCommand_LongForm_PodClique(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "podclique" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.forestResourceType != "pc" {
		t.Fatalf("expected forestResourceType=pc (normalized), got %q", m.forestResourceType)
	}
}

func TestExecuteCommand_LongForm_PodCliqueScalingGroup(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "podcliquescalinggroup" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.forestResourceType != "pcsg" {
		t.Fatalf("expected forestResourceType=pcsg (normalized), got %q", m.forestResourceType)
	}
}

func TestExecuteCommand_ResourceType_ClearsViewState(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Drill into a PCS first
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.SelectedPodCliqueSet == "" {
		t.Fatal("expected SelectedPodCliqueSet to be set after drill")
	}

	// Switch to :pc
	m = sendRune(m, ':')
	for _, r := range "pc" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.SelectedPodCliqueSet != "" {
		t.Fatalf("expected SelectedPodCliqueSet cleared, got %q", m.viewState.SelectedPodCliqueSet)
	}
	if m.viewState.SelectedReplicaIndex != "" {
		t.Fatalf("expected SelectedReplicaIndex cleared, got %q", m.viewState.SelectedReplicaIndex)
	}
}

func TestExecuteCommand_ResourceType_PreservesFilter(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set a filter
	m.filterText = "alpha"
	m.filterInput.SetValue("alpha")

	// Switch to :pod
	m = sendRune(m, ':')
	for _, r := range "pod" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.filterText != "alpha" {
		t.Fatalf("expected filter preserved after resource type switch, got %q", m.filterText)
	}
}

func TestExecuteCommand_Forest_StillWorks(t *testing.T) {
	mc := buildFullMockCache()
	// Add topology data so 't' can switch to TopologyView
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	mc.SetSnapshot(snap)
	m := newTestModelWithCache(mc)

	// Switch to topology first
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// :forest should go back to ForestView
	m = sendRune(m, ':')
	for _, r := range "forest" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after :forest, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.forestResourceType != "pcs" {
		t.Fatalf("expected forestResourceType=pcs after :forest, got %q", m.forestResourceType)
	}
}

func TestExecuteCommand_Topology_StillWorks(t *testing.T) {
	mc := buildFullMockCache()
	snap := mc.Snapshot()
	snap.TopologyViewData = sampleTopologyViewData()
	mc.SetSnapshot(snap)
	m := newTestModelWithCache(mc)

	m = sendRune(m, ':')
	for _, r := range "topology" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView after :topology, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

// --- 11d. Autocomplete with expanded candidates ---

func TestAutocompleter_UniqueMatch_WithResourceTypes(t *testing.T) {
	ac := NewAutocompleter(LensCommandNames())

	tests := []struct {
		prefix      string
		expectMatch bool
		expected    string
	}{
		{"for", true, "forest"},
		{"top", true, "topology"},
		{"pcsg", true, "pcsg"}, // only "pcsg" starts with "pcsg"
		{"pod", false, ""},     // "pod", "podclique", "podcliqueset", "podcliquescalinggroup" all match
		{"pcs", false, ""},     // "pcs" and "pcsg" both match (prefix)
		{"pc", false, ""},      // many matches
		{"p", false, ""},       // many matches
	}

	for _, tt := range tests {
		name, ok := ac.UniqueMatch(tt.prefix)
		if ok != tt.expectMatch {
			t.Errorf("UniqueMatch(%q): got ok=%v, want ok=%v (name=%q)", tt.prefix, ok, tt.expectMatch, name)
		}
		if ok && name != tt.expected {
			t.Errorf("UniqueMatch(%q): got name=%q, want %q", tt.prefix, name, tt.expected)
		}
	}
}

// --- 11e. Navigation from flat lists ---

func TestFlatPC_DrillInto_ShowsPods(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Switch to :pc (flat PodClique list)
	m.forestResourceType = "pc"
	m.applySnapshot()
	m.rebuildResourcesTable()

	// Verify we have PodCliques in the forest
	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected PodCliques in forest view")
	}
	if forest[0].Type != "PodClique" {
		t.Fatalf("expected first resource to be PodClique, got %q", forest[0].Type)
	}

	// Drill into the first PodClique
	m = sendKey(m, tea.KeyEnter)

	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView after drilling into PodClique, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestFlatPCSG_DrillInto(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Switch to :pcsg (flat PCSG list)
	m.forestResourceType = "pcsg"
	m.applySnapshot()
	m.rebuildResourcesTable()

	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected PCSGs in forest view")
	}
	if forest[0].Type != "PodCliqueScalingGroup" {
		t.Fatalf("expected first resource to be PodCliqueScalingGroup, got %q", forest[0].Type)
	}
}

func TestFlatPod_DrillInto_ShowsPodView(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Switch to :pod (flat Pod list)
	m.forestResourceType = "pod"
	m.applySnapshot()
	m.rebuildResourcesTable()

	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected Pods in forest view")
	}
	if forest[0].Type != "Pod" {
		t.Fatalf("expected first resource to be Pod, got %q", forest[0].Type)
	}

	// Drill into the first Pod
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.viewState.ViewType != data.PodView {
		t.Fatalf("expected PodView after drilling into Pod, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

// --- 11f. Back navigation from flat-list drill-ins ---

func TestNavigateBack_PodCliqueView_NoPCSContext_GoesToForest(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set up PodCliqueView with no PCS context (as if from flat PC list)
	m.viewState.ViewType = data.PodCliqueView
	m.viewState.SelectedPodClique = "alpha-pcs-0-standalone-pc"
	m.viewState.SelectedPodCliqueSet = "" // no PCS context

	m = sendKey(m, tea.KeyEsc)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc from PodCliqueView with no PCS context, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBack_PodView_NoPCContext_GoesToForest(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set up PodView with no PodClique context (as if from flat Pod list)
	m.viewState.ViewType = data.PodView
	m.viewState.SelectedPod = "alpha-pcs-0-pc-worker-0"
	m.viewState.SelectedPodClique = "" // no PodClique context

	m = sendKey(m, tea.KeyEsc)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc from PodView with no PC context, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBack_PCSGView_NoPCSContext_GoesToForest(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m.viewState.ViewType = data.PodCliqueScalingGroupView
	m.viewState.SelectedScalingGroup = "alpha-pcs-0-sg-prefill"
	m.viewState.SelectedPodCliqueSet = "" // no PCS context

	m = sendKey(m, tea.KeyEsc)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc from PCSGView with no PCS context, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBack_PCSGReplicaView_NoPCSContext_GoesToForest(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m.viewState.ViewType = data.PodCliqueScalingGroupReplicaView
	m.viewState.SelectedScalingGroup = "alpha-pcs-0-sg-prefill"
	m.viewState.SelectedPCSGReplicaIndex = "0"
	m.viewState.SelectedPodCliqueSet = "" // no PCS context

	m = sendKey(m, tea.KeyEsc)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc from PCSGReplicaView with no PCS context, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBack_PreservesForestResourceType(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set forest resource type to "pc" and drill into a PodClique from flat list
	m.forestResourceType = "pc"
	m.viewState.ViewType = data.PodCliqueView
	m.viewState.SelectedPodClique = "alpha-pcs-0-standalone-pc"
	m.viewState.SelectedPodCliqueSet = "" // no PCS context

	m = sendKey(m, tea.KeyEsc)

	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.forestResourceType != "pc" {
		t.Fatalf("expected forestResourceType to be preserved as 'pc', got %q", m.forestResourceType)
	}
}

// --- 11g. Filter preservation tests ---

func TestFilterPreserved_OnNavigateBack(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Drill into alpha-pcs
	m = sendKey(m, tea.KeyEnter)

	// Set a filter
	m.filterText = "alpha"
	m.filterInput.SetValue("alpha")

	// Navigate back
	m = sendKey(m, tea.KeyEsc)

	if m.filterText != "alpha" {
		t.Fatalf("expected filter preserved after back navigation, got %q", m.filterText)
	}
}

func TestFilterCleared_OnNavigateInto(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set a filter
	m.filterText = "alpha"
	m.filterInput.SetValue("alpha")
	m.rebuildResourcesTable()

	// Navigate into (drill in clears filter)
	m = sendKey(m, tea.KeyEnter)

	if m.filterText != "" {
		t.Fatalf("expected filter cleared after drill-in, got %q", m.filterText)
	}
}

func TestFilterPreserved_OnResourceTypeSwitch(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Set a filter
	m.filterText = "alpha"
	m.filterInput.SetValue("alpha")

	// Switch resource type via command
	m = sendRune(m, ':')
	for _, r := range "pod" {
		m = mustApply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = sendKey(m, tea.KeyEnter)

	if m.filterText != "alpha" {
		t.Fatalf("expected filter preserved after resource type switch, got %q", m.filterText)
	}
	if m.forestResourceType != "pod" {
		t.Fatalf("expected forestResourceType=pod, got %q", m.forestResourceType)
	}
}

// --- 11h. Rendering tests ---

func TestViewDisplayName_ForestAlwaysShowsForest(t *testing.T) {
	m := newTestModel(nil)

	// Even with different forestResourceType values, the lens should say "forest"
	for _, rt := range []string{"pcs", "pc", "pcsg", "pod"} {
		m.forestResourceType = rt
		got := m.viewDisplayName()
		if got != "forest" {
			t.Errorf("viewDisplayName() with forestResourceType=%q = %q, want %q", rt, got, "forest")
		}
	}

	// Even when drilled into sub-views, the lens should say "forest"
	for _, vt := range []data.ViewType{
		data.PodCliqueSetView,
		data.PodCliqueView,
		data.PodView,
	} {
		m.viewState.ViewType = vt
		got := m.viewDisplayName()
		if got != "forest" {
			t.Errorf("viewDisplayName() for %s = %q, want %q",
				data.ViewTypeName(vt), got, "forest")
		}
	}
}

func TestRenderBreadcrumb_ForestPC(t *testing.T) {
	m := newTestModel(nil)
	m.forestResourceType = "pc"
	breadcrumb := m.renderBreadcrumb()
	if !strings.Contains(breadcrumb, "PodCliques") {
		t.Errorf("expected breadcrumb to contain 'PodCliques', got %q", breadcrumb)
	}
}

func TestRenderBreadcrumb_ForestPCSG(t *testing.T) {
	m := newTestModel(nil)
	m.forestResourceType = "pcsg"
	breadcrumb := m.renderBreadcrumb()
	if !strings.Contains(breadcrumb, "PodCliqueScalingGroups") {
		t.Errorf("expected breadcrumb to contain 'PodCliqueScalingGroups', got %q", breadcrumb)
	}
}

func TestRenderBreadcrumb_ForestPod(t *testing.T) {
	m := newTestModel(nil)
	m.forestResourceType = "pod"
	breadcrumb := m.renderBreadcrumb()
	if !strings.Contains(breadcrumb, "Pods") {
		t.Errorf("expected breadcrumb to contain 'Pods', got %q", breadcrumb)
	}
}

func TestRenderBreadcrumb_ForestPCS(t *testing.T) {
	m := newTestModel(nil)
	m.forestResourceType = "pcs"
	breadcrumb := m.renderBreadcrumb()
	if !strings.Contains(breadcrumb, "Forest") {
		t.Errorf("expected breadcrumb to contain 'Forest', got %q", breadcrumb)
	}
}

// --- WithForestResourceType option test ---

func TestWithForestResourceType(t *testing.T) {
	mc := data.NewMockGlobalCache()
	m := NewModel(mc, WithForestResourceType("podclique"))
	if m.forestResourceType != "pc" {
		t.Errorf("expected forestResourceType=pc after WithForestResourceType(podclique), got %q", m.forestResourceType)
	}
}

func TestWithFilter(t *testing.T) {
	mc := data.NewMockGlobalCache()
	m := NewModel(mc, WithFilter("test"))
	if m.filterText != "test" {
		t.Errorf("expected filterText=test after WithFilter, got %q", m.filterText)
	}
}

// ---------------------------------------------------------------------------
// Events update correctly for non-PCS forest resource types
// ---------------------------------------------------------------------------

func TestForestPC_EventsUpdateOnSelection(t *testing.T) {
	// Build a cache where different PodCliques have different events
	mc := buildMockCacheWithEvents(map[string][]data.Event{
		"PodClique/alpha-pcs-0-standalone-pc": {
			{Type: "Normal", Kind: "PodClique", Reason: "ScaledUp", Age: "1m", From: "controller", Message: "standalone scaled", Parent: "alpha-pcs-0-standalone-pc"},
		},
		"PodClique/alpha-pcs-0-sg-prefill-0-worker": {
			{Type: "Warning", Kind: "PodClique", Reason: "Degraded", Age: "2m", From: "controller", Message: "prefill worker degraded", Parent: "alpha-pcs-0-sg-prefill-0-worker"},
		},
	})
	m := newTestModelWithCache(mc)

	// Switch to :pc (flat PodClique list)
	m.forestResourceType = "pc"
	m.applySnapshot()
	m.rebuildResourcesTable()

	// Verify we have PodCliques in the forest
	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected PodCliques in forest")
	}

	// Rebuild events for the current selection — should dispatch based on selected row type
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
	m.rebuildEventsTable()

	// The selected PodClique should determine which events are shown.
	// The first row (cursor=0) is the first sorted PodClique.
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		t.Fatal("no selected row in resources table")
	}
	selectedPC := selectedRow[2]

	// Events should be for the selected PodClique, not for a PCS
	foundMatchingEvent := false
	for _, e := range m.allEvents {
		if e.Parent == selectedPC {
			foundMatchingEvent = true
		}
	}
	if len(m.allEvents) > 0 && !foundMatchingEvent {
		t.Errorf("events do not match selected PodClique %q; got %d events with parents: %v",
			selectedPC, len(m.allEvents), eventParents(m.allEvents))
	}

	// Navigate down and verify events change
	eventsBefore := len(m.allEvents)
	firstParent := ""
	if len(m.allEvents) > 0 {
		firstParent = m.allEvents[0].Parent
	}

	m = sendKey(m, tea.KeyDown)

	selectedRow = m.resourcesTable.SelectedRow()
	if len(selectedRow) >= 3 {
		newSelectedPC := selectedRow[2]
		if newSelectedPC == selectedPC {
			t.Skip("only one PodClique, can't test navigation")
		}

		// After navigation, events should have been rebuilt for the new selection
		newParent := ""
		if len(m.allEvents) > 0 {
			newParent = m.allEvents[0].Parent
		}

		// Either the events changed, or both have zero events, or the parent changed
		if eventsBefore > 0 && len(m.allEvents) > 0 && firstParent == newParent && firstParent == selectedPC {
			t.Errorf("events did not update after navigating to different PodClique: still showing events for %q", firstParent)
		}
	}
}

func TestForestPod_EventsUpdateOnSelection(t *testing.T) {
	mc := buildMockCacheWithEvents(map[string][]data.Event{
		"Pod/alpha-pcs-0-pc-worker-0": {
			{Type: "Normal", Kind: "Pod", Reason: "Scheduled", Age: "1m", From: "scheduler", Message: "pod-0 scheduled", Parent: "alpha-pcs-0-pc-worker-0"},
		},
		"Pod/alpha-pcs-0-pc-worker-1": {
			{Type: "Warning", Kind: "Pod", Reason: "BackOff", Age: "30s", From: "kubelet", Message: "pod-1 backoff", Parent: "alpha-pcs-0-pc-worker-1"},
		},
	})
	m := newTestModelWithCache(mc)

	// Switch to :pod (flat Pod list)
	m.forestResourceType = "pod"
	m.applySnapshot()
	m.rebuildResourcesTable()

	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected Pods in forest")
	}

	// Rebuild events for current selection
	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
	m.rebuildEventsTable()

	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		t.Fatal("no selected row in resources table")
	}
	selectedPod := selectedRow[2]

	// Events should be for the selected Pod
	for _, e := range m.allEvents {
		if e.Parent != selectedPod {
			t.Errorf("event parent %q doesn't match selected pod %q", e.Parent, selectedPod)
		}
	}
}

func TestForestPCSG_EventsMatchSelection(t *testing.T) {
	mc := buildMockCacheWithEvents(map[string][]data.Event{
		"PodCliqueScalingGroup/alpha-pcs-0-sg-prefill": {
			{Type: "Normal", Kind: "PodCliqueScalingGroup", Reason: "ScaledUp", Age: "3m", From: "controller", Message: "sg scaled", Parent: "alpha-pcs-0-sg-prefill"},
		},
	})
	m := newTestModelWithCache(mc)

	// Switch to :pcsg (flat PCSG list)
	m.forestResourceType = "pcsg"
	m.applySnapshot()
	m.rebuildResourcesTable()

	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected PCSGs in forest")
	}

	m.rebuildEventsFromSnapshot(m.cachedSnapshot)
	m.rebuildEventsTable()

	// Should have the PCSG event
	if len(m.allEvents) == 0 {
		t.Fatal("expected events for selected PCSG, got none")
	}
	if m.allEvents[0].Parent != "alpha-pcs-0-sg-prefill" {
		t.Errorf("expected event for alpha-pcs-0-sg-prefill, got parent=%q", m.allEvents[0].Parent)
	}
}

// eventParents returns a slice of event Parent values for debugging.
func eventParents(events []data.Event) []string {
	parents := make([]string, len(events))
	for i, e := range events {
		parents[i] = e.Parent
	}
	return parents
}

// Test that Lens stays "forest" when drilling from a flat list
// ---------------------------------------------------------------------------
// Comprehensive flat-list drill-in tests
// These verify that drilling from :pc, :pcsg, :pod actually populates child
// resources, shows correct breadcrumbs, and the lens stays "forest".
// ---------------------------------------------------------------------------

func TestFlatPC_DrillIn_PopulatesPodsAndBreadcrumb(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Switch to :pc
	m.forestResourceType = "pc"
	m.applySnapshot()
	m.rebuildResourcesTable()

	// Lens should say "forest" at top level
	if got := m.viewDisplayName(); got != "forest" {
		t.Fatalf("expected lens='forest', got %q", got)
	}

	// Verify we have PodCliques
	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected PodCliques in forest")
	}

	// Find the PodClique that has pods and select it
	pcWithPods := ""
	for _, r := range forest {
		if _, ok := mc.Snapshot().PodsByPodClique[r.Name]; ok {
			pcWithPods = r.Name
			break
		}
	}
	if pcWithPods == "" {
		t.Fatal("no PodClique with pods found in test data")
	}

	// Select and drill into it
	for i, r := range forest {
		if r.Name == pcWithPods {
			m.resourcesTable.SetCursor(i)
			break
		}
	}
	m = sendKey(m, tea.KeyEnter)

	// Should be in PodCliqueView
	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Lens should still say "forest"
	if got := m.viewDisplayName(); got != "forest" {
		t.Fatalf("expected lens='forest' after drill, got %q", got)
	}

	// Resources table should have pods (not be empty!)
	viewKey := m.getCurrentViewKey()
	resources := m.allResources[viewKey]
	if len(resources) == 0 {
		t.Fatalf("expected pods in %q after drilling from flat PC list, got 0 resources", viewKey)
	}
	for _, r := range resources {
		if r.Type != "Pod" {
			t.Errorf("expected Pod type in PodClique drill-in, got %q", r.Type)
		}
	}

	// Breadcrumb should NOT contain empty PCS segments
	bc := m.renderBreadcrumb()
	if strings.Contains(bc, "replica-") {
		t.Errorf("breadcrumb should not contain 'replica-' for flat-list drill-in, got: %s", bc)
	}
	// Should contain the PodClique name
	if !strings.Contains(bc, pcWithPods) {
		t.Errorf("breadcrumb should contain PodClique name %q, got: %s", pcWithPods, bc)
	}

	// Navigate back — should return to ForestView with pc list
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.forestResourceType != "pc" {
		t.Fatalf("expected forestResourceType='pc' preserved, got %q", m.forestResourceType)
	}
}

func TestFlatPCSG_DrillIn_PopulatesReplicasAndBreadcrumb(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Switch to :pcsg
	m.forestResourceType = "pcsg"
	m.applySnapshot()
	m.rebuildResourcesTable()

	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected PCSGs in forest")
	}

	// Drill into the first PCSG
	m = sendKey(m, tea.KeyEnter)

	// Should be in PCSGView or PCSGReplicaView (auto-skip if single replica)
	validView := m.viewState.ViewType == data.PodCliqueScalingGroupView ||
		m.viewState.ViewType == data.PodCliqueScalingGroupReplicaView
	if !validView {
		t.Fatalf("expected PCSGView or PCSGReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Lens should say "forest"
	if got := m.viewDisplayName(); got != "forest" {
		t.Fatalf("expected lens='forest' after PCSG drill, got %q", got)
	}

	// Resources table should have content (replicas or PodCliques)
	viewKey := m.getCurrentViewKey()
	resources := m.allResources[viewKey]
	if len(resources) == 0 {
		t.Fatalf("expected resources in %q after drilling from flat PCSG list, got 0", viewKey)
	}

	// Breadcrumb should contain the PCSG name, not empty PCS segments
	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, m.viewState.SelectedScalingGroup) {
		t.Errorf("breadcrumb should contain PCSG name %q, got: %s", m.viewState.SelectedScalingGroup, bc)
	}

	// Navigate back
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestFlatPCSG_DeepDrill_IntoPodClique(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Switch to :pcsg and drill into a PCSG
	m.forestResourceType = "pcsg"
	m.applySnapshot()
	m.rebuildResourcesTable()
	m = sendKey(m, tea.KeyEnter) // into PCSG (auto-skip to replica if single, else PCSGView)

	// If we landed on PCSGView (multi-replica), drill into a replica first
	if m.viewState.ViewType == data.PodCliqueScalingGroupView {
		viewKey := m.getCurrentViewKey()
		resources := m.allResources[viewKey]
		if len(resources) == 0 {
			t.Skip("no PCSG replicas, skipping deep drill test")
		}
		m = sendKey(m, tea.KeyEnter) // into PCSG replica
	}

	// Should now be in PCSGReplicaView
	if m.viewState.ViewType != data.PodCliqueScalingGroupReplicaView {
		t.Fatalf("expected PodCliqueScalingGroupReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Get PodCliques in this replica
	viewKey := m.getCurrentViewKey()
	resources := m.allResources[viewKey]
	if len(resources) == 0 {
		t.Skip("no PodCliques in PCSG replica, skipping deep drill test")
	}

	m = sendKey(m, tea.KeyEnter) // into PodClique

	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView after deep drill, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Should have pods
	viewKey = m.getCurrentViewKey()
	resources = m.allResources[viewKey]
	if len(resources) == 0 {
		t.Fatalf("expected pods in %q after deep drill from PCSG, got 0", viewKey)
	}

	// Lens should still say "forest"
	if got := m.viewDisplayName(); got != "forest" {
		t.Fatalf("expected lens='forest' after deep drill, got %q", got)
	}

	// Breadcrumb should contain PCSG and PC names, but no empty PCS
	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, m.viewState.SelectedPodClique) {
		t.Errorf("breadcrumb should contain PodClique name, got: %s", bc)
	}
	if !strings.Contains(bc, m.viewState.SelectedScalingGroup) {
		t.Errorf("breadcrumb should contain PCSG name, got: %s", bc)
	}

	// Back nav should go through the PCSG hierarchy back to forest
	m = sendKey(m, tea.KeyEsc) // back to PCSG replica view
	validBack := m.viewState.ViewType == data.PodCliqueScalingGroupReplicaView ||
		m.viewState.ViewType == data.PodCliqueScalingGroupView
	if !validBack {
		t.Fatalf("expected PCSGReplicaView or PCSGView after Esc, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Keep pressing Esc until we reach ForestView
	for m.viewState.ViewType != data.ForestView {
		m = sendKey(m, tea.KeyEsc)
	}
	if m.forestResourceType != "pcsg" {
		t.Fatalf("expected forestResourceType='pcsg' preserved, got %q", m.forestResourceType)
	}
}

func TestFlatPod_DrillIn_ShowsPodViewAndBreadcrumb(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Switch to :pod
	m.forestResourceType = "pod"
	m.applySnapshot()
	m.rebuildResourcesTable()

	forest := m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected Pods in forest")
	}

	// Drill into the first Pod
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.viewState.ViewType != data.PodView {
		t.Fatalf("expected PodView after drilling from flat Pod list, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Lens should say "forest"
	if got := m.viewDisplayName(); got != "forest" {
		t.Fatalf("expected lens='forest' in PodView, got %q", got)
	}

	// Breadcrumb should show Pods > pod-name, not empty PCS segments
	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, m.viewState.SelectedPod) {
		t.Errorf("breadcrumb should contain pod name %q, got: %s", m.viewState.SelectedPod, bc)
	}
	if strings.Contains(bc, "replica-") {
		t.Errorf("breadcrumb should not contain 'replica-' for flat pod drill, got: %s", bc)
	}

	// Should have returned a loadPodYAML command
	if cmd == nil {
		t.Error("expected loadPodYAMLCmd after drilling into Pod")
	}

	// Navigate back
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc from Pod, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.forestResourceType != "pod" {
		t.Fatalf("expected forestResourceType='pod' preserved, got %q", m.forestResourceType)
	}
}

func TestFlatPC_DrillIntoPod_FullRoundTrip(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// :pc → drill PodClique → drill Pod → back → back
	m.forestResourceType = "pc"
	m.applySnapshot()
	m.rebuildResourcesTable()

	// Find a PodClique with pods
	forest := m.allResources["forest"]
	for i, r := range forest {
		if _, ok := mc.Snapshot().PodsByPodClique[r.Name]; ok {
			m.resourcesTable.SetCursor(i)
			break
		}
	}

	// Drill into PodClique
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Verify pods are populated
	viewKey := m.getCurrentViewKey()
	pods := m.allResources[viewKey]
	if len(pods) == 0 {
		t.Fatal("expected pods after drilling from flat PC list")
	}

	// Drill into first Pod
	m, _ = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.viewState.ViewType != data.PodView {
		t.Fatalf("expected PodView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Back to PodCliqueView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView after Esc from PodView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Pods should still be there
	viewKey = m.getCurrentViewKey()
	pods = m.allResources[viewKey]
	if len(pods) == 0 {
		t.Fatal("expected pods still populated after back from PodView")
	}

	// Back to ForestView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after second Esc, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.forestResourceType != "pc" {
		t.Fatalf("expected forestResourceType='pc', got %q", m.forestResourceType)
	}
}

// ---------------------------------------------------------------------------
// Esc from non-default forest resource type resets to PCS
// ---------------------------------------------------------------------------

func TestEscFromFlatPC_ResetsToPCS(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Switch to :pc
	m.forestResourceType = "pc"
	m.applySnapshot()
	m.rebuildResourcesTable()

	// Verify we're showing PodCliques
	forest := m.allResources["forest"]
	if len(forest) == 0 || forest[0].Type != "PodClique" {
		t.Fatal("expected PodCliques in forest")
	}

	// Press Esc — should reset to PCS view
	m = sendKey(m, tea.KeyEsc)

	if m.forestResourceType != "pcs" {
		t.Fatalf("expected forestResourceType reset to 'pcs', got %q", m.forestResourceType)
	}
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Forest should now contain PodCliqueSets
	forest = m.allResources["forest"]
	if len(forest) == 0 {
		t.Fatal("expected PodCliqueSets in forest after reset")
	}
	if forest[0].Type != "PodCliqueSet" {
		t.Fatalf("expected PodCliqueSet type after reset, got %q", forest[0].Type)
	}
}

func TestEscFromFlatPCSG_ResetsToPCS(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m.forestResourceType = "pcsg"
	m.applySnapshot()
	m.rebuildResourcesTable()

	m = sendKey(m, tea.KeyEsc)

	if m.forestResourceType != "pcs" {
		t.Fatalf("expected forestResourceType reset to 'pcs', got %q", m.forestResourceType)
	}
	forest := m.allResources["forest"]
	if len(forest) > 0 && forest[0].Type != "PodCliqueSet" {
		t.Fatalf("expected PodCliqueSet type after reset, got %q", forest[0].Type)
	}
}

func TestEscFromFlatPod_ResetsToPCS(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	m.forestResourceType = "pod"
	m.applySnapshot()
	m.rebuildResourcesTable()

	m = sendKey(m, tea.KeyEsc)

	if m.forestResourceType != "pcs" {
		t.Fatalf("expected forestResourceType reset to 'pcs', got %q", m.forestResourceType)
	}
}

func TestEscAtDefaultPCS_DoesNothing(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// Already at default pcs
	before := m.forestResourceType
	m = sendKey(m, tea.KeyEsc)

	if m.forestResourceType != before {
		t.Fatalf("expected forestResourceType unchanged at %q, got %q", before, m.forestResourceType)
	}
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView unchanged, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestEscFromDrilledFlatPC_BackToPC_ThenToPCS(t *testing.T) {
	mc := buildFullMockCache()
	m := newTestModelWithCache(mc)

	// :pc, drill into a PodClique
	m.forestResourceType = "pc"
	m.applySnapshot()
	m.rebuildResourcesTable()

	// Find a PC with pods
	forest := m.allResources["forest"]
	for i, r := range forest {
		if _, ok := mc.Snapshot().PodsByPodClique[r.Name]; ok {
			m.resourcesTable.SetCursor(i)
			break
		}
	}
	m = sendKey(m, tea.KeyEnter) // drill into PodClique
	if m.viewState.ViewType != data.PodCliqueView {
		t.Fatalf("expected PodCliqueView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Esc back to ForestView (still pc)
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.forestResourceType != "pc" {
		t.Fatalf("expected forestResourceType='pc' after first Esc, got %q", m.forestResourceType)
	}

	// Esc again — should reset to pcs
	m = sendKey(m, tea.KeyEsc)
	if m.forestResourceType != "pcs" {
		t.Fatalf("expected forestResourceType='pcs' after second Esc, got %q", m.forestResourceType)
	}
	if m.allResources["forest"][0].Type != "PodCliqueSet" {
		t.Fatalf("expected PodCliqueSets after reset, got %q", m.allResources["forest"][0].Type)
	}
}

// ---------------------------------------------------------------------------
// Namespace filtering tests
// ---------------------------------------------------------------------------

func TestWithNamespaceOption(t *testing.T) {
	mc := data.NewMockGlobalCache()
	m := NewModel(mc, WithNamespace("gpu-stack"))
	if m.namespace != "gpu-stack" {
		t.Errorf("expected namespace 'gpu-stack', got %q", m.namespace)
	}
	if m.allNamespaces {
		t.Error("expected allNamespaces=false when namespace is set")
	}
}

func TestWithAllNamespacesOption(t *testing.T) {
	mc := data.NewMockGlobalCache()
	m := NewModel(mc, WithAllNamespaces(true))
	if !m.allNamespaces {
		t.Error("expected allNamespaces=true")
	}
	if m.namespace != "" {
		t.Errorf("expected empty namespace, got %q", m.namespace)
	}
}

func TestDefaultAllNamespacesIsTrue(t *testing.T) {
	mc := data.NewMockGlobalCache()
	m := NewModel(mc)
	if !m.allNamespaces {
		t.Error("expected allNamespaces=true by default")
	}
}

func TestNamespaceFilteringInPopulateForestResources(t *testing.T) {
	mc := data.NewMockGlobalCache()
	snap := mc.Snapshot()
	snap.PodCliqueSets = []data.Resource{
		{Name: "pcs-a", Type: "PodCliqueSet", Namespace: "gpu-stack", Ready: "1/1", Scheduled: "1/1"},
		{Name: "pcs-b", Type: "PodCliqueSet", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
		{Name: "pcs-c", Type: "PodCliqueSet", Namespace: "gpu-stack", Ready: "2/2", Scheduled: "2/2"},
	}
	mc.SetSnapshot(snap)

	// All namespaces: should see all 3
	m := NewModel(mc)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	if len(m.allResources["forest"]) != 3 {
		t.Fatalf("expected 3 resources with all namespaces, got %d", len(m.allResources["forest"]))
	}

	// Scoped to gpu-stack: should see 2
	m2 := NewModel(mc, WithNamespace("gpu-stack"))
	m2 = mustApply(m2, tea.WindowSizeMsg{Width: 120, Height: 40})
	m2.cacheSynced = true
	m2 = mustApply(m2, CacheSyncedMsg{})
	if len(m2.allResources["forest"]) != 2 {
		t.Fatalf("expected 2 resources in gpu-stack namespace, got %d", len(m2.allResources["forest"]))
	}
	for _, r := range m2.allResources["forest"] {
		if r.Namespace != "gpu-stack" {
			t.Errorf("expected namespace 'gpu-stack', got %q for resource %s", r.Namespace, r.Name)
		}
	}

	// Scoped to default: should see 1
	m3 := NewModel(mc, WithNamespace("default"))
	m3 = mustApply(m3, tea.WindowSizeMsg{Width: 120, Height: 40})
	m3.cacheSynced = true
	m3 = mustApply(m3, CacheSyncedMsg{})
	if len(m3.allResources["forest"]) != 1 {
		t.Fatalf("expected 1 resource in default namespace, got %d", len(m3.allResources["forest"]))
	}
	if m3.allResources["forest"][0].Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", m3.allResources["forest"][0].Namespace)
	}

	// Scoped to nonexistent namespace: should see 0
	m4 := NewModel(mc, WithNamespace("nonexistent"))
	m4 = mustApply(m4, tea.WindowSizeMsg{Width: 120, Height: 40})
	m4.cacheSynced = true
	m4 = mustApply(m4, CacheSyncedMsg{})
	if len(m4.allResources["forest"]) != 0 {
		t.Fatalf("expected 0 resources in nonexistent namespace, got %d", len(m4.allResources["forest"]))
	}
}

func TestHeaderShowsNamespaceAll(t *testing.T) {
	m := newTestModel(nil)
	header := m.renderHeaderFrame()
	if !strings.Contains(header, "Namespace:") {
		t.Errorf("expected header to contain 'Namespace:' label, got:\n%s", header)
	}
	if !strings.Contains(header, "all") {
		t.Errorf("expected header to show 'all' for default namespace scope, got:\n%s", header)
	}
}

func TestHeaderShowsSpecificNamespace(t *testing.T) {
	mc := data.NewMockGlobalCache()
	m := NewModel(mc, WithNamespace("gpu-stack"))
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.cacheSynced = true
	m = mustApply(m, CacheSyncedMsg{})
	header := m.renderHeaderFrame()
	if !strings.Contains(header, "gpu-stack") {
		t.Errorf("expected header to show 'gpu-stack', got:\n%s", header)
	}
}
