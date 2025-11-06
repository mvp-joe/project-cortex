---
status: draft
started_at: 2025-11-05T00:00:00Z
completed_at: null
dependencies: []
---

# Indexer Daemon Architecture

## Purpose

The indexer daemon provides continuous, automatic code indexing across all registered projects with minimal resource overhead. It eliminates per-process file watching, prevents duplicate indexing work, and maintains up-to-date search indexes without user intervention. A single machine-wide daemon watches all registered projects and incrementally updates their SQLite caches, while stateless MCP servers query these caches directly.

## Core Concept

**Input**: Registered projects in `~/.cortex/projects.json`, file change events, git branch switches

**Process**: Actor-per-repo model → git watching + file watching → incremental indexing → SQLite cache updates

**Output**: Always-fresh SQLite caches per project/branch, zero manual indexing, automatic resource cleanup

## Technology Stack

- **Language**: Go 1.25+
- **RPC**: ConnectRPC (gRPC over Unix domain socket)
- **Protocol**: Protocol Buffers (schema-defined APIs)
- **File Watching**: fsnotify (existing watcher code in `internal/watcher/`)
- **Storage**: SQLite with sqlite-vec and FTS5
- **Process Coordination**: File locking (`github.com/gofrs/flock`)
- **Existing Components**:
  - `internal/watcher/git_watcher.go` - Git HEAD watching
  - `internal/watcher/file_watcher.go` - Source file watching with debouncing
  - `internal/watcher/coordinator.go` - Actor coordination pattern
  - `internal/cache/key.go` - Cache key computation
  - `internal/cache/branch.go` - Branch detection and ancestry
  - `internal/indexer/branch_synchronizer.go` - Branch DB preparation

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Indexer Daemon Process                     │
│  (Single machine-wide instance)                               │
│                                                               │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │  Registry Watcher (fsnotify on projects.json)           │ │
│  └────────────────────┬────────────────────────────────────┘ │
│                       │                                       │
│                       ▼                                       │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │  Actor Registry (map[projectPath]*Actor)                │ │
│  │  - Spawns/stops actors based on projects.json changes   │ │
│  └──┬───────────────────┬──────────────────────┬───────────┘ │
│     │                   │                      │              │
│     ▼                   ▼                      ▼              │
│  ┌────────┐         ┌────────┐            ┌────────┐         │
│  │ Actor  │         │ Actor  │            │ Actor  │         │
│  │(gorout)│         │(gorout)│   ...      │(gorout)│         │
│  │        │         │        │            │        │         │
│  │ Git    │         │ Git    │            │ Git    │         │
│  │ Watch  │         │ Watch  │            │ Watch  │         │
│  │   +    │         │   +    │            │   +    │         │
│  │ File   │         │ File   │            │ File   │         │
│  │ Watch  │         │ Watch  │            │ Watch  │         │
│  │   ↓    │         │   ↓    │            │   ↓    │         │
│  │ Index  │         │ Index  │            │ Index  │         │
│  │   ↓    │         │   ↓    │            │   ↓    │         │
│  │SQLite  │         │SQLite  │            │SQLite  │         │
│  └────────┘         └────────┘            └────────┘         │
│                                                               │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │  ConnectRPC Server (Unix socket: ~/.cortex/indexer.sock)│ │
│  │  - Index(stream) - Trigger indexing, stream progress    │ │
│  │  - GetStatus() - Query daemon and project states        │ │
│  │  - StreamLogs(stream) - Tail logs for projects          │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
         ▲                              ▲
         │                              │
    ┌────┴──────┐                  ┌────┴──────┐
    │ cortex    │                  │ cortex    │
    │ index     │                  │ mcp       │
    │ (client)  │                  │ (client)  │
    └───────────┘                  └───────────┘
```

### MCP Server Architecture (Stateless)

```
┌─────────────────────────────────────────────────────┐
│              MCP Server Process (stdio)              │
│  (One per Claude Code tab, ephemeral)                │
│                                                      │
│  ┌────────────────────────────────────────────────┐ │
│  │  DBHolder (thread-safe DB accessor)            │ │
│  │  - DBProvider interface (read-only, to tools)  │ │
│  │  - DBManager interface (read-write, to server) │ │
│  └───────────────┬────────────────────────────────┘ │
│                  │                                   │
│                  ▼                                   │
│  ┌────────────────────────────────────────────────┐ │
│  │  Git Watcher (watches .git/HEAD)               │ │
│  │  On branch switch: open new DB, swap DBHolder  │ │
│  └────────────────────────────────────────────────┘ │
│                                                      │
│  ┌────────────────────────────────────────────────┐ │
│  │  MCP Tools (composable registration)           │ │
│  │  - cortex_search (queries vec_chunks)          │ │
│  │  - cortex_exact (queries files_fts)            │ │
│  │  - cortex_graph (lazy-loads graph)             │ │
│  │  - cortex_files (queries files/modules)        │ │
│  │  All tools call DBProvider.GetDB()             │ │
│  └────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
```

## Protobuf API

### Service Definition

```protobuf
// api/indexer/v1/indexer.proto
syntax = "proto3";

package indexer.v1;

option go_package = "github.com/yourusername/project-cortex/gen/indexer/v1;indexerv1";

// IndexerService manages the indexing daemon and project indexing operations.
service IndexerService {
  // Index triggers indexing for a project and streams progress updates.
  // If the project is not registered, it will be registered automatically.
  // The stream completes when initial indexing finishes.
  rpc Index(IndexRequest) returns (stream IndexProgress);

  // GetStatus returns the daemon status and all watched projects.
  rpc GetStatus(StatusRequest) returns (StatusResponse);

  // StreamLogs streams log entries for one or all projects.
  // If follow=true, the stream remains open for new logs.
  rpc StreamLogs(LogsRequest) returns (stream LogEntry);

  // UnregisterProject stops watching a project and optionally removes its cache.
  rpc UnregisterProject(UnregisterRequest) returns (UnregisterResponse);
}

// IndexRequest specifies the project to index.
message IndexRequest {
  // Absolute path to the project root.
  string project_path = 1;
}

// IndexProgress represents a progress update during indexing.
message IndexProgress {
  // Current indexing phase.
  enum Phase {
    PHASE_UNSPECIFIED = 0;
    PHASE_DISCOVERING = 1;   // Discovering files to index
    PHASE_INDEXING = 2;      // Parsing and chunking files
    PHASE_EMBEDDING = 3;     // Generating embeddings
    PHASE_COMPLETE = 4;      // Indexing complete, transitioning to watching
  }

  Phase phase = 1;
  int32 files_total = 2;
  int32 files_processed = 3;
  int32 chunks_generated = 4;
  string current_file = 5;  // File currently being processed (optional)
  string message = 6;        // Human-readable status message
}

// StatusRequest has no parameters (queries all state).
message StatusRequest {}

// StatusResponse contains daemon and project status.
message StatusResponse {
  // Daemon process information.
  DaemonStatus daemon = 1;

  // All registered projects.
  repeated ProjectStatus projects = 2;
}

// DaemonStatus describes the indexer daemon state.
message DaemonStatus {
  int32 pid = 1;                    // Process ID
  int64 started_at = 2;             // Unix timestamp (seconds)
  int64 uptime_seconds = 3;         // Seconds since startup
  string socket_path = 4;           // Unix socket path
}

// ProjectStatus describes a single project's indexing state.
message ProjectStatus {
  string path = 1;                  // Absolute project path
  string cache_key = 2;             // Cache key (remote-hash-worktree-hash)
  string current_branch = 3;        // Active git branch
  int32 files_indexed = 4;          // Total files in current branch DB
  int32 chunks_count = 5;           // Total chunks in current branch DB
  int64 registered_at = 6;          // Unix timestamp
  int64 last_indexed_at = 7;        // Unix timestamp
  bool is_indexing = 8;             // True if actively indexing
  IndexProgress.Phase current_phase = 9;  // Current phase if indexing
}

// LogsRequest specifies which logs to stream.
message LogsRequest {
  // Project path filter. Empty = all projects.
  string project_path = 1;

  // If true, stream remains open for new logs (tail -f behavior).
  bool follow = 2;
}

// LogEntry represents a single log line.
message LogEntry {
  int64 timestamp = 1;              // Unix timestamp (milliseconds)
  string project = 2;               // Project path that generated the log
  string level = 3;                 // INFO, WARN, ERROR, DEBUG
  string message = 4;               // Log message
}

// UnregisterRequest specifies the project to unregister.
message UnregisterRequest {
  string project_path = 1;
  bool remove_cache = 2;            // If true, delete cache directory
}

// UnregisterResponse confirms unregistration.
message UnregisterResponse {
  bool success = 1;
  string message = 2;
}
```

### Generated Code

ConnectRPC generates:
- `gen/indexer/v1/indexer.pb.go` - Protobuf message types
- `gen/indexer/v1/indexerconnect/indexer.connect.go` - Client and server interfaces

## Projects Registry

### File Location

```
~/.cortex/projects.json
```

### Schema

```json
{
  "projects": [
    {
      "path": "/Users/joe/code/project-cortex",
      "cache_key": "a1b2c3d4-e5f6g7h8",
      "registered_at": "2025-11-05T10:30:00Z",
      "last_indexed_at": "2025-11-05T14:22:15Z"
    },
    {
      "path": "/Users/joe/code/other-project",
      "cache_key": "f9e8d7c6-b5a4f3e2",
      "registered_at": "2025-11-05T11:00:00Z",
      "last_indexed_at": "2025-11-05T14:20:00Z"
    }
  ]
}
```

### Go Types

```go
type ProjectsRegistry struct {
    Projects []*RegisteredProject `json:"projects"`
}

type RegisteredProject struct {
    Path          string    `json:"path"`
    CacheKey      string    `json:"cache_key"`
    RegisteredAt  time.Time `json:"registered_at"`
    LastIndexedAt time.Time `json:"last_indexed_at"`
}
```

### Registry Operations

**Register project:**
```go
func RegisterProject(projectPath string) error {
    registry := loadRegistry()

    // Check if already registered
    for _, p := range registry.Projects {
        if p.Path == projectPath {
            return nil  // Already registered
        }
    }

    // Compute cache key
    cacheKey, err := cache.GetCacheKey(projectPath)
    if err != nil {
        return err
    }

    // Add to registry
    registry.Projects = append(registry.Projects, &RegisteredProject{
        Path:         projectPath,
        CacheKey:     cacheKey,
        RegisteredAt: time.Now(),
    })

    // Save atomically (temp file + rename)
    return saveRegistryAtomic(registry)
}
```

**Unregister project:**
```go
func UnregisterProject(projectPath string, removeCache bool) error {
    registry := loadRegistry()

    var cacheKey string
    filtered := []*RegisteredProject{}
    for _, p := range registry.Projects {
        if p.Path == projectPath {
            cacheKey = p.CacheKey  // Save for cache removal
            continue  // Skip (remove from list)
        }
        filtered = append(filtered, p)
    }

    registry.Projects = filtered

    if err := saveRegistryAtomic(registry); err != nil {
        return err
    }

    // Optionally remove cache
    if removeCache && cacheKey != "" {
        cachePath := filepath.Join(cortexDir, "cache", cacheKey)
        os.RemoveAll(cachePath)
    }

    return nil
}
```

### Move Detection and Recovery

When a project directory moves, the cache key changes (worktree path hash differs). Recovery flow:

```go
func RecoverMovedProject(newPath string) error {
    // Check for .cortex/settings.local.json
    settingsPath := filepath.Join(newPath, ".cortex", "settings.local.json")
    settings, err := loadSettings(settingsPath)
    if err != nil {
        return nil  // Not a moved project, just new
    }

    oldCacheKey := settings.CacheKey

    // Compute new cache key
    newCacheKey, _ := cache.GetCacheKey(newPath)

    if oldCacheKey == newCacheKey {
        return nil  // Not moved, same location
    }

    // Find old entry in registry
    registry := loadRegistry()
    for _, p := range registry.Projects {
        if p.CacheKey == oldCacheKey {
            // Update entry with new path and cache key
            p.Path = newPath
            p.CacheKey = newCacheKey

            // Rename cache directory
            oldCachePath := filepath.Join(cortexDir, "cache", oldCacheKey)
            newCachePath := filepath.Join(cortexDir, "cache", newCacheKey)
            os.Rename(oldCachePath, newCachePath)

            // Update settings.local.json
            settings.CacheKey = newCacheKey
            saveSettings(settingsPath, settings)

            return saveRegistryAtomic(registry)
        }
    }

    return nil  // Old entry not found, treat as new project
}
```

## Daemon Lifecycle

### EnsureDaemon (Reusable Pattern)

Idempotent daemon startup used by all CLI commands:

```go
// internal/indexer/daemon/ensure.go

// EnsureDaemon ensures the indexer daemon is running, starting it if needed.
// Safe to call concurrently from multiple processes.
// Returns nil if daemon is healthy (already running or successfully started).
func EnsureDaemon(ctx context.Context) error {
    sockPath := GetDaemonSocketPath()  // ~/.cortex/indexer.sock

    // Fast path: daemon already running and healthy
    if isDaemonHealthy(ctx, sockPath) {
        return nil
    }

    // Acquire exclusive lock to prevent concurrent daemon starts
    lockPath := filepath.Join(cortexDir, "indexer.lock")
    lock := flock.New(lockPath)

    if err := lock.Lock(); err != nil {
        return fmt.Errorf("failed to acquire daemon lock: %w", err)
    }
    defer lock.Unlock()

    // Re-check after acquiring lock (another process may have started it)
    if isDaemonHealthy(ctx, sockPath) {
        return nil
    }

    // Remove stale socket if exists
    os.Remove(sockPath)

    // Start daemon process (detached from parent)
    cmd := exec.Command("cortex", "indexer", "start")
    cmd.Stdout = nil  // Daemon logs to ~/.cortex/logs/indexer.log
    cmd.Stderr = nil
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,  // Create new process group (detach from parent)
    }

    if err := cmd.Start(); err != nil {
        return fmt.Errorf("failed to start daemon: %w", err)
    }

    // Wait for daemon to become healthy (up to 5 seconds)
    return waitForDaemonHealthy(ctx, sockPath, 5*time.Second)
}

// isDaemonHealthy checks if daemon is running and responding.
func isDaemonHealthy(ctx context.Context, sockPath string) bool {
    // Check socket file exists
    if _, err := os.Stat(sockPath); err != nil {
        return false
    }

    // Try to connect and call GetStatus (health check)
    client := newIndexerClient(sockPath)
    ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
    defer cancel()

    _, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
    return err == nil
}

// waitForDaemonHealthy polls until daemon is healthy or timeout.
func waitForDaemonHealthy(ctx context.Context, sockPath string, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if isDaemonHealthy(ctx, sockPath) {
                return nil
            }
        case <-ctx.Done():
            return fmt.Errorf("daemon failed to start within %v", timeout)
        }
    }
}

// GetDaemonSocketPath returns the Unix socket path for the daemon.
func GetDaemonSocketPath() string {
    cortexDir := filepath.Join(os.Getenv("HOME"), ".cortex")
    return filepath.Join(cortexDir, "indexer.sock")
}
```

### Daemon Server Startup

```go
// cmd/cortex/indexer_start.go

func runIndexerDaemon(ctx context.Context) error {
    sockPath := daemon.GetDaemonSocketPath()

    // Check if already running (self-protection)
    if daemon.IsDaemonHealthy(ctx, sockPath) {
        fmt.Println("Indexer daemon already running")
        return nil  // Exit gracefully (not an error)
    }

    // Remove stale socket
    os.Remove(sockPath)

    // Create daemon server
    srv := daemon.NewServer(ctx)

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
    path, handler := indexerv1connect.NewIndexerServiceHandler(srv)
    mux.Handle(path, handler)

    // Start HTTP server over Unix socket
    httpServer := &http.Server{
        Handler: mux,
    }

    // Graceful shutdown on signal
    go func() {
        <-ctx.Done()
        httpServer.Shutdown(context.Background())
        srv.Stop()
    }()

    log.Printf("Indexer daemon started (socket: %s)", sockPath)

    return httpServer.Serve(listener)
}
```

### Double-Start Protection

**Scenario:** User runs `cortex indexer start` twice.

**Flow:**
1. First instance acquires lock, creates socket, starts serving
2. Second instance blocks on lock
3. First instance releases lock after socket creation
4. Second instance acquires lock, calls `isDaemonHealthy()` → success
5. Second instance prints "already running" and exits (code 0)

**Result:** No duplicate daemons, clean user experience.

### Stale Socket Cleanup

**Scenario:** Daemon crashes, leaves stale `.sock` file.

**Recovery:**
1. Next `EnsureDaemon()` sees socket exists
2. Tries `isDaemonHealthy()` → connection refused
3. Acquires lock, removes stale socket
4. Starts new daemon

**Automatic recovery, no manual intervention.**

## Actor Model

### Actor Structure

```go
// internal/indexer/daemon/actor.go

type Actor struct {
    projectPath   string
    cacheKey      string
    currentBranch string

    // Watchers
    gitWatcher  watcher.GitWatcher
    fileWatcher watcher.FileWatcher

    // Indexing state
    indexer      *indexer.Indexer
    isIndexing   atomic.Bool
    currentPhase atomic.Value  // IndexProgress.Phase

    // Progress subscribers (for RPC streaming)
    progressMu   sync.RWMutex
    progressSubs map[string]chan *indexerv1.IndexProgress

    // Lifecycle
    ctx    context.Context
    cancel context.CancelFunc
    stopCh chan struct{}
    doneCh chan struct{}
}

func NewActor(ctx context.Context, project *RegisteredProject) (*Actor, error) {
    actorCtx, cancel := context.WithCancel(ctx)

    a := &Actor{
        projectPath:  project.Path,
        cacheKey:     project.CacheKey,
        progressSubs: make(map[string]chan *indexerv1.IndexProgress),
        ctx:          actorCtx,
        cancel:       cancel,
        stopCh:       make(chan struct{}),
        doneCh:       make(chan struct{}),
    }

    // Detect current branch
    a.currentBranch = cache.GetCurrentBranch(project.Path)

    // Initialize indexer
    a.indexer = indexer.New(project.Path, a.cacheKey)

    return a, nil
}

func (a *Actor) Start() error {
    // Start git watcher
    if err := a.gitWatcher.Start(a.ctx, a.handleBranchSwitch); err != nil {
        return fmt.Errorf("failed to start git watcher: %w", err)
    }

    // Start file watcher
    if err := a.fileWatcher.Start(a.ctx, a.handleFileChanges); err != nil {
        a.gitWatcher.Stop()
        return fmt.Errorf("failed to start file watcher: %w", err)
    }

    log.Printf("[%s] Actor started (branch: %s)", filepath.Base(a.projectPath), a.currentBranch)

    return nil
}

func (a *Actor) Stop() {
    close(a.stopCh)

    a.gitWatcher.Stop()
    a.fileWatcher.Stop()
    a.cancel()

    <-a.doneCh  // Wait for cleanup

    log.Printf("[%s] Actor stopped", filepath.Base(a.projectPath))
}

func (a *Actor) handleBranchSwitch(oldBranch, newBranch string) {
    log.Printf("[%s] Branch switch: %s → %s", filepath.Base(a.projectPath), oldBranch, newBranch)

    a.currentBranch = newBranch

    // Pause file watching (accumulate events during sync)
    a.fileWatcher.Pause()
    defer a.fileWatcher.Resume()

    // Prepare branch DB (copy from ancestor if needed)
    if err := a.indexer.PrepareDB(a.ctx, newBranch); err != nil {
        log.Printf("[%s] Failed to prepare branch DB: %v", filepath.Base(a.projectPath), err)
        return
    }

    // Switch indexer to new branch
    a.indexer.SwitchBranch(newBranch)

    // Resume will trigger file watcher callback if events accumulated
}

func (a *Actor) handleFileChanges(files []string) {
    if a.isIndexing.Load() {
        // Already indexing, skip (debouncer will trigger again)
        return
    }

    a.isIndexing.Store(true)
    defer a.isIndexing.Store(false)

    log.Printf("[%s] Indexing %d changed files", filepath.Base(a.projectPath), len(files))

    // Process changed files
    stats, err := a.indexer.ProcessFiles(a.ctx, files)
    if err != nil {
        log.Printf("[%s] Indexing failed: %v", filepath.Base(a.projectPath), err)
        return
    }

    log.Printf("[%s] Indexed %d files (%d chunks)", filepath.Base(a.projectPath), stats.FilesProcessed, stats.ChunksGenerated)
}

// SubscribeProgress registers a channel for progress updates.
func (a *Actor) SubscribeProgress(id string) chan *indexerv1.IndexProgress {
    a.progressMu.Lock()
    defer a.progressMu.Unlock()

    ch := make(chan *indexerv1.IndexProgress, 10)
    a.progressSubs[id] = ch
    return ch
}

// UnsubscribeProgress removes a progress subscriber.
func (a *Actor) UnsubscribeProgress(id string) {
    a.progressMu.Lock()
    defer a.progressMu.Unlock()

    if ch, ok := a.progressSubs[id]; ok {
        close(ch)
        delete(a.progressSubs, id)
    }
}

// publishProgress sends progress to all subscribers.
func (a *Actor) publishProgress(progress *indexerv1.IndexProgress) {
    a.progressMu.RLock()
    defer a.progressMu.RUnlock()

    for _, ch := range a.progressSubs {
        select {
        case ch <- progress:
        default:
            // Subscriber slow, skip (non-blocking)
        }
    }
}
```

### Registry Synchronization

Daemon watches `projects.json` and syncs actors:

```go
// internal/indexer/daemon/server.go

type Server struct {
    ctx      context.Context
    registry *ProjectsRegistry

    // Actor management
    actorsMu sync.RWMutex
    actors   map[string]*Actor  // key: project path

    // Registry watcher
    registryWatcher *fsnotify.Watcher

    // Shutdown
    stopCh chan struct{}
    doneCh chan struct{}
}

func (s *Server) Start() error {
    // Load initial registry
    s.registry = loadProjectsRegistry()

    // Spawn actors for all registered projects
    for _, project := range s.registry.Projects {
        if err := s.spawnActor(project); err != nil {
            log.Printf("Failed to spawn actor for %s: %v", project.Path, err)
        }
    }

    // Watch registry file for changes
    registryPath := getProjectsRegistryPath()
    s.registryWatcher.Add(filepath.Dir(registryPath))

    go s.watchRegistry()

    return nil
}

func (s *Server) watchRegistry() {
    debounce := NewDebouncer(500 * time.Millisecond)
    registryPath := getProjectsRegistryPath()

    for {
        select {
        case event := <-s.registryWatcher.Events:
            if event.Name == registryPath && event.Op&fsnotify.Write == fsnotify.Write {
                debounce.Trigger(func() {
                    s.syncActors()
                })
            }

        case err := <-s.registryWatcher.Errors:
            log.Printf("Registry watcher error: %v", err)

        case <-s.stopCh:
            return
        }
    }
}

func (s *Server) syncActors() {
    newRegistry := loadProjectsRegistry()

    s.actorsMu.Lock()
    defer s.actorsMu.Unlock()

    // Build map of new projects
    newProjects := make(map[string]*RegisteredProject)
    for _, p := range newRegistry.Projects {
        newProjects[p.Path] = p
    }

    // Stop actors for removed projects
    for path, actor := range s.actors {
        if _, exists := newProjects[path]; !exists {
            log.Printf("Project removed from registry: %s", path)
            actor.Stop()
            delete(s.actors, path)
        }
    }

    // Start actors for new projects
    for path, project := range newProjects {
        if _, exists := s.actors[path]; !exists {
            log.Printf("New project registered: %s", path)
            if err := s.spawnActor(project); err != nil {
                log.Printf("Failed to spawn actor: %v", err)
            }
        }
    }

    s.registry = newRegistry
}

func (s *Server) spawnActor(project *RegisteredProject) error {
    actor, err := NewActor(s.ctx, project)
    if err != nil {
        return err
    }

    if err := actor.Start(); err != nil {
        return err
    }

    s.actors[project.Path] = actor
    return nil
}
```

## DBHolder Pattern

MCP servers need thread-safe, swappable access to SQLite DB (for live branch switching).

### Interfaces

```go
// internal/mcp/db_holder.go

// DBProvider is a read-only interface for tools to access the database.
// Passed to MCP tool registration functions.
type DBProvider interface {
    // GetDB returns the current database connection and branch name.
    // Returns error if database not initialized (project not indexed).
    GetDB() (*sql.DB, string, error)
}

// DBManager extends DBProvider with write operations for the MCP server.
// Allows server to swap database connections (e.g., on branch switches).
type DBManager interface {
    DBProvider

    // SetDB atomically swaps the database connection.
    // Closes the previous connection if any.
    SetDB(db *sql.DB, branch string)

    // Close closes the current database connection.
    Close()
}

// dbHolder implements both interfaces with thread-safe access.
type dbHolder struct {
    mu            sync.RWMutex
    db            *sql.DB
    currentBranch string
}

// NewDBHolder creates a new DBHolder (initially nil DB).
func NewDBHolder() DBManager {
    return &dbHolder{}
}

func (h *dbHolder) GetDB() (*sql.DB, string, error) {
    h.mu.RLock()
    defer h.mu.RUnlock()

    if h.db == nil {
        return nil, "", fmt.Errorf("project not indexed - run 'cortex index' to enable search")
    }

    return h.db, h.currentBranch, nil
}

func (h *dbHolder) SetDB(db *sql.DB, branch string) {
    h.mu.Lock()
    defer h.mu.Unlock()

    // Close old connection
    if h.db != nil {
        h.db.Close()
    }

    h.db = db
    h.currentBranch = branch
}

func (h *dbHolder) Close() {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.db != nil {
        h.db.Close()
        h.db = nil
    }
}
```

### MCP Server Usage

```go
// internal/mcp/server.go

type Server struct {
    mcpServer   *server.MCPServer
    dbManager   DBManager
    projectPath string
    gitWatcher  watcher.GitWatcher
}

func NewMCPServer(ctx context.Context, projectPath string) (*Server, error) {
    s := &Server{
        dbManager:   NewDBHolder(),
        projectPath: projectPath,
    }

    // Create mcp-go server
    s.mcpServer = server.NewMCPServer(/* ... */)

    // Register tools with DBProvider interface (read-only)
    AddCortexSearchTool(s.mcpServer, s.dbManager)
    AddCortexExactTool(s.mcpServer, s.dbManager)
    AddCortexGraphTool(s.mcpServer, s.dbManager)
    AddCortexFilesTool(s.mcpServer, s.dbManager)

    // Initialize DB asynchronously
    go s.initializeDB(ctx)

    // Start git watcher for live branch switching
    go s.watchBranch(ctx)

    return s, nil
}

func (s *Server) initializeDB(ctx context.Context) {
    // Detect current branch
    branch := cache.GetCurrentBranch(s.projectPath)

    // Compute DB path
    cacheKey, _ := cache.GetCacheKey(s.projectPath)
    dbPath := filepath.Join(cortexDir, "cache", cacheKey, "branches", branch+".db")

    // Wait for DB file to exist (indexer creates it)
    if err := s.waitForDBFile(ctx, dbPath); err != nil {
        log.Printf("Warning: DB not available: %v", err)
        return
    }

    // Open DB
    db, err := sql.Open("sqlite3", dbPath)
    if err != nil {
        log.Printf("Failed to open DB: %v", err)
        return
    }

    // Set DB (tools can now query)
    s.dbManager.SetDB(db, branch)

    log.Printf("MCP server initialized (branch: %s)", branch)
}

func (s *Server) waitForDBFile(ctx context.Context, dbPath string) error {
    // Check if already exists
    if _, err := os.Stat(dbPath); err == nil {
        return nil
    }

    // Watch parent directory for DB creation
    watcher, _ := fsnotify.NewWatcher()
    defer watcher.Close()

    watcher.Add(filepath.Dir(dbPath))

    timeout := time.After(30 * time.Second)

    for {
        select {
        case event := <-watcher.Events:
            if event.Name == dbPath && event.Op&fsnotify.Create == fsnotify.Create {
                // DB created, wait a bit for writes to finish
                time.Sleep(100 * time.Millisecond)
                return nil
            }

        case <-timeout:
            return fmt.Errorf("timeout waiting for database")

        case <-ctx.Done():
            return ctx.Err()
        }
    }
}

func (s *Server) watchBranch(ctx context.Context) {
    s.gitWatcher = watcher.NewGitWatcher(s.projectPath)

    s.gitWatcher.Start(ctx, func(oldBranch, newBranch string) {
        log.Printf("Branch switch detected: %s → %s", oldBranch, newBranch)

        // Compute new DB path
        cacheKey, _ := cache.GetCacheKey(s.projectPath)
        newDBPath := filepath.Join(cortexDir, "cache", cacheKey, "branches", newBranch+".db")

        // Wait for new branch DB (indexer prepares it)
        if err := s.waitForDBFile(ctx, newDBPath); err != nil {
            log.Printf("Failed to switch branch DB: %v", err)
            return
        }

        // Open new DB
        newDB, err := sql.Open("sqlite3", newDBPath)
        if err != nil {
            log.Printf("Failed to open new branch DB: %v", err)
            return
        }

        // Atomic swap (closes old DB automatically)
        s.dbManager.SetDB(newDB, newBranch)

        log.Printf("Switched to branch: %s", newBranch)
    })
}
```

### Tool Implementation

Tools use `DBProvider` interface (cannot call `SetDB`):

```go
// internal/mcp/search_tool.go

func AddCortexSearchTool(srv *server.MCPServer, dbProvider DBProvider) {
    tool := mcp.NewTool("cortex_search", /* schema */)

    handler := func(ctx context.Context, req map[string]interface{}) (interface{}, error) {
        // Get DB from provider
        db, branch, err := dbProvider.GetDB()
        if err != nil {
            return nil, err  // "project not indexed" error
        }

        // Query vector search
        results, err := queryVectorSearch(db, req["query"].(string))
        if err != nil {
            return nil, err
        }

        return results, nil
    }

    srv.AddTool(tool, handler)
}
```

**Type safety ensures tools cannot modify database connection.**

## CLI Commands

### cortex index

Register project and trigger indexing (streaming progress):

```go
// internal/cli/index.go

func runIndexCommand(ctx context.Context, projectPath string) error {
    // Ensure daemon is running
    if err := daemon.EnsureDaemon(ctx); err != nil {
        return fmt.Errorf("failed to start indexer daemon: %w", err)
    }

    // Check for project move and recover
    if err := RecoverMovedProject(projectPath); err != nil {
        log.Printf("Warning: failed to recover moved project: %v", err)
    }

    // Register project
    if err := RegisterProject(projectPath); err != nil {
        return fmt.Errorf("failed to register project: %w", err)
    }

    // Connect to daemon
    client := daemon.NewIndexerClient()

    // Stream indexing progress
    stream, err := client.Index(ctx, connect.NewRequest(&indexerv1.IndexRequest{
        ProjectPath: projectPath,
    }))
    if err != nil {
        return err
    }

    fmt.Printf("Indexing project: %s\n", projectPath)

    for stream.Receive() {
        msg := stream.Msg()

        switch msg.Phase {
        case indexerv1.IndexProgress_PHASE_DISCOVERING:
            fmt.Printf("Discovering files...\n")

        case indexerv1.IndexProgress_PHASE_INDEXING:
            fmt.Printf("\rIndexing: %d/%d files", msg.FilesProcessed, msg.FilesTotal)

        case indexerv1.IndexProgress_PHASE_EMBEDDING:
            fmt.Printf("\rGenerating embeddings: %d chunks", msg.ChunksGenerated)

        case indexerv1.IndexProgress_PHASE_COMPLETE:
            fmt.Printf("\n✓ Indexed %d files (%d chunks)\n", msg.FilesProcessed, msg.ChunksGenerated)
            fmt.Printf("✓ Indexer daemon is watching for changes\n")
            return nil
        }
    }

    return stream.Err()
}
```

**User experience:**
```bash
$ cortex index
Indexing project: /Users/joe/code/project-cortex
Discovering files...
Indexing: 1234/1234 files
Generating embeddings: 5678 chunks
✓ Indexed 1234 files (5678 chunks)
✓ Indexer daemon is watching for changes
```

### cortex indexer start

Start daemon (or no-op if running):

```bash
$ cortex indexer start
Indexer daemon started (socket: ~/.cortex/indexer.sock)
```

```bash
$ cortex indexer start
Indexer daemon already running
```

### cortex indexer stop

Stop daemon gracefully:

```go
func runIndexerStop(ctx context.Context) error {
    client := daemon.NewIndexerClient()

    // Send shutdown signal (not in protobuf API, use Unix signal)
    sockPath := daemon.GetDaemonSocketPath()

    // Find daemon PID (query status first)
    resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
    if err != nil {
        return fmt.Errorf("daemon not running")
    }

    pid := resp.Msg.Daemon.Pid

    // Send SIGTERM
    process, _ := os.FindProcess(int(pid))
    process.Signal(syscall.SIGTERM)

    // Wait for socket to disappear
    timeout := time.After(5 * time.Second)
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if _, err := os.Stat(sockPath); os.IsNotExist(err) {
                fmt.Println("Indexer daemon stopped")
                return nil
            }
        case <-timeout:
            return fmt.Errorf("daemon failed to stop within 5 seconds")
        }
    }
}
```

### cortex indexer status

Show daemon and project status:

```go
func runIndexerStatus(ctx context.Context) error {
    client := daemon.NewIndexerClient()

    resp, err := client.GetStatus(ctx, connect.NewRequest(&indexerv1.StatusRequest{}))
    if err != nil {
        fmt.Println("Indexer daemon: Not running")
        return nil
    }

    daemon := resp.Msg.Daemon

    fmt.Printf("Indexer Daemon: Running\n")
    fmt.Printf("  PID:       %d\n", daemon.Pid)
    fmt.Printf("  Uptime:    %s\n", formatDuration(daemon.UptimeSeconds))
    fmt.Printf("  Socket:    %s\n", daemon.SocketPath)
    fmt.Printf("\n")

    if len(resp.Msg.Projects) == 0 {
        fmt.Println("No projects registered")
        return nil
    }

    fmt.Printf("Watching %d projects:\n", len(resp.Msg.Projects))

    for _, p := range resp.Msg.Projects {
        status := "✓"
        statusMsg := fmt.Sprintf("%d files, indexed %s ago", p.FilesIndexed, formatTimeSince(p.LastIndexedAt))

        if p.IsIndexing {
            status = "⏳"
            statusMsg = fmt.Sprintf("indexing... (%s)", formatPhase(p.CurrentPhase))
        }

        fmt.Printf("  %s %s\n", status, p.Path)
        fmt.Printf("     Branch: %s, %s\n", p.CurrentBranch, statusMsg)
    }

    return nil
}
```

**Output:**
```
Indexer Daemon: Running
  PID:       12345
  Uptime:    2h 15m
  Socket:    ~/.cortex/indexer.sock

Watching 3 projects:
  ✓ /Users/joe/code/project-cortex
     Branch: main, 1234 files, indexed 5m ago
  ⏳ /Users/joe/code/other-project
     Branch: feature-branch, indexing... (embedding)
  ✓ /Users/joe/code/third-project
     Branch: main, 89 files, indexed 1h ago
```

### cortex indexer logs

Stream logs for projects:

```go
func runIndexerLogs(ctx context.Context, projectPath string, follow bool) error {
    client := daemon.NewIndexerClient()

    stream, err := client.StreamLogs(ctx, connect.NewRequest(&indexerv1.LogsRequest{
        ProjectPath: projectPath,  // Empty = all projects
        Follow:      follow,
    }))
    if err != nil {
        return err
    }

    for stream.Receive() {
        entry := stream.Msg()

        timestamp := time.UnixMilli(entry.Timestamp).Format("15:04:05")
        project := filepath.Base(entry.Project)

        fmt.Printf("[%s] [%s] %s: %s\n", timestamp, project, entry.Level, entry.Message)
    }

    return stream.Err()
}
```

**Usage:**
```bash
# Current project only
$ cortex indexer logs

# All projects
$ cortex indexer logs --all

# Follow mode (tail -f)
$ cortex indexer logs --follow
```

**Output:**
```
[14:22:15] [project-cortex] INFO: Indexed internal/mcp/server.go (234 chunks)
[14:22:16] [other-project] INFO: Branch switch detected: main → feature-branch
[14:22:17] [project-cortex] INFO: File changed: internal/indexer/actor.go
```

### cortex index --unwatch / --remove

Unregister project:

```go
func runUnregister(ctx context.Context, projectPath string, removeCache bool) error {
    client := daemon.NewIndexerClient()

    resp, err := client.UnregisterProject(ctx, connect.NewRequest(&indexerv1.UnregisterRequest{
        ProjectPath: projectPath,
        RemoveCache: removeCache,
    }))
    if err != nil {
        return err
    }

    if resp.Msg.Success {
        if removeCache {
            fmt.Printf("✓ Project removed and cache deleted\n")
        } else {
            fmt.Printf("✓ Project unwatched (cache preserved)\n")
        }
    }

    return nil
}
```

**Usage:**
```bash
# Stop watching, keep cache
$ cortex index --unwatch

# Stop watching, delete cache
$ cortex index --remove
```

## Error Handling

### Unindexed Project

When MCP tools are called before indexing:

```go
func (h *dbHolder) GetDB() (*sql.DB, string, error) {
    if h.db == nil {
        return nil, "", fmt.Errorf("project not indexed - run 'cortex index' to enable search")
    }
    return h.db, h.currentBranch, nil
}
```

**User sees:**
```
Error calling cortex_search: project not indexed - run 'cortex index' to enable search
```

**LLM response:**
> "It looks like this project hasn't been indexed yet. Please run `cortex index` in your terminal to enable semantic search."

### Indexing in Progress

Tools return partial results (eventual consistency):

```go
func queryVectorSearch(db *sql.DB, query string) ([]Result, error) {
    // Query works even if indexing incomplete
    rows, err := db.Query("SELECT * FROM vec_chunks WHERE ...")

    // Returns whatever chunks exist so far
    return parseResults(rows), nil
}
```

**User experience:** First query might return 100 results, second query (after indexing progresses) returns 500 results. This is acceptable.

### Daemon Startup Failure

If daemon fails to start (e.g., all ports taken, permission denied):

```go
func runIndexCommand(ctx context.Context, projectPath string) error {
    if err := daemon.EnsureDaemon(ctx); err != nil {
        return fmt.Errorf("failed to start indexer daemon: %w\nTry running 'cortex indexer stop' first", err)
    }
    // ...
}
```

**User sees:**
```
Error: failed to start indexer daemon: failed to listen on socket: permission denied
Try running 'cortex indexer stop' first
```

### Stale Socket Recovery

Automatic (no user intervention):

```go
func EnsureDaemon(ctx context.Context) error {
    if isDaemonHealthy(ctx, sockPath) {
        return nil
    }

    // Lock acquired

    // Remove stale socket
    os.Remove(sockPath)

    // Start fresh daemon
    // ...
}
```

## Performance Characteristics

### Memory Usage

**Daemon:**
- Base overhead: ~10 MB
- Per-actor overhead: ~5 MB (watchers + goroutine)
- 10 projects: ~60 MB total

**MCP Server:**
- Base overhead: ~5 MB
- DBHolder: <1 MB
- No in-memory indexes (queries SQLite directly)

**Total for 3 Claude tabs, 10 projects:**
- Daemon: 60 MB
- MCP servers: 3 × 5 MB = 15 MB
- **Total: 75 MB**

**vs. Old Model (3 tabs, no daemon):**
- Each MCP loads indexes: 3 × 100 MB = 300 MB
- **Savings: 225 MB (75%)**

### Startup Time

**First `cortex index` (cold start):**
- Start daemon: ~200ms
- Initial indexing: depends on project size (1000 files ~10-30s)
- Total: 10-30s

**Subsequent `cortex mcp` (warm start):**
- Check daemon health: <10ms
- Open SQLite DB: <50ms
- Total: <100ms

**Live branch switching:**
- Git watcher fires: <10ms
- Open new DB: <50ms
- Swap DBHolder: <1ms
- Total: <100ms (imperceptible to user)

### Query Performance

**No change from current implementation:**
- Vector search: 50-100ms (embedding + sqlite-vec)
- Exact search: 2-8ms (FTS5)
- Graph queries: 10-50ms (lazy-loaded)

## Testing Strategy

### Unit Tests

**DBHolder:**
```go
func TestDBHolder_ThreadSafety(t *testing.T)
func TestDBHolder_GetDB_Uninitialized(t *testing.T)
func TestDBHolder_SetDB_ClosesPrevious(t *testing.T)
```

**EnsureDaemon:**
```go
func TestEnsureDaemon_AlreadyRunning(t *testing.T)
func TestEnsureDaemon_StaleSocket(t *testing.T)
func TestEnsureDaemon_ConcurrentStarts(t *testing.T)
```

**Registry:**
```go
func TestRegisterProject_AlreadyRegistered(t *testing.T)
func TestUnregisterProject_RemoveCache(t *testing.T)
func TestRecoverMovedProject(t *testing.T)
```

### Integration Tests

**Actor lifecycle:**
```go
//go:build integration

func TestActor_BranchSwitch(t *testing.T) {
    // Create actor, start watchers
    // Simulate git branch switch
    // Verify DB swap, file watcher pause/resume
}

func TestActor_FileChanges(t *testing.T) {
    // Create actor
    // Modify files
    // Verify incremental indexing
}
```

**Registry sync:**
```go
func TestServer_RegistrySync(t *testing.T) {
    // Start server
    // Add project to registry
    // Verify actor spawned
    // Remove project from registry
    // Verify actor stopped
}
```

**RPC:**
```go
func TestRPC_Index_Streaming(t *testing.T) {
    // Start daemon
    // Call Index RPC
    // Verify progress stream
    // Verify completion
}

func TestRPC_GetStatus(t *testing.T) {
    // Start daemon with multiple projects
    // Call GetStatus
    // Verify response contains all projects
}
```

### End-to-End Tests

**Full workflow:**
```bash
# Start fresh (no daemon)
cortex index /path/to/project

# Verify daemon started
cortex indexer status

# Start MCP, query
cortex mcp  # In another terminal
# (MCP tools should work)

# Switch branch
git checkout feature-branch

# Verify MCP still works (live switching)

# Stop daemon
cortex indexer stop

# Verify MCP tools fail with helpful error
```

## Migration Path

### From Current Architecture

**Current:** Per-MCP file watching, in-memory vector DB (chromem-go), JSON chunk files.

**New:** Single indexer daemon, SQLite storage, stateless MCP servers.

**Migration steps:**

1. **Phase 1: SQLite storage (already done)**
   - Chunks stored in SQLite with sqlite-vec
   - FTS5 for exact search
   - Graph in SQLite tables

2. **Phase 2: Indexer daemon (this spec)**
   - Implement daemon, actors, RPC
   - Keep existing `cortex mcp` working
   - Add `cortex indexer` commands

3. **Phase 3: MCP server refactor**
   - Remove chromem-go dependency
   - Add DBHolder pattern
   - Remove file watching from MCP
   - Query SQLite directly

4. **Phase 4: Cleanup**
   - Remove `cortex index --watch` flag
   - Remove old JSON chunk writing code
   - Update documentation

**Backward compatibility:** Existing `.cortex/cache/` directories work as-is (SQLite format unchanged).

## Non-Goals

This specification does NOT cover:

- **Multi-user daemon**: Single-user, single-machine only
- **Remote indexing**: No network protocol (Unix socket only)
- **Distributed indexing**: No coordination across machines
- **Real-time collaboration**: No shared state between users
- **IDE integration**: MCP is the integration layer
- **Custom indexing plugins**: Fixed indexer implementation
- **Incremental embedding updates**: Full re-embedding on file change

## Future Enhancements

**Potential additions (not in initial implementation):**

1. **Metrics endpoint**: Expose indexing stats, query latency
2. **Web UI**: Browser-based dashboard for daemon status
3. **Configurable indexing**: Per-project .cortexignore files
4. **Embedding caching**: Reuse embeddings for unchanged chunks
5. **Multi-repo workspaces**: Single daemon, multiple related projects
6. **Resource limits**: CPU/memory caps per actor

## References

### Existing Code Patterns

- **Git watching**: `internal/watcher/git_watcher.go`
- **File watching**: `internal/watcher/file_watcher.go`
- **Actor coordination**: `internal/watcher/coordinator.go`
- **Cache key computation**: `internal/cache/key.go`
- **Branch detection**: `internal/cache/branch.go`
- **Branch synchronization**: `internal/indexer/branch_synchronizer.go`
- **MCP tool registration**: `internal/mcp/server.go`
- **Change detection**: `internal/indexer/change_detector.go`
- **File processing**: `internal/indexer/processor.go`

### Related Specifications

- **Supersedes**: `specs/2025-10-29_auto-daemon.md` (HTTP/SSE approach, per-project daemons)
- **Builds on**: SQLite storage migration (completed, no formal spec)
- **Related**: `specs/2025-10-26_indexer.md` (original indexer design)

## Conclusion

The indexer daemon architecture provides:

✅ **Zero-config indexing**: Run `cortex index` once, forget about it
✅ **Resource efficiency**: 75% memory savings vs per-process watching
✅ **Live updates**: Files and branches stay in sync automatically
✅ **Crash resilience**: Self-healing process management
✅ **Type-safe RPC**: Protobuf schemas prevent API drift
✅ **Clean separation**: Daemon writes, MCP reads (no coordination needed)
✅ **Extensible**: ConnectRPC makes adding features straightforward

**Next steps:**
1. Implement protobuf schemas and generate code
2. Build EnsureDaemon and process coordination
3. Implement daemon server with actor model
4. Add ConnectRPC handlers for Index, GetStatus, StreamLogs
5. Refactor MCP server to use DBHolder pattern
6. Add CLI commands for daemon management
7. Write integration tests for full workflow
