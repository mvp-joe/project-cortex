package daemon

import (
	"errors"
	"time"
)

// DaemonConfig specifies parameters for daemon auto-start via EnsureDaemon.
//
// The configuration controls how the daemon lifecycle is managed, including
// process spawning and startup timeouts. Health checking is done via socket
// dial attempts (canDial helper).
//
// Example:
//
//	cfg, err := daemon.NewDaemonConfig(
//	    "indexer",
//	    "~/.cortex/indexer.sock",
//	    []string{"cortex", "indexer", "start"},
//	    30 * time.Second,
//	)
type DaemonConfig struct {
	// Name is the daemon identifier (e.g., "indexer", "embed").
	// Used for file lock paths: ~/.cortex/{name}.lock
	Name string

	// SocketPath is the Unix domain socket path for daemon communication.
	SocketPath string

	// StartCommand is the command to spawn the daemon.
	// Example: ["cortex", "indexer", "start"]
	StartCommand []string

	// StartupTimeout is the maximum time to wait for daemon to become healthy.
	// If socket doesn't become dialable within this duration, EnsureDaemon fails.
	StartupTimeout time.Duration
}

// NewDaemonConfig creates a validated DaemonConfig.
func NewDaemonConfig(name, socketPath string, startCommand []string, timeout time.Duration) (*DaemonConfig, error) {
	if name == "" {
		return nil, errors.New("daemon name is required")
	}
	if socketPath == "" {
		return nil, errors.New("socket path is required")
	}
	if len(startCommand) == 0 {
		return nil, errors.New("start command is required")
	}
	if timeout <= 0 {
		return nil, errors.New("startup timeout must be positive")
	}

	return &DaemonConfig{
		Name:           name,
		SocketPath:     socketPath,
		StartCommand:   startCommand,
		StartupTimeout: timeout,
	}, nil
}
