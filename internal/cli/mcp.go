package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/git"
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

	// Load global configuration for daemon settings
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load global configuration: %w", err)
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

	gitOps := git.NewOperations()
	currentBranch := gitOps.GetCurrentBranch(projectPath)
	fmt.Fprintf(os.Stderr, "Current Branch: %s\n", currentBranch)
	fmt.Fprintf(os.Stderr, "\n")

	// Create cache instance (empty string = default ~/.cortex/cache)
	c := cache.NewCache("")

	// Open database connection using centralized cache management
	fmt.Fprintf(os.Stderr, "Opening database connection...\n")
	db, err := c.OpenDatabase(projectPath, currentBranch, true) // true = read-only mode
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	fmt.Fprintf(os.Stderr, "✓ Database connection ready\n")

	// Build MCP server configuration
	mcpConfig := &mcp.MCPServerConfig{
		ProjectPath: projectPath,
		EmbeddingService: &mcp.EmbeddingServiceConfig{
			BaseURL: cfg.Embedding.Endpoint,
		},
	}

	// Create embedding provider (optional — if it fails, vector search is disabled)
	var embedProvider embed.Provider
	provider, err := embed.NewProvider(embed.Config{
		Provider:   cfg.Embedding.Provider,
		Endpoint:   cfg.Embedding.Endpoint,
		SocketPath: globalCfg.EmbedDaemon.SocketPath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: embedding provider unavailable: %v\n", err)
		fmt.Fprintf(os.Stderr, "  cortex_search (vector) will be disabled; other tools still work\n")
	} else {
		if err := provider.Initialize(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: embedding provider failed to initialize: %v\n", err)
			fmt.Fprintf(os.Stderr, "  cortex_search (vector) will be disabled; other tools still work\n")
			provider.Close()
		} else {
			embedProvider = provider
			defer embedProvider.Close()
		}
	}

	// Create and start MCP server (provider can be nil — vector search disabled)
	server, err := mcp.NewMCPServer(ctx, mcpConfig, db, embedProvider)
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
