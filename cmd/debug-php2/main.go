package main

import (
	"fmt"
	"os"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

func main() {
	source, err := os.ReadFile("testdata/code/php/simple.php")
	if err != nil {
		panic(err)
	}

	lang := sitter.NewLanguage(php.LanguagePHP())
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang)

	tree := parser.Parse(source, nil)
	if tree == nil {
		panic("failed to parse")
	}
	defer tree.Close()

	rootNode := tree.RootNode()

	fmt.Println("=== Looking for const declarations ===")
	walkTree(rootNode, source)
}

func walkTree(node *sitter.Node, source []byte) {
	if node == nil {
		return
	}

	if node.Kind() == "const_declaration" {
		fmt.Printf("\nFound const_declaration at line %d\n", node.StartPosition().Row+1)
		fmt.Printf("  Child count: %d\n", node.ChildCount())

		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(uint(i))
			fmt.Printf("  Child %d: %s\n", i, child.Kind())

			if child.Kind() == "const_element" {
				fmt.Printf("    Found const_element!\n")
				nameNode := child.ChildByFieldName("name")
				if nameNode != nil {
					name := string(source[nameNode.StartByte():nameNode.EndByte()])
					fmt.Printf("      Name: %s\n", name)
				} else {
					fmt.Printf("      Name node is nil!\n")
				}

				valueNode := child.ChildByFieldName("value")
				if valueNode != nil {
					value := string(source[valueNode.StartByte():valueNode.EndByte()])
					fmt.Printf("      Value: %s\n", value)
				} else {
					fmt.Printf("      Value node is nil!\n")
				}

				// Show all children of const_element
				fmt.Printf("      const_element has %d children:\n", child.ChildCount())
				for j := 0; j < int(child.ChildCount()); j++ {
					grandchild := child.Child(uint(j))
					text := string(source[grandchild.StartByte():grandchild.EndByte()])
					fmt.Printf("        [%d] %s = %q\n", j, grandchild.Kind(), text)
				}
			}
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		walkTree(child, source)
	}
}
