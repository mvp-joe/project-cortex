package cli

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/config"
)

// cacheCmd represents the cache command group
var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the SQLite cache",
	Long: `Manage the SQLite cache for indexed code chunks.

The cache command provides utilities for inspecting and managing
the branch-specific SQLite databases used to store indexed chunks.

Available commands:
  info   - Show cache location and stats
  clean  - Manually trigger cache eviction
  stats  - Show detailed per-branch statistics`,
}

// cacheInfoCmd shows cache location and basic stats
var cacheInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show cache location and stats",
	Long: `Display the cache location and basic statistics.

Shows:
  - Cache directory location
  - Storage backend (SQLite or JSON)
  - Total cache size
  - Number of cached branches
  - Last eviction timestamp`,
	RunE: runCacheInfo,
}

// cacheCleanCmd manually triggers cache eviction
var cacheCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Manually trigger cache eviction",
	Long: `Manually trigger cache eviction to remove old/deleted branches.

Eviction criteria (in order):
  1. Branches deleted in git (no longer exist)
  2. Branches older than MaxAgeDays (not accessed recently)
  3. Oldest branches if TotalSize > MaxSizeMB (LRU)

Protected branches (main, master) are never evicted.`,
	RunE: runCacheClean,
}

// cacheStatsCmd shows detailed per-branch statistics
var cacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show detailed per-branch statistics",
	Long: `Display detailed statistics for each cached branch.

Shows for each branch:
  - Branch name
  - Last accessed timestamp
  - Database size (MB)
  - Number of chunks
  - Eviction status (immortal, active, candidate)`,
	RunE: runCacheStats,
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheInfoCmd)
	cacheCmd.AddCommand(cacheCleanCmd)
	cacheCmd.AddCommand(cacheStatsCmd)
}

func runCacheInfo(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Get project path
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Show storage backend
	fmt.Printf("Storage Backend: %s\n", cfg.Storage.Backend)

	// If using JSON, show chunks directory
	if cfg.Storage.Backend == "json" {
		fmt.Printf("Chunks Directory: .cortex/chunks\n")
		return nil
	}

	// SQLite cache info
	cacheKey, err := cache.GetCacheKey(projectPath)
	if err != nil {
		return fmt.Errorf("failed to get cache key: %w", err)
	}

	cachePath, err := cache.EnsureCacheLocation(projectPath)
	if err != nil {
		return fmt.Errorf("failed to get cache location: %w", err)
	}

	fmt.Printf("Cache Location: %s\n", cachePath)
	fmt.Printf("Cache Key: %s\n", cacheKey)

	// Load metadata
	metadata, err := cache.LoadMetadata(cachePath)
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	fmt.Printf("Total Size: %.2f MB\n", metadata.TotalSizeMB)
	fmt.Printf("Branches: %d\n", len(metadata.Branches))

	if !metadata.LastEviction.IsZero() {
		fmt.Printf("Last Eviction: %s\n", formatDuration(time.Since(metadata.LastEviction)))
	} else {
		fmt.Printf("Last Eviction: Never\n")
	}

	// Show current branch
	currentBranch := cache.GetCurrentBranch(projectPath)
	fmt.Printf("\nCurrent Branch: %s\n", currentBranch)

	return nil
}

func runCacheClean(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if using SQLite
	if cfg.Storage.Backend != "sqlite" {
		fmt.Println("Cache eviction is only applicable to SQLite backend")
		return nil
	}

	// Get project path
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	cachePath, err := cache.EnsureCacheLocation(projectPath)
	if err != nil {
		return fmt.Errorf("failed to get cache location: %w", err)
	}

	// Build eviction policy from config
	policy := cache.EvictionPolicy{
		MaxAgeDays:      cfg.Storage.CacheMaxAgeDays,
		MaxSizeMB:       cfg.Storage.CacheMaxSizeMB,
		ProtectBranches: []string{"main", "master"},
	}

	fmt.Println("Running cache eviction...")

	result, err := cache.EvictStaleBranches(cachePath, projectPath, policy)
	if err != nil {
		return fmt.Errorf("eviction failed: %w", err)
	}

	if len(result.EvictedBranches) == 0 {
		fmt.Println("No branches evicted (cache is within limits)")
	} else {
		fmt.Printf("Evicted %d branch(es), freed %.2f MB\n",
			len(result.EvictedBranches),
			result.FreedMB)
		fmt.Printf("Remaining: %.2f MB\n", result.RemainingMB)

		if verbose {
			fmt.Println("\nEvicted branches:")
			for _, branch := range result.EvictedBranches {
				fmt.Printf("  - %s\n", branch)
			}
		}
	}

	return nil
}

func runCacheStats(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if using SQLite
	if cfg.Storage.Backend != "sqlite" {
		fmt.Println("Cache stats are only applicable to SQLite backend")
		return nil
	}

	// Get project path
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	cachePath, err := cache.EnsureCacheLocation(projectPath)
	if err != nil {
		return fmt.Errorf("failed to get cache location: %w", err)
	}

	// Load metadata
	metadata, err := cache.LoadMetadata(cachePath)
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	if len(metadata.Branches) == 0 {
		fmt.Println("No cached branches")
		return nil
	}

	// Get current branch for highlighting
	currentBranch := cache.GetCurrentBranch(projectPath)

	// Sort branches by last accessed (most recent first)
	type branchStats struct {
		name         string
		lastAccessed time.Time
		sizeMB       float64
		chunkCount   int
		isImmortal   bool
		isCurrent    bool
	}

	branches := make([]branchStats, 0, len(metadata.Branches))
	for name, meta := range metadata.Branches {
		branches = append(branches, branchStats{
			name:         name,
			lastAccessed: meta.LastAccessed,
			sizeMB:       meta.SizeMB,
			chunkCount:   meta.ChunkCount,
			isImmortal:   meta.IsImmortal,
			isCurrent:    name == currentBranch,
		})
	}

	sort.Slice(branches, func(i, j int) bool {
		return branches[i].lastAccessed.After(branches[j].lastAccessed)
	})

	// Print header
	fmt.Printf("%-25s %-20s %-10s %-10s %s\n",
		"Branch", "Last Accessed", "Size", "Chunks", "Status")
	fmt.Println("--------------------------------------------------------------------------------")

	// Print each branch
	for _, b := range branches {
		status := "active"
		if b.isImmortal {
			status = "immortal"
		} else {
			age := time.Since(b.lastAccessed)
			if age > time.Duration(cfg.Storage.CacheMaxAgeDays)*24*time.Hour {
				status = "eviction candidate"
			}
		}

		if b.isCurrent {
			status += " (current)"
		}

		fmt.Printf("%-25s %-20s %-10s %-10d %s\n",
			truncate(b.name, 25),
			formatDuration(time.Since(b.lastAccessed)),
			fmt.Sprintf("%.2f MB", b.sizeMB),
			b.chunkCount,
			status)
	}

	fmt.Println()
	fmt.Printf("Total: %.2f MB across %d branch(es)\n",
		metadata.TotalSizeMB,
		len(branches))

	return nil
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", minutes)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

// truncate truncates a string to the specified length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
