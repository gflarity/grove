#!/usr/bin/env bash
# Setup fake GPU node pools for the arborist demo.
#
# Upgrades the fake-gpu-operator with H200 and B200 node pools, then labels
# worker nodes so that even-numbered racks get H200s and odd-numbered racks
# get B200s.  This matches the topology-gpu-pcs.yaml expected layout:
#
#   block-0: rack-0 (H200) + rack-1 (B200)
#   block-1: rack-2 (H200) + rack-3 (B200)
#
# Prerequisites:
#   - k3d e2e cluster running (make e2e-cluster-up)
#   - fake-gpu-operator installed (python hack/e2e-cluster/config-cluster.py --fake-gpu=yes)
#   - Topology labels already applied to nodes (done by create-e2e-cluster.py)
#
# Usage:
#   ./setup-fake-gpus.sh                         # defaults
#   ./setup-fake-gpus.sh --cluster <name>        # custom cluster name

set -euo pipefail

CLUSTER_NAME="shared-e2e-test-cluster"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cluster) CLUSTER_NAME="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

SERVER="k3d-${CLUSTER_NAME}-server-0"

kc() {
  docker exec "$SERVER" kubectl "$@"
}

echo "==> Upgrading fake-gpu-operator with H200/B200 node pools..."
helm upgrade -i fake-gpu-operator \
  oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator \
  --namespace gpu-operator \
  --version 0.0.72 \
  --devel \
  --set computeDomainDraPlugin.enabled=false \
  --set topology.nodePoolLabelKey=run.ai/simulated-gpu-node-pool \
  --set 'topology.nodePools.h200.gpuCount=8' \
  --set 'topology.nodePools.h200.gpuMemory=143360' \
  --set 'topology.nodePools.h200.gpuProduct=NVIDIA-H200-141GB-HBM3e' \
  --set 'topology.nodePools.b200.gpuCount=8' \
  --set 'topology.nodePools.b200.gpuMemory=196608' \
  --set 'topology.nodePools.b200.gpuProduct=NVIDIA-B200-192GB-HBM3e' \
  --wait

echo "==> Labeling worker nodes with GPU node pools..."
# Even racks (0, 2, ...) -> H200, odd racks (1, 3, ...) -> B200
# Also enable GPU operands (create-e2e-cluster.py sets operands=false by default)
# and set nvidia.com/gpu.product label directly (status-updater doesn't set it for kwok nodes)
kc get nodes -l node_role.e2e.grove.nvidia.com=agent \
  -o go-template='{{range .items}}{{.metadata.name}} {{index .metadata.labels "kubernetes.io/rack"}}{{"\n"}}{{end}}' \
| while read -r node rack; do
  rack_num="${rack##rack-}"
  if (( rack_num % 2 == 0 )); then
    pool="h200"
    gpu_product="NVIDIA-H200-141GB-HBM3e"
  else
    pool="b200"
    gpu_product="NVIDIA-B200-192GB-HBM3e"
  fi
  kc label node "$node" \
    run.ai/simulated-gpu-node-pool="$pool" \
    nvidia.com/gpu.deploy.operands=true \
    nvidia.com/gpu.product="$gpu_product" \
    --overwrite
  echo "    $node ($rack) -> $pool ($gpu_product)"
done

echo "==> Patching device-plugin DaemonSet to tolerate e2e node taint..."
kc patch daemonset device-plugin -n gpu-operator --type=json \
  -p '[{"op":"add","path":"/spec/template/spec/tolerations/-","value":{"key":"node_role.e2e.grove.nvidia.com","operator":"Equal","value":"agent","effect":"NoSchedule"}}]'

echo "==> Waiting for device-plugin DaemonSet to roll out..."
kc rollout status daemonset/device-plugin -n gpu-operator --timeout=120s

echo "==> Verifying GPU labels on a sample of nodes..."
kc get nodes -l node_role.e2e.grove.nvidia.com=agent \
  -o go-template='{{range .items}}{{.metadata.name}}{{"\t"}}{{index .metadata.labels "nvidia.com/gpu.product"}}{{"\t"}}gpu={{index .status.allocatable "nvidia.com/gpu"}}{{"\n"}}{{end}}' \
| sort | head -10

echo "==> Done. Fake GPU node pools configured."
