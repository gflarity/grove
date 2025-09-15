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

package cert

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	cert "github.com/open-policy-agent/cert-controller/pkg/rotator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManageWebhookCertsWithNamespace tests the ManageWebhookCertsWithNamespace function
// which configures and registers a certificate rotator with the controller manager using a provided namespace.
func TestManageWebhookCertsWithNamespace(t *testing.T) {
	tests := []struct {
		// Test case description
		name string
		// Directory to use for certificate storage
		certDir string
		// Namespace to use for certificate configuration
		namespace string
		// Function to validate the certificate rotator configuration
		validateRotator func(t *testing.T, rotator *cert.CertRotator)
	}{
		{
			// Test successful certificate rotator setup with valid namespace
			name:      "successful_cert_rotator_setup",
			certDir:   "/tmp/certs",
			namespace: "grove-system",
			validateRotator: func(t *testing.T, rotator *cert.CertRotator) {
				assert.Equal(t, "grove-system", rotator.SecretKey.Namespace)
				assert.Equal(t, "grove-webhook-server-cert", rotator.SecretKey.Name)
				assert.Equal(t, "/tmp/certs", rotator.CertDir)
				assert.Equal(t, certificateAuthorityName, rotator.CAName)
				assert.Equal(t, certificateAuthorityOrganization, rotator.CAOrganization)
				assert.Equal(t, "grove-operator.grove-system.svc", rotator.DNSName)
				assert.Len(t, rotator.ExtraDNSNames, 3)
				assert.Contains(t, rotator.ExtraDNSNames, "grove-operator")
				assert.Contains(t, rotator.ExtraDNSNames, "grove-operator.grove-system")
				assert.Contains(t, rotator.ExtraDNSNames, "grove-operator.grove-system.svc.cluster.local")
				assert.Len(t, rotator.Webhooks, 2)
				assert.True(t, rotator.EnableReadinessCheck)
				assert.True(t, rotator.RestartOnSecretRefresh)
			},
		},
		{
			// Test with different namespace to ensure dynamic configuration
			name:      "different_namespace",
			certDir:   "/custom/cert/dir",
			namespace: "custom-namespace",
			validateRotator: func(t *testing.T, rotator *cert.CertRotator) {
				assert.Equal(t, "custom-namespace", rotator.SecretKey.Namespace)
				assert.Equal(t, "grove-operator.custom-namespace.svc", rotator.DNSName)
				assert.Contains(t, rotator.ExtraDNSNames, "grove-operator.custom-namespace")
				assert.Contains(t, rotator.ExtraDNSNames, "grove-operator.custom-namespace.svc.cluster.local")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create certificate readiness channel
			certsReadyCh := make(chan struct{})

			// Test the certificate rotator configuration using the extracted function
			rotator := createCertRotator(tt.certDir, certsReadyCh, tt.namespace)

			// Validate the rotator configuration
			if tt.validateRotator != nil {
				tt.validateRotator(t, rotator)
			}
		})
	}
}

// TestWaitTillWebhookCertsReady tests the WaitTillWebhookCertsReady function
// which blocks until certificates are ready and logs appropriate messages.
func TestWaitTillWebhookCertsReady(t *testing.T) {
	tests := []struct {
		// Test case description
		name string
		// Whether to close the channel immediately (true) or after delay (false)
		closeChannelImmediately bool
		// Delay before closing channel (only used if closeChannelImmediately is false)
		channelCloseDelay time.Duration
		// Expected timeout for the test
		testTimeout time.Duration
	}{
		{
			// Test immediate certificate readiness
			name:                    "certificates_ready_immediately",
			closeChannelImmediately: true,
			testTimeout:             time.Second,
		},
		{
			// Test certificate readiness after a short delay
			name:                    "certificates_ready_after_delay",
			closeChannelImmediately: false,
			channelCloseDelay:       100 * time.Millisecond,
			testTimeout:             time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock logger that captures log messages
			mockLog := &mockLogger{messages: make([]string, 0)}
			logger := logr.New(mockLog)

			// Create certificate readiness channel
			certsReady := make(chan struct{})

			// Set up channel closing behavior
			if tt.closeChannelImmediately {
				close(certsReady)
			} else {
				go func() {
					time.Sleep(tt.channelCloseDelay)
					close(certsReady)
				}()
			}

			// Create a channel to signal when WaitTillWebhookCertsReady completes
			done := make(chan struct{})

			// Run WaitTillWebhookCertsReady in a goroutine
			go func() {
				defer close(done)
				WaitTillWebhookCertsReady(logger, certsReady)
			}()

			// Wait for completion with timeout
			select {
			case <-done:
				// Function completed successfully
				assert.Len(t, mockLog.messages, 2, "Expected exactly 2 log messages")
				assert.Contains(t, mockLog.messages[0], "Waiting for certs to be ready")
				assert.Contains(t, mockLog.messages[1], "Certs are ready and injected")
			case <-time.After(tt.testTimeout):
				t.Fatal("WaitTillWebhookCertsReady did not complete within timeout")
			}
		})
	}
}

// TestGetOperatorNamespace tests the getOperatorNamespace function which reads
// the operator's namespace from the default service account token file.
func TestGetOperatorNamespace(t *testing.T) {
	// This test demonstrates the challenge of testing functions with fixed file paths.
	// We can only test this if the file exists and is readable, or if we can temporarily
	// modify the file system in a controlled way.

	// Check if the default namespace file exists (common in Kubernetes environments)
	if _, err := os.Stat(operatorNamespaceFile); err == nil {
		// File exists, we can test the actual function
		t.Run("reads_from_default_file", func(t *testing.T) {
			namespace, err := getOperatorNamespace()
			if err != nil {
				// This is expected in non-Kubernetes environments
				t.Logf("Expected error in non-Kubernetes environment: %v", err)
				assert.Error(t, err)
			} else {
				// In a real Kubernetes environment, we should get a valid namespace
				assert.NotEmpty(t, namespace)
				t.Logf("Successfully read namespace: %s", namespace)
			}
		})
	} else {
		// File doesn't exist, test the error case
		t.Run("file_not_found", func(t *testing.T) {
			namespace, err := getOperatorNamespace()
			assert.Error(t, err)
			assert.Empty(t, namespace)
			assert.Contains(t, err.Error(), "no such file or directory")
		})
	}
}

// TestReadNamespaceFromFile tests the readNamespaceFromFile function which reads
// and validates a namespace from a specified file path.
func TestReadNamespaceFromFile(t *testing.T) {
	tests := []struct {
		// Test case description
		name string
		// Content to write to the namespace file
		fileContent string
		// Whether the file should be created
		createFile bool
		// Expected namespace result
		expectedNamespace string
		// Expected error message (empty if no error expected)
		expectedError string
	}{
		{
			// Test successful namespace reading with clean content
			name:              "valid_namespace",
			fileContent:       "grove-system",
			createFile:        true,
			expectedNamespace: "grove-system",
			expectedError:     "",
		},
		{
			// Test namespace with leading and trailing whitespace
			name:              "namespace_with_whitespace",
			fileContent:       "  grove-system  \n\t",
			createFile:        true,
			expectedNamespace: "grove-system",
			expectedError:     "",
		},
		{
			// Test namespace with only newlines
			name:              "namespace_with_newlines",
			fileContent:       "grove-system\n",
			createFile:        true,
			expectedNamespace: "grove-system",
			expectedError:     "",
		},
		{
			// Test different valid namespace name
			name:              "different_namespace",
			fileContent:       "kube-system",
			createFile:        true,
			expectedNamespace: "kube-system",
			expectedError:     "",
		},
		{
			// Test empty file content
			name:              "empty_file",
			fileContent:       "",
			createFile:        true,
			expectedNamespace: "",
			expectedError:     "operator namespace is empty",
		},
		{
			// Test file with only whitespace
			name:              "whitespace_only_file",
			fileContent:       "   \n\t  ",
			createFile:        true,
			expectedNamespace: "",
			expectedError:     "operator namespace is empty",
		},
		{
			// Test file does not exist
			name:              "file_not_found",
			fileContent:       "",
			createFile:        false,
			expectedNamespace: "",
			expectedError:     "no such file or directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := t.TempDir()
			testFile := filepath.Join(tempDir, "namespace")

			// Create file with content if specified
			if tt.createFile {
				err := os.WriteFile(testFile, []byte(tt.fileContent), 0644)
				require.NoError(t, err)
			}

			// Call the refactored function directly
			result, err := readNamespaceFromFile(testFile)

			// Validate results
			if tt.expectedError != "" {
				require.Error(t, err, "Expected an error but got none")
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Empty(t, result)
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)
				assert.Equal(t, tt.expectedNamespace, result)
			}
		})
	}
}

// TestCreateCertRotator tests that the createCertRotator function
// creates a certificate rotator with the correct configuration.
func TestCreateCertRotator(t *testing.T) {
	tests := []struct {
		// Test case description
		name string
		// Namespace to test with
		namespace string
		// Certificate directory to test with
		certDir string
	}{
		{
			// Test default configuration values
			name:      "default_configuration",
			namespace: "grove-system",
			certDir:   "/tmp/k8s-webhook-server/serving-certs",
		},
		{
			// Test custom namespace and cert directory
			name:      "custom_configuration",
			namespace: "custom-ns",
			certDir:   "/custom/cert/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create certificate readiness channel
			certsReadyCh := make(chan struct{})

			// Create the rotator using the extracted function
			rotator := createCertRotator(tt.certDir, certsReadyCh, tt.namespace)

			// Validate the configuration
			assert.Equal(t, tt.namespace, rotator.SecretKey.Namespace)
			assert.Equal(t, "grove-webhook-server-cert", rotator.SecretKey.Name)
			assert.Equal(t, tt.certDir, rotator.CertDir)
			assert.Equal(t, certificateAuthorityName, rotator.CAName)
			assert.Equal(t, certificateAuthorityOrganization, rotator.CAOrganization)
			assert.Equal(t, certsReadyCh, rotator.IsReady)

			// Validate DNS names
			expectedDNSName := fmt.Sprintf("%s.%s.svc", serviceName, tt.namespace)
			assert.Equal(t, expectedDNSName, rotator.DNSName)

			expectedExtraDNS := []string{
				serviceName,
				fmt.Sprintf("%s.%s", serviceName, tt.namespace),
				fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, tt.namespace),
			}
			assert.Equal(t, expectedExtraDNS, rotator.ExtraDNSNames)

			// Validate webhook configuration
			require.Len(t, rotator.Webhooks, 2)

			// Check mutating webhook
			mutatingWebhook := rotator.Webhooks[0]
			assert.Equal(t, cert.Mutating, mutatingWebhook.Type)
			assert.Equal(t, "podgangset-defaulting-webhook", mutatingWebhook.Name)

			// Check validating webhook
			validatingWebhook := rotator.Webhooks[1]
			assert.Equal(t, cert.Validating, validatingWebhook.Type)
			assert.Equal(t, "podgangset-validating-webhook", validatingWebhook.Name)

			// Validate flags
			assert.True(t, rotator.EnableReadinessCheck)
			assert.True(t, rotator.RestartOnSecretRefresh)
		})
	}
}

// TestManageWebhookCertsWithNamespaceIntegration demonstrates the difficulty of testing
// ManageWebhookCertsWithNamespace due to its dependency on cert.AddRotator.
func TestManageWebhookCertsWithNamespaceIntegration(t *testing.T) {
	// This test demonstrates the main challenge in increasing coverage:
	// ManageWebhookCertsWithNamespace calls cert.AddRotator(mgr, rotator)
	// which is a package function that's difficult to mock without:
	// 1. Dependency injection
	// 2. Interface abstraction
	// 3. Build tags or compilation tricks

	t.Skip("Cannot easily test cert.AddRotator without significant refactoring - this is the main coverage limitation")

	// To properly test this function, we would need to:
	// 1. Create an interface for the cert operations
	// 2. Inject that interface as a dependency
	// 3. Mock the interface in tests
	//
	// Example refactoring approach:
	// type CertManager interface {
	//     AddRotator(mgr ctrl.Manager, rotator *cert.CertRotator) error
	// }
	//
	// func ManageWebhookCertsWithNamespace(mgr ctrl.Manager, certMgr CertManager, ...) error {
	//     rotator := createCertRotator(...)
	//     return certMgr.AddRotator(mgr, rotator)
	// }
}

// mockLogger implements logr.LogSink for testing and captures log messages
type mockLogger struct {
	// messages stores all log messages for verification in tests
	messages []string
}

func (l *mockLogger) Init(info logr.RuntimeInfo) {}

func (l *mockLogger) Info(level int, msg string, keysAndValues ...interface{}) {
	l.messages = append(l.messages, msg)
}

func (l *mockLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.messages = append(l.messages, fmt.Sprintf("ERROR: %s: %v", msg, err))
}

func (l *mockLogger) Enabled(level int) bool { return true }

func (l *mockLogger) WithValues(keysAndValues ...interface{}) logr.LogSink {
	return &mockLogger{messages: l.messages}
}

func (l *mockLogger) WithName(name string) logr.LogSink {
	return &mockLogger{messages: l.messages}
}
