package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// BenchmarkIncrementalIndexing benchmarks the incremental indexing performance
// with the hybrid two-stage filtering (mtime + checksum).
//
// This benchmark creates a test project with many files, performs a full index,
// then modifies a small percentage of files and benchmarks the incremental index.
//
// Expected results:
// - With 1000 files and 1% change rate (10 files changed):
//   - Old approach: ~200-500ms (reads all 1000 files for checksums)
//   - New approach: ~20-50ms (stats 1000 files, reads only 10 changed files)
//   - Speedup: 4-10x
func BenchmarkIncrementalIndexing(b *testing.B) {
	// Setup: Create test project with many files
	tempDir := b.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require := require.New(b)
	require.NoError(os.MkdirAll(rootDir, 0755))

	// Create 1000 test files
	fileCount := 1000
	for i := 0; i < fileCount; i++ {
		filename := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		content := fmt.Sprintf("package main\n\nfunc File%d() {}\n", i)
		require.NoError(os.WriteFile(filename, []byte(content), 0644))
	}

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(err)
	defer indexer.Close()

	ctx := context.Background()

	// Initial full index (not timed)
	_, err = indexer.Index(ctx)
	require.NoError(err)

	// Modify 10 files (1% change rate - typical for incremental scenario)
	for i := 0; i < 10; i++ {
		filename := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		content := fmt.Sprintf("package main\n\nfunc File%d() {}\nfunc Modified%d() {}\n", i, i)
		require.NoError(os.WriteFile(filename, []byte(content), 0644))
	}

	b.ResetTimer()

	// Benchmark incremental indexing
	for i := 0; i < b.N; i++ {
		_, err := indexer.IndexIncremental(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIncrementalIndexing_NoChanges benchmarks incremental indexing
// when no files have changed (best case scenario).
//
// Expected results:
// - Should return very quickly (<5ms) after detecting no changes
// - Most time spent on file discovery and stat calls
func BenchmarkIncrementalIndexing_NoChanges(b *testing.B) {
	// Setup
	tempDir := b.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require := require.New(b)
	require.NoError(os.MkdirAll(rootDir, 0755))

	// Create 1000 test files
	fileCount := 1000
	for i := 0; i < fileCount; i++ {
		filename := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		content := fmt.Sprintf("package main\n\nfunc File%d() {}\n", i)
		require.NoError(os.WriteFile(filename, []byte(content), 0644))
	}

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(err)
	defer indexer.Close()

	ctx := context.Background()

	// Initial full index
	_, err = indexer.Index(ctx)
	require.NoError(err)

	// No modifications - benchmark detecting no changes

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := indexer.IndexIncremental(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIncrementalIndexing_HighChangeRate benchmarks incremental indexing
// with a high change rate (10% of files changed).
//
// This represents a scenario with substantial changes, but still less than
// what would warrant a full reindex.
func BenchmarkIncrementalIndexing_HighChangeRate(b *testing.B) {
	// Setup
	tempDir := b.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require := require.New(b)
	require.NoError(os.MkdirAll(rootDir, 0755))

	// Create 1000 test files
	fileCount := 1000
	for i := 0; i < fileCount; i++ {
		filename := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		content := fmt.Sprintf("package main\n\nfunc File%d() {}\n", i)
		require.NoError(os.WriteFile(filename, []byte(content), 0644))
	}

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(err)
	defer indexer.Close()

	ctx := context.Background()

	// Initial full index
	_, err = indexer.Index(ctx)
	require.NoError(err)

	// Modify 100 files (10% change rate)
	for i := 0; i < 100; i++ {
		filename := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		content := fmt.Sprintf("package main\n\nfunc File%d() {}\nfunc Modified%d() {}\n", i, i)
		require.NoError(os.WriteFile(filename, []byte(content), 0644))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := indexer.IndexIncremental(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIncrementalIndexing_LargeCodebase benchmarks incremental indexing
// on a larger codebase (10,000 files) with typical change rate.
//
// This tests scalability of the mtime optimization.
func BenchmarkIncrementalIndexing_LargeCodebase(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping large codebase benchmark in short mode")
	}

	// Setup
	tempDir := b.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require := require.New(b)
	require.NoError(os.MkdirAll(rootDir, 0755))

	// Create 10,000 test files (takes a few seconds)
	fileCount := 10000
	b.Logf("Creating %d test files...", fileCount)
	for i := 0; i < fileCount; i++ {
		filename := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		content := fmt.Sprintf("package main\n\nfunc File%d() {}\n", i)
		require.NoError(os.WriteFile(filename, []byte(content), 0644))
	}

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(err)
	defer indexer.Close()

	ctx := context.Background()

	// Initial full index
	b.Logf("Performing initial full index...")
	_, err = indexer.Index(ctx)
	require.NoError(err)

	// Modify 50 files (0.5% change rate)
	b.Logf("Modifying 50 files...")
	for i := 0; i < 50; i++ {
		filename := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		content := fmt.Sprintf("package main\n\nfunc File%d() {}\nfunc Modified%d() {}\n", i, i)
		require.NoError(os.WriteFile(filename, []byte(content), 0644))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := indexer.IndexIncremental(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	b.Logf("Completed %d iterations", b.N)
}

// BenchmarkChecksumCalculation benchmarks the checksum calculation
// for different file sizes to understand the cost of reading files.
func BenchmarkChecksumCalculation(b *testing.B) {
	fileSizes := []struct {
		name string
		size int
	}{
		{"Small_1KB", 1024},
		{"Medium_10KB", 10 * 1024},
		{"Large_100KB", 100 * 1024},
		{"VeryLarge_1MB", 1024 * 1024},
	}

	for _, fs := range fileSizes {
		b.Run(fs.name, func(b *testing.B) {
			tempDir := b.TempDir()
			testFile := filepath.Join(tempDir, "test.go")

			// Create file with specified size
			content := make([]byte, fs.size)
			for i := range content {
				content[i] = byte(i % 256)
			}
			require.NoError(b, os.WriteFile(testFile, content, 0644))

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := calculateChecksum(testFile)
				if err != nil {
					b.Fatal(err)
				}
			}

			b.SetBytes(int64(fs.size))
		})
	}
}

// BenchmarkFileStatVsChecksum compares the performance of os.Stat
// versus reading and checksumming a file.
//
// This demonstrates the performance advantage of mtime-based filtering.
func BenchmarkFileStatVsChecksum(b *testing.B) {
	tempDir := b.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	// Create a typical-sized Go source file (~5KB)
	content := make([]byte, 5*1024)
	for i := range content {
		content[i] = byte('a' + (i % 26))
	}
	require.NoError(b, os.WriteFile(testFile, content, 0644))

	b.Run("Stat", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := os.Stat(testFile)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Checksum", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := calculateChecksum(testFile)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
