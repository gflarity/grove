package utils

import (
	"fmt"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
)

// GroveInstallConfig holds configuration for installing Grove
type GroveInstallConfig struct {
	// ReleaseName is the name of the Helm release (default: "grove")
	ReleaseName string
	// ChartRef is the OCI chart reference (default: "oci://ghcr.io/nvidia/grove/grove-charts")
	ChartRef string
	// ChartVersion is the version of the chart to install (default: "v0.1.0-alpha.1")
	ChartVersion string
	// Namespace is the Kubernetes namespace to install into (default: "default")
	Namespace string
	// Values is a map of custom values to pass to the chart
	Values map[string]interface{}
	// Logger is the logger to use for output (default: uses CILogger)
	Logger func(format string, v ...interface{})
}

// Validate validates required fields and normalizes optional ones.
// It returns an error if any required field is missing.
func (c *GroveInstallConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config cannot be nil")
	}

	var missing []string
	if c.ReleaseName == "" {
		missing = append(missing, "release name")
	}
	if c.ChartRef == "" {
		missing = append(missing, "chart reference")
	}
	if c.ChartVersion == "" {
		missing = append(missing, "chart version")
	}
	if c.Namespace == "" {
		missing = append(missing, "namespace")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
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

// GroveInstallConfigV0_1_0_Alpha1 returns a configuration for Grove v0.1.0-alpha.1 installation
func GroveInstallConfigV0_1_0_Alpha1() *GroveInstallConfig {
	defaultLogger := NewCILogger(nil)
	return &GroveInstallConfig{
		ReleaseName:  "grove",
		ChartRef:     "oci://ghcr.io/nvidia/grove/grove-charts",
		ChartVersion: "v0.1.0-alpha.1",
		Namespace:    "default",
		Values:       make(map[string]interface{}),
		Logger:       defaultLogger.Printf,
	}
}

// InstallGrove installs Grove on a Kubernetes cluster using Helm
// It returns the installed release and any error that occurred
func InstallGrove(config *GroveInstallConfig, logger *CILogger) (*release.Release, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Override the logger in config with the passed logger
	config.Logger = logger.Printf

	config.Logger("Setting up Helm and Kubernetes configuration...")

	// Initialize Helm settings
	settings := cli.New()

	// Create a new ActionConfig object
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), config.Namespace, os.Getenv("HELM_DRIVER"), config.Logger); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %w", err)
	}

	// Initialize an OCI registry client for pulling charts via OCI
	regClient, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Helm registry client: %w", err)
	}
	actionConfig.RegistryClient = regClient

	config.Logger("Locating and pulling chart %s version %s...", config.ChartRef, config.ChartVersion)

	// Create a new Install action client
	installClient := action.NewInstall(actionConfig)
	installClient.Namespace = config.Namespace
	installClient.ReleaseName = config.ReleaseName
	installClient.Version = config.ChartVersion

	// Set up chart path options for locating the chart
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

	config.Logger("Installing release: %s", config.ReleaseName)

	// Perform the installation
	rel, err := installClient.Run(chart, config.Values)
	if err != nil {
		return nil, fmt.Errorf("helm install failed: %w", err)
	}

	config.Logger("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}

// InstallOrUpgradeGrove installs Grove if it doesn't exist, or upgrades it if it does
// It returns the installed/upgraded release and any error that occurred
func InstallOrUpgradeGrove(config *GroveInstallConfig, logger *CILogger) (*release.Release, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Override the logger in config with the passed logger
	config.Logger = logger.Printf

	config.Logger("Setting up Helm and Kubernetes configuration...")

	// Initialize Helm settings
	settings := cli.New()

	// Create a new ActionConfig object
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), config.Namespace, os.Getenv("HELM_DRIVER"), config.Logger); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %w", err)
	}

	// Initialize an OCI registry client for pulling charts via OCI
	regClient, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Helm registry client: %w", err)
	}
	actionConfig.RegistryClient = regClient

	config.Logger("Locating and pulling chart %s version %s...", config.ChartRef, config.ChartVersion)

	// Create upgrade client first to locate the chart
	upgradeClient := action.NewUpgrade(actionConfig)
	upgradeClient.ChartPathOptions.Version = config.ChartVersion
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

	// Try upgrade first
	config.Logger("Attempting to upgrade release: %s", config.ReleaseName)
	upgradeClient.Install = false
	if rel, err := upgradeClient.Run(config.ReleaseName, chart, config.Values); err == nil {
		config.Logger("Success! Release '%s' upgraded in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
		return rel, nil
	}

	// If upgrade failed, try install
	config.Logger("Upgrade failed, attempting fresh install of release: %s", config.ReleaseName)
	installClient := action.NewInstall(actionConfig)
	installClient.Namespace = config.Namespace
	installClient.ReleaseName = config.ReleaseName
	installClient.Version = config.ChartVersion

	rel, err := installClient.Run(chart, config.Values)
	if err != nil {
		return nil, fmt.Errorf("both upgrade and install failed: %w", err)
	}

	config.Logger("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}
