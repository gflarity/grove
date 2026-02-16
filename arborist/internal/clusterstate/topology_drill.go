package clusterstate

import "strings"

// TopologyDrillStack encapsulates the topology drill-down state,
// providing operations for drilling into/out of topology domains.
type TopologyDrillStack struct {
	entries []TopologyDrillSelection
}

// NewTopologyDrillStack creates a TopologyDrillStack with the given entries.
func NewTopologyDrillStack(entries []TopologyDrillSelection) TopologyDrillStack {
	return TopologyDrillStack{entries: entries}
}

// IsEmpty returns true if no drill-down is active.
func (s TopologyDrillStack) IsEmpty() bool {
	return len(s.entries) == 0
}

// Depth returns the number of entries in the drill stack.
func (s TopologyDrillStack) Depth() int {
	return len(s.entries)
}

// Entries returns a copy of the drill selections.
func (s TopologyDrillStack) Entries() []TopologyDrillSelection {
	if s.entries == nil {
		return nil
	}
	result := make([]TopologyDrillSelection, len(s.entries))
	copy(result, s.entries)
	return result
}

// LastEntry returns the last entry in the stack, or false if empty.
func (s TopologyDrillStack) LastEntry() (TopologyDrillSelection, bool) {
	if len(s.entries) == 0 {
		return TopologyDrillSelection{}, false
	}
	return s.entries[len(s.entries)-1], true
}

// Reset clears the drill stack.
func (s *TopologyDrillStack) Reset() {
	s.entries = nil
}

// PushDomain adds a new domain entry to the stack (no value selected yet).
func (s *TopologyDrillStack) PushDomain(domain, key string) {
	s.entries = append(s.entries, TopologyDrillSelection{
		Domain: domain,
		Key:    key,
		Value:  "",
	})
}

// SelectValueAndAdvance sets the value on the current (last) entry and
// pushes the next domain in the hierarchy. Returns false if already at the
// narrowest domain (no-op in that case).
func (s *TopologyDrillStack) SelectValueAndAdvance(value string, domains []TopologyDomainRow) bool {
	if len(s.entries) == 0 {
		return false
	}
	s.entries[len(s.entries)-1].Value = value

	currentDomain := s.entries[len(s.entries)-1].Domain

	nextDomain := ""
	nextKey := ""
	for i, d := range domains {
		if d.Domain == currentDomain && i+1 < len(domains) {
			nextDomain = domains[i+1].Domain
			nextKey = domains[i+1].Key
			break
		}
	}

	if nextDomain == "" {
		// At narrowest domain, revert
		s.entries[len(s.entries)-1].Value = ""
		return false
	}

	s.entries = append(s.entries, TopologyDrillSelection{
		Domain: nextDomain,
		Key:    nextKey,
		Value:  "",
	})
	return true
}

// DrillBack goes up one level in the drill-down hierarchy.
// When the last entry has no value (showing values for a domain), popping it
// also clears the previous entry's value so the user sees the parent domain's
// values list in a single back action.
func (s *TopologyDrillStack) DrillBack() {
	if len(s.entries) == 0 {
		return
	}

	lastEntry := s.entries[len(s.entries)-1]

	if lastEntry.Value == "" {
		s.entries = s.entries[:len(s.entries)-1]
		if len(s.entries) > 0 {
			s.entries[len(s.entries)-1].Value = ""
		}
	} else {
		s.entries[len(s.entries)-1].Value = ""
	}
}

// CurrentDomain returns the domain name and label key for the current
// drill-down level. If drilled into a domain but no value selected yet, it
// returns that domain. If a value was selected, it returns the next domain
// in the hierarchy. Returns ("", "") if the stack is empty or at the narrowest.
func (s TopologyDrillStack) CurrentDomain(domains []TopologyDomainRow) (string, string) {
	if len(s.entries) == 0 {
		return "", ""
	}

	lastEntry := s.entries[len(s.entries)-1]

	if lastEntry.Value == "" {
		return lastEntry.Domain, lastEntry.Key
	}

	for i, d := range domains {
		if d.Domain == lastEntry.Domain && i+1 < len(domains) {
			next := domains[i+1]
			return next.Domain, next.Key
		}
	}

	return "", ""
}

// Validate checks that the current drill stack is still valid against the given
// topology data. It verifies both that each domain still exists and that each
// selected value still exists within that domain. If any entry is stale, the
// stack is truncated up to (but not including) the invalid entry.
func (s *TopologyDrillStack) Validate(domains []TopologyDomainRow, nodeLabels map[string]map[string]string) {
	if len(s.entries) == 0 {
		return
	}

	domainSet := make(map[string]bool)
	for _, d := range domains {
		domainSet[d.Domain] = true
	}

	for i, entry := range s.entries {
		if !domainSet[entry.Domain] {
			s.entries = s.entries[:i]
			if len(s.entries) == 0 {
				s.entries = nil
			}
			return
		}
		if entry.Value != "" && entry.Key != "" && len(nodeLabels) > 0 {
			matchingNodes := FilterNodesByBreadcrumb(nodeLabels, s.entries[:i])
			values := DistinctValuesForDomain(nodeLabels, entry.Key, matchingNodes)
			found := false
			for _, v := range values {
				if v == entry.Value {
					found = true
					break
				}
			}
			if !found {
				s.entries = s.entries[:i]
				if len(s.entries) == 0 {
					s.entries = nil
				}
				return
			}
		}
	}
}

// MatchingNodes returns the nodes matching the current breadcrumb constraints.
func (s TopologyDrillStack) MatchingNodes(nodeLabels map[string]map[string]string) []string {
	return FilterNodesByBreadcrumb(nodeLabels, s.entries)
}

// Breadcrumb generates a string like "region=us-east-1 > zone=us-east-1a".
func (s TopologyDrillStack) Breadcrumb() string {
	if len(s.entries) == 0 {
		return ""
	}

	var parts []string
	for _, entry := range s.entries {
		if entry.Value != "" {
			parts = append(parts, entry.Domain+"="+entry.Value)
		}
	}

	return strings.Join(parts, " > ")
}
