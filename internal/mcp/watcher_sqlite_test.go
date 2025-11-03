package mcp

// Integration tests for SQLite database file watching.
// These tests verify that the watcher can detect changes to SQLite database files.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFileWatcher_SQLiteFile tests watching a SQLite database file directly.
func TestFileWatcher_SQLiteFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create a real SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Create a table and insert data
	_, err = db.Exec(`CREATE TABLE chunks (id TEXT PRIMARY KEY, data TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chunks (id, data) VALUES ('chunk1', 'initial data')`)
	require.NoError(t, err)

	mock := &mockReloadable{}

	// Watch the database file directly
	watcher, err := NewFileWatcherMulti(mock, []string{dbPath})
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Modify database (insert new row)
	_, err = db.Exec(`INSERT INTO chunks (id, data) VALUES ('chunk2', 'new data')`)
	require.NoError(t, err)

	// Wait for debounce + reload
	time.Sleep(800 * time.Millisecond)

	assert.Equal(t, 1, mock.getReloadCount(), "Expected reload after database modification")
}

// TestFileWatcher_SQLiteDirectory tests watching directory containing SQLite database.
func TestFileWatcher_SQLiteDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE chunks (id TEXT PRIMARY KEY, data TEXT)`)
	require.NoError(t, err)

	mock := &mockReloadable{}

	// Watch the directory (not the file directly)
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Modify database
	_, err = db.Exec(`INSERT INTO chunks (id, data) VALUES ('chunk1', 'data')`)
	require.NoError(t, err)

	// Wait for debounce + reload
	time.Sleep(800 * time.Millisecond)

	assert.Equal(t, 1, mock.getReloadCount(), "Expected reload after database modification in watched directory")
}

// TestFileWatcher_SQLiteAndJSON tests watching both SQLite and JSON simultaneously.
func TestFileWatcher_SQLiteAndJSON(t *testing.T) {
	t.Parallel()

	jsonDir := t.TempDir()
	dbDir := t.TempDir()

	jsonFile := filepath.Join(jsonDir, "chunks.json")
	dbPath := filepath.Join(dbDir, "cache.db")

	// Create JSON file
	require.NoError(t, os.WriteFile(jsonFile, []byte(`{"chunks": []}`), 0644))

	// Create SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE chunks (id TEXT PRIMARY KEY, data TEXT)`)
	require.NoError(t, err)

	mock := &mockReloadable{}

	// Watch both JSON directory and SQLite file
	watcher, err := NewFileWatcherMulti(mock, []string{jsonDir, dbPath})
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Test 1: Modify JSON file
	require.NoError(t, os.WriteFile(jsonFile, []byte(`{"chunks": [{"id": "1"}]}`), 0644))
	time.Sleep(800 * time.Millisecond)
	assert.Equal(t, 1, mock.getReloadCount(), "Expected reload after JSON modification")

	// Test 2: Modify SQLite database
	_, err = db.Exec(`INSERT INTO chunks (id, data) VALUES ('chunk1', 'data')`)
	require.NoError(t, err)
	time.Sleep(800 * time.Millisecond)
	assert.Equal(t, 2, mock.getReloadCount(), "Expected second reload after SQLite modification")
}

// TestFileWatcher_SQLiteTransactions tests that watcher detects transaction commits.
func TestFileWatcher_SQLiteTransactions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE chunks (id TEXT PRIMARY KEY, data TEXT)`)
	require.NoError(t, err)

	mock := &mockReloadable{}
	watcher, err := NewFileWatcherMulti(mock, []string{dbPath})
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Start transaction
	tx, err := db.Begin()
	require.NoError(t, err)

	_, err = tx.Exec(`INSERT INTO chunks (id, data) VALUES ('chunk1', 'data')`)
	require.NoError(t, err)

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Wait for debounce + reload
	time.Sleep(800 * time.Millisecond)

	// Should trigger reload on transaction commit
	assert.Equal(t, 1, mock.getReloadCount(), "Expected reload after transaction commit")
}

// TestFileWatcher_SQLiteWALMode tests watching SQLite database in WAL mode.
// WAL (Write-Ahead Logging) is the default mode for better concurrency.
func TestFileWatcher_SQLiteWALMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create SQLite database with WAL mode
	db, err := sql.Open("sqlite3", dbPath+"?_journal=WAL")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE chunks (id TEXT PRIMARY KEY, data TEXT)`)
	require.NoError(t, err)

	mock := &mockReloadable{}

	// Watch the directory (WAL creates multiple files: .db, .db-wal, .db-shm)
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Modify database in WAL mode
	_, err = db.Exec(`INSERT INTO chunks (id, data) VALUES ('chunk1', 'data')`)
	require.NoError(t, err)

	// Wait for debounce + reload
	time.Sleep(800 * time.Millisecond)

	assert.GreaterOrEqual(t, mock.getReloadCount(), 1,
		"Expected at least one reload after database modification in WAL mode")
}

// TestNewFileWatcherAuto_Integration tests auto-detection of SQLite database.
// This is an integration test that requires a project-like structure.
func TestNewFileWatcherAuto_NoProject(t *testing.T) {
	t.Parallel()

	// Create temporary directories
	projectPath := t.TempDir()
	chunksDir := t.TempDir()

	mock := &mockReloadable{}

	// Try auto-detection without valid cache (should still work, just watch JSON)
	watcher, err := NewFileWatcherAuto(mock, projectPath, chunksDir)
	require.NoError(t, err, "Auto-detection should succeed even without cache")
	defer watcher.Stop()

	// Verify it at least watches the chunks directory
	assert.Contains(t, watcher.watchPaths, chunksDir, "Should watch chunks directory")
}
