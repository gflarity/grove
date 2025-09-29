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

// ApplyYAMLContent applies YAML content directly to Kubernetes
func ApplyYAMLContent(ctx context.Context, yamlContent string, namespace string, restConfig *rest.Config, logger *CILogger) ([]AppliedResource, error) {
	logger.Debug("📄 Applying YAML content...")
	return applyYAMLData(ctx, []byte(yamlContent), namespace, restConfig, logger)
}

// ApplyYAML applies a YAML file containing Kubernetes resources
func ApplyYAML(ctx context.Context, config *WorkloadConfig, logger *CILogger) ([]AppliedResource, error) {
	logger.Debugf("📄 Applying resources from %s...\n", config.YAMLFilePath)

	// Read the YAML file
	yamlData, err := os.ReadFile(config.YAMLFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file %s: %w", config.YAMLFilePath, err)
	}

	return applyYAMLData(ctx, yamlData, config.Namespace, config.RestConfig, logger)
}

// applyYAMLData is the common function that applies YAML data to Kubernetes
func applyYAMLData(ctx context.Context, yamlData []byte, namespace string, restConfig *rest.Config, logger *CILogger) ([]AppliedResource, error) {
	dynamicClient, restMapper, err := createKubernetesClients(restConfig)
	if err != nil {
		return nil, err
	}

	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(string(yamlData)), 4096)
	var appliedResources []AppliedResource

	for {
		unstructuredObj, gvk, err := decodeNextYAMLObject(decoder)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if unstructuredObj == nil {
			continue // Skip empty objects
		}

		// Apply the resource
		appliedResource, err := applyResource(ctx, dynamicClient, restMapper, unstructuredObj, gvk, namespace)
		if err != nil {
			return nil, err
		}

		appliedResources = append(appliedResources, *appliedResource)
	}

	logger.Debugf("📋 Applied %d resources successfully", len(appliedResources))
	return appliedResources, nil
}

// createKubernetesClients creates the dynamic client and REST mapper
func createKubernetesClients(restConfig *rest.Config) (dynamic.Interface, meta.RESTMapper, error) {
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	cachedDiscoveryClient := memory.NewMemCacheClient(discoveryClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoveryClient)

	return dynamicClient, restMapper, nil
}

// decodeNextYAMLObject decodes the next YAML object from the decoder
func decodeNextYAMLObject(decoder *yamlutil.YAMLOrJSONDecoder) (*unstructured.Unstructured, *schema.GroupVersionKind, error) {
	var rawObj runtime.RawExtension
	if err := decoder.Decode(&rawObj); err != nil {
		return nil, nil, err
	}

	if len(rawObj.Raw) == 0 {
		return nil, nil, nil // Empty object
	}

	// Decode the object as unstructured
	yamlDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj, gvk, err := yamlDecoder.Decode(rawObj.Raw, nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode object: %w", err)
	}

	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, nil, fmt.Errorf("expected unstructured object, got %T", obj)
	}

	return unstructuredObj, gvk, nil
}

// applyResource applies a single Kubernetes resource
func applyResource(ctx context.Context, dynamicClient dynamic.Interface, restMapper meta.RESTMapper, obj *unstructured.Unstructured, gvk *schema.GroupVersionKind, namespace string) (*AppliedResource, error) {
	// Get resource mapping
	gvr, mapping, err := getResourceMapping(restMapper, gvk)
	if err != nil {
		return nil, err
	}

	// Handle namespace based on resource scope
	handleResourceNamespace(obj, mapping, namespace)

	// Apply the resource (create or update)
	result, err := createOrUpdateResource(ctx, dynamicClient, gvr, mapping, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to apply %s %s: %w", gvk.Kind, obj.GetName(), err)
	}

	return &AppliedResource{
		Name:      result.GetName(),
		Namespace: result.GetNamespace(),
		GVK:       *gvk,
		GVR:       gvr,
	}, nil
}

// getResourceMapping gets the GVR and mapping for a resource
func getResourceMapping(restMapper meta.RESTMapper, gvk *schema.GroupVersionKind) (schema.GroupVersionResource, *meta.RESTMapping, error) {
	gvr, err := getGVRFromGVK(restMapper, *gvk)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to get GVR for %s: %w", gvk.String(), err)
	}

	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
	}

	return gvr, mapping, nil
}

// handleResourceNamespace sets the appropriate namespace based on resource scope
func handleResourceNamespace(obj *unstructured.Unstructured, mapping *meta.RESTMapping, namespace string) {
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		// Namespaced resource
		if namespace != "" {
			obj.SetNamespace(namespace)
		}
		if obj.GetNamespace() == "" {
			obj.SetNamespace("default")
		}
	} else {
		// Cluster-scoped resource - clear any namespace
		obj.SetNamespace("")
	}
}

// logResourceApplication logs what resource is being applied
func logResourceApplication(obj *unstructured.Unstructured, gvk *schema.GroupVersionKind, logger *CILogger) {
	if obj.GetNamespace() != "" {
		logger.Infof("🔧 Applying %s: %s/%s", gvk.Kind, obj.GetNamespace(), obj.GetName())
	} else {
		logger.Infof("🔧 Applying %s: %s", gvk.Kind, obj.GetName())
	}
}

// createOrUpdateResource creates or updates a resource
func createOrUpdateResource(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, mapping *meta.RESTMapping, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	// Try to create first
	result, err := createResource(ctx, dynamicClient, gvr, mapping, obj)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Resource exists, try to update
			return updateResource(ctx, dynamicClient, gvr, mapping, obj)
		}
		return nil, err
	}
	return result, nil
}

// createResource creates a new resource
func createResource(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, mapping *meta.RESTMapping, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return dynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Create(ctx, obj, metav1.CreateOptions{})
	}
	return dynamicClient.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
}

// updateResource updates an existing resource
func updateResource(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, mapping *meta.RESTMapping, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return dynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{})
	}
	return dynamicClient.Resource(gvr).Update(ctx, obj, metav1.UpdateOptions{})
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

	logger.Debugf("⏳ Waiting for pods to be ready in namespaces: %v", namespaces)

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

		if totalPods == 0 {
			logger.Debug("⏳ No pods found yet, resources may still be creating pods...")
			return false, nil
		}

		if !allReady {
			logger.Debugf("⏳ Waiting for %d more pods to become ready...", totalPods-readyPods)
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
		logger.Debugf("📋 Found %d PodCliqueSet resources, now waiting for pods to be ready...", len(appliedPodCliqueSets))
		if err := waitForPodCliqueSetPodsReady(ctx, config, appliedPodCliqueSets, logger); err != nil {
			return fmt.Errorf("failed waiting for pods to be ready: %w", err)
		}
		logger.Debugf("🎉 All pods are ready!")
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

		return allReady, nil
	})
}

// WaitForPodsInNamespace waits for all pods in a namespace to be ready
func WaitForPodsInNamespace(ctx context.Context, namespace string, restConfig *rest.Config, timeout time.Duration, logger *CILogger) error {
	workloadConfig := &WorkloadConfig{
		RestConfig: restConfig,
		Timeout:    timeout,
	}

	return WaitForPods(ctx, workloadConfig, []string{namespace}, logger)
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
