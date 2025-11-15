# Scheduled Column Implementation

## Overview
Added support for displaying scheduled replica counts in the Scheduled column for PodClique and PodCliqueScalingGroup resources.

## Changes Made

### 1. Updated Resource Struct (`k8s_client.go`)

Added `Scheduled` field to the Resource struct:

```go
type Resource struct {
    Name       string
    Type       string
    Ready      string
    Scheduled  string // Scheduled replicas (for PodClique and PodCliqueScalingGroup)
    Status     string
    Namespace  string
    ParentType string
    ParentName string
    YAML       string // For Pod detail view
}
```

### 2. Updated `GetPodCliqueScalingGroupsForPodCliqueSet()`

Modified to extract and format scheduled replicas:

```go
replicas := pcsg.Status.Replicas
availableReplicas := pcsg.Status.AvailableReplicas
scheduledReplicas := pcsg.Status.ScheduledReplicas

ready := fmt.Sprintf("%d/%d", availableReplicas, replicas)
scheduled := fmt.Sprintf("%d/%d", scheduledReplicas, replicas)

resources = append(resources, Resource{
    Name:       pcsg.Name,
    Type:       "PodCliqueScalingGroup",
    Ready:      ready,
    Scheduled:  scheduled,  // Now populated!
    // ...
})
```

### 3. Updated `GetPodCliquesForPodCliqueSet()`

Modified to extract and format scheduled replicas:

```go
replicas := pc.Spec.Replicas
readyReplicas := pc.Status.ReadyReplicas
scheduledReplicas := pc.Status.ScheduledReplicas

ready := fmt.Sprintf("%d/%d", readyReplicas, replicas)
scheduled := fmt.Sprintf("%d/%d", scheduledReplicas, replicas)

resources = append(resources, Resource{
    Name:       pc.Name,
    Type:       "PodClique",
    Ready:      ready,
    Scheduled:  scheduled,  // Now populated!
    // ...
})
```

### 4. Updated Table Rendering (`main.go`)

Changed rowData to use `Scheduled` instead of `Status`:

```go
// Add data rows
for row, resource := range resources {
    rowData := []string{
        resource.Namespace,
        resource.Type,
        resource.Name,
        resource.Ready,
        resource.Scheduled,  // Changed from resource.Status
    }
    // ...
}
```

## Column Definitions

The table now shows:

| Column | Description | Example |
|--------|-------------|---------|
| NAMESPACE | Kubernetes namespace | `default` |
| TYPE | Resource type | `PodClique`, `PodCliqueScalingGroup` |
| NAME | Resource name | `container-error-invalid-env-0-api` |
| READY | Ready replicas / Total replicas | `0/2`, `3/3` |
| SCHEDULED | Scheduled replicas / Total replicas | `0/2`, `2/2` |

## Behavior by Resource Type

### PodCliqueSet
- **Ready**: `availableReplicas / replicas` from status
- **Scheduled**: Empty (PodCliqueSet doesn't have scheduledReplicas)

### PodCliqueScalingGroup
- **Ready**: `availableReplicas / replicas` from status
- **Scheduled**: `scheduledReplicas / replicas` from status ✅

### PodClique
- **Ready**: `readyReplicas / replicas` from status
- **Scheduled**: `scheduledReplicas / replicas` from status ✅

### Pod
- **Ready**: Pod ready status
- **Scheduled**: Empty (Pods don't have replica counts)

## Status Field Meanings

### PodClique Status
- **`scheduledReplicas`**: Number of Pods that have been scheduled by kube-scheduler
- **`readyReplicas`**: Number of ready Pods targeted by this PodClique

### PodCliqueScalingGroup Status
- **`scheduledReplicas`**: Number of replicas that are scheduled for the PodCliqueScalingGroup
  - A replica is "scheduled" when at least MinAvailable number of pods in each constituent PodClique has been scheduled
- **`availableReplicas`**: Number of PodCliqueScalingGroup replicas that are available
  - A replica is "available" when all constituent PodCliques have ReadyReplicas >= MinAvailable

## Example Output

When viewing a PodCliqueSet with PodCliques and PodCliqueScalingGroups:

```
NAMESPACE  TYPE                        NAME                                  READY   SCHEDULED
default    PodClique                   container-error-invalid-env-0-api     0/2     0/2
default    PodClique                   container-error-invalid-env-0-web     0/2     0/2
default    PodClique                   container-error-invalid-env-0-worker  0/1     0/1
default    PodCliqueScalingGroup       my-scaling-group                      5/5     5/5
```

## Testing

To verify the implementation:

```bash
cd /home/gflarity/git/grove_error_ux/operator/cmd/forest
export KUBECONFIG=/home/gflarity/.kube/config
./forest
```

Then:
1. Navigate to Forest view (shows PodCliqueSets)
2. Press Enter on a PodCliqueSet to drill in
3. View should show PodCliques and/or PodCliqueScalingGroups
4. **SCHEDULED column now shows actual scheduled replica counts** ✅

## Status Progression

Typical progression for a healthy resource:

1. **Created**: `Ready: 0/3, Scheduled: 0/3`
2. **Scheduling**: `Ready: 0/3, Scheduled: 1/3`
3. **Starting**: `Ready: 0/3, Scheduled: 3/3`
4. **Running**: `Ready: 3/3, Scheduled: 3/3`

For resources with issues (like the example cluster):
- `Ready: 0/2, Scheduled: 0/2` - Pods not being scheduled (scheduling gate active)
- `Ready: 0/2, Scheduled: 2/2` - Pods scheduled but not ready yet
