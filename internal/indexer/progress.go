package indexer

import "time"

// ProgressReporter provides callbacks for reporting indexing progress.
// Implementations can display progress bars, log messages, or remain silent.
type ProgressReporter interface {
	// OnDiscoveryStart is called when file discovery begins.
	OnDiscoveryStart()

	// OnDiscoveryComplete is called when file discovery finishes.
	OnDiscoveryComplete(codeFiles, docFiles int)

	// OnFileProcessingStart is called before processing files.
	OnFileProcessingStart(totalFiles int)

	// OnFileProcessed is called after each file is processed.
	OnFileProcessed(fileName string)

	// OnEmbeddingStart is called before generating embeddings.
	OnEmbeddingStart(totalChunks int)

	// OnEmbeddingProgress is called after each batch of embeddings.
	OnEmbeddingProgress(processedChunks int)

	// OnWritingChunks is called when writing chunk files begins.
	OnWritingChunks()

	// OnComplete is called when indexing completes successfully.
	OnComplete(stats *ProcessingStats)

	// Graph building progress
	OnGraphBuildingStart(totalFiles int)
	OnGraphFileProcessed(processedFiles, totalFiles int, fileName string)
	OnGraphBuildingComplete(nodeCount, edgeCount int, duration time.Duration)
}

// NoOpProgressReporter is a progress reporter that does nothing.
// Used when progress reporting is disabled (e.g., --quiet flag).
type NoOpProgressReporter struct{}

func (n *NoOpProgressReporter) OnDiscoveryStart()                           {}
func (n *NoOpProgressReporter) OnDiscoveryComplete(codeFiles, docFiles int) {}
func (n *NoOpProgressReporter) OnFileProcessingStart(totalFiles int)        {}
func (n *NoOpProgressReporter) OnFileProcessed(fileName string)             {}
func (n *NoOpProgressReporter) OnEmbeddingStart(totalChunks int)            {}
func (n *NoOpProgressReporter) OnEmbeddingProgress(processedChunks int)     {}
func (n *NoOpProgressReporter) OnWritingChunks()                            {}
func (n *NoOpProgressReporter) OnComplete(stats *ProcessingStats)           {}
func (n *NoOpProgressReporter) OnGraphBuildingStart(totalFiles int)         {}
func (n *NoOpProgressReporter) OnGraphFileProcessed(processedFiles, totalFiles int, fileName string) {
}
func (n *NoOpProgressReporter) OnGraphBuildingComplete(nodeCount, edgeCount int, duration time.Duration) {
}
