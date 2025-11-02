package storage

import (
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for FileReader:
// - Can create new FileReader instance with valid DB
// - Can get single file statistics by path
// - Can get all files
// - Can filter files by language
// - Can filter files by module path
// - Can get module statistics
// - Can get all modules
// - Can get top modules by LOC
// - Can get file content from FTS5
// - Can search file content with FTS5
// - Returns nil for non-existent file
// - Close releases resources

func TestFileReader_New(t *testing.T) {
	t.Parallel()

	// Test: Creating FileReader with valid DB succeeds
	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	reader := NewFileReader(db)
	require.NotNil(t, reader)

	err = reader.Close()
	require.NoError(t, err)
}

func TestFileReader_GetFileStats(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	// Write test data
	writer := NewFileWriter(db)
	defer writer.Close()

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
		FileHash:         "abc123",
		LastModified:     now,
		IndexedAt:        now,
	}
	err = writer.WriteFileStats(stats)
	require.NoError(t, err)

	// Test: Read file statistics
	reader := NewFileReader(db)
	defer reader.Close()

	result, err := reader.GetFileStats("internal/indexer/parser.go")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, stats.FilePath, result.FilePath)
	assert.Equal(t, stats.Language, result.Language)
	assert.Equal(t, stats.ModulePath, result.ModulePath)
	assert.Equal(t, stats.LineCountTotal, result.LineCountTotal)
	assert.Equal(t, stats.LineCountCode, result.LineCountCode)
	assert.Equal(t, stats.SizeBytes, result.SizeBytes)
}

func TestFileReader_GetFileStats_NotFound(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	reader := NewFileReader(db)
	defer reader.Close()

	// Test: Non-existent file returns nil, nil
	result, err := reader.GetFileStats("nonexistent.go")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestFileReader_GetAllFiles(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

	// Write multiple files
	files := []*FileStats{
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
			LineCountTotal: 150,
			LineCountCode:  120,
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

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	// Test: Get all files
	reader := NewFileReader(db)
	defer reader.Close()

	results, err := reader.GetAllFiles()
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestFileReader_GetFilesByLanguage(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

	files := []*FileStats{
		{
			FilePath:       "file1.go",
			Language:       "go",
			ModulePath:     "pkg",
			LineCountTotal: 100,
			LineCountCode:  80,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "file2.go",
			Language:       "go",
			ModulePath:     "pkg",
			LineCountTotal: 150,
			LineCountCode:  120,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "file3.ts",
			Language:       "typescript",
			ModulePath:     "src",
			LineCountTotal: 200,
			LineCountCode:  150,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
	}

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	// Test: Filter by language
	reader := NewFileReader(db)
	defer reader.Close()

	goFiles, err := reader.GetFilesByLanguage("go")
	require.NoError(t, err)
	assert.Len(t, goFiles, 2)
	assert.Equal(t, "go", goFiles[0].Language)
	assert.Equal(t, "go", goFiles[1].Language)

	tsFiles, err := reader.GetFilesByLanguage("typescript")
	require.NoError(t, err)
	assert.Len(t, tsFiles, 1)
	assert.Equal(t, "typescript", tsFiles[0].Language)
}

func TestFileReader_GetFilesByModule(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

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
			FilePath:       "internal/indexer/chunker.go",
			Language:       "go",
			ModulePath:     "internal/indexer",
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
			LineCountTotal: 300,
			LineCountCode:  220,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
	}

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	// Test: Filter by module
	reader := NewFileReader(db)
	defer reader.Close()

	indexerFiles, err := reader.GetFilesByModule("internal/indexer")
	require.NoError(t, err)
	assert.Len(t, indexerFiles, 2)
	assert.Equal(t, "internal/indexer", indexerFiles[0].ModulePath)
	assert.Equal(t, "internal/indexer", indexerFiles[1].ModulePath)

	mcpFiles, err := reader.GetFilesByModule("internal/mcp")
	require.NoError(t, err)
	assert.Len(t, mcpFiles, 1)
	assert.Equal(t, "internal/mcp", mcpFiles[0].ModulePath)
}

func TestFileReader_GetModuleStats(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

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

	err = writer.UpdateModuleStats()
	require.NoError(t, err)

	// Test: Get module statistics
	reader := NewFileReader(db)
	defer reader.Close()

	stats, err := reader.GetModuleStats("internal/indexer")
	require.NoError(t, err)
	require.NotNil(t, stats)

	assert.Equal(t, "internal/indexer", stats.ModulePath)
	assert.Equal(t, 2, stats.FileCount)
	assert.Equal(t, 1, stats.TestFileCount)
	assert.Equal(t, 400, stats.LineCountTotal)
	assert.Equal(t, 300, stats.LineCountCode)
	assert.Equal(t, 1, stats.Depth)
}

func TestFileReader_GetModuleStats_NotFound(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	reader := NewFileReader(db)
	defer reader.Close()

	// Test: Non-existent module returns nil, nil
	stats, err := reader.GetModuleStats("nonexistent/module")
	require.NoError(t, err)
	assert.Nil(t, stats)
}

func TestFileReader_GetAllModules(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

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

	err = writer.UpdateModuleStats()
	require.NoError(t, err)

	// Test: Get all modules
	reader := NewFileReader(db)
	defer reader.Close()

	modules, err := reader.GetAllModules()
	require.NoError(t, err)
	assert.Len(t, modules, 3)
}

func TestFileReader_GetTopModules(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	now := time.Now().UTC()

	files := []*FileStats{
		{
			FilePath:       "module_a/file.go",
			Language:       "go",
			ModulePath:     "module_a",
			LineCountTotal: 100,
			LineCountCode:  80,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "module_b/file.go",
			Language:       "go",
			ModulePath:     "module_b",
			LineCountTotal: 500,
			LineCountCode:  400,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "module_c/file.go",
			Language:       "go",
			ModulePath:     "module_c",
			LineCountTotal: 200,
			LineCountCode:  150,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
	}

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	err = writer.UpdateModuleStats()
	require.NoError(t, err)

	// Test: Get top modules by LOC
	reader := NewFileReader(db)
	defer reader.Close()

	top2, err := reader.GetTopModules(2)
	require.NoError(t, err)
	assert.Len(t, top2, 2)

	// Should be ordered by line_count_code DESC
	assert.Equal(t, "module_b", top2[0].ModulePath)
	assert.Equal(t, 400, top2[0].LineCountCode)
	assert.Equal(t, "module_c", top2[1].ModulePath)
	assert.Equal(t, 150, top2[1].LineCountCode)
}

func TestFileReader_GetFileContent(t *testing.T) {
	t.Parallel()

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
	require.NoError(t, err)

	// Test: Read file content from FTS5
	reader := NewFileReader(db)
	defer reader.Close()

	result, err := reader.GetFileContent("internal/indexer/parser.go")
	require.NoError(t, err)
	assert.Equal(t, content.Content, result)
}

func TestFileReader_GetFileContent_NotFound(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	reader := NewFileReader(db)
	defer reader.Close()

	// Test: Non-existent file returns empty string
	result, err := reader.GetFileContent("nonexistent.go")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFileReader_SearchFileContent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	// Write files with different content
	contents := []*FileContent{
		{
			FilePath: "file1.go",
			Content:  "package main\n\ntype Provider interface {\n\tEmbed() error\n}",
		},
		{
			FilePath: "file2.go",
			Content:  "package main\n\ntype Handler struct {}",
		},
		{
			FilePath: "file3.go",
			Content:  "package main\n\nfunc Provider() error { return nil }",
		},
	}

	err = writer.WriteFileContentBatch(contents)
	require.NoError(t, err)

	// Also write file stats so we can join
	now := time.Now().UTC()
	files := []*FileStats{
		{
			FilePath:       "file1.go",
			Language:       "go",
			ModulePath:     "main",
			LineCountTotal: 5,
			LineCountCode:  4,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "file2.go",
			Language:       "go",
			ModulePath:     "main",
			LineCountTotal: 3,
			LineCountCode:  2,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "file3.go",
			Language:       "go",
			ModulePath:     "main",
			LineCountTotal: 3,
			LineCountCode:  2,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
	}

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	// Test: Search for "Provider"
	reader := NewFileReader(db)
	defer reader.Close()

	results, err := reader.SearchFileContent("Provider")
	require.NoError(t, err)
	assert.Len(t, results, 2) // file1.go and file3.go

	// Verify results contain Provider
	for _, f := range results {
		assert.Contains(t, []string{"file1.go", "file3.go"}, f.FilePath)
	}
}

func TestFileReader_SearchFileContent_BooleanQuery(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	writer := NewFileWriter(db)
	defer writer.Close()

	contents := []*FileContent{
		{
			FilePath: "file1.go",
			Content:  "package main\n\ntype Provider interface {}",
		},
		{
			FilePath: "file2.go",
			Content:  "package main\n\ntype Provider struct {}",
		},
		{
			FilePath: "file3.go",
			Content:  "package main\n\ntype Handler interface {}",
		},
	}

	err = writer.WriteFileContentBatch(contents)
	require.NoError(t, err)

	now := time.Now().UTC()
	files := []*FileStats{
		{
			FilePath:       "file1.go",
			Language:       "go",
			ModulePath:     "main",
			LineCountTotal: 3,
			LineCountCode:  2,
			FileHash:       "hash1",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "file2.go",
			Language:       "go",
			ModulePath:     "main",
			LineCountTotal: 3,
			LineCountCode:  2,
			FileHash:       "hash2",
			LastModified:   now,
			IndexedAt:      now,
		},
		{
			FilePath:       "file3.go",
			Language:       "go",
			ModulePath:     "main",
			LineCountTotal: 3,
			LineCountCode:  2,
			FileHash:       "hash3",
			LastModified:   now,
			IndexedAt:      now,
		},
	}

	err = writer.WriteFileStatsBatch(files)
	require.NoError(t, err)

	// Test: Boolean search "Provider AND interface"
	reader := NewFileReader(db)
	defer reader.Close()

	results, err := reader.SearchFileContent("Provider AND interface")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "file1.go", results[0].FilePath)
}

func TestFileReader_Close(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	err := CreateSchema(db)
	require.NoError(t, err)

	reader := NewFileReader(db)

	// Test: Close releases resources
	err = reader.Close()
	require.NoError(t, err)

	// Second close should be safe
	err = reader.Close()
	// No assertion - implementation can choose behavior
}
