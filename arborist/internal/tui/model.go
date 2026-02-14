package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrorEntry represents a single error entry in the error log.
type ErrorEntry struct {
	Time    time.Time
	Message string
}

// maxErrorLogEntries is the maximum number of errors kept in the error log.
const maxErrorLogEntries = 3

// replicaSeparator is the separator between a resource name and its replica index
// in display names like "my-pcs-replica-0". Used consistently across navigation,
// handlers, and model logic to avoid magic string duplication.
const replicaSeparator = "-replica-"

// replicaDisplayName constructs a display name for a virtual replica resource.
func replicaDisplayName(baseName, index string) string {
	return baseName + replicaSeparator + index
}

// newSearchInput creates a textinput.Model with standard defaults.
func newSearchInput(prompt string, width int, promptStyle lipgloss.Style) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = ""
	ti.CharLimit = 256
	ti.Width = width
	ti.Prompt = prompt
	ti.PromptStyle = promptStyle
	return ti
}

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
		{Title: "KIND", Weight: 3},
		{Title: "REASON", Weight: 3},
		{Title: "AGE", Weight: 1},
		{Title: "FROM", Weight: 3},
		{Title: "MESSAGE", Weight: 8},
	}

	topologyDomainColumnSpecs = []ColumnSpec{
		{Title: "DOMAIN", Weight: 2},
		{Title: "KEY", Weight: 5},
		{Title: "VALUES", Weight: 1},
	}

	topologyDrillValueColumnSpecs = []ColumnSpec{
		{Title: "VALUE", Weight: 1},
	}

	topologyPodColumnSpecs = []ColumnSpec{
		{Title: "NAMESPACE", Weight: 2},
		{Title: "NODE", Weight: 2},
		{Title: "NAME", Weight: 4},
		{Title: "TOPOLOGY", Weight: 3},
		{Title: "PHASE", Weight: 1},
	}

	containerColumnSpecs = []ColumnSpec{
		{Title: "NAME", Weight: 3},
		{Title: "IMAGE", Weight: 5},
		{Title: "STATE", Weight: 2},
		{Title: "READY", Weight: 1},
		{Title: "RESTARTS", Weight: 1},
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

	// Command mode (vim-style ":" lens switching)
	commandActive bool
	commandInput  textinput.Model

	// Lens edit mode (inline editing in the header Lens line, triggered by 'l')
	lensEditActive bool
	lensInput      textinput.Model

	// Autocomplete for lens/command inputs (shared candidate list)
	lensAutocomplete *Autocompleter

	// Data — all derived from the cache snapshot
	allResources       map[string][]data.Resource
	allEvents          []data.Event
	podYAMLData        map[string]string
	cachedTopologyInfo *data.TopologyInfo
	cachedSnapshot     *data.CacheSnapshot // latest snapshot from the global cache

	// Sub-models (bubbles components)
	resourcesTable       table.Model
	eventsTable          table.Model
	topologyDomainsTable table.Model
	topologyPodsTable    table.Model
	filterInput          textinput.Model
	podViewport          viewport.Model

	// YAML overlay (shown when user presses 'y' on any resource)
	yamlOverlay      OverlayModel
	yamlResourceName string // name of the resource being viewed
	yamlResourceType string // type of the resource being viewed

	// Logs overlay (shown when user presses 'l' on a pod/container)
	logsOverlay          OverlayModel
	logsPodName          string
	logsContainerName    string
	logsWrapEnabled      bool
	logsNamespace        string // namespace for re-fetching logs (autoscroll)
	logsAutoScroll       bool   // autoscroll (tail -f) toggle state
	logsHorizontalOffset int    // horizontal scroll offset (rune count from left)

	// Container view state
	containerInfos []data.ContainerInfo

	// Topology view state
	topologyViewData   *data.TopologyViewData
	topologyDrillStack []data.TopologyDrillSelection

	// GPU data (from cache, used in Forest view for GPU columns)
	gpuSummary *data.GPUSummary

	// Dependencies (injected)
	cache        data.GlobalCache
	cacheStarted bool
	cacheSynced  bool
	ctx          context.Context

	// Layout
	width  int
	height int
	ready  bool // true after first WindowSizeMsg

	// Configuration
	debug              bool
	forestResourceType string // "pcs" (default), "pc", "pcsg", "pod"
	namespace          string // if non-empty, scope to this namespace
	allNamespaces      bool   // true = show all namespaces (default)

	// Cluster info (resolved from kubeconfig at startup)
	contextName      string
	clusterName      string
	userName         string
	k8sVersion       string
	arboristVersion  string

	// Error state
	lastError      error
	errorLog       []ErrorEntry
	errorLogVisible bool
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

// WithNamespace sets the namespace to scope resources to.
// When non-empty, only resources in this namespace are shown.
func WithNamespace(ns string) Option {
	return func(m *Model) {
		m.namespace = ns
		if ns != "" {
			m.allNamespaces = false
		}
	}
}

// WithAllNamespaces sets whether to show resources from all namespaces.
func WithAllNamespaces(all bool) Option {
	return func(m *Model) {
		m.allNamespaces = all
	}
}

// WithGlobalCache sets the global cache for the model.
func WithGlobalCache(cache data.GlobalCache) Option {
	return func(m *Model) {
		m.cache = cache
	}
}

// WithForestResourceType sets the initial resource type for the forest view.
// Valid values: "pcs" (default), "pc", "pcsg", "pod" and their long forms.
func WithForestResourceType(rt string) Option {
	return func(m *Model) {
		m.forestResourceType = normalizeResourceType(rt)
	}
}

// WithFilter sets the initial filter text.
func WithFilter(filter string) Option {
	return func(m *Model) {
		m.filterText = filter
		m.filterInput.SetValue(filter)
	}
}

// normalizeResourceType maps long resource type names to their short forms.
func normalizeResourceType(rt string) string {
	switch strings.ToLower(strings.TrimSpace(rt)) {
	case "podcliqueset", "pcs":
		return "pcs"
	case "podclique", "pc":
		return "pc"
	case "podcliquescalinggroup", "pcsg":
		return "pcsg"
	case "pod":
		return "pod"
	default:
		return "pcs"
	}
}

// NewModel creates a new Model with the given GlobalCache and options.
func NewModel(cache data.GlobalCache, opts ...Option) Model {
	ti := newSearchInput("/ ", 40, FilterBarStyle)
	ci := newSearchInput(": ", 40, CommandBarStyle)
	li := newSearchInput("", 30, lipgloss.NewStyle())

	// Initialize autocomplete for lens/command inputs
	ac := NewAutocompleter(LensCommandNames())
	ac.ConfigureInput(&ci, AutocompleteSuggestionStyle)
	ac.ConfigureInput(&li, AutocompleteSuggestionStyle)

	m := Model{
		viewState: data.ViewState{
			ViewType: data.ForestView,
		},
		activePane:         data.ResourcesPane,
		allResources:       make(map[string][]data.Resource),
		podYAMLData:        make(map[string]string),
		cache:              cache,
		ctx:                context.Background(),
		filterInput:        ti,
		commandInput:       ci,
		lensInput:          li,
		yamlOverlay:        OverlayModel{SearchInput: newSearchInput("/ ", 40, FilterBarStyle)},
		logsOverlay:        OverlayModel{SearchInput: newSearchInput("/ ", 40, FilterBarStyle)},
		lensAutocomplete:   ac,
		forestResourceType: "pcs", // default resource type
		allNamespaces:      true,  // default: show all namespaces
	}

	// Apply options
	for _, opt := range opts {
		opt(&m)
	}

	// Initialize tables with empty data (will be populated after data loads)
	m.resourcesTable = createTableModel(resourceColumnSpecs, true)
	m.eventsTable = createTableModel(eventColumnSpecs, false)
	m.topologyDomainsTable = createTableModel(topologyDomainColumnSpecs, true)
	m.topologyPodsTable = createTableModel(topologyPodColumnSpecs, false)

	return m
}

// resizeTable updates a table's width, height, and styles to match the current
// terminal dimensions. Used by handleWindowSize to avoid duplicating resize
// logic for every table in the TUI.
func resizeTable(t *table.Model, width, height int) {
	t.SetWidth(width)
	t.SetHeight(height)
	t.SetStyles(ArboristTableStylesWithWidth(width))
}

// saveCursorState saves the name at nameCol and the current cursor index for later restoration.
func saveCursorState(t *table.Model, nameCol int) (string, int) {
	prevName := ""
	if row := t.SelectedRow(); len(row) > nameCol {
		prevName = row[nameCol]
	}
	return prevName, t.Cursor()
}

// restoreCursorState tries to find the row with prevName at nameCol, otherwise clamps
// the cursor to valid range.
func restoreCursorState(t *table.Model, rows []table.Row, prevName string, prevCursor, nameCol int) {
	if len(rows) == 0 {
		return
	}
	if prevName != "" {
		for i, row := range rows {
			if len(row) > nameCol && row[nameCol] == prevName {
				t.SetCursor(i)
				return
			}
		}
	}
	if prevCursor >= len(rows) {
		t.SetCursor(len(rows) - 1)
	} else if prevCursor >= 0 {
		t.SetCursor(prevCursor)
	} else {
		t.SetCursor(0)
	}
}

// createTableModel creates a table.Model with the given column spec and focus state.
// This is the shared factory used by all table types in the TUI.
func createTableModel(specs []ColumnSpec, focused bool) table.Model {
	columns := computeWeightedColumns(specs, 80) // placeholder widths until first resize
	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(focused),
		table.WithHeight(10),
	)
	t.SetStyles(ArboristTableStyles())
	return t
}

// Init initializes the model and returns the initial command.
func (m Model) Init() tea.Cmd {
	debugLog("starting arborist TUI (bubbletea)")
	// The cache will be started after the first WindowSizeMsg (when we know the terminal is ready).
	// Return nil — no data loading needed until the cache is synced.
	return nil
}

// addError prepends a new error entry to the error log, caps at maxErrorLogEntries
// (oldest dropped), and makes the error log visible. Triggers a layout resize so
// that tables shrink to make room for the error log frame.
func (m *Model) addError(message string) {
	prevCount := len(m.errorLog)
	entry := ErrorEntry{
		Time:    time.Now(),
		Message: message,
	}
	m.errorLog = append([]ErrorEntry{entry}, m.errorLog...)
	if len(m.errorLog) > maxErrorLogEntries {
		m.errorLog = m.errorLog[:maxErrorLogEntries]
	}
	wasVisible := m.errorLogVisible
	m.errorLogVisible = true
	// Resize layout if the error log frame height changed (first show, or
	// entry count grew within cap). Skip if terminal size hasn't been received yet.
	if m.ready && (!wasVisible || len(m.errorLog) != prevCount) {
		m.resizeLayout()
	}
}

// topologyAvailable returns true only when topology data exists with at least
// one domain. Used to guard the 't' key and ":topology" command.
func (m *Model) topologyAvailable() bool {
	return m.topologyViewData != nil && len(m.topologyViewData.Domains) > 0
}

// topologyColumnVisible returns true when the TOPOLOGY column should be shown
// in the resources table. Requires both that the cache has synced (so we know
// cluster state) and that a ClusterTopology resource exists (i.e. topology is
// available). Before cache sync, we don't show or hide — the column is hidden
// by default since topologyAvailable() returns false when topologyViewData is nil.
func (m *Model) topologyColumnVisible() bool {
	return m.topologyAvailable()
}

// rebuildContainersTable rebuilds the resources table for the ContainersView.
func (m *Model) rebuildContainersTable() {
	prevName, prevCursor := saveCursorState(&m.resourcesTable, 0)

	// Clear and set columns
	m.resourcesTable.SetRows([]table.Row{})
	w := tableContentWidth(m.width, len(containerColumnSpecs))
	m.resourcesTable.SetColumns(computeWeightedColumns(containerColumnSpecs, w))

	// Build rows from containerInfos
	rows := make([]table.Row, 0, len(m.containerInfos))
	for _, c := range m.containerInfos {
		readyStr := "false"
		if c.Ready {
			readyStr = "true"
		}
		rows = append(rows, table.Row{
			c.Name,
			c.Image,
			c.State,
			readyStr,
			fmt.Sprintf("%d", c.RestartCount),
		})
	}

	m.resourcesTable.SetRows(rows)
	restoreCursorState(&m.resourcesTable, rows, prevName, prevCursor, 0)
}

// rebuildResourcesTable rebuilds the resources table from current data with color-coded cells.
func (m *Model) rebuildResourcesTable() {
	if m.viewState.ViewType == data.ContainersView {
		m.rebuildContainersTable()
		return
	}

	// Save cursor state for restoration after rebuild.
	// The NAME column is at index 2 (NAMESPACE=0, TYPE=1, NAME=2).
	prevName, prevCursor := saveCursorState(&m.resourcesTable, 2)

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

	// Determine GPU types for dynamic columns
	var gpuTypes []string
	if m.gpuSummary != nil && len(m.gpuSummary.GPUTypes) > 0 {
		gpuTypes = m.gpuSummary.GPUTypes
	}

	// Update column header based on view type
	lastColHeader := "SCHEDULED"
	if m.viewState.ViewType == data.PodCliqueView && len(resources) > 0 && resources[0].Type == "Pod" {
		lastColHeader = "PHASE"
	}

	// Build dynamic column spec — must set columns BEFORE rows to avoid
	// column/row count mismatch panics (SetRows triggers UpdateViewport
	// which renders rows against the current column definitions).
	specs := m.buildResourceColumnSpecs(gpuTypes, lastColHeader)

	// Clear rows, set columns, then set new rows (same pattern as rebuildTopologyDomainsTable).
	m.resourcesTable.SetRows([]table.Row{})
	w := tableContentWidth(m.width, len(specs))
	m.resourcesTable.SetColumns(computeWeightedColumns(specs, w))

	// Build rows with plain text cells including GPU columns
	rows := make([]table.Row, 0, len(resources))
	for _, r := range resources {
		styledCells := m.colorizeResourceRowWithGPU(r, gpuTypes)
		rows = append(rows, table.Row(styledCells))
	}

	m.resourcesTable.SetRows(rows)
	restoreCursorState(&m.resourcesTable, rows, prevName, prevCursor, 2)
}

// buildResourceColumnSpecs builds the column spec for the resources table,
// inserting dynamic GPU columns between TOPOLOGY and READY.
// The TOPOLOGY column is only included when topology data is available.
func (m *Model) buildResourceColumnSpecs(gpuTypes []string, lastColTitle string) []ColumnSpec {
	// Base columns: NAMESPACE, TYPE, NAME, [TOPOLOGY], [GPU types], READY
	specs := []ColumnSpec{
		{Title: "NAMESPACE", Weight: 2},
		{Title: "TYPE", Weight: 3},
		{Title: "NAME", Weight: 5},
	}
	if m.topologyColumnVisible() {
		specs = append(specs, ColumnSpec{Title: "TOPOLOGY", Weight: 3})
	}

	// GPU type columns (one per discovered GPU type)
	for _, gpuType := range gpuTypes {
		specs = append(specs, ColumnSpec{Title: gpuType, Weight: 1})
	}

	specs = append(specs, ColumnSpec{Title: "READY", Weight: 2})

	// Final column: SCHEDULED or PHASE
	specs = append(specs, ColumnSpec{Title: lastColTitle, Weight: 2})

	return specs
}

// colorizeResourceRowWithGPU returns plain text for each column value including GPU columns.
// The row format is: [Namespace, Type, Name, [Topology], <gpu1>, <gpu2>, ..., Ready, Scheduled]
// The Topology field is only included when topology data is available.
func (m *Model) colorizeResourceRowWithGPU(r data.Resource, gpuTypes []string) []string {
	row := []string{r.Namespace, r.Type, r.Name}
	if m.topologyColumnVisible() {
		row = append(row, r.Topology)
	}

	// Add GPU count values
	if len(gpuTypes) > 0 {
		counts := m.gpuCountsForResource(r)
		isPending := m.isResourcePending(r)

		for _, gpuType := range gpuTypes {
			if isPending {
				row = append(row, "?")
			} else if counts != nil {
				count := counts[gpuType]
				if count > 0 {
					row = append(row, fmt.Sprintf("%d", count))
				} else {
					row = append(row, "0")
				}
			} else {
				row = append(row, "0")
			}
		}
	}

	row = append(row, r.Ready)
	row = append(row, r.Scheduled)
	return row
}

// gpuCountsForResource returns the GPU counts for a given resource based on its type and name.
func (m *Model) gpuCountsForResource(r data.Resource) data.GPUCounts {
	if m.gpuSummary == nil {
		return nil
	}

	switch r.Type {
	case "PodCliqueSet":
		return m.gpuSummary.ByPCS[r.Name]
	case "(PodCliqueSet replica)":
		// Name format: "pcsName-replica-INDEX" — extract pcsName and index
		pcsName := m.viewState.SelectedPodCliqueSet
		replicaIndex := extractReplicaIndex(r.Name)
		if pcsName != "" && replicaIndex != "" {
			return m.gpuSummary.ByReplica[pcsName+"/"+replicaIndex]
		}
	case "PodCliqueScalingGroup":
		return m.gpuSummary.ByPCSG[r.Name]
	case "(PodCliqueScalingGroup replica)":
		// Name format: "pcsgName-replica-INDEX" — extract pcsgName and index
		pcsgName := m.viewState.SelectedScalingGroup
		replicaIndex := extractReplicaIndex(r.Name)
		if pcsgName != "" && replicaIndex != "" {
			return m.gpuSummary.ByPCSGReplica[pcsgName+"/"+replicaIndex]
		}
	case "PodClique":
		return m.gpuSummary.ByPodClique[r.Name]
	case "Pod":
		return m.gpuSummary.ByPod[r.Name]
	}

	return nil
}

// isResourcePending returns true if the resource (or any of its descendant pods) is pending
// with GPU requests that can't be attributed to a GPU type yet.
func (m *Model) isResourcePending(r data.Resource) bool {
	if m.gpuSummary == nil || len(m.gpuSummary.PendingGPUPods) == 0 {
		return false
	}

	// For pods, check directly
	if r.Type == "Pod" {
		_, isPending := m.gpuSummary.PendingGPUPods[r.Name]
		return isPending
	}

	return false
}

// extractReplicaIndex extracts the replica index from a name like "foo-replica-0".
func extractReplicaIndex(name string) string {
	parts := strings.Split(name, replicaSeparator)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
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
	// In Pod view or Containers view, show events for that specific pod
	if m.viewState.ViewType == data.PodView || m.viewState.ViewType == data.ContainersView {
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

// rebuildTopologyDomainsTable rebuilds the top pane of the Topology view.
// If the drill stack is empty, shows domain rows. If drilled in, shows distinct
// values for the current domain scoped by the breadcrumb.
func (m *Model) rebuildTopologyDomainsTable() {
	if m.topologyViewData == nil {
		m.topologyDomainsTable.SetRows([]table.Row{})
		return
	}

	// Save cursor state for restoration after rebuild
	prevName, prevCursor := saveCursorState(&m.topologyDomainsTable, 0)

	if len(m.topologyDrillStack) == 0 {
		// Top-level: show domain rows (DOMAIN, KEY, VALUES)
		// Clear rows, set columns, then set rows to avoid column/row count mismatch
		// panics (SetColumns and SetRows both trigger UpdateViewport which renders).
		m.topologyDomainsTable.SetRows([]table.Row{})
		w := tableContentWidth(m.width, len(topologyDomainColumnSpecs))
		m.topologyDomainsTable.SetColumns(computeWeightedColumns(topologyDomainColumnSpecs, w))

		rows := make([]table.Row, 0, len(m.topologyViewData.Domains))
		for _, d := range m.topologyViewData.Domains {
			valuesStr := "—"
			if d.ValuesCount >= 0 {
				valuesStr = fmt.Sprintf("%d", d.ValuesCount)
			}
			rows = append(rows, table.Row{d.Domain, d.Key, valuesStr})
		}
		m.topologyDomainsTable.SetRows(rows)
	} else {
		// Drilled in: show distinct values for the current domain
		currentDomain, currentKey := m.currentTopologyDomain()
		if currentDomain == "" {
			m.topologyDomainsTable.SetRows([]table.Row{})
			return
		}

		// Get matching nodes based on breadcrumb constraints
		matchingNodes := data.FilterNodesByBreadcrumb(m.topologyViewData.NodeLabels, m.topologyDrillStack)

		// Compute GPU summary for domain values
		gpuSummary := data.ComputeDomainGPUSummary(
			currentKey,
			matchingNodes,
			m.topologyViewData.NodeLabels,
			m.topologyViewData.NodeGPUProducts,
			m.topologyViewData.NodeGPUCapacity,
			m.topologyViewData.RawPods,
		)

		// Compute pod counts per domain value
		podCounts := data.ComputeDomainPodCounts(
			currentKey,
			matchingNodes,
			m.topologyViewData.NodeLabels,
			m.topologyViewData.RawPods,
		)

		// Build dynamic column spec: VALUE + <GPU>¹ columns + GPU PODS + PODS
		specs := []ColumnSpec{
			{Title: "VALUE", Weight: 2},
		}
		for _, gpuType := range gpuSummary.GPUTypes {
			specs = append(specs, ColumnSpec{Title: gpuType + "¹", Weight: 3})
		}
		specs = append(specs,
			ColumnSpec{Title: "GPU PODS", Weight: 1},
			ColumnSpec{Title: "PODS", Weight: 1},
		)

		// Clear rows, set columns, then set rows to avoid column/row mismatch panics.
		m.topologyDomainsTable.SetRows([]table.Row{})
		w := tableContentWidth(m.width, len(specs))
		m.topologyDomainsTable.SetColumns(computeWeightedColumns(specs, w))

		// Get distinct values for the current domain key
		values := data.DistinctValuesForDomain(m.topologyViewData.NodeLabels, currentKey, matchingNodes)

		rows := make([]table.Row, 0, len(values))
		for _, v := range values {
			pc := podCounts[v]
			row := table.Row{v}
			// GPU columns: bar graph + grove/other/total
			valueCounts := gpuSummary.ByValue[v]
			for _, gpuType := range gpuSummary.GPUTypes {
				if valueCounts != nil {
					counts := valueCounts[gpuType]
					row = append(row, data.FormatGPUBar(counts.Grove, counts.Other, counts.Total, 20))
				} else {
					row = append(row, data.FormatGPUBar(0, 0, 0, 20))
				}
			}
			// GPU PODS, PODS
			row = append(row,
				fmt.Sprintf("%d", pc.GPU),
				fmt.Sprintf("%d", pc.Total),
			)
			rows = append(rows, row)
		}
		m.topologyDomainsTable.SetRows(rows)

		_ = currentDomain // used for title rendering
	}

	// Restore cursor by name or clamp to valid range
	restoreCursorState(&m.topologyDomainsTable, m.topologyDomainsTable.Rows(), prevName, prevCursor, 0)
}

// rebuildTopologyPodsTable rebuilds the bottom pane of the Topology view.
// It shows pods on nodes matching the current breadcrumb constraints.
func (m *Model) rebuildTopologyPodsTable() {
	if m.topologyViewData == nil {
		m.topologyPodsTable.SetRows([]table.Row{})
		return
	}

	// Save cursor state for restoration after rebuild
	prevName, prevCursor := saveCursorState(&m.topologyPodsTable, 2)

	// Get matching nodes based on breadcrumb
	matchingNodes := data.FilterNodesByBreadcrumb(m.topologyViewData.NodeLabels, m.topologyDrillStack)

	// Additionally filter by the currently highlighted row in the domains table.
	if len(m.topologyDrillStack) == 0 {
		// At top-level: don't show any pods until the user drills into a domain.
		m.topologyPodsTable.SetRows([]table.Row{})
		w := tableContentWidth(m.width, len(topologyPodColumnSpecs))
		m.topologyPodsTable.SetColumns(computeWeightedColumns(topologyPodColumnSpecs, w))
		return
	} else {
		// Drilled into a values list: highlighted row is a value — filter to nodes
		// where the current domain's label key matches the highlighted value.
		lastEntry := m.topologyDrillStack[len(m.topologyDrillStack)-1]
		if lastEntry.Value == "" {
			// Showing the values list for this domain; use the highlighted value
			selectedRow := m.topologyDomainsTable.SelectedRow()
			if len(selectedRow) >= 1 && selectedRow[0] != "" {
				highlightedValue := selectedRow[0]
				var filtered []string
				for _, nodeName := range matchingNodes {
					if labels, ok := m.topologyViewData.NodeLabels[nodeName]; ok {
						if labels[lastEntry.Key] == highlightedValue {
							filtered = append(filtered, nodeName)
						}
					}
				}
				matchingNodes = filtered
			}
		}
	}

	// Filter pods by matching nodes
	filteredPods := data.FilterPodsByNodes(m.topologyViewData.Pods, matchingNodes)

	// Sort pods by name for stable, predictable display order
	sort.Slice(filteredPods, func(i, j int) bool {
		return filteredPods[i].Name < filteredPods[j].Name
	})

	rows := make([]table.Row, 0, len(filteredPods))
	for _, p := range filteredPods {
		rows = append(rows, table.Row{p.Namespace, p.Node, p.Name, p.Topology, p.Phase})
	}
	m.topologyPodsTable.SetRows(rows)

	// Set column widths
	w := tableContentWidth(m.width, len(topologyPodColumnSpecs))
	m.topologyPodsTable.SetColumns(computeWeightedColumns(topologyPodColumnSpecs, w))

	// Restore cursor by name or clamp to valid range
	restoreCursorState(&m.topologyPodsTable, rows, prevName, prevCursor, 2)
}

// currentTopologyDomain returns the domain name and label key for the current
// drill-down level. If drilled into a domain but no value selected yet, it
// returns that domain. If a value was selected, it returns the next domain
// in the hierarchy.
func (m *Model) currentTopologyDomain() (string, string) {
	if m.topologyViewData == nil || len(m.topologyViewData.Domains) == 0 {
		return "", ""
	}

	// Find the domain we should be showing values for
	if len(m.topologyDrillStack) == 0 {
		return "", ""
	}

	lastEntry := m.topologyDrillStack[len(m.topologyDrillStack)-1]

	// If the last entry has no value, we're showing values for that domain
	if lastEntry.Value == "" {
		return lastEntry.Domain, lastEntry.Key
	}

	// Last entry has a value — find the next domain in the hierarchy
	domains := m.topologyViewData.Domains
	for i, d := range domains {
		if d.Domain == lastEntry.Domain && i+1 < len(domains) {
			next := domains[i+1]
			return next.Domain, next.Key
		}
	}

	return "", "" // at the narrowest domain
}
