package utils

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/config"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	k3dlogger "github.com/k3d-io/k3d/v5/pkg/logger"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	k3d "github.com/k3d-io/k3d/v5/pkg/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

// NodeTaint represents a Kubernetes node taint
type NodeTaint struct {
	Key    string
	Value  string
	Effect string
}

// ClusterConfig holds configuration for creating a k3d cluster
type ClusterConfig struct {
	Name             string
	Servers          int
	Agents           int
	Image            string
	HostPort         string
	LoadBalancerPort string
	AgentNodeLabels  map[string]string
	AgentNodeTaints  []NodeTaint // Taints to apply to agent nodes
	WorkerMemory     string      // Memory allocation for worker/agent nodes (e.g., "150m")
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
		WorkerMemory:     "150m",
	}
}

// SetupK3DCluster creates a k3d cluster and returns a kubernetes clientset and REST config
func SetupK3DCluster(ctx context.Context, cfg ClusterConfig, logger *CILogger) (*kubernetes.Clientset, *rest.Config, *v1alpha5.ClusterConfig, func(), error) {
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
				AgentsMemory: cfg.WorkerMemory,
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

	// Apply agent node taints if specified
	for _, taint := range cfg.AgentNodeTaints {
		clusterConfig.Options.K3sOptions.ExtraArgs = append(
			clusterConfig.Options.K3sOptions.ExtraArgs,
			v1alpha5.K3sArgWithNodeFilters{
				Arg:         fmt.Sprintf("--node-taint=%s=%s:%s", taint.Key, taint.Value, taint.Effect),
				NodeFilters: []string{"agent:*"},
			},
		)
	}

	// Transform configuration
	k3dConfig, err := config.TransformSimpleToClusterConfig(ctx, runtimes.Docker, clusterConfig, "")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to transform config: %w", err)
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
		return nil, nil, nil, cleanup, fmt.Errorf("failed to create cluster: %w", err)
	}
	logger.Info("✅ Cluster created successfully!")

	// Get kubeconfig
	logger.Info("📄 Fetching kubeconfig...")
	cluster, err := client.ClusterGet(ctx, runtimes.Docker, &k3dConfig.Cluster)
	if err != nil {
		return nil, nil, nil, cleanup, fmt.Errorf("could not get cluster: %w", err)
	}

	kubeconfig, err := client.KubeconfigGet(ctx, runtimes.Docker, cluster)
	if err != nil {
		return nil, nil, nil, cleanup, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	kubeconfigBytes, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return nil, nil, nil, cleanup, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	// Create kubernetes clientset
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, nil, nil, cleanup, fmt.Errorf("could not create rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, cleanup, fmt.Errorf("could not create clientset: %w", err)
	}

	return clientset, restConfig, k3dConfig, cleanup, nil
}

// KindClusterConfig holds configuration for creating a kind cluster
type KindClusterConfig struct {
	Name             string
	ControlPlanes    int
	Workers          int
	Image            string
	WorkerNodeLabels map[string]string // Labels to apply to worker nodes
	WorkerNodeTaints []NodeTaint       // Taints to apply to worker nodes
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

// SetupKindCluster creates a kind cluster and returns a kubernetes clientset and REST config
// Note that kind clsuters don't support total memory limits unlike k3d, this is here incase
// we still want to use Kind for some reason
func SetupKindCluster(_ context.Context, cfg KindClusterConfig, logger *CILogger) (*kubernetes.Clientset, *rest.Config, func(), error) {
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

		// Build node labels string for kubelet
		var allLabels []string

		// Add custom worker node labels, if provided
		if len(cfg.WorkerNodeLabels) > 0 {
			for k, v := range cfg.WorkerNodeLabels {
				allLabels = append(allLabels, fmt.Sprintf("%s=%s", k, v))
			}
		}

		// Apply labels and taints via kubeadmConfigPatches if specified
		if len(allLabels) > 0 || len(cfg.WorkerNodeTaints) > 0 {
			var configParts []string

			// Build kubeletExtraArgs section if labels are specified
			if len(allLabels) > 0 {
				nodeLabelsStr := strings.Join(allLabels, ",")
				configParts = append(configParts, fmt.Sprintf(`  kubeletExtraArgs:
    node-labels: "%s"`, nodeLabelsStr))
			}

			// Build taints section if taints are specified
			if len(cfg.WorkerNodeTaints) > 0 {
				taintLines := []string{"  taints:"}
				for _, taint := range cfg.WorkerNodeTaints {
					taintLines = append(taintLines, fmt.Sprintf(`  - key: "%s"
    value: "%s"
    effect: "%s"`, taint.Key, taint.Value, taint.Effect))
				}
				configParts = append(configParts, strings.Join(taintLines, "\n"))
			}

			configPatch := fmt.Sprintf(`kind: JoinConfiguration
nodeRegistration:
%s`, strings.Join(configParts, "\n"))

			node.KubeadmConfigPatches = []string{configPatch}
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
		return nil, nil, cleanup, fmt.Errorf("failed to create kind cluster: %w", err)
	}
	logger.Info("✅ Kind cluster created successfully!")

	// Get kubeconfig
	logger.Info("📄 Fetching kubeconfig...")
	// ... (the rest of your function remains the same) ...
	kubeConfigYaml, err := provider.KubeConfig(cfg.Name, false)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	// Create kubernetes clientset
	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfigYaml))
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("could not create rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("could not create clientset: %w", err)
	}

	return clientset, restConfig, cleanup, nil
}

func InstallCoreComponents(logger *CILogger, groveConfig *GroveInstallConfig, kaiConfig *KaiInstallConfig, nvidiaConfig *NvidiaOperatorInstallConfig) error {
	var wg sync.WaitGroup
	errChan := make(chan error, 3) // Buffer for up to 3 errors

	// Install Grove
	if groveConfig != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("🚀 Starting Grove installation...")
			_, err := InstallGrove(groveConfig, logger)
			if err != nil {
				logger.Errorf("❌ Grove installation failed: %v", err)
				errChan <- fmt.Errorf("Grove installation failed: %w", err)
			} else {
				logger.Info("✅ Grove installation completed successfully")
			}
		}()
	}

	// Install Kai Scheduler
	if kaiConfig != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("🚀 Starting Kai Scheduler installation...")
			_, err := InstallKai(kaiConfig, logger)
			if err != nil {
				logger.Errorf("❌ Kai Scheduler installation failed: %v", err)
				errChan <- fmt.Errorf("Kai Scheduler installation failed: %w", err)
			} else {
				logger.Info("✅ Kai Scheduler installation completed successfully")
			}
		}()
	}

	// Install NVIDIA GPU Operator
	if nvidiaConfig != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("🚀 Starting NVIDIA GPU Operator installation...")
			_, err := InstallNvidiaOperator(nvidiaConfig, logger)
			if err != nil {
				logger.Errorf("❌ NVIDIA GPU Operator installation failed: %v", err)
				errChan <- fmt.Errorf("NVIDIA GPU Operator installation failed: %w", err)
			} else {
				logger.Info("✅ NVIDIA GPU Operator installation completed successfully")
			}
		}()
	}

	// Wait for all installations to complete
	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		return err // Return the first error encountered
	}

	logger.Info("✅ All component installations completed successfully")
	return nil
}

func SetupCompleteK3DCluster(ctx context.Context, cfg ClusterConfig, logger *CILogger) (*kubernetes.Clientset, *rest.Config, *v1alpha5.ClusterConfig, func(), error) {
	clientset, restConfig, k3dConfig, cleanup, err := SetupK3DCluster(ctx, cfg, logger)
	if err != nil {
		return nil, nil, nil, cleanup, err
	}

	namespace := "grove-system"
	if err := ensureNamespace(ctx, clientset, namespace); err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}

	tolerations := []map[string]interface{}{
		{
			"key":      "node-role.kubernetes.io/control-plane",
			"operator": "Exists",
			"effect":   "NoSchedule",
		},
		{
			"key":      "node_role.e2e.grove.nvidia.com",
			"operator": "Equal",
			"value":    "agent",
			"effect":   "NoSchedule",
		},
	}

	groveConfig := GroveInstallConfigV0_1_0_Alpha1()
	groveConfig.ReleaseName = "grove"
	groveConfig.Namespace = namespace
	groveConfig.RestConfig = restConfig
	if groveConfig.Values == nil {
		groveConfig.Values = make(map[string]interface{})
	}
	groveConfig.Values["tolerations"] = tolerations

	kaiConfig := KaiInstallConfigLatest("v0.9.3")
	kaiConfig.ReleaseName = "kai-scheduler"
	kaiConfig.RestConfig = restConfig
	if kaiConfig.Values == nil {
		kaiConfig.Values = make(map[string]interface{})
	}
	kaiConfig.Values["global"] = map[string]interface{}{
		"tolerations": tolerations,
	}

	nvidiaConfig := NvidiaOperatorInstallConfigLatest("v25.3.4")
	nvidiaConfig.ReleaseName = "nvidia-gpu-operator"
	nvidiaConfig.GenerateName = false
	nvidiaConfig.RestConfig = restConfig
	if nvidiaConfig.Values == nil {
		nvidiaConfig.Values = make(map[string]interface{})
	}
	nvidiaConfig.Values["tolerations"] = tolerations
	nvidiaConfig.Values["driver"] = map[string]interface{}{"enabled": false}
	nvidiaConfig.Values["toolkit"] = map[string]interface{}{"enabled": false}
	nvidiaConfig.Values["devicePlugin"] = map[string]interface{}{"enabled": false}
	nvidiaConfig.Values["dcgmExporter"] = map[string]interface{}{"enabled": false}
	nvidiaConfig.Values["gfd"] = map[string]interface{}{"enabled": false}
	nvidiaConfig.Values["migManager"] = map[string]interface{}{"enabled": false}
	nvidiaConfig.Values["nodeStatusExporter"] = map[string]interface{}{"enabled": false}

	logger.Info("🚀 Installing Grove, Kai Scheduler, and NVIDIA GPU Operator...")
	if err := InstallCoreComponents(logger, groveConfig, kaiConfig, nvidiaConfig); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("component installation failed: %w", err)
	}

	logger.Info("⏳ Waiting for Grove pods to be ready...")
	if err := WaitForGrovePodsReady(ctx, namespace, restConfig, logger); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("Grove pods not ready: %w", err)
	}

	logger.Info("⏳ Waiting for Kai Scheduler pods to be ready...")
	if err := WaitForKaiPodsReady(ctx, restConfig, logger); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("Kai Scheduler pods not ready: %w", err)
	}

	logger.Info("⏳ Waiting for Kai CRDs to be ready...")
	if err := WaitForKaiCRDs(ctx, restConfig, logger); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("Failed to wait for Kai CRDs: %w", err)
	}

	logger.Info("📄 Creating default Kai queues...")
	if err := CreateDefaultKaiQueues(ctx, restConfig, logger); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("Failed to create default Kai queue: %w", err)
	}

	logger.Info("⏳ Waiting for NVIDIA GPU Operator to be ready...")
	if err := WaitForNvidiaOperatorReady(ctx, restConfig, logger); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("NVIDIA GPU Operator not ready: %w", err)
	}

	logger.Info("🎉 Complete k3d cluster setup successful!")
	return clientset, restConfig, k3dConfig, cleanup, nil
}

func ensureNamespace(ctx context.Context, clientset kubernetes.Interface, namespace string) error {
	_, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if errors.IsNotFound(err) {
		_, createErr := clientset.CoreV1().Namespaces().Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{})
		if createErr != nil {
			return fmt.Errorf("failed to create namespace %s: %w", namespace, createErr)
		}
		return nil
	}
	return fmt.Errorf("failed to get namespace %s: %w", namespace, err)
}
