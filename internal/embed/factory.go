package embed

import (
	"fmt"

	"github.com/josephward/project-cortex/internal/embed/client"
)

// Config contains configuration for creating an embedding provider.
type Config struct {
	// Provider specifies which embedding provider to use ("local", "openai", etc.)
	Provider string

	// Endpoint is the URL for the embedding service (for local provider)
	Endpoint string

	// BinaryPath is the path to the cortex-embed binary (for local provider)
	BinaryPath string

	// APIKey for cloud providers (future)
	APIKey string

	// Model name (future: for provider-specific model selection)
	Model string
}

// NewProvider creates an embedding provider based on the configuration.
// Currently supports "local" provider. Future: OpenAI, Anthropic, etc.
func NewProvider(config Config) (Provider, error) {
	switch config.Provider {
	case "local", "": // empty defaults to local
		binaryPath := config.BinaryPath
		if binaryPath == "" {
			binaryPath = "cortex-embed" // Default binary name
		}
		return client.NewLocalProvider(binaryPath)

	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s (supported: local)", config.Provider)
	}
}
