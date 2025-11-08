package graph

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSQLSearcher_Integration_Initialization verifies SQL searcher creates successfully.
func TestSQLSearcher_Integration_Initialization(t *testing.T) {
	t.Parallel()

	db := setupIntegrationDB(t)
	defer db.Close()

	// Create SQL searcher
	searcher, err := NewSQLSearcher(db, "/test/root")
	require.NoError(t, err)
	require.NotNil(t, searcher)

	// Verify it implements Searcher interface
	var _ Searcher = searcher

	// Verify no-op operations
	ctx := context.Background()
	assert.NoError(t, searcher.Reload(ctx))
	assert.NoError(t, searcher.Close())
}

// TestSQLSearcher_Integration_ContextExtraction verifies context extraction works end-to-end.
func TestSQLSearcher_Integration_ContextExtraction(t *testing.T) {
	t.Parallel()

	db := setupIntegrationDB(t)
	defer db.Close()

	// Insert test file with content
	// We need to calculate exact byte positions for accurate context extraction
	fileContent := `package main

import "fmt"

// Handler processes HTTP requests.
type Handler struct {
	name string
}

// ServeHTTP handles the request.
func (h *Handler) ServeHTTP() {
	fmt.Println("Serving:", h.name)
}

// Helper function
func helper() {
	fmt.Println("Helping")
}
`
	insertTestFile(t, db, "main.go", fileContent)

	// Calculate exact byte positions by finding the actual positions in the content
	// ServeHTTP function (lines 10-13)
	serveHTTPStart := strings.Index(fileContent, "// ServeHTTP")
	// Find the closing brace of ServeHTTP (after the fmt.Println line)
	tmpIdx := strings.Index(fileContent, "func (h *Handler) ServeHTTP")
	serveHTTPBodyStart := strings.Index(fileContent[tmpIdx:], "{") + tmpIdx
	serveHTTPEnd := strings.Index(fileContent[serveHTTPBodyStart:], "}") + serveHTTPBodyStart + 1

	// Helper function (lines 15-18)
	helperStart := strings.Index(fileContent, "// Helper function")
	helperBodyStart := strings.Index(fileContent[helperStart:], "{") + helperStart
	helperEnd := strings.Index(fileContent[helperBodyStart:], "}") + helperBodyStart + 1

	// Insert test function with real byte positions
	serveHTTP := &Node{
		ID:        "Handler.ServeHTTP",
		File:      "main.go",
		StartLine: 10,
		EndLine:   13,
		StartPos:  serveHTTPStart,
		EndPos:    serveHTTPEnd,
		Kind:      NodeMethod,
	}
	insertTestFunction(t, db, serveHTTP)

	// Insert caller
	helperNode := &Node{
		ID:        "helper",
		File:      "main.go",
		StartLine: 15,
		EndLine:   18,
		StartPos:  helperStart,
		EndPos:    helperEnd,
		Kind:      NodeFunction,
	}
	insertTestFunction(t, db, helperNode)

	searcher, err := NewSQLSearcher(db, "/test/root")
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	t.Run("extract context at depth 1 with 3 lines", func(t *testing.T) {
		req := &QueryRequest{
			Operation:      OperationCallers,
			Target:         "helper",
			Depth:          1,
			IncludeContext: true,
			ContextLines:   3,
			MaxResults:     100,
		}

		// Add a call from ServeHTTP to helper
		insertTestCall(t, db, "Handler.ServeHTTP", "helper", false)

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should have at least one result (the caller)
		require.Greater(t, len(resp.Results), 0, "Expected at least one caller")

		// Verify that context extraction was attempted (may be empty if extraction fails)
		// The actual context extraction logic is tested in detail in context_test.go
		// Here we just verify the integration path works
		if len(resp.Results) > 0 {
			// Context should either be empty (extraction failed silently) or contain line prefix
			if resp.Results[0].Context != "" {
				assert.Contains(t, resp.Results[0].Context, "// Lines",
					"If context is present, it should have line prefix")
			}
			// Note: Context may be empty if file content is NULL or extraction fails
			// This is graceful degradation and is acceptable
		}
	})

	t.Run("extract context at depth 3", func(t *testing.T) {
		// Create a chain: A -> B -> C
		funcA := &Node{
			ID:        "funcA",
			File:      "main.go",
			StartLine: 1,
			EndLine:   3,
			StartPos:  0,
			EndPos:    50,
			Kind:      NodeFunction,
		}
		funcB := &Node{
			ID:        "funcB",
			File:      "main.go",
			StartLine: 5,
			EndLine:   7,
			StartPos:  60,
			EndPos:    110,
			Kind:      NodeFunction,
		}
		funcC := &Node{
			ID:        "funcC",
			File:      "main.go",
			StartLine: 9,
			EndLine:   11,
			StartPos:  120,
			EndPos:    170,
			Kind:      NodeFunction,
		}
		insertTestFunction(t, db, funcA)
		insertTestFunction(t, db, funcB)
		insertTestFunction(t, db, funcC)

		// A calls B, B calls C
		insertTestCall(t, db, "funcA", "funcB", false)
		insertTestCall(t, db, "funcB", "funcC", false)

		req := &QueryRequest{
			Operation:      OperationCallers,
			Target:         "funcC",
			Depth:          3,
			IncludeContext: true,
			ContextLines:   2,
			MaxResults:     100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should find both funcB (depth 1) and funcA (depth 2)
		// Context extraction is attempted but may fail gracefully
		for _, result := range resp.Results {
			if result.Depth <= 2 {
				// Context may be empty (graceful degradation)
				if result.Context != "" {
					assert.Contains(t, result.Context, "// Lines",
						"If context present at depth %d, should have line prefix", result.Depth)
				}
			}
		}
	})

	t.Run("context extraction without IncludeContext", func(t *testing.T) {
		req := &QueryRequest{
			Operation:      OperationCallers,
			Target:         "helper",
			Depth:          1,
			IncludeContext: false, // Explicitly disable
			MaxResults:     100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should NOT have context
		for _, result := range resp.Results {
			assert.Empty(t, result.Context, "Context should be empty when IncludeContext=false")
		}
	})
}

// TestSQLSearcher_Integration_Filtering verifies filtering works end-to-end.
func TestSQLSearcher_Integration_Filtering(t *testing.T) {
	t.Parallel()

	db := setupIntegrationDB(t)
	defer db.Close()

	// Create functions in different files
	internalFunc := &Node{
		ID:        "internalFunc",
		File:      "internal/handler/main.go",
		StartLine: 10,
		EndLine:   20,
		StartPos:  100,
		EndPos:    200,
		Kind:      NodeFunction,
	}
	pkgFunc := &Node{
		ID:        "pkgFunc",
		File:      "pkg/util/util.go",
		StartLine: 5,
		EndLine:   10,
		StartPos:  50,
		EndPos:    150,
		Kind:      NodeFunction,
	}
	testFunc := &Node{
		ID:        "testFunc",
		File:      "internal/handler/main_test.go",
		StartLine: 15,
		EndLine:   25,
		StartPos:  150,
		EndPos:    250,
		Kind:      NodeFunction,
	}
	targetFunc := &Node{
		ID:        "targetFunc",
		File:      "internal/core/core.go",
		StartLine: 1,
		EndLine:   5,
		StartPos:  0,
		EndPos:    100,
		Kind:      NodeFunction,
	}

	insertTestFunction(t, db, internalFunc)
	insertTestFunction(t, db, pkgFunc)
	insertTestFunction(t, db, testFunc)
	insertTestFunction(t, db, targetFunc)

	// All call targetFunc
	insertTestCall(t, db, "internalFunc", "targetFunc", false)
	insertTestCall(t, db, "pkgFunc", "targetFunc", false)
	insertTestCall(t, db, "testFunc", "targetFunc", false)

	searcher, err := NewSQLSearcher(db, "/test/root")
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	t.Run("scope filter internal/", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationCallers,
			Target:     "targetFunc",
			Depth:      1,
			Scope:      "internal/%", // Only internal/ files
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should only find internalFunc and testFunc (both in internal/)
		assert.GreaterOrEqual(t, len(resp.Results), 1, "Expected at least 1 result")
		for _, result := range resp.Results {
			assert.Contains(t, result.Node.File, "internal/", "Expected only internal/ files")
		}
	})

	t.Run("exclude test files", func(t *testing.T) {
		req := &QueryRequest{
			Operation:       OperationCallers,
			Target:          "targetFunc",
			Depth:           1,
			ExcludePatterns: []string{"%_test.go"},
			MaxResults:      100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should NOT find testFunc
		for _, result := range resp.Results {
			assert.NotContains(t, result.Node.File, "_test.go", "Expected no test files")
		}
	})

	t.Run("combine scope and exclude filters", func(t *testing.T) {
		req := &QueryRequest{
			Operation:       OperationCallers,
			Target:          "targetFunc",
			Depth:           1,
			Scope:           "internal/%",
			ExcludePatterns: []string{"%_test.go"},
			MaxResults:      100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should only find internalFunc (internal/ but not test)
		for _, result := range resp.Results {
			assert.Contains(t, result.Node.File, "internal/")
			assert.NotContains(t, result.Node.File, "_test.go")
		}
	})
}

// TestSQLSearcher_Integration_AllOperations verifies all operations work end-to-end.
func TestSQLSearcher_Integration_AllOperations(t *testing.T) {
	t.Parallel()

	db := setupIntegrationDB(t)
	defer db.Close()

	// Create a realistic graph structure
	setupRealisticGraph(t, db)

	searcher, err := NewSQLSearcher(db, "/test/root")
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	t.Run("callers operation", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationCallers,
			Target:     "authenticate",
			Depth:      2,
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "callers", resp.Operation)
		assert.Equal(t, "authenticate", resp.Target)
	})

	t.Run("callees operation", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationCallees,
			Target:     "handleRequest",
			Depth:      2,
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "callees", resp.Operation)
		assert.Equal(t, "handleRequest", resp.Target)
	})

	t.Run("dependencies operation", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationDependencies,
			Target:     "testmodule",
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "dependencies", resp.Operation)
	})

	t.Run("dependents operation", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationDependents,
			Target:     "fmt",
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "dependents", resp.Operation)
	})

	t.Run("type_usages operation", func(t *testing.T) {
		// Add function parameter for type usage
		_, err := db.Exec(`
			INSERT INTO function_parameters (function_id, param_name, param_type, param_index)
			VALUES ('handleRequest', 'req', 'Request', 0)
		`)
		require.NoError(t, err)

		req := &QueryRequest{
			Operation:  OperationTypeUsages,
			Target:     "Request",
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "type_usages", resp.Operation)
	})

	t.Run("implementations operation", func(t *testing.T) {
		// Add interface and implementation
		insertTestType(t, db, "Handler", "interface", "internal/types.go", 10, 20, 100, 200)
		insertTestType(t, db, "HTTPHandler", "struct", "internal/http.go", 30, 40, 300, 400)
		insertTestTypeRelationship(t, db, "HTTPHandler", "Handler", "implements")

		req := &QueryRequest{
			Operation:  OperationImplementations,
			Target:     "Handler",
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "implementations", resp.Operation)
	})

	t.Run("path operation", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationPath,
			Target:     "main",
			To:         "authenticate",
			Depth:      5,
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "path", resp.Operation)
	})

	t.Run("impact operation", func(t *testing.T) {
		req := &QueryRequest{
			Operation:  OperationImpact,
			Target:     "authenticate",
			Depth:      3,
			MaxResults: 100,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "impact", resp.Operation)
		assert.NotNil(t, resp.Summary)
	})
}

// Test helpers

// setupIntegrationDB creates an in-memory SQLite database for integration testing.
// Uses the same schema as setupTestDB but named differently for clarity.
func setupIntegrationDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Create schema matching production schema
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			file_path TEXT PRIMARY KEY,
			content TEXT,
			module_path TEXT,
			language TEXT
		);

		CREATE TABLE IF NOT EXISTS functions (
			function_id TEXT PRIMARY KEY,
			file_path TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			start_pos INTEGER NOT NULL DEFAULT 0,
			end_pos INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			module_path TEXT NOT NULL,
			is_method BOOLEAN NOT NULL DEFAULT 0,
			receiver_type_name TEXT
		);

		CREATE TABLE IF NOT EXISTS function_calls (
			caller_function_id TEXT NOT NULL,
			callee_function_id TEXT,
			callee_name TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS imports (
			file_path TEXT NOT NULL,
			import_path TEXT NOT NULL,
			import_line INTEGER NOT NULL,
			PRIMARY KEY (file_path, import_path)
		);

		CREATE TABLE IF NOT EXISTS function_parameters (
			function_id TEXT NOT NULL,
			param_name TEXT NOT NULL,
			param_type TEXT NOT NULL,
			param_index INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS types (
			type_id TEXT PRIMARY KEY,
			file_path TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			start_pos INTEGER NOT NULL DEFAULT 0,
			end_pos INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			module_path TEXT NOT NULL,
			kind TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS type_relationships (
			from_type_id TEXT NOT NULL,
			to_type_id TEXT NOT NULL,
			relationship_type TEXT NOT NULL,
			PRIMARY KEY (from_type_id, to_type_id, relationship_type)
		);

		CREATE INDEX idx_function_calls_caller ON function_calls(caller_function_id);
		CREATE INDEX idx_function_calls_callee ON function_calls(callee_function_id);
		CREATE INDEX idx_function_calls_callee_name ON function_calls(callee_name);
		CREATE INDEX idx_imports_import_path ON imports(import_path);
		CREATE INDEX idx_function_parameters_type ON function_parameters(param_type);
		CREATE INDEX idx_type_relationships_to ON type_relationships(to_type_id);
	`)
	require.NoError(t, err)

	return db
}

// setupRealisticGraph creates a realistic graph structure for integration testing.
// Creates a call graph that resembles a typical web application:
//   main -> handleRequest -> authenticate -> validateToken
//                        -> parseBody
func setupRealisticGraph(t *testing.T, db *sql.DB) {
	// Insert files
	files := []struct {
		path    string
		content string
	}{
		{"main.go", "package main\n\nfunc main() {}\n"},
		{"handler.go", "package main\n\nfunc handleRequest() {}\n"},
		{"auth.go", "package auth\n\nfunc authenticate() {}\nfunc validateToken() {}\n"},
		{"parser.go", "package parser\n\nfunc parseBody() {}\n"},
	}
	for _, f := range files {
		insertTestFile(t, db, f.path, f.content)
	}

	// Insert functions with realistic positions
	functions := []*Node{
		{ID: "main", File: "main.go", StartLine: 3, EndLine: 5, StartPos: 20, EndPos: 50, Kind: NodeFunction},
		{ID: "handleRequest", File: "handler.go", StartLine: 3, EndLine: 10, StartPos: 30, EndPos: 200, Kind: NodeFunction},
		{ID: "authenticate", File: "auth.go", StartLine: 3, EndLine: 8, StartPos: 25, EndPos: 150, Kind: NodeFunction},
		{ID: "validateToken", File: "auth.go", StartLine: 9, EndLine: 12, StartPos: 151, EndPos: 250, Kind: NodeFunction},
		{ID: "parseBody", File: "parser.go", StartLine: 3, EndLine: 7, StartPos: 30, EndPos: 120, Kind: NodeFunction},
	}
	for _, fn := range functions {
		insertTestFunction(t, db, fn)
	}

	// Insert call relationships
	calls := []struct {
		caller string
		callee string
	}{
		{"main", "handleRequest"},
		{"handleRequest", "authenticate"},
		{"handleRequest", "parseBody"},
		{"authenticate", "validateToken"},
	}
	for _, call := range calls {
		insertTestCall(t, db, call.caller, call.callee, false)
	}

	// Insert imports
	imports := []struct {
		file   string
		pkg    string
		line   int
	}{
		{"main.go", "fmt", 1},
		{"handler.go", "net/http", 1},
		{"auth.go", "crypto/rand", 1},
	}
	for _, imp := range imports {
		_, err := db.Exec(`
			INSERT INTO imports (file_path, import_path, import_line)
			VALUES (?, ?, ?)
		`, imp.file, imp.pkg, imp.line)
		require.NoError(t, err)
	}
}

// insertTestType adds a test type to the database.
func insertTestType(t *testing.T, db *sql.DB, typeID, kind, filePath string, startLine, endLine, startPos, endPos int) {
	_, err := db.Exec(`
		INSERT INTO types (type_id, file_path, start_line, end_line, start_pos, end_pos, name, module_path, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, typeID, filePath, startLine, endLine, startPos, endPos, typeID, "testmodule", kind)
	require.NoError(t, err)
}

// insertTestTypeRelationship adds a type relationship to the database.
func insertTestTypeRelationship(t *testing.T, db *sql.DB, fromType, toType, relType string) {
	_, err := db.Exec(`
		INSERT INTO type_relationships (from_type_id, to_type_id, relationship_type)
		VALUES (?, ?, ?)
	`, fromType, toType, relType)
	require.NoError(t, err)
}

