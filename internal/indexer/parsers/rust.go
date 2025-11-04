package parsers

import (
	"context"
	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
	"os"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// RustParser parses Rust files.
type rustParser struct {
	*treeSitterParser
}

// NewRustParser creates a new Rust parser.
func NewRustParser() *rustParser {
	lang := sitter.NewLanguage(rust.Language())
	return &rustParser{
		treeSitterParser: newTreeSitterParser(lang, "rust"),
	}
}

// ParseFile parses a Rust source file.
func (p *rustParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
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

	// Count imports (use declarations)
	p.countImports(rootNode, codeExtraction)

	// Extract symbols, definitions, and data
	p.extractStructure(rootNode, source, lines, codeExtraction)

	return codeExtraction, nil
}

// countImports counts use declarations.
func (p *rustParser) countImports(node *sitter.Node, codeExtraction *CodeExtraction) {
	count := 0
	walkTree(node, func(n *sitter.Node) bool {
		if n.Kind() == "use_declaration" {
			count++
		}
		return true
	})
	codeExtraction.Symbols.ImportsCount = count
}

// extractStructure extracts structs, enums, traits, functions, and constants.
func (p *rustParser) extractStructure(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "struct_item":
			p.extractStruct(n, source, lines, codeExtraction)
		case "enum_item":
			p.extractEnum(n, source, lines, codeExtraction)
		case "trait_item":
			p.extractTrait(n, source, lines, codeExtraction)
		case "impl_item":
			p.extractImpl(n, source, lines, codeExtraction)
			return false // Don't recurse into impl
		case "function_item":
			p.extractFunction(n, source, lines, codeExtraction)
		case "const_item":
			p.extractConst(n, source, lines, codeExtraction)
		case "static_item":
			p.extractStatic(n, source, lines, codeExtraction)
		}
		return true
	})
}

// extractStruct extracts a struct definition.
func (p *rustParser) extractStruct(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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
		Type:      "struct",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "struct",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractEnum extracts an enum definition.
func (p *rustParser) extractEnum(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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
		Type:      "enum",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "enum",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractTrait extracts a trait definition.
func (p *rustParser) extractTrait(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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
		Type:      "trait",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "trait",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractImpl extracts methods from an impl block.
func (p *rustParser) extractImpl(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return
	}

	typeName := extractNodeText(typeNode, source)

	// Extract methods from impl body
	bodyNode := node.ChildByFieldName("body")
	if bodyNode != nil {
		for i := 0; i < int(bodyNode.ChildCount()); i++ {
			child := bodyNode.Child(uint(i))
			if child.Kind() == "function_item" {
				p.extractMethod(child, source, lines, codeExtraction, typeName)
			}
		}
	}
}

// extractMethod extracts a method from an impl block.
func (p *rustParser) extractMethod(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction, typeName string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Build signature
	signature := p.buildFunctionSignature(node, source, typeName)

	// Add to symbols
	codeExtraction.Symbols.Functions = append(codeExtraction.Symbols.Functions, extraction.SymbolInfo{
		Name:      name,
		Type:      "method",
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only)
	sigCode := p.extractFunctionSignature(lines, startLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "method",
		Code:      sigCode,
		StartLine: startLine,
		EndLine:   startLine,
	})
}

// extractFunction extracts a function definition.
func (p *rustParser) extractFunction(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Build signature
	signature := p.buildFunctionSignature(node, source, "")

	// Add to symbols
	codeExtraction.Symbols.Functions = append(codeExtraction.Symbols.Functions, extraction.SymbolInfo{
		Name:      name,
		Type:      "function",
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only)
	sigCode := p.extractFunctionSignature(lines, startLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "function",
		Code:      sigCode,
		StartLine: startLine,
		EndLine:   startLine,
	})
}

// buildFunctionSignature builds a function signature string.
func (p *rustParser) buildFunctionSignature(node *sitter.Node, source []byte, typeName string) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	name := extractNodeText(nameNode, source)
	paramsNode := node.ChildByFieldName("parameters")
	returnNode := node.ChildByFieldName("return_type")

	sig := ""
	if typeName != "" {
		sig = typeName + "::"
	}
	sig += name

	if paramsNode != nil {
		sig += extractNodeText(paramsNode, source)
	} else {
		sig += "()"
	}

	if returnNode != nil {
		sig += " " + extractNodeText(returnNode, source)
	}

	return sig
}

// extractFunctionSignature extracts just the function signature (up to opening brace).
func (p *rustParser) extractFunctionSignature(lines []string, startLine int) string {
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

// extractConst extracts a const declaration.
func (p *rustParser) extractConst(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	typeNode := node.ChildByFieldName("type")
	var typeName string
	if typeNode != nil {
		typeName = extractNodeText(typeNode, source)
	}

	valueNode := node.ChildByFieldName("value")
	var value string
	if valueNode != nil {
		value = extractNodeText(valueNode, source)
	}

	codeExtraction.Data.Constants = append(codeExtraction.Data.Constants, extraction.ConstantInfo{
		Name:      name,
		Value:     value,
		Type:      typeName,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// extractStatic extracts a static variable declaration.
func (p *rustParser) extractStatic(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	typeNode := node.ChildByFieldName("type")
	var typeName string
	if typeNode != nil {
		typeName = extractNodeText(typeNode, source)
	}

	valueNode := node.ChildByFieldName("value")
	var value string
	if valueNode != nil {
		value = extractNodeText(valueNode, source)
	}

	codeExtraction.Data.Variables = append(codeExtraction.Data.Variables, extraction.VariableInfo{
		Name:      name,
		Value:     value,
		Type:      typeName,
		StartLine: startLine,
		EndLine:   endLine,
	})
}
