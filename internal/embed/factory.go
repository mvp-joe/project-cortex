package embed

import (
	"fmt"
)

// Config contains configuration for creating an embedding provider.
type Config struct {
	// Provider specifies which embedding provider to use ("local", "openai", etc.)
	Provider string

	// Endpoint is the URL for the embedding service (for local provider)
	Endpoint string

	// SocketPath is the Unix socket path for the local embedding daemon
	SocketPath string

	// APIKey for cloud providers (future)
	APIKey string

	// Model name (future: for provider-specific model selection)
	Model string
}

// NewProvider creates an embedding provider based on the configuration.
// Currently supports "local" and "mock" providers. Future: OpenAI, Anthropic, etc.
// Call Initialize() on the returned provider before use.
func NewProvider(config Config) (Provider, error) {
	switch config.Provider {
	case "local", "": // empty defaults to local
		// Just create the provider - Initialize() will handle binary installation
		return newLocalProvider(config.SocketPath)

	case "mock": // for testing
		return newMockProvider(), nil

	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s (supported: local, mock)", config.Provider)
	}
}
