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

package cli

import (
	"context"
	"fmt"
	"os"
	rtdebug "runtime/debug"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/ai-dynamo/grove/arborist/internal/k8s"
	"github.com/ai-dynamo/grove/arborist/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// TUICmd launches the interactive Bubble Tea TUI.
type TUICmd struct{}

// Run executes the TUI command.
func (c *TUICmd) Run(globals *CLI) error {
	// Recover from panics so we can log the stack trace and restore the
	// terminal before exiting.
	defer func() {
		if r := recover(); r != nil {
			stack := rtdebug.Stack()
			tui.DebugLog("PANIC: %v\n%s", r, stack)
			tui.CloseDebugLog()
			fmt.Fprintf(os.Stderr, "arborist panic: %v\n%s", r, stack)
			os.Exit(1)
		}
	}()

	// Initialize Kubernetes client (the real DataProvider)
	tui.DebugLog("initializing Kubernetes client")
	var provider data.DataProvider
	k8sClient, err := k8s.NewK8sClient()
	if err != nil {
		tui.DebugLog("WARNING: failed to initialize Kubernetes client: %v", err)
		// provider stays nil — the TUI will show empty data
	} else {
		tui.DebugLog("Kubernetes client initialized successfully")
		provider = k8sClient
	}

	// Resolve kubeconfig context/cluster/user for the header display
	contextName, clusterName := k8s.ResolveCurrentContext()
	userName := k8s.ResolveCurrentUser()
	tui.DebugLog("resolved kubeconfig context=%s cluster=%s user=%s", contextName, clusterName, userName)

	// Resolve K8s server version
	k8sVersion := "(unknown)"
	if k8sClient != nil {
		k8sVersion = k8sClient.GetServerVersion()
	}
	tui.DebugLog("resolved k8s version=%s", k8sVersion)

	// Resolve arborist version from build info
	arboristVersion := resolveArboristVersion()
	tui.DebugLog("arborist version=%s", arboristVersion)

	// Create the Bubble Tea model
	m := tui.NewModel(provider,
		tui.WithContext(context.Background()),
		tui.WithDebug(globals.Debug != ""),
		tui.WithClusterInfo(contextName, clusterName),
		tui.WithUserName(userName),
		tui.WithK8sVersion(k8sVersion),
		tui.WithArboristVersion(arboristVersion),
	)

	// Create and run the Bubble Tea program
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)

	tui.DebugLog("starting bubbletea program")
	if _, err := p.Run(); err != nil {
		tui.DebugLog("program exited with error: %v", err)
		return fmt.Errorf("TUI error: %w", err)
	}
	tui.DebugLog("arborist TUI exiting cleanly")
	return nil
}

// Version can be set at build time via ldflags:
//
//	go build -ldflags "-X github.com/ai-dynamo/grove/arborist/internal/cli.Version=v1.0.0"
var Version string

// resolveArboristVersion returns the arborist version string.
// Checks ldflags-injected Version first, then Go build info, then falls back to "dev".
func resolveArboristVersion() string {
	if v := strings.TrimSpace(Version); v != "" {
		return v
	}

	info, ok := rtdebug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Use the main module version if it's set (not "(devel)")
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	// Fall back to VCS revision from build settings
	var vcsRev, vcsDirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			vcsRev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				vcsDirty = "-dirty"
			}
		}
	}
	if vcsRev != "" {
		short := vcsRev
		if len(short) > 7 {
			short = short[:7]
		}
		return short + vcsDirty
	}

	return "dev"
}
