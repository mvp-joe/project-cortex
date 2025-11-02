package cache

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

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
func EnsureCacheLocation(projectPath string) (string, error) {
	// Load existing settings or create new ones
	settings, err := LoadOrCreateSettings(projectPath)
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
		newPath := GetCachePath(currentKey)

		// Only attempt migration if old cache exists
		if pathExists(oldPath) {
			log.Printf("Cache key changed: %s → %s", settings.CacheKey, currentKey)
			log.Printf("Migrating cache: %s → %s", oldPath, newPath)

			// Try atomic rename (works only on same filesystem)
			if err := os.Rename(oldPath, newPath); err != nil {
				// Rename failed - likely cross-filesystem move
				log.Printf("Warning: Could not migrate cache (cross-filesystem?): %v", err)
				log.Printf("Creating new cache directory instead")

				// Create new cache directory
				if err := os.MkdirAll(newPath, 0755); err != nil {
					return "", fmt.Errorf("failed to create new cache directory: %w", err)
				}
			} else {
				log.Printf("✓ Cache migrated successfully")
			}
		}
	}

	// Update settings with current project state
	newPath := GetCachePath(currentKey)
	settings.CacheKey = currentKey
	settings.CacheLocation = newPath
	settings.RemoteURL = getGitRemote(projectPath)
	settings.WorktreePath = getWorktreeRoot(projectPath)

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

// expandPath expands ~ in paths to the user's home directory.
// Returns the path unchanged if it doesn't start with ~/.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// pathExists checks if a path exists on the filesystem.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
