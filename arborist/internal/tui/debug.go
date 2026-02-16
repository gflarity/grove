package tui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	"github.com/ai-dynamo/grove/arborist/internal/debug"
	tea "github.com/charmbracelet/bubbletea"
)

// tuiDebugCtx tracks TUI-specific context for structured debug logging.
// The core init/close/log/enabled primitives live in the debug package;
// this struct only holds the view/pane context that prefixes TUI messages.
var tuiDebugCtx = &debugContext{}

type debugContext struct {
	mu              sync.Mutex
	currentViewType clusterstate.ViewType
	currentPane     clusterstate.Pane
	currentOpID     string
	filterActive    bool
}

// prefix returns the current "[ViewType/Pane] " string.
// Caller must hold ctx.mu.
func (ctx *debugContext) prefix() string {
	return fmt.Sprintf("[%s/%s] ", clusterstate.ViewTypeName(ctx.currentViewType), clusterstate.PaneName(ctx.currentPane))
}

// debugLog is a convenience wrapper around debug.Log for use within the tui package.
func debugLog(format string, args ...interface{}) {
	debug.Log(format, args...)
}

// debugLogWithContext writes a formatted message with the current view context prefix.
// Format: [ViewType/Pane] message
func debugLogWithContext(format string, args ...interface{}) {
	if !debug.Enabled() {
		return
	}
	tuiDebugCtx.mu.Lock()
	prefix := tuiDebugCtx.prefix()
	tuiDebugCtx.mu.Unlock()
	debug.Log(prefix+format, args...)
}

// debugSetContext updates the current context for structured logging.
// Called from model.Update to keep context in sync.
func debugSetContext(viewType clusterstate.ViewType, pane clusterstate.Pane, filterActive bool) {
	tuiDebugCtx.mu.Lock()
	defer tuiDebugCtx.mu.Unlock()
	tuiDebugCtx.currentViewType = viewType
	tuiDebugCtx.currentPane = pane
	tuiDebugCtx.filterActive = filterActive
}

// debugLogMsg logs a tea.Msg with type and key summary.
// Example output: "MSG tea.KeyMsg key="enter""
// Example output: "MSG ForestDataMsg resources=5 err=<nil>"
func debugLogMsg(msg tea.Msg) {
	if !debug.Enabled() {
		return
	}

	tuiDebugCtx.mu.Lock()
	prefix := tuiDebugCtx.prefix()
	tuiDebugCtx.mu.Unlock()

	switch m := msg.(type) {
	case tea.KeyMsg:
		var keyDesc string
		if m.Type == tea.KeyRunes {
			keyDesc = string(m.Runes)
		} else {
			keyDesc = m.Type.String()
		}
		debug.Log("%sMSG tea.KeyMsg key=%q", prefix, keyDesc)

	case tea.WindowSizeMsg:
		debug.Log("%sMSG tea.WindowSizeMsg width=%d height=%d", prefix, m.Width, m.Height)

	case CacheSyncedMsg:
		debug.Log("%sMSG CacheSyncedMsg", prefix)

	case CacheUpdateMsg:
		debug.Log("%sMSG CacheUpdateMsg", prefix)

	case PodYAMLMsg:
		debug.Log("%sMSG PodYAMLMsg pod=%s yamlLen=%d err=%v", prefix, m.PodName, len(m.YAML), m.Err)

	case ErrorMsg:
		debug.Log("%sMSG ErrorMsg operation=%s err=%v", prefix, m.Operation, m.Err)

	default:
		debug.Log("%sMSG %T", prefix, msg)
	}
}

// debugLogStateTransition logs a view state transition.
// Example output: "[ForestView/Resources] STATE ForestView -> PodCliqueSetView pcs="my-pcs""
func debugLogStateTransition(from, to clusterstate.ViewType, details string) {
	if !debug.Enabled() {
		return
	}
	tuiDebugCtx.mu.Lock()
	prefix := tuiDebugCtx.prefix()
	tuiDebugCtx.currentViewType = to
	tuiDebugCtx.mu.Unlock()

	if details != "" {
		debug.Log("%sSTATE %s -> %s %s", prefix, clusterstate.ViewTypeName(from), clusterstate.ViewTypeName(to), details)
	} else {
		debug.Log("%sSTATE %s -> %s", prefix, clusterstate.ViewTypeName(from), clusterstate.ViewTypeName(to))
	}
}

// debugLogCmd logs a command dispatch with its name and arguments.
// Example output: "[ForestView/Resources] CMD loadForestData op=abc123"
// Example output: "[ForestView/Resources] CMD loadEventsForPCS pcs="my-pcs" ns="default" op=abc123"
func debugLogCmd(name string, args ...interface{}) {
	if !debug.Enabled() {
		return
	}
	tuiDebugCtx.mu.Lock()
	prefix := tuiDebugCtx.prefix()
	tuiDebugCtx.mu.Unlock()

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
		debug.Log("%sCMD %s %s op=%s", prefix, name, argsStr, opID)
	} else {
		debug.Log("%sCMD %s op=%s", prefix, name, opID)
	}
	tuiDebugCtx.mu.Lock()
	tuiDebugCtx.currentOpID = opID
	tuiDebugCtx.mu.Unlock()
}

// debugLogView logs a view render summary (optional, verbose).
// Example output: "[ForestView/Resources] VIEW rendered lines=45 resources=5 events=12"
func debugLogView(lines, resourceCount, eventCount int) {
	if !debug.Enabled() {
		return
	}
	tuiDebugCtx.mu.Lock()
	prefix := tuiDebugCtx.prefix()
	tuiDebugCtx.mu.Unlock()
	debug.Log("%sVIEW rendered lines=%d resources=%d events=%d", prefix, lines, resourceCount, eventCount)
}

// generateOpID generates a short random operation ID for correlation.
func generateOpID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "??????"
	}
	return hex.EncodeToString(b)
}
