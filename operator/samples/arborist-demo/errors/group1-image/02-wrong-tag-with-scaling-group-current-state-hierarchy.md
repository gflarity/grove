# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `02-wrong-tag-with-scaling-group.yaml` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: image-error-wrong-tag-sg
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  currentGenerationHash: 5b786d666f8fcdd4fdf9

# Events
Events:
  Type    Reason                                 Age                 From                     Message
  ----    ------                                 ----                ----                     -------
  Normal  PodCliqueScalingGroupCreateSuccessful  57s (x9 over 58s)   podcliqueset-controller  Created PodCliqueScalingGroup default/image-error-wrong-tag-sg-0-main
  Normal  PodGangCreateOrUpdateSuccessful        57s (x6 over 58s)   podcliqueset-controller  Created/Updated PodGang default/image-error-wrong-tag-sg-0
  Normal  PodCliqueCreateOrUpdateSuccessful      55s (x10 over 58s)  podcliqueset-controller  PodClique default/image-error-wrong-tag-sg-0-cache created or updated successfully
```

## 2. PodCliqueScalingGroup Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueScalingGroup
metadata:
  name: image-error-wrong-tag-sg-0-main
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
      lastTransitionTime: "2025-11-12T16:55:35Z"

# Events
Events:
  Type    Reason                     Age   From                              Message
  ----    ------                     ----  ----                              -------
  Normal  PodCliqueCreateSuccessful  58s   podcliquescalinggroup-controller  PodClique default/image-error-wrong-tag-sg-0-main-0-api created successfully
  Normal  PodCliqueCreateSuccessful  58s   podcliquescalinggroup-controller  PodClique default/image-error-wrong-tag-sg-0-main-0-database created successfully
  Normal  PodCliqueCreateSuccessful  58s   podcliquescalinggroup-controller  PodClique default/image-error-wrong-tag-sg-0-main-0-web created successfully
```

## 3. PodClique Level

### PodClique with Image Error (api) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-wrong-tag-sg-0-main-0-api
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
      lastTransitionTime: "2025-11-12T16:55:35Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:55:35Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  29s   podclique-controller  Created Pod: image-error-wrong-tag-sg-0-main-0-api-652np
  Normal  PodCreateSuccessful  29s   podclique-controller  Created Pod: image-error-wrong-tag-sg-0-main-0-api-mv4vp
```

**Note:** Pods are in `ErrImagePull` state with error: `Failed to pull image "nginx:definitely-not-a-real-tag": rpc error: code = NotFound desc = failed to pull and unpack image "docker.io/library/nginx:definitely-not-a-real-tag": failed to resolve reference "docker.io/library/nginx:definitely-not-a-real-tag": docker.io/library/nginx:definitely-not-a-real-tag: not found`

### Healthy PodClique (web) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-wrong-tag-sg-0-main-0-web
  namespace: default
status:
  # Aggregate counts
  replicas: 3
  readyReplicas: 3
  scheduledReplicas: 3
  scheduleGatedReplicas: 0
  updatedReplicas: 3
  observedGeneration: 1
  
  # Standard conditions
  conditions:
    - type: PodCliqueScheduled
      status: "True"
      reason: SufficientScheduledPods
      message: "Sufficient scheduled pods found. expected at least: 2, found: 3"
      lastTransitionTime: "2025-11-12T16:55:35Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 3"
      lastTransitionTime: "2025-11-12T16:55:35Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  29s   podclique-controller  Created Pod: image-error-wrong-tag-sg-0-main-0-web-8swlw
  Normal  PodCreateSuccessful  29s   podclique-controller  Created Pod: image-error-wrong-tag-sg-0-main-0-web-46w86
  Normal  PodCreateSuccessful  29s   podclique-controller  Created Pod: image-error-wrong-tag-sg-0-main-0-web-hr4h2
```

### Healthy PodClique (database) - Part of Scaling Group:
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-wrong-tag-sg-0-main-0-database
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
      lastTransitionTime: "2025-11-12T16:55:35Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:55:35Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  29s   podclique-controller  Created Pod: image-error-wrong-tag-sg-0-main-0-database-9sfnh
```

### Healthy Standalone PodClique (cache):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-wrong-tag-sg-0-cache
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
      lastTransitionTime: "2025-11-12T16:55:35Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:55:35Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  29s   podclique-controller  Created Pod: image-error-wrong-tag-sg-0-cache-sphgz
```
