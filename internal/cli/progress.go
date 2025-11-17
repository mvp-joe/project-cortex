package cli

import (
	"fmt"
	"log"
	"time"

	"github.com/mvp-joe/project-cortex/internal/indexer"
	"github.com/schollz/progressbar/v3"
)

// CLIProgressReporter implements progress reporting with progress bars.
type CLIProgressReporter struct {
	quiet               bool
	fileBar             *progressbar.ProgressBar
	embeddingBar        *progressbar.ProgressBar
	graphBar            *progressbar.ProgressBar
	startTime           time.Time
	totalFiles          int
	processedFiles      int
	totalEmbeddings     int
	processedEmbeddings int
}

// NewCLIProgressReporter creates a new CLI progress reporter.
func NewCLIProgressReporter(quiet bool) *CLIProgressReporter {
	return &CLIProgressReporter{
		quiet:     quiet,
		startTime: time.Now(),
	}
}

func (c *CLIProgressReporter) OnDiscoveryStart() {
	if c.quiet {
		return
	}
	log.Println("Discovering files...")
}

func (c *CLIProgressReporter) OnDiscoveryComplete(codeFiles, docFiles int) {
	if c.quiet {
		return
	}
	log.Printf("Processing %d code files and %d documentation files\n", codeFiles, docFiles)
	fmt.Println()
}

func (c *CLIProgressReporter) OnFileProcessingStart(totalFiles int) {
	if c.quiet {
		return
	}
	c.totalFiles = totalFiles
	c.processedFiles = 0

	c.fileBar = progressbar.NewOptions(totalFiles,
		progressbar.OptionSetDescription("Indexing files"),
		progressbar.OptionSetWidth(40),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("files/s"),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
	)
}

func (c *CLIProgressReporter) OnFileProcessed(fileName string) {
	if c.quiet {
		return
	}
	if c.fileBar != nil {
		c.processedFiles++
		c.fileBar.Add(1)
	}
}

func (c *CLIProgressReporter) OnEmbeddingStart(totalChunks int) {
	if c.quiet {
		return
	}
	c.totalEmbeddings = totalChunks
	c.processedEmbeddings = 0

	c.embeddingBar = progressbar.NewOptions(totalChunks,
		progressbar.OptionSetDescription("Generating embeddings"),
		progressbar.OptionSetWidth(40),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("emb/s"),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
	)
}

func (c *CLIProgressReporter) OnEmbeddingProgress(processedChunks int) {
	if c.quiet {
		return
	}
	if c.embeddingBar != nil {
		delta := processedChunks - c.processedEmbeddings
		if delta > 0 {
			c.embeddingBar.Add(delta)
			c.processedEmbeddings = processedChunks
		}
	}
}

func (c *CLIProgressReporter) OnWritingChunks() {
	if c.quiet {
		return
	}
	log.Println("Writing chunk files...")
}

func (c *CLIProgressReporter) OnComplete(stats *indexer.ProcessingStats) {
	if c.quiet {
		return
	}

	fmt.Println()
	fmt.Printf("✓ Indexing complete: %s chunks in %.1fs\n",
		formatNumber(stats.TotalCodeChunks+stats.TotalDocChunks),
		stats.ProcessingTimeSeconds)
	fmt.Printf("  Code chunks: %s\n", formatNumber(stats.TotalCodeChunks))
	fmt.Printf("  Doc chunks:  %s\n", formatNumber(stats.TotalDocChunks))
}

func (c *CLIProgressReporter) OnGraphBuildingStart(totalFiles int) {
	if c.quiet {
		return
	}
	// Finish any existing progress bar
	if c.graphBar != nil {
		c.graphBar.Finish()
	}
	c.graphBar = progressbar.NewOptions(totalFiles,
		progressbar.OptionSetDescription("Building code graph"),
		progressbar.OptionSetWidth(40),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("files/s"),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
	)
}

func (c *CLIProgressReporter) OnGraphFileProcessed(processedFiles, totalFiles int, fileName string) {
	if c.quiet {
		return
	}
	if c.graphBar != nil {
		c.graphBar.Add(1)
	}
}

func (c *CLIProgressReporter) OnGraphBuildingComplete(nodeCount, edgeCount int, duration time.Duration) {
	if c.quiet {
		return
	}
	if c.graphBar != nil {
		c.graphBar.Finish()
		c.graphBar = nil
	}
	fmt.Printf("✓ Graph built: %s nodes, %s edges (took %.1fs)\n",
		formatNumber(nodeCount), formatNumber(edgeCount), duration.Seconds())
}

// formatNumber is defined in indexer_status.go and reused here
