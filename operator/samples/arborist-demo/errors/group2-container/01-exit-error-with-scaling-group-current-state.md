# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `01-exit-error-with-scaling-group` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-exit-error-sg
  namespace: default
  labels:
    app: container-exit-error-sg
    demo: group2-container
    scenario: 01-exit-error-scaling-group
  generation: 1
  creationTimestamp: "2025-11-12T20:57:40Z"

status:
  availableReplicas: 0           # No replicas are available
  currentGenerationHash: 54989786d4ddd9c6d9fc
  observedGeneration: 1
  replicas: 1                    # Total replicas expected
  updatedReplicas: 0             # No replicas successfully updated
  # NOTE: No conditions array in status
  # NOTE: No lastErrors array in status
  # NOTE: No operational summary showing degraded state
```

### Events

| Type   | Reason                                | Age                      | From                    | Message                                                                         |
|--------|---------------------------------------|--------------------------|-------------------------|---------------------------------------------------------------------------------|
| Normal | PodCliqueScalingGroupCreateSuccessful | 13m (x9 over 13m)        | podcliqueset-controller | Created PodCliqueScalingGroup default/container-exit-error-sg-0-main            |
| Normal | PodGangCreateOrUpdateSuccessful       | 13m (x7 over 13m)        | podcliqueset-controller | Created/Updated PodGang default/container-exit-error-sg-0                       |
| Normal | PodCliqueCreateOrUpdateSuccessful     | 3m19s (x132 over 13m)    | podcliqueset-controller | PodClique default/container-exit-error-sg-0-health-checker created or updated successfully |

## 2. PodCliqueScalingGroup Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: container-exit-error-sg-0-main
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podcliquescalinggroup
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-exit-error-sg-0-main
    app.kubernetes.io/part-of: container-exit-error-sg
    grove.io/podcliqueset-replica-index: "0"
  generation: 1
  creationTimestamp: "2025-11-12T20:57:40Z"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-exit-error-sg
    uid: 109692a6-7f6a-428f-a040-6bde19c6d606

spec:
  cliqueNames:
  - api
  - frontend
  - worker
  minAvailable: 1
  replicas: 1

status:
  availableReplicas: 0          # No replicas are available
  conditions:
  - lastTransitionTime: "2025-11-12T20:57:43Z"
    message: 'Insufficient PodCliqueScalingGroup ready replicas, expected at least: 1, found: 0'
    reason: InsufficientAvailablePodCliqueScalingGroupReplicas
    status: "True"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 54989786d4ddd9c6d9fc
  observedGeneration: 1
  replicas: 1                   # Total replicas expected
  scheduledReplicas: 1          # Replica is scheduled
  updatedReplicas: 0            # No replicas successfully updated
  # NOTE: Has conditions showing MinAvailableBreached
  # NOTE: No lastErrors array showing which child PodClique is crashing
  # NOTE: Scheduled but not available - indicates runtime issue with one of the children
```

### Events

| Type   | Reason                    | Age | From                             | Message                                                                |
|--------|---------------------------|-----|----------------------------------|------------------------------------------------------------------------|
| Normal | PodCliqueCreateSuccessful | 13m | podcliquescalinggroup-controller | PodClique default/container-exit-error-sg-0-main-0-api created successfully |
| Normal | PodCliqueCreateSuccessful | 13m | podcliquescalinggroup-controller | PodClique default/container-exit-error-sg-0-main-0-worker created successfully |
| Normal | PodCliqueCreateSuccessful | 13m | podcliquescalinggroup-controller | PodClique default/container-exit-error-sg-0-main-0-frontend created successfully |

## 3. PodClique Level - api (PODS CRASHING - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-exit-error-sg-0-main-0-api
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-exit-error-sg-0-main-0-api
    app.kubernetes.io/part-of: container-exit-error-sg
    grove.io/pod-template-hash: d9b8684454fb569ddc9
    grove.io/podcliquescalinggroup: container-exit-error-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-exit-error-sg-0
  generation: 1
  creationTimestamp: "2025-11-12T20:57:40Z"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-exit-error-sg-0-main
    uid: e3dd047e-d448-4fb8-8e65-9876d433f7a6

spec:
  minAvailable: 2
  replicas: 2
  roleName: api

status:
  conditions:
  - lastTransitionTime: "2025-11-12T20:57:40Z"
    message: 'Sufficient scheduled pods found. expected at least: 2, found: 2'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T20:57:43Z"
    message: 'Insufficient ready or starting pods. expected at least: 2, found: 0'
    reason: InsufficientReadyPods
    status: "True"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 54989786d4ddd9c6d9fc
  currentPodTemplateHash: d9b8684454fb569ddc9
  observedGeneration: 1
  readyReplicas: 0              # No pods are ready (they are crashing)
  replicas: 2                   # Total replicas expected
  scheduleGatedReplicas: 0      # Pods are not schedule-gated
  scheduledReplicas: 2          # Pods are scheduled
  updatedReplicas: 2            # Pods created and up-to-date
  # NOTE: Has conditions showing MinAvailableBreached
  # NOTE: No lastErrors array showing crash information
  # NOTE: Pods are scheduled but crashing - this is a runtime error (CrashLoopBackOff)
```

### Events

| Type   | Reason              | Age | From                 | Message                                                      |
|--------|---------------------|-----|----------------------|--------------------------------------------------------------|
| Normal | PodCreateSuccessful | 13m | podclique-controller | Created Pod: container-exit-error-sg-0-main-0-api-24qh2      |
| Normal | PodCreateSuccessful | 13m | podclique-controller | Created Pod: container-exit-error-sg-0-main-0-api-kkss4      |

## 4. PodClique Level - frontend (RUNNING - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-exit-error-sg-0-main-0-frontend
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-exit-error-sg-0-main-0-frontend
    app.kubernetes.io/part-of: container-exit-error-sg
    grove.io/pod-template-hash: 589899b559ff46597897
    grove.io/podcliquescalinggroup: container-exit-error-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-exit-error-sg-0
  generation: 1
  creationTimestamp: "2025-11-12T20:57:40Z"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-exit-error-sg-0-main
    uid: e3dd047e-d448-4fb8-8e65-9876d433f7a6

spec:
  minAvailable: 2
  replicas: 2
  roleName: frontend

status:
  conditions:
  - lastTransitionTime: "2025-11-12T20:57:40Z"
    message: 'Sufficient scheduled pods found. expected at least: 2, found: 2'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T20:57:40Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 2, found: 2'
    reason: SufficientReadyPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 54989786d4ddd9c6d9fc
  currentPodTemplateHash: 589899b559ff46597897
  observedGeneration: 1
  readyReplicas: 2              # Pods are ready
  replicas: 2                   # Total replicas expected
  scheduleGatedReplicas: 0      # Pods are not schedule-gated
  scheduledReplicas: 2          # Pods are scheduled
  updatedReplicas: 2            # Pods created and up-to-date
  # NOTE: MinAvailableBreached is False (meaning minAvailable is met)
  # NOTE: This PodClique is healthy and running
```

### Events

| Type   | Reason              | Age | From                 | Message                                                       |
|--------|---------------------|-----|----------------------|---------------------------------------------------------------|
| Normal | PodCreateSuccessful | 13m | podclique-controller | Created Pod: container-exit-error-sg-0-main-0-frontend-kb59m  |
| Normal | PodCreateSuccessful | 13m | podclique-controller | Created Pod: container-exit-error-sg-0-main-0-frontend-d49p9  |

## 5. PodClique Level - worker (RUNNING - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-exit-error-sg-0-main-0-worker
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-exit-error-sg-0-main-0-worker
    app.kubernetes.io/part-of: container-exit-error-sg
    grove.io/pod-template-hash: 7dd76f6b74d89bcfc9d
    grove.io/podcliquescalinggroup: container-exit-error-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-exit-error-sg-0
  generation: 1
  creationTimestamp: "2025-11-12T20:57:40Z"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-exit-error-sg-0-main
    uid: e3dd047e-d448-4fb8-8e65-9876d433f7a6

spec:
  minAvailable: 1
  replicas: 1
  roleName: worker

status:
  conditions:
  - lastTransitionTime: "2025-11-12T20:57:40Z"
    message: 'Sufficient scheduled pods found. expected at least: 1, found: 1'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T20:57:40Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 1, found: 1'
    reason: SufficientReadyPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 54989786d4ddd9c6d9fc
  currentPodTemplateHash: 7dd76f6b74d89bcfc9d
  observedGeneration: 1
  readyReplicas: 1              # Pod is ready
  replicas: 1                   # Total replicas expected
  scheduleGatedReplicas: 0      # Pod is not schedule-gated
  scheduledReplicas: 1          # Pod is scheduled
  updatedReplicas: 1            # Pod created and up-to-date
  # NOTE: MinAvailableBreached is False (meaning minAvailable is met)
  # NOTE: This PodClique is healthy and running
```

### Events

| Type   | Reason              | Age | From                 | Message                                                      |
|--------|---------------------|-----|----------------------|--------------------------------------------------------------|
| Normal | PodCreateSuccessful | 13m | podclique-controller | Created Pod: container-exit-error-sg-0-main-0-worker-44lgg   |

## 6. PodClique Level - health-checker (RUNNING - Standalone, Not in Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-exit-error-sg-0-health-checker
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-exit-error-sg-0-health-checker
    app.kubernetes.io/part-of: container-exit-error-sg
    grove.io/pod-template-hash: 888dbf7cdcfd76cb974
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-exit-error-sg-0
  generation: 1
  creationTimestamp: "2025-11-12T20:57:40Z"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-exit-error-sg
    uid: 109692a6-7f6a-428f-a040-6bde19c6d606

spec:
  minAvailable: 1
  replicas: 1
  roleName: health-checker

status:
  conditions:
  - lastTransitionTime: "2025-11-12T20:57:41Z"
    message: 'Sufficient scheduled pods found. expected at least: 1, found: 1'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T20:57:40Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 1, found: 1'
    reason: SufficientReadyPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 54989786d4ddd9c6d9fc
  currentPodTemplateHash: 888dbf7cdcfd76cb974
  observedGeneration: 1
  readyReplicas: 1              # Pod is ready
  replicas: 1                   # Total replicas expected
  scheduleGatedReplicas: 0      # Pod is not schedule-gated
  scheduledReplicas: 1          # Pod is scheduled
  updatedReplicas: 1            # Pod created and up-to-date
  # NOTE: MinAvailableBreached is False (meaning minAvailable is met)
  # NOTE: This PodClique is NOT part of the scaling group and is healthy
```

### Events

| Type   | Reason              | Age | From                 | Message                                                             |
|--------|---------------------|-----|----------------------|---------------------------------------------------------------------|
| Normal | PodCreateSuccessful | 13m | podclique-controller | Created Pod: container-exit-error-sg-0-health-checker-gkbj9         |
