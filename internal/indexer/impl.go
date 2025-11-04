package indexer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/graph"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// indexer implements the Indexer interface.
type indexer struct {
	config    *Config
	parser    Parser
	chunker   Chunker
	formatter Formatter
	discovery *FileDiscovery
	storage   Storage // Was: writer *AtomicWriter
	provider  embed.Provider
	progress  ProgressReporter
}

// Close releases all resources held by the indexer.
func (idx *indexer) Close() error {
	var firstErr error

	// Close provider
	if idx.provider != nil {
		if err := idx.provider.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Close storage
	if idx.storage != nil {
		if err := idx.storage.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// New creates a new indexer instance.
func New(config *Config) (Indexer, error) {
	return NewWithProgress(config, &NoOpProgressReporter{})
}

// NewWithProgress creates a new indexer instance with a custom progress reporter.
// The indexer will create and manage its own embedding provider.
func NewWithProgress(config *Config, progress ProgressReporter) (Indexer, error) {
	// Create file discovery
	discovery, err := NewFileDiscovery(
		config.RootDir,
		config.CodePatterns,
		config.DocsPatterns,
		config.IgnorePatterns,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create file discovery: %w", err)
	}

	// Create SQLite storage (only supported backend)
	storage, err := NewSQLiteStorage(config.RootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	// Create embedding provider
	provider, err := embed.NewProvider(embed.Config{
		Provider: config.EmbeddingProvider,
		Endpoint: config.EmbeddingEndpoint,
		Model:    config.EmbeddingModel,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding provider: %w", err)
	}

	// Initialize provider (downloads binary if needed, starts server, waits for ready)
	ctx := context.Background()
	if err := provider.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize embedding provider: %w", err)
	}

	if progress == nil {
		progress = &NoOpProgressReporter{}
	}

	return &indexer{
		config:    config,
		parser:    NewParser(),
		chunker:   NewChunker(config.DocChunkSize, config.Overlap),
		formatter: NewFormatter(),
		discovery: discovery,
		storage:   storage,
		provider:  provider,
		progress:  progress,
	}, nil
}

// NewWithProvider creates a new indexer instance with a pre-initialized embedding provider.
// The provider must already be initialized via provider.Initialize().
// The indexer will NOT close the provider - caller is responsible for cleanup.
func NewWithProvider(config *Config, provider embed.Provider, progress ProgressReporter) (Indexer, error) {
	// Create file discovery
	discovery, err := NewFileDiscovery(
		config.RootDir,
		config.CodePatterns,
		config.DocsPatterns,
		config.IgnorePatterns,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create file discovery: %w", err)
	}

	// Create SQLite storage (only supported backend)
	storage, err := NewSQLiteStorage(config.RootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	if progress == nil {
		progress = &NoOpProgressReporter{}
	}

	return &indexer{
		config:    config,
		parser:    NewParser(),
		chunker:   NewChunker(config.DocChunkSize, config.Overlap),
		formatter: NewFormatter(),
		discovery: discovery,
		storage:   storage,
		provider:  provider,
		progress:  progress,
	}, nil
}

// processFiles is the core processing pipeline used by both Index() and IndexIncremental().
// It handles: file metadata collection -> SQLite file stats write -> code/doc processing ->
// chunk embedding -> chunk writing -> graph build/update.
// CRITICAL: File metadata MUST be written before chunks due to foreign key constraints.
func (idx *indexer) processFiles(
	ctx context.Context,
	codeFiles, docFiles []string,
	allFiles []string, // All files in project (for graph)
	deletedFiles []string, // nil for full index, populated for incremental
) (*ProcessingStats, error) {
	stats := &ProcessingStats{}

	// Phase 1: Collect file metadata for ALL files (code + docs)
	phaseStart := time.Now()
	allFilesToProcess := append(codeFiles, docFiles...)
	fileStatsMap := make(map[string]*storage.FileStats) // relPath -> FileStats

	log.Printf("Collecting file metadata for %d files...\n", len(allFilesToProcess))
	for _, file := range allFilesToProcess {
		fileStats, err := collectFileMetadata(idx.config.RootDir, file)
		if err != nil {
			log.Printf("Warning: failed to collect metadata for %s: %v\n", file, err)
			continue
		}
		fileStatsMap[fileStats.FilePath] = fileStats
	}
	log.Printf("[TIMING] Collect file metadata: %v (%d files)\n", time.Since(phaseStart), len(fileStatsMap))

	// Phase 2: Write file stats to SQLite BEFORE processing chunks
	// CRITICAL: Chunks have foreign key to files table, so files MUST exist first
	phaseStart = time.Now()
	fileStatsList := make([]*storage.FileStats, 0, len(fileStatsMap))
	for _, stats := range fileStatsMap {
		fileStatsList = append(fileStatsList, stats)
	}

	writer := storage.NewFileWriter(idx.storage.GetDB())
	if err := writer.WriteFileStatsBatch(fileStatsList); err != nil {
		return nil, fmt.Errorf("failed to write file stats: %w", err)
	}
	log.Printf("✓ Wrote file stats for %d files to SQLite\n", len(fileStatsList))
	log.Printf("[TIMING] Write file stats: %v\n", time.Since(phaseStart))

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Phase 3: Process code files
	phaseStart = time.Now()
	symbolsChunks, defsChunks, dataChunks, err := idx.processCodeFiles(ctx, codeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to process code files: %w", err)
	}
	stats.CodeFilesProcessed = len(codeFiles)
	stats.TotalCodeChunks = len(symbolsChunks) + len(defsChunks) + len(dataChunks)
	log.Printf("[TIMING] Process code files: %v (%d files -> %d chunks)\n",
		time.Since(phaseStart), len(codeFiles), stats.TotalCodeChunks)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Phase 4: Process documentation files
	phaseStart = time.Now()
	docChunks, err := idx.processDocFiles(ctx, docFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to process documentation files: %w", err)
	}
	stats.DocsProcessed = len(docFiles)
	stats.TotalDocChunks = len(docChunks)
	log.Printf("[TIMING] Process doc files: %v (%d files -> %d chunks)\n",
		time.Since(phaseStart), len(docFiles), stats.TotalDocChunks)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Phase 5: Write chunk files
	phaseStart = time.Now()
	idx.progress.OnWritingChunks()
	// deletedFiles == nil means full index, otherwise incremental
	incremental := deletedFiles != nil
	if err := idx.writeChunkFiles(symbolsChunks, defsChunks, dataChunks, docChunks, incremental); err != nil {
		return nil, fmt.Errorf("failed to write chunk files: %w", err)
	}
	log.Printf("[TIMING] Write chunk files: %v\n", time.Since(phaseStart))

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Phase 6: Build/update graph
	phaseStart = time.Now()
	log.Println("Building code graph...")
	changedFiles := append(codeFiles, docFiles...)
	if err := idx.buildAndSaveGraph(ctx, changedFiles, deletedFiles, allFiles); err != nil {
		log.Printf("Warning: failed to build graph: %v\n", err)
		// Don't fail indexing if graph fails - it's supplementary
	}
	log.Printf("[TIMING] Build graph: %v\n", time.Since(phaseStart))

	return stats, nil
}

// Index processes all files in the codebase and generates chunk files.
func (idx *indexer) Index(ctx context.Context) (*ProcessingStats, error) {
	startTime := time.Now()

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Phase 1: Discovery
	phaseStart := time.Now()
	idx.progress.OnDiscoveryStart()
	codeFiles, docFiles, err := idx.discovery.DiscoverFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}
	idx.progress.OnDiscoveryComplete(len(codeFiles), len(docFiles))
	log.Printf("[TIMING] Discovery: %v\n", time.Since(phaseStart))

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Phase 1.5: Branch optimization (always enabled for SQLite)
	var filesToProcess []string
	phaseStart = time.Now()
	optimizer, err := NewBranchOptimizer(idx.config.RootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create branch optimizer: %w", err)
	}

	if optimizer != nil {
		log.Println("Branch optimization enabled: copying unchanged chunks from ancestor branch")

		// Build file info map for change detection
		fileInfoMap := make(map[string]FileInfo)
		for _, file := range append(codeFiles, docFiles...) {
			checksum, err := calculateChecksum(file)
			if err != nil {
				log.Printf("Warning: failed to calculate checksum for %s: %v\n", file, err)
				continue
			}
			relPath, _ := filepath.Rel(idx.config.RootDir, file)

			var modTime time.Time
			if info, err := os.Stat(file); err == nil {
				modTime = info.ModTime()
			}

			fileInfoMap[relPath] = FileInfo{
				Path:    relPath,
				Hash:    checksum,
				ModTime: modTime,
			}
		}

		// Copy unchanged chunks from ancestor branch
		copiedChunks, changedFiles, err := optimizer.CopyUnchangedChunks(fileInfoMap)
		if err != nil {
			log.Printf("Warning: branch optimization failed: %v (falling back to full indexing)\n", err)
			filesToProcess = append(codeFiles, docFiles...)
		} else {
			log.Printf("✓ Copied %d chunks from ancestor branch, %d files need re-indexing\n",
				copiedChunks, len(changedFiles))

			// Convert relative paths back to absolute paths
			filesToProcess = make([]string, 0, len(changedFiles))
			for _, relPath := range changedFiles {
				absPath := filepath.Join(idx.config.RootDir, relPath)
				filesToProcess = append(filesToProcess, absPath)
			}
		}
		log.Printf("[TIMING] Branch optimization: %v\n", time.Since(phaseStart))
	} else {
		// No optimization available (on main branch)
		filesToProcess = append(codeFiles, docFiles...)
	}

	// Separate files to process into code and docs
	filesToProcessSet := make(map[string]bool)
	for _, f := range filesToProcess {
		filesToProcessSet[f] = true
	}

	filteredCodeFiles := make([]string, 0, len(codeFiles))
	for _, f := range codeFiles {
		if filesToProcessSet[f] {
			filteredCodeFiles = append(filteredCodeFiles, f)
		}
	}

	filteredDocFiles := make([]string, 0, len(docFiles))
	for _, f := range docFiles {
		if filesToProcessSet[f] {
			filteredDocFiles = append(filteredDocFiles, f)
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Phase 2-6: Process files (metadata -> code/docs -> chunks -> graph)
	// Use helper method that both Index and IndexIncremental share
	allFiles := append(codeFiles, docFiles...)
	stats, err := idx.processFiles(ctx, filteredCodeFiles, filteredDocFiles, allFiles, nil)
	if err != nil {
		return nil, err
	}

	// Complete timing
	stats.ProcessingTimeSeconds = time.Since(startTime).Seconds()

	totalTime := time.Since(startTime)
	log.Printf("[TIMING] ===== TOTAL INDEX TIME: %v =====\n", totalTime)

	idx.progress.OnComplete(stats)

	// Phase 9: Post-index cache maintenance (metadata update + eviction)
	phaseStart = time.Now()
	evictionConfig := DefaultEvictionConfig()
	if err := PostIndexEviction(idx.storage, idx.config.RootDir, stats, evictionConfig); err != nil {
		log.Printf("Warning: post-index cache maintenance failed: %v\n", err)
		// Don't fail indexing if cache maintenance fails
	}
	log.Printf("[TIMING] Cache maintenance: %v\n", time.Since(phaseStart))

	return stats, nil
}

// shouldReprocessFile determines if a file needs reprocessing using two-stage filtering:
// Stage 1: Fast mtime check (stat only, no file read)
// Stage 2: Checksum verification for files that passed Stage 1 (reads file content)
func shouldReprocessFile(
	filePath string,
	relPath string,
	metadata *GeneratorMetadata,
) (shouldProcess bool, currentMtime time.Time, currentChecksum string, err error) {
	// Stage 1: Fast mtime check (no file content read)
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return false, time.Time{}, "", fmt.Errorf("failed to stat file: %w", err)
	}

	currentMtime = fileInfo.ModTime()

	// Check if we have previous mtime
	if lastMtime, exists := metadata.FileMtimes[relPath]; exists {
		// If mtime hasn't changed, file is definitely unchanged
		if !currentMtime.After(lastMtime) {
			// FAST PATH: File definitely unchanged, reuse existing checksum
			existingChecksum := metadata.FileChecksums[relPath]
			return false, currentMtime, existingChecksum, nil
		}
	} else {
		// No previous mtime - either new file or old metadata format
		// Fall through to Stage 2
	}

	// Stage 2: mtime changed or missing - verify with checksum
	currentChecksum, err = calculateChecksum(filePath)
	if err != nil {
		return false, currentMtime, "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	lastChecksum, exists := metadata.FileChecksums[relPath]
	if !exists {
		// New file
		return true, currentMtime, currentChecksum, nil
	}

	// Compare checksums - detects content changes even with backdated mtimes
	return currentChecksum != lastChecksum, currentMtime, currentChecksum, nil
}

// IndexIncremental processes only changed files based on checksums.
func (idx *indexer) IndexIncremental(ctx context.Context) (*ProcessingStats, error) {
	startTime := time.Now()

	// Read previous metadata
	metadata, err := idx.storage.ReadMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	// Discover all files
	codeFiles, docFiles, err := idx.discovery.DiscoverFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}

	// Calculate checksums and detect changes using two-stage filtering
	currentFiles := make(map[string]string) // relPath -> absolute path
	changedFiles := make(map[string]bool)   // relPath -> true if changed
	newChecksums := make(map[string]string) // relPath -> checksum
	newMtimes := make(map[string]time.Time) // relPath -> mtime

	// Process all current files using two-stage filtering
	for _, file := range append(codeFiles, docFiles...) {
		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		currentFiles[relPath] = file

		// Two-stage filtering: mtime first, then checksum if needed
		changed, mtime, checksum, err := shouldReprocessFile(file, relPath, metadata)
		if err != nil {
			log.Printf("Warning: error checking %s: %v\n", file, err)
			continue
		}

		// Track current state
		newMtimes[relPath] = mtime
		newChecksums[relPath] = checksum

		if changed {
			changedFiles[relPath] = true
		}
	}

	// Detect deleted files (existed in metadata but not in current files)
	deletedFiles := make(map[string]bool)
	for relPath := range metadata.FileChecksums {
		if _, exists := currentFiles[relPath]; !exists {
			deletedFiles[relPath] = true
		}
	}

	// Check if there are any changes
	if len(changedFiles) == 0 && len(deletedFiles) == 0 {
		log.Println("No changes detected")
		// Return stats showing no processing happened, but preserve chunk counts
		stats := &ProcessingStats{
			CodeFilesProcessed:    0,
			DocsProcessed:         0,
			TotalCodeChunks:       metadata.Stats.TotalCodeChunks,
			TotalDocChunks:        metadata.Stats.TotalDocChunks,
			ProcessingTimeSeconds: 0,
		}
		return stats, nil
	}

	log.Printf("Detected %d changed files and %d deleted files\n", len(changedFiles), len(deletedFiles))

	// Handle deletions (delete file records which cascade to chunks via foreign key)
	if len(deletedFiles) > 0 {
		writer := storage.NewFileWriter(idx.storage.GetDB())
		for relPath := range deletedFiles {
			if err := writer.DeleteFile(relPath); err != nil {
				log.Printf("Warning: failed to delete file %s: %v\n", relPath, err)
			}
		}
		log.Printf("✓ Deleted %d files from SQLite (cascaded to chunks)\n", len(deletedFiles))
	}

	// Separate changed files into code and docs
	changedCode := []string{}
	changedDocs := []string{}
	for relPath := range changedFiles {
		absPath := currentFiles[relPath]
		isCode := false
		for _, codeFile := range codeFiles {
			if codeFile == absPath {
				isCode = true
				break
			}
		}
		if isCode {
			changedCode = append(changedCode, absPath)
		} else {
			changedDocs = append(changedDocs, absPath)
		}
	}

	log.Printf("Processing %d changed code files and %d changed documentation files\n", len(changedCode), len(changedDocs))

	// Process changed files using shared helper (handles metadata -> code/docs -> chunks -> graph)
	deletedPaths := make([]string, 0, len(deletedFiles))
	for relPath := range deletedFiles {
		deletedPaths = append(deletedPaths, relPath)
	}
	allFiles := append(codeFiles, docFiles...)

	stats, err := idx.processFiles(ctx, changedCode, changedDocs, allFiles, deletedPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to process changed files: %w", err)
	}

	// SQLite handles incremental updates natively via WriteChunksIncremental
	stats.ProcessingTimeSeconds = time.Since(startTime).Seconds()

	log.Printf("✓ Incremental indexing complete: %d code chunks, %d doc chunks in %.2fs\n",
		stats.TotalCodeChunks, stats.TotalDocChunks, stats.ProcessingTimeSeconds)

	// Post-index cache maintenance (metadata update + eviction)
	evictionConfig := DefaultEvictionConfig()
	if err := PostIndexEviction(idx.storage, idx.config.RootDir, stats, evictionConfig); err != nil {
		log.Printf("Warning: post-index cache maintenance failed: %v\n", err)
		// Don't fail indexing if cache maintenance fails
	}

	return stats, nil
}

// Watch starts watching for file changes and reindexes incrementally.
func (idx *indexer) Watch(ctx context.Context) error {
	// Create the file watcher
	watcher, err := NewIndexerWatcher(idx, idx.config.RootDir)
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer watcher.Stop()

	// Start watching in a goroutine
	watcher.Start(ctx)

	// Block until context is cancelled
	<-ctx.Done()

	return nil
}

// processCodeFiles processes code files and returns chunks.
func (idx *indexer) processCodeFiles(ctx context.Context, files []string) (symbols, definitions, data []Chunk, err error) {
	symbols = []Chunk{}
	definitions = []Chunk{}
	data = []Chunk{}

	idx.progress.OnFileProcessingStart(len(files))

	var parsingTime, chunkingTime time.Duration

	for _, file := range files {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, nil, nil, ctx.Err()
		default:
		}

		// Parse file
		parseStart := time.Now()
		extraction, err := idx.parser.ParseFile(ctx, file)
		parsingTime += time.Since(parseStart)
		if err != nil {
			log.Printf("Warning: failed to parse %s: %v\n", file, err)
			idx.progress.OnFileProcessed(file)
			continue
		}

		if extraction == nil {
			// Unsupported language
			idx.progress.OnFileProcessed(file)
			continue
		}

		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		now := time.Now()
		chunkStart := time.Now()

		// Create symbols chunk
		if extraction.Symbols != nil {
			text := idx.formatter.FormatSymbols(extraction.Symbols, extraction.Language)
			if text != "" {
				tags := []string{"code", extraction.Language, "symbols"}
				metadata := map[string]interface{}{
					"source":    "code",
					"file_path": relPath,
					"language":  extraction.Language,
					"package":   extraction.Symbols.PackageName,
				}
				// Store tags as indexed metadata keys for chromem-go WHERE filtering
				for i, tag := range tags {
					metadata[fmt.Sprintf("tag_%d", i)] = tag
				}
				chunk := Chunk{
					ID:        fmt.Sprintf("code-symbols-%s", relPath),
					ChunkType: ChunkTypeSymbols,
					Title:     fmt.Sprintf("Symbols: %s", relPath),
					Text:      text,
					Tags:      tags,
					Metadata:  metadata,
					CreatedAt: now,
					UpdatedAt: now,
				}
				symbols = append(symbols, chunk)
			}
		}

		// Create definitions chunk
		if extraction.Definitions != nil && len(extraction.Definitions.Definitions) > 0 {
			text := idx.formatter.FormatDefinitions(extraction.Definitions, extraction.Language)
			if text != "" {
				tags := []string{"code", extraction.Language, "definitions"}
				metadata := map[string]interface{}{
					"source":    "code",
					"file_path": relPath,
					"language":  extraction.Language,
				}
				// Store tags as indexed metadata keys for chromem-go WHERE filtering
				for i, tag := range tags {
					metadata[fmt.Sprintf("tag_%d", i)] = tag
				}
				chunk := Chunk{
					ID:        fmt.Sprintf("code-definitions-%s", relPath),
					ChunkType: ChunkTypeDefinitions,
					Title:     fmt.Sprintf("Definitions: %s", relPath),
					Text:      text,
					Tags:      tags,
					Metadata:  metadata,
					CreatedAt: now,
					UpdatedAt: now,
				}
				definitions = append(definitions, chunk)
			}
		}

		// Create data chunk
		if extraction.Data != nil && (len(extraction.Data.Constants) > 0 || len(extraction.Data.Variables) > 0) {
			text := idx.formatter.FormatData(extraction.Data, extraction.Language)
			if text != "" {
				tags := []string{"code", extraction.Language, "data"}
				metadata := map[string]interface{}{
					"source":    "code",
					"file_path": relPath,
					"language":  extraction.Language,
				}
				// Store tags as indexed metadata keys for chromem-go WHERE filtering
				for i, tag := range tags {
					metadata[fmt.Sprintf("tag_%d", i)] = tag
				}
				chunk := Chunk{
					ID:        fmt.Sprintf("code-data-%s", relPath),
					ChunkType: ChunkTypeData,
					Title:     fmt.Sprintf("Data: %s", relPath),
					Text:      text,
					Tags:      tags,
					Metadata:  metadata,
					CreatedAt: now,
					UpdatedAt: now,
				}
				data = append(data, chunk)
			}
		}

		chunkingTime += time.Since(chunkStart)
		idx.progress.OnFileProcessed(file)
	}

	log.Printf("[TIMING]   - Parsing (tree-sitter): %v\n", parsingTime)
	log.Printf("[TIMING]   - Chunking (formatting): %v\n", chunkingTime)

	// Generate embeddings
	totalChunks := len(symbols) + len(definitions) + len(data)
	if totalChunks > 0 {
		idx.progress.OnEmbeddingStart(totalChunks)
	}

	embeddingStart := time.Now()
	if len(symbols) > 0 {
		if err := idx.embedChunks(ctx, symbols); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to embed symbols: %w", err)
		}
		idx.progress.OnEmbeddingProgress(len(symbols))
	}
	if len(definitions) > 0 {
		if err := idx.embedChunks(ctx, definitions); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to embed definitions: %w", err)
		}
		idx.progress.OnEmbeddingProgress(len(symbols) + len(definitions))
	}
	if len(data) > 0 {
		if err := idx.embedChunks(ctx, data); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to embed data: %w", err)
		}
		idx.progress.OnEmbeddingProgress(totalChunks)
	}
	embeddingTime := time.Since(embeddingStart)
	log.Printf("[TIMING]   - Embedding: %v (%d chunks)\n", embeddingTime, totalChunks)

	return symbols, definitions, data, nil
}

// processDocFiles processes documentation files and returns chunks.
func (idx *indexer) processDocFiles(ctx context.Context, files []string) ([]Chunk, error) {
	chunks := []Chunk{}

	var chunkingTime, formattingTime time.Duration

	for _, file := range files {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		chunkStart := time.Now()
		docChunks, err := idx.chunker.ChunkDocument(ctx, file, "")
		chunkingTime += time.Since(chunkStart)
		if err != nil {
			log.Printf("Warning: failed to chunk %s: %v\n", file, err)
			idx.progress.OnFileProcessed(file)
			continue
		}

		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		now := time.Now()

		formatStart := time.Now()
		for _, dc := range docChunks {
			text := idx.formatter.FormatDocumentation(&dc)
			chunkID := fmt.Sprintf("doc-%s-s%d", relPath, dc.SectionIndex)
			if dc.ChunkIndex > 0 {
				chunkID = fmt.Sprintf("doc-%s-s%d-c%d", relPath, dc.SectionIndex, dc.ChunkIndex)
			}

			tags := []string{"documentation", "markdown"}
			metadata := map[string]interface{}{
				"source":        "markdown",
				"file_path":     relPath,
				"section_index": dc.SectionIndex,
				"chunk_index":   dc.ChunkIndex,
				"start_line":    dc.StartLine,
				"end_line":      dc.EndLine,
			}
			// Store tags as indexed metadata keys for chromem-go WHERE filtering
			for i, tag := range tags {
				metadata[fmt.Sprintf("tag_%d", i)] = tag
			}
			chunk := Chunk{
				ID:        chunkID,
				ChunkType: ChunkTypeDocumentation,
				Title:     fmt.Sprintf("Documentation: %s (section %d)", relPath, dc.SectionIndex),
				Text:      text,
				Tags:      tags,
				Metadata:  metadata,
				CreatedAt: now,
				UpdatedAt: now,
			}
			chunks = append(chunks, chunk)
		}
		formattingTime += time.Since(formatStart)

		idx.progress.OnFileProcessed(file)
	}

	log.Printf("[TIMING]   - Chunking (markdown parsing): %v\n", chunkingTime)
	log.Printf("[TIMING]   - Formatting: %v\n", formattingTime)

	// Generate embeddings
	embeddingStart := time.Now()
	if len(chunks) > 0 {
		idx.progress.OnEmbeddingStart(len(chunks))
		if err := idx.embedChunks(ctx, chunks); err != nil {
			return nil, fmt.Errorf("failed to embed documentation: %w", err)
		}
		idx.progress.OnEmbeddingProgress(len(chunks))
	}
	embeddingTime := time.Since(embeddingStart)
	log.Printf("[TIMING]   - Embedding: %v (%d chunks)\n", embeddingTime, len(chunks))

	return chunks, nil
}

// embedChunks generates embeddings for chunks with progress feedback.
func (idx *indexer) embedChunks(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Extract texts
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Text
	}

	// Create progress channel
	progressCh := make(chan embed.BatchProgress, 10)

	// Handle progress updates in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		for progress := range progressCh {
			idx.progress.OnEmbeddingProgress(progress.ProcessedChunks)
		}
	}()

	// Embed with batching for progress feedback
	// Batch size 50 = progress update every ~1.5s (50 chunks × 30ms)
	embeddings, err := embed.EmbedWithProgress(
		ctx,
		idx.provider,
		texts,
		embed.EmbedModePassage,
		50, // Batch size for ~1s progress updates
		progressCh,
	)

	// Close progress channel and wait for handler
	close(progressCh)
	<-done

	if err != nil {
		return err
	}

	// Assign embeddings to chunks
	for i := range chunks {
		chunks[i].Embedding = embeddings[i]
	}

	return nil
}

// writeChunkFiles writes chunk files using the configured storage backend.
// If incremental is true, uses WriteChunksIncremental which only updates chunks
// for changed files. If false, uses WriteChunks which replaces all chunks.
func (idx *indexer) writeChunkFiles(symbols, definitions, data, docs []Chunk, incremental bool) error {
	// Combine all chunks for writing
	allChunks := make([]Chunk, 0, len(symbols)+len(definitions)+len(data)+len(docs))
	allChunks = append(allChunks, symbols...)
	allChunks = append(allChunks, definitions...)
	allChunks = append(allChunks, data...)
	allChunks = append(allChunks, docs...)

	// Use appropriate write method based on incremental flag
	if incremental {
		return idx.storage.WriteChunksIncremental(allChunks)
	}
	return idx.storage.WriteChunks(allChunks)
}

// buildAndSaveGraph builds the graph from code files and saves it.
// If deletedFiles is nil, performs full build. Otherwise, performs incremental update.
func (idx *indexer) buildAndSaveGraph(ctx context.Context, changedFiles, deletedFiles, allFiles []string) error {
	// Create graph directory path
	graphDir := filepath.Join(idx.config.OutputDir, "graph")

	// Create storage
	storage, err := graph.NewStorage(graphDir)
	if err != nil {
		return fmt.Errorf("failed to create graph storage: %w", err)
	}

	// Create builder with progress reporter
	builder := graph.NewBuilder(idx.config.RootDir, graph.WithProgress(idx.wrapProgressReporter()))

	var graphData *graph.GraphData

	if deletedFiles == nil {
		// Full build
		graphData, err = builder.BuildFull(ctx, allFiles)
	} else {
		// Incremental build
		previousGraph, err := storage.Load()
		if err != nil {
			return fmt.Errorf("failed to load previous graph: %w", err)
		}
		graphData, err = builder.BuildIncremental(ctx, previousGraph, changedFiles, deletedFiles, allFiles)
	}

	if err != nil {
		return fmt.Errorf("failed to build graph: %w", err)
	}

	// Save graph
	if err := storage.Save(graphData); err != nil {
		return fmt.Errorf("failed to save graph: %w", err)
	}

	return nil
}

// calculateChecksum calculates SHA-256 checksum of a file.
func calculateChecksum(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// collectFileMetadata collects file-level statistics for a single file.
// Returns FileStats with all required fields populated.
func collectFileMetadata(rootDir, filePath string) (*storage.FileStats, error) {
	// Get relative path
	relPath, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get relative path: %w", err)
	}

	// Get file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// Calculate checksum
	checksum, err := calculateChecksum(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}

	// Detect language from extension
	language := detectLanguage(filePath)

	// Detect if test file
	isTest := isTestFile(filePath)

	// Count lines
	lineCounts, err := countLines(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to count lines: %w", err)
	}

	// Extract module path (package path for code files)
	modulePath := extractModulePath(rootDir, relPath)

	return &storage.FileStats{
		FilePath:         relPath,
		Language:         language,
		ModulePath:       modulePath,
		IsTest:           isTest,
		LineCountTotal:   lineCounts.Total,
		LineCountCode:    lineCounts.Code,
		LineCountComment: lineCounts.Comment,
		LineCountBlank:   lineCounts.Blank,
		SizeBytes:        fileInfo.Size(),
		FileHash:         checksum,
		LastModified:     fileInfo.ModTime(),
		IndexedAt:        time.Now(),
	}, nil
}

// LineCounts holds line count statistics for a file.
type LineCounts struct {
	Total   int
	Code    int
	Comment int
	Blank   int
}

// countLines counts different types of lines in a file.
func countLines(filePath string) (*LineCounts, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	counts := &LineCounts{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		counts.Total++
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			counts.Blank++
		} else if isCommentLine(line, filePath) {
			counts.Comment++
		} else {
			counts.Code++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return counts, nil
}

// isCommentLine determines if a line is a comment based on language.
func isCommentLine(line, filePath string) bool {
	ext := filepath.Ext(filePath)

	switch ext {
	case ".go", ".js", ".ts", ".tsx", ".jsx", ".c", ".cpp", ".h", ".java", ".rs", ".php":
		return strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*")
	case ".py", ".rb", ".sh":
		return strings.HasPrefix(line, "#")
	default:
		return false
	}
}

// isTestFile determines if a file is a test file.
func isTestFile(filePath string) bool {
	base := filepath.Base(filePath)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.js") ||
		strings.Contains(filePath, "/test/") ||
		strings.Contains(filePath, "/tests/") ||
		strings.Contains(filePath, "__tests__")
}

// extractModulePath extracts the module/package path from a file path.
// For Go: internal/indexer/impl.go -> internal/indexer
// For others: returns directory path
func extractModulePath(rootDir, relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return ""
	}
	return dir
}

// graphProgressAdapter adapts indexer.ProgressReporter to graph.GraphProgressReporter
type graphProgressAdapter struct {
	reporter ProgressReporter
}

func (a *graphProgressAdapter) OnGraphBuildingStart(totalFiles int) {
	if a.reporter != nil {
		a.reporter.OnGraphBuildingStart(totalFiles)
	}
}

func (a *graphProgressAdapter) OnGraphFileProcessed(processedFiles, totalFiles int, fileName string) {
	if a.reporter != nil {
		a.reporter.OnGraphFileProcessed(processedFiles, totalFiles, fileName)
	}
}

func (a *graphProgressAdapter) OnGraphBuildingComplete(nodeCount, edgeCount int, duration time.Duration) {
	if a.reporter != nil {
		a.reporter.OnGraphBuildingComplete(nodeCount, edgeCount, duration)
	}
}

func (idx *indexer) wrapProgressReporter() graph.GraphProgressReporter {
	if idx.progress == nil {
		return nil
	}
	return &graphProgressAdapter{reporter: idx.progress}
}
