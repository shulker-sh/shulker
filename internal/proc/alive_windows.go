package proc

import "golang.org/x/sys/windows"

// IsAlive reports whether a process is still running. It is how a run whose watcher was killed is told
// from one that ended while nothing was watching: a handle that can't be opened, or one that is
// already signalled, is a process that has gone.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	return err == nil && state == uint32(windows.WAIT_TIMEOUT)
}
