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

	"project-cortex/internal/embed"
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
}

// Close releases all resources held by the indexer.
func (idx *indexer) Close() error {
	if idx.provider != nil {
		return idx.provider.Close()
	}
	return nil
}

// New creates a new indexer instance.
func New(config *Config) (Indexer, error) {
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
		Provider:   config.EmbeddingProvider,
		BinaryPath: config.EmbeddingBinary,
		Endpoint:   config.EmbeddingEndpoint,
		Model:      config.EmbeddingModel,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding provider: %w", err)
	}

	return &indexer{
		config:    config,
		parser:    NewParser(),
		chunker:   NewChunker(config.DocChunkSize, config.Overlap),
		formatter: NewFormatter(),
		discovery: discovery,
		writer:    writer,
		provider:  provider,
	}, nil
}

// Index processes all files in the codebase and generates chunk files.
func (idx *indexer) Index(ctx context.Context) (*ProcessingStats, error) {
	startTime := time.Now()
	stats := &ProcessingStats{}

	log.Println("Discovering files...")
	codeFiles, docFiles, err := idx.discovery.DiscoverFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}

	log.Printf("Found %d code files and %d documentation files\n", len(codeFiles), len(docFiles))

	// Process code files
	symbolsChunks, defsChunks, dataChunks, err := idx.processCodeFiles(ctx, codeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to process code files: %w", err)
	}
	stats.CodeFilesProcessed = len(codeFiles)
	stats.TotalCodeChunks = len(symbolsChunks) + len(defsChunks) + len(dataChunks)

	// Process documentation files
	docChunks, err := idx.processDocFiles(ctx, docFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to process documentation files: %w", err)
	}
	stats.DocsProcessed = len(docFiles)
	stats.TotalDocChunks = len(docChunks)

	// Write chunk files
	log.Println("Writing chunk files...")
	if err := idx.writeChunkFiles(symbolsChunks, defsChunks, dataChunks, docChunks); err != nil {
		return nil, fmt.Errorf("failed to write chunk files: %w", err)
	}

	// Calculate checksums for incremental indexing
	checksums := make(map[string]string)
	for _, file := range append(codeFiles, docFiles...) {
		checksum, err := calculateChecksum(file)
		if err != nil {
			log.Printf("Warning: failed to calculate checksum for %s: %v\n", file, err)
			continue
		}
		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		checksums[relPath] = checksum
	}

	// Write metadata
	metadata := &GeneratorMetadata{
		Version:       "2.0.0",
		GeneratedAt:   time.Now(),
		FileChecksums: checksums,
		Stats:         *stats,
	}
	stats.ProcessingTimeSeconds = time.Since(startTime).Seconds()
	metadata.Stats.ProcessingTimeSeconds = stats.ProcessingTimeSeconds

	if err := idx.writer.WriteMetadata(metadata); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Printf("✓ Indexing complete: %d code chunks, %d doc chunks in %.2fs\n",
		stats.TotalCodeChunks, stats.TotalDocChunks, stats.ProcessingTimeSeconds)

	return stats, nil
}

// IndexIncremental processes only changed files based on checksums.
func (idx *indexer) IndexIncremental(ctx context.Context) (*ProcessingStats, error) {
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

	// Find changed files
	changedCode := []string{}
	changedDocs := []string{}

	for _, file := range codeFiles {
		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		oldChecksum := metadata.FileChecksums[relPath]
		newChecksum, err := calculateChecksum(file)
		if err != nil {
			log.Printf("Warning: failed to calculate checksum for %s: %v\n", file, err)
			continue
		}
		if oldChecksum != newChecksum {
			changedCode = append(changedCode, file)
		}
	}

	for _, file := range docFiles {
		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		oldChecksum := metadata.FileChecksums[relPath]
		newChecksum, err := calculateChecksum(file)
		if err != nil {
			log.Printf("Warning: failed to calculate checksum for %s: %v\n", file, err)
			continue
		}
		if oldChecksum != newChecksum {
			changedDocs = append(changedDocs, file)
		}
	}

	if len(changedCode) == 0 && len(changedDocs) == 0 {
		log.Println("No changes detected")
		return &metadata.Stats, nil
	}

	log.Printf("Processing %d changed code files and %d changed documentation files\n", len(changedCode), len(changedDocs))

	// For incremental updates, we'd need to:
	// 1. Load existing chunk files
	// 2. Remove chunks for changed files
	// 3. Add new chunks for changed files
	// 4. Write updated chunk files
	//
	// For now, we'll just do a full re-index (simpler implementation)
	// TODO: Implement true incremental updates
	return idx.Index(ctx)
}

// Watch starts watching for file changes and reindexes incrementally.
func (idx *indexer) Watch(ctx context.Context) error {
	// TODO: Implement file watching with fsnotify
	// For now, this is a placeholder
	return fmt.Errorf("watch mode not yet implemented")
}

// processCodeFiles processes code files and returns chunks.
func (idx *indexer) processCodeFiles(ctx context.Context, files []string) (symbols, definitions, data []Chunk, err error) {
	symbols = []Chunk{}
	definitions = []Chunk{}
	data = []Chunk{}

	for _, file := range files {
		// Parse file
		extraction, err := idx.parser.ParseFile(ctx, file)
		if err != nil {
			log.Printf("Warning: failed to parse %s: %v\n", file, err)
			continue
		}

		if extraction == nil {
			// Unsupported language
			continue
		}

		relPath, _ := filepath.Rel(idx.config.RootDir, file)
		now := time.Now()

		// Create symbols chunk
		if extraction.Symbols != nil {
			text := idx.formatter.FormatSymbols(extraction.Symbols, extraction.Language)
			if text != "" {
				chunk := Chunk{
					ID:        fmt.Sprintf("code-symbols-%s", relPath),
					ChunkType: ChunkTypeSymbols,
					Title:     fmt.Sprintf("Symbols: %s", relPath),
					Text:      text,
					Tags:      []string{"code", extraction.Language, "symbols"},
					Metadata: map[string]interface{}{
						"source":    "code",
						"file_path": relPath,
						"language":  extraction.Language,
						"package":   extraction.Symbols.PackageName,
					},
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
				chunk := Chunk{
					ID:        fmt.Sprintf("code-definitions-%s", relPath),
					ChunkType: ChunkTypeDefinitions,
					Title:     fmt.Sprintf("Definitions: %s", relPath),
					Text:      text,
					Tags:      []string{"code", extraction.Language, "definitions"},
					Metadata: map[string]interface{}{
						"source":    "code",
						"file_path": relPath,
						"language":  extraction.Language,
					},
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
				chunk := Chunk{
					ID:        fmt.Sprintf("code-data-%s", relPath),
					ChunkType: ChunkTypeData,
					Title:     fmt.Sprintf("Data: %s", relPath),
					Text:      text,
					Tags:      []string{"code", extraction.Language, "data"},
					Metadata: map[string]interface{}{
						"source":    "code",
						"file_path": relPath,
						"language":  extraction.Language,
					},
					CreatedAt: now,
					UpdatedAt: now,
				}
				data = append(data, chunk)
			}
		}
	}

	// Generate embeddings
	if len(symbols) > 0 {
		if err := idx.embedChunks(ctx, symbols); err != nil {
			log.Printf("Warning: failed to embed symbols: %v\n", err)
		}
	}
	if len(definitions) > 0 {
		if err := idx.embedChunks(ctx, definitions); err != nil {
			log.Printf("Warning: failed to embed definitions: %v\n", err)
		}
	}
	if len(data) > 0 {
		if err := idx.embedChunks(ctx, data); err != nil {
			log.Printf("Warning: failed to embed data: %v\n", err)
		}
	}

	return symbols, definitions, data, nil
}

// processDocFiles processes documentation files and returns chunks.
func (idx *indexer) processDocFiles(ctx context.Context, files []string) ([]Chunk, error) {
	chunks := []Chunk{}

	for _, file := range files {
		docChunks, err := idx.chunker.ChunkDocument(ctx, file, "")
		if err != nil {
			log.Printf("Warning: failed to chunk %s: %v\n", file, err)
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

			chunk := Chunk{
				ID:        chunkID,
				ChunkType: ChunkTypeDocumentation,
				Title:     fmt.Sprintf("Documentation: %s (section %d)", relPath, dc.SectionIndex),
				Text:      text,
				Tags:      []string{"documentation", "markdown"},
				Metadata: map[string]interface{}{
					"source":        "markdown",
					"file_path":     relPath,
					"section_index": dc.SectionIndex,
					"chunk_index":   dc.ChunkIndex,
					"start_line":    dc.StartLine,
					"end_line":      dc.EndLine,
				},
				CreatedAt: now,
				UpdatedAt: now,
			}
			chunks = append(chunks, chunk)
		}
	}

	// Generate embeddings
	if len(chunks) > 0 {
		if err := idx.embedChunks(ctx, chunks); err != nil {
			log.Printf("Warning: failed to embed documentation: %v\n", err)
		}
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

// calculateChecksum calculates SHA-256 checksum of a file.
func calculateChecksum(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
