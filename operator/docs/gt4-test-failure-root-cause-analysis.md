# GT4 Test Failure: Root Cause Analysis

## Summary
When PCSG replica `sg-x-0` is gang-terminated (after deleting all pods from `sg-x-0-pc-c`), **BOTH PCSG replicas (`sg-x-0` AND `sg-x-1`) get gang-terminated**, causing the entire workload to be gang-terminated. This violates the test's expectation that only the breached replica should be gang-terminated while the healthy replica (`sg-x-1`) should remain running.

## Evidence from Test Logs

### Timeline (from test run at 17:43:47)

**After deleting all pods from `sg-x-0-pc-c`:**

```
PodClique workload2-0-pc-a:       replicas=2/2, MinAvailableBreached=False
PodClique workload2-0-sg-x-0-pc-b: replicas=1/1, MinAvailableBreached=False (recreated)
PodClique workload2-0-sg-x-0-pc-c: replicas=3/3, MinAvailableBreached=False (recreated)
PodClique workload2-0-sg-x-1-pc-b: replicas=0/1, MinAvailableBreached=True  ⚠️
PodClique workload2-0-sg-x-1-pc-c: replicas=0/3, MinAvailableBreached=True  ⚠️
```

**PCSG Status:**
```
PCSG workload2-0-sg-x: available=0/2, scheduled=0, minAvailable=1, MinAvailableBreached=True
```

**Key observations:**
1. ✅ `sg-x-0` was successfully recreated (both `pc-b` and `pc-c` are healthy)
2. ❌ `sg-x-1` was ALSO gang-terminated (both `pc-b` and `pc-c` have 0 replicas and MinAvailableBreached=True)
3. ❌ The PCSG reports `scheduledReplicas=0` because BOTH replicas are not scheduled
4. ❌ This triggers workload-level gang termination

## Root Cause

### Bug Description

When a single PCSG replica (`sg-x-0`) breaches minAvailable and triggers gang termination:
- The breached replica's PodCliques are deleted
- **BUT**: The OTHER healthy replica (`sg-x-1`) is ALSO being gang-terminated
- This causes the entire PCSG to report `scheduledReplicas=0`
- Which triggers workload-level gang termination (since PCSG minAvailable=1 and scheduledReplicas=0)

### Why `scheduledReplicas` Goes to 0

From operator debug logs at `22:43:41.950Z`:

**Replica 0 (sg-x-0):**
```json
{
  "pcsgReplicaIndex": "0",
  "pclqName": "workload2-0-sg-x-0-pc-c",
  "readyReplicas": 3,
  "replicas": 3,
  "scheduledCondition": true
}
{
  "pcsgReplicaIndex": "0",
  "isScheduled": true,
  "isAvailable": true
}
```
✅ Replica 0 is healthy and scheduled.

**Replica 1 (sg-x-1):**
```json
{
  "pcsgReplicaIndex": "1",
  "pclqName": "workload2-0-sg-x-1-pc-b",
  "readyReplicas": 0,
  "replicas": 1,
  "scheduledCondition": false,
  "isTerminating": false,
  "deletionTimestamp": "<nil>"
}
{
  "pcsgReplicaIndex": "1",
  "isScheduled": false,
  "isAvailable": false
}
```
❌ Replica 1 is NOT terminating, but has `scheduledCondition=false` and `readyReplicas=0`.

**Result:**
```json
{
  "scheduledReplicas": 1,
  "availableReplicas": 1
}
```

Later, when `sg-x-0` gets gang-terminated, its PodCliques become terminating, and then:
- Replica 0: terminating → filtered out → `isScheduled=false`
- Replica 1: still not scheduled → `isScheduled=false`
- **Both replicas unscheduled → `scheduledReplicas=0`**

## The Real Question: Why Was `sg-x-1` Gang-Terminated?

The critical question is: **What caused replica `sg-x-1`'s pods to be deleted when only replica `sg-x-0` breached minAvailable?**

### Hypothesis 1: PCSG-level gang termination incorrectly terminates all replicas

When a single PCSG replica breaches minAvailable:
1. The PodClique gang termination logic correctly deletes only `sg-x-0`'s pods
2. BUT: There may be PCSG-level logic that sees `sg-x-0` is being recreated
3. And incorrectly triggers gang termination of ALL PCSG replicas (including healthy `sg-x-1`)

### Hypothesis 2: Rapid status reconciliation race condition

During rapid reconciliation when `sg-x-0` is being deleted/recreated:
1. The PCSG status reconciliation runs multiple times
2. At some point, it sees `sg-x-0` terminating → `scheduledReplicas` temporarily drops
3. This causes PCSG MinAvailableBreached=True
4. Which triggers gang termination of the remaining replica `sg-x-1`

## The Cascading Failure

```
Step 1: Delete all pods from sg-x-0-pc-c
   ↓
Step 2: sg-x-0 PodClique breaches minAvailable → gang terminates sg-x-0 replica
   ↓
Step 3: sg-x-0 PodCliques get terminating → start being recreated
   ↓
Step 4: During recreation, sg-x-1 ALSO gets gang-terminated (BUG!)
   ↓  
Step 5: BOTH sg-x-0 (terminating) and sg-x-1 (gang-terminated) are not scheduled
   ↓
Step 6: PCSG reports scheduledReplicas=0
   ↓
Step 7: PCSG MinAvailableBreached=True (scheduledReplicas=0 < minAvailable=1)
   ↓
Step 8: Workload-level gang termination triggered!
```

## Code Location to Investigate

The issue likely originates in the gang termination logic that operates at the PCSG level:

1. **PodCliqueScalingGroup reconciliation** (`internal/controller/podcliquescalinggroup/reconcilestatus.go`)
   - Line 107-134: `computeReplicaStatus` filters terminating PodCliques
   - This causes rapid `scheduledReplicas` fluctuations during recreation

2. **Gang termination logic** (likely in PodCliqueSet or PodGang controller)
   - Need to find where the decision is made to gang-terminate PCSG replicas
   - Check if it's incorrectly terminating ALL replicas when only ONE breaches minAvailable

3. **Possible fix locations:**
   - The PCSG status should NOT count terminating replicas as "breached" if they're being recreated
   - OR: The gang termination logic should only terminate the specific breached replica, not all replicas
   - OR: Add grace period/debouncing to PCSG status calculation during rapid recreation

## Next Steps

1. ✅ Add detailed logging (completed) to understand the sequence of events
2. 🔍 Trace gang termination logic to find where `sg-x-1` is incorrectly terminated
3. 🔍 Check if PCSG MinAvailableBreached condition is triggering too aggressively during rapid reconciliation
4. 🔧 Implement fix to prevent healthy PCSG replicas from being gang-terminated when only one replica breaches

## Key Insight

The original bug description was correct about filtering terminating PodCliques causing `scheduledReplicas=0`, but that's only HALF the story. The REAL bug is:

> **When one PCSG replica is gang-terminated, the system incorrectly gang-terminates ALL replicas in that PCSG, instead of just the breached replica.**

This causes a cascading failure where:
1. Breached replica terminates (correct)
2. Healthy replica also terminates (BUG!)
3. All replicas unscheduled → PCSG breached
4. Workload terminates (incorrect consequence of the bug)
