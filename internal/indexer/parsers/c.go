package parsers

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	c "github.com/tree-sitter/tree-sitter-c/bindings/go"
)

// CParser parses C files.
type cParser struct {
	*treeSitterParser
}

// NewCParser creates a new C parser.
func NewCParser() *cParser {
	lang := sitter.NewLanguage(c.Language())
	return &cParser{
		treeSitterParser: newTreeSitterParser(lang, "c"),
	}
}

// ParseFile parses a C source file.
func (p *cParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
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

	// Determine language based on extension
	lang := "c"
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".cpp" || ext == ".cc" || ext == ".hpp" {
		lang = "cpp"
	}

	extraction := &CodeExtraction{
		Language:  lang,
		FilePath:  filePath,
		StartLine: 1,
		EndLine:   int(rootNode.EndPosition().Row) + 1,
		Symbols: &SymbolsData{
			Types:     []SymbolInfo{},
			Functions: []SymbolInfo{},
		},
		Definitions: &DefinitionsData{
			Definitions: []Definition{},
		},
		Data: &DataData{
			Constants: []ConstantInfo{},
			Variables: []VariableInfo{},
		},
	}

	// Count includes
	p.countIncludes(rootNode, extraction)

	// Extract symbols, definitions, and data
	p.extractStructure(rootNode, source, lines, extraction)

	return extraction, nil
}

// countIncludes counts #include directives.
func (p *cParser) countIncludes(node *sitter.Node, extraction *CodeExtraction) {
	count := 0
	walkTree(node, func(n *sitter.Node) bool {
		if n.Kind() == "preproc_include" {
			count++
		}
		return true
	})
	extraction.Symbols.ImportsCount = count
}

// extractStructure extracts structs, unions, enums, functions, and variables.
func (p *cParser) extractStructure(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "struct_specifier":
			p.extractStruct(n, source, lines, extraction)
		case "union_specifier":
			p.extractUnion(n, source, lines, extraction)
		case "enum_specifier":
			p.extractEnum(n, source, lines, extraction)
		case "function_definition":
			p.extractFunction(n, source, lines, extraction)
		case "declaration":
			p.extractDeclaration(n, source, lines, extraction)
		}
		return true
	})
}

// extractStruct extracts a struct definition.
func (p *cParser) extractStruct(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Add to symbols
	extraction.Symbols.Types = append(extraction.Symbols.Types, SymbolInfo{
		Name:      name,
		Type:      "struct",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	extraction.Definitions.Definitions = append(extraction.Definitions.Definitions, Definition{
		Name:      name,
		Type:      "struct",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractUnion extracts a union definition.
func (p *cParser) extractUnion(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Add to symbols
	extraction.Symbols.Types = append(extraction.Symbols.Types, SymbolInfo{
		Name:      name,
		Type:      "union",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	extraction.Definitions.Definitions = append(extraction.Definitions.Definitions, Definition{
		Name:      name,
		Type:      "union",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractEnum extracts an enum definition.
func (p *cParser) extractEnum(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Add to symbols
	extraction.Symbols.Types = append(extraction.Symbols.Types, SymbolInfo{
		Name:      name,
		Type:      "enum",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	extraction.Definitions.Definitions = append(extraction.Definitions.Definitions, Definition{
		Name:      name,
		Type:      "enum",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractFunction extracts a function definition.
func (p *cParser) extractFunction(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
	declaratorNode := node.ChildByFieldName("declarator")
	if declaratorNode == nil {
		return
	}

	// Find function name (might be nested in pointer_declarator, etc.)
	name := p.findFunctionName(declaratorNode, source)
	if name == "" {
		return
	}

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Build signature
	signature := p.buildFunctionSignature(node, source)

	// Add to symbols
	extraction.Symbols.Functions = append(extraction.Symbols.Functions, SymbolInfo{
		Name:      name,
		Type:      "function",
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only)
	sigCode := p.extractFunctionSignature(lines, startLine)
	extraction.Definitions.Definitions = append(extraction.Definitions.Definitions, Definition{
		Name:      name,
		Type:      "function",
		Code:      sigCode,
		StartLine: startLine,
		EndLine:   startLine,
	})
}

// findFunctionName recursively finds the function name in a declarator.
func (p *cParser) findFunctionName(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	switch node.Kind() {
	case "identifier":
		return extractNodeText(node, source)
	case "function_declarator":
		declaratorNode := node.ChildByFieldName("declarator")
		return p.findFunctionName(declaratorNode, source)
	case "pointer_declarator":
		declaratorNode := node.ChildByFieldName("declarator")
		return p.findFunctionName(declaratorNode, source)
	default:
		// Try to find identifier child
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(uint(i))
			if child.Kind() == "identifier" {
				return extractNodeText(child, source)
			}
		}
	}

	return ""
}

// buildFunctionSignature builds a function signature string.
func (p *cParser) buildFunctionSignature(node *sitter.Node, source []byte) string {
	declaratorNode := node.ChildByFieldName("declarator")
	if declaratorNode == nil {
		return ""
	}

	// Extract the entire function declaration (without body)
	typeNode := node.ChildByFieldName("type")
	var sig string
	if typeNode != nil {
		sig = extractNodeText(typeNode, source) + " "
	}

	sig += extractNodeText(declaratorNode, source)
	return sig
}

// extractFunctionSignature extracts just the function signature (up to opening brace).
func (p *cParser) extractFunctionSignature(lines []string, startLine int) string {
	if startLine < 1 || startLine > len(lines) {
		return ""
	}

	// Find the opening brace across multiple lines
	result := ""
	for i := startLine - 1; i < len(lines); i++ {
		line := lines[i]
		result += line
		if strings.Contains(line, "{") {
			parts := strings.Split(result, "{")
			return strings.TrimSpace(parts[0]) + " { ... }"
		}
		result += "\n"
	}

	return lines[startLine-1]
}

// extractDeclaration extracts variable declarations (including const).
func (p *cParser) extractDeclaration(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
	// Check if it's a top-level declaration
	if !p.isTopLevel(node) {
		return
	}

	// Get type
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return
	}

	typeText := extractNodeText(typeNode, source)
	isConst := strings.Contains(typeText, "const")

	// Extract all declarators
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child.Kind() == "init_declarator" || child.Kind() == "pointer_declarator" || child.Kind() == "array_declarator" {
			p.extractDeclarator(child, source, lines, extraction, typeText, isConst)
		}
	}
}

// isTopLevel checks if a node is at file level.
func (p *cParser) isTopLevel(node *sitter.Node) bool {
	parent := node.Parent()
	for parent != nil {
		nodeType := parent.Kind()
		if nodeType == "function_definition" || nodeType == "compound_statement" {
			return false
		}
		if nodeType == "translation_unit" {
			return true
		}
		parent = parent.Parent()
	}
	return true
}

// extractDeclarator extracts a single variable declarator.
func (p *cParser) extractDeclarator(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction, typeName string, isConst bool) {
	var name string
	var value string

	if node.Kind() == "init_declarator" {
		declaratorNode := node.ChildByFieldName("declarator")
		if declaratorNode != nil {
			name = extractNodeText(declaratorNode, source)
		}
		valueNode := node.ChildByFieldName("value")
		if valueNode != nil {
			value = extractNodeText(valueNode, source)
		}
	} else {
		name = extractNodeText(node, source)
	}

	if name == "" {
		return
	}

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	if isConst {
		extraction.Data.Constants = append(extraction.Data.Constants, ConstantInfo{
			Name:      name,
			Value:     value,
			Type:      typeName,
			StartLine: startLine,
			EndLine:   endLine,
		})
	} else {
		extraction.Data.Variables = append(extraction.Data.Variables, VariableInfo{
			Name:      name,
			Value:     value,
			Type:      typeName,
			StartLine: startLine,
			EndLine:   endLine,
		})
	}
}

// CppParser is an alias for CParser since C++ uses the same grammar.
type CppParser = cParser

// NewCppParser creates a new C++ parser.
func NewCppParser() *CppParser {
	return NewCParser()
}
