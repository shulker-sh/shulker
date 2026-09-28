package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/manual"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/resolve"
)

// downloadCheckEvery is how often the download checklist looks for its files unasked.
const downloadCheckEvery = 5 * time.Second

// awaitingDownloads runs work and, when it fails missing-files at a terminal, waits for the files
// to be downloaded by hand, then runs work again: skipping them when the wait was skipped. Off a
// terminal, or with --no-input or --json, the failure stands, so nothing scripted or launched
// hangs on a prompt.
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

// awaitDownloads creates the downloads folder and draws a checklist of the files e asks for,
// which ends once they are all there, or skipped. An error with files it can't name, such as a
// locked local file that is gone, stands.
func (a *app) awaitDownloads(ctx context.Context, downloads string, e *out.Error) (skip bool, err error) {
	files := manual.Of(e)
	if len(files) == 0 {
		return false, e
	}
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		return false, err
	}
	a.printer.Settle()
	w := manual.NewWait(downloads, a.watchedFolders(), files)
	rows := make([]out.WaitFile, len(files))
	for i, f := range files {
		rows[i] = out.WaitFile{Name: f.Name, Page: f.Page}
	}
	skip, err = a.printer.AwaitDownloads(ctx, out.DownloadWait{
		Title: fmt.Sprintf("%s into %s", e.Message, downloads),
		Files: rows,
		Check: func() ([]out.WaitFile, error) {
			status, err := w.Check()
			if err != nil {
				return nil, err
			}
			checked := make([]out.WaitFile, len(status))
			for i, s := range status {
				checked[i] = out.WaitFile{Found: s.Found, Note: s.Note}
			}
			return checked, nil
		},
		Every: downloadCheckEvery,
	}, a.stdin)
	return skip, escaped(err)
}

// watchedFolders are the folders downloads.watch names, besides the project's downloads/. A
// config.json that can't be read watches none of them, with a warning.
func (a *app) watchedFolders() []string {
	home := a.home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	path, err := a.configFile()
	if err == nil {
		var cfg config.Config
		if cfg, err = config.LoadFile(path); err == nil {
			return cfg.Downloads.Watched(home)
		}
	}
	a.printer.Warn("couldn't read downloads.watch, looking only in downloads/: %v", err)
	return nil
}
