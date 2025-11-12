// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ai-dynamo/grove/operator/e2e/setup"
	"github.com/ai-dynamo/grove/operator/e2e/utils"

	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	k3dclient "github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	k3d "github.com/k3d-io/k3d/v5/pkg/types"
	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var (
	// Cluster configuration flags
	clusterName       string
	controlPlaneNodes int
	workerNodes       int
	k3sImage          string
	hostPort          string
	loadBalancerPort  string
	workerMemory      string
	enableRegistry    bool
	registryPort      string

	// Operator deployment flags
	skipGrove        bool
	skipKaiScheduler bool
	skipGPUOperator  bool
	deploymentMode   string
	skaffoldPath     string

	// Test image flags
	testImages []string

	// Logging flags
	verbose bool
	quiet   bool

	// Help flag
	showHelp bool
)

func main() {
	// Define flags
	fs := pflag.NewFlagSet("setup-cluster", pflag.ContinueOnError)

	// Cluster configuration
	fs.StringVar(&clusterName, "name", "grove-cluster", "Name of the K3D cluster")
	fs.IntVar(&controlPlaneNodes, "control-plane-nodes", 3, "Number of control plane nodes")
	fs.IntVar(&workerNodes, "worker-nodes", 28, "Number of worker nodes")
	fs.StringVar(&k3sImage, "k3s-image", "rancher/k3s:v1.33.5-k3s1", "K3s Docker image to use")
	fs.StringVar(&hostPort, "api-port", "6550", "Port on host to expose Kubernetes API")
	fs.StringVar(&loadBalancerPort, "lb-port", "8080:80", "Load balancer port mapping (host:container)")
	fs.StringVar(&workerMemory, "worker-memory", "150m", "Memory allocation for worker nodes")
	fs.BoolVar(&enableRegistry, "enable-registry", true, "Enable built-in Docker registry")
	fs.StringVar(&registryPort, "registry-port", "5001", "Port for the Docker registry")

	// Operator deployment
	fs.BoolVar(&skipGrove, "skip-grove", false, "Skip Grove operator deployment")
	fs.BoolVar(&skipKaiScheduler, "skip-kai-scheduler", false, "Skip Kai scheduler deployment")
	fs.BoolVar(&skipGPUOperator, "skip-gpu-operator", false, "Skip NVIDIA GPU operator deployment")
	fs.StringVar(&deploymentMode, "deployment-mode", "skaffold", "Deployment mode (skaffold, kubectl)")
	fs.StringVar(&skaffoldPath, "skaffold-path", "", "Path to skaffold.yaml (defaults to workspace root)")

	// Test images
	fs.StringSliceVar(&testImages, "test-images", []string{"nginx:alpine-slim"}, "Comma-separated list of test images to pre-load into registry")

	// Logging
	fs.BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	fs.BoolVarP(&quiet, "quiet", "q", false, "Suppress non-error output")

	// Help
	fs.BoolVarP(&showHelp, "help", "h", false, "Show help message")

	// Parse command line
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == pflag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	// Show help if requested
	if showHelp {
		printHelp(fs)
		os.Exit(0)
	}

	// Set up logging
	logger := utils.NewTestLogger(getLogLevel())

	// Create cluster configuration matching the e2e test setup
	cfg := setup.ClusterConfig{
		Name:              clusterName,
		ControlPlaneNodes: controlPlaneNodes,
		WorkerNodes:       workerNodes,
		Image:             k3sImage,
		HostPort:          hostPort,
		LoadBalancerPort:  loadBalancerPort,
		WorkerMemory:      workerMemory,
		EnableRegistry:    enableRegistry,
		RegistryPort:      registryPort,
		NodeLabels: []setup.NodeLabel{
			{
				Key:   "node_role.e2e.grove.nvidia.com",
				Value: "agent",
				// k3s refers to worker nodes as agent nodes
				NodeFilters: []string{"agent:*"},
			},
			// Disable GPU deployment on all nodes (validator causes issues)
			{
				Key:         "nvidia.com/gpu.deploy.operands",
				Value:       "false",
				NodeFilters: []string{"server:*", "agent:*"},
			},
		},
		WorkerNodeTaints: []setup.NodeTaint{
			{
				Key:    "node_role.e2e.grove.nvidia.com",
				Value:  "agent",
				Effect: "NoSchedule",
			},
		},
	}

	// Determine skaffold path
	if skaffoldPath == "" {
		// Try to find skaffold.yaml by walking up directory tree
		cwd, err := os.Getwd()
		if err != nil {
			logger.Errorf("Failed to get current working directory: %v", err)
			os.Exit(1)
		}

		// Walk up the directory tree looking for skaffold.yaml
		skaffoldPath = findSkaffoldYAML(cwd)
		if skaffoldPath == "" {
			logger.Errorf("Could not find skaffold.yaml in %s or any parent directory", cwd)
			logger.Errorf("Please specify the path with --skaffold-path flag")
			os.Exit(1)
		}
		logger.Debugf("Using skaffold.yaml from: %s", skaffoldPath)
	} else {
		// Validate specified skaffold path exists
		if _, err := os.Stat(skaffoldPath); err != nil {
			logger.Errorf("Skaffold file not found at %s: %v", skaffoldPath, err)
			os.Exit(1)
		}
	}

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Infof("Received signal %v, initiating graceful shutdown...", sig)
		cancel()
	}()

	// Print configuration
	if !quiet {
		printConfiguration(&cfg, logger)
	}

	// Set up the cluster
	logger.Info("🚀 Setting up K3D cluster with Grove operator...")

	_, cleanupFunc, err := setup.SetupCompleteK3DCluster(ctx, cfg, skaffoldPath, logger)
	if err != nil {
		logger.Errorf("Failed to setup K3D cluster: %v", err)
		if cleanupFunc != nil {
			logger.Info("Running cleanup...")
			cleanupFunc()
		}
		os.Exit(1)
	}

	// Wrap cleanup to use a fresh context with timeout
	// This ensures cleanup works even if the main context is cancelled
	// The cleanup function from SetupCompleteK3DCluster uses the original context,
	// which may be cancelled, so we need to delete the cluster manually with a fresh context
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()

		logger.Debug("🗑️ Deleting cluster...")
		cluster := &k3d.Cluster{Name: clusterName}
		if err := k3dclient.ClusterDelete(cleanupCtx, runtimes.Docker, cluster, k3d.ClusterDeleteOpts{}); err != nil {
			logger.Errorf("Failed to delete cluster: %v", err)
		} else {
			logger.Info("✅ Cluster deleted successfully")
		}
	}

	// Setup test images in registry (matching what e2e tests do)
	logger.Infof("📦 Pre-loading %d test image(s) to registry...", len(testImages))
	if err := pushTestImagesToRegistry(ctx, testImages, registryPort, logger); err != nil {
		logger.Warnf("⚠️  Failed to pre-load test images (you can push them manually): %v", err)
		// Don't fail - user can push images manually if needed
	} else {
		logger.Info("✅ Test images successfully pre-loaded to registry")
	}

	// Write kubeconfig to KUBECONFIG env var or default location
	kubeconfigPath, err := writeKubeconfig(ctx, clusterName, logger)
	if err != nil {
		logger.Errorf("Failed to write kubeconfig: %v", err)
		logger.Info("Running cleanup...")
		cleanup()
		os.Exit(1)
	}

	// Success message
	logger.Info("✅ K3D cluster successfully created!")
	logger.Infof("Cluster name: %s", clusterName)
	logger.Infof("API server: https://localhost:%s", hostPort)
	if enableRegistry {
		logger.Infof("Docker registry: localhost:%s", registryPort)
	}
	logger.Infof("Kubeconfig written to: %s", kubeconfigPath)

	// Print kubectl config instructions
	fmt.Println("\nTo use this cluster:")
	if kubeconfigPath != getDefaultKubeconfigPath() {
		fmt.Printf("  export KUBECONFIG=%s\n", kubeconfigPath)
	}
	fmt.Printf("  kubectl cluster-info\n\n")

	// Print teardown instructions
	fmt.Println("To tear down the cluster:")
	fmt.Printf("  k3d cluster delete %s\n\n", clusterName)

	// If running interactively, wait for signal
	if isInteractive() {
		fmt.Println("Press Ctrl+C to tear down the cluster...")
		<-ctx.Done()

		logger.Info("Tearing down cluster...")
		cleanup()
		logger.Info("✅ Cluster teardown complete")
	} else {
		logger.Info("Cluster is ready. Run 'k3d cluster delete " + clusterName + "' to tear it down.")
	}
}

func getLogLevel() utils.LogLevel {
	if quiet {
		return utils.ErrorLevel
	}
	if verbose {
		return utils.DebugLevel
	}
	return utils.InfoLevel
}

func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printHelp(fs *pflag.FlagSet) {
	fmt.Println("setup-cluster - Create a complete K3D cluster with Grove operator")
	fmt.Println("\nUsage:")
	fmt.Println("  setup-cluster [flags]")
	fmt.Println("\nDescription:")
	fmt.Println("  This command creates a complete K3D cluster with Grove operator, Kai scheduler,")
	fmt.Println("  and optionally the NVIDIA GPU operator. It handles all setup steps including:")
	fmt.Println("  - Creating the K3D cluster")
	fmt.Println("  - Setting up a Docker registry")
	fmt.Println("  - Pre-pulling and caching images")
	fmt.Println("  - Deploying Grove operator")
	fmt.Println("  - Installing Kai scheduler")
	fmt.Println("  - Optionally installing NVIDIA GPU operator")
	fmt.Println("\nExamples:")
	fmt.Println("  # Create a basic cluster with defaults")
	fmt.Println("  setup-cluster")
	fmt.Println()
	fmt.Println("  # Create a cluster with 3 worker nodes")
	fmt.Println("  setup-cluster --worker-nodes 3")
	fmt.Println()
	fmt.Println("  # Create a cluster without GPU operator")
	fmt.Println("  setup-cluster --skip-gpu-operator")
	fmt.Println()
	fmt.Println("  # Create a cluster with custom name and ports")
	fmt.Println("  setup-cluster --name my-cluster --api-port 6551 --registry-port 5002")
	fmt.Println("\nFlags:")
	fs.PrintDefaults()
}

func printConfiguration(cfg *setup.ClusterConfig, logger *utils.Logger) {
	logger.Info("Cluster Configuration:")
	logger.Infof("  Name: %s", cfg.Name)
	logger.Infof("  Control Plane Nodes: %d", cfg.ControlPlaneNodes)
	logger.Infof("  Worker Nodes: %d", cfg.WorkerNodes)
	logger.Infof("  K3s Image: %s", cfg.Image)
	logger.Infof("  API Port: %s", cfg.HostPort)
	logger.Infof("  Load Balancer Port: %s", cfg.LoadBalancerPort)
	logger.Infof("  Worker Memory: %s", cfg.WorkerMemory)
	if cfg.EnableRegistry {
		logger.Infof("  Registry: Enabled (port %s)", cfg.RegistryPort)
	} else {
		logger.Info("  Registry: Disabled")
	}
	logger.Info("")
	logger.Info("Components to deploy:")
	logger.Infof("  Grove Operator: %s", boolToStatus(!skipGrove))
	logger.Infof("  Kai Scheduler: %s", boolToStatus(!skipKaiScheduler))
	logger.Infof("  GPU Operator: %s", boolToStatus(!skipGPUOperator))
	logger.Info("")
}

func boolToStatus(enabled bool) string {
	if enabled {
		return "✓ Yes"
	}
	return "✗ No"
}

// findSkaffoldYAML walks up the directory tree looking for skaffold.yaml
// It checks for common project root indicators (.git, go.mod) along the way
func findSkaffoldYAML(startDir string) string {
	dir := startDir

	// Walk up to filesystem root
	for {
		// Check if skaffold.yaml exists in current directory
		skaffoldPath := filepath.Join(dir, "skaffold.yaml")
		if _, err := os.Stat(skaffoldPath); err == nil {
			return skaffoldPath
		}

		// Check if we're at a project root (has .git or go.mod)
		// If so, this is likely the right place even if skaffold.yaml is missing
		gitDir := filepath.Join(dir, ".git")
		goMod := filepath.Join(dir, "go.mod")

		if _, err := os.Stat(gitDir); err == nil {
			// Found .git directory, check one more time for skaffold.yaml
			skaffoldPath := filepath.Join(dir, "skaffold.yaml")
			if _, err := os.Stat(skaffoldPath); err == nil {
				return skaffoldPath
			}
			// At repo root but no skaffold.yaml
			return ""
		}

		if _, err := os.Stat(goMod); err == nil {
			// Found go.mod, check for skaffold.yaml
			skaffoldPath := filepath.Join(dir, "skaffold.yaml")
			if _, err := os.Stat(skaffoldPath); err == nil {
				return skaffoldPath
			}
		}

		// Move up one directory
		parent := filepath.Dir(dir)

		// Check if we've reached the filesystem root
		if parent == dir {
			return ""
		}

		dir = parent
	}
}

// getDefaultKubeconfigPath returns the default kubeconfig path
func getDefaultKubeconfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}

// getKubeconfigPath returns the path where kubeconfig should be written
// Priority: KUBECONFIG env var > default location (~/.kube/config)
func getKubeconfigPath() string {
	// Check KUBECONFIG environment variable
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		return kubeconfigEnv
	}

	// Fall back to default location
	return getDefaultKubeconfigPath()
}

// writeKubeconfig writes the kubeconfig for the cluster to the appropriate location
func writeKubeconfig(ctx context.Context, clusterName string, logger *utils.Logger) (string, error) {
	logger.Debug("📄 Fetching kubeconfig from k3d cluster...")

	// Get the cluster
	cluster, err := k3dclient.ClusterGet(ctx, runtimes.Docker, &k3d.Cluster{Name: clusterName})
	if err != nil {
		return "", fmt.Errorf("could not get cluster: %w", err)
	}

	// Get kubeconfig from k3d
	kubeconfig, err := k3dclient.KubeconfigGet(ctx, runtimes.Docker, cluster)
	if err != nil {
		return "", fmt.Errorf("failed to get kubeconfig from k3d: %w", err)
	}

	// Determine target path
	targetPath := getKubeconfigPath()
	if targetPath == "" {
		return "", fmt.Errorf("could not determine kubeconfig path")
	}

	// Ensure directory exists
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create kubeconfig directory: %w", err)
	}

	// Check if kubeconfig file already exists
	var existingConfig *clientcmdapi.Config
	if _, err := os.Stat(targetPath); err == nil {
		// File exists, load it
		logger.Debugf("Loading existing kubeconfig from %s", targetPath)
		existingConfig, err = clientcmd.LoadFromFile(targetPath)
		if err != nil {
			logger.Warnf("Failed to load existing kubeconfig, will overwrite: %v", err)
			existingConfig = nil
		}
	}

	// Merge or use new config
	var finalConfig *clientcmdapi.Config
	if existingConfig != nil {
		// Merge the new cluster config into existing
		logger.Debug("Merging new cluster config with existing kubeconfig")
		finalConfig = mergeKubeconfigs(existingConfig, kubeconfig, clusterName)
	} else {
		finalConfig = kubeconfig
	}

	// Write the kubeconfig
	if err := clientcmd.WriteToFile(*finalConfig, targetPath); err != nil {
		return "", fmt.Errorf("failed to write kubeconfig to %s: %w", targetPath, err)
	}

	logger.Debugf("✓ Kubeconfig written to %s", targetPath)
	return targetPath, nil
}

// mergeKubeconfigs merges a new kubeconfig into an existing one
func mergeKubeconfigs(existing, new *clientcmdapi.Config, _ string) *clientcmdapi.Config {
	merged := existing.DeepCopy()

	// Add/update cluster
	for name, cluster := range new.Clusters {
		merged.Clusters[name] = cluster
	}

	// Add/update auth info
	for name, authInfo := range new.AuthInfos {
		merged.AuthInfos[name] = authInfo
	}

	// Add/update context
	for name, context := range new.Contexts {
		merged.Contexts[name] = context
		// Set the new cluster as current context
		merged.CurrentContext = name
	}

	return merged
}

// pushTestImagesToRegistry pulls test images and pushes them to the local registry
// This matches what the e2e test SharedClusterManager does
func pushTestImagesToRegistry(ctx context.Context, images []string, registryPort string, logger *utils.Logger) error {
	if len(images) == 0 {
		return nil
	}

	// Create Docker client
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()

	// Process each image
	for _, imageName := range images {
		registryImage := fmt.Sprintf("localhost:%s/%s", registryPort, imageName)

		logger.Debugf("  🔄 Pulling image: %s", imageName)

		// Step 1: Pull the image
		pullReader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
		if err != nil {
			return fmt.Errorf("failed to pull %s: %w", imageName, err)
		}

		// Consume the pull output to avoid blocking
		if logger.GetLevel() == utils.DebugLevel {
			_, err = io.Copy(logger.WriterLevel(utils.DebugLevel), pullReader)
		} else {
			_, err = io.Copy(io.Discard, pullReader)
		}
		pullReader.Close()
		if err != nil {
			return fmt.Errorf("failed to read pull output for %s: %w", imageName, err)
		}

		// Step 2: Tag the image for the local registry
		logger.Debugf("  🏷️  Tagging as: %s", registryImage)
		err = cli.ImageTag(ctx, imageName, registryImage)
		if err != nil {
			return fmt.Errorf("failed to tag image %s as %s: %w", imageName, registryImage, err)
		}

		// Step 3: Push the image to the local registry
		logger.Debugf("  ⬆️  Pushing to registry: %s", registryImage)
		pushReader, err := cli.ImagePush(ctx, registryImage, image.PushOptions{})
		if err != nil {
			return fmt.Errorf("failed to push %s: %w", registryImage, err)
		}

		// Consume the push output to avoid blocking
		if logger.GetLevel() == utils.DebugLevel {
			_, err = io.Copy(logger.WriterLevel(utils.DebugLevel), pushReader)
		} else {
			_, err = io.Copy(io.Discard, pushReader)
		}
		pushReader.Close()
		if err != nil {
			return fmt.Errorf("failed to read push output for %s: %w", registryImage, err)
		}

		logger.Debugf("  ✅ Successfully pushed: %s", registryImage)
	}

	return nil
}
