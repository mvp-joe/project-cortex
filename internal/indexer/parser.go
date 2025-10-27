package indexer

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
	"github.com/mvp-joe/project-cortex/internal/indexer/parsers"
)

// multiLanguageParser implements Parser using Go's ast for Go files and tree-sitter for others.
type multiLanguageParser struct {
	// Tree-sitter parsers (using interface types to avoid exposing internal types)
	tsParser   languageParser
	jsParser   languageParser
	pyParser   languageParser
	rsParser   languageParser
	cParser    languageParser
	javaParser languageParser
	phpParser  languageParser
	rubyParser languageParser
}

// languageParser is an internal interface for language-specific parsers.
type languageParser interface {
	ParseFile(ctx context.Context, filePath string) (*parsers.CodeExtraction, error)
}

// NewParser creates a new parser instance that supports all languages.
func NewParser() Parser {
	return &multiLanguageParser{
		tsParser:   parsers.NewTypeScriptParser(),
		jsParser:   parsers.NewJavaScriptParser(),
		pyParser:   parsers.NewPythonParser(),
		rsParser:   parsers.NewRustParser(),
		cParser:    parsers.NewCParser(),
		javaParser: parsers.NewJavaParser(),
		phpParser:  parsers.NewPhpParser(),
		rubyParser: parsers.NewRubyParser(),
	}
}

// ParseFile extracts code structure from a source file.
func (p *multiLanguageParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
	language := detectLanguage(filePath)

	// Route to appropriate parser based on language
	var result *parsers.CodeExtraction
	var err error

	switch language {
	case "go":
		return p.parseGoFile(filePath)
	case "typescript":
		result, err = p.tsParser.ParseFile(ctx, filePath)
	case "javascript":
		result, err = p.jsParser.ParseFile(ctx, filePath)
	case "python":
		result, err = p.pyParser.ParseFile(ctx, filePath)
	case "rust":
		result, err = p.rsParser.ParseFile(ctx, filePath)
	case "c", "cpp":
		result, err = p.cParser.ParseFile(ctx, filePath)
	case "java":
		result, err = p.javaParser.ParseFile(ctx, filePath)
	case "php":
		result, err = p.phpParser.ParseFile(ctx, filePath)
	case "ruby":
		result, err = p.rubyParser.ParseFile(ctx, filePath)
	default:
		// Unsupported language
		return nil, nil
	}

	if err != nil || result == nil {
		return nil, err
	}

	// Convert parsers.CodeExtraction to indexer.CodeExtraction
	return convertCodeExtraction(result), nil
}

// convertCodeExtraction converts parsers.CodeExtraction to indexer.CodeExtraction.
// Since both now use the same extraction.* types, we can directly reuse the pointers.
func convertCodeExtraction(src *parsers.CodeExtraction) *CodeExtraction {
	if src == nil {
		return nil
	}

	return &CodeExtraction{
		Language:    src.Language,
		FilePath:    src.FilePath,
		StartLine:   src.StartLine,
		EndLine:     src.EndLine,
		Symbols:     src.Symbols,     // Direct reuse
		Definitions: src.Definitions, // Direct reuse
		Data:        src.Data,        // Direct reuse
	}
}

// SupportsLanguage checks if this parser supports the given language.
func (p *multiLanguageParser) SupportsLanguage(language string) bool {
	switch language {
	case "go", "typescript", "javascript", "python", "rust", "c", "cpp", "java", "php", "ruby":
		return true
	default:
		return false
	}
}

// parseGoFile parses a Go source file using go/ast.
func (p *multiLanguageParser) parseGoFile(filePath string) (*CodeExtraction, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	codeExtraction := &CodeExtraction{
		Language: "go",
		FilePath: filePath,
		Symbols: &extraction.SymbolsData{
			PackageName: node.Name.Name,
			Types:       []extraction.SymbolInfo{},
			Functions:   []extraction.SymbolInfo{},
		},
		Definitions: &extraction.DefinitionsData{
			Definitions: []extraction.Definition{},
		},
		Data: &extraction.DataData{
			Constants: []extraction.ConstantInfo{},
			Variables: []extraction.VariableInfo{},
		},
	}

	// Count imports
	codeExtraction.Symbols.ImportsCount = len(node.Imports)

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
			p.processGenDecl(decl, fset, lines, codeExtraction)
		case *ast.FuncDecl:
			p.processFuncDecl(decl, fset, lines, codeExtraction)
		}
		return true
	})

	return codeExtraction, nil
}

// processGenDecl processes general declarations (types, constants, variables).
func (p *multiLanguageParser) processGenDecl(decl *ast.GenDecl, fset *token.FileSet, lines []string, codeExtraction *CodeExtraction) {
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			p.processTypeSpec(s, decl, fset, lines, codeExtraction)
		case *ast.ValueSpec:
			p.processValueSpec(s, decl, fset, lines, codeExtraction)
		}
	}
}

// processTypeSpec processes type declarations.
func (p *multiLanguageParser) processTypeSpec(spec *ast.TypeSpec, decl *ast.GenDecl, fset *token.FileSet, lines []string, codeExtraction *CodeExtraction) {
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
	codeExtraction.Symbols.Types = append(codeExtraction.Symbols.Types, extraction.SymbolInfo{
		Name:      typeName,
		Type:      typeKind,
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions (extract source code)
	code := extractLines(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      typeName,
		Type:      "type",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// processValueSpec processes constant and variable declarations.
func (p *multiLanguageParser) processValueSpec(spec *ast.ValueSpec, decl *ast.GenDecl, fset *token.FileSet, lines []string, codeExtraction *CodeExtraction) {
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
			codeExtraction.Data.Constants = append(codeExtraction.Data.Constants, extraction.ConstantInfo{
				Name:      varName,
				Value:     value,
				Type:      typeName,
				StartLine: startLine,
				EndLine:   endLine,
			})
		} else if decl.Tok == token.VAR {
			// Variable
			codeExtraction.Data.Variables = append(codeExtraction.Data.Variables, extraction.VariableInfo{
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
func (p *multiLanguageParser) processFuncDecl(decl *ast.FuncDecl, fset *token.FileSet, lines []string, codeExtraction *CodeExtraction) {
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
	codeExtraction.Symbols.Functions = append(codeExtraction.Symbols.Functions, extraction.SymbolInfo{
		Name:      funcName,
		Type:      "function",
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only, not body)
	// For simplicity, we'll extract just the function declaration line
	code := extractFunctionSignature(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
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
