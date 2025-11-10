package daemon

import (
	"fmt"
	"net"

	"github.com/gofrs/flock"
)

// SingletonDaemon manages daemon singleton enforcement.
// It ensures only one instance of a daemon runs at a time using both
// socket binding and file locking.
type SingletonDaemon struct {
	name       string
	socketPath string
	lock       *flock.Flock
}

// NewSingletonDaemon creates a new singleton daemon manager.
// name is used to identify the daemon (e.g., "indexer", "embed").
// socketPath is the Unix domain socket path for the daemon.
func NewSingletonDaemon(name, socketPath string) *SingletonDaemon {
	return &SingletonDaemon{
		name:       name,
		socketPath: socketPath,
	}
}

// EnforceSingleton attempts to become the singleton instance.
// Returns (true, nil) if this process won and should continue serving.
// Returns (false, nil) if another instance is running (this process should exit 0).
// Returns (false, err) on actual errors.
//
// The check uses both socket binding (to detect running daemons) and file locking
// (to prevent race conditions during startup).
func (s *SingletonDaemon) EnforceSingleton() (bool, error) {
	// Try to bind socket
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		// Socket bind failed - another daemon has it
		if isAddrInUse(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to bind socket: %w", err)
	}
	listener.Close() // Close test listener

	// Acquire file lock
	lockPath := getLockPath(s.name)
	s.lock = flock.New(lockPath)

	locked, err := s.lock.TryLock()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !locked {
		// Another process has the lock
		return false, nil
	}

	// This process won
	return true, nil
}

// BindSocket creates the Unix socket listener.
// Caller must have already won via EnforceSingleton().
func (s *SingletonDaemon) BindSocket() (net.Listener, error) {
	return net.Listen("unix", s.socketPath)
}

// Release releases the file lock (called on shutdown).
func (s *SingletonDaemon) Release() error {
	if s.lock != nil {
		return s.lock.Unlock()
	}
	return nil
}
