package parsers

import (
	"context"
	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
	"os"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

// JavaParser parses Java files.
type javaParser struct {
	*treeSitterParser
}

// NewJavaParser creates a new Java parser.
func NewJavaParser() *javaParser {
	lang := sitter.NewLanguage(java.Language())
	return &javaParser{
		treeSitterParser: newTreeSitterParser(lang, "java"),
	}
}

// ParseFile parses a Java source file.
func (p *javaParser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
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

	// Extract package name
	p.extractPackageName(rootNode, source, codeExtraction)

	// Count imports
	p.countImports(rootNode, codeExtraction)

	// Extract symbols, definitions, and data
	p.extractStructure(rootNode, source, lines, codeExtraction)

	return codeExtraction, nil
}

// extractPackageName extracts the package name.
func (p *javaParser) extractPackageName(node *sitter.Node, source []byte, codeExtraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		if n.Kind() == "package_declaration" {
			nameNode := findChildByType(n, "scoped_identifier")
			if nameNode == nil {
				nameNode = findChildByType(n, "identifier")
			}
			if nameNode != nil {
				codeExtraction.Symbols.PackageName = extractNodeText(nameNode, source)
			}
			return false
		}
		return true
	})
}

// countImports counts import statements.
func (p *javaParser) countImports(node *sitter.Node, codeExtraction *CodeExtraction) {
	count := 0
	walkTree(node, func(n *sitter.Node) bool {
		if n.Kind() == "import_declaration" {
			count++
		}
		return true
	})
	codeExtraction.Symbols.ImportsCount = count
}

// extractStructure extracts classes, interfaces, enums, and methods.
func (p *javaParser) extractStructure(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	walkTree(node, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "class_declaration":
			p.extractClass(n, source, lines, codeExtraction)
			return false // Don't recurse into class body here
		case "interface_declaration":
			p.extractInterface(n, source, lines, codeExtraction)
			return false
		case "enum_declaration":
			p.extractEnum(n, source, lines, codeExtraction)
			return false
		case "field_declaration":
			p.extractField(n, source, lines, codeExtraction)
		}
		return true
	})
}

// extractClass extracts a class declaration.
func (p *javaParser) extractClass(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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
func (p *javaParser) extractInterface(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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

	// Extract methods from interface body
	bodyNode := node.ChildByFieldName("body")
	if bodyNode != nil {
		p.extractMethodsFromClass(bodyNode, source, lines, codeExtraction, name)
	}
}

// extractEnum extracts an enum declaration.
func (p *javaParser) extractEnum(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
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

// extractMethodsFromClass extracts methods from a class/interface body.
func (p *javaParser) extractMethodsFromClass(bodyNode *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction, className string) {
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(uint(i))
		if child.Kind() == "method_declaration" {
			p.extractMethod(child, source, lines, codeExtraction, className)
		}
	}
}

// extractMethod extracts a method from a class.
func (p *javaParser) extractMethod(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction, className string) {
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

// buildMethodSignature builds a method signature string.
func (p *javaParser) buildMethodSignature(node *sitter.Node, source []byte, className string) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	name := extractNodeText(nameNode, source)
	typeNode := node.ChildByFieldName("type")
	paramsNode := node.ChildByFieldName("parameters")

	sig := className + "." + name
	if paramsNode != nil {
		sig += extractNodeText(paramsNode, source)
	} else {
		sig += "()"
	}

	if typeNode != nil {
		sig += ": " + extractNodeText(typeNode, source)
	}

	return sig
}

// extractMethodSignature extracts just the method signature (up to opening brace).
func (p *javaParser) extractMethodSignature(lines []string, startLine int) string {
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
		if strings.Contains(line, ";") {
			// Abstract method (interface)
			return strings.TrimSpace(result)
		}
		result += "\n"
	}

	return lines[startLine-1]
}

// extractField extracts a field declaration (static or instance).
func (p *javaParser) extractField(node *sitter.Node, source []byte, lines []string, codeExtraction *CodeExtraction) {
	// Check if it's a constant (static final)
	modifiersNode := node.ChildByFieldName("modifiers")
	isStatic := false
	isFinal := false
	if modifiersNode != nil {
		modText := extractNodeText(modifiersNode, source)
		isStatic = strings.Contains(modText, "static")
		isFinal = strings.Contains(modText, "final")
	}

	typeNode := node.ChildByFieldName("type")
	var typeName string
	if typeNode != nil {
		typeName = extractNodeText(typeNode, source)
	}

	// Extract all declarators
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child.Kind() == "variable_declarator" {
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

			if isStatic && isFinal {
				codeExtraction.Data.Constants = append(codeExtraction.Data.Constants, extraction.ConstantInfo{
					Name:      name,
					Value:     value,
					Type:      typeName,
					StartLine: startLine,
					EndLine:   endLine,
				})
			} else if isStatic {
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
}
