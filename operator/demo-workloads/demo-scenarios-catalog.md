# Demo Scenarios Catalog

This document catalogs all error scenarios to be demonstrated in the Grove Operator error visibility demos, organized by implementation difficulty and error type.

## Group 1: Easy Image Error Demos

These scenarios demonstrate various ImagePullBackOff and ErrImagePull errors that are easy to trigger and reliably reproduce.

### 1.1 ImagePullBackOff - Registry Hostname Typo
- **Error Type**: ImagePullBackOff
- **Trigger**: Typo in registry hostname (e.g., `registr:5001/nginx:latest` instead of `registry:5001/nginx:latest`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "api"**: Has image pull error (registry typo)
  - **PodClique "web"**: Healthy, but schedule-gated waiting for gang member "api"
  - Both PodCliques in same gang for coordinated scheduling
- **Expected Behavior**: 
  - "api" PodClique: Pods fail to pull image, error in status.lastErrors
  - "web" PodClique: Pods are healthy but schedule-gated (status shows `grove.io/waiting-for-gang: true`)
  - PodCliqueSet: OperationalSummary shows Degraded phase with ChildrenUnhealthy reason
  - Gang blocking: PodGangStatuses shows "api" is blocking, "web" is waiting
  - Events: "api" emits PodErrors event, PCS emits ChildrenUnhealthy event
- **YAML File**: `demo-workloads/errors/group1-image/01-typo-registry.yaml`
- **CLI Commands**: 
  - `grovectl describe pcs <name>` - Shows both cliques, identifies "api" as problematic
  - `grovectl analyze scheduling pcs <name>` - Shows gang blocking info

### 1.2 ImagePullBackOff - Non-existent Image Tag
- **Error Type**: ImagePullBackOff
- **Trigger**: Image tag doesn't exist (e.g., `nginx:does-not-exist`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "backend"**: Has image pull error (non-existent tag)
  - **PodClique "frontend"**: Healthy, schedule-gated waiting for gang
  - **PodClique "cache"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "backend" PodClique: Pods fail to pull image, error shows tag not found
  - "frontend" and "cache" PodCliques: Healthy but schedule-gated
  - PodCliqueSet: Shows Degraded with 1/3 cliques unhealthy
  - Gang blocking: backend blocks gang, frontend and cache wait
- **YAML File**: `demo-workloads/errors/group1-image/02-wrong-tag.yaml`

### 1.3 ImagePullBackOff - Malformed Image Reference
- **Error Type**: ImagePullBackOff
- **Trigger**: Invalid image format (e.g., `nginx@sha256:invalid`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "worker"**: Has malformed image reference
  - **PodClique "monitor"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "worker" PodClique: Pods fail to parse image reference, error shows malformed format
  - "monitor" PodClique: Healthy but schedule-gated
  - PodCliqueSet: Shows Degraded, gang blocked by worker
  - Demonstrates error propagation from pod → clique → cliqueset
- **YAML File**: `demo-workloads/errors/group1-image/03-malformed-image.yaml`

### 1.4 ErrImagePull - Invalid Registry Port
- **Error Type**: ErrImagePull
- **Trigger**: Invalid registry port (e.g., `localhost:99999/nginx`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "app"**: Has invalid registry port
  - **PodClique "sidecar"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "app" PodClique: Connection refused or invalid port error
  - "sidecar" PodClique: Healthy but cannot be scheduled due to gang dependency
  - Error propagates: Pod → PodClique → PodCliqueSet with origin tracking
  - OperationalSummary: Degraded with ChildrenUnhealthy reason
- **YAML File**: `demo-workloads/errors/group1-image/04-invalid-port.yaml`

## Group 2: Easy Container Error Demos

These scenarios demonstrate container runtime errors that occur after image pull succeeds.

### 2.1 CrashLoopBackOff - Exit Immediately
- **Error Type**: CrashLoopBackOff
- **Trigger**: Container exits immediately with error code (`command: ["false"]`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "processor"**: Container exits immediately with error
  - **PodClique "database"**: Healthy, schedule-gated waiting for gang
  - **PodClique "queue"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "processor" PodClique: Pods enter CrashLoopBackOff, restart count increases
  - "database" and "queue" PodCliques: Healthy but schedule-gated
  - PodCliqueSet: Shows Degraded with ChildrenUnhealthy (processor has pod errors)
  - Gang scheduling blocked: PodGangStatuses shows processor blocking the gang
  - Events: processor emits PodErrors with CrashLoopBackOff breakdown
- **YAML File**: `demo-workloads/errors/group2-container/01-exit-error.yaml`

### 2.2 CrashLoopBackOff - Missing Binary
- **Error Type**: CrashLoopBackOff
- **Trigger**: Command references non-existent binary (`command: ["/bin/does-not-exist"]`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "service"**: Container has missing binary
  - **PodClique "ingress"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "service" PodClique: Container fails to start, error shows binary not found
  - "ingress" PodClique: Healthy but schedule-gated
  - PodCliqueSet: Degraded with error origin tracking to service PodClique
  - Demonstrates CrashLoopBackOff error capture and propagation
- **YAML File**: `demo-workloads/errors/group2-container/02-missing-binary.yaml`

### 2.3 Error - Invalid Container Arguments
- **Error Type**: Error
- **Trigger**: Invalid arguments to container (`args: ["--invalid-flag"]`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "app"**: Container has invalid arguments
  - **PodClique "proxy"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "app" PodClique: Container starts but exits with error, shows invalid argument
  - "proxy" PodClique: Healthy but schedule-gated
  - PodCliqueSet: Shows Degraded, lastErrors[] includes argument validation error
  - ChildrenHealthy=False condition on PodCliqueSet
- **YAML File**: `demo-workloads/errors/group2-container/03-invalid-args.yaml`

### 2.4 CreateContainerConfigError - Invalid Mount Path
- **Error Type**: CreateContainerConfigError
- **Trigger**: Empty or invalid mount path (`mountPath: ""`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "stateful"**: Has invalid mount configuration
  - **PodClique "stateless"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "stateful" PodClique: Pod fails to create container, error shows mount issue
  - "stateless" PodClique: Healthy but schedule-gated
  - PodCliqueSet: Degraded with CreateContainerConfigError in lastErrors[]
  - Demonstrates configuration error detection and propagation
- **YAML File**: `demo-workloads/errors/group2-container/04-invalid-mount.yaml`

### 2.5 CreateContainerConfigError - Invalid Environment Variable Key
- **Error Type**: CreateContainerConfigError
- **Trigger**: Invalid environment variable key with embedded equals sign (`name: "KEY=1"`)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "api"**: Has invalid environment variable configuration
  - **PodClique "web"**: Healthy, schedule-gated waiting for gang
  - **PodClique "worker"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "api" PodClique: Pod fails to create container, error shows env var config issue
  - "web" and "worker" PodCliques: Healthy but schedule-gated
  - PodCliqueSet: Error propagates with origin tracking (originPCSReplicaIndex: 0)
  - Demonstrates multiple healthy cliques vs. one problematic clique pattern
- **YAML File**: `demo-workloads/errors/group2-container/05-invalid-env-key.yaml`

## Group 3: Scheduling Constraint Demos

These scenarios demonstrate how gang scheduling constraints prevent pod scheduling.

### 3.1 Gang Scheduling Blocked - Node Affinity
- **Error Type**: Unschedulable (Gang Blocked)
- **Trigger**: Node affinity to non-existent label
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "compute"**: Has node affinity to non-existent label
  - **PodClique "storage"**: Healthy, schedule-gated waiting for gang
  - **PodClique "network"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "compute" PodClique: Pods cannot be scheduled, schedulingSummary shows unschedulableReason
  - "storage" and "network" PodCliques: Healthy but schedule-gated
  - PodCliqueSet: OperationalSummary shows Degraded with SomeGangsBlocked reason
  - **ChildrenHealthy=True** (no pod errors), **GangSchedulingReady=False** (scheduling constraint)
  - PodGangStatuses shows compute blocking the gang with scheduling constraint
  - lastErrors[] remains empty (scheduling constraints not in errors array)
- **YAML File**: `demo-workloads/errors/group3-scheduling/01-node-affinity.yaml`
- **Setup Required**: None (uses non-existent label)

### 3.2 Gang Scheduling Blocked - Pod Anti-Affinity
- **Error Type**: Unschedulable (Gang Blocked)
- **Trigger**: Pod anti-affinity conflicts (all pods must be on different nodes, but gang requires same node)
- **Expected Behavior**:
  - Conflicting constraints prevent scheduling
  - Status shows anti-affinity conflict
- **YAML File**: `demo-workloads/errors/group3-scheduling/02-anti-affinity.yaml`

### 3.3 Gang Scheduling Blocked - Cordoned Nodes
- **Error Type**: Unschedulable (Gang Blocked)
- **Trigger**: Target nodes are cordoned (simulated maintenance scenario)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "app"**: Requires specific node that is cordoned
  - **PodClique "sidecar"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "app" PodClique: Pods cannot be scheduled, unschedulableReason shows node taint
  - "sidecar" PodClique: Healthy but schedule-gated
  - PodCliqueSet: Degraded with GangSchedulingReady=False
  - Demonstrates environmental blocking (cluster maintenance) vs. workload errors
  - Status shows which nodes are unavailable and blocking scheduling
- **YAML File**: `demo-workloads/errors/group3-scheduling/03-cordoned-nodes.yaml`
- **Setup Required**: Use `CordonNode()` helper to cordon nodes

### 3.4 Gang Scheduling Blocked - Schedule Gates
- **Error Type**: Unschedulable (Gang Blocked)
- **Trigger**: Custom schedule gates preventing pod scheduling (external dependency simulation)
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "service"**: Has custom schedule gate (e.g., waiting for external database)
  - **PodClique "client"**: Healthy, schedule-gated waiting for gang
  - **PodClique "metrics"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "service" PodClique: Pods blocked by custom schedule gate
  - "client" and "metrics" PodCliques: Healthy but schedule-gated for gang
  - PodCliqueSet: schedulingSummary shows gate information
  - Demonstrates external dependency blocking pattern
  - All cliques healthy (ChildrenHealthy=True) but gang cannot proceed (GangSchedulingReady=False)
- **YAML File**: `demo-workloads/errors/group3-scheduling/04-schedule-gates.yaml`

## Group 4: Resource Constraint Demos

These scenarios demonstrate resource-related scheduling failures.

### 4.1 Gang Scheduling Blocked - High CPU Requests
- **Error Type**: Unschedulable (Insufficient Resources)
- **Trigger**: Request more CPU than available (e.g., 1000 cores)
- **Expected Behavior**:
  - Pods cannot be scheduled
  - Error shows insufficient CPU
  - Gang scheduling blocked
- **YAML File**: `demo-workloads/errors/group4-resources/01-high-cpu.yaml`

### 4.2 Gang Scheduling Blocked - High Memory Requests
- **Error Type**: Unschedulable (Insufficient Resources)
- **Trigger**: Request more memory than available (e.g., 1Ti)
- **Expected Behavior**:
  - Pods cannot be scheduled
  - Error shows insufficient memory
- **YAML File**: `demo-workloads/errors/group4-resources/02-high-memory.yaml`

### 4.3 Resource Quota Exceeded
- **Error Type**: Unschedulable (Resource Quota Exceeded)
- **Trigger**: Pod requests exceed namespace quota
- **Expected Behavior**:
  - Pods cannot be scheduled
  - Error shows quota exceeded
  - Status shows quota violation
- **YAML File**: `demo-workloads/errors/group4-resources/03-quota-exceeded.yaml`
- **Setup Required**: Apply `demo-workloads/errors/group4-resources/setup-quota.yaml` first

### 4.4 MinAvailable Violations
- **Error Type**: MinAvailableBreached
- **Trigger**: Insufficient resources to meet MinAvailable requirement
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "workers"**: Has high replica count with MinAvailable, but insufficient cluster resources for all
  - **PodClique "coordinator"**: Healthy, schedule-gated waiting for workers to meet MinAvailable
  - **PodClique "storage"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "workers" PodClique: Some pods scheduled, but not enough for MinAvailable threshold
  - MinAvailableBreached condition = True on workers PodClique
  - "coordinator" and "storage" PodCliques: Healthy but schedule-gated
  - PodCliqueSet: OperationalSummary shows Degraded, gang cannot proceed until MinAvailable met
  - Demonstrates partial scheduling with MinAvailable constraints
- **YAML File**: `demo-workloads/errors/group4-resources/04-min-available.yaml`

## Group 5: Advanced/Complex Scenarios

These scenarios combine multiple error types or demonstrate complex failure modes.

### 5.1 Mixed Errors
- **Error Type**: Multiple (ImagePullBackOff, CrashLoopBackOff)
- **Trigger**: Different PodCliques have different error types
- **Topology**: 
  - PodCliqueSet with 1 replica
  - **PodClique "frontend"**: Has ImagePullBackOff error
  - **PodClique "backend"**: Has CrashLoopBackOff error
  - **PodClique "cache"**: Healthy, schedule-gated waiting for gang
  - **PodClique "metrics"**: Healthy, schedule-gated waiting for gang
- **Expected Behavior**:
  - "frontend" PodClique: Image pull failures in status.lastErrors
  - "backend" PodClique: Container crash errors in status.lastErrors
  - "cache" and "metrics" PodCliques: Healthy but schedule-gated
  - PodCliqueSet: Status aggregates multiple error types with origin tracking
  - OperationalSummary reflects worst state (Degraded), lastErrors[] shows both error types
  - Demonstrates multiple simultaneous failure modes in single deployment
- **YAML File**: `demo-workloads/errors/group5-complex/01-mixed-errors.yaml`

### 5.2 Cascading Failures
- **Error Type**: Multiple (Cascading)
- **Trigger**: Errors in one clique cause errors in dependent cliques
- **Expected Behavior**:
  - Errors propagate through dependencies
  - Status shows dependency chain
- **YAML File**: `demo-workloads/errors/group5-complex/02-cascading.yaml`

### 5.3 Rolling Update Blocked
- **Error Type**: Multiple (Rolling Update Blocked)
- **Trigger**: Rolling update cannot proceed due to error conditions
- **Expected Behavior**:
  - Update progress stalls
  - Status shows blocking errors
  - RollingUpdateProgress shows current state
- **YAML File**: `demo-workloads/errors/group5-complex/03-rolling-update.yaml`

### 5.4 Multi-Gang Scenarios
- **Error Type**: Multiple (Different per Gang)
- **Trigger**: Different gangs have different error types
- **Expected Behavior**:
  - Each gang shows its own errors
  - Status aggregates across gangs
  - OperationalSummary reflects overall state
- **YAML File**: `demo-workloads/errors/group5-complex/04-multi-gang.yaml`

## Success Scenarios

These scenarios demonstrate error resolution and successful operations. Even success scenarios follow the multi-PodClique pattern.

### S1: Image Error Resolution
- **Scenario**: Fix image reference after ImagePullBackOff
- **Topology**: Same as Group 1.1, but with corrected image reference
  - PodClique "api": Fixed image reference (was broken, now working)
  - PodClique "web": Healthy, still in same gang
- **Expected Behavior**:
  - All pods successfully pull images
  - Gang scheduling proceeds, all pods scheduled and running
  - Status transitions from Degraded → Healthy
  - ChildrenHealthy condition transitions False → True
  - GangSchedulingReady condition transitions False → True
  - Events show successful recovery
- **YAML File**: Use Group 1 scenario, then apply fix

### S2: Gang Scheduling Unblocked
- **Scenario**: Resolve scheduling constraints (e.g., add node labels)
- **Topology**: Same as Group 3.1, but with correct node labels
  - PodClique "compute": Now schedulable (node labels added)
  - PodClique "storage": Healthy
  - PodClique "network": Healthy
- **Expected Behavior**:
  - Pods become schedulable after constraint resolution
  - Gang scheduling succeeds, all members scheduled
  - Status transitions from Degraded → Healthy
  - schedulingSummary.unschedulableReason clears
  - PodGangStatuses shows all gangs healthy
- **YAML File**: Use Group 3 scenario, then resolve constraints

### S3: Successful Rolling Update
- **Scenario**: Complete rolling update without errors (all components healthy)
- **Topology**: 
  - PodCliqueSet with 3 replicas, rolling update in progress
  - Each replica has: PodClique "api", "web", "worker" (all healthy)
  - Rolling update proceeds gang by gang
- **Expected Behavior**:
  - Update progresses smoothly through all replicas
  - Each gang transitions: Pending → Scheduled → Running → Ready
  - RollingUpdateProgress advances: 1/3 → 2/3 → 3/3
  - Status shows completion, OperationalSummary remains Healthy throughout
  - Demonstrates successful multi-component rolling update
- **YAML File**: Use base template with valid configuration

### S4: Degraded → Healthy Transition
- **Scenario**: Resolve all errors in degraded system (multiple component fixes)
- **Topology**: Start with Group 5.1 (mixed errors), then fix both
  - PodClique "frontend": Fix ImagePullBackOff (correct image)
  - PodClique "backend": Fix CrashLoopBackOff (correct command)
  - PodClique "cache": Already healthy
  - PodClique "metrics": Already healthy
- **Expected Behavior**:
  - OperationalSummary transitions Degraded → Healthy
  - lastErrors[] clears as issues resolve
  - All conditions become healthy (ChildrenHealthy=True, GangSchedulingReady=True)
  - Events show recovery for each fixed component
  - Gang scheduling proceeds once all components healthy
  - Demonstrates multi-component error recovery
- **YAML File**: Fix errors in any Group 1-5 scenario
