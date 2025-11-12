# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `03-malformed-image.yaml` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: image-error-malformed
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  currentGenerationHash: 9869cb8fd9c7f885ffb

# Events
Events:
  Type    Reason                             Age                From                     Message
  ----    ------                             ----               ----                     -------
  Normal  PodCliqueCreateOrUpdateSuccessful  26s (x7 over 31s)  podcliqueset-controller  PodClique default/image-error-malformed-0-monitor created or updated successfully
  Normal  PodCliqueCreateOrUpdateSuccessful  26s (x7 over 31s)  podcliqueset-controller  PodClique default/image-error-malformed-0-worker created or updated successfully
  Normal  PodGangCreateOrUpdateSuccessful    26s (x6 over 31s)  podcliqueset-controller  Created/Updated PodGang default/image-error-malformed-0
```

## 2. PodCliqueScalingGroup Level

No PodCliqueScalingGroup resources exist in this scenario (simple PodCliqueSet without scaling groups).

## 3. PodClique Level

### PodClique with Image Error (worker):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-malformed-0-worker
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
      lastTransitionTime: "2025-11-12T16:51:52Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:51:52Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  33s   podclique-controller  Created Pod: image-error-malformed-0-worker-7slvr
  Normal  PodCreateSuccessful  33s   podclique-controller  Created Pod: image-error-malformed-0-worker-dgmhz
```

**Note:** Pods are in `InvalidImageName` state with error: `Failed to apply default image tag "nginx@sha256:invalid": couldn't parse image name "nginx@sha256:invalid": invalid reference format`

### Healthy PodClique (monitor):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-malformed-0-monitor
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
      lastTransitionTime: "2025-11-12T16:51:52Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:51:52Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  33s   podclique-controller  Created Pod: image-error-malformed-0-monitor-z7r72
```
