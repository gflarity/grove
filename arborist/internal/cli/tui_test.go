package cli

import (
	"testing"
)

func TestResolveArboristVersion_WithLdflagsVersion(t *testing.T) {
	// Save and restore the package-level Version variable
	saved := Version
	defer func() { Version = saved }()

	Version = "v1.2.3"
	got := resolveArboristVersion()
	if got != "v1.2.3" {
		t.Errorf("resolveArboristVersion() = %q, want %q", got, "v1.2.3")
	}
}

func TestResolveArboristVersion_WithWhitespaceVersion(t *testing.T) {
	saved := Version
	defer func() { Version = saved }()

	Version = "  v1.2.3  "
	got := resolveArboristVersion()
	if got != "v1.2.3" {
		t.Errorf("resolveArboristVersion() = %q, want %q (should trim whitespace)", got, "v1.2.3")
	}
}

func TestResolveArboristVersion_EmptyVersionFallsBack(t *testing.T) {
	saved := Version
	defer func() { Version = saved }()

	Version = ""
	got := resolveArboristVersion()
	// In test builds, runtime/debug.ReadBuildInfo() succeeds and the version
	// is typically "(devel)" which causes the function to fall back to VCS info
	// or "dev". Either way, it should not be empty or panic.
	if got == "" {
		t.Error("resolveArboristVersion() returned empty string, expected fallback")
	}
}
