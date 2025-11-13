# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `02-missing-binary` test scenario.

## Scenario Description
Missing Binary - Container attempts to execute a binary that doesn't exist. Expected: ContainerCannotRun or similar error state.

---

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-error-missing-binary
  namespace: default
  generation: 1
  observedGeneration: 1

status:
  availableReplicas: 0              # Shows unhealthy state
  currentGenerationHash: 94879d6d9f7b8cd4b8c
  observedGeneration: 1
  replicas: 1
  updatedReplicas: 0
  # NOTE: No conditions field
  # NOTE: No error information
```

### Events

| Type   | Reason                            | Age                | From                     | Message |
|--------|-----------------------------------|--------------------|--------------------------|---------
| Normal | PodGangCreateOrUpdateSuccessful   | 16s (x7 over 21s)  | podcliqueset-controller  | Created/Updated PodGang default/container-error-missing-binary-0 |
| Normal | PodCliqueCreateOrUpdateSuccessful | 11s (x9 over 21s)  | podcliqueset-controller  | PodClique default/container-error-missing-binary-0-ingress created or updated successfully |
| Normal | PodCliqueCreateOrUpdateSuccessful | 11s (x9 over 21s)  | podcliqueset-controller  | PodClique default/container-error-missing-binary-0-service created or updated successfully |

---

## 2. PodClique Level

### 2.1 PodClique: service (FAILING)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-missing-binary-0-service
  namespace: default
  generation: 1
  observedGeneration: 1

status:
  conditions:
  - lastTransitionTime: "2025-11-12T20:44:24Z"
    message: 'Sufficient scheduled pods found. expected at least: 2, found: 2'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T20:44:25Z"
    message: 'Insufficient ready or starting pods. expected at least: 2, found: 0'
    reason: InsufficientReadyPods
    status: "True"                  # Breach is active
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 94879d6d9f7b8cd4b8c
  currentPodTemplateHash: b6589ffb9f5d848bf6b
  readyReplicas: 0                   # No ready pods
  replicas: 2
  scheduleGatedReplicas: 0
  scheduledReplicas: 2
  updatedReplicas: 2
```

#### Events

| Type   | Reason              | Age | From                  | Message |
|--------|---------------------|-----|-----------------------|---------|
| Normal | PodCreateSuccessful | 26s | podclique-controller  | Created Pod: container-error-missing-binary-0-service-q8hcf |
| Normal | PodCreateSuccessful | 26s | podclique-controller  | Created Pod: container-error-missing-binary-0-service-tt9dh |

---

### 2.2 PodClique: ingress (HEALTHY)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-missing-binary-0-ingress
  namespace: default
  generation: 1
  observedGeneration: 1

status:
  conditions:
  - lastTransitionTime: "2025-11-12T20:44:24Z"
    message: 'Sufficient scheduled pods found. expected at least: 1, found: 1'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T20:44:24Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 1, found: 1'
    reason: SufficientReadyPods
    status: "False"                 # No breach (healthy)
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 94879d6d9f7b8cd4b8c
  currentPodTemplateHash: c5fbc846dcfd5d68f74
  readyReplicas: 1
  replicas: 1
  scheduleGatedReplicas: 0
  scheduledReplicas: 1
  updatedReplicas: 1
```

#### Events

| Type   | Reason              | Age | From                  | Message |
|--------|---------------------|-----|-----------------------|---------|
| Normal | PodCreateSuccessful | 27s | podclique-controller  | Created Pod: container-error-missing-binary-0-ingress-qh52s |

---

## Summary

### Current Error Reporting Behavior:

1. **PodCliqueSet Level**:
   - Shows `availableReplicas: 0` indicating problems
   - NO conditions, NO error details, NO indication of which PodClique is failing

2. **PodClique Level**:
   - Failing PodClique shows `MinAvailableBreached` condition
   - Message: "Insufficient ready or starting pods"
   - `readyReplicas: 0` shows the problem
   - Events only show pod creation, no container errors

3. **Information Gap**:
   - Pod-level container errors (missing binary) NOT propagated to PodClique
   - PodClique errors NOT propagated to PodCliqueSet
   - No indication of root cause (container cannot run due to missing binary)
