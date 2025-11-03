package files_test

import (
	"fmt"
	"log"

	"github.com/mvp-joe/project-cortex/internal/files"
)

// ExampleBuildQuery_SimpleSelect demonstrates a simple SELECT query.
func ExampleBuildQuery_simpleSelect() {
	qd := &files.QueryDefinition{
		From: "files",
		Where: &files.Filter{
			Field:    "language",
			Operator: files.OpEqual,
			Value:    "go",
		},
		OrderBy: []files.OrderBy{
			{Field: "line_count_total", Direction: files.SortDesc},
		},
		Limit: 10,
	}

	sql, args, err := files.BuildQuery(qd)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(sql)
	fmt.Println(args)
	// Output:
	// SELECT * FROM files WHERE language = ? ORDER BY line_count_total DESC LIMIT 10
	// [go]
}

// ExampleBuildQuery_Aggregation demonstrates an aggregation query with GROUP BY.
func ExampleBuildQuery_aggregation() {
	qd := &files.QueryDefinition{
		From:    "files",
		GroupBy: []string{"language"},
		Aggregations: []files.Aggregation{
			{Function: files.AggCount, Alias: "file_count"},
			{Function: files.AggSum, Field: "line_count_total", Alias: "total_lines"},
		},
		OrderBy: []files.OrderBy{
			{Field: "total_lines", Direction: files.SortDesc},
		},
	}

	sql, args, err := files.BuildQuery(qd)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(sql)
	fmt.Println(args)
	// Output:
	// SELECT language, COUNT(*) AS file_count, SUM(line_count_total) AS total_lines FROM files GROUP BY language ORDER BY total_lines DESC
	// []
}

// ExampleBuildQuery_ComplexFilter demonstrates complex nested filters.
func ExampleBuildQuery_complexFilter() {
	qd := &files.QueryDefinition{
		From: "files",
		Where: &files.Filter{
			And: []files.Filter{
				{Field: "language", Operator: files.OpEqual, Value: "go"},
				{
					Or: []files.Filter{
						{Field: "line_count_total", Operator: files.OpGreater, Value: 100},
						{Field: "is_test", Operator: files.OpEqual, Value: true},
					},
				},
			},
		},
		Limit: 10,
	}

	sql, args, err := files.BuildQuery(qd)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(sql)
	fmt.Println(args)
	// Output:
	// SELECT * FROM files WHERE (language = ? AND (line_count_total > ? OR is_test = ?)) LIMIT 10
	// [go 100 true]
}
