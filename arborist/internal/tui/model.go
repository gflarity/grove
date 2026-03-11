package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
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
	return clusterstate.ReplicaDisplayName(baseName, index)
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
// YAMLOverlayState groups all state for the YAML full-screen overlay.
type YAMLOverlayState struct {
	OverlayModel
	ResourceName string // name of the resource being viewed
	ResourceType string // type of the resource being viewed
}

// LogsOverlayState groups all state for the logs full-screen overlay.
type LogsOverlayState struct {
	OverlayModel
	PodName          string
	ContainerName    string
	WrapEnabled      bool
	Namespace        string // namespace for re-fetching logs (autoscroll)
	AutoScroll       bool   // autoscroll (tail -f) toggle state
	HorizontalOffset int    // horizontal scroll offset (rune count from left)
}

// ErrorState groups error tracking fields.
type ErrorState struct {
	lastError       error
	errorLog        []ErrorEntry
	errorLogVisible bool
}

// ClusterInfo groups kubeconfig-derived cluster metadata for display.
type ClusterInfo struct {
	contextName     string
	clusterName     string
	userName        string
	k8sVersion      string
	arboristVersion string
}

// LayoutState groups terminal layout fields.
type LayoutState struct {
	width  int
	height int
	ready  bool // true after first WindowSizeMsg
}

// InputModes groups all text-input mode fields (filter, command, view).
type InputModes struct {
	filterActive        bool
	filterText          string
	commandActive       bool
	commandInput        textinput.Model
	commandAutocomplete *Autocompleter
	viewEditActive      bool
	viewInput           textinput.Model
	viewAutocomplete    *Autocompleter
}

// TopologyState groups topology view fields.
type TopologyState struct {
	topologyViewData     *clusterstate.TopologyViewData
	topologyDrill        clusterstate.TopologyDrillStack
	topologyDomainsTable table.Model
	topologyPodsTable    table.Model
	cachedTopologyInfo   *clusterstate.TopologyInfo
}

// DataState groups all data derived from cache snapshots.
type DataState struct {
	allResources   map[string][]clusterstate.Resource
	allEvents      []clusterstate.Event
	podYAMLData    map[string]string
	cachedSnapshot *clusterstate.CacheSnapshot
	gpuSummary     *clusterstate.GPUSummary
	containerInfos []clusterstate.ContainerInfo
}

// CacheState groups cache lifecycle fields.
type CacheState struct {
	cache        clusterstate.GlobalCache
	cacheStarted bool
	cacheSynced  bool
	ctx          context.Context
}

// Config groups configuration fields.
type Config struct {
	debug              bool
	forestResourceType string // "pcs" (default), "pc", "pcsg", "pod"
	namespace          string // if non-empty, scope to this namespace
	allNamespaces      bool   // true = show all namespaces (default)
}

// Model is the main Bubble Tea model for the arborist TUI.
//
// Architecture notes (Elm / Bubble Tea conventions):
//
//   - Large Model struct: Bubble Tea requires a single Model with Update/View.
//     This is the Elm architecture — not a God object. The struct is intentionally
//     large because it IS the entire application state. Splitting it into separate
//     objects with their own update methods would fight the framework.
//
//   - Embedded sub-structs: These group related fields (ErrorState, ClusterInfo,
//     LayoutState, etc.) while keeping them directly accessible via Go's field
//     promotion. This is the Bubble Tea convention for organizing state — not a
//     sign that the Model should be decomposed into separate components.
//
//   - Value-receiver Update, pointer-receiver helpers: Update() uses a value
//     receiver (Bubble Tea's contract — it returns a new Model). Internal helpers
//     like rebuildResourcesTable() use pointer receivers because they mutate in
//     place. This mixed pattern looks inconsistent but is standard Bubble Tea
//     practice — taking &m of the value copy in Update, mutating through it,
//     then returning the copy.
//
//   - Many fields touched in applySnapshot: A single cache update triggers
//     rebuilds of all tables, events, and hierarchy clusterstate. This looks like
//     excessive coupling but is inherent to the Elm architecture — when state
//     changes, the entire view tree is rebuilt from the new state.
//
// Zero-value note: Model requires NewModel() for construction. The following
// fields need explicit initialization and are NOT safe at their zero value:
//   - allResources, podYAMLData (maps — nil map panics on write)
//   - filterInput, commandInput, viewInput (textinput.Model — need New())
//   - resourcesTable, eventsTable, topology tables (table.Model — need columns)
//   - yamlOverlay, logsOverlay (contain nested OverlayModel with search input)
//
// Fields with useful zero values (safe without init):
//   - ErrorState, ClusterInfo, LayoutState, Config (all scalar/bool/string)
//   - TopologyState (nil pointers checked before use)
//   - viewState, activePane (zero values are valid initial states)
type Model struct {
	// Embedded sub-structs (fields promoted for direct access)
	ErrorState
	ClusterInfo
	LayoutState
	InputModes
	TopologyState
	DataState
	CacheState
	Config

	// Core navigation state
	viewState  clusterstate.ViewState
	activePane clusterstate.Pane

	// Hierarchy UI components
	resourcesTable table.Model
	eventsTable    table.Model
	filterInput    textinput.Model
	podViewport    viewport.Model

	// Full-screen overlays
	yamlOverlay YAMLOverlayState
	logsOverlay LogsOverlayState
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
func WithGlobalCache(cache clusterstate.GlobalCache) Option {
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

// WithConnectionError records a K8s client error so the TUI displays it
// immediately on startup instead of showing empty data with no explanation.
func WithConnectionError(err error) Option {
	return func(m *Model) {
		m.lastError = err
		m.addError(fmt.Sprintf("Kubernetes connection: %v", err))
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
func NewModel(cache clusterstate.GlobalCache, opts ...Option) Model {
	ti := newSearchInput("/ ", 40, FilterBarStyle)
	ci := newSearchInput(": ", 40, CommandBarStyle)
	vi := newSearchInput("", 30, lipgloss.NewStyle())

	// Initialize separate autocompleters for command mode and view mode
	cmdAC := NewAutocompleter(CommandModeNames())
	cmdAC.ConfigureInput(&ci, AutocompleteSuggestionStyle)

	viewAC := NewAutocompleter(ViewCommandNames())
	viewAC.ConfigureInput(&vi, AutocompleteSuggestionStyle)

	m := Model{
		InputModes: InputModes{
			commandInput:        ci,
			commandAutocomplete: cmdAC,
			viewInput:           vi,
			viewAutocomplete:    viewAC,
		},
		DataState: DataState{
			allResources: make(map[string][]clusterstate.Resource),
			podYAMLData:  make(map[string]string),
		},
		CacheState: CacheState{cache: cache, ctx: context.Background()},
		Config:     Config{forestResourceType: "pcs", allNamespaces: true},
		viewState: clusterstate.ViewState{
			ViewType: clusterstate.ForestView,
		},
		activePane:  clusterstate.ResourcesPane,
		filterInput: ti,
		yamlOverlay: YAMLOverlayState{OverlayModel: OverlayModel{SearchInput: newSearchInput("/ ", 40, FilterBarStyle)}},
		logsOverlay: LogsOverlayState{OverlayModel: OverlayModel{SearchInput: newSearchInput("/ ", 40, FilterBarStyle)}},
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
func (t *TopologyState) topologyAvailable() bool {
	return t.topologyViewData != nil && len(t.topologyViewData.Domains) > 0
}

// topologyColumnVisible returns true when the TOPOLOGY column should be shown
// in the resources table. Requires both that the cache has synced (so we know
// cluster state) and that a ClusterTopology resource exists (i.e. topology is
// available). Before cache sync, we don't show or hide — the column is hidden
// by default since topologyAvailable() returns false when topologyViewData is nil.
func (t *TopologyState) topologyColumnVisible() bool {
	return t.topologyAvailable()
}

// rebuildContainersTable rebuilds the resources table for the ContainersView.
func (m *Model) rebuildContainersTable() {
	rebuildTable(tableRebuildConfig{
		table: &m.resourcesTable, specs: containerColumnSpecs, nameCol: 0, width: m.width,
	}, func() []table.Row {
		rows := make([]table.Row, 0, len(m.containerInfos))
		for _, c := range m.containerInfos {
			readyStr := "false"
			if c.Ready {
				readyStr = "true"
			}
			rows = append(rows, table.Row{
				c.Name, c.Image, c.State, readyStr, fmt.Sprintf("%d", c.RestartCount),
			})
		}
		return rows
	})
}

// rebuildResourcesTable rebuilds the resources table from current data with color-coded cells.
func (m *Model) rebuildResourcesTable() {
	if m.viewState.ViewType == clusterstate.ContainersView {
		m.rebuildContainersTable()
		return
	}

	viewKey := m.getCurrentViewKey()
	resources, exists := m.allResources[viewKey]
	if !exists {
		resources = []clusterstate.Resource{}
	}

	// Apply filter if active
	if m.filterText != "" {
		filter := strings.ToLower(m.filterText)
		var filtered []clusterstate.Resource
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
	if m.viewState.ViewType == clusterstate.PodCliqueView && len(resources) > 0 && resources[0].Type == clusterstate.ResourceTypePod {
		lastColHeader = "PHASE"
	}

	// Build dynamic column spec — must set columns BEFORE rows to avoid
	// column/row count mismatch panics (SetRows triggers UpdateViewport
	// which renders rows against the current column definitions).
	specs := m.buildResourceColumnSpecs(gpuTypes, lastColHeader)

	rebuildTable(tableRebuildConfig{
		table: &m.resourcesTable, specs: specs, nameCol: 2, width: m.width,
	}, func() []table.Row {
		rows := make([]table.Row, 0, len(resources))
		for _, r := range resources {
			rows = append(rows, table.Row(m.colorizeResourceRowWithGPU(r, gpuTypes)))
		}
		return rows
	})
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
func (m Model) getFilteredEvents() []clusterstate.Event {
	// In Pod view or Containers view, show events for that specific pod
	if m.viewState.ViewType == clusterstate.PodView || m.viewState.ViewType == clusterstate.ContainersView {
		filtered := []clusterstate.Event{}
		for _, event := range m.allEvents {
			if event.Parent == m.viewState.SelectedPod {
				filtered = append(filtered, event)
			}
		}
		return filtered
	}

	// In PodClique view, if a Pod is selected, show only that Pod's events
	if m.viewState.ViewType == clusterstate.PodCliqueView {
		selectedRow := m.resourcesTable.SelectedRow()
		if len(selectedRow) >= 3 && selectedRow[1] == clusterstate.ResourceTypePod {
			podName := selectedRow[2]
			filtered := []clusterstate.Event{}
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

	if m.topologyDrill.IsEmpty() {
		// Top-level: show domain rows (DOMAIN, KEY, VALUES)
		rebuildTable(tableRebuildConfig{
			table: &m.topologyDomainsTable, specs: topologyDomainColumnSpecs, nameCol: 0, width: m.width,
		}, func() []table.Row {
			rows := make([]table.Row, 0, len(m.topologyViewData.Domains))
			for _, d := range m.topologyViewData.Domains {
				valuesStr := "—"
				if d.ValuesCount >= 0 {
					valuesStr = fmt.Sprintf("%d", d.ValuesCount)
				}
				rows = append(rows, table.Row{d.Domain, d.Key, valuesStr})
			}
			return rows
		})
		return
	}

	// Drilled in: show distinct values for the current domain
	currentDomain, currentKey := m.currentTopologyDomain()
	if currentDomain == "" {
		m.topologyDomainsTable.SetRows([]table.Row{})
		return
	}

	// Get matching nodes based on breadcrumb constraints
	matchingNodes := m.topologyDrill.MatchingNodes(m.topologyViewData.NodeLabels)

	// Compute GPU summary for domain values
	gpuSummary := clusterstate.ComputeDomainGPUSummary(clusterstate.DomainGPUInput{
		DomainKey:       currentKey,
		MatchingNodes:   matchingNodes,
		NodeLabels:      m.topologyViewData.NodeLabels,
		NodeGPUProducts: m.topologyViewData.NodeGPUProducts,
		NodeGPUCapacity: m.topologyViewData.NodeGPUCapacity,
		Pods:            m.topologyViewData.RawPods,
	})

	// Compute pod counts per domain value
	podCounts := clusterstate.ComputeDomainPodCounts(
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

	// Get distinct values for the current domain key
	values := clusterstate.DistinctValuesForDomain(m.topologyViewData.NodeLabels, currentKey, matchingNodes)

	rebuildTable(tableRebuildConfig{
		table: &m.topologyDomainsTable, specs: specs, nameCol: 0, width: m.width,
	}, func() []table.Row {
		rows := make([]table.Row, 0, len(values))
		for _, v := range values {
			pc := podCounts[v]
			row := table.Row{v}
			valueCounts := gpuSummary.ByValue[v]
			for _, gpuType := range gpuSummary.GPUTypes {
				if valueCounts != nil {
					counts := valueCounts[gpuType]
					row = append(row, clusterstate.FormatGPUBar(counts.Grove, counts.Other, counts.Total, 20))
				} else {
					row = append(row, clusterstate.FormatGPUBar(0, 0, 0, 20))
				}
			}
			row = append(row,
				fmt.Sprintf("%d", pc.GPU),
				fmt.Sprintf("%d", pc.Total),
			)
			rows = append(rows, row)
		}
		return rows
	})

	_ = currentDomain // used for title rendering
}

// rebuildTopologyPodsTable rebuilds the bottom pane of the Topology view.
// It shows pods on nodes matching the current breadcrumb constraints.
func (m *Model) rebuildTopologyPodsTable() {
	if m.topologyViewData == nil {
		m.topologyPodsTable.SetRows([]table.Row{})
		return
	}

	// Get matching nodes based on breadcrumb
	matchingNodes := m.topologyDrill.MatchingNodes(m.topologyViewData.NodeLabels)

	// At top-level: don't show any pods until the user drills into a domain.
	if m.topologyDrill.IsEmpty() {
		m.topologyPodsTable.SetRows([]table.Row{})
		w := tableContentWidth(m.width, len(topologyPodColumnSpecs))
		m.topologyPodsTable.SetColumns(computeWeightedColumns(topologyPodColumnSpecs, w))
		return
	}

	// Drilled into a values list: highlighted row is a value — filter to nodes
	// where the current domain's label key matches the highlighted value.
	lastEntry, _ := m.topologyDrill.LastEntry()
	if lastEntry.Value == "" {
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

	// Filter pods by matching nodes
	filteredPods := clusterstate.FilterPodsByNodes(m.topologyViewData.Pods, matchingNodes)

	// Sort pods by name for stable, predictable display order
	sort.Slice(filteredPods, func(i, j int) bool {
		return filteredPods[i].Name < filteredPods[j].Name
	})

	rebuildTable(tableRebuildConfig{
		table: &m.topologyPodsTable, specs: topologyPodColumnSpecs, nameCol: 2, width: m.width,
	}, func() []table.Row {
		rows := make([]table.Row, 0, len(filteredPods))
		for _, p := range filteredPods {
			rows = append(rows, table.Row{p.Namespace, p.Node, p.Name, p.Topology, p.Phase})
		}
		return rows
	})
}

// currentTopologyDomain returns the domain name and label key for the current
// drill-down level. Delegates to TopologyDrillStack.CurrentDomain.
func (t *TopologyState) currentTopologyDomain() (string, string) {
	if t.topologyViewData == nil || len(t.topologyViewData.Domains) == 0 {
		return "", ""
	}
	return t.topologyDrill.CurrentDomain(t.topologyViewData.Domains)
}
