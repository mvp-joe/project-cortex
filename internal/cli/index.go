package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"project-cortex/internal/indexer"
)

// indexCmd represents the index command
var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index the codebase for semantic search",
	Long: `Index processes your codebase (source code + documentation) and generates
semantically searchable chunks with vector embeddings.

The indexer:
  - Parses source code (Go, TypeScript, Python, etc.)
  - Extracts symbols, definitions, and data
  - Chunks documentation by semantic sections
  - Generates embeddings using cortex-embed
  - Stores chunks in .cortex/chunks/ directory

Examples:
  # Index the current directory
  cortex index

  # Index a specific directory
  cortex index --config /path/to/project/.cortex/config.yml
`,
	RunE: runIndex,
}

func init() {
	rootCmd.AddCommand(indexCmd)
}

func runIndex(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Determine root directory
	rootDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Load configuration (use defaults for now)
	// TODO: Load from .cortex/config.yml
	config := indexer.DefaultConfig(rootDir)

	// Ensure output directory exists
	outputDir := filepath.Join(rootDir, config.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create indexer
	log.Println("Initializing indexer...")
	idx, err := indexer.New(config)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	// Ensure provider cleanup on exit
	type closer interface {
		Close() error
	}
	if c, ok := interface{}(idx).(closer); ok {
		defer func() {
			if err := c.Close(); err != nil {
				log.Printf("Warning: failed to close indexer: %v", err)
			}
		}()
	}

	// One-time indexing
	log.Println("Starting indexing process...")
	stats, err := idx.Index(ctx)
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Print summary
	fmt.Println()
	fmt.Println("Indexing Summary:")
	fmt.Printf("  Code files processed:    %d\n", stats.CodeFilesProcessed)
	fmt.Printf("  Docs processed:          %d\n", stats.DocsProcessed)
	fmt.Printf("  Total code chunks:       %d\n", stats.TotalCodeChunks)
	fmt.Printf("  Total doc chunks:        %d\n", stats.TotalDocChunks)
	fmt.Printf("  Processing time:         %.2fs\n", stats.ProcessingTimeSeconds)
	fmt.Println()
	fmt.Printf("✓ Chunks written to: %s\n", outputDir)

	return nil
}
