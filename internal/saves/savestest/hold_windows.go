package savestest

import (
	"math"
	"os"

	"golang.org/x/sys/windows"
)

func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, math.MaxUint32, math.MaxInt32, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
