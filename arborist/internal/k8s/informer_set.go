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
	"fmt"
	"sort"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	corev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// GVR definitions for Grove CRDs.
var (
	globalPcsGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquesets",
	}
	globalPcsgGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliquescalinggroups",
	}
	globalPcGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "podcliques",
	}
	globalClusterTopologyGVR = schema.GroupVersionResource{
		Group:    "grove.io",
		Version:  "v1alpha1",
		Resource: "clustertopologies",
	}
)

const (
	// eventMaxAge bounds in-memory event storage to recent events only.
	eventMaxAge = 1 * time.Hour
)

// informerSet holds the informer infrastructure: factories, informers,
// CRD availability flags, and data-reading methods. It is embedded in
// InformerGlobalCache.
type informerSet struct {
	// namespace limits namespace-scoped informers to a single namespace.
	// Empty string means all namespaces (cluster-wide).
	namespace string

	// Informer factories
	coreFactory           informers.SharedInformerFactory
	dynamicFactory        dynamicinformer.DynamicSharedInformerFactory
	clusterDynamicFactory dynamicinformer.DynamicSharedInformerFactory

	// Additional factories for filtered informers
	podInformerFactory   informers.SharedInformerFactory
	eventInformerFactory informers.SharedInformerFactory

	// Informers
	nodeInformer  cache.SharedIndexInformer
	podInformer   cache.SharedIndexInformer
	eventInformer cache.SharedIndexInformer
	pcsInformer   cache.SharedIndexInformer
	pcsgInformer  cache.SharedIndexInformer
	pcInformer    cache.SharedIndexInformer
	ctInformer    cache.SharedIndexInformer

	// Optional warning callback for non-fatal startup issues.
	onWarning func(string)

	// CRD availability flags — set during setup() by checking the Discovery API.
	// When false, the corresponding informer is nil and features degrade gracefully.
	pcsAvailable         bool
	pcsgAvailable        bool
	pcAvailable          bool
	topologyCRDAvailable bool
}

// setup creates factories and informers, checks CRD availability.
func (s *informerSet) setup(clientset kubernetes.Interface, dynamicClient dynamic.Interface) {
	// Create core informer factory for Nodes — always cluster-wide.
	s.coreFactory = informers.NewSharedInformerFactoryWithOptions(
		clientset,
		0, // no resync period — we rely on watch events
	)

	// Pod informer watches all pods (no label selector) so that non-Grove GPU
	// pods are included in topology view GPU accounting.
	// When a namespace is set, scope to that namespace.
	var podFactoryOpts []informers.SharedInformerOption
	if s.namespace != "" {
		podFactoryOpts = append(podFactoryOpts, informers.WithNamespace(s.namespace))
	}
	s.podInformerFactory = informers.NewSharedInformerFactoryWithOptions(
		clientset,
		0,
		podFactoryOpts...,
	)

	// Event informer — when a namespace is set, scope to that namespace.
	var eventFactoryOpts []informers.SharedInformerOption
	if s.namespace != "" {
		eventFactoryOpts = append(eventFactoryOpts, informers.WithNamespace(s.namespace))
	}
	s.eventInformerFactory = informers.NewSharedInformerFactoryWithOptions(
		clientset,
		0,
		eventFactoryOpts...,
	)

	// Dynamic informer factory for namespace-scoped CRDs (PCS, PCSG, PC).
	// When a namespace is set, use NewFilteredDynamicSharedInformerFactory to scope.
	if s.namespace != "" {
		s.dynamicFactory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(
			dynamicClient,
			0,
			s.namespace,
			nil,
		)
	} else {
		s.dynamicFactory = dynamicinformer.NewDynamicSharedInformerFactory(
			dynamicClient,
			0,
		)
	}

	// Set up core informers
	s.nodeInformer = s.coreFactory.Core().V1().Nodes().Informer()
	s.podInformer = s.podInformerFactory.Core().V1().Pods().Informer()
	s.eventInformer = s.eventInformerFactory.Core().V1().Events().Informer()

	// Check which Grove CRDs are available before creating dynamic informers.
	groveAvailable := s.checkGroveCRDsAvailable(clientset)
	s.pcsAvailable = groveAvailable[globalPcsGVR.Resource]
	s.pcsgAvailable = groveAvailable[globalPcsgGVR.Resource]
	s.pcAvailable = groveAvailable[globalPcGVR.Resource]
	s.topologyCRDAvailable = groveAvailable[globalClusterTopologyGVR.Resource]

	// Dynamic informers for namespace-scoped CRDs — only create if CRD exists
	// to avoid noisy reflector errors when CRDs aren't installed.
	if s.pcsAvailable {
		s.pcsInformer = s.dynamicFactory.ForResource(globalPcsGVR).Informer()
	} else if s.onWarning != nil {
		s.onWarning("PodCliqueSet CRD not found — PCS data unavailable")
	}
	if s.pcsgAvailable {
		s.pcsgInformer = s.dynamicFactory.ForResource(globalPcsgGVR).Informer()
	} else if s.onWarning != nil {
		s.onWarning("PodCliqueScalingGroup CRD not found — PCSG data unavailable")
	}
	if s.pcAvailable {
		s.pcInformer = s.dynamicFactory.ForResource(globalPcGVR).Informer()
	} else if s.onWarning != nil {
		s.onWarning("PodClique CRD not found — PC data unavailable")
	}

	// ClusterTopology informer — cluster-scoped, needs its own factory.
	if s.topologyCRDAvailable {
		s.clusterDynamicFactory = dynamicinformer.NewDynamicSharedInformerFactory(
			dynamicClient,
			0,
		)
		s.ctInformer = s.clusterDynamicFactory.ForResource(globalClusterTopologyGVR).Informer()
	} else if s.onWarning != nil {
		s.onWarning("ClusterTopology CRD not found — topology view unavailable")
	}
}

// startFactories registers event handlers on all informers and starts all factories.
func (s *informerSet) startFactories(ctx context.Context, onChange func()) {
	// Register event handlers on all informers
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { onChange() },
		UpdateFunc: func(_, _ interface{}) { onChange() },
		DeleteFunc: func(_ interface{}) { onChange() },
	}

	//nolint:errcheck // registration errors only occur if informer is stopped
	s.nodeInformer.AddEventHandler(handler)
	//nolint:errcheck
	s.podInformer.AddEventHandler(handler)
	//nolint:errcheck
	s.eventInformer.AddEventHandler(handler)
	if s.pcsInformer != nil {
		//nolint:errcheck
		s.pcsInformer.AddEventHandler(handler)
	}
	if s.pcsgInformer != nil {
		//nolint:errcheck
		s.pcsgInformer.AddEventHandler(handler)
	}
	if s.pcInformer != nil {
		//nolint:errcheck
		s.pcInformer.AddEventHandler(handler)
	}
	if s.ctInformer != nil {
		//nolint:errcheck
		s.ctInformer.AddEventHandler(handler)
	}

	// Start informers
	s.coreFactory.Start(ctx.Done())
	s.podInformerFactory.Start(ctx.Done())
	s.eventInformerFactory.Start(ctx.Done())
	s.dynamicFactory.Start(ctx.Done())
	if s.clusterDynamicFactory != nil {
		s.clusterDynamicFactory.Start(ctx.Done())
	}
}

// waitForSync blocks until all informers have completed their initial list,
// or the context is cancelled.
func (s *informerSet) waitForSync(ctx context.Context) bool {
	syncFuncs := []cache.InformerSynced{
		s.nodeInformer.HasSynced,
		s.podInformer.HasSynced,
		s.eventInformer.HasSynced,
	}
	if s.pcsInformer != nil {
		syncFuncs = append(syncFuncs, s.pcsInformer.HasSynced)
	}
	if s.pcsgInformer != nil {
		syncFuncs = append(syncFuncs, s.pcsgInformer.HasSynced)
	}
	if s.pcInformer != nil {
		syncFuncs = append(syncFuncs, s.pcInformer.HasSynced)
	}
	if s.ctInformer != nil {
		syncFuncs = append(syncFuncs, s.ctInformer.HasSynced)
	}
	return cache.WaitForCacheSync(ctx.Done(), syncFuncs...)
}

// warnf logs a non-fatal warning via the onWarning callback (if set).
func (s *informerSet) warnf(format string, args ...interface{}) {
	if s.onWarning != nil {
		s.onWarning(fmt.Sprintf(format, args...))
	}
}

// checkGroveCRDsAvailable makes a single Discovery call for the grove.io/v1alpha1
// API group and returns a map of resource name → available for all resources found.
func (s *informerSet) checkGroveCRDsAvailable(clientset kubernetes.Interface) map[string]bool {
	result := make(map[string]bool)
	resourceList, err := clientset.Discovery().ServerResourcesForGroupVersion("grove.io/v1alpha1")
	if err != nil {
		// API group not found — no Grove CRDs installed
		return result
	}
	for _, r := range resourceList.APIResources {
		result[r.Name] = true
	}
	return result
}

// readClusterTopologyLevels reads the ClusterTopology CR from the informer cache.
// Returns nil when the ClusterTopology CRD is not available.
func (s *informerSet) readClusterTopologyLevels() []corev1alpha1.TopologyLevel {
	if s.ctInformer == nil {
		return nil
	}
	return readClusterTopologyLevelsFromInformer(s.ctInformer)
}

// readNodeLabels reads all node labels from the informer cache.
func (s *informerSet) readNodeLabels(topologyKeys map[string]bool) nodeReadResult {
	return readNodeLabelsFromInformer(s.nodeInformer, topologyKeys)
}

// readPCSSpecs reads all PodCliqueSet specs from the informer cache.
func (s *informerSet) readPCSSpecs() map[string]*corev1alpha1.PodCliqueSet {
	if s.pcsInformer == nil {
		return nil
	}
	return readPCSSpecsFromInformer(s.pcsInformer)
}

// readPCSGs reads all PodCliqueScalingGroups from the informer cache and groups them by PCS replica.
func (s *informerSet) readPCSGs() (map[string][]*corev1alpha1.PodCliqueScalingGroup, []*corev1alpha1.PodCliqueScalingGroup) {
	if s.pcsgInformer == nil {
		return nil, nil
	}
	items := s.pcsgInformer.GetStore().List()
	byReplica := make(map[string][]*corev1alpha1.PodCliqueScalingGroup)
	var allPCSGs []*corev1alpha1.PodCliqueScalingGroup

	for _, item := range items {
		pcsg, err := toTyped[corev1alpha1.PodCliqueScalingGroup](item)
		if err != nil {
			s.warnf("failed to convert PodCliqueScalingGroup: %v", err)
			continue
		}

		allPCSGs = append(allPCSGs, pcsg)

		pcsName := pcsg.Labels[clusterstate.LabelPartOf]
		replicaIndex := pcsg.Labels[clusterstate.LabelPCSReplicaIndex]
		if pcsName != "" && replicaIndex != "" {
			key := clusterstate.CompositeKey(pcsName, replicaIndex)
			byReplica[key] = append(byReplica[key], pcsg)
		}
	}

	return byReplica, allPCSGs
}

// readPodCliques reads all PodCliques from the informer cache and groups them.
// It uses scheduledByPodClique (computed from pod NodeName) instead of status.ScheduledReplicas.
// It also returns pcObjectsByPCSGReplica for PCSG scheduled-count roll-up.
func (s *informerSet) readPodCliques(scheduledByPodClique map[string]int32) podCliqueResult {
	result := podCliqueResult{
		ByReplica:              make(map[string][]clusterstate.Resource),
		ByPCSG:                 make(map[string][]clusterstate.Resource),
		ByPCSGReplica:          make(map[string][]clusterstate.Resource),
		PCObjectsByPCSGReplica: make(map[string][]*corev1alpha1.PodClique),
		StandalonePCObjects:    make(map[string][]*corev1alpha1.PodClique),
	}
	if s.pcInformer == nil {
		return result
	}
	items := s.pcInformer.GetStore().List()

	for _, item := range items {
		pc, err := toTyped[corev1alpha1.PodClique](item)
		if err != nil {
			s.warnf("failed to convert PodClique: %v", err)
			continue
		}

		labels := pc.Labels
		pcsName := labels[clusterstate.LabelPartOf]
		replicaIndex := labels[clusterstate.LabelPCSReplicaIndex]
		pcsgName := labels[clusterstate.LabelPCSG]
		pcsgReplicaIndex := labels[clusterstate.LabelPCSGReplicaIndex]

		replicas := pc.Spec.Replicas
		readyReplicas := pc.Status.ReadyReplicas
		scheduledReplicas := scheduledByPodClique[pc.Name]

		resource := clusterstate.Resource{
			Name:      pc.Name,
			Type:      clusterstate.ResourceTypePodClique,
			Ready:     fmt.Sprintf("%d/%d", readyReplicas, replicas),
			Scheduled: fmt.Sprintf("%d/%d", scheduledReplicas, replicas),
			Status:    "",
			Namespace: pc.Namespace,
		}

		if pcsgName != "" {
			// PodClique belongs to a PCSG
			resource.ParentType = clusterstate.ResourceTypePCSG
			resource.ParentName = pcsgName
			result.ByPCSG[pcsgName] = append(result.ByPCSG[pcsgName], resource)

			if pcsgReplicaIndex != "" {
				pcsgReplicaKey := clusterstate.CompositeKey(pcsgName, pcsgReplicaIndex)
				replicaResource := resource
				replicaResource.ParentType = clusterstate.ResourceTypePCSGReplica
				replicaResource.ParentName = clusterstate.ReplicaDisplayName(pcsgName, pcsgReplicaIndex)
				result.ByPCSGReplica[pcsgReplicaKey] = append(result.ByPCSGReplica[pcsgReplicaKey], replicaResource)

				result.PCObjectsByPCSGReplica[pcsgReplicaKey] = append(result.PCObjectsByPCSGReplica[pcsgReplicaKey], pc)
			}
		} else if pcsName != "" && replicaIndex != "" {
			// Standalone PodClique directly under PCS replica
			resource.ParentType = clusterstate.ResourceTypePCSReplica
			resource.ParentName = clusterstate.ReplicaDisplayName(pcsName, replicaIndex)
			key := clusterstate.CompositeKey(pcsName, replicaIndex)
			result.ByReplica[key] = append(result.ByReplica[key], resource)

			result.StandalonePCObjects[key] = append(result.StandalonePCObjects[key], pc)
		}
	}

	return result
}

// readPods reads all pods from the informer cache and converts them to TopologyPodInput.
func (s *informerSet) readPods() []clusterstate.TopologyPodInput {
	return readPodsFromInformer(s.podInformer)
}

// readEvents reads all events from the informer cache and indexes by involved object.
func (s *informerSet) readEvents() map[string][]clusterstate.Event {
	items := s.eventInformer.GetStore().List()
	result := make(map[string][]clusterstate.Event)
	cutoff := time.Now().Add(-eventMaxAge)

	for _, item := range items {
		event, err := toTyped[corev1.Event](item)
		if err != nil {
			s.warnf("failed to convert Event: %v", err)
			continue
		}

		// Skip old events
		eventTime := event.LastTimestamp.Time
		if eventTime.IsZero() {
			eventTime = event.EventTime.Time
		}
		if !eventTime.IsZero() && eventTime.Before(cutoff) {
			continue
		}

		age := clusterstate.FormatAge(eventTime)

		evt := clusterstate.Event{
			Type:      event.Type,
			Kind:      event.InvolvedObject.Kind,
			Reason:    event.Reason,
			Age:       age,
			From:      event.Source.Component,
			Message:   event.Message,
			Parent:    event.InvolvedObject.Name,
			Timestamp: eventTime,
		}

		key := event.InvolvedObject.Kind + "/" + event.InvolvedObject.Name
		result[key] = append(result[key], evt)
	}

	// Sort each event list newest-first
	for k := range result {
		sort.Slice(result[k], func(i, j int) bool {
			return result[k][i].Timestamp.After(result[k][j].Timestamp)
		})
	}

	return result
}
