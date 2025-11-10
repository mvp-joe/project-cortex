package daemon

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsConnectionError_ConnectionRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "exact match",
			err:  errors.New("connection refused"),
		},
		{
			name: "dial error with connection refused",
			err:  fmt.Errorf("dial unix /tmp/daemon.sock: connect: connection refused"),
		},
		{
			name: "wrapped error",
			err:  fmt.Errorf("failed to connect: %w", errors.New("connection refused")),
		},
		{
			name: "TCP connection refused",
			err:  errors.New("dial tcp 127.0.0.1:8080: connect: connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, IsConnectionError(tt.err), "expected connection refused to be detected")
		})
	}
}

func TestIsConnectionError_NoSuchFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "exact match",
			err:  errors.New("no such file or directory"),
		},
		{
			name: "dial error with no such file",
			err:  fmt.Errorf("dial unix /tmp/daemon.sock: connect: no such file or directory"),
		},
		{
			name: "socket file missing",
			err:  errors.New("stat /var/run/daemon.sock: no such file or directory"),
		},
		{
			name: "wrapped error",
			err:  fmt.Errorf("failed to connect: %w", errors.New("no such file or directory")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, IsConnectionError(tt.err), "expected no such file to be detected")
		})
	}
}

func TestIsConnectionError_BrokenPipe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "exact match",
			err:  errors.New("broken pipe"),
		},
		{
			name: "write error with broken pipe",
			err:  fmt.Errorf("write unix /tmp/daemon.sock: write: broken pipe"),
		},
		{
			name: "wrapped error",
			err:  fmt.Errorf("failed to send request: %w", errors.New("broken pipe")),
		},
		{
			name: "connection closed mid-communication",
			err:  errors.New("read tcp 127.0.0.1:8080: broken pipe"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, IsConnectionError(tt.err), "expected broken pipe to be detected")
		})
	}
}

func TestIsConnectionError_NilError(t *testing.T) {
	t.Parallel()

	assert.False(t, IsConnectionError(nil), "expected nil error to return false")
}

func TestIsConnectionError_OtherError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "timeout error",
			err:  errors.New("context deadline exceeded"),
		},
		{
			name: "generic network error",
			err:  errors.New("network unreachable"),
		},
		{
			name: "application error",
			err:  errors.New("invalid request"),
		},
		{
			name: "permission denied",
			err:  errors.New("permission denied"),
		},
		{
			name: "similar but different",
			err:  errors.New("refused connection"), // Different order
		},
		{
			name: "partial match",
			err:  errors.New("connection"), // Not "connection refused"
		},
		{
			name: "EOF error",
			err:  errors.New("EOF"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.False(t, IsConnectionError(tt.err), "expected non-connection error to return false")
		})
	}
}

// TestIsConnectionError_AllCases is a comprehensive table test covering all scenarios
func TestIsConnectionError_AllCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		// Nil error
		{"nil error", nil, false},

		// Connection refused
		{"connection refused", errors.New("connection refused"), true},
		{"dial with connection refused", fmt.Errorf("dial: connection refused"), true},

		// No such file or directory
		{"no such file", errors.New("no such file or directory"), true},
		{"socket missing", fmt.Errorf("dial unix: no such file or directory"), true},

		// Broken pipe
		{"broken pipe", errors.New("broken pipe"), true},
		{"write with broken pipe", fmt.Errorf("write: broken pipe"), true},

		// Other errors (should return false)
		{"timeout", errors.New("timeout"), false},
		{"EOF", errors.New("EOF"), false},
		{"permission denied", errors.New("permission denied"), false},
		{"invalid argument", errors.New("invalid argument"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := IsConnectionError(tt.err)
			assert.Equal(t, tt.expected, actual, "IsConnectionError(%v) = %v, want %v", tt.err, actual, tt.expected)
		})
	}
}
