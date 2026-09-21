package saves

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func locked(f *os.File) (bool, error) {
	h := windows.Handle(f.Fd())
	err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, windows.UnlockFileEx(h, 0, 1, 0, &windows.Overlapped{})
}
