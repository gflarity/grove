# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `04-invalid-port-with-scaling-group.yaml` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: image-error-invalid-port-sg
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  currentGenerationHash: 5b978566db6f966d4d68

# Events
Events:
  Type    Reason                                 Age                 From                     Message
  ----    ------                                 ----                ----                     -------
  Normal  PodCliqueScalingGroupCreateSuccessful  38s (x9 over 40s)   podcliqueset-controller  Created PodCliqueScalingGroup default/image-error-invalid-port-sg-0-main
  Normal  PodGangCreateOrUpdateSuccessful        38s (x6 over 40s)  podcliqueset-controller  Created/Updated PodGang default/image-error-invalid-port-sg-0
  Normal  PodCliqueCreateOrUpdateSuccessful      36s (x10 over 40s) podcliqueset-controller  PodClique default/image-error-invalid-port-sg-0-logger created or updated successfully
```

## 2. PodCliqueScalingGroup Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: image-error-invalid-port-sg-0-main
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  scheduledReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: MinAvailableBreached
      status: "False"
      reason: InsufficientScheduledPodCliqueScalingGroupReplicas
      message: "Insufficient scheduled replicas. expected at least: 1, found: 0"
      lastTransitionTime: "2025-11-12T16:58:33Z"

# Events
Events:
  Type    Reason                     Age   From                              Message
  ----    ------                     ----  ----                              -------
  Normal  PodCliqueCreateSuccessful  38s   podcliquescalinggroup-controller  PodClique default/image-error-invalid-port-sg-0-main-0-api created successfully
  Normal  PodCliqueCreateSuccessful  38s   podcliquescalinggroup-controller  PodClique default/image-error-invalid-port-sg-0-main-0-processor created successfully
  Normal  PodCliqueCreateSuccessful  38s   podcliquescalinggroup-controller  PodClique default/image-error-invalid-port-sg-0-main-0-gateway created successfully
```

## 3. PodClique Level

### PodClique with Invalid Port Error (api) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-invalid-port-sg-0-main-0-api
  namespace: default
status:
  # Aggregate counts
  replicas: 2
  readyReplicas: 0
  scheduledReplicas: 0
  scheduleGatedReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  
  # Last error
  lastErrors:
    - code: ERR_CREATE_POD
      description: "failed to sync Pod: [Operation: Sync, Code: ERR_CREATE_POD] message: failed to create Pod:  for PodClique default/image-error-invalid-port-sg-0-main-0-api, cause: Pod \"image-error-invalid-port-sg-0-main-0-api-tzcw7\" is invalid: spec.containers[0].ports[0].containerPort: Invalid value: 80000: must be between 1 and 65535, inclusive"
      observedAt: "2025-11-12T16:58:53Z"

# Events
Events:
  Type     Reason           Age                From                  Message
  ----     ------           ----               ----                  -------
  Warning  PodCreateFailed  39s                podclique-controller  Error creating pod : Pod "image-error-invalid-port-sg-0-main-0-api-sgjcm" is invalid: spec.containers[0].ports[0].containerPort: Invalid value: 80000: must be between 1 and 65535, inclusive
  Warning  PodCreateFailed  19s (x4 over 37s)  podclique-controller  (combined from similar events): Error creating pod : Pod "image-error-invalid-port-sg-0-main-0-api-tzcw7" is invalid: spec.containers[0].ports[0].containerPort: Invalid value: 80000: must be between 1 and 65535, inclusive
```

**Note:** Pods cannot be created due to invalid container port value (80000 exceeds maximum of 65535)

### Schedule-Gated PodClique (gateway) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-invalid-port-sg-0-main-0-gateway
  namespace: default
status:
  # Aggregate counts
  replicas: 2
  readyReplicas: 0
  scheduledReplicas: 0
  scheduleGatedReplicas: 2
  updatedReplicas: 2
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: PodCliqueScheduled
      status: "False"
      reason: InsufficientScheduledPods
      message: "Insufficient scheduled pods. expected at least: 1, found: 0"
      lastTransitionTime: "2025-11-12T16:58:33Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: InsufficientScheduledPods
      message: "Insufficient scheduled pods. expected at least: 1, found: 0"
      lastTransitionTime: "2025-11-12T16:58:33Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  39s   podclique-controller  Created Pod: image-error-invalid-port-sg-0-main-0-gateway-g54v4
  Normal  PodCreateSuccessful  39s   podclique-controller  Created Pod: image-error-invalid-port-sg-0-main-0-gateway-qjgqd
```

**Note:** Pods are schedule-gated waiting for gang member 'api' to be ready

### Schedule-Gated PodClique (processor) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-invalid-port-sg-0-main-0-processor
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  readyReplicas: 0
  scheduledReplicas: 0
  scheduleGatedReplicas: 1
  updatedReplicas: 1
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: PodCliqueScheduled
      status: "False"
      reason: InsufficientScheduledPods
      message: "Insufficient scheduled pods. expected at least: 1, found: 0"
      lastTransitionTime: "2025-11-12T16:58:33Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: InsufficientScheduledPods
      message: "Insufficient scheduled pods. expected at least: 1, found: 0"
      lastTransitionTime: "2025-11-12T16:58:33Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  39s   podclique-controller  Created Pod: image-error-invalid-port-sg-0-main-0-processor-qqrrn
```

**Note:** Pod is schedule-gated waiting for gang member 'api' to be ready

### Schedule-Gated Standalone PodClique (logger):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-invalid-port-sg-0-logger
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  readyReplicas: 0
  scheduledReplicas: 0
  scheduleGatedReplicas: 1
  updatedReplicas: 1
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: PodCliqueScheduled
      status: "False"
      reason: InsufficientScheduledPods
      message: "Insufficient scheduled pods. expected at least: 1, found: 0"
      lastTransitionTime: "2025-11-12T16:58:33Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: InsufficientScheduledPods
      message: "Insufficient scheduled pods. expected at least: 1, found: 0"
      lastTransitionTime: "2025-11-12T16:58:33Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  39s   podclique-controller  Created Pod: image-error-invalid-port-sg-0-logger-lpbch
```

**Note:** Pod is schedule-gated waiting for gang member 'api' to be ready
