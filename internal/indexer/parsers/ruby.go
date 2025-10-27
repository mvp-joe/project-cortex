package parsers

import (
	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
	"context"
	"os"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
)

// RubyParser parses Ruby files.
type rubyParser struct {
	*treeSitterParser
}

// NewRubyParser creates a new Ruby parser.
func NewRubyParser() *rubyParser {
	lang := sitter.NewLanguage(ruby.Language())
	return &rubyParser{
		treeSitterParser: newTreeSitterParser(lang, "ruby"),
	}
}

// ParseFile parses a Ruby source file.
func (p *rubyParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
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

	// Count requires
	p.countImports(rootNode, codeExtraction)

	// Extract symbols, definitions, and data
	p.extractStructure(rootNode, source, lines, codeExtraction)

	return codeExtraction, nil
}

// countImports counts require statements.
func (p *rubyParser) countImports(node *sitter.Node, codeExtraction *CodeExtraction) {
	count := 0
	walkTree(node, func(n *sitter.Node) bool {
		nodeType := n.Kind()
		if nodeType == "call" {
			methodNode := n.ChildByFieldName("method")
			if methodNode != nil && methodNode.Kind() == "identifier" {
				// This is a simplified check - would need more context
				count++
			}
		}
		return true
	})
	// Simplified: we don't accurately count requires without more context
	codeExtraction.Symbols.ImportsCount = 0
}

// extractStructure extracts classes, modules, and methods.
func (p *rubyParser) extractStructure(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "class":
			p.extractClass(n, source, lines, codeExtraction)
			return false
		case "module":
			p.extractModule(n, source, lines, codeExtraction)
			return false
		case "method":
			if p.isTopLevel(n) {
				p.extractMethod(n, source, lines, codeExtraction,"")
			}
		case "assignment":
			if p.isTopLevel(n) {
				p.extractAssignment(n, source, lines, codeExtraction)
			}
		}
		return true
	})
}

// isTopLevel checks if a node is at the top level.
func (p *rubyParser) isTopLevel(node *sitter.Node) bool {
	parent := node.Parent()
	for parent != nil {
		nodeType := parent.Kind()
		if nodeType == "class" || nodeType == "module" || nodeType == "method" {
			return false
		}
		if nodeType == "program" {
			return true
		}
		parent = parent.Parent()
	}
	return true
}

// extractClass extracts a class definition.
func (p *rubyParser) extractClass(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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
	p.extractMethodsFromClass(node, source, lines, codeExtraction,name)
}

// extractModule extracts a module definition.
func (p *rubyParser) extractModule(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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
		Type:      "module",
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Add to definitions
	code := extractLines(lines, startLine, endLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      "module",
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Extract nested types and methods from module body
	p.extractModuleContents(node, source, lines, codeExtraction,name)
}

// extractModuleContents extracts nested classes, modules, and methods from a module body.
func (p *rubyParser) extractModuleContents(moduleNode *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction, moduleName string) {
	for i := 0; i < int(moduleNode.ChildCount()); i++ {
		child := moduleNode.Child(uint(i))
		switch child.Kind() {
		case "class":
			p.extractClass(child, source, lines, codeExtraction)
		case "module":
			p.extractModule(child, source, lines, codeExtraction)
		case "method":
			p.extractMethod(child, source, lines, codeExtraction,moduleName)
		case "body_statement":
			// Nested content might be inside body_statement
			for j := 0; j < int(child.ChildCount()); j++ {
				bodyChild := child.Child(uint(j))
				switch bodyChild.Kind() {
				case "class":
					p.extractClass(bodyChild, source, lines, codeExtraction)
				case "module":
					p.extractModule(bodyChild, source, lines, codeExtraction)
				case "method":
					p.extractMethod(bodyChild, source, lines, codeExtraction,moduleName)
				}
			}
		}
	}
}

// extractMethodsFromClass extracts methods from a class/module body.
func (p *rubyParser) extractMethodsFromClass(classNode *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction, className string) {
	for i := 0; i < int(classNode.ChildCount()); i++ {
		child := classNode.Child(uint(i))
		if child.Kind() == "method" {
			p.extractMethod(child, source, lines, codeExtraction,className)
		} else if child.Kind() == "body_statement" {
			// Methods might be inside body_statement
			for j := 0; j < int(child.ChildCount()); j++ {
				bodyChild := child.Child(uint(j))
				if bodyChild.Kind() == "method" {
					p.extractMethod(bodyChild, source, lines, codeExtraction,className)
				}
			}
		}
	}
}

// extractMethod extracts a method definition.
func (p *rubyParser) extractMethod(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction, className string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := extractNodeText(nameNode, source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	// Build signature
	signature := p.buildMethodSignature(node, source, className)

	methodType := "function"
	if className != "" {
		methodType = "method"
	}

	// Add to symbols
	codeExtraction.Symbols.Functions = append(codeExtraction.Symbols.Functions, extraction.SymbolInfo{
		Name:      name,
		Type:      methodType,
		StartLine: startLine,
		EndLine:   endLine,
		Signature: signature,
	})

	// Add to definitions (signature only)
	sigCode := p.extractMethodSignature(lines, startLine)
	codeExtraction.Definitions.Definitions = append(codeExtraction.Definitions.Definitions, extraction.Definition{
		Name:      name,
		Type:      methodType,
		Code:      sigCode,
		StartLine: startLine,
		EndLine:   startLine,
	})
}

// buildMethodSignature builds a method signature string.
func (p *rubyParser) buildMethodSignature(node *sitter.Node, source []byte, className string) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	name := extractNodeText(nameNode, source)
	paramsNode := node.ChildByFieldName("parameters")

	sig := ""
	if className != "" {
		sig = className + "#"
	}
	sig += name

	if paramsNode != nil {
		sig += extractNodeText(paramsNode, source)
	} else {
		sig += "()"
	}

	return sig
}

// extractMethodSignature extracts just the method signature (def line).
func (p *rubyParser) extractMethodSignature(lines []string, startLine int) string {
	if startLine < 1 || startLine > len(lines) {
		return ""
	}

	return lines[startLine-1]
}

// extractAssignment extracts variable/constant assignments.
func (p *rubyParser) extractAssignment(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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

	// In Ruby, constants start with uppercase letter
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		codeExtraction.Data.Constants = append(codeExtraction.Data.Constants, extraction.ConstantInfo{
			Name:      name,
			Value:     value,
			Type:      "",
			StartLine: startLine,
			EndLine:   endLine,
		})
	} else {
		codeExtraction.Data.Variables = append(codeExtraction.Data.Variables, extraction.VariableInfo{
			Name:      name,
			Value:     value,
			Type:      "",
			StartLine: startLine,
			EndLine:   endLine,
		})
	}
}
