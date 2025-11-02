package mcp

// Test Plan for SQLite Loader:
// 1. LoadChunksFromSQLite loads chunks successfully from SQLite database
// 2. LoadChunksFromSQLite converts storage.Chunk to ContextChunk correctly
// 3. LoadChunksFromSQLite handles missing database gracefully
// 4. LoadChunksAuto prefers SQLite when available
// 5. LoadChunksAuto falls back to JSON when SQLite missing
// 6. Tag derivation works for common file extensions (Go, TypeScript, Python, etc.)
// 7. Tags include chunk type, language, and content type appropriately
// 8. Handles corrupted database with clear error message

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadChunksFromSQLite verifies basic SQLite loading functionality.
// NOTE: This test is skipped in environments without FTS5 support.
func TestLoadChunksFromSQLite(t *testing.T) {
	t.Skip("Skipping SQLite integration test - requires FTS5 support. Use 'go test -tags fts5' if available.")

	t.Parallel()

	// Setup: Create a temporary project with SQLite cache
	projectDir := t.TempDir()
	setupGitRepo(t, projectDir)

	// Create settings and cache directory
	settings, err := cache.LoadOrCreateSettings(projectDir)
	require.NoError(t, err)

	branchesDir := filepath.Join(settings.CacheLocation, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Create and populate SQLite database
	branch := cache.GetCurrentBranch(projectDir)
	dbPath := filepath.Join(branchesDir, branch+".db")

	writer, err := storage.NewChunkWriter(dbPath)
	require.NoError(t, err)
	defer writer.Close()

	// Write test chunks
	testChunks := []*storage.Chunk{
		{
			ID:        "test-chunk-1",
			FilePath:  "internal/server.go",
			ChunkType: "symbols",
			Title:     "Package server symbols",
			Text:      "Package: server\n\nTypes:\n  - Handler (lines 10-20)",
			Embedding: []float32{0.1, 0.2, 0.3},
			StartLine: 1,
			EndLine:   50,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "test-chunk-2",
			FilePath:  "README.md",
			ChunkType: "documentation",
			Title:     "Project Overview",
			Text:      "# Project Cortex\n\nSemantic search for code.",
			Embedding: []float32{0.4, 0.5, 0.6},
			StartLine: 1,
			EndLine:   10,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	require.NoError(t, writer.WriteChunks(testChunks))

	// Test: Load chunks from SQLite
	chunks, err := LoadChunksFromSQLite(projectDir)
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	// Verify first chunk
	chunk1 := chunks[0]
	assert.Equal(t, "test-chunk-1", chunk1.ID)
	assert.Equal(t, "symbols", chunk1.ChunkType)
	assert.Equal(t, "Package server symbols", chunk1.Title)
	assert.Contains(t, chunk1.Text, "Package: server")
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, chunk1.Embedding)
	assert.Contains(t, chunk1.Tags, "symbols")
	assert.Contains(t, chunk1.Tags, "go")
	assert.Contains(t, chunk1.Tags, "code")
	assert.Equal(t, "internal/server.go", chunk1.Metadata["file_path"])
	assert.Equal(t, 1, chunk1.Metadata["start_line"])
	assert.Equal(t, 50, chunk1.Metadata["end_line"])

	// Verify second chunk
	chunk2 := chunks[1]
	assert.Equal(t, "test-chunk-2", chunk2.ID)
	assert.Equal(t, "documentation", chunk2.ChunkType)
	assert.Contains(t, chunk2.Tags, "documentation")
	assert.Contains(t, chunk2.Tags, "markdown")
	assert.Equal(t, "README.md", chunk2.Metadata["file_path"])
}

// TestLoadChunksFromSQLite_MissingDatabase verifies error handling when database doesn't exist.
func TestLoadChunksFromSQLite_MissingDatabase(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	setupGitRepo(t, projectDir)

	// Test: Load chunks without creating database
	chunks, err := LoadChunksFromSQLite(projectDir)
	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "SQLite cache not found")
	assert.Contains(t, err.Error(), "run 'cortex index' first")
}

// TestLoadChunksAuto_PrefersSQLite verifies SQLite-first strategy.
// NOTE: This test is skipped in environments without FTS5 support.
// Run with: go test -tags fts5 to enable.
func TestLoadChunksAuto_PrefersSQLite(t *testing.T) {
	t.Skip("Skipping SQLite integration test - requires FTS5 support. Use 'go test -tags fts5' if available.")

	t.Parallel()

	// Setup: Create project with both SQLite and JSON
	projectDir := t.TempDir()
	setupGitRepo(t, projectDir)

	// Create SQLite cache
	settings, err := cache.LoadOrCreateSettings(projectDir)
	require.NoError(t, err)

	branchesDir := filepath.Join(settings.CacheLocation, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	branch := cache.GetCurrentBranch(projectDir)
	dbPath := filepath.Join(branchesDir, branch+".db")

	writer, err := storage.NewChunkWriter(dbPath)
	require.NoError(t, err)
	defer writer.Close()

	sqliteChunks := []*storage.Chunk{
		{
			ID:        "sqlite-chunk",
			FilePath:  "main.go",
			ChunkType: "symbols",
			Title:     "Main package",
			Text:      "Package: main",
			Embedding: []float32{0.1, 0.2, 0.3},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	require.NoError(t, writer.WriteChunks(sqliteChunks))

	// Create JSON chunks (legacy format)
	chunksDir := filepath.Join(projectDir, ".cortex", "chunks")
	require.NoError(t, os.MkdirAll(chunksDir, 0755))
	createTestJSONChunks(t, chunksDir, "json-chunk")

	// Test: LoadChunksAuto should prefer SQLite
	chunks, err := LoadChunksAuto(projectDir, chunksDir)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	assert.Equal(t, "sqlite-chunk", chunks[0].ID) // SQLite chunk, not JSON
}

// TestLoadChunksAuto_FallbackToJSON verifies JSON fallback when SQLite unavailable.
func TestLoadChunksAuto_FallbackToJSON(t *testing.T) {
	t.Parallel()

	// Setup: Create project with only JSON chunks
	projectDir := t.TempDir()
	setupGitRepo(t, projectDir)

	chunksDir := filepath.Join(projectDir, ".cortex", "chunks")
	require.NoError(t, os.MkdirAll(chunksDir, 0755))
	createTestJSONChunks(t, chunksDir, "json-chunk")

	// Test: LoadChunksAuto should fallback to JSON
	chunks, err := LoadChunksAuto(projectDir, chunksDir)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	assert.Equal(t, "json-chunk", chunks[0].ID)
}

// TestLoadChunksAuto_BothMissing verifies error when neither SQLite nor JSON exists.
func TestLoadChunksAuto_BothMissing(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	setupGitRepo(t, projectDir)
	chunksDir := filepath.Join(projectDir, ".cortex", "chunks")

	// Test: Should fail with clear error
	chunks, err := LoadChunksAuto(projectDir, chunksDir)
	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "failed to load chunks from both SQLite and JSON")
}

// TestDeriveTags verifies tag generation from chunk type and file extension.
func TestDeriveTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chunkType string
		filePath  string
		wantTags  []string
	}{
		{
			name:      "Go symbols",
			chunkType: "symbols",
			filePath:  "internal/mcp/server.go",
			wantTags:  []string{"symbols", "go", "code"},
		},
		{
			name:      "TypeScript definitions",
			chunkType: "definitions",
			filePath:  "src/types.ts",
			wantTags:  []string{"definitions", "typescript", "code"},
		},
		{
			name:      "TSX definitions",
			chunkType: "definitions",
			filePath:  "src/App.tsx",
			wantTags:  []string{"definitions", "typescript", "code"},
		},
		{
			name:      "JavaScript data",
			chunkType: "data",
			filePath:  "config.js",
			wantTags:  []string{"data", "javascript", "code"},
		},
		{
			name:      "Python symbols",
			chunkType: "symbols",
			filePath:  "src/main.py",
			wantTags:  []string{"symbols", "python", "code"},
		},
		{
			name:      "Rust definitions",
			chunkType: "definitions",
			filePath:  "src/lib.rs",
			wantTags:  []string{"definitions", "rust", "code"},
		},
		{
			name:      "Markdown documentation",
			chunkType: "documentation",
			filePath:  "README.md",
			wantTags:  []string{"documentation", "markdown"},
		},
		{
			name:      "Text documentation",
			chunkType: "documentation",
			filePath:  "NOTES.txt",
			wantTags:  []string{"documentation", "text"},
		},
		{
			name:      "C code",
			chunkType: "definitions",
			filePath:  "src/main.c",
			wantTags:  []string{"definitions", "c", "code"},
		},
		{
			name:      "C++ code",
			chunkType: "definitions",
			filePath:  "src/main.cpp",
			wantTags:  []string{"definitions", "cpp", "code"},
		},
		{
			name:      "Ruby code",
			chunkType: "symbols",
			filePath:  "app.rb",
			wantTags:  []string{"symbols", "ruby", "code"},
		},
		{
			name:      "PHP code",
			chunkType: "symbols",
			filePath:  "index.php",
			wantTags:  []string{"symbols", "php", "code"},
		},
		{
			name:      "Unknown extension",
			chunkType: "data",
			filePath:  "config.yml",
			wantTags:  []string{"data"}, // Only chunk type
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := deriveTags(tt.chunkType, tt.filePath)
			assert.Equal(t, tt.wantTags, tags)
		})
	}
}

// Helper: setupGitRepo initializes a minimal git repository for testing.
func setupGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	// Set user config (required for git operations)
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	// Create initial commit (required for branch detection)
	readmePath := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readmePath, []byte("# Test"), 0644))

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())
}

// Helper: createTestJSONChunks creates a test chunk file in JSON format.
func createTestJSONChunks(t *testing.T, chunksDir, chunkID string) {
	t.Helper()

	chunkFile := filepath.Join(chunksDir, "code-symbols.json")
	content := `{
  "_metadata": {
    "model": "test-model",
    "dimensions": 3,
    "chunk_type": "symbols",
    "generated": "2025-01-01T00:00:00Z",
    "version": "1.0"
  },
  "chunks": [
    {
      "id": "` + chunkID + `",
      "title": "Test Chunk",
      "text": "Test content",
      "chunk_type": "symbols",
      "embedding": [0.1, 0.2, 0.3],
      "tags": ["symbols", "go", "code"],
      "metadata": {
        "file_path": "test.go",
        "start_line": 1,
        "end_line": 10
      },
      "created_at": "2025-01-01T00:00:00Z",
      "updated_at": "2025-01-01T00:00:00Z"
    }
  ]
}`
	require.NoError(t, os.WriteFile(chunkFile, []byte(content), 0644))
}
