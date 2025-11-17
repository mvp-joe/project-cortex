package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test plan for indexer logs command:
// 1. Test non-follow mode (stream existing logs and exit)
// 2. Test follow mode with manual cancellation
// 3. Test project filter
// 4. Test daemon not running
// 5. Test log entry formatting
// 6. Test helper functions (formatLogLevel, formatProjectPath)

func (m *mockIndexerServer) StreamLogs(
	ctx context.Context,
	req *connect.Request[indexerv1.LogsRequest],
	stream *connect.ServerStream[indexerv1.LogEntry],
) error {
	// Stream all mock log entries
	for _, entry := range m.logsEntries {
		// If project filter set, only send matching logs
		if req.Msg.ProjectPath != "" && entry.Project != req.Msg.ProjectPath {
			continue
		}

		if err := stream.Send(entry); err != nil {
			return err
		}
	}

	// In follow mode, we would keep streaming, but for tests just exit
	return nil
}

func TestIndexerLogsCommand_NonFollow(t *testing.T) {
	// Note: Cannot use t.Parallel() because test captures os.Stdout

	// Setup mock server with sample logs
	now := time.Now().UnixMilli()
	mock := &mockIndexerServer{
		logsEntries: []*indexerv1.LogEntry{
			{
				Timestamp: now,
				Project:   "/Users/joe/code/my-project",
				Level:     "INFO",
				Message:   "Started indexing project",
			},
			{
				Timestamp: now + 1000,
				Project:   "/Users/joe/code/my-project",
				Level:     "DEBUG",
				Message:   "Discovered 1234 files",
			},
			{
				Timestamp: now + 2000,
				Project:   "/Users/joe/code/my-project",
				Level:     "INFO",
				Message:   "Indexing complete",
			},
		},
	}

	socketPath := setupTestServer(t, mock)

	// Create a new command instance to avoid state pollution
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream logs from the indexer daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			follow, _ := cmd.Flags().GetBool("follow")
			project, _ := cmd.Flags().GetString("project")

			req := &indexerv1.LogsRequest{
				Follow:      follow,
				ProjectPath: project,
			}

			stream, err := client.StreamLogs(ctx, connect.NewRequest(req))
			if err != nil {
				if isConnectionError(err) {
					return fmt.Errorf("daemon not running. Start with: cortex indexer start")
				}
				return fmt.Errorf("failed to stream logs: %w", err)
			}

			for stream.Receive() {
				entry := stream.Msg()
				formatLogEntry(entry)
			}

			return stream.Err()
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().String("project", "", "Filter to specific project path")

	// Capture stdout since formatLogEntry prints directly to it
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, err)

	// Verify all log entries present
	assert.Contains(t, output, "Started indexing project")
	assert.Contains(t, output, "Discovered 1234 files")
	assert.Contains(t, output, "Indexing complete")
	assert.Contains(t, output, "[INFO ]")
	assert.Contains(t, output, "[DEBUG]")
}

func TestIndexerLogsCommand_ProjectFilter(t *testing.T) {
	// Note: Cannot use t.Parallel() because test captures os.Stdout

	// Setup mock server with logs from multiple projects
	now := time.Now().UnixMilli()
	mock := &mockIndexerServer{
		logsEntries: []*indexerv1.LogEntry{
			{
				Timestamp: now,
				Project:   "/Users/joe/code/project-a",
				Level:     "INFO",
				Message:   "Project A message",
			},
			{
				Timestamp: now + 1000,
				Project:   "/Users/joe/code/project-b",
				Level:     "INFO",
				Message:   "Project B message",
			},
			{
				Timestamp: now + 2000,
				Project:   "/Users/joe/code/project-a",
				Level:     "INFO",
				Message:   "Another Project A message",
			},
		},
	}

	socketPath := setupTestServer(t, mock)

	// Create a new command instance with project filter
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream logs from the indexer daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			follow, _ := cmd.Flags().GetBool("follow")
			project, _ := cmd.Flags().GetString("project")

			req := &indexerv1.LogsRequest{
				Follow:      follow,
				ProjectPath: project,
			}

			stream, err := client.StreamLogs(ctx, connect.NewRequest(req))
			if err != nil {
				if isConnectionError(err) {
					return fmt.Errorf("daemon not running. Start with: cortex indexer start")
				}
				return fmt.Errorf("failed to stream logs: %w", err)
			}

			for stream.Receive() {
				entry := stream.Msg()
				formatLogEntry(entry)
			}

			return stream.Err()
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().String("project", "", "Filter to specific project path")
	cmd.SetArgs([]string{"--project", "/Users/joe/code/project-a"})

	// Capture stdout since formatLogEntry prints directly to it
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, err)

	// Verify only project-a logs present
	assert.Contains(t, output, "Project A message")
	assert.Contains(t, output, "Another Project A message")
	assert.NotContains(t, output, "Project B message")
}

func TestIndexerLogsCommand_NotRunning(t *testing.T) {
	t.Parallel()

	// Set socket path to non-existent location (use /tmp to avoid long path issues)
	socketPath := "/tmp/cortex-test-nonexistent.sock"

	// Create a new command instance
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream logs from the indexer daemon",
		Args:  cobra.NoArgs, // Explicitly specify no args expected
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := newIndexerClient(socketPath)
			if err != nil {
				return err
			}

			follow, _ := cmd.Flags().GetBool("follow")
			project, _ := cmd.Flags().GetString("project")

			req := &indexerv1.LogsRequest{
				Follow:      follow,
				ProjectPath: project,
			}

			stream, err := client.StreamLogs(ctx, connect.NewRequest(req))
			if err != nil {
				if isConnectionError(err) {
					return fmt.Errorf("daemon not running. Start with: cortex indexer start")
				}
				return fmt.Errorf("failed to stream logs: %w", err)
			}

			for stream.Receive() {
				entry := stream.Msg()
				formatLogEntry(entry)
			}

			return stream.Err()
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().String("project", "", "Filter to specific project path")
	cmd.SilenceUsage = true // Don't print usage on error
	cmd.SilenceErrors = true // Don't print error twice

	err := cmd.Execute()

	// Verify error mentions connection failure (either our custom message or the raw connection error)
	require.Error(t, err)
	// The error should be one of: our custom "daemon not running" message OR the raw connection error
	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "daemon not running") || strings.Contains(errMsg, "dial unix"),
		"error should mention either 'daemon not running' or 'dial unix', got: %s", errMsg,
	)
}

func TestFormatLogEntry(t *testing.T) {
	t.Parallel()

	// Create sample log entry
	now := time.Date(2025, 11, 16, 14, 30, 45, 0, time.UTC)
	entry := &indexerv1.LogEntry{
		Timestamp: now.UnixMilli(),
		Project:   "/Users/joe/code/my-project",
		Level:     "INFO",
		Message:   "Test message",
	}

	// Capture output
	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatLogEntry(entry)

	w.Close()
	os.Stdout = oldStdout
	buf.ReadFrom(r)

	output := buf.String()

	// Verify format: [HH:MM:SS] [LEVEL] [PROJECT] message
	assert.Contains(t, output, "[14:30:45]")
	assert.Contains(t, output, "[INFO ]")
	assert.Contains(t, output, "[my-project]")
	assert.Contains(t, output, "Test message")
}

func TestFormatLogLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level    string
		expected string
	}{
		{"DEBUG", "DEBUG"},
		{"INFO", "INFO "},
		{"WARN", "WARN "},
		{"ERROR", "ERROR"},
		{"UNKNOWN", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			t.Parallel()

			result := formatLogLevel(tt.level)
			assert.Equal(t, tt.expected, result)
			// All should be consistent width (5 chars) except UNKNOWN
			if tt.level != "UNKNOWN" {
				assert.Len(t, result, 5)
			}
		})
	}
}

func TestFormatProjectPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"empty path", "", "system"},
		{"simple name", "my-project", "my-project"},
		{"Unix path", "/Users/joe/code/my-project", "my-project"},
		{"Windows path", "C:\\Users\\joe\\code\\my-project", "my-project"},
		{"nested path", "/very/long/nested/path/to/project", "project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatProjectPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
