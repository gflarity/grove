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

// Package main implements the Grove init container, which waits for parent PodCliques
// to become ready before allowing dependent pods to start. This ensures proper startup
// ordering in multi-pod deployments.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	configv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/initc/cmd/opts"
	"github.com/NVIDIA/grove/operator/initc/internal"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"
	"github.com/NVIDIA/grove/operator/internal/logger"
	"github.com/NVIDIA/grove/operator/internal/version"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	// log is the structured logger for the init container
	log = logger.MustNewLogger(false, configv1alpha1.InfoLevel, configv1alpha1.LogFormatJSON).WithName("grove-initc")
)

// main is the entry point for the Grove init container. It initializes the application,
// parses configuration, and waits for parent PodCliques to become ready before exiting.
func main() {
	ctx := setupSignalHandler()

	// Parse command-line options and generate configuration
	config, err := opts.InitializeCLIOptions()
	if err != nil {
		log.Error(err, "Failed to generate configuration for the init container from the flags")
		os.Exit(1)
	}
	version.PrintVersionAndExitIfRequested()

	log.Info("Starting grove init container", "version", version.Get())

	// Parse PodClique dependencies from configuration
	podCliqueDependencies, err := config.GetPodCliqueDependencies()
	if err != nil {
		log.Error(err, "Failed to parse CLI input")
		os.Exit(1)
	}

	// Create PodClique state manager
	podCliqueState, err := internal.NewPodCliqueState(podCliqueDependencies, log)
	if err != nil {
		os.Exit(1)
	}

	// Create Kubernetes client for API server communication
	client, err := createClient()
	if err != nil {
		log.Error(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}

	// Wait for all parent PodCliques to become ready
	if err = podCliqueState.WaitForReady(ctx, client, log); err != nil {
		log.Error(err, "Failed to wait for all parent PodCliques")
		os.Exit(1)
	}

	log.Info("Successfully waited for all parent PodCliques to start up")
}

// createClient creates a Kubernetes clientset using in-cluster configuration.
// Returns an error if the cluster config cannot be loaded or the client cannot be created.
func createClient() (*kubernetes.Clientset, error) {
	// Get in-cluster config using service account credentials
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		// Wrap error with context for debugging
		return nil, groveerr.WrapError(
			err,
			grovecorev1alpha1.ErrorCode("ERR_CLIENT_CREATION"),
			"CreateKubernetesClient",
			"failed to fetch the in cluster config",
		)
	}
	// Create typed clientset for Kubernetes API operations
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		// Wrap error with context for debugging
		return nil, groveerr.WrapError(
			err,
			grovecorev1alpha1.ErrorCode("ERR_CLIENT_CREATION"),
			"CreateKubernetesClient",
			"failed to create clientSet with the fetched restConfig",
		)
	}
	return client, nil
}

// setupSignalHandler creates a context that gets cancelled on SIGTERM or SIGINT signals.
// The first signal cancels the context, allowing graceful shutdown. The second signal
// forces immediate termination with exit code 1.
func setupSignalHandler() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	// Buffer size of 2 allows handling two signals before blocking
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-ch // First signal: cancel context for graceful shutdown
		cancel()
		<-ch // Second signal: force immediate termination
		os.Exit(1)
	}()

	return ctx
}
