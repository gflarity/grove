package main

import (
	"context"
	"fmt"
	"strings"

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
	const frameBorders = 4 // 2 chars on each side for the rounded border
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
	viewState    ViewState
	activePane   Pane
	filterActive bool
	filterText   string

	// Data
	allResources       map[string][]Resource
	allEvents          []Event
	podYAMLData        map[string]string
	cachedTopologyInfo *TopologyInfo
	cachedPods         map[string]CachedPodInfo
	cachedNodeLabels   map[string]map[string]string

	// Sub-models (bubbles components)
	resourcesTable table.Model
	eventsTable    table.Model
	filterInput    textinput.Model
	podViewport    viewport.Model

	// Dependencies (injected)
	provider DataProvider
	ctx      context.Context

	// Layout
	width  int
	height int
	ready  bool // true after first WindowSizeMsg

	// Configuration
	debug bool

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

// NewModel creates a new Model with the given DataProvider and options.
func NewModel(provider DataProvider, opts ...Option) Model {
	// Initialize filter input
	ti := textinput.New()
	ti.Placeholder = "Filter..."
	ti.CharLimit = 256
	ti.Width = 40
	ti.Prompt = "/ "
	ti.PromptStyle = FilterBarStyle

	m := Model{
		viewState: ViewState{
			viewType: ForestView,
		},
		activePane:   ResourcesPane,
		allResources: make(map[string][]Resource),
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

// Update handles messages and updates the model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Update debug context and log the message
	debugSetContext(m.viewState.viewType, m.activePane, m.filterActive)
	debugLogMsg(msg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	// Data messages
	case ForestDataMsg:
		return m.handleForestData(msg)

	case ReplicaDataMsg:
		return m.handleReplicaData(msg)

	case ReplicaChildrenMsg:
		return m.handleReplicaChildren(msg)

	case PCSGChildrenMsg:
		return m.handlePCSGChildren(msg)

	case PodCliqueChildrenMsg:
		return m.handlePodCliqueChildren(msg)

	case EventsMsg:
		return m.handleEvents(msg)

	case PodYAMLMsg:
		return m.handlePodYAML(msg)

	case TopologyInfoMsg:
		return m.handleTopologyInfo(msg)

	case PodInfoMsg:
		return m.handlePodInfo(msg)

	case NodeLabelsMsg:
		return m.handleNodeLabels(msg)

	case ErrorMsg:
		debugLog("ERROR: %s: %v", msg.Operation, msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	return m, nil
}

// handleWindowSize handles terminal resize events.
func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	// Calculate table heights
	// Layout: empty line(1) + header frame(3) + resources frame + events frame + menu(1)
	fixedLines := 10 // header area + frame borders + menu
	if m.filterActive {
		fixedLines++
	}
	availableHeight := m.height - fixedLines
	paneHeight := availableHeight / 2
	if paneHeight < 5 {
		paneHeight = 5
	}

	// Table dimensions - width is terminal minus frame borders (2 on each side)
	tableWidth := m.width - 4
	m.resourcesTable.SetWidth(tableWidth)
	m.resourcesTable.SetHeight(paneHeight)
	m.eventsTable.SetWidth(tableWidth)
	m.eventsTable.SetHeight(paneHeight)

	// Update table styles so the selected-row highlight spans the full row width
	styledWidth := ArboristTableStylesWithWidth(tableWidth)
	m.resourcesTable.SetStyles(styledWidth)
	m.eventsTable.SetStyles(styledWidth)

	// Update viewport for pod view
	m.podViewport.Width = tableWidth
	m.podViewport.Height = paneHeight

	// Update filter input width
	m.filterInput.Width = m.width - 10

	// Rebuild tables
	m.rebuildResourcesTable()
	m.rebuildEventsTable()

	if !m.ready {
		m.ready = true
		debugLogWithContext("window ready: %dx%d", m.width, m.height)
	}

	return m, nil
}

// handleKeyMsg handles keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C always quits
	if msg.Type == tea.KeyCtrlC {
		debugLogWithContext("quitting (ctrl+c)")
		return m, tea.Quit
	}

	// Filter mode has different key handling
	if m.filterActive {
		return m.handleFilterModeKey(msg)
	}

	// Normal mode key handling
	return m.handleNormalModeKey(msg)
}

// handleFilterModeKey handles keys when filter mode is active.
func (m Model) handleFilterModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Exit filter mode and clear filter
		m.filterActive = false
		m.filterText = ""
		m.filterInput.SetValue("")
		m.rebuildResourcesTable()
		debugLogWithContext("filter mode deactivated (cleared)")
		return m, nil

	case tea.KeyEnter:
		// Exit filter mode but keep filter applied
		m.filterActive = false
		m.filterText = m.filterInput.Value()
		m.rebuildResourcesTable()
		debugLogWithContext("filter mode deactivated (applied: %q)", m.filterText)
		return m, nil

	case tea.KeyTab:
		// Allow pane switching while filtering
		m.switchPane()
		return m, nil

	default:
		// Pass other keys to the filter input
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.filterText = m.filterInput.Value()
		m.rebuildResourcesTable()
		return m, cmd
	}
}

// handleNormalModeKey handles keys in normal (non-filter) mode.
func (m Model) handleNormalModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab:
		m.switchPane()
		return m, nil

	case tea.KeyEsc:
		return m.navigateBack()

	case tea.KeyEnter:
		if m.activePane == ResourcesPane {
			return m.navigateInto()
		}
		return m, nil

	case tea.KeyUp, tea.KeyDown:
		// Pass to active table
		if m.activePane == ResourcesPane {
			if m.viewState.viewType == PodView {
				var cmd tea.Cmd
				m.podViewport, cmd = m.podViewport.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.resourcesTable, cmd = m.resourcesTable.Update(msg)
			// Load events for newly selected resource
			m.loadEventsForSelection()
			return m, cmd
		}
		var cmd tea.Cmd
		m.eventsTable, cmd = m.eventsTable.Update(msg)
		return m, cmd

	case tea.KeyRunes:
		switch msg.String() {
		case "q", "Q":
			debugLogWithContext("quitting (q)")
			return m, tea.Quit
		case "/":
			m.filterActive = true
			m.filterInput.SetValue(m.filterText)
			m.filterInput.Focus()
			debugLogWithContext("filter mode activated")
			return m, textinput.Blink
		}
	}

	return m, nil
}

// switchPane toggles between Resources and Events panes.
func (m *Model) switchPane() {
	if m.activePane == ResourcesPane {
		m.activePane = EventsPane
		m.resourcesTable.Blur()
		m.eventsTable.Focus()
	} else {
		m.activePane = ResourcesPane
		m.eventsTable.Blur()
		m.resourcesTable.Focus()
	}
	debugLogWithContext("switched pane to %s", paneName(m.activePane))
}

// loadEventsForSelection loads events based on the current selection.
func (m *Model) loadEventsForSelection() {
	// This is called when selection changes - we'll trigger event reload via commands
	// For now, events are already loaded when navigating
}

// View is implemented in views.go

// getCurrentViewKey returns the key for looking up resources in allResources map.
func (m Model) getCurrentViewKey() string {
	switch m.viewState.viewType {
	case ForestView:
		return "forest"
	case PodCliqueSetView:
		return "PodCliqueSet/" + m.viewState.selectedPodCliqueSet
	case PodCliqueSetReplicaView:
		return "PodCliqueSetReplica/" + m.viewState.selectedPodCliqueSet + "/" + m.viewState.selectedReplicaIndex
	case PodCliqueScalingGroupView:
		return "PodCliqueScalingGroup/" + m.viewState.selectedScalingGroup
	case PodCliqueView:
		return "PodClique/" + m.viewState.selectedPodClique
	case PodView:
		return "" // Pod view doesn't list resources
	}
	return "forest"
}

// rebuildResourcesTable rebuilds the resources table from current data with color-coded cells.
func (m *Model) rebuildResourcesTable() {
	viewKey := m.getCurrentViewKey()
	resources, exists := m.allResources[viewKey]
	if !exists {
		resources = []Resource{}
	}

	// Apply filter if active
	if m.filterText != "" {
		filter := strings.ToLower(m.filterText)
		var filtered []Resource
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
	if m.viewState.viewType == PodCliqueView && len(resources) > 0 && resources[0].Type == "Pod" {
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
func (m Model) getFilteredEvents() []Event {
	// In Pod view, show events for that specific pod
	if m.viewState.viewType == PodView {
		filtered := []Event{}
		for _, event := range m.allEvents {
			if event.Parent == m.viewState.selectedPod {
				filtered = append(filtered, event)
			}
		}
		return filtered
	}

	// In PodClique view, if a Pod is selected, show only that Pod's events
	if m.viewState.viewType == PodCliqueView {
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 3 && selectedRow[1] == "Pod" {
			podName := selectedRow[2]
			filtered := []Event{}
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
	yaml, exists := m.podYAMLData[m.viewState.selectedPod]
	if !exists {
		yaml = "# No YAML data available for pod: " + m.viewState.selectedPod
	}
	m.podViewport.SetContent(yaml)
}

// navigateInto drills down into the selected resource.
func (m Model) navigateInto() (tea.Model, tea.Cmd) {
	// Clear any active filter when navigating
	m.filterText = ""
	m.filterInput.SetValue("")

	// Can't navigate if in Pod view
	if m.viewState.viewType == PodView {
		debugLogWithContext("navigateInto: already in PodView, ignoring")
		return m, nil
	}

	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		debugLogWithContext("navigateInto: no valid row selected")
		return m, nil
	}

	selectedNamespace := selectedRow[0]
	selectedType := selectedRow[1]
	selectedName := selectedRow[2]

	debugLogWithContext("navigateInto: type=%s name=%s namespace=%s", selectedType, selectedName, selectedNamespace)

	var cmds []tea.Cmd

	switch selectedType {
	case "PodCliqueSet":
		m.viewState.selectedPodCliqueSet = selectedName
		m.viewState.selectedReplicaIndex = ""
		m.viewState.selectedScalingGroup = ""
		m.viewState.selectedPodClique = ""
		m.viewState.selectedPod = ""

		// Clear cached topology data
		m.cachedTopologyInfo = nil
		m.cachedPods = nil
		m.cachedNodeLabels = nil

		// Load topology info, pod info, node labels, replicas, and events
		cmds = append(cmds,
			loadTopologyInfoCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadPodInfoCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadReplicasCmd(m.provider, m.ctx, selectedName, selectedNamespace),
		)

	case "PodCliqueSetReplica":
		// Extract replica index from name (format: "pcsname-replica-0")
		parts := strings.Split(selectedName, "-replica-")
		if len(parts) == 2 {
			oldViewType := m.viewState.viewType
			m.viewState.viewType = PodCliqueSetReplicaView
			m.viewState.selectedReplicaIndex = parts[1]
			m.viewState.selectedScalingGroup = ""
			m.viewState.selectedPodClique = ""
			m.viewState.selectedPod = ""

			debugLogStateTransition(oldViewType, PodCliqueSetReplicaView, fmt.Sprintf("replica=%q", parts[1]))

			// Load children resources and events for this replica
			cmds = append(cmds,
				loadReplicaChildrenCmd(m.provider, m.ctx, m.viewState.selectedPodCliqueSet, selectedNamespace, parts[1]),
				loadEventsForReplicaCmd(m.provider, m.ctx, m.viewState.selectedPodCliqueSet, selectedNamespace, parts[1]),
			)
		}

	case "PodCliqueScalingGroup":
		oldViewType := m.viewState.viewType
		m.viewState.viewType = PodCliqueScalingGroupView
		m.viewState.selectedScalingGroup = selectedName
		m.viewState.selectedPodClique = ""
		m.viewState.selectedPod = ""

		debugLogStateTransition(oldViewType, PodCliqueScalingGroupView, fmt.Sprintf("pcsg=%q", selectedName))

		// Load children resources and events for this PCSG
		cmds = append(cmds,
			loadPCSGChildrenCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadEventsForPCSGCmd(m.provider, m.ctx, selectedName, selectedNamespace),
		)

	case "PodClique":
		oldViewType := m.viewState.viewType
		m.viewState.viewType = PodCliqueView
		m.viewState.selectedPodClique = selectedName
		m.viewState.selectedPod = ""

		debugLogStateTransition(oldViewType, PodCliqueView, fmt.Sprintf("podClique=%q", selectedName))

		// Load children resources (Pods) and events for this PodClique
		cmds = append(cmds,
			loadPodCliqueChildrenCmd(m.provider, m.ctx, selectedName, selectedNamespace),
			loadEventsForPodCliqueCmd(m.provider, m.ctx, selectedName, selectedNamespace),
		)

	case "Pod":
		oldViewType := m.viewState.viewType
		m.viewState.viewType = PodView
		m.viewState.selectedPod = selectedName

		debugLogStateTransition(oldViewType, PodView, fmt.Sprintf("pod=%q", selectedName))

		// Load Pod YAML
		cmds = append(cmds, loadPodYAMLCmd(m.provider, m.ctx, selectedName, selectedNamespace))
	}

	m.rebuildResourcesTable()
	m.rebuildEventsTable()

	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// navigateBack goes up one level in the hierarchy.
func (m Model) navigateBack() (tea.Model, tea.Cmd) {
	// Clear any active filter when navigating
	m.filterText = ""
	m.filterInput.SetValue("")

	oldViewType := m.viewState.viewType
	debugLogWithContext("navigateBack: current viewType=%s", viewTypeName(oldViewType))

	var cmds []tea.Cmd

	switch m.viewState.viewType {
	case ForestView:
		// Already at root, nothing to do
		debugLogWithContext("navigateBack: already at ForestView, ignoring")
		return m, nil

	case PodCliqueSetView:
		// Go back to Forest
		m.viewState.viewType = ForestView
		m.viewState.selectedPodCliqueSet = ""
		m.viewState.selectedReplicaIndex = ""
		m.viewState.selectedScalingGroup = ""
		m.viewState.selectedPodClique = ""
		m.viewState.selectedPod = ""
		m.cachedTopologyInfo = nil
		m.cachedPods = nil
		m.cachedNodeLabels = nil

		debugLogStateTransition(oldViewType, ForestView, "")

		// Reload forest data
		cmds = append(cmds, loadForestDataCmd(m.provider, m.ctx))
		m.allEvents = []Event{}

	case PodCliqueSetReplicaView:
		// Check if we should go back to PodCliqueSet view or directly to Forest
		// (if there's only 1 replica, we skip PodCliqueSetView)
		viewKey := m.getCurrentViewKey()
		resources, exists := m.allResources[viewKey]
		namespace := "default"
		if exists && len(resources) > 0 {
			namespace = resources[0].Namespace
		}

		// Check how many replicas (by looking at PodCliqueSet key)
		pcsKey := "PodCliqueSet/" + m.viewState.selectedPodCliqueSet
		pcsResources := m.allResources[pcsKey]

		if len(pcsResources) == 1 {
			// Only 1 replica, so we came directly from Forest view
			m.viewState.viewType = ForestView
			m.viewState.selectedPodCliqueSet = ""
			m.viewState.selectedReplicaIndex = ""
			m.cachedTopologyInfo = nil
			m.cachedPods = nil
			m.cachedNodeLabels = nil

			debugLogStateTransition(oldViewType, ForestView, "single replica skip")

			cmds = append(cmds, loadForestDataCmd(m.provider, m.ctx))
			m.allEvents = []Event{}
		} else {
			// Multiple replicas, go back to PodCliqueSet view
			m.viewState.viewType = PodCliqueSetView
			m.viewState.selectedReplicaIndex = ""
			m.viewState.selectedScalingGroup = ""
			m.viewState.selectedPodClique = ""
			m.viewState.selectedPod = ""

			debugLogStateTransition(oldViewType, PodCliqueSetView, "")

			cmds = append(cmds,
				loadReplicasCmd(m.provider, m.ctx, m.viewState.selectedPodCliqueSet, namespace),
				loadEventsForPCSCmd(m.provider, m.ctx, m.viewState.selectedPodCliqueSet, namespace),
			)
		}

	case PodCliqueScalingGroupView:
		// Go back to PodCliqueSetReplica
		m.viewState.viewType = PodCliqueSetReplicaView
		m.viewState.selectedScalingGroup = ""
		m.viewState.selectedPodClique = ""
		m.viewState.selectedPod = ""

		debugLogStateTransition(oldViewType, PodCliqueSetReplicaView, "")

	case PodCliqueView:
		// Go back to parent (either PodCliqueSetReplica or PodCliqueScalingGroup)
		var newViewType ViewType
		if m.viewState.selectedScalingGroup != "" {
			newViewType = PodCliqueScalingGroupView
		} else {
			newViewType = PodCliqueSetReplicaView
		}
		m.viewState.viewType = newViewType
		m.viewState.selectedPodClique = ""
		m.viewState.selectedPod = ""

		debugLogStateTransition(oldViewType, newViewType, "")

	case PodView:
		// Go back to PodClique view
		m.viewState.viewType = PodCliqueView
		m.viewState.selectedPod = ""

		debugLogStateTransition(oldViewType, PodCliqueView, "")
	}

	m.rebuildResourcesTable()
	m.rebuildEventsTable()

	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// --- Data message handlers ---

// handleForestData handles ForestDataMsg.
func (m Model) handleForestData(msg ForestDataMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading forest data: %v", msg.Err)
		m.lastError = msg.Err
		m.allResources["forest"] = []Resource{}
	} else {
		debugLogWithContext("loaded %d PodCliqueSets", len(msg.Resources))
		m.allResources["forest"] = msg.Resources
	}
	m.rebuildResourcesTable()

	// If we have PCSes, load events for the first one
	if len(msg.Resources) > 0 {
		firstPCS := msg.Resources[0]
		return m, loadEventsForPCSCmd(m.provider, m.ctx, firstPCS.Name, firstPCS.Namespace)
	}
	return m, nil
}

// handleReplicaData handles ReplicaDataMsg.
func (m Model) handleReplicaData(msg ReplicaDataMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading replica data: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	debugLogWithContext("loaded %d replica indexes for %s/%s", len(msg.ReplicaIndexes), msg.Namespace, msg.PCSName)

	// If there's only 1 replica, skip directly to PodCliqueSetReplicaView
	if len(msg.ReplicaIndexes) == 1 {
		replicaIndex := msg.ReplicaIndexes[0]
		oldViewType := m.viewState.viewType
		m.viewState.viewType = PodCliqueSetReplicaView
		m.viewState.selectedReplicaIndex = replicaIndex

		debugLogStateTransition(oldViewType, PodCliqueSetReplicaView, fmt.Sprintf("single replica skip, replica=%q", replicaIndex))

		// Build virtual replica resources for tracking (needed for back navigation)
		key := "PodCliqueSet/" + msg.PCSName
		m.allResources[key] = []Resource{{
			Name:      msg.PCSName + "-replica-" + replicaIndex,
			Type:      "PodCliqueSetReplica",
			Namespace: msg.Namespace,
		}}

		// Store children data and load events
		return m, tea.Batch(
			loadReplicaChildrenCmd(m.provider, m.ctx, msg.PCSName, msg.Namespace, replicaIndex),
			loadEventsForReplicaCmd(m.provider, m.ctx, msg.PCSName, msg.Namespace, replicaIndex),
		)
	}

	// Multiple replicas, show PodCliqueSetView with replica list
	m.viewState.viewType = PodCliqueSetView

	// Build virtual PodCliqueSetReplica resources
	key := "PodCliqueSet/" + msg.PCSName
	resources := make([]Resource, 0, len(msg.ReplicaIndexes))

	for _, replicaIndex := range msg.ReplicaIndexes {
		// Calculate aggregate ready/scheduled counts from ScalingGroups and PodCliques
		var totalReady, totalScheduled, totalReplicas int

		for _, sg := range msg.ScalingGroupsByReplica[replicaIndex] {
			var ready, replicas int
			fmt.Sscanf(sg.Ready, "%d/%d", &ready, &replicas)
			totalReady += ready
			totalReplicas += replicas

			var scheduled, scheduledMax int
			fmt.Sscanf(sg.Scheduled, "%d/%d", &scheduled, &scheduledMax)
			totalScheduled += scheduled
		}

		for _, pc := range msg.PodCliquesByReplica[replicaIndex] {
			var ready, replicas int
			fmt.Sscanf(pc.Ready, "%d/%d", &ready, &replicas)
			totalReady += ready
			totalReplicas += replicas

			var scheduled, scheduledMax int
			fmt.Sscanf(pc.Scheduled, "%d/%d", &scheduled, &scheduledMax)
			totalScheduled += scheduled
		}

		// Resolve topology for replica
		replicaTopology := "N/A"
		if m.cachedTopologyInfo != nil && m.cachedTopologyInfo.PCSPackDomain != "" {
			replicaTopology = ResolveTopologyDisplay("", m.cachedTopologyInfo.PCSPackDomain)
			domain := extractDomain(replicaTopology)
			value := resolveTopologyValueByReplicaIndex(domain, msg.PCSName, replicaIndex, m.cachedTopologyInfo, m.cachedPods, m.cachedNodeLabels)
			replicaTopology = enhanceTopologyDisplay(replicaTopology, value)
		}

		resources = append(resources, Resource{
			Name:       fmt.Sprintf("%s-replica-%s", msg.PCSName, replicaIndex),
			Type:       "PodCliqueSetReplica",
			Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
			Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
			Namespace:  msg.Namespace,
			ParentType: "PodCliqueSet",
			ParentName: msg.PCSName,
			Topology:   replicaTopology,
		})
	}

	m.allResources[key] = resources
	m.rebuildResourcesTable()

	return m, loadEventsForPCSCmd(m.provider, m.ctx, msg.PCSName, msg.Namespace)
}

// handleReplicaChildren handles ReplicaChildrenMsg.
func (m Model) handleReplicaChildren(msg ReplicaChildrenMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading replica children: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	key := "PodCliqueSetReplica/" + msg.PCSName + "/" + msg.ReplicaIndex
	debugLogWithContext("loaded %d scaling groups and %d pod cliques for replica %s",
		len(msg.ScalingGroups), len(msg.PodCliques), msg.ReplicaIndex)

	// Set topology on PCSGs
	for i := range msg.ScalingGroups {
		pcsgConfigName := ExtractConfigName(msg.ScalingGroups[i].Name, msg.PCSName, msg.ReplicaIndex)
		if m.cachedTopologyInfo != nil {
			msg.ScalingGroups[i].Topology = m.cachedTopologyInfo.ResolvePCSGTopology(pcsgConfigName)
			domain := extractDomain(msg.ScalingGroups[i].Topology)
			value := resolveTopologyValue(domain, "grove.io/podcliquescalinggroup", msg.ScalingGroups[i].Name, m.cachedTopologyInfo, m.cachedPods, m.cachedNodeLabels)
			msg.ScalingGroups[i].Topology = enhanceTopologyDisplay(msg.ScalingGroups[i].Topology, value)
		} else {
			msg.ScalingGroups[i].Topology = "N/A"
		}
	}

	// Set topology on standalone PodCliques
	for i := range msg.PodCliques {
		cliqueTemplateName := ExtractConfigName(msg.PodCliques[i].Name, msg.PCSName, msg.ReplicaIndex)
		if m.cachedTopologyInfo != nil {
			msg.PodCliques[i].Topology = m.cachedTopologyInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
			domain := extractDomain(msg.PodCliques[i].Topology)
			value := resolveTopologyValue(domain, "grove.io/podclique", msg.PodCliques[i].Name, m.cachedTopologyInfo, m.cachedPods, m.cachedNodeLabels)
			msg.PodCliques[i].Topology = enhanceTopologyDisplay(msg.PodCliques[i].Topology, value)
		} else {
			msg.PodCliques[i].Topology = "N/A"
		}
	}

	// Combine scaling groups and pod cliques
	resources := append(msg.ScalingGroups, msg.PodCliques...)
	m.allResources[key] = resources
	m.rebuildResourcesTable()

	return m, nil
}

// handlePCSGChildren handles PCSGChildrenMsg.
func (m Model) handlePCSGChildren(msg PCSGChildrenMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading PCSG children: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	key := "PodCliqueScalingGroup/" + msg.PCSGName
	debugLogWithContext("loaded %d pod cliques for PCSG %s", len(msg.PodCliques), msg.PCSGName)

	// Set topology on PodCliques within this PCSG
	pcsgConfigName := ExtractConfigName(msg.PCSGName, m.viewState.selectedPodCliqueSet, m.viewState.selectedReplicaIndex)
	for i := range msg.PodCliques {
		cliqueTemplateName := ExtractCliqueTemplateNameFromPCSGChild(msg.PodCliques[i].Name, msg.PCSGName)
		if m.cachedTopologyInfo != nil {
			msg.PodCliques[i].Topology = m.cachedTopologyInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
			domain := extractDomain(msg.PodCliques[i].Topology)
			value := resolveTopologyValue(domain, "grove.io/podclique", msg.PodCliques[i].Name, m.cachedTopologyInfo, m.cachedPods, m.cachedNodeLabels)
			msg.PodCliques[i].Topology = enhanceTopologyDisplay(msg.PodCliques[i].Topology, value)
		} else {
			msg.PodCliques[i].Topology = "N/A"
		}
	}

	m.allResources[key] = msg.PodCliques
	m.rebuildResourcesTable()

	return m, nil
}

// handlePodCliqueChildren handles PodCliqueChildrenMsg.
func (m Model) handlePodCliqueChildren(msg PodCliqueChildrenMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading PodClique children: %v", msg.Err)
		m.lastError = msg.Err
		return m, nil
	}

	key := "PodClique/" + msg.PodCliqueName
	debugLogWithContext("loaded %d pods for PodClique %s", len(msg.Pods), msg.PodCliqueName)

	// Set topology on Pods (inherited from parent PodClique)
	basePodTopology := "N/A"
	if m.cachedTopologyInfo != nil {
		if m.viewState.selectedScalingGroup != "" {
			pcsgConfigName := ExtractConfigName(m.viewState.selectedScalingGroup, m.viewState.selectedPodCliqueSet, m.viewState.selectedReplicaIndex)
			cliqueTemplateName := ExtractCliqueTemplateNameFromPCSGChild(msg.PodCliqueName, m.viewState.selectedScalingGroup)
			effectiveClique := m.cachedTopologyInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
			basePodTopology = wrapInherited(effectiveClique)
		} else {
			cliqueTemplateName := ExtractConfigName(msg.PodCliqueName, m.viewState.selectedPodCliqueSet, m.viewState.selectedReplicaIndex)
			effectiveClique := m.cachedTopologyInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
			basePodTopology = wrapInherited(effectiveClique)
		}
	}

	domain := extractDomain(basePodTopology)
	for i := range msg.Pods {
		if domain != "" {
			nodeName := ""
			if cached, ok := m.cachedPods[msg.Pods[i].Name]; ok {
				nodeName = cached.NodeName
			}
			value := resolveTopologyValueForNode(domain, nodeName, m.cachedTopologyInfo, m.cachedNodeLabels)
			msg.Pods[i].Topology = enhanceTopologyDisplay(basePodTopology, value)
		} else {
			msg.Pods[i].Topology = basePodTopology
		}
	}

	m.allResources[key] = msg.Pods
	m.rebuildResourcesTable()

	return m, nil
}

// handleEvents handles EventsMsg.
func (m Model) handleEvents(msg EventsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading events: %v", msg.Err)
		m.lastError = msg.Err
		m.allEvents = []Event{}
	} else {
		debugLogWithContext("loaded %d events", len(msg.Events))
		m.allEvents = msg.Events
	}
	m.rebuildEventsTable()
	return m, nil
}

// handlePodYAML handles PodYAMLMsg.
func (m Model) handlePodYAML(msg PodYAMLMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading Pod YAML: %v", msg.Err)
		m.podYAMLData[msg.PodName] = fmt.Sprintf("# Error loading Pod YAML: %v", msg.Err)
	} else {
		debugLogWithContext("loaded %d bytes of YAML for Pod %s", len(msg.YAML), msg.PodName)
		m.podYAMLData[msg.PodName] = msg.YAML
	}
	m.updatePodViewport()
	return m, nil
}

// handleTopologyInfo handles TopologyInfoMsg.
func (m Model) handleTopologyInfo(msg TopologyInfoMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading topology info: %v", msg.Err)
		m.cachedTopologyInfo = nil
	} else if msg.TopologyInfo != nil {
		debugLogWithContext("built topology info: PCSPackDomain=%q, %d PCSGs, %d cliques",
			msg.TopologyInfo.PCSPackDomain, len(msg.TopologyInfo.PCSGPackDomains), len(msg.TopologyInfo.CliquePackDomains))
		m.cachedTopologyInfo = msg.TopologyInfo
	}
	return m, loadNodeLabelsCmd(m.provider, m.ctx, m.cachedTopologyInfo)
}

// handlePodInfo handles PodInfoMsg.
func (m Model) handlePodInfo(msg PodInfoMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading pod info: %v", msg.Err)
		m.cachedPods = nil
	} else {
		debugLogWithContext("cached %d pods for PCS %s/%s", len(msg.PodInfos), msg.Namespace, msg.PCSName)
		m.cachedPods = msg.PodInfos
	}
	return m, nil
}

// handleNodeLabels handles NodeLabelsMsg.
func (m Model) handleNodeLabels(msg NodeLabelsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debugLogWithContext("ERROR loading node labels: %v", msg.Err)
		m.cachedNodeLabels = nil
	} else {
		debugLogWithContext("cached labels for %d nodes", len(msg.NodeLabels))
		m.cachedNodeLabels = msg.NodeLabels
	}
	// Rebuild tables to apply topology values
	m.rebuildResourcesTable()
	return m, nil
}
