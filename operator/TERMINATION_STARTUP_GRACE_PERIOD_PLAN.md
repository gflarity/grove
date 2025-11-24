# Implementation Plan: terminationStartupGracePeriod

## Overview

Add a `terminationStartupGracePeriod` parameter to PodCliqueSet that prevents gang termination during the initial startup period of replicas. This replaces the current "scheduled < minAvailable → breach = false" logic with a cleaner time-based approach.

**Key Insight**: 
- The `MinAvailableBreached` condition should reflect the **actual current state** of availability
- The grace period check should only happen in the **gang termination decision logic**
- This eliminates the need for special startup protection logic in reconcilestatus

**Estimated Effort:** 12-16 hours (1.5-2 days)

---

## Current Problem

The existing code has special logic in reconcilestatus to prevent premature gang termination during startup:

```go
// In PodClique reconcilestatus
if scheduledReplicas < minAvailable {
    return metav1.Condition{
        Type:    constants.ConditionTypeMinAvailableBreached,
        Status:  metav1.ConditionFalse,  // Force false to prevent termination
        Reason:  constants.ConditionReasonInsufficientScheduledPods,
        Message: "Insufficient scheduled pods...",
    }
}
```

This is confusing because:
1. It makes `MinAvailableBreached = False` even when availability IS breached
2. The condition doesn't reflect the actual state during startup
3. It mixes state reporting with policy enforcement

---

## Proposed Solution

### Two-Level Approach

**Level 1: State Reporting (reconcilestatus)**
- Simplify: Remove the "scheduledReplicas < minAvailable" protection logic
- Report actual state: If available < minAvailable → `MinAvailableBreached = True`
- No grace period checks here

**Level 2: Policy Enforcement (gang termination)**
- Add grace period check: `if (now - creationTimestamp) < gracePeriod → skip termination`
- Only consider resources for termination if they're past their grace period
- Check terminationDelay as before

### Logic Flow

```
Current:
  scheduledReplicas < minAvailable → MinAvailableBreached = False (lie to prevent termination)
  availableReplicas < minAvailable → MinAvailableBreached = True
  MinAvailableBreached = True → wait terminationDelay → terminate

New:
  availableReplicas < minAvailable → MinAvailableBreached = True (always truth)
  
  Gang Termination Logic:
    MinAvailableBreached = True
      → Check: (now - creationTimestamp) > gracePeriod?
        → NO: Skip (within startup grace period)
        → YES: Check terminationDelay → terminate if expired
```

---

## Phase 1: API Changes (2-3 hours)

- [x] **Phase 1 Complete**

### 1.1 Add Field to PodCliqueSetTemplateSpec

- [x] Add terminationStartupGracePeriod field to PodCliqueSetTemplateSpec

**File:** `api/core/v1alpha1/podcliqueset.go`

**Location:** After line 153 (after `TerminationDelay` field)

**Changes:**
```go
// TerminationStartupGracePeriod is the grace period after replica creation before gang termination can be triggered.
// During this period, MinAvailable breaches will not trigger gang termination, allowing time for pods to schedule and start.
// This prevents premature termination of newly created replicas that haven't had a chance to become healthy yet.
// After the grace period expires, normal gang termination logic applies based on TerminationDelay.
// Defaults to 5 minutes.
// +optional
TerminationStartupGracePeriod *metav1.Duration `json:"terminationStartupGracePeriod,omitempty"`
```

### 1.2 Add Defaulting Logic

- [x] Add defaulting for terminationStartupGracePeriod

**File:** `internal/webhook/admission/pcs/defaulting/podcliqueset.go` (or similar)

**Changes:**
- Add defaulting for `terminationStartupGracePeriod` if not set
- Default value: 5 minutes (`metav1.Duration{Duration: 5 * time.Minute}`)
- Similar pattern to existing `terminationDelay` defaulting

### 1.3 Add Validation Logic

- [x] Add validation for terminationStartupGracePeriod

**File:** `internal/webhook/admission/pcs/validation/podcliqueset.go`

**Location:** After `validateTerminationDelay()` function (around line 244)

**Changes:**
```go
func validateTerminationStartupGracePeriod(gracePeriod *metav1.Duration) error {
    if gracePeriod == nil {
        return nil // Optional field, defaulting webhook will set it
    }
    if gracePeriod.Duration < 0 {
        return fmt.Errorf("terminationStartupGracePeriod must be non-negative, got: %v", gracePeriod.Duration)
    }
    return nil
}
```

Update `validatePodCliqueSetTemplateSpec()` to call the new validation function.

### 1.4 Update CRDs

- [x] Regenerate CRDs with new field

**Files:**
- `api/core/v1alpha1/crds/grove.io_podcliquesets.yaml`
- `charts/crds/grove.io_podcliquesets.yaml`

**Action:**
- Run `make generate` to regenerate CRDs
- Verify the new field appears with proper OpenAPI schema

---

## Phase 2: Simplify reconcilestatus (2-3 hours)

- [x] **Phase 2 Complete**

### 2.1 Simplify PodClique MinAvailableBreached Computation

- [x] Remove scheduledReplicas < minAvailable check from PodClique reconcilestatus

**File:** `internal/controller/podclique/reconcilestatus.go`

**Location:** `computeMinAvailableBreachedCondition()` function (lines 177-225)

**Current Logic (lines 192-202):**
```go
if scheduledReplicas < minAvailable {
    return metav1.Condition{
        Type:               constants.ConditionTypeMinAvailableBreached,
        Status:             metav1.ConditionFalse,
        Reason:             constants.ConditionReasonInsufficientScheduledPods,
        Message:            fmt.Sprintf("Insufficient scheduled pods. expected at least: %d, found: %d", minAvailable, scheduledReplicas),
        LastTransitionTime: now,
    }
}
```

**New Logic (REMOVE the above block):**
- Remove lines 192-202 entirely
- The function will now simply compute breach based on `readyOrStartingPods < minAvailable`
- This makes the condition accurately reflect the actual state

**Justification:**
The grace period in gang termination logic now handles startup protection, so we don't need this special case that makes the condition lie about the actual state.

### 2.2 Simplify PCSG MinAvailableBreached Computation

- [x] Remove scheduledReplicas < minAvailable check from PCSG reconcilestatus

**File:** `internal/controller/podcliquescalinggroup/reconcilestatus.go`

**Location:** `computeMinAvailableBreachedCondition()` function (lines 155-191)

**Current Logic (lines 167-174):**
```go
if scheduledReplicas < minAvailable {
    return metav1.Condition{
        Type:    constants.ConditionTypeMinAvailableBreached,
        Status:  metav1.ConditionFalse,
        Reason:  constants.ConditionReasonInsufficientScheduledPCSGReplicas,
        Message: fmt.Sprintf("Insufficient scheduled replicas. expected at least: %d, found: %d", minAvailable, scheduledReplicas),
    }
}
```

**New Logic (REMOVE the above block):**
- Remove lines 167-174 entirely
- The function will now simply compute breach based on `availableReplicas < minAvailable`
- `availableReplicas = scheduledReplicas - minAvailableBreachedReplicas`

**Justification:**
Same as PodClique - let the condition reflect actual state, use grace period for termination policy.

---

## Phase 3: Update Gang Termination Utilities (4-5 hours)

- [x] **Phase 3 Complete**

### 3.1 Update GetMinAvailableBreachedPCLQInfo

- [x] Add gracePeriod parameter and filtering logic to GetMinAvailableBreachedPCLQInfo

**File:** `internal/controller/common/component/utils/podclique.go`

**Current Signature (line 91):**
```go
func GetMinAvailableBreachedPCLQInfo(pclqs []grovecorev1alpha1.PodClique, terminationDelay time.Duration, since time.Time) ([]string, time.Duration)
```

**New Signature:**
```go
func GetMinAvailableBreachedPCLQInfo(pclqs []grovecorev1alpha1.PodClique, terminationDelay time.Duration, gracePeriod time.Duration, since time.Time) ([]string, time.Duration)
```

**New Logic (lines 91-110):**
```go
func GetMinAvailableBreachedPCLQInfo(pclqs []grovecorev1alpha1.PodClique, terminationDelay time.Duration, gracePeriod time.Duration, since time.Time) ([]string, time.Duration) {
    pclqCandidateNames := make([]string, 0, len(pclqs))
    waitForDurations := make([]time.Duration, 0, len(pclqs))
    
    for _, pclq := range pclqs {
        cond := meta.FindStatusCondition(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
        if cond == nil || cond.Status != metav1.ConditionTrue {
            continue
        }
        
        // Check if PodClique is still within startup grace period
        timeSinceCreation := since.Sub(pclq.CreationTimestamp.Time)
        if timeSinceCreation < gracePeriod {
            // Still within grace period, don't consider for termination
            continue
        }
        
        // Past grace period, check termination delay
        pclqCandidateNames = append(pclqCandidateNames, pclq.Name)
        waitFor := terminationDelay - since.Sub(cond.LastTransitionTime.Time)
        waitForDurations = append(waitForDurations, waitFor)
    }
    
    if len(pclqCandidateNames) == 0 {
        return nil, 0
    }
    slices.Sort(waitForDurations)
    return pclqCandidateNames, waitForDurations[0]
}
```

### 3.2 Update getMinAvailableBreachedPCSGInfo

- [x] Add gracePeriod parameter and filtering logic to getMinAvailableBreachedPCSGInfo

**File:** `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`

**Current Signature (line 168):**
```go
func getMinAvailableBreachedPCSGInfo(pcsgs []grovecorev1alpha1.PodCliqueScalingGroup, terminationDelay time.Duration, since time.Time) ([]string, time.Duration)
```

**New Signature:**
```go
func getMinAvailableBreachedPCSGInfo(pcsgs []grovecorev1alpha1.PodCliqueScalingGroup, terminationDelay time.Duration, gracePeriod time.Duration, since time.Time) ([]string, time.Duration)
```

**New Logic (lines 168-187):**
```go
func getMinAvailableBreachedPCSGInfo(pcsgs []grovecorev1alpha1.PodCliqueScalingGroup, terminationDelay time.Duration, gracePeriod time.Duration, since time.Time) ([]string, time.Duration) {
    pcsgCandidateNames := make([]string, 0, len(pcsgs))
    waitForDurations := make([]time.Duration, 0, len(pcsgs))
    
    for _, pcsg := range pcsgs {
        cond := meta.FindStatusCondition(pcsg.Status.Conditions, apiconstants.ConditionTypeMinAvailableBreached)
        if cond == nil || cond.Status != metav1.ConditionTrue {
            continue
        }
        
        // Check if PCSG is still within startup grace period
        timeSinceCreation := since.Sub(pcsg.CreationTimestamp.Time)
        if timeSinceCreation < gracePeriod {
            // Still within grace period, don't consider for termination
            continue
        }
        
        // Past grace period, check termination delay
        pcsgCandidateNames = append(pcsgCandidateNames, pcsg.Name)
        waitFor := terminationDelay - since.Sub(cond.LastTransitionTime.Time)
        waitForDurations = append(waitForDurations, waitFor)
    }
    
    if len(waitForDurations) == 0 {
        return pcsgCandidateNames, 0
    }
    slices.Sort(waitForDurations)
    return pcsgCandidateNames, waitForDurations[0]
}
```

### 3.3 Update Callers of GetMinAvailableBreachedPCLQInfo

- [x] Update caller in gangterminate.go (PCS level)
- [x] Update caller in sync.go (PCSG level)

**File 1:** `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`

**Location:** Line 146

**Before:**
```go
breachedPCLQNames, minWaitFor = componentutils.GetMinAvailableBreachedPCLQInfo(pclqs, pcs.Spec.Template.TerminationDelay.Duration, since)
```

**After:**
```go
breachedPCLQNames, minWaitFor = componentutils.GetMinAvailableBreachedPCLQInfo(
    pclqs, 
    pcs.Spec.Template.TerminationDelay.Duration,
    pcs.Spec.Template.TerminationStartupGracePeriod.Duration,
    since)
```

**File 2:** `internal/controller/podcliquescalinggroup/components/podclique/sync.go`

**Location:** Line 237

**Before:**
```go
pclqNames, minWaitFor := componentutils.GetMinAvailableBreachedPCLQInfo(pclqs, terminationDelay, now)
```

**After:**
```go
gracePeriod := sc.pcs.Spec.Template.TerminationStartupGracePeriod.Duration
pclqNames, minWaitFor := componentutils.GetMinAvailableBreachedPCLQInfo(pclqs, terminationDelay, gracePeriod, now)
```

**Note:** Verify that `sc.pcs` is available in the sync context. If not, it may need to be passed through.

### 3.4 Update Caller of getMinAvailableBreachedPCSGInfo

- [x] Update getMinAvailableBreachedPCSGs method signature and caller

**File:** `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`

**Location:** Line 80 (in `getPCSReplicaDeletionWork` function)

**Before:**
```go
breachedPCSGNames, minPCSGWaitFor, err := r.getMinAvailableBreachedPCSGs(ctx, pcsObjectKey, pcsReplicaIndex, terminationDelay, now)
```

The `getMinAvailableBreachedPCSGs` method (line 108) calls `getMinAvailableBreachedPCSGInfo` at line 122:

**Before:**
```go
breachedPCSGNames, minWaitFor := getMinAvailableBreachedPCSGInfo(pcsgList.Items, terminationDelay, since)
```

**After:**
```go
breachedPCSGNames, minWaitFor := getMinAvailableBreachedPCSGInfo(
    pcsgList.Items, 
    terminationDelay,
    gracePeriod,  // Need to pass this from caller
    since)
```

**Update the method signature:**

Line 108:
```go
func (r _resource) getMinAvailableBreachedPCSGs(ctx context.Context, pcsObjKey client.ObjectKey, pcsReplicaIndex int, terminationDelay time.Duration, gracePeriod time.Duration, since time.Time) ([]string, time.Duration, error)
```

And update the caller at line 80:
```go
gracePeriod := pcs.Spec.Template.TerminationStartupGracePeriod.Duration
breachedPCSGNames, minPCSGWaitFor, err := r.getMinAvailableBreachedPCSGs(ctx, pcsObjectKey, pcsReplicaIndex, terminationDelay, gracePeriod, now)
```

---

## Phase 4: Constants Updates (30 minutes)

- [x] **Phase 4 Complete**

### 4.1 Review and Clean Up Condition Reasons

- [x] Review condition reasons and clean up if needed

**File:** `api/common/constants/constants.go`

**Review:**
- The existing condition reasons should be sufficient
- `ConditionReasonInsufficientScheduledPods` and `ConditionReasonInsufficientScheduledPCSGReplicas` may no longer be used after simplification
- Consider whether to remove them or leave them for backward compatibility

**No new constants needed** - the grace period check is purely in the termination logic and doesn't require new condition reasons.

---

## Phase 5: Testing & Documentation (3-5 hours)

- [x] **Phase 5 Complete**

### 5.1 Unit Test Updates

- [x] Update reconcilestatus unit tests
- [x] Update gang termination utility tests

**Files Updated:**
- `internal/controller/podcliquescalinggroup/reconcilestatus_test.go` ✅
- `internal/controller/common/component/utils/podclique_test.go` ✅ (created new test file)

**Changes Made:**
1. **reconcilestatus tests:**
   - Updated PCSG test case "insufficient scheduled" to expect `MinAvailableBreached = True` when availability is breached
   - Added breached PodClique to test case to match new logic

2. **gang termination utility tests:**
   - Created comprehensive test suite for `GetMinAvailableBreachedPCLQInfo` with grace period filtering
   - Test cases cover:
     - PodClique created < gracePeriod ago with breach → Not returned ✅
     - PodClique created > gracePeriod ago with breach → Returned ✅
     - Multiple replicas with mixed grace period status ✅
     - Zero grace period behavior ✅
     - Boundary conditions ✅

### 5.2 E2E Test Updates

- [x] Update e2e gang termination tests
- [x] Add grace period test cases
- [x] Update test workload YAMLs

**Files Updated:** `e2e/yaml/workload*.yaml` (all 6 workload files)

**Changes Made:**
- Added `terminationStartupGracePeriod: 3s` to all test workloads for faster test execution
- The 3s grace period is short enough to allow gang termination tests to complete quickly
- This is intentionally shorter than the 5m production default to speed up e2e tests
- Pods typically become ready within 5s, so 3s grace period allows tests to verify gang termination triggers after TerminationDelay (10s) expires

### 5.3 Update Documentation

- [x] Update gang-termination-architecture.md
- [x] Update sample YAMLs with terminationStartupGracePeriod

**File:** `docs/gang-termination-architecture.md` ✅

**Changes Made:**
- Added comprehensive `terminationStartupGracePeriod` section to Key Timing Concepts
- Updated MinAvailable Breach definition to include grace period logic
- Updated both Level 1 and Level 2 decision logic explanations
- Added detailed timeline example showing grace period in action (with 5m grace + 10s delay)
- Added new section "Why TerminationStartupGracePeriod?" explaining:
  - Purpose and benefits
  - Default value rationale
  - Per-resource application
  - Relationship to TerminationDelay with timeline
  - Recommended values for different scenarios
- Clarified that MinAvailableBreached condition now reflects actual state (not artificially False during startup)

**Files Updated:** All sample YAMLs ✅
- `samples/simple/simple1.yaml`
- `samples/simple/simple2-explicit-startup-order.yaml`
- `samples/simple/simple3-explicit-startup-order.yaml`
- `samples/user-guide/concept-overview/single-node-disaggregated.yaml`
- `samples/user-guide/concept-overview/single-node-aggregated.yaml`
- `samples/user-guide/concept-overview/multi-node-disaggregated.yaml`
- `samples/user-guide/concept-overview/multi-node-aggregated.yaml`
- `samples/user-guide/concept-overview/complete-inference-pipeline.yaml`

**Changes Made:**
- Added `terminationDelay` and `terminationStartupGracePeriod` fields to all samples
- Added helpful inline comments explaining purpose and usage
- Used consistent values: `terminationDelay: 4m`, `terminationStartupGracePeriod: 5m`

---

## Implementation Order

1. [ ] **Phase 4:** Constants review (quick, no changes expected)
2. [ ] **Phase 1:** API changes (foundation)
3. [ ] **Phase 2:** Simplify reconcilestatus (remove old startup protection)
4. [ ] **Phase 3:** Update gang termination utilities (add grace period checks)
5. [ ] **Phase 5:** Testing and documentation

---

## Key Design Decisions

### 1. Why Not Check Grace Period in reconcilestatus?

**Decision:** Only check grace period in gang termination logic, not in reconcilestatus.

**Rationale:**
- `MinAvailableBreached` should reflect actual current state, not policy
- Separation of concerns: state reporting vs. policy enforcement
- Easier to test and reason about
- Grace period is a termination policy, not a state attribute

### 2. Grace Period vs Termination Delay

**Grace Period:**
- Purpose: Prevent false positives during initial startup
- Starts at: Resource creation time
- Applies to: Newly created replicas

**Termination Delay:**
- Purpose: Allow time for transient failures to recover
- Starts at: When MinAvailableBreached transitions to True
- Applies to: All replicas, regardless of age

**Combined Timeline:**
```
T=0: Replica created
T=0-5min: Grace period (no termination possible even if breached)
T=5min: Grace period expires
T=5min: Breach occurs (MinAvailableBreached = True)
T=5min-9min: Termination delay (waiting for recovery)
T=9min: Termination occurs (if still breached)
```

### 3. Default Grace Period Value

**Decision:** 5 minutes

**Rationale:**
- Long enough for typical pod scheduling + startup (1-3 minutes)
- Short enough to detect real problems quickly
- Can be overridden per workload based on startup characteristics

### 4. Grace Period Per Level

**Decision:** Single grace period at PodCliqueSet level applies to all resources.

**Rationale:**
- Simpler to configure and understand
- PodCliques, PCSGs, and PCS replicas all created at similar times
- If needed, can be extended to per-level grace periods later

**Application:**
- **PodClique:** Check `pclq.CreationTimestamp`
- **PCSG:** Check `pcsg.CreationTimestamp`
- **PCS Replica:** Implicitly handled via PodClique and PCSG checks

---

## Success Criteria

- [x] All unit tests pass (updated tests for new behavior)
- [x] All e2e tests pass (workload YAMLs updated with grace period)
- [x] Gang termination works correctly at all levels (Phase 3 implementation complete)
- [x] No premature termination during initial startup (grace period filtering implemented)
- [x] After grace period, normal termination logic applies (verified in implementation)
- [x] `MinAvailableBreached` condition accurately reflects actual state (Phase 2 simplification complete)
- [x] Code is simpler than before (removed startup protection from reconcilestatus)
- [x] Documentation is clear and comprehensive (gang-termination-architecture.md updated with detailed examples)

---

## Benefits of This Approach

1. **Simpler reconcilestatus:** No special cases for startup protection
2. **Accurate conditions:** `MinAvailableBreached` reflects actual state
3. **Clear separation:** State reporting vs. policy enforcement
4. **Easier to test:** Each layer tests one concern
5. **Flexible:** Grace period can be tuned per workload
6. **Observable:** Users can see actual breach state and understand why termination hasn't occurred (grace period)

---

## Estimated Timeline

- **Hours 1-3:** Phase 1 (API changes, validation, defaulting, CRD regeneration)
- **Hours 4-6:** Phase 2 (Simplify reconcilestatus logic)
- **Hours 7-11:** Phase 3 (Update gang termination utilities and callers)
- **Hours 12-16:** Phase 5 (Testing and documentation)

**Total: 12-16 hours of focused development**

---

## Rollback Plan

If issues arise:
1. **Before merging:** Changes are in feature branch, easy to discard
2. **After merging:** Can revert the commit
3. **In production:** The new field is optional with defaulting, so existing CRDs will work with default 5-minute grace period

**Recommendation:** Implement in feature branch, thorough testing before merge.
