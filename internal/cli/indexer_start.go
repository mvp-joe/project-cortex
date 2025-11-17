package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mvp-joe/project-cortex/gen/indexer/v1/indexerv1connect"
	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/mvp-joe/project-cortex/internal/daemon"
	"github.com/mvp-joe/project-cortex/internal/embed"
	indexerdaemon "github.com/mvp-joe/project-cortex/internal/indexer/daemon"
	"github.com/spf13/cobra"
)

var indexerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start indexer daemon server",
	Long: `Start the indexer daemon server.
The server listens on a Unix socket (~/.cortex/indexer.sock) and serves
ConnectRPC requests for code indexing and project watching.

The daemon manages per-project indexing actors, tracks file changes via
git and inotify watchers, and provides incremental indexing with hot reload.

Singleton enforcement ensures only one indexer daemon runs at a time.`,
	RunE: runIndexerStart,
}

func init() {
	indexerCmd.AddCommand(indexerStartCmd)
}

func runIndexerStart(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Load global config
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	socketPath := globalCfg.IndexerDaemon.SocketPath

	// Use daemon foundation for singleton enforcement
	singleton := daemon.NewSingletonDaemon("indexer", socketPath)
	won, err := singleton.EnforceSingleton()
	if err != nil {
		return fmt.Errorf("singleton check failed: %w", err)
	}

	if !won {
		// Another daemon already running - exit gracefully
		fmt.Println("Indexer daemon already running")
		return nil
	}
	defer singleton.Release()

	// Create and initialize embedding provider (shared across all actors)
	embedProvider, err := embed.NewProvider(embed.Config{
		Provider:   "local", // Default to local provider
		Endpoint:   "",      // Will use default endpoint
		SocketPath: globalCfg.EmbedDaemon.SocketPath,
	})
	if err != nil {
		return fmt.Errorf("failed to create embedding provider: %w", err)
	}
	defer embedProvider.Close()

	// Initialize embedding provider (starts daemon if needed)
	if err := embedProvider.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize embedding provider: %w", err)
	}

	// Create cache instance (empty string = default ~/.cortex/cache)
	cacheInstance := cache.NewCache("")

	// Create server instance
	srv, err := indexerdaemon.NewServer(ctx, socketPath, embedProvider, cacheInstance)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Bind Unix socket
	listener, err := singleton.BindSocket()
	if err != nil {
		return fmt.Errorf("failed to bind socket: %w", err)
	}
	defer listener.Close()

	// Set socket permissions (user-only)
	if err := os.Chmod(socketPath, 0600); err != nil {
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	// Setup ConnectRPC HTTP server
	mux := http.NewServeMux()
	path, handler := indexerv1connect.NewIndexerServiceHandler(srv)
	mux.Handle(path, handler)

	httpServer := &http.Server{Handler: mux}

	// Graceful shutdown on signal
	go func() {
		<-ctx.Done()
		log.Println("Shutdown signal received, shutting down gracefully...")

		// Give server 30s to shutdown gracefully
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		// Shutdown HTTP server (stops accepting new connections, waits for active requests)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("Indexer daemon started (PID %d) on %s", os.Getpid(), socketPath)

	// Serve ConnectRPC requests (blocks until Shutdown() is called)
	if err := httpServer.Serve(listener); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	// Clean shutdown - defers will run here
	return nil
}
