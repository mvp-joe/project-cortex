package files

import (
	"fmt"

	sq "github.com/Masterminds/squirrel"
)

// BuildQuery translates a QueryDefinition into SQL using Squirrel.
// Returns SQL string, arguments, and error.
func BuildQuery(qd *QueryDefinition) (string, []interface{}, error) {
	// Validate query first
	if err := ValidateQuery(qd); err != nil {
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
	if qd.Limit > 0 {
		builder = builder.Limit(uint64(qd.Limit))
	}

	// Add OFFSET
	if qd.Offset > 0 {
		builder = builder.Offset(uint64(qd.Offset))
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
		ands := make([]sq.Sqlizer, 0, len(filter.And))
		for _, f := range filter.And {
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
		ors := make([]sq.Sqlizer, 0, len(filter.Or))
		for _, f := range filter.Or {
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
		clause, err := buildFilter(filter.Not)
		if err != nil {
			return nil, err
		}
		return sq.Expr("NOT (?)", clause), nil
	}

	return nil, fmt.Errorf("invalid filter type")
}

// buildFieldFilter translates a field filter to Squirrel.
func buildFieldFilter(filter *Filter) (sq.Sqlizer, error) {
	field := filter.Field
	op := filter.Operator
	value := filter.Value

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
func buildAggregation(agg Aggregation) string {
	var expr string

	switch agg.Function {
	case AggCount:
		if agg.Field == "" {
			expr = "COUNT(*)"
		} else if agg.Distinct {
			expr = fmt.Sprintf("COUNT(DISTINCT %s)", agg.Field)
		} else {
			expr = fmt.Sprintf("COUNT(%s)", agg.Field)
		}

	case AggSum:
		if agg.Distinct {
			expr = fmt.Sprintf("SUM(DISTINCT %s)", agg.Field)
		} else {
			expr = fmt.Sprintf("SUM(%s)", agg.Field)
		}

	case AggAvg:
		if agg.Distinct {
			expr = fmt.Sprintf("AVG(DISTINCT %s)", agg.Field)
		} else {
			expr = fmt.Sprintf("AVG(%s)", agg.Field)
		}

	case AggMin:
		expr = fmt.Sprintf("MIN(%s)", agg.Field)

	case AggMax:
		expr = fmt.Sprintf("MAX(%s)", agg.Field)

	default:
		// Should never reach here due to validation
		expr = "NULL"
	}

	return fmt.Sprintf("%s AS %s", expr, agg.Alias)
}
