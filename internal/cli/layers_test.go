package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func buildWarnings(t *testing.T, h *harness, args ...string) []string {
	t.Helper()
	stdout := h.mustRun(t, append(args, "--json")...)
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	return env.Warnings
}

func twoSided(t *testing.T, h *harness) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true}
	})
}

func TestOverrideFoldersLayerInOrder(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.editManifest(t, func(m map[string]any) {
		m["features"] = map[string]any{
			"alpha": map[string]any{"default": true},
			"zulu":  map[string]any{"default": true},
			"off":   map[string]any{},
		}
	})
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "layered.txt"), "base\n")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "shared.txt"), "shared\n")
	writeFile(t, filepath.Join(h.dir, "client-overrides", "config", "layered.txt"), "client\n")
	writeFile(t, filepath.Join(h.dir, "server-overrides", "config", "layered.txt"), "server\n")
	writeFile(t, filepath.Join(h.dir, "alpha-overrides", "config", "layered.txt"), "alpha\n")
	writeFile(t, filepath.Join(h.dir, "zulu-overrides", "config", "layered.txt"), "zulu\n")
	writeFile(t, filepath.Join(h.dir, "off-overrides", "config", "never.txt"), "off\n")

	warnings := buildWarnings(t, h, "build")
	if got := readBuilt(t, h, "config/layered.txt"); got != "zulu\n" {
		t.Fatalf("last enabled feature wins: %q", got)
	}
	if got := readBuilt(t, h, "config/shared.txt"); got != "shared\n" {
		t.Fatalf("shared override: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", "never.txt")); !os.IsNotExist(err) {
		t.Fatal("a feature that is off must not lay down its folder")
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "alpha and zulu both write config/layered.txt; zulu wins") {
		t.Fatalf("feature conflict warning: %q", warnings)
	}
	if joined := strings.Join(warnings, "\n"); strings.Contains(joined, "client") {
		t.Fatalf("a feature overriding a base layer says nothing: %q", warnings)
	}
}

func TestOverrideFoldersFollowTheSide(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	twoSided(t, h)
	h.editManifest(t, func(m map[string]any) {
		m["variables"] = map[string]any{"motd": "shared"}
		m["server"].(map[string]any)["variables"] = map[string]any{"motd": "the server"}
		m["server"].(map[string]any)["properties"] = map[string]any{"motd": "${motd}"}
	})
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "both.txt"), "both\n")
	writeFile(t, filepath.Join(h.dir, "client-overrides", "config", "client.txt"), "client\n")
	writeFile(t, filepath.Join(h.dir, "server-overrides", "config", "server.txt"), "server\n")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "vars.txt.tmpl"), "${motd}\n")
	h.mustRun(t, "install")

	for path, want := range map[string]bool{
		"build/client/config/both.txt":   true,
		"build/client/config/client.txt": true,
		"build/client/config/server.txt": false,
		"build/server/config/both.txt":   true,
		"build/server/config/server.txt": true,
		"build/server/config/client.txt": false,
	} {
		_, err := os.Stat(filepath.Join(h.dir, filepath.FromSlash(path)))
		if got := err == nil; got != want {
			t.Errorf("%s present = %v, want %v", path, got, want)
		}
	}
	if got := readFile(t, filepath.Join(h.dir, "build", "server", "config", "vars.txt")); got != "the server\n" {
		t.Errorf("the side's variables layer over the project's: %q", got)
	}
	if got := readFile(t, filepath.Join(h.dir, "build", "client", "config", "vars.txt")); got != "shared\n" {
		t.Errorf("a side without its own variable keeps the project's: %q", got)
	}
}

func TestFeatureFolderForms(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	twoSided(t, h)
	h.editManifest(t, func(m map[string]any) {
		m["features"] = map[string]any{
			"minimap": map[string]any{"default": true, "overrides": "extras/minimap"},
			"voice":   map[string]any{"default": true, "overrides": map[string]any{"client": "voice-client", "server": "voice-server"}},
		}
	})
	writeFile(t, filepath.Join(h.dir, "extras", "minimap", "config", "minimap.txt"), "minimap\n")
	writeFile(t, filepath.Join(h.dir, "voice-client", "config", "voice.txt"), "client voice\n")
	writeFile(t, filepath.Join(h.dir, "voice-server", "config", "voice.txt"), "server voice\n")
	writeFile(t, filepath.Join(h.dir, "minimap-overrides", "config", "unused.txt"), "unused\n")
	h.mustRun(t, "install")

	if got := readBuilt(t, h, "config/minimap.txt"); got != "minimap\n" {
		t.Errorf("a named folder applies to both sides: %q", got)
	}
	if got := readFile(t, filepath.Join(h.dir, "build", "server", "config", "minimap.txt")); got != "minimap\n" {
		t.Errorf("a named folder applies to the server too: %q", got)
	}
	if got := readBuilt(t, h, "config/voice.txt"); got != "client voice\n" {
		t.Errorf("the client's own folder: %q", got)
	}
	if got := readFile(t, filepath.Join(h.dir, "build", "server", "config", "voice.txt")); got != "server voice\n" {
		t.Errorf("the server's own folder: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", "unused.txt")); !os.IsNotExist(err) {
		t.Error("a declared folder replaces the default one")
	}
}

func TestFeaturesSettingOneKeyDifferentlyWarn(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.editManifest(t, func(m map[string]any) {
		m["features"] = map[string]any{
			"alpha": map[string]any{"default": true},
			"zulu":  map[string]any{"default": true},
		}
	})
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "mod.properties"), "base=1\nscale=1\nquiet=1\n")
	writeFile(t, filepath.Join(h.dir, "alpha-overrides", "config", "mod.properties"), "scale=2\nquiet=9\n")
	writeFile(t, filepath.Join(h.dir, "zulu-overrides", "config", "mod.properties"), "scale=3\nquiet=9\n")

	warnings := strings.Join(buildWarnings(t, h, "build"), "\n")
	if !strings.Contains(warnings, "alpha and zulu set scale in config/mod.properties differently; zulu wins") {
		t.Fatalf("differing key warns: %q", warnings)
	}
	if strings.Contains(warnings, "quiet") {
		t.Fatalf("the same value in both is no conflict: %q", warnings)
	}
	if got := readBuilt(t, h, "config/mod.properties"); !strings.Contains(got, "scale=3") || !strings.Contains(got, "base=1") {
		t.Fatalf("merged properties: %q", got)
	}
}

func TestPulledPackFeaturesMergeIntoTheProject(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	base := filepath.Join(h.dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(base, "shulker.json"), `{"name": "base", "minecraft": "~26.2", "loader": {"type": "fabric", "version": "*"},
  "features": {"shaders": {"default": true, "note": "the pack's note"}, "voice": {}},
  "requires": {}, "client": {}}`)
	writeFile(t, filepath.Join(base, "overrides", "config", "base.txt"), "from the pack\n")
	writeFile(t, filepath.Join(base, "shaders-overrides", "config", "shade.txt"), "pack\n")
	writeFile(t, filepath.Join(h.dir, "shaders-overrides", "config", "shade.txt"), "project\n")
	h.editManifest(t, func(m map[string]any) {
		m["features"] = map[string]any{"shaders": map[string]any{"default": false, "note": "the project's note"}}
	})
	h.mustRun(t, "modpack", "add", "./base")

	h.mustRun(t, "build")
	if got := readBuilt(t, h, "config/base.txt"); got != "from the pack\n" {
		t.Fatalf("the pack's shared overrides: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", "shade.txt")); !os.IsNotExist(err) {
		t.Fatal("the project's default must win the clash, leaving the feature off")
	}

	var listed []struct {
		Name   string `json:"name"`
		Note   string `json:"note"`
		Origin string `json:"origin"`
		On     bool   `json:"default"`
	}
	if err := json.Unmarshal([]byte(dataJSON(t, h.mustRun(t, "feature", "list", "--json"))), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Name != "shaders" || listed[0].On || listed[0].Origin != "" {
		t.Fatalf("one switch for the name both declare, and the project owns it: %+v", listed)
	}
	if listed[1].Name != "voice" || listed[1].Origin != "base" {
		t.Fatalf("a feature only the pack declares keeps its origin: %+v", listed[1])
	}
	if stdout := h.mustRun(t, "feature", "list"); !strings.Contains(stdout, "off (from base)") {
		t.Fatalf("feature list names where a feature comes from: %s", stdout)
	}

	h.mustRun(t, "build", "--with", "shaders")
	if got := readBuilt(t, h, "config/shade.txt"); got != "project\n" {
		t.Fatalf("the project's feature folder layers over the pack's: %q", got)
	}
}

func dataJSON(t *testing.T, stdout string) string {
	t.Helper()
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	data, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBuiltinVariables(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	twoSided(t, h)
	writePrismPack(t, filepath.Join(h.dir, "base"), "~26.2", "", map[string]string{
		"config/base.txt.tmpl": "${project.name} ${project.displayName} ${project.version} ${minecraft.version}\n",
	})
	h.mustRun(t, "modpack", "add", "./base")
	h.editManifest(t, func(m map[string]any) {
		m["version"] = "1.2.3"
		m["server"].(map[string]any)["properties"] = map[string]any{"motd": "${project.name} ${project.version}"}
		m["client"] = map[string]any{"name": "Pack Client", "options": map[string]any{"version": "${minecraft.dataVersion}"}}
	})
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "stamp.txt.tmpl"),
		"${project.name} ${project.displayName} ${project.version} ${minecraft.version} ${minecraft.dataVersion} ${java.major} ${loader.type} ${loader.version}\n")

	code, _, stderr := h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "base:overrides/config/base.txt.tmpl:1: variable ${project.version} is not set") {
		t.Fatalf("a pulled pack without a version doesn't take the project's: %d %s", code, stderr)
	}

	pulled := filepath.Join(h.dir, "base", "shulker.json")
	data, err := os.ReadFile(pulled)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, pulled, strings.Replace(string(data), `"name": "base",`, `"name": "base", "version": "9.0",`, 1))
	h.mustRun(t, "lock")
	h.mustRun(t, "install")

	if got := readBuilt(t, h, "config/stamp.txt"); got != "pack Pack Client 1.2.3 26.2 4903 25 fabric 0.17.3\n" {
		t.Errorf("the project's overrides see its own pack and the locked versions: %q", got)
	}
	if got := readBuilt(t, h, "config/base.txt"); got != "base base 9.0 26.2\n" {
		t.Errorf("a pulled pack's overrides see that pack's own name and version: %q", got)
	}
	if got := readFile(t, filepath.Join(h.dir, "build", "server", "server.properties")); !strings.Contains(got, "motd=pack 1.2.3\n") {
		t.Errorf("server.properties values see the built-in variables: %q", got)
	}
	if got := readBuilt(t, h, "options.txt"); !strings.Contains(got, "version:4903\n") {
		t.Errorf("client.options values see the built-in variables, unquoted: %q", got)
	}

	h.allowMrpackHost(t)
	h.mustRun(t, "export", "mrpack", "--version", "2.0")
	archive := readArchive(t, filepath.Join(h.dir, "build", "pack-2.0.mrpack"))
	if got := archive["client-overrides/config/stamp.txt"]; got != "pack Pack Client 2.0 26.2 4903 25 fabric 0.17.3\n" {
		t.Errorf("an export's --version stands in for the project's version: %q", got)
	}
	if got := archive["overrides/config/base.txt"]; got != "base base 9.0 26.2\n" {
		t.Errorf("an export's --version leaves a pulled pack's own version: %q", got)
	}

	h.editManifest(t, func(m map[string]any) { delete(m, "version") })
	code, _, stderr = h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "${project.version} is not set") || !strings.Contains(stderr, "help: run shulker set version <version>") {
		t.Fatalf("a manifest without a version leaves ${project.version} unset: %d %s", code, stderr)
	}

	writeFile(t, filepath.Join(h.dir, "overrides", "config", "stamp.txt.tmpl"), "${project.nme}\n")
	code, _, stderr = h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "did you mean:") || !strings.Contains(stderr, "‣ project.name") {
		t.Fatalf("a misspelt variable suggests the closest: %d %s", code, stderr)
	}
}

func TestAnUnreadableDataVersionLeavesTheLockToTheNextRelock(t *testing.T) {
	h := newHarness(t)
	h.noRanges = true
	code, _, stderr := h.run(t, "init", "--yes", "--loader", "fabric")
	if code != 0 || !strings.Contains(stderr, "couldn't read the Minecraft 26.2 data version") {
		t.Fatalf("an unreadable data version warns rather than failing the lock: %d %s", code, stderr)
	}
	if l := h.readLock(t); l.DataVersion != 0 {
		t.Fatalf("dataVersion = %d, want unset", l.DataVersion)
	}
	writeFile(t, filepath.Join(h.dir, "overrides", "v.txt.tmpl"), "${minecraft.dataVersion}")
	code, _, stderr = h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "help: run shulker lock") {
		t.Fatalf("an unset data version says how to fill it: %d %s", code, stderr)
	}

	h.noRanges = false
	h.mustRun(t, "lock")
	if l := h.readLock(t); l.DataVersion != 4903 {
		t.Fatalf("the next relock fills the data version: %d", l.DataVersion)
	}
}
