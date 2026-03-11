# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `01-typo-registry-with-scaling-group.yaml` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: image-error-typo-registry-sg
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  currentGenerationHash: 8d85b7f8dc75cb8cd6b

# Events
Events:
  Type    Reason                                 Age              From                     Message
  ----    ------                                 ----             ----                     -------
  Normal  PodCliqueCreateOrUpdateSuccessful      0s (x8 over 0s)  podcliqueset-controller  PodClique default/image-error-typo-registry-sg-0-monitoring created or updated successfully
  Normal  PodCliqueScalingGroupCreateSuccessful  0s (x8 over 0s)  podcliqueset-controller  Created PodCliqueScalingGroup default/image-error-typo-registry-sg-0-main
  Normal  PodGangCreateOrUpdateSuccessful        0s (x5 over 0s)  podcliqueset-controller  Created/Updated PodGang default/image-error-typo-registry-sg-0
```

## 2. PodCliqueScalingGroup Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: image-error-typo-registry-sg-0-main
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
      lastTransitionTime: "2025-11-12T16:54:45Z"

# Events
Events:
  Type    Reason                     Age   From                              Message
  ----    ------                     ----  ----                              -------
  Normal  PodCliqueCreateSuccessful  31s   podcliquescalinggroup-controller  PodClique default/image-error-typo-registry-sg-0-main-0-worker created successfully
  Normal  PodCliqueCreateSuccessful  31s   podcliquescalinggroup-controller  PodClique default/image-error-typo-registry-sg-0-main-0-api created successfully
  Normal  PodCliqueCreateSuccessful  31s   podcliquescalinggroup-controller  PodClique default/image-error-typo-registry-sg-0-main-0-web created successfully
```

## 3. PodClique Level

### PodClique with Image Error (api) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-typo-registry-sg-0-main-0-api
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
      lastTransitionTime: "2025-11-12T16:54:45Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:54:45Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  32s   podclique-controller  Created Pod: image-error-typo-registry-sg-0-main-0-api-dtgd4
  Normal  PodCreateSuccessful  32s   podclique-controller  Created Pod: image-error-typo-registry-sg-0-main-0-api-jmz8l
```

**Note:** Pods are in `ErrImagePull` state with error: `Failed to pull image "registr:5001/nginx:latest": failed to pull and unpack image "registr:5001/nginx:latest": failed to resolve reference "registr:5001/nginx:latest": failed to do request: Head "https://registr:5001/v2/nginx/manifests/latest": dial tcp: lookup registr: no such host`

### Healthy PodClique (web) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-typo-registry-sg-0-main-0-web
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
      lastTransitionTime: "2025-11-12T16:54:45Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:54:45Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  32s   podclique-controller  Created Pod: image-error-typo-registry-sg-0-main-0-web-l8jzl
  Normal  PodCreateSuccessful  32s   podclique-controller  Created Pod: image-error-typo-registry-sg-0-main-0-web-cf6f6
```

### Healthy PodClique (worker) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-typo-registry-sg-0-main-0-worker
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
      lastTransitionTime: "2025-11-12T16:54:45Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:54:45Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  32s   podclique-controller  Created Pod: image-error-typo-registry-sg-0-main-0-worker-hzbck
```

### Healthy Standalone PodClique (monitoring):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-typo-registry-sg-0-monitoring
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
      lastTransitionTime: "2025-11-12T16:54:45Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:54:45Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  32s   podclique-controller  Created Pod: image-error-typo-registry-sg-0-monitoring-tdc4x
```
