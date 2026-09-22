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
	if raw.ResourcePacks["faithful"]["file"] != "files/faithful.zip" || raw.Shaders["bsl"]["loader"] != "iris" {
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
