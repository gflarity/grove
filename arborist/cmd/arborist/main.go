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

package main

import (
	"os"

	"github.com/ai-dynamo/grove/arborist/internal/cli"
	"github.com/alecthomas/kong"
)

func main() {
	// Kong doesn't chain two levels of default:"withargs", so bare
	// `arborist` (no args at all) or `arborist --flag ...` (flags only,
	// no subcommand) would fail to resolve tui→forest. Detect the
	// absence of a known subcommand and inject "tui" so the inner
	// default kicks in.
	knownSubcmds := map[string]bool{"tui": true, "topology": true, "diagnostics": true, "diag": true}
	hasSubcmd := false
	for _, arg := range os.Args[1:] {
		if knownSubcmds[arg] {
			hasSubcmd = true
			break
		}
	}
	if !hasSubcmd {
		// Insert "tui" right after the program name, before any flags.
		newArgs := make([]string, 0, len(os.Args)+1)
		newArgs = append(newArgs, os.Args[0], "tui")
		newArgs = append(newArgs, os.Args[1:]...)
		os.Args = newArgs
	}

	var c cli.CLI
	ctx := kong.Parse(&c,
		kong.Name("arborist"),
		kong.Description("Grove cluster inspector — interactive TUI and CLI tools for PodCliqueSets."),
		kong.UsageOnError(),
	)
	defer c.Cleanup()

	err := ctx.Run(&c)
	ctx.FatalIfErrorf(err)
}
