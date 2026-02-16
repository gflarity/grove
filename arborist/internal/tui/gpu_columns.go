package tui

import (
	"fmt"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
)

// buildResourceColumnSpecs builds the column spec for the resources table,
// inserting dynamic GPU columns between TOPOLOGY and READY.
// The TOPOLOGY column is only included when topology data is available.
func (m *Model) buildResourceColumnSpecs(gpuTypes []string, lastColTitle string) []ColumnSpec {
	// Base columns: NAMESPACE, TYPE, NAME, [TOPOLOGY], [GPU types], READY
	specs := []ColumnSpec{
		{Title: "NAMESPACE", Weight: 2},
		{Title: "TYPE", Weight: 3},
		{Title: "NAME", Weight: 5},
	}
	if m.topologyColumnVisible() {
		specs = append(specs, ColumnSpec{Title: "TOPOLOGY", Weight: 3})
	}

	// GPU type columns (one per discovered GPU type)
	for _, gpuType := range gpuTypes {
		specs = append(specs, ColumnSpec{Title: gpuType, Weight: 1})
	}

	specs = append(specs, ColumnSpec{Title: "READY", Weight: 2})

	// Final column: SCHEDULED or PHASE
	specs = append(specs, ColumnSpec{Title: lastColTitle, Weight: 2})

	return specs
}

// colorizeResourceRowWithGPU returns plain text for each column value including GPU columns.
// The row format is: [Namespace, Type, Name, [Topology], <gpu1>, <gpu2>, ..., Ready, Scheduled]
// The Topology field is only included when topology data is available.
func (m *Model) colorizeResourceRowWithGPU(r clusterstate.Resource, gpuTypes []string) []string {
	row := []string{r.Namespace, r.Type, r.Name}
	if m.topologyColumnVisible() {
		row = append(row, r.Topology)
	}

	// Add GPU count values
	if len(gpuTypes) > 0 {
		counts := m.gpuCountsForResource(r)
		isPending := m.isResourcePending(r)

		for _, gpuType := range gpuTypes {
			if isPending {
				row = append(row, "?")
			} else if counts != nil {
				count := counts[gpuType]
				if count > 0 {
					row = append(row, fmt.Sprintf("%d", count))
				} else {
					row = append(row, "0")
				}
			} else {
				row = append(row, "0")
			}
		}
	}

	row = append(row, r.Ready)
	row = append(row, r.Scheduled)
	return row
}

// gpuCountsForResource returns the GPU counts for a given resource based on its type and name.
func (m *Model) gpuCountsForResource(r clusterstate.Resource) clusterstate.GPUCounts {
	if m.gpuSummary == nil {
		return nil
	}
	switch r.Type {
	case clusterstate.ResourceTypePodCliqueSet:
		return m.gpuSummary.ByPCS[r.Name]
	case clusterstate.ResourceTypePCSReplica:
		pcsName := m.viewState.SelectedPodCliqueSet
		replicaIndex := extractReplicaIndex(r.Name)
		if pcsName != "" && replicaIndex != "" {
			return m.gpuSummary.ByReplica[clusterstate.CompositeKey(pcsName, replicaIndex)]
		}
		return nil
	case clusterstate.ResourceTypePCSG:
		return m.gpuSummary.ByPCSG[r.Name]
	case clusterstate.ResourceTypePCSGReplica:
		pcsgName := m.viewState.SelectedScalingGroup
		replicaIndex := extractReplicaIndex(r.Name)
		if pcsgName != "" && replicaIndex != "" {
			return m.gpuSummary.ByPCSGReplica[clusterstate.CompositeKey(pcsgName, replicaIndex)]
		}
		return nil
	case clusterstate.ResourceTypePodClique:
		return m.gpuSummary.ByPodClique[r.Name]
	case clusterstate.ResourceTypePod:
		return m.gpuSummary.ByPod[r.Name]
	default:
		return nil
	}
}

// isResourcePending returns true if the resource (or any of its descendant pods) is pending
// with GPU requests that can't be attributed to a GPU type yet.
func (m *Model) isResourcePending(r clusterstate.Resource) bool {
	if m.gpuSummary == nil || len(m.gpuSummary.PendingGPUPods) == 0 {
		return false
	}

	// For pods, check directly
	if r.Type == clusterstate.ResourceTypePod {
		_, isPending := m.gpuSummary.PendingGPUPods[r.Name]
		return isPending
	}

	return false
}

// extractReplicaIndex extracts the replica index from a name like "foo-replica-0".
func extractReplicaIndex(name string) string {
	parts := strings.Split(name, replicaSeparator)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}
