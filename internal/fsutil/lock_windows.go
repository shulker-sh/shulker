package fsutil

import (
	"os"

	"golang.org/x/sys/windows"
)

// Lock takes an exclusive lock on path, creating it if needed, and waits for any other holder, in
// this process or another, to let go. The operating system drops the lock with a process that dies
// holding it, so a killed holder never leaves it stuck.
func Lock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		windows.UnlockFileEx(h, 0, 1, 0, &windows.Overlapped{})
		f.Close()
	}, nil
}
