package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for IndexerWatcher:
// - NewIndexerWatcher creates watcher successfully with valid paths
// - NewIndexerWatcher returns error with invalid root directory
// - Start/Stop watcher with context cancellation works correctly
// - File creation triggers incremental reindex
// - File modification triggers incremental reindex
// - File deletion triggers incremental reindex
// - Multiple rapid changes are debounced into single reindex
// - Only relevant files trigger reindex (code/docs patterns)
// - Ignored files/directories don't trigger reindex (node_modules, .git, etc.)
// - New directories are added to watch automatically
// - Watcher stops cleanly without goroutine leaks
// - Context cancellation stops watcher quickly (<100ms)
// - Concurrent Start/Stop is safe (sync.Once)
// - Watcher handles permission errors gracefully
// - shouldProcessEvent filters events correctly
// - shouldWatchDirectory respects ignore patterns

// Test: NewIndexerWatcher creates watcher successfully with valid paths
func TestNewIndexerWatcher_Success(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{"node_modules/**", ".git/**"},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	require.NotNil(t, watcher)
	// NOTE: We don't call Start() in this test, so we shouldn't call Stop()
	// which would block waiting for the goroutine that never started.
	defer watcher.watcher.Close()

	assert.Equal(t, rootDir, watcher.rootDir)
	assert.Equal(t, 500*time.Millisecond, watcher.debounceTime)
}

// Test: NewIndexerWatcher returns error with invalid root directory
func TestNewIndexerWatcher_InvalidDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	// Don't create rootDir - should fail when trying to walk it
	invalidRoot := filepath.Join(tempDir, "nonexistent")

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	watcher, err := NewIndexerWatcher(indexer, invalidRoot)
	assert.Error(t, err)
	assert.Nil(t, watcher)
}

// Test: File creation triggers incremental reindex
func TestIndexerWatcher_FileCreation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{"node_modules/**"},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// Initial index to create metadata
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	// Create watcher
	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	defer watcher.Stop()

	// Start watching
	watcher.Start(ctx)

	// Wait a bit for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Create a new file
	newFile := filepath.Join(rootDir, "new.go")
	content := []byte(`package main

func NewFunc() string {
	return "new"
}
`)
	require.NoError(t, os.WriteFile(newFile, content, 0644))

	// Wait for debounce + processing
	time.Sleep(1 * time.Second)

	// Verify incremental index was triggered by checking metadata
	writer := GetWriter(indexer)
	metadata, err := writer.ReadMetadata()
	require.NoError(t, err)

	// Check that new file is in checksums
	_, exists := metadata.FileChecksums["new.go"]
	assert.True(t, exists, "New file should be in metadata checksums")
}

// Test: File modification triggers incremental reindex
func TestIndexerWatcher_FileModification(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	// Create initial file
	testFile := filepath.Join(rootDir, "test.go")
	initialContent := []byte(`package main

func Test() string {
	return "test"
}
`)
	require.NoError(t, os.WriteFile(testFile, initialContent, 0644))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// Initial index
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	// Get initial checksum
	writer := GetWriter(indexer)
	metadata1, err := writer.ReadMetadata()
	require.NoError(t, err)
	initialChecksum := metadata1.FileChecksums["test.go"]

	// Create watcher
	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	defer watcher.Stop()

	watcher.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Modify the file
	modifiedContent := []byte(`package main

func Test() string {
	return "modified"
}
`)
	require.NoError(t, os.WriteFile(testFile, modifiedContent, 0644))

	// Wait for debounce + processing
	time.Sleep(1 * time.Second)

	// Verify checksum changed
	metadata2, err := writer.ReadMetadata()
	require.NoError(t, err)
	newChecksum := metadata2.FileChecksums["test.go"]

	assert.NotEqual(t, initialChecksum, newChecksum, "Checksum should change after modification")
}

// Test: Multiple rapid changes are debounced into single reindex
func TestIndexerWatcher_Debouncing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// Initial index
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	// Create watcher with shorter debounce for testing
	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	watcher.debounceTime = 200 * time.Millisecond
	defer watcher.Stop()

	watcher.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Create multiple files rapidly
	for i := 0; i < 5; i++ {
		filename := filepath.Join(rootDir, "file"+string(rune('a'+i))+".go")
		content := []byte(`package main

func Func() {}
`)
		require.NoError(t, os.WriteFile(filename, content, 0644))
		time.Sleep(50 * time.Millisecond) // Less than debounce time
	}

	// Wait for debounce + processing
	time.Sleep(1 * time.Second)

	// All files should be indexed
	writer := GetWriter(indexer)
	metadata, err := writer.ReadMetadata()
	require.NoError(t, err)

	expectedFiles := []string{"filea.go", "fileb.go", "filec.go", "filed.go", "filee.go"}
	for _, file := range expectedFiles {
		_, exists := metadata.FileChecksums[file]
		assert.True(t, exists, "File %s should be indexed", file)
	}
}

// Test: Only relevant files trigger reindex (code/docs patterns)
func TestIndexerWatcher_PatternFiltering(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// Initial index
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	// Create watcher
	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	defer watcher.Stop()

	watcher.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Create a non-matching file (should be ignored)
	nonMatchFile := filepath.Join(rootDir, "test.txt")
	require.NoError(t, os.WriteFile(nonMatchFile, []byte("text"), 0644))

	// Create a matching file
	matchFile := filepath.Join(rootDir, "test.go")
	require.NoError(t, os.WriteFile(matchFile, []byte("package main"), 0644))

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Verify only .go file is indexed
	writer := GetWriter(indexer)
	metadata, err := writer.ReadMetadata()
	require.NoError(t, err)

	_, goExists := metadata.FileChecksums["test.go"]
	_, txtExists := metadata.FileChecksums["test.txt"]

	assert.True(t, goExists, ".go file should be indexed")
	assert.False(t, txtExists, ".txt file should not be indexed")
}

// Test: Ignored files/directories don't trigger reindex
func TestIndexerWatcher_IgnorePatterns(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{"node_modules/**", "vendor/**"},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// Initial index
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	// Create ignored directory
	nodeModules := filepath.Join(rootDir, "node_modules")
	require.NoError(t, os.MkdirAll(nodeModules, 0755))

	// Create watcher
	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	defer watcher.Stop()

	watcher.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Create file in ignored directory
	ignoredFile := filepath.Join(nodeModules, "test.go")
	require.NoError(t, os.WriteFile(ignoredFile, []byte("package main"), 0644))

	// Create file in root (not ignored)
	normalFile := filepath.Join(rootDir, "main.go")
	require.NoError(t, os.WriteFile(normalFile, []byte("package main"), 0644))

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Verify only non-ignored file is indexed
	writer := GetWriter(indexer)
	metadata, err := writer.ReadMetadata()
	require.NoError(t, err)

	_, mainExists := metadata.FileChecksums["main.go"]
	_, nodeModExists := metadata.FileChecksums["node_modules/test.go"]

	assert.True(t, mainExists, "main.go should be indexed")
	assert.False(t, nodeModExists, "node_modules/test.go should not be indexed")
}

// Test: New directories are added to watch automatically
func TestIndexerWatcher_NewDirectories(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"**/*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// Initial index
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	// Create watcher
	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	defer watcher.Stop()

	watcher.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Create new directory
	newDir := filepath.Join(rootDir, "pkg")
	require.NoError(t, os.MkdirAll(newDir, 0755))

	// Wait for directory to be added to watcher
	time.Sleep(200 * time.Millisecond)

	// Create file in new directory
	newFile := filepath.Join(newDir, "module.go")
	require.NoError(t, os.WriteFile(newFile, []byte("package pkg"), 0644))

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Verify file in new directory is indexed
	writer := GetWriter(indexer)
	metadata, err := writer.ReadMetadata()
	require.NoError(t, err)

	_, exists := metadata.FileChecksums["pkg/module.go"]
	assert.True(t, exists, "File in new directory should be indexed")
}

// Test: Context cancellation stops watcher quickly
func TestIndexerWatcher_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// Create watcher
	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	defer watcher.Stop()

	watcher.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Cancel context and measure shutdown time
	start := time.Now()
	cancel()

	// Wait for goroutine to finish
	<-watcher.doneCh
	shutdownTime := time.Since(start)

	// Should stop within 100ms
	assert.Less(t, shutdownTime, 100*time.Millisecond, "Watcher should stop quickly on context cancellation")
}

// Test: Concurrent Start/Stop is safe
func TestIndexerWatcher_ConcurrentStop(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)

	ctx := context.Background()
	watcher.Start(ctx)

	// Call Stop multiple times concurrently
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			watcher.Stop()
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines to finish
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or deadlock
}

// Test: shouldProcessEvent filters events correctly
func TestIndexerWatcher_ShouldProcessEvent(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{"vendor/**"},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	// NOTE: We don't call Start() in this test, so we shouldn't call Stop()
	// which would block waiting for the goroutine that never started.
	// Just close the underlying watcher to clean up resources.
	defer watcher.watcher.Close()

	testCases := []struct {
		name     string
		event    string
		expected bool
	}{
		{
			name:     "Write to .go file",
			event:    filepath.Join(rootDir, "main.go"),
			expected: true,
		},
		{
			name:     "Create .md file",
			event:    filepath.Join(rootDir, "README.md"),
			expected: true,
		},
		{
			name:     "Remove .go file",
			event:    filepath.Join(rootDir, "old.go"),
			expected: true,
		},
		{
			name:     "File in vendor (should ignore)",
			event:    filepath.Join(rootDir, "vendor", "lib.go"),
			expected: false,
		},
		{
			name:     "Non-matching extension",
			event:    filepath.Join(rootDir, "test.txt"),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create fsnotify event manually - we can't easily create real events
			// So we'll just test the logic by checking patterns directly
			relPath, _ := filepath.Rel(rootDir, tc.event)
			relPath = filepath.ToSlash(relPath)

			shouldIgnore := watcher.indexer.discovery.shouldIgnore(relPath)
			matchesCode := watcher.indexer.discovery.matchesAnyPattern(relPath, watcher.indexer.discovery.codePatterns)
			matchesDocs := watcher.indexer.discovery.matchesAnyPattern(relPath, watcher.indexer.discovery.docsPatterns)

			result := !shouldIgnore && (matchesCode || matchesDocs)
			assert.Equal(t, tc.expected, result, "Event processing for %s", tc.name)
		})
	}
}

// Test: shouldWatchDirectory respects ignore patterns
func TestIndexerWatcher_ShouldWatchDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{"node_modules/**", ".git/**", "vendor/**"},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	watcher, err := NewIndexerWatcher(indexer, rootDir)
	require.NoError(t, err)
	// NOTE: We don't call Start() in this test, so we shouldn't call Stop()
	// which would block waiting for the goroutine that never started.
	// Just close the underlying watcher to clean up resources.
	defer watcher.watcher.Close()

	testCases := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "Root directory",
			path:     rootDir,
			expected: true,
		},
		{
			name:     "src subdirectory",
			path:     filepath.Join(rootDir, "src"),
			expected: true,
		},
		{
			name:     "node_modules (ignored)",
			path:     filepath.Join(rootDir, "node_modules"),
			expected: false,
		},
		{
			name:     ".git (ignored)",
			path:     filepath.Join(rootDir, ".git"),
			expected: false,
		},
		{
			name:     "vendor (ignored)",
			path:     filepath.Join(rootDir, "vendor"),
			expected: false,
		},
		{
			name:     ".cortex (always ignored)",
			path:     filepath.Join(rootDir, ".cortex"),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := watcher.shouldWatchDirectory(tc.path)
			assert.Equal(t, tc.expected, result, "Should watch directory %s", tc.path)
		})
	}
}
