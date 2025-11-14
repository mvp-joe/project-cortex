package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateSchema creates all tables, indexes, and virtual tables for the unified cache.
// Uses transactions for atomicity - all schema creation succeeds or fails together.
//
// Schema includes:
//   - 10 core tables (files, types, functions, chunks, etc.)
//   - FTS5 virtual table for full-text search (chunks_fts)
//   - sqlite-vec virtual table for vector similarity search (chunks_vec)
//   - All foreign key constraints and indexes
//   - Bootstrap metadata
//
// Must be called with SQLite PRAGMA foreign_keys = ON.
// Note: sqlite-vec extension must be initialized before calling this (InitVectorExtension).
func CreateSchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin schema transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// Enable foreign keys (must be set for each connection)
	if _, err := tx.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create all tables in dependency order
	tables := []struct {
		name string
		ddl  string
	}{
		{"files", createFilesTable},
		{"files_fts", createFilesFTSTable},
		{"types", createTypesTable},
		{"type_fields", createTypeFieldsTable},
		{"functions", createFunctionsTable},
		{"function_parameters", createFunctionParametersTable},
		{"type_relationships", createTypeRelationshipsTable},
		{"function_calls", createFunctionCallsTable},
		{"imports", createImportsTable},
		{"chunks", createChunksTable},
		{"cache_metadata", createCacheMetadataTable},
	}

	for _, table := range tables {
		if _, err := tx.Exec(table.ddl); err != nil {
			return fmt.Errorf("failed to create %s table: %w", table.name, err)
		}
	}

	// Create all indexes
	indexes := getAllIndexes()
	for i, idx := range indexes {
		if _, err := tx.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index %d: %w", i+1, err)
		}
	}

	// Commit transaction before creating virtual tables
	// (FTS5 and vec0 virtual tables must be created outside transaction)
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit schema transaction: %w", err)
	}

	// Note: files_fts virtual table is already created above in tables list
	// No need to call CreateFTSIndex() - it's part of schema creation

	// Create sqlite-vec virtual table for vector similarity search
	// Get embedding dimensions from metadata (default 384 for BGE-small)
	dimensions := 384
	if err := CreateVectorIndex(db, dimensions); err != nil {
		return fmt.Errorf("failed to create vector index: %w", err)
	}

	// Create triggers for FTS sync (must be outside transaction)
	if err := createFTSTriggers(db); err != nil {
		return fmt.Errorf("failed to create FTS triggers: %w", err)
	}

	// Bootstrap cache_metadata in separate transaction
	tx, err = db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin metadata transaction: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	bootstrapSQL := `
		INSERT INTO cache_metadata (key, value, updated_at) VALUES
			('schema_version', '2.1', ?),
			('branch', 'main', ?),
			('last_indexed', '', ?),
			('embedding_dimensions', '384', ?)
	`
	if _, err := tx.Exec(bootstrapSQL, now, now, now, now); err != nil {
		return fmt.Errorf("failed to bootstrap cache_metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit schema transaction: %w", err)
	}

	return nil
}

// GetSchemaVersion retrieves the schema version from cache_metadata.
// Returns "0" if the table doesn't exist (new database).
func GetSchemaVersion(db *sql.DB) (string, error) {
	// First check if cache_metadata table exists
	var tableExists int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cache_metadata'").Scan(&tableExists)
	if err != nil {
		return "", fmt.Errorf("failed to check cache_metadata existence: %w", err)
	}
	if tableExists == 0 {
		return "0", nil // New database
	}

	// Table exists, query for version
	var version string
	query := "SELECT value FROM cache_metadata WHERE key = 'schema_version'"
	err = db.QueryRow(query).Scan(&version)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("schema_version key not found in cache_metadata")
	}
	if err != nil {
		return "", fmt.Errorf("failed to query schema version: %w", err)
	}
	return version, nil
}

// UpdateSchemaVersion sets or updates the schema version in cache_metadata.
func UpdateSchemaVersion(db *sql.DB, version string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	query := `
		INSERT INTO cache_metadata (key, value, updated_at)
		VALUES ('schema_version', ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`
	_, err := db.Exec(query, version, now)
	if err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}
	return nil
}

// Table DDL constants

const createFilesTable = `
CREATE TABLE files (
    file_path TEXT PRIMARY KEY,                  -- Natural key: relative path from repo root
    language TEXT NOT NULL,                      -- go, typescript, python, etc.
    module_path TEXT NOT NULL,                   -- Package/module (denormalized for perf)
    is_test INTEGER NOT NULL DEFAULT 0,          -- Boolean: Is this a test file?
    line_count_total INTEGER NOT NULL DEFAULT 0, -- Total lines
    line_count_code INTEGER NOT NULL DEFAULT 0,  -- Code lines (excludes comments/blank)
    line_count_comment INTEGER NOT NULL DEFAULT 0,
    line_count_blank INTEGER NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    file_hash TEXT NOT NULL,                     -- SHA-256 for change detection
    last_modified TEXT NOT NULL,                 -- ISO 8601 mtime from filesystem
    indexed_at TEXT NOT NULL,                    -- ISO 8601 when this file was indexed
    content TEXT                                 -- Full file content (NULL for binary files)
)
`

const createFilesFTSTable = `
CREATE VIRTUAL TABLE files_fts USING fts5(
    file_path UNINDEXED,                         -- FK to files.file_path (for display)
    content,                                     -- Full file content (synced via triggers from files.content)
    tokenize = "unicode61 separators '._'"       -- Tokenize on underscore and dot
)
`

const createTypesTable = `
CREATE TABLE types (
    type_id TEXT PRIMARY KEY,                    -- {file_path}::{name} or UUID
    file_path TEXT NOT NULL,
    module_path TEXT NOT NULL,                   -- Denormalized from files for perf
    name TEXT NOT NULL,                          -- Type name
    kind TEXT NOT NULL,                          -- interface, struct, class, enum
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    start_pos INTEGER NOT NULL DEFAULT 0,        -- 0-indexed byte offset of type start
    end_pos INTEGER NOT NULL DEFAULT 0,          -- 0-indexed byte offset of type end
    is_exported INTEGER NOT NULL DEFAULT 0,      -- Boolean: Uppercase first letter in Go
    field_count INTEGER NOT NULL DEFAULT 0,      -- Denormalized count
    method_count INTEGER NOT NULL DEFAULT 0,     -- Denormalized count
    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
)
`

const createTypeFieldsTable = `
CREATE TABLE type_fields (
    field_id TEXT PRIMARY KEY,                   -- UUID or {type_id}::{name}
    type_id TEXT NOT NULL,
    name TEXT NOT NULL,
    field_type TEXT NOT NULL,                    -- string, int, *User, etc.
    position INTEGER NOT NULL,                   -- 0-indexed position in type
    is_method INTEGER NOT NULL DEFAULT 0,        -- Boolean: Interface method vs struct field
    is_exported INTEGER NOT NULL DEFAULT 0,      -- Boolean
    param_count INTEGER,                         -- For methods: parameter count
    return_count INTEGER,                        -- For methods: return value count
    FOREIGN KEY (type_id) REFERENCES types(type_id) ON DELETE CASCADE
)
`

const createFunctionsTable = `
CREATE TABLE functions (
    function_id TEXT PRIMARY KEY,                -- {file_path}::{name} or UUID
    file_path TEXT NOT NULL,
    module_path TEXT NOT NULL,                   -- Denormalized for perf
    name TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    start_pos INTEGER NOT NULL DEFAULT 0,        -- 0-indexed byte offset of function start
    end_pos INTEGER NOT NULL DEFAULT 0,          -- 0-indexed byte offset of function end
    line_count INTEGER NOT NULL DEFAULT 0,       -- end_line - start_line
    is_exported INTEGER NOT NULL DEFAULT 0,      -- Boolean
    is_method INTEGER NOT NULL DEFAULT 0,        -- Boolean: Has receiver?
    receiver_type_id TEXT,                       -- FK to types (for methods)
    receiver_type_name TEXT,                     -- Denormalized for queries
    param_count INTEGER NOT NULL DEFAULT 0,      -- Denormalized count
    return_count INTEGER NOT NULL DEFAULT 0,     -- Denormalized count
    cyclomatic_complexity INTEGER,               -- Optional complexity metric
    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE,
    FOREIGN KEY (receiver_type_id) REFERENCES types(type_id) ON DELETE SET NULL
)
`

const createFunctionParametersTable = `
CREATE TABLE function_parameters (
    param_id TEXT PRIMARY KEY,                   -- UUID or {function_id}::param{N}
    function_id TEXT NOT NULL,
    name TEXT,                                   -- NULL for unnamed return values
    param_type TEXT NOT NULL,                    -- string, *User, error, etc.
    position INTEGER NOT NULL,                   -- 0-indexed
    is_return INTEGER NOT NULL DEFAULT 0,        -- Boolean: Parameter vs return value
    is_variadic INTEGER NOT NULL DEFAULT 0,      -- Boolean: ...args
    FOREIGN KEY (function_id) REFERENCES functions(function_id) ON DELETE CASCADE
)
`

const createTypeRelationshipsTable = `
CREATE TABLE type_relationships (
    relationship_id TEXT PRIMARY KEY,            -- UUID
    from_type_id TEXT NOT NULL,                  -- Source type
    to_type_id TEXT NOT NULL,                    -- Target type
    relationship_type TEXT NOT NULL,             -- implements, embeds, extends
    source_file_path TEXT NOT NULL,              -- Where relationship is declared
    source_line INTEGER NOT NULL,
    FOREIGN KEY (from_type_id) REFERENCES types(type_id) ON DELETE CASCADE,
    FOREIGN KEY (to_type_id) REFERENCES types(type_id) ON DELETE CASCADE,
    FOREIGN KEY (source_file_path) REFERENCES files(file_path) ON DELETE CASCADE,
    UNIQUE(from_type_id, to_type_id, relationship_type)
)
`

const createFunctionCallsTable = `
CREATE TABLE function_calls (
    call_id TEXT PRIMARY KEY,                    -- UUID
    caller_function_id TEXT NOT NULL,            -- Who is calling
    callee_function_id TEXT,                     -- What is being called (NULL if external/unknown)
    callee_name TEXT NOT NULL,                   -- Function name (for external calls)
    source_file_path TEXT NOT NULL,              -- Where call occurs
    call_line INTEGER NOT NULL,
    call_column INTEGER,                         -- Optional column number
    FOREIGN KEY (caller_function_id) REFERENCES functions(function_id) ON DELETE CASCADE,
    FOREIGN KEY (callee_function_id) REFERENCES functions(function_id) ON DELETE SET NULL,
    FOREIGN KEY (source_file_path) REFERENCES files(file_path) ON DELETE CASCADE
)
`

const createImportsTable = `
CREATE TABLE imports (
    import_id TEXT PRIMARY KEY,                  -- UUID or {file_path}::{import_path}
    file_path TEXT NOT NULL,
    import_path TEXT NOT NULL,                   -- github.com/user/pkg, ./local, etc.
    is_standard_lib INTEGER NOT NULL DEFAULT 0,  -- Boolean: Part of language stdlib
    is_external INTEGER NOT NULL DEFAULT 0,      -- Boolean: Third-party dependency
    is_relative INTEGER NOT NULL DEFAULT 0,      -- Boolean: ./pkg, ../other
    import_line INTEGER NOT NULL,
    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE,
    UNIQUE(file_path, import_path)
)
`

const createChunksTable = `
CREATE TABLE chunks (
    chunk_id TEXT PRIMARY KEY,                   -- code-symbols-{file_path}, doc-{file}-s{N}
    file_path TEXT NOT NULL,                     -- FK to files
    chunk_type TEXT NOT NULL,                    -- symbols, definitions, data, documentation
    title TEXT NOT NULL,                         -- Human-readable title
    text TEXT NOT NULL,                          -- Natural language formatted content
    embedding BLOB NOT NULL,                     -- Float32 array, serialized (4 bytes per float)
    start_line INTEGER,                          -- NULL for file-level chunks
    end_line INTEGER,
    created_at TEXT NOT NULL,                    -- ISO 8601
    updated_at TEXT NOT NULL,                    -- ISO 8601
    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
)
`

const createCacheMetadataTable = `
CREATE TABLE cache_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
)
`

// getAllIndexes returns all index creation statements.
func getAllIndexes() []string {
	return []string{
		// files table indexes
		"CREATE INDEX idx_files_language ON files(language)",
		"CREATE INDEX idx_files_module ON files(module_path)",
		"CREATE INDEX idx_files_is_test ON files(is_test)",

		// types table indexes
		"CREATE INDEX idx_types_file_path ON types(file_path)",
		"CREATE INDEX idx_types_module ON types(module_path)",
		"CREATE INDEX idx_types_name ON types(name)",
		"CREATE INDEX idx_types_kind ON types(kind)",
		"CREATE INDEX idx_types_is_exported ON types(is_exported)",

		// type_fields table indexes
		"CREATE INDEX idx_type_fields_type_id ON type_fields(type_id)",
		"CREATE INDEX idx_type_fields_name ON type_fields(name)",
		"CREATE INDEX idx_type_fields_is_method ON type_fields(is_method)",

		// functions table indexes
		"CREATE INDEX idx_functions_file_path ON functions(file_path)",
		"CREATE INDEX idx_functions_module ON functions(module_path)",
		"CREATE INDEX idx_functions_name ON functions(name)",
		"CREATE INDEX idx_functions_is_exported ON functions(is_exported)",
		"CREATE INDEX idx_functions_is_method ON functions(is_method)",
		"CREATE INDEX idx_functions_receiver_type_id ON functions(receiver_type_id)",

		// function_parameters table indexes
		"CREATE INDEX idx_function_parameters_function_id ON function_parameters(function_id)",
		"CREATE INDEX idx_function_parameters_is_return ON function_parameters(is_return)",

		// type_relationships table indexes
		"CREATE INDEX idx_type_relationships_from ON type_relationships(from_type_id)",
		"CREATE INDEX idx_type_relationships_to ON type_relationships(to_type_id)",
		"CREATE INDEX idx_type_relationships_type ON type_relationships(relationship_type)",

		// function_calls table indexes
		"CREATE INDEX idx_function_calls_caller ON function_calls(caller_function_id)",
		"CREATE INDEX idx_function_calls_callee ON function_calls(callee_function_id)",
		"CREATE INDEX idx_function_calls_callee_name ON function_calls(callee_name)",

		// imports table indexes
		"CREATE INDEX idx_imports_file_path ON imports(file_path)",
		"CREATE INDEX idx_imports_import_path ON imports(import_path)",
		"CREATE INDEX idx_imports_is_external ON imports(is_external)",

		// chunks table indexes
		"CREATE INDEX idx_chunks_file_path ON chunks(file_path)",
		"CREATE INDEX idx_chunks_chunk_type ON chunks(chunk_type)",
	}
}

// createFTSTriggers creates triggers to automatically sync files table content to files_fts.
// Only syncs text files (where content IS NOT NULL).
// Binary files (content IS NULL) are excluded from FTS.
//
// Strategy: Store content in both files.content and files_fts, but use triggers to
// keep them in sync. This allows us to selectively index only text files.
func createFTSTriggers(db *sql.DB) error {
	triggers := []string{
		// Insert trigger: sync new text files to FTS
		// Note: When using INSERT OR REPLACE, this trigger fires after the internal DELETE,
		// so we use INSERT OR REPLACE to handle both new files and updates.
		`CREATE TRIGGER files_fts_insert AFTER INSERT ON files
		BEGIN
			-- Delete old FTS entry first (in case of INSERT OR REPLACE)
			DELETE FROM files_fts WHERE file_path = NEW.file_path;

			-- Insert new FTS entry only if content is not NULL
			INSERT INTO files_fts(file_path, content)
			SELECT NEW.file_path, NEW.content
			WHERE NEW.content IS NOT NULL;
		END`,

		// Update trigger: handle content changes (for explicit UPDATE statements)
		// Note: This won't fire for INSERT OR REPLACE, only for UPDATE statements
		`CREATE TRIGGER files_fts_update AFTER UPDATE OF content ON files
		BEGIN
			-- Delete old FTS entry (if it exists)
			DELETE FROM files_fts WHERE file_path = OLD.file_path;

			-- Insert new FTS entry only if NEW.content is not NULL
			INSERT INTO files_fts(file_path, content)
			SELECT NEW.file_path, NEW.content
			WHERE NEW.content IS NOT NULL;
		END`,

		// Delete trigger: remove from FTS when file deleted (only if it had content)
		`CREATE TRIGGER files_fts_delete AFTER DELETE ON files
		WHEN OLD.content IS NOT NULL
		BEGIN
			DELETE FROM files_fts WHERE file_path = OLD.file_path;
		END`,
	}

	for i, trigger := range triggers {
		if _, err := db.Exec(trigger); err != nil {
			return fmt.Errorf("failed to create trigger %d: %w", i+1, err)
		}
	}

	return nil
}
