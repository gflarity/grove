package utils

// Example usage:
//
// Basic installation:
//   config := NvidiaOperatorInstallConfigLatest("v25.3.4")
//   logger := NewCILogger(logrus.InfoLevel)
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
//   logger := NewCILogger(logrus.InfoLevel)
//   release, err := InstallOrUpgradeNvidiaOperator(config, logger)

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/rest"
)

// NvidiaOperatorInstallConfig holds configuration for installing NVIDIA GPU Operator
type NvidiaOperatorInstallConfig struct {
	BaseInstallConfig
	// Wait enables waiting for the installation to complete (default: false)
	Wait bool
	// GenerateName enables auto-generation of release name (default: true)
	GenerateName bool
}

// Component-specific configuration methods for ComponentInstallConfig interface
func (c *NvidiaOperatorInstallConfig) GetCreateNamespace() bool { return true }           // NVIDIA operator installation creates namespace by default
func (c *NvidiaOperatorInstallConfig) GetWait() bool            { return c.Wait }         // Use instance field
func (c *NvidiaOperatorInstallConfig) GetGenerateName() bool    { return c.GenerateName } // Use instance field

func (c *NvidiaOperatorInstallConfig) GetDefaultReleaseName() string { return "nvidia-gpu-operator" }
func (c *NvidiaOperatorInstallConfig) GetDefaultChartRef() string    { return "nvidia/gpu-operator" }
func (c *NvidiaOperatorInstallConfig) GetDefaultNamespace() string   { return "gpu-operator" }

// ValidateComponent provides component-specific validation
func (c *NvidiaOperatorInstallConfig) ValidateComponent() error {
	// No additional validation needed for NVIDIA GPU Operator beyond base validation
	return c.ValidateBase()
}

// ToHelmInstallConfig converts NvidiaOperatorInstallConfig to HelmInstallConfig (for backward compatibility)
func (c *NvidiaOperatorInstallConfig) ToHelmInstallConfig() *HelmInstallConfig {
	return ToHelmInstallConfig(c)
}

// Validate validates required fields and normalizes optional ones (for backward compatibility)
func (c *NvidiaOperatorInstallConfig) Validate() error {
	return ValidateComponentConfig(c)
}

// NvidiaOperatorInstallConfigLatest returns a configuration for NVIDIA GPU Operator installation with latest defaults
// Note: You must specify the version as it's required
func NvidiaOperatorInstallConfigLatest(version string) *NvidiaOperatorInstallConfig {

	// TODO logging function needs to be passed in
	return &NvidiaOperatorInstallConfig{
		BaseInstallConfig: BaseInstallConfig{
			ReleaseName:  "", // Will be auto-generated
			ChartRef:     "nvidia/gpu-operator",
			ChartVersion: version,
			Namespace:    "gpu-operator",
			Values:       make(map[string]interface{}),
			Logger:       func(format string, args ...interface{}) {},
		},
		Wait:         false, // Disable wait for test environments without GPUs
		GenerateName: true,
	}
}

// InstallNvidiaOperator installs NVIDIA GPU Operator on a Kubernetes cluster using Helm
// It returns the installed release and any error that occurred
func InstallNvidiaOperator(config *NvidiaOperatorInstallConfig, logger *logrus.Logger) (*release.Release, error) {
	return InstallComponent(config, logger)
}

// InstallOrUpgradeNvidiaOperator installs NVIDIA GPU Operator if it doesn't exist, or upgrades it if it does
// It returns the installed/upgraded release and any error that occurred
func InstallOrUpgradeNvidiaOperator(config *NvidiaOperatorInstallConfig, logger *logrus.Logger) (*release.Release, error) {
	return InstallOrUpgradeComponent(config, logger)
}

// WaitForNvidiaOperatorPodsReady waits for NVIDIA GPU Operator pods to be ready
func WaitForNvidiaOperatorPodsReady(ctx context.Context, restConfig *rest.Config, logger *logrus.Logger) error {
	logger.Debug("⏳ Waiting for NVIDIA GPU Operator pods to be ready...")

	// NVIDIA GPU operator is installed in gpu-operator namespace by default
	// Use shorter timeout for test environments with disabled components
	err := WaitForPodsInNamespace(ctx, "gpu-operator", restConfig, 5*time.Minute, logger)
	if err != nil {
		return fmt.Errorf("failed waiting for NVIDIA GPU Operator pods: %w", err)
	}

	logger.Debug("✅ NVIDIA GPU Operator pods are ready!")
	return nil
}

// WaitForNvidiaOperatorReady waits for the NVIDIA GPU Operator to be fully ready
// In test environments without GPUs, this simply waits for the operator pods to be ready
func WaitForNvidiaOperatorReady(ctx context.Context, restConfig *rest.Config, logger *logrus.Logger) error {
	logger.Debug("⏳ Waiting for NVIDIA GPU Operator to be ready...")

	// For test environments, just wait for the operator pods to be ready
	// The WaitForNvidiaOperatorPodsReady function already does comprehensive pod readiness checking
	if err := WaitForNvidiaOperatorPodsReady(ctx, restConfig, logger); err != nil {
		return err
	}

	logger.Debug("✅ NVIDIA GPU Operator is ready!")
	return nil
}
