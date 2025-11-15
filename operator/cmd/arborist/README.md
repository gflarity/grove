# Forest - Grove Operator TUI

Forest is a k9s-style terminal user interface for the Grove operator, built using the `tview` library. It provides a hierarchical navigation experience for exploring PodCliqueSets, PodCliqueScalingGroups, PodCliques, and Pods.

## Features

- **Hierarchical Navigation**: Navigate through the Grove resource hierarchy
  - **Forest View** (root) - Lists all PodCliqueSets
  - **PodCliqueSet View** - Lists PodCliques and PodCliqueScalingGroups
  - **PodCliqueScalingGroup View** - Lists PodCliques
  - **PodClique View** - Lists Pods
  - **Pod View** - Shows Pod YAML status with pod-specific events
- **Breadcrumb Navigation**: Title bar shows current location in hierarchy (e.g., `Forest > web-frontend > web-autoscaler > web-autoscaler-0`)
- **Drill-Down Navigation**: Press `Enter` on any resource to navigate into it (including Pods!)
- **Pod Detail View**: When you drill into a Pod, see its full YAML status in the top pane
- **Back Navigation**: Press `Esc` to go up one level in the hierarchy
- **Dual Pane Layout**: Vertically stacked Resources/Pod Status and Events tables
- **Tab Switching**: Press `Tab` to switch between top and bottom panes
- **Arrow Navigation**: Use `↑`/`↓` to navigate within the active pane (or scroll in Pod view)
- **Active Highlighting**: Active pane has yellow border, inactive is dim gray
- **Filtered Events**: Events are automatically filtered based on selected resource
  - In Pod view, only shows events specific to that pod
- **Status Bar**: Shows current selection and available keyboard shortcuts
- **Full-Width Tables**: Columns expand proportionally to fill the entire terminal width
- **Color Coding**: Status and event levels are color-coded (green=success, yellow=warning, red=error)

### Resources Table
Displays Grove resources with the following columns:
- **NAME** - Resource name
- **RESOURCE TYPE** - PodClique/PodCliqueSet/PodCliqueScalingGroup/Pod (color-coded)
  - 🔵 Blue - PodCliqueSet
  - 🟣 Purple - PodCliqueScalingGroup
  - 🔷 Aqua - PodClique
  - 🟢 Lime - Pod
- **READY** - Replica status (e.g., "3/3")
- **STATUS** - Running/Scaling/Pending (color-coded)
- **NAMESPACE** - Kubernetes namespace

### Events Table
Displays Kubernetes events filtered by current selection:
- **TYPE** - Event type
- **MESSAGE** - Detailed event message
- **RESOURCE** - Related resource name
- **AGE** - Time since event
- **LEVEL** - Normal/Warning/Error (color-coded)

## Running Forest

```bash
# Run directly
make run
# or
go run main.go

# Build and run
make build
./forest
```

## Keyboard Shortcuts

- `Enter` - Drill down into selected resource (navigate deeper in hierarchy)
- `Esc` - Go back up one level in the hierarchy
- `Tab` - Switch between Resources and Events panes
- `↑`/`↓` - Navigate rows in the active table
- `q` or `Ctrl+C` - Quit

## Navigation Example

1. Start at **Forest View** - see all PodCliqueSets
2. Select `web-frontend` and press `Enter` → **PodCliqueSet View**
3. See PodCliques (`web-frontend-primary`, `web-frontend-canary`) and PodCliqueScalingGroups (`web-autoscaler`)
4. Select `web-autoscaler` and press `Enter` → **PodCliqueScalingGroup View**
5. See PodCliques managed by the scaling group (`web-autoscaler-0`, `web-autoscaler-1`, etc.)
6. Select `web-autoscaler-0` and press `Enter` → **PodClique View**
7. See individual Pods in the clique
8. Select `web-frontend-primary-0` (a Pod) and press `Enter` → **Pod View**
9. See the Pod's full YAML status in the top pane, with pod-specific events below
10. Press `Esc` to go back to PodClique view
11. Press `Esc` again to go back to PodCliqueScalingGroup view
12. Press `Esc` again to go back to PodCliqueSet view
13. Press `Esc` again to return to Forest view

At each level, the Events pane automatically filters to show only events relevant to the current selection. In Pod view, you only see events for that specific pod.

## Building

```bash
# Build the binary
make build

# Clean build artifacts
make clean

# Install dependencies
make deps
```

## Current Implementation

This is a **prototype** with hardcoded fake data demonstrating the hierarchical navigation concept. The data structure includes:

- 4 PodCliqueSets (web-frontend, api-backend, ml-training, db-cluster)
- Multiple PodCliques and PodCliqueScalingGroups under each set
- Individual Pods under each PodClique
- Events associated with various resources at all levels

## Next Steps

Future enhancements planned:

1. **Real Data**: Connect to Kubernetes API and show actual PodCliques/PodCliqueSets from live cluster
2. **Detail Views**: Add detail/describe view for selected resources (YAML, metadata, etc.)
3. **Log Viewer**: Stream logs from selected Pods
4. **Actions**: Add keyboard shortcuts for actions like delete, edit, scale
5. **Search/Filter**: Add search functionality to filter resources by name or label
6. **Auto-refresh**: Update data periodically from the cluster
7. **Multi-namespace**: Support filtering by namespace or viewing across namespaces
8. **Copy/Export**: Copy resource names or export data to clipboard

## Dependencies

- [tview](https://github.com/rivo/tview) - Terminal UI library
- [tcell](https://github.com/gdamore/tcell) - Terminal cell library (used by tview)