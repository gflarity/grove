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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func TestTransformPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
				"grove.io/podclique":        "worker",
			},
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": "big-json-blob",
			},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "kubectl", Operation: metav1.ManagedFieldsOperationApply},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{
					Name:    "main",
					Image:   "nvcr.io/nvidia/tritonserver:latest",
					Command: []string{"/bin/tritonserver"},
					Args:    []string{"--model-repository=/models"},
					Env: []corev1.EnvVar{
						{Name: "SECRET_KEY", Value: "supersecret"},
						{Name: "CONFIG", Value: "large-config-value"},
					},
					EnvFrom: []corev1.EnvFromSource{
						{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}}},
					},
					Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("8"),
							corev1.ResourceCPU:                    resource.MustParse("4"),
						},
					},
					VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
					LivenessProbe:   &corev1.Probe{InitialDelaySeconds: 10},
					ReadinessProbe:  &corev1.Probe{InitialDelaySeconds: 5},
					StartupProbe:    &corev1.Probe{InitialDelaySeconds: 0},
					SecurityContext: &corev1.SecurityContext{Privileged: boolPtr(true)},
				},
			},
			InitContainers: []corev1.Container{
				{Name: "init", Image: "busybox"},
			},
			Volumes: []corev1.Volume{
				{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "main", Ready: true, RestartCount: 0},
			},
			PodIP:  "10.0.0.1",
			HostIP: "192.168.1.1",
		},
	}

	result, err := transformPod(pod)
	if err != nil {
		t.Fatalf("transformPod returned error: %v", err)
	}
	transformed := result.(*corev1.Pod)

	// Kept fields
	if transformed.Name != "my-pod" {
		t.Errorf("Name = %q, want %q", transformed.Name, "my-pod")
	}
	if transformed.Namespace != "default" {
		t.Errorf("Namespace = %q, want %q", transformed.Namespace, "default")
	}
	if transformed.Labels["app.kubernetes.io/part-of"] != "my-pcs" {
		t.Error("Labels should be preserved")
	}
	if transformed.Spec.NodeName != "node-1" {
		t.Errorf("NodeName = %q, want %q", transformed.Spec.NodeName, "node-1")
	}
	if transformed.Status.Phase != corev1.PodRunning {
		t.Errorf("Phase = %v, want Running", transformed.Status.Phase)
	}

	// GPU requests preserved
	gpuQty := transformed.Spec.Containers[0].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]
	if gpuQty.Value() != 8 {
		t.Errorf("GPU requests = %d, want 8", gpuQty.Value())
	}

	// Stripped fields
	if transformed.ManagedFields != nil {
		t.Error("ManagedFields should be nil")
	}
	if transformed.Annotations != nil {
		t.Error("Annotations should be nil")
	}
	if transformed.Spec.InitContainers != nil {
		t.Error("InitContainers should be nil")
	}
	if transformed.Spec.Volumes != nil {
		t.Error("Volumes should be nil")
	}

	c := &transformed.Spec.Containers[0]
	if c.Env != nil {
		t.Error("Container Env should be nil")
	}
	if c.EnvFrom != nil {
		t.Error("Container EnvFrom should be nil")
	}
	if c.VolumeMounts != nil {
		t.Error("Container VolumeMounts should be nil")
	}
	if c.Command != nil {
		t.Error("Container Command should be nil")
	}
	if c.Args != nil {
		t.Error("Container Args should be nil")
	}
	if c.Image != "" {
		t.Error("Container Image should be empty")
	}
	if c.Ports != nil {
		t.Error("Container Ports should be nil")
	}
	if c.LivenessProbe != nil {
		t.Error("Container LivenessProbe should be nil")
	}
	if c.ReadinessProbe != nil {
		t.Error("Container ReadinessProbe should be nil")
	}
	if c.StartupProbe != nil {
		t.Error("Container StartupProbe should be nil")
	}
	if c.SecurityContext != nil {
		t.Error("Container SecurityContext should be nil")
	}

	// Status subfields stripped
	if transformed.Status.Conditions != nil {
		t.Error("Status.Conditions should be nil")
	}
	if transformed.Status.ContainerStatuses != nil {
		t.Error("Status.ContainerStatuses should be nil")
	}
	if transformed.Status.PodIP != "" {
		t.Error("Status.PodIP should be empty")
	}
}

func TestTransformPod_MultipleContainers(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "multi-container", Namespace: "default"},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{
					Name:    "inference",
					Image:   "nvcr.io/nvidia/tritonserver:latest",
					Command: []string{"/bin/tritonserver"},
					Env:     []corev1.EnvVar{{Name: "MODEL", Value: "llama"}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("4"),
						},
					},
					LivenessProbe:   &corev1.Probe{InitialDelaySeconds: 10},
					SecurityContext: &corev1.SecurityContext{Privileged: boolPtr(true)},
				},
				{
					Name:         "sidecar",
					Image:        "nvcr.io/nvidia/cuda:latest",
					Args:         []string{"--worker"},
					EnvFrom:      []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}}}},
					VolumeMounts: []corev1.VolumeMount{{Name: "shm", MountPath: "/dev/shm"}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("8"),
						},
					},
					ReadinessProbe: &corev1.Probe{InitialDelaySeconds: 5},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	result, err := transformPod(pod)
	if err != nil {
		t.Fatalf("transformPod returned error: %v", err)
	}
	transformed := result.(*corev1.Pod)

	if len(transformed.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(transformed.Spec.Containers))
	}

	// Both containers stripped
	for i, c := range transformed.Spec.Containers {
		if c.Env != nil {
			t.Errorf("container[%d] Env should be nil", i)
		}
		if c.EnvFrom != nil {
			t.Errorf("container[%d] EnvFrom should be nil", i)
		}
		if c.Command != nil {
			t.Errorf("container[%d] Command should be nil", i)
		}
		if c.Args != nil {
			t.Errorf("container[%d] Args should be nil", i)
		}
		if c.Image != "" {
			t.Errorf("container[%d] Image should be empty", i)
		}
		if c.VolumeMounts != nil {
			t.Errorf("container[%d] VolumeMounts should be nil", i)
		}
		if c.LivenessProbe != nil {
			t.Errorf("container[%d] LivenessProbe should be nil", i)
		}
		if c.ReadinessProbe != nil {
			t.Errorf("container[%d] ReadinessProbe should be nil", i)
		}
		if c.SecurityContext != nil {
			t.Errorf("container[%d] SecurityContext should be nil", i)
		}
	}

	// GPU requests preserved per container
	gpu0 := transformed.Spec.Containers[0].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]
	if gpu0.Value() != 4 {
		t.Errorf("container[0] GPU requests = %d, want 4", gpu0.Value())
	}
	gpu1 := transformed.Spec.Containers[1].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]
	if gpu1.Value() != 8 {
		t.Errorf("container[1] GPU requests = %d, want 8", gpu1.Value())
	}

	// Total GPU via helper
	if total := GPURequestsFromPod(transformed); total != 12 {
		t.Errorf("GPURequestsFromPod = %d, want 12", total)
	}
}

func TestTransformPod_EmptyContainers(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-containers",
			Namespace: "test-ns",
			Labels:    map[string]string{"app": "test"},
		},
		Spec: corev1.PodSpec{
			NodeName:   "node-1",
			Containers: []corev1.Container{},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}

	result, err := transformPod(pod)
	if err != nil {
		t.Fatalf("transformPod returned error: %v", err)
	}
	transformed := result.(*corev1.Pod)

	if transformed.Name != "empty-containers" {
		t.Errorf("Name = %q, want %q", transformed.Name, "empty-containers")
	}
	if transformed.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", transformed.Namespace, "test-ns")
	}
	if transformed.Spec.NodeName != "node-1" {
		t.Errorf("NodeName = %q, want %q", transformed.Spec.NodeName, "node-1")
	}
	if transformed.Status.Phase != corev1.PodPending {
		t.Errorf("Phase = %v, want Pending", transformed.Status.Phase)
	}
	if transformed.Spec.Containers == nil {
		t.Error("Containers should be empty slice, not nil")
	}
	if len(transformed.Spec.Containers) != 0 {
		t.Errorf("Containers length = %d, want 0", len(transformed.Spec.Containers))
	}
}

func TestTransformPod_NoGPURequests(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-only"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	result, err := transformPod(pod)
	if err != nil {
		t.Fatalf("transformPod returned error: %v", err)
	}
	transformed := result.(*corev1.Pod)

	// CPU request preserved
	cpuQty := transformed.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
	if cpuQty.Value() != 4 {
		t.Errorf("CPU requests = %d, want 4", cpuQty.Value())
	}

	// No GPU
	if total := GPURequestsFromPod(transformed); total != 0 {
		t.Errorf("GPURequestsFromPod = %d, want 0", total)
	}
}

func TestTransformPod_WrongType(t *testing.T) {
	_, err := transformPod("not-a-pod")
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestTransformNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-node-01",
			Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"nvidia.com/gpu.product":      "NVIDIA-H200-141GB-HBM3e",
				"node-role.kubernetes.io/gpu":  "",
			},
			Annotations: map[string]string{
				"node.alpha.kubernetes.io/ttl": "0",
			},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "kubelet"},
			},
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{Key: "nvidia.com/gpu", Effect: corev1.TaintEffectNoSchedule},
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("8"),
				corev1.ResourceCPU:                    resource.MustParse("96"),
				corev1.ResourceMemory:                 resource.MustParse("768Gi"),
			},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
			},
		},
	}

	result, err := transformNode(node)
	if err != nil {
		t.Fatalf("transformNode returned error: %v", err)
	}
	transformed := result.(*corev1.Node)

	// Kept fields
	if transformed.Name != "gpu-node-01" {
		t.Errorf("Name = %q, want %q", transformed.Name, "gpu-node-01")
	}
	if len(transformed.Labels) != 3 {
		t.Errorf("Labels count = %d, want 3 (all labels preserved)", len(transformed.Labels))
	}
	if transformed.Labels["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Error("topology label should be preserved")
	}
	if transformed.Labels["nvidia.com/gpu.product"] != "NVIDIA-H200-141GB-HBM3e" {
		t.Error("GPU product label should be preserved")
	}

	// GPU allocatable preserved
	gpuQty := transformed.Status.Allocatable[corev1.ResourceName("nvidia.com/gpu")]
	if gpuQty.Value() != 8 {
		t.Errorf("GPU allocatable = %d, want 8", gpuQty.Value())
	}

	// Only GPU in allocatable (CPU, memory stripped)
	if len(transformed.Status.Allocatable) != 1 {
		t.Errorf("Allocatable count = %d, want 1 (only GPU)", len(transformed.Status.Allocatable))
	}

	// Stripped fields
	if transformed.ManagedFields != nil {
		t.Error("ManagedFields should be nil")
	}
	if transformed.Annotations != nil {
		t.Error("Annotations should be nil")
	}
	if transformed.Spec.Taints != nil {
		t.Error("Spec.Taints should be nil")
	}
	if transformed.Status.Conditions != nil {
		t.Error("Status.Conditions should be nil")
	}
	if transformed.Status.Addresses != nil {
		t.Error("Status.Addresses should be nil")
	}
}

func TestTransformNode_NoGPU(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-node"},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("32"),
			},
		},
	}

	result, err := transformNode(node)
	if err != nil {
		t.Fatalf("transformNode returned error: %v", err)
	}
	transformed := result.(*corev1.Node)

	if len(transformed.Status.Allocatable) != 0 {
		t.Errorf("Allocatable should be empty for non-GPU node, got %d entries", len(transformed.Status.Allocatable))
	}
}

func TestTransformNode_WrongType(t *testing.T) {
	_, err := transformNode("not-a-node")
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestTransformEvent(t *testing.T) {
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-event.12345",
			Namespace: "default",
			Annotations: map[string]string{
				"some-annotation": "value",
			},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "kubelet"},
			},
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:            "Pod",
			Name:            "my-pod",
			Namespace:       "default",
			UID:             types.UID("abc-123"),
			APIVersion:      "v1",
			ResourceVersion: "999",
			FieldPath:       "spec.containers{main}",
		},
		Reason:  "Scheduled",
		Message: "Successfully assigned default/my-pod to node-1",
		Type:    "Normal",
		Source:  corev1.EventSource{Component: "default-scheduler"},
		LastTimestamp: metav1.Time{Time: time.Now()},
		EventTime:    metav1.MicroTime{Time: time.Now()},
	}

	result, err := transformEvent(event)
	if err != nil {
		t.Fatalf("transformEvent returned error: %v", err)
	}
	transformed := result.(*corev1.Event)

	// Kept fields
	if transformed.Type != "Normal" {
		t.Errorf("Type = %q, want %q", transformed.Type, "Normal")
	}
	if transformed.Reason != "Scheduled" {
		t.Errorf("Reason = %q, want %q", transformed.Reason, "Scheduled")
	}
	if transformed.Message == "" {
		t.Error("Message should be preserved")
	}
	if transformed.InvolvedObject.Kind != "Pod" {
		t.Errorf("InvolvedObject.Kind = %q, want %q", transformed.InvolvedObject.Kind, "Pod")
	}
	if transformed.InvolvedObject.Name != "my-pod" {
		t.Errorf("InvolvedObject.Name = %q, want %q", transformed.InvolvedObject.Name, "my-pod")
	}
	if transformed.Source.Component != "default-scheduler" {
		t.Errorf("Source.Component = %q, want %q", transformed.Source.Component, "default-scheduler")
	}
	if transformed.LastTimestamp.IsZero() {
		t.Error("LastTimestamp should be preserved")
	}
	if transformed.EventTime.IsZero() {
		t.Error("EventTime should be preserved")
	}

	// Stripped fields
	if transformed.ManagedFields != nil {
		t.Error("ManagedFields should be nil")
	}
	if transformed.Annotations != nil {
		t.Error("Annotations should be nil")
	}
	if transformed.InvolvedObject.Namespace != "" {
		t.Error("InvolvedObject.Namespace should be empty")
	}
	if transformed.InvolvedObject.UID != "" {
		t.Error("InvolvedObject.UID should be empty")
	}
	if transformed.InvolvedObject.APIVersion != "" {
		t.Error("InvolvedObject.APIVersion should be empty")
	}
	if transformed.InvolvedObject.ResourceVersion != "" {
		t.Error("InvolvedObject.ResourceVersion should be empty")
	}
	if transformed.InvolvedObject.FieldPath != "" {
		t.Error("InvolvedObject.FieldPath should be empty")
	}
}

func TestTransformEvent_WrongType(t *testing.T) {
	_, err := transformEvent("not-an-event")
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestTransformDynamicObject(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "grove.io/v1alpha1",
			"kind":       "PodCliqueSet",
			"metadata": map[string]interface{}{
				"name":      "my-pcs",
				"namespace": "default",
				"labels": map[string]interface{}{
					"app": "inference",
				},
				"annotations": map[string]interface{}{
					"kubectl.kubernetes.io/last-applied-configuration": "huge-json",
				},
				"managedFields": []interface{}{
					map[string]interface{}{"manager": "kubectl"},
				},
			},
			"spec": map[string]interface{}{
				"replicas": int64(3),
			},
		},
	}

	result, err := transformDynamicObject(u)
	if err != nil {
		t.Fatalf("transformDynamicObject returned error: %v", err)
	}
	transformed := result.(*unstructured.Unstructured)

	// Kept fields
	if transformed.GetName() != "my-pcs" {
		t.Errorf("Name = %q, want %q", transformed.GetName(), "my-pcs")
	}
	if transformed.GetLabels()["app"] != "inference" {
		t.Error("Labels should be preserved")
	}
	spec, _, _ := unstructured.NestedMap(transformed.Object, "spec")
	if spec == nil {
		t.Error("spec should be preserved")
	}

	// Stripped fields
	if transformed.GetAnnotations() != nil {
		t.Error("Annotations should be nil")
	}
	mf, exists, _ := unstructured.NestedSlice(transformed.Object, "metadata", "managedFields")
	if exists || mf != nil {
		t.Error("managedFields should be removed")
	}
}

func TestTransformDynamicObject_WrongType(t *testing.T) {
	_, err := transformDynamicObject("not-unstructured")
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func boolPtr(b bool) *bool {
	return &b
}
