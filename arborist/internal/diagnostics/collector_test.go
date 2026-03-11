package diagnostics

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockOutput implements DiagnosticOutput for testing.
type mockOutput struct {
	sections    []string
	subSections []string
	lines       []string
	tables      int
	yamlWrites  int
	flushed     bool
	flushErr    error
}

func (m *mockOutput) WriteSection(title string) error {
	m.sections = append(m.sections, title)
	return nil
}

func (m *mockOutput) WriteSubSection(title string) error {
	m.subSections = append(m.subSections, title)
	return nil
}

func (m *mockOutput) WriteLine(line string) error {
	m.lines = append(m.lines, line)
	return nil
}

func (m *mockOutput) WriteLinef(format string, args ...any) error {
	m.lines = append(m.lines, fmt.Sprintf(format, args...))
	return nil
}

func (m *mockOutput) WriteYAML(resourceType, resourceName string, yamlContent []byte) error {
	m.yamlWrites++
	return nil
}

func (m *mockOutput) WriteTable(headers []string, rows [][]string) error {
	m.tables++
	return nil
}

func (m *mockOutput) Flush() error {
	m.flushed = true
	return m.flushErr
}

func TestCollectAll_AllSucceed(t *testing.T) {
	callOrder := []string{}
	c := &Collector{
		collectors: []namedCollector{
			{name: "A", fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
				callOrder = append(callOrder, "A")
				return nil
			}},
			{name: "B", fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
				callOrder = append(callOrder, "B")
				return nil
			}},
		},
	}

	ctx := context.Background()
	dc := &DiagnosticContext{
		Namespace:         "test-ns",
		OperatorNamespace: "grove-system",
	}
	out := &mockOutput{}

	err := c.CollectAll(ctx, dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(callOrder) != 2 || callOrder[0] != "A" || callOrder[1] != "B" {
		t.Errorf("expected collectors called in order [A, B], got %v", callOrder)
	}

	if !out.flushed {
		t.Error("expected output to be flushed")
	}

	// Should have header and footer sections
	if len(out.sections) < 2 {
		t.Errorf("expected at least 2 sections (header + footer), got %d", len(out.sections))
	}
}

func TestCollectAll_CollectorErrors(t *testing.T) {
	c := &Collector{
		collectors: []namedCollector{
			{name: "Failing", fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
				return fmt.Errorf("something broke")
			}},
			{name: "Succeeding", fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
				return nil
			}},
		},
	}

	ctx := context.Background()
	dc := &DiagnosticContext{
		Namespace:         "test-ns",
		OperatorNamespace: "grove-system",
	}
	out := &mockOutput{}

	err := c.CollectAll(ctx, dc, out)
	if err != nil {
		t.Fatalf("CollectAll should not return error for collector failures, got: %v", err)
	}

	// Should log the error
	found := false
	for _, line := range out.lines {
		if strings.Contains(line, "1 collector(s) encountered errors") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error summary in output lines")
	}

	if !out.flushed {
		t.Error("expected output to be flushed even with collector errors")
	}
}

func TestCollectAll_PanicRecovery(t *testing.T) {
	c := &Collector{
		collectors: []namedCollector{
			{name: "Panicker", fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
				panic("unexpected nil pointer")
			}},
			{name: "AfterPanic", fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
				return nil
			}},
		},
	}

	ctx := context.Background()
	dc := &DiagnosticContext{
		Namespace:         "test-ns",
		OperatorNamespace: "grove-system",
	}
	out := &mockOutput{}

	err := c.CollectAll(ctx, dc, out)
	if err != nil {
		t.Fatalf("CollectAll should not propagate panics, got: %v", err)
	}

	// Should log the panic as an error
	found := false
	for _, line := range out.lines {
		if strings.Contains(line, "Panicker") && strings.Contains(line, "panic") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected panic to be logged in output lines")
	}
}

func TestCollectAll_FlushError(t *testing.T) {
	c := &Collector{
		collectors: []namedCollector{},
	}

	ctx := context.Background()
	dc := &DiagnosticContext{
		Namespace:         "test-ns",
		OperatorNamespace: "grove-system",
	}
	out := &mockOutput{flushErr: fmt.Errorf("disk full")}

	err := c.CollectAll(ctx, dc, out)
	if err == nil {
		t.Fatal("expected error from flush failure")
	}
	if !strings.Contains(err.Error(), "flush") {
		t.Errorf("expected flush error, got: %v", err)
	}
}

func TestNewCollector(t *testing.T) {
	c := NewCollector()
	if len(c.collectors) != 4 {
		t.Errorf("expected 4 default collectors, got %d", len(c.collectors))
	}

	expectedNames := []string{"Operator Logs", "Grove Resources", "Pod Details", "Kubernetes Events"}
	for i, expected := range expectedNames {
		if c.collectors[i].name != expected {
			t.Errorf("collector %d: expected name %q, got %q", i, expected, c.collectors[i].name)
		}
	}
}

func TestSafeRunCollector_NormalReturn(t *testing.T) {
	c := &Collector{}
	nc := namedCollector{
		name: "test",
		fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
			return nil
		},
	}

	err := c.safeRunCollector(context.Background(), nc, nil, &mockOutput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSafeRunCollector_ErrorReturn(t *testing.T) {
	c := &Collector{}
	nc := namedCollector{
		name: "test",
		fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
			return fmt.Errorf("test error")
		},
	}

	err := c.safeRunCollector(context.Background(), nc, nil, &mockOutput{})
	if err == nil || err.Error() != "test error" {
		t.Fatalf("expected 'test error', got: %v", err)
	}
}

func TestSafeRunCollector_PanicRecovery(t *testing.T) {
	c := &Collector{}
	nc := namedCollector{
		name: "test",
		fn: func(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
			panic("boom")
		},
	}

	err := c.safeRunCollector(context.Background(), nc, nil, &mockOutput{})
	if err == nil {
		t.Fatal("expected error from panic recovery")
	}
	if !strings.Contains(err.Error(), "panic: boom") {
		t.Errorf("expected panic message in error, got: %v", err)
	}
}
