package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect"
	embedv1 "github.com/mvp-joe/project-cortex/gen/embed/v1"
	"github.com/mvp-joe/project-cortex/internal/embed/onnx"
	embffi "github.com/mvp-joe/project-cortex/internal/embeddings-ffi"
)

// Server implements EmbedService RPC handlers using Rust FFI backend.
// Replaces ONNX library dependencies with unified Rust implementation.
type Server struct {
	model         *embffi.Model // nil until Initialize
	lastRequestMu sync.RWMutex
	lastRequest   time.Time
	idleTimeout   time.Duration
	dimensions    int
	startTime     time.Time
	modelDir      string // Directory for embedding models (~/.cortex/models)
	ctx           context.Context
}

// NewServer creates a new RPC server instance using Rust FFI backend.
// Does not initialize the model - client must call Initialize() RPC first.
func NewServer(ctx context.Context, libDir, modelDir string, dimensions int, idleTimeout time.Duration) (*Server, error) {
	s := &Server{
		model:       nil,
		lastRequest: time.Now(),
		idleTimeout: idleTimeout,
		dimensions:  dimensions,
		startTime:   time.Now(),
		modelDir:    modelDir,
		ctx:         ctx,
	}

	// Start idle monitor goroutine
	go s.monitorIdle()

	return s, nil
}

// Initialize implements the Initialize RPC endpoint.
// Downloads model if needed, then loads it into Rust FFI.
// Streams progress updates via server stream.
func (s *Server) Initialize(
	ctx context.Context,
	req *connect.Request[embedv1.InitializeRequest],
	stream *connect.ServerStream[embedv1.InitializeProgress],
) error {
	fmt.Print("Test")
	// Check if files exist
	if err := stream.Send(&embedv1.InitializeProgress{
		Status:  "checking",
		Message: "Checking for embedding models...",
	}); err != nil {
		return fmt.Errorf("failed to send progress: %w", err)
	}

	// Create downloader for model downloads
	// Note: Rust FFI handles ONNX runtime internally, no separate download needed
	downloader := onnx.NewDownloader()

	// Download embedding model if needed
	if !onnx.EmbeddingModelExists(s.modelDir) {
		if err := stream.Send(&embedv1.InitializeProgress{
			Status:          "downloading",
			Message:         "Downloading embedding models...",
			DownloadPercent: 0,
		}); err != nil {
			return fmt.Errorf("failed to send progress: %w", err)
		}

		if err := downloader.DownloadEmbeddingModel(ctx, s.modelDir, func(pct int) {
			_ = stream.Send(&embedv1.InitializeProgress{
				Status:          "downloading",
				Message:         "Downloading embedding models...",
				DownloadPercent: int32(pct),
			})
		}); err != nil {
			return fmt.Errorf("model download failed: %w", err)
		}
	}

	// Load model into Rust FFI
	if err := stream.Send(&embedv1.InitializeProgress{
		Status:  "loading",
		Message: "Loading embedding model into memory...",
	}); err != nil {
		return fmt.Errorf("failed to send progress: %w", err)
	}

	// Model files are in models/bge subdirectory
	bgeDir := filepath.Join(s.modelDir, "bge")
	model, err := embffi.NewModel(
		filepath.Join(bgeDir, "model.onnx"),
		filepath.Join(bgeDir, "tokenizer.json"),
	)
	if err != nil {
		return fmt.Errorf("failed to load model: %w", err)
	}

	s.model = model

	// Send ready status
	return stream.Send(&embedv1.InitializeProgress{
		Status:  "ready",
		Message: "Ready to generate embeddings (Rust FFI backend)",
	})
}

// Embed implements the Embed RPC endpoint.
// Generates embeddings for input texts using Rust FFI backend.
func (s *Server) Embed(
	ctx context.Context,
	req *connect.Request[embedv1.EmbedRequest],
) (*connect.Response[embedv1.EmbedResponse], error) {
	// Update last request time
	s.lastRequestMu.Lock()
	s.lastRequest = time.Now()
	s.lastRequestMu.Unlock()

	// Check initialization
	if s.model == nil {
		return nil, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("not initialized: call Initialize first"),
		)
	}

	// Generate embeddings
	start := time.Now()
	log.Printf("[EMBED] Batch size: %d texts (Rust FFI)", len(req.Msg.Texts))
	embeddings, err := s.model.EncodeBatch(req.Msg.Texts)
	if err != nil {
		return nil, fmt.Errorf("embedding generation failed: %w", err)
	}
	log.Printf("[EMBED] Completed in %dms (%d texts)", time.Since(start).Milliseconds(), len(req.Msg.Texts))

	// BGE model outputs fixed 384 dimensions (no Matryoshka truncation support)

	// Build response
	resp := &embedv1.EmbedResponse{
		Embeddings:      make([]*embedv1.Embedding, len(embeddings)),
		Dimensions:      int32(s.dimensions),
		InferenceTimeMs: time.Since(start).Milliseconds(),
	}

	for i, emb := range embeddings {
		resp.Embeddings[i] = &embedv1.Embedding{Values: emb}
	}

	return connect.NewResponse(resp), nil
}

// Health implements the Health RPC endpoint.
// Returns server health and activity statistics.
func (s *Server) Health(
	ctx context.Context,
	req *connect.Request[embedv1.HealthRequest],
) (*connect.Response[embedv1.HealthResponse], error) {
	s.lastRequestMu.RLock()
	lastReq := s.lastRequest
	s.lastRequestMu.RUnlock()

	return connect.NewResponse(&embedv1.HealthResponse{
		Healthy:          true,
		UptimeSeconds:    int64(time.Since(s.startTime).Seconds()),
		LastRequestMsAgo: time.Since(lastReq).Milliseconds(),
	}), nil
}

// monitorIdle periodically checks for idle timeout and exits if exceeded.
// Runs in background goroutine started by NewServer.
func (s *Server) monitorIdle() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.lastRequestMu.RLock()
			idle := time.Since(s.lastRequest)
			s.lastRequestMu.RUnlock()

			if idle > s.idleTimeout {
				log.Printf("Idle timeout exceeded (%v), shutting down...", s.idleTimeout)
				os.Exit(0)
			}

		case <-s.ctx.Done():
			return
		}
	}
}

// Close cleans up server resources.
// Should be called during graceful shutdown.
func (s *Server) Close() error {
	if s.model != nil {
		return s.model.Close()
	}
	return nil
}
