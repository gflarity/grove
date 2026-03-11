package tui

import (
	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	tea "github.com/charmbracelet/bubbletea"
)

// topologyView implements only ViewBehavior (no BackNavigator, LogsExecutor, or ShellExecutor).
type topologyView struct{}

func (topologyView) ViewKey(_ clusterstate.ViewState) string                     { return "" }
func (topologyView) PaneList() []clusterstate.Pane                               { return []clusterstate.Pane{clusterstate.TopologyDomainsPane, clusterstate.TopologyPodsPane} }
func (topologyView) RebuildEvents(m *Model, _ *clusterstate.CacheSnapshot)       { m.allEvents = nil }
func (topologyView) RenderPanes(m *Model, resourcesH, eventsH int) []string {
	sections := []string{
		m.renderTopologyDomainsFrame(resourcesH),
		m.renderTopologyPodsFrame(eventsH),
	}
	if m.topologyHasGPUColumns() {
		sections = append(sections, m.renderTopologyFootnote())
	}
	return sections
}

func (topologyView) HandleKey(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyEsc:
		if m.topologyDrill.IsEmpty() {
			m.viewState.ViewType = clusterstate.ForestView
			m.activePane = clusterstate.ResourcesPane
			m.updateTableFocus()
			debugLogWithContext("switched from TopologyView to ForestView via Esc")
			return *m, nil, true
		}
		m.topologyDrillBack()
		return *m, nil, true

	case tea.KeyEnter:
		if m.activePane == clusterstate.TopologyDomainsPane {
			m.topologyDrillInto()
			return *m, nil, true
		}
		return *m, nil, true

	case tea.KeyUp, tea.KeyDown:
		if m.activePane == clusterstate.TopologyDomainsPane {
			var cmd tea.Cmd
			m.topologyDomainsTable, cmd = m.topologyDomainsTable.Update(msg)
			m.rebuildTopologyPodsTable()
			return *m, cmd, true
		}
		var cmd tea.Cmd
		m.topologyPodsTable, cmd = m.topologyPodsTable.Update(msg)
		return *m, cmd, true
	}

	return *m, nil, false
}

func init() {
	registerViewBehavior(clusterstate.TopologyView, topologyView{})
}
