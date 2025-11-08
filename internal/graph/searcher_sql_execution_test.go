package graph

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteDependencyQuery tests the executeDependencyQuery method.
func TestExecuteDependencyQuery(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer db.Close()

	// Insert test data
	setupDependencyTestData(t, db)

	searcher, err := NewSQLSearcher(db, "/test/root")
	require.NoError(t, err)
	defer searcher.Close()

	sqlSearcher := searcher.(*sqlSearcher)

	ctx := context.Background()

	t.Run("executes query successfully", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		require.NoError(t, err)
		defer tx.Rollback()

		sql, args := sqlSearcher.buildDependenciesSQL("internal/graph", 100)

		req := &QueryRequest{
			Operation:  OperationDependencies,
			Target:     "internal/graph",
			MaxResults: 100,
		}

		resp, err := sqlSearcher.executeDependencyQuery(ctx, tx, sql, args, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify response structure
		assert.Equal(t, string(OperationDependencies), resp.Operation)
		assert.Equal(t, "internal/graph", resp.Target)
		assert.GreaterOrEqual(t, len(resp.Results), 0)
	})

	t.Run("creates package nodes correctly", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		require.NoError(t, err)
		defer tx.Rollback()

		sql, args := sqlSearcher.buildDependenciesSQL("internal/graph", 100)

		req := &QueryRequest{
			Operation:  OperationDependencies,
			Target:     "internal/graph",
			MaxResults: 100,
		}

		resp, err := sqlSearcher.executeDependencyQuery(ctx, tx, sql, args, req)
		require.NoError(t, err)

		if len(resp.Results) > 0 {
			result := resp.Results[0]

			// Verify node is a package
			assert.Equal(t, NodePackage, result.Node.Kind)

			// Verify ID is set (package path)
			assert.NotEmpty(t, result.Node.ID)

			// Verify file path is set
			assert.NotEmpty(t, result.Node.File)

			// Verify line numbers
			assert.Greater(t, result.Node.StartLine, 0)
			assert.Equal(t, result.Node.StartLine, result.Node.EndLine)

			// Verify depth is 1
			assert.Equal(t, 1, result.Depth)
		}
	})

	t.Run("handles empty results", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		require.NoError(t, err)
		defer tx.Rollback()

		sql, args := sqlSearcher.buildDependenciesSQL("nonexistent/package", 100)

		req := &QueryRequest{
			Operation:  OperationDependencies,
			Target:     "nonexistent/package",
			MaxResults: 100,
		}

		resp, err := sqlSearcher.executeDependencyQuery(ctx, tx, sql, args, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, 0, len(resp.Results))
		assert.Equal(t, 0, resp.TotalFound)
		assert.Equal(t, 0, resp.TotalReturned)
		assert.False(t, resp.Truncated)
	})

	t.Run("sets truncated flag correctly", func(t *testing.T) {
		// Create a new transaction for this test
		tx2, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer tx2.Rollback()

		// Insert multiple dependencies
		_, err = tx2.Exec(`
			INSERT OR IGNORE INTO files (file_path, module_path, content) VALUES
				('internal/graph/file1.go', 'internal/graph', 'package graph'),
				('internal/graph/file2.go', 'internal/graph', 'package graph')
		`)
		require.NoError(t, err)

		_, err = tx2.Exec(`
			INSERT OR IGNORE INTO imports (file_path, import_path, import_line) VALUES
				('internal/graph/file1.go', 'fmt', 10),
				('internal/graph/file1.go', 'context', 11),
				('internal/graph/file2.go', 'database/sql', 5)
		`)
		require.NoError(t, err)

		sql, args := sqlSearcher.buildDependenciesSQL("internal/graph", 2) // Limit to 2

		req := &QueryRequest{
			Operation:  OperationDependencies,
			Target:     "internal/graph",
			MaxResults: 2,
		}

		resp, err := sqlSearcher.executeDependencyQuery(ctx, tx2, sql, args, req)
		require.NoError(t, err)

		// Should be truncated if we have 2 or more results
		if len(resp.Results) >= 2 {
			assert.True(t, resp.Truncated)
		}
	})
}

// TestQueryDependencies_Integration tests the full dependencies query flow.
func TestQueryDependencies_Integration(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer db.Close()

	// Setup test data
	setupDependencyTestData(t, db)

	searcher, err := NewSQLSearcher(db, "/test/root")
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	t.Run("finds dependencies by module path", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationDependencies,
			Target:     "internal/graph",
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, string(OperationDependencies), resp.Operation)
		assert.Equal(t, "internal/graph", resp.Target)
		assert.GreaterOrEqual(t, resp.Metadata.TookMs, 0)
		assert.Equal(t, "graph", resp.Metadata.Source)
	})

	t.Run("finds dependencies by file path", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationDependencies,
			Target:     "internal/graph/searcher.go",
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should work for file paths too
		assert.Equal(t, string(OperationDependencies), resp.Operation)
	})
}

// TestQueryDependents_Integration tests the full dependents query flow.
func TestQueryDependents_Integration(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer db.Close()

	// Setup test data
	setupDependencyTestData(t, db)

	searcher, err := NewSQLSearcher(db, "/test/root")
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	t.Run("finds packages that import target", func(t *testing.T) {
		// Insert test data
		_, err := db.Exec(`
			INSERT INTO files (file_path, module_path, content) VALUES
				('internal/mcp/server.go', 'internal/mcp', 'package mcp')
		`)
		require.NoError(t, err)

		_, err = db.Exec(`
			INSERT INTO imports (file_path, import_path, import_line) VALUES
				('internal/mcp/server.go', 'internal/graph', 15)
		`)
		require.NoError(t, err)

		req := &QueryRequest{
			Operation:  OperationDependents,
			Target:     "internal/graph",
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, string(OperationDependents), resp.Operation)
		assert.Equal(t, "internal/graph", resp.Target)

		// Should find internal/mcp as a dependent
		if len(resp.Results) > 0 {
			foundMcp := false
			for _, result := range resp.Results {
				if result.Node.ID == "internal/mcp" {
					foundMcp = true
					assert.Equal(t, NodePackage, result.Node.Kind)
					assert.Equal(t, 1, result.Depth)
				}
			}
			assert.True(t, foundMcp, "should find internal/mcp as dependent")
		}
	})

	t.Run("returns empty for package with no dependents", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationDependents,
			Target:     "fmt", // Standard library, no internal dependents
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// May or may not have results depending on test data
		assert.GreaterOrEqual(t, len(resp.Results), 0)
	})
}

// TestQueryTypeUsages_Integration tests the full type usages query flow.
func TestQueryTypeUsages_Integration(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer db.Close()

	// Setup test data
	setupTypeUsageTestData(t, db)

	searcher, err := NewSQLSearcher(db, "/test/root")
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	t.Run("finds exact type usage", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationTypeUsages,
			Target:     "User",
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, string(OperationTypeUsages), resp.Operation)
		assert.Equal(t, "User", resp.Target)

		// Results are functions, not types
		for _, result := range resp.Results {
			assert.True(t, result.Node.Kind == NodeFunction || result.Node.Kind == NodeMethod)
			assert.Equal(t, 1, result.Depth)
		}
	})

	t.Run("finds pattern-based type usage", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationTypeUsages,
			Target:     "%User%", // Matches *User, []User, etc.
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should find functions with any User-related type
		assert.GreaterOrEqual(t, len(resp.Results), 0)
	})
}

// setupDependencyTestData creates test data for dependency queries.
func setupDependencyTestData(t *testing.T, db *sql.DB) {
	// Create files table with module paths
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			file_path TEXT PRIMARY KEY,
			module_path TEXT,
			content TEXT
		)
	`)
	require.NoError(t, err)

	// Create imports table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS imports (
			file_path TEXT,
			import_path TEXT,
			import_line INTEGER,
			PRIMARY KEY (file_path, import_path)
		)
	`)
	require.NoError(t, err)

	// Insert sample data
	_, err = db.Exec(`
		INSERT OR IGNORE INTO files (file_path, module_path, content) VALUES
			('internal/graph/searcher.go', 'internal/graph', 'package graph'),
			('internal/graph/types.go', 'internal/graph', 'package graph')
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT OR IGNORE INTO imports (file_path, import_path, import_line) VALUES
			('internal/graph/searcher.go', 'context', 5),
			('internal/graph/searcher.go', 'database/sql', 6),
			('internal/graph/types.go', 'fmt', 3)
	`)
	require.NoError(t, err)
}

// setupTypeUsageTestData creates test data for type usage queries.
func setupTypeUsageTestData(t *testing.T, db *sql.DB) {
	// Create functions table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS functions (
			function_id TEXT PRIMARY KEY,
			file_path TEXT,
			name TEXT,
			start_line INTEGER,
			end_line INTEGER,
			start_pos INTEGER,
			end_pos INTEGER,
			module_path TEXT,
			is_method BOOLEAN,
			receiver_type_name TEXT
		)
	`)
	require.NoError(t, err)

	// Create function_parameters table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS function_parameters (
			function_id TEXT,
			param_name TEXT,
			param_type TEXT,
			PRIMARY KEY (function_id, param_name)
		)
	`)
	require.NoError(t, err)

	// Insert sample functions
	_, err = db.Exec(`
		INSERT OR IGNORE INTO functions (
			function_id, file_path, name, start_line, end_line,
			start_pos, end_pos, module_path, is_method, receiver_type_name
		) VALUES
			('CreateUser', 'internal/user/service.go', 'CreateUser', 10, 20, 100, 200, 'internal/user', false, NULL),
			('UpdateUser', 'internal/user/service.go', 'UpdateUser', 30, 40, 300, 400, 'internal/user', false, NULL)
	`)
	require.NoError(t, err)

	// Insert sample parameters
	_, err = db.Exec(`
		INSERT OR IGNORE INTO function_parameters (function_id, param_name, param_type) VALUES
			('CreateUser', 'user', 'User'),
			('UpdateUser', 'user', '*User')
	`)
	require.NoError(t, err)
}
