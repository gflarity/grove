package utils

import (
	"context"
	"fmt"

	"github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/config"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	k3dlogger "github.com/k3d-io/k3d/v5/pkg/logger"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	k3d "github.com/k3d-io/k3d/v5/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

// ClusterConfig holds configuration for creating a k3d cluster
type ClusterConfig struct {
	Name             string
	Servers          int
	Agents           int
	Image            string
	HostPort         string
	LoadBalancerPort string
	AgentNodeLabels  map[string]string
}

// DefaultClusterConfig returns a sensible default cluster configuration
func DefaultClusterConfig() ClusterConfig {
	return ClusterConfig{
		Name:             "test-k3d-cluster",
		Servers:          1,
		Agents:           2,
		Image:            "rancher/k3s:v1.28.8-k3s1",
		HostPort:         "6550",
		LoadBalancerPort: "8080:80",
	}
}

// SetupK3DCluster creates a k3d cluster and returns a kubernetes clientset
func SetupK3DCluster(ctx context.Context, cfg ClusterConfig, logger *CILogger) (*kubernetes.Clientset, *v1alpha5.ClusterConfig, func(), error) {
	logger.Infof("📝 Preparing k3d cluster configuration for '%s'...", cfg.Name)

	// Route k3d internal logs to our logger writer
	k3dlogger.Log().SetOutput(logger.Writer())

	// Create cluster configuration
	clusterConfig := v1alpha5.SimpleConfig{
		ObjectMeta: types.ObjectMeta{
			Name: cfg.Name,
		},
		Servers: cfg.Servers,
		Agents:  cfg.Agents,
		Image:   cfg.Image,
		ExposeAPI: v1alpha5.SimpleExposureOpts{
			Host:     "0.0.0.0",
			HostPort: cfg.HostPort,
		},
		Ports: []v1alpha5.PortWithNodeFilters{
			{
				Port:        cfg.LoadBalancerPort,
				NodeFilters: []string{"loadbalancer"},
			},
		},
		Options: v1alpha5.SimpleConfigOptions{
			Runtime: v1alpha5.SimpleConfigOptionsRuntime{
				AgentsMemory: "150m",
			},
		},
	}

	// Apply Kubernetes node labels to all agent nodes, if provided
	if len(cfg.AgentNodeLabels) > 0 {
		for k, v := range cfg.AgentNodeLabels {
			clusterConfig.Options.K3sOptions.NodeLabels = append(
				clusterConfig.Options.K3sOptions.NodeLabels,
				v1alpha5.LabelWithNodeFilters{
					Label:       fmt.Sprintf("%s=%s", k, v),
					NodeFilters: []string{"agent:*"},
				},
			)
		}
	}

	// Transform configuration
	k3dConfig, err := config.TransformSimpleToClusterConfig(ctx, runtimes.Docker, clusterConfig, "")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to transform config: %w", err)
	}

	// this is the cleanup funciton, we always return it now so the caller can decide to use it or not
	cleanup := func() {
		logger.Info("🗑️ Deleting cluster...")
		if err := client.ClusterDelete(ctx, runtimes.Docker, &k3dConfig.Cluster, k3d.ClusterDeleteOpts{}); err != nil {
			logger.Errorf("Failed to delete cluster: %v", err)
		} else {
			logger.Info("✅ Cluster deleted successfully")
		}
	}

	// Create cluster
	logger.Infof("🚀 Creating cluster '%s' with %d server(s) and %d agent(s)...",
		k3dConfig.Name, cfg.Servers, cfg.Agents)

	if err := client.ClusterRun(ctx, runtimes.Docker, k3dConfig); err != nil {
		return nil, nil, cleanup, fmt.Errorf("failed to create cluster: %w", err)
	}
	logger.Info("✅ Cluster created successfully!")

	// Get kubeconfig
	logger.Info("📄 Fetching kubeconfig...")
	cluster, err := client.ClusterGet(ctx, runtimes.Docker, &k3dConfig.Cluster)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("could not get cluster: %w", err)
	}

	kubeconfig, err := client.KubeconfigGet(ctx, runtimes.Docker, cluster)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	kubeconfigBytes, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	// Create kubernetes clientset
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("could not create rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("could not create clientset: %w", err)
	}

	return clientset, k3dConfig, cleanup, nil
}

// KindClusterConfig holds configuration for creating a kind cluster
type KindClusterConfig struct {
	Name          string
	ControlPlanes int
	Workers       int
	Image         string
}

// DefaultKindClusterConfig returns a sensible default kind cluster configuration
func DefaultKindClusterConfig() KindClusterConfig {
	return KindClusterConfig{
		Name:          "test-kind-cluster",
		ControlPlanes: 1,
		Workers:       2,
		Image:         "", // Empty means use kind's default
	}
}

// SetupKindCluster creates a kind cluster and returns a kubernetes clientset
func SetupKindCluster(_ context.Context, cfg KindClusterConfig, logger *CILogger) (*kubernetes.Clientset, func(), error) {
	logger.Infof("📝 Preparing kind cluster configuration for '%s'...", cfg.Name)

	// Provide our unified CILogger to kind with verbosity filtering
	// This will suppress verbose node logs but keep kind's main status updates
	provider := cluster.NewProvider(cluster.ProviderWithLogger(logger))

	// Create cluster configuration
	kindConfig := &v1alpha4.Cluster{
		Nodes: []v1alpha4.Node{},
	}

	// ... (rest of your config setup is correct) ...
	// Add control plane nodes
	for i := 0; i < cfg.ControlPlanes; i++ {
		node := v1alpha4.Node{
			Role: v1alpha4.ControlPlaneRole,
		}
		if cfg.Image != "" {
			node.Image = cfg.Image
		}
		kindConfig.Nodes = append(kindConfig.Nodes, node)
	}

	// Add worker nodes
	for i := 0; i < cfg.Workers; i++ {
		node := v1alpha4.Node{
			Role: v1alpha4.WorkerRole,
		}
		if cfg.Image != "" {
			node.Image = cfg.Image
		}
		kindConfig.Nodes = append(kindConfig.Nodes, node)
	}

	// Create cluster
	logger.Infof("🚀 Creating kind cluster '%s' with %d control-plane(s) and %d worker(s)...",
		cfg.Name, cfg.ControlPlanes, cfg.Workers)

	// this is the cleanup funciton, we always return it now so the caller can decide to use it or not
	cleanup := func() {
		logger.Info("🗑️ Deleting kind cluster...")
		if err := provider.Delete(cfg.Name, ""); err != nil {
			logger.Errorf("Failed to delete kind cluster: %v", err)
		} else {
			logger.Info("✅ Kind cluster deleted successfully")
		}
	}

	// Create cluster
	if err := provider.Create(
		cfg.Name,
		cluster.CreateWithV1Alpha4Config(kindConfig),
	); err != nil {
		return nil, cleanup, fmt.Errorf("failed to create kind cluster: %w", err)
	}
	logger.Info("✅ Kind cluster created successfully!")

	// Get kubeconfig
	logger.Info("📄 Fetching kubeconfig...")
	// ... (the rest of your function remains the same) ...
	kubeConfigYaml, err := provider.KubeConfig(cfg.Name, false)
	if err != nil {
		return nil, cleanup, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	// Create kubernetes clientset
	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfigYaml))
	if err != nil {
		return nil, cleanup, fmt.Errorf("could not create rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, cleanup, fmt.Errorf("could not create clientset: %w", err)
	}

	return clientset, cleanup, nil
}
