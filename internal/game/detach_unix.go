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

// hide starts the watcher the same way. It has no console to hide from on this OS; what matters is
// that it outlives the command that spawned it, which a session of its own is what gives it.
func hide(cmd *exec.Cmd) { detach(cmd) }
