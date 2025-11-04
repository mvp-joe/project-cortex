package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/spf13/cobra"
)

var cleanQuietFlag bool
var cleanAllFlag bool

// cleanCmd represents the clean command
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean SQLite cache to force full reindex",
	Long: `Clean removes the SQLite cache database for the current branch.
This forces a complete reindex on the next 'cortex index' run.

By default, only the current branch's cache is deleted. Use --all to delete
the entire cache (all branches).

The configuration file (.cortex/config.yml) is preserved.

Use cases:
  - Changed embedding model or dimensions
  - Corrupted cache data
  - Want fresh start after major refactoring
  - Debugging indexing issues

Examples:
  # Clean cache for current branch
  cortex clean

  # Clean entire cache (all branches)
  cortex clean --all

  # Clean with minimal output
  cortex clean --quiet
`,
	RunE: runClean,
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().BoolVarP(&cleanQuietFlag, "quiet", "q", false, "Suppress output messages")
	cleanCmd.Flags().BoolVarP(&cleanAllFlag, "all", "a", false, "Delete entire cache (all branches)")
}

func runClean(cmd *cobra.Command, args []string) error {
	// Get project path
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Get cache location
	cachePath, err := cache.EnsureCacheLocation(projectPath)
	if err != nil {
		return fmt.Errorf("failed to get cache location: %w", err)
	}

	// Check if cache directory exists
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		if !cleanQuietFlag {
			fmt.Println("No cache found for this project")
		}
		return nil
	}

	// Handle --all flag: delete entire cache directory
	if cleanAllFlag {
		// Calculate size before deletion
		totalSize, branchCount, err := getCacheStats(cachePath)
		if err != nil {
			totalSize = 0
			branchCount = 0
		}

		// Delete entire cache directory
		if err := os.RemoveAll(cachePath); err != nil {
			return fmt.Errorf("failed to remove cache: %w", err)
		}

		if !cleanQuietFlag {
			if branchCount > 0 {
				fmt.Printf("✓ Cleaned entire cache (%d branches, ~%.1f MB)\n", branchCount, totalSize)
			} else {
				fmt.Println("✓ Cleaned entire cache")
			}
			fmt.Println("Next 'cortex index' will perform a full reindex")
		}

		return nil
	}

	// Default: delete current branch only
	currentBranch := cache.GetCurrentBranch(projectPath)
	branchDBPath := filepath.Join(cachePath, "branches", fmt.Sprintf("%s.db", currentBranch))

	// Check if branch database exists
	if _, err := os.Stat(branchDBPath); os.IsNotExist(err) {
		if !cleanQuietFlag {
			fmt.Printf("No cache found for branch '%s'\n", currentBranch)
		}
		return nil
	}

	// Get database size before deletion
	fileInfo, err := os.Stat(branchDBPath)
	var sizeMB float64
	if err == nil {
		sizeMB = float64(fileInfo.Size()) / (1024 * 1024)
	}

	// Delete branch database
	if err := os.Remove(branchDBPath); err != nil {
		return fmt.Errorf("failed to remove branch database: %w", err)
	}

	if !cleanQuietFlag {
		if sizeMB > 0 {
			fmt.Printf("✓ Cleaned cache for branch '%s' (~%.1f MB)\n", currentBranch, sizeMB)
		} else {
			fmt.Printf("✓ Cleaned cache for branch '%s'\n", currentBranch)
		}
		fmt.Println("Next 'cortex index' will perform a full reindex")
	}

	return nil
}

// getCacheStats calculates total cache size and branch count
func getCacheStats(cachePath string) (totalSizeMB float64, branchCount int, err error) {
	branchesDir := filepath.Join(cachePath, "branches")

	entries, err := os.ReadDir(branchesDir)
	if err != nil {
		return 0, 0, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".db" {
			continue
		}

		branchCount++

		info, err := entry.Info()
		if err == nil {
			totalSizeMB += float64(info.Size()) / (1024 * 1024)
		}
	}

	return totalSizeMB, branchCount, nil
}
