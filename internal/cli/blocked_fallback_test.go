package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestImportLocksABlockedCurseForgeFileFromModrinth(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(t.TempDir(), "blocked.zip")
	writeCurseForgeZip(t, archive, importedCurseForgePack(cfPackFile{ProjectID: 300002, FileID: 5100002, Required: true}), map[string][]byte{})
	dir := filepath.Join(t.TempDir(), "craft-pack")

	code, stdout, stderr := h.run(t, "import", archive, "--dir", dir)
	if code != 0 {
		t.Fatalf("import: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "iris-cf: CurseForge doesn't allow third-party downloads of "+h.jars["irisshaders"].filename+"; locked from Modrinth as irisshaders instead") {
		t.Fatalf("the fallback warns naming both projects: %s", stderr)
	}
	m, l := readProject(t, dir)
	if entry := m.Requires["iris"]; entry.Provider != "" || entry.Project != "YL57xq9U" {
		t.Fatalf("manifest entry: %+v", entry)
	}
	if locked := l.Mods["iris"]; locked.Provider != "modrinth" || locked.Project != "YL57xq9U" || locked.Sha512 != h.jars["irisshaders"].sha512 || locked.URL == nil {
		t.Fatalf("lock entry: %+v", locked)
	}
}

func TestAddLocksABlockedCurseForgeFileFromModrinth(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	stdout, stderr := h.mustRunStderr(t, "add", "iris-cf", "--provider", "curseforge")
	if !strings.Contains(stderr, "iris-cf: CurseForge doesn't allow third-party downloads of "+h.jars["irisshaders"].filename+"; locked from Modrinth as irisshaders instead") {
		t.Fatalf("the fallback warns naming both projects: %s\n%s", stderr, stdout)
	}
	if entry := h.readManifest(t).Requires["iris"]; entry.Provider != "" || entry.Project != "YL57xq9U" {
		t.Fatalf("manifest entry: %+v", entry)
	}
	if locked := h.readLock(t).Mods["iris"]; locked.Provider != "modrinth" || locked.Project != "YL57xq9U" {
		t.Fatalf("lock entry: %+v", locked)
	}

	code, stdout, _ := h.run(t, "--json", "add", "nodist", "--provider", "curseforge")
	if code == 0 || failureCode(t, stdout).Code != "manual-download" {
		t.Fatalf("a blocked file Modrinth lacks still needs a manual download: code=%d %s", code, stdout)
	}
}
