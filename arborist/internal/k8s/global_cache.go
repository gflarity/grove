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

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Compile-time check that InformerGlobalCache satisfies clusterstate.GlobalCache.
var _ clusterstate.GlobalCache = (*InformerGlobalCache)(nil)

// GlobalCacheOption configures an InformerGlobalCache.
type GlobalCacheOption func(*InformerGlobalCache)

// WithCacheNamespace scopes the cache's informers to a single namespace.
// Namespace-scoped resources (Pods, Events, PCS, PCSG, PC) will only watch
// the given namespace. Cluster-scoped resources (Nodes, ClusterTopology) are
// always watched cluster-wide regardless of this option.
// When ns is "" or this option is not provided, the cache watches all namespaces.
func WithCacheNamespace(ns string) GlobalCacheOption {
	return func(c *InformerGlobalCache) {
		c.namespace = ns
	}
}

// WithOnWarning sets a callback that is invoked for non-fatal warnings during
// cache startup (e.g. a missing CRD). The TUI uses this to surface warnings in
// the error log box instead of losing them to stderr.
func WithOnWarning(fn func(string)) GlobalCacheOption {
	return func(c *InformerGlobalCache) {
		c.onWarning = fn
	}
}

// InformerGlobalCache implements clusterstate.GlobalCache using client-go informers.
// It watches all resource types needed by the TUI: Nodes, Pods, Events,
// PodCliqueSets, PodCliqueScalingGroups, PodCliques, and ClusterTopology.
//
// The struct uses composition to separate concerns:
//   - APIResourceFetcher: on-demand API calls (YAML, logs, containers)
//   - informerSet: informer factories, CRD flags, and data-reading methods
//   - snapshotStore: snapshot state, mutex, notification channel, debounce
//
// Zero-value note: InformerGlobalCache requires NewInformerGlobalCache() for
// construction. This is justified by the K8s client dependencies (clientset,
// dynamicClient) that cannot be meaningfully defaulted, and the snapshotStore
// which needs an initialized notification channel.
type InformerGlobalCache struct {
	*APIResourceFetcher // satisfies ResourceFetcher interface
	informerSet         // factories, informers, CRD flags, read methods
	snapshotStore       // snapshot, mutex, channel, debounce

	// cancel stops all informer goroutines; set by Start().
	cancel context.CancelFunc
}

// NewInformerGlobalCache creates a new InformerGlobalCache.
// The cache is not started until Start() is called.
func NewInformerGlobalCache(clientset kubernetes.Interface, dynamicClient dynamic.Interface, opts ...GlobalCacheOption) *InformerGlobalCache {
	c := &InformerGlobalCache{
		APIResourceFetcher: &APIResourceFetcher{
			clientset:     clientset,
			dynamicClient: dynamicClient,
		},
		snapshotStore: newSnapshotStore(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetOnWarning sets the warning callback. Implements clusterstate.WarningConfigurable.
func (c *InformerGlobalCache) SetOnWarning(fn func(string)) {
	c.onWarning = fn
}

// Start begins the informers and starts watching. Non-blocking.
func (c *InformerGlobalCache) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.setup(c.clientset, c.dynamicClient)
	c.startFactories(ctx, c.onChange)

	return nil
}

// WaitForSync blocks until all informers have completed their initial list,
// or the context is cancelled. After sync completes, it triggers an immediate
// snapshot rebuild.
func (c *InformerGlobalCache) WaitForSync(ctx context.Context) bool {
	synced := c.waitForSync(ctx)
	if synced {
		c.rebuildSnapshot()
	}
	return synced
}

// Stop shuts down informers and closes the updates channel.
func (c *InformerGlobalCache) Stop() {
	c.stop()
	if c.cancel != nil {
		c.cancel()
	}
}

// onChange is called by informer event handlers whenever any watched resource changes.
func (c *InformerGlobalCache) onChange() {
	c.scheduleRebuild(c.rebuildSnapshot)
}
