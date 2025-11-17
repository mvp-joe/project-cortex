package cli

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/spf13/cobra"
)

var indexerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the indexer daemon",
	Long: `Stop the indexer daemon gracefully.

The daemon will:
1. Stop accepting new index requests
2. Wait for in-progress indexing to complete (timeout: 30s)
3. Stop all project actors
4. Close embedding server connection
5. Remove Unix socket and exit`,
	RunE: runIndexerStop,
}

func init() {
	indexerCmd.AddCommand(indexerStopCmd)
}

func runIndexerStop(cmd *cobra.Command, args []string) error {
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

	// Call Shutdown RPC
	resp, err := client.Shutdown(ctx, connect.NewRequest(&indexerv1.ShutdownRequest{}))
	if err != nil {
		// Check for connection refused (daemon not running)
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
