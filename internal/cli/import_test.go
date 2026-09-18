package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
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
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", "mrpack", archive, "--dir", dir, "--json")), &env); err != nil {
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

	code, stdout, _ := h.run(t, "import", "mrpack", archive, "--dir", dir, "--json")
	if code == 0 || failureCode(t, stdout).Code != "manifest-exists" {
		t.Fatalf("import over an existing project: exit %d %s", code, stdout)
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
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", "mrpack", archive, "--dir", dir, "--ignore-shulker", "--json")), &env); err != nil {
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
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", "mrpack", archive, "--dir", dir, "--json")), &env); err != nil {
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
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", "mrpack", tampered, "--dir", dir, "--json")), &env); err != nil {
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
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", "mrpack", archive, "--dir", filepath.Join(parent, "someone-s-pack"), "--json")), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data
	dir := filepath.Join(parent, "someone-s-pack")
	if res.Marker || res.Name != "someone-s-pack" || res.Version != "2.0" || strings.Join(res.Sides, ",") != "client,server" {
		t.Fatalf("result: %+v", res)
	}
	if strings.Join(res.Mods.Locked, ",") != "fabric-api,sodium" || strings.Join(res.Mods.Unmanaged, ",") != "overrides/mods/local-1.0.jar,server-overrides/mods/extra-1.0.jar" {
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
