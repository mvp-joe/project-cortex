---
status: planned
started_at: null
completed_at: null
dependencies: [indexer, mcp-server, cortex-embed]
---

# Auto-Daemon MCP Server Architecture

## Purpose

The auto-daemon architecture enables resource-efficient, zero-configuration MCP server deployment by automatically managing daemon lifecycle based on active sessions. It eliminates manual daemon management, reduces memory overhead for multi-session usage, and maintains the simplicity of stdio-based MCP while providing the benefits of a shared, long-running server.

## Core Concept

**Problem**: Multiple Claude sessions create multiple `cortex mcp` processes, each loading indexes into memory (N sessions = N × memory). Manual daemon management is complex and error-prone.

**Solution**: Auto-starting daemon with process coordination. First session starts daemon, subsequent sessions connect to it, last session shuts it down. Zero user intervention.

**Input**: `cortex mcp` invocation via stdio (from Claude/MCP client)

**Process**: Check for running daemon → Start if needed OR connect to existing → Proxy stdio ↔ HTTP/SSE → Monitor via heartbeat → Shutdown when unused

**Output**: Shared MCP server state across all sessions, automatic cleanup, graceful degradation

## Technology Stack

- **Language**: Go 1.25+
- **MCP Transport**: HTTP/SSE (Server-Sent Events per MCP spec)
- **Process Coordination**: JSON registry (`~/.cortex/sessions.json`) + file locking
- **Heartbeat**: HTTP POST every 10s, 30s timeout
- **Port Range**: 5173-5273 (100 ports, sequential allocation)
- **Dependencies**: `github.com/mark3labs/mcp-go` for MCP protocol

## Architecture Modes

### Mode 1: Embedded (Stdio, Per-Session)

**When used:**
- Daemon startup fails (all ports taken, permission denied)
- Explicit fallback mode (`cortex mcp --embedded`)
- Development/debugging

**Characteristics:**
- One process per session
- Loads indexes in-process
- Watches files directly
- No daemon coordination
- Current behavior (backward compatible)

**Memory**: N sessions × 50-100MB = high overhead

### Mode 2: Daemon (HTTP/SSE, Shared State)

**When used:**
- Default mode (auto-detected)
- Daemon running on localhost

**Characteristics:**
- One daemon process per project
- Multiple sessions connect to daemon
- Shared indexes (chromem-go, bleve, SQLite)
- Daemon watches files, updates in-memory
- Sessions are thin proxies (stdio ↔ HTTP)

**Memory**: 1 daemon × 50-100MB + N sessions × 5MB = low overhead

### Auto-Detection Logic

```go
func main() {
    // Check if daemon is running
    daemon, err := discoverDaemon()
    if err == nil && daemon.IsHealthy() {
        // Daemon exists and is healthy → Connect
        return runProxyMode(daemon)
    }

    // Try to start daemon
    daemon, err = startDaemon()
    if err != nil {
        // Daemon startup failed → Fallback to embedded
        log.Printf("Daemon startup failed (%v), using embedded mode", err)
        return runEmbeddedMode()
    }

    // Connect to newly started daemon
    return runProxyMode(daemon)
}
```

**User experience**: Transparent. First session starts daemon, subsequent sessions connect instantly.

## Process Coordination

### Session Registry

**File**: `~/.cortex/sessions.json`

**Structure:**
```json
{
  "projects": {
    "/Users/joe/code/project-cortex": {
      "daemon_pid": 12345,
      "daemon_port": 5173,
      "daemon_started_at": "2025-10-29T10:30:00Z",
      "last_heartbeat": "2025-10-29T10:35:42Z",
      "sessions": [
        {
          "pid": 12346,
          "started_at": "2025-10-29T10:30:15Z",
          "last_heartbeat": "2025-10-29T10:35:42Z"
        },
        {
          "pid": 12347,
          "started_at": "2025-10-29T10:32:00Z",
          "last_heartbeat": "2025-10-29T10:35:41Z"
        }
      ]
    },
    "/Users/joe/code/other-project": {
      "daemon_pid": 12350,
      "daemon_port": 5174,
      "sessions": [...]
    }
  }
}
```

**Schema:**

```go
type SessionRegistry struct {
    Projects map[string]*ProjectDaemon `json:"projects"`
}

type ProjectDaemon struct {
    DaemonPID        int               `json:"daemon_pid"`
    DaemonPort       int               `json:"daemon_port"`
    DaemonStartedAt  time.Time         `json:"daemon_started_at"`
    LastHeartbeat    time.Time         `json:"last_heartbeat"`
    Sessions         []*Session        `json:"sessions"`
}

type Session struct {
    PID           int       `json:"pid"`
    StartedAt     time.Time `json:"started_at"`
    LastHeartbeat time.Time `json:"last_heartbeat"`
}
```

### File Locking

**Problem**: Two sessions start simultaneously, both see no daemon, both try to start daemon → race condition.

**Solution**: Exclusive file lock during daemon startup.

```go
import "github.com/gofrs/flock"

func startDaemon() (*Daemon, error) {
    // Acquire exclusive lock
    lockFile := filepath.Join(cortexDir, "sessions.lock")
    lock := flock.New(lockFile)

    if err := lock.Lock(); err != nil {
        return nil, fmt.Errorf("failed to acquire lock: %w", err)
    }
    defer lock.Unlock()

    // Critical section: check registry and start daemon
    registry := loadRegistry()
    daemon := registry.Projects[projectPath]

    if daemon != nil && isDaemonHealthy(daemon) {
        // Another process started daemon while we waited for lock
        return &Daemon{Port: daemon.DaemonPort}, nil
    }

    // Start new daemon
    port, err := findAvailablePort(5173)
    if err != nil {
        return nil, err
    }

    cmd := exec.Command("cortex", "mcp", "--daemon", "--port", strconv.Itoa(port))
    cmd.Env = append(os.Environ(), fmt.Sprintf("CORTEX_PROJECT_ROOT=%s", projectPath))

    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("failed to start daemon: %w", err)
    }

    // Wait for daemon to become healthy
    daemon = &Daemon{
        PID:  cmd.Process.Pid,
        Port: port,
    }

    if err := waitForHealthy(daemon, 10*time.Second); err != nil {
        cmd.Process.Kill()
        return nil, fmt.Errorf("daemon failed to start: %w", err)
    }

    // Register daemon in registry
    registry.Projects[projectPath] = &ProjectDaemon{
        DaemonPID:       daemon.PID,
        DaemonPort:      port,
        DaemonStartedAt: time.Now(),
        LastHeartbeat:   time.Now(),
        Sessions:        []*Session{},
    }
    saveRegistry(registry)

    return daemon, nil
}
```

**Lock guarantees:**
- Only one process can start daemon for a project
- Others wait on lock, then connect to existing daemon
- Lock released after daemon is registered

### Port Allocation

**Strategy**: Sequential search starting at 5173.

```go
func findAvailablePort(startPort int) (int, error) {
    for port := startPort; port < startPort+100; port++ {
        // Try to bind to port
        ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
        if err == nil {
            ln.Close()
            return port, nil
        }
    }
    return 0, errors.New("no available ports in range 5173-5273")
}
```

**Port range**: 5173-5273 (100 ports)

**Rationale**: Allows 100 concurrent projects (more than enough for typical usage).

**Conflict handling**: If port is taken by non-Cortex process, skip to next port.

### Multi-Project Isolation

**Each project gets its own daemon:**

```
~/.cortex/sessions.json:
  /Users/joe/code/project-cortex → daemon on port 5173
  /Users/joe/code/other-project  → daemon on port 5174
```

**Why not multi-tenant (one daemon for all projects)?**

❌ **Rejected**: Single daemon serving multiple projects

**Reasons:**
1. **Complexity**: Routing requests by project, managing multiple file watchers
2. **Isolation**: Crash in one project affects all projects
3. **Memory**: Savings are minimal (shared Go runtime, but indexes are per-project)
4. **Simplicity**: One daemon per project is easier to reason about

✅ **Chosen**: One daemon per project

**Benefits:**
1. **Simple**: Each daemon watches one project's files
2. **Isolated**: Crashes don't affect other projects
3. **Independent shutdown**: Daemon shuts down when project unused
4. **No routing**: No need to multiplex requests by project

**Memory trade-off**: If user has 10 projects open simultaneously, 10 daemons running (~500MB total). Acceptable for development machines.

## Daemon Lifecycle

### Startup Sequence

**Session perspective:**

```
1. cortex mcp invoked by Claude
2. Detect project root (find .git or .cortex/)
3. Load ~/.cortex/sessions.json
4. Check if daemon exists for this project:
   a. If yes and healthy → Connect (skip to 8)
   b. If yes but stale → Clean up, start new daemon (continue to 5)
   c. If no → Start new daemon (continue to 5)
5. Acquire file lock (~/.cortex/sessions.lock)
6. Find available port (5173-5273)
7. Spawn daemon process: cortex mcp --daemon --port {port}
8. Wait for daemon health (poll GET /health every 500ms, timeout 10s)
9. Register self in sessions list
10. Start heartbeat goroutine (POST /heartbeat every 10s)
11. Proxy stdio ↔ HTTP/SSE
12. Listen for shutdown signal (SIGTERM, SIGINT)
```

**Daemon perspective:**

```
1. cortex mcp --daemon --port {port} invoked
2. Calculate cache key (hash of git remote + worktree path)
3. Load .cortex/settings.local.json (get cache location)
4. Detect current branch (read .git/HEAD)
5. Open SQLite cache (~/.cortex/cache/{key}/branches/{branch}.db)
6. Build in-memory indexes from SQLite:
   - chromem-go vector index (from chunks table)
   - bleve full-text index (from chunks table)
   - Graph builder ready (builds on-demand from relational data)
7. Start file watcher (watch project source files)
8. Start branch watcher (watch .git/HEAD)
9. Start HTTP/SSE server on port
10. Start heartbeat monitor goroutine (check sessions every 10s)
11. Serve MCP requests via HTTP/SSE
12. On file change: Re-index → Update SQLite → Rebuild indexes
13. On branch switch: Swap SQLite DB → Rebuild indexes
14. On shutdown signal: Close SQLite, exit gracefully
```

### Shutdown Sequence

**Session perspective (graceful):**

```
1. Receive SIGTERM or SIGINT
2. Send final heartbeat to daemon (POST /heartbeat)
3. Remove self from sessions list
4. Check if sessions list is now empty:
   a. If yes: Send shutdown to daemon (POST /shutdown)
   b. If no: Daemon stays running
5. Exit
```

**Session perspective (crash):**

```
1. Process crashes (no cleanup)
2. Session entry remains in registry
3. Daemon detects stale heartbeat after 30s
4. Daemon removes stale session from registry
5. Daemon checks if sessions list is empty:
   a. If yes: Flush indexes, shutdown
   b. If no: Continue running
```

**Daemon perspective (graceful):**

```
1. Receive shutdown request (POST /shutdown) OR all sessions stale
2. Close SQLite cache connection (automatic WAL checkpoint)
3. Close file watchers
4. Close branch watcher
5. Close HTTP server
6. Remove self from registry
7. Exit with code 0
```

**Daemon perspective (crash):**

```
1. Daemon process crashes
2. Registry contains stale daemon entry
3. Next session startup detects stale daemon:
   a. Checks health (fails)
   b. Checks last heartbeat (>30s ago)
   c. Removes stale entry from registry
   d. Starts new daemon
```

### Heartbeat Protocol

**Purpose**: Detect crashed sessions and daemons.

**Interval**: 10 seconds

**Timeout**: 30 seconds (3 missed heartbeats)

**Session → Daemon:**

```go
func (s *Session) heartbeatLoop() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if err := s.sendHeartbeat(); err != nil {
                log.Printf("Heartbeat failed: %v", err)
                // Daemon might be down, try to reconnect
                if err := s.reconnect(); err != nil {
                    log.Fatalf("Cannot reconnect to daemon: %v", err)
                }
            }
        case <-s.shutdown:
            s.sendFinalHeartbeat()
            return
        }
    }
}

func (s *Session) sendHeartbeat() error {
    req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/heartbeat", s.daemonPort), nil)
    req.Header.Set("X-Session-PID", strconv.Itoa(os.Getpid()))

    resp, err := s.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("heartbeat failed: %d", resp.StatusCode)
    }

    return nil
}
```

**Daemon → Session monitoring:**

```go
func (d *Daemon) heartbeatMonitor() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            d.checkStaleSessions()
        case <-d.shutdown:
            return
        }
    }
}

func (d *Daemon) checkStaleSessions() {
    registry := d.loadRegistry()
    project := registry.Projects[d.projectPath]

    var activeSessions []*Session
    for _, session := range project.Sessions {
        age := time.Since(session.LastHeartbeat)
        if age > 30*time.Second {
            log.Printf("Session %d is stale (last heartbeat %v ago), removing", session.PID, age)
            continue
        }
        activeSessions = append(activeSessions, session)
    }

    project.Sessions = activeSessions

    // If no active sessions, shutdown daemon
    if len(activeSessions) == 0 {
        log.Println("No active sessions, shutting down daemon")
        d.shutdown <- true
    }

    d.saveRegistry(registry)
}
```

**Heartbeat handler (daemon side):**

```go
func (d *Daemon) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
    pidStr := r.Header.Get("X-Session-PID")
    pid, err := strconv.Atoi(pidStr)
    if err != nil {
        http.Error(w, "Invalid PID", http.StatusBadRequest)
        return
    }

    // Update session heartbeat
    registry := d.loadRegistry()
    project := registry.Projects[d.projectPath]

    for _, session := range project.Sessions {
        if session.PID == pid {
            session.LastHeartbeat = time.Now()
            break
        }
    }

    // Update daemon heartbeat
    project.LastHeartbeat = time.Now()

    d.saveRegistry(registry)

    w.WriteHeader(http.StatusOK)
}
```

## MCP Transport (HTTP/SSE)

### Why HTTP/SSE?

**MCP spec defines multiple transports:**
1. Stdio (current Cortex implementation)
2. HTTP with Server-Sent Events (SSE)
3. WebSocket (not in spec, community extension)

**Why not keep stdio for daemon?**
- Stdio requires parent-child process relationship
- Daemon is independent process (not child of session)
- HTTP/SSE is standard MCP transport for networked servers

**SSE benefits:**
- Server can push notifications to client (MCP spec uses this for progress, logs)
- Long-lived connection (like stdio)
- Standard HTTP (no WebSocket complexity)

### HTTP/SSE Implementation

**Endpoints:**

```
POST /rpc              - MCP JSON-RPC requests (tools/*, resources/*, prompts/*)
GET  /sse              - Server-Sent Events stream (notifications, progress)
GET  /health           - Health check (used by sessions)
POST /heartbeat        - Session heartbeat
POST /shutdown         - Graceful shutdown request
```

**MCP RPC Handler:**

```go
func (d *Daemon) handleRPC(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // Read JSON-RPC request
    var req mcp.JSONRPCRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON-RPC request", http.StatusBadRequest)
        return
    }

    // Route to MCP server
    ctx := r.Context()
    resp, err := d.mcpServer.HandleRequest(ctx, &req)
    if err != nil {
        // Internal error
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]interface{}{
            "jsonrpc": "2.0",
            "id":      req.ID,
            "error": map[string]interface{}{
                "code":    -32603,
                "message": err.Error(),
            },
        })
        return
    }

    // Success
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}
```

**SSE Stream Handler:**

```go
func (d *Daemon) handleSSE(w http.ResponseWriter, r *http.Request) {
    // Set SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
        return
    }

    // Subscribe to daemon events
    events := d.eventBus.Subscribe()
    defer d.eventBus.Unsubscribe(events)

    for {
        select {
        case event := <-events:
            // Send SSE event
            fmt.Fprintf(w, "event: %s\n", event.Type)
            fmt.Fprintf(w, "data: %s\n\n", event.Data)
            flusher.Flush()

        case <-r.Context().Done():
            // Client disconnected
            return
        }
    }
}
```

**Health Check Handler:**

```go
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
    health := map[string]interface{}{
        "status":         "healthy",
        "uptime_seconds": int(time.Since(d.startedAt).Seconds()),
        "sessions_count": len(d.getSessions()),
        "cache": map[string]interface{}{
            "cache_key":      d.cacheKey,
            "current_branch": d.currentBranch,
            "cache_path":     d.cachePath,
        },
        "indexes": map[string]interface{}{
            "chunks_count":      d.chunksCount(),      // From chunks table
            "files_count":       d.filesCount(),       // From files table
            "types_count":       d.typesCount(),       // From types table
            "functions_count":   d.functionsCount(),   // From functions table
            "last_reload":       d.indexesLastReload,
        },
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(health)
}
```

### Proxy Mode (Session Side)

**Session acts as stdio ↔ HTTP proxy:**

```go
func runProxyMode(daemon *Daemon) error {
    // Create HTTP client
    client := &http.Client{Timeout: 60 * time.Second}

    // Read JSON-RPC from stdin
    scanner := bufio.NewScanner(os.Stdin)
    for scanner.Scan() {
        line := scanner.Bytes()

        // Parse JSON-RPC request
        var req mcp.JSONRPCRequest
        if err := json.Unmarshal(line, &req); err != nil {
            log.Printf("Invalid JSON-RPC: %v", err)
            continue
        }

        // Forward to daemon
        resp, err := client.Post(
            fmt.Sprintf("http://127.0.0.1:%d/rpc", daemon.Port),
            "application/json",
            bytes.NewReader(line),
        )
        if err != nil {
            log.Printf("Daemon request failed: %v", err)
            continue
        }

        // Read response
        respBody, _ := io.ReadAll(resp.Body)
        resp.Body.Close()

        // Write to stdout
        os.Stdout.Write(respBody)
        os.Stdout.Write([]byte("\n"))
    }

    return scanner.Err()
}
```

**Benefits:**
- Session is thin proxy (~5MB memory)
- All heavy lifting in daemon (indexes, parsing, embeddings)
- Stdio interface preserved (Claude/MCP client sees no difference)

## Storage Patterns

### Daemon In-Memory State

**Daemon loads ONE unified SQLite cache and builds indexes from it:**

```go
type Daemon struct {
    // MCP server
    mcpServer *server.MCPServer

    // Unified cache (loaded from ~/.cortex/cache/{key}/branches/{branch}.db)
    cacheDB *sql.DB

    // In-memory indexes (built from cacheDB)
    chunksSearcher  *ChromemSearcher  // chromem-go vector DB (built from chunks table)
    exactSearcher   *BleveSearcher    // bleve full-text index (built from chunks table)
    graphBuilder    *GraphBuilder     // Builds in-memory graph from relational data

    // File watcher
    watcher       *FileWatcher   // Watches project source files
    branchWatcher *BranchWatcher // Watches .git/HEAD for branch switches

    // Cache identification
    cacheKey     string // {remote-hash}-{worktree-hash}
    cachePath    string // ~/.cortex/cache/{key}
    currentBranch string

    // Process coordination
    projectPath string
    port        int
    pid         int
    startedAt   time.Time

    // Shutdown
    shutdown chan bool
}
```

**Storage location:** `~/.cortex/cache/{cache-key}/branches/{branch}.db`

**Cache key:** Hash of git remote + worktree path (e.g., `a1b2c3d4-e5f6g7h8`)

**SQLite schema (11 tables):**
- **files** - File metadata, line counts, hashes
- **types** - Interfaces, structs, classes
- **type_fields** - Struct fields, interface methods
- **functions** - Standalone and method functions
- **function_parameters** - Parameters and return values
- **type_relationships** - Implements, embeds edges
- **function_calls** - Call graph edges
- **imports** - Import declarations
- **chunks** - Semantic search chunks with embeddings
- **modules** - Aggregated package/module statistics
- **cache_metadata** - Cache configuration and stats

**Memory footprint:**
- chromem-go: ~30-50MB (10K chunks, built from chunks table)
- bleve: ~20-30MB (indexed chunks from chunks table)
- In-memory graph: ~5-10MB (built on-demand from types, functions, relationships tables)
- **Total: ~70-90MB per project**

**Shared across all sessions** → Memory savings scale with session count.

### Persistence Strategy

**The unified SQLite cache is the source of truth.** Daemon maintains in-memory indexes for fast queries but all data lives in SQLite.

**On file changes:**

```go
func (d *Daemon) handleFileChange(path string) {
    // 1. Re-index changed file
    chunks, graphData, fileStats := d.indexer.ProcessFile(path)

    // 2. Update SQLite cache (within transaction)
    tx, _ := d.cacheDB.Begin()
    d.updateFileInCache(tx, path, chunks, graphData, fileStats)
    tx.Commit()

    // 3. Rebuild in-memory indexes from updated SQLite data
    d.rebuildIndexes()
}
```

**Indexing triggers cache update and index rebuild:**

The indexer writes directly to the unified SQLite cache (not separate JSON files). This happens:

1. **On-demand (cortex index):** User runs indexer explicitly
2. **File watcher (daemon mode):** Daemon detects source file changes, re-indexes, updates cache
3. **Branch switch:** Daemon switches to different branch.db, rebuilds indexes

**Cache update flow:**

```go
func (d *Daemon) rebuildIndexes() {
    // Load chunks from SQLite
    rows, _ := d.cacheDB.Query("SELECT chunk_id, text, embedding FROM chunks")
    defer rows.Close()

    var chunks []*Chunk
    for rows.Next() {
        var chunk Chunk
        rows.Scan(&chunk.ID, &chunk.Text, &chunk.Embedding)
        chunks = append(chunks, &chunk)
    }

    // Rebuild chromem-go vector index
    d.chunksSearcher.RebuildFromChunks(chunks)

    // Rebuild bleve full-text index
    d.exactSearcher.RebuildFromChunks(chunks)

    // Graph is built on-demand from relational data (no preload needed)

    log.Printf("Indexes rebuilt from %d chunks", len(chunks))
}
```

**Trade-offs:**
- SQLite writes are transactional and atomic
- In-memory indexes rebuilt from SQLite on changes (~100-500ms for 10K chunks)
- No periodic writes needed (SQLite handles persistence)
- Crash recovery: SQLite transactions ensure consistency
- Branch isolation: Each branch has separate .db file

### File Watching

**Daemon watches project source files** (not cache directories):

```go
func (d *Daemon) startFileWatcher() error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }

    // Watch project source files
    // Walk project root, add all directories (excluding .git, node_modules, etc.)
    filepath.Walk(d.projectPath, func(path string, info os.FileInfo, err error) error {
        if info.IsDir() && !shouldIgnore(path) {
            watcher.Add(path)
        }
        return nil
    })

    go d.watchLoop(watcher)
    return nil
}

func (d *Daemon) watchLoop(watcher *fsnotify.Watcher) {
    debounce := NewDebouncer(500 * time.Millisecond)

    for {
        select {
        case event := <-watcher.Events:
            if event.Op&fsnotify.Write == fsnotify.Write {
                // Debounce rapid changes
                debounce.Trigger(func() {
                    d.handleFileChange(event.Name)
                })
            }

        case err := <-watcher.Errors:
            log.Printf("Watcher error: %v", err)

        case <-d.shutdown:
            watcher.Close()
            return
        }
    }
}

func (d *Daemon) handleFileChange(path string) {
    // Only process source files (ignore .cortex/, .git/, etc.)
    if !isSourceFile(path) {
        return
    }

    // 1. Re-index changed file
    chunks, graphData, fileStats := d.indexer.ProcessFile(path)

    // 2. Update unified SQLite cache (atomic transaction)
    tx, _ := d.cacheDB.Begin()
    d.updateFileInCache(tx, path, chunks, graphData, fileStats)
    tx.Commit()

    // 3. Rebuild in-memory indexes from updated cache
    d.rebuildIndexes()

    log.Printf("File %s updated in cache and indexes rebuilt", path)
}

func shouldIgnore(path string) bool {
    ignoredDirs := []string{".git", ".cortex", "node_modules", "vendor", ".next", "dist"}
    for _, ignored := range ignoredDirs {
        if strings.Contains(path, ignored) {
            return true
        }
    }
    return false
}

func isSourceFile(path string) bool {
    ext := filepath.Ext(path)
    supportedExts := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".rb"}
    for _, supported := range supportedExts {
        if ext == supported {
            return true
        }
    }
    return false
}
```

**What changed:**
- ❌ **Old:** Watch `.cortex/chunks/` and `.cortex/graph/` directories
- ✅ **New:** Watch project source files (`.go`, `.ts`, `.py`, etc.)
- ❌ **Old:** Reload from separate JSON/NDJSON files
- ✅ **New:** Update SQLite cache, rebuild indexes from cache

**Benefits:**
- Single file watcher (not N watchers for N sessions)
- Centralized re-indexing logic
- All sessions see updates simultaneously
- Cache updates are transactional and atomic

**Debouncing:**
- Multiple file changes trigger one cache update (after 500ms quiet)
- Prevents reload storms during bulk edits or `git checkout`

### Branch Management

**Daemon maintains branch-specific cache and detects branch switches.**

Each branch gets its own SQLite database: `~/.cortex/cache/{cache-key}/branches/{branch}.db`

**Branch detection on startup:**

```go
func (d *Daemon) detectBranch() (string, error) {
    // Read .git/HEAD
    headData, err := os.ReadFile(filepath.Join(d.projectPath, ".git", "HEAD"))
    if err != nil {
        return "", err
    }

    // Parse ref (e.g., "ref: refs/heads/main")
    headStr := string(headData)
    if strings.HasPrefix(headStr, "ref: refs/heads/") {
        branch := strings.TrimPrefix(headStr, "ref: refs/heads/")
        return strings.TrimSpace(branch), nil
    }

    // Detached HEAD state
    return "detached", nil
}
```

**Branch watcher (.git/HEAD):**

```go
func (d *Daemon) startBranchWatcher() error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }

    // Watch .git/HEAD for branch switches
    headPath := filepath.Join(d.projectPath, ".git", "HEAD")
    watcher.Add(headPath)

    go d.branchWatchLoop(watcher)
    return nil
}

func (d *Daemon) branchWatchLoop(watcher *fsnotify.Watcher) {
    for {
        select {
        case event := <-watcher.Events:
            if event.Op&fsnotify.Write == fsnotify.Write {
                d.handleBranchSwitch()
            }

        case err := <-watcher.Errors:
            log.Printf("Branch watcher error: %v", err)

        case <-d.shutdown:
            watcher.Close()
            return
        }
    }
}
```

**Branch switch handler:**

```go
func (d *Daemon) handleBranchSwitch() {
    newBranch, err := d.detectBranch()
    if err != nil {
        log.Printf("Failed to detect branch: %v", err)
        return
    }

    if newBranch == d.currentBranch {
        // No change (spurious write to .git/HEAD)
        return
    }

    log.Printf("Branch switch detected: %s → %s", d.currentBranch, newBranch)

    // 1. Close current branch DB
    d.cacheDB.Close()

    // 2. Load new branch DB
    newDBPath := filepath.Join(d.cachePath, "branches", newBranch+".db")
    newDB, err := sql.Open("sqlite3", newDBPath)
    if err != nil {
        log.Printf("Failed to open branch DB: %v", err)
        return
    }

    // 3. Update daemon state
    d.cacheDB = newDB
    d.currentBranch = newBranch

    // 4. Rebuild in-memory indexes from new branch DB
    d.rebuildIndexes()

    log.Printf("Switched to branch %s, indexes rebuilt", newBranch)
}
```

**Fast-path copying for new branches:**

When creating a new branch from an existing branch, the cache can be copied instead of re-indexed:

```go
func (d *Daemon) onNewBranch(newBranch, sourceBranch string) error {
    sourceDB := filepath.Join(d.cachePath, "branches", sourceBranch+".db")
    targetDB := filepath.Join(d.cachePath, "branches", newBranch+".db")

    // Copy source DB to target (fast, <1s for typical projects)
    data, _ := os.ReadFile(sourceDB)
    os.WriteFile(targetDB, data, 0644)

    log.Printf("Created branch cache %s from %s", newBranch, sourceBranch)
    return nil
}
```

**Incremental indexing after copy:**

After copying, only changed files need re-indexing. The daemon detects file changes and updates only modified chunks:

```go
func (d *Daemon) incrementalIndex(changedFiles []string) {
    for _, file := range changedFiles {
        d.handleFileChange(file)  // Updates cache + rebuilds indexes
    }
}
```

**Branch-specific queries:**

All MCP queries operate on the current branch's cache automatically. No client-side branch awareness needed.

**Benefits:**
- Branch isolation (no cross-branch pollution)
- Fast branch switching (<1s, just reload SQLite + rebuild indexes)
- Efficient new branch creation (copy existing cache)
- Automatic detection (no manual commands)

## Crash Scenarios & Recovery

### Scenario 1: Session Crashes

**What happens:**
1. Session process crashes (no cleanup)
2. Heartbeat stops
3. Daemon detects stale heartbeat after 30s
4. Daemon removes session from registry
5. If last session: Daemon shuts down after 30s

**Recovery:**
- Next session startup sees no daemon (or stale daemon)
- Starts new daemon
- Normal operation resumes

**Data loss:** None (daemon flushed on shutdown or periodically)

### Scenario 2: Daemon Crashes

**What happens:**
1. Daemon process crashes
2. Sessions detect connection failure
3. Sessions check registry: daemon stale
4. First session to recover:
   a. Acquires lock
   b. Cleans stale daemon entry
   c. Starts new daemon
5. Other sessions connect to new daemon

**Recovery:**
- Automatic (next RPC request triggers reconnection)
- Session retries with exponential backoff

**Data loss:** Max 60s of changes (or 100 file updates), re-indexed incrementally

### Scenario 3: Both Session and Daemon Crash

**What happens:**
1. Session crashes → stale entry in registry
2. Daemon crashes → stale entry in registry
3. Registry contains garbage

**Recovery:**
1. Next `cortex mcp` startup
2. Checks daemon health: fails
3. Checks last heartbeat: stale
4. Removes stale entries (both session and daemon)
5. Starts fresh daemon

**Data loss:** Max 60s of changes

### Scenario 4: Port Conflict

**What happens:**
1. Daemon tries to start on port 5173
2. Port already taken by another process (not Cortex)

**Recovery:**
1. Listen fails
2. Try next port (5174, 5175, ...)
3. Find available port in range 5173-5273
4. Start daemon on that port
5. Register port in registry

**Fallback:** If all 100 ports taken, fall back to embedded mode.

### Scenario 5: Registry Corruption

**What happens:**
1. Concurrent writes corrupt `sessions.json`
2. JSON parsing fails

**Recovery:**
1. Detect parse error
2. Backup corrupted file: `sessions.json.corrupt.{timestamp}`
3. Create new empty registry
4. Start fresh daemon

**Prevention:** Atomic writes (temp → rename), file locking during updates.

## Logging & Debugging

### Daemon Logs

**Location:** `~/.cortex/logs/daemon-{project-hash}.log`

**Project hash:** SHA-256 of project path (first 8 chars)

```go
func projectHash(path string) string {
    h := sha256.Sum256([]byte(path))
    return hex.EncodeToString(h[:4])  // First 8 hex chars
}

// Example: /Users/joe/code/project-cortex → a3f5b21c
```

**Log file:**
```
~/.cortex/logs/daemon-a3f5b21c.log
```

**Log format (structured JSON):**

```json
{"time":"2025-10-29T10:30:00Z","level":"info","msg":"Daemon started","port":5173,"pid":12345}
{"time":"2025-10-29T10:30:05Z","level":"info","msg":"Indexes loaded","chunks":1234,"graph_nodes":567,"stats_rows":89}
{"time":"2025-10-29T10:30:10Z","level":"info","msg":"File watcher started"}
{"time":"2025-10-29T10:35:42Z","level":"info","msg":"Heartbeat received","session_pid":12346}
{"time":"2025-10-29T10:40:00Z","level":"info","msg":"File changed","path":"internal/mcp/server.go"}
{"time":"2025-10-29T10:40:01Z","level":"info","msg":"Indexes reloaded","took_ms":120}
```

**Log rotation:**
- Max size: 10 MB
- Keep last 5 files
- Rotate on daemon startup if log >10MB

### User-Facing Commands

**Check daemon status:**

```bash
cortex mcp --status
```

**Output:**
```
Daemon Status:
  Project: /Users/joe/code/project-cortex
  Port:    5173
  PID:     12345
  Uptime:  2h 15m
  Sessions: 2 active

  Cache:
    Key:    a1b2c3d4-e5f6g7h8
    Branch: main
    Path:   ~/.cortex/cache/a1b2c3d4-e5f6g7h8/branches/main.db

  Indexes (in-memory, built from SQLite):
    Chunks:    1234
    Files:     89
    Types:     156
    Functions: 423
    Last reload: 5m ago

  Log: ~/.cortex/logs/daemon-a3f5b21c.log
```

**Stop daemon:**

```bash
cortex mcp --stop
```

**Output:**
```
Stopping daemon for project: /Users/joe/code/project-cortex
Daemon stopped (PID 12345)
```

**List all daemons:**

```bash
cortex mcp --list
```

**Output:**
```
Active Daemons:
  /Users/joe/code/project-cortex
    Port: 5173, PID: 12345, Sessions: 2

  /Users/joe/code/other-project
    Port: 5174, PID: 12350, Sessions: 1
```

### Debugging Tips

**Check if daemon is running:**
```bash
lsof -i :5173  # Check if port is listening
```

**View daemon logs:**
```bash
tail -f ~/.cortex/logs/daemon-a3f5b21c.log
```

**Inspect registry:**
```bash
cat ~/.cortex/sessions.json | jq
```

**Force cleanup (if stuck):**
```bash
rm ~/.cortex/sessions.json
cortex mcp --stop-all
```

## Implementation Phases

### Phase 1: Embedded Mode with Auto-Watch (1 week)

**Goal:** Eliminate manual `cortex index` (no daemon yet)

**Changes:**
- [ ] `cortex mcp` watches files directly (integrate watcher)
- [ ] Incremental index updates on file change
- [ ] Periodic write to disk (60s + 100 changes)
- [ ] Deprecate `cortex index --watch` (functionality merged)

**Benefits:**
- Solves UX problem (no more manual indexing)
- No daemon complexity yet
- Backward compatible

**Effort:** 5-7 days

### Phase 2: Session Registry & Coordination (1 week)

**Goal:** Build process coordination infrastructure

**Changes:**
- [ ] Session registry (`~/.cortex/sessions.json`)
- [ ] File locking for concurrent access
- [ ] Registry CRUD operations (load, save, update)
- [ ] Port allocation logic
- [ ] Health check endpoint
- [ ] Unit tests for registry operations

**Benefits:**
- Foundation for daemon auto-start
- No user-facing changes yet (prep work)

**Effort:** 5-7 days

### Phase 3: Daemon Mode (2 weeks)

**Goal:** Auto-starting daemon with HTTP/SSE transport

**Changes:**
- [ ] HTTP/SSE server implementation
- [ ] MCP JSON-RPC over HTTP
- [ ] SSE event stream
- [ ] Daemon startup logic (spawn process, wait for health)
- [ ] Proxy mode (stdio ↔ HTTP)
- [ ] Auto-detection (check registry, start or connect)
- [ ] Heartbeat protocol (send + monitor)
- [ ] Graceful shutdown (last session stops daemon)
- [ ] Crash recovery (stale session cleanup)
- [ ] Integration tests (multi-session scenarios)

**Benefits:**
- Memory savings (shared state)
- Auto-start/stop (zero config)
- Transparent to user

**Effort:** 10-14 days

### Phase 4: Polish & Observability (3-5 days)

**Goal:** User-facing commands, logging, debugging

**Changes:**
- [ ] Daemon logs (`~/.cortex/logs/daemon-{hash}.log`)
- [ ] `cortex mcp --status` command
- [ ] `cortex mcp --stop` command
- [ ] `cortex mcp --list` command
- [ ] Log rotation
- [ ] Structured logging (JSON format)
- [ ] Error message improvements
- [ ] Documentation updates (CLAUDE.md, README)

**Benefits:**
- Debuggability
- User confidence (can inspect daemon state)

**Effort:** 3-5 days

### Phase 5: Process Supervision (Optional, 2-3 days)

**Goal:** Auto-start daemon on system boot (advanced users)

**Changes:**
- [ ] systemd unit file (Linux)
- [ ] launchd plist (macOS)
- [ ] Installation scripts
- [ ] Documentation

**Benefits:**
- Daemon always running (instant session startup)
- Good for heavy users

**Effort:** 2-3 days

**Total effort: 4-6 weeks**

## Performance Characteristics

### Memory Usage

**Embedded mode (per session):**
- Process overhead: ~10 MB
- Indexes: ~100 MB
- Total: ~110 MB per session
- N sessions: N × 110 MB

**Daemon mode:**
- Daemon: ~100 MB (indexes)
- Session: ~5 MB (proxy only)
- Total: 100 MB + N × 5 MB

**Example (3 sessions):**
- Embedded: 3 × 110 MB = 330 MB
- Daemon: 100 MB + 3 × 5 MB = 115 MB
- **Savings: 65% (215 MB)**

**Scales with session count:** More sessions = more savings.

### Startup Time

**Embedded mode:**
- Load indexes: ~200ms
- Total: ~200ms per session

**Daemon mode (cold start):**
- First session: ~200ms (same as embedded)
- Subsequent sessions: <50ms (connect to daemon)

**Daemon mode (warm start):**
- All sessions: <50ms (daemon already running)

### Query Performance

**No difference:**
- Both modes serve from in-memory indexes
- Query time: <25ms (same performance)

**Network overhead:**
- HTTP round-trip: ~1-2ms (localhost)
- Negligible compared to query time

## User Experience

### Transparent Operation

**User perspective:**
1. Invoke `cortex mcp` (via Claude MCP config)
2. MCP server starts (no manual steps)
3. Queries are fast
4. Close Claude → Daemon stays running (other sessions)
5. Close all Claude windows → Daemon shuts down automatically

**No visible difference** between embedded and daemon mode.

### Configuration (Optional)

**Force embedded mode:**

```json
// .cortex/config.yml
mcp:
  mode: embedded  # Default: auto (try daemon, fallback to embedded)
```

**Custom port range:**

```json
mcp:
  port_range: [6000, 6100]  # Default: [5173, 5273]
```

**Disable auto-daemon:**

```json
mcp:
  auto_daemon: false  # Always use embedded mode
```

**Most users:** Don't configure anything (auto mode works).

## Security Considerations

### Localhost-Only

**All daemons bind to 127.0.0.1:**

```go
ln, err := net.Listen("tcp", "127.0.0.1:5173")
```

**No external access:** Daemons are not reachable from network.

### No Authentication

**Rationale:**
- Daemon is localhost-only
- Process isolation (user's own processes)
- Same security model as stdio MCP

**Risk:** If user runs untrusted code, it can connect to daemon (same risk as stdio).

### Registry Permissions

**File permissions:**

```go
// Create registry with user-only permissions
os.WriteFile("~/.cortex/sessions.json", data, 0600)  // rw-------
```

**No other users can read/write registry.**

### Process Isolation

**Each project gets own daemon:**
- No cross-project data leakage
- Crashes don't affect other projects

## Testing Strategy

### Unit Tests

**Registry operations:**
```go
func TestLoadRegistry(t *testing.T)
func TestSaveRegistry(t *testing.T)
func TestUpdateSession(t *testing.T)
func TestRemoveStaleSession(t *testing.T)
```

**Port allocation:**
```go
func TestFindAvailablePort(t *testing.T)
func TestFindAvailablePort_AllTaken(t *testing.T)
```

**Heartbeat:**
```go
func TestHeartbeatMonitor_RemoveStale(t *testing.T)
func TestHeartbeatMonitor_Shutdown(t *testing.T)
```

### Integration Tests

**Multi-session scenarios:**

```go
//go:build integration

func TestDaemon_AutoStart(t *testing.T) {
    // Start first session
    session1 := startSession(t)
    defer session1.Stop()

    // Verify daemon started
    registry := loadRegistry()
    assert.NotNil(t, registry.Projects[projectPath])
    assert.Equal(t, 1, len(registry.Projects[projectPath].Sessions))

    // Start second session
    session2 := startSession(t)
    defer session2.Stop()

    // Verify both sessions connected to same daemon
    registry = loadRegistry()
    assert.Equal(t, 2, len(registry.Projects[projectPath].Sessions))
    assert.Equal(t, session1.daemonPort, session2.daemonPort)
}

func TestDaemon_AutoShutdown(t *testing.T) {
    // Start session
    session := startSession(t)

    // Get daemon PID
    registry := loadRegistry()
    daemonPID := registry.Projects[projectPath].DaemonPID

    // Stop session
    session.Stop()

    // Wait for daemon shutdown (heartbeat timeout + shutdown)
    time.Sleep(35 * time.Second)

    // Verify daemon stopped
    _, err := os.FindProcess(daemonPID)
    assert.Error(t, err)  // Process should not exist
}

func TestDaemon_CrashRecovery(t *testing.T) {
    // Start session + daemon
    session1 := startSession(t)

    // Kill daemon (simulate crash)
    registry := loadRegistry()
    process, _ := os.FindProcess(registry.Projects[projectPath].DaemonPID)
    process.Kill()

    // Wait for session to detect crash
    time.Sleep(15 * time.Second)

    // Start new session
    session2 := startSession(t)

    // Verify new daemon started
    registry = loadRegistry()
    assert.NotEqual(t, 0, registry.Projects[projectPath].DaemonPID)

    // Verify session connected
    resp, err := session2.Query(ctx, "cortex_search", map[string]interface{}{
        "query": "test",
    })
    assert.NoError(t, err)
    assert.NotNil(t, resp)
}
```

**File watching:**

```go
func TestDaemon_FileReload(t *testing.T) {
    session := startSession(t)
    defer session.Stop()

    // Query initial state
    resp1, _ := session.Query(ctx, "cortex_search", map[string]interface{}{
        "query": "test",
    })
    count1 := len(resp1.Results)

    // Modify file
    os.WriteFile("test.go", []byte("// new content"), 0644)

    // Wait for reload (debounce + processing)
    time.Sleep(1 * time.Second)

    // Query updated state
    resp2, _ := session.Query(ctx, "cortex_search", map[string]interface{}{
        "query": "test",
    })
    count2 := len(resp2.Results)

    // Results should differ (file changed)
    assert.NotEqual(t, count1, count2)
}
```

### Load Tests

**Concurrent sessions:**

```go
func TestDaemon_ConcurrentSessions(t *testing.T) {
    // Start 10 sessions concurrently
    var wg sync.WaitGroup
    sessions := make([]*Session, 10)

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            sessions[i] = startSession(t)
        }(i)
    }

    wg.Wait()

    // Verify all connected to same daemon
    registry := loadRegistry()
    assert.Equal(t, 10, len(registry.Projects[projectPath].Sessions))

    // All sessions use same port
    port := sessions[0].daemonPort
    for _, s := range sessions {
        assert.Equal(t, port, s.daemonPort)
    }

    // Cleanup
    for _, s := range sessions {
        s.Stop()
    }
}
```

**Race detector:**

```bash
go test -race ./internal/daemon/...
```

## Future Enhancements

### Potential Additions

1. **Metrics endpoint**: Expose Prometheus metrics (query counts, latency, index size)
2. **Web UI**: Browser-based dashboard for daemon status and logs
3. **Remote daemons**: Connect to daemon on another machine (team sharing)
4. **Multi-user support**: Daemons with authentication for shared servers
5. **Resource limits**: CPU/memory caps per daemon
6. **Caching layer**: Redis cache for query results
7. **High availability**: Master-slave daemon setup for large teams

### Not Planned (Out of Scope)

1. **Multi-tenant single daemon**: Complexity outweighs benefits
2. **Daemon clustering**: Over-engineering for typical usage
3. **Built-in reverse proxy**: Users can use nginx/caddy if needed
4. **GUI installer**: CLI-first tool, not appropriate

## Migration Path

### Backward Compatibility

**Embedded mode always available:**
- Users can force embedded mode via config
- Fallback is automatic (daemon startup fails → embedded)

**No breaking changes:**
- MCP interface unchanged (stdio to Claude)
- Existing workflows continue to work

### Transition Plan

**Phase 1 release:** Embedded mode with auto-watch
- Users get auto-indexing (no manual `cortex index`)
- No daemon yet (low risk)

**Phase 2 release:** Daemon mode (opt-in via config)
- Early adopters test daemon
- Default is still embedded

**Phase 3 release:** Daemon mode default
- Auto-detection enabled by default
- Embedded mode is fallback

**Gradual rollout** minimizes disruption.

## Conclusion

The auto-daemon architecture provides:

✅ **Zero configuration**: Daemons start/stop automatically
✅ **Resource efficiency**: Shared state across sessions (65% memory savings)
✅ **Crash resilience**: Automatic recovery from session/daemon crashes
✅ **Transparent operation**: User sees no difference from stdio mode
✅ **Graceful degradation**: Falls back to embedded mode on failure
✅ **Multi-project isolation**: One daemon per project (no cross-contamination)
✅ **Observability**: Logs, status commands, health checks

**Trade-offs:**
- Increased complexity (process coordination, HTTP/SSE)
- Slightly slower first-session startup (~200ms vs instant)
- Additional code surface (~2000 LOC)

**Verdict:** Benefits outweigh costs for typical multi-session usage. Worth implementing.
