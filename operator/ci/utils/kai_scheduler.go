package utils

// Example usage:
//
// Basic installation:
//   config := KaiInstallConfigLatest("v1.2.3")
//   logger := NewCILogger(nil)
//   release, err := InstallKai(config, logger)
//
// Custom installation:
//   config := &KaiInstallConfig{
//       ReleaseName:  "my-kai-scheduler",
//       ChartRef:     "oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler",
//       ChartVersion: "v1.2.3",
//       Namespace:    "kube-system",
//       Values: map[string]interface{}{
//           "replicas": 2,
//           "resources": map[string]interface{}{
//               "requests": map[string]interface{}{
//                   "cpu": "100m",
//                   "memory": "128Mi",
//               },
//           },
//       },
//   }
//   logger := NewCILogger(nil)
//   release, err := InstallOrUpgradeKai(config, logger)

import (
	"context"
	"fmt"
	"time"

	schedulingv2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// KaiInstallConfig holds configuration for installing Kai Scheduler
type KaiInstallConfig struct {
	// ReleaseName is the name of the Helm release (default: "kai-scheduler")
	ReleaseName string
	// ChartRef is the OCI chart reference (default: "oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler")
	ChartRef string
	// ChartVersion is the version of the chart to install
	ChartVersion string
	// Namespace is the Kubernetes namespace to install into (default: "kai-scheduler")
	Namespace string
	// Values is a map of custom values to pass to the chart
	Values map[string]interface{}
	// Logger is the logger to use for output (default: uses CILogger)
	Logger func(format string, v ...interface{})
	// RestConfig is the Kubernetes REST config to use (optional, defaults to system kubeconfig)
	RestConfig *rest.Config
}

// ToHelmInstallConfig converts KaiInstallConfig to HelmInstallConfig
func (c *KaiInstallConfig) ToHelmInstallConfig() *HelmInstallConfig {
	return &HelmInstallConfig{
		ReleaseName:     c.ReleaseName,
		ChartRef:        c.ChartRef,
		ChartVersion:    c.ChartVersion,
		Namespace:       c.Namespace,
		CreateNamespace: true, // Kai scheduler installation creates namespace by default
		Values:          c.Values,
		Logger:          c.Logger,
		RestConfig:      c.RestConfig,
	}
}

// Validate validates required fields and normalizes optional ones.
func (c *KaiInstallConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Set defaults
	if c.ReleaseName == "" {
		c.ReleaseName = "kai-scheduler"
	}
	if c.ChartRef == "" {
		c.ChartRef = "oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler"
	}
	if c.Namespace == "" {
		c.Namespace = "kai-scheduler"
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

// KaiInstallConfigLatest returns a configuration for Kai Scheduler installation with latest defaults
// Note: You must specify the version as it's required
func KaiInstallConfigLatest(version string) *KaiInstallConfig {
	defaultLogger := NewCILogger(nil)
	return &KaiInstallConfig{
		ReleaseName:  "kai-scheduler",
		ChartRef:     "oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler",
		ChartVersion: version,
		Namespace:    "kai-scheduler",
		Values:       make(map[string]interface{}),
		Logger:       defaultLogger.Printf,
	}
}

// InstallKai installs Kai Scheduler on a Kubernetes cluster using Helm
// It returns the installed release and any error that occurred
func InstallKai(config *KaiInstallConfig, logger *CILogger) (*release.Release, error) {
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
	installClient.ReleaseName = config.ReleaseName
	installClient.Version = config.ChartVersion
	installClient.CreateNamespace = true // Equivalent to --create-namespace

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

	config.Logger("Installing release: %s", config.ReleaseName)

	// Perform the installation
	rel, err := installClient.Run(chart, config.Values)
	if err != nil {
		return nil, fmt.Errorf("helm install failed: %w", err)
	}

	config.Logger("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}

// InstallOrUpgradeKai installs Kai Scheduler if it doesn't exist, or upgrades it if it does
// It returns the installed/upgraded release and any error that occurred
func InstallOrUpgradeKai(config *KaiInstallConfig, logger *CILogger) (*release.Release, error) {
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
	installClient.CreateNamespace = true // Equivalent to --create-namespace

	rel, err := installClient.Run(chart, config.Values)
	if err != nil {
		return nil, fmt.Errorf("both upgrade and install failed: %w", err)
	}

	config.Logger("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}

// WaitForKaiPodsReady waits for Kai Scheduler pods to be ready
func WaitForKaiPodsReady(ctx context.Context, restConfig *rest.Config, logger *CILogger) error {
	logger.Info("⏳ Waiting for Kai Scheduler pods to be ready...")

	// Kai scheduler is installed in kai-scheduler namespace by default
	err := WaitForPodsInNamespace(ctx, "kai-scheduler", restConfig, 5*time.Minute, logger)
	if err != nil {
		return fmt.Errorf("failed waiting for Kai Scheduler pods: %w", err)
	}

	logger.Info("✅ Kai Scheduler pods are ready!")
	return nil
}

// WaitForKaiCRDs waits for the Queue CRD from scheduling.run.ai/v2 to be available
func WaitForKaiCRDs(ctx context.Context, restConfig *rest.Config, logger *CILogger) error {
	logger.Info("⏳ Waiting for Queue CRD (scheduling.run.ai/v2) to be available...")

	// Create API extensions client to check CRDs
	apiExtClient, err := apiextensionsclientset.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create API extensions client: %w", err)
	}

	crdName := "queues.scheduling.run.ai"
	timeout := 1 * time.Minute
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Check if the Queue CRD exists
		crd, err := apiExtClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crdName, metav1.GetOptions{})
		if err != nil {
			logger.Debugf("❌ Queue CRD not found yet: %v", err)
			logger.Info("⏳ Queue CRD not available, waiting 2 seconds...")
			time.Sleep(2 * time.Second)
			continue
		}

		// Check if the CRD is established and has the v2 version
		if isKaiCRDEstablished(crd) && hasKaiCRDVersion(crd, "v2") {
			logger.Info("✅ Queue CRD (scheduling.run.ai/v2) is available and established!")
			return nil
		}

		logger.Info("⏳ Queue CRD exists but not fully established, waiting 2 seconds...")
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for Queue CRD to be available after %v", timeout)
}

// isKaiCRDEstablished checks if a CRD is established
func isKaiCRDEstablished(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
			return true
		}
	}
	return false
}

// hasKaiCRDVersion checks if a CRD has a specific version available
func hasKaiCRDVersion(crd *apiextensionsv1.CustomResourceDefinition, version string) bool {
	for _, ver := range crd.Spec.Versions {
		if ver.Name == version && ver.Served {
			return true
		}
	}
	return false
}

// CreateDefaultKaiQueue creates a default queue using the KAI-Scheduler Go types
func CreateDefaultKaiQueue(ctx context.Context, restConfig *rest.Config, logger *CILogger) error {
	logger.Info("📄 Creating default queue using KAI-Scheduler Go client...")

	// Add the KAI-Scheduler scheme to the runtime scheme
	if err := schedulingv2.AddToScheme(scheme.Scheme); err != nil {
		return fmt.Errorf("failed to add KAI-Scheduler scheme: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create the default queue object
	queue := &schedulingv2.Queue{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "scheduling.run.ai/v2",
			Kind:       "Queue",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Spec: schedulingv2.QueueSpec{
			Resources: &schedulingv2.QueueResources{
				CPU: schedulingv2.QueueResource{
					Quota:           -1,
					Limit:           -1,
					OverQuotaWeight: 1,
				},
				GPU: schedulingv2.QueueResource{
					Quota:           -1,
					Limit:           -1,
					OverQuotaWeight: 1,
				},
				Memory: schedulingv2.QueueResource{
					Quota:           -1,
					Limit:           -1,
					OverQuotaWeight: 1,
				},
			},
		},
	}

	// Convert to unstructured for dynamic client
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(queue)
	if err != nil {
		return fmt.Errorf("failed to convert queue to unstructured: %w", err)
	}

	// Get the GVR for Queue
	gvr := schedulingv2.SchemeGroupVersion.WithResource("queues")

	// Create the queue
	_, err = dynamicClient.Resource(gvr).Create(ctx, &unstructured.Unstructured{Object: unstructuredObj}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create queue: %w", err)
	}

	logger.Info("✅ Default queue created successfully using Go client")
	return nil
}
