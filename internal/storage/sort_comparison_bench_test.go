package storage

import (
	"sort"
	"testing"
)

type testChunk struct {
	ID string
}

// Benchmark comparing sort algorithms for chunk sorting
func BenchmarkChunkSorting(b *testing.B) {
	// Setup test data
	makeTestData := func(n int) ([]*testChunk, map[string]float64) {
		chunks := make([]*testChunk, n)
		distanceMap := make(map[string]float64, n)

		for i := 0; i < n; i++ {
			id := "chunk" + string(rune(i))
			chunks[i] = &testChunk{ID: id}
			// Random-ish distances
			distanceMap[id] = float64(n-i) * 0.123
		}
		return chunks, distanceMap
	}

	benchSizes := []int{10, 50, 100}

	for _, size := range benchSizes {
		chunks, distanceMap := makeTestData(size)

		b.Run("SortSlice_"+string(rune(size)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// Make a copy for sorting
				testChunks := make([]*testChunk, len(chunks))
				copy(testChunks, chunks)

				sort.Slice(testChunks, func(i, j int) bool {
					return distanceMap[testChunks[i].ID] < distanceMap[testChunks[j].ID]
				})
			}
		})

		b.Run("BubbleSort_"+string(rune(size)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// Make a copy for sorting
				testChunks := make([]*testChunk, len(chunks))
				copy(testChunks, chunks)

				// Bubble sort
				for i := 0; i < len(testChunks)-1; i++ {
					for j := 0; j < len(testChunks)-i-1; j++ {
						if distanceMap[testChunks[j].ID] > distanceMap[testChunks[j+1].ID] {
							testChunks[j], testChunks[j+1] = testChunks[j+1], testChunks[j]
						}
					}
				}
			}
		})
	}
}
