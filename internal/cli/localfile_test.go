package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

// localFiles is a project requiring a hand-written local mod, resource pack and shader, with the mod
// depending on fabric-api, which only a provider has.
func localFiles(t *testing.T) (*harness, fakeJar) {
	t.Helper()
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	jar := makeJarWith(t, "private-mod", "private-mod-1.4.jar", "client", `"depends":{"fabricloader":">=0.17","fabric-api":"*"}`)
	files := map[string][]byte{
		"files/private-mod-1.4.jar": jar.data,
		"files/faithful.zip":        makeJarFile(t, "faithful", "faithful.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"faithful"}}`).data,
		"files/bsl.zip":             makeJarFile(t, "bsl", "bsl.zip", "shaders/gbuffers_basic.vsh", "// bsl").data,
	}
	for rel, data := range files {
		writeProjectFile(t, h, rel, data)
	}
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{
			"private-mod": map[string]any{"file": "files/private-mod-1.4.jar"},
			"faithful":    map[string]any{"type": "resourcepack", "file": "files/faithful.zip"},
			"bsl":         map[string]any{"type": "shader", "file": "files/bsl.zip"},
		}
	})
	return h, jar
}

func writeProjectFile(t *testing.T, h *harness, rel string, data []byte) {
	t.Helper()
	path := filepath.Join(h.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalFilesLockAndBuild(t *testing.T) {
	h, jar := localFiles(t)
	h.mustRun(t, "lock")

	var raw struct {
		Mods          map[string]map[string]any `json:"mods"`
		ResourcePacks map[string]map[string]any `json:"resourcepacks"`
		Shaders       map[string]map[string]any `json:"shaders"`
	}
	h.readJSON(t, "shulker.lock", &raw)
	mod := raw.Mods["private-mod"]
	if mod["file"] != "files/private-mod-1.4.jar" || mod["filename"] != "private-mod-1.4.jar" || mod["sha512"] != jar.sha512 || mod["size"] != float64(len(jar.data)) || mod["side"] != "client" {
		t.Fatalf("local mod entry: %v", mod)
	}
	for _, entry := range []map[string]any{mod, raw.ResourcePacks["faithful"], raw.Shaders["bsl"]} {
		for _, key := range []string{"provider", "project", "version", "versionNumber", "url", "page", "channel"} {
			if _, ok := entry[key]; ok {
				t.Errorf("a local file entry carries %s: %v", key, entry)
			}
		}
	}
	if raw.ResourcePacks["faithful"]["file"] != "files/faithful.zip" || raw.Shaders["bsl"]["loaders"] != nil {
		t.Fatalf("local packs: %v %v", raw.ResourcePacks, raw.Shaders)
	}
	if dep := raw.Mods["fabric-api"]; dep["provider"] != "modrinth" || dep["requiredBy"].([]any)[0] != "private-mod" {
		t.Fatalf("the local jar's dependency resolves from a provider: %v", dep)
	}

	_, stderr := h.mustRunStderr(t, "install")
	if strings.Contains(stderr, "out of date") {
		t.Fatalf("install after lock: %s", stderr)
	}
	if got := readBuilt(t, h, "mods/private-mod-1.4.jar"); got != string(jar.data) {
		t.Fatal("the local mod is placed under its own file name")
	}
	for _, rel := range []string{"resourcepacks/faithful.zip", "shaderpacks/bsl.zip", "mods/" + h.jars["fabric-api"].filename} {
		if readBuilt(t, h, rel) == "" {
			t.Errorf("%s is not placed", rel)
		}
	}
}

func TestLocalFileChangedBytesAreStaleness(t *testing.T) {
	h, _ := localFiles(t)
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	changed := makeJarFile(t, "faithful", "faithful.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"faithful 2"}}`)
	writeProjectFile(t, h, "files/faithful.zip", changed.data)

	code, stdout, _ := h.run(t, "build", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code != 0 || !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "faithful: the file's bytes changed") || !strings.Contains(env.Warnings[0], "run `shulker lock`") {
		t.Fatalf("code=%d env=%+v", code, env)
	}
	code, stdout, _ = h.run(t, "export", "mrpack", "--version", "1.0.0", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "lock-stale" {
		t.Fatalf("export must refuse: code=%d env=%+v", code, env)
	}

	h.mustRun(t, "lock")
	if l := h.readLock(t); l.ResourcePacks["faithful"].Sha512 != changed.sha512 || l.ResourcePacks["faithful"].Size != int64(len(changed.data)) {
		t.Fatalf("lock adopts the new bytes: %+v", l.ResourcePacks["faithful"])
	}
	env = out.Envelope{}
	if err := json.Unmarshal([]byte(h.mustRun(t, "install", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.LockStale || len(env.Warnings) != 0 {
		t.Fatalf("install after lock: %+v", env)
	}
	if got := readBuilt(t, h, "resourcepacks/faithful.zip"); got != string(changed.data) {
		t.Fatal("the build places the new bytes")
	}
}

func TestLocalFileGoneBuildsFromCache(t *testing.T) {
	h, jar := localFiles(t)
	h.mustRun(t, "lock")
	if err := os.Remove(filepath.Join(h.dir, "files", "private-mod-1.4.jar")); err != nil {
		t.Fatal(err)
	}

	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "install", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "private-mod: files/private-mod-1.4.jar is gone") {
		t.Fatalf("a gone file warns once and builds: %+v", env)
	}
	if got := readBuilt(t, h, "mods/private-mod-1.4.jar"); got != string(jar.data) {
		t.Fatal("the gone file is placed from the cache")
	}
	env = out.Envelope{}
	if err := json.Unmarshal([]byte(h.mustRun(t, "lock", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if l := h.readLock(t); len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "files/private-mod-1.4.jar is gone") || l.Mods["private-mod"].Sha512 != jar.sha512 {
		t.Fatalf("lock keeps the entry the cache serves, with one warning: %+v %+v", env, l.Mods["private-mod"])
	}

	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "install", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || len(env.Warnings) != 0 || env.Error == nil || env.Error.Code != "missing-files" || len(env.Error.Items) != 1 || !strings.Contains(env.Error.Items[0], "private-mod") {
		t.Fatalf("install with neither file nor cache: code=%d env=%+v", code, env)
	}
	code, stdout, _ = h.run(t, "lock", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "local-file-missing" {
		t.Fatalf("lock with neither file nor cache: code=%d env=%+v", code, env)
	}
}

func TestLocalModKeyedAndChanged(t *testing.T) {
	h, _ := localFiles(t)
	h.editManifest(t, func(m map[string]any) {
		requires := m["requires"].(map[string]any)
		delete(requires, "private-mod")
		requires["mine"] = map[string]any{"file": "files/private-mod-1.4.jar", "side": "both"}
	})
	h.mustRun(t, "lock")
	if l := h.readLock(t); l.Mods["mine"].ModID != "private-mod" || l.Mods["mine"].Side != "both" || l.Mods["fabric-api"].RequiredBy[0] != "mine" {
		t.Fatalf("a local jar under another key: %+v", l.Mods)
	}

	changed := makeJarVersion(t, "private-mod", "private-mod-1.4.jar", "client", "1.5.0", `"depends":{"fabricloader":">=0.17","fabric-api":"*"}`)
	writeProjectFile(t, h, "files/private-mod-1.4.jar", changed.data)
	code, stdout, _ := h.run(t, "build", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code != 0 || !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "mine: the file's bytes changed") {
		t.Fatalf("code=%d env=%+v", code, env)
	}
	h.mustRun(t, "lock")
	if l := h.readLock(t); l.Mods["mine"].Sha512 != changed.sha512 || l.Mods["mine"].ModID != "private-mod" {
		t.Fatalf("lock adopts the new jar: %+v", l.Mods["mine"])
	}
	h.mustRun(t, "install")
	if got := readBuilt(t, h, "mods/private-mod-1.4.jar"); got != string(changed.data) {
		t.Fatal("the build places the new jar")
	}
}

func TestExportMrpackBundlesLocalFiles(t *testing.T) {
	h, jar := localFiles(t)
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	h.allowMrpackHost(t)

	code, stdout, _ := h.run(t, "export", "mrpack", "--version", "1.0.0", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "mrpack-host-not-allowed" || len(env.Error.Items) != 3 {
		t.Fatalf("export without --bundle: code=%d env=%+v", code, env)
	}
	for _, item := range env.Error.Items {
		if !strings.HasSuffix(item, " (local file)") {
			t.Errorf("blocked item %q is not labelled a local file", item)
		}
	}

	_, stderr := h.mustRunStderr(t, "export", "mrpack", "--version", "1.0.0", "--bundle")
	for _, key := range []string{"private-mod", "faithful", "bsl"} {
		if !strings.Contains(stderr, "bundled "+key+" from local file into the archive; recipients receive the file itself") {
			t.Errorf("no bundle warning for %s: %s", key, stderr)
		}
	}
	index, entries := readMrpack(t, filepath.Join(h.dir, "build", filepath.Base(h.dir)+"-1.0.0.mrpack"))
	if len(index.Files) != 1 || index.Files[0].Path != "mods/"+h.jars["fabric-api"].filename {
		t.Fatalf("only the provider mod ships by download: %+v", index.Files)
	}
	if entries["overrides/mods/private-mod-1.4.jar"] != string(jar.data) || entries["overrides/resourcepacks/faithful.zip"] == "" || entries["overrides/shaderpacks/bsl.zip"] == "" {
		t.Fatalf("bundled entries: %v", keys(entries))
	}
}

func TestImportMrpackTakesBundledLocalFilesAsItsOwn(t *testing.T) {
	h, jar := localFiles(t)
	h.editManifest(t, func(m map[string]any) {
		requires := m["requires"].(map[string]any)
		requires["faithful"] = map[string]any{"type": "resourcepack", "file": "packs/faithful.zip", "side": "client"}
	})
	if err := os.MkdirAll(filepath.Join(h.dir, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(h.dir, "files", "faithful.zip"), filepath.Join(h.dir, "packs", "faithful.zip")); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "lock")
	h.allowMrpackHost(t)
	h.mustRun(t, "export", "mrpack", "--version", "1.0.0", "--bundle")

	dir := filepath.Join(t.TempDir(), "imported")
	h.mustRun(t, "import", "mrpack", filepath.Join(h.dir, "build", filepath.Base(h.dir)+"-1.0.0.mrpack"), "--dir", dir)
	m, l := readProject(t, dir)
	for key, want := range map[string]string{"private-mod": "files/private-mod-1.4.jar", "faithful": "files/faithful.zip", "bsl": "files/bsl.zip"} {
		if m.Requires[key].File != want {
			t.Errorf("%s requires file %q, want %q", key, m.Requires[key].File, want)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("%s is not in the new project: %v", key, err)
		}
	}
	if m.Requires["faithful"].Side != "client" || l.Mods["private-mod"].File != "files/private-mod-1.4.jar" || l.ResourcePacks["faithful"].File != "files/faithful.zip" || l.Shaders["bsl"].File != "files/bsl.zip" {
		t.Fatalf("lock entries: %+v %+v %+v", l.Mods["private-mod"], l.ResourcePacks["faithful"], l.Shaders["bsl"])
	}

	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	h.dir = dir
	h.mustRun(t, "install")
	if got := readBuilt(t, h, "mods/private-mod-1.4.jar"); got != string(jar.data) {
		t.Fatal("the imported project builds its local jar with an empty cache")
	}
}

func TestImportMrpackRestoresAPackFolder(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	writeFolder(t, filepath.Join(h.dir, "packs", "Helper"), helperFiles)
	h.mustRun(t, "resourcepack", "add", "packs/Helper")
	_, sha := folderZip(t, h, "packs/Helper")
	h.allowMrpackHost(t)
	h.mustRun(t, "export", "mrpack", "--version", "1.0.0", "--bundle")

	dir := filepath.Join(t.TempDir(), "imported")
	h.mustRun(t, "import", "mrpack", filepath.Join(h.dir, "build", filepath.Base(h.dir)+"-1.0.0.mrpack"), "--dir", dir)
	h.dir = dir
	m, l := readProject(t, dir)
	if m.Requires["helper"].File != "files/Helper" || l.ResourcePacks["helper"].File != "files/Helper" {
		t.Fatalf("the pack comes back as a folder: %+v %+v", m.Requires["helper"], l.ResourcePacks["helper"])
	}
	for rel, body := range helperFiles {
		if got := readProjectFile(t, h, "files/Helper/"+rel); got != body {
			t.Errorf("files/Helper/%s = %q, want %q", rel, got, body)
		}
	}
	if _, got := folderZip(t, h, "files/Helper"); got != sha || l.ResourcePacks["helper"].Sha512 != sha {
		t.Fatalf("the restored folder zips to the locked bytes: %s, lock %s, want %s", got, l.ResourcePacks["helper"].Sha512, sha)
	}
	h.mustRun(t, "resourcepack", "add", "files/Helper")
}

func TestImportMrpackRefusesTwoLocalFilesOfOneName(t *testing.T) {
	h, _ := localFiles(t)
	pack := makeJarFile(t, "other", "faithful.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"other"}}`).data
	writeProjectFile(t, h, "packs/faithful.zip", pack)
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["other"] = map[string]any{"type": "resourcepack", "file": "packs/faithful.zip"}
	})
	h.mustRun(t, "lock")
	h.allowMrpackHost(t)
	h.mustRun(t, "export", "mrpack", "--version", "1.0.0", "--bundle")

	dir := filepath.Join(t.TempDir(), "imported")
	code, stdout, _ := h.run(t, "import", "mrpack", filepath.Join(h.dir, "build", filepath.Base(h.dir)+"-1.0.0.mrpack"), "--dir", dir, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "file-taken" {
		t.Fatalf("two local files named faithful.zip: code=%d %+v", code, e)
	}
	if _, err := os.Stat(filepath.Join(dir, "shulker.json")); err == nil {
		t.Fatal("a refused import writes no project")
	}
}

func TestExportCurseForgeMatchesLocalCopy(t *testing.T) {
	h, _ := localFiles(t)
	writeProjectFile(t, h, "files/jei.jar", h.jars["jei"].data)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{
			"private-mod": map[string]any{"file": "files/private-mod-1.4.jar"},
			"jei-copy":    map[string]any{"file": "files/jei.jar"},
		}
	})
	h.mustRun(t, "lock")
	h.mustRun(t, "install")

	code, stdout, _ := h.run(t, "export", "curseforge", "--version", "1.0.0", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "curseforge-not-found" || len(e.Items) != 1 || e.Items[0] != "private-mod (local file)" {
		t.Fatalf("only the unpublished file is unknown: code=%d %+v", code, e)
	}

	h.mustRun(t, "export", "curseforge", "--version", "1.0.0", "--bundle")
	entries := readArchive(t, filepath.Join(h.dir, "build", filepath.Base(h.dir)+"-1.0.0.zip"))
	var pack curseForgePack
	if err := json.Unmarshal([]byte(entries["manifest.json"]), &pack); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range pack.Files {
		if f.ProjectID == 238222 && f.FileID == 5000001 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the local copy of jei exports by file id: %+v", pack.Files)
	}
	if _, ok := entries["overrides/mods/jei-26.2-fabric-1.0.0.jar"]; ok {
		t.Fatal("the local copy of jei was bundled")
	}
	if _, ok := entries["overrides/mods/private-mod-1.4.jar"]; !ok {
		t.Fatalf("the unpublished jar is bundled: %v", keys(entries))
	}
}

func TestListShowsLocalFiles(t *testing.T) {
	h, _ := localFiles(t)
	h.mustRun(t, "lock")

	stdout := h.mustRun(t, "list")
	for _, want := range []string{"files/private-mod-1.4.jar", "files/faithful.zip", "files/bsl.zip", "local file"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("list lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "not locked") {
		t.Fatalf("a local file reads as not locked:\n%s", stdout)
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(dataJSON(t, h.mustRun(t, "list", "--json"))), &entries); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"private-mod": "files/private-mod-1.4.jar", "faithful": "files/faithful.zip", "bsl": "files/bsl.zip"}
	for _, e := range entries {
		key := e["key"].(string)
		want, local := files[key]
		if !local {
			continue
		}
		_, hasVersion := e["version"]
		_, hasProvider := e["provider"]
		if e["file"] != want || hasVersion || hasProvider {
			t.Errorf("list --json entry for %s: %v", key, e)
		}
		delete(files, key)
	}
	if len(files) != 0 {
		t.Fatalf("list --json lacks %v", files)
	}
}

func TestOutdatedAndUpdateSkipLocalFiles(t *testing.T) {
	h, _ := localFiles(t)
	h.mustRun(t, "lock")

	stdout := h.mustRun(t, "outdated")
	if strings.Contains(stdout, "private-mod") || strings.Contains(stdout, "faithful") {
		t.Fatalf("a bare outdated names a local file:\n%s", stdout)
	}
	for _, args := range [][]string{{"outdated", "private-mod"}, {"outdated", "faithful"}, {"update", "private-mod"}, {"update", "bsl"}} {
		stdout := h.mustRun(t, args...)
		if !strings.Contains(stdout, args[1]+" is a local file; nothing to check") || strings.Contains(stdout, "up to date") {
			t.Errorf("%v:\n%s", args, stdout)
		}
	}
	stdout = h.mustRun(t, "update")
	if strings.Contains(stdout, "local file") || strings.Contains(stdout, "private-mod") {
		t.Fatalf("a bare update names a local file:\n%s", stdout)
	}
}

func TestPinRefusesLocalFiles(t *testing.T) {
	h, _ := localFiles(t)
	h.mustRun(t, "lock")

	for _, args := range [][]string{{"pin", "private-mod"}, {"pin", "private-mod", "abc"}, {"unpin", "private-mod"}, {"pin", "faithful"}, {"unpin", "bsl"}} {
		code, stdout, _ := h.run(t, append(args, "--json")...)
		var env out.Envelope
		_ = json.Unmarshal([]byte(stdout), &env)
		if code == 0 || env.Error == nil || env.Error.Code != "local-file" || env.Error.Message != args[1]+" is a local file; there is no provider version to pin" {
			t.Errorf("%v: code=%d env=%+v", args, code, env)
		}
	}
}
