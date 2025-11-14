package onnx

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getModelPaths returns paths to ONNX models if available.
// Returns empty strings if models not downloaded.
func getModelPaths() (onnxPath, tokenizerPath string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}

	modelDir := filepath.Join(homeDir, ".cortex", "onnx")
	onnxPath = filepath.Join(modelDir, "model_q4.onnx")
	tokenizerPath = filepath.Join(modelDir, "tokenizer.model")

	// Check if both files exist
	if _, err := os.Stat(onnxPath); os.IsNotExist(err) {
		return "", ""
	}
	if _, err := os.Stat(tokenizerPath); os.IsNotExist(err) {
		return "", ""
	}

	return onnxPath, tokenizerPath
}

func TestNewEmbeddingModel(t *testing.T) {
	t.Parallel()

	onnxPath, tokenizerPath := getModelPaths()
	if onnxPath == "" || tokenizerPath == "" {
		t.Skip("ONNX models not downloaded, skipping test")
	}

	t.Run("ValidPaths", func(t *testing.T) {
		model, err := NewEmbeddingModel(onnxPath, tokenizerPath)
		require.NoError(t, err)
		require.NotNil(t, model)
		require.NotNil(t, model.session)
		require.NotNil(t, model.tokenizer)

		// Cleanup
		err = model.Destroy()
		assert.NoError(t, err)
	})

	t.Run("InvalidONNXPath", func(t *testing.T) {
		model, err := NewEmbeddingModel("/nonexistent/model.onnx", tokenizerPath)
		assert.Error(t, err)
		assert.Nil(t, model)
	})

	t.Run("InvalidTokenizerPath", func(t *testing.T) {
		model, err := NewEmbeddingModel(onnxPath, "/nonexistent/tokenizer.model")
		assert.Error(t, err)
		assert.Nil(t, model)
	})
}

func TestEmbedBatch_Single(t *testing.T) {
	t.Parallel()

	onnxPath, tokenizerPath := getModelPaths()
	if onnxPath == "" || tokenizerPath == "" {
		t.Skip("ONNX models not downloaded, skipping test")
	}

	model, err := NewEmbeddingModel(onnxPath, tokenizerPath)
	require.NoError(t, err)
	defer model.Destroy()

	texts := []string{"Hello, world!"}
	embeddings, err := model.EmbedBatch(texts)

	require.NoError(t, err)
	require.Len(t, embeddings, 1)
	require.Len(t, embeddings[0], 768, "Expected 768-dimensional embedding")

	// Verify embedding is normalized (L2 norm ~ 1.0)
	norm := computeL2Norm(embeddings[0])
	assert.InDelta(t, 1.0, norm, 0.01, "Embedding should be approximately normalized")

	// Verify embedding contains non-zero values
	hasNonZero := false
	for _, v := range embeddings[0] {
		if v != 0 {
			hasNonZero = true
			break
		}
	}
	assert.True(t, hasNonZero, "Embedding should contain non-zero values")
}

func TestEmbedBatch_Multiple(t *testing.T) {
	t.Parallel()

	onnxPath, tokenizerPath := getModelPaths()
	if onnxPath == "" || tokenizerPath == "" {
		t.Skip("ONNX models not downloaded, skipping test")
	}

	model, err := NewEmbeddingModel(onnxPath, tokenizerPath)
	require.NoError(t, err)
	defer model.Destroy()

	texts := []string{
		"The quick brown fox jumps over the lazy dog",
		"Machine learning",
		"Embedding models convert text to vectors",
	}

	embeddings, err := model.EmbedBatch(texts)

	require.NoError(t, err)
	require.Len(t, embeddings, 3)

	for i, emb := range embeddings {
		require.Len(t, emb, 768, "Text %d: expected 768-dimensional embedding", i)

		// Verify normalization
		norm := computeL2Norm(emb)
		assert.InDelta(t, 1.0, norm, 0.01, "Text %d: embedding should be normalized", i)
	}

	// Verify different texts produce different embeddings
	similarity01 := cosineSimilarity(embeddings[0], embeddings[1])
	similarity12 := cosineSimilarity(embeddings[1], embeddings[2])
	similarity02 := cosineSimilarity(embeddings[0], embeddings[2])

	// Embeddings should not be identical (cosine similarity < 1.0)
	assert.Less(t, similarity01, 0.99, "Different texts should have distinct embeddings")
	assert.Less(t, similarity12, 0.99, "Different texts should have distinct embeddings")
	assert.Less(t, similarity02, 0.99, "Different texts should have distinct embeddings")
}

func TestEmbedBatch_EmptyText(t *testing.T) {
	t.Parallel()

	onnxPath, tokenizerPath := getModelPaths()
	if onnxPath == "" || tokenizerPath == "" {
		t.Skip("ONNX models not downloaded, skipping test")
	}

	model, err := NewEmbeddingModel(onnxPath, tokenizerPath)
	require.NoError(t, err)
	defer model.Destroy()

	// Empty string edge case
	texts := []string{""}
	embeddings, err := model.EmbedBatch(texts)

	require.NoError(t, err)
	require.Len(t, embeddings, 1)
	require.Len(t, embeddings[0], 768)
}

func TestEmbedBatch_EmptySlice(t *testing.T) {
	t.Parallel()

	onnxPath, tokenizerPath := getModelPaths()
	if onnxPath == "" || tokenizerPath == "" {
		t.Skip("ONNX models not downloaded, skipping test")
	}

	model, err := NewEmbeddingModel(onnxPath, tokenizerPath)
	require.NoError(t, err)
	defer model.Destroy()

	// Empty slice edge case
	texts := []string{}
	embeddings, err := model.EmbedBatch(texts)

	require.NoError(t, err)
	require.Len(t, embeddings, 0)
}

func TestTruncateEmbedding(t *testing.T) {
	t.Parallel()

	// Create a test embedding (768d) - start at 1 to avoid zero
	original := make([]float32, 768)
	for i := range original {
		original[i] = float32(i+1) / 768.0 // Simple pattern, avoid zero at index 0
	}

	t.Run("TruncateTo512", func(t *testing.T) {
		truncated := TruncateEmbedding(original, 512)

		require.Len(t, truncated, 512)

		// Verify normalization
		norm := computeL2Norm(truncated)
		assert.InDelta(t, 1.0, norm, 0.001, "Truncated embedding should be normalized")

		// Verify contains non-zero values
		hasNonZero := false
		for _, v := range truncated {
			if v != 0 {
				hasNonZero = true
				break
			}
		}
		assert.True(t, hasNonZero, "Should contain non-zero values")
	})

	t.Run("TruncateTo256", func(t *testing.T) {
		truncated := TruncateEmbedding(original, 256)

		require.Len(t, truncated, 256)

		// Verify normalization
		norm := computeL2Norm(truncated)
		assert.InDelta(t, 1.0, norm, 0.001, "Truncated embedding should be normalized")
	})

	t.Run("TruncateTo128", func(t *testing.T) {
		truncated := TruncateEmbedding(original, 128)

		require.Len(t, truncated, 128)

		// Verify normalization
		norm := computeL2Norm(truncated)
		assert.InDelta(t, 1.0, norm, 0.001, "Truncated embedding should be normalized")
	})
}

func TestTruncateEmbedding_NoTruncation(t *testing.T) {
	t.Parallel()

	original := make([]float32, 768)
	for i := range original {
		original[i] = float32(i+1) / 768.0
	}

	t.Run("TargetDimEqualToLength", func(t *testing.T) {
		truncated := TruncateEmbedding(original, 768)

		// Should return original (no truncation)
		assert.Equal(t, original, truncated)
	})

	t.Run("TargetDimGreaterThanLength", func(t *testing.T) {
		truncated := TruncateEmbedding(original, 1024)

		// Should return original (no truncation)
		assert.Equal(t, original, truncated)
	})
}

func TestTruncateEmbedding_ZeroVector(t *testing.T) {
	t.Parallel()

	// Zero vector edge case
	original := make([]float32, 768)

	truncated := TruncateEmbedding(original, 512)

	require.Len(t, truncated, 512)

	// Should handle zero vector without panic
	for _, v := range truncated {
		assert.Equal(t, float32(0), v, "Zero vector should remain zero")
	}
}

func TestDestroy(t *testing.T) {
	t.Parallel()

	onnxPath, tokenizerPath := getModelPaths()
	if onnxPath == "" || tokenizerPath == "" {
		t.Skip("ONNX models not downloaded, skipping test")
	}

	model, err := NewEmbeddingModel(onnxPath, tokenizerPath)
	require.NoError(t, err)

	// Destroy should not panic
	err = model.Destroy()
	assert.NoError(t, err)

	// Second destroy should be safe (idempotent)
	err = model.Destroy()
	assert.NoError(t, err)
}

func TestDestroy_NilSession(t *testing.T) {
	t.Parallel()

	// Edge case: model with nil session
	model := &EmbeddingModel{
		session:   nil,
		tokenizer: nil,
	}

	// Should not panic
	err := model.Destroy()
	assert.NoError(t, err)
}

// Helper: Compute L2 norm of a vector
func computeL2Norm(vec []float32) float64 {
	var sum float64
	for _, v := range vec {
		sum += float64(v * v)
	}
	return math.Sqrt(sum)
}

// Helper: Compute cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}

	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (normA * normB)
}
