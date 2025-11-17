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

	"github.com/mvp-joe/project-cortex/gen/embed/v1/embedv1connect"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/mvp-joe/project-cortex/internal/daemon"
	embeddaemon "github.com/mvp-joe/project-cortex/internal/embed/daemon"
	"github.com/spf13/cobra"
)

var embedStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start embedding daemon server",
	Long: `Start the embedding daemon server using Rust FFI backend.
The server listens on a Unix socket (~/.cortex/embed.sock) and serves
ConnectRPC requests for text embedding generation.

Uses tract-onnx (pure Rust ONNX inference) with the BGE-small-en-v1.5
embedding model for semantic search capabilities.

The daemon automatically exits after 10 minutes of idle time.`,
	RunE: runEmbedStart,
}

func init() {
	embedCmd.AddCommand(embedStartCmd)
}

func runEmbedStart(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Load global config
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	socketPath := globalCfg.EmbedDaemon.SocketPath
	libDir := globalCfg.EmbedDaemon.LibDir
	modelDir := globalCfg.EmbedDaemon.ModelDir
	idleTimeout := time.Duration(globalCfg.EmbedDaemon.IdleTimeout) * time.Second
	dimensions := 384 // BGE-small-en-v1.5 model produces 384-dimensional embeddings

	// Use daemon foundation for singleton enforcement
	singleton := daemon.NewSingletonDaemon("embed", socketPath)
	won, err := singleton.EnforceSingleton()
	if err != nil {
		return fmt.Errorf("singleton check failed: %w", err)
	}

	if !won {
		// Another daemon already running - exit gracefully
		fmt.Println("Embedding server already running")
		return nil
	}
	defer singleton.Release()

	// Create server instance (Rust FFI backend)
	srv, err := embeddaemon.NewServer(ctx, libDir, modelDir, dimensions, idleTimeout)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	// Note: Skip defer srv.Close() to avoid cleanup issues on signal-based shutdown

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
	path, handler := embedv1connect.NewEmbedServiceHandler(srv)
	mux.Handle(path, handler)

	httpServer := &http.Server{Handler: mux}

	// Exit immediately on signal (skip graceful shutdown)
	go func() {
		<-ctx.Done()
		log.Println("Shutdown signal received, exiting...")
		// No ONNX environment cleanup needed for Rust FFI
		os.Exit(0)
	}()

	log.Printf("Embedding server started (Rust FFI backend) (socket: %s, dimensions: %d, idle timeout: %v)",
		socketPath, dimensions, idleTimeout)

	// Serve ConnectRPC requests
	if err := httpServer.Serve(listener); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
