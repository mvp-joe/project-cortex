package indexer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan:
// 1. Test DefaultEvictionConfig returns expected values
// 2. Test updateBranchMetadata creates/updates metadata correctly
// 3. Test PostIndexEviction with SQLite storage updates metadata
// 4. Test PostIndexEviction with JSON storage is no-op
// 5. Test PostIndexEviction handles errors gracefully

func TestDefaultEvictionConfig(t *testing.T) {
	t.Parallel()

	config := DefaultEvictionConfig()
	assert.True(t, config.Enabled)
	assert.True(t, config.UpdateMetadata)
	assert.Equal(t, 30, config.Policy.MaxAgeDays)
	assert.Equal(t, 500.0, config.Policy.MaxSizeMB)
}

func TestUpdateBranchMetadata_CreatesMetadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create a test database file
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))
	dbPath := filepath.Join(branchesDir, "main.db")
	testData := make([]byte, 1024*1024*5) // 5 MB
	require.NoError(t, os.WriteFile(dbPath, testData, 0644))

	stats := &ProcessingStats{
		TotalCodeChunks: 100,
		TotalDocChunks:  50,
	}

	// Update metadata
	err := updateBranchMetadata(tmpDir, cacheDir, "main", stats)
	require.NoError(t, err)

	// Verify metadata was created
	metadata, err := cache.LoadMetadata(cacheDir)
	require.NoError(t, err)
	assert.NotNil(t, metadata.Branches["main"])

	branchMeta := metadata.Branches["main"]
	assert.InDelta(t, 5.0, branchMeta.SizeMB, 0.1)
	assert.Equal(t, 150, branchMeta.ChunkCount)
	assert.True(t, branchMeta.IsImmortal)
	assert.True(t, time.Since(branchMeta.LastAccessed) < 5*time.Second)
}

func TestUpdateBranchMetadata_UpdatesExisting(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Create initial metadata
	oldTime := time.Now().Add(-24 * time.Hour)
	metadata := &cache.CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test",
		Branches: map[string]*cache.BranchMetadata{
			"main": {
				LastAccessed: oldTime,
				SizeMB:       3.0,
				ChunkCount:   50,
				IsImmortal:   true,
			},
		},
	}
	require.NoError(t, metadata.Save(cacheDir))

	// Create database file
	dbPath := filepath.Join(branchesDir, "main.db")
	testData := make([]byte, 1024*1024*7) // 7 MB
	require.NoError(t, os.WriteFile(dbPath, testData, 0644))

	stats := &ProcessingStats{
		TotalCodeChunks: 200,
		TotalDocChunks:  100,
	}

	// Update metadata
	err := updateBranchMetadata(tmpDir, cacheDir, "main", stats)
	require.NoError(t, err)

	// Verify metadata was updated
	reloaded, err := cache.LoadMetadata(cacheDir)
	require.NoError(t, err)

	branchMeta := reloaded.Branches["main"]
	assert.InDelta(t, 7.0, branchMeta.SizeMB, 0.1)
	assert.Equal(t, 300, branchMeta.ChunkCount)
	assert.True(t, branchMeta.LastAccessed.After(oldTime))
}

func TestPostIndexEviction_SQLiteStorage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Create mock SQLite storage
	storage := &SQLiteStorage{
		cachePath: cacheDir,
		branch:    "main",
	}

	// Create database file
	dbPath := filepath.Join(branchesDir, "main.db")
	testData := make([]byte, 1024*1024*10) // 10 MB
	require.NoError(t, os.WriteFile(dbPath, testData, 0644))

	stats := &ProcessingStats{
		TotalCodeChunks: 150,
		TotalDocChunks:  75,
	}

	config := EvictionConfig{
		Enabled:        true,
		UpdateMetadata: true,
		Policy:         cache.DefaultEvictionPolicy(),
	}

	// Run post-index eviction
	err := PostIndexEviction(storage, projectPath, stats, config)
	require.NoError(t, err)

	// Verify metadata was created
	metadata, err := cache.LoadMetadata(cacheDir)
	require.NoError(t, err)

	branchMeta := metadata.Branches["main"]
	require.NotNil(t, branchMeta)
	assert.InDelta(t, 10.0, branchMeta.SizeMB, 0.1)
	assert.Equal(t, 225, branchMeta.ChunkCount)
}

func TestPostIndexEviction_MetadataDisabled(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	storage := &SQLiteStorage{
		cachePath: cacheDir,
		branch:    "feature-x",
	}

	stats := &ProcessingStats{
		TotalCodeChunks: 50,
		TotalDocChunks:  25,
	}

	config := EvictionConfig{
		Enabled:        false, // Eviction disabled
		UpdateMetadata: false, // Metadata disabled
		Policy:         cache.DefaultEvictionPolicy(),
	}

	// Run post-index eviction
	err := PostIndexEviction(storage, projectPath, stats, config)
	require.NoError(t, err)

	// Verify metadata file doesn't exist
	metadataPath := filepath.Join(cacheDir, "metadata.json")
	assert.NoFileExists(t, metadataPath)
}

func TestPostIndexEviction_GracefulErrorHandling(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")

	// Don't create cache directory - should handle gracefully
	storage := &SQLiteStorage{
		cachePath: cacheDir,
		branch:    "main",
	}

	stats := &ProcessingStats{
		TotalCodeChunks: 100,
		TotalDocChunks:  50,
	}

	config := DefaultEvictionConfig()

	// Should not error even if cache directory doesn't exist
	err := PostIndexEviction(storage, projectPath, stats, config)
	require.NoError(t, err)
}
