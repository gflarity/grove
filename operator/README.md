# grove
PodCliqueSet CRD and Controller for Network Topology Aware Gang Scheduling & Autoscaling

:construction_worker: `This project site is currently under active construction`

## Scaling Groups: MinAvailable and Gang Scheduling

Grove's PodClique Scaling Groups provide sophisticated gang scheduling and termination protection through two key configuration parameters: `replicas` and `minAvailable`.

### Overview

**Scaling Groups** allow you to group multiple PodCliques together and scale them as a unit while maintaining gang scheduling semantics. This is particularly useful for distributed workloads that require coordinated scheduling and graceful scaling behavior.

### Key Configuration Parameters

#### `replicas`
- **Purpose**: Sets the desired number of replicas for the scaling group
- **Default**: 1 if not specified
- **Behavior**: Controls how many instances of the scaling group are created

#### `minAvailable` 
- **Purpose**: Defines the minimum number of ready replicas required for operational stability
- **Default**: 1 if not specified  
- **Behavior**: Enables gang scheduling and controls termination policies

### Gang Scheduling Behavior

Grove implements a sophisticated **two-tier gang scheduling** system based on the `minAvailable` setting:

#### Base PodGang (Core Cluster)
- **Replicas**: 0 through (`minAvailable` - 1) 
- **Scheduling**: All pods scheduled together as a single gang
- **Purpose**: Establishes the minimum viable cluster
- **Gates Removed**: Immediately when pods are assigned to the PodGang

#### Scaled PodGangs (Scale-Out Replicas) 
- **Replicas**: `minAvailable` and above
- **Scheduling**: Each replica gets its own scaled PodGang
- **Purpose**: Provides additional capacity once core functionality is established
- **Gates Removed**: Only **after** the base PodGang is ready and running

### Example Scenarios

#### Scenario 1: Database Cluster
```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
spec:
  template:
    podCliqueScalingGroupConfigs:
    - name: "database-cluster"
      replicas: 5
      minAvailable: 3
      cliqueNames: ["db-primary", "db-secondary"]
```

**Behavior**:
- **Replicas 0, 1, 2**: Form base PodGang, scheduled together (minimum viable cluster)
- **Replicas 3, 4**: Scaled PodGangs, wait for base cluster to be ready
- **Result**: Ensures core 3-node cluster is operational before adding scale-out nodes

#### Scenario 2: Machine Learning Training
```yaml
apiVersion: grove.io/v1alpha1  
kind: PodCliqueSet
spec:
  template:
    podCliqueScalingGroupConfigs:
    - name: "ml-training"
      replicas: 8
      minAvailable: 4
      cliqueNames: ["parameter-server", "worker"]
```

**Behavior**:
- **Replicas 0, 1, 2, 3**: Base PodGang for core training infrastructure
- **Replicas 4, 5, 6, 7**: Scaled PodGangs for additional training capacity
- **Result**: Core training cluster established before scaling out workers

### Ready Replica Definition

A scaling group replica is considered **"ready"** when:
- Its associated PodClique has sufficient ready Pods: `PodClique.Status.ReadyReplicas >= PodClique.Spec.MinReplicas`
- A Pod is considered ready when its `PodReady` condition is `True`

### Gang Termination Protection

If the number of ready replicas falls below `minAvailable`:
- **Gang termination** is triggered for the affected scaling group replica
- **Purpose**: Prevents resource waste and maintains workload integrity
- **Behavior**: Ensures workloads fail fast rather than running in degraded states

### Benefits

#### **Efficient Resource Utilization**
- Core functionality established first before scaling out
- Prevents wasteful scheduling of non-essential replicas

#### **Workload Stability** 
- Gang scheduling ensures all-or-nothing scheduling semantics
- Termination protection maintains minimum viable cluster size

#### **Graceful Scaling**
- Base cluster provides stable foundation 
- Scale-out replicas add capacity without disrupting core functionality

---

## Schedule Gate Events and Troubleshooting

Grove uses Kubernetes schedule gates to implement gang scheduling and startup ordering. When pods are blocked from scheduling, Grove emits informative events to help operators understand what's happening.

### Schedule Gate Event Reference

| Event Reason | Emitted On | Meaning | Action |
|--------------|------------|---------|--------|
| `ScheduleGateWaitingGangFormation` | **Pod** | Pod waiting for other PodCliques in gang to create pods | Check blocking PodCliques in event message |
| `ScheduleGateWaitingBasePodCliques` | **Pod** | Scaled gang pod waiting for base gang | Check referenced base PodCliques |
| `ScheduleGateRemoved` | **Pod** | Gate removed successfully | Pod can now be scheduled |
| `PodsPendingCreation` | **PodCliqueSet** | Gang formation waiting for pod creation | Check PodClique controllers |
| `GangFormationComplete` | **PodCliqueSet** | Gang successfully formed | Normal progression |

### Enhanced Status Conditions

PodClique status conditions now include schedule-gated replica counts for better visibility:

```yaml
status:
  replicas: 3
  scheduledReplicas: 0
  scheduleGatedReplicas: 3  # Pods blocked by gates
  conditions:
  - type: PodCliqueScheduled
    status: "False"
    message: "Insufficient scheduled pods. expected at least: 3, scheduled: 0, schedule-gated: 3, total replicas: 3"
```

This immediately shows that all 3 pods exist but are blocked by schedule gates.

### Quick Troubleshooting

**Check schedule-gated pods:**
```bash
kubectl describe pod <pod-name>
```

**View schedule gate events:**
```bash
kubectl get events --field-selector reason=ScheduleGateWaitingGangFormation
kubectl get events --field-selector reason=ScheduleGateWaitingBasePodCliques
```

**Check pod schedule gates:**
```bash
kubectl get pod <pod-name> -o jsonpath='{.spec.schedulingGates}' | jq
```

**For detailed troubleshooting guidance, see:** [Debugging Schedule Gates](./.agents/debugging-schedule-gates.md)
