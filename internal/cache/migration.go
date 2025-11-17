package cache

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// migrateCache handles migration from old cache path to new path.
func migrateCache(oldPath, newPath, oldKey, newKey string) error {
	log.Printf("Cache key changed: %s → %s", oldKey, newKey)
	log.Printf("Migrating cache: %s → %s", oldPath, newPath)

	// Try atomic rename (works only on same filesystem)
	if err := os.Rename(oldPath, newPath); err != nil {
		// Rename failed - likely cross-filesystem move
		log.Printf("Warning: Could not migrate cache (cross-filesystem?): %v", err)
		log.Printf("Creating new cache directory instead")

		// Create new cache directory
		if err := os.MkdirAll(newPath, 0755); err != nil {
			return fmt.Errorf("failed to create new cache directory: %w", err)
		}
	} else {
		log.Printf("✓ Cache migrated successfully")
	}

	return nil
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
