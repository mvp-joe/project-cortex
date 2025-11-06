package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TEST PLAN: ChangeDetector Component
//
// The ChangeDetector compares filesystem state to database state to identify:
// - Added files: New files not in DB
// - Modified files: Files with different hash than DB
// - Deleted files: Files in DB but not on disk
// - Unchanged files: Files with same hash (may have mtime drift)
//
// Key optimization: Mtime fast-path
// - If DB mtime == disk mtime, skip hash calculation (file unchanged)
// - Only calculate hash when mtime differs
// - If hash same but mtime differs: mtime drift (mark as Unchanged)
//
// Test Cases:
// 1. No changes detected (all files unchanged)
// 2. New file added (not in DB)
// 3. File modified (content changed, different hash)
// 4. File deleted (in DB, not on disk)
// 5. Mtime drift (content unchanged, mtime changed) - should mark as Unchanged
// 6. Hint optimization (only checks hinted files, not full discovery)
// 7. Large file sets (performance test with 1000+ files)
// 8. Context cancellation (graceful stop)
// 9. Mixed operations (add + modify + delete + unchanged)
// 10. Empty hint (full discovery)
// 11. Non-empty hint with non-existent files (error handling)

// Test 1: No changes detected - all files unchanged (mtime fast-path)
func TestChangeDetector_NoChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(rootDir, "file1.go")
	writeFile(t, file1, "package main\n")

	// Setup DB with matching state
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	// Write file to DB with current mtime and hash
	fileInfo, err := os.Stat(file1)
	require.NoError(t, err)
	hash := calculateHash(t, file1)

	writer := storage.NewFileWriter(db)
	err = writer.WriteFileStats(&storage.FileStats{
		FilePath:     "file1.go",
		FileHash:     hash,
		LastModified: fileInfo.ModTime(),
		Language:     "go",
	})
	require.NoError(t, err)

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes (no hint - full discovery)
	changes, err := detector.DetectChanges(ctx, nil)
	require.NoError(t, err)

	// Verify: all unchanged, nothing else
	assert.Empty(t, changes.Added)
	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Deleted)
	assert.Equal(t, []string{"file1.go"}, changes.Unchanged)
}

// Test 2: New file added (not in DB)
func TestChangeDetector_FileAdded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create new file
	file1 := filepath.Join(rootDir, "new_file.go")
	writeFile(t, file1, "package main\n")

	// Setup DB (empty - no files)
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes
	changes, err := detector.DetectChanges(ctx, nil)
	require.NoError(t, err)

	// Verify: file is added
	assert.Equal(t, []string{"new_file.go"}, changes.Added)
	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Deleted)
	assert.Empty(t, changes.Unchanged)
}

// Test 3: File modified (content changed, different hash)
func TestChangeDetector_FileModified(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create file
	file1 := filepath.Join(rootDir, "file1.go")
	writeFile(t, file1, "package main\n")

	// Setup DB with old version
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	oldHash := calculateHash(t, file1)
	oldMtime := time.Now().Add(-1 * time.Hour)

	writer := storage.NewFileWriter(db)
	err = writer.WriteFileStats(&storage.FileStats{
		FilePath:     "file1.go",
		FileHash:     oldHash,
		LastModified: oldMtime,
		Language:     "go",
	})
	require.NoError(t, err)

	// Modify file (wait to ensure mtime changes)
	time.Sleep(10 * time.Millisecond)
	writeFile(t, file1, "package main\n\nfunc main() {}\n")

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes
	changes, err := detector.DetectChanges(ctx, nil)
	require.NoError(t, err)

	// Verify: file is modified
	assert.Empty(t, changes.Added)
	assert.Equal(t, []string{"file1.go"}, changes.Modified)
	assert.Empty(t, changes.Deleted)
	assert.Empty(t, changes.Unchanged)
}

// Test 4: File deleted (in DB, not on disk)
func TestChangeDetector_FileDeleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Setup DB with file that doesn't exist
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	writer := storage.NewFileWriter(db)
	err = writer.WriteFileStats(&storage.FileStats{
		FilePath:     "deleted_file.go",
		FileHash:     "abc123",
		LastModified: time.Now(),
		Language:     "go",
	})
	require.NoError(t, err)

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes
	changes, err := detector.DetectChanges(ctx, nil)
	require.NoError(t, err)

	// Verify: file is deleted
	assert.Empty(t, changes.Added)
	assert.Empty(t, changes.Modified)
	assert.Equal(t, []string{"deleted_file.go"}, changes.Deleted)
	assert.Empty(t, changes.Unchanged)
}

// Test 5: Mtime drift (content unchanged, mtime changed) - should mark as Unchanged
func TestChangeDetector_MtimeDrift(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create file
	file1 := filepath.Join(rootDir, "file1.go")
	writeFile(t, file1, "package main\n")
	hash := calculateHash(t, file1)

	// Setup DB with same hash but old mtime (simulates git operations)
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	oldMtime := time.Now().Add(-1 * time.Hour)

	writer := storage.NewFileWriter(db)
	err = writer.WriteFileStats(&storage.FileStats{
		FilePath:     "file1.go",
		FileHash:     hash,
		LastModified: oldMtime,
		Language:     "go",
	})
	require.NoError(t, err)

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes
	changes, err := detector.DetectChanges(ctx, nil)
	require.NoError(t, err)

	// Verify: mtime differs but hash same -> Unchanged (needs DB update)
	assert.Empty(t, changes.Added)
	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Deleted)
	assert.Equal(t, []string{"file1.go"}, changes.Unchanged)
}

// Test 6: Hint optimization (only checks hinted files, not full discovery)
func TestChangeDetector_HintOptimization(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create multiple files
	file1 := filepath.Join(rootDir, "file1.go")
	file2 := filepath.Join(rootDir, "file2.go")
	writeFile(t, file1, "package main\n")
	writeFile(t, file2, "package main\n")

	// Setup DB
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	writer := storage.NewFileWriter(db)

	// Add file1 to DB (unchanged)
	fileInfo1, err := os.Stat(file1)
	require.NoError(t, err)
	hash1 := calculateHash(t, file1)
	err = writer.WriteFileStats(&storage.FileStats{
		FilePath:     "file1.go",
		FileHash:     hash1,
		LastModified: fileInfo1.ModTime(),
		Language:     "go",
	})
	require.NoError(t, err)

	// file2 not in DB (would be detected as added)

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes with hint (only check file1)
	changes, err := detector.DetectChanges(ctx, []string{"file1.go"})
	require.NoError(t, err)

	// Verify: only file1 checked (unchanged), file2 ignored
	assert.Empty(t, changes.Added)
	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Deleted)
	assert.Equal(t, []string{"file1.go"}, changes.Unchanged)
}

// Test 7: Large file sets (performance test with 100 files)
func TestChangeDetector_LargeFileSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create 100 files
	numFiles := 100
	for i := 0; i < numFiles; i++ {
		file := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		writeFile(t, file, "package main\n")
	}

	// Setup DB with all files
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	writer := storage.NewFileWriter(db)

	for i := 0; i < numFiles; i++ {
		fileName := fmt.Sprintf("file%d.go", i)
		file := filepath.Join(rootDir, fileName)
		fileInfo, err := os.Stat(file)
		require.NoError(t, err)
		hash := calculateHash(t, file)
		err = writer.WriteFileStats(&storage.FileStats{
			FilePath:     fileName,
			FileHash:     hash,
			LastModified: fileInfo.ModTime(),
			Language:     "go",
		})
		require.NoError(t, err)
	}

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes (performance test)
	start := time.Now()
	changes, err := detector.DetectChanges(ctx, nil)
	elapsed := time.Since(start)
	require.NoError(t, err)

	// Verify: all unchanged
	assert.Empty(t, changes.Added)
	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Deleted)
	assert.Len(t, changes.Unchanged, numFiles)

	// Performance: should be fast (<500ms for 100 files with mtime fast-path)
	assert.Less(t, elapsed.Milliseconds(), int64(500), "Detection should be fast with mtime optimization")
}

// Test 8: Context cancellation (graceful stop)
func TestChangeDetector_ContextCancellation(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()

	// Create many files
	for i := 0; i < 100; i++ {
		file := filepath.Join(rootDir, fmt.Sprintf("file%d.go", i))
		writeFile(t, file, "package main\n")
	}

	// Setup DB
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Detect changes (should fail with context cancelled)
	_, err = detector.DetectChanges(ctx, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// Test 9: Mixed operations (add + modify + delete + unchanged)
func TestChangeDetector_MixedOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create files
	file1 := filepath.Join(rootDir, "unchanged.go")
	file2 := filepath.Join(rootDir, "modified.go")
	file3 := filepath.Join(rootDir, "added.go")
	writeFile(t, file1, "package main\n")
	writeFile(t, file2, "package main\n")

	// Setup DB
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	writer := storage.NewFileWriter(db)

	// Add unchanged file to DB (current state)
	fileInfo1, err := os.Stat(file1)
	require.NoError(t, err)
	hash1 := calculateHash(t, file1)
	err = writer.WriteFileStats(&storage.FileStats{
		FilePath:     "unchanged.go",
		FileHash:     hash1,
		LastModified: fileInfo1.ModTime(),
		Language:     "go",
	})
	require.NoError(t, err)

	// Add modified file to DB (old version)
	oldHash := calculateHash(t, file2)
	err = writer.WriteFileStats(&storage.FileStats{
		FilePath:     "modified.go",
		FileHash:     oldHash,
		LastModified: time.Now().Add(-1 * time.Hour),
		Language:     "go",
	})
	require.NoError(t, err)

	// Add deleted file to DB (doesn't exist on disk)
	err = writer.WriteFileStats(&storage.FileStats{
		FilePath:     "deleted.go",
		FileHash:     "abc123",
		LastModified: time.Now(),
		Language:     "go",
	})
	require.NoError(t, err)

	// Modify file2
	time.Sleep(10 * time.Millisecond)
	writeFile(t, file2, "package main\n\nfunc main() {}\n")

	// Create file3 (added)
	writeFile(t, file3, "package main\n")

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes
	changes, err := detector.DetectChanges(ctx, nil)
	require.NoError(t, err)

	// Verify all operations detected correctly
	assert.ElementsMatch(t, []string{"added.go"}, changes.Added)
	assert.ElementsMatch(t, []string{"modified.go"}, changes.Modified)
	assert.ElementsMatch(t, []string{"deleted.go"}, changes.Deleted)
	assert.ElementsMatch(t, []string{"unchanged.go"}, changes.Unchanged)
}

// Test 10: Empty hint means full discovery
func TestChangeDetector_EmptyHintFullDiscovery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create files
	file1 := filepath.Join(rootDir, "file1.go")
	file2 := filepath.Join(rootDir, "file2.go")
	writeFile(t, file1, "package main\n")
	writeFile(t, file2, "package main\n")

	// Setup DB (empty)
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes with empty hint (nil)
	changes, err := detector.DetectChanges(ctx, nil)
	require.NoError(t, err)

	// Verify: both files discovered and marked as added
	assert.ElementsMatch(t, []string{"file1.go", "file2.go"}, changes.Added)
	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Deleted)
	assert.Empty(t, changes.Unchanged)

	// Also test with empty slice (same as nil)
	changes, err = detector.DetectChanges(ctx, []string{})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"file1.go", "file2.go"}, changes.Added)
}

// Test 11: Non-empty hint with non-existent files (skip missing files)
func TestChangeDetector_HintWithMissingFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create one file
	file1 := filepath.Join(rootDir, "file1.go")
	writeFile(t, file1, "package main\n")

	// Setup DB
	db := storage.NewTestDB(t)

	stor, err := NewSQLiteStorage(db, rootDir, rootDir)
	require.NoError(t, err)

	// Create file discovery
	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	// Create detector
	detector := NewChangeDetector(rootDir, stor, discovery)

	// Detect changes with hint including non-existent file
	changes, err := detector.DetectChanges(ctx, []string{"file1.go", "missing.go"})
	require.NoError(t, err)

	// Verify: file1 added, missing.go ignored
	assert.ElementsMatch(t, []string{"file1.go"}, changes.Added)
	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Deleted)
	assert.Empty(t, changes.Unchanged)
}

// Helper functions

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0755)
	require.NoError(t, err)
	err = os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

func calculateHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
