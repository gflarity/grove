package utils

// Example usage:
//
// Basic installation:
//   config := NvidiaOperatorInstallConfigLatest("v25.3.4")
//   logger := NewCILogger(nil)
//   release, err := InstallNvidiaOperator(config, logger)
//
// Custom installation:
//   config := &NvidiaOperatorInstallConfig{
//       ReleaseName:  "my-nvidia-operator",
//       ChartRef:     "nvidia/gpu-operator",
//       ChartVersion: "v25.3.4",
//       Namespace:    "gpu-operator",
//       Values: map[string]interface{}{
//           "driver": map[string]interface{}{
//               "enabled": true,
//           },
//       },
//   }
//   logger := NewCILogger(nil)
//   release, err := InstallOrUpgradeNvidiaOperator(config, logger)

import (
	"context"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/rest"
)

// NvidiaOperatorInstallConfig holds configuration for installing NVIDIA GPU Operator
type NvidiaOperatorInstallConfig struct {
	// ReleaseName is the name of the Helm release (default: auto-generated)
	ReleaseName string
	// ChartRef is the chart reference (default: "nvidia/gpu-operator")
	ChartRef string
	// ChartVersion is the version of the chart to install
	ChartVersion string
	// Namespace is the Kubernetes namespace to install into (default: "gpu-operator")
	Namespace string
	// Values is a map of custom values to pass to the chart
	Values map[string]interface{}
	// Logger is the logger to use for output (default: uses CILogger)
	Logger func(format string, v ...interface{})
	// RestConfig is the Kubernetes REST config to use (optional, defaults to system kubeconfig)
	RestConfig *rest.Config
	// Wait enables waiting for the installation to complete (default: true)
	Wait bool
	// GenerateName enables auto-generation of release name (default: true)
	GenerateName bool
}

// ToHelmInstallConfig converts NvidiaOperatorInstallConfig to HelmInstallConfig
func (c *NvidiaOperatorInstallConfig) ToHelmInstallConfig() *HelmInstallConfig {
	return &HelmInstallConfig{
		ReleaseName:     c.ReleaseName,
		ChartRef:        c.ChartRef,
		ChartVersion:    c.ChartVersion,
		Namespace:       c.Namespace,
		CreateNamespace: true, // NVIDIA operator installation creates namespace by default
		Values:          c.Values,
		Logger:          c.Logger,
		RestConfig:      c.RestConfig,
	}
}

// Validate validates required fields and normalizes optional ones.
func (c *NvidiaOperatorInstallConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Set defaults
	if c.ChartRef == "" {
		c.ChartRef = "nvidia/gpu-operator"
	}
	if c.Namespace == "" {
		c.Namespace = "gpu-operator"
	}

	// ChartVersion is required and has no default
	if c.ChartVersion == "" {
		return fmt.Errorf("chart version is required")
	}

	if c.Values == nil {
		c.Values = make(map[string]interface{})
	}
	if c.Logger == nil {
		// Create a default logger that writes to stdout
		defaultLogger := NewCILogger(nil)
		c.Logger = defaultLogger.Printf
	}
	return nil
}

// NvidiaOperatorInstallConfigLatest returns a configuration for NVIDIA GPU Operator installation with latest defaults
// Note: You must specify the version as it's required
func NvidiaOperatorInstallConfigLatest(version string) *NvidiaOperatorInstallConfig {
	defaultLogger := NewCILogger(nil)
	return &NvidiaOperatorInstallConfig{
		ReleaseName:  "", // Will be auto-generated
		ChartRef:     "nvidia/gpu-operator",
		ChartVersion: version,
		Namespace:    "gpu-operator",
		Values:       make(map[string]interface{}),
		Logger:       defaultLogger.Printf,
		Wait:         false, // Disable wait for test environments without GPUs
		GenerateName: true,
	}
}

// InstallNvidiaOperator installs NVIDIA GPU Operator on a Kubernetes cluster using Helm
// It returns the installed release and any error that occurred
func InstallNvidiaOperator(config *NvidiaOperatorInstallConfig, logger *CILogger) (*release.Release, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Override the logger in config with the passed logger
	config.Logger = logger.Printf

	// Convert to HelmInstallConfig
	helmConfig := config.ToHelmInstallConfig()

	config.Logger("Setting up Helm and Kubernetes configuration...")

	// Set up Helm action configuration
	actionConfig, _, err := setupHelmAction(helmConfig)
	if err != nil {
		return nil, err
	}

	config.Logger("Locating and pulling chart %s version %s...", config.ChartRef, config.ChartVersion)

	// Create a new Install action client
	installClient := action.NewInstall(actionConfig)
	installClient.Namespace = config.Namespace
	installClient.Version = config.ChartVersion
	installClient.CreateNamespace = true             // Equivalent to --create-namespace
	installClient.Wait = config.Wait                 // Equivalent to --wait
	installClient.GenerateName = config.GenerateName // Equivalent to --generate-name

	// Only set ReleaseName if not generating name
	if !config.GenerateName && config.ReleaseName != "" {
		installClient.ReleaseName = config.ReleaseName
	}

	// Set up chart path options for locating the chart
	settings := cli.New()
	installClient.ChartPathOptions.Version = config.ChartVersion
	chartPath, err := installClient.ChartPathOptions.LocateChart(config.ChartRef, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart: %w", err)
	}
	config.Logger("Chart located at: %s", chartPath)

	// Load the chart from the located path
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	if config.GenerateName {
		config.Logger("Installing release with auto-generated name...")
	} else {
		config.Logger("Installing release: %s", config.ReleaseName)
	}

	// Perform the installation
	rel, err := installClient.Run(chart, config.Values)
	if err != nil {
		return nil, fmt.Errorf("helm install failed: %w", err)
	}

	config.Logger("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}

// InstallOrUpgradeNvidiaOperator installs NVIDIA GPU Operator if it doesn't exist, or upgrades it if it does
// It returns the installed/upgraded release and any error that occurred
func InstallOrUpgradeNvidiaOperator(config *NvidiaOperatorInstallConfig, logger *CILogger) (*release.Release, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Override the logger in config with the passed logger
	config.Logger = logger.Printf

	// Convert to HelmInstallConfig
	helmConfig := config.ToHelmInstallConfig()

	config.Logger("Setting up Helm and Kubernetes configuration...")

	// Set up Helm action configuration
	actionConfig, _, err := setupHelmAction(helmConfig)
	if err != nil {
		return nil, err
	}

	config.Logger("Locating and pulling chart %s version %s...", config.ChartRef, config.ChartVersion)

	// Create upgrade client first to locate the chart
	upgradeClient := action.NewUpgrade(actionConfig)
	upgradeClient.ChartPathOptions.Version = config.ChartVersion
	upgradeClient.Wait = config.Wait
	settings := cli.New()
	chartPath, err := upgradeClient.ChartPathOptions.LocateChart(config.ChartRef, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart: %w", err)
	}
	config.Logger("Chart located at: %s", chartPath)

	// Load the chart from the located path
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// For upgrade, we need a specific release name, not auto-generated
	releaseName := config.ReleaseName
	if releaseName == "" {
		releaseName = "nvidia-gpu-operator" // Default name for upgrades
	}

	// Try upgrade first
	config.Logger("Attempting to upgrade release: %s", releaseName)
	upgradeClient.Install = false
	if rel, err := upgradeClient.Run(releaseName, chart, config.Values); err == nil {
		config.Logger("Success! Release '%s' upgraded in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
		return rel, nil
	}

	// If upgrade failed, try install
	config.Logger("Upgrade failed, attempting fresh install of release: %s", releaseName)
	installClient := action.NewInstall(actionConfig)
	installClient.Namespace = config.Namespace
	installClient.ReleaseName = releaseName
	installClient.Version = config.ChartVersion
	installClient.CreateNamespace = true // Equivalent to --create-namespace
	installClient.Wait = config.Wait     // Equivalent to --wait

	rel, err := installClient.Run(chart, config.Values)
	if err != nil {
		return nil, fmt.Errorf("both upgrade and install failed: %w", err)
	}

	config.Logger("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}

// WaitForNvidiaOperatorPodsReady waits for NVIDIA GPU Operator pods to be ready
func WaitForNvidiaOperatorPodsReady(ctx context.Context, restConfig *rest.Config, logger *CILogger) error {
	logger.Info("⏳ Waiting for NVIDIA GPU Operator pods to be ready...")

	// NVIDIA GPU operator is installed in gpu-operator namespace by default
	// Use shorter timeout for test environments with disabled components
	err := WaitForPodsInNamespace(ctx, "gpu-operator", restConfig, 5*time.Minute, logger)
	if err != nil {
		return fmt.Errorf("failed waiting for NVIDIA GPU Operator pods: %w", err)
	}

	logger.Info("✅ NVIDIA GPU Operator pods are ready!")
	return nil
}

// WaitForNvidiaOperatorReady waits for the NVIDIA GPU Operator to be fully ready
// In test environments without GPUs, this simply waits for the operator pods to be ready
func WaitForNvidiaOperatorReady(ctx context.Context, restConfig *rest.Config, logger *CILogger) error {
	logger.Info("⏳ Waiting for NVIDIA GPU Operator to be ready...")

	// For test environments, just wait for the operator pods to be ready
	// The WaitForNvidiaOperatorPodsReady function already does comprehensive pod readiness checking
	if err := WaitForNvidiaOperatorPodsReady(ctx, restConfig, logger); err != nil {
		return err
	}

	logger.Info("✅ NVIDIA GPU Operator is ready!")
	return nil
}
