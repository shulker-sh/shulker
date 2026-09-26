package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/term"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/resolve"
)

// ctrlC is the byte a terminal in raw mode sends for ctrl-c, in place of the signal.
const ctrlC = 0x03

// awaitingDownloads runs work and, when it fails missing-files at a terminal, lists the files to
// download by hand and the folder they go in, and waits: enter runs work again, until it passes or
// fails otherwise, and ctrl-c runs it once more skipping them. Off a terminal, or with --no-input
// or --json, the failure stands, so nothing scripted or launched hangs on a prompt.
func (a *app) awaitingDownloads(ctx context.Context, dir string, work func(skip bool) error) error {
	skip := false
	for {
		err := work(skip)
		if out.CodeOf(err) != "missing-files" || !a.canWait() || skip {
			return err
		}
		if skip, err = a.awaitDownloads(ctx, filepath.Join(dir, resolve.DownloadsDir), out.AsError(err)); err != nil {
			return err
		}
	}
}

func (a *app) canWait() bool {
	return a.tty != nil && a.tty() && a.stdin != nil && !a.printer.JSON && !a.printer.NoInput
}

// awaitDownloads creates the downloads folder, says what belongs in it, and blocks until enter,
// which reports skip false, or ctrl-c, which reports it true. Stdin closing leaves nobody to wait
// for, so the error stands then.
func (a *app) awaitDownloads(ctx context.Context, downloads string, e *out.Error) (skip bool, err error) {
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		return false, err
	}
	a.printer.Settle()
	l := a.printer.Err()
	if a.waits > 0 {
		l.Blank()
		l.Warn(fmt.Sprintf("%s still missing from %s", out.Count(len(e.Items), "file is", "files are"), downloads))
	} else {
		l.Warn(fmt.Sprintf("%s into %s", e.Message, downloads))
	}
	a.waits++
	rows := make([]out.Row, 0, len(e.Items))
	for _, d := range e.Rows {
		text := d.Text
		for _, c := range d.Children {
			text += "\n" + c.Text
		}
		rows = append(rows, out.Row{Text: text})
	}
	if len(rows) == 0 {
		for _, item := range e.Items {
			rows = append(rows, out.Row{Text: item})
		}
	}
	l.Tree(rows...)
	l.Blank()
	l.Muted("Press Enter once they're there, or Ctrl-C to skip them")
	// A terminal in raw mode hands ctrl-c over as a byte, so it skips the files rather than
	// interrupting the command, and sends enter as a carriage return.
	if f, ok := a.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		if state, err := term.MakeRaw(int(f.Fd())); err == nil {
			defer term.Restore(int(f.Fd()), state)
		}
	}
	answered := make(chan byte, 1)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := a.stdin.Read(buf)
			if n == 1 && (buf[0] == '\n' || buf[0] == '\r' || buf[0] == ctrlC) {
				answered <- buf[0]
				return
			}
			if err != nil {
				answered <- 0
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		return false, escaped(out.ErrPickCancelled)
	case key := <-answered:
		if key == 0 {
			return false, e
		}
		return key == ctrlC, nil
	}
}
