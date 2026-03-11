package diagnostics

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func newFakeDynamicScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	for _, res := range GroveResourceTypes {
		gvk := schema.GroupVersionKind{
			Group:   res.GVR.Group,
			Version: res.GVR.Version,
			Kind:    res.GVR.Resource, // fake dynamic client indexes on resource
		}
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind + "List",
			},
			&unstructured.UnstructuredList{},
		)
	}
	return scheme
}

func TestCollectGroveResources_WithResources(t *testing.T) {
	scheme := newFakeDynamicScheme()

	pcs := &unstructured.Unstructured{}
	pcs.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "grove.io", Version: "v1alpha1", Kind: "PodCliqueSet",
	})
	pcs.SetName("my-pcs")
	pcs.SetNamespace("test-ns")

	dc := &DiagnosticContext{
		DynamicClient: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
			scheme,
			map[schema.GroupVersionResource]string{
				{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquesets"}:          "PodCliqueSetList",
				{Group: "grove.io", Version: "v1alpha1", Resource: "podcliques"}:             "PodCliqueList",
				{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquescalinggroups"}: "PodCliqueScalingGroupList",
				{Group: "scheduler.grove.io", Version: "v1alpha1", Resource: "podgangs"}:     "PodGangList",
			},
			pcs,
		),
		Namespace: "test-ns",
	}
	out := &mockOutput{}

	err := CollectGroveResources(context.Background(), dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.yamlWrites < 1 {
		t.Errorf("expected at least 1 YAML write for the PCS, got %d", out.yamlWrites)
	}

	foundPCS := false
	for _, line := range out.lines {
		if strings.Contains(line, "Found 1 PodCliqueSets") {
			foundPCS = true
			break
		}
	}
	if !foundPCS {
		t.Errorf("expected 'Found 1 PodCliqueSets' in output, got lines: %v", out.lines)
	}
}

func TestCollectGroveResources_NoResources(t *testing.T) {
	scheme := newFakeDynamicScheme()
	dc := &DiagnosticContext{
		DynamicClient: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
			scheme,
			map[schema.GroupVersionResource]string{
				{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquesets"}:          "PodCliqueSetList",
				{Group: "grove.io", Version: "v1alpha1", Resource: "podcliques"}:             "PodCliqueList",
				{Group: "grove.io", Version: "v1alpha1", Resource: "podcliquescalinggroups"}: "PodCliqueScalingGroupList",
				{Group: "scheduler.grove.io", Version: "v1alpha1", Resource: "podgangs"}:     "PodGangList",
			},
		),
		Namespace: "test-ns",
	}
	out := &mockOutput{}

	err := CollectGroveResources(context.Background(), dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.yamlWrites != 0 {
		t.Errorf("expected 0 YAML writes, got %d", out.yamlWrites)
	}

	// Should have "No X found" messages for each resource type
	noFoundCount := 0
	for _, line := range out.lines {
		if strings.Contains(line, "No ") && strings.Contains(line, "found") {
			noFoundCount++
		}
	}
	if noFoundCount != len(GroveResourceTypes) {
		t.Errorf("expected 'No X found' for each resource type (%d), got %d", len(GroveResourceTypes), noFoundCount)
	}
}

func TestCollectGroveResources_NilDynamicClient(t *testing.T) {
	dc := &DiagnosticContext{
		DynamicClient: nil,
		Namespace:     "test-ns",
	}
	out := &mockOutput{}

	err := CollectGroveResources(context.Background(), dc, out)
	if err == nil {
		t.Fatal("expected error for nil dynamic client")
	}
	if !strings.Contains(err.Error(), "dynamic client is nil") {
		t.Errorf("expected 'dynamic client is nil' error, got: %v", err)
	}
}
