package sandbox

import (
	"os"
	"syscall"
)

const sandboxExec = "/usr/bin/sandbox-exec"

// Supported reports whether this machine can sandbox a game.
func Supported() bool {
	_, err := os.Stat(sandboxExec)
	return err == nil
}

// Exec replaces this process with Java under the policy, and only returns when that fails.
// sandbox-exec applies the profile and then execs the command, so the sandboxed process is Java
// itself and everything it starts inherits the sandbox.
func Exec(p Policy, java string, argv []string) error {
	args := append([]string{sandboxExec, "-p", Profile(p), java}, argv...)
	return syscall.Exec(sandboxExec, args, os.Environ())
}
