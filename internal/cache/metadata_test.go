package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan:
// 1. Test newEmptyMetadata creates valid structure
// 2. Test LoadMetadata on non-existent file (graceful degradation)
// 3. Test LoadMetadata on invalid JSON (graceful degradation)
// 4. Test LoadMetadata on valid file
// 5. Test Save creates file atomically
// 6. Test UpdateBranchStats creates new branch metadata
// 7. Test UpdateBranchStats updates existing branch metadata
// 8. Test UpdateBranchStats marks main/master as immortal
// 9. Test UpdateBranchStats recalculates total size
// 10. Test RemoveBranch removes branch and recalculates size
// 11. Test GetBranchStats returns correct metadata
// 12. Test GetBranchDBSize returns file size in MB

func TestNewEmptyMetadata(t *testing.T) {
	t.Parallel()

	cacheDir := "/home/user/.cortex/cache/abc123-def456"
	metadata := newEmptyMetadata(cacheDir)

	assert.Equal(t, "1.0.0", metadata.Version)
	assert.Equal(t, "abc123-def456", metadata.ProjectKey)
	assert.NotNil(t, metadata.Branches)
	assert.Empty(t, metadata.Branches)
	assert.Equal(t, 0.0, metadata.TotalSizeMB)
}

func TestLoadMetadata_NonExistent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache", "test-key")

	metadata, err := LoadMetadata(cacheDir)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", metadata.Version)
	assert.Equal(t, "test-key", metadata.ProjectKey)
	assert.NotNil(t, metadata.Branches)
}

func TestLoadMetadata_InvalidJSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache", "test-key")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Write invalid JSON
	metadataPath := filepath.Join(cacheDir, "metadata.json")
	require.NoError(t, os.WriteFile(metadataPath, []byte("{invalid json}"), 0644))

	// Should return empty metadata (graceful degradation)
	metadata, err := LoadMetadata(cacheDir)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", metadata.Version)
	assert.NotNil(t, metadata.Branches)
}

func TestLoadMetadata_ValidFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache", "test-key")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create valid metadata file
	now := time.Now().UTC().Truncate(time.Second)
	expected := &CacheMetadata{
		Version:      "1.0.0",
		ProjectKey:   "test-key",
		TotalSizeMB:  15.5,
		LastEviction: now,
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now,
				SizeMB:       10.5,
				ChunkCount:   100,
				IsImmortal:   true,
			},
			"feature-x": {
				LastAccessed: now.Add(-24 * time.Hour),
				SizeMB:       5.0,
				ChunkCount:   50,
				IsImmortal:   false,
			},
		},
	}

	data, err := json.MarshalIndent(expected, "", "  ")
	require.NoError(t, err)
	metadataPath := filepath.Join(cacheDir, "metadata.json")
	require.NoError(t, os.WriteFile(metadataPath, data, 0644))

	// Load and verify
	metadata, err := LoadMetadata(cacheDir)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", metadata.Version)
	assert.Equal(t, "test-key", metadata.ProjectKey)
	assert.Equal(t, 15.5, metadata.TotalSizeMB)
	assert.Equal(t, now, metadata.LastEviction)
	assert.Len(t, metadata.Branches, 2)

	// Verify main branch
	mainMeta := metadata.Branches["main"]
	require.NotNil(t, mainMeta)
	assert.Equal(t, 10.5, mainMeta.SizeMB)
	assert.Equal(t, 100, mainMeta.ChunkCount)
	assert.True(t, mainMeta.IsImmortal)

	// Verify feature branch
	featureMeta := metadata.Branches["feature-x"]
	require.NotNil(t, featureMeta)
	assert.Equal(t, 5.0, featureMeta.SizeMB)
	assert.Equal(t, 50, featureMeta.ChunkCount)
	assert.False(t, featureMeta.IsImmortal)
}

func TestSave_CreatesFileAtomically(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache", "test-key")

	metadata := &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test-key",
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: time.Now().UTC(),
				SizeMB:       10.0,
				ChunkCount:   100,
				IsImmortal:   true,
			},
		},
		TotalSizeMB: 10.0,
	}

	// Save should create directory and file
	err := metadata.Save(cacheDir)
	require.NoError(t, err)

	// Verify file exists
	metadataPath := filepath.Join(cacheDir, "metadata.json")
	assert.FileExists(t, metadataPath)

	// Verify temp file was cleaned up
	tmpPath := metadataPath + ".tmp"
	assert.NoFileExists(t, tmpPath)

	// Verify content
	data, err := os.ReadFile(metadataPath)
	require.NoError(t, err)

	var loaded CacheMetadata
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, "1.0.0", loaded.Version)
	assert.Equal(t, "test-key", loaded.ProjectKey)
	assert.Len(t, loaded.Branches, 1)
}

func TestUpdateBranchStats_NewBranch(t *testing.T) {
	t.Parallel()

	metadata := newEmptyMetadata("/test/cache")
	beforeUpdate := time.Now()

	metadata.UpdateBranchStats("feature-x", 5.5, 50)

	branchMeta := metadata.Branches["feature-x"]
	require.NotNil(t, branchMeta)
	assert.Equal(t, 5.5, branchMeta.SizeMB)
	assert.Equal(t, 50, branchMeta.ChunkCount)
	assert.False(t, branchMeta.IsImmortal)
	assert.True(t, branchMeta.LastAccessed.After(beforeUpdate) || branchMeta.LastAccessed.Equal(beforeUpdate))
	assert.Equal(t, 5.5, metadata.TotalSizeMB)
}

func TestUpdateBranchStats_ExistingBranch(t *testing.T) {
	t.Parallel()

	metadata := newEmptyMetadata("/test/cache")
	oldTime := time.Now().Add(-1 * time.Hour)

	// Create initial stats
	metadata.Branches["feature-x"] = &BranchMetadata{
		LastAccessed: oldTime,
		SizeMB:       5.0,
		ChunkCount:   50,
		IsImmortal:   false,
	}
	metadata.TotalSizeMB = 5.0

	// Update stats
	beforeUpdate := time.Now()
	metadata.UpdateBranchStats("feature-x", 7.5, 75)

	branchMeta := metadata.Branches["feature-x"]
	require.NotNil(t, branchMeta)
	assert.Equal(t, 7.5, branchMeta.SizeMB)
	assert.Equal(t, 75, branchMeta.ChunkCount)
	assert.True(t, branchMeta.LastAccessed.After(beforeUpdate) || branchMeta.LastAccessed.Equal(beforeUpdate))
	assert.Equal(t, 7.5, metadata.TotalSizeMB)
}

func TestUpdateBranchStats_ImmortalBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		branchName string
	}{
		{"main branch", "main"},
		{"master branch", "master"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			metadata := newEmptyMetadata("/test/cache")
			metadata.UpdateBranchStats(tt.branchName, 10.0, 100)

			branchMeta := metadata.Branches[tt.branchName]
			require.NotNil(t, branchMeta)
			assert.True(t, branchMeta.IsImmortal, "expected %s to be immortal", tt.branchName)
		})
	}
}

func TestUpdateBranchStats_RecalculatesTotalSize(t *testing.T) {
	t.Parallel()

	metadata := newEmptyMetadata("/test/cache")

	metadata.UpdateBranchStats("main", 10.0, 100)
	assert.Equal(t, 10.0, metadata.TotalSizeMB)

	metadata.UpdateBranchStats("feature-x", 5.0, 50)
	assert.Equal(t, 15.0, metadata.TotalSizeMB)

	metadata.UpdateBranchStats("feature-y", 3.5, 35)
	assert.Equal(t, 18.5, metadata.TotalSizeMB)

	// Update existing branch
	metadata.UpdateBranchStats("main", 12.0, 120)
	assert.Equal(t, 20.5, metadata.TotalSizeMB)
}

func TestRemoveBranch(t *testing.T) {
	t.Parallel()

	metadata := newEmptyMetadata("/test/cache")
	metadata.UpdateBranchStats("main", 10.0, 100)
	metadata.UpdateBranchStats("feature-x", 5.0, 50)
	metadata.UpdateBranchStats("feature-y", 3.0, 30)

	assert.Equal(t, 18.0, metadata.TotalSizeMB)
	assert.Len(t, metadata.Branches, 3)

	// Remove feature-x
	metadata.RemoveBranch("feature-x")

	assert.Equal(t, 13.0, metadata.TotalSizeMB)
	assert.Len(t, metadata.Branches, 2)
	assert.Nil(t, metadata.Branches["feature-x"])
	assert.NotNil(t, metadata.Branches["main"])
	assert.NotNil(t, metadata.Branches["feature-y"])

	// Remove non-existent branch (should not error)
	metadata.RemoveBranch("non-existent")
	assert.Equal(t, 13.0, metadata.TotalSizeMB)
}

func TestGetBranchStats(t *testing.T) {
	t.Parallel()

	metadata := newEmptyMetadata("/test/cache")
	metadata.UpdateBranchStats("main", 10.0, 100)

	// Existing branch
	stats := metadata.GetBranchStats("main")
	require.NotNil(t, stats)
	assert.Equal(t, 10.0, stats.SizeMB)

	// Non-existent branch
	stats = metadata.GetBranchStats("non-existent")
	assert.Nil(t, stats)
}

func TestGetBranchDBSize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Create a test database file
	dbPath := filepath.Join(branchesDir, "main.db")
	testData := make([]byte, 1024*1024*5) // 5 MB
	require.NoError(t, os.WriteFile(dbPath, testData, 0644))

	size := GetBranchDBSize(cacheDir, "main")
	assert.InDelta(t, 5.0, size, 0.1) // Allow 0.1 MB tolerance

	// Non-existent file
	size = GetBranchDBSize(cacheDir, "non-existent")
	assert.Equal(t, 0.0, size)
}

func TestLoadMetadata_NilBranchesMap(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache", "test-key")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create metadata with nil branches map
	metadataPath := filepath.Join(cacheDir, "metadata.json")
	jsonData := `{
		"version": "1.0.0",
		"project_key": "test-key",
		"branches": null,
		"total_size_mb": 0,
		"last_eviction": "2025-01-01T00:00:00Z"
	}`
	require.NoError(t, os.WriteFile(metadataPath, []byte(jsonData), 0644))

	// Load should initialize branches map
	metadata, err := LoadMetadata(cacheDir)
	require.NoError(t, err)
	assert.NotNil(t, metadata.Branches)
	assert.Empty(t, metadata.Branches)
}

func TestSave_RoundTrip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache", "test-key")

	// Create metadata
	now := time.Now().UTC().Truncate(time.Second)
	original := &CacheMetadata{
		Version:      "1.0.0",
		ProjectKey:   "test-key",
		TotalSizeMB:  25.5,
		LastEviction: now,
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now,
				SizeMB:       10.0,
				ChunkCount:   100,
				IsImmortal:   true,
			},
			"feature-x": {
				LastAccessed: now.Add(-24 * time.Hour),
				SizeMB:       15.5,
				ChunkCount:   150,
				IsImmortal:   false,
			},
		},
	}

	// Save
	require.NoError(t, original.Save(cacheDir))

	// Load
	loaded, err := LoadMetadata(cacheDir)
	require.NoError(t, err)

	// Verify all fields
	assert.Equal(t, original.Version, loaded.Version)
	assert.Equal(t, original.ProjectKey, loaded.ProjectKey)
	assert.Equal(t, original.TotalSizeMB, loaded.TotalSizeMB)
	assert.Equal(t, original.LastEviction.Unix(), loaded.LastEviction.Unix()) // Compare Unix timestamps
	assert.Len(t, loaded.Branches, 2)

	// Verify main branch
	mainOrig := original.Branches["main"]
	mainLoaded := loaded.Branches["main"]
	require.NotNil(t, mainLoaded)
	assert.Equal(t, mainOrig.SizeMB, mainLoaded.SizeMB)
	assert.Equal(t, mainOrig.ChunkCount, mainLoaded.ChunkCount)
	assert.Equal(t, mainOrig.IsImmortal, mainLoaded.IsImmortal)

	// Verify feature branch
	featureOrig := original.Branches["feature-x"]
	featureLoaded := loaded.Branches["feature-x"]
	require.NotNil(t, featureLoaded)
	assert.Equal(t, featureOrig.SizeMB, featureLoaded.SizeMB)
	assert.Equal(t, featureOrig.ChunkCount, featureLoaded.ChunkCount)
	assert.Equal(t, featureOrig.IsImmortal, featureLoaded.IsImmortal)
}
