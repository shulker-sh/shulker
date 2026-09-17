//go:build !windows

package pack

import (
	"os/exec"
	"syscall"
)

// killGroup ends git together with the remote helpers it spawns. Killing git alone orphans a
// git-remote-http still holding git's output open, and Wait blocks until that helper exits.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
