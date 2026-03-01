# Arborist Feature Catalog

This document maps every user-facing feature to the files that implement it. Use it to quickly locate the code behind any behavior.

## Forest View

The default view showing Grove resources in a hierarchical drill-down table.

| Aspect | Files |
|--------|-------|
| 6-level hierarchy (PCS → replica → PCSG → replica → PodClique → Pod) | `tui/handlers.go` (`buildAllResources`, `buildPCSReplicaResources`, `buildReplicaChildResources`, `buildPCSGReplicaChildResources`, `buildPodChildResources`) |
| Auto-skip single-replica levels | `tui/navigation.go` (`navigateIntoPCS`, `navigateIntoPCSG` — skip when `len(replicaIndexes) == 1`) |
| Resource type switching (pcs/pc/pcsg/pod) | `tui/commands.go` (`switchToForestResourceType`), `tui/handlers.go` (`buildForestResources`, `buildFlatDrillInResources`) |
| Drill-in / back navigation | `tui/navigation.go` (`navigateInto`, `navigateActions` map), `tui/view_behavior_hierarchy.go` (per-view `NavigateBack`) |
| Filter (/ key, live filtering) | `tui/update.go` (`handleFilterModeKey`), `tui/handlers.go` (`rebuildHierarchyFromSnapshot` applies filter) |
| Resource table rendering | `tui/views.go` (`renderResourcesFrame`), `tui/table_helpers.go` (`rebuildTable`, `computeWeightedColumns`) |
| Events pane | `tui/views.go` (`renderEventsFrame`), `tui/view_behavior_hierarchy.go` (per-view `RebuildEvents`), `clusterstate/event_aggregator.go` |

## Topology View

Drill-down view of cluster topology (e.g., region → zone → rack) with GPU usage bars.

| Aspect | Files |
|--------|-------|
| Domain list table | `tui/views.go` (`renderTopologyDomainsFrame`), `tui/model.go` (`rebuildTopologyDomainsTable`) |
| Pod list table (filtered by drill state) | `tui/views.go` (`renderTopologyPodsFrame`), `tui/model.go` (`rebuildTopologyPodsTable`) |
| Drill stack (breadcrumb navigation) | `clusterstate/topology_drill.go` (`TopologyDrillStack`, `PushDomain`, `SelectValueAndAdvance`, `DrillBack`) |
| Drill-in / back | `tui/navigation_topology.go` (`topologyDrillInto`, `topologyDrillBack`) |
| Breadcrumb rendering | `tui/navigation_topology.go` (`topologyBreadcrumbString`), `tui/views.go` (`renderBreadcrumb`) |
| GPU bar columns in drilled-in view | `tui/gpu_columns.go` (`buildResourceColumnSpecs` — topology drill branch), `clusterstate/gpu.go` (`ComputeDomainGPUSummary`, `FormatGPUBar`) |
| GPU footnote | `tui/views.go` (footnote rendered when `topologyDrill.Depth() > 0`) |
| Toggle between forest/topology | `tui/update.go` (`toggleTopologyView`) |
| View behavior | `tui/view_behavior_topology.go` (`topologyView`) |
| Topology data building | `clusterstate/topology_view.go` (`BuildTopologyViewData`), `clusterstate/topology.go` (`BuildTopologyInfo`, resolution functions) |
| Node filtering by breadcrumb | `clusterstate/topology_view.go` (`FilterNodesByBreadcrumb`) |

## Full-Screen Overlays

### YAML Viewer

| Aspect | Files |
|--------|-------|
| Open overlay (y key) | `tui/overlay_yaml.go` (`openYAMLOverlay`) |
| YAML fetch (async) | `tui/commands.go` (`loadResourceYAMLCmd`, `loadPodYAMLCmd`), `k8s/api_calls.go` (`GetResourceYAML`, `GetPodYAML`) |
| Handle response | `tui/overlay_yaml.go` (`handleResourceYAML`) |
| Search (/ in overlay) | `tui/overlay.go` (`HandleSearchKey`, `ApplySearch`, `SearchNext`, `RenderSearchFrame`) |
| Scroll (up/down/pgup/pgdn/home/end/ctrl-d/ctrl-u) | `tui/overlay.go` (`HandleKey`) |
| Close (esc/q) | `tui/overlay.go` (`Close`) |
| Virtual replica → actual resource mapping | `tui/overlay_yaml.go` (`selectedResourceInfo`) |

### Logs Viewer

| Aspect | Files |
|--------|-------|
| Open overlay (l key) | `tui/overlay_logs.go` (`handleLogsExec`, `openLogsOverlay`) |
| Container resolution (auto-pick first running) | `tui/commands.go` (`fetchFirstContainerForLogsCmd`, `fetchFirstRunningContainerCmd`) |
| Log fetch (async) | `tui/commands.go` (`loadPodLogsCmd`), `k8s/api_calls.go` (`GetPodLogs`) |
| Handle response | `tui/overlay_logs.go` (`handleLogsContent`) |
| Word wrap toggle (w key) | `tui/overlay_logs.go` (`handleLogsOverlayKey`, `wrapText`) |
| Horizontal scroll (left/right) | `tui/overlay_logs.go` (`handleLogsOverlayKey`, `horizontalSlice`) |
| Auto-scroll toggle (s key, 2s poll) | `tui/overlay_logs.go` (`handleLogsAutoScrollTick`), `tui/commands.go` (`logsAutoScrollTickCmd`) |
| Carriage return handling | `tui/overlay_logs.go` (`resolveCarriageReturns`) |
| Search | `tui/overlay.go` (shared with YAML) |

## Input Modes

| Mode | Activation | Deactivation | Files |
|------|-----------|--------------|-------|
| Normal | default | entering another mode | `tui/update.go` (`handleNormalModeKey`) |
| Filter (`/`) | `/` key | `Esc` (clear) or `Enter` (apply) | `tui/update.go` (`handleFilterModeKey`) |
| Command (`:`) | `:` key | `Esc` (cancel) or `Enter` (execute) | `tui/update.go` (`handleCommandModeKey`) |
| View (`v`) | `v` key | `Esc` (cancel) or `Enter` (execute) | `tui/update.go` (`handleViewEditKey`) |

Command and View modes share execution logic (`executeCommand`) but accept different command lists. Both have inline autocomplete via `Autocompleter`.

## GPU Visualization

| Aspect | Files |
|--------|-------|
| GPU summary pre-computation | `clusterstate/gpu.go` (`BuildGPUSummary`) |
| Dynamic GPU columns in resource table | `tui/gpu_columns.go` (`buildResourceColumnSpecs`) |
| Three-way accounting (Grove/Other/Free) | `clusterstate/gpu.go` (`ComputeDomainGPUSummary`, `DomainGPUCounts`) |
| Bar graph rendering | `clusterstate/gpu.go` (`FormatGPUBar` — `[▓▓░   ] (grove/other/total)`) |
| GPU colorization in rows | `tui/gpu_columns.go` (`colorizeResourceRowWithGPU`) |
| Cluster GPU header suffix | `clusterstate/gpu.go` (`FormatClusterGPUHeaderSuffix`, `FormatScopedGPUHeaderSuffix`) |
| GPU product short name parsing | `clusterstate/gpu.go` (`ParseGPUProductShortName`) |
| Node GPU extraction | `k8s/informer_conversion.go` (`GPUCapacityFromNode`, `GPUProductFromNode`, `GPURequestsFromPod`) |

## Topology CLI

Non-interactive tree output for topology inspection.

| Aspect | Files |
|--------|-------|
| Command entry | `cli/topology.go` (`TopologyCmd.Run`, `runTopology`) |
| Targeted API fetch (3 calls, not informers) | `k8s/topology_fetcher.go` (`FetchTopologyCLIData`) |
| Pod grouping by topology domain | `cli/topology.go` (`groupPodsByTopology`) |
| PCS filter | `cli/topology.go` (`isPCSMatch`, `filterDisplayPods`) |
| Three-way GPU computation | `cli/topology.go` (`computeThreeWayGPUUsage`), `clusterstate/gpu.go` (`ComputeDomainGPUSummary`) |
| Tree rendering with box-drawing | `cli/topology.go` (`printTopologyTree`, `printPodListWithGPU`, `printGPUMiniTable`) |

## Diagnostics

| Aspect | Files |
|--------|-------|
| Command entry | `cli/diagnostics.go` (`DiagnosticsCmd.Run`) |
| Collector orchestration | `diagnostics/collector.go` (`NewCollector`, `CollectAll`, `safeRunCollector`) |
| Operator logs (last 2000 lines) | `diagnostics/operator_logs.go` (`CollectOperatorLogs`) |
| Grove CRD YAML dumps | `diagnostics/resources.go` (`CollectGroveResources`) |
| Pod summary + unhealthy detail | `diagnostics/pods.go` (`CollectPodDetails`) |
| Recent events (10 min) | `diagnostics/events.go` (`CollectEvents`) |
| File output + .tgz bundling | `diagnostics/file_output.go` (`FileOutput`, `CollectAndBundle`, `createTGZ`) |
| Output abstraction | `diagnostics/types.go` (`DiagnosticOutput` interface) |

## Error UX

| Aspect | Files |
|--------|-------|
| Error log box (toggle with `!`) | `tui/views.go` (`renderErrorLogFrame`), `tui/model.go` (`ErrorState`, `addError`) |
| Error auto-show (visible on first error) | `tui/model.go` (`addError` sets `errorLogVisible = true`) |
| Max 3 entries, newest first | `tui/model.go` (`addError` caps at `maxErrorLogEntries`) |
| Connection errors (pre-seeded) | `cli/tui.go` (`WithConnectionError`), `tui/model.go` |
| CRD pre-flight check | `k8s/client.go` (`CheckGroveCRDs`) |
| Async error messages | `tui/messages.go` (`ErrorMsg`, `WarningMsg`), `tui/update.go` (handler) |
| Error log styling | `tui/styles.go` (`ErrorLogTimestampStyle`, `ErrorLogMessageStyle`, orange border) |

## Header

| Aspect | Files |
|--------|-------|
| k9s-style 3-column layout | `tui/views.go` (`renderHeaderFrame`) |
| Left: context/cluster/user/version/namespace/view | `tui/views.go` (`renderHeaderFrame`) |
| Middle: context-sensitive key menu | `tui/views.go` (`buildMenuItems`) |
| Right: ASCII art logo | `tui/styles.go` (`ArboristASCII`, `LogoStyle`) |
| Responsive hiding (< 60 / 60-90 / 90+) | `tui/views.go` (`renderHeaderFrame` width branches) |
| View edit inline in header | `tui/views.go` (`renderHeaderFrame` — `viewEditActive` branch) |

## Key Bindings

### Global (Normal Mode)

| Key | Action | Guard |
|-----|--------|-------|
| `q` / `Q` | Quit | |
| `Ctrl+C` | Force quit | Always (checked before all modes) |
| `t` / `T` | Toggle forest ↔ topology | Topology data available |
| `/` | Enter filter mode | |
| `:` | Enter command mode | |
| `v` / `V` | Enter view edit mode | |
| `!` | Toggle error log | |
| `y` / `Y` | Open YAML overlay | |
| `l` / `L` | Open logs overlay | Views with `LogsExecutor` |
| `s` / `S` | Shell (kubectl exec) | Views with `ShellExecutor` |
| `Tab` | Switch pane | |

### Hierarchy Views

| Key | Action |
|-----|--------|
| `Enter` | Drill into selected resource |
| `Esc` | Navigate back |
| `Up` / `Down` | Move cursor |

### Topology View

| Key | Action |
|-----|--------|
| `Enter` | Drill into domain / select value |
| `Esc` | Drill back (or return to forest if at top) |
| `Up` / `Down` | Move cursor (domains pane also updates pods) |

### Overlay (YAML / Logs)

| Key | Action |
|-----|--------|
| `Esc` / `q` | Close overlay |
| `Up` / `Down` / `PgUp` / `PgDn` / `Home` / `End` | Scroll |
| `Ctrl+D` / `Ctrl+U` | Half-page scroll |
| `/` | Open search |
| `n` / `N` | Next / previous search match |

### Logs Overlay (Additional)

| Key | Action |
|-----|--------|
| `w` / `W` | Toggle word wrap |
| `s` / `S` | Toggle auto-scroll (2s poll) |
| `Left` / `Right` | Horizontal pan (disabled when wrap on) |

### Filter Mode

| Key | Action |
|-----|--------|
| `Esc` | Cancel filter, clear text |
| `Enter` | Commit filter |
| `Tab` | Switch pane (filter stays active) |
| Other | Update filter (live filtering) |

### Command / View Edit Mode

| Key | Action |
|-----|--------|
| `Esc` | Cancel |
| `Enter` | Execute command |
| Other | Update text (with autocomplete) |
