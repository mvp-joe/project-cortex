package embed

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EmbedServerVersion is the well-known version of the cortex-embed binary.
// This is decoupled from the main cortex version to allow independent releases.
const EmbedServerVersion = "v1.0.1"

// Downloader handles downloading and extracting archives.
type Downloader interface {
	DownloadAndExtract(url, targetDir, ext string) error
}

// HTTPDownloader implements Downloader using real HTTP requests.
type HTTPDownloader struct{}

// NewHTTPDownloader creates a new HTTP-based downloader.
func NewHTTPDownloader() Downloader {
	return &HTTPDownloader{}
}

// EnsureBinaryInstalled checks if cortex-embed is installed and downloads it if not.
// Returns the absolute path to the binary.
// If downloader is nil, uses HTTPDownloader.
func EnsureBinaryInstalled(downloader Downloader) (string, error) {
	if downloader == nil {
		downloader = NewHTTPDownloader()
	}
	// Get installation directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	binDir := filepath.Join(homeDir, ".cortex", "bin")
	binaryPath := filepath.Join(binDir, "cortex-embed")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	// Check if binary already exists
	if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, nil
	}

	// Binary doesn't exist - download it
	fmt.Printf("Downloading required embedding server (%s, ~150MB)...\n", EmbedServerVersion)

	// Determine platform
	platform, err := detectPlatform()
	if err != nil {
		return "", err
	}

	// Construct download URL and extension
	url := constructDownloadURL(platform)
	ext := getFileExtension(platform)

	// Download and extract
	if err := downloader.DownloadAndExtract(url, binDir, ext); err != nil {
		return "", fmt.Errorf("failed to download cortex-embed: %w\n\nDiagnostics:\n  Platform: %s\n  Version: %s\n  Install path: %s\n  \nYou can manually download from:\n  %s",
			err, platform, EmbedServerVersion, binDir, url)
	}

	// Archive contains platform-specific binary name (cortex-embed-darwin-arm64)
	// Rename to generic name (cortex-embed) for easier use
	extractedName := "cortex-embed-" + platform
	if runtime.GOOS == "windows" {
		extractedName += ".exe"
	}
	extractedPath := filepath.Join(binDir, extractedName)

	// Check if extracted file exists
	if _, err := os.Stat(extractedPath); err != nil {
		return "", fmt.Errorf("extracted binary not found at %s: %w", extractedPath, err)
	}

	// Rename to generic name
	if err := os.Rename(extractedPath, binaryPath); err != nil {
		return "", fmt.Errorf("failed to rename binary: %w", err)
	}

	// Make executable on Unix systems
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0755); err != nil {
			return "", fmt.Errorf("failed to make binary executable: %w", err)
		}
	}

	fmt.Printf("✓ Embedding server installed to %s\n", binaryPath)
	return binaryPath, nil
}

// detectPlatform returns the platform string for the current system.
func detectPlatform() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Map to supported platforms
	platform := fmt.Sprintf("%s-%s", goos, goarch)

	// Validate against supported platforms
	supported := []string{
		"darwin-arm64",
		"darwin-amd64",
		"linux-amd64",
		"linux-arm64",
		"windows-amd64",
	}

	for _, p := range supported {
		if platform == p {
			return platform, nil
		}
	}

	return "", fmt.Errorf("unsupported platform: %s (supported: %s)",
		platform, strings.Join(supported, ", "))
}

// constructDownloadURL builds the object storage URL for the platform.
func constructDownloadURL(platform string) string {
	// Determine file extension
	ext := ".tar.gz"
	if strings.HasPrefix(platform, "windows") {
		ext = ".zip"
	}

	// Construct object storage URL:
	// Pattern: cortex-embed-{version}-{platform}.{ext}
	// Example: cortex-embed-v1.0.1-darwin-amd64.tar.gz
	return fmt.Sprintf(
		"https://project-cortex-files.t3.storage.dev/cortex-embed-%s-%s%s",
		EmbedServerVersion, // Keep 'v' prefix (e.g., v1.0.1)
		platform,           // Already in correct format (e.g., darwin-amd64)
		ext,
	)
}

// getFileExtension returns the archive extension for the platform.
func getFileExtension(platform string) string {
	if strings.HasPrefix(platform, "windows") {
		return ".zip"
	}
	return ".tar.gz"
}

// DownloadAndExtract downloads the archive from the given URL and extracts it to targetDir.
func (d *HTTPDownloader) DownloadAndExtract(url, targetDir, ext string) error {
	// Create target directory
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}

	// Download file
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	// Show progress
	fmt.Printf("Downloading from %s...\n", url)

	// Create a temporary file for download (use appropriate extension)
	tmpSuffix := "cortex-embed-*" + ext
	tmpFile, err := os.CreateTemp("", tmpSuffix)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Copy with progress indication
	totalBytes := resp.ContentLength
	written, err := io.Copy(tmpFile, &progressReader{
		reader:     resp.Body,
		total:      totalBytes,
		onProgress: printProgress,
	})
	tmpFile.Close()

	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	if totalBytes > 0 && written != totalBytes {
		return fmt.Errorf("incomplete download: got %d bytes, expected %d", written, totalBytes)
	}

	fmt.Println("\n✓ Download complete, extracting...")

	// Extract archive (format depends on OS)
	if ext == ".zip" {
		if err := extractZip(tmpPath, targetDir); err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
	} else {
		if err := extractTarGz(tmpPath, targetDir); err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
	}

	return nil
}

// extractTarGz extracts a .tar.gz file to the target directory.
func extractTarGz(archivePath, targetDir string) error {
	// Open archive
	file, err := os.Open(archivePath)
	if err != nil {
		return err
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

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read error: %w", err)
		}

		// Construct target path
		target := filepath.Join(targetDir, header.Name)

		// Security: prevent path traversal
		if !strings.HasPrefix(target, filepath.Clean(targetDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", target, err)
			}

		case tar.TypeReg:
			// Create file
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", target, err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("failed to write file %s: %w", target, err)
			}
			f.Close()

		default:
			// Skip other types (symlinks, etc.)
			fmt.Printf("Skipping unsupported file type %c: %s\n", header.Typeflag, header.Name)
		}
	}

	return nil
}

// extractZip extracts a .zip file to the target directory (Windows).
func extractZip(archivePath, targetDir string) error {
	// Open zip archive
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	// Extract files
	for _, f := range r.File {
		// Construct target path
		target := filepath.Join(targetDir, f.Name)

		// Security: prevent path traversal
		if !strings.HasPrefix(target, filepath.Clean(targetDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in archive: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			// Create directory
			if err := os.MkdirAll(target, f.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", target, err)
			}
		} else {
			// Create file
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", target, err)
			}

			rc, err := f.Open()
			if err != nil {
				outFile.Close()
				return fmt.Errorf("failed to open file in archive: %w", err)
			}

			if _, err := io.Copy(outFile, rc); err != nil {
				rc.Close()
				outFile.Close()
				return fmt.Errorf("failed to write file %s: %w", target, err)
			}

			rc.Close()
			outFile.Close()
		}
	}

	return nil
}

// progressReader wraps an io.Reader and calls a callback with progress updates.
type progressReader struct {
	reader     io.Reader
	total      int64
	current    int64
	onProgress func(current, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.current += int64(n)

	if pr.onProgress != nil && pr.total > 0 {
		pr.onProgress(pr.current, pr.total)
	}

	return n, err
}

// printProgress prints download progress.
func printProgress(current, total int64) {
	if total <= 0 {
		return
	}

	percent := float64(current) / float64(total) * 100
	mb := float64(current) / 1024 / 1024
	totalMB := float64(total) / 1024 / 1024

	// Print progress on same line
	fmt.Printf("\r  Progress: %.1f%% (%.1f/%.1f MB)", percent, mb, totalMB)
}
