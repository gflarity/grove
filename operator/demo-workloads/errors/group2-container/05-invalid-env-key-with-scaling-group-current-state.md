# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `05-invalid-env-key-with-scaling-group` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: env-err-sg
  namespace: default
  labels:
    app: env-err-sg
    demo: group2-container
    scenario: 05-invalid-env-key-scaling-group

status:
  availableReplicas: 0              # No replicas available
  currentGenerationHash: fd78d74c9bd45f4d5c6
  replicas: 1
  updatedReplicas: 0                # No replicas successfully updated
  # No conditions field present
  # No lastErrors field present - errors not propagated to PCS level
```

### Events
| Type   | Reason                                | Age                 | From                     | Message |
|--------|---------------------------------------|---------------------|--------------------------|---------|
| Normal | PodCliqueCreateOrUpdateSuccessful     | 3s (x10 over 33s)   | podcliqueset-controller  | PodClique default/env-err-sg-0-monitoring created or updated successfully |
| Normal | PodCliqueScalingGroupCreateSuccessful | 3s (x10 over 33s)   | podcliqueset-controller  | Created PodCliqueScalingGroup default/env-err-sg-0-main |

---

## 2. PodCliqueScalingGroup Level

### PodCliqueScalingGroup: env-err-sg-0-main

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: env-err-sg-0-main
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podcliquescalinggroup
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: env-err-sg-0-main
    app.kubernetes.io/part-of: env-err-sg
    grove.io/podcliqueset-replica-index: "0"

spec:
  cliqueNames:
    - api
    - web
    - worker
  minAvailable: 1
  replicas: 1

status:
  availableReplicas: 0              # No replicas available
  conditions:
    - lastTransitionTime: "2025-11-12T21:15:08Z"
      message: 'Insufficient scheduled replicas. expected at least: 1, found: 0'
      reason: InsufficientScheduledPodCliqueScalingGroupReplicas
      status: "False"
      type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: fd78d74c9bd45f4d5c6
  observedGeneration: 1
  replicas: 1
  scheduledReplicas: 0              # No replicas scheduled
  updatedReplicas: 0                # No replicas successfully updated
  # No lastErrors field present - errors not propagated to PCSG level
```

### Events
| Type   | Reason                    | Age | From                              | Message |
|--------|---------------------------|-----|-----------------------------------|---------|
| Normal | PodCliqueCreateSuccessful | 34s | podcliquescalinggroup-controller  | PodClique default/env-err-sg-0-main-0-api created successfully |
| Normal | PodCliqueCreateSuccessful | 34s | podcliquescalinggroup-controller  | PodClique default/env-err-sg-0-main-0-web created successfully |
| Normal | PodCliqueCreateSuccessful | 34s | podcliquescalinggroup-controller  | PodClique default/env-err-sg-0-main-0-worker created successfully |

---

## 3. PodClique Level

### PodClique: env-err-sg-0-main-0-api (FAILING - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: env-err-sg-0-main-0-api
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: env-err-sg-0-main-0-api
    app.kubernetes.io/part-of: env-err-sg
    grove.io/pod-template-hash: 5bd48485d788d55cd5c4
    grove.io/podcliquescalinggroup: env-err-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: env-err-sg-0

status:
  currentPodCliqueSetGenerationHash: fd78d74c9bd45f4d5c6
  currentPodTemplateHash: 5bd48485d788d55cd5c4
  lastErrors:
    - code: ERR_CREATE_POD
      description: 'failed to sync Pod: [Operation: Sync, Code: ERR_CREATE_POD] message: failed to create Pod:  for PodClique default/env-err-sg-0-main-0-api, cause: Pod "env-err-sg-0-main-0-api-42cdn" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than ''='''
      observedAt: "2025-11-12T21:15:29Z"
  readyReplicas: 0                  # No pods ready
  scheduleGatedReplicas: 0          # No pods schedule-gated
  scheduledReplicas: 0              # No pods scheduled
  updatedReplicas: 0                # No pods successfully created
  # No conditions field present
```

### Events
| Type    | Reason          | Age                 | From                  | Message |
|---------|-----------------|---------------------|-----------------------|---------|
| Warning | PodCreateFailed | 36s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-4dv2v" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 36s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-tjl56" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 36s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-bccpg" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 36s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-xc7x5" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 36s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-765xh" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 36s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-6bzzh" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 35s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-zrkkp" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 35s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-f8qf8" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 34s                 | podclique-controller  | Error creating pod : Pod "env-err-sg-0-main-0-api-r9q5r" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 15s (x4 over 33s)   | podclique-controller  | (combined from similar events): Error creating pod : Pod "env-err-sg-0-main-0-api-42cdn" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |

---

### PodClique: env-err-sg-0-main-0-web (SCHEDULE-GATED - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: env-err-sg-0-main-0-web
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: env-err-sg-0-main-0-web
    app.kubernetes.io/part-of: env-err-sg
    grove.io/pod-template-hash: 86584d8b46c9bcc58fb
    grove.io/podcliquescalinggroup: env-err-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: env-err-sg-0

status:
  conditions:
    - lastTransitionTime: "2025-11-12T21:15:08Z"
      message: 'Insufficient scheduled pods. expected at least: 2, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: PodCliqueScheduled
    - lastTransitionTime: "2025-11-12T21:15:08Z"
      message: 'Insufficient scheduled pods. expected at least: 2, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: fd78d74c9bd45f4d5c6
  currentPodTemplateHash: 86584d8b46c9bcc58fb
  observedGeneration: 1
  readyReplicas: 0                  # No pods ready
  replicas: 2                       # 2 pods exist
  scheduleGatedReplicas: 2          # Both pods are schedule-gated (waiting for gang)
  scheduledReplicas: 0              # No pods scheduled yet
  updatedReplicas: 2                # Both pods successfully created
  # No lastErrors field - this PodClique is healthy, just waiting
```

### Events
| Type   | Reason              | Age | From                  | Message |
|--------|---------------------|-----|-----------------------|---------|
| Normal | PodCreateSuccessful | 38s | podclique-controller  | Created Pod: env-err-sg-0-main-0-web-zkqdq |
| Normal | PodCreateSuccessful | 38s | podclique-controller  | Created Pod: env-err-sg-0-main-0-web-xfzg7 |

---

### PodClique: env-err-sg-0-main-0-worker (SCHEDULE-GATED - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: env-err-sg-0-main-0-worker
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: env-err-sg-0-main-0-worker
    app.kubernetes.io/part-of: env-err-sg
    grove.io/pod-template-hash: 5bf89fdcffd687d8f95f
    grove.io/podcliquescalinggroup: env-err-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: env-err-sg-0

status:
  conditions:
    - lastTransitionTime: "2025-11-12T21:15:08Z"
      message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: PodCliqueScheduled
    - lastTransitionTime: "2025-11-12T21:15:08Z"
      message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: fd78d74c9bd45f4d5c6
  currentPodTemplateHash: 5bf89fdcffd687d8f95f
  observedGeneration: 1
  readyReplicas: 0                  # No pods ready
  replicas: 1                       # 1 pod exists
  scheduleGatedReplicas: 1          # Pod is schedule-gated (waiting for gang)
  scheduledReplicas: 0              # No pods scheduled yet
  updatedReplicas: 1                # Pod successfully created
  # No lastErrors field - this PodClique is healthy, just waiting
```

### Events
| Type   | Reason              | Age | From                  | Message |
|--------|---------------------|-----|-----------------------|---------|
| Normal | PodCreateSuccessful | 39s | podclique-controller  | Created Pod: env-err-sg-0-main-0-worker-ffddg |

---

### PodClique: env-err-sg-0-monitoring (SCHEDULE-GATED - Standalone, Not in Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: env-err-sg-0-monitoring
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: env-err-sg-0-monitoring
    app.kubernetes.io/part-of: env-err-sg
    grove.io/pod-template-hash: 7f989664cfd798594f6
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: env-err-sg-0

status:
  conditions:
    - lastTransitionTime: "2025-11-12T21:15:08Z"
      message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: PodCliqueScheduled
    - lastTransitionTime: "2025-11-12T21:15:08Z"
      message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: fd78d74c9bd45f4d5c6
  currentPodTemplateHash: 7f989664cfd798594f6
  observedGeneration: 1
  readyReplicas: 0                  # No pods ready
  replicas: 1                       # 1 pod exists
  scheduleGatedReplicas: 1          # Pod is schedule-gated (waiting for gang)
  scheduledReplicas: 0              # No pods scheduled yet
  updatedReplicas: 1                # Pod successfully created
  # No lastErrors field - this PodClique is healthy, just waiting
```

### Events
| Type   | Reason              | Age | From                  | Message |
|--------|---------------------|-----|-----------------------|---------|
| Normal | PodCreateSuccessful | 42s | podclique-controller  | Created Pod: env-err-sg-0-monitoring-r7l9k |
