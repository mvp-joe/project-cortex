package cache

// Test Plan for Settings Management:
// - LoadOrCreateSettings creates new settings when none exist
// - LoadOrCreateSettings populates CacheKey in correct format ({remote-hash}-{worktree-hash})
// - LoadOrCreateSettings sets CacheLocation, RemoteURL, WorktreePath, and SchemaVersion
// - LoadOrCreateSettings loads existing settings from settings.local.json
// - LoadOrCreateSettings preserves LastIndexed timestamp when loading
// - LoadOrCreateSettings creates new settings when existing JSON is malformed
// - Settings.Save creates .cortex directory if it doesn't exist
// - Settings.Save writes settings.local.json with correct JSON format
// - Settings.Save uses atomic write pattern (temp file → rename)
// - Settings.Save cleans up temporary file after successful write
// - Settings.Save produces well-formatted JSON with indentation
// - GetCachePath returns absolute path in format ~/.cortex/cache/{cache-key}
// - GetCachePath includes user home directory in path
// - Settings round-trip (create → save → load) preserves all fields
// - Settings JSON format includes all required fields with correct snake_case names
// - Settings without remote use placeholder cache key (00000000-{worktree-hash})

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOrCreateSettingsNew(t *testing.T) {
	testCache := setupTestCache(t)

	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")
	runGitCmd(t, tmpDir, "remote", "add", "origin", "https://github.com/user/repo.git")

	// Load settings (should create new)
	settings, err := testCache.LoadOrCreateSettings(tmpDir)
	require.NoError(t, err)

	// Verify settings structure
	assert.NotEmpty(t, settings.CacheKey)
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{8}$`, settings.CacheKey)
	assert.Contains(t, settings.CacheLocation, settings.CacheKey, "cache location should contain cache key")
	assert.Equal(t, "github.com/user/repo", settings.RemoteURL)

	// Compare worktree paths with symlinks resolved (macOS /var → /private/var)
	expectedPath, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	actualPath, err := filepath.EvalSymlinks(settings.WorktreePath)
	require.NoError(t, err)
	assert.Equal(t, expectedPath, actualPath)

	assert.Equal(t, "2.0", settings.SchemaVersion)
	assert.True(t, settings.LastIndexed.IsZero())
}

func TestLoadOrCreateSettingsExisting(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create .cortex directory
	cortexDir := filepath.Join(tmpDir, ".cortex")
	err := os.MkdirAll(cortexDir, 0755)
	require.NoError(t, err)

	// Write existing settings
	existingSettings := &Settings{
		CacheKey:      "abcd1234-efgh5678",
		CacheLocation: "/home/user/.cortex/cache/abcd1234-efgh5678",
		RemoteURL:     "github.com/existing/repo",
		WorktreePath:  tmpDir,
		LastIndexed:   time.Now(),
		SchemaVersion: "2.0",
	}

	data, err := json.MarshalIndent(existingSettings, "", "  ")
	require.NoError(t, err)

	settingsPath := filepath.Join(cortexDir, "settings.local.json")
	err = os.WriteFile(settingsPath, data, 0644)
	require.NoError(t, err)

	// Load settings (should load existing)
	testCache := NewCache(t.TempDir())
	settings, err := testCache.LoadOrCreateSettings(tmpDir)
	require.NoError(t, err)

	// Verify loaded settings match
	assert.Equal(t, existingSettings.CacheKey, settings.CacheKey)
	assert.Equal(t, existingSettings.CacheLocation, settings.CacheLocation)
	assert.Equal(t, existingSettings.RemoteURL, settings.RemoteURL)
	assert.Equal(t, existingSettings.WorktreePath, settings.WorktreePath)
	assert.Equal(t, existingSettings.SchemaVersion, settings.SchemaVersion)
	assert.WithinDuration(t, existingSettings.LastIndexed, settings.LastIndexed, time.Second)
}

func TestLoadOrCreateSettingsInvalidJSON(t *testing.T) {
	testCache := setupTestCache(t)

	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")
	runGitCmd(t, tmpDir, "remote", "add", "origin", "https://github.com/user/repo.git")

	// Create .cortex directory
	cortexDir := filepath.Join(tmpDir, ".cortex")
	err := os.MkdirAll(cortexDir, 0755)
	require.NoError(t, err)

	// Write invalid JSON
	settingsPath := filepath.Join(cortexDir, "settings.local.json")
	err = os.WriteFile(settingsPath, []byte("invalid json {"), 0644)
	require.NoError(t, err)

	// Load settings (should create new since existing is invalid)
	settings, err := testCache.LoadOrCreateSettings(tmpDir)
	require.NoError(t, err)

	// Verify new settings were created
	assert.NotEmpty(t, settings.CacheKey)
	assert.Equal(t, "github.com/user/repo", settings.RemoteURL)
}

func TestSettingsSave(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	settings := &Settings{
		CacheKey:      "test1234-hash5678",
		CacheLocation: "/test/path",
		RemoteURL:     "github.com/test/repo",
		WorktreePath:  tmpDir,
		LastIndexed:   time.Now(),
		SchemaVersion: "2.0",
	}

	// Save settings
	err := settings.Save(tmpDir)
	require.NoError(t, err)

	// Verify .cortex directory was created
	cortexDir := filepath.Join(tmpDir, ".cortex")
	_, err = os.Stat(cortexDir)
	require.NoError(t, err)

	// Verify settings file exists
	settingsPath := filepath.Join(cortexDir, "settings.local.json")
	_, err = os.Stat(settingsPath)
	require.NoError(t, err)

	// Read back and verify content
	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var loaded Settings
	err = json.Unmarshal(data, &loaded)
	require.NoError(t, err)

	assert.Equal(t, settings.CacheKey, loaded.CacheKey)
	assert.Equal(t, settings.CacheLocation, loaded.CacheLocation)
	assert.Equal(t, settings.RemoteURL, loaded.RemoteURL)
	assert.Equal(t, settings.WorktreePath, loaded.WorktreePath)
	assert.Equal(t, settings.SchemaVersion, loaded.SchemaVersion)
	assert.WithinDuration(t, settings.LastIndexed, loaded.LastIndexed, time.Second)
}

func TestSettingsSaveAtomic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create .cortex directory
	cortexDir := filepath.Join(tmpDir, ".cortex")
	err := os.MkdirAll(cortexDir, 0755)
	require.NoError(t, err)

	settingsPath := filepath.Join(cortexDir, "settings.local.json")

	// Write initial settings
	initial := &Settings{
		CacheKey:      "initial",
		CacheLocation: "/initial/path",
		SchemaVersion: "2.0",
	}
	err = initial.Save(tmpDir)
	require.NoError(t, err)

	// Update settings
	updated := &Settings{
		CacheKey:      "updated",
		CacheLocation: "/updated/path",
		SchemaVersion: "2.0",
	}
	err = updated.Save(tmpDir)
	require.NoError(t, err)

	// Verify temp file doesn't exist
	tmpPath := settingsPath + ".tmp"
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "temp file should be cleaned up")

	// Verify final settings are correct
	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var loaded Settings
	err = json.Unmarshal(data, &loaded)
	require.NoError(t, err)

	assert.Equal(t, "updated", loaded.CacheKey)
	assert.Equal(t, "/updated/path", loaded.CacheLocation)
}

func TestSettingsSaveCreatesDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// .cortex directory doesn't exist yet
	cortexDir := filepath.Join(tmpDir, ".cortex")
	_, err := os.Stat(cortexDir)
	assert.True(t, os.IsNotExist(err))

	settings := &Settings{
		CacheKey:      "test1234",
		SchemaVersion: "2.0",
	}

	// Save should create directory
	err = settings.Save(tmpDir)
	require.NoError(t, err)

	// Verify directory was created
	_, err = os.Stat(cortexDir)
	require.NoError(t, err)
}

func TestGetCachePath(t *testing.T) {
	t.Parallel()
	cacheKey := "test1234-hash5678"

	t.Run("default location", func(t *testing.T) {
		t.Parallel()
		testCache := NewCache("")
		path := testCache.GetCachePath(cacheKey)

		// Verify path format
		assert.Contains(t, path, ".cortex/cache/")
		assert.Contains(t, path, cacheKey)

		// Verify path is absolute (contains home directory)
		home, err := os.UserHomeDir()
		if err == nil {
			assert.Contains(t, path, home)
		}
	})

	t.Run("custom cache root", func(t *testing.T) {
		t.Parallel()
		// Set custom cache root
		customRoot := "/custom/cache/root"
		testCache := NewCache(customRoot)

		path := testCache.GetCachePath(cacheKey)

		// Should use custom root instead of ~/.cortex/cache
		assert.Contains(t, path, customRoot)
		assert.Contains(t, path, cacheKey)
		assert.Equal(t, filepath.Join(customRoot, cacheKey), path)
	})
}

func TestSettingsRoundTrip(t *testing.T) {
	testCache := setupTestCache(t)

	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")
	runGitCmd(t, tmpDir, "remote", "add", "origin", "git@github.com:user/repo.git")

	// Create settings
	settings1, err := testCache.LoadOrCreateSettings(tmpDir)
	require.NoError(t, err)

	// Update LastIndexed
	settings1.LastIndexed = time.Now()

	// Save settings
	err = settings1.Save(tmpDir)
	require.NoError(t, err)

	// Load settings again
	settings2, err := testCache.LoadOrCreateSettings(tmpDir)
	require.NoError(t, err)

	// Verify all fields match
	assert.Equal(t, settings1.CacheKey, settings2.CacheKey)
	assert.Equal(t, settings1.CacheLocation, settings2.CacheLocation)
	assert.Equal(t, settings1.RemoteURL, settings2.RemoteURL)
	assert.Equal(t, settings1.WorktreePath, settings2.WorktreePath)
	assert.Equal(t, settings1.SchemaVersion, settings2.SchemaVersion)
	assert.WithinDuration(t, settings1.LastIndexed, settings2.LastIndexed, time.Second)
}

func TestSettingsJSONFormat(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	settings := &Settings{
		CacheKey:      "a1b2c3d4-e5f6g7h8",
		CacheLocation: "~/.cortex/cache/a1b2c3d4-e5f6g7h8",
		RemoteURL:     "github.com/user/repo",
		WorktreePath:  "/Users/joe/code/myproject",
		LastIndexed:   time.Date(2025, 10, 30, 10, 0, 0, 0, time.UTC),
		SchemaVersion: "2.0",
	}

	// Save and read raw JSON
	err := settings.Save(tmpDir)
	require.NoError(t, err)

	settingsPath := filepath.Join(tmpDir, ".cortex", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	// Verify JSON is well-formatted (indented)
	assert.Contains(t, string(data), "  \"cache_key\":")
	assert.Contains(t, string(data), "  \"remote_url\":")
	assert.Contains(t, string(data), "  \"schema_version\": \"2.0\"")

	// Verify all fields are present
	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "a1b2c3d4-e5f6g7h8", parsed["cache_key"])
	assert.Equal(t, "github.com/user/repo", parsed["remote_url"])
	assert.Equal(t, "2.0", parsed["schema_version"])
}

func TestSettingsWithoutRemote(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Initialize git repo without remote
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")

	// Create settings
	testCache := NewCache(t.TempDir())
	settings, err := testCache.LoadOrCreateSettings(tmpDir)
	require.NoError(t, err)

	// Should have placeholder remote hash
	assert.Regexp(t, `^00000000-[0-9a-f]{8}$`, settings.CacheKey)
	assert.Empty(t, settings.RemoteURL)
}
