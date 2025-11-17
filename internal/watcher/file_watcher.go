package watcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// fileWatcher implements FileWatcher interface.
type fileWatcher struct {
	watcher         *fsnotify.Watcher
	dirs            []string                // Directories to watch
	extensions      map[string]bool         // Extensions to monitor (.go, .ts, etc.)
	debounceTime    time.Duration           // Quiet period before firing callback
	callback        func(files []string)    // Callback to invoke with changed files
	ctx             context.Context         // Context for lifecycle management
	cancel          context.CancelFunc      // Cancel function for internal context
	paused          bool                    // Whether watching is paused
	pausedMu        sync.RWMutex            // Protects paused flag
	accumulated     map[string]bool         // Accumulated file changes
	accumulatedMu   sync.Mutex              // Protects accumulated map
	debounceTimer   *time.Timer             // Current debounce timer
	timerMu         sync.Mutex              // Protects debounce timer
	stopOnce        sync.Once               // Ensures Stop() is idempotent
	doneCh          chan struct{}           // Signals watch goroutine has finished
	maxDirectories  int                     // Maximum total directories to watch
	maxDepth        int                     // Maximum directory depth to recurse
	watchedDirCount int                     // Number of directories currently watched
	countMu         sync.Mutex              // Protects watchedDirCount
}

// NewFileWatcher creates a new file watcher for the given directories.
// dirs: Source directories to watch recursively
// extensions: File extensions to monitor (e.g., []string{".go", ".ts", ".tsx"})
func NewFileWatcher(dirs []string, extensions []string) (FileWatcher, error) {
	// Detect if running in test mode
	isTest := isTestMode()

	// Set limits based on environment
	maxDirectories := 1000 // Production limit
	maxDepth := 10         // Production depth limit

	if isTest {
		maxDirectories = 50 // Test limit (much lower to catch bugs)
		maxDepth = 5        // Test depth limit
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Convert extensions slice to map for O(1) lookup
	extMap := make(map[string]bool)
	for _, ext := range extensions {
		extMap[ext] = true
	}

	fw := &fileWatcher{
		watcher:        watcher,
		dirs:           dirs,
		extensions:     extMap,
		debounceTime:   500 * time.Millisecond,
		accumulated:    make(map[string]bool),
		doneCh:         make(chan struct{}),
		maxDirectories: maxDirectories,
		maxDepth:       maxDepth,
	}

	// Add all directories recursively
	for _, dir := range dirs {
		if err := fw.addDirectoriesRecursively(dir, 0); err != nil {
			watcher.Close()
			return nil, err
		}
	}

	return fw, nil
}

// isTestMode detects if we're running in test mode.
func isTestMode() bool {
	// Check if any argument contains ".test" or if -test flag is present
	for _, arg := range os.Args {
		if strings.Contains(arg, ".test") || strings.HasPrefix(arg, "-test.") {
			return true
		}
	}
	return false
}

// Start begins watching for file changes.
func (fw *fileWatcher) Start(ctx context.Context, callback func(files []string)) error {
	if callback == nil {
		return nil
	}

	fw.callback = callback
	fw.ctx, fw.cancel = context.WithCancel(ctx)

	go fw.watch()
	return nil
}

// Stop stops the file watcher.
func (fw *fileWatcher) Stop() error {
	var err error
	fw.stopOnce.Do(func() {
		// Cancel context to signal goroutine
		if fw.cancel != nil {
			fw.cancel()

			// Wait for goroutine to finish (only if Start() was called)
			<-fw.doneCh
		} else {
			// Never started, close doneCh manually
			close(fw.doneCh)
		}

		// Close watcher
		err = fw.watcher.Close()
	})
	return err
}

// Pause stops firing callbacks but continues accumulating events.
func (fw *fileWatcher) Pause() {
	fw.pausedMu.Lock()
	defer fw.pausedMu.Unlock()
	fw.paused = true
}

// Resume resumes firing callbacks. If events accumulated during pause, fires immediately.
func (fw *fileWatcher) Resume() {
	fw.pausedMu.Lock()
	wasPaused := fw.paused
	fw.paused = false
	fw.pausedMu.Unlock()

	// If we were paused and have accumulated events, fire callback immediately
	if wasPaused {
		fw.accumulatedMu.Lock()
		if len(fw.accumulated) > 0 {
			// Copy accumulated files
			files := make([]string, 0, len(fw.accumulated))
			for file := range fw.accumulated {
				files = append(files, file)
			}
			// Clear accumulated
			fw.accumulated = make(map[string]bool)
			fw.accumulatedMu.Unlock()

			// Fire callback
			if fw.callback != nil {
				fw.callback(files)
			}
		} else {
			fw.accumulatedMu.Unlock()
		}
	}
}

// watch is the main event loop.
func (fw *fileWatcher) watch() {
	defer close(fw.doneCh)

	reindexCh := make(chan struct{}, 1)

	for {
		select {
		case <-fw.ctx.Done():
			// Context cancelled - clean shutdown
			fw.stopDebounceTimer()
			return

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}

			// Handle new directories - add them to watcher
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					// Start at depth 0 - the function will enforce limits
					if err := fw.addDirectoriesRecursively(event.Name, 0); err != nil {
						log.Printf("Warning: failed to watch new directory %s: %v", event.Name, err)
					}
				}
			}

			// Filter events by extension
			if !fw.shouldProcessEvent(event) {
				continue
			}

			// Accumulate file change
			fw.accumulatedMu.Lock()
			fw.accumulated[event.Name] = true
			fw.accumulatedMu.Unlock()

			// Reset debounce timer
			fw.resetDebounceTimer(reindexCh)

		case <-reindexCh:
			// Debounce period expired - fire callback if not paused
			fw.handleDebounceExpired()

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// handleDebounceExpired is called when the debounce timer expires.
func (fw *fileWatcher) handleDebounceExpired() {
	fw.pausedMu.RLock()
	paused := fw.paused
	fw.pausedMu.RUnlock()

	if paused {
		// Paused - keep accumulating, don't fire callback
		return
	}

	// Not paused - fire callback with accumulated files
	fw.accumulatedMu.Lock()
	if len(fw.accumulated) == 0 {
		fw.accumulatedMu.Unlock()
		return
	}

	files := make([]string, 0, len(fw.accumulated))
	for file := range fw.accumulated {
		files = append(files, file)
	}
	// Clear accumulated
	fw.accumulated = make(map[string]bool)
	fw.accumulatedMu.Unlock()

	// Fire callback
	if fw.callback != nil {
		fw.callback(files)
	}
}

// resetDebounceTimer resets the debounce timer, properly stopping the old one.
func (fw *fileWatcher) resetDebounceTimer(reindexCh chan struct{}) {
	fw.timerMu.Lock()
	defer fw.timerMu.Unlock()

	// Stop and drain existing timer
	if fw.debounceTimer != nil {
		if !fw.debounceTimer.Stop() {
			// Timer already fired, drain the channel
			select {
			case <-fw.debounceTimer.C:
			default:
			}
		}
	}

	// Create new timer
	fw.debounceTimer = time.AfterFunc(fw.debounceTime, func() {
		// Send reindex signal (non-blocking)
		select {
		case reindexCh <- struct{}{}:
		default:
		}
	})
}

// stopDebounceTimer stops the debounce timer if it exists.
func (fw *fileWatcher) stopDebounceTimer() {
	fw.timerMu.Lock()
	defer fw.timerMu.Unlock()

	if fw.debounceTimer != nil {
		fw.debounceTimer.Stop()
		fw.debounceTimer = nil
	}
}

// shouldProcessEvent checks if an event should be processed based on extension.
func (fw *fileWatcher) shouldProcessEvent(event fsnotify.Event) bool {
	// Only care about WRITE, CREATE, and REMOVE events
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
		return false
	}

	// Check if extension matches
	ext := filepath.Ext(event.Name)
	return fw.extensions[ext]
}

// addDirectoriesRecursively adds all directories in the tree to the watcher.
// Skips common directories that should not be watched (.git, node_modules, .cortex).
// depth: current depth level (0 for root directories)
func (fw *fileWatcher) addDirectoriesRecursively(rootPath string, depth int) error {
	// Check depth limit
	if depth > fw.maxDepth {
		return fmt.Errorf("max depth %d exceeded at path %s", fw.maxDepth, rootPath)
	}

	// Skip common directories that should not be watched
	dirName := filepath.Base(rootPath)
	if dirName == ".git" || dirName == "node_modules" || dirName == ".cortex" {
		return nil
	}

	// Check directory count limit
	fw.countMu.Lock()
	if fw.watchedDirCount >= fw.maxDirectories {
		count := fw.watchedDirCount
		fw.countMu.Unlock()
		return fmt.Errorf("directory limit reached: %d directories already watched (max: %d)", count, fw.maxDirectories)
	}
	fw.countMu.Unlock()

	// Read directory entries
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return err
	}

	// Add this directory to watcher
	fw.countMu.Lock()
	fw.watchedDirCount++
	currentCount := fw.watchedDirCount
	fw.countMu.Unlock()

	if err := fw.watcher.Add(rootPath); err != nil {
		fw.countMu.Lock()
		fw.watchedDirCount--
		fw.countMu.Unlock()
		return fmt.Errorf("failed to watch directory %s: %w", rootPath, err)
	}

	// Log warning if approaching limit
	if currentCount >= fw.maxDirectories*9/10 {
		log.Printf("Warning: watching %d directories (approaching limit of %d)", currentCount, fw.maxDirectories)
	}

	// Recursively add subdirectories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip common directories that should not be watched
		if entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == ".cortex" {
			continue
		}

		subPath := filepath.Join(rootPath, entry.Name())
		if err := fw.addDirectoriesRecursively(subPath, depth+1); err != nil {
			// Log but continue with other directories
			log.Printf("Warning: %v", err)
		}
	}

	return nil
}
