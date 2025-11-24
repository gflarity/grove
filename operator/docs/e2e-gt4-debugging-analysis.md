# E2E Test GT-4 Failure Analysis & Instrumentation

## Summary

The E2E test `Test_GT4_GangTerminationMinReplicasPCSGOwned` is failing at step 9, where it expects that deleting all pods from one PCSG replica should NOT trigger gang termination for the entire workload, because the PCSG has `minAvailable: 1` with 2 total replicas.

## What Was Added

I've added comprehensive logging and instrumentation to help debug this issue:

### 1. Enhanced Verification Function

**Function**: `verifyNoGangTerminationWithDetailedLogging()`
- Logs detailed PodClique and PCSG status before verification
- Tracks pod UIDs to distinguish between original and newly created pods
- Provides per-clique pod count breakdowns
- Enhanced logging every 3 polls or on failure

### 2. PodClique and PCSG Status Logging

**Function**: `logPodCliqueAndPCSGStatus()`

This function logs the current state of all PodCliques and PCSGs including:
- **PodCliques**: replicas, readyReplicas, minAvailable, MinAvailableBreached condition status and timing
- **PCSGs**: availableReplicas, scheduledReplicas, minAvailable, MinAvailableBreached condition status, timing, and creation timestamp

The creation timestamp is critical because it's used to determine if a PCSG is within the `terminationStartupGracePeriod` (3s in workload2).

### 3. Detailed Failure Diagnostics

**Function**: `logDetailedFailureDiagnostics()`

On failure, this logs:
- Current pod state grouped by clique
- Whether each pod is ORIGINAL, RECREATED (UID changed), or NEW (not in original list)
- Pod phase, ready status, terminating status
- Re-logs PodClique and PCSG status at time of failure

### 4. Helper Functions

- `getCondition()`: Extracts a specific condition from the status
- `conditionStatus()`: Formats condition status as string
- `conditionLastTransition()`: Formats condition transition time with seconds-ago calculation
- `listPodCliques()`: Lists all PodCliques for the workload
- `listPodCliqueScalingGroups()`: Lists all PCSGs for the workload

## Expected Test Behavior

### Workload Structure (workload2.yaml)

```
PodCliqueSet: workload2 (1 replica)
├── PodClique pc-a: 2 replicas, minAvailable: 1 (PCS-owned)
└── PodCliqueScalingGroup sg-x: 2 replicas, minAvailable: 1
    ├── Replica sg-x-0:
    │   ├── pc-b: 1 pod, minAvailable: 1
    │   └── pc-c: 3 pods, minAvailable: 1
    └── Replica sg-x-1:
        ├── pc-b: 1 pod, minAvailable: 1
        └── pc-c: 3 pods, minAvailable: 1
Total: 2 + (1+3) + (1+3) = 10 pods
```

### Test Scenario (Step 7-9)

**Step 7**: Delete all 3 ready pods from `workload2-0-sg-x-0-pc-c`
- This breaches the podclique `sg-x-0-pc-c` (0/3 ready, needs 1)
- This breaches the PCSG replica `sg-x-0` (one of its constituent cliques is breached)
- **But** the PCSG `sg-x` should NOT be considered breached because:
  - It has `minAvailable: 1` and `replicas: 2`
  - Replica `sg-x-1` is still healthy (has all its pods)
  - Therefore, 1 out of 2 replicas is available, meeting the minAvailable requirement

**Step 8**: Wait 2x TerminationDelay (20 seconds)

**Step 9**: Verify no gang termination occurred
- **Expected**: At least 3 original pods should remain (those from sg-x-1 and pc-a)
- **Actual**: Test is timing out, suggesting gang termination IS occurring

## Likely Root Cause

Based on the gang termination logic in `/internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`:

### The Issue

When all pods from `sg-x-0-pc-c` are deleted, the following sequence likely occurs:

1. **PodClique `sg-x-0-pc-c` gets MinAvailableBreached condition set to True**
   - This is expected - the clique lost all its pods

2. **The PCSG `sg-x` may get recreated with a new creation timestamp**
   - When podcliques are deleted, the PCSG controller may recreate them
   - A new PCSG has a fresh creation timestamp

3. **Grace period check might not be preventing termination**
   - Line 184-188 in `gangterminate.go`:
   ```go
   timeSinceCreation := since.Sub(pcsg.CreationTimestamp.Time)
   if timeSinceCreation < gracePeriod {
       // Still within grace period, don't consider for termination
       continue
   }
   ```
   - If the PCSG was recreated, it should be within the 3s grace period
   - But if enough time has passed, or if the PCSG wasn't actually recreated, the grace period check passes

4. **MinAvailableBreached at PCSG level is incorrectly set**
   - The key question is: Is the PCSG `sg-x` getting its MinAvailableBreached condition set to True?
   - It should NOT, because:
     - `sg-x` has 2 replicas (`sg-x-0` and `sg-x-1`)
     - `sg-x` has `minAvailable: 1`
     - Even though `sg-x-0` is breached, `sg-x-1` is still healthy
     - Therefore, 1 out of 2 replicas is available, meeting minAvailable
   - **But if the PCSG controller is incorrectly setting MinAvailableBreached=True when ANY replica is breached (rather than checking if available replicas < minAvailable), this would trigger gang termination**

5. **Gang termination is triggered for the entire PCS replica**
   - Line 92-98 in `gangterminate.go`:
   ```go
   if (len(breachedPCSGNames) > 0 && minPCSGWaitFor <= 0) ||
       (len(breachedPCLQNames) > 0 && minPCLQWaitFor <= 0) {
       // terminate all PodCliques for this PCS replica index
       ...
   }
   ```
   - If the PCSG has MinAvailableBreached=True and the grace period + termination delay have passed, all podcliques for the PCS replica (index 0) are deleted

## What to Look For in Test Output

When you run the test with the new instrumentation, look for:

1. **PCSG Status at Step 9**:
   ```
   📊 Checking PodClique and PCSG status:
     PCSG workload2-0-sg-x: available=?/2, scheduled=?, minAvailable=1, MinAvailableBreached=? (since=?), created=...
   ```
   - Is `MinAvailableBreached=True` when it should be `False`?
   - Is `available < 1` when `sg-x-1` should still be healthy?
   - When was the PCSG created? Is it within the 3s grace period?

2. **Per-clique pod counts**:
   ```
     Per-clique pod counts:
       workload2-0-sg-x-0-pc-b: ? pods
       workload2-0-sg-x-0-pc-c: ? pods
       workload2-0-sg-x-1-pc-b: ? pods
       workload2-0-sg-x-1-pc-c: ? pods
       workload2-0-pc-a: ? pods
   ```
   - Are the `sg-x-1` pods still present with original UIDs?
   - Are new pods being created for `sg-x-0` but then gang-terminated?

3. **Timing information**:
   - How long ago was the MinAvailableBreached condition set?
   - Is it longer than TerminationDelay (10s)?
   - Is it longer than TerminationStartupGracePeriod (3s)?

4. **Pod recreation patterns**:
   - Are we seeing pods marked as [RECREATED - UID changed]?
   - This would indicate gang termination is happening

## Hypothesis

**Most likely**: The PCSG controller is incorrectly setting `MinAvailableBreached=True` at the PCSG level when any single replica is breached, rather than checking if the number of available replicas is less than minAvailable.

**Alternative**: The PCSG might be getting recreated after the podcliques are deleted, resetting the creation timestamp, but the recreation timing combined with the termination delay is causing the grace period check to be insufficient.

## Next Steps (Don't Fix - Just Document)

The instrumentation will help identify:
1. Whether the PCSG MinAvailableBreached condition is incorrectly set
2. Whether the grace period logic is working correctly
3. The exact timing of when gang termination is triggered
4. Which pods are being recreated vs. remaining from the original set

Once you run the test, the detailed logs will reveal exactly what's happening and confirm or refute this hypothesis.
