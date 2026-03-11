// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package diagnostics

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileOutput implements DiagnosticOutput by writing diagnostics to a directory.
// It writes a summary.txt file for text output and separate YAML files per resource type.
type FileOutput struct {
	dir         string
	summaryFile *os.File

	// yamlFiles tracks open YAML files by resource type so we can append
	yamlFiles map[string]*os.File
}

// NewFileOutput creates a new FileOutput that writes to the given directory.
// The directory is created if it doesn't exist.
func NewFileOutput(dir string) (*FileOutput, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory %s: %w", dir, err)
	}

	summaryPath := filepath.Join(dir, "summary.txt")
	f, err := os.Create(summaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create summary file: %w", err)
	}

	return &FileOutput{
		dir:         dir,
		summaryFile: f,
		yamlFiles:   make(map[string]*os.File),
	}, nil
}

// WriteSection writes a section header to the summary file.
func (fo *FileOutput) WriteSection(title string) error {
	separator := strings.Repeat("=", 80)
	_, err := fmt.Fprintf(fo.summaryFile, "%s\n=== %s ===\n%s\n", separator, title, separator)
	return err
}

// WriteSubSection writes a subsection header to the summary file.
func (fo *FileOutput) WriteSubSection(title string) error {
	separator := strings.Repeat("-", 80)
	_, err := fmt.Fprintf(fo.summaryFile, "%s\n--- %s ---\n%s\n", separator, title, separator)
	return err
}

// WriteLine writes a single line to the summary file.
func (fo *FileOutput) WriteLine(line string) error {
	_, err := fmt.Fprintln(fo.summaryFile, line)
	return err
}

// WriteLinef writes a formatted line to the summary file.
func (fo *FileOutput) WriteLinef(format string, args ...any) error {
	_, err := fmt.Fprintf(fo.summaryFile, format+"\n", args...)
	return err
}

// WriteYAML writes YAML content to a per-resource-type file.
// Each resource type gets its own file (e.g., podcliquesets.yaml).
// Multiple resources of the same type are separated by "---".
func (fo *FileOutput) WriteYAML(resourceType, resourceName string, yamlContent []byte) error {
	f, ok := fo.yamlFiles[resourceType]
	if !ok {
		path := filepath.Join(fo.dir, resourceType+".yaml")
		var err error
		f, err = os.Create(path)
		if err != nil {
			return fmt.Errorf("failed to create YAML file for %s: %w", resourceType, err)
		}
		fo.yamlFiles[resourceType] = f
	} else {
		// Separate multiple resources with YAML document separator
		if _, err := fmt.Fprintln(f, "---"); err != nil {
			return err
		}
	}

	// Write a comment with the resource name
	if _, err := fmt.Fprintf(f, "# %s: %s\n", resourceType, resourceName); err != nil {
		return err
	}
	if _, err := f.Write(yamlContent); err != nil {
		return err
	}

	// Also note in summary
	_ = fo.WriteLinef("[INFO] Wrote %s/%s to %s.yaml", resourceType, resourceName, resourceType)

	return nil
}

// WriteTable writes tabular data to the summary file with right-padded columns.
func (fo *FileOutput) WriteTable(headers []string, rows [][]string) error {
	if len(headers) == 0 {
		return nil
	}

	// Compute column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Write header
	var headerLine strings.Builder
	for i, h := range headers {
		if i > 0 {
			headerLine.WriteString("  ")
		}
		headerLine.WriteString(fmt.Sprintf("%-*s", widths[i], h))
	}
	if _, err := fmt.Fprintln(fo.summaryFile, headerLine.String()); err != nil {
		return err
	}

	// Write separator
	totalWidth := 0
	for _, w := range widths {
		totalWidth += w
	}
	totalWidth += (len(widths) - 1) * 2 // account for "  " separators
	if _, err := fmt.Fprintln(fo.summaryFile, strings.Repeat("-", totalWidth)); err != nil {
		return err
	}

	// Write rows
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			if i > 0 {
				line.WriteString("  ")
			}
			if i < len(widths) {
				line.WriteString(fmt.Sprintf("%-*s", widths[i], cell))
			} else {
				line.WriteString(cell)
			}
		}
		if _, err := fmt.Fprintln(fo.summaryFile, line.String()); err != nil {
			return err
		}
	}

	return nil
}

// Flush closes all open files.
func (fo *FileOutput) Flush() error {
	var firstErr error

	for _, f := range fo.yamlFiles {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	fo.yamlFiles = nil

	if err := fo.summaryFile.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

// CollectToDirectory runs all collectors and writes output to the given directory.
func CollectToDirectory(ctx context.Context, dc *DiagnosticContext, outputDir string) error {
	output, err := NewFileOutput(outputDir)
	if err != nil {
		return fmt.Errorf("failed to create file output: %w", err)
	}

	return CollectAllDiagnostics(ctx, dc, output)
}

// CollectAndBundle runs all collectors, writes to a timestamped directory,
// and creates a .tgz archive. Returns the path to the tgz file.
func CollectAndBundle(ctx context.Context, dc *DiagnosticContext, baseDir string) (string, error) {
	// Create timestamped directory name
	timestamp := time.Now().Format("2006-01-04-150405")
	dirName := fmt.Sprintf("grove-diagnostics-%s", timestamp)
	outputDir := filepath.Join(baseDir, dirName)

	// Collect diagnostics to directory
	if err := CollectToDirectory(ctx, dc, outputDir); err != nil {
		return "", fmt.Errorf("failed to collect diagnostics: %w", err)
	}

	// Create tgz archive
	tgzPath := outputDir + ".tgz"
	if err := createTGZ(outputDir, tgzPath, dirName); err != nil {
		return "", fmt.Errorf("failed to create tgz archive: %w", err)
	}

	// Clean up the directory since we have the tgz
	_ = os.RemoveAll(outputDir)

	return tgzPath, nil
}

// createTGZ creates a .tgz archive from the given source directory.
func createTGZ(sourceDir, tgzPath, archivePrefix string) error {
	tgzFile, err := os.Create(tgzPath)
	if err != nil {
		return fmt.Errorf("failed to create tgz file: %w", err)
	}
	defer tgzFile.Close()

	gzWriter := gzip.NewWriter(tgzFile)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if path == sourceDir {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.Join(archivePrefix, relPath)

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// If it's a directory, no content to write
		if info.IsDir() {
			return nil
		}

		// Write file content
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tarWriter, f)
		return err
	})
}
