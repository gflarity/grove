# Implementation Plan: User-Facing Schedule Gate Events

## Overview

**Goal:** Add informative Kubernetes events around gang scheduling and schedule gates that reference user-facing resources (PodCliques, PodCliqueScalingGroups, Pods) rather than internal implementation details (PodGangs).

**Motivation:**
- Users currently have limited visibility into why pods are stuck with schedule gates
- Gang scheduling failures are opaque - operators don't know if they're waiting for resources, pod creation, or dependencies
- Troubleshooting requires deep knowledge of Grove internals (PodGang resources)
- Events should guide operators to the actual blocking resources

**Desired Outcomes:**
1. Clear, actionable events when schedule gates block pod scheduling
2. Events reference user-facing resources (PodCliques, not PodGangs)
3. Events include specific blocking conditions (e.g., "waiting for my-app-0-worker: 2/3 scheduled")
4. Operators can follow a clear debugging path without knowing Grove internals
5. Enhanced status condition messages with more context (schedule-gated pod counts)

---

## Progress Tracking

**Started:** [DATE]  
**Target Completion:** [DATE]  
**Implementer:** [NAME]

### Overall Progress
- [ ] Phase 1: Foundation & Constants (0/1 tasks)
- [ ] Phase 2: Helper Functions (0/3 tasks)
- [ ] Phase 3: PodClique Events (0/4 tasks)
- [ ] Phase 4: PodCliqueSet Events (0/2 tasks)
- [ ] Phase 5: Enhanced Conditions (0/2 tasks)
- [ ] Phase 6: Testing (0/3 tasks)
- [ ] Phase 7: Documentation (0/3 tasks)
- [ ] Phase 8: Optional Enhancements (0/3 tasks)

---

## Phase 1: Foundation & Constants

### Task 1.1: Add New Event Reason Constants
**File:** `internal/constants/constants.go`

- [ ] Add new constants section for scheduling gate events:
  ```go
  // constants for scheduling gate events
  const (
      // ReasonScheduleGateRemoved indicates a schedule gate was successfully removed from a pod
      ReasonScheduleGateRemoved = "ScheduleGateRemoved"
      
      // ReasonScheduleGateWaitingGangFormation indicates pod is waiting for all pods in the gang to be created and labeled
      ReasonScheduleGateWaitingGangFormation = "ScheduleGateWaitingGangFormation"
      
      // ReasonScheduleGateWaitingBasePodCliques indicates pod in scaled gang waiting for base PodCliques to schedule
      ReasonScheduleGateWaitingBasePodCliques = "ScheduleGateWaitingBasePodCliques"
      
      // ReasonPodsPendingCreation indicates gang formation blocked waiting for pods to be created
      ReasonPodsPendingCreation = "PodsPendingCreation"
      
      // ReasonGangFormationComplete indicates all pods for a gang have been created and labeled
      ReasonGangFormationComplete = "GangFormationComplete"
  )
  ```
- [ ] Commit with message: "Add event reason constants for schedule gate operations"

**Motivation:** Centralized constants ensure consistent event reasons across the codebase and make them discoverable.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

---

## Phase 2: Helper Functions for User-Facing Context

### Task 2.1: Create Helper to Get Blocking PodCliques
**File:** `internal/controller/podclique/components/pod/syncflow.go`

- [ ] Add function `getBlockingPodCliquesInfo()` to retrieve user-facing blocking information:
  ```go
  // getBlockingPodCliquesInfo returns detailed information about PodCliques blocking the base PodGang
  // Returns a slice of human-readable strings like "my-app-0-worker (2/3 scheduled)"
  func (r _resource) getBlockingPodCliquesInfo(
      ctx context.Context, 
      logger logr.Logger, 
      namespace, basePodGangName string,
  ) ([]string, error)
  ```
- [ ] Implement logic to:
  - Get the base PodGang resource
  - Iterate through its PodGroups
  - For each PodGroup, get the corresponding PodClique
  - Check if `ScheduledReplicas < MinReplicas`
  - Build formatted string: `"<pclq-name> (<scheduled>/<required> scheduled)"`
- [ ] Add error handling for API failures
- [ ] Add tests in `syncflow_test.go` covering:
  - All PodCliques scheduled (returns empty slice)
  - Some PodCliques blocking (returns correct details)
  - Multiple PodCliques blocking (includes all in result)
  - PodGang not found (returns appropriate error)
  - PodClique not found (skips gracefully)
  - API errors (propagates error)

**Motivation:** This function translates PodGang scheduling status into user-facing PodClique information, hiding implementation details.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 2.2: Create Helper to Extract Replica Index
**File:** `internal/controller/podclique/components/pod/syncflow.go`

- [ ] Add function `getReplicaIndexFromPodClique()`:
  ```go
  // getReplicaIndexFromPodClique extracts the PodCliqueSet replica index from a PodClique
  // Returns the replica index or error if it cannot be determined
  func getReplicaIndexFromPodClique(pclq *grovecorev1alpha1.PodClique) (int, error)
  ```
- [ ] Implement using labels (check `apicommon.LabelPodCliqueSetReplicaIndex`)
- [ ] Add fallback to parse from name if label missing (for robustness)
- [ ] Add tests covering:
  - Standard PodClique with label
  - PodClique without label (parse from name)
  - Invalid name format (returns error)

**Motivation:** Users think in terms of replicas (replica 0, replica 1), not PodGang names. This helper provides that context.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 2.3: Create Helper for PodCliqueSet Pending Pod Summary
**File:** `internal/controller/podcliqueset/components/podgang/syncflow.go`

- [ ] Add function to summarize which PodCliques have pending pods:
  ```go
  // getPendingPodsSummary returns a summary of pending pods grouped by PodClique
  // Returns map of PodClique name to pending pod count
  func getPendingPodsSummary(sc *syncContext, podGang podGangInfo) map[string]int
  ```
- [ ] Use existing logic from `getPodsPendingCreationOrAssociation()`
- [ ] Group by PodClique for better event messages
- [ ] Add tests

**Motivation:** Instead of saying "waiting for 5 pods," we can say "waiting for 3 pods in my-app-0-worker, 2 in my-app-0-ps."

**Status:** Not Started  
**Blocker:** None  
**Notes:**

---

## Phase 3: Event Recording in PodClique Controller

### Task 3.1: Add Events for Gang Formation Waiting
**File:** `internal/controller/podclique/components/pod/syncflow.go`  
**Function:** `checkAndRemovePodSchedulingGates()` (around line 260)

- [ ] When pod waiting for PodGang assignment, emit event:
  ```go
  r.eventRecorder.Eventf(sc.pclq, corev1.EventTypeNormal, 
      constants.ReasonScheduleGateWaitingGangFormation,
      "Pod %s schedule gate blocked: waiting for gang formation to complete (all pods in PodClique %s must be created and tracked)",
      p.Name, sc.pclq.Name)
  ```
- [ ] Add deduplication logic to avoid event spam:
  - Only emit if last event was >1 minute ago
  - Track last event time in component state or use annotations
- [ ] Update existing log statement to include new context

**Motivation:** Operators see when pods are in the initial gang formation phase, understand this is expected, and know what's being waited on.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 3.2: Add Events for Base PodClique Waiting
**File:** `internal/controller/podclique/components/pod/syncflow.go`  
**Function:** `checkAndRemovePodSchedulingGates()` (around line 267)

- [ ] When scaled gang pod waiting for base, get replica index:
  ```go
  replicaIndex, err := getReplicaIndexFromPodClique(sc.pclq)
  ```
- [ ] Get blocking PodClique details:
  ```go
  blockingInfo, err := r.getBlockingPodCliquesInfo(sc.ctx, logger, sc.pclq.Namespace, basePodGangName)
  ```
- [ ] Emit event with context:
  - If `blockingInfo` available and non-empty:
    ```go
    r.eventRecorder.Eventf(sc.pclq, corev1.EventTypeNormal,
        constants.ReasonScheduleGateWaitingBasePodCliques,
        "Pod %s schedule gate blocked: waiting for base PodCliques in replica %d to schedule: %s",
        p.Name, replicaIndex, strings.Join(blockingInfo, ", "))
    ```
  - If `blockingInfo` empty (all scheduled but PodGang not marked ready yet):
    ```go
    r.eventRecorder.Eventf(sc.pclq, corev1.EventTypeNormal,
        constants.ReasonScheduleGateWaitingBasePodCliques,
        "Pod %s schedule gate blocked: waiting for base PodCliques in replica %d to become ready",
        p.Name, replicaIndex)
    ```
  - If error getting blocking info (fallback):
    ```go
    r.eventRecorder.Eventf(sc.pclq, corev1.EventTypeNormal,
        constants.ReasonScheduleGateWaitingBasePodCliques,
        "Pod %s schedule gate blocked: waiting for base PodCliques in replica %d to schedule",
        p.Name, replicaIndex)
    ```
- [ ] Add error handling - don't let event recording block reconciliation
- [ ] Add deduplication logic

**Motivation:** This is the most valuable event - it tells operators exactly which PodCliques are blocking scheduling and their status.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 3.3: Add Events for Successful Gate Removal
**File:** `internal/controller/podclique/components/pod/syncflow.go`  
**Function:** `checkAndRemovePodSchedulingGates()` (around line 279)

- [ ] After successful `Patch()` to remove gate, emit event:
  ```go
  r.eventRecorder.Eventf(sc.pclq, corev1.EventTypeNormal,
      constants.ReasonScheduleGateRemoved,
      "Removed schedule gate from pod %s - pod is now eligible for scheduling",
      p.Name)
  ```
- [ ] Update the success log message to include more context

**Motivation:** Confirms the happy path - operators can see gates being removed as expected.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 3.4: Handle Event Recording Errors
**All event recording in Phase 3**

- [ ] Wrap all `eventRecorder.Eventf()` calls in defer/recover or error handling
- [ ] Log errors but don't fail reconciliation
- [ ] Add metric/counter for event recording failures (optional but recommended)

**Motivation:** Event recording should never break reconciliation logic.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

---

## Phase 4: Event Recording in PodCliqueSet Controller

### Task 4.1: Add Events for Pods Pending Creation
**File:** `internal/controller/podcliqueset/components/podgang/syncflow.go`  
**Function:** `createOrUpdatePodGangs()` (around line 402)

- [ ] When skipping PodGang creation, get summary of pending pods:
  ```go
  pendingSummary := getPendingPodsSummary(sc, podGang)
  ```
- [ ] Build detailed message if possible:
  ```go
  if len(pendingSummary) > 0 {
      details := []string{}
      for pclqName, count := range pendingSummary {
          details = append(details, fmt.Sprintf("%s: %d pods", pclqName, count))
      }
      r.eventRecorder.Eventf(sc.pcs, corev1.EventTypeNormal,
          constants.ReasonPodsPendingCreation,
          "Replica gang formation blocked: waiting for pods to be created (%s)",
          strings.Join(details, ", "))
  } else {
      // Fallback if we can't get details
      r.eventRecorder.Eventf(sc.pcs, corev1.EventTypeNormal,
          constants.ReasonPodsPendingCreation,
          "Replica gang formation blocked: waiting for %d pods to be created or assigned",
          numPendingPods)
  }
  ```
- [ ] Parse replica index from PodGang name and include in event
- [ ] Add deduplication logic

**Motivation:** Operators understand why PodGang isn't created yet and which PodCliques need attention.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 4.2: Add Events for Gang Formation Complete
**File:** `internal/controller/podcliqueset/components/podgang/syncflow.go`  
**Function:** `createOrUpdatePodGang()` (around line 482)

- [ ] After successful PodGang creation/update, emit completion event:
  ```go
  // Parse replica index from PodGang name
  replicaIndex := extractReplicaIndexFromPodGangName(pgInfo.fqn)
  
  r.eventRecorder.Eventf(sc.pcs, corev1.EventTypeNormal,
      constants.ReasonGangFormationComplete,
      "Replica %d gang formation complete: all %d PodCliques have required pods created and tracked",
      replicaIndex, len(pgInfo.pclqs))
  ```
- [ ] Add helper function `extractReplicaIndexFromPodGangName()` if needed
- [ ] Only emit on first creation, not on updates (track in syncContext)

**Motivation:** Positive feedback showing progress - operators see when gang formation succeeds.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

---

## Phase 5: Enhanced Status Conditions

### Task 5.1: Enhance PodCliqueScheduled Condition Message
**File:** `internal/controller/podclique/reconcilestatus.go`  
**Function:** `computePodCliqueScheduledCondition()` (around line 236)

- [ ] When condition is `False`, include schedule-gated count:
  ```go
  if pclq.Status.ScheduledReplicas < minAvailable {
      return metav1.Condition{
          Type:   constants.ConditionTypePodCliqueScheduled,
          Status: metav1.ConditionFalse,
          Reason: constants.ConditionReasonInsufficientScheduledPods,
          Message: fmt.Sprintf(
              "Insufficient scheduled pods. expected at least: %d, scheduled: %d, schedule-gated: %d, total replicas: %d",
              minAvailable, 
              pclq.Status.ScheduledReplicas, 
              pclq.Status.ScheduleGatedReplicas,
              pclq.Status.Replicas),
          LastTransitionTime: now,
      }
  }
  ```
- [ ] Update success message to include scheduled count:
  ```go
  return metav1.Condition{
      Type:   constants.ConditionTypePodCliqueScheduled,
      Status: metav1.ConditionTrue,
      Reason: constants.ConditionReasonSufficientScheduledPods,
      Message: fmt.Sprintf(
          "Sufficient scheduled pods found. expected at least: %d, scheduled: %d",
          minAvailable, scheduledReplicas),
      LastTransitionTime: now,
  }
  ```

**Motivation:** Status conditions should provide actionable data - seeing "3 schedule-gated" immediately tells operators to check schedule gate events.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 5.2: Enhance MinAvailableBreached Condition Message
**File:** `internal/controller/podclique/reconcilestatus.go`  
**Function:** `computeMinAvailableBreachedCondition()` (around line 177)

- [ ] Add schedule-gated count to `InsufficientScheduledPods` reason:
  ```go
  if scheduledReplicas < minAvailable {
      return metav1.Condition{
          Type:    constants.ConditionTypeMinAvailableBreached,
          Status:  metav1.ConditionFalse,
          Reason:  constants.ConditionReasonInsufficientScheduledPods,
          Message: fmt.Sprintf(
              "Insufficient scheduled pods. expected at least: %d, scheduled: %d, schedule-gated: %d",
              minAvailable, scheduledReplicas, pclq.Status.ScheduleGatedReplicas),
          LastTransitionTime: now,
      }
  }
  ```
- [ ] Similar enhancement for `InsufficientReadyPods` message

**Motivation:** MinAvailableBreached is a critical condition - operators need all context to diagnose.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

---

## Phase 6: Testing

### Task 6.1: Unit Tests for Helper Functions

**Files:**
- `internal/controller/podclique/components/pod/syncflow_test.go`
- `internal/controller/podcliqueset/components/podgang/syncflow_test.go`

- [ ] Test `getBlockingPodCliquesInfo()`:
  - All PodCliques scheduled → returns empty slice
  - Some PodCliques blocking → returns correct formatted strings
  - Multiple PodCliques blocking → includes all in result
  - PodGang not found → returns appropriate error
  - PodClique not found → skips gracefully
  - API errors → propagates error
- [ ] Test `getReplicaIndexFromPodClique()`:
  - PodClique with label → extracts correct index
  - PodClique without label → parses from name
  - Invalid format → returns error
- [ ] Test `getPendingPodsSummary()`:
  - No pending pods → returns empty map
  - Pending pods in one PodClique → correct count
  - Pending pods in multiple PodCliques → all counted correctly

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 6.2: Integration Tests for Event Recording

**File:** `e2e/schedule_gate_events_test.go` (new file)

- [ ] Create test that verifies events are recorded during schedule gate operations
- [ ] Test scenario: Base gang formation
  - Create PodCliqueSet with 1 PodClique
  - Verify `ScheduleGateWaitingGangFormation` event appears
  - Complete gang formation
  - Verify `ScheduleGateRemoved` events appear
  - Verify `GangFormationComplete` event appears
- [ ] Test scenario: Scaled gang waiting for base
  - Create PodCliqueSet with ScalingGroup
  - Verify base gang schedules first
  - Verify scaled gang pods emit `ScheduleGateWaitingBasePodCliques` events
  - Verify events include correct PodClique names
- [ ] Test scenario: Partial base gang scheduling
  - Create multi-clique base gang
  - Have some PodCliques schedule but not others
  - Verify events reference specific blocking PodCliques

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 6.3: Manual Testing Checklist

- [ ] Deploy simple PodCliqueSet (1 replica, 1 PodClique)
  - Verify gang formation events appear
  - Verify schedule gate removal events appear
- [ ] Deploy PodCliqueSet with multiple PodCliques per replica
  - Verify events reference correct PodCliques
  - Verify blocking PodClique details are accurate
- [ ] Deploy PodCliqueSet with ScalingGroup
  - Verify base gang completes first
  - Verify scaled gang events reference base PodCliques
  - Verify specific blocking information appears
- [ ] Create resource-constrained scenario
  - Verify events clearly show which PodCliques can't schedule
  - Verify event count matches expected (not spamming)
- [ ] Check `kubectl describe podclique <name>` output readability
- [ ] Check `kubectl describe podcliqueset <name>` output readability
- [ ] Check `kubectl get events --sort-by='.lastTimestamp'` for timeline clarity

**Status:** Not Started  
**Blocker:** None  
**Notes:**

---

## Phase 7: Documentation

### Task 7.1: Update Debugging Documentation

- [ ] Create/update `.agents/debugging-schedule-gates.md` with:
  - Overview of new events
  - Debugging workflow using events
  - Example event outputs
  - Common scenarios and their event patterns
- [ ] Add section on interpreting schedule-gated replica counts in conditions

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 7.2: Update Operator Documentation

- [ ] Update main README or operator documentation with:
  - Brief overview of schedule gate events
  - Link to debugging guide
  - Example troubleshooting commands
- [ ] Add event reference table:
  | Event Reason | Meaning | Action |
  |--------------|---------|--------|
  | ScheduleGateWaitingGangFormation | Pods waiting for gang to form | Wait for all pods in PodClique to be created |
  | ScheduleGateWaitingBasePodCliques | Scaled gang waiting for base | Check referenced base PodCliques |
  | ScheduleGateRemoved | Gate removed successfully | Pod can now be scheduled |
  | PodsPendingCreation | Gang formation waiting for pod creation | Check PodClique controllers |
  | GangFormationComplete | Gang successfully formed | Normal progression |

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 7.3: Add Code Comments

- [ ] Add comprehensive godoc comments to all new functions
- [ ] Add inline comments explaining event deduplication logic
- [ ] Add comments in constants.go explaining when each event is emitted

**Status:** Not Started  
**Blocker:** None  
**Notes:**

---

## Phase 8: Optional Enhancements

### Task 8.1: Event Deduplication and Rate Limiting

- [ ] Implement event deduplication mechanism:
  - Track last event time per (reason, message pattern)
  - Only emit if >1 minute since last similar event
  - Use annotation or in-memory cache
- [ ] Add configuration for event rate limiting
- [ ] Add metric tracking event emission rate

**Motivation:** Prevent event spam during reconciliation loops while still providing visibility.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 8.2: Aggregate Summary Events

- [ ] Add periodic summary event at PodCliqueSet level:
  ```go
  "Replica 0 scheduling status: 2/3 PodCliques scheduled. Blocked: my-app-0-worker (0/3 pods scheduled)"
  ```
- [ ] Emit only when state changes or every N minutes
- [ ] Include rollup of all replicas

**Motivation:** High-level view for operators managing complex deployments.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

### Task 8.3: Add Prometheus Metrics

- [ ] Add metric for pods with schedule gates:
  ```
  grove_schedule_gated_pods{podclique="name",reason="waiting_gang_formation"} 3
  ```
- [ ] Add metric for gang formation duration:
  ```
  grove_gang_formation_duration_seconds{podcliqueset="name",replica="0"} 45.2
  ```

**Motivation:** Alerting and dashboards for production monitoring.

**Status:** Not Started  
**Blocker:** None  
**Notes:**

---

## Acceptance Criteria

### Must Have (Required for completion)
- [ ] All Phase 1-5 tasks completed
- [ ] All unit tests passing
- [ ] At least one integration test passing
- [ ] Manual testing shows expected events in `kubectl describe` output
- [ ] Events reference only user-facing resources (no PodGang mentions)
- [ ] Blocking PodClique details included in events when available
- [ ] No regression in existing functionality

### Should Have (Highly desired)
- [ ] All Phase 6 tests implemented
- [ ] Phase 7 documentation completed
- [ ] Event deduplication implemented (Phase 8.1)
- [ ] No noticeable performance impact from event recording

### Nice to Have (Future enhancements)
- [ ] Aggregate summary events (Phase 8.2)
- [ ] Prometheus metrics (Phase 8.3)
- [ ] Event recording configurable via operator config

---

## Estimated Effort

- **Phase 1:** 1 hour
- **Phase 2:** 4-6 hours (includes testing)
- **Phase 3:** 6-8 hours (includes event logic and error handling)
- **Phase 4:** 3-4 hours
- **Phase 5:** 2-3 hours
- **Phase 6:** 8-12 hours (testing always takes longer)
- **Phase 7:** 3-4 hours
- **Phase 8:** 6-8 hours (optional)

**Total Core Implementation (Phases 1-7):** ~27-38 hours (~4-5 days)  
**With Optional Features (Phase 8):** ~33-46 hours (~5-6 days)

---

## Issues / Blockers

*None currently*

---

## Implementation Notes

### Key Design Decisions

1. **User-Facing Resources Only:** Events must reference PodCliques, PodCliqueScalingGroups, and Pods - never expose PodGang resources to end users.

2. **Graceful Degradation:** If we can't get detailed blocking information (API errors, missing resources), emit simpler generic events rather than failing.

3. **Event Deduplication:** Schedule gate checks happen on every reconciliation loop. Without deduplication, we'd spam events. Implement throttling from the start.

4. **Error Handling:** Event recording must never break reconciliation. Wrap all event calls with error handling that logs but doesn't fail.

5. **Performance:** Helper functions make extra API calls. Consider caching PodClique status if performance becomes an issue.

### Code Locations Reference

- **Schedule gate removal logic:** `internal/controller/podclique/components/pod/syncflow.go:241-301`
- **PodGang creation logic:** `internal/controller/podcliqueset/components/podgang/syncflow.go:393-415`
- **PodClique status reconciliation:** `internal/controller/podclique/reconcilestatus.go:40-96`
- **Event constants:** `internal/constants/constants.go:36-98`
- **Existing event recording examples:** 
  - `internal/controller/podclique/components/pod/task.go:52-65`
  - `internal/controller/podcliqueset/components/podgang/syncflow.go:379-386`

### Testing Strategy

1. **Unit tests** validate helper functions work correctly with various inputs
2. **Integration tests** verify events are actually recorded during real reconciliation
3. **Manual tests** ensure events are readable and actionable for operators

### Documentation Files to Review

- `.agents/general_index.md` - Codebase structure
- `.agents/status-and-events-handling.md` - Current event patterns
- `.agents/gang-scheduling-and-termination.md` - Gang scheduling flow
- `.agents/startup-ordering.md` - Startup ordering and dependencies

---

## Questions / Decisions Needed

*Document any questions or decisions that need input as they arise*

---

## Changelog

**[DATE]** - Initial plan created  
**[DATE]** - [Add updates as implementation progresses]
