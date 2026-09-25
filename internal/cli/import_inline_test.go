package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// checkInlined fails unless the modpack key left no trace: no requires entry, no lock section and
// no entry tagged with it, and mod removes as the project's own.
func checkInlined(t *testing.T, h *harness, key, mod string) {
	t.Helper()
	if _, ok := h.readManifest(t).Requires[key]; ok {
		t.Fatalf("requires.%s is still there", key)
	}
	l := h.readLock(t)
	if _, ok := l.Modpacks[key]; ok {
		t.Fatalf("modpacks.%s is still there", key)
	}
	for id, m := range l.Mods {
		if m.Modpack != "" {
			t.Fatalf("%s is still tagged with modpack %s", id, m.Modpack)
		}
	}
	for _, section := range l.PackSections() {
		for id, p := range section {
			if p.Modpack != "" {
				t.Fatalf("%s is still tagged with modpack %s", id, p.Modpack)
			}
		}
	}
	if _, ok := h.readManifest(t).Requires[mod]; !ok {
		t.Fatalf("%s isn't the project's own", mod)
	}
	h.mustRun(t, "remove", mod)
}

func refusesRemoval(t *testing.T, h *harness, mod string) {
	t.Helper()
	if code, stdout, _ := h.run(t, "--json", "remove", mod); code == 0 || failureCode(t, stdout).Code != "modpack-provided" {
		t.Fatalf("remove %s before inlining: exit %d: %s", mod, code, stdout)
	}
}

func TestImportInlinesALockedArchiveModpackOffline(t *testing.T) {
	h := archiveProject(t)
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"}}
	})
	h.mustRun(t, "lock")
	refusesRemoval(t, h, "sodium")
	h.server.Close()
	h.mustRun(t, "import", "someone")
	if data, err := os.ReadFile(filepath.Join(h.dir, "overrides", "config", "shared.txt")); err != nil || string(data) != "shared v1\n" {
		t.Fatalf("override: %q %v", data, err)
	}
	checkInlined(t, h, "someone", "sodium")
}

func TestImportInlinesALockedGitSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	project := h.dir
	h.dir = filepath.Join(t.TempDir(), "base")
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "create", "--loader", "fabric", "--name", "base")
	h.mustRun(t, "add", "sodium")
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "base")
	remote := filepath.Join(t.TempDir(), "base.git")
	gitRun(t, h.dir, "clone", "-q", "--bare", h.dir, remote)
	h.dir = project
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "modpack", "add", "file://"+remote)
	if h.readLock(t).Modpacks["base"].Commit == "" {
		t.Fatalf("not locked at a commit: %+v", h.readLock(t).Modpacks)
	}
	refusesRemoval(t, h, "sodium")
	h.mustRun(t, "import", "base", "--type", "modpack")
	checkInlined(t, h, "base", "sodium")
}

func TestImportInlinesAFloatingLocalSource(t *testing.T) {
	h := newHarness(t)
	project := h.dir
	h.dir = filepath.Join(project, "base")
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "create", "--loader", "fabric", "--name", "base")
	h.mustRun(t, "add", "sodium")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "base.txt"), "base")
	h.dir = project
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "modpack", "add", "./base", "--unlocked")
	refusesRemoval(t, h, "sodium")
	h.mustRun(t, "import", "base")
	if _, err := os.Stat(filepath.Join(h.dir, "overrides", "config", "base.txt")); err != nil {
		t.Fatal(err)
	}
	checkInlined(t, h, "base", "sodium")
}

func TestImportTypeModpackRefusesAnythingElse(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	if code, stdout, _ := h.run(t, "--json", "import", "sodium", "--type", "modpack"); code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}
