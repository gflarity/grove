package tui

import (
	"fmt"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
)

// openYAMLOverlay starts loading YAML for the currently selected resource.
func (m Model) openYAMLOverlay() (tea.Model, tea.Cmd) {
	// Determine what resource is selected
	resourceType, resourceName, namespace := m.selectedResourceInfo()
	if resourceType == "" || resourceName == "" {
		debugLogWithContext("openYAMLOverlay: no resource selected")
		return m, nil
	}

	// For virtual replica types, resolve to the actual parent resource
	actualType := resourceType
	actualName := resourceName
	switch resourceType {
	case "(PodCliqueSet replica)":
		actualType = "PodCliqueSet"
		actualName = m.viewState.SelectedPodCliqueSet
	case "(PodCliqueScalingGroup replica)":
		actualType = "PodCliqueScalingGroup"
		actualName = m.viewState.SelectedScalingGroup
	}

	// Set overlay state — show "Loading..." while fetching
	loadingMsg := "# Loading YAML for " + actualType + "/" + actualName + "..."
	m.yamlOverlay.Open(loadingMsg, m.width, m.height)
	m.yamlResourceType = resourceType
	m.yamlResourceName = resourceName
	m.yamlOverlay.Viewport.GotoTop()

	debugLogWithContext("openYAMLOverlay: loading %s/%s (actual: %s/%s)", resourceType, resourceName, actualType, actualName)

	return m, loadResourceYAMLCmd(m.cache, m.ctx, actualType, actualName, namespace)
}

// selectedResourceInfo returns the type, name, and namespace of the currently selected resource.
func (m Model) selectedResourceInfo() (string, string, string) {
	if m.viewState.ViewType == data.TopologyView {
		// In topology view, use the pods table if focused on pods pane
		if m.activePane == data.TopologyPodsPane {
			row := m.topologyPodsTable.SelectedRow()
			if len(row) >= 3 {
				return "Pod", row[2], row[0] // NAME at index 2, NAMESPACE at index 0
			}
		}
		return "", "", ""
	}

	// For PodView or ContainersView, the selected resource is the pod itself
	if m.viewState.ViewType == data.PodView || m.viewState.ViewType == data.ContainersView {
		return "Pod", m.viewState.SelectedPod, m.resolveNamespace()
	}

	selectedRow := m.resourcesTable.SelectedRow()
	if len(selectedRow) < 3 {
		return "", "", ""
	}
	return selectedRow[1], selectedRow[2], selectedRow[0] // TYPE, NAME, NAMESPACE
}

// handleResourceYAML handles ResourceYAMLMsg.
func (m Model) handleResourceYAML(msg ResourceYAMLMsg) (tea.Model, tea.Cmd) {
	if !m.yamlOverlay.Active {
		return m, nil
	}

	if msg.Err != nil {
		debugLogWithContext("ERROR loading resource YAML: %v", msg.Err)
		m.yamlOverlay.Content = fmt.Sprintf("# Error loading YAML for %s/%s: %v", msg.ResourceType, msg.ResourceName, msg.Err)
		m.addError(fmt.Sprintf("Failed to load YAML for %s/%s: %v", msg.ResourceType, msg.ResourceName, msg.Err))
	} else {
		debugLogWithContext("loaded %d bytes of YAML for %s/%s", len(msg.YAML), msg.ResourceType, msg.ResourceName)
		m.yamlOverlay.Content = msg.YAML
	}
	m.yamlOverlay.UpdateViewportContent()
	m.yamlOverlay.Viewport.GotoTop()
	return m, nil
}

// handleYAMLOverlayKey handles keys when the YAML overlay is active.
func (m Model) handleYAMLOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If search is active, delegate to the overlay's search handler
	if m.yamlOverlay.SearchActive {
		needsUpdate, cmd := m.yamlOverlay.HandleSearchKey(msg)
		if needsUpdate {
			m.yamlOverlay.UpdateViewportContent()
			if m.yamlOverlay.SearchText != "" {
				m.yamlOverlay.ApplySearch()
			}
			debugLogWithContext("YAML search %s: %q",
				map[bool]string{true: "applied", false: "cancelled"}[m.yamlOverlay.SearchText != ""],
				m.yamlOverlay.SearchText)
		}
		return m, cmd
	}

	// Delegate common keys to the overlay model
	handled, cmd := m.yamlOverlay.HandleKey(msg)
	if handled {
		if !m.yamlOverlay.Active {
			debugLogWithContext("YAML overlay closed")
		}
		return m, cmd
	}

	return m, nil
}
