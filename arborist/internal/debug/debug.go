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

// Package debug provides file-based debug logging for arborist.
// Since the TUI owns stdout/stderr, debug output goes to a separate file.
// Both the cli and tui packages import this package, avoiding a dependency
// inversion where cli would otherwise import tui solely for logging.
package debug

import (
	"fmt"
	"log"
	"os"
	"sync"
)

var (
	mu      sync.Mutex
	enabled bool
	file    *os.File
	logger  *log.Logger
)

// Init opens the debug log file and enables logging.
// The caller should defer Close().
func Init(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open debug log %s: %w", path, err)
	}

	mu.Lock()
	defer mu.Unlock()
	file = f
	logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	enabled = true

	logger.Println("=== Arborist debug log started ===")
	return nil
}

// Close flushes and closes the debug log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		logger.Println("=== Arborist debug log closed ===")
		_ = file.Close() // error intentionally suppressed: best-effort cleanup
		file = nil
		enabled = false
	}
}

// Log writes a formatted message to the debug log if enabled.
func Log(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()
	if !enabled {
		return
	}
	logger.Printf(format, args...)
}

// Enabled returns whether debug logging is enabled.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}
