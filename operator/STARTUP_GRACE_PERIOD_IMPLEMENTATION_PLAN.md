# Implementation Plan: Move Startup Grace Period to Breach Detection

## Overview

Move startup grace period logic from gang termination decision points to breach detection points. Resources will only be marked as `MinAvailableBreached=True` after the grace period expires, eliminating the need for grace period checks during gang termination.

## Benefits

1. **Cleaner architecture**: Single responsibility - breach detection vs. termination decision
2. **Better timing**: `gracePeriod + terminationDelay` for early breaches prevents premature cascading termination
3. **Fixes GT2/GT4-Step15**: Stuck unschedulable pods will eventually be marked breached after grace period
4. **Better observability**: `Unknown` status clearly indicates "waiting to determine if this is a real problem"

## Phase 1: Update PodClique Breach Detection

### File: `internal/controller/podclique/reconcilestatus.go`

**Change 1**: Update function signature to accept PCS
```go
// Line 169 - OLD
func mutateMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, numNotReadyPodsWithContainersInError, numPodsStartedButNotReady int)

// Line 169 - NEW
func mutateMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, pcs *grovecorev1alpha1.PodCliqueSet, numNotReadyPodsWithContainersInError, numPodsStartedButNotReady int)
```

**Change 2**: Update function signature for compute function
```go
// Line 177 - OLD
func computeMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, numPodsHavingAtleastOneContainerWithNonZeroExitCode, numPodsStartedButNotReady int) metav1.Condition

// Line 177 - NEW
func computeMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, pcs *grovecorev1alpha1.PodCliqueSet, numPodsHavingAtleastOneContainerWithNonZeroExitCode, numPodsStartedButNotReady int) metav1.Condition
```

**Change 3**: Update caller to pass PCS
```go
// Line 72-74 - OLD
mutateMinAvailableBreachedCondition(pclq,
    len(podCategories[k8sutils.PodHasAtleastOneContainerWithNonZeroExitCode]),
    len(podCategories[k8sutils.PodStartedButNotReady]))

// Line 72-74 - NEW
mutateMinAvailableBreachedCondition(pclq, pcs,
    len(podCategories[k8sutils.PodHasAtleastOneContainerWithNonZeroExitCode]),
    len(podCategories[k8sutils.PodStartedButNotReady]))
```

**Change 4**: Add grace period logic to breach computation
```go
// Line 177-213 - REPLACE computeMinAvailableBreachedCondition function body

func computeMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, pcs *grovecorev1alpha1.PodCliqueSet, numPodsHavingAtleastOneContainerWithNonZeroExitCode, numPodsStartedButNotReady int) metav1.Condition {
    if componentutils.IsPCLQUpdateInProgress(pclq) {
        return metav1.Condition{
            Type:    constants.ConditionTypeMinAvailableBreached,
            Status:  metav1.ConditionUnknown,
            Reason:  constants.ConditionReasonUpdateInProgress,
            Message: "Update is in progress",
        }
    }
    
    minAvailable := int(*pclq.Spec.MinAvailable)
    scheduledReplicas := int(pclq.Status.ScheduledReplicas)
    now := metav1.Now()
    
    readyOrStartingPods := scheduledReplicas - numPodsHavingAtleastOneContainerWithNonZeroExitCode - numPodsStartedButNotReady
    
    if readyOrStartingPods < minAvailable {
        // Check if PodClique is still within startup grace period
        timeSinceCreation := now.Time.Sub(pclq.CreationTimestamp.Time)
        gracePeriod := pcs.Spec.Template.TerminationStartupGracePeriod.Duration
        
        if timeSinceCreation < gracePeriod {
            // Still within grace period - don't mark as breached yet
            return metav1.Condition{
                Type:               constants.ConditionTypeMinAvailableBreached,
                Status:             metav1.ConditionUnknown,
                Reason:             constants.ConditionReasonWithinStartupGracePeriod,
                Message:            fmt.Sprintf("Insufficient ready or starting pods (expected at least: %d, found: %d), but within startup grace period (%v elapsed of %v)", minAvailable, readyOrStartingPods, timeSinceCreation.Round(time.Second), gracePeriod),
                LastTransitionTime: now,
            }
        }
        
        // Past grace period - mark as breached
        return metav1.Condition{
            Type:               constants.ConditionTypeMinAvailableBreached,
            Status:             metav1.ConditionTrue,
            Reason:             constants.ConditionReasonInsufficientReadyPods,
            Message:            fmt.Sprintf("Insufficient ready or starting pods. expected at least: %d, found: %d", minAvailable, readyOrStartingPods),
            LastTransitionTime: now,
        }
    }
    
    return metav1.Condition{
        Type:               constants.ConditionTypeMinAvailableBreached,
        Status:             metav1.ConditionFalse,
        Reason:             constants.ConditionReasonSufficientReadyPods,
        Message:            fmt.Sprintf("Either sufficient ready or starting pods found. expected at least: %d, found: %d", minAvailable, readyOrStartingPods),
        LastTransitionTime: now,
    }
}
```

## Phase 2: Update PCSG Breach Detection

### File: `internal/controller/podcliquescalinggroup/reconcilestatus.go`

**Change 1**: Add grace period logic to `computeMinAvailableBreachedReplicas`

Currently filters out unscheduled PodCliques. Update to filter based on grace period:

```go
// Line 236-268 - REPLACE computeMinAvailableBreachedReplicas function

func computeMinAvailableBreachedReplicas(logger logr.Logger, pcs *grovecorev1alpha1.PodCliqueSet, pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) int {
    var breachedReplicas int
    now := time.Now()
    gracePeriod := pcs.Spec.Template.TerminationStartupGracePeriod.Duration
    
    for pcsgReplicaIndex, pclqs := range pclqsPerPCSGReplica {
        // Filter out terminating PodCliques
        nonTerminatedPCSGPodCliques := lo.Filter(pclqs, func(pclq grovecorev1alpha1.PodClique, _ int) bool {
            return !k8sutils.IsResourceTerminating(pclq.ObjectMeta)
        })
        
        // Check each PodClique for breach status
        isMinAvailableBreached := lo.Reduce(nonTerminatedPCSGPodCliques, func(agg bool, pclq grovecorev1alpha1.PodClique, _ int) bool {
            // Check if the PodClique has MinAvailableBreached=True
            cond := k8sutils.IsConditionTrue(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
            if !cond {
                return agg
            }
            
            // Double-check grace period at PCSG level as well (defense in depth)
            // This protects against edge cases where PodClique status hasn't updated yet
            timeSinceCreation := now.Sub(pclq.CreationTimestamp.Time)
            if timeSinceCreation < gracePeriod {
                // Within grace period, don't count as breached
                logger.Info("DEBUG: PodClique marked breached but within grace period at PCSG level",
                    "pclq", pclq.Name,
                    "timeSinceCreation", timeSinceCreation,
                    "gracePeriod", gracePeriod)
                return agg
            }
            
            return true
        }, false)
        
        if isMinAvailableBreached {
            breachedReplicas++
        }
        logger.Info("PodCliqueScalingGroup replica MinAvailable breach status", 
            "pcsgReplicaIndex", pcsgReplicaIndex, 
            "isMinAvailableBreached", isMinAvailableBreached)
    }
    return breachedReplicas
}
```

**Change 2**: Update function signature
```go
// Line 236 - OLD
func computeMinAvailableBreachedReplicas(logger logr.Logger, pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) int

// Line 236 - NEW
func computeMinAvailableBreachedReplicas(logger logr.Logger, pcs *grovecorev1alpha1.PodCliqueSet, pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) int
```

**Change 3**: Update caller
```go
// Line 204 - OLD
minAvailableBreachedReplicas := computeMinAvailableBreachedReplicas(logger, pclqsPerPCSGReplica)

// Line 204 - NEW
minAvailableBreachedReplicas := computeMinAvailableBreachedReplicas(logger, pcs, pclqsPerPCSGReplica)
```

**Change 4**: Add grace period check to PCSG-level breach condition

Update `computeMinAvailableBreachedCondition` to also respect grace period:

```go
// Line 192-233 - UPDATE computeMinAvailableBreachedCondition function

func computeMinAvailableBreachedCondition(logger logr.Logger, pcs *grovecorev1alpha1.PodCliqueSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup, pclqsPerPCSGReplica map[string][]grovecorev1alpha1.PodClique) metav1.Condition {
    if componentutils.IsPCSGUpdateInProgress(pcsg) {
        return metav1.Condition{
            Type:    constants.ConditionTypeMinAvailableBreached,
            Status:  metav1.ConditionUnknown,
            Reason:  constants.ConditionReasonUpdateInProgress,
            Message: "Update is in progress",
        }
    }
    
    // Check if PCSG is within startup grace period
    now := time.Now()
    timeSinceCreation := now.Sub(pcsg.CreationTimestamp.Time)
    gracePeriod := pcs.Spec.Template.TerminationStartupGracePeriod.Duration
    
    minAvailable := int(*pcsg.Spec.MinAvailable)
    scheduledReplicas := int(pcsg.Status.ScheduledReplicas)
    minAvailableBreachedReplicas := computeMinAvailableBreachedReplicas(logger, pcs, pclqsPerPCSGReplica)
    availableReplicas := scheduledReplicas - minAvailableBreachedReplicas
    
    logger.Info("🔍 Computing PCSG MinAvailableBreached condition",
        "pcsg", client.ObjectKeyFromObject(pcsg),
        "minAvailable", minAvailable,
        "scheduledReplicas", scheduledReplicas,
        "minAvailableBreachedReplicas", minAvailableBreachedReplicas,
        "availableReplicas", availableReplicas,
        "totalReplicasInMap", len(pclqsPerPCSGReplica),
        "timeSinceCreation", timeSinceCreation,
        "gracePeriod", gracePeriod)
    
    if availableReplicas < minAvailable {
        // Check if within grace period
        if timeSinceCreation < gracePeriod {
            return metav1.Condition{
                Type:    constants.ConditionTypeMinAvailableBreached,
                Status:  metav1.ConditionUnknown,
                Reason:  constants.ConditionReasonWithinStartupGracePeriod,
                Message: fmt.Sprintf("Insufficient PodCliqueScalingGroup ready replicas (expected at least: %d, found: %d), but within startup grace period (%v elapsed of %v)", minAvailable, availableReplicas, timeSinceCreation.Round(time.Second), gracePeriod),
            }
        }
        
        logger.Info("🚨 PCSG will be marked MinAvailableBreached=True",
            "pcsg", client.ObjectKeyFromObject(pcsg),
            "minAvailable", minAvailable,
            "availableReplicas", availableReplicas)
        return metav1.Condition{
            Type:    constants.ConditionTypeMinAvailableBreached,
            Status:  metav1.ConditionTrue,
            Reason:  constants.ConditionReasonInsufficientAvailablePCSGReplicas,
            Message: fmt.Sprintf("Insufficient PodCliqueScalingGroup ready replicas, expected at least: %d, found: %d", minAvailable, availableReplicas),
        }
    }
    
    return metav1.Condition{
        Type:    constants.ConditionTypeMinAvailableBreached,
        Status:  metav1.ConditionFalse,
        Reason:  constants.ConditionReasonSufficientAvailablePCSGReplicas,
        Message: fmt.Sprintf("Sufficient PodCliqueScalingGroup ready replicas, expected at least: %d, found: %d", minAvailable, availableReplicas),
    }
}
```

## Phase 3: Add New Condition Reason Constant

### File: `api/common/constants/conditions.go`

Add new constant for the grace period reason:

```go
// Add to existing condition reasons
ConditionReasonWithinStartupGracePeriod = "WithinStartupGracePeriod"
```

## Phase 4: Remove Grace Period Logic from Gang Termination

### File: `internal/controller/common/component/utils/podclique.go`

**Change**: Remove grace period check from `GetMinAvailableBreachedPCLQInfo`

```go
// Line 92-119 - REPLACE function

func GetMinAvailableBreachedPCLQInfo(pclqs []grovecorev1alpha1.PodClique, terminationDelay time.Duration, gracePeriod time.Duration, since time.Time) ([]string, time.Duration) {
    pclqCandidateNames := make([]string, 0, len(pclqs))
    waitForDurations := make([]time.Duration, 0, len(pclqs))
    for _, pclq := range pclqs {
        cond := meta.FindStatusCondition(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
        if cond == nil {
            continue
        }
        if cond.Status == metav1.ConditionTrue {
            // Grace period check removed - now handled at breach detection
            // If condition is True, it means we're already past grace period
            pclqCandidateNames = append(pclqCandidateNames, pclq.Name)
            waitFor := terminationDelay - since.Sub(cond.LastTransitionTime.Time)
            waitForDurations = append(waitForDurations, waitFor)
        }
    }
    if len(pclqCandidateNames) == 0 {
        return nil, 0
    }
    slices.Sort(waitForDurations)
    return pclqCandidateNames, waitForDurations[0]
}
```

**Note**: We keep the `gracePeriod` parameter for now to maintain API compatibility, but it's no longer used.

### File: `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`

**Change**: Remove grace period check from `getMinAvailableBreachedPCSGInfo`

```go
// Line 190-224 - REPLACE function

func getMinAvailableBreachedPCSGInfo(pcsgs []grovecorev1alpha1.PodCliqueScalingGroup, terminationDelay time.Duration, gracePeriod time.Duration, since time.Time) ([]string, time.Duration) {
    pcsgCandidateNames := make([]string, 0, len(pcsgs))
    waitForDurations := make([]time.Duration, 0, len(pcsgs))
    for _, pcsg := range pcsgs {
        cond := meta.FindStatusCondition(pcsg.Status.Conditions, apiconstants.ConditionTypeMinAvailableBreached)
        if cond == nil {
            continue
        }
        if cond.Status == metav1.ConditionTrue {
            // Grace period check removed - now handled at breach detection
            // If condition is True, it means we're already past grace period
            pcsgCandidateNames = append(pcsgCandidateNames, pcsg.Name)
            waitFor := terminationDelay - since.Sub(cond.LastTransitionTime.Time)
            waitForDurations = append(waitForDurations, waitFor)
        }
    }
    if len(waitForDurations) == 0 {
        return pcsgCandidateNames, 0
    }
    slices.Sort(waitForDurations)
    return pcsgCandidateNames, waitForDurations[0]
}
```

## Phase 5: Update Tests

### File: `internal/controller/podcliquescalinggroup/reconcilestatus_test.go`

Update test to pass PCS parameter:

```go
// Line 207 - OLD
condition := computeMinAvailableBreachedCondition(logr.Discard(), pcs, pcsg, tt.pclqsMap)

// Should already have pcs, just verify it's being passed
```

Add new test cases for grace period behavior:

```go
func TestComputeMinAvailableBreachedCondition_WithGracePeriod(t *testing.T) {
    // Test case 1: Breach within grace period -> Unknown
    // Test case 2: Breach after grace period -> True
    // Test case 3: Healthy within grace period -> False
    // Test case 4: Healthy after grace period -> False
}
```

### File: `internal/controller/common/component/utils/podclique_test.go`

Update existing tests in `TestGetMinAvailableBreachedPCLQInfo` to reflect that grace period is no longer checked:

```go
// Line 30+ - Update test expectations
// Tests should now assume that if MinAvailableBreached=True, 
// grace period was already respected at breach detection time
```

## Phase 6: E2E Test Validation

Run all gang termination tests to ensure they pass:

```bash
cd e2e
go test -v -run TestGT ./tests/
```

Expected results:
- ✅ GT1 - PCS full replicas
- ✅ GT2 - PCSG full replicas (previously failing - should now pass!)
- ✅ GT3 - PCS min replicas
- ✅ GT4 - PCSG min replicas (step 9 and step 15 should both pass!)

## Phase 7: Documentation Updates

### File: `api/core/v1alpha1/podcliqueset.go`

Update documentation for `TerminationStartupGracePeriod`:

```go
// Line 154-160 - UPDATE

// TerminationStartupGracePeriod is the grace period after replica creation before MinAvailableBreached can be set to True.
// During this period, resources with pods below minAvailable will have MinAvailableBreached set to Unknown,
// preventing gang termination from being triggered. This allows newly created replicas time to schedule and start.
// After the grace period expires, if pods are still below minAvailable, MinAvailableBreached will be set to True,
// and normal gang termination logic will apply based on TerminationDelay.
// Defaults to 5 minutes.
// +optional
TerminationStartupGracePeriod *metav1.Duration `json:"terminationStartupGracePeriod,omitempty"`
```

## Risk Mitigation

### Risk 1: Timing Changes
**Mitigation**: E2E tests use generous timeouts (2x TerminationDelay). The additional grace period time should not cause test failures.

### Risk 2: Condition Status Unknown Handling
**Mitigation**: Gang termination code already filters for `Status == metav1.ConditionTrue`, so `Unknown` will be correctly ignored.

### Risk 3: Backward Compatibility
**Mitigation**: 
- Function signatures change but are all internal (no public API changes)
- Grace period parameters kept in gang termination functions (unused) for potential future use
- Defaults remain unchanged (5 minutes grace period, 4 hours termination delay)

## Validation Checklist

- [ ] All unit tests pass: `make test`
- [ ] GT1 passes (PCS full replicas)
- [ ] GT2 passes (PCSG full replicas - **currently failing**)
- [ ] GT3 passes (PCS min replicas)
- [ ] GT4 Step 9 passes (PCSG recreation doesn't cascade - **original bug**)
- [ ] GT4 Step 15 passes (both PCSG replicas breached - **currently failing**)
- [ ] No regression in other tests
- [ ] Manual testing: create workload, verify `Unknown` status appears during grace period
- [ ] Manual testing: verify breach transitions to `True` after grace period expires

## Rollout Strategy

1. **Implement in feature branch**
2. **Run full test suite** (unit + e2e)
3. **Review timing behavior** in logs to confirm grace period is working
4. **Merge to main** once all tests pass
5. **Monitor** first deployment for unexpected timing issues

## Timeline Estimate

- Phase 1-2 (Core logic): 2-3 hours
- Phase 3 (Constants): 15 minutes
- Phase 4 (Gang termination cleanup): 1 hour
- Phase 5-6 (Testing): 2-3 hours
- Phase 7 (Documentation): 30 minutes
- **Total: 6-8 hours**

## Expected Behavior Changes

### Timing for Breaches During Grace Period

**Before**: 
- T=0: Resource created
- T=1s: Pods < minAvailable → mark `MinAvailableBreached=True`
- T=1-3s: Gang termination skipped (within grace period)
- T=3s+: Grace period expired, start counting termination delay
- T=3s + terminationDelay: Gang termination executes

**After**:
- T=0: Resource created
- T=1s: Pods < minAvailable → mark `MinAvailableBreached=Unknown` (within grace period)
- T=3s: Grace period expired → mark `MinAvailableBreached=True`
- T=3s + terminationDelay: Gang termination executes

**Result**: Same effective timing! Both wait `gracePeriod + terminationDelay` from creation.

### Key Difference

The new approach is cleaner because:
1. The breach condition accurately reflects "this is a problem that needs action" vs "we're still determining"
2. Gang termination logic is simplified - no grace period checks needed
3. Observability is improved - `Unknown` status clearly indicates the waiting period
4. Fixes the bug where unscheduled PodCliques were filtered out indefinitely

## Notes

- Grace period is checked against `CreationTimestamp`, not when breach first occurred
- This ensures consistent behavior: newly created resources always get the full grace period
- For resources that breach after being healthy, the grace period won't apply (timeSinceCreation will already be > gracePeriod)
