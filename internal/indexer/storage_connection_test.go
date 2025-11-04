package indexer

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/require"
)

// TestSQLiteStorage_SingleConnection verifies that SQLiteStorage uses a single
// shared database connection instead of opening multiple connections.
func TestSQLiteStorage_SingleConnection(t *testing.T) {
	// Create a temporary project directory
	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "test-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	// Initialize git repo (required for cache key)
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, ".git"), 0755))

	// Setup database and cache
	db, cacheDir := setupTestDB(t, tmpDir)
	defer db.Close()

	// Create SQLiteStorage
	store, err := NewSQLiteStorage(db, cacheDir, projectPath)
	require.NoError(t, err)
	defer store.Close()

	// Verify that database connection is initialized
	storageDB := store.GetDB()
	require.NotNil(t, storageDB, "Database connection should be initialized")

	// Insert a file record first (chunks table has FK constraint to files table)
	fileSQL := `
		INSERT INTO files (file_path, language, module_path, is_test, line_count_total,
			line_count_code, line_count_comment, line_count_blank, size_bytes,
			file_hash, last_modified, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err = storageDB.Exec(fileSQL,
		"test.go", "go", "test", 0, 10, 8, 1, 1, 100,
		"test-hash", "2025-11-02T00:00:00Z", "2025-11-02T00:00:00Z")
	require.NoError(t, err, "Should be able to insert file record")

	// Test that we can write chunks (proving the connection works)
	chunks := []Chunk{
		{
			ID:        "test-chunk-1",
			ChunkType: ChunkTypeSymbols,
			Title:     "Test Chunk",
			Text:      "This is a test chunk",
			Embedding: make([]float32, 384), // 384-dim embedding
			Metadata: map[string]interface{}{
				"file_path":  "test.go",
				"start_line": 1,
				"end_line":   10,
			},
		},
	}

	err = store.WriteChunks(chunks)
	require.NoError(t, err, "Should be able to write chunks with shared connection")

	// Verify data was written by reading it back
	metadata, err := store.ReadMetadata()
	require.NoError(t, err)
	require.NotNil(t, metadata)
	require.Contains(t, metadata.FileChecksums, "test.go", "Should have file metadata")

	// Close the storage (should only close db once)
	err = store.Close()
	require.NoError(t, err, "Close should work without double-close errors")
}

// TestStorageWriters_WithDB verifies that the WithDB constructors work correctly
// and don't close shared connections.
func TestStorageWriters_WithDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Open a database connection
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Enable foreign keys
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Create schema
	err = storage.CreateSchema(db)
	require.NoError(t, err)

	// Create writers using shared connection
	chunkWriter := storage.NewChunkWriterWithDB(db)
	require.NotNil(t, chunkWriter)

	chunkReader := storage.NewChunkReaderWithDB(db)
	require.NotNil(t, chunkReader)

	graphWriter := storage.NewGraphWriterWithDB(db)
	require.NotNil(t, graphWriter)

	// Close writers (should NOT close the shared db)
	require.NoError(t, chunkWriter.Close())
	require.NoError(t, chunkReader.Close())
	require.NoError(t, graphWriter.Close())

	// Verify db is still usable
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&count)
	require.NoError(t, err, "Database should still be open after closing shared writers")
	require.Equal(t, 0, count)
}
