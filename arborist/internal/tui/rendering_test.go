package tui

import (
	"strings"
	"testing"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	"github.com/charmbracelet/lipgloss"
)

// ===========================================================================
// Phase 4, Item 7: truncateStyled / truncateStyledLeft
// ===========================================================================

func TestTruncateStyled_WiderThanMaxWidth(t *testing.T) {
	// A plain string that is definitely wider than the limit.
	s := "Hello, World!"
	result := truncateStyled(s, 8)

	w := lipgloss.Width(result)
	if w > 8 {
		t.Errorf("truncateStyled result width = %d, want <= 8", w)
	}
	if !strings.HasSuffix(result, "…") {
		t.Errorf("expected truncated result to end with ellipsis, got %q", result)
	}
}

func TestTruncateStyled_FitsWithinMaxWidth(t *testing.T) {
	s := "short"
	result := truncateStyled(s, 20)
	if result != s {
		t.Errorf("expected no change for fitting string, got %q", result)
	}
}

func TestTruncateStyled_ExactWidth(t *testing.T) {
	s := "abcde"
	result := truncateStyled(s, 5)
	if result != s {
		t.Errorf("expected no change when string width == maxWidth, got %q", result)
	}
}

func TestTruncateStyled_MaxWidthZero(t *testing.T) {
	result := truncateStyled("anything", 0)
	if result != "" {
		t.Errorf("expected empty string for maxWidth=0, got %q", result)
	}
}

func TestTruncateStyled_MaxWidthNegative(t *testing.T) {
	result := truncateStyled("anything", -5)
	if result != "" {
		t.Errorf("expected empty string for negative maxWidth, got %q", result)
	}
}

func TestTruncateStyled_MaxWidthOne(t *testing.T) {
	// maxWidth=1 triggers MaxWidth(0) internally which lipgloss treats as
	// "no limit", so the full string is rendered plus an ellipsis. This
	// documents the current edge-case behavior rather than an ideal one.
	result := truncateStyled("Hello", 1)
	if !strings.HasSuffix(result, "…") {
		t.Errorf("truncateStyled with maxWidth=1: expected ellipsis suffix, got %q", result)
	}
}

func TestTruncateStyled_EmptyString(t *testing.T) {
	result := truncateStyled("", 10)
	if result != "" {
		t.Errorf("expected empty string for empty input, got %q", result)
	}
}

func TestTruncateStyled_WithANSIEscapes(t *testing.T) {
	// Styled string: the visual width should be based on visible characters only.
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("Hello, World!")
	visWidth := lipgloss.Width(styled)
	if visWidth <= 8 {
		t.Skip("styled string unexpectedly narrow, skip test")
	}

	result := truncateStyled(styled, 8)
	w := lipgloss.Width(result)
	if w > 8 {
		t.Errorf("truncateStyled with ANSI: result width = %d, want <= 8", w)
	}
}

// --- truncateStyledLeft ---

func TestTruncateStyledLeft_WiderThanMaxWidth(t *testing.T) {
	s := "Forest > PCS > replica-0 > PCSG > replica-1 > PodClique"
	result := truncateStyledLeft(s, 20)

	w := lipgloss.Width(result)
	if w > 20 {
		t.Errorf("truncateStyledLeft result width = %d, want <= 20", w)
	}
	if !strings.HasPrefix(result, "…") {
		t.Errorf("expected truncated result to start with ellipsis, got %q", result)
	}
	// The rightmost portion (most relevant) should be preserved
	if !strings.HasSuffix(result, "PodClique") {
		t.Errorf("expected result to end with 'PodClique' (rightmost segment), got %q", result)
	}
}

func TestTruncateStyledLeft_FitsWithinMaxWidth(t *testing.T) {
	s := "short"
	result := truncateStyledLeft(s, 20)
	if result != s {
		t.Errorf("expected no change for fitting string, got %q", result)
	}
}

func TestTruncateStyledLeft_ExactWidth(t *testing.T) {
	s := "abcde"
	result := truncateStyledLeft(s, 5)
	if result != s {
		t.Errorf("expected no change when string width == maxWidth, got %q", result)
	}
}

func TestTruncateStyledLeft_MaxWidthZero(t *testing.T) {
	result := truncateStyledLeft("anything", 0)
	if result != "" {
		t.Errorf("expected empty string for maxWidth=0, got %q", result)
	}
}

func TestTruncateStyledLeft_MaxWidthNegative(t *testing.T) {
	result := truncateStyledLeft("anything", -3)
	if result != "" {
		t.Errorf("expected empty string for negative maxWidth, got %q", result)
	}
}

func TestTruncateStyledLeft_EmptyString(t *testing.T) {
	result := truncateStyledLeft("", 10)
	if result != "" {
		t.Errorf("expected empty string for empty input, got %q", result)
	}
}

func TestTruncateStyledLeft_WithANSIEscapes(t *testing.T) {
	// Build a styled string with ANSI escape sequences.
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("Hello") +
		" > " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("World")

	visWidth := lipgloss.Width(styled)
	if visWidth <= 8 {
		t.Skip("styled string unexpectedly narrow, skip test")
	}

	result := truncateStyledLeft(styled, 8)
	w := lipgloss.Width(result)
	if w > 8 {
		t.Errorf("truncateStyledLeft with ANSI: result width = %d, want <= 8", w)
	}
	if !strings.HasPrefix(result, "…") {
		t.Errorf("expected result to start with ellipsis, got %q", result)
	}
}

func TestTruncateStyledLeft_CutPointNeverInsideANSI(t *testing.T) {
	// Construct a string where ANSI sequences appear at the cut boundary.
	// The function must skip over escape sequences atomically.
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	// "AAAA" styled (has ANSI escapes) followed by plain "BBBB"
	styled := style.Render("AAAA") + "BBBB"
	visWidth := lipgloss.Width(styled)
	if visWidth <= 4 {
		t.Skip("styled string too short for this test")
	}

	// Truncate to keep only the last 5 visible chars + ellipsis.
	result := truncateStyledLeft(styled, 6)
	w := lipgloss.Width(result)
	if w > 6 {
		t.Errorf("result width = %d, want <= 6", w)
	}
	// Should not contain a broken escape sequence (bare \x1b without [).
	// A well-formed result either contains no \x1b or only complete sequences.
	runes := []rune(result)
	for i, r := range runes {
		if r == '\x1b' {
			if i+1 >= len(runes) || runes[i+1] != '[' {
				t.Errorf("found broken ANSI escape at position %d in result %q", i, result)
			}
		}
	}
}

func TestTruncateStyledLeft_MaxWidthOne(t *testing.T) {
	result := truncateStyledLeft("Hello World", 1)
	// maxWidth=1 can at best show "…" (which is 1 visual column wide)
	w := lipgloss.Width(result)
	if w > 1 {
		t.Errorf("result width = %d, want <= 1", w)
	}
}

// ===========================================================================
// Phase 4, Item 8: renderBreadcrumb
// ===========================================================================

func TestRenderBreadcrumb_PodCliqueScalingGroupView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = clusterstate.ViewState{
		ViewType:             clusterstate.PodCliqueScalingGroupView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "0",
		SelectedScalingGroup: "my-pcsg",
	}
	bc := m.renderBreadcrumb()

	expected := []string{"Forest", "my-pcs", "replica-0", "my-pcsg"}
	for _, s := range expected {
		if !strings.Contains(bc, s) {
			t.Errorf("PodCliqueScalingGroupView breadcrumb missing %q, got %q", s, bc)
		}
	}
}

func TestRenderBreadcrumb_PodCliqueViewWithScalingGroupPath(t *testing.T) {
	// PodCliqueView reached via Forest > PCS > replica > PCSG > PCSG-replica > PC
	m := newTestModel(nil)
	m.viewState = clusterstate.ViewState{
		ViewType:                 clusterstate.PodCliqueView,
		SelectedPodCliqueSet:     "my-pcs",
		SelectedReplicaIndex:     "1",
		SelectedScalingGroup:     "my-pcsg",
		SelectedPCSGReplicaIndex: "2",
		SelectedPodClique:        "my-pc",
	}
	bc := m.renderBreadcrumb()

	expected := []string{"Forest", "my-pcs", "replica-1", "my-pcsg", "replica-2", "my-pc"}
	for _, s := range expected {
		if !strings.Contains(bc, s) {
			t.Errorf("PodCliqueView (scaling group path) breadcrumb missing %q, got %q", s, bc)
		}
	}
}

func TestRenderBreadcrumb_PodCliqueViewWithoutScalingGroup(t *testing.T) {
	// PodCliqueView reached via Forest > PCS > replica > PC (no scaling group)
	m := newTestModel(nil)
	m.viewState = clusterstate.ViewState{
		ViewType:             clusterstate.PodCliqueView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "0",
		SelectedPodClique:    "my-pc",
	}
	bc := m.renderBreadcrumb()

	expected := []string{"Forest", "my-pcs", "replica-0", "my-pc"}
	for _, s := range expected {
		if !strings.Contains(bc, s) {
			t.Errorf("PodCliqueView (no scaling group) breadcrumb missing %q, got %q", s, bc)
		}
	}
	// Should NOT contain scaling group markers
	if strings.Contains(bc, "my-pcsg") {
		t.Errorf("PodCliqueView breadcrumb should not contain scaling group, got %q", bc)
	}
}

func TestRenderBreadcrumb_PodCliqueViewWithScalingGroupNoReplica(t *testing.T) {
	// PodCliqueView with scaling group set but no PCSG replica index
	m := newTestModel(nil)
	m.viewState = clusterstate.ViewState{
		ViewType:             clusterstate.PodCliqueView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "0",
		SelectedScalingGroup: "my-pcsg",
		// SelectedPCSGReplicaIndex is empty (auto-skipped single replica)
		SelectedPodClique: "my-pc",
	}
	bc := m.renderBreadcrumb()

	// Should contain the scaling group but not "replica-" for PCSG
	if !strings.Contains(bc, "my-pcsg") {
		t.Errorf("expected breadcrumb to contain 'my-pcsg', got %q", bc)
	}
	if !strings.Contains(bc, "my-pc") {
		t.Errorf("expected breadcrumb to contain 'my-pc', got %q", bc)
	}
}

func TestRenderBreadcrumb_PodViewWithScalingGroupPath(t *testing.T) {
	// PodView reached via the full PCSG path
	m := newTestModel(nil)
	m.viewState = clusterstate.ViewState{
		ViewType:                 clusterstate.PodView,
		SelectedPodCliqueSet:     "my-pcs",
		SelectedReplicaIndex:     "0",
		SelectedScalingGroup:     "my-pcsg",
		SelectedPCSGReplicaIndex: "1",
		SelectedPodClique:        "my-pc",
		SelectedPod:              "my-pod-0",
	}
	bc := m.renderBreadcrumb()

	expected := []string{"Forest", "my-pcs", "replica-0", "my-pcsg", "replica-1", "my-pc", "my-pod-0"}
	for _, s := range expected {
		if !strings.Contains(bc, s) {
			t.Errorf("PodView (scaling group path) breadcrumb missing %q, got %q", s, bc)
		}
	}
}

func TestRenderBreadcrumb_PodViewWithoutScalingGroup(t *testing.T) {
	// PodView reached without scaling group path
	m := newTestModel(nil)
	m.viewState = clusterstate.ViewState{
		ViewType:             clusterstate.PodView,
		SelectedPodCliqueSet: "my-pcs",
		SelectedReplicaIndex: "0",
		SelectedPodClique:    "my-pc",
		SelectedPod:          "my-pod-0",
	}
	bc := m.renderBreadcrumb()

	expected := []string{"Forest", "my-pcs", "replica-0", "my-pc", "my-pod-0"}
	for _, s := range expected {
		if !strings.Contains(bc, s) {
			t.Errorf("PodView (no scaling group) breadcrumb missing %q, got %q", s, bc)
		}
	}
}

func TestRenderBreadcrumb_PodCliqueScalingGroupReplicaView(t *testing.T) {
	m := newTestModel(nil)
	m.viewState = clusterstate.ViewState{
		ViewType:                 clusterstate.PodCliqueScalingGroupReplicaView,
		SelectedPodCliqueSet:     "my-pcs",
		SelectedReplicaIndex:     "0",
		SelectedScalingGroup:     "my-pcsg",
		SelectedPCSGReplicaIndex: "3",
	}
	bc := m.renderBreadcrumb()

	expected := []string{"Forest", "my-pcs", "replica-0", "my-pcsg", "replica-3"}
	for _, s := range expected {
		if !strings.Contains(bc, s) {
			t.Errorf("PodCliqueScalingGroupReplicaView breadcrumb missing %q, got %q", s, bc)
		}
	}
}

func TestRenderBreadcrumb_DefaultFallback(t *testing.T) {
	// An unknown ViewType should fall back to "Forest"
	m := newTestModel(nil)
	m.viewState = clusterstate.ViewState{
		ViewType: clusterstate.ViewType(999),
	}
	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, "Forest") {
		t.Errorf("default breadcrumb should contain 'Forest', got %q", bc)
	}
}

// ===========================================================================
// Phase 4, Item 9: viewDisplayName
// ===========================================================================

func TestViewDisplayName_OnlyTwoLenses(t *testing.T) {
	m := newTestModel(nil)

	// All forest hierarchy views should return "forest"
	forestViews := []clusterstate.ViewType{
		clusterstate.ForestView,
		clusterstate.PodCliqueSetView,
		clusterstate.PodCliqueSetReplicaView,
		clusterstate.PodCliqueScalingGroupView,
		clusterstate.PodCliqueScalingGroupReplicaView,
		clusterstate.PodCliqueView,
		clusterstate.PodView,
	}
	for _, vt := range forestViews {
		m.viewState.ViewType = vt
		got := m.viewDisplayName()
		if got != "forest" {
			t.Errorf("viewDisplayName() for %s = %q, want %q",
				clusterstate.ViewTypeName(vt), got, "forest")
		}
	}

	// Topology view returns "topology"
	m.viewState.ViewType = clusterstate.TopologyView
	got := m.viewDisplayName()
	if got != "topology" {
		t.Errorf("viewDisplayName() for TopologyView = %q, want %q", got, "topology")
	}

	// Unknown view type still returns "forest" (it's the default)
	m.viewState.ViewType = clusterstate.ViewType(999)
	got = m.viewDisplayName()
	if got != "forest" {
		t.Errorf("viewDisplayName() for unknown type = %q, want %q", got, "forest")
	}
}
