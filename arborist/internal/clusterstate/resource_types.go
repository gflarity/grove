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

import "strings"

// Resource type constants used across the Arborist codebase.
const (
	ResourceTypePodCliqueSet     = "PodCliqueSet"
	ResourceTypePCSReplica       = "(PodCliqueSet replica)"
	ResourceTypePCSG             = "PodCliqueScalingGroup"
	ResourceTypePCSGReplica      = "(PodCliqueScalingGroup replica)"
	ResourceTypePodClique        = "PodClique"
	ResourceTypePod              = "Pod"
)

// Label key constants for Grove's Kubernetes labels.
const (
	LabelPartOf           = "app.kubernetes.io/part-of"
	LabelPCSReplicaIndex  = "grove.io/podcliqueset-replica-index"
	LabelPCSG             = "grove.io/podcliquescalinggroup"
	LabelPCSGReplicaIndex = "grove.io/podcliquescalinggroup-replica-index"
	LabelPodClique        = "grove.io/podclique"
)

// ReplicaDisplayName constructs a display name for a virtual replica resource,
// e.g. "my-pcs-replica-0". Used consistently across the TUI and snapshot builder.
func ReplicaDisplayName(baseName, index string) string {
	return baseName + "-replica-" + index
}

// CompositeKey builds a "name/index" key used for replica lookups in snapshot maps.
func CompositeKey(name, index string) string {
	return name + "/" + index
}

// SplitCompositeKey splits a "name/index" key into its components.
// Splits on the last "/" to handle names that might contain "/".
// Returns ("", "") if the key has no separator.
func SplitCompositeKey(key string) (name, index string) {
	i := strings.LastIndex(key, "/")
	if i < 0 {
		return "", ""
	}
	return key[:i], key[i+1:]
}
