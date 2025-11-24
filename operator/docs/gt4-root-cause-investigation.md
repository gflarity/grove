# GT4 Root Cause Investigation: Why Does Deleting sg-x-0 Pods Cause sg-x-1 Pods to be Deleted?

## Investigation Setup

I've added targeted logging with the prefix `🔍 GT4-ROOT-CAUSE:` and `🚨 GT4-ROOT-CAUSE:` to trace the exact sequence of events when pods from `sg-x-0` are deleted and how that causes `sg-x-1` pods to also be deleted.

## The Architecture

### Workload Structure (workload2.yaml)
```
PodCliqueSet: workload2 (1 replica)
├── PCS Replica 0 (workload2-0)
    ├── pc-a (2 replicas, minAvailable=1) [standalone, not in PCSG]
    └── PCSG: sg-x (2 replicas, minAvailable=1)
        ├── PCSG Replica 0 (sg-x-0)
        │   ├── pc-b (1 replica, minAvailable=1)
        │   └── pc-c (3 replicas, minAvailable=1)
        └── PCSG Replica 1 (sg-x-1)
            ├── pc-b (1 replica, minAvailable=1)
            └── pc-c (3 replicas, minAvailable=1)
```

### Gang Termination Hierarchy

There are TWO levels of gang termination:

1. **PCSG Replica-Level Gang Termination** (`podcliquescalinggroup/components/podclique/sync.go`)
   - When a PCSG replica (e.g., `sg-x-0`) has a PodClique breach minAvailable
   - If PCSG's minAvailable is NOT breached: Deletes ONLY that specific PCSG replica
   - If PCSG's minAvailable IS breached: Escalates to PCS controller

2. **PCS Replica-Level Gang Termination** (`podcliqueset/components/podcliquesetreplica/gangterminate.go`)
   - When a PCS replica has a breached component (standalone PodClique OR PCSG)
   - **Deletes ALL PodCliques for that PCS replica** (including ALL PCSG replicas within it)
   - This is the key: PCS replica deletion is ALL-or-NOTHING

## The Critical Question

When all pods from `sg-x-0-pc-c` are deleted:

1. ✅ `sg-x-0-pc-c` gets MinAvailableBreached=True
2. ✅ PCSG controller gang-terminates `sg-x-0` replica (deletes `sg-x-0-pc-b` and `sg-x-0-pc-c`)
3. ❌ **BUT** `sg-x-1` also gets gang-terminated - WHY?

## Three Hypotheses

### Hypothesis 1: PCSG MinAvailableBreached Triggers PCS Replica Deletion

**The Smoking Gun**: After `sg-x-0` is gang-terminated and being recreated:

1. During recreation, `sg-x-0`'s PodCliques are terminating
2. PCSG status reconciliation sees:
   - `sg-x-0`: terminating → NOT counted as scheduled (filtered out)
   - `sg-x-1`: healthy → counted as scheduled
   - **Result**: `scheduledReplicas = 1`
3. PCSG computes MinAvailableBreached:
   - `availableReplicas = scheduledReplicas - minAvailableBreachedReplicas = 1 - 0 = 1`
   - Since `availableReplicas (1) >= minAvailable (1)`: MinAvailableBreached=False ✅

**BUT** if `sg-x-0` starts terminating rapidly:
- Rapid reconciliation might see `scheduledReplicas = 0` temporarily
- This would make `availableReplicas = 0`, triggering PCSG MinAvailableBreached=True
- The PCS controller sees PCSG `sg-x` has MinAvailableBreached=True
- **PCS controller deletes the ENTIRE PCS replica (including sg-x-1)**

### Hypothesis 2: Race Condition in Status Calculation

During rapid reconciliation when `sg-x-0` is being deleted/recreated:
- Status reconciliation runs multiple times in quick succession
- At some point, it sees BOTH `sg-x-0` and `sg-x-1` as not scheduled
- This temporarily makes `scheduledReplicas = 0`
- Triggers PCSG MinAvailableBreached=True
- Which cascades to PCS replica deletion

### Hypothesis 3: Bug in Replica Counting Logic

The `computeReplicaStatus` function filters out terminating PodCliques:
- When `sg-x-0` is being recreated, its PodCliques have `DeletionTimestamp` set
- They get filtered out, making `scheduledReplicas` drop
- But if there's a bug in how we count "available" vs "scheduled" vs "breached" replicas
- We might incorrectly count `sg-x-1` as breached

## Key Code Locations with New Logging

### 1. PCSG Status Reconciliation (`reconcilestatus.go:192`)
```go
func computeMinAvailableBreachedCondition(...)
```
**Logging Added**:
- `🔍 GT4-ROOT-CAUSE: Computing PCSG MinAvailableBreached condition` - Shows all the inputs to the decision
- `🚨 GT4-ROOT-CAUSE: PCSG will be marked MinAvailableBreached=True` - When PCSG is about to be marked as breached

**Key Fields Logged**:
- `scheduledReplicas`: How many PCSG replicas are considered scheduled
- `minAvailableBreachedReplicas`: How many PCSG replicas have breached minAvailable
- `availableReplicas`: `scheduledReplicas - minAvailableBreachedReplicas`
- `totalReplicasInMap`: Total PCSG replicas in the status calculation

### 2. PCS Replica Gang Termination (`gangterminate.go:68`)
```go
func (r _resource) getPCSReplicaDeletionWork(...)
```
**Logging Added**:
- `🔍 GT4-ROOT-CAUSE: Checking PCS replica for gang termination` - For each PCS replica, shows if it has breached components
- `🚨 GT4-ROOT-CAUSE: PCS REPLICA WILL BE GANG-TERMINATED` - When PCS replica deletion is about to happen

**Key Fields Logged**:
- `breachedPCSGNames`: Which PCSGs in this PCS replica have MinAvailableBreached=True
- `breachedPCLQNames`: Which standalone PodCliques have MinAvailableBreached=True
- `minPCSGWaitFor`: How long until termination delay expires for PCSGs

### 3. PCSG Replica Gang Termination (`sync.go:210`)
```go
func (r _resource) processMinAvailableBreachedPCSGReplicas(...)
```
**Logging Added**:
- `🔍 GT4-ROOT-CAUSE: Checking if PCSG replica-level gang termination should occur`
- `🚨 GT4-ROOT-CAUSE: PCSG minAvailable is breached - delegating to PCS controller`
- `🔍 GT4-ROOT-CAUSE: PCSG replica-level gang termination will delete SPECIFIC PCSG replicas only`

**Key Fields Logged**:
- `minAvailableBreachedPCSGReplicas`: How many PCSG replicas are breached
- `pcsgMinAvailable`: What the PCSG's minAvailable threshold is
- `indicesToTerminate`: Which PCSG replica indices will be gang-terminated

### 4. PCSG Breach Detection (`getMinAvailableBreachedPCSGInfo` in `gangterminate.go:174`)
**Logging Added**:
- `🚨 GT4-ROOT-CAUSE: PCSG %s has MinAvailableBreached=True` - When a PCSG is detected as breached
- Shows PCSG status: scheduledReplicas, availableReplicas, updatedReplicas, minAvailable

## How to Interpret the Logs

When running the GT4 test, look for this sequence:

### Expected Correct Flow (if working properly):
```
1. All pods from sg-x-0-pc-c deleted
2. sg-x-0-pc-c gets MinAvailableBreached=True
3. 🔍 PCSG sg-x: checking replica-level gang termination
4. 🔍 PCSG sg-x: will delete SPECIFIC PCSG replica 0 only
5. 🔍 PCSG sg-x: scheduledReplicas=1 (sg-x-1 is still scheduled)
6. 🔍 PCSG sg-x: MinAvailableBreached=False (1 >= 1)
7. ✅ sg-x-0 recreated, sg-x-1 remains healthy
```

### Buggy Flow (what's actually happening):
```
1. All pods from sg-x-0-pc-c deleted
2. sg-x-0-pc-c gets MinAvailableBreached=True
3. 🔍 PCSG sg-x: checking replica-level gang termination
4. 🔍 PCSG sg-x: will delete SPECIFIC PCSG replica 0 only
5. [RAPID RECONCILIATION]
6. 🚨 PCSG sg-x: scheduledReplicas=0 (both replicas filtered?)
7. 🚨 PCSG sg-x: MinAvailableBreached=True (0 < 1)
8. 🚨 PCS replica 0: PCSG sg-x has MinAvailableBreached=True
9. 🚨 PCS REPLICA WILL BE GANG-TERMINATED - ALL PCSG REPLICAS DELETED!
10. ❌ Both sg-x-0 AND sg-x-1 deleted
```

## Next Steps

1. **Run the GT4 test** with these logs enabled
2. **Search for the `🚨` emoji** to find the critical decision points
3. **Trace the sequence** to see exactly when and why the PCSG gets marked as MinAvailableBreached=True
4. **Identify the specific timing** when `scheduledReplicas` drops to 0 or when `sg-x-1` gets incorrectly counted as breached

## Expected Root Cause

Based on the code analysis, I believe the root cause is:

> **When PCSG replica `sg-x-0` is gang-terminated, during the rapid reconciliation cycles, the PCSG status reconciliation sees both `sg-x-0` (terminating) and `sg-x-1` (temporarily not scheduled?) as not meeting the "scheduled" criteria, causing `scheduledReplicas` to drop below `minAvailable`. This triggers PCSG-level MinAvailableBreached=True, which then causes the PCS controller to perform PCS replica-level gang termination, deleting ALL PodCliques in that PCS replica (including the healthy `sg-x-1`).**

The fix would likely involve:
- NOT counting terminating-but-recreating PCSG replicas as "breached" 
- Adding grace period logic to prevent rapid status fluctuations from triggering gang termination
- Distinguishing between "PCSG replica is breached" vs "PCSG replica is being recreated due to its own breach"
