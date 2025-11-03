package pattern

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// SupportedLanguages defines all languages that ast-grep supports
var SupportedLanguages = map[string]bool{
	"go":         true,
	"typescript": true,
	"javascript": true,
	"tsx":        true,
	"jsx":        true,
	"python":     true,
	"rust":       true,
	"c":          true,
	"cpp":        true,
	"java":       true,
	"php":        true,
	"ruby":       true,
}

// ValidStrictnessLevels defines all valid strictness levels for ast-grep
var ValidStrictnessLevels = map[string]bool{
	"cst":       true,
	"smart":     true,
	"ast":       true,
	"relaxed":   true,
	"signature": true,
}

const (
	DefaultContextLines = 3
	MinContextLines     = 0
	MaxContextLines     = 10

	DefaultLimit = 50
	MinLimit     = 1
	MaxLimit     = 100

	DefaultStrictness = "smart"
)

// ValidateRequest validates a PatternRequest and returns an error if invalid
func ValidateRequest(req *PatternRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	// Required fields
	if req.Pattern == "" {
		return errors.New("pattern is required")
	}
	if req.Language == "" {
		return errors.New("language is required")
	}

	// Validate language
	if !SupportedLanguages[req.Language] {
		return fmt.Errorf("unsupported language: %s (supported: go, typescript, javascript, tsx, jsx, python, rust, c, cpp, java, php, ruby)", req.Language)
	}

	// Validate strictness
	if req.Strictness != "" && !ValidStrictnessLevels[req.Strictness] {
		return fmt.Errorf("invalid strictness: %s (valid: cst, smart, ast, relaxed, signature)", req.Strictness)
	}

	// Validate context lines
	if req.ContextLines != nil {
		if *req.ContextLines < MinContextLines || *req.ContextLines > MaxContextLines {
			return fmt.Errorf("context_lines must be between %d and %d", MinContextLines, MaxContextLines)
		}
	}

	// Validate limit
	if req.Limit != nil {
		if *req.Limit < MinLimit || *req.Limit > MaxLimit {
			return fmt.Errorf("limit must be between %d and %d", MinLimit, MaxLimit)
		}
	}

	return nil
}

// BuildCommand constructs a safe argv array for ast-grep execution.
// This function NEVER uses shell execution - it builds argv directly.
// Security: All file paths are validated to prevent directory traversal attacks.
func BuildCommand(req *PatternRequest, projectRoot string) ([]string, error) {
	// Validate request first
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}

	// Clean the project root to get absolute canonical path
	cleanRoot := filepath.Clean(projectRoot)
	if !filepath.IsAbs(cleanRoot) {
		return nil, fmt.Errorf("project root must be absolute path: %s", projectRoot)
	}

	// Build argv directly (never use shell)
	args := []string{
		"--pattern", req.Pattern,
		"--lang", req.Language,
		"--json=compact", // Always use compact JSON output
	}

	// Add context lines (-C flag)
	contextLines := DefaultContextLines
	if req.ContextLines != nil {
		contextLines = *req.ContextLines
	}
	if contextLines > 0 {
		args = append(args, "-C", strconv.Itoa(contextLines))
	}

	// Add strictness
	strictness := DefaultStrictness
	if req.Strictness != "" {
		strictness = req.Strictness
	}
	args = append(args, "--strictness", strictness)

	// Add file path filters (validate first!)
	if len(req.FilePaths) > 0 {
		// Security: Validate EVERY path to prevent directory traversal
		for _, path := range req.FilePaths {
			if err := validateFilePath(path, cleanRoot); err != nil {
				return nil, err
			}
		}
		// All paths validated, add to command
		args = append(args, "--globs", strings.Join(req.FilePaths, ","))
	}

	// Search current directory (command will be run with cwd=projectRoot)
	args = append(args, ".")

	return args, nil
}

// validateFilePath validates that a file path is safe and within the project root.
// This prevents directory traversal attacks like "../../../etc/passwd".
func validateFilePath(path string, projectRoot string) error {
	// Reject absolute paths outright
	if filepath.IsAbs(path) {
		return fmt.Errorf("path outside project root: %s (absolute paths not allowed)", path)
	}

	// Clean the path to resolve any ".." or "." components
	cleanPath := filepath.Clean(path)

	// Join with project root and clean again
	absPath := filepath.Join(projectRoot, cleanPath)
	absPath = filepath.Clean(absPath)

	// Verify the resulting path is still within project root
	// Use filepath.Clean on both sides to ensure consistent comparison
	if !strings.HasPrefix(absPath, projectRoot+string(filepath.Separator)) &&
		absPath != projectRoot {
		return fmt.Errorf("path outside project root: %s", path)
	}

	// Additional checks for malicious patterns
	// Check for ".." in the original path (before cleaning)
	if strings.Contains(path, "..") {
		// Verify the cleaned path doesn't escape
		rel, err := filepath.Rel(projectRoot, absPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("path outside project root: %s", path)
		}
	}

	return nil
}

// GetContextLines returns the context lines value or default if nil
func GetContextLines(req *PatternRequest) int {
	if req.ContextLines != nil {
		return *req.ContextLines
	}
	return DefaultContextLines
}

// GetLimit returns the limit value or default if nil
func GetLimit(req *PatternRequest) int {
	if req.Limit != nil {
		return *req.Limit
	}
	return DefaultLimit
}

// GetStrictness returns the strictness value or default if empty
func GetStrictness(req *PatternRequest) string {
	if req.Strictness != "" {
		return req.Strictness
	}
	return DefaultStrictness
}
