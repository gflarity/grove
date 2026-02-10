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

import "context"

// TopologyCache provides a real-time, informer-backed cache of topology-relevant
// cluster state. It watches Nodes, Pods, PodCliqueSets, and ClusterTopology,
// debounces changes, and provides atomic snapshots for the TUI.
type TopologyCache interface {
	// Start begins the informers and starts watching. Non-blocking.
	// The provided context controls the cache lifetime.
	Start(ctx context.Context) error

	// WaitForSync blocks until all informers have completed their initial list,
	// or the context is cancelled. Returns true if synced.
	WaitForSync(ctx context.Context) bool

	// Snapshot returns the latest computed TopologyViewData.
	// Thread-safe. Returns nil if the cache hasn't synced yet.
	Snapshot() *TopologyViewData

	// Updates returns a channel that receives a signal whenever the cache
	// has a new snapshot available (after debounce). The TUI should block
	// on this channel in a tea.Cmd.
	Updates() <-chan struct{}

	// Stop shuts down informers and closes the updates channel.
	Stop()
}
