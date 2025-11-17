package embed

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

	"connectrpc.com/connect"
	embedv1 "github.com/mvp-joe/project-cortex/gen/embed/v1"
	"github.com/mvp-joe/project-cortex/gen/embed/v1/embedv1connect"
	"github.com/mvp-joe/project-cortex/internal/daemon"
)

// localProvider manages a ConnectRPC client to the ONNX embedding daemon.
type localProvider struct {
	socketPath   string
	client       embedv1connect.EmbedServiceClient
	httpClient   *http.Client
	daemonConfig *daemon.DaemonConfig
	initialized  bool
}

// newLocalProvider creates a new local embedding provider with ConnectRPC client.
// Sets up Unix socket transport for daemon communication.
// socketPath: Unix socket path for the embedding daemon (from GlobalConfig).
func newLocalProvider(socketPath string) (*localProvider, error) {
	// Create daemon config for auto-start
	daemonConfig, err := daemon.NewEmbedDaemonConfig(socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create daemon config: %w", err)
	}

	// Create HTTP client with Unix socket transport
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
	}

	// Create ConnectRPC client
	client := embedv1connect.NewEmbedServiceClient(
		httpClient,
		"http://localhost", // Dummy URL (using Unix socket)
	)

	return &localProvider{
		socketPath:   socketPath,
		client:       client,
		httpClient:   httpClient,
		daemonConfig: daemonConfig,
		initialized:  false,
	}, nil
}

// Initialize ensures the embedding daemon is running and initializes the ONNX model.
// Automatically starts the daemon if not running (via EnsureDaemon).
// Streams progress updates during model download and loading.
func (p *localProvider) Initialize(ctx context.Context) error {
	if p.initialized {
		return nil
	}

	// 1. Ensure daemon is running (auto-start if needed)
	if err := daemon.EnsureDaemon(ctx, p.daemonConfig); err != nil {
		return fmt.Errorf("failed to ensure daemon: %w", err)
	}

	// 2. Call Initialize RPC with streaming progress
	req := connect.NewRequest(&embedv1.InitializeRequest{})

	stream, err := p.client.Initialize(ctx, req)
	if err != nil {
		return fmt.Errorf("initialize RPC failed: %w", err)
	}

	// 3. Stream progress updates
	log.Println("Initializing embedding server...")
	for stream.Receive() {
		progress := stream.Msg()
		if progress.Message != "" {
			log.Println(progress.Message)
		}
		// Show download progress if available
		if progress.Status == "downloading" && progress.DownloadPercent > 0 {
			log.Printf("Downloading: %d%%", progress.DownloadPercent)
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("initialize stream error: %w", err)
	}

	p.initialized = true
	return nil
}

// Embed generates embeddings for the given texts using the ONNX daemon.
// Implements resurrection pattern: auto-restarts daemon on connection failure.
// Initialize() must be called before Embed().
func (p *localProvider) Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized: call Initialize() first")
	}

	// Try RPC call
	embeddings, err := p.embedRPC(ctx, texts, mode)

	// Resurrect on connection failure (daemon may have shut down due to idle timeout)
	if daemon.IsConnectionError(err) {
		log.Println("Daemon connection lost, resurrecting...")
		if err := daemon.EnsureDaemon(ctx, p.daemonConfig); err != nil {
			return nil, fmt.Errorf("resurrection failed: %w", err)
		}
		// Retry once
		embeddings, err = p.embedRPC(ctx, texts, mode)
	}

	return embeddings, err
}

// embedRPC performs the actual Embed RPC call to the daemon.
func (p *localProvider) embedRPC(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {
	req := connect.NewRequest(&embedv1.EmbedRequest{
		Texts: texts,
		Mode:  string(mode),
	})

	resp, err := p.client.Embed(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("embed RPC failed: %w", err)
	}

	// Convert []*Embedding (with []float32) to [][]float32
	embeddings := make([][]float32, len(resp.Msg.Embeddings))
	for i, emb := range resp.Msg.Embeddings {
		embeddings[i] = emb.Values
	}

	return embeddings, nil
}

// Dimensions returns the dimensionality of the embeddings (384 for BGE-small model).
func (p *localProvider) Dimensions() int {
	return 384
}

// Close releases resources. The daemon manages its own lifecycle (idle timeout auto-shutdown).
// No manual process management needed.
func (p *localProvider) Close() error {
	// Daemon foundation handles lifecycle - nothing to clean up
	return nil
}
