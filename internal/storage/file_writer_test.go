package storage

import (
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for FileWriter:
// - Can create new FileWriter instance with valid DB path
// - Can write single file statistics
// - Can write file statistics in batch
// - Can write file content to FTS5 table
// - Can write file content in batch
// - Can delete file (cascades to FTS5)
// - Can update module statistics via aggregation
// - Module aggregation calculates correct counts
// - Module aggregation calculates depth correctly
// - INSERT OR REPLACE updates existing files
// - Prepared statements used for performance
// - Transactions used for batch operations
// - Close releases resources properly
// - Error handling for invalid inputs

func TestFileWriter_New(t *testing.T) {
	t.Parallel()

	// Test: Creating FileWriter with valid in-memory DB succeeds
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	require.NotNil(t, writer)

	err = writer.Close()
	require.NoError(t, err)
}

func TestFileWriter_WriteFileStats(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: Write single file statistics
	now := time.Now().UTC()
	stats := &FileStats{
		FilePath:         "internal/indexer/parser.go",
		Language:         "go",
		ModulePath:       "internal/indexer",
		IsTest:           false,
		LineCountTotal:   250,
		LineCountCode:    180,
		LineCountComment: 40,
		LineCountBlank:   30,
		SizeBytes:        8192,
		FileHash:         "abc123def456",
		LastModified:     now,
		IndexedAt:        now,
	}

	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Verify data was written
	var filePath, language, modulePath string
	var isTest bool
	var lineCountTotal, lineCountCode int
	err = db.QueryRow(`
		SELECT file_path, language, module_path, is_test, line_count_total, line_count_code
		FROM files WHERE file_path = ?
	`, stats.FilePath).Scan(&filePath, &language, &modulePath, &isTest, &lineCountTotal, &lineCountCode)
	require.NoError(t, err)

	assert.Equal(t, stats.FilePath, filePath)
	assert.Equal(t, stats.Language, language)
	assert.Equal(t, stats.ModulePath, modulePath)
	assert.Equal(t, stats.IsTest, isTest)
	assert.Equal(t, stats.LineCountTotal, lineCountTotal)
	assert.Equal(t, stats.LineCountCode, lineCountCode)
}

func TestFileWriter_WriteFileStats_Replace(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()
	stats := &FileStats{
		FilePath:       "test.go",
		Language:       "go",
		ModulePath:     "main",
		LineCountTotal: 100,
		LineCountCode:  80,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}

	// Write first time
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Test: Update existing file (INSERT OR REPLACE)
	stats.LineCountTotal = 150
	stats.LineCountCode = 120
	stats.FileHash = "hash2"
	stats.IndexedAt = now.Add(time.Hour)

	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Verify updated values
	var lineCountTotal, lineCountCode int
	var fileHash string
	err = db.QueryRow(`
		SELECT line_count_total, line_count_code, file_hash
		FROM files WHERE file_path = ?
	`, stats.FilePath).Scan(&lineCountTotal, &lineCountCode, &fileHash)
	require.NoError(t, err)

	assert.Equal(t, 150, lineCountTotal)
	assert.Equal(t, 120, lineCountCode)
	assert.Equal(t, "hash2", fileHash)
}

func TestFileWriter_WriteFileStatsBatch(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: Write multiple files in batch transaction
	now := time.Now().UTC()
	batch := []*FileStats{
		{
			FilePath:       "file1.go",
			Language:       "go",
			ModulePath:     "pkg/a",
			LineCountTotal: 100,
			LineCountCode:  80,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "file2.go",
			Language:       "go",
			ModulePath:     "pkg/b",
			IsTest:         true,
			LineCountTotal: 50,
			LineCountCode:  40,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "file3.ts",
			Language:       "typescript",
			ModulePath:     "src/components",
			LineCountTotal: 200,
			LineCountCode:  150,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
	}

	err = writer.WriteFileStatsBatch(batch)
	require.NoError(t, err)

	// Verify all files written
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM files").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Verify specific file
	var language string
	var isTest bool
	err = db.QueryRow(`
		SELECT language, is_test FROM files WHERE file_path = ?
	`, "file2.go").Scan(&language, &isTest)
	require.NoError(t, err)
	assert.Equal(t, "go", language)
	assert.True(t, isTest)
}

func TestFileWriter_WriteFileContent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: Write file content to FTS5
	content := &FileContent{
		FilePath: "internal/indexer/parser.go",
		Content:  "package indexer\n\ntype Provider interface {\n\tEmbed(ctx context.Context) error\n}",
	}

	err = writer.WriteFileContent(content)
	require.NoError(t, err)

	// Verify content was indexed
	var filePath string
	err = db.QueryRow(`
		SELECT file_path FROM files_fts WHERE content MATCH 'Provider'
	`).Scan(&filePath)
	require.NoError(t, err)
	assert.Equal(t, content.FilePath, filePath)
}

func TestFileWriter_WriteFileContent_Replace(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	content := &FileContent{
		FilePath: "test.go",
		Content:  "package main\n\nfunc Handler() {}",
	}

	// Write first time
	err = writer.WriteFileContent(content)
	require.NoError(t, err)

	// Test: Replace content for same file
	content.Content = "package main\n\nfunc Provider() {}"
	err = writer.WriteFileContent(content)
	require.NoError(t, err)

	// Verify only one entry exists
	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, "test.go").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify new content is searchable
	var found bool
	err = db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM files_fts WHERE file_path = ? AND content MATCH 'Provider')
	`, "test.go").Scan(&found)
	require.NoError(t, err)
	assert.True(t, found)

	// Verify old content not searchable
	err = db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM files_fts WHERE file_path = ? AND content MATCH 'Handler')
	`, "test.go").Scan(&found)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestFileWriter_WriteFileContentBatch(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: Write multiple file contents in batch
	batch := []*FileContent{
		{
			FilePath: "file1.go",
			Content:  "package a\n\ntype Handler struct {}",
		},
		{
			FilePath: "file2.go",
			Content:  "package b\n\ntype Provider interface {}",
		},
		{
			FilePath: "file3.ts",
			Content:  "export class Component {}",
		},
	}

	err = writer.WriteFileContentBatch(batch)
	require.NoError(t, err)

	// Verify all indexed
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM files_fts").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Verify searchable
	var filePath string
	err = db.QueryRow(`
		SELECT file_path FROM files_fts WHERE content MATCH 'Provider'
	`).Scan(&filePath)
	require.NoError(t, err)
	assert.Equal(t, "file2.go", filePath)
}

func TestFileWriter_DeleteFile(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

	// Write file stats and content
	stats := &FileStats{
		FilePath:       "test.go",
		Language:       "go",
		ModulePath:     "main",
		LineCountTotal: 100,
		LineCountCode:  80,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	content := &FileContent{
		FilePath: "test.go",
		Content:  "package main",
	}
	err = writer.WriteFileContent(content)
	require.NoError(t, err)

	// Test: Delete file removes from both tables
	err = writer.DeleteFile("test.go")
	require.NoError(t, err)

	// Verify removed from files table
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM files WHERE file_path = ?", "test.go").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify removed from FTS5 table
	err = db.QueryRow("SELECT COUNT(*) FROM files_fts WHERE file_path = ?", "test.go").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestFileWriter_UpdateModuleStats(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

	// Create files in different modules
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
		{
			FilePath:       "internal/mcp/server.go",
			Language:       "go",
			ModulePath:     "internal/mcp",
			IsTest:         false,
			LineCountTotal: 300,
			LineCountCode:  220,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "pkg/utils/helper.go",
			Language:       "go",
			ModulePath:     "pkg/utils",
			IsTest:         false,
			LineCountTotal: 80,
			LineCountCode:  60,
			FileHash:       "hash4",
			LastModified:   now,
			IndexedAt:      now,
		},
	}

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	// Test: UpdateModuleStats aggregates correctly
	err = writer.UpdateModuleStats()
	require.NoError(t, err)

	// Verify module count
	var moduleCount int
	err = db.QueryRow("SELECT COUNT(*) FROM modules").Scan(&moduleCount)
	require.NoError(t, err)
	assert.Equal(t, 3, moduleCount)

	// Verify internal/indexer module stats
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
	assert.Equal(t, 1, depth)            // internal/indexer = depth 1

	// Verify pkg/utils module stats
	err = db.QueryRow(`
		SELECT file_count, test_file_count, line_count_total, depth
		FROM modules WHERE module_path = ?
	`, "pkg/utils").Scan(&fileCount, &testFileCount, &lineCountTotal, &depth)
	require.NoError(t, err)

	assert.Equal(t, 1, fileCount)
	assert.Equal(t, 0, testFileCount)
	assert.Equal(t, 80, lineCountTotal)
	assert.Equal(t, 1, depth) // pkg/utils = depth 1
}

func TestFileWriter_UpdateModuleStats_Depth(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

	// Test: Module depth calculated correctly
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
	}

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	err = writer.UpdateModuleStats()
	require.NoError(t, err)

	// Verify depths
	tests := []struct {
		modulePath    string
		expectedDepth int
	}{
		{"main", 0},              // No slashes = depth 0
		{"internal", 0},          // No slashes = depth 0
		{"internal/pkg/auth", 2}, // 2 slashes = depth 2
	}

	for _, tt := range tests {
		var depth int
		err := db.QueryRow(`
			SELECT depth FROM modules WHERE module_path = ?
		`, tt.modulePath).Scan(&depth)
		require.NoError(t, err, "module %s should exist", tt.modulePath)
		assert.Equal(t, tt.expectedDepth, depth, "module %s should have depth %d", tt.modulePath, tt.expectedDepth)
	}
}

func TestFileWriter_UpdateModuleStats_Idempotent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

	stats := &FileStats{
		FilePath:       "test.go",
		Language:       "go",
		ModulePath:     "main",
		LineCountTotal: 100,
		LineCountCode:  80,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Test: Multiple calls to UpdateModuleStats produce same result
	err = writer.UpdateModuleStats()
	require.NoError(t, err)

	var firstCount, firstLineCount int
	err = db.QueryRow(`
		SELECT file_count, line_count_total FROM modules WHERE module_path = 'main'
	`).Scan(&firstCount, &firstLineCount)
	require.NoError(t, err)

	// Call again
	err = writer.UpdateModuleStats()
	require.NoError(t, err)

	var secondCount, secondLineCount int
	err = db.QueryRow(`
		SELECT file_count, line_count_total FROM modules WHERE module_path = 'main'
	`).Scan(&secondCount, &secondLineCount)
	require.NoError(t, err)

	assert.Equal(t, firstCount, secondCount)
	assert.Equal(t, firstLineCount, secondLineCount)
}

func TestFileWriter_Close(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)

	// Test: Close releases resources
	err = writer.Close()
	require.NoError(t, err)

	// Second close should be safe (no-op or error is acceptable)
	err = writer.Close()
	// No assertion - implementation can choose to error or be idempotent
}
