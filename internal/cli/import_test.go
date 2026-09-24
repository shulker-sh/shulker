package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/resolve"
)

func readProject(t *testing.T, dir string) (*manifest.Manifest, *lock.Lock) {
	t.Helper()
	m, err := manifest.Load(filepath.Join(dir, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	l, err := lock.Load(filepath.Join(dir, lock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return m, l
}

func writeMrpack(t *testing.T, path string, index mrpack.Index, entries map[string][]byte) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create(mrpack.IndexName)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(data)
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
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rewriteMrpack(t *testing.T, src, dst string, edit func(index *mrpack.Index, entries map[string][]byte)) {
	t.Helper()
	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var index mrpack.Index
	entries := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		b.ReadFrom(r)
		r.Close()
		if f.Name == mrpack.IndexName {
			if err := json.Unmarshal(b.Bytes(), &index); err != nil {
				t.Fatal(err)
			}
			continue
		}
		entries[f.Name] = b.Bytes()
	}
	edit(&index, entries)
	writeMrpack(t, dst, index, entries)
}

func TestImportMrpackRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.allowMrpackHost(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	if err := os.MkdirAll(filepath.Join(h.dir, "overrides", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "overrides", "config", "x.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "export", "mrpack", "--version", "1.0.0")
	archive := filepath.Join(h.dir, "build", "pack-1.0.0.mrpack")

	dir := filepath.Join(t.TempDir(), "imported")
	var env struct {
		Data importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", dir, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data
	if !res.Marker || res.Version != "1.0.0" || res.Minecraft != "26.2" || strings.Join(res.Sides, ",") != "client" {
		t.Fatalf("result: %+v", res)
	}
	if strings.Join(res.Mods.Reused, ",") != "fabric-api,sodium" || len(res.Mods.Locked) != 0 || len(res.Mods.Dropped) != 0 || len(res.Mods.Unmanaged) != 0 {
		t.Fatalf("mods: %+v", res.Mods)
	}
	if strings.Join(res.Overrides, ",") != "overrides/config/x.txt" {
		t.Fatalf("overrides: %v", res.Overrides)
	}
	m, l := readProject(t, dir)
	if m.Name != "pack" || m.Version != "1.0.0" || len(m.Mods()) != 1 || m.Client == nil {
		t.Fatalf("manifest: %+v", m)
	}
	if _, ok := m.Mods()["sodium"]; !ok {
		t.Fatalf("manifest mods: %v", m.Mods())
	}
	if m.Server != nil {
		t.Fatalf("a client-only pack declared a server: %+v", m.Server)
	}
	if len(l.Mods) != 2 || strings.Join(l.Mods["fabric-api"].RequiredBy, ",") != "sodium" {
		t.Fatalf("lock: %+v", l)
	}
	if _, err := os.Stat(filepath.Join(dir, "overrides", "options.txt")); !os.IsNotExist(err) {
		t.Fatalf("options.txt is owned by client.options and must not be imported as an override: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "overrides", "mods")); !os.IsNotExist(err) {
		t.Fatalf("the marker jar must not be imported as an override: %v", err)
	}

	if stdout := h.mustRun(t, "import", archive, "--dir", dir, "--json"); !strings.Contains(stdout, `"merged": true`) {
		t.Fatalf("import over an existing project merges: %s", stdout)
	}
}

func TestImportMrpackIgnoreShulker(t *testing.T) {
	h := newHarness(t)
	h.allowMrpackHost(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "export", "mrpack", "--version", "1.0.0")
	archive := filepath.Join(h.dir, "build", "pack-1.0.0.mrpack")

	dir := filepath.Join(t.TempDir(), "imported")
	var env struct {
		Data importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", dir, "--ignore-shulker", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	// The pack's own manifest and lock are ignored, so every mod is looked up
	// from the index instead of reused.
	if res := env.Data; res.Marker || len(res.Mods.Reused) != 0 || len(res.Mods.Locked) != 2 {
		t.Fatalf("result: %+v", res)
	}
}

func TestImportMrpackVanillaRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.allowMrpackHost(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	h.mustRun(t, "export", "mrpack", "--version", "1.0.0")
	archive := filepath.Join(h.dir, "build", "pack-1.0.0.mrpack")

	dir := filepath.Join(t.TempDir(), "imported")
	var env struct {
		Data importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", dir, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	// A project with no loader ships no marker jar, so only the manifest and lock
	// at the archive root can carry it back.
	if res := env.Data; !res.Marker || res.Version != "1.0.0" || res.Loader.Type != "" {
		t.Fatalf("result: %+v", res)
	}
	m, l := readProject(t, dir)
	if m.Loader.Type != "" {
		t.Fatalf("an imported vanilla pack must stay vanilla: %+v", m.Loader)
	}
	if _, ok := l.ResourcePacks["fresh-animations"]; !ok {
		t.Fatalf("resource packs: %+v", l.ResourcePacks)
	}
	if _, ok := l.Shaders["complementary-reimagined"]; !ok {
		t.Fatalf("shaders: %+v", l.Shaders)
	}
}

func TestImportMrpackTamperedMarker(t *testing.T) {
	h := newHarness(t)
	h.allowMrpackHost(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "export", "mrpack", "--version", "1.0.0")
	archive := filepath.Join(h.dir, "build", "pack-1.0.0.mrpack")
	tampered := filepath.Join(t.TempDir(), "tampered.mrpack")
	rewriteMrpack(t, archive, tampered, func(index *mrpack.Index, entries map[string][]byte) {
		files := index.Files[:0]
		for _, f := range index.Files {
			if f.Path == "mods/"+h.jars["sodium"].filename {
				continue
			}
			files = append(files, f)
		}
		index.Files = files
	})

	dir := filepath.Join(t.TempDir(), "imported")
	var env struct {
		Warnings []string     `json:"warnings"`
		Data     importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", tampered, "--dir", dir, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data
	if strings.Join(res.Mods.Reused, ",") != "fabric-api" || strings.Join(res.Mods.Dropped, ",") != "sodium" {
		t.Fatalf("mods: %+v", res.Mods)
	}
	if len(env.Warnings) != 0 {
		t.Fatalf("warnings: %v", env.Warnings)
	}
	m, l := readProject(t, dir)
	if len(m.Mods()) != 0 || m.Minecraft != "26.2" {
		t.Fatalf("manifest: %+v", m)
	}
	if len(l.Mods) != 1 || len(l.Mods["fabric-api"].RequiredBy) != 0 {
		t.Fatalf("lock: %+v", l.Mods)
	}
}

func TestImportMrpackForeign(t *testing.T) {
	h := newHarness(t)
	extra := makeJar(t, "extra", "extra-1.0.jar", "*")
	h.jars["extra"] = extra
	sodium, fabricAPI := h.jars["sodium"], h.jars["fabric-api"]
	file := func(jar fakeJar, side string) mrpack.File {
		return mrpack.File{Path: "mods/" + jar.filename, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: mrpack.Env(side), Downloads: []string{h.server.URL + "/cdn/" + jar.filename}, FileSize: int64(len(jar.data))}
	}
	index := mrpack.Index{
		FormatVersion: 1, Game: "minecraft", VersionID: "2.0", Name: "Someone's Pack", Summary: "hello",
		Files:        []mrpack.File{file(sodium, "client"), file(fabricAPI, "both"), file(extra, "server")},
		Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	}
	archive := filepath.Join(t.TempDir(), "foreign.mrpack")
	writeMrpack(t, archive, index, map[string][]byte{
		"overrides/config/shared.txt":        []byte("shared\n"),
		"client-overrides/config/client.txt": []byte("client\n"),
		"overrides/mods/local-1.0.jar":       []byte("not really a jar"),
	})

	parent := t.TempDir()
	h.dir = parent
	var env struct {
		Data importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", filepath.Join(parent, "someone-s-pack"), "--json")), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data
	dir := filepath.Join(parent, "someone-s-pack")
	if res.Marker || res.Name != "someone-s-pack" || res.Version != "2.0" || strings.Join(res.Sides, ",") != "client,server" {
		t.Fatalf("result: %+v", res)
	}
	if strings.Join(res.Mods.LockedIDs(), ",") != "fabric-api,sodium" || strings.Join(res.Mods.Unmanaged, ",") != "overrides/mods/local-1.0.jar,server-overrides/mods/extra-1.0.jar" {
		t.Fatalf("mods: %+v", res.Mods)
	}
	m, l := readProject(t, dir)
	if m.Note != "hello" || m.Minecraft != "26.2" || m.Loader.Version != "0.17.3" || m.Server == nil || len(m.Mods()) != 2 {
		t.Fatalf("manifest: %+v", m)
	}
	if got := m.Sides(); strings.Join(got, ",") != "client,server" {
		t.Fatalf("sides: %v", got)
	}
	if l.Mods["sodium"].Side != "client" || l.Mods["fabric-api"].Side != "both" || l.Mods["sodium"].VersionNumber != "1.0.0+mc26.2" {
		t.Fatalf("lock mods: %+v", l.Mods)
	}
	for _, rel := range []string{"overrides/config/shared.txt", "client-overrides/config/client.txt", "overrides/mods/local-1.0.jar", "server-overrides/mods/extra-1.0.jar"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatal(err)
		}
	}

	h.dir = dir
	stdout := h.mustRun(t, "install")
	if !strings.Contains(stdout, "server") && !strings.Contains(stdout, "client") {
		t.Fatalf("install after import: %s", stdout)
	}
	for _, rel := range []string{"build/client/mods/" + sodium.filename, "build/client/config/client.txt", "build/server/mods/extra-1.0.jar", "build/server/config/shared.txt"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImportMrpackLocksHostedPacks(t *testing.T) {
	h := newHarness(t)
	fresh, complementary, sodium := h.jars["fresh-animations"], h.jars["complementary"], h.jars["sodium"]
	file := func(dir string, jar fakeJar, name string) mrpack.File {
		return mrpack.File{Path: dir + "/" + name, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: mrpack.Env("client"), Downloads: []string{h.server.URL + "/cdn/" + jar.filename}, FileSize: int64(len(jar.data))}
	}
	index := mrpack.Index{
		FormatVersion: 1, Game: "minecraft", VersionID: "1.0", Name: "Packs",
		Files: []mrpack.File{
			file("resourcepacks", fresh, "Fresh Animations.zip"),
			file("shaderpacks", complementary, complementary.filename),
			file("resourcepacks", sodium, "sodium-datapack.zip"),
			file("datapacks", sodium, "sodium-datapack.zip"),
		},
		Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	}
	archive := filepath.Join(t.TempDir(), "packs.mrpack")
	writeMrpack(t, archive, index, nil)

	dir := filepath.Join(t.TempDir(), "packs")
	h.dir = filepath.Dir(dir)
	var env struct {
		Data importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", dir, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data.Mods
	wantLocked := []resolve.LockedFile{{ID: "complementary-reimagined", Type: "shader", Provider: "modrinth"}, {ID: "fresh-animations", Type: "resourcepack", Provider: "modrinth"}}
	if !slices.Equal(res.Locked, wantLocked) || strings.Join(res.Unmanaged, ",") != "client-overrides/datapacks/sodium-datapack.zip,client-overrides/resourcepacks/sodium-datapack.zip" {
		t.Fatalf("import: %+v", res)
	}
	m, l := readProject(t, dir)
	if got := m.Requires["fresh-animations"]; got.Type != manifest.TypeResourcePack || got.Filename != "Fresh Animations.zip" || got.Project != "" || got.Pin != "Vb7Kq2Xn" {
		t.Fatalf("fresh-animations entry: %+v", got)
	}
	if got := m.Requires["complementary-reimagined"]; got.Type != manifest.TypeShader || got.Filename != complementary.filename || got.Pin != "pcrMhvuU" {
		t.Fatalf("complementary entry: %+v", got)
	}
	if l.ResourcePacks["fresh-animations"].VersionNumber != "1.9.4" || l.ResourcePacks["fresh-animations"].Filename != "Fresh Animations.zip" || l.Shaders["complementary-reimagined"].VersionNumber != "r5.5.1" {
		t.Fatalf("lock: %+v %+v", l.ResourcePacks, l.Shaders)
	}
	if len(m.Mods()) != 0 || len(l.Mods) != 0 {
		t.Fatalf("mods: %+v", l.Mods)
	}
	if _, err := os.Stat(filepath.Join(dir, "client-overrides/resourcepacks/Fresh Animations.zip")); !os.IsNotExist(err) {
		t.Fatalf("locked pack vendored as an override: %v", err)
	}

	h.dir = dir
	h.mustRun(t, "install")
	for _, rel := range []string{"build/client/resourcepacks/Fresh Animations.zip", "build/client/shaderpacks/" + complementary.filename} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImportMrpackMatchesCurseForge(t *testing.T) {
	h := newHarness(t)
	jei, nodist, iris := h.jars["jei"], h.jars["nodist"], h.jars["irisshaders"]
	file := func(jar fakeJar) mrpack.File {
		return mrpack.File{Path: "mods/" + jar.filename, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: mrpack.Env("both"), Downloads: []string{h.server.URL + "/cdn/" + jar.filename}, FileSize: int64(len(jar.data))}
	}
	index := mrpack.Index{
		FormatVersion: 1, Game: "minecraft", VersionID: "1.0", Name: "Mixed",
		Files:        []mrpack.File{file(jei), file(nodist), file(h.jars["sodium"])},
		Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	}
	archive := filepath.Join(t.TempDir(), "mixed.mrpack")
	writeMrpack(t, archive, index, map[string][]byte{
		"client-overrides/mods/" + iris.filename: iris.data,
		"overrides/mods/unknown-1.0.jar":         []byte("not on any provider"),
	})
	importMixed := func(t *testing.T) (importResult, []string, string) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "mixed")
		h.dir = filepath.Dir(dir)
		var env struct {
			Warnings []string     `json:"warnings"`
			Data     importResult `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", dir, "--json")), &env); err != nil {
			t.Fatal(err)
		}
		return env.Data, env.Warnings, dir
	}

	h.cfMods[455508].files[0].truncated = true
	h.cfHits = 0
	res, warnings, dir := importMixed(t)
	if h.modrinthBatches != 2 || h.cfHits != 3 {
		t.Fatalf("requests: %d to Modrinth, %d to CurseForge", h.modrinthBatches, h.cfHits)
	}
	if strings.Join(res.Mods.LockedIDs(), ",") != "jei,sodium" || strings.Join(res.Mods.Unmanaged, ",") != "client-overrides/mods/"+iris.filename+",overrides/mods/"+nodist.filename+",overrides/mods/unknown-1.0.jar" {
		t.Fatalf("import: %+v", res.Mods)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0], nodist.filename) || !strings.Contains(warnings[0], "third-party downloads") ||
		!strings.Contains(warnings[1], iris.filename) || !strings.Contains(warnings[1], "download failed") {
		t.Fatalf("warnings: %v", warnings)
	}
	m, l := readProject(t, dir)
	if _, ok := l.Mods["iris"]; ok || l.Mods["jei"].Provider != "curseforge" || l.Mods["sodium"].Provider != "modrinth" {
		t.Fatalf("lock: %+v", l.Mods)
	}
	if got := m.Requires["jei"]; got.Provider != "curseforge" || got.Project != "238222" {
		t.Fatalf("jei entry: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "client-overrides/mods", iris.filename)); err != nil {
		t.Fatalf("jar whose download failed not kept as an override: %v", err)
	}

	h.cfMods[455508].files[0].truncated = false
	res, _, dir = importMixed(t)
	if _, l := readProject(t, dir); strings.Join(res.Mods.LockedIDs(), ",") != "iris,jei,sodium" || l.Mods["iris"].Side != "client" {
		t.Fatalf("import once CurseForge serves iris: %+v", res.Mods)
	}
	if _, err := os.Stat(filepath.Join(dir, "client-overrides/mods", iris.filename)); !os.IsNotExist(err) {
		t.Fatalf("locked jar kept as an override: %v", err)
	}

	h.noCurseForge = true
	res, warnings, _ = importMixed(t)
	if strings.Join(res.Mods.LockedIDs(), ",") != "sodium" || len(res.Mods.Unmanaged) != 4 {
		t.Fatalf("import without CurseForge: %+v", res.Mods)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "CurseForge") {
		t.Fatalf("warnings without CurseForge: %v", warnings)
	}
}

func TestImportMrpackKeepsAnIndexFileModrinthFailsToServe(t *testing.T) {
	h := newHarness(t)
	sodium := h.jars["sodium"]
	h.cdnDown = map[string]bool{"/cdn/" + sodium.filename: true}
	importWith := func(t *testing.T, downloads ...string) (int, string, string) {
		t.Helper()
		index := mrpack.Index{
			FormatVersion: 1, Game: "minecraft", VersionID: "1.0", Name: "Mirrored",
			Files:        []mrpack.File{{Path: "mods/" + sodium.filename, Hashes: map[string]string{"sha1": sodium.sha1, "sha512": sodium.sha512}, Env: mrpack.Env("client"), Downloads: downloads, FileSize: int64(len(sodium.data))}},
			Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
		}
		archive := filepath.Join(t.TempDir(), "mirrored.mrpack")
		writeMrpack(t, archive, index, nil)
		h.dir = t.TempDir()
		return h.run(t, "import", archive, "--dir", filepath.Join(h.dir, "mirrored"), "--json")
	}

	if code, stdout, _ := importWith(t, h.server.URL+"/cdn/"+sodium.filename); code == 0 || !strings.Contains(stdout, "mrpack-download") || !strings.Contains(stdout, "no other URL") {
		t.Fatalf("exit %d: %s", code, stdout)
	}

	code, stdout, stderr := importWith(t, h.server.URL+"/cdn/"+sodium.filename, h.server.URL+"/cdn/mirror/"+sodium.filename)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	var env struct {
		Warnings []string     `json:"warnings"`
		Data     importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Mods.LockedIDs()) != 0 || strings.Join(env.Data.Mods.Unmanaged, ",") != "client-overrides/mods/"+sodium.filename {
		t.Fatalf("import: %+v", env.Data.Mods)
	}
	if len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "Modrinth's download failed") {
		t.Fatalf("warnings: %v", env.Warnings)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "mirrored/client-overrides/mods", sodium.filename)); err != nil {
		t.Fatalf("not kept as an override: %v", err)
	}
}

func TestImportMrpackTakesAModsSideFromThePackOnlyWhereItAddsABuiltSide(t *testing.T) {
	h := newHarness(t)
	h.jars["config-manager"] = makeJar(t, "config_manager", "config_manager-1.0.0.jar", "server")
	h.cfMods[800000] = &cfMod{id: 800000, slug: "config-manager", files: []cfFile{{id: 5600001, jar: h.jars["config-manager"], date: "2026-09-01T00:00:00Z", channel: 1}}}
	h.jars["server-tweaks"] = makeJar(t, "server_tweaks", "server_tweaks-1.0.0.jar", "server")
	h.cfMods[800001] = &cfMod{id: 800001, slug: "server-tweaks", files: []cfFile{{id: 5600002, jar: h.jars["server-tweaks"], date: "2026-09-01T00:00:00Z", channel: 1}}}
	file := func(jar fakeJar, env map[string]string) mrpack.File {
		return mrpack.File{Path: "mods/" + jar.filename, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: env, Downloads: []string{h.server.URL + "/cdn/" + jar.filename}, FileSize: int64(len(jar.data))}
	}
	index := mrpack.Index{
		FormatVersion: 1, Game: "minecraft", VersionID: "1.0", Name: "Singleplayer",
		Files: []mrpack.File{
			file(h.jars["config-manager"], mrpack.Env("both")),
			file(h.jars["server-tweaks"], mrpack.Env("client")),
			file(h.jars["sodium"], mrpack.Env("both")),
			file(h.jars["jei"], mrpack.Env("client")),
			file(h.jars["fabric-api"], nil),
		},
		Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	}
	archive := filepath.Join(t.TempDir(), "singleplayer.mrpack")
	writeMrpack(t, archive, index, nil)
	dir := filepath.Join(t.TempDir(), "singleplayer")
	h.dir = filepath.Dir(dir)
	var env struct {
		Warnings []string     `json:"warnings"`
		Data     importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", dir, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if want := []resolve.SideChoice{{ID: "config_manager", Pack: "both", Provider: "server"}, {ID: "server_tweaks", Pack: "both", Provider: "server"}}; !slices.Equal(env.Data.Mods.Sides, want) {
		t.Fatalf("sides: %+v", env.Data.Mods.Sides)
	}
	sideWarnings := slices.DeleteFunc(slices.Clone(env.Warnings), func(w string) bool { return !strings.Contains(w, "take their side") })
	if len(sideWarnings) != 1 || !strings.Contains(sideWarnings[0], "2 mod(s)") || !strings.Contains(sideWarnings[0], "config_manager (server → both), server_tweaks (server → both)") {
		t.Fatalf("warnings: %v", env.Warnings)
	}
	m, l := readProject(t, dir)
	for _, id := range []string{"config_manager", "server_tweaks"} {
		if l.Mods[id].Side != "both" || m.Requires[id].Side != "both" {
			t.Errorf("%s: lock %+v, manifest %+v", id, l.Mods[id], m.Requires[id])
		}
	}
	for id, side := range map[string]string{"sodium": "client", "jei": "both", "fabric-api": "both"} {
		if l.Mods[id].Side != side || m.Requires[id].Side != "" {
			t.Errorf("%s: lock %+v, manifest %+v", id, l.Mods[id], m.Requires[id])
		}
	}
}

func TestImportMrpackRecordsAnOverrideLayersSideQuietly(t *testing.T) {
	h := newHarness(t)
	iris := h.jars["irisshaders"]
	index := mrpack.Index{FormatVersion: 1, Game: "minecraft", VersionID: "1.0", Name: "Server", Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"}}
	archive := filepath.Join(t.TempDir(), "server.mrpack")
	writeMrpack(t, archive, index, map[string][]byte{"server-overrides/mods/" + iris.filename: iris.data})
	dir := filepath.Join(t.TempDir(), "server")
	h.dir = filepath.Dir(dir)
	var env struct {
		Warnings []string     `json:"warnings"`
		Data     importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", dir, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Mods.Sides) != 0 || slices.ContainsFunc(env.Warnings, func(w string) bool { return strings.Contains(w, "take their side") }) {
		t.Fatalf("sides %+v, warnings %v", env.Data.Mods.Sides, env.Warnings)
	}
	m, l := readProject(t, dir)
	if l.Mods["iris"].Side != "server" || m.Requires["iris"].Side != "server" {
		t.Fatalf("iris: lock %+v, manifest %+v", l.Mods["iris"], m.Requires["iris"])
	}
}

func writeEmptyMrpack(t *testing.T) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "empty.mrpack")
	writeMrpack(t, archive, mrpack.Index{FormatVersion: 1, Game: "minecraft", VersionID: "1.0", Name: "Empty Pack", Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"}}, nil)
	return archive
}

func TestImportCreatesTheProjectInTheCurrentFolder(t *testing.T) {
	h := newHarness(t)
	h.dir = ""
	archive := writeEmptyMrpack(t)
	here := t.TempDir()
	t.Chdir(here)
	stdout := h.mustRun(t, "import", archive)
	if _, err := os.Stat(filepath.Join(here, manifest.FileName)); err != nil {
		t.Fatalf("not imported here: %v", err)
	}
	if strings.Contains(stdout, "cd ") || !strings.Contains(stdout, "shulker install") {
		t.Fatalf("nudge: %s", stdout)
	}
	h.mustRun(t, "-C", "new", "import", archive)
	if _, err := os.Stat(filepath.Join(here, "new", manifest.FileName)); err != nil {
		t.Fatalf("not imported into new: %v", err)
	}
	if code, stdout, _ := h.run(t, "-C", "other", "import", archive, "extra", "--json"); code != out.ExitUsage || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("a second argument: exit %d: %s", code, stdout)
	}
}

func TestImportDetectsACurseForgePack(t *testing.T) {
	h := newHarness(t)
	h.dir = ""
	archive := filepath.Join(t.TempDir(), "craft.zip")
	writeCurseForgeZip(t, archive, importedCurseForgePack(), map[string][]byte{})
	here := t.TempDir()
	t.Chdir(here)
	h.mustRun(t, "import", archive, "--type", "curseforge")
	if _, err := os.Stat(filepath.Join(here, manifest.FileName)); err != nil {
		t.Fatalf("not imported here: %v", err)
	}
}

func TestImportTypeRefusesAMismatch(t *testing.T) {
	h := newHarness(t)
	archive := writeEmptyMrpack(t)
	cf := filepath.Join(t.TempDir(), "craft.zip")
	writeCurseForgeZip(t, cf, importedCurseForgePack(), map[string][]byte{})
	for _, args := range [][]string{{archive, "--type", "curseforge"}, {cf, "--type", "mrpack"}, {archive, "--type", "shader"}} {
		code, stdout, _ := h.run(t, append([]string{"--json", "-C", filepath.Join(t.TempDir(), "p"), "import"}, args...)...)
		if code != out.ExitUsage || failureCode(t, stdout).Code != "usage" {
			t.Fatalf("%v: exit %d: %s", args, code, stdout)
		}
	}
}

func TestImportSideNarrowsANewProject(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(t.TempDir(), "sided.mrpack")
	writeMrpack(t, archive, mrpack.Index{FormatVersion: 1, Game: "minecraft", VersionID: "1.0", Name: "Sided", Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"}}, map[string][]byte{
		"overrides/config/both.txt":          []byte("both"),
		"server-overrides/config/server.txt": []byte("server"),
		"client-overrides/config/client.txt": []byte("client"),
	})
	dir := filepath.Join(t.TempDir(), "client-only")
	var env struct {
		Data importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "-C", dir, "import", archive, "--side", "client", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(env.Data.Sides, []string{"client"}) {
		t.Fatalf("sides: %v", env.Data.Sides)
	}
	if !slices.Equal(env.Data.LeftOut, []string{"server-overrides/config/server.txt"}) {
		t.Fatalf("left out: %v", env.Data.LeftOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "server-overrides")); !os.IsNotExist(err) {
		t.Fatalf("server overrides written: %v", err)
	}
	m, err := manifest.Load(filepath.Join(dir, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if m.Server != nil {
		t.Fatalf("server block kept: %+v", m.Server)
	}
	if code, stdout, _ := h.run(t, "--json", "-C", filepath.Join(t.TempDir(), "x"), "import", archive, "--side", "both"); code != out.ExitUsage || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--side both: exit %d: %s", code, stdout)
	}
}
