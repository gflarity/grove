# GT-4 Test Failure Summary

## Quick Summary

The test `Test_GT4_GangTerminationMinReplicasPCSGOwned` is failing because **the entire PCS replica is being gang-terminated when it shouldn't be**.

### What Should Happen
- Delete all 3 pods from PCSG replica `sg-x-0`
- Only `sg-x-0` gets gang-terminated (both its `pc-b` and `pc-c` PodCliques)
- PCSG `sg-x` still has replica `sg-x-1` healthy → satisfies `minAvailable: 1`
- PCS replica 0 remains healthy (3+ original pods should remain)

### What Actually Happens
- PCSG controller correctly gang-terminates `sg-x-0`
- But PCSG shows `scheduledReplicas=0` (should be 1)
- PCSG sets `MinAvailableBreached=True` (should be False)
- **PCS controller gang-terminates the ENTIRE PCS replica 0**, including:
  - The standalone `workload2-0-pc-a` PodClique
  - The healthy PCSG replica `sg-x-1` (both `pc-b` and `pc-c`)
- All 10 original pods are replaced with new ones
- Test fails because no original pods remain

## Three Interrelated Bugs

### Bug 1: PCSG `scheduledReplicas` Goes to 0 During Rapid Deletion/Recreation
**Location**: `internal/controller/podcliquescalinggroup/reconcilestatus.go:107-134`

When PCSG replica `sg-x-0` is being gang-terminated:
- Its PodCliques have `DeletionTimestamp` set (terminating state)
- The status reconciliation filters them out: `!k8sutils.IsResourceTerminating()`
- Multiple rapid reconciliations all report `scheduledReplicas=0`
- Even though `sg-x-1` is still healthy!

**Operator Log Evidence**:
```
22:27:08.585Z scheduledReplicas:0, availableReplicas:0
22:27:08.643Z scheduledReplicas:0, availableReplicas:0
22:27:08.698Z scheduledReplicas:0, availableReplicas:0
```

### Bug 2: PCSG `MinAvailableBreached` Incorrectly Set to True
**Location**: `internal/controller/podcliquescalinggroup/reconcilestatus.go:155-176`

The condition logic calculates:
```go
availableReplicas = scheduledReplicas - minAvailableBreachedReplicas
                 = 0 - 1 = -1
if -1 < 1 (minAvailable):
    MinAvailableBreached = True  // ❌ WRONG
```

But the actual state is:
- `sg-x-0`: breached (being recreated)
- `sg-x-1`: healthy
- 1 out of 2 replicas available → SHOULD satisfy `minAvailable: 1`

### Bug 3: PCS Gang Termination Deletes ALL PCSG Replicas
**Location**: `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go:204-227`

The PCS controller deletes all PodCliques with label `LabelPodCliqueSetReplicaIndex: "0"`.

**The Problem**: ALL PodCliques owned by PCSG `workload2-0-sg-x` have this label, including:
- `workload2-0-sg-x-0-*` (PCSG replica 0 - correctly deleted)
- `workload2-0-sg-x-1-*` (PCSG replica 1 - incorrectly deleted!)

**Operator Log Evidence**:
```
22:27:08.575Z "Deleted PCS replica PodCliques" pcsReplicaIndex:0
              Reason: "MinAvailable breached longer than TerminationDelay"
22:27:08.582Z PCSG controller creates workload2-0-sg-x-1-pc-b
22:27:08.786Z Immediately deleted: "Triggering delete of PodClique resources"
              name: "workload2-0-sg-x-1-pc-b"
```

The healthy PCSG replica `sg-x-1` is recreated and immediately deleted in a loop!

## Visual Timeline

```
Time         Event
------------ ----------------------------------------------------------
17:26:28     Delete 1 pod from sg-x-0-pc-c
17:26:48     Delete 2 more pods from sg-x-0-pc-c (all 3 pods gone)
17:26:49     PCSG sets MinAvailableBreached=True ❌ (shouldn't be true)
17:27:08     PCS controller deletes ALL PodCliques for replica 0
             Including: pc-a, sg-x-0-*, and sg-x-1-* ❌
17:27:08     PCSG controller tries to recreate sg-x-1-*
17:27:08     PCS controller immediately deletes them again
17:27:08-38  Continuous recreation/deletion loop
17:27:38     Test fails: 0 original pods remain (expected >= 3)
```

## Why This Matters

This violates the **hierarchical gang termination model**:

1. **PodClique level**: Manages individual pod failures
2. **PCSG level**: Gang-terminates PCSG replicas when they breach minAvailable
3. **PCS level**: Gang-terminates entire PCS replicas only when PCSGs breach THEIR minAvailable

In this case:
- ✅ PCSG replica `sg-x-0` breached → PCSG-level gang termination (correct)
- ❌ PCSG `sg-x` did NOT breach (1 of 2 replicas available, minAvailable=1)
- ❌ But PCS-level gang termination triggered anyway → deleted everything

## Test Evidence

**From test output**:
```
PCSG workload2-0-sg-x: 
  available=0/2         ❌ Should be 1/2
  scheduled=0           ❌ Should be 1 or 2  
  minAvailable=1
  MinAvailableBreached=True  ❌ Should be False
```

**Pod counts over time**:
```
Poll 1: 73 pods, 0 original (massive recreation)
Poll 2: 37 pods, 0 original
Poll 3: 5 pods, 0 original
Poll 4: 3 pods, 0 original (only pc-a + 1 other)
```

All original pods were replaced → gang termination occurred

## Recommendations

### Short-term (Test Perspective)
The test is correctly failing - it found a real bug in the gang termination logic.

### Long-term (Fix Required)
Three issues need to be addressed:

1. **Fix PCSG status calculation** during rapid PodClique deletion/recreation
   - Don't count terminating replicas as "not scheduled"
   - Or delay status updates until replicas stabilize

2. **Fix PCSG MinAvailableBreached logic** to account for transient states
   - Consider replicas that are being recreated after PCSG-level gang termination
   - Respect the `terminationStartupGracePeriod`

3. **Fix PCS gang termination scope** to not delete healthy PCSG replicas
   - Use more granular labels to distinguish PCSG replicas
   - Or have PCS gang termination only delete the PCSG resource, not PodCliques directly
   - Let PCSG controller handle its own cleanup

## Related Files

- **Test**: `operator/e2e/tests/gang_termination_test.go` (lines 337-491)
- **Workload**: `operator/e2e/yaml/workload2.yaml`
- **PCSG Status**: `internal/controller/podcliquescalinggroup/reconcilestatus.go`
- **PCS Gang Termination**: `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`
- **Full Analysis**: `operator/docs/gt4-test-failure-root-cause.md`
- **Operator Logs**: `/tmp/gt4_test_output_with_logs.log`
