package files

import (
	"fmt"
	"unicode"

	sq "github.com/Masterminds/squirrel"
)

// BuildQuery translates a QueryDefinition into SQL using Squirrel.
// Returns SQL string, arguments, and error.
func BuildQuery(qd *QueryDefinition) (string, []interface{}, error) {
	// Validate query first
	validator := NewValidator()
	if err := validator.Validate(qd); err != nil {
		return "", nil, fmt.Errorf("query validation failed: %w", err)
	}

	// Start with SELECT fields
	fields := qd.Fields
	if len(fields) == 0 {
		fields = []string{"*"}
	}

	// If aggregations are present, build aggregation fields
	if len(qd.Aggregations) > 0 {
		aggFields := make([]string, 0, len(qd.GroupBy)+len(qd.Aggregations))
		// Add GROUP BY fields first
		aggFields = append(aggFields, qd.GroupBy...)
		// Add aggregation expressions
		for _, agg := range qd.Aggregations {
			aggFields = append(aggFields, buildAggregation(agg))
		}
		fields = aggFields
	}

	builder := sq.Select(fields...).From(qd.From)

	// Add WHERE clause
	if qd.Where != nil {
		whereClause, err := buildFilter(qd.Where)
		if err != nil {
			return "", nil, fmt.Errorf("failed to build WHERE clause: %w", err)
		}
		builder = builder.Where(whereClause)
	}

	// Add JOINs
	for _, join := range qd.Joins {
		var err error
		builder, err = buildJoin(join, builder)
		if err != nil {
			return "", nil, fmt.Errorf("failed to build JOIN: %w", err)
		}
	}

	// Add GROUP BY
	if len(qd.GroupBy) > 0 {
		builder = builder.GroupBy(qd.GroupBy...)
	}

	// Add HAVING
	if qd.Having != nil {
		havingClause, err := buildFilter(qd.Having)
		if err != nil {
			return "", nil, fmt.Errorf("failed to build HAVING clause: %w", err)
		}
		builder = builder.Having(havingClause)
	}

	// Add ORDER BY
	for _, order := range qd.OrderBy {
		builder = builder.OrderBy(fmt.Sprintf("%s %s", order.Field, order.Direction))
	}

	// Add LIMIT
	if qd.Limit != nil && *qd.Limit > 0 {
		builder = builder.Limit(uint64(*qd.Limit))
	}

	// Add OFFSET
	if qd.Offset != nil && *qd.Offset > 0 {
		builder = builder.Offset(uint64(*qd.Offset))
	}

	// Generate SQL with SQLite placeholders
	sql, args, err := builder.PlaceholderFormat(sq.Question).ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate SQL: %w", err)
	}

	return sql, args, nil
}

// buildFilter translates a Filter to a Squirrel Sqlizer.
func buildFilter(filter *Filter) (sq.Sqlizer, error) {
	if filter == nil {
		return nil, fmt.Errorf("filter cannot be nil")
	}

	// Handle FieldFilter
	if filter.IsFieldFilter() {
		return buildFieldFilter(filter)
	}

	// Handle AndFilter
	if filter.IsAndFilter() {
		andFilter := filter.AsAndFilter()
		ands := make([]sq.Sqlizer, 0, len(andFilter.And))
		for _, f := range andFilter.And {
			clause, err := buildFilter(&f)
			if err != nil {
				return nil, err
			}
			ands = append(ands, clause)
		}
		return sq.And(ands), nil
	}

	// Handle OrFilter
	if filter.IsOrFilter() {
		orFilter := filter.AsOrFilter()
		ors := make([]sq.Sqlizer, 0, len(orFilter.Or))
		for _, f := range orFilter.Or {
			clause, err := buildFilter(&f)
			if err != nil {
				return nil, err
			}
			ors = append(ors, clause)
		}
		return sq.Or(ors), nil
	}

	// Handle NotFilter
	if filter.IsNotFilter() {
		notFilter := filter.AsNotFilter()
		clause, err := buildFilter(&notFilter.Not)
		if err != nil {
			return nil, err
		}
		return sq.Expr("NOT (?)", clause), nil
	}

	return nil, fmt.Errorf("invalid filter type")
}

// buildFieldFilter translates a field filter to Squirrel.
func buildFieldFilter(filter *Filter) (sq.Sqlizer, error) {
	fieldFilter := filter.AsFieldFilter()
	if fieldFilter == nil {
		return nil, fmt.Errorf("expected field filter")
	}
	field := fieldFilter.Field
	op := fieldFilter.Operator
	value := fieldFilter.Value

	switch op {
	case OpEqual:
		return sq.Eq{field: value}, nil

	case OpNotEqual:
		return sq.NotEq{field: value}, nil

	case OpGreater:
		return sq.Gt{field: value}, nil

	case OpGreaterEqual:
		return sq.GtOrEq{field: value}, nil

	case OpLess:
		return sq.Lt{field: value}, nil

	case OpLessEqual:
		return sq.LtOrEq{field: value}, nil

	case OpLike:
		return sq.Like{field: value}, nil

	case OpNotLike:
		return sq.NotLike{field: value}, nil

	case OpIn:
		// Squirrel auto-detects IN when value is array
		return sq.Eq{field: value}, nil

	case OpNotIn:
		// Squirrel auto-detects NOT IN when value is array
		return sq.NotEq{field: value}, nil

	case OpIsNull:
		return sq.Eq{field: nil}, nil

	case OpIsNotNull:
		return sq.NotEq{field: nil}, nil

	case OpBetween:
		// BETWEEN is translated to: field >= min AND field <= max
		vals, ok := value.([]interface{})
		if !ok || len(vals) != 2 {
			return nil, fmt.Errorf("BETWEEN requires array of 2 values")
		}
		return sq.And{
			sq.GtOrEq{field: vals[0]},
			sq.LtOrEq{field: vals[1]},
		}, nil

	default:
		return nil, fmt.Errorf("unknown operator: %s", op)
	}
}

// buildJoin adds a JOIN clause to the query builder.
func buildJoin(join Join, builder sq.SelectBuilder) (sq.SelectBuilder, error) {
	// Build the ON clause
	onClause, err := buildFilter(&join.On)
	if err != nil {
		return builder, fmt.Errorf("failed to build ON clause: %w", err)
	}

	// Format: "table ON condition"
	// Squirrel expects the condition as a Sqlizer, so we use Expr with placeholder
	joinExpr := fmt.Sprintf("%s ON (?)", join.Table)

	switch join.Type {
	case JoinInner:
		return builder.InnerJoin(joinExpr, onClause), nil
	case JoinLeft:
		return builder.LeftJoin(joinExpr, onClause), nil
	case JoinRight:
		return builder.RightJoin(joinExpr, onClause), nil
	case JoinFull:
		// SQLite doesn't support FULL OUTER JOIN, but we'll generate it anyway
		// The executor will handle the error
		return builder.Join(fmt.Sprintf("FULL OUTER JOIN %s ON (?)", join.Table), onClause), nil
	default:
		return builder, fmt.Errorf("unknown join type: %s", join.Type)
	}
}

// buildAggregation builds an aggregation expression string.
// Returns "FUNCTION(field) AS alias" or "COUNT(*) AS alias"
//
// SECURITY: This function constructs SQL strings directly. Field name validation
// MUST be performed by the validator BEFORE this function is called to prevent
// SQL injection. The BuildQuery function enforces this by validating first.
func buildAggregation(agg Aggregation) string {
	var expr string
	fieldName := ""
	if agg.Field != nil {
		fieldName = *agg.Field

		// CRITICAL: Runtime assertion that field name is valid SQL identifier.
		// This is defense-in-depth - validation should have already occurred.
		if fieldName != "" && !IsValidSQLIdentifier(fieldName) {
			// Panic here because this indicates a programming error:
			// either validation was skipped or bypassed.
			panic(fmt.Sprintf("SECURITY: invalid SQL identifier in aggregation field: %q - validation should have caught this", fieldName))
		}
	}

	// Runtime assertion for alias (also validated earlier, but critical for SQL safety)
	if !IsValidSQLIdentifier(agg.Alias) {
		panic(fmt.Sprintf("SECURITY: invalid SQL identifier in aggregation alias: %q - validation should have caught this", agg.Alias))
	}

	switch agg.Function {
	case AggCount:
		if fieldName == "" {
			expr = "COUNT(*)"
		} else if agg.Distinct {
			expr = fmt.Sprintf("COUNT(DISTINCT %s)", fieldName)
		} else {
			expr = fmt.Sprintf("COUNT(%s)", fieldName)
		}

	case AggSum:
		if agg.Distinct {
			expr = fmt.Sprintf("SUM(DISTINCT %s)", fieldName)
		} else {
			expr = fmt.Sprintf("SUM(%s)", fieldName)
		}

	case AggAvg:
		if agg.Distinct {
			expr = fmt.Sprintf("AVG(DISTINCT %s)", fieldName)
		} else {
			expr = fmt.Sprintf("AVG(%s)", fieldName)
		}

	case AggMin:
		expr = fmt.Sprintf("MIN(%s)", fieldName)

	case AggMax:
		expr = fmt.Sprintf("MAX(%s)", fieldName)

	default:
		// Should never reach here due to validation
		expr = "NULL"
	}

	return fmt.Sprintf("%s AS %s", expr, agg.Alias)
}

// IsValidSQLIdentifier checks if a string is a valid SQL identifier.
// Valid identifiers must:
// - Start with a letter (a-z, A-Z) or underscore (_)
// - Contain only letters, digits, or underscores
// - Be non-empty
//
// This prevents SQL injection by ensuring field names cannot contain
// special characters like quotes, semicolons, or SQL keywords.
func IsValidSQLIdentifier(s string) bool {
	if s == "" {
		return false
	}

	// Must start with letter or underscore
	firstRune := rune(s[0])
	if !unicode.IsLetter(firstRune) && firstRune != '_' {
		return false
	}

	// Rest must be alphanumeric or underscore
	for _, r := range s[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}

	return true
}
