package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Settings represents the local cache settings stored in .cortex/settings.local.json.
// This file tracks the current cache location and project identity.
type Settings struct {
	CacheKey      string    `json:"cache_key"`      // Combined hash: {remoteHash}-{worktreeHash}
	CacheLocation string    `json:"cache_location"` // Full path to cache directory
	RemoteURL     string    `json:"remote_url"`     // Git remote URL (for debugging)
	WorktreePath  string    `json:"worktree_path"`  // Worktree root path (for debugging)
	LastIndexed   time.Time `json:"last_indexed"`   // Last successful index timestamp
	SchemaVersion string    `json:"schema_version"` // Schema version for future migrations
}

// LoadOrCreateSettings loads existing settings or creates new ones.
// If settings.local.json doesn't exist or is invalid, new settings are created
// based on the current project state.
func LoadOrCreateSettings(projectPath string) (*Settings, error) {
	settingsPath := filepath.Join(projectPath, ".cortex", "settings.local.json")

	// Try loading existing settings
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		var settings Settings
		if json.Unmarshal(data, &settings) == nil {
			return &settings, nil
		}
	}

	// Create new settings based on current project state
	cacheKey, err := GetCacheKey(projectPath)
	if err != nil {
		return nil, err
	}

	remote := getGitRemote(projectPath)
	settings := &Settings{
		CacheKey:      cacheKey,
		CacheLocation: GetCachePath(cacheKey),
		RemoteURL:     normalizeRemoteURL(remote),
		WorktreePath:  getWorktreeRoot(projectPath),
		SchemaVersion: "2.0",
	}

	return settings, nil
}

// Save writes settings to disk using atomic write (temp + rename).
// Creates .cortex directory if it doesn't exist.
func (s *Settings) Save(projectPath string) error {
	cortexDir := filepath.Join(projectPath, ".cortex")
	settingsPath := filepath.Join(cortexDir, "settings.local.json")

	// Ensure .cortex directory exists
	if err := os.MkdirAll(cortexDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: write to temp file, then rename
	tmpPath := settingsPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, settingsPath); err != nil {
		// Clean up temp file on failure
		os.Remove(tmpPath)
		return err
	}

	return nil
}

// GetCachePath returns the cache directory path for a given cache key.
// Format: ~/.cortex/cache/{cacheKey}
func GetCachePath(cacheKey string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cortex", "cache", cacheKey)
}
