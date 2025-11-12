# Grove Setup Cluster Command

A command-line tool for creating a complete K3D cluster with Grove operator, Kai scheduler, and optionally the NVIDIA GPU operator.

## Overview

The `setup-cluster` command provides a simple way to create a fully configured K3D cluster for Grove development and testing. By default, it creates a 31-node cluster (3 control plane + 28 worker nodes) matching the e2e test configuration. It automates all the setup steps that are normally handled by the e2e test framework, making it easy to create a cluster for manual testing or development.

## Features

- Creates a K3D cluster with configurable nodes (defaults to 31 nodes: 3 control plane + 28 workers)
- Sets up a built-in Docker registry for faster image loading
- Pre-pulls and caches all required operator images (Grove, Kai scheduler, GPU operator)
- **Pre-loads test workload images to the registry** (defaults to `nginx:alpine-slim`)
- Deploys Grove operator using Skaffold
- Installs Kai scheduler with Grove integration
- Optionally installs NVIDIA GPU operator
- Automatically writes kubeconfig to `KUBECONFIG` environment variable or default location
- Merges with existing kubeconfig without overwriting other clusters
- Provides graceful shutdown with cleanup
- Configurable logging levels

## Usage

### Basic Usage

Create a cluster with default settings:

```bash
./bin/setup-cluster
```

Or using the wrapper script:

```bash
./bin/grove-setup-cluster
```

### Common Examples

Create a cluster with 3 worker nodes:
```bash
./bin/setup-cluster --worker-nodes 3
```

Create a cluster without GPU operator (faster startup):
```bash
./bin/setup-cluster --skip-gpu-operator
```

Create a cluster with custom name and ports:
```bash
./bin/setup-cluster --name my-cluster --api-port 6551 --registry-port 5002
```

Create a minimal cluster (no optional components):
```bash
./bin/setup-cluster --skip-kai-scheduler --skip-gpu-operator
```

Enable verbose logging:
```bash
./bin/setup-cluster --verbose
```

Specify custom test images to pre-load:
```bash
./bin/setup-cluster --test-images nginx:alpine-slim,ubuntu:22.04,busybox:latest
```

### Command-Line Flags

#### Cluster Configuration
- `--name`: Name of the K3D cluster (default: "grove-cluster")
- `--control-plane-nodes`: Number of control plane nodes (default: 3)
- `--worker-nodes`: Number of worker nodes (default: 28, for 31 total nodes matching e2e test configuration)
- `--k3s-image`: K3s Docker image to use (default: "rancher/k3s:v1.33.5-k3s1")
- `--api-port`: Port on host to expose Kubernetes API (default: "6550")
- `--lb-port`: Load balancer port mapping in format "host:container" (default: "8080:80")
- `--worker-memory`: Memory allocation for worker nodes (default: "150m")
- `--enable-registry`: Enable built-in Docker registry (default: true)
- `--registry-port`: Port for the Docker registry (default: "5001")

#### Component Deployment
- `--skip-grove`: Skip Grove operator deployment
- `--skip-kai-scheduler`: Skip Kai scheduler deployment
- `--skip-gpu-operator`: Skip NVIDIA GPU operator deployment
- `--deployment-mode`: Deployment mode - "skaffold" or "kubectl" (default: "skaffold")
- `--skaffold-path`: Path to skaffold.yaml (defaults to workspace root)

#### Test Images
- `--test-images`: Comma-separated list of test images to pre-load into the registry (default: "nginx:alpine-slim")

#### Logging
- `-v, --verbose`: Enable verbose logging
- `-q, --quiet`: Suppress non-error output

## Building

To build the command:

```bash
cd /home/gflarity/git/grove_error_ux/operator
go build -o bin/setup-cluster ./cmd/setup-cluster
```

The command automatically finds `skaffold.yaml` by walking up the directory tree from wherever you run it, so you can execute it from any subdirectory of the project.

## Interactive Mode

When run from an interactive terminal, the command will wait for Ctrl+C before tearing down the cluster. This allows you to work with the cluster and ensures proper cleanup.

In non-interactive mode (e.g., in scripts), the cluster is created and the command exits, leaving the cluster running.

## Cluster Access

The command automatically writes the kubeconfig to:
1. The path specified in the `KUBECONFIG` environment variable (if set)
2. The default kubeconfig location (`~/.kube/config`) if `KUBECONFIG` is not set

The new cluster context is automatically set as the current context and merged with any existing kubeconfig.

After creating the cluster, you can immediately use kubectl:

```bash
kubectl cluster-info
```

If you wrote to a custom location via `KUBECONFIG`, make sure to export it:
```bash
export KUBECONFIG=/path/to/custom/kubeconfig
kubectl cluster-info
```

## Teardown

To manually tear down the cluster:

```bash
k3d cluster delete grove-cluster
```

Or press Ctrl+C if running in interactive mode.

## Implementation Details

This command uses the same `SetupCompleteK3DCluster` function that's used by the e2e test framework, ensuring consistency between test environments and manual clusters.

### Cluster Configuration

The cluster is configured with the following node labels and taints (matching e2e tests):

**Node Labels:**
- `node_role.e2e.grove.nvidia.com=agent` on all worker nodes
- `nvidia.com/gpu.deploy.operands=false` on all nodes (GPU validator disabled)

**Node Taints:**
- `node_role.e2e.grove.nvidia.com=agent:NoSchedule` on all worker nodes

These labels and taints ensure that:
1. Grove operator pods can schedule on worker nodes (they have matching tolerations)
2. GPU operator validation is disabled (to avoid issues in non-GPU environments)
3. The cluster matches the e2e test environment exactly

### Setup Process

The setup process includes:
1. Creating the K3D cluster with specified configuration, labels, and taints
2. Setting up node monitoring to handle not-ready nodes
3. Pre-pulling and pushing operator images to the local registry
4. Installing Grove operator via Skaffold
5. Installing Kai scheduler with Grove integration
6. Optionally installing NVIDIA GPU operator
7. Waiting for all components to be ready
8. **Pre-loading test workload images to the registry** (e.g., `nginx:alpine-slim`)
9. Writing kubeconfig to the specified location

This matches exactly what the e2e test framework does, ensuring your manual tests behave identically to automated tests.

## Troubleshooting

If the cluster creation fails:
1. Check Docker is running: `docker ps`
2. Ensure k3d is installed: `k3d version`
3. Check for port conflicts on the specified API and registry ports
4. Use `--verbose` flag for detailed logging
5. Check the operator logs: `kubectl logs -n grove-system`

## Related Commands

- `grovectl`: CLI tool for debugging Grove resources
- `k3d`: Tool for managing K3D clusters
- `skaffold`: Tool used for deploying Grove operator