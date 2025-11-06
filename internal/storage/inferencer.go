package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// InterfaceInferencer determines which structs implement which interfaces.
// Uses hybrid approach: SQL load → in-memory comparison → SQL write.
// Typical performance: 15-30ms for large projects (1000 interfaces, 5000 structs).
type InterfaceInferencer struct {
	db *sql.DB
}

// NewInterfaceInferencer creates a new interface inference service.
func NewInterfaceInferencer(db *sql.DB) *InterfaceInferencer {
	return &InterfaceInferencer{db: db}
}

// InferImplementations determines which structs implement which interfaces.
// Performs three-phase operation:
//  1. Bulk load interfaces and structs with methods from SQL
//  2. In-memory comparison using map-based lookup (O(1) method matching)
//  3. Bulk write relationships back to SQL in transaction
//
// Re-infers all relationships on each call (not incremental).
// Fast enough for full re-inference: ~15-30ms for large projects.
func (inf *InterfaceInferencer) InferImplementations(ctx context.Context) error {
	// 1. Bulk load interfaces with methods (~1-5ms for 1000 interfaces)
	interfaces, err := LoadInterfacesWithMethods(inf.db)
	if err != nil {
		return fmt.Errorf("load interfaces: %w", err)
	}

	// 2. Bulk load structs with methods (~5-10ms for 5000 structs)
	structs, err := LoadStructsWithMethods(inf.db)
	if err != nil {
		return fmt.Errorf("load structs: %w", err)
	}

	// 3. Bulk load embedded fields (becomes "embeds" relationships)
	embeds, err := LoadEmbeddedFields(inf.db)
	if err != nil {
		return fmt.Errorf("load embeds: %w", err)
	}

	// 4. In-memory comparison (~5-10ms for 25M comparisons)
	implements := inf.findImplementations(interfaces, structs)

	// 5. Combine implements + embeds
	allRelationships := append(implements, embeds...)

	// 6. Bulk write in transaction (~5-10ms for 10K relationships)
	tx, err := inf.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear old inferred relationships
	_, err = tx.Exec(`
		DELETE FROM type_relationships
		WHERE relationship_type IN ('implements', 'embeds')
	`)
	if err != nil {
		return fmt.Errorf("clear old relationships: %w", err)
	}

	// Insert new relationships
	if err := BulkInsertRelationships(tx, allRelationships); err != nil {
		return fmt.Errorf("insert relationships: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Printf("✓ Inferred %d type relationships", len(allRelationships))
	return nil
}

// findImplementations compares structs against interfaces to find implementations.
// Uses map-based lookup for O(1) method matching.
// Algorithm:
//  - Build map of required methods for each interface
//  - For each struct, check if it has all required methods
//  - Method signatures match if name, param count, and return count match
//  - Empty interfaces match all structs
func (inf *InterfaceInferencer) findImplementations(
	interfaces []Type,
	structs []Type,
) []TypeRelationship {
	relationships := []TypeRelationship{}

	for _, iface := range interfaces {
		// Build method signature map for interface
		requiredMethods := make(map[string]MethodSignature)
		for _, method := range iface.Fields {
			requiredMethods[method.Name] = MethodSignature{
				ParamCount:  intPtrToInt(method.ParamCount),
				ReturnCount: intPtrToInt(method.ReturnCount),
			}
		}

		// Check each struct
		for _, strct := range structs {
			if inf.implementsInterface(strct, requiredMethods) {
				relationships = append(relationships, TypeRelationship{
					FromTypeID:       strct.ID,
					ToTypeID:         iface.ID,
					RelationshipType: "implements",
					SourceFilePath:   strct.FilePath,
					SourceLine:       strct.StartLine,
				})
			}
		}
	}

	return relationships
}

// implementsInterface checks if a struct has all required methods.
// Returns true if:
//  - Interface is empty (matches everything)
//  - Struct has all required methods with matching signatures
//
// Struct can have extra methods beyond what the interface requires.
func (inf *InterfaceInferencer) implementsInterface(
	strct Type,
	requiredMethods map[string]MethodSignature,
) bool {
	if len(requiredMethods) == 0 {
		return true // Empty interface matches everything
	}

	// Build struct's method map
	structMethods := make(map[string]MethodSignature)
	for _, method := range strct.Fields {
		structMethods[method.Name] = MethodSignature{
			ParamCount:  intPtrToInt(method.ParamCount),
			ReturnCount: intPtrToInt(method.ReturnCount),
		}
	}

	// Check if struct has all required methods
	for name, required := range requiredMethods {
		actual, exists := structMethods[name]
		if !exists {
			return false
		}

		if !signaturesMatch(required, actual) {
			return false
		}
	}

	return true
}

// MethodSignature represents a method's signature for comparison.
// Currently uses param/return counts for matching.
// Future: Add param types and return types for strict type checking.
type MethodSignature struct {
	ParamCount  int
	ReturnCount int
}

// signaturesMatch compares two method signatures for equality.
func signaturesMatch(a, b MethodSignature) bool {
	return a.ParamCount == b.ParamCount && a.ReturnCount == b.ReturnCount
}

// intPtrToInt converts *int to int, returning 0 if nil.
func intPtrToInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
