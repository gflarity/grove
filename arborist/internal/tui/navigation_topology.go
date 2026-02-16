package tui

// topologyDrillInto pushes to the drill stack based on the current selection.
func (m *Model) topologyDrillInto() {
	if m.topologyViewData == nil {
		return
	}

	if m.topologyDrill.IsEmpty() {
		selectedRow := m.topologyDomainsTable.SelectedRow()
		if len(selectedRow) < 2 {
			return
		}

		domain := selectedRow[0]
		key := selectedRow[1]

		m.topologyDrill.PushDomain(domain, key)
		debugLogWithContext("topology drill into domain: %s (key: %s)", domain, key)
	} else {
		selectedRow := m.topologyDomainsTable.SelectedRow()
		if len(selectedRow) < 1 {
			return
		}

		value := selectedRow[0]
		if !m.topologyDrill.SelectValueAndAdvance(value, m.topologyViewData.Domains) {
			debugLogWithContext("topology drill: at narrowest domain, no-op")
			return
		}
		debugLogWithContext("topology drill into value %q, advancing to next domain", value)
	}

	m.rebuildTopologyDomainsTable()
	m.rebuildTopologyPodsTable()
}

// topologyDrillBack pops the last entry from the drill stack.
// When the last entry has no value (showing values for a domain), popping it
// also clears the previous entry's value so the user sees the parent domain's
// values list in a single Esc press. Without this, the intermediate state
// (previous entry still has a value) causes currentTopologyDomain to return
// the same domain again, making Esc appear to do nothing.
func (m *Model) topologyDrillBack() {
	if m.topologyDrill.IsEmpty() {
		return
	}

	m.topologyDrill.DrillBack()

	debugLogWithContext("topology drill back, stack depth now: %d", m.topologyDrill.Depth())
	m.rebuildTopologyDomainsTable()
	m.rebuildTopologyPodsTable()
}

// topologyBreadcrumbString generates a breadcrumb like "region=us-east-1 > zone=us-east-1a".
func (t *TopologyState) topologyBreadcrumbString() string {
	return t.topologyDrill.Breadcrumb()
}

// validateTopologyDrillStack checks that the current drill stack is still valid.
// It verifies both that each domain still exists and that each selected value
// still exists within that domain. If any entry is stale, the stack is truncated
// up to (but not including) the invalid entry.
func (t *TopologyState) validateTopologyDrillStack() {
	if t.topologyViewData == nil || t.topologyDrill.IsEmpty() {
		return
	}

	depthBefore := t.topologyDrill.Depth()
	t.topologyDrill.Validate(t.topologyViewData.Domains, t.topologyViewData.NodeLabels)
	if t.topologyDrill.Depth() != depthBefore {
		debugLogWithContext("drill stack truncated from depth %d to %d", depthBefore, t.topologyDrill.Depth())
	}
}

