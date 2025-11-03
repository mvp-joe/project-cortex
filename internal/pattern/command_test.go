package pattern

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       *PatternRequest
		expectErr string
	}{
		{
			name:      "nil request",
			req:       nil,
			expectErr: "request cannot be nil",
		},
		{
			name: "missing pattern",
			req: &PatternRequest{
				Language: "go",
			},
			expectErr: "pattern is required",
		},
		{
			name: "missing language",
			req: &PatternRequest{
				Pattern: "test",
			},
			expectErr: "language is required",
		},
		{
			name: "unsupported language",
			req: &PatternRequest{
				Pattern:  "test",
				Language: "cobol",
			},
			expectErr: "unsupported language: cobol",
		},
		{
			name: "invalid strictness",
			req: &PatternRequest{
				Pattern:    "test",
				Language:   "go",
				Strictness: "super-strict",
			},
			expectErr: "invalid strictness: super-strict",
		},
		{
			name: "context lines too low",
			req: &PatternRequest{
				Pattern:      "test",
				Language:     "go",
				ContextLines: intPtr(-1),
			},
			expectErr: "context_lines must be between 0 and 10",
		},
		{
			name: "context lines too high",
			req: &PatternRequest{
				Pattern:      "test",
				Language:     "go",
				ContextLines: intPtr(11),
			},
			expectErr: "context_lines must be between 0 and 10",
		},
		{
			name: "limit too low",
			req: &PatternRequest{
				Pattern:  "test",
				Language: "go",
				Limit:    intPtr(0),
			},
			expectErr: "limit must be between 1 and 100",
		},
		{
			name: "limit too high",
			req: &PatternRequest{
				Pattern:  "test",
				Language: "go",
				Limit:    intPtr(101),
			},
			expectErr: "limit must be between 1 and 100",
		},
		{
			name: "valid minimal request",
			req: &PatternRequest{
				Pattern:  "defer $FUNC()",
				Language: "go",
			},
			expectErr: "",
		},
		{
			name: "valid full request",
			req: &PatternRequest{
				Pattern:      "defer $FUNC()",
				Language:     "go",
				FilePaths:    []string{"internal/**/*.go"},
				ContextLines: intPtr(5),
				Strictness:   "smart",
				Limit:        intPtr(50),
			},
			expectErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRequest(tt.req)
			if tt.expectErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRequest_AllLanguages(t *testing.T) {
	t.Parallel()

	languages := []string{
		"go", "typescript", "javascript", "tsx", "jsx",
		"python", "rust", "c", "cpp", "java", "php", "ruby",
	}

	for _, lang := range languages {
		t.Run(lang, func(t *testing.T) {
			t.Parallel()

			req := &PatternRequest{
				Pattern:  "test",
				Language: lang,
			}
			err := ValidateRequest(req)
			require.NoError(t, err)
		})
	}
}

func TestValidateRequest_AllStrictnessLevels(t *testing.T) {
	t.Parallel()

	strictnessLevels := []string{"cst", "smart", "ast", "relaxed", "signature"}

	for _, strictness := range strictnessLevels {
		t.Run(strictness, func(t *testing.T) {
			t.Parallel()

			req := &PatternRequest{
				Pattern:    "test",
				Language:   "go",
				Strictness: strictness,
			}
			err := ValidateRequest(req)
			require.NoError(t, err)
		})
	}
}

func TestBuildCommand_Basic(t *testing.T) {
	t.Parallel()

	req := &PatternRequest{
		Pattern:  "defer $FUNC()",
		Language: "go",
	}

	args, err := BuildCommand(req, "/tmp/project")
	require.NoError(t, err)

	expected := []string{
		"--pattern", "defer $FUNC()",
		"--lang", "go",
		"--json",
		"-C", "3", // default context lines
		"--strictness", "smart", // default strictness
		".",
	}
	assert.Equal(t, expected, args)
}

func TestBuildCommand_CustomParameters(t *testing.T) {
	t.Parallel()

	req := &PatternRequest{
		Pattern:      "useState($INIT)",
		Language:     "tsx",
		ContextLines: intPtr(5),
		Strictness:   "relaxed",
		FilePaths:    []string{"src/**/*.tsx"},
	}

	args, err := BuildCommand(req, "/tmp/project")
	require.NoError(t, err)

	expected := []string{
		"--pattern", "useState($INIT)",
		"--lang", "tsx",
		"--json",
		"-C", "5",
		"--strictness", "relaxed",
		"--globs", "src/**/*.tsx",
		".",
	}
	assert.Equal(t, expected, args)
}

func TestBuildCommand_ZeroContextLines(t *testing.T) {
	t.Parallel()

	req := &PatternRequest{
		Pattern:      "test",
		Language:     "go",
		ContextLines: intPtr(0),
	}

	args, err := BuildCommand(req, "/tmp/project")
	require.NoError(t, err)

	// Should NOT include -C flag when context lines is 0
	assert.NotContains(t, args, "-C")
	assert.NotContains(t, args, "0")
}

func TestBuildCommand_MultipleFilePaths(t *testing.T) {
	t.Parallel()

	req := &PatternRequest{
		Pattern:   "test",
		Language:  "go",
		FilePaths: []string{"internal/**/*.go", "cmd/**/*.go", "pkg/**/*.go"},
	}

	args, err := BuildCommand(req, "/tmp/project")
	require.NoError(t, err)

	// Find the --globs argument
	globsIdx := -1
	for i, arg := range args {
		if arg == "--globs" {
			globsIdx = i
			break
		}
	}
	require.NotEqual(t, -1, globsIdx, "should have --globs flag")
	assert.Equal(t, "internal/**/*.go,cmd/**/*.go,pkg/**/*.go", args[globsIdx+1])
}

func TestBuildCommand_InvalidRequest(t *testing.T) {
	t.Parallel()

	req := &PatternRequest{
		Language: "go", // missing pattern
	}

	_, err := BuildCommand(req, "/tmp/project")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pattern is required")
}

func TestBuildCommand_RelativeProjectRoot(t *testing.T) {
	t.Parallel()

	req := &PatternRequest{
		Pattern:  "test",
		Language: "go",
	}

	_, err := BuildCommand(req, "relative/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project root must be absolute path")
}

// Security Tests - Path Traversal Prevention
func TestBuildCommand_PathTraversal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filePaths   []string
		shouldErr   bool
		errContains string
	}{
		{
			name:      "normal relative path",
			filePaths: []string{"internal/**/*.go"},
			shouldErr: false,
		},
		{
			name:      "normal nested path",
			filePaths: []string{"src/components/auth/**/*.tsx"},
			shouldErr: false,
		},
		{
			name:        "absolute path",
			filePaths:   []string{"/etc/passwd"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "parent traversal simple",
			filePaths:   []string{"../../../etc/passwd"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "parent traversal with prefix",
			filePaths:   []string{"internal/../../../etc/passwd"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "parent then child",
			filePaths:   []string{"../other-project/file.go"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "parent from nested",
			filePaths:   []string{"internal/../../outside.go"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "multiple paths one bad",
			filePaths:   []string{"internal/**/*.go", "../bad.txt"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "all bad paths",
			filePaths:   []string{"../../../etc/passwd", "../../other/file.go"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:      "parent within project",
			filePaths: []string{"internal/../cmd/**/*.go"},
			shouldErr: false, // resolves to cmd/**/*.go which is fine
		},
		{
			name:      "current directory reference",
			filePaths: []string{"./internal/**/*.go"},
			shouldErr: false,
		},
		{
			name:      "empty path",
			filePaths: []string{""},
			shouldErr: false, // empty paths are technically safe (though useless)
		},
	}

	projectRoot := "/tmp/test-project"
	if runtime.GOOS == "windows" {
		projectRoot = "C:\\tmp\\test-project"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &PatternRequest{
				Pattern:   "test",
				Language:  "go",
				FilePaths: tt.filePaths,
			}

			args, err := BuildCommand(req, projectRoot)
			if tt.shouldErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, args)
			}
		})
	}
}

// Security Tests - Additional Edge Cases
func TestBuildCommand_PathTraversalEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		projectRoot string
		filePaths   []string
		shouldErr   bool
		errContains string
	}{
		{
			name:        "absolute path on unix",
			projectRoot: "/home/user/project",
			filePaths:   []string{"/etc/passwd"},
			shouldErr:   true,
			errContains: "absolute paths not allowed",
		},
		{
			name:        "escaping with many parents",
			projectRoot: "/home/user/project",
			filePaths:   []string{"../../../../../../../../etc/passwd"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "mixed valid and invalid",
			projectRoot: "/home/user/project",
			filePaths:   []string{"internal/*.go", "../../etc/passwd"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "URL-like path",
			projectRoot: "/home/user/project",
			filePaths:   []string{"http://example.com/file.go"},
			shouldErr:   false, // Not our job to validate URL syntax, just traversal
		},
		{
			name:        "sibling directory reference",
			projectRoot: "/home/user/project",
			filePaths:   []string{"../sibling/file.go"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Skip test if running on Windows and test uses Unix paths
			if runtime.GOOS == "windows" && filepath.Separator != tt.projectRoot[0] {
				t.Skip("skipping unix path test on windows")
			}

			req := &PatternRequest{
				Pattern:   "test",
				Language:  "go",
				FilePaths: tt.filePaths,
			}

			args, err := BuildCommand(req, tt.projectRoot)
			if tt.shouldErr {
				require.Error(t, err, "expected error for paths: %v", tt.filePaths)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, args)
			}
		})
	}
}

// Windows-specific path tests
func TestBuildCommand_WindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skipping windows-specific tests")
	}
	t.Parallel()

	tests := []struct {
		name        string
		filePaths   []string
		shouldErr   bool
		errContains string
	}{
		{
			name:        "windows absolute path",
			filePaths:   []string{"C:\\Windows\\System32\\config"},
			shouldErr:   true,
			errContains: "absolute paths not allowed",
		},
		{
			name:        "windows UNC path",
			filePaths:   []string{"\\\\server\\share\\file.go"},
			shouldErr:   true,
			errContains: "absolute paths not allowed",
		},
		{
			name:      "windows relative path",
			filePaths: []string{"internal\\**\\*.go"},
			shouldErr: false,
		},
		{
			name:        "windows parent traversal",
			filePaths:   []string{"..\\..\\..\\Windows\\System32"},
			shouldErr:   true,
			errContains: "path outside project root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &PatternRequest{
				Pattern:   "test",
				Language:  "go",
				FilePaths: tt.filePaths,
			}

			_, err := BuildCommand(req, "C:\\Users\\test\\project")
			if tt.shouldErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateFilePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		projectRoot string
		shouldErr   bool
		errContains string
	}{
		{
			name:        "absolute path",
			path:        "/etc/passwd",
			projectRoot: "/tmp/project",
			shouldErr:   true,
			errContains: "absolute paths not allowed",
		},
		{
			name:        "parent traversal",
			path:        "../../../etc/passwd",
			projectRoot: "/tmp/project",
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "relative outside",
			path:        "../other/file.go",
			projectRoot: "/tmp/project",
			shouldErr:   true,
			errContains: "path outside project root",
		},
		{
			name:        "valid relative",
			path:        "internal/pattern/command.go",
			projectRoot: "/tmp/project",
			shouldErr:   false,
		},
		{
			name:        "valid with dot",
			path:        "./internal/pattern/command.go",
			projectRoot: "/tmp/project",
			shouldErr:   false,
		},
		{
			name:        "valid glob",
			path:        "**/*.go",
			projectRoot: "/tmp/project",
			shouldErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Skip Windows path tests on Unix and vice versa
			if runtime.GOOS == "windows" && tt.projectRoot[0] == '/' {
				t.Skip("skipping unix path test on windows")
			}

			err := validateFilePath(tt.path, tt.projectRoot)
			if tt.shouldErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetContextLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      *PatternRequest
		expected int
	}{
		{
			name: "nil context lines",
			req: &PatternRequest{
				Pattern:  "test",
				Language: "go",
			},
			expected: DefaultContextLines,
		},
		{
			name: "zero context lines",
			req: &PatternRequest{
				Pattern:      "test",
				Language:     "go",
				ContextLines: intPtr(0),
			},
			expected: 0,
		},
		{
			name: "custom context lines",
			req: &PatternRequest{
				Pattern:      "test",
				Language:     "go",
				ContextLines: intPtr(7),
			},
			expected: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := GetContextLines(tt.req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      *PatternRequest
		expected int
	}{
		{
			name: "nil limit",
			req: &PatternRequest{
				Pattern:  "test",
				Language: "go",
			},
			expected: DefaultLimit,
		},
		{
			name: "custom limit",
			req: &PatternRequest{
				Pattern:  "test",
				Language: "go",
				Limit:    intPtr(25),
			},
			expected: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := GetLimit(tt.req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetStrictness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      *PatternRequest
		expected string
	}{
		{
			name: "empty strictness",
			req: &PatternRequest{
				Pattern:  "test",
				Language: "go",
			},
			expected: DefaultStrictness,
		},
		{
			name: "custom strictness",
			req: &PatternRequest{
				Pattern:    "test",
				Language:   "go",
				Strictness: "relaxed",
			},
			expected: "relaxed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := GetStrictness(tt.req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper function for test readability
func intPtr(i int) *int {
	return &i
}
