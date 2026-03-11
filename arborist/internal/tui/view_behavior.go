package tui

import (
	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	tea "github.com/charmbracelet/bubbletea"
)

// ViewBehavior encapsulates per-view-type behavior for the TUI.
//
// This is how we get separation of concerns within Bubble Tea's single-Model
// constraint. Instead of `switch viewType` scattered across files, each view
// declares its own behavior.
//
// Optional interfaces (BackNavigator, LogsExecutor, ShellExecutor) use runtime
// type assertions instead of required methods. This looks like it should be
// compile-time checked, but is intentional — not every view supports logs or
// shell, and adding no-op methods to satisfy the interface would be misleading.
// The type assertion pattern is idiomatic Go for optional capabilities.
type ViewBehavior interface {
	// ViewKey returns the allResources map key for this view type.
	ViewKey(vs clusterstate.ViewState) string

	// Panes lists the panes available in this view.
	PaneList() []clusterstate.Pane

	// RebuildEvents populates m.allEvents from the snapshot.
	RebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot)

	// RenderPanes renders the view-specific pane sections.
	RenderPanes(m *Model, resourcesH, eventsH int) []string

	// HandleKey processes a key press for this view. Returns (model, cmd, handled).
	// When handled is true, the key was consumed by the view. When false, the
	// caller should fall through to global key handling.
	//
	// This is the equivalent of polymorphic dispatch in an Elm architecture:
	// view-specific keys (Esc, Enter, Up/Down) are handled first, global keys
	// (q, t, /, :, etc.) fall through.
	HandleKey(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd, bool)
}

// BackNavigator is an optional interface for views that support back-navigation.
type BackNavigator interface {
	NavigateBack(m *Model) bool
}

// LogsExecutor is an optional interface for views that support logs.
type LogsExecutor interface {
	LogsExec(m *Model) (tea.Model, tea.Cmd)
}

// ShellExecutor is an optional interface for views that support shell exec.
type ShellExecutor interface {
	ShellExec(m *Model) (tea.Model, tea.Cmd)
}

// viewBehaviors is a package-level registry of per-view-type behaviors.
// It is only written during init() via registerViewBehavior() and is read-only
// afterward. This initialization-time-only write pattern is safe for concurrent
// reads without synchronization because all writes complete before any goroutine
// can call behavior().
var viewBehaviors map[clusterstate.ViewType]ViewBehavior

func init() {
	viewBehaviors = make(map[clusterstate.ViewType]ViewBehavior)
}

// behavior returns the ViewBehavior for the current view type.
func (m *Model) behavior() ViewBehavior {
	if b, ok := viewBehaviors[m.viewState.ViewType]; ok {
		return b
	}
	return viewBehaviors[clusterstate.ForestView]
}

// registerViewBehavior registers a ViewBehavior for a given ViewType.
func registerViewBehavior(vt clusterstate.ViewType, b ViewBehavior) {
	viewBehaviors[vt] = b
}
