package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/graph"
)

// indexer implements the Indexer interface.
type indexer struct {
	config    *Config
	parser    Parser
	chunker   Chunker
	formatter Formatter
	discovery *FileDiscovery
	writer    *AtomicWriter
	provider  embed.Provider
	progress  ProgressReporter
}

// Close releases all resources held by the indexer.
func (idx *indexer) Close() error {
	if idx.provider != nil {
		return idx.provider.Close()
	}
	return nil
}

// getWriter returns the atomic writer for testing purposes.
// This is an unexported method only used in tests.
func (idx *indexer) getWriter() *AtomicWriter {
	return idx.writer
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

	// Create atomic writer
	writer, err := NewAtomicWriter(config.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create atomic writer: %w", err)
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
		writer:    writer,
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

	// Create atomic writer
	writer, err := NewAtomicWriter(config.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create atomic writer: %w", err)
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
		writer:    writer,
		provider:  provider,
		progress:  progress,
	}, nil
}

// Index processes all files in the codebase and generates chunk files.
func (idx *indexer) Index(ctx context.Context) (*ProcessingStats, error) {
	startTime := time.Now()
	stats := &ProcessingStats{}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	idx.progress.OnDiscoveryStart()
	codeFiles, docFiles, err := idx.discovery.DiscoverFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}
	idx.progress.OnDiscoveryComplete(len(codeFiles), len(docFiles))

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Process code files
	symbolsChunks, defsChunks, dataChunks, err := idx.processCodeFiles(ctx, codeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to process code files: %w", err)
	}
	stats.CodeFilesProcessed = len(codeFiles)
	stats.TotalCodeChunks = len(symbolsChunks) + len(defsChunks) + len(dataChunks)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Process documentation files
	docChunks, err := idx.processDocFiles(ctx, docFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to process documentation files: %w", err)
	}
	stats.DocsProcessed = len(docFiles)
	stats.TotalDocChunks = len(docChunks)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Write chunk files
	idx.progress.OnWritingChunks()
	if err := idx.writeChunkFiles(symbolsChunks, defsChunks, dataChunks, docChunks); err != nil {
		return nil, fmt.Errorf("failed to write chunk files: %w", err)
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Build and save graph
	log.Println("Building code graph...")
	if err := idx.buildAndSaveGraph(ctx, codeFiles, nil, codeFiles); err != nil {
		log.Printf("Warning: failed to build graph: %v\n", err)
		// Don't fail indexing if graph fails - it's supplementary
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Calculate checksums and mtimes for incremental indexing
	checksums := make(map[string]string)
	mtimes := make(map[string]time.Time)
	for _, file := range append(codeFiles, docFiles...) {
		checksum, err := calculateChecksum(file)
		if err != nil {
			log.Printf("Warning: failed to calculate checksum for %s: %v\n", file, err)
			continue
		}
		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		checksums[relPath] = checksum

		// Capture mtime
		if fileInfo, err := os.Stat(file); err == nil {
			mtimes[relPath] = fileInfo.ModTime()
		}
	}

	// Write metadata
	metadata := &GeneratorMetadata{
		Version:       "2.0.0",
		GeneratedAt:   time.Now(),
		FileChecksums: checksums,
		FileMtimes:    mtimes,
		Stats:         *stats,
	}
	stats.ProcessingTimeSeconds = time.Since(startTime).Seconds()
	metadata.Stats.ProcessingTimeSeconds = stats.ProcessingTimeSeconds

	if err := idx.writer.WriteMetadata(metadata); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	idx.progress.OnComplete(stats)

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
	metadata, err := idx.writer.ReadMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	// Discover all files
	codeFiles, docFiles, err := idx.discovery.DiscoverFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}

	// Calculate checksums and detect changes using two-stage filtering
	currentFiles := make(map[string]string)    // relPath -> absolute path
	changedFiles := make(map[string]bool)      // relPath -> true if changed
	newChecksums := make(map[string]string)    // relPath -> checksum
	newMtimes := make(map[string]time.Time)    // relPath -> mtime

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

	// Load existing chunks from all chunk files
	log.Println("Loading existing chunks...")
	existingChunks, err := idx.loadAllChunks()
	if err != nil {
		return nil, fmt.Errorf("failed to load existing chunks: %w", err)
	}

	// Build file_path → chunks index for efficient removal
	fileChunksIndex := idx.buildFileChunksIndex(existingChunks)

	// Filter out chunks for changed and deleted files
	log.Printf("Removing chunks for %d changed/deleted files...\n", len(changedFiles)+len(deletedFiles))
	filteredChunks := idx.filterChunks(existingChunks, changedFiles, deletedFiles)

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

	// Process only changed files
	newSymbols, newDefs, newData, err := idx.processCodeFiles(ctx, changedCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process changed code files: %w", err)
	}

	newDocs, err := idx.processDocFiles(ctx, changedDocs)
	if err != nil {
		return nil, fmt.Errorf("failed to process changed documentation files: %w", err)
	}

	// Merge filtered chunks with new chunks
	log.Println("Merging chunks...")
	mergedSymbols := append(filteredChunks[ChunkTypeSymbols], newSymbols...)
	mergedDefs := append(filteredChunks[ChunkTypeDefinitions], newDefs...)
	mergedData := append(filteredChunks[ChunkTypeData], newData...)
	mergedDocs := append(filteredChunks[ChunkTypeDocumentation], newDocs...)

	// Write merged chunk files
	log.Println("Writing chunk files...")
	if err := idx.writeChunkFiles(mergedSymbols, mergedDefs, mergedData, mergedDocs); err != nil {
		return nil, fmt.Errorf("failed to write chunk files: %w", err)
	}

	// Build and save graph incrementally
	log.Println("Updating code graph...")
	deletedPaths := make([]string, 0, len(deletedFiles))
	for relPath := range deletedFiles {
		deletedPaths = append(deletedPaths, relPath)
	}
	if err := idx.buildAndSaveGraph(ctx, append(changedCode, changedDocs...), deletedPaths, append(codeFiles, docFiles...)); err != nil {
		log.Printf("Warning: failed to update graph: %v\n", err)
		// Don't fail indexing if graph fails
	}

	// Calculate stats
	stats := &ProcessingStats{
		CodeFilesProcessed:    len(changedCode),
		DocsProcessed:         len(changedDocs),
		TotalCodeChunks:       len(mergedSymbols) + len(mergedDefs) + len(mergedData),
		TotalDocChunks:        len(mergedDocs),
		ProcessingTimeSeconds: time.Since(startTime).Seconds(),
	}

	// Write updated metadata with both checksums and mtimes
	newMetadata := &GeneratorMetadata{
		Version:       "2.0.0",
		GeneratedAt:   time.Now(),
		FileChecksums: newChecksums,
		FileMtimes:    newMtimes,
		Stats:         *stats,
	}

	if err := idx.writer.WriteMetadata(newMetadata); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Printf("✓ Incremental indexing complete: %d code chunks, %d doc chunks in %.2fs\n",
		stats.TotalCodeChunks, stats.TotalDocChunks, stats.ProcessingTimeSeconds)
	log.Printf("  Kept %d unchanged chunks, added %d new chunks, removed %d old chunks\n",
		len(filteredChunks[ChunkTypeSymbols])+len(filteredChunks[ChunkTypeDefinitions])+
			len(filteredChunks[ChunkTypeData])+len(filteredChunks[ChunkTypeDocumentation]),
		len(newSymbols)+len(newDefs)+len(newData)+len(newDocs),
		len(fileChunksIndex))

	return stats, nil
}

// loadAllChunks loads existing chunks from all chunk files.
func (idx *indexer) loadAllChunks() (map[ChunkType][]Chunk, error) {
	chunks := make(map[ChunkType][]Chunk)

	// Load code-symbols.json
	symbolsFile, err := idx.writer.ReadChunkFile("code-symbols.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read code-symbols.json: %w", err)
	}
	chunks[ChunkTypeSymbols] = symbolsFile.Chunks

	// Load code-definitions.json
	defsFile, err := idx.writer.ReadChunkFile("code-definitions.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read code-definitions.json: %w", err)
	}
	chunks[ChunkTypeDefinitions] = defsFile.Chunks

	// Load code-data.json
	dataFile, err := idx.writer.ReadChunkFile("code-data.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read code-data.json: %w", err)
	}
	chunks[ChunkTypeData] = dataFile.Chunks

	// Load doc-chunks.json
	docsFile, err := idx.writer.ReadChunkFile("doc-chunks.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read doc-chunks.json: %w", err)
	}
	chunks[ChunkTypeDocumentation] = docsFile.Chunks

	return chunks, nil
}

// buildFileChunksIndex builds an index: file_path → [chunk_ids] for efficient lookup.
func (idx *indexer) buildFileChunksIndex(chunks map[ChunkType][]Chunk) map[string][]string {
	index := make(map[string][]string)

	for _, chunkList := range chunks {
		for _, chunk := range chunkList {
			if filePath, ok := chunk.Metadata["file_path"].(string); ok {
				index[filePath] = append(index[filePath], chunk.ID)
			}
		}
	}

	return index
}

// filterChunks removes chunks for changed and deleted files, keeping only unchanged chunks.
func (idx *indexer) filterChunks(chunks map[ChunkType][]Chunk, changedFiles, deletedFiles map[string]bool) map[ChunkType][]Chunk {
	filtered := make(map[ChunkType][]Chunk)

	for chunkType, chunkList := range chunks {
		filteredList := []Chunk{}
		for _, chunk := range chunkList {
			filePath, ok := chunk.Metadata["file_path"].(string)
			if !ok {
				// No file_path metadata, skip this chunk
				log.Printf("Warning: chunk %s has no file_path metadata\n", chunk.ID)
				continue
			}

			// Keep chunk only if file is not changed and not deleted
			if !changedFiles[filePath] && !deletedFiles[filePath] {
				filteredList = append(filteredList, chunk)
			}
		}
		filtered[chunkType] = filteredList
	}

	return filtered
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

	for _, file := range files {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, nil, nil, ctx.Err()
		default:
		}

		// Parse file
		extraction, err := idx.parser.ParseFile(ctx, file)
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

		idx.progress.OnFileProcessed(file)
	}

	// Generate embeddings
	totalChunks := len(symbols) + len(definitions) + len(data)
	if totalChunks > 0 {
		idx.progress.OnEmbeddingStart(totalChunks)
	}

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

	return symbols, definitions, data, nil
}

// processDocFiles processes documentation files and returns chunks.
func (idx *indexer) processDocFiles(ctx context.Context, files []string) ([]Chunk, error) {
	chunks := []Chunk{}

	for _, file := range files {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		docChunks, err := idx.chunker.ChunkDocument(ctx, file, "")
		if err != nil {
			log.Printf("Warning: failed to chunk %s: %v\n", file, err)
			idx.progress.OnFileProcessed(file)
			continue
		}

		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		now := time.Now()

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

		idx.progress.OnFileProcessed(file)
	}

	// Generate embeddings
	if len(chunks) > 0 {
		idx.progress.OnEmbeddingStart(len(chunks))
		if err := idx.embedChunks(ctx, chunks); err != nil {
			return nil, fmt.Errorf("failed to embed documentation: %w", err)
		}
		idx.progress.OnEmbeddingProgress(len(chunks))
	}

	return chunks, nil
}

// embedChunks generates embeddings for chunks.
func (idx *indexer) embedChunks(ctx context.Context, chunks []Chunk) error {
	// Extract texts
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Text
	}

	// Generate embeddings
	embeddings, err := idx.provider.Embed(ctx, texts, embed.EmbedModePassage)
	if err != nil {
		return err
	}

	// Assign embeddings
	for i := range chunks {
		chunks[i].Embedding = embeddings[i]
	}

	return nil
}

// writeChunkFiles writes chunk files.
func (idx *indexer) writeChunkFiles(symbols, definitions, data, docs []Chunk) error {
	now := time.Now()
	dims := idx.provider.Dimensions()

	// Write symbols
	if len(symbols) > 0 {
		chunkFile := &ChunkFile{
			Metadata: ChunkFileMetadata{
				Model:      idx.config.EmbeddingModel,
				Dimensions: dims,
				ChunkType:  ChunkTypeSymbols,
				Generated:  now,
				Version:    "2.0.0",
			},
			Chunks: symbols,
		}
		if err := idx.writer.WriteChunkFile("code-symbols.json", chunkFile); err != nil {
			return err
		}
	}

	// Write definitions
	if len(definitions) > 0 {
		chunkFile := &ChunkFile{
			Metadata: ChunkFileMetadata{
				Model:      idx.config.EmbeddingModel,
				Dimensions: dims,
				ChunkType:  ChunkTypeDefinitions,
				Generated:  now,
				Version:    "2.0.0",
			},
			Chunks: definitions,
		}
		if err := idx.writer.WriteChunkFile("code-definitions.json", chunkFile); err != nil {
			return err
		}
	}

	// Write data
	if len(data) > 0 {
		chunkFile := &ChunkFile{
			Metadata: ChunkFileMetadata{
				Model:      idx.config.EmbeddingModel,
				Dimensions: dims,
				ChunkType:  ChunkTypeData,
				Generated:  now,
				Version:    "2.0.0",
			},
			Chunks: data,
		}
		if err := idx.writer.WriteChunkFile("code-data.json", chunkFile); err != nil {
			return err
		}
	}

	// Write docs
	if len(docs) > 0 {
		chunkFile := &ChunkFile{
			Metadata: ChunkFileMetadata{
				Model:      idx.config.EmbeddingModel,
				Dimensions: dims,
				ChunkType:  ChunkTypeDocumentation,
				Generated:  now,
				Version:    "2.0.0",
			},
			Chunks: docs,
		}
		if err := idx.writer.WriteChunkFile("doc-chunks.json", chunkFile); err != nil {
			return err
		}
	}

	return nil
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

	// Create builder
	builder := graph.NewBuilder(idx.config.RootDir)

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

	log.Printf("✓ Graph saved: %d nodes, %d edges\n", len(graphData.Nodes), len(graphData.Edges))
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
