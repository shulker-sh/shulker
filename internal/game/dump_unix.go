//go:build !windows

package game

import (
	"os"
	"syscall"
	"time"

	"shulker.sh/shulker/internal/instance"
)

// dump sends the game SIGQUIT, on which HotSpot prints every thread's stack to its own stdout, which
// is the run's log, and keeps running.
func dump(run instance.Launch, timeout time.Duration) (string, error) {
	info, err := os.Stat(run.Log)
	if err != nil {
		return "", err
	}
	p, err := os.FindProcess(run.PID)
	if err != nil {
		return "", err
	}
	if err := p.Signal(syscall.SIGQUIT); err != nil {
		return "", err
	}
	return awaitDump(run.Log, info.Size(), timeout)
}
