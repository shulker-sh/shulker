package cli

import (
	"context"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/resolve"
)

// awaitingDownloads runs work and, when it fails missing-files at a terminal, lists the files to
// download by hand and the folder they go in, waits for enter, and runs it again, until it
// passes or fails otherwise. Off a terminal, or with --no-input or --json, the failure stands, so
// nothing scripted or launched hangs on a prompt.
func (a *app) awaitingDownloads(ctx context.Context, dir string, work func() error) error {
	for {
		err := work()
		if out.CodeOf(err) != "missing-files" || !a.canWait() {
			return err
		}
		if err := a.awaitDownloads(ctx, dir, out.AsError(err)); err != nil {
			return err
		}
	}
}

func (a *app) canWait() bool {
	return a.tty != nil && a.tty() && a.stdin != nil && !a.printer.JSON && !a.printer.NoInput
}

// awaitDownloads creates the project's downloads folder, says what belongs in it, and blocks until
// a line arrives on stdin. Stdin closing leaves nobody to wait for, so the error stands then too.
func (a *app) awaitDownloads(ctx context.Context, dir string, e *out.Error) error {
	downloads := filepath.Join(dir, resolve.DownloadsDir)
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		return err
	}
	a.printer.Settle()
	l := a.printer.Err()
	l.Warn(e.Message)
	rows := make([]out.Row, 0, len(e.Items)+1)
	for _, item := range e.Items {
		rows = append(rows, out.Row{Text: item})
	}
	l.Tree(append(rows, out.Row{Label: "put them in", Text: downloads})...)
	l.Muted("Press enter once they are there, or ctrl-c to stop")
	answered := make(chan bool, 1)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := a.stdin.Read(buf)
			if n == 1 && buf[0] == '\n' {
				answered <- true
				return
			}
			if err != nil {
				answered <- false
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		return escaped(out.ErrPickCancelled)
	case ok := <-answered:
		if !ok {
			return e
		}
		return nil
	}
}
