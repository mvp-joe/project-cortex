package daemon

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for EnsureDaemon:
// - DaemonConfig can be created with constructor
// - Fast path: Returns immediately if socket is dialable
// - Slow path: Spawns daemon if socket not dialable
// - Concurrent calls: Multiple clients can spawn, daemon-side singleton prevents duplicates
// - Health wait: Waits for socket to become dialable after spawn
// - Timeout: Returns error if socket doesn't become dialable in time
// - Invalid command: Returns error for invalid start command
// - Context cancellation: Respects context deadlines

func TestEnsureDaemon_FastPath_AlreadyHealthy(t *testing.T) {
	t.Parallel()

	// Test: If socket is already dialable, return immediately without spawning
	ctx := context.Background()
	socketPath := "/tmp/cortex-test-fp1.sock"
	defer os.Remove(socketPath)

	// Create listener (simulates daemon already running)
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	cfg, err := NewDaemonConfig(
		"test-daemon",
		socketPath,
		[]string{"echo", "test"},
		1*time.Second,
	)
	require.NoError(t, err)

	err = EnsureDaemon(ctx, cfg)

	require.NoError(t, err)
}

func TestEnsureDaemon_FastPath_MultipleHealthyChecks(t *testing.T) {
	t.Parallel()

	// Test: Multiple concurrent calls all take fast path when socket is dialable
	ctx := context.Background()
	socketPath := "/tmp/cortex-test-fp2.sock"
	defer os.Remove(socketPath)

	// Create listener (simulates daemon already running)
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	cfg, err := NewDaemonConfig(
		"test-daemon-multi",
		socketPath,
		[]string{"echo", "test"},
		1*time.Second,
	)
	require.NoError(t, err)

	// Run 10 concurrent EnsureDaemon calls
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- EnsureDaemon(ctx, cfg)
		}()
	}

	// All should succeed quickly
	for i := 0; i < 10; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}

func TestEnsureDaemon_SlowPath_SpawnsCommand(t *testing.T) {
	t.Parallel()

	// Test: Spawns valid command when socket is not dialable
	ctx := context.Background()

	// Create a marker file that the command will touch
	markerFile := "/tmp/cortex-test-sp-marker"
	socketPath := "/tmp/cortex-test-sp.sock"
	defer os.Remove(markerFile)
	defer os.Remove(socketPath)

	// Start a listener in background after delay (simulates daemon startup)
	go func() {
		time.Sleep(200 * time.Millisecond)
		listener, _ := net.Listen("unix", socketPath)
		if listener != nil {
			defer listener.Close()
			time.Sleep(2 * time.Second) // Keep listening
		}
	}()

	cfg, err := NewDaemonConfig(
		"test-spawn",
		socketPath,
		[]string{"touch", markerFile},
		2*time.Second,
	)
	require.NoError(t, err)

	err = EnsureDaemon(ctx, cfg)

	require.NoError(t, err)

	// Verify command was executed
	_, statErr := os.Stat(markerFile)
	assert.NoError(t, statErr, "Command should have created marker file")
}

func TestEnsureDaemon_InvalidCommand(t *testing.T) {
	t.Parallel()

	// Test: Invalid start command returns error
	ctx := context.Background()

	cfg, err := NewDaemonConfig(
		"test-invalid",
		"/tmp/cortex-test-inv.sock",
		[]string{"/nonexistent/binary/that/does/not/exist"},
		100*time.Millisecond,
	)
	require.NoError(t, err)

	err = EnsureDaemon(ctx, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start daemon")
}

func TestEnsureDaemon_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Test: Context cancellation should be respected during waitForHealthy
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	socketPath := "/tmp/cortex-test-ctx.sock"
	defer os.Remove(socketPath)

	cfg, err := NewDaemonConfig(
		"test-cancel",
		socketPath,
		[]string{"sleep", "10"}, // Long-running command
		100*time.Millisecond,    // Longer than context timeout
	)
	require.NoError(t, err)

	err = EnsureDaemon(ctx, cfg)

	// Should fail due to timeout
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start within")
}

func TestEnsureDaemon_HealthCheckTimeout(t *testing.T) {
	t.Parallel()

	// Test: Returns error if socket doesn't become dialable within timeout
	ctx := context.Background()

	cfg, err := NewDaemonConfig(
		"test-timeout",
		"/tmp/cortex-test-to.sock",
		[]string{"sleep", "0.1"}, // Valid command that completes but doesn't create socket
		200*time.Millisecond,     // Short timeout
	)
	require.NoError(t, err)

	err = EnsureDaemon(ctx, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start within")
}

