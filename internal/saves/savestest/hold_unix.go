//go:build unix

package savestest

import (
	"os"

	"golang.org/x/sys/unix"
)

func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	lk := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &lk); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
