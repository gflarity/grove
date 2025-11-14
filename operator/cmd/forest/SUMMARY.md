# Forest CLI - Real Data Integration Summary

## Overview
Successfully integrated real Kubernetes data into the Forest CLI GUI application for debugging Grove operator issues.

## Completed Features

### ✅ Forest View (Root View)
- **Resources**: Shows all PodCliqueSet resources from the cluster
  - **Ready Column**: `availableReplicas/replicas` from status
  - **Scheduled Column**: Empty (PodCliqueSets don't track scheduled replicas)
- **Events**: Shows events for selected PodCliqueSet
  - Automatically loads when selecting a PodCliqueSet
  - Filters by label `app.kubernetes.io/part-of=<podcliqueset-name>`
  - Includes events for PodCliqueSet, PodCliqueScalingGroups, PodCliques, and Pods

### ✅ PodCliqueSet View (Drill-Down View)
- **Resources**: Shows children of the selected PodCliqueSet
  - PodCliqueScalingGroup resources
    - **Ready**: `availableReplicas/replicas`
    - **Scheduled**: `scheduledReplicas/replicas` ✅
  - Standalone PodClique resources
    - **Ready**: `readyReplicas/replicas`
    - **Scheduled**: `scheduledReplicas/replicas` ✅
- **Events**: Shows events for all resources under the PodCliqueSet

## Implementation Details

### Data Sources

#### PodCliqueSet Resources
- Queried via dynamic client using GVR: `grove.io/v1alpha1/podcliquesets`
- Across all namespaces
- Status fields used:
  - `replicas` (spec)
  - `availableReplicas` (status)

#### PodCliqueScalingGroup Resources
- Queried via dynamic client using GVR: `grove.io/v1alpha1/podcliquescalinggroups`
- Filtered by label: `app.kubernetes.io/part-of=<podcliqueset-name>`
- Status fields used:
  - `replicas` (status)
  - `availableReplicas` (status)
  - `scheduledReplicas` (status)

#### PodClique Resources
- Queried via dynamic client using GVR: `grove.io/v1alpha1/podcliques`
- Filtered by label: `app.kubernetes.io/part-of=<podcliqueset-name>`
- Excludes PodCliques that belong to scaling groups
- Status fields used:
  - `replicas` (spec)
  - `readyReplicas` (status)
  - `scheduledReplicas` (status)

#### Events
- Queried via clientset: `CoreV1().Events(namespace).List()`
- Filtered by:
  1. Find all resources with label `app.kubernetes.io/part-of=<podcliqueset-name>`
  2. Match events where `involvedObject.Kind` and `involvedObject.Name` match collected resources
- Supports events for: PodCliqueSet, PodCliqueScalingGroup, PodClique, Pod
- Sorted by timestamp (newest first)

### Key Functions

#### k8s_client.go
- `NewK8sClient()` - Initialize Kubernetes client (supports kubeconfig and in-cluster)
- `GetAllPodCliqueSets(ctx)` - Fetch all PodCliqueSets
- `GetPodCliqueScalingGroupsForPodCliqueSet(ctx, name, ns)` - Fetch scaling groups
- `GetPodCliquesForPodCliqueSet(ctx, name, ns)` - Fetch standalone PodCliques
- `GetEventsForPodCliqueSet(ctx, name, ns)` - Fetch filtered events

#### main.go
- `loadForestData()` - Load PodCliqueSets on startup
- `loadPodCliqueSetChildren(name, ns)` - Load children when drilling down
- `loadEventsForPodCliqueSet(name, ns)` - Load events for selected resource
- Selection handler automatically loads events when selecting PodCliqueSet in Forest view

## Column Definitions

| Column | Width | Description |
|--------|-------|-------------|
| NAMESPACE | 2x | Kubernetes namespace |
| TYPE | 2x | Resource type (PodCliqueSet, PodCliqueScalingGroup, PodClique, Pod) |
| NAME | 3x | Resource name |
| READY | 1x | Ready replicas / Total replicas |
| SCHEDULED/PHASE | 2x | Scheduled replicas / Total replicas (PodClique & PCSG) or Pod Phase (Pods) |

## Status Progression Examples

### Healthy PodClique
1. **Created**: Ready: `0/3`, Scheduled: `0/3`
2. **Scheduling**: Ready: `0/3`, Scheduled: `1/3`
3. **Starting**: Ready: `0/3`, Scheduled: `3/3`
4. **Running**: Ready: `3/3`, Scheduled: `3/3`

### PodClique with Issues (Current Cluster Example)
- Ready: `0/2`, Scheduled: `0/2`
- Reason: Pods have scheduling gates active (gang scheduling waiting)
- Events show: "Insufficient scheduled pods. expected at least: 2, found: 0"

### PodClique with Pod Creation Errors (Current Cluster Example)
- Ready: `0/2`, Scheduled: `0/2`
- Reason: Pod creation failing due to invalid environment variable names
- Events show: "PodCreateFailed: Pod ... is invalid: spec.containers[0].env[0].name: Invalid value: 'KEY=1'"

## Navigation Flow

```
Forest View (All PodCliqueSets)
  ├─ Select PodCliqueSet → Events load automatically
  ├─ Press Enter → Drill into PodCliqueSet View
  │
  └─> PodCliqueSet View (Children)
       ├─ PodCliqueScalingGroups (with Ready & Scheduled)
       ├─ PodCliques (with Ready & Scheduled)
       └─ Events (filtered for all related resources)
```

## Usage

```bash
cd /home/gflarity/git/grove_error_ux/operator/cmd/forest
export KUBECONFIG=/home/gflarity/.kube/config
./forest
```

### Keyboard Shortcuts
- `↑`/`↓` - Navigate rows in active pane
- `Tab` - Switch between Resources and Events panes
- `Enter` - Drill down into selected resource
- `Esc` - Go back up one level in hierarchy
- `q` or `Ctrl+C` - Quit

## Verified Against Live Cluster

Tested with k3d cluster containing:
- **PodCliqueSet**: `container-error-invalid-env`
- **PodCliques**: 
  - `container-error-invalid-env-0-api` (Ready: 0/2, Scheduled: 0/2)
  - `container-error-invalid-env-0-web` (Ready: 0/2, Scheduled: 0/2)
  - `container-error-invalid-env-0-worker` (Ready: 0/1, Scheduled: 0/1)
- **Events**: 
  - PodCreateFailed warnings for invalid env variable names
  - PodCreateSuccessful for web and worker pods
  - PodCliqueCreateOrUpdateSuccessful events

## Files Modified

1. **cmd/forest/go.mod** - Added Kubernetes client dependencies
2. **cmd/forest/k8s_client.go** - New file with Kubernetes integration
3. **cmd/forest/main.go** - Updated to use real data instead of fake data

## Future Enhancements

Still using fake/placeholder data:
- [ ] PodCliqueScalingGroup detail view
- [ ] PodClique detail view  
- [ ] Pod YAML view
- [ ] Pod events filtering
- [ ] Auto-refresh functionality
- [ ] Cross-namespace support
- [ ] Search/filter functionality

## Documentation Files

- `REAL_DATA_INTEGRATION.md` - Initial real data integration
- `FOREST_VIEW_EVENTS_FIX.md` - Fix for Forest view event loading
- `SCHEDULED_COLUMN_IMPLEMENTATION.md` - Scheduled column implementation
- `SUMMARY.md` - This file (comprehensive overview)

## Architecture

```
┌─────────────────────────────────────────┐
│  Forest TUI Application (main.go)      │
│  - Resource navigation                  │
│  - Event display                        │
│  - User interaction                     │
└─────────────┬───────────────────────────┘
              │
              ├─ Resource Loading
              ├─ Event Loading
              ├─ Selection Handling
              │
┌─────────────▼───────────────────────────┐
│  K8sClient (k8s_client.go)              │
│  - Kubernetes API integration           │
│  - Resource queries with label filters  │
│  - Event queries with field selectors   │
└─────────────┬───────────────────────────┘
              │
              ├─ Dynamic Client (CRDs)
              ├─ Typed Client (Events, Pods)
              │
┌─────────────▼───────────────────────────┐
│  Kubernetes API Server                  │
│  - PodCliqueSet CRD                     │
│  - PodCliqueScalingGroup CRD            │
│  - PodClique CRD                        │
│  - Core API (Events, Pods)              │
└─────────────────────────────────────────┘
```

## Success Criteria ✅

- [x] Forest view shows real PodCliqueSets from cluster
- [x] Ready column shows actual replica status
- [x] Scheduled column shows scheduled replica status (PodClique & PCSG)
- [x] Events load when selecting PodCliqueSet in Forest view
- [x] Events filter by label `app.kubernetes.io/part-of`
- [x] Events include PodCliqueSet, PCSG, PodClique, and Pod events
- [x] Drilling into PodCliqueSet shows children resources
- [x] Navigation works seamlessly with real data
- [x] Graceful error handling for missing K8s connection
- [x] Code compiles and runs successfully
