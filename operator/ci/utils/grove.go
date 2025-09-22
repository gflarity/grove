package utils

import (
	"context"
	"fmt"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// AppliedPodCliqueSet holds information about an applied PodCliqueSet (for backward compatibility)
type AppliedPodCliqueSet struct {
	Name      string
	Namespace string
}

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
	// RestConfig is the Kubernetes REST config to use (optional, defaults to system kubeconfig)
	RestConfig *rest.Config
}

// ToHelmInstallConfig converts GroveInstallConfig to HelmInstallConfig
func (c *GroveInstallConfig) ToHelmInstallConfig() *HelmInstallConfig {
	return &HelmInstallConfig{
		ReleaseName:     c.ReleaseName,
		ChartRef:        c.ChartRef,
		ChartVersion:    c.ChartVersion,
		Namespace:       c.Namespace,
		CreateNamespace: false, // Grove installation doesn't create namespace by default
		Values:          c.Values,
		Logger:          c.Logger,
		RestConfig:      c.RestConfig,
	}
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

// InstallOrUpgradeGrove installs Grove if it doesn't exist, or upgrades it if it does
// It returns the installed/upgraded release and any error that occurred
func InstallOrUpgradeGrove(config *GroveInstallConfig, logger *CILogger) (*release.Release, error) {
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

	rel, err := installClient.Run(chart, config.Values)
	if err != nil {
		return nil, fmt.Errorf("both upgrade and install failed: %w", err)
	}

	config.Logger("Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	return rel, nil
}

// WaitForGrovePodsReady waits for Grove operator pods to be ready
func WaitForGrovePodsReady(ctx context.Context, namespace string, restConfig *rest.Config, logger *CILogger) error {
	logger.Info("⏳ Waiting for Grove operator pods to be ready...")

	err := WaitForPodsInNamespace(ctx, namespace, restConfig, 5*time.Minute, logger)
	if err != nil {
		return fmt.Errorf("failed waiting for Grove pods: %w", err)
	}

	logger.Info("✅ Grove operator pods are ready!")
	return nil
}

// waitForGroveOperatorReady waits for the Grove operator to be ready before applying workloads
func waitForGroveOperatorReady(ctx context.Context, config *WorkloadConfig, logger *CILogger) error {
	clientset, err := kubernetes.NewForConfig(config.RestConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Grove operator is always installed in grove-system namespace
	namespace := "grove-system"

	logger.Infof("⏳ Waiting for Grove operator to be ready in namespace %s...", namespace)

	// Wait for the Grove operator deployment to be ready
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		// Check if the Grove operator deployment is ready
		deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, "grove-operator", metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("⏳ Grove operator deployment not found yet, waiting...")
				return false, nil
			}
			logger.Errorf("Failed to get Grove operator deployment: %v", err)
			return false, nil
		}

		// Check if deployment is ready
		if deployment.Status.ReadyReplicas > 0 && deployment.Status.ReadyReplicas == deployment.Status.Replicas {
			logger.Info("✅ Grove operator deployment is ready!")

			// Also check if the webhook service has endpoints
			endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(ctx, "grove-operator", metav1.GetOptions{})
			if err != nil {
				logger.Infof("⏳ Grove operator service endpoints not ready yet: %v", err)
				return false, nil
			}

			if len(endpoints.Subsets) > 0 && len(endpoints.Subsets[0].Addresses) > 0 {
				logger.Info("✅ Grove operator webhook service endpoints are ready!")

				// Check if the webhook server is actually ready by verifying webhook configurations
				logger.Info("⏳ Checking webhook server readiness...")
				if checkWebhookReadiness(ctx, clientset, config.RestConfig, namespace, logger) {
					logger.Info("✅ Grove operator webhook server is ready!")
					return true, nil
				}

				logger.Info("⏳ Grove operator webhook server not ready yet...")
				return false, nil
			}

			logger.Info("⏳ Grove operator webhook service endpoints not ready yet...")
			return false, nil
		}

		logger.Infof("⏳ Grove operator deployment not ready yet (ready: %d/%d)", deployment.Status.ReadyReplicas, deployment.Status.Replicas)
		return false, nil
	})
}

// checkWebhookReadiness checks if the Grove operator webhook server is ready by verifying webhook configurations
func checkWebhookReadiness(ctx context.Context, clientset *kubernetes.Clientset, restConfig *rest.Config, namespace string, logger *CILogger) bool {
	// Check that the webhook configurations are properly set up with CA bundles
	// This indicates that the cert-manager has finished setting up certificates
	// and the webhook server should be ready to accept requests

	// 1. Check if the webhook server certificate secret exists
	logger.Info("⏳ Checking webhook server certificate secret...")
	_, err := clientset.CoreV1().Secrets(namespace).Get(ctx, "grove-webhook-server-cert", metav1.GetOptions{})
	if err != nil {
		logger.Infof("Webhook certificate secret not ready: %v", err)
		return false
	}
	logger.Info("✅ Webhook certificate secret exists")

	// 2. Check ValidatingWebhookConfiguration has CA bundle set
	logger.Info("⏳ Checking ValidatingWebhookConfiguration...")
	vwc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, "podcliqueset-validating-webhook", metav1.GetOptions{})
	if err != nil {
		logger.Infof("ValidatingWebhookConfiguration not ready: %v", err)
		return false
	}
	if len(vwc.Webhooks) == 0 || len(vwc.Webhooks[0].ClientConfig.CABundle) == 0 {
		logger.Info("ValidatingWebhookConfiguration CA bundle not set yet")
		return false
	}
	logger.Info("✅ ValidatingWebhookConfiguration has CA bundle")

	// 3. Check MutatingWebhookConfiguration has CA bundle set
	logger.Info("⏳ Checking MutatingWebhookConfiguration...")
	mwc, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, "podcliqueset-defaulting-webhook", metav1.GetOptions{})
	if err != nil {
		logger.Infof("MutatingWebhookConfiguration not ready: %v", err)
		return false
	}
	if len(mwc.Webhooks) == 0 || len(mwc.Webhooks[0].ClientConfig.CABundle) == 0 {
		logger.Info("MutatingWebhookConfiguration CA bundle not set yet")
		return false
	}
	logger.Info("✅ MutatingWebhookConfiguration has CA bundle")

	// 4. Test webhook connectivity by creating a test PodCliqueSet to see if webhook responds
	logger.Info("⏳ Testing webhook server connectivity...")
	if testWebhookConnectivity(ctx, restConfig, namespace, logger) {
		logger.Info("✅ Webhook server is responding to requests")
		return true
	}

	logger.Info("⏳ Webhook server not responding yet...")
	return false
}

// testWebhookConnectivity tests if the webhook server is actually responding by attempting to create a test resource
func testWebhookConnectivity(ctx context.Context, restConfig *rest.Config, namespace string, logger *CILogger) bool {
	// Create clientset for testing
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		logger.Infof("Failed to create clientset for webhook test: %v", err)
		return false
	}

	// Create a test namespace for webhook connectivity test
	testNamespace := "webhook-test-" + fmt.Sprintf("%d", time.Now().Unix())

	// Create the test namespace
	_, err = clientset.CoreV1().Namespaces().Create(ctx, &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		logger.Infof("Failed to create test namespace: %v", err)
		return false
	}

	// Clean up the test namespace regardless of outcome
	defer func() {
		_ = clientset.CoreV1().Namespaces().Delete(ctx, testNamespace, metav1.DeleteOptions{})
	}()

	// Create a minimal test PodCliqueSet to trigger the webhook
	testPCS := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "grove.io/v1alpha1",
			"kind":       "PodCliqueSet",
			"metadata": map[string]interface{}{
				"name":      "webhook-test",
				"namespace": testNamespace,
			},
			"spec": map[string]interface{}{
				"replicas": 1,
				"template": map[string]interface{}{
					"cliques": []interface{}{
						map[string]interface{}{
							"name": "test-clique",
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name":  "test",
											"image": "nginx:latest",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Try to create the test PodCliqueSet using dynamic client
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		logger.Infof("Failed to create dynamic client for webhook test: %v", err)
		return false
	}

	gvr := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquesets",
	}

	// Attempt to create the test resource - if webhook is working, this should succeed or fail with validation error
	// If webhook is not ready, we'll get a connection error
	_, err = dynamicClient.Resource(gvr).Namespace(testNamespace).Create(ctx, testPCS, metav1.CreateOptions{})
	if err != nil {
		// Check if it's a webhook connectivity error (502 Bad Gateway, connection refused, etc.)
		errStr := err.Error()
		if strings.Contains(errStr, "502 Bad Gateway") ||
			strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "proxy error") ||
			strings.Contains(errStr, "failed to call webhook") {
			logger.Info("⏳ Webhook server not ready yet (connectivity test pending)")
			return false
		}
		// If it's a validation error or other webhook response, that means webhook is working
		logger.Infof("Webhook responded (validation error expected): %v", err)
	}

	// Clean up the test resource if it was created
	_ = dynamicClient.Resource(gvr).Namespace(testNamespace).Delete(ctx, "webhook-test", metav1.DeleteOptions{})

	return true
}

// waitForPodCliqueSetPodsReady waits for all pods created by the PodCliqueSet resources to be ready
func waitForPodCliqueSetPodsReady(ctx context.Context, config *WorkloadConfig, podCliqueSets []AppliedPodCliqueSet, logger *CILogger) error {
	// Create clientset for pod operations
	clientset, err := kubernetes.NewForConfig(config.RestConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	// Extract PodCliqueSet names and namespaces
	var podCliqueSetNames []string
	var namespaces []string

	for _, pcs := range podCliqueSets {
		podCliqueSetNames = append(podCliqueSetNames, pcs.Name)
		namespace := pcs.Namespace
		if namespace == "" {
			namespace = "default"
		}
		namespaces = append(namespaces, namespace)
	}

	logger.Infof("⏳ Waiting for pods from PodCliqueSet resources: %v", podCliqueSetNames)

	// Wait for all pods to be ready
	return wait.PollUntilContextTimeout(timeoutCtx, 5*time.Second, config.Timeout, true, func(ctx context.Context) (bool, error) {
		allReady := true
		totalPods := 0
		readyPods := 0

		for i, pcsName := range podCliqueSetNames {
			namespace := namespaces[i]

			// List pods with labels that match the PodCliqueSet
			labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s", pcsName)
			pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil {
				logger.Errorf("Failed to list pods for PodCliqueSet %s: %v", pcsName, err)
				return false, nil // Continue polling
			}

			for _, pod := range pods.Items {
				totalPods++
				if isPodReady(&pod) {
					readyPods++
				} else {
					allReady = false
				}
			}
		}

		logger.Infof("📊 Pod status: %d/%d ready", readyPods, totalPods)

		if totalPods == 0 {
			logger.Info("⏳ No pods found yet, Grove operator may still be creating resources...")
			return false, nil
		}

		return allReady, nil
	})
}
