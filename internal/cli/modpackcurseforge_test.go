package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cfpack"
)

// curseForgeArchive writes a CurseForge zip at rel in h's project naming files, with a config and
// a jar in its overrides folder. note varies the bytes.
func curseForgeArchive(t *testing.T, h *harness, rel, note string, files ...cfpack.File) {
	t.Helper()
	path := filepath.Join(h.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCurseForgeZip(t, path, importedCurseForgePack(files...), map[string][]byte{
		"extras/config/jei.toml":  []byte("jei = " + note + "\n"),
		"extras/mods/bundled.jar": []byte("not really a jar"),
		"modlist.html":            []byte("<ul></ul>"),
	})
}

func curseForgeModpack(t *testing.T, h *harness) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"craft": map[string]any{"type": "modpack", "file": "packs/craft.zip"}}
	})
}

var craftFiles = []cfpack.File{
	{ProjectID: 238222, FileID: 5000001, Required: true},
	{ProjectID: 306612, FileID: 5000010, Required: true},
	{ProjectID: 600000, FileID: 5300001, Required: true},
}

func TestCurseForgeArchiveModpackLocksAndBuilds(t *testing.T) {
	h := archiveProject(t)
	curseForgeArchive(t, h, "packs/craft.zip", "v1", craftFiles...)
	curseForgeModpack(t, h)
	h.mustRun(t, "lock")

	l := h.readLock(t)
	mp := l.Modpacks["craft"]
	if mp.File != "packs/craft.zip" || mp.Sha512 == "" || mp.Size == 0 || !mp.UsesLock {
		t.Fatalf("modpack lock entry: %+v", mp)
	}
	if len(mp.Unmanaged) != 1 || mp.Unmanaged["overrides/mods/bundled.jar"] == "" {
		t.Fatalf("unmanaged: %v", mp.Unmanaged)
	}
	for _, id := range []string{"jei", "fabric-api"} {
		if m := l.Mods[id]; m.Modpack != "craft" || m.Provider != "curseforge" {
			t.Fatalf("%s is tagged to the pack: %+v", id, m)
		}
	}
	if p := l.ResourcePacks["fresh-animations"]; p.Modpack != "craft" || p.Sha512 != h.jars["cf-fresh-animations"].sha512 {
		t.Fatalf("fresh-animations is tagged to the pack: %+v", p)
	}

	h.mustRun(t, "install")
	for _, rel := range []string{"client/mods/" + h.jars["jei"].filename, "client/mods/bundled.jar", "client/config/jei.toml", "client/resourcepacks/fresh-animations.zip", "server/config/jei.toml"} {
		if _, err := os.Stat(filepath.Join(h.dir, "build", rel)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "modlist.html")); !os.IsNotExist(err) {
		t.Fatalf("a file outside the overrides folder is laid: %v", err)
	}
	if stdout := h.mustRun(t, "lock"); strings.Contains(stdout, "jei") {
		t.Fatalf("a second lock moved something: %s", stdout)
	}
}

func TestCurseForgeArchiveModpackManualDownload(t *testing.T) {
	h := archiveProject(t)
	curseForgeArchive(t, h, "packs/craft.zip", "v1", cfpack.File{ProjectID: 238222, FileID: 5000001, Required: true}, cfpack.File{ProjectID: 300000, FileID: 5100001, Required: true})
	curseForgeModpack(t, h)
	code, stdout, _ := h.run(t, "--json", "lock")
	if e := failureCode(t, stdout); code == 0 || e.Code != "missing-files" || len(e.Items) != 1 || !strings.Contains(e.Items[0], "nodist-1.0.0.jar") {
		t.Fatalf("expected missing-files, got %d %s", code, stdout)
	}

	writeProjectFile(t, h, "downloads/nodist-1.0.0.jar", h.jars["nodist"].data)
	h.mustRun(t, "lock")
	nodist := h.readLock(t).Mods["nodist"]
	if nodist.URL != nil || !strings.Contains(nodist.Page, "mc-mods/nodist/files/5100001") || nodist.Modpack != "craft" || nodist.Sha512 != h.jars["nodist"].sha512 {
		t.Fatalf("nodist lock entry: %+v", nodist)
	}
}

func TestCurseForgeArchiveModpackSyncsOfflineWhenUnchanged(t *testing.T) {
	h := newInPlace(t)
	curseForgeModpack(t, h)
	curseForgeArchive(t, h, "packs/craft.zip", "v1", craftFiles...)
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	h.mustRun(t, "sync", "--offline")
}

func TestCurseForgeArchiveModpackNeverResolvesOffline(t *testing.T) {
	h := newInPlace(t)
	curseForgeModpack(t, h)
	curseForgeArchive(t, h, "packs/craft.zip", "v1", craftFiles...)
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	curseForgeArchive(t, h, "packs/craft.zip", "v2", craftFiles...)
	code, stdout, _ := h.run(t, "--json", "sync", "--offline")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "modpack-offline" || !strings.Contains(e.Message, "modpack craft") {
		t.Fatalf("code=%d %+v", code, e)
	}
}

func TestAddCurseForgeArchiveModpack(t *testing.T) {
	h := archiveProject(t)
	outside := filepath.Join(t.TempDir(), "proj")
	inside := h.dir
	h.dir = outside
	curseForgeArchive(t, h, "craft.zip", "v1", craftFiles...)
	h.dir = inside

	h.mustRun(t, "modpack", "add", filepath.Join(outside, "craft.zip"), "--unlocked")
	if entry := h.readManifest(t).Requires["craft"]; entry.File != "files/craft.zip" || entry.Type != "modpack" || entry.Locked == nil || *entry.Locked {
		t.Fatalf("an outside archive is copied into files/: %+v", entry)
	}
	if l := h.readLock(t); l.Modpacks["craft"].File != "files/craft.zip" || l.Modpacks["craft"].UsesLock {
		t.Fatalf("add locks it at once: %+v", l.Modpacks)
	}

	curseForgeArchive(t, h, "packs/other.zip", "v1", craftFiles[:1]...)
	h.mustRun(t, "add", filepath.Join(h.dir, "packs", "other.zip"), "--as", "other")
	if entry := h.readManifest(t).Requires["other"]; entry.File != "packs/other.zip" || entry.Type != "modpack" {
		t.Fatalf("a bare add reads a CurseForge zip as a modpack by what it holds: %+v", entry)
	}
}

func TestCurseForgeArchiveModpackLeavesTheExportersMarkerOut(t *testing.T) {
	h := archiveProject(t)
	h.mustRun(t, "install")
	markers, err := filepath.Glob(filepath.Join(h.dir, "build", "client", "mods", "shulker-*.jar"))
	if err != nil || len(markers) != 1 {
		t.Fatalf("the project builds one marker jar: %v %v", markers, err)
	}
	marker, err := os.ReadFile(markers[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.dir, "packs", "craft.zip")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCurseForgeZip(t, path, importedCurseForgePack(craftFiles...), map[string][]byte{
		"extras/mods/shulker-exporter.jar": marker,
	})
	curseForgeModpack(t, h)
	h.mustRun(t, "lock")

	if u := h.readLock(t).Modpacks["craft"].Unmanaged; len(u) != 0 {
		t.Fatalf("the exporter's marker is not the pack's to lay: %v", u)
	}
	h.mustRun(t, "install")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "mods", "shulker-exporter.jar")); !os.IsNotExist(err) {
		t.Fatalf("a second marker jar reaches the build: %v", err)
	}
}
