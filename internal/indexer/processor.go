package indexer

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// Processor handles the parse → chunk → embed → write pipeline.
type Processor interface {
	// ProcessFiles parses, chunks, embeds, and writes files to database.
	// Returns statistics about what was processed.
	ProcessFiles(ctx context.Context, files []string) (*Stats, error)
}

// Stats tracks what was processed.
type Stats struct {
	CodeFilesProcessed int
	DocsProcessed      int
	TotalCodeChunks    int
	TotalDocChunks     int
	ProcessingTime     time.Duration
}

// processor implements Processor interface.
type processor struct {
	rootDir   string
	parser    Parser
	chunker   Chunker
	formatter Formatter
	provider  embed.Provider
	storage   Storage
	progress  ProgressReporter
}

// NewProcessor creates a new Processor instance.
func NewProcessor(
	rootDir string,
	parser Parser,
	chunker Chunker,
	formatter Formatter,
	provider embed.Provider,
	storage Storage,
	progress ProgressReporter,
) Processor {
	if progress == nil {
		progress = &NoOpProgressReporter{}
	}

	return &processor{
		rootDir:   rootDir,
		parser:    parser,
		chunker:   chunker,
		formatter: formatter,
		provider:  provider,
		storage:   storage,
		progress:  progress,
	}
}

// ProcessFiles processes a list of files through the complete pipeline.
func (p *processor) ProcessFiles(ctx context.Context, files []string) (*Stats, error) {
	startTime := time.Now()
	stats := &Stats{}

	if len(files) == 0 {
		return stats, nil
	}

	// Separate code files from doc files
	codeFiles, docFiles := p.separateFiles(files)

	// Phase 1: Collect file metadata for ALL files (code + docs)
	// This must happen BEFORE writing chunks due to foreign key constraints
	phaseStart := time.Now()
	fileStatsMap := make(map[string]*storage.FileStats)

	log.Printf("Collecting file metadata for %d files...\n", len(files))
	for _, file := range files {
		fileStats, err := collectFileMetadata(p.rootDir, file)
		if err != nil {
			log.Printf("Warning: failed to collect metadata for %s: %v\n", file, err)
			continue
		}
		fileStatsMap[fileStats.FilePath] = fileStats
	}
	log.Printf("[TIMING] Collect file metadata: %v (%d files)\n", time.Since(phaseStart), len(fileStatsMap))

	// Phase 2: Write file stats + content atomically using unified WriteFile API
	// CRITICAL: Chunks have foreign key to files table, so files MUST exist first
	phaseStart = time.Now()
	writer := storage.NewFileWriter(p.storage.GetDB())

	skippedBinary := 0
	skippedError := 0

	log.Printf("Writing file metadata and content for %d files...\n", len(files))
	for _, file := range files {
		relPath, _ := filepath.Rel(p.rootDir, file)
		fileStats, exists := fileStatsMap[relPath]
		if !exists {
			log.Printf("Warning: no metadata for %s, skipping\n", relPath)
			skippedError++
			continue
		}

		// Check if file is text or binary
		isText, err := isTextFile(file)
		if err != nil {
			log.Printf("Warning: failed to check if %s is text: %v\n", file, err)
			skippedError++
			continue
		}

		// Prepare content pointer (nil for binary, &string for text)
		var content *string
		if isText {
			fileContent, err := os.ReadFile(file)
			if err != nil {
				log.Printf("Warning: failed to read %s: %v\n", file, err)
				skippedError++
				continue
			}
			contentStr := string(fileContent)
			content = &contentStr
		} else {
			content = nil // Binary file - no content stored
			skippedBinary++
		}

		// Single atomic write of stats + content
		if err := writer.WriteFile(fileStats, content); err != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", relPath, err)
		}
	}

	log.Printf("✓ Wrote file stats + content for %d files (%d binary skipped, %d errors)\n",
		len(files)-skippedBinary-skippedError, skippedBinary, skippedError)
	log.Printf("[TIMING] Write files (stats + content): %v\n", time.Since(phaseStart))

	// Check for cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 3: Process code files
	phaseStart = time.Now()
	symbolsChunks, defsChunks, dataChunks, err := p.processCodeFiles(ctx, codeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to process code files: %w", err)
	}
	stats.CodeFilesProcessed = len(codeFiles)
	stats.TotalCodeChunks = len(symbolsChunks) + len(defsChunks) + len(dataChunks)
	log.Printf("[TIMING] Process code files: %v (%d files -> %d chunks)\n",
		time.Since(phaseStart), len(codeFiles), stats.TotalCodeChunks)

	// Check for cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 4: Process documentation files
	phaseStart = time.Now()
	docChunks, err := p.processDocFiles(ctx, docFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to process documentation files: %w", err)
	}
	stats.DocsProcessed = len(docFiles)
	stats.TotalDocChunks = len(docChunks)
	log.Printf("[TIMING] Process doc files: %v (%d files -> %d chunks)\n",
		time.Since(phaseStart), len(docFiles), stats.TotalDocChunks)

	// Check for cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 5: Write chunks to storage
	phaseStart = time.Now()
	p.progress.OnWritingChunks()
	if err := p.writeChunks(symbolsChunks, defsChunks, dataChunks, docChunks); err != nil {
		return nil, fmt.Errorf("failed to write chunks: %w", err)
	}
	log.Printf("[TIMING] Write chunks: %v\n", time.Since(phaseStart))

	stats.ProcessingTime = time.Since(startTime)
	return stats, nil
}

// separateFiles separates files into code files and documentation files.
func (p *processor) separateFiles(files []string) (codeFiles, docFiles []string) {
	for _, file := range files {
		ext := filepath.Ext(file)
		if ext == ".md" || ext == ".markdown" {
			docFiles = append(docFiles, file)
		} else {
			codeFiles = append(codeFiles, file)
		}
	}
	return codeFiles, docFiles
}

// processCodeFiles processes code files and returns chunks.
func (p *processor) processCodeFiles(ctx context.Context, files []string) (symbols, definitions, data []Chunk, err error) {
	symbols = []Chunk{}
	definitions = []Chunk{}
	data = []Chunk{}

	if len(files) == 0 {
		return symbols, definitions, data, nil
	}

	p.progress.OnFileProcessingStart(len(files))

	var parsingTime, chunkingTime time.Duration

	for _, file := range files {
		// Check for cancellation
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}

		// Parse file
		parseStart := time.Now()
		extraction, err := p.parser.ParseFile(ctx, file)
		parsingTime += time.Since(parseStart)
		if err != nil {
			log.Printf("Warning: failed to parse %s: %v\n", file, err)
			p.progress.OnFileProcessed(file)
			continue
		}

		if extraction == nil {
			// Unsupported language
			p.progress.OnFileProcessed(file)
			continue
		}

		relPath, _ := filepath.Rel(p.rootDir, file)
		now := time.Now()
		chunkStart := time.Now()

		// Create symbols chunk
		if extraction.Symbols != nil {
			text := p.formatter.FormatSymbols(extraction.Symbols, extraction.Language)
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
			text := p.formatter.FormatDefinitions(extraction.Definitions, extraction.Language)
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
			text := p.formatter.FormatData(extraction.Data, extraction.Language)
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
		p.progress.OnFileProcessed(file)
	}

	log.Printf("[TIMING]   - Parsing (tree-sitter): %v\n", parsingTime)
	log.Printf("[TIMING]   - Chunking (formatting): %v\n", chunkingTime)

	// Generate embeddings
	totalChunks := len(symbols) + len(definitions) + len(data)
	if totalChunks > 0 {
		p.progress.OnEmbeddingStart(totalChunks)
	}

	embeddingStart := time.Now()
	if len(symbols) > 0 {
		if err := p.embedChunks(ctx, symbols); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to embed symbols: %w", err)
		}
		p.progress.OnEmbeddingProgress(len(symbols))
	}
	if len(definitions) > 0 {
		if err := p.embedChunks(ctx, definitions); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to embed definitions: %w", err)
		}
		p.progress.OnEmbeddingProgress(len(symbols) + len(definitions))
	}
	if len(data) > 0 {
		if err := p.embedChunks(ctx, data); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to embed data: %w", err)
		}
		p.progress.OnEmbeddingProgress(totalChunks)
	}
	embeddingTime := time.Since(embeddingStart)
	log.Printf("[TIMING]   - Embedding: %v (%d chunks)\n", embeddingTime, totalChunks)

	return symbols, definitions, data, nil
}

// processDocFiles processes documentation files and returns chunks.
func (p *processor) processDocFiles(ctx context.Context, files []string) ([]Chunk, error) {
	chunks := []Chunk{}

	if len(files) == 0 {
		return chunks, nil
	}

	var chunkingTime, formattingTime time.Duration

	for _, file := range files {
		// Check for cancellation
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		chunkStart := time.Now()
		docChunks, err := p.chunker.ChunkDocument(ctx, file, "")
		chunkingTime += time.Since(chunkStart)
		if err != nil {
			log.Printf("Warning: failed to chunk %s: %v\n", file, err)
			p.progress.OnFileProcessed(file)
			continue
		}

		relPath, _ := filepath.Rel(p.rootDir, file)
		now := time.Now()

		formatStart := time.Now()
		for _, dc := range docChunks {
			text := p.formatter.FormatDocumentation(&dc)
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

		p.progress.OnFileProcessed(file)
	}

	log.Printf("[TIMING]   - Chunking (markdown parsing): %v\n", chunkingTime)
	log.Printf("[TIMING]   - Formatting: %v\n", formattingTime)

	// Generate embeddings
	embeddingStart := time.Now()
	if len(chunks) > 0 {
		p.progress.OnEmbeddingStart(len(chunks))
		if err := p.embedChunks(ctx, chunks); err != nil {
			return nil, fmt.Errorf("failed to embed documentation: %w", err)
		}
		p.progress.OnEmbeddingProgress(len(chunks))
	}
	embeddingTime := time.Since(embeddingStart)
	log.Printf("[TIMING]   - Embedding: %v (%d chunks)\n", embeddingTime, len(chunks))

	return chunks, nil
}

// embedChunks generates embeddings for chunks with progress feedback.
func (p *processor) embedChunks(ctx context.Context, chunks []Chunk) error {
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
			p.progress.OnEmbeddingProgress(progress.ProcessedChunks)
		}
	}()

	// Embed with batching for progress feedback
	// Batch size 50 = progress update every ~1.5s (50 chunks × 30ms)
	embeddings, err := embed.EmbedWithProgress(
		ctx,
		p.provider,
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

// writeChunks writes chunks to storage using WriteChunksIncremental.
func (p *processor) writeChunks(symbols, definitions, data, docs []Chunk) error {
	// Combine all chunks for writing
	allChunks := make([]Chunk, 0, len(symbols)+len(definitions)+len(data)+len(docs))
	allChunks = append(allChunks, symbols...)
	allChunks = append(allChunks, definitions...)
	allChunks = append(allChunks, data...)
	allChunks = append(allChunks, docs...)

	if len(allChunks) == 0 {
		return nil
	}

	// Use incremental write (updates chunks for specific files)
	return p.storage.WriteChunksIncremental(allChunks)
}
