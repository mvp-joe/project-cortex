package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"connectrpc.com/connect"
	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/spf13/cobra"
)

var (
	statusJSON bool
)

var indexerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show indexer daemon status",
	Long: `Show the status of the indexer daemon and all registered projects.

Displays:
- Daemon process information (PID, uptime, socket path)
- All watched projects with indexing statistics
- Current indexing phase (if actively indexing)`,
	RunE: runIndexerStatus,
}

func init() {
	indexerCmd.AddCommand(indexerStatusCmd)
	indexerStatusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")
}

func runIndexerStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Load global config
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	client, err := newIndexerClient(globalCfg.IndexerDaemon.SocketPath)
	if err != nil {
		return err
	}

	// Call GetStatus RPC
	resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
	if err != nil {
		if isConnectionError(err) {
			if statusJSON {
				// Output minimal JSON indicating daemon is not running
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

	// Output as JSON if flag set
	if statusJSON {
		jsonBytes, err := json.MarshalIndent(resp.Msg, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(jsonBytes))
		return nil
	}

	// Format output for human consumption
	return formatStatus(resp.Msg)
}

func formatStatus(status *indexerv1.StatusResponse) error {
	daemon := status.Daemon

	// Daemon info
	fmt.Println("Daemon Status:")
	fmt.Printf("  PID:    %d\n", daemon.Pid)
	fmt.Printf("  Uptime: %s\n", formatDuration(time.Duration(daemon.UptimeSeconds)*time.Second))
	fmt.Printf("  Socket: %s\n", daemon.SocketPath)
	fmt.Println()

	// Projects info
	if len(status.Projects) == 0 {
		fmt.Println("No projects registered")
		return nil
	}

	fmt.Printf("Registered Projects (%d):\n", len(status.Projects))
	for _, p := range status.Projects {
		formatProject(p)
	}

	return nil
}

func formatProject(p *indexerv1.ProjectStatus) {
	fmt.Printf("  %s\n", p.Path)
	fmt.Printf("    Branch:       %s\n", p.CurrentBranch)
	fmt.Printf("    Files:        %s\n", formatNumber(int(p.FilesIndexed)))
	fmt.Printf("    Chunks:       %s\n", formatNumber(int(p.ChunksCount)))
	fmt.Printf("    Last indexed: %s\n", formatTimeSince(p.LastIndexedAt))

	if p.IsIndexing {
		fmt.Printf("    Status:       indexing (%s)\n", formatPhase(p.CurrentPhase))
	} else {
		fmt.Printf("    Status:       watching\n")
	}
	fmt.Println()
}

// formatDuration formats a duration in compact format.
// Examples: "5s", "1m", "1h 30m", "2h", "1d", "1d 3h", "3d"
func formatDuration(d time.Duration) string {
	seconds := int(d.Seconds())

	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	}

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}

	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}

	return fmt.Sprintf("%ds", secs)
}

// formatTimeSince formats Unix timestamp as time ago.
// Examples: "5m ago", "2h ago", "3d ago"
func formatTimeSince(unixSeconds int64) string {
	if unixSeconds == 0 {
		return "never"
	}

	timestamp := time.Unix(unixSeconds, 0)
	since := time.Since(timestamp)

	days := int(since.Hours() / 24)
	hours := int(since.Hours()) % 24
	minutes := int(since.Minutes()) % 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%dd %dh ago", days, hours)
		}
		return fmt.Sprintf("%dd ago", days)
	}

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm ago", hours, minutes)
		}
		return fmt.Sprintf("%dh ago", hours)
	}

	if minutes > 0 {
		return fmt.Sprintf("%dm ago", minutes)
	}

	return fmt.Sprintf("%ds ago", int(since.Seconds()))
}

// formatPhase converts indexing phase enum to human-readable string.
func formatPhase(phase indexerv1.IndexProgress_Phase) string {
	switch phase {
	case indexerv1.IndexProgress_PHASE_DISCOVERING:
		return "discovering files"
	case indexerv1.IndexProgress_PHASE_INDEXING:
		return "parsing and chunking"
	case indexerv1.IndexProgress_PHASE_EMBEDDING:
		return "generating embeddings"
	case indexerv1.IndexProgress_PHASE_COMPLETE:
		return "complete"
	default:
		return "unknown"
	}
}

// formatNumber formats integer with thousand separators.
// Examples: 1234 -> "1,234", 1234567 -> "1,234,567"
func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	// Simple implementation for thousands/millions
	str := fmt.Sprintf("%d", n)
	var result string
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}
