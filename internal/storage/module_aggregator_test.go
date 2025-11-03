package storage

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for ModuleAggregator:
// - Can create ModuleAggregator with valid DB
// - Can aggregate single module from files table
// - Can aggregate single module including types counts
// - Can aggregate single module including function counts
// - Can aggregate single module including import counts
// - Can aggregate single module with exported symbol counts
// - Can calculate module depth correctly (0 for no slashes, N for N slashes)
// - Can handle empty modules gracefully
// - Can aggregate all modules at once
// - Incremental update only affects target module
// - Module deletion when last file removed
// - Module creation when first file added
// - Transaction safety (rollback on errors)
// - Idempotent operations (safe to run multiple times)
// - Handles nested module hierarchies correctly

// openModuleTestDB creates an in-memory SQLite database for module testing.
// Initializes vector extension to support FTS5 and vector tables.
func openModuleTestDB(t *testing.T) *sql.DB {
	InitVectorExtension() // Initialize globally for all tests
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	return db
}

func TestModuleAggregator_New(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	agg := NewModuleAggregator(db)
	require.NotNil(t, agg)
}

func TestModuleAggregator_AggregateModule_FileStats(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Test: Aggregate file statistics for a module
	files := []*FileStats{
		{
			FilePath:       "internal/indexer/parser.go",
			Language:       "go",
			ModulePath:     "internal/indexer",
			IsTest:         false,
			LineCountTotal: 250,
			LineCountCode:  180,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "internal/indexer/parser_test.go",
			Language:       "go",
			ModulePath:     "internal/indexer",
			IsTest:         true,
			LineCountTotal: 150,
			LineCountCode:  120,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
	}

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	// Verify aggregated stats
	var fileCount, testFileCount, lineCountTotal, lineCountCode, depth int
	err = db.QueryRow(`
		SELECT file_count, test_file_count, line_count_total, line_count_code, depth
		FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&fileCount, &testFileCount, &lineCountTotal, &lineCountCode, &depth)
	require.NoError(t, err)

	assert.Equal(t, 2, fileCount)
	assert.Equal(t, 1, testFileCount)
	assert.Equal(t, 400, lineCountTotal) // 250 + 150
	assert.Equal(t, 300, lineCountCode)  // 180 + 120
	assert.Equal(t, 1, depth)            // internal/indexer = 1 slash
}

func TestModuleAggregator_AggregateModule_Depth(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Test: Module depth calculation
	tests := []struct {
		modulePath    string
		expectedDepth int
	}{
		{"main", 0},
		{"pkg", 0},
		{"internal/indexer", 1},
		{"internal/pkg/auth", 2},
		{"a/b/c/d/e", 4},
	}

	for _, tt := range tests {
		stats := &FileStats{
			FilePath:       tt.modulePath + "/file.go",
			Language:       "go",
			ModulePath:     tt.modulePath,
			LineCountTotal: 100,
			LineCountCode:  80,
			FileHash:       "hash",
			LastModified:   now,
			IndexedAt:      now,
		}
		err := writer.WriteFileStats(stats)
		require.NoError(t, err)

		err = agg.AggregateModule(tt.modulePath)
		require.NoError(t, err)

		var depth int
		err = db.QueryRow(`
			SELECT depth FROM modules WHERE module_path = ?
		`, tt.modulePath).Scan(&depth)
		require.NoError(t, err, "module %s should exist", tt.modulePath)
		assert.Equal(t, tt.expectedDepth, depth, "module %s should have depth %d", tt.modulePath, tt.expectedDepth)
	}
}

func TestModuleAggregator_AggregateModule_TypeCounts(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Write file first
	stats := &FileStats{
		FilePath:       "internal/indexer/parser.go",
		Language:       "go",
		ModulePath:     "internal/indexer",
		LineCountTotal: 100,
		LineCountCode:  80,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Insert types directly
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported)
		VALUES
			('t1', 'internal/indexer/parser.go', 'internal/indexer', 'Parser', 'struct', 10, 20, 1),
			('t2', 'internal/indexer/parser.go', 'internal/indexer', 'handler', 'struct', 25, 30, 0),
			('t3', 'internal/indexer/parser.go', 'internal/indexer', 'Provider', 'interface', 35, 40, 1)
	`)
	require.NoError(t, err)

	// Test: Aggregate type counts
	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	var typeCount, exportedTypeCount int
	err = db.QueryRow(`
		SELECT type_count, exported_type_count FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&typeCount, &exportedTypeCount)
	require.NoError(t, err)

	assert.Equal(t, 3, typeCount)
	assert.Equal(t, 2, exportedTypeCount) // Parser, Provider
}

func TestModuleAggregator_AggregateModule_FunctionCounts(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Write file first
	stats := &FileStats{
		FilePath:       "internal/indexer/parser.go",
		Language:       "go",
		ModulePath:     "internal/indexer",
		LineCountTotal: 100,
		LineCountCode:  80,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Insert functions directly
	_, err = db.Exec(`
		INSERT INTO functions (function_id, file_path, module_path, name, start_line, end_line, is_exported)
		VALUES
			('f1', 'internal/indexer/parser.go', 'internal/indexer', 'Parse', 10, 20, 1),
			('f2', 'internal/indexer/parser.go', 'internal/indexer', 'helper', 25, 30, 0),
			('f3', 'internal/indexer/parser.go', 'internal/indexer', 'New', 35, 40, 1)
	`)
	require.NoError(t, err)

	// Test: Aggregate function counts
	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	var functionCount, exportedFunctionCount int
	err = db.QueryRow(`
		SELECT function_count, exported_function_count FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&functionCount, &exportedFunctionCount)
	require.NoError(t, err)

	assert.Equal(t, 3, functionCount)
	assert.Equal(t, 2, exportedFunctionCount) // Parse, New
}

func TestModuleAggregator_AggregateModule_ImportCounts(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Write files first
	files := []*FileStats{
		{
			FilePath:       "internal/indexer/parser.go",
			Language:       "go",
			ModulePath:     "internal/indexer",
			LineCountTotal: 100,
			LineCountCode:  80,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "internal/indexer/chunker.go",
			Language:       "go",
			ModulePath:     "internal/indexer",
			LineCountTotal: 80,
			LineCountCode:  60,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
	}
	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	// Insert imports directly
	_, err = db.Exec(`
		INSERT INTO imports (import_id, file_path, import_path, is_standard_lib, is_external, import_line)
		VALUES
			('i1', 'internal/indexer/parser.go', 'fmt', 1, 0, 3),
			('i2', 'internal/indexer/parser.go', 'github.com/foo/bar', 0, 1, 4),
			('i3', 'internal/indexer/parser.go', 'github.com/baz/qux', 0, 1, 5),
			('i4', 'internal/indexer/chunker.go', 'fmt', 1, 0, 3),
			('i5', 'internal/indexer/chunker.go', 'github.com/foo/bar', 0, 1, 4)
	`)
	require.NoError(t, err)

	// Test: Aggregate import counts
	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	var importCount, externalImportCount int
	err = db.QueryRow(`
		SELECT import_count, external_import_count FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&importCount, &externalImportCount)
	require.NoError(t, err)

	assert.Equal(t, 5, importCount)            // Total imports across all files
	assert.Equal(t, 2, externalImportCount) // Unique external imports: github.com/foo/bar (2x), github.com/baz/qux
}

func TestModuleAggregator_AggregateModule_EmptyModule(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	agg := NewModuleAggregator(db)

	// Test: Aggregating non-existent module should not error
	err = agg.AggregateModule("nonexistent/module")
	require.NoError(t, err)

	// Verify no module entry created
	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM modules WHERE module_path = ?
	`, "nonexistent/module").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestModuleAggregator_AggregateAllModules(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Create files in multiple modules
	files := []*FileStats{
		{
			FilePath:       "internal/indexer/parser.go",
			Language:       "go",
			ModulePath:     "internal/indexer",
			LineCountTotal: 250,
			LineCountCode:  180,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "internal/mcp/server.go",
			Language:       "go",
			ModulePath:     "internal/mcp",
			LineCountTotal: 300,
			LineCountCode:  220,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "pkg/utils/helper.go",
			Language:       "go",
			ModulePath:     "pkg/utils",
			LineCountTotal: 80,
			LineCountCode:  60,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
	}
	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	// Test: Aggregate all modules at once
	err = agg.AggregateAllModules()
	require.NoError(t, err)

	// Verify all modules created
	var moduleCount int
	err = db.QueryRow("SELECT COUNT(*) FROM modules").Scan(&moduleCount)
	require.NoError(t, err)
	assert.Equal(t, 3, moduleCount)

	// Verify each module has correct stats
	modules := []string{"internal/indexer", "internal/mcp", "pkg/utils"}
	for _, modulePath := range modules {
		var fileCount int
		err = db.QueryRow(`
			SELECT file_count FROM modules WHERE module_path = ?
		`, modulePath).Scan(&fileCount)
		require.NoError(t, err, "module %s should exist", modulePath)
		assert.Equal(t, 1, fileCount)
	}
}

func TestModuleAggregator_AggregateModule_Incremental(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Create initial modules
	files := []*FileStats{
		{
			FilePath:       "internal/indexer/parser.go",
			Language:       "go",
			ModulePath:     "internal/indexer",
			LineCountTotal: 250,
			LineCountCode:  180,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "internal/mcp/server.go",
			Language:       "go",
			ModulePath:     "internal/mcp",
			LineCountTotal: 300,
			LineCountCode:  220,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
	}
	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	err = agg.AggregateAllModules()
	require.NoError(t, err)

	// Test: Incremental update of one module doesn't affect others
	newFile := &FileStats{
		FilePath:       "internal/indexer/chunker.go",
		Language:       "go",
		ModulePath:     "internal/indexer",
		LineCountTotal: 200,
		LineCountCode:  150,
		FileHash:       "hash3",
		LastModified:   now,
		IndexedAt:      now,
	}
	err = writer.WriteFileStats(newFile)
	require.NoError(t, err)

	// Get initial updated_at for internal/mcp
	var initialUpdatedAt string
	err = db.QueryRow(`
		SELECT updated_at FROM modules WHERE module_path = ?
	`, "internal/mcp").Scan(&initialUpdatedAt)
	require.NoError(t, err)

	// Wait a bit to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	// Re-aggregate only internal/indexer
	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	// Verify internal/indexer updated
	var indexerFileCount, indexerLineCount int
	err = db.QueryRow(`
		SELECT file_count, line_count_total FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&indexerFileCount, &indexerLineCount)
	require.NoError(t, err)
	assert.Equal(t, 2, indexerFileCount)
	assert.Equal(t, 450, indexerLineCount) // 250 + 200

	// Verify internal/mcp unchanged (timestamp should be same)
	var mcpUpdatedAt string
	err = db.QueryRow(`
		SELECT updated_at FROM modules WHERE module_path = ?
	`, "internal/mcp").Scan(&mcpUpdatedAt)
	require.NoError(t, err)
	assert.Equal(t, initialUpdatedAt, mcpUpdatedAt, "internal/mcp timestamp should not change")
}

func TestModuleAggregator_AggregateModule_ModuleDeletion(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Create module with one file
	stats := &FileStats{
		FilePath:       "internal/indexer/parser.go",
		Language:       "go",
		ModulePath:     "internal/indexer",
		LineCountTotal: 250,
		LineCountCode:  180,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	// Verify module exists
	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Test: Remove last file, then re-aggregate should delete module
	err = writer.DeleteFile("internal/indexer/parser.go")
	require.NoError(t, err)

	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	// Verify module deleted
	err = db.QueryRow(`
		SELECT COUNT(*) FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestModuleAggregator_AggregateModule_ModuleCreation(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Test: Creating first file should create module on aggregation
	stats := &FileStats{
		FilePath:       "internal/indexer/parser.go",
		Language:       "go",
		ModulePath:     "internal/indexer",
		LineCountTotal: 250,
		LineCountCode:  180,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Module doesn't exist yet
	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Aggregate creates the module
	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	// Verify module created
	var fileCount int
	err = db.QueryRow(`
		SELECT file_count FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&fileCount)
	require.NoError(t, err)
	assert.Equal(t, 1, fileCount)
}

func TestModuleAggregator_AggregateModule_Idempotent(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	stats := &FileStats{
		FilePath:       "internal/indexer/parser.go",
		Language:       "go",
		ModulePath:     "internal/indexer",
		LineCountTotal: 250,
		LineCountCode:  180,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Test: Multiple aggregations produce same result
	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	var firstFileCount, firstLineCount int
	err = db.QueryRow(`
		SELECT file_count, line_count_total FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&firstFileCount, &firstLineCount)
	require.NoError(t, err)

	// Aggregate again
	err = agg.AggregateModule("internal/indexer")
	require.NoError(t, err)

	var secondFileCount, secondLineCount int
	err = db.QueryRow(`
		SELECT file_count, line_count_total FROM modules WHERE module_path = ?
	`, "internal/indexer").Scan(&secondFileCount, &secondLineCount)
	require.NoError(t, err)

	assert.Equal(t, firstFileCount, secondFileCount)
	assert.Equal(t, firstLineCount, secondLineCount)
}

func TestModuleAggregator_AggregateModule_ComplexHierarchy(t *testing.T) {
	t.Parallel()

	db := openModuleTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()
	agg := NewModuleAggregator(db)

	now := time.Now().UTC()

	// Test: Complex nested module hierarchy
	files := []*FileStats{
		{
			FilePath:       "main.go",
			Language:       "go",
			ModulePath:     "main",
			LineCountTotal: 50,
			LineCountCode:  40,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "internal/server.go",
			Language:       "go",
			ModulePath:     "internal",
			LineCountTotal: 100,
			LineCountCode:  80,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "internal/pkg/auth/handler.go",
			Language:       "go",
			ModulePath:     "internal/pkg/auth",
			LineCountTotal: 150,
			LineCountCode:  120,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "internal/pkg/auth/middleware.go",
			Language:       "go",
			ModulePath:     "internal/pkg/auth",
			LineCountTotal: 80,
			LineCountCode:  60,
			FileHash:       "hash4",
			LastModified:   now,
			IndexedAt:      now,
		},
	}
	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	err = agg.AggregateAllModules()
	require.NoError(t, err)

	// Verify hierarchy
	tests := []struct {
		modulePath     string
		expectedFiles  int
		expectedDepth  int
		expectedLines  int
	}{
		{"main", 1, 0, 50},
		{"internal", 1, 0, 100},
		{"internal/pkg/auth", 2, 2, 230}, // 150 + 80
	}

	for _, tt := range tests {
		var fileCount, depth, lineCount int
		err := db.QueryRow(`
			SELECT file_count, depth, line_count_total FROM modules WHERE module_path = ?
		`, tt.modulePath).Scan(&fileCount, &depth, &lineCount)
		require.NoError(t, err, "module %s should exist", tt.modulePath)
		assert.Equal(t, tt.expectedFiles, fileCount, "module %s file count", tt.modulePath)
		assert.Equal(t, tt.expectedDepth, depth, "module %s depth", tt.modulePath)
		assert.Equal(t, tt.expectedLines, lineCount, "module %s line count", tt.modulePath)
	}
}
