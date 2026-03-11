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

package clusterstate

import (
	"context"
	"fmt"
	"time"

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// CacheLifecycle manages the lifecycle of the informer-backed cache.
type CacheLifecycle interface {
	Start(ctx context.Context) error
	WaitForSync(ctx context.Context) bool
	Stop()
}

// CacheReader provides read-only access to cached cluster state.
type CacheReader interface {
	Snapshot() *CacheSnapshot
	Updates() <-chan struct{}
}

// ResourceFetcher performs on-demand API calls for resource details.
type ResourceFetcher interface {
	GetPodYAML(ctx context.Context, podName, namespace string) (string, error)
	GetResourceYAML(ctx context.Context, resourceType, name, namespace string) (string, error)
	GetPodContainers(ctx context.Context, podName, namespace string) ([]ContainerInfo, error)
	GetPodLogs(ctx context.Context, podName, namespace, container string, tailLines int64) (string, error)
}

// GlobalCache provides a single, informer-backed cache of all cluster state
// needed by the TUI. It composes the narrower CacheLifecycle, CacheReader,
// and ResourceFetcher interfaces.
type GlobalCache interface {
	CacheLifecycle
	CacheReader
	ResourceFetcher
}

// WarningConfigurable is an optional interface that GlobalCache implementations
// can satisfy to receive a warning callback before Start(). The TUI uses this
// to capture non-fatal startup warnings (e.g. missing CRDs) and route them to
// the error log box.
type WarningConfigurable interface {
	SetOnWarning(fn func(string))
}

// HierarchyData holds the pre-built resource hierarchy from grove labels.
type HierarchyData struct {
	PodCliqueSets           []Resource
	PodCliqueSetSpecs       map[string]*corev1alpha1.PodCliqueSet // pcsName -> PCS
	ReplicaIndexesByPCS     map[string][]string                   // pcsName -> sorted replica indexes
	ScalingGroupsByReplica  map[string][]Resource                 // "pcsName/replicaIndex" -> []PCSG resources
	PodCliquesByReplica     map[string][]Resource                 // "pcsName/replicaIndex" -> standalone PodCliques
	ReplicaIndexesByPCSG    map[string][]string                   // pcsgName -> sorted PCSG replica indexes
	PodCliquesByPCSG        map[string][]Resource                 // pcsgName -> []PodClique resources
	PodCliquesByPCSGReplica map[string][]Resource                 // "pcsgName/replicaIndex" -> []PodClique resources
	PodsByPodClique         map[string][]Resource                 // podCliqueName -> []Pod resources
	PodInfos                map[string]CachedPodInfo              // all PCS-managed pods: podName -> info
}

// CacheSnapshot holds a point-in-time snapshot of all cluster state needed by the TUI.
// Built from informer caches on every debounced change. All fields are read-only
// after construction.
type CacheSnapshot struct {
	HierarchyData
	EventsByObject   map[string][]Event // "Kind/name" -> events (sorted newest first)
	TopologyViewData *TopologyViewData  // single source for GPU/node data
}

// PodCliqueSetInfo holds display-ready info about a PodCliqueSet.
type PodCliqueSetInfo struct {
	Name      string
	Namespace string
	Resource  Resource
}

// FormatAge formats a time duration into a human-readable age string.
func FormatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	duration := time.Since(t)

	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd", int(duration.Hours()/24))
}
