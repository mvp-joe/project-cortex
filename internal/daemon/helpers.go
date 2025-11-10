package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// getLockPath returns the file lock path for a daemon.
//
// Lock files are stored in ~/.cortex/ with format: {name}.lock
// Creates the ~/.cortex directory if it doesn't exist.
//
// Example:
//
//	path := getLockPath("indexer")
//	// Returns: ~/.cortex/indexer.lock
func getLockPath(name string) string {
	home, _ := os.UserHomeDir()
	cortexDir := filepath.Join(home, ".cortex")

	// Ensure directory exists
	os.MkdirAll(cortexDir, 0755)

	return filepath.Join(cortexDir, name+".lock")
}

// canDial checks if a Unix socket is dialable (daemon is running).
func canDial(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// waitForHealthy polls the daemon's socket until it becomes dialable or times out.
//
// This function is called after spawning a daemon to ensure it has fully started
// and is ready to accept connections. It polls the socket every 100ms until
// either:
//   - Socket is dialable (daemon is running)
//   - Context timeout expires (returns error)
//   - Context is cancelled (returns error)
//
// The polling interval (100ms) balances responsiveness with CPU usage.
//
// Example:
//
//	cfg, _ := NewDaemonConfig(
//	    "indexer",
//	    "/path/to/socket",
//	    []string{"cortex", "indexer", "start"},
//	    30 * time.Second,
//	)
//	if err := waitForHealthy(ctx, cfg); err != nil {
//	    log.Fatalf("Daemon failed to start: %v", err)
//	}
func waitForHealthy(ctx context.Context, cfg *DaemonConfig) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if canDial(cfg.SocketPath) {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("daemon failed to start within %v", cfg.StartupTimeout)
		}
	}
}

// isAddrInUse checks if an error indicates "address already in use".
//
// This helper is used during singleton enforcement to detect when another daemon
// instance has already bound the Unix socket. It checks both:
//   - syscall.EADDRINUSE error code (reliable)
//   - String matching as fallback (for wrapped errors)
//
// Returns true if the error is definitely "address in use", false otherwise.
//
// Example:
//
//	listener, err := net.Listen("unix", socketPath)
//	if isAddrInUse(err) {
//	    // Another daemon is running
//	    return false, nil
//	}
func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}

	// Check for syscall EADDRINUSE (most reliable)
	if opErr, ok := err.(*net.OpError); ok {
		if syscallErr, ok := opErr.Err.(*os.SyscallError); ok {
			return syscallErr.Err == syscall.EADDRINUSE
		}
	}

	// Fallback to string check for wrapped errors
	return strings.Contains(err.Error(), "address already in use")
}
