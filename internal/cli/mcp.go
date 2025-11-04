package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/mcp"
	"github.com/spf13/cobra"
)

// mcpCmd represents the mcp command
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server for semantic code search",
	Long: `Start the Model Context Protocol (MCP) server that enables LLM-powered coding
assistants like Claude Code to search and understand your codebase.

The MCP server:
- Loads indexed code chunks from SQLite cache
- Provides semantic search via the cortex_search tool
- Communicates via stdio (standard MCP transport)

Example:
  cortex mcp`,
	RunE: runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Load configuration from .cortex/config.yml
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Get current working directory (project root)
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Show startup information (SQLite is the only backend now)
	fmt.Fprintf(os.Stderr, "Cortex MCP Server\n")

	// Show cache location and current branch
	cacheKey, err := cache.GetCacheKey(projectPath)
	if err == nil && cacheKey != "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			cachePath := filepath.Join(homeDir, ".cortex", "cache", cacheKey)
			fmt.Fprintf(os.Stderr, "Cache Location: %s\n", cachePath)
		}
	}

	currentBranch := cache.GetCurrentBranch(projectPath)
	fmt.Fprintf(os.Stderr, "Current Branch: %s\n", currentBranch)
	fmt.Fprintf(os.Stderr, "\n")

	// Build MCP server configuration
	mcpConfig := &mcp.MCPServerConfig{
		ProjectPath: projectPath,
		EmbeddingService: &mcp.EmbeddingServiceConfig{
			BaseURL: cfg.Embedding.Endpoint,
		},
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
	if err := embedProvider.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize embedding provider: %w", err)
	}

	// Wrap provider to match MCP interface
	provider := &providerAdapter{Provider: embedProvider}

	// Create and start MCP server
	server, err := mcp.NewMCPServer(ctx, mcpConfig, provider)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}
	defer server.Close()

	// Serve (blocks until shutdown)
	if err := server.Serve(ctx); err != nil {
		return fmt.Errorf("MCP server error: %w", err)
	}

	return nil
}

// providerAdapter adapts embed.Provider to mcp.EmbeddingProvider interface.
type providerAdapter struct {
	embed.Provider
}

func (a *providerAdapter) Embed(ctx context.Context, texts []string, mode string) ([][]float32, error) {
	embedMode := embed.EmbedMode(mode)
	return a.Provider.Embed(ctx, texts, embedMode)
}
