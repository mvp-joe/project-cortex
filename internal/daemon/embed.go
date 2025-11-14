package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	embedv1 "github.com/mvp-joe/project-cortex/gen/embed/v1"
	"github.com/mvp-joe/project-cortex/gen/embed/v1/embedv1connect"
)

// EnsureEmbedDaemon ensures the embedding daemon is running.
// Uses the same pattern as EnsureDaemon from daemon foundation.
// Safe to call concurrently - daemon-side singleton enforcement prevents duplicates.
func EnsureEmbedDaemon(ctx context.Context) error {
	cfg, err := NewEmbedDaemonConfig()
	if err != nil {
		return fmt.Errorf("failed to create embed daemon config: %w", err)
	}

	return EnsureDaemon(ctx, cfg)
}

// NewEmbedDaemonConfig creates daemon config for embedding server.
func NewEmbedDaemonConfig() (*DaemonConfig, error) {
	socketPath, err := GetEmbedSocketPath()
	if err != nil {
		return nil, err
	}

	// Use currently running binary path instead of "cortex" from PATH
	// This ensures bin/cortex launches bin/cortex, not global cortex
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	return NewDaemonConfig(
		"embed",                        // name
		socketPath,                     // socket path
		[]string{execPath, "embed", "start"}, // start command
		30*time.Second,                 // startup timeout
	)
}

// GetEmbedSocketPath returns the Unix socket path for embedding daemon.
// Respects CORTEX_EMBED_SOCKET environment variable override.
func GetEmbedSocketPath() (string, error) {
	// Allow override via environment variable
	if override := os.Getenv("CORTEX_EMBED_SOCKET"); override != "" {
		return override, nil
	}

	// Use cross-platform home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	cortexDir := filepath.Join(homeDir, ".cortex")

	// Ensure directory exists
	if err := os.MkdirAll(cortexDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cortex directory: %w", err)
	}

	return filepath.Join(cortexDir, "embed.sock"), nil
}

// IsEmbedDaemonHealthy checks if the embedding daemon is running and healthy.
func IsEmbedDaemonHealthy(ctx context.Context, socketPath string) error {
	// Check socket exists
	if _, err := os.Stat(socketPath); err != nil {
		return fmt.Errorf("socket does not exist: %w", err)
	}

	// Create HTTP client over Unix socket
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 1 * time.Second,
	}

	// Create ConnectRPC client
	client := embedv1connect.NewEmbedServiceClient(httpClient, "http://unix")

	// Call Health RPC
	req := connect.NewRequest(&embedv1.HealthRequest{})
	_, err := client.Health(ctx, req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}
