# Embedding Server

## What is it?

The embedding server provides semantic search capabilities for Project Cortex. It runs as part of the `cortex` binary using the `cortex embed start` command.

## Why does it exist?

The `cortex` CLI is designed to run as a project-specific MCP Server using stdio. You'll typically have one or more `cortex` instances per project (possibly multiple per Claude Code session).

The embedding server is a **shared service** that any number of `cortex` processes can call. This architecture means:
- The embedding model is loaded into memory only once
- Multiple projects can share the same embedding server
- Both `cortex index` and `cortex mcp` use the same embedding service
- The main `cortex` CLI remains lightweight for fast startup

**Users never manually download runtime files or models.** The `cortex` CLI automatically downloads and manages them as needed.

## How does it work?

### Runtime and Model Files

`cortex embed start` uses Rust FFI with tract-onnx (pure Rust ONNX inference) and BGE embedding model:
- Rust FFI library (libembeddings_ffi.a) compiled into cortex binary
- BGE-small-en-v1.5 model files (~100MB)
- Model files downloaded automatically on first use
- Stored in `~/.cortex/models/bge/`

### Startup Flow

1. User runs `cortex index` or `cortex mcp`
2. Cortex checks if embedding server is running (Unix socket health check at ~/.cortex/embed.sock)
3. If not running, checks for model files in `~/.cortex/models/bge/`
4. Downloads missing model files automatically
5. Starts embedding server: `cortex embed start`
6. Server loads Rust FFI model and initializes tract-onnx runtime
7. Listens on Unix socket (~/.cortex/embed.sock) for ConnectRPC requests
8. Cortex makes ConnectRPC calls to the embedding service

### API

ConnectRPC service on Unix socket (~/.cortex/embed.sock):

**Initialize RPC** (server stream)
```protobuf
message InitializeRequest {}

message InitializeProgress {
  string status = 1;          // "checking", "downloading", "loading", "ready"
  string message = 2;
  int32 download_percent = 3;
}
```

**Embed RPC**
```protobuf
message EmbedRequest {
  repeated string texts = 1;
}

message EmbedResponse {
  repeated Embedding embeddings = 1;
  int32 dimensions = 2;
  int64 inference_time_ms = 3;
}

message Embedding {
  repeated float values = 1;
}
```

Returns 384-dimensional embeddings ready for cosine similarity.

**Health RPC**
```protobuf
message HealthRequest {}

message HealthResponse {
  bool healthy = 1;
  int64 uptime_seconds = 2;
  int64 last_request_ms_ago = 3;
}
```

## Embedding Model

Uses BAAI/bge-small-en-v1.5 model optimized for semantic search:
- 384 dimensions (balance of quality and efficiency)
- 512 token context window
- Excellent performance on code and documentation retrieval
- ~100MB download, cached locally after first use

## Architecture Decisions

### Why Local Embeddings?

- Zero cost for local development
- No API keys needed
- Works offline
- Privacy - code never leaves the machine
- Lower latency for large codebases

Cloud embedding APIs (OpenAI, Anthropic) are supported via configuration for users who prefer them.

### Why Daemon Architecture?

- Single embedding model instance shared across multiple projects
- Faster than loading model for each indexing operation
- Memory efficient - model loaded once, not per-project
- Process isolation - embedding server crash doesn't crash indexer/MCP server

### Why ConnectRPC over Unix Sockets?

- Fast binary protocol for embedding vectors
- Unix socket provides better security and performance than TCP
- No port conflicts or firewall configuration needed
- Built-in connection management and health checking
- Streaming support for initialization progress
- Better performance than HTTP/JSON for numerical data

## Platform Support

Each platform gets appropriate runtime libraries:
- macOS Apple Silicon (darwin-arm64)
- macOS Intel (darwin-amd64)
- Linux x64 (linux-amd64)
- Linux ARM64 (linux-arm64)
- Windows x64 (windows-amd64)

Downloads happen automatically on first use based on detected platform.

## Storage Locations

### User Home Directory (`~/.cortex/`)

```
~/.cortex/
├── embed.sock              # Unix socket for ConnectRPC communication
├── embed.pid               # PID file for daemon singleton
└── models/
    └── bge/                # BGE embedding model files (~100MB)
        ├── model.onnx
        └── tokenizer.json
```

**Storage requirements:**
- Embedding model: ~100MB
- **Total**: ~100MB (one-time, shared across all projects)

Files are downloaded automatically on first use of `cortex index` or `cortex mcp`.

## Environment Variables

Control storage locations with environment variables:

- `CORTEX_MODEL_DIR`: Override model directory (default: `~/.cortex/models/`)

Example:
```bash
export CORTEX_MODEL_DIR=/opt/cortex/models
cortex embed start
```

## Development

For contributors working on embedding functionality:

```bash
# Start embedding server manually
cortex embed start

# Test with different model directory
CORTEX_MODEL_DIR=/tmp/models cortex embed start

# View server logs
cortex embed start --log-level debug

# Stop server
cortex embed stop
```

## Implementation Notes

### Model Loading

To avoid confusing users with repeated download messages:
- Check if model exists in `~/.cortex/models/bge/`
- First run: Show "Downloading embedding model (~100MB)..."
- Subsequent runs: Silent load, just "Loading model..." → "✓ Model ready"
- Download progress shown during first-time setup via Initialize RPC stream

### Startup Timeout

Client uses timeout with health checks to allow for:
- First-run model download (~20-40s depending on connection)
- Model loading into memory (~5-10s)
- tract-onnx runtime initialization (~2-5s)

### Lifecycle Management

- Server runs as background process (daemon mode)
- Health checks every 500ms during startup
- Automatic restart if server crashes
- Graceful shutdown on SIGTERM/SIGINT

## Future Enhancements

### Remote Embedding Providers
- **OpenAI API**: Use `text-embedding-3-small` or `text-embedding-3-large` via API key
- **Anthropic API**: Support for Anthropic's embedding models when available
- **Other providers**: Cohere, Voyage AI, or any OpenAI-compatible embedding endpoint
- **Benefits**: No local files needed, potentially better quality, pay-as-you-go pricing

### Performance Optimizations
- **Thermal-friendly threading**: Currently using 2 threads with 150ms batch delays for M-series Macs
- **Batch processing improvements**: Optimize rayon thread pool configuration per platform
- **Model caching**: Keep model warm across daemon restarts

### Model Options
- Additional embedding models (larger/smaller trade-offs)
- User-provided custom models
- Automatic model selection based on use case
