package onnx

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	// Version numbers
	onnxRuntimeVersion = "1.22.0"
	bgeModelVersion    = "1.5.0"

	// Base URL for CDN downloads
	defaultBaseURL = "https://project-cortex-files.t3.storage.dev"

	// Number of retry attempts for downloads
	maxRetries = 3

	// Initial backoff duration for retries
	initialBackoff = 1 * time.Second
)

// ModelFiles lists all required files that must exist after download
var ModelFiles = []string{
	"model.onnx",     // BGE ONNX model
	"tokenizer.json", // HuggingFace tokenizer
	"config.json",    // Model config
}

// EmbeddingModelExists checks if all required embedding model files exist
func EmbeddingModelExists(modelDir string) bool {
	// Check in bge subdirectory
	bgeDir := filepath.Join(modelDir, "bge")

	for _, file := range ModelFiles {
		path := filepath.Join(bgeDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return false
		}
	}

	return true
}

// detectPlatform returns the platform string for model downloads
func detectPlatform() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Map Go platform names to release names
	return fmt.Sprintf("%s-%s", goos, goarch)
}

// Downloader handles downloading ONNX runtime and embedding models
type Downloader struct {
	baseURL string
}

// NewDownloader creates a Downloader with the default CDN base URL
func NewDownloader() *Downloader {
	return &Downloader{
		baseURL: defaultBaseURL,
	}
}

// DownloadEmbeddingModel downloads the BGE embedding model with progress callback
func (d *Downloader) DownloadEmbeddingModel(ctx context.Context, modelDir string, progress func(percent int)) error {
	// Create models/bge subdirectory
	bgeDir := filepath.Join(modelDir, "bge")
	if err := os.MkdirAll(bgeDir, 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	// Construct download URL
	platform := detectPlatform()
	// Example: https://project-cortex-files.t3.storage.dev/bge-small-en-v1.5-1.5.0-darwin-arm64.tar.gz
	url := fmt.Sprintf("%s/bge-small-en-v%s-%s.tar.gz", d.baseURL, bgeModelVersion, platform)

	// Download with retries
	var lastErr error
	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := d.downloadWithProgress(ctx, url, bgeDir, progress)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Exponential backoff before retry
		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}
	}

	return fmt.Errorf("download failed after %d attempts: %w", maxRetries, lastErr)
}

// downloadWithProgress performs HTTP download with progress tracking
func (d *Downloader) downloadWithProgress(ctx context.Context, url, destDir string, progress func(percent int)) error {
	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	totalSize := resp.ContentLength

	// Wrap response body with progress tracking
	progressReader := &progressReader{
		reader:     resp.Body,
		totalSize:  totalSize,
		onProgress: progress,
	}

	// Extract tar.gz to destination
	if err := extractTarGz(progressReader, destDir); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Final progress update
	if progress != nil {
		progress(100)
	}

	return nil
}

// progressReader wraps io.Reader with progress callbacks
type progressReader struct {
	reader     io.Reader
	totalSize  int64
	readBytes  int64
	onProgress func(percent int)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.readBytes += int64(n)

	if pr.onProgress != nil && pr.totalSize > 0 {
		percent := int(math.Min(float64(pr.readBytes*100/pr.totalSize), 100))
		pr.onProgress(percent)
	}

	return n, err
}

// extractTarGz extracts a .tar.gz archive to the destination directory
func extractTarGz(reader io.Reader, destDir string) error {
	// Create gzip reader
	gzr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("gzip reader creation failed: %w", err)
	}
	defer gzr.Close()

	// Create tar reader
	tr := tar.NewReader(gzr)

	// Extract all files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return fmt.Errorf("tar read error: %w", err)
		}

		// Construct destination path
		target := filepath.Join(destDir, header.Name)

		// Check for directory traversal attacks
		if !filepath.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// Create file
			f, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			f.Close()
		}
	}

	return nil
}
