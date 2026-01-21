# Event Loading Fix for PodCliqueSetReplicaView

## Problem

When navigating to the PodCliqueSetReplicaView and selecting child resources (PodCliques or PodCliqueScalingGroups), the events pane was not updating to show events for the selected resource.

## Root Cause

The selection change handler in `refreshResourcesTable()` was missing a case for `PodCliqueSetReplicaView`. It had handlers for:
- `ForestView` - Load events when PodCliqueSet is selected
- `PodCliqueSetView` - Load events when PodCliqueSetReplica is selected

But it was missing:
- `PodCliqueSetReplicaView` - Load events when PodClique or PodCliqueScalingGroup is selected

## Solution

Added a new handler in the selection change callback that detects when we're in `PodCliqueSetReplicaView` and loads the appropriate events based on the selected resource type:

```go
// If in PodCliqueSetReplicaView and a child resource is selected, load its events
if a.viewState.viewType == PodCliqueSetReplicaView {
    selectedName := strings.TrimSpace(table.GetCell(row, 2).Text)
    selectedNamespace := strings.TrimSpace(table.GetCell(row, 0).Text)
    selectedType := strings.TrimSpace(table.GetCell(row, 1).Text)
    
    if selectedType == "PodCliqueScalingGroup" {
        a.loadEventsForPodCliqueScalingGroup(selectedName, selectedNamespace)
    } else if selectedType == "PodClique" {
        a.loadEventsForPodClique(selectedName, selectedNamespace)
    }
}
```

## Behavior After Fix

1. **Initial Navigation**: When you navigate into a PodCliqueSetReplicaView, events for the entire replica are loaded
2. **First Row Auto-Selection**: When the first row is automatically selected, events for that specific resource are loaded and displayed
3. **Manual Selection**: When you navigate up/down through resources, events update to show events for the currently selected resource
4. **Proper Filtering**: Events are filtered to show only events related to:
   - The selected PodCliqueScalingGroup and its child PodCliques/Pods
   - The selected PodClique and its child Pods

## Testing

To verify the fix works:
1. Navigate to a PodCliqueSet with replicas
2. Enter a PodCliqueSetReplica view
3. Select different PodCliques and PodCliqueScalingGroups
4. Verify that the events pane updates each time to show events relevant to the selected resource
5. Navigate down into child resources and back up to verify events update correctly at each level
