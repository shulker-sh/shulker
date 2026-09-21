//go:build unix

package saves

import (
	"os"

	"golang.org/x/sys/unix"
)

// locked asks for the record lock a game takes without taking it; F_GETLK needs no write access.
func locked(f *os.File) (bool, error) {
	lk := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(f.Fd(), unix.F_GETLK, &lk); err != nil {
		return false, err
	}
	return lk.Type != unix.F_UNLCK, nil
}
