# Gang Termination Architecture

## Overview

Gang termination in Grove operates at multiple hierarchical levels to handle failures gracefully while giving workloads opportunities to recover. This document explains where gang termination occurs, why it's structured this way, and how the levels interact.

## Terminology

### Key Timing Concepts

- **terminationGracePeriodSeconds** (Kubernetes standard)
  - How long Kubernetes waits for a single pod to shut down gracefully
  - Default: 30 seconds in Grove
  - Scope: Individual pod shutdown (SIGTERM → wait → SIGKILL)

- **TerminationDelay** (Grove-specific)
  - How long to wait after MinAvailable breach before triggering gang termination
  - Default: 4 minutes (configurable in PodCliqueSet.Spec.Template.TerminationDelay)
  - Scope: Applies to **both** Level 1 (PCSG replica) and Level 2 (PCS replica) gang termination
  - Defined once at PCS level, inherited by all child resources
  - Purpose: Allows time for transient issues to resolve before drastic action

- **TerminationStartupGracePeriod** (Grove-specific)
  - How long after resource creation before gang termination can be triggered
  - Default: 5 minutes (configurable in PodCliqueSet.Spec.Template.TerminationStartupGracePeriod)
  - Scope: Applies to PodCliques and PCSG replicas from their creation time
  - Purpose: Prevents premature gang termination during initial startup before pods have had a chance to schedule and become ready
  - Note: This is checked **before** TerminationDelay - resources within grace period are not considered for termination even if breached

### MinAvailable Breach

A resource is considered "breached" when:
- **PodClique**: `readyReplicas < minAvailable` AND past grace period AND breached for longer than TerminationDelay
- **PCSG**: Number of available PCSG replicas < `PCSG.spec.minAvailable` AND past grace period AND breached for longer than TerminationDelay
- **PCS**: Not directly breached; responds to constituent breaches

**Important**: The `MinAvailableBreached` condition now reflects the **actual current state** of availability. During the grace period, the condition will show `True` if availability is breached, but gang termination will not be triggered. This separation of state reporting (condition) from policy enforcement (termination logic) makes the system easier to observe and debug.

## Gang Termination Levels

### Level 1: PCSG Replica Level

**Location**: `internal/controller/podcliquescalinggroup/components/podclique/sync.go`

**When**: Individual PodClique within a PCSG replica breaches MinAvailable

**Decision Logic**:
```go
// processMinAvailableBreachedPCSGReplicas
// If a PodClique in PCSG replica N has:
//   - Been created more than TerminationStartupGracePeriod ago, AND
//   - Has MinAvailable breached for > TerminationDelay
// → Check if PCSG still has enough healthy replicas
// → If yes: Delete all PodCliques for PCSG replica N only
// → If no: Signal PCS controller (Level 2)
//
// PodCliques within grace period are skipped entirely, even if breached.
```

**Action**: 
- Delete **all PodCliques** belonging to that PCSG replica index
- Example: If `sg-x-0-pc-c` breaches → delete `sg-x-0-pc-a`, `sg-x-0-pc-b`, `sg-x-0-pc-c`

**Recovery**:
- PCSG controller **recreates** the PodCliques for that replica
- Gives the replica another chance to become healthy
- This is intentional recovery behavior, not a bug

**Why This Level Exists**:
- Isolates failure to a single PCSG replica
- Other PCSG replicas continue running
- Allows workload to recover without full teardown
- Respects `PCSG.spec.minAvailable` guarantee

**Example**:
```
PCSG: workload1-0-sg-x (replicas=2, minAvailable=1)
├── Replica 0: sg-x-0
│   ├── pc-a (healthy)
│   ├── pc-b (healthy)
│   └── pc-c (BREACHED) ← Pod deleted, can't reschedule
└── Replica 1: sg-x-1 (all healthy)

Decision: PCSG has 1 healthy replica >= minAvailable(1)
Action: Gang terminate replica 0 only
Result: Delete pc-a, pc-b, pc-c for replica 0
Recovery: PCSG recreates replica 0
```

**Key Code**:
```go
// Check if PCSG minAvailable is breached
minAvailableBreachedPCSGReplicas := len(sc.pcsgIndicesToTerminate) + len(sc.pcsgIndicesToRequeue)
if int(sc.pcsg.Spec.Replicas)-minAvailableBreachedPCSGReplicas < int(*sc.pcsg.Spec.MinAvailable) {
    // Too many PCSG replicas are failing → Escalate to Level 2
    return errPCCGMinAvailableBreached
}
// Otherwise, just delete the failed PCSG replica(s)
```

### Level 2: PCS Replica Level

**Location**: `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`

**When**: 
- PCSG's MinAvailable is breached (Level 1 escalation), OR
- Standalone PodClique (not in PCSG) breaches MinAvailable

**Decision Logic**:
```go
// getPCSReplicaDeletionWork
// For each PCS replica:
//   - Check if any PCSG (created > grace period ago) has MinAvailable breached
//   - Check if any standalone PCLQ (created > grace period ago) has MinAvailable breached
//   - If breach duration > TerminationDelay → Delete entire PCS replica
//
// Resources within grace period are skipped, preventing startup failures from triggering termination.
```

**Action**:
- Delete **all PodCliques** for that PCS replica index (all PCSGs + standalone)
- Example: If PCSG fails → delete all of `pcs-0` (across all PCSGs)

**Recovery**:
- PCS controller recreates all resources for that replica
- Fresh start for the entire replica

**Why This Level Exists**:
- When a PCSG can't maintain minAvailable, the entire PCS replica is likely failing
- Prevents cascading partial failures
- Clean slate recovery for the entire replica
- Respects `PCS.spec.minAvailable` (PCS-level, not shown here but implied)

**Example**:
```
PCS: workload1 (replicas=1)
└── Replica 0: pcs-0
    ├── Standalone PodCliques:
    │   ├── pc-a (healthy)
    │   └── pc-b (healthy)
    └── PCSG: sg-x (minAvailable=1)
        ├── Replica 0: (BREACHED)
        └── Replica 1: (BREACHED)

Decision: PCSG has 0 healthy replicas < minAvailable(1)
Action: Gang terminate entire PCS replica 0
Result: Delete pc-a, pc-b, all sg-x replicas
Recovery: PCS recreates everything
```

**Key Code**:
```go
// Check if PCSG is breached
breachedPCSGNames, minPCSGWaitFor, err := r.getMinAvailableBreachedPCSGs(...)
breachedPCLQNames, minPCLQWaitFor, skipPCSReplicaIndex, err := r.getMinAvailableBreachedPCLQsNotInPCSG(...)

if (len(breachedPCSGNames) > 0 && minPCSGWaitFor <= 0) ||
   (len(breachedPCLQNames) > 0 && minPCLQWaitFor <= 0) {
    // Terminate entire PCS replica
    deletionTasks = append(deletionTasks, r.createPCSReplicaDeleteTask(...))
}
```

## Gang Termination Flow

### Full Cascade Example

```
Timeline for workload with PCSG (replicas=2, minAvailable=1):
Assumptions: TerminationStartupGracePeriod=5m, TerminationDelay=10s

T=0:
  Workload created, all pods start scheduling
  → All resources within grace period (T < 5m)
  → Even if MinAvailable breached, no gang termination possible

T=0 to T+5m (Grace Period):
  Resources starting up, some may fail to schedule initially
  → MinAvailable may be breached, condition shows True
  → But gang termination is blocked by grace period check
  → Operator waits for resources to become healthy

T=6m:
  Grace period expired, normal operation begins
  sg-x-0-pc-c: Pod deleted, node cordoned
  → MinAvailable breached for pc-c in replica 0

T=6m to T+6m+10s (TerminationDelay):
  Operator waits, hoping pod reschedules
  → pc-c remains breached (can't schedule, node cordoned)

T=6m+10s (Level 1 Gang Termination):
  PCSG replica 0: created > grace period ago AND breached for > TerminationDelay
  → Check: PCSG has 1 healthy replica (sg-x-1) >= minAvailable(1)
  → Action: Delete all PodCliques in sg-x-0
  → PCSG recreates sg-x-0
  → New PodCliques enter grace period again (T=6m+10s is their creation time)

T=6m+10s to T+11m+10s (New Grace Period for sg-x-0):
  New sg-x-0 PodCliques within grace period
  → Can't trigger gang termination even if failing
  → If they become healthy before grace period ends: Success!
  → If they remain unhealthy: Will be eligible for termination after grace period

T=8m:
  User deletes pods from sg-x-1-pc-c (original replica, past grace period)
  → Now BOTH PCSG replicas have breaches
  → But sg-x-0 is within grace period, sg-x-1 is not

T=8m to T+8m+10s (TerminationDelay for sg-x-1):
  Operator waits for sg-x-1
  → sg-x-1 remains breached
  → sg-x-0 still within grace period

T=8m+10s (Level 1 tries but blocked):
  sg-x-1 eligible for termination (past grace + past delay)
  But PCSG has only 0 available replicas < minAvailable(1)
  (sg-x-0 not counted as breached yet due to grace period)
  → Can't safely terminate sg-x-1 alone
  → No action yet, wait for sg-x-0 grace period to expire

T=11m+10s (sg-x-0 grace period expires):
  Now BOTH replicas past grace period and breached > TerminationDelay
  → PCSG has 0 healthy replicas < minAvailable(1)
  → PCSG signals PCS: errPCCGMinAvailableBreached
  → PCS sees entire replica is failing
  → Action: Delete ALL PodCliques for PCS replica 0
  → PCS recreates everything with new grace period
```

## The WasOnceHealthy Problem

### Issue

When Level 1 gang termination recreates a PCSG replica, the new PodCliques lose their "was once healthy" tracking. This causes the operator to treat them as "starting up" instead of "degraded", breaking gang termination logic.

### Why It Matters

The `WasOnceHealthy` condition distinguishes:
- **Initial startup**: `readyReplicas=0` because pods are still being created → Don't trigger gang termination
- **Degradation**: `readyReplicas=0` because healthy pods were lost → DO trigger gang termination

Without preserving this across recreation:
- Recreated replicas are always treated as "starting up"
- Gang termination at Level 2 never triggers properly
- Test_GT4_GangTerminationMinReplicasPCSGOwned fails

### Solution

Track which PCSG replicas were ever healthy in `PodCliqueScalingGroupStatus.WasOnceHealthyReplicaIndices`:
1. When PCSG replica becomes healthy → Record in status
2. Level 1 gang termination deletes PodCliques → Status persists in PCSG
3. PCSG recreates PodCliques → Check status, set annotation
4. New PodClique inherits `WasOnceHealthy=True`
5. Can't reschedule → Correctly treated as degradation, triggers Level 2

## Design Rationale

### Why Two Levels?

**Level 1 (PCSG Replica)**:
- Fast recovery for transient failures
- Minimize blast radius
- Keep other replicas running
- Honor PCSG minAvailable contracts

**Level 2 (PCS Replica)**:
- Handle persistent failures
- Clean slate when partial recovery fails
- Honor PCS-level availability contracts

### Why Recreation Instead of Permanent Deletion?

**Recreation gives workloads a chance to recover**:
- Transient node failures
- Temporary network issues
- Resource contention that resolves

**Without recreation**:
- One failure would permanently reduce capacity
- No automatic recovery path
- Would require manual intervention

### Why TerminationDelay?

**Prevents premature termination**:
- Scheduler may need time to find resources after a pod failure
- Transient issues may self-resolve (temporary network blips, node recovery)
- Allows time for rescheduling after node failure

**4-minute default balances**:
- Long enough: Most transient issues resolve
- Short enough: Don't leave unhealthy workloads running indefinitely
- Configurable: Users can adjust per workload requirements

**Same TerminationDelay for both levels**:
- Defined once at `PodCliqueSet.Spec.Template.TerminationDelay`
- Used by both PCSG replica termination (Level 1) and PCS replica termination (Level 2)
- Simplifies configuration - one value controls all gang termination timing
- Ensures consistent behavior across the hierarchy

### Why TerminationStartupGracePeriod?

**Prevents false positives during initial startup**:
- Pods may be slow to start (large images, init containers, slow dependencies)
- Scheduler may need time to find resources for all pods
- Some pods may fail initially but succeed on retry
- Node autoscaling may need time to provision capacity

**5-minute default balances**:
- Long enough: Typical pod startup (1-3 minutes) with buffer
- Short enough: Detect real problems reasonably quickly
- Longer than TerminationDelay: Ensures initial startup breaches never trigger termination

**Applies per-resource from creation time**:
- PodClique creation → Grace period starts
- PCSG replica creation → Grace period starts  
- Recreated resources get a **new** grace period
- This is intentional: Gives recreated resources a fair chance to become healthy

**Relationship to TerminationDelay**:
```
Timeline for a newly created PodClique:
T=0:        Resource created
T=0-5min:   Within grace period - no gang termination possible (even if breached)
T=5min:     Grace period expires
T=5min-9min: Breach occurs, TerminationDelay countdown starts
T=9min:     TerminationDelay expires - gang termination triggered (if still breached)
```

**Recommended values for different scenarios**:
- **Fast iteration/testing**: 1-2 minutes
- **Production workloads**: 5-10 minutes (default: 5)
- **Large-scale deployments**: 10-15 minutes (many pods, slow startup)
- **With autoscaling**: Match or exceed autoscaler provisioning time

## Key Files

- **Level 1 Logic**: `internal/controller/podcliquescalinggroup/components/podclique/sync.go`
  - `processMinAvailableBreachedPCSGReplicas()`
  - `getMinAvailableBreachedPCSGIndices()`

- **Level 2 Logic**: `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`
  - `getPCSReplicaDeletionWork()`
  - `getMinAvailableBreachedPCSGs()`
  - `getMinAvailableBreachedPCLQsNotInPCSG()`

- **WasOnceHealthy Tracking**: `internal/controller/podclique/reconcilestatus.go`
  - `computeWasOnceHealthyCondition()`
  - Checks annotation: `constants.AnnotationInheritWasHealthy`

## Related Documentation

- [Gang Scheduling and Termination](./gang-scheduling-and-termination.md)
- [PodGang Creation and Management](./podgang-creation-and-management.md)
