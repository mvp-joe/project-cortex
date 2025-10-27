---
status: implemented
created_at: 2025-10-27T00:00:00Z
implemented_at: 2025-10-27T00:00:00Z
depends_on: []
---

# Provider Initialization Refactoring

## Problem Statement

The current embedding provider architecture has several issues:

1. **Duplicated Logic**: CLI commands (`index.go` and `mcp.go`) duplicate binary detection/download logic
2. **MCP Server Broken**: `mcp.go` hardcodes `BinaryPath: "cortex-embed"` without checking if it exists or downloading it
3. **Inconsistent Flow**: `index.go` has 40 lines (94-132) of binary management that `mcp.go` lacks entirely
4. **Wrong Abstraction**: Binary lifecycle management is in CLI layer instead of provider layer

**Current Error** (from MCP server startup):
```
Error: failed to create embedding provider: embedding binary not found at cortex-embed:
stat cortex-embed: no such file or directory
```

## Root Cause

The factory (`factory.go:38`) validates that BinaryPath exists:
```go
// Verify binary exists
if _, err := os.Stat(binaryPath); err != nil {
    return nil, fmt.Errorf("embedding binary not found at %s: %w", binaryPath, err)
}
```

But `mcp.go:65` passes a relative path:
```go
embedProvider, err := embed.NewProvider(embed.Config{
    Provider:   cfg.Embedding.Provider,
    BinaryPath: "cortex-embed",  // ❌ Wrong - not an absolute path
})
```

Meanwhile `index.go:94-132` properly calls `embed.EnsureBinaryInstalled()` to get the absolute path.

## Proposed Solution: Provider.Initialize() Method

Move **ALL lifecycle logic** into the provider itself via an `Initialize()` method.

### Design Philosophy

**Provider Responsibility**: Each provider knows how to prepare itself
- **LocalProvider**: Detect/download binary, start process, wait for health check
- **OpenAIProvider** (future): Validate API key, check connectivity
- **MockProvider**: No-op (always ready)

**CLI Responsibility**: Create provider, call Initialize(), use provider
- No binary path knowledge
- No download logic
- No process management
- Just: create → initialize → use

## Technical Design

### 1. Provider Interface Changes

**File**: `internal/embed/provider.go`

```go
type Provider interface {
    // Initialize prepares the provider and blocks until ready.
    // This method MUST be called before Embed() and should only be called once.
    //
    // For LocalProvider:
    //   - Detects if cortex-embed binary exists
    //   - Downloads from object storage if missing (~300MB)
    //   - Starts the cortex-embed server process
    //   - Waits for health check (retries with backoff)
    //
    // For future cloud providers:
    //   - Validates API credentials
    //   - Checks service connectivity
    //
    // Returns error if provider cannot be initialized.
    Initialize(ctx context.Context) error

    Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error)
    Dimensions() int
    Close() error
}
```

### 2. LocalProvider Implementation

**File**: `internal/embed/local.go`

#### Add Initialize() Method

```go
// Initialize prepares the local embedding provider by ensuring the binary
// is installed and the server process is running.
func (p *localProvider) Initialize(ctx context.Context) error {
    // Step 1: Ensure binary is installed (download if needed)
    binaryPath, err := EnsureBinaryInstalled(nil)
    if err != nil {
        return fmt.Errorf("failed to ensure cortex-embed binary: %w", err)
    }

    p.binaryPath = binaryPath

    // Step 2: Start the server process if not already running
    if err := p.startServer(ctx); err != nil {
        return fmt.Errorf("failed to start embedding server: %w", err)
    }

    // Step 3: Wait for health check with retries
    if err := p.waitForHealthy(ctx); err != nil {
        return fmt.Errorf("embedding server failed health check: %w", err)
    }

    return nil
}
```

#### Add Health Check Method

```go
// waitForHealthy waits for the embedding server to respond to health checks.
func (p *localProvider) waitForHealthy(ctx context.Context) error {
    maxRetries := 20
    retryDelay := 500 * time.Millisecond

    for i := 0; i < maxRetries; i++ {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // Try to connect to health endpoint
        healthURL := strings.Replace(p.endpoint, "/embed", "/health", 1)
        resp, err := http.Get(healthURL)
        if err == nil && resp.StatusCode == http.StatusOK {
            resp.Body.Close()
            return nil
        }
        if resp != nil {
            resp.Body.Close()
        }

        // Wait before retry (with exponential backoff)
        time.Sleep(retryDelay)
        if i > 5 {
            retryDelay *= 2
        }
    }

    return fmt.Errorf("embedding server did not become healthy after %d attempts", maxRetries)
}
```

#### Update Constructor

Remove BinaryPath parameter requirement:

```go
// newLocalProvider creates a local embedding provider.
// Binary path will be determined during Initialize().
func newLocalProvider(endpoint string) (Provider, error) {
    if endpoint == "" {
        endpoint = "http://localhost:8121/embed"
    }

    return &localProvider{
        endpoint: endpoint,
        // binaryPath will be set in Initialize()
    }, nil
}
```

#### Update Struct

```go
type localProvider struct {
    endpoint   string
    binaryPath string  // Set during Initialize()
    cmd        *exec.Cmd
    mu         sync.Mutex
}
```

#### Refactor Embed() Method

Move process startup to Initialize():

```go
func (p *localProvider) Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {
    // No longer starts server here - assumes Initialize() was called

    // Build request payload
    payload := map[string]interface{}{
        "texts": texts,
        "mode":  string(mode),
    }
    // ... rest of existing logic
}
```

### 3. MockProvider Implementation

**File**: `internal/embed/mock.go`

```go
// Initialize is a no-op for the mock provider (always ready).
func (m *mockProvider) Initialize(ctx context.Context) error {
    return nil
}
```

### 4. Factory Simplification

**File**: `internal/embed/factory.go`

```go
// Config contains configuration for creating an embedding provider.
type Config struct {
    // Provider specifies which embedding provider to use ("local", "openai", etc.)
    Provider string

    // Endpoint is the URL for the embedding service (for local provider)
    Endpoint string

    // APIKey for cloud providers (future)
    APIKey string

    // Model name (future: for provider-specific model selection)
    Model string

    // BinaryPath removed - no longer needed
}

// NewProvider creates an embedding provider based on the configuration.
// The provider must be initialized via Initialize() before use.
func NewProvider(config Config) (Provider, error) {
    switch config.Provider {
    case "local", "": // empty defaults to local
        endpoint := config.Endpoint
        if endpoint == "" {
            endpoint = "http://localhost:8121/embed"
        }
        return newLocalProvider(endpoint)

    case "mock": // for testing
        return newMockProvider(), nil

    default:
        return nil, fmt.Errorf("unsupported embedding provider: %s (supported: local, mock)", config.Provider)
    }
}
```

### 5. CLI Command Updates

#### index.go Changes

**Replace lines 94-132** with:

```go
// Create embedding provider
embedProvider, err := embed.NewProvider(embed.Config{
    Provider: cfg.Embedding.Provider,
    Endpoint: cfg.Embedding.Endpoint,
})
if err != nil {
    return fmt.Errorf("failed to create embedding provider: %w", err)
}
defer embedProvider.Close()

// Initialize provider (downloads binary if needed, starts server, waits for ready)
if !quietFlag {
    fmt.Println("Initializing embedding provider...")
}
if err := embedProvider.Initialize(ctx); err != nil {
    return fmt.Errorf("failed to initialize embedding provider: %w", err)
}

if !quietFlag {
    fmt.Println("✓ Embedding provider ready")
}

// Remove: indexerConfig.EmbeddingBinary assignment (no longer needed)
```

**Also update** where provider is created (around line 141):

```go
// Create indexer with progress reporting
if !quietFlag {
    log.Println("Initializing indexer...")
}

idx, err := indexer.New(indexerConfig)
if err != nil {
    return fmt.Errorf("failed to create indexer: %w", err)
}
defer idx.Close()
```

#### mcp.go Changes

**Replace lines 62-70** with:

```go
// Create embedding provider
embedProvider, err := embed.NewProvider(embed.Config{
    Provider: cfg.Embedding.Provider,
    Endpoint: cfg.Embedding.Endpoint,
})
if err != nil {
    return fmt.Errorf("failed to create embedding provider: %w", err)
}
defer embedProvider.Close()

// Initialize provider (downloads binary if needed, starts server, waits for ready)
if err := embedProvider.Initialize(ctx); err != nil {
    return fmt.Errorf("failed to initialize embedding provider: %w", err)
}
```

### 6. Indexer Config Cleanup

**File**: `internal/indexer/types.go` or config files

Remove `EmbeddingBinary` field if it exists in config structures, since providers now manage their own binaries.

## Implementation Steps

### Phase 1: Provider Interface & LocalProvider
1. Add `Initialize()` to Provider interface in `provider.go`
2. Implement `Initialize()` in `local.go`:
   - Call `EnsureBinaryInstalled()`
   - Move process startup from `Embed()` to `Initialize()`
   - Add `waitForHealthy()` method
3. Update `newLocalProvider()` to remove BinaryPath parameter
4. Add `Initialize()` to `mock.go` (no-op)

### Phase 2: Factory Simplification
1. Remove `BinaryPath` from `Config` struct in `factory.go`
2. Remove binary existence validation in `NewProvider()`
3. Update factory to just create provider instances

### Phase 3: CLI Updates
1. Update `index.go`:
   - Remove binary detection logic (lines 94-132)
   - Add `Initialize()` call after creating provider
2. Update `mcp.go`:
   - Remove hardcoded BinaryPath
   - Add `Initialize()` call after creating provider

### Phase 4: Testing
1. Update existing provider tests to call `Initialize()`
2. Test `cortex index` command
3. Test `cortex mcp` command (should now work)
4. Add integration test for Initialize() lifecycle

## Benefits

### 1. Single Responsibility
Each provider manages its own lifecycle:
- LocalProvider: Binary, process, health
- Future OpenAIProvider: API key, connectivity
- MockProvider: Nothing (always ready)

### 2. CLI Simplification
Both commands use identical 5-line pattern:
```go
provider, err := embed.NewProvider(config)
if err := provider.Initialize(ctx); err != nil { ... }
defer provider.Close()
// Use provider
```

### 3. Fixes MCP Bug
MCP server will now:
- Detect missing binary
- Download if needed
- Start server
- Wait for healthy
- Start successfully ✅

### 4. Extensibility
Adding new providers is straightforward:
```go
type openAIProvider struct { ... }

func (p *openAIProvider) Initialize(ctx context.Context) error {
    // Validate API key
    // Check connectivity
    return nil
}
```

## Testing Strategy

### Unit Tests

1. **Provider Interface Compliance**:
   ```go
   func TestLocalProviderImplementsInterface(t *testing.T) {
       var _ embed.Provider = (*embed.LocalProvider)(nil)
   }
   ```

2. **Initialize() Success Cases**:
   - Binary already installed
   - Binary downloaded successfully
   - Server starts and becomes healthy

3. **Initialize() Error Cases**:
   - Download fails (network error)
   - Binary won't execute (permission error)
   - Server never becomes healthy (timeout)
   - Context cancelled during initialization

4. **Initialize() Idempotency**:
   - Calling Initialize() twice should be safe
   - Second call should be fast (server already running)

### Integration Tests

1. **Full Lifecycle Test**:
   ```go
   func TestLocalProviderFullLifecycle(t *testing.T) {
       provider, _ := embed.NewProvider(embed.Config{Provider: "local"})

       err := provider.Initialize(context.Background())
       require.NoError(t, err)

       embeddings, err := provider.Embed(ctx, []string{"test"}, embed.EmbedModeQuery)
       require.NoError(t, err)
       require.NotEmpty(t, embeddings)

       provider.Close()
   }
   ```

2. **CLI Command Tests**:
   - Run `cortex index` on test project
   - Run `cortex mcp` and verify it starts
   - Test watch mode with provider initialization

### Manual Testing

```bash
# Test index command
cd /path/to/test/project
cortex index

# Test MCP command (should now work)
cortex mcp
# In another terminal, verify MCP server responds
```

## Migration Path

### Backward Compatibility

**Not a concern** - Project is pre-release. Existing users will upgrade cleanly because:
1. Config file doesn't change (no BinaryPath field to remove from user configs)
2. Binary location remains `~/.cortex/bin/cortex-embed`
3. CLI commands remain identical: `cortex index`, `cortex mcp`

### Documentation Updates

Update these files:
- `README.md` - No changes needed (user-facing commands unchanged)
- `specs/indexer.md` - Update code examples to show Initialize()
- `specs/mcp-server.md` - Update code examples to show Initialize()
- `CLAUDE.md` - Update architecture section with Initialize() pattern

## Success Metrics

✅ Provider interface has Initialize() method
✅ LocalProvider.Initialize() handles full lifecycle
✅ MockProvider.Initialize() is no-op
✅ Factory simplified (no BinaryPath validation)
✅ index.go simplified (40 lines → 10 lines)
✅ mcp.go fixed (hardcoded path → proper initialization)
✅ All tests pass
✅ `cortex mcp` starts successfully
✅ No code duplication between CLI commands

## Future Enhancements (Out of Scope)

1. **Process Reuse**: Detect already-running cortex-embed and reuse it
2. **Health Check Endpoint**: Add `/health` to cortex-embed server
3. **Graceful Restart**: Restart server if it crashes during operation
4. **Cloud Providers**: Implement OpenAIProvider, AnthropicProvider with Initialize()
5. **Configuration Validation**: Move all config validation to Initialize()

## Related Issues

- MCP server startup failure (current bug being fixed)
- Code duplication between index.go and mcp.go
- Provider abstraction improvements

## References

- `internal/embed/provider.go` - Provider interface definition
- `internal/embed/local.go` - LocalProvider implementation
- `internal/embed/downloader.go` - Binary download logic (used by Initialize)
- `internal/cli/index.go` - Index command (40 lines of duplication)
- `internal/cli/mcp.go` - MCP command (missing initialization)
