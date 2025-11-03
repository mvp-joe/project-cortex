package storage

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestChunkWriter_OwnershipTracking verifies that ChunkWriter tracks
// connection ownership correctly and only closes connections it owns.
func TestChunkWriter_OwnershipTracking(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("NewChunkWriter owns connection", func(t *testing.T) {
		// Create writer that owns connection
		writer, err := NewChunkWriter(tmpDir + "/owned.db")
		require.NoError(t, err)

		// Verify it has ownsDB=true (by checking Close() actually closes)
		err = writer.Close()
		require.NoError(t, err)

		// Verify db is closed by trying to execute a query
		var count int
		err = writer.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&count)
		require.Error(t, err, "DB should be closed after Close()")
		require.Contains(t, err.Error(), "closed", "Error should indicate connection is closed")
	})

	t.Run("NewChunkWriterWithDB doesn't own connection", func(t *testing.T) {
		// Open a connection
		db, err := sql.Open("sqlite3", tmpDir+"/shared.db")
		require.NoError(t, err)
		defer db.Close()

		// Enable foreign keys and create schema
		_, err = db.Exec("PRAGMA foreign_keys = ON")
		require.NoError(t, err)

		// Create writer with shared connection
		writer := NewChunkWriterWithDB(db)

		// Close writer (should NOT close db)
		err = writer.Close()
		require.NoError(t, err)

		// Verify db is still usable
		var count int
		err = db.QueryRow("SELECT 1").Scan(&count)
		require.NoError(t, err, "DB should still be open after writer.Close()")
		require.Equal(t, 1, count)
	})
}

// TestChunkReader_OwnershipTracking verifies ChunkReader ownership behavior.
func TestChunkReader_OwnershipTracking(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("NewChunkReader owns connection", func(t *testing.T) {
		// Need to create a database first
		db, err := sql.Open("sqlite3", tmpDir+"/owned.db")
		require.NoError(t, err)
		_, err = db.Exec("CREATE TABLE IF NOT EXISTS chunks (id INTEGER)")
		require.NoError(t, err)
		db.Close()

		// Create reader that owns connection
		reader, err := NewChunkReader(tmpDir + "/owned.db")
		require.NoError(t, err)

		// Close reader (should close db)
		err = reader.Close()
		require.NoError(t, err)

		// Verify db is closed
		var count int
		err = reader.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&count)
		require.Error(t, err, "DB should be closed after Close()")
	})

	t.Run("NewChunkReaderWithDB doesn't own connection", func(t *testing.T) {
		// Open a connection
		db, err := sql.Open("sqlite3", tmpDir+"/shared-read.db")
		require.NoError(t, err)
		defer db.Close()

		// Create reader with shared connection
		reader := NewChunkReaderWithDB(db)

		// Close reader (should NOT close db)
		err = reader.Close()
		require.NoError(t, err)

		// Verify db is still usable
		var count int
		err = db.QueryRow("SELECT 1").Scan(&count)
		require.NoError(t, err, "DB should still be open after reader.Close()")
	})
}

// TestGraphWriter_OwnershipTracking verifies GraphWriter ownership behavior.
func TestGraphWriter_OwnershipTracking(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("NewGraphWriter owns connection", func(t *testing.T) {
		// Create writer that owns connection
		writer, err := NewGraphWriter(tmpDir + "/owned-graph.db")
		require.NoError(t, err)

		// Close writer (should close db)
		err = writer.Close()
		require.NoError(t, err)

		// Verify db is closed
		var count int
		err = writer.db.QueryRow("SELECT 1").Scan(&count)
		require.Error(t, err, "DB should be closed after Close()")
	})

	t.Run("NewGraphWriterWithDB doesn't own connection", func(t *testing.T) {
		// Open a connection
		db, err := sql.Open("sqlite3", tmpDir+"/shared-graph.db")
		require.NoError(t, err)
		defer db.Close()

		// Create writer with shared connection
		writer := NewGraphWriterWithDB(db)

		// Close writer (should NOT close db)
		err = writer.Close()
		require.NoError(t, err)

		// Verify db is still usable
		var count int
		err = db.QueryRow("SELECT 1").Scan(&count)
		require.NoError(t, err, "DB should still be open after writer.Close()")
	})
}
