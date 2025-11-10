package daemon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for NewDaemonConfig:
// - Valid config creation succeeds
// - Empty name returns error
// - Empty socket path returns error
// - Empty start command returns error
// - Zero/negative timeout returns error

func TestNewDaemonConfig_Success(t *testing.T) {
	t.Parallel()

	cfg, err := NewDaemonConfig(
		"test-daemon",
		"/tmp/test.sock",
		[]string{"test", "daemon", "start"},
		30*time.Second,
	)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-daemon", cfg.Name)
	assert.Equal(t, "/tmp/test.sock", cfg.SocketPath)
	assert.Equal(t, []string{"test", "daemon", "start"}, cfg.StartCommand)
	assert.Equal(t, 30*time.Second, cfg.StartupTimeout)
}

func TestNewDaemonConfig_EmptyName(t *testing.T) {
	t.Parallel()

	cfg, err := NewDaemonConfig(
		"",
		"/tmp/test.sock",
		[]string{"test", "daemon", "start"},
		30*time.Second,
	)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "daemon name is required")
}

func TestNewDaemonConfig_EmptySocketPath(t *testing.T) {
	t.Parallel()

	cfg, err := NewDaemonConfig(
		"test-daemon",
		"",
		[]string{"test", "daemon", "start"},
		30*time.Second,
	)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "socket path is required")
}

func TestNewDaemonConfig_EmptyStartCommand(t *testing.T) {
	t.Parallel()

	cfg, err := NewDaemonConfig(
		"test-daemon",
		"/tmp/test.sock",
		[]string{},
		30*time.Second,
	)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "start command is required")
}

func TestNewDaemonConfig_NilStartCommand(t *testing.T) {
	t.Parallel()

	cfg, err := NewDaemonConfig(
		"test-daemon",
		"/tmp/test.sock",
		nil,
		30*time.Second,
	)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "start command is required")
}

func TestNewDaemonConfig_ZeroTimeout(t *testing.T) {
	t.Parallel()

	cfg, err := NewDaemonConfig(
		"test-daemon",
		"/tmp/test.sock",
		[]string{"test", "daemon", "start"},
		0,
	)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "startup timeout must be positive")
}

func TestNewDaemonConfig_NegativeTimeout(t *testing.T) {
	t.Parallel()

	cfg, err := NewDaemonConfig(
		"test-daemon",
		"/tmp/test.sock",
		[]string{"test", "daemon", "start"},
		-5*time.Second,
	)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "startup timeout must be positive")
}
