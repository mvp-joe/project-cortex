//go:build !rust_ffi

package onnx

import (
	"fmt"
	"path/filepath"

	"github.com/daulet/tokenizers"
	onnxruntime "github.com/yalue/onnxruntime_go"
)

// EmbeddingModel wraps ONNX Runtime for text embeddings.
// Thread-safe for inference (no mutex needed).
type EmbeddingModel struct {
	session   *onnxruntime.DynamicAdvancedSession
	tokenizer *tokenizers.Tokenizer
}

// NewEmbeddingModel creates a new embedding model from model directory.
// Expects:
//   - tokenizer.json in modelDir
//   - model.onnx in modelDir
// The model should be BAAI/bge-small-en-v1.5 (384 dimensions).
func NewEmbeddingModel(onnxPath, tokenizerPath string) (*EmbeddingModel, error) {
	// Load HuggingFace tokenizer (requires CGO via daulet/tokenizers)
	// If tokenizerPath is relative, it's assumed to be tokenizer.json in model dir
	if filepath.Base(tokenizerPath) != "tokenizer.json" {
		tokenizerPath = filepath.Join(filepath.Dir(onnxPath), "tokenizer.json")
	}

	tokenizer, err := tokenizers.FromFile(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load tokenizer: %w", err)
	}

	// Get model I/O info
	inputs, outputs, err := onnxruntime.GetInputOutputInfo(onnxPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get model info: %w", err)
	}

	// Extract input/output names
	inputNames := make([]string, len(inputs))
	outputNames := make([]string, len(outputs))
	for i := range inputs {
		inputNames[i] = inputs[i].Name
	}
	for i := range outputs {
		outputNames[i] = outputs[i].Name
	}

	// Create ONNX session with dynamic shapes
	session, err := onnxruntime.NewDynamicAdvancedSession(
		onnxPath,
		inputNames,
		outputNames,
		nil, // No options
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ONNX session: %w", err)
	}

	return &EmbeddingModel{
		session:   session,
		tokenizer: tokenizer,
	}, nil
}

const maxTokens = 512 // Maximum sequence length to prevent OOM and cache issues

// EmbedBatch generates embeddings for multiple texts in a single batch.
// Returns 384-dimensional embeddings (one per input text).
// Thread-safe: ONNX Runtime handles concurrency internally.
// Truncates sequences to maxTokens (512) to prevent performance degradation.
func (m *EmbeddingModel) EmbedBatch(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// Tokenize all texts with attention masks and token type IDs
	allTokens := make([][]int64, len(texts))
	allAttentionMasks := make([][]int64, len(texts))
	allTokenTypeIDs := make([][]int64, len(texts))
	maxLen := 0

	for i, text := range texts {
		// Encode with special tokens ([CLS], [SEP]) and return attention mask + type IDs
		encoding := m.tokenizer.EncodeWithOptions(text, true, // add special tokens
			tokenizers.WithReturnAttentionMask(),
			tokenizers.WithReturnTypeIDs(),
		)

		// Convert uint32 → int64 for ONNX
		tokenIDs := make([]int64, len(encoding.IDs))
		attentionMask := make([]int64, len(encoding.AttentionMask))
		tokenTypeIDs := make([]int64, len(encoding.TypeIDs))
		for j := range encoding.IDs {
			tokenIDs[j] = int64(encoding.IDs[j])
			attentionMask[j] = int64(encoding.AttentionMask[j])
			tokenTypeIDs[j] = int64(encoding.TypeIDs[j])
		}

		// Truncate to maxTokens
		if len(tokenIDs) > maxTokens {
			tokenIDs = tokenIDs[:maxTokens]
			attentionMask = attentionMask[:maxTokens]
			tokenTypeIDs = tokenTypeIDs[:maxTokens]
		}

		allTokens[i] = tokenIDs
		allAttentionMasks[i] = attentionMask
		allTokenTypeIDs[i] = tokenTypeIDs
		if len(tokenIDs) > maxLen {
			maxLen = len(tokenIDs)
		}
	}

	fmt.Printf("[BATCH] Processing %d texts, maxLen=%d tokens (max allowed: %d)\n", len(texts), maxLen, maxTokens)

	// Pad to maxLen
	batchSize := len(texts)
	inputIDs := make([]int64, batchSize*maxLen)
	attentionMaskFlat := make([]int64, batchSize*maxLen)
	tokenTypeIDsFlat := make([]int64, batchSize*maxLen)

	for i := range allTokens {
		for j := 0; j < maxLen; j++ {
			idx := i*maxLen + j
			if j < len(allTokens[i]) {
				inputIDs[idx] = allTokens[i][j]
				attentionMaskFlat[idx] = allAttentionMasks[i][j]
				tokenTypeIDsFlat[idx] = allTokenTypeIDs[i][j]
			} else {
				// PAD token ID (0 for BERT/BGE)
				inputIDs[idx] = 0
				attentionMaskFlat[idx] = 0
				tokenTypeIDsFlat[idx] = 0
			}
		}
	}

	// Create ONNX tensors
	inputShape := onnxruntime.NewShape(int64(batchSize), int64(maxLen))

	inputTensor, err := onnxruntime.NewTensor(inputShape, inputIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	attentionTensor, err := onnxruntime.NewTensor(inputShape, attentionMaskFlat)
	if err != nil {
		return nil, fmt.Errorf("failed to create attention tensor: %w", err)
	}
	defer attentionTensor.Destroy()

	tokenTypeTensor, err := onnxruntime.NewTensor(inputShape, tokenTypeIDsFlat)
	if err != nil {
		return nil, fmt.Errorf("failed to create token type tensor: %w", err)
	}
	defer tokenTypeTensor.Destroy()

	// Run inference (thread-safe)
	// BGE models have 3 inputs: input_ids, attention_mask, token_type_ids
	inputs := []onnxruntime.Value{inputTensor, attentionTensor, tokenTypeTensor}
	outputs := []onnxruntime.Value{nil} // Will be populated by Run()

	if err := m.session.Run(inputs, outputs); err != nil {
		return nil, fmt.Errorf("inference failed: %w", err)
	}

	// Extract embeddings via CLS token pooling
	// BGE outputs: last_hidden_state [batch_size, seq_length, 384]
	// We extract the CLS token (position 0) embedding from each sequence
	if outputs[0] == nil {
		return nil, fmt.Errorf("output tensor is nil")
	}

	resultTensor, ok := outputs[0].(*onnxruntime.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("unexpected output type, expected *Tensor[float32]")
	}
	defer resultTensor.Destroy()

	allEmbeddings := resultTensor.GetData()
	embeddingDim := 384 // BGE-small model dimension

	// Output shape is [batch_size, seq_length, 384]
	// We need to extract the CLS token (first token) embeddings
	// So we need to skip seq_length * 384 for each batch item
	result := make([][]float32, batchSize)

	for i := 0; i < batchSize; i++ {
		// CLS token is at position 0 for each sequence
		// Offset: i * maxLen * embeddingDim
		start := i * maxLen * embeddingDim
		end := start + embeddingDim

		if end > len(allEmbeddings) {
			return nil, fmt.Errorf("unexpected output size: batch %d, expected at least %d elements, got %d",
				i, end, len(allEmbeddings))
		}

		embedding := make([]float32, embeddingDim)
		copy(embedding, allEmbeddings[start:end])
		result[i] = embedding
	}

	return result, nil
}

// Destroy cleans up ONNX session and tokenizer resources.
// Should be called when the model is no longer needed.
func (m *EmbeddingModel) Destroy() error {
	// Close tokenizer first (Rust resources)
	if m.tokenizer != nil {
		m.tokenizer.Close()
	}

	// Then destroy ONNX session
	if m.session != nil {
		return m.session.Destroy()
	}
	return nil
}
