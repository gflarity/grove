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
	defaultLogger := NewCILogger(nil)
	return &KaiInstallConfig{
		BaseInstallConfig: BaseInstallConfig{
			ReleaseName:  "kai-scheduler",
			ChartRef:     "oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler",
			ChartVersion: version,
			Namespace:    "kai-scheduler",
			Values:       make(map[string]interface{}),
			Logger:       defaultLogger.Printf,
		},
	}
}

// InstallKai installs Kai Scheduler on a Kubernetes cluster using Helm
// It returns the installed release and any error that occurred
func InstallKai(config *KaiInstallConfig, logger *CILogger) (*release.Release, error) {
	return InstallComponent(config, logger)
}

// InstallOrUpgradeKai installs Kai Scheduler if it doesn't exist, or upgrades it if it does
// It returns the installed/upgraded release and any error that occurred
func InstallOrUpgradeKai(config *KaiInstallConfig, logger *CILogger) (*release.Release, error) {
	return InstallOrUpgradeComponent(config, logger)
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
