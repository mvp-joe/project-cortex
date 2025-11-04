package storage

import (
	"sort"
	"testing"
)

// BenchmarkSortChunksByDistance compares bubble sort vs sort.Slice
func BenchmarkSortChunksByDistance(b *testing.B) {
	// Setup test data
	makeTestData := func(n int) ([]*Chunk, map[string]float64) {
		chunks := make([]*Chunk, n)
		distanceMap := make(map[string]float64, n)

		for i := 0; i < n; i++ {
			id := "chunk" + string(rune(i))
			chunks[i] = &Chunk{ID: id}
			// Random-ish distances
			distanceMap[id] = float64(n-i) * 0.123
		}
		return chunks, distanceMap
	}

	b.Run("SortSlice_O(n_log_n)_10", func(b *testing.B) {
		chunks, distanceMap := makeTestData(10)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Make a copy for sorting
			testChunks := make([]*Chunk, len(chunks))
			copy(testChunks, chunks)

			sort.Slice(testChunks, func(i, j int) bool {
				return distanceMap[testChunks[i].ID] < distanceMap[testChunks[j].ID]
			})
		}
	})

	b.Run("BubbleSort_O(n²)_10", func(b *testing.B) {
		chunks, distanceMap := makeTestData(10)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Make a copy for sorting
			testChunks := make([]*Chunk, len(chunks))
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

	b.Run("SortSlice_O(n_log_n)_100", func(b *testing.B) {
		chunks, distanceMap := makeTestData(100)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			testChunks := make([]*Chunk, len(chunks))
			copy(testChunks, chunks)

			sort.Slice(testChunks, func(i, j int) bool {
				return distanceMap[testChunks[i].ID] < distanceMap[testChunks[j].ID]
			})
		}
	})

	b.Run("BubbleSort_O(n²)_100", func(b *testing.B) {
		chunks, distanceMap := makeTestData(100)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			testChunks := make([]*Chunk, len(chunks))
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
