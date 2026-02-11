package tui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	tea "github.com/charmbracelet/bubbletea"
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

	// Context tracking for structured logging
	currentViewType  data.ViewType
	currentPane      data.Pane
	currentOpID      string
	filterActive     bool
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

// DebugLog writes a formatted message to the debug log if enabled.
// Exported so that other packages (e.g. cli) can log debug messages.
func DebugLog(format string, args ...interface{}) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if !debugLogger.enabled {
		return
	}
	debugLogger.logger.Printf(format, args...)
}

// debugLog is an internal alias for DebugLog kept for convenience within the tui package.
func debugLog(format string, args ...interface{}) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if !debugLogger.enabled {
		return
	}
	debugLogger.logger.Printf(format, args...)
}

// debugLogWithContext writes a formatted message with the current view context prefix.
// Format: [ViewType/Pane] message
func debugLogWithContext(format string, args ...interface{}) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if !debugLogger.enabled {
		return
	}
	prefix := fmt.Sprintf("[%s/%s] ", data.ViewTypeName(debugLogger.currentViewType), data.PaneName(debugLogger.currentPane))
	debugLogger.logger.Printf(prefix+format, args...)
}

// debugSetContext updates the current context for structured logging.
// Called from model.Update to keep context in sync.
func debugSetContext(viewType data.ViewType, pane data.Pane, filterActive bool) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	debugLogger.currentViewType = viewType
	debugLogger.currentPane = pane
	debugLogger.filterActive = filterActive
}

// debugLogMsg logs a tea.Msg with type and key summary.
// Example output: "MSG tea.KeyMsg key="enter""
// Example output: "MSG ForestDataMsg resources=5 err=<nil>"
func debugLogMsg(msg tea.Msg) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if !debugLogger.enabled {
		return
	}

	prefix := fmt.Sprintf("[%s/%s] ", data.ViewTypeName(debugLogger.currentViewType), data.PaneName(debugLogger.currentPane))

	switch m := msg.(type) {
	case tea.KeyMsg:
		var keyDesc string
		if m.Type == tea.KeyRunes {
			keyDesc = string(m.Runes)
		} else {
			keyDesc = m.Type.String()
		}
		debugLogger.logger.Printf("%sMSG tea.KeyMsg key=%q", prefix, keyDesc)

	case tea.WindowSizeMsg:
		debugLogger.logger.Printf("%sMSG tea.WindowSizeMsg width=%d height=%d", prefix, m.Width, m.Height)

	case CacheSyncedMsg:
		debugLogger.logger.Printf("%sMSG CacheSyncedMsg", prefix)

	case CacheUpdateMsg:
		debugLogger.logger.Printf("%sMSG CacheUpdateMsg", prefix)

	case PodYAMLMsg:
		debugLogger.logger.Printf("%sMSG PodYAMLMsg pod=%s yamlLen=%d err=%v", prefix, m.PodName, len(m.YAML), m.Err)

	case ErrorMsg:
		debugLogger.logger.Printf("%sMSG ErrorMsg operation=%s err=%v", prefix, m.Operation, m.Err)

	default:
		debugLogger.logger.Printf("%sMSG %T", prefix, msg)
	}
}

// debugLogStateTransition logs a view state transition.
// Example output: "[ForestView/Resources] STATE ForestView -> PodCliqueSetView pcs="my-pcs""
func debugLogStateTransition(from, to data.ViewType, details string) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if !debugLogger.enabled {
		return
	}
	prefix := fmt.Sprintf("[%s/%s] ", data.ViewTypeName(debugLogger.currentViewType), data.PaneName(debugLogger.currentPane))
	if details != "" {
		debugLogger.logger.Printf("%sSTATE %s -> %s %s", prefix, data.ViewTypeName(from), data.ViewTypeName(to), details)
	} else {
		debugLogger.logger.Printf("%sSTATE %s -> %s", prefix, data.ViewTypeName(from), data.ViewTypeName(to))
	}
	// Update context to new view type
	debugLogger.currentViewType = to
}

// debugLogCmd logs a command dispatch with its name and arguments.
// Example output: "[ForestView/Resources] CMD loadForestData op=abc123"
// Example output: "[ForestView/Resources] CMD loadEventsForPCS pcs="my-pcs" ns="default" op=abc123"
func debugLogCmd(name string, args ...interface{}) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if !debugLogger.enabled {
		return
	}
	prefix := fmt.Sprintf("[%s/%s] ", data.ViewTypeName(debugLogger.currentViewType), data.PaneName(debugLogger.currentPane))

	// Build args string
	argsStr := ""
	for i := 0; i < len(args)-1; i += 2 {
		if i > 0 {
			argsStr += " "
		}
		key := args[i]
		val := args[i+1]
		argsStr += fmt.Sprintf("%v=%q", key, val)
	}

	opID := generateOpID()
	if argsStr != "" {
		debugLogger.logger.Printf("%sCMD %s %s op=%s", prefix, name, argsStr, opID)
	} else {
		debugLogger.logger.Printf("%sCMD %s op=%s", prefix, name, opID)
	}
	debugLogger.currentOpID = opID
}

// debugLogView logs a view render summary (optional, verbose).
// Example output: "[ForestView/Resources] VIEW rendered lines=45 resources=5 events=12"
func debugLogView(lines, resourceCount, eventCount int) {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	if !debugLogger.enabled {
		return
	}
	prefix := fmt.Sprintf("[%s/%s] ", data.ViewTypeName(debugLogger.currentViewType), data.PaneName(debugLogger.currentPane))
	debugLogger.logger.Printf("%sVIEW rendered lines=%d resources=%d events=%d", prefix, lines, resourceCount, eventCount)
}

// generateOpID generates a short random operation ID for correlation.
func generateOpID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "??????"
	}
	return hex.EncodeToString(b)
}

// DebugLogEnabled returns whether debug logging is enabled.
func DebugLogEnabled() bool {
	debugLogger.mu.Lock()
	defer debugLogger.mu.Unlock()
	return debugLogger.enabled
}
