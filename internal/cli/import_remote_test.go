package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func importedManifest(t *testing.T, dir string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(filepath.Join(dir, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func readLockAt(t *testing.T, dir string) *lock.Lock {
	t.Helper()
	l, err := lock.Load(filepath.Join(dir, lock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestImportReadsAnArchiveAtAURL(t *testing.T) {
	h := newHarness(t)
	archive := hostedMrpack(t, h, "cozy-1.0.0.mrpack", "1.0.0")
	dir := filepath.Join(t.TempDir(), "cozy")
	h.mustRun(t, "-C", dir, "import", h.server.URL+"/cdn/"+archive.filename)
	if m := importedManifest(t, dir); m.Version != "1.0.0" {
		t.Fatalf("manifest: %+v", m)
	}
	if !(&cache.Cache{Dir: h.cache}).Has(archive.sha512) {
		t.Fatal("the archive isn't cached at its sha512")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "overrides", "config", "cozy.txt")); err != nil || string(data) != "cozy 1.0.0\n" {
		t.Fatalf("override: %q %v", data, err)
	}
}

func TestImportLooksASlugUpAsAModpack(t *testing.T) {
	h := newHarness(t)
	archive := hostedMrpack(t, h, "cozy-1.0.0.mrpack", "1.0.0")
	h.modrinthPacks = map[string]*modrinthPack{"COZYpack": {slug: "cozy", versions: []modrinthPackVersion{{id: "cozyV100", number: "1.0.0", published: "2026-09-01T00:00:00Z", archive: archive}}}}
	dir := filepath.Join(t.TempDir(), "cozy")
	h.mustRun(t, "-C", dir, "import", "cozy")
	if l := readLockAt(t, dir); l.Mods["sodium"].Sha512 == "" {
		t.Fatalf("sodium isn't locked: %+v", l.Mods)
	}
	if code, stdout, _ := h.run(t, "--json", "-C", filepath.Join(t.TempDir(), "x"), "import", "cozy", "--type", "source"); code != out.ExitUsage || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--type source on a slug: exit %d: %s", code, stdout)
	}
	if code, stdout, _ := h.run(t, "--json", "-C", filepath.Join(t.TempDir(), "y"), "import", "cozy", "--ref", "main"); code == 0 || failureCode(t, stdout).Code != "source-ref" {
		t.Fatalf("--ref on a slug: exit %d: %s", code, stdout)
	}
}

func TestImportRefusesASlugThatIsAMod(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "--json", "-C", filepath.Join(t.TempDir(), "x"), "import", "terralith")
	if code == 0 || failureCode(t, stdout).Code != "type-mismatch" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestImportCopiesALocalSource(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "friends")
	h.mustRun(t, "add", "sodium")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "a.txt"), "a")
	source := h.dir
	dir := filepath.Join(t.TempDir(), "copy")
	h.dir = ""
	h.mustRun(t, "-C", dir, "import", source)
	if m := importedManifest(t, dir); m.Name != "friends" || m.Requires["sodium"].Kind() != manifest.TypeMod {
		t.Fatalf("manifest: %+v", m)
	}
	if l := readLockAt(t, dir); l.Mods["sodium"].Sha512 == "" {
		t.Fatalf("lock: %+v", l.Mods)
	}
	if _, err := os.Stat(filepath.Join(dir, "overrides", "config", "a.txt")); err != nil {
		t.Fatal(err)
	}
	if code, stdout, _ := h.run(t, "--json", "-C", filepath.Join(t.TempDir(), "x"), "import", source, "--type", "mrpack"); code != out.ExitUsage || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--type mrpack on a folder: exit %d: %s", code, stdout)
	}
}

func TestImportCopiesAGitSourceAtARefAndPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	repo := h.dir
	h.dir = filepath.Join(repo, "packs", "one")
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "one")
	h.mustRun(t, "add", "sodium")
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "one")
	gitRun(t, repo, "branch", "stable")
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, repo, "clone", "-q", "--bare", repo, remote)
	dir := filepath.Join(t.TempDir(), "copy")
	h.dir = ""
	h.mustRun(t, "-C", dir, "import", "file://"+remote, "--ref", "stable", "--path", "packs/one")
	if m := importedManifest(t, dir); m.Name != "one" {
		t.Fatalf("manifest: %+v", m)
	}
	if l := readLockAt(t, dir); l.Mods["sodium"].Sha512 == "" {
		t.Fatalf("lock: %+v", l.Mods)
	}
}

func TestImportReportsAMissingArchiveURL(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "--json", "-C", filepath.Join(t.TempDir(), "x"), "import", h.server.URL+"/cdn/missing.mrpack")
	if code == 0 || failureCode(t, stdout).Code != "modpack-fetch" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestImportCopiesOnlyASourcesOwnFiles(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "a.txt"), "a")
	writeFile(t, filepath.Join(h.dir, "notes.txt"), "not the project's")
	source := h.dir
	dir := filepath.Join(t.TempDir(), "copy")
	h.dir = ""
	h.mustRun(t, "-C", dir, "import", source)
	for _, rel := range []string{"mods", "options.txt", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Fatalf("%s was copied: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "overrides", "config", "a.txt")); err != nil {
		t.Fatal(err)
	}
}
