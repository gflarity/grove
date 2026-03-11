# Grove Error Propagation - Current State
This document shows the actual current state of error reporting in Grove using the `04-invalid-mount-with-scaling-group` test scenario.

## Scenario Description
- **Scenario**: CreateContainerConfigError - Invalid Volume Mount with Scaling Group
- **Error Type**: Volume mount references non-existent volume
- **Expected Behavior**: 
  - PodClique 'storage' fails to create pods (part of scaling group)
  - PodCliques 'app' and 'processor' are schedule-gated (part of scaling group, blocked by 'storage' failure)
  - PodClique 'metrics' should run successfully (standalone, not part of gang) but is currently schedule-gated
  - PodCliqueScalingGroup shows unhealthy status due to 'storage' failure
  - Error should propagate: PodClique → PodCliqueScalingGroup → PodCliqueSet

---

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-invalid-mount-sg
  namespace: default
  labels:
    app: container-invalid-mount-sg
    demo: group2-container
    scenario: 04-invalid-mount-scaling-group
  generation: 1
  resourceVersion: "455779"
  uid: 63ff2260-a435-4905-aac6-6b8b4129e3b8

spec:
  replicas: 1
  template:
    cliqueStartupType: CliqueStartupTypeAnyOrder
    podCliqueScalingGroups:
    - cliqueNames:
      - storage
      - app
      - processor
      minAvailable: 1
      name: main
      replicas: 1
    cliques:
    - name: storage
      spec:
        minAvailable: 2
        podSpec:
          containers:
          - command:
            - minio
            - server
            - /data
            env:
            - name: MINIO_ROOT_USER
              value: minioadmin
            - name: MINIO_ROOT_PASSWORD
              value: minioadmin
            image: minio/minio:latest
            name: storage
            ports:
            - containerPort: 9000
              name: api
              protocol: TCP
            - containerPort: 9001
              name: console
              protocol: TCP
            resources:
              requests:
                cpu: 20m
                memory: 128Mi
            volumeMounts:
            - mountPath: /data
              name: data-volume-does-not-exist  # References non-existent volume
            - mountPath: /config
              name: config-volume
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
          volumes:
          - emptyDir: {}
            name: config-volume  # Only config-volume is defined
        replicas: 2
        roleName: storage
    - name: app
      spec:
        minAvailable: 2
        podSpec:
          containers:
          - command:
            - /bin/sh
            - -c
            - |
              echo "Application server starting..."
              echo "Connecting to storage service..."
              while true; do
                echo "$(date): Processing requests"
                sleep 10
              done
            env:
            - name: STORAGE_ENDPOINT
              value: http://storage:9000
            - name: NODE_ENV
              value: production
            image: node:18-alpine
            name: app
            ports:
            - containerPort: 3000
              name: http
              protocol: TCP
            resources:
              requests:
                cpu: 15m
                memory: 64Mi
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
        replicas: 3
        roleName: app
    - name: processor
      spec:
        minAvailable: 1
        podSpec:
          containers:
          - command:
            - /bin/sh
            - -c
            - |
              echo "Data processor starting..."
              pip install boto3
              while true; do
                echo "$(date): Processing data from storage..."
                sleep 20
              done
            env:
            - name: S3_ENDPOINT
              value: http://storage:9000
            - name: AWS_ACCESS_KEY_ID
              value: minioadmin
            - name: AWS_SECRET_ACCESS_KEY
              value: minioadmin
            image: python:3.9-alpine
            name: processor
            resources:
              requests:
                cpu: 25m
                memory: 128Mi
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
        replicas: 2
        roleName: processor
    - name: metrics
      spec:
        minAvailable: 1
        podSpec:
          containers:
          - env:
            - name: GF_SECURITY_ADMIN_PASSWORD
              value: admin
            - name: GF_USERS_ALLOW_SIGN_UP
              value: "false"
            image: grafana/grafana:latest
            name: metrics
            ports:
            - containerPort: 3000
              name: http
              protocol: TCP
            resources:
              requests:
                cpu: 10m
                memory: 64Mi
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
        replicas: 1
        roleName: metrics
    headlessServiceConfig:
      publishNotReadyAddresses: true
    terminationDelay: 4h0m0s

status:
  availableReplicas: 0      # No replicas available
  currentGenerationHash: 5587bdb766f8ffd8d458
  replicas: 1               # 1 replica expected
  updatedReplicas: 0        # No replicas updated
  # NOTE: No conditions field
  # NOTE: No lastErrors field - errors are NOT propagated from PCSG or PodCliques
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCliqueCreateOrUpdateSuccessful | 3s (x9 over 28s) | podcliqueset-controller | PodClique default/container-invalid-mount-sg-0-metrics created or updated successfully |
| Normal | PodCliqueScalingGroupCreateSuccessful | 3s (x9 over 28s) | podcliqueset-controller | Created PodCliqueScalingGroup default/container-invalid-mount-sg-0-main |

**Observations:**
- Only success events are shown
- No error events indicating the 'storage' PodClique failure
- No warning about PodCliqueScalingGroup being unhealthy
- PCS status shows 0 availableReplicas but doesn't explain why
- No conditions or error information at this level

---

## 2. PodCliqueScalingGroup Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: container-invalid-mount-sg-0-main
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podcliquescalinggroup
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-mount-sg-0-main
    app.kubernetes.io/part-of: container-invalid-mount-sg
    grove.io/podcliqueset-replica-index: "0"
  generation: 1
  resourceVersion: "455787"
  uid: 033eefa8-b6b9-4081-b90e-02748ce949b4
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-invalid-mount-sg
    uid: 63ff2260-a435-4905-aac6-6b8b4129e3b8

spec:
  cliqueNames:
  - storage
  - app
  - processor
  minAvailable: 1
  replicas: 1

status:
  availableReplicas: 0      # No replicas available
  conditions:
  - lastTransitionTime: "2025-11-12T21:20:29Z"
    message: 'Insufficient scheduled replicas. expected at least: 1, found: 0'
    reason: InsufficientScheduledPodCliqueScalingGroupReplicas
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5587bdb766f8ffd8d458
  observedGeneration: 1
  replicas: 1               # 1 replica expected
  scheduledReplicas: 0      # No replicas scheduled
  updatedReplicas: 0        # No replicas updated
  # NOTE: No lastErrors field - error from 'storage' PodClique NOT propagated here
  # NOTE: Condition shows "InsufficientScheduledReplicas" but doesn't explain WHY
  # NOTE: No indication that 'storage' PodClique is failing
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCliqueCreateSuccessful | 29s | podcliquescalinggroup-controller | PodClique default/container-invalid-mount-sg-0-main-0-app created successfully |
| Normal | PodCliqueCreateSuccessful | 29s | podcliquescalinggroup-controller | PodClique default/container-invalid-mount-sg-0-main-0-storage created successfully |
| Normal | PodCliqueCreateSuccessful | 29s | podcliquescalinggroup-controller | PodClique default/container-invalid-mount-sg-0-main-0-processor created successfully |

**Observations:**
- Only success events shown - PodCliques were created
- No error events indicating 'storage' PodClique is failing
- Condition shows "InsufficientScheduledReplicas" but doesn't explain root cause
- No cross-reference to which PodClique(s) are causing the problem
- No lastErrors field despite child PodClique having errors

---

## 3. PodClique Level - storage (Failing - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-invalid-mount-sg-0-main-0-storage
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-mount-sg-0-main-0-storage
    app.kubernetes.io/part-of: container-invalid-mount-sg
    grove.io/pod-template-hash: bf54d9db4f769fdf746
    grove.io/podcliquescalinggroup: container-invalid-mount-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-invalid-mount-sg-0
  generation: 1
  resourceVersion: "455990"
  uid: 113ed1e9-c0d5-42f7-baba-7ab9c1f5e8a3
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-invalid-mount-sg-0-main
    uid: 033eefa8-b6b9-4081-b90e-02748ce949b4

spec:
  minAvailable: 2
  podSpec:
    containers:
    - command:
      - minio
      - server
      - /data
      env:
      - name: MINIO_ROOT_USER
        value: minioadmin
      - name: MINIO_ROOT_PASSWORD
        value: minioadmin
      - name: GROVE_PCSG_NAME
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup']
      - name: GROVE_PCSG_TEMPLATE_NUM_PODS
        value: "7"
      - name: GROVE_PCSG_INDEX
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup-replica-index']
      image: minio/minio:latest
      name: storage
      ports:
      - containerPort: 9000
        name: api
        protocol: TCP
      - containerPort: 9001
        name: console
        protocol: TCP
      resources:
        requests:
          cpu: 20m
          memory: 128Mi
      volumeMounts:
      - mountPath: /data
        name: data-volume-does-not-exist  # References non-existent volume
      - mountPath: /config
        name: config-volume
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
    volumes:
    - emptyDir: {}
      name: config-volume  # Only config-volume is defined
  replicas: 2
  roleName: storage

status:
  currentPodCliqueSetGenerationHash: 5587bdb766f8ffd8d458
  currentPodTemplateHash: bf54d9db4f769fdf746
  # NOTE: No conditions field - error not reflected in conditions
  lastErrors:
  - code: ERR_CREATE_POD
    description: 'failed to sync Pod: [Operation: Sync, Code: ERR_CREATE_POD] message:
      failed to create Pod:  for PodClique default/container-invalid-mount-sg-0-main-0-storage,
      cause: Pod "container-invalid-mount-sg-0-main-0-storage-qspvn" is invalid: spec.containers[0].volumeMounts[0].name:
      Not found: "data-volume-does-not-exist"'
    observedAt: "2025-11-12T21:20:50Z"
  readyReplicas: 0           # No pods ready
  scheduleGatedReplicas: 0   # No pods waiting on scheduling gate
  scheduledReplicas: 0       # No pods scheduled
  updatedReplicas: 0         # No pods updated
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Warning | PodCreateFailed | 31s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-hncbc" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 31s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-f2w5n" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 30s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-q7clw" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 30s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-5xtgm" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 30s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-xqf9w" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 30s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-8l85h" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 30s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-dnbw7" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 30s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-7kxmw" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 29s | podclique-controller | Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-bqkkf" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |
| Warning | PodCreateFailed | 10s (x4 over 28s) | podclique-controller | (combined from similar events): Error creating pod : Pod "container-invalid-mount-sg-0-main-0-storage-qspvn" is invalid: spec.containers[0].volumeMounts[0].name: Not found: "data-volume-does-not-exist" |

**Observations:**
- lastErrors field contains the validation error details
- Multiple pod creation attempts all failed with volume mount validation error
- Error clearly shows: "spec.containers[0].volumeMounts[0].name: Not found: 'data-volume-does-not-exist'"
- No conditions field to indicate health status
- Error is NOT propagated to parent PodCliqueScalingGroup

---

## 4. PodClique Level - app (Blocked by Gang Scheduling - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-invalid-mount-sg-0-main-0-app
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-mount-sg-0-main-0-app
    app.kubernetes.io/part-of: container-invalid-mount-sg
    grove.io/pod-template-hash: 64f7bc69b8764766bfd
    grove.io/podcliquescalinggroup: container-invalid-mount-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-invalid-mount-sg-0
  generation: 1
  resourceVersion: "455803"
  uid: 937b5374-41fb-4250-a56e-4649b1d44176
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-invalid-mount-sg-0-main
    uid: 033eefa8-b6b9-4081-b90e-02748ce949b4

spec:
  minAvailable: 2
  podSpec:
    containers:
    - command:
      - /bin/sh
      - -c
      - |
        echo "Application server starting..."
        echo "Connecting to storage service..."
        while true; do
          echo "$(date): Processing requests"
          sleep 10
        done
      env:
      - name: STORAGE_ENDPOINT
        value: http://storage:9000
      - name: NODE_ENV
        value: production
      - name: GROVE_PCSG_NAME
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup']
      - name: GROVE_PCSG_TEMPLATE_NUM_PODS
        value: "7"
      - name: GROVE_PCSG_INDEX
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup-replica-index']
      image: node:18-alpine
      name: app
      ports:
      - containerPort: 3000
        name: http
        protocol: TCP
      resources:
        requests:
          cpu: 15m
          memory: 64Mi
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
  replicas: 3
  roleName: app

status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:20:29Z"
    message: 'Insufficient scheduled pods. expected at least: 2, found: 0'
    reason: InsufficientScheduledPods
    status: "False"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:20:29Z"
    message: 'Insufficient scheduled pods. expected at least: 2, found: 0'
    reason: InsufficientScheduledPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5587bdb766f8ffd8d458
  currentPodTemplateHash: 64f7bc69b8764766bfd
  observedGeneration: 1
  readyReplicas: 0           # No pods ready
  replicas: 3                # 3 pods created
  scheduleGatedReplicas: 3   # 3 pods waiting on scheduling gate (gang scheduling)
  scheduledReplicas: 0       # No pods scheduled
  updatedReplicas: 3         # 3 pods up-to-date
  # NOTE: No lastErrors field - this PodClique itself has no errors
  # NOTE: Conditions don't explain WHY scheduling is blocked
  # NOTE: No cross-reference to 'storage' PodClique failure
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCreateSuccessful | 37s | podclique-controller | Created Pod: container-invalid-mount-sg-0-main-0-app-94vv8 |
| Normal | PodCreateSuccessful | 37s | podclique-controller | Created Pod: container-invalid-mount-sg-0-main-0-app-qxqxs |
| Normal | PodCreateSuccessful | 37s | podclique-controller | Created Pod: container-invalid-mount-sg-0-main-0-app-rwtmf |

**Observations:**
- Pods created successfully but are schedule-gated (waiting for gang scheduling)
- Conditions show "InsufficientScheduledPods" but don't explain root cause
- No indication that the 'storage' PodClique failure is blocking this PodClique
- scheduleGatedReplicas = 3 indicates gang scheduling is blocking
- No lastErrors field since this PodClique itself has no errors

---

## 5. PodClique Level - processor (Blocked by Gang Scheduling - Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-invalid-mount-sg-0-main-0-processor
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-mount-sg-0-main-0-processor
    app.kubernetes.io/part-of: container-invalid-mount-sg
    grove.io/pod-template-hash: 5488d945f54c88b4b86c
    grove.io/podcliquescalinggroup: container-invalid-mount-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-invalid-mount-sg-0
  generation: 1
  resourceVersion: "455804"
  uid: 61721eda-9215-4e68-a06a-e5e72ccfca94
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-invalid-mount-sg-0-main
    uid: 033eefa8-b6b9-4081-b90e-02748ce949b4

spec:
  minAvailable: 1
  podSpec:
    containers:
    - command:
      - /bin/sh
      - -c
      - |
        echo "Data processor starting..."
        pip install boto3
        while true; do
          echo "$(date): Processing data from storage..."
          sleep 20
        done
      env:
      - name: S3_ENDPOINT
        value: http://storage:9000
      - name: AWS_ACCESS_KEY_ID
        value: minioadmin
      - name: AWS_SECRET_ACCESS_KEY
        value: minioadmin
      - name: GROVE_PCSG_NAME
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup']
      - name: GROVE_PCSG_TEMPLATE_NUM_PODS
        value: "7"
      - name: GROVE_PCSG_INDEX
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup-replica-index']
      image: python:3.9-alpine
      name: processor
      resources:
        requests:
          cpu: 25m
          memory: 128Mi
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
  replicas: 2
  roleName: processor

status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:20:29Z"
    message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
    reason: InsufficientScheduledPods
    status: "False"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:20:29Z"
    message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
    reason: InsufficientScheduledPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5587bdb766f8ffd8d458
  currentPodTemplateHash: 5488d945f54c88b4b86c
  observedGeneration: 1
  readyReplicas: 0           # No pods ready
  replicas: 2                # 2 pods created
  scheduleGatedReplicas: 2   # 2 pods waiting on scheduling gate (gang scheduling)
  scheduledReplicas: 0       # No pods scheduled
  updatedReplicas: 2         # 2 pods up-to-date
  # NOTE: No lastErrors field - this PodClique itself has no errors
  # NOTE: Conditions don't explain WHY scheduling is blocked
  # NOTE: No cross-reference to 'storage' PodClique failure
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCreateSuccessful | 39s | podclique-controller | Created Pod: container-invalid-mount-sg-0-main-0-processor-mt5pj |
| Normal | PodCreateSuccessful | 39s | podclique-controller | Created Pod: container-invalid-mount-sg-0-main-0-processor-25g6c |

**Observations:**
- Pods created successfully but are schedule-gated (waiting for gang scheduling)
- Conditions show "InsufficientScheduledPods" but don't explain root cause
- No indication that the 'storage' PodClique failure is blocking this PodClique
- scheduleGatedReplicas = 2 indicates gang scheduling is blocking
- No lastErrors field since this PodClique itself has no errors

---

## 6. PodClique Level - metrics (Blocked by Gang Scheduling - Standalone, Not Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-invalid-mount-sg-0-metrics
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-mount-sg-0-metrics
    app.kubernetes.io/part-of: container-invalid-mount-sg
    grove.io/pod-template-hash: dddf48f48d8bb8979cd
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-invalid-mount-sg-0  # Part of same podgang
  generation: 1
  resourceVersion: "455789"
  uid: 4d537617-79fd-4e0c-b82c-be92dc57eadc
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-invalid-mount-sg
    uid: 63ff2260-a435-4905-aac6-6b8b4129e3b8

spec:
  minAvailable: 1
  podSpec:
    containers:
    - env:
      - name: GF_SECURITY_ADMIN_PASSWORD
        value: admin
      - name: GF_USERS_ALLOW_SIGN_UP
        value: "false"
      image: grafana/grafana:latest
      name: metrics
      ports:
      - containerPort: 3000
        name: http
        protocol: TCP
      resources:
        requests:
          cpu: 10m
          memory: 64Mi
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
  replicas: 1
  roleName: metrics

status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:20:29Z"
    message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
    reason: InsufficientScheduledPods
    status: "False"
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:20:29Z"
    message: 'Insufficient scheduled pods. expected at least: 1, found: 0'
    reason: InsufficientScheduledPods
    status: "False"
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5587bdb766f8ffd8d458
  currentPodTemplateHash: dddf48f48d8bb8979cd
  observedGeneration: 1
  readyReplicas: 0           # No pods ready
  replicas: 1                # 1 pod created
  scheduleGatedReplicas: 1   # 1 pod waiting on scheduling gate (gang scheduling)
  scheduledReplicas: 0       # No pods scheduled
  updatedReplicas: 1         # 1 pod up-to-date
  # NOTE: No lastErrors field - this PodClique itself has no errors
  # NOTE: NOT part of podCliquescalinggroup label
  # NOTE: But IS part of same podgang (container-invalid-mount-sg-0)
  # NOTE: Should be running independently but is blocked by gang scheduling
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCreateSuccessful | 40s | podclique-controller | Created Pod: container-invalid-mount-sg-0-metrics-shpb7 |

**Observations:**
- Pod created successfully but is schedule-gated (waiting for gang scheduling)
- NOT part of PodCliqueScalingGroup (no grove.io/podcliquescalinggroup label)
- BUT is part of the same podgang (grove.io/podgang: container-invalid-mount-sg-0)
- **UNEXPECTED**: This PodClique should be running independently since it's not part of the scaling group
- **BUG**: Gang scheduling is incorrectly blocking this standalone PodClique
- Conditions show "InsufficientScheduledPods" but don't explain root cause

---

## Summary of Error Propagation Issues

### Error Propagation Chain
1. **PodClique (storage) → PodCliqueScalingGroup**:
   - ❌ Storage PodClique has errors in lastErrors field
   - ❌ Error NOT propagated to parent PodCliqueScalingGroup
   - ❌ PCSG only shows generic "InsufficientScheduledReplicas" condition
   - ❌ No indication which member PodClique is failing

2. **PodCliqueScalingGroup → PodCliqueSet**:
   - ❌ PCSG has "MinAvailableBreached" condition
   - ❌ Condition/error NOT propagated to parent PodCliqueSet
   - ❌ PCS shows 0 availableReplicas but no explanation
   - ❌ PCS events only show success messages

3. **Overall Error Visibility**:
   - Errors visible only at PodClique level (lastErrors + events)
   - Errors do NOT propagate upward through the hierarchy
   - Users must drill down to each PodClique to find errors
   - No top-level view of which components are failing

### Specific Issues by Resource Level

1. **PodCliqueSet Level**:
   - ❌ No error information despite multiple failures
   - ❌ No conditions indicating degraded state
   - ❌ Status shows 0 availableReplicas without explanation
   - ❌ Events only show successful operations

2. **PodCliqueScalingGroup Level**:
   - ✅ Has conditions field (MinAvailableBreached)
   - ❌ Conditions are generic, don't explain root cause
   - ❌ No lastErrors field despite child PodClique errors
   - ❌ No indication which member PodCliques are failing
   - ❌ Events only show successful PodClique creation

3. **PodClique Level (storage - failing)**:
   - ✅ lastErrors field contains detailed error information
   - ✅ Events show multiple pod creation failures
   - ❌ No conditions field to indicate health status
   - ❌ Error not propagated to parent PCSG

4. **PodClique Level (app, processor - blocked)**:
   - ✅ Conditions show scheduling blocked
   - ❌ Conditions don't explain why (blocked by storage failure)
   - ❌ No cross-reference to the failing 'storage' PodClique
   - ❌ No indication this is gang scheduling related

5. **PodClique Level (metrics - incorrectly blocked)**:
   - ✅ Conditions show scheduling blocked
   - ❌ **BUG**: Should be running independently (not part of scaling group)
   - ❌ Incorrectly blocked by gang scheduling
   - ❌ No indication of why it's blocked

### Critical Gap
The most critical gap is the **lack of error propagation from child resources to parent resources**. Users looking at the PodCliqueSet or PodCliqueScalingGroup cannot determine what is wrong without manually inspecting each PodClique.
