package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan:
// 1. TestNewProjectsRegistry_CreatesDirectory - Verify registry creation creates ~/.cortex if missing
// 2. TestNewProjectsRegistry_LoadsExisting - Verify loading existing projects.json
// 3. TestRegister_AddsProject - Verify new project registration
// 4. TestRegister_Idempotent - Verify duplicate registration is safe
// 5. TestRegister_InvalidPath - Verify absolute path validation
// 6. TestRegister_NonExistentPath - Verify path existence validation
// 7. TestUnregister_RemovesProject - Verify project removal
// 8. TestUnregister_NonExistent - Verify unregistering non-existent project is safe
// 9. TestGet_ReturnsProject - Verify Get returns registered project
// 10. TestGet_NotFound - Verify Get returns false for non-existent project
// 11. TestList_ReturnsAllProjects - Verify List returns all projects
// 12. TestList_Empty - Verify List returns empty slice when no projects
// 13. TestUpdateLastIndexed - Verify LastIndexed update
// 14. TestUpdateLastIndexed_NonExistent - Verify updating non-existent project returns error
// 15. TestUpdateCacheKey - Verify CacheKey update
// 16. TestUpdateCacheKey_NonExistent - Verify updating non-existent project returns error
// 17. TestPersistence_AtomicWrites - Verify atomic file writes on updates
// 18. TestThreadSafety_ConcurrentAccess - Verify concurrent operations are safe

func TestNewProjectsRegistry_CreatesDirectory(t *testing.T) {
	t.Parallel()

	// Setup: Use temp directory for test isolation
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	// Verify directory doesn't exist initially
	_, err := os.Stat(cortexDir)
	require.True(t, os.IsNotExist(err))

	// Create registry
	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)
	require.NotNil(t, registry)

	// Verify directory was created
	info, err := os.Stat(cortexDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewProjectsRegistry_UsesHomeDir(t *testing.T) {
	t.Parallel()

	// Setup: Use temp directory to test with explicit cortex dir
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)
	require.NotNil(t, registry)

	// Verify we can list (even if empty)
	projects := registry.List()
	require.NotNil(t, projects)

	// Verify it created the directory
	_, err = os.Stat(cortexDir)
	require.NoError(t, err, ".cortex directory should be created")
}

func TestNewProjectsRegistry_LoadsExisting(t *testing.T) {
	t.Parallel()

	// Setup: Create existing registry file
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	projectsFile := filepath.Join(cortexDir, "projects.json")
	now := time.Now().UTC()
	existingData := registryFile{
		Projects: []*RegisteredProject{
			{
				Path:          "/test/project1",
				CacheKey:      "abc123-def456",
				RegisteredAt:  now,
				LastIndexedAt: now.Add(-1 * time.Hour),
			},
		},
	}

	data, err := json.MarshalIndent(existingData, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(projectsFile, data, 0644))

	// Load registry
	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Verify project was loaded
	proj, found := registry.Get("/test/project1")
	require.True(t, found)
	assert.Equal(t, "abc123-def456", proj.CacheKey)
	assert.Equal(t, now.Unix(), proj.RegisteredAt.Unix())
}

func TestRegister_AddsProject(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	// Create a real project directory to test against
	projectPath := filepath.Join(tempHome, "test-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Register project
	beforeRegister := time.Now().UTC()
	proj, err := registry.Register(projectPath)
	afterRegister := time.Now().UTC()

	require.NoError(t, err)
	require.NotNil(t, proj)

	// Verify project fields
	assert.Equal(t, projectPath, proj.Path)
	assert.NotEmpty(t, proj.CacheKey)
	assert.True(t, proj.RegisteredAt.After(beforeRegister) || proj.RegisteredAt.Equal(beforeRegister))
	assert.True(t, proj.RegisteredAt.Before(afterRegister) || proj.RegisteredAt.Equal(afterRegister))
	assert.True(t, proj.LastIndexedAt.IsZero()) // Not indexed yet

	// Verify project can be retrieved
	retrieved, found := registry.Get(projectPath)
	require.True(t, found)
	assert.Equal(t, proj.Path, retrieved.Path)
	assert.Equal(t, proj.CacheKey, retrieved.CacheKey)

	// Verify persistence
	registry2, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)
	retrieved2, found := registry2.Get(projectPath)
	require.True(t, found)
	assert.Equal(t, proj.CacheKey, retrieved2.CacheKey)
}

func TestRegister_Idempotent(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")
	projectPath := filepath.Join(tempHome, "test-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// First registration
	proj1, err := registry.Register(projectPath)
	require.NoError(t, err)

	// Second registration should return same project
	proj2, err := registry.Register(projectPath)
	require.NoError(t, err)

	assert.Equal(t, proj1.Path, proj2.Path)
	assert.Equal(t, proj1.CacheKey, proj2.CacheKey)
	assert.Equal(t, proj1.RegisteredAt.Unix(), proj2.RegisteredAt.Unix())

	// Verify only one project in registry
	projects := registry.List()
	assert.Len(t, projects, 1)
}

func TestRegister_InvalidPath(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Test relative path
	_, err = registry.Register("relative/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be absolute")
}

func TestRegister_NonExistentPath(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Test non-existent absolute path
	nonExistentPath := filepath.Join(tempHome, "does-not-exist")
	_, err = registry.Register(nonExistentPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestUnregister_RemovesProject(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")
	projectPath := filepath.Join(tempHome, "test-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Register project
	_, err = registry.Register(projectPath)
	require.NoError(t, err)

	// Verify project exists
	_, found := registry.Get(projectPath)
	require.True(t, found)

	// Unregister project
	err = registry.Unregister(projectPath)
	require.NoError(t, err)

	// Verify project removed
	_, found = registry.Get(projectPath)
	assert.False(t, found)

	// Verify persistence
	registry2, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)
	_, found = registry2.Get(projectPath)
	assert.False(t, found)
}

func TestUnregister_NonExistent(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Unregister non-existent project (should be safe/no-op)
	err = registry.Unregister("/test/does-not-exist")
	require.NoError(t, err)
}

func TestGet_ReturnsProject(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")
	projectPath := filepath.Join(tempHome, "test-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Register project
	registered, err := registry.Register(projectPath)
	require.NoError(t, err)

	// Get project
	proj, found := registry.Get(projectPath)
	require.True(t, found)
	assert.Equal(t, registered.Path, proj.Path)
	assert.Equal(t, registered.CacheKey, proj.CacheKey)
}

func TestGet_NotFound(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Get non-existent project
	_, found := registry.Get("/test/does-not-exist")
	assert.False(t, found)
}

func TestList_ReturnsAllProjects(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	project1Path := filepath.Join(tempHome, "project1")
	project2Path := filepath.Join(tempHome, "project2")
	require.NoError(t, os.MkdirAll(project1Path, 0755))
	require.NoError(t, os.MkdirAll(project2Path, 0755))

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Register projects
	_, err = registry.Register(project1Path)
	require.NoError(t, err)
	_, err = registry.Register(project2Path)
	require.NoError(t, err)

	// List projects
	projects := registry.List()
	require.Len(t, projects, 2)

	// Verify both projects are present
	paths := make(map[string]bool)
	for _, p := range projects {
		paths[p.Path] = true
	}
	assert.True(t, paths[project1Path])
	assert.True(t, paths[project2Path])
}

func TestList_Empty(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// List should return empty slice
	projects := registry.List()
	assert.Empty(t, projects)
}

func TestUpdateLastIndexed(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")
	projectPath := filepath.Join(tempHome, "test-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Register project
	_, err = registry.Register(projectPath)
	require.NoError(t, err)

	// Update last indexed time
	now := time.Now().UTC()
	err = registry.UpdateLastIndexed(projectPath, now)
	require.NoError(t, err)

	// Verify update
	proj, found := registry.Get(projectPath)
	require.True(t, found)
	assert.Equal(t, now.Unix(), proj.LastIndexedAt.Unix())

	// Verify persistence
	registry2, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)
	proj2, found := registry2.Get(projectPath)
	require.True(t, found)
	assert.Equal(t, now.Unix(), proj2.LastIndexedAt.Unix())
}

func TestUpdateLastIndexed_NonExistent(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Update non-existent project
	err = registry.UpdateLastIndexed("/test/does-not-exist", time.Now().UTC())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateCacheKey(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")
	projectPath := filepath.Join(tempHome, "test-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Register project
	proj, err := registry.Register(projectPath)
	require.NoError(t, err)
	originalCacheKey := proj.CacheKey

	// Update cache key
	newCacheKey := "new-key-12345678"
	err = registry.UpdateCacheKey(projectPath, newCacheKey)
	require.NoError(t, err)

	// Verify update
	proj, found := registry.Get(projectPath)
	require.True(t, found)
	assert.Equal(t, newCacheKey, proj.CacheKey)
	assert.NotEqual(t, originalCacheKey, proj.CacheKey)

	// Verify persistence
	registry2, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)
	proj2, found := registry2.Get(projectPath)
	require.True(t, found)
	assert.Equal(t, newCacheKey, proj2.CacheKey)
}

func TestUpdateCacheKey_NonExistent(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Update non-existent project
	err = registry.UpdateCacheKey("/test/does-not-exist", "new-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPersistence_AtomicWrites(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")
	projectPath := filepath.Join(tempHome, "test-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Register project
	_, err = registry.Register(projectPath)
	require.NoError(t, err)

	// Verify no temporary files remain
	files, err := filepath.Glob(filepath.Join(cortexDir, "*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, files, "no temporary files should remain after atomic write")

	// Verify projects.json exists
	projectsFile := filepath.Join(cortexDir, "projects.json")
	_, err = os.Stat(projectsFile)
	require.NoError(t, err)
}

func TestThreadSafety_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	// Setup
	tempHome := t.TempDir()
	cortexDir := filepath.Join(tempHome, ".cortex")

	// Create multiple project directories
	const numProjects = 20
	projectPaths := make([]string, numProjects)
	for i := 0; i < numProjects; i++ {
		projectPaths[i] = filepath.Join(tempHome, "project", string(rune('a'+i)))
		require.NoError(t, os.MkdirAll(projectPaths[i], 0755))
	}

	registry, err := newProjectsRegistryWithDir(cortexDir)
	require.NoError(t, err)

	// Concurrent operations
	var wg sync.WaitGroup
	wg.Add(numProjects * 4) // Register, Get, UpdateLastIndexed, List

	// Concurrent Register
	for i := 0; i < numProjects; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := registry.Register(projectPaths[i])
			assert.NoError(t, err)
		}()
	}

	// Concurrent Get
	for i := 0; i < numProjects; i++ {
		i := i
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond) // Let Register happen first
			registry.Get(projectPaths[i])
		}()
	}

	// Concurrent UpdateLastIndexed
	for i := 0; i < numProjects; i++ {
		i := i
		go func() {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond) // Let Register happen first
			registry.UpdateLastIndexed(projectPaths[i], time.Now().UTC())
		}()
	}

	// Concurrent List
	for i := 0; i < numProjects; i++ {
		go func() {
			defer wg.Done()
			registry.List()
		}()
	}

	wg.Wait()

	// Verify all projects were registered
	projects := registry.List()
	assert.Len(t, projects, numProjects)
}
