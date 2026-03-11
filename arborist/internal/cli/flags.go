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

	"github.com/ai-dynamo/grove/arborist/internal/k8s"
)

// NamespaceFlags holds the common namespace-scoping flags shared by all CLI commands.
type NamespaceFlags struct {
	Namespace     string `short:"n" help:"Kubernetes namespace (defaults to current kubeconfig context namespace)."`
	AllNamespaces bool   `short:"A" help:"Show resources across all namespaces."`
}

// resolveNamespace returns the effective namespace for a CLI command.
// If allNamespaces is true, returns "" (all namespaces).
// If ns is non-empty, returns it directly.
// Otherwise, resolves from the current kubeconfig context.
func resolveNamespace(ns string, allNamespaces bool) (string, error) {
	if allNamespaces {
		return "", nil
	}
	if ns != "" {
		return ns, nil
	}
	resolved, err := k8s.ResolveCurrentNamespace()
	if err != nil {
		return "", fmt.Errorf("failed to resolve namespace: %w", err)
	}
	return resolved, nil
}

// defaultToAllNamespaces returns the effective namespace for commands that
// default to all-namespaces when neither -n nor -A is given (e.g. the TUI).
// Returns (namespace, allNamespaces).
func defaultToAllNamespaces(ns string, allNamespaces bool) (string, bool) {
	if allNamespaces {
		return "", true
	}
	if ns != "" {
		return ns, false
	}
	// Neither flag given — default to all namespaces
	return "", true
}
