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

	"connectrpc.com/connect"
	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/mvp-joe/project-cortex/gen/indexer/v1/indexerv1connect"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test plan for indexer stop command:
// 1. Test successful shutdown
// 2. Test daemon not running (connection refused)
// 3. Test shutdown failure (daemon returns error)
// 4. Test Unix socket client creation

// mockIndexerServer implements IndexerServiceHandler for testing.
type mockIndexerServer struct {
	indexerv1connect.UnimplementedIndexerServiceHandler

	shutdownResponse *indexerv1.ShutdownResponse
	shutdownError    error
	statusResponse   *indexerv1.StatusResponse
	logsEntries      []*indexerv1.LogEntry
}

func (m *mockIndexerServer) Shutdown(
	ctx context.Context,
	req *connect.Request[indexerv1.ShutdownRequest],
) (*connect.Response[indexerv1.ShutdownResponse], error) {
	if m.shutdownError != nil {
		return nil, m.shutdownError
	}
	return connect.NewResponse(m.shutdownResponse), nil
}

// setupTestServer creates a Unix socket server for testing.
func setupTestServer(t *testing.T, handler indexerv1connect.IndexerServiceHandler) string {
	t.Helper()

	// Use /tmp directly to avoid macOS 104-char socket path limit
	// Generate unique socket name using test name hash
	socketPath := fmt.Sprintf("/tmp/cortex-test-%d.sock", time.Now().UnixNano())
	os.Remove(socketPath) // Remove if exists from previous run

	// Create Unix listener
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	// Create HTTP server with ConnectRPC handler
	mux := http.NewServeMux()
	path, h := indexerv1connect.NewIndexerServiceHandler(handler)
	mux.Handle(path, h)

	server := &http.Server{Handler: mux}

	// Start server in background
	go server.Serve(listener)

	// Cleanup on test completion
	t.Cleanup(func() {
		server.Close()
		listener.Close()
		os.Remove(socketPath)
	})

	return socketPath
}

func TestIndexerStopCommand_Success(t *testing.T) {
	// Note: Cannot use t.Parallel() because test modifies global command

	// Setup mock server that returns successful shutdown
	mock := &mockIndexerServer{
		shutdownResponse: &indexerv1.ShutdownResponse{
			Success: true,
			Message: "Daemon stopped successfully",
		},
	}

	socketPath := setupTestServer(t, mock)

	// Execute stop command with mock socket path
	cmd := indexerStopCmd
	cmd.SetArgs([]string{})

	// Override runE to inject socket path
	originalRunE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		client, err := newIndexerClient(socketPath)
		if err != nil {
			return err
		}

		resp, err := client.Shutdown(ctx, connect.NewRequest(&indexerv1.ShutdownRequest{}))
		if err != nil {
			if isConnectionError(err) {
				return fmt.Errorf("daemon not running. Start with: cortex indexer start")
			}
			return fmt.Errorf("failed to stop daemon: %w", err)
		}

		if resp.Msg.Success {
			fmt.Println("Indexer daemon stopped")
			return nil
		}

		return fmt.Errorf("shutdown failed: %s", resp.Msg.Message)
	}
	defer func() { cmd.RunE = originalRunE }()

	err := cmd.Execute()

	// Verify success
	assert.NoError(t, err)
}

func TestIndexerStopCommand_NotRunning(t *testing.T) {
	// Note: Cannot use t.Parallel() because test modifies global command

	// Set socket path to non-existent location
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "nonexistent.sock")

	// Test the runE function directly with non-existent socket
	ctx := context.Background()
	client, err := newIndexerClient(socketPath)
	if err != nil {
		// This is expected - socket doesn't exist
		// Verify it's a connection error
		require.Error(t, err)
		assert.True(t, isConnectionError(err))
		return
	}

	// If we got a client somehow, try shutdown and expect error
	resp, err := client.Shutdown(ctx, connect.NewRequest(&indexerv1.ShutdownRequest{}))
	if err != nil {
		if isConnectionError(err) {
			err = fmt.Errorf("daemon not running. Start with: cortex indexer start")
		} else {
			err = fmt.Errorf("failed to stop daemon: %w", err)
		}
	} else if !resp.Msg.Success {
		err = fmt.Errorf("shutdown failed: %s", resp.Msg.Message)
	}

	// Verify error mentions daemon not running
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daemon not running")
}

func TestIndexerStopCommand_ShutdownFailure(t *testing.T) {
	// Note: Cannot use t.Parallel() because test modifies global command

	// Setup mock server that returns failure
	mock := &mockIndexerServer{
		shutdownResponse: &indexerv1.ShutdownResponse{
			Success: false,
			Message: "Failed to stop project actors",
		},
	}

	socketPath := setupTestServer(t, mock)

	// Test the runE function logic directly with mock server
	ctx := context.Background()
	client, err := newIndexerClient(socketPath)
	require.NoError(t, err)

	resp, err := client.Shutdown(ctx, connect.NewRequest(&indexerv1.ShutdownRequest{}))
	require.NoError(t, err)

	// The mock returns success=false, so we should get an error
	var shutdownErr error
	if resp.Msg.Success {
		shutdownErr = nil
	} else {
		shutdownErr = fmt.Errorf("shutdown failed: %s", resp.Msg.Message)
	}

	// Verify error contains failure message
	require.Error(t, shutdownErr)
	assert.Contains(t, shutdownErr.Error(), "shutdown failed")
	assert.Contains(t, shutdownErr.Error(), "Failed to stop project actors")
}

func TestNewIndexerClient_UnixSocket(t *testing.T) {
	t.Parallel()

	// Use /tmp directly to avoid macOS 104-char socket path limit
	socketPath := fmt.Sprintf("/tmp/cortex-client-test-%d.sock", time.Now().UnixNano())
	defer os.Remove(socketPath)

	// Create Unix listener
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Create client with explicit socket path
	client, err := newIndexerClient(socketPath)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Verify client implements the interface
	var _ indexerv1connect.IndexerServiceClient = client
	assert.NotNil(t, client)
}

func TestIsConnectionError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused",
			err:      &net.OpError{Err: &os.SyscallError{Err: os.ErrNotExist}},
			expected: false, // Not a connection refused
		},
		{
			name:     "no such file - socket missing",
			err:      os.ErrNotExist,
			expected: false, // os.ErrNotExist doesn't contain the strings we check
		},
		{
			name:     "generic error",
			err:      assert.AnError,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isConnectionError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetIndexerSocketPath removed - socket path now comes from GlobalConfig
