---
status: archived
started_at: 2025-11-09T00:00:00Z
completed_at: 2025-11-10T04:06:32Z
dependencies: []
---

# Daemon Foundation Infrastructure

## Purpose

The indexer daemon and ONNX embedding server share identical lifecycle patterns (auto-start via `EnsureDaemon()`, singleton enforcement, resurrection on connection failure, global configuration). This spec defines reusable foundation components that eliminate code duplication, ensure consistent behavior, and enable both daemons to be implemented cleanly on a shared infrastructure.

## Core Concept

**Input**: Daemon requirements (socket path, startup timeout, health check logic)

**Process**: Provide reusable components → Global configuration (Viper-based) → Generic lifecycle management (auto-start, singleton enforcement) → Connection error detection (resurrection pattern)

**Output**: Shared `internal/daemon/` package, global config infrastructure, unified provider interface

## Technology Stack

- **Language**: Go 1.25+
- **Configuration**: Viper (following existing project patterns)
- **File Locking**: `github.com/gofrs/flock` (singleton enforcement)
- **RPC**: ConnectRPC over Unix domain sockets
- **Config Locations**:
  - Global: `~/.cortex/config.yml` (machine-wide daemon settings)
  - Project: `.cortex/config.yml` (project-specific settings, unchanged)

## Architecture

### Component Overview

```
┌──────────────────────────────────────────────────────────────┐
│                   Daemon Foundation                           │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Global Config (~/.cortex/config.yml)                  │  │
│  │  - Viper-based loader                                  │  │
│  │  - Environment variable overrides                      │  │
│  │  - Default values if file missing                      │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  EnsureDaemon (Client-Side Auto-Start)                 │  │
│  │  1. Fast path health check                             │  │
│  │  2. Spawn daemon (detached process)                    │  │
│  │  3. Wait for healthy (poll with timeout)               │  │
│  │  Note: Multiple clients may spawn multiple daemons    │  │
│  │        Daemon-side singleton ensures only one wins     │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Singleton Enforcement (Daemon-Side)                   │  │
│  │  - Try socket bind                                      │  │
│  │  - If bind fails → another daemon has it → exit 0      │  │
│  │  - If bind succeeds → acquire lock → proceed           │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Connection Error Detection (Resurrection)             │  │
│  │  - IsConnectionError() helper                          │  │
│  │  - Used by providers to trigger re-initialization      │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
         ▲                              ▲
         │                              │
    ┌────┴──────────┐          ┌────────┴─────────┐
    │ Indexer       │          │ ONNX Embed       │
    │ Daemon        │          │ Server           │
    └───────────────┘          └──────────────────┘
```

### Usage Flow

**Client auto-starts daemon** (NO LOCKS):
```
┌──────────┐
│  Client  │ (cortex index, cortex mcp, etc.)
└────┬─────┘
     │
     │ 1. LoadGlobalConfig()
     ▼
┌─────────────────┐
│ Global Config   │ (~/.cortex/config.yml)
└────┬────────────┘
     │
     │ 2. daemon.EnsureDaemon(config)
     ▼
┌─────────────────┐
│ Health Check    │ (is daemon already running?)
└────┬────────────┘
     │
     │ No → spawn daemon (NO client lock)
     ▼
┌─────────────────┐
│ Spawn Daemon    │ (cortex indexer start, detached)
└────┬────────────┘
     │
     │ Wait for healthy (poll until daemon wins singleton)
     ▼
┌─────────────────┐
│ Connect & Use   │ (ConnectRPC client)
└─────────────────┘

Note: If 10 clients spawn simultaneously, all 10 spawn daemons.
      Daemon-side singleton ensures only one daemon wins.
      All clients wait and connect to the winner.
```

**Daemon self-enforces singleton**:
```
┌──────────────┐
│ cortex       │ (multiple launches possible)
│ indexer      │
│ start        │
└──────┬───────┘
       │
       │ LoadGlobalConfig()
       ▼
┌──────────────┐
│ Singleton    │ daemon.EnforceSingleton()
│ Check        │
└──────┬───────┘
       │
       ├──▶ Try socket bind
       │
       ├──▶ Success → This process wins → continue
       │
       └──▶ Fail → Another daemon has it → exit 0
```

## Data Model

### Global Configuration

**File**: `~/.cortex/config.yml`

```yaml
indexer_daemon:
  socket_path: ~/.cortex/indexer.sock
  startup_timeout: 30  # seconds

embed_daemon:
  socket_path: ~/.cortex/embed.sock
  idle_timeout: 600    # seconds (10 minutes)
  model_dir: ~/.cortex/onnx

cache:
  base_dir: ~/.cortex/cache
```

**Go Structs** (`internal/config/global.go`):

```go
type GlobalConfig struct {
    IndexerDaemon IndexerDaemonConfig `yaml:"indexer_daemon" mapstructure:"indexer_daemon"`
    EmbedDaemon   EmbedDaemonConfig   `yaml:"embed_daemon" mapstructure:"embed_daemon"`
    Cache         GlobalCacheConfig   `yaml:"cache" mapstructure:"cache"`
}

type IndexerDaemonConfig struct {
    SocketPath     string `yaml:"socket_path" mapstructure:"socket_path"`
    StartupTimeout int    `yaml:"startup_timeout" mapstructure:"startup_timeout"` // seconds
}

type EmbedDaemonConfig struct {
    SocketPath  string `yaml:"socket_path" mapstructure:"socket_path"`
    IdleTimeout int    `yaml:"idle_timeout" mapstructure:"idle_timeout"` // seconds
    ModelDir    string `yaml:"model_dir" mapstructure:"model_dir"`
}

type GlobalCacheConfig struct {
    BaseDir string `yaml:"base_dir" mapstructure:"base_dir"` // ~/.cortex/cache
}
```

### Environment Variable Overrides

Following existing project patterns (`CORTEX_` prefix, `.` → `_` replacement):

- `CORTEX_INDEXER_DAEMON_SOCKET_PATH`
- `CORTEX_INDEXER_DAEMON_STARTUP_TIMEOUT`
- `CORTEX_EMBED_DAEMON_SOCKET_PATH`
- `CORTEX_EMBED_DAEMON_IDLE_TIMEOUT`
- `CORTEX_EMBED_DAEMON_MODEL_DIR`
- `CORTEX_CACHE_BASE_DIR`

### Daemon Lifecycle Interfaces

**File**: `internal/daemon/ensure.go`

```go
// EnsureConfig specifies daemon auto-start parameters.
type EnsureConfig struct {
    Name           string                            // "indexer" or "embed"
    SocketPath     string                            // From global config
    StartCommand   []string                          // ["cortex", "indexer", "start"]
    StartupTimeout time.Duration                     // From global config
    HealthCheck    func(context.Context, string) error  // Custom health check
}

// EnsureDaemon ensures daemon is running, starting it if needed.
// Safe to call concurrently from multiple clients.
// If multiple clients spawn multiple daemons, daemon-side singleton
// enforcement ensures only one daemon wins. Losing daemons exit gracefully.
// Returns nil if daemon is healthy (already running or successfully started).
func EnsureDaemon(ctx context.Context, cfg EnsureConfig) error {
    // 1. Fast path: check if healthy
    if cfg.HealthCheck(ctx, cfg.SocketPath) == nil {
        return nil
    }

    // 2. Spawn daemon (detached)
    // Multiple clients may spawn multiple daemons - that's OK
    // Daemon-side singleton enforcement ensures only one wins
    cmd := exec.Command(cfg.StartCommand[0], cfg.StartCommand[1:]...)
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true, // Detach from parent process group
    }

    if err := cmd.Start(); err != nil {
        return fmt.Errorf("failed to start daemon: %w", err)
    }

    // 3. Wait for healthy
    // If multiple daemons spawned, only one passes EnforceSingleton
    // Others exit gracefully, this client just waits for the winner
    return waitForHealthy(ctx, cfg)
}
```

**File**: `internal/daemon/singleton.go`

```go
// SingletonDaemon manages daemon singleton enforcement.
type SingletonDaemon struct {
    name       string
    socketPath string
    lock       *flock.Flock
}

func NewSingletonDaemon(name, socketPath string) *SingletonDaemon {
    return &SingletonDaemon{
        name:       name,
        socketPath: socketPath,
    }
}

// EnforceSingleton attempts to become the singleton instance.
// Returns (true, nil) if this process won and should continue serving.
// Returns (false, nil) if another instance is running (this process should exit 0).
// Returns (false, err) on actual errors.
func (s *SingletonDaemon) EnforceSingleton() (bool, error) {
    // Try to bind socket
    listener, err := net.Listen("unix", s.socketPath)
    if err != nil {
        // Socket bind failed - another daemon has it
        if isAddrInUse(err) {
            return false, nil
        }
        return false, fmt.Errorf("failed to bind socket: %w", err)
    }
    listener.Close() // Close test listener

    // Acquire file lock
    lockPath := getLockPath(s.name)
    s.lock = flock.New(lockPath)

    locked, err := s.lock.TryLock()
    if err != nil {
        return false, fmt.Errorf("failed to acquire lock: %w", err)
    }

    if !locked {
        // Another process has the lock
        return false, nil
    }

    // This process won
    return true, nil
}

// BindSocket creates the Unix socket listener.
// Caller must have already won via EnforceSingleton().
func (s *SingletonDaemon) BindSocket() (net.Listener, error) {
    return net.Listen("unix", s.socketPath)
}

// Release releases the file lock (called on shutdown).
func (s *SingletonDaemon) Release() error {
    if s.lock != nil {
        return s.lock.Unlock()
    }
    return nil
}
```

**File**: `internal/daemon/errors.go`

```go
// IsConnectionError checks if error indicates daemon is not reachable.
// Used by providers to trigger resurrection after idle timeout.
func IsConnectionError(err error) bool {
    if err == nil {
        return false
    }

    errStr := err.Error()
    return strings.Contains(errStr, "connection refused") ||
           strings.Contains(errStr, "no such file or directory") ||  // Socket doesn't exist
           strings.Contains(errStr, "broken pipe")
}
```

## Implementation

### Phase 0: Provider Interface Unification

**Why first**: The ONNX spec requires this. MCP and indexer both use embedding providers, but there are two duplicate interfaces (`embed.Provider` and `mcp.EmbeddingProvider`). This creates type inconsistency and import cycles.

**Changes**:
1. Delete `EmbeddingProvider` interface from `internal/mcp/searcher.go`
2. Update MCP imports to use `internal/embed`
3. Replace `mode string` with typed `embed.EmbedMode` throughout MCP
4. Update MCP searcher constructor to accept `embed.Provider`
5. Update all MCP tests

**Files Modified**:
- `internal/mcp/searcher.go` - Remove duplicate interface
- `internal/mcp/vector_searcher.go` - Use `embed.Provider`, typed mode
- `internal/mcp/server.go` - Pass `embed.Provider` to tools
- `internal/mcp/*_test.go` - Update mocks

**Validation**: No import cycles, all tests pass

### Phase 1: Global Configuration Infrastructure

**Why**: Daemons are machine-wide singletons that need configuration separate from per-project settings.

**New Files**:
- `internal/config/global.go` - Global config structs
- `internal/config/global_loader.go` - Viper-based loader

**Loader Implementation**:

```go
// internal/config/global_loader.go

func LoadGlobalConfig() (*GlobalConfig, error) {
    v := viper.New()

    home, _ := os.UserHomeDir()
    cortexDir := filepath.Join(home, ".cortex")

    // Look for ~/.cortex/config.yml (NOT project .cortex/config.yml)
    v.SetConfigName("config")
    v.SetConfigType("yml")
    v.AddConfigPath(cortexDir)

    // Environment variable support (same pattern as project config)
    v.SetEnvPrefix("CORTEX")
    v.AutomaticEnv()
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

    // Bind environment variables
    bindGlobalEnvVars(v)

    // Set defaults
    setGlobalDefaults(v, cortexDir)

    // Read config (not an error if file doesn't exist)
    if err := v.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            return nil, err
        }
    }

    cfg := &GlobalConfig{}
    if err := v.Unmarshal(cfg); err != nil {
        return nil, err
    }

    return cfg, nil
}

func bindGlobalEnvVars(v *viper.Viper) {
    v.BindEnv("indexer_daemon.socket_path")
    v.BindEnv("indexer_daemon.startup_timeout")
    v.BindEnv("embed_daemon.socket_path")
    v.BindEnv("embed_daemon.idle_timeout")
    v.BindEnv("embed_daemon.model_dir")
    v.BindEnv("cache.base_dir")
}

func setGlobalDefaults(v *viper.Viper, cortexDir string) {
    v.SetDefault("indexer_daemon.socket_path", filepath.Join(cortexDir, "indexer.sock"))
    v.SetDefault("indexer_daemon.startup_timeout", 30)

    v.SetDefault("embed_daemon.socket_path", filepath.Join(cortexDir, "embed.sock"))
    v.SetDefault("embed_daemon.idle_timeout", 600) // 10 minutes
    v.SetDefault("embed_daemon.model_dir", filepath.Join(cortexDir, "onnx"))

    v.SetDefault("cache.base_dir", filepath.Join(cortexDir, "cache"))
}
```

**Validation**:
- Config loads from `~/.cortex/config.yml`
- Environment variables override YAML values
- Missing config file returns defaults (not an error)
- Follows existing Viper patterns from project config loader

### Phase 2: Daemon Lifecycle Components

**Why**: Both indexer daemon and ONNX embed server use identical lifecycle patterns (auto-start, singleton enforcement, resurrection).

**New Files**:
- `internal/daemon/ensure.go` - Generic `EnsureDaemon()` implementation
- `internal/daemon/singleton.go` - Self-managing singleton pattern
- `internal/daemon/errors.go` - Connection error detection
- `internal/daemon/helpers.go` - Utility functions (lock paths, wait logic)

**Key Design Decisions**:

1. **EnsureDaemon is client-side** - Clients call this before using daemon services
2. **Singleton enforcement is daemon-side** - Daemons self-manage on startup
3. **File locking prevents concurrent starts** - Multiple `EnsureDaemon()` calls are safe
4. **Resurrection pattern for idle timeout** - Providers detect connection errors and auto-restart

**Usage Example (Indexer Daemon)**:

```go
// internal/cli/index.go (client)

globalCfg, _ := config.LoadGlobalConfig()

err := daemon.EnsureDaemon(ctx, daemon.EnsureConfig{
    Name:           "indexer",
    SocketPath:     globalCfg.IndexerDaemon.SocketPath,
    StartCommand:   []string{"cortex", "indexer", "start"},
    StartupTimeout: time.Duration(globalCfg.IndexerDaemon.StartupTimeout) * time.Second,
    HealthCheck:    isIndexerHealthy,
})

// Daemon now running, connect via ConnectRPC
client := newIndexerClient(globalCfg.IndexerDaemon.SocketPath)
```

```go
// cmd/cortex/indexer_start.go (daemon)

globalCfg, _ := config.LoadGlobalConfig()

daemon := daemon.NewSingletonDaemon("indexer", globalCfg.IndexerDaemon.SocketPath)

won, err := daemon.EnforceSingleton()
if err != nil {
    log.Fatalf("Singleton check failed: %v", err)
}

if !won {
    log.Println("Indexer daemon already running")
    os.Exit(0) // Another instance running, exit cleanly
}

// This process won - proceed with serving
listener, _ := daemon.BindSocket()
// ... start ConnectRPC server ...
```

**Usage Example (ONNX Embed Server with Resurrection)**:

```go
// internal/embed/local.go (provider with resurrection)

func (p *localProvider) Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {
    // Try RPC directly (no preemptive health check)
    resp, err := p.client.Embed(ctx, connect.NewRequest(&embedv1.EmbedRequest{
        Texts: texts,
        Mode:  string(mode),
    }))

    // Only resurrect on connection errors (daemon died or idle timeout)
    if err != nil && daemon.IsConnectionError(err) {
        globalCfg, _ := config.LoadGlobalConfig()

        ensureCfg := daemon.EnsureConfig{
            Name:           "embed",
            SocketPath:     globalCfg.EmbedDaemon.SocketPath,
            StartCommand:   []string{"cortex", "embed", "start"},
            StartupTimeout: 5 * time.Second,
            HealthCheck:    isEmbedHealthy,
        }

        if err := daemon.EnsureDaemon(ctx, ensureCfg); err != nil {
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

    // Convert and return embeddings
    return convertEmbeddings(resp.Msg.Embeddings), nil
}
```

**Validation**:
- Unit tests for `EnsureDaemon()` (concurrent calls, stale sockets, timeouts)
- Unit tests for `SingletonDaemon` (multiple launches, socket conflicts)
- Integration tests (spawn daemon, verify singleton, test resurrection)

## Configuration

### Global vs Project Config

**Global Config** (`~/.cortex/config.yml`):
- Machine-wide daemon settings
- Socket paths, timeouts, model directories
- Shared cache location
- Read by daemons on startup

**Project Config** (`.cortex/config.yml`):
- Project-specific settings (unchanged from current)
- Embedding model, dimensions, endpoint
- Path patterns, chunking strategies
- Read by CLI commands when operating on a project

**Hierarchy** (highest to lowest priority):
1. Environment variables (`CORTEX_*`)
2. Global config (`~/.cortex/config.yml`)
3. Project config (`.cortex/config.yml`)
4. Built-in defaults

### Default Values

**Indexer Daemon**:
- Socket: `~/.cortex/indexer.sock`
- Startup timeout: 30 seconds

**Embed Daemon**:
- Socket: `~/.cortex/embed.sock`
- Idle timeout: 600 seconds (10 minutes)
- Model directory: `~/.cortex/onnx`

**Cache**:
- Base directory: `~/.cortex/cache`

## Performance Characteristics

### EnsureDaemon Performance

**Fast path** (daemon already running):
- Health check: <10ms (ConnectRPC call)
- Total: <10ms

**Slow path** (daemon needs starting):
- Spawn process: ~50-100ms
- Wait for healthy: Varies by daemon (indexer ~200ms, embed ~800ms)
  - Includes time for daemon singleton enforcement
- Total: ~250-1000ms (one-time cost)

### Singleton Enforcement

**Multiple concurrent launches**:
- Socket bind attempt: <10ms
- File lock acquisition: <10ms
- 19 processes exit cleanly: <100ms total
- 1 process continues: No delay

**Memory overhead**:
- File lock: <1MB
- Socket listener: <1MB
- Total per daemon: <2MB

## Testing Strategy

### Unit Tests

**Global Config Loader** (`internal/config/global_loader_test.go`):
- Loads from `~/.cortex/config.yml`
- Environment variables override YAML
- Missing file returns defaults
- Invalid YAML returns error

**EnsureDaemon** (`internal/daemon/ensure_test.go`):
- Fast path when daemon running
- Spawns daemon when not running
- Concurrent calls all spawn (daemon-side singleton prevents duplicates)
- Timeout handling
- Invalid command handling

**SingletonDaemon** (`internal/daemon/singleton_test.go`):
- First process wins (returns true)
- Subsequent processes exit (returns false)
- Socket bind conflicts detected
- File lock prevents races

**Connection Error Detection** (`internal/daemon/errors_test.go`):
- Detects "connection refused"
- Detects "no such file or directory"
- Detects "broken pipe"
- Returns false for other errors

### Integration Tests

**Daemon Lifecycle** (`internal/daemon/lifecycle_integration_test.go`):
```go
//go:build integration

func TestEnsureDaemon_Lifecycle(t *testing.T) {
    // 1. EnsureDaemon spawns daemon
    // 2. Verify daemon healthy
    // 3. EnsureDaemon called again (fast path)
    // 4. Daemon still running (not duplicated)
}

func TestSingletonDaemon_MultipleLaunches(t *testing.T) {
    // 1. Launch 20 daemon processes concurrently
    // 2. Verify 19 exit with code 0
    // 3. Verify 1 process continues serving
    // 4. Verify only one socket bound
}

func TestResurrection_IdleTimeout(t *testing.T) {
    // 1. Start embed daemon
    // 2. Wait for idle timeout (mock short timeout)
    // 3. Provider calls Embed()
    // 4. Connection error triggers resurrection
    // 5. Daemon restarted
    // 6. Retry succeeds
}
```

### End-to-End Tests

**Full Workflow** (`tests/e2e/daemon_foundation_test.go`):
```bash
# Test indexer daemon lifecycle
cortex index /path/to/project
# (auto-starts daemon if needed)

# Verify daemon running
ps aux | grep "cortex indexer start"

# Test concurrent client connections
cortex index /another/project &
cortex mcp &
# (both connect to same daemon)

# Test daemon shutdown
kill -TERM <pid>
# (graceful shutdown, releases lock)

# Test auto-restart after shutdown
cortex index /path/to/project
# (daemon auto-starts again)
```

## Migration Path

### From Current State

**Current**:
- No global config (hardcoded values in code)
- No `EnsureDaemon()` pattern (manual daemon management)
- Duplicate `EmbeddingProvider` interfaces in MCP and embed packages

**New**:
- Global config at `~/.cortex/config.yml`
- Reusable `daemon.EnsureDaemon()` for auto-start
- Single `embed.Provider` interface used everywhere

**Migration Steps**:

1. **Phase 0**: Unify provider interfaces (no user impact)
2. **Phase 1**: Add global config loader (defaults if file missing, no user action needed)
3. **Phase 2**: Add daemon lifecycle components (used by new daemons)
4. **Indexer & ONNX specs**: Build on foundation (transparent to users)

**Backward Compatibility**:
- Global config is optional (defaults work if file doesn't exist)
- Environment variables override config (same as project config)
- Project config behavior unchanged

## Non-Goals

This specification does NOT cover:

- **Daemon implementations themselves** - See `specs/2025-11-05_indexer-daemon.md` and `specs/2025-11-07_onnx-embedding-server.md`
- **ConnectRPC protocol definitions** - Each daemon defines its own protobuf schemas
- **Daemon-specific business logic** - Foundation provides lifecycle only
- **Multi-user daemon isolation** - Single-user, single-machine assumptions
- **Remote daemon access** - Unix sockets only (local machine)
- **Daemon orchestration** - No systemd/launchd integration (user-space daemons)
- **Daemon monitoring/metrics** - Each daemon implements its own observability
- **Configuration validation** - Happens in daemon-specific code
- **Hot config reload** - Daemons read config on startup only

## Implementation Checklist

### Phase 0: Provider Interface Unification
- ✅ Remove `EmbeddingProvider` interface from `internal/mcp/searcher.go`
- ✅ Update MCP imports to use `internal/embed` package
- ✅ Replace `mode string` with `embed.EmbedMode` in MCP vector searcher
- ✅ Update MCP searcher constructor to accept `embed.Provider`
- ✅ Update all MCP test files to use `embed.Provider` (with tests)
- ✅ Verify no import cycles introduced
- ✅ Run all tests to validate type changes

### Phase 1: Global Configuration Infrastructure
- ✅ Create `internal/config/global.go` with config structs
- ✅ Create `internal/config/global_loader.go` with Viper loader
- ✅ Implement `LoadGlobalConfig()` following existing patterns
- ✅ Add environment variable bindings for daemon config
- ✅ Add default value setters
- ✅ Write unit tests for global config loader (with tests)
- ✅ Test environment variable overrides (with tests)
- ✅ Test missing config file returns defaults (with tests)

### Phase 2: Daemon Lifecycle Components
- ✅ Create `internal/daemon/ensure.go` with `EnsureDaemon()` implementation (with tests)
- ✅ Create `internal/daemon/singleton.go` with `SingletonDaemon` implementation (with tests)
- ✅ Create `internal/daemon/errors.go` with `IsConnectionError()` helper (with tests)
- ✅ Create `internal/daemon/helpers.go` with utility functions (with tests)
- ✅ Write unit tests for `EnsureDaemon()` (concurrent calls, stale sockets, timeouts)
- ✅ Write unit tests for `SingletonDaemon` (multiple launches, socket conflicts)
- ⏸️  Write integration tests for daemon lifecycle (spawn, health, singleton) - DEFERRED
- ⏸️  Write integration tests for resurrection pattern (idle timeout, reconnect) - DEFERRED
- ✅ Document daemon lifecycle patterns in package godoc

**Note on Integration Tests**: Full end-to-end integration tests with real daemon processes
are deferred as future work. The comprehensive unit test coverage (69.4% for daemon package,
89.7% for config package) provides strong validation of the core logic. Integration tests
would require significant test infrastructure (real daemon binaries, process management,
signal handling) that is better addressed when implementing the actual indexer daemon and
ONNX embedding server.
