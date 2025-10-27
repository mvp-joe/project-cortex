package embed

import (
	"bufio"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for localProvider.Close():
// - Graceful shutdown: Process exits cleanly on SIGTERM
// - Force kill on timeout: Process killed after 5s if not responding to SIGTERM
// - Process already dead: Returns error when trying to signal dead process
// - No process: Returns nil when cmd or process is nil
// - No goroutine leaks: Verify cleanup goroutine completes

// Test: Close returns nil when cmd is nil
func TestLocalProvider_Close_NilCmd(t *testing.T) {
	t.Parallel()

	p := &localProvider{
		cmd: nil,
	}

	err := p.Close()
	assert.NoError(t, err)
}

// Test: Close returns nil when process is nil
func TestLocalProvider_Close_NilProcess(t *testing.T) {
	t.Parallel()

	p := &localProvider{
		cmd: &exec.Cmd{},
	}

	err := p.Close()
	assert.NoError(t, err)
}

// Test: Close gracefully shuts down process that responds to SIGTERM
func TestLocalProvider_Close_GracefulShutdown(t *testing.T) {
	t.Parallel()

	// Build the test helper
	helperPath := buildTestHelper(t, "graceful_exit")

	// Start the process with piped stdout to wait for readiness
	cmd := exec.Command(helperPath)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err, "Failed to create stdout pipe")
	require.NoError(t, cmd.Start(), "Failed to start test process")

	// Wait for "READY" signal
	scanner := bufio.NewScanner(stdout)
	ready := make(chan bool, 1)
	go func() {
		if scanner.Scan() && strings.Contains(scanner.Text(), "READY") {
			ready <- true
		}
	}()

	select {
	case <-ready:
		// Process is ready
	case <-time.After(2 * time.Second):
		t.Fatal("Process never became ready")
	}

	p := &localProvider{
		cmd: cmd,
	}

	// Close should complete within reasonable time (well before 5s timeout)
	start := time.Now()
	err = p.Close()
	elapsed := time.Since(start)

	// Should complete quickly (not hit the 5s timeout)
	// nil error is expected when process exits cleanly (exit 0)
	assert.NoError(t, err)
	assert.Less(t, elapsed, 2*time.Second, "Graceful shutdown took too long")

	// Verify process is actually dead
	assert.Error(t, cmd.Process.Signal(syscall.Signal(0)), "Process should be dead")
}

// Test: Close force kills process that doesn't respond to SIGTERM
func TestLocalProvider_Close_ForceKillOnTimeout(t *testing.T) {
	// Don't run parallel - this test takes 5+ seconds due to timeout

	// Build the test helper that ignores SIGTERM
	helperPath := buildTestHelper(t, "ignore_sigterm")

	// Start the process with piped stdout to wait for readiness
	cmd := exec.Command(helperPath)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err, "Failed to create stdout pipe")
	require.NoError(t, cmd.Start(), "Failed to start test process")

	// Wait for "READY" signal
	scanner := bufio.NewScanner(stdout)
	ready := make(chan bool, 1)
	go func() {
		if scanner.Scan() && strings.Contains(scanner.Text(), "READY") {
			ready <- true
		}
	}()

	select {
	case <-ready:
		// Process is ready
	case <-time.After(2 * time.Second):
		t.Fatal("Process never became ready")
	}

	p := &localProvider{
		cmd: cmd,
	}

	// Close should timeout and force kill
	start := time.Now()
	err = p.Close()
	elapsed := time.Since(start)

	// Kill returns nil on success
	assert.NoError(t, err, "Kill should succeed")
	assert.GreaterOrEqual(t, elapsed, 5*time.Second, "Should wait for timeout")
	assert.Less(t, elapsed, 6*time.Second, "Should not wait much longer than timeout")

	// Verify process is actually dead (give it time to clean up after SIGKILL)
	time.Sleep(100 * time.Millisecond)
	assert.Error(t, cmd.Process.Signal(syscall.Signal(0)), "Process should be dead")
}

// Test: Close returns error when process is already dead
func TestLocalProvider_Close_ProcessAlreadyDead(t *testing.T) {
	t.Parallel()

	// Create a simple process that exits immediately
	cmd := exec.Command("sh", "-c", "exit 0")
	require.NoError(t, cmd.Start(), "Failed to start test process")

	// Wait for it to exit
	require.NoError(t, cmd.Wait(), "Process should exit cleanly")

	p := &localProvider{
		cmd: cmd,
	}

	// Close should return error since process is already dead
	err := p.Close()
	assert.Error(t, err, "Should error when signaling dead process")
}

// Test: Close doesn't leak goroutines on graceful shutdown
func TestLocalProvider_Close_NoGoroutineLeakGraceful(t *testing.T) {
	t.Parallel()

	helperPath := buildTestHelper(t, "graceful_exit")

	cmd := exec.Command(helperPath)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start(), "Failed to start test process")

	// Wait for readiness
	scanner := bufio.NewScanner(stdout)
	ready := make(chan bool, 1)
	go func() {
		if scanner.Scan() && strings.Contains(scanner.Text(), "READY") {
			ready <- true
		}
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("Process never became ready")
	}

	p := &localProvider{
		cmd: cmd,
	}

	// Record initial goroutine count
	before := countGoroutines()

	_ = p.Close()

	// Give goroutines time to clean up
	time.Sleep(100 * time.Millisecond)

	after := countGoroutines()

	// Goroutine count should return to baseline (within small tolerance)
	assert.InDelta(t, before, after, 2, "Goroutine leak detected")
}

// Test: Close doesn't leak goroutines on force kill
func TestLocalProvider_Close_NoGoroutineLeakForceKill(t *testing.T) {
	// Don't run parallel - this test takes 5+ seconds due to timeout

	helperPath := buildTestHelper(t, "ignore_sigterm")

	cmd := exec.Command(helperPath)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start(), "Failed to start test process")

	// Wait for readiness
	scanner := bufio.NewScanner(stdout)
	ready := make(chan bool, 1)
	go func() {
		if scanner.Scan() && strings.Contains(scanner.Text(), "READY") {
			ready <- true
		}
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("Process never became ready")
	}

	p := &localProvider{
		cmd: cmd,
	}

	// Record initial goroutine count
	before := countGoroutines()

	err = p.Close()
	require.NoError(t, err)

	// Give goroutines time to clean up
	time.Sleep(100 * time.Millisecond)

	after := countGoroutines()

	// Goroutine count should return to baseline (within small tolerance)
	assert.InDelta(t, before, after, 2, "Goroutine leak detected")
}

// buildTestHelper builds a test helper Go program and returns its path.
func buildTestHelper(t *testing.T, name string) string {
	t.Helper()

	srcPath := "testdata/" + name + ".go"
	tmpDir := t.TempDir()
	binPath := tmpDir + "/" + name

	// Build the helper binary
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to build test helper %s: %s", name, string(output))

	return binPath
}

// countGoroutines returns the current number of running goroutines.
func countGoroutines() int {
	// Give runtime a moment to stabilize
	time.Sleep(10 * time.Millisecond)
	return runtime.NumGoroutine()
}
