package mcp

// Implementation Plan:
// 1. MCPServer struct with searcher and watcher
// 2. NewMCPServer - creates server, initializes searcher, starts watcher
// 3. Serve - starts MCP server on stdio with graceful shutdown
// 4. Graceful shutdown on SIGTERM/SIGINT
// 5. Clean error handling and logging

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/server"
)

// MCPServer manages the MCP server lifecycle.
type MCPServer struct {
	config   *MCPServerConfig
	searcher ContextSearcher
	watcher  *FileWatcher
	provider EmbeddingProvider
	mcp      *server.MCPServer
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

	// Create searcher
	searcher, err := NewChromemSearcher(ctx, config, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create searcher: %w", err)
	}

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"cortex-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register cortex_search tool
	AddCortexSearchTool(mcpServer, searcher)

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, config.ChunksDir)
	if err != nil {
		searcher.Close()
		provider.Close()
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &MCPServer{
		config:   config,
		searcher: searcher,
		watcher:  watcher,
		provider: provider,
		mcp:      mcpServer,
	}, nil
}

// Serve starts the MCP server and blocks until shutdown.
func (s *MCPServer) Serve(ctx context.Context) error {
	// Start file watcher
	s.watcher.Start(ctx)
	defer s.watcher.Stop()

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
	if s.searcher != nil {
		s.searcher.Close()
	}
	if s.provider != nil {
		return s.provider.Close()
	}
	return nil
}
