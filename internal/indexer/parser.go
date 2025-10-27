package indexer

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// simpleParser implements Parser using Go's ast package for Go files.
// This is a simplified implementation. A full implementation would use tree-sitter
// for all supported languages.
type simpleParser struct{}

// NewParser creates a new parser instance.
func NewParser() Parser {
	return &simpleParser{}
}

// ParseFile extracts code structure from a Go source file.
func (p *simpleParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
	language := detectLanguage(filePath)

	if language != "go" {
		// For now, only Go is supported
		// TODO: Add tree-sitter support for other languages
		return nil, nil
	}

	return p.parseGoFile(filePath)
}

// SupportsLanguage checks if this parser supports the given language.
func (p *simpleParser) SupportsLanguage(language string) bool {
	return language == "go"
}

// parseGoFile parses a Go source file using go/ast.
func (p *simpleParser) parseGoFile(filePath string) (*CodeExtraction, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	extraction := &CodeExtraction{
		Language: "go",
		FilePath: filePath,
		Symbols: &SymbolsData{
			PackageName: node.Name.Name,
			Types:       []SymbolInfo{},
			Functions:   []SymbolInfo{},
		},
		Definitions: &DefinitionsData{
			Definitions: []Definition{},
		},
		Data: &DataData{
			Constants: []ConstantInfo{},
			Variables: []VariableInfo{},
		},
	}

	// Count imports
	extraction.Symbols.ImportsCount = len(node.Imports)

	// Read file for getting source code snippets
	sourceBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	source := string(sourceBytes)
	lines := strings.Split(source, "\n")

	// Walk the AST
	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			p.processGenDecl(decl, fset, lines, extraction)
		case *ast.FuncDecl:
			p.processFuncDecl(decl, fset, lines, extraction)
		}
		return true
	})

	return extraction, nil
}

// processGenDecl processes general declarations (types, constants, variables).
func (p *simpleParser) processGenDecl(decl *ast.GenDecl, fset *token.FileSet, lines []string, extraction *CodeExtraction) {
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			p.processTypeSpec(s, decl, fset, lines, extraction)
		case *ast.ValueSpec:
			p.processValueSpec(s, decl, fset, lines, extraction)
		}
	}
}

// processTypeSpec processes type declarations.
func (p *simpleParser) processTypeSpec(spec *ast.TypeSpec, decl *ast.GenDecl, fset *token.FileSet, lines []string, extraction *CodeExtraction) {
	startLine := fset.Position(spec.Pos()).Line
	endLine := fset.Position(spec.End()).Line

	typeName := spec.Name.Name
	typeKind := "type"

	// Determine type kind
	switch spec.Type.(type) {
	case *ast.StructType:
		typeKind = "struct"
	case *ast.InterfaceType:
		typeKind = "interface"
	}

	// Add to symbols
	extraction.Symbols.Types = append(extraction.Symbols.Types, SymbolInfo{
		Name:      typeName,
		Type:      typeKind,
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions (extract source code)
	code := extractLines(lines, startLine, endLine)
	extraction.Definitions.Definitions = append(extraction.Definitions.Definitions, Definition{
		Name:      typeName,
		Type:      "type",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// processValueSpec processes constant and variable declarations.
func (p *simpleParser) processValueSpec(spec *ast.ValueSpec, decl *ast.GenDecl, fset *token.FileSet, lines []string, extraction *CodeExtraction) {
	startLine := fset.Position(spec.Pos()).Line
	endLine := fset.Position(spec.End()).Line

	for i, name := range spec.Names {
		varName := name.Name
		var value string
		if i < len(spec.Values) {
			value = extractLines(lines, fset.Position(spec.Values[i].Pos()).Line, fset.Position(spec.Values[i].End()).Line)
		}

		var typeName string
		if spec.Type != nil {
			typeName = extractLines(lines, fset.Position(spec.Type.Pos()).Line, fset.Position(spec.Type.End()).Line)
		}

		if decl.Tok == token.CONST {
			// Constant
			extraction.Data.Constants = append(extraction.Data.Constants, ConstantInfo{
				Name:      varName,
				Value:     value,
				Type:      typeName,
				StartLine: startLine,
				EndLine:   endLine,
			})
		} else if decl.Tok == token.VAR {
			// Variable
			extraction.Data.Variables = append(extraction.Data.Variables, VariableInfo{
				Name:      varName,
				Value:     value,
				Type:      typeName,
				StartLine: startLine,
				EndLine:   endLine,
			})
		}
	}
}

// processFuncDecl processes function declarations.
func (p *simpleParser) processFuncDecl(decl *ast.FuncDecl, fset *token.FileSet, lines []string, extraction *CodeExtraction) {
	startLine := fset.Position(decl.Pos()).Line
	endLine := fset.Position(decl.End()).Line

	funcName := decl.Name.Name
	signature := funcName + "()"

	// Build signature with receiver
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		// Method
		recv := decl.Recv.List[0]
		recvType := extractLines(lines, fset.Position(recv.Type.Pos()).Line, fset.Position(recv.Type.End()).Line)
		signature = "(" + strings.TrimSpace(recvType) + ") " + funcName + "()"
	}

	// Add to symbols
	extraction.Symbols.Functions = append(extraction.Symbols.Functions, SymbolInfo{
		Name:      funcName,
		Type:      "function",
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only, not body)
	// For simplicity, we'll extract just the function declaration line
	code := extractFunctionSignature(lines, startLine, endLine)
	extraction.Definitions.Definitions = append(extraction.Definitions.Definitions, Definition{
		Name:      funcName,
		Type:      "function",
		Code:      code,
		StartLine: startLine,
		EndLine:   startLine, // Just the signature line
	})
}

// extractLines extracts source code lines from startLine to endLine (1-indexed).
func extractLines(lines []string, startLine, endLine int) string {
	if startLine < 1 || endLine < 1 || startLine > len(lines) {
		return ""
	}

	// Adjust for 0-indexed array
	start := startLine - 1
	end := endLine
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start:end], "\n")
}

// extractFunctionSignature extracts just the function signature (up to opening brace).
func extractFunctionSignature(lines []string, startLine, endLine int) string {
	if startLine < 1 || startLine > len(lines) {
		return ""
	}

	// Find the opening brace
	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, "{") {
			// Extract up to but not including the brace
			parts := strings.Split(line, "{")
			if i == startLine-1 {
				return strings.TrimSpace(parts[0]) + " { ... }"
			}
			// Multi-line signature
			sig := strings.Join(lines[startLine-1:i], "\n")
			sig += "\n" + strings.TrimSpace(parts[0]) + " { ... }"
			return sig
		}
	}

	// No brace found, return the line
	return lines[startLine-1]
}

// detectLanguage detects the programming language based on file extension.
func detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".php":
		return "php"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	default:
		return "unknown"
	}
}
