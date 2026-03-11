# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `01-exit-error` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-error-exit
  namespace: default
  labels:
    app: container-error-exit
    demo: group2-container
    scenario: 01-exit-error
  generation: 1
  creationTimestamp: "2025-11-12T21:09:23Z"

status:
  availableReplicas: 0           # No replicas are available
  currentGenerationHash: 5b77b8888676d858c9f
  observedGeneration: 1
  replicas: 1                    # Total replicas expected
  updatedReplicas: 0             # No replicas successfully updated
  # NOTE: No conditions array in status
  # NOTE: No lastErrors array in status
  # NOTE: No operational summary showing degraded state
```

### Events

| Type   | Reason                            | Age                | From                    | Message                                                                          |
|--------|-----------------------------------|--------------------|-------------------------|----------------------------------------------------------------------------------|
| Normal | PodCliqueCreateOrUpdateSuccessful | 23s (x7 over 25s)  | podcliqueset-controller | PodClique default/container-error-exit-0-database created or updated successfully |
| Normal | PodCliqueCreateOrUpdateSuccessful | 23s (x7 over 25s)  | podcliqueset-controller | PodClique default/container-error-exit-0-processor created or updated successfully |
| Normal | PodCliqueCreateOrUpdateSuccessful | 23s (x7 over 25s)  | podcliqueset-controller | PodClique default/container-error-exit-0-queue created or updated successfully    |
| Normal | PodGangCreateOrUpdateSuccessful   | 23s (x4 over 25s)  | podcliqueset-controller | Created/Updated PodGang default/container-error-exit-0                           |

## 2. PodClique Level - processor (PODS RUNNING BUT CRASHING)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-exit-0-processor
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-exit-0-processor
    app.kubernetes.io/part-of: container-error-exit
    grove.io/pod-template-hash: c9b6b54df4669699678
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-exit-0
  generation: 1
  creationTimestamp: "2025-11-12T21:09:23Z"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-error-exit
    uid: 84599c40-186c-4a39-97cb-68615991ead7

spec:
  minAvailable: 2
  replicas: 2
  roleName: processor

status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:09:23Z"
    message: 'Sufficient scheduled pods found. expected at least: 2, found: 2'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:09:25Z"
    message: 'Insufficient ready or starting pods. expected at least: 2, found: 0'
    reason: InsufficientReadyPods
    status: "True"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5b77b8888676d858c9f
  currentPodTemplateHash: c9b6b54df4669699678
  observedGeneration: 1
  readyReplicas: 0              # No pods are ready (they are crashing)
  replicas: 2                   # Total replicas expected
  scheduleGatedReplicas: 0      # Pods are not schedule-gated
  scheduledReplicas: 2          # Pods are scheduled
  updatedReplicas: 2            # Pods created and up-to-date
  # NOTE: Has conditions showing MinAvailableBreached
  # NOTE: No lastErrors array showing container crash information
  # NOTE: Pods are scheduled but crashing - this is a runtime error, not a creation error
```

### Events

| Type   | Reason              | Age | From                 | Message                                            |
|--------|---------------------|-----|----------------------|----------------------------------------------------|
| Normal | PodCreateSuccessful | 27s | podclique-controller | Created Pod: container-error-exit-0-processor-df8cx |
| Normal | PodCreateSuccessful | 27s | podclique-controller | Created Pod: container-error-exit-0-processor-cvrrj |

## 3. PodClique Level - database (RUNNING)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-exit-0-database
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-exit-0-database
    app.kubernetes.io/part-of: container-error-exit
    grove.io/pod-template-hash: 5887bddf8d74659d54ff
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-exit-0
  generation: 1
  creationTimestamp: "2025-11-12T21:09:23Z"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-error-exit
    uid: 84599c40-186c-4a39-97cb-68615991ead7

spec:
  minAvailable: 1
  replicas: 1
  roleName: database

status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:09:23Z"
    message: 'Sufficient scheduled pods found. expected at least: 1, found: 1'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:09:23Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 1, found: 1'
    reason: SufficientReadyPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5b77b8888676d858c9f
  currentPodTemplateHash: 5887bddf8d74659d54ff
  observedGeneration: 1
  readyReplicas: 1              # Pod is ready
  replicas: 1                   # Total replicas expected
  scheduleGatedReplicas: 0      # Pod is not schedule-gated
  scheduledReplicas: 1          # Pod is scheduled
  updatedReplicas: 1            # Pod created and up-to-date
  # NOTE: MinAvailableBreached is False (meaning minAvailable is met)
  # NOTE: PodCliqueScheduled is True (scheduled successfully)
```

### Events

| Type   | Reason              | Age | From                 | Message                                            |
|--------|---------------------|-----|----------------------|----------------------------------------------------|
| Normal | PodCreateSuccessful | 28s | podclique-controller | Created Pod: container-error-exit-0-database-flzv4 |

## 4. PodClique Level - queue (RUNNING)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-exit-0-queue
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-exit-0-queue
    app.kubernetes.io/part-of: container-error-exit
    grove.io/pod-template-hash: 8bfb6df796b548bdb7d
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-exit-0
  generation: 1
  creationTimestamp: "2025-11-12T21:09:23Z"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-error-exit
    uid: 84599c40-186c-4a39-97cb-68615991ead7

spec:
  minAvailable: 1
  replicas: 1
  roleName: queue

status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:09:23Z"
    message: 'Sufficient scheduled pods found. expected at least: 1, found: 1'
    reason: SufficientScheduledPods
    status: "True"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:09:23Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 1, found: 1'
    reason: SufficientReadyPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5b77b8888676d858c9f
  currentPodTemplateHash: 8bfb6df796b548bdb7d
  observedGeneration: 1
  readyReplicas: 1              # Pod is ready
  replicas: 1                   # Total replicas expected
  scheduleGatedReplicas: 0      # Pod is not schedule-gated
  scheduledReplicas: 1          # Pod is scheduled
  updatedReplicas: 1            # Pod created and up-to-date
  # NOTE: MinAvailableBreached is False (meaning minAvailable is met)
  # NOTE: PodCliqueScheduled is True (scheduled successfully)
```

### Events

| Type   | Reason              | Age | From                 | Message                                         |
|--------|---------------------|-----|----------------------|-------------------------------------------------|
| Normal | PodCreateSuccessful | 29s | podclique-controller | Created Pod: container-error-exit-0-queue-zf4vl |
