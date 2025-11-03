package files

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateFilesToolHandler_ValidQuery tests successful query execution.
func TestCreateFilesToolHandler_ValidQuery(t *testing.T) {
	t.Parallel()

	// Setup test database
	db := setupTestDBForMCP(t)
	defer db.Close()

	// Create handler
	handler := CreateFilesToolHandler(db)

	// Build valid request
	queryDef := &QueryDefinition{
		Fields: []string{"file_path", "line_count_total"},
		From:   "files",
		Where: &Filter{
			Field:    "language",
			Operator: OpEqual,
			Value:    "Go",
		},
		Limit: 10,
	}

	queryJSON, err := json.Marshal(queryDef)
	require.NoError(t, err)

	// Create MCP request
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "cortex_files",
			Arguments: map[string]interface{}{
				"operation": "query",
				"query":     json.RawMessage(queryJSON),
			},
		},
	}

	// Execute handler
	ctx := context.Background()
	result, err := handler(ctx, request)

	// Verify success
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Content)
	require.Len(t, result.Content, 1)

	// Verify content is text
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected TextContent")

	// Parse response JSON
	var queryResult QueryResult
	err = json.Unmarshal([]byte(textContent.Text), &queryResult)
	require.NoError(t, err)

	// Verify structure
	assert.Equal(t, []string{"file_path", "line_count_total"}, queryResult.Columns)
	assert.Equal(t, 2, queryResult.RowCount) // We have 2 Go files in test data
	assert.Len(t, queryResult.Rows, 2)
	assert.Equal(t, "files", queryResult.Metadata.Source)
	assert.GreaterOrEqual(t, queryResult.Metadata.TookMs, int64(0))
}

// TestCreateFilesToolHandler_InvalidOperation tests unsupported operations.
func TestCreateFilesToolHandler_InvalidOperation(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	handler := CreateFilesToolHandler(db)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "cortex_files",
			Arguments: map[string]interface{}{
				"operation": "unsupported",
				"query":     map[string]interface{}{},
			},
		},
	}

	ctx := context.Background()
	result, err := handler(ctx, request)

	// Should return error result, not error
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check for error content
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)

	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "unsupported operation")
	assert.Contains(t, textContent.Text, "unsupported")
}

// TestCreateFilesToolHandler_MissingOperation tests missing operation field.
func TestCreateFilesToolHandler_MissingOperation(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	handler := CreateFilesToolHandler(db)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "cortex_files",
			Arguments: map[string]interface{}{
				"query": map[string]interface{}{},
			},
		},
	}

	ctx := context.Background()
	result, err := handler(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsError)

	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "operation field is required")
}

// TestCreateFilesToolHandler_MissingQuery tests missing query field.
func TestCreateFilesToolHandler_MissingQuery(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	handler := CreateFilesToolHandler(db)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "cortex_files",
			Arguments: map[string]interface{}{
				"operation": "query",
			},
		},
	}

	ctx := context.Background()
	result, err := handler(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsError)

	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "query field is required")
}

// TestCreateFilesToolHandler_MalformedJSON tests invalid JSON in arguments.
func TestCreateFilesToolHandler_MalformedJSON(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	handler := CreateFilesToolHandler(db)

	// Pass non-map arguments
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "cortex_files",
			Arguments: "not a map",
		},
	}

	ctx := context.Background()
	result, err := handler(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsError)

	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "invalid arguments format")
}

// TestCreateFilesToolHandler_InvalidQueryDefinition tests malformed query structure.
func TestCreateFilesToolHandler_InvalidQueryDefinition(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	handler := CreateFilesToolHandler(db)

	// Query with missing 'from' field (will fail validation during execute)
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "cortex_files",
			Arguments: map[string]interface{}{
				"operation": "query",
				"query": map[string]interface{}{
					"fields": []string{"file_path"},
					// Missing "from" field
				},
			},
		},
	}

	ctx := context.Background()
	_, err := handler(ctx, request)

	// Should return error (not error result) because execution failed
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query execution failed")
}

// TestCreateFilesToolHandler_DatabaseError tests handling of database errors.
func TestCreateFilesToolHandler_DatabaseError(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	handler := CreateFilesToolHandler(db)

	// Query with invalid table name
	queryDef := &QueryDefinition{
		Fields: []string{"*"},
		From:   "nonexistent_table",
	}

	queryJSON, err := json.Marshal(queryDef)
	require.NoError(t, err)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "cortex_files",
			Arguments: map[string]interface{}{
				"operation": "query",
				"query":     json.RawMessage(queryJSON),
			},
		},
	}

	ctx := context.Background()
	_, err = handler(ctx, request)

	// Database errors are returned as actual errors
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query execution failed")
}

// TestCreateFilesToolHandler_ComplexQuery tests a more complex query with joins and aggregations.
func TestCreateFilesToolHandler_ComplexQuery(t *testing.T) {
	t.Parallel()

	db := setupTestDBForMCP(t)
	defer db.Close()

	handler := CreateFilesToolHandler(db)

	// Count types grouped by kind (struct, interface, etc)
	queryDef := &QueryDefinition{
		From: "types",
		Aggregations: []Aggregation{
			{
				Function: AggCount,
				Alias:    "type_count",
			},
		},
		GroupBy: []string{"kind"},
		OrderBy: []OrderBy{
			{
				Field:     "type_count",
				Direction: SortDesc,
			},
		},
	}

	queryJSON, err := json.Marshal(queryDef)
	require.NoError(t, err)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "cortex_files",
			Arguments: map[string]interface{}{
				"operation": "query",
				"query":     json.RawMessage(queryJSON),
			},
		},
	}

	ctx := context.Background()
	result, err := handler(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	// Parse result
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var queryResult QueryResult
	err = json.Unmarshal([]byte(textContent.Text), &queryResult)
	require.NoError(t, err)

	// Verify aggregation worked
	assert.Greater(t, queryResult.RowCount, 0)
	assert.Contains(t, queryResult.Columns, "type_count")
}

// setupTestDBForMCP creates an in-memory SQLite database with test data for MCP handler tests.
func setupTestDBForMCP(t *testing.T) *sql.DB {
	t.Helper()

	// Use unique in-memory database for each test
	db, err := sql.Open("sqlite3", ":memory:?cache=shared")
	require.NoError(t, err)

	// Create schema matching schema.go definitions
	schema := `
	CREATE TABLE files (
		file_path TEXT PRIMARY KEY,
		language TEXT NOT NULL,
		module_path TEXT,
		is_test INTEGER NOT NULL,
		line_count_total INTEGER NOT NULL,
		line_count_code INTEGER NOT NULL,
		line_count_comment INTEGER NOT NULL,
		line_count_blank INTEGER NOT NULL,
		size_bytes INTEGER NOT NULL,
		file_hash TEXT NOT NULL,
		last_modified TEXT NOT NULL,
		indexed_at TEXT NOT NULL
	);

	CREATE TABLE types (
		type_id TEXT PRIMARY KEY,
		file_path TEXT NOT NULL,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		is_exported INTEGER NOT NULL,
		field_count INTEGER NOT NULL,
		method_count INTEGER NOT NULL
	);
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)

	// Insert test data
	testData := `
	INSERT INTO files VALUES
		('internal/files/executor.go', 'Go', 'github.com/mvp-joe/project-cortex', 0, 116, 96, 15, 5, 2158, 'abc123', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z'),
		('internal/files/types.go', 'Go', 'github.com/mvp-joe/project-cortex', 0, 119, 89, 20, 10, 3462, 'def456', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z');

	INSERT INTO types VALUES
		('executor_executor', 'internal/files/executor.go', 'Executor', 'struct', 10, 15, 1, 1, 0),
		('executor_queryresult', 'internal/files/executor.go', 'QueryResult', 'struct', 95, 101, 1, 4, 0),
		('types_filter', 'internal/files/types.go', 'Filter', 'struct', 51, 63, 1, 4, 0),
		('types_querydefinition', 'internal/files/types.go', 'QueryDefinition', 'struct', 106, 118, 1, 9, 0);
	`

	_, err = db.Exec(testData)
	require.NoError(t, err)

	return db
}
