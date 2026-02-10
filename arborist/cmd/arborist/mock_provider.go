package main

import (
	"context"
	"fmt"
	"time"

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// Compile-time check that MockProvider satisfies DataProvider.
var _ DataProvider = (*MockProvider)(nil)

// MockProvider implements DataProvider with canned data for testing.
type MockProvider struct {
	// PodCliqueSets is the list returned by GetAllPodCliqueSets.
	PodCliqueSets []Resource
	// PodCliqueSetSpecs maps "namespace/name" to a PodCliqueSet object.
	PodCliqueSetSpecs map[string]*corev1alpha1.PodCliqueSet
	// ReplicaIndexes maps "namespace/pcsName" to replica index list.
	ReplicaIndexes map[string][]string
	// ScalingGroups maps "namespace/pcsName/replicaIndex" to PCSG resources.
	ScalingGroups map[string][]Resource
	// ReplicaPodCliques maps "namespace/pcsName/replicaIndex" to standalone PodClique resources.
	ReplicaPodCliques map[string][]Resource
	// PCSGPodCliques maps "namespace/pcsgName" to PodClique resources within a PCSG.
	PCSGPodCliques map[string][]Resource
	// PodCliquePods maps "namespace/podCliqueName" to Pod resources.
	PodCliquePods map[string][]Resource
	// PodYAMLs maps "namespace/podName" to YAML strings.
	PodYAMLs map[string]string
	// Events maps a context key to event lists. Keys follow the pattern used by
	// the various GetEventsFor* methods (e.g. "pcs/namespace/name").
	Events map[string][]Event
	// ClusterTopology is returned by GetClusterTopology.
	ClusterTopology *corev1alpha1.ClusterTopology
	// PodInfos maps "namespace/pcsName" to cached pod info.
	PodInfos map[string]map[string]CachedPodInfo
	// NodeLabels is returned by GetAllNodeLabels.
	NodeLabels map[string]map[string]string

	// Errors allows injecting errors for specific operations.
	// Keys: "GetAllPodCliqueSets", "GetPodCliqueSet", etc.
	Errors map[string]error

	// Delay adds an artificial delay to all operations (for testing async behavior).
	Delay time.Duration
}

// NewMockProvider creates a MockProvider with all maps initialized.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		PodCliqueSetSpecs: make(map[string]*corev1alpha1.PodCliqueSet),
		ReplicaIndexes:    make(map[string][]string),
		ScalingGroups:     make(map[string][]Resource),
		ReplicaPodCliques: make(map[string][]Resource),
		PCSGPodCliques:    make(map[string][]Resource),
		PodCliquePods:     make(map[string][]Resource),
		PodYAMLs:          make(map[string]string),
		Events:            make(map[string][]Event),
		PodInfos:          make(map[string]map[string]CachedPodInfo),
		NodeLabels:        make(map[string]map[string]string),
		Errors:            make(map[string]error),
	}
}

func (m *MockProvider) maybeDelay() {
	if m.Delay > 0 {
		time.Sleep(m.Delay)
	}
}

func (m *MockProvider) GetAllPodCliqueSets(_ context.Context) ([]Resource, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetAllPodCliqueSets"]; ok {
		return nil, err
	}
	return m.PodCliqueSets, nil
}

func (m *MockProvider) GetPodCliqueSet(_ context.Context, name, namespace string) (*corev1alpha1.PodCliqueSet, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetPodCliqueSet"]; ok {
		return nil, err
	}
	key := namespace + "/" + name
	pcs, ok := m.PodCliqueSetSpecs[key]
	if !ok {
		return nil, fmt.Errorf("PodCliqueSet %s not found", key)
	}
	return pcs, nil
}

func (m *MockProvider) GetReplicaIndexesForPodCliqueSet(_ context.Context, pcsName, namespace string) ([]string, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetReplicaIndexesForPodCliqueSet"]; ok {
		return nil, err
	}
	key := namespace + "/" + pcsName
	return m.ReplicaIndexes[key], nil
}

func (m *MockProvider) GetPodCliqueScalingGroupsForPodCliqueSetReplica(_ context.Context, pcsName, namespace, replicaIndex string) ([]Resource, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetPodCliqueScalingGroupsForPodCliqueSetReplica"]; ok {
		return nil, err
	}
	key := namespace + "/" + pcsName + "/" + replicaIndex
	return m.ScalingGroups[key], nil
}

func (m *MockProvider) GetPodCliquesForPodCliqueSetReplica(_ context.Context, pcsName, namespace, replicaIndex string) ([]Resource, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetPodCliquesForPodCliqueSetReplica"]; ok {
		return nil, err
	}
	key := namespace + "/" + pcsName + "/" + replicaIndex
	return m.ReplicaPodCliques[key], nil
}

func (m *MockProvider) GetPodCliquesForPodCliqueScalingGroup(_ context.Context, pcsgName, namespace string) ([]Resource, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetPodCliquesForPodCliqueScalingGroup"]; ok {
		return nil, err
	}
	key := namespace + "/" + pcsgName
	return m.PCSGPodCliques[key], nil
}

func (m *MockProvider) GetPodsForPodClique(_ context.Context, podCliqueName, namespace string) ([]Resource, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetPodsForPodClique"]; ok {
		return nil, err
	}
	key := namespace + "/" + podCliqueName
	return m.PodCliquePods[key], nil
}

func (m *MockProvider) GetPodYAML(_ context.Context, podName, namespace string) (string, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetPodYAML"]; ok {
		return "", err
	}
	key := namespace + "/" + podName
	yaml, ok := m.PodYAMLs[key]
	if !ok {
		return "", fmt.Errorf("Pod YAML for %s not found", key)
	}
	return yaml, nil
}

func (m *MockProvider) GetEventsForPodCliqueSet(_ context.Context, pcsName, namespace string) ([]Event, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetEventsForPodCliqueSet"]; ok {
		return nil, err
	}
	key := "pcs/" + namespace + "/" + pcsName
	return m.Events[key], nil
}

func (m *MockProvider) GetEventsForPodCliqueSetReplica(_ context.Context, pcsName, namespace, replicaIndex string) ([]Event, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetEventsForPodCliqueSetReplica"]; ok {
		return nil, err
	}
	key := "replica/" + namespace + "/" + pcsName + "/" + replicaIndex
	return m.Events[key], nil
}

func (m *MockProvider) GetEventsForPodCliqueScalingGroup(_ context.Context, pcsgName, namespace string) ([]Event, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetEventsForPodCliqueScalingGroup"]; ok {
		return nil, err
	}
	key := "pcsg/" + namespace + "/" + pcsgName
	return m.Events[key], nil
}

func (m *MockProvider) GetEventsForPodClique(_ context.Context, podCliqueName, namespace string) ([]Event, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetEventsForPodClique"]; ok {
		return nil, err
	}
	key := "pc/" + namespace + "/" + podCliqueName
	return m.Events[key], nil
}

func (m *MockProvider) GetClusterTopology(_ context.Context) (*corev1alpha1.ClusterTopology, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetClusterTopology"]; ok {
		return nil, err
	}
	return m.ClusterTopology, nil
}

func (m *MockProvider) GetPodInfoForPCS(_ context.Context, pcsName, namespace string) (map[string]CachedPodInfo, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetPodInfoForPCS"]; ok {
		return nil, err
	}
	key := namespace + "/" + pcsName
	return m.PodInfos[key], nil
}

func (m *MockProvider) GetAllNodeLabels(_ context.Context, _ []string) (map[string]map[string]string, error) {
	m.maybeDelay()
	if err, ok := m.Errors["GetAllNodeLabels"]; ok {
		return nil, err
	}
	return m.NodeLabels, nil
}
