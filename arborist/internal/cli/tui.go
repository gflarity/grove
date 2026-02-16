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
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
	"github.com/ai-dynamo/grove/arborist/internal/debug"
	"github.com/ai-dynamo/grove/arborist/internal/k8s"
	"github.com/ai-dynamo/grove/arborist/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// ForestCmd implements the "tui" subcommand (the default). It renders the
// forest view with optional resource type, namespace scoping, and name filter.
type ForestCmd struct {
	Resource string `arg:"" optional:"" default:"pcs" help:"Resource type to display: pcs, pc, pcsg, pod."`
	NamespaceFlags
	Filter string `short:"f" help:"Filter resources by name." default:""`
}

// Run executes the ForestCmd (TUI forest view).
func (c *ForestCmd) Run(globals *CLI) error {
	// Resolve namespace: defaults to all-namespaces when neither -n nor -A is given.
	namespace, allNamespaces := defaultToAllNamespaces(c.Namespace, c.AllNamespaces)

	// Resolve kubeconfig context/cluster/user for the header display (single load)
	kubeInfo := k8s.ResolveKubeConfigInfo()
	contextName, clusterName := kubeInfo.ContextName, kubeInfo.ClusterName
	userName := kubeInfo.UserName

	// Initialize Kubernetes client
	debug.Log("initializing Kubernetes client")
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startupCancel()

	var globalCache clusterstate.GlobalCache
	var connErr error
	k8sClient, err := k8s.NewK8sClient()
	if err != nil {
		debug.Log("WARNING: failed to initialize Kubernetes client: %v", err)
		connErr = err
		// globalCache stays nil — the TUI will show the error in its error log
	} else {
		debug.Log("Kubernetes client initialized successfully")

		// Pre-flight: verify Grove CRDs are installed before starting the TUI.
		// Without this, missing CRDs cause client-go reflector errors that
		// interleave with alt-screen rendering, producing unreadable output.
		if missing := k8sClient.CheckGroveCRDs(startupCtx); len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "Error: Grove CRDs not found on cluster %q:\n", clusterName)
			for _, name := range missing {
				fmt.Fprintf(os.Stderr, "  - %s.grove.io not found\n", name)
			}
			fmt.Fprintln(os.Stderr, "\nPlease install Grove operator CRDs before using arborist.")
			return fmt.Errorf("missing Grove CRDs")
		}

		var cacheOpts []k8s.GlobalCacheOption
		if namespace != "" {
			cacheOpts = append(cacheOpts, k8s.WithCacheNamespace(namespace))
		}
		globalCache = k8sClient.NewGlobalCache(cacheOpts...)
		debug.Log("global cache created (namespace=%q)", namespace)
	}
	debug.Log("resolved kubeconfig context=%s cluster=%s user=%s", contextName, clusterName, userName)

	// Resolve K8s server version
	k8sVersion := "(unknown)"
	if k8sClient != nil {
		k8sVersion = k8sClient.GetServerVersion(startupCtx)
	}
	debug.Log("resolved k8s version=%s", k8sVersion)

	// Resolve arborist version from build info
	arboristVersion := resolveArboristVersion()
	debug.Log("arborist version=%s", arboristVersion)

	debug.Log("forest args: resource=%s namespace=%q allNamespaces=%v filter=%q",
		c.Resource, namespace, allNamespaces, c.Filter)

	// Build TUI model options
	opts := []tui.Option{
		tui.WithContext(context.Background()),
		tui.WithDebug(globals.Debug != ""),
		tui.WithClusterInfo(contextName, clusterName),
		tui.WithUserName(userName),
		tui.WithK8sVersion(k8sVersion),
		tui.WithArboristVersion(arboristVersion),
		tui.WithForestResourceType(c.Resource),
		tui.WithNamespace(namespace),
		tui.WithAllNamespaces(allNamespaces),
	}
	if c.Filter != "" {
		opts = append(opts, tui.WithFilter(c.Filter))
	}
	if connErr != nil {
		opts = append(opts, tui.WithConnectionError(connErr))
	}

	// Create the Bubble Tea model
	m := tui.NewModel(globalCache, opts...)

	// Ensure global cache is cleaned up when the TUI exits
	if globalCache != nil {
		defer globalCache.Stop()
	}

	// Create and run the Bubble Tea program
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)

	debug.Log("starting bubbletea program")
	if _, err := p.Run(); err != nil {
		debug.Log("program exited with error: %v", err)
		return fmt.Errorf("TUI error: %w", err)
	}
	debug.Log("arborist TUI exiting cleanly")
	return nil
}

// Version can be set at build time via ldflags:
//
//	go build -ldflags "-X github.com/ai-dynamo/grove/arborist/internal/cli.Version=v1.0.0"
var Version string

// resolveArboristVersion returns the arborist version string.
func resolveArboristVersion() string {
	if v := strings.TrimSpace(Version); v != "" {
		return v
	}

	info, ok := rtdebug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

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
