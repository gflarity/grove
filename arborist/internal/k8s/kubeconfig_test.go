package k8s

import (
	"testing"
)

// ---------------------------------------------------------------------------
// containsString
// ---------------------------------------------------------------------------

func TestContainsString(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		s     string
		want  bool
	}{
		{name: "found", slice: []string{"a", "b", "c"}, s: "b", want: true},
		{name: "not found", slice: []string{"a", "b", "c"}, s: "d", want: false},
		{name: "empty slice", slice: []string{}, s: "a", want: false},
		{name: "nil slice", slice: nil, s: "a", want: false},
		{name: "empty string in slice", slice: []string{"", "a"}, s: "", want: true},
		{name: "single element match", slice: []string{"x"}, s: "x", want: true},
		{name: "single element no match", slice: []string{"x"}, s: "y", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsString(tt.slice, tt.s)
			if got != tt.want {
				t.Errorf("containsString(%v, %q) = %v, want %v", tt.slice, tt.s, got, tt.want)
			}
		})
	}
}

// GPU helper tests (gpuRequestsFromPod, gpuCapacityFromNode, gpuProductFromNode)
// live in client_test.go.

// NOTE: FormatAge tests live in data/cache_test.go (data.FormatAge is the canonical implementation).
