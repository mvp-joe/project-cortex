package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaMigration_2_0_to_2_1 validates schema migration from v2.0 to v2.1.
//
// Migration adds:
// - start_pos/end_pos columns to functions table
// - start_pos/end_pos columns to types table
// - content column to files table
// - FTS triggers for files_fts sync
// - Updates schema_version to "2.1"
//
// This test ensures backward compatibility - old data works with new schema.
func TestSchemaMigration_2_0_to_2_1(t *testing.T) {
	t.Parallel()

	// 1. Create database with schema 2.0 (old schema without new columns)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Enable foreign keys
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Create schema 2.0 (without byte positions, without content field)
	err = createSchema_2_0(db)
	require.NoError(t, err)

	// Verify schema version is 2.0
	version, err := GetSchemaVersion(db)
	require.NoError(t, err)
	assert.Equal(t, "2.0", version, "initial schema should be 2.0")

	// 2. Insert test data using old schema
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Insert file WITHOUT content column (old schema)
	_, err = db.Exec(`
		INSERT INTO files (file_path, language, module_path, is_test,
			line_count_total, line_count_code, line_count_comment, line_count_blank,
			size_bytes, file_hash, last_modified, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "test.go", "go", "main", 0, 100, 80, 10, 10, 1024, "abc123", nowStr, nowStr)
	require.NoError(t, err)

	// Insert type WITHOUT byte positions (old schema)
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind,
			start_line, end_line, is_exported, field_count, method_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "test.go::Handler", "test.go", "main", "Handler", "struct", 10, 15, 1, 2, 3)
	require.NoError(t, err)

	// Insert function WITHOUT byte positions (old schema)
	_, err = db.Exec(`
		INSERT INTO functions (function_id, file_path, module_path, name,
			start_line, end_line, line_count, is_exported, is_method, param_count, return_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "test.go::main", "test.go", "main", "main", 20, 30, 10, 1, 0, 0, 0)
	require.NoError(t, err)

	// 3. Run migration to add new columns
	err = migrateSchema_2_0_to_2_1(db)
	require.NoError(t, err)

	// 4. Verify new columns exist with DEFAULT values
	var startPos, endPos int
	var content sql.NullString

	// Check types table has byte positions (should be 0 from DEFAULT)
	err = db.QueryRow(`
		SELECT start_pos, end_pos FROM types WHERE type_id = ?
	`, "test.go::Handler").Scan(&startPos, &endPos)
	require.NoError(t, err)
	assert.Equal(t, 0, startPos, "default start_pos should be 0")
	assert.Equal(t, 0, endPos, "default end_pos should be 0")

	// Check functions table has byte positions (should be 0 from DEFAULT)
	err = db.QueryRow(`
		SELECT start_pos, end_pos FROM functions WHERE function_id = ?
	`, "test.go::main").Scan(&startPos, &endPos)
	require.NoError(t, err)
	assert.Equal(t, 0, startPos, "default start_pos should be 0")
	assert.Equal(t, 0, endPos, "default end_pos should be 0")

	// Check files table has content column (should be NULL)
	err = db.QueryRow(`
		SELECT content FROM files WHERE file_path = ?
	`, "test.go").Scan(&content)
	require.NoError(t, err)
	assert.False(t, content.Valid, "default content should be NULL for existing files")

	// 5. Verify old data still accessible
	var filePath, language string
	var lineCount int
	err = db.QueryRow(`
		SELECT file_path, language, line_count_total FROM files WHERE file_path = ?
	`, "test.go").Scan(&filePath, &language, &lineCount)
	require.NoError(t, err)
	assert.Equal(t, "test.go", filePath)
	assert.Equal(t, "go", language)
	assert.Equal(t, 100, lineCount)

	var typeName, typeKind string
	err = db.QueryRow(`
		SELECT name, kind FROM types WHERE type_id = ?
	`, "test.go::Handler").Scan(&typeName, &typeKind)
	require.NoError(t, err)
	assert.Equal(t, "Handler", typeName)
	assert.Equal(t, "struct", typeKind)

	var funcName string
	err = db.QueryRow(`
		SELECT name FROM functions WHERE function_id = ?
	`, "test.go::main").Scan(&funcName)
	require.NoError(t, err)
	assert.Equal(t, "main", funcName)

	// 6. Verify schema_version updated to 2.1
	version, err = GetSchemaVersion(db)
	require.NoError(t, err)
	assert.Equal(t, "2.1", version, "schema should be upgraded to 2.1")

	// 7. Verify new inserts can use new columns
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind,
			start_line, end_line, start_pos, end_pos, is_exported, field_count, method_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "test.go::Server", "test.go", "main", "Server", "struct", 40, 50, 1024, 2048, 1, 1, 1)
	require.NoError(t, err)

	// Verify new type has correct byte positions
	err = db.QueryRow(`
		SELECT start_pos, end_pos FROM types WHERE type_id = ?
	`, "test.go::Server").Scan(&startPos, &endPos)
	require.NoError(t, err)
	assert.Equal(t, 1024, startPos)
	assert.Equal(t, 2048, endPos)

	// 8. Test FTS triggers work after migration
	testContent := "package main\n\nfunc main() {}\n"
	_, err = db.Exec(`
		UPDATE files SET content = ? WHERE file_path = ?
	`, testContent, "test.go")
	require.NoError(t, err)

	// Verify FTS entry created automatically
	var ftsContent string
	err = db.QueryRow(`
		SELECT content FROM files_fts WHERE file_path = ?
	`, "test.go").Scan(&ftsContent)
	require.NoError(t, err)
	assert.Equal(t, testContent, ftsContent, "FTS trigger should sync content")
}

// createSchema_2_0 creates schema version 2.0 WITHOUT new features:
// - No start_pos/end_pos columns in types/functions
// - No content column in files
// - No FTS triggers
func createSchema_2_0(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Schema 2.0: files table WITHOUT content column
	_, err = tx.Exec(`
		CREATE TABLE files (
			file_path TEXT PRIMARY KEY,
			language TEXT NOT NULL,
			module_path TEXT NOT NULL,
			is_test INTEGER NOT NULL DEFAULT 0,
			line_count_total INTEGER NOT NULL DEFAULT 0,
			line_count_code INTEGER NOT NULL DEFAULT 0,
			line_count_comment INTEGER NOT NULL DEFAULT 0,
			line_count_blank INTEGER NOT NULL DEFAULT 0,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			file_hash TEXT NOT NULL,
			last_modified TEXT NOT NULL,
			indexed_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	// Schema 2.0: types table WITHOUT start_pos/end_pos
	_, err = tx.Exec(`
		CREATE TABLE types (
			type_id TEXT PRIMARY KEY,
			file_path TEXT NOT NULL,
			module_path TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			is_exported INTEGER NOT NULL DEFAULT 0,
			field_count INTEGER NOT NULL DEFAULT 0,
			method_count INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}

	// Schema 2.0: functions table WITHOUT start_pos/end_pos
	_, err = tx.Exec(`
		CREATE TABLE functions (
			function_id TEXT PRIMARY KEY,
			file_path TEXT NOT NULL,
			module_path TEXT NOT NULL,
			name TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			line_count INTEGER NOT NULL DEFAULT 0,
			is_exported INTEGER NOT NULL DEFAULT 0,
			is_method INTEGER NOT NULL DEFAULT 0,
			receiver_type_id TEXT,
			receiver_type_name TEXT,
			param_count INTEGER NOT NULL DEFAULT 0,
			return_count INTEGER NOT NULL DEFAULT 0,
			cyclomatic_complexity INTEGER,
			FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE,
			FOREIGN KEY (receiver_type_id) REFERENCES types(type_id) ON DELETE SET NULL
		)
	`)
	if err != nil {
		return err
	}

	// Create FTS table (exists in both 2.0 and 2.1, but 2.0 has no triggers)
	_, err = tx.Exec(`
		CREATE VIRTUAL TABLE files_fts USING fts5(
			file_path UNINDEXED,
			content,
			tokenize = "unicode61 separators '._'"
		)
	`)
	if err != nil {
		return err
	}

	// Create cache_metadata table
	_, err = tx.Exec(`
		CREATE TABLE cache_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	// Bootstrap metadata with schema version 2.0
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.Exec(`
		INSERT INTO cache_metadata (key, value, updated_at) VALUES
			('schema_version', '2.0', ?),
			('branch', 'main', ?),
			('last_indexed', '', ?),
			('embedding_dimensions', '384', ?)
	`, now, now, now, now)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// migrateSchema_2_0_to_2_1 performs the actual migration.
// Adds new columns with DEFAULT values for backward compatibility.
func migrateSchema_2_0_to_2_1(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Add byte position columns to types table
	_, err = tx.Exec(`ALTER TABLE types ADD COLUMN start_pos INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`ALTER TABLE types ADD COLUMN end_pos INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		return err
	}

	// Add byte position columns to functions table
	_, err = tx.Exec(`ALTER TABLE functions ADD COLUMN start_pos INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`ALTER TABLE functions ADD COLUMN end_pos INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		return err
	}

	// Add content column to files table (nullable for backward compatibility)
	_, err = tx.Exec(`ALTER TABLE files ADD COLUMN content TEXT`)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Create FTS triggers (must be outside transaction)
	err = createFTSTriggers(db)
	if err != nil {
		return err
	}

	// Update schema version to 2.1
	return UpdateSchemaVersion(db, "2.1")
}
