# E2E Test Helpers for Demo Creation

This document catalogs useful E2E test helpers that can be leveraged when creating demo scripts and automation.

## Node Management Helpers

### CordonNode()
- **Location**: `e2e/utils/k8s_client.go:310`
- **Signature**: `func CordonNode(ctx context.Context, clientset kubernetes.Interface, nodeName string, cordon bool) error`
- **Purpose**: Cordon or uncordon Kubernetes nodes to trigger scheduling issues
- **Usage in Demos**:
  - Cordon all nodes to demonstrate gang scheduling blocked scenarios
  - Uncordon nodes to show recovery from scheduling constraints
- **Example**:
  ```go
  clientset, _ := kubernetes.NewForConfig(restConfig)
  CordonNode(ctx, clientset, "node-1", true)  // Cordon
  CordonNode(ctx, clientset, "node-1", false) // Uncordon
  ```

### Node Labeling Functions
- **Location**: `e2e/setup/k8s_clusters.go`
- **Purpose**: Apply labels to nodes for affinity demos
- **Usage**: Configure `NodeLabels` in `ClusterConfig` during cluster setup
- **Example**:
  ```go
  cfg := ClusterConfig{
    NodeLabels: []NodeLabel{
      {Key: "demo-label", Value: "demo-value", NodeFilters: []string{"agent:*"}},
    },
  }
  ```

## Resource Application Helpers

### ApplyYAMLFile()
- **Location**: `e2e/utils/k8s_client.go:55`
- **Signature**: `func ApplyYAMLFile(ctx context.Context, yamlFilePath string, namespace string, restConfig *rest.Config, logger *Logger) ([]AppliedResource, error)`
- **Purpose**: Apply YAML files containing Kubernetes resources programmatically
- **Usage in Demos**:
  - Deploy demo workloads from YAML templates
  - Apply configuration resources (quotas, limits, etc.)
  - Clean, consistent deployment across demo runs
- **Returns**: List of applied resources for tracking
- **Example**:
  ```go
  resources, err := ApplyYAMLFile(ctx, "demo-workloads/errors/group1-image/01-typo-registry.yaml", "demo-ns", restConfig, logger)
  ```

### applyYAMLData()
- **Location**: `e2e/utils/k8s_client.go:123`
- **Purpose**: Internal function for applying YAML data (can be used with in-memory YAML)
- **Usage**: When generating YAML dynamically in demo scripts

## Pod Monitoring Helpers

### WaitForPods()
- **Location**: `e2e/utils/k8s_client.go:69`
- **Signature**: `func WaitForPods(ctx context.Context, restConfig *rest.Config, namespaces []string, labelSelector string, timeout time.Duration, interval time.Duration, logger *Logger) error`
- **Purpose**: Wait for pods to be ready in specified namespaces
- **Usage in Demos**:
  - Wait for pods to enter error states (with appropriate timeout)
  - Verify pod status changes during demos
  - Show timing of error propagation
- **Parameters**:
  - `labelSelector`: Optional label selector (empty string for all pods)
  - `timeout`: Timeout duration (0 defaults to 5 minutes)
  - `interval`: Poll interval (0 defaults to 5 seconds)
- **Example**:
  ```go
  // Wait for pods to be ready (or fail)
  err := WaitForPods(ctx, restConfig, []string{"demo-ns"}, "app=demo", 2*time.Minute, 5*time.Second, logger)
  ```

### WaitForPodsInNamespace()
- **Location**: `e2e/utils/k8s_client.go:287`
- **Signature**: `func WaitForPodsInNamespace(ctx context.Context, namespace string, restConfig *rest.Config, timeout time.Duration, interval time.Duration, logger *Logger) error`
- **Purpose**: Convenience wrapper for waiting for pods in a single namespace
- **Usage**: Simplified version of `WaitForPods()` for single namespace scenarios

### isPodReady()
- **Location**: `e2e/utils/k8s_client.go:301`
- **Signature**: `func isPodReady(pod *v1.Pod) bool`
- **Purpose**: Check if a pod is in Ready state
- **Usage**: Custom pod status checking in demo scripts

## Cluster Setup Helpers

### SetupCompleteK3DCluster()
- **Location**: `e2e/setup/k8s_clusters.go:177`
- **Signature**: `func SetupCompleteK3DCluster(ctx context.Context, cfg ClusterConfig, skaffoldYAMLPath string, logger *utils.Logger) (*rest.Config, func(), error)`
- **Purpose**: Create complete k3d cluster with Grove, Kai Scheduler, and NVIDIA GPU Operator
- **Usage in Demos**:
  - Set up demo environment
  - Configure cluster with specific node labels/taints
  - Pre-load images for faster demo execution
- **Returns**: REST config and cleanup function
- **Example**:
  ```go
  cfg := DefaultClusterConfig()
  cfg.Name = "demo-cluster"
  cfg.EnableRegistry = true
  restConfig, cleanup, err := SetupCompleteK3DCluster(ctx, cfg, "skaffold.yaml", logger)
  defer cleanup()
  ```

### SetupK3DCluster()
- **Location**: `e2e/setup/k8s_clusters.go:334`
- **Purpose**: Create basic k3d cluster without additional components
- **Usage**: When only basic cluster is needed for demos

### ClusterConfig
- **Location**: `e2e/setup/k8s_clusters.go:73`
- **Purpose**: Configuration struct for cluster creation
- **Fields**:
  - `Name`: Cluster name
  - `ControlPlaneNodes`: Number of control plane nodes
  - `WorkerNodes`: Number of worker nodes
  - `NodeLabels`: Labels to apply to nodes
  - `WorkerNodeTaints`: Taints to apply to worker nodes
  - `EnableRegistry`: Enable local Docker registry
  - `RegistryPort`: Port for registry

## Node Monitoring Helpers

### StartNodeMonitoring()
- **Location**: `e2e/setup/k8s_clusters.go:628`
- **Signature**: `func StartNodeMonitoring(ctx context.Context, clientset *kubernetes.Clientset, logger *utils.Logger) func()`
- **Purpose**: Monitor nodes for not-ready status and automatically replace them
- **Usage**: 
  - Automatically handle node failures during demos
  - Ensure cluster stability during long-running demos
- **Returns**: Cleanup function to stop monitoring
- **Note**: Automatically started by `SetupCompleteK3DCluster()`

## Image Management Helpers

### prepullImages()
- **Location**: `e2e/setup/k8s_clusters.go:768`
- **Purpose**: Pre-pull container images and push to local registry
- **Usage**: Speed up demo execution by pre-loading images
- **Note**: Called automatically when `EnableRegistry` is true in `ClusterConfig`

## Shared Cluster Management

### SharedClusterManager
- **Location**: `e2e/setup/shared_cluster.go`
- **Purpose**: Manages cluster state between tests/demos
- **Usage**: 
  - Share cluster across multiple demo scenarios
  - Manage resource cleanup
  - Coordinate namespace creation

## Logger

### Logger
- **Location**: `e2e/utils/logger.go` (implied)
- **Purpose**: Structured logging for demo output
- **Usage**: All helper functions accept a logger parameter
- **Features**:
  - Debug, Info, Warn, Error levels
  - Formatted output with emojis for visual clarity
  - Level-based filtering

## Best Practices for Using Helpers in Demos

1. **Use ApplyYAMLFile() for Deployment**: Ensures consistent resource application
2. **Leverage CordonNode() for Scheduling Demos**: More reliable than kubectl commands
3. **Use WaitForPods() with Appropriate Timeouts**: Account for error state transitions
4. **Follow SharedClusterManager Patterns**: For reliable cleanup and resource management
5. **Use Logger for Output**: Provides consistent, formatted demo output
6. **Pre-load Images**: Use registry setup for faster demo execution
7. **Monitor Node Health**: Let StartNodeMonitoring() handle node failures automatically

## Integration with Demo Scripts

Demo scripts should:
- Import `e2e/utils` and `e2e/setup` packages
- Use helper functions instead of direct kubectl calls where possible
- Follow the same patterns as E2E tests for consistency
- Leverage cleanup functions for proper resource management
- Use logger for all output to maintain consistency
