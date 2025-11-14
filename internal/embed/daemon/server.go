//go:build !rust_ffi

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
	onnxruntime "github.com/yalue/onnxruntime_go"
)

// Server implements EmbedService RPC handlers.
// Unexported implementation following project conventions.
type Server struct {
	model         *onnx.EmbeddingModel // nil until Initialize
	lastRequestMu sync.RWMutex
	lastRequest   time.Time
	idleTimeout   time.Duration
	dimensions    int
	startTime     time.Time
	libDir        string // Directory for runtime libraries (~/.cortex/lib)
	modelDir      string // Directory for embedding models (~/.cortex/models)
	ctx           context.Context
}

var (
	onnxInitialized bool
	onnxInitMutex   sync.Mutex
)

// initializeONNXEnvironment initializes the ONNX Runtime environment once per process.
// Subsequent calls are no-ops. Thread-safe.
func initializeONNXEnvironment(libDir string) error {
	onnxInitMutex.Lock()
	defer onnxInitMutex.Unlock()

	if onnxInitialized {
		return nil // Already initialized
	}

	// Set ONNX Runtime library path
	runtimeLibPath := filepath.Join(libDir, onnx.GetRuntimeLibName())
	onnxruntime.SetSharedLibraryPath(runtimeLibPath)

	// Initialize ONNX Runtime environment (once per process)
	if err := onnxruntime.InitializeEnvironment(); err != nil {
		return fmt.Errorf("failed to initialize ONNX environment: %w", err)
	}

	onnxInitialized = true
	return nil
}

// NewServer creates a new RPC server instance.
// Does not initialize the model - client must call Initialize() RPC first.
func NewServer(ctx context.Context, libDir, modelDir string, dimensions int, idleTimeout time.Duration) (*Server, error) {
	// Initialize ONNX environment (once per process)
	if err := initializeONNXEnvironment(libDir); err != nil {
		return nil, err
	}

	s := &Server{
		model:       nil,
		lastRequest: time.Now(),
		idleTimeout: idleTimeout,
		dimensions:  dimensions,
		startTime:   time.Now(),
		libDir:      libDir,
		modelDir:    modelDir,
		ctx:         ctx,
	}

	// Start idle monitor goroutine
	go s.monitorIdle()

	return s, nil
}

// Initialize implements the Initialize RPC endpoint.
// Downloads runtime and model if needed, then loads them into memory.
// Streams progress updates via server stream.
func (s *Server) Initialize(
	ctx context.Context,
	req *connect.Request[embedv1.InitializeRequest],
	stream *connect.ServerStream[embedv1.InitializeProgress],
) error {
	// Check if files exist
	if err := stream.Send(&embedv1.InitializeProgress{
		Status:  "checking",
		Message: "Checking for embedding models...",
	}); err != nil {
		return fmt.Errorf("failed to send progress: %w", err)
	}

	// Create downloader for runtime and model downloads
	downloader := onnx.NewDownloader()

	// Download ONNX runtime if needed
	if !onnx.RuntimeExists(s.libDir) {
		if err := stream.Send(&embedv1.InitializeProgress{
			Status:          "downloading",
			Message:         "Downloading runtime libraries...",
			DownloadPercent: 0,
		}); err != nil {
			return fmt.Errorf("failed to send progress: %w", err)
		}

		if err := downloader.DownloadRuntime(ctx, s.libDir, func(pct int) {
			_ = stream.Send(&embedv1.InitializeProgress{
				Status:          "downloading",
				Message:         "Downloading runtime libraries...",
				DownloadPercent: int32(pct),
			})
		}); err != nil {
			return fmt.Errorf("runtime download failed: %w", err)
		}
	}

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

	// Load model into memory
	if err := stream.Send(&embedv1.InitializeProgress{
		Status:  "loading",
		Message: "Loading embedding model into memory...",
	}); err != nil {
		return fmt.Errorf("failed to send progress: %w", err)
	}

	// Model files are in models/bge subdirectory
	bgeDir := filepath.Join(s.modelDir, "bge")
	model, err := onnx.NewEmbeddingModel(
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
		Message: "Ready to generate embeddings",
	})
}

// Embed implements the Embed RPC endpoint.
// Generates embeddings for input texts.
func (s *Server) Embed(
	ctx context.Context,
	req *connect.Request[embedv1.EmbedRequest],
) (*connect.Response[embedv1.EmbedResponse], error) {
	// Update last request time (do this before checking initialization
	// so idle timeout is reset even for failed requests)
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
	log.Printf("[EMBED] Batch size: %d texts", len(req.Msg.Texts))
	embeddings, err := s.model.EmbedBatch(req.Msg.Texts)
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
		return s.model.Destroy()
	}
	return nil
}
