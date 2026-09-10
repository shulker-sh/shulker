package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type Runner struct {
	Java      string
	Dir       string
	Args      []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Interrupt <-chan os.Signal
	Log       io.Writer
}

func (r *Runner) Run() (int, error) {
	cmd := exec.Command(r.Java, r.Args...)
	cmd.Dir = r.Dir
	cmd.Stdout, cmd.Stderr = r.Stdout, r.Stderr
	detach(cmd)
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	console := &lockedWriter{w: pipe}
	if r.Stdin != nil {
		go func() { _, _ = io.Copy(console, r.Stdin) }()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	for {
		select {
		case err := <-done:
			return exitCode(err)
		case <-r.Interrupt:
			if stopped {
				fmt.Fprintln(r.Log, "shulker: killing the server")
				_ = cmd.Process.Kill()
				continue
			}
			stopped = true
			fmt.Fprintln(r.Log, "shulker: sending stop; press Ctrl-C again to kill")
			_, _ = console.Write([]byte("stop\n"))
		}
	}
}

func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
