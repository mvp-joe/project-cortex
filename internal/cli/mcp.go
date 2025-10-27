package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/mcp"
)

var (
	chunksDir string
)

// mcpCmd represents the mcp command
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server for semantic code search",
	Long: `Start the Model Context Protocol (MCP) server that enables LLM-powered coding
assistants like Claude Code to search and understand your codebase.

The MCP server:
- Loads indexed code chunks from .cortex/chunks/
- Provides semantic search via the cortex_search tool
- Watches for chunk updates and hot-reloads automatically
- Communicates via stdio (standard MCP transport)

Example:
  cortex mcp
  cortex mcp --chunks-dir .cortex/chunks`,
	RunE: runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)

	// Flags
	mcpCmd.Flags().StringVar(&chunksDir, "chunks-dir", ".cortex/chunks",
		"Directory containing indexed chunk files")
}

func runMCP(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Load configuration from .cortex/config.yml
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Build MCP server configuration
	mcpConfig := &mcp.MCPServerConfig{
		ChunksDir: getChunksDir(),
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

// getChunksDir returns the chunks directory from flag or env var.
func getChunksDir() string {
	// Priority: flag > env var > default
	if chunksDir != "" {
		return chunksDir
	}
	if envDir := os.Getenv("CORTEX_CHUNKS_DIR"); envDir != "" {
		return envDir
	}
	return ".cortex/chunks"
}

// providerAdapter adapts embed.Provider to mcp.EmbeddingProvider interface.
type providerAdapter struct {
	embed.Provider
}

func (a *providerAdapter) Embed(ctx context.Context, texts []string, mode string) ([][]float32, error) {
	embedMode := embed.EmbedMode(mode)
	return a.Provider.Embed(ctx, texts, embedMode)
}
