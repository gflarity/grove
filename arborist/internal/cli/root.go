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
	"fmt"
	"os"
	"strings"

	"github.com/ai-dynamo/grove/arborist/internal/debug"
)

// CLI defines the top-level Kong command structure for arborist.
type CLI struct {
	Debug string `help:"Write arborist CLI debug logs to the given file path." short:"d" type:"path"`

	// Subcommands
	TUI         ForestCmd      `cmd:"" default:"withargs" help:"Launch the interactive TUI."`
	Topology    TopologyCmd    `cmd:"" help:"Show pods grouped by topology domain."`
	Diagnostics DiagnosticsCmd `cmd:"" aliases:"diag" help:"Collect cluster diagnostics for a PodCliqueSet."`
}

// AfterApply is called by Kong after CLI flags are parsed but before a
// subcommand runs.  It initialises the debug log when --debug is set.
func (c *CLI) AfterApply() error {
	if c.Debug != "" {
		path := c.Debug
		// Kong's type:"path" does not expand ~; handle ~/... manually.
		// Note: ~user syntax is not supported.
		if strings.HasPrefix(path, "~/") || path == "~" {
			if home, err := os.UserHomeDir(); err == nil {
				path = home + path[1:]
			}
		}
		if err := debug.Init(path); err != nil {
			return fmt.Errorf("failed to open debug log: %w", err)
		}
	}
	debug.Log("starting arborist")
	return nil
}

// Cleanup closes the debug log.  The caller (main) should defer this.
func (c *CLI) Cleanup() {
	debug.Close()
}
