package cli

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cfpack"
	"shulker.sh/shulker/internal/mrpack"
)

func writeCurseForgeZip(t *testing.T, path string, m cfpack.Manifest, entries map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	entries[cfpack.ManifestName] = data
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(content)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func importedCurseForgePack(files ...cfpack.File) cfpack.Manifest {
	return cfpack.Manifest{
		Minecraft:    cfpack.Minecraft{Version: "26.2", ModLoaders: []cfpack.ModLoader{{ID: "fabric-0.17.3", Primary: true}}},
		ManifestType: cfpack.ManifestType, ManifestVersion: cfpack.ManifestVersion,
		Name: "Craft Pack", Version: "3.1", Author: "someone", Files: files, Overrides: "extras",
	}
}

func TestImportCurseForge(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(t.TempDir(), "craft.zip")
	writeCurseForgeZip(t, archive, importedCurseForgePack(
		cfpack.File{ProjectID: 238222, FileID: 5000001, Required: true},
		cfpack.File{ProjectID: 306612, FileID: 5000010, Required: true},
		cfpack.File{ProjectID: 600000, FileID: 5300001, Required: true},
		cfpack.File{ProjectID: 394468, FileID: 5000020, Required: false},
	), map[string][]byte{
		"extras/config/jei.toml":  []byte("jei = true\n"),
		"extras/mods/bundled.jar": []byte("not really a jar"),
		"modlist.html":            []byte("<ul></ul>"),
	})

	parent := t.TempDir()
	h.dir = parent
	dir := filepath.Join(parent, "craft-pack")
	stdout := h.mustRun(t, "import", "curseforge", archive, "--dir", dir, "--json")
	var env struct {
		Data     importResult `json:"data"`
		Warnings []string     `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data
	if res.Name != "craft-pack" || res.Version != "3.1" || res.Minecraft != "26.2" || res.Loader.Type != "fabric" || res.Loader.Version != "0.17.3" || strings.Join(res.Sides, ",") != "client" {
		t.Fatalf("result: %+v", res)
	}
	if strings.Join(res.Mods.Locked, ",") != "fabric-api,fresh-animations,jei" || strings.Join(res.Mods.Unmanaged, ",") != "overrides/mods/bundled.jar" {
		t.Fatalf("mods: %+v", res.Mods)
	}
	if strings.Join(res.Overrides, ",") != "overrides/config/jei.toml,overrides/mods/bundled.jar" {
		t.Fatalf("overrides: %v", res.Overrides)
	}
	if strings.Join(env.Warnings, "\n") != "skipped CurseForge project 394468 file 5000020: the pack marks it optional" {
		t.Fatalf("warnings: %v", env.Warnings)
	}
	m, l := readProject(t, dir)
	if m.Minecraft != "26.2" || m.Loader.Type != "fabric" || m.Server != nil || m.Client == nil || strings.Join(m.Authors, ",") != "someone" {
		t.Fatalf("manifest: %+v", m)
	}
	if jei := m.Mods()["jei"]; jei.Provider != "curseforge" || jei.Project.(json.Number) != "238222" || jei.Pin != nil {
		t.Fatalf("jei manifest entry: %+v", jei)
	}
	if pack := m.ResourcePacks()["fresh-animations"]; pack.Provider != "curseforge" || pack.Project.(json.Number) != "600000" {
		t.Fatalf("fresh-animations manifest entry: %+v", pack)
	}
	jei := l.Mods["jei"]
	if jei.Provider != "curseforge" || jei.Version.(json.Number) != "5000001" || jei.Sha512 != h.jars["jei"].sha512 || jei.URL == nil {
		t.Fatalf("jei lock entry: %+v", jei)
	}
	if pack := l.ResourcePacks["fresh-animations"]; pack.Version.(json.Number) != "5300001" || pack.Sha512 != h.jars["cf-fresh-animations"].sha512 {
		t.Fatalf("fresh-animations lock entry: %+v", pack)
	}
	if _, ok := l.Mods["sodium"]; ok {
		t.Fatal("an optional file was locked")
	}
	for _, rel := range []string{"overrides/config/jei.toml", "overrides/mods/bundled.jar"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatal(err)
		}
	}

	h.dir = dir
	h.mustRun(t, "install")
	for _, rel := range []string{"build/client/mods/" + h.jars["jei"].filename, "build/client/mods/bundled.jar", "build/client/config/jei.toml", "build/client/resourcepacks/fresh-animations.zip"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatal(err)
		}
	}
	if stdout := h.mustRun(t, "lock"); strings.Contains(stdout, "jei") {
		t.Fatalf("lock after import moved something: %s", stdout)
	}
}

func TestImportCurseForgeManualDownload(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(t.TempDir(), "craft.zip")
	writeCurseForgeZip(t, archive, importedCurseForgePack(
		cfpack.File{ProjectID: 238222, FileID: 5000001, Required: true},
		cfpack.File{ProjectID: 300000, FileID: 5100001, Required: true},
		cfpack.File{ProjectID: 400000, FileID: 5200001, Required: true},
	), map[string][]byte{})
	parent := t.TempDir()
	h.dir = parent
	dir := filepath.Join(parent, "craft-pack")

	code, stdout, _ := h.run(t, "--json", "import", "curseforge", archive, "--dir", dir)
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "missing-files" || len(e.Items) != 2 || !strings.Contains(e.Items[0], "nodist-1.0.0.jar from https://www.curseforge.com/minecraft/mc-mods/nodist/files/5100001") || !strings.Contains(e.Items[1], "locked-1.0.0.jar") || !strings.Contains(e.Items[1], filepath.Join(dir, "downloads")) {
		t.Fatalf("expected missing-files, got %d %s", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "shulker.json")); err == nil {
		t.Fatal("a failed import wrote a manifest")
	}

	downloads := filepath.Join(dir, "downloads")
	os.MkdirAll(downloads, 0o755)
	os.WriteFile(filepath.Join(downloads, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	os.WriteFile(filepath.Join(downloads, "locked-1.0.0.jar"), h.jars["locked"].data, 0o644)
	h.mustRun(t, "import", "curseforge", archive, "--dir", dir)
	_, l := readProject(t, dir)
	nodist := l.Mods["nodist"]
	if nodist.URL != nil || nodist.Page != "https://www.curseforge.com/minecraft/mc-mods/nodist/files/5100001" || nodist.Sha512 != h.jars["nodist"].sha512 {
		t.Fatalf("nodist lock entry: %+v", nodist)
	}
	if locked := l.Mods["locked"]; locked.URL != nil || !strings.Contains(locked.Page, "mc-mods/locked/files/5200001") {
		t.Fatalf("locked lock entry: %+v", locked)
	}
}

func TestImportCurseForgeRefusesOtherArchives(t *testing.T) {
	h := newHarness(t)
	h.dir = t.TempDir()
	mrpackFile := filepath.Join(t.TempDir(), "pack.mrpack")
	writeMrpack(t, mrpackFile, mrpack.Index{FormatVersion: 1, Game: "minecraft", VersionID: "1", Name: "x", Dependencies: map[string]string{"minecraft": "26.2"}}, map[string][]byte{})
	resourcePack := filepath.Join(t.TempDir(), "pack.zip")
	f, _ := os.Create(resourcePack)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("pack.mcmeta")
	w.Write([]byte("{}"))
	zw.Close()
	f.Close()
	for _, file := range []string{mrpackFile, resourcePack} {
		code, stdout, _ := h.run(t, "--json", "import", "curseforge", file)
		if e := failureCode(t, stdout); code == 0 || e.Code != "archive-not-modpack" {
			t.Fatalf("%s: expected archive-not-modpack, got %d %s", file, code, stdout)
		}
	}
	_, stdout, _ := h.run(t, "--json", "import", "curseforge", mrpackFile)
	if e := failureCode(t, stdout); !strings.Contains(e.Help, "shulker import mrpack") {
		t.Fatalf("help: %+v", e)
	}
}

func TestImportCurseForgeRoundTrip(t *testing.T) {
	h := newCurseForgeExport(t)
	h.mustRun(t, "export", "curseforge")
	archive := filepath.Join(h.dir, "build", "pack-1.0.zip")
	exported := h.readLock(t)

	dir := filepath.Join(t.TempDir(), "demo")
	h.mustRun(t, "import", "curseforge", archive, "--dir", dir)
	m, l := readProject(t, dir)
	if m.Name != "demo-pack" || m.Version != "1.0" || strings.Join(m.Authors, ",") != "Ann, Bo" || l.Loader.Type != "fabric" || l.Loader.Version != exported.Loader.Version {
		t.Fatalf("project: %+v %+v", m, l.Loader)
	}
	for _, id := range []string{"sodium", "jei", "fabric-api"} {
		if l.Mods[id].Provider != "curseforge" || l.Mods[id].Sha512 != exported.Mods[id].Sha512 {
			t.Fatalf("%s: %+v, exported %+v", id, l.Mods[id], exported.Mods[id])
		}
	}
	if len(l.Mods) != 3 {
		t.Fatalf("mods: %v", l.Mods)
	}
}
