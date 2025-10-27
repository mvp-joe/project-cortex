# Coding Conventions

## Public Interface Pattern

All major components use public interfaces with unexported implementations:

```go
// Public interface in internal/embed/provider.go
type Provider interface {
    Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error)
    Dimensions() int
    Close() error
}

// Public constructor returns interface
func NewProvider(config Config) (Provider, error) {
    return &localProvider{
        config: config,
    }, nil
}

// Unexported implementation in internal/embed/local.go
type localProvider struct {
    config Config
}
```

**Benefits:** Encapsulation, testability, interface-driven development, easy mocking

## Package Organization

- `cmd/cortex/`: CLI entry points and command definitions
- `internal/indexer/`: Tree-sitter parsing, chunking, embedding
- `internal/mcp/`: MCP server and protocol implementation
- `internal/embed/`: Embedding provider interface and implementations
- `tests/`: E2E and integration tests
- `testdata/`: Test fixtures and sample code

## Error Handling

### Standard Go Patterns

```go
if err != nil {
    return fmt.Errorf("failed to parse file: %w", err)
}

// Custom error types for errors.Is()
var (
    ErrUnsupportedLanguage = errors.New("unsupported language")
    ErrInvalidChunkFormat  = errors.New("invalid chunk format")
)

if errors.Is(err, ErrUnsupportedLanguage) {
    // handle gracefully
}
```

### MCP Protocol Errors

```go
import "github.com/mark3labs/mcp-go/mcp"

// Return MCP-compliant errors
return nil, fmt.Errorf("invalid query: %w", err)
```

Ref: [mcp-go Documentation](https://github.com/mark3labs/mcp-go)

## Logging

### Standard log package

```go
import "log"

// Simple CLI logging
log.Printf("Indexing %d files...", fileCount)
log.Printf("✓ Generated %d chunks", chunkCount)

// For errors
log.Fatalf("Failed to start MCP server: %v", err)
```

**Guidelines:**
- Keep CLI output clean and user-friendly
- Use `log.Printf` for informational messages
- Use `log.Fatalf` for fatal errors
- Consider adding verbose flag for detailed logging

## Configuration

### YAML + Environment Variables

Configuration is loaded from `.cortex/config.yml`:

```go
type Config struct {
    Embedding struct {
        Provider   string `yaml:"provider"`
        Model      string `yaml:"model"`
        Dimensions int    `yaml:"dimensions"`
    } `yaml:"embedding"`

    Paths struct {
        Code   []string `yaml:"code"`
        Docs   []string `yaml:"docs"`
        Ignore []string `yaml:"ignore"`
    } `yaml:"paths"`
}
```

**Environment variable overrides:**
- `CORTEX_CHUNKS_DIR`: Override chunks directory
- `CORTEX_EMBEDDING_ENDPOINT`: Override embedding service URL
