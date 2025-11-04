package indexer

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for FTS5 File Content Indexing:
// 1. Test isTextFile() correctly identifies text vs binary files
//    - Test text files (.go, .md, .txt, .js, .py)
//    - Test binary files (with null bytes)
//    - Test empty files
//    - Test small files vs large files
//    - Test unreadable files (permission errors)
// 2. Test indexer writes file content to files_fts table
//    - Test content is written for text files
//    - Test content is NOT written for binary files
//    - Test content matches actual file content
// 3. Test error handling
//    - Test missing files
//    - Test permission errors

func TestIsTextFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    []byte
		wantIsText bool
		wantErr    bool
	}{
		{
			name:       "go source file",
			content:    []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"),
			wantIsText: true,
			wantErr:    false,
		},
		{
			name:       "markdown file",
			content:    []byte("# Heading\n\nThis is a markdown file with **bold** text.\n"),
			wantIsText: true,
			wantErr:    false,
		},
		{
			name:       "empty file",
			content:    []byte(""),
			wantIsText: true,
			wantErr:    false,
		},
		{
			name:       "file with unicode",
			content:    []byte("Hello 世界 🌍\n"),
			wantIsText: true,
			wantErr:    false,
		},
		{
			name:       "binary file with null bytes",
			content:    []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE},
			wantIsText: false,
			wantErr:    false,
		},
		{
			name:       "text with embedded null (still binary)",
			content:    []byte("hello\x00world"),
			wantIsText: false,
			wantErr:    false,
		},
		{
			name:       "large text file",
			content:    bytes.Repeat([]byte("line of text\n"), 10000),
			wantIsText: true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "testfile")
			err := os.WriteFile(tmpFile, tt.content, 0644)
			require.NoError(t, err)

			// Test
			isText, err := isTextFile(tmpFile)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantIsText, isText, "isTextFile() result mismatch")
			}
		})
	}
}

func TestIsTextFile_UnreadableFile(t *testing.T) {
	t.Parallel()

	// Test non-existent file
	isText, err := isTextFile("/nonexistent/file/path")
	assert.Error(t, err)
	assert.False(t, isText)
}

func TestIsTextFile_MaxReadSize(t *testing.T) {
	t.Parallel()

	// Test that we only read first 512 bytes for detection
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "largefile")

	// Create a large file (1MB of text)
	content := bytes.Repeat([]byte("text content\n"), 100000)
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	// Should still detect as text (reading only first 512 bytes)
	isText, err := isTextFile(tmpFile)
	assert.NoError(t, err)
	assert.True(t, isText)
}

func TestIsTextFile_BinaryAtStart(t *testing.T) {
	t.Parallel()

	// Test file that starts with binary but has text later
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "mixedfile")

	// Binary at start should mark as binary
	content := append([]byte{0x00, 0xFF}, []byte("text content after binary")...)
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	isText, err := isTextFile(tmpFile)
	assert.NoError(t, err)
	assert.False(t, isText)
}

// setupTestDB creates a test database and cache directory for indexer tests.
func setupTestDB(t *testing.T, tmpDir string) (*sql.DB, string) {
	// Initialize sqlite-vec extension
	storage.InitVectorExtension()

	// Create cache directory
	cacheDir := filepath.Join(tmpDir, ".cortex", "cache", "test")
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "branches"), 0755))

	// Create database
	dbPath := filepath.Join(cacheDir, "branches", "main.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	// Enable foreign keys
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Create schema
	err = storage.CreateSchema(db)
	require.NoError(t, err)

	return db, cacheDir
}

// Integration tests for indexer writing file content to FTS

func TestIndexer_WritesFTSContent(t *testing.T) {
	t.Parallel()

	// Test plan:
	// 1. Create test project with mix of text and binary files
	// 2. Run indexer
	// 3. Verify text files are in files_fts table
	// 4. Verify binary files are NOT in files_fts table
	// 5. Verify content matches actual file content

	// Setup test project
	tmpDir := t.TempDir()

	// Create text files
	goFile := filepath.Join(tmpDir, "main.go")
	goContent := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	err := os.WriteFile(goFile, []byte(goContent), 0644)
	require.NoError(t, err)

	mdFile := filepath.Join(tmpDir, "README.md")
	mdContent := "# Test Project\n\nThis is a test.\n"
	err = os.WriteFile(mdFile, []byte(mdContent), 0644)
	require.NoError(t, err)

	// Create binary file (with null bytes)
	binFile := filepath.Join(tmpDir, "binary.dat")
	binContent := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	err = os.WriteFile(binFile, binContent, 0644)
	require.NoError(t, err)

	// Setup database
	db, cacheDir := setupTestDB(t, tmpDir)
	defer db.Close()

	// Create indexer config
	config := &Config{
		RootDir:      tmpDir,
		OutputDir:    filepath.Join(tmpDir, ".cortex"),
		CodePatterns: []string{"**/*.go"},
		DocsPatterns: []string{"**/*.md"},
		DocChunkSize: 1000,
		Overlap:      200,
	}

	// Create mock embedding provider
	mockProvider := &mockEmbeddingProvider{dimensions: 384}

	// Create indexer with progress reporter
	progress := &NoOpProgressReporter{}
	indexer, err := NewWithProvider(config, db, cacheDir, mockProvider, progress)
	require.NoError(t, err)
	defer indexer.Close()

	// Run indexing
	ctx := context.Background()
	stats, err := indexer.Index(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify files_fts table has text files
	storageDB := indexer.GetStorage().GetDB()
	reader := storage.NewFileReader(storageDB)

	// Check go file content
	goContentResult, err := reader.GetFileContent("main.go")
	require.NoError(t, err)
	assert.Equal(t, goContent, goContentResult, "Go file content should match")

	// Check markdown file content
	mdContentResult, err := reader.GetFileContent("README.md")
	require.NoError(t, err)
	assert.Equal(t, mdContent, mdContentResult, "Markdown file content should match")

	// Binary file should NOT be in files_fts (returns empty string for missing entries)
	binContentResult, err := reader.GetFileContent("binary.dat")
	require.NoError(t, err) // No error, just empty result for missing files
	assert.Empty(t, binContentResult, "Binary file should not be in FTS index")
}

func TestIndexer_SkipsBinaryFiles(t *testing.T) {
	t.Parallel()

	// Test that binary files are skipped during FTS indexing
	tmpDir := t.TempDir()

	// Create a binary file (PNG-like with null bytes to ensure it's detected as binary)
	binFile := filepath.Join(tmpDir, "image.png")
	binContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x00, 0x00} // PNG header with nulls
	err := os.WriteFile(binFile, binContent, 0644)
	require.NoError(t, err)

	// Setup database
	db, cacheDir := setupTestDB(t, tmpDir)
	defer db.Close()

	// Create indexer config (match binary file pattern)
	config := &Config{
		RootDir:      tmpDir,
		OutputDir:    filepath.Join(tmpDir, ".cortex"),
		CodePatterns: []string{"**/*.png"}, // Explicitly include binary
		DocsPatterns: []string{},
		DocChunkSize: 1000,
		Overlap:      200,
	}

	mockProvider := &mockEmbeddingProvider{dimensions: 384}
	progress := &NoOpProgressReporter{}
	indexer, err := NewWithProvider(config, db, cacheDir, mockProvider, progress)
	require.NoError(t, err)
	defer indexer.Close()

	// Run indexing
	ctx := context.Background()
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	// Verify binary file is NOT in files_fts (returns empty string for missing entries)
	storageDB := indexer.GetStorage().GetDB()
	reader := storage.NewFileReader(storageDB)
	pngContent, err := reader.GetFileContent("image.png")
	require.NoError(t, err) // No error, just empty result for missing files
	assert.Empty(t, pngContent, "Binary file should not be in FTS index")
}

func TestIndexer_HandlesUnreadableFiles(t *testing.T) {
	t.Parallel()

	// Test that indexer handles unreadable files gracefully
	tmpDir := t.TempDir()

	// Create a text file
	textFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(textFile, []byte("package main"), 0644)
	require.NoError(t, err)

	// Setup database
	db, cacheDir := setupTestDB(t, tmpDir)
	defer db.Close()

	config := &Config{
		RootDir:      tmpDir,
		OutputDir:    filepath.Join(tmpDir, ".cortex"),
		CodePatterns: []string{"**/*.go"},
		DocsPatterns: []string{},
		DocChunkSize: 1000,
		Overlap:      200,
	}

	mockProvider := &mockEmbeddingProvider{dimensions: 384}
	progress := &NoOpProgressReporter{}
	indexer, err := NewWithProvider(config, db, cacheDir, mockProvider, progress)
	require.NoError(t, err)
	defer indexer.Close()

	// Make file unreadable (if on Unix-like system)
	if err := os.Chmod(textFile, 0000); err == nil {
		// Restore permissions at end
		defer os.Chmod(textFile, 0644)

		// Run indexing - should succeed but skip unreadable file
		ctx := context.Background()
		_, err := indexer.Index(ctx)
		// Indexing should still succeed even with unreadable files
		require.NoError(t, err)

		// File should not be in FTS (couldn't read it) - returns empty string
		db := indexer.GetStorage().GetDB()
		reader := storage.NewFileReader(db)
		unreadableContent, err := reader.GetFileContent("test.go")
		require.NoError(t, err) // No error, just empty result for missing files
		assert.Empty(t, unreadableContent, "Unreadable file should not be in FTS index")
	}
}

// mockEmbeddingProvider for testing (no actual embedding service needed)
type mockEmbeddingProvider struct {
	dimensions int
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, texts []string, mode embed.EmbedMode) ([][]float32, error) {
	// Return dummy embeddings
	embeddings := make([][]float32, len(texts))
	for i := range embeddings {
		embeddings[i] = make([]float32, m.dimensions)
		// Fill with dummy values
		for j := range embeddings[i] {
			embeddings[i][j] = 0.1
		}
	}
	return embeddings, nil
}

func (m *mockEmbeddingProvider) Dimensions() int {
	return m.dimensions
}

func (m *mockEmbeddingProvider) Close() error {
	return nil
}

func (m *mockEmbeddingProvider) Initialize(ctx context.Context) error {
	return nil
}
