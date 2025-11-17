package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mvp-joe/project-cortex/internal/cache"
)

// RegisteredProject represents a project registered with the daemon for indexing.
type RegisteredProject struct {
	Path          string    `json:"path"`            // Absolute project path
	CacheKey      string    `json:"cache_key"`       // Cache key (remote-hash-worktree-hash)
	RegisteredAt  time.Time `json:"registered_at"`   // When project was first registered
	LastIndexedAt time.Time `json:"last_indexed_at"` // Last indexing completion time
}

// ProjectsRegistry manages the registry of watched projects.
// It provides thread-safe access to project registration state.
type ProjectsRegistry interface {
	Register(path string) (*RegisteredProject, error)
	Unregister(path string) error
	Get(path string) (*RegisteredProject, bool)
	List() []*RegisteredProject
	UpdateLastIndexed(path string, t time.Time) error
	UpdateCacheKey(path string, cacheKey string) error
}

// projectsRegistry is the concrete implementation of ProjectsRegistry.
type projectsRegistry struct {
	cortexDir string
	mu        sync.RWMutex
	projects  map[string]*RegisteredProject // keyed by absolute path
}

// registryFile represents the JSON structure persisted to disk.
type registryFile struct {
	Projects []*RegisteredProject `json:"projects"`
}

// NewProjectsRegistry creates a new projects registry.
// It loads existing projects from ~/.cortex/projects.json if present.
// Creates the ~/.cortex directory if it doesn't exist.
func NewProjectsRegistry() (ProjectsRegistry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	cortexDir := filepath.Join(home, ".cortex")
	return newProjectsRegistryWithDir(cortexDir)
}

// newProjectsRegistryWithDir creates a registry with a custom cortex directory.
// Used for testing with isolated temporary directories.
func newProjectsRegistryWithDir(cortexDir string) (ProjectsRegistry, error) {
	// Create cortex directory if missing
	if err := os.MkdirAll(cortexDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cortex directory: %w", err)
	}

	registry := &projectsRegistry{
		cortexDir: cortexDir,
		projects:  make(map[string]*RegisteredProject),
	}

	// Load existing registry
	if err := registry.load(); err != nil {
		return nil, err
	}

	return registry, nil
}

// Register adds a project to the registry.
// If the project is already registered, returns the existing registration (idempotent).
func (r *projectsRegistry) Register(path string) (*RegisteredProject, error) {
	// Validate path
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("project path must be absolute: %s", path)
	}

	// Check path exists
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("project path does not exist: %s", path)
		}
		return nil, fmt.Errorf("failed to stat project path: %w", err)
	}

	// Compute cache key BEFORE taking lock (runs git commands - slow!)
	cacheKey, err := cache.GetCacheKey(path)
	if err != nil {
		return nil, fmt.Errorf("failed to compute cache key: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already registered (idempotent)
	if existing, found := r.projects[path]; found {
		return existing, nil
	}

	// Create new registration
	now := time.Now().UTC()
	project := &RegisteredProject{
		Path:          path,
		CacheKey:      cacheKey,
		RegisteredAt:  now,
		LastIndexedAt: time.Time{}, // Zero value = not indexed yet
	}

	r.projects[path] = project

	// Persist to disk
	if err := r.save(); err != nil {
		// Rollback in-memory state on persistence failure
		delete(r.projects, path)
		return nil, fmt.Errorf("failed to persist registry: %w", err)
	}

	return project, nil
}

// Unregister removes a project from the registry.
// If the project is not registered, this is a no-op (idempotent).
func (r *projectsRegistry) Unregister(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if project exists
	if _, found := r.projects[path]; !found {
		return nil // Idempotent - not an error
	}

	delete(r.projects, path)

	// Persist to disk
	if err := r.save(); err != nil {
		return fmt.Errorf("failed to persist registry: %w", err)
	}

	return nil
}

// Get retrieves a project by path.
// Returns (project, true) if found, (nil, false) otherwise.
func (r *projectsRegistry) Get(path string) (*RegisteredProject, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	project, found := r.projects[path]
	if !found {
		return nil, false
	}

	// Return a copy to prevent external modification
	copy := *project
	return &copy, true
}

// List returns all registered projects.
// Returns a copy of the internal state to prevent external modification.
func (r *projectsRegistry) List() []*RegisteredProject {
	r.mu.RLock()
	defer r.mu.RUnlock()

	projects := make([]*RegisteredProject, 0, len(r.projects))
	for _, p := range r.projects {
		copy := *p
		projects = append(projects, &copy)
	}

	return projects
}

// UpdateLastIndexed updates the last indexed time for a project.
func (r *projectsRegistry) UpdateLastIndexed(path string, t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	project, found := r.projects[path]
	if !found {
		return fmt.Errorf("project not found: %s", path)
	}

	project.LastIndexedAt = t

	// Persist to disk
	if err := r.save(); err != nil {
		return fmt.Errorf("failed to persist registry: %w", err)
	}

	return nil
}

// UpdateCacheKey updates the cache key for a project.
func (r *projectsRegistry) UpdateCacheKey(path string, cacheKey string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	project, found := r.projects[path]
	if !found {
		return fmt.Errorf("project not found: %s", path)
	}

	project.CacheKey = cacheKey

	// Persist to disk
	if err := r.save(); err != nil {
		return fmt.Errorf("failed to persist registry: %w", err)
	}

	return nil
}

// load reads the registry from disk.
// If the file doesn't exist, initializes an empty registry (not an error).
func (r *projectsRegistry) load() error {
	registryPath := r.getRegistryPath()

	// Read file
	data, err := os.ReadFile(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - start with empty registry
			return nil
		}
		return fmt.Errorf("failed to read registry file: %w", err)
	}

	// Parse JSON
	var file registryFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("failed to parse registry file: %w", err)
	}

	// Load projects into map
	for _, p := range file.Projects {
		r.projects[p.Path] = p
	}

	return nil
}

// save writes the registry to disk using atomic writes.
// Writes to a temporary file first, then renames to prevent partial state.
func (r *projectsRegistry) save() error {
	registryPath := r.getRegistryPath()

	// Convert map to slice for JSON serialization
	projects := make([]*RegisteredProject, 0, len(r.projects))
	for _, p := range r.projects {
		projects = append(projects, p)
	}

	file := registryFile{
		Projects: projects,
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	// Write to temporary file
	tmpPath := registryPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary registry file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, registryPath); err != nil {
		os.Remove(tmpPath) // Clean up temp file on failure
		return fmt.Errorf("failed to rename temporary registry file: %w", err)
	}

	return nil
}

// getRegistryPath returns the path to the projects.json file.
func (r *projectsRegistry) getRegistryPath() string {
	return filepath.Join(r.cortexDir, "projects.json")
}
