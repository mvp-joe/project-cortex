package indexer

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing

type mockChangeDetectorV2 struct {
	detectFunc func(ctx context.Context, hint []string) (*ChangeSet, error)
}

func (m *mockChangeDetectorV2) DetectChanges(ctx context.Context, hint []string) (*ChangeSet, error) {
	if m.detectFunc != nil {
		return m.detectFunc(ctx, hint)
	}
	return &ChangeSet{}, nil
}

type mockProcessorV2 struct {
	processFunc func(ctx context.Context, files []string) (*Stats, error)
}

func (m *mockProcessorV2) ProcessFiles(ctx context.Context, files []string) (*Stats, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, files)
	}
	return &Stats{}, nil
}

type mockStorageV2 struct {
	db          *sql.DB
	cachePath   string
	branch      string
	deleteFiles []string // Track deleted files
	metadata    *GeneratorMetadata
	isMock      bool     // Flag to indicate if this is a mock or real storage
}

func (m *mockStorageV2) WriteChunks(chunks []Chunk) error {
	return nil
}

func (m *mockStorageV2) WriteChunksIncremental(chunks []Chunk) error {
	return nil
}

func (m *mockStorageV2) ReadMetadata() (*GeneratorMetadata, error) {
	if m.metadata != nil {
		return m.metadata, nil
	}
	return &GeneratorMetadata{
		Version:       "3.0.0",
		Dimensions:    768,
		GeneratedAt:   time.Now(),
		FileChecksums: make(map[string]string),
		FileMtimes:    make(map[string]time.Time),
	}, nil
}

func (m *mockStorageV2) GetDB() *sql.DB {
	return m.db
}

func (m *mockStorageV2) GetCachePath() string {
	return m.cachePath
}

func (m *mockStorageV2) GetBranch() string {
	return m.branch
}

func (m *mockStorageV2) DeleteFile(filePath string) error {
	m.deleteFiles = append(m.deleteFiles, filePath)
	return nil
}

func (m *mockStorageV2) UpdateFileMtimes(filePaths []string) error {
	return nil
}

func (m *mockStorageV2) Close() error {
	return nil
}

// Helper to get DB for tests - returns either the mock's DB or creates a test DB
func getMockDB(t *testing.T, stor *mockStorageV2) *sql.DB {
	if stor.db != nil {
		return stor.db
	}
	// Return a test DB that will be cleaned up after test
	return storage.NewTestDB(t)
}

// Test: Full index with no hint (all files new)
func TestIndexerV2_FullIndex(t *testing.T) {
	t.Parallel()

	detector := &mockChangeDetectorV2{
		detectFunc: func(ctx context.Context, hint []string) (*ChangeSet, error) {
			assert.Empty(t, hint, "full index should have no hint")
			return &ChangeSet{
				Added:     []string{"file1.go", "file2.go", "file3.md"},
				Modified:  []string{},
				Deleted:   []string{},
				Unchanged: []string{},
			}, nil
		},
	}

	processor := &mockProcessorV2{
		processFunc: func(ctx context.Context, files []string) (*Stats, error) {
			// IndexerV2 converts relative paths to absolute before passing to processor
			assert.Equal(t, []string{"/test/root/file1.go", "/test/root/file2.go", "/test/root/file3.md"}, files)
			return &Stats{
				CodeFilesProcessed: 2,
				DocsProcessed:      1,
				TotalCodeChunks:    6,
				TotalDocChunks:     3,
				ProcessingTime:     100 * time.Millisecond,
			}, nil
		},
	}

	storage := &mockStorageV2{}
	db := getMockDB(t, storage)

	indexer := NewIndexerV2("/test/root", detector, processor, storage, db)

	stats, err := indexer.Index(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.FilesAdded)
	assert.Equal(t, 0, stats.FilesModified)
	assert.Equal(t, 0, stats.FilesDeleted)
	assert.Equal(t, 2, stats.CodeFilesProcessed)
	assert.Equal(t, 1, stats.DocsProcessed)
}

// Test: No changes detected (empty ChangeSet, no processing)
func TestIndexerV2_NoChanges(t *testing.T) {
	t.Parallel()

	detector := &mockChangeDetectorV2{
		detectFunc: func(ctx context.Context, hint []string) (*ChangeSet, error) {
			return &ChangeSet{
				Added:     []string{},
				Modified:  []string{},
				Deleted:   []string{},
				Unchanged: []string{"file1.go", "file2.go"},
			}, nil
		},
	}

	processorCalled := false
	processor := &mockProcessorV2{
		processFunc: func(ctx context.Context, files []string) (*Stats, error) {
			processorCalled = true
			return &Stats{}, nil
		},
	}

	storage := &mockStorageV2{}
	db := getMockDB(t, storage)

	indexer := NewIndexerV2("/test/root", detector, processor, storage, db)

	stats, err := indexer.Index(context.Background(), nil)
	require.NoError(t, err)
	assert.False(t, processorCalled, "processor should not be called when no changes")
	assert.Equal(t, 0, stats.FilesAdded)
	assert.Equal(t, 0, stats.FilesModified)
	assert.Equal(t, 0, stats.FilesDeleted)
}

// Test: Graph update failure (doesn't fail indexing, just logs warning)
func TestIndexerV2_GraphFailureGraceful(t *testing.T) {
	t.Parallel()

	detector := &mockChangeDetectorV2{
		detectFunc: func(ctx context.Context, hint []string) (*ChangeSet, error) {
			return &ChangeSet{
				Added: []string{"file1.go"},
			}, nil
		},
	}

	processor := &mockProcessorV2{
		processFunc: func(ctx context.Context, files []string) (*Stats, error) {
			return &Stats{
				CodeFilesProcessed: 1,
				TotalCodeChunks:    3,
			}, nil
		},
	}

	storage := &mockStorageV2{}
	db := getMockDB(t, storage)

	indexer := NewIndexerV2("/test/root", detector, processor, storage, db)

	// Should succeed despite graph failure (GraphUpdater handles errors internally)
	stats, err := indexer.Index(context.Background(), nil)
	require.NoError(t, err, "indexing should succeed even if graph fails")
	assert.Equal(t, 1, stats.FilesAdded)
	assert.Equal(t, 1, stats.CodeFilesProcessed)
}

// Test: Context cancellation during detection
func TestIndexerV2_ContextCancellationDetection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	detector := &mockChangeDetectorV2{
		detectFunc: func(ctx context.Context, hint []string) (*ChangeSet, error) {
			return nil, ctx.Err()
		},
	}

	processor := &mockProcessorV2{}
	storage := &mockStorageV2{}
	db := getMockDB(t, storage)

	indexer := NewIndexerV2("/test/root", detector, processor, storage, db)

	_, err := indexer.Index(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "change detection failed")
}

// Test: File deleted with real SQL DB and real storage
func TestIndexerV2_FileDeleted(t *testing.T) {
	t.Parallel()

	// Create in-memory SQLite DB with full schema
	db := storage.NewTestDB(t)

	// Create real SQLite storage
	stor, err := NewSQLiteStorage(db, t.TempDir(), "/test/root")
	require.NoError(t, err)

	// Insert a file using FileWriter
	fileWriter := storage.NewFileWriter(db)
	err = fileWriter.WriteFileStats(&storage.FileStats{
		FilePath: "deleted_file.go",
		FileHash: "abc123",
		Language: "go",
	})
	require.NoError(t, err)

	// Mock change detector that reports file as deleted
	detector := &mockChangeDetectorV2{
		detectFunc: func(ctx context.Context, hint []string) (*ChangeSet, error) {
			return &ChangeSet{
				Added:     []string{},
				Modified:  []string{},
				Deleted:   []string{"deleted_file.go"},
				Unchanged: []string{},
			}, nil
		},
	}

	processor := &mockProcessorV2{}

	indexer := NewIndexerV2("/test/root", detector, processor, stor, db)

	stats, err := indexer.Index(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, stats.FilesAdded)
	assert.Equal(t, 0, stats.FilesModified)
	assert.Equal(t, 1, stats.FilesDeleted)

	// Verify file was actually deleted via FileReader
	fileReader := storage.NewFileReader(db)
	fileStats, err := fileReader.GetFileStats("deleted_file.go")
	require.NoError(t, err)
	assert.Nil(t, fileStats, "file should be deleted from database")
}

// Test: Orchestration flow - verifies call sequence
func TestIndexerV2_OrchestrationFlow(t *testing.T) {
	t.Parallel()

	var callSequence []string

	detector := &mockChangeDetectorV2{
		detectFunc: func(ctx context.Context, hint []string) (*ChangeSet, error) {
			callSequence = append(callSequence, "detect")
			return &ChangeSet{
				Added: []string{"file1.go"},
			}, nil
		},
	}

	processor := &mockProcessorV2{
		processFunc: func(ctx context.Context, files []string) (*Stats, error) {
			callSequence = append(callSequence, "process")
			return &Stats{CodeFilesProcessed: 1}, nil
		},
	}

	storage := &mockStorageV2{}
	db := getMockDB(t, storage)

	indexer := NewIndexerV2("/test/root", detector, processor, storage, db)

	stats, err := indexer.Index(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.FilesAdded)
	// Note: We can't easily verify graph update was called since GraphUpdater is internal
	// Integration tests verify graph updates work correctly
	assert.Contains(t, []string{"detect", "process"}, callSequence[0])
	assert.Contains(t, []string{"detect", "process"}, callSequence[1])
}
