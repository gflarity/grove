# Events Table Format

## Standard Kubernetes Event Format ✅

The events table now follows the standard Kubernetes event format:

```
TYPE | REASON | AGE | FROM | MESSAGE
```

### Column Descriptions

1. **TYPE** - Event severity (color-coded)
   - 🟢 **Normal** (green) - Standard operations
   - 🟡 **Warning** (yellow) - Attention needed
   - 🔴 **Error** (red) - Critical issues

2. **REASON** - Short identifier for the event
   - Examples: `Pulled`, `Started`, `ScalingComplete`, `HealthCheck`, etc.

3. **AGE** - How long ago the event occurred
   - Examples: `5m`, `1h`, `3h`

4. **FROM** - Component that generated the event
   - Examples: `kubelet`, `scheduler`, `podclique-controller`, `scalinggroup-controller`

5. **MESSAGE** - Detailed description of what happened

## Event Statistics

- **Total Events**: 53
- **Normal Events**: 48 (🟢)
- **Warning Events**: 5 (🟡)
- **Error Events**: 0 (🔴)

## Components That Generate Events

### Kubernetes Core Components
- **kubelet** - Container lifecycle (Pulled, Created, Started)
- **scheduler** - Pod scheduling
- **attachdetach-controller** - Volume attachment

### Grove Operator Components
- **podcliqueset-controller** - PodCliqueSet operations
- **podclique-controller** - PodClique operations
- **scalinggroup-controller** - Scaling group operations
- **postgres-operator** - Database-specific operations
- **ml-operator** - ML training operations
- **backup-controller** - Backup operations

## Example Events by Resource Type

### PodCliqueSet Events
```
Normal  Deployed           2h   podcliqueset-controller  Database cluster deployed
Normal  ScalingComplete    5m   podcliqueset-controller  Scaled from 2 to 3 replicas
Normal  HealthCheck       10m   podcliqueset-controller  All database nodes healthy
```

### PodClique Events
```
Normal  PodAdded          10m   podclique-controller     Successfully added pod
Normal  PodReady          10m   podclique-controller     Pod is ready
Warning HighMemory         5m   podclique-controller     High memory usage detected
```

### Pod Events
```
Normal  Pulled            15m   kubelet                  Container image nginx:1.21 pulled
Normal  Created           15m   kubelet                  Created container nginx
Normal  Started           15m   kubelet                  Started container nginx
Warning DiskPressure       1h   kubelet                  Database disk usage at 75%
```

### ScalingGroup Events
```
Normal  ScalingDecision   15m   scalinggroup-controller  Scaling up based on CPU metrics
Normal  PodCliqueAdded    15m   scalinggroup-controller  Added PodClique web-autoscaler-2
Normal  GPUAllocated       2h   scalinggroup-controller  GPU resources allocated
```

## Event Filtering

Events are automatically filtered based on the current view:

1. **Forest View** → All events (53 total)
2. **PodCliqueSet selected** → Events for that set and children
3. **PodClique selected** → Events for that clique and its pods
4. **Pod View** → Only events for that specific pod

## Comparison to Old Format

### Before
```
LEVEL | TYPE | MESSAGE | RESOURCE | AGE
```

### After (Kubernetes Standard)
```
TYPE | REASON | AGE | FROM | MESSAGE
```

**Key Changes:**
- `LEVEL` renamed to `TYPE` (Normal/Warning/Error)
- Old `TYPE` renamed to `REASON` (event reason)
- Added `FROM` column (component that generated event)
- Removed `RESOURCE` column (redundant with filtering)
- Reordered for better readability

## Benefits of New Format

1. ✅ **Standard Kubernetes Format** - Familiar to K8s users
2. ✅ **Component Visibility** - See which controller generated each event
3. ✅ **Better Event Classification** - Reason field provides semantic meaning
4. ✅ **Consistent with kubectl** - Matches `kubectl get events` output
5. ✅ **More Informative** - FROM field adds context about event source
