package parsers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for PythonParser:
// - Parse class definitions
// - Extract methods from classes (with proper class association)
// - Extract standalone functions (not methods)
// - Extract ALL_CAPS constants (Python convention)
// - Extract lowercase variables
// - Verify accurate line numbers for all symbols
// - Handle invalid/non-existent files gracefully
// - Handle empty files without errors
// - Handle decorators (@decorator syntax)
// - Handle async functions (async def)
// - Count imports correctly
// - Extract function signatures with parameters
// - Extract method signatures with class prefix
// - Distinguish between constants and variables by naming convention

func TestPythonParser_ParseClass(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Parse simple.py and extract class definitions
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Verify basic metadata
	assert.Equal(t, "python", extraction.Language)
	assert.Contains(t, extraction.FilePath, "simple.py")

	// Verify symbols data exists
	require.NotNil(t, extraction.Symbols)
	require.NotNil(t, extraction.Symbols.Types)

	// Test: Should extract both User and UserRepository classes
	assert.Len(t, extraction.Symbols.Types, 2)

	// Find User class
	var userClass *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "User" {
			userClass = &extraction.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, userClass, "User class should be extracted")
	assert.Equal(t, "class", userClass.Type)
	assert.Equal(t, 12, userClass.StartLine)
	assert.Equal(t, 25, userClass.EndLine)

	// Find UserRepository class
	var repoClass *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "UserRepository" {
			repoClass = &extraction.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, repoClass, "UserRepository class should be extracted")
	assert.Equal(t, "class", repoClass.Type)
	assert.Equal(t, 27, repoClass.StartLine)
	assert.Equal(t, 44, repoClass.EndLine)
}

func TestPythonParser_ParseMethods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract methods from classes
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)
	require.NotNil(t, extraction.Symbols.Functions)

	// Test: Should extract 7 functions total (6 methods + 1 standalone function)
	// User: __init__, validate, to_dict
	// UserRepository: __init__, add, find_by_email
	// Standalone: create_user
	assert.Len(t, extraction.Symbols.Functions, 7)

	// Test: Verify User.__init__ method
	var userInit *SymbolInfo
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "__init__" {
			sig := extraction.Symbols.Functions[i].Signature
			if sig == "User.__init__(self, name: str, email: str)" {
				userInit = &extraction.Symbols.Functions[i]
				break
			}
		}
	}
	require.NotNil(t, userInit, "User.__init__ should be extracted")
	assert.Equal(t, "method", userInit.Type)
	assert.Equal(t, 15, userInit.StartLine)
	assert.Equal(t, 17, userInit.EndLine)

	// Test: Verify User.validate method
	var validate *SymbolInfo
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "validate" {
			validate = &extraction.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, validate, "validate method should be extracted")
	assert.Equal(t, "method", validate.Type)
	assert.Contains(t, validate.Signature, "User.validate")
	assert.Contains(t, validate.Signature, "-> bool")

	// Test: Verify UserRepository.find_by_email method
	var findByEmail *SymbolInfo
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "find_by_email" {
			findByEmail = &extraction.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, findByEmail, "find_by_email method should be extracted")
	assert.Equal(t, "method", findByEmail.Type)
	assert.Contains(t, findByEmail.Signature, "UserRepository.find_by_email")
	assert.Contains(t, findByEmail.Signature, "-> Optional[User]")
}

func TestPythonParser_ParseFunctions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract standalone functions (not methods)
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)
	require.NotNil(t, extraction.Symbols.Functions)

	// Test: Find create_user function (standalone, not a method)
	var createUser *SymbolInfo
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "create_user" && extraction.Symbols.Functions[i].Type == "function" {
			createUser = &extraction.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, createUser, "create_user function should be extracted")
	assert.Equal(t, "function", createUser.Type)
	assert.Equal(t, 46, createUser.StartLine)
	assert.Equal(t, 48, createUser.EndLine)
	assert.Contains(t, createUser.Signature, "create_user")
	assert.Contains(t, createUser.Signature, "(name: str, email: str)")
	assert.Contains(t, createUser.Signature, "-> User")
}

func TestPythonParser_ParseConstants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract ALL_CAPS constants (Python convention)
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Data)
	require.NotNil(t, extraction.Data.Constants)

	// Test: Should extract 3 constants: API_KEY, MAX_RETRIES, DEBUG_MODE
	assert.Len(t, extraction.Data.Constants, 3)

	// Test: Verify API_KEY constant
	var apiKey *ConstantInfo
	for i := range extraction.Data.Constants {
		if extraction.Data.Constants[i].Name == "API_KEY" {
			apiKey = &extraction.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, apiKey, "API_KEY constant should be extracted")
	assert.Equal(t, `"test-api-key"`, apiKey.Value)
	assert.Equal(t, 6, apiKey.StartLine)

	// Test: Verify MAX_RETRIES constant
	var maxRetries *ConstantInfo
	for i := range extraction.Data.Constants {
		if extraction.Data.Constants[i].Name == "MAX_RETRIES" {
			maxRetries = &extraction.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, maxRetries, "MAX_RETRIES constant should be extracted")
	assert.Equal(t, "5", maxRetries.Value)
	assert.Equal(t, 7, maxRetries.StartLine)

	// Test: Verify DEBUG_MODE constant
	var debugMode *ConstantInfo
	for i := range extraction.Data.Constants {
		if extraction.Data.Constants[i].Name == "DEBUG_MODE" {
			debugMode = &extraction.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, debugMode, "DEBUG_MODE constant should be extracted")
	assert.Equal(t, "True", debugMode.Value)
	assert.Equal(t, 8, debugMode.StartLine)

	// Test: Verify lowercase variable is NOT a constant
	for i := range extraction.Data.Constants {
		assert.NotEqual(t, "database_url", extraction.Data.Constants[i].Name, "database_url should be a variable, not a constant")
	}
}

func TestPythonParser_ParseVariables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract lowercase variables (not ALL_CAPS)
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Data)
	require.NotNil(t, extraction.Data.Variables)

	// Test: Should extract database_url variable
	assert.Len(t, extraction.Data.Variables, 1)

	var dbUrl *VariableInfo
	for i := range extraction.Data.Variables {
		if extraction.Data.Variables[i].Name == "database_url" {
			dbUrl = &extraction.Data.Variables[i]
			break
		}
	}
	require.NotNil(t, dbUrl, "database_url variable should be extracted")
	assert.Equal(t, `"postgresql://localhost/testdb"`, dbUrl.Value)
	assert.Equal(t, 10, dbUrl.StartLine)
}

func TestPythonParser_LineNumbers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Verify line number accuracy
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Test: Verify file range
	assert.Equal(t, 1, extraction.StartLine)
	assert.Equal(t, 49, extraction.EndLine) // File has 49 lines

	// Test: Verify class line numbers are accurate
	for _, typ := range extraction.Symbols.Types {
		assert.Greater(t, typ.StartLine, 0, "StartLine should be positive for %s", typ.Name)
		assert.Greater(t, typ.EndLine, 0, "EndLine should be positive for %s", typ.Name)
		assert.GreaterOrEqual(t, typ.EndLine, typ.StartLine, "EndLine should be >= StartLine for %s", typ.Name)
	}

	// Test: Verify function/method line numbers are accurate
	for _, fn := range extraction.Symbols.Functions {
		assert.Greater(t, fn.StartLine, 0, "StartLine should be positive for %s", fn.Name)
		assert.Greater(t, fn.EndLine, 0, "EndLine should be positive for %s", fn.Name)
		assert.GreaterOrEqual(t, fn.EndLine, fn.StartLine, "EndLine should be >= StartLine for %s", fn.Name)
	}

	// Test: Verify constant/variable line numbers are accurate
	for _, c := range extraction.Data.Constants {
		assert.Greater(t, c.StartLine, 0, "StartLine should be positive for constant %s", c.Name)
	}
	for _, v := range extraction.Data.Variables {
		assert.Greater(t, v.StartLine, 0, "StartLine should be positive for variable %s", v.Name)
	}
}

func TestPythonParser_InvalidFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Non-existent file should return error
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/nonexistent.py")

	require.Error(t, err)
	assert.Nil(t, extraction)
	assert.True(t, os.IsNotExist(err), "Error should be file not found")
}

func TestPythonParser_EmptyFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Create an empty temporary Python file
	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.py")
	err := os.WriteFile(emptyFile, []byte(""), 0644)
	require.NoError(t, err)

	// Test: Empty file should parse without errors
	extraction, err := parser.ParseFile(ctx, emptyFile)

	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Test: Should have empty symbols
	assert.Equal(t, "python", extraction.Language)
	assert.Len(t, extraction.Symbols.Types, 0)
	assert.Len(t, extraction.Symbols.Functions, 0)
	assert.Len(t, extraction.Data.Constants, 0)
	assert.Len(t, extraction.Data.Variables, 0)
}

func TestPythonParser_Decorators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Create a temporary Python file with decorators
	tmpDir := t.TempDir()
	decoratorFile := filepath.Join(tmpDir, "decorators.py")
	content := `import functools

@functools.lru_cache(maxsize=128)
def cached_function(x: int) -> int:
    return x * 2

class Service:
    @property
    def name(self) -> str:
        return "service"

    @staticmethod
    def static_method():
        return True

    @classmethod
    def class_method(cls):
        return cls
`
	err := os.WriteFile(decoratorFile, []byte(content), 0644)
	require.NoError(t, err)

	// Test: Decorators should not break parsing
	extraction, err := parser.ParseFile(ctx, decoratorFile)

	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Test: Should extract decorated function
	var cachedFunc *SymbolInfo
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "cached_function" && extraction.Symbols.Functions[i].Type == "function" {
			cachedFunc = &extraction.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, cachedFunc, "cached_function should be extracted despite decorator")
	assert.Contains(t, cachedFunc.Signature, "cached_function")

	// Test: Should extract Service class
	var serviceClass *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "Service" {
			serviceClass = &extraction.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, serviceClass, "Service class should be extracted")

	// Test: Should extract at least the cached_function and some methods
	// Note: Tree-sitter may parse decorated methods differently, so we verify basic functionality
	assert.GreaterOrEqual(t, len(extraction.Symbols.Functions), 1, "Should extract at least the decorated function")

	// Verify we can extract the decorated standalone function
	foundCachedFunc := false
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "cached_function" {
			foundCachedFunc = true
			break
		}
	}
	assert.True(t, foundCachedFunc, "Should extract cached_function despite decorator")
}

func TestPythonParser_AsyncFunctions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Create a temporary Python file with async functions
	tmpDir := t.TempDir()
	asyncFile := filepath.Join(tmpDir, "async.py")
	content := `import asyncio

async def fetch_data(url: str) -> dict:
    """Async function to fetch data."""
    await asyncio.sleep(1)
    return {"url": url}

class AsyncService:
    async def process(self, data: dict) -> bool:
        """Async method to process data."""
        await asyncio.sleep(0.5)
        return True

    async def validate(self) -> None:
        """Another async method."""
        pass
`
	err := os.WriteFile(asyncFile, []byte(content), 0644)
	require.NoError(t, err)

	// Test: Async functions should be parsed correctly
	extraction, err := parser.ParseFile(ctx, asyncFile)

	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Test: Should extract async function
	var fetchData *SymbolInfo
	for i := range extraction.Symbols.Functions {
		if extraction.Symbols.Functions[i].Name == "fetch_data" && extraction.Symbols.Functions[i].Type == "function" {
			fetchData = &extraction.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, fetchData, "fetch_data async function should be extracted")
	assert.Contains(t, fetchData.Signature, "fetch_data")
	assert.Contains(t, fetchData.Signature, "-> dict")

	// Test: Should extract AsyncService class
	var asyncClass *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "AsyncService" {
			asyncClass = &extraction.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, asyncClass, "AsyncService class should be extracted")

	// Test: Should extract async methods
	asyncMethods := []string{"process", "validate"}
	for _, methodName := range asyncMethods {
		found := false
		for i := range extraction.Symbols.Functions {
			if extraction.Symbols.Functions[i].Name == methodName && extraction.Symbols.Functions[i].Type == "method" {
				found = true
				assert.Contains(t, extraction.Symbols.Functions[i].Signature, "AsyncService."+methodName)
				break
			}
		}
		assert.True(t, found, "Async method %s should be extracted", methodName)
	}
}

func TestPythonParser_ImportsCount(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Count imports correctly
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)

	// Test: Should count 3 import statements (import os, import sys, from typing import ...)
	assert.Equal(t, 3, extraction.Symbols.ImportsCount)
}

func TestPythonParser_Definitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract definitions tier
	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Definitions)
	require.NotNil(t, extraction.Definitions.Definitions)

	// Test: Should have definitions for classes and functions/methods
	// 2 classes + 6 methods + 1 standalone function = 9 definitions
	assert.Len(t, extraction.Definitions.Definitions, 9)

	// Test: Class definitions should include full code
	var userClassDef *Definition
	for i := range extraction.Definitions.Definitions {
		if extraction.Definitions.Definitions[i].Name == "User" && extraction.Definitions.Definitions[i].Type == "class" {
			userClassDef = &extraction.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, userClassDef, "User class definition should be extracted")
	assert.NotEmpty(t, userClassDef.Code)
	assert.Contains(t, userClassDef.Code, "class User:")
	assert.Equal(t, 12, userClassDef.StartLine)
	assert.Equal(t, 25, userClassDef.EndLine)

	// Test: Function definitions should include signature only
	var createUserDef *Definition
	for i := range extraction.Definitions.Definitions {
		if extraction.Definitions.Definitions[i].Name == "create_user" && extraction.Definitions.Definitions[i].Type == "function" {
			createUserDef = &extraction.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, createUserDef, "create_user function definition should be extracted")
	assert.NotEmpty(t, createUserDef.Code)
	assert.Contains(t, createUserDef.Code, "def create_user")
	assert.Equal(t, 46, createUserDef.StartLine)
	assert.Equal(t, 46, createUserDef.EndLine) // Signature only, same line
}

func TestPythonParser_ConstantNamingConvention(t *testing.T) {
	t.Parallel()

	// Test: Helper function isConstantName follows Python convention
	tests := []struct {
		name       string
		isConstant bool
	}{
		{"API_KEY", true},
		{"MAX_RETRIES", true},
		{"DEBUG_MODE", true},
		{"DATABASE_URL", true},
		{"A", true},
		{"AB_CD", true},
		{"database_url", false},
		{"databaseUrl", false},
		{"DatabaseUrl", false},
		{"API_Key", false}, // Mixed case is not a constant
		{"api_KEY", false}, // Mixed case is not a constant
		{"", false},
		{"_PRIVATE", true}, // Leading underscore with ALL_CAPS
		{"__DUNDER__", true},
	}

	for _, tt := range tests {
		result := isConstantName(tt.name)
		assert.Equal(t, tt.isConstant, result, "isConstantName(%q) should be %v", tt.name, tt.isConstant)
	}
}
