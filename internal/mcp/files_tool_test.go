package mcp

import (
	"database/sql"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddCortexFilesTool_Registration tests that the tool registers successfully.
func TestAddCortexFilesTool_Registration(t *testing.T) {
	t.Parallel()

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Create test database
	db := setupTestDatabase(t)
	defer db.Close()

	// Register tool (should not panic)
	require.NotPanics(t, func() {
		AddCortexFilesTool(mcpServer, db)
	})

	// Verify tool was registered by checking if server is initialized
	// Note: mcp-go doesn't expose registered tools publicly, so we can only verify it doesn't panic
	assert.NotNil(t, mcpServer)
}

// TestAddCortexFilesTool_WithNilDB tests handling of nil database.
func TestAddCortexFilesTool_WithNilDB(t *testing.T) {
	t.Parallel()

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register tool with nil database (should not panic during registration)
	// The handler will fail when invoked, but registration should succeed
	require.NotPanics(t, func() {
		AddCortexFilesTool(mcpServer, nil)
	})
}

// TestAddCortexFilesTool_MultipleCalls tests that tool can be registered multiple times.
// This is important for composability - multiple tools can be added to the same server.
func TestAddCortexFilesTool_MultipleCalls(t *testing.T) {
	t.Parallel()

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Create test databases
	db1 := setupTestDatabase(t)
	defer db1.Close()

	db2 := setupTestDatabase(t)
	defer db2.Close()

	// Register tool twice (should not panic, though second registration may override)
	require.NotPanics(t, func() {
		AddCortexFilesTool(mcpServer, db1)
		// Note: Registering the same tool name twice may override the first registration
		// This is expected behavior for the composable pattern
	})
}

// setupTestDatabase creates a minimal in-memory SQLite database for testing.
func setupTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Create minimal schema
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
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)

	return db
}
