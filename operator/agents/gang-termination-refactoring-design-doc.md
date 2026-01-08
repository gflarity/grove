# Gang Termination Refactoring Design Doc

# Overview

This design document details the refactoring of the gang termination feature in the Grove operator to address critical bugs in breach detection and to simplify the termination behavior based on stakeholder feedback.

## Abbreviations

| Abbreviation | Full Name | Description |
|--------------|-----------|-------------|
| PCS | PodCliqueSet | Grove CRD that manages a set of PodCliques and PodCliqueScalingGroups |
| PC / PCLQ | PodClique | Grove CRD representing a group of related pods |
| PCSG | PodCliqueScalingGroup | Grove CRD that manages scaling of PodCliques |

## Motivation

The gang termination refactoring is driven by two key factors:

**Bug Fix:** The existing gang termination logic excludes unscheduled pods from breach calculations. This causes E2E tests to fail when they cordon nodes and kill pods to trigger breaches. The problematic code path:

```go
// If the number of scheduled pods is less than the minimum available, then minAvailable is not considered as breached.
// Consider a case where none of the PodCliques have been scheduled yet, then it should not cause the PodGang to be recreated all the time.
if scheduledReplicas < minAvailable {
    return metav1.Condition{
        Type:   constants.ConditionTypeMinAvailableBreached,
        Status: metav1.ConditionFalse,
        Reason: constants.ConditionReasonInsufficientScheduledPods,
        // ...
    }
}
```

This guard prevents legitimate breaches from being detected when pods are cordoned and killed, as the newly pending pods become "unscheduled."

**Stakeholder Feedback:** Based on discussions with SREs who actively manage AI infrastructure and workloads, we decided to:

1. **Keep the implementation simple** while ensuring correctness
2. **Protect startup scenarios** - initial startup of PodCliqueSet replicas should never trigger gang termination
3. **Only terminate degraded workloads** - only after workloads start successfully and subsequently degrade should they be terminated (after the configured delay)
4. **Disable by default** - gang termination should be an opt-in feature to prevent unexpected workload disruptions

## Background

### What is Gang Termination?

Gang termination is a Grove feature that ensures workload consistency by terminating an entire PodCliqueSet replica when its health degrades below acceptable thresholds. When the number of ready replicas in a PodClique or PCSG falls below the configured `MinAvailable` threshold for longer than the configured `TerminationDelay`, the entire replica is terminated and recreated.

### Current Behavior Problems

1. **Startup False Positives:** During initial startup, pods are not yet scheduled or ready. The current logic can incorrectly flag these as breaches, causing premature termination of workloads that haven't even started.

2. **Unscheduled Pod Exclusion Bug:** The guard meant to prevent startup false positives has the side effect of preventing legitimate breach detection when pods become unscheduled after a node failure.

3. **Default-On Behavior:** Gang termination is currently enabled by default with a 4-hour delay, which can cause unexpected workload disruptions.

# Goals

The following key goals guide this refactoring:

* **Transition-Based Detection:** `MinAvailableBreached` condition only becomes `True` when ready replicas **transitions** from `>= MinAvailable` to `< MinAvailable`. This naturally protects startup scenarios since there is no transition from a healthy state.

* **Include All Replicas:** Remove the guard that excludes unscheduled pods from breach calculation. Breaches are now purely based on `ReadyReplicas` vs `MinAvailable`.

* **Disabled by Default:** Gang termination is disabled unless explicitly enabled at the PCS level by setting `terminationDelay`.

* **Hierarchical Configuration:** Support `terminationDelay` configuration at PCS, PCSG, and standalone PCLQ levels with a clear inheritance hierarchy.

* **LastTransitionTime-Based Timing:** Gang termination triggers when `MinAvailableBreached` has been `True` for longer than the applicable `TerminationDelay`, measured from the condition's `LastTransitionTime`.

## Scope and Limitations

**Limitations:**

- **PCS-Level Enablement Required:** Gang termination can only be enabled at the PCS level. Individual PCSGs and standalone PCLQs can override the delay but cannot enable gang termination independently.

- **PCSG-Owned PCLQs Cannot Override:** PCLQs that belong to a PCSG inherit their `terminationDelay` from either the PCSG or PCS and cannot specify their own.

- **Immutable After Creation:** Once a PCS is created with or without gang termination enabled, the PCS-level `terminationDelay` cannot be changed (immutability enforced by validation webhook).

# Design Details

## Enabling Gang Termination

Gang termination is disabled by default. To enable it, set `terminationDelay` at the PCS level:

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: my-pcs
spec:
  replicas: 3
  template:
    terminationDelay: 4h  # Gang termination enabled with 4-hour delay
    cliques:
      - name: worker
        spec:
          replicas: 4
          minAvailable: 3
          # ...
```

When `terminationDelay` is **not set** (nil), gang termination is completely disabled for the entire PodCliqueSet:

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: my-pcs
spec:
  replicas: 3
  template:
    # No terminationDelay - gang termination disabled
    cliques:
      - name: worker
        spec:
          replicas: 4
          minAvailable: 3
          # ...
```

## TerminationDelay Hierarchy

When gang termination is enabled at the PCS level, individual PCSGs and standalone PCLQs can override the delay:

```
Resolution Order:
├── Standalone PCLQ:  PCLQ.Spec.TerminationDelay  →  PCS.Spec.Template.TerminationDelay
├── PCSG-owned PCLQ:  PCSG.Spec.TerminationDelay  →  PCS.Spec.Template.TerminationDelay
└── PCSG:             PCSG.Spec.TerminationDelay  →  PCS.Spec.Template.TerminationDelay
```

### Example with PCSG Override

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: my-pcs
spec:
  replicas: 3
  template:
    terminationDelay: 4h  # Default for entire PCS
    cliques:
      - name: worker
        spec:
          replicas: 4
          minAvailable: 3
      - name: driver
        spec:
          replicas: 1
          minAvailable: 1
    podCliqueScalingGroups:
      - name: scaling-group
        cliqueNames:
          - worker
        replicas: 2
        minAvailable: 2
        terminationDelay: 1h  # Override: faster termination for this PCSG
```

### Example with Standalone PCLQ Override

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: my-pcs
spec:
  replicas: 3
  template:
    terminationDelay: 4h  # Default for entire PCS
    cliques:
      - name: worker
        spec:
          replicas: 4
          minAvailable: 3
        terminationDelay: 30m  # Override for this standalone PCLQ
      - name: driver
        spec:
          replicas: 1
          minAvailable: 1
        # Uses PCS default: 4h
```

## Transition-Based Breach Detection

The core change to breach detection logic ensures that `MinAvailableBreached` only becomes `True` when there is an actual transition from healthy to unhealthy state.

### Algorithm

```go
func computeMinAvailableBreachedCondition(pclq *PodClique, oldReadyReplicas int32) metav1.Condition {
    existingCondition := meta.FindStatusCondition(pclq.Status.Conditions, ConditionTypeMinAvailableBreached)
    now := metav1.Now()
    
    // Helper to preserve LastTransitionTime when status doesn't change
    getLastTransitionTime := func(newStatus metav1.ConditionStatus) metav1.Time {
        if existingCondition != nil && existingCondition.Status == newStatus {
            return existingCondition.LastTransitionTime // Preserve existing time
        }
        return now // Status is changing, use new time
    }
    
    minAvailable := int(*pclq.Spec.MinAvailable)
    currentReadyReplicas := int(pclq.Status.ReadyReplicas)
    
    // During an update, the breach condition is Unknown - we cannot be in breach during an update.
    if IsPCLQUpdateInProgress(pclq) {
        return Condition{
            Status: Unknown, 
            Reason: "UpdateInProgress",
            Message: "Update is in progress",
            LastTransitionTime: now,
        }
    }
    
    // Check current state
    if currentReadyReplicas >= minAvailable {
        // Healthy - set/maintain False condition
        return Condition{
            Status: False, 
            Reason: "SufficientReadyPods",
            LastTransitionTime: getLastTransitionTime(False),
        }
    }
    
    // Current ready < minAvailable
    // Only set to True if this is a TRANSITION from healthy to unhealthy
    if int(oldReadyReplicas) >= minAvailable {
        // Transition detected: was healthy, now unhealthy - this IS a status change
        return Condition{Status: True, Reason: "InsufficientReadyPods", LastTransitionTime: now}
    }
    
    // Was already unhealthy - preserve existing True condition
    if existingCondition != nil && existingCondition.Status == metav1.ConditionTrue {
        // Maintain existing True condition (preserve LastTransitionTime for termination delay calculation)
        return *existingCondition
    }
    
    // Edge case: was unhealthy but condition wasn't True (e.g., was Unknown during update)
    // Now that update is complete, set to False since no transition occurred
    return Condition{
        Status: False, 
        Reason: "NoTransitionDetected",
        LastTransitionTime: getLastTransitionTime(False),
    }
}
```

### Why This Works for Startup

During startup:
1. Initial state: `oldReadyReplicas = 0`, `currentReadyReplicas = 0`, `minAvailable = 3`
2. Condition check: `currentReadyReplicas (0) >= minAvailable (3)` → **False**
3. Transition check: `oldReadyReplicas (0) >= minAvailable (3)` → **False**
4. Result: **No breach** - the workload was never healthy, so no transition occurred

After startup and subsequent degradation:
1. Healthy state: `oldReadyReplicas = 3`, `currentReadyReplicas = 3`, `minAvailable = 3`
2. Pod failure: `oldReadyReplicas = 3`, `currentReadyReplicas = 2`, `minAvailable = 3`
3. Condition check: `currentReadyReplicas (2) >= minAvailable (3)` → **False**
4. Transition check: `oldReadyReplicas (3) >= minAvailable (3)` → **True**
5. Result: **Breach detected** - transition from healthy to unhealthy

## Removing the Unscheduled Pods Guard

The following code is **removed** from both PodClique and PCSG status reconciliation:

```go
// REMOVED: This guard was causing the E2E test failures
if scheduledReplicas < minAvailable {
    return metav1.Condition{
        Type:   constants.ConditionTypeMinAvailableBreached,
        Status: metav1.ConditionFalse,
        Reason: constants.ConditionReasonInsufficientScheduledPods,
        // ...
    }
}
```

With transition-based detection, this guard is no longer needed. Startup scenarios are protected by the transition check, not by excluding unscheduled pods.

## Gang Termination Timing

Gang termination uses the `LastTransitionTime` of the `MinAvailableBreached` condition to determine when to terminate:

```go
GangTerminate() IF:
    MinAvailableBreached.Status == True  AND
    (Time.Now() - MinAvailableBreached.LastTransitionTime) > applicableTerminationDelay
```

Key points:
- `LastTransitionTime` is set **only** when the condition **status** transitions (e.g., False→True, True→False)
- `LastTransitionTime` is **preserved** when status remains unchanged (e.g., True→True, False→False)
- The timer starts from the moment of the initial transition to `True`, not from repeated reconciliations
- `applicableTerminationDelay` is resolved using the hierarchy (PCLQ/PCSG → PCS)

### LastTransitionTime Preservation

Following Kubernetes condition semantics, `LastTransitionTime` is only updated when the condition's **status** actually changes. This is critical for accurate gang termination timing:

| Scenario | Status Change | LastTransitionTime |
|----------|--------------|-------------------|
| Healthy → Unhealthy | False → True | **Updated** (new timestamp) |
| Unhealthy → Healthy | True → False | **Updated** (new timestamp) |
| Stays Healthy | False → False | **Preserved** (existing timestamp) |
| Stays Unhealthy | True → True | **Preserved** (existing timestamp) |
| Update starts | Any → Unknown | **Updated** (status changed) |
| Update ends (no transition) | Unknown → False | **Updated** (status changed) |

This ensures that:
1. The gang termination delay timer is calculated correctly from the moment the breach actually occurred
2. The timer is not reset on each reconciliation when the workload remains unhealthy
3. Workloads that remain stable (healthy or unhealthy) don't have their timestamps updated unnecessarily

### Update In Progress Disables Breach Detection

When a rolling update is in progress, the breach condition status becomes `Unknown` - we cannot be in breach during an update. After the update completes, the workload must become healthy (`ReadyReplicas >= MinAvailable`) before a new breach can be detected.

## API Changes

### PodCliqueSetTemplateSpec

The existing `TerminationDelay` field behavior changes:

```go
type PodCliqueSetTemplateSpec struct {
    // ... existing fields ...
    
    // TerminationDelay is the delay after which gang termination will be triggered.
    // If not set (nil), gang termination is DISABLED for the entire PodCliqueSet.
    // Must be > 0 if set.
    // +optional
    TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
}
```

### PodCliqueScalingGroupConfig

New field added:

```go
type PodCliqueScalingGroupConfig struct {
    // ... existing fields ...
    
    // TerminationDelay overrides the PCS-level terminationDelay for this scaling group.
    // If not specified, inherits from PodCliqueSetTemplateSpec.TerminationDelay.
    // Only valid when PCS-level terminationDelay is set (gang termination enabled).
    // +optional
    TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
}
```

### PodCliqueTemplateSpec

New field added for standalone PCLQs:

```go
type PodCliqueTemplateSpec struct {
    // ... existing fields ...
    
    // TerminationDelay overrides the PCS-level terminationDelay for this standalone PodClique.
    // Only valid for PodCliques that are NOT part of a PodCliqueScalingGroup.
    // If this PodClique is part of a PCSG, this field must not be set.
    // Only valid when PCS-level terminationDelay is set (gang termination enabled).
    // +optional
    TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
}
```

### PodCliqueScalingGroupSpec

New field added:

```go
type PodCliqueScalingGroupSpec struct {
    // ... existing fields ...
    
    // TerminationDelay is the delay after which gang termination will be triggered for this scaling group.
    // Copied from PodCliqueScalingGroupConfig.TerminationDelay or inherited from PCS.
    // +optional
    TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
}
```

### PodCliqueSpec

New field added:

```go
type PodCliqueSpec struct {
    // ... existing fields ...
    
    // TerminationDelay is the delay after which gang termination will be triggered.
    // Only set for standalone PodCliques (not part of a PCSG).
    // +optional
    TerminationDelay *metav1.Duration `json:"terminationDelay,omitempty"`
}
```

## Validation Rules

The validation webhook enforces the following rules:

1. **PCS-level terminationDelay is optional**
   - If not set (nil), gang termination is disabled
   - If set, must be > 0

2. **PCSG/PCLQ terminationDelay requires PCS enablement**
   - If PCS-level `terminationDelay` is nil, PCSG `terminationDelay` MUST NOT be set
   - If PCS-level `terminationDelay` is nil, standalone PCLQ `terminationDelay` MUST NOT be set

3. **PCSG-owned PCLQs cannot override**
   - If a PCLQ is part of a PCSG (appears in any `PodCliqueScalingGroupConfig.CliqueNames`), it MUST NOT have `terminationDelay` set

4. **All delays must be positive**
   - If `terminationDelay` is set anywhere, it MUST be > 0

## Defaulting Changes

The defaulting webhook is updated to:

1. **Remove the 4-hour default** - `terminationDelay` is no longer defaulted
2. **Leave nil values as nil** - no automatic population of `terminationDelay` at any level

## Helper Functions

New utility functions are added to resolve effective termination delays:

```go
// IsGangTerminationEnabled returns true if gang termination is enabled for the PodCliqueSet.
func IsGangTerminationEnabled(pcs *PodCliqueSet) bool {
    return pcs.Spec.Template.TerminationDelay != nil
}

// GetEffectiveTerminationDelayForPCLQ returns the effective termination delay for a PodClique.
// Returns (duration, true) if gang termination is enabled, (0, false) if disabled.
func GetEffectiveTerminationDelayForPCLQ(pcs *PodCliqueSet, pclq *PodClique) (time.Duration, bool) {
    if pcs.Spec.Template.TerminationDelay == nil {
        return 0, false
    }
    if pclq.Spec.TerminationDelay != nil {
        return pclq.Spec.TerminationDelay.Duration, true
    }
    return pcs.Spec.Template.TerminationDelay.Duration, true
}

// GetEffectiveTerminationDelayForPCSG returns the effective termination delay for a PCSG.
// Returns (duration, true) if gang termination is enabled, (0, false) if disabled.
func GetEffectiveTerminationDelayForPCSG(pcs *PodCliqueSet, pcsg *PodCliqueScalingGroup) (time.Duration, bool) {
    if pcs.Spec.Template.TerminationDelay == nil {
        return 0, false
    }
    if pcsg.Spec.TerminationDelay != nil {
        return pcsg.Spec.TerminationDelay.Duration, true
    }
    return pcs.Spec.Template.TerminationDelay.Duration, true
}
```

## Controller Changes

### Early-Exit for Disabled Gang Termination

At the start of gang termination logic in both PCS replica and PCSG controllers:

```go
if !componentutils.IsGangTerminationEnabled(pcs) {
    // Gang termination is disabled, skip termination logic
    return
}
```

### Per-Component Termination Delay Resolution

When evaluating gang termination, each component (standalone PCLQ or PCSG) uses its effective termination delay:

```go
// For standalone PCLQs
effectiveDelay, enabled := componentutils.GetEffectiveTerminationDelayForPCLQ(pcs, &pclq)
if !enabled {
    continue
}
// Use effectiveDelay for this PCLQ

// For PCSGs
effectiveDelay, enabled := componentutils.GetEffectiveTerminationDelayForPCSG(pcs, &pcsg)
if !enabled {
    continue
}
// Use effectiveDelay for this PCSG
```

## Observability

The existing observability mechanisms continue to work:

- **Kubernetes Events:** Breach events are emitted on the PCS resource
- **Conditions:** `MinAvailableBreached` condition is still set, even when gang termination is disabled
- **Logs:** Detailed information is logged by the operator

Administrators can monitor the `MinAvailableBreached` condition to understand workload health even when gang termination is disabled:

```bash
kubectl describe pcs my-pcs
# Conditions:
#   Type                    Status  Reason                      Message
#   MinAvailableBreached    True    InsufficientReadyPods       Ready replicas below minimum available
```

# Migration Guide

## Existing PodCliqueSets with Default Termination Delay

If you have existing PodCliqueSets that relied on the default 4-hour termination delay, you must explicitly set `terminationDelay` to maintain gang termination behavior:

**Before (implicit 4-hour default):**
```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: my-pcs
spec:
  template:
    # No terminationDelay - previously defaulted to 4h
    cliques:
      - name: worker
        # ...
```

**After (explicit 4-hour delay):**
```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: my-pcs
spec:
  template:
    terminationDelay: 4h  # Explicitly enable gang termination
    cliques:
      - name: worker
        # ...
```

## Workloads That Should Not Use Gang Termination

For workloads where you don't want automatic gang termination, simply don't set `terminationDelay`:

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: my-batch-job
spec:
  template:
    # No terminationDelay - gang termination disabled
    cliques:
      - name: worker
        # ...
```

# Summary of Key Design Decisions

| Decision | Description |
|----------|-------------|
| Transition-Based Detection | `MinAvailableBreached = True` only when transitioning from healthy to unhealthy |
| Unscheduled Replicas Included | Remove guard that excluded unscheduled pods from breach calculation |
| Disabled by Default | Gang termination requires explicit `terminationDelay` at PCS level |
| Hierarchical Configuration | PCS → PCSG → standalone PCLQ override chain |
| LastTransitionTime Timing | Termination delay measured from condition transition, not reconciliation time |
| LastTransitionTime Preservation | `LastTransitionTime` only updated when condition **status** changes, not on every reconciliation |
| Update Disables Breach | When update is in progress, breach condition is `Unknown`; workload must become healthy before new breach can be detected |
| PCSG-Owned PCLQs Inherit | Cannot specify their own `terminationDelay` |


