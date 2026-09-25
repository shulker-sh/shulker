package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readerFunc is a stdin that acts when read, so a test can drop a file into downloads/ at the
// moment the run waits for one.
type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// enterAfter answers enter each time it is read, running then before the nth answer.
func enterAfter(n int, then func()) readerFunc {
	reads := 0
	return func(p []byte) (int, error) {
		reads++
		if reads == n {
			then()
		}
		p[0] = '\n'
		return 1, nil
	}
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
	h.stdin = enterAfter(2, func() {
		if _, err := os.Stat(downloads); err != nil {
			t.Errorf("downloads/ is created before the wait: %v", err)
		}
		os.WriteFile(filepath.Join(downloads, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	})

	code, stdout, stderr := h.run(t, "install")
	if code != 0 {
		t.Fatalf("install after the file arrives: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if n := strings.Count(stderr, "1 file(s) need a manual download"); n != 2 {
		t.Fatalf("the wait lists the files each time they are missing (%d): %s", n, stderr)
	}
	if !strings.Contains(stderr, downloads) || !strings.Contains(stderr, "nodist-1.0.0.jar from https://www.curseforge.com") || !strings.Contains(stderr, "press enter") {
		t.Fatalf("the wait names the folder and each file's page: %s", stderr)
	}
	if !strings.Contains(stdout, "built client") {
		t.Fatalf("install goes on after the wait: %s", stdout)
	}
}

func TestInstallFailsMissingFilesWhereItCantWait(t *testing.T) {
	h := lockedManualDownload(t)
	answered := false
	h.stdin = readerFunc(func(p []byte) (int, error) { answered = true; p[0] = '\n'; return 1, nil })
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
		if code == 0 || !strings.Contains(stdout+stderr, "missing-files") || answered {
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
	h.stdin = enterAfter(1, func() {
		if _, err := os.Stat(downloads); err != nil {
			t.Errorf("downloads/ is created before the wait: %v", err)
		}
		os.WriteFile(filepath.Join(downloads, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	})

	code, stdout, stderr := h.run(t, "import", archive, "--dir", dir)
	if code != 0 || !strings.Contains(stderr, downloads) || !strings.Contains(stderr, "press enter") {
		t.Fatalf("import: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, l := readProject(t, dir); l.Mods["nodist"].Sha512 != h.jars["nodist"].sha512 {
		t.Fatalf("nodist locks from downloads/: %+v", l.Mods)
	}

	h.tty = false
	if code, stdout, _ := h.run(t, "--json", "import", archive, "--dir", filepath.Join(t.TempDir(), "again")); code == 0 || failureCode(t, stdout).Code != "missing-files" {
		t.Fatalf("off a terminal import fails: code=%d %s", code, stdout)
	}
}
