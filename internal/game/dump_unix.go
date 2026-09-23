//go:build !windows

package game

import (
	"os"
	"syscall"
	"time"
)

// dump sends the game SIGQUIT, on which HotSpot prints every thread's stack to its own stdout, which
// is the run's log, and keeps running.
func dump(pid int, _, log string, timeout time.Duration) (string, error) {
	info, err := os.Stat(log)
	if err != nil {
		return "", err
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return "", err
	}
	if err := p.Signal(syscall.SIGQUIT); err != nil {
		return "", err
	}
	return awaitDump(log, info.Size(), timeout)
}
