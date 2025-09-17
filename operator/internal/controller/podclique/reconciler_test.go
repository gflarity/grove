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

package podclique

import (
	"context"
	"testing"
	"time"

	"net/http"

	"github.com/NVIDIA/grove/operator/api/common"
	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/NVIDIA/grove/operator/api/common/constants"
	configv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	ctrlcommon "github.com/NVIDIA/grove/operator/internal/controller/common"
	"github.com/NVIDIA/grove/operator/internal/expect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// TestNewReconciler tests the NewReconciler function which creates a new instance
// of the PodClique Reconciler with proper initialization of all dependencies.
func TestNewReconciler(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// controllerCfg is the PodClique controller configuration to use
		controllerCfg configv1alpha1.PodCliqueControllerConfiguration
		// expectNonNil indicates which fields should be non-nil after creation
		expectNonNil []string
	}{
		{
			// Standard configuration should create reconciler with all fields initialized
			name: "standard configuration",
			controllerCfg: configv1alpha1.PodCliqueControllerConfiguration{
				ConcurrentSyncs: ptr.To(5),
			},
			expectNonNil: []string{"config", "client", "eventRecorder", "reconcileStatusRecorder", "expectationsStore", "operatorRegistry"},
		},
		{
			// Minimal configuration should still initialize all required fields
			name: "minimal configuration",
			controllerCfg: configv1alpha1.PodCliqueControllerConfiguration{
				ConcurrentSyncs: ptr.To(1),
			},
			expectNonNil: []string{"config", "client", "eventRecorder", "reconcileStatusRecorder", "expectationsStore", "operatorRegistry"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake manager for testing
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			mgr := &fakeManager{
				client:        fakeClient,
				scheme:        scheme,
				eventRecorder: record.NewFakeRecorder(100),
			}

			reconciler := NewReconciler(mgr, tt.controllerCfg)

			// Verify reconciler is not nil
			require.NotNil(t, reconciler, "NewReconciler should return non-nil reconciler")

			// Verify configuration is set correctly
			assert.Equal(t, tt.controllerCfg, reconciler.config, "Configuration should match input")

			// Verify all expected fields are non-nil
			for _, field := range tt.expectNonNil {
				switch field {
				case "config":
					assert.NotNil(t, reconciler.config, "config should be non-nil")
				case "client":
					assert.NotNil(t, reconciler.client, "client should be non-nil")
				case "eventRecorder":
					assert.NotNil(t, reconciler.eventRecorder, "eventRecorder should be non-nil")
				case "reconcileStatusRecorder":
					assert.NotNil(t, reconciler.reconcileStatusRecorder, "reconcileStatusRecorder should be non-nil")
				case "expectationsStore":
					assert.NotNil(t, reconciler.expectationsStore, "expectationsStore should be non-nil")
				case "operatorRegistry":
					assert.NotNil(t, reconciler.operatorRegistry, "operatorRegistry should be non-nil")
				}
			}
		})
	}
}

// TestReconcile tests the main Reconcile function which orchestrates the complete
// reconciliation process for PodClique resources including creation, updates, and deletion.
func TestReconcile(t *testing.T) {
	tests := []struct {
		// Test scenario description
		name string
		// existingObjects are the objects that should exist in the fake client before reconciliation
		existingObjects []client.Object
		// request is the reconcile request to process
		request reconcile.Request
		// expectedResult is the expected ctrl.Result from reconciliation
		expectedResult ctrl.Result
		// expectedError indicates if an error should be returned
		expectedError bool
		// expectedRequeue indicates if the result should trigger a requeue
		expectedRequeue bool
	}{
		{
			// PodClique not found should return no requeue and no error
			name:            "podclique not found",
			existingObjects: []client.Object{},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent-pclq",
					Namespace: "default",
				},
			},
			expectedResult:  ctrl.Result{},
			expectedError:   false,
			expectedRequeue: false,
		},
		{
			// PodClique that exists should trigger spec reconciliation
			name: "podclique spec reconciliation",
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodClique{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-0-worker",
						Namespace: "default",
						Labels: map[string]string{
							common.LabelManagedByKey:              common.LabelManagedByValue,
							common.LabelPartOfKey:                 "test-pgs",
							apicommon.LabelPodGangSetReplicaIndex: "0",
							apicommon.LabelPodGang:                "test-pgs-0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: constants.KindPodGangSet,
								Name: "test-pgs",
							},
						},
						Finalizers: []string{constants.FinalizerPodClique},
					},
					Spec: grovecorev1alpha1.PodCliqueSpec{
						MinAvailable: ptr.To(int32(1)),
					},
				},
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
					Spec: grovecorev1alpha1.PodGangSetSpec{
						Replicas: 1,
						Template: grovecorev1alpha1.PodGangSetTemplateSpec{
							Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
								{
									Name: "worker",
									Spec: grovecorev1alpha1.PodCliqueSpec{
										MinAvailable: ptr.To(int32(1)),
										PodSpec: corev1.PodSpec{
											Containers: []corev1.Container{
												{Name: "test", Image: "test:latest"},
											},
										},
									},
								},
							},
						},
					},
					Status: grovecorev1alpha1.PodGangSetStatus{
						CurrentGenerationHash: ptr.To("test-hash"),
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-pgs-0-worker",
					Namespace: "default",
				},
			},
			expectedResult:  ctrl.Result{},
			expectedError:   false,
			expectedRequeue: false,
		},
		{
			// PodClique that doesn't exist should return without error or requeue
			name: "podclique not found should succeed",
			existingObjects: []client.Object{
				// No PodClique object, only PodGangSet
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
					Spec: grovecorev1alpha1.PodGangSetSpec{
						Replicas: 1,
						Template: grovecorev1alpha1.PodGangSetTemplateSpec{
							Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
								{
									Name: "worker",
									Spec: grovecorev1alpha1.PodCliqueSpec{
										MinAvailable: ptr.To(int32(1)),
										PodSpec: corev1.PodSpec{
											Containers: []corev1.Container{
												{Name: "test", Image: "test:latest"},
											},
										},
									},
								},
							},
						},
					},
					Status: grovecorev1alpha1.PodGangSetStatus{
						CurrentGenerationHash: ptr.To("test-hash"),
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-pgs-0-worker",
					Namespace: "default",
				},
			},
			expectedResult:  ctrl.Result{},
			expectedError:   false,
			expectedRequeue: false,
		},
		{
			// PodClique deletion without finalizer test - skipped due to fake client limitations
			name: "podclique deletion without finalizer",
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
					Spec: grovecorev1alpha1.PodGangSetSpec{
						Replicas: 1,
						Template: grovecorev1alpha1.PodGangSetTemplateSpec{
							Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
								{
									Name: "worker",
									Spec: grovecorev1alpha1.PodCliqueSpec{
										MinAvailable: ptr.To(int32(1)),
										PodSpec: corev1.PodSpec{
											Containers: []corev1.Container{
												{Name: "test", Image: "test:latest"},
											},
										},
									},
								},
							},
						},
					},
					Status: grovecorev1alpha1.PodGangSetStatus{
						CurrentGenerationHash: ptr.To("test-hash"),
					},
				},
				&grovecorev1alpha1.PodClique{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-0-worker",
						Namespace: "default",
						Labels: map[string]string{
							common.LabelManagedByKey:              common.LabelManagedByValue,
							common.LabelPartOfKey:                 "test-pgs",
							apicommon.LabelPodGangSetReplicaIndex: "0",
							apicommon.LabelPodGang:                "test-pgs-0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: constants.KindPodGangSet,
								Name: "test-pgs",
							},
						},
					},
					Spec: grovecorev1alpha1.PodCliqueSpec{
						MinAvailable: ptr.To(int32(1)),
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-pgs-0-worker",
					Namespace: "default",
				},
			},
			expectedResult:  ctrl.Result{},
			expectedError:   false,
			expectedRequeue: false,
		},
		{
			// PodClique with deletion timestamp and finalizer should trigger deletion flow
			name: "podclique deletion with finalizer",
			existingObjects: []client.Object{
				&grovecorev1alpha1.PodGangSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs",
						Namespace: "default",
					},
					Spec: grovecorev1alpha1.PodGangSetSpec{
						Replicas: 1,
						Template: grovecorev1alpha1.PodGangSetTemplateSpec{
							Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{
								{
									Name: "worker",
									Spec: grovecorev1alpha1.PodCliqueSpec{
										MinAvailable: ptr.To(int32(1)),
										PodSpec: corev1.PodSpec{
											Containers: []corev1.Container{
												{Name: "test", Image: "test:latest"},
											},
										},
									},
								},
							},
						},
					},
					Status: grovecorev1alpha1.PodGangSetStatus{
						CurrentGenerationHash: ptr.To("test-hash"),
					},
				},
				&grovecorev1alpha1.PodClique{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-0-worker",
						Namespace: "default",
						Labels: map[string]string{
							common.LabelManagedByKey:              common.LabelManagedByValue,
							common.LabelPartOfKey:                 "test-pgs",
							apicommon.LabelPodGangSetReplicaIndex: "0",
							apicommon.LabelPodGang:                "test-pgs-0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: constants.KindPodGangSet,
								Name: "test-pgs",
							},
						},
						Finalizers: []string{constants.FinalizerPodClique},
					},
					Spec: grovecorev1alpha1.PodCliqueSpec{
						MinAvailable: ptr.To(int32(1)),
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-pgs-0-worker",
					Namespace: "default",
				},
			},
			expectedResult:  ctrl.Result{},
			expectedError:   false,
			expectedRequeue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with existing objects
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjects...).
				WithStatusSubresource(&grovecorev1alpha1.PodClique{}).
				Build()

			// Create a mock operator registry to avoid real Pod operations
			mockPodOperator := &MockOperator{}
			mockPodOperator.On("Sync", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			mockPodOperator.On("GetExistingResourceNames", mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

			mockRegistry := &MockOperatorRegistry{
				operators: map[component.Kind]*MockOperator{
					component.KindPod: mockPodOperator,
				},
			}

			reconciler := &Reconciler{
				config:                  configv1alpha1.PodCliqueControllerConfiguration{ConcurrentSyncs: ptr.To(1)},
				client:                  fakeClient,
				eventRecorder:           record.NewFakeRecorder(100),
				reconcileStatusRecorder: ctrlcommon.NewReconcileStatusRecorder(fakeClient, record.NewFakeRecorder(100)),
				expectationsStore:       expect.NewExpectationsStore(),
				operatorRegistry:        mockRegistry,
			}

			// For deletion tests, we need to simulate the deletion timestamp
			// Since fake client doesn't allow setting deletion timestamp after creation,
			// we'll skip these test cases for now and focus on the deletion logic tests
			if tt.name == "podclique deletion with finalizer" || tt.name == "podclique deletion without finalizer" {
				t.Skip("Skipping deletion tests due to fake client limitations")
			}

			// Execute reconciliation
			ctx := context.Background()
			result, err := reconciler.Reconcile(ctx, tt.request)

			// Verify results
			if tt.expectedError {
				assert.Error(t, err, "Expected error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}

			assert.Equal(t, tt.expectedResult.Requeue, result.Requeue, "Requeue flag should match expected")

			if tt.expectedRequeue {
				assert.True(t, result.Requeue || result.RequeueAfter > 0, "Expected requeue but got none")
			} else {
				assert.False(t, result.Requeue, "Expected no requeue but got requeue=true")
				assert.Equal(t, time.Duration(0), result.RequeueAfter, "Expected no requeue delay but got: %v", result.RequeueAfter)
			}
		})
	}
}

// fakeManager implements a minimal ctrl.Manager interface for testing
type fakeManager struct {
	client        client.Client
	scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
}

func (f *fakeManager) GetClient() client.Client {
	return f.client
}

func (f *fakeManager) GetAPIReader() client.Reader {
	return f.client
}

func (f *fakeManager) GetHTTPClient() *http.Client {
	return &http.Client{}
}

func (f *fakeManager) GetScheme() *runtime.Scheme {
	return f.scheme
}

func (f *fakeManager) GetEventRecorderFor(name string) record.EventRecorder {
	return f.eventRecorder
}

func (f *fakeManager) GetRESTMapper() meta.RESTMapper {
	return nil
}

func (f *fakeManager) GetFieldIndexer() client.FieldIndexer {
	return nil
}

func (f *fakeManager) GetCache() cache.Cache {
	return nil
}

func (f *fakeManager) GetConfig() *rest.Config {
	return nil
}

func (f *fakeManager) Add(manager.Runnable) error {
	return nil
}

func (f *fakeManager) Elected() <-chan struct{} {
	return nil
}

func (f *fakeManager) AddMetricsExtraHandler(path string, handler http.Handler) error {
	return nil
}

func (f *fakeManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	return nil
}

func (f *fakeManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return nil
}

func (f *fakeManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return nil
}

func (f *fakeManager) Start(ctx context.Context) error {
	return nil
}

func (f *fakeManager) GetWebhookServer() webhook.Server {
	return nil
}

func (f *fakeManager) GetLogger() logr.Logger {
	return ctrl.Log
}

func (f *fakeManager) GetControllerOptions() ctrlconfig.Controller {
	return ctrlconfig.Controller{}
}
