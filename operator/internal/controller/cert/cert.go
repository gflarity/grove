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
	"strings"

	defaultingwebhook "github.com/NVIDIA/grove/operator/internal/webhook/admission/pgs/defaulting"
	validatingwebhook "github.com/NVIDIA/grove/operator/internal/webhook/admission/pgs/validation"

	"github.com/go-logr/logr"
	cert "github.com/open-policy-agent/cert-controller/pkg/rotator"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Certificate management constants
const (
	// serviceName is the Kubernetes service name for the Grove operator
	serviceName = "grove-operator"
	// certificateAuthorityName is the name of the CA used for webhook certificates
	certificateAuthorityName = "Grove-CA"
	// certificateAuthorityOrganization is the organization name for the CA
	certificateAuthorityOrganization = "Grove"
	// operatorNamespaceFile is the path to the file containing the operator's namespace
	operatorNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// ManageWebhookCerts registers the cert-controller with the manager which will be used to manage
// webhook certificates.
func ManageWebhookCerts(mgr ctrl.Manager, certDir string, certsReadyCh chan struct{}) error {
	// Get the namespace where the operator is running
	namespace, err := getOperatorNamespace()
	if err != nil {
		return err
	}
	return ManageWebhookCertsWithNamespace(mgr, certDir, certsReadyCh, namespace)
}

// ManageWebhookCertsWithNamespace registers the cert-controller with the manager using the provided namespace.
// This function is extracted to make testing easier by removing the file system dependency.
func ManageWebhookCertsWithNamespace(mgr ctrl.Manager, certDir string, certsReadyCh chan struct{}, namespace string) error {
	// Configure the certificate rotator with webhook details and DNS names
	rotator := createCertRotator(certDir, certsReadyCh, namespace)
	// Add the rotator to the manager to start certificate management
	return cert.AddRotator(mgr, rotator)
}

// createCertRotator creates and configures a certificate rotator with the provided parameters.
// This function is extracted to make the configuration logic testable.
func createCertRotator(certDir string, certsReadyCh chan struct{}, namespace string) *cert.CertRotator {
	return &cert.CertRotator{
		SecretKey: types.NamespacedName{
			Namespace: namespace,
			Name:      "grove-webhook-server-cert",
		},
		CertDir:        certDir,
		CAName:         certificateAuthorityName,
		CAOrganization: certificateAuthorityOrganization,
		IsReady:        certsReadyCh,
		DNSName:        fmt.Sprintf("%s.%s.svc", serviceName, namespace),
		// Include all possible DNS names for the webhook service
		ExtraDNSNames: []string{
			serviceName,
			fmt.Sprintf("%s.%s", serviceName, namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
		},
		// Register both mutating and validating webhooks
		Webhooks: []cert.WebhookInfo{
			{
				Type: cert.Mutating,
				Name: defaultingwebhook.Name,
			},
			{
				Type: cert.Validating,
				Name: validatingwebhook.Name,
			},
		},
		EnableReadinessCheck:   true,
		RestartOnSecretRefresh: true,
	}
}

// WaitTillWebhookCertsReady blocks on the certsReady channel. Once the cert-controller
// has ensured that the certificates are generated and injected then it will close this channel.
func WaitTillWebhookCertsReady(logger logr.Logger, certsReady chan struct{}) {
	logger.Info("Waiting for certs to be ready and injected into webhook configurations")
	// Block until certificates are ready
	<-certsReady
	logger.Info("Certs are ready and injected into webhook configurations")
}

// getOperatorNamespace reads the operator's namespace from the service account token file.
func getOperatorNamespace() (string, error) {
	return readNamespaceFromFile(operatorNamespaceFile)
}

// readNamespaceFromFile reads and validates a namespace from the specified file path.
// This function is extracted to make testing easier by allowing file path injection.
func readNamespaceFromFile(filePath string) (string, error) {
	// Read namespace from the specified file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	// Clean up the namespace string and validate it's not empty
	namespace := strings.TrimSpace(string(data))
	if len(namespace) == 0 {
		return "", fmt.Errorf("operator namespace is empty")
	}
	return namespace, nil
}
