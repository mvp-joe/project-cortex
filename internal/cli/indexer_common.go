package cli

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/mvp-joe/project-cortex/gen/indexer/v1/indexerv1connect"
	"github.com/spf13/cobra"
)

var indexerCmd = &cobra.Command{
	Use:   "indexer",
	Short: "Indexer daemon commands",
	Long:  `Manage the indexer daemon.`,
}

func init() {
	rootCmd.AddCommand(indexerCmd)
}

// newIndexerClient creates a ConnectRPC client for the indexer daemon.
// socketPath: Unix socket path for the indexer daemon (from GlobalConfig).
func newIndexerClient(socketPath string) (indexerv1connect.IndexerServiceClient, error) {

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}

	return indexerv1connect.NewIndexerServiceClient(
		httpClient,
		"http://localhost", // Ignored for Unix socket
	), nil
}

// isConnectionError checks if error is a connection refused or similar error.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such file") ||
		strings.Contains(errStr, "connect: no such file or directory") ||
		strings.Contains(errStr, "dial unix")
}

