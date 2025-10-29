package graph

import (
	"log"
)

// InterfaceMatcher handles interface implementation inference via signature matching.
//
// Limitations of Signature-Based Matching:
//
// This implementation uses AST-based signature matching without full type checking.
// The following scenarios may produce false positives or negatives:
//
//   - Type aliases: Methods with `type MyString string` parameters won't match
//     methods expecting `string`, even though they're compatible at runtime.
//
//   - Generic type constraints: Interface bounds on type parameters cannot be
//     verified without analyzing the constraint satisfaction.
//
//   - Method expressions vs method values: The matcher cannot distinguish between
//     these different ways of referencing methods.
//
//   - Vendored packages: Multiple copies of the same package in different vendor
//     directories will be treated as distinct types, even if they're identical.
//
// These limitations are inherent to the signature-based approach and represent
// trade-offs for performance and simplicity.
type InterfaceMatcher struct {
	nodes map[string]*Node // All nodes indexed by ID
}

// NewInterfaceMatcher creates a new interface matcher.
func NewInterfaceMatcher(nodes []Node) *InterfaceMatcher {
	nodeMap := make(map[string]*Node)
	for i := range nodes {
		nodeMap[nodes[i].ID] = &nodes[i]
	}

	return &InterfaceMatcher{
		nodes: nodeMap,
	}
}

// ResolveEmbeddings flattens all embedded interfaces recursively.
// This populates the ResolvedMethods field for all interface nodes.
func (m *InterfaceMatcher) ResolveEmbeddings() {
	for _, node := range m.nodes {
		if node.Kind != NodeInterface {
			continue
		}

		visited := make(map[string]bool)
		node.ResolvedMethods = m.flattenMethods(node, visited)
	}
}

// flattenMethods recursively flattens embedded interfaces to get the complete method set.
func (m *InterfaceMatcher) flattenMethods(node *Node, visited map[string]bool) []MethodSignature {
	if visited[node.ID] {
		// Cycle detected, skip
		return nil
	}
	visited[node.ID] = true

	// Start with node's own methods
	methods := make([]MethodSignature, len(node.Methods))
	copy(methods, node.Methods)

	// Recursively add methods from embedded interfaces
	for _, embeddedID := range node.EmbeddedTypes {
		embeddedNode, exists := m.nodes[embeddedID]
		if !exists {
			log.Printf("Warning: embedded interface %s not found (referenced by %s)", embeddedID, node.ID)
			continue
		}

		if embeddedNode.Kind != NodeInterface {
			// Embedded type is not an interface (could be struct), skip
			continue
		}

		// Recursively flatten the embedded interface
		embeddedMethods := m.flattenMethods(embeddedNode, visited)
		methods = append(methods, embeddedMethods...)
	}

	return methods
}

// InferImplementations finds all struct->interface implementation relationships.
// Returns a list of "implements" edges.
func (m *InterfaceMatcher) InferImplementations() []Edge {
	var edges []Edge

	// Get all interfaces and structs
	var interfaces []*Node
	var structs []*Node

	for _, node := range m.nodes {
		switch node.Kind {
		case NodeInterface:
			interfaces = append(interfaces, node)
		case NodeStruct:
			structs = append(structs, node)
		}
	}

	// Check each struct against each interface
	for _, strct := range structs {
		for _, iface := range interfaces {
			if m.implementsInterface(strct, iface) {
				edges = append(edges, Edge{
					From: strct.ID,
					To:   iface.ID,
					Type: EdgeImplements,
					Location: &Location{
						File: strct.File,
						Line: strct.StartLine,
					},
				})
			}
		}
	}

	return edges
}

// implementsInterface checks if a struct implements an interface via signature matching.
func (m *InterfaceMatcher) implementsInterface(strct, iface *Node) bool {
	// Use resolved methods (includes embedded interfaces)
	ifaceMethods := iface.ResolvedMethods
	if len(ifaceMethods) == 0 {
		// Empty interface: interface{} - all types implement it
		return true
	}

	// Build index of struct methods by name for quick lookup
	structMethodsByName := make(map[string]MethodSignature)
	for _, method := range strct.Methods {
		structMethodsByName[method.Name] = method
	}

	// Check if struct has all required interface methods
	for _, ifaceMethod := range ifaceMethods {
		structMethod, exists := structMethodsByName[ifaceMethod.Name]
		if !exists {
			return false
		}

		if !m.signaturesEqual(structMethod, ifaceMethod) {
			return false
		}
	}

	return true
}

// signaturesEqual compares two method signatures for equality.
func (m *InterfaceMatcher) signaturesEqual(a, b MethodSignature) bool {
	// Check parameter count
	if len(a.Parameters) != len(b.Parameters) {
		return false
	}

	// Check return count
	if len(a.Returns) != len(b.Returns) {
		return false
	}

	// Compare parameter types
	for i := range a.Parameters {
		if !m.typeRefsEqual(a.Parameters[i].Type, b.Parameters[i].Type) {
			return false
		}
	}

	// Compare return types
	for i := range a.Returns {
		if !m.typeRefsEqual(a.Returns[i].Type, b.Returns[i].Type) {
			return false
		}
	}

	return true
}

// typeRefsEqual compares two type references for equality.
func (m *InterfaceMatcher) typeRefsEqual(a, b TypeRef) bool {
	return a.Name == b.Name &&
		a.Package == b.Package &&
		a.IsPointer == b.IsPointer &&
		a.IsSlice == b.IsSlice &&
		a.IsMap == b.IsMap
}

// InferImplementationsIncremental re-infers implementations only for changed entities.
// This is used during incremental updates to avoid re-checking all implementations.
func (m *InterfaceMatcher) InferImplementationsIncremental(changedInterfaceIDs, changedStructIDs []string) []Edge {
	var edges []Edge

	// Get changed interfaces and structs
	var changedInterfaces []*Node
	var changedStructs []*Node
	var allStructs []*Node

	for _, id := range changedInterfaceIDs {
		if node, exists := m.nodes[id]; exists && node.Kind == NodeInterface {
			changedInterfaces = append(changedInterfaces, node)
		}
	}

	for _, id := range changedStructIDs {
		if node, exists := m.nodes[id]; exists && node.Kind == NodeStruct {
			changedStructs = append(changedStructs, node)
		}
	}

	// Get all structs for interface changes
	for _, node := range m.nodes {
		if node.Kind == NodeStruct {
			allStructs = append(allStructs, node)
		}
	}

	// When an interface changes, check all structs against it
	for _, iface := range changedInterfaces {
		for _, strct := range allStructs {
			if m.implementsInterface(strct, iface) {
				edges = append(edges, Edge{
					From: strct.ID,
					To:   iface.ID,
					Type: EdgeImplements,
					Location: &Location{
						File: strct.File,
						Line: strct.StartLine,
					},
				})
			}
		}
	}

	// When a struct changes, check it against all interfaces
	var allInterfaces []*Node
	for _, node := range m.nodes {
		if node.Kind == NodeInterface {
			allInterfaces = append(allInterfaces, node)
		}
	}

	for _, strct := range changedStructs {
		for _, iface := range allInterfaces {
			if m.implementsInterface(strct, iface) {
				edges = append(edges, Edge{
					From: strct.ID,
					To:   iface.ID,
					Type: EdgeImplements,
					Location: &Location{
						File: strct.File,
						Line: strct.StartLine,
					},
				})
			}
		}
	}

	return edges
}
