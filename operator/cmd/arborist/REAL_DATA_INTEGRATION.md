# Real Data Integration Summary

## Overview
Successfully integrated real Kubernetes data into the Forest CLI GUI application. The application now queries live data from the Kubernetes cluster instead of using fake hardcoded data.

## Changes Made

### 1. Updated `go.mod`
- Added Kubernetes client dependencies:
  - `k8s.io/client-go v0.34.1`
  - `k8s.io/api v0.34.1`
  - `k8s.io/apimachinery v0.34.1`
  - `github.com/ai-dynamo/grove/operator/api v0.0.0`
- Added replace directive for local Grove API

### 2. Created `k8s_client.go`
New file with Kubernetes client integration:

#### Key Functions:
- **`NewK8sClient()`** - Initializes Kubernetes client
  - Tries in-cluster config first
  - Falls back to kubeconfig (respects `KUBECONFIG` env var)
  
- **`GetAllPodCliqueSets(ctx)`** - Fetches all PodCliqueSet resources
  - Queries across all namespaces
  - Returns Ready status as `availableReplicas/replicas`
  
- **`GetEventsForPodCliqueSet(ctx, name, namespace)`** - Fetches events for a PodCliqueSet
  - Finds all resources with label `app.kubernetes.io/part-of: <podcliqueset-name>`
  - Queries events for:
    - PodCliqueSet itself
    - PodCliqueScalingGroup resources
    - PodClique resources  
    - Pod resources
  - Filters by event's `involvedObject.Kind` and `involvedObject.Name`
  - Sorts events by timestamp (newest first)
  
- **`GetPodCliqueScalingGroupsForPodCliqueSet(ctx, name, namespace)`** - Fetches scaling groups
  - Uses label selector `app.kubernetes.io/part-of=<name>`
  - Returns Ready status as `availableReplicas/replicas`
  
- **`GetPodCliquesForPodCliqueSet(ctx, name, namespace)`** - Fetches standalone PodCliques
  - Uses label selector `app.kubernetes.io/part-of=<name>`
  - Filters out PodCliques that belong to scaling groups
  - Returns Ready status as `readyReplicas/replicas`

#### Type Definitions:
- **`Resource`** - Represents a Grove resource (PodCliqueSet, PodCliqueScalingGroup, PodClique, Pod)
- **`Event`** - Represents a Kubernetes event with timestamp for sorting

### 3. Updated `main.go`
Modified the application to use real data:

#### Changes:
- Added `K8sClient` and `context.Context` fields to `App` struct
- Added `Timestamp` field to `Event` struct for proper sorting
- Created `loadForestData()` - Loads PodCliqueSet data on startup
- Created `loadPodCliqueSetChildren()` - Dynamically loads children when navigating
- Created `loadEventsForPodCliqueSet()` - Fetches events for selected PodCliqueSet
- Updated `navigateInto()` - Triggers data loading when drilling down
- Updated `navigateBack()` - Reloads forest data when returning to root

#### Data Loading Strategy:
- **On Startup**: Load all PodCliqueSets
- **On Navigate Into PodCliqueSet**: 
  - Load children (PodCliqueScalingGroups and PodCliques)
  - Load events for all related resources
- **On Navigate Back to Forest**: Reload PodCliqueSets

## Verified Against Live Cluster

Tested queries against k3d cluster with:
- PodCliqueSet: `container-error-invalid-env`
- PodCliques: `container-error-invalid-env-0-api`, `container-error-invalid-env-0-web`, `container-error-invalid-env-0-worker`
- Events: Successfully fetched events showing pod creation failures and warnings

### Sample Events Found:
```
Warning  PodCreateFailed  container-error-invalid-env-0-api   Error creating pod : Pod "..." is invalid
Normal   PodCliqueCreateOrUpdateSuccessful  container-error-invalid-env  PodClique created or updated
```

## How to Run

```bash
cd /home/gflarity/git/grove_error_ux/operator/cmd/forest

# Set kubeconfig if needed
export KUBECONFIG=/home/gflarity/.kube/config

# Run the application
./forest
```

## Features Implemented

✅ **Forest View** - Shows all PodCliqueSet resources from cluster
  - Real `Ready` status: `availableReplicas/replicas` format
  - Actual namespace and names from cluster
  
✅ **PodCliqueSet View** - Shows children when drilling into a PodCliqueSet
  - Dynamically loads PodCliqueScalingGroups
  - Dynamically loads standalone PodCliques
  - Both show Ready status from cluster
  
✅ **Events View** - Shows real events from Kubernetes
  - Queries events by resource label `app.kubernetes.io/part-of`
  - Supports PodCliqueSet, PodCliqueScalingGroup, PodClique, and Pod events
  - Sorted by timestamp (newest first)
  - Shows Type (Normal/Warning), Reason, Age, From, and Message

✅ **Error Handling** - Graceful degradation
  - Shows warning if K8s client fails to initialize
  - Shows empty data instead of crashing

## Architecture

```
forest (TUI App)
    ↓
K8sClient (Kubernetes Integration)
    ↓
┌─────────────────────────────────────┐
│  Kubernetes API                     │
├─────────────────────────────────────┤
│  • PodCliqueSet CRD                 │
│  • PodCliqueScalingGroup CRD        │
│  • PodClique CRD                    │
│  • Pod resources                    │
│  • Events                           │
└─────────────────────────────────────┘
```

## Event Query Logic

1. User selects PodCliqueSet (e.g., `container-error-invalid-env`)
2. Application queries for resources with label:
   ```
   app.kubernetes.io/part-of=container-error-invalid-env
   ```
3. Builds map of resource names by Kind:
   - `PodCliqueSet`: `[container-error-invalid-env]`
   - `PodClique`: `[container-error-invalid-env-0-api, container-error-invalid-env-0-web, ...]`
   - `Pod`: `[container-error-invalid-env-0-web-gzmbk, ...]`
4. Fetches all events in namespace
5. Filters events where:
   ```
   event.involvedObject.Kind ∈ {PodCliqueSet, PodClique, Pod, ...}
   AND
   event.involvedObject.Name ∈ resourceNames[event.involvedObject.Kind]
   ```
6. Sorts by timestamp (newest first)

## Future Enhancements

The following views are still using fake data and can be implemented later:
- [ ] PodCliqueScalingGroup detail view (drill into scaling group)
- [ ] PodClique detail view (drill into clique)
- [ ] Pod YAML view (drill into pod)
- [ ] Auto-refresh functionality
- [ ] Namespace filtering

## Notes

- **Scheduled column** remains empty as requested (can be implemented later)
- **Ready column** now shows actual status from cluster
- Application connects using kubeconfig automatically
- Gracefully handles missing Kubernetes connection
