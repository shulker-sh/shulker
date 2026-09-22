package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func TestPullToChoosesTheOverrideFolder(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["features"] = map[string]any{
			"shaders": map[string]any{"default": true},
			"voice":   map[string]any{"default": true, "overrides": map[string]any{"client": "voice-client"}},
		}
	})
	writeOverride(t, h.dir, "overrides/config/plain.txt", "a=1\n")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "client")
	read := func(rel string) string {
		data, _ := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(rel)))
		return string(data)
	}

	writeOverride(t, buildDir, "config/plain.txt", "a=2\n")
	h.mustRun(t, "pull", "--to", "client")
	if read("client-overrides/config/plain.txt") != "a=2\n" || read("overrides/config/plain.txt") != "a=1\n" {
		t.Fatalf("--to client writes the side folder and leaves the shared one: %q %q", read("client-overrides/config/plain.txt"), read("overrides/config/plain.txt"))
	}

	writeOverride(t, buildDir, "config/plain.txt", "a=3\n")
	h.mustRun(t, "pull")
	if read("client-overrides/config/plain.txt") != "a=3\n" || read("overrides/config/plain.txt") != "a=1\n" {
		t.Fatalf("a file is updated in the folder that already holds it: %q %q", read("client-overrides/config/plain.txt"), read("overrides/config/plain.txt"))
	}

	writeOverride(t, buildDir, "config/new.txt", "new\n")
	h.mustRun(t, "pull", "config/new.txt")
	if read("overrides/config/new.txt") != "new\n" {
		t.Fatalf("a new file lands in overrides/: %q", read("overrides/config/new.txt"))
	}

	writeOverride(t, buildDir, "config/shaders.txt", "s\n")
	h.mustRun(t, "pull", "config/shaders.txt", "--to", "shaders")
	if read("shaders-overrides/config/shaders.txt") != "s\n" {
		t.Fatalf("--to feature writes the feature's default folder: %q", read("shaders-overrides/config/shaders.txt"))
	}

	writeOverride(t, buildDir, "config/voice.txt", "v\n")
	h.mustRun(t, "pull", "config/voice.txt", "--to", "voice")
	if read("voice-client/config/voice.txt") != "v\n" {
		t.Fatalf("--to feature picks the object form's path for the side pulled from: %q", read("voice-client/config/voice.txt"))
	}

	writeOverride(t, buildDir, "config/voice.txt", "v2\n")
	code, stdout, _ := h.run(t, "pull", "--to", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" || strings.Join(e.Candidates, ",") != "client,shaders,voice" {
		t.Fatalf("unknown --to: exit %d %s", code, stdout)
	}
}

func pullReport(t *testing.T, h *harness, args ...string) build.PullReport {
	t.Helper()
	stdout := h.mustRun(t, append([]string{"pull", "--json"}, args...)...)
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	var rep build.PullReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestPullAdoptsADroppedJarOrPackAsAFileEntry(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "client")
	jar := makeJarWith(t, "private-mod", "private-mod-1.4.jar", "client", `"depends":{"fabricloader":">=0.17"}`)
	pack := makeJarFile(t, "stay", "Stay True.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"stay true"}}`)
	shader := makeJarFile(t, "bsl", "bsl.zip", "shaders/gbuffers_basic.vsh", "// bsl")
	writeOverride(t, buildDir, "mods/private-mod-1.4.jar", string(jar.data))
	writeOverride(t, buildDir, "resourcepacks/Stay True.zip", string(pack.data))
	writeOverride(t, buildDir, "shaderpacks/bsl.zip", string(shader.data))
	writeOverride(t, buildDir, "resourcepacks/nested/deep.zip", string(pack.data))
	writeOverride(t, buildDir, "config/plain.txt", "a=1\n")

	rep := pullReport(t, h, "mods/private-mod-1.4.jar", "resourcepacks/Stay True.zip", "shaderpacks/bsl.zip", "resourcepacks/nested/deep.zip", "config/plain.txt")
	if want := []string{"mods/private-mod-1.4.jar -> files/private-mod-1.4.jar", "resourcepacks/Stay True.zip -> files/Stay True.zip", "shaderpacks/bsl.zip -> files/bsl.zip"}; !reflect.DeepEqual(rep.Entries, want) {
		t.Fatalf("jars and packs are adopted: %q", rep.Entries)
	}
	if want := []string{"config/plain.txt -> overrides/config/plain.txt", "resourcepacks/nested/deep.zip -> overrides/resourcepacks/nested/deep.zip"}; !reflect.DeepEqual(rep.Pulled, want) {
		t.Fatalf("everything else is overridden: %q", rep.Pulled)
	}
	m := h.readManifest(t)
	for key, want := range map[string]manifest.Require{
		"private-mod": {File: "files/private-mod-1.4.jar"},
		"stay-true":   {Type: manifest.TypeResourcePack, File: "files/Stay True.zip", Filename: "Stay True.zip"},
		"bsl":         {Type: manifest.TypeShader, File: "files/bsl.zip"},
	} {
		if got := m.Requires[key]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s is written unconditional: %+v", key, got)
		}
	}
	if readProjectFile(t, h, "files/private-mod-1.4.jar") != string(jar.data) {
		t.Fatal("the jar is copied into files/")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "overrides", "mods")); !os.IsNotExist(err) {
		t.Fatalf("an adopted jar is not also overridden: %v", err)
	}
	l := h.readLock(t)
	if mod := l.Mods["private-mod"]; mod.Sha512 != jar.sha512 || mod.Side != "client" {
		t.Fatalf("the jar is locked with the side it declares: %+v", mod)
	}
	if l.ResourcePacks["stay-true"].Sha512 != pack.sha512 || l.Shaders["bsl"].Sha512 != shader.sha512 {
		t.Fatalf("packs are locked: %+v %+v", l.ResourcePacks, l.Shaders)
	}

	other := makeJarFile(t, "stay2", "Stay+True.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"another"}}`)
	writeOverride(t, buildDir, "resourcepacks/Stay+True.zip", string(other.data))
	rep = pullReport(t, h, "resourcepacks/Stay+True.zip")
	if len(rep.Entries) != 0 || len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0], "requires already has stay-true as a resourcepack") || !strings.Contains(rep.Skipped[0], "pass `--as <key>`") {
		t.Fatalf("a taken key is skipped with the --as hint: %+v", rep)
	}
	rep = pullReport(t, h, "resourcepacks/Stay+True.zip", "--as", "stay-true-2")
	if got := h.readManifest(t).Requires["stay-true-2"]; len(rep.Entries) != 1 || got.File != "files/Stay+True.zip" || got.Filename != "Stay+True.zip" {
		t.Fatalf("--as adopts under the given key: %+v %+v", rep, h.readManifest(t).Requires)
	}
	h.mustRun(t, "install")
	if data, _ := os.ReadFile(filepath.Join(buildDir, "mods", "private-mod-1.4.jar")); string(data) != string(jar.data) {
		t.Fatal("the build lays the adopted jar where it was found")
	}

	code, stdout, _ := h.run(t, "pull", "config/plain.txt", "resourcepacks/Stay+True.zip", "--as", "x", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" {
		t.Fatalf("--as takes a single file: exit %d %s", code, stdout)
	}
}

func TestPullKeepsAnAdoptedPacksName(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "client")
	pack := makeJarFile(t, "stay", "Stay True.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"stay true"}}`)
	plain := makeJarFile(t, "plain", "plain-pack.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"plain"}}`)
	shader := makeJarFile(t, "bsl", "BSL v8.zip", "shaders/gbuffers_basic.vsh", "// bsl")
	plainShader := makeJarFile(t, "plain-shader", "plain-shader.zip", "shaders/gbuffers_basic.vsh", "// plain")
	writeOverride(t, buildDir, "resourcepacks/Stay True.zip", string(pack.data))
	writeOverride(t, buildDir, "resourcepacks/plain-pack.zip", string(plain.data))
	writeOverride(t, buildDir, "shaderpacks/BSL v8.zip", string(shader.data))
	writeOverride(t, buildDir, "shaderpacks/plain-shader.zip", string(plainShader.data))

	pullReport(t, h, "resourcepacks/Stay True.zip", "resourcepacks/plain-pack.zip", "shaderpacks/BSL v8.zip", "shaderpacks/plain-shader.zip")
	m := h.readManifest(t)
	for key, want := range map[string]string{"stay-true": "Stay True.zip", "plain-pack": "", "bsl-v8": "BSL v8.zip", "plain-shader": ""} {
		if got := m.Requires[key].Filename; got != want {
			t.Fatalf("%s gets filename %q only when its name isn't <key>.zip: %q", key, want, got)
		}
	}
	l := h.readLock(t)
	if l.ResourcePacks["stay-true"].Filename != "Stay True.zip" || l.Shaders["bsl-v8"].Filename != "BSL v8.zip" {
		t.Fatalf("the lock places the packs under their names: %+v %+v", l.ResourcePacks, l.Shaders)
	}

	h.mustRun(t, "install")
	for dir, want := range map[string][]string{"resourcepacks": {"Stay True.zip", "plain-pack.zip"}, "shaderpacks": {"BSL v8.zip", "plain-shader.zip"}} {
		entries, err := os.ReadDir(filepath.Join(buildDir, dir))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("the build leaves one copy of each pack in %s/: %q", dir, got)
		}
	}
}

func TestPullReportsWhereAnAdoptedFileLives(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "install")
	jar := makeJarWith(t, "private-mod", "private-mod-1.4.jar", "client", `"depends":{"fabricloader":">=0.17"}`)
	synced := filepath.Join(h.dir, "synced")
	writeOverride(t, synced, "mods/private-mod-1.4.jar", string(jar.data))

	rep := pullReport(t, h, "mods/private-mod-1.4.jar", "--into", synced)
	if want := []string{"mods/private-mod-1.4.jar -> synced/mods/private-mod-1.4.jar"}; !reflect.DeepEqual(rep.Entries, want) {
		t.Fatalf("the report names the path the entry points at: %q", rep.Entries)
	}
	if got := h.readManifest(t).Requires["private-mod"].File; got != "synced/mods/private-mod-1.4.jar" {
		t.Fatalf("a file inside the project is referenced where it lies: %q", got)
	}
}
