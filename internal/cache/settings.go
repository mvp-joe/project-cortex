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

// unmarshalSettings unmarshals JSON data into settings struct.
func unmarshalSettings(data []byte, settings *Settings) error {
	return json.Unmarshal(data, settings)
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
