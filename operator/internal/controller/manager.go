// /*
// Copyright 2024 The Grove Authors.
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

package controller

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	configv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	groveclientscheme "github.com/NVIDIA/grove/operator/internal/client"
	"github.com/NVIDIA/grove/operator/internal/controller/cert"
	"github.com/NVIDIA/grove/operator/internal/webhook"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlmetricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

// pprofBindAddress is the default bind address for pprof profiling endpoints.
const (
	pprofBindAddress = "127.0.0.1:2753"
)

// CreateManager creates and configures a new controller-runtime manager with the provided operator configuration.
func CreateManager(operatorCfg *configv1alpha1.OperatorConfiguration) (ctrl.Manager, error) {
	return ctrl.NewManager(getRestConfig(operatorCfg), createManagerOptions(operatorCfg))
}

// RegisterControllersAndWebhooks registers all controllers and webhooks with the manager.
// It waits for webhook certificates to be ready before proceeding with registration.
func RegisterControllersAndWebhooks(mgr ctrl.Manager, logger logr.Logger, operatorCfg *configv1alpha1.OperatorConfiguration, certsReady chan struct{}) error {
	// Wait for webhook certificates to be ready before registering controllers.
	// Controllers depend on fully operational webhooks with valid certificates.
	cert.WaitTillWebhookCertsReady(logger, certsReady)

	// Register all controllers with their configurations.
	if err := RegisterControllers(mgr, operatorCfg.Controllers); err != nil {
		return err
	}

	// Register all webhooks with the manager.
	if err := webhook.RegisterWebhooks(mgr); err != nil {
		return err
	}
	return nil
}

// SetupHealthAndReadinessEndpoints configures health and readiness probes for the operator.
// The readiness probe waits for webhook certificates to be ready before reporting ready.
func SetupHealthAndReadinessEndpoints(mgr ctrl.Manager, webhookCertsReadyCh chan struct{}) error {
	// Add basic health check endpoint that responds with 200 OK.
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("could not setup health check :%w", err)
	}

	// Add readiness check that waits for webhook certificates and server startup.
	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		select {
		case <-webhookCertsReadyCh:
			return mgr.GetWebhookServer().StartedChecker()(req)
		default:
			return errors.New("certificates are not ready yet")
		}
	}); err != nil {
		return fmt.Errorf("could not setup ready check :%w", err)
	}
	return nil
}

// createManagerOptions builds controller-runtime manager options from the operator configuration.
func createManagerOptions(operatorCfg *configv1alpha1.OperatorConfiguration) ctrl.Options {
	// Configure base manager options with scheme and graceful shutdown.
	opts := ctrl.Options{
		Scheme:                  groveclientscheme.Scheme,
		GracefulShutdownTimeout: ptr.To(5 * time.Second),
		Metrics: ctrlmetricsserver.Options{
			BindAddress: net.JoinHostPort(operatorCfg.Server.Metrics.BindAddress, strconv.Itoa(operatorCfg.Server.Metrics.Port)),
		},
		HealthProbeBindAddress:        net.JoinHostPort(operatorCfg.Server.HealthProbes.BindAddress, strconv.Itoa(operatorCfg.Server.HealthProbes.Port)),
		LeaderElection:                operatorCfg.LeaderElection.Enabled,
		LeaderElectionID:              operatorCfg.LeaderElection.ResourceName,
		LeaderElectionNamespace:       operatorCfg.LeaderElection.ResourceNamespace,
		LeaderElectionResourceLock:    operatorCfg.LeaderElection.ResourceLock,
		LeaderElectionReleaseOnCancel: true,
		LeaseDuration:                 &operatorCfg.LeaderElection.LeaseDuration.Duration,
		RenewDeadline:                 &operatorCfg.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:                   &operatorCfg.LeaderElection.RetryPeriod.Duration,
		Controller: ctrlconfig.Controller{
			RecoverPanic: ptr.To(true),
		},
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Host:    operatorCfg.Server.Webhooks.BindAddress,
			Port:    operatorCfg.Server.Webhooks.Port,
			CertDir: operatorCfg.Server.Webhooks.ServerCertDir,
		}),
	}

	// Enable pprof profiling if configured in debugging options.
	if operatorCfg.Debugging != nil {
		if operatorCfg.Debugging.EnableProfiling != nil &&
			*operatorCfg.Debugging.EnableProfiling {
			opts.PprofBindAddress = pprofBindAddress
		}
	}
	return opts
}

// getRestConfig creates a Kubernetes REST client configuration with operator-specific settings.
func getRestConfig(operatorCfg *configv1alpha1.OperatorConfiguration) *rest.Config {
	// Get the default REST configuration from the environment.
	restCfg := ctrl.GetConfigOrDie()

	// Apply operator-specific client connection settings if provided.
	if operatorCfg != nil {
		restCfg.Burst = operatorCfg.ClientConnection.Burst
		restCfg.QPS = operatorCfg.ClientConnection.QPS
		restCfg.AcceptContentTypes = operatorCfg.ClientConnection.AcceptContentTypes
		restCfg.ContentType = operatorCfg.ClientConnection.ContentType
	}
	return restCfg
}
