package files_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/mvp-joe/project-cortex/internal/files"
)

// ExampleExecutor_Execute demonstrates basic usage of the Executor.
func ExampleExecutor_Execute() {
	// Open database connection
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create table and insert data
	_, _ = db.Exec(`
		CREATE TABLE files (
			file_path TEXT,
			language TEXT,
			line_count_code INTEGER
		)
	`)
	_, _ = db.Exec(`INSERT INTO files VALUES ('server.go', 'go', 210)`)
	_, _ = db.Exec(`INSERT INTO files VALUES ('main.go', 'go', 75)`)

	// Create executor
	executor := files.NewExecutor(db)

	// Define query
	whereFilter := files.NewFieldFilter(files.FieldFilter{
		Field:    "language",
		Operator: files.OpEqual,
		Value:    "go",
	})
	limit := 2
	query := &files.QueryDefinition{
		Fields: []string{"file_path", "line_count_code"},
		From:   "files",
		Where:  &whereFilter,
		OrderBy: []files.OrderBy{
			{
				Field:     "line_count_code",
				Direction: files.SortDesc,
			},
		},
		Limit: &limit,
	}

	// Execute query
	result, err := executor.Execute(query)
	if err != nil {
		log.Fatal(err)
	}

	// Marshal to JSON
	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(jsonBytes))

	// Output:
	// {
	//   "columns": [
	//     "file_path",
	//     "line_count_code"
	//   ],
	//   "rows": [
	//     [
	//       "server.go",
	//       210
	//     ],
	//     [
	//       "main.go",
	//       75
	//     ]
	//   ],
	//   "row_count": 2,
	//   "metadata": {
	//     "took_ms": 0,
	//     "query": "SELECT file_path, line_count_code FROM files WHERE language = ? ORDER BY line_count_code DESC LIMIT 2",
	//     "source": "files"
	//   }
	// }
}

// ExampleExecutor_Execute_aggregation demonstrates aggregation queries.
func ExampleExecutor_Execute_aggregation() {
	// Open database connection
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create table and insert data
	_, _ = db.Exec(`
		CREATE TABLE files (
			language TEXT,
			line_count_code INTEGER
		)
	`)
	_, _ = db.Exec(`INSERT INTO files VALUES ('go', 210)`)
	_, _ = db.Exec(`INSERT INTO files VALUES ('go', 160)`)
	_, _ = db.Exec(`INSERT INTO files VALUES ('typescript', 180)`)

	// Create executor
	executor := files.NewExecutor(db)

	// Define aggregation query
	fieldName := "line_count_code"
	query := &files.QueryDefinition{
		From:    "files",
		GroupBy: []string{"language"},
		Aggregations: []files.Aggregation{
			{
				Function: files.AggCount,
				Alias:    "file_count",
			},
			{
				Function: files.AggSum,
				Field:    &fieldName,
				Alias:    "total_lines",
			},
		},
		OrderBy: []files.OrderBy{
			{
				Field:     "total_lines",
				Direction: files.SortDesc,
			},
		},
	}

	// Execute query
	result, err := executor.Execute(query)
	if err != nil {
		log.Fatal(err)
	}

	// Marshal to JSON
	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(jsonBytes))

	// Output:
	// {
	//   "columns": [
	//     "language",
	//     "file_count",
	//     "total_lines"
	//   ],
	//   "rows": [
	//     [
	//       "go",
	//       2,
	//       370
	//     ],
	//     [
	//       "typescript",
	//       1,
	//       180
	//     ]
	//   ],
	//   "row_count": 2,
	//   "metadata": {
	//     "took_ms": 0,
	//     "query": "SELECT language, COUNT(*) AS file_count, SUM(line_count_code) AS total_lines FROM files GROUP BY language ORDER BY total_lines DESC",
	//     "source": "files"
	//   }
	// }
}
