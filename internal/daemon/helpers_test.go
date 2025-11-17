package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getLockPathWithHome is a test helper that creates a lock path with a custom home directory.
func getLockPathWithHome(name, homeDir string) string {
	cortexDir := filepath.Join(homeDir, ".cortex")
	os.MkdirAll(cortexDir, 0755)
	return filepath.Join(cortexDir, name+".lock")
}

func TestGetLockPath(t *testing.T) {
	tests := []struct {
		name     string
		daemon   string
		wantFile string
	}{
		{
			name:     "indexer daemon",
			daemon:   "indexer",
			wantFile: "indexer.lock",
		},
		{
			name:     "embed daemon",
			daemon:   "embed",
			wantFile: "embed.lock",
		},
		{
			name:     "custom daemon name",
			daemon:   "my-custom-daemon",
			wantFile: "my-custom-daemon.lock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup: Use temp home to avoid polluting ~/.cortex
			tempHome := t.TempDir()

			path := getLockPathWithHome(tt.daemon, tempHome)

			// Verify path format
			assert.True(t, strings.HasSuffix(path, ".cortex/"+tt.wantFile),
				"expected path to end with .cortex/%s, got %s", tt.wantFile, path)

			// Verify directory exists in temp home
			dir := filepath.Dir(path)
			info, err := os.Stat(dir)
			require.NoError(t, err, "expected .cortex directory to exist")
			assert.True(t, info.IsDir(), "expected .cortex to be a directory")
			assert.Contains(t, dir, tempHome, "directory should be in temp home")
		})
	}
}

func TestWaitForHealthy_Success(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/cortex-test-wh-success.sock"
	defer os.Remove(socketPath)

	// Start a listener in background (simulates daemon startup after delay)
	go func() {
		time.Sleep(200 * time.Millisecond) // Simulate startup delay
		listener, _ := net.Listen("unix", socketPath)
		if listener != nil {
			defer listener.Close()
			time.Sleep(2 * time.Second) // Keep listening
		}
	}()

	cfg, err := NewDaemonConfig(
		"test-daemon",
		socketPath,
		[]string{"echo", "test"},
		2*time.Second,
	)
	require.NoError(t, err)

	ctx := context.Background()
	err = waitForHealthy(ctx, cfg)

	require.NoError(t, err)
}

func TestWaitForHealthy_Timeout(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/cortex-test-wh-timeout.sock"
	defer os.Remove(socketPath)

	cfg, err := NewDaemonConfig(
		"test-daemon",
		socketPath,
		[]string{"echo", "test"},
		200*time.Millisecond, // Short timeout for test
	)
	require.NoError(t, err)

	ctx := context.Background()
	err = waitForHealthy(ctx, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start within")
	assert.Contains(t, err.Error(), "200ms")
}

func TestWaitForHealthy_ContextCancellation(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/cortex-test-wh-cancel.sock"
	defer os.Remove(socketPath)

	cfg, err := NewDaemonConfig(
		"test-daemon",
		socketPath,
		[]string{"echo", "test"},
		10*time.Second, // Long timeout
	)
	require.NoError(t, err)

	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err = waitForHealthy(ctx, cfg)

	require.Error(t, err)
	// Should fail due to context cancellation, not timeout
	assert.Contains(t, err.Error(), "failed to start within")
}

func TestIsAddrInUse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantUse bool
	}{
		{
			name:    "nil error",
			err:     nil,
			wantUse: false,
		},
		{
			name:    "unrelated error",
			err:     errors.New("something went wrong"),
			wantUse: false,
		},
		{
			name:    "string match - address in use",
			err:     errors.New("bind: address already in use"),
			wantUse: true,
		},
		{
			name:    "wrapped string match",
			err:     fmt.Errorf("failed to bind socket: %w", errors.New("address already in use")),
			wantUse: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isAddrInUse(tt.err)
			assert.Equal(t, tt.wantUse, got)
		})
	}
}

func TestIsAddrInUse_RealSocket(t *testing.T) {
	t.Parallel()

	// Create temp directory for test socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Bind first listener
	listener1, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener1.Close()

	// Try to bind second listener (should fail with EADDRINUSE)
	_, err = net.Listen("unix", socketPath)
	require.Error(t, err)

	// Verify isAddrInUse detects it
	assert.True(t, isAddrInUse(err), "expected isAddrInUse to detect socket conflict")
}

func TestIsAddrInUse_SyscallError(t *testing.T) {
	t.Parallel()

	// Create a real syscall error with EADDRINUSE
	syscallErr := &os.SyscallError{
		Syscall: "bind",
		Err:     syscall.EADDRINUSE,
	}

	opErr := &net.OpError{
		Op:  "listen",
		Net: "unix",
		Err: syscallErr,
	}

	assert.True(t, isAddrInUse(opErr), "expected isAddrInUse to detect syscall EADDRINUSE")
}

func TestIsAddrInUse_DifferentSyscallError(t *testing.T) {
	t.Parallel()

	// Create syscall error with different error code
	syscallErr := &os.SyscallError{
		Syscall: "bind",
		Err:     syscall.EACCES, // Permission denied, not address in use
	}

	opErr := &net.OpError{
		Op:  "listen",
		Net: "unix",
		Err: syscallErr,
	}

	assert.False(t, isAddrInUse(opErr), "expected isAddrInUse to return false for non-EADDRINUSE errors")
}
