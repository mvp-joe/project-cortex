package indexer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// FileDiscovery handles file discovery with glob patterns and ignore rules.
type FileDiscovery struct {
	rootDir         string
	codePatterns    []glob.Glob
	docsPatterns    []glob.Glob
	ignorePatterns  []glob.Glob
}

// NewFileDiscovery creates a new file discovery instance.
func NewFileDiscovery(rootDir string, codePatterns, docsPatterns, ignorePatterns []string) (*FileDiscovery, error) {
	fd := &FileDiscovery{
		rootDir: rootDir,
	}

	// Compile glob patterns
	for _, pattern := range codePatterns {
		g, err := glob.Compile(pattern, '/')
		if err != nil {
			return nil, err
		}
		fd.codePatterns = append(fd.codePatterns, g)
	}

	for _, pattern := range docsPatterns {
		g, err := glob.Compile(pattern, '/')
		if err != nil {
			return nil, err
		}
		fd.docsPatterns = append(fd.docsPatterns, g)
	}

	for _, pattern := range ignorePatterns {
		g, err := glob.Compile(pattern, '/')
		if err != nil {
			return nil, err
		}
		fd.ignorePatterns = append(fd.ignorePatterns, g)
	}

	return fd, nil
}

// DiscoverFiles walks the directory tree and returns code and doc files.
func (fd *FileDiscovery) DiscoverFiles() (codeFiles []string, docFiles []string, err error) {
	codeFiles = []string{}
	docFiles = []string{}

	err = filepath.Walk(fd.rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Get relative path for pattern matching
		relPath, err := filepath.Rel(fd.rootDir, path)
		if err != nil {
			return err
		}

		// Normalize path separators for glob matching
		relPath = filepath.ToSlash(relPath)

		// Check ignore patterns
		if fd.shouldIgnore(relPath) {
			return nil
		}

		// Check code patterns
		if fd.matchesAnyPattern(relPath, fd.codePatterns) {
			codeFiles = append(codeFiles, path)
			return nil
		}

		// Check docs patterns
		if fd.matchesAnyPattern(relPath, fd.docsPatterns) {
			docFiles = append(docFiles, path)
			return nil
		}

		return nil
	})

	return codeFiles, docFiles, err
}

// shouldIgnore checks if a path matches any ignore pattern.
func (fd *FileDiscovery) shouldIgnore(relPath string) bool {
	// Always ignore .cortex directory
	if strings.HasPrefix(relPath, ".cortex/") || relPath == ".cortex" {
		return true
	}

	// Check if the path matches any ignore pattern
	if fd.matchesAnyPattern(relPath, fd.ignorePatterns) {
		return true
	}

	// Also check if this is a directory that would match with /** suffix
	// For example, "node_modules" should match pattern "node_modules/**"
	pathWithSuffix := relPath + "/**"
	return fd.matchesAnyPattern(pathWithSuffix, fd.ignorePatterns)
}

// matchesAnyPattern checks if a path matches any of the given patterns.
func (fd *FileDiscovery) matchesAnyPattern(path string, patterns []glob.Glob) bool {
	for _, pattern := range patterns {
		if pattern.Match(path) {
			return true
		}
	}
	return false
}
