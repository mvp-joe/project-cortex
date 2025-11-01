package embed

import (
	"context"
	"fmt"
)

// BatchProgress reports embedding progress for real-time feedback.
type BatchProgress struct {
	BatchIndex      int // Current batch number (1-indexed)
	TotalBatches    int // Total number of batches
	ProcessedChunks int // Number of chunks processed so far
	TotalChunks     int // Total number of chunks to process
}

// EmbedWithProgress embeds texts in batches with progress feedback via channel.
//
// This function splits the input texts into batches and processes them sequentially,
// sending progress updates via the progressCh channel after each batch completes.
//
// Parameters:
//   - ctx: Context for cancellation
//   - provider: The embedding provider to use
//   - texts: Slice of text strings to embed
//   - mode: Embedding mode (query or passage)
//   - batchSize: Number of texts to process per batch (e.g., 50 for ~1.5s updates)
//   - progressCh: Channel for progress updates (can be nil to disable progress)
//
// Returns:
//   - Embeddings in the same order as input texts
//   - Error if any batch fails
//
// Example usage:
//
//	progressCh := make(chan embed.BatchProgress, 10)
//	go func() {
//	    for progress := range progressCh {
//	        fmt.Printf("Progress: %d/%d chunks\n", progress.ProcessedChunks, progress.TotalChunks)
//	    }
//	}()
//
//	embeddings, err := embed.EmbedWithProgress(ctx, provider, texts, embed.EmbedModePassage, 50, progressCh)
//	close(progressCh)
func EmbedWithProgress(
	ctx context.Context,
	provider Provider,
	texts []string,
	mode EmbedMode,
	batchSize int,
	progressCh chan<- BatchProgress,
) ([][]float32, error) {
	totalChunks := len(texts)
	if totalChunks == 0 {
		return [][]float32{}, nil
	}

	// Calculate number of batches
	numBatches := (totalChunks + batchSize - 1) / batchSize
	results := make([][]float32, totalChunks)

	// Process batches sequentially
	processedChunks := 0
	for batchIdx := 0; batchIdx < numBatches; batchIdx++ {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Calculate batch boundaries
		start := batchIdx * batchSize
		end := start + batchSize
		if end > totalChunks {
			end = totalChunks
		}

		batchTexts := texts[start:end]

		// Embed this batch
		batchEmbeddings, err := provider.Embed(ctx, batchTexts, mode)
		if err != nil {
			return nil, fmt.Errorf("batch %d/%d failed: %w", batchIdx+1, numBatches, err)
		}

		// Store results in correct position
		for i, emb := range batchEmbeddings {
			results[start+i] = emb
		}

		// Report progress
		processedChunks += len(batchTexts)
		if progressCh != nil {
			progressCh <- BatchProgress{
				BatchIndex:      batchIdx + 1,
				TotalBatches:    numBatches,
				ProcessedChunks: processedChunks,
				TotalChunks:     totalChunks,
			}
		}
	}

	return results, nil
}
