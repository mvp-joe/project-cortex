package cli

// Test Plan for Clean Command:
// - runClean deletes current branch database successfully
// - runClean with --all deletes entire cache directory
// - runClean with --quiet suppresses output
// - runClean handles missing cache directory gracefully
// - runClean handles missing branch database gracefully
// - runClean calculates and displays correct file sizes
// - runClean --all calculates total size and branch count correctly
// - getCacheStats correctly counts branches and calculates total size
// - getCacheStats ignores non-.db files
// - getCacheStats handles empty branches directory

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestCache creates a test cache directory with branch databases
func setupTestCache(t *testing.T, branches []string) (projectPath string, cachePath string) {
	t.Helper()

	// Create project directory
	projectPath = t.TempDir()

	// Initialize as git repo
	initGitRepo(t, projectPath)

	// Get cache path (will create cache directory structure)
	var err error
	cachePath, err = cache.EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	// Create branch databases
	branchesDir := filepath.Join(cachePath, "branches")
	for _, branch := range branches {
		dbPath := filepath.Join(branchesDir, branch+".db")
		// Write some data to make file non-empty
		err := os.WriteFile(dbPath, []byte("test database content for "+branch), 0644)
		require.NoError(t, err)
	}

	return projectPath, cachePath
}

// initGitRepo initializes a proper git repository with a main branch
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Use actual git init for proper repository structure
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	err := cmd.Run()
	require.NoError(t, err)

	// Configure git to avoid warnings
	configCmd := exec.Command("git", "config", "user.name", "Test User")
	configCmd.Dir = dir
	_ = configCmd.Run()

	configCmd = exec.Command("git", "config", "user.email", "test@example.com")
	configCmd.Dir = dir
	_ = configCmd.Run()

	// Create initial commit on main branch to establish branch
	readmeFile := filepath.Join(dir, "README.md")
	err = os.WriteFile(readmeFile, []byte("# Test Repo\n"), 0644)
	require.NoError(t, err)

	addCmd := exec.Command("git", "add", "README.md")
	addCmd.Dir = dir
	err = addCmd.Run()
	require.NoError(t, err)

	commitCmd := exec.Command("git", "commit", "-m", "Initial commit")
	commitCmd.Dir = dir
	err = commitCmd.Run()
	require.NoError(t, err)
}

func TestRunClean_DeletesCurrentBranchDatabase(t *testing.T) {
	// Setup test cache with multiple branches
	projectPath, _ := setupTestCache(t, []string{"main", "feature-1", "feature-2"})

	// Save original working directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	// Change to project directory
	err = os.Chdir(projectPath)
	require.NoError(t, err)

	// Set quiet flag to suppress output during tests
	cleanQuietFlag = true
	cleanAllFlag = false
	defer func() { cleanQuietFlag = false }()

	// Run clean (should delete main.db since that's the current branch)
	// Note: runClean calls EnsureCacheLocation which may trigger migration
	err = runClean(nil, nil)
	require.NoError(t, err)

	// Get the actual cache path after runClean (which may have migrated)
	actualCachePath, err := cache.EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	// Verify main.db was deleted
	mainDBPath := filepath.Join(actualCachePath, "branches", "main.db")
	_, err = os.Stat(mainDBPath)
	assert.True(t, os.IsNotExist(err), "main.db should be deleted")

	// Verify other branches still exist
	feature1Path := filepath.Join(actualCachePath, "branches", "feature-1.db")
	_, err = os.Stat(feature1Path)
	assert.NoError(t, err, "feature-1.db should still exist")

	feature2Path := filepath.Join(actualCachePath, "branches", "feature-2.db")
	_, err = os.Stat(feature2Path)
	assert.NoError(t, err, "feature-2.db should still exist")
}

func TestRunClean_AllFlag_DeletesEntireCache(t *testing.T) {
	// Setup test cache with multiple branches
	projectPath, cachePath := setupTestCache(t, []string{"main", "feature-1", "feature-2"})

	// Save original working directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	// Change to project directory
	err = os.Chdir(projectPath)
	require.NoError(t, err)

	// Set flags
	cleanQuietFlag = true
	cleanAllFlag = true
	defer func() {
		cleanQuietFlag = false
		cleanAllFlag = false
	}()

	// Run clean with --all flag
	err = runClean(nil, nil)
	require.NoError(t, err)

	// Verify entire cache directory was deleted
	_, err = os.Stat(cachePath)
	assert.True(t, os.IsNotExist(err), "entire cache directory should be deleted")
}

func TestRunClean_MissingCacheDirectory(t *testing.T) {
	// Create project directory without cache
	projectPath := t.TempDir()
	initGitRepo(t, projectPath)

	// Save original working directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	// Change to project directory
	err = os.Chdir(projectPath)
	require.NoError(t, err)

	// Set quiet flag
	cleanQuietFlag = true
	cleanAllFlag = false
	defer func() { cleanQuietFlag = false }()

	// Run clean - should not error when cache doesn't exist
	err = runClean(nil, nil)
	assert.NoError(t, err, "should handle missing cache gracefully")
}

func TestRunClean_MissingBranchDatabase(t *testing.T) {
	// Setup test cache but don't create main.db
	projectPath, _ := setupTestCache(t, []string{"feature-1"})

	// Save original working directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	// Change to project directory
	err = os.Chdir(projectPath)
	require.NoError(t, err)

	// Set quiet flag
	cleanQuietFlag = true
	cleanAllFlag = false
	defer func() { cleanQuietFlag = false }()

	// Run clean - should not error when current branch DB doesn't exist
	err = runClean(nil, nil)
	assert.NoError(t, err, "should handle missing branch database gracefully")

	// Get actual cache path after runClean
	actualCachePath, err := cache.EnsureCacheLocation(projectPath)
	require.NoError(t, err)

	// Verify feature-1.db still exists
	feature1Path := filepath.Join(actualCachePath, "branches", "feature-1.db")
	_, err = os.Stat(feature1Path)
	assert.NoError(t, err, "feature-1.db should still exist")
}

func TestGetCacheStats_CorrectCalculation(t *testing.T) {
	// Create test cache directory
	cachePath := t.TempDir()
	branchesDir := filepath.Join(cachePath, "branches")
	err := os.MkdirAll(branchesDir, 0755)
	require.NoError(t, err)

	// Create test database files with known sizes
	testFiles := map[string]int{
		"main.db":      1024 * 1024,      // 1 MB
		"feature-1.db": 2 * 1024 * 1024,  // 2 MB
		"feature-2.db": 512 * 1024,       // 0.5 MB
	}

	for name, size := range testFiles {
		path := filepath.Join(branchesDir, name)
		data := make([]byte, size)
		err := os.WriteFile(path, data, 0644)
		require.NoError(t, err)
	}

	// Calculate stats
	totalSizeMB, branchCount, err := getCacheStats(cachePath)
	require.NoError(t, err)

	// Verify results
	assert.Equal(t, 3, branchCount, "should count 3 branches")
	expectedSizeMB := float64(1 + 2 + 0.5)
	assert.InDelta(t, expectedSizeMB, totalSizeMB, 0.01, "total size should be 3.5 MB")
}

func TestGetCacheStats_IgnoresNonDBFiles(t *testing.T) {
	// Create test cache directory
	cachePath := t.TempDir()
	branchesDir := filepath.Join(cachePath, "branches")
	err := os.MkdirAll(branchesDir, 0755)
	require.NoError(t, err)

	// Create test files including non-.db files
	testFiles := []struct {
		name  string
		size  int
		count bool // should this file be counted?
	}{
		{"main.db", 1024 * 1024, true},
		{"feature.db", 1024 * 1024, true},
		{"metadata.json", 1024, false},  // should be ignored
		{"temp.txt", 2048, false},        // should be ignored
		{".DS_Store", 4096, false},       // should be ignored
	}

	for _, tf := range testFiles {
		path := filepath.Join(branchesDir, tf.name)
		data := make([]byte, tf.size)
		err := os.WriteFile(path, data, 0644)
		require.NoError(t, err)
	}

	// Calculate stats
	totalSizeMB, branchCount, err := getCacheStats(cachePath)
	require.NoError(t, err)

	// Verify only .db files were counted
	assert.Equal(t, 2, branchCount, "should only count .db files")
	expectedSizeMB := float64(2) // 2 MB total from the two .db files
	assert.InDelta(t, expectedSizeMB, totalSizeMB, 0.01, "should only count .db file sizes")
}

func TestGetCacheStats_EmptyBranchesDirectory(t *testing.T) {
	// Create test cache directory with no branches
	cachePath := t.TempDir()
	branchesDir := filepath.Join(cachePath, "branches")
	err := os.MkdirAll(branchesDir, 0755)
	require.NoError(t, err)

	// Calculate stats
	totalSizeMB, branchCount, err := getCacheStats(cachePath)
	require.NoError(t, err)

	// Verify results
	assert.Equal(t, 0, branchCount, "should count 0 branches")
	assert.Equal(t, 0.0, totalSizeMB, "total size should be 0")
}

func TestGetCacheStats_MissingBranchesDirectory(t *testing.T) {
	// Create cache directory but no branches subdirectory
	cachePath := t.TempDir()

	// Calculate stats - should return error
	_, _, err := getCacheStats(cachePath)
	assert.Error(t, err, "should error when branches directory doesn't exist")
}
