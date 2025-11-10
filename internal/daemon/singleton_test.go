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
