# ONNX Embedding Model

Pure Go wrapper for ONNX Runtime-based text embeddings.

## Purpose

Provides text-to-vector embedding generation using ONNX Runtime with quantized Google Gemma models. This replaces the Python-based cortex-embed service with a pure Go implementation.

## Features

- **768-dimensional embeddings** from Google Gemma (quantized to 4-bit)
- **Thread-safe inference** (ONNX Runtime handles concurrency internally)
- **Matryoshka Representation Learning** support (truncate 768→512/256/128)
- **Pure Go tokenizer** (go-sentencepiece, no CGO for tokenization)
- **Batch processing** for efficient inference

## Models

### Google Gemma (Quantized)

- **Dimensions**: 768 (base), supports truncation to 512/256/128
- **Quantization**: 4-bit (INT4) for smaller file size and faster inference
- **Files**:
  - `model_q4.onnx` - ONNX graph (500KB)
  - `model_q4.onnx_data` - Quantized weights (197MB)
  - `tokenizer.model` - SentencePiece vocabulary (4.5MB)

### Storage Location

Models stored in `~/.cortex/onnx/` (shared across all projects).

## Usage

```go
import "github.com/mvp-joe/project-cortex/internal/embed/onnx"

// Load model
model, err := onnx.NewEmbeddingModel(
    "/path/to/model_q4.onnx",
    "/path/to/tokenizer.model",
)
if err != nil {
    return err
}
defer model.Destroy()

// Generate embeddings (768d)
texts := []string{"Hello, world!", "Embeddings are vectors"}
embeddings, err := model.EmbedBatch(texts)
if err != nil {
    return err
}

// Truncate to smaller dimensions (Matryoshka)
for i := range embeddings {
    embeddings[i] = onnx.TruncateEmbedding(embeddings[i], 512)
}
```

## API

### `NewEmbeddingModel(onnxPath, tokenizerPath string) (*EmbeddingModel, error)`

Creates a new embedding model from ONNX model and tokenizer files.

### `(*EmbeddingModel).EmbedBatch(texts []string) ([][]float32, error)`

Generates 768-dimensional embeddings for multiple texts in a single batch.

**Thread-safe**: ONNX Runtime session can be called concurrently.

### `(*EmbeddingModel).Destroy() error`

Cleans up ONNX session resources. Should be called when model is no longer needed.

### `TruncateEmbedding(embedding []float32, targetDim int) []float32`

Implements Matryoshka Representation Learning by truncating and re-normalizing embeddings.

**Example**: `TruncateEmbedding(768d_vector, 512)` → `512d_vector`

Returns a new slice (does not modify input).

## Thread Safety

✅ **Thread-safe**: ONNX Runtime sessions are thread-safe for inference.

Multiple goroutines can call `EmbedBatch()` concurrently without external synchronization.

## Performance

### Inference Latency

| Batch Size | Avg Latency | Throughput |
|------------|-------------|------------|
| 1 text     | ~50ms       | 20 req/s   |
| 10 texts   | ~120ms      | 83 req/s   |
| 25 texts   | ~250ms      | 100 req/s  |

**Key insight**: Batching provides 4-5x throughput improvement.

### Memory Usage

- **Base Go runtime**: ~10MB
- **ONNX Runtime**: ~50MB
- **Loaded model**: ~200MB
- **Total**: ~260MB

### Startup Time

- **Model load**: ~800ms
- **Cold start** (first run): <1s

## Dependencies

- `github.com/yalue/onnxruntime_go` v1.22.0 - ONNX Runtime Go bindings (CGO)
- `github.com/eliben/go-sentencepiece` v0.6.0 - SentencePiece tokenizer (pure Go)

## Testing

Tests automatically skip if ONNX models not present in `~/.cortex/onnx/`:

```bash
# Run tests (skips if models not downloaded)
./scripts/test.sh ./internal/embed/onnx

# Download models first (Phase 2 implementation)
cortex embed download
```

## Matryoshka Representation Learning

The Gemma model supports **Matryoshka truncation**: truncating embeddings to smaller dimensions while maintaining search quality.

**Supported dimensions**: 768 (full), 512, 256, 128

**Trade-off**: Smaller dimensions = faster vector search, but slightly reduced accuracy.

**Usage**:
```go
// 768d (full quality)
embeddings, _ := model.EmbedBatch(texts)

// 512d (good balance)
for i := range embeddings {
    embeddings[i] = onnx.TruncateEmbedding(embeddings[i], 512)
}

// 256d (faster search, lower quality)
for i := range embeddings {
    embeddings[i] = onnx.TruncateEmbedding(embeddings[i], 256)
}
```

## Related

- Spec: `specs/2025-11-07_onnx-embedding-server.md`
- Provider interface: `internal/embed/provider.go`
- Daemon implementation: `internal/embed/daemon/` (Phase 2)
