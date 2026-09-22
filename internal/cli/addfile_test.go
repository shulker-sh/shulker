package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
)

func writeOutside(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readProjectFile(t *testing.T, h *harness, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAddLocalJarFromOutsideCopiesIntoFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	jar := makeJarWith(t, "private-mod", "private-mod-1.4.jar", "client", `"depends":{"fabricloader":">=0.17","fabric-api":"*"}`)
	h.mustRun(t, "add", writeOutside(t, jar.filename, jar.data))

	if got := h.readManifest(t).Requires["private-mod"]; !reflect.DeepEqual(got, manifest.Require{File: "files/private-mod-1.4.jar"}) {
		t.Fatalf("manifest entry: %+v", got)
	}
	if readProjectFile(t, h, "files/private-mod-1.4.jar") != string(jar.data) {
		t.Fatal("the jar is copied into files/")
	}
	l := h.readLock(t)
	if m := l.Mods["private-mod"]; m.File != "files/private-mod-1.4.jar" || m.Sha512 != jar.sha512 || m.Side != "client" {
		t.Fatalf("locked in the same run: %+v", m)
	}
	if dep := l.Mods["fabric-api"]; dep.Provider != "modrinth" || dep.RequiredBy[0] != "private-mod" {
		t.Fatalf("the jar's dependency resolves: %+v", dep)
	}
}

func TestAddLocalPacksInsideAndOutside(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	faithful := makeJarFile(t, "faithful", "faithful.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"faithful"}}`)
	writeProjectFile(t, h, "packs/faithful.zip", faithful.data)
	h.mustRun(t, "resourcepack", "add", "packs/faithful.zip")
	bsl := makeJarFile(t, "bsl", "BSL Shaders v8.zip", "shaders/gbuffers_basic.vsh", "// bsl")
	h.mustRun(t, "shader", "add", writeOutside(t, bsl.filename, bsl.data))
	sniffed := makeJarFile(t, "stay", "stay-true.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"stay true"}}`)
	h.mustRun(t, "add", writeOutside(t, sniffed.filename, sniffed.data))

	m := h.readManifest(t)
	if got := m.Requires["faithful"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeResourcePack, File: "packs/faithful.zip"}) {
		t.Fatalf("an inside path is referenced where it lies: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "files", "faithful.zip")); !os.IsNotExist(err) {
		t.Fatalf("an inside path is not copied: %v", err)
	}
	if got := m.Requires["bsl-shaders-v8"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeShader, File: "files/BSL Shaders v8.zip"}) {
		t.Fatalf("an outside pack is keyed by its stem: %+v", m.Requires)
	}
	if got := m.Requires["stay-true"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeResourcePack, File: "files/stay-true.zip"}) {
		t.Fatalf("a bare add reads a zip's kind from what it holds: %+v", got)
	}
	l := h.readLock(t)
	if l.ResourcePacks["faithful"].Sha512 != faithful.sha512 || l.Shaders["bsl-shaders-v8"].Sha512 != bsl.sha512 || l.ResourcePacks["stay-true"].Sha512 != sniffed.sha512 {
		t.Fatalf("packs are locked: %+v %+v", l.ResourcePacks, l.Shaders)
	}
}

func TestAddLocalFileKeys(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	jar := makeJar(t, "private-mod", "private-mod-1.4.jar", "client")
	path := writeOutside(t, jar.filename, jar.data)
	h.mustRun(t, "add", path, "--as", "mine")
	if l := h.readLock(t); l.Mods["mine"].ModID != "private-mod" {
		t.Fatalf("--as keys the entry: %+v", l.Mods)
	}

	pack := makeJarFile(t, "mine", "mine.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"mine"}}`)
	code, stdout, _ := h.run(t, "--json", "resourcepack", "add", writeOutside(t, pack.filename, pack.data))
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "requires-taken" || !strings.Contains(e.Help, "--as <key>") {
		t.Fatalf("a taken key: code=%d %+v", code, e)
	}
	if got := h.readManifest(t).Requires["mine"]; !reflect.DeepEqual(got, manifest.Require{File: "files/private-mod-1.4.jar"}) {
		t.Fatalf("the refused add leaves the entry alone: %+v", got)
	}
}

func TestAddLocalFileFromADownloadedFolderIsCopied(t *testing.T) {
	h := newInPlace(t)
	jar := makeJar(t, "private-mod", "private-mod-1.4.jar", "client")
	writeProjectFile(t, h, "mods/private-mod-1.4.jar", jar.data)
	h.mustRun(t, "add", "mods/private-mod-1.4.jar")
	if got := h.readManifest(t).Requires["private-mod"].File; got != "files/private-mod-1.4.jar" {
		t.Fatalf("a jar in an in-place build folder is copied into files/: %q", got)
	}
	pack := makeJarFile(t, "faithful", "faithful.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"faithful"}}`)
	writeProjectFile(t, h, "downloads/faithful.zip", pack.data)
	h.mustRun(t, "resourcepack", "add", "downloads/faithful.zip")
	if got := h.readManifest(t).Requires["faithful"].File; got != "files/faithful.zip" {
		t.Fatalf("a pack in downloads/ is copied into files/: %q", got)
	}

	_, stderr := h.mustRunStderr(t, "install")
	if strings.Contains(stderr, "files/") {
		t.Fatalf("install leaves files/ alone: %s", stderr)
	}
	if readProjectFile(t, h, "files/private-mod-1.4.jar") != string(jar.data) {
		t.Fatal("files/ survives an in-place build")
	}
}

func TestBuildInPlaceRefusesAnOverrideIntoFiles(t *testing.T) {
	h := newInPlace(t)
	writeProjectFile(t, h, "overrides/files/x.jar", []byte("mine"))
	code, stdout, _ := h.run(t, "--json", "install")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "build-reserved" || strings.Join(e.Items, ",") != "files/x.jar" {
		t.Fatalf("files/ is reserved: code=%d %+v", code, e)
	}
}

func TestAddLocalFileRefusals(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	pack := makeJarFile(t, "faithful", "faithful.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"faithful"}}`)
	writeProjectFile(t, h, "files/faithful.zip", []byte("another"))
	unknown := makeJarFile(t, "notes", "notes.zip", "notes.txt", "hi")
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"resourcepack", "add", writeOutside(t, pack.filename, pack.data)}, "file-taken"},
		{[]string{"add", writeOutside(t, unknown.filename, unknown.data)}, "type-ambiguous"},
		{[]string{"add", writeOutside(t, "pack.mrpack", pack.data)}, "archive-not-modpack"},
		{[]string{"resourcepack", "add", writeOutside(t, "other.zip", pack.data), "--pin", "abc"}, "usage"},
		{[]string{"add", filepath.Join(t.TempDir(), "gone.jar")}, "file-not-found"},
	} {
		code, stdout, _ := h.run(t, append([]string{"--json"}, c.args...)...)
		if e := failureCode(t, stdout); code == 0 || e.Code != c.code {
			t.Errorf("%v: code=%d %+v", c.args, code, e)
		}
	}
	if readProjectFile(t, h, "files/faithful.zip") != "another" {
		t.Fatal("a different file in files/ is never replaced")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "files", "pack.mrpack")); !os.IsNotExist(err) {
		t.Fatalf("a refused archive is not copied in: %v", err)
	}
}

func TestAddLocalJarOverADependencyKeepsItsDependents(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	jar := makeJarWith(t, "private-mod", "private-mod-1.4.jar", "client", `"depends":{"fabricloader":">=0.17","fabric-api":"*"}`)
	h.mustRun(t, "add", writeOutside(t, jar.filename, jar.data))
	own := makeJar(t, "fabric-api", "fabric-api-local.jar", "*")
	h.mustRun(t, "add", writeOutside(t, own.filename, own.data))
	if m := h.readLock(t).Mods["fabric-api"]; m.File != "files/fabric-api-local.jar" || len(m.RequiredBy) != 1 || m.RequiredBy[0] != "private-mod" {
		t.Fatalf("the local jar takes over the dependency: %+v", m)
	}
}

func TestReaddingAnOutsideFileRefreshesItsCopy(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	jar := makeJar(t, "private-mod", "private-mod.jar", "client")
	path := writeOutside(t, jar.filename, jar.data)
	h.mustRun(t, "add", path, "--side", "both")
	rebuilt := makeJarVersion(t, "private-mod", "private-mod.jar", "client", "1.1.0", `"depends":{"fabricloader":">=0.17"}`)
	if err := os.WriteFile(path, rebuilt.data, 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "add", path)
	if readProjectFile(t, h, "files/private-mod.jar") != string(rebuilt.data) {
		t.Fatal("the copy takes the rebuilt bytes")
	}
	if m := h.readLock(t).Mods["private-mod"]; m.Sha512 != rebuilt.sha512 || m.Side != "both" {
		t.Fatalf("the re-add relocks and keeps the entry's side: %+v", m)
	}
}
