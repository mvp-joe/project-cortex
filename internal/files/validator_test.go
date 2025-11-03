package files

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  ValidationError
		want string
	}{
		{
			name: "with hint",
			err: ValidationError{
				Field:   "limit",
				Value:   "5000",
				Message: "limit must be between 1 and 1000",
				Hint:    "Adjust the limit value",
			},
			want: `limit: limit must be between 1 and 1000 (value: "5000"). Adjust the limit value`,
		},
		{
			name: "without hint",
			err: ValidationError{
				Field:   "table",
				Value:   "unknown",
				Message: "table does not exist",
			},
			want: `table: table does not exist (value: "unknown")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestValidationErrors_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		errors ValidationErrors
		want   string
	}{
		{
			name:   "no errors",
			errors: ValidationErrors{},
			want:   "no validation errors",
		},
		{
			name: "single error",
			errors: ValidationErrors{
				{Field: "limit", Value: "5000", Message: "invalid"},
			},
			want: `limit: invalid (value: "5000")`,
		},
		{
			name: "multiple errors",
			errors: ValidationErrors{
				{Field: "limit", Value: "5000", Message: "invalid"},
				{Field: "offset", Value: "-1", Message: "must be non-negative"},
			},
			want: "2 validation errors:\n  1. limit: invalid (value: \"5000\")\n  2. offset: must be non-negative (value: \"-1\")\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.errors.Error())
		})
	}
}

func TestValidator_Validate_BasicQuery(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From:   "files",
		Fields: []string{"file_path", "language"},
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_MissingFrom(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		Fields: []string{"file_path"},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.Len(t, validationErrs, 1)
	assert.Equal(t, "from", validationErrs[0].Field)
}

func TestValidator_Validate_InvalidTable(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "nonexistent_table",
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.Len(t, validationErrs, 1)
	assert.Equal(t, "from", validationErrs[0].Field)
	assert.Contains(t, validationErrs[0].Message, "unknown table")
}

func TestValidator_Validate_InvalidField(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From:   "files",
		Fields: []string{"file_path", "invalid_field"},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.Len(t, validationErrs, 1)
	assert.Equal(t, "fields", validationErrs[0].Field)
	assert.Equal(t, "invalid_field", validationErrs[0].Value)
}

func TestValidator_Validate_WildcardField(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From:   "files",
		Fields: []string{"*"},
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_WhereFilter(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	filter := NewFieldFilter(FieldFilter{
		Field:    "language",
		Operator: OpEqual,
		Value:    "go",
	})

	query := &QueryDefinition{
		From:  "files",
		Where: &filter,
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_WhereFilter_InvalidField(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	filter := NewFieldFilter(FieldFilter{
		Field:    "invalid_field",
		Operator: OpEqual,
		Value:    "value",
	})

	query := &QueryDefinition{
		From:  "files",
		Where: &filter,
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Message, "unknown column")
}

func TestValidator_Validate_WhereFilter_InvalidOperator(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	filter := NewFieldFilter(FieldFilter{
		Field:    "language",
		Operator: ComparisonOperator("INVALID"),
		Value:    "go",
	})

	query := &QueryDefinition{
		From:  "files",
		Where: &filter,
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Message, "invalid comparison operator")
}

func TestValidator_Validate_WhereFilter_MissingValue(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	filter := NewFieldFilter(FieldFilter{
		Field:    "language",
		Operator: OpEqual,
		Value:    nil,
	})

	query := &QueryDefinition{
		From:  "files",
		Where: &filter,
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Message, "requires a value")
}

func TestValidator_Validate_WhereFilter_IsNull(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	filter := NewFieldFilter(FieldFilter{
		Field:    "cyclomatic_complexity",
		Operator: OpIsNull,
	})

	query := &QueryDefinition{
		From:  "functions",
		Where: &filter,
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_ComplexFilter(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	filter := NewAndFilter(AndFilter{
		And: []Filter{
			NewFieldFilter(FieldFilter{Field: "language", Operator: OpEqual, Value: "go"}),
			NewOrFilter(OrFilter{
				Or: []Filter{
					NewFieldFilter(FieldFilter{Field: "is_test", Operator: OpEqual, Value: 1}),
					NewFieldFilter(FieldFilter{Field: "line_count_code", Operator: OpGreater, Value: 100}),
				},
			}),
		},
	})

	query := &QueryDefinition{
		From:  "files",
		Where: &filter,
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_Join(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "functions",
		Joins: []Join{
			{
				Table: "files",
				Type:  JoinInner,
				On: NewFieldFilter(FieldFilter{
					Field:    "functions.file_path",
					Operator: OpEqual,
					Value:    "files.file_path",
				}),
			},
		},
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_Join_InvalidType(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "functions",
		Joins: []Join{
			{
				Table: "files",
				Type:  JoinType("OUTER"),
				On: NewFieldFilter(FieldFilter{
					Field:    "file_path",
					Operator: OpEqual,
					Value:    "file_path",
				}),
			},
		},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Field, "joins[0].type")
}

func TestValidator_Validate_Join_InvalidTable(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "functions",
		Joins: []Join{
			{
				Table: "nonexistent",
				Type:  JoinInner,
				On: NewFieldFilter(FieldFilter{
					Field:    "file_path",
					Operator: OpEqual,
					Value:    "file_path",
				}),
			},
		},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Field, "joins[0].table")
}

func TestValidator_Validate_GroupBy(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From:    "files",
		GroupBy: []string{"language"},
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_GroupBy_InvalidField(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From:    "files",
		GroupBy: []string{"invalid_field"},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.Len(t, validationErrs, 1)
	assert.Equal(t, "groupBy", validationErrs[0].Field)
}

func TestValidator_Validate_OrderBy(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "files",
		OrderBy: []OrderBy{
			{Field: "line_count_code", Direction: SortDesc},
		},
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_OrderBy_InvalidDirection(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "files",
		OrderBy: []OrderBy{
			{Field: "line_count_code", Direction: SortDirection("RANDOM")},
		},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Field, "orderBy[0].direction")
}

func TestValidator_Validate_OrderBy_InvalidField(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "files",
		OrderBy: []OrderBy{
			{Field: "invalid_field", Direction: SortDesc},
		},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Field, "orderBy[0].field")
}

func TestValidator_Validate_Limit(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	tests := []struct {
		name    string
		limit   int
		wantErr bool
	}{
		{"valid limit 1", 1, false},
		{"valid limit 500", 500, false},
		{"valid limit 1000", 1000, false},
		{"invalid limit 0", 0, true},
		{"invalid limit -1", -1, true},
		{"invalid limit 1001", 1001, true},
		{"invalid limit 5000", 5000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limit := tt.limit
			query := &QueryDefinition{
				From:  "files",
				Limit: &limit,
			}

			err := validator.Validate(query)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidator_Validate_Offset(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	tests := []struct {
		name    string
		offset  int
		wantErr bool
	}{
		{"valid offset 0", 0, false},
		{"valid offset 100", 100, false},
		{"invalid offset -1", -1, true},
		{"invalid offset -100", -100, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			offset := tt.offset
			query := &QueryDefinition{
				From:   "files",
				Offset: &offset,
			}

			err := validator.Validate(query)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidator_Validate_Aggregations(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	lineCountField := "line_count_total"
	query := &QueryDefinition{
		From: "files",
		Aggregations: []Aggregation{
			{Function: AggCount, Alias: "total_files"},
			{Function: AggSum, Field: &lineCountField, Alias: "total_lines"},
		},
	}

	err := validator.Validate(query)
	assert.NoError(t, err)
}

func TestValidator_Validate_Aggregations_InvalidFunction(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "files",
		Aggregations: []Aggregation{
			{Function: AggregationFunction("MEDIAN"), Alias: "median_value"},
		},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Field, "aggregations[0].function")
}

func TestValidator_Validate_Aggregations_MissingField(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "files",
		Aggregations: []Aggregation{
			{Function: AggSum, Alias: "total"}, // Missing required field
		},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Field, "aggregations[0].field")
	assert.Contains(t, validationErrs[0].Message, "requires a field")
}

func TestValidator_Validate_Aggregations_InvalidField(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	invalidField := "invalid_field"
	query := &QueryDefinition{
		From: "files",
		Aggregations: []Aggregation{
			{Function: AggSum, Field: &invalidField, Alias: "total"},
		},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Field, "aggregations[0].field")
}

func TestValidator_Validate_Aggregations_MissingAlias(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	query := &QueryDefinition{
		From: "files",
		Aggregations: []Aggregation{
			{Function: AggCount}, // Missing alias
		},
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) > 0)
	assert.Contains(t, validationErrs[0].Field, "aggregations[0].alias")
}

func TestValidator_Validate_MultipleErrors(t *testing.T) {
	t.Parallel()

	validator := NewValidator()

	limit := 5000
	offset := -10
	query := &QueryDefinition{
		From:   "files",
		Fields: []string{"invalid_field"},
		Limit:  &limit,
		Offset: &offset,
	}

	err := validator.Validate(query)
	require.Error(t, err)

	validationErrs, ok := err.(ValidationErrors)
	require.True(t, ok)
	assert.True(t, len(validationErrs) >= 3, "should have multiple errors")
}
