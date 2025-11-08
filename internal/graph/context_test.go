package graph

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB creates an in-memory SQLite database with test data.
func setupContextTestDB(t *testing.T, content string) (*sql.DB, *ContextExtractor) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Create files table
	_, err = db.Exec(`
		CREATE TABLE files (
			file_path TEXT PRIMARY KEY,
			content TEXT
		)
	`)
	require.NoError(t, err)

	// Insert test file
	_, err = db.Exec(`INSERT INTO files (file_path, content) VALUES (?, ?)`, "test.go", content)
	require.NoError(t, err)

	return db, NewContextExtractor(db)
}

func TestContextExtractor_Basic(t *testing.T) {
	t.Parallel()

	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
}
`

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	// Target: fmt.Println line (line 6)
	// Byte positions calculated from content:
	// "package main\n\nimport \"fmt\"\n\nfunc main() {\n\t" = 48 bytes
	// "fmt.Println(\"Hello, world!\")" = 28 bytes
	// End position: 48 + 28 = 76

	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 6, End: 6},
		ByteRange{Start: 48, End: 76},
		3, // 3 context lines
	)

	require.NoError(t, err)
	assert.Contains(t, result, "// Lines")
	assert.Contains(t, result, "fmt.Println")
	assert.Contains(t, result, "func main()")
	assert.Contains(t, result, "import \"fmt\"")
}

func TestContextExtractor_ZeroContextLines(t *testing.T) {
	t.Parallel()

	content := `package main

func foo() {
	bar()
}

func bar() {
	baz()
}
`

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	// Target: bar() call (line 4)
	// Byte position: "package main\n\nfunc foo() {\n\t" = 28 bytes
	// "bar()" = 5 bytes
	// End: 28 + 5 = 33

	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 4, End: 4},
		ByteRange{Start: 28, End: 33},
		0, // No context lines
	)

	require.NoError(t, err)
	assert.Contains(t, result, "bar()")
	// With 0 context and overfetch safety, might include immediate surrounding lines
}

func TestContextExtractor_LargeContextLines(t *testing.T) {
	t.Parallel()

	content := `line 1
line 2
line 3
line 4
line 5
line 6
line 7
line 8
line 9
line 10
`

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	// Target: line 5 (middle)
	// Byte position: "line 1\nline 2\nline 3\nline 4\n" = 28 bytes
	// "line 5" = 6 bytes
	// End: 28 + 6 = 34

	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 5, End: 5},
		ByteRange{Start: 28, End: 34},
		10, // Large context
	)

	require.NoError(t, err)
	assert.Contains(t, result, "line 1") // Should include all lines due to large context
	assert.Contains(t, result, "line 5")
	assert.Contains(t, result, "line 10")
}

func TestContextExtractor_FileStart(t *testing.T) {
	t.Parallel()

	content := `package main

import "fmt"

func main() {
	fmt.Println("start")
}
`

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	// Target: package declaration (line 1)
	// Byte position: 0
	// "package main" = 12 bytes
	// End: 12

	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 1, End: 1},
		ByteRange{Start: 0, End: 12},
		3, // 3 context lines (should clamp to start)
	)

	require.NoError(t, err)
	assert.Contains(t, result, "package main")
	assert.Contains(t, result, "import \"fmt\"")
	// Should start from line 1 (clamped)
}

func TestContextExtractor_FileEnd(t *testing.T) {
	t.Parallel()

	content := `package main

func main() {
	println("last line")
}
`

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	// Target: closing brace (last line 5)
	// Byte position: everything up to "}"
	// Calculate: "package main\n\nfunc main() {\n\tprintln(\"last line\")\n" = 48 bytes
	// "}" = 1 byte
	// End: 48 + 1 = 49

	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 5, End: 5},
		ByteRange{Start: 48, End: 49},
		3, // 3 context lines (should clamp to end)
	)

	require.NoError(t, err)
	assert.Contains(t, result, "}")
	assert.Contains(t, result, "println")
	assert.Contains(t, result, "func main()")
}

func TestContextExtractor_MultiByteCharacters(t *testing.T) {
	t.Parallel()

	content := `package main

// 你好世界
func greet() {
	msg := "Hello 🌍"
	println(msg)
}
`

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	// Target: msg assignment (line 5)
	// Note: UTF-8 byte positions differ from character positions
	// "package main\n\n// 你好世界\nfunc greet() {\n\t" = variable bytes due to UTF-8
	// Calculate bytes precisely
	prefix := "package main\n\n// 你好世界\nfunc greet() {\n\t"
	startPos := len(prefix)
	text := `msg := "Hello 🌍"`
	endPos := startPos + len(text)

	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 5, End: 5},
		ByteRange{Start: startPos, End: endPos},
		2, // 2 context lines
	)

	require.NoError(t, err)
	assert.Contains(t, result, "Hello 🌍")
	assert.Contains(t, result, "你好世界")
	assert.Contains(t, result, "func greet()")
}

func TestContextExtractor_MissingFile(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Create empty table
	_, err = db.Exec(`
		CREATE TABLE files (
			file_path TEXT PRIMARY KEY,
			content TEXT
		)
	`)
	require.NoError(t, err)

	extractor := NewContextExtractor(db)

	_, err = extractor.ExtractContext(
		"nonexistent.go",
		LineRange{Start: 1, End: 1},
		ByteRange{Start: 0, End: 10},
		3,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "extract chunk")
}

func TestContextExtractor_BinaryFile(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Create table and insert binary file (content = NULL)
	_, err = db.Exec(`
		CREATE TABLE files (
			file_path TEXT PRIMARY KEY,
			content TEXT
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO files (file_path, content) VALUES (?, NULL)`, "binary.bin")
	require.NoError(t, err)

	extractor := NewContextExtractor(db)

	_, err = extractor.ExtractContext(
		"binary.bin",
		LineRange{Start: 1, End: 1},
		ByteRange{Start: 0, End: 10},
		3,
	)

	// Should error because content is NULL
	assert.Error(t, err)
}

func TestContextExtractor_MultiLineSpan(t *testing.T) {
	t.Parallel()

	content := `package main

type Config struct {
	Host string
	Port int
	Timeout int
}

func main() {}
`

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	// Target: entire struct definition (lines 3-7)
	// Byte position: "package main\n\n" = 14 bytes
	// "type Config struct {\n\tHost string\n\tPort int\n\tTimeout int\n}" = variable
	prefix := "package main\n\n"
	startPos := len(prefix)
	structDef := "type Config struct {\n\tHost string\n\tPort int\n\tTimeout int\n}"
	endPos := startPos + len(structDef)

	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 3, End: 7},
		ByteRange{Start: startPos, End: endPos},
		1, // 1 context line
	)

	require.NoError(t, err)
	assert.Contains(t, result, "type Config struct")
	assert.Contains(t, result, "Host string")
	assert.Contains(t, result, "Port int")
	assert.Contains(t, result, "Timeout int")
	// With 1 context line and a 5-line struct (lines 3-7), we get lines 2-8
	// Line 2 is blank, Line 8 is blank, Line 9 would be "func main()" but outside range
	// So we won't see "package main" (line 1) or "func main()" (line 9)
}

func TestContextExtractor_EmptyFile(t *testing.T) {
	t.Parallel()

	content := ""

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	// Extracting from empty file should work but return minimal content
	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 1, End: 1},
		ByteRange{Start: 0, End: 0},
		3,
	)

	// Should succeed with empty or minimal content
	require.NoError(t, err)
	assert.Contains(t, result, "// Lines")
}

func TestContextExtractor_SingleLineFile(t *testing.T) {
	t.Parallel()

	content := "package main"

	db, extractor := setupContextTestDB(t, content)
	defer db.Close()

	result, err := extractor.ExtractContext(
		"test.go",
		LineRange{Start: 1, End: 1},
		ByteRange{Start: 0, End: 12},
		5, // Large context should clamp to single line
	)

	require.NoError(t, err)
	assert.Contains(t, result, "package main")
}
