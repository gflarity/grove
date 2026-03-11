package tui

import (
	"fmt"
	"os/exec"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	tea "github.com/charmbracelet/bubbletea"
)

// hierarchyViewBase implements ViewBehavior + BackNavigator for hierarchy
// views that do not support pod actions (logs/shell).
type hierarchyViewBase struct {
	viewKeyFn       func(vs clusterstate.ViewState) string
	navigateBackFn  func(m *Model) bool
	rebuildEventsFn func(m *Model, snapshot *clusterstate.CacheSnapshot)
	handleKeyFn     func(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) // optional override
}

func (h hierarchyViewBase) ViewKey(vs clusterstate.ViewState) string                     { return h.viewKeyFn(vs) }
func (h hierarchyViewBase) PaneList() []clusterstate.Pane                                { return hierarchyPanes }
func (h hierarchyViewBase) RebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) { h.rebuildEventsFn(m, snapshot) }
func (h hierarchyViewBase) RenderPanes(m *Model, resourcesH, eventsH int) []string {
	return hierarchyRenderPanes(m, resourcesH, eventsH)
}
func (h hierarchyViewBase) NavigateBack(m *Model) bool { return h.navigateBackFn(m) }
func (h hierarchyViewBase) HandleKey(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if h.handleKeyFn != nil {
		return h.handleKeyFn(m, msg)
	}
	return defaultHierarchyHandleKey(m, msg, h.navigateBackFn)
}

// defaultHierarchyHandleKey handles Esc, Enter, and Up/Down for hierarchy views.
func defaultHierarchyHandleKey(m *Model, msg tea.KeyMsg, navigateBackFn func(*Model) bool) (tea.Model, tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyEsc:
		if navigateBackFn(m) {
			m.rebuildAllFromSnapshot()
		}
		return *m, nil, true

	case tea.KeyEnter:
		if m.activePane == clusterstate.ResourcesPane {
			model, cmd := m.navigateInto()
			return model, cmd, true
		}
		return *m, nil, true

	case tea.KeyUp, tea.KeyDown:
		if m.activePane == clusterstate.ResourcesPane {
			var cmd tea.Cmd
			m.resourcesTable, cmd = m.resourcesTable.Update(msg)
			m.updateEventsForSelection()
			m.rebuildEventsTable()
			return *m, cmd, true
		}
		var cmd tea.Cmd
		m.eventsTable, cmd = m.eventsTable.Update(msg)
		return *m, cmd, true
	}

	return *m, nil, false
}

// podViewHandleKey handles keys for PodView, scrolling the viewport on Up/Down
// instead of updating the resources table.
func podViewHandleKey(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyEsc:
		if podViewNavigateBack(m) {
			m.rebuildAllFromSnapshot()
		}
		return *m, nil, true

	case tea.KeyEnter:
		return *m, nil, true

	case tea.KeyUp, tea.KeyDown:
		if m.activePane == clusterstate.ResourcesPane {
			var cmd tea.Cmd
			m.podViewport, cmd = m.podViewport.Update(msg)
			return *m, cmd, true
		}
		var cmd tea.Cmd
		m.eventsTable, cmd = m.eventsTable.Update(msg)
		return *m, cmd, true
	}

	return *m, nil, false
}

// hierarchyViewWithPodActions extends hierarchyViewBase with LogsExecutor + ShellExecutor.
type hierarchyViewWithPodActions struct {
	hierarchyViewBase
	logsExecFn  func(m *Model) (tea.Model, tea.Cmd)
	shellExecFn func(m *Model) (tea.Model, tea.Cmd)
}

func (h hierarchyViewWithPodActions) LogsExec(m *Model) (tea.Model, tea.Cmd)  { return h.logsExecFn(m) }
func (h hierarchyViewWithPodActions) ShellExec(m *Model) (tea.Model, tea.Cmd) { return h.shellExecFn(m) }

var hierarchyPanes = []clusterstate.Pane{clusterstate.ResourcesPane, clusterstate.EventsPane}

func init() {
	// ---- ForestView ----
	registerViewBehavior(clusterstate.ForestView, hierarchyViewWithPodActions{
		hierarchyViewBase: hierarchyViewBase{
			viewKeyFn:       func(vs clusterstate.ViewState) string { return "forest" },
			navigateBackFn:  forestNavigateBack,
			rebuildEventsFn: forestRebuildEvents,
		},
		logsExecFn:  podRowLogsExec,
		shellExecFn: podRowShellExec,
	})

	// ---- PodCliqueSetView ----
	registerViewBehavior(clusterstate.PodCliqueSetView, hierarchyViewBase{
		viewKeyFn: func(vs clusterstate.ViewState) string {
			return "PodCliqueSet/" + vs.SelectedPodCliqueSet
		},
		navigateBackFn:  pcsNavigateBack,
		rebuildEventsFn: pcsRebuildEvents,
	})

	// ---- PodCliqueSetReplicaView ----
	registerViewBehavior(clusterstate.PodCliqueSetReplicaView, hierarchyViewBase{
		viewKeyFn: func(vs clusterstate.ViewState) string {
			return "PodCliqueSetReplica/" + vs.SelectedPodCliqueSet + "/" + vs.SelectedReplicaIndex
		},
		navigateBackFn:  pcsReplicaNavigateBack,
		rebuildEventsFn: pcsReplicaRebuildEvents,
	})

	// ---- PodCliqueScalingGroupView ----
	registerViewBehavior(clusterstate.PodCliqueScalingGroupView, hierarchyViewBase{
		viewKeyFn: func(vs clusterstate.ViewState) string {
			return "PodCliqueScalingGroup/" + vs.SelectedScalingGroup
		},
		navigateBackFn:  pcsgNavigateBack,
		rebuildEventsFn: pcsgRebuildEvents,
	})

	// ---- PodCliqueScalingGroupReplicaView ----
	registerViewBehavior(clusterstate.PodCliqueScalingGroupReplicaView, hierarchyViewBase{
		viewKeyFn: func(vs clusterstate.ViewState) string {
			return "PodCliqueScalingGroupReplica/" + vs.SelectedScalingGroup + "/" + vs.SelectedPCSGReplicaIndex
		},
		navigateBackFn:  pcsgReplicaNavigateBack,
		rebuildEventsFn: pcsgReplicaRebuildEvents,
	})

	// ---- PodCliqueView ----
	registerViewBehavior(clusterstate.PodCliqueView, hierarchyViewWithPodActions{
		hierarchyViewBase: hierarchyViewBase{
			viewKeyFn: func(vs clusterstate.ViewState) string {
				return "PodClique/" + vs.SelectedPodClique
			},
			navigateBackFn:  pcNavigateBack,
			rebuildEventsFn: pcRebuildEvents,
		},
		logsExecFn:  podRowLogsExec,
		shellExecFn: podRowShellExec,
	})

	// ---- PodView ----
	registerViewBehavior(clusterstate.PodView, hierarchyViewBase{
		viewKeyFn:       func(vs clusterstate.ViewState) string { return "" },
		navigateBackFn:  podViewNavigateBack,
		rebuildEventsFn: podViewRebuildEvents,
		handleKeyFn:     podViewHandleKey,
	})

	// ---- ContainersView ----
	registerViewBehavior(clusterstate.ContainersView, hierarchyViewWithPodActions{
		hierarchyViewBase: hierarchyViewBase{
			viewKeyFn:       func(vs clusterstate.ViewState) string { return "" },
			navigateBackFn:  containersNavigateBack,
			rebuildEventsFn: containersRebuildEvents,
		},
		logsExecFn:  containersLogsExec,
		shellExecFn: containersShellExec,
	})
}

// =============================================================================
// RenderPanes — shared by all 8 hierarchy views
// =============================================================================

func hierarchyRenderPanes(m *Model, resourcesH, eventsH int) []string {
	return []string{
		m.renderResourcesFrame(resourcesH),
		m.renderEventsFrame(eventsH),
	}
}

// =============================================================================
// NavigateBack — one per hierarchy view
// =============================================================================

func forestNavigateBack(m *Model) bool {
	if m.forestResourceType != "pcs" {
		debugLogWithContext("navigateBack: resetting forestResourceType from %q to pcs", m.forestResourceType)
		m.forestResourceType = "pcs"
		return true
	}
	debugLogWithContext("navigateBack: already at ForestView/pcs, ignoring")
	return false
}

func pcsNavigateBack(m *Model) bool {
	m.viewState.ViewType = clusterstate.ForestView
	m.viewState.ClearBelow(clusterstate.ForestView)
	m.cachedTopologyInfo = nil
	debugLogStateTransition(clusterstate.PodCliqueSetView, clusterstate.ForestView, "")
	return true
}

func pcsReplicaNavigateBack(m *Model) bool {
	pcsKey := "PodCliqueSet/" + m.viewState.SelectedPodCliqueSet
	pcsResources := m.allResources[pcsKey]

	if len(pcsResources) == 1 {
		m.viewState.ViewType = clusterstate.ForestView
		m.viewState.ClearBelow(clusterstate.ForestView)
		m.cachedTopologyInfo = nil
		debugLogStateTransition(clusterstate.PodCliqueSetReplicaView, clusterstate.ForestView, "single replica skip")
	} else {
		m.viewState.ViewType = clusterstate.PodCliqueSetView
		m.viewState.ClearBelow(clusterstate.PodCliqueSetView)
		debugLogStateTransition(clusterstate.PodCliqueSetReplicaView, clusterstate.PodCliqueSetView, "")
	}
	return true
}

func pcsgNavigateBack(m *Model) bool {
	if m.viewState.SelectedPodCliqueSet == "" {
		m.viewState.ViewType = clusterstate.ForestView
		debugLogStateTransition(clusterstate.PodCliqueScalingGroupView, clusterstate.ForestView, "no PCS context")
	} else {
		m.viewState.ViewType = clusterstate.PodCliqueSetReplicaView
		debugLogStateTransition(clusterstate.PodCliqueScalingGroupView, clusterstate.PodCliqueSetReplicaView, "")
	}
	m.viewState.ClearBelow(clusterstate.PodCliqueSetReplicaView)
	return true
}

func pcsgReplicaNavigateBack(m *Model) bool {
	if m.viewState.SelectedPodCliqueSet == "" {
		m.viewState.ViewType = clusterstate.ForestView
		m.viewState.ClearBelow(clusterstate.PodCliqueSetReplicaView)
		debugLogStateTransition(clusterstate.PodCliqueScalingGroupReplicaView, clusterstate.ForestView, "no PCS context")
	} else {
		pcsgKey := "PodCliqueScalingGroup/" + m.viewState.SelectedScalingGroup
		pcsgResources := m.allResources[pcsgKey]

		if len(pcsgResources) == 1 {
			m.viewState.ViewType = clusterstate.PodCliqueSetReplicaView
			m.viewState.ClearBelow(clusterstate.PodCliqueSetReplicaView)
			debugLogStateTransition(clusterstate.PodCliqueScalingGroupReplicaView, clusterstate.PodCliqueSetReplicaView, "single PCSG replica skip")
		} else {
			m.viewState.ViewType = clusterstate.PodCliqueScalingGroupView
			m.viewState.ClearBelow(clusterstate.PodCliqueScalingGroupView)
			debugLogStateTransition(clusterstate.PodCliqueScalingGroupReplicaView, clusterstate.PodCliqueScalingGroupView, "")
		}
	}
	return true
}

func pcNavigateBack(m *Model) bool {
	var newViewType clusterstate.ViewType
	if m.viewState.SelectedScalingGroup != "" && m.viewState.SelectedPCSGReplicaIndex != "" {
		newViewType = clusterstate.PodCliqueScalingGroupReplicaView
	} else if m.viewState.SelectedScalingGroup != "" {
		newViewType = clusterstate.PodCliqueScalingGroupView
	} else if m.viewState.SelectedPodCliqueSet != "" {
		newViewType = clusterstate.PodCliqueSetReplicaView
	} else {
		newViewType = clusterstate.ForestView
	}
	m.viewState.ViewType = newViewType
	m.viewState.ClearBelow(clusterstate.PodCliqueScalingGroupReplicaView)
	debugLogStateTransition(clusterstate.PodCliqueView, newViewType, "")
	return true
}

func containersNavigateBack(m *Model) bool {
	if m.viewState.SelectedPodClique != "" {
		m.viewState.ViewType = clusterstate.PodCliqueView
	} else {
		m.viewState.ViewType = clusterstate.ForestView
	}
	m.viewState.ClearBelow(clusterstate.PodCliqueView)
	m.containerInfos = nil
	debugLogStateTransition(clusterstate.ContainersView, m.viewState.ViewType, "")
	return true
}

func podViewNavigateBack(m *Model) bool {
	if m.viewState.SelectedPodClique != "" {
		m.viewState.ViewType = clusterstate.PodCliqueView
	} else {
		m.viewState.ViewType = clusterstate.ForestView
	}
	m.viewState.ClearBelow(clusterstate.PodCliqueView)
	debugLogStateTransition(clusterstate.PodView, m.viewState.ViewType, "")
	return true
}

// =============================================================================
// RebuildEvents — one per hierarchy view
// =============================================================================

// selectedRowInfo reads the selected row from the resources table and returns
// the resource type and name. Returns ("", "", false) if no valid row is selected.
func selectedRowInfo(m *Model) (resourceType, resourceName string, ok bool) {
	row := m.resourcesTable.SelectedRow()
	if len(row) >= 3 {
		return row[1], row[2], true
	}
	return "", "", false
}

func forestRebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) {
	resType, resName, ok := selectedRowInfo(m)
	if ok {
		switch resType {
		case clusterstate.ResourceTypePodCliqueSet:
			m.allEvents = snapshot.GetEventsForPCS(resName)
		case clusterstate.ResourceTypePCSG:
			m.allEvents = snapshot.GetEventsForPCSG(resName)
		case clusterstate.ResourceTypePodClique:
			m.allEvents = snapshot.GetEventsForPodClique(resName)
		case clusterstate.ResourceTypePod:
			m.allEvents = snapshot.EventsByObject[clusterstate.ResourceTypePod+"/"+resName]
		default:
			m.allEvents = nil
		}
	} else if m.forestResourceType == "pcs" && len(snapshot.PodCliqueSets) > 0 {
		m.allEvents = snapshot.GetEventsForPCS(snapshot.PodCliqueSets[0].Name)
	} else {
		m.allEvents = nil
	}
}

func pcsRebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) {
	resType, resName, ok := selectedRowInfo(m)
	if ok && resType == clusterstate.ResourceTypePCSReplica {
		replicaIndex := extractReplicaIndex(resName)
		m.allEvents = snapshot.GetEventsForReplica(m.viewState.SelectedPodCliqueSet, replicaIndex)
	} else {
		m.allEvents = snapshot.GetEventsForPCS(m.viewState.SelectedPodCliqueSet)
	}
}

func pcsReplicaRebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) {
	resType, resName, ok := selectedRowInfo(m)
	if ok {
		switch resType {
		case clusterstate.ResourceTypePCSG:
			m.allEvents = snapshot.GetEventsForPCSG(resName)
		case clusterstate.ResourceTypePodClique:
			m.allEvents = snapshot.GetEventsForPodClique(resName)
		default:
			m.allEvents = snapshot.GetEventsForReplica(m.viewState.SelectedPodCliqueSet, m.viewState.SelectedReplicaIndex)
		}
	} else {
		m.allEvents = snapshot.GetEventsForReplica(m.viewState.SelectedPodCliqueSet, m.viewState.SelectedReplicaIndex)
	}
}

func pcsgRebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) {
	resType, resName, ok := selectedRowInfo(m)
	if ok && resType == clusterstate.ResourceTypePCSGReplica {
		replicaIndex := extractReplicaIndex(resName)
		m.allEvents = snapshot.GetEventsForPCSGReplica(m.viewState.SelectedScalingGroup, replicaIndex)
	} else {
		m.allEvents = snapshot.GetEventsForPCSG(m.viewState.SelectedScalingGroup)
	}
}

func pcsgReplicaRebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) {
	resType, resName, ok := selectedRowInfo(m)
	if ok && resType == clusterstate.ResourceTypePodClique {
		m.allEvents = snapshot.GetEventsForPodClique(resName)
	} else {
		m.allEvents = snapshot.GetEventsForPCSGReplica(m.viewState.SelectedScalingGroup, m.viewState.SelectedPCSGReplicaIndex)
	}
}

func pcRebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) {
	m.allEvents = snapshot.GetEventsForPodClique(m.viewState.SelectedPodClique)
}

func containersRebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) {
	m.allEvents = snapshot.EventsByObject[clusterstate.ResourceTypePod+"/"+m.viewState.SelectedPod]
}

func podViewRebuildEvents(m *Model, snapshot *clusterstate.CacheSnapshot) {
	m.allEvents = snapshot.GetEventsForPodClique(m.viewState.SelectedPodClique)
}

// =============================================================================
// LogsExec — shared "pod row" version + ContainersView version
// =============================================================================

// podRowLogsExec handles 'l' for views where a Pod row may be selected
// (ForestView, PodCliqueView).
func podRowLogsExec(m *Model) (tea.Model, tea.Cmd) {
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 || selectedRow[1] != clusterstate.ResourceTypePod {
		return *m, nil
	}
	podName := selectedRow[2]
	namespace := selectedRow[0]
	return *m, fetchFirstContainerForLogsCmd(m.ctx, m.cache, podName, namespace)
}

// containersLogsExec handles 'l' in ContainersView (open logs for selected container).
func containersLogsExec(m *Model) (tea.Model, tea.Cmd) {
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		return *m, nil
	}
	containerName := selectedRow[0]
	namespace := m.resolveNamespace()

	m.openLogsOverlay(m.viewState.SelectedPod, containerName, namespace)

	debugLogWithContext("opening logs overlay: pod=%s container=%s", m.viewState.SelectedPod, containerName)
	return *m, loadPodLogsCmd(m.ctx, m.cache, m.viewState.SelectedPod, namespace, containerName, 1000)
}

// =============================================================================
// ShellExec — shared "pod row" version + ContainersView version
// =============================================================================

// podRowShellExec handles 's' for views where a Pod row may be selected
// (ForestView, PodCliqueView).
func podRowShellExec(m *Model) (tea.Model, tea.Cmd) {
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 || selectedRow[1] != clusterstate.ResourceTypePod {
		return *m, nil
	}
	podName := selectedRow[2]
	namespace := selectedRow[0]
	return *m, fetchFirstRunningContainerCmd(m.ctx, m.cache, podName, namespace)
}

// containersShellExec handles 's' in ContainersView (shell into selected container).
func containersShellExec(m *Model) (tea.Model, tea.Cmd) {
	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		return *m, nil
	}
	containerName := selectedRow[0]
	containerState := selectedRow[2]
	if containerState != "Running" {
		m.addError(fmt.Sprintf("Cannot shell into container %q — state is %s", containerName, containerState))
		return *m, nil
	}
	namespace := m.resolveNamespace()
	c := exec.Command("kubectl", "exec", "-it", m.viewState.SelectedPod, "-n", namespace, "-c", containerName, "--", "/bin/sh") //nolint:gosec // user-initiated shell exec
	return *m, tea.ExecProcess(c, func(err error) tea.Msg {
		return ShellExitMsg{Err: err}
	})
}
