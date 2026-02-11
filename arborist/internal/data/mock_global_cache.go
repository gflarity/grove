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

package data

import (
	"context"
	"fmt"
	"sync"
)

// Compile-time check that MockGlobalCache satisfies GlobalCache.
var _ GlobalCache = (*MockGlobalCache)(nil)

// MockGlobalCache implements GlobalCache for testing.
// Tests set the CacheSnapshot directly and trigger updates on demand.
type MockGlobalCache struct {
	snapshot  *CacheSnapshot
	updatesCh chan struct{}
	mu        sync.RWMutex
	stopped   bool

	// PodYAMLs maps "namespace/podName" to YAML strings.
	PodYAMLs map[string]string

	// Errors allows injecting errors for specific operations.
	Errors map[string]error
}

// NewMockGlobalCache creates a new MockGlobalCache with an initialized updates channel.
func NewMockGlobalCache() *MockGlobalCache {
	return &MockGlobalCache{
		updatesCh: make(chan struct{}, 1),
		PodYAMLs:  make(map[string]string),
		Errors:    make(map[string]error),
		snapshot: &CacheSnapshot{
			PodCliqueSets:          []Resource{},
			ReplicaIndexesByPCS:    make(map[string][]string),
			ScalingGroupsByReplica: make(map[string][]Resource),
			PodCliquesByReplica:    make(map[string][]Resource),
			ReplicaIndexesByPCSG:   make(map[string][]string),
			PodCliquesByPCSG:       make(map[string][]Resource),
			PodCliquesByPCSGReplica: make(map[string][]Resource),
			PodsByPodClique:        make(map[string][]Resource),
			EventsByObject:         make(map[string][]Event),
			NodeLabels:             make(map[string]map[string]string),
			PodInfos:               make(map[string]CachedPodInfo),
			NodeGPUProducts:        make(map[string]string),
			NodeGPUCapacity:        make(map[string]int64),
		},
	}
}

// Start is a no-op for the mock. Always returns nil.
func (m *MockGlobalCache) Start(_ context.Context) error {
	return nil
}

// WaitForSync always returns true immediately for the mock.
func (m *MockGlobalCache) WaitForSync(_ context.Context) bool {
	return true
}

// Snapshot returns the current snapshot. Thread-safe.
func (m *MockGlobalCache) Snapshot() *CacheSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

// Updates returns the notification channel.
func (m *MockGlobalCache) Updates() <-chan struct{} {
	return m.updatesCh
}

// Stop marks the mock as stopped and closes the updates channel.
func (m *MockGlobalCache) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopped {
		m.stopped = true
		close(m.updatesCh)
	}
}

// GetPodYAML returns pre-configured YAML for a pod.
func (m *MockGlobalCache) GetPodYAML(_ context.Context, podName, namespace string) (string, error) {
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

// SetSnapshot sets the current snapshot. Thread-safe.
func (m *MockGlobalCache) SetSnapshot(s *CacheSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot = s
}

// SendUpdate sends a notification on the updates channel.
func (m *MockGlobalCache) SendUpdate() {
	select {
	case m.updatesCh <- struct{}{}:
	default:
	}
}
