package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"project-cortex/internal/embed"
	"project-cortex/internal/mcp"
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

	// Bind flags to viper for config file support
	viper.BindPFlag("mcp.chunks_dir", mcpCmd.Flags().Lookup("chunks-dir"))
}

func runMCP(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Build configuration
	config := &mcp.MCPServerConfig{
		ChunksDir: getChunksDir(),
		EmbeddingService: &mcp.EmbeddingServiceConfig{
			BaseURL: viper.GetString("embedding.endpoint"),
		},
	}

	// Apply defaults if not configured
	if config.EmbeddingService.BaseURL == "" {
		config.EmbeddingService.BaseURL = "http://localhost:8121"
	}

	// Create embedding provider
	embedProvider, err := embed.NewProvider(embed.Config{
		Provider:   "local",
		BinaryPath: "cortex-embed",
	})
	if err != nil {
		return fmt.Errorf("failed to create embedding provider: %w", err)
	}
	defer embedProvider.Close()

	// Wrap provider to match MCP interface
	provider := &providerAdapter{Provider: embedProvider}

	// Create and start MCP server
	server, err := mcp.NewMCPServer(ctx, config, provider)
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

// getChunksDir returns the chunks directory from flag, env var, or default.
func getChunksDir() string {
	// Priority: flag > env var > config > default
	if chunksDir != "" {
		return chunksDir
	}
	if envDir := os.Getenv("CORTEX_CHUNKS_DIR"); envDir != "" {
		return envDir
	}
	if configDir := viper.GetString("mcp.chunks_dir"); configDir != "" {
		return configDir
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
