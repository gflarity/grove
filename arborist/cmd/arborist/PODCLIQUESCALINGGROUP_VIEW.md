# PodCliqueScalingGroup View Implementation

## Overview
Implemented the PodCliqueScalingGroup detail view to show PodCliques that belong to a scaling group and their associated events.

## Implementation

### 1. New Functions in `k8s_client.go`

#### `GetPodCliquesForPodCliqueScalingGroup(ctx, pcsgName, namespace)`
Fetches PodCliques that belong to a specific PodCliqueScalingGroup using label selector.

**Label Query**: `grove.io/podcliquescalinggroup=<pcsgName>`

**Returns**: List of PodClique resources with:
- Name
- Type: "PodClique"
- Ready: `readyReplicas/replicas`
- Scheduled: `scheduledReplicas/replicas`
- ParentType: "PodCliqueScalingGroup"
- ParentName: `<pcsgName>`

#### `GetEventsForPodCliqueScalingGroup(ctx, pcsgName, namespace)`
Fetches events for all resources related to a PodCliqueScalingGroup.

**Label Query**: `grove.io/podcliquescalinggroup=<pcsgName>`

**Process**:
1. Create maps to track resource names by Kind
2. Add the PodCliqueScalingGroup itself
3. Query PodCliques with the label
4. Query Pods with the same label
5. Fetch all events in namespace
6. Filter events by `involvedObject.Kind` and `involvedObject.Name`
7. Sort by timestamp (newest first)

**Supports events for**:
- PodCliqueScalingGroup
- PodClique
- Pod

### 2. New Functions in `main.go`

#### `loadPodCliqueScalingGroupChildren(pcsgName, namespace)`
Loads PodCliques that belong to the scaling group.

```go
key := "PodCliqueScalingGroup/" + pcsgName
podCliques, err := a.k8sClient.GetPodCliquesForPodCliqueScalingGroup(a.ctx, pcsgName, namespace)
a.allResources[key] = podCliques
```

#### `loadEventsForPodCliqueScalingGroup(pcsgName, namespace)`
Loads events for the scaling group and its children.

```go
events, err := a.k8sClient.GetEventsForPodCliqueScalingGroup(a.ctx, pcsgName, namespace)
a.allEvents = events
```

### 3. Updated Navigation Logic

When drilling into a PodCliqueScalingGroup:
```go
case "PodCliqueScalingGroup":
    a.viewState.viewType = PodCliqueScalingGroupView
    a.viewState.selectedScalingGroup = selectedName
    
    // Load children and events
    a.loadPodCliqueScalingGroupChildren(selectedName, selectedNamespace)
    a.loadEventsForPodCliqueScalingGroup(selectedName, selectedNamespace)
```

## Label Structure

PodCliques that belong to a PodCliqueScalingGroup have these labels:

```yaml
labels:
  app.kubernetes.io/component: pcsg-podclique
  app.kubernetes.io/managed-by: grove-operator
  app.kubernetes.io/name: env-err-sg-0-main-0-api
  app.kubernetes.io/part-of: env-err-sg
  grove.io/podcliquescalinggroup: env-err-sg-0-main  # ← This is the key label!
  grove.io/podcliquescalinggroup-replica-index: "0"
  grove.io/podcliqueset-replica-index: "0"
  grove.io/podgang: env-err-sg-0
  grove.io/pod-template-hash: 5bd48485d788d55cd5c4
```

## Verified With Live Cluster

### Cluster State
- **PodCliqueSet**: `env-err-sg`
- **PodCliqueScalingGroup**: `env-err-sg-0-main`
- **PodCliques**:
  - `env-err-sg-0-main-0-api`
  - `env-err-sg-0-main-0-web`
  - `env-err-sg-0-main-0-worker`

### Label Query Test
```bash
kubectl get podcliques -n default -l grove.io/podcliquescalinggroup=env-err-sg-0-main
```

**Result**: Returns 3 PodCliques ✅

### Events Query Test
```bash
kubectl get events -n default --field-selector involvedObject.kind=PodClique,involvedObject.name=env-err-sg-0-main-0-api
```

**Result**: Shows PodCreateFailed events ✅

## Navigation Flow

```
Forest View
  └─> Select: env-err-sg (PodCliqueSet)
      └─> Press Enter
          └─> PodCliqueSet View
              ├─ PodCliqueScalingGroup: env-err-sg-0-main
              └─> Select and Press Enter
                  └─> PodCliqueScalingGroup View  ← NEW!
                      ├─ Resources:
                      │  ├─ env-err-sg-0-main-0-api     (Ready: 0/2, Scheduled: 0/2)
                      │  ├─ env-err-sg-0-main-0-web     (Ready: 0/2, Scheduled: 0/2)
                      │  └─ env-err-sg-0-main-0-worker  (Ready: 0/1, Scheduled: 0/1)
                      └─ Events:
                         ├─ PodCreateFailed for env-err-sg-0-main-0-api
                         ├─ PodCreateSuccessful for env-err-sg-0-main-0-web
                         └─ PodCreateSuccessful for env-err-sg-0-main-0-worker
```

## Display Example

When viewing PodCliqueScalingGroup `env-err-sg-0-main`:

### Resources Table
```
NAMESPACE  TYPE        NAME                         READY  SCHEDULED
default    PodClique   env-err-sg-0-main-0-api      0/2    0/2
default    PodClique   env-err-sg-0-main-0-web      0/2    0/2
default    PodClique   env-err-sg-0-main-0-worker   0/1    0/1
```

### Events Table
```
TYPE     REASON            AGE    FROM                  MESSAGE
Warning  PodCreateFailed   6m5s   podclique-controller  Error creating pod : Pod "..." is invalid...
Normal   PodCreateSuccessful 6m5s podclique-controller  Created Pod: env-err-sg-0-main-0-web-...
Normal   PodCreateSuccessful 6m5s podclique-controller  Created Pod: env-err-sg-0-main-0-worker-...
```

## Key Differences from PodCliqueSet View

| Aspect | PodCliqueSet View | PodCliqueScalingGroup View |
|--------|-------------------|----------------------------|
| **Children** | PodCliqueScalingGroups + standalone PodCliques | Only PodCliques (from scaling group) |
| **Label Filter** | `app.kubernetes.io/part-of=<pcs-name>` | `grove.io/podcliquescalinggroup=<pcsg-name>` |
| **Event Sources** | PCSG, PodClique, Pod | PodClique, Pod |
| **Breadcrumb** | Forest > PodCliqueSet | Forest > PodCliqueSet > PCSG |

## Testing

To test the implementation:

```bash
cd /home/gflarity/git/grove_error_ux/operator/cmd/forest
export KUBECONFIG=/home/gflarity/.kube/config
./forest
```

**Steps**:
1. Start at Forest view
2. Select `env-err-sg` PodCliqueSet
3. Press Enter to drill in
4. See `env-err-sg-0-main` PodCliqueScalingGroup
5. Press Enter to drill into the scaling group
6. ✅ **See 3 PodCliques with Ready/Scheduled status**
7. ✅ **See events showing PodCreateFailed and PodCreateSuccessful**

## Status Indicators

### Typical PodClique Status in Scaling Group

**Healthy**:
- Ready: `2/2`
- Scheduled: `2/2`
- Events: PodCreateSuccessful, Started, Ready

**Scheduling Issues** (Current cluster):
- Ready: `0/2`
- Scheduled: `0/2`
- Events: Shows scheduling gates or scheduling failures

**Container Issues** (Current cluster):
- Ready: `0/2`
- Scheduled: `0/2`
- Events: PodCreateFailed with validation errors

## Future Enhancements

- [ ] Drill into individual PodCliques to see Pods
- [ ] Show Pod detail view with YAML
- [ ] Filter events by severity
- [ ] Show PodCliqueScalingGroup-level status and conditions
- [ ] Display replica indices and rolling update progress
