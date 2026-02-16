package cli

import (
	"testing"

	"github.com/alecthomas/kong"
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

// parseForestCmd is a test helper that parses args through Kong and returns the ForestCmd.
func parseForestCmd(t *testing.T, args []string) ForestCmd {
	t.Helper()
	var c CLI
	parser, err := kong.New(&c,
		kong.Name("arborist"),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		t.Fatalf("kong.New failed: %v", err)
	}
	_, err = parser.Parse(args)
	if err != nil {
		t.Fatalf("parse(%v) failed: %v", args, err)
	}
	return c.TUI
}

func TestForestCmd_DefaultsViaExplicitTui(t *testing.T) {
	// ./arborist tui → defaults (pcs, all namespaces, no filter)
	fc := parseForestCmd(t, []string{"tui"})
	if fc.Resource != "pcs" {
		t.Errorf("expected default resource 'pcs', got %q", fc.Resource)
	}
	// ForestCmd.Run() ignores AllNamespaces — it always defaults to all-namespaces
	// and only restricts when -n is set. So we don't assert on AllNamespaces here.
	if fc.Namespace != "" {
		t.Errorf("expected empty namespace by default, got %q", fc.Namespace)
	}
	if fc.Filter != "" {
		t.Errorf("expected empty filter by default, got %q", fc.Filter)
	}
}

func TestForestCmd_ExplicitResource(t *testing.T) {
	// ./arborist tui pcsg
	fc := parseForestCmd(t, []string{"tui", "pcsg"})
	if fc.Resource != "pcsg" {
		t.Errorf("expected resource 'pcsg', got %q", fc.Resource)
	}
}

func TestForestCmd_NamespaceFlag(t *testing.T) {
	// ./arborist tui -n gpu-stack
	fc := parseForestCmd(t, []string{"tui", "-n", "gpu-stack"})
	if fc.Namespace != "gpu-stack" {
		t.Errorf("expected namespace 'gpu-stack', got %q", fc.Namespace)
	}
	if fc.Resource != "pcs" {
		t.Errorf("expected default resource 'pcs', got %q", fc.Resource)
	}
}

func TestForestCmd_FilterFlag(t *testing.T) {
	// ./arborist tui pcs -f myapp
	fc := parseForestCmd(t, []string{"tui", "pcs", "-f", "myapp"})
	if fc.Filter != "myapp" {
		t.Errorf("expected filter 'myapp', got %q", fc.Filter)
	}
	if fc.Resource != "pcs" {
		t.Errorf("expected resource 'pcs', got %q", fc.Resource)
	}
}

func TestForestCmd_ResourceWithNamespace(t *testing.T) {
	// ./arborist tui pc -n gpu-stack
	fc := parseForestCmd(t, []string{"tui", "pc", "-n", "gpu-stack"})
	if fc.Resource != "pc" {
		t.Errorf("expected resource 'pc', got %q", fc.Resource)
	}
	if fc.Namespace != "gpu-stack" {
		t.Errorf("expected namespace 'gpu-stack', got %q", fc.Namespace)
	}
}

func TestForestCmd_AllFlags(t *testing.T) {
	// ./arborist tui pod -n default -f myapp
	fc := parseForestCmd(t, []string{"tui", "pod", "-n", "default", "-f", "myapp"})
	if fc.Resource != "pod" {
		t.Errorf("expected resource 'pod', got %q", fc.Resource)
	}
	if fc.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", fc.Namespace)
	}
	if fc.Filter != "myapp" {
		t.Errorf("expected filter 'myapp', got %q", fc.Filter)
	}
}

func TestForestCmd_BareTuiResolvesToDefaults(t *testing.T) {
	// Verify that `arborist tui` resolves to defaults.
	fc := parseForestCmd(t, []string{"tui"})
	if fc.Resource != "pcs" {
		t.Errorf("arborist tui: expected resource 'pcs', got %q", fc.Resource)
	}
}

func TestForestCmd_ImplicitTuiViaResource(t *testing.T) {
	// ./arborist pcsg → default:"withargs" resolves to tui, "pcsg" matches the Resource arg.
	fc := parseForestCmd(t, []string{"pcsg"})
	if fc.Resource != "pcsg" {
		t.Errorf("expected resource 'pcsg', got %q", fc.Resource)
	}
}
