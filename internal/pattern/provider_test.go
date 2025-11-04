package pattern

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAstGrepProvider(t *testing.T) {
	t.Parallel()

	provider := NewAstGrepProvider()
	assert.NotNil(t, provider)
	assert.Equal(t, AstGrepVersion, provider.version)
	assert.False(t, provider.IsInitialized())
	assert.Empty(t, provider.BinaryPath())
}

func TestAstGrepProvider_EnsureBinaryInstalled_FirstCall(t *testing.T) {
	// Don't run in parallel - this test modifies global function variables

	// Create temp directory for fake binary
	tmpDir := t.TempDir()
	fakeBinaryPath := filepath.Join(tmpDir, "ast-grep")
	if runtime.GOOS == "windows" {
		fakeBinaryPath += ".exe"
	}

	// Create a fake valid binary
	var scriptContent string
	if runtime.GOOS == "windows" {
		scriptContent = "@echo off\necho ast-grep 0.29.0\n"
	} else {
		scriptContent = "#!/bin/sh\necho 'ast-grep 0.29.0'\n"
	}
	err := os.WriteFile(fakeBinaryPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	// Mock getBinaryPath to return our temp path
	originalGetBinaryPath := getBinaryPath
	defer func() { getBinaryPath = originalGetBinaryPath }()
	getBinaryPath = func() (string, error) {
		return fakeBinaryPath, nil
	}

	// Create provider and initialize
	provider := NewAstGrepProvider()
	ctx := context.Background()

	err = provider.ensureBinaryInstalled(ctx)
	require.NoError(t, err)

	// Verify state
	assert.True(t, provider.IsInitialized())
	assert.Equal(t, fakeBinaryPath, provider.BinaryPath())
}

func TestAstGrepProvider_EnsureBinaryInstalled_AlreadyExists(t *testing.T) {
	// Don't run in parallel - this test modifies global function variables

	// Create temp directory with existing valid binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "ast-grep")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	// Create fake valid binary
	var scriptContent string
	if runtime.GOOS == "windows" {
		scriptContent = "@echo off\necho ast-grep 0.29.0\n"
	} else {
		scriptContent = "#!/bin/sh\necho 'ast-grep 0.29.0'\n"
	}
	err := os.WriteFile(binaryPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	// Mock getBinaryPath
	originalGetBinaryPath := getBinaryPath
	defer func() { getBinaryPath = originalGetBinaryPath }()
	getBinaryPath = func() (string, error) {
		return binaryPath, nil
	}

	// Create provider
	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Should find existing binary and verify it
	err = provider.ensureBinaryInstalled(ctx)
	require.NoError(t, err)
	assert.True(t, provider.IsInitialized())
}

func TestAstGrepProvider_EnsureBinaryInstalled_InvalidExisting(t *testing.T) {
	// Don't run in parallel - this test modifies global function variables

	// Create temp directory with invalid binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "ast-grep")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	// Create fake invalid binary (wrong output)
	var scriptContent string
	if runtime.GOOS == "windows" {
		scriptContent = "@echo off\necho wrong-tool 1.0.0\n"
	} else {
		scriptContent = "#!/bin/sh\necho 'wrong-tool 1.0.0'\n"
	}
	err := os.WriteFile(binaryPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	// Mock getBinaryPath
	originalGetBinaryPath := getBinaryPath
	defer func() { getBinaryPath = originalGetBinaryPath }()
	getBinaryPath = func() (string, error) {
		return binaryPath, nil
	}

	// Mock detectPlatform to avoid real platform detection
	originalDetectPlatform := detectPlatform
	defer func() { detectPlatform = originalDetectPlatform }()
	detectPlatform = func() (string, error) {
		return "test-platform", nil
	}

	// Mock downloadBinary to create a valid binary
	originalDownloadBinary := downloadBinary
	defer func() { downloadBinary = originalDownloadBinary }()
	downloadBinary = func(ctx context.Context, version, platform, destPath string) error {
		// Create valid binary at destPath
		var content string
		if runtime.GOOS == "windows" {
			content = "@echo off\necho ast-grep 0.29.0\n"
		} else {
			content = "#!/bin/sh\necho 'ast-grep 0.29.0'\n"
		}
		return os.WriteFile(destPath, []byte(content), 0755)
	}

	// Create provider
	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Should detect invalid binary, remove it, and download new one
	err = provider.ensureBinaryInstalled(ctx)
	require.NoError(t, err)
	assert.True(t, provider.IsInitialized())
}

func TestAstGrepProvider_EnsureBinaryInstalled_Idempotent(t *testing.T) {
	// Don't run in parallel - this test modifies global function variables

	// Create temp directory with valid binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "ast-grep")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	// Create fake valid binary
	var scriptContent string
	if runtime.GOOS == "windows" {
		scriptContent = "@echo off\necho ast-grep 0.29.0\n"
	} else {
		scriptContent = "#!/bin/sh\necho 'ast-grep 0.29.0'\n"
	}
	err := os.WriteFile(binaryPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	// Mock getBinaryPath
	originalGetBinaryPath := getBinaryPath
	defer func() { getBinaryPath = originalGetBinaryPath }()
	getBinaryPath = func() (string, error) {
		return binaryPath, nil
	}

	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Call multiple times
	err = provider.ensureBinaryInstalled(ctx)
	require.NoError(t, err)

	err = provider.ensureBinaryInstalled(ctx)
	require.NoError(t, err)

	err = provider.ensureBinaryInstalled(ctx)
	require.NoError(t, err)

	// Should still be initialized
	assert.True(t, provider.IsInitialized())
}

func TestAstGrepProvider_EnsureBinaryInstalled_Concurrent(t *testing.T) {
	// Don't run in parallel - this test modifies global function variables

	// Create temp directory with valid binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "ast-grep")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	// Create fake valid binary
	var scriptContent string
	if runtime.GOOS == "windows" {
		scriptContent = "@echo off\necho ast-grep 0.29.0\n"
	} else {
		scriptContent = "#!/bin/sh\necho 'ast-grep 0.29.0'\n"
	}
	err := os.WriteFile(binaryPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	// Mock getBinaryPath
	originalGetBinaryPath := getBinaryPath
	defer func() { getBinaryPath = originalGetBinaryPath }()
	getBinaryPath = func() (string, error) {
		return binaryPath, nil
	}

	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Launch 10 concurrent goroutines
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := provider.ensureBinaryInstalled(ctx); err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check no errors
	for err := range errors {
		t.Errorf("Concurrent call failed: %v", err)
	}

	// Should be initialized
	assert.True(t, provider.IsInitialized())
}

func TestAstGrepProvider_EnsureBinaryInstalled_DownloadFailure(t *testing.T) {
	// Don't run in parallel - this test modifies global function variables

	// Mock getBinaryPath to return non-existent path
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "ast-grep")

	originalGetBinaryPath := getBinaryPath
	defer func() { getBinaryPath = originalGetBinaryPath }()
	getBinaryPath = func() (string, error) {
		return binaryPath, nil
	}

	// Mock detectPlatform
	originalDetectPlatform := detectPlatform
	defer func() { detectPlatform = originalDetectPlatform }()
	detectPlatform = func() (string, error) {
		return "test-platform", nil
	}

	// Mock downloadBinary to simulate download failure
	originalDownloadBinary := downloadBinary
	defer func() { downloadBinary = originalDownloadBinary }()
	downloadBinary = func(ctx context.Context, version, platform, destPath string) error {
		return fmt.Errorf("simulated download failure")
	}

	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Should fail because download is mocked to fail
	err := provider.ensureBinaryInstalled(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download")
	assert.False(t, provider.IsInitialized())
}

func TestAstGrepProvider_ThreadSafety(t *testing.T) {
	t.Parallel()

	provider := NewAstGrepProvider()

	// Concurrent reads
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = provider.IsInitialized()
			_ = provider.BinaryPath()
		}()
	}

	wg.Wait()
	// Should not race
}

// TestAstGrepProvider_Integration tests the provider with the real binary download.
func TestAstGrepProvider_Integration(t *testing.T) {
	// Only run if explicitly requested
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Should download and initialize
	err := provider.ensureBinaryInstalled(ctx)
	require.NoError(t, err)

	// Verify state
	assert.True(t, provider.IsInitialized())
	assert.NotEmpty(t, provider.BinaryPath())

	// Verify binary actually works
	binaryPath := provider.BinaryPath()
	err = verifyBinary(ctx, binaryPath)
	require.NoError(t, err)

	// Second call should be fast (no re-download)
	err = provider.ensureBinaryInstalled(ctx)
	require.NoError(t, err)
}
