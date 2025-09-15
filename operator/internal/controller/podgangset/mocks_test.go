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

package podgangset

import (
	"context"
	"net/http"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// mockManager implements a minimal manager interface for testing
type mockManager struct {
	client        client.Client
	eventRecorder record.EventRecorder
}

func (m *mockManager) Add(manager.Runnable) error                                     { return nil }
func (m *mockManager) Elected() <-chan struct{}                                       { return nil }
func (m *mockManager) AddMetricsExtraHandler(path string, handler http.Handler) error { return nil }
func (m *mockManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	return nil
}
func (m *mockManager) AddHealthzCheck(name string, check healthz.Checker) error { return nil }
func (m *mockManager) AddReadyzCheck(name string, check healthz.Checker) error  { return nil }
func (m *mockManager) Start(ctx context.Context) error                          { return nil }
func (m *mockManager) GetConfig() *rest.Config                                  { return nil }
func (m *mockManager) GetScheme() *runtime.Scheme                               { return nil }
func (m *mockManager) GetClient() client.Client                                 { return m.client }
func (m *mockManager) GetFieldIndexer() client.FieldIndexer                     { return nil }
func (m *mockManager) GetCache() cache.Cache                                    { return nil }
func (m *mockManager) GetEventRecorderFor(name string) record.EventRecorder     { return m.eventRecorder }
func (m *mockManager) GetRESTMapper() meta.RESTMapper                           { return nil }
func (m *mockManager) GetAPIReader() client.Reader                              { return nil }
func (m *mockManager) GetWebhookServer() webhook.Server                         { return nil }
func (m *mockManager) GetLogger() logr.Logger                                   { return log.Log }
func (m *mockManager) GetControllerOptions() config.Controller                  { return config.Controller{} }
func (m *mockManager) GetHTTPClient() *http.Client                              { return nil }

// mockReconcileStatusRecorder implements ReconcileStatusRecorder for testing
type mockReconcileStatusRecorder struct {
	mock.Mock
}

func (m *mockReconcileStatusRecorder) RecordStart(ctx context.Context, obj ctrlcommon.ReconciledObject, operationType grovecorev1alpha1.LastOperationType) error {
	args := m.Called(ctx, obj, operationType)
	return args.Error(0)
}

func (m *mockReconcileStatusRecorder) RecordCompletion(ctx context.Context, obj ctrlcommon.ReconciledObject, operationType grovecorev1alpha1.LastOperationType, result *ctrlcommon.ReconcileStepResult) error {
	args := m.Called(ctx, obj, operationType, result)
	return args.Error(0)
}

// mockOperatorRegistry implements OperatorRegistry for testing
type mockOperatorRegistry struct {
	mock.Mock
}

func (m *mockOperatorRegistry) Register(kind component.Kind, operator component.Operator[grovecorev1alpha1.PodGangSet]) {
	m.Called(kind, operator)
}

func (m *mockOperatorRegistry) GetOperator(kind component.Kind) (component.Operator[grovecorev1alpha1.PodGangSet], error) {
	args := m.Called(kind)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(component.Operator[grovecorev1alpha1.PodGangSet]), args.Error(1)
}

func (m *mockOperatorRegistry) GetAllOperators() map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet] {
	args := m.Called()
	return args.Get(0).(map[component.Kind]component.Operator[grovecorev1alpha1.PodGangSet])
}

// mockOperator implements Operator for testing
type mockOperator struct {
	mock.Mock
}

func (m *mockOperator) GetExistingResourceNames(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) ([]string, error) {
	args := m.Called(ctx, logger, objMeta)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockOperator) Sync(ctx context.Context, logger logr.Logger, obj *grovecorev1alpha1.PodGangSet) error {
	args := m.Called(ctx, logger, obj)
	return args.Error(0)
}

func (m *mockOperator) Delete(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) error {
	args := m.Called(ctx, logger, objMeta)
	return args.Error(0)
}

func (m *mockOperator) Exists(ctx context.Context, objMeta metav1.ObjectMeta) (bool, error) {
	args := m.Called(ctx, objMeta)
	return args.Bool(0), args.Error(1)
}
