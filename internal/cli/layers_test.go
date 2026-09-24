package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func twoSided(t *testing.T, h *harness) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true}
	})
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
