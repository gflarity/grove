# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `03-malformed-image-with-scaling-group.yaml` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: image-error-malformed-sg
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  currentGenerationHash: 59d87664747dffb58cd8

# Events
Events:
  Type    Reason                                 Age                 From                     Message
  ----    ------                                 ----                ----                     -------
  Normal  PodCliqueScalingGroupCreateSuccessful  57s (x9 over 58s)   podcliqueset-controller  Created PodCliqueScalingGroup default/image-error-malformed-sg-0-main
  Normal  PodGangCreateOrUpdateSuccessful        57s (x6 over 58s)   podcliqueset-controller  Created/Updated PodGang default/image-error-malformed-sg-0
  Normal  PodCliqueCreateOrUpdateSuccessful      55s (x10 over 58s)  podcliqueset-controller  PodClique default/image-error-malformed-sg-0-metrics created or updated successfully
```

## 2. PodCliqueScalingGroup Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: image-error-malformed-sg-0-main
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  scheduledReplicas: 1
  updatedReplicas: 0
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientAvailablePodCliqueScalingGroupReplicas
      message: "Sufficient PodCliqueScalingGroup ready replicas, expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:57:05Z"

# Events
Events:
  Type    Reason                     Age   From                              Message
  ----    ------                     ----  ----                              -------
  Normal  PodCliqueCreateSuccessful  57s   podcliquescalinggroup-controller  PodClique default/image-error-malformed-sg-0-main-0-backend created successfully
  Normal  PodCliqueCreateSuccessful  57s   podcliquescalinggroup-controller  PodClique default/image-error-malformed-sg-0-main-0-auth created successfully
  Normal  PodCliqueCreateSuccessful  57s   podcliquescalinggroup-controller  PodClique default/image-error-malformed-sg-0-main-0-frontend created successfully
```

## 3. PodClique Level

### PodClique with Image Error (frontend) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-malformed-sg-0-main-0-frontend
  namespace: default
status:
  # Aggregate counts
  replicas: 2
  readyReplicas: 0
  scheduledReplicas: 2
  scheduleGatedReplicas: 0
  updatedReplicas: 2
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: PodCliqueScheduled
      status: "True"
      reason: SufficientScheduledPods
      message: "Sufficient scheduled pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:57:05Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:57:05Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  59s   podclique-controller  Created Pod: image-error-malformed-sg-0-main-0-frontend-bsv5g
  Normal  PodCreateSuccessful  59s   podclique-controller  Created Pod: image-error-malformed-sg-0-main-0-frontend-t7vcv
```

**Note:** Pods are in `InvalidImageName` state with error: `Failed to apply default image tag "nginx::latest@@invalid": couldn't parse image name "nginx::latest@@invalid": invalid reference format`

### Healthy PodClique (backend) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-malformed-sg-0-main-0-backend
  namespace: default
status:
  # Aggregate counts
  replicas: 2
  readyReplicas: 2
  scheduledReplicas: 2
  scheduleGatedReplicas: 0
  updatedReplicas: 2
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: PodCliqueScheduled
      status: "True"
      reason: SufficientScheduledPods
      message: "Sufficient scheduled pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:57:05Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:57:05Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  59s   podclique-controller  Created Pod: image-error-malformed-sg-0-main-0-backend-99xtd
  Normal  PodCreateSuccessful  59s   podclique-controller  Created Pod: image-error-malformed-sg-0-main-0-backend-6fdnz
```

### Healthy PodClique (auth) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-malformed-sg-0-main-0-auth
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  readyReplicas: 1
  scheduledReplicas: 1
  scheduleGatedReplicas: 0
  updatedReplicas: 1
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: PodCliqueScheduled
      status: "True"
      reason: SufficientScheduledPods
      message: "Sufficient scheduled pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:57:05Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:57:05Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  59s   podclique-controller  Created Pod: image-error-malformed-sg-0-main-0-auth-82qjt
```

### Standalone PodClique (metrics):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-malformed-sg-0-metrics
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  readyReplicas: 0
  scheduledReplicas: 1
  scheduleGatedReplicas: 0
  updatedReplicas: 1
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: PodCliqueScheduled
      status: "True"
      reason: SufficientScheduledPods
      message: "Sufficient scheduled pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:57:05Z"
    
    - type: MinAvailableBreached
      status: "True"
      reason: InsufficientReadyPods
      message: "Insufficient ready or starting pods. expected at least: 1, found: 0"
      lastTransitionTime: "2025-11-12T16:57:23Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  59s   podclique-controller  Created Pod: image-error-malformed-sg-0-metrics-5487c
```

**Note:** Metrics pod is in `CrashLoopBackOff` state (unrelated to image error - likely a different issue with the node-exporter configuration)
