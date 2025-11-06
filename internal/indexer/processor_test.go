package indexer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mvp-joe/project-cortex/internal/embed"
	storagepkg "github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessor_ProcessFiles_EmptyList(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	stor, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, stor)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{})

	// Verify
	require.NoError(t, err)
	assert.Equal(t, 0, stats.CodeFilesProcessed)
	assert.Equal(t, 0, stats.DocsProcessed)
	assert.Equal(t, 0, stats.TotalCodeChunks)
	assert.Equal(t, 0, stats.TotalDocChunks)
}

func TestProcessor_ProcessFiles_SingleGoFile(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a simple Go file
	goFile := filepath.Join(tempDir, "main.go")
	goContent := `package main

import "fmt"

// User represents a user
type User struct {
	ID   int
	Name string
}

// Greet prints a greeting
func Greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

const MaxUsers = 100
`
	err = os.WriteFile(goFile, []byte(goContent), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{goFile})

	// Verify
	require.NoError(t, err)
	assert.Equal(t, 1, stats.CodeFilesProcessed)
	assert.Equal(t, 0, stats.DocsProcessed)
	assert.Greater(t, stats.TotalCodeChunks, 0, "should have generated code chunks")
	assert.Equal(t, 0, stats.TotalDocChunks)
	assert.Greater(t, stats.ProcessingTime, time.Duration(0))

	// Verify chunks were written to storage
	chunkReader := storagepkg.NewChunkReaderWithDB(db)
	defer chunkReader.Close()
	chunks, err := chunkReader.ReadChunksByFile("main.go")
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0, "should have written chunks to storage")

	// Verify file stats were written
	fileReader := storagepkg.NewFileReader(db)
	fileStats, err := fileReader.GetFileStats("main.go")
	require.NoError(t, err)
	assert.Equal(t, "main.go", fileStats.FilePath)
	assert.Equal(t, "go", fileStats.Language)
}

func TestProcessor_ProcessFiles_SingleMarkdownFile(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a markdown file
	mdFile := filepath.Join(tempDir, "README.md")
	mdContent := `# Test Project

This is a test project.

## Features

- Feature 1
- Feature 2

## Installation

Run the following command:

` + "```bash\nnpm install\n```" + `
`
	err = os.WriteFile(mdFile, []byte(mdContent), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{mdFile})

	// Verify
	require.NoError(t, err)
	assert.Equal(t, 0, stats.CodeFilesProcessed)
	assert.Equal(t, 1, stats.DocsProcessed)
	assert.Equal(t, 0, stats.TotalCodeChunks)
	assert.Greater(t, stats.TotalDocChunks, 0, "should have generated doc chunks")

	// Verify chunks were written
	chunkReader := storagepkg.NewChunkReaderWithDB(db)
	defer chunkReader.Close()
	chunks, err := chunkReader.ReadChunksByFile("README.md")
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0, "should have written doc chunks")
}

func TestProcessor_ProcessFiles_MultipleFiles(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create multiple files
	goFile1 := filepath.Join(tempDir, "file1.go")
	goFile2 := filepath.Join(tempDir, "file2.go")
	mdFile := filepath.Join(tempDir, "README.md")

	err = os.WriteFile(goFile1, []byte("package main\n\nfunc Hello() {}\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(goFile2, []byte("package main\n\nfunc World() {}\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mdFile, []byte("# README\n\nTest content.\n"), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{goFile1, goFile2, mdFile})

	// Verify
	require.NoError(t, err)
	assert.Equal(t, 2, stats.CodeFilesProcessed)
	assert.Equal(t, 1, stats.DocsProcessed)
	assert.Greater(t, stats.TotalCodeChunks, 0)
	assert.Greater(t, stats.TotalDocChunks, 0)
}

func TestProcessor_ProcessFiles_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a Go file
	goFile := filepath.Join(tempDir, "main.go")
	err = os.WriteFile(goFile, []byte("package main\n\nfunc Test() {}\n"), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	stats, err := processor.ProcessFiles(ctx, []string{goFile})

	// Verify
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Nil(t, stats)
}

func TestProcessor_ProcessFiles_InvalidFilePath(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	stor, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, stor)

	// Execute with non-existent file
	ctx := context.Background()
	nonExistentFile := filepath.Join(tempDir, "nonexistent.go")
	stats, err := processor.ProcessFiles(ctx, []string{nonExistentFile})

	// Verify - should complete but warn about failed file
	// The processor should be resilient and continue with other files
	require.NoError(t, err)
	assert.NotNil(t, stats)
}

func TestProcessor_ProcessFiles_UnsupportedLanguage(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a file with unsupported extension
	unknownFile := filepath.Join(tempDir, "test.xyz")
	err = os.WriteFile(unknownFile, []byte("some content"), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{unknownFile})

	// Verify - should complete successfully (just skips unsupported files)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.CodeFilesProcessed) // Counted but produces no chunks
	assert.Equal(t, 0, stats.TotalCodeChunks)    // No chunks generated
}

func TestProcessor_ProcessFiles_StatsTracking(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create test files
	goFile := filepath.Join(tempDir, "main.go")
	mdFile := filepath.Join(tempDir, "README.md")

	goContent := `package main

type User struct {
	Name string
}

func main() {}

const Version = "1.0"
`
	err = os.WriteFile(goFile, []byte(goContent), 0644)
	require.NoError(t, err)

	mdContent := `# Title

Content here.
`
	err = os.WriteFile(mdFile, []byte(mdContent), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{goFile, mdFile})

	// Verify stats are tracked correctly
	require.NoError(t, err)
	assert.Equal(t, 1, stats.CodeFilesProcessed, "should count code files")
	assert.Equal(t, 1, stats.DocsProcessed, "should count doc files")
	assert.Greater(t, stats.TotalCodeChunks, 0, "should count code chunks")
	assert.Greater(t, stats.TotalDocChunks, 0, "should count doc chunks")
	assert.Greater(t, stats.ProcessingTime, time.Duration(0), "should track processing time")
}

func TestProcessor_ProcessFiles_ForeignKeyConstraint(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a Go file
	goFile := filepath.Join(tempDir, "test.go")
	err = os.WriteFile(goFile, []byte("package test\n\nfunc Test() {}\n"), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	_, err = processor.ProcessFiles(ctx, []string{goFile})
	require.NoError(t, err)

	// Verify that file stats were written BEFORE chunks
	// This is critical for foreign key constraints
	fileReader := storagepkg.NewFileReader(db)
	fileStats, err := fileReader.GetFileStats("test.go")
	require.NoError(t, err)
	assert.Equal(t, "test.go", fileStats.FilePath)

	// Verify chunks reference the file
	chunkReader := storagepkg.NewChunkReaderWithDB(db)
	defer chunkReader.Close()
	chunks, err := chunkReader.ReadChunksByFile("test.go")
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)
	for _, chunk := range chunks {
		assert.Equal(t, "test.go", chunk.FilePath, "chunk should reference file")
	}
}

// Helper functions

func setupProcessorTestStorage(t *testing.T, db *sql.DB, rootDir string) (Storage, error) {
	t.Helper()

	cacheRoot := t.TempDir()
	return NewSQLiteStorage(db, cacheRoot, rootDir)
}

func createTestProcessor(t *testing.T, rootDir string, stor Storage) Processor {
	t.Helper()

	// Create mock embedding provider
	mockProvider := &mockEmbedProvider{}

	// Create real components
	parser := NewParser()
	chunker := NewChunker(512, 50)
	formatter := NewFormatter()
	progress := &NoOpProgressReporter{}

	return NewProcessor(
		rootDir,
		parser,
		chunker,
		formatter,
		mockProvider,
		stor,
		progress,
	)
}

func TestProcessor_ProcessFiles_EmbeddingFailure(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a Go file
	goFile := filepath.Join(tempDir, "main.go")
	err = os.WriteFile(goFile, []byte("package main\n\nfunc Test() {}\n"), 0644)
	require.NoError(t, err)

	// Create processor with failing embed provider
	parser := NewParser()
	chunker := NewChunker(512, 50)
	formatter := NewFormatter()
	failingProvider := &mockFailingEmbedProvider{}
	progress := &NoOpProgressReporter{}

	processor := NewProcessor(
		tempDir,
		parser,
		chunker,
		formatter,
		failingProvider,
		storage,
		progress,
	)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{goFile})

	// Verify - should propagate embedding error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embedding error")
	assert.Nil(t, stats)
}

func TestProcessor_ProcessFiles_StorageFailure(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	// Create a Go file
	goFile := filepath.Join(tempDir, "main.go")
	err := os.WriteFile(goFile, []byte("package main\n\nfunc Test() {}\n"), 0644)
	require.NoError(t, err)

	// Create processor with failing storage (but valid DB for file stats phase)
	parser := NewParser()
	chunker := NewChunker(512, 50)
	formatter := NewFormatter()
	mockProvider := &mockEmbedProvider{}
	failingStorage := &mockFailingStorage{db: db}
	progress := &NoOpProgressReporter{}

	processor := NewProcessor(
		tempDir,
		parser,
		chunker,
		formatter,
		mockProvider,
		failingStorage,
		progress,
	)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{goFile})

	// Verify - should propagate storage error from WriteChunksIncremental
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "storage error")
	assert.Nil(t, stats)
}

func TestProcessor_ProcessFiles_ParsingFailure(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a Go file (parser should handle gracefully)
	goFile := filepath.Join(tempDir, "bad.go")
	err = os.WriteFile(goFile, []byte("this is not valid go code {{{"), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{goFile})

	// Verify - should complete gracefully (logs warning, continues)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.CodeFilesProcessed) // File is processed
	assert.Equal(t, 0, stats.TotalCodeChunks)    // But produces no chunks
}

func TestProcessor_ProcessFiles_SeparateFilesCorrectly(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create mix of code and doc files
	goFile := filepath.Join(tempDir, "code.go")
	mdFile := filepath.Join(tempDir, "doc.md")
	markdownFile := filepath.Join(tempDir, "guide.markdown")
	tsFile := filepath.Join(tempDir, "script.ts")

	err = os.WriteFile(goFile, []byte("package main\n\nfunc Test() {}\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mdFile, []byte("# Doc\n\nContent.\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(markdownFile, []byte("# Guide\n\nContent.\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(tsFile, []byte("function test() {}\n"), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{goFile, mdFile, markdownFile, tsFile})

	// Verify - correct separation of code vs docs
	require.NoError(t, err)
	assert.Equal(t, 2, stats.CodeFilesProcessed, "should process 2 code files (go, ts)")
	assert.Equal(t, 2, stats.DocsProcessed, "should process 2 doc files (md, markdown)")
	assert.Greater(t, stats.TotalCodeChunks, 0)
	assert.Greater(t, stats.TotalDocChunks, 0)
}

func TestProcessor_ProcessFiles_ChunkingFailure(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a markdown file
	mdFile := filepath.Join(tempDir, "doc.md")
	err = os.WriteFile(mdFile, []byte("# Doc\n\nContent.\n"), 0644)
	require.NoError(t, err)

	// Create processor with failing chunker
	parser := NewParser()
	failingChunker := &mockFailingChunker{}
	formatter := NewFormatter()
	mockProvider := &mockEmbedProvider{}
	progress := &NoOpProgressReporter{}

	processor := NewProcessor(
		tempDir,
		parser,
		failingChunker,
		formatter,
		mockProvider,
		storage,
		progress,
	)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{mdFile})

	// Verify - should complete gracefully (logs warning, continues)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.DocsProcessed)
	assert.Equal(t, 0, stats.TotalDocChunks) // No chunks due to failure
}

// mockEmbedProvider is a mock embedding provider for testing.
type mockEmbedProvider struct{}

func (m *mockEmbedProvider) Initialize(ctx context.Context) error {
	return nil
}

func (m *mockEmbedProvider) Embed(ctx context.Context, texts []string, mode embed.EmbedMode) ([][]float32, error) {
	// Return mock embeddings (384 dimensions, all zeros)
	embeddings := make([][]float32, len(texts))
	for i := range embeddings {
		embeddings[i] = make([]float32, 384)
		// Add some variation to avoid identical embeddings
		for j := range embeddings[i] {
			embeddings[i][j] = float32(i+j) * 0.001
		}
	}
	return embeddings, nil
}

func (m *mockEmbedProvider) Dimensions() int {
	return 384
}

func (m *mockEmbedProvider) Close() error {
	return nil
}

// mockFailingEmbedProvider simulates embedding failures.
type mockFailingEmbedProvider struct{}

func (m *mockFailingEmbedProvider) Initialize(ctx context.Context) error {
	return nil
}

func (m *mockFailingEmbedProvider) Embed(ctx context.Context, texts []string, mode embed.EmbedMode) ([][]float32, error) {
	return nil, fmt.Errorf("embedding error: simulated failure")
}

func (m *mockFailingEmbedProvider) Dimensions() int {
	return 384
}

func (m *mockFailingEmbedProvider) Close() error {
	return nil
}

// mockFailingStorage simulates storage failures.
type mockFailingStorage struct {
	db *sql.DB
}

func (m *mockFailingStorage) WriteChunks(chunks []Chunk) error {
	return fmt.Errorf("storage error: simulated failure")
}

func (m *mockFailingStorage) WriteChunksIncremental(chunks []Chunk) error {
	return fmt.Errorf("storage error: simulated failure")
}

func (m *mockFailingStorage) ReadMetadata() (*GeneratorMetadata, error) {
	return nil, fmt.Errorf("storage error: simulated failure")
}

func (m *mockFailingStorage) GetDB() *sql.DB {
	return m.db
}

func (m *mockFailingStorage) GetCachePath() string {
	return ""
}

func (m *mockFailingStorage) GetBranch() string {
	return "main"
}

func (m *mockFailingStorage) DeleteFile(filePath string) error {
	return fmt.Errorf("storage error: simulated failure")
}

func (m *mockFailingStorage) UpdateFileMtimes(filePaths []string) error {
	return fmt.Errorf("storage error: simulated failure")
}

func (m *mockFailingStorage) Close() error {
	return nil
}

// mockFailingChunker simulates chunking failures.
type mockFailingChunker struct{}

func (m *mockFailingChunker) ChunkDocument(ctx context.Context, filePath, language string) ([]DocumentationChunk, error) {
	return nil, fmt.Errorf("chunking error: simulated failure")
}
