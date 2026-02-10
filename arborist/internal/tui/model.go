package tui

import (
	"context"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ColumnSpec defines a table column's title and relative weight for width calculation.
// This is the single source of truth for a table's column layout — inspired by K9s's
// HeaderColumn approach but using weighted proportions instead of content-aware sizing
// (which is a better fit for bubbles/table's fixed-width rendering).
type ColumnSpec struct {
	Title  string
	Weight int
}

// Column layouts for each table type. Single source of truth.
var (
	resourceColumnSpecs = []ColumnSpec{
		{Title: "NAMESPACE", Weight: 2},
		{Title: "TYPE", Weight: 3},
		{Title: "NAME", Weight: 5},
		{Title: "TOPOLOGY", Weight: 3},
		{Title: "READY", Weight: 2},
		{Title: "SCHEDULED", Weight: 2}, // Title overridden to "PHASE" for pod views
	}

	eventColumnSpecs = []ColumnSpec{
		{Title: "TYPE", Weight: 2},
		{Title: "REASON", Weight: 3},
		{Title: "AGE", Weight: 1},
		{Title: "FROM", Weight: 3},
		{Title: "MESSAGE", Weight: 8},
	}
)

// computeWeightedColumns takes a column spec and available width, and returns
// []table.Column with widths distributed proportionally by weight.
// The last column absorbs any rounding remainder so the table fills exactly.
func computeWeightedColumns(specs []ColumnSpec, availableWidth int) []table.Column {
	if len(specs) == 0 {
		return nil
	}

	totalWeight := 0
	for _, s := range specs {
		totalWeight += s.Weight
	}
	if totalWeight == 0 {
		totalWeight = len(specs) // fallback: equal weights
	}

	unit := availableWidth / totalWeight
	columns := make([]table.Column, len(specs))
	usedWidth := 0
	for i, s := range specs {
		w := unit * s.Weight
		columns[i] = table.Column{Title: s.Title, Width: w}
		usedWidth += w
	}
	// Last column absorbs rounding remainder
	columns[len(columns)-1].Width += availableWidth - usedWidth

	return columns
}

// tableContentWidth returns the usable content width for a table given terminal
// width, frame border overhead, and per-cell padding (2 chars per column for
// the Padding(0,1) style).
func tableContentWidth(termWidth, numColumns int) int {
	const frameBorders = 2 // 1 char on each side for the rounded border
	cellPadding := numColumns * 2
	w := termWidth - frameBorders - cellPadding
	if w < 80 {
		w = 80
	}
	return w
}

// Model is the main Bubble Tea model for the arborist TUI.
type Model struct {
	// State
	viewState    data.ViewState
	activePane   data.Pane
	filterActive bool
	filterText   string

	// Data
	allResources       map[string][]data.Resource
	allEvents          []data.Event
	podYAMLData        map[string]string
	cachedTopologyInfo *data.TopologyInfo
	cachedPods         map[string]data.CachedPodInfo
	cachedNodeLabels   map[string]map[string]string

	// Sub-models (bubbles components)
	resourcesTable table.Model
	eventsTable    table.Model
	filterInput    textinput.Model
	podViewport    viewport.Model

	// Dependencies (injected)
	provider data.DataProvider
	ctx      context.Context

	// Layout
	width  int
	height int
	ready  bool // true after first WindowSizeMsg

	// Configuration
	debug bool

	// Cluster info (resolved from kubeconfig at startup)
	contextName      string
	clusterName      string
	userName         string
	k8sVersion       string
	arboristVersion  string

	// Error state
	lastError error
}

// Option is a function that configures the Model.
type Option func(*Model)

// WithContext sets the context for K8s operations.
func WithContext(ctx context.Context) Option {
	return func(m *Model) {
		m.ctx = ctx
	}
}

// WithDebug enables debug logging.
func WithDebug(enabled bool) Option {
	return func(m *Model) {
		m.debug = enabled
	}
}

// WithClusterInfo sets the kubeconfig context and cluster name for display in the header.
func WithClusterInfo(contextName, clusterName string) Option {
	return func(m *Model) {
		m.contextName = contextName
		m.clusterName = clusterName
	}
}

// WithUserName sets the kubeconfig user name for display in the header.
func WithUserName(userName string) Option {
	return func(m *Model) {
		m.userName = userName
	}
}

// WithK8sVersion sets the Kubernetes server version for display in the header.
func WithK8sVersion(version string) Option {
	return func(m *Model) {
		m.k8sVersion = version
	}
}

// WithArboristVersion sets the arborist version for display in the header.
func WithArboristVersion(version string) Option {
	return func(m *Model) {
		m.arboristVersion = version
	}
}

// NewModel creates a new Model with the given DataProvider and options.
func NewModel(provider data.DataProvider, opts ...Option) Model {
	// Initialize filter input
	ti := textinput.New()
	ti.Placeholder = ""
	ti.CharLimit = 256
	ti.Width = 40
	ti.Prompt = "/ "
	ti.PromptStyle = FilterBarStyle

	m := Model{
		viewState: data.ViewState{
			ViewType: data.ForestView,
		},
		activePane:   data.ResourcesPane,
		allResources: make(map[string][]data.Resource),
		podYAMLData:  make(map[string]string),
		provider:     provider,
		ctx:          context.Background(),
		filterInput:  ti,
	}

	// Apply options
	for _, opt := range opts {
		opt(&m)
	}

	// Initialize tables with empty data (will be populated after data loads)
	m.resourcesTable = m.createResourcesTableModel()
	m.eventsTable = m.createEventsTableModel()

	return m
}

// createResourcesTableModel creates the resources table with appropriate columns.
func (m *Model) createResourcesTableModel() table.Model {
	columns := computeWeightedColumns(resourceColumnSpecs, 80) // placeholder widths until first resize
	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(ArboristTableStyles())
	return t
}

// createEventsTableModel creates the events table with appropriate columns.
func (m *Model) createEventsTableModel() table.Model {
	columns := computeWeightedColumns(eventColumnSpecs, 80) // placeholder widths until first resize
	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(false),
		table.WithHeight(10),
	)
	t.SetStyles(ArboristTableStyles())
	return t
}

// Init initializes the model and returns the initial command.
func (m Model) Init() tea.Cmd {
	debugLog("starting arborist TUI (bubbletea)")
	// Load forest data on startup
	return loadForestDataCmd(m.provider, m.ctx)
}

// rebuildResourcesTable rebuilds the resources table from current data with color-coded cells.
func (m *Model) rebuildResourcesTable() {
	viewKey := m.getCurrentViewKey()
	resources, exists := m.allResources[viewKey]
	if !exists {
		resources = []data.Resource{}
	}

	// Apply filter if active
	if m.filterText != "" {
		filter := strings.ToLower(m.filterText)
		var filtered []data.Resource
		for _, r := range resources {
			if strings.Contains(strings.ToLower(r.Name), filter) ||
				strings.Contains(strings.ToLower(r.Type), filter) ||
				strings.Contains(strings.ToLower(r.Namespace), filter) {
				filtered = append(filtered, r)
			}
		}
		debugLogWithContext("filter %q: %d -> %d resources", m.filterText, len(resources), len(filtered))
		resources = filtered
	}

	// Build rows with plain text cells
	rows := make([]table.Row, 0, len(resources))
	for _, r := range resources {
		styledCells := colorizeResourceRow(r)
		rows = append(rows, table.Row(styledCells))
	}

	m.resourcesTable.SetRows(rows)

	// Ensure cursor is valid after setting rows.
	if len(rows) > 0 {
		cursor := m.resourcesTable.Cursor()
		if cursor < 0 || cursor >= len(rows) {
			m.resourcesTable.SetCursor(0)
		}
	}

	// Update column header based on view type
	lastColHeader := "SCHEDULED"
	if m.viewState.ViewType == data.PodCliqueView && len(resources) > 0 && resources[0].Type == "Pod" {
		lastColHeader = "PHASE"
	}

	// Build column spec, overriding last column title if needed
	specs := make([]ColumnSpec, len(resourceColumnSpecs))
	copy(specs, resourceColumnSpecs)
	specs[len(specs)-1].Title = lastColHeader

	w := tableContentWidth(m.width, len(specs))
	m.resourcesTable.SetColumns(computeWeightedColumns(specs, w))
}

// rebuildEventsTable rebuilds the events table from current data with color-coded TYPE column.
func (m *Model) rebuildEventsTable() {
	events := m.getFilteredEvents()

	// Build rows with plain text cells
	rows := make([]table.Row, 0, len(events))
	for _, e := range events {
		styledCells := colorizeEventRow(e)
		rows = append(rows, table.Row(styledCells))
	}

	m.eventsTable.SetRows(rows)

	// Ensure cursor is valid after setting rows (same as resources table).
	if len(rows) > 0 {
		cursor := m.eventsTable.Cursor()
		if cursor < 0 || cursor >= len(rows) {
			m.eventsTable.SetCursor(0)
		}
	}

	w := tableContentWidth(m.width, len(eventColumnSpecs))
	m.eventsTable.SetColumns(computeWeightedColumns(eventColumnSpecs, w))
}

// getFilteredEvents returns events filtered by current selection.
func (m Model) getFilteredEvents() []data.Event {
	// In Pod view, show events for that specific pod
	if m.viewState.ViewType == data.PodView {
		filtered := []data.Event{}
		for _, event := range m.allEvents {
			if event.Parent == m.viewState.SelectedPod {
				filtered = append(filtered, event)
			}
		}
		return filtered
	}

	// In PodClique view, if a Pod is selected, show only that Pod's events
	if m.viewState.ViewType == data.PodCliqueView {
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 3 && selectedRow[1] == "Pod" {
			podName := selectedRow[2]
			filtered := []data.Event{}
			for _, event := range m.allEvents {
				if event.Parent == podName {
					filtered = append(filtered, event)
				}
			}
			return filtered
		}
	}

	// Default: return all events for current context
	return m.allEvents
}

// updatePodViewport updates the pod viewport with YAML content.
func (m *Model) updatePodViewport() {
	yaml, exists := m.podYAMLData[m.viewState.SelectedPod]
	if !exists {
		yaml = "# No YAML data available for pod: " + m.viewState.SelectedPod
	}
	m.podViewport.SetContent(yaml)
}
