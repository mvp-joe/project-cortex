package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// NewIndexerDaemonConfig creates daemon config for indexer server.
// socketPath: Unix socket path for the indexer daemon (from GlobalConfig).
func NewIndexerDaemonConfig(socketPath string) (*DaemonConfig, error) {
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
		"indexer",                              // name
		socketPath,                             // socket path
		[]string{execPath, "indexer", "start"}, // start command
		30*time.Second,                         // startup timeout
	)
}

// EnsureIndexerDaemon ensures the indexer daemon is running.
// Uses the same pattern as EnsureEmbedDaemon.
// Safe to call concurrently - daemon-side singleton enforcement prevents duplicates.
// socketPath: Unix socket path for the indexer daemon (from GlobalConfig).
func EnsureIndexerDaemon(ctx context.Context, socketPath string) error {
	cfg, err := NewIndexerDaemonConfig(socketPath)
	if err != nil {
		return fmt.Errorf("failed to create indexer daemon config: %w", err)
	}

	return EnsureDaemon(ctx, cfg)
}
