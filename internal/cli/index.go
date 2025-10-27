package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"project-cortex/internal/config"
	"project-cortex/internal/indexer"
)

var (
	quietFlag bool
	watchFlag bool
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

  # Index with progress bars disabled
  cortex index --quiet

  # Watch for changes and reindex incrementally
  cortex index --watch

  # Index a specific directory
  cortex index --config /path/to/project/.cortex/config.yml
`,
	RunE: runIndex,
}

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.Flags().BoolVarP(&quietFlag, "quiet", "q", false, "Disable progress bars and non-error output")
	indexCmd.Flags().BoolVarP(&watchFlag, "watch", "w", false, "Watch for file changes and reindex incrementally")
}

func runIndex(cmd *cobra.Command, args []string) error {
	// Set up context with cancellation for Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nInterrupted! Cancelling indexing...")
		cancel()
	}()

	// Determine root directory
	rootDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Load configuration from .cortex/config.yml
	cfg, err := config.LoadConfigFromDir(rootDir)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Convert to indexer configuration
	indexerConfig := cfg.ToIndexerConfig(rootDir)

	// Ensure output directory exists
	outputDir := filepath.Join(rootDir, indexerConfig.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create progress reporter
	progress := NewCLIProgressReporter(quietFlag)

	// Create indexer with progress reporting
	if !quietFlag {
		log.Println("Initializing indexer...")
	}
	idx, err := indexer.NewWithProgress(indexerConfig, progress)
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

	// Check if watch mode is enabled
	if watchFlag {
		// Do initial index first
		if !quietFlag {
			log.Println("Performing initial indexing...")
		}
		stats, err := idx.Index(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("indexing cancelled")
			}
			return fmt.Errorf("initial indexing failed: %w", err)
		}

		if !quietFlag {
			fmt.Printf("Initial indexing complete: %d chunks in %.2fs\n",
				stats.TotalCodeChunks+stats.TotalDocChunks,
				stats.ProcessingTimeSeconds)
			log.Println("Starting watch mode...")
		}

		// Start watch mode (blocks until cancelled)
		if err := idx.Watch(ctx); err != nil && ctx.Err() == nil {
			return fmt.Errorf("watch mode failed: %w", err)
		}

		if !quietFlag {
			log.Println("Watch mode stopped")
		}
		return nil
	}

	// One-time indexing
	stats, err := idx.Index(ctx)
	if err != nil {
		// Check if it was a cancellation
		if ctx.Err() != nil {
			return fmt.Errorf("indexing cancelled")
		}
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Print summary (if not quiet, OnComplete already printed it)
	if quietFlag {
		fmt.Printf("Indexing complete: %d chunks in %.2fs\n",
			stats.TotalCodeChunks+stats.TotalDocChunks,
			stats.ProcessingTimeSeconds)
	}

	return nil
}
