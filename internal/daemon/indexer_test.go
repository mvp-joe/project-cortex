package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for indexer daemon helpers:
// - NewIndexerDaemonConfig creates correct DaemonConfig
// - NewIndexerDaemonConfig uses current executable path for start command
// - NewIndexerDaemonConfig accepts custom socket path

func TestNewIndexerDaemonConfig_ReturnsCorrectConfig(t *testing.T) {
	t.Parallel()

	// Setup: Use temp directory for socket
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "indexer.sock")

	// Test: Should create DaemonConfig with correct fields
	config, err := NewIndexerDaemonConfig(socketPath)
	require.NoError(t, err)

	// Verify name
	assert.Equal(t, "indexer", config.Name)

	// Verify socket path is set
	assert.Equal(t, socketPath, config.SocketPath)
	assert.Contains(t, config.SocketPath, "indexer.sock")

	// Verify start command
	require.Len(t, config.StartCommand, 3)
	assert.Equal(t, "indexer", config.StartCommand[1])
	assert.Equal(t, "start", config.StartCommand[2])

	// Verify executable path is absolute
	assert.True(t, filepath.IsAbs(config.StartCommand[0]))

	// Verify startup timeout is set
	assert.NotZero(t, config.StartupTimeout)
}

func TestNewIndexerDaemonConfig_UsesCurrentExecutable(t *testing.T) {
	t.Parallel()

	// Setup: Use temp directory for socket
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "indexer.sock")

	// Test: Start command should use current executable path, not "cortex" from PATH
	config, err := NewIndexerDaemonConfig(socketPath)
	require.NoError(t, err)

	execPath, err := os.Executable()
	require.NoError(t, err)

	// First element of StartCommand should be the current executable
	assert.Equal(t, execPath, config.StartCommand[0])
}

func TestNewIndexerDaemonConfig_WithCustomSocketPath(t *testing.T) {
	t.Parallel()

	// Setup: Use temp directory for socket
	tempDir := t.TempDir()
	customPath := filepath.Join(tempDir, "custom-indexer.sock")

	// Test: Config should use provided socket path
	config, err := NewIndexerDaemonConfig(customPath)
	require.NoError(t, err)

	assert.Equal(t, customPath, config.SocketPath)
}
