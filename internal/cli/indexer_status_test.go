package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test plan for indexer status command:
// 1. Test formatted output with running daemon
// 2. Test JSON output with running daemon
// 3. Test daemon not running (formatted output)
// 4. Test daemon not running (JSON output)
// 5. Test no projects registered
// 6. Test multiple projects with various states
// 7. Test formatting helpers (duration, time since, phase, numbers)

func (m *mockIndexerServer) GetStatus(
	ctx context.Context,
	req *connect.Request[indexerv1.StatusRequest],
) (*connect.Response[indexerv1.StatusResponse], error) {
	if m.statusResponse != nil {
		return connect.NewResponse(m.statusResponse), nil
	}
	return nil, assert.AnError
}

func TestIndexerStatusCommand_FormattedOutput(t *testing.T) {
	// Note: Cannot use t.Parallel() because test manipulates os.Stdout

	// Setup mock server with sample status
	now := time.Now().Unix()
	mock := &mockIndexerServer{
		statusResponse: &indexerv1.StatusResponse{
			Daemon: &indexerv1.DaemonStatus{
				Pid:           12345,
				StartedAt:     now - 7200, // Started 2 hours ago
				UptimeSeconds: 7200,
				SocketPath:    "/tmp/test.sock",
			},
			Projects: []*indexerv1.ProjectStatus{
				{
					Path:          "/Users/joe/code/my-project",
					CacheKey:      "abc123-def456",
					CurrentBranch: "main",
					FilesIndexed:  1234,
					ChunksCount:   5678,
					RegisteredAt:  now - 3600,
					LastIndexedAt: now - 300, // 5 minutes ago
					IsIndexing:    false,
					CurrentPhase:  indexerv1.IndexProgress_PHASE_UNSPECIFIED,
				},
			},
		},
	}

	socketPath := setupTestServer(t, mock)

	// Create a new command instance to avoid state pollution
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show indexer daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
			if err != nil {
				if isConnectionError(err) {
					if statusJSON {
						output := map[string]interface{}{
							"running": false,
							"error":   "Daemon not running",
						}
						jsonBytes, _ := json.MarshalIndent(output, "", "  ")
						fmt.Println(string(jsonBytes))
						return nil
					}
					fmt.Println("Indexer daemon: Not running")
					return nil
				}
				return fmt.Errorf("failed to get status: %w", err)
			}

			if statusJSON {
				jsonBytes, err := json.MarshalIndent(resp.Msg, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(jsonBytes))
				return nil
			}

			return formatStatus(resp.Msg)
		},
	}
	cmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Read from pipe in goroutine
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()

	err := cmd.Execute()

	w.Close()
	<-done
	os.Stdout = oldStdout

	output := buf.String()

	require.NoError(t, err)

	// Verify daemon info
	assert.Contains(t, output, "Daemon Status:")
	assert.Contains(t, output, "PID:    12345")
	assert.Contains(t, output, "Uptime: 2h")
	assert.Contains(t, output, "Socket: /tmp/test.sock")

	// Verify project info
	assert.Contains(t, output, "Registered Projects (1):")
	assert.Contains(t, output, "/Users/joe/code/my-project")
	assert.Contains(t, output, "Branch:       main")
	assert.Contains(t, output, "Files:        1,234")
	assert.Contains(t, output, "Chunks:       5,678")
	assert.Contains(t, output, "Status:       watching")
}

func TestIndexerStatusCommand_JSONOutput(t *testing.T) {
	// Note: Cannot use t.Parallel() because test manipulates os.Stdout

	// Setup mock server
	now := time.Now().Unix()
	mock := &mockIndexerServer{
		statusResponse: &indexerv1.StatusResponse{
			Daemon: &indexerv1.DaemonStatus{
				Pid:           12345,
				StartedAt:     now,
				UptimeSeconds: 100,
				SocketPath:    "/tmp/test.sock",
			},
			Projects: []*indexerv1.ProjectStatus{},
		},
	}

	socketPath := setupTestServer(t, mock)

	// Create fresh command with local json flag
	var localJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show indexer daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
			if err != nil {
				if isConnectionError(err) {
					if localJSON {
						output := map[string]interface{}{
							"running": false,
							"error":   "Daemon not running",
						}
						jsonBytes, _ := json.MarshalIndent(output, "", "  ")
						fmt.Println(string(jsonBytes))
						return nil
					}
					fmt.Println("Indexer daemon: Not running")
					return nil
				}
				return fmt.Errorf("failed to get status: %w", err)
			}

			if localJSON {
				jsonBytes, err := json.MarshalIndent(resp.Msg, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(jsonBytes))
				return nil
			}

			return formatStatus(resp.Msg)
		},
	}
	cmd.Flags().BoolVar(&localJSON, "json", false, "Output as JSON")
	cmd.SetArgs([]string{"--json"})

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Read from pipe in goroutine
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()

	err := cmd.Execute()

	w.Close()
	<-done
	os.Stdout = oldStdout

	require.NoError(t, err)

	// Parse JSON output
	var result indexerv1.StatusResponse
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	// Verify JSON structure
	assert.Equal(t, int32(12345), result.Daemon.Pid)
	assert.Equal(t, int64(100), result.Daemon.UptimeSeconds)
	assert.Equal(t, "/tmp/test.sock", result.Daemon.SocketPath)
	assert.Empty(t, result.Projects)
}

func TestIndexerStatusCommand_NotRunning_Formatted(t *testing.T) {
	// Note: Cannot use t.Parallel() because test manipulates os.Stdout

	// Set socket path to non-existent location
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "nonexistent.sock")

	// Create fresh command
	var localJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show indexer daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
			if err != nil {
				if isConnectionError(err) {
					if localJSON {
						output := map[string]interface{}{
							"running": false,
							"error":   "Daemon not running",
						}
						jsonBytes, _ := json.MarshalIndent(output, "", "  ")
						fmt.Println(string(jsonBytes))
						return nil
					}
					fmt.Println("Indexer daemon: Not running")
					return nil
				}
				return fmt.Errorf("failed to get status: %w", err)
			}

			if localJSON {
				jsonBytes, err := json.MarshalIndent(resp.Msg, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(jsonBytes))
				return nil
			}

			return formatStatus(resp.Msg)
		},
	}
	cmd.Flags().BoolVar(&localJSON, "json", false, "Output as JSON")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Read from pipe in goroutine
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()

	err := cmd.Execute()

	w.Close()
	<-done
	os.Stdout = oldStdout

	output := buf.String()

	require.NoError(t, err)
	assert.Contains(t, output, "Not running")
}

func TestIndexerStatusCommand_NotRunning_JSON(t *testing.T) {
	// Note: Cannot use t.Parallel() because test manipulates os.Stdout

	// Set socket path to non-existent location
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "nonexistent.sock")

	// Create fresh command with JSON flag
	var localJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show indexer daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
			if err != nil {
				if isConnectionError(err) {
					if localJSON {
						output := map[string]interface{}{
							"running": false,
							"error":   "Daemon not running",
						}
						jsonBytes, _ := json.MarshalIndent(output, "", "  ")
						fmt.Println(string(jsonBytes))
						return nil
					}
					fmt.Println("Indexer daemon: Not running")
					return nil
				}
				return fmt.Errorf("failed to get status: %w", err)
			}

			if localJSON {
				jsonBytes, err := json.MarshalIndent(resp.Msg, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(jsonBytes))
				return nil
			}

			return formatStatus(resp.Msg)
		},
	}
	cmd.Flags().BoolVar(&localJSON, "json", false, "Output as JSON")
	cmd.SetArgs([]string{"--json"})

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Read from pipe in goroutine
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()

	err := cmd.Execute()

	w.Close()
	<-done
	os.Stdout = oldStdout

	require.NoError(t, err)

	// Parse JSON
	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Equal(t, false, result["running"])
	assert.Equal(t, "Daemon not running", result["error"])
}

func TestIndexerStatusCommand_NoProjects(t *testing.T) {
	// Note: Cannot use t.Parallel() because test manipulates os.Stdout

	// Setup mock server with no projects
	now := time.Now().Unix()
	mock := &mockIndexerServer{
		statusResponse: &indexerv1.StatusResponse{
			Daemon: &indexerv1.DaemonStatus{
				Pid:           12345,
				StartedAt:     now,
				UptimeSeconds: 100,
				SocketPath:    "/tmp/test.sock",
			},
			Projects: []*indexerv1.ProjectStatus{},
		},
	}

	socketPath := setupTestServer(t, mock)

	// Create fresh command
	var localJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show indexer daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
			if err != nil {
				if isConnectionError(err) {
					if localJSON {
						output := map[string]interface{}{
							"running": false,
							"error":   "Daemon not running",
						}
						jsonBytes, _ := json.MarshalIndent(output, "", "  ")
						fmt.Println(string(jsonBytes))
						return nil
					}
					fmt.Println("Indexer daemon: Not running")
					return nil
				}
				return fmt.Errorf("failed to get status: %w", err)
			}

			if localJSON {
				jsonBytes, err := json.MarshalIndent(resp.Msg, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(jsonBytes))
				return nil
			}

			return formatStatus(resp.Msg)
		},
	}
	cmd.Flags().BoolVar(&localJSON, "json", false, "Output as JSON")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Read from pipe in goroutine
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()

	err := cmd.Execute()

	w.Close()
	<-done
	os.Stdout = oldStdout

	output := buf.String()

	require.NoError(t, err)
	assert.Contains(t, output, "No projects registered")
}

func TestIndexerStatusCommand_IndexingProject(t *testing.T) {
	// Note: Cannot use t.Parallel() because test manipulates os.Stdout

	// Setup mock server with indexing project
	now := time.Now().Unix()
	mock := &mockIndexerServer{
		statusResponse: &indexerv1.StatusResponse{
			Daemon: &indexerv1.DaemonStatus{
				Pid:           12345,
				StartedAt:     now,
				UptimeSeconds: 100,
				SocketPath:    "/tmp/test.sock",
			},
			Projects: []*indexerv1.ProjectStatus{
				{
					Path:          "/Users/joe/code/my-project",
					CurrentBranch: "feature/new-api",
					FilesIndexed:  567,
					ChunksCount:   2345,
					LastIndexedAt: now,
					IsIndexing:    true,
					CurrentPhase:  indexerv1.IndexProgress_PHASE_EMBEDDING,
				},
			},
		},
	}

	socketPath := setupTestServer(t, mock)

	// Create fresh command
	var localJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show indexer daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
			if err != nil {
				if isConnectionError(err) {
					if localJSON {
						output := map[string]interface{}{
							"running": false,
							"error":   "Daemon not running",
						}
						jsonBytes, _ := json.MarshalIndent(output, "", "  ")
						fmt.Println(string(jsonBytes))
						return nil
					}
					fmt.Println("Indexer daemon: Not running")
					return nil
				}
				return fmt.Errorf("failed to get status: %w", err)
			}

			if localJSON {
				jsonBytes, err := json.MarshalIndent(resp.Msg, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(jsonBytes))
				return nil
			}

			return formatStatus(resp.Msg)
		},
	}
	cmd.Flags().BoolVar(&localJSON, "json", false, "Output as JSON")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Read from pipe in goroutine
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()

	err := cmd.Execute()

	w.Close()
	<-done
	os.Stdout = oldStdout

	output := buf.String()

	require.NoError(t, err)
	assert.Contains(t, output, "Status:       indexing (generating embeddings)")
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		seconds  int64
		expected string
	}{
		{"5 seconds", 5, "5s"},
		{"90 seconds", 90, "1m"},
		{"5 minutes", 300, "5m"},
		{"90 minutes", 5400, "1h 30m"},
		{"2 hours", 7200, "2h"},
		{"1 day", 86400, "1d"},
		{"1 day 3 hours", 97200, "1d 3h"},
		{"3 days", 259200, "3d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatDuration(time.Duration(tt.seconds) * time.Second)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTimeSince(t *testing.T) {
	t.Parallel()

	now := time.Now().Unix()

	tests := []struct {
		name     string
		unixTime int64
		contains string
	}{
		{"never", 0, "never"},
		{"5 seconds ago", now - 5, "5s ago"},
		{"5 minutes ago", now - 300, "5m ago"},
		{"2 hours ago", now - 7200, "2h ago"},
		{"1 day ago", now - 86400, "1d ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatTimeSince(tt.unixTime)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestFormatPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		phase    indexerv1.IndexProgress_Phase
		expected string
	}{
		{indexerv1.IndexProgress_PHASE_DISCOVERING, "discovering files"},
		{indexerv1.IndexProgress_PHASE_INDEXING, "parsing and chunking"},
		{indexerv1.IndexProgress_PHASE_EMBEDDING, "generating embeddings"},
		{indexerv1.IndexProgress_PHASE_COMPLETE, "complete"},
		{indexerv1.IndexProgress_PHASE_UNSPECIFIED, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()

			result := formatPhase(tt.phase)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		number   int
		expected string
	}{
		{"single digit", 5, "5"},
		{"double digit", 42, "42"},
		{"triple digit", 999, "999"},
		{"thousands", 1234, "1,234"},
		{"ten thousands", 12345, "12,345"},
		{"millions", 1234567, "1,234,567"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatNumber(tt.number)
			assert.Equal(t, tt.expected, result)
		})
	}
}
