# GREP-373: Arborist (aka kubectl-grove) — CLI for Grove

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
    - [Story 1: Debugging a Stuck PodCliqueSet Rollout](#story-1-debugging-a-stuck-podcliqueset-rollout)
    - [Story 2: Visualizing GPU Placement Across Topology in the TUI](#story-2-visualizing-gpu-placement-across-topology-in-the-tui)
    - [Story 3: Understanding GPU Utilization Across Topology](#story-3-understanding-gpu-utilization-across-topology)
    - [Story 4: Collecting Diagnostics for a Support Case](#story-4-collecting-diagnostics-for-a-support-case)
  - [Limitations/Risks &amp; Mitigations](#limitationsrisks--mitigations)
- [Design Details](#design-details)
  - [Architecture](#architecture)
  - [Package Structure](#package-structure)
  - [Data Flow](#data-flow)
  - [Subcommands](#subcommands)
    - [tui — Interactive Terminal UI](#tui--interactive-terminal-ui)
    - [topology — Non-Interactive Topology Tree](#topology--non-interactive-topology-tree)
    - [diagnostics — Diagnostic Bundle Collector](#diagnostics--diagnostic-bundle-collector)
  - [TUI Views](#tui-views)
    - [Forest View](#forest-view)
    - [Topology View](#topology-view)
  - [Full-Screen Overlays](#full-screen-overlays)
  - [GPU Visualization](#gpu-visualization)
  - [Error UX](#error-ux)
  - [kubectl Plugin Integration](#kubectl-plugin-integration)
  - [k9s Plugin](#k9s-plugin)
  - [Monitoring](#monitoring)
  - [Dependencies](#dependencies)
  - [Test Plan](#test-plan)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

Arborist will be the CLI for Grove. It will provide an interactive Terminal User Interface (TUI), non-interactive CLI commands, and a diagnostic bundle collector — giving operators and developers immediate visibility into Grove-managed AI inference workloads without requiring deep knowledge of the underlying CRD hierarchy. The binary will be symlinked as `kubectl-grove`, making it immediately usable as a kubectl plugin (`kubectl grove`). Its source will live in the Grove monorepo under `tools/arborist/`, with planned distribution through [Krew](https://krew.sigs.k8s.io/).

This proposal covers three subcommands (`tui`, `topology`, `diagnostics`). Additional commands (e.g., `status`, `health`, lifecycle operations) are planned as follow-up work in a separate proposal.

## Motivation

Grove introduces a multi-level CRD hierarchy (PodCliqueSet, PodCliqueScalingGroup, PodClique, PodGang) that, while powerful, can be difficult to navigate with `kubectl` alone. Operators managing GPU clusters running AI inference workloads need to quickly answer questions like:

- Why is part of my workload not scheduling? What errors are occurring, on which resources, and what is the root cause?
- Which pods belong to which PodCliqueSet, and what is their status?
- Where in the CRD hierarchy is the problem — at the PodCliqueSet level, a specific PodClique, or an individual pod?
- How are GPUs allocated across the cluster topology?
- Which workloads are Grove-managed vs. non-Grove, and how does that affect available capacity?

Today, answering these questions requires multiple `kubectl` commands, manual cross-referencing of owner references, and mental reconstruction of the resource hierarchy. The most advanced and helpful general-purpose Kubernetes tools like k9s are completely agnostic to Grove's CRD hierarchy and cannot provide a unified view. This is error-prone and slow, especially during incident response.

### Goals

- Provide a real-time, interactive TUI that renders the full Grove CRD hierarchy as a navigable tree.
- Visualize cluster topology with GPU utilization broken down by Grove-managed, non-Grove, and free capacity.
- Enable drill-in navigation from high-level PodCliqueSets down to individual pods, with contextual Kubernetes events, YAML, and log inspection — eliminating the need to manually trace owner references across multiple `kubectl` calls and debug more effeciently
- Provide a non-interactive CLI mode for topology inspection, suitable for CI or users who prefer not to use the TUI.
- Provide a diagnostics bundle collector that gathers operator logs, CRD state, events, and pod summaries into a single `.tgz` archive for support cases.
- Deliver a single static binary with no external dependencies beyond a valid kubeconfig.
- Integrate with kubectl as `kubectl grove` via the kubectl plugin mechanism.
- Provide a [k9s plugin](https://k9scli.io/topics/plugins/) that allows k9s users to launch Arborist views directly from k9s when navigating Grove resources.

### Non-Goals

- **General-purpose Kubernetes dashboard.** Arborist will be purpose-built for Grove resources, not a replacement for k9s or Lens.
- **Mutating operations.** Arborist will be strictly read-only in this proposal. Lifecycle commands (`rollout`, `scale`, `restart`, etc.) are planned for a future proposal.
- **Historical or time-series data.** Arborist will show a live, point-in-time view.
- **Status and health commands.** `kubectl grove status` and `kubectl grove health` are planned for a future proposal.
- **Metrics.** Live metrics from pod endpoints (`kubectl grove metrics`) is planned for a future proposal.
- **Scheduling or placement decisions.** Arborist will only visualize the current state.

## Proposal

Arborist will be delivered as a standalone Go binary with a `kubectl-grove` symlink that connects to a Kubernetes cluster via kubeconfig and provides three subcommands:

| Subcommand | Mode | Description |
|------------|------|-------------|
| `kubectl grove` | Interactive TUI (default) | Full-featured terminal UI with forest and topology views |
| `kubectl grove topology` | Non-interactive CLI | Prints topology tree with GPU utilization to stdout |
| `kubectl grove diagnostics` | Non-interactive CLI | Collects and bundles cluster diagnostic data |

The TUI will be the default subcommand — running `kubectl grove` with no arguments will launch it directly.

The TUI will be built on the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework (Elm architecture) and will use informer-based watches for real-time updates. All cluster reads will go through a shared informer cache, minimizing API server load.

### User Stories

#### Story 1: Debugging a Stuck PodCliqueSet Rollout

An operator notices that a PodCliqueSet rollout is not completing. They launch `kubectl grove` and navigate the forest view to the PodCliqueSet in question. As they drill into its PodCliqueScalingGroups and PodCliques, the events pane at the bottom of the screen updates to show Kubernetes events scoped to each resource — immediately surfacing why certain pods are not being scheduled (e.g., insufficient GPU resources, scheduling gates, topology constraints). They can inspect any resource's YAML (`y`), tail pod logs (`l`), or continue drilling deeper — all while the contextual events pane keeps them informed at every level of the hierarchy. The combination of hierarchical navigation, contextual events, YAML inspection, and log tailing gives operators a complete debugging picture without leaving the TUI. Future work will enrich the events pane with additional Grove-specific events to make this workflow even more powerful.

#### Story 2: Visualizing GPU Placement Across Topology in the TUI

An operator wants to understand how their grove workloads are physically distributed across the cluster. From the forest view, they press `t` to switch to the topology view within the TUI. The display reorganizes from the CRD hierarchy into a topology-domain tree (block → rack → host), with three-way GPU bar graphs (Grove / Other / Free) at each level. They can drill into a specific rack to see which nodes and pods are placed there, and toggle back to the forest view to continue navigating by PodCliqueSet. The live-updating topology view provides real-time visibility into GPU fragmentation and placement without leaving the interactive session.

#### Story 3: Understanding GPU Utilization Across Topology

A cluster administrator needs to understand GPU allocation before deploying a new large inference workload. They run `kubectl grove topology` from their terminal and see GPU utilization broken down by topology domain (block, rack, host). The three-way bar graph (Grove / Other / Free) at each level immediately shows where free GPU capacity exists and whether non-Grove workloads are consuming resources unexpectedly. The non-interactive output is suitable for piping to other tools or including in runbooks.

#### Story 4: Collecting Diagnostics for a Support Case

A user encounters unexpected behavior and needs to file a support case. They run `kubectl grove diagnostics`, which collects operator pod logs, all Grove CRD instances (PodCliqueSets, PodCliques, PodCliqueScalingGroups, PodGangs, ClusterTopology), recent Kubernetes events, and a pod summary table. The output is a timestamped `.tgz` archive ready to attach to a GitHub issue or discretely share with the Grove team.

### Limitations/Risks & Mitigations

| Limitation/Risk | Mitigation |
|-----------------|------------|
| Large clusters (hundreds of PodCliqueSets, thousands of pods) may cause rendering slowdowns. | Informer events will be coalesced by a 500ms debounce window to avoid excessive snapshot rebuilds (see [Data Flow](#data-flow)). GPU data will be pre-computed during snapshot construction to avoid re-scanning on every render frame. Performance will be validated with manual tests against local Kind clusters with thousands of simulated pods using [KWOK](https://kwok.sigs.k8s.io/) (Kubernetes WithOut Kubelet), following the same approach used to scale-test the Grove operator (see `operator/hack/kind-up.sh --fake-nodes`). |
| TUI will require a terminal with 256-color support for optimal rendering. | Graceful fallback to basic styling is possible but not yet implemented. |
| Informer caches hold full Kubernetes objects in memory, including all pods for GPU accounting. | Informers will use `cache.TransformFunc` to strip unneeded fields before caching, and the `-n` flag scopes to a single namespace (see [Architecture](#architecture)). |
| Informer watches for all Grove CRDs might create many API server connections. | A shared `InformerGlobalCache` will multiplex watches across a single set of informer factories. |
| Read-only design means operators will not be able to take corrective action from within Arborist. | This is an intentional design choice. Lifecycle commands are planned for a future proposal. |
| Arborist requires broad read RBAC permissions (Pods, Nodes, Events, pod logs, all Grove CRDs) and users with restricted roles may get partial or confusing views. | The pre-flight CRD check will fail fast if Grove CRDs are missing. Permission errors from informers and API calls will be surfaced in the TUI error log box (`!`). Future work may add a `--check-permissions` flag that verifies required RBAC rules before launching. |
| Diagnostic bundles may inadvertently include sensitive data (e.g., environment variables in operator logs, configuration values in CRD YAML dumps). | Bundles are written to a local `.tgz` and are never transmitted automatically — the user controls where they are shared. Future work may add redaction of known sensitive fields (e.g., `env[].value`, annotations containing tokens). |
| Arborist imports Grove operator API types directly (`operator/api`), creating tight version coupling between Arborist and the operator CRD schemas. | Arborist will be built and released from the Grove monorepo, ensuring the binary always matches the CRD definitions for that release. Version skew guidance will be documented (e.g., Arborist version should match the deployed operator version). |

## Design Details

### Architecture

The TUI will be built on the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework — the most widely adopted Go TUI framework (~28k GitHub stars), backed by [Charm](https://charm.sh/) and used in production across the Go ecosystem. Bubble Tea uses a unidirectional data flow pattern:

1. **Model** — a single struct that will hold all application state, including a lightweight snapshot of cluster data (display-ready summaries — not full Kubernetes objects), cursor position, active view, filter text, etc. Heavy payloads like resource YAML and pod logs are fetched on-demand and not held in the snapshot.
2. **Update** — a function that will receive messages (user input, new cluster data, async results) and return an updated Model. This will be the only place state changes happen.
3. **View** — a function that will take the current Model and return a string of terminal output to display. It will be a pure rendering function with no side effects.

This means the TUI will never mutate state in response to rendering, and will never render in response to input — all changes will flow through the Update function, making the behavior predictable and testable. Because Update is a pure function (message in, model out), state transitions can be unit tested without a terminal. Bubble Tea also provides a dedicated testing library ([`teatest`](https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest)) for programmatic testing of TUI programs.

```
┌───────────────────────────────────────────────────────────┐
│                         TUI                               │
│                                                           │
│  Model (all state)                                        │
│    │                                                      │
│    ├──→ Update (handle input + cluster data messages)     │
│    │       │                                              │
│    │       ├── user keypress → navigate, filter, toggle   │
│    │       └── new snapshot  → replace cluster data       │
│    │                                                      │
│    └──→ View (render Model to terminal output)            │
│            │                                              │
│            └── reads ViewState from snapshot              │
│                to produce rows, GPU bars, events pane     │
└───────────────────────┬───────────────────────────────────┘
                        │ async messages
                        │
┌───────────────────────┴───────────────────────────────────┐
│                  Informer Cache (k8s)                      │
│  informer events → debouncer → snapshot rebuild           │
│  (client-go informers for all Grove CRDs + Pods)          │
└───────────────────────────────────────────────────────────┘
```

When the TUI's View function needs to render, it will read the `ViewState` from the current snapshot. The `ViewState` will contain pre-assembled, render-ready data: a flat list of table rows (each representing a resource in the hierarchy with its indentation level, status, and GPU summary), the list of Kubernetes events for the currently selected resource, and topology domain trees with aggregated GPU counts. The View function will simply iterate these structures to produce terminal output — it will not need to query Kubernetes or traverse owner references at render time.

Key design decisions:

- **Abstraction layer between TUI and Kubernetes.** The TUI code will not depend on Kubernetes client-go directly. Instead, a pure Go package (`clusterstate`) will define types for resources, events, GPU summaries, and topology — along with a cache interface that the TUI programs against. The Kubernetes-specific implementation will live in a separate `k8s` package. This means TUI logic can be unit tested using a mock implementation of the cache interface, without needing a real cluster or API server.
- **Bottom-up GPU computation.** Rather than reading GPU counts from CRD status fields (which may be stale while the operator is reconciling), Arborist will compute GPU utilization directly from actual pod resource requests and their node assignments, then aggregate upward through the hierarchy. This will provide an accurate real-time view even when CRD status lags behind reality.
- **Cursor stability across snapshot rebuilds.** When informer events trigger a new snapshot (see [Data Flow](#data-flow)), the selected row will be tracked by resource name rather than index, so the cursor will stay on the same logical item even as the underlying data refreshes.
- **Informer memory minimization.** The Pod informer watches all pods (not just Grove-managed ones) because non-Grove pods are needed for three-way GPU accounting. To keep the memory footprint low, all informers will register a [`cache.TransformFunc`](https://pkg.go.dev/k8s.io/client-go/tools/cache#TransformFunc) that strips unneeded fields — `managedFields`, unused annotations, container environment variables, verbose status subfields — from objects *before* they enter the informer store. This is the standard Kubernetes approach for reducing informer memory, used in production by projects like kube-state-metrics. The `-n` namespace flag further scopes all informers to a single namespace, and Events are bounded by a 1-hour max age cutoff.

### Package Structure

| Package | Responsibility |
|---------|---------------|
| `cmd/kubectl-grove` | Binary entry point |
| `internal/cli` | CLI argument parsing and subcommand dispatch |
| `internal/tui` | Bubble Tea model, views, update loop, key bindings, overlays |
| `internal/k8s` | `InformerGlobalCache` implementation, informer factories, snapshot builder |
| `internal/clusterstate` | Pure types and logic — `Resource`, `Event`, `ViewState`, GPU, Topology |
| `internal/diagnostics` | Diagnostic bundle collector |
| `internal/debug` | File-based debug logging |

### Data Flow

Arborist will maintain a **snapshot** — a fully materialized, point-in-time representation of all Grove resources, their hierarchy, GPU summaries, and topology placement. The snapshot will be a pure data structure (a `ViewState`) that the TUI renders directly. When cluster state changes, a new snapshot will be built from scratch to replace the previous one.

1. **Informer watches** will be established for Pods, PodCliqueSets, PodCliqueScalingGroups, PodCliques, PodGangs, and ClusterTopology CRs.
2. **Informer events** (add/update/delete) will be coalesced by a 500ms debounce window to avoid rebuilding the snapshot on every individual event (e.g., during a large rollout that creates many pods in quick succession).
3. When the debounce window fires, the **snapshot builder** will read the current state of all watched resources from the in-memory informer caches (no API server calls at this stage) and construct a new snapshot. This will involve walking owner references to assemble the CRD hierarchy, computing GPU summaries from pod resource requests, and resolving topology placement from node labels.
4. The new snapshot will be delivered to the TUI via Bubble Tea's recommended async pattern and rendered.

**Snapshot size and rebuild cost.** The snapshot will be lightweight. Each resource in the hierarchy will be represented as a small struct of display-ready strings (name, status, GPU counts) — roughly 200 bytes per resource. GPU summaries will be aggregated in a single pass during construction, and topology data will be derived from node labels already held in the informer cache. For a cluster with hundreds of PodCliqueSets and thousands of pods, the snapshot will occupy the low single-digit megabyte range. Rebuilds will be fast because the snapshot builder will read entirely from in-memory informer caches (no API server calls) and use single-pass algorithms. The snapshot will be rebuilt from scratch each time rather than incrementally patched — this will keep the builder simple and avoid subtle inconsistencies from partial updates, while the debounce window will ensure rebuilds happen at most every 500ms regardless of event volume.

### Subcommands

#### tui — Interactive Terminal UI (default)

The default subcommand. Running `kubectl grove` with no arguments will launch the TUI.

```bash
kubectl grove [pcs|pcsg|pc|pod] [-n namespace | -A] [-f filter]
```

- **Resource type argument** (`pcs`, `pcsg`, `pc`, `pod`) will set the initial hierarchy level (default: `pcs`).
- **Pre-flight check** will verify Grove CRDs exist before entering the TUI to avoid rendering errors on a non-Grove cluster.

See [TUI Views](#tui-views) below for details on the forest and topology views.

#### topology — Non-Interactive Topology Tree

Will print a topology tree with GPU utilization to stdout. Suitable for scripting and CI/CD pipelines. Will use 3 targeted API calls (ClusterTopology, Nodes, Pods) instead of the full informer set.

```bash
kubectl grove topology [domain] [pcs-name] [-n namespace | -A]
```

Example output:

```
Topology: rack (topology.io/rack)
Namespace: default

┌ rack: rack-0
│  H200 [██████░░░░░░░░░░░░░░] 7/0/56 (Grove/Other/Free)
├─ pod-0                           [H200: 4]
├─ pod-1                           [H200: 8]
└─ pod-2                           [H200: 4]
```

Features:
- **Domain filtering** by positional argument (e.g., `rack`, `block`, `host`).
- **PCS filtering** to show only pods from a specific PodCliqueSet.
- **Three-way GPU bar graphs** per domain with legend.
- **Unscheduled pod tracking** separates GPU pods waiting for scheduling.

#### diagnostics — Diagnostic Bundle Collector

Will collect cluster diagnostic data and bundle it into a timestamped `.tgz` archive.

```bash
kubectl grove diagnostics [-n namespace | -A] [-o output-dir]
```

Will collect:
- **Operator logs** — all available logs from Grove operator pods.
- **Grove CRD instances** — YAML dumps of PodCliqueSets, PodCliqueScalingGroups, PodCliques, PodGangs.
- **Pod details** — pod summary table plus YAML for unhealthy pods.
- **Kubernetes events** — all events related to Grove resources.

Output: `grove-diagnostics-YYYY-MM-DD-HHMMSS.tgz`

### TUI Views

#### Forest View

Will render the Grove CRD hierarchy as a navigable tree:

```
PodCliqueSet/my-inference-app
├── PodCliqueScalingGroup/my-inference-app-prefill
│   └── PodClique/my-inference-app-prefill-0
│       ├── Pod/my-inference-app-prefill-0-abc12  Running  2 GPU
│       └── Pod/my-inference-app-prefill-0-def34  Running  2 GPU
└── PodCliqueScalingGroup/my-inference-app-decode
    └── PodClique/my-inference-app-decode-0
        └── Pod/my-inference-app-decode-0-ghi56   Pending  4 GPU
```

Features:
- **6-level hierarchy** with automatic single-replica level skipping for cleaner display.
- **Drill-in / drill-back** navigation (Enter / Backspace) to focus on a subtree.
- **Live filtering** (press `/`) with inline autocomplete.
- **Events pane** showing Kubernetes events scoped to the current selection.
- **GPU columns** with three-way accounting at every level.

#### Topology View

Will render the cluster topology as a domain-based tree with GPU utilization:

```
DOMAIN                          GPU (Grove)    GPU (Other)    GPU (Free)
block/block-1                   ████░░░░ 4/8   ██░░░░░░ 2/8   ░░░░░░░░ 2/8
├── rack/rack-1a                ████░░░░ 4/8   ░░░░░░░░ 0/8   ░░░░░░░░ 4/8
│   ├── host/node-01            ████████ 4/4   ░░░░░░░░ 0/4   ░░░░░░░░ 0/4
│   │   └── Pod/prefill-0-abc   Running  2 GPU
```

Features:
- **Domain hierarchy** ordered by network distance (block → rack → host).
- **Three-way GPU bar columns** showing Grove-managed, non-Grove, and free GPU counts.
- **Pod filtering** scoped by drill-in state.
- **Toggle** between forest and topology views (press `t`).

### Full-Screen Overlays

- **YAML Viewer** (`y`): Will fetch and display the YAML representation of the selected resource. Will support search (`/`) and scroll. Virtual replica mapping will resolve PodClique replicas to their parent context.
- **Logs Viewer** (`l`): Will stream logs for the selected pod with 2-second polling. Will support container selection, word wrap toggle, horizontal scroll, and auto-scroll. Will handle carriage-return-based progress output.

### GPU Visualization

GPU data will be pre-computed at snapshot time with three-way accounting:

| Category | Description |
|----------|-------------|
| **Grove** | GPUs allocated to pods managed by Grove (owned by a PodClique) |
| **Other** | GPUs allocated to pods not managed by Grove |
| **Free** | Allocatable GPUs minus all allocated GPUs |

GPU summaries will be aggregated at every level of both the forest and topology views, providing immediate visibility into capacity at any granularity.

### Error UX

- **Error log box** will be toggled with `!`, auto-shown on first error.
- **Maximum 3 entries**, newest-first, to avoid overwhelming the display.
- **Connection error pre-seeding** will surface initial connectivity failures immediately.
- **Async error messages** from informer callbacks will be surfaced through the Bubble Tea message bus.

### kubectl Plugin Integration

The binary will be built as `kubectl-grove`, which kubectl automatically discovers as a plugin when it is on `$PATH`. Running `kubectl grove <subcommand>` will be transparently forwarded to the binary. Distribution via [Krew](https://krew.sigs.k8s.io/) (the kubectl plugin manager) is planned.

The source will live in the Grove monorepo at `tools/arborist/` (currently `arborist/` during initial development, to be relocated).

### k9s Plugin

A [k9s plugin](https://k9scli.io/topics/plugins/) will bring Grove-aware functionality to users who prefer k9s as their primary Kubernetes TUI. The plugin will allow k9s users to launch Arborist views (e.g., forest, topology) directly from k9s when navigating Grove resources.

### Monitoring

Arborist will be a client-side tool and will not expose metrics or health endpoints. Observability will be provided through:

- **Debug logging** to a file (`--debug` flag) for troubleshooting TUI behavior.
- **Diagnostics bundle** captures the state needed to debug cluster-side issues.

### Dependencies

| Dependency | Purpose |
|------------|---------|
| [Bubble Tea](https://github.com/charmbracelet/bubbletea) | TUI framework (Elm architecture) |
| [Lip Gloss](https://github.com/charmbracelet/lipgloss) | Terminal styling |
| [client-go](https://github.com/kubernetes/client-go) | Kubernetes API client and informers |
| Grove operator API (`operator/api/`) | PodCliqueSet, PodClique, PodCliqueScalingGroup, ClusterTopology types |
| Grove scheduler API (`scheduler/api/`) | PodGang types |

### Test Plan

#### Unit Tests

All packages under `internal/` will have unit test coverage. Tests will use table-driven patterns, a `MockGlobalCache` (implementing the `GlobalCache` interface with configurable snapshots, pod YAML, logs, and injectable errors), and fake Kubernetes clients (`kubefake`, `dynamicfake`) for the `k8s` package.

Key areas covered:

- **clusterstate**: GPU computation from pod resource requests and node labels (`BuildGPUSummary`, `ComputeDomainGPUSummary`, `ComputeClusterGPUSummary`), topology resolution (`ResolveTopologyDisplay`, `BuildTopologyInfo`), event aggregation scoped to each CRD level, GPU bar formatting.
- **tui**: Model state transitions for navigation (drill-in/drill-back, cursor movement, view toggling), error UX (cap at 3 entries, newest-first ordering, auto-show on first error), cursor stability across snapshot rebuilds, filter/autocomplete behavior, text truncation and rendering.
- **k8s**: `InformerGlobalCache` lifecycle (start, stop, sync), snapshot rebuilding from informer caches with full object hierarchy, topology data fetching for the CLI command.
- **diagnostics**: Collection orchestration (call ordering, error handling, panic recovery), event filtering and sorting, YAML/table output formatting.
- **cli**: CLI argument parsing, default flag handling, version resolution.

#### Manual Scale Tests

Performance under large-cluster conditions will be validated with manual tests against local Kind clusters using [KWOK](https://kwok.sigs.k8s.io/) to simulate thousands of pods and nodes without real kubelet overhead — the same approach used to scale-test the Grove operator (`operator/hack/kind-up.sh --fake-nodes`). These tests will verify that snapshot rebuilds, TUI rendering, and topology aggregation remain responsive at scale (e.g., hundreds of PodCliqueSets, thousands of pods).

### Graduation Criteria

#### Alpha (current)

- Forest view with full CRD hierarchy navigation.
- Topology view with GPU utilization visualization.
- YAML and logs overlays.
- Diagnostics bundle collection.
- Non-interactive topology CLI command.
- Unit test coverage for core packages.

#### Beta

- Krew distribution manifest.
- k9s plugin for launching Arborist views from within k9s.
- Improved large-cluster performance (pagination, virtual scrolling for very long lists).
- Graceful terminal fallback for environments without 256-color support.
- User documentation and installation instructions.
- Published binary releases for Linux and macOS (amd64 and arm64).

#### GA

- Stable CLI interface with backward-compatibility guarantees.
- Integration into Grove's release process and versioning.
- Comprehensive documentation in the Grove docs site.

## Implementation History

- **2026-02**: Initial implementation with forest view, topology view, GPU visualization, overlays, diagnostics, and error UX.

## Alternatives

- **kubectl with custom output:** Users can inspect Grove resources using `kubectl get` with `-o custom-columns` or JSONPath, but this requires manual cross-referencing of owner references and does not provide a unified hierarchical view.
- **k9s:** A general-purpose Kubernetes TUI that supports custom resource views. However, it does not understand Grove's CRD hierarchy, cannot compute Grove-specific GPU accounting, and does not provide topology-aware visualization.
- **Web-based dashboard:** A browser-based UI was considered but rejected in favor of a terminal tool that works in SSH sessions, jump hosts, and environments without browser access — common in GPU cluster operations.
