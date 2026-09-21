package game

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach gives the game a console of its own with no window on it, and a process group of its own so
// it neither flashes a terminal up nor takes the ctrl-c meant for the shell shulker was run from.
// The watcher has no console to pass on, so without this the game would open one of its own.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP}
}

// hide starts the watcher with no console at all. DETACHED_PROCESS neither inherits this process's
// console nor allocates one, so no window appears whatever subsystem the executable was built for,
// which is what a -H=windowsgui build would otherwise be needed for.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}
