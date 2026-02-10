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
)

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
