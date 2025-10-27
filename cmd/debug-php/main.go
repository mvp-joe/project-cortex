package main

import (
	"context"
	"fmt"
	"log"

	"project-cortex/internal/indexer/parsers"
)

func main() {
	parser := parsers.NewPhpParser()
	extraction, err := parser.ParseFile(context.Background(), "testdata/code/php/simple.php")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== CONSTANTS ===")
	fmt.Printf("Count: %d\n", len(extraction.Data.Constants))
	for _, c := range extraction.Data.Constants {
		fmt.Printf("  %s = %s (line %d)\n", c.Name, c.Value, c.StartLine)
	}

	fmt.Println("\n=== TYPES ===")
	for _, t := range extraction.Symbols.Types {
		fmt.Printf("  %s (%s) at lines %d-%d\n", t.Name, t.Type, t.StartLine, t.EndLine)
	}
}
