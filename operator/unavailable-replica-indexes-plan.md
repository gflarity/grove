# Plan: Track Unavailable Replica Indexes in PodCliqueSet

## Overview

**Objective:** Add tracking of unavailable replica indexes to PodCliqueSet status, in addition to the existing count of unavailable replicas.

**Current State:** PodCliqueSet tracks the number of available replicas via `AvailableReplicas` field. The number of unavailable replicas can be computed as `Replicas - AvailableReplicas`, but we don't know *which* replicas are unavailable.

**Desired State:** PodCliqueSet status should include `UnavailableReplicaIndices []int32` field containing the indexes (not reasons) of unavailable replicas.

**Use Case:** Operators and debugging tools need to quickly identify which specific replicas are problematic without inspecting all replicas individually.

---

## Background

### What Makes a Replica Available?

A PodCliqueSet replica is considered **available** when:
1. All standalone PodCliques within that replica have `ReadyReplicas >= MinAvailable`
2. All PodCliqueScalingGroups within that replica have `AvailableReplicas >= MinAvailable`

### Current Implementation

The status reconciliation logic in `internal/controller/podcliqueset/reconcilestatus.go`:
- `computeAvailableAndUpdatedReplicas()` iterates through each replica index (0 to Spec.Replicas-1)
- For each replica, calls `computeReplicaStatus()` to check if all components are available
- Increments `availableReplicas` counter when a replica is available
- Returns only the count, not which replicas are unavailable

### Relevant Code References

- **API Definition:** `api/core/v1alpha1/podcliqueset.go` (line 60-91, PodCliqueSetStatus struct)
- **Status Logic:** `internal/controller/podcliqueset/reconcilestatus.go` (lines 62-118)
- **Helper Functions:** `internal/controller/common/component/utils/podcliqueset.go`
- **Tests:** `internal/controller/podcliqueset/reconcilestatus_test.go`
- **CRDs:** 
  - `api/core/v1alpha1/crds/grove.io_podcliquesets.yaml`
  - `charts/crds/grove.io_podcliquesets.yaml`

---

## Implementation Plan

### Phase 1: API Changes ✓ COMPLETE

**File:** `api/core/v1alpha1/podcliqueset.go`

- [x] **Task 1.1:** Add `UnavailableReplicaIndices` field to `PodCliqueSetStatus` struct
  - **Location:** After line 76 (after the `AvailableReplicas` field)
  - **Code to add:**
    ```go
    // UnavailableReplicaIndices contains the indexes of replicas that are currently unavailable.
    // A replica is considered unavailable when any of its constituent components (PCSGs or standalone PCLQs)
    // do not meet their MinAvailable requirements.
    // +optional
    UnavailableReplicaIndices []int32 `json:"unavailableReplicaIndices,omitempty"`
    ```
  - **Notes:**
    - Use `[]int32` to match the `Replicas` field type
    - Mark as `+optional` for backward compatibility
    - Use `omitempty` so field is omitted from JSON when empty
  - **Verification:** Field appears in struct between `AvailableReplicas` and `Selector` fields

---

### Phase 2: Status Computation Logic ✓ COMPLETE

**File:** `internal/controller/podcliqueset/reconcilestatus.go`

- [x] **Task 2.1:** Update `mutateReplicas` function to handle unavailable indices
  - **Location:** Lines 49-60
  - **Changes:**
    1. Update function call from `computeAvailableAndUpdatedReplicas` to `computeReplicaMetrics`
    2. Add third return value to capture unavailable indices
    3. Set `pcs.Status.UnavailableReplicaIndices = unavailableIndices`
  - **Updated code:**
    ```go
    func (r *Reconciler) mutateReplicas(ctx context.Context, logger logr.Logger, pcs *grovecorev1alpha1.PodCliqueSet) error {
        // Set basic replica count
        pcs.Status.Replicas = pcs.Spec.Replicas
        availableReplicas, updatedReplicas, unavailableIndices, err := r.computeReplicaMetrics(ctx, logger, pcs)
        if err != nil {
            return fmt.Errorf("could not compute replica metrics: %w", err)
        }
        pcs.Status.AvailableReplicas = availableReplicas
        pcs.Status.UpdatedReplicas = updatedReplicas
        pcs.Status.UnavailableReplicaIndices = unavailableIndices
        return nil
    }
    ```
  - **Verification:** Function compiles and sets all three status fields

- [x] **Task 2.2:** Rename `computeAvailableAndUpdatedReplicas` to `computeReplicaMetrics`
  - **Location:** Line 62 (function signature)
  - **Changes:** Update function name and signature
  - **Updated signature:**
    ```go
    func (r *Reconciler) computeReplicaMetrics(ctx context.Context, logger logr.Logger, pcs *grovecorev1alpha1.PodCliqueSet) (int32, int32, []int32, error) {
    ```
  - **Verification:** Function renamed, return type includes `[]int32`

- [x] **Task 2.3:** Add unavailable indices tracking to `computeReplicaMetrics`
  - **Location:** Lines 62-118
  - **Changes:**
    1. Add import for `slices` package at top of file
    2. Initialize `unavailableIndices := []int32{}` after line 69
    3. Update replica loop to track unavailable indexes
    4. Sort indices before returning
    5. Update logging statement
    6. Add unavailable indices to return statement
  - **Key changes in replica loop (around line 108):**
    ```go
    for replicaIndex := 0; replicaIndex < int(pcs.Spec.Replicas); replicaIndex++ {
        replicaIndexStr := strconv.Itoa(replicaIndex)
        replicaStandalonePCLQs := standalonePCLQsByReplica[replicaIndexStr]
        replicaPCSGs := pcsgsByReplica[replicaIndexStr]
        
        // Check if this PCS replica is available based on all its components
        isReplicaAvailable, isReplicaUpdated := r.computeReplicaStatus(pcs.Status.CurrentGenerationHash, replicaPCSGs,
            replicaStandalonePCLQs, len(expectedPCSGFQNsPerPCSReplica[replicaIndex]), len(expectedStandAlonePCLQFQNsPerPCSReplica[replicaIndex]))
        
        if isReplicaAvailable {
            availableReplicas++
        } else {
            unavailableIndices = append(unavailableIndices, int32(replicaIndex))
        }
        if isReplicaUpdated {
            updatedReplicas++
        }
    }
    
    // Sort for consistent ordering
    slices.Sort(unavailableIndices)
    ```
  - **Update logging (around line 116):**
    ```go
    logger.Info("Calculated replica metrics for PCS",
        "pcs", pcsObjectKey,
        "availableReplicas", availableReplicas,
        "updatedReplicas", updatedReplicas,
        "unavailableReplicaIndices", unavailableIndices,
        "totalReplicas", pcs.Spec.Replicas)
    ```
  - **Update return statement (line 117):**
    ```go
    return availableReplicas, updatedReplicas, unavailableIndices, nil
    ```
  - **Verification:** 
    - Function returns three values plus error
    - Unavailable indices are tracked and sorted
    - Logging includes new field

- [x] **Task 2.4:** Add import for `slices` package
  - **Location:** Top of file in imports section (around line 19)
  - **Code to add:** `"slices"` to the standard library imports
  - **Verification:** Import compiles without errors

---

### Phase 3: CRD Generation ✓ COMPLETE

- [x] **Task 3.1:** Generate updated CRDs and deepcopy methods
  - **Command:** `cd api && make generate`
  - **Expected changes:**
    - `api/core/v1alpha1/crds/grove.io_podcliquesets.yaml` includes new field
    - `api/core/v1alpha1/zz_generated.deepcopy.go` includes deepcopy logic for new field
  - **Verification:** Run `git diff api/` and check that:
    - CRD YAML shows `unavailableReplicaIndices` in status schema
    - DeepCopy code handles the new slice field

- [x] **Task 3.2:** Copy updated CRD to Helm charts directory
  - **Command:** `cp api/core/v1alpha1/crds/grove.io_podcliquesets.yaml charts/crds/`
  - **Verification:** File timestamp in `charts/crds/` is updated

- [x] **Task 3.3:** Verify CRD schema correctness
  - **File to check:** `charts/crds/grove.io_podcliquesets.yaml`
  - **What to verify:**
    - Field `unavailableReplicaIndices` appears in status schema
    - Field type is `array` with `items: {type: integer, format: int32}`
    - Field includes proper description from code comments
    - Field is not marked as required (should be optional)
  - **Example expected schema:**
    ```yaml
    unavailableReplicaIndices:
      description: UnavailableReplicaIndices contains the indexes of replicas...
      items:
        format: int32
        type: integer
      type: array
    ```
  - **Verification:** Schema looks correct in YAML

---

### Phase 4: Test Updates ✓ COMPLETE

**File:** `internal/controller/podcliqueset/reconcilestatus_test.go`

- [x] **Task 4.1:** Update test case structure
  - **Location:** Line ~41 (test case struct definition)
  - **Change:** Add `expectedUnavailableIndices []int32` field
  - **Updated struct:**
    ```go
    testCases := []struct {
        name                         string
        setupPCS                     func() *grovecorev1alpha1.PodCliqueSet
        childResources               func() []client.Object
        expectedAvailable            int32
        expectedUpdated              int32
        expectedUnavailableIndices   []int32  // NEW FIELD
    }{
    ```
  - **Verification:** Struct compiles with new field

- [x] **Task 4.2:** Update "all replicas available and updated" test case
  - **Location:** First test case (~line 47)
  - **Add:** `expectedUnavailableIndices: []int32{},`
  - **Verification:** Empty slice for all available replicas

- [x] **Task 4.3:** Update "one replica unavailable" test case (if exists)
  - **Search for:** Test case with 1 unavailable replica
  - **Add:** `expectedUnavailableIndices: []int32{1},` (adjust index based on test)
  - **Verification:** Unavailable replica index matches test setup

- [x] **Task 4.4:** Update "replica with insufficient ready replicas" test case (if exists)
  - **Search for:** Test case with insufficient replicas
  - **Add:** `expectedUnavailableIndices: []int32{0},` (adjust based on test)
  - **Verification:** Index matches which replica is unavailable

- [x] **Task 4.5:** Update "terminating resources" test case (if exists)
  - **Search for:** Test with terminating resources
  - **Add:** `expectedUnavailableIndices: []int32{0},` (adjust based on test)
  - **Verification:** Terminating replicas counted as unavailable

- [x] **Task 4.6:** Update all remaining existing test cases
  - **Action:** Review each test case and add appropriate `expectedUnavailableIndices` value
  - **Guidelines:**
    - Empty slice `[]int32{}` when all replicas available
    - Include specific indexes when replicas are unavailable
    - Match the test's setup and expected behavior
  - **Verification:** All test cases have the new field populated

- [x] **Task 4.7:** Update test function call and assertions
  - **Location:** Test execution section (~line 267-270)
  - **Current code:**
    ```go
    available, updated, err := reconciler.computeAvailableAndUpdatedReplicas(context.Background(), logr.Discard(), pcs)
    assert.NoError(t, err)
    assert.Equal(t, tt.expectedAvailable, available)
    assert.Equal(t, tt.expectedUpdated, updated)
    ```
  - **Updated code:**
    ```go
    available, updated, unavailableIndices, err := reconciler.computeReplicaMetrics(context.Background(), logr.Discard(), pcs)
    assert.NoError(t, err)
    assert.Equal(t, tt.expectedAvailable, available, "Available replicas mismatch")
    assert.Equal(t, tt.expectedUpdated, updated, "Updated replicas mismatch")
    assert.Equal(t, tt.expectedUnavailableIndices, unavailableIndices, "Unavailable replica indices mismatch")
    ```
  - **Verification:** Test assertions check all three values

- [x] **Task 4.8:** Add new test case - "multiple non-contiguous unavailable replicas"
  - **Location:** Add before the test execution loop
  - **Purpose:** Test that non-contiguous unavailable replicas are tracked correctly
  - **Setup:**
    - Create PCS with 5 replicas
    - Make replicas at indexes 0, 2, and 4 unavailable
    - Other replicas (1, 3) are available
  - **Expected results:**
    - `expectedAvailable: 2`
    - `expectedUnavailableIndices: []int32{0, 2, 4}`
  - **Verification:** Test validates non-contiguous index tracking

- [x] **Task 4.9:** Add new test case - "all replicas unavailable"
  - **Location:** Add before the test execution loop
  - **Purpose:** Test edge case where no replicas are available
  - **Setup:**
    - Create PCS with 3 replicas
    - Make all replicas unavailable (e.g., insufficient ready pods)
  - **Expected results:**
    - `expectedAvailable: 0`
    - `expectedUnavailableIndices: []int32{0, 1, 2}`
  - **Verification:** Test validates all-unavailable scenario

- [x] **Task 4.10:** Run all PodCliqueSet status tests
  - **Command:** `go test ./internal/controller/podcliqueset/... -v -run TestComputePCSAvailableReplicas`
  - **Expected:** All tests pass with no failures
  - **Verification:** Look for "PASS" in output, no test failures

---

### Phase 5: Documentation ✓ COMPLETE

- [x] **Task 5.1:** Update architecture documentation
  - **File:** `.agents/status-and-events-handling.md`
  - **Location:** Search for "PodCliqueSetStatus" section
  - **Content to add:**
    ```markdown
    ### UnavailableReplicaIndices Field

    **Field:** `status.unavailableReplicaIndices`  
    **Type:** `[]int32`  
    **Purpose:** Tracks the specific replica indexes that are currently unavailable

    **Availability Criteria:**
    A replica is considered unavailable when:
    - Any standalone PodClique within the replica has `ReadyReplicas < MinAvailable`, OR
    - Any PodCliqueScalingGroup within the replica has `AvailableReplicas < MinAvailable`

    **Properties:**
    - Empty slice when all replicas are available
    - Contains 0-based replica indexes
    - Always sorted in ascending order
    - Invariant: `len(UnavailableReplicaIndices) + AvailableReplicas == Replicas`

    **Examples:**
    - `[]` or omitted - All replicas are available
    - `[0, 2]` - Replicas with indexes 0 and 2 are unavailable (e.g., `pcs-0-*` and `pcs-2-*`)
    - `[1]` - Only replica index 1 is unavailable

    **Use Cases:**
    - **Debugging:** Quickly identify which replicas have issues
    - **Monitoring:** Alert on specific replica failures
    - **Targeted remediation:** Enable tools to focus on problematic replicas
    - **Observability:** Track patterns in replica failures over time
    ```
  - **Verification:** Documentation accurately describes the new field

- [x] **Task 5.2:** Add inline code comments
  - **File:** `internal/controller/podcliqueset/reconcilestatus.go`
  - **Location:** In `computeReplicaMetrics`, before unavailable tracking logic
  - **Comment to add:**
    ```go
    // Track unavailable replica indexes for observability and debugging.
    // This enables operators to quickly identify which specific replicas
    // are problematic without inspecting all replicas individually.
    if isReplicaAvailable {
        availableReplicas++
    } else {
        unavailableIndices = append(unavailableIndices, int32(replicaIndex))
    }
    ```
  - **Verification:** Comments explain why we track indexes

---

### Phase 6: Build & Validation ✓ COMPLETE

- [x] **Task 6.1:** Run all PodCliqueSet controller tests
  - **Command:** `go test ./internal/controller/podcliqueset/... -v`
  - **Expected:** All tests pass with "PASS" status
  - **Verification:** No test failures in output

- [x] **Task 6.2:** Run broader controller tests
  - **Command:** `go test ./internal/controller/... -v`
  - **Expected:** All controller tests pass
  - **Verification:** Ensure no regressions in other controllers

- [x] **Task 6.3:** Verify code generation is complete
  - **Command:** `cd api && make generate && git diff`
  - **Expected:** Only expected files changed (CRDs, deepcopy, etc.)
  - **Verification:** No unexpected changes in git diff

- [x] **Task 6.4:** Build the operator binary
  - **Command:** `make build` (from operator root directory)
  - **Expected:** Clean build with no errors
  - **Verification:** Binary created successfully in expected location

- [x] **Task 6.5:** Verify invariant consistency in code
  - **Action:** Manual code review
  - **Check:** In all code paths in `reconcilestatus.go`, verify that:
    ```
    len(UnavailableReplicaIndices) + AvailableReplicas == Replicas
    ```
  - **Verification:** Mathematical invariant holds true

- [x] **Task 6.6:** Check nil/empty slice handling
  - **Action:** Review serialization and access patterns
  - **Verify:**
    - Empty slice is serialized as `[]` or omitted due to `omitempty`
    - No nil pointer dereferences in code
    - Safe to iterate over field even when empty
  - **Verification:** No nil pointer risks

---

### Phase 7: Manual Testing (Optional - if test cluster available)

- [ ] **Task 7.1:** Deploy operator to test cluster
  - **Command:** `make deploy` or equivalent deployment method
  - **Expected:** Operator pod running successfully
  - **Verification:** `kubectl get pods -n grove-system` shows operator running

- [ ] **Task 7.2:** Create test PodCliqueSet with 3 replicas
  - **Command:** `kubectl apply -f <test-pcs.yaml>`
  - **Expected:** PCS created with 3 replicas
  - **Verification:** `kubectl get pcs` shows the test PCS

- [ ] **Task 7.3:** Verify all replicas become available
  - **Command:** `kubectl get pcs <name> -o yaml | grep -A 5 status`
  - **Expected:** 
    - `availableReplicas: 3`
    - `unavailableReplicaIndices: []` or field omitted
  - **Verification:** All replicas are available, no unavailable indices

- [ ] **Task 7.4:** Make replica 1 unavailable
  - **Action:** Delete pods or scale down components to breach MinAvailable for replica 1
  - **Example:** `kubectl delete pods -l grove.io/podcliqueset-replica-index=1`
  - **Expected:** Replica 1 becomes unavailable

- [ ] **Task 7.5:** Verify unavailable indices are tracked
  - **Command:** `kubectl get pcs <name> -o jsonpath='{.status.unavailableReplicaIndices}'`
  - **Expected:** `[1]`
  - **Verification:** Correct replica index appears in status

- [ ] **Task 7.6:** Fix replica 1 (restore to healthy state)
  - **Action:** Allow pods to be recreated and reach ready state
  - **Expected:** Replica 1 becomes available again

- [ ] **Task 7.7:** Verify indices are cleared
  - **Command:** `kubectl get pcs <name> -o yaml | grep unavailableReplicaIndices`
  - **Expected:** `unavailableReplicaIndices: []` or field omitted
  - **Verification:** Field is empty when all replicas available

- [ ] **Task 7.8:** Test multiple unavailable replicas
  - **Action:** Make replicas 0 and 2 unavailable simultaneously
  - **Expected:** `unavailableReplicaIndices: [0, 2]`
  - **Verification:** Multiple indexes tracked and sorted

- [ ] **Task 7.9:** Clean up test resources
  - **Command:** `kubectl delete pcs <name>`
  - **Verification:** Test PCS and related resources deleted

---

### Phase 8: Code Review & Commit Preparation

- [ ] **Task 8.1:** Self-review code changes
  - **Action:** Review all changed files using `git diff`
  - **Check for:**
    - No debugging code left in (e.g., print statements)
    - Consistent code formatting
    - No unintended changes
    - All comments are clear and accurate
  - **Verification:** Code is clean and ready for review

- [ ] **Task 8.2:** Verify logging statements are appropriate
  - **Check:** Log levels in `computeReplicaMetrics`
  - **Verify:**
    - Indices logged at Info level (not Debug, not Error)
    - No verbose logging in hot paths
    - Logs include proper context (PCS name, namespace)
  - **Verification:** Logging is helpful but not excessive

- [ ] **Task 8.3:** Review performance impact
  - **Analyze:** Array operations in the code
  - **Verify:**
    - Operations are O(n) where n = number of replicas
    - No unnecessary allocations
    - Sorting overhead is acceptable (small arrays)
  - **Verification:** Performance impact is negligible

- [ ] **Task 8.4:** Run final full test suite
  - **Command:** `go test ./... -v`
  - **Expected:** All tests pass across entire codebase
  - **Verification:** No regressions anywhere

- [ ] **Task 8.5:** Stage changes for commit
  - **Command:** `git add <modified files>`
  - **Files to include:**
    - `api/core/v1alpha1/podcliqueset.go`
    - `api/core/v1alpha1/zz_generated.deepcopy.go`
    - `api/core/v1alpha1/crds/grove.io_podcliquesets.yaml`
    - `charts/crds/grove.io_podcliquesets.yaml`
    - `internal/controller/podcliqueset/reconcilestatus.go`
    - `internal/controller/podcliqueset/reconcilestatus_test.go`
    - `.agents/status-and-events-handling.md`
  - **Verification:** Only intended files are staged

- [ ] **Task 8.6:** Create commit with descriptive message
  - **Commit message template:**
    ```
    Add unavailable replica index tracking to PodCliqueSet

    Enhances PodCliqueSet status with UnavailableReplicaIndices field 
    to track which specific replicas are unavailable, complementing 
    the existing AvailableReplicas count.

    Changes:
    - Add UnavailableReplicaIndices []int32 field to PodCliqueSetStatus
    - Update status reconciliation to track unavailable replica indexes
    - Rename computeAvailableAndUpdatedReplicas to computeReplicaMetrics
    - Enhance computeReplicaMetrics to return unavailable indexes list
    - Add comprehensive test coverage for index tracking scenarios
    - Update CRDs with new status field definition
    - Document new field in architecture documentation

    Benefits:
    - Faster debugging: Identify problematic replicas without inspection
    - Better observability: Monitor specific replica failures over time
    - Enables targeted remediation in future enhancements
    - Maintains backward compatibility (optional field)

    Testing:
    - All existing tests updated and passing
    - New test cases for edge scenarios (all unavailable, non-contiguous)
    - Manual testing verified in test cluster (if applicable)

    Invariant maintained: 
    len(UnavailableReplicaIndices) + AvailableReplicas == Replicas
    ```
  - **Command:** `git commit -m "<message>"`
  - **Verification:** Commit created successfully

---

## Acceptance Criteria

Before considering this work complete, verify all of the following:

- [ ] `UnavailableReplicaIndices` field exists in `PodCliqueSetStatus` struct
- [ ] Field is properly populated during status reconciliation
- [ ] Field contains correct indexes of unavailable replicas
- [ ] Invariant holds: `len(UnavailableReplicaIndices) + AvailableReplicas == Replicas`
- [ ] All existing tests pass without modifications to test logic (only expected values updated)
- [ ] New tests cover edge cases (multiple unavailable, all unavailable, non-contiguous indexes)
- [ ] CRDs are updated with new field in both `api/` and `charts/` directories
- [ ] Documentation is updated in `.agents/status-and-events-handling.md`
- [ ] Code builds successfully with `make build`
- [ ] No breaking changes to existing behavior or API
- [ ] Field is backward compatible (optional with `omitempty` tag)
- [ ] Code follows existing patterns and conventions in the codebase

---

## Key Design Principles

### Type Choice
- **`[]int32`** matches the `Replicas` field type
- Standard Go slice, no pointers needed

### Backward Compatibility
- Field marked as `+optional` in Kubebuilder annotations
- Uses `omitempty` JSON tag so field is omitted when empty
- Existing clients can ignore the new field
- No changes to existing logic except to populate new field

### Deterministic Output
- Indexes are **always sorted** in ascending order
- Makes testing easier and output predictable
- Uses `slices.Sort()` from Go standard library

### Invariant
The following must **always** be true:
```go
len(pcs.Status.UnavailableReplicaIndices) + pcs.Status.AvailableReplicas == pcs.Status.Replicas
```

### Scope
- Track **indexes only**, not reasons for unavailability
- Indexes are **0-based** matching resource naming convention
- Indexes correspond to the replica number in resource names:
  - Index `0` → `<pcs-name>-0-<component>`
  - Index `1` → `<pcs-name>-1-<component>`
  - etc.

---

## Troubleshooting

### Issue: Tests fail after updating test structure
**Solution:** Ensure all test cases have the new `expectedUnavailableIndices` field populated

### Issue: CRD generation fails
**Solution:** 
- Ensure Kubebuilder markers are correct (`+optional`, JSON tags)
- Run `cd api && make generate` from the correct directory
- Check for syntax errors in the struct field definition

### Issue: Build fails with "undefined: slices"
**Solution:** Add `"slices"` to imports in `reconcilestatus.go` (Go 1.21+ standard library)

### Issue: Invariant doesn't hold in tests
**Solution:** 
- Double-check logic in `computeReplicaMetrics`
- Ensure every replica is counted as either available OR unavailable, not both
- Verify loop iterates exactly `pcs.Spec.Replicas` times

### Issue: Empty slice vs nil in JSON
**Solution:** 
- Go will serialize empty slice as `[]` or omit due to `omitempty`
- Both are acceptable for empty unavailable list
- Tests should use `[]int32{}` not `nil` for expected values

---

## Rollback Plan

If issues are discovered after implementation:

1. **Immediate mitigation:** The field is optional, so existing clients continue to work
2. **Safe revert:** Revert changes to `reconcilestatus.go` - field will remain empty but won't break anything
3. **Full rollback:** Revert all changes including API - new field will be ignored by older clients
4. **CRD rollback:** Not typically required - additional optional fields don't break existing functionality
5. **Deployment strategy:** Can be rolled out gradually, old clients ignore new field

---

## Estimated Effort

| Phase | Estimated Time |
|-------|---------------|
| Phase 1: API Changes | 15 minutes |
| Phase 2: Status Logic | 30 minutes |
| Phase 3: CRD Generation | 10 minutes |
| Phase 4: Test Updates | 1 hour |
| Phase 5: Documentation | 20 minutes |
| Phase 6: Build & Validation | 30 minutes |
| Phase 7: Manual Testing (optional) | 45 minutes |
| Phase 8: Code Review & Commit | 20 minutes |
| **Total** | **~3-4 hours** |

*Note: Time estimates are for an experienced Go developer familiar with the Grove codebase.*

---

## Dependencies & Prerequisites

### Required Tools
- Go 1.21 or higher (for `slices` package)
- Make (for build automation)
- Kubebuilder code generation tools (already configured)

### Required Knowledge
- Go programming language
- Kubernetes Custom Resource Definitions (CRDs)
- Controller-runtime patterns
- Table-driven testing in Go

### Codebase Familiarity
- Review `.agents/general_index.md` for file locations
- Review `.agents/status-and-events-handling.md` for status patterns
- Understand PodCliqueSet replica model
- Understand current availability calculation logic

---

## References

### Related Documentation
- `.agents/general_index.md` - File location index
- `.agents/status-and-events-handling.md` - Status and event handling patterns
- `.agents/grove-operator-architecture.md` - Overall architecture

### Key Code Files
- `api/core/v1alpha1/podcliqueset.go` - API definitions
- `internal/controller/podcliqueset/reconcilestatus.go` - Status reconciliation
- `internal/controller/common/component/utils/podcliqueset.go` - Helper utilities

### Related Issues/PRs
*(Add links if applicable)*

---

## Questions or Issues?

If you encounter any problems or have questions while implementing this plan:

1. Review the **Troubleshooting** section above
2. Check existing tests in `reconcilestatus_test.go` for patterns
3. Review similar status fields in other controllers (e.g., PodCliqueScalingGroup)
4. Consult the architecture documentation in `.agents/` directory

---

**Plan Version:** 1.0  
**Last Updated:** 2025-11-13  
**Status:** Ready for Implementation
