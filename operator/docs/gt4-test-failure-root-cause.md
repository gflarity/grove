# GT-4 Test Failure: Root Cause Analysis

## Test Details

**Test**: `Test_GT4_GangTerminationMinReplicasPCSGOwned`  
**Location**: `operator/e2e/tests/gang_termination_test.go`  
**Failure Point**: Step 9 - Verification that workload is NOT gang-terminated after deleting all pods from one PCSG replica

## Expected vs Actual Behavior

### Expected Behavior
When all 3 pods from `workload2-0-sg-x-0-pc-c` are deleted:
1. The PCSG controller should gang-terminate only PCSG replica `sg-x-0` (both `pc-b` and `pc-c`)
2. PCSG replica `sg-x-1` should remain healthy
3. The PCSG `workload2-0-sg-x` should have 1 out of 2 replicas available, satisfying `minAvailable: 1`
4. The PCSG should NOT have `MinAvailableBreached=True`
5. The PCS controller should NOT gang-terminate the entire PCS replica 0
6. Only the pods from `sg-x-0` should be recreated; `sg-x-1` and `pc-a` should remain intact

### Actual Behavior
1. ✅ PCSG controller gang-terminates PCSG replica `sg-x-0` (correct)
2. ❌ PCSG shows `scheduledReplicas=0, availableReplicas=0` (incorrect - should be 1)
3. ❌ PCSG sets `MinAvailableBreached=True` (incorrect - should be False)
4. ❌ PCS controller gang-terminates the ENTIRE PCS replica 0, including:
   - `workload2-0-pc-a` (standalone PodClique)
   - ALL PodCliques from PCSG replica `sg-x-1` (healthy replica!)
5. The entire workload enters a recreation loop

## Root Cause

### Issue 1: PCSG `scheduledReplicas` Calculation During Rapid Deletion/Recreation

**File**: `internal/controller/podcliquescalinggroup/reconcilestatus.go`  
**Function**: `computeReplicaStatus()` (lines 107-134)

```go
nonTerminatedPCSGPodCliques := lo.Filter(pclqs, func(pclq grovecorev1alpha1.PodClique, _ int) bool {
    return !k8sutils.IsResourceTerminating(pclq.ObjectMeta)
})
if len(nonTerminatedPCSGPodCliques) != numPCSGCliqueNames {
    // Returns false for isScheduled
    return
}
```

**Problem**: When PCSG replica `sg-x-0` PodCliques are being deleted and recreated:
- During deletion, they have `DeletionTimestamp` set (terminating)
- They're filtered out from the count
- Even though `sg-x-1` is healthy, the reconciliation for `sg-x-0` sets `scheduledReplicas=0`
- Multiple rapid reconciliations all report `scheduledReplicas=0`

**Impact**: The PCSG status shows `scheduledReplicas=0` instead of `1`, leading to incorrect `MinAvailableBreached=True`.

### Issue 2: PCSG Label Architecture and PCS Gang Termination Scope

**File**: `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`  
**Function**: `createPCSReplicaDeleteTask()` (lines 204-227)

```go
client.DeleteAllOf(ctx,
    &grovecorev1alpha1.PodClique{},
    client.InNamespace(pcs.Namespace),
    client.MatchingLabels(
        lo.Assign(
            apicommon.GetDefaultLabelsForPodCliqueSetManagedResources(pcs.Name),
            map[string]string{
                apicommon.LabelPodCliqueSetReplicaIndex: strconv.Itoa(pcsReplicaIndex),
            },
        )))
```

**Problem**: 
- The PCSG `workload2-0-sg-x` belongs to PCS replica 0 (the "0" in the name)
- ALL PodCliques owned by this PCSG have label `LabelPodCliqueSetReplicaIndex: "0"`
- This includes PodCliques from BOTH PCSG replicas:
  - `workload2-0-sg-x-0-pc-b`, `workload2-0-sg-x-0-pc-c` (PCSG replica 0)
  - `workload2-0-sg-x-1-pc-b`, `workload2-0-sg-x-1-pc-c` (PCSG replica 1)
- When the PCS controller deletes all PodCliques with `LabelPodCliqueSetReplicaIndex: "0"`, it deletes PodCliques from ALL PCSG replicas, not just the breached one

**Impact**: Gang termination at the PCS level incorrectly includes healthy PCSG replicas.

### Issue 3: Incorrect PCSG `MinAvailableBreached` Condition

**File**: `internal/controller/podcliquescalinggroup/reconcilestatus.go`  
**Function**: `computeMinAvailableBreachedCondition()` (lines 149-183)

```go
scheduledReplicas := int(pcsg.Status.ScheduledReplicas)
minAvailableBreachedReplicas := computeMinAvailableBreachedReplicas(logger, pclqsPerPCSGReplica)
availableReplicas := scheduledReplicas - minAvailableBreachedReplicas
if availableReplicas < minAvailable {
    return metav1.Condition{
        Type:    constants.ConditionTypeMinAvailableBreached,
        Status:  metav1.ConditionTrue,
        ...
    }
}
```

**Problem**:
- Due to Issue 1, `scheduledReplicas=0`
- `minAvailableBreachedReplicas=1` (only sg-x-0 is breached)
- `availableReplicas = 0 - 1 = -1`
- `-1 < 1` (minAvailable), so condition is set to True
- But the actual state is: 1 replica healthy, 1 replica breached, which SHOULD satisfy `minAvailable: 1`

**Impact**: Triggers PCS-level gang termination when it shouldn't.

## Event Timeline (from Operator Logs)

```
22:26:49 - PCSG MinAvailableBreached condition first set to True
22:27:08.543Z - PCSG controller creates workload2-0-sg-x-1-pc-b (trying to maintain sg-x-1)
22:27:08.575Z - PCS controller deletes ALL PodCliques for PCS replica 0
               Reason: "MinAvailable breached longer than TerminationDelay: 10s"
22:27:08.585Z - PCSG reports: scheduledReplicas=0, availableReplicas=0
22:27:08.636Z - PCSG controller recreates workload2-0-sg-x-1-pc-c
22:27:08.786Z - Newly created workload2-0-sg-x-1-pc-b is deleted (by PCS gang termination)
...continuous recreation and deletion loop...
```

## Test Workload Structure

```
PodCliqueSet: workload2 (1 replica - replica 0)
├── PodClique pc-a: 2 replicas, minAvailable: 1 (PCS-owned)
└── PCSG sg-x: 2 replicas, minAvailable: 1 (Label: LabelPodCliqueSetReplicaIndex: "0")
    ├── Replica sg-x-0: (Label: LabelPodCliqueSetReplicaIndex: "0")
    │   ├── pc-b: 1 pod, minAvailable: 1
    │   └── pc-c: 3 pods, minAvailable: 1
    └── Replica sg-x-1: (Label: LabelPodCliqueSetReplicaIndex: "0") 
        ├── pc-b: 1 pod, minAvailable: 1
        └── pc-c: 3 pods, minAvailable: 1
Total: 2 + (1+3) + (1+3) = 10 pods
```

## Why the Test Fails

The test expects that after deleting all pods from `workload2-0-sg-x-0-pc-c`:
- At least 3 original pods remain (from `workload2-0-pc-a`, `workload2-0-sg-x-1-pc-b`, `workload2-0-sg-x-1-pc-c`)

But the actual behavior is:
- ALL 10 original pods are gang-terminated
- New pods are continuously created and deleted
- Eventually only 2-6 pods remain, all with NEW UIDs
- Verification fails because no original pods exist

## Implications

This bug affects any workload where:
1. A PCSG has `minAvailable` < `replicas` (i.e., can tolerate some replica failures)
2. One PCSG replica breaches minAvailable due to pod deletions
3. The expectation is that only that PCSG replica is gang-terminated, not the entire PCS replica

The current implementation violates the hierarchical gang termination model:
- **PCSG-level**: Should handle gang termination within its own replicas
- **PCS-level**: Should only trigger when PCSG itself violates its minAvailable constraint

Instead, the PCS-level gang termination is triggered even when the PCSG is within its minAvailable threshold.

## Recommended Fixes

### Fix 1: Correct `scheduledReplicas` Calculation
During rapid PodClique deletion/recreation, count PodCliques that are in the process of being created but not yet fully scheduled. Or delay status updates until PodCliques stabilize.

### Fix 2: PCSG `MinAvailableBreached` Condition Logic
Ensure the condition only considers fully scheduled/available PCSG replicas, not those in transient states during gang termination.

### Fix 3: PCS Gang Termination Scope
Reconsider the label architecture and deletion logic so that PCS-level gang termination doesn't inadvertently delete healthy PCSG replicas. Perhaps:
- Use more granular labels to distinguish between PCSG replicas
- Have PCS-level gang termination only delete the PCSG resource itself, not its constituent PodCliques directly
- Rely on PCSG controller to handle cleanup of its own replicas

### Fix 4: Grace Period for PCSG Recreation
Add logic to prevent PCS-level gang termination from triggering while PCSG replicas are being recreated after PCSG-level gang termination. The `terminationStartupGracePeriod` should apply here.

## Additional Context

- **Operator Logs**: See `/tmp/gt4_test_output_with_logs.log` for full operator logs
- **Related Documentation**: 
  - `operator/docs/e2e-gt4-debugging-analysis.md` - Initial analysis
  - `operator/docs/gang-termination-architecture.md` - Gang termination design
- **Test Definition**: `operator/e2e/tests/gang_termination_test.go` (lines 337-491)
- **Workload Config**: `operator/e2e/yaml/workload2.yaml`
