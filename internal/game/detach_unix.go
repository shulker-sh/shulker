//go:build !windows

package game

import (
	"os/exec"
	"syscall"
)

// detach puts the game in a session of its own, so the terminal shulker was run from is no longer
// its controlling terminal: closing that shell, or interrupting it, leaves the game running.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
