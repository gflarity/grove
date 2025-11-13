# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `05-invalid-env-key` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-error-invalid-env
  namespace: default
  labels:
    app: container-error-invalid-env
    demo: group2-container
    scenario: 05-invalid-env-key

status:
  availableReplicas: 0              # No replicas available
  currentGenerationHash: 59d79975f95c6cfbbf4f
  replicas: 1
  updatedReplicas: 0                # No replicas successfully updated
  # No conditions field present
  # No lastErrors field present - errors not propagated to PCS level
```

### Events
| Type   | Reason                             | Age                | From                     | Message |
|--------|------------------------------------|--------------------|--------------------------|---------|
| Normal | PodCliqueCreateOrUpdateSuccessful  | 5s (x7 over 25s)   | podcliqueset-controller  | PodClique default/container-error-invalid-env-0-api created or updated successfully |
| Normal | PodCliqueCreateOrUpdateSuccessful  | 5s (x7 over 25s)   | podcliqueset-controller  | PodClique default/container-error-invalid-env-0-worker created or updated successfully |
| Normal | PodCliqueCreateOrUpdateSuccessful  | 5s (x7 over 25s)   | podcliqueset-controller  | PodClique default/container-error-invalid-env-0-web created or updated successfully |

---

## 2. PodClique Level

### PodClique: container-error-invalid-env-0-api (FAILING)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-invalid-env-0-api
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-invalid-env-0-api
    app.kubernetes.io/part-of: container-error-invalid-env
    grove.io/pod-template-hash: 5bd48485d788d55cd5c4
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-invalid-env-0

status:
  currentPodCliqueSetGenerationHash: 59d79975f95c6cfbbf4f
  currentPodTemplateHash: 5bd48485d788d55cd5c4
  lastErrors:
    - code: ERR_CREATE_POD
      description: 'failed to sync Pod: [Operation: Sync, Code: ERR_CREATE_POD] message: failed to create Pod:  for PodClique default/container-error-invalid-env-0-api, cause: Pod "container-error-invalid-env-0-api-7p8f2" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than ''='''
      observedAt: "2025-11-12T21:14:16Z"
  readyReplicas: 0                  # No pods ready
  scheduleGatedReplicas: 0          # No pods schedule-gated
  scheduledReplicas: 0              # No pods scheduled
  updatedReplicas: 0                # No pods successfully created
  # No conditions field present
```

### Events
| Type    | Reason          | Age                 | From                  | Message |
|---------|-----------------|---------------------|-----------------------|---------|
| Warning | PodCreateFailed | 26s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-kh9fn" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 26s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-f8xtv" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 26s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-48nns" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 26s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-4jpwh" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 26s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-tpzsj" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 26s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-qxpnp" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 25s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-5wwxf" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 25s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-kcpxj" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 25s                 | podclique-controller  | Error creating pod : Pod "container-error-invalid-env-0-api-f5tfc" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |
| Warning | PodCreateFailed | 5s (x4 over 23s)    | podclique-controller  | (combined from similar events): Error creating pod : Pod "container-error-invalid-env-0-api-7p8f2" is invalid: spec.containers[0].env[0].name: Invalid value: "KEY=1": a valid environment variable name must consist only of printable ASCII characters other than '=' |

---

### PodClique: container-error-invalid-env-0-web (SCHEDULE-GATED)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-invalid-env-0-web
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-invalid-env-0-web
    app.kubernetes.io/part-of: container-error-invalid-env
    grove.io/pod-template-hash: 86584d8b46c9bcc58fb
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-invalid-env-0

status:
  conditions:
    - lastTransitionTime: "2025-11-12T21:13:55Z"
      message: 'Insufficient scheduled pods. expected at least: 2, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: PodCliqueScheduled
    - lastTransitionTime: "2025-11-12T21:13:55Z"
      message: 'Insufficient scheduled pods. expected at least: 2, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 59d79975f95c6cfbbf4f
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
| Normal | PodCreateSuccessful | 28s | podclique-controller  | Created Pod: container-error-invalid-env-0-web-l6bdd |
| Normal | PodCreateSuccessful | 28s | podclique-controller  | Created Pod: container-error-invalid-env-0-web-n5ldz |

---

### PodClique: container-error-invalid-env-0-worker (SCHEDULE-GATED)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-invalid-env-0-worker
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-invalid-env-0-worker
    app.kubernetes.io/part-of: container-error-invalid-env
    grove.io/pod-template-hash: 5bf89fdcffd687d8f95f
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-invalid-env-0

status:
  conditions:
    - lastTransitionTime: "2025-11-12T21:13:55Z"
      message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: PodCliqueScheduled
    - lastTransitionTime: "2025-11-12T21:13:55Z"
      message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
      reason: InsufficientScheduledPods
      status: "False"
      type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 59d79975f95c6cfbbf4f
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
| Normal | PodCreateSuccessful | 30s | podclique-controller  | Created Pod: container-error-invalid-env-0-worker-qmcs9 |
