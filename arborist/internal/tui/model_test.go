package tui

import (
	"strings"
	"testing"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestModel creates a Model backed by a MockProvider pre-loaded with the
// given PodCliqueSet resources. It also sends a WindowSizeMsg so the model is
// considered "ready" and will render a full view.
func newTestModel(pcsResources []data.Resource) Model {
	mp := data.NewMockProvider()
	mp.PodCliqueSets = pcsResources
	m := NewModel(mp)
	// Simulate initial window size so the model is ready
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// newTestModelWithProvider creates a Model backed by a fully configured
// MockProvider. Sends a WindowSizeMsg to make the model ready.
func newTestModelWithProvider(mp *data.MockProvider) Model {
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

// buildFullMockProvider creates a MockProvider with a complete hierarchy for
// "alpha-pcs": Forest -> PCS (1 replica) -> Replica children -> PodClique -> Pods.
func buildFullMockProvider() *data.MockProvider {
	mp := data.NewMockProvider()
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
	m := newTestModel([]data.Resource{})

	// Deliver empty forest data
	m = mustApply(m, ForestDataMsg{Resources: []data.Resource{}})

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
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Press Enter to drill into first PCS (alpha-pcs).
	// This dispatches batch commands (loadTopologyInfo, loadPodInfo, loadReplicas).
	var cmd tea.Cmd
	m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Verify the PCS was selected
	if m.viewState.SelectedPodCliqueSet != "alpha-pcs" {
		t.Fatalf("expected SelectedPodCliqueSet=alpha-pcs, got %s", m.viewState.SelectedPodCliqueSet)
	}

	// Execute all batch commands (loadTopologyInfo, loadPodInfo, loadReplicas)
	// This triggers ReplicaDataMsg handling which does single-replica skip.
	m = executeCmdAndApply(m, cmd)

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

// TestNavigationDrillDown_ViaMessages tests the navigation state machine by
// directly sending messages (bypassing table selection and batch commands).
// This provides a clean unit test of the Update handlers.
func TestNavigationDrillDown_ViaMessages(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Start at ForestView
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Simulate navigating into alpha-pcs: set SelectedPodCliqueSet, then send ReplicaDataMsg
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"

	// ReplicaDataMsg with 1 replica triggers skip to PodCliqueSetReplicaView
	m = mustApply(m, ReplicaDataMsg{
		PCSName:        "alpha-pcs",
		Namespace:      "default",
		ReplicaIndexes: []string{"0"},
		ScalingGroupsByReplica: map[string][]data.Resource{
			"0": {{Name: "sg1", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "1/1", Scheduled: "1/1"}},
		},
		PodCliquesByReplica: map[string][]data.Resource{},
	})
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
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
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after back, got %s", data.ViewTypeName(m.viewState.ViewType))
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

// TestSingleReplicaSkip_ViaMessages tests skip behavior via direct messages.
func TestSingleReplicaSkip_ViaMessages(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.SelectedPodCliqueSet = "test-pcs"

	// Send ReplicaDataMsg with exactly 1 replica
	m = mustApply(m, ReplicaDataMsg{
		PCSName:                "test-pcs",
		Namespace:              "default",
		ReplicaIndexes:         []string{"0"},
		ScalingGroupsByReplica: map[string][]data.Resource{"0": {}},
		PodCliquesByReplica:    map[string][]data.Resource{"0": {}},
	})

	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.viewState.SelectedReplicaIndex != "0" {
		t.Fatalf("expected SelectedReplicaIndex=0, got %s", m.viewState.SelectedReplicaIndex)
	}

	// Pressing Esc should go back to ForestView (single-replica entry in allResources)
	pcsKey := "PodCliqueSet/test-pcs"
	if len(m.allResources[pcsKey]) != 1 {
		t.Fatalf("expected 1 replica resource tracked, got %d", len(m.allResources[pcsKey]))
	}

	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestMultiReplicaShowsReplicaList(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.SelectedPodCliqueSet = "beta-pcs"

	// Send ReplicaDataMsg with 2 replicas
	m = mustApply(m, ReplicaDataMsg{
		PCSName:        "beta-pcs",
		Namespace:      "staging",
		ReplicaIndexes: []string{"0", "1"},
		ScalingGroupsByReplica: map[string][]data.Resource{
			"0": {{Name: "sg-0", Type: "PodCliqueScalingGroup", Namespace: "staging", Ready: "1/1", Scheduled: "1/1"}},
			"1": {{Name: "sg-1", Type: "PodCliqueScalingGroup", Namespace: "staging", Ready: "1/1", Scheduled: "1/1"}},
		},
		PodCliquesByReplica: map[string][]data.Resource{},
	})

	// beta-pcs has 2 replicas, so should show PodCliqueSetView with replica list
	if m.viewState.ViewType != data.PodCliqueSetView {
		t.Fatalf("expected PodCliqueSetView for multi-replica PCS, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Should have 2 replica resources stored
	pcsKey := "PodCliqueSet/beta-pcs"
	if len(m.allResources[pcsKey]) != 2 {
		t.Fatalf("expected 2 replica resources, got %d", len(m.allResources[pcsKey]))
	}

	// Back should go to ForestView
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView after Esc from PodCliqueSetView, got %s", data.ViewTypeName(m.viewState.ViewType))
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
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

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
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	m.viewState.ViewType = data.ForestView
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
	m.viewState.ViewType = data.PodCliqueView
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"
	m.viewState.SelectedReplicaIndex = "0"
	m.viewState.SelectedPodClique = "alpha-pcs-0-standalone-pc"

	// Manually set up the view to navigate into PodView
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
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Load forest and drill all the way to pods via messages
	m = executeCmdAndApply(m, m.Init())

	// Navigate into alpha-pcs
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdAndApply(m, cmd)

	// Should be at PodCliqueSetReplicaView (single replica skip)
	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Navigate into the first child resource (PodCliqueScalingGroup or PodClique)
	m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		m = executeCmdAndApply(m, cmd)
	}

	// Continue drilling until we reach PodView or can't go further
	for m.viewState.ViewType != data.PodView && m.viewState.ViewType != data.ForestView {
		prevViewType := m.viewState.ViewType
		m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil {
			m = executeCmdAndApply(m, cmd)
		}
		// If view didn't change, we're stuck - break to avoid infinite loop
		if m.viewState.ViewType == prevViewType {
			break
		}
	}

	if m.viewState.ViewType == data.PodView {
		// Should have YAML data
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

func TestErrorHandling_ForestDataError(t *testing.T) {
	mp := data.NewMockProvider()
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

func TestErrorHandling_ReplicaDataError(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.SelectedPodCliqueSet = "my-pcs"

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
	mp := data.NewMockProvider()
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
	before := m.viewState.ViewType
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != before {
		t.Fatalf("expected viewType to remain %s, got %s", data.ViewTypeName(before), data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestNavigateBackClearsFilter(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

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
}

func TestNavigateBackFromPodCliqueView_WithScalingGroup(t *testing.T) {
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

func TestHandleReplicaDataMsg(t *testing.T) {
	mp := buildFullMockProvider()
	m := newTestModelWithProvider(mp)

	// Simulate being about to navigate into a PCS
	m.viewState.ViewType = data.ForestView
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"

	// Send ReplicaDataMsg with 1 replica -> should trigger single-replica skip
	m = mustApply(m, ReplicaDataMsg{
		PCSName:        "alpha-pcs",
		Namespace:      "default",
		ReplicaIndexes: []string{"0"},
		ScalingGroupsByReplica: map[string][]data.Resource{
			"0": {{Name: "sg1", Type: "PodCliqueScalingGroup", Namespace: "default", Ready: "1/1", Scheduled: "1/1"}},
		},
		PodCliquesByReplica: map[string][]data.Resource{},
	})

	if m.viewState.ViewType != data.PodCliqueSetReplicaView {
		t.Fatalf("expected PodCliqueSetReplicaView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestHandleReplicaChildrenMsg(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.ViewType = data.PodCliqueSetReplicaView
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"
	m.viewState.SelectedReplicaIndex = "0"

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
	m.viewState.ViewType = data.PodCliqueScalingGroupView
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"
	m.viewState.SelectedReplicaIndex = "0"
	m.viewState.SelectedScalingGroup = "alpha-pcs-0-sg-prefill"

	podCliques := []data.Resource{
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
	m.viewState.ViewType = data.PodCliqueView
	m.viewState.SelectedPodCliqueSet = "alpha-pcs"
	m.viewState.SelectedReplicaIndex = "0"
	m.viewState.SelectedPodClique = "alpha-pcs-0-standalone-pc"

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

	topoInfo := &data.TopologyInfo{
		PCSPackDomain:     "zone",
		PCSGPackDomains:   map[string]string{"prefill": "block"},
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

	podInfos := map[string]data.CachedPodInfo{
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
		viewState data.ViewState
		expected  string
	}{
		{data.ViewState{ViewType: data.ForestView}, "forest"},
		{data.ViewState{ViewType: data.PodCliqueSetView, SelectedPodCliqueSet: "my-pcs"}, "PodCliqueSet/my-pcs"},
		{data.ViewState{ViewType: data.PodCliqueSetReplicaView, SelectedPodCliqueSet: "my-pcs", SelectedReplicaIndex: "1"}, "PodCliqueSetReplica/my-pcs/1"},
		{data.ViewState{ViewType: data.PodCliqueScalingGroupView, SelectedScalingGroup: "my-sg"}, "PodCliqueScalingGroup/my-sg"},
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
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

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
	m = mustApply(m, ForestDataMsg{Resources: samplePCSResources()})

	// Shortcuts should appear in the header (moved from old bottom menu bar)
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

	// The header should show "Back" when not at ForestView
	header := m.renderHeaderFrame()
	if !strings.Contains(header, "Back") {
		t.Errorf("expected header to contain 'Back' when not at ForestView, got:\n%s", header)
	}
}

func TestHeaderHidesBackAtForestView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.ViewType = data.ForestView

	// The header should NOT show "Back" at ForestView
	header := m.renderHeaderFrame()
	if strings.Contains(header, "Back") {
		t.Errorf("expected header NOT to contain 'Back' at ForestView, got:\n%s", header)
	}
}

func TestHeaderShowsScrollInPodView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState.ViewType = data.PodView

	// The header should show "Scroll" (not "Nav" or "Drill") in PodView
	header := m.renderHeaderFrame()
	if !strings.Contains(header, "Scroll") {
		t.Errorf("expected header to contain 'Scroll' at PodView, got:\n%s", header)
	}
	if strings.Contains(header, "Drill") {
		t.Errorf("expected header NOT to contain 'Drill' at PodView, got:\n%s", header)
	}
}

func TestHeaderShowsClusterInfo(t *testing.T) {
	mp := data.NewMockProvider()
	m := NewModel(mp,
		WithClusterInfo("my-test-context", "my-test-cluster"),
		WithUserName("admin@my-test-cluster"),
		WithK8sVersion("v1.33.5+k3s1"),
		WithArboristVersion("v0.1.0"),
	)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	header := m.renderHeaderFrame()

	// Should display context name
	if !strings.Contains(header, "my-test-context") {
		t.Errorf("expected header to contain context name 'my-test-context', got:\n%s", header)
	}
	// Should display cluster name
	if !strings.Contains(header, "my-test-cluster") {
		t.Errorf("expected header to contain cluster name 'my-test-cluster', got:\n%s", header)
	}
	// Should display user name
	if !strings.Contains(header, "admin@my-test-cluster") {
		t.Errorf("expected header to contain user name 'admin@my-test-cluster', got:\n%s", header)
	}
	// Should display K8s version
	if !strings.Contains(header, "v1.33.5+k3s1") {
		t.Errorf("expected header to contain K8s version 'v1.33.5+k3s1', got:\n%s", header)
	}
	// Should display Arborist version
	if !strings.Contains(header, "v0.1.0") {
		t.Errorf("expected header to contain Arborist version 'v0.1.0', got:\n%s", header)
	}
	// Should display the view name
	if !strings.Contains(header, "Forest") {
		t.Errorf("expected header to contain view name 'Forest', got:\n%s", header)
	}
	// Should display all labels
	for _, label := range []string{"Context:", "Cluster:", "User:", "Arborist Rev:", "K8s Rev:", "View:"} {
		if !strings.Contains(header, label) {
			t.Errorf("expected header to contain '%s' label, got:\n%s", label, header)
		}
	}
}

func TestHeaderShowsUnknownWhenNoClusterInfo(t *testing.T) {
	m := newTestModel(nil)

	header := m.renderHeaderFrame()

	// Without cluster info, should display "(unknown)" for context, cluster, user, versions
	count := strings.Count(header, "(unknown)")
	// Expect at least 4 "(unknown)" entries: context, cluster, user, k8sVersion
	// (arboristVersion also shows "(unknown)" when empty)
	if count < 4 {
		t.Errorf("expected at least 4 '(unknown)' entries when no info set, got %d in:\n%s", count, header)
	}
}

func TestHeaderShowsCurrentViewName(t *testing.T) {
	m := newTestModel(nil)

	// Test view name changes with navigation state
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
// Topology View Tests (Phase 6)
// ---------------------------------------------------------------------------

// sampleTopologyViewData creates a TopologyViewData snapshot for testing.
func sampleTopologyViewData() *data.TopologyViewData {
	return &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
			{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
			{Domain: "rack", Key: "topology.kubernetes.io/rack", ValuesCount: 6},
			{Domain: "N/A", Key: "—", ValuesCount: -1},
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

// newTopologyTestModel creates a Model with a MockTopologyCache pre-loaded
// with topology data. It simulates toggling to Topology view and receiving
// the initial cache sync.
func newTopologyTestModel() Model {
	mp := data.NewMockProvider()
	mockCache := data.NewMockTopologyCache()
	mockCache.SetSnapshot(sampleTopologyViewData())

	m := NewModel(mp, WithTopologyCache(mockCache))
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Toggle to Topology view — this starts the cache
	var cmd tea.Cmd
	m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})

	// The cmd is startTopologyCacheCmd. Execute it to get TopologyCacheSyncedMsg.
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			m, _ = applyMsg(m, msg)
		}
	}

	return m
}

func TestTopologyView_ToggleForestToTopology(t *testing.T) {
	mp := data.NewMockProvider()
	mp.PodCliqueSets = samplePCSResources()
	mockCache := data.NewMockTopologyCache()
	mockCache.SetSnapshot(sampleTopologyViewData())

	m := NewModel(mp, WithTopologyCache(mockCache))
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Initially in Forest view
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Press 't' to toggle to Topology view
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView after pressing t, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
	if m.activePane != data.TopologyDomainsPane {
		t.Fatalf("expected TopologyDomainsPane, got %s", data.PaneName(m.activePane))
	}
	if !m.topologyCacheStarted {
		t.Fatal("expected topologyCacheStarted to be true")
	}

	// Execute startTopologyCacheCmd to get TopologyCacheSyncedMsg
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			m, _ = applyMsg(m, msg)
		}
	}

	// After sync, view data should be populated
	if m.topologyViewData == nil {
		t.Fatal("expected topologyViewData to be set after cache sync")
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
	if len(rows) != 4 {
		t.Fatalf("expected 4 domain rows (region, zone, rack, N/A), got %d", len(rows))
	}

	// Check domain names
	expectedDomains := []string{"region", "zone", "rack", "N/A"}
	for i, expected := range expectedDomains {
		if rows[i][0] != expected {
			t.Errorf("row %d: expected domain %q, got %q", i, expected, rows[i][0])
		}
	}

	// Check that N/A row has "—" for key
	if rows[3][1] != "—" {
		t.Errorf("expected N/A key to be '—', got %q", rows[3][1])
	}
}

func TestTopologyView_DrillIntoDomain(t *testing.T) {
	m := newTopologyTestModel()

	// Cursor is on first row (region). Press Enter to drill in.
	m = sendKey(m, tea.KeyEnter)

	// Should have drill stack with one entry
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

	// Values should be sorted
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

	// Should have advanced to zone domain
	if len(m.topologyDrillStack) != 2 {
		t.Fatalf("expected drill stack depth 2, got %d", len(m.topologyDrillStack))
	}
	if m.topologyDrillStack[0].Domain != "region" {
		t.Fatalf("expected first stack entry domain='region', got %q", m.topologyDrillStack[0].Domain)
	}
	if m.topologyDrillStack[0].Value != "us-east-1" {
		t.Fatalf("expected first stack entry value='us-east-1', got %q", m.topologyDrillStack[0].Value)
	}
	if m.topologyDrillStack[1].Domain != "zone" {
		t.Fatalf("expected second stack entry domain='zone', got %q", m.topologyDrillStack[1].Domain)
	}
	if m.topologyDrillStack[1].Value != "" {
		t.Fatalf("expected second stack entry empty value, got %q", m.topologyDrillStack[1].Value)
	}

	// Should show zone values scoped to us-east-1
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 zone values for us-east-1 (us-east-1a, us-east-1b), got %d", len(rows))
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

	// Drill into region
	m = sendKey(m, tea.KeyEnter)
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected drill stack depth 1, got %d", len(m.topologyDrillStack))
	}

	// Press Esc to go back
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 0 {
		t.Fatalf("expected drill stack to be empty after Esc, got depth %d", len(m.topologyDrillStack))
	}

	// Should be back at domain list
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 4 {
		t.Fatalf("expected 4 domain rows after Esc, got %d", len(rows))
	}
}

func TestTopologyView_EscFromEmptyDrillStackSwitchesToForest(t *testing.T) {
	m := newTopologyTestModel()

	// At top-level domains (empty drill stack), Esc switches back to Forest
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

	// At top-level, cursor on "region" — pods table should show all pods on nodes
	// that have the region label key
	podRows := m.topologyPodsTable.Rows()
	if len(podRows) != 6 {
		t.Fatalf("expected 6 pods when highlighting 'region' domain, got %d", len(podRows))
	}

	// Drill into region (showing values list: us-east-1, us-west-2)
	m = sendKey(m, tea.KeyEnter)

	// Cursor defaults to first value (us-east-1) — pods are filtered to nodes
	// matching the highlighted value. us-east-1 has nodes: node-01, node-02, node-03 => 3 pods.
	podRows = m.topologyPodsTable.Rows()
	if len(podRows) != 3 {
		t.Fatalf("expected 3 pods when highlighting us-east-1 value, got %d", len(podRows))
	}

	// Move cursor down to us-west-2 — pods should change to that region's nodes
	m = sendKey(m, tea.KeyDown)
	podRows = m.topologyPodsTable.Rows()
	// us-west-2 has nodes: node-04, node-05, node-06 => 3 pods
	if len(podRows) != 3 {
		t.Fatalf("expected 3 pods when highlighting us-west-2 value, got %d", len(podRows))
	}
	for _, row := range podRows {
		if len(row) >= 3 {
			name := row[2]
			if name != "pod-d" && name != "pod-e" && name != "pod-f" {
				t.Errorf("unexpected pod %q in us-west-2 filtered list", name)
			}
		}
	}

	// Move cursor back up to us-east-1 and select it — advance to zone values
	m = sendKey(m, tea.KeyUp)
	m = sendKey(m, tea.KeyEnter)

	// Now at zone values level with region=us-east-1 committed. Zone values are:
	// us-east-1a (cursor defaults here), us-east-1b.
	// Highlighting us-east-1a filters to nodes: node-01, node-02 => 2 pods.
	podRows = m.topologyPodsTable.Rows()
	if len(podRows) != 2 {
		t.Fatalf("expected 2 pods when highlighting zone us-east-1a, got %d", len(podRows))
	}

	// Verify pod names are from us-east-1a nodes (node-01, node-02)
	podNames := make(map[string]bool)
	for _, row := range podRows {
		if len(row) >= 3 {
			podNames[row[2]] = true
		}
	}
	for _, expected := range []string{"pod-a", "pod-b"} {
		if !podNames[expected] {
			t.Errorf("expected pod %q in filtered list", expected)
		}
	}

	// Move cursor down to us-east-1b — should show pod-c (node-03)
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

	// Initially in TopologyDomainsPane
	if m.activePane != data.TopologyDomainsPane {
		t.Fatalf("expected TopologyDomainsPane, got %s", data.PaneName(m.activePane))
	}
	if !m.topologyDomainsTable.Focused() {
		t.Fatal("expected topology domains table to be focused")
	}

	// Press Tab to switch to TopologyPodsPane
	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.TopologyPodsPane {
		t.Fatalf("expected TopologyPodsPane after Tab, got %s", data.PaneName(m.activePane))
	}
	if !m.topologyPodsTable.Focused() {
		t.Fatal("expected topology pods table to be focused after Tab")
	}
	if m.topologyDomainsTable.Focused() {
		t.Fatal("expected topology domains table to be blurred after Tab")
	}

	// Press Tab again to switch back to TopologyDomainsPane
	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.TopologyDomainsPane {
		t.Fatalf("expected TopologyDomainsPane after second Tab, got %s", data.PaneName(m.activePane))
	}
	if !m.topologyDomainsTable.Focused() {
		t.Fatal("expected topology domains table to be focused after second Tab")
	}
}

func TestTopologyView_NADomainIsNotDrillable(t *testing.T) {
	m := newTopologyTestModel()

	// Navigate cursor to N/A row (last row, index 3)
	m = sendKey(m, tea.KeyDown) // zone
	m = sendKey(m, tea.KeyDown) // rack
	m = sendKey(m, tea.KeyDown) // N/A

	// Press Enter on N/A — should be a no-op
	m = sendKey(m, tea.KeyEnter)

	if len(m.topologyDrillStack) != 0 {
		t.Fatalf("expected empty drill stack after Enter on N/A, got depth %d", len(m.topologyDrillStack))
	}

	// Should still be at top-level domains
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 4 {
		t.Fatalf("expected 4 domain rows (no drill happened), got %d", len(rows))
	}
}

func TestTopologyView_CacheUpdateRebuildsTables(t *testing.T) {
	mp := data.NewMockProvider()
	mockCache := data.NewMockTopologyCache()
	mockCache.SetSnapshot(sampleTopologyViewData())

	m := NewModel(mp, WithTopologyCache(mockCache))
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Toggle to Topology view and handle sync
	var cmd tea.Cmd
	m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			m, _ = applyMsg(m, msg)
		}
	}

	// Drill into region
	m = sendKey(m, tea.KeyEnter)
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected drill stack depth 1, got %d", len(m.topologyDrillStack))
	}

	// Now simulate a cache update with additional pods
	updatedData := sampleTopologyViewData()
	updatedData.Pods = append(updatedData.Pods, data.TopologyViewPod{
		Namespace: "default", Node: "node-01", Name: "pod-new", Topology: "rack: rack-01", Phase: "Running",
	})

	// Deliver TopologyViewDataMsg directly
	m = mustApply(m, TopologyViewDataMsg{Data: updatedData})

	// Drill stack should be preserved
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected drill stack preserved after cache update, got depth %d", len(m.topologyDrillStack))
	}
	if m.topologyDrillStack[0].Domain != "region" {
		t.Fatalf("expected drill stack domain='region', got %q", m.topologyDrillStack[0].Domain)
	}
}

func TestTopologyView_DeepDrillDown(t *testing.T) {
	m := newTopologyTestModel()

	// Drill: region -> us-east-1 -> zone values -> us-east-1a -> rack values
	m = sendKey(m, tea.KeyEnter) // into region (shows values)
	m = sendKey(m, tea.KeyEnter) // select us-east-1, advance to zone (shows values)
	m = sendKey(m, tea.KeyEnter) // select us-east-1a, advance to rack (shows values)

	// Stack: [{region, _, us-east-1}, {zone, _, us-east-1a}, {rack, _, ""}]
	if len(m.topologyDrillStack) != 3 {
		t.Fatalf("expected drill stack depth 3, got %d", len(m.topologyDrillStack))
	}

	// Should show rack values for region=us-east-1 AND zone=us-east-1a
	// Nodes matching: node-01 (rack-01), node-02 (rack-02)
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rack values (rack-01, rack-02), got %d", len(rows))
	}

	// Drill-back logic:
	// - If last entry has empty Value: pop the entry
	// - If last entry has non-empty Value: clear the Value

	// Esc 1: last={rack, _, ""} -> pop. Stack: [{region, _, us-east-1}, {zone, _, us-east-1a}]
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 2 {
		t.Fatalf("expected depth 2 after Esc 1, got %d", len(m.topologyDrillStack))
	}

	// Esc 2: last={zone, _, us-east-1a} -> clear value. Stack: [{region, _, us-east-1}, {zone, _, ""}]
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 2 {
		t.Fatalf("expected depth 2 after Esc 2 (value cleared), got %d", len(m.topologyDrillStack))
	}

	// Esc 3: last={zone, _, ""} -> pop. Stack: [{region, _, us-east-1}]
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected depth 1 after Esc 3, got %d", len(m.topologyDrillStack))
	}

	// Esc 4: last={region, _, us-east-1} -> clear value. Stack: [{region, _, ""}]
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected depth 1 after Esc 4 (value cleared), got %d", len(m.topologyDrillStack))
	}

	// Esc 5: last={region, _, ""} -> pop. Stack: []
	m = sendKey(m, tea.KeyEsc)
	if len(m.topologyDrillStack) != 0 {
		t.Fatalf("expected empty drill stack after Esc 5, got depth %d", len(m.topologyDrillStack))
	}

	// Esc 6: empty stack -> switches to Forest
	m = sendKey(m, tea.KeyEsc)
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestTopologyView_NarrowestDomainEnterIsNoop(t *testing.T) {
	m := newTopologyTestModel()

	// Drill down to the narrowest domain (rack)
	// region -> us-east-1 -> zone -> us-east-1a -> rack -> rack-01 (should be noop)
	m = sendKey(m, tea.KeyEnter) // into region
	m = sendKey(m, tea.KeyEnter) // us-east-1 -> zone
	m = sendKey(m, tea.KeyEnter) // us-east-1a -> rack

	stackDepthAtRack := len(m.topologyDrillStack) // should be 3

	// Select rack-01 and press Enter — should be no-op (N/A is next, not drillable)
	m = sendKey(m, tea.KeyEnter)

	if len(m.topologyDrillStack) != stackDepthAtRack {
		t.Fatalf("expected drill stack depth unchanged at narrowest domain, was %d, got %d",
			stackDepthAtRack, len(m.topologyDrillStack))
	}
}

func TestTopologyView_TogglePreservesDrillStackOnReturn(t *testing.T) {
	m := newTopologyTestModel()

	// Drill into region
	m = sendKey(m, tea.KeyEnter)
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
	// View should show "Topology" in the header
	if !strings.Contains(view, "Topology") {
		t.Errorf("expected view to contain 'Topology', got:\n%s", view)
	}
	// Should show Topology Domains section header
	if !strings.Contains(view, "Topology Domains") {
		t.Errorf("expected view to contain 'Topology Domains', got:\n%s", view)
	}
	// Should show Pods section header
	if !strings.Contains(view, "Pods") {
		t.Errorf("expected view to contain 'Pods', got:\n%s", view)
	}
}

func TestTopologyView_WindowResizeUpdatesTopologyTables(t *testing.T) {
	m := newTopologyTestModel()

	// Resize
	m = mustApply(m, tea.WindowSizeMsg{Width: 200, Height: 60})

	// View should still render correctly
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

	// Initial cursor should be at row 0 (region)
	selectedRow := m.topologyDomainsTable.SelectedRow()
	if len(selectedRow) < 1 || selectedRow[0] != "region" {
		t.Fatalf("expected initial selection on 'region', got %v", selectedRow)
	}

	// Down arrow
	m = sendKey(m, tea.KeyDown)
	selectedRow = m.topologyDomainsTable.SelectedRow()
	if len(selectedRow) < 1 || selectedRow[0] != "zone" {
		t.Fatalf("expected selection on 'zone' after down arrow, got %v", selectedRow)
	}

	// Down arrow
	m = sendKey(m, tea.KeyDown)
	selectedRow = m.topologyDomainsTable.SelectedRow()
	if len(selectedRow) < 1 || selectedRow[0] != "rack" {
		t.Fatalf("expected selection on 'rack' after down arrow, got %v", selectedRow)
	}
}

func TestTopologyView_EnterOnPodsPane_IsNoop(t *testing.T) {
	m := newTopologyTestModel()

	// Switch to pods pane
	m = sendKey(m, tea.KeyTab)
	if m.activePane != data.TopologyPodsPane {
		t.Fatalf("expected TopologyPodsPane, got %s", data.PaneName(m.activePane))
	}

	// Enter on pods pane should not change view or drill state
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

func TestTopologyView_CacheUpdatePreservesDrillDown(t *testing.T) {
	m := newTopologyTestModel()

	// Drill into region > us-east-1 > zone
	m = sendKey(m, tea.KeyEnter) // into region values
	m = sendKey(m, tea.KeyEnter) // select us-east-1, into zone values

	originalStackDepth := len(m.topologyDrillStack)

	// Simulate cache update with same domain structure
	updatedData := sampleTopologyViewData()
	// Add an extra pod
	updatedData.Pods = append(updatedData.Pods, data.TopologyViewPod{
		Namespace: "staging", Node: "node-01", Name: "extra-pod", Topology: "N/A", Phase: "Running",
	})

	m = mustApply(m, TopologyViewDataMsg{Data: updatedData})

	// Drill stack should be preserved
	if len(m.topologyDrillStack) != originalStackDepth {
		t.Fatalf("expected drill stack depth %d after update, got %d", originalStackDepth, len(m.topologyDrillStack))
	}

	// The zone values should still show us-east-1 zones
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 zone values after update, got %d", len(rows))
	}
}

func TestTopologyView_CacheUpdateInvalidDomainResetsDrillStack(t *testing.T) {
	m := newTopologyTestModel()

	// Drill into region
	m = sendKey(m, tea.KeyEnter)
	if len(m.topologyDrillStack) != 1 {
		t.Fatalf("expected drill stack depth 1, got %d", len(m.topologyDrillStack))
	}

	// Simulate cache update with different domains (region removed)
	updatedData := &data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 3},
			{Domain: "N/A", Key: "—", ValuesCount: -1},
		},
		NodeLabels:  sampleTopologyViewData().NodeLabels,
		Pods:        sampleTopologyViewData().Pods,
		DomainToKey: map[string]string{"zone": "topology.kubernetes.io/zone"},
	}

	m = mustApply(m, TopologyViewDataMsg{Data: updatedData})

	// Drill stack should be reset (region no longer exists)
	if len(m.topologyDrillStack) != 0 {
		t.Fatalf("expected drill stack reset when domain removed, got depth %d", len(m.topologyDrillStack))
	}
}

func TestTopologyView_TopologyCacheSyncedMsg(t *testing.T) {
	mp := data.NewMockProvider()
	mockCache := data.NewMockTopologyCache()
	mockCache.SetSnapshot(sampleTopologyViewData())

	m := NewModel(mp, WithTopologyCache(mockCache))
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Manually set up topology view state
	m.viewState.ViewType = data.TopologyView
	m.activePane = data.TopologyDomainsPane
	m.topologyCache = mockCache
	m.topologyCacheStarted = true

	// Send TopologyCacheSyncedMsg
	m, cmd := applyMsg(m, TopologyCacheSyncedMsg{})

	if m.topologyViewData == nil {
		t.Fatal("expected topologyViewData to be set after sync")
	}

	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 4 {
		t.Fatalf("expected 4 domain rows after sync, got %d", len(rows))
	}

	// Should have returned a cmd to wait for updates
	if cmd == nil {
		t.Fatal("expected waitForTopologyCacheUpdateCmd after sync")
	}
}

func TestTopologyView_NilCacheToggleDoesNotPanic(t *testing.T) {
	mp := data.NewMockProvider()
	// No topology cache set
	m := NewModel(mp)
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Press 't' — should toggle view but not start cache (no cache available)
	m = sendRune(m, 't')

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView even without cache, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// Should not panic and should not have started cache
	if m.topologyCacheStarted {
		t.Fatal("expected topologyCacheStarted to be false without a cache")
	}

	// Toggle back should work
	m = sendRune(m, 't')
	if m.viewState.ViewType != data.ForestView {
		t.Fatalf("expected ForestView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}
}

func TestTopologyView_EmptySnapshot(t *testing.T) {
	mp := data.NewMockProvider()
	mockCache := data.NewMockTopologyCache()
	// Set a minimal empty snapshot
	mockCache.SetSnapshot(&data.TopologyViewData{
		Domains: []data.TopologyDomainRow{
			{Domain: "N/A", Key: "—", ValuesCount: -1},
		},
		NodeLabels:  map[string]map[string]string{},
		Pods:        []data.TopologyViewPod{},
		DomainToKey: map[string]string{},
	})

	m := NewModel(mp, WithTopologyCache(mockCache))
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Toggle to topology
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			m, _ = applyMsg(m, msg)
		}
	}

	// Domain table should have just N/A
	rows := m.topologyDomainsTable.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 domain row (N/A), got %d", len(rows))
	}
	if rows[0][0] != "N/A" {
		t.Errorf("expected domain 'N/A', got %q", rows[0][0])
	}

	// Pods table should be empty
	podRows := m.topologyPodsTable.Rows()
	if len(podRows) != 0 {
		t.Fatalf("expected 0 pod rows, got %d", len(podRows))
	}
}

func TestTopologyView_BreadcrumbString(t *testing.T) {
	m := newTopologyTestModel()

	// Empty drill stack
	bc := m.topologyBreadcrumbString()
	if bc != "" {
		t.Errorf("expected empty breadcrumb at top level, got %q", bc)
	}

	// Drill into region
	m = sendKey(m, tea.KeyEnter)
	bc = m.topologyBreadcrumbString()
	// No value selected yet, so breadcrumb should be empty
	if bc != "" {
		t.Errorf("expected empty breadcrumb when no value selected, got %q", bc)
	}

	// Select us-east-1
	m = sendKey(m, tea.KeyEnter)
	bc = m.topologyBreadcrumbString()
	if !strings.Contains(bc, "region=us-east-1") {
		t.Errorf("expected breadcrumb to contain 'region=us-east-1', got %q", bc)
	}

	// Select us-east-1a (advance to rack)
	m = sendKey(m, tea.KeyEnter)
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

	// At top-level, cursor on "region" — all pods shown
	podRows := m.topologyPodsTable.Rows()
	initialPodCount := len(podRows)
	if initialPodCount != 6 {
		t.Fatalf("expected 6 pods when on 'region', got %d", initialPodCount)
	}

	// Move cursor to "zone" — should still show all pods (all nodes have zone)
	m = sendKey(m, tea.KeyDown)
	podRows = m.topologyPodsTable.Rows()
	if len(podRows) != 6 {
		t.Fatalf("expected 6 pods when on 'zone', got %d", len(podRows))
	}
}

func TestTopologyView_SecondToggleUsesWarmCache(t *testing.T) {
	mp := data.NewMockProvider()
	mockCache := data.NewMockTopologyCache()
	mockCache.SetSnapshot(sampleTopologyViewData())

	m := NewModel(mp, WithTopologyCache(mockCache))
	m = mustApply(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// First toggle: starts cache
	m, cmd := applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			m, _ = applyMsg(m, msg)
		}
	}
	if !m.topologyCacheStarted {
		t.Fatal("expected cache to be started after first toggle")
	}

	// Toggle back to Forest
	m = sendRune(m, 't')

	// Toggle to Topology again — should NOT return a start cmd (cache already running)
	m, cmd = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})

	if m.viewState.ViewType != data.TopologyView {
		t.Fatalf("expected TopologyView, got %s", data.ViewTypeName(m.viewState.ViewType))
	}

	// cmd should be nil (no cache start needed)
	if cmd != nil {
		t.Fatal("expected nil cmd on second toggle (cache already warm)")
	}

	// Data should already be populated from the warm cache
	if m.topologyViewData == nil {
		t.Fatal("expected topologyViewData to be set from warm cache")
	}
}

func TestTopologyView_TopologyViewDataMsgWithError(t *testing.T) {
	m := newTopologyTestModel()

	m = mustApply(m, TopologyViewDataMsg{Err: errForTest("cache error")})

	if m.lastError == nil {
		t.Fatal("expected lastError to be set")
	}
	if !strings.Contains(m.lastError.Error(), "cache error") {
		t.Errorf("expected error message to contain 'cache error', got %q", m.lastError.Error())
	}
}
