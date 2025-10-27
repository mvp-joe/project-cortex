---
status: implemented
completed_at: 2025-10-15T00:00:00Z
dependencies: []
---

# Cortex-Embed Design Spec

## Overview

`cortex-embed` is a standalone embedding server for Project Cortex. It embeds a complete Python runtime with the BGE-small-en-v1.5 model using go-embed-python, resulting in a single ~300MB binary (compresses to ~150MB) with zero external dependencies.

## Design Goals

1. **Zero dependencies** - Single binary with embedded Python runtime and ML model
2. **Cross-platform** - Support macOS (Intel/ARM), Linux (x64/ARM64), Windows (x64)
3. **Easy distribution** - Users download pre-built binaries, no Python setup required
4. **Good UX** - Fast startup, clear messaging, automatic model caching

## Architecture

```
project-cortex/
├── cmd/
│   ├── cortex/              # Main CLI (installable via go install)
│   └── cortex-embed/        # Embedding server (download binary)
│       └── main.go
├── internal/
│   └── embed/
│       └── server/
│           ├── generate/
│           │   └── main.go           # Platform-specific pip package generator
│           ├── dummy.go              # go:generate trigger
│           ├── embed_darwin_arm64.go # Platform-specific embed (macOS ARM)
│           ├── embed_darwin_amd64.go # Platform-specific embed (macOS Intel)
│           ├── embed_linux_amd64.go  # Platform-specific embed (Linux x64)
│           ├── embed_linux_arm64.go  # Platform-specific embed (Linux ARM64)
│           ├── embed_windows_amd64.go # Platform-specific embed (Windows x64)
│           ├── requirements.txt       # Python dependencies
│           └── embedding_service.py   # FastAPI server
└── Taskfile.yml              # Build automation (task runner)
```

### Platform-Specific Embedding Strategy

Each platform gets its own Go file with build constraints:

- **Build tags**: `//go:build darwin && arm64`
- **Embedded path**: `//go:embed all:data/darwin-arm64`
- **Exported FS**: `var Data fs.FS` (strips platform directory prefix)

This allows:
- Single `go build` command automatically selects correct platform
- Cross-compilation works seamlessly
- Binary only includes dependencies for target platform

## Technology Stack

- **go-embed-python**: `github.com/kluctl/go-embed-python` - Embeds Python 3.11 runtime
- **Model**: BAAI/bge-small-en-v1.5 (384 dimensions, ~130MB download on first run)
- **API Framework**: FastAPI + Uvicorn
- **ML Stack**: sentence-transformers, PyTorch (CPU-only to minimize size)

## Python Embedding Service

### API Endpoints

**POST /embed**
- Request: `{"texts": ["string1", "string2"], "mode": "query|passage"}`
- Response: `{"embeddings": [[float...], [float...]]}`
- Embeddings are normalized (L2 norm = 1)

**GET /**
- Health check and model info
- Returns: `{"status": "ok", "model": "BAAI/bge-small-en-v1.5", "dimensions": 384, "max_tokens": 512}`

**GET /model_info**
- Returns model metadata

### Implementation Details

**PyTorch Compatibility Workaround**
- Embedded Python environments exclude PyTorch test modules
- Test modules are not used for inference, but is imported in PyTorch
- Need to monkey-patch to create dummy `torch._inductor.test_operators` module
- Must be done before any torch imports
- Only affects imports, doesn't impact inference functionality

**User notification**
- PyTorch will download the model the first time we use it, this can take 10-30s, need to let users know
- On startup, if the model isn't downloaded, it should notify user 
- Checks `~/.cache/huggingface/hub/models--BAAI--bge-small-en-v1.5/` for cached model
- First run: Shows download message "(First run: downloading ~130MB from HuggingFace)"
- Models take time to load into memory and be ready to use (5-10s), we want to let users know what is happening
- Loading message: At startup we show "Loading..." and then "✓ Model ready"
- HuggingFace libraries show native tqdm progress bars during download

**Go Wrapper Behavior**
- Go wrapper starts Python server and waits for readiness
- 2-minute timeout allows for model download on first run
- Health checks every 500ms via HTTP GET to root endpoint
- Clean shutdown on SIGINT

## Build Process

### Using Taskfile (Task Runner)

All build commands use `task`. See `Taskfile.yml` for complete task definitions.

**Generate Python Dependencies**
- Python dependencies need to be downloaded locally so they can be embedded into the go binary
- This is a one-time process (unless deps change)
- Dependencies are downloaded to internal/embed/server/data/{platform} - the data dir is in .gitignore
- After dependencies are downloaded, the binaries can be built
- The go code and the .py code can be changed and the binaries rebuilt without refetching the python deps

```bash
# For all platforms (10-20 min, ~300MB per platform)
task python:deps:all

# For specific platform (faster for local testing)
task python:deps:darwin-arm64
task python:deps:linux-amd64
...
```

The generator (`internal/embed/server/generate/main.go`) supports:
- All platforms via `go:generate` or `task python:deps:all`
- Selective platforms via `-platforms` flag (e.g., `darwin-arm64,linux-amd64`)

**Build Binaries**

```bash
# Build cortex CLI (small, cross-platform)
task build

# Build cortex-embed for current platform
task build:embed

# Build both
task build:all

# Cross-compile cortex for specific platform
task build:cross OS=linux ARCH=amd64

# Cross-compile cortex for all platforms
task build:cross:all
```

**Note**: Cross-compiling `cortex-embed` requires pre-generated Python dependencies for the target platform.

## Release Pipeline

### Automated Releases via GitHub Actions

**Trigger**: Push git tag (e.g., `v0.1.0`)

**Process**:
1. Generate Python dependencies for all platforms (~20-30 min)
2. GoReleaser builds binaries for all platforms
3. Create GitHub release with:
   - cortex binaries (all platforms) - users can `go install`
   - cortex-embed binaries (all platforms, tar.gz/zip compressed)
   - Checksums
   - Release notes

**Configuration**:
- `.goreleaser.yml` - Build matrix, archive settings, release notes template
- `.github/workflows/release.yml` - CI pipeline

## Distribution Strategy

**cortex CLI**:
- Installable via `go install github.com/user/project-cortex/cmd/cortex@latest`
- Small, fast download
- Core MCP server functionality

**cortex-embed**:
- Download pre-built binary from GitHub releases
- Users don't need to build 
- Platform-specific downloads (darwin-arm64, linux-amd64, etc.)

## Testing

```bash
# Start embedding server
task run:embed

# Test health endpoint
curl http://127.0.0.1:8121/

# Test embedding
curl -X POST http://127.0.0.1:8121/embed \
  -H "Content-Type: application/json" \
  -d '{"texts": ["hello world", "test embedding"]}'
```

## Design Trade-offs

**Why embed Python instead of Go ML libraries?**
- Mature ecosystem (sentence-transformers, HuggingFace)
- Pre-trained models readily available
- Better inference performance than pure Go alternatives
- Trade size (~300MB) for functionality and maintainability

**Why separate binaries?**
- cortex CLI is used as a project-specific MCP Server that uses stdio
  - we will run at least one cortex instance per project, possibly multiple (one for each claude code session) 
- cortex-embed is a generic embed-server, any number of cortex processes can call it
  - this means we only load the embed model once 
- cortex-indexer also needs the embed model to be running
- cortex CLI stays small for fast `go install`
- Easier to version and release independently
