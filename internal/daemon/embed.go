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
// socketPath: Unix socket path for the embed daemon (from GlobalConfig).
func EnsureEmbedDaemon(ctx context.Context, socketPath string) error {
	cfg, err := NewEmbedDaemonConfig(socketPath)
	if err != nil {
		return fmt.Errorf("failed to create embed daemon config: %w", err)
	}

	return EnsureDaemon(ctx, cfg)
}

// NewEmbedDaemonConfig creates daemon config for embedding server.
// socketPath: Unix socket path for the embed daemon (from GlobalConfig).
func NewEmbedDaemonConfig(socketPath string) (*DaemonConfig, error) {
	// Ensure parent directory exists
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
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
