package storage

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for FileWriter:
// - Can create new FileWriter instance with valid DB path
// - Can write single file statistics (deprecated WriteFileStats)
// - Can write file statistics in batch
// - Can write file content to FTS5 table (deprecated WriteFileContent)
// - Can write file content in batch
// - Can delete file (cascades to FTS5)
// - INSERT OR REPLACE updates existing files
// - Prepared statements used for performance
// - Transactions used for batch operations
// - Close releases resources properly
// - Error handling for invalid inputs
//
// NEW: WriteFile unified API tests:
// - Text file creates files entry with content + FTS entry
// - Binary file (nil) creates files entry, NO FTS entry
// - Empty file (&"") creates files + FTS (not same as NULL)
// - Updating content triggers FTS update via trigger
// - File changing binary→text creates FTS entry via trigger
// - File changing text→binary removes FTS entry via trigger

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

	// Test: Deprecated WriteFileContent method returns error
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	content := &FileContent{
		FilePath: "internal/indexer/parser.go",
		Content:  "package indexer\n\ntype Provider interface {\n\tEmbed(ctx context.Context) error\n}",
	}

	err = writer.WriteFileContent(content)
	assert.Error(t, err, "WriteFileContent should return deprecation error")
	assert.Contains(t, err.Error(), "deprecated")
}

func TestFileWriter_WriteFileContent_Replace(t *testing.T) {
	t.Parallel()

	// Test: Deprecated WriteFileContent method returns error
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

	err = writer.WriteFileContent(content)
	assert.Error(t, err, "WriteFileContent should return deprecation error")
	assert.Contains(t, err.Error(), "deprecated")
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

	// Write file with content using new unified API
	content := "package main"
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
	err = writer.WriteFile(stats, &content)
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

// --- NEW: WriteFile Unified API Tests ---

func TestFileWriter_WriteFile_TextFile(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: Text file creates files entry with content + FTS entry
	now := time.Now().UTC()
	content := "package main\n\nfunc Handler() error {\n\treturn nil\n}"
	file := &FileStats{
		FilePath:       "internal/handler.go",
		Language:       "go",
		ModulePath:     "internal",
		LineCountTotal: 5,
		LineCountCode:  4,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}

	err = writer.WriteFile(file, &content)
	require.NoError(t, err)

	// Verify files entry created with content
	var storedContent sql.NullString
	err = db.QueryRow(`
		SELECT content FROM files WHERE file_path = ?
	`, file.FilePath).Scan(&storedContent)
	require.NoError(t, err)
	assert.True(t, storedContent.Valid)
	assert.Equal(t, content, storedContent.String)

	// Verify FTS entry created (triggers synced it)
	var ftsCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, file.FilePath).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 1, ftsCount)

	// Verify FTS content searchable
	var foundPath string
	err = db.QueryRow(`
		SELECT file_path FROM files_fts WHERE content MATCH 'Handler'
	`).Scan(&foundPath)
	require.NoError(t, err)
	assert.Equal(t, file.FilePath, foundPath)
}

func TestFileWriter_WriteFile_BinaryFile(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: Binary file (nil) creates files entry, NO FTS entry
	now := time.Now().UTC()
	file := &FileStats{
		FilePath:       "assets/logo.png",
		Language:       "binary",
		ModulePath:     "assets",
		LineCountTotal: 0,
		LineCountCode:  0,
		SizeBytes:      4096,
		FileHash:       "hash_binary",
		LastModified:   now,
		IndexedAt:      now,
	}

	err = writer.WriteFile(file, nil) // nil = binary file
	require.NoError(t, err)

	// Verify files entry created
	var exists bool
	err = db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM files WHERE file_path = ?)
	`, file.FilePath).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists)

	// Verify content is NULL
	var storedContent sql.NullString
	err = db.QueryRow(`
		SELECT content FROM files WHERE file_path = ?
	`, file.FilePath).Scan(&storedContent)
	require.NoError(t, err)
	assert.False(t, storedContent.Valid, "Binary file should have NULL content")

	// Verify NO FTS entry created
	var ftsCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, file.FilePath).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 0, ftsCount, "Binary file should not have FTS entry")
}

func TestFileWriter_WriteFile_EmptyFile(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: Empty string (&"") creates files + FTS (not same as NULL)
	now := time.Now().UTC()
	emptyContent := ""
	file := &FileStats{
		FilePath:       "empty.txt",
		Language:       "text",
		ModulePath:     ".",
		LineCountTotal: 0,
		LineCountCode:  0,
		FileHash:       "hash_empty",
		LastModified:   now,
		IndexedAt:      now,
	}

	err = writer.WriteFile(file, &emptyContent)
	require.NoError(t, err)

	// Verify files entry created with empty string (NOT NULL)
	var storedContent sql.NullString
	err = db.QueryRow(`
		SELECT content FROM files WHERE file_path = ?
	`, file.FilePath).Scan(&storedContent)
	require.NoError(t, err)
	assert.True(t, storedContent.Valid, "Empty text file should have Valid=true")
	assert.Equal(t, "", storedContent.String)

	// Verify FTS entry created (empty content is still indexable)
	var ftsCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, file.FilePath).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 1, ftsCount, "Empty text file should have FTS entry")
}

func TestFileWriter_WriteFile_UpdateContent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: Updating content triggers FTS update via trigger
	now := time.Now().UTC()
	content1 := "package main\n\nfunc Handler() {}"
	file := &FileStats{
		FilePath:       "main.go",
		Language:       "go",
		ModulePath:     "main",
		LineCountTotal: 3,
		LineCountCode:  2,
		FileHash:       "hash1",
		LastModified:   now,
		IndexedAt:      now,
	}

	// Write initial version
	err = writer.WriteFile(file, &content1)
	require.NoError(t, err)

	// Verify initial content searchable
	var foundPath string
	err = db.QueryRow(`
		SELECT file_path FROM files_fts WHERE content MATCH 'Handler'
	`).Scan(&foundPath)
	require.NoError(t, err)
	assert.Equal(t, file.FilePath, foundPath)

	// Update with new content
	content2 := "package main\n\nfunc Provider() {}"
	file.FileHash = "hash2"
	file.IndexedAt = now.Add(time.Hour)

	err = writer.WriteFile(file, &content2)
	require.NoError(t, err)

	// Verify new content searchable
	err = db.QueryRow(`
		SELECT file_path FROM files_fts WHERE content MATCH 'Provider'
	`).Scan(&foundPath)
	require.NoError(t, err)
	assert.Equal(t, file.FilePath, foundPath)

	// Verify old content NOT searchable
	err = db.QueryRow(`
		SELECT file_path FROM files_fts WHERE content MATCH 'Handler'
	`).Scan(&foundPath)
	assert.Error(t, err, "Old content should not be searchable after update")
	assert.Equal(t, sql.ErrNoRows, err)

	// Verify only one FTS entry exists
	var ftsCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, file.FilePath).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 1, ftsCount, "Should have exactly one FTS entry after update")
}

func TestFileWriter_WriteFile_BinaryToText(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: File changing binary→text creates FTS entry via trigger
	now := time.Now().UTC()
	file := &FileStats{
		FilePath:       "changed.txt",
		Language:       "binary",
		ModulePath:     ".",
		LineCountTotal: 0,
		LineCountCode:  0,
		FileHash:       "hash_binary",
		LastModified:   now,
		IndexedAt:      now,
	}

	// Write as binary (nil content)
	err = writer.WriteFile(file, nil)
	require.NoError(t, err)

	// Verify NO FTS entry
	var ftsCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, file.FilePath).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 0, ftsCount)

	// Update to text file
	content := "Now I'm a text file!"
	file.Language = "text"
	file.LineCountTotal = 1
	file.LineCountCode = 1
	file.FileHash = "hash_text"
	file.IndexedAt = now.Add(time.Hour)

	err = writer.WriteFile(file, &content)
	require.NoError(t, err)

	// Verify FTS entry now exists (trigger created it)
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, file.FilePath).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 1, ftsCount, "Binary→text transition should create FTS entry")

	// Verify content searchable
	var foundPath string
	err = db.QueryRow(`
		SELECT file_path FROM files_fts WHERE content MATCH 'text'
	`).Scan(&foundPath)
	require.NoError(t, err)
	assert.Equal(t, file.FilePath, foundPath)
}

func TestFileWriter_WriteFile_TextToBinary(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Test: File changing text→binary removes FTS entry via trigger
	now := time.Now().UTC()
	content := "I'm a text file"
	file := &FileStats{
		FilePath:       "changed.txt",
		Language:       "text",
		ModulePath:     ".",
		LineCountTotal: 1,
		LineCountCode:  1,
		FileHash:       "hash_text",
		LastModified:   now,
		IndexedAt:      now,
	}

	// Write as text
	err = writer.WriteFile(file, &content)
	require.NoError(t, err)

	// Verify FTS entry exists
	var ftsCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, file.FilePath).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 1, ftsCount)

	// Update to binary (nil content)
	file.Language = "binary"
	file.LineCountTotal = 0
	file.LineCountCode = 0
	file.FileHash = "hash_binary"
	file.IndexedAt = now.Add(time.Hour)

	err = writer.WriteFile(file, nil)
	require.NoError(t, err)

	// Verify FTS entry removed (trigger deleted it)
	err = db.QueryRow(`
		SELECT COUNT(*) FROM files_fts WHERE file_path = ?
	`, file.FilePath).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 0, ftsCount, "Text→binary transition should remove FTS entry")

	// Verify content is NULL in files table
	var storedContent sql.NullString
	err = db.QueryRow(`
		SELECT content FROM files WHERE file_path = ?
	`, file.FilePath).Scan(&storedContent)
	require.NoError(t, err)
	assert.False(t, storedContent.Valid, "Binary file should have NULL content")
}
