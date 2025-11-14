# PodClique View Implementation

## Overview
Implemented the PodClique detail view to show Pods that belong to a PodClique, with events filtered by selected Pod.

## Implementation

### 1. New Functions in `k8s_client.go`

#### `GetPodsForPodClique(ctx, podCliqueName, namespace)`
Fetches Pods that belong to a specific PodClique using label selector.

**Label Query**: `grove.io/podclique=<podCliqueName>`

**Returns**: List of Pod resources with:
- Name
- Type: "Pod"
- Ready: "1/1" or "0/1" based on PodReady condition
- Status: Pod phase (Running, Pending, Failed, etc.)
- ParentType: "PodClique"
- ParentName: `<podCliqueName>`

#### `GetEventsForPodClique(ctx, podCliqueName, namespace)`
Fetches events for the PodClique and its Pods.

**Label Query**: `grove.io/podclique=<podCliqueName>`

**Process**:
1. Create maps to track resource names by Kind
2. Add the PodClique itself
3. Query Pods with the label
4. Fetch all events in namespace
5. Filter events by `involvedObject.Kind` and `involvedObject.Name`
6. Sort by timestamp (newest first)

**Supports events for**:
- PodClique
- Pod

### 2. New Functions in `main.go`

#### `loadPodCliqueChildren(podCliqueName, namespace)`
Loads Pods that belong to the PodClique.

```go
key := "PodClique/" + podCliqueName
pods, err := a.k8sClient.GetPodsForPodClique(a.ctx, podCliqueName, namespace)
a.allResources[key] = pods
```

#### `loadEventsForPodClique(podCliqueName, namespace)`
Loads events for the PodClique and its Pods.

```go
events, err := a.k8sClient.GetEventsForPodClique(a.ctx, podCliqueName, namespace)
a.allEvents = events
```

### 3. Updated Event Filtering

Modified `getFilteredEvents()` to handle Pod selection in PodClique view:

```go
// In PodClique view, if a Pod is selected, show only that Pod's events
if a.viewState.viewType == PodCliqueView {
    row, _ := a.resourcesTable.GetSelection()
    if row >= 1 && row < a.resourcesTable.GetRowCount() {
        selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 2).Text)
        selectedType := strings.TrimSpace(a.resourcesTable.GetCell(row, 1).Text)
        
        if selectedType == "Pod" {
            // Filter to just this pod's events
            filtered := []Event{}
            for _, event := range a.allEvents {
                if event.Parent == selectedName {
                    filtered = append(filtered, event)
                }
            }
            return filtered
        }
    }
    // No pod selected, show all events for the PodClique
    return a.allEvents
}
```

### 4. Updated Navigation Logic

When drilling into a PodClique:
```go
case "PodClique":
    a.viewState.viewType = PodCliqueView
    a.viewState.selectedPodClique = selectedName
    
    // Load children (Pods) and events
    a.loadPodCliqueChildren(selectedName, selectedNamespace)
    a.loadEventsForPodClique(selectedName, selectedNamespace)
```

## Label Structure

Pods that belong to a PodClique have these labels:

```yaml
labels:
  app.kubernetes.io/component: pcsg-podclique
  app.kubernetes.io/managed-by: grove-operator
  app.kubernetes.io/name: env-err-sg-0-main-0-web
  app.kubernetes.io/part-of: env-err-sg
  grove.io/podclique: env-err-sg-0-main-0-web  # ← This is the key label!
  grove.io/podcliqueset-replica-index: "0"
  grove.io/podcliquescalinggroup: env-err-sg-0-main
  grove.io/podcliquescalinggroup-replica-index: "0"
  grove.io/podgang: env-err-sg-0
  grove.io/pod-template-hash: 86584d8b46c9bcc58fb
```

## Navigation Flow

```
Forest View
  └─> PodCliqueSet
      └─> PodCliqueScalingGroup (or standalone PodClique)
          └─> PodClique  ← Can now drill in!
              ├─ Resources (Pods):
              │  ├─ env-err-sg-0-main-0-web-gzmbk  (Ready: 0/1, Status: SchedulingGated)
              │  └─ env-err-sg-0-main-0-web-v88gd  (Ready: 0/1, Status: SchedulingGated)
              └─ Events:
                 ├─ When no Pod selected: Shows all events for PodClique and all Pods
                 └─ When Pod selected: Shows only events for that specific Pod
```

## Event Filtering Behavior

### When viewing PodClique (no Pod selected)
Shows all events for:
- The PodClique itself
- All Pods belonging to the PodClique

**Example events**:
```
Normal   PodCreateSuccessful   podclique-controller   Created Pod: env-err-sg-0-main-0-web-gzmbk
Normal   PodCreateSuccessful   podclique-controller   Created Pod: env-err-sg-0-main-0-web-v88gd
Normal   Scheduled             scheduler              Successfully assigned to node...
```

### When Pod is selected (arrow keys to select a Pod)
Shows only events for that specific Pod:

**Example events for selected Pod `env-err-sg-0-main-0-web-gzmbk`**:
```
Normal   Scheduled    scheduler   Successfully assigned to k3d-grove-cluster-agent-0
Normal   Pulling      kubelet     Pulling container image nginx:latest
Normal   Pulled       kubelet     Successfully pulled image
Normal   Created      kubelet     Created container web
Normal   Started      kubelet     Started container web
```

## Display Example

### Resources Table (PodClique View)
```
NAMESPACE  TYPE  NAME                            READY  PHASE
default    Pod   env-err-sg-0-main-0-web-gzmbk   0/1    Pending
default    Pod   env-err-sg-0-main-0-web-v88gd   0/1    Running
```

Note: Phase column shows the Pod's current phase (Running, Pending, Failed, etc.)

### Events Table (No Pod Selected)
```
TYPE    REASON               AGE   FROM                  MESSAGE
Normal  PodCreateSuccessful  6m    podclique-controller  Created Pod: env-err-sg-0-main-0-web-gzmbk
Normal  PodCreateSuccessful  6m    podclique-controller  Created Pod: env-err-sg-0-main-0-web-v88gd
Normal  Scheduled            6m    scheduler             Successfully assigned...
```

### Events Table (Pod `env-err-sg-0-main-0-web-gzmbk` Selected)
```
TYPE    REASON      AGE   FROM       MESSAGE
Normal  Scheduled   6m    scheduler  Successfully assigned to k3d-grove-cluster-agent-0
Normal  Pulling     6m    kubelet    Pulling container image nginx:latest
Normal  Pulled      6m    kubelet    Successfully pulled image
Normal  Created     6m    kubelet    Created container web
Normal  Started     6m    kubelet    Started container web
```

## Pod Status

The Ready column shows Pod readiness:
- **"1/1"**: Pod has Ready condition = True
- **"0/1"**: Pod is not ready

The Status column shows Pod phase:
- **"Running"**: Pod is running
- **"Pending"**: Pod is being scheduled or waiting to start
- **"SchedulingGated"**: Pod has scheduling gates (waiting for gang scheduling)
- **"Failed"**: Pod has failed
- **"Succeeded"**: Pod completed successfully

## Testing

To test the implementation:

```bash
cd /home/gflarity/git/grove_error_ux/operator/cmd/forest
export KUBECONFIG=/home/gflarity/.kube/config
./forest
```

**Steps**:
1. Start at Forest view
2. Select `env-err-sg` PodCliqueSet → Press Enter
3. Select `env-err-sg-0-main` PodCliqueScalingGroup → Press Enter
4. Select `env-err-sg-0-main-0-web` PodClique → Press Enter
5. ✅ **See Pods listed with Ready status**
6. ✅ **Events show PodCreateSuccessful and Pod lifecycle events**
7. Use arrow keys to select a specific Pod
8. ✅ **Events filter to show only that Pod's events**

## Key Features

### Dynamic Event Filtering
- **Selection-aware**: Events automatically filter when you select a Pod
- **Real-time**: No need to press Enter, just arrow key to select
- **Comprehensive**: Shows all relevant events when no Pod is selected

### Pod Information
- **Ready Status**: Shows if container is ready (1/1 or 0/1)
- **Phase**: Shows current Pod phase (Running, Pending, etc.)
- **Name**: Full Pod name with unique identifier

## Verified with Live Cluster

**Test Query**:
```bash
kubectl get pods -n default -l grove.io/podclique=env-err-sg-0-main-0-web
```

**Result**: Returns 2 Pods ✅

**Events Query**:
```bash
kubectl get events -n default --field-selector involvedObject.kind=Pod | grep env-err-sg-0-main-0-web
```

**Result**: Shows Scheduled, Pulling, Pulled, Created, Started events ✅

## Complete Navigation Hierarchy

```
Forest View (PodCliqueSets)
  ↓ Enter
PodCliqueSet View (PodCliqueScalingGroups + PodCliques)
  ↓ Enter on PodCliqueScalingGroup
PodCliqueScalingGroup View (PodCliques)
  ↓ Enter on PodClique
PodClique View (Pods) ← NEW!
  ├─ No selection: All events for PodClique and Pods
  └─ Pod selected: Only that Pod's events
  ↓ Enter on Pod
Pod Detail View (YAML) - Future enhancement
```

## Future Enhancements

- [ ] Pod detail view showing full YAML/status
- [ ] Container logs viewer
- [ ] Pod restart/delete actions
- [ ] Filter by Pod phase/status
- [ ] Show pod conditions in detail
- [ ] Display resource usage (CPU/Memory)
