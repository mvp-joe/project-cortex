package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/git"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// Cache manages cache directory location and database access.
// Encapsulates cache root directory to avoid environment variable pollution in tests.
type Cache struct {
	// cacheRoot is the root directory for all cache data.
	// If empty, defaults to ~/.cortex/cache
	cacheRoot string
}

// NewCache creates a new Cache instance.
// If cacheRoot is empty, defaults to ~/.cortex/cache
func NewCache(cacheRoot string) *Cache {
	return &Cache{cacheRoot: cacheRoot}
}

// GetCachePath returns the cache directory path for a given cache key.
// Format: {cacheRoot}/{cacheKey}
func (c *Cache) GetCachePath(cacheKey string) string {
	root := c.cacheRoot
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".cortex", "cache")
	}
	return filepath.Join(root, cacheKey)
}

// LoadOrCreateSettings loads existing settings or creates new ones.
// If settings.local.json doesn't exist or is invalid, new settings are created
// based on the current project state.
func (c *Cache) LoadOrCreateSettings(projectPath string) (*Settings, error) {
	settingsPath := filepath.Join(projectPath, ".cortex", "settings.local.json")

	// Try loading existing settings
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		var settings Settings
		if err := unmarshalSettings(data, &settings); err == nil {
			return &settings, nil
		}
	}

	// Create new settings based on current project state
	cacheKey, err := GetCacheKey(projectPath)
	if err != nil {
		return nil, err
	}

	gitOps := git.NewOperations()
	remote := gitOps.GetRemoteURL(projectPath)
	settings := &Settings{
		CacheKey:      cacheKey,
		CacheLocation: c.GetCachePath(cacheKey),
		RemoteURL:     normalizeRemoteURL(remote),
		WorktreePath:  gitOps.GetWorktreeRoot(projectPath),
		SchemaVersion: "2.0",
	}

	return settings, nil
}

// EnsureCacheLocation handles cache migration when the project identity changes.
// It detects changes to git remote or worktree location and migrates the cache
// to the new location. Returns the current cache path.
//
// Migration scenarios:
//   - First run: Create new cache directory
//   - No change: Return existing cache path
//   - Key changed (same filesystem): Atomic rename of cache directory
//   - Key changed (cross-filesystem): Create new cache, log warning
//
// This function is idempotent and safe to call on every startup.
func (c *Cache) EnsureCacheLocation(projectPath string) (string, error) {
	// Load existing settings or create new ones
	settings, err := c.LoadOrCreateSettings(projectPath)
	if err != nil {
		return "", fmt.Errorf("failed to load settings: %w", err)
	}

	// Calculate current cache key based on project state
	currentKey, err := GetCacheKey(projectPath)
	if err != nil {
		return "", fmt.Errorf("failed to get cache key: %w", err)
	}

	// Check if cache key has changed (remote added/changed, or project moved)
	if settings.CacheKey != "" && settings.CacheKey != currentKey {
		oldPath := expandPath(settings.CacheLocation)
		newPath := c.GetCachePath(currentKey)

		// Only attempt migration if old cache exists
		if pathExists(oldPath) {
			if err := migrateCache(oldPath, newPath, settings.CacheKey, currentKey); err != nil {
				return "", err
			}
		}
	}

	// Update settings with current project state
	newPath := c.GetCachePath(currentKey)
	gitOps := git.NewOperations()
	settings.CacheKey = currentKey
	settings.CacheLocation = newPath
	settings.RemoteURL = gitOps.GetRemoteURL(projectPath)
	settings.WorktreePath = gitOps.GetWorktreeRoot(projectPath)

	// Save updated settings
	if err := settings.Save(projectPath); err != nil {
		return "", fmt.Errorf("failed to save settings: %w", err)
	}

	// Ensure cache directory structure exists
	// Layout: {cachePath}/branches/{branch}.db
	branchesDir := filepath.Join(newPath, "branches")
	if err := os.MkdirAll(branchesDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create branches directory: %w", err)
	}

	return newPath, nil
}

// OpenDatabase opens the SQLite database for the specified project and branch.
// If readOnly is true, opens in read-only mode and checks if file exists.
// If readOnly is false, opens in write mode and initializes schema.
//
// Database location: {cacheRoot}/{cacheKey}/branches/{branch}.db
//
// This is the single source of truth for database connection management.
// Both indexing (write mode) and MCP server (read mode) use this function.
func (c *Cache) OpenDatabase(projectPath string, branch string, readOnly bool) (*sql.DB, error) {
	// Initialize sqlite-vec extension before any database operations
	storage.InitVectorExtension()

	// Get cache settings
	settings, err := c.LoadOrCreateSettings(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load cache settings: %w", err)
	}

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
