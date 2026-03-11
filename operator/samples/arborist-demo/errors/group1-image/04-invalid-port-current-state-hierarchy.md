# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `04-invalid-port.yaml` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: image-error-invalid-port
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  currentGenerationHash: fb6d6b4b778788cfc4

# Events
Events:
  Type    Reason                             Age                From                     Message
  ----    ------                             ----               ----                     -------
  Normal  PodCliqueCreateOrUpdateSuccessful  26s (x7 over 31s)  podcliqueset-controller  PodClique default/image-error-invalid-port-0-app created or updated successfully
  Normal  PodCliqueCreateOrUpdateSuccessful  26s (x7 over 31s)  podcliqueset-controller  PodClique default/image-error-invalid-port-0-sidecar created or updated successfully
  Normal  PodGangCreateOrUpdateSuccessful    26s (x6 over 31s)  podcliqueset-controller  Created/Updated PodGang default/image-error-invalid-port-0
```

## 2. PodCliqueScalingGroup Level

No PodCliqueScalingGroup resources exist in this scenario (simple PodCliqueSet without scaling groups).

## 3. PodClique Level

### PodClique with Image Error (app):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-invalid-port-0-app
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
      lastTransitionTime: "2025-11-12T16:52:38Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:52:38Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  33s   podclique-controller  Created Pod: image-error-invalid-port-0-app-c49hp
  Normal  PodCreateSuccessful  33s   podclique-controller  Created Pod: image-error-invalid-port-0-app-6gvwt
```

**Note:** Pods are in `ErrImagePull` state with error: `Failed to pull image "localhost:99999/nginx:latest": failed to pull and unpack image "localhost:99999/nginx:latest": failed to resolve reference "localhost:99999/nginx:latest": failed to do request: Head "https://localhost:99999/v2/nginx/manifests/latest": dial tcp: address 99999: invalid port`

### Healthy PodClique (sidecar):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-invalid-port-0-sidecar
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
      lastTransitionTime: "2025-11-12T16:52:38Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:52:38Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  34s   podclique-controller  Created Pod: image-error-invalid-port-0-sidecar-c847p
```
