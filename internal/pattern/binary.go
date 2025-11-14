package pattern

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// AstGrepVersion is the pinned version of ast-grep binary (without v prefix).
const AstGrepVersion = "0.39.6"

// detectPlatform returns the platform string for ast-grep binary names.
// It maps Go's runtime.GOOS/GOARCH to cortex naming conventions.
// Declared as a variable to allow mocking in tests.
var detectPlatform = func() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch goos {
	case "darwin":
		if goarch == "arm64" {
			return "darwin-arm64", nil
		} else if goarch == "amd64" {
			return "darwin-amd64", nil
		}
	case "linux":
		if goarch == "arm64" {
			return "linux-arm64", nil
		} else if goarch == "amd64" {
			return "linux-amd64", nil
		}
	case "windows":
		if goarch == "amd64" {
			return "windows-amd64", nil
		}
	}

	return "", fmt.Errorf("unsupported platform: %s/%s (ast-grep not available for this platform)",
		goos, goarch)
}

// constructDownloadURL builds the Tigris object storage URL for the ast-grep binary.
// Binaries are mirrored from GitHub releases to Tigris to avoid rate limits.
// Declared as a variable to allow mocking in tests.
var constructDownloadURL = func(platform string) string {
	// ast-grep binaries are stored in cortex naming convention: ast-grep-v{version}-{platform}.zip
	return fmt.Sprintf(
		"https://project-cortex-files.t3.storage.dev/ast-grep-v%s-%s.zip",
		AstGrepVersion,
		platform,
	)
}

// getBinaryPath returns the cache path where ast-grep should be stored.
// Declared as a variable to allow mocking in tests.
var getBinaryPath = func() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	binDir := filepath.Join(homeDir, ".cortex", "bin")
	binaryPath := filepath.Join(binDir, "ast-grep")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	return binaryPath, nil
}

// downloadBinary downloads the ast-grep binary from GitHub releases.
// It downloads a zip file, extracts the binary, and moves it to destPath.
// Declared as a variable to allow mocking in tests.
var downloadBinary = func(ctx context.Context, version, platform, destPath string) error {
	// Construct download URL (all platforms use .zip)
	url := constructDownloadURL(platform)

	// Create destination directory
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Download to temp zip file
	tmpZip, err := os.CreateTemp(destDir, "ast-grep-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath) // Clean up temp zip

	// Download from Tigris object storage
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	// Copy to temp zip file
	if _, err := io.Copy(tmpZip, resp.Body); err != nil {
		tmpZip.Close()
		return fmt.Errorf("failed to write download: %w", err)
	}
	tmpZip.Close()

	// Extract binary from zip
	if err := extractBinaryFromZip(tmpZipPath, destPath); err != nil {
		return fmt.Errorf("failed to extract binary: %w", err)
	}

	return nil
}

// extractBinaryFromZip extracts the ast-grep/ast-grep.exe binary from the zip archive.
func extractBinaryFromZip(zipPath, destPath string) error {
	// Open zip archive
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	// Look for the binary (ast-grep or ast-grep.exe in our Tigris archives)
	var binaryName string
	if runtime.GOOS == "windows" {
		binaryName = "ast-grep.exe"
	} else {
		binaryName = "ast-grep"
	}

	// Find and extract the binary
	for _, f := range r.File {
		if filepath.Base(f.Name) == binaryName {
			// Found the binary - extract it
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("failed to open file in zip: %w", err)
			}
			defer rc.Close()

			// Write to temp file first
			tmpBinary, err := os.CreateTemp(filepath.Dir(destPath), "sg-*.tmp")
			if err != nil {
				return fmt.Errorf("failed to create temp binary: %w", err)
			}
			tmpPath := tmpBinary.Name()
			defer os.Remove(tmpPath)

			if _, err := io.Copy(tmpBinary, rc); err != nil {
				tmpBinary.Close()
				return fmt.Errorf("failed to write binary: %w", err)
			}
			tmpBinary.Close()

			// Make executable (Unix systems)
			if runtime.GOOS != "windows" {
				if err := os.Chmod(tmpPath, 0755); err != nil {
					return fmt.Errorf("failed to make binary executable: %w", err)
				}
			}

			// Atomic rename
			if err := os.Rename(tmpPath, destPath); err != nil {
				return fmt.Errorf("failed to rename binary: %w", err)
			}

			return nil
		}
	}

	return fmt.Errorf("binary %s not found in zip archive", binaryName)
}

// verifyBinary checks if the ast-grep binary is valid by running --version.
func verifyBinary(ctx context.Context, binaryPath string) error {
	// Run --version
	cmd := exec.CommandContext(ctx, binaryPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("binary verification failed: %w", err)
	}

	// Parse version output
	// Expected format: "ast-grep 0.29.0" or similar
	outputStr := strings.TrimSpace(string(output))
	if !strings.Contains(outputStr, "ast-grep") {
		return fmt.Errorf("invalid binary output: %s", outputStr)
	}

	return nil
}
