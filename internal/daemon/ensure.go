// Package daemon provides reusable daemon lifecycle management components
// for Project Cortex daemons (indexer daemon, ONNX embedding server).
//
// # Core Components
//
// 1. Client-Side Auto-Start (EnsureDaemon)
//   - Ensures daemon is running before client operations
//   - NO client-side locking (multiple spawns allowed)
//   - Daemon-side singleton enforcement prevents duplicates
//   - Safe to call concurrently from multiple clients
//
// 2. Daemon-Side Singleton Enforcement (SingletonDaemon)
//   - Prevents multiple daemon processes using socket bind + file lock
//   - Losing daemons exit gracefully (code 0)
//   - File lock prevents race conditions during startup
//
// 3. Connection Error Detection (IsConnectionError)
//   - Identifies daemon connection failures for resurrection pattern
//   - Used by providers to auto-restart crashed/idle daemons
//
// # Usage Pattern: Client Auto-Start
//
// Clients use EnsureDaemon to transparently start daemons on-demand:
//
//	func (c *IndexerClient) Index(ctx context.Context, path string) error {
//	    // Auto-start daemon if needed
//	    cfg, err := daemon.NewDaemonConfig(
//	        "indexer",
//	        "~/.cortex/indexer.sock",
//	        []string{"cortex", "indexer", "start"},
//	        30 * time.Second,
//	    )
//	    if err != nil {
//	        return fmt.Errorf("invalid daemon config: %w", err)
//	    }
//
//	    if err := daemon.EnsureDaemon(ctx, cfg); err != nil {
//	        return fmt.Errorf("failed to ensure daemon: %w", err)
//	    }
//
//	    // Connect and use daemon...
//	    return c.index(ctx, path)
//	}
//
// # Usage Pattern: Daemon Singleton Enforcement
//
// Daemons use SingletonDaemon to prevent duplicate processes:
//
//	func main() {
//	    singleton := daemon.NewSingletonDaemon("indexer", "~/.cortex/indexer.sock")
//
//	    won, err := singleton.EnforceSingleton()
//	    if err != nil {
//	        log.Fatalf("Singleton check failed: %v", err)
//	    }
//
//	    if !won {
//	        // Another daemon already running
//	        fmt.Println("Indexer daemon already running")
//	        os.Exit(0)  // Exit gracefully
//	    }
//
//	    defer singleton.Release()  // Release lock on shutdown
//
//	    // Bind socket and start serving...
//	    listener, _ := singleton.BindSocket()
//	    http.Serve(listener, handler)
//	}
//
// # Usage Pattern: Resurrection (Provider Auto-Restart)
//
// Providers use IsConnectionError to detect and resurrect crashed daemons:
//
//	func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
//	    // Try RPC call
//	    resp, err := p.client.Embed(ctx, req)
//
//	    // Resurrect on connection failure
//	    if daemon.IsConnectionError(err) {
//	        if err := daemon.EnsureDaemon(ctx, p.ensureConfig); err != nil {
//	            return nil, fmt.Errorf("resurrection failed: %w", err)
//	        }
//	        // Retry once
//	        resp, err = p.client.Embed(ctx, req)
//	    }
//
//	    return resp, err
//	}
//
// # Concurrent Client Spawns
//
// Multiple clients can call EnsureDaemon simultaneously. All spawn daemons,
// but daemon-side singleton enforcement ensures only one survives:
//
//	Scenario: 10 clients call EnsureDaemon() simultaneously, daemon not running
//
//	Flow:
//	  1. All 10 clients see socket dial fail (daemon not running)
//	  2. All 10 clients spawn "cortex indexer start" (no client locks)
//	  3. 10 daemon processes start simultaneously
//	  4. All 10 daemons call EnforceSingleton()
//	  5. ONE daemon wins (socket bind + file lock succeed)
//	  6. 9 daemons lose (socket bind fails EADDRINUSE) → exit code 0
//	  7. All 10 clients wait for socket to be dialable → all succeed (connect to winner)
//
//	Result: Only one daemon survives, all clients succeed
//
// # Key Design Principles
//
// 1. NO CLIENT-SIDE LOCKING
//   - Clients never acquire locks
//   - Multiple daemon spawns are expected and OK
//   - Simplifies client code, prevents deadlocks
//
// 2. DAEMON-SIDE SINGLETON ENFORCEMENT
//   - Daemons use socket bind (fast, reliable detection)
//   - File lock prevents race conditions during startup
//   - Losing daemons exit gracefully (not an error)
//
// 3. GRACEFUL DEGRADATION
//   - Missing config files → use defaults
//   - Connection failures → auto-resurrect
//   - Stale sockets → auto-cleanup
//
// See also:
//   - specs/2025-11-09_daemon-foundation.md for architecture details
//   - specs/2025-11-05_indexer-daemon.md for indexer-specific usage
//   - specs/2025-11-07_onnx-embedding-server.md for embed server usage
package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// EnsureDaemon ensures daemon is running, starting it if needed.
// Safe to call concurrently from multiple clients.
// If multiple clients spawn multiple daemons, daemon-side singleton
// enforcement ensures only one daemon wins. Losing daemons exit gracefully.
// Returns nil if daemon is healthy (already running or successfully started).
//
// Flow:
//  1. Fast path: Check if socket is dialable → return immediately
//  2. Spawn daemon in detached process group
//  3. Wait for socket to become dialable (with timeout)
//
// Note: Multiple clients may spawn multiple daemon processes simultaneously.
// Daemon-side singleton enforcement (socket bind + file lock) ensures only
// one daemon wins. Losing daemons detect they lost and exit gracefully (code 0).
//
// Example usage:
//
//	cfg, _ := daemon.NewDaemonConfig(
//	    "indexer",
//	    "/tmp/indexer.sock",
//	    []string{"cortex", "indexer", "start"},
//	    30 * time.Second,
//	)
//	err := daemon.EnsureDaemon(ctx, cfg)
func EnsureDaemon(ctx context.Context, cfg *DaemonConfig) error {
	// 1. Fast path: check if socket is dialable
	if canDial(cfg.SocketPath) {
		return nil
	}

	// 2. Spawn daemon (detached)
	// Multiple clients may spawn multiple daemons - that's OK
	// Daemon-side singleton enforcement ensures only one wins
	cmd := exec.Command(cfg.StartCommand[0], cfg.StartCommand[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Detach from parent process group
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// 3. Wait for socket to become dialable
	// If multiple daemons spawned, only one passes EnforceSingleton
	// Others exit gracefully, this client just waits for the winner
	return waitForHealthy(ctx, cfg)
}
