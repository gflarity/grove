# Forest TUI - Usage Guide

## Quick Start

```bash
# From the forest directory
make run
# or
./forest
```

## Hierarchical Navigation

Forest implements a tree-based navigation system for Grove resources:

```
Forest (Root)
└── PodCliqueSet
    ├── PodClique
    │   └── Pod (drillable → Pod View)
    └── PodCliqueScalingGroup
        └── PodClique
            └── Pod (drillable → Pod View)
```

**Note**: Pods are now drillable! When you press Enter on a Pod, you enter **Pod View** which shows the Pod's YAML status and pod-specific events.

## Views

### 1. Forest View (Root)
- **What it shows**: All PodCliqueSets in the cluster
- **Breadcrumb**: `Forest`
- **Example resources**: web-frontend, api-backend, ml-training, db-cluster

### 2. PodCliqueSet View
- **What it shows**: PodCliques and PodCliqueScalingGroups belonging to the selected PodCliqueSet
- **Breadcrumb**: `Forest > [PodCliqueSet name]`
- **Example**: `Forest > web-frontend`
- **Example resources**: web-frontend-primary (PodClique), web-autoscaler (PodCliqueScalingGroup)

### 3. PodCliqueScalingGroup View
- **What it shows**: PodCliques managed by the selected PodCliqueScalingGroup
- **Breadcrumb**: `Forest > [PodCliqueSet] > [ScalingGroup name]`
- **Example**: `Forest > web-frontend > web-autoscaler`
- **Example resources**: web-autoscaler-0, web-autoscaler-1, web-autoscaler-2 (all PodCliques)

### 4. PodClique View
- **What it shows**: Individual Pods in the selected PodClique
- **Breadcrumb**: `Forest > [parent path] > [PodClique name]`
- **Example**: `Forest > web-frontend > web-frontend-primary`
- **Example resources**: web-frontend-primary-0, web-frontend-primary-1 (Pods - these are drillable!)

### 5. Pod View (NEW!)
- **What it shows**: Pod YAML status (top pane) and pod-specific events (bottom pane)
- **Breadcrumb**: `Forest > [parent path] > [PodClique] > [Pod name]`
- **Example**: `Forest > web-frontend > web-frontend-primary > web-frontend-primary-0`
- **Top pane**: Full YAML including metadata, spec, and status
- **Bottom pane**: Only events for this specific pod (Pulled, Created, Started, etc.)

## Navigation Controls

| Key | Action | Description |
|-----|--------|-------------|
| `↑` / `↓` | Navigate | Move selection up/down in active pane |
| `Tab` | Switch Pane | Toggle between Resources and Events panes |
| `Enter` | Drill Down | Navigate into selected resource (go deeper) |
| `Esc` | Go Back | Navigate up one level in hierarchy |
| `q` / `Ctrl+C` | Quit | Exit the application |

## Event Filtering

The Events pane automatically filters based on your current selection:

1. **No selection / Forest View**: Shows all events
2. **PodCliqueSet selected**: Shows events for that PodCliqueSet and all its children
3. **PodCliqueScalingGroup selected**: Shows events for that group and its PodCliques
4. **PodClique selected (table view)**: Shows events for that PodClique and its Pods
5. **Pod selected (table view)**: Shows events for that specific Pod
6. **Pod View**: Shows ONLY events specific to the current pod (Pulled, Created, Started, health checks, etc.)

## Color Coding

### Resource Types
- 🔵 **Blue** - PodCliqueSet
- 🟣 **Purple** - PodCliqueScalingGroup
- 🔷 **Aqua** - PodClique
- 🟢 **Lime** - Pod

### Status
- 🟢 **Green** - Running (healthy)
- 🟡 **Yellow** - Scaling (in progress)
- 🟠 **Orange** - Pending (waiting)

### Event Levels
- 🟢 **Green** - Normal (informational)
- 🟡 **Yellow** - Warning (attention needed)
- 🔴 **Red** - Error (critical)

## UI Layout

```
┌─────────────────────────────────────────────────────────────────┐
│ 🌳 Forest | Grove Operator | Hierarchical Resource Viewer       │
├─────────────────────────────────────────────────────────────────┤
│ Resources | Forest > web-frontend > web-autoscaler              │
│ ┌───────────────────────────────────────────────────────────┐   │
│ │ NAME              RESOURCE TYPE    READY  STATUS  NAMESPACE│   │
│ │ web-autoscaler-0  PodClique        2/2    Running production│   │
│ │ web-autoscaler-1  PodClique        2/2    Running production│   │
│ │ web-autoscaler-2  PodClique        1/1    Running production│   │
│ └───────────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│ Events                                                          │
│ ┌───────────────────────────────────────────────────────────┐   │
│ │ TYPE        MESSAGE              RESOURCE      AGE   LEVEL │   │
│ │ PodAdded    Added pod            web-auto...   5m    Normal│   │
│ │ Scaling...  Scaling operation    web-auto...   2m    Normal│   │
│ └───────────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│ Resources | Selected: web-autoscaler-0 (PodClique) | <Tab>...  │
└─────────────────────────────────────────────────────────────────┘
```

## Example Navigation Session

```
1. Launch Forest → See all PodCliqueSets
   Forest
   ├─ web-frontend     [SELECT THIS]
   ├─ api-backend
   ├─ ml-training
   └─ db-cluster

2. Press Enter → Drill into web-frontend
   Forest > web-frontend
   ├─ web-frontend-primary (PodClique)
   ├─ web-frontend-canary (PodClique)
   └─ web-autoscaler (PodCliqueScalingGroup)   [SELECT THIS]

3. Press Enter → Drill into web-autoscaler
   Forest > web-frontend > web-autoscaler
   ├─ web-autoscaler-0 (PodClique)   [SELECT THIS]
   ├─ web-autoscaler-1 (PodClique)
   └─ web-autoscaler-2 (PodClique)

4. Press Enter → Drill into web-autoscaler-0
   Forest > web-frontend > web-autoscaler > web-autoscaler-0
   ├─ web-autoscaler-0-pod-0 (Pod)   [SELECT THIS]
   └─ web-autoscaler-0-pod-1 (Pod)

5. Press Enter → View Pod details
   Forest > web-frontend > web-autoscaler > web-autoscaler-0 > web-autoscaler-0-pod-0
   [Top pane shows Pod YAML with metadata, spec, status]
   [Bottom pane shows pod-specific events: Pulled, Created, Started, etc.]

6. Press Esc → Back to PodClique
   Forest > web-frontend > web-autoscaler > web-autoscaler-0
   [Shows list of Pods again]

7. Press Esc → Back to web-autoscaler
   Forest > web-frontend > web-autoscaler
   [Shows list of PodCliques again]

6. Press Esc → Back to web-frontend
   Forest > web-frontend
   [Shows PodCliques and PodCliqueScalingGroups]

7. Press Esc → Back to Forest
   Forest
   [Shows all PodCliqueSets]
```

## Tips

1. **Watch the breadcrumb**: The title bar always shows your current location
2. **Check the status bar**: Shows available actions based on current selection
3. **Use Tab to explore events**: Switch to Events pane to see related events
4. **Filter as you go**: Events automatically filter as you navigate
5. **Drill into Pods!**: Press Enter on any Pod to see its full YAML and events
6. **Scroll in Pod View**: Use ↑/↓ to scroll through Pod YAML when in the top pane

## Prototype Data

This prototype includes hardcoded data with:
- 4 PodCliqueSets
- Multiple PodCliques and PodCliqueScalingGroups
- Various Pods
- Sample events at all levels

The data demonstrates:
- Different resource types at each level
- Mixed hierarchies (direct PodCliques + ScalingGroups)
- Multiple namespaces (production, research, staging)
- Various statuses (Running, Scaling, Pending)
- Different event levels (Normal, Warning, Error)
