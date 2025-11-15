package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

// Resource represents a generic resource item
type Resource struct {
	Name       string
	Type       string
	Ready      string
	Scheduled  string // Scheduled replicas (for PodClique/PodCliqueScalingGroup) or Phase (for Pods)
	Status     string
	Namespace  string
	ParentType string
	ParentName string
	YAML       string // For Pod detail view
}

// Event represents a Kubernetes event
type Event struct {
	Type      string    // Normal, Warning, Error
	Reason    string    // The reason for the event
	Age       string    // How long ago
	From      string    // Component that generated the event
	Message   string    // Detailed message
	Parent    string    // Parent resource name for filtering
	Timestamp time.Time // Event timestamp for sorting
}

// K8sClient wraps the Kubernetes clients
type K8sClient struct {
	clientset     *kubernetes.Clientset
	dynamicClient dynamic.Interface
	restConfig    *rest.Config
}

// NewK8sClient creates a new Kubernetes client using the default kubeconfig
func NewK8sClient() (*K8sClient, error) {
	// Try to use in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &K8sClient{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		restConfig:    config,
	}, nil
}

// GetAllPodCliqueSets fetches all PodCliqueSet resources from all namespaces
func (k *K8sClient) GetAllPodCliqueSets(ctx context.Context) ([]Resource, error) {
	gvr := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquesets",
	}

	unstructuredList, err := k.dynamicClient.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list PodCliqueSets: %w", err)
	}

	resources := make([]Resource, 0, len(unstructuredList.Items))
	for _, item := range unstructuredList.Items {
		// Convert unstructured to PodCliqueSet
		var pcs corev1alpha1.PodCliqueSet
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &pcs)
		if err != nil {
			// Skip items that can't be converted
			continue
		}

		// Calculate ready status
		replicas := pcs.Spec.Replicas
		availableReplicas := pcs.Status.AvailableReplicas
		ready := fmt.Sprintf("%d/%d", availableReplicas, replicas)

		resources = append(resources, Resource{
			Name:      pcs.Name,
			Type:      "PodCliqueSet",
			Ready:     ready,
			Status:    "", // Scheduled - leave empty for now
			Namespace: pcs.Namespace,
		})
	}

	return resources, nil
}

// GetEventsForPodCliqueSetReplica fetches events for resources related to a specific replica of a PodCliqueSet
func (k *K8sClient) GetEventsForPodCliqueSetReplica(ctx context.Context, podCliqueSetName, namespace, replicaIndex string) ([]Event, error) {
	// Find all resources with the label app.kubernetes.io/part-of: <podCliqueSetName> and grove.io/podcliqueset-replica-index: <replicaIndex>
	labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s,grove.io/podcliqueset-replica-index=%s", podCliqueSetName, replicaIndex)
	
	// Collect all resource names by kind
	resourceNamesByKind := make(map[string]map[string]bool)
	resourceNamesByKind["PodCliqueScalingGroup"] = make(map[string]bool)
	resourceNamesByKind["PodClique"] = make(map[string]bool)
	resourceNamesByKind["Pod"] = make(map[string]bool)
	
	// Query PodCliqueScalingGroups
	pcsgGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquescalinggroups",
	}
	pcsgList, err := k.dynamicClient.Resource(pcsgGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, item := range pcsgList.Items {
			resourceNamesByKind["PodCliqueScalingGroup"][item.GetName()] = true
		}
	}

	// Query PodCliques
	pcGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}
	pcList, err := k.dynamicClient.Resource(pcGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, item := range pcList.Items {
			resourceNamesByKind["PodClique"][item.GetName()] = true
		}
	}

	// Query Pods
	pods, err := k.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, pod := range pods.Items {
			resourceNamesByKind["Pod"][pod.Name] = true
		}
	}

	// Fetch all events in the namespace
	events, err := k.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	// Filter events that are related to the resources we found
	var filteredEvents []Event
	for _, event := range events.Items {
		kind := event.InvolvedObject.Kind
		name := event.InvolvedObject.Name
		
		// Check if this event belongs to one of our resources
		if namesMap, ok := resourceNamesByKind[kind]; ok {
			if namesMap[name] {
				filteredEvents = append(filteredEvents, convertK8sEventToEvent(event))
			}
		}
	}

	// Sort events by timestamp (newest first)
	sort.Slice(filteredEvents, func(i, j int) bool {
		return filteredEvents[i].Timestamp.After(filteredEvents[j].Timestamp)
	})

	return filteredEvents, nil
}

// GetEventsForPodCliqueSet fetches events for resources related to a PodCliqueSet
// It queries for events related to PodCliqueSet, PodCliqueScalingGroup, PodClique, and Pod resources
// that belong to the specified PodCliqueSet
func (k *K8sClient) GetEventsForPodCliqueSet(ctx context.Context, podCliqueSetName, namespace string) ([]Event, error) {
	// First, find all resources with the label app.kubernetes.io/part-of: <podCliqueSetName>
	labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s", podCliqueSetName)
	
	// Collect all resource names by kind
	resourceNamesByKind := make(map[string]map[string]bool)
	resourceNamesByKind["PodCliqueSet"] = make(map[string]bool)
	resourceNamesByKind["PodCliqueScalingGroup"] = make(map[string]bool)
	resourceNamesByKind["PodClique"] = make(map[string]bool)
	resourceNamesByKind["Pod"] = make(map[string]bool)
	
	// Add the PodCliqueSet itself
	resourceNamesByKind["PodCliqueSet"][podCliqueSetName] = true
	
	// Query PodCliqueScalingGroups
	pcsgGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquescalinggroups",
	}
	pcsgList, err := k.dynamicClient.Resource(pcsgGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, item := range pcsgList.Items {
			resourceNamesByKind["PodCliqueScalingGroup"][item.GetName()] = true
		}
	}

	// Query PodCliques
	pcGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}
	pcList, err := k.dynamicClient.Resource(pcGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, item := range pcList.Items {
			resourceNamesByKind["PodClique"][item.GetName()] = true
		}
	}

	// Query Pods
	pods, err := k.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, pod := range pods.Items {
			resourceNamesByKind["Pod"][pod.Name] = true
		}
	}

	// Fetch all events in the namespace
	events, err := k.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	// Filter events that are related to the resources we found
	var filteredEvents []Event
	for _, event := range events.Items {
		kind := event.InvolvedObject.Kind
		name := event.InvolvedObject.Name
		
		// Check if this event belongs to one of our resources
		if namesMap, ok := resourceNamesByKind[kind]; ok {
			if namesMap[name] {
				filteredEvents = append(filteredEvents, convertK8sEventToEvent(event))
			}
		}
	}

	// Sort events by timestamp (newest first)
	sort.Slice(filteredEvents, func(i, j int) bool {
		return filteredEvents[i].Timestamp.After(filteredEvents[j].Timestamp)
	})

	return filteredEvents, nil
}

// GetEventsForResource fetches events for a specific resource
func (k *K8sClient) GetEventsForResource(ctx context.Context, resourceName, namespace string) ([]Event, error) {
	fieldSelector := fmt.Sprintf("involvedObject.name=%s", resourceName)
	
	events, err := k.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list events for resource %s: %w", resourceName, err)
	}

	var result []Event
	for _, event := range events.Items {
		result = append(result, convertK8sEventToEvent(event))
	}

	// Sort events by timestamp (newest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})

	return result, nil
}

// convertK8sEventToEvent converts a Kubernetes Event to our Event type
func convertK8sEventToEvent(k8sEvent corev1.Event) Event {
	// Calculate age
	age := formatAge(k8sEvent.LastTimestamp.Time)
	if k8sEvent.LastTimestamp.IsZero() {
		age = formatAge(k8sEvent.EventTime.Time)
	}

	return Event{
		Type:      k8sEvent.Type,
		Reason:    k8sEvent.Reason,
		Age:       age,
		From:      k8sEvent.Source.Component,
		Message:   k8sEvent.Message,
		Parent:    k8sEvent.InvolvedObject.Name,
		Timestamp: k8sEvent.LastTimestamp.Time,
	}
}

// formatAge formats a time duration into a human-readable age string
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	
	duration := time.Since(t)
	
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	} else {
		return fmt.Sprintf("%dd", int(duration.Hours()/24))
	}
}

// GetReplicaIndexesForPodCliqueSet fetches all unique replica indexes for a PodCliqueSet
// Returns a slice of replica index strings (e.g., ["0", "1", "2"])
func (k *K8sClient) GetReplicaIndexesForPodCliqueSet(ctx context.Context, pcsName, namespace string) ([]string, error) {
	labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s", pcsName)
	
	replicaIndexes := make(map[string]bool)
	
	// Query PodCliqueScalingGroups
	pcsgGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquescalinggroups",
	}
	pcsgList, err := k.dynamicClient.Resource(pcsgGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, item := range pcsgList.Items {
			labels := item.GetLabels()
			if replicaIndex, ok := labels["grove.io/podcliqueset-replica-index"]; ok {
				replicaIndexes[replicaIndex] = true
			}
		}
	}

	// Query PodCliques
	pcGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}
	pcList, err := k.dynamicClient.Resource(pcGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, item := range pcList.Items {
			labels := item.GetLabels()
			if replicaIndex, ok := labels["grove.io/podcliqueset-replica-index"]; ok {
				replicaIndexes[replicaIndex] = true
			}
		}
	}

	// Convert to sorted slice
	result := make([]string, 0, len(replicaIndexes))
	for index := range replicaIndexes {
		result = append(result, index)
	}
	sort.Strings(result)
	
	return result, nil
}

// GetPodCliqueScalingGroupsForPodCliqueSetReplica fetches all PodCliqueScalingGroups for a specific replica of a PodCliqueSet
func (k *K8sClient) GetPodCliqueScalingGroupsForPodCliqueSetReplica(ctx context.Context, pcsName, namespace, replicaIndex string) ([]Resource, error) {
	// Use multiple labels to filter by both PodCliqueSet and replica index
	labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s,grove.io/podcliqueset-replica-index=%s", pcsName, replicaIndex)
	
	gvr := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquescalinggroups",
	}
	
	unstructuredList, err := k.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list PodCliqueScalingGroups: %w", err)
	}

	var resources []Resource
	for _, item := range unstructuredList.Items {
		var pcsg corev1alpha1.PodCliqueScalingGroup
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &pcsg)
		if err != nil {
			continue
		}

		replicas := pcsg.Status.Replicas
		availableReplicas := pcsg.Status.AvailableReplicas
		scheduledReplicas := pcsg.Status.ScheduledReplicas
		
		ready := fmt.Sprintf("%d/%d", availableReplicas, replicas)
		scheduled := fmt.Sprintf("%d/%d", scheduledReplicas, replicas)

		resources = append(resources, Resource{
			Name:       pcsg.Name,
			Type:       "PodCliqueScalingGroup",
			Ready:      ready,
			Scheduled:  scheduled,
			Status:     "",
			Namespace:  pcsg.Namespace,
			ParentType: "PodCliqueSetReplica",
			ParentName: fmt.Sprintf("%s-replica-%s", pcsName, replicaIndex),
		})
	}

	return resources, nil
}

// GetPodCliquesForPodCliqueSetReplica fetches standalone PodCliques for a specific replica of a PodCliqueSet (not part of scaling groups)
func (k *K8sClient) GetPodCliquesForPodCliqueSetReplica(ctx context.Context, pcsName, namespace, replicaIndex string) ([]Resource, error) {
	// Get PodCliques with the label app.kubernetes.io/part-of=<pcsName> and grove.io/podcliqueset-replica-index=<replicaIndex>
	// and without the label that indicates they're part of a scaling group
	labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s,grove.io/podcliqueset-replica-index=%s", pcsName, replicaIndex)
	
	gvr := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}
	
	unstructuredList, err := k.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list PodCliques: %w", err)
	}

	var resources []Resource
	for _, item := range unstructuredList.Items {
		// Skip PodCliques that are part of a PodCliqueScalingGroup
		// (they have the label app.kubernetes.io/managed-by or similar)
		labels := item.GetLabels()
		if _, hasScalingGroup := labels["grove.io/podcliquescalinggroup"]; hasScalingGroup {
			continue
		}

		var pc corev1alpha1.PodClique
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &pc)
		if err != nil {
			continue
		}

		replicas := pc.Spec.Replicas
		readyReplicas := pc.Status.ReadyReplicas
		scheduledReplicas := pc.Status.ScheduledReplicas
		
		ready := fmt.Sprintf("%d/%d", readyReplicas, replicas)
		scheduled := fmt.Sprintf("%d/%d", scheduledReplicas, replicas)

		resources = append(resources, Resource{
			Name:       pc.Name,
			Type:       "PodClique",
			Ready:      ready,
			Scheduled:  scheduled,
			Status:     "",
			Namespace:  pc.Namespace,
			ParentType: "PodCliqueSetReplica",
			ParentName: fmt.Sprintf("%s-replica-%s", pcsName, replicaIndex),
		})
	}

	return resources, nil
}

// GetPodCliquesForPodCliqueScalingGroup fetches PodCliques that belong to a specific PodCliqueScalingGroup
func (k *K8sClient) GetPodCliquesForPodCliqueScalingGroup(ctx context.Context, pcsgName, namespace string) ([]Resource, error) {
	// Get PodCliques with the label grove.io/podcliquescalinggroup=<pcsgName>
	labelSelector := fmt.Sprintf("grove.io/podcliquescalinggroup=%s", pcsgName)
	
	gvr := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}
	
	unstructuredList, err := k.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list PodCliques for PodCliqueScalingGroup: %w", err)
	}

	var resources []Resource
	for _, item := range unstructuredList.Items {
		var pc corev1alpha1.PodClique
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &pc)
		if err != nil {
			continue
		}

		replicas := pc.Spec.Replicas
		readyReplicas := pc.Status.ReadyReplicas
		scheduledReplicas := pc.Status.ScheduledReplicas
		
		ready := fmt.Sprintf("%d/%d", readyReplicas, replicas)
		scheduled := fmt.Sprintf("%d/%d", scheduledReplicas, replicas)

		resources = append(resources, Resource{
			Name:       pc.Name,
			Type:       "PodClique",
			Ready:      ready,
			Scheduled:  scheduled,
			Status:     "",
			Namespace:  pc.Namespace,
			ParentType: "PodCliqueScalingGroup",
			ParentName: pcsgName,
		})
	}

	return resources, nil
}

// GetEventsForPodCliqueScalingGroup fetches events for resources related to a PodCliqueScalingGroup
func (k *K8sClient) GetEventsForPodCliqueScalingGroup(ctx context.Context, pcsgName, namespace string) ([]Event, error) {
	// Get PodCliques with the label grove.io/podcliquescalinggroup=<pcsgName>
	labelSelector := fmt.Sprintf("grove.io/podcliquescalinggroup=%s", pcsgName)
	
	// Collect all resource names by kind
	resourceNamesByKind := make(map[string]map[string]bool)
	resourceNamesByKind["PodCliqueScalingGroup"] = make(map[string]bool)
	resourceNamesByKind["PodClique"] = make(map[string]bool)
	resourceNamesByKind["Pod"] = make(map[string]bool)
	
	// Add the PodCliqueScalingGroup itself
	resourceNamesByKind["PodCliqueScalingGroup"][pcsgName] = true
	
	// Query PodCliques
	pcGVR := schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}
	pcList, err := k.dynamicClient.Resource(pcGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, item := range pcList.Items {
			resourceNamesByKind["PodClique"][item.GetName()] = true
		}
	}

	// Query Pods with the same label
	pods, err := k.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, pod := range pods.Items {
			resourceNamesByKind["Pod"][pod.Name] = true
		}
	}

	// Fetch all events in the namespace
	events, err := k.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	// Filter events that are related to the resources we found
	var filteredEvents []Event
	for _, event := range events.Items {
		kind := event.InvolvedObject.Kind
		name := event.InvolvedObject.Name
		
		// Check if this event belongs to one of our resources
		if namesMap, ok := resourceNamesByKind[kind]; ok {
			if namesMap[name] {
				filteredEvents = append(filteredEvents, convertK8sEventToEvent(event))
			}
		}
	}

	// Sort events by timestamp (newest first)
	sort.Slice(filteredEvents, func(i, j int) bool {
		return filteredEvents[i].Timestamp.After(filteredEvents[j].Timestamp)
	})

	return filteredEvents, nil
}

// GetPodsForPodClique fetches Pods that belong to a specific PodClique
func (k *K8sClient) GetPodsForPodClique(ctx context.Context, podCliqueName, namespace string) ([]Resource, error) {
	// Get Pods with the label grove.io/podclique=<podCliqueName>
	labelSelector := fmt.Sprintf("grove.io/podclique=%s", podCliqueName)
	
	pods, err := k.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Pods for PodClique: %w", err)
	}

	var resources []Resource
	for _, pod := range pods.Items {
		// Determine ready status
		ready := "0/1"
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = "1/1"
				break
			}
		}
		
		// Get pod phase for status
		phase := string(pod.Status.Phase)

		resources = append(resources, Resource{
			Name:       pod.Name,
			Type:       "Pod",
			Ready:      ready,
			Scheduled:  phase, // Pod phase (Running, Pending, etc.)
			Status:     phase,
			Namespace:  pod.Namespace,
			ParentType: "PodClique",
			ParentName: podCliqueName,
		})
	}

	return resources, nil
}

// GetEventsForPodClique fetches events for resources related to a PodClique
func (k *K8sClient) GetEventsForPodClique(ctx context.Context, podCliqueName, namespace string) ([]Event, error) {
	// Get Pods with the label grove.io/podclique=<podCliqueName>
	labelSelector := fmt.Sprintf("grove.io/podclique=%s", podCliqueName)
	
	// Collect all resource names by kind
	resourceNamesByKind := make(map[string]map[string]bool)
	resourceNamesByKind["PodClique"] = make(map[string]bool)
	resourceNamesByKind["Pod"] = make(map[string]bool)
	
	// Add the PodClique itself
	resourceNamesByKind["PodClique"][podCliqueName] = true
	
	// Query Pods
	pods, err := k.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, pod := range pods.Items {
			resourceNamesByKind["Pod"][pod.Name] = true
		}
	}

	// Fetch all events in the namespace
	events, err := k.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	// Filter events that are related to the resources we found
	var filteredEvents []Event
	for _, event := range events.Items {
		kind := event.InvolvedObject.Kind
		name := event.InvolvedObject.Name
		
		// Check if this event belongs to one of our resources
		if namesMap, ok := resourceNamesByKind[kind]; ok {
			if namesMap[name] {
				filteredEvents = append(filteredEvents, convertK8sEventToEvent(event))
			}
		}
	}

	// Sort events by timestamp (newest first)
	sort.Slice(filteredEvents, func(i, j int) bool {
		return filteredEvents[i].Timestamp.After(filteredEvents[j].Timestamp)
	})

	return filteredEvents, nil
}

// GetPodYAML fetches a Pod and returns it as YAML string
func (k *K8sClient) GetPodYAML(ctx context.Context, podName, namespace string) (string, error) {
	// Get the Pod
	pod, err := k.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get Pod: %w", err)
	}

	// Convert to YAML
	yamlData, err := yaml.Marshal(pod)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Pod to YAML: %w", err)
	}

	return string(yamlData), nil
}
