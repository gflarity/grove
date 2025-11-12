# Demo Scenario Matrix

This matrix maps demo scenarios to their expected status fields, phases, conditions, events, and CLI commands.

## Matrix Legend

- **Error Types**: ImagePullBackOff, CrashLoopBackOff, Unschedulable, etc.
- **Status Fields**: Fields in PodCliqueSet/PodClique status that are affected
- **OperationalSummary Phase**: High-level operational state (Healthy, Degraded, Blocked)
- **Conditions**: Kubernetes conditions that are set
- **Events**: Kubernetes events that are emitted
- **CLI Commands**: grovectl commands to showcase the scenario

## Group 1: Image Error Scenarios

| Scenario | Error Type | Status Fields | OperationalSummary Phase | Conditions | Events | CLI Commands |
|----------|------------|---------------|-------------------------|------------|--------|--------------|
| 1.1 Registry Typo | ImagePullBackOff | LastErrors, PodGangStatuses | Degraded | ImagePullError=True | Failed, Warning | `describe`, `analyze` |
| 1.2 Wrong Tag | ImagePullBackOff | LastErrors, PodGangStatuses | Degraded | ImagePullError=True | Failed, Warning | `describe`, `analyze` |
| 1.3 Malformed Image | ImagePullBackOff | LastErrors, PodGangStatuses | Degraded | ImagePullError=True | Failed, Warning | `describe`, `analyze` |
| 1.4 Invalid Port | ErrImagePull | LastErrors, PodGangStatuses | Degraded | ImagePullError=True | Failed, Warning | `describe`, `analyze` |

## Group 2: Container Error Scenarios

| Scenario | Error Type | Status Fields | OperationalSummary Phase | Conditions | Events | CLI Commands |
|----------|------------|---------------|-------------------------|------------|--------|--------------|
| 2.1 Exit Error | CrashLoopBackOff | LastErrors, PodGangStatuses | Degraded | ContainerError=True, Ready=False | Failed, Warning | `describe`, `analyze`, `debug` |
| 2.2 Missing Binary | CrashLoopBackOff | LastErrors, PodGangStatuses | Degraded | ContainerError=True, Ready=False | Failed, Warning | `describe`, `analyze` |
| 2.3 Invalid Args | Error | LastErrors, PodGangStatuses | Degraded | ContainerError=True | Failed, Warning | `describe`, `analyze` |
| 2.4 Invalid Mount | CreateContainerConfigError | LastErrors, PodGangStatuses | Degraded | ContainerError=True | Failed, Warning | `describe`, `analyze` |

## Group 3: Scheduling Constraint Scenarios

| Scenario | Error Type | Status Fields | OperationalSummary Phase | Conditions | Events | CLI Commands |
|----------|------------|---------------|-------------------------|------------|--------|--------------|
| 3.1 Node Affinity | Unschedulable | PodGangStatuses, BlockingReasons | Blocked | SchedulingBlocked=True, Ready=False | Failed, Warning | `describe`, `debug` |
| 3.2 Anti-Affinity | Unschedulable | PodGangStatuses, BlockingReasons | Blocked | SchedulingBlocked=True, Ready=False | Failed, Warning | `describe`, `debug` |
| 3.3 Cordoned Nodes | Unschedulable | PodGangStatuses, BlockingReasons | Blocked | SchedulingBlocked=True, Ready=False | Failed, Warning | `describe`, `debug` |
| 3.4 Schedule Gates | Unschedulable | PodGangStatuses, BlockingReasons | Blocked | SchedulingBlocked=True, Ready=False | Failed, Warning | `describe`, `debug` |

## Group 4: Resource Constraint Scenarios

| Scenario | Error Type | Status Fields | OperationalSummary Phase | Conditions | Events | CLI Commands |
|----------|------------|---------------|-------------------------|------------|--------|--------------|
| 4.1 High CPU | Unschedulable | PodGangStatuses, BlockingReasons | Blocked | InsufficientResources=True, SchedulingBlocked=True | Failed, Warning | `describe`, `debug` |
| 4.2 High Memory | Unschedulable | PodGangStatuses, BlockingReasons | Blocked | InsufficientResources=True, SchedulingBlocked=True | Failed, Warning | `describe`, `debug` |
| 4.3 Quota Exceeded | Unschedulable | LastErrors, PodGangStatuses | Blocked | ResourceQuotaExceeded=True, SchedulingBlocked=True | Failed, Warning | `describe`, `debug` |
| 4.4 MinAvailable | MinAvailableBreached | PodGangStatuses, AvailableReplicas | Degraded | MinAvailableBreached=True, Ready=False | Warning | `describe`, `analyze` |

## Group 5: Complex Scenarios

| Scenario | Error Type | Status Fields | OperationalSummary Phase | Conditions | Events | CLI Commands |
|----------|------------|---------------|-------------------------|------------|--------|--------------|
| 5.1 Mixed Errors | Multiple | LastErrors, PodGangStatuses | Degraded | Multiple=True | Multiple | `describe`, `analyze`, `debug` |
| 5.2 Cascading | Multiple | LastErrors, PodGangStatuses | Degraded | Multiple=True | Multiple | `describe`, `analyze`, `debug` |
| 5.3 Rolling Update Blocked | Multiple | RollingUpdateProgress, LastErrors | Degraded | UpdateBlocked=True | Failed, Warning | `describe`, `analyze` |
| 5.4 Multi-Gang | Multiple | PodGangStatuses (multiple), LastErrors | Degraded | Multiple=True | Multiple | `describe`, `analyze` |

## Success Scenarios

| Scenario | Initial Phase | Final Phase | Status Fields Changed | Conditions Changed | Events | CLI Commands |
|----------|--------------|-------------|----------------------|-------------------|--------|--------------|
| S1 Image Resolution | Degraded | Healthy | LastErrors cleared, PodGangStatuses updated | ImagePullError=False, Ready=True | Normal | `describe` (before/after) |
| S2 Scheduling Unblocked | Blocked | Healthy | BlockingReasons cleared, PodGangStatuses updated | SchedulingBlocked=False, Ready=True | Normal | `describe`, `debug` (before/after) |
| S3 Rolling Update | Healthy | Healthy | RollingUpdateProgress, UpdatedReplicas | (none) | Normal | `describe` (during) |
| S4 Degraded→Healthy | Degraded | Healthy | All error fields cleared | All=False, Ready=True | Normal | `analyze`, `describe` (before/after) |
| S5 Container Recovery | Degraded | Healthy | LastErrors cleared, PodGangStatuses updated | ContainerError=False, Ready=True | Normal | `describe` (before/after) |
| S6 Resource Resolution | Blocked | Healthy | BlockingReasons cleared, PodGangStatuses updated | InsufficientResources=False, Ready=True | Normal | `describe`, `debug` (before/after) |

## Status Field Details

### LastErrors
- **When Set**: Any error condition detected
- **Content**: Array of error objects with type, message, timestamp
- **Cleared**: When error condition resolves
- **Scenarios**: All error scenarios (Groups 1-5)

### PodGangStatuses
- **When Set**: Always present, updated with pod states
- **Content**: Status for each PodGang including pod states
- **Updated**: Continuously as pod states change
- **Scenarios**: All scenarios

### BlockingReasons
- **When Set**: Scheduling constraints prevent pod scheduling
- **Content**: Array of blocking reason objects
- **Cleared**: When constraints resolved
- **Scenarios**: Groups 3, 4 (scheduling/resource constraints)

### RollingUpdateProgress
- **When Set**: During rolling updates
- **Content**: Update progress information
- **Updated**: As update progresses
- **Scenarios**: S3, 5.3

### OperationalSummary
- **When Set**: Always present
- **Content**: High-level phase and summary
- **Updated**: As overall state changes
- **Scenarios**: All scenarios

## Condition Details

### ImagePullError
- **Type**: Error condition
- **Status**: True when image pull fails
- **Scenarios**: Group 1 (all image errors)

### ContainerError
- **Type**: Error condition
- **Status**: True when container fails
- **Scenarios**: Group 2 (all container errors)

### SchedulingBlocked
- **Type**: Blocking condition
- **Status**: True when scheduling is blocked
- **Scenarios**: Groups 3, 4 (scheduling/resource constraints)

### InsufficientResources
- **Type**: Resource condition
- **Status**: True when resources insufficient
- **Scenarios**: Group 4 (resource constraints)

### MinAvailableBreached
- **Type**: Availability condition
- **Status**: True when MinAvailable not met
- **Scenarios**: 4.4, potentially others

### Ready
- **Type**: Ready condition
- **Status**: True when resource is ready
- **Scenarios**: All scenarios (False in errors, True when healthy)

## Event Types

### Failed
- **When**: Pod failures, scheduling failures
- **Reason**: Specific failure reason (ImagePullBackOff, CrashLoopBackOff, etc.)
- **Scenarios**: All error scenarios

### Warning
- **When**: Non-fatal issues, degraded states
- **Reason**: Warning reason
- **Scenarios**: All error scenarios, some success scenarios

### Normal
- **When**: Successful operations, recovery
- **Reason**: Success reason
- **Scenarios**: Success scenarios

## CLI Command Usage

### grovectl describe
- **Purpose**: Show detailed resource status
- **Usage**: All scenarios (before, during, after)
- **Shows**: Status fields, conditions, operational summary

### grovectl analyze
- **Purpose**: Analyze errors and issues
- **Usage**: Error scenarios, complex scenarios
- **Shows**: Error analysis, recommendations

### grovectl debug
- **Purpose**: Show debugging information
- **Usage**: Scheduling constraints, complex scenarios
- **Shows**: Blocking reasons, detailed error information

## Cross-Reference to YAML Files

| Scenario | YAML File |
|----------|-----------|
| 1.1 | `demo-workloads/errors/group1-image/01-typo-registry.yaml` |
| 1.2 | `demo-workloads/errors/group1-image/02-wrong-tag.yaml` |
| 1.3 | `demo-workloads/errors/group1-image/03-malformed-image.yaml` |
| 1.4 | `demo-workloads/errors/group1-image/04-invalid-port.yaml` |
| 2.1 | `demo-workloads/errors/group2-container/01-exit-error.yaml` |
| 2.2 | `demo-workloads/errors/group2-container/02-missing-binary.yaml` |
| 2.3 | `demo-workloads/errors/group2-container/03-invalid-args.yaml` |
| 2.4 | `demo-workloads/errors/group2-container/04-invalid-mount.yaml` |
| 3.1 | `demo-workloads/errors/group3-scheduling/01-node-affinity.yaml` |
| 3.2 | `demo-workloads/errors/group3-scheduling/02-anti-affinity.yaml` |
| 3.3 | `demo-workloads/errors/group3-scheduling/03-cordoned-nodes.yaml` |
| 3.4 | `demo-workloads/errors/group3-scheduling/04-schedule-gates.yaml` |
| 4.1 | `demo-workloads/errors/group4-resources/01-high-cpu.yaml` |
| 4.2 | `demo-workloads/errors/group4-resources/02-high-memory.yaml` |
| 4.3 | `demo-workloads/errors/group4-resources/03-quota-exceeded.yaml` |
| 4.4 | `demo-workloads/errors/group4-resources/04-min-available.yaml` |
| 5.1 | `demo-workloads/errors/group5-complex/01-mixed-errors.yaml` |
| 5.2 | `demo-workloads/errors/group5-complex/02-cascading.yaml` |
| 5.3 | `demo-workloads/errors/group5-complex/03-rolling-update.yaml` |
| 5.4 | `demo-workloads/errors/group5-complex/04-multi-gang.yaml` |
