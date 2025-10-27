package indexer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for Formatter:
// - FormatSymbols creates natural language text with package, imports, types, functions
// - FormatDefinitions creates code with line comments
// - FormatData creates code with line comments for constants and variables
// - FormatDocumentation returns markdown text as-is
// - Handles empty data gracefully
// - Formats line ranges correctly (single line vs range)
// - Language-specific formatting for constants and variables

func TestFormatter_FormatSymbols_GoFile(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Format symbols with package, imports, types, and functions
	data := &SymbolsData{
		PackageName:  "server",
		ImportsCount: 5,
		Types: []SymbolInfo{
			{Name: "Handler", Type: "struct", StartLine: 10, EndLine: 15},
			{Name: "Config", Type: "struct", StartLine: 20, EndLine: 25},
		},
		Functions: []SymbolInfo{
			{Name: "NewHandler", Signature: "NewHandler()", StartLine: 30, EndLine: 45},
			{Name: "ServeHTTP", Signature: "(Handler) ServeHTTP()", StartLine: 50, EndLine: 75},
		},
	}

	result := formatter.FormatSymbols(data, "go")

	require.NotEmpty(t, result)
	assert.Contains(t, result, "Package: server")
	assert.Contains(t, result, "Imports: 5 packages")
	assert.Contains(t, result, "Types:")
	assert.Contains(t, result, "Handler (struct) (lines 10-15)")
	assert.Contains(t, result, "Config (struct) (lines 20-25)")
	assert.Contains(t, result, "Functions:")
	assert.Contains(t, result, "NewHandler() (lines 30-45)")
	assert.Contains(t, result, "(Handler) ServeHTTP() (lines 50-75)")
}

func TestFormatter_FormatSymbols_EmptyData(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Empty data returns empty string
	data := &SymbolsData{}
	result := formatter.FormatSymbols(data, "go")
	assert.Empty(t, result)
}

func TestFormatter_FormatSymbols_NoImports(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: No imports section when count is zero
	data := &SymbolsData{
		PackageName:  "main",
		ImportsCount: 0,
		Types: []SymbolInfo{
			{Name: "User", Type: "struct", StartLine: 5, EndLine: 10},
		},
	}

	result := formatter.FormatSymbols(data, "go")

	assert.Contains(t, result, "Package: main")
	assert.NotContains(t, result, "Imports:")
	assert.Contains(t, result, "User (struct) (lines 5-10)")
}

func TestFormatter_FormatDefinitions_MultipleDefinitions(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Format multiple definitions with line comments
	data := &DefinitionsData{
		Definitions: []Definition{
			{
				Name:      "Handler",
				Type:      "type",
				Code:      "type Handler struct {\n    router *http.ServeMux\n    config *Config\n}",
				StartLine: 10,
				EndLine:   14,
			},
			{
				Name:      "NewHandler",
				Type:      "function",
				Code:      "func NewHandler(config *Config) (*Handler, error) { ... }",
				StartLine: 20,
				EndLine:   20,
			},
		},
	}

	result := formatter.FormatDefinitions(data, "go")

	require.NotEmpty(t, result)
	assert.Contains(t, result, "// (lines 10-14)")
	assert.Contains(t, result, "type Handler struct")
	assert.Contains(t, result, "// (line 20)")
	assert.Contains(t, result, "func NewHandler(config *Config)")

	// Check that definitions are separated
	parts := strings.Split(result, "\n\n")
	assert.GreaterOrEqual(t, len(parts), 2)
}

func TestFormatter_FormatData_ConstantsAndVariables(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Format constants and variables with line comments
	data := &DataData{
		Constants: []ConstantInfo{
			{Name: "DefaultPort", Value: "8080", Type: "int", StartLine: 5, EndLine: 5},
			{Name: "DefaultTimeout", Value: "30 * time.Second", Type: "time.Duration", StartLine: 6, EndLine: 6},
		},
		Variables: []VariableInfo{
			{Name: "DefaultConfig", Value: "Config{Port: DefaultPort}", Type: "Config", StartLine: 10, EndLine: 10},
		},
	}

	result := formatter.FormatData(data, "go")

	require.NotEmpty(t, result)
	assert.Contains(t, result, "// (line 5)")
	assert.Contains(t, result, "const DefaultPort int = 8080")
	assert.Contains(t, result, "// (line 6)")
	assert.Contains(t, result, "const DefaultTimeout time.Duration = 30 * time.Second")
	assert.Contains(t, result, "// (line 10)")
	assert.Contains(t, result, "var DefaultConfig Config = Config{Port: DefaultPort}")
}

func TestFormatter_FormatData_PythonConstants(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Python constant formatting
	data := &DataData{
		Constants: []ConstantInfo{
			{Name: "DEFAULT_PORT", Value: "8080", Type: "", StartLine: 5, EndLine: 5},
			{Name: "MAX_CONNECTIONS", Value: "100", Type: "", StartLine: 6, EndLine: 6},
		},
	}

	result := formatter.FormatData(data, "python")

	require.NotEmpty(t, result)
	assert.Contains(t, result, "DEFAULT_PORT = 8080")
	assert.Contains(t, result, "MAX_CONNECTIONS = 100")
	assert.NotContains(t, result, "const")
}

func TestFormatter_FormatData_TypeScriptConstants(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: TypeScript constant formatting
	data := &DataData{
		Constants: []ConstantInfo{
			{Name: "API_URL", Value: "'https://api.example.com'", Type: "", StartLine: 5, EndLine: 5},
		},
	}

	result := formatter.FormatData(data, "typescript")

	require.NotEmpty(t, result)
	assert.Contains(t, result, "const API_URL = 'https://api.example.com'")
}

func TestFormatter_FormatData_EmptyData(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Empty data returns empty string
	data := &DataData{}
	result := formatter.FormatData(data, "go")
	assert.Empty(t, result)
}

func TestFormatter_FormatDocumentation(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Documentation text is returned as-is
	chunk := &DocumentationChunk{
		FilePath:     "README.md",
		SectionIndex: 0,
		ChunkIndex:   0,
		Text:         "# Introduction\n\nThis is a documentation chunk with **markdown** formatting.",
		StartLine:    1,
		EndLine:      3,
	}

	result := formatter.FormatDocumentation(chunk)

	require.NotEmpty(t, result)
	assert.Equal(t, chunk.Text, result)
}

func TestFormatter_FormatSymbols_SingleLineRange(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Single line displays as "(line N)" not "(lines N-N)"
	data := &SymbolsData{
		Types: []SymbolInfo{
			{Name: "EmptyStruct", Type: "struct", StartLine: 42, EndLine: 42},
		},
	}

	result := formatter.FormatSymbols(data, "go")

	assert.Contains(t, result, "(line 42)")
	assert.NotContains(t, result, "(lines 42-42)")
}

func TestFormatter_FormatData_ConstantsOnly(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Only constants, no variables
	data := &DataData{
		Constants: []ConstantInfo{
			{Name: "MaxRetries", Value: "3", Type: "int", StartLine: 5, EndLine: 5},
		},
	}

	result := formatter.FormatData(data, "go")

	require.NotEmpty(t, result)
	assert.Contains(t, result, "const MaxRetries int = 3")
}

func TestFormatter_FormatData_VariablesOnly(t *testing.T) {
	t.Parallel()

	formatter := NewFormatter()

	// Test: Only variables, no constants
	data := &DataData{
		Variables: []VariableInfo{
			{Name: "globalCache", Value: "make(map[string]string)", Type: "map[string]string", StartLine: 10, EndLine: 10},
		},
	}

	result := formatter.FormatData(data, "go")

	require.NotEmpty(t, result)
	assert.Contains(t, result, "var globalCache map[string]string = make(map[string]string)")
}
