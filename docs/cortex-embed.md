# Cortex-Embed

## What is it?

`cortex-embed` is a standalone embedding server that provides semantic search capabilities for Project Cortex. It's a single ~300MB binary (150MB compressed) with an embedded Python runtime, ML model, and all dependencies.

## Why does it exist?

The `cortex` CLI is designed to run as a project-specific MCP Server using stdio. You'll typically have one or more `cortex` instances per project (possibly multiple per Claude Code session).

`cortex-embed` is a **shared service** that any number of `cortex` processes can call. This architecture means:
- The embedding model (~130MB) is loaded into memory only once
- Multiple projects can share the same embedding server
- Both `cortex` CLI and `cortex-indexer` use the same embedding service
- The main `cortex` CLI stays small (~7MB) for fast `go install`

**Users never manually download or run cortex-embed.** The `cortex` CLI automatically downloads and manages it as needed.

## How does it work?

### Embedded Python Runtime

`cortex-embed` uses [go-embed-python](https://github.com/kluctl/go-embed-python) to bundle:
- Complete Python 3.11 runtime
- sentence-transformers library
- PyTorch (CPU-only)
- FastAPI web server
- All Python dependencies

The entire ML stack is embedded in a single Go binary with zero external dependencies.

### Startup Flow

1. `cortex` starts and needs embeddings
2. Checks if `cortex-embed` is running (health check on `http://127.0.0.1:8121`)
3. If not running, downloads appropriate binary for the platform (if needed) and starts it
4. `cortex-embed` extracts Python environment to temp directory
5. Launches FastAPI server and loads BGE-small-en-v1.5 model
6. First run: Downloads ~130MB model from HuggingFace (cached in `~/.cache/huggingface/`)
7. Subsequent runs: Loads cached model in 5-10 seconds
8. `cortex` makes HTTP POST requests to `/embed` endpoint

### API

Simple HTTP API on port 8121:

**POST /embed**
```json
{
  "texts": ["code snippet", "another chunk"],
  "mode": "passage"  // or "query"
}
```

Returns normalized 384-dimensional embeddings ready for cosine similarity.

**GET /** - Health check and model info

## Embedding Model

Uses **BAAI/bge-small-en-v1.5**:
- 384 dimensions (balance of quality and efficiency)
- 512 token context window
- Optimized for semantic search and retrieval
- ~130MB download, cached locally after first use

## Architecture Decisions

### Why Python for embeddings?

- Mature ML ecosystem (HuggingFace, transformers, PyTorch)
- Access to thousands of pre-trained models
- Better inference performance than pure Go ML libraries
- Trade binary size (~300MB) for functionality and ecosystem access

### Why separate binary?

- `cortex` stays small (~7MB) and fast to install via `go install`
- Users who only use non-embedding features don't download 300MB
- Embedding server can be shared across multiple projects
- Can version and release independently
- Easier to swap out embedding providers in the future

### Why not a remote API?

We will support remote embedding APIs (OpenAI, Anthropic, etc.) in the future, but local embeddings offer:
- Zero cost for local development
- No API keys needed
- Works offline
- Privacy - code never leaves the machine
- Lower latency for large codebases

## Platform Support

Each platform gets a separate binary via build tags and platform-specific embed files:
- `embed_darwin_arm64.go` - macOS Apple Silicon
- `embed_darwin_amd64.go` - macOS Intel
- `embed_linux_amd64.go` - Linux x64
- `embed_linux_arm64.go` - Linux ARM64
- `embed_windows_amd64.go` - Windows x64

Go's build system automatically selects the correct platform file, so each binary only includes dependencies for its target platform.

## Distribution

Pre-built binaries are released via GitHub Actions:
- Compressed archives (tar.gz/zip)
- 54% compression ratio (~300MB → ~150MB)
- Hosted on GitHub Releases
- `cortex` CLI downloads appropriate binary on first use

## Development

For contributors working on `cortex-embed`:

```bash
# Generate Python deps for your platform (one-time, ~20-30 min)
task python:deps:darwin-arm64

# Build and test
task build:embed
task run:embed

# Test the API
curl http://127.0.0.1:8121/
curl -X POST http://127.0.0.1:8121/embed \
  -H "Content-Type: application/json" \
  -d '{"texts": ["test"], "mode": "passage"}'
```

Python code changes and Go code changes can be made and rebuilt without regenerating Python dependencies (unless `requirements.txt` changes).

## Implementation Notes

### PyTorch Compatibility

Embedded Python environments exclude test modules. PyTorch's `torch._dynamo.trace_rules` unconditionally imports `torch._inductor.test_operators`, which doesn't exist in our embedded environment.

We monkey-patch a dummy module before any torch imports:
```python
import sys, types
sys.modules['torch._inductor.test_operators'] = types.ModuleType('torch._inductor.test_operators')
```

This only affects imports - inference functionality is unaffected.

### Model Caching UX

To avoid confusing users with repeated download messages:
- Check if model exists in `~/.cache/huggingface/hub/models--BAAI--bge-small-en-v1.5/`
- First run: Show "(First run: downloading ~130MB from HuggingFace)"
- Subsequent runs: Silent load, just "Loading..." → "✓ Model ready"
- HuggingFace's tqdm progress bars show during download

### Startup Timeout

Go wrapper uses 2-minute timeout with 500ms health checks to allow for:
- First-run model download (~10-30s)
- Python environment extraction (~5-10s)
- Model loading into memory (~5-10s)

## Future Enhancements

### Remote Embedding Providers
- **OpenAI API**: Use `text-embedding-3-small` or `text-embedding-3-large` via API key
- **Anthropic API**: Support for Anthropic's embedding models when available
- **Other providers**: Cohere, Voyage AI, or any OpenAI-compatible embedding endpoint
- **Benefits**: No local binary needed, potentially better quality, pay-as-you-go pricing

### Direct Python Execution
Instead of embedding Python in a Go binary, detect if the user's machine already has the necessary dependencies:
- **Detect Python environment**: Check for Python 3.9+ with required packages
- **Version validation**: Ensure PyTorch, sentence-transformers, FastAPI are correct versions
- **Direct execution**: Run `embedding_service.py` directly without `cortex-embed` download
- **Benefits**: Faster startup (no extraction), easier development, smaller download
- **Fallback**: If deps missing or wrong versions, fall back to `cortex-embed` binary

### Model Options
- Additional embedding models (larger/smaller trade-offs)
- Model quantization to reduce size

