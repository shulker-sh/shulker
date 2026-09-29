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

// awaitingDownloads runs work and, when it fails for files to download by hand at a terminal,
// waits for them and runs work again. A skipped wait runs work once more with skip set when work
// is skippable, and leaves the failure standing when it isn't. Off a terminal, or with --no-input
// or --json, the failure stands, so nothing scripted or launched hangs on a prompt. dir is the
// project's, read once work has opened it.
func (a *app) awaitingDownloads(ctx context.Context, dir func() string, skippable bool, work func(skip bool) error) error {
	skip := false
	for {
		err := work(skip)
		if len(manual.Of(err)) == 0 || !a.canWait() || skip {
			return err
		}
		skipped, waitErr := a.awaitDownloads(ctx, filepath.Join(dir(), resolve.DownloadsDir), out.AsError(err))
		if waitErr != nil {
			return waitErr
		}
		if skipped && !skippable {
			return err
		}
		skip = skipped
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
	if d, err := a.deps(); err == nil {
		w.Cache = d.Cache
	}
	rows := make([]out.WaitFile, len(files))
	for i, f := range files {
		rows[i] = out.WaitFile{Name: f.Name, Page: f.Page}
	}
	skip, err = a.printer.AwaitDownloads(ctx, out.DownloadWait{
		Title: fmt.Sprintf("%s a manual download into %s", out.Count(len(files), "file needs", "files need"), downloads),
		Files: rows,
		Check: func() ([]out.WaitFile, error) {
			status, err := w.Check()
			return waitFiles(downloads, status), err
		},
		Every: downloadCheckEvery,
		Paste: func(text string) ([]out.WaitFile, string, error) {
			status, note, err := w.Take(text)
			return waitFiles(downloads, status), note, err
		},
	}, a.stdin)
	return skip, escaped(err)
}

func waitFiles(downloads string, status []manual.Status) []out.WaitFile {
	files := make([]out.WaitFile, len(status))
	for i, s := range status {
		files[i] = out.WaitFile{Found: s.Found, From: foundFrom(downloads, s), Note: s.Note}
	}
	return files
}

// foundFrom is where a found file came from, as its row says it.
func foundFrom(downloads string, s manual.Status) string {
	switch {
	case !s.Found:
		return ""
	case s.Cached:
		return "from the cache"
	case s.From == "":
		return "dropped here"
	case s.From == downloads:
		return "from " + resolve.DownloadsDir + "/"
	}
	return "from " + out.Tilde(s.From)
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
	a.printer.Warn("couldn't read downloads.watch, looking only in downloads/: %v.", err)
	return nil
}
