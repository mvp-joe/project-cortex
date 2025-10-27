package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/mvp-joe/project-cortex/internal/embed/server"

	"github.com/kluctl/go-embed-python/embed_util"
	"github.com/kluctl/go-embed-python/python"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Get or create persistent cache directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get user home directory: %v", err)
	}
	cortexDir := filepath.Join(homeDir, ".cortex")

	// Create embedded Python environment in persistent location
	// This persists across runs and survives reboots (unlike /tmp)
	// Hash suffix ensures version safety when Python/deps are updated
	pythonRuntimeDir := filepath.Join(cortexDir, "embed", "runtime")
	ep, err := python.NewEmbeddedPythonWithTmpDir(pythonRuntimeDir, true)
	if err != nil {
		log.Fatalf("Failed to create embedded Python: %v", err)
	}

	// Extract pip packages to persistent location
	// Smart caching: only extracts files that changed
	pipCacheDir := filepath.Join(cortexDir, "embed", "packages")
	embeddedFiles, err := embed_util.NewEmbeddedFilesWithTmpDir(server.Data, pipCacheDir, true)
	if err != nil {
		log.Fatalf("Failed to load embedded files: %v", err)
	}
	ep.AddPythonPath(embeddedFiles.GetExtractedPath())

	// Write Python script to temp file
	tmpDir, err := os.MkdirTemp("", "cortex-embed-*")
	if err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "embedding_service.py")
	if err := os.WriteFile(scriptPath, []byte(server.EmbeddingScript), 0644); err != nil {
		log.Fatalf("Failed to write script: %v", err)
	}

	// Start Python server
	cmd, err := ep.PythonCmd(scriptPath)
	if err != nil {
		log.Fatalf("Failed to create Python command: %v", err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start Python server: %v", err)
	}

	// Wait for service to be ready
	log.Println("Starting embedding service on http://127.0.0.1:8121")

	if err := waitForReady(ctx); err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		log.Fatalf("Service failed to start: %v", err)
	}

	log.Println("✓ Service ready")

	// Wait for interrupt
	<-ctx.Done()
	log.Println("Shutting down...")
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func waitForReady(ctx context.Context) error {
	client := &http.Client{Timeout: 2 * time.Second}
	timeout := 2 * time.Minute // Allow time for model download on first run

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout after %v waiting for service", timeout)
			}

			resp, err := client.Get("http://127.0.0.1:8121/")
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}
