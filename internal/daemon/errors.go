package daemon

import "strings"

// IsConnectionError checks if an error indicates the daemon is not reachable.
//
// This function is used by providers implementing the resurrection pattern to
// detect when a daemon has shut down (e.g., due to idle timeout) and needs to
// be restarted.
//
// Returns true for errors containing:
//   - "connection refused" - TCP/Unix socket connection failed
//   - "no such file or directory" - Unix socket file doesn't exist
//   - "broken pipe" - Connection closed mid-communication
//
// Returns false for nil or other unrelated errors.
//
// Example:
//
//	err := client.Embed(ctx, texts)
//	if daemon.IsConnectionError(err) {
//	    // Daemon died, resurrect and retry
//	    if err := daemon.EnsureDaemon(ctx, config); err != nil {
//	        return err
//	    }
//	    err = client.Embed(ctx, texts)
//	}
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such file or directory") || // Socket doesn't exist
		strings.Contains(errStr, "broken pipe")
}
