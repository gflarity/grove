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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// transformPod strips fields not needed by the informer cache to reduce memory.
//
// Kept: Name, Namespace, Labels, Spec.NodeName, Spec.Containers[*].Resources.Requests, Status.Phase.
// Stripped: ManagedFields, Annotations, InitContainers, Volumes, container Env/VolumeMounts/Command/Args/
// Image/Ports/Probes/SecurityContext, and all Status subfields except Phase.
func transformPod(obj interface{}) (interface{}, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("transformPod: expected *corev1.Pod, got %T", obj)
	}

	pod.ManagedFields = nil
	pod.Annotations = nil
	pod.Spec.InitContainers = nil
	pod.Spec.Volumes = nil

	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		c.Env = nil
		c.EnvFrom = nil
		c.VolumeMounts = nil
		c.Command = nil
		c.Args = nil
		c.Image = ""
		c.Ports = nil
		c.LivenessProbe = nil
		c.ReadinessProbe = nil
		c.StartupProbe = nil
		c.SecurityContext = nil
	}

	pod.Status = corev1.PodStatus{Phase: pod.Status.Phase}

	return pod, nil
}

// transformNode strips fields not needed by the informer cache to reduce memory.
//
// Kept: Name, Labels (all — topology keys are dynamic), Status.Allocatable["nvidia.com/gpu"].
// Stripped: ManagedFields, Annotations, Spec, and all Status subfields except GPU allocatable.
func transformNode(obj interface{}) (interface{}, error) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return nil, fmt.Errorf("transformNode: expected *corev1.Node, got %T", obj)
	}

	node.ManagedFields = nil
	node.Annotations = nil
	node.Spec = corev1.NodeSpec{}

	gpuKey := corev1.ResourceName("nvidia.com/gpu")
	gpuQty, hasGPU := node.Status.Allocatable[gpuKey]
	if hasGPU {
		node.Status = corev1.NodeStatus{
			Allocatable: corev1.ResourceList{gpuKey: gpuQty},
		}
	} else {
		node.Status = corev1.NodeStatus{}
	}

	return node, nil
}

// transformEvent strips fields not needed by the informer cache to reduce memory.
//
// Kept: Type, Reason, Message, LastTimestamp, EventTime, InvolvedObject.Kind,
// InvolvedObject.Name, Source.Component.
// Stripped: ManagedFields, Annotations, and unused InvolvedObject subfields.
func transformEvent(obj interface{}) (interface{}, error) {
	event, ok := obj.(*corev1.Event)
	if !ok {
		return nil, fmt.Errorf("transformEvent: expected *corev1.Event, got %T", obj)
	}

	event.ManagedFields = nil
	event.Annotations = nil

	event.InvolvedObject.Namespace = ""
	event.InvolvedObject.UID = ""
	event.InvolvedObject.APIVersion = ""
	event.InvolvedObject.ResourceVersion = ""
	event.InvolvedObject.FieldPath = ""

	return event, nil
}

// transformDynamicObject strips metadata.managedFields and metadata.annotations
// from dynamic (unstructured) informer objects. Used for all Grove CRDs.
func transformDynamicObject(obj interface{}) (interface{}, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("transformDynamicObject: expected *unstructured.Unstructured, got %T", obj)
	}

	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(u.Object, "metadata", "annotations")

	return u, nil
}
