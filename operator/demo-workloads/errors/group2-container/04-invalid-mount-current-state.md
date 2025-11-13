# Grove Error Propagation - Current State
This document shows the actual current state of error reporting in Grove using the `04-invalid-mount` test scenario.

## Scenario Description
- **Scenario**: CreateContainerConfigError - Invalid Mount Path
- **Error Type**: Empty mount path causing Pod creation validation failure
- **Expected Behavior**: 
  - PodClique 'stateful' fails to create pods due to invalid mount configuration
  - PodClique 'stateless' is schedule-gated waiting for gang scheduling
  - Error should propagate from PodClique → PodCliqueSet

---

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-error-invalid-mount
  namespace: default
  labels:
    app: container-error-invalid-mount
    demo: group2-container
    scenario: 04-invalid-mount
  generation: 1
  resourceVersion: "455021"
  uid: 9ff51785-3207-4ee8-b581-9a1b34dc4305

spec:
  replicas: 1
  template:
    cliqueStartupType: CliqueStartupTypeAnyOrder
    cliques:
    - name: stateful
      spec:
        minAvailable: 2
        podSpec:
          containers:
          - image: nginx:latest
            name: stateful
            resources:
              requests:
                cpu: 10m
                memory: 32Mi
            volumeMounts:
            - mountPath: ""  # Empty mount path - causes validation error
              name: config
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
          volumes:
          - configMap:
              name: non-existent-config
            name: config
        replicas: 2
        roleName: stateful
    - name: stateless
      spec:
        minAvailable: 2
        podSpec:
          containers:
          - image: nginx:latest
            name: stateless
            ports:
            - containerPort: 80
              name: http
              protocol: TCP
            resources:
              requests:
                cpu: 10m
                memory: 32Mi
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
        replicas: 2
        roleName: stateless
    headlessServiceConfig:
      publishNotReadyAddresses: true
    terminationDelay: 4h0m0s

status:
  availableReplicas: 0      # No replicas available
  currentGenerationHash: 564d4b6465cf95b8d4fd
  replicas: 1               # 1 replica expected
  updatedReplicas: 0        # No replicas updated
  # NOTE: No conditions field
  # NOTE: No lastErrors field - error is NOT propagated to PCS level
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCliqueCreateOrUpdateSuccessful | 65s (x12 over 115s) | podcliqueset-controller | PodClique default/container-error-invalid-mount-0-stateless created or updated successfully |
| Normal | PodCliqueCreateOrUpdateSuccessful | 60s (x13 over 115s) | podcliqueset-controller | PodClique default/container-error-invalid-mount-0-stateful created or updated successfully |

**Observations:**
- Only success events are shown
- No error events indicating the 'stateful' PodClique is failing
- PCS status shows 0 availableReplicas but doesn't explain why
- No conditions or error information at this level

---

## 2. PodClique Level - stateful (Failing)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-invalid-mount-0-stateful
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-invalid-mount-0-stateful
    app.kubernetes.io/part-of: container-error-invalid-mount
    grove.io/pod-template-hash: 6dc9bcb6f8758cb6f7b
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-invalid-mount-0
  generation: 1
  resourceVersion: "455023"
  uid: eae5ed1a-983b-4c32-bddf-8670d3ec85f2
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-error-invalid-mount
    uid: 9ff51785-3207-4ee8-b581-9a1b34dc4305

spec:
  minAvailable: 2
  podSpec:
    containers:
    - image: nginx:latest
      name: stateful
      resources:
        requests:
          cpu: 10m
          memory: 32Mi
      volumeMounts:
      - mountPath: ""  # Empty mount path - causes validation error
        name: config
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
    volumes:
    - configMap:
        name: non-existent-config
      name: config
  replicas: 2
  roleName: stateful

status:
  currentPodCliqueSetGenerationHash: 564d4b6465cf95b8d4fd
  currentPodTemplateHash: 6dc9bcb6f8758cb6f7b
  # NOTE: No conditions field - error not reflected in conditions
  lastErrors:
  - code: ERR_CREATE_POD
    description: 'failed to sync Pod: [Operation: Sync, Code: ERR_CREATE_POD] message:
      failed to create Pod:  for PodClique default/container-error-invalid-mount-0-stateful,
      cause: Pod "container-error-invalid-mount-0-stateful-4ws6n" is invalid: spec.containers[0].volumeMounts[0].mountPath:
      Required value'
    observedAt: "2025-11-12T21:18:54Z"
  readyReplicas: 0           # No pods ready
  scheduleGatedReplicas: 0   # No pods waiting on scheduling gate
  scheduledReplicas: 0       # No pods scheduled
  updatedReplicas: 0         # No pods updated
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Warning | PodCreateFailed | 116s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-grf7s" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 116s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-wmz25" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 116s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-c2m48" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 116s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-825h4" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 116s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-9dntv" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 116s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-fz9dq" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 116s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-czpj5" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 116s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-qhdzb" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 115s | podclique-controller | Error creating pod : Pod "container-error-invalid-mount-0-stateful-j8s27" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |
| Warning | PodCreateFailed | 29s (x7 over 114s) | podclique-controller | (combined from similar events): Error creating pod : Pod "container-error-invalid-mount-0-stateful-4ws6n" is invalid: spec.containers[0].volumeMounts[0].mountPath: Required value |

**Observations:**
- lastErrors field contains the validation error details
- Multiple pod creation attempts all failed with the same validation error
- Error shows exact validation issue: "spec.containers[0].volumeMounts[0].mountPath: Required value"
- No conditions field to indicate health status
- status.readyReplicas = 0 but no condition explaining why

---

## 3. PodClique Level - stateless (Blocked by Gang Scheduling)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-invalid-mount-0-stateless
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-invalid-mount-0-stateless
    app.kubernetes.io/part-of: container-error-invalid-mount
    grove.io/pod-template-hash: 846457975f9b4575785
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-invalid-mount-0
  generation: 1
  resourceVersion: "454335"
  uid: bd35063b-57de-4ace-b521-015e9329effe
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-error-invalid-mount
    uid: 9ff51785-3207-4ee8-b581-9a1b34dc4305

spec:
  minAvailable: 2
  podSpec:
    containers:
    - image: nginx:latest
      name: stateless
      ports:
      - containerPort: 80
        name: http
        protocol: TCP
      resources:
        requests:
          cpu: 10m
          memory: 32Mi
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
  replicas: 2
  roleName: stateless

status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:17:27Z"
    message: 'Insufficient scheduled pods. expected at least: 2, found: 0'
    reason: InsufficientScheduledPods
    status: "False"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:17:27Z"
    message: 'Insufficient scheduled pods. expected at least: 2, found: 0'
    reason: InsufficientScheduledPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 564d4b6465cf95b8d4fd
  currentPodTemplateHash: 846457975f9b4575785
  observedGeneration: 1
  readyReplicas: 0           # No pods ready
  replicas: 2                # 2 pods created
  scheduleGatedReplicas: 2   # 2 pods waiting on scheduling gate (gang scheduling)
  scheduledReplicas: 0       # No pods scheduled
  updatedReplicas: 2         # 2 pods up-to-date
  # NOTE: No lastErrors field
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCreateSuccessful | 117s | podclique-controller | Created Pod: container-error-invalid-mount-0-stateless-8gdqv |
| Normal | PodCreateSuccessful | 117s | podclique-controller | Created Pod: container-error-invalid-mount-0-stateless-zsxht |

**Observations:**
- Pods created successfully but are schedule-gated (waiting for gang scheduling)
- Conditions show "InsufficientScheduledPods" but don't explain root cause
- No indication that the 'stateful' PodClique failure is blocking this PodClique
- scheduleGatedReplicas = 2 indicates gang scheduling is blocking
- No lastErrors field since this PodClique itself has no errors

---

## Summary of Error Propagation Issues

1. **PodCliqueSet Level**:
   - ❌ No error information despite child PodClique failure
   - ❌ No conditions indicating degraded state
   - ❌ Status shows 0 availableReplicas but doesn't explain why
   - ❌ Events only show successful operations, no warnings/errors

2. **PodClique Level (stateful - failing)**:
   - ✅ lastErrors field contains detailed error information
   - ✅ Events show multiple pod creation failures with validation errors
   - ❌ No conditions field to indicate health status
   - ❌ Error not propagated to parent PodCliqueSet

3. **PodClique Level (stateless - blocked)**:
   - ✅ Conditions show scheduling blocked
   - ❌ Conditions don't explain why (gang scheduling blocked by 'stateful' failure)
   - ❌ No cross-reference to the failing 'stateful' PodClique

4. **Overall Error Visibility**:
   - Errors are visible at the PodClique level (lastErrors + events)
   - Errors do NOT propagate to PodCliqueSet level
   - Users must drill down to each PodClique to find errors
   - No top-level view of which components are failing and why
