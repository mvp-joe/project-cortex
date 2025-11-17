package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/config"
	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/git"
	"github.com/mvp-joe/project-cortex/internal/indexer"
	"github.com/mvp-joe/project-cortex/internal/watcher"
)

// Actor manages per-project indexing lifecycle, watches git/file changes, and orchestrates incremental indexing.
// Each registered project gets one Actor goroutine that coordinates watching and indexing.
type Actor struct {
	projectPath   string
	currentBranch string
	cacheKey      string

	// Existing components (dependency injection)
	cache          *cache.Cache
	indexer        *indexer.IndexerV2 // Use IndexerV2 (current implementation)
	branchWatcher  *watcher.BranchWatcher
	fileWatcher    watcher.FileWatcher

	// Progress tracking for RPC streaming
	isIndexing   atomic.Bool
	currentPhase atomic.Value // IndexProgress_Phase (from gen/indexer/v1)
	progressMu   sync.RWMutex
	progressSubs map[string]chan *indexerv1.IndexProgress

	// Status tracking
	registeredAt   time.Time
	lastIndexedAt  atomic.Value // time.Time
	filesIndexed   atomic.Int32
	chunksCount    atomic.Int32

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	stopCh chan struct{}
	doneCh chan struct{}

	// Cleanup resources
	embedProvider embed.Provider
	db            *sql.DB
}

// NewActor creates a new Actor for the given project path.
// The projectPath must be absolute and point to a valid git repository with a .cortex directory.
// The embedProvider must be a valid, initialized embedding provider.
// The cache must be a valid, initialized Cache instance.
//
// The Actor will:
// 1. Load project configuration from .cortex/config.yml
// 2. Create an IndexerV2 instance with all required components
// 3. Create git and file watchers
// 4. Detect the current branch
//
// The Actor is created in a stopped state. Call Start() to begin watching.
func NewActor(ctx context.Context, projectPath string, embedProvider embed.Provider, c *cache.Cache) (*Actor, error) {
	// Validate project path
	if !filepath.IsAbs(projectPath) {
		return nil, fmt.Errorf("project path must be absolute: %s", projectPath)
	}

	if embedProvider == nil {
		return nil, fmt.Errorf("embedProvider cannot be nil")
	}

	if c == nil {
		return nil, fmt.Errorf("cache cannot be nil")
	}

	actorCtx, cancel := context.WithCancel(ctx)

	// Load project configuration
	cfg, err := config.LoadConfigFromDir(projectPath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Convert to indexer configuration
	indexerCfg := cfg.ToIndexerConfig(projectPath)

	// Get cache settings
	cacheSettings, err := c.LoadOrCreateSettings(projectPath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load cache settings: %w", err)
	}

	// Get current branch
	gitOps := git.NewOperations()
	currentBranch := gitOps.GetCurrentBranch(projectPath)

	// Open database connection
	db, err := c.OpenDatabase(projectPath, currentBranch, false) // false = write mode
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Note: embedProvider is passed in as a parameter (dependency injection)

	// Create indexer components (v2 architecture)
	storage, err := indexer.NewSQLiteStorage(db, cacheSettings.CacheLocation, projectPath)
	if err != nil {
		cancel()
		embedProvider.Close()
		db.Close()
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	// Create file discovery
	discovery, err := indexer.NewFileDiscovery(projectPath, indexerCfg.CodePatterns, indexerCfg.DocsPatterns, indexerCfg.IgnorePatterns)
	if err != nil {
		cancel()
		embedProvider.Close()
		db.Close()
		return nil, fmt.Errorf("failed to create file discovery: %w", err)
	}

	// Create change detector
	changeDetector := indexer.NewChangeDetector(projectPath, storage, discovery)

	// Create parser, chunker, formatter
	parser := indexer.NewParser()
	chunker := indexer.NewChunker(indexerCfg.DocChunkSize, indexerCfg.Overlap)
	formatter := indexer.NewFormatter()

	// Create processor (no progress reporter - we'll handle progress internally)
	processor := indexer.NewProcessor(projectPath, parser, chunker, formatter, embedProvider, storage, nil)

	// Create v2 indexer
	idx := indexer.NewIndexerV2(projectPath, changeDetector, processor, storage, db)

	// Create Actor struct first (BranchWatcher needs a.handleBranchSwitch callback)
	a := &Actor{
		projectPath:    projectPath,
		cache:          c,
		indexer:        idx,
		fileWatcher:    nil, // Will be set below
		branchWatcher:  nil, // Will be set below
		progressSubs:  make(map[string]chan *indexerv1.IndexProgress),
		ctx:           actorCtx,
		cancel:        cancel,
		stopCh:        make(chan struct{}),
		db:            db,
		embedProvider: embedProvider,
	}

	// Create branch watcher with Actor's callback method
	branchWatcher, err := watcher.NewBranchWatcher(projectPath, a.handleBranchSwitch)
	if err != nil {
		cancel()
		embedProvider.Close()
		db.Close()
		return nil, fmt.Errorf("failed to create branch watcher: %w", err)
	}
	a.branchWatcher = branchWatcher

	// Create file watcher
	extensions := cfg.GetSourceExtensions()
	fileWatcher, err := watcher.NewFileWatcher([]string{projectPath}, extensions)
	if err != nil {
		cancel()
		branchWatcher.Close()
		embedProvider.Close()
		db.Close()
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}
	a.fileWatcher = fileWatcher
	// Create doneCh as already closed. It will be recreated in Start() when eventLoop starts.
	// This makes Stop() safe to call even if Start() was never called.
	a.doneCh = make(chan struct{})
	close(a.doneCh)
	a.registeredAt = time.Now()

	// Set current branch (already detected earlier)
	a.currentBranch = currentBranch

	// Get cache key from settings
	a.cacheKey = cacheSettings.CacheKey

	// Initialize phase to unspecified
	a.currentPhase.Store(indexerv1.IndexProgress_PHASE_UNSPECIFIED)

	// Initialize lastIndexedAt to zero time
	a.lastIndexedAt.Store(time.Time{})

	return a, nil
}

// Start begins watching for file modifications.
// Branch watcher is already started in constructor.
// This method starts file watcher and returns immediately (non-blocking).
// The Actor will process events in background goroutines until Stop() is called.
func (a *Actor) Start() error {
	// BranchWatcher already started in constructor (auto-starts)

	// Start file watcher with file change callback
	if err := a.fileWatcher.Start(a.ctx, a.handleFileChanges); err != nil {
		return fmt.Errorf("failed to start file watcher: %w", err)
	}

	log.Printf("[%s] Actor started (branch: %s)", filepath.Base(a.projectPath), a.currentBranch)

	// Create new doneCh for the event loop
	a.doneCh = make(chan struct{})

	// Launch event loop goroutine
	go a.eventLoop()

	return nil
}

// Stop gracefully shuts down the Actor.
// This method:
// 1. Signals stop via stopCh
// 2. Stops git and file watchers
// 3. Cancels context
// 4. Waits for event loop to finish
// 5. Cleans up all resources (DB, embedding provider)
//
// Stop is safe to call multiple times.
func (a *Actor) Stop() {
	select {
	case <-a.stopCh:
		// Already stopped
		return
	default:
		close(a.stopCh)
	}

	// Stop watchers
	if a.branchWatcher != nil {
		a.branchWatcher.Close()
	}
	if a.fileWatcher != nil {
		a.fileWatcher.Stop()
	}

	// Cancel context
	if a.cancel != nil {
		a.cancel()
	}

	// Wait for event loop to finish
	<-a.doneCh

	// Clean up resources
	if a.embedProvider != nil {
		a.embedProvider.Close()
	}
	if a.db != nil {
		a.db.Close()
	}

	log.Printf("[%s] Actor stopped", filepath.Base(a.projectPath))
}

// Index triggers a full index of the project and returns progress information.
// This method:
// 1. Checks if already indexing (returns error if so)
// 2. Calls indexer.Index() to process all changes
// 3. Converts IndexerV2Stats to IndexProgress
// 4. Broadcasts progress to all subscribers
// 5. Updates status tracking fields
// 6. Returns final progress
//
// The method is synchronous and blocks until indexing completes.
func (a *Actor) Index(ctx context.Context) (*indexerv1.IndexProgress, error) {
	// Check if already indexing
	if !a.isIndexing.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("already indexing")
	}
	defer a.isIndexing.Store(false)

	// Update phase to indexing
	a.currentPhase.Store(indexerv1.IndexProgress_PHASE_INDEXING)
	defer a.currentPhase.Store(indexerv1.IndexProgress_PHASE_UNSPECIFIED)

	// Call indexer
	stats, err := a.indexer.Index(ctx, nil) // nil = full discovery
	if err != nil {
		return nil, fmt.Errorf("indexing failed: %w", err)
	}

	// Update status tracking
	totalFiles := stats.CodeFilesProcessed + stats.DocsProcessed
	totalChunks := stats.TotalCodeChunks + stats.TotalDocChunks
	a.filesIndexed.Store(int32(totalFiles))
	a.chunksCount.Store(int32(totalChunks))
	a.lastIndexedAt.Store(time.Now())

	// Convert stats to progress
	progress := statsToProgress(stats)
	progress.Phase = indexerv1.IndexProgress_PHASE_COMPLETE
	progress.Message = fmt.Sprintf("Indexing complete. Processed %d files, generated %d chunks.",
		totalFiles, totalChunks)

	// Broadcast to subscribers
	a.publishProgress(progress)

	return progress, nil
}

// handleBranchSwitch is called when the git branch changes.
// It pauses file watching, updates the current branch, triggers a full index,
// and resumes file watching.
func (a *Actor) handleBranchSwitch(oldBranch, newBranch string) {
	log.Printf("[%s] Branch switch: %s → %s", filepath.Base(a.projectPath), oldBranch, newBranch)

	a.currentBranch = newBranch

	// Pause file watching (accumulate events during sync)
	a.fileWatcher.Pause()
	defer a.fileWatcher.Resume()

	// Trigger full index on branch switch
	// (indexer's storage layer handles branch DB preparation automatically)
	stats, err := a.indexer.Index(a.ctx, nil)
	if err != nil {
		log.Printf("[%s] Failed to index after branch switch: %v", filepath.Base(a.projectPath), err)
		return
	}

	log.Printf("[%s] Indexed %d files on new branch",
		filepath.Base(a.projectPath),
		stats.CodeFilesProcessed+stats.DocsProcessed)

	// Resume will trigger file watcher callback if events accumulated
}

// handleFileChanges is called when source files change.
// It skips if already indexing, then triggers incremental indexing.
func (a *Actor) handleFileChanges(files []string) {
	if a.isIndexing.Load() {
		// Already indexing, skip (debouncer will trigger again)
		return
	}

	a.isIndexing.Store(true)
	defer a.isIndexing.Store(false)

	log.Printf("[%s] Processing %d changed files", filepath.Base(a.projectPath), len(files))

	// Update phase
	a.currentPhase.Store(indexerv1.IndexProgress_PHASE_INDEXING)
	defer a.currentPhase.Store(indexerv1.IndexProgress_PHASE_UNSPECIFIED)

	// Trigger incremental indexing with hint
	// (indexer's ChangeDetector will determine what actually changed)
	stats, err := a.indexer.Index(a.ctx, files)
	if err != nil {
		log.Printf("[%s] Indexing failed: %v", filepath.Base(a.projectPath), err)
		return
	}

	log.Printf("[%s] Indexed %d files",
		filepath.Base(a.projectPath),
		stats.CodeFilesProcessed+stats.DocsProcessed)

	// Broadcast progress
	progress := statsToProgress(stats)
	progress.Phase = indexerv1.IndexProgress_PHASE_COMPLETE
	progress.Message = fmt.Sprintf("Incremental indexing complete. Processed %d files.",
		stats.CodeFilesProcessed+stats.DocsProcessed)
	a.publishProgress(progress)
}

// eventLoop is the main goroutine that coordinates lifecycle.
// Currently it just waits for stop signal, but could be extended
// for more complex coordination in the future.
func (a *Actor) eventLoop() {
	defer close(a.doneCh)

	<-a.stopCh
	// Event loop finished
}

// SubscribeProgress registers a channel for progress updates.
// The returned channel will receive IndexProgress updates during indexing operations.
// The channel is buffered (capacity 10) to avoid blocking the Actor.
//
// Callers must call UnsubscribeProgress() when done to avoid leaking channels.
func (a *Actor) SubscribeProgress(id string) chan *indexerv1.IndexProgress {
	a.progressMu.Lock()
	defer a.progressMu.Unlock()

	ch := make(chan *indexerv1.IndexProgress, 10)
	a.progressSubs[id] = ch
	return ch
}

// UnsubscribeProgress removes a progress subscriber and closes its channel.
// Safe to call multiple times with the same ID.
func (a *Actor) UnsubscribeProgress(id string) {
	a.progressMu.Lock()
	defer a.progressMu.Unlock()

	if ch, ok := a.progressSubs[id]; ok {
		close(ch)
		delete(a.progressSubs, id)
	}
}

// publishProgress sends progress to all subscribers.
// Sending is non-blocking - slow subscribers will miss updates.
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

// GetStatus returns the current status of the project.
// This method is thread-safe and can be called concurrently with indexing operations.
func (a *Actor) GetStatus() *indexerv1.ProjectStatus {
	// Get current phase (atomic load)
	phase := a.currentPhase.Load().(indexerv1.IndexProgress_Phase)

	// Get last indexed time (atomic load)
	lastIndexed := a.lastIndexedAt.Load().(time.Time)
	var lastIndexedUnix int64
	if !lastIndexed.IsZero() {
		lastIndexedUnix = lastIndexed.Unix()
	}

	return &indexerv1.ProjectStatus{
		Path:           a.projectPath,
		CacheKey:       a.cacheKey,
		CurrentBranch:  a.currentBranch,
		FilesIndexed:   a.filesIndexed.Load(),
		ChunksCount:    a.chunksCount.Load(),
		RegisteredAt:   a.registeredAt.Unix(),
		LastIndexedAt:  lastIndexedUnix,
		IsIndexing:     a.isIndexing.Load(),
		CurrentPhase:   phase,
	}
}

// statsToProgress converts IndexerV2Stats to IndexProgress protobuf message.
func statsToProgress(stats *indexer.IndexerV2Stats) *indexerv1.IndexProgress {
	totalFiles := stats.FilesAdded + stats.FilesModified
	totalChunks := stats.TotalCodeChunks + stats.TotalDocChunks

	return &indexerv1.IndexProgress{
		Phase:           indexerv1.IndexProgress_PHASE_INDEXING,
		FilesTotal:      int32(totalFiles),
		FilesProcessed:  int32(stats.CodeFilesProcessed + stats.DocsProcessed),
		ChunksGenerated: int32(totalChunks),
		CurrentFile:     "", // Not tracked at this level
		Message:         fmt.Sprintf("Processing files..."),
	}
}
