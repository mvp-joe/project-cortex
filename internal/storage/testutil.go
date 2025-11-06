package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func init() {
	InitVectorExtension()
}

// NewTestDB creates a fully configured in-memory SQLite database for testing.
//
// The database includes:
//   - Foreign key constraints enabled (CRITICAL for cascade deletes)
//   - sqlite-vec extension initialized
//   - Full schema created (all tables, indexes, virtual tables)
//   - Automatic cleanup registered with t.Cleanup()
//
// This is the standard test database helper - use it for 90% of tests.
//
// Example:
//
//	func TestSomething(t *testing.T) {
//	    db := storage.NewTestDB(t)
//	    // ... test code ...
//	    // No need to close - t.Cleanup() handles it
//	}
func NewTestDB(t testing.TB) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Enable foreign key constraints (required for cascade deletes)
	// SQLite disables foreign keys by default for backward compatibility
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Initialize vector extension (global, idempotent)
	InitVectorExtension()

	// Create full schema (tables, indexes, FTS, vector tables)
	err = CreateSchema(db)
	require.NoError(t, err)

	return db
}

// NewTestDBFile creates a file-based SQLite database in t.TempDir().
//
// Use this when you need to test:
//   - Database persistence across connections
//   - File operations (copy, move, delete)
//   - Multi-connection scenarios
//   - Branch database isolation
//
// The database includes:
//   - Foreign key constraints enabled
//   - sqlite-vec extension initialized
//   - Full schema created
//   - File located in t.TempDir() (auto-cleaned up)
//   - Automatic connection cleanup registered with t.Cleanup()
//
// Example:
//
//	func TestDatabasePersistence(t *testing.T) {
//	    db := storage.NewTestDBFile(t)
//	    // Write data
//	    db.Close()
//	    // Reopen and verify data persisted
//	}
func NewTestDBFile(t testing.TB) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Enable foreign key constraints
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Initialize vector extension
	InitVectorExtension()

	// Create full schema
	err = CreateSchema(db)
	require.NoError(t, err)

	return db
}

// NewTestDBMinimal creates an in-memory SQLite database without schema.
//
// Use this when you need to:
//   - Test schema creation itself (CreateSchema, migrations)
//   - Create custom test schemas
//   - Test schema validation logic
//   - Have full control over database structure
//
// The database includes:
//   - Foreign key constraints enabled
//   - sqlite-vec extension initialized
//   - NO schema created (empty database)
//   - Automatic cleanup registered with t.Cleanup()
//
// You must manually create your schema after getting the database.
//
// Example:
//
//	func TestSchemaCreation(t *testing.T) {
//	    db := storage.NewTestDBMinimal(t)
//	    // Now test CreateSchema()
//	    err := storage.CreateSchema(db)
//	    require.NoError(t, err)
//	}
func NewTestDBMinimal(t testing.TB) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Enable foreign key constraints
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Initialize vector extension
	InitVectorExtension()

	// Do NOT create schema - caller is responsible

	return db
}
