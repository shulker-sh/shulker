package game

import (
	"io"
	"os/exec"
	"slices"

	"shulker.sh/shulker/internal/proc"
)

// Run runs a launch in the foreground on the given streams and hands back the game's exit status.
// A wrapper that can't be run at all is not worth losing the launch over, so the game then starts
// with Java on its own and gaveWay says why the wrapper didn't; err is Java itself never
// starting, which is the end of the launch.
func Run(l Launch, stdin io.Reader, stdout, stderr io.Writer) (code int, gaveWay, err error) {
	if w := l.Wrapper; len(w) > 0 {
		args := slices.Concat(w[1:], []string{l.Java}, l.Argv)
		code, err := runExe(w[0], args, stdin, stdout, stderr)
		if err == nil {
			return code, nil, nil
		}
		gaveWay = err
	}
	code, err = runExe(l.Java, l.Argv, stdin, stdout, stderr)
	return code, gaveWay, err
}

// runExe runs one program to completion. A non-zero status comes back as the code, so only a
// program that never started at all is an error.
func runExe(exe string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return proc.ExitCode(cmd.Run())
}
