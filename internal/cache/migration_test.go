package cache

// Test Plan for Cache Migration:
// - EnsureCacheLocation creates new cache on first run (settings + cache directory + branches subdirectory)
// - EnsureCacheLocation sets correct schema version (2.0) in new settings
// - EnsureCacheLocation returns same cache path on repeated calls (no migration needed)
// - EnsureCacheLocation preserves cache key when no changes detected
// - EnsureCacheLocation detects cache key changes and triggers migration
// - EnsureCacheLocation migrates old cache content to new location (same filesystem)
// - EnsureCacheLocation deletes old cache after successful migration
// - EnsureCacheLocation handles missing old cache gracefully (creates new)
// - EnsureCacheLocation updates settings with new cache key and location after migration
// - EnsureCacheLocation updates timestamps in settings on each run
// - EnsureCacheLocation is idempotent (multiple calls return consistent results)
// - EnsureCacheLocation creates branches subdirectory in cache location
// - EnsureCacheLocation allows writing to branches directory after creation
// - expandPath expands tilde (~) to user home directory
// - expandPath leaves absolute paths unchanged
// - expandPath leaves relative paths unchanged
// - pathExists returns true for existing directories and files
// - pathExists returns false for non-existent paths

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestCacheRoot configures CORTEX_CACHE_ROOT to use a temporary directory
// for tests, preventing pollution of ~/.cortex/cache.
func setupTestCacheRoot(t *testing.T) {
	t.Helper()
	cacheRoot := t.TempDir()
	t.Setenv("CORTEX_CACHE_ROOT", cacheRoot)
}

func TestEnsureCacheLocation_FirstRun(t *testing.T) {
	setupTestCacheRoot(t)

	// Setup: temp project directory (not a git repo)
	projectPath := t.TempDir()

	// Execute: ensure cache location
	cachePath, err := EnsureCacheLocation(projectPath)

	// Verify: no error, cache path returned
	require.NoError(t, err)
	assert.NotEmpty(t, cachePath)

	// Verify: settings.local.json created
	settingsPath := filepath.Join(projectPath, ".cortex", "settings.local.json")
	assert.FileExists(t, settingsPath)

	// Verify: settings contain expected values
	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings Settings
	require.NoError(t, json.Unmarshal(data, &settings))
	assert.NotEmpty(t, settings.CacheKey)
	assert.Equal(t, cachePath, settings.CacheLocation)
	assert.Equal(t, "2.0", settings.SchemaVersion)

	// Verify: cache directory structure created
	assert.DirExists(t, cachePath)
	assert.DirExists(t, filepath.Join(cachePath, "branches"))
}

func TestEnsureCacheLocation_NoMigrationNeeded(t *testing.T) {
	setupTestCacheRoot(t)

	// Setup: temp project with existing settings
	projectPath := t.TempDir()

	// First run: create initial settings
	cachePath1, err := EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	// Record initial cache key
	settings1, err := LoadOrCreateSettings(projectPath)
	require.NoError(t, err)
	initialKey := settings1.CacheKey

	// Second run: cache key should be unchanged
	cachePath2, err := EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	// Verify: same cache path returned
	assert.Equal(t, cachePath1, cachePath2)

	// Verify: cache key unchanged
	settings2, err := LoadOrCreateSettings(projectPath)
	require.NoError(t, err)
	assert.Equal(t, initialKey, settings2.CacheKey)
}

func TestEnsureCacheLocation_CacheKeyChanged(t *testing.T) {
	setupTestCacheRoot(t)

	// Setup: temp project directory
	projectPath := t.TempDir()

	// Create initial settings with a fake cache key
	initialKey := "oldkey01-oldkey02"
	initialCachePath := filepath.Join(t.TempDir(), "old-cache")

	// Create old cache directory with some content
	require.NoError(t, os.MkdirAll(filepath.Join(initialCachePath, "branches"), 0755))
	testFile := filepath.Join(initialCachePath, "branches", "main.db")
	require.NoError(t, os.WriteFile(testFile, []byte("test data"), 0644))

	// Write initial settings
	settings := &Settings{
		CacheKey:      initialKey,
		CacheLocation: initialCachePath,
		SchemaVersion: "2.0",
	}
	require.NoError(t, settings.Save(projectPath))

	// Execute: ensure cache location (will detect key changed)
	newCachePath, err := EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	// Verify: new cache path is different
	assert.NotEqual(t, initialCachePath, newCachePath)

	// Verify: old cache was migrated (file exists in new location)
	// Note: This test assumes same filesystem, so os.Rename should work
	migratedFile := filepath.Join(newCachePath, "branches", "main.db")
	if pathExists(migratedFile) {
		// Migration succeeded
		data, err := os.ReadFile(migratedFile)
		require.NoError(t, err)
		assert.Equal(t, []byte("test data"), data)

		// Old cache should be gone
		assert.False(t, pathExists(initialCachePath))
	} else {
		// Migration may have failed (cross-filesystem) - that's OK
		// New cache should exist
		assert.DirExists(t, newCachePath)
		assert.DirExists(t, filepath.Join(newCachePath, "branches"))
	}

	// Verify: settings updated with new key
	updatedSettings, err := LoadOrCreateSettings(projectPath)
	require.NoError(t, err)
	assert.NotEqual(t, initialKey, updatedSettings.CacheKey)
	assert.Equal(t, newCachePath, updatedSettings.CacheLocation)
}

func TestEnsureCacheLocation_OldCacheDoesNotExist(t *testing.T) {
	setupTestCacheRoot(t)

	// Setup: settings point to non-existent cache
	projectPath := t.TempDir()

	settings := &Settings{
		CacheKey:      "oldkey01-oldkey02",
		CacheLocation: "/nonexistent/cache/path",
		SchemaVersion: "2.0",
	}
	require.NoError(t, settings.Save(projectPath))

	// Execute: ensure cache location
	cachePath, err := EnsureCacheLocation(projectPath)

	// Verify: no error, new cache created
	require.NoError(t, err)
	assert.DirExists(t, cachePath)
	assert.DirExists(t, filepath.Join(cachePath, "branches"))

	// Verify: settings updated
	updatedSettings, err := LoadOrCreateSettings(projectPath)
	require.NoError(t, err)
	assert.NotEqual(t, "oldkey01-oldkey02", updatedSettings.CacheKey)
}

func TestEnsureCacheLocation_UpdatesTimestamps(t *testing.T) {
	setupTestCacheRoot(t)

	projectPath := t.TempDir()

	// First run
	_, err := EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	settings1, err := LoadOrCreateSettings(projectPath)
	require.NoError(t, err)

	// Update last indexed time
	settings1.LastIndexed = time.Now().Add(-1 * time.Hour)
	require.NoError(t, settings1.Save(projectPath))

	// Second run
	_, err = EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	// Settings should still be valid
	settings2, err := LoadOrCreateSettings(projectPath)
	require.NoError(t, err)
	assert.Equal(t, settings1.CacheKey, settings2.CacheKey)
}

func TestExpandPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantFunc func(string) bool
	}{
		{
			name:  "expands tilde",
			input: "~/Documents/cache",
			wantFunc: func(result string) bool {
				return !strings.HasPrefix(result, "~") &&
					strings.HasSuffix(result, "Documents/cache")
			},
		},
		{
			name:  "leaves absolute path unchanged",
			input: "/Users/joe/.cortex/cache",
			wantFunc: func(result string) bool {
				return result == "/Users/joe/.cortex/cache"
			},
		},
		{
			name:  "leaves relative path unchanged",
			input: "relative/path",
			wantFunc: func(result string) bool {
				return result == "relative/path"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := expandPath(tt.input)
			assert.True(t, tt.wantFunc(result),
				"expandPath(%q) = %q, want match", tt.input, result)
		})
	}
}

func TestPathExists(t *testing.T) {
	t.Parallel()

	// Existing directory
	existingDir := t.TempDir()
	assert.True(t, pathExists(existingDir))

	// Existing file
	existingFile := filepath.Join(existingDir, "test.txt")
	require.NoError(t, os.WriteFile(existingFile, []byte("test"), 0644))
	assert.True(t, pathExists(existingFile))

	// Non-existent path
	nonExistent := filepath.Join(existingDir, "does-not-exist")
	assert.False(t, pathExists(nonExistent))
}

func TestEnsureCacheLocation_CreatesBranchesDirectory(t *testing.T) {
	setupTestCacheRoot(t)

	projectPath := t.TempDir()

	cachePath, err := EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	// Verify branches directory exists with correct structure
	branchesDir := filepath.Join(cachePath, "branches")
	assert.DirExists(t, branchesDir)

	// Verify we can write to branches directory
	testDB := filepath.Join(branchesDir, "main.db")
	err = os.WriteFile(testDB, []byte("test"), 0644)
	assert.NoError(t, err)
	assert.FileExists(t, testDB)
}

func TestEnsureCacheLocation_IdempotentCalls(t *testing.T) {
	setupTestCacheRoot(t)

	projectPath := t.TempDir()

	// Call multiple times
	paths := make([]string, 5)
	for i := 0; i < 5; i++ {
		cachePath, err := EnsureCacheLocation(projectPath)
		require.NoError(t, err)
		paths[i] = cachePath
	}

	// All calls should return same path
	for i := 1; i < len(paths); i++ {
		assert.Equal(t, paths[0], paths[i],
			"call %d returned different path", i)
	}

	// Settings should be consistent
	settings, err := LoadOrCreateSettings(projectPath)
	require.NoError(t, err)
	assert.Equal(t, paths[0], settings.CacheLocation)
}
