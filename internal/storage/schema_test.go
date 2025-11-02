package storage

// Test Plan for SQLite Schema:
// - CreateSchema creates all 12 tables successfully (files, files_fts, types, type_fields, functions, function_parameters, type_relationships, function_calls, imports, chunks, modules, cache_metadata)
// - CreateSchema creates all 31 indexes with idx_ prefix
// - Foreign key CASCADE deletes work (deleting file cascades to types)
// - Foreign key SET NULL works (deleting type sets function.receiver_type_id to NULL)
// - UNIQUE constraints prevent duplicate type_relationships (from_type_id, to_type_id, relationship_type)
// - UNIQUE constraints prevent duplicate imports (file_path, import_path)
// - FTS5 virtual table (files_fts) supports full-text search with MATCH operator
// - Bootstrap metadata is inserted correctly (schema_version=2.0, branch=main, embedding_dimensions=384, last_indexed=empty)
// - GetSchemaVersion returns "0" for new database without schema
// - GetSchemaVersion returns "2.0" after CreateSchema
// - UpdateSchemaVersion updates version in cache_metadata table
// - UpdateSchemaVersion updates updated_at timestamp

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSchema(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Should create schema without errors
	err := CreateSchema(db)
	require.NoError(t, err, "CreateSchema should succeed")

	// Verify all tables were created
	tables := []string{
		"files",
		"files_fts",
		"types",
		"type_fields",
		"functions",
		"function_parameters",
		"type_relationships",
		"function_calls",
		"imports",
		"chunks",
		"modules",
		"cache_metadata",
	}

	for _, table := range tables {
		exists := tableExists(t, db, table)
		assert.True(t, exists, "Table %s should exist", table)
	}
}

func TestCreateSchema_ForeignKeys(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	// Enable foreign keys for this connection
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Insert a file
	_, err = db.Exec(`
		INSERT INTO files (file_path, language, module_path, file_hash, last_modified, indexed_at)
		VALUES ('test.go', 'go', 'main', 'abc123', '2025-11-02T10:00:00Z', '2025-11-02T10:00:00Z')
	`)
	require.NoError(t, err)

	// Insert a type that references the file
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line)
		VALUES ('test::Handler', 'test.go', 'main', 'Handler', 'struct', 10, 20)
	`)
	require.NoError(t, err)

	// Try to delete the file - should fail due to CASCADE
	// Actually, CASCADE means child records are deleted, not that delete fails
	_, err = db.Exec("DELETE FROM files WHERE file_path = 'test.go'")
	require.NoError(t, err)

	// Verify the type was also deleted (CASCADE behavior)
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM types WHERE type_id = 'test::Handler'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "Type should be deleted via CASCADE")
}

func TestCreateSchema_ForeignKey_SetNull(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Insert file, type, and function
	_, err = db.Exec(`
		INSERT INTO files (file_path, language, module_path, file_hash, last_modified, indexed_at)
		VALUES ('test.go', 'go', 'main', 'abc123', '2025-11-02T10:00:00Z', '2025-11-02T10:00:00Z')
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line)
		VALUES ('test::Handler', 'test.go', 'main', 'Handler', 'struct', 10, 20)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO functions (
			function_id, file_path, module_path, name,
			start_line, end_line, is_method, receiver_type_id, receiver_type_name
		)
		VALUES (
			'test::Handle', 'test.go', 'main', 'Handle',
			25, 35, 1, 'test::Handler', 'Handler'
		)
	`)
	require.NoError(t, err)

	// Delete the type - function.receiver_type_id should become NULL (SET NULL)
	_, err = db.Exec("DELETE FROM types WHERE type_id = 'test::Handler'")
	require.NoError(t, err)

	// Function should still exist but receiver_type_id should be NULL
	var receiverTypeID sql.NullString
	err = db.QueryRow("SELECT receiver_type_id FROM functions WHERE function_id = 'test::Handle'").Scan(&receiverTypeID)
	require.NoError(t, err)
	assert.False(t, receiverTypeID.Valid, "receiver_type_id should be NULL after type deletion")
}

func TestCreateSchema_Indexes(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	// Query sqlite_master for indexes
	rows, err := db.Query(`
		SELECT name FROM sqlite_master
		WHERE type = 'index' AND name LIKE 'idx_%'
		ORDER BY name
	`)
	require.NoError(t, err)
	defer rows.Close()

	var indexes []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		indexes = append(indexes, name)
	}
	require.NoError(t, rows.Err())

	// Should have created all expected indexes
	expectedIndexes := []string{
		"idx_chunks_chunk_type",
		"idx_chunks_file_path",
		"idx_files_is_test",
		"idx_files_language",
		"idx_files_module",
		"idx_function_calls_callee",
		"idx_function_calls_callee_name",
		"idx_function_calls_caller",
		"idx_function_parameters_function_id",
		"idx_function_parameters_is_return",
		"idx_functions_file_path",
		"idx_functions_is_exported",
		"idx_functions_is_method",
		"idx_functions_module",
		"idx_functions_name",
		"idx_functions_receiver_type_id",
		"idx_imports_file_path",
		"idx_imports_import_path",
		"idx_imports_is_external",
		"idx_modules_depth",
		"idx_type_fields_is_method",
		"idx_type_fields_name",
		"idx_type_fields_type_id",
		"idx_type_relationships_from",
		"idx_type_relationships_to",
		"idx_type_relationships_type",
		"idx_types_file_path",
		"idx_types_is_exported",
		"idx_types_kind",
		"idx_types_module",
		"idx_types_name",
	}

	assert.ElementsMatch(t, expectedIndexes, indexes, "All indexes should be created")
}

func TestCreateSchema_BootstrapMetadata(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	// Verify bootstrap metadata
	tests := []struct {
		key      string
		expected string
	}{
		{"schema_version", "2.0"},
		{"branch", "main"},
		{"embedding_dimensions", "384"},
	}

	for _, tt := range tests {
		var value string
		err := db.QueryRow("SELECT value FROM cache_metadata WHERE key = ?", tt.key).Scan(&value)
		require.NoError(t, err)
		assert.Equal(t, tt.expected, value, "Metadata key %s should have expected value", tt.key)
	}

	// last_indexed should be empty initially
	var lastIndexed string
	err = db.QueryRow("SELECT value FROM cache_metadata WHERE key = 'last_indexed'").Scan(&lastIndexed)
	require.NoError(t, err)
	assert.Empty(t, lastIndexed, "last_indexed should be empty initially")
}

func TestCreateSchema_FTSTable(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	// Insert test data into files_fts
	_, err = db.Exec(`
		INSERT INTO files_fts (file_path, content)
		VALUES ('test.go', 'package main\n\nfunc Provider() error { return nil }')
	`)
	require.NoError(t, err)

	// Test FTS5 search
	var filePath string
	err = db.QueryRow(`
		SELECT file_path FROM files_fts
		WHERE content MATCH 'Provider'
	`).Scan(&filePath)
	require.NoError(t, err)
	assert.Equal(t, "test.go", filePath)
}

func TestCreateSchema_UniqueConstraints(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Insert file and types
	_, err = db.Exec(`
		INSERT INTO files (file_path, language, module_path, file_hash, last_modified, indexed_at)
		VALUES ('test.go', 'go', 'main', 'abc123', '2025-11-02T10:00:00Z', '2025-11-02T10:00:00Z')
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line)
		VALUES
			('test::Reader', 'test.go', 'main', 'Reader', 'interface', 10, 15),
			('test::FileReader', 'test.go', 'main', 'FileReader', 'struct', 20, 30)
	`)
	require.NoError(t, err)

	// Insert type_relationship
	_, err = db.Exec(`
		INSERT INTO type_relationships (
			relationship_id, from_type_id, to_type_id,
			relationship_type, source_file_path, source_line
		)
		VALUES (
			'rel1', 'test::FileReader', 'test::Reader',
			'implements', 'test.go', 25
		)
	`)
	require.NoError(t, err)

	// Try to insert duplicate relationship - should fail due to UNIQUE constraint
	_, err = db.Exec(`
		INSERT INTO type_relationships (
			relationship_id, from_type_id, to_type_id,
			relationship_type, source_file_path, source_line
		)
		VALUES (
			'rel2', 'test::FileReader', 'test::Reader',
			'implements', 'test.go', 25
		)
	`)
	assert.Error(t, err, "Duplicate relationship should fail UNIQUE constraint")
	assert.Contains(t, err.Error(), "UNIQUE", "Error should mention UNIQUE constraint")
}

func TestCreateSchema_ImportsUniqueConstraint(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Insert file
	_, err = db.Exec(`
		INSERT INTO files (file_path, language, module_path, file_hash, last_modified, indexed_at)
		VALUES ('test.go', 'go', 'main', 'abc123', '2025-11-02T10:00:00Z', '2025-11-02T10:00:00Z')
	`)
	require.NoError(t, err)

	// Insert import
	_, err = db.Exec(`
		INSERT INTO imports (import_id, file_path, import_path, import_line)
		VALUES ('imp1', 'test.go', 'fmt', 1)
	`)
	require.NoError(t, err)

	// Try to insert duplicate import - should fail
	_, err = db.Exec(`
		INSERT INTO imports (import_id, file_path, import_path, import_line)
		VALUES ('imp2', 'test.go', 'fmt', 1)
	`)
	assert.Error(t, err, "Duplicate import should fail UNIQUE constraint")
}

func TestGetSchemaVersion(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*sql.DB)
		expected string
		wantErr  bool
	}{
		{
			name: "new database",
			setup: func(db *sql.DB) {
				// Don't create schema
			},
			expected: "0",
			wantErr:  false,
		},
		{
			name: "schema created",
			setup: func(db *sql.DB) {
				err := CreateSchema(db)
				require.NoError(t, err)
			},
			expected: "2.0",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDB(t)
			defer db.Close()

			tt.setup(db)

			version, err := GetSchemaVersion(db)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, version)
			}
		})
	}
}

func TestUpdateSchemaVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	// Update to new version
	err = UpdateSchemaVersion(db, "3.0")
	require.NoError(t, err)

	// Verify update
	version, err := GetSchemaVersion(db)
	require.NoError(t, err)
	assert.Equal(t, "3.0", version)

	// Verify updated_at changed
	var updatedAt string
	err = db.QueryRow("SELECT updated_at FROM cache_metadata WHERE key = 'schema_version'").Scan(&updatedAt)
	require.NoError(t, err)
	assert.NotEmpty(t, updatedAt)
}

// Helper functions

func openTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	return db
}

func tableExists(t *testing.T, db *sql.DB, tableName string) bool {
	var count int
	query := `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type IN ('table', 'view') AND name = ?
	`
	err := db.QueryRow(query, tableName).Scan(&count)
	require.NoError(t, err)
	return count > 0
}
