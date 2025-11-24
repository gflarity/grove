# GT4 Complete Timeline: The Root Cause of Incorrect sg-x-1 Deletion

## Executive Summary

**ROOT CAUSE**: When PCSG replica `sg-x-0` is gang-terminated and recreated, the PCSG status calculation has an inconsistency:
- `scheduledReplicas` **filters OUT** terminating PodCliques
- `minAvailableBreachedReplicas` **does NOT filter** terminating PodCliques

During the recreation window, the **OLD terminating `sg-x-0-pc-c`** (which still has `MinAvailableBreached=True`) gets counted as a breached replica, even though it's being deleted. This causes:
```
availableReplicas = scheduledReplicas - minAvailableBreachedReplicas
                  = 1 - 1
                  = 0
```
This incorrectly marks the entire PCSG as breached, triggering PCS replica-level gang termination that deletes the healthy `sg-x-1`.

---

## The Complete Timeline

### Initial State (18:01:53)
**PCSG `workload2-0-sg-x`** (2 replicas, minAvailable=1):

- **PCSG Replica 0 (`sg-x-0`)**:
  - `sg-x-0-pc-b`: 1/1 pods, healthy ✅
  - `sg-x-0-pc-c`: 3/3 pods, healthy ✅
  - Status: `MinAvailableBreached=False` ✅

- **PCSG Replica 1 (`sg-x-1`)**:
  - `sg-x-1-pc-b`: 1/1 pods, healthy ✅
  - `sg-x-1-pc-c`: 3/3 pods, healthy ✅
  - Status: `MinAvailableBreached=False` ✅

- **PCSG Status**: `scheduledReplicas=2`, `availableReplicas=2`, `MinAvailableBreached=False` ✅

---

### 18:02:18 - Test Deletes Pods from sg-x-0-pc-c

**Action**: Test cordons nodes and deletes all 3 pods from `sg-x-0-pc-c`

**Result**:
- `sg-x-0-pc-c`: 0/3 pods, `MinAvailableBreached=True` ❌
- All other PodCliques remain healthy

---

### ~18:02:21 - PodClique Controller Detects Breach

**PodClique `sg-x-0-pc-c`**:
- Detects 0/3 pods with minAvailable=1
- Sets `MinAvailableBreached=True` on `sg-x-0-pc-c`
- Grace period starts (3s)

---

### ~18:02:24 - PCSG Controller: Replica-Level Gang Termination Decision

**PCSG Controller Reconciles**:
- Sees PCSG replica 0 has a breached PodClique (`sg-x-0-pc-c`)
- Marks **PCSG Replica 0** as `MinAvailableBreached=True`
- Checks: `minAvailableBreachedPCSGReplicas (1) < pcsgMinAvailable (1)` → FALSE
- **Decision**: PCSG-level minAvailable is NOT breached
- **Action**: Perform PCSG replica-level gang termination on replica 0 ONLY

**Gang Termination on sg-x-0**:
- Deletes `sg-x-0-pc-b` (sets `DeletionTimestamp`)
- Deletes `sg-x-0-pc-c` (sets `DeletionTimestamp`)
- Immediately creates NEW `sg-x-0-pc-b`
- Immediately creates NEW `sg-x-0-pc-c`

---

### ~18:02:25-18:02:28 - The Critical Recreation Window (THE BUG OCCURS)

**What Exists During Recreation**:

**PCSG Replica 0 (`sg-x-0`)** - Has 4 PodCliques:
- **OLD `sg-x-0-pc-b`**: 
  - `DeletionTimestamp` set (terminating) 
  - `MinAvailableBreached=False`
  - Pods terminating
- **OLD `sg-x-0-pc-c`**: 
  - `DeletionTimestamp` set (terminating)
  - `MinAvailableBreached=True` ❌ (still set from before!)
  - Pods terminating
- **NEW `sg-x-0-pc-b`**: 
  - Not terminating
  - Being created
  - Pods starting
- **NEW `sg-x-0-pc-c`**: 
  - Not terminating
  - Being created
  - Pods starting

**PCSG Replica 1 (`sg-x-1`)** - Healthy:
- `sg-x-1-pc-b`: 1/1 pods, healthy ✅
- `sg-x-1-pc-c`: 3/3 pods, healthy ✅

---

### ~18:02:29 - PCSG Status Reconciliation (THE BUG TRIGGERS)

#### Step 1: Calculate `scheduledReplicas`
Code: `computeReplicaStatus()` at line 139-153

**For PCSG Replica 0 (`sg-x-0`)**:
```go
// Line 139: Filter OUT terminating PodCliques
nonTerminatedPCSGPodCliques := lo.Filter(pclqs, func(pclq) {
    return !k8sutils.IsResourceTerminating(pclq.ObjectMeta)
})
// Result: [NEW sg-x-0-pc-b, NEW sg-x-0-pc-c] (2 PodCliques)

// Line 148: Check if we have the expected number
if len(nonTerminatedPCSGPodCliques) != numPCSGCliqueNames {
    return  // isScheduled=false
}
```
- Filters out OLD terminating PodCliques
- Only has 2 NEW PodCliques
- Expected: 2 ✅
- But NEW PodCliques might not have `Scheduled=True` yet
- **Result**: `isScheduled=false` (likely) or `isScheduled=true` (if fast enough)

**For PCSG Replica 1 (`sg-x-1`)**:
- Has 2 healthy non-terminating PodCliques ✅
- Both scheduled ✅
- **Result**: `isScheduled=true` ✅

**`scheduledReplicas` Result**: 
```
scheduledReplicas = 1  (only sg-x-1 is counted as scheduled)
```

#### Step 2: Calculate `minAvailableBreachedReplicas`
Code: `computeMinAvailableBreachedReplicas()` at line 238-241

**For PCSG Replica 0 (`sg-x-0`)**:
```go
// Line 238: Iterate over ALL PodCliques (including terminating!)
for pcsgReplicaIndex, pclqs := range pclqsPerPCSGReplica {
    // pclqs includes: OLD sg-x-0-pc-b (terminating), 
    //                 OLD sg-x-0-pc-c (terminating), 
    //                 NEW sg-x-0-pc-b, 
    //                 NEW sg-x-0-pc-c
    
    // Line 239: Check if ANY PodClique is breached
    isMinAvailableBreached := lo.Reduce(pclqs, func(agg bool, pclq) {
        return agg || k8sutils.IsConditionTrue(pclq.Status.Conditions, 
                                                constants.ConditionTypeMinAvailableBreached)
    }, false)
    // OLD sg-x-0-pc-c STILL has MinAvailableBreached=True!
    // Result: isMinAvailableBreached = true ❌
}
```
- Does NOT filter terminating PodCliques
- OLD terminating `sg-x-0-pc-c` still has `MinAvailableBreached=True`
- **Result**: PCSG Replica 0 is counted as breached ❌

**For PCSG Replica 1 (`sg-x-1`)**:
- All PodCliques have `MinAvailableBreached=False` ✅
- **Result**: NOT counted as breached ✅

**`minAvailableBreachedReplicas` Result**:
```
minAvailableBreachedReplicas = 1  (sg-x-0 counted as breached due to OLD terminating PodClique)
```

#### Step 3: Calculate `availableReplicas`
Code: Line 205

```go
availableReplicas := scheduledReplicas - minAvailableBreachedReplicas
                   = 1 - 1
                   = 0
```

**Logs Show**:
```json
{
  "msg": "🔍 GT4-ROOT-CAUSE: Computing PCSG MinAvailableBreached condition",
  "minAvailable": 1,
  "scheduledReplicas": 1,
  "minAvailableBreachedReplicas": 1,
  "availableReplicas": 0,
  "totalReplicasInMap": 2
}
```

#### Step 4: Compute PCSG MinAvailableBreached Condition
Code: Line 215-225

```go
if availableReplicas < minAvailable {
    // 0 < 1 → TRUE
    return metav1.Condition{
        Type:   constants.ConditionTypeMinAvailableBreached,
        Status: metav1.ConditionTrue,
        ...
    }
}
```

**Result**: PCSG `workload2-0-sg-x` gets `MinAvailableBreached=True` ❌

**Logs Show**:
```json
{
  "msg": "🚨 GT4-ROOT-CAUSE: PCSG will be marked MinAvailableBreached=True - THIS MAY TRIGGER PCS REPLICA DELETION!",
  "pcsg": {"name": "workload2-0-sg-x"},
  "minAvailable": 1,
  "availableReplicas": 0
}
```

---

### ~18:02:29 - PCS Controller: Detects Breached PCSG

**PCS Controller Reconciles**:
- Sees PCSG `workload2-0-sg-x` has `MinAvailableBreached=True`
- Checks grace period: `waitFor=5.358s` (still waiting)
- Requeues for later

**Logs Show**:
```json
{
  "msg": "🔍 GT4-ROOT-CAUSE: Checking PCS replica for gang termination",
  "pcsReplicaIndex": 0,
  "breachedPCSGNames": ["workload2-0-sg-x"],
  "minPCSGWaitFor": "5.358800786s",
  "breachedPCLQNames": []
}
```

---

### ~18:02:35 - PCS Controller: Grace Period Expires

**PCS Controller Reconciles Again**:
- PCSG `workload2-0-sg-x` still has `MinAvailableBreached=True`
- Grace period expired (waitFor ≤ 0)
- **Decision**: Perform PCS replica-level gang termination

**PCS Replica-Level Gang Termination**:
- Deletes ALL PodCliques in PCS replica 0
- This includes ALL PCSG replicas within PCS replica 0:
  - `sg-x-0-pc-b` ❌
  - `sg-x-0-pc-c` ❌
  - **`sg-x-1-pc-b`** ❌ (INCORRECTLY DELETED!)
  - **`sg-x-1-pc-c`** ❌ (INCORRECTLY DELETED!)

**Result**: Healthy `sg-x-1` is deleted even though it was never breached!

---

### 18:02:38 - Test Verification Fails

**Test Checks PodClique Status**:
```
✅ sg-x-0-pc-b: 1/1, MinAvailableBreached=False (recreated at 18:02:32)
✅ sg-x-0-pc-c: 3/3, MinAvailableBreached=False (recreated at 18:02:32)
❌ sg-x-1-pc-b: 0/1, MinAvailableBreached=True (deleted at 18:02:29!)
❌ sg-x-1-pc-c: 0/3, MinAvailableBreached=True (deleted at 18:02:29!)
```

**Test output**:
```
❌ VERIFICATION FAILED: sg-x-1 was incorrectly gang-terminated
```

---

## The Bug in Detail

### Location
`operator/internal/controller/podcliquescalinggroup/reconcilestatus.go`

### The Inconsistency

**Line 139-153: `computeReplicaStatus()` filters terminating PodCliques**:
```go
nonTerminatedPCSGPodCliques := lo.Filter(pclqs, func(pclq grovecorev1alpha1.PodClique, _ int) bool {
    return !k8sutils.IsResourceTerminating(pclq.ObjectMeta)
})
```

**Line 238-241: `computeMinAvailableBreachedReplicas()` does NOT filter terminating PodCliques**:
```go
for pcsgReplicaIndex, pclqs := range pclqsPerPCSGReplica {
    isMinAvailableBreached := lo.Reduce(pclqs, func(agg bool, pclq grovecorev1alpha1.PodClique, _ int) bool {
        return agg || k8sutils.IsConditionTrue(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
    }, false)
    // ... uses ALL pclqs, including terminating ones!
}
```

### Why This Causes the Bug

During PCSG replica recreation:
1. OLD PodCliques are terminating (have `DeletionTimestamp`)
2. NEW PodCliques are being created (not terminating)
3. **Scheduled calculation**: Filters out OLD PodCliques → counts only NEW ones
4. **Breached calculation**: Includes OLD PodCliques → if OLD PodClique is breached, entire replica is breached
5. **Result**: Replica is "breached but not scheduled" → subtracts from `availableReplicas` without contributing to `scheduledReplicas`

### The Formula That Fails

```
availableReplicas = scheduledReplicas - minAvailableBreachedReplicas
```

This formula assumes:
- Every "breached replica" is also a "scheduled replica"
- But during recreation, terminating replicas are breached but NOT scheduled
- This makes `availableReplicas` go negative (clamped to 0)

---

## The Fix

### Option 1: Filter Terminating PodCliques in Both Places (RECOMMENDED)

**Modify `computeMinAvailableBreachedReplicas()` to filter terminating PodCliques**:

```go
func computeMinAvailableBreachedReplicas(logger logr.Logger, pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) int {
    var breachedReplicas int
    for pcsgReplicaIndex, pclqs := range pclqsPerPCSGReplica {
        // FILTER OUT TERMINATING PODCLIQUES (same as computeReplicaStatus does)
        nonTerminatedPCSGPodCliques := lo.Filter(pclqs, func(pclq grovecorev1alpha1.PodClique, _ int) bool {
            return !k8sutils.IsResourceTerminating(pclq.ObjectMeta)
        })
        
        isMinAvailableBreached := lo.Reduce(nonTerminatedPCSGPodCliques, func(agg bool, pclq grovecorev1alpha1.PodClique, _ int) bool {
            return agg || k8sutils.IsConditionTrue(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
        }, false)
        if isMinAvailableBreached {
            breachedReplicas++
        }
        logger.Info("PodCliqueScalingGroup replica has MinAvailableBreached condition set to true", 
            "pcsgReplicaIndex", pcsgReplicaIndex, 
            "isMinAvailableBreached", isMinAvailableBreached)
    }
    return breachedReplicas
}
```

**Why This Works**:
- Both `scheduledReplicas` and `minAvailableBreachedReplicas` now use the same filtering logic
- Terminating PodCliques (being recreated) are ignored in both calculations
- The formula `availableReplicas = scheduledReplicas - minAvailableBreachedReplicas` now works correctly

### Option 2: Change the Formula

Instead of:
```go
availableReplicas := scheduledReplicas - minAvailableBreachedReplicas
```

Use:
```go
// Count replicas that are both scheduled AND not breached
healthyReplicas := countHealthyReplicas(pclqsPerPCSGReplica)
```

This is more complex and less efficient.

---

## Verification

After implementing the fix, the GT4 test should pass with this sequence:

1. ✅ Test deletes pods from `sg-x-0-pc-c`
2. ✅ PCSG replica-level gang termination deletes only `sg-x-0`
3. ✅ During `sg-x-0` recreation, PCSG status ignores terminating PodCliques
4. ✅ `scheduledReplicas=1` (sg-x-1), `minAvailableBreachedReplicas=0`
5. ✅ `availableReplicas=1`, which satisfies `minAvailable=1`
6. ✅ PCSG remains `MinAvailableBreached=False`
7. ✅ PCS controller does NOT trigger PCS replica-level gang termination
8. ✅ `sg-x-1` remains healthy and is NOT deleted
