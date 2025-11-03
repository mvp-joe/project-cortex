package indexer

import (
	"database/sql"
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

// Test Plan for BranchOptimizer:
// - NewBranchOptimizer returns nil for main/master branches
// - NewBranchOptimizer returns nil when no ancestor found
// - NewBranchOptimizer returns optimizer when ancestor exists
// - CopyUnchangedChunks handles missing ancestor database gracefully
// - CopyUnchangedChunks identifies unchanged vs changed files correctly
// - CopyUnchangedChunks copies chunks with updated timestamps
// - CopyUnchangedChunks handles empty ancestor database
// - CopyUnchangedChunks handles all files changed (no copies)
// - CopyUnchangedChunks handles all files unchanged (copy all)
// - CopyUnchangedChunks uses transactions (atomic copying)
// - loadFileHashes handles missing files table gracefully
// - Integration: full workflow with git branches and SQLite databases

// createTestGitRepo creates a test git repository with an initial commit on main
func createTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git init failed")

	// Configure git identity
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git config user.email failed")

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git config user.name failed")

	// Create initial commit
	testFile := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test content"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git add failed")

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git commit failed")

	// Rename to main (modern git default)
	cmd = exec.Command("git", "branch", "-M", "main")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git branch -M main failed")

	return dir
}

// createBranch creates a new branch from the current HEAD
func createBranch(t *testing.T, repoPath, branchName string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run(), "git checkout -b failed")
}

// checkoutBranch switches to an existing branch
func checkoutBranch(t *testing.T, repoPath, branchName string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", branchName)
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run(), "git checkout failed")
}

// createTestDatabase creates a test SQLite database with sample chunks
func createTestDatabase(t *testing.T, dbPath string, chunks []*storage.Chunk) {
	t.Helper()

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	require.NoError(t, os.MkdirAll(dir, 0755))

	// Create database
	writer, err := storage.NewChunkWriter(dbPath)
	require.NoError(t, err)
	defer writer.Close()

	// Insert file metadata BEFORE chunks (FK constraint)
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Insert file metadata for each unique file path
	filesSeen := make(map[string]bool)
	for _, chunk := range chunks {
		if !filesSeen[chunk.FilePath] {
			_, err := db.Exec(`
				INSERT OR REPLACE INTO files (file_path, language, module_path, file_hash, last_modified, indexed_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, chunk.FilePath, "go", "test", "hash-"+chunk.FilePath, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339))
			require.NoError(t, err)
			filesSeen[chunk.FilePath] = true
		}
	}

	// Write chunks
	if len(chunks) > 0 {
		require.NoError(t, writer.WriteChunks(chunks))
	}
}

func TestNewBranchOptimizer(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for main branch", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		assert.Nil(t, optimizer, "should return nil for main branch")
	})

	t.Run("returns nil for master branch", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		// Rename to master
		cmd := exec.Command("git", "branch", "-M", "master")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		assert.Nil(t, optimizer, "should return nil for master branch")
	})

	t.Run("returns nil when no ancestor found", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		// Rename main to develop (no main/master)
		cmd := exec.Command("git", "branch", "-M", "develop")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())

		createBranch(t, repo, "feature")

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		assert.Nil(t, optimizer, "should return nil when no ancestor found")
	})

	t.Run("returns optimizer when ancestor exists", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature")

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		require.NotNil(t, optimizer, "should return optimizer when ancestor exists")
		assert.Equal(t, "feature", optimizer.currentBranch)
		assert.Equal(t, "main", optimizer.ancestorBranch)
	})
}

func TestCopyUnchangedChunks(t *testing.T) {
	t.Parallel()

	t.Run("handles nil optimizer gracefully", func(t *testing.T) {
		t.Parallel()
		var optimizer *BranchOptimizer = nil

		files := map[string]FileInfo{
			"file1.go": {Path: "file1.go", Hash: "hash1"},
			"file2.go": {Path: "file2.go", Hash: "hash2"},
		}

		copiedCount, skippedFiles, err := optimizer.CopyUnchangedChunks(files)
		require.NoError(t, err)
		assert.Equal(t, 0, copiedCount)
		assert.ElementsMatch(t, []string{"file1.go", "file2.go"}, skippedFiles)
	})

	t.Run("handles missing ancestor database", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature")

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		require.NotNil(t, optimizer)

		files := map[string]FileInfo{
			"file1.go": {Path: "file1.go", Hash: "hash1"},
		}

		copiedCount, skippedFiles, err := optimizer.CopyUnchangedChunks(files)
		require.NoError(t, err)
		assert.Equal(t, 0, copiedCount, "should copy 0 chunks when ancestor DB missing")
		assert.ElementsMatch(t, []string{"file1.go"}, skippedFiles)
	})

	t.Run("identifies unchanged vs changed files", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature")

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		require.NotNil(t, optimizer)

		// Create ancestor database with chunks
		ancestorChunks := []*storage.Chunk{
			{
				ID:        "chunk-file1",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Symbols: file1.go",
				Text:      "package main",
				Embedding: []float32{0.1, 0.2, 0.3},
				CreatedAt: time.Now().Add(-1 * time.Hour),
				UpdatedAt: time.Now().Add(-1 * time.Hour),
			},
			{
				ID:        "chunk-file2",
				FilePath:  "file2.go",
				ChunkType: "symbols",
				Title:     "Symbols: file2.go",
				Text:      "package test",
				Embedding: []float32{0.4, 0.5, 0.6},
				CreatedAt: time.Now().Add(-1 * time.Hour),
				UpdatedAt: time.Now().Add(-1 * time.Hour),
			},
		}

		ancestorDBPath := filepath.Join(optimizer.cachePath, "branches", "main.db")
		createTestDatabase(t, ancestorDBPath, ancestorChunks)

		// Create current database (empty)
		currentDBPath := filepath.Join(optimizer.cachePath, "branches", "feature.db")
		createTestDatabase(t, currentDBPath, []*storage.Chunk{})

		// Current files: file1 unchanged, file2 changed, file3 new
		files := map[string]FileInfo{
			"file1.go": {Path: "file1.go", Hash: "hash-file1.go"}, // Unchanged (hash matches)
			"file2.go": {Path: "file2.go", Hash: "hash-changed"},  // Changed
			"file3.go": {Path: "file3.go", Hash: "hash-file3.go"}, // New
		}

		copiedCount, skippedFiles, err := optimizer.CopyUnchangedChunks(files)
		require.NoError(t, err)
		assert.Equal(t, 1, copiedCount, "should copy 1 chunk (file1)")
		assert.ElementsMatch(t, []string{"file2.go", "file3.go"}, skippedFiles)
	})

	t.Run("copies chunks with updated timestamps", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature")

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		require.NotNil(t, optimizer)

		// Create ancestor database
		ancestorTime := time.Now().Add(-24 * time.Hour)
		ancestorChunks := []*storage.Chunk{
			{
				ID:        "chunk-file1",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Symbols: file1.go",
				Text:      "package main",
				Embedding: []float32{0.1, 0.2, 0.3},
				CreatedAt: ancestorTime,
				UpdatedAt: ancestorTime,
			},
		}

		ancestorDBPath := filepath.Join(optimizer.cachePath, "branches", "main.db")
		createTestDatabase(t, ancestorDBPath, ancestorChunks)

		// Create current database (empty)
		currentDBPath := filepath.Join(optimizer.cachePath, "branches", "feature.db")
		createTestDatabase(t, currentDBPath, []*storage.Chunk{})

		files := map[string]FileInfo{
			"file1.go": {Path: "file1.go", Hash: "hash-file1.go"},
		}

		beforeCopy := time.Now().Add(-1 * time.Second) // 1 second buffer for timing
		copiedCount, _, err := optimizer.CopyUnchangedChunks(files)
		require.NoError(t, err)
		assert.Equal(t, 1, copiedCount)

		// Verify chunk was copied with updated timestamp
		db, err := sql.Open("sqlite3", currentDBPath)
		require.NoError(t, err)
		defer db.Close()

		var updatedAt string
		err = db.QueryRow("SELECT updated_at FROM chunks WHERE chunk_id = ?", "chunk-file1").Scan(&updatedAt)
		require.NoError(t, err)

		updatedTime, err := time.Parse(time.RFC3339, updatedAt)
		require.NoError(t, err)

		// Verify updated_at is recent (after ancestor time and after beforeCopy)
		assert.True(t, updatedTime.After(beforeCopy),
			"updated_at should be recent, got %v, beforeCopy was %v", updatedTime, beforeCopy)
		assert.True(t, updatedTime.After(ancestorTime),
			"updated_at should be after ancestor time")
	})

	t.Run("handles all files changed", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature")

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		require.NotNil(t, optimizer)

		// Create ancestor database
		ancestorChunks := []*storage.Chunk{
			{
				ID:        "chunk-file1",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Symbols: file1.go",
				Text:      "package main",
				Embedding: []float32{0.1, 0.2, 0.3},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		ancestorDBPath := filepath.Join(optimizer.cachePath, "branches", "main.db")
		createTestDatabase(t, ancestorDBPath, ancestorChunks)

		// Create current database
		currentDBPath := filepath.Join(optimizer.cachePath, "branches", "feature.db")
		createTestDatabase(t, currentDBPath, []*storage.Chunk{})

		// All files changed
		files := map[string]FileInfo{
			"file1.go": {Path: "file1.go", Hash: "hash-changed"},
		}

		copiedCount, skippedFiles, err := optimizer.CopyUnchangedChunks(files)
		require.NoError(t, err)
		assert.Equal(t, 0, copiedCount, "should copy 0 chunks when all files changed")
		assert.ElementsMatch(t, []string{"file1.go"}, skippedFiles)
	})

	t.Run("handles all files unchanged", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature")

		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		require.NotNil(t, optimizer)

		// Create ancestor database with multiple chunks
		ancestorChunks := []*storage.Chunk{
			{
				ID:        "chunk-file1-symbols",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Symbols: file1.go",
				Text:      "package main",
				Embedding: []float32{0.1, 0.2, 0.3},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			{
				ID:        "chunk-file1-definitions",
				FilePath:  "file1.go",
				ChunkType: "definitions",
				Title:     "Definitions: file1.go",
				Text:      "func main() { }",
				Embedding: []float32{0.4, 0.5, 0.6},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			{
				ID:        "chunk-file2-symbols",
				FilePath:  "file2.go",
				ChunkType: "symbols",
				Title:     "Symbols: file2.go",
				Text:      "package test",
				Embedding: []float32{0.7, 0.8, 0.9},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		ancestorDBPath := filepath.Join(optimizer.cachePath, "branches", "main.db")
		createTestDatabase(t, ancestorDBPath, ancestorChunks)

		// Create current database
		currentDBPath := filepath.Join(optimizer.cachePath, "branches", "feature.db")
		createTestDatabase(t, currentDBPath, []*storage.Chunk{})

		// All files unchanged
		files := map[string]FileInfo{
			"file1.go": {Path: "file1.go", Hash: "hash-file1.go"},
			"file2.go": {Path: "file2.go", Hash: "hash-file2.go"},
		}

		copiedCount, skippedFiles, err := optimizer.CopyUnchangedChunks(files)
		require.NoError(t, err)
		assert.Equal(t, 3, copiedCount, "should copy all 3 chunks")
		assert.Empty(t, skippedFiles, "no files should need indexing")
	})
}

func TestLoadFileHashes(t *testing.T) {
	t.Parallel()

	t.Run("handles missing files table", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")

		// Create empty database (no schema)
		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		defer db.Close()

		hashes, err := loadFileHashes(db)
		require.NoError(t, err)
		assert.Empty(t, hashes, "should return empty map when files table missing")
	})

	t.Run("loads file hashes correctly", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")

		// Create database with files
		chunks := []*storage.Chunk{
			{
				ID:        "chunk-1",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Symbols",
				Text:      "test",
				Embedding: []float32{0.1, 0.2},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}
		createTestDatabase(t, dbPath, chunks)

		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		defer db.Close()

		hashes, err := loadFileHashes(db)
		require.NoError(t, err)
		assert.Equal(t, "hash-file1.go", hashes["file1.go"])
	})
}

func TestBranchOptimizerIntegration(t *testing.T) {
	t.Parallel()

	t.Run("full workflow: feature branch copies from main", func(t *testing.T) {
		t.Parallel()

		// Create git repo
		repo := createTestGitRepo(t)

		// Create some files on main
		file1 := filepath.Join(repo, "file1.go")
		file2 := filepath.Join(repo, "file2.go")
		require.NoError(t, os.WriteFile(file1, []byte("package main"), 0644))
		require.NoError(t, os.WriteFile(file2, []byte("package test"), 0644))

		// Commit files
		cmd := exec.Command("git", "add", ".")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "commit", "-m", "add files")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())

		// Create main branch database with chunks
		cachePath, err := cache.EnsureCacheLocation(repo)
		require.NoError(t, err)

		mainChunks := []*storage.Chunk{
			{
				ID:        "chunk-file1",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Symbols: file1.go",
				Text:      "package main",
				Embedding: []float32{0.1, 0.2, 0.3},
				StartLine: 1,
				EndLine:   1,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			{
				ID:        "chunk-file2",
				FilePath:  "file2.go",
				ChunkType: "symbols",
				Title:     "Symbols: file2.go",
				Text:      "package test",
				Embedding: []float32{0.4, 0.5, 0.6},
				StartLine: 1,
				EndLine:   1,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}
		mainDBPath := filepath.Join(cachePath, "branches", "main.db")
		createTestDatabase(t, mainDBPath, mainChunks)

		// Create feature branch
		createBranch(t, repo, "feature")

		// Modify file2 on feature branch
		require.NoError(t, os.WriteFile(file2, []byte("package test // modified"), 0644))

		// Create optimizer
		optimizer, err := NewBranchOptimizer(repo)
		require.NoError(t, err)
		require.NotNil(t, optimizer)
		assert.Equal(t, "feature", optimizer.currentBranch)
		assert.Equal(t, "main", optimizer.ancestorBranch)

		// Create feature branch database (empty)
		featureDBPath := filepath.Join(cachePath, "branches", "feature.db")
		createTestDatabase(t, featureDBPath, []*storage.Chunk{})

		// File info: file1 unchanged, file2 changed
		files := map[string]FileInfo{
			"file1.go": {Path: "file1.go", Hash: "hash-file1.go"}, // Unchanged
			"file2.go": {Path: "file2.go", Hash: "hash-changed"},  // Changed
		}

		// Copy unchanged chunks
		copiedCount, skippedFiles, err := optimizer.CopyUnchangedChunks(files)
		require.NoError(t, err)
		assert.Equal(t, 1, copiedCount, "should copy chunk for file1")
		assert.ElementsMatch(t, []string{"file2.go"}, skippedFiles, "file2 needs re-indexing")

		// Verify chunk was copied to feature database
		db, err := sql.Open("sqlite3", featureDBPath)
		require.NoError(t, err)
		defer db.Close()

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks WHERE file_path = ?", "file1.go").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "file1 chunk should be in feature database")

		err = db.QueryRow("SELECT COUNT(*) FROM chunks WHERE file_path = ?", "file2.go").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "file2 chunk should NOT be in feature database (changed)")
	})
}
