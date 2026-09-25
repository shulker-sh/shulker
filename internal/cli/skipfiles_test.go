package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverridesLeaveOutSkippedFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.allowMrpackHost(t)
	h.editManifest(t, func(m map[string]any) {
		m["skipFiles"] = []string{"*.bak"}
	})
	for _, rel := range []string{".DS_Store", "config/.DS_Store", "config/._mod.json", "config/Thumbs.db", "config/desktop.ini", "config/mod.json.bak"} {
		writeFile(t, filepath.Join(h.dir, "overrides", filepath.FromSlash(rel)), "junk")
	}
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "mod.json"), "{}")

	h.mustRun(t, "build")
	built := filepath.Join(h.dir, "build", "client")
	if _, err := os.Stat(filepath.Join(built, "config", "mod.json")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".DS_Store", "config/.DS_Store", "config/._mod.json", "config/Thumbs.db", "config/desktop.ini", "config/mod.json.bak"} {
		if _, err := os.Stat(filepath.Join(built, filepath.FromSlash(rel))); err == nil {
			t.Errorf("build placed %s", rel)
		}
	}

	h.mustRun(t, "export", "mrpack", "--version", "0.1")
	_, mrpackEntries := readMrpack(t, filepath.Join(h.dir, "build", "pack-0.1.mrpack"))
	h.mustRun(t, "export", "curseforge", "--version", "0.1")
	cfEntries := readArchive(t, filepath.Join(h.dir, "build", "pack-0.1.zip"))
	for name, entries := range map[string]map[string]string{"mrpack": mrpackEntries, "curseforge": cfEntries} {
		if _, ok := entries["overrides/config/mod.json"]; !ok {
			t.Errorf("%s left out config/mod.json: %v", name, keys(entries))
		}
		for entry := range entries {
			if base := filepath.Base(entry); base == ".DS_Store" || strings.HasPrefix(base, "._") || base == "Thumbs.db" || base == "desktop.ini" || strings.HasSuffix(base, ".bak") {
				t.Errorf("%s holds %s", name, entry)
			}
		}
	}
}

func TestLocalModpackIgnoresSkippedFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	writePrismPack(t, filepath.Join(h.dir, "base"), "~26.2", `"sodium": {}`, map[string]string{"config/base.txt": "from pack\n"})
	h.mustRun(t, "modpack", "add", "./base")

	writeFile(t, filepath.Join(h.dir, "base", "overrides", "config", ".DS_Store"), "junk")
	if stdout := h.mustRun(t, "modpack", "list"); !strings.Contains(stdout, "(local, ok") {
		t.Fatalf("a .DS_Store should not change a local pack: %s", stdout)
	}
	h.mustRun(t, "build")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", ".DS_Store")); err == nil {
		t.Fatal("build placed the pack's .DS_Store")
	}
}

func TestPullLeavesSkippedFilesInTheBuild(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "build")
	writeFile(t, filepath.Join(h.dir, "build", "client", "config", ".DS_Store"), "junk")

	stdout := h.mustRun(t, "pull", "config/.DS_Store")
	if !strings.Contains(stdout, "config/.DS_Store (left out of builds)") {
		t.Fatalf("pull should skip a file builds leave out: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "overrides", "config", ".DS_Store")); err == nil {
		t.Fatal("pull copied a .DS_Store into overrides")
	}

	code, stdout, _ := h.run(t, "pull", "config/.DS_Stor", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "file-not-found" || len(e.Candidates) != 0 {
		t.Fatalf("a skipped file should not be suggested: exit %d %s", code, stdout)
	}
}
