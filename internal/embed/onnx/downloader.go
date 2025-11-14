//go:build !rust_ffi

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
	"model.onnx",      // BGE ONNX model
	"tokenizer.json",  // HuggingFace tokenizer
	"config.json",     // Model config
}

// GetRuntimeLibName returns the platform-specific ONNX runtime library name.
// Exported so daemon can set the library path before initialization.
func GetRuntimeLibName() string {
	switch runtime.GOOS {
	case "darwin":
		return fmt.Sprintf("libonnxruntime.%s.dylib", onnxRuntimeVersion)
	case "windows":
		return fmt.Sprintf("onnxruntime.%s.dll", onnxRuntimeVersion)
	default:
		return fmt.Sprintf("libonnxruntime.so.%s", onnxRuntimeVersion)
	}
}

// getRuntimeLibName is kept for backwards compatibility within this package
func getRuntimeLibName() string {
	return GetRuntimeLibName()
}

// RuntimeExists checks if the ONNX runtime library exists
func RuntimeExists(libDir string) bool {
	runtimeLib := filepath.Join(libDir, getRuntimeLibName())
	_, err := os.Stat(runtimeLib)
	return err == nil
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

// newDownloaderWithBaseURL creates a Downloader with custom base URL (for testing)
func newDownloaderWithBaseURL(baseURL string) *Downloader {
	return &Downloader{
		baseURL: baseURL,
	}
}

// DownloadRuntime downloads platform-specific ONNX Runtime library from CDN
func (d *Downloader) DownloadRuntime(ctx context.Context, libDir string, progressCallback func(percent int)) error {
	// Detect platform
	platform := detectPlatform()

	// Build download URL
	url := fmt.Sprintf("%s/onnxruntime-v%s-%s.tar.gz", d.baseURL, onnxRuntimeVersion, platform)

	// Create temp file
	tmpFile, err := os.CreateTemp("", "onnxruntime-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	// Download with retries and progress
	if err := downloadWithRetry(ctx, url, tmpFile, progressCallback); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Close before extraction
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Extract tar.gz to lib directory
	if err := extractTarGz(tmpFile.Name(), libDir); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Verify runtime library exists
	if !RuntimeExists(libDir) {
		return fmt.Errorf("runtime library missing after extraction")
	}

	return nil
}

// DownloadEmbeddingModel downloads embedding model files from CDN
func (d *Downloader) DownloadEmbeddingModel(ctx context.Context, modelDir string, progressCallback func(percent int)) error {
	// Build download URL
	url := fmt.Sprintf("%s/bge-small-en-v%s.tar.gz", d.baseURL, bgeModelVersion)

	// Create temp file
	tmpFile, err := os.CreateTemp("", "bge-small-en-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	// Download with retries and progress
	if err := downloadWithRetry(ctx, url, tmpFile, progressCallback); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Close before extraction
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Extract tar.gz to models/bge directory
	bgeDir := filepath.Join(modelDir, "bge")
	if err := extractTarGz(tmpFile.Name(), bgeDir); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Verify all model files exist
	if !EmbeddingModelExists(modelDir) {
		return fmt.Errorf("model files missing after extraction")
	}

	return nil
}

// downloadWithRetry downloads a file with exponential backoff retry logic
func downloadWithRetry(ctx context.Context, url string, dest *os.File, progressCallback func(percent int)) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate exponential backoff
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * initialBackoff

			select {
			case <-time.After(backoff):
				// Continue with retry
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Attempt download
		err := downloadWithProgress(ctx, url, dest, progressCallback)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if context was cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Reset file position for retry
		if _, err := dest.Seek(0, 0); err != nil {
			return fmt.Errorf("failed to reset file position: %w", err)
		}
		if err := dest.Truncate(0); err != nil {
			return fmt.Errorf("failed to truncate file: %w", err)
		}
	}

	return fmt.Errorf("download failed after %d attempts: %w", maxRetries, lastErr)
}

// downloadWithProgress downloads a file and reports progress via callback
func downloadWithProgress(ctx context.Context, url string, dest *os.File, progressCallback func(percent int)) error {
	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	client := &http.Client{
		Timeout: 30 * time.Minute, // Large files may take time
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Get content length
	contentLength := resp.ContentLength
	if contentLength <= 0 {
		// Unknown content length, can't report progress accurately
		contentLength = 0
	}

	// Stream response body with progress tracking
	var bytesDownloaded int64
	buffer := make([]byte, 32*1024) // 32KB buffer
	lastPercent := -1

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := dest.Write(buffer[:n]); writeErr != nil {
				return fmt.Errorf("failed to write to file: %w", writeErr)
			}

			bytesDownloaded += int64(n)

			// Report progress
			if progressCallback != nil && contentLength > 0 {
				percent := int((float64(bytesDownloaded) / float64(contentLength)) * 100)
				if percent > 100 {
					percent = 100
				}
				if percent != lastPercent {
					progressCallback(percent)
					lastPercent = percent
				}
			}
		}

		if err != nil {
			if err == io.EOF {
				// Download complete
				if progressCallback != nil {
					progressCallback(100)
				}
				return nil
			}
			return fmt.Errorf("failed to read response: %w", err)
		}
	}
}

// extractTarGz extracts a tar.gz archive to the destination directory
func extractTarGz(tarPath, destDir string) error {
	// Create destination directory if needed
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Open archive file
	file, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	// Create gzip reader
	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	// Create tar reader
	tr := tar.NewReader(gzr)

	// Extract each file
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Construct destination path
		destPath := filepath.Join(destDir, header.Name)

		// Ensure the destination is within destDir (security check)
		if !filepath.HasPrefix(destPath, filepath.Clean(destDir)+string(os.PathSeparator)) &&
			destPath != filepath.Clean(destDir) {
			return fmt.Errorf("illegal file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}

		case tar.TypeReg:
			// Create parent directory if needed
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", destPath, err)
			}

			// Create file
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", destPath, err)
			}

			// Copy file contents
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to extract file %s: %w", destPath, err)
			}

			outFile.Close()

		default:
			// Skip other types (symlinks, etc.)
			continue
		}
	}

	return nil
}
