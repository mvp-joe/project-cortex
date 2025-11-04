//go:build integration

package pattern

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDownloadBinaryIntegration is an integration test that downloads the real binary.
// It's tagged to run separately from unit tests.
func TestDownloadBinaryIntegration(t *testing.T) {
	// Only run if explicitly requested
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Detect platform
	platform, err := detectPlatform()
	require.NoError(t, err)

	// Download to temp directory
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "ast-grep")
	if runtime.GOOS == "windows" {
		destPath += ".exe"
	}

	// Download
	err = downloadBinary(ctx, AstGrepVersion, platform, destPath)
	require.NoError(t, err)

	// Verify downloaded binary
	err = verifyBinary(ctx, destPath)
	require.NoError(t, err)

	// Try running it
	cmd := exec.CommandContext(ctx, destPath, "--version")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(output), "ast-grep")
}
