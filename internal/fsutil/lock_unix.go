//go:build unix

package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Lock takes an exclusive lock on path, creating it if needed, and waits for any other holder, in
// this process or another, to let go. The operating system drops the lock with a process that dies
// holding it, so a killed holder never leaves it stuck.
func Lock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
