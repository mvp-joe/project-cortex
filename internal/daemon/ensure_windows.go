//go:build windows

package daemon

import "syscall"

// getSysProcAttr returns platform-specific process attributes for daemon spawning.
// On Windows, processes are automatically detached when started without inheriting handles.
func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		// Windows-specific: Don't create a new console window
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
