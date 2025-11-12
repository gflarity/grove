# Demo Setup Requirements Summary

This document summarizes which demo scenarios require special setup.

## ✅ Scenarios with Setup Requirements

### 1. Resource Quota Demo (`errors/group4-resources/03-quota-exceeded.yaml`)
- **Setup Required**: Apply `setup-quota.yaml` first
- **Quick Start Instructions**: ✅ Clearly documented in YAML header
- **Commands**:
  ```bash
  kubectl apply -f errors/group4-resources/setup-quota.yaml
  kubectl apply -f errors/group4-resources/03-quota-exceeded.yaml
  ```

### 2. Cordoned Nodes Demo (`errors/group3-scheduling/03-cordoned-nodes.yaml`)
- **Setup Required**: Cordon cluster nodes first
- **Quick Start Instructions**: ✅ Clearly documented with commands
- **Commands**:
  ```bash
  # Cordon all nodes
  kubectl get nodes -o name | xargs -I {} kubectl cordon {}
  # Apply scenario
  kubectl apply -f errors/group3-scheduling/03-cordoned-nodes.yaml
  # Don't forget to uncordon after!
  kubectl get nodes -o name | xargs -I {} kubectl uncordon {}
  ```

### 3. Schedule Gates Demo (`errors/group3-scheduling/04-schedule-gates.yaml`)
- **Setup Required**: Custom scheduler that supports schedule gates
- **Quick Start Instructions**: ✅ Noted that custom scheduler may be required
- **Note**: This is more of a limitation than a setup step

## ✅ All Other Scenarios

All other demo scenarios are self-contained and require no special setup:
- ✅ Image error demos (group1-image)
- ✅ Container error demos (group2-container) 
- ✅ Other scheduling demos (node affinity, anti-affinity)
- ✅ Other resource demos (high CPU, high memory, min-available)
- ✅ Complex scenarios (group5-complex)

## Summary

**All demos that need setup have clear instructions in their YAML headers!** Users just need to:
1. Open the YAML file
2. Follow the QUICK START instructions
3. Any required setup steps are listed as step 1

The approach of embedding instructions directly in each YAML file ensures users won't miss setup requirements.

## Note on Pod Labels

Grove Operator uses specific labels for pods:
- Pods are labeled with `app.kubernetes.io/part-of=<podcliqueset-name>`
- This is why the quick-start instructions use: `kubectl get pods -l app.kubernetes.io/part-of=<name>`
- Other useful pod labels include:
  - `grove.io/podclique=<podclique-name>`
  - `grove.io/podgang=<podgang-name>`
  - `app.kubernetes.io/managed-by=grove-operator`