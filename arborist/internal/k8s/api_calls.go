// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package k8s

import (
	"context"
	"fmt"
	"io"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// APIResourceFetcher performs on-demand Kubernetes API calls for resource
// details (YAML, containers, logs). It holds only the clients needed for
// direct API access and never touches the informer cache.
type APIResourceFetcher struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
}

// GetPodYAML fetches a Pod and returns it as YAML string.
func (f *APIResourceFetcher) GetPodYAML(ctx context.Context, podName, namespace string) (string, error) {
	pod, err := f.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get Pod: %w", err)
	}

	yamlData, err := yaml.Marshal(pod)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Pod to YAML: %w", err)
	}

	return string(yamlData), nil
}

// GetPodContainers fetches a Pod and returns container info for display.
func (f *APIResourceFetcher) GetPodContainers(ctx context.Context, podName, namespace string) ([]clusterstate.ContainerInfo, error) {
	pod, err := f.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Pod: %w", err)
	}

	// Build a map of container statuses for quick lookup
	statusMap := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		statusMap[cs.Name] = cs
	}

	containers := make([]clusterstate.ContainerInfo, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		info := clusterstate.ContainerInfo{
			Name:  c.Name,
			Image: c.Image,
		}

		if cs, ok := statusMap[c.Name]; ok {
			info.Ready = cs.Ready
			info.RestartCount = cs.RestartCount
			switch {
			case cs.State.Running != nil:
				info.State = "Running"
			case cs.State.Waiting != nil:
				info.State = "Waiting"
			case cs.State.Terminated != nil:
				info.State = "Terminated"
			default:
				info.State = "Unknown"
			}
		} else {
			info.State = "Waiting"
		}

		containers = append(containers, info)
	}

	return containers, nil
}

// GetPodLogs fetches the last tailLines of logs for a specific container in a pod.
func (f *APIResourceFetcher) GetPodLogs(ctx context.Context, podName, namespace, container string, tailLines int64) (string, error) {
	opts := &corev1.PodLogOptions{
		Container: container,
		TailLines: &tailLines,
	}
	stream, err := f.clientset.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs for %s/%s: %w", podName, container, err)
	}
	defer stream.Close()

	logBytes, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("failed to read logs for %s/%s: %w", podName, container, err)
	}
	return string(logBytes), nil
}

// resourceGVRs maps resource types to their GroupVersionResource for dynamic API access.
var resourceGVRs = map[string]schema.GroupVersionResource{
	clusterstate.ResourceTypePodCliqueSet: globalPcsGVR,
	clusterstate.ResourceTypePCSG:         globalPcsgGVR,
	clusterstate.ResourceTypePodClique:    globalPcGVR,
}

// GetResourceYAML fetches any resource's YAML by type and name.
// For CRDs it uses the dynamic client; for Pods it delegates to GetPodYAML.
func (f *APIResourceFetcher) GetResourceYAML(ctx context.Context, resourceType, name, namespace string) (string, error) {
	if resourceType == clusterstate.ResourceTypePod {
		return f.GetPodYAML(ctx, name, namespace)
	}
	if gvr, ok := resourceGVRs[resourceType]; ok {
		return f.getDynamicResourceYAML(ctx, gvr, name, namespace)
	}
	return "", fmt.Errorf("unsupported resource type: %s", resourceType)
}

// getDynamicResourceYAML fetches a CRD resource via the dynamic client and returns YAML.
func (f *APIResourceFetcher) getDynamicResourceYAML(ctx context.Context, gvr schema.GroupVersionResource, name, namespace string) (string, error) {
	var obj *unstructured.Unstructured
	var err error

	if namespace != "" {
		obj, err = f.dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = f.dynamicClient.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return "", fmt.Errorf("failed to get %s/%s: %w", gvr.Resource, name, err)
	}

	yamlData, err := yaml.Marshal(obj.Object)
	if err != nil {
		return "", fmt.Errorf("failed to marshal %s/%s to YAML: %w", gvr.Resource, name, err)
	}

	return string(yamlData), nil
}
