package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// writeMergePack writes a Modrinth pack for 26.2 on loaderKey listing jars in its index, on both sides,
// with entries as its override files.
func writeMergePack(t *testing.T, h *harness, loaderKey, loaderVersion string, jars []fakeJar, entries map[string][]byte) string {
	t.Helper()
	var files []mrpack.File
	for _, jar := range jars {
		files = append(files, mrpack.File{Path: "mods/" + jar.filename, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: mrpack.Env("both"), Downloads: []string{h.server.URL + "/cdn/" + jar.filename}, FileSize: int64(len(jar.data))})
	}
	path := filepath.Join(t.TempDir(), "merged.mrpack")
	writeMrpack(t, path, mrpack.Index{FormatVersion: 1, Game: "minecraft", VersionID: "3.0", Name: "Merged", Files: files, Dependencies: map[string]string{"minecraft": "26.2", loaderKey: loaderVersion}}, entries)
	return path
}

type mergeResult struct {
	Data struct {
		Merged    bool     `json:"merged"`
		KeptYours []string `json:"keptYours"`
		LeftOut   []string `json:"leftOut"`
	} `json:"data"`
}

func importMerge(t *testing.T, h *harness, args ...string) mergeResult {
	t.Helper()
	var res mergeResult
	if err := json.Unmarshal([]byte(h.mustRun(t, append([]string{"import", "--json"}, args...)...)), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Data.Merged {
		t.Fatalf("not a merge: %+v", res)
	}
	return res
}

func TestImportMergeKeepsTheProjectsVersion(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "fabric-api")
	h.newerAPI = true
	archive := writeMergePack(t, h, "fabric-loader", "0.17.3", []fakeJar{h.jars["fabric-api-next"], h.jars["sodium"]}, nil)
	res := importMerge(t, h, archive)
	if !slices.Contains(res.Data.KeptYours, "fabric-api") {
		t.Fatalf("kept yours: %v", res.Data.KeptYours)
	}
	l := h.readLock(t)
	if l.Mods["fabric-api"].Sha512 != h.jars["fabric-api"].sha512 {
		t.Fatalf("fabric-api moved: %+v", l.Mods["fabric-api"])
	}
	if l.Mods["sodium"].Sha512 != h.jars["sodium"].sha512 {
		t.Fatalf("sodium not merged: %+v", l.Mods["sodium"])
	}
	entry, ok := h.readManifest(t).Requires["sodium"]
	if !ok || entry.Pin != "" {
		t.Fatalf("sodium entry: %+v %v", entry, ok)
	}
	if code, stdout, _ := h.run(t, "--json", "import", archive, "--name", "x"); code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--name on a merge: exit %d: %s", code, stdout)
	}
}

func TestImportMergeKeepsYourOverride(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "a.txt"), "mine")
	archive := writeMergePack(t, h, "fabric-loader", "0.17.3", nil, map[string][]byte{"overrides/config/a.txt": []byte("theirs"), "overrides/config/b.txt": []byte("theirs")})
	res := importMerge(t, h, archive)
	if !slices.Contains(res.Data.KeptYours, "overrides/config/a.txt") {
		t.Fatalf("kept yours: %v", res.Data.KeptYours)
	}
	for file, want := range map[string]string{"a.txt": "mine", "b.txt": "theirs"} {
		if data, err := os.ReadFile(filepath.Join(h.dir, "overrides", "config", file)); err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", file, data, err)
		}
	}
}

func TestImportMergeRefusesAnotherPlatform(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	manifestBefore, _ := os.ReadFile(filepath.Join(h.dir, manifest.FileName))
	lockBefore, _ := os.ReadFile(filepath.Join(h.dir, lock.FileName))
	archive := writeMergePack(t, h, "neoforge", "26.2.0.87", nil, map[string][]byte{"overrides/config/n.txt": []byte("neo")})
	if code, stdout, _ := h.run(t, "--json", "import", archive); code == 0 || failureCode(t, stdout).Code != "import-mismatch" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	manifestAfter, _ := os.ReadFile(filepath.Join(h.dir, manifest.FileName))
	lockAfter, _ := os.ReadFile(filepath.Join(h.dir, lock.FileName))
	if !bytes.Equal(manifestBefore, manifestAfter) || !bytes.Equal(lockBefore, lockAfter) {
		t.Fatal("a refused merge wrote the project")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "overrides", "config", "n.txt")); !os.IsNotExist(err) {
		t.Fatalf("a refused merge wrote an override: %v", err)
	}
}

func TestImportMergeTakesOnlyTheProjectsSides(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	if h.readManifest(t).HasSide("server") {
		t.Fatal("init made a server side")
	}
	sodium := h.jars["sodium"]
	archive := writeMergePack(t, h, "fabric-loader", "0.17.3", []fakeJar{h.jars["fabric-api"]}, map[string][]byte{
		"server-overrides/mods/" + sodium.filename: sodium.data,
		"server-overrides/config/s.txt":            []byte("server"),
	})
	res := importMerge(t, h, archive)
	if !slices.Contains(res.Data.LeftOut, "sodium") || !slices.Contains(res.Data.LeftOut, "server-overrides/config/s.txt") {
		t.Fatalf("left out: %v", res.Data.LeftOut)
	}
	if _, ok := h.readLock(t).Mods["sodium"]; ok {
		t.Fatal("the server-only mod came in")
	}
	if _, ok := h.readLock(t).Mods["fabric-api"]; !ok {
		t.Fatal("the both-side mod didn't come in")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "server-overrides")); !os.IsNotExist(err) {
		t.Fatalf("server overrides written: %v", err)
	}
	if h.readManifest(t).HasSide("server") {
		t.Fatal("the pack's server block came in")
	}
}

func TestImportMergesAMarkersFeaturesAndVariables(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "source")
	h.editManifest(t, func(m map[string]any) {
		m["version"] = "1.0.0"
		m["variables"] = map[string]any{"a": "pack", "b": "pack"}
		m["features"] = map[string]any{"extra": map[string]any{"note": "from the pack"}, "shared": map[string]any{"note": "the pack's"}}
	})
	h.mustRun(t, "lock")
	h.mustRun(t, "export", "mrpack")
	archive := filepath.Join(h.dir, "build", "source-1.0.0.mrpack")

	h.dir = t.TempDir()
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "mine")
	h.editManifest(t, func(m map[string]any) {
		m["variables"] = map[string]any{"a": "mine"}
		m["features"] = map[string]any{"shared": map[string]any{"note": "mine"}}
	})
	h.mustRun(t, "lock")
	importMerge(t, h, archive)
	m := h.readManifest(t)
	if m.Name != "mine" || m.Variables["a"] != "mine" || m.Variables["b"] != "pack" {
		t.Fatalf("variables: %s %v", m.Name, m.Variables)
	}
	if m.Features["extra"].Note != "from the pack" || m.Features["shared"].Note != "mine" {
		t.Fatalf("features: %+v", m.Features)
	}
}

func TestImportMergeIntoAnInstanceTakesAHistoryEntry(t *testing.T) {
	h := newInPlace(t)
	archive := writeMergePack(t, h, "fabric-loader", "0.17.3", []fakeJar{h.jars["sodium"]}, nil)
	importMerge(t, h, archive)
	entries, err := build.History(h.dir)
	if err != nil || len(entries) != 1 || entries[0].Reason != "import" {
		t.Fatalf("history: %+v %v", entries, err)
	}
}

func TestImportMergeRefusesAnotherLoaderVersion(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	archive := writeMergePack(t, h, "fabric-loader", "0.16.0", nil, nil)
	if code, stdout, _ := h.run(t, "--json", "import", archive); code == 0 || failureCode(t, stdout).Code != "import-mismatch" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestImportMergeTakesTheSlugsVersionForTheProjectsLoader(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	older := hostedMrpack(t, h, "cozy-1.0.0.mrpack", "1.0.0")
	newer := hostedMrpack(t, h, "cozy-2.0.0.mrpack", "2.0.0")
	h.modrinthPacks = map[string]*modrinthPack{"COZYpack": {slug: "cozy", versions: []modrinthPackVersion{
		{id: "cozyV100", number: "1.0.0", published: "2026-09-01T00:00:00Z", archive: older},
		{id: "cozyV200", number: "2.0.0", published: "2026-09-05T00:00:00Z", archive: newer, loaders: []string{"neoforge"}},
	}}}
	importMerge(t, h, "cozy")
	if data, err := os.ReadFile(filepath.Join(h.dir, "overrides", "config", "cozy.txt")); err != nil || string(data) != "cozy 1.0.0\n" {
		t.Fatalf("override: %q %v", data, err)
	}
}

func TestMergeFailsOnALocalFileThePackLacks(t *testing.T) {
	dir := t.TempDir()
	p := &project.Project{Dir: dir, Manifest: &manifest.Manifest{Name: "p", Requires: map[string]manifest.Require{}}, Lock: lock.New()}
	pl := lock.New()
	pl.Mods["gone"] = lock.Mod{File: "files/gone.jar"}
	inc := &incoming{manifest: &manifest.Manifest{Name: "pack", Requires: map[string]manifest.Require{"gone": {File: "files/gone.jar"}}}, lock: pl, dir: t.TempDir()}
	if _, err := mergePack(p, inc, []string{"client"}); out.CodeOf(err) != "local-file-missing" {
		t.Fatalf("got %v", err)
	}
}
