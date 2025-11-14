package daemon

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for SingletonDaemon:
// - NewSingletonDaemon creates instance with correct fields
// - SingletonDaemon struct has expected fields (name, socketPath, lock)
// - BindSocket can be called (basic functionality check)
// - Release handles nil lock gracefully

func TestNewSingletonDaemon(t *testing.T) {
	t.Parallel()

	// Test: NewSingletonDaemon creates instance with correct name and socket path
	name := "test-daemon"
	socketPath := "/tmp/test.sock"

	daemon := NewSingletonDaemon(name, socketPath)

	require.NotNil(t, daemon)
	assert.Equal(t, name, daemon.name)
	assert.Equal(t, socketPath, daemon.socketPath)
	assert.Nil(t, daemon.lock) // lock not acquired yet
}

func TestSingletonDaemon_Structure(t *testing.T) {
	t.Parallel()

	// Test: SingletonDaemon has expected struct fields
	daemon := &SingletonDaemon{
		name:       "indexer",
		socketPath: "/tmp/indexer.sock",
		lock:       nil,
	}

	assert.Equal(t, "indexer", daemon.name)
	assert.Equal(t, "/tmp/indexer.sock", daemon.socketPath)
	assert.Nil(t, daemon.lock)
}

func TestSingletonDaemon_Release_NilLock(t *testing.T) {
	t.Parallel()

	// Test: Release returns nil when lock is nil
	daemon := NewSingletonDaemon("test", "/tmp/test.sock")

	err := daemon.Release()
	assert.NoError(t, err)
}

func TestSingletonDaemon_BindSocket_NotWon(t *testing.T) {
	t.Parallel()

	// Test: BindSocket can be called (will fail if socket exists, but tests method exists)
	// Note: This is a basic structural test. Integration tests will verify full lifecycle.

	// Use /tmp directly to avoid macOS path length restrictions for Unix sockets
	socketPath := "/tmp/test-daemon-" + t.Name() + ".sock"
	t.Cleanup(func() {
		// Clean up socket file
		_ = os.Remove(socketPath)
	})

	daemon := NewSingletonDaemon("test", socketPath)

	// First call should succeed
	listener, err := daemon.BindSocket()
	require.NoError(t, err)
	require.NotNil(t, listener)

	// Clean up
	listener.Close()
}

func TestSingletonDaemon_EnforceSingleton_StaleSocket(t *testing.T) {
	t.Parallel()

	// Test: EnforceSingleton detects and cleans up stale socket files
	socketPath := "/tmp/test-stale-" + t.Name() + ".sock"
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
		_ = os.Remove(getLockPath("test-stale"))
	})

	// Create a stale socket file (no process listening)
	staleFile, err := os.Create(socketPath)
	require.NoError(t, err)
	staleFile.Close()

	// Verify stale socket exists
	_, err = os.Stat(socketPath)
	require.NoError(t, err)

	// First daemon should detect stale socket and clean it up
	daemon1 := NewSingletonDaemon("test-stale", socketPath)
	won, err := daemon1.EnforceSingleton()
	require.NoError(t, err)
	assert.True(t, won, "first daemon should win and clean up stale socket")
	defer daemon1.Release()

	// Bind socket to complete the lifecycle
	listener, err := daemon1.BindSocket()
	require.NoError(t, err)
	defer listener.Close()

	// Second daemon should detect the LIVE socket and back off
	daemon2 := NewSingletonDaemon("test-stale", socketPath)
	won, err = daemon2.EnforceSingleton()
	require.NoError(t, err)
	assert.False(t, won, "second daemon should detect live socket and back off")
}
