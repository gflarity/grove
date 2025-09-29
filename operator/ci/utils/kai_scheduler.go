package utils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/release"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// KaiInstallConfig holds configuration for installing Kai Scheduler
type KaiInstallConfig struct {
	BaseInstallConfig
}

// Component-specific configuration methods for ComponentInstallConfig interface
func (c *KaiInstallConfig) GetCreateNamespace() bool { return true }  // Kai scheduler installation creates namespace by default
func (c *KaiInstallConfig) GetWait() bool            { return false } // Default wait behavior
func (c *KaiInstallConfig) GetGenerateName() bool    { return false } // Default generate name behavior

func (c *KaiInstallConfig) GetDefaultReleaseName() string { return "kai-scheduler" }
func (c *KaiInstallConfig) GetDefaultChartRef() string {
	return "oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler"
}
func (c *KaiInstallConfig) GetDefaultNamespace() string { return "kai-scheduler" }

// ValidateComponent provides component-specific validation
func (c *KaiInstallConfig) ValidateComponent() error {
	// No additional validation needed for Kai Scheduler beyond base validation
	return c.ValidateBase()
}

// ToHelmInstallConfig converts KaiInstallConfig to HelmInstallConfig (for backward compatibility)
func (c *KaiInstallConfig) ToHelmInstallConfig() *HelmInstallConfig {
	return ToHelmInstallConfig(c)
}

// Validate validates required fields and normalizes optional ones (for backward compatibility)
func (c *KaiInstallConfig) Validate() error {
	return ValidateComponentConfig(c)
}

// KaiInstallConfigLatest returns a configuration for Kai Scheduler installation with latest defaults
// Note: You must specify the version as it's required
func KaiInstallConfigLatest(version string) *KaiInstallConfig {

	// TODO fix this to pass in logger properl
	return &KaiInstallConfig{
		BaseInstallConfig: BaseInstallConfig{
			ReleaseName:  "kai-scheduler",
			ChartRef:     "oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler",
			ChartVersion: version,
			Namespace:    "kai-scheduler",
			Values:       make(map[string]interface{}),
			Logger:       func(format string, args ...interface{}) {},
		},
	}
}

// InstallKai installs Kai Scheduler on a Kubernetes cluster using Helm
// It returns the installed release and any error that occurred
func InstallKai(config *KaiInstallConfig, logger *logrus.Logger) (*release.Release, error) {
	return InstallComponent(config, logger)
}

// InstallOrUpgradeKai installs Kai Scheduler if it doesn't exist, or upgrades it if it does
// It returns the installed/upgraded release and any error that occurred
func InstallOrUpgradeKai(config *KaiInstallConfig, logger *logrus.Logger) (*release.Release, error) {
	return InstallOrUpgradeComponent(config, logger)
}

// WaitForKaiPodsReady waits for Kai Scheduler pods to be ready
func WaitForKaiPodsReady(ctx context.Context, restConfig *rest.Config, logger *logrus.Logger) error {
	logger.Debug("⏳ Waiting for Kai Scheduler pods to be ready...")

	// Kai scheduler is installed in kai-scheduler namespace by default
	err := WaitForPodsInNamespace(ctx, "kai-scheduler", restConfig, 5*time.Minute, logger)
	if err != nil {
		return fmt.Errorf("failed waiting for Kai Scheduler pods: %w", err)
	}

	logger.Debug("✅ Kai Scheduler pods are ready!")
	return nil
}

// WaitForKaiCRDs waits for the Queue CRD from scheduling.run.ai/v2 to be available
func WaitForKaiCRDs(ctx context.Context, restConfig *rest.Config, logger *logrus.Logger) error {
	logger.Debug("⏳ Waiting for Queue CRD (scheduling.run.ai/v2) to be available...")

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
			logger.Debug("✅ Queue CRD (scheduling.run.ai/v2) is available and established!")
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

// CreateDefaultKaiQueues creates queues using the k8s client YAML apply functionality
func CreateDefaultKaiQueues(ctx context.Context, restConfig *rest.Config, logger *logrus.Logger) error {
	logger.Debug("📄 Creating queues using k8s client...")

	// Get the path to the queues.yaml file relative to this source file
	_, currentFile, _, _ := runtime.Caller(0)
	queuesPath := filepath.Join(filepath.Dir(currentFile), "../yaml/queues.yaml")

	// Read the queues YAML file content
	yamlContent, err := os.ReadFile(queuesPath)
	if err != nil {
		return fmt.Errorf("failed to read queues YAML file %s: %w", queuesPath, err)
	}

	// Apply the YAML content using the k8s client
	appliedResources, err := ApplyYAMLContent(ctx, string(yamlContent), "", restConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to apply queues YAML: %w", err)
	}

	logger.Debugf("✅ Successfully applied %d queue resources", len(appliedResources))
	return nil
}
