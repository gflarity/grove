package diagnostics

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFileOutput(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "diagnostics")

	fo, err := NewFileOutput(outDir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}
	defer fo.Flush()

	// Directory should exist
	info, err := os.Stat(outDir)
	if err != nil {
		t.Fatalf("output directory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory, got file")
	}

	// summary.txt should exist
	summaryPath := filepath.Join(outDir, "summary.txt")
	if _, err := os.Stat(summaryPath); err != nil {
		t.Fatalf("summary.txt does not exist: %v", err)
	}
}

func TestFileOutput_WriteSection(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	if err := fo.WriteSection("TEST SECTION"); err != nil {
		t.Fatalf("WriteSection failed: %v", err)
	}
	fo.Flush()

	content, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("failed to read summary.txt: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "=== TEST SECTION ===") {
		t.Errorf("expected section header in output, got:\n%s", text)
	}
	if !strings.Contains(text, strings.Repeat("=", 80)) {
		t.Error("expected separator line of 80 '=' characters")
	}
}

func TestFileOutput_WriteSubSection(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	if err := fo.WriteSubSection("SUB SECTION"); err != nil {
		t.Fatalf("WriteSubSection failed: %v", err)
	}
	fo.Flush()

	content, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("failed to read summary.txt: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "--- SUB SECTION ---") {
		t.Errorf("expected subsection header, got:\n%s", text)
	}
}

func TestFileOutput_WriteLinef(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	if err := fo.WriteLinef("Hello %s, count: %d", "world", 42); err != nil {
		t.Fatalf("WriteLinef failed: %v", err)
	}
	fo.Flush()

	content, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("failed to read summary.txt: %v", err)
	}

	if !strings.Contains(string(content), "Hello world, count: 42") {
		t.Errorf("expected formatted line, got:\n%s", string(content))
	}
}

func TestFileOutput_WriteTable(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	headers := []string{"NAME", "STATUS", "NODE"}
	rows := [][]string{
		{"pod-1", "Running", "node-a"},
		{"long-pod-name", "Pending", "node-b"},
	}

	if err := fo.WriteTable(headers, rows); err != nil {
		t.Fatalf("WriteTable failed: %v", err)
	}
	fo.Flush()

	content, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("failed to read summary.txt: %v", err)
	}

	text := string(content)
	lines := strings.Split(strings.TrimSpace(text), "\n")

	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (header + separator + 2 rows), got %d:\n%s", len(lines), text)
	}

	// Header should contain all column names
	if !strings.Contains(lines[0], "NAME") || !strings.Contains(lines[0], "STATUS") || !strings.Contains(lines[0], "NODE") {
		t.Errorf("header missing columns: %q", lines[0])
	}

	// Separator line should be all dashes
	if !strings.HasPrefix(strings.TrimSpace(lines[1]), "---") {
		t.Errorf("expected separator line, got: %q", lines[1])
	}

	// Data rows
	if !strings.Contains(lines[2], "pod-1") || !strings.Contains(lines[2], "Running") {
		t.Errorf("row 1 missing data: %q", lines[2])
	}
	if !strings.Contains(lines[3], "long-pod-name") || !strings.Contains(lines[3], "Pending") {
		t.Errorf("row 2 missing data: %q", lines[3])
	}
}

func TestFileOutput_WriteTable_ColumnAlignment(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	headers := []string{"A", "B"}
	rows := [][]string{
		{"short", "x"},
		{"longer-value", "y"},
	}

	if err := fo.WriteTable(headers, rows); err != nil {
		t.Fatalf("WriteTable failed: %v", err)
	}
	fo.Flush()

	content, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("failed to read summary.txt: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// The "B" column in both rows should start at the same offset
	headerBPos := strings.Index(lines[0], "B")
	row1BPos := strings.Index(lines[2], "x")
	row2BPos := strings.Index(lines[3], "y")

	if headerBPos != row1BPos || headerBPos != row2BPos {
		t.Errorf("columns not aligned: header B at %d, row1 at %d, row2 at %d", headerBPos, row1BPos, row2BPos)
	}
}

func TestFileOutput_WriteTable_EmptyHeaders(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	err = fo.WriteTable(nil, nil)
	if err != nil {
		t.Fatalf("WriteTable with empty headers should succeed, got: %v", err)
	}
	fo.Flush()

	content, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("failed to read summary.txt: %v", err)
	}

	if strings.TrimSpace(string(content)) != "" {
		t.Errorf("expected empty output for empty table, got: %q", string(content))
	}
}

func TestFileOutput_WriteYAML(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	yamlContent := []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: test-pod\n")
	if err := fo.WriteYAML("pods", "test-pod", yamlContent); err != nil {
		t.Fatalf("WriteYAML failed: %v", err)
	}
	fo.Flush()

	// Check YAML file was created
	yamlPath := filepath.Join(dir, "pods.yaml")
	content, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("failed to read pods.yaml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "# pods: test-pod") {
		t.Errorf("expected resource comment, got:\n%s", text)
	}
	if !strings.Contains(text, "apiVersion: v1") {
		t.Errorf("expected YAML content, got:\n%s", text)
	}
}

func TestFileOutput_WriteYAML_MultipleResources(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	if err := fo.WriteYAML("pods", "pod-1", []byte("name: pod-1\n")); err != nil {
		t.Fatalf("WriteYAML 1 failed: %v", err)
	}
	if err := fo.WriteYAML("pods", "pod-2", []byte("name: pod-2\n")); err != nil {
		t.Fatalf("WriteYAML 2 failed: %v", err)
	}
	fo.Flush()

	content, err := os.ReadFile(filepath.Join(dir, "pods.yaml"))
	if err != nil {
		t.Fatalf("failed to read pods.yaml: %v", err)
	}

	text := string(content)
	// Should have separator between resources
	if !strings.Contains(text, "---") {
		t.Error("expected '---' separator between YAML documents")
	}
	if !strings.Contains(text, "# pods: pod-1") {
		t.Error("expected comment for pod-1")
	}
	if !strings.Contains(text, "# pods: pod-2") {
		t.Error("expected comment for pod-2")
	}
}

func TestFileOutput_WriteYAML_DifferentTypes(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	if err := fo.WriteYAML("pods", "my-pod", []byte("kind: Pod\n")); err != nil {
		t.Fatalf("WriteYAML pods failed: %v", err)
	}
	if err := fo.WriteYAML("services", "my-svc", []byte("kind: Service\n")); err != nil {
		t.Fatalf("WriteYAML services failed: %v", err)
	}
	fo.Flush()

	// Both YAML files should exist
	if _, err := os.Stat(filepath.Join(dir, "pods.yaml")); err != nil {
		t.Error("pods.yaml should exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "services.yaml")); err != nil {
		t.Error("services.yaml should exist")
	}
}

func TestCreateTGZ(t *testing.T) {
	// Create source directory with test files
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	// Create test files
	files := map[string]string{
		"summary.txt": "test summary content",
		"pods.yaml":   "kind: Pod\nname: test",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	tgzPath := filepath.Join(dir, "test.tgz")
	if err := createTGZ(srcDir, tgzPath, "test-archive"); err != nil {
		t.Fatalf("createTGZ failed: %v", err)
	}

	// Verify the archive
	f, err := os.Open(tgzPath)
	if err != nil {
		t.Fatalf("failed to open tgz: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	foundFiles := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error reading tar: %v", err)
		}

		if hdr.Typeflag == tar.TypeReg {
			content, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("failed to read file from tar: %v", err)
			}
			foundFiles[hdr.Name] = string(content)
		}
	}

	// Check expected files are in the archive with the prefix
	for name, expectedContent := range files {
		archiveName := filepath.Join("test-archive", name)
		content, ok := foundFiles[archiveName]
		if !ok {
			t.Errorf("file %q not found in archive (found: %v)", archiveName, foundFiles)
			continue
		}
		if content != expectedContent {
			t.Errorf("file %q: expected content %q, got %q", archiveName, expectedContent, content)
		}
	}
}

func TestFileOutput_Flush_ClosesAllFiles(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	// Write some YAML to create additional files
	if err := fo.WriteYAML("type1", "res1", []byte("content1")); err != nil {
		t.Fatalf("WriteYAML failed: %v", err)
	}
	if err := fo.WriteYAML("type2", "res2", []byte("content2")); err != nil {
		t.Fatalf("WriteYAML failed: %v", err)
	}

	err = fo.Flush()
	if err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Writing after flush should fail (files are closed)
	err = fo.WriteLine("after flush")
	if err == nil {
		t.Error("expected error writing after flush")
	}
}

func TestFileOutput_WriteLine(t *testing.T) {
	dir := t.TempDir()
	fo, err := NewFileOutput(dir)
	if err != nil {
		t.Fatalf("NewFileOutput failed: %v", err)
	}

	if err := fo.WriteLine("simple line"); err != nil {
		t.Fatalf("WriteLine failed: %v", err)
	}
	fo.Flush()

	content, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("failed to read summary.txt: %v", err)
	}
	if !strings.Contains(string(content), "simple line") {
		t.Errorf("expected 'simple line' in output, got: %s", string(content))
	}
}

func TestCollectAndBundle_CreatesTGZ(t *testing.T) {
	baseDir := t.TempDir()

	dc := &DiagnosticContext{
		Namespace:         "test-ns",
		OperatorNamespace: "grove-system",
	}

	tgzPath, err := CollectAndBundle(context.Background(), dc, baseDir)
	if err != nil {
		t.Fatalf("CollectAndBundle failed: %v", err)
	}

	// Verify tgz was created
	if !strings.HasSuffix(tgzPath, ".tgz") {
		t.Errorf("expected .tgz suffix, got: %s", tgzPath)
	}
	info, err := os.Stat(tgzPath)
	if err != nil {
		t.Fatalf("tgz file does not exist: %v", err)
	}
	if info.Size() == 0 {
		t.Error("tgz file is empty")
	}

	// Verify the intermediate directory was cleaned up
	dirName := strings.TrimSuffix(tgzPath, ".tgz")
	if _, err := os.Stat(dirName); !os.IsNotExist(err) {
		t.Errorf("expected intermediate directory to be removed, but it still exists: %s", dirName)
	}

	// Verify the tgz contains summary.txt
	f, err := os.Open(tgzPath)
	if err != nil {
		t.Fatalf("failed to open tgz: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	foundSummary := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error reading tar: %v", err)
		}
		if strings.HasSuffix(hdr.Name, "summary.txt") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Error("expected summary.txt in tgz archive")
	}
}

func TestCollectToDirectory_CreatesSummary(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "diag-output")

	dc := &DiagnosticContext{
		Namespace:         "test-ns",
		OperatorNamespace: "grove-system",
	}

	err := CollectToDirectory(context.Background(), dc, outputDir)
	if err != nil {
		t.Fatalf("CollectToDirectory failed: %v", err)
	}

	// Verify summary.txt was created
	summaryPath := filepath.Join(outputDir, "summary.txt")
	content, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("summary.txt does not exist: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "COLLECTING GROVE DIAGNOSTICS") {
		t.Error("expected header section in summary.txt")
	}
	if !strings.Contains(text, "END OF DIAGNOSTICS") {
		t.Error("expected footer section in summary.txt")
	}
	if !strings.Contains(text, "test-ns") {
		t.Error("expected namespace in summary.txt")
	}
}
