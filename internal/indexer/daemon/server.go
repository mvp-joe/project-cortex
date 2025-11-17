package daemon

import (
	"container/ring"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect"
	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/embed"
)

// Server implements IndexerServiceHandler for the indexer daemon.
// Manages actor lifecycle, handles RPC requests, and streams progress updates.
type Server struct {
	registry      ProjectsRegistry  // Projects registry
	cache         *cache.Cache      // Cache instance (shared across all actors)
	embedProvider embed.Provider    // Shared embedding provider for all actors
	actors        map[string]*Actor // path -> Actor
	actorsMu      sync.RWMutex      // Protects actors map
	startedAt     time.Time         // Daemon start time
	socketPath    string            // Unix socket path

	// Logging
	logsMu    sync.RWMutex                        // Protects log buffer and subscriptions
	logBuffer *ring.Ring                          // Circular buffer for logs (capacity 1000)
	logSubs   map[string]chan *indexerv1.LogEntry // Subscription ID -> channel

	ctx    context.Context    // Server context
	cancel context.CancelFunc // Cancel function
}

// NewServer creates a new indexer daemon RPC server.
// The server manages per-project actors and coordinates indexing operations.
// The embedProvider must be initialized before passing to NewServer and is shared across all actors.
// The cache must be a valid, initialized Cache instance (shared across all actors).
func NewServer(ctx context.Context, socketPath string, embedProvider embed.Provider, c *cache.Cache) (*Server, error) {
	if embedProvider == nil {
		return nil, fmt.Errorf("embedProvider cannot be nil")
	}
	if c == nil {
		return nil, fmt.Errorf("cache cannot be nil")
	}
	// Create projects registry
	registry, err := NewProjectsRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to create registry: %w", err)
	}

	// Create server context
	serverCtx, cancel := context.WithCancel(ctx)

	s := &Server{
		registry:      registry,
		cache:         c,
		embedProvider: embedProvider,
		actors:        make(map[string]*Actor),
		logBuffer:     ring.New(1000), // Circular buffer with capacity 1000
		logSubs:       make(map[string]chan *indexerv1.LogEntry),
		startedAt:     time.Now(),
		socketPath:    socketPath,
		ctx:           serverCtx,
		cancel:        cancel,
	}

	return s, nil
}

// Index triggers indexing for a project and streams progress updates.
//
// Behavior:
// - If project not registered: Registers project, creates actor, starts initial indexing
// - If project already registered: Reuses existing actor
// - Stream completes when initial indexing finishes
// - After initial index, actor transitions to watching mode (git + file watchers)
func (s *Server) Index(
	ctx context.Context,
	req *connect.Request[indexerv1.IndexRequest],
	stream *connect.ServerStream[indexerv1.IndexProgress],
) error {
	projectPath := req.Msg.ProjectPath

	// Validate project path is absolute
	if !filepath.IsAbs(projectPath) {
		return fmt.Errorf("project path must be absolute: %s", projectPath)
	}

	// Validate path exists
	if _, err := os.Stat(projectPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("project path does not exist: %s", projectPath)
		}
		return fmt.Errorf("failed to stat project path: %w", err)
	}

	// Get or create actor
	actor, isNew, err := s.getOrCreateActor(ctx, projectPath)
	if err != nil {
		return fmt.Errorf("failed to get or create actor: %w", err)
	}

	// If new actor, register project and start watching
	if isNew {
		if _, err := s.registry.Register(projectPath); err != nil {
			return fmt.Errorf("failed to register project: %w", err)
		}

		if err := actor.Start(); err != nil {
			return fmt.Errorf("failed to start actor: %w", err)
		}
	}

	// Subscribe to progress updates
	subID := fmt.Sprintf("index-%d", time.Now().UnixNano())
	progressCh := actor.SubscribeProgress(subID)
	defer actor.UnsubscribeProgress(subID)

	// Start indexing in background
	indexDone := make(chan error, 1)
	go func() {
		_, err := actor.Index(ctx)
		indexDone <- err
	}()

	// Stream progress updates until indexing completes
	for {
		select {
		case progress := <-progressCh:
			if err := stream.Send(progress); err != nil {
				return fmt.Errorf("failed to send progress: %w", err)
			}

			// If complete, we can finish
			if progress.Phase == indexerv1.IndexProgress_PHASE_COMPLETE {
				// Wait for index to finish
				if err := <-indexDone; err != nil {
					return fmt.Errorf("indexing failed: %w", err)
				}
				return nil
			}

		case err := <-indexDone:
			if err != nil {
				return fmt.Errorf("indexing failed: %w", err)
			}
			return nil

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// GetStatus returns the daemon status and all watched projects.
func (s *Server) GetStatus(
	ctx context.Context,
	req *connect.Request[indexerv1.StatusRequest],
) (*connect.Response[indexerv1.StatusResponse], error) {
	// Get daemon info
	daemon := &indexerv1.DaemonStatus{
		Pid:           int32(os.Getpid()),
		StartedAt:     s.startedAt.Unix(),
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		SocketPath:    s.socketPath,
	}

	// Get all project statuses
	s.actorsMu.RLock()
	projects := make([]*indexerv1.ProjectStatus, 0, len(s.actors))
	for _, actor := range s.actors {
		projects = append(projects, actor.GetStatus())
	}
	s.actorsMu.RUnlock()

	resp := &indexerv1.StatusResponse{
		Daemon:   daemon,
		Projects: projects,
	}

	return connect.NewResponse(resp), nil
}

// StreamLogs streams log entries for one or all projects.
//
// Filtering:
// - If project_path empty: Stream logs from all projects
// - If project_path set: Only logs for that project
//
// Follow mode:
// - If follow=false: Stream existing logs, then close
// - If follow=true: Stream existing logs, keep connection open for new logs
func (s *Server) StreamLogs(
	ctx context.Context,
	req *connect.Request[indexerv1.LogsRequest],
	stream *connect.ServerStream[indexerv1.LogEntry],
) error {
	projectPath := req.Msg.ProjectPath
	follow := req.Msg.Follow

	// Validate project path if specified
	if projectPath != "" && !filepath.IsAbs(projectPath) {
		return fmt.Errorf("project path must be absolute: %s", projectPath)
	}

	// Create subscription channel if following
	var subID string
	var subCh chan *indexerv1.LogEntry
	if follow {
		subID = fmt.Sprintf("logs-%d", time.Now().UnixNano())
		subCh = make(chan *indexerv1.LogEntry, 10)

		s.logsMu.Lock()
		s.logSubs[subID] = subCh
		s.logsMu.Unlock()

		defer func() {
			s.logsMu.Lock()
			delete(s.logSubs, subID)
			close(subCh)
			s.logsMu.Unlock()
		}()
	}

	// Send buffered logs
	s.logsMu.RLock()
	bufferedLogs := s.collectBufferedLogs(projectPath)
	s.logsMu.RUnlock()

	for _, entry := range bufferedLogs {
		if err := stream.Send(entry); err != nil {
			return fmt.Errorf("failed to send log entry: %w", err)
		}
	}

	// If not following, we're done
	if !follow {
		return nil
	}

	// Follow mode: keep streaming new logs
	for {
		select {
		case entry := <-subCh:
			// Filter by project if specified
			if projectPath != "" && entry.Project != projectPath {
				continue
			}

			if err := stream.Send(entry); err != nil {
				return fmt.Errorf("failed to send log entry: %w", err)
			}

		case <-ctx.Done():
			return ctx.Err()

		case <-s.ctx.Done():
			// Server shutting down
			return nil
		}
	}
}

// UnregisterProject stops watching a project and optionally removes its cache.
func (s *Server) UnregisterProject(
	ctx context.Context,
	req *connect.Request[indexerv1.UnregisterRequest],
) (*connect.Response[indexerv1.UnregisterResponse], error) {
	projectPath := req.Msg.ProjectPath
	removeCache := req.Msg.RemoveCache

	// Stop and remove actor if exists
	if err := s.stopActor(projectPath); err != nil {
		return connect.NewResponse(&indexerv1.UnregisterResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to stop actor: %v", err),
		}), nil
	}

	// Unregister from registry
	if err := s.registry.Unregister(projectPath); err != nil {
		return connect.NewResponse(&indexerv1.UnregisterResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to unregister project: %v", err),
		}), nil
	}

	// Remove cache if requested
	if removeCache {
		// Load cache settings to get cache location
		settings, err := s.cache.LoadOrCreateSettings(projectPath)
		if err == nil && settings.CacheLocation != "" {
			// Remove the entire cache directory
			if err := os.RemoveAll(settings.CacheLocation); err != nil {
				return connect.NewResponse(&indexerv1.UnregisterResponse{
					Success: false,
					Message: fmt.Sprintf("Failed to remove cache: %v", err),
				}), nil
			}
		}
	}

	message := "Project unregistered successfully"
	if s.getActor(projectPath) == nil {
		message = "Project not registered (already removed)"
	}

	return connect.NewResponse(&indexerv1.UnregisterResponse{
		Success: true,
		Message: message,
	}), nil
}

// shutdownInternal performs the actual shutdown logic.
// This is called by both the Shutdown RPC and the signal handler
// to ensure consistent cleanup behavior.
func (s *Server) shutdownInternal() {
	// Stop all actors
	s.actorsMu.Lock()
	for _, actor := range s.actors {
		actor.Stop()
	}
	// Clear actors map
	s.actors = make(map[string]*Actor)
	s.actorsMu.Unlock()

	// Close all log subscriptions
	s.logsMu.Lock()
	for id, ch := range s.logSubs {
		close(ch)
		delete(s.logSubs, id)
	}
	s.logsMu.Unlock()

	// Cancel server context (this triggers httpServer.Shutdown in the signal handler)
	s.cancel()
}

// Shutdown gracefully stops the daemon via RPC.
// This is the external API called by `cortex indexer stop`.
func (s *Server) Shutdown(
	ctx context.Context,
	req *connect.Request[indexerv1.ShutdownRequest],
) (*connect.Response[indexerv1.ShutdownResponse], error) {
	s.shutdownInternal()

	return connect.NewResponse(&indexerv1.ShutdownResponse{
		Success: true,
		Message: "Daemon stopped gracefully",
	}), nil
}

// getOrCreateActor gets an existing actor or creates a new one.
// Returns (actor, isNew, error).
func (s *Server) getOrCreateActor(ctx context.Context, projectPath string) (*Actor, bool, error) {
	// Check if actor already exists
	s.actorsMu.RLock()
	actor, exists := s.actors[projectPath]
	s.actorsMu.RUnlock()

	if exists {
		return actor, false, nil
	}

	// Create new actor (use server's shared embedding provider and cache)
	actor, err := NewActor(s.ctx, projectPath, s.embedProvider, s.cache)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create actor: %w", err)
	}

	// Store in map
	s.actorsMu.Lock()
	// Check again in case another goroutine created it
	if existing, exists := s.actors[projectPath]; exists {
		s.actorsMu.Unlock()
		return existing, false, nil
	}
	s.actors[projectPath] = actor
	s.actorsMu.Unlock()

	return actor, true, nil
}

// stopActor stops an actor and removes it from the actors map.
// Returns nil if actor doesn't exist (idempotent).
func (s *Server) stopActor(projectPath string) error {
	s.actorsMu.Lock()
	defer s.actorsMu.Unlock()

	actor, exists := s.actors[projectPath]
	if !exists {
		return nil // Idempotent - not an error
	}

	actor.Stop()
	delete(s.actors, projectPath)

	return nil
}

// getActor retrieves an actor by project path.
// Returns nil if not found.
func (s *Server) getActor(projectPath string) *Actor {
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	return s.actors[projectPath]
}

// logMessage adds a log entry to the buffer and broadcasts to subscribers.
// Thread-safe - can be called from multiple goroutines.
func (s *Server) logMessage(project, level, message string) {
	entry := &indexerv1.LogEntry{
		Timestamp: time.Now().UnixMilli(),
		Project:   project,
		Level:     level,
		Message:   message,
	}

	s.logsMu.Lock()
	defer s.logsMu.Unlock()

	// Add to ring buffer
	s.logBuffer.Value = entry
	s.logBuffer = s.logBuffer.Next()

	// Broadcast to subscribers
	s.broadcastLogLocked(entry)
}

// broadcastLogLocked sends log entry to all subscribers.
// Must be called with logsMu held.
func (s *Server) broadcastLogLocked(entry *indexerv1.LogEntry) {
	for _, ch := range s.logSubs {
		select {
		case ch <- entry:
		default:
			// Subscriber slow, skip (non-blocking)
		}
	}
}

// collectBufferedLogs returns all buffered logs, optionally filtered by project.
// Must be called with logsMu held (at least RLock).
func (s *Server) collectBufferedLogs(projectPath string) []*indexerv1.LogEntry {
	logs := make([]*indexerv1.LogEntry, 0, 1000)

	s.logBuffer.Do(func(v interface{}) {
		if v == nil {
			return
		}
		entry := v.(*indexerv1.LogEntry)

		// Filter by project if specified
		if projectPath != "" && entry.Project != projectPath {
			return
		}

		logs = append(logs, entry)
	})

	return logs
}
