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
	"sort"
	"strconv"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// snapshotInput holds all data read from informers, decoupling snapshot
// building from the InformerGlobalCache receiver. This makes buildSnapshot
// a pure function of its inputs, easier to test and reason about.
type snapshotInput struct {
	levels         []corev1alpha1.TopologyLevel
	nodeResult     nodeReadResult
	pcsSpecs       map[string]*corev1alpha1.PodCliqueSet
	pcsgsByReplica map[string][]*corev1alpha1.PodCliqueScalingGroup
	pods           []clusterstate.TopologyPodInput
	pcResult       podCliqueResult
	eventsByObject map[string][]clusterstate.Event
}

// schedulingContext groups the maps needed by all scheduling-computation
// functions. Zero value is safe — nil-map reads return zero values in Go,
// so all computations correctly return 0/false with no clusterstate.
type schedulingContext struct {
	scheduledByPodClique   map[string]int32
	pcObjectsByPCSGReplica map[string][]*corev1alpha1.PodClique
	standalonePCObjects    map[string][]*corev1alpha1.PodClique
	pcsgsByReplica         map[string][]*corev1alpha1.PodCliqueScalingGroup
}

// rebuildSnapshot reads from all informer caches, builds a new CacheSnapshot,
// and stores it with notification. This is the thin wrapper on InformerGlobalCache
// that reads inputs and delegates to buildSnapshot.
func (c *InformerGlobalCache) rebuildSnapshot() {
	// Read all topology keys from levels for node label filtering
	levels := c.readClusterTopologyLevels()
	topologyKeys := make(map[string]bool, len(levels))
	for _, level := range levels {
		topologyKeys[level.Key] = true
	}

	// Read Nodes, PCS specs, PCSGs
	nodeResult := c.readNodeLabels(topologyKeys)
	pcsSpecs := c.readPCSSpecs()
	pcsgsByReplica, _ := c.readPCSGs()

	// Read Pods first — needed to build scheduledByPodClique before reading PodCliques.
	pods := c.readPods()
	_, _, scheduledByPodClique := buildPodMappings(pods)

	// Read PodClique resources (uses scheduledByPodClique computed from pods above)
	pcResult := c.readPodCliques(scheduledByPodClique)

	// Read Events
	eventsByObject := c.readEvents()

	snapshot := buildSnapshot(snapshotInput{
		levels:         levels,
		nodeResult:     nodeResult,
		pcsSpecs:       pcsSpecs,
		pcsgsByReplica: pcsgsByReplica,
		pods:           pods,
		pcResult:       pcResult,
		eventsByObject: eventsByObject,
	})

	c.storeAndNotify(snapshot)
}

// buildSnapshot constructs a CacheSnapshot from pre-read informer clusterstate.
// This is a pure function — it does not access the InformerGlobalCache.
func buildSnapshot(in snapshotInput) *clusterstate.CacheSnapshot {
	// Build pod lookup, pods-by-PodClique, and scheduled-by-PodClique mappings.
	podInfos, podsByPodClique, scheduledByPodClique := buildPodMappings(in.pods)

	// Build scheduling context — groups the 4 maps used by all scheduling computations.
	sc := &schedulingContext{
		scheduledByPodClique:   scheduledByPodClique,
		pcObjectsByPCSGReplica: in.pcResult.PCObjectsByPCSGReplica,
		standalonePCObjects:    in.pcResult.StandalonePCObjects,
		pcsgsByReplica:         in.pcsgsByReplica,
	}

	// Build forest view (PodCliqueSet resources) — uses computed scheduled counts
	pcsResources := buildPCSResources(in.pcsSpecs, sc)

	// Build replica indexes by PCS
	replicaIndexesByPCS := make(map[string][]string)
	collectReplicaIndexes(replicaIndexesByPCS, in.pcsgsByReplica)
	collectReplicaIndexes(replicaIndexesByPCS, in.pcResult.ByReplica)
	// Also populate from PCS specs — ensures replicas are visible even when
	// child resources (PCSGs/PodCliques) haven't been created yet or haven't
	// synced to the informer cache.
	for pcsName, pcs := range in.pcsSpecs {
		for i := int32(0); i < pcs.Spec.Replicas; i++ {
			ri := fmt.Sprintf("%d", i)
			if !containsString(replicaIndexesByPCS[pcsName], ri) {
				replicaIndexesByPCS[pcsName] = append(replicaIndexesByPCS[pcsName], ri)
			}
		}
	}
	sortStringMapValues(replicaIndexesByPCS)

	// Build PCSG replica indexes
	replicaIndexesByPCSG := make(map[string][]string)
	collectReplicaIndexes(replicaIndexesByPCSG, in.pcResult.ByPCSGReplica)
	sortStringMapValues(replicaIndexesByPCSG)

	// Build the topology view snapshot
	topologyViewData := clusterstate.BuildTopologyViewData(in.levels, in.pcsSpecs, in.pods, in.nodeResult.nodeLabels)

	// Build and attach GPU summary
	gpuSummary := clusterstate.BuildGPUSummary(in.pods, in.nodeResult.nodeGPUProducts)
	topologyViewData.GPUSummary = gpuSummary
	topologyViewData.NodeGPUProducts = in.nodeResult.nodeGPUProducts
	topologyViewData.NodeGPUCapacity = in.nodeResult.nodeGPUCapacity

	// Sort all resource lists by name for stable table ordering.
	scalingGroupsByReplica := make(map[string][]clusterstate.Resource)
	for k, v := range in.pcsgsByReplica {
		scalingGroupsByReplica[k] = convertPCSGsToResources(v, sc)
	}
	clusterstate.SortResourceMapsByName(scalingGroupsByReplica)
	clusterstate.SortResourceMapsByName(in.pcResult.ByReplica)
	clusterstate.SortResourceMapsByName(in.pcResult.ByPCSG)
	clusterstate.SortResourceMapsByName(in.pcResult.ByPCSGReplica)
	clusterstate.SortResourceMapsByName(podsByPodClique)

	return &clusterstate.CacheSnapshot{
		HierarchyData: clusterstate.HierarchyData{
			PodCliqueSets:           pcsResources,
			PodCliqueSetSpecs:       in.pcsSpecs,
			ReplicaIndexesByPCS:     replicaIndexesByPCS,
			ScalingGroupsByReplica:  scalingGroupsByReplica,
			PodCliquesByReplica:     in.pcResult.ByReplica,
			ReplicaIndexesByPCSG:    replicaIndexesByPCSG,
			PodCliquesByPCSG:        in.pcResult.ByPCSG,
			PodCliquesByPCSGReplica: in.pcResult.ByPCSGReplica,
			PodsByPodClique:         podsByPodClique,
			PodInfos:                podInfos,
		},
		EventsByObject:   in.eventsByObject,
		TopologyViewData: topologyViewData,
	}
}

// buildPodMappings constructs pod lookup, pods-by-PodClique grouping, and
// scheduled-pod counts from raw pod input.
func buildPodMappings(pods []clusterstate.TopologyPodInput) (
	podInfos map[string]clusterstate.CachedPodInfo,
	podsByPodClique map[string][]clusterstate.Resource,
	scheduledByPodClique map[string]int32,
) {
	podInfos = make(map[string]clusterstate.CachedPodInfo, len(pods))
	podsByPodClique = make(map[string][]clusterstate.Resource)
	scheduledByPodClique = make(map[string]int32)

	for _, pod := range pods {
		podInfos[pod.Name] = clusterstate.CachedPodInfo{
			NodeName: pod.NodeName,
			Labels:   pod.Labels,
		}

		podCliqueName := pod.Labels[clusterstate.LabelPodClique]
		if podCliqueName != "" {
			if pod.NodeName != "" {
				scheduledByPodClique[podCliqueName]++
			}

			ready := "0/1"
			phase := pod.Phase
			if phase == "Running" {
				ready = "1/1"
			}

			podsByPodClique[podCliqueName] = append(podsByPodClique[podCliqueName], clusterstate.Resource{
				Name:       pod.Name,
				Type:       clusterstate.ResourceTypePod,
				Ready:      ready,
				Scheduled:  phase,
				Status:     phase,
				Namespace:  pod.Namespace,
				ParentType: clusterstate.ResourceTypePodClique,
				ParentName: podCliqueName,
			})
		}
	}
	return
}

// convertPCSGsToResources converts PCSG objects to Resource display objects.
// It computes the Scheduled count bottom-up: a PCSG replica is "scheduled" when
// all its constituent PodCliques have scheduledPods >= minAvailable.
func convertPCSGsToResources(
	pcsgs []*corev1alpha1.PodCliqueScalingGroup,
	sc *schedulingContext,
) []clusterstate.Resource {
	resources := make([]clusterstate.Resource, 0, len(pcsgs))
	for _, pcsg := range pcsgs {
		replicas := pcsg.Status.Replicas
		availableReplicas := pcsg.Status.AvailableReplicas

		// Compute scheduled replicas bottom-up from PodClique data
		scheduledReplicas := sc.pcsgScheduledReplicas(pcsg)

		pcsName := pcsg.Labels[clusterstate.LabelPartOf]
		replicaIndex := pcsg.Labels[clusterstate.LabelPCSReplicaIndex]

		resources = append(resources, clusterstate.Resource{
			Name:       pcsg.Name,
			Type:       clusterstate.ResourceTypePCSG,
			Ready:      fmt.Sprintf("%d/%d", availableReplicas, replicas),
			Scheduled:  fmt.Sprintf("%d/%d", scheduledReplicas, replicas),
			Status:     "",
			Namespace:  pcsg.Namespace,
			ParentType: clusterstate.ResourceTypePCSReplica,
			ParentName: clusterstate.ReplicaDisplayName(pcsName, replicaIndex),
		})
	}
	return resources
}

// pcsgScheduledReplicas computes how many PCSG replicas are "scheduled".
// A PCSG replica is scheduled when ALL its constituent PodCliques have
// scheduledPods >= minAvailable (where minAvailable defaults to replicas if nil).
func (sc *schedulingContext) pcsgScheduledReplicas(pcsg *corev1alpha1.PodCliqueScalingGroup) int32 {
	totalReplicas := pcsg.Spec.Replicas
	var scheduled int32
	for i := int32(0); i < totalReplicas; i++ {
		replicaKey := clusterstate.CompositeKey(pcsg.Name, strconv.Itoa(int(i)))
		pcs := sc.pcObjectsByPCSGReplica[replicaKey]
		if sc.isPCSGReplicaScheduled(pcs) {
			scheduled++
		}
	}
	return scheduled
}

// effectiveMinAvailable returns *minAvailable if non-nil, otherwise replicas.
func effectiveMinAvailable(replicas int32, minAvailable *int32) int32 {
	if minAvailable != nil {
		return *minAvailable
	}
	return replicas
}

// isPCSGReplicaScheduled returns true if all PodCliques in a PCSG replica
// have scheduledPods >= minAvailable.
func (sc *schedulingContext) isPCSGReplicaScheduled(pcs []*corev1alpha1.PodClique) bool {
	if len(pcs) == 0 {
		return false
	}
	for _, pc := range pcs {
		minAvail := effectiveMinAvailable(pc.Spec.Replicas, pc.Spec.MinAvailable)
		if sc.scheduledByPodClique[pc.Name] < minAvail {
			return false
		}
	}
	return true
}

// podCliqueResult holds the grouped outputs of readPodCliques.
type podCliqueResult struct {
	ByReplica              map[string][]clusterstate.Resource            // "pcsName/replicaIndex" -> standalone PodCliques
	ByPCSG                 map[string][]clusterstate.Resource            // pcsgName -> PodCliques
	ByPCSGReplica          map[string][]clusterstate.Resource            // "pcsgName/replicaIndex" -> PodCliques
	PCObjectsByPCSGReplica map[string][]*corev1alpha1.PodClique  // "pcsgName/replicaIndex" -> PodClique objects
	StandalonePCObjects    map[string][]*corev1alpha1.PodClique  // "pcsName/replicaIndex" -> standalone PodClique objects
}

// buildPCSResources constructs Resource display objects for PodCliqueSets.
// It computes the Scheduled count bottom-up: a PCS replica is "scheduled" when
// all its standalone PodCliques have scheduledPods >= minAvailable AND all its
// PCSGs are fully scheduled (all PCSG replicas meet minAvailable).
func buildPCSResources(
	pcsSpecs map[string]*corev1alpha1.PodCliqueSet,
	sc *schedulingContext,
) []clusterstate.Resource {
	resources := make([]clusterstate.Resource, 0, len(pcsSpecs))

	for _, pcs := range pcsSpecs {
		replicas := pcs.Spec.Replicas
		availableReplicas := pcs.Status.AvailableReplicas

		// Compute scheduled replicas bottom-up
		scheduledReplicas := sc.pcsScheduledReplicas(pcs)

		topology := "N/A"
		if pcs.Spec.Template.TopologyConstraint != nil {
			topology = string(pcs.Spec.Template.TopologyConstraint.PackDomain)
		}

		resources = append(resources, clusterstate.Resource{
			Name:      pcs.Name,
			Type:      clusterstate.ResourceTypePodCliqueSet,
			Ready:     fmt.Sprintf("%d/%d", availableReplicas, replicas),
			Scheduled: fmt.Sprintf("%d/%d", scheduledReplicas, replicas),
			Status:    "",
			Namespace: pcs.Namespace,
			Topology:  topology,
		})
	}

	// Sort by name for stable ordering
	clusterstate.SortResourcesByName(resources)

	return resources
}

// pcsScheduledReplicas computes how many PCS replicas are "scheduled".
// A PCS replica is scheduled when:
//  1. All standalone PodCliques in that replica have scheduledPods >= minAvailable
//  2. All PCSGs in that replica are fully scheduled (every PCSG replica meets minAvailable)
func (sc *schedulingContext) pcsScheduledReplicas(pcs *corev1alpha1.PodCliqueSet) int32 {
	var scheduled int32
	for i := int32(0); i < pcs.Spec.Replicas; i++ {
		replicaKey := clusterstate.CompositeKey(pcs.Name, strconv.Itoa(int(i)))
		if sc.isPCSReplicaScheduled(replicaKey) {
			scheduled++
		}
	}
	return scheduled
}

// isPCSReplicaScheduled checks whether a single PCS replica is fully scheduled.
func (sc *schedulingContext) isPCSReplicaScheduled(replicaKey string) bool {
	// Check standalone PodCliques
	for _, pc := range sc.standalonePCObjects[replicaKey] {
		minAvail := effectiveMinAvailable(pc.Spec.Replicas, pc.Spec.MinAvailable)
		if sc.scheduledByPodClique[pc.Name] < minAvail {
			return false
		}
	}

	// Check PCSGs in this replica
	for _, pcsg := range sc.pcsgsByReplica[replicaKey] {
		pcsgScheduled := sc.pcsgScheduledReplicas(pcsg)
		pcsgMinAvail := effectiveMinAvailable(pcsg.Spec.Replicas, pcsg.Spec.MinAvailable)
		if pcsgScheduled < pcsgMinAvail {
			return false
		}
	}

	// A replica with no standalone PCs and no PCSGs has nothing to schedule,
	// so we consider it scheduled only if at least one component exists.
	standalones := sc.standalonePCObjects[replicaKey]
	pcsgs := sc.pcsgsByReplica[replicaKey]
	if len(standalones) == 0 && len(pcsgs) == 0 {
		return false
	}

	return true
}

// Helper functions

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// collectReplicaIndexes extracts name/index pairs from keys in keyedMap
// (which use "name/index" format) and appends unique indexes to indexMap.
// Works with any map whose keys are replica-style "name/index" strings.
func collectReplicaIndexes[V any](indexMap map[string][]string, keyedMap map[string]V) {
	for key := range keyedMap {
		name, idx := clusterstate.SplitCompositeKey(key)
		if name != "" {
			if !containsString(indexMap[name], idx) {
				indexMap[name] = append(indexMap[name], idx)
			}
		}
	}
}

// sortStringMapValues sorts each string slice value in the map.
func sortStringMapValues(m map[string][]string) {
	for k, v := range m {
		sort.Strings(v)
		m[k] = v
	}
}
