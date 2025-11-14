//go:build !rust_ffi

package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mvp-joe/project-cortex/gen/embed/v1/embedv1connect"
	"github.com/mvp-joe/project-cortex/internal/daemon"
	embeddaemon "github.com/mvp-joe/project-cortex/internal/embed/daemon"
	"github.com/spf13/cobra"
	onnxruntime "github.com/yalue/onnxruntime_go"
)

var embedStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start embedding daemon server",
	Long: `Start the embedding daemon server.
The server listens on a Unix socket (~/.cortex/embed.sock) and serves
ConnectRPC requests for text embedding generation.

The daemon automatically exits after 10 minutes of idle time.`,
	RunE: runEmbedStart,
}

func init() {
	embedCmd.AddCommand(embedStartCmd)
	// embedStart2Cmd is registered in embed_rust_ffi.go when rust_ffi build tag is present
}

func runEmbedStart(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	socketPath, err := daemon.GetEmbedSocketPath()
	if err != nil {
		return fmt.Errorf("failed to get socket path: %w", err)
	}

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

	// Get configuration
	libDir := getLibDir()
	modelDir := getModelDir()
	idleTimeout := getIdleTimeout()
	dimensions := getDimensions()

	// Create server instance
	srv, err := embeddaemon.NewServer(ctx, libDir, modelDir, dimensions, idleTimeout)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	// Note: Skip defer srv.Close() to avoid C++/Rust threading cleanup issues
	// on signal-based shutdown. OS handles resource cleanup on process exit.

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

	// Exit immediately on signal (skip graceful shutdown to avoid C++/Rust cleanup issues)
	go func() {
		<-ctx.Done()
		log.Println("Shutdown signal received, exiting...")
		// Destroy ONNX environment to clean up C++ threads before exit
		onnxruntime.DestroyEnvironment()
		os.Exit(0)
	}()

	log.Printf("Embedding server started (socket: %s, dimensions: %d, idle timeout: %v)",
		socketPath, dimensions, idleTimeout)

	// Serve ConnectRPC requests
	if err := httpServer.Serve(listener); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
