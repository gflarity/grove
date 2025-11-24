# GT4 Root Cause FOUND: The Bug in PCSG MinAvailable Calculation

## Executive Summary

**ROOT CAUSE IDENTIFIED**: When PCSG replica `sg-x-0` is gang-terminated at the replica level (correctly, due to `pc-c` breach), the PCSG status reconciliation **incorrectly calculates `availableReplicas`**, causing the entire PCSG to be marked as `MinAvailableBreached=True`. This triggers the PCS controller to perform PCS replica-level gang termination, which deletes ALL PCSG replicas (including the healthy `sg-x-1`).

## The Smoking Gun

At timestamp `2025-11-22T23:02:33.476Z`, the PCSG controller logs:

```json
{
  "msg": "🔍 GT4-ROOT-CAUSE: Computing PCSG MinAvailableBreached condition",
  "pcsg": {"name": "workload2-0-sg-x"},
  "minAvailable": 1,
  "scheduledReplicas": 1,
  "minAvailableBreachedReplicas": 1,
  "availableReplicas": 0,
  "totalReplicasInMap": 2
}

{
  "msg": "🚨 GT4-ROOT-CAUSE: PCSG will be marked MinAvailableBreached=True - THIS MAY TRIGGER PCS REPLICA DELETION!",
  "pcsg": {"name": "workload2-0-sg-x"},
  "minAvailable": 1,
  "availableReplicas": 0
}
```

## The Bug Analysis

### Current (Buggy) Logic

The PCSG status reconciliation computes:

```
availableReplicas = scheduledReplicas - minAvailableBreachedReplicas
                  = 1 - 1
                  = 0

Since availableReplicas (0) < minAvailable (1):
  → Mark PCSG as MinAvailableBreached=True
```

### What's Happening

1. **Initial State**:
   - `sg-x-0`: All 3 pods from `sg-x-0-pc-c` are deleted
   - `sg-x-0-pc-c` gets `MinAvailableBreached=True`
   - PCSG replica 0 is marked with `MinAvailableBreached=True`
   
2. **PCSG Replica-Level Gang Termination** (CORRECT):
   - PCSG controller sees replica 0 is breached
   - Since `minAvailableBreachedPCSGReplicas (1) < pcsgMinAvailable (1)`:
     - Does NOT breach PCSG-level minAvailable
     - Deletes ONLY replica 0 (both `sg-x-0-pc-b` and `sg-x-0-pc-c`)
   
3. **PCSG Status Reconciliation** (BUG HERE):
   - Counts replicas:
     - `sg-x-0`: Scheduled but has `MinAvailableBreached=True` → counted in `scheduledReplicas`, counted in `minAvailableBreachedReplicas`
     - `sg-x-1`: Healthy → counted in `scheduledReplicas`
   - Computes `availableReplicas = 1 - 1 = 0`
   - Since `availableReplicas (0) < minAvailable (1)`: **Marks entire PCSG as `MinAvailableBreached=True`**

4. **PCS Replica-Level Gang Termination** (CASCADING EFFECT):
   - PCS controller sees PCSG `sg-x` has `MinAvailableBreached=True`
   - Waits for termination delay (5.3s remaining)
   - After delay expires: **Deletes ENTIRE PCS replica 0**
   - This includes ALL PCSG replicas (`sg-x-0` AND `sg-x-1`)

## The Root Problem

**The bug is in how `availableReplicas` is calculated for a PCSG.**

The current logic treats a PCSG replica that:
- Is **being gang-terminated at the replica level** (correctly, due to its own breach)
- Is **being recreated** (as part of normal replica-level gang termination)

...as if it's **permanently unavailable**, which incorrectly makes the entire PCSG appear breached.

### The Key Insight

When `sg-x-0` is being recreated due to replica-level gang termination:
- ✅ It SHOULD be counted as `scheduledReplicas=1` (it exists)
- ✅ It SHOULD be counted as `minAvailableBreachedReplicas=1` (it's breached)
- ❌ But this SHOULD NOT make the entire PCSG appear breached!

The issue is that the calculation:
```
availableReplicas = scheduledReplicas - minAvailableBreachedReplicas
```

...is correct for counting "how many replicas are available right now", but it's **NOT correct** for determining "should the entire PCSG be marked as MinAvailableBreached?"

## Why This Causes sg-x-1 to be Deleted

1. **PCSG-level `MinAvailableBreached=True`** is a signal to the PCS controller
2. The PCS controller sees: "One of my components (PCSG `sg-x`) is breached"
3. The PCS controller's gang termination logic says: "If any component is breached, delete the ENTIRE PCS replica"
4. **PCS replica deletion is ALL-or-NOTHING**
5. Therefore, both `sg-x-0` AND `sg-x-1` get deleted

## The Correct Behavior

The PCSG should NOT be marked as `MinAvailableBreached=True` when:
- PCSG replica-level gang termination is handling the breach
- There are still enough healthy replicas to meet `minAvailable`

In this case:
- `sg-x-0`: Being recreated (handled by replica-level gang termination)
- `sg-x-1`: Healthy
- `minAvailable=1` is satisfied by `sg-x-1`
- ✅ PCSG should remain `MinAvailableBreached=False`

## The Fix Strategy

We need to distinguish between:
1. **"PCSG replica is breached and being handled by replica-level gang termination"** (temporary, should not trigger PCSG-level breach)
2. **"PCSG replica is permanently unavailable"** (e.g., insufficient resources, should count toward PCSG-level breach)

### Option 1: Exclude Replicas Being Recreated from availableReplicas Calculation

When computing `availableReplicas` for the PCSG-level `MinAvailableBreached` condition:
- Do NOT count replicas that are currently being gang-terminated at the replica level
- Only count replicas that are:
  - Scheduled
  - NOT currently in the process of being recreated

### Option 2: Use a Different Calculation for PCSG-Level MinAvailableBreached

Instead of:
```
availableReplicas = scheduledReplicas - minAvailableBreachedReplicas
minAvailableBreached = (availableReplicas < minAvailable)
```

Use:
```
// Count replicas that are NOT breached
healthyReplicas = count(replicas where MinAvailableBreached=False)
minAvailableBreached = (healthyReplicas < minAvailable)
```

### Option 3: Add Grace Period for PCSG-Level Breach

When a PCSG replica is being recreated:
- Do not immediately mark the PCSG as breached
- Wait for a grace period (e.g., max termination delay + recreation time)
- Only mark PCSG as breached if the replica doesn't recover within the grace period

## Recommended Fix: Option 2

**Option 2 is the cleanest and most correct solution.**

The current logic conflates two different concepts:
1. **"How many replicas are currently available?"** (for status reporting)
2. **"Is the PCSG breached and requires PCS-level gang termination?"** (for gang termination decision)

These should be calculated differently:
- For status reporting: `availableReplicas = scheduledReplicas - minAvailableBreachedReplicas`
- For gang termination: `minAvailableBreached = (count(healthy replicas) < minAvailable)`

## Next Steps

1. Implement the fix in `reconcilestatus.go:computeMinAvailableBreachedCondition()`
2. Ensure that PCSG-level `MinAvailableBreached` only triggers when:
   - The number of **non-breached** replicas falls below `minAvailable`
   - NOT when replicas are being recreated due to replica-level gang termination
3. Add unit tests for this scenario
4. Verify GT4 test passes

## Code Location

The bug is in:
```
operator/internal/controller/podcliquescalinggroup/reconcilestatus.go
Line ~192: computeMinAvailableBreachedCondition()
```

Specifically, this logic:
```go
availableReplicas := scheduledReplicas - minAvailableBreachedReplicas
isMinAvailableBreached := availableReplicas < minAvailable
```

Should be changed to:
```go
// For PCSG-level MinAvailableBreached, count non-breached replicas
healthyReplicas := 0
for _, replicaStatus := range replicaStatuses {
    if !replicaStatus.MinAvailableBreached {
        healthyReplicas++
    }
}
isMinAvailableBreached := healthyReplicas < minAvailable
```
