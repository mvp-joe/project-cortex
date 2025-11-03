package files

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Executor executes QueryDefinitions against a SQLite database.
// It translates queries to SQL, executes them, and returns structured results.
type Executor struct {
	db *sql.DB
}

// NewExecutor creates an Executor that uses the provided database connection.
// The database connection is not owned by the executor and will not be closed.
func NewExecutor(db *sql.DB) *Executor {
	return &Executor{db: db}
}

// Execute runs a QueryDefinition and returns structured results with metadata.
// Returns an error if query building or execution fails.
func (e *Executor) Execute(qd *QueryDefinition) (*QueryResult, error) {
	// Build SQL from query definition
	sqlQuery, args, err := BuildQuery(qd)
	if err != nil {
		return nil, fmt.Errorf("query build failed: %w", err)
	}

	// Measure execution time
	start := time.Now()

	// Execute query
	rows, err := e.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// Extract column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column names: %w", err)
	}

	// Scan rows into generic structure
	rowData := make([][]interface{}, 0)
	for rows.Next() {
		// Create slice of interface{} for scanning
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan row
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert []byte to string (SQLite TEXT columns)
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}

		rowData = append(rowData, values)
	}

	// Check for iteration errors
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Calculate execution time
	tookMs := time.Since(start).Milliseconds()

	// Build result
	result := &QueryResult{
		Columns:  columns,
		Rows:     rowData,
		RowCount: len(rowData),
		Metadata: QueryMetadata{
			TookMs: tookMs,
			Query:  sqlQuery,
			Source: "files",
		},
	}

	return result, nil
}

// QueryResult represents the structured result of a query execution.
type QueryResult struct {
	Columns  []string        `json:"columns"`
	Rows     [][]interface{} `json:"rows"`
	RowCount int             `json:"row_count"`
	Metadata QueryMetadata   `json:"metadata"`
}

// QueryMetadata contains execution metadata for a query result.
type QueryMetadata struct {
	TookMs int64  `json:"took_ms"` // Execution time in milliseconds
	Query  string `json:"query"`   // Generated SQL (for debugging)
	Source string `json:"source"`  // Always "files"
}

// MarshalJSON implements custom JSON marshaling for QueryResult.
// This ensures proper handling of heterogeneous row data types.
func (qr *QueryResult) MarshalJSON() ([]byte, error) {
	type Alias QueryResult
	return json.Marshal((*Alias)(qr))
}
