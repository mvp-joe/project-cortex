package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/mvp-joe/project-cortex/internal/cache"
)

// Test Plan for Server:
// - Constructor creates server with registry and initializes fields
// - Index() validates project path (absolute, exists)
// - Index() creates actor for new project and registers it
// - Index() reuses existing actor for already-registered project
// - Index() streams progress updates to client
// - Index() handles concurrent indexing for different projects
// - GetStatus() returns daemon info (PID, uptime, socket path)
// - GetStatus() returns all project statuses
// - GetStatus() works with empty daemon (no projects)
// - UnregisterProject() stops actor and removes from registry
// - UnregisterProject() removes cache when requested
// - UnregisterProject() is idempotent (no error if not registered)
// - Shutdown() stops all actors and clears map
// - Shutdown() cancels context
// - StreamLogs() returns unimplemented error (MVP)
// - Thread safety for concurrent operations

// Test: Constructor creates server with registry and initializes fields
func TestNewServer_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, socketPath, mockProvider, testCache)
	require.NoError(t, err)
	require.NotNil(t, server)
	assert.NotNil(t, server.registry)
	assert.NotNil(t, server.actors)
	assert.Equal(t, socketPath, server.socketPath)
	assert.NotZero(t, server.startedAt)
}

// Test: getOrCreateActor validates project path must be absolute
func TestGetOrCreateActor_RejectsRelativePath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// This will fail when NewActor validates the path
	_, _, err = server.getOrCreateActor(ctx, "relative/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

// Test: GetStatus() returns daemon info with no projects
func TestGetStatus_EmptyDaemon(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, socketPath, mockProvider, testCache)
	require.NoError(t, err)

	req := connect.NewRequest(&indexerv1.StatusRequest{})
	resp, err := server.GetStatus(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Msg.Daemon)

	assert.Equal(t, int32(os.Getpid()), resp.Msg.Daemon.Pid)
	assert.Equal(t, socketPath, resp.Msg.Daemon.SocketPath)
	assert.GreaterOrEqual(t, resp.Msg.Daemon.UptimeSeconds, int64(0))
	assert.Empty(t, resp.Msg.Projects)
}

// Test: UnregisterProject() is idempotent for non-registered project
func TestUnregisterProject_NotRegistered(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	req := connect.NewRequest(&indexerv1.UnregisterRequest{
		ProjectPath: "/nonexistent/project",
		RemoveCache: false,
	})

	resp, err := server.UnregisterProject(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Msg.Success)
	assert.Contains(t, resp.Msg.Message, "not registered")
}

// Test: Shutdown() clears actors map and cancels context
func TestShutdown_ClearsState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// We can't easily mock an Actor since it's a concrete type,
	// so we just verify the shutdown logic without actors

	req := connect.NewRequest(&indexerv1.ShutdownRequest{})
	resp, err := server.Shutdown(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Msg.Success)

	// Verify actors map cleared
	server.actorsMu.RLock()
	assert.Empty(t, server.actors)
	server.actorsMu.RUnlock()

	// Verify context cancelled
	select {
	case <-server.ctx.Done():
		// Good, context was cancelled
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled")
	}
}

// Test: stopActor is idempotent
func TestStopActor_Idempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// Stop non-existent actor should not error
	err = server.stopActor("/nonexistent/project")
	assert.NoError(t, err)
}

// Test: logMessage adds logs to buffer
func TestLogMessage_AddsToBuffer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// Add some logs
	server.logMessage("/project/one", "INFO", "Test message 1")
	server.logMessage("/project/two", "WARN", "Test message 2")
	server.logMessage("/project/one", "ERROR", "Test message 3")

	// Collect buffered logs
	server.logsMu.RLock()
	logs := server.collectBufferedLogs("")
	server.logsMu.RUnlock()

	// Should have all 3 logs
	assert.Len(t, logs, 3)
	assert.Equal(t, "Test message 1", logs[0].Message)
	assert.Equal(t, "Test message 2", logs[1].Message)
	assert.Equal(t, "Test message 3", logs[2].Message)
}

// Test: collectBufferedLogs filters by project
func TestCollectBufferedLogs_ProjectFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// Add logs for different projects
	server.logMessage("/project/one", "INFO", "Message for project one")
	server.logMessage("/project/two", "INFO", "Message for project two")
	server.logMessage("/project/one", "INFO", "Another message for project one")

	// Collect logs for project one only
	server.logsMu.RLock()
	logs := server.collectBufferedLogs("/project/one")
	server.logsMu.RUnlock()

	// Should only have logs for /project/one
	assert.Len(t, logs, 2)
	for _, entry := range logs {
		assert.Equal(t, "/project/one", entry.Project)
	}
}

// Test: logMessage broadcasts to subscribers
func TestLogMessage_BroadcastsToSubscribers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// Create subscriptions
	ch1 := make(chan *indexerv1.LogEntry, 10)
	ch2 := make(chan *indexerv1.LogEntry, 10)

	server.logsMu.Lock()
	server.logSubs["sub1"] = ch1
	server.logSubs["sub2"] = ch2
	server.logsMu.Unlock()

	// Send a log message
	server.logMessage("/project/test", "INFO", "Broadcast message")

	// Both subscribers should receive it
	select {
	case entry := <-ch1:
		assert.Equal(t, "Broadcast message", entry.Message)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ch1")
	}

	select {
	case entry := <-ch2:
		assert.Equal(t, "Broadcast message", entry.Message)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ch2")
	}
}

// Test: Shutdown closes log subscriptions
func TestShutdown_ClosesLogSubscriptions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// Create subscriptions manually
	ch := make(chan *indexerv1.LogEntry, 10)
	server.logsMu.Lock()
	server.logSubs["test-sub"] = ch
	server.logsMu.Unlock()

	// Verify subscription exists
	server.logsMu.RLock()
	subCount := len(server.logSubs)
	server.logsMu.RUnlock()
	assert.Equal(t, 1, subCount)

	// Shutdown server
	shutdownReq := connect.NewRequest(&indexerv1.ShutdownRequest{})
	resp, err := server.Shutdown(ctx, shutdownReq)
	require.NoError(t, err)
	assert.True(t, resp.Msg.Success)

	// Verify subscriptions cleared
	server.logsMu.RLock()
	subCount = len(server.logSubs)
	server.logsMu.RUnlock()
	assert.Equal(t, 0, subCount)

	// Verify channel closed
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for channel close")
	}
}

// Test: logMessage is thread-safe
func TestLogMessage_ThreadSafe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// Concurrently add logs
	const numGoroutines = 10
	const messagesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				server.logMessage(
					fmt.Sprintf("/project/%d", id),
					"INFO",
					fmt.Sprintf("Message %d-%d", id, j),
				)
			}
		}(i)
	}

	wg.Wait()

	// Verify logs were added (should be last 1000 due to ring buffer)
	server.logsMu.RLock()
	logs := server.collectBufferedLogs("")
	server.logsMu.RUnlock()

	// Ring buffer capacity is 1000, so we should have 1000 logs
	assert.Equal(t, 1000, len(logs))
}

// Test: ring buffer respects capacity limit
func TestLogBuffer_CapacityLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())
	server, err := NewServer(ctx, filepath.Join(t.TempDir(), "test.sock"), mockProvider, testCache)
	require.NoError(t, err)

	// Add more than 1000 logs
	for i := 0; i < 1500; i++ {
		server.logMessage("/project/test", "INFO", fmt.Sprintf("Message %d", i))
	}

	// Should only have 1000 logs (oldest ones dropped)
	server.logsMu.RLock()
	logs := server.collectBufferedLogs("")
	server.logsMu.RUnlock()

	assert.Equal(t, 1000, len(logs))

	// Verify we have the most recent logs (500-1499)
	// First log should be message 500
	assert.Equal(t, "Message 500", logs[0].Message)
	// Last log should be message 1499
	assert.Equal(t, "Message 1499", logs[999].Message)
}
