package main

import (
	"fmt"
	"log"
	"os"
	"sync"
)

// debugLogger is a package-level debug logger.
// When enabled (via --debug <file>), it writes timestamped messages to the given file.
// When disabled, all calls are no-ops.
var debugLogger = &DebugLogger{}

// DebugLogger provides file-based debug logging for the arborist TUI.
// Since the TUI owns stdout/stderr, debug output must go to a separate file.
type DebugLogger struct {
	mu      sync.Mutex
	enabled bool
	file    *os.File
	logger  *log.Logger
}

// InitDebugLog opens the debug log file and enables logging.
// The caller should defer CloseDebugLog().
func InitDebugLog(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open debug log %s: %w", path, err)
	}

	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	debugLogger.file = f
	debugLogger.logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	debugLogger.enabled = true

	debugLogger.logger.Println("=== Arborist debug log started ===")
	return nil
}

// CloseDebugLog flushes and closes the debug log file.
func CloseDebugLog() {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if debugLogger.file != nil {
		debugLogger.logger.Println("=== Arborist debug log closed ===")
		debugLogger.file.Close()
		debugLogger.file = nil
		debugLogger.enabled = false
	}
}

// debugLog writes a formatted message to the debug log if enabled.
func debugLog(format string, args ...interface{}) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if !debugLogger.enabled {
		return
	}
	debugLogger.logger.Printf(format, args...)
}
