package graph

import (
	"database/sql"
	"fmt"
	"strings"
)

// LineRange represents a line-based range in a file (for display).
type LineRange struct {
	Start int // 1-indexed line number
	End   int // 1-indexed line number
}

// ByteRange represents a byte-based range in a file (for extraction).
type ByteRange struct {
	Start int // 0-indexed byte offset
	End   int // 0-indexed byte offset
}

// ContextExtractor extracts code context from SQLite file content.
type ContextExtractor struct {
	db *sql.DB
}

// NewContextExtractor creates a new context extractor.
func NewContextExtractor(db *sql.DB) *ContextExtractor {
	return &ContextExtractor{db: db}
}

// ExtractContext extracts code snippet with context lines around target range.
// Uses byte positions for efficient extraction from SQLite without reading full files.
//
// Parameters:
//   - filePath: Path to the file in the database
//   - lines: Line range for display (1-indexed)
//   - pos: Byte range for extraction (0-indexed)
//   - contextLines: Number of additional lines to include before/after target
//
// Returns:
//   - Formatted snippet with line number prefix (e.g., "// Lines 10-25\n...")
//   - Error if extraction fails
func (ce *ContextExtractor) ExtractContext(
	filePath string,
	lines LineRange,
	pos ByteRange,
	contextLines int,
) (string, error) {
	const estimatedCharsPerLine = 120 // Reasonable for most code

	// Calculate byte window (overfetch by 1 line for safety)
	contextBytes := (contextLines + 1) * estimatedCharsPerLine
	fromPos := max(0, pos.Start-contextBytes)
	toPos := pos.End + contextBytes

	// Extract chunk from SQLite (substr is 1-indexed)
	var chunk string
	err := ce.db.QueryRow(`
		SELECT substr(content, ?, ?) FROM files WHERE file_path = ?
	`, fromPos+1, toPos-fromPos, filePath).Scan(&chunk)
	if err != nil {
		return "", fmt.Errorf("extract chunk: %w", err)
	}

	// Split chunk into lines
	chunkLines := strings.Split(chunk, "\n")

	// Find target position in chunk by counting newlines before the start position
	relativePos := pos.Start - fromPos
	newlinesBeforeTarget := countNewlines(chunk[:relativePos])

	// The target line is at index newlinesBeforeTarget (0-indexed in chunkLines array)
	// Line 1 in file = index 0 in array
	targetLineIndexInChunk := newlinesBeforeTarget

	// Calculate desired window (inclusive)
	targetSpan := lines.End - lines.Start // Number of lines in target (0 for single line)
	desiredFrom := targetLineIndexInChunk - contextLines
	desiredTo := targetLineIndexInChunk + targetSpan + contextLines + 1 // +1 because we need inclusive end

	// Clamp to available lines
	actualFrom := max(0, desiredFrom)
	actualTo := min(len(chunkLines), desiredTo)

	// Extract snippet
	snippet := strings.Join(chunkLines[actualFrom:actualTo], "\n")

	// Calculate display line numbers
	// How many lines before target were excluded due to clamping?
	linesBeforeTargetExcluded := targetLineIndexInChunk - actualFrom
	displayStart := lines.Start - linesBeforeTargetExcluded
	displayEnd := displayStart + (actualTo - actualFrom) - 1

	prefix := fmt.Sprintf("// Lines %d-%d\n", displayStart, displayEnd)
	return prefix + snippet, nil
}

// countNewlines counts the number of newline characters in a string.
func countNewlines(s string) int {
	count := 0
	for _, ch := range s {
		if ch == '\n' {
			count++
		}
	}
	return count
}
