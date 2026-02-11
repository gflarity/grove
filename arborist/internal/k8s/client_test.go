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
	"testing"

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestFetchTopologyCLIData(t *testing.T) {
	// Create test nodes with topology labels + GPU info
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"topology.io/rack":            "rack-0",
				"nvidia.com/gpu.product":      "NVIDIA-H200-141GB-HBM3e",
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("8"),
			},
		},
	}
	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-2",
			Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1b",
				"topology.io/rack":            "rack-1",
				"nvidia.com/gpu.product":      "NVIDIA-B200-192GB-HBM3e",
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("4"),
			},
		},
	}
	node3 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-3",
			Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"topology.io/rack":            "rack-0",
			},
		},
		// No GPU
	}

	// Create test pods
	gpuPod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs-0-worker-abc",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("2"),
						},
					},
				},
			},
		},
	}
	gpuPod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs-0-worker-def",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}
	nonGPUPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs-0-coordinator-xyz",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-3",
		},
	}
	pendingGPUPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pcs-1-worker-pending",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "my-pcs",
			},
		},
		Spec: corev1.PodSpec{
			// No NodeName — pending
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("4"),
						},
					},
				},
			},
		},
	}
	otherWorkloadPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-workload-pod",
			Namespace: "default",
			Labels:    map[string]string{},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-2",
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}
	// Pod in different namespace
	otherNSPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-ns-pod",
			Namespace: "prod",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "prod-pcs",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-2",
		},
	}

	clientset := kubefake.NewSimpleClientset(node1, node2, node3, gpuPod1, gpuPod2, nonGPUPod, pendingGPUPod, otherWorkloadPod, otherNSPod)

	ct := &corev1alpha1.ClusterTopology{
		ObjectMeta: metav1.ObjectMeta{
			Name: corev1alpha1.DefaultClusterTopologyName,
		},
		Spec: corev1alpha1.ClusterTopologySpec{
			Levels: []corev1alpha1.TopologyLevel{
				{Domain: corev1alpha1.TopologyDomainZone, Key: "topology.kubernetes.io/zone"},
				{Domain: corev1alpha1.TopologyDomainRack, Key: "topology.io/rack"},
			},
		},
	}

	dynClient := dynamicfake.NewSimpleDynamicClient(
		newFakeScheme(),
		toUnstructuredClusterTopology(ct),
	)

	k8sClient := &K8sClient{
		clientset:     clientset,
		dynamicClient: dynClient,
	}

	t.Run("namespace-scoped", func(t *testing.T) {
		data, err := k8sClient.FetchTopologyCLIData(context.Background(), "default")
		if err != nil {
			t.Fatalf("FetchTopologyCLIData returned error: %v", err)
		}

		// Verify DomainToKey
		if len(data.DomainToKey) != 2 {
			t.Errorf("expected 2 domains, got %d", len(data.DomainToKey))
		}
		if data.DomainToKey["zone"] != "topology.kubernetes.io/zone" {
			t.Errorf("zone key = %q", data.DomainToKey["zone"])
		}
		if data.DomainToKey["rack"] != "topology.io/rack" {
			t.Errorf("rack key = %q", data.DomainToKey["rack"])
		}

		// Verify NodeLabels — only topology labels kept
		if len(data.NodeLabels) != 3 {
			t.Errorf("expected 3 nodes, got %d", len(data.NodeLabels))
		}
		if data.NodeLabels["node-1"]["topology.io/rack"] != "rack-0" {
			t.Errorf("node-1 rack = %q", data.NodeLabels["node-1"]["topology.io/rack"])
		}
		// GPU product label should NOT be in topology labels
		if _, ok := data.NodeLabels["node-1"]["nvidia.com/gpu.product"]; ok {
			t.Error("GPU product label should not be in filtered topology labels")
		}

		// Verify NodeGPUProducts
		if data.NodeGPUProducts["node-1"] != "H200" {
			t.Errorf("node-1 GPU product = %q, want H200", data.NodeGPUProducts["node-1"])
		}
		if data.NodeGPUProducts["node-2"] != "B200" {
			t.Errorf("node-2 GPU product = %q, want B200", data.NodeGPUProducts["node-2"])
		}
		if _, ok := data.NodeGPUProducts["node-3"]; ok {
			t.Error("node-3 should not have a GPU product")
		}

		// Verify NodeGPUCapacity
		if data.NodeGPUCapacity["node-1"] != 8 {
			t.Errorf("node-1 GPU capacity = %d, want 8", data.NodeGPUCapacity["node-1"])
		}
		if data.NodeGPUCapacity["node-2"] != 4 {
			t.Errorf("node-2 GPU capacity = %d, want 4", data.NodeGPUCapacity["node-2"])
		}

		// Verify AllPods — should include ALL pods in "default" namespace (not just PCS-managed)
		if len(data.AllPods) != 5 { // gpuPod1, gpuPod2, nonGPUPod, pendingGPUPod, otherWorkloadPod
			t.Errorf("expected 5 pods in default namespace, got %d", len(data.AllPods))
		}

		// Verify GPU requests are parsed correctly
		for _, pod := range data.AllPods {
			switch pod.Name {
			case "my-pcs-0-worker-abc":
				if pod.GPURequests != 2 {
					t.Errorf("pod %s: GPURequests = %d, want 2", pod.Name, pod.GPURequests)
				}
			case "my-pcs-0-worker-def":
				if pod.GPURequests != 1 {
					t.Errorf("pod %s: GPURequests = %d, want 1", pod.Name, pod.GPURequests)
				}
			case "my-pcs-0-coordinator-xyz":
				if pod.GPURequests != 0 {
					t.Errorf("pod %s: GPURequests = %d, want 0", pod.Name, pod.GPURequests)
				}
			case "my-pcs-1-worker-pending":
				if pod.GPURequests != 4 {
					t.Errorf("pod %s: GPURequests = %d, want 4", pod.Name, pod.GPURequests)
				}
				if pod.NodeName != "" {
					t.Errorf("pod %s: should be pending (no NodeName), got %q", pod.Name, pod.NodeName)
				}
			case "other-workload-pod":
				if pod.GPURequests != 1 {
					t.Errorf("pod %s: GPURequests = %d, want 1", pod.Name, pod.GPURequests)
				}
			}
		}
	})

	t.Run("all-namespaces", func(t *testing.T) {
		data, err := k8sClient.FetchTopologyCLIData(context.Background(), "")
		if err != nil {
			t.Fatalf("FetchTopologyCLIData returned error: %v", err)
		}

		// Should include pods from both namespaces
		if len(data.AllPods) != 6 { // 5 in default + 1 in prod
			t.Errorf("expected 6 pods across all namespaces, got %d", len(data.AllPods))
		}
	})
}

func TestGPURequestsFromPod(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want int64
	}{
		{
			name: "no GPU requests",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "main"}},
				},
			},
			want: 0,
		},
		{
			name: "single container with GPU",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("2"),
								},
							},
						},
					},
				},
			},
			want: 2,
		},
		{
			name: "multiple containers sum",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("2"),
								},
							},
						},
						{
							Name: "sidecar",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gpuRequestsFromPod(tt.pod)
			if got != tt.want {
				t.Errorf("gpuRequestsFromPod() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGPUCapacityFromNode(t *testing.T) {
	tests := []struct {
		name string
		node *corev1.Node
		want int64
	}{
		{
			name: "no GPU",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Allocatable: corev1.ResourceList{},
				},
			},
			want: 0,
		},
		{
			name: "with GPU",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Allocatable: corev1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("8"),
					},
				},
			},
			want: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gpuCapacityFromNode(tt.node)
			if got != tt.want {
				t.Errorf("gpuCapacityFromNode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGPUProductFromNode(t *testing.T) {
	tests := []struct {
		name string
		node *corev1.Node
		want string
	}{
		{
			name: "no GPU label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			want: "",
		},
		{
			name: "H200 label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"nvidia.com/gpu.product": "NVIDIA-H200-141GB-HBM3e",
					},
				},
			},
			want: "H200",
		},
		{
			name: "B200 label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"nvidia.com/gpu.product": "NVIDIA-B200-192GB-HBM3e",
					},
				},
			},
			want: "B200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gpuProductFromNode(tt.node)
			if got != tt.want {
				t.Errorf("gpuProductFromNode() = %q, want %q", got, tt.want)
			}
		})
	}
}
