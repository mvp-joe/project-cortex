package files

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComparisonOperator_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		operator ComparisonOperator
		want     bool
	}{
		{"equal", OpEqual, true},
		{"not equal", OpNotEqual, true},
		{"greater", OpGreater, true},
		{"greater equal", OpGreaterEqual, true},
		{"less", OpLess, true},
		{"less equal", OpLessEqual, true},
		{"like", OpLike, true},
		{"not like", OpNotLike, true},
		{"in", OpIn, true},
		{"not in", OpNotIn, true},
		{"is null", OpIsNull, true},
		{"is not null", OpIsNotNull, true},
		{"between", OpBetween, true},
		{"invalid", ComparisonOperator("INVALID"), false},
		{"empty", ComparisonOperator(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.operator.IsValid())
		})
	}
}

func TestComparisonOperator_RequiresValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		operator ComparisonOperator
		want     bool
	}{
		{"equal requires value", OpEqual, true},
		{"greater requires value", OpGreater, true},
		{"like requires value", OpLike, true},
		{"in requires value", OpIn, true},
		{"between requires value", OpBetween, true},
		{"is null no value", OpIsNull, false},
		{"is not null no value", OpIsNotNull, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.operator.RequiresValue())
		})
	}
}

func TestJoinType_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		joinType JoinType
		want     bool
	}{
		{"inner", JoinInner, true},
		{"left", JoinLeft, true},
		{"right", JoinRight, true},
		{"full", JoinFull, true},
		{"invalid", JoinType("OUTER"), false},
		{"empty", JoinType(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.joinType.IsValid())
		})
	}
}

func TestAggregationFunction_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   AggregationFunction
		want bool
	}{
		{"count", AggCount, true},
		{"sum", AggSum, true},
		{"avg", AggAvg, true},
		{"min", AggMin, true},
		{"max", AggMax, true},
		{"invalid", AggregationFunction("MEDIAN"), false},
		{"empty", AggregationFunction(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.fn.IsValid())
		})
	}
}

func TestAggregationFunction_RequiresField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   AggregationFunction
		want bool
	}{
		{"count no field required", AggCount, false},
		{"sum requires field", AggSum, true},
		{"avg requires field", AggAvg, true},
		{"min requires field", AggMin, true},
		{"max requires field", AggMax, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.fn.RequiresField())
		})
	}
}

func TestSortDirection_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		direction SortDirection
		want      bool
	}{
		{"asc", SortAsc, true},
		{"desc", SortDesc, true},
		{"invalid", SortDirection("RANDOM"), false},
		{"empty", SortDirection(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.direction.IsValid())
		})
	}
}

func TestFilter_FieldFilter(t *testing.T) {
	t.Parallel()

	ff := FieldFilter{
		Field:    "name",
		Operator: OpEqual,
		Value:    "test",
	}

	filter := NewFieldFilter(ff)

	assert.True(t, filter.IsFieldFilter())
	assert.False(t, filter.IsAndFilter())
	assert.False(t, filter.IsOrFilter())
	assert.False(t, filter.IsNotFilter())

	assert.NotNil(t, filter.AsFieldFilter())
	assert.Nil(t, filter.AsAndFilter())
	assert.Nil(t, filter.AsOrFilter())
	assert.Nil(t, filter.AsNotFilter())

	assert.Equal(t, "name", filter.AsFieldFilter().Field)
	assert.Equal(t, OpEqual, filter.AsFieldFilter().Operator)
	assert.Equal(t, "test", filter.AsFieldFilter().Value)
}

func TestFilter_AndFilter(t *testing.T) {
	t.Parallel()

	andFilter := AndFilter{
		And: []Filter{
			NewFieldFilter(FieldFilter{Field: "name", Operator: OpEqual, Value: "test"}),
			NewFieldFilter(FieldFilter{Field: "age", Operator: OpGreater, Value: 18}),
		},
	}

	filter := NewAndFilter(andFilter)

	assert.False(t, filter.IsFieldFilter())
	assert.True(t, filter.IsAndFilter())
	assert.False(t, filter.IsOrFilter())
	assert.False(t, filter.IsNotFilter())

	assert.Nil(t, filter.AsFieldFilter())
	assert.NotNil(t, filter.AsAndFilter())
	assert.Nil(t, filter.AsOrFilter())
	assert.Nil(t, filter.AsNotFilter())

	assert.Len(t, filter.AsAndFilter().And, 2)
}

func TestFilter_OrFilter(t *testing.T) {
	t.Parallel()

	orFilter := OrFilter{
		Or: []Filter{
			NewFieldFilter(FieldFilter{Field: "status", Operator: OpEqual, Value: "active"}),
			NewFieldFilter(FieldFilter{Field: "status", Operator: OpEqual, Value: "pending"}),
		},
	}

	filter := NewOrFilter(orFilter)

	assert.False(t, filter.IsFieldFilter())
	assert.False(t, filter.IsAndFilter())
	assert.True(t, filter.IsOrFilter())
	assert.False(t, filter.IsNotFilter())

	assert.Len(t, filter.AsOrFilter().Or, 2)
}

func TestFilter_NotFilter(t *testing.T) {
	t.Parallel()

	notFilter := NotFilter{
		Not: NewFieldFilter(FieldFilter{Field: "deleted", Operator: OpIsNull}),
	}

	filter := NewNotFilter(notFilter)

	assert.False(t, filter.IsFieldFilter())
	assert.False(t, filter.IsAndFilter())
	assert.False(t, filter.IsOrFilter())
	assert.True(t, filter.IsNotFilter())

	assert.NotNil(t, filter.AsNotFilter())
	assert.True(t, filter.AsNotFilter().Not.IsFieldFilter())
}

func TestFilter_MarshalJSON_FieldFilter(t *testing.T) {
	t.Parallel()

	filter := NewFieldFilter(FieldFilter{
		Field:    "name",
		Operator: OpEqual,
		Value:    "test",
	})

	data, err := json.Marshal(filter)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "name", result["field"])
	assert.Equal(t, "=", result["operator"])
	assert.Equal(t, "test", result["value"])
}

func TestFilter_MarshalJSON_AndFilter(t *testing.T) {
	t.Parallel()

	filter := NewAndFilter(AndFilter{
		And: []Filter{
			NewFieldFilter(FieldFilter{Field: "name", Operator: OpEqual, Value: "test"}),
			NewFieldFilter(FieldFilter{Field: "age", Operator: OpGreater, Value: float64(18)}),
		},
	})

	data, err := json.Marshal(filter)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Contains(t, result, "and")
	andList := result["and"].([]interface{})
	assert.Len(t, andList, 2)
}

func TestFilter_UnmarshalJSON_FieldFilter(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"field": "name",
		"operator": "=",
		"value": "test"
	}`

	var filter Filter
	err := json.Unmarshal([]byte(jsonData), &filter)
	require.NoError(t, err)

	assert.True(t, filter.IsFieldFilter())
	assert.Equal(t, "name", filter.AsFieldFilter().Field)
	assert.Equal(t, OpEqual, filter.AsFieldFilter().Operator)
	assert.Equal(t, "test", filter.AsFieldFilter().Value)
}

func TestFilter_UnmarshalJSON_AndFilter(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"and": [
			{"field": "name", "operator": "=", "value": "test"},
			{"field": "age", "operator": ">", "value": 18}
		]
	}`

	var filter Filter
	err := json.Unmarshal([]byte(jsonData), &filter)
	require.NoError(t, err)

	assert.True(t, filter.IsAndFilter())
	assert.Len(t, filter.AsAndFilter().And, 2)
}

func TestFilter_UnmarshalJSON_OrFilter(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"or": [
			{"field": "status", "operator": "=", "value": "active"},
			{"field": "status", "operator": "=", "value": "pending"}
		]
	}`

	var filter Filter
	err := json.Unmarshal([]byte(jsonData), &filter)
	require.NoError(t, err)

	assert.True(t, filter.IsOrFilter())
	assert.Len(t, filter.AsOrFilter().Or, 2)
}

func TestFilter_UnmarshalJSON_NotFilter(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"not": {
			"field": "deleted",
			"operator": "IS NULL"
		}
	}`

	var filter Filter
	err := json.Unmarshal([]byte(jsonData), &filter)
	require.NoError(t, err)

	assert.True(t, filter.IsNotFilter())
	assert.True(t, filter.AsNotFilter().Not.IsFieldFilter())
	assert.Equal(t, "deleted", filter.AsNotFilter().Not.AsFieldFilter().Field)
}

func TestFilter_UnmarshalJSON_NestedFilters(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"and": [
			{"field": "name", "operator": "=", "value": "test"},
			{
				"or": [
					{"field": "age", "operator": ">", "value": 18},
					{"field": "verified", "operator": "=", "value": true}
				]
			}
		]
	}`

	var filter Filter
	err := json.Unmarshal([]byte(jsonData), &filter)
	require.NoError(t, err)

	assert.True(t, filter.IsAndFilter())
	assert.Len(t, filter.AsAndFilter().And, 2)

	// First filter is a field filter
	assert.True(t, filter.AsAndFilter().And[0].IsFieldFilter())

	// Second filter is an OR filter
	assert.True(t, filter.AsAndFilter().And[1].IsOrFilter())
	assert.Len(t, filter.AsAndFilter().And[1].AsOrFilter().Or, 2)
}

func TestQueryDefinition_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"from": "files",
		"fields": ["file_path", "language"],
		"where": {
			"field": "language",
			"operator": "=",
			"value": "go"
		},
		"limit": 10,
		"offset": 0
	}`

	var query QueryDefinition
	err := json.Unmarshal([]byte(jsonData), &query)
	require.NoError(t, err)

	assert.Equal(t, "files", query.From)
	assert.Equal(t, []string{"file_path", "language"}, query.Fields)
	assert.NotNil(t, query.Where)
	assert.True(t, query.Where.IsFieldFilter())
	assert.Equal(t, 10, *query.Limit)
	assert.Equal(t, 0, *query.Offset)
}

func TestQueryDefinition_ComplexQuery(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"from": "functions",
		"fields": ["name", "line_count"],
		"where": {
			"and": [
				{"field": "is_exported", "operator": "=", "value": 1},
				{"field": "line_count", "operator": ">", "value": 50}
			]
		},
		"joins": [
			{
				"table": "files",
				"type": "INNER",
				"on": {
					"field": "functions.file_path",
					"operator": "=",
					"value": "files.file_path"
				}
			}
		],
		"orderBy": [
			{"field": "line_count", "direction": "DESC"}
		],
		"limit": 20
	}`

	var query QueryDefinition
	err := json.Unmarshal([]byte(jsonData), &query)
	require.NoError(t, err)

	assert.Equal(t, "functions", query.From)
	assert.Len(t, query.Fields, 2)
	assert.NotNil(t, query.Where)
	assert.True(t, query.Where.IsAndFilter())
	assert.Len(t, query.Joins, 1)
	assert.Equal(t, "files", query.Joins[0].Table)
	assert.Equal(t, JoinInner, query.Joins[0].Type)
	assert.Len(t, query.OrderBy, 1)
	assert.Equal(t, "line_count", query.OrderBy[0].Field)
	assert.Equal(t, SortDesc, query.OrderBy[0].Direction)
	assert.Equal(t, 20, *query.Limit)
}

func TestQueryDefinition_WithAggregations(t *testing.T) {
	t.Parallel()

	lineCountField := "line_count_total"
	jsonData := `{
		"from": "files",
		"aggregations": [
			{"function": "COUNT", "alias": "total_files"},
			{"function": "SUM", "field": "line_count_total", "alias": "total_lines"},
			{"function": "AVG", "field": "line_count_code", "alias": "avg_lines", "distinct": true}
		],
		"groupBy": ["language"],
		"orderBy": [{"field": "language", "direction": "ASC"}]
	}`

	var query QueryDefinition
	err := json.Unmarshal([]byte(jsonData), &query)
	require.NoError(t, err)

	assert.Equal(t, "files", query.From)
	assert.Len(t, query.Aggregations, 3)

	// COUNT aggregation
	assert.Equal(t, AggCount, query.Aggregations[0].Function)
	assert.Nil(t, query.Aggregations[0].Field)
	assert.Equal(t, "total_files", query.Aggregations[0].Alias)

	// SUM aggregation
	assert.Equal(t, AggSum, query.Aggregations[1].Function)
	assert.Equal(t, &lineCountField, query.Aggregations[1].Field)
	assert.Equal(t, "total_lines", query.Aggregations[1].Alias)

	// AVG aggregation with DISTINCT
	assert.Equal(t, AggAvg, query.Aggregations[2].Function)
	assert.NotNil(t, query.Aggregations[2].Field)
	assert.True(t, query.Aggregations[2].Distinct)

	assert.Len(t, query.GroupBy, 1)
	assert.Equal(t, "language", query.GroupBy[0])
}
