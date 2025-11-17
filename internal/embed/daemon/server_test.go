package daemon

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	embedv1 "github.com/mvp-joe/project-cortex/gen/embed/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	libDir := "/tmp/test-lib"
	modelDir := "/tmp/test-models"
	dimensions := 384
	idleTimeout := 10 * time.Minute

	server, err := NewServer(ctx, libDir, modelDir, dimensions, idleTimeout)
	require.NoError(t, err)
	require.NotNil(t, server)

	// Note: libDir no longer stored in Server (Rust FFI handles library loading)
	assert.Equal(t, modelDir, server.modelDir)
	assert.Equal(t, dimensions, server.dimensions)
	assert.Equal(t, idleTimeout, server.idleTimeout)
	assert.Nil(t, server.model, "model should be nil before Initialize")
	assert.WithinDuration(t, time.Now(), server.startTime, time.Second)
	assert.WithinDuration(t, time.Now(), server.lastRequest, time.Second)

	err = server.Close()
	assert.NoError(t, err)
}

func TestHealth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", 384, 10*time.Minute)
	require.NoError(t, err)
	defer server.Close()

	// Call Health
	req := connect.NewRequest(&embedv1.HealthRequest{})
	resp, err := server.Health(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Msg.Healthy)
	assert.GreaterOrEqual(t, resp.Msg.UptimeSeconds, int64(0))
	assert.LessOrEqual(t, resp.Msg.UptimeSeconds, int64(2))
	assert.GreaterOrEqual(t, resp.Msg.LastRequestMsAgo, int64(0))
	assert.LessOrEqual(t, resp.Msg.LastRequestMsAgo, int64(1000))
}

func TestHealth_MultipleRequests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", 384, 10*time.Minute)
	require.NoError(t, err)
	defer server.Close()

	// First health check
	req := connect.NewRequest(&embedv1.HealthRequest{})
	resp1, err := server.Health(ctx, req)
	require.NoError(t, err)

	// Wait long enough for uptime to increase by at least 1 second
	time.Sleep(1100 * time.Millisecond)

	// Second health check
	resp2, err := server.Health(ctx, req)
	require.NoError(t, err)

	// Uptime should have increased
	assert.Greater(t, resp2.Msg.UptimeSeconds, resp1.Msg.UptimeSeconds)
}

func TestEmbed_NotInitialized(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", 384, 10*time.Minute)
	require.NoError(t, err)
	defer server.Close()

	// Try to embed without initializing
	req := connect.NewRequest(&embedv1.EmbedRequest{
		Texts: []string{"test text"},
		Mode:  "passage",
	})

	resp, err := server.Embed(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	// Check error is FailedPrecondition
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "not initialized")
}

func TestEmbed_UpdatesLastRequestTime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", 384, 10*time.Minute)
	require.NoError(t, err)
	defer server.Close()

	// Record initial last request time
	server.lastRequestMu.RLock()
	initialTime := server.lastRequest
	server.lastRequestMu.RUnlock()

	// Wait long enough to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// Try to embed (will fail, but should still update lastRequest)
	req := connect.NewRequest(&embedv1.EmbedRequest{
		Texts: []string{"test"},
	})
	_, _ = server.Embed(ctx, req)

	// Check last request time was updated
	server.lastRequestMu.RLock()
	updatedTime := server.lastRequest
	server.lastRequestMu.RUnlock()

	assert.True(t, updatedTime.After(initialTime),
		"updatedTime=%v should be after initialTime=%v", updatedTime, initialTime)
}

func TestClose_BeforeInitialize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", 384, 10*time.Minute)
	require.NoError(t, err)

	// Close before Initialize should work
	err = server.Close()
	assert.NoError(t, err)
}

func TestMonitorIdle_Cancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", 384, 10*time.Minute)
	require.NoError(t, err)
	defer server.Close()

	// Cancel context to stop idle monitor
	cancel()

	// Give monitor goroutine time to exit
	time.Sleep(100 * time.Millisecond)

	// No assertions needed - just ensuring no panic/deadlock
}

func TestServer_DimensionConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dimensions int
	}{
		{"384 dimensions", 384},
		{"512 dimensions", 512},
		{"768 dimensions", 768},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", tt.dimensions, 10*time.Minute)
			require.NoError(t, err)
			defer server.Close()

			assert.Equal(t, tt.dimensions, server.dimensions)
		})
	}
}

func TestServer_IdleTimeoutConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		idleTimeout time.Duration
	}{
		{"5 minutes", 5 * time.Minute},
		{"10 minutes", 10 * time.Minute},
		{"30 minutes", 30 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", 384, tt.idleTimeout)
			require.NoError(t, err)
			defer server.Close()

			assert.Equal(t, tt.idleTimeout, server.idleTimeout)
		})
	}
}

func TestHealth_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server, err := NewServer(ctx, "/tmp/test-lib", "/tmp/test-models", 384, 10*time.Minute)
	require.NoError(t, err)
	defer server.Close()

	// Launch multiple concurrent health checks
	const numGoroutines = 10
	done := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			req := connect.NewRequest(&embedv1.HealthRequest{})
			resp, err := server.Health(ctx, req)
			if err != nil {
				done <- err
				return
			}
			if !resp.Msg.Healthy {
				done <- assert.AnError
				return
			}
			done <- nil
		}()
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}
