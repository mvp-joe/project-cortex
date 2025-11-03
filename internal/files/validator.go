package files

import (
	"fmt"
	"strings"
)

// ValidationError represents a validation error with context.
type ValidationError struct {
	Field   string // Field that failed validation
	Value   string // Invalid value
	Message string // Error message
	Hint    string // Helpful hint for fixing the error
}

// Error implements the error interface.
func (ve *ValidationError) Error() string {
	if ve.Hint != "" {
		return fmt.Sprintf("%s: %s (value: %q). %s", ve.Field, ve.Message, ve.Value, ve.Hint)
	}
	return fmt.Sprintf("%s: %s (value: %q)", ve.Field, ve.Message, ve.Value)
}

// ValidationErrors represents multiple validation errors.
type ValidationErrors []ValidationError

// Error implements the error interface.
func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return "no validation errors"
	}
	if len(ve) == 1 {
		return ve[0].Error()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d validation errors:\n", len(ve)))
	for i, err := range ve {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, err.Error()))
	}
	return sb.String()
}

// Add appends a validation error.
func (ve *ValidationErrors) Add(field, value, message, hint string) {
	*ve = append(*ve, ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
		Hint:    hint,
	})
}

// HasErrors returns true if there are validation errors.
func (ve ValidationErrors) HasErrors() bool {
	return len(ve) > 0
}

// Validator validates query definitions against the schema.
type Validator struct {
	registry *SchemaRegistry
}

// NewValidator creates a new validator with the schema registry.
func NewValidator() *Validator {
	return &Validator{
		registry: NewSchemaRegistry(),
	}
}

// Validate validates a complete query definition.
func (v *Validator) Validate(q *QueryDefinition) error {
	var errors ValidationErrors

	// Validate FROM table
	if q.From == "" {
		errors.Add("from", "", "from table is required", "Specify the table to query")
		// Return early if FROM is missing - can't validate other fields without it
		return errors
	} else if !v.registry.HasTable(q.From) {
		errors.Add("from", q.From, "unknown table", "Valid tables: files, types, type_fields, functions, function_parameters, type_relationships, function_calls, imports, chunks, modules, cache_metadata")
		// Return early if FROM is invalid - can't validate other fields without valid table
		return errors
	}

	// Validate fields reference valid columns
	if len(q.Fields) > 0 {
		fromTable, _ := v.registry.GetTable(q.From)
		for _, field := range q.Fields {
			if field == "*" {
				continue // Wildcard is always valid
			}
			if !fromTable.HasColumn(field) {
				errors.Add("fields", field, fmt.Sprintf("unknown column in table %s", q.From), "Check the table schema for valid columns")
			}
		}
	}

	// Validate WHERE filter
	if q.Where != nil {
		v.validateFilter(q.From, *q.Where, &errors)
	}

	// Validate JOINs
	for i, join := range q.Joins {
		if !join.Type.IsValid() {
			errors.Add(fmt.Sprintf("joins[%d].type", i), string(join.Type), "invalid join type", "Valid types: INNER, LEFT, RIGHT, FULL")
		}
		if !v.registry.HasTable(join.Table) {
			errors.Add(fmt.Sprintf("joins[%d].table", i), join.Table, "unknown table", "Valid tables: files, types, type_fields, functions, function_parameters, type_relationships, function_calls, imports, chunks, modules, cache_metadata")
		}
		// Validate ON condition (need to check both tables)
		v.validateJoinFilter(q.From, join.Table, join.On, i, &errors)
	}

	// Validate GROUP BY
	if len(q.GroupBy) > 0 {
		fromTable, _ := v.registry.GetTable(q.From)
		for _, field := range q.GroupBy {
			if !fromTable.HasColumn(field) {
				errors.Add("groupBy", field, fmt.Sprintf("unknown column in table %s", q.From), "Check the table schema for valid columns")
			}
		}
	}

	// Build set of available columns for ORDER BY and HAVING
	// Includes: base table columns, aggregation aliases, GROUP BY columns
	availableColumns := make(map[string]bool)
	fromTable, _ := v.registry.GetTable(q.From)
	for col := range fromTable.Columns {
		availableColumns[col] = true
	}
	for _, agg := range q.Aggregations {
		if agg.Alias != "" {
			availableColumns[agg.Alias] = true
		}
	}
	for _, col := range q.GroupBy {
		availableColumns[col] = true
	}

	// Validate HAVING filter (can reference aggregation aliases)
	if q.Having != nil {
		v.validateFilterWithAvailableColumns(q.From, *q.Having, availableColumns, "having", &errors)
	}

	// Validate ORDER BY (can reference aggregation aliases)
	for i, orderBy := range q.OrderBy {
		if !orderBy.Direction.IsValid() {
			errors.Add(fmt.Sprintf("orderBy[%d].direction", i), string(orderBy.Direction), "invalid sort direction", "Valid directions: ASC, DESC")
		}
		if !availableColumns[orderBy.Field] {
			errors.Add(fmt.Sprintf("orderBy[%d].field", i), orderBy.Field, fmt.Sprintf("unknown column in table %s", q.From), "Check the table schema for valid columns, aggregation aliases, or GROUP BY columns")
		}
	}

	// Validate LIMIT
	if q.Limit != nil {
		if *q.Limit < 1 || *q.Limit > 1000 {
			errors.Add("limit", fmt.Sprintf("%d", *q.Limit), "limit must be between 1 and 1000", "Adjust the limit value")
		}
	}

	// Validate OFFSET
	if q.Offset != nil {
		if *q.Offset < 0 {
			errors.Add("offset", fmt.Sprintf("%d", *q.Offset), "offset must be non-negative", "Set offset to 0 or greater")
		}
	}

	// Validate aggregations
	for i, agg := range q.Aggregations {
		if !agg.Function.IsValid() {
			errors.Add(fmt.Sprintf("aggregations[%d].function", i), string(agg.Function), "invalid aggregation function", "Valid functions: COUNT, SUM, AVG, MIN, MAX")
		}
		if agg.Function.RequiresField() && (agg.Field == nil || *agg.Field == "") {
			errors.Add(fmt.Sprintf("aggregations[%d].field", i), "", fmt.Sprintf("%s requires a field", agg.Function), "Specify the field to aggregate")
		}
		if agg.Field != nil && *agg.Field != "" {
			fromTable, _ := v.registry.GetTable(q.From)
			if !fromTable.HasColumn(*agg.Field) {
				errors.Add(fmt.Sprintf("aggregations[%d].field", i), *agg.Field, fmt.Sprintf("unknown column in table %s", q.From), "Check the table schema for valid columns")
			}
		}
		if agg.Alias == "" {
			errors.Add(fmt.Sprintf("aggregations[%d].alias", i), "", "aggregation alias is required", "Provide an alias for the aggregation result")
		}
	}

	if errors.HasErrors() {
		return errors
	}

	return nil
}

// validateFilter validates a filter against a table schema.
func (v *Validator) validateFilter(tableName string, filter Filter, errors *ValidationErrors) {
	if filter.IsFieldFilter() {
		v.validateFieldFilter(tableName, *filter.AsFieldFilter(), errors)
	} else if filter.IsAndFilter() {
		for _, f := range filter.AsAndFilter().And {
			v.validateFilter(tableName, f, errors)
		}
	} else if filter.IsOrFilter() {
		for _, f := range filter.AsOrFilter().Or {
			v.validateFilter(tableName, f, errors)
		}
	} else if filter.IsNotFilter() {
		v.validateFilter(tableName, filter.AsNotFilter().Not, errors)
	}
}

// validateFilterWithAvailableColumns validates a filter against a set of available columns.
// Used for HAVING and other contexts where aggregation aliases are valid.
func (v *Validator) validateFilterWithAvailableColumns(tableName string, filter Filter, availableColumns map[string]bool, context string, errors *ValidationErrors) {
	if filter.IsFieldFilter() {
		v.validateFieldFilterWithAvailableColumns(tableName, *filter.AsFieldFilter(), availableColumns, context, errors)
	} else if filter.IsAndFilter() {
		for _, f := range filter.AsAndFilter().And {
			v.validateFilterWithAvailableColumns(tableName, f, availableColumns, context, errors)
		}
	} else if filter.IsOrFilter() {
		for _, f := range filter.AsOrFilter().Or {
			v.validateFilterWithAvailableColumns(tableName, f, availableColumns, context, errors)
		}
	} else if filter.IsNotFilter() {
		v.validateFilterWithAvailableColumns(tableName, filter.AsNotFilter().Not, availableColumns, context, errors)
	}
}

// validateJoinFilter validates a filter in a JOIN ON clause (may reference two tables).
func (v *Validator) validateJoinFilter(table1, table2 string, filter Filter, joinIndex int, errors *ValidationErrors) {
	if filter.IsFieldFilter() {
		ff := filter.AsFieldFilter()
		// In JOIN ON, fields might be qualified (table.column) or not
		// For now, we'll just validate that unqualified fields exist in at least one table
		field := ff.Field

		table1Schema, _ := v.registry.GetTable(table1)
		table2Schema, _ := v.registry.GetTable(table2)

		// Check if field is qualified (table.column)
		if strings.Contains(field, ".") {
			parts := strings.SplitN(field, ".", 2)
			tableName, colName := parts[0], parts[1]

			if tableName != table1 && tableName != table2 {
				errors.Add(fmt.Sprintf("joins[%d].on.field", joinIndex), field,
					fmt.Sprintf("table %s not involved in this join", tableName),
					fmt.Sprintf("Join is between %s and %s", table1, table2))
				return
			}

			var tableSchema TableSchema
			if tableName == table1 {
				tableSchema = table1Schema
			} else {
				tableSchema = table2Schema
			}

			if !tableSchema.HasColumn(colName) {
				errors.Add(fmt.Sprintf("joins[%d].on.field", joinIndex), field,
					fmt.Sprintf("unknown column %s in table %s", colName, tableName),
					"Check the table schema for valid columns")
			}
		} else {
			// Unqualified field - must exist in at least one table
			if !table1Schema.HasColumn(field) && !table2Schema.HasColumn(field) {
				errors.Add(fmt.Sprintf("joins[%d].on.field", joinIndex), field,
					fmt.Sprintf("field not found in tables %s or %s", table1, table2),
					"Qualify the field with table name (e.g., table.field) or ensure it exists in one of the joined tables")
			}
		}

		// Validate operator
		if !ff.Operator.IsValid() {
			errors.Add(fmt.Sprintf("joins[%d].on.operator", joinIndex), string(ff.Operator),
				"invalid comparison operator",
				"Valid operators: =, !=, >, >=, <, <=, LIKE, NOT LIKE, IN, NOT IN, IS NULL, IS NOT NULL, BETWEEN")
		}

		// Validate value requirement
		if ff.Operator.RequiresValue() && ff.Value == nil {
			errors.Add(fmt.Sprintf("joins[%d].on.value", joinIndex), "",
				fmt.Sprintf("operator %s requires a value", ff.Operator),
				"Provide a value for the comparison")
		}
	} else if filter.IsAndFilter() {
		for _, f := range filter.AsAndFilter().And {
			v.validateJoinFilter(table1, table2, f, joinIndex, errors)
		}
	} else if filter.IsOrFilter() {
		for _, f := range filter.AsOrFilter().Or {
			v.validateJoinFilter(table1, table2, f, joinIndex, errors)
		}
	} else if filter.IsNotFilter() {
		v.validateJoinFilter(table1, table2, filter.AsNotFilter().Not, joinIndex, errors)
	}
}

// validateFieldFilter validates a field filter.
func (v *Validator) validateFieldFilter(tableName string, ff FieldFilter, errors *ValidationErrors) {
	// Validate operator
	if !ff.Operator.IsValid() {
		errors.Add("filter.operator", string(ff.Operator), "invalid comparison operator", "Valid operators: =, !=, >, >=, <, <=, LIKE, NOT LIKE, IN, NOT IN, IS NULL, IS NOT NULL, BETWEEN")
		return
	}

	// Validate field exists in table
	table, ok := v.registry.GetTable(tableName)
	if ok && !table.HasColumn(ff.Field) {
		errors.Add("filter.field", ff.Field, fmt.Sprintf("unknown column in table %s", tableName), "Check the table schema for valid columns")
	}

	// Validate value requirement
	if ff.Operator.RequiresValue() && ff.Value == nil {
		errors.Add("filter.value", "", fmt.Sprintf("operator %s requires a value", ff.Operator), "Provide a value for the comparison")
	}

	if !ff.Operator.RequiresValue() && ff.Value != nil {
		errors.Add("filter.value", fmt.Sprintf("%v", ff.Value), fmt.Sprintf("operator %s does not take a value", ff.Operator), "Remove the value parameter")
	}
}

// validateFieldFilterWithAvailableColumns validates a field filter with custom available columns.
// Used for HAVING and other contexts where aggregation aliases are valid.
func (v *Validator) validateFieldFilterWithAvailableColumns(tableName string, ff FieldFilter, availableColumns map[string]bool, context string, errors *ValidationErrors) {
	// Validate operator
	if !ff.Operator.IsValid() {
		errors.Add(fmt.Sprintf("%s.operator", context), string(ff.Operator), "invalid comparison operator", "Valid operators: =, !=, >, >=, <, <=, LIKE, NOT LIKE, IN, NOT IN, IS NULL, IS NOT NULL, BETWEEN")
		return
	}

	// Validate field exists in available columns (base table + aggregations + GROUP BY)
	if !availableColumns[ff.Field] {
		errors.Add(fmt.Sprintf("%s.field", context), ff.Field, fmt.Sprintf("unknown column in table %s", tableName), "Check the table schema for valid columns, aggregation aliases, or GROUP BY columns")
	}

	// Validate value requirement
	if ff.Operator.RequiresValue() && ff.Value == nil {
		errors.Add(fmt.Sprintf("%s.value", context), "", fmt.Sprintf("operator %s requires a value", ff.Operator), "Provide a value for the comparison")
	}

	if !ff.Operator.RequiresValue() && ff.Value != nil {
		errors.Add(fmt.Sprintf("%s.value", context), fmt.Sprintf("%v", ff.Value), fmt.Sprintf("operator %s does not take a value", ff.Operator), "Remove the value parameter")
	}
}
