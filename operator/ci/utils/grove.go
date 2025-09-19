package utils

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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
	// RestConfig is the Kubernetes REST config to use (optional, defaults to system kubeconfig)
	RestConfig *rest.Config
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

// AppliedPodCliqueSet holds information about an applied PodCliqueSet
type AppliedPodCliqueSet struct {
	Name      string
	Namespace string
}

// WorkloadConfig holds configuration for applying workload YAML files
type WorkloadConfig struct {
	// YAMLFilePath is the path to the YAML file to apply
	YAMLFilePath string
	// Namespace is the namespace to apply the workload to (optional, uses namespace from YAML if not specified)
	Namespace string
	// RestConfig is the Kubernetes REST config to use
	RestConfig *rest.Config
	// Timeout is the maximum time to wait for pods to be ready (default: 5 minutes)
	Timeout time.Duration
}

// ApplyWorkloadAndWaitForPods applies a YAML workload file and waits for all pods to be ready
func ApplyWorkloadAndWaitForPods(ctx context.Context, config *WorkloadConfig, logger *CILogger) error {
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Minute
	}

	logger.Infof("📄 Applying workload from %s...", config.YAMLFilePath)

	// Wait for Grove operator to be ready first
	if err := waitForGroveOperatorReady(ctx, config, logger); err != nil {
		return fmt.Errorf("Grove operator not ready: %w", err)
	}

	// Read the YAML file
	yamlData, err := os.ReadFile(config.YAMLFilePath)
	if err != nil {
		return fmt.Errorf("failed to read YAML file %s: %w", config.YAMLFilePath, err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config.RestConfig)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// We don't need discovery client for this approach since we know the GVR

	// Parse and apply YAML documents
	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(string(yamlData)), 4096)
	var appliedPodCliqueSets []AppliedPodCliqueSet

	for {
		var rawObj runtime.RawExtension
		if err := decoder.Decode(&rawObj); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode YAML: %w", err)
		}

		if len(rawObj.Raw) == 0 {
			continue
		}

		// Create a scheme with Grove types
		scheme := runtime.NewScheme()
		if err := grovecorev1alpha1.AddToScheme(scheme); err != nil {
			return fmt.Errorf("failed to add Grove types to scheme: %w", err)
		}

		// Decode the object using Grove scheme
		decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
		obj, gvk, err := decoder.Decode(rawObj.Raw, nil, nil)
		if err != nil {
			return fmt.Errorf("failed to decode object: %w", err)
		}

		// Convert to unstructured for dynamic client
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("expected unstructured object, got %T", obj)
		}

		// Handle PodCliqueSet resources
		if gvk.Kind == "PodCliqueSet" && gvk.Group == "grove.io" {
			// Override namespace if specified
			if config.Namespace != "" {
				unstructuredObj.SetNamespace(config.Namespace)
			}
			if unstructuredObj.GetNamespace() == "" {
				unstructuredObj.SetNamespace("default")
			}

			logger.Infof("🔧 Applying PodCliqueSet: %s/%s", unstructuredObj.GetNamespace(), unstructuredObj.GetName())

			// Get REST mapping for PodCliqueSet
			gvr := schema.GroupVersionResource{
				Group:    "grove.io",
				Version:  "v1alpha1",
				Resource: "podcliquesets",
			}

			// Apply the resource using dynamic client
			result, err := dynamicClient.Resource(gvr).Namespace(unstructuredObj.GetNamespace()).Create(ctx, unstructuredObj, metav1.CreateOptions{})
			if err != nil {
				if errors.IsAlreadyExists(err) {
					// Try to update if it already exists
					result, err = dynamicClient.Resource(gvr).Namespace(unstructuredObj.GetNamespace()).Update(ctx, unstructuredObj, metav1.UpdateOptions{})
					if err != nil {
						return fmt.Errorf("failed to update PodCliqueSet %s: %w", unstructuredObj.GetName(), err)
					}
					logger.Infof("✅ Updated PodCliqueSet: %s/%s", result.GetNamespace(), result.GetName())
				} else {
					return fmt.Errorf("failed to create PodCliqueSet %s: %w", unstructuredObj.GetName(), err)
				}
			} else {
				logger.Infof("✅ Created PodCliqueSet: %s/%s", result.GetNamespace(), result.GetName())
			}

			appliedPodCliqueSets = append(appliedPodCliqueSets, AppliedPodCliqueSet{
				Name:      result.GetName(),
				Namespace: result.GetNamespace(),
			})
		} else {
			logger.Infof("⚠️  Skipping unsupported resource type: %s", gvk.Kind)
		}
	}

	if len(appliedPodCliqueSets) == 0 {
		return fmt.Errorf("no PodCliqueSet resources found in YAML file")
	}

	logger.Infof("📋 Applied %d PodCliqueSet resources, now waiting for pods to be ready...", len(appliedPodCliqueSets))

	// Wait for all pods to be ready
	if err := waitForPodCliqueSetPodsReady(ctx, config, appliedPodCliqueSets, logger); err != nil {
		return fmt.Errorf("failed waiting for pods to be ready: %w", err)
	}

	logger.Info("🎉 All pods are ready!")
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
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
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
				if checkWebhookReadiness(ctx, clientset, namespace, logger) {
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
func checkWebhookReadiness(ctx context.Context, clientset *kubernetes.Clientset, namespace string, logger *CILogger) bool {
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

	// All checks passed - webhook server should be ready
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
					logger.Infof("⏳ Pod %s/%s not ready yet (phase: %s)", pod.Namespace, pod.Name, pod.Status.Phase)
				}
			}
		}

		logger.Infof("📊 Pod status: %d/%d ready", readyPods, totalPods)

		if totalPods == 0 {
			logger.Info("⏳ No pods found yet, Grove operator may still be creating resources...")
			return false, nil
		}

		if !allReady {
			logger.Infof("⏳ Waiting for %d more pods to become ready...", totalPods-readyPods)
		}

		return allReady, nil
	})
}

// waitForRegularPods waits for regular pods (non-PodCliqueSet) to be ready
func waitForRegularPods(ctx context.Context, clientset *kubernetes.Clientset, namespace string, logger *CILogger) error {
	if namespace == "" {
		namespace = "default"
	}

	logger.Infof("⏳ Waiting for regular pods in namespace %s...", namespace)

	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			logger.Errorf("Failed to list pods: %v", err)
			return false, nil
		}

		if len(pods.Items) == 0 {
			logger.Info("⏳ No pods found yet, continuing to wait...")
			return false, nil
		}

		allReady := true
		readyCount := 0
		for _, pod := range pods.Items {
			if isPodReady(&pod) {
				readyCount++
			} else {
				allReady = false
				logger.Infof("⏳ Pod %s not ready yet (phase: %s)", pod.Name, pod.Status.Phase)
			}
		}

		logger.Infof("📊 Pod status: %d/%d ready", readyCount, len(pods.Items))
		return allReady, nil
	})
}

// isPodReady checks if a pod is ready
func isPodReady(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}
