package indexer

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mvp-joe/project-cortex/internal/cache"
)

// EvictionConfig controls cache eviction behavior.
type EvictionConfig struct {
	Enabled         bool                   // Enable automatic eviction after indexing
	Policy          cache.EvictionPolicy   // Eviction policy to use
	UpdateMetadata  bool                   // Update branch metadata after indexing
}

// DefaultEvictionConfig returns the default eviction configuration.
func DefaultEvictionConfig() EvictionConfig {
	return EvictionConfig{
		Enabled:        true,
		Policy:         cache.DefaultEvictionPolicy(),
		UpdateMetadata: true,
	}
}

// updateBranchMetadata updates cache metadata for the current branch.
// This tracks when the branch was last accessed and its current size/chunk count.
func updateBranchMetadata(projectPath, cacheDir, branch string, stats *ProcessingStats) error {
	// Load existing metadata
	metadata, err := cache.LoadMetadata(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	// Calculate branch database size
	dbPath := filepath.Join(cacheDir, "branches", branch+".db")
	sizeMB := cache.GetBranchDBSize(cacheDir, branch)

	// If database doesn't exist yet, try to get size from file
	if sizeMB == 0 {
		if info, err := os.Stat(dbPath); err == nil {
			sizeMB = float64(info.Size()) / (1024 * 1024)
		}
	}

	// Calculate total chunk count
	totalChunks := stats.TotalCodeChunks + stats.TotalDocChunks

	// Update branch stats
	metadata.UpdateBranchStats(branch, sizeMB, totalChunks)

	// Save metadata
	if err := metadata.Save(cacheDir); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	log.Printf("Updated cache metadata for branch '%s': %.2f MB, %d chunks\n", branch, sizeMB, totalChunks)
	return nil
}

// runEviction runs cache eviction based on the configured policy.
func runEviction(projectPath, cacheDir string, policy cache.EvictionPolicy) error {
	log.Println("Running cache eviction...")

	result, err := cache.EvictStaleBranches(cacheDir, projectPath, policy)
	if err != nil {
		return fmt.Errorf("eviction failed: %w", err)
	}

	if len(result.EvictedBranches) > 0 {
		log.Printf("✓ Evicted %d branches, freed %.2f MB (%.2f MB remaining)\n",
			len(result.EvictedBranches),
			result.FreedMB,
			result.RemainingMB)
		log.Printf("  Evicted: %v\n", result.EvictedBranches)
	} else {
		log.Println("✓ No branches evicted (all branches are recent)")
	}

	return nil
}

// PostIndexEviction should be called after successful indexing to update metadata
// and optionally run eviction. Only applies to SQLite storage.
func PostIndexEviction(
	storage Storage,
	projectPath string,
	stats *ProcessingStats,
	config EvictionConfig,
) error {
	// Only apply to SQLite storage
	sqliteStorage, ok := storage.(*SQLiteStorage)
	if !ok {
		// JSON storage doesn't use cache metadata
		return nil
	}

	cacheDir := sqliteStorage.cachePath
	branch := sqliteStorage.branch

	// Update branch metadata if enabled
	if config.UpdateMetadata {
		if err := updateBranchMetadata(projectPath, cacheDir, branch, stats); err != nil {
			log.Printf("Warning: failed to update branch metadata: %v\n", err)
			// Don't fail indexing if metadata update fails
		}
	}

	// Run eviction if enabled
	if config.Enabled {
		if err := runEviction(projectPath, cacheDir, config.Policy); err != nil {
			log.Printf("Warning: cache eviction failed: %v\n", err)
			// Don't fail indexing if eviction fails
		}
	}

	return nil
}
