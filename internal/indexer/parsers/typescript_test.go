package parsers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for TypeScript/JavaScript Parsers:
// - Parse TypeScript classes with methods
// - Parse TypeScript interfaces
// - Parse TypeScript type aliases
// - Parse TypeScript functions
// - Parse TypeScript constants (const declarations)
// - Parse TypeScript variables (let/var declarations)
// - Count import statements accurately
// - Verify accurate line numbers for all symbols
// - Handle invalid/unparseable files gracefully
// - Parse JavaScript classes
// - Parse JavaScript functions
// - Parse JavaScript constants
// - Handle empty files
// - Ensure Language field is set correctly

func TestTypeScriptParser_ParseClass(t *testing.T) {
	t.Parallel()

	// Test: Extract class with methods and verify symbols and definitions
	parser := NewTypeScriptParser()

	tsPath := filepath.Join("../../../testdata/code/typescript/simple.ts")
	extraction, err := parser.ParseFile(context.Background(), tsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Verify basic metadata
	assert.Equal(t, "typescript", extraction.Language)
	assert.Equal(t, tsPath, extraction.FilePath)

	// Verify class symbol
	var classSymbol *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "UserService" {
			classSymbol = &extraction.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, classSymbol, "UserService class not found in symbols")
	assert.Equal(t, "class", classSymbol.Type)
	assert.Equal(t, 17, classSymbol.StartLine)
	assert.Equal(t, 29, classSymbol.EndLine)

	// Verify class definition
	var classDef *Definition
	for i := range extraction.Definitions.Definitions {
		if extraction.Definitions.Definitions[i].Name == "UserService" {
			classDef = &extraction.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, classDef, "UserService class not found in definitions")
	assert.Equal(t, "class", classDef.Type)
	assert.Contains(t, classDef.Code, "class UserService")
	assert.Contains(t, classDef.Code, "private users: User[]")
	assert.Equal(t, 17, classDef.StartLine)
	assert.Equal(t, 29, classDef.EndLine)
}

func TestTypeScriptParser_ParseInterface(t *testing.T) {
	t.Parallel()

	// Test: Extract interface definition with accurate line numbers
	parser := NewTypeScriptParser()

	tsPath := filepath.Join("../../../testdata/code/typescript/simple.ts")
	extraction, err := parser.ParseFile(context.Background(), tsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Find User interface
	var userInterface *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "User" {
			userInterface = &extraction.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, userInterface, "User interface not found")
	assert.Equal(t, "interface", userInterface.Type)
	assert.Equal(t, 11, userInterface.StartLine)
	assert.Equal(t, 15, userInterface.EndLine)

	// Verify interface definition
	var userDef *Definition
	for i := range extraction.Definitions.Definitions {
		if extraction.Definitions.Definitions[i].Name == "User" && extraction.Definitions.Definitions[i].Type == "interface" {
			userDef = &extraction.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, userDef, "User interface not found in definitions")
	assert.Contains(t, userDef.Code, "interface User")
	assert.Contains(t, userDef.Code, "id: UserId")
	assert.Contains(t, userDef.Code, "name: string")
	assert.Contains(t, userDef.Code, "email: string")
}

func TestTypeScriptParser_ParseTypeAlias(t *testing.T) {
	t.Parallel()

	// Test: Extract type alias with correct symbol type
	parser := NewTypeScriptParser()

	tsPath := filepath.Join("../../../testdata/code/typescript/simple.ts")
	extraction, err := parser.ParseFile(context.Background(), tsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Find UserId type alias
	var userIdType *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "UserId" {
			userIdType = &extraction.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, userIdType, "UserId type alias not found")
	assert.Equal(t, "type", userIdType.Type)
	assert.Equal(t, 9, userIdType.StartLine)
	assert.Equal(t, 9, userIdType.EndLine)

	// Verify type definition
	var userIdDef *Definition
	for i := range extraction.Definitions.Definitions {
		if extraction.Definitions.Definitions[i].Name == "UserId" {
			userIdDef = &extraction.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, userIdDef, "UserId type not found in definitions")
	assert.Contains(t, userIdDef.Code, "type UserId = string")
}

func TestTypeScriptParser_ParseFunctions(t *testing.T) {
	t.Parallel()

	// Test: Extract function declarations with signatures
	parser := NewTypeScriptParser()

	tsPath := filepath.Join("../../../testdata/code/typescript/simple.ts")
	extraction, err := parser.ParseFile(context.Background(), tsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Find validateEmail function
	var validateEmailFunc *SymbolInfo
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "validateEmail" {
			validateEmailFunc = &extraction.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, validateEmailFunc, "validateEmail function not found")
	assert.Equal(t, "function", validateEmailFunc.Type)
	assert.Equal(t, 31, validateEmailFunc.StartLine)
	assert.Equal(t, 33, validateEmailFunc.EndLine)
	assert.NotEmpty(t, validateEmailFunc.Signature, "function signature should not be empty")
	assert.Contains(t, validateEmailFunc.Signature, "validateEmail")

	// Verify function definition (should be signature only)
	var funcDef *Definition
	for i := range extraction.Definitions.Definitions {
		if extraction.Definitions.Definitions[i].Name == "validateEmail" {
			funcDef = &extraction.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, funcDef, "validateEmail function not found in definitions")
	assert.Equal(t, "function", funcDef.Type)
	assert.Contains(t, funcDef.Code, "validateEmail")
	assert.Contains(t, funcDef.Code, "...")
}

func TestTypeScriptParser_ParseConstants(t *testing.T) {
	t.Parallel()

	// Test: Extract const declarations with values
	parser := NewTypeScriptParser()

	tsPath := filepath.Join("../../../testdata/code/typescript/simple.ts")
	extraction, err := parser.ParseFile(context.Background(), tsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Should have at least two constants
	require.GreaterOrEqual(t, len(extraction.Data.Constants), 2, "expected at least 2 constants")

	// Find API_KEY constant
	var apiKeyConst *ConstantInfo
	for i := range extraction.Data.Constants {
		if extraction.Data.Constants[i].Name == "API_KEY" {
			apiKeyConst = &extraction.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, apiKeyConst, "API_KEY constant not found")
	assert.Equal(t, `"test-key-123"`, apiKeyConst.Value)
	assert.Equal(t, 4, apiKeyConst.StartLine)

	// Find MAX_RETRIES constant
	var maxRetriesConst *ConstantInfo
	for i := range extraction.Data.Constants {
		if extraction.Data.Constants[i].Name == "MAX_RETRIES" {
			maxRetriesConst = &extraction.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, maxRetriesConst, "MAX_RETRIES constant not found")
	assert.Equal(t, "3", maxRetriesConst.Value)
	assert.Equal(t, 5, maxRetriesConst.StartLine)
}

func TestTypeScriptParser_ParseVariables(t *testing.T) {
	t.Parallel()

	// Test: Extract let/var declarations as variables
	parser := NewTypeScriptParser()

	tsPath := filepath.Join("../../../testdata/code/typescript/simple.ts")
	extraction, err := parser.ParseFile(context.Background(), tsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Find globalCounter variable
	var globalCounterVar *VariableInfo
	for i := range extraction.Data.Variables {
		if extraction.Data.Variables[i].Name == "globalCounter" {
			globalCounterVar = &extraction.Data.Variables[i]
			break
		}
	}
	require.NotNil(t, globalCounterVar, "globalCounter variable not found")
	assert.Equal(t, "0", globalCounterVar.Value)
	assert.Equal(t, 7, globalCounterVar.StartLine)
}

func TestTypeScriptParser_ImportsCount(t *testing.T) {
	t.Parallel()

	// Test: Count import statements accurately
	parser := NewTypeScriptParser()

	tsPath := filepath.Join("../../../testdata/code/typescript/simple.ts")
	extraction, err := parser.ParseFile(context.Background(), tsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// simple.ts has 2 import statements
	assert.Equal(t, 2, extraction.Symbols.ImportsCount)
}

func TestTypeScriptParser_LineNumbers(t *testing.T) {
	t.Parallel()

	// Test: Verify all symbols have accurate line numbers matching source
	parser := NewTypeScriptParser()

	tsPath := filepath.Join("../../../testdata/code/typescript/simple.ts")
	extraction, err := parser.ParseFile(context.Background(), tsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// All symbols should have valid line numbers
	for _, symbol := range extraction.Symbols.Types {
		assert.Greater(t, symbol.StartLine, 0, "symbol %s has invalid start line", symbol.Name)
		assert.GreaterOrEqual(t, symbol.EndLine, symbol.StartLine, "symbol %s has invalid end line", symbol.Name)
	}

	for _, symbol := range extraction.Symbols.Functions {
		assert.Greater(t, symbol.StartLine, 0, "function %s has invalid start line", symbol.Name)
		assert.GreaterOrEqual(t, symbol.EndLine, symbol.StartLine, "function %s has invalid end line", symbol.Name)
	}

	// File should have proper bounds
	assert.Equal(t, 1, extraction.StartLine)
	assert.Greater(t, extraction.EndLine, extraction.StartLine)
}

func TestTypeScriptParser_InvalidFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewTypeScriptParser()

	// Test: Handle parse errors gracefully for unparseable files
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "invalid.ts")
	err := os.WriteFile(invalidFile, []byte("class {{{{{"), 0644)
	require.NoError(t, err)

	extraction, err := parser.ParseFile(ctx, invalidFile)

	// Tree-sitter creates a best-effort parse tree even for invalid syntax
	// So we expect an extraction, not nil
	assert.NoError(t, err)
	assert.NotNil(t, extraction, "tree-sitter returns partial parse for invalid syntax")
	assert.Equal(t, "typescript", extraction.Language)
}

func TestTypeScriptParser_EmptyFile(t *testing.T) {
	t.Parallel()

	// Test: Handle empty files without errors
	parser := NewTypeScriptParser()

	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.ts")
	err := os.WriteFile(emptyFile, []byte(""), 0644)
	require.NoError(t, err)

	extraction, err := parser.ParseFile(context.Background(), emptyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Should have initialized empty slices
	assert.Equal(t, 0, len(extraction.Symbols.Types))
	assert.Equal(t, 0, len(extraction.Symbols.Functions))
	assert.Equal(t, 0, extraction.Symbols.ImportsCount)
	assert.Equal(t, 0, len(extraction.Data.Constants))
	assert.Equal(t, 0, len(extraction.Data.Variables))
}

func TestTypeScriptParser_NonexistentFile(t *testing.T) {
	t.Parallel()

	// Test: Return error for nonexistent files
	parser := NewTypeScriptParser()

	extraction, err := parser.ParseFile(context.Background(), "/nonexistent/file.ts")
	assert.Error(t, err)
	assert.Nil(t, extraction)
}

func TestJavaScriptParser_ParseClass(t *testing.T) {
	t.Parallel()

	// Test: Extract JavaScript class with constructor and methods
	parser := NewJavaScriptParser()

	jsPath := filepath.Join("../../../testdata/code/javascript/simple.js")
	extraction, err := parser.ParseFile(context.Background(), jsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Verify language is set to javascript
	assert.Equal(t, "javascript", extraction.Language)
	assert.Equal(t, jsPath, extraction.FilePath)

	// Verify ConnectionPool class
	var classSymbol *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "ConnectionPool" {
			classSymbol = &extraction.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, classSymbol, "ConnectionPool class not found")
	assert.Equal(t, "class", classSymbol.Type)
	assert.Equal(t, 6, classSymbol.StartLine)
	assert.Equal(t, 27, classSymbol.EndLine)

	// Verify class definition
	var classDef *Definition
	for i := range extraction.Definitions.Definitions {
		if extraction.Definitions.Definitions[i].Name == "ConnectionPool" {
			classDef = &extraction.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, classDef, "ConnectionPool class not found in definitions")
	assert.Contains(t, classDef.Code, "class ConnectionPool")
	assert.Contains(t, classDef.Code, "constructor")
	assert.Contains(t, classDef.Code, "acquire")
	assert.Contains(t, classDef.Code, "release")
}

func TestJavaScriptParser_ParseFunctions(t *testing.T) {
	t.Parallel()

	// Test: Extract JavaScript function declarations
	parser := NewJavaScriptParser()

	jsPath := filepath.Join("../../../testdata/code/javascript/simple.js")
	extraction, err := parser.ParseFile(context.Background(), jsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Find createClient function
	var createClientFunc *SymbolInfo
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "createClient" {
			createClientFunc = &extraction.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, createClientFunc, "createClient function not found")
	assert.Equal(t, "function", createClientFunc.Type)
	assert.Equal(t, 29, createClientFunc.StartLine)
	assert.Equal(t, 31, createClientFunc.EndLine)

	// Verify function definition
	var funcDef *Definition
	for i := range extraction.Definitions.Definitions {
		if extraction.Definitions.Definitions[i].Name == "createClient" {
			funcDef = &extraction.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, funcDef, "createClient function not found in definitions")
	assert.Contains(t, funcDef.Code, "createClient")
}

func TestJavaScriptParser_ParseConstants(t *testing.T) {
	t.Parallel()

	// Test: Extract JavaScript const declarations with values
	parser := NewJavaScriptParser()

	jsPath := filepath.Join("../../../testdata/code/javascript/simple.js")
	extraction, err := parser.ParseFile(context.Background(), jsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Should have at least two constants
	require.GreaterOrEqual(t, len(extraction.Data.Constants), 2, "expected at least 2 constants")

	// Find API_URL constant
	var apiURLConst *ConstantInfo
	for i := range extraction.Data.Constants {
		if extraction.Data.Constants[i].Name == "API_URL" {
			apiURLConst = &extraction.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, apiURLConst, "API_URL constant not found")
	assert.Equal(t, `"https://api.example.com"`, apiURLConst.Value)
	assert.Equal(t, 1, apiURLConst.StartLine)

	// Find MAX_CONNECTIONS constant
	var maxConnConst *ConstantInfo
	for i := range extraction.Data.Constants {
		if extraction.Data.Constants[i].Name == "MAX_CONNECTIONS" {
			maxConnConst = &extraction.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, maxConnConst, "MAX_CONNECTIONS constant not found")
	assert.Equal(t, "10", maxConnConst.Value)
	assert.Equal(t, 2, maxConnConst.StartLine)
}

func TestJavaScriptParser_ParseVariables(t *testing.T) {
	t.Parallel()

	// Test: Extract JavaScript let/var declarations
	parser := NewJavaScriptParser()

	jsPath := filepath.Join("../../../testdata/code/javascript/simple.js")
	extraction, err := parser.ParseFile(context.Background(), jsPath)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Find currentConnections variable
	var currentConnVar *VariableInfo
	for i := range extraction.Data.Variables {
		if extraction.Data.Variables[i].Name == "currentConnections" {
			currentConnVar = &extraction.Data.Variables[i]
			break
		}
	}
	require.NotNil(t, currentConnVar, "currentConnections variable not found")
	assert.Equal(t, "0", currentConnVar.Value)
	assert.Equal(t, 4, currentConnVar.StartLine)
}

func TestJavaScriptParser_EmptyFile(t *testing.T) {
	t.Parallel()

	// Test: Handle empty JavaScript files
	parser := NewJavaScriptParser()

	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.js")
	err := os.WriteFile(emptyFile, []byte(""), 0644)
	require.NoError(t, err)

	extraction, err := parser.ParseFile(context.Background(), emptyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	assert.Equal(t, "javascript", extraction.Language)
	assert.Equal(t, 0, len(extraction.Symbols.Types))
	assert.Equal(t, 0, len(extraction.Symbols.Functions))
}

func TestJavaScriptParser_InvalidFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewJavaScriptParser()

	// Test: Handle invalid JavaScript syntax gracefully
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "invalid.js")
	err := os.WriteFile(invalidFile, []byte("function {{{{"), 0644)
	require.NoError(t, err)

	extraction, err := parser.ParseFile(ctx, invalidFile)

	// Tree-sitter creates a best-effort parse tree even for invalid syntax
	assert.NoError(t, err)
	assert.NotNil(t, extraction, "tree-sitter returns partial parse for invalid syntax")
	assert.Equal(t, "javascript", extraction.Language)
}
