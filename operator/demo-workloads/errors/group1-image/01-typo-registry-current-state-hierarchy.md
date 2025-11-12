# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `01-typo-registry.yaml` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: image-error-typo-registry
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  currentGenerationHash: 5bb6dd764558df6469c4

# Events
Events:
  Type    Reason                             Age                From                     Message
  ----    ------                             ----               ----                     -------
  Normal  PodCliqueCreateOrUpdateSuccessful  31s (x7 over 36s)  podcliqueset-controller  PodClique default/image-error-typo-registry-0-api created or updated successfully
  Normal  PodCliqueCreateOrUpdateSuccessful  31s (x7 over 36s)  podcliqueset-controller  PodClique default/image-error-typo-registry-0-web created or updated successfully
  Normal  PodGangCreateOrUpdateSuccessful    31s (x5 over 35s)  podcliqueset-controller  Created/Updated PodGang default/image-error-typo-registry-0
```

## 2. PodCliqueScalingGroup Level

No PodCliqueScalingGroup resources exist in this scenario (simple PodCliqueSet without scaling groups).

## 3. PodClique Level

### PodClique with Image Error (api):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-typo-registry-0-api
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
      lastTransitionTime: "2025-11-12T16:49:54Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:49:53Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  36s   podclique-controller  Created Pod: image-error-typo-registry-0-api-9bkzd
  Normal  PodCreateSuccessful  36s   podclique-controller  Created Pod: image-error-typo-registry-0-api-5l8bj
```

**Note:** Pods are in `ErrImagePull` state with error: `Failed to pull image "registr:5001/nginx:latest": failed to resolve reference "registr:5001/nginx:latest": failed to do request: Head "https://registr:5001/v2/nginx/manifests/latest": dial tcp: lookup registr: no such host`

### Healthy PodClique (web):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-typo-registry-0-web
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
      lastTransitionTime: "2025-11-12T16:49:54Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:49:53Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  37s   podclique-controller  Created Pod: image-error-typo-registry-0-web-ftdvk
  Normal  PodCreateSuccessful  37s   podclique-controller  Created Pod: image-error-typo-registry-0-web-pm8vj
```
