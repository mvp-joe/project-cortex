package mcp

// Implementation Plan:
// 1. LoadChunksFromSQLite - Load chunks from SQLite cache database
// 2. Detect cache location using cache.LoadOrCreateSettings
// 3. Get current branch using cache.GetCurrentBranch
// 4. Construct database path: ~/.cortex/cache/{cacheKey}/branches/{branch}.db
// 5. Use storage.ChunkReader to read all chunks
// 6. Convert storage.Chunk → mcp.ContextChunk
// 7. Derive tags from chunk type and file extension (language detection)

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// LoadChunksFromSQLite loads chunks from SQLite cache database.
// Returns chunks for the current git branch.
//
// This function:
// 1. Determines cache location from project settings
// 2. Identifies current git branch
// 3. Opens branch-specific SQLite database
// 4. Reads all chunks
// 5. Converts storage format to MCP format
// 6. Derives tags from chunk type and language
//
// Returns error if:
// - Settings cannot be loaded (invalid project state)
// - Database doesn't exist (user needs to run 'cortex index')
// - Database is corrupted or inaccessible
func LoadChunksFromSQLite(projectPath string) ([]*ContextChunk, error) {
	// 1. Load project settings to get cache location
	settings, err := cache.LoadOrCreateSettings(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load cache settings: %w", err)
	}

	// 2. Get current branch
	branch := cache.GetCurrentBranch(projectPath)

	// 3. Construct database path: ~/.cortex/cache/{cacheKey}/branches/{branch}.db
	dbPath := filepath.Join(settings.CacheLocation, "branches", fmt.Sprintf("%s.db", branch))

	// 4. Check if database exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("SQLite cache not found: %s (run 'cortex index' first)", dbPath)
	}

	// 5. Open chunk reader
	reader, err := storage.NewChunkReader(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open chunk reader: %w", err)
	}
	defer reader.Close()

	// 6. Read all chunks
	storageChunks, err := reader.ReadAllChunks()
	if err != nil {
		return nil, fmt.Errorf("failed to read chunks: %w", err)
	}

	// 7. Convert storage.Chunk → mcp.ContextChunk
	chunks := make([]*ContextChunk, len(storageChunks))
	for i, sc := range storageChunks {
		chunks[i] = &ContextChunk{
			ID:        sc.ID,
			ChunkType: sc.ChunkType,
			Title:     sc.Title,
			Text:      sc.Text,
			Embedding: sc.Embedding,
			Tags:      deriveTags(sc.ChunkType, sc.FilePath),
			Metadata: map[string]interface{}{
				"file_path":  sc.FilePath,
				"start_line": sc.StartLine,
				"end_line":   sc.EndLine,
			},
			CreatedAt: sc.CreatedAt,
			UpdatedAt: sc.UpdatedAt,
		}
	}

	log.Printf("✓ Loaded %d chunks from SQLite cache (branch: %s)", len(chunks), branch)

	// 8. Update branch access time in metadata
	if err := updateBranchAccessTime(settings.CacheLocation, branch, len(chunks)); err != nil {
		// Don't fail chunk loading if metadata update fails
		log.Printf("Warning: failed to update branch access time: %v", err)
	}

	return chunks, nil
}

// updateBranchAccessTime updates the last access time for a branch in cache metadata.
// This is used by the eviction system to track branch usage.
func updateBranchAccessTime(cacheDir, branch string, chunkCount int) error {
	// Load metadata
	metadata, err := cache.LoadMetadata(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	// Get database size
	sizeMB := cache.GetBranchDBSize(cacheDir, branch)

	// Update branch stats (this updates LastAccessed to now)
	metadata.UpdateBranchStats(branch, sizeMB, chunkCount)

	// Save metadata
	if err := metadata.Save(cacheDir); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// deriveTags creates tags from chunk type and file path (language detection).
// Tags are used for filtering in cortex_search queries.
//
// Tag structure:
// - Chunk type tag: "symbols", "definitions", "data", "documentation"
// - Language tag: "go", "typescript", "python", etc.
// - Content type tag: "code" (for programming languages, not added for documentation)
//
// Examples:
//   - internal/mcp/server.go (symbols) → ["symbols", "go", "code"]
//   - README.md (documentation) → ["documentation", "markdown"]
//   - types.ts (definitions) → ["definitions", "typescript", "code"]
func deriveTags(chunkType, filePath string) []string {
	tags := []string{chunkType}

	// Detect language from file extension
	ext := filepath.Ext(filePath)
	switch ext {
	case ".go":
		tags = append(tags, "go", "code")
	case ".ts", ".tsx":
		tags = append(tags, "typescript", "code")
	case ".js", ".jsx":
		tags = append(tags, "javascript", "code")
	case ".py":
		tags = append(tags, "python", "code")
	case ".rs":
		tags = append(tags, "rust", "code")
	case ".java":
		tags = append(tags, "java", "code")
	case ".c", ".h":
		tags = append(tags, "c", "code")
	case ".cpp", ".hpp", ".cc":
		tags = append(tags, "cpp", "code")
	case ".rb":
		tags = append(tags, "ruby", "code")
	case ".php":
		tags = append(tags, "php", "code")
	case ".md":
		tags = append(tags, "markdown")
	case ".txt":
		tags = append(tags, "text")
	// Add more as needed
	default:
		// Unknown extension, just use chunk type
	}

	return tags
}
