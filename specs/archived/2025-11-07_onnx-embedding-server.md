---
status: implemented
started_at: 2025-11-07T00:00:00Z
completed_at: 2025-11-10T08:35:00Z
dependencies: []
implementation: ONNX Runtime (pure Go with C bindings)
---

# Embedding Daemon Server

## Purpose

Replace the Python-based cortex-embed embedding service with a pure Go implementation using ONNX Runtime. This eliminates the 500-700MB total distribution (300-500MB Python binary + 200MB model), simplifies cross-platform builds to trivial `go build`, reduces total distribution to 237-239MB, and enables sub-second cold starts with aggressive idle shutdown patterns.

**Note**: ONNX is the current runtime implementation detail. Users interact with "embedding server/daemon" concepts - ONNX terminology is not exposed in public APIs, CLI commands, or configuration.

## Core Concept

**Input**: Text strings requiring vector embeddings (from indexer and MCP servers)

**Process**: Shared daemon serving ConnectRPC over Unix socket → ONNX model inference → Idle timeout after 10 minutes → Auto-restart on demand

**Output**: 768-dimensional float32 embeddings with Matryoshka truncation support

## Benefits Over Python Implementation

**Distribution Size Reduction:**
- **Current state**: 500-700MB total
  - cortex-embed binary: 300-500MB (Python runtime + PyTorch + sentence-transformers embedded via go-embed-python)
  - BGE-small-en-v1.5 model: 200MB (downloaded on first run to HuggingFace cache)
- **New state**: 237-239MB total
  - cortex binary: 3-5MB (pure Go with ONNX Runtime C bindings)
  - ONNX model archive: 234MB (ONNX Runtime + quantized Gemma model + tokenizer)
- **Savings**: 260-460MB

**Build Complexity Elimination:**
- **Current**: Platform-specific Python dependency generation
  - Run `task python:deps:<platform>` to download Python itself
  - Download all PyTorch binaries and sentence-transformers for target platform
  - Package into go-embed-python format
  - Separate builds for each platform (darwin-arm64, darwin-amd64, linux-amd64, linux-arm64, windows-amd64)
  - Build failures common due to dependency resolution issues
  - 10-20 minutes to generate all platform deps
- **New**: Standard Go cross-compilation
  - Single `go build` command
  - GOOS/GOARCH flags handle all platforms
  - No dependency pre-generation needed
  - ~30 seconds to build for all platforms

**Startup Performance:**
- **Current**: 5-10 seconds (Python runtime + PyTorch model loading)
- **New**: <1 second (ONNX model loading 2-3x faster)

**Embedding Quality:**
- **Current**: BGE-small-en-v1.5 (384 dimensions)
- **New**: Google Gemma (768 dimensions, quantized to 4-bit)
- **Quality**: Gemma has significantly higher accuracy scores on embedding benchmarks
- **Flexibility**: Matryoshka truncation allows 768/512/256/128 dimensions

## Technology Stack

- **Language**: Go 1.25+
- **RPC**: ConnectRPC (gRPC over Unix domain socket)
- **Protocol**: Protocol Buffers (schema-defined APIs)
- **ONNX Runtime**: `github.com/yalue/onnxruntime_go` v1.22.0 (C bindings to onnxruntime.so)
- **Tokenizer**: `github.com/eliben/go-sentencepiece` v0.6.0 (pure Go, no CGO)
- **Model**: Google Gemma embedding model (quantized, 768 dimensions)
- **Process Coordination**: File locking (`github.com/gofrs/flock`)
- **Pattern**: Follows indexer daemon architecture (Unix socket, auto-start, EnsureDaemon pattern)

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────┐
│         cortex embed (daemon process)                │
│  Single shared instance per machine                  │
│                                                      │
│  ┌────────────────────────────────────────────────┐ │
│  │  ONNX Model                                    │ │
│  │  - Loaded once on startup (~200MB RAM)        │ │
│  │  - Thread-safe inference (no mutex)           │ │
│  │  - Batch processing optimized                 │ │
│  │  - <1s cold start                             │ │
│  └────────────────────────────────────────────────┘ │
│                                                      │
│  ┌────────────────────────────────────────────────┐ │
│  │  ConnectRPC Server                             │ │
│  │  Unix socket: ~/.cortex/embed.sock             │ │
│  │  - Embed(texts[]) → embeddings[][]             │ │
│  │  - Health() → uptime, last_request_ago         │ │
│  └────────────────────────────────────────────────┘ │
│                                                      │
│  ┌────────────────────────────────────────────────┐ │
│  │  Idle Timeout Manager                          │ │
│  │  - Tracks last request time (RWMutex)          │ │
│  │  - Checks every 30s                            │ │
│  │  - Exits after 5min idle                       │ │
│  └────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
         ↑                            ↑
         │ ConnectRPC clients         │
    ┌────┴──────────┐          ┌─────┴──────────┐
    │ cortex index  │          │ cortex mcp (×N) │
    │ (indexer)     │          │ (per tab)       │
    └───────────────┘          └────────────────┘
```

### Process Model

**Daemon lifecycle:**
1. First client calls `provider.Initialize()` → `EnsureEmbedServer()`
2. Check if `~/.cortex/embed.sock` exists and healthy
3. If not healthy: acquire lock, start `cortex embed start` (detached)
4. Wait for health check to pass (<1s)
5. Return (daemon now running)
6. After 10 minutes idle: daemon exits automatically
7. Next request repeats cycle (transparent to client)

**Key properties:**
- **Zero configuration**: Clients auto-start server on demand
- **Automatic shutdown**: 10-minute idle timeout (fast restart justifies it)
- **Crash resilient**: Stale socket detection and cleanup
- **Concurrency**: Multiple clients share single model instance

### Model Storage Layout

```
~/.cortex/onnx/                       (Shared across all projects)
├── onnxruntime.dylib                 (macOS, 33MB)
│   onnxruntime.so                    (Linux, 33MB)
│   onnxruntime.dll                   (Windows, 33MB)
├── model_q4.onnx                     (500KB - ONNX graph)
├── model_q4.onnx_data                (197MB - quantized weights)
└── tokenizer.model                   (4.5MB - SentencePiece vocab)

Total per platform: ~234MB uncompressed, ~174MB zipped
```

**Download source**: GitHub releases (platform-specific archives)

**Download timing**: On-demand during first `provider.Initialize()` call

**Download UX Strategy**: Streaming progress via Initialize RPC
- Daemon starts without loading models (fast startup)
- Client calls `provider.Initialize()` which invokes `Initialize()` RPC
- Server checks if models exist; if not, downloads while streaming progress
- Client receives real-time progress updates via ConnectRPC stream
- Progress displayed to user: "Downloading embedding models (174MB)... 45%"
- After download completes, models loaded into memory
- Client receives "ready" status and can begin calling `Embed()`
- Consistent with indexer daemon streaming progress pattern (see indexer daemon spec)
- No silent 60s hangs - user sees progress throughout
- If models already exist, Initialize completes instantly (just loads into memory)

## Interface Unification

The current architecture has duplicate embedding provider interfaces that need consolidation:

**Current Duplicate Interfaces:**

1. `internal/embed/provider.go`:
```go
type Provider interface {
    Initialize(ctx context.Context) error
    Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error)
    Dimensions() int
    Close() error
}
```

2. `internal/mcp/searcher.go`:
```go
type EmbeddingProvider interface {
    Embed(ctx context.Context, texts []string, mode string) ([][]float32, error)
    Dimensions() int
    Close() error
}
```

**Problems:**
- Two interfaces for same purpose (created to avoid import cycle)
- MCP uses `mode string`, embed uses typed `mode EmbedMode`
- MCP interface missing `Initialize()` method
- Code duplication and potential drift

**Solution:**

Remove `mcp.EmbeddingProvider` and use `embed.Provider` everywhere:

1. **No Import Cycle**: `internal/embed` does not import `internal/mcp`, so `internal/mcp` can safely import `internal/embed`
2. **Type Safety**: All code uses `embed.EmbedMode` type instead of strings
3. **Consistency**: Indexer and MCP use identical interface

**Changes Required:**
- Delete `mcp.EmbeddingProvider` interface
- Update MCP searcher to use `embed.Provider`
- Update MCP call sites to use `embed.EmbedMode` instead of string literals
- Update MCP searcher constructor to accept `embed.Provider`

**Example:**
```go
// Before (MCP)
type vectorSearcher struct {
    provider mcp.EmbeddingProvider
}

func (s *vectorSearcher) Query(ctx context.Context, query string, ...) {
    embeddings, err := s.provider.Embed(ctx, []string{query}, "query") // string mode
}

// After (MCP)
import "github.com/mvp-joe/project-cortex/internal/embed"

type vectorSearcher struct {
    provider embed.Provider
}

func (s *vectorSearcher) Query(ctx context.Context, query string, ...) {
    embeddings, err := s.provider.Embed(ctx, []string{query}, embed.EmbedModeQuery) // typed mode
}
```

## Provider Naming Convention

**localProvider stays localProvider** - The name distinguishes between *deployment models* (local vs cloud), not transport or server implementation details.

**Naming Rationale:**
- `localProvider`: Connects to embedding service on local machine
- `openaiProvider`: (Future) Calls OpenAI Embeddings API
- `anthropicProvider`: (Future) Calls Anthropic Embeddings API
- `remoteProvider`: (Future) Calls custom remote embedding service

The provider interface remains `embed.Provider`, and the factory function `embed.NewProvider(config)` returns the appropriate implementation based on config. Implementation changes (HTTP → ConnectRPC, etc.) are transparent to callers.

## Dimension Validation

To prevent runtime errors from dimension mismatches, the MCP server validates that the provider's configured dimensions match the dimensions of indexed chunks in the cache.

**Scenario**: User changes config from 768 → 512 after indexing, or upgrades from 384d to 768d version without reindexing.

**Problem**: Existing chunks have 768d embeddings, new queries use 512d → vector search dimension mismatch error.

**Solution**: Validate dimensions on MCP searcher initialization:

```go
// internal/mcp/vector_searcher.go

func NewVectorSearcher(db *sql.DB, provider embed.Provider) (*vectorSearcher, error) {
    // Read stored embedding dimensions from cache metadata
    var storedDim int
    err := db.QueryRow("SELECT value FROM cache_metadata WHERE key = 'embedding_dimensions'").Scan(&storedDim)
    if err != nil {
        return nil, fmt.Errorf("failed to read stored embedding dimensions: %w", err)
    }

    // Validate provider dimensions match stored dimensions
    if storedDim != provider.Dimensions() {
        return nil, fmt.Errorf(
            "embedding dimension mismatch: indexed chunks use %dd embeddings, but provider is configured for %dd\n"+
            "Run 'cortex index' to reindex with new dimensions",
            storedDim, provider.Dimensions(),
        )
    }

    return &vectorSearcher{
        db:       db,
        provider: provider,
    }, nil
}
```

**Benefits:**
- Fail fast with clear error message on dimension mismatch
- User knows exactly what to do (run `cortex index`)
- Prevents confusing runtime errors during vector search
- No silent data corruption or incorrect results

## Data Model

### ConnectRPC API

```protobuf
// api/embed/v1/embed.proto
syntax = "proto3";

package embed.v1;

option go_package = "github.com/username/project-cortex/gen/embed/v1;embedv1";

// EmbedService provides text embedding generation.
service EmbedService {
  // Initialize prepares the embedding server for use.
  // Downloads models if needed, loads them into memory.
  // Streams progress updates during model download and loading.
  // Must be called before Embed() requests.
  rpc Initialize(InitializeRequest) returns (stream InitializeProgress);

  // Embed generates embeddings for one or more texts.
  // Batching is handled automatically via EmbedBatch.
  rpc Embed(EmbedRequest) returns (EmbedResponse);

  // Health returns server health and uptime statistics.
  // Used by EnsureEmbedServer for startup verification.
  rpc Health(HealthRequest) returns (HealthResponse);
}

message EmbedRequest {
  // Texts to embed (batched internally for efficiency).
  repeated string texts = 1;

  // Mode is "query" or "passage" (currently ignored, reserved for future).
  string mode = 2;
}

message EmbedResponse {
  // Embeddings parallel to request.texts (same order).
  repeated Embedding embeddings = 1;

  // Dimensions of each embedding vector.
  int32 dimensions = 2;

  // Server-side inference time in milliseconds.
  int64 inference_time_ms = 3;
}

message Embedding {
  // Embedding vector (length = dimensions from response).
  repeated float values = 1;
}

message HealthRequest {}

message HealthResponse {
  // True if server is accepting requests.
  bool healthy = 1;

  // Seconds since server started.
  int64 uptime_seconds = 2;

  // Milliseconds since last Embed() request.
  int64 last_request_ms_ago = 3;
}

message InitializeRequest {}

message InitializeProgress {
  // Current initialization status.
  // Values: "checking", "downloading", "loading", "ready"
  string status = 1;

  // Download progress percentage (0-100).
  // Only set when status = "downloading".
  int32 download_percent = 2;

  // Human-readable status message.
  // Examples:
  // - "Checking for models..."
  // - "Downloading embedding models (174MB)..."
  // - "Loading ONNX model into memory..."
  // - "Ready to generate embeddings"
  string message = 3;
}
```

### Go Server Implementation

```go
// internal/embed/daemon/server.go

type Server struct {
    model *onnx.EmbeddingModel  // Thread-safe, no mutex needed (nil until Initialize called)

    // Only mutex is for idle timeout tracking
    lastRequestMu sync.RWMutex
    lastRequest   time.Time

    idleTimeout   time.Duration  // 10 * time.Minute
    dimensions    int            // 768 or configurable
    startTime     time.Time
}

func NewServer(ctx context.Context, dimensions int) (*Server, error) {
    s := &Server{
        model:       nil,  // Model loaded during Initialize RPC
        lastRequest: time.Now(),
        idleTimeout: 10 * time.Minute,
        dimensions:  dimensions,
        startTime:   time.Now(),
    }

    // Start idle timeout monitor
    go s.monitorIdle(ctx)

    return s, nil
}

func (s *Server) Initialize(ctx context.Context, req *connect.Request[embedv1.InitializeRequest], stream *connect.ServerStream[embedv1.InitializeProgress]) error {
    modelDir := filepath.Join(os.Getenv("HOME"), ".cortex", "onnx")

    // Check if models already exist
    stream.Send(&embedv1.InitializeProgress{
        Status:  "checking",
        Message: "Checking for ONNX models...",
    })

    modelsExist := onnx.ModelsExist(modelDir)

    if !modelsExist {
        // Download models with progress updates
        stream.Send(&embedv1.InitializeProgress{
            Status:  "downloading",
            Message: "Downloading embedding models (174MB)...",
            DownloadPercent: 0,
        })

        progressCallback := func(percent int) {
            stream.Send(&embedv1.InitializeProgress{
                Status:          "downloading",
                Message:         fmt.Sprintf("Downloading embedding models (174MB)..."),
                DownloadPercent: int32(percent),
            })
        }

        if err := onnx.DownloadModels(ctx, modelDir, progressCallback); err != nil {
            return fmt.Errorf("model download failed: %w", err)
        }
    }

    // Load models into memory
    stream.Send(&embedv1.InitializeProgress{
        Status:  "loading",
        Message: "Loading ONNX model into memory...",
    })

    model, err := onnx.NewEmbeddingModel(
        filepath.Join(modelDir, "model_q4.onnx"),
        filepath.Join(modelDir, "tokenizer.model"),
    )
    if err != nil {
        return fmt.Errorf("failed to load model: %w", err)
    }

    s.model = model

    // Send ready status
    stream.Send(&embedv1.InitializeProgress{
        Status:  "ready",
        Message: "Ready to generate embeddings",
    })

    return nil
}

func (s *Server) Embed(ctx context.Context, req *connect.Request[embedv1.EmbedRequest]) (*connect.Response[embedv1.EmbedResponse], error) {
    // Reset idle timer (only mutex in hot path)
    s.lastRequestMu.Lock()
    s.lastRequest = time.Now()
    s.lastRequestMu.Unlock()

    start := time.Now()

    // Call ONNX model (thread-safe, no locking)
    embeddings, err := s.model.EmbedBatch(req.Msg.Texts)
    if err != nil {
        return nil, fmt.Errorf("inference failed: %w", err)
    }

    // Apply Matryoshka truncation if needed
    if s.dimensions < 768 {
        for i := range embeddings {
            embeddings[i] = onnx.TruncateEmbedding(embeddings[i], s.dimensions)
        }
    }

    inferenceTime := time.Since(start).Milliseconds()

    // Convert to protobuf
    resp := &embedv1.EmbedResponse{
        Embeddings:       make([]*embedv1.Embedding, len(embeddings)),
        Dimensions:       int32(s.dimensions),
        InferenceTimeMs:  inferenceTime,
    }

    for i, emb := range embeddings {
        resp.Embeddings[i] = &embedv1.Embedding{Values: emb}
    }

    return connect.NewResponse(resp), nil
}

func (s *Server) Health(ctx context.Context, req *connect.Request[embedv1.HealthRequest]) (*connect.Response[embedv1.HealthResponse], error) {
    s.lastRequestMu.RLock()
    lastReq := s.lastRequest
    s.lastRequestMu.RUnlock()

    resp := &embedv1.HealthResponse{
        Healthy:            true,
        UptimeSeconds:      int64(time.Since(s.startTime).Seconds()),
        LastRequestMsAgo:   time.Since(lastReq).Milliseconds(),
    }

    return connect.NewResponse(resp), nil
}

func (s *Server) monitorIdle(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            s.lastRequestMu.RLock()
            idleDuration := time.Since(s.lastRequest)
            s.lastRequestMu.RUnlock()

            if idleDuration > s.idleTimeout {
                log.Printf("Idle timeout (%.0fm), shutting down...", s.idleTimeout.Minutes())
                os.Exit(0)  // Clean exit
            }

        case <-ctx.Done():
            return
        }
    }
}
```

### ONNX Model Wrapper

```go
// internal/embed/onnx/model.go

type EmbeddingModel struct {
    session   *onnxruntime.DynamicAdvancedSession
    tokenizer *sentencepiece.Processor
}

func NewEmbeddingModel(onnxPath, tokenizerPath string) (*EmbeddingModel, error) {
    // Load tokenizer (pure Go)
    tokenizer, err := sentencepiece.NewProcessorFromPath(tokenizerPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load tokenizer: %w", err)
    }

    // Get model I/O info
    inputs, outputs, err := onnxruntime.GetInputOutputInfo(onnxPath)
    if err != nil {
        return nil, fmt.Errorf("failed to get model info: %w", err)
    }

    // Extract names
    inputNames := make([]string, len(inputs))
    outputNames := make([]string, len(outputs))
    for i := range inputs {
        inputNames[i] = inputs[i].Name
    }
    for i := range outputs {
        outputNames[i] = outputs[i].Name
    }

    // Create ONNX session
    session, err := onnxruntime.NewDynamicAdvancedSession(
        onnxPath,
        inputNames,
        outputNames,
        nil,
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create session: %w", err)
    }

    return &EmbeddingModel{
        session:   session,
        tokenizer: tokenizer,
    }, nil
}

func (m *EmbeddingModel) EmbedBatch(texts []string) ([][]float32, error) {
    // Tokenize all texts
    allTokens := make([][]int64, len(texts))
    maxLen := 0

    for i, text := range texts {
        tokens := m.tokenizer.Encode(text)
        tokenIDs := make([]int64, len(tokens))
        for j, tok := range tokens {
            tokenIDs[j] = int64(tok.ID)
        }
        allTokens[i] = tokenIDs
        if len(tokenIDs) > maxLen {
            maxLen = len(tokenIDs)
        }
    }

    // Pad to maxLen and create attention mask
    batchSize := len(texts)
    inputIDs := make([]int64, batchSize*maxLen)
    attentionMask := make([]int64, batchSize*maxLen)

    for i, tokens := range allTokens {
        for j := 0; j < maxLen; j++ {
            idx := i*maxLen + j
            if j < len(tokens) {
                inputIDs[idx] = tokens[j]
                attentionMask[idx] = 1
            } else {
                inputIDs[idx] = 0
                attentionMask[idx] = 0
            }
        }
    }

    // Create ONNX tensors
    inputShape := onnxruntime.NewShape(int64(batchSize), int64(maxLen))

    inputTensor, err := onnxruntime.NewTensor(inputShape, inputIDs)
    if err != nil {
        return nil, err
    }
    defer inputTensor.Destroy()

    attentionTensor, err := onnxruntime.NewTensor(inputShape, attentionMask)
    if err != nil {
        return nil, err
    }
    defer attentionTensor.Destroy()

    // Run inference (thread-safe)
    inputs := []onnxruntime.Value{inputTensor, attentionTensor}
    outputs := []onnxruntime.Value{nil, nil}

    if err := m.session.Run(inputs, outputs); err != nil {
        return nil, fmt.Errorf("inference failed: %w", err)
    }

    // Extract sentence embeddings (output[1])
    resultTensor, ok := outputs[1].(*onnxruntime.Tensor[float32])
    if !ok {
        return nil, fmt.Errorf("unexpected output type")
    }
    defer resultTensor.Destroy()

    allEmbeddings := resultTensor.GetData()
    embeddingDim := 768

    // Split batched embeddings
    result := make([][]float32, batchSize)
    for i := 0; i < batchSize; i++ {
        start := i * embeddingDim
        end := start + embeddingDim
        result[i] = allEmbeddings[start:end]
    }

    return result, nil
}

func (m *EmbeddingModel) Destroy() error {
    if m.session != nil {
        return m.session.Destroy()
    }
    return nil
}

// TruncateEmbedding implements Matryoshka Representation Learning.
// Truncates and re-normalizes embeddings to smaller dimensions.
func TruncateEmbedding(embedding []float32, targetDim int) []float32 {
    if targetDim >= len(embedding) {
        return embedding
    }

    truncated := embedding[:targetDim]

    // Re-normalize
    var norm float32
    for _, v := range truncated {
        norm += v * v
    }
    norm = float32(math.Sqrt(float64(norm)))

    result := make([]float32, targetDim)
    if norm > 0 {
        for i := 0; i < targetDim; i++ {
            result[i] = truncated[i] / norm
        }
    }

    return result
}
```

### Client Provider Implementation

**Update existing `internal/embed/local.go`** - change transport from HTTP to ConnectRPC:

```go
// internal/embed/local.go

type localProvider struct {
    client     embedv1connect.EmbedServiceClient
    dimensions int
}

func (p *localProvider) Initialize(ctx context.Context) error {
    // Auto-start embed server if not running
    if err := daemon.EnsureEmbedServer(ctx); err != nil {
        return fmt.Errorf("failed to ensure embed server: %w", err)
    }

    // Create ConnectRPC client over Unix socket
    sockPath := daemon.GetEmbedSocketPath()
    httpClient := &http.Client{
        Transport: &http.Transport{
            DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
                return net.Dial("unix", sockPath)
            },
        },
    }

    p.client = embedv1connect.NewEmbedServiceClient(
        httpClient,
        "http://unix",  // URL doesn't matter for Unix socket
    )

    // Call Initialize RPC to ensure models are downloaded and loaded
    // This streams progress updates during model download
    stream, err := p.client.Initialize(ctx, connect.NewRequest(&embedv1.InitializeRequest{}))
    if err != nil {
        return fmt.Errorf("failed to start initialization: %w", err)
    }

    // Stream progress updates to user
    for stream.Receive() {
        progress := stream.Msg()

        // Display status message
        if progress.DownloadPercent > 0 {
            fmt.Printf("\r%s (%d%%)...", progress.Message, progress.DownloadPercent)
        } else {
            fmt.Printf("%s\n", progress.Message)
        }

        // Ready status indicates completion
        if progress.Status == "ready" {
            fmt.Println() // New line after progress
            break
        }
    }

    if err := stream.Err(); err != nil {
        return fmt.Errorf("initialization failed: %w", err)
    }

    return nil
}

func (p *localProvider) Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {
    // Resurrection pattern: Try RPC directly, only resurrect on connection failure
    // This handles cases where daemon exited due to idle timeout:
    // - MCP server idle for hours, user asks question
    // - Watch mode no changes for 10+ minutes, then file updated
    // - User runs `cortex index`, waits, runs again
    //
    // Avoids preemptive health checks (wasted RPC calls during active sessions)
    // and race conditions (health check passes, daemon exits before Embed() call)

    resp, err := p.client.Embed(ctx, connect.NewRequest(&embedv1.EmbedRequest{
        Texts: texts,
        Mode:  string(mode),  // Ignored by server (reserved for future)
    }))

    // Only resurrect on connection errors (daemon dead or not started)
    if err != nil && isConnectionError(err) {
        if err := daemon.EnsureEmbedServer(ctx); err != nil {
            return nil, fmt.Errorf("failed to resurrect embed server: %w", err)
        }

        // Retry once after resurrection
        resp, err = p.client.Embed(ctx, connect.NewRequest(&embedv1.EmbedRequest{
            Texts: texts,
            Mode:  string(mode),
        }))
    }

    if err != nil {
        return nil, err
    }

    // Convert from protobuf
    embeddings := make([][]float32, len(resp.Msg.Embeddings))
    for i, emb := range resp.Msg.Embeddings {
        embeddings[i] = emb.Values
    }

    // Truncation already applied server-side if dimensions < 768
    return embeddings, nil
}

// isConnectionError checks if the error indicates daemon is not reachable
func isConnectionError(err error) bool {
    if err == nil {
        return false
    }

    // Check for common connection errors
    errStr := err.Error()
    return strings.Contains(errStr, "connection refused") ||
           strings.Contains(errStr, "no such file or directory") ||  // Socket doesn't exist
           strings.Contains(errStr, "broken pipe")
}

func (p *localProvider) Dimensions() int {
    return p.dimensions
}

func (p *localProvider) Close() error {
    // Server manages its own lifecycle (idle timeout)
    return nil
}
```

### Daemon Lifecycle Management

```go
// internal/embed/daemon/ensure.go

// EnsureEmbedServer ensures the embedding server is running.
// Safe to call concurrently from multiple processes.
// Follows same pattern as indexer daemon's EnsureDaemon().
func EnsureEmbedServer(ctx context.Context) error {
    sockPath := GetEmbedSocketPath()  // ~/.cortex/embed.sock

    // Fast path: server already running and healthy
    if isEmbedServerHealthy(ctx, sockPath) {
        return nil
    }

    // Acquire exclusive lock to prevent concurrent starts
    lockPath := filepath.Join(cortexDir, "embed.lock")
    lock := flock.New(lockPath)

    if err := lock.Lock(); err != nil {
        return fmt.Errorf("failed to acquire embed lock: %w", err)
    }
    defer lock.Unlock()

    // Re-check after acquiring lock (another process may have started it)
    if isEmbedServerHealthy(ctx, sockPath) {
        return nil
    }

    // Remove stale socket if exists
    os.Remove(sockPath)

    // Start daemon process (detached from parent)
    cmd := exec.Command("cortex", "embed", "start")
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,  // Create new process group
    }

    if err := cmd.Start(); err != nil {
        return fmt.Errorf("failed to start embed server: %w", err)
    }

    // Wait for server to become healthy (up to 5 seconds)
    return waitForEmbedHealthy(ctx, sockPath, 5*time.Second)
}

func isEmbedServerHealthy(ctx context.Context, sockPath string) bool {
    // Check socket exists
    if _, err := os.Stat(sockPath); err != nil {
        return false
    }

    // Connect and call Health RPC
    httpClient := &http.Client{
        Transport: &http.Transport{
            DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
                return net.Dial("unix", sockPath)
            },
        },
    }

    client := embedv1connect.NewEmbedServiceClient(httpClient, "http://unix")

    ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
    defer cancel()

    _, err := client.Health(ctx, connect.NewRequest(&embedv1.HealthRequest{}))
    return err == nil
}

func waitForEmbedHealthy(ctx context.Context, sockPath string, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if isEmbedServerHealthy(ctx, sockPath) {
                return nil
            }
        case <-ctx.Done():
            return fmt.Errorf("embed server failed to start within %v", timeout)
        }
    }
}

func GetEmbedSocketPath() string {
    // Allow override via environment variable
    if override := os.Getenv("CORTEX_EMBED_SOCKET"); override != "" {
        return override
    }

    // Use cross-platform home directory
    homeDir, err := os.UserHomeDir()
    if err != nil {
        // Fallback to current directory if home cannot be determined
        homeDir = "."
    }

    cortexDir := filepath.Join(homeDir, ".cortex")

    // Ensure directory exists
    os.MkdirAll(cortexDir, 0755)

    return filepath.Join(cortexDir, "embed.sock")
}
```

### CLI Command

```go
// cmd/cortex/embed_start.go

func runEmbedStart(ctx context.Context) error {
    sockPath := daemon.GetEmbedSocketPath()

    // Check if already running (self-protection)
    if daemon.IsEmbedServerHealthy(ctx, sockPath) {
        fmt.Println("Embedding server already running")
        return nil
    }

    // Remove stale socket
    os.Remove(sockPath)

    // Ensure models downloaded
    modelDir := filepath.Join(os.Getenv("HOME"), ".cortex", "onnx")
    if err := onnx.EnsureModels(ctx, modelDir); err != nil {
        return fmt.Errorf("failed to download models: %w", err)
    }

    // Get dimensions from config (default 768)
    dimensions := config.GetInt("embedding.dimensions", 768)

    // Create server
    srv, err := daemon.NewServer(ctx, modelDir, dimensions)
    if err != nil {
        return fmt.Errorf("failed to create server: %w", err)
    }

    // Start Unix socket listener
    listener, err := net.Listen("unix", sockPath)
    if err != nil {
        return fmt.Errorf("failed to listen on socket: %w", err)
    }
    defer listener.Close()

    // Set socket permissions (user-only)
    os.Chmod(sockPath, 0600)

    // Register ConnectRPC handlers
    mux := http.NewServeMux()
    path, handler := embedv1connect.NewEmbedServiceHandler(srv)
    mux.Handle(path, handler)

    // Start HTTP server over Unix socket
    httpServer := &http.Server{
        Handler: mux,
    }

    // Graceful shutdown on signal
    go func() {
        <-ctx.Done()
        httpServer.Shutdown(context.Background())
        srv.Close()
    }()

    log.Printf("Embedding server started (socket: %s)", sockPath)

    return httpServer.Serve(listener)
}
```

## Configuration

### Embedding Dimensions

Default: **768** (up from 384)

Configurable via `.cortex/config.yml`:

```yaml
embedding:
  provider: "local"      # Still "local" (implementation changes transparently)
  dimensions: 768        # or 512, 256, 128 (Matryoshka truncation)
```

Environment variable override:
```bash
CORTEX_EMBEDDING_DIMENSIONS=512 cortex index
```

### Model Storage Location

Models stored in `~/.cortex/onnx/` (shared across all projects, not per-project).

Override via environment variable:
```bash
CORTEX_ONNX_MODEL_DIR=/custom/path cortex embed start
```

### Environment Variables

**CORTEX_EMBED_SOCKET**
- Override Unix socket path for daemon communication
- Default: `~/.cortex/embed.sock`
- Example: `CORTEX_EMBED_SOCKET=/tmp/cortex-embed.sock cortex index`
- Use case: Custom socket location, multi-user systems, testing

**CORTEX_EMBED_IDLE_TIMEOUT**
- Daemon idle timeout in seconds before automatic shutdown
- Default: `600` (10 minutes)
- Example: `CORTEX_EMBED_IDLE_TIMEOUT=1800 cortex embed start` (30 minutes)
- Use case: Adjust memory vs latency trade-off based on usage patterns

**CORTEX_EMBEDDING_DIMENSIONS**
- Embedding vector dimensions (Matryoshka truncation)
- Default: `768`
- Valid values: `768`, `512`, `256`, `128`
- Example: `CORTEX_EMBEDDING_DIMENSIONS=512 cortex index`
- Use case: Trade embedding quality for smaller vector storage

**CORTEX_ONNX_MODEL_DIR**
- Directory for ONNX model files
- Default: `~/.cortex/onnx/`
- Example: `CORTEX_ONNX_MODEL_DIR=/data/models cortex embed start`
- Use case: Custom model location, shared storage, testing

## Performance Characteristics

### Startup Time

**Cold start** (model not loaded):
- Model download: ~60s (one-time, 174MB)
- Model load: ~800ms
- Socket bind: <10ms
- **Total first run**: ~60-80s (download dominates)
- **Total subsequent runs**: <1s

**Warm start** (daemon already running):
- Health check: <10ms
- **Total**: <10ms (nearly instant)

### Inference Latency

Based on spike benchmarks:

| Batch Size | Avg Latency | Throughput |
|------------|-------------|------------|
| 1 text     | ~50ms       | 20 req/s   |
| 10 texts   | ~120ms      | 83 req/s   |
| 25 texts   | ~250ms      | 100 req/s  |

**Key insight**: Batching provides 4-5x throughput improvement.

### Memory Usage

**Daemon process**:
- Base Go runtime: ~10MB
- ONNX Runtime: ~50MB
- Loaded model: ~200MB
- **Total**: ~260MB

**Client overhead**: <1MB (just ConnectRPC client)

**Comparison to old Python approach**:
- Old: 300MB (Python + PyTorch, no models)
- New: 260MB (includes models)
- **Savings**: 40MB + no Python runtime distribution

### Idle Timeout Impact

**5-minute idle timeout**:
- Average cold start: <1s
- Typical indexing session: 2-10 minutes (no restarts)
- MCP query patterns: bursty (server stays alive during active coding)

**Memory trade-off**: Save 260MB when idle vs <1s latency hit on first request

## Migration Strategy

### Breaking Changes

**Incompatible embeddings**: 384d → 768d vectors require full reindex.

**Migration path**:
1. User updates `cortex` binary
2. First `cortex index` detects dimension mismatch
3. Automatic reindex with 768d embeddings
4. Old cache invalidated (SQLite schema tracks dimensions)

### Schema Migration

```go
// internal/storage/migration.go

func MigrateTo768d(db *sql.DB) error {
    // Check current dimension
    var currentDim int
    err := db.QueryRow("SELECT value FROM cache_metadata WHERE key = 'embedding_dimensions'").Scan(&currentDim)
    if err != nil || currentDim == 768 {
        return nil  // Already migrated or fresh DB
    }

    log.Printf("Detected %dd embeddings, migrating to 768d (requires reindex)", currentDim)

    // Drop vector index (dimensions changed)
    db.Exec("DROP TABLE IF EXISTS vec_chunks")

    // Update metadata
    db.Exec("UPDATE cache_metadata SET value = '768' WHERE key = 'embedding_dimensions'")

    // Recreate vector index with new dimensions
    return CreateVectorIndex(db, 768)
}
```

### Backward Compatibility

**Config field `provider: "local"`** remains unchanged (transparent implementation swap).

**Factory unchanged**:
```go
// internal/embed/factory.go

func NewProvider(config Config) (Provider, error) {
    switch config.Provider {
    case "local", "":
        return newLocalProvider()

    case "mock":
        return newMockProvider(), nil

    default:
        return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
    }
}
```

**User experience**: No config changes required, just update binary and reindex.

## Testing Strategy

### Unit Tests

**ONNX model wrapper** (`internal/embed/onnx/model_test.go`):
- Tokenization correctness
- Batch processing (padding, attention masks)
- Matryoshka truncation and normalization
- Memory cleanup (tensor destruction)

**Idle timeout** (`internal/embed/daemon/server_test.go`):
- LastRequest tracking (thread-safe)
- Timeout detection logic
- No premature exits

**Provider interface** (`internal/embed/connect_provider_test.go`):
- Implements Provider interface fully
- Dimensions() returns configured value
- Initialize() calls EnsureEmbedServer()

### Integration Tests

**Full daemon lifecycle** (`internal/embed/daemon/lifecycle_test.go`):
- Start server → embed request → verify result → idle shutdown
- Concurrent EnsureEmbedServer() calls (lock safety)
- Stale socket recovery (connection refused → cleanup → restart)
- Double-start protection (already running → exit cleanly)

**ConnectRPC communication** (`internal/embed/daemon/rpc_test.go`):
- Embed() RPC with various batch sizes
- Health() RPC returns accurate stats
- Error handling (invalid input, server crashed)

**Model download** (`internal/embed/onnx/downloader_test.go`):
- Platform detection (darwin-arm64, linux-amd64, etc.)
- Download progress reporting
- Retry logic on network failure
- Checksum validation

### End-to-End Tests

**Indexer integration** (`tests/e2e/embedding_test.go`):
- `cortex index` auto-starts embed server
- Generates 768d embeddings
- Writes to SQLite correctly
- Verify vector search works

**MCP integration** (`tests/e2e/mcp_embedding_test.go`):
- `cortex mcp` uses existing embed server
- Multiple MCP instances share server
- Query embeddings match indexed chunks

**Cold start performance** (`tests/e2e/performance_test.go`):
- Measure model load time (<1s)
- Verify idle shutdown after 5min
- Measure restart latency (<1s)

## Removal of Python Infrastructure

### Files to Delete

**Binary and build system**:
- `cmd/cortex-embed/` (entire directory)
- `internal/embed/server/` (Python embedded data)
- `internal/embed/local.go` (HTTP client to Python server)
- `internal/embed/downloader.go` (Python binary downloader)

**Taskfile targets**:
- All `python:deps:*` tasks
- `build:embed` task
- `run:embed` task

**Dependencies**:
- Remove `github.com/kluctl/go-embed-python` from `go.mod`
- Remove `requirements.txt` (if exists)

### Files to Keep/Update

**Keep**:
- `internal/embed/provider.go` (interface unchanged)
- `internal/embed/factory.go` (unchanged)
- `internal/embed/local.go` (update to use ConnectRPC instead of HTTP)
- `internal/embed/mock.go` (testing)
- `internal/embed/batched.go` (batch optimizer wrapper)

**Update**:
- `.cortex/config.yml`: Change `dimensions: 384` → `768`
- `internal/storage/schema.go`: Change default dimensions to 768
- All test fixtures: Update mock embeddings to 768 dimensions

### Documentation Updates

**README.md**:
- Remove Python setup instructions
- Add "Pure Go, zero dependencies" messaging
- Update quick start (no cortex-embed binary)

**CLAUDE.md**:
- Update architecture section (ONNX instead of Python)
- Change "Embedding Provider Interface" section
- Update "Dependencies" list

**Build documentation**:
- Remove cross-compilation complexity notes
- Update binary size expectations (~8-10MB)

## Release Strategy

### GitHub Release Artifacts

**Per-platform model archives**:
```
cortex-onnx-darwin-arm64-v1.0.0.tar.gz    (~174MB)
cortex-onnx-darwin-amd64-v1.0.0.tar.gz    (~174MB)
cortex-onnx-linux-amd64-v1.0.0.tar.gz     (~174MB)
cortex-onnx-linux-arm64-v1.0.0.tar.gz     (~174MB)
cortex-onnx-windows-amd64-v1.0.0.zip      (~174MB)
```

**Archive contents**:
```
onnxruntime.{dylib,so,dll}
model_q4.onnx
model_q4.onnx_data
tokenizer.model
```

**Single cortex binary** (all platforms):
```
cortex-darwin-arm64      (~8-10MB)
cortex-darwin-amd64      (~8-10MB)
cortex-linux-amd64       (~8-10MB)
cortex-linux-arm64       (~8-10MB)
cortex-windows-amd64.exe (~8-10MB)
```

**CI automation**:
- Build model archives from spike directory
- Upload to GitHub releases
- Tag with version (e.g., `v2.0.0`)
- Update download URLs in code to match version

### Version Bump

**Major version bump**: v1.x → v2.0.0

**Reason**: Breaking change (requires reindex)

**Changelog excerpt**:
```markdown
## v2.0.0 - Pure Go Embedding Server

### Breaking Changes
- **Embedding dimensions increased from 384 to 768** (requires reindex)
- Python cortex-embed binary removed (no manual installation needed)
- First run downloads ~174MB models automatically

### New Features
- Pure Go implementation (no Python runtime)
- Sub-second cold start (<1s)
- Aggressive idle timeout (5min) saves memory
- Smaller total distribution (~174MB models vs 300MB Python+PyTorch)

### Migration Guide
1. Update cortex binary: `brew upgrade cortex` (or download from releases)
2. Run `cortex index` in your project (auto-downloads models, reindexes)
3. Done! Embedding server auto-starts on demand
```

## Non-Goals

This specification does NOT cover:

- **Multi-model support**: Single Gemma model only (no model switching)
- **Cloud provider fallback**: No OpenAI/Anthropic embedding APIs (future)
- **Query/passage mode differentiation**: Single model for both (mode parameter ignored)
- **Distributed embedding**: No network protocol (Unix socket only, single-machine)
- **GPU acceleration**: CPU-only ONNX inference (quantized model is fast enough)
- **Model fine-tuning**: Pre-trained model only
- **Embedding caching**: Recompute on every request (stateless server)
- **Multi-user isolation**: Single-user daemon (no auth, no multi-tenancy)
- **Custom model loading**: Fixed model path, no plugin system

## Risk Mitigation

### Risk: ONNX Model Quality Differs from PyTorch

**Mitigation**:
- Validate embeddings match PyTorch reference within tolerance
- Test on real queries from existing projects
- Compare search quality before release

**Acceptance criteria**: Cosine similarity >0.95 for sample texts

### Risk: Cross-Platform ONNX Runtime Issues

**Mitigation**:
- Test on all supported platforms (macOS Intel/ARM, Linux x64/ARM, Windows)
- Bundle platform-specific onnxruntime libs in releases
- Clear error messages if platform unsupported

**Fallback**: Document manual installation for exotic platforms

### Risk: Model Download Failures

**Mitigation**:
- Retry logic with exponential backoff (3 attempts)
- Resume partial downloads (HTTP Range requests)
- Clear error messages with download URL
- Document manual download option

**User experience**: Progress bar during download, informative errors

### Risk: Breaking Change Forces Reindex

**Mitigation**:
- Automatic detection of dimension mismatch
- Clear messaging: "Upgrading to 768d embeddings, reindexing..."
- Fast reindex on fresh cache miss (no data loss)

**User experience**: Transparent, happens automatically on first `cortex index`

### Risk: Idle Timeout Configuration

**Default**: 10 minutes (balances memory savings with avoiding frequent restarts during coding pauses)

**Mitigation**:
- Make timeout configurable via environment variable: `CORTEX_EMBED_IDLE_TIMEOUT=600` (seconds)
- Document that <1s restart justifies automatic shutdown
- Monitor user feedback post-release

**Tuning**: Can increase to 15-30min if 10min proves too short for typical workflows

## Implementation Checklist

### Phase 0: Interface Unification (Prerequisite)
- [ ] Remove `mcp.EmbeddingProvider` interface from `internal/mcp/searcher.go`
- [ ] Update MCP searcher to import and use `embed.Provider`
- [ ] Update MCP call sites to use `embed.EmbedMode` instead of string literals
- [ ] Update MCP searcher constructor to accept `embed.Provider` parameter
- [ ] Update all MCP tests to use `embed.Provider` and `embed.EmbedMode`
- [ ] Verify no import cycles introduced
- [ ] Run all tests to ensure type changes work correctly

### Phase 1: ONNX Core Infrastructure
- [ ] Create `internal/embed/onnx/` package structure
- [ ] Port `EmbeddingModel` from spike (with tests)
- [ ] Add `TruncateEmbedding()` for Matryoshka support (with tests)
- [ ] Create model downloader for platform-specific archives with progress callback (with tests)
- [ ] Add `ModelsExist()` helper to check if models are downloaded

### Phase 2: ConnectRPC API
- [ ] Create `api/embed/v1/embed.proto` protobuf schema with Initialize, Embed, and Health RPCs
- [ ] Add `InitializeRequest` and `InitializeProgress` (streaming) messages
- [ ] Generate Go code with `buf generate` to `gen/embed/v1/`
- [ ] Add protobuf generation to CI/build pipeline

### Phase 3: Embedding Daemon
- [ ] Implement daemon server struct (without model loading in constructor) (with tests)
- [ ] Add idle timeout monitoring (with tests)
- [ ] Implement Initialize handler with streaming progress (downloads and loads models) (with tests)
- [ ] Implement Embed handler (with tests)
- [ ] Implement Health handler (with tests)
- [ ] Add `cortex embed start` CLI command
- [ ] Implement `EnsureEmbedServer()` with file locking (with tests)

### Phase 4: Client Provider
- [ ] Update `localProvider` to use ConnectRPC client instead of HTTP (with tests)
- [ ] Update `Initialize()` method to call Initialize RPC with streaming progress (with tests)
- [ ] Update `Embed()` method with on-error resurrection pattern (try RPC, resurrect on connection error, retry once)
- [ ] Add `isConnectionError()` helper function to detect connection failures
- [ ] Add integration test (indexer → embed server with model download → embeddings)
- [ ] Add integration test for resurrection (idle timeout → query → auto-restart)
- [ ] Add integration test for streaming progress (first-time model download displays progress)

### Phase 5: Configuration and Schema
- [ ] Update default dimensions to 768 in config
- [ ] Implement schema migration (384d → 768d detection)
- [ ] Update `CreateVectorIndex()` to accept dynamic dimensions
- [ ] Add dimension validation in MCP searcher initialization (with tests)
- [ ] Update socket path to use `os.UserHomeDir()` and support `CORTEX_EMBED_SOCKET` override
- [ ] Add support for `CORTEX_EMBED_IDLE_TIMEOUT` environment variable in daemon
- [ ] Update all config files and documentation with new environment variables

### Phase 6: Testing
- [ ] Unit tests for ONNX wrapper (tokenization, batching, truncation)
- [ ] Integration tests for daemon lifecycle (start, request, shutdown)
- [ ] E2E tests for indexer + MCP integration
- [ ] Performance benchmarks (cold start, throughput)

### Phase 7: Cleanup
- [ ] Remove `cmd/cortex-embed/` directory
- [ ] Remove `internal/embed/server/` directory
- [ ] Remove `internal/embed/downloader.go` (binary download no longer needed)
- [ ] Remove Python-related Taskfile targets
- [ ] Remove `go-embed-python` dependency from `go.mod`
- [ ] Update all 384 references to 768 in tests

### Phase 8: Documentation and Release
- [ ] Update README.md (remove Python setup)
- [ ] Update CLAUDE.md (architecture section)
- [ ] Write migration guide for v2.0.0
- [ ] Create GitHub release artifacts (model archives per platform)
- [ ] Update CI to build and upload model archives
- [ ] Create changelog for v2.0.0 release
