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
	"syscall"

	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/graph"
	"github.com/mvp-joe/project-cortex/internal/pattern"
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

// NewMCPServer creates a new MCP server with the given configuration, database, and embedding provider.
// The database must be opened via cache.OpenDatabase() in read-only mode.
// The provider is passed in to avoid import cycles.
// The server does NOT close the database or provider - caller is responsible for cleanup.
func NewMCPServer(ctx context.Context, config *MCPServerConfig, db *sql.DB, provider EmbeddingProvider) (*MCPServer, error) {
	if config == nil {
		config = DefaultMCPServerConfig()
	}
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding provider is required")
	}

	// Create SQLite-backed vector searcher (replaces chromem)
	vectorSearcher, err := NewSQLiteSearcher(db, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create SQLite vector searcher: %w", err)
	}

	// Create SQLite-backed exact searcher (replaces bleve)
	exactSearcher, err := NewSQLiteExactSearcher(db)
	if err != nil {
		vectorSearcher.Close()
		return nil, fmt.Errorf("failed to create SQLite exact searcher: %w", err)
	}

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"cortex-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register cortex_search tool (semantic/vector) - using SQLite searcher
	AddCortexSearchTool(mcpServer, vectorSearcher)

	// Register cortex_exact tool (keyword/text) - using SQLite searcher
	AddCortexExactTool(mcpServer, exactSearcher)

	// Create graph searcher using SQLite backend
	rootDir := config.ProjectPath

	graphQuerier, err := graph.NewSQLSearcher(db, rootDir)
	if err != nil {
		vectorSearcher.Close()
		exactSearcher.Close()
		return nil, fmt.Errorf("failed to create graph searcher: %w", err)
	}

	// Register cortex_graph tool
	AddCortexGraphTool(mcpServer, graphQuerier)

	// Register cortex_files tool (using shared database connection)
	AddCortexFilesTool(mcpServer, db)
	log.Printf("Registered cortex_files tool")

	// Create pattern searcher
	patternSearcher := pattern.NewAstGrepProvider()

	// Register cortex_pattern tool
	AddCortexPatternTool(mcpServer, patternSearcher, config.ProjectPath)

	// NOTE: Hot reload is no longer needed for SQLite-backed searchers
	// Database is always current (no in-memory cache to reload)
	// File watching will be reimplemented in daemon phase for live source file indexing

	return &MCPServer{
		config:       config,
		coordinator:  nil, // No longer used (SQLite searchers don't need coordination)
		graphQuerier: graphQuerier,
		watcher:      nil, // No longer used (no chunk file watching)
		graphWatcher: nil, // No longer used (SQLite-backed graph)
		provider:     provider,
		db:           db,
		mcp:          mcpServer,
	}, nil
}

// Serve starts the MCP server and blocks until shutdown.
func (s *MCPServer) Serve(ctx context.Context) error {
	// Start file watchers (only if not nil)
	if s.watcher != nil {
		s.watcher.Start(ctx)
		defer s.watcher.Stop()
	}

	if s.graphWatcher != nil {
		s.graphWatcher.Start(ctx)
		defer s.graphWatcher.Stop()
	}

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
// Note: Does NOT close the database or provider - caller is responsible.
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
	// Database and provider are managed by caller
	return nil
}
