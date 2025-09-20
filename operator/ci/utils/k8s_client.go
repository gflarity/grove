package utils

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// AppliedResource holds information about an applied Kubernetes resource
type AppliedResource struct {
	Name      string
	Namespace string
	GVK       schema.GroupVersionKind
	GVR       schema.GroupVersionResource
}

// AppliedPodCliqueSet holds information about an applied PodCliqueSet (for backward compatibility)
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
	// PodLabelSelector is the label selector to use when waiting for pods (optional)
	PodLabelSelector string
}

// ApplyYAML applies a YAML file containing Kubernetes resources
func ApplyYAML(ctx context.Context, config *WorkloadConfig, logger *CILogger) ([]AppliedResource, error) {
	logger.Infof("📄 Applying resources from %s...", config.YAMLFilePath)

	// Read the YAML file
	yamlData, err := os.ReadFile(config.YAMLFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file %s: %w", config.YAMLFilePath, err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config.RestConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create REST mapper for dynamic GVR discovery
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config.RestConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	cachedDiscoveryClient := memory.NewMemCacheClient(discoveryClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoveryClient)

	// Parse and apply YAML documents
	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(string(yamlData)), 4096)
	var appliedResources []AppliedResource

	for {
		var rawObj runtime.RawExtension
		if err := decoder.Decode(&rawObj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode YAML: %w", err)
		}

		if len(rawObj.Raw) == 0 {
			continue
		}

		// Decode the object as unstructured (no specific scheme needed)
		yamlDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
		obj, gvk, err := yamlDecoder.Decode(rawObj.Raw, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to decode object: %w", err)
		}

		// Convert to unstructured for dynamic client
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return nil, fmt.Errorf("expected unstructured object, got %T", obj)
		}

		// Override namespace if specified
		if config.Namespace != "" {
			unstructuredObj.SetNamespace(config.Namespace)
		}
		if unstructuredObj.GetNamespace() == "" {
			unstructuredObj.SetNamespace("default")
		}

		logger.Infof("🔧 Applying %s: %s/%s", gvk.Kind, unstructuredObj.GetNamespace(), unstructuredObj.GetName())

		// Get REST mapping dynamically from GVK
		gvr, err := getGVRFromGVK(restMapper, *gvk)
		if err != nil {
			return nil, fmt.Errorf("failed to get GVR for %s: %w", gvk.String(), err)
		}

		// Apply the resource using dynamic client (assuming namespaced resources)
		result, err := dynamicClient.Resource(gvr).Namespace(unstructuredObj.GetNamespace()).Create(ctx, unstructuredObj, metav1.CreateOptions{})
		if err != nil {
			if errors.IsAlreadyExists(err) {
				// Try to update if it already exists
				result, err = dynamicClient.Resource(gvr).Namespace(unstructuredObj.GetNamespace()).Update(ctx, unstructuredObj, metav1.UpdateOptions{})
				if err != nil {
					return nil, fmt.Errorf("failed to update %s %s: %w", gvk.Kind, unstructuredObj.GetName(), err)
				}
				logger.Infof("✅ Updated %s: %s/%s", gvk.Kind, result.GetNamespace(), result.GetName())
			} else {
				return nil, fmt.Errorf("failed to create %s %s: %w", gvk.Kind, unstructuredObj.GetName(), err)
			}
		} else {
			logger.Infof("✅ Created %s: %s/%s", gvk.Kind, result.GetNamespace(), result.GetName())
		}

		appliedResources = append(appliedResources, AppliedResource{
			Name:      result.GetName(),
			Namespace: result.GetNamespace(),
			GVK:       *gvk,
			GVR:       gvr,
		})
	}

	logger.Infof("📋 Applied %d resources successfully", len(appliedResources))
	return appliedResources, nil
}

// WaitForPods waits for pods to be ready in the specified namespaces
func WaitForPods(ctx context.Context, config *WorkloadConfig, namespaces []string, logger *CILogger) error {
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Minute
	}

	clientset, err := kubernetes.NewForConfig(config.RestConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	// If no namespaces specified, use default
	if len(namespaces) == 0 {
		namespaces = []string{"default"}
	}

	logger.Infof("⏳ Waiting for pods to be ready in namespaces: %v", namespaces)

	return wait.PollUntilContextTimeout(timeoutCtx, 5*time.Second, config.Timeout, true, func(ctx context.Context) (bool, error) {
		allReady := true
		totalPods := 0
		readyPods := 0

		for _, namespace := range namespaces {
			var labelSelector string
			if config.PodLabelSelector != "" {
				labelSelector = config.PodLabelSelector
			}

			pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil {
				logger.Errorf("Failed to list pods in namespace %s: %v", namespace, err)
				return false, nil
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
			logger.Info("⏳ No pods found yet, resources may still be creating pods...")
			return false, nil
		}

		if !allReady {
			logger.Infof("⏳ Waiting for %d more pods to become ready...", totalPods-readyPods)
		}

		return allReady, nil
	})
}

// ApplyYAMLAndWaitForPods applies a YAML file and waits for all pods to be ready (backward compatibility)
func ApplyYAMLAndWaitForPods(ctx context.Context, config *WorkloadConfig, logger *CILogger) error {
	// Check if this is a Grove workload that needs the operator ready
	needsGroveOperator := false
	if yamlData, err := os.ReadFile(config.YAMLFilePath); err == nil {
		needsGroveOperator = strings.Contains(string(yamlData), "grove.io")
	}

	if needsGroveOperator {
		if err := waitForGroveOperatorReady(ctx, config, logger); err != nil {
			return fmt.Errorf("grove operator not ready: %w", err)
		}
	}

	// Apply the YAML
	appliedResources, err := ApplyYAML(ctx, config, logger)
	if err != nil {
		return err
	}

	// Extract namespaces from applied resources
	namespaceSet := make(map[string]bool)
	var appliedPodCliqueSets []AppliedPodCliqueSet // For backward compatibility

	for _, resource := range appliedResources {
		if resource.Namespace != "" {
			namespaceSet[resource.Namespace] = true
		}
		// Maintain backward compatibility for PodCliqueSet resources
		if resource.GVK.Kind == "PodCliqueSet" && resource.GVK.Group == "grove.io" {
			appliedPodCliqueSets = append(appliedPodCliqueSets, AppliedPodCliqueSet{
				Name:      resource.Name,
				Namespace: resource.Namespace,
			})
		}
	}

	// Wait for pods if we have PodCliqueSet resources (for backward compatibility)
	if len(appliedPodCliqueSets) > 0 {
		logger.Infof("📋 Found %d PodCliqueSet resources, now waiting for pods to be ready...", len(appliedPodCliqueSets))
		if err := waitForPodCliqueSetPodsReady(ctx, config, appliedPodCliqueSets, logger); err != nil {
			return fmt.Errorf("failed waiting for pods to be ready: %w", err)
		}
		logger.Info("🎉 All pods are ready!")
	}

	return nil
}

// getGVRFromGVK converts a GroupVersionKind to GroupVersionResource using REST mapper
func getGVRFromGVK(restMapper meta.RESTMapper, gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	return mapping.Resource, nil
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
