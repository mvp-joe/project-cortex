package pattern

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
)

// AstGrepProvider manages the ast-grep binary and provides pattern search functionality.
// It uses lazy initialization to download and verify the binary only when first needed.
type AstGrepProvider struct {
	binaryPath  string
	version     string
	initialized bool
	mu          sync.Mutex
}

// NewAstGrepProvider creates a new ast-grep provider with lazy initialization.
func NewAstGrepProvider() *AstGrepProvider {
	return &AstGrepProvider{
		version:     AstGrepVersion,
		initialized: false,
	}
}

// ensureBinaryInstalled ensures the ast-grep binary is installed and ready to use.
// It uses lazy initialization: only downloads on first call, then returns cached path.
// Thread-safe: multiple concurrent calls will only download once.
func (p *AstGrepProvider) ensureBinaryInstalled(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Already initialized - return early
	if p.initialized {
		return nil
	}

	// Get binary path
	binaryPath, err := getBinaryPath()
	if err != nil {
		return fmt.Errorf("failed to get binary path: %w", err)
	}

	// Check if binary already exists
	if _, err := os.Stat(binaryPath); err == nil {
		// Binary exists - verify it works
		if err := verifyBinary(ctx, binaryPath); err == nil {
			// Binary is valid
			p.binaryPath = binaryPath
			p.initialized = true
			return nil
		}

		// Binary exists but is invalid - remove it and re-download
		log.Printf("Existing ast-grep binary is invalid, removing and re-downloading...")
		if err := os.Remove(binaryPath); err != nil {
			log.Printf("Warning: failed to remove invalid binary: %v", err)
		}
	}

	// Binary doesn't exist or was invalid - download it
	log.Printf("Downloading ast-grep %s...", p.version)

	platform, err := detectPlatform()
	if err != nil {
		return fmt.Errorf("failed to detect platform: %w", err)
	}

	if err := downloadBinary(ctx, p.version, platform, binaryPath); err != nil {
		return fmt.Errorf("failed to download ast-grep: %w", err)
	}

	// Verify downloaded binary
	if err := verifyBinary(ctx, binaryPath); err != nil {
		return fmt.Errorf("downloaded binary verification failed: %w", err)
	}

	log.Printf("✓ ast-grep installed to %s", binaryPath)

	p.binaryPath = binaryPath
	p.initialized = true
	return nil
}

// BinaryPath returns the path to the ast-grep binary.
// Returns empty string if not yet initialized.
func (p *AstGrepProvider) BinaryPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.binaryPath
}

// IsInitialized returns whether the provider has been initialized.
func (p *AstGrepProvider) IsInitialized() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.initialized
}

// Search implements the PatternSearcher interface.
// It delegates to ExecutePattern which handles all phases (binary management, validation, execution).
func (p *AstGrepProvider) Search(ctx context.Context, req *PatternRequest, projectRoot string) (*PatternResponse, error) {
	return ExecutePattern(ctx, p, req, projectRoot)
}
