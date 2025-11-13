# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `02-missing-binary-with-scaling-group` test scenario.

## Scenario Description
Missing Binary with Scaling Group - Backend container attempts to execute a binary that doesn't exist, part of a scaling group with frontend and cache.

---

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-missing-binary-sg
  namespace: default

status:
  availableReplicas: 0
  currentGenerationHash: 558dfc678fbd6bc8d955
  observedGeneration: 1
  replicas: 1
  updatedReplicas: 0
  # NOTE: No conditions, no error details
```

### Events
- Normal: PodGangCreateOrUpdateSuccessful
- Normal: PodCliqueCreateOrUpdateSuccessful (monitoring)
- Normal: PodCliqueScalingGroupCreateSuccessful

---

## 2. PodCliqueScalingGroup Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: container-missing-binary-sg-0-main
  namespace: default

spec:
  cliqueNames: [backend, cache, frontend]
  minAvailable: 1
  replicas: 1

status:
  availableReplicas: 0
  conditions:
  - lastTransitionTime: "2025-11-12T20:45:25Z"
    message: 'Insufficient PodCliqueScalingGroup ready replicas, expected at least: 1, found: 0'
    reason: InsufficientAvailablePodCliqueScalingGroupReplicas
    status: "True"
    type: MinAvailableBreached
  replicas: 1
  scheduledReplicas: 1
  updatedReplicas: 0
  # NOTE: Does NOT identify which PodClique (backend/cache/frontend) is failing
```

### Events
- Normal: PodCliqueCreateSuccessful (backend, cache, frontend)

---

## 3. PodClique Level Summary

### backend (FAILING - part of scaling group)
- readyReplicas: 0
- replicas: 2
- MinAvailableBreached: True

### cache (HEALTHY - part of scaling group)
- readyReplicas: 1
- replicas: 1
- MinAvailableBreached: False

### frontend (HEALTHY - part of scaling group)
- readyReplicas: 3
- replicas: 3
- MinAvailableBreached: False

### monitoring (HEALTHY - standalone)
- readyReplicas: 1
- replicas: 1
- MinAvailableBreached: False

---

## Summary

Same pattern as other scenarios:
- **PodCliqueSet**: Shows `availableReplicas: 0`, no error details
- **PodCliqueScalingGroup**: Shows breach, doesn't identify failing PodClique
- **PodClique**: Failing clique shows breach with `readyReplicas: 0`
- **Gap**: No propagation of container-level errors (missing binary) up the hierarchy
