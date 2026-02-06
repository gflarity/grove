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

### 4. Topology Demo - Single-Level (`base/topology-pcs.yaml`)
- **Setup Required**: Cluster created via `setup-cluster` with topology enabled
- **Quick Start Instructions**: ✅ Clearly documented in YAML header
- **Requirements**:
  - `setup-cluster` provides topology labels on nodes, kai-scheduler, and topology-enabled Grove operator
  - At least 7 worker nodes with rack/block topology labels
  - Nodes are tainted; workloads use affinity/tolerations to target them
- **Commands**:
  ```bash
  kubectl apply -f base/topology-pcs.yaml
  # Verify all 4 pods land in the same rack
  kubectl get pods -l app.kubernetes.io/part-of=topology-pcs -o wide
  ```

### 5. Topology Demo - Multi-Level (`base/topology-multi-level-pcs.yaml`)
- **Setup Required**: Cluster created via `setup-cluster` with topology enabled
- **Quick Start Instructions**: ✅ Clearly documented in YAML header
- **Requirements**:
  - `setup-cluster` provides topology labels on nodes, kai-scheduler, and topology-enabled Grove operator
  - At least 28 worker nodes with rack/block topology labels
  - Nodes are tainted; workloads use affinity/tolerations to target them
- **Commands**:
  ```bash
  kubectl apply -f base/topology-multi-level-pcs.yaml
  # Verify worker-rack pods share a rack, worker-block pods share a block
  kubectl get pods -l app.kubernetes.io/part-of=topology-multi-level-pcs -o wide
  ```

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