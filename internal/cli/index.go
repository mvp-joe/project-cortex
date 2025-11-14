package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/indexer"
	"github.com/spf13/cobra"
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
  - Generates embeddings via embedding server
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

	// Get cache settings to obtain cache root path
	cacheSettings, err := cache.LoadOrCreateSettings(rootDir)
	if err != nil {
		return fmt.Errorf("failed to load cache settings: %w", err)
	}

	// Open database connection using centralized cache management
	if !quietFlag {
		fmt.Println("Opening database connection...")
	}
	db, err := cache.OpenDatabase(rootDir, false) // false = write mode
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	if !quietFlag {
		fmt.Println("✓ Database connection ready")
	}

	// Create embedding provider
	embedProvider, err := embed.NewProvider(embed.Config{
		Provider: cfg.Embedding.Provider,
		Endpoint: cfg.Embedding.Endpoint,
	})
	if err != nil {
		return fmt.Errorf("failed to create embedding provider: %w", err)
	}
	defer embedProvider.Close()

	// Initialize provider (downloads binary if needed, starts server, waits for ready)
	if !quietFlag {
		fmt.Println("Initializing embedding provider...")
	}
	if err := embedProvider.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize embedding provider: %w", err)
	}

	if !quietFlag {
		fmt.Println("✓ Embedding provider ready")
	}

	// Create progress reporter
	progress := NewCLIProgressReporter(quietFlag)

	// Create indexer components (v2 architecture)
	if !quietFlag {
		log.Println("Initializing indexer...")
	}

	// Create storage
	storage, err := indexer.NewSQLiteStorage(db, cacheSettings.CacheLocation, rootDir)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	// Create file discovery
	discovery, err := indexer.NewFileDiscovery(rootDir, indexerConfig.CodePatterns, indexerConfig.DocsPatterns, indexerConfig.IgnorePatterns)
	if err != nil {
		return fmt.Errorf("failed to create file discovery: %w", err)
	}

	// Create change detector
	changeDetector := indexer.NewChangeDetector(rootDir, storage, discovery)

	// Create parser, chunker, formatter
	parser := indexer.NewParser()
	chunker := indexer.NewChunker(indexerConfig.DocChunkSize, indexerConfig.Overlap)
	formatter := indexer.NewFormatter()

	// Create processor
	processor := indexer.NewProcessor(rootDir, parser, chunker, formatter, embedProvider, storage, progress)

	// Create v2 indexer
	idx := indexer.NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Check if watch mode is enabled
	if watchFlag {
		// TODO: Implement watch mode using WatchCoordinator from internal/watcher
		return fmt.Errorf("watch mode not yet implemented with v2 indexer - coming soon")
	}

	// Run indexing (v2 automatically handles incremental via change detection)
	if !quietFlag {
		log.Println("Starting indexing...")
	}

	stats, err := idx.Index(ctx, nil) // nil hint = full discovery with change detection
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("indexing cancelled")
		}
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Print summary
	if !quietFlag {
		fmt.Printf("\n✓ Indexing complete:\n")
		fmt.Printf("  Files: %d added, %d modified, %d deleted (%d unchanged)\n",
			stats.FilesAdded, stats.FilesModified, stats.FilesDeleted, stats.FilesUnchanged)
		fmt.Printf("  Chunks: %d code + %d docs = %d total\n",
			stats.TotalCodeChunks, stats.TotalDocChunks, stats.TotalCodeChunks+stats.TotalDocChunks)
		fmt.Printf("  Time: %v\n", stats.IndexingTime)
	} else {
		fmt.Printf("Indexing complete: %d chunks in %v\n",
			stats.TotalCodeChunks+stats.TotalDocChunks, stats.IndexingTime)
	}

	return nil
}
