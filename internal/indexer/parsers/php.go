package parsers

import (
	"context"
	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
	"os"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// PhpParser parses PHP files.
type phpParser struct {
	*treeSitterParser
}

// NewPhpParser creates a new PHP parser.
func NewPhpParser() *phpParser {
	lang := sitter.NewLanguage(php.LanguagePHP())
	return &phpParser{
		treeSitterParser: newTreeSitterParser(lang, "php"),
	}
}

// ParseFile parses a PHP source file.
func (p *phpParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
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

	// Extract namespace
	p.extractNamespace(rootNode, source, codeExtraction)

	// Count imports (use statements)
	p.countImports(rootNode, codeExtraction)

	// Extract symbols, definitions, and data
	p.extractStructure(rootNode, source, lines, codeExtraction)

	return codeExtraction, nil
}

// extractNamespace extracts the namespace.
func (p *phpParser) extractNamespace(node *sitter.Node, source []byte, codeExtraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		if n.Kind() == "namespace_definition" {
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				codeExtraction.Symbols.PackageName = extractNodeText(nameNode, source)
			}
			return false
		}
		return true
	})
}

// countImports counts use statements.
func (p *phpParser) countImports(node *sitter.Node, codeExtraction *CodeExtraction) {
	count := 0
	walkTree(node, func(n *sitter.Node) bool {
		if n.Kind() == "namespace_use_declaration" {
			count++
		}
		return true
	})
	codeExtraction.Symbols.ImportsCount = count
}

// extractStructure extracts classes, interfaces, traits, and functions.
func (p *phpParser) extractStructure(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "class_declaration":
			p.extractClass(n, source, lines, codeExtraction)
			return false
		case "interface_declaration":
			p.extractInterface(n, source, lines, codeExtraction)
			return false
		case "trait_declaration":
			p.extractTrait(n, source, lines, codeExtraction)
			return false
		case "function_definition":
			p.extractFunction(n, source, lines, codeExtraction)
		case "const_declaration":
			p.extractConst(n, source, lines, codeExtraction)
		}
		return true
	})
}

// extractClass extracts a class declaration.
func (p *phpParser) extractClass(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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

	// Extract methods from class body
	bodyNode := node.ChildByFieldName("body")
	if bodyNode != nil {
		p.extractMethodsFromClass(bodyNode, source, lines, codeExtraction, name)
	}
}

// extractInterface extracts an interface declaration.
func (p *phpParser) extractInterface(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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

// extractTrait extracts a trait declaration.
func (p *phpParser) extractTrait(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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

// extractMethodsFromClass extracts methods from a class body.
func (p *phpParser) extractMethodsFromClass(bodyNode *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction, className string) {
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(uint(i))
		if child.Kind() == "method_declaration" {
			p.extractMethod(child, source, lines, codeExtraction, className)
		}
	}
}

// extractMethod extracts a method from a class.
func (p *phpParser) extractMethod(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction, className string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Build signature
	signature := p.buildMethodSignature(node, source, className)

	// Add to symbols
	codeExtraction.Symbols.Functions = append(codeExtraction.Symbols.Functions, extraction.SymbolInfo{
		Name:      name,
		Type:      "method",
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only)
	sigCode := p.extractMethodSignature(lines, startLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "method",
		Code:      sigCode,
		StartLine: startLine,
		EndLine:   startLine,
	})
}

// extractFunction extracts a function definition.
func (p *phpParser) extractFunction(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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
	sigCode := p.extractMethodSignature(lines, startLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "function",
		Code:      sigCode,
		StartLine: startLine,
		EndLine:   startLine,
	})
}

// buildMethodSignature builds a method signature string.
func (p *phpParser) buildMethodSignature(node *sitter.Node, source []byte, className string) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	name := extractNodeText(nameNode, source)
	paramsNode := node.ChildByFieldName("parameters")

	sig := className + "->" + name
	if paramsNode != nil {
		sig += extractNodeText(paramsNode, source)
	} else {
		sig += "()"
	}

	return sig
}

// buildFunctionSignature builds a function signature string.
func (p *phpParser) buildFunctionSignature(node *sitter.Node, source []byte) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	name := extractNodeText(nameNode, source)
	paramsNode := node.ChildByFieldName("parameters")

	sig := name
	if paramsNode != nil {
		sig += extractNodeText(paramsNode, source)
	} else {
		sig += "()"
	}

	return sig
}

// extractMethodSignature extracts just the method signature (up to opening brace).
func (p *phpParser) extractMethodSignature(lines []string, startLine int) string {
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

// extractConst extracts a constant declaration.
func (p *phpParser) extractConst(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	// Extract all const elements
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child.Kind() == "const_element" {
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}

			name := extractNodeText(nameNode, source)
			startLine := int(child.StartPosition().Row) + 1
			endLine := int(child.EndPosition().Row) + 1

			valueNode := child.ChildByFieldName("value")
			var value string
			if valueNode != nil {
				value = extractNodeText(valueNode, source)
			}

			codeExtraction.Data.Constants = append(codeExtraction.Data.Constants, extraction.ConstantInfo{
				Name:      name,
				Value:     value,
				Type:      "",
				StartLine: startLine,
				EndLine:   endLine,
			})
		}
	}
}
