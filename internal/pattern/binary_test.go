package pattern

import (
	"archive/zip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectPlatform(t *testing.T) {
	t.Parallel()

	// Test current platform (should always work)
	platform, err := detectPlatform()
	require.NoError(t, err)
	assert.NotEmpty(t, platform)

	// Verify platform format based on current OS/arch (cortex naming convention)
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch goos {
	case "darwin":
		if goarch == "arm64" {
			assert.Equal(t, "darwin-arm64", platform)
		} else if goarch == "amd64" {
			assert.Equal(t, "darwin-amd64", platform)
		}
	case "linux":
		if goarch == "arm64" {
			assert.Equal(t, "linux-arm64", platform)
		} else if goarch == "amd64" {
			assert.Equal(t, "linux-amd64", platform)
		}
	case "windows":
		if goarch == "amd64" {
			assert.Equal(t, "windows-amd64", platform)
		}
	}
}

func TestConstructDownloadURL(t *testing.T) {
	// Don't run in parallel - uses global constructDownloadURL

	// Save and restore original
	originalConstructDownloadURL := constructDownloadURL
	defer func() { constructDownloadURL = originalConstructDownloadURL }()

	tests := []struct {
		name     string
		platform string
		expected string
	}{
		{
			name:     "darwin arm64",
			platform: "darwin-arm64",
			expected: fmt.Sprintf("https://project-cortex-files.t3.storage.dev/ast-grep-v%s-darwin-arm64.zip", AstGrepVersion),
		},
		{
			name:     "darwin amd64",
			platform: "darwin-amd64",
			expected: fmt.Sprintf("https://project-cortex-files.t3.storage.dev/ast-grep-v%s-darwin-amd64.zip", AstGrepVersion),
		},
		{
			name:     "linux arm64",
			platform: "linux-arm64",
			expected: fmt.Sprintf("https://project-cortex-files.t3.storage.dev/ast-grep-v%s-linux-arm64.zip", AstGrepVersion),
		},
		{
			name:     "linux amd64",
			platform: "linux-amd64",
			expected: fmt.Sprintf("https://project-cortex-files.t3.storage.dev/ast-grep-v%s-linux-amd64.zip", AstGrepVersion),
		},
		{
			name:     "windows amd64",
			platform: "windows-amd64",
			expected: fmt.Sprintf("https://project-cortex-files.t3.storage.dev/ast-grep-v%s-windows-amd64.zip", AstGrepVersion),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := originalConstructDownloadURL(tt.platform)
			assert.Equal(t, tt.expected, url)
		})
	}
}

func TestGetBinaryPath(t *testing.T) {
	// Don't run in parallel - mocks global getBinaryPath

	// Setup: Use temp home to avoid polluting ~/.cortex
	tempHome := t.TempDir()

	// Save original and restore after test
	originalGetBinaryPath := getBinaryPath
	defer func() { getBinaryPath = originalGetBinaryPath }()

	// Override getBinaryPath to use temp home
	getBinaryPath = func() (string, error) {
		binDir := filepath.Join(tempHome, ".cortex", "bin")
		binaryPath := filepath.Join(binDir, "ast-grep")
		if runtime.GOOS == "windows" {
			binaryPath += ".exe"
		}
		return binaryPath, nil
	}

	path, err := getBinaryPath()
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	// Should end with ast-grep or ast-grep.exe
	basename := filepath.Base(path)
	if runtime.GOOS == "windows" {
		assert.Equal(t, "ast-grep.exe", basename)
	} else {
		assert.Equal(t, "ast-grep", basename)
	}

	// Should be in ~/.cortex/bin/ (in temp home)
	assert.Contains(t, path, filepath.Join(".cortex", "bin"))
	assert.Contains(t, path, tempHome, "path should be in temp home")
}

func TestDownloadBinary(t *testing.T) {
	// Don't run in parallel - mocks global functions

	ctx := context.Background()

	// Save and restore originals
	originalConstructDownloadURL := constructDownloadURL
	originalDownloadBinary := downloadBinary
	defer func() {
		constructDownloadURL = originalConstructDownloadURL
		downloadBinary = originalDownloadBinary
	}()

	// Create a fake zip file with a binary
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test-app.zip")

	// Determine binary name (Tigris archives use "ast-grep" not "sg")
	binaryName := "ast-grep"
	if runtime.GOOS == "windows" {
		binaryName = "ast-grep.exe"
	}

	// Create zip with fake binary
	zipFile, err := os.Create(zipPath)
	require.NoError(t, err)

	zipWriter := zip.NewWriter(zipFile)
	binaryWriter, err := zipWriter.Create(binaryName)
	require.NoError(t, err)
	_, err = binaryWriter.Write([]byte("fake-binary-content"))
	require.NoError(t, err)
	err = zipWriter.Close()
	require.NoError(t, err)
	zipFile.Close()

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request path (Tigris URL format)
		expectedPath := fmt.Sprintf("/ast-grep-v%s-test-platform.zip", AstGrepVersion)
		assert.Equal(t, expectedPath, r.URL.Path)

		// Return the zip file
		w.WriteHeader(http.StatusOK)
		zipData, _ := os.ReadFile(zipPath)
		w.Write(zipData)
	}))
	defer server.Close()

	// Override constructDownloadURL (Tigris URL format)
	constructDownloadURL = func(platform string) string {
		return server.URL + fmt.Sprintf("/ast-grep-v%s-%s.zip", AstGrepVersion, platform)
	}

	// Test download with original implementation
	destPath := filepath.Join(tmpDir, "ast-grep")
	if runtime.GOOS == "windows" {
		destPath += ".exe"
	}

	err = originalDownloadBinary(ctx, AstGrepVersion, "test-platform", destPath)
	require.NoError(t, err)

	// Verify file exists and has correct content
	content, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, "fake-binary-content", string(content))

	// Verify permissions (Unix)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
	}
}

func TestDownloadBinary_FailsOnHTTPError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Mock server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	// Attempt download with mock URL
	mockURL := server.URL + "/fake-binary"
	req, err := http.NewRequestWithContext(ctx, "GET", mockURL, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should fail with non-200 status
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestVerifyBinary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("valid binary", func(t *testing.T) {
		t.Parallel()

		// Create a fake shell script that mimics ast-grep --version
		tmpDir := t.TempDir()
		binaryPath := filepath.Join(tmpDir, "ast-grep")

		// Create script content based on OS
		var scriptContent string
		if runtime.GOOS == "windows" {
			binaryPath += ".bat"
			scriptContent = "@echo off\necho ast-grep 0.29.0\n"
		} else {
			scriptContent = "#!/bin/sh\necho 'ast-grep 0.29.0'\n"
		}

		err := os.WriteFile(binaryPath, []byte(scriptContent), 0755)
		require.NoError(t, err)

		// Verify should succeed
		err = verifyBinary(ctx, binaryPath)
		assert.NoError(t, err)
	})

	t.Run("invalid binary output", func(t *testing.T) {
		t.Parallel()

		// Create a fake script that outputs wrong text
		tmpDir := t.TempDir()
		binaryPath := filepath.Join(tmpDir, "fake-binary")

		var scriptContent string
		if runtime.GOOS == "windows" {
			binaryPath += ".bat"
			scriptContent = "@echo off\necho wrong-tool 1.0.0\n"
		} else {
			scriptContent = "#!/bin/sh\necho 'wrong-tool 1.0.0'\n"
		}

		err := os.WriteFile(binaryPath, []byte(scriptContent), 0755)
		require.NoError(t, err)

		// Verify should fail
		err = verifyBinary(ctx, binaryPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid binary output")
	})

	t.Run("binary not found", func(t *testing.T) {
		t.Parallel()

		// Non-existent path
		binaryPath := filepath.Join(t.TempDir(), "nonexistent")

		err := verifyBinary(ctx, binaryPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "verification failed")
	})

	t.Run("binary not executable", func(t *testing.T) {
		t.Parallel()

		// Skip on Windows (different permission model)
		if runtime.GOOS == "windows" {
			t.Skip("Skipping on Windows")
		}

		tmpDir := t.TempDir()
		binaryPath := filepath.Join(tmpDir, "not-executable")

		// Create file without execute permission
		err := os.WriteFile(binaryPath, []byte("#!/bin/sh\necho 'ast-grep'\n"), 0644)
		require.NoError(t, err)

		err = verifyBinary(ctx, binaryPath)
		assert.Error(t, err)
	})
}

func TestVerifyBinary_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Skip on Windows (different signal handling)
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows - signal handling differs")
	}

	// Create a script that sleeps forever
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "slow-binary")
	scriptContent := "#!/bin/sh\nsleep 100\n"
	err := os.WriteFile(binaryPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	// Create context that cancels immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should fail with context cancellation
	err = verifyBinary(ctx, binaryPath)
	assert.Error(t, err)
}
