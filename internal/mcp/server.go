package mcp

// Implementation Plan:
// 1. MCPServer struct with searcher and watcher
// 2. NewMCPServer - creates server, initializes searcher, starts watcher
// 3. Serve - starts MCP server on stdio with graceful shutdown
// 4. Graceful shutdown on SIGTERM/SIGINT
// 5. Clean error handling and logging

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/graph"
	"github.com/mvp-joe/project-cortex/internal/pattern"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// MCPServer manages the MCP server lifecycle.
type MCPServer struct {
	config       *MCPServerConfig
	coordinator  *SearcherCoordinator // Coordinates vector + text search
	graphQuerier GraphQuerier
	watcher      *FileWatcher
	graphWatcher *FileWatcher
	provider     EmbeddingProvider
	db           *sql.DB
	mcp          *server.MCPServer
}

// NewMCPServer creates a new MCP server with the given configuration and embedding provider.
// The provider is passed in to avoid import cycles.
func NewMCPServer(ctx context.Context, config *MCPServerConfig, provider EmbeddingProvider) (*MCPServer, error) {
	if config == nil {
		config = DefaultMCPServerConfig()
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding provider is required")
	}

	// Create chunk manager (shared across all searchers)
	// Use new constructor with project path for SQLite support
	chunkManager := NewChunkManagerWithProject(config.ProjectPath, config.ChunksDir)

	// Load initial chunks
	initialSet, err := chunkManager.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load initial chunks: %w", err)
	}

	// Create chromem vector searcher
	chromemSearcher, err := newChromemSearcherWithChunkManager(ctx, config, provider, chunkManager, initialSet)
	if err != nil {
		return nil, fmt.Errorf("failed to create chromem searcher: %w", err)
	}

	// Create bleve exact searcher
	exactSearcher, err := NewExactSearcher(ctx, initialSet)
	if err != nil {
		chromemSearcher.Close()
		return nil, fmt.Errorf("failed to create exact searcher: %w", err)
	}

	// Create coordinator to manage both searchers
	coordinator := NewSearcherCoordinator(chunkManager, chromemSearcher, exactSearcher)

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"cortex-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register cortex_search tool (semantic/vector)
	AddCortexSearchTool(mcpServer, coordinator.GetChromemSearcher())

	// Register cortex_exact tool (keyword/text)
	AddCortexExactTool(mcpServer, coordinator.GetExactSearcher())

	// Create graph searcher
	graphDir := filepath.Join(config.ChunksDir, "..", "graph")
	rootDir := filepath.Join(config.ChunksDir, "..", "..")
	graphStorage, err := graph.NewStorage(graphDir)
	if err != nil {
		coordinator.Close()
		provider.Close()
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	graphQuerier, err := graph.NewSearcher(graphStorage, rootDir)
	if err != nil {
		coordinator.Close()
		provider.Close()
		return nil, fmt.Errorf("failed to create graph searcher: %w", err)
	}

	// Register cortex_graph tool
	AddCortexGraphTool(mcpServer, graphQuerier)

	// Initialize vector extension for SQLite
	storage.InitVectorExtension()

	// Attempt to register cortex_files tool
	var db *sql.DB
	settings, err := cache.LoadOrCreateSettings(config.ProjectPath)
	if err != nil {
		log.Printf("Warning: Failed to load cache settings, cortex_files tool will not be available: %v", err)
	} else {
		branch := cache.GetCurrentBranch(config.ProjectPath)
		dbPath := filepath.Join(settings.CacheLocation, "branches", fmt.Sprintf("%s.db", branch))

		// Check if database file exists
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			log.Printf("Warning: Branch database not found at %s, cortex_files tool will not be available. Run 'cortex index' first.", dbPath)
		} else {
			// Open database in read-only mode
			db, err = sql.Open("sqlite3", dbPath+"?mode=ro")
			if err != nil {
				log.Printf("Warning: Failed to open branch database, cortex_files tool will not be available: %v", err)
			} else {
				// Register cortex_files tool
				AddCortexFilesTool(mcpServer, db)
				log.Printf("Registered cortex_files tool with database: %s", dbPath)
			}
		}
	}

	// Create pattern searcher
	patternSearcher := pattern.NewAstGrepProvider()

	// Register cortex_pattern tool
	AddCortexPatternTool(mcpServer, patternSearcher, config.ProjectPath)

	// Create file watcher for chunks (watches coordinator, not individual searchers)
	// Use auto-detection to watch both JSON and SQLite (if available)
	watcher, err := NewFileWatcherAuto(coordinator, config.ProjectPath, config.ChunksDir)
	if err != nil {
		coordinator.Close()
		graphQuerier.Close()
		provider.Close()
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Create file watcher for graph
	graphWatcher, err := NewFileWatcher(graphQuerier, graphDir)
	if err != nil {
		coordinator.Close()
		graphQuerier.Close()
		watcher.Stop()
		provider.Close()
		return nil, fmt.Errorf("failed to create graph watcher: %w", err)
	}

	return &MCPServer{
		config:       config,
		coordinator:  coordinator,
		graphQuerier: graphQuerier,
		watcher:      watcher,
		graphWatcher: graphWatcher,
		provider:     provider,
		db:           db,
		mcp:          mcpServer,
	}, nil
}

// Serve starts the MCP server and blocks until shutdown.
func (s *MCPServer) Serve(ctx context.Context) error {
	// Start file watchers
	s.watcher.Start(ctx)
	defer s.watcher.Stop()

	s.graphWatcher.Start(ctx)
	defer s.graphWatcher.Stop()

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Start MCP server in goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Starting MCP server on stdio...")
		if err := server.ServeStdio(s.mcp); err != nil {
			errCh <- fmt.Errorf("MCP server error: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case <-sigCh:
		log.Printf("Received shutdown signal, stopping gracefully...")
		cancel()
		return nil
	case err := <-errCh:
		cancel()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close releases all resources.
func (s *MCPServer) Close() error {
	if s.watcher != nil {
		s.watcher.Stop()
	}
	if s.graphWatcher != nil {
		s.graphWatcher.Stop()
	}
	if s.coordinator != nil {
		s.coordinator.Close()
	}
	if s.graphQuerier != nil {
		s.graphQuerier.Close()
	}
	if s.db != nil {
		s.db.Close()
	}
	if s.provider != nil {
		return s.provider.Close()
	}
	return nil
}
