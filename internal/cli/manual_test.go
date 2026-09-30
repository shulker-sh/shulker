package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// keysOnceWaiting is a stdin that, once the wait has created downloads, runs then and types keys.
func keysOnceWaiting(t *testing.T, downloads string, then func(), keys string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	go func() {
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(downloads); err == nil {
				break
			}
		}
		then()
		w.WriteString(keys)
	}()
	return r
}

// lockedManualDownload is a project with nodist locked, its jar gone from downloads/ and the
// cache, so the next install needs it by hand.
func lockedManualDownload(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	downloads := filepath.Join(h.dir, "downloads")
	os.MkdirAll(downloads, 0o755)
	os.WriteFile(filepath.Join(downloads, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	h.mustRun(t, "add", "nodist")
	os.RemoveAll(downloads)
	os.RemoveAll(h.cache)
	return h
}

func TestInstallWaitsForAManualDownloadAtATerminal(t *testing.T) {
	h := lockedManualDownload(t)
	downloads := filepath.Join(h.dir, "downloads")
	h.tty = true
	h.stdin = keysOnceWaiting(t, downloads, func() {
		os.WriteFile(filepath.Join(downloads, "renamed.jar"), h.jars["nodist"].data, 0o644)
	}, "\r")

	code, stdout, stderr := h.run(t, "install")
	if code != 0 {
		t.Fatalf("install after the file arrives: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "1 file needs a manual download into") || !strings.Contains(stderr, downloads) || !strings.Contains(stderr, "nodist-1.0.0.jar") || !strings.Contains(stderr, "https://www.curseforge.com") {
		t.Fatalf("the wait names each file and its page: %s", stderr)
	}
	if !strings.Contains(stdout, "Built client") {
		t.Fatalf("install goes on after the wait: %s", stdout)
	}
}

func TestInstallFailsMissingFilesWhereItCantWait(t *testing.T) {
	h := lockedManualDownload(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w.Close()
	h.stdin = r
	for _, c := range []struct {
		tty  bool
		args []string
	}{
		{false, []string{"install"}},
		{true, []string{"install", "--no-input"}},
		{true, []string{"install", "--json"}},
	} {
		h.tty = c.tty
		code, stdout, stderr := h.run(t, c.args...)
		if code == 0 || !strings.Contains(stdout+stderr, "missing-files") || strings.Contains(stderr, "Press Enter") {
			t.Fatalf("%v tty=%v: code=%d stdout=%s stderr=%s", c.args, c.tty, code, stdout, stderr)
		}
	}
}

func TestImportWaitsForAManualDownloadAtATerminal(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(t.TempDir(), "blocked.zip")
	writeCurseForgeZip(t, archive, importedCurseForgePack(cfPackFile{ProjectID: 300000, FileID: 5100001, Required: true}), map[string][]byte{})
	dir := filepath.Join(t.TempDir(), "craft-pack")
	downloads := filepath.Join(dir, "downloads")
	h.tty = true
	h.stdin = keysOnceWaiting(t, downloads, func() {
		os.WriteFile(filepath.Join(downloads, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	}, "\r")

	code, stdout, stderr := h.run(t, "import", archive, "--dir", dir)
	if code != 0 || !strings.Contains(stderr, downloads) || !strings.Contains(stderr, "✔ nodist-1.0.0.jar (from downloads/)") {
		t.Fatalf("import: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, l := readProject(t, dir); l.Mods["nodist"].Sha512 != h.jars["nodist"].sha512 {
		t.Fatalf("nodist locks from downloads/: %+v", l.Mods)
	}

	h.tty = false
	if _, stderr := h.mustRunStderr(t, "import", archive, "--dir", filepath.Join(t.TempDir(), "cached")); !strings.Contains(stderr, "Took 1 manual download from the cache") || strings.Contains(stderr, "Fetched 1 mod") {
		t.Fatalf("the next import takes the file from the cache: %s", stderr)
	}
	os.RemoveAll(h.cache)
	if code, stdout, _ := h.run(t, "--json", "import", archive, "--dir", filepath.Join(t.TempDir(), "again")); code == 0 || failureCode(t, stdout).Code != "missing-files" {
		t.Fatalf("off a terminal import fails: code=%d %s", code, stdout)
	}
}

func TestImportSkipsAManualDownloadAndInstallAsksForItAgain(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(t.TempDir(), "blocked.zip")
	writeCurseForgeZip(t, archive, importedCurseForgePack(cfPackFile{ProjectID: 300000, FileID: 5100001, Required: true}), map[string][]byte{})
	dir := filepath.Join(t.TempDir(), "craft-pack")
	h.tty = true
	h.stdin = keysOnceWaiting(t, filepath.Join(dir, "downloads"), func() {}, "\x1b")

	code, stdout, stderr := h.run(t, "import", archive, "--dir", dir)
	if code != 0 {
		t.Fatalf("esc skips the file and the import finishes: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "waiting for a manual download") {
		t.Fatalf("the import names what it left pending: %s", stdout)
	}
	m, l := readProject(t, dir)
	pending := l.Mods["nodist"]
	if !pending.IsPending() || pending.Sha1 == "" || m.Requires["nodist"].Project == "" {
		t.Fatalf("nodist stays in the lock as pending: %+v %+v", pending, m.Requires["nodist"])
	}

	h.tty = false
	h.dir = dir
	stdout, stderr = h.mustRunStderr(t, "install")
	if !strings.Contains(stderr, "nodist is left out until its manual download is in downloads/") {
		t.Fatalf("off a terminal install builds without the pending mod: %s", stderr)
	}

	h.tty = true
	h.stdin = keysOnceWaiting(t, filepath.Join(dir, "downloads"), func() {
		os.WriteFile(filepath.Join(dir, "downloads", "any-name.jar"), h.jars["nodist"].data, 0o644)
	}, "")
	if code, stdout, stderr := h.run(t, "install"); code != 0 {
		t.Fatalf("install asks for it again: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, l := readProject(t, dir); l.Mods["nodist"].Sha512 != h.jars["nodist"].sha512 {
		t.Fatalf("the dropped file fills the pending entry: %+v", l.Mods["nodist"])
	}
}

func TestInstallTakesAManualDownloadFromAWatchedFolder(t *testing.T) {
	h := lockedManualDownload(t)
	h.tty = true
	if got := h.mustRun(t, "config", "get", "downloads.watch"); !strings.Contains(got, `"~/Downloads"`) {
		t.Fatalf("unset, downloads.watch is the home folder's Downloads: %s", got)
	}
	browser := filepath.Join(h.home, "Downloads")
	os.MkdirAll(browser, 0o755)
	os.WriteFile(filepath.Join(browser, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	h.stdin = keysOnceWaiting(t, filepath.Join(h.dir, "downloads"), func() {}, "")

	code, stdout, stderr := h.run(t, "install")
	if code != 0 || !strings.Contains(stdout, "Built client") || !strings.Contains(stderr, "nodist-1.0.0.jar (from "+browser+")") {
		t.Fatalf("the wait finds the file in ~/Downloads and goes on by itself: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(browser, "nodist-1.0.0.jar")); err != nil {
		t.Fatalf("a file already in ~/Downloads is copied, not moved: %v", err)
	}

	h = lockedManualDownload(t)
	h.tty = true
	h.mustRun(t, "config", "set", "downloads.watch", "--literal", `["~/elsewhere"]`)
	elsewhere := filepath.Join(h.home, "elsewhere")
	os.MkdirAll(elsewhere, 0o755)
	os.WriteFile(filepath.Join(elsewhere, "nodist-1.0.0.jar"), []byte("not the jar"), 0o644)
	h.stdin = keysOnceWaiting(t, filepath.Join(h.dir, "downloads"), func() {
		time.Sleep(100 * time.Millisecond)
		os.WriteFile(filepath.Join(elsewhere, "nodist-1.0.0 (1).jar"), h.jars["nodist"].data, 0o644)
	}, "\r")
	code, stdout, stderr = h.run(t, "install")
	if code != 0 || !strings.Contains(stderr, "nodist-1.0.0.jar in "+elsewhere+" isn't the expected file") {
		t.Fatalf("a name match with other bytes is noted, and the duplicate that matches is taken: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestAddWaitsForAManualDownloadAtATerminal(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	downloads := filepath.Join(h.dir, "downloads")
	h.tty = true
	h.stdin = keysOnceWaiting(t, downloads, func() {
		os.WriteFile(filepath.Join(downloads, "nodist.jar"), h.jars["nodist"].data, 0o644)
	}, "\r")
	code, stdout, stderr := h.run(t, "add", "nodist")
	if code != 0 || !strings.Contains(stderr, "1 file needs a manual download into") || !strings.Contains(stderr, downloads) || !strings.Contains(stderr, "nodist-1.0.0.jar") {
		t.Fatalf("add waits for the file and goes on: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, l := readProject(t, h.dir); l.Mods["nodist"].Sha512 != h.jars["nodist"].sha512 {
		t.Fatalf("nodist locks from downloads/: %+v", l.Mods)
	}

	h = newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.tty = true
	h.stdin = keysOnceWaiting(t, filepath.Join(h.dir, "downloads"), func() {}, "\x1b")
	if code, stdout, _ := h.run(t, "--json", "add", "nodist"); code == 0 || failureCode(t, stdout).Code != "manual-download" {
		t.Fatalf("with --json, add fails without waiting: code=%d %s", code, stdout)
	}
	code, stdout, stderr = h.run(t, "add", "nodist")
	if code == 0 || !strings.Contains(stderr, "manual-download") || !strings.Contains(stderr, "○ nodist-1.0.0.jar") {
		t.Fatalf("esc ends the wait and add fails as off a terminal: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestInstallSkipsAMissingLockedFileOnEsc(t *testing.T) {
	h := lockedManualDownload(t)
	h.tty = true
	h.stdin = keysOnceWaiting(t, filepath.Join(h.dir, "downloads"), func() {}, "\x1b")
	code, stdout, stderr := h.run(t, "install")
	if code != 0 || !strings.Contains(stderr, "nodist is left out until its manual download is in downloads/") || !strings.Contains(stdout, "Built client") {
		t.Fatalf("esc builds without the file: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, l := readProject(t, h.dir); l.Mods["nodist"].Sha512 != h.jars["nodist"].sha512 {
		t.Fatalf("the lock keeps the file's hash for the next install: %+v", l.Mods["nodist"])
	}
}
