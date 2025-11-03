package cache_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mvp-joe/project-cortex/internal/cache"
)

// ExampleGetCurrentBranch demonstrates how to get the current git branch
func ExampleGetCurrentBranch() {
	// In a real scenario, you'd use your actual project path
	projectPath := "/path/to/your/project"

	branch := cache.GetCurrentBranch(projectPath)
	fmt.Printf("Current branch: %s\n", branch)
	// Output depends on current state:
	// - "main" or "feature-x" for normal branches
	// - "detached-a1b2c3d" for detached HEAD
	// - "unknown" if git commands fail
}

// ExampleFindAncestorBranch demonstrates how to find the ancestor branch
func ExampleFindAncestorBranch() {
	projectPath := "/path/to/your/project"
	currentBranch := "feature-awesome"

	ancestor := cache.FindAncestorBranch(projectPath, currentBranch)
	if ancestor != "" {
		fmt.Printf("Branch %s derives from %s\n", currentBranch, ancestor)
		// Can now optimize by copying chunks from ancestor DB
	} else {
		fmt.Printf("No ancestor found for %s\n", currentBranch)
		// Must do full indexing
	}
}

// ExampleGetGitBranches demonstrates how to list all git branches
func ExampleGetGitBranches() {
	projectPath := "/path/to/your/project"

	branches, err := cache.GetGitBranches(projectPath)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("Available branches:")
	for _, branch := range branches {
		// Current branch is prefixed with "* "
		fmt.Printf("  %s\n", branch)
	}
}

// Example_branchOptimizationWorkflow shows a complete workflow for branch-aware indexing
func Example_branchOptimizationWorkflow() {
	projectPath := "/path/to/your/project"

	// Step 1: Detect current branch
	currentBranch := cache.GetCurrentBranch(projectPath)
	fmt.Printf("Working on branch: %s\n", currentBranch)

	// Step 2: Find ancestor for optimization
	ancestorBranch := cache.FindAncestorBranch(projectPath, currentBranch)
	if ancestorBranch != "" {
		fmt.Printf("Can copy unchanged chunks from: %s\n", ancestorBranch)
		// In real code, you would:
		// 1. Open ancestor DB: reader, _ := storage.NewChunkReader(cachePath, ancestorBranch)
		// 2. For each file, check if hash matches ancestor
		// 3. If match: copy chunks from ancestor (no re-embedding!)
		// 4. If different: full indexing for that file
	} else {
		fmt.Println("No ancestor found, doing full indexing")
	}

	// Step 3: List all branches for cache management
	branches, _ := cache.GetGitBranches(projectPath)
	fmt.Printf("Total branches: %d\n", len(branches))
}

// Example_detachedHeadHandling shows how detached HEAD is handled
func Example_detachedHeadHandling() {
	// Create a temporary git repo for demonstration
	dir, _ := os.MkdirTemp("", "example")
	defer os.RemoveAll(dir)

	// Initialize repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = dir
	cmd.Run()

	// Create commit
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "branch", "-M", "main")
	cmd.Dir = dir
	cmd.Run()

	// Normal branch
	branch := cache.GetCurrentBranch(dir)
	fmt.Printf("Normal branch: %s\n", branch)

	// Create detached HEAD by checking out commit hash
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, _ := cmd.Output()
	commitHash := strings.TrimSpace(string(output))

	cmd = exec.Command("git", "checkout", commitHash)
	cmd.Dir = dir
	cmd.Run()

	// Now in detached HEAD state
	branch = cache.GetCurrentBranch(dir)
	// Will return something like "detached-a1b2c3d"
	fmt.Printf("Detached HEAD: starts with 'detached-': %v\n", len(branch) > 9 && branch[:9] == "detached-")

	// Output:
	// Normal branch: main
	// Detached HEAD: starts with 'detached-': true
}
