package game

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach gives the game no console and a process group of its own, so it neither flashes a window
// up nor takes the ctrl-c meant for the shell shulker was run from.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}
