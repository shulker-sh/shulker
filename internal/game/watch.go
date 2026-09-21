package game

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
)

// Watch starts the watcher: shulker re-execed hidden, in a session of its own, with request written
// to its stdin. The request travels over stdin rather than the argv because it carries the game's
// own arguments, and those hold the session access token, which the process table would make public.
// What comes back is the one line the watcher writes once it has tried to start the game; the
// watcher writes nothing after it, so nothing is left to read when this process has gone.
func Watch(exe string, args []string, request []byte) ([]byte, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdin = bytes.NewReader(request)
	cmd.Stdout = w
	hide(cmd)
	err = cmd.Start()
	w.Close()
	if err != nil {
		return nil, err
	}
	// The watcher is on its own from here: it is in its own session and reaping it would mean
	// waiting for the game, which is the whole reason it exists.
	defer cmd.Process.Release()
	line, err := bufio.NewReader(r).ReadBytes('\n')
	if len(line) == 0 && err != nil {
		return nil, errors.New("the watcher stopped before it said whether the game started")
	}
	return line, nil
}
