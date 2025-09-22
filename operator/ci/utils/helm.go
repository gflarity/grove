package utils

import (
	"fmt"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
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
