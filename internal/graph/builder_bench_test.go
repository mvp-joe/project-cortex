package graph

import (
	"testing"
)

// BenchmarkEdgeFiltering tests the performance of edge filtering with different approaches
func BenchmarkEdgeFiltering(b *testing.B) {
	// Setup test data
	changedStructIDs := make([]string, 100)
	changedInterfaceIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		changedStructIDs[i] = "struct" + string(rune(i))
		changedInterfaceIDs[i] = "interface" + string(rune(i))
	}

	// Create 1000 edges
	allEdges := make([]Edge, 1000)
	for i := 0; i < 1000; i++ {
		allEdges[i] = Edge{
			Type: EdgeImplements,
			From: "struct" + string(rune(i%150)), // Some match, some don't
			To:   "interface" + string(rune(i%150)),
		}
	}

	b.Run("MapBased_O(n)", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Build sets for O(1) lookup
			changedStructSet := make(map[string]bool, len(changedStructIDs))
			for _, id := range changedStructIDs {
				changedStructSet[id] = true
			}

			changedInterfaceSet := make(map[string]bool, len(changedInterfaceIDs))
			for _, id := range changedInterfaceIDs {
				changedInterfaceSet[id] = true
			}

			// Filter edges - O(n)
			filteredEdges := make([]Edge, 0, len(allEdges))
			for _, edge := range allEdges {
				if edge.Type == EdgeImplements {
					if changedStructSet[edge.From] || changedInterfaceSet[edge.To] {
						continue
					}
				}
				filteredEdges = append(filteredEdges, edge)
			}
		}
	})

	b.Run("NestedLoop_O(n²)", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Filter edges - O(n²)
			filteredEdges := make([]Edge, 0, len(allEdges))
			for _, edge := range allEdges {
				if edge.Type == EdgeImplements {
					isChangedStruct := false
					for _, id := range changedStructIDs {
						if edge.From == id {
							isChangedStruct = true
							break
						}
					}
					isChangedInterface := false
					for _, id := range changedInterfaceIDs {
						if edge.To == id {
							isChangedInterface = true
							break
						}
					}

					if isChangedStruct || isChangedInterface {
						continue
					}
				}
				filteredEdges = append(filteredEdges, edge)
			}
		}
	})
}
