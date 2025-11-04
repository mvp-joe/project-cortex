package parsers

import (
	"context"
	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
	"os"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// TypeScriptParser parses TypeScript files.
type typeScriptParser struct {
	*treeSitterParser
}

// NewTypeScriptParser creates a new TypeScript parser.
func NewTypeScriptParser() *typeScriptParser {
	lang := sitter.NewLanguage(typescript.LanguageTypescript())
	return &typeScriptParser{
		treeSitterParser: newTreeSitterParser(lang, "typescript"),
	}
}

// ParseFile parses a TypeScript source file.
func (p *typeScriptParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	parser := sitter.NewParser()
	defer parser.Close()

	parser.SetLanguage(p.language)

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, nil // Return nil for unparseable files
	}
	defer tree.Close()

	rootNode := tree.RootNode()
	lines := strings.Split(string(source), "\n")

	codeExtraction := &CodeExtraction{
		Language:  p.lang,
		FilePath:  filePath,
		StartLine: 1,
		EndLine:   int(rootNode.EndPosition().Row) + 1,
		Symbols: &extraction.SymbolsData{
			Types:     []extraction.SymbolInfo{},
			Functions: []extraction.SymbolInfo{},
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
	p.countImports(rootNode, codeExtraction)

	// Extract symbols, definitions, and data
	p.extractStructure(rootNode, source, lines, codeExtraction)

	return codeExtraction, nil
}

// countImports counts import statements.
func (p *typeScriptParser) countImports(node *sitter.Node, codeExtraction *CodeExtraction) {
	count := 0
	walkTree(node, func(n *sitter.Node) bool {
		if n.Kind() == "import_statement" {
			count++
		}
		return true
	})
	codeExtraction.Symbols.ImportsCount = count
}

// extractStructure extracts classes, interfaces, functions, and variables.
func (p *typeScriptParser) extractStructure(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "class_declaration":
			p.extractClass(n, source, lines, codeExtraction)
		case "interface_declaration":
			p.extractInterface(n, source, lines, codeExtraction)
		case "type_alias_declaration":
			p.extractTypeAlias(n, source, lines, codeExtraction)
		case "function_declaration":
			p.extractFunction(n, source, lines, codeExtraction)
		case "lexical_declaration":
			p.extractLexicalDeclaration(n, source, lines, codeExtraction)
		case "variable_declaration":
			p.extractVariableDeclaration(n, source, lines, codeExtraction)
		}
		return true
	})
}

// extractClass extracts a class declaration.
func (p *typeScriptParser) extractClass(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Add to symbols
	codeExtraction.Symbols.Types = append(codeExtraction.Symbols.Types, extraction.SymbolInfo{
		Name:      name,
		Type:      "class",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "class",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractInterface extracts an interface declaration.
func (p *typeScriptParser) extractInterface(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Add to symbols
	codeExtraction.Symbols.Types = append(codeExtraction.Symbols.Types, extraction.SymbolInfo{
		Name:      name,
		Type:      "interface",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "interface",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractTypeAlias extracts a type alias declaration.
func (p *typeScriptParser) extractTypeAlias(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Add to symbols
	codeExtraction.Symbols.Types = append(codeExtraction.Symbols.Types, extraction.SymbolInfo{
		Name:      name,
		Type:      "type",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "type",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractFunction extracts a function declaration.
func (p *typeScriptParser) extractFunction(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Build signature
	signature := p.buildFunctionSignature(node, source)

	// Add to symbols
	codeExtraction.Symbols.Functions = append(codeExtraction.Symbols.Functions, extraction.SymbolInfo{
		Name:      name,
		Type:      "function",
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only)
	sigCode := p.extractFunctionSignature(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "function",
		Code:      sigCode,
		StartLine: startLine,
		EndLine:   startLine,
	})
}

// buildFunctionSignature builds a function signature string.
func (p *typeScriptParser) buildFunctionSignature(node *sitter.Node, source []byte) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	name := extractNodeText(nameNode, source)
	paramsNode := node.ChildByFieldName("parameters")
	returnNode := node.ChildByFieldName("return_type")

	sig := name + "("
	if paramsNode != nil {
		sig += extractNodeText(paramsNode, source)
	} else {
		sig += "()"
	}

	if returnNode != nil {
		sig += ": " + extractNodeText(returnNode, source)
	}

	return sig
}

// extractFunctionSignature extracts just the function signature (up to opening brace).
func (p *typeScriptParser) extractFunctionSignature(lines []string, startLine, endLine int) string {
	if startLine < 1 || startLine > len(lines) {
		return ""
	}

	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, "{") {
			parts := strings.Split(line, "{")
			if i == startLine-1 {
				return strings.TrimSpace(parts[0]) + " { ... }"
			}
			sig := strings.Join(lines[startLine-1:i], "\n")
			sig += "\n" + strings.TrimSpace(parts[0]) + " { ... }"
			return sig
		}
	}

	return lines[startLine-1]
}

// extractLexicalDeclaration extracts const/let declarations.
func (p *typeScriptParser) extractLexicalDeclaration(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	// Find variable_declarator children
	declarators := findChildrenByType(node, "variable_declarator")
	for _, decl := range declarators {
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		name := extractNodeText(nameNode, source)
		startLine := int(decl.StartPosition().Row) + 1
		endLine := int(decl.EndPosition().Row) + 1

		valueNode := decl.ChildByFieldName("value")
		var value string
		if valueNode != nil {
			value = extractNodeText(valueNode, source)
		}

		typeNode := decl.ChildByFieldName("type")
		var typeName string
		if typeNode != nil {
			typeName = extractNodeText(typeNode, source)
		}

		// Check if this is a const
		parentText := extractNodeText(node, source)
		if strings.HasPrefix(parentText, "const") {
			codeExtraction.Data.Constants = append(codeExtraction.Data.Constants, extraction.ConstantInfo{
				Name:      name,
				Value:     value,
				Type:      typeName,
				StartLine: startLine,
				EndLine:   endLine,
			})
		} else {
			codeExtraction.Data.Variables = append(codeExtraction.Data.Variables, extraction.VariableInfo{
				Name:      name,
				Value:     value,
				Type:      typeName,
				StartLine: startLine,
				EndLine:   endLine,
			})
		}
	}
}

// extractVariableDeclaration extracts var declarations.
func (p *typeScriptParser) extractVariableDeclaration(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	declarators := findChildrenByType(node, "variable_declarator")
	for _, decl := range declarators {
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		name := extractNodeText(nameNode, source)
		startLine := int(decl.StartPosition().Row) + 1
		endLine := int(decl.EndPosition().Row) + 1

		valueNode := decl.ChildByFieldName("value")
		var value string
		if valueNode != nil {
			value = extractNodeText(valueNode, source)
		}

		typeNode := decl.ChildByFieldName("type")
		var typeName string
		if typeNode != nil {
			typeName = extractNodeText(typeNode, source)
		}

		codeExtraction.Data.Variables = append(codeExtraction.Data.Variables, extraction.VariableInfo{
			Name:      name,
			Value:     value,
			Type:      typeName,
			StartLine: startLine,
			EndLine:   endLine,
		})
	}
}

// JavaScriptParser parses JavaScript files.
type javaScriptParser struct {
	*treeSitterParser
}

// NewJavaScriptParser creates a new JavaScript parser.
func NewJavaScriptParser() *javaScriptParser {
	lang := sitter.NewLanguage(typescript.LanguageTypescript())
	return &javaScriptParser{
		treeSitterParser: newTreeSitterParser(lang, "javascript"),
	}
}

// ParseFile parses a JavaScript source file (reuses TypeScript logic).
func (p *javaScriptParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
	// JavaScript uses same AST structure as TypeScript
	tsParser := &typeScriptParser{
		treeSitterParser: p.treeSitterParser,
	}
	extraction, err := tsParser.ParseFile(ctx, filePath)
	if extraction != nil {
		extraction.Language = "javascript"
	}
	return extraction, err
}
