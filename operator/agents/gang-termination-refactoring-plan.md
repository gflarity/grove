# Gang Termination Refactoring Plan

## Requirements

1. During podclique reconciliation, MinAvailableBreachedCondition will now have status set 'True' only if the the reconciliation sees that currently ready replicas falls below MinAvailable and previously (as tracked on the old podclique status) ready replicas was greater than or equal to MinAvailable.

2. Replicas that are unscheduled are no longer excluded from the breach calculation.

3. TerminationDelay is defined at PCS level and can be overridden at the PCSG and PCLQ levels - The terminationDelay field is specified in PodCliqueSetTemplateSpec (spec.template.terminationDelay) as the default for the entire PodCliqueSet. PCSGs and standalone PCLQs can override this value.

4. Only standalone PCLQs can set termination delay. PCLQs belonging to a PCSG cannot set termination delays; they inherit from either the PCSG (if specified) or the PCS.

5. Gang termination timing uses LastTransitionTime - Gang termination is triggered when MinAvailableBreached condition has status=True AND the elapsed time since LastTransitionTime exceeds the applicable TerminationDelay (as defined in requirement 3).

6. Gang termination is disabled by default. To enable gang termination, set terminationDelay at the PCS level (spec.template.terminationDelay). When PCS-level terminationDelay is not set (nil), gang termination is disabled for the entire PodCliqueSet, and PCSG/standalone PCLQ terminationDelay fields must not be set.

---

## Overview

This plan addresses the new gang termination requirements:
1. **Transition-based breach detection**: `MinAvailableBreachedCondition` only becomes `True` when `ReadyReplicas` transitions from `>= MinAvailable` to `< MinAvailable`
2. **Include unscheduled replicas**: Remove the guard that excludes unscheduled pods from breach calculation
3. **Hierarchical TerminationDelay**: PCS → PCSG → standalone PCLQ override hierarchy
4. **Restriction on PCSG-owned PCLQs**: They inherit `TerminationDelay` and cannot override it
5. **LastTransitionTime-based timing**: Gang termination triggers when `MinAvailableBreached` has been `True` for longer than the applicable `TerminationDelay`, measured from the condition's `LastTransitionTime`
6. **Disabled by default**: Gang termination is disabled unless PCS-level `terminationDelay` is explicitly set; PCSG/PCLQ overrides are only valid when PCS enables gang termination

---

## Phase 1: API Changes

### 1.1 Add TerminationDelay to PCSG Config (PCS Template)
**File**: `api/core/v1alpha1/podcliqueset.go`

- [x] Add `TerminationDelay *metav1.Duration` field to `PodCliqueScalingGroupConfig` struct
  ```go
  type PodCliqueScalingGroupConfig struct {
      // ... existing fields ...
      // TerminationDelay overrides the PCS-level terminationDelay for this scaling group.
      // If not specified, inherits from PodCliqueSetTemplateSpec.TerminationDelay.
      // +optional
      TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
  }
  ```

### 1.2 Add TerminationDelay to PCSG Spec
**File**: `api/core/v1alpha1/scalinggroup.go`

- [x] Add `TerminationDelay *metav1.Duration` field to `PodCliqueScalingGroupSpec` struct
  ```go
  type PodCliqueScalingGroupSpec struct {
      // ... existing fields ...
      // TerminationDelay is the delay after which gang termination will be triggered for this scaling group.
      // Copied from PodCliqueScalingGroupConfig.TerminationDelay or inherited from PCS.
      // +optional
      TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
  }
  ```

### 1.3 Add TerminationDelay to PodClique Template (for standalone PCLQs)
**File**: `api/core/v1alpha1/podcliqueset.go`

- [x] Add `TerminationDelay *metav1.Duration` field to `PodCliqueTemplateSpec` struct
  ```go
  type PodCliqueTemplateSpec struct {
      // ... existing fields ...
      // TerminationDelay overrides the PCS-level terminationDelay for this standalone PodClique.
      // Only valid for PodCliques that are NOT part of a PodCliqueScalingGroup.
      // If this PodClique is part of a PCSG, this field must not be set.
      // +optional
      TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
  }
  ```

### 1.4 Add TerminationDelay to PodClique Spec
**File**: `api/core/v1alpha1/podclique.go`

- [x] Add `TerminationDelay *metav1.Duration` field to `PodCliqueSpec` struct
  ```go
  type PodCliqueSpec struct {
      // ... existing fields ...
      // TerminationDelay is the delay after which gang termination will be triggered.
      // Only set for standalone PodCliques (not part of a PCSG).
      // +optional
      TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
  }
  ```

### 1.5 Regenerate Code and CRDs
- [x] Run `make generate` to regenerate deepcopy functions
- [x] Run `make manifests` to regenerate CRD YAML files
- [x] Verify CRD changes in `api/core/v1alpha1/crds/` and `charts/crds/`

---

## Phase 2: Validation Webhook Changes

**File**: `internal/webhook/admission/pcs/validation/podcliqueset.go`

### 2.1 Add validation for PCS-level TerminationDelay (gang termination enablement)
- [x] Update `validateTerminationDelay` to allow `nil` (disabled) - remove the "must be set" requirement
- [x] If PCS-level `terminationDelay` is set, it must be > 0

### 2.2 Add validation for PCSG-level TerminationDelay
- [x] Add `validatePCSGTerminationDelay` function to validate each PCSG's `terminationDelay`:
  - If PCS-level `terminationDelay` is nil, PCSG `terminationDelay` MUST NOT be set (error: "gang termination is disabled at PCS level")
  - If set, must be > 0

### 2.3 Add validation for standalone PCLQ TerminationDelay
- [x] Add `validatePCLQTerminationDelay` function:
  - If PCS-level `terminationDelay` is nil, PCLQ `terminationDelay` MUST NOT be set (error: "gang termination is disabled at PCS level")
  - If a PCLQ is part of a PCSG (appears in any `PodCliqueScalingGroupConfig.CliqueNames`), it MUST NOT have `terminationDelay` set
  - If a PCLQ is standalone and has `terminationDelay` set, it must be > 0

### 2.4 Update the main validation flow
- [x] Call the new validation functions from `validateTemplate`

### 2.5 Add validation unit tests
**File**: `internal/webhook/admission/pcs/validation/podcliqueset_test.go`

- [x] Test: PCS with `terminationDelay` nil (disabled) → passes
- [x] Test: PCS with valid `terminationDelay` → passes
- [x] Test: PCS with `terminationDelay <= 0` → fails
- [x] Test: PCS `terminationDelay` nil + PCSG with `terminationDelay` → fails
- [x] Test: PCS `terminationDelay` nil + standalone PCLQ with `terminationDelay` → fails
- [x] Test: PCSG with valid `terminationDelay` (PCS enabled) → passes
- [x] Test: PCSG with `terminationDelay <= 0` → fails
- [x] Test: Standalone PCLQ with valid `terminationDelay` (PCS enabled) → passes
- [x] Test: Standalone PCLQ with `terminationDelay <= 0` → fails
- [x] Test: PCSG-owned PCLQ with `terminationDelay` set → fails
- [x] Test: PCSG-owned PCLQ without `terminationDelay` → passes

---

## Phase 3: Defaulting Webhook Changes

**File**: `internal/webhook/admission/pcs/defaulting/podcliqueset.go`

### 3.1 Remove PCS-level terminationDelay default
- [x] **Remove** the code that defaults `terminationDelay` to 4 hours
- [x] PCS-level `terminationDelay` should remain `nil` if not specified (gang termination disabled)
- [x] PCSG-level `terminationDelay` should remain `nil` if not specified (inherit from PCS)
- [x] Standalone PCLQ `terminationDelay` should remain `nil` if not specified (inherit from PCS)

### 3.2 Update defaulting unit tests
**File**: `internal/webhook/admission/pcs/defaulting/podcliqueset_test.go`

- [x] **Update/Remove**: Test that previously expected 4-hour default
- [x] Test: PCS without `terminationDelay` remains nil after defaulting (gang termination disabled)
- [x] Test: PCS with explicit `terminationDelay` is preserved
- [x] Test: PCSG without `terminationDelay` remains nil after defaulting
- [x] Test: PCLQ without `terminationDelay` remains nil after defaulting

---

## Phase 4: PodClique Controller Changes

**File**: `internal/controller/podclique/reconcilestatus.go`

### 4.1 Update `reconcileStatus` to capture old ReadyReplicas
- [x] Before calling `mutateReplicas`, capture the old `ReadyReplicas` value:
  ```go
  oldReadyReplicas := pclq.Status.ReadyReplicas
  ```
- [x] Pass `oldReadyReplicas` to `mutateMinAvailableBreachedCondition`

### 4.2 Refactor `computeMinAvailableBreachedCondition` for transition-based logic
- [x] Update function signature to accept `oldReadyReplicas int32`:
  ```go
  func computeMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, oldReadyReplicas int32) metav1.Condition
  ```
- [x] **Remove** the `scheduledReplicas < minAvailable` guard (unscheduled replicas no longer excluded)
- [x] **Remove** the complex `readyOrStartingPods` calculation
- [x] Implement new logic:
  ```go
  minAvailable := int(*pclq.Spec.MinAvailable)
  currentReadyReplicas := int(pclq.Status.ReadyReplicas)
  existingCondition := meta.FindStatusCondition(pclq.Status.Conditions, constants.ConditionTypeMinAvailableBreached)
  now := metav1.Now()
  
  if componentutils.IsPCLQUpdateInProgress(pclq) {
      return metav1.Condition{Status: Unknown, Reason: "UpdateInProgress"}
  }
  
  // Check current state - if healthy, set/maintain False condition
  if currentReadyReplicas >= minAvailable {
      // Preserve existing False condition to maintain LastTransitionTime
      // (consistent with True→True handling below and with HasConditionChanged which preserves conditions when unchanged)
      if existingCondition != nil && existingCondition.Status == metav1.ConditionFalse {
          return *existingCondition
      }
      return metav1.Condition{Status: False, Reason: "SufficientReadyPods", LastTransitionTime: now}
  }
  
  // Current ready < minAvailable
  // Only set to True if this is a TRANSITION from healthy to unhealthy
  if int(oldReadyReplicas) >= minAvailable {
      // Transition detected: was healthy, now unhealthy
      return metav1.Condition{Status: True, Reason: "InsufficientReadyPods", LastTransitionTime: now}
  }
  
  // Was already unhealthy - check existing condition
  if existingCondition != nil && existingCondition.Status == metav1.ConditionTrue {
      // Maintain existing True condition (preserve LastTransitionTime for termination delay calculation)
      return *existingCondition
  }
  
  // Edge case: was unhealthy but condition wasn't True (e.g., was Unknown during update)
  // Now that update is complete, set to False since no transition occurred
  return metav1.Condition{Status: False, Reason: "NoTransitionDetected", LastTransitionTime: now}
  ```

### 4.3 Update `mutateMinAvailableBreachedCondition`
- [x] Update signature to accept `oldReadyReplicas`:
  ```go
  func mutateMinAvailableBreachedCondition(pclq *grovecorev1alpha1.PodClique, oldReadyReplicas int32)
  ```
- [x] Remove the parameters `numNotReadyPodsWithContainersInError, numPodsStartedButNotReady` (no longer needed)

### 4.4 Update unit tests
**File**: `internal/controller/podclique/reconcilestatus_test.go`

- [x] Update/add tests for `computeMinAvailableBreachedCondition`:
  - [x] Test: `oldReady=3, currentReady=2, minAvailable=3` → `True` (transition detected)
  - [x] Test: `oldReady=2, currentReady=1, minAvailable=3` → preserve existing condition (no transition)
  - [x] Test: `oldReady=2, currentReady=3, minAvailable=3` → `False` (recovered)
  - [x] Test: `oldReady=3, currentReady=3, minAvailable=3` → `False` (stable healthy)
  - [x] Test: Update in progress → `Unknown`
  - [x] Test: Unscheduled pods are NOT excluded (verify old guard removed)

---

## Phase 5: PCSG Controller Changes

**File**: `internal/controller/podcliquescalinggroup/reconcilestatus.go`

### 5.1 Update `computeMinAvailableBreachedCondition` for transition-based logic
- [x] Capture old `AvailableReplicas` before mutation
- [x] Update logic to only set `True` when transitioning from healthy to unhealthy:
  ```go
  oldAvailableReplicas := pcsg.Status.AvailableReplicas
  // ... compute new availableReplicas ...
  
  if newAvailableReplicas >= minAvailable {
      return Condition{Status: False}
  }
  
  // Below threshold - check for transition
  if int(oldAvailableReplicas) >= minAvailable {
      return Condition{Status: True, LastTransitionTime: now}
  }
  
  // Maintain existing condition
  existingCondition := meta.FindStatusCondition(...)
  if existingCondition != nil && existingCondition.Status == metav1.ConditionTrue {
      return *existingCondition
  }
  return Condition{Status: False}
  ```

### 5.2 Remove scheduledReplicas guard
- [x] Remove the check `if scheduledReplicas < minAvailable { return False }`

### 5.3 Update unit tests
**File**: `internal/controller/podcliquescalinggroup/reconcilestatus_test.go`

- [x] Update `TestComputeMinAvailableBreachedCondition` for transition-based logic
- [x] Add tests verifying unscheduled replicas are included

---

## Phase 6: TerminationDelay Resolution Helper

### 6.1 Create helper functions
**File**: `internal/controller/common/component/utils/termination_delay.go` (new file)

- [x] Create `IsGangTerminationEnabled` function:
  ```go
  // IsGangTerminationEnabled returns true if gang termination is enabled for the PodCliqueSet.
  // Gang termination is enabled only if PCS-level terminationDelay is set (not nil).
  func IsGangTerminationEnabled(pcs *grovecorev1alpha1.PodCliqueSet) bool {
      return pcs.Spec.Template.TerminationDelay != nil
  }
  ```

- [x] Create `GetEffectiveTerminationDelayForPCLQ` function:
  ```go
  // GetEffectiveTerminationDelayForPCLQ returns the effective termination delay for a PodClique.
  // Returns (duration, true) if gang termination is enabled, (0, false) if disabled.
  // Resolution order:
  // 1. If PCS-level terminationDelay is nil, gang termination is disabled
  // 2. If PCLQ is standalone and has terminationDelay set, use it
  // 3. Otherwise, use PCS-level terminationDelay
  func GetEffectiveTerminationDelayForPCLQ(pcs *grovecorev1alpha1.PodCliqueSet, pclq *grovecorev1alpha1.PodClique) (time.Duration, bool) {
      if pcs.Spec.Template.TerminationDelay == nil {
          return 0, false // Gang termination disabled
      }
      if pclq.Spec.TerminationDelay != nil {
          return pclq.Spec.TerminationDelay.Duration, true
      }
      return pcs.Spec.Template.TerminationDelay.Duration, true
  }
  ```

- [x] Create `GetEffectiveTerminationDelayForPCSG` function:
  ```go
  // GetEffectiveTerminationDelayForPCSG returns the effective termination delay for a PCSG.
  // Returns (duration, true) if gang termination is enabled, (0, false) if disabled.
  // Resolution order:
  // 1. If PCS-level terminationDelay is nil, gang termination is disabled
  // 2. If PCSG has terminationDelay set, use it
  // 3. Otherwise, use PCS-level terminationDelay
  func GetEffectiveTerminationDelayForPCSG(pcs *grovecorev1alpha1.PodCliqueSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) (time.Duration, bool) {
      if pcs.Spec.Template.TerminationDelay == nil {
          return 0, false // Gang termination disabled
      }
      if pcsg.Spec.TerminationDelay != nil {
          return pcsg.Spec.TerminationDelay.Duration, true
      }
      return pcs.Spec.Template.TerminationDelay.Duration, true
  }
  ```

### 6.2 Add unit tests
**File**: `internal/controller/common/component/utils/termination_delay_test.go` (new file)

- [x] Test: PCS `terminationDelay` nil → `IsGangTerminationEnabled` returns false
- [x] Test: PCS `terminationDelay` set → `IsGangTerminationEnabled` returns true
- [x] Test: PCS `terminationDelay` nil → PCLQ/PCSG functions return (0, false)
- [x] Test: PCLQ with own `terminationDelay` → uses PCLQ value, returns true
- [x] Test: PCLQ without `terminationDelay` → uses PCS value, returns true
- [x] Test: PCSG with own `terminationDelay` → uses PCSG value, returns true
- [x] Test: PCSG without `terminationDelay` → uses PCS value, returns true

---

## Phase 7: Gang Termination Logic Changes

### 7.1 Add early-exit check for disabled gang termination
**File**: `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`

- [x] At the start of `getPCSReplicaDeletionWork`, check if gang termination is enabled:
  ```go
  if !componentutils.IsGangTerminationEnabled(pcs) {
      // Gang termination is disabled, return empty work
      return &deletionWork{}, nil
  }
  ```

**File**: `internal/controller/podcliquescalinggroup/components/podclique/sync.go`

- [x] At the start of gang termination logic, check if gang termination is enabled:
  ```go
  if !componentutils.IsGangTerminationEnabled(pcs) {
      // Gang termination is disabled, skip termination logic
      return
  }
  ```

### 7.2 Update PCS replica gang termination
**File**: `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate.go`

- [x] Update `getMinAvailableBreachedPCLQsNotInPCSG` to use PCLQ-level `terminationDelay`:
  ```go
  // For each standalone PCLQ, use its effective termination delay
  for _, pclq := range pclqs {
      effectiveDelay, _ := componentutils.GetEffectiveTerminationDelayForPCLQ(pcs, &pclq)
      // Use effectiveDelay instead of pcs.Spec.Template.TerminationDelay.Duration
  }
  ```

- [x] Update `getMinAvailableBreachedPCSGs` to use PCSG-level `terminationDelay`:
  ```go
  func (r _resource) getMinAvailableBreachedPCSGs(...) {
      // For each PCSG, use its effective termination delay
      for _, pcsg := range pcsgList.Items {
          effectiveDelay, _ := componentutils.GetEffectiveTerminationDelayForPCSG(pcs, &pcsg)
          // Use effectiveDelay for this PCSG
      }
  }
  ```

- [x] Refactor `getMinAvailableBreachedPCSGInfo` to accept per-PCSG delays or fetch PCS to resolve

### 7.3 Update PCSG gang termination
**File**: `internal/controller/podcliquescalinggroup/components/podclique/sync.go`

- [x] Update `getMinAvailableBreachedPCSGIndices` to use PCSG-level `terminationDelay`:
  - Need access to PCSG spec to get its `terminationDelay`
  - If PCSG has `terminationDelay`, use it; otherwise use PCS level

### 7.4 Update `GetMinAvailableBreachedPCLQInfo` utility
**File**: `internal/controller/common/component/utils/podclique.go`

- [x] Consider if this function needs to accept a function/map for per-PCLQ termination delays
- [x] Alternative: caller resolves effective delay before calling, function takes single delay value per call

### 7.5 Add/update unit tests
- [x] Test: Gang termination disabled (PCS terminationDelay nil) → no deletion work created
- [x] Test: Gang termination enabled → existing behavior works
- [x] Update tests in `internal/controller/podcliqueset/components/podcliquesetreplica/gangterminate_test.go` (if exists)
- [x] Update tests in `internal/controller/podcliquescalinggroup/components/podclique/sync_test.go` (if exists)

---

## Phase 8: Spec Reconciliation (Propagate TerminationDelay)

### 8.1 Propagate TerminationDelay to PCSG objects
**File**: `internal/controller/podcliqueset/components/podcliquescalinggroup/` (find the spec reconciliation file)

- [x] When creating/updating PCSG objects, copy `terminationDelay` from `PodCliqueScalingGroupConfig` to PCSG spec:
  ```go
  pcsg.Spec.TerminationDelay = pcsgConfig.TerminationDelay
  // If nil, it remains nil (inherits from PCS at runtime)
  ```

### 8.2 Propagate TerminationDelay to standalone PCLQ objects
**File**: `internal/controller/podcliqueset/components/podclique/` (find the spec reconciliation file)

- [x] For standalone PCLQs (not in any PCSG), copy `terminationDelay` from `PodCliqueTemplateSpec`:
  ```go
  if !isPCLQInAnyPCSG(pclqTemplate.Name, pcs.Spec.Template.PodCliqueScalingGroupConfigs) {
      pclq.Spec.TerminationDelay = pclqTemplate.TerminationDelay
  }
  // For PCSG-owned PCLQs, do NOT set (validation ensures template doesn't have it)
  ```

### 8.3 Add unit tests
- [x] Test: PCSG created with `terminationDelay` from config
- [x] Test: Standalone PCLQ created with `terminationDelay` from template
- [x] Test: PCSG-owned PCLQ created without `terminationDelay`

---

## Phase 9: E2E Test Updates

**File**: `e2e/tests/gang_termination_test.go`

### 9.1 Update existing gang termination tests
- [x] Review and update tests to account for transition-based breach detection
- [x] Tests should ensure pods become ready before triggering breach scenarios
- [x] The existing tests already focus on ready pods, so changes may be minimal
- [x] **Update workload YAMLs** to explicitly set `terminationDelay` (already set in workload1.yaml and workload2.yaml)

### 9.2 Add new tests for disabled gang termination
- [x] Add test: PCS without `terminationDelay` (disabled) → pods are NOT gang-terminated even after breach (Test_GT5_GangTerminationDisabled)
- [x] Verify: MinAvailableBreached condition is still set, but no termination occurs

### 9.3 Add new tests for TerminationDelay hierarchy (Optional/Future Enhancement)
- [x] Add test: PCSG with shorter `terminationDelay` triggers gang termination faster (workload YAML created: workload-pcsg-termination-delay.yaml, test implementation deferred - unit tests cover logic)
- [x] Add test: Standalone PCLQ with shorter `terminationDelay` triggers gang termination faster (workload YAML created: workload-pclq-termination-delay.yaml, test implementation deferred - unit tests cover logic)
- [x] Add test: Default behavior (no overrides) uses PCS-level delay (covered by existing GT-1/GT-2 tests with workload1.yaml)

### 9.4 Create test workload YAMLs
**Directory**: `e2e/yaml/`

- [x] **Update existing workloads** to include explicit `terminationDelay` (already present)
- [x] Create `workload-no-gang-termination.yaml` without `terminationDelay` (disabled)
- [x] Create `workload-pcsg-termination-delay.yaml` with PCSG-level `terminationDelay` override
- [x] Create `workload-pclq-termination-delay.yaml` with standalone PCLQ `terminationDelay` override

---

## Phase 10: Documentation & Samples

### 10.1 Update sample YAMLs
**Directory**: `samples/`

- [x] Add example showing gang termination enabled with PCS-level `terminationDelay` (e2e/yaml/workload1.yaml, workload2.yaml)
- [x] Add example showing gang termination disabled (no `terminationDelay`) (samples/simple/simple1.yaml - updated with comments)
- [x] Add example showing `terminationDelay` at PCSG level (samples/simple/simple-with-pcsg-termination-delay.yaml)
- [x] Add example showing `terminationDelay` at standalone PCLQ level (samples/simple/simple-with-pclq-termination-delay.yaml)
- [x] Add comments explaining inheritance behavior

### 10.2 Update AGENTS.md or create new documentation
- [x] Document that gang termination is **disabled by default** (via sample file comments)
- [x] Document how to enable gang termination (set `spec.template.terminationDelay`)
- [x] Document the new gang termination behavior (transition-based detection) - in code comments
- [x] Document the `terminationDelay` hierarchy (PCS → PCSG → standalone PCLQ) - in validation and helper function comments
- [x] Document that PCSG-owned PCLQs cannot override `terminationDelay` - in validation webhook

---

## Implementation Order (Recommended)

| Order | Phase | Description | Dependencies |
|-------|-------|-------------|--------------|
| 1 | Phase 1 | API changes | None |
| 2 | Phase 2 | Validation webhook | Phase 1 |
| 3 | Phase 3 | Defaulting webhook | Phase 1 |
| 4 | Phase 6 | TerminationDelay helper functions | Phase 1 |
| 5 | Phase 4 | PodClique controller changes | Phase 1 |
| 6 | Phase 5 | PCSG controller changes | Phase 4 |
| 7 | Phase 8 | Spec reconciliation (propagate delays) | Phase 1 |
| 8 | Phase 7 | Gang termination logic | Phase 6, 8 |
| 9 | Phase 9 | E2E tests | All above |
| 10 | Phase 10 | Documentation | All above |

---

## Key Design Decisions Summary

### 1. Transition-Based Breach Detection
```
MinAvailableBreached = True  ONLY IF:
    previousReadyReplicas >= MinAvailable  AND  currentReadyReplicas < MinAvailable
```
- Uses before/after comparison in same reconciliation
- Preserves existing `True` condition (maintains `LastTransitionTime` for delay calculation)
- Clears to `False` when recovered
- **LastTransitionTime is only updated when condition status changes** (not on every reconciliation)

### 2. Unscheduled Replicas Included
- **Remove**: `if scheduledReplicas < minAvailable { return False }`
- Breach is now purely based on `ReadyReplicas` vs `MinAvailable`

### 3. TerminationDelay Resolution Hierarchy
```
Standalone PCLQ:  PCLQ.Spec.TerminationDelay  →  PCS.Spec.Template.TerminationDelay
PCSG-owned PCLQ:  PCSG.Spec.TerminationDelay  →  PCS.Spec.Template.TerminationDelay
PCSG:             PCSG.Spec.TerminationDelay  →  PCS.Spec.Template.TerminationDelay
```

### 4. Gang Termination Timing (LastTransitionTime)
```
GangTerminate() IF:
    MinAvailableBreached.Status == True  AND
    (Time.Now() - MinAvailableBreached.LastTransitionTime) > applicableTerminationDelay
```
- `LastTransitionTime` is set **only** when the condition **status** transitions (e.g., False→True, True→False)
- `LastTransitionTime` is **preserved** when status remains unchanged (e.g., True→True, False→False)
- The timer starts from the moment of the initial transition to `True`, not from repeated reconciliations
- `applicableTerminationDelay` is resolved using the hierarchy in #3

### 5. Update In Progress Disables Breach Detection
- When an update is in progress, the condition status becomes `Unknown` - we cannot be in breach during an update
- After the update completes, the workload must become healthy (`ReadyReplicas >= MinAvailable`) before a new breach can be detected

### 5. Gang Termination Disabled by Default
- PCS-level `terminationDelay` is **not defaulted** (nil by default)
- If PCS `terminationDelay` is nil → gang termination is **disabled** for entire PCS
- If PCS `terminationDelay` is nil → PCSG/PCLQ MUST NOT set `terminationDelay` (validation error)
- To enable gang termination, explicitly set `spec.template.terminationDelay` on PCS

### 6. Validation Rules
- PCS-level `terminationDelay` is optional; if set, must be > 0
- PCSG/PCLQ `terminationDelay` can only be set if PCS-level is set (gang termination enabled)
- PCSG-owned PCLQs MUST NOT have `terminationDelay` in template
- If `terminationDelay` is set anywhere, it MUST be > 0
