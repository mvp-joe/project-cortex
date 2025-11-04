package files

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan:
// 1. Test simple SELECT query execution
// 2. Test aggregation queries (COUNT, SUM, AVG)
// 3. Test empty result sets
// 4. Test NULL value handling
// 5. Test various data types (int, string, bool, float)
// 6. Test timing measurement
// 7. Test error cases (closed DB, invalid SQL)
// 8. Test query result JSON marshaling

// setupTestDB creates an in-memory SQLite database with test data.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Create test table with various data types
	_, err = db.Exec(`
		CREATE TABLE files (
			file_path TEXT PRIMARY KEY,
			language TEXT,
			module_path TEXT,
			is_test INTEGER,
			line_count_total INTEGER,
			line_count_code INTEGER,
			line_count_comment INTEGER,
			line_count_blank INTEGER,
			size_bytes INTEGER
		)
	`)
	require.NoError(t, err)

	// Insert test data
	testData := []struct {
		path         string
		lang         string
		module       string
		isTest       int
		linesTotal   int
		linesCode    int
		linesComment int
		linesBlank   int
		sizeBytes    int
	}{
		{"internal/mcp/server.go", "go", "internal/mcp", 0, 245, 210, 20, 15, 8192},
		{"internal/mcp/server_test.go", "go", "internal/mcp", 1, 156, 140, 10, 6, 5120},
		{"internal/files/query.go", "go", "internal/files", 0, 180, 160, 15, 5, 6144},
		{"internal/files/executor.go", "go", "internal/files", 0, 120, 105, 10, 5, 4096},
		{"cmd/cortex/main.go", "go", "cmd/cortex", 0, 89, 75, 8, 6, 2048},
	}

	for _, td := range testData {
		_, err = db.Exec(`
			INSERT INTO files (file_path, language, module_path, is_test, line_count_total, line_count_code, line_count_comment, line_count_blank, size_bytes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, td.path, td.lang, td.module, td.isTest, td.linesTotal, td.linesCode, td.linesComment, td.linesBlank, td.sizeBytes)
		require.NoError(t, err)
	}

	return db
}

func TestExecutor_SimpleSelect(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Query: SELECT file_path, language FROM files WHERE language = 'go' LIMIT 2
	whereFilter := NewFieldFilter(FieldFilter{
		Field:    "language",
		Operator: OpEqual,
		Value:    "go",
	})
	limit := 2
	qd := &QueryDefinition{
		Fields: []string{"file_path", "language"},
		From:   "files",
		Where:  &whereFilter,
		Limit:  &limit,
	}

	result, err := executor.Execute(qd)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify columns
	assert.Equal(t, []string{"file_path", "language"}, result.Columns)

	// Verify row count
	assert.Equal(t, 2, result.RowCount)
	assert.Len(t, result.Rows, 2)

	// Verify data types and values
	for _, row := range result.Rows {
		assert.Len(t, row, 2)
		assert.IsType(t, "", row[0]) // file_path is string
		assert.IsType(t, "", row[1]) // language is string
		assert.Equal(t, "go", row[1])
	}

	// Verify metadata
	assert.GreaterOrEqual(t, result.Metadata.TookMs, int64(0))
	assert.NotEmpty(t, result.Metadata.Query)
	assert.Equal(t, "files", result.Metadata.Source)
}

func TestExecutor_Aggregation(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Query: SELECT language, COUNT(*) AS file_count, SUM(line_count_code) AS total_lines
	//        FROM files GROUP BY language
	fieldName := "line_count_code"
	qd := &QueryDefinition{
		From:    "files",
		GroupBy: []string{"language"},
		Aggregations: []Aggregation{
			{
				Function: AggCount,
				Alias:    "file_count",
			},
			{
				Function: AggSum,
				Field:    &fieldName,
				Alias:    "total_lines",
			},
		},
	}

	result, err := executor.Execute(qd)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify columns (SQLite returns the AS aliases)
	assert.Equal(t, []string{"language", "file_count", "total_lines"}, result.Columns)

	// Verify row count (should be 1 row for "go")
	assert.Equal(t, 1, result.RowCount)
	assert.Len(t, result.Rows, 1)

	// Verify aggregation results
	row := result.Rows[0]
	assert.Equal(t, "go", row[0])
	assert.Equal(t, int64(5), row[1])   // COUNT(*)
	assert.Equal(t, int64(690), row[2]) // SUM(line_count_code) = 210+140+160+105+75
}

func TestExecutor_EmptyResultSet(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Query: SELECT * FROM files WHERE language = 'python' (no python files)
	whereFilter := NewFieldFilter(FieldFilter{
		Field:    "language",
		Operator: OpEqual,
		Value:    "python",
	})
	qd := &QueryDefinition{
		Fields: []string{"*"},
		From:   "files",
		Where:  &whereFilter,
	}

	result, err := executor.Execute(qd)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify empty result
	assert.Equal(t, 0, result.RowCount)
	assert.Empty(t, result.Rows)
	assert.NotEmpty(t, result.Columns) // Columns should still be populated
}

func TestExecutor_NullValues(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	// Add row with NULL module_path
	_, err := db.Exec(`
		INSERT INTO files (file_path, language, module_path, is_test, line_count_total, line_count_code, line_count_comment, line_count_blank, size_bytes)
		VALUES (?, ?, NULL, ?, ?, ?, ?, ?, ?)
	`, "scripts/build.sh", "shell", 0, 50, 45, 3, 2, 1024)
	require.NoError(t, err)

	executor := NewExecutor(db)

	// Query: SELECT file_path, module_path FROM files WHERE language = 'shell'
	whereFilter := NewFieldFilter(FieldFilter{
		Field:    "language",
		Operator: OpEqual,
		Value:    "shell",
	})
	qd := &QueryDefinition{
		Fields: []string{"file_path", "module_path"},
		From:   "files",
		Where:  &whereFilter,
	}

	result, err := executor.Execute(qd)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 1, result.RowCount)
	assert.Len(t, result.Rows, 1)

	// Verify NULL is represented as nil
	row := result.Rows[0]
	assert.Equal(t, "scripts/build.sh", row[0])
	assert.Nil(t, row[1]) // NULL module_path
}

func TestExecutor_VariousDataTypes(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Query: SELECT file_path, is_test, line_count_total, size_bytes FROM files LIMIT 1
	limit := 1
	qd := &QueryDefinition{
		Fields: []string{"file_path", "is_test", "line_count_total", "size_bytes"},
		From:   "files",
		Limit:  &limit,
	}

	result, err := executor.Execute(qd)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 1, result.RowCount)
	row := result.Rows[0]

	// Verify SQLite type mapping
	assert.IsType(t, "", row[0])       // TEXT -> string
	assert.IsType(t, int64(0), row[1]) // INTEGER -> int64
	assert.IsType(t, int64(0), row[2]) // INTEGER -> int64
	assert.IsType(t, int64(0), row[3]) // INTEGER -> int64
}

func TestExecutor_TimingMeasurement(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Query: SELECT * FROM files
	qd := &QueryDefinition{
		Fields: []string{"*"},
		From:   "files",
	}

	result, err := executor.Execute(qd)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Timing should be measured (>= 0ms)
	assert.GreaterOrEqual(t, result.Metadata.TookMs, int64(0))

	// For in-memory queries, timing should be very fast (typically < 10ms)
	assert.Less(t, result.Metadata.TookMs, int64(100))
}

func TestExecutor_ErrorInvalidSQL(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Query with invalid field name (will fail validation)
	qd := &QueryDefinition{
		Fields: []string{"nonexistent_field"},
		From:   "files",
	}

	_, err := executor.Execute(qd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query build failed")
}

func TestExecutor_ErrorClosedDB(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	db.Close() // Close before executing

	executor := NewExecutor(db)

	qd := &QueryDefinition{
		Fields: []string{"*"},
		From:   "files",
	}

	_, err := executor.Execute(qd)
	assert.Error(t, err)
}

func TestExecutor_ErrorQueryBuildFailure(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Invalid query: missing FROM
	qd := &QueryDefinition{
		Fields: []string{"*"},
		From:   "", // Empty FROM
	}

	_, err := executor.Execute(qd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query build failed")
}

func TestQueryResult_JSONMarshaling(t *testing.T) {
	t.Parallel()

	result := &QueryResult{
		Columns: []string{"file_path", "language", "lines"},
		Rows: [][]interface{}{
			{"internal/mcp/server.go", "go", int64(245)},
			{"cmd/cortex/main.go", "go", int64(89)},
		},
		RowCount: 2,
		Metadata: QueryMetadata{
			TookMs: 3,
			Query:  "SELECT file_path, language, lines FROM files LIMIT 2",
			Source: "files",
		},
	}

	// Marshal to JSON
	jsonBytes, err := result.MarshalJSON()
	require.NoError(t, err)

	// Verify JSON structure
	jsonStr := string(jsonBytes)
	assert.Contains(t, jsonStr, `"columns"`)
	assert.Contains(t, jsonStr, `"rows"`)
	assert.Contains(t, jsonStr, `"row_count":2`)
	assert.Contains(t, jsonStr, `"metadata"`)
	assert.Contains(t, jsonStr, `"took_ms":3`)
	assert.Contains(t, jsonStr, `"source":"files"`)

	// Verify array structure for heterogeneous types
	assert.Contains(t, jsonStr, `"internal/mcp/server.go"`)
	assert.Contains(t, jsonStr, `245`)
}

func TestExecutor_OrderByAndLimit(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Query: SELECT file_path, line_count_code FROM files ORDER BY line_count_code DESC LIMIT 3
	limit := 3
	qd := &QueryDefinition{
		Fields: []string{"file_path", "line_count_code"},
		From:   "files",
		OrderBy: []OrderBy{
			{
				Field:     "line_count_code",
				Direction: SortDesc,
			},
		},
		Limit: &limit,
	}

	result, err := executor.Execute(qd)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 3, result.RowCount)

	// Verify descending order
	assert.Equal(t, int64(210), result.Rows[0][1]) // server.go
	assert.Equal(t, int64(160), result.Rows[1][1]) // query.go
	assert.Equal(t, int64(140), result.Rows[2][1]) // server_test.go
}
