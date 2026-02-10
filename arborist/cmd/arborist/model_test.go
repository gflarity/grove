package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestModel creates a Model backed by a MockProvider pre-loaded with the
// given PodCliqueSet resources. It also sends a WindowSizeMsg so the model is
// considered "ready" and will render a full view.
func newTestModel(pcsResources []Resource) Model {
	mp := NewMockProvider()
	mp.PodCliqueSets = pcsResources
	m := NewModel(mp)
	// Simulate initial window size so the model is ready
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// newTestModelWithProvider creates a Model backed by a fully configured
// MockProvider. Sends a WindowSizeMsg to make the model ready.
func newTestModelWithProvider(mp *MockProvider) Model {
	m := NewModel(mp)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
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
					// Recursively handle nested batches
					if _, isBatch := subMsg.(tea.BatchMsg); isBatch {
						m = executeCmdAndApply(m, subCmd)
					} else {
						var nextCmd tea.Cmd
						m, nextCmd = applyMsg(m, subMsg)
						// Also execute any resulting commands from the handler
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

func samplePCSResources() []Resource {
	return []Resource{
		{Name: "alpha-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "3/3", Scheduled: "3/3", Topology: "rack"},
		{Name: "beta-pcs", Type: "PodCliqueSet", Namespace: "staging", Ready: "2/3", Scheduled: "3/3", Topology: "zone"},
		{Name: "gamma-pcs", Type: "PodCliqueSet", Namespace: "default", Ready: "0/5", Scheduled: "5/5", Topology: "N/A"},
	}
}

func sampleEvents() []Event {
	return []Event{
		{Type: "Normal", Reason: "Scaled", Age: "5m", From: "controller", Message: "Scaled up to 3", Parent: "alpha-pcs"},
		{Type: "Warning", Reason: "Unschedulable", Age: "2m", From: "scheduler", Message: "No nodes available", Parent: "beta-pcs"},
		{Type: "Normal", Reason: "Created", Age: "10m", From: "controller", Message: "Created pod", Parent: "alpha-pcs"},
	}
}

func sampleReplicaChildren() ([]Resource, []Resource) {
	scalingGroups := []Resource{
		{Name: "alpha-pcs-0-sg-prefill", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "2/2", Scheduled: "2/2", Topology: "block"},
	}
	podCliques := []Resource{
		{Name: "alpha-pcs-0-standalone-pc", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1", Topology: "rack"},
	}
	return scalingGroups, podCliques
}

func samplePods() []Resource {
	return []Resource{
		{Name: "alpha-pcs-0-pc-worker-0", Type: "Pod", Namespace: "default", Ready: "1/1", Scheduled: "Running", Topology: "(rack)"},
		{Name: "alpha-pcs-0-pc-worker-1", Type: "Pod", Namespace: "default", Ready: "1/1", Scheduled: "Running", Topology: "(rack)"},
		{Name: "alpha-pcs-0-pc-worker-2", Type: "Pod", Namespace: "default", Ready: "0/1", Scheduled: "Pending", Topology: "(rack)"},
	}
}

// buildFullMockProvider creates a MockProvider with a complete hierarchy for
// "alpha-pcs": Forest -> PCS (1 replica) -> Replica children -> PodClique -> Pods.
func buildFullMockProvider() *MockProvider {
	mp := NewMockProvider()
	mp.PodCliqueSets = samplePCSResources()

	// alpha-pcs has 1 replica
	mp.ReplicaIndexes["default/alpha-pcs"] = []string{"0"}
	// beta-pcs has 2 replicas
	mp.ReplicaIndexes["staging/beta-pcs"] = []string{"0", "1"}

	scalingGroups, podCliques := sampleReplicaChildren()
	mp.ScalingGroups["default/alpha-pcs/0"] = scalingGroups
	mp.ReplicaPodCliques["default/alpha-pcs/0"] = podCliques

	// PodClique children (pods)
	mp.PodCliquePods["default/alpha-pcs-0-standalone-pc"] = samplePods()

	// Pod YAML
	mp.PodYAMLs["default/alpha-pcs-0-pc-worker-0"] = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: alpha-pcs-0-pc-worker-0\n"

	// Events
	mp.Events["pcs/default/alpha-pcs"] = sampleEvents()[:2]
	mp.Events["replica/default/alpha-pcs/0"] = sampleEvents()[:1]
	mp.Events["pc/default/alpha-pcs-0-standalone-pc"] = sampleEvents()[2:]

	return mp
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
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Simulate Init() -> forest data load -> deliver ForestDataMsg
	initCmd := m.Init()
	m = executeCmdAndApply(m, initCmd)

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
	m := newTestModel([]Resource{})

	// Deliver empty forest data
	m = mustApply(m, ForestDataMsg{Resources: []Resource{}})

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
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Load forest data
	m = executeCmdAndApply(m, m.Init())

	// Verify we're at ForestView
	if m.viewState.viewType != ForestView {
		t.Fatalf("expected ForestView, got %s", viewTypeName(m.viewState.viewType))
	}

	// Press Enter to drill into first PCS (alpha-pcs).
	// This dispatches batch commands (loadTopologyInfo, loadPodInfo, loadReplicas).
	var cmd tea.Cmd
	m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Verify the PCS was selected
	if m.viewState.selectedPodCliqueSet != "alpha-pcs" {
		t.Fatalf("expected selectedPodCliqueSet=alpha-pcs, got %s", m.viewState.selectedPodCliqueSet)
	}

	// Execute all batch commands (loadTopologyInfo, loadPodInfo, loadReplicas)
	// This triggers ReplicaDataMsg handling which does single-replica skip.
	m = executeCmdAndApply(m, cmd)

	// alpha-pcs has 1 replica, so it should skip to PodCliqueSetReplicaView
	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView after single-replica skip, got %s", viewTypeName(m.viewState.viewType))
	}

	// Breadcrumb should show the path
	assertView(t, m, []string{"Forest", "alpha-pcs", "replica-0"})

	// Now navigate back
	m = sendKey(m, tea.KeyEsc)

	// Since alpha-pcs has 1 replica, back should go to ForestView directly
	if m.viewState.viewType != ForestView {
		t.Fatalf("expected ForestView after back from single-replica PCS, got %s", viewTypeName(m.viewState.viewType))
	}
}

// TestNavigationDrillDown_ViaMessages tests the navigation state machine by
// directly sending messages (bypassing table selection and batch commands).
// This provides a clean unit test of the Update handlers.
func TestNavigationDrillDown_ViaMessages(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Start at ForestView
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})
	if m.viewState.viewType != ForestView {
		t.Fatalf("expected ForestView, got %s", viewTypeName(m.viewState.viewType))
	}

	// Simulate navigating into alpha-pcs: set selectedPodCliqueSet, then send ReplicaDataMsg
	m.viewState.selectedPodCliqueSet = "alpha-pcs"

	// ReplicaDataMsg with 1 replica triggers skip to PodCliqueSetReplicaView
	m = mustApply(m, ReplicaDataMsg{
		PCSName:        "alpha-pcs",
		Namespace:      "default",
		ReplicaIndexes: []string{"0"},
		ScalingGroupsByReplica: map[string][]Resource{
			"0": {{Name: "sg1", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "1/1", Scheduled: "1/1"}},
		},
		PodCliquesByReplica: map[string][]Resource{},
	})
	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", viewTypeName(m.viewState.viewType))
	}

	// ReplicaChildrenMsg populates the replica view
	scalingGroups, podCliques := sampleReplicaChildren()
	m = mustApply(m, ReplicaChildrenMsg{
		PCSName:       "alpha-pcs",
		Namespace:     "default",
		ReplicaIndex:  "0",
		ScalingGroups: scalingGroups,
		PodCliques:    podCliques,
	})
	assertView(t, m, []string{"Forest", "alpha-pcs", "replica-0"})

	// Navigate back from PodCliqueSetReplicaView -> ForestView (single replica skip)
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != ForestView {
		t.Fatalf("expected ForestView after back, got %s", viewTypeName(m.viewState.viewType))
	}
}

// ---------------------------------------------------------------------------
// 6.4 Test: Single-replica skip behavior
// ---------------------------------------------------------------------------

func TestSingleReplicaSkip(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Load forest data
	m = executeCmdAndApply(m, m.Init())

	// Press Enter to drill into alpha-pcs (has 1 replica)
	var cmd tea.Cmd
	m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Execute batch commands
	m = executeCmdAndApply(m, cmd)

	// Should skip directly to PodCliqueSetReplicaView
	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView (single-replica skip), got %s", viewTypeName(m.viewState.viewType))
	}
	if m.viewState.selectedReplicaIndex != "0" {
		t.Fatalf("expected selectedReplicaIndex=0, got %s", m.viewState.selectedReplicaIndex)
	}

	// Press Esc - should return to ForestView (not PodCliqueSetView)
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != ForestView {
		t.Fatalf("expected ForestView after Esc from single-replica skip, got %s", viewTypeName(m.viewState.viewType))
	}
}

// TestSingleReplicaSkip_ViaMessages tests skip behavior via direct messages.
func TestSingleReplicaSkip_ViaMessages(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.selectedPodCliqueSet = "test-pcs"

	// Send ReplicaDataMsg with exactly 1 replica
	m = mustApply(m, ReplicaDataMsg{
		PCSName:                "test-pcs",
		Namespace:              "default",
		ReplicaIndexes:         []string{"0"},
		ScalingGroupsByReplica: map[string][]Resource{"0": {}},
		PodCliquesByReplica:    map[string][]Resource{"0": {}},
	})

	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", viewTypeName(m.viewState.viewType))
	}
	if m.viewState.selectedReplicaIndex != "0" {
		t.Fatalf("expected selectedReplicaIndex=0, got %s", m.viewState.selectedReplicaIndex)
	}

	// Pressing Esc should go back to ForestView (single-replica entry in allResources)
	pcsKey := "PodCliqueSet/test-pcs"
	if len(m.allResources[pcsKey]) != 1 {
		t.Fatalf("expected 1 replica resource tracked, got %d", len(m.allResources[pcsKey]))
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != ForestView {
		t.Fatalf("expected ForestView, got %s", viewTypeName(m.viewState.viewType))
	}
}

func TestMultiReplicaShowsReplicaList(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.selectedPodCliqueSet = "beta-pcs"

	// Send ReplicaDataMsg with 2 replicas
	m = mustApply(m, ReplicaDataMsg{
		PCSName:        "beta-pcs",
		Namespace:      "staging",
		ReplicaIndexes: []string{"0", "1"},
		ScalingGroupsByReplica: map[string][]Resource{
			"0": {{Name: "sg-0", Type: "PodCliqueScalingGroup", Namespace: "staging", Ready: "1/1", Scheduled: "1/1"}},
			"1": {{Name: "sg-1", Type: "PodCliqueScalingGroup", Namespace: "staging", Ready: "1/1", Scheduled: "1/1"}},
		},
		PodCliquesByReplica: map[string][]Resource{},
	})

	// beta-pcs has 2 replicas, so should show PodCliqueSetView with replica list
	if m.viewState.viewType != PodCliqueSetView {
		t.Fatalf("expected PodCliqueSetView for multi-replica PCS, got %s", viewTypeName(m.viewState.viewType))
	}

	// Should have 2 replica resources stored
	pcsKey := "PodCliqueSet/beta-pcs"
	if len(m.allResources[pcsKey]) != 2 {
		t.Fatalf("expected 2 replica resources, got %d", len(m.allResources[pcsKey]))
	}

	// Back should go to ForestView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != ForestView {
		t.Fatalf("expected ForestView after Esc from PodCliqueSetView, got %s", viewTypeName(m.viewState.viewType))
	}
}

// ---------------------------------------------------------------------------
// 6.5 Test: Filter functionality
// ---------------------------------------------------------------------------

func TestFilterFunctionality(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Load forest data
	m = executeCmdAndApply(m, m.Init())

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
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

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
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Load forest data
	m = executeCmdAndApply(m, m.Init())

	// Initial active pane should be Resources
	if m.activePane != ResourcesPane {
		t.Fatalf("expected initial pane to be ResourcesPane, got %d", m.activePane)
	}

	// Press Tab to switch to Events
	m = sendKey(m, tea.KeyTab)
	if m.activePane != EventsPane {
		t.Fatalf("expected EventsPane after Tab, got %d", m.activePane)
	}

	// Press Tab again to switch back to Resources
	m = sendKey(m, tea.KeyTab)
	if m.activePane != ResourcesPane {
		t.Fatalf("expected ResourcesPane after second Tab, got %d", m.activePane)
	}
}

func TestPaneSwitchingDuringFilter(t *testing.T) {
	m := newTestModel(samplePCSResources())
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

	// Activate filter
	m = sendRune(m, '/')
	if !m.filterActive {
		t.Fatal("expected filter active")
	}

	// Tab should still switch panes
	m = sendKey(m, tea.KeyTab)
	if m.activePane != EventsPane {
		t.Fatalf("expected pane switch during filter mode, got %d", m.activePane)
	}
}

func TestPaneSwitchingUpdatesTableFocus(t *testing.T) {
	m := newTestModel(samplePCSResources())
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

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
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Put model into PodView state manually
	m.viewState.viewType = PodView
	m.viewState.selectedPod = "alpha-pcs-0-pc-worker-0"
	m.allEvents = []Event{
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
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	m.viewState.viewType = ForestView
	m.allEvents = sampleEvents()

	filtered := m.getFilteredEvents()
	if len(filtered) != len(sampleEvents()) {
		t.Fatalf("expected all %d events in ForestView, got %d", len(sampleEvents()), len(filtered))
	}
}

func TestEventsAreLoadedForPCS(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Load forest data - this should also trigger loading events for the first PCS
	m = executeCmdAndApply(m, m.Init())

	// After loading forest data and events, allEvents should be populated
	if len(m.allEvents) == 0 {
		t.Fatal("expected events to be loaded after forest data init")
	}
}

// ---------------------------------------------------------------------------
// 6.8 Test: Pod YAML view
// ---------------------------------------------------------------------------

func TestPodYAMLView_ViaMessages(t *testing.T) {
	m := newTestModel(nil)

	// Set up at PodCliqueView with pods
	m.viewState.viewType = PodCliqueView
	m.viewState.selectedPodCliqueSet = "alpha-pcs"
	m.viewState.selectedReplicaIndex = "0"
	m.viewState.selectedPodClique = "alpha-pcs-0-standalone-pc"

	// Manually set up the view to navigate into PodView
	m.viewState.viewType = PodView
	m.viewState.selectedPod = "alpha-pcs-0-pc-worker-0"

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
	if m.viewState.viewType != PodCliqueView {
		t.Fatalf("expected PodCliqueView after Esc from PodView, got %s", viewTypeName(m.viewState.viewType))
	}
}

func TestPodYAMLView_FullDrillDown(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Load forest and drill all the way to pods via messages
	m = executeCmdAndApply(m, m.Init())

	// Navigate into alpha-pcs
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdAndApply(m, cmd)

	// Should be at PodCliqueSetReplicaView (single replica skip)
	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", viewTypeName(m.viewState.viewType))
	}

	// Navigate into the first child resource (PodCliqueScalingGroup or PodClique)
	m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		m = executeCmdAndApply(m, cmd)
	}

	// Continue drilling until we reach PodView or can't go further
	for m.viewState.viewType != PodView && m.viewState.viewType != ForestView {
		prevViewType := m.viewState.viewType
		m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil {
			m = executeCmdAndApply(m, cmd)
		}
		// If view didn't change, we're stuck - break to avoid infinite loop
		if m.viewState.viewType == prevViewType {
			break
		}
	}

	if m.viewState.viewType == PodView {
		// Should have YAML data
		if m.viewState.selectedPod == "" {
			t.Fatal("expected selectedPod to be set in PodView")
		}

		// Go back
		m = sendKey(m, tea.KeyEsc)
		if m.viewState.viewType != PodCliqueView {
			t.Fatalf("expected PodCliqueView after Esc, got %s", viewTypeName(m.viewState.viewType))
		}
	}
}

func TestPodViewportContent(t *testing.T) {
	m := newTestModel(nil)

	// Manually put model in PodView
	m.viewState.viewType = PodView
	m.viewState.selectedPod = "my-pod"

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

func TestErrorHandling_ForestDataError(t *testing.T) {
	mp := NewMockProvider()
	mp.Errors["GetAllPodCliqueSets"] = errForTest("cluster unreachable")
	m := newTestModelWithProvider(mp)

	// Execute Init which calls loadForestData
	m = executeCmdAndApply(m, m.Init())

	// The model should have recorded the error
	if m.lastError == nil {
		t.Fatal("expected lastError to be set after forest data error")
	}
	if !strings.Contains(m.lastError.Error(), "cluster unreachable") {
		t.Errorf("expected error message to contain 'cluster unreachable', got %q", m.lastError.Error())
	}

	// View should still render without panic
	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view even after error")
	}
}

func TestErrorHandling_GenericErrorMsg(t *testing.T) {
	m := newTestModel(nil)

	m = mustApply(m, ErrorMsg{Operation: "test-op", Err: errForTest("something broke")})

	if m.lastError == nil {
		t.Fatal("expected lastError to be set")
	}
}

func TestErrorHandling_EventsError(t *testing.T) {
	m := newTestModel(nil)

	m = mustApply(m, EventsMsg{Err: errForTest("events fetch failed")})

	if m.lastError == nil {
		t.Fatal("expected lastError to be set after events error")
	}
	// Events should be empty, not nil
	if m.allEvents == nil {
		t.Fatal("expected allEvents to be empty slice, not nil")
	}
}

func TestErrorHandling_PodYAMLError(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.viewType = PodView
	m.viewState.selectedPod = "broken-pod"

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

func TestErrorHandling_ReplicaDataError(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.selectedPodCliqueSet = "my-pcs"

	m = mustApply(m, ReplicaDataMsg{
		PCSName:   "my-pcs",
		Namespace: "default",
		Err:       errForTest("replica fetch failed"),
	})

	if m.lastError == nil {
		t.Fatal("expected lastError to be set")
	}
}

func TestErrorHandling_ReplicaChildrenError(t *testing.T) {
	m := newTestModel(nil)

	m = mustApply(m, ReplicaChildrenMsg{
		PCSName:      "my-pcs",
		Namespace:    "default",
		ReplicaIndex: "0",
		Err:          errForTest("children fetch failed"),
	})

	if m.lastError == nil {
		t.Fatal("expected lastError to be set")
	}
}

func TestErrorHandling_TopologyInfoError(t *testing.T) {
	m := newTestModel(nil)

	m = mustApply(m, TopologyInfoMsg{
		PCSName:   "my-pcs",
		Namespace: "default",
		Err:       errForTest("topology fetch failed"),
	})

	// cachedTopologyInfo should be nil
	if m.cachedTopologyInfo != nil {
		t.Fatal("expected cachedTopologyInfo to be nil after error")
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
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

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
	mp := NewMockProvider()
	m := NewModel(mp)

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
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

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
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

	// Esc at forest view should not change anything
	before := m.viewState.viewType
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != before {
		t.Fatalf("expected viewType to remain %s, got %s", viewTypeName(before), viewTypeName(m.viewState.viewType))
	}
}

func TestNavigateBackClearsFilter(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Set up at PodCliqueSetReplicaView manually
	m.viewState.viewType = PodCliqueSetReplicaView
	m.viewState.selectedPodCliqueSet = "alpha-pcs"
	m.viewState.selectedReplicaIndex = "0"
	m.filterText = "something"

	// Navigate back
	m = sendKey(m, tea.KeyEsc)

	// Filter should be cleared
	if m.filterText != "" {
		t.Fatalf("expected filter to be cleared after navigateBack, got %q", m.filterText)
	}
}

func TestNavigateIntoClearsFilter(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Load forest data and set a filter
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})
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
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Set up deep navigation state manually
	m.viewState = ViewState{
		viewType:             PodView,
		selectedPodCliqueSet: "alpha-pcs",
		selectedReplicaIndex: "0",
		selectedPodClique:    "alpha-pcs-0-standalone-pc",
		selectedPod:          "alpha-pcs-0-pc-worker-0",
	}

	// PodView -> Esc -> PodCliqueView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != PodCliqueView {
		t.Fatalf("expected PodCliqueView, got %s", viewTypeName(m.viewState.viewType))
	}

	// PodCliqueView -> Esc -> PodCliqueSetReplicaView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", viewTypeName(m.viewState.viewType))
	}

	// PodCliqueSetReplicaView -> Esc -> ForestView (single replica)
	// Need allResources with single replica entry for the skip logic
	m.allResources["PodCliqueSet/alpha-pcs"] = []Resource{{
		Name: "alpha-pcs-replica-0", Type: "PodCliqueSetReplica", Namespace: "default",
	}}
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != ForestView {
		t.Fatalf("expected ForestView, got %s", viewTypeName(m.viewState.viewType))
	}
}

func TestNavigateBackFromPCSGView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = ViewState{
		viewType:             PodCliqueScalingGroupView,
		selectedPodCliqueSet: "my-pcs",
		selectedReplicaIndex: "0",
		selectedScalingGroup: "my-sg",
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", viewTypeName(m.viewState.viewType))
	}
	if m.viewState.selectedScalingGroup != "" {
		t.Fatalf("expected selectedScalingGroup cleared, got %q", m.viewState.selectedScalingGroup)
	}
}

func TestNavigateBackFromPodCliqueView_WithScalingGroup(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = ViewState{
		viewType:             PodCliqueView,
		selectedPodCliqueSet: "my-pcs",
		selectedReplicaIndex: "0",
		selectedScalingGroup: "my-sg",
		selectedPodClique:    "my-pc",
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != PodCliqueScalingGroupView {
		t.Fatalf("expected PodCliqueScalingGroupView, got %s", viewTypeName(m.viewState.viewType))
	}
}

func TestNavigateBackFromPodCliqueView_WithoutScalingGroup(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = ViewState{
		viewType:             PodCliqueView,
		selectedPodCliqueSet: "my-pcs",
		selectedReplicaIndex: "0",
		selectedPodClique:    "my-pc",
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", viewTypeName(m.viewState.viewType))
	}
}

func TestHandleReplicaDataMsg(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Simulate being about to navigate into a PCS
	m.viewState.viewType = ForestView
	m.viewState.selectedPodCliqueSet = "alpha-pcs"

	// Send ReplicaDataMsg with 1 replica -> should trigger single-replica skip
	m = mustApply(m, ReplicaDataMsg{
		PCSName:        "alpha-pcs",
		Namespace:      "default",
		ReplicaIndexes: []string{"0"},
		ScalingGroupsByReplica: map[string][]Resource{
			"0": {{Name: "sg1", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "1/1", Scheduled: "1/1"}},
		},
		PodCliquesByReplica: map[string][]Resource{},
	})

	if m.viewState.viewType != PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", viewTypeName(m.viewState.viewType))
	}
}

func TestHandleReplicaChildrenMsg(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.viewType = PodCliqueSetReplicaView
	m.viewState.selectedPodCliqueSet = "alpha-pcs"
	m.viewState.selectedReplicaIndex = "0"

	scalingGroups, podCliques := sampleReplicaChildren()
	m = mustApply(m, ReplicaChildrenMsg{
		PCSName:       "alpha-pcs",
		Namespace:     "default",
		ReplicaIndex:  "0",
		ScalingGroups: scalingGroups,
		PodCliques:    podCliques,
	})

	key := "PodCliqueSetReplica/alpha-pcs/0"
	resources, exists := m.allResources[key]
	if !exists {
		t.Fatal("expected resources stored for replica children key")
	}
	// Should have both scaling groups and pod cliques
	if len(resources) != len(scalingGroups)+len(podCliques) {
		t.Fatalf("expected %d resources, got %d", len(scalingGroups)+len(podCliques), len(resources))
	}
}

func TestHandlePCSGChildrenMsg(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.viewType = PodCliqueScalingGroupView
	m.viewState.selectedPodCliqueSet = "alpha-pcs"
	m.viewState.selectedReplicaIndex = "0"
	m.viewState.selectedScalingGroup = "alpha-pcs-0-sg-prefill"

	podCliques := []Resource{
		{Name: "alpha-pcs-0-sg-prefill-pc1", Type: "PodClique", Namespace: "default", Ready: "1/1", Scheduled: "1/1"},
	}
	m = mustApply(m, PCSGChildrenMsg{
		PCSGName:   "alpha-pcs-0-sg-prefill",
		Namespace:  "default",
		PodCliques: podCliques,
	})

	key := "PodCliqueScalingGroup/alpha-pcs-0-sg-prefill"
	resources, exists := m.allResources[key]
	if !exists {
		t.Fatal("expected resources stored for PCSG children key")
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
}

func TestHandlePodCliqueChildrenMsg(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.viewType = PodCliqueView
	m.viewState.selectedPodCliqueSet = "alpha-pcs"
	m.viewState.selectedReplicaIndex = "0"
	m.viewState.selectedPodClique = "alpha-pcs-0-standalone-pc"

	m = mustApply(m, PodCliqueChildrenMsg{
		PodCliqueName: "alpha-pcs-0-standalone-pc",
		Namespace:     "default",
		Pods:          samplePods(),
	})

	key := "PodClique/alpha-pcs-0-standalone-pc"
	resources, exists := m.allResources[key]
	if !exists {
		t.Fatal("expected resources stored for PodClique children key")
	}
	if len(resources) != 3 {
		t.Fatalf("expected 3 pods, got %d", len(resources))
	}
}

func TestHandleEventsMsg(t *testing.T) {
	m := newTestModel(nil)

	events := sampleEvents()
	m = mustApply(m, EventsMsg{Events: events})

	if len(m.allEvents) != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), len(m.allEvents))
	}
}

func TestHandleTopologyInfoMsg(t *testing.T) {
	m := newTestModel(nil)

	topoInfo := &TopologyInfo{
		PCSPackDomain:    "zone",
		PCSGPackDomains:  map[string]string{"prefill": "block"},
		CliquePackDomains: map[string]string{"worker": "rack"},
		DomainToKey:       map[string]string{},
	}

	m, cmd := applyMsg(m, TopologyInfoMsg{
		PCSName:      "my-pcs",
		Namespace:    "default",
		TopologyInfo: topoInfo,
	})

	if m.cachedTopologyInfo == nil {
		t.Fatal("expected cachedTopologyInfo to be set")
	}
	if m.cachedTopologyInfo.PCSPackDomain != "zone" {
		t.Fatalf("expected PCSPackDomain=zone, got %s", m.cachedTopologyInfo.PCSPackDomain)
	}
	// Should have dispatched loadNodeLabelsCmd
	if cmd == nil {
		t.Fatal("expected loadNodeLabelsCmd to be dispatched")
	}
}

func TestHandlePodInfoMsg(t *testing.T) {
	m := newTestModel(nil)

	podInfos := map[string]CachedPodInfo{
		"pod-1": {NodeName: "node-1"},
		"pod-2": {NodeName: "node-2"},
	}

	m = mustApply(m, PodInfoMsg{
		PCSName:   "my-pcs",
		Namespace: "default",
		PodInfos:  podInfos,
	})

	if len(m.cachedPods) != 2 {
		t.Fatalf("expected 2 cached pods, got %d", len(m.cachedPods))
	}
}

func TestHandleNodeLabelsMsg(t *testing.T) {
	m := newTestModel(nil)

	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.io/rack": "rack-1"},
		"node-2": {"topology.io/rack": "rack-2"},
	}

	m = mustApply(m, NodeLabelsMsg{NodeLabels: nodeLabels})

	if len(m.cachedNodeLabels) != 2 {
		t.Fatalf("expected 2 node label entries, got %d", len(m.cachedNodeLabels))
	}
}

func TestGetCurrentViewKey(t *testing.T) {
	m := newTestModel(nil)

	tests := []struct {
		viewState ViewState
		expected  string
	}{
		{ViewState{viewType: ForestView}, "forest"},
		{ViewState{viewType: PodCliqueSetView, selectedPodCliqueSet: "my-pcs"}, "PodCliqueSet/my-pcs"},
		{ViewState{viewType: PodCliqueSetReplicaView, selectedPodCliqueSet: "my-pcs", selectedReplicaIndex: "1"}, "PodCliqueSetReplica/my-pcs/1"},
		{ViewState{viewType: PodCliqueScalingGroupView, selectedScalingGroup: "my-sg"}, "PodCliqueScalingGroup/my-sg"},
		{ViewState{viewType: PodCliqueView, selectedPodClique: "my-pc"}, "PodClique/my-pc"},
		{ViewState{viewType: PodView}, ""},
	}

	for _, tt := range tests {
		m.viewState = tt.viewState
		got := m.getCurrentViewKey()
		if got != tt.expected {
			t.Errorf("getCurrentViewKey() for viewType=%s: expected %q, got %q", viewTypeName(tt.viewState.viewType), tt.expected, got)
		}
	}
}

func TestEnterOnEventsPane_DoesNothing(t *testing.T) {
	m := newTestModel(samplePCSResources())
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

	// Switch to events pane
	m = sendKey(m, tea.KeyTab)
	if m.activePane != EventsPane {
		t.Fatal("expected events pane")
	}

	// Enter on events pane should not navigate
	before := m.viewState.viewType
	m = sendKey(m, tea.KeyEnter)
	if m.viewState.viewType != before {
		t.Fatalf("expected no navigation on Enter in events pane, got %s", viewTypeName(m.viewState.viewType))
	}
}

func TestBreadcrumbRendering(t *testing.T) {
	m := newTestModel(nil)

	// ForestView breadcrumb
	m.viewState = ViewState{viewType: ForestView}
	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, "Forest") {
		t.Errorf("expected ForestView breadcrumb to contain 'Forest', got %q", bc)
	}

	// PodCliqueSetView breadcrumb
	m.viewState = ViewState{viewType: PodCliqueSetView, selectedPodCliqueSet: "my-pcs"}
	bc = m.renderBreadcrumb()
	if !strings.Contains(bc, "Forest") || !strings.Contains(bc, "my-pcs") {
		t.Errorf("expected PodCliqueSetView breadcrumb to contain 'Forest' and 'my-pcs', got %q", bc)
	}

	// PodCliqueSetReplicaView breadcrumb
	m.viewState = ViewState{
		viewType:             PodCliqueSetReplicaView,
		selectedPodCliqueSet: "my-pcs",
		selectedReplicaIndex: "2",
	}
	bc = m.renderBreadcrumb()
	if !strings.Contains(bc, "replica-2") {
		t.Errorf("expected breadcrumb to contain 'replica-2', got %q", bc)
	}

	// PodView breadcrumb
	m.viewState = ViewState{
		viewType:             PodView,
		selectedPodCliqueSet: "my-pcs",
		selectedReplicaIndex: "0",
		selectedPodClique:    "my-pc",
		selectedPod:          "my-pod-0",
	}
	bc = m.renderBreadcrumb()
	if !strings.Contains(bc, "my-pod-0") {
		t.Errorf("expected PodView breadcrumb to contain 'my-pod-0', got %q", bc)
	}
}

func TestMenuBarContainsShortcuts(t *testing.T) {
	m := newTestModel(samplePCSResources())
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

	menuBar := m.renderMenuBar()
	expectedShortcuts := []string{"Filter", "Switch", "Quit"}
	for _, s := range expectedShortcuts {
		if !strings.Contains(menuBar, s) {
			t.Errorf("expected menu bar to contain %q, got %q", s, menuBar)
		}
	}
}

func TestMenuBarShowsEscBackWhenNotAtForest(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.viewType = PodCliqueSetView

	shortcuts := m.buildShortcutsString()
	if !strings.Contains(shortcuts, "<esc>Back") {
		t.Errorf("expected '<esc>Back' in shortcuts when not at ForestView, got %q", shortcuts)
	}
}

func TestMenuBarHidesEscAtForestView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.viewType = ForestView

	shortcuts := m.buildShortcutsString()
	if strings.Contains(shortcuts, "<esc>Back") {
		t.Errorf("expected no '<esc>Back' at ForestView, got %q", shortcuts)
	}
}

func TestMenuBarShowsScrollInPodView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.viewType = PodView

	shortcuts := m.buildShortcutsString()
	if !strings.Contains(shortcuts, "Scroll") {
		t.Errorf("expected 'Scroll' in shortcuts at PodView, got %q", shortcuts)
	}
	if strings.Contains(shortcuts, "Nav") && !strings.Contains(shortcuts, "Scroll") {
		t.Errorf("expected no 'Nav' at PodView, got %q", shortcuts)
	}
}
