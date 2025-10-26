package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// LocalProvider manages a local cortex-embed binary and provides embedding functionality.
type LocalProvider struct {
	binaryPath string
	port       int
	cmd        *exec.Cmd
	client     *http.Client
}

// NewLocalProvider creates a new local embedding provider.
// The binaryPath should point to the cortex-embed executable.
func NewLocalProvider(binaryPath string) (*LocalProvider, error) {
	return &LocalProvider{
		binaryPath: binaryPath,
		port:       8121,
		client:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ensureRunning checks if the embedding server is running and starts it if not.
func (p *LocalProvider) ensureRunning(ctx context.Context) error {
	// Check if already running
	if p.isHealthy() {
		return nil
	}

	// Start the binary
	p.cmd = exec.CommandContext(ctx, p.binaryPath)
	p.cmd.Stdout = os.Stdout
	p.cmd.Stderr = os.Stderr

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start embedding server: %w", err)
	}

	// Wait for health check
	return p.waitForHealthy(ctx, 60*time.Second)
}

// isHealthy checks if the embedding server is responding to health checks.
func (p *LocalProvider) isHealthy() bool {
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
func (p *LocalProvider) waitForHealthy(ctx context.Context, timeout time.Duration) error {
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
}

// embedResponse represents the JSON response from the /embed endpoint.
type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed converts a slice of text strings into their vector representations.
func (p *LocalProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := p.ensureRunning(ctx); err != nil {
		return nil, err
	}

	reqBody := embedRequest{Texts: texts}
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
func (p *LocalProvider) Dimensions() int {
	return 384
}

// Close stops the embedding server and releases resources.
func (p *LocalProvider) Close() error {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}
