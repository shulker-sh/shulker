//go:build !windows

package game

import (
	"errors"
	"os"
	"syscall"
)

// Alive reports whether a process is still running. It is how a run whose watcher was killed is told
// from one that ended while nothing was watching: signal 0 is delivered to nothing, so it only asks
// the question. A process this one may not signal is still a process that is there.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, os.ErrPermission)
}
