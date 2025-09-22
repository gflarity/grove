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
	logger.Infof("📄 Applying YAML content...")

	return applyYAMLData(ctx, []byte(yamlContent), namespace, restConfig, logger)
}

// ApplyYAML applies a YAML file containing Kubernetes resources
func ApplyYAML(ctx context.Context, config *WorkloadConfig, logger *CILogger) ([]AppliedResource, error) {
	logger.Infof("📄 Applying resources from %s...", config.YAMLFilePath)

	// Read the YAML file
	yamlData, err := os.ReadFile(config.YAMLFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file %s: %w", config.YAMLFilePath, err)
	}

	return applyYAMLData(ctx, yamlData, config.Namespace, config.RestConfig, logger)
}

// applyYAMLData is the common function that applies YAML data to Kubernetes
func applyYAMLData(ctx context.Context, yamlData []byte, namespace string, restConfig *rest.Config, logger *CILogger) ([]AppliedResource, error) {

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create REST mapper for dynamic GVR discovery
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
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
		if namespace != "" {
			unstructuredObj.SetNamespace(namespace)
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
