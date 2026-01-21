# Forest View Events Fix

## Problem
When in the Forest view (root view showing all PodCliqueSets), selecting a PodCliqueSet was not showing its events in the Events pane. Events were only loaded when drilling INTO the PodCliqueSet.

## Root Cause
Events were only being loaded in the `navigateInto()` function when the user pressed Enter to drill down. The selection handler was not triggering event loading when a PodCliqueSet was simply selected.

## Solution

### 1. Updated Selection Handler in `refreshResourcesTable()`

Added logic to detect when a PodCliqueSet is selected in Forest view and immediately load its events:

```go
// Selection handler
table.SetSelectionChangedFunc(func(row, column int) {
    // ... highlighting code ...
    
    if a.activePane == ResourcesPane && row > 0 {
        // If in Forest view and a PodCliqueSet is selected, load its events
        if a.viewState.viewType == ForestView {
            selectedName := strings.TrimSpace(table.GetCell(row, 2).Text)
            selectedNamespace := strings.TrimSpace(table.GetCell(row, 0).Text)
            selectedType := strings.TrimSpace(table.GetCell(row, 1).Text)
            
            if selectedType == "PodCliqueSet" {
                a.loadEventsForPodCliqueSet(selectedName, selectedNamespace)
            }
        }
        
        a.updateStatusBar()
        a.refreshEventsTable()
    }
})
```

### 2. Updated `getFilteredEvents()` Function

Modified to handle Forest view explicitly, returning all events from `allEvents` since they're already filtered for the selected PodCliqueSet:

```go
// In Forest view, show events for the selected PodCliqueSet
if a.viewState.viewType == ForestView {
    // Events are already filtered for the selected PodCliqueSet in allEvents
    // when selection changes, so just return them all
    return a.allEvents
}
```

### 3. Added Initial Event Loading in `Run()`

Ensured that when the app first starts, if there are PodCliqueSets in the Forest view, events for the first one are loaded automatically:

```go
// Populate tables with initial data
a.refreshResourcesView()

// If we're in Forest view and there are PodCliqueSets, load events for the first one
if a.viewState.viewType == ForestView && len(a.allResources["forest"]) > 0 {
    firstPCS := a.allResources["forest"][0]
    a.loadEventsForPodCliqueSet(firstPCS.Name, firstPCS.Namespace)
}

a.refreshEventsTable()
```

## How It Works

### Event Loading Flow

1. **On App Start**:
   - Load all PodCliqueSets into Forest view
   - If any exist, load events for the first one
   - Display events in Events pane

2. **On Selection Change** (Forest View):
   - User uses arrow keys to select a different PodCliqueSet
   - Selection handler detects the change
   - Calls `loadEventsForPodCliqueSet(name, namespace)`
   - This function:
     - Queries for resources with label `app.kubernetes.io/part-of=<name>`
     - Finds PodCliqueScalingGroups, PodCliques, and Pods
     - Fetches all events in the namespace
     - Filters events by `involvedObject.Kind` and `involvedObject.Name`
     - Sorts events by timestamp (newest first)
     - Stores in `a.allEvents`
   - Refreshes events table
   - Events are displayed

3. **On Enter** (Drill Into PodCliqueSet):
   - Events are already loaded
   - Navigation changes to PodCliqueSet view
   - Same events continue to be displayed

## Verification

Verified with live cluster data:
- PodCliqueSet: `container-error-invalid-env`
- PodCliques have label: `app.kubernetes.io/part-of=container-error-invalid-env`
- Events exist for PodCliques showing:
  - `PodCreateFailed` warnings for invalid environment variable names
  - `PodCreateSuccessful` for successfully created pods

## User Experience

### Before Fix:
1. Select PodCliqueSet in Forest view
2. Events pane is empty ❌
3. Press Enter to drill in
4. Events now appear ✓

### After Fix:
1. App loads with PodCliqueSets
2. First PodCliqueSet is selected
3. **Events immediately show in Events pane** ✅
4. Use arrow keys to select different PodCliqueSet
5. **Events update in real-time** ✅
6. Press Enter to drill in (events already loaded)

## Testing

To test the fix:
```bash
cd /home/gflarity/git/grove_error_ux/operator/cmd/forest
export KUBECONFIG=/home/gflarity/.kube/config
./forest
```

Expected behavior:
1. Forest view shows PodCliqueSet(s)
2. First PodCliqueSet is selected
3. Events pane shows events related to that PodCliqueSet
4. Arrow down/up to select different PodCliqueSets
5. Events pane updates to show events for the newly selected PodCliqueSet

## Related Files
- `cmd/forest/main.go` - Updated selection handler and event filtering
- `cmd/forest/k8s_client.go` - Event fetching logic using label selector
