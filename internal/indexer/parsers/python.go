package parsers

import (
	"context"
	"os"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// PythonParser parses Python files.
type pythonParser struct {
	*treeSitterParser
}

// NewPythonParser creates a new Python parser.
func NewPythonParser() *pythonParser {
	lang := sitter.NewLanguage(python.Language())
	return &pythonParser{
		treeSitterParser: newTreeSitterParser(lang, "python"),
	}
}

// ParseFile parses a Python source file.
func (p *pythonParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
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

	extraction := &CodeExtraction{
		Language:  p.lang,
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

	// Count imports
	p.countImports(rootNode, extraction)

	// Extract symbols, definitions, and data
	p.extractStructure(rootNode, source, lines, extraction)

	return extraction, nil
}

// countImports counts import statements.
func (p *pythonParser) countImports(node *sitter.Node, extraction *CodeExtraction) {
	count := 0
	walkTree(node, func(n *sitter.Node) bool {
		nodeType := n.Kind()
		if nodeType == "import_statement" || nodeType == "import_from_statement" {
			count++
		}
		return true
	})
	extraction.Symbols.ImportsCount = count
}

// extractStructure extracts classes, functions, and variables.
func (p *pythonParser) extractStructure(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "class_definition":
			p.extractClass(n, source, lines, extraction)
			return false // Don't recurse into class body here
		case "function_definition":
			// Only extract top-level functions
			if p.isTopLevel(n) {
				p.extractFunction(n, source, lines, extraction)
			}
		case "assignment":
			// Only extract top-level assignments
			if p.isTopLevel(n) {
				p.extractAssignment(n, source, lines, extraction)
			}
		}
		return true
	})
}

// isTopLevel checks if a node is at the module level (not inside a class or function).
func (p *pythonParser) isTopLevel(node *sitter.Node) bool {
	parent := node.Parent()
	for parent != nil {
		nodeType := parent.Kind()
		if nodeType == "class_definition" || nodeType == "function_definition" {
			return false
		}
		if nodeType == "module" {
			return true
		}
		parent = parent.Parent()
	}
	return true
}

// extractClass extracts a class definition.
func (p *pythonParser) extractClass(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
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
		Type:      "class",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	extraction.Definitions.Definitions = append(extraction.Definitions.Definitions, Definition{
		Name:      name,
		Type:      "class",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Extract methods from class body
	bodyNode := node.ChildByFieldName("body")
	if bodyNode != nil {
		p.extractMethodsFromClass(bodyNode, source, lines, extraction, name)
	}
}

// extractMethodsFromClass extracts methods from a class body.
func (p *pythonParser) extractMethodsFromClass(bodyNode *sitter.Node, source []byte, lines []string, extraction *CodeExtraction, className string) {
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(uint(i))
		if child.Kind() == "function_definition" {
			p.extractMethod(child, source, lines, extraction, className)
		}
	}
}

// extractMethod extracts a method from a class.
func (p *pythonParser) extractMethod(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction, className string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Build signature
	signature := p.buildFunctionSignature(node, source, className)

	// Add to symbols
	extraction.Symbols.Functions = append(extraction.Symbols.Functions, SymbolInfo{
		Name:      name,
		Type:      "method",
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only)
	sigCode := p.extractFunctionSignature(lines, startLine)
	extraction.Definitions.Definitions = append(extraction.Definitions.Definitions, Definition{
		Name:      name,
		Type:      "method",
		Code:      sigCode,
		StartLine: startLine,
		EndLine:   startLine,
	})
}

// extractFunction extracts a function definition.
func (p *pythonParser) extractFunction(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
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

// buildFunctionSignature builds a function signature string.
func (p *pythonParser) buildFunctionSignature(node *sitter.Node, source []byte, className string) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	name := extractNodeText(nameNode, source)
	paramsNode := node.ChildByFieldName("parameters")
	returnNode := node.ChildByFieldName("return_type")

	sig := ""
	if className != "" {
		sig = className + "."
	}
	sig += name

	if paramsNode != nil {
		sig += extractNodeText(paramsNode, source)
	} else {
		sig += "()"
	}

	if returnNode != nil {
		sig += " -> " + extractNodeText(returnNode, source)
	}

	return sig
}

// extractFunctionSignature extracts just the function signature (def line).
func (p *pythonParser) extractFunctionSignature(lines []string, startLine int) string {
	if startLine < 1 || startLine > len(lines) {
		return ""
	}

	line := lines[startLine-1]
	// Find the colon
	if colonIdx := strings.Index(line, ":"); colonIdx != -1 {
		return strings.TrimSpace(line[:colonIdx+1])
	}

	return line
}

// extractAssignment extracts variable/constant assignments.
func (p *pythonParser) extractAssignment(node *sitter.Node, source []byte, lines []string, extraction *CodeExtraction) {
	// Get left side (variable name)
	leftNode := node.ChildByFieldName("left")
	if leftNode == nil {
		return
	}

	name := extractNodeText(leftNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Get right side (value)
	rightNode := node.ChildByFieldName("right")
	var value string
	if rightNode != nil {
		value = extractNodeText(rightNode, source)
	}

	// In Python, we consider ALL_CAPS names as constants
	if isConstantName(name) {
		extraction.Data.Constants = append(extraction.Data.Constants, ConstantInfo{
			Name:      name,
			Value:     value,
			Type:      "",
			StartLine: startLine,
			EndLine:   endLine,
		})
	} else {
		extraction.Data.Variables = append(extraction.Data.Variables, VariableInfo{
			Name:      name,
			Value:     value,
			Type:      "",
			StartLine: startLine,
			EndLine:   endLine,
		})
	}
}

// isConstantName checks if a name follows Python constant naming convention (ALL_CAPS).
func isConstantName(name string) bool {
	if len(name) == 0 {
		return false
	}
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' {
			return false
		}
	}
	return true
}
