# Arborist File Index

Quick-reference table of every `.go` file across all Arborist packages. Test files (`_test.go`) are listed separately at the end.

## `cmd/arborist/` (1 file)

| File | Purpose |
|------|---------|
| `main.go` | Binary entry point — Kong parse, run subcommand, defer cleanup |

## `internal/cli/` (5 files)

| File | Purpose |
|------|---------|
| `root.go` | Top-level `CLI` struct, `-d` debug flag, `AfterApply()` / `Cleanup()` lifecycle |
| `flags.go` | Shared `NamespaceFlags` (`-n`, `-A`), namespace resolution helpers |
| `tui.go` | `ForestCmd` — default subcommand, wires K8s client → cache → TUI → Bubble Tea |
| `topology.go` | `TopologyCmd` — non-interactive topology tree with GPU bars to stdout |
| `diagnostics.go` | `DiagnosticsCmd` — diagnostic bundle collection to `.tgz` |

## `internal/tui/` (20 files)

| File | Purpose |
|------|---------|
| `model.go` | Central `Model` struct, embedded sub-structs, `NewModel()`, functional options, `Init()` |
| `update.go` | `Update()` dispatch, `handleWindowSize`, all key mode handlers, `toggleTopologyView` |
| `views.go` | `View()` rendering, all `renderXxxFrame()`, breadcrumb, section headers, truncation |
| `handlers.go` | Cache handlers (`applySnapshot`), hierarchy builders, pod/container/shell handlers |
| `view_behavior.go` | `ViewBehavior` interface, optional interfaces, registry map, `behavior()` dispatch |
| `view_behavior_hierarchy.go` | 8 hierarchy view implementations, `init()` registration, per-view nav/events/logs/shell |
| `view_behavior_topology.go` | `topologyView` implementation, `init()` registration |
| `navigation.go` | `navigateInto()`, all `navigateIntoXxx()` drill functions, `switchPane()` |
| `navigation_topology.go` | `topologyDrillInto()`, `topologyDrillBack()`, breadcrumb string, drill validation |
| `commands.go` | Command lists, `executeCommand()`, `switchToForestResourceType()`, all `tea.Cmd` factories |
| `messages.go` | All `tea.Msg` types (12 types: cache, YAML, logs, error, shell, containers) |
| `keys.go` | `KeyMap` struct, `DefaultKeyMap()` with all key bindings |
| `styles.go` | k9s-inspired color palette, ASCII logo, all lipgloss styles |
| `table_helpers.go` | `ColumnSpec`, weighted column layout, cursor save/restore, table rebuild helpers |
| `gpu_columns.go` | Dynamic GPU column specs, GPU row colorization, GPU counts lookup |
| `overlay.go` | `OverlayModel` base — viewport, search, content transforms |
| `overlay_yaml.go` | YAML overlay — open, fetch, key handling, resource info resolution |
| `overlay_logs.go` | Logs overlay — open, fetch, auto-scroll, wrap, horizontal scroll, carriage returns |
| `autocomplete.go` | `Autocompleter` — inline ghost-text completions for command/view inputs |
| `debug.go` | TUI-specific debug logging with view/pane context prefix |

## `internal/k8s/` (9 files)

| File | Purpose |
|------|---------|
| `client.go` | `K8sClient` — REST config, clientset, dynamic client, CRD check, version |
| `global_cache.go` | `InformerGlobalCache` — composes fetcher + informers + snapshot store |
| `informer_set.go` | `informerSet` — 5 factories, 7 informers, setup, start, sync, all `readXxx()` methods |
| `informer_conversion.go` | Generic `toTyped[T]`, informer-to-typed readers, GPU extraction from nodes/pods |
| `snapshot_builder.go` | `rebuildSnapshot()` (I/O) + `buildSnapshot()` (pure), hierarchy + scheduling computation |
| `snapshot_store.go` | `snapshotStore` — 500ms debouncer, `Snapshot()`, `Updates()`, `storeAndNotify()` |
| `api_calls.go` | `APIResourceFetcher` — on-demand YAML, containers, logs via K8s API |
| `kubeconfig.go` | `KubeConfigInfo` — context, cluster, user, namespace from kubeconfig |
| `topology_fetcher.go` | `TopologyCLIData` — 3 targeted API calls for the `topology` CLI subcommand |

## `internal/clusterstate/` (9 files)

| File | Purpose |
|------|---------|
| `types.go` | `Pane`, `ViewType`, `ViewState` enums; `Resource`, `Event`, `CachedPodInfo`, topology display types |
| `cache.go` | `GlobalCache` interface (composite), `CacheSnapshot`, `HierarchyData` structs |
| `resource_types.go` | Resource type string constants, Grove label key constants, `CompositeKey()` |
| `gpu.go` | `GPUSummary`, `GPUCounts`, all GPU aggregation/rendering (`BuildGPUSummary`, `FormatGPUBar`) |
| `topology.go` | `TopologyInfo`, topology resolution (explicit → PCSG → PCS fallback chain) |
| `topology_view.go` | `BuildTopologyViewData()`, node filtering, domain value counts, pod filtering |
| `topology_drill.go` | `TopologyDrillStack` — push/select/back/validate/matching-nodes state machine |
| `event_aggregator.go` | Hierarchy-scoped event gathering (`GetEventsForPCS` through `GetEventsForPodClique`) |
| `mock_global_cache.go` | `MockGlobalCache` test double implementing `GlobalCache` |

## `internal/diagnostics/` (7 files)

| File | Purpose |
|------|---------|
| `types.go` | `DiagnosticContext`, `DiagnosticOutput` interface, `GroveResourceType`, constants |
| `collector.go` | `Collector` orchestrator — registers 4 collectors, runs with panic recovery |
| `resources.go` | `CollectGroveResources` — YAML dump of all 4 Grove CRD types |
| `events.go` | `CollectEvents` — K8s events from last 10 minutes, tabulated |
| `pods.go` | `CollectPodDetails` — pod summary table + unhealthy pod container details |
| `operator_logs.go` | `CollectOperatorLogs` — last 2000 lines from operator pod containers |
| `file_output.go` | `FileOutput` — `summary.txt` + YAML files + `.tgz` bundling |

## `internal/debug/` (1 file)

| File | Purpose |
|------|---------|
| `debug.go` | Package-level file logger — `Init()`, `Close()`, `Log()`, `Enabled()` |

## Test Files

| Package | Files |
|---------|-------|
| `cli/` | `tui_test.go`, `topology_test.go` |
| `tui/` | `model_test.go`, `model_internals_test.go`, `rendering_test.go`, `cache_update_test.go`, `cursor_stability_test.go`, `error_ux_test.go`, `phase3_logic_gaps_test.go`, `autocomplete_test.go` |
| `k8s/` | `client_test.go`, `global_cache_test.go`, `kubeconfig_test.go` |
| `clusterstate/` | `cache_test.go`, `topology_test.go`, `topology_view_test.go`, `topology_drill_test.go`, `gpu_test.go`, `event_aggregator_test.go` |
| `diagnostics/` | `collector_test.go`, `events_test.go`, `resources_test.go`, `pods_test.go`, `operator_logs_test.go`, `file_output_test.go` |
