# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `03-invalid-args` test scenario.

## Scenario Description
Container starts with invalid arguments, causing CrashLoopBackOff. This tests error propagation when containers receive invalid command-line arguments.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-error-invalid-args
  namespace: default
  labels:
    app: container-error-invalid-args
    demo: group2-container
    scenario: 03-invalid-args
spec:
  replicas: 1
  template:
    cliqueStartupType: CliqueStartupTypeAnyOrder
    cliques:
    - name: app
      spec:
        minAvailable: 2
        podSpec:
          containers:
          - args:
            - --invalid-flag
            - --another-bad-flag
            image: nginx:latest
            name: app
            resources:
              requests:
                cpu: 10m
                memory: 32Mi
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
        replicas: 2
        roleName: app
    - name: proxy
      spec:
        minAvailable: 1
        podSpec:
          containers:
          - image: nginx:latest
            name: proxy
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
        replicas: 1
        roleName: proxy
    headlessServiceConfig:
      publishNotReadyAddresses: true
    terminationDelay: 4h0m0s
status:
  availableReplicas: 0                     # No replicas available due to app clique failures
  currentGenerationHash: 5c988c4d9fdb45946c8d
  observedGeneration: 1
  replicas: 1
  updatedReplicas: 0                      # No replicas fully updated/ready
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodGangCreateOrUpdateSuccessful | 18s (x7 over 23s) | podcliqueset-controller | Created/Updated PodGang default/container-error-invalid-args-0 |
| Normal | PodCliqueCreateOrUpdateSuccessful | 13s (x9 over 23s) | podcliqueset-controller | PodClique default/container-error-invalid-args-0-app created or updated successfully |
| Normal | PodCliqueCreateOrUpdateSuccessful | 13s (x9 over 23s) | podcliqueset-controller | PodClique default/container-error-invalid-args-0-proxy created or updated successfully |

## 2. PodClique Level - app (Failing)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-invalid-args-0-app
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-invalid-args-0-app
    app.kubernetes.io/part-of: container-error-invalid-args
    grove.io/pod-template-hash: b4f66ddd6979cf57454
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-invalid-args-0
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-error-invalid-args
    uid: e3d8b097-184b-4c92-84db-69273d0c8368
spec:
  minAvailable: 2
  podSpec:
    containers:
    - args:
      - --invalid-flag
      - --another-bad-flag
      image: nginx:latest
      name: app
      resources:
        requests:
          cpu: 10m
          memory: 32Mi
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
  replicas: 2
  roleName: app
status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:00:50Z"
    message: 'Sufficient scheduled pods found. expected at least: 2, found: 2'
    reason: SufficientScheduledPods
    status: "True"                        # Pods are scheduled
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:00:52Z"
    message: 'Insufficient ready or starting pods. expected at least: 2, found: 0'
    reason: InsufficientReadyPods
    status: "True"                        # MinAvailableBreached - pods not ready
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5c988c4d9fdb45946c8d
  currentPodTemplateHash: b4f66ddd6979cf57454
  observedGeneration: 1
  readyReplicas: 0                        # No pods are ready
  replicas: 2
  scheduleGatedReplicas: 0
  scheduledReplicas: 2
  updatedReplicas: 2
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCreateSuccessful | 25s | podclique-controller | Created Pod: container-error-invalid-args-0-app-vq952 |
| Normal | PodCreateSuccessful | 25s | podclique-controller | Created Pod: container-error-invalid-args-0-app-pqrh9 |

## 3. PodClique Level - proxy (Healthy)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-error-invalid-args-0-proxy
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-error-invalid-args-0-proxy
    app.kubernetes.io/part-of: container-error-invalid-args
    grove.io/pod-template-hash: 5cf6457df769cd697989
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-error-invalid-args-0
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-error-invalid-args
    uid: e3d8b097-184b-4c92-84db-69273d0c8368
spec:
  minAvailable: 1
  podSpec:
    containers:
    - image: nginx:latest
      name: proxy
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
  replicas: 1
  roleName: proxy
status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:00:50Z"
    message: 'Sufficient scheduled pods found. expected at least: 1, found: 1'
    reason: SufficientScheduledPods
    status: "True"                        # Pods are scheduled
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:00:50Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 1,
      found: 1'
    reason: SufficientReadyPods
    status: "False"                       # MinAvailable NOT breached - pods are ready
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 5c988c4d9fdb45946c8d
  currentPodTemplateHash: 5cf6457df769cd697989
  observedGeneration: 1
  readyReplicas: 1                        # One pod is ready
  replicas: 1
  scheduleGatedReplicas: 0
  scheduledReplicas: 1
  updatedReplicas: 1
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCreateSuccessful | 26s | podclique-controller | Created Pod: container-error-invalid-args-0-proxy-69wlj |
