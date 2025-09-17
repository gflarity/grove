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
	"io/ioutil"
	"os"
	"testing"
	"time"

	configv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	groveclientscheme "github.com/NVIDIA/grove/operator/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestCreateManagerOptions tests the createManagerOptions function which builds
// controller-runtime manager options from the operator configuration.
func TestCreateManagerOptions(t *testing.T) {
	tests := []struct {
		// Test case description
		name string
		// Input operator configuration
		operatorCfg *configv1alpha1.OperatorConfiguration
		// Function to validate the generated options
		validateOptions func(t *testing.T, opts ctrl.Options)
	}{
		{
			// Test options creation with minimal configuration
			name: "minimal_configuration",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				Server: configv1alpha1.ServerConfiguration{
					Webhooks: configv1alpha1.WebhookServer{
						Server: configv1alpha1.Server{
							BindAddress: "127.0.0.1",
							Port:        9443,
						},
						ServerCertDir: "/tmp/certs",
					},
					HealthProbes: &configv1alpha1.Server{
						BindAddress: "127.0.0.1",
						Port:        8081,
					},
					Metrics: &configv1alpha1.Server{
						BindAddress: "127.0.0.1",
						Port:        8080,
					},
				},
				LeaderElection: configv1alpha1.LeaderElectionConfiguration{
					Enabled:           false,
					LeaseDuration:     metav1.Duration{Duration: 15 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 10 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 2 * time.Second},
					ResourceLock:      "leases",
					ResourceName:      "grove-operator",
					ResourceNamespace: "grove-system",
				},
			},
			validateOptions: func(t *testing.T, opts ctrl.Options) {
				assert.Equal(t, groveclientscheme.Scheme, opts.Scheme)
				assert.Equal(t, 5*time.Second, *opts.GracefulShutdownTimeout)
				assert.Equal(t, "127.0.0.1:8080", opts.Metrics.BindAddress)
				assert.Equal(t, "127.0.0.1:8081", opts.HealthProbeBindAddress)
				assert.False(t, opts.LeaderElection)
				assert.Equal(t, "grove-operator", opts.LeaderElectionID)
				assert.Equal(t, "grove-system", opts.LeaderElectionNamespace)
				assert.True(t, opts.LeaderElectionReleaseOnCancel)
				assert.NotNil(t, opts.WebhookServer)
				assert.Empty(t, opts.PprofBindAddress)
			},
		},
		{
			// Test options creation with leader election enabled
			name: "leader_election_enabled",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				Server: configv1alpha1.ServerConfiguration{
					Webhooks: configv1alpha1.WebhookServer{
						Server: configv1alpha1.Server{
							BindAddress: "0.0.0.0",
							Port:        9443,
						},
						ServerCertDir: "/var/certs",
					},
					HealthProbes: &configv1alpha1.Server{
						BindAddress: "0.0.0.0",
						Port:        8081,
					},
					Metrics: &configv1alpha1.Server{
						BindAddress: "0.0.0.0",
						Port:        8080,
					},
				},
				LeaderElection: configv1alpha1.LeaderElectionConfiguration{
					Enabled:           true,
					LeaseDuration:     metav1.Duration{Duration: 30 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 20 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 5 * time.Second},
					ResourceLock:      "leases",
					ResourceName:      "grove-operator-leader",
					ResourceNamespace: "grove-system",
				},
			},
			validateOptions: func(t *testing.T, opts ctrl.Options) {
				assert.True(t, opts.LeaderElection)
				assert.Equal(t, "grove-operator-leader", opts.LeaderElectionID)
				assert.Equal(t, "grove-system", opts.LeaderElectionNamespace)
				assert.Equal(t, "leases", opts.LeaderElectionResourceLock)
				assert.Equal(t, 30*time.Second, *opts.LeaseDuration)
				assert.Equal(t, 20*time.Second, *opts.RenewDeadline)
				assert.Equal(t, 5*time.Second, *opts.RetryPeriod)
				assert.Empty(t, opts.PprofBindAddress)
			},
		},
		{
			// Test options creation with profiling enabled
			name: "profiling_enabled",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				Server: configv1alpha1.ServerConfiguration{
					Webhooks: configv1alpha1.WebhookServer{
						Server: configv1alpha1.Server{
							BindAddress: "127.0.0.1",
							Port:        9443,
						},
						ServerCertDir: "/tmp/certs",
					},
					HealthProbes: &configv1alpha1.Server{
						BindAddress: "127.0.0.1",
						Port:        8081,
					},
					Metrics: &configv1alpha1.Server{
						BindAddress: "127.0.0.1",
						Port:        8080,
					},
				},
				LeaderElection: configv1alpha1.LeaderElectionConfiguration{
					Enabled:           false,
					LeaseDuration:     metav1.Duration{Duration: 15 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 10 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 2 * time.Second},
					ResourceLock:      "leases",
					ResourceName:      "grove-operator",
					ResourceNamespace: "grove-system",
				},
				Debugging: &configv1alpha1.DebuggingConfiguration{
					EnableProfiling: ptr.To(true),
				},
			},
			validateOptions: func(t *testing.T, opts ctrl.Options) {
				assert.Equal(t, pprofBindAddress, opts.PprofBindAddress)
			},
		},
		{
			// Test options creation with profiling explicitly disabled
			name: "profiling_disabled",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				Server: configv1alpha1.ServerConfiguration{
					Webhooks: configv1alpha1.WebhookServer{
						Server: configv1alpha1.Server{
							BindAddress: "127.0.0.1",
							Port:        9443,
						},
						ServerCertDir: "/tmp/certs",
					},
					HealthProbes: &configv1alpha1.Server{
						BindAddress: "127.0.0.1",
						Port:        8081,
					},
					Metrics: &configv1alpha1.Server{
						BindAddress: "127.0.0.1",
						Port:        8080,
					},
				},
				LeaderElection: configv1alpha1.LeaderElectionConfiguration{
					Enabled:           false,
					LeaseDuration:     metav1.Duration{Duration: 15 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 10 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 2 * time.Second},
					ResourceLock:      "leases",
					ResourceName:      "grove-operator",
					ResourceNamespace: "grove-system",
				},
				Debugging: &configv1alpha1.DebuggingConfiguration{
					EnableProfiling: ptr.To(false),
				},
			},
			validateOptions: func(t *testing.T, opts ctrl.Options) {
				assert.Empty(t, opts.PprofBindAddress)
			},
		},
		{
			// Test options creation with custom port configurations
			name: "custom_ports",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				Server: configv1alpha1.ServerConfiguration{
					Webhooks: configv1alpha1.WebhookServer{
						Server: configv1alpha1.Server{
							BindAddress: "192.168.1.100",
							Port:        8443,
						},
						ServerCertDir: "/custom/certs",
					},
					HealthProbes: &configv1alpha1.Server{
						BindAddress: "192.168.1.100",
						Port:        9081,
					},
					Metrics: &configv1alpha1.Server{
						BindAddress: "192.168.1.100",
						Port:        9080,
					},
				},
				LeaderElection: configv1alpha1.LeaderElectionConfiguration{
					Enabled:           false,
					LeaseDuration:     metav1.Duration{Duration: 15 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 10 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 2 * time.Second},
					ResourceLock:      "leases",
					ResourceName:      "grove-operator",
					ResourceNamespace: "grove-system",
				},
			},
			validateOptions: func(t *testing.T, opts ctrl.Options) {
				assert.Equal(t, "192.168.1.100:9080", opts.Metrics.BindAddress)
				assert.Equal(t, "192.168.1.100:9081", opts.HealthProbeBindAddress)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := createManagerOptions(tt.operatorCfg)

			// Common validations for all test cases
			assert.NotNil(t, opts.Scheme)
			assert.NotNil(t, opts.GracefulShutdownTimeout)
			assert.NotNil(t, opts.WebhookServer)

			if tt.validateOptions != nil {
				tt.validateOptions(t, opts)
			}
		})
	}
}

// TestGetRestConfig tests the getRestConfig function which creates a Kubernetes
// REST client configuration with operator-specific settings.
func TestGetRestConfig(t *testing.T) {
	// Set up environment variable for kubeconfig since ctrl.GetConfigOrDie() depends on it
	originalKubeconfig := os.Getenv("KUBECONFIG")
	defer func() {
		if originalKubeconfig != "" {
			os.Setenv("KUBECONFIG", originalKubeconfig)
		} else {
			os.Unsetenv("KUBECONFIG")
		}
	}()

	// Create a temporary kubeconfig file for testing
	testKubeconfig := `
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: test-token
`

	tmpFile, err := ioutil.TempFile("", "kubeconfig-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(testKubeconfig)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	os.Setenv("KUBECONFIG", tmpFile.Name())

	tests := []struct {
		// Test case description
		name string
		// Input operator configuration
		operatorCfg *configv1alpha1.OperatorConfiguration
		// Function to validate the REST configuration
		validateConfig func(t *testing.T, cfg *rest.Config)
	}{
		{
			// Test REST config creation with nil operator configuration
			name:        "nil_operator_config",
			operatorCfg: nil,
			validateConfig: func(t *testing.T, cfg *rest.Config) {
				assert.NotNil(t, cfg)
				// Default values should be used when operator config is nil
			},
		},
		{
			// Test REST config creation with custom client connection settings
			name: "custom_client_connection",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				ClientConnection: configv1alpha1.ClientConnectionConfiguration{
					QPS:                25.0,
					Burst:              50,
					ContentType:        "application/vnd.kubernetes.protobuf",
					AcceptContentTypes: "application/vnd.kubernetes.protobuf,application/json",
				},
			},
			validateConfig: func(t *testing.T, cfg *rest.Config) {
				assert.NotNil(t, cfg)
				assert.Equal(t, float32(25.0), cfg.QPS)
				assert.Equal(t, 50, cfg.Burst)
				assert.Equal(t, "application/vnd.kubernetes.protobuf", cfg.ContentType)
				assert.Equal(t, "application/vnd.kubernetes.protobuf,application/json", cfg.AcceptContentTypes)
			},
		},
		{
			// Test REST config creation with default client connection settings
			name: "default_client_connection",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				ClientConnection: configv1alpha1.ClientConnectionConfiguration{
					QPS:                5.0,
					Burst:              10,
					ContentType:        "application/json",
					AcceptContentTypes: "application/json",
				},
			},
			validateConfig: func(t *testing.T, cfg *rest.Config) {
				assert.NotNil(t, cfg)
				assert.Equal(t, float32(5.0), cfg.QPS)
				assert.Equal(t, 10, cfg.Burst)
				assert.Equal(t, "application/json", cfg.ContentType)
				assert.Equal(t, "application/json", cfg.AcceptContentTypes)
			},
		},
		{
			// Test REST config creation with high-throughput client settings
			name: "high_throughput_client_connection",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				ClientConnection: configv1alpha1.ClientConnectionConfiguration{
					QPS:                100.0,
					Burst:              200,
					ContentType:        "application/json",
					AcceptContentTypes: "application/json",
				},
			},
			validateConfig: func(t *testing.T, cfg *rest.Config) {
				assert.NotNil(t, cfg)
				assert.Equal(t, float32(100.0), cfg.QPS)
				assert.Equal(t, 200, cfg.Burst)
				assert.Equal(t, "application/json", cfg.ContentType)
				assert.Equal(t, "application/json", cfg.AcceptContentTypes)
			},
		},
		{
			// Test REST config creation with zero QPS (unlimited)
			name: "unlimited_qps",
			operatorCfg: &configv1alpha1.OperatorConfiguration{
				ClientConnection: configv1alpha1.ClientConnectionConfiguration{
					QPS:                0.0,
					Burst:              1,
					ContentType:        "application/json",
					AcceptContentTypes: "application/json",
				},
			},
			validateConfig: func(t *testing.T, cfg *rest.Config) {
				assert.NotNil(t, cfg)
				assert.Equal(t, float32(0.0), cfg.QPS)
				assert.Equal(t, 1, cfg.Burst)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := getRestConfig(tt.operatorCfg)

			// Common validations for all test cases
			assert.NotNil(t, cfg)

			if tt.validateConfig != nil {
				tt.validateConfig(t, cfg)
			}
		})
	}
}

// TestPprofBindAddressConstant tests that the pprof bind address constant is correctly defined.
func TestPprofBindAddressConstant(t *testing.T) {
	// Test that the constant is properly defined and has expected value
	assert.Equal(t, "127.0.0.1:2753", pprofBindAddress)
}
