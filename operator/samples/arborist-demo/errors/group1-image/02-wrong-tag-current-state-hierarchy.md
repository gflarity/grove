# Grove Error Propagation - Current State

This document shows the actual current state of error reporting in Grove using the `02-wrong-tag.yaml` test scenario.

## 1. PodCliqueSet Level

```yaml
apiVersion: grove.io/v1alpha1
kind: PodCliqueSet
metadata:
  name: image-error-wrong-tag
  namespace: default
status:
  # Aggregate counts
  replicas: 1
  availableReplicas: 0
  updatedReplicas: 0
  observedGeneration: 1
  currentGenerationHash: 5c7f887944f65bf6df6

# Events
Events:
  Type    Reason                             Age              From                     Message
  ----    ------                             ----             ----                     -------
  Normal  PodCliqueCreateOrUpdateSuccessful  4s (x6 over 6s)  podcliqueset-controller  PodClique default/image-error-wrong-tag-0-cache created or updated successfully
  Normal  PodGangCreateOrUpdateSuccessful    4s (x5 over 6s)  podcliqueset-controller  Created/Updated PodGang default/image-error-wrong-tag-0
  Normal  PodCliqueCreateOrUpdateSuccessful  2s (x7 over 6s)  podcliqueset-controller  PodClique default/image-error-wrong-tag-0-backend created or updated successfully
  Normal  PodCliqueCreateOrUpdateSuccessful  2s (x7 over 6s)  podcliqueset-controller  PodClique default/image-error-wrong-tag-0-frontend created or updated successfully
```

## 2. PodCliqueScalingGroup Level

No PodCliqueScalingGroup resources exist in this scenario (simple PodCliqueSet without scaling groups).

## 3. PodClique Level

### PodClique with Image Error (backend):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-wrong-tag-0-backend
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
      lastTransitionTime: "2025-11-12T16:51:02Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:51:01Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  37s   podclique-controller  Created Pod: image-error-wrong-tag-0-backend-r8khl
  Normal  PodCreateSuccessful  37s   podclique-controller  Created Pod: image-error-wrong-tag-0-backend-97kcv
```

**Note:** Pods are in `ErrImagePull` state with error: `Failed to pull image "nginx:does-not-exist": rpc error: code = NotFound desc = failed to pull and unpack image "docker.io/library/nginx:does-not-exist": failed to resolve reference "docker.io/library/nginx:does-not-exist": docker.io/library/nginx:does-not-exist: not found`

### Healthy PodClique (frontend):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-wrong-tag-0-frontend
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
      lastTransitionTime: "2025-11-12T16:51:02Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 2, found: 2"
      lastTransitionTime: "2025-11-12T16:51:01Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  37s   podclique-controller  Created Pod: image-error-wrong-tag-0-frontend-fchbm
  Normal  PodCreateSuccessful  37s   podclique-controller  Created Pod: image-error-wrong-tag-0-frontend-xfkh6
```

### Healthy PodClique (cache):
```yaml
apiVersion: grove.io/v1alpha1
kind: PodClique
metadata:
  name: image-error-wrong-tag-0-cache
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
      lastTransitionTime: "2025-11-12T16:51:02Z"
    
    - type: MinAvailableBreached
      status: "False"
      reason: SufficientReadyPods
      message: "Either sufficient ready or starting pods found. expected at least: 1, found: 1"
      lastTransitionTime: "2025-11-12T16:51:01Z"

# Events
Events:
  Type    Reason               Age   From                  Message
  ----    ------               ----  ----                  -------
  Normal  PodCreateSuccessful  37s   podclique-controller  Created Pod: image-error-wrong-tag-0-cache-5wkqp
```
