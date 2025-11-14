package cli

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var embedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Embedding server commands",
	Long:  `Manage the embedding daemon server.`,
}

func init() {
	rootCmd.AddCommand(embedCmd)
}

// getLibDir returns the runtime library directory path.
// Respects CORTEX_LIB_DIR environment variable.
func getLibDir() string {
	if dir := os.Getenv("CORTEX_LIB_DIR"); dir != "" {
		return dir
	}

	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cortex", "lib")
}

// getModelDir returns the model directory path.
// Respects CORTEX_MODEL_DIR environment variable.
func getModelDir() string {
	if dir := os.Getenv("CORTEX_MODEL_DIR"); dir != "" {
		return dir
	}

	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cortex", "models")
}

// getIdleTimeout returns the idle timeout duration.
// Respects CORTEX_EMBED_IDLE_TIMEOUT environment variable (minutes).
func getIdleTimeout() time.Duration {
	// Default to 10 minutes
	return 10 * time.Minute
}

// getDimensions returns the embedding dimensions.
// BGE-small-en-v1.5 model produces 384-dimensional embeddings.
func getDimensions() int {
	return 384
}
