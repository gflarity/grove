# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `03-invalid-args-with-scaling-group` test scenario.

## Scenario Description
Container with invalid arguments within a PodCliqueScalingGroup. The api PodClique has invalid nginx arguments causing CrashLoopBackOff, while web and db are part of the same scaling group and backup is standalone. This tests error propagation through the scaling group hierarchy.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: container-invalid-args-sg
  namespace: default
  labels:
    app: container-invalid-args-sg
    demo: group2-container
    scenario: 03-invalid-args-scaling-group
spec:
  replicas: 1
  template:
    cliqueStartupType: CliqueStartupTypeAnyOrder
    cliques:
    - name: api
      spec:
        minAvailable: 2
        podSpec:
          containers:
          - args:
            - nginx
            - --invalid-option        # Invalid nginx argument
            - some-value
            - -g
            - daemon off;
            env:
            - name: API_VERSION
              value: v1
            image: nginx:latest
            name: api
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
        roleName: api
    - name: web
      spec:
        minAvailable: 2
        podSpec:
          containers:
          - env:
            - name: SERVER_NAME
              value: web.example.com
            - name: API_ENDPOINT
              value: http://api
            image: httpd:2.4-alpine
            name: web
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
        roleName: web
    - name: db
      spec:
        minAvailable: 1
        podSpec:
          containers:
          - env:
            - name: MYSQL_ROOT_PASSWORD
              value: example
            - name: MYSQL_DATABASE
              value: appdb
            - name: MYSQL_USER
              value: appuser
            - name: MYSQL_PASSWORD
              value: apppass
            image: mysql:8.0
            name: db
            ports:
            - containerPort: 3306
              name: mysql
              protocol: TCP
            resources:
              requests:
                cpu: 50m
                memory: 256Mi
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
        replicas: 1
        roleName: db
    - name: backup
      spec:
        minAvailable: 1
        podSpec:
          containers:
          - command:
            - /bin/sh
            - -c
            - |
              echo "Backup service started"
              while true; do
                echo "$(date): Running backup job..."
                echo "Backing up database..."
                sleep 3600  # Run every hour
              done
            env:
            - name: BACKUP_RETENTION_DAYS
              value: "7"
            - name: BACKUP_DESTINATION
              value: /backups
            image: busybox:latest
            name: backup
            resources:
              requests:
                cpu: 5m
                memory: 16Mi
          restartPolicy: Always
          terminationGracePeriodSeconds: 30
        replicas: 1
        roleName: backup
    headlessServiceConfig:
      publishNotReadyAddresses: true
    podCliqueScalingGroups:
    - cliqueNames:
      - api
      - web
      - db
      minAvailable: 1
      name: main
      replicas: 1
    terminationDelay: 4h0m0s
status:
  availableReplicas: 0                    # No replicas available due to scaling group issues
  currentGenerationHash: 6f765b98fc6bd4dfc98
  observedGeneration: 1
  replicas: 1
  updatedReplicas: 0                      # No replicas fully updated/ready
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCliqueCreateOrUpdateSuccessful | 23s (x9 over 24s) | podcliqueset-controller | PodClique default/container-invalid-args-sg-0-backup created or updated successfully |
| Normal | PodCliqueScalingGroupCreateSuccessful | 23s (x9 over 24s) | podcliqueset-controller | Created PodCliqueScalingGroup default/container-invalid-args-sg-0-main |
| Normal | PodGangCreateOrUpdateSuccessful | 23s (x7 over 24s) | podcliqueset-controller | Created/Updated PodGang default/container-invalid-args-sg-0 |

## 2. PodCliqueScalingGroup Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: container-invalid-args-sg-0-main
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podcliquescalinggroup
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-args-sg-0-main
    app.kubernetes.io/part-of: container-invalid-args-sg
    grove.io/podcliqueset-replica-index: "0"
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-invalid-args-sg
    uid: 60b74a31-457b-4d50-8438-0359b6091ebf
spec:
  cliqueNames:
  - api
  - web
  - db
  minAvailable: 1
  replicas: 1
status:
  availableReplicas: 0                    # No replicas available
  conditions:
  - lastTransitionTime: "2025-11-12T21:01:53Z"
    message: 'Insufficient PodCliqueScalingGroup ready replicas, expected at least:
      1, found: 0'
    reason: InsufficientAvailablePodCliqueScalingGroupReplicas
    status: "True"                        # MinAvailableBreached condition
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 6f765b98fc6bd4dfc98
  observedGeneration: 1
  replicas: 1
  scheduledReplicas: 1
  updatedReplicas: 0                      # No replicas fully updated
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCliqueCreateSuccessful | 25s | podcliquescalinggroup-controller | PodClique default/container-invalid-args-sg-0-main-0-db created successfully |
| Normal | PodCliqueCreateSuccessful | 25s | podcliquescalinggroup-controller | PodClique default/container-invalid-args-sg-0-main-0-api created successfully |
| Normal | PodCliqueCreateSuccessful | 25s | podcliquescalinggroup-controller | PodClique default/container-invalid-args-sg-0-main-0-web created successfully |

## 3. PodClique Level - api (Failing, Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-invalid-args-sg-0-main-0-api
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-args-sg-0-main-0-api
    app.kubernetes.io/part-of: container-invalid-args-sg
    grove.io/pod-template-hash: 5c549fdc65cc4475554f
    grove.io/podcliquescalinggroup: container-invalid-args-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-invalid-args-sg-0
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-invalid-args-sg-0-main
    uid: 3695c5f6-7ec2-4459-a5c9-bd23abe964c8
spec:
  minAvailable: 2
  podSpec:
    containers:
    - args:
      - nginx
      - --invalid-option            # Invalid argument causing failure
      - some-value
      - -g
      - daemon off;
      env:
      - name: API_VERSION
        value: v1
      - name: GROVE_PCSG_NAME
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup']
      - name: GROVE_PCSG_TEMPLATE_NUM_PODS
        value: "5"
      - name: GROVE_PCSG_INDEX
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup-replica-index']
      image: nginx:latest
      name: api
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
  roleName: api
status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:01:52Z"
    message: 'Sufficient scheduled pods found. expected at least: 2, found: 2'
    reason: SufficientScheduledPods
    status: "True"                        # Pods are scheduled
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:01:53Z"
    message: 'Insufficient ready or starting pods. expected at least: 2, found: 0'
    reason: InsufficientReadyPods
    status: "True"                        # MinAvailableBreached - pods not ready
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 6f765b98fc6bd4dfc98
  currentPodTemplateHash: 5c549fdc65cc4475554f
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
| Normal | PodCreateSuccessful | 27s | podclique-controller | Created Pod: container-invalid-args-sg-0-main-0-api-zmtqp |
| Normal | PodCreateSuccessful | 27s | podclique-controller | Created Pod: container-invalid-args-sg-0-main-0-api-bxvkf |

## 4. PodClique Level - web (Healthy, Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-invalid-args-sg-0-main-0-web
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-args-sg-0-main-0-web
    app.kubernetes.io/part-of: container-invalid-args-sg
    grove.io/pod-template-hash: 5f7f6d8b7cc5dd64987
    grove.io/podcliquescalinggroup: container-invalid-args-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-invalid-args-sg-0
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-invalid-args-sg-0-main
    uid: 3695c5f6-7ec2-4459-a5c9-bd23abe964c8
spec:
  minAvailable: 2
  podSpec:
    containers:
    - env:
      - name: SERVER_NAME
        value: web.example.com
      - name: API_ENDPOINT
        value: http://api
      - name: GROVE_PCSG_NAME
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup']
      - name: GROVE_PCSG_TEMPLATE_NUM_PODS
        value: "5"
      - name: GROVE_PCSG_INDEX
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup-replica-index']
      image: httpd:2.4-alpine
      name: web
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
  roleName: web
status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:01:52Z"
    message: 'Sufficient scheduled pods found. expected at least: 2, found: 2'
    reason: SufficientScheduledPods
    status: "True"                        # Pods are scheduled
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:01:52Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 2,
      found: 2'
    reason: SufficientReadyPods
    status: "False"                       # MinAvailable NOT breached - pods are ready
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 6f765b98fc6bd4dfc98
  currentPodTemplateHash: 5f7f6d8b7cc5dd64987
  observedGeneration: 1
  readyReplicas: 2                        # Both pods are ready
  replicas: 2
  scheduleGatedReplicas: 0
  scheduledReplicas: 2
  updatedReplicas: 2
```

### Events

| Type | Reason | Age | From | Message |
|------|--------|-----|------|---------|
| Normal | PodCreateSuccessful | 33s | podclique-controller | Created Pod: container-invalid-args-sg-0-main-0-web-jx56g |
| Normal | PodCreateSuccessful | 33s | podclique-controller | Created Pod: container-invalid-args-sg-0-main-0-web-m6xgp |

## 5. PodClique Level - db (Healthy, Part of Scaling Group)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-invalid-args-sg-0-main-0-db
  namespace: default
  labels:
    app.kubernetes.io/component: pcsg-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-args-sg-0-main-0-db
    app.kubernetes.io/part-of: container-invalid-args-sg
    grove.io/pod-template-hash: 58b84758c969b948c545
    grove.io/podcliquescalinggroup: container-invalid-args-sg-0-main
    grove.io/podcliquescalinggroup-replica-index: "0"
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-invalid-args-sg-0
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueScalingGroup
    name: container-invalid-args-sg-0-main
    uid: 3695c5f6-7ec2-4459-a5c9-bd23abe964c8
spec:
  minAvailable: 1
  podSpec:
    containers:
    - env:
      - name: MYSQL_ROOT_PASSWORD
        value: example
      - name: MYSQL_DATABASE
        value: appdb
      - name: MYSQL_USER
        value: appuser
      - name: MYSQL_PASSWORD
        value: apppass
      - name: GROVE_PCSG_NAME
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup']
      - name: GROVE_PCSG_TEMPLATE_NUM_PODS
        value: "5"
      - name: GROVE_PCSG_INDEX
        valueFrom:
          fieldRef:
            fieldPath: metadata.labels['grove.io/podcliquescalinggroup-replica-index']
      image: mysql:8.0
      name: db
      ports:
      - containerPort: 3306
        name: mysql
        protocol: TCP
      resources:
        requests:
          cpu: 50m
          memory: 256Mi
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
  replicas: 1
  roleName: db
status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:01:52Z"
    message: 'Sufficient scheduled pods found. expected at least: 1, found: 1'
    reason: SufficientScheduledPods
    status: "True"                        # Pods are scheduled
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:01:52Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 1,
      found: 1'
    reason: SufficientReadyPods
    status: "False"                       # MinAvailable NOT breached - pods are ready
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 6f765b98fc6bd4dfc98
  currentPodTemplateHash: 58b84758c969b948c545
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
| Normal | PodCreateSuccessful | 35s | podclique-controller | Created Pod: container-invalid-args-sg-0-main-0-db-9jsx8 |

## 6. PodClique Level - backup (Healthy, Standalone)

```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: container-invalid-args-sg-0-backup
  namespace: default
  labels:
    app.kubernetes.io/component: pcs-podclique
    app.kubernetes.io/managed-by: grove-operator
    app.kubernetes.io/name: container-invalid-args-sg-0-backup
    app.kubernetes.io/part-of: container-invalid-args-sg
    grove.io/pod-template-hash: cb65cf4b5d99cdd784d
    grove.io/podcliqueset-replica-index: "0"
    grove.io/podgang: container-invalid-args-sg-0
  ownerReferences:
  - apiVersion: grove.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: PodCliqueSet
    name: container-invalid-args-sg
    uid: 60b74a31-457b-4d50-8438-0359b6091ebf
spec:
  minAvailable: 1
  podSpec:
    containers:
    - command:
      - /bin/sh
      - -c
      - |
        echo "Backup service started"
        while true; do
          echo "$(date): Running backup job..."
          echo "Backing up database..."
          sleep 3600  # Run every hour
        done
      env:
      - name: BACKUP_RETENTION_DAYS
        value: "7"
      - name: BACKUP_DESTINATION
        value: /backups
      image: busybox:latest
      name: backup
      resources:
        requests:
          cpu: 5m
          memory: 16Mi
    restartPolicy: Always
    terminationGracePeriodSeconds: 30
  replicas: 1
  roleName: backup
status:
  conditions:
  - lastTransitionTime: "2025-11-12T21:01:52Z"
    message: 'Sufficient scheduled pods found. expected at least: 1, found: 1'
    reason: SufficientScheduledPods
    status: "True"                        # Pods are scheduled
    type: PodCliqueScheduled
  - lastTransitionTime: "2025-11-12T21:01:52Z"
    message: 'Either sufficient ready or starting pods found. expected at least: 1,
      found: 1'
    reason: SufficientReadyPods
    status: "False"                       # MinAvailable NOT breached - pods are ready
    type: MinAvailableBreached
  currentPodCliqueSetGenerationHash: 6f765b98fc6bd4dfc98
  currentPodTemplateHash: cb65cf4b5d99cdd784d
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
| Normal | PodCreateSuccessful | 36s | podclique-controller | Created Pod: container-invalid-args-sg-0-backup-kkwbb |
