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
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/diagnostics"
	"github.com/ai-dynamo/grove/arborist/internal/k8s"
)

// DiagnosticsCmd collects cluster diagnostics for a PodCliqueSet.
type DiagnosticsCmd struct {
	PodCliqueSet  string `arg:"" optional:"" help:"Name of the PodCliqueSet (optional, collects all if omitted)."`
	Namespace     string `short:"n" help:"Kubernetes namespace (defaults to current kubeconfig context namespace)."`
	AllNamespaces bool   `short:"A" help:"Collect diagnostics across all namespaces."`
	Output        string `short:"o" help:"Output directory for diagnostics bundle." default:"."`
}

// Run executes the diagnostics command.
func (c *DiagnosticsCmd) Run(globals *CLI) (retErr error) {
	defer recoverPanic()
	ns, err := resolveNamespace(c.Namespace, c.AllNamespaces)
	if err != nil {
		return err
	}

	// Create Kubernetes clients
	k8sClient, err := k8s.NewK8sClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Build diagnostic context
	dc := diagnostics.NewDiagnosticContext(
		ctx,
		k8sClient.Clientset(),
		k8sClient.DynamicClient(),
		ns,
	)

	if ns == "" {
		fmt.Fprintln(os.Stderr, "Collecting diagnostics across all namespaces...")
	} else {
		fmt.Fprintf(os.Stderr, "Collecting diagnostics for namespace %q...\n", ns)
	}

	// Collect and bundle diagnostics into a tgz
	tgzPath, err := diagnostics.CollectAndBundle(dc, c.Output)
	if err != nil {
		return fmt.Errorf("failed to collect diagnostics: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Diagnostics bundle written to: %s\n", tgzPath)
	return nil
}
