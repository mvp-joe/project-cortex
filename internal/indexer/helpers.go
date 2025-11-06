package indexer

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvp-joe/project-cortex/internal/storage"
)

// calculateChecksum computes SHA-256 hash of a file.
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

// isTextFile determines if a file is text (vs binary) by reading the first 512 bytes
// and checking for null bytes. This is the same heuristic used by tools like 'file'.
// Returns false for binary files, true for text files.
func isTextFile(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	// Read first 512 bytes (or less if file is smaller)
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}

	// Check for null bytes (0x00) - indicates binary
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return false, nil
		}
	}

	return true, nil
}
