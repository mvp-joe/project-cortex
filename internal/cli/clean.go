package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/spf13/cobra"
)

var cleanQuietFlag bool

// cleanCmd represents the clean command
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean generated chunks to force full reindex",
	Long: `Clean removes all generated chunk files from the .cortex/chunks/ directory.
This forces a complete reindex on the next 'cortex index' run.

The configuration file (.cortex/config.yml) is preserved.

Use cases:
  - Changed embedding model or dimensions
  - Corrupted chunk data
  - Want fresh start after major refactoring
  - Debugging indexing issues

Examples:
  # Clean all chunks
  cortex clean

  # Clean with minimal output
  cortex clean --quiet
`,
	RunE: runClean,
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().BoolVarP(&cleanQuietFlag, "quiet", "q", false, "Suppress output messages")
}

func runClean(cmd *cobra.Command, args []string) error {
	// Determine root directory
	rootDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Load configuration to get output directory
	cfg, err := config.LoadConfigFromDir(rootDir)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Convert to indexer configuration to get output directory
	indexerConfig := cfg.ToIndexerConfig(rootDir)
	chunksDir := filepath.Join(rootDir, indexerConfig.OutputDir)

	// Check if chunks directory exists
	if _, err := os.Stat(chunksDir); os.IsNotExist(err) {
		if !cleanQuietFlag {
			fmt.Printf("No chunks directory found at %s\n", chunksDir)
		}
		return nil
	}

	// Find all JSON files in chunks directory
	pattern := filepath.Join(chunksDir, "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to list chunk files: %w", err)
	}

	if len(files) == 0 {
		if !cleanQuietFlag {
			fmt.Println("No chunk files to clean")
		}
		return nil
	}

	// Delete each file
	deletedCount := 0
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			return fmt.Errorf("failed to remove %s: %w", file, err)
		}
		deletedCount++
	}

	if !cleanQuietFlag {
		fmt.Printf("✓ Cleaned %d chunk file(s) from %s\n", deletedCount, chunksDir)
		fmt.Println("Next 'cortex index' will perform a full reindex")
	}

	return nil
}
