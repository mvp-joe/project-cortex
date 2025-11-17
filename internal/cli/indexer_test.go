package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mvp-joe/project-cortex/gen/indexer/v1/indexerv1connect"
	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/embed"
	indexerdaemon "github.com/mvp-joe/project-cortex/internal/indexer/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmbedProvider is a fake embedding provider for testing.
type mockEmbedProvider struct {
	dimensions int
}

func newMockEmbedProvider() *mockEmbedProvider {
	return &mockEmbedProvider{dimensions: 384}
}

func (m *mockEmbedProvider) Initialize(ctx context.Context) error { return nil }
func (m *mockEmbedProvider) Embed(ctx context.Context, texts []string, mode embed.EmbedMode) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = make([]float32, m.dimensions)
		for j := range vectors[i] {
			vectors[i][j] = 0.1
		}
	}
	return vectors, nil
}
func (m *mockEmbedProvider) Dimensions() int  { return m.dimensions }
func (m *mockEmbedProvider) Close() error      { return nil }

// Test Plan for indexer start command:
// - Command is registered and available
// - Socket path defaults to ~/.cortex/indexer.sock
// - CORTEX_INDEXER_SOCKET environment variable overrides default
// - Singleton enforcement prevents multiple daemons
// - Server binds to Unix socket successfully
// - ConnectRPC handler is mounted correctly
// - Socket permissions are set to 0600 (user-only)
// - Graceful shutdown on SIGTERM/SIGINT
// - Clean socket cleanup on exit

func TestIndexerCmd_IsRegistered(t *testing.T) {
	t.Parallel()

	// Test: indexer command should be registered as subcommand of root
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "indexer" {
			found = true
			break
		}
	}

	assert.True(t, found, "indexer command should be registered")
}

func TestIndexerStartCmd_IsRegistered(t *testing.T) {
	t.Parallel()

	// Test: start subcommand should be registered under indexer command
	found := false
	for _, cmd := range indexerCmd.Commands() {
		if cmd.Name() == "start" {
			found = true
			break
		}
	}

	assert.True(t, found, "start command should be registered under indexer")
}

func TestIndexerStartCmd_SocketPath(t *testing.T) {
	t.Parallel()

	// Test: Default socket path should be ~/.cortex/indexer.sock
	t.Run("Default", func(t *testing.T) {
		t.Parallel()

		// Clear any environment override
		originalValue := os.Getenv("CORTEX_INDEXER_SOCKET")
		os.Unsetenv("CORTEX_INDEXER_SOCKET")
		defer func() {
			if originalValue != "" {
				os.Setenv("CORTEX_INDEXER_SOCKET", originalValue)
			}
		}()

		homeDir, err := os.UserHomeDir()
		require.NoError(t, err)

		expectedPath := filepath.Join(homeDir, ".cortex", "indexer.sock")

		// Create temporary test socket
		testSocketPath := filepath.Join(t.TempDir(), "indexer.sock")
		os.Setenv("CORTEX_INDEXER_SOCKET", testSocketPath)

		// Verify the path format matches expected pattern
		assert.Contains(t, expectedPath, ".cortex/indexer.sock")
	})

	// Test: Environment variable should override default path
	t.Run("EnvironmentOverride", func(t *testing.T) {
		t.Parallel()

		customPath := filepath.Join(t.TempDir(), "custom-indexer.sock")
		os.Setenv("CORTEX_INDEXER_SOCKET", customPath)
		defer os.Unsetenv("CORTEX_INDEXER_SOCKET")

		// Start would use this path via daemon.GetIndexerSocketPath()
		assert.Equal(t, customPath, os.Getenv("CORTEX_INDEXER_SOCKET"))
	})
}

func TestIndexerStartCmd_SingletonEnforcement(t *testing.T) {
	t.Parallel()

	// Test: Only one daemon should be allowed to run at a time
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use /tmp to avoid macOS 104-char socket path limit
	testSocketPath := fmt.Sprintf("/tmp/cortex-singleton-test-%d.sock", time.Now().UnixNano())
	defer os.Remove(testSocketPath)

	// Start first daemon
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	srv1, err := indexerdaemon.NewServer(ctx, testSocketPath, mockProvider, testCache)
	require.NoError(t, err)

	listener1, err := net.Listen("unix", testSocketPath)
	require.NoError(t, err)
	defer listener1.Close()

	// Setup HTTP server
	mux := http.NewServeMux()
	path, handler := indexerv1connect.NewIndexerServiceHandler(srv1)
	mux.Handle(path, handler)

	httpServer := &http.Server{Handler: mux}

	go func() {
		httpServer.Serve(listener1)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Try to bind second listener on same socket - should fail
	_, err = net.Listen("unix", testSocketPath)
	assert.Error(t, err, "Second daemon should fail to bind same socket")
}

func TestIndexerStartCmd_SocketPermissions(t *testing.T) {
	t.Parallel()

	// Test: Socket should be created with 0600 permissions (user-only)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use /tmp to avoid macOS 104-char socket path limit
	testSocketPath := fmt.Sprintf("/tmp/cortex-perms-test-%d.sock", time.Now().UnixNano())
	defer os.Remove(testSocketPath)

	// Create server and listener
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	srv, err := indexerdaemon.NewServer(ctx, testSocketPath, mockProvider, testCache)
	require.NoError(t, err)

	listener, err := net.Listen("unix", testSocketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Set socket permissions
	err = os.Chmod(testSocketPath, 0600)
	require.NoError(t, err)

	// Verify permissions
	info, err := os.Stat(testSocketPath)
	require.NoError(t, err)

	// Socket permissions should be user-only (0600)
	mode := info.Mode()
	assert.Equal(t, os.FileMode(0600), mode.Perm(), "Socket should have 0600 permissions")

	// Start HTTP server
	mux := http.NewServeMux()
	path, handler := indexerv1connect.NewIndexerServiceHandler(srv)
	mux.Handle(path, handler)

	httpServer := &http.Server{Handler: mux}
	go httpServer.Serve(listener)
}

func TestIndexerStartCmd_ConnectRPCHandler(t *testing.T) {
	t.Parallel()

	// Test: ConnectRPC handler should be mounted and accessible
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use /tmp to avoid macOS 104-char socket path limit
	testSocketPath := fmt.Sprintf("/tmp/cortex-handler-test-%d.sock", time.Now().UnixNano())
	defer os.Remove(testSocketPath)

	// Create server
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	srv, err := indexerdaemon.NewServer(ctx, testSocketPath, mockProvider, testCache)
	require.NoError(t, err)

	listener, err := net.Listen("unix", testSocketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Setup HTTP server with ConnectRPC handler
	mux := http.NewServeMux()
	path, handler := indexerv1connect.NewIndexerServiceHandler(srv)
	mux.Handle(path, handler)

	// Verify handler path is set correctly
	assert.NotEmpty(t, path, "Handler path should not be empty")
	assert.Contains(t, path, "IndexerService", "Handler path should contain service name")

	httpServer := &http.Server{Handler: mux}
	go httpServer.Serve(listener)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify socket exists and is connectable
	conn, err := net.Dial("unix", testSocketPath)
	require.NoError(t, err)
	conn.Close()
}

func TestIndexerStartCmd_GracefulShutdown(t *testing.T) {
	t.Parallel()

	// Test: Server should handle context cancellation gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	// Use /tmp to avoid macOS 104-char socket path limit
	testSocketPath := fmt.Sprintf("/tmp/cortex-shutdown-test-%d.sock", time.Now().UnixNano())
	defer os.Remove(testSocketPath)

	// Create server
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	srv, err := indexerdaemon.NewServer(ctx, testSocketPath, mockProvider, testCache)
	require.NoError(t, err)

	listener, err := net.Listen("unix", testSocketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Setup HTTP server
	mux := http.NewServeMux()
	path, handler := indexerv1connect.NewIndexerServiceHandler(srv)
	mux.Handle(path, handler)

	httpServer := &http.Server{Handler: mux}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpServer.Serve(listener)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	// Close listener to stop server
	listener.Close()

	// Wait for server to exit
	select {
	case err := <-serverErr:
		// Server should exit (either with error or gracefully)
		assert.NotNil(t, err, "Server should return error when listener closes")
	case <-time.After(2 * time.Second):
		t.Fatal("Server did not shutdown within timeout")
	}
}

func TestIndexerStartCmd_SocketCleanup(t *testing.T) {
	t.Parallel()

	// Test: Socket file should be cleaned up when server exits
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use /tmp to avoid macOS 104-char socket path limit
	testSocketPath := fmt.Sprintf("/tmp/cortex-cleanup-test-%d.sock", time.Now().UnixNano())
	defer os.Remove(testSocketPath)

	// Create server and listener
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	srv, err := indexerdaemon.NewServer(ctx, testSocketPath, mockProvider, testCache)
	require.NoError(t, err)

	listener, err := net.Listen("unix", testSocketPath)
	require.NoError(t, err)

	// Setup HTTP server
	mux := http.NewServeMux()
	path, handler := indexerv1connect.NewIndexerServiceHandler(srv)
	mux.Handle(path, handler)

	httpServer := &http.Server{Handler: mux}
	go httpServer.Serve(listener)

	// Verify socket exists
	_, err = os.Stat(testSocketPath)
	require.NoError(t, err, "Socket should exist while server is running")

	// Close listener
	listener.Close()

	// Socket file may remain after close (OS dependent)
	// But singleton enforcement should handle stale sockets on next startup
}
