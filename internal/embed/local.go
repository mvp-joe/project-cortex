package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// localProvider manages a local cortex-embed binary and provides embedding functionality.
type localProvider struct {
	binaryPath  string
	port        int
	cmd         *exec.Cmd
	client      *http.Client
	initialized bool
}

// newLocalProvider creates a new local embedding provider.
// The binaryPath will be determined during Initialize().
func newLocalProvider() (*localProvider, error) {
	return &localProvider{
		port:   DefaultEmbedServerPort,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Initialize prepares the local provider by ensuring the binary is installed
// and starting the embedding server process.
func (p *localProvider) Initialize(ctx context.Context) error {
	if p.initialized {
		return nil
	}

	// 1. Ensure binary is installed (download if needed)
	binaryPath, err := EnsureBinaryInstalled(nil)
	if err != nil {
		return fmt.Errorf("failed to ensure binary installed: %w", err)
	}
	p.binaryPath = binaryPath

	// 2. Start the cortex-embed process
	if err := p.startServer(ctx); err != nil {
		return fmt.Errorf("failed to start embedding server: %w", err)
	}

	// 3. Wait for health check (retry with backoff)
	if err := p.waitForHealthy(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("embedding server failed to become healthy: %w", err)
	}

	p.initialized = true
	return nil
}

// startServer starts the embedding server process if not already running.
func (p *localProvider) startServer(ctx context.Context) error {
	// Check if already running (maybe from previous initialization)
	if p.isHealthy() {
		return nil
	}

	// Start the binary
	p.cmd = exec.CommandContext(ctx, p.binaryPath)
	p.cmd.Stdout = os.Stdout
	p.cmd.Stderr = os.Stderr

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	return nil
}

// isHealthy checks if the embedding server is responding to health checks.
func (p *localProvider) isHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("http://127.0.0.1:%d/", p.port), nil)
	resp, err := p.client.Do(req)
	if err == nil && resp.StatusCode == 200 {
		resp.Body.Close()
		return true
	}
	return false
}

// waitForHealthy waits for the embedding server to become healthy.
func (p *localProvider) waitForHealthy(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for embedding server")
		case <-ticker.C:
			if p.isHealthy() {
				return nil
			}
		}
	}
}

// embedRequest represents the JSON request body for the /embed endpoint.
type embedRequest struct {
	Texts []string `json:"texts"`
	Mode  string   `json:"mode"` // "query" or "passage"
}

// embedResponse represents the JSON response from the /embed endpoint.
type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed converts a slice of text strings into their vector representations.
// Initialize() must be called before Embed().
func (p *localProvider) Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized: call Initialize() first")
	}

	reqBody := embedRequest{
		Texts: texts,
		Mode:  string(mode),
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/embed", p.port)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding server returned status %d", resp.StatusCode)
	}

	var embedResp embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return embedResp.Embeddings, nil
}

// Dimensions returns the dimensionality of the embeddings (384 for BGE-small-en-v1.5).
func (p *localProvider) Dimensions() int {
	return 384
}

// Close stops the embedding server and releases resources.
// It attempts a graceful shutdown with SIGTERM first, then falls back to SIGKILL after 5 seconds.
func (p *localProvider) Close() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	// Try graceful shutdown first (SIGTERM)
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process already dead or error sending signal
		return err
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process exited gracefully
		return err
	case <-time.After(5 * time.Second):
		// Timeout - force kill
		return p.cmd.Process.Kill()
	}
}
