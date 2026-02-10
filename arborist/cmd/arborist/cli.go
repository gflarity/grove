package main

import (
	"context"
	"fmt"
	"os"
	rtdebug "runtime/debug"

	tea "github.com/charmbracelet/bubbletea"
)

// CLI defines the top-level Kong command structure for arborist.
type CLI struct {
	Debug string `help:"Path to debug log file (e.g. ~/tmp/arborist.log)." short:"d" type:"path"`

	// Subcommands
	TUI        TUICmd        `cmd:"" default:"withargs" help:"Launch the interactive TUI (default)."`
	Topology   TopologyCmd   `cmd:"" help:"Show pods grouped by topology domain."`
	Diagnostics DiagnosticsCmd `cmd:"" help:"Collect cluster diagnostics for a PodCliqueSet."`
}

// TUICmd launches the interactive Bubble Tea TUI.
type TUICmd struct{}

// Run executes the TUI command.
func (c *TUICmd) Run(globals *CLI) error {
	// Recover from panics so we can log the stack trace and restore the
	// terminal before exiting.
	defer func() {
		if r := recover(); r != nil {
			stack := rtdebug.Stack()
			debugLog("PANIC: %v\n%s", r, stack)
			CloseDebugLog()
			fmt.Fprintf(os.Stderr, "arborist panic: %v\n%s", r, stack)
			os.Exit(1)
		}
	}()

	// Initialize Kubernetes client (the real DataProvider)
	debugLog("initializing Kubernetes client")
	var provider DataProvider
	k8sClient, err := NewK8sClient()
	if err != nil {
		debugLog("WARNING: failed to initialize Kubernetes client: %v", err)
		// provider stays nil — the TUI will show empty data
	} else {
		debugLog("Kubernetes client initialized successfully")
		provider = k8sClient
	}

	// Create the Bubble Tea model
	m := NewModel(provider,
		WithContext(context.Background()),
		WithDebug(globals.Debug != ""),
	)

	// Create and run the Bubble Tea program
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)

	debugLog("starting bubbletea program")
	if _, err := p.Run(); err != nil {
		debugLog("program exited with error: %v", err)
		return fmt.Errorf("TUI error: %w", err)
	}
	debugLog("arborist TUI exiting cleanly")
	return nil
}

// TopologyCmd shows pods for a PodCliqueSet grouped by topology domain.
type TopologyCmd struct {
	PodCliqueSet string `arg:"" help:"Name of the PodCliqueSet."`
	Domain       string `arg:"" help:"Topology domain (e.g. rack, zone, block, host)."`
	Namespace    string `short:"n" help:"Kubernetes namespace (defaults to current kubeconfig context namespace)."`
}

// Run executes the topology command.
func (c *TopologyCmd) Run(globals *CLI) error {
	return runTopology(c.PodCliqueSet, c.Domain, c.Namespace)
}

// DiagnosticsCmd collects cluster diagnostics for a PodCliqueSet.
type DiagnosticsCmd struct {
	PodCliqueSet string `arg:"" optional:"" help:"Name of the PodCliqueSet (optional, collects all if omitted)."`
	Namespace    string `short:"n" help:"Kubernetes namespace (defaults to current kubeconfig context namespace)."`
	Output       string `short:"o" help:"Output directory for diagnostics bundle." default:"."`
}

// Run executes the diagnostics command.
func (c *DiagnosticsCmd) Run(globals *CLI) error {
	fmt.Fprintln(os.Stderr, "diagnostics command is not yet implemented")
	return nil
}
