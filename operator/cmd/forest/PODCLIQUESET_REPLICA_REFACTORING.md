# PodCliqueSet Replica Refactoring

## Overview

This refactoring introduces support for the abstract concept of `PodCliqueSetReplica` in the Forest TUI. While there is no actual CRD for PodCliqueSetReplica, it represents a logical grouping of PodCliques and PodCliqueScalingGroups that belong to the same replica index (tracked via the `grove.io/podcliqueset-replica-index` label).

## Changes Made

### 1. New View Type: PodCliqueSetReplicaView

Added a new view type `PodCliqueSetReplicaView` to the view hierarchy:
- **Forest** (all PodCliqueSets)
  - **PodCliqueSet** (shows virtual PodCliqueSetReplica resources) - NEW!
    - **PodCliqueSetReplica** (shows PodCliques and PodCliqueScalingGroups for a specific replica) - RENAMED from old PodCliqueSetView
      - **PodCliqueScalingGroup** (shows PodCliques for a scaling group)
        - **PodClique** (shows Pods)
          - **Pod** (shows YAML)

### 2. ViewState Updates

Updated `ViewState` struct to track the selected replica index:
```go
type ViewState struct {
    viewType             ViewType
    selectedPodCliqueSet string
    selectedReplicaIndex string // NEW: The replica index (e.g., "0", "1", "2")
    selectedScalingGroup string
    selectedPodClique    string
    selectedPod          string
}
```

### 3. New K8s Client Methods

Added methods to `k8s_client.go`:
- `GetReplicaIndexesForPodCliqueSet()` - Fetches all unique replica indexes for a PodCliqueSet
- `GetPodCliqueScalingGroupsForPodCliqueSetReplica()` - Fetches scaling groups for a specific replica
- `GetPodCliquesForPodCliqueSetReplica()` - Fetches standalone PodCliques for a specific replica
- `GetEventsForPodCliqueSetReplica()` - Fetches events for resources in a specific replica

### 4. Virtual PodCliqueSetReplica Resources

The PodCliqueSet view now displays virtual resources:
- **Type**: `PodCliqueSetReplica`
- **Name**: `<podcliqueset-name>-replica-<index>` (e.g., "web-frontend-replica-0")
- **Ready/Scheduled**: Aggregated counts from all PodCliques and PodCliqueScalingGroups in that replica
- **Color**: Cyan (RGB: 0, 255, 255)

### 5. Smart Navigation

**Single Replica Skip**: When a PodCliqueSet has only 1 replica, the UI automatically skips the PodCliqueSetView and goes directly to the PodCliqueSetReplicaView for that single replica. This improves UX by eliminating unnecessary navigation steps.

**Back Navigation**: The back button intelligently handles this:
- If navigating back from a PodCliqueSetReplicaView with only 1 replica → goes back to Forest
- If navigating back from a PodCliqueSetReplicaView with multiple replicas → goes back to PodCliqueSet view

### 6. Event Filtering

Events are now properly filtered by replica index:
- **PodCliqueSetView**: Shows events for the selected replica when a PodCliqueSetReplica is selected
- **PodCliqueSetReplicaView**: Shows events for all resources in that replica (PodCliques, PodCliqueScalingGroups, and their Pods)

### 7. Breadcrumb Updates

Updated breadcrumb navigation to show replica index:
- `Forest > web-frontend > replica-0 > scaling-group-1 > ...`
- `Forest > web-frontend > replica-1 > podclique-1 > ...`

## Usage

### Viewing Multiple Replicas
1. Navigate to Forest view
2. Select a PodCliqueSet with multiple replicas
3. See list of PodCliqueSetReplica resources (e.g., "web-frontend-replica-0", "web-frontend-replica-1")
4. Select a replica to view its PodCliques and PodCliqueScalingGroups

### Viewing Single Replica
1. Navigate to Forest view
2. Select a PodCliqueSet with only 1 replica
3. Automatically taken to the PodCliqueSetReplica view for that replica
4. See the PodCliques and PodCliqueScalingGroups for that replica

### Events
- When selecting a PodCliqueSetReplica in PodCliqueSet view, events for that replica are shown
- When in PodCliqueSetReplica view, all events for resources in that replica are shown
- Events are properly filtered by the `grove.io/podcliqueset-replica-index` label

## Implementation Details

### Label-Based Filtering

All queries use the `grove.io/podcliqueset-replica-index` label to filter resources:
```go
labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s,grove.io/podcliqueset-replica-index=%s", pcsName, replicaIndex)
```

### Aggregate Statistics

When displaying PodCliqueSetReplica resources, the Ready and Scheduled counts are aggregated from:
1. All PodCliqueScalingGroups with matching replica index
2. All standalone PodCliques with matching replica index

### Resource Keys

Resources are stored in `allResources` map with these keys:
- `"PodCliqueSet/<name>"` - Virtual PodCliqueSetReplica resources
- `"PodCliqueSetReplica/<name>/<index>"` - PodCliques and PodCliqueScalingGroups for a specific replica
- Other keys remain unchanged

## Benefits

1. **Better Organization**: Resources are logically grouped by replica index
2. **Clearer Hierarchy**: The replica concept is now explicit in the UI
3. **Improved UX**: Single replica case is handled automatically
4. **Accurate Event Filtering**: Events are properly associated with the correct replica
5. **Scalability**: Easy to navigate PodCliqueSets with many replicas

## Testing Recommendations

1. Test with PodCliqueSets that have 0, 1, 2, and 3+ replicas
2. Verify event filtering works correctly at each level
3. Test navigation (forward and back) through the hierarchy
4. Verify aggregate statistics are calculated correctly
5. Test with PodCliqueSets that have both PodCliques and PodCliqueScalingGroups
