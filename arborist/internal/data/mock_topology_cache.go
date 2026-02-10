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
	"sync"
)

// Compile-time check that MockTopologyCache satisfies TopologyCache.
var _ TopologyCache = (*MockTopologyCache)(nil)

// MockTopologyCache implements TopologyCache for testing.
// It allows tests to pre-load snapshots, trigger update notifications on demand,
// and avoid any real informer machinery.
type MockTopologyCache struct {
	snapshot  *TopologyViewData
	updatesCh chan struct{}
	mu        sync.RWMutex
	stopped   bool
}

// NewMockTopologyCache creates a new MockTopologyCache with an initialized updates channel.
func NewMockTopologyCache() *MockTopologyCache {
	return &MockTopologyCache{
		updatesCh: make(chan struct{}, 1),
	}
}

// Start is a no-op for the mock. Always returns nil.
func (m *MockTopologyCache) Start(_ context.Context) error {
	return nil
}

// WaitForSync always returns true immediately for the mock.
func (m *MockTopologyCache) WaitForSync(_ context.Context) bool {
	return true
}

// Snapshot returns the current snapshot. Thread-safe.
func (m *MockTopologyCache) Snapshot() *TopologyViewData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

// Updates returns the notification channel.
func (m *MockTopologyCache) Updates() <-chan struct{} {
	return m.updatesCh
}

// Stop marks the mock as stopped and closes the updates channel.
func (m *MockTopologyCache) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopped {
		m.stopped = true
		close(m.updatesCh)
	}
}

// SetSnapshot sets the current snapshot. Thread-safe.
// This allows tests to control exactly what data the TUI receives.
func (m *MockTopologyCache) SetSnapshot(data *TopologyViewData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot = data
}

// SendUpdate sends a notification on the updates channel.
// This triggers the TUI to read a new snapshot.
// Non-blocking: if the channel already has a pending notification, this is a no-op.
func (m *MockTopologyCache) SendUpdate() {
	select {
	case m.updatesCh <- struct{}{}:
	default:
	}
}
