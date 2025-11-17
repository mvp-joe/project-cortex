package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"
	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/spf13/cobra"
)

var (
	logsFollow  bool
	logsProject string
)

var indexerLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Stream logs from the indexer daemon",
	Long: `Stream log entries from the indexer daemon.

By default, streams existing logs and exits.
Use --follow to continuously stream new logs (like tail -f).
Use --project to filter logs to a specific project path.`,
	RunE: runIndexerLogs,
}

func init() {
	indexerCmd.AddCommand(indexerLogsCmd)
	indexerLogsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output (tail -f behavior)")
	indexerLogsCmd.Flags().StringVar(&logsProject, "project", "", "Filter to specific project path")
}

func runIndexerLogs(cmd *cobra.Command, args []string) error {
	// Handle Ctrl+C gracefully for follow mode
	ctx := context.Background()
	if logsFollow {
		var cancel context.CancelFunc
		ctx, cancel = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer cancel()
	}

	// Load global config
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	client, err := newIndexerClient(globalCfg.IndexerDaemon.SocketPath)
	if err != nil {
		return err
	}

	// Call StreamLogs RPC
	req := &indexerv1.LogsRequest{
		ProjectPath: logsProject,
		Follow:      logsFollow,
	}

	stream, err := client.StreamLogs(ctx, connect.NewRequest(req))
	if err != nil {
		if isConnectionError(err) {
			return fmt.Errorf("daemon not running. Start with: cortex indexer start")
		}
		return fmt.Errorf("failed to stream logs: %w", err)
	}

	// Stream log entries
	for stream.Receive() {
		entry := stream.Msg()
		formatLogEntry(entry)
	}

	// Check for errors (EOF is expected for non-follow mode)
	if err := stream.Err(); err != nil {
		// EOF is normal for non-follow mode
		if err == io.EOF {
			return nil
		}
		// Context cancelled (Ctrl+C) is normal for follow mode
		if ctx.Err() == context.Canceled {
			return nil
		}
		return fmt.Errorf("stream error: %w", err)
	}

	return nil
}

// formatLogEntry prints a log entry in human-readable format.
// Format: [TIMESTAMP] [LEVEL] [PROJECT] message
func formatLogEntry(entry *indexerv1.LogEntry) {
	// Convert Unix milliseconds to time (UTC)
	timestamp := time.Unix(0, entry.Timestamp*int64(time.Millisecond)).UTC()

	// Format timestamp as HH:MM:SS
	timeStr := timestamp.Format("15:04:05")

	// Format level with color/padding
	levelStr := formatLogLevel(entry.Level)

	// Truncate project path to basename for readability
	projectStr := formatProjectPath(entry.Project)

	fmt.Printf("[%s] [%s] [%s] %s\n", timeStr, levelStr, projectStr, entry.Message)
}

// formatLogLevel formats log level with consistent width.
func formatLogLevel(level string) string {
	// Pad to 5 chars for alignment (DEBUG, INFO, WARN, ERROR)
	switch level {
	case "DEBUG":
		return "DEBUG"
	case "INFO":
		return "INFO "
	case "WARN":
		return "WARN "
	case "ERROR":
		return "ERROR"
	default:
		return level
	}
}

// formatProjectPath shortens project path for display.
// Example: /Users/joe/code/my-project -> my-project
func formatProjectPath(path string) string {
	if path == "" {
		return "system"
	}

	// Get last component of path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}

	return path
}
