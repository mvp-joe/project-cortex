# Storage Package

Implements SQLite schema and cache management for Project Cortex's unified cache storage.

## Overview

The `storage` package provides the foundational database schema for consolidating all cache data (chunks, code graph, file statistics) into a single SQLite database per branch. This unified approach enables powerful cross-queries and eliminates the need for multiple storage systems.

## Files

- **schema.go** (359 lines) - Complete SQLite schema definition with 12 tables, 31 indexes, and schema versioning
- **schema_test.go** (411 lines) - Comprehensive test suite with 76.2% coverage

## Schema Architecture

### Tables (12 total)

1. **files** - Root table tracking all indexed files (natural PK: file_path)
2. **files_fts** - FTS5 virtual table for full-text search (cortex_exact tool)
3. **types** - Interfaces, structs, classes (FK → files)
4. **type_fields** - Struct fields, interface methods (FK → types)
5. **functions** - Standalone functions and methods (FK → files, optional FK → types)
6. **function_parameters** - Parameters and return values (FK → functions)
7. **type_relationships** - Implements, embeds edges (FK → types)
8. **function_calls** - Call graph edges (FK → functions)
9. **imports** - Import declarations (FK → files)
10. **chunks** - Semantic search chunks with embeddings (FK → files)
11. **modules** - Aggregated statistics per module/package
12. **cache_metadata** - Cache configuration and versioning

### Indexes (31 total)

All foreign keys are indexed for JOIN performance, plus additional indexes on:
- Filter columns (language, is_exported, is_test, etc.)
- Search columns (name, module_path, import_path)
- Relationship columns (from_type_id, to_type_id, caller_function_id, etc.)

### Foreign Key Constraints

- **CASCADE DELETE**: Child records deleted when parent deleted (types → files, chunks → files, etc.)
- **SET NULL**: Optional references nullified when target deleted (functions.receiver_type_id → types)
- All constraints enforced with `PRAGMA foreign_keys = ON`

## API

### CreateSchema

```go
func CreateSchema(db *sql.DB) error
```

Creates all tables, indexes, and virtual tables for the unified cache.
- Uses transaction for atomicity (all-or-nothing)
- Enables foreign key enforcement
- Bootstraps cache_metadata with defaults
- Creates FTS5 virtual table for full-text search

**Bootstrap metadata:**
- `schema_version`: "2.0"
- `branch`: "main"
- `last_indexed`: "" (empty until first index)
- `embedding_dimensions`: "384"

### GetSchemaVersion

```go
func GetSchemaVersion(db *sql.DB) (string, error)
```

Retrieves current schema version from cache_metadata.
- Returns "0" for new/uninitialized databases
- Returns version string for existing schemas

### UpdateSchemaVersion

```go
func UpdateSchemaVersion(db *sql.DB, version string) error
```

Updates schema version in cache_metadata.
- Uses UPSERT (INSERT OR REPLACE)
- Updates timestamp automatically

## Usage Example

```go
import (
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
    "github.com/mvp-joe/project-cortex/internal/storage"
)

// Open or create database
db, err := sql.Open("sqlite3", ".cortex/cache/main.db")
if err != nil {
    return err
}
defer db.Close()

// Check schema version
version, err := storage.GetSchemaVersion(db)
if err != nil {
    return err
}

if version == "0" {
    // New database - create schema
    if err := storage.CreateSchema(db); err != nil {
        return err
    }
} else if version != "2.0" {
    // Migration needed (future Phase)
    return fmt.Errorf("unsupported schema version: %s", version)
}
```

## Testing

Run tests with FTS5 enabled:

```bash
go test -v -tags fts5 ./internal/storage/
```

Test coverage: **76.2%**

### Test Suite

- **TestCreateSchema**: Verifies all tables created successfully
- **TestCreateSchema_ForeignKeys**: Tests CASCADE DELETE behavior
- **TestCreateSchema_ForeignKey_SetNull**: Tests SET NULL behavior
- **TestCreateSchema_Indexes**: Verifies all 31 indexes created
- **TestCreateSchema_BootstrapMetadata**: Validates initial metadata
- **TestCreateSchema_FTSTable**: Tests FTS5 search functionality
- **TestCreateSchema_UniqueConstraints**: Tests UNIQUE constraints
- **TestCreateSchema_ImportsUniqueConstraint**: Tests import uniqueness
- **TestGetSchemaVersion**: Tests version retrieval (new/existing DB)
- **TestUpdateSchemaVersion**: Tests version updates

## Design Decisions

### 1. Natural Primary Key for Files

Using `file_path` as PK instead of synthetic ID:
- ✅ More intuitive for debugging and introspection
- ✅ Simpler JOINs (no extra lookup needed)
- ✅ File paths already unique within repo
- ⚠️ Slightly longer than integer ID, but acceptable for SQLite

### 2. Denormalized Fields

Some fields duplicated for query performance:
- `module_path` in types and functions (avoid JOIN to files)
- `receiver_type_name` in functions (quick filtering without JOIN)
- `*_count` fields in types/functions/modules (aggregated stats)

Trade-off: Storage cost (~5-10% more) for query speed improvement (2-5x faster).

### 3. BLOB for Embeddings

Storing 384-dim float32 arrays as BLOB (1536 bytes):
- More efficient than JSON (~40% smaller)
- Faster serialization/deserialization
- Direct byte access with `math.Float32frombits()`

### 4. ISO 8601 Timestamps

Using TEXT for dates in ISO 8601 format (YYYY-MM-DDTHH:MM:SSZ):
- SQLite's datetime() functions work seamlessly
- Human-readable in SQL queries
- Timezone-aware (always UTC)
- Language-agnostic parsing

### 5. Boolean as INTEGER

SQLite convention: 0 = false, 1 = true:
- Native SQLite type system (no BOOLEAN type)
- Compatible with indexes
- Standard practice in SQLite ecosystem

## Future Phases

### Phase 5: sqlite-vec Integration

The schema includes a commented placeholder for vector search:

```sql
-- Vector index (requires sqlite-vec extension)
-- Uncomment when extension is loaded:
-- CREATE VIRTUAL TABLE vec_chunks USING vec0(
--     chunk_id TEXT PRIMARY KEY,
--     embedding FLOAT[384]
-- );
```

This will replace chromem-go for semantic search with native SQL queries.

### Migration System (Phase 6)

Future schema versions will use migration files:
- Forward-only migrations (2.0 → 2.1 → 3.0)
- Version tracking via cache_metadata.schema_version
- Automated migration on cortex startup

## Related Documentation

- **Spec**: [specs/2025-10-30_sqlite-cache-storage.md](/Users/josephward/code/project-cortex/specs/2025-10-30_sqlite-cache-storage.md)
- **Phase 2A**: Schema creation (this package)
- **Phase 2B**: File statistics writer
- **Phase 3**: Code graph writer
- **Phase 4**: Chunk writer with embeddings
- **Phase 5**: MCP query tools integration
