package game

import (
	"io"
	"os/exec"

	"shulker.sh/shulker/internal/proc"
)

// Run runs a launch in the foreground on the given streams and hands back the game's exit status.
// A wrapper that can't be run at all is not worth losing the launch over, so the game then starts
// without it and gaveWay says why the wrapper didn't; err is the game itself never starting, which
// is the end of the launch. The sandbox is never dropped that way: a launch that asks for one
// starts inside it or not at all.
func Run(l Launch, stdin io.Reader, stdout, stderr io.Writer) (code int, gaveWay, err error) {
	if len(l.Wrapper) > 0 {
		words := l.command(l.Wrapper)
		code, err := runExe(words[0], words[1:], stdin, stdout, stderr)
		if err == nil {
			return code, nil, nil
		}
		gaveWay = err
	}
	words := l.command(nil)
	code, err = runExe(words[0], words[1:], stdin, stdout, stderr)
	return code, gaveWay, err
}

// runExe runs one program to completion. A non-zero status comes back as the code, so only a
// program that never started at all is an error.
func runExe(exe string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return proc.ExitCode(cmd.Run())
}
