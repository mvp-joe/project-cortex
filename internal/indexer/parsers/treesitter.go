package parsers

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TreeSitterParser provides common tree-sitter parsing functionality.
type treeSitterParser struct {
	language *sitter.Language
	lang     string
}

// newTreeSitterParser creates a new tree-sitter parser for the given language.
func newTreeSitterParser(language *sitter.Language, lang string) *treeSitterParser {
	return &treeSitterParser{
		language: language,
		lang:     lang,
	}
}

// ParseFile parses a source file using tree-sitter and extracts code structure.
func (p *treeSitterParser) ParseFile(ctx context.Context, filePath string, source []byte) (*CodeExtraction, error) {
	parser := sitter.NewParser()
	defer parser.Close()

	parser.SetLanguage(p.language)

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse %s file: %s", p.lang, filePath)
	}
	defer tree.Close()

	rootNode := tree.RootNode()

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

	return extraction, nil
}

// extractNodeText extracts the text content of a tree-sitter node.
func extractNodeText(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}

// extractLines extracts source code lines from startLine to endLine (1-indexed).
func extractLines(lines []string, startLine, endLine int) string {
	if startLine < 1 || endLine < 1 || startLine > len(lines) {
		return ""
	}

	start := startLine - 1
	end := endLine
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start:end], "\n")
}

// nodeToSymbolInfo converts a tree-sitter node to a SymbolInfo.
func nodeToSymbolInfo(node *sitter.Node, source []byte, typeName string) SymbolInfo {
	nameNode := node.ChildByFieldName("name")
	var name string
	if nameNode != nil {
		name = extractNodeText(nameNode, source)
	}

	return SymbolInfo{
		Name:      name,
		Type:      typeName,
		StartLine: int(node.StartPosition().Row) + 1,
		EndLine:   int(node.EndPosition().Row) + 1,
	}
}

// nodeToDefinition converts a tree-sitter node to a Definition.
func nodeToDefinition(node *sitter.Node, source []byte, defType string, lines []string) Definition {
	nameNode := node.ChildByFieldName("name")
	var name string
	if nameNode != nil {
		name = extractNodeText(nameNode, source)
	}

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1
	code := extractLines(lines, startLine, endLine)

	return Definition{
		Name:      name,
		Type:      defType,
		Code:      code,
		StartLine: startLine,
		EndLine:   endLine,
	}
}

// walkTree recursively walks a tree-sitter tree and calls the visitor for each node.
func walkTree(node *sitter.Node, visitor func(*sitter.Node) bool) {
	if node == nil {
		return
	}

	if !visitor(node) {
		return
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		walkTree(child, visitor)
	}
}

// findChildByType finds the first child node with the given type.
func findChildByType(node *sitter.Node, nodeType string) *sitter.Node {
	if node == nil {
		return nil
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child.Kind() == nodeType {
			return child
		}
	}
	return nil
}

// findChildrenByType finds all child nodes with the given type.
func findChildrenByType(node *sitter.Node, nodeType string) []*sitter.Node {
	var results []*sitter.Node
	if node == nil {
		return results
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child.Kind() == nodeType {
			results = append(results, child)
		}
	}
	return results
}
