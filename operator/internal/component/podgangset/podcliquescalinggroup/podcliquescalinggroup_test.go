/*
Copyright 2025 The Grove Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podcliquescalinggroup

import (
	"context"
	"errors"
	"fmt"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"
	"github.com/NVIDIA/grove/operator/internal/component"
	groveevents "github.com/NVIDIA/grove/operator/internal/component/events"
	groveerr "github.com/NVIDIA/grove/operator/internal/errors"

	apicommon "github.com/NVIDIA/grove/operator/api/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// MockClient is a mock implementation of client.Client for testing
type MockClient struct {
	mock.Mock
}

func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj, opts)
	return args.Error(0)
}

func (m *MockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

func (m *MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := m.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

func (m *MockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Status() client.StatusWriter {
	args := m.Called()
	return args.Get(0).(client.StatusWriter)
}

func (m *MockClient) Scheme() *runtime.Scheme {
	args := m.Called()
	return args.Get(0).(*runtime.Scheme)
}

func (m *MockClient) RESTMapper() meta.RESTMapper {
	args := m.Called()
	return args.Get(0).(meta.RESTMapper)
}

func (m *MockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	args := m.Called(obj)
	return args.Get(0).(schema.GroupVersionKind), args.Error(1)
}

func (m *MockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	args := m.Called(obj)
	return args.Bool(0), args.Error(1)
}

func (m *MockClient) SubResource(subResource string) client.SubResourceClient {
	args := m.Called(subResource)
	return args.Get(0).(client.SubResourceClient)
}

// TestNew validates the constructor function for the PodCliqueScalingGroup operator.
// It ensures that the New function correctly initializes the operator with the provided dependencies.
func TestNew(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// client is the Kubernetes client to pass to the constructor
		client client.Client
		// scheme is the runtime scheme to pass to the constructor
		scheme *runtime.Scheme
		// eventRecorder is the event recorder to pass to the constructor
		eventRecorder record.EventRecorder
		// expectNil indicates whether the result should be nil (for error cases)
		expectNil bool
	}{
		{
			// Tests successful creation of operator with valid dependencies.
			// Should return a non-nil operator instance with all fields properly initialized.
			name:          "successful_creation",
			client:        fake.NewClientBuilder().Build(),
			scheme:        runtime.NewScheme(),
			eventRecorder: record.NewFakeRecorder(10),
			expectNil:     false,
		},
		{
			// Tests creation with nil client.
			// Should still create operator as nil client is handled by the implementation.
			name:          "nil_client",
			client:        nil,
			scheme:        runtime.NewScheme(),
			eventRecorder: record.NewFakeRecorder(10),
			expectNil:     false,
		},
		{
			// Tests creation with nil scheme.
			// Should still create operator as nil scheme is handled by the implementation.
			name:          "nil_scheme",
			client:        fake.NewClientBuilder().Build(),
			scheme:        nil,
			eventRecorder: record.NewFakeRecorder(10),
			expectNil:     false,
		},
		{
			// Tests creation with nil event recorder.
			// Should still create operator as nil event recorder is handled by the implementation.
			name:          "nil_event_recorder",
			client:        fake.NewClientBuilder().Build(),
			scheme:        runtime.NewScheme(),
			eventRecorder: nil,
			expectNil:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operator := New(tt.client, tt.scheme, tt.eventRecorder)

			if tt.expectNil {
				assert.Nil(t, operator)
			} else {
				assert.NotNil(t, operator)

				// Verify the operator implements the expected interface
				assert.Implements(t, (*component.Operator[grovecorev1alpha1.PodGangSet])(nil), operator, "Operator should implement component.Operator[PodGangSet] interface")
			}
		})
	}
}

// TestGetExistingResourceNames validates the GetExistingResourceNames method.
// It tests various scenarios including successful retrieval, empty results, and error conditions.
func TestGetExistingResourceNames(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsObjMeta is the PodGangSet ObjectMeta used for the test
		pgsObjMeta metav1.ObjectMeta
		// setupMock configures the mock client expectations for this test case
		setupMock func(*MockClient)
		// expectedNames are the resource names expected to be returned
		expectedNames []string
		// expectError indicates whether an error is expected
		expectError bool
		// errorContains is a substring that should be present in the error message
		errorContains string
	}{
		{
			// Tests successful retrieval of existing PodCliqueScalingGroup resources.
			// Should return the names of all PodCliqueScalingGroup resources owned by the PodGangSet.
			name: "successful_retrieval",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			setupMock: func(mockClient *MockClient) {
				objMetaList := &metav1.PartialObjectMetadataList{}
				objMetaList.Items = []metav1.PartialObjectMetadata{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pgs-0-sg1",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "core.grove.io/v1alpha1",
									Kind:       "PodGangSet",
									Name:       "test-pgs",
									UID:        "test-uid",
									Controller: ptr.To(true),
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pgs-1-sg1",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "core.grove.io/v1alpha1",
									Kind:       "PodGangSet",
									Name:       "test-pgs",
									UID:        "test-uid",
									Controller: ptr.To(true),
								},
							},
						},
					},
				}
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Run(func(args mock.Arguments) {
						list := args.Get(1).(*metav1.PartialObjectMetadataList)
						*list = *objMetaList
					}).Return(nil)
			},
			expectedNames: []string{"test-pgs-0-sg1", "test-pgs-1-sg1"},
			expectError:   false,
		},
		{
			// Tests scenario where no PodCliqueScalingGroup resources exist.
			// Should return an empty slice without error.
			name: "no_existing_resources",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			setupMock: func(mockClient *MockClient) {
				objMetaList := &metav1.PartialObjectMetadataList{}
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Run(func(args mock.Arguments) {
						list := args.Get(1).(*metav1.PartialObjectMetadataList)
						*list = *objMetaList
					}).Return(nil)
			},
			expectedNames: []string{},
			expectError:   false,
		},
		{
			// Tests error handling when the List operation fails.
			// Should return an error with appropriate error code and message.
			name: "list_error",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			setupMock: func(mockClient *MockClient) {
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Return(errors.New("list operation failed"))
			},
			expectedNames: nil,
			expectError:   true,
			errorContains: "Error listing PodCliqueScalingGroup",
		},
		{
			// Tests filtering of resources that are not owned by the PodGangSet.
			// Should only return resources that have the PodGangSet as controller owner.
			name: "filter_non_owned_resources",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			setupMock: func(mockClient *MockClient) {
				objMetaList := &metav1.PartialObjectMetadataList{}
				objMetaList.Items = []metav1.PartialObjectMetadata{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pgs-0-sg1",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "core.grove.io/v1alpha1",
									Kind:       "PodGangSet",
									Name:       "test-pgs",
									UID:        "test-uid",
									Controller: ptr.To(true),
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "other-pgs-0-sg1",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "core.grove.io/v1alpha1",
									Kind:       "PodGangSet",
									Name:       "other-pgs",
									UID:        "other-uid",
									Controller: ptr.To(true),
								},
							},
						},
					},
				}
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Run(func(args mock.Arguments) {
						list := args.Get(1).(*metav1.PartialObjectMetadataList)
						*list = *objMetaList
					}).Return(nil)
			},
			expectedNames: []string{"test-pgs-0-sg1"},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{}
			tt.setupMock(mockClient)

			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			operator := &_resource{
				client:        mockClient,
				scheme:        scheme,
				eventRecorder: record.NewFakeRecorder(10),
			}

			names, err := operator.GetExistingResourceNames(ctx, logger, tt.pgsObjMeta)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				// Verify error is properly wrapped with Grove error
				var groveErr *groveerr.GroveError
				assert.True(t, errors.As(err, &groveErr))
				assert.Equal(t, errListPodCliqueScalingGroup, groveErr.Code)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedNames, names)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

// TestSync validates the Sync method which orchestrates the creation, update, and deletion
// of PodCliqueScalingGroup resources based on the PodGangSet specification.
func TestSync(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgs is the PodGangSet resource used for the test
		pgs *grovecorev1alpha1.PodGangSet
		// existingResources are the PodCliqueScalingGroup resources that already exist
		existingResources []string
		// setupMock configures the mock client expectations for this test case
		setupMock func(*MockClient)
		// expectError indicates whether an error is expected
		expectError bool
		// errorContains is a substring that should be present in the error message
		errorContains string
	}{
		{
			// Tests successful sync when creating new PodCliqueScalingGroup resources.
			// Should create all required resources based on PodGangSet spec without error.
			name: "successful_sync_create_resources",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 2,
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:         "sg1",
								Replicas:     ptr.To(int32(3)),
								MinAvailable: ptr.To(int32(2)),
								CliqueNames:  []string{"clique1", "clique2"},
							},
						},
					},
				},
			},
			existingResources: []string{},
			setupMock: func(mockClient *MockClient) {
				// Mock GetExistingResourceNames call
				objMetaList := &metav1.PartialObjectMetadataList{}
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Run(func(args mock.Arguments) {
						list := args.Get(1).(*metav1.PartialObjectMetadataList)
						*list = *objMetaList
					}).Return(nil)

				// Mock Get calls for CreateOrPatch (resources don't exist)
				notFoundErr := apierrors.NewNotFound(
					schema.GroupResource{Group: "core.grove.io", Resource: "podcliquescalinggroups"},
					"test-resource",
				)
				mockClient.On("Get", ctx, mock.Anything, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(notFoundErr).Times(2)

				// Mock Create calls for new resources
				mockClient.On("Create", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(nil).Times(2) // 2 replicas * 1 scaling group config
			},
			expectError: false,
		},
		{
			// Tests successful sync when some resources already exist.
			// Should only create missing resources and skip existing ones.
			name: "successful_sync_skip_existing_resources",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 2,
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:         "sg1",
								Replicas:     ptr.To(int32(3)),
								MinAvailable: ptr.To(int32(2)),
								CliqueNames:  []string{"clique1", "clique2"},
							},
						},
					},
				},
			},
			existingResources: []string{"test-pgs-0-sg1"}, // One resource already exists
			setupMock: func(mockClient *MockClient) {
				// Mock GetExistingResourceNames call
				objMetaList := &metav1.PartialObjectMetadataList{}
				objMetaList.Items = []metav1.PartialObjectMetadata{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pgs-0-sg1",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "core.grove.io/v1alpha1",
									Kind:       "PodGangSet",
									Name:       "test-pgs",
									UID:        "test-uid",
									Controller: ptr.To(true),
								},
							},
						},
					},
				}
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Run(func(args mock.Arguments) {
						list := args.Get(1).(*metav1.PartialObjectMetadataList)
						*list = *objMetaList
					}).Return(nil)

				// Mock Get calls for CreateOrPatch - first one exists, second doesn't
				existingResource := &grovecorev1alpha1.PodCliqueScalingGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-0-sg1",
						Namespace: "default",
					},
				}
				mockClient.On("Get", ctx, client.ObjectKey{Name: "test-pgs-0-sg1", Namespace: "default"}, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*grovecorev1alpha1.PodCliqueScalingGroup)
						*obj = *existingResource
					}).Return(nil).Once()

				notFoundErr := apierrors.NewNotFound(
					schema.GroupResource{Group: "core.grove.io", Resource: "podcliquescalinggroups"},
					"test-pgs-1-sg1",
				)
				mockClient.On("Get", ctx, client.ObjectKey{Name: "test-pgs-1-sg1", Namespace: "default"}, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(notFoundErr).Once()

				// Mock Patch call for existing resource (CreateOrPatch uses Patch, not Update)
				mockClient.On("Patch", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything, mock.Anything).
					Return(nil).Once()

				// Mock Create call for only the missing resource
				mockClient.On("Create", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(nil).Once() // Only one resource needs to be created
			},
			expectError: false,
		},
		{
			// Tests successful sync when deleting excess resources.
			// Should delete resources that are no longer needed based on current spec.
			name: "successful_sync_delete_excess_resources",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1, // Reduced from 2 replicas
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:         "sg1",
								Replicas:     ptr.To(int32(3)),
								MinAvailable: ptr.To(int32(2)),
								CliqueNames:  []string{"clique1", "clique2"},
							},
						},
					},
				},
			},
			existingResources: []string{"test-pgs-0-sg1", "test-pgs-1-sg1"}, // Extra resource exists
			setupMock: func(mockClient *MockClient) {
				// Mock GetExistingResourceNames call
				objMetaList := &metav1.PartialObjectMetadataList{}
				objMetaList.Items = []metav1.PartialObjectMetadata{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pgs-0-sg1",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "core.grove.io/v1alpha1",
									Kind:       "PodGangSet",
									Name:       "test-pgs",
									UID:        "test-uid",
									Controller: ptr.To(true),
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pgs-1-sg1",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "core.grove.io/v1alpha1",
									Kind:       "PodGangSet",
									Name:       "test-pgs",
									UID:        "test-uid",
									Controller: ptr.To(true),
								},
							},
						},
					},
				}
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Run(func(args mock.Arguments) {
						list := args.Get(1).(*metav1.PartialObjectMetadataList)
						*list = *objMetaList
					}).Return(nil)

				// Mock Get call for CreateOrPatch (for the remaining resource)
				existingResource := &grovecorev1alpha1.PodCliqueScalingGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-0-sg1",
						Namespace: "default",
					},
				}
				mockClient.On("Get", ctx, client.ObjectKey{Name: "test-pgs-0-sg1", Namespace: "default"}, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*grovecorev1alpha1.PodCliqueScalingGroup)
						*obj = *existingResource
					}).Return(nil).Once()

				// Mock Patch call for existing resource (CreateOrPatch uses Patch, not Update)
				mockClient.On("Patch", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything, mock.Anything).
					Return(nil).Once()

				// Mock Delete call for the excess resource
				mockClient.On("Delete", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(nil).Once()
			},
			expectError: false,
		},
		{
			// Tests error handling when GetExistingResourceNames fails.
			// Should return an error without attempting any create/delete operations.
			name: "error_getting_existing_resources",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:         "sg1",
								Replicas:     ptr.To(int32(3)),
								MinAvailable: ptr.To(int32(2)),
								CliqueNames:  []string{"clique1"},
							},
						},
					},
				},
			},
			existingResources: nil,
			setupMock: func(mockClient *MockClient) {
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Return(errors.New("list operation failed"))
			},
			expectError:   true,
			errorContains: "Error listing PodCliqueScalingGroup",
		},
		{
			// Tests error handling when resource creation fails.
			// Should return an error with appropriate error code and message.
			name: "error_creating_resource",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
					Template: grovecorev1alpha1.PodGangSetTemplateSpec{
						PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
							{
								Name:         "sg1",
								Replicas:     ptr.To(int32(3)),
								MinAvailable: ptr.To(int32(2)),
								CliqueNames:  []string{"clique1"},
							},
						},
					},
				},
			},
			existingResources: []string{},
			setupMock: func(mockClient *MockClient) {
				// Mock GetExistingResourceNames call
				objMetaList := &metav1.PartialObjectMetadataList{}
				mockClient.On("List", ctx, mock.AnythingOfType("*v1.PartialObjectMetadataList"), mock.Anything).
					Run(func(args mock.Arguments) {
						list := args.Get(1).(*metav1.PartialObjectMetadataList)
						*list = *objMetaList
					}).Return(nil)

				// Mock Get call for CreateOrPatch (resource doesn't exist)
				notFoundErr := apierrors.NewNotFound(
					schema.GroupResource{Group: "core.grove.io", Resource: "podcliquescalinggroups"},
					"test-resource",
				)
				mockClient.On("Get", ctx, mock.Anything, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(notFoundErr)

				// Mock Create call that fails
				mockClient.On("Create", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(errors.New("create operation failed"))
			},
			expectError:   true,
			errorContains: "Error creating or updating PodCliqueScalingGroup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{}
			tt.setupMock(mockClient)

			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			eventRecorder := record.NewFakeRecorder(100)
			operator := &_resource{
				client:        mockClient,
				scheme:        scheme,
				eventRecorder: eventRecorder,
			}

			err := operator.Sync(ctx, logger, tt.pgs)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				// Verify error is properly wrapped with Grove error
				var groveErr *groveerr.GroveError
				assert.True(t, errors.As(err, &groveErr))
			} else {
				assert.NoError(t, err)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

// TestDelete validates the Delete method which removes all PodCliqueScalingGroup resources
// managed by a specific PodGangSet.
func TestDelete(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsObjMeta is the PodGangSet ObjectMeta used for the test
		pgsObjMeta metav1.ObjectMeta
		// setupMock configures the mock client expectations for this test case
		setupMock func(*MockClient)
		// expectError indicates whether an error is expected
		expectError bool
		// errorContains is a substring that should be present in the error message
		errorContains string
	}{
		{
			// Tests successful deletion of all PodCliqueScalingGroup resources.
			// Should call DeleteAllOf with appropriate namespace and label selectors.
			name: "successful_delete",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			setupMock: func(mockClient *MockClient) {
				mockClient.On("DeleteAllOf", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(nil)
			},
			expectError: false,
		},
		{
			// Tests error handling when DeleteAllOf operation fails.
			// Should return an error with appropriate error code and message.
			name: "delete_error",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			setupMock: func(mockClient *MockClient) {
				mockClient.On("DeleteAllOf", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(errors.New("delete operation failed"))
			},
			expectError:   true,
			errorContains: "Error deleting PodCliqueScalingGroup",
		},
		{
			// Tests deletion with different namespace.
			// Should work correctly regardless of namespace.
			name: "delete_different_namespace",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			setupMock: func(mockClient *MockClient) {
				mockClient.On("DeleteAllOf", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(nil)
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{}
			tt.setupMock(mockClient)

			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			operator := &_resource{
				client:        mockClient,
				scheme:        scheme,
				eventRecorder: record.NewFakeRecorder(10),
			}

			err := operator.Delete(ctx, logger, tt.pgsObjMeta)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				// Verify error is properly wrapped with Grove error
				var groveErr *groveerr.GroveError
				assert.True(t, errors.As(err, &groveErr))
				assert.Equal(t, errDeletePodCliqueScalingGroup, groveErr.Code)
			} else {
				assert.NoError(t, err)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

// TestDoCreateOrUpdate validates the doCreateOrUpdate method which handles the creation/update of individual
// PodCliqueScalingGroup resources.
func TestDoCreateOrUpdate(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgs is the PodGangSet resource used for the test
		pgs *grovecorev1alpha1.PodGangSet
		// pgsReplica is the replica index for the PodGangSet
		pgsReplica int
		// pcsgObjectKey is the object key for the PodCliqueScalingGroup to create
		pcsgObjectKey client.ObjectKey
		// pcsgConfig is the configuration for the PodCliqueScalingGroup
		pcsgConfig grovecorev1alpha1.PodCliqueScalingGroupConfig
		// setupMock configures the mock client expectations for this test case
		setupMock func(*MockClient)
		// expectError indicates whether an error is expected
		expectError bool
		// errorContains is a substring that should be present in the error message
		errorContains string
	}{
		{
			// Tests successful creation of a PodCliqueScalingGroup resource.
			// Should build the resource correctly and create it in the cluster.
			name: "successful_create_or_update",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 2,
				},
			},
			pgsReplica: 0,
			pcsgObjectKey: client.ObjectKey{
				Name:      "test-pgs-0-sg1",
				Namespace: "default",
			},
			pcsgConfig: grovecorev1alpha1.PodCliqueScalingGroupConfig{
				Name:         "sg1",
				Replicas:     ptr.To(int32(3)),
				MinAvailable: ptr.To(int32(2)),
				CliqueNames:  []string{"clique1", "clique2"},
			},
			setupMock: func(mockClient *MockClient) {
				// Mock Get call for CreateOrPatch (resource doesn't exist)
				notFoundErr := apierrors.NewNotFound(
					schema.GroupResource{Group: "core.grove.io", Resource: "podcliquescalinggroups"},
					"test-pgs-0-sg1",
				)
				mockClient.On("Get", ctx, mock.Anything, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(notFoundErr)

				mockClient.On("Create", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(nil)
			},
			expectError: false,
		},
		{
			// Tests handling of existing resource during CreateOrPatch.
			// Should update the existing resource successfully.
			name: "update_existing_resource",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
				},
			},
			pgsReplica: 0,
			pcsgObjectKey: client.ObjectKey{
				Name:      "test-pgs-0-sg1",
				Namespace: "default",
			},
			pcsgConfig: grovecorev1alpha1.PodCliqueScalingGroupConfig{
				Name:         "sg1",
				Replicas:     ptr.To(int32(2)),
				MinAvailable: ptr.To(int32(1)),
				CliqueNames:  []string{"clique1"},
			},
			setupMock: func(mockClient *MockClient) {
				// Mock Get call for CreateOrPatch (resource exists)
				existingResource := &grovecorev1alpha1.PodCliqueScalingGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pgs-0-sg1",
						Namespace: "default",
					},
				}
				mockClient.On("Get", ctx, mock.Anything, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*grovecorev1alpha1.PodCliqueScalingGroup)
						*obj = *existingResource
					}).Return(nil)

				mockClient.On("Patch", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything, mock.Anything).
					Return(nil)
			},
			expectError: false,
		},
		{
			// Tests error handling when create or update fails.
			// Should return an error with appropriate error code and generate warning event.
			name: "create_or_update_error",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
				},
			},
			pgsReplica: 0,
			pcsgObjectKey: client.ObjectKey{
				Name:      "test-pgs-0-sg1",
				Namespace: "default",
			},
			pcsgConfig: grovecorev1alpha1.PodCliqueScalingGroupConfig{
				Name:         "sg1",
				Replicas:     ptr.To(int32(2)),
				MinAvailable: ptr.To(int32(1)),
				CliqueNames:  []string{"clique1"},
			},
			setupMock: func(mockClient *MockClient) {
				// Mock Get call for CreateOrPatch (resource doesn't exist)
				notFoundErr := apierrors.NewNotFound(
					schema.GroupResource{Group: "core.grove.io", Resource: "podcliquescalinggroups"},
					"test-pgs-0-sg1",
				)
				mockClient.On("Get", ctx, mock.Anything, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(notFoundErr)

				mockClient.On("Create", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(errors.New("create operation failed"))
			},
			expectError:   true,
			errorContains: "create operation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{}
			tt.setupMock(mockClient)

			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			eventRecorder := record.NewFakeRecorder(100)
			operator := &_resource{
				client:        mockClient,
				scheme:        scheme,
				eventRecorder: eventRecorder,
			}

			err := operator.doCreateOrUpdate(ctx, logger, tt.pgs, tt.pgsReplica, tt.pcsgObjectKey, tt.pcsgConfig, false)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}

				// Check that warning event was recorded
				select {
				case event := <-eventRecorder.Events:
					assert.Contains(t, event, groveevents.ReasonPodCliqueScalingGroupCreateOrUpdateFailed)
					assert.Contains(t, event, "Warning")
				default:
					t.Error("Expected warning event to be recorded")
				}
			} else {
				assert.NoError(t, err)

				// Check that success event was recorded
				select {
				case event := <-eventRecorder.Events:
					assert.Contains(t, event, groveevents.ReasonPodCliqueScalingGroupCreateSuccessful)
					assert.Contains(t, event, "Normal")
				default:
					t.Error("Expected success event to be recorded")
				}
			}

			mockClient.AssertExpectations(t)
		})
	}
}

// TestDoDelete validates the doDelete method which handles the deletion of individual
// PodCliqueScalingGroup resources.
func TestDoDelete(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgs is the PodGangSet resource used for the test
		pgs *grovecorev1alpha1.PodGangSet
		// pcsgObjectKey is the object key for the PodCliqueScalingGroup to delete
		pcsgObjectKey client.ObjectKey
		// setupMock configures the mock client expectations for this test case
		setupMock func(*MockClient)
		// expectError indicates whether an error is expected
		expectError bool
		// errorContains is a substring that should be present in the error message
		errorContains string
	}{
		{
			// Tests successful deletion of a PodCliqueScalingGroup resource.
			// Should delete the resource and generate success event.
			name: "successful_delete",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
			},
			pcsgObjectKey: client.ObjectKey{
				Name:      "test-pgs-0-sg1",
				Namespace: "default",
			},
			setupMock: func(mockClient *MockClient) {
				mockClient.On("Delete", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(nil)
			},
			expectError: false,
		},
		{
			// Tests error handling when deletion fails.
			// Should return an error with appropriate error code and generate warning event.
			name: "delete_error",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
			},
			pcsgObjectKey: client.ObjectKey{
				Name:      "test-pgs-0-sg1",
				Namespace: "default",
			},
			setupMock: func(mockClient *MockClient) {
				mockClient.On("Delete", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(errors.New("delete operation failed"))
			},
			expectError:   true,
			errorContains: "Error in delete of PodCliqueScalingGroup",
		},
		{
			// Tests deletion when resource doesn't exist (NotFound error).
			// Should handle gracefully and not return error.
			name: "delete_not_found",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
			},
			pcsgObjectKey: client.ObjectKey{
				Name:      "test-pgs-0-sg1",
				Namespace: "default",
			},
			setupMock: func(mockClient *MockClient) {
				notFoundErr := apierrors.NewNotFound(
					schema.GroupResource{Group: "core.grove.io", Resource: "podcliquescalinggroups"},
					"test-pgs-0-sg1",
				)
				mockClient.On("Delete", ctx, mock.AnythingOfType("*v1alpha1.PodCliqueScalingGroup"), mock.Anything).
					Return(notFoundErr)
			},
			expectError:   true, // NotFound errors are still returned as errors
			errorContains: "Error in delete of PodCliqueScalingGroup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{}
			tt.setupMock(mockClient)

			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			eventRecorder := record.NewFakeRecorder(100)
			operator := &_resource{
				client:        mockClient,
				scheme:        scheme,
				eventRecorder: eventRecorder,
			}

			err := operator.doDelete(ctx, logger, tt.pgs, tt.pcsgObjectKey)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				// Verify error is properly wrapped with Grove error
				var groveErr *groveerr.GroveError
				assert.True(t, errors.As(err, &groveErr))
				assert.Equal(t, errDeletePodCliqueScalingGroup, groveErr.Code)

				// Check that warning event was recorded
				select {
				case event := <-eventRecorder.Events:
					assert.Contains(t, event, groveevents.ReasonPodCliqueScalingGroupDeleteFailed)
					assert.Contains(t, event, "Warning")
				default:
					t.Error("Expected warning event to be recorded")
				}
			} else {
				assert.NoError(t, err)

				// Check that success event was recorded
				select {
				case event := <-eventRecorder.Events:
					assert.Contains(t, event, groveevents.ReasonPodCliqueScalingGroupDeleteSuccessful)
					assert.Contains(t, event, "Normal")
				default:
					t.Error("Expected success event to be recorded")
				}
			}

			mockClient.AssertExpectations(t)
		})
	}
}

// TestBuildResource validates the buildResource method which populates a PodCliqueScalingGroup
// resource with the appropriate specification and metadata.
func TestBuildResource(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgs is the PodGangSet resource used for the test
		pgs *grovecorev1alpha1.PodGangSet
		// pgsReplica is the replica index for the PodGangSet
		pgsReplica int
		// pcsgConfig is the configuration for the PodCliqueScalingGroup
		pcsgConfig grovecorev1alpha1.PodCliqueScalingGroupConfig
		// expectError indicates whether an error is expected
		expectError bool
		// validateFunc performs additional validation on the built resource
		validateFunc func(*testing.T, *grovecorev1alpha1.PodCliqueScalingGroup)
	}{
		{
			// Tests successful building of PodCliqueScalingGroup resource.
			// Should populate all fields correctly from the configuration.
			name: "successful_build",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 2,
				},
			},
			pgsReplica: 0,
			pcsgConfig: grovecorev1alpha1.PodCliqueScalingGroupConfig{
				Name:         "sg1",
				Replicas:     ptr.To(int32(3)),
				MinAvailable: ptr.To(int32(2)),
				CliqueNames:  []string{"clique1", "clique2"},
			},
			expectError: false,
			validateFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				// Verify spec fields are set correctly
				assert.Equal(t, int32(3), pcsg.Spec.Replicas)
				assert.Equal(t, ptr.To(int32(2)), pcsg.Spec.MinAvailable)
				assert.Equal(t, []string{"clique1", "clique2"}, pcsg.Spec.CliqueNames)

				// Verify owner reference is set
				assert.Len(t, pcsg.OwnerReferences, 1)
				ownerRef := pcsg.OwnerReferences[0]
				assert.Equal(t, "PodGangSet", ownerRef.Kind)
				assert.Equal(t, "test-pgs", ownerRef.Name)
				assert.Equal(t, "test-uid", string(ownerRef.UID))
				assert.True(t, *ownerRef.Controller)

				// Verify labels are set correctly
				assert.NotEmpty(t, pcsg.Labels)
				assert.Equal(t, apicommon.LabelComponentNamePodCliqueScalingGroup, pcsg.Labels[apicommon.LabelComponentKey])
				assert.Equal(t, "0", pcsg.Labels[apicommon.LabelPodGangSetReplicaIndex])
			},
		},
		{
			// Tests building with different replica index.
			// Should set the replica index label correctly.
			name: "different_replica_index",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 3,
				},
			},
			pgsReplica: 2,
			pcsgConfig: grovecorev1alpha1.PodCliqueScalingGroupConfig{
				Name:         "sg1",
				Replicas:     ptr.To(int32(1)),
				MinAvailable: ptr.To(int32(1)),
				CliqueNames:  []string{"clique1"},
			},
			expectError: false,
			validateFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				assert.Equal(t, "2", pcsg.Labels[apicommon.LabelPodGangSetReplicaIndex])
			},
		},
		{
			// Tests building with minimal configuration.
			// Should handle nil MinAvailable correctly.
			name: "minimal_config",
			pgs: &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pgs",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: grovecorev1alpha1.PodGangSetSpec{
					Replicas: 1,
				},
			},
			pgsReplica: 0,
			pcsgConfig: grovecorev1alpha1.PodCliqueScalingGroupConfig{
				Name:         "sg1",
				Replicas:     ptr.To(int32(1)),
				MinAvailable: nil, // Test nil MinAvailable
				CliqueNames:  []string{"clique1"},
			},
			expectError: false,
			validateFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				assert.Equal(t, int32(1), pcsg.Spec.Replicas)
				assert.Nil(t, pcsg.Spec.MinAvailable)
				assert.Equal(t, []string{"clique1"}, pcsg.Spec.CliqueNames)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, grovecorev1alpha1.AddToScheme(scheme))

			operator := &_resource{
				client:        fake.NewClientBuilder().Build(),
				scheme:        scheme,
				eventRecorder: record.NewFakeRecorder(10),
			}

			pcsg := &grovecorev1alpha1.PodCliqueScalingGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d-%s", tt.pgs.Name, tt.pgsReplica, tt.pcsgConfig.Name),
					Namespace: tt.pgs.Namespace,
				},
			}

			err := operator.buildResource(pcsg, tt.pgs, tt.pgsReplica, tt.pcsgConfig, false)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.validateFunc != nil {
					tt.validateFunc(t, pcsg)
				}
			}
		})
	}
}

// TestGetLabels validates the getLabels function which generates appropriate labels
// for PodCliqueScalingGroup resources.
func TestGetLabels(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsName is the name of the PodGangSet
		pgsName string
		// pgsReplica is the replica index for the PodGangSet
		pgsReplica int
		// pclqScalingGroupObjKey is the object key for the PodCliqueScalingGroup
		pclqScalingGroupObjKey client.ObjectKey
		// expectedLabels are the labels expected to be generated
		expectedLabels map[string]string
	}{
		{
			// Tests label generation for a typical PodCliqueScalingGroup.
			// Should include all required labels with correct values.
			name:       "typical_labels",
			pgsName:    "test-pgs",
			pgsReplica: 0,
			pclqScalingGroupObjKey: client.ObjectKey{
				Name:      "test-pgs-0-sg1",
				Namespace: "default",
			},
			expectedLabels: map[string]string{
				apicommon.LabelAppNameKey:             "test-pgs-0-sg1",
				apicommon.LabelComponentKey:           apicommon.LabelComponentNamePodCliqueScalingGroup,
				apicommon.LabelPodGangSetReplicaIndex: "0",
				apicommon.LabelPartOfKey:              "test-pgs",
				apicommon.LabelManagedByKey:           apicommon.LabelManagedByValue,
			},
		},
		{
			// Tests label generation with different replica index.
			// Should set the replica index label correctly.
			name:       "different_replica",
			pgsName:    "my-pgs",
			pgsReplica: 5,
			pclqScalingGroupObjKey: client.ObjectKey{
				Name:      "my-pgs-5-scaling-group",
				Namespace: "test-ns",
			},
			expectedLabels: map[string]string{
				apicommon.LabelAppNameKey:             "my-pgs-5-scaling-group",
				apicommon.LabelComponentKey:           apicommon.LabelComponentNamePodCliqueScalingGroup,
				apicommon.LabelPodGangSetReplicaIndex: "5",
				apicommon.LabelPartOfKey:              "my-pgs",
				apicommon.LabelManagedByKey:           apicommon.LabelManagedByValue,
			},
		},
		{
			// Tests label generation with long names.
			// Should handle long names correctly without truncation.
			name:       "long_names",
			pgsName:    "very-long-podgangset-name-for-testing",
			pgsReplica: 10,
			pclqScalingGroupObjKey: client.ObjectKey{
				Name:      "very-long-podgangset-name-for-testing-10-very-long-scaling-group-name",
				Namespace: "very-long-namespace-name",
			},
			expectedLabels: map[string]string{
				apicommon.LabelAppNameKey:             "very-long-podgangset-name-for-testing-10-very-long-scaling-group-name",
				apicommon.LabelComponentKey:           apicommon.LabelComponentNamePodCliqueScalingGroup,
				apicommon.LabelPodGangSetReplicaIndex: "10",
				apicommon.LabelPartOfKey:              "very-long-podgangset-name-for-testing",
				apicommon.LabelManagedByKey:           apicommon.LabelManagedByValue,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgs := &grovecorev1alpha1.PodGangSet{
				ObjectMeta: metav1.ObjectMeta{Name: tt.pgsName},
			}
			labels := getLabels(pgs, tt.pgsReplica, tt.pclqScalingGroupObjKey)

			// Verify all expected labels are present
			for key, expectedValue := range tt.expectedLabels {
				actualValue, exists := labels[key]
				assert.True(t, exists, "Label %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "Label %s should have correct value", key)
			}

			// Verify no unexpected labels are present
			for key := range labels {
				_, expected := tt.expectedLabels[key]
				assert.True(t, expected, "Unexpected label %s found", key)
			}
		})
	}
}

// TestGetPodCliqueScalingGroupSelectorLabels validates the getPodCliqueScalingGroupSelectorLabels
// function which generates label selectors for identifying PodCliqueScalingGroup resources.
func TestGetPodCliqueScalingGroupSelectorLabels(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// pgsObjMeta is the PodGangSet ObjectMeta used for the test
		pgsObjMeta metav1.ObjectMeta
		// expectedLabels are the selector labels expected to be generated
		expectedLabels map[string]string
	}{
		{
			// Tests selector label generation for a typical PodGangSet.
			// Should include PodGangSet name and component labels.
			name: "typical_selector",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "test-pgs",
				Namespace: "default",
				UID:       "test-uid",
			},
			expectedLabels: map[string]string{
				apicommon.LabelPartOfKey:    "test-pgs",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodCliqueScalingGroup,
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
			},
		},
		{
			// Tests selector label generation with different PodGangSet name.
			// Should use the correct PodGangSet name in the selector.
			name: "different_pgs_name",
			pgsObjMeta: metav1.ObjectMeta{
				Name:      "my-podgangset",
				Namespace: "test-namespace",
				UID:       "different-uid",
			},
			expectedLabels: map[string]string{
				apicommon.LabelPartOfKey:    "my-podgangset",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodCliqueScalingGroup,
				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
			},
		},
		{
			// Tests selector label generation with minimal ObjectMeta.
			// Should work correctly even with minimal metadata.
			name: "minimal_metadata",
			pgsObjMeta: metav1.ObjectMeta{
				Name: "minimal-pgs",
			},
			expectedLabels: map[string]string{
				apicommon.LabelPartOfKey:    "minimal-pgs",
				apicommon.LabelComponentKey: apicommon.LabelComponentNamePodCliqueScalingGroup,

				apicommon.LabelManagedByKey: apicommon.LabelManagedByValue,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := getPodCliqueScalingGroupSelectorLabels(tt.pgsObjMeta)

			// Verify all expected labels are present
			for key, expectedValue := range tt.expectedLabels {
				actualValue, exists := labels[key]
				assert.True(t, exists, "Selector label %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "Selector label %s should have correct value", key)
			}

			// Verify no unexpected labels are present
			for key := range labels {
				_, expected := tt.expectedLabels[key]
				assert.True(t, expected, "Unexpected selector label %s found", key)
			}
		})
	}
}

// TestEmptyPodCliqueScalingGroup validates the emptyPodCliqueScalingGroup function
// which creates a new PodCliqueScalingGroup with basic ObjectMeta.
func TestEmptyPodCliqueScalingGroup(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario being validated
		name string
		// objKey is the object key used to create the empty resource
		objKey client.ObjectKey
		// validateFunc performs additional validation on the created resource
		validateFunc func(*testing.T, *grovecorev1alpha1.PodCliqueScalingGroup)
	}{
		{
			// Tests creation of empty PodCliqueScalingGroup with typical object key.
			// Should create resource with correct name and namespace.
			name: "typical_empty_resource",
			objKey: client.ObjectKey{
				Name:      "test-pgs-0-sg1",
				Namespace: "default",
			},
			validateFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				assert.Equal(t, "test-pgs-0-sg1", pcsg.Name)
				assert.Equal(t, "default", pcsg.Namespace)
				assert.Empty(t, pcsg.Spec.CliqueNames)
				assert.Equal(t, int32(0), pcsg.Spec.Replicas)
				assert.Nil(t, pcsg.Spec.MinAvailable)
			},
		},
		{
			// Tests creation with different namespace.
			// Should set the namespace correctly.
			name: "different_namespace",
			objKey: client.ObjectKey{
				Name:      "my-resource",
				Namespace: "test-namespace",
			},
			validateFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				assert.Equal(t, "my-resource", pcsg.Name)
				assert.Equal(t, "test-namespace", pcsg.Namespace)
			},
		},
		{
			// Tests creation with empty namespace (cluster-scoped).
			// Should handle empty namespace correctly.
			name: "empty_namespace",
			objKey: client.ObjectKey{
				Name:      "cluster-resource",
				Namespace: "",
			},
			validateFunc: func(t *testing.T, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
				assert.Equal(t, "cluster-resource", pcsg.Name)
				assert.Equal(t, "", pcsg.Namespace)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pcsg := emptyPodCliqueScalingGroup(tt.objKey)

			assert.NotNil(t, pcsg)
			assert.IsType(t, &grovecorev1alpha1.PodCliqueScalingGroup{}, pcsg)

			if tt.validateFunc != nil {
				tt.validateFunc(t, pcsg)
			}
		})
	}
}
