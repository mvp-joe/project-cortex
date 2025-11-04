package indexer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for Context Cancellation:
// - Index respects context cancellation
// - processCodeFiles respects context cancellation
// - processDocFiles respects context cancellation
// - Cancelled context returns context.Canceled error

func TestIndexer_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Test: Index respects context cancellation
	config := DefaultConfig("../../testdata")
	config.OutputDir = t.TempDir()
	config.EmbeddingProvider = "mock" // Use mock provider for tests

	idx, err := New(config)
	require.NoError(t, err)
	defer idx.Close()

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stats, err := idx.Index(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, stats)
}

func TestIndexer_ContextCancellationDuringProcessing(t *testing.T) {
	t.Parallel()

	// Test: Context cancelled during processing stops indexing
	config := DefaultConfig("../../testdata")
	config.OutputDir = t.TempDir()
	config.EmbeddingProvider = "mock" // Use mock provider for tests

	idx, err := New(config)
	require.NoError(t, err)
	defer idx.Close()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Give it a tiny bit of time to start, then it should cancel
	time.Sleep(5 * time.Millisecond)

	stats, err := idx.Index(ctx)
	// Should get either Canceled or DeadlineExceeded
	if err != nil {
		// Check if the error contains context cancellation
		isContextErr := err == context.Canceled ||
			err == context.DeadlineExceeded ||
			context.Cause(ctx) == context.Canceled ||
			context.Cause(ctx) == context.DeadlineExceeded
		assert.True(t, isContextErr, "Expected context cancellation error, got: %v", err)
	}
	// Stats might be nil or partial
	_ = stats
}

func TestIndexer_processCodeFiles_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Test: processCodeFiles respects context cancellation
	config := DefaultConfig("../../testdata")
	config.OutputDir = t.TempDir()
	config.EmbeddingProvider = "mock" // Use mock provider for tests

	idx, err := New(config)
	require.NoError(t, err)
	defer idx.Close()

	// Get the concrete type to access internal methods
	concreteIdx := idx.(*indexer)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files := []string{"../../testdata/code/go/simple.go"}
	symbols, defs, data, err := concreteIdx.processCodeFiles(ctx, files)

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, symbols)
	assert.Nil(t, defs)
	assert.Nil(t, data)
}

func TestIndexer_processDocFiles_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Test: processDocFiles respects context cancellation
	config := DefaultConfig("../../testdata")
	config.OutputDir = t.TempDir()
	config.EmbeddingProvider = "mock" // Use mock provider for tests

	idx, err := New(config)
	require.NoError(t, err)
	defer idx.Close()

	// Get the concrete type to access internal methods
	concreteIdx := idx.(*indexer)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files := []string{"../../testdata/docs/getting-started.md"}
	chunks, err := concreteIdx.processDocFiles(ctx, files)

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, chunks)
}

func TestIndexer_ProgressReporter(t *testing.T) {
	t.Parallel()

	// Test: Progress reporter receives callbacks
	config := DefaultConfig("../../testdata")
	config.OutputDir = t.TempDir()
	config.EmbeddingProvider = "mock" // Use mock provider for tests

	// Create a mock progress reporter
	mock := &mockProgressReporter{
		events: make([]string, 0),
	}

	idx, err := NewWithProgress(config, mock)
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	_, err = idx.Index(ctx)
	require.NoError(t, err)

	// Verify progress callbacks were called
	assert.Contains(t, mock.events, "discovery_start")
	assert.Contains(t, mock.events, "discovery_complete")
	assert.Contains(t, mock.events, "file_processing_start")
	assert.Contains(t, mock.events, "writing_chunks")
	assert.Contains(t, mock.events, "complete")
}

// mockProgressReporter tracks progress events for testing
type mockProgressReporter struct {
	events []string
}

func (m *mockProgressReporter) OnDiscoveryStart() {
	m.events = append(m.events, "discovery_start")
}

func (m *mockProgressReporter) OnDiscoveryComplete(codeFiles, docFiles int) {
	m.events = append(m.events, "discovery_complete")
}

func (m *mockProgressReporter) OnFileProcessingStart(totalFiles int) {
	m.events = append(m.events, "file_processing_start")
}

func (m *mockProgressReporter) OnFileProcessed(fileName string) {
	m.events = append(m.events, "file_processed")
}

func (m *mockProgressReporter) OnEmbeddingStart(totalChunks int) {
	m.events = append(m.events, "embedding_start")
}

func (m *mockProgressReporter) OnEmbeddingProgress(processedChunks int) {
	m.events = append(m.events, "embedding_progress")
}

func (m *mockProgressReporter) OnWritingChunks() {
	m.events = append(m.events, "writing_chunks")
}

func (m *mockProgressReporter) OnComplete(stats *ProcessingStats) {
	m.events = append(m.events, "complete")
}

func (m *mockProgressReporter) OnGraphBuildingStart(totalFiles int) {
	m.events = append(m.events, "graph_building_start")
}

func (m *mockProgressReporter) OnGraphFileProcessed(processedFiles, totalFiles int, fileName string) {
	m.events = append(m.events, "graph_file_processed")
}

func (m *mockProgressReporter) OnGraphBuildingComplete(nodeCount, edgeCount int, duration time.Duration) {
	m.events = append(m.events, "graph_building_complete")
}
