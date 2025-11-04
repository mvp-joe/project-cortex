package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/storage"
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
//
// For testing: Set CORTEX_CACHE_ROOT environment variable to override the cache root.
// This allows tests to use temporary directories instead of ~/.cortex/cache.
func GetCachePath(cacheKey string) string {
	cacheRoot := os.Getenv("CORTEX_CACHE_ROOT")
	if cacheRoot == "" {
		home, _ := os.UserHomeDir()
		cacheRoot = filepath.Join(home, ".cortex", "cache")
	}
	return filepath.Join(cacheRoot, cacheKey)
}

// OpenDatabase opens the SQLite database for the current project and branch.
// If readOnly is true, opens in read-only mode and checks if file exists.
// If readOnly is false, opens in write mode and initializes schema.
//
// Database location: ~/.cortex/cache/{cacheKey}/branches/{branch}.db
//
// This is the single source of truth for database connection management.
// Both indexing (write mode) and MCP server (read mode) use this function.
func OpenDatabase(projectPath string, readOnly bool) (*sql.DB, error) {
	// Initialize sqlite-vec extension before any database operations
	storage.InitVectorExtension()

	// Get cache settings
	settings, err := LoadOrCreateSettings(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load cache settings: %w", err)
	}

	// Get current branch
	branch := GetCurrentBranch(projectPath)

	// Build database path
	dbPath := filepath.Join(settings.CacheLocation, "branches", fmt.Sprintf("%s.db", branch))

	// Ensure branches directory exists (for write mode)
	if !readOnly {
		branchesDir := filepath.Join(settings.CacheLocation, "branches")
		if err := os.MkdirAll(branchesDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create branches directory: %w", err)
		}
	}

	// Check file exists in read-only mode
	if readOnly {
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("database not found at %s, run 'cortex index' first", dbPath)
		}
	}

	// Open database with appropriate mode
	mode := ""
	if readOnly {
		mode = "?mode=ro"
	}

	db, err := sql.Open("sqlite3", dbPath+mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Initialize schema if not read-only and schema doesn't exist
	if !readOnly {
		version, err := storage.GetSchemaVersion(db)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to check schema version: %w", err)
		}

		if version == "0" {
			if err := storage.CreateSchema(db); err != nil {
				db.Close()
				return nil, fmt.Errorf("failed to create schema: %w", err)
			}
		}
	}

	return db, nil
}
