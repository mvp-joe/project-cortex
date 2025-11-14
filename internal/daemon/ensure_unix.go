//go:build unix

package daemon

import "syscall"

// getSysProcAttr returns platform-specific process attributes for daemon spawning.
// On Unix systems, we use Setpgid to detach from the parent process group.
func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true, // Detach from parent process group
	}
}
