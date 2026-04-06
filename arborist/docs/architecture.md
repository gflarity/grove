# Arborist Architecture

Arborist is a TUI/CLI tool for inspecting Grove clusters. It is built with Charm's [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework following the Elm architecture pattern (Model → Update → View).

## Package Dependency Graph

```
cmd/arborist
    └── cli
         ├── tui  ──────────┐
         │    └── clusterstate
         ├── k8s  ──────────┤
         │    ├── clusterstate
         │    └── operator/api
         ├── clusterstate ──┘
         ├── diagnostics
         └── debug
```

- `cmd/arborist` — binary entry point, delegates to `cli`
- `cli` — wires together all other packages per subcommand
- `tui` — Bubble Tea model, knows `clusterstate` types but not `k8s` implementation
- `k8s` — implements `clusterstate.GlobalCache` interface using client-go informers
- `clusterstate` — pure types and logic, no I/O dependencies
- `diagnostics` — standalone; uses only `k8s.io/client-go` directly
- `debug` — standalone singleton logger, imported by `cli` and `tui`

The key architectural boundary: `tui` depends on `clusterstate` interfaces, not on `k8s` directly. The `cli` package bridges them at startup.

## Package Responsibilities

### `cmd/arborist/`

Single `main.go`. Creates a `cli.CLI{}`, calls `kong.Parse()`, defers `Cleanup()`, runs the selected subcommand.

### `internal/cli/`

Kong-based CLI with three subcommands:

- **`tui` (default)** — `ForestCmd`: resolves namespace, creates K8s client, checks CRDs, creates `InformerGlobalCache`, builds `tui.Model` with functional options, runs `tea.NewProgram` with alt-screen.
- **`topology`** — `TopologyCmd`: makes 3 targeted API calls (not informers), groups pods by topology domain, renders a tree to stdout with GPU bar charts.
- **`diagnostics`** — `DiagnosticsCmd`: creates a `DiagnosticContext`, calls `CollectAndBundle()`, writes a `.tgz` bundle.

Shared `NamespaceFlags` struct (`-n`, `-A`) is embedded by all subcommands. The TUI subcommand defaults to all-namespaces when neither flag is given.

### `internal/tui/`

The largest package (20 production files). Implements the Bubble Tea `tea.Model` interface with a single `Model` struct. Key design decisions:

- **Embedded sub-structs** for field grouping: `ErrorState`, `ClusterInfo`, `LayoutState`, `InputModes`, `TopologyState`, `DataState`, `CacheState`, `Config`. All fields are promoted (accessible as `m.width`, not `m.LayoutState.width`).
- **`ViewBehavior` interface** dispatches per-view-type logic (rendering, key handling, event rebuilding). Implementations registered at `init()` time in a package-level map.
- **Optional interfaces via type assertions**: `BackNavigator`, `LogsExecutor`, `ShellExecutor` — checked with `if nav, ok := behavior.(BackNavigator); ok { ... }`.
- **Async message passing**: all I/O happens in `tea.Cmd` goroutines that return typed `tea.Msg` values. The `Update()` loop is synchronous.
- **No direct K8s access**: the TUI communicates with the cluster through `clusterstate.GlobalCache` (an interface), receiving immutable `*CacheSnapshot` values.

### `internal/k8s/`

Implements `clusterstate.GlobalCache` via `InformerGlobalCache`, which composes three embedded structs:

- **`*APIResourceFetcher`** (pointer) — on-demand API calls for YAML, logs, containers. Satisfies `clusterstate.ResourceFetcher`.
- **`informerSet`** (value) — all client-go informer factories and 7 informers (Node, Pod, Event, PCS, PCSG, PodClique, ClusterTopology). Only creates informers for CRDs that actually exist (checked via Discovery API).
- **`snapshotStore`** (value) — holds the current `*CacheSnapshot` behind an `RWMutex`, plus a debouncer and notification channel. Satisfies `clusterstate.CacheReader`.

Also contains `TopologyCLIData` and `FetchTopologyCLIData()` for the non-TUI topology command (3 targeted API calls instead of 7 informers).

### `internal/clusterstate/`

Pure types and logic — no I/O, no K8s client imports (only operator API types for CRD structs). Contains:

- **Interfaces**: `GlobalCache` (composite of `CacheLifecycle`, `CacheReader`, `ResourceFetcher`), `WarningConfigurable`
- **Display types**: `Resource`, `Event`, `CachedPodInfo`, `ContainerInfo`, `ViewType`, `ViewState`, `Pane`
- **Snapshot types**: `CacheSnapshot`, `HierarchyData` (all hierarchy maps), `TopologyViewData`
- **GPU logic**: `GPUSummary`, `GPUCounts`, `DomainGPUSummary`, `BuildGPUSummary()`, `ComputeDomainGPUSummary()`, `FormatGPUBar()`
- **Topology logic**: `TopologyInfo`, `TopologyDrillStack`, `BuildTopologyViewData()`, `FilterNodesByBreadcrumb()`
- **Event aggregation**: hierarchy-scoped event gathering (e.g., `GetEventsForPCS()` gathers events for all descendants)
- **Resource type constants and label keys**: `ResourceTypePodCliqueSet`, `LabelPartOf`, `CompositeKey()`, etc.
- **`MockGlobalCache`**: test double implementing `GlobalCache`

### `internal/diagnostics/`

Collector pattern with a uniform `CollectorFunc` signature: `func(ctx, *DiagnosticContext, DiagnosticOutput) error`.

Four registered collectors run in sequence with panic recovery:
1. **Operator Logs** — last 2000 lines from operator pods
2. **Grove Resources** — YAML dumps of all 4 CRD types via dynamic client
3. **Pod Details** — pod summary table + unhealthy pod detail
4. **Kubernetes Events** — events from last 10 minutes

`DiagnosticOutput` is an interface (allows different output backends). `FileOutput` writes `summary.txt` + per-resource YAML files, then bundles everything into a timestamped `.tgz`.

### `internal/debug/`

Package-level file-based debug logger (`Init`, `Close`, `Log`, `Enabled`). Protected by a `sync.Mutex`. Necessary because the Bubble Tea alt-screen TUI owns stdout/stderr, so debug output must go to a separate file. Activated by `-d <path>` flag.

## Data Flow

### Startup Sequence (TUI)

```
1. main()           → kong.Parse(&cli.CLI{})
2. CLI.AfterApply() → debug.Init(path) if -d flag
3. ForestCmd.Run()  → resolve namespace
4.                  → k8s.NewK8sClient() (REST config + clientset + dynamic client)
5.                  → client.CheckGroveCRDs() (abort early if core CRDs missing; optional CRDs degrade gracefully)
6.                  → client.NewGlobalCache(namespace, onWarning)
7.                  → tui.NewModel(cache, ...options)
8.                  → tea.NewProgram(model, tea.WithAltScreen())
9. Model.Init()     → returns nil (cache start deferred to first WindowSizeMsg)
10. WindowSizeMsg   → startGlobalCacheCmd() → cache.Start() + WaitForSync()
                    → CacheSyncedMsg → applySnapshot() → first render
```

### Cache → TUI Pipeline

```
K8s API server
  ↓ (watch streams)
informer Add/Update/Delete event
  ↓
onChange()
  ↓
debouncer.schedule(rebuildSnapshot)  ← resets 500ms timer
  ↓ [500ms quiet period]
rebuildSnapshot()
  ↓ reads all informer stores (no API calls)
  ↓ calls buildSnapshot() (pure function — sorts, computes hierarchy, GPU summary)
  ↓
storeAndNotify(snapshot)
  ↓ atomic pointer swap under RWMutex
  ↓ non-blocking send on updatesCh
  ↓
waitForCacheUpdateCmd() (blocking select on updatesCh)
  ↓
CacheUpdateMsg
  ↓
handleCacheUpdate() → applySnapshot()
  ↓ saveCursorState → rebuild tables → restoreCursorState
  ↓
View() re-renders
```

### CacheSnapshot Structure

```
CacheSnapshot
├── HierarchyData (embedded)
│   ├── PodCliqueSets           []Resource
│   ├── PodCliqueSetSpecs       map[pcsName]*PodCliqueSet
│   ├── ReplicaIndexesByPCS     map[pcsName][]string
│   ├── ScalingGroupsByReplica  map["pcsName/idx"][]Resource
│   ├── PodCliquesByReplica     map["pcsName/idx"][]Resource
│   ├── ReplicaIndexesByPCSG    map[pcsgName][]string
│   ├── PodCliquesByPCSG        map[pcsgName][]Resource
│   ├── PodCliquesByPCSGReplica map["pcsgName/idx"][]Resource
│   ├── PodsByPodClique         map[pcName][]Resource
│   └── PodInfos               map[podName]CachedPodInfo
├── EventsByObject              map["Kind/name"][]Event
└── TopologyViewData
    ├── Domains         []TopologyDomainRow
    ├── NodeLabels      map[nodeName]map[labelKey]string
    ├── Pods            []TopologyViewPod
    ├── DomainToKey     map["rack"]"topology.io/rack"
    ├── GPUSummary      *GPUSummary
    ├── NodeGPUProducts map[nodeName]"H200"
    ├── NodeGPUCapacity map[nodeName]int64
    └── RawPods         []TopologyPodInput
```

All composite map keys use `"name/index"` format via `CompositeKey()` / `SplitCompositeKey()`.

### Bottom-Up Scheduling Computation

Instead of trusting CRD `status.ScheduledReplicas`, the snapshot builder recomputes scheduling status bottom-up from pod `NodeName` assignments:

- **Pod** is scheduled if `pod.Spec.NodeName != ""`
- **PodClique** is scheduled if `scheduledPods >= minAvailable`
- **PCSG replica** is scheduled if all its PodCliques are scheduled
- **PCS replica** is scheduled if all standalone PCs and all PCSGs within it are fully scheduled

## Key Patterns

### ViewBehavior Dispatch

```go
type ViewBehavior interface {
    ViewKey(vs ViewState) string
    PaneList() []Pane
    RebuildEvents(m *Model, snap *CacheSnapshot)
    RenderPanes(m *Model, resourcesH, eventsH int) []string
    HandleKey(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd, bool)
}
```

9 ViewType constants map to ViewBehavior implementations registered at `init()` time. The `Model.behavior()` method dispatches. Optional capabilities (back navigation, logs, shell) are checked via type assertions on optional interfaces.

### Async Message Passing

Every I/O operation (YAML fetch, log fetch, container list, cache sync) runs in a `tea.Cmd` goroutine and returns a typed `tea.Msg`. The `Update()` loop handles messages synchronously — no shared mutable state, no locks in the TUI code.

Cache subscription uses a long-polling chain: `waitForCacheUpdateCmd` blocks on `cache.Updates()` channel, returns `CacheUpdateMsg`, which re-subscribes — event-driven, never polling.

### Cursor Stability Across Data Rebuilds

`saveCursorState()` captures the name of the currently selected row. After rebuilding the table with new data, `restoreCursorState()` searches for that name in the new rows and restores the cursor position. Falls back to clamping the previous numeric index if the name is gone.

### Weighted Column Layout

`ColumnSpec{Title, Weight}` defines proportional column widths. `computeWeightedColumns()` divides available terminal width proportionally by weight, with the last column absorbing rounding remainder. GPU columns are added dynamically based on discovered GPU types.

### GPU Data Pre-Computed at Snapshot Time

`BuildGPUSummary()` runs during `buildSnapshot()`, aggregating GPU counts at every hierarchy level (pod, PodClique, PCSG, PCSG replica, PCS replica, PCS). This avoids re-scanning pods on every render. Domain-level GPU summaries (`ComputeDomainGPUSummary()`) are computed on demand during topology drill-down.

### Debounced Cache Rebuilds

A `debouncer` with 500ms interval coalesces rapid informer events (e.g., during rolling updates). Only after 500ms of silence does `rebuildSnapshot()` fire. The `snapshotStore` uses an `RWMutex` for the snapshot pointer and a buffered channel (size 1) for notifications.

### Auto-Skip Single-Replica Levels

When navigating into a PCS or PCSG with exactly 1 replica, the TUI automatically skips the intermediate replica-list view and drills directly into the single replica's children. Back navigation mirrors this behavior.
