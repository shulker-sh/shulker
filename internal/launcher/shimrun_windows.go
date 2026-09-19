package launcher

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// commandLineTail is this process's own command line with its program name cut off. It comes from the
// OS rather than from os.Args because the game's arguments go to the child exactly as the launcher
// wrote them: rebuilding them from a parsed argv would corrupt the quoting.
func commandLineTail() string {
	return cutProgramName(windows.UTF16PtrToString(windows.GetCommandLine()))
}

// shimSpawn starts the child with a command line built by hand, since os/exec would re-quote an
// argument list, and reports its exit code. An error is a program that never started at all, which
// the caller says out loud rather than exiting on quietly.
func shimSpawn(dir, program, arguments string) (int, error) {
	cmd := exec.Command(program)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"` + program + `" ` + arguments, CreationFlags: windows.CREATE_NO_WINDOW}
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	}
	return 0, err
}
