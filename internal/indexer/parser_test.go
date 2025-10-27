package indexer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for Parser:
// - Parses Go files successfully
// - Extracts package name
// - Counts imports
// - Extracts type definitions (structs)
// - Extracts function definitions
// - Extracts constants
// - Extracts variables
// - Extracts function signatures
// - Detects language from file extension
// - Returns nil for unsupported languages

func TestParser_ParseGoFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewParser()

	// Test: Parse a simple Go file
	extraction, err := parser.ParseFile(ctx, "../../testdata/code/go/simple.go")

	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Check basic metadata
	assert.Equal(t, "go", extraction.Language)
	assert.Contains(t, extraction.FilePath, "simple.go")

	// Check symbols
	require.NotNil(t, extraction.Symbols)
	assert.Equal(t, "server", extraction.Symbols.PackageName)
	assert.Equal(t, 2, extraction.Symbols.ImportsCount) // fmt and net/http

	// Check types
	assert.GreaterOrEqual(t, len(extraction.Symbols.Types), 2) // Config and Handler
	hasConfig := false
	hasHandler := false
	for _, typ := range extraction.Symbols.Types {
		if typ.Name == "Config" {
			hasConfig = true
			assert.Equal(t, "struct", typ.Type)
		}
		if typ.Name == "Handler" {
			hasHandler = true
			assert.Equal(t, "struct", typ.Type)
		}
	}
	assert.True(t, hasConfig, "Should have Config type")
	assert.True(t, hasHandler, "Should have Handler type")

	// Check functions
	assert.GreaterOrEqual(t, len(extraction.Symbols.Functions), 2) // NewHandler and ServeHTTP
	hasNewHandler := false
	hasServeHTTP := false
	for _, fn := range extraction.Symbols.Functions {
		if fn.Name == "NewHandler" {
			hasNewHandler = true
		}
		if fn.Name == "ServeHTTP" {
			hasServeHTTP = true
			assert.Contains(t, fn.Signature, "Handler")
		}
	}
	assert.True(t, hasNewHandler, "Should have NewHandler function")
	assert.True(t, hasServeHTTP, "Should have ServeHTTP method")

	// Check constants
	require.NotNil(t, extraction.Data)
	assert.GreaterOrEqual(t, len(extraction.Data.Constants), 2) // DefaultPort and DefaultTimeout
	hasDefaultPort := false
	for _, constant := range extraction.Data.Constants {
		if constant.Name == "DefaultPort" {
			hasDefaultPort = true
		}
	}
	assert.True(t, hasDefaultPort, "Should have DefaultPort constant")

	// Check variables
	assert.GreaterOrEqual(t, len(extraction.Data.Variables), 1) // globalConfig
	hasGlobalConfig := false
	for _, variable := range extraction.Data.Variables {
		if variable.Name == "globalConfig" {
			hasGlobalConfig = true
		}
	}
	assert.True(t, hasGlobalConfig, "Should have globalConfig variable")

	// Check definitions
	require.NotNil(t, extraction.Definitions)
	assert.GreaterOrEqual(t, len(extraction.Definitions.Definitions), 4) // 2 types + 2 functions
}

func TestParser_SupportsLanguage(t *testing.T) {
	t.Parallel()

	parser := NewParser()

	// Test: Supports Go
	assert.True(t, parser.SupportsLanguage("go"))

	// Test: Does not support other languages (yet)
	assert.False(t, parser.SupportsLanguage("python"))
	assert.False(t, parser.SupportsLanguage("typescript"))
}

func TestParser_DetectLanguage(t *testing.T) {
	t.Parallel()

	// Test: Detect language from file extension
	tests := []struct {
		filePath string
		language string
	}{
		{"test.go", "go"},
		{"test.ts", "typescript"},
		{"test.tsx", "typescript"},
		{"test.js", "javascript"},
		{"test.jsx", "javascript"},
		{"test.py", "python"},
		{"test.rs", "rust"},
		{"test.c", "c"},
		{"test.cpp", "cpp"},
		{"test.php", "php"},
		{"test.rb", "ruby"},
		{"test.java", "java"},
		{"test.txt", "unknown"},
	}

	for _, tt := range tests {
		lang := detectLanguage(tt.filePath)
		assert.Equal(t, tt.language, lang, "file: %s", tt.filePath)
	}
}

func TestParser_UnsupportedLanguage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewParser()

	// Test: Unsupported language returns nil (not an error)
	extraction, err := parser.ParseFile(ctx, "../../testdata/code/test.py")

	require.NoError(t, err)
	assert.Nil(t, extraction)
}

func TestParser_InvalidGoFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewParser()

	// Test: Invalid Go file returns error
	_, err := parser.ParseFile(ctx, "../../testdata/code/go/nonexistent.go")

	require.Error(t, err)
}
