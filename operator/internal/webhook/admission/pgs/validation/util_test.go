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

package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
)

// TestValidateEnumType validates the enum type validation utility function.
// It ensures proper validation of pointer values against allowed enum sets
// and generates appropriate field errors for invalid values.
func TestValidateEnumType(t *testing.T) {
	allowedValues := sets.New("value1", "value2", "value3")
	fldPath := field.NewPath("test", "field")

	testCases := []struct {
		// Test case name describing the enum validation scenario
		name string
		// value is the pointer to the value being validated
		value *string
		// allowedValues is the set of allowed enum values
		allowedValues sets.Set[string]
		// fldPath is the field path for error reporting
		fldPath *field.Path
		// expectError indicates whether validation should fail
		expectError bool
		// expectedErrType is the expected field error type
		expectedErrType field.ErrorType
	}{
		{
			// Valid enum value should pass validation
			name:          "valid enum value passes validation",
			value:         ptr.To("value1"),
			allowedValues: allowedValues,
			fldPath:       fldPath,
			expectError:   false,
		},
		{
			// Invalid enum value should fail validation
			name:            "invalid enum value fails validation",
			value:           ptr.To("invalid"),
			allowedValues:   allowedValues,
			fldPath:         fldPath,
			expectError:     true,
			expectedErrType: field.ErrorTypeInvalid,
		},
		{
			// Nil value should fail validation with required error
			name:            "nil value fails validation",
			value:           nil,
			allowedValues:   allowedValues,
			fldPath:         fldPath,
			expectError:     true,
			expectedErrType: field.ErrorTypeRequired,
		},
		{
			// Empty allowed values set with valid pointer should fail
			name:            "empty allowed values fails validation",
			value:           ptr.To("any"),
			allowedValues:   sets.New[string](),
			fldPath:         fldPath,
			expectError:     true,
			expectedErrType: field.ErrorTypeInvalid,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateEnumType(tc.value, tc.allowedValues, tc.fldPath)

			if tc.expectError {
				assert.NotEmpty(t, errs, "Expected validation errors")
				if len(errs) > 0 {
					assert.Equal(t, tc.expectedErrType, errs[0].Type, "Error type should match expected")
					assert.Equal(t, tc.fldPath.String(), errs[0].Field, "Field path should match")
				}
			} else {
				assert.Empty(t, errs, "Expected no validation errors")
			}
		})
	}
}

// TestValidateNonNilField validates the non-nil field validation utility function.
// It ensures proper validation of pointer fields and generates required field errors
// when values are nil.
func TestValidateNonNilField(t *testing.T) {
	fldPath := field.NewPath("test", "field")

	testCases := []struct {
		// Test case name describing the nil validation scenario
		name string
		// value is the pointer value being validated
		value interface{}
		// fldPath is the field path for error reporting
		fldPath *field.Path
		// expectError indicates whether validation should fail
		expectError bool
		// expectedErrType is the expected field error type
		expectedErrType field.ErrorType
	}{
		{
			// Non-nil string pointer should pass validation
			name:        "non-nil string pointer passes validation",
			value:       ptr.To("test"),
			fldPath:     fldPath,
			expectError: false,
		},
		{
			// Non-nil int pointer should pass validation
			name:        "non-nil int pointer passes validation",
			value:       ptr.To(42),
			fldPath:     fldPath,
			expectError: false,
		},
		{
			// Nil string pointer should fail validation
			name:            "nil string pointer fails validation",
			value:           (*string)(nil),
			fldPath:         fldPath,
			expectError:     true,
			expectedErrType: field.ErrorTypeRequired,
		},
		{
			// Nil int pointer should fail validation
			name:            "nil int pointer fails validation",
			value:           (*int)(nil),
			fldPath:         fldPath,
			expectError:     true,
			expectedErrType: field.ErrorTypeRequired,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var errs field.ErrorList

			// Use type assertion to call the appropriate validateNonNilField function
			switch v := tc.value.(type) {
			case *string:
				errs = validateNonNilField(v, tc.fldPath)
			case *int:
				errs = validateNonNilField(v, tc.fldPath)
			}

			if tc.expectError {
				assert.NotEmpty(t, errs, "Expected validation errors")
				if len(errs) > 0 {
					assert.Equal(t, tc.expectedErrType, errs[0].Type, "Error type should match expected")
					assert.Equal(t, tc.fldPath.String(), errs[0].Field, "Field path should match")
				}
			} else {
				assert.Empty(t, errs, "Expected no validation errors")
			}
		})
	}
}

// TestValidateNonEmptyStringField validates the non-empty string validation utility function.
// It ensures proper validation of string fields and generates required field errors
// when strings are empty.
func TestValidateNonEmptyStringField(t *testing.T) {
	fldPath := field.NewPath("test", "field")

	testCases := []struct {
		// Test case name describing the string validation scenario
		name string
		// value is the string value being validated
		value string
		// fldPath is the field path for error reporting
		fldPath *field.Path
		// expectError indicates whether validation should fail
		expectError bool
		// expectedErrType is the expected field error type
		expectedErrType field.ErrorType
	}{
		{
			// Non-empty string should pass validation
			name:        "non-empty string passes validation",
			value:       "test-value",
			fldPath:     fldPath,
			expectError: false,
		},
		{
			// String with whitespace should pass validation
			name:        "string with whitespace passes validation",
			value:       "  test  ",
			fldPath:     fldPath,
			expectError: false,
		},
		{
			// Empty string should fail validation
			name:            "empty string fails validation",
			value:           "",
			fldPath:         fldPath,
			expectError:     true,
			expectedErrType: field.ErrorTypeRequired,
		},
		{
			// String with only whitespace should fail validation
			name:            "whitespace-only string fails validation",
			value:           "   ",
			fldPath:         fldPath,
			expectError:     true,
			expectedErrType: field.ErrorTypeRequired,
		},
		{
			// String with tabs and newlines should fail validation
			name:            "string with tabs and newlines fails validation",
			value:           "\t\n\r",
			fldPath:         fldPath,
			expectError:     true,
			expectedErrType: field.ErrorTypeRequired,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateNonEmptyStringField(tc.value, tc.fldPath)

			if tc.expectError {
				assert.NotEmpty(t, errs, "Expected validation errors")
				if len(errs) > 0 {
					assert.Equal(t, tc.expectedErrType, errs[0].Type, "Error type should match expected")
					assert.Equal(t, tc.fldPath.String(), errs[0].Field, "Field path should match")
				}
			} else {
				assert.Empty(t, errs, "Expected no validation errors")
			}
		})
	}
}

// TestSliceMustHaveUniqueElements validates the slice uniqueness validation utility function.
// It ensures proper detection of duplicate elements in string slices and generates
// appropriate field errors with custom messages.
func TestSliceMustHaveUniqueElements(t *testing.T) {
	fldPath := field.NewPath("test", "field")

	testCases := []struct {
		// Test case name describing the uniqueness validation scenario
		name string
		// slice is the string slice being validated for uniqueness
		slice []string
		// fldPath is the field path for error reporting
		fldPath *field.Path
		// msg is the custom error message to use
		msg string
		// expectError indicates whether validation should fail
		expectError bool
		// expectedErrType is the expected field error type
		expectedErrType field.ErrorType
		// expectedDuplicates are the expected duplicate values in the error
		expectedDuplicates []string
	}{
		{
			// Slice with unique elements should pass validation
			name:        "slice with unique elements passes validation",
			slice:       []string{"a", "b", "c"},
			fldPath:     fldPath,
			msg:         "elements must be unique",
			expectError: false,
		},
		{
			// Empty slice should pass validation
			name:        "empty slice passes validation",
			slice:       []string{},
			fldPath:     fldPath,
			msg:         "elements must be unique",
			expectError: false,
		},
		{
			// Single element slice should pass validation
			name:        "single element slice passes validation",
			slice:       []string{"single"},
			fldPath:     fldPath,
			msg:         "elements must be unique",
			expectError: false,
		},
		{
			// Slice with duplicate elements should fail validation
			name:               "slice with duplicates fails validation",
			slice:              []string{"a", "b", "a", "c"},
			fldPath:            fldPath,
			msg:                "elements must be unique",
			expectError:        true,
			expectedErrType:    field.ErrorTypeInvalid,
			expectedDuplicates: []string{"a"},
		},
		{
			// Slice with multiple duplicates should fail validation
			name:               "slice with multiple duplicates fails validation",
			slice:              []string{"a", "b", "a", "b", "c"},
			fldPath:            fldPath,
			msg:                "elements must be unique",
			expectError:        true,
			expectedErrType:    field.ErrorTypeInvalid,
			expectedDuplicates: []string{"a", "b"},
		},
		{
			// Slice with all duplicate elements should fail validation
			name:               "slice with all duplicates fails validation",
			slice:              []string{"same", "same", "same"},
			fldPath:            fldPath,
			msg:                "all elements are duplicates",
			expectError:        true,
			expectedErrType:    field.ErrorTypeInvalid,
			expectedDuplicates: []string{"same"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := sliceMustHaveUniqueElements(tc.slice, tc.fldPath, tc.msg)

			if tc.expectError {
				assert.NotEmpty(t, errs, "Expected validation errors")
				if len(errs) > 0 {
					assert.Equal(t, tc.expectedErrType, errs[0].Type, "Error type should match expected")
					assert.Equal(t, tc.fldPath.String(), errs[0].Field, "Field path should match")
					assert.Contains(t, errs[0].Detail, tc.msg, "Error detail should contain custom message")

					// Verify that all expected duplicates are mentioned in the error
					for _, duplicate := range tc.expectedDuplicates {
						assert.Contains(t, errs[0].BadValue, duplicate, "Error should mention duplicate value")
					}
				}
			} else {
				assert.Empty(t, errs, "Expected no validation errors")
			}
		})
	}
}

// TestUtilityFunctionsIntegration performs integration testing of validation utilities.
// It validates that the utility functions work correctly together in realistic scenarios.
func TestUtilityFunctionsIntegration(t *testing.T) {
	// Test scenario: validating a complex configuration structure
	fldPath := field.NewPath("spec", "config")

	// Test enum validation with valid value
	validEnum := "option1"
	allowedOptions := sets.New("option1", "option2", "option3")
	enumErrs := validateEnumType(&validEnum, allowedOptions, fldPath.Child("option"))
	assert.Empty(t, enumErrs, "Valid enum should pass validation")

	// Test non-nil validation with valid pointer
	validPointer := ptr.To("test-value")
	nilErrs := validateNonNilField(validPointer, fldPath.Child("value"))
	assert.Empty(t, nilErrs, "Valid pointer should pass validation")

	// Test non-empty string validation with valid string
	validString := "non-empty-string"
	stringErrs := validateNonEmptyStringField(validString, fldPath.Child("name"))
	assert.Empty(t, stringErrs, "Valid string should pass validation")

	// Test slice uniqueness validation with unique elements
	uniqueSlice := []string{"item1", "item2", "item3"}
	uniqueErrs := sliceMustHaveUniqueElements(uniqueSlice, fldPath.Child("items"), "items must be unique")
	assert.Empty(t, uniqueErrs, "Unique slice should pass validation")

	// Test combined validation with errors
	invalidEnum := "invalid-option"
	enumErrsCombined := validateEnumType(&invalidEnum, allowedOptions, fldPath.Child("option"))

	duplicateSlice := []string{"item1", "item2", "item1"}
	duplicateErrs := sliceMustHaveUniqueElements(duplicateSlice, fldPath.Child("items"), "items must be unique")

	// Combine all errors
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, enumErrsCombined...)
	allErrs = append(allErrs, duplicateErrs...)

	assert.Len(t, allErrs, 2, "Should have exactly 2 validation errors")
	assert.Equal(t, field.ErrorTypeInvalid, allErrs[0].Type, "First error should be invalid enum")
	assert.Equal(t, field.ErrorTypeInvalid, allErrs[1].Type, "Second error should be duplicate items")
}
