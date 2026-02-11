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

	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// DataProvider abstracts Kubernetes data fetching for the TUI.
// Implementations: K8sClient (real), MockProvider (tests).
type DataProvider interface {
	GetAllPodCliqueSets(ctx context.Context) ([]Resource, error)
	GetPodCliqueSet(ctx context.Context, name, namespace string) (*corev1alpha1.PodCliqueSet, error)
	GetReplicaIndexesForPodCliqueSet(ctx context.Context, pcsName, namespace string) ([]string, error)
	GetPodCliqueScalingGroupsForPodCliqueSetReplica(ctx context.Context, pcsName, namespace, replicaIndex string) ([]Resource, error)
	GetPodCliquesForPodCliqueSetReplica(ctx context.Context, pcsName, namespace, replicaIndex string) ([]Resource, error)
	GetPodCliquesForPodCliqueScalingGroup(ctx context.Context, pcsgName, namespace string) ([]Resource, error)
	GetReplicaIndexesForPodCliqueScalingGroup(ctx context.Context, pcsgName, namespace string) ([]string, error)
	GetPodCliquesForPodCliqueScalingGroupReplica(ctx context.Context, pcsgName, namespace, replicaIndex string) ([]Resource, error)
	GetPodsForPodClique(ctx context.Context, podCliqueName, namespace string) ([]Resource, error)
	GetPodYAML(ctx context.Context, podName, namespace string) (string, error)
	GetEventsForPodCliqueSet(ctx context.Context, pcsName, namespace string) ([]Event, error)
	GetEventsForPodCliqueSetReplica(ctx context.Context, pcsName, namespace, replicaIndex string) ([]Event, error)
	GetEventsForPodCliqueScalingGroup(ctx context.Context, pcsgName, namespace string) ([]Event, error)
	GetEventsForPodCliqueScalingGroupReplica(ctx context.Context, pcsgName, namespace, replicaIndex string) ([]Event, error)
	GetEventsForPodClique(ctx context.Context, podCliqueName, namespace string) ([]Event, error)
	GetClusterTopology(ctx context.Context) (*corev1alpha1.ClusterTopology, error)
	GetPodInfoForPCS(ctx context.Context, pcsName, namespace string) (map[string]CachedPodInfo, error)
	GetAllNodeLabels(ctx context.Context, topologyKeys []string) (map[string]map[string]string, error)
}
