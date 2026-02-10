package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
)

// Pane represents which pane is active
type Pane int

const (
	ResourcesPane Pane = iota
	EventsPane
)

// ViewType represents the current view in the hierarchy
type ViewType int

const (
	ForestView ViewType = iota
	PodCliqueSetView
	PodCliqueSetReplicaView
	PodCliqueScalingGroupView
	PodCliqueView
	PodView
)

// ViewState tracks the current navigation state
type ViewState struct {
	viewType             ViewType
	selectedPodCliqueSet string
	selectedReplicaIndex string // The replica index (e.g., "0", "1", "2")
	selectedScalingGroup string
	selectedPodClique    string
	selectedPod          string
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("arborist"),
		kong.Description("Grove cluster inspector — interactive TUI and CLI tools for PodCliqueSets."),
		kong.UsageOnError(),
	)

	// Initialize debug logging if requested
	if cli.Debug != "" {
		path := cli.Debug
		if len(path) > 0 && path[0] == '~' {
			home, err := os.UserHomeDir()
			if err == nil {
				path = home + path[1:]
			}
		}
		if err := InitDebugLog(path); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open debug log: %v\n", err)
			os.Exit(1)
		}
		defer CloseDebugLog()
	}

	debugLog("starting arborist")

	err := ctx.Run(&cli)
	ctx.FatalIfErrorf(err)
}
