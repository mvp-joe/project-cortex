package onnx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectPlatform(t *testing.T) {
	t.Parallel()

	platform := detectPlatform()

	// Should match format: os-arch
	assert.Contains(t, platform, "-")

	// Should contain valid OS
	goos := runtime.GOOS
	assert.Contains(t, platform, goos)

	// Should contain valid arch
	goarch := runtime.GOARCH
	assert.Contains(t, platform, goarch)

	// Common platforms
	validPlatforms := []string{
		"darwin-arm64",
		"darwin-amd64",
		"linux-amd64",
		"linux-arm64",
		"windows-amd64",
	}

	found := false
	for _, valid := range validPlatforms {
		if platform == valid {
			found = true
			break
		}
	}

	assert.True(t, found, "platform should be one of the supported platforms: %s", platform)
}

func TestGetRuntimeLibName(t *testing.T) {
	t.Parallel()

	libName := getRuntimeLibName()

	switch runtime.GOOS {
	case "darwin":
		assert.Equal(t, "onnxruntime.dylib", libName)
	case "windows":
		assert.Equal(t, "onnxruntime.dll", libName)
	default:
		assert.Equal(t, "onnxruntime.so", libName)
	}
}

// createMockRuntimeTarGz creates a mock tar.gz archive with runtime library only
func createMockRuntimeTarGz(t *testing.T) []byte {
	var buf bytes.Buffer

	// Create gzip writer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add runtime library
	runtimeLib := getRuntimeLibName()
	runtimeContent := []byte("fake runtime library")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:    runtimeLib,
		Mode:    0644,
		Size:    int64(len(runtimeContent)),
		ModTime: time.Now(),
	}))
	_, err := tw.Write(runtimeContent)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())

	return buf.Bytes()
}

// createMockEmbeddingModelTarGz creates a mock tar.gz archive with model files only
func createMockEmbeddingModelTarGz(t *testing.T) []byte {
	var buf bytes.Buffer

	// Create gzip writer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add model files
	for _, file := range ModelFiles {
		content := []byte("fake model file: " + file)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:    file,
			Mode:    0644,
			Size:    int64(len(content)),
			ModTime: time.Now(),
		}))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())

	return buf.Bytes()
}

func TestDownloadRuntime_MockHTTP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create mock archive
	mockArchive := createMockRuntimeTarGz(t)

	// Start test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", string(rune(len(mockArchive))))
		http.ServeContent(w, r, "test.tar.gz", time.Now(), bytes.NewReader(mockArchive))
	}))
	defer server.Close()

	// Test download
	tempDir := t.TempDir()

	var progressCalls []int
	callback := func(percent int) {
		progressCalls = append(progressCalls, percent)
	}

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadRuntime(context.Background(), tempDir, callback)

	require.NoError(t, err)
	assert.True(t, len(progressCalls) > 0, "progress callback should be called")
	assert.Equal(t, 100, progressCalls[len(progressCalls)-1], "final progress should be 100")
	assert.True(t, RuntimeExists(tempDir), "runtime library should exist after download")
}

func TestDownloadEmbeddingModel_MockHTTP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create mock archive
	mockArchive := createMockEmbeddingModelTarGz(t)

	// Start test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", string(rune(len(mockArchive))))
		http.ServeContent(w, r, "test.tar.gz", time.Now(), bytes.NewReader(mockArchive))
	}))
	defer server.Close()

	// Test download
	tempDir := t.TempDir()

	var progressCalls []int
	callback := func(percent int) {
		progressCalls = append(progressCalls, percent)
	}

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadEmbeddingModel(context.Background(), tempDir, callback)

	require.NoError(t, err)
	assert.True(t, len(progressCalls) > 0, "progress callback should be called")
	assert.Equal(t, 100, progressCalls[len(progressCalls)-1], "final progress should be 100")
	assert.True(t, EmbeddingModelExists(tempDir), "embedding model files should exist after download")
}

func TestDownloadRuntime_ProgressCallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create mock archive
	mockArchive := createMockRuntimeTarGz(t)

	// Start test HTTP server with proper Content-Length
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		// Must set Content-Length for progress to work
		w.WriteHeader(http.StatusOK)
		w.Write(mockArchive)
	}))
	defer server.Close()

	// Test download
	tempDir := t.TempDir()

	var progressCalls []int
	var progressValues []int
	callback := func(percent int) {
		progressCalls = append(progressCalls, 1)
		progressValues = append(progressValues, percent)
	}

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadRuntime(context.Background(), tempDir, callback)

	require.NoError(t, err)
	assert.Greater(t, len(progressCalls), 0, "progress callback should be called at least once")
	assert.Equal(t, 100, progressValues[len(progressValues)-1], "final progress should be 100%")

	// Progress should be monotonically increasing
	for i := 1; i < len(progressValues); i++ {
		assert.GreaterOrEqual(t, progressValues[i], progressValues[i-1],
			"progress should be monotonically increasing")
	}

	// All progress values should be in [0, 100]
	for _, p := range progressValues {
		assert.GreaterOrEqual(t, p, 0)
		assert.LessOrEqual(t, p, 100)
	}
}

func TestDownloadEmbeddingModel_ProgressCallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create mock archive
	mockArchive := createMockEmbeddingModelTarGz(t)

	// Start test HTTP server with proper Content-Length
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		// Must set Content-Length for progress to work
		w.WriteHeader(http.StatusOK)
		w.Write(mockArchive)
	}))
	defer server.Close()

	// Test download
	tempDir := t.TempDir()

	var progressCalls []int
	var progressValues []int
	callback := func(percent int) {
		progressCalls = append(progressCalls, 1)
		progressValues = append(progressValues, percent)
	}

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadEmbeddingModel(context.Background(), tempDir, callback)

	require.NoError(t, err)
	assert.Greater(t, len(progressCalls), 0, "progress callback should be called at least once")
	assert.Equal(t, 100, progressValues[len(progressValues)-1], "final progress should be 100%")

	// Progress should be monotonically increasing
	for i := 1; i < len(progressValues); i++ {
		assert.GreaterOrEqual(t, progressValues[i], progressValues[i-1],
			"progress should be monotonically increasing")
	}

	// All progress values should be in [0, 100]
	for _, p := range progressValues {
		assert.GreaterOrEqual(t, p, 0)
		assert.LessOrEqual(t, p, 100)
	}
}

func TestDownloadRuntime_Retry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create mock archive
	mockArchive := createMockRuntimeTarGz(t)

	// Track attempt count
	var attemptCount atomic.Int32

	// Start test HTTP server that fails first 2 times
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		if attempt < 3 {
			// Fail first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Succeed on 3rd attempt
		http.ServeContent(w, r, "test.tar.gz", time.Now(), bytes.NewReader(mockArchive))
	}))
	defer server.Close()

	// Test download (should succeed after retries)
	tempDir := t.TempDir()

	var progressCalls []int
	callback := func(percent int) {
		progressCalls = append(progressCalls, percent)
	}

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadRuntime(context.Background(), tempDir, callback)

	require.NoError(t, err)
	assert.Equal(t, int32(3), attemptCount.Load(), "should retry exactly 3 times")
	assert.True(t, RuntimeExists(tempDir), "runtime library should exist after successful retry")
}

func TestDownloadEmbeddingModel_Retry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create mock archive
	mockArchive := createMockEmbeddingModelTarGz(t)

	// Track attempt count
	var attemptCount atomic.Int32

	// Start test HTTP server that fails first 2 times
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		if attempt < 3 {
			// Fail first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Succeed on 3rd attempt
		http.ServeContent(w, r, "test.tar.gz", time.Now(), bytes.NewReader(mockArchive))
	}))
	defer server.Close()

	// Test download (should succeed after retries)
	tempDir := t.TempDir()

	var progressCalls []int
	callback := func(percent int) {
		progressCalls = append(progressCalls, percent)
	}

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadEmbeddingModel(context.Background(), tempDir, callback)

	require.NoError(t, err)
	assert.Equal(t, int32(3), attemptCount.Load(), "should retry exactly 3 times")
	assert.True(t, EmbeddingModelExists(tempDir), "embedding model files should exist after successful retry")
}

func TestDownloadRuntime_RetryExhausted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Track attempt count
	var attemptCount atomic.Int32

	// Start test HTTP server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Test download (should fail after max retries)
	tempDir := t.TempDir()

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadRuntime(context.Background(), tempDir, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "download failed after")
	assert.Equal(t, int32(maxRetries), attemptCount.Load(), "should retry exactly maxRetries times")
}

func TestDownloadEmbeddingModel_RetryExhausted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Track attempt count
	var attemptCount atomic.Int32

	// Start test HTTP server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Test download (should fail after max retries)
	tempDir := t.TempDir()

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadEmbeddingModel(context.Background(), tempDir, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "download failed after")
	assert.Equal(t, int32(maxRetries), attemptCount.Load(), "should retry exactly maxRetries times")
}

func TestDownloadRuntime_Cancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create mock archive
	mockArchive := createMockRuntimeTarGz(t)

	// Create a slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send data slowly
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)

		// Write in small chunks with delays
		chunkSize := 100
		for i := 0; i < len(mockArchive); i += chunkSize {
			end := i + chunkSize
			if end > len(mockArchive) {
				end = len(mockArchive)
			}
			w.Write(mockArchive[i:end])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer server.Close()

	// Test download with cancellation
	tempDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	downloader := newDownloaderWithBaseURL(server.URL)
	err := downloader.DownloadRuntime(ctx, tempDir, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestExtractTarGz_RuntimeLib(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create test archive with runtime library
	mockArchive := createMockRuntimeTarGz(t)

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test-*.tar.gz")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(mockArchive)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	// Extract to temp directory
	destDir := t.TempDir()

	err = extractTarGz(tmpFile.Name(), destDir)
	require.NoError(t, err)

	// Verify runtime library extracted
	runtimeLib := getRuntimeLibName()
	path := filepath.Join(destDir, runtimeLib)
	_, err = os.Stat(path)
	assert.NoError(t, err, "runtime library should be extracted")

	// Verify content
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "fake runtime library")
}

func TestExtractTarGz_EmbeddingModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create test archive with model files
	mockArchive := createMockEmbeddingModelTarGz(t)

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test-*.tar.gz")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(mockArchive)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	// Extract to temp directory
	destDir := t.TempDir()

	err = extractTarGz(tmpFile.Name(), destDir)
	require.NoError(t, err)

	// Verify all model files extracted
	for _, file := range ModelFiles {
		path := filepath.Join(destDir, file)
		_, err := os.Stat(path)
		assert.NoError(t, err, "model file %s should be extracted", file)

		// Verify content
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "fake model file")
	}
}

func TestExtractTarGz_WithSubdirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create archive with subdirectory
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add directory
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "subdir/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
		ModTime:  time.Now(),
	}))

	// Add file in subdirectory
	content := []byte("test file in subdir")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:    "subdir/test.txt",
		Mode:    0644,
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}))
	_, err := tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test-*.tar.gz")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(buf.Bytes())
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	// Extract
	destDir := t.TempDir()
	err = extractTarGz(tmpFile.Name(), destDir)
	require.NoError(t, err)

	// Verify subdirectory and file
	subdir := filepath.Join(destDir, "subdir")
	info, err := os.Stat(subdir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	filePath := filepath.Join(destDir, "subdir", "test.txt")
	content2, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, content, content2)
}

func TestExtractTarGz_SecurityPathTraversal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create archive with path traversal attempt
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add file with path traversal
	content := []byte("malicious content")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:    "../../../etc/passwd",
		Mode:    0644,
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}))
	_, err := tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test-*.tar.gz")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(buf.Bytes())
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	// Extract should fail
	destDir := t.TempDir()
	err = extractTarGz(tmpFile.Name(), destDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "illegal file path")
}

func TestDownloadWithProgress_NoContentLength(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows (zip extraction not implemented)")
	}

	t.Parallel()

	// Create test data
	testData := []byte("test data for download")

	// Start server without Content-Length header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Explicitly don't set Content-Length
		w.WriteHeader(http.StatusOK)
		w.Write(testData)
	}))
	defer server.Close()

	// Test download
	tmpFile, err := os.CreateTemp("", "test-*.dat")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	var progressCalls []int
	callback := func(percent int) {
		progressCalls = append(progressCalls, percent)
	}

	err = downloadWithProgress(context.Background(), server.URL, tmpFile, callback)
	require.NoError(t, err)

	// Should still call callback with 100 at the end
	assert.Equal(t, 100, progressCalls[len(progressCalls)-1])

	// Verify data written correctly
	_, err = tmpFile.Seek(0, 0)
	require.NoError(t, err)
	downloaded, err := io.ReadAll(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, testData, downloaded)
}
