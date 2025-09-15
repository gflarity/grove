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

package component

import (
	"context"
	"testing"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestOperationConstants validates that all operation constants have the expected values.
// This ensures that the operation names remain consistent and don't change unexpectedly.
func TestOperationConstants(t *testing.T) {
	tests := []struct {
		// name describes the specific constant being tested
		name string
		// actualValue is the constant value being validated
		actualValue string
		// expectedValue is what the constant should equal
		expectedValue string
	}{
		{
			// Validates the constant used for getting existing resource names operations
			name:          "OperationGetExistingResourceNames",
			actualValue:   OperationGetExistingResourceNames,
			expectedValue: "GetExistingResourceNames",
		},
		{
			// Validates the constant used for sync operations
			name:          "OperationSync",
			actualValue:   OperationSync,
			expectedValue: "Sync",
		},
		{
			// Validates the constant used for delete operations
			name:          "OperationDelete",
			actualValue:   OperationDelete,
			expectedValue: "Delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedValue, tt.actualValue, "operation constant should have expected value")
		})
	}
}

// TestComponentNameConstants was removed because the referenced Name* constants
// were refactored and no longer exist in the codebase. The component name
// constants are now defined in api/common/labels.go with LabelComponentName* prefix.

// TestKindConstants validates that all Kind constants have the expected values.
// Kind constants are used to identify different types of Kubernetes resources.
func TestKindConstants(t *testing.T) {
	tests := []struct {
		// name describes the specific Kind constant being tested
		name string
		// actualValue is the Kind constant value being validated
		actualValue Kind
		// expectedValue is what the Kind constant should equal
		expectedValue Kind
	}{
		{
			// Validates the Kind for PodClique resources
			name:          "KindPodClique",
			actualValue:   KindPodClique,
			expectedValue: "PodClique",
		},
		{
			// Validates the Kind for ServiceAccount resources
			name:          "KindServiceAccount",
			actualValue:   KindServiceAccount,
			expectedValue: "ServiceAccount",
		},
		{
			// Validates the Kind for Role resources
			name:          "KindRole",
			actualValue:   KindRole,
			expectedValue: "Role",
		},
		{
			// Validates the Kind for RoleBinding resources
			name:          "KindRoleBinding",
			actualValue:   KindRoleBinding,
			expectedValue: "RoleBinding",
		},
		{
			// Validates the Kind for ServiceAccount token secrets
			name:          "KindServiceAccountTokenSecret",
			actualValue:   KindServiceAccountTokenSecret,
			expectedValue: "ServiceAccountTokenSecret",
		},
		{
			// Validates the Kind for headless Service resources
			name:          "KindHeadlessService",
			actualValue:   KindHeadlessService,
			expectedValue: "HeadlessService",
		},
		{
			// Validates the Kind for HorizontalPodAutoscaler resources
			name:          "KindHorizontalPodAutoscaler",
			actualValue:   KindHorizontalPodAutoscaler,
			expectedValue: "HorizontalPodAutoscaler",
		},
		{
			// Validates the Kind for Pod resources
			name:          "KindPod",
			actualValue:   KindPod,
			expectedValue: "Pod",
		},
		{
			// Validates the Kind for PodCliqueScalingGroup resources
			name:          "KindPodCliqueScalingGroup",
			actualValue:   KindPodCliqueScalingGroup,
			expectedValue: "PodCliqueScalingGroup",
		},
		{
			// Validates the Kind for PodGang resources
			name:          "KindPodGang",
			actualValue:   KindPodGang,
			expectedValue: "PodGang",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedValue, tt.actualValue, "Kind constant should have expected value")
		})
	}
}

// TestNewOperatorRegistry validates the creation of new OperatorRegistry instances.
// It ensures that the registry is properly initialized and ready for use.
func TestNewOperatorRegistry(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario
		name string
		// registryType specifies which Grove custom resource type to test with
		registryType string
		// validateFunc performs assertions on the created registry
		validateFunc func(t *testing.T, registry interface{})
	}{
		{
			// Tests registry creation for PodGangSet resources
			name:         "create_podgangset_registry",
			registryType: "PodGangSet",
			validateFunc: func(t *testing.T, registry interface{}) {
				pgsRegistry := registry.(OperatorRegistry[grovecorev1alpha1.PodGangSet])
				require.NotNil(t, pgsRegistry, "PodGangSet registry should not be nil")

				// Verify initial state - no operators registered
				allOperators := pgsRegistry.GetAllOperators()
				assert.Empty(t, allOperators, "new registry should have no operators initially")

				// Verify error when getting non-existent operator
				_, err := pgsRegistry.GetOperator(KindPodClique)
				assert.Error(t, err, "should return error for non-existent operator")
				assert.Contains(t, err.Error(), "operator for kind PodClique not found")
			},
		},
		{
			// Tests registry creation for PodClique resources
			name:         "create_podclique_registry",
			registryType: "PodClique",
			validateFunc: func(t *testing.T, registry interface{}) {
				pcRegistry := registry.(OperatorRegistry[grovecorev1alpha1.PodClique])
				require.NotNil(t, pcRegistry, "PodClique registry should not be nil")

				// Verify initial state
				allOperators := pcRegistry.GetAllOperators()
				assert.Empty(t, allOperators, "new registry should have no operators initially")

				// Verify error handling
				_, err := pcRegistry.GetOperator(KindServiceAccount)
				assert.Error(t, err, "should return error for non-existent operator")
			},
		},
		{
			// Tests registry creation for PodCliqueScalingGroup resources
			name:         "create_podcliquescalinggroup_registry",
			registryType: "PodCliqueScalingGroup",
			validateFunc: func(t *testing.T, registry interface{}) {
				pcsgRegistry := registry.(OperatorRegistry[grovecorev1alpha1.PodCliqueScalingGroup])
				require.NotNil(t, pcsgRegistry, "PodCliqueScalingGroup registry should not be nil")

				// Verify initial state
				allOperators := pcsgRegistry.GetAllOperators()
				assert.Empty(t, allOperators, "new registry should have no operators initially")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var registry interface{}

			// Create registry based on type
			switch tt.registryType {
			case "PodGangSet":
				registry = NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
			case "PodClique":
				registry = NewOperatorRegistry[grovecorev1alpha1.PodClique]()
			case "PodCliqueScalingGroup":
				registry = NewOperatorRegistry[grovecorev1alpha1.PodCliqueScalingGroup]()
			default:
				t.Fatalf("unknown registry type: %s", tt.registryType)
			}

			tt.validateFunc(t, registry)
		})
	}
}

// TestOperatorRegistryRegister validates the Register method of OperatorRegistry.
// It ensures that operators can be properly registered and retrieved.
func TestOperatorRegistryRegister(t *testing.T) {
	tests := []struct {
		// name describes the specific registration scenario
		name string
		// setupFunc prepares the test by creating registry and mock operators
		setupFunc func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], []mockOperatorRegistration)
		// validateFunc performs assertions on the registry after registration
		validateFunc func(t *testing.T, registry OperatorRegistry[grovecorev1alpha1.PodGangSet], registrations []mockOperatorRegistration)
	}{
		{
			// Tests registration of a single operator
			name: "register_single_operator",
			setupFunc: func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], []mockOperatorRegistration) {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				mockOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "test-operator"}
				registrations := []mockOperatorRegistration{
					{kind: KindPodClique, operator: mockOp},
				}
				return registry, registrations
			},
			validateFunc: func(t *testing.T, registry OperatorRegistry[grovecorev1alpha1.PodGangSet], registrations []mockOperatorRegistration) {
				// Register the operator
				registry.Register(registrations[0].kind, registrations[0].operator)

				// Verify operator can be retrieved
				retrievedOp, err := registry.GetOperator(KindPodClique)
				assert.NoError(t, err, "should successfully retrieve registered operator")
				assert.Same(t, registrations[0].operator, retrievedOp, "retrieved operator should be the same instance")

				// Verify GetAllOperators returns the registered operator
				allOperators := registry.GetAllOperators()
				assert.Len(t, allOperators, 1, "should have exactly one operator")
				assert.Contains(t, allOperators, KindPodClique, "should contain the registered kind")
				assert.Same(t, registrations[0].operator, allOperators[KindPodClique], "should return the same operator instance")
			},
		},
		{
			// Tests registration of multiple operators
			name: "register_multiple_operators",
			setupFunc: func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], []mockOperatorRegistration) {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				registrations := []mockOperatorRegistration{
					{kind: KindPodClique, operator: &mockOperator[grovecorev1alpha1.PodGangSet]{name: "podclique-operator"}},
					{kind: KindServiceAccount, operator: &mockOperator[grovecorev1alpha1.PodGangSet]{name: "serviceaccount-operator"}},
					{kind: KindRole, operator: &mockOperator[grovecorev1alpha1.PodGangSet]{name: "role-operator"}},
				}
				return registry, registrations
			},
			validateFunc: func(t *testing.T, registry OperatorRegistry[grovecorev1alpha1.PodGangSet], registrations []mockOperatorRegistration) {
				// Register all operators
				for _, reg := range registrations {
					registry.Register(reg.kind, reg.operator)
				}

				// Verify all operators can be retrieved
				for _, reg := range registrations {
					retrievedOp, err := registry.GetOperator(reg.kind)
					assert.NoError(t, err, "should successfully retrieve operator for kind %s", reg.kind)
					assert.Same(t, reg.operator, retrievedOp, "retrieved operator should be the same instance for kind %s", reg.kind)
				}

				// Verify GetAllOperators returns all registered operators
				allOperators := registry.GetAllOperators()
				assert.Len(t, allOperators, len(registrations), "should have all registered operators")
				for _, reg := range registrations {
					assert.Contains(t, allOperators, reg.kind, "should contain kind %s", reg.kind)
					assert.Same(t, reg.operator, allOperators[reg.kind], "should return correct operator for kind %s", reg.kind)
				}
			},
		},
		{
			// Tests overwriting an existing operator registration
			name: "overwrite_existing_operator",
			setupFunc: func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], []mockOperatorRegistration) {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				registrations := []mockOperatorRegistration{
					{kind: KindPodClique, operator: &mockOperator[grovecorev1alpha1.PodGangSet]{name: "original-operator"}},
					{kind: KindPodClique, operator: &mockOperator[grovecorev1alpha1.PodGangSet]{name: "replacement-operator"}},
				}
				return registry, registrations
			},
			validateFunc: func(t *testing.T, registry OperatorRegistry[grovecorev1alpha1.PodGangSet], registrations []mockOperatorRegistration) {
				// Register original operator
				registry.Register(registrations[0].kind, registrations[0].operator)

				// Verify original operator is registered
				retrievedOp, err := registry.GetOperator(KindPodClique)
				assert.NoError(t, err)
				assert.Same(t, registrations[0].operator, retrievedOp, "should retrieve original operator")

				// Register replacement operator (overwrite)
				registry.Register(registrations[1].kind, registrations[1].operator)

				// Verify replacement operator is now registered
				retrievedOp, err = registry.GetOperator(KindPodClique)
				assert.NoError(t, err)
				assert.Same(t, registrations[1].operator, retrievedOp, "should retrieve replacement operator")

				// Verify only one operator is registered
				allOperators := registry.GetAllOperators()
				assert.Len(t, allOperators, 1, "should have exactly one operator after overwrite")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, registrations := tt.setupFunc(t)
			tt.validateFunc(t, registry, registrations)
		})
	}
}

// TestOperatorRegistryGetOperator validates the GetOperator method of OperatorRegistry.
// It tests both successful retrieval and error scenarios.
func TestOperatorRegistryGetOperator(t *testing.T) {
	tests := []struct {
		// name describes the specific retrieval scenario
		name string
		// setupFunc prepares the registry with registered operators
		setupFunc func(t *testing.T) OperatorRegistry[grovecorev1alpha1.PodGangSet]
		// kindToRetrieve is the Kind of operator to attempt to retrieve
		kindToRetrieve Kind
		// expectError indicates whether an error should be returned
		expectError bool
		// expectedErrorMessage is the expected error message (if expectError is true)
		expectedErrorMessage string
		// validateOperator performs additional validation on the retrieved operator (if no error)
		validateOperator func(t *testing.T, operator Operator[grovecorev1alpha1.PodGangSet])
	}{
		{
			// Tests successful retrieval of a registered operator
			name: "successful_retrieval",
			setupFunc: func(t *testing.T) OperatorRegistry[grovecorev1alpha1.PodGangSet] {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				mockOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "test-operator"}
				registry.Register(KindPodClique, mockOp)
				return registry
			},
			kindToRetrieve: KindPodClique,
			expectError:    false,
			validateOperator: func(t *testing.T, operator Operator[grovecorev1alpha1.PodGangSet]) {
				assert.NotNil(t, operator, "retrieved operator should not be nil")
				mockOp := operator.(*mockOperator[grovecorev1alpha1.PodGangSet])
				assert.Equal(t, "test-operator", mockOp.name, "should retrieve the correct operator")
			},
		},
		{
			// Tests error when attempting to retrieve non-existent operator
			name: "operator_not_found",
			setupFunc: func(t *testing.T) OperatorRegistry[grovecorev1alpha1.PodGangSet] {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				// Register a different operator to ensure registry is not empty
				mockOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "other-operator"}
				registry.Register(KindServiceAccount, mockOp)
				return registry
			},
			kindToRetrieve:       KindPodClique,
			expectError:          true,
			expectedErrorMessage: "operator for kind PodClique not found",
		},
		{
			// Tests retrieval from empty registry
			name: "empty_registry",
			setupFunc: func(t *testing.T) OperatorRegistry[grovecorev1alpha1.PodGangSet] {
				return NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
			},
			kindToRetrieve:       KindRole,
			expectError:          true,
			expectedErrorMessage: "operator for kind Role not found",
		},
		{
			// Tests retrieval with multiple operators registered
			name: "multiple_operators_registered",
			setupFunc: func(t *testing.T) OperatorRegistry[grovecorev1alpha1.PodGangSet] {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				registry.Register(KindPodClique, &mockOperator[grovecorev1alpha1.PodGangSet]{name: "podclique-op"})
				registry.Register(KindServiceAccount, &mockOperator[grovecorev1alpha1.PodGangSet]{name: "sa-op"})
				registry.Register(KindRole, &mockOperator[grovecorev1alpha1.PodGangSet]{name: "role-op"})
				return registry
			},
			kindToRetrieve: KindServiceAccount,
			expectError:    false,
			validateOperator: func(t *testing.T, operator Operator[grovecorev1alpha1.PodGangSet]) {
				mockOp := operator.(*mockOperator[grovecorev1alpha1.PodGangSet])
				assert.Equal(t, "sa-op", mockOp.name, "should retrieve the correct ServiceAccount operator")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := tt.setupFunc(t)

			operator, err := registry.GetOperator(tt.kindToRetrieve)

			if tt.expectError {
				assert.Error(t, err, "expected an error but got none")
				assert.Nil(t, operator, "operator should be nil when error occurs")
				if tt.expectedErrorMessage != "" {
					assert.Contains(t, err.Error(), tt.expectedErrorMessage, "error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "expected no error but got: %v", err)
				assert.NotNil(t, operator, "operator should not be nil")
				if tt.validateOperator != nil {
					tt.validateOperator(t, operator)
				}
			}
		})
	}
}

// TestOperatorRegistryGetAllOperators validates the GetAllOperators method of OperatorRegistry.
// It ensures that all registered operators are returned correctly.
func TestOperatorRegistryGetAllOperators(t *testing.T) {
	tests := []struct {
		// name describes the specific test scenario
		name string
		// setupFunc prepares the registry with operators and returns expected operators map
		setupFunc func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], map[Kind]Operator[grovecorev1alpha1.PodGangSet])
		// validateFunc performs assertions on the returned operators map
		validateFunc func(t *testing.T, allOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet], expectedOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet])
	}{
		{
			// Tests GetAllOperators on empty registry
			name: "empty_registry",
			setupFunc: func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], map[Kind]Operator[grovecorev1alpha1.PodGangSet]) {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				expectedOperators := make(map[Kind]Operator[grovecorev1alpha1.PodGangSet])
				return registry, expectedOperators
			},
			validateFunc: func(t *testing.T, allOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet], expectedOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet]) {
				assert.Empty(t, allOperators, "empty registry should return empty operators map")
				assert.Equal(t, expectedOperators, allOperators, "should match expected empty map")
			},
		},
		{
			// Tests GetAllOperators with single registered operator
			name: "single_operator",
			setupFunc: func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], map[Kind]Operator[grovecorev1alpha1.PodGangSet]) {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				mockOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "single-operator"}
				registry.Register(KindPodClique, mockOp)

				expectedOperators := map[Kind]Operator[grovecorev1alpha1.PodGangSet]{
					KindPodClique: mockOp,
				}
				return registry, expectedOperators
			},
			validateFunc: func(t *testing.T, allOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet], expectedOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet]) {
				assert.Len(t, allOperators, 1, "should have exactly one operator")
				assert.Equal(t, expectedOperators, allOperators, "should match expected operators map")
				assert.Contains(t, allOperators, KindPodClique, "should contain PodClique operator")
			},
		},
		{
			// Tests GetAllOperators with multiple registered operators
			name: "multiple_operators",
			setupFunc: func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], map[Kind]Operator[grovecorev1alpha1.PodGangSet]) {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()

				podCliqueOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "podclique-operator"}
				serviceAccountOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "serviceaccount-operator"}
				roleOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "role-operator"}
				hpaOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "hpa-operator"}

				registry.Register(KindPodClique, podCliqueOp)
				registry.Register(KindServiceAccount, serviceAccountOp)
				registry.Register(KindRole, roleOp)
				registry.Register(KindHorizontalPodAutoscaler, hpaOp)

				expectedOperators := map[Kind]Operator[grovecorev1alpha1.PodGangSet]{
					KindPodClique:               podCliqueOp,
					KindServiceAccount:          serviceAccountOp,
					KindRole:                    roleOp,
					KindHorizontalPodAutoscaler: hpaOp,
				}
				return registry, expectedOperators
			},
			validateFunc: func(t *testing.T, allOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet], expectedOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet]) {
				assert.Len(t, allOperators, 4, "should have exactly four operators")
				assert.Equal(t, expectedOperators, allOperators, "should match expected operators map")

				// Verify each expected operator is present
				for kind, expectedOp := range expectedOperators {
					assert.Contains(t, allOperators, kind, "should contain operator for kind %s", kind)
					assert.Same(t, expectedOp, allOperators[kind], "should return same operator instance for kind %s", kind)
				}
			},
		},
		{
			// Tests that GetAllOperators returns a copy/reference that doesn't affect the registry
			name: "returned_map_isolation",
			setupFunc: func(t *testing.T) (OperatorRegistry[grovecorev1alpha1.PodGangSet], map[Kind]Operator[grovecorev1alpha1.PodGangSet]) {
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				mockOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "test-operator"}
				registry.Register(KindPodClique, mockOp)

				expectedOperators := map[Kind]Operator[grovecorev1alpha1.PodGangSet]{
					KindPodClique: mockOp,
				}
				return registry, expectedOperators
			},
			validateFunc: func(t *testing.T, allOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet], expectedOperators map[Kind]Operator[grovecorev1alpha1.PodGangSet]) {
				// Modify the returned map
				delete(allOperators, KindPodClique)
				allOperators[KindServiceAccount] = &mockOperator[grovecorev1alpha1.PodGangSet]{name: "new-operator"}

				// Verify the registry is unaffected by getting all operators again
				registry := NewOperatorRegistry[grovecorev1alpha1.PodGangSet]()
				mockOp := &mockOperator[grovecorev1alpha1.PodGangSet]{name: "test-operator"}
				registry.Register(KindPodClique, mockOp)

				freshOperators := registry.GetAllOperators()
				assert.Len(t, freshOperators, 1, "registry should still have one operator")
				assert.Contains(t, freshOperators, KindPodClique, "registry should still contain PodClique operator")
				assert.NotContains(t, freshOperators, KindServiceAccount, "registry should not contain ServiceAccount operator")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, expectedOperators := tt.setupFunc(t)
			allOperators := registry.GetAllOperators()
			tt.validateFunc(t, allOperators, expectedOperators)
		})
	}
}

// mockOperatorRegistration represents an operator registration for testing purposes.
type mockOperatorRegistration struct {
	// kind is the Kind of resource this operator manages
	kind Kind
	// operator is the mock operator instance
	operator Operator[grovecorev1alpha1.PodGangSet]
}

// mockOperator is a mock implementation of the Operator interface for testing purposes.
type mockOperator[T GroveCustomResourceType] struct {
	// name is a unique identifier for this mock operator instance
	name string
	// getExistingResourceNamesFunc allows customizing the GetExistingResourceNames behavior
	getExistingResourceNamesFunc func(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) ([]string, error)
	// syncFunc allows customizing the Sync behavior
	syncFunc func(ctx context.Context, logger logr.Logger, obj *T) error
	// deleteFunc allows customizing the Delete behavior
	deleteFunc func(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) error
}

// GetExistingResourceNames implements the Operator interface.
func (m *mockOperator[T]) GetExistingResourceNames(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) ([]string, error) {
	if m.getExistingResourceNamesFunc != nil {
		return m.getExistingResourceNamesFunc(ctx, logger, objMeta)
	}
	return []string{}, nil
}

// Sync implements the Operator interface.
func (m *mockOperator[T]) Sync(ctx context.Context, logger logr.Logger, obj *T) error {
	if m.syncFunc != nil {
		return m.syncFunc(ctx, logger, obj)
	}
	return nil
}

// Delete implements the Operator interface.
func (m *mockOperator[T]) Delete(ctx context.Context, logger logr.Logger, objMeta metav1.ObjectMeta) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, logger, objMeta)
	}
	return nil
}
