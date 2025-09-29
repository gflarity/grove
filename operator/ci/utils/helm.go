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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// HelmInstallConfig holds common configuration for Helm installations
type HelmInstallConfig struct {
	// ReleaseName is the name of the Helm release
	ReleaseName string
	// ChartRef is the OCI chart reference
	ChartRef string
	// ChartVersion is the version of the chart to install
	ChartVersion string
	// Namespace is the Kubernetes namespace to install into
	Namespace string
	// CreateNamespace determines if the namespace should be created if it doesn't exist
	CreateNamespace bool
	// Values is a map of custom values to pass to the chart
	Values map[string]interface{}
	// Logger is the logger to use for output
	Logger func(format string, v ...interface{})
	// RestConfig is the Kubernetes REST config to use (optional, defaults to system kubeconfig)
	RestConfig *rest.Config
}

// Validate validates required fields and normalizes optional ones.
func (c *HelmInstallConfig) Validate() error {
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

// customRESTClientGetter implements genericclioptions.RESTClientGetter using a provided rest.Config
type customRESTClientGetter struct {
	restConfig *rest.Config
	namespace  string
}

func (c *customRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return c.restConfig, nil
}

func (c *customRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(c.restConfig)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(discoveryClient), nil
}

func (c *customRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := c.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	return mapper, nil
}

func (c *customRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return &directClientConfig{
		config:    c.restConfig,
		namespace: c.namespace,
	}
}

// directClientConfig implements clientcmd.ClientConfig for direct REST config usage
type directClientConfig struct {
	config    *rest.Config
	namespace string
}

func (d *directClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, fmt.Errorf("raw config not available")
}

func (d *directClientConfig) ClientConfig() (*rest.Config, error) {
	return d.config, nil
}

func (d *directClientConfig) Namespace() (string, bool, error) {
	if d.namespace == "" {
		return "default", false, nil
	}
	return d.namespace, true, nil
}

func (d *directClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}

// ComponentInstallConfig represents a common interface for all component installation configs
type ComponentInstallConfig interface {
	GetReleaseName() string
	SetReleaseName(string)
	GetChartRef() string
	SetChartRef(string)
	GetChartVersion() string
	SetChartVersion(string)
	GetNamespace() string
	SetNamespace(string)
	GetValues() map[string]interface{}
	SetValues(map[string]interface{})
	GetLogger() func(format string, v ...interface{})
	SetLogger(func(format string, v ...interface{}))
	GetRestConfig() *rest.Config
	SetRestConfig(*rest.Config)

	// Component-specific configuration
	GetCreateNamespace() bool
	GetWait() bool
	GetGenerateName() bool
	GetDefaultReleaseName() string
	GetDefaultChartRef() string
	GetDefaultNamespace() string

	// Validation with component-specific rules
	ValidateComponent() error
}

// BaseInstallConfig provides common fields and methods for all component install configs
type BaseInstallConfig struct {
	ReleaseName  string
	ChartRef     string
	ChartVersion string
	Namespace    string
	Values       map[string]interface{}
	Logger       func(format string, v ...interface{})
	RestConfig   *rest.Config
}

// Common getter methods
func (c *BaseInstallConfig) GetReleaseName() string                           { return c.ReleaseName }
func (c *BaseInstallConfig) SetReleaseName(name string)                       { c.ReleaseName = name }
func (c *BaseInstallConfig) GetChartRef() string                              { return c.ChartRef }
func (c *BaseInstallConfig) SetChartRef(ref string)                           { c.ChartRef = ref }
func (c *BaseInstallConfig) GetChartVersion() string                          { return c.ChartVersion }
func (c *BaseInstallConfig) SetChartVersion(version string)                   { c.ChartVersion = version }
func (c *BaseInstallConfig) GetNamespace() string                             { return c.Namespace }
func (c *BaseInstallConfig) SetNamespace(namespace string)                    { c.Namespace = namespace }
func (c *BaseInstallConfig) GetValues() map[string]interface{}                { return c.Values }
func (c *BaseInstallConfig) SetValues(values map[string]interface{})          { c.Values = values }
func (c *BaseInstallConfig) GetLogger() func(format string, v ...interface{}) { return c.Logger }
func (c *BaseInstallConfig) SetLogger(logger func(format string, v ...interface{})) {
	c.Logger = logger
}
func (c *BaseInstallConfig) GetRestConfig() *rest.Config       { return c.RestConfig }
func (c *BaseInstallConfig) SetRestConfig(config *rest.Config) { c.RestConfig = config }

// ValidateBase provides common validation logic
func (c *BaseInstallConfig) ValidateBase() error {
	if c.Values == nil {
		c.Values = make(map[string]interface{})
	}
	if c.Logger == nil {
		c.Logger = func(format string, args ...interface{}) {}
	}
	return nil
}

// ToHelmInstallConfig converts any ComponentInstallConfig to HelmInstallConfig
func ToHelmInstallConfig(config ComponentInstallConfig) *HelmInstallConfig {
	return &HelmInstallConfig{
		ReleaseName:     config.GetReleaseName(),
		ChartRef:        config.GetChartRef(),
		ChartVersion:    config.GetChartVersion(),
		Namespace:       config.GetNamespace(),
		CreateNamespace: config.GetCreateNamespace(),
		Values:          config.GetValues(),
		Logger:          config.GetLogger(),
		RestConfig:      config.GetRestConfig(),
	}
}

// ValidateComponentConfig validates a component config with defaults
func ValidateComponentConfig(config ComponentInstallConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Set defaults
	if config.GetReleaseName() == "" {
		config.SetReleaseName(config.GetDefaultReleaseName())
	}
	if config.GetChartRef() == "" {
		config.SetChartRef(config.GetDefaultChartRef())
	}
	if config.GetNamespace() == "" {
		config.SetNamespace(config.GetDefaultNamespace())
	}

	// ChartVersion is required and has no default
	if config.GetChartVersion() == "" {
		return fmt.Errorf("chart version is required")
	}

	if config.GetValues() == nil {
		config.SetValues(make(map[string]interface{}))
	}
	if config.GetLogger() == nil {
		defaultLogger := NewCILogger(nil)
		config.SetLogger(defaultLogger.Printf)
	}

	// Component-specific validation
	return config.ValidateComponent()
}

// InstallComponent installs a component using the common installation pattern
func InstallComponent(config ComponentInstallConfig, logger *CILogger) (*release.Release, error) {
	if err := ValidateComponentConfig(config); err != nil {
		return nil, err
	}

	// Convert to HelmInstallConfig
	helmConfig := ToHelmInstallConfig(config)

	logger.Debugf("Setting up Helm and Kubernetes configuration for %s...", config.GetReleaseName())

	// Set up Helm action configuration
	actionConfig, _, err := setupHelmAction(helmConfig)
	if err != nil {
		return nil, err
	}

	logger.Debugf("Locating and pulling chart %s version %s...", config.GetChartRef(), config.GetChartVersion())

	// Create a new Install action client
	installClient := action.NewInstall(actionConfig)
	installClient.Namespace = config.GetNamespace()
	installClient.Version = config.GetChartVersion()
	installClient.CreateNamespace = config.GetCreateNamespace()
	installClient.Wait = config.GetWait()
	installClient.GenerateName = config.GetGenerateName()

	// Only set ReleaseName if not generating name
	if !config.GetGenerateName() && config.GetReleaseName() != "" {
		installClient.ReleaseName = config.GetReleaseName()
	}

	// Set up chart path options for locating the chart
	settings := cli.New()
	installClient.ChartPathOptions.Version = config.GetChartVersion()
	chartPath, err := installClient.ChartPathOptions.LocateChart(config.GetChartRef(), settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart: %w", err)
	}
	logger.Debugf("Chart located at: %s", chartPath)

	// Load the chart from the located path
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// Perform the installation
	rel, err := installClient.Run(chart, config.GetValues())
	if err != nil {
		return nil, fmt.Errorf("helm install failed: %w", err)
	}

	logger.Debugf("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}

// InstallOrUpgradeComponent installs a component if it doesn't exist, or upgrades it if it does
func InstallOrUpgradeComponent(config ComponentInstallConfig, logger *CILogger) (*release.Release, error) {
	if err := ValidateComponentConfig(config); err != nil {
		return nil, err
	}

	// Convert to HelmInstallConfig
	helmConfig := ToHelmInstallConfig(config)

	logger.Debug("Setting up Helm and Kubernetes configuration...")

	// Set up Helm action configuration
	actionConfig, _, err := setupHelmAction(helmConfig)
	if err != nil {
		return nil, err
	}

	logger.Debugf("Locating and pulling chart %s version %s...", config.GetChartRef(), config.GetChartVersion())

	// Create upgrade client first to locate the chart
	upgradeClient := action.NewUpgrade(actionConfig)
	upgradeClient.ChartPathOptions.Version = config.GetChartVersion()
	upgradeClient.Wait = config.GetWait()
	settings := cli.New()
	chartPath, err := upgradeClient.ChartPathOptions.LocateChart(config.GetChartRef(), settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart: %w", err)
	}
	logger.Debugf("Chart located at: %s", chartPath)

	// Load the chart from the located path
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// Determine release name for upgrade (can't use auto-generated names for upgrades)
	releaseName := config.GetReleaseName()
	if releaseName == "" {
		releaseName = config.GetDefaultReleaseName()
	}

	// Try upgrade first
	logger.Debugf("Attempting to upgrade release: %s", releaseName)
	upgradeClient.Install = false
	if rel, err := upgradeClient.Run(releaseName, chart, config.GetValues()); err == nil {
		logger.Debugf("Success! Release '%s' upgraded in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
		return rel, nil
	}

	// If upgrade failed, try install
	logger.Debugf("Upgrade failed, attempting fresh install of release: %s", releaseName)
	installClient := action.NewInstall(actionConfig)
	installClient.Namespace = config.GetNamespace()
	installClient.ReleaseName = releaseName
	installClient.Version = config.GetChartVersion()
	installClient.CreateNamespace = config.GetCreateNamespace()
	installClient.Wait = config.GetWait()

	rel, err := installClient.Run(chart, config.GetValues())
	if err != nil {
		return nil, fmt.Errorf("both upgrade and install failed: %w", err)
	}

	logger.Debugf("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}

// setupHelmAction sets up Helm action configuration for the given config
func setupHelmAction(config *HelmInstallConfig) (*action.Configuration, genericclioptions.RESTClientGetter, error) {
	// Initialize Helm settings and REST client getter
	settings := cli.New()
	var restClientGetter genericclioptions.RESTClientGetter

	if config.RestConfig != nil {
		// Use custom REST config if provided
		restClientGetter = &customRESTClientGetter{
			restConfig: config.RestConfig,
			namespace:  config.Namespace,
		}
	} else {
		// Use default settings
		restClientGetter = settings.RESTClientGetter()
	}

	// Create a new ActionConfig object
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(restClientGetter, config.Namespace, os.Getenv("HELM_DRIVER"), config.Logger); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize Helm action configuration: %w", err)
	}

	// Initialize an OCI registry client for pulling charts via OCI
	regClient, err := registry.NewClient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Helm registry client: %w", err)
	}
	actionConfig.RegistryClient = regClient

	return actionConfig, restClientGetter, nil
}
