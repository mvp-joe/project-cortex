// Package files provides SQL-queryable code statistics via JSON query definitions.
// It translates JSON query specifications into SQL queries using Squirrel,
// enabling LLM-friendly queries against the cortex SQLite database.
//
// The package follows the query schema defined in specs/2025-10-29_cortex-files.md
// and validates queries against the actual database schema in internal/storage/schema.go.
package files

import (
	"encoding/json"
	"fmt"
)

// ComparisonOperator represents SQL comparison operators for field filters.
type ComparisonOperator string

const (
	OpEqual        ComparisonOperator = "="
	OpNotEqual     ComparisonOperator = "!="
	OpGreater      ComparisonOperator = ">"
	OpGreaterEqual ComparisonOperator = ">="
	OpLess         ComparisonOperator = "<"
	OpLessEqual    ComparisonOperator = "<="
	OpLike         ComparisonOperator = "LIKE"
	OpNotLike      ComparisonOperator = "NOT LIKE"
	OpIn           ComparisonOperator = "IN"
	OpNotIn        ComparisonOperator = "NOT IN"
	OpIsNull       ComparisonOperator = "IS NULL"
	OpIsNotNull    ComparisonOperator = "IS NOT NULL"
	OpBetween      ComparisonOperator = "BETWEEN"
)

// IsValid checks if the comparison operator is valid.
func (op ComparisonOperator) IsValid() bool {
	switch op {
	case OpEqual, OpNotEqual, OpGreater, OpGreaterEqual, OpLess, OpLessEqual,
		OpLike, OpNotLike, OpIn, OpNotIn, OpIsNull, OpIsNotNull, OpBetween:
		return true
	default:
		return false
	}
}

// RequiresValue returns true if the operator requires a value parameter.
func (op ComparisonOperator) RequiresValue() bool {
	return op != OpIsNull && op != OpIsNotNull
}

// JoinType represents SQL join types.
type JoinType string

const (
	JoinInner JoinType = "INNER"
	JoinLeft  JoinType = "LEFT"
	JoinRight JoinType = "RIGHT"
	JoinFull  JoinType = "FULL"
)

// IsValid checks if the join type is valid.
func (jt JoinType) IsValid() bool {
	switch jt {
	case JoinInner, JoinLeft, JoinRight, JoinFull:
		return true
	default:
		return false
	}
}

// AggregationFunction represents SQL aggregation functions.
type AggregationFunction string

const (
	AggCount AggregationFunction = "COUNT"
	AggSum   AggregationFunction = "SUM"
	AggAvg   AggregationFunction = "AVG"
	AggMin   AggregationFunction = "MIN"
	AggMax   AggregationFunction = "MAX"
)

// IsValid checks if the aggregation function is valid.
func (af AggregationFunction) IsValid() bool {
	switch af {
	case AggCount, AggSum, AggAvg, AggMin, AggMax:
		return true
	default:
		return false
	}
}

// RequiresField returns true if the aggregation function requires a field parameter.
func (af AggregationFunction) RequiresField() bool {
	// COUNT can work without a field (COUNT(*))
	return af != AggCount
}

// SortDirection represents SQL sort order.
type SortDirection string

const (
	SortAsc  SortDirection = "ASC"
	SortDesc SortDirection = "DESC"
)

// IsValid checks if the sort direction is valid.
func (sd SortDirection) IsValid() bool {
	return sd == SortAsc || sd == SortDesc
}

// FieldFilter represents a field-level comparison filter.
type FieldFilter struct {
	Field    string             `json:"field"`
	Operator ComparisonOperator `json:"operator"`
	Value    interface{}        `json:"value,omitempty"`
}

// AndFilter represents a logical AND of multiple filters.
type AndFilter struct {
	And []Filter `json:"and"`
}

// OrFilter represents a logical OR of multiple filters.
type OrFilter struct {
	Or []Filter `json:"or"`
}

// NotFilter represents a logical NOT of a filter.
type NotFilter struct {
	Not Filter `json:"not"`
}

// Filter is a recursive filter type that can be a field filter or logical combination.
// It uses custom JSON marshaling/unmarshaling to handle the discriminated union.
type Filter struct {
	field *FieldFilter
	and   *AndFilter
	or    *OrFilter
	not   *NotFilter
}

// NewFieldFilter creates a Filter from a FieldFilter.
func NewFieldFilter(f FieldFilter) Filter {
	return Filter{field: &f}
}

// NewAndFilter creates a Filter from an AndFilter.
func NewAndFilter(f AndFilter) Filter {
	return Filter{and: &f}
}

// NewOrFilter creates a Filter from an OrFilter.
func NewOrFilter(f OrFilter) Filter {
	return Filter{or: &f}
}

// NewNotFilter creates a Filter from a NotFilter.
func NewNotFilter(f NotFilter) Filter {
	return Filter{not: &f}
}

// IsFieldFilter returns true if this is a field filter.
func (f Filter) IsFieldFilter() bool {
	return f.field != nil
}

// IsAndFilter returns true if this is an AND filter.
func (f Filter) IsAndFilter() bool {
	return f.and != nil
}

// IsOrFilter returns true if this is an OR filter.
func (f Filter) IsOrFilter() bool {
	return f.or != nil
}

// IsNotFilter returns true if this is a NOT filter.
func (f Filter) IsNotFilter() bool {
	return f.not != nil
}

// AsFieldFilter returns the FieldFilter if this is a field filter, or nil.
func (f Filter) AsFieldFilter() *FieldFilter {
	return f.field
}

// AsAndFilter returns the AndFilter if this is an AND filter, or nil.
func (f Filter) AsAndFilter() *AndFilter {
	return f.and
}

// AsOrFilter returns the OrFilter if this is an OR filter, or nil.
func (f Filter) AsOrFilter() *OrFilter {
	return f.or
}

// AsNotFilter returns the NotFilter if this is a NOT filter, or nil.
func (f Filter) AsNotFilter() *NotFilter {
	return f.not
}

// MarshalJSON implements custom JSON marshaling for Filter.
func (f Filter) MarshalJSON() ([]byte, error) {
	if f.field != nil {
		return json.Marshal(f.field)
	}
	if f.and != nil {
		return json.Marshal(f.and)
	}
	if f.or != nil {
		return json.Marshal(f.or)
	}
	if f.not != nil {
		return json.Marshal(f.not)
	}
	return nil, fmt.Errorf("empty filter")
}

// UnmarshalJSON implements custom JSON unmarshaling for Filter.
func (f *Filter) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a generic map to detect the type
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("filter must be a JSON object: %w", err)
	}

	// Check which type of filter this is based on keys
	if _, hasAnd := obj["and"]; hasAnd {
		var andFilter AndFilter
		if err := json.Unmarshal(data, &andFilter); err != nil {
			return fmt.Errorf("invalid AND filter: %w", err)
		}
		f.and = &andFilter
		return nil
	}

	if _, hasOr := obj["or"]; hasOr {
		var orFilter OrFilter
		if err := json.Unmarshal(data, &orFilter); err != nil {
			return fmt.Errorf("invalid OR filter: %w", err)
		}
		f.or = &orFilter
		return nil
	}

	if _, hasNot := obj["not"]; hasNot {
		var notFilter NotFilter
		if err := json.Unmarshal(data, &notFilter); err != nil {
			return fmt.Errorf("invalid NOT filter: %w", err)
		}
		f.not = &notFilter
		return nil
	}

	// Must be a field filter
	var fieldFilter FieldFilter
	if err := json.Unmarshal(data, &fieldFilter); err != nil {
		return fmt.Errorf("invalid field filter: %w", err)
	}
	f.field = &fieldFilter
	return nil
}

// Join represents a SQL JOIN clause.
type Join struct {
	Table string   `json:"table"`
	Type  JoinType `json:"type"`
	On    Filter   `json:"on"`
}

// Aggregation represents a SQL aggregation function.
type Aggregation struct {
	Function AggregationFunction `json:"function"`
	Field    *string             `json:"field,omitempty"`
	Alias    string              `json:"alias"`
	Distinct bool                `json:"distinct,omitempty"`
}

// OrderBy represents a SQL ORDER BY clause.
type OrderBy struct {
	Field     string        `json:"field"`
	Direction SortDirection `json:"direction"`
}

// QueryDefinition represents a complete SQL query in JSON form.
type QueryDefinition struct {
	Fields       []string       `json:"fields,omitempty"`
	From         string         `json:"from"`
	Where        *Filter        `json:"where,omitempty"`
	Joins        []Join         `json:"joins,omitempty"`
	GroupBy      []string       `json:"groupBy,omitempty"`
	Having       *Filter        `json:"having,omitempty"`
	OrderBy      []OrderBy      `json:"orderBy,omitempty"`
	Limit        *int           `json:"limit,omitempty"`
	Offset       *int           `json:"offset,omitempty"`
	Aggregations []Aggregation  `json:"aggregations,omitempty"`
}
