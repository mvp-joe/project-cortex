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

`cortex embed start` uses ONNX Runtime with a quantized embedding model:
- ONNX Runtime libraries (~50MB, platform-specific)
- Gemma-based embedding model files (~200MB)
- All files downloaded automatically on first use
- Stored in `~/.cortex/lib/` (runtime) and `~/.cortex/models/gemma/` (model)

### Startup Flow

1. User runs `cortex index` or `cortex mcp`
2. Cortex checks if embedding server is running (gRPC health check on port 50051)
3. If not running, checks for required files in `~/.cortex/`
4. Downloads missing files automatically (runtime libs + model)
5. Starts embedding server: `cortex embed start`
6. Server loads ONNX runtime and model into memory
7. Listens on gRPC (port 50051) for embedding requests
8. Cortex makes gRPC calls to `/embed.EmbeddingService/Embed` endpoint

### API

gRPC service on port 50051:

**Embed RPC**
```protobuf
message EmbedRequest {
  repeated string texts = 1;
  string mode = 2;  // "query" or "passage"
}

message EmbedResponse {
  repeated Embedding embeddings = 1;
}

message Embedding {
  repeated float values = 1;
}
```

Returns normalized 384-dimensional embeddings ready for cosine similarity.

**Health Check** - Standard gRPC health check service

## Embedding Model

Uses a quantized Gemma-based model optimized for code and documentation:
- 384 dimensions (balance of quality and efficiency)
- 512 token context window
- Optimized for semantic search and retrieval
- ~200MB download, cached locally after first use

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

### Why gRPC?

- Fast binary protocol for embedding vectors
- Built-in connection management and health checking
- Streaming support for future batching optimizations
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
├── lib/                    # ONNX Runtime libraries (~50MB)
│   ├── darwin-arm64/
│   ├── darwin-amd64/
│   ├── linux-amd64/
│   └── windows-amd64/
└── models/
    └── gemma/              # Embedding model files (~200MB)
        ├── model.onnx
        ├── tokenizer.json
        └── config.json
```

**Storage requirements:**
- ONNX Runtime: ~50MB (one platform)
- Embedding model: ~200MB
- **Total**: ~250MB (one-time, shared across all projects)

Files are downloaded automatically on first use of `cortex index` or `cortex mcp`.

## Environment Variables

Control storage locations with environment variables:

- `CORTEX_LIB_DIR`: Override runtime library directory (default: `~/.cortex/lib/`)
- `CORTEX_MODEL_DIR`: Override model directory (default: `~/.cortex/models/gemma/`)

Example:
```bash
export CORTEX_LIB_DIR=/opt/cortex/lib
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
- Check if model exists in `~/.cortex/models/gemma/`
- First run: Show "Downloading embedding model (~200MB)..."
- Subsequent runs: Silent load, just "Loading model..." → "✓ Model ready"
- Download progress shown during first-time setup

### Startup Timeout

Client uses 2-minute timeout with 500ms health checks to allow for:
- First-run model download (~30-60s depending on connection)
- Model loading into memory (~10-20s)
- ONNX runtime initialization (~5-10s)

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

### Library Mode
- **Direct ONNX Runtime**: Embed ONNX runtime directly in cortex binary (no separate process)
- **Benefits**: Simpler deployment, faster startup, no daemon management
- **Trade-off**: Model loaded per cortex instance, higher memory usage for multi-project use

### Model Options
- Additional embedding models (larger/smaller trade-offs)
- User-provided custom models
- Automatic model selection based on use case
