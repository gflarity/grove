package clusterstate

import "testing"

func TestTopologyDrillStack_ZeroValue(t *testing.T) {
	var s TopologyDrillStack
	if !s.IsEmpty() {
		t.Error("zero-value stack should be empty")
	}
	if s.Depth() != 0 {
		t.Errorf("expected depth 0, got %d", s.Depth())
	}
	if s.Entries() != nil {
		t.Error("expected nil entries for zero-value stack")
	}
	if _, ok := s.LastEntry(); ok {
		t.Error("expected LastEntry to return false for empty stack")
	}
	if b := s.Breadcrumb(); b != "" {
		t.Errorf("expected empty breadcrumb, got %q", b)
	}
}

func TestTopologyDrillStack_PushDomain(t *testing.T) {
	var s TopologyDrillStack
	s.PushDomain("region", "topology.kubernetes.io/region")

	if s.IsEmpty() {
		t.Error("stack should not be empty after PushDomain")
	}
	if s.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", s.Depth())
	}
	entry, ok := s.LastEntry()
	if !ok {
		t.Fatal("expected LastEntry to return true")
	}
	if entry.Domain != "region" || entry.Key != "topology.kubernetes.io/region" || entry.Value != "" {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

var testDomains = []TopologyDomainRow{
	{Domain: "region", Key: "topology.kubernetes.io/region", ValuesCount: 2},
	{Domain: "zone", Key: "topology.kubernetes.io/zone", ValuesCount: 4},
	{Domain: "rack", Key: "nvidia.com/gpu.machine", ValuesCount: 8},
}

func TestTopologyDrillStack_SelectValueAndAdvance(t *testing.T) {
	var s TopologyDrillStack
	s.PushDomain("region", "topology.kubernetes.io/region")

	if !s.SelectValueAndAdvance("us-east-1", testDomains) {
		t.Fatal("expected SelectValueAndAdvance to succeed")
	}
	if s.Depth() != 2 {
		t.Errorf("expected depth 2, got %d", s.Depth())
	}
	entries := s.Entries()
	if entries[0].Value != "us-east-1" {
		t.Errorf("expected first entry value 'us-east-1', got %q", entries[0].Value)
	}
	if entries[1].Domain != "zone" {
		t.Errorf("expected second entry domain 'zone', got %q", entries[1].Domain)
	}
}

func TestTopologyDrillStack_SelectValueAndAdvance_AtNarrowest(t *testing.T) {
	var s TopologyDrillStack
	s.PushDomain("rack", "nvidia.com/gpu.machine")

	// rack is the narrowest domain — should fail
	if s.SelectValueAndAdvance("rack-1", testDomains) {
		t.Fatal("expected SelectValueAndAdvance to fail at narrowest domain")
	}
	// Value should be reverted
	entry, _ := s.LastEntry()
	if entry.Value != "" {
		t.Errorf("expected value reverted to empty, got %q", entry.Value)
	}
}

func TestTopologyDrillStack_DrillBack_ClearValue(t *testing.T) {
	var s TopologyDrillStack
	s.PushDomain("region", "topology.kubernetes.io/region")
	s.SelectValueAndAdvance("us-east-1", testDomains)
	// Stack: [region=us-east-1, zone(empty)]

	// First back: clear zone entry (has no value), pop it, clear region's value
	s.DrillBack()
	if s.Depth() != 1 {
		t.Fatalf("expected depth 1, got %d", s.Depth())
	}
	entry, _ := s.LastEntry()
	if entry.Value != "" {
		t.Errorf("expected region value cleared, got %q", entry.Value)
	}
}

func TestTopologyDrillStack_DrillBack_PopEmpty(t *testing.T) {
	var s TopologyDrillStack
	s.PushDomain("region", "topology.kubernetes.io/region")

	// Stack: [region(empty)] — back should pop it
	s.DrillBack()
	if !s.IsEmpty() {
		t.Errorf("expected empty stack after DrillBack, got depth %d", s.Depth())
	}
}

func TestTopologyDrillStack_DrillBack_HasValue(t *testing.T) {
	s := NewTopologyDrillStack([]TopologyDrillSelection{
		{Domain: "region", Key: "k", Value: "us-east-1"},
	})

	// Last entry has a value — just clear the value
	s.DrillBack()
	if s.Depth() != 1 {
		t.Fatalf("expected depth 1, got %d", s.Depth())
	}
	entry, _ := s.LastEntry()
	if entry.Value != "" {
		t.Errorf("expected value cleared, got %q", entry.Value)
	}
}

func TestTopologyDrillStack_CurrentDomain(t *testing.T) {
	tests := []struct {
		name           string
		entries        []TopologyDrillSelection
		wantDomain     string
		wantKey        string
	}{
		{
			name:       "empty stack",
			entries:    nil,
			wantDomain: "",
			wantKey:    "",
		},
		{
			name: "last entry no value — return that domain",
			entries: []TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: ""},
			},
			wantDomain: "region",
			wantKey:    "topology.kubernetes.io/region",
		},
		{
			name: "last entry has value — return next domain",
			entries: []TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
			},
			wantDomain: "zone",
			wantKey:    "topology.kubernetes.io/zone",
		},
		{
			name: "at narrowest with value — return empty",
			entries: []TopologyDrillSelection{
				{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
				{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: "z1"},
				{Domain: "rack", Key: "nvidia.com/gpu.machine", Value: "r1"},
			},
			wantDomain: "",
			wantKey:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewTopologyDrillStack(tt.entries)
			domain, key := s.CurrentDomain(testDomains)
			if domain != tt.wantDomain || key != tt.wantKey {
				t.Errorf("CurrentDomain() = (%q, %q), want (%q, %q)", domain, key, tt.wantDomain, tt.wantKey)
			}
		})
	}
}

func TestTopologyDrillStack_Validate_DomainRemoved(t *testing.T) {
	s := NewTopologyDrillStack([]TopologyDrillSelection{
		{Domain: "region", Key: "k1", Value: "us-east-1"},
		{Domain: "zone", Key: "k2", Value: ""},
	})

	// Remove "zone" from domains
	domains := []TopologyDomainRow{
		{Domain: "region", Key: "k1"},
	}
	s.Validate(domains, nil)

	if s.Depth() != 1 || s.Entries()[0].Domain != "region" {
		t.Errorf("expected stack truncated to [region], got entries: %+v", s.Entries())
	}
}

func TestTopologyDrillStack_Validate_ValueRemoved(t *testing.T) {
	s := NewTopologyDrillStack([]TopologyDrillSelection{
		{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-west-2"},
		{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: ""},
	})

	domains := []TopologyDomainRow{
		{Domain: "region", Key: "topology.kubernetes.io/region"},
		{Domain: "zone", Key: "topology.kubernetes.io/zone"},
	}
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.kubernetes.io/region": "us-east-1"},
	}

	// "us-west-2" doesn't exist in node labels → truncate at region entry
	s.Validate(domains, nodeLabels)

	if s.Depth() != 0 {
		t.Errorf("expected empty stack, got depth %d", s.Depth())
	}
}

func TestTopologyDrillStack_Validate_AllValid(t *testing.T) {
	s := NewTopologyDrillStack([]TopologyDrillSelection{
		{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
		{Domain: "zone", Key: "topology.kubernetes.io/zone", Value: ""},
	})

	domains := []TopologyDomainRow{
		{Domain: "region", Key: "topology.kubernetes.io/region"},
		{Domain: "zone", Key: "topology.kubernetes.io/zone"},
	}
	nodeLabels := map[string]map[string]string{
		"node-1": {
			"topology.kubernetes.io/region": "us-east-1",
			"topology.kubernetes.io/zone":   "z1",
		},
	}

	s.Validate(domains, nodeLabels)

	if s.Depth() != 2 {
		t.Errorf("expected depth 2 preserved, got %d", s.Depth())
	}
}

func TestTopologyDrillStack_Breadcrumb(t *testing.T) {
	tests := []struct {
		name    string
		entries []TopologyDrillSelection
		want    string
	}{
		{"empty", nil, ""},
		{
			"single with value",
			[]TopologyDrillSelection{{Domain: "region", Key: "k", Value: "us-east-1"}},
			"region=us-east-1",
		},
		{
			"multiple with values",
			[]TopologyDrillSelection{
				{Domain: "region", Key: "k1", Value: "us-east-1"},
				{Domain: "zone", Key: "k2", Value: "z1"},
			},
			"region=us-east-1 > zone=z1",
		},
		{
			"last entry no value",
			[]TopologyDrillSelection{
				{Domain: "region", Key: "k1", Value: "us-east-1"},
				{Domain: "zone", Key: "k2", Value: ""},
			},
			"region=us-east-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewTopologyDrillStack(tt.entries)
			if got := s.Breadcrumb(); got != tt.want {
				t.Errorf("Breadcrumb() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTopologyDrillStack_Reset(t *testing.T) {
	s := NewTopologyDrillStack([]TopologyDrillSelection{
		{Domain: "region", Key: "k", Value: "v"},
	})
	s.Reset()
	if !s.IsEmpty() {
		t.Error("expected empty stack after Reset")
	}
	if s.Entries() != nil {
		t.Error("expected nil entries after Reset")
	}
}

func TestTopologyDrillStack_MatchingNodes(t *testing.T) {
	nodeLabels := map[string]map[string]string{
		"node-1": {"topology.kubernetes.io/region": "us-east-1"},
		"node-2": {"topology.kubernetes.io/region": "us-west-2"},
	}

	s := NewTopologyDrillStack([]TopologyDrillSelection{
		{Domain: "region", Key: "topology.kubernetes.io/region", Value: "us-east-1"},
	})

	nodes := s.MatchingNodes(nodeLabels)
	if len(nodes) != 1 || nodes[0] != "node-1" {
		t.Errorf("expected [node-1], got %v", nodes)
	}
}
